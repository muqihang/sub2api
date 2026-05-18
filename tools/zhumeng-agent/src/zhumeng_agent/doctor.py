from __future__ import annotations

import json
import os
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
    }
