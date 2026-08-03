from __future__ import annotations

import socket
from pathlib import Path


def detect_codex_app_path(*, search_roots: list[Path], platform: str) -> Path | None:
    if platform == "darwin":
        for root in search_roots:
            candidate = root / "Codex.app"
            if candidate.exists():
                return candidate
        return None
    if platform == "win32":
        for root in search_roots:
            candidate = root / "Codex.exe"
            if candidate.exists():
                return candidate
        return None
    return None


def select_cdp_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


def build_codex_launch_command(app_path: Path, cdp_port: int) -> list[str]:
    if app_path.suffix == ".app":
        executable = app_path / "Contents" / "MacOS" / "Codex"
    else:
        executable = app_path
    return [
        str(executable),
        f"--remote-debugging-port={cdp_port}",
        f"--remote-allow-origins=http://127.0.0.1:{cdp_port}",
    ]
