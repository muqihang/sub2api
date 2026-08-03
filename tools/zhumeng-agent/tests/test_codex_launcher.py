from pathlib import Path

from zhumeng_agent.adapters.codex.launcher import (
    build_codex_launch_command,
    detect_codex_app_path,
    select_cdp_port,
)


def test_detects_macos_app_path(tmp_path: Path):
    app = tmp_path / "Codex.app"
    (app / "Contents" / "MacOS").mkdir(parents=True)
    assert detect_codex_app_path(search_roots=[tmp_path], platform="darwin") == app


def test_detects_windows_app_path_stub(tmp_path: Path):
    app = tmp_path / "Codex.exe"
    app.write_text("", encoding="utf-8")
    assert detect_codex_app_path(search_roots=[tmp_path], platform="win32") == app


def test_cdp_port_avoids_conflicts():
    port = select_cdp_port()
    assert port > 0


def test_build_launch_command_is_non_fatal_dry_structure(tmp_path: Path):
    app = tmp_path / "Codex.app"
    (app / "Contents" / "MacOS").mkdir(parents=True)
    command = build_codex_launch_command(app, 9222)
    assert "--remote-debugging-port=9222" in command
