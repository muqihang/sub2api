from __future__ import annotations

import json
from pathlib import Path


STATUS_AVAILABLE = "available"
STATUS_CONFIGURED = "configured"
STATUS_USER_ACTION_REQUIRED = "user_action_required"
STATUS_BLOCKED_BY_REGION_OR_POLICY = "blocked_by_region_or_policy"
STATUS_UNSUPPORTED_CODEX_VERSION = "unsupported_codex_version"
STATUS_MISSING_BUNDLE = "missing_bundle"
STATUS_UNKNOWN = "unknown"


def inspect_codex_plugins(
    codex_home: Path,
    *,
    native_host_manifest: Path | None = None,
    chrome_extensions_dir: Path | None = None,
) -> dict[str, dict[str, object]]:
    bundled_root = codex_home / "plugins" / "cache" / "openai-bundled"
    computer_use_manifest = find_plugin_manifest(bundled_root, "computer-use")
    browser_use_manifest = find_plugin_manifest(bundled_root, "browser-use")
    chrome_manifest = find_plugin_manifest(bundled_root, "chrome")

    helper_app = codex_home / "computer-use" / "Codex Computer Use.app"
    report = {
        "computer-use": inspect_computer_use(computer_use_manifest, helper_app),
        "browser-use": inspect_browser_use(browser_use_manifest),
        "chrome": inspect_chrome(
            chrome_manifest,
            native_host_manifest=native_host_manifest,
            chrome_extensions_dir=chrome_extensions_dir,
        ),
    }
    return report


def inspect_computer_use(manifest: Path | None, helper_app: Path) -> dict[str, object]:
    if manifest is None:
        return {"status": STATUS_MISSING_BUNDLE}
    if helper_app.exists():
        return {"status": STATUS_CONFIGURED, "manifest": str(manifest), "helper_app": str(helper_app)}
    return {"status": STATUS_AVAILABLE, "manifest": str(manifest)}


def inspect_browser_use(manifest: Path | None) -> dict[str, object]:
    if manifest is None:
        return {"status": STATUS_MISSING_BUNDLE}
    return {"status": STATUS_AVAILABLE, "manifest": str(manifest)}


def inspect_chrome(
    manifest: Path | None,
    *,
    native_host_manifest: Path | None,
    chrome_extensions_dir: Path | None,
) -> dict[str, object]:
    if manifest is None:
        return {"status": STATUS_MISSING_BUNDLE}
    if native_host_manifest is None or not native_host_manifest.exists():
        return {"status": STATUS_USER_ACTION_REQUIRED, "manifest": str(manifest)}

    payload = json.loads(native_host_manifest.read_text(encoding="utf-8"))
    origins = payload.get("allowed_origins", [])
    extension_id = None
    if origins:
        origin = str(origins[0])
        extension_id = origin.removeprefix("chrome-extension://").rstrip("/")

    if not extension_id or chrome_extensions_dir is None:
        return {"status": STATUS_USER_ACTION_REQUIRED, "manifest": str(manifest)}

    extension_path = chrome_extensions_dir / extension_id
    if not extension_path.exists():
        return {"status": STATUS_USER_ACTION_REQUIRED, "manifest": str(manifest), "extension_id": extension_id}

    return {
        "status": STATUS_CONFIGURED,
        "manifest": str(manifest),
        "native_host_manifest": str(native_host_manifest),
        "extension_id": extension_id,
    }


def find_plugin_manifest(bundled_root: Path, plugin_name: str) -> Path | None:
    plugin_root = bundled_root / plugin_name
    if not plugin_root.exists():
        return None
    manifests = sorted(plugin_root.glob("*/.codex-plugin/plugin.json"))
    if manifests:
        return manifests[-1]
    latest = plugin_root / "latest" / ".codex-plugin" / "plugin.json"
    return latest if latest.exists() else None
