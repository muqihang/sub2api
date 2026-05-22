import json

import pytest
import platform
from pathlib import Path

import zhumeng_agent.cli as cli
from zhumeng_agent.cli import main


@pytest.fixture(autouse=True)
def restore_cli_defaults():
    originals = {
        name: getattr(cli, name)
        for name in (
            "default_state_store",
            "default_http_client",
            "default_config_manager",
            "generate_loopback_secret",
            "choose_local_proxy_port",
            "ensure_proxy_running",
            "default_codex_app_path",
            "build_codex_launch_command",
            "select_cdp_port",
            "launch_codex_process",
            "inspect_codex_enhancements",
            "patch_codex_enhancements",
            "restore_codex_enhancements",
            "resolve_codex_home",
            "codex_doctor_report",
        )
        if hasattr(cli, name)
    }
    yield
    for name, value in originals.items():
        setattr(cli, name, value)


def parse_output(capsys):
    out = capsys.readouterr().out.strip()
    assert out
    return json.loads(out)


class Store:
    def read(self):
        return {
            "status": "configured",
            "client": "codex",
            "access_token": "access-token-secret",
            "refresh_token": "refresh-token-secret",
            "loopback_secret": "loopback-secret-secret",
            "server_base_url": "https://example.com",
            "proxy_port": 18081,
            "user_email": "alice@example.com",
            "codex_home": str(Path.home() / ".codex"),
        }


def test_desktop_diagnose_redacts_token_email_machine_name_and_home(capsys):
    cli.default_state_store = lambda: Store()
    cli.resolve_codex_home = lambda: Path.home() / ".codex"
    cli.default_codex_app_path = lambda: Path("/Applications/Codex.app")
    cli.codex_doctor_report = lambda *args, **kwargs: {
        "client": "codex",
        "message": f"alice@example.com {platform.node()} {Path.home()} access-token-secret refresh-token-secret loopback-secret-secret",
    }

    exit_code = main(["desktop", "diagnose", "--redacted", "--json"])

    assert exit_code == 0
    payload = parse_output(capsys)
    assert payload["ok"] is True
    assert payload["command"] == "desktop diagnose"
    dumped = json.dumps(payload)
    assert "access-token-secret" not in dumped
    assert "refresh-token-secret" not in dumped
    assert "loopback-secret-secret" not in dumped
    assert "alice@example.com" not in dumped
    assert platform.node() not in dumped
    assert str(Path.home()) not in dumped
    assert "<redacted" in dumped
