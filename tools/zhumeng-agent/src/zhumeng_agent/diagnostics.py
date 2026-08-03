from __future__ import annotations

import os
import stat
from pathlib import Path
from typing import Any

from .desktop_schema import redact


def desktop_diagnostic_report(*, state: dict[str, Any], doctor: dict[str, Any], codex_home: Path) -> dict[str, Any]:
    return redact({
        "state": public_state(state),
        "doctor": doctor,
        "security": {
            "state_file_private": state_file_private(state.get("state_file")),
            "codex_home": str(codex_home),
        },
    })


def public_state(state: dict[str, Any]) -> dict[str, Any]:
    allowed = {
        "status",
        "client",
        "server_base_url",
        "gateway_base_url",
        "device_id",
        "proxy_port",
        "proxy_pid",
        "model_catalog_meta",
        "desktop_capture_enabled",
        "restore_status",
    }
    visible = {key: value for key, value in state.items() if key in allowed or key.endswith("_hash_after")}
    session = state.get("managed_session_id")
    if session:
        text = str(session)
        visible["managed_session_id_redacted"] = f"...{text[-4:]}" if len(text) > 4 else "<redacted>"
    for key in state:
        if key.endswith("token") or "secret" in key:
            visible[key] = "<redacted>"
    return visible


def state_file_private(path_value: object) -> bool | None:
    if not path_value:
        return None
    path = Path(str(path_value))
    if not path.exists():
        return None
    if os.name != "posix":
        return None
    mode = stat.S_IMODE(path.stat().st_mode)
    return mode & 0o077 == 0
