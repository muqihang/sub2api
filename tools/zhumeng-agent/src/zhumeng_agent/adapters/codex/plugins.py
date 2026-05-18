from __future__ import annotations

import json
import platform
import re
import subprocess
from pathlib import Path
from typing import Callable


STATUS_AVAILABLE = "available"
STATUS_CONFIGURED = "configured"
STATUS_USER_ACTION_REQUIRED = "user_action_required"
STATUS_BLOCKED_BY_REGION_OR_POLICY = "blocked_by_region_or_policy"
STATUS_UNSUPPORTED_CODEX_VERSION = "unsupported_codex_version"
STATUS_UNSUPPORTED_OS = "unsupported_os"
STATUS_MISSING_BUNDLE = "missing_bundle"
STATUS_UNKNOWN = "unknown"


def inspect_codex_plugins(
    codex_home: Path,
    *,
    native_host_manifest: Path | None = None,
    chrome_extensions_dir: Path | None = None,
    macos_version: str | None = None,
    runner: Callable[..., subprocess.CompletedProcess[str]] = subprocess.run,
) -> dict[str, dict[str, object]]:
    bundled_root = codex_home / "plugins" / "cache" / "openai-bundled"
    computer_use_manifest = find_plugin_manifest(bundled_root, "computer-use")
    browser_use_manifest = find_plugin_manifest(bundled_root, "browser-use")
    chrome_manifest = find_plugin_manifest(bundled_root, "chrome")

    helper_app = codex_home / "computer-use" / "Codex Computer Use.app"
    report = {
        "computer-use": inspect_computer_use(
            computer_use_manifest,
            helper_app,
            macos_version=macos_version,
            runner=runner,
        ),
        "browser-use": inspect_browser_use(browser_use_manifest),
        "chrome": inspect_chrome(
            chrome_manifest,
            native_host_manifest=native_host_manifest,
            chrome_extensions_dir=chrome_extensions_dir,
        ),
    }
    return report


def inspect_computer_use(
    manifest: Path | None,
    helper_app: Path,
    *,
    macos_version: str | None = None,
    runner: Callable[..., subprocess.CompletedProcess[str]] = subprocess.run,
) -> dict[str, object]:
    if manifest is None:
        return {"status": STATUS_MISSING_BUNDLE}
    if helper_app.exists():
        mcp_client = computer_use_mcp_client_path(helper_app)
        current_macos = macos_version or platform.mac_ver()[0]
        required_macos = read_macos_minos(mcp_client, runner=runner) if mcp_client.exists() else None
        if required_macos is not None and current_macos and compare_versions(current_macos, required_macos) < 0:
            return {
                "status": STATUS_UNSUPPORTED_OS,
                "manifest": str(manifest),
                "helper_app": str(helper_app),
                "mcp_client": str(mcp_client),
                "current_macos": current_macos,
                "required_macos": required_macos,
                "message": f"Computer Use MCP client requires macOS {required_macos} or newer.",
            }
        return {
            "status": STATUS_CONFIGURED,
            "manifest": str(manifest),
            "helper_app": str(helper_app),
            **({"mcp_client": str(mcp_client)} if mcp_client.exists() else {}),
        }
    return {"status": STATUS_AVAILABLE, "manifest": str(manifest)}


def computer_use_mcp_client_path(helper_app: Path) -> Path:
    return (
        helper_app
        / "Contents"
        / "SharedSupport"
        / "SkyComputerUseClient.app"
        / "Contents"
        / "MacOS"
        / "SkyComputerUseClient"
    )


def read_macos_minos(
    executable: Path,
    *,
    runner: Callable[..., subprocess.CompletedProcess[str]] = subprocess.run,
) -> str | None:
    completed = runner(["otool", "-l", str(executable)], capture_output=True, text=True, check=False)
    if completed.returncode != 0:
        return None
    in_build_version = False
    in_macos_build_version = False
    for raw_line in completed.stdout.splitlines():
        line = raw_line.strip()
        if line == "cmd LC_BUILD_VERSION":
            in_build_version = True
            in_macos_build_version = False
            continue
        if in_build_version and line.startswith("platform "):
            in_macos_build_version = line == "platform 1"
            continue
        if in_build_version and in_macos_build_version and line.startswith("minos "):
            return line.removeprefix("minos ").strip()
    return None


def compare_versions(left: str, right: str) -> int:
    left_parts = version_parts(left)
    right_parts = version_parts(right)
    width = max(len(left_parts), len(right_parts))
    left_parts.extend([0] * (width - len(left_parts)))
    right_parts.extend([0] * (width - len(right_parts)))
    if left_parts == right_parts:
        return 0
    return -1 if left_parts < right_parts else 1


def version_parts(version: str) -> list[int]:
    return [int(part) for part in re.findall(r"\d+", version)]


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
