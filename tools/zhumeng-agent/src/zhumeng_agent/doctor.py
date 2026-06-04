from __future__ import annotations

import hashlib
import json
import os
import tomllib
from datetime import datetime, timezone
from pathlib import Path

from .adapters.codex.model_picker import (
    ModelPickerPatchError,
    inspect_model_picker_app,
    inspect_plugin_auth_gate_app,
    inspect_plugin_mention_marketplace_app,
)
from .adapters.codex.plugins import inspect_codex_plugins
from .adapters.codex.capture_config import CodexDesktopCaptureConfig
from .platform_paths import state_dir
from .adapters.codex.config_manager import CODEX_MODEL_CATALOG_FILE, CODEX_ROUTING_BRIDGE_INSTRUCTIONS
from .state import JsonStateStore


def load_desktop_capture_config() -> CodexDesktopCaptureConfig:
    store = JsonStateStore(state_dir() / "state.json")
    state = store.read()
    enabled = bool(state.get("desktop_capture_enabled", False))
    correlation_key = state.get("desktop_capture_correlation_hash_key_file")
    correlation_path = Path(str(correlation_key)).expanduser() if correlation_key else None
    return CodexDesktopCaptureConfig.defaults(
        enabled=enabled,
        correlation_hash_key_file=correlation_path,
    )


def capture_install_manifest_state(config: CodexDesktopCaptureConfig) -> dict[str, object]:
    manifest_path = config.base_dir / "capture_install.json"
    if not manifest_path.exists():
        return {"status": "not_installed"}
    try:
        payload = json.loads(manifest_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return {"status": "invalid_manifest"}
    return {
        "status": "installed" if bool(payload.get("enabled")) else "uninstalled",
        "hook_mode": payload.get("hook_mode", "unknown"),
        "app_asar_modified": payload.get("app_asar_modified"),
    }


def chrome_integration_paths(platform_name: str | None = None) -> tuple[Path | None, Path | None]:
    platform_name = platform_name or os.name
    if platform_name == "nt":
        local_app_data = os.environ.get("LOCALAPPDATA")
        if not local_app_data:
            return None, None
        root = Path(local_app_data) / "Google" / "Chrome" / "User Data"
        return (
            root / "NativeMessagingHosts" / "com.openai.codexextension.json",
            root / "Default" / "Extensions",
        )
    home = Path.home()
    return (
        home / "Library" / "Application Support" / "Google" / "Chrome" / "NativeMessagingHosts" / "com.openai.codexextension.json",
        home / "Library" / "Application Support" / "Google" / "Chrome" / "Default" / "Extensions",
    )


def codex_doctor_report(
    codex_home: Path,
    *,
    codex_app_path: Path | None = None,
    native_host_manifest: Path | None = None,
    chrome_extensions_dir: Path | None = None,
    state: dict[str, object] | None = None,
) -> dict[str, object]:
    capture_config = load_desktop_capture_config()
    if native_host_manifest is None and chrome_extensions_dir is None:
        native_host_manifest, chrome_extensions_dir = chrome_integration_paths(os.name)
    model_picker: dict[str, object]
    if codex_app_path is None:
        model_picker = {"status": "app_not_found"}
        plugin_auth_gate: dict[str, object] = {"status": "app_not_found"}
        plugin_mention_marketplace: dict[str, object] = {"status": "app_not_found"}
    else:
        try:
            model_picker = inspect_model_picker_app(codex_app_path)
        except ModelPickerPatchError as err:
            model_picker = {
                "status": "failed",
                "message": str(err),
            }
        try:
            plugin_auth_gate = inspect_plugin_auth_gate_app(codex_app_path)
        except ModelPickerPatchError as err:
            plugin_auth_gate = {
                "status": "failed",
                "message": str(err),
            }
        try:
            plugin_mention_marketplace = inspect_plugin_mention_marketplace_app(codex_app_path)
        except ModelPickerPatchError as err:
            plugin_mention_marketplace = {
                "status": "failed",
                "message": str(err),
            }
    return {
        "client": "codex",
        "plugins": inspect_codex_plugins(
            codex_home,
            native_host_manifest=native_host_manifest,
            chrome_extensions_dir=chrome_extensions_dir,
        ),
        "model_picker": model_picker,
        "plugin_auth_gate": plugin_auth_gate,
        "plugin_mention_marketplace": plugin_mention_marketplace,
        "desktop_capture": {
            "config": capture_config.public_dict(),
            "installation": capture_install_manifest_state(capture_config),
        },
        "model_catalog_freshness": codex_model_catalog_freshness(codex_home, state=state),
        "skills_evidence": codex_skills_evidence(codex_home),
    }


def _read_codex_config(codex_home: Path) -> dict[str, object]:
    config_path = codex_home / "config.toml"
    if not config_path.exists():
        return {}
    try:
        parsed = tomllib.loads(config_path.read_text(encoding="utf-8"))
    except (OSError, tomllib.TOMLDecodeError):
        return {}
    return parsed if isinstance(parsed, dict) else {}


def _sha256_file(path: Path) -> str | None:
    if not path.exists() or not path.is_file():
        return None
    digest = hashlib.sha256()
    try:
        with path.open("rb") as handle:
            for chunk in iter(lambda: handle.read(1024 * 1024), b""):
                digest.update(chunk)
    except OSError:
        return None
    return digest.hexdigest()


def _catalog_path_from_config(codex_home: Path, parsed: dict[str, object]) -> Path:
    raw = parsed.get("model_catalog_json") or parsed.get("model_catalog_file") or CODEX_MODEL_CATALOG_FILE
    path = Path(str(raw)).expanduser()
    if not path.is_absolute():
        path = codex_home / path
    return path


def _read_model_catalog(path: Path) -> dict[str, object]:
    if not path.exists():
        return {"models": []}
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return {"models": []}
    return payload if isinstance(payload, dict) else {"models": []}


def _catalog_models(catalog: dict[str, object]) -> list[dict[str, object]]:
    models = catalog.get("models")
    if not isinstance(models, list):
        return []
    return [model for model in models if isinstance(model, dict)]


def _model_slug(model: dict[str, object]) -> str:
    return str(model.get("slug") or model.get("model") or model.get("id") or "").strip()


def codex_model_catalog_freshness(codex_home: Path, *, state: dict[str, object] | None = None) -> dict[str, object]:
    state = state or {}
    parsed = _read_codex_config(codex_home)
    catalog_path = _catalog_path_from_config(codex_home, parsed)
    catalog = _read_model_catalog(catalog_path)
    models = _catalog_models(catalog)
    slugs = sorted(slug for model in models if (slug := _model_slug(model)))
    deepseek = sorted(slug for slug in slugs if slug.startswith("deepseek-"))
    catalog_hash = _sha256_file(catalog_path)
    config_hash = _sha256_file(codex_home / "config.toml")
    reasons: list[str] = []
    if state.get("catalog_hash_after") and catalog_hash and str(state.get("catalog_hash_after")) != catalog_hash:
        reasons.append("catalog_hash_changed")
    if state.get("config_hash_after") and config_hash and str(state.get("config_hash_after")) != config_hash:
        reasons.append("config_hash_changed")
    if state.get("restart_required") is True:
        reasons.append("state_restart_required")
    reasons = sorted(set(reasons))
    mtime = None
    if catalog_path.exists():
        try:
            mtime = datetime.fromtimestamp(catalog_path.stat().st_mtime, timezone.utc).isoformat().replace("+00:00", "Z")
        except OSError:
            mtime = None
    return {
        "model_catalog_json": str(catalog_path),
        "catalog_exists": catalog_path.exists(),
        "catalog_hash": catalog_hash,
        "catalog_mtime": mtime,
        "model_count": len(models),
        "active_default_model": str(parsed.get("model") or "").strip() or None,
        "catalog_has_deepseek": bool(deepseek),
        "deepseek_models_present": deepseek,
        "claude_model_count": sum(1 for slug in slugs if slug.startswith("claude-")),
        "restart_required": bool(reasons),
        "restart_required_reasons": reasons,
        "app_server_refresh_boundary": "process_start_or_app_server_cache; restart Codex after catalog/config changes unless capture proves live refresh",
    }


def _dir_status(path: Path) -> dict[str, object]:
    try:
        count = sum(1 for child in path.iterdir() if child.is_dir()) if path.exists() else 0
    except OSError:
        count = 0
    return {"path": str(path), "present": path.exists(), "entry_count": count}


def codex_skills_evidence(codex_home: Path) -> dict[str, object]:
    parsed = _read_codex_config(codex_home)
    marketplaces = parsed.get("marketplaces") if isinstance(parsed.get("marketplaces"), dict) else {}
    plugins = parsed.get("plugins") if isinstance(parsed.get("plugins"), dict) else {}
    enabled_plugins = sorted(
        name for name, settings in plugins.items()
        if isinstance(name, str) and isinstance(settings, dict) and bool(settings.get("enabled", True))
    )
    catalog = _read_model_catalog(_catalog_path_from_config(codex_home, parsed))
    routing_phrase = "skills, plugins, MCP servers, or tool routing guidance"
    deepseek_with_guidance: list[str] = []
    checked_models: list[str] = []
    routing_present = False
    for model in _catalog_models(catalog):
        slug = _model_slug(model)
        if not slug:
            continue
        checked_models.append(slug)
        base = str(model.get("base_instructions") or "")
        has_guidance = routing_phrase in base or CODEX_ROUTING_BRIDGE_INSTRUCTIONS.strip() in base
        routing_present = routing_present or has_guidance
        if slug.startswith("deepseek-") and has_guidance:
            deepseek_with_guidance.append(slug)
    plugin_skill_paths = sorted((codex_home / "plugins" / "cache").glob("*/*/*/skills/*")) if (codex_home / "plugins" / "cache").exists() else []
    return {
        "configured_marketplaces": sorted(str(name) for name in marketplaces.keys()),
        "enabled_plugins": enabled_plugins,
        "skills_dirs": [_dir_status(codex_home / "skills")],
        "superpowers_skills_dirs": [_dir_status(codex_home / "superpowers" / "skills")],
        "plugin_cache_skill_paths": [_dir_status(path) for path in plugin_skill_paths[:20]],
        "base_instruction_routing_guidance_present": routing_present,
        "checked_models": sorted(checked_models),
        "deepseek_models_with_routing_guidance": sorted(deepseek_with_guidance),
        "evidence_only": True,
    }
