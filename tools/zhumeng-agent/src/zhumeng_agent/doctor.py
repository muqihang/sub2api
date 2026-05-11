from __future__ import annotations

import os
from pathlib import Path

from .adapters.codex.plugins import inspect_codex_plugins


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


def codex_doctor_report(codex_home: Path, *, native_host_manifest: Path | None = None, chrome_extensions_dir: Path | None = None) -> dict[str, object]:
    if native_host_manifest is None and chrome_extensions_dir is None:
        native_host_manifest, chrome_extensions_dir = chrome_integration_paths(os.name)
    return {
        "client": "codex",
        "plugins": inspect_codex_plugins(
            codex_home,
            native_host_manifest=native_host_manifest,
            chrome_extensions_dir=chrome_extensions_dir,
        ),
    }
