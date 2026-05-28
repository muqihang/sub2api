import json

import pytest
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
            "codex_app_is_running",
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


class MemoryStore:
    def __init__(self, payload=None):
        self.payload = payload or {}
        self.updated = []

    def read(self):
        return dict(self.payload)

    def write(self, payload):
        self.payload = dict(payload)

    def update(self, patch):
        self.payload.update(patch)
        self.updated.append(dict(patch))
        return dict(self.payload)


def test_desktop_status_outputs_envelope_and_redacts_secrets(capsys):
    cli.default_state_store = lambda: MemoryStore({
        "status": "configured",
        "client": "codex",
        "access_token": "access-token-secret",
        "refresh_token": "refresh-token-secret",
        "loopback_secret": "loopback-secret-secret",
        "server_base_url": "https://example.com",
        "proxy_port": 18081,
        "user_email": "alice@example.com",
    })

    exit_code = main(["desktop", "status", "--json"])

    assert exit_code == 0
    payload = parse_output(capsys)
    assert payload["schema_version"] == 1
    assert payload["ok"] is True
    assert payload["command"] == "desktop status"
    assert payload["status"] == "configured"
    dumped = json.dumps(payload)
    assert "access-token-secret" not in dumped
    assert "refresh-token-secret" not in dumped
    assert "loopback-secret-secret" not in dumped
    assert "alice@example.com" not in dumped
    assert payload["data"]["state"]["access_token"] == "<redacted>"


def test_desktop_setup_returns_json_envelope_without_tokens(capsys, tmp_path: Path):
    class FakeClient:
        def exchange_setup_grant(self, **kwargs):
            return {
                "access_token": "access-token-secret",
                "refresh_token": "refresh-token-secret",
                "managed_session_id": "sess-1",
                "expires_at": "2026-05-11T12:00:00Z",
                "device_id": 9,
                "server_base_url": "https://example.com",
                "gateway_base_url": "https://example.com",
                "config_profile": {"model_provider": "zhumeng-codex"},
            }

        def list_codex_models(self, **kwargs):
            return {"models": [{"slug": "deepseek-v4-pro", "display_name": "DeepSeek V4 Pro"}]}

    store = MemoryStore()
    cli.default_http_client = lambda server: FakeClient()
    cli.default_state_store = lambda: store
    cli.default_config_manager = lambda: cli.CodexConfigManager(tmp_path / ".codex")
    cli.generate_loopback_secret = lambda: "loopback-secret-secret"
    cli.choose_local_proxy_port = lambda preferred=None: 18081
    cli.ensure_proxy_running = lambda store: 123

    exit_code = main([
        "desktop", "setup", "--client", "codex", "--code", "one-time-code", "--server", "https://example.com", "--json"
    ])

    assert exit_code == 0
    payload = parse_output(capsys)
    assert payload["ok"] is True
    assert payload["command"] == "desktop setup"
    assert payload["status"] == "configured"
    dumped = json.dumps(payload)
    assert "access-token-secret" not in dumped
    assert "refresh-token-secret" not in dumped
    assert "loopback-secret-secret" not in dumped
    assert "one-time-code" not in dumped
    assert payload["data"]["proxy_port"] == 18081
    assert store.payload["access_token"] == "access-token-secret"
    assert store.payload["config_hash_after"]
    assert store.payload["auth_hash_after"]
    assert store.payload["catalog_hash_after"]


def test_desktop_setup_marks_restart_required_when_codex_is_running(capsys, tmp_path: Path):
    class FakeClient:
        def exchange_setup_grant(self, **kwargs):
            return {
                "access_token": "access-token-secret",
                "refresh_token": "refresh-token-secret",
                "managed_session_id": "sess-1",
                "expires_at": "2026-05-11T12:00:00Z",
                "device_id": 9,
                "server_base_url": "https://example.com",
                "gateway_base_url": "https://example.com",
                "config_profile": {"model_provider": "zhumeng-codex"},
            }

        def list_codex_models(self, **kwargs):
            return {"models": [{"slug": "deepseek-v4-pro", "display_name": "DeepSeek V4 Pro"}]}

    manager = cli.CodexConfigManager(tmp_path / ".codex")
    manager.config_path.parent.mkdir(parents=True, exist_ok=True)
    manager.config_path.write_text("model_provider = \"old\"\n", encoding="utf-8")
    store = MemoryStore()
    cli.default_http_client = lambda server: FakeClient()
    cli.default_state_store = lambda: store
    cli.default_config_manager = lambda: manager
    cli.generate_loopback_secret = lambda: "loopback-secret-secret"
    cli.choose_local_proxy_port = lambda preferred=None: 18081
    cli.ensure_proxy_running = lambda store: 123
    cli.default_codex_app_path = lambda: Path("/Applications/Codex.app")
    cli.codex_app_is_running = lambda app_path: True

    exit_code = main([
        "desktop", "setup", "--client", "codex", "--code", "one-time-code", "--server", "https://example.com", "--json"
    ])

    assert exit_code == 0
    payload = parse_output(capsys)
    assert payload["data"]["adapters"]["codex"]["restart_required"] is True
    assert store.payload["restart_required"] is True


def test_desktop_setup_failure_is_json_without_traceback(capsys):
    class FakeClient:
        def exchange_setup_grant(self, **kwargs):
            raise RuntimeError("backend unavailable access-token-secret")

    cli.default_http_client = lambda server: FakeClient()

    exit_code = main([
        "desktop", "setup", "--client", "codex", "--code", "secret-code", "--server", "https://example.com", "--json"
    ])

    assert exit_code == 1
    payload = parse_output(capsys)
    assert payload["ok"] is False
    assert payload["command"] == "desktop setup"
    assert payload["status"] == "error"
    dumped = json.dumps(payload)
    assert "Traceback" not in dumped
    assert "secret-code" not in dumped
    assert "access-token-secret" not in dumped


def test_desktop_open_codex_uses_adapter_launch(capsys):
    cli.default_codex_app_path = lambda: Path("/Applications/Codex.app")
    cli.build_codex_launch_command = lambda app_path, cdp_port: ["open", str(app_path)]
    cli.select_cdp_port = lambda: 9222
    launched = {}
    cli.launch_codex_process = lambda command: launched.setdefault("command", command)

    exit_code = main(["desktop", "open", "--app", "codex", "--json"])

    assert exit_code == 0
    payload = parse_output(capsys)
    assert payload["command"] == "desktop open"
    assert payload["status"] == "launched"
    assert launched["command"] == ["open", "/Applications/Codex.app"]


def test_desktop_codex_enhancements_status_envelope(capsys, tmp_path: Path):
    app = tmp_path / "Codex.app"
    cli.inspect_codex_enhancements = lambda app_path: {
        "status": "ok",
        "app_path": str(app_path),
        "items": {"model-picker": {"status": "patched"}},
    }

    exit_code = main(["desktop", "codex-enhancements", "status", "--app", str(app), "--json"])

    assert exit_code == 0
    payload = parse_output(capsys)
    assert payload["ok"] is True
    assert payload["command"] == "desktop codex-enhancements status"
    assert payload["data"]["items"]["model-picker"]["status"] == "patched"


def test_desktop_codex_enhancements_patch_refuses_running_app(capsys, tmp_path: Path):
    app = tmp_path / "Codex.app"
    cli.default_state_store = lambda: MemoryStore()
    cli.patch_codex_enhancements = lambda app_path, item="all": {
        "status": "app_running_blocking_change",
        "app_path": str(app_path),
        "message": "Codex App is running",
    }

    exit_code = main(["desktop", "codex-enhancements", "patch", "--app", str(app), "--item", "all", "--json"])

    assert exit_code == 1
    payload = parse_output(capsys)
    assert payload["ok"] is False
    assert payload["status"] == "app_running_blocking_change"
    assert payload["error"]["code"] == "app_running_blocking_change"


def test_desktop_reauth_preserves_restore_baseline_and_proxy(capsys, tmp_path: Path):
    class FakeClient:
        def exchange_setup_grant(self, **kwargs):
            return {
                "access_token": "fresh-access-token",
                "refresh_token": "fresh-refresh-token",
                "managed_session_id": "sess-fresh",
                "device_id": 9,
                "server_base_url": "https://example.com",
                "gateway_base_url": "https://example.com",
                "config_profile": {"model_provider": "zhumeng-codex"},
            }
        def list_codex_models(self, **kwargs):
            return {"models": []}

    store = MemoryStore({
        "status": "configured",
        "client": "codex",
        "proxy_port": 18081,
        "loopback_secret": "existing-loopback-secret",
        "prior_auth_json": "original-auth",
        "prior_catalog_json": "original-catalog",
        "catalog_preexisting": True,
        "config_profile": {"model_provider": "zhumeng-codex"},
    })
    cli.default_state_store = lambda: store
    cli.default_http_client = lambda server: FakeClient()
    cli.default_config_manager = lambda: cli.CodexConfigManager(tmp_path / ".codex")
    cli.ensure_proxy_running = lambda store: 123

    exit_code = main(["desktop", "reauth", "--client", "codex", "--code", "new-code", "--server", "https://example.com", "--json"])

    assert exit_code == 0
    payload = parse_output(capsys)
    assert payload["status"] == "reauthorized"
    assert store.payload["proxy_port"] == 18081
    assert store.payload["loopback_secret"] == "existing-loopback-secret"
    assert store.payload["prior_auth_json"] == "original-auth"
    assert store.payload["prior_catalog_json"] == "original-catalog"


def test_desktop_reauth_marks_restart_required_when_codex_is_running(capsys, tmp_path: Path):
    class FakeClient:
        def exchange_setup_grant(self, **kwargs):
            return {
                "access_token": "fresh-access-token",
                "refresh_token": "fresh-refresh-token",
                "managed_session_id": "sess-fresh",
                "device_id": 9,
                "server_base_url": "https://example.com",
                "gateway_base_url": "https://example.com",
                "config_profile": {"model_provider": "zhumeng-codex"},
            }

        def list_codex_models(self, **kwargs):
            return {"models": []}

    manager = cli.CodexConfigManager(tmp_path / ".codex")
    manager.config_path.parent.mkdir(parents=True, exist_ok=True)
    manager.config_path.write_text("model_provider = \"old\"\n", encoding="utf-8")
    store = MemoryStore({
        "status": "configured",
        "client": "codex",
        "proxy_port": 18081,
        "loopback_secret": "existing-loopback-secret",
        "config_profile": {"model_provider": "zhumeng-codex"},
    })
    cli.default_state_store = lambda: store
    cli.default_http_client = lambda server: FakeClient()
    cli.default_config_manager = lambda: manager
    cli.ensure_proxy_running = lambda store: 123
    cli.default_codex_app_path = lambda: Path("/Applications/Codex.app")
    cli.codex_app_is_running = lambda app_path: True

    exit_code = main(["desktop", "reauth", "--client", "codex", "--code", "new-code", "--server", "https://example.com", "--json"])

    assert exit_code == 0
    payload = parse_output(capsys)
    assert payload["data"]["adapters"]["codex"]["restart_required"] is True
    assert store.payload["restart_required"] is True


def test_desktop_invalid_args_stdout_json_and_no_stderr(capsys):
    exit_code = main(["desktop", "setup", "--client", "codex", "--code", "secret-code", "--server"])

    captured = capsys.readouterr()
    assert exit_code == 1
    assert captured.err == ""
    payload = json.loads(captured.out)
    assert payload["ok"] is False
    assert payload["error"]["code"] == "invalid_arguments"
    assert "secret-code" not in captured.out


def test_desktop_status_keeps_authorization_object(capsys):
    cli.default_state_store = lambda: MemoryStore({
        "status": "configured",
        "client": "codex",
        "device_id": 9,
        "managed_session_id": "managed-session-id-abcdef123456",
        "proxy_port": 18081,
    })

    exit_code = main(["desktop", "status", "--json"])

    assert exit_code == 0
    payload = parse_output(capsys)
    assert isinstance(payload["data"]["authorization"], dict)
    assert payload["data"]["authorization"]["managed_session_id_redacted"] == "...3456"
    assert "managed-session-id-abcdef123456" not in json.dumps(payload)


def test_desktop_repair_enhancement_failure_is_not_ok(capsys):
    class FakeManager:
        def repair(self, *args, **kwargs):
            return None
        def read_existing_model_catalog(self, *args, **kwargs):
            return {"models": []}

    store = MemoryStore({
        "status": "configured",
        "client": "codex",
        "proxy_port": 18081,
        "loopback_secret": "loopback-secret",
        "config_profile": {"model_provider": "zhumeng-codex"},
    })
    cli.default_state_store = lambda: store
    cli.default_config_manager = lambda: FakeManager()
    cli.ensure_proxy_running = lambda store: 123
    cli.default_codex_app_path = lambda: Path("/Applications/Codex.app")
    cli.patch_codex_enhancements = lambda app_path, item="all": {"status": "app_running_blocking_change", "restart_required": False}

    exit_code = main(["desktop", "repair", "--client", "codex", "--json"])

    payload = parse_output(capsys)
    assert exit_code == 1
    assert payload["ok"] is False
    assert payload["status"] == "app_running_blocking_change"

def test_desktop_models_status_summarizes_local_catalog(capsys, tmp_path: Path):
    manager = cli.CodexConfigManager(tmp_path / ".codex")
    manager.write_model_catalog({"model_provider": "zhumeng-codex"}, {
        "models": [
            {
                "slug": "main-model",
                "display_name": "Main Model",
                "visibility": "list",
                "supported_in_api": True,
                "origin": "zhumeng",
                "provider_id": "zhumeng",
                "capabilities": {"responses": True, "streaming": True, "tool_calls": True, "context_continuation": True},
                "pricing": {"input_price": "1.00", "output_price": "2.00", "currency": "USD", "unit": "per_1m_tokens", "source": "database_model_pricing"},
            },
            {
                "slug": "incompatible-model",
                "display_name": "Incompatible Model",
                "visibility": "list",
                "supported_in_api": True,
                "origin": "zhumeng",
                "provider_id": "zhumeng",
                "capabilities": {"responses": True, "streaming": True, "tool_calls": False, "context_continuation": True},
                "pricing": None,
            },
            {
                "slug": "restricted-model",
                "display_name": "Restricted Model",
                "visibility": "hide",
                "supported_in_api": False,
                "origin": "zhumeng",
                "provider_id": "zhumeng",
                "capabilities": {"responses": True, "streaming": True, "tool_calls": True, "context_continuation": True},
                "pricing": None,
            },
        ]
    })
    cli.default_config_manager = lambda: manager
    cli.default_state_store = lambda: MemoryStore({
        "status": "configured",
        "client": "codex",
        "config_profile": {"model_provider": "zhumeng-codex"},
        "model_catalog_synced_at": "2026-05-21T00:00:00Z",
    })

    exit_code = main(["desktop", "models", "status", "--client", "codex", "--json"])

    assert exit_code == 0
    payload = parse_output(capsys)
    assert payload["ok"] is True
    assert payload["command"] == "desktop models status"
    assert payload["data"]["model_count"] == 3
    assert payload["data"]["main_list_count"] == 1
    assert payload["data"]["restricted_count"] == 1
    assert payload["data"]["incompatible_count"] == 1
    assert payload["data"]["missing_pricing_count"] == 2
    assert payload["data"]["last_synced_at"] == "2026-05-21T00:00:00Z"
    assert [model["slug"] for model in payload["data"]["models"]] == ["main-model", "incompatible-model", "restricted-model"]
    assert payload["data"]["models"][0]["pricing"]["source"] == "database_model_pricing"


def test_desktop_models_sync_fetches_and_writes_catalog(capsys, tmp_path: Path):
    class FakeClient:
        def list_codex_models(self, **kwargs):
            return {"models": [{
                "slug": "gpt-5.5",
                "display_name": "GPT-5.5",
                "origin": "zhumeng",
                "provider_id": "zhumeng",
                "capabilities": {"responses": True, "streaming": True, "tool_calls": True, "context_continuation": True},
                "pricing": {"input_price": "2.50", "output_price": "15.00", "currency": "USD", "unit": "per_1m_tokens", "source": "database_model_pricing"},
            }]}

    manager = cli.CodexConfigManager(tmp_path / ".codex")
    store = MemoryStore({
        "status": "configured",
        "client": "codex",
        "gateway_base_url": "https://gateway.example.com",
        "server_base_url": "https://example.com",
        "access_token": "access-token-secret",
        "managed_session_id": "sess-1",
        "device_id": 9,
        "config_profile": {"model_provider": "zhumeng-codex"},
    })
    cli.default_config_manager = lambda: manager
    cli.default_state_store = lambda: store
    cli.default_http_client = lambda server: FakeClient()

    exit_code = main(["desktop", "models", "sync", "--client", "codex", "--json"])

    assert exit_code == 0
    payload = parse_output(capsys)
    assert payload["ok"] is True
    assert payload["command"] == "desktop models sync"
    assert payload["data"]["model_count"] == 1
    saved = json.loads((tmp_path / ".codex" / "zhumeng-codex-models.json").read_text(encoding="utf-8"))
    assert saved["models"][0]["capabilities"]["tool_calls"] is True
    assert saved["models"][0]["pricing"]["source"] == "database_model_pricing"
    assert store.payload["model_catalog_synced_at"]
