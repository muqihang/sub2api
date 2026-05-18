import json
from pathlib import Path

import pytest
from aiohttp.test_utils import TestClient, TestServer

import zhumeng_agent.cli as cli
from zhumeng_agent.adapters.codex.model_picker import ModelPickerPatchError
from zhumeng_agent.cli import main

ORIGINAL_DEFAULT_CAPTURE_CONFIG = cli.default_capture_config
ORIGINAL_GENERATE_CAPTURE_REPORT = cli.generate_capture_report
ORIGINAL_ENSURE_CAPTURE_RECEIVER_RUNNING = cli.ensure_capture_receiver_running
ORIGINAL_ENSURE_CAPTURE_BRIDGE_RUNNING = cli.ensure_capture_bridge_running
ORIGINAL_ENSURE_PROXY_RUNNING = cli.ensure_proxy_running


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

        def list_codex_models(self, **kwargs):
            return {
                "models": [
                    {
                        "slug": "custom-web-model",
                        "display_name": "Custom Web Model",
                        "visibility": "visible",
                        "supported_reasoning_levels": ["high"],
                    }
                ]
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
    catalog = json.loads((tmp_path / ".codex" / "zhumeng-codex-models.json").read_text(encoding="utf-8"))
    assert catalog["models"][0]["slug"] == "custom-web-model"


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

        def list_codex_models(self, **kwargs):
            return {"models": [{"slug": "deepseek-v4-pro", "display_name": "DeepSeek V4 Pro"}]}

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


def test_deeplink_invocation_uses_sys_argv_when_run_as_module(capsys, tmp_path: Path, monkeypatch):
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

        def list_codex_models(self, **kwargs):
            return {"models": [{"slug": "deepseek-v4-pro", "display_name": "DeepSeek V4 Pro"}]}

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
    monkeypatch.setattr(cli.sys, "argv", [
        "zhumeng-agent",
        "zhumeng-agent://setup?client=codex&code=abc&server=https%3A%2F%2Fexample.com",
    ])

    exit_code = main()

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


def test_fetch_codex_model_catalog_falls_back_on_non_auth_errors():
    class FakeClient:
        def list_codex_models(self, **kwargs):
            class Response:
                status_code = 404
            err = RuntimeError("not found")
            err.response = Response()
            raise err

    class FakeManager:
        def read_existing_model_catalog(self, profile):
            return {"models": [{"slug": "fallback-model"}]}

    catalog, meta = cli.fetch_codex_model_catalog(FakeClient(), FakeManager(), {
        "gateway_base_url": "https://example.com",
        "access_token": "access-token",
        "managed_session_id": "sess-1",
        "device_id": 9,
        "config_profile": {"model_provider": "zhumeng-codex"},
    })

    assert catalog["models"][0]["slug"] == "fallback-model"
    assert meta["source"] == "existing"
    assert meta["reason"] == "upstream_fetch_failed"
    assert meta["status_code"] == 404


def test_fetch_codex_model_catalog_refreshes_managed_token_after_unauthorized():
    class FakeClient:
        def __init__(self):
            self.calls = 0

        def list_codex_models(self, **kwargs):
            self.calls += 1
            if self.calls == 1:
                class Response:
                    status_code = 401
                err = RuntimeError("unauthorized")
                err.response = Response()
                raise err
            assert kwargs["access_token"] == "fresh-access-token"
            assert kwargs["managed_session_id"] == "sess-2"
            return {
                "models": [
                    {
                        "slug": "gpt-5.5",
                        "display_name": "GPT-5.5",
                        "visibility": "visible",
                    }
                ]
            }

        def refresh_device_token(self, **kwargs):
            assert kwargs["device_id"] == 9
            assert kwargs["refresh_token"] == "refresh-token"
            return {
                "access_token": "fresh-access-token",
                "refresh_token": "refresh-token-2",
                "managed_session_id": "sess-2",
            }

    class FakeManager:
        def read_existing_model_catalog(self, profile):
            return {"models": [{"slug": "fallback-model"}]}

        def build_model_catalog(self, payload):
            return payload

    class FakeStore:
        def __init__(self):
            self.patch = None

        def update(self, patch):
            self.patch = patch
            return patch

    state = {
        "gateway_base_url": "https://example.com",
        "access_token": "stale-access-token",
        "refresh_token": "refresh-token",
        "managed_session_id": "sess-1",
        "device_id": 9,
        "config_profile": {"model_provider": "zhumeng-codex"},
    }
    store = FakeStore()

    catalog, meta = cli.fetch_codex_model_catalog(FakeClient(), FakeManager(), state, store)

    assert catalog["models"][0]["slug"] == "gpt-5.5"
    assert meta["source"] == "gateway"
    assert meta["refreshed"] is True
    assert state["access_token"] == "fresh-access-token"
    assert state["refresh_token"] == "refresh-token-2"
    assert state["managed_session_id"] == "sess-2"
    assert store.patch == {
        "access_token": "fresh-access-token",
        "refresh_token": "refresh-token-2",
        "managed_session_id": "sess-2",
        "status": "configured",
    }


def test_repair_requires_configured_state(capsys):
    cli.default_state_store = lambda: type("Store", (), {"read": lambda self: {}})()
    exit_code = main(["repair", "codex"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "repair"
    assert data["status"] == "not_configured"


def test_repair_codex_patches_desktop_when_app_is_detected(capsys):
    class FakeManager:
        def repair(self, *args, **kwargs):
            return None

        def read_existing_model_catalog(self, *args, **kwargs):
            return {"models": []}

    class FakeStore:
        def read(self):
            return {
                "proxy_port": 18081,
                "config_profile": {"model_provider": "zhumeng-managed"},
                "loopback_secret": "loopback-secret",
            }

        def update(self, patch):
            return patch

    cli.default_state_store = lambda: FakeStore()
    cli.default_config_manager = lambda: FakeManager()
    cli.ensure_proxy_running = lambda store: 9999
    cli.detect_codex_app_path = lambda **kwargs: Path("/Applications/Codex.app")
    cli.patch_model_picker_app = lambda app_path: {"status": "patched", "app_path": str(app_path)}
    cli.patch_plugin_auth_gate_app = lambda app_path: {"status": "already_patched", "app_path": str(app_path)}
    cli.patch_plugin_mention_marketplace_app = lambda app_path: {"status": "patched", "app_path": str(app_path)}

    exit_code = main(["repair", "codex"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "repair"
    assert data["status"] == "repaired"
    assert data["model_picker"]["status"] == "patched"
    assert data["plugin_auth_gate"]["status"] == "already_patched"
    assert data["plugin_mention_marketplace"]["status"] == "patched"


def test_repair_codex_does_not_require_desktop_app_for_model_picker(capsys):
    class FakeManager:
        def repair(self, *args, **kwargs):
            return None

        def read_existing_model_catalog(self, *args, **kwargs):
            return {"models": []}

    class FakeStore:
        def read(self):
            return {
                "proxy_port": 18081,
                "config_profile": {"model_provider": "zhumeng-managed"},
                "loopback_secret": "loopback-secret",
            }

        def update(self, patch):
            return patch

    cli.default_state_store = lambda: FakeStore()
    cli.default_config_manager = lambda: FakeManager()
    cli.ensure_proxy_running = lambda store: 9999
    cli.detect_codex_app_path = lambda **kwargs: None

    exit_code = main(["repair", "codex"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "repair"
    assert data["status"] == "repaired"
    assert data["model_picker"]["status"] == "app_not_found"
    assert data["plugin_auth_gate"]["status"] == "app_not_found"
    assert data["plugin_mention_marketplace"]["status"] == "app_not_found"


def test_ensure_proxy_running_reuses_existing_loopback_listener(monkeypatch: pytest.MonkeyPatch):
    class FakeStore:
        path = Path("/tmp/state.json")

        def __init__(self):
            self.updated = None

        def read(self):
            return {
                "gateway_base_url": "https://example.com",
                "device_id": 9,
                "managed_session_id": "sess-1",
                "access_token": "access-token",
                "loopback_secret": "loopback-secret",
                "proxy_port": 18081,
                "proxy_pid": 999999,
            }

        def update(self, patch):
            self.updated = patch
            return patch

    store = FakeStore()
    monkeypatch.setattr(cli, "ensure_proxy_running", ORIGINAL_ENSURE_PROXY_RUNNING)
    monkeypatch.setattr(cli, "is_process_alive", lambda pid: False)
    monkeypatch.setattr(cli, "is_loopback_port_accepting_connections", lambda port: port == 18081)
    monkeypatch.setattr(cli, "proxy_matches_current_runtime", lambda port: True)
    monkeypatch.setattr(cli.subprocess, "Popen", lambda *args, **kwargs: (_ for _ in ()).throw(AssertionError("proxy already running")))

    assert cli.ensure_proxy_running(store) == 0
    assert store.updated is None


def test_ensure_proxy_running_starts_detached_process(monkeypatch: pytest.MonkeyPatch):
    class FakeStore:
        path = Path("/tmp/state.json")

        def read(self):
            return {
                "gateway_base_url": "https://example.com",
                "device_id": 9,
                "managed_session_id": "sess-1",
                "access_token": "access-token",
                "loopback_secret": "loopback-secret",
                "proxy_port": 18081,
                "proxy_pid": 0,
            }

        def update(self, patch):
            self.patch = patch
            return patch

    captured = {}

    class FakeProcess:
        pid = 12345

    def fake_popen(*args, **kwargs):
        captured["args"] = args
        captured["kwargs"] = kwargs
        return FakeProcess()

    store = FakeStore()
    monkeypatch.setattr(cli, "ensure_proxy_running", ORIGINAL_ENSURE_PROXY_RUNNING)
    monkeypatch.setattr(cli, "is_process_alive", lambda pid: False)
    monkeypatch.setattr(cli, "is_loopback_port_accepting_connections", lambda port: False)
    monkeypatch.setattr(cli, "proxy_matches_current_runtime", lambda port: False)
    monkeypatch.setattr(cli.subprocess, "Popen", fake_popen)

    assert cli.ensure_proxy_running(store) == 12345
    assert captured["kwargs"]["start_new_session"] is True
    assert captured["kwargs"]["stdout"] == cli.subprocess.DEVNULL
    assert captured["kwargs"]["stderr"] == cli.subprocess.DEVNULL
    assert store.patch["proxy_pid"] == 12345


def test_ensure_proxy_running_restarts_when_listener_is_old_runtime(monkeypatch: pytest.MonkeyPatch):
    class FakeStore:
        path = Path("/tmp/state.json")

        def read(self):
            return {
                "gateway_base_url": "https://example.com",
                "device_id": 9,
                "managed_session_id": "sess-1",
                "access_token": "access-token",
                "loopback_secret": "loopback-secret",
                "proxy_port": 18081,
                "proxy_pid": 999999,
            }

        def update(self, patch):
            self.patch = patch
            return patch

    class FakeProcess:
        pid = 23456

    store = FakeStore()
    monkeypatch.setattr(cli, "ensure_proxy_running", ORIGINAL_ENSURE_PROXY_RUNNING)
    monkeypatch.setattr(cli, "is_process_alive", lambda pid: True)
    monkeypatch.setattr(cli, "is_loopback_port_accepting_connections", lambda port: True)
    monkeypatch.setattr(cli, "proxy_matches_current_runtime", lambda port: False)
    monkeypatch.setattr(cli.subprocess, "Popen", lambda *args, **kwargs: FakeProcess())

    assert cli.ensure_proxy_running(store) == 23456
    assert store.patch["proxy_pid"] == 23456


def test_ensure_proxy_running_terminates_stale_listener_before_restart(monkeypatch: pytest.MonkeyPatch):
    class FakeStore:
        path = Path("/tmp/state.json")

        def read(self):
            return {
                "gateway_base_url": "https://example.com",
                "device_id": 9,
                "managed_session_id": "sess-1",
                "access_token": "access-token",
                "loopback_secret": "loopback-secret",
                "proxy_port": 18081,
                "proxy_pid": 999999,
            }

        def update(self, patch):
            self.patch = patch
            return patch

    class FakeProcess:
        pid = 34567

    killed: list[tuple[int, int]] = []

    store = FakeStore()
    monkeypatch.setattr(cli, "ensure_proxy_running", ORIGINAL_ENSURE_PROXY_RUNNING)
    monkeypatch.setattr(cli, "is_process_alive", lambda pid: True)
    monkeypatch.setattr(cli, "is_loopback_port_accepting_connections", lambda port: True)
    monkeypatch.setattr(cli, "proxy_matches_current_runtime", lambda port: False)
    monkeypatch.setattr(cli.os, "kill", lambda pid, sig: killed.append((pid, sig)))
    monkeypatch.setattr(cli.subprocess, "Popen", lambda *args, **kwargs: FakeProcess())

    assert cli.ensure_proxy_running(store) == 34567
    assert killed == [(999999, cli.signal.SIGTERM)]
    assert store.patch["proxy_pid"] == 34567


def test_proxy_matches_current_runtime_rejects_stale_runtime_signature(monkeypatch: pytest.MonkeyPatch):
    class FakeResponse:
        def __enter__(self):
            return self

        def __exit__(self, exc_type, exc, tb):
            return False

        def read(self):
            return json.dumps({
                "ok": True,
                "agent_version": cli.AGENT_VERSION,
                "source_root": cli.AGENT_SOURCE_ROOT,
                "runtime_signature": "stale-signature",
            }).encode("utf-8")

    monkeypatch.setattr(
        cli.urllib.request,
        "urlopen",
        lambda *args, **kwargs: FakeResponse(),
    )

    assert cli.proxy_matches_current_runtime(18081) is False


def test_launch_codex_patches_desktop_before_launch(capsys):
    class FakeManager:
        def repair(self, *args, **kwargs):
            return None

    class FakeStore:
        def read(self):
            return {
                "proxy_port": 18081,
                "config_profile": {"model_provider": "zhumeng-managed"},
                "loopback_secret": "loopback-secret",
            }

        def update(self, patch):
            return patch

    cli.default_state_store = lambda: FakeStore()
    cli.default_config_manager = lambda: FakeManager()
    cli.ensure_proxy_running = lambda store: 9999
    cli.detect_codex_app_path = lambda **kwargs: Path("/Applications/Codex.app")
    cli.patch_model_picker_app = lambda app_path: {"status": "patched", "app_path": str(app_path)}
    cli.patch_plugin_auth_gate_app = lambda app_path: {"status": "already_patched", "app_path": str(app_path)}
    cli.patch_plugin_mention_marketplace_app = lambda app_path: {"status": "patched", "app_path": str(app_path)}
    launched = {}
    cli.launch_codex_process = lambda command: launched.setdefault("command", command)

    exit_code = main(["launch", "codex"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "launch"
    assert data["status"] == "launched"
    assert data["model_picker"]["status"] == "patched"
    assert data["plugin_auth_gate"]["status"] == "already_patched"
    assert data["plugin_mention_marketplace"]["status"] == "patched"
    assert launched["command"]


@pytest.mark.asyncio
async def test_capture_receiver_accepts_cors_preflight_for_desktop_renderer(tmp_path: Path):
    app = cli.create_capture_receiver_app(tmp_path, cli.default_capture_config())
    server = TestServer(app)
    await server.start_server()
    client = TestClient(server)
    await client.start_server()
    try:
        response = await client.options(
            "/codex-desktop-capture-v2",
            headers={
                "Origin": "app://-",
                "Access-Control-Request-Method": "POST",
                "Access-Control-Request-Headers": "content-type",
            },
        )

        assert response.status == 204
        assert response.headers["Access-Control-Allow-Origin"] == "app://-"
        assert "POST" in response.headers["Access-Control-Allow-Methods"]
        assert "content-type" in response.headers["Access-Control-Allow-Headers"].lower()
    finally:
        await client.close()
        await server.close()


@pytest.mark.asyncio
async def test_capture_receiver_posts_with_desktop_origin_and_writes_shape_only_trace(tmp_path: Path):
    key = tmp_path / "correlation.key"
    key.write_bytes(b"shared")
    config = cli.CodexDesktopCaptureConfig.defaults(correlation_hash_key_file=key)
    app = cli.create_capture_receiver_app(tmp_path, config)
    server = TestServer(app)
    await server.start_server()
    client = TestClient(server)
    await client.start_server()
    try:
        response = await client.post(
            "/codex-desktop-capture-v2",
            json={
                "type": "app_server_frame",
                "direction": "desktop_to_app_server",
                "frame_text": '{"id":1,"method":"model/list","params":{"x-client-request-id":"request-1"}}',
            },
            headers={"Origin": "app://-"},
        )

        assert response.status == 200
        assert response.headers["Access-Control-Allow-Origin"] == "app://-"
        dumped = (tmp_path / "app_server_v2.jsonl").read_text(encoding="utf-8")
        assert "request-1" not in dumped
        assert "x_client_request_id_hash" in dumped
    finally:
        await client.close()
        await server.close()


@pytest.mark.asyncio
async def test_capture_receiver_accepts_text_plain_beacon_payload(tmp_path: Path):
    app = cli.create_capture_receiver_app(tmp_path, cli.default_capture_config())
    server = TestServer(app)
    await server.start_server()
    client = TestClient(server)
    await client.start_server()
    try:
        response = await client.post(
            "/codex-desktop-capture-v2",
            data='{"type":"model_picker","selected_model":"beacon-smoke"}',
            headers={
                "Origin": "app://-",
                "Content-Type": "text/plain;charset=UTF-8",
            },
        )

        assert response.status == 200
        assert response.headers["Access-Control-Allow-Origin"] == "app://-"
        dumped = (tmp_path / "model_picker.jsonl").read_text(encoding="utf-8")
        assert "beacon-smoke" in dumped
    finally:
        await client.close()
        await server.close()


@pytest.mark.asyncio
async def test_capture_receiver_accepts_websocket_events(tmp_path: Path):
    app = cli.create_capture_receiver_app(tmp_path, cli.default_capture_config())
    server = TestServer(app)
    await server.start_server()
    client = TestClient(server)
    await client.start_server()
    try:
        ws = await client.ws_connect("/codex-desktop-capture-v2/ws", headers={"Origin": "app://-"})
        await ws.send_str('{"type":"model_picker","selected_model":"websocket-smoke"}')
        await ws.close()

        dumped = (tmp_path / "model_picker.jsonl").read_text(encoding="utf-8")
        assert "websocket-smoke" in dumped
    finally:
        await client.close()
        await server.close()


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


def test_codex_capture_status_reports_config(capsys, tmp_path: Path):
    cli.default_state_store = lambda: type("Store", (), {"read": lambda self: {"desktop_capture_enabled": True}})()
    cli.default_capture_config = ORIGINAL_DEFAULT_CAPTURE_CONFIG
    cli.capture_install_manifest_state = lambda config: {"status": "installed"}

    exit_code = main(["codex", "capture", "status"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "codex capture status"
    assert data["config"]["enabled"] is True
    assert data["installation"]["status"] == "installed"
    assert data["config"]["raw_payloads"] is False


def test_codex_capture_baseline_delegates_to_baseline_generator(capsys, tmp_path: Path):
    cli.default_capture_config = lambda *args: __import__("zhumeng_agent.adapters.codex.capture_config", fromlist=["CodexDesktopCaptureConfig"]).CodexDesktopCaptureConfig.defaults(base_dir=tmp_path / "captures", correlation_hash_key_file=args[0] if args else None)
    cli.default_codex_app_path = lambda: tmp_path / "Codex.app"
    cli.generate_capture_baseline = lambda out_dir, app_path, config: {"status": "baseline_created", "out_dir": str(out_dir), "desktop_app_path_hash": "hmac-sha256:x"}

    exit_code = main(["codex", "capture", "baseline", "--out", str(tmp_path / "out")])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "codex capture baseline"
    assert data["status"] == "baseline_created"
    assert "/Codex.app" not in json.dumps(data)


def test_codex_capture_install_and_uninstall_do_not_patch_model_picker(capsys, tmp_path: Path):
    cli.default_capture_config = lambda *args: __import__("zhumeng_agent.adapters.codex.capture_config", fromlist=["CodexDesktopCaptureConfig"]).CodexDesktopCaptureConfig.defaults(base_dir=tmp_path / "captures", correlation_hash_key_file=args[0] if args else None)
    cli.install_capture_hook = lambda app_path, config: {"status": "installed", "app_asar_modified": False, "hook_mode": "renderer_readonly"}
    cli.uninstall_capture_hook = lambda app_path, config: {"status": "uninstalled", "app_asar_modified": False}
    cli.patch_model_picker_app = lambda app_path: (_ for _ in ()).throw(AssertionError("capture install must not patch model picker"))
    updates = {}
    cli.default_state_store = lambda: type(
        "Store",
        (),
        {
            "read": lambda self: {},
            "update": lambda self, patch: updates.update(patch) or patch,
        },
    )()

    assert main(["codex", "capture", "install", "--app", str(tmp_path / "Codex.app")]) == 0
    install_data = parse_output(capsys)
    assert install_data["status"] == "installed"
    assert install_data["app_asar_modified"] is False
    assert updates["desktop_capture_enabled"] is True

    assert main(["codex", "capture", "uninstall", "--app", str(tmp_path / "Codex.app")]) == 0
    uninstall_data = parse_output(capsys)
    assert uninstall_data["status"] == "uninstalled"
    assert updates["desktop_capture_enabled"] is False


def test_codex_capture_report_reads_trace_dir(capsys, tmp_path: Path):
    cli.generate_capture_report = lambda trace_dir, config=None, gateway_trace_dir=None: {"status": "reported", "trace_dir_hash": "hmac-sha256:x", "app_server_methods": ["model/list"]}

    exit_code = main(["codex", "capture", "report", "--trace-dir", str(tmp_path / "traces")])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "codex capture report"
    assert data["status"] == "reported"
    assert "model/list" in data["app_server_methods"]


def test_codex_capture_attach_uses_cdp_binding_bridge(capsys, tmp_path: Path):
    seen = {}

    def fake_attach(cdp_port, trace_dir, config, *, capture_port, timeout_seconds, target_wait_seconds, once):
        seen["cdp_port"] = cdp_port
        seen["trace_dir"] = trace_dir
        seen["capture_port"] = capture_port
        seen["timeout_seconds"] = timeout_seconds
        seen["target_wait_seconds"] = target_wait_seconds
        seen["once"] = once
        return {
            "status": "attached",
            "bridge": "cdp_binding",
            "targets_attached": 2,
            "events_written": 1,
        }

    cli.attach_capture_bridge_via_cdp = fake_attach

    exit_code = main([
        "codex", "capture", "attach",
        "--cdp-port", "65031",
        "--trace-dir", str(tmp_path / "traces"),
        "--capture-port", "65030",
        "--timeout-seconds", "1.5",
        "--once",
    ])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "codex capture attach"
    assert data["bridge"] == "cdp_binding"
    assert data["targets_attached"] == 2
    assert seen["cdp_port"] == 65031
    assert seen["trace_dir"] == tmp_path / "traces"
    assert seen["capture_port"] == 65030
    assert seen["timeout_seconds"] == 1.5
    assert seen["target_wait_seconds"] == 10
    assert seen["once"] is True


def test_codex_capture_report_flags_serialized_sensitive_content(capsys, tmp_path: Path):
    cli.default_capture_config = ORIGINAL_DEFAULT_CAPTURE_CONFIG
    cli.generate_capture_report = ORIGINAL_GENERATE_CAPTURE_REPORT
    trace_dir = tmp_path / "traces"
    trace_dir.mkdir()
    (trace_dir / "app_server_v2.jsonl").write_text('{"payload_shape":{"/Users/alice/secret.py":"str"}}\n', encoding="utf-8")

    exit_code = main(["codex", "capture", "report", "--trace-dir", str(trace_dir)])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["content_policy_violations"] == 1


def test_codex_capture_report_flags_bearer_and_cookie(capsys, tmp_path: Path):
    cli.default_capture_config = ORIGINAL_DEFAULT_CAPTURE_CONFIG
    cli.generate_capture_report = ORIGINAL_GENERATE_CAPTURE_REPORT
    trace_dir = tmp_path / "traces"
    trace_dir.mkdir()
    (trace_dir / "app_server_v2.jsonl").write_text(
        '{"headers":{"Authorization":"Bearer sk-test","Cookie":"abc"}}\n',
        encoding="utf-8",
    )

    exit_code = main(["codex", "capture", "report", "--trace-dir", str(trace_dir)])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["content_policy_violations"] == 1


def test_codex_capture_report_generates_trace_links(capsys, tmp_path: Path):
    trace_dir = tmp_path / "traces"
    trace_dir.mkdir()
    shared = {"x_client_request_id_hash": "hmac-sha256:abc"}
    (trace_dir / "app_server_v2.jsonl").write_text(json.dumps({
        "desktop_trace_id": "cd_1",
        "ts": "2026-05-14T00:00:00.000Z",
        "model": "deepseek-v4-pro",
        "request_path": "/codex/v1/responses",
        "correlation_hashes": shared,
    }) + "\n", encoding="utf-8")
    (trace_dir / "gateway_trace.jsonl").write_text(json.dumps({
        "gateway_trace_id": "trace_1",
        "ts": "2026-05-14T00:00:00.010Z",
        "model": "deepseek-v4-pro",
        "request_path": "/codex/v1/responses",
        "correlation_hashes": shared,
    }) + "\n", encoding="utf-8")

    exit_code = main(["codex", "capture", "report", "--trace-dir", str(trace_dir)])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["gateway_trace_links"] == 1
    assert (trace_dir / "trace_link.jsonl").exists()
    assert "request-1" not in (trace_dir / "trace_link.jsonl").read_text(encoding="utf-8")


def test_codex_capture_report_accepts_separate_gateway_trace_dir(capsys, tmp_path: Path):
    trace_dir = tmp_path / "desktop"
    gateway_dir = tmp_path / "gateway"
    trace_dir.mkdir()
    gateway_dir.mkdir()
    shared = {"x_client_request_id_hash": "hmac-sha256:abc"}
    (trace_dir / "app_server_v2.jsonl").write_text(json.dumps({
        "desktop_trace_id": "cd_1",
        "ts": "2026-05-14T00:00:00.000Z",
        "model": "deepseek-v4-pro",
        "request_path": "/codex/v1/responses",
        "correlation_hashes": shared,
    }) + "\n", encoding="utf-8")
    (gateway_dir / "gateway_trace.jsonl").write_text(json.dumps({
        "gateway_trace_id": "trace_1",
        "ts": "2026-05-14T00:00:00.010Z",
        "model": "deepseek-v4-pro",
        "request_path": "/codex/v1/responses",
        "correlation_hashes": shared,
    }) + "\n", encoding="utf-8")

    exit_code = main([
        "codex", "capture", "report",
        "--trace-dir", str(trace_dir),
        "--gateway-trace-dir", str(gateway_dir),
    ])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["gateway_trace_links"] == 1
    assert (trace_dir / "trace_link.jsonl").exists()


def test_codex_capture_report_discovers_gateway_trace_files_recursively(capsys, tmp_path: Path):
    trace_dir = tmp_path / "desktop"
    gateway_dir = tmp_path / "gateway" / "2026-05-14" / "trace_1"
    trace_dir.mkdir()
    gateway_dir.mkdir(parents=True)
    shared = {"x_client_request_id_hash": "hmac-sha256:abc"}
    (trace_dir / "app_server_v2.jsonl").write_text(json.dumps({
        "desktop_trace_id": "cd_1",
        "ts": "2026-05-14T00:00:00.000Z",
        "model": "deepseek-v4-pro",
        "request_path": "/codex/v1/responses",
        "correlation_hashes": shared,
    }) + "\n", encoding="utf-8")
    (gateway_dir / "gateway_trace.jsonl").write_text(json.dumps({
        "gateway_trace_id": "trace_1",
        "ts": "2026-05-14T00:00:00.010Z",
        "model": "deepseek-v4-pro",
        "request_path": "/codex/v1/responses",
        "correlation_hashes": shared,
    }) + "\n", encoding="utf-8")

    exit_code = main([
        "codex", "capture", "report",
        "--trace-dir", str(trace_dir),
        "--gateway-trace-dir", str(tmp_path / "gateway"),
    ])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["gateway_trace_links"] == 1


def test_codex_capture_status_accepts_correlation_key_file(capsys, tmp_path: Path):
    cli.default_capture_config = ORIGINAL_DEFAULT_CAPTURE_CONFIG
    cli.default_state_store = lambda: type("Store", (), {"read": lambda self: {"desktop_capture_enabled": True}})()
    key = tmp_path / "key"
    key.write_text("shared", encoding="utf-8")

    exit_code = main(["codex", "capture", "--correlation-hash-key-file", str(key), "status"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["config"]["correlation_hash_key_file"] == "set"


def test_capture_install_marks_desktop_capture_enabled(capsys, tmp_path: Path):
    updates = {}
    cli.default_state_store = lambda: type("Store", (), {"update": lambda self, patch: updates.update(patch) or patch, "read": lambda self: {}})()
    cli.default_capture_config = lambda *args: __import__("zhumeng_agent.adapters.codex.capture_config", fromlist=["CodexDesktopCaptureConfig"]).CodexDesktopCaptureConfig.defaults(base_dir=tmp_path / "captures", correlation_hash_key_file=args[0] if args else None)
    cli.install_capture_hook = lambda app_path, config: {"status": "installed", "app_asar_modified": False, "hook_mode": "renderer_readonly"}

    assert main(["codex", "capture", "install", "--app", str(tmp_path / "Codex.app")]) == 0
    install_data = parse_output(capsys)
    assert install_data["status"] == "installed"
    assert updates["desktop_capture_enabled"] is True


def test_capture_uninstall_marks_desktop_capture_disabled(capsys, tmp_path: Path):
    updates = {}
    cli.default_state_store = lambda: type("Store", (), {"update": lambda self, patch: updates.update(patch) or patch, "read": lambda self: {}})()
    cli.default_capture_config = lambda *args: __import__("zhumeng_agent.adapters.codex.capture_config", fromlist=["CodexDesktopCaptureConfig"]).CodexDesktopCaptureConfig.defaults(base_dir=tmp_path / "captures", correlation_hash_key_file=args[0] if args else None)
    cli.uninstall_capture_hook = lambda app_path, config: {"status": "uninstalled", "app_asar_modified": False}

    assert main(["codex", "capture", "uninstall", "--app", str(tmp_path / "Codex.app")]) == 0
    uninstall_data = parse_output(capsys)
    assert uninstall_data["status"] == "uninstalled"
    assert updates["desktop_capture_enabled"] is False


def test_codex_capture_report_uses_cli_correlation_key_file(capsys, tmp_path: Path):
    key = tmp_path / "key"
    key.write_text("shared", encoding="utf-8")
    trace_dir = tmp_path / "traces"
    trace_dir.mkdir()

    exit_code = main(["codex", "capture", "--correlation-hash-key-file", str(key), "report", "--trace-dir", str(trace_dir)])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["correlation_hash_key_file"] == "set"


def test_codex_capture_report_does_not_flag_hash_fields_as_commit_hashes(capsys, tmp_path: Path):
    cli.default_capture_config = ORIGINAL_DEFAULT_CAPTURE_CONFIG
    cli.generate_capture_report = ORIGINAL_GENERATE_CAPTURE_REPORT
    trace_dir = tmp_path / "traces"
    trace_dir.mkdir()
    (trace_dir / "app_server_v2.jsonl").write_text(json.dumps({
        "payload_hash": "sha256:" + "a" * 64,
        "schema_hash": "hmac-sha256:" + "b" * 64,
        "result_hash": "sha256:" + "c" * 64,
    }) + "\n", encoding="utf-8")

    exit_code = main(["codex", "capture", "report", "--trace-dir", str(trace_dir)])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["content_policy_violations"] == 0


def test_launch_codex_starts_cdp_binding_bridge_when_capture_is_installed(capsys, tmp_path: Path):
    class FakeManager:
        def repair(self, *args, **kwargs):
            return None

        def build_model_catalog(self, payload):
            return payload

    class FakeClient:
        def list_codex_models(self, **kwargs):
            return {"models": [{"slug": "claude-opus-4-6", "display_name": "Claude Opus 4.6"}]}

    class FakeStore:
        def __init__(self):
            self.data = {
                "proxy_port": 18081,
                "config_profile": {"model_provider": "zhumeng-managed"},
                "loopback_secret": "loopback-secret",
                "gateway_base_url": "https://example.com",
                "access_token": "access-token",
                "managed_session_id": "managed-session",
                "device_id": 9,
            }

        def read(self):
            return dict(self.data)

        def update(self, payload):
            self.data.update(payload)

    installed = {}
    cli.default_state_store = lambda: FakeStore()
    cli.default_config_manager = lambda: FakeManager()
    cli.default_http_client = lambda server: FakeClient()
    cli.ensure_proxy_running = lambda store: 9999
    cli.detect_codex_app_path = lambda **kwargs: tmp_path / "Codex.app"
    cli.patch_model_picker_app = lambda app_path: {"status": "patched", "app_path": str(app_path)}
    cli.patch_plugin_auth_gate_app = lambda app_path: {"status": "patched", "app_path": str(app_path)}
    cli.patch_plugin_mention_marketplace_app = lambda app_path: {"status": "patched", "app_path": str(app_path)}
    cli.select_cdp_port = lambda: 9333
    cli.launch_codex_process = lambda command: None
    cli.default_capture_config = lambda: __import__(
        "zhumeng_agent.adapters.codex.capture_config",
        fromlist=["CodexDesktopCaptureConfig"],
    ).CodexDesktopCaptureConfig.defaults(enabled=True, base_dir=tmp_path / "captures")
    cli.capture_installation_enabled = lambda app_path, config: True
    cli.ensure_capture_receiver_running = lambda config: (_ for _ in ()).throw(AssertionError("launch capture path must not use renderer network receiver"))
    cli.inject_capture_hook_via_cdp = lambda *args, **kwargs: (_ for _ in ()).throw(AssertionError("launch capture path must use cdp binding bridge"))
    cli.ensure_capture_bridge_running = lambda config, cdp_port: installed.setdefault("bridge", {
        "status": "running",
        "bridge": "cdp_binding",
        "cdp_port": cdp_port,
        "trace_dir_hash": "hmac-sha256:x",
    })

    exit_code = main(["launch", "codex"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["capture"]["status"] == "running"
    assert data["capture"]["bridge"] == "cdp_binding"
    assert data["model_picker"]["status"] == "patched"
    assert data["plugin_auth_gate"]["status"] == "patched"
    assert data["plugin_mention_marketplace"]["status"] == "patched"
    assert installed["bridge"]["cdp_port"] == 9333


def test_launch_codex_reports_installed_but_disabled_capture(capsys, tmp_path: Path):
    class FakeManager:
        def repair(self, *args, **kwargs):
            return None

        def build_model_catalog(self, payload):
            return payload

    class FakeClient:
        def list_codex_models(self, **kwargs):
            return {"models": [{"slug": "claude-opus-4-6", "display_name": "Claude Opus 4.6"}]}

    class FakeStore:
        def __init__(self):
            self.data = {
                "proxy_port": 18081,
                "config_profile": {"model_provider": "zhumeng-managed"},
                "loopback_secret": "loopback-secret",
                "gateway_base_url": "https://example.com",
                "access_token": "access-token",
                "managed_session_id": "managed-session",
                "device_id": 9,
            }

        def read(self):
            return dict(self.data)

        def update(self, payload):
            self.data.update(payload)

    capture_config = __import__(
        "zhumeng_agent.adapters.codex.capture_config",
        fromlist=["CodexDesktopCaptureConfig"],
    ).CodexDesktopCaptureConfig.defaults(enabled=False, base_dir=tmp_path / "captures")

    cli.default_state_store = lambda: FakeStore()
    cli.default_config_manager = lambda: FakeManager()
    cli.default_http_client = lambda server: FakeClient()
    cli.ensure_proxy_running = lambda store: 9999
    cli.detect_codex_app_path = lambda **kwargs: tmp_path / "Codex.app"
    cli.patch_model_picker_app = lambda app_path: {"status": "patched", "app_path": str(app_path)}
    cli.patch_plugin_auth_gate_app = lambda app_path: {"status": "patched", "app_path": str(app_path)}
    cli.patch_plugin_mention_marketplace_app = lambda app_path: {"status": "patched", "app_path": str(app_path)}
    cli.select_cdp_port = lambda: 9333
    cli.launch_codex_process = lambda command: None
    cli.default_capture_config = lambda: capture_config
    cli.capture_installation_enabled = lambda app_path, config: True
    cli.ensure_capture_bridge_running = lambda config, cdp_port: (_ for _ in ()).throw(
        AssertionError("disabled capture must not start bridge")
    )

    exit_code = main(["launch", "codex"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["capture"]["status"] == "installed_but_disabled"


def test_capture_receiver_process_receives_correlation_key_file(tmp_path: Path, monkeypatch: pytest.MonkeyPatch):
    cli.ensure_capture_receiver_running = ORIGINAL_ENSURE_CAPTURE_RECEIVER_RUNNING
    key = tmp_path / "key"
    key.write_bytes(b"shared")
    config = __import__("zhumeng_agent.adapters.codex.capture_config", fromlist=["CodexDesktopCaptureConfig"]).CodexDesktopCaptureConfig.defaults(
        base_dir=tmp_path / "captures",
        correlation_hash_key_file=key,
    )
    captured = {}
    cli.select_cdp_port = lambda: 18765
    monkeypatch.setattr(cli.subprocess, "Popen", lambda command: captured.setdefault("command", command))

    result = cli.ensure_capture_receiver_running(config)

    assert result["port"] == 18765
    assert "--correlation-hash-key-file" in captured["command"]
    assert str(key) in captured["command"]


def test_capture_bridge_process_receives_correlation_key_and_cdp_port(tmp_path: Path, monkeypatch: pytest.MonkeyPatch):
    cli.ensure_capture_bridge_running = ORIGINAL_ENSURE_CAPTURE_BRIDGE_RUNNING
    key = tmp_path / "key"
    key.write_bytes(b"shared")
    config = __import__("zhumeng_agent.adapters.codex.capture_config", fromlist=["CodexDesktopCaptureConfig"]).CodexDesktopCaptureConfig.defaults(
        base_dir=tmp_path / "captures",
        correlation_hash_key_file=key,
    )
    captured = {}

    class FakeProcess:
        pid = 12345

    def fake_popen(command):
        captured["command"] = command
        return FakeProcess()

    monkeypatch.setattr(cli.subprocess, "Popen", fake_popen)

    result = cli.ensure_capture_bridge_running(config, 65031)

    assert result["status"] == "running"
    assert result["bridge"] == "cdp_binding"
    assert "--correlation-hash-key-file" in captured["command"]
    assert str(key) in captured["command"]
    assert "--cdp-port" in captured["command"]
    assert "65031" in captured["command"]
    assert "--target-wait-seconds" in captured["command"]


def test_codex_model_picker_patch_is_separate_from_capture(capsys, tmp_path: Path):
    cli.patch_model_picker_app = lambda app_path: {"status": "patched", "app_path": str(app_path)}

    exit_code = main(["codex", "model-picker", "patch", "--app", str(tmp_path / "Codex.app")])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "codex model-picker patch"
    assert data["status"] == "patched"


def test_codex_plugin_auth_gate_patch_is_explicit(capsys, tmp_path: Path):
    cli.patch_plugin_auth_gate_app = lambda app_path: {"status": "patched", "app_path": str(app_path)}

    exit_code = main(["codex", "plugin-auth-gate", "patch", "--app", str(tmp_path / "Codex.app")])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "codex plugin-auth-gate patch"
    assert data["status"] == "patched"


def test_codex_plugin_auth_gate_patch_failure_returns_json(capsys, tmp_path: Path):
    def fail_patch(app_path):
        raise ModelPickerPatchError("unsupported desktop build")

    cli.patch_plugin_auth_gate_app = fail_patch

    exit_code = main(["codex", "plugin-auth-gate", "patch", "--app", str(tmp_path / "Codex.app")])

    assert exit_code == 1
    data = parse_output(capsys)
    assert data["command"] == "codex plugin-auth-gate patch"
    assert data["status"] == "failed"
    assert "unsupported desktop build" in data["message"]
    assert data["recovery_hint"]


def test_codex_plugin_mention_marketplace_patch_is_explicit(capsys, tmp_path: Path):
    cli.patch_plugin_mention_marketplace_app = lambda app_path: {"status": "patched", "app_path": str(app_path)}

    exit_code = main(["codex", "plugin-mention-marketplace", "patch", "--app", str(tmp_path / "Codex.app")])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "codex plugin-mention-marketplace patch"
    assert data["status"] == "patched"


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
