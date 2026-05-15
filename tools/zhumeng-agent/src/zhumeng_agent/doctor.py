from __future__ import annotations

import os
from pathlib import Path

from .adapters.codex.model_picker import ModelPickerPatchError, inspect_model_picker_app, inspect_plugin_auth_gate_app
from .adapters.codex.plugins import inspect_codex_plugins
from .adapters.codex.capture_config import CodexDesktopCaptureConfig


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
    if native_host_manifest is None and chrome_extensions_dir is None:
        native_host_manifest, chrome_extensions_dir = chrome_integration_paths(os.name)
    model_picker: dict[str, object]
    if codex_app_path is None:
        model_picker = {"status": "app_not_found"}
        plugin_auth_gate: dict[str, object] = {"status": "app_not_found"}
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
    return {
        "client": "codex",
        "plugins": inspect_codex_plugins(
            codex_home,
            native_host_manifest=native_host_manifest,
            chrome_extensions_dir=chrome_extensions_dir,
        ),
        "model_picker": model_picker,
        "plugin_auth_gate": plugin_auth_gate,
        "desktop_capture": CodexDesktopCaptureConfig.defaults().public_dict(),
    }
