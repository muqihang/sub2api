import json
from pathlib import Path

import pytest

import zhumeng_agent.cli as cli
from zhumeng_agent.cli import main


def parse_output(capsys):
    output = capsys.readouterr().out.strip()
    assert output, "expected CLI to print JSON"
    return json.loads(output)


def test_setup_parses_codex_setup_args(capsys, tmp_path: Path):
    class FakeClient:
        def exchange_setup_grant(self, **kwargs):
            return {
                "access_token": "access-token",
                "refresh_token": "refresh-token",
                "managed_session_id": "sess-1",
                "expires_at": "2026-05-11T12:00:00Z",
                "device_id": 9,
                "server_base_url": "https://example.com",
                "gateway_base_url": "https://example.com",
                "config_profile": {
                    "model_provider": "zhumeng-managed",
                    "wire_api": "responses",
                    "requires_openai_auth": True,
                    "supports_websockets": True,
                },
            }

    class FakeStore:
        def __init__(self):
            self.payload = None

        def write(self, payload):
            self.payload = payload

    fake_store = FakeStore()
    auth_path = tmp_path / ".codex" / "auth.json"
    auth_path.parent.mkdir(parents=True, exist_ok=True)
    auth_path.write_text('{"OPENAI_API_KEY":"legacy-secret"}', encoding="utf-8")
    cli.default_http_client = lambda server: FakeClient()
    cli.default_state_store = lambda: fake_store
    cli.default_config_manager = lambda: __import__("zhumeng_agent.adapters.codex.config_manager", fromlist=["CodexConfigManager"]).CodexConfigManager(tmp_path / ".codex")
    cli.generate_loopback_secret = lambda: "loopback-secret"
    cli.choose_local_proxy_port = lambda preferred=None: 18081
    cli.ensure_proxy_running = lambda store: 9999

    exit_code = main([
        "setup",
        "--client", "codex",
        "--code", "abc",
        "--server", "https://example.com",
    ])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "setup"
    assert data["client"] == "codex"
    assert data["server"] == "https://example.com"
    assert data["code_redacted"] is True
    assert data["status"] == "configured"
    assert fake_store.payload["device_id"] == 9
    assert fake_store.payload["prior_auth_json"]


def test_deeplink_invocation_dispatches_to_setup(capsys, tmp_path: Path):
    class FakeClient:
        def exchange_setup_grant(self, **kwargs):
            return {
                "access_token": "access-token",
                "refresh_token": "refresh-token",
                "managed_session_id": "sess-1",
                "expires_at": "2026-05-11T12:00:00Z",
                "device_id": 9,
                "server_base_url": "https://example.com",
                "gateway_base_url": "https://example.com",
                "config_profile": {
                    "model_provider": "zhumeng-managed",
                    "wire_api": "responses",
                    "requires_openai_auth": True,
                    "supports_websockets": True,
                },
            }

    class FakeStore:
        def __init__(self):
            self.payload = None

        def write(self, payload):
            self.payload = payload

    fake_store = FakeStore()
    cli.default_http_client = lambda server: FakeClient()
    cli.default_state_store = lambda: fake_store
    cli.default_config_manager = lambda: __import__("zhumeng_agent.adapters.codex.config_manager", fromlist=["CodexConfigManager"]).CodexConfigManager(tmp_path / ".codex")
    cli.generate_loopback_secret = lambda: "loopback-secret"
    cli.choose_local_proxy_port = lambda preferred=None: 18081
    cli.ensure_proxy_running = lambda store: 9999

    exit_code = main(["zhumeng-agent://setup?client=codex&code=abc&server=https%3A%2F%2Fexample.com"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "setup"
    assert data["client"] == "codex"


def test_doctor_returns_json(capsys):
    cli.resolve_codex_home = lambda: Path("/tmp/fake-codex-home")
    cli.codex_doctor_report = lambda *args, **kwargs: {"client": "codex", "plugins": {}}
    exit_code = main(["doctor", "--json"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "doctor"
    assert data["format"] == "json"


def test_repair_requires_configured_state(capsys):
    cli.default_state_store = lambda: type("Store", (), {"read": lambda self: {}})()
    exit_code = main(["repair", "codex"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "repair"
    assert data["status"] == "not_configured"


def test_launch_codex_dry_run_returns_not_implemented_status(capsys):
    cli.default_state_store = lambda: type("Store", (), {"read": lambda self: {"proxy_port": 18081}})()
    exit_code = main(["launch", "codex", "--dry-run"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "launch"
    assert data["client"] == "codex"
    assert data["status"] == "not_configured"


def test_launch_codex_dry_run_returns_plan_when_state_exists(capsys):
    cli.default_state_store = lambda: type("Store", (), {
        "read": lambda self: {
            "proxy_port": 18081,
            "config_profile": {"model_provider": "zhumeng-managed"},
            "loopback_secret": "loopback-secret",
        }
    })()
    exit_code = main(["launch", "codex", "--dry-run"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["status"] == "planned"
    assert data["proxy_port"] == 18081


def test_codex_wrapper_parses_passthrough_args_without_launching(capsys):
    cli.default_state_store = lambda: type("Store", (), {"read": lambda self: {"proxy_port": 18081}})()
    cli.default_config_manager = lambda: type("Manager", (), {"repair": lambda *args, **kwargs: None})()
    cli.run_codex_process = lambda args, env: 0
    cli.ensure_proxy_running = lambda store: 9999
    exit_code = main(["codex", "--", "--version"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "codex"
    assert data["args"] == ["--version"]
    assert data["status"] == "not_configured"


def test_logout_local_only_removes_managed_auth_when_no_prior_auth(tmp_path: Path, capsys):
    codex_home = tmp_path / ".codex"
    codex_home.mkdir(parents=True, exist_ok=True)
    (codex_home / "auth.json").write_text('{"OPENAI_API_KEY":"zhumeng-local-managed-loopback-secret"}', encoding="utf-8")
    (codex_home / "config.toml").write_text('model_provider = "zhumeng-managed"\n', encoding="utf-8")
    backup = codex_home / "backups" / "config.toml.1.bak"
    backup.parent.mkdir(parents=True, exist_ok=True)
    backup.write_text('model_provider = "legacy"\n', encoding="utf-8")

    class FakeStore:
        def read(self):
            return {"backup_paths": [str(backup)]}

        def delete(self):
            return None

    cli.default_state_store = lambda: FakeStore()
    cli.default_config_manager = lambda: __import__("zhumeng_agent.adapters.codex.config_manager", fromlist=["CodexConfigManager"]).CodexConfigManager(codex_home)

    exit_code = main(["logout", "--local-only"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["status"] == "completed"
    assert not (codex_home / "auth.json").exists()
    assert (codex_home / "config.toml").read_text(encoding="utf-8") == 'model_provider = "legacy"\n'


def test_logout_revoke_device_calls_backend_and_cleans_local_state(tmp_path: Path, capsys):
    codex_home = tmp_path / ".codex"
    codex_home.mkdir(parents=True, exist_ok=True)
    (codex_home / "auth.json").write_text('{"OPENAI_API_KEY":"zhumeng-local-managed-loopback-secret"}', encoding="utf-8")
    (codex_home / "config.toml").write_text('model_provider = "zhumeng-managed"\n', encoding="utf-8")

    class FakeStore:
        def __init__(self):
            self.state = {
                "device_id": 9,
                "server_base_url": "https://example.com",
                "managed_session_id": "sess-1",
                "access_token": "access-token",
                "refresh_token": "refresh-token",
            }

        def read(self):
            return self.state

        def update(self, patch):
            self.state.update(patch)
            return self.state

        def delete(self):
            return None

    class FakeClient:
        def revoke_managed_device(self, **kwargs):
            return {"device_id": 9, "revoked": True}

        def refresh_device_token(self, **kwargs):
            raise AssertionError("refresh should not be needed in this test")

    cli.default_state_store = lambda: FakeStore()
    cli.default_http_client = lambda server: FakeClient()
    cli.default_config_manager = lambda: __import__("zhumeng_agent.adapters.codex.config_manager", fromlist=["CodexConfigManager"]).CodexConfigManager(codex_home)

    exit_code = main(["logout", "--revoke-device"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["status"] == "completed"
    assert not (codex_home / "auth.json").exists()
