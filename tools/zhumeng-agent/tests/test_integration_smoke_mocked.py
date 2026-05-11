from __future__ import annotations

from pathlib import Path

import zhumeng_agent.cli as cli
from zhumeng_agent.proxy.server import ManagedProxyConfig, ManagedProxyServer
from zhumeng_agent.state import JsonStateStore


def test_setup_writes_managed_state_and_codex_config(tmp_path: Path, capsys):
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

    state_store = JsonStateStore(tmp_path / "state" / "state.json")
    cli.default_http_client = lambda server: FakeClient()
    cli.default_state_store = lambda: state_store
    cli.default_config_manager = lambda: __import__("zhumeng_agent.adapters.codex.config_manager", fromlist=["CodexConfigManager"]).CodexConfigManager(tmp_path / ".codex")
    cli.generate_loopback_secret = lambda: "loopback-secret"
    cli.choose_local_proxy_port = lambda preferred=None: 18081

    exit_code = cli.main([
        "setup",
        "--client", "codex",
        "--code", "grant-1",
        "--server", "https://example.com",
    ])

    assert exit_code == 0
    state = state_store.read()
    assert state["device_id"] == 9
    assert state["proxy_port"] == 18081

    config_text = (tmp_path / ".codex" / "config.toml").read_text(encoding="utf-8")
    auth_text = (tmp_path / ".codex" / "auth.json").read_text(encoding="utf-8")
    assert 'model_provider = "zhumeng-managed"' in config_text
    assert "sk-" not in auth_text
    assert "zhumeng-local-managed-loopback-secret" in auth_text
    assert "grant-1" not in capsys.readouterr().out


def test_proxy_marks_reauthorization_without_dropping_state(tmp_path: Path):
    store = JsonStateStore(tmp_path / "state.json")
    store.write({
        "device_id": 9,
        "server_base_url": "https://example.com",
        "gateway_base_url": "https://example.com",
        "status": "configured",
    })

    config = ManagedProxyConfig(
        upstream_base_url="https://example.com",
        device_id=9,
        managed_session_id="sess-1",
        access_token="access-token",
        loopback_secret="loopback-secret",
        agent_version="0.1.0",
        state_store=store,
    )
    config.state_store.update({"status": "reauthorization_required"})

    state = store.read()
    assert state["status"] == "reauthorization_required"
    assert state["device_id"] == 9
    assert state["server_base_url"] == "https://example.com"
