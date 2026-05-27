from __future__ import annotations

import asyncio
import contextlib
import json

from aiohttp import web
from aiohttp.test_utils import TestClient, TestServer
import pytest

from zhumeng_agent.proxy.server import ManagedProxyConfig, ManagedProxyServer, merge_no_proxy, sanitize_response_headers
from zhumeng_agent.state import JsonStateStore


@pytest.mark.asyncio
async def make_proxy(tmp_path, status_code: int = 200, response_body: dict | None = None, ws_status: int = 200):
    seen: dict[str, object] = {}
    response_counter = {"count": 0}
    default_models_payload = {
        "models": [
            {
                "slug": "deepseek-v4-pro",
                "display_name": "DeepSeek V4 Pro",
                "visibility": "visible",
                "supported_in_api": True,
                "capabilities": {
                    "responses": True,
                    "streaming": True,
                    "tool_calls": True,
                    "image_input": True,
                    "context_continuation": True,
                },
                "input_modalities": ["text", "image"],
                "supports_image_detail_original": True,
                "supports_search_tool": True,
                "web_search_tool_type": "text_and_image",
                "supported_reasoning_levels": ["high", "xhigh"],
                "context_window": 262144,
                "max_context_window": 262144,
            }
        ]
    }

    async def upstream_handler(request: web.Request):
        response_counter["count"] += 1
        seen["path"] = request.path
        seen["query_string"] = request.query_string
        body = await request.read()
        seen["body_size"] = len(body)
        seen["authorization"] = request.headers.get("Authorization")
        seen["managed_session"] = request.headers.get("X-Zhumeng-Managed-Session")
        seen["device_id"] = request.headers.get("X-Zhumeng-Device-ID")
        seen["agent_version"] = request.headers.get("X-Zhumeng-Agent-Version")
        seen["config_hash"] = request.headers.get("X-Zhumeng-Config-Hash")
        seen["spoofed_header"] = request.headers.get("X-Zhumeng-Spoofed")
        if callable(response_body):
            result = response_body(response_counter["count"])
            if isinstance(result, tuple):
                status, body = result
            else:
                status, body = status_code, result
        else:
            if request.path == "/codex/v1/models" and response_body is None:
                status, body = 200, default_models_payload
            else:
                status, body = status_code, response_body or {"ok": True}
        return web.json_response(body, status=status)

    async def upstream_ws(request: web.Request):
        if ws_status != 200:
            raise web.HTTPUnauthorized() if ws_status == 401 else web.HTTPForbidden()
        seen["ws_path"] = request.path
        seen["ws_authorization"] = request.headers.get("Authorization")
        ws = web.WebSocketResponse()
        await ws.prepare(request)
        await ws.send_str("welcome")
        msg = await ws.receive_str()
        await ws.send_str(f"echo:{msg}")
        await ws.send_str("done")
        await ws.close()
        return ws

    upstream_app = web.Application(client_max_size=4 * 1024 * 1024)
    upstream_app.router.add_post("/codex/v1/responses", upstream_handler)
    upstream_app.router.add_post("/codex/v1/responses/{tail:.*}", upstream_handler)
    upstream_app.router.add_get("/codex/v1/models", upstream_handler)
    upstream_app.router.add_get("/codex/v1/responses", upstream_ws)

    upstream_server = TestServer(upstream_app)
    await upstream_server.start_server()

    state_store = JsonStateStore(tmp_path / "state.json")
    config = ManagedProxyConfig(
      upstream_base_url=str(upstream_server.make_url("")).rstrip("/"),
      device_id=9,
      managed_session_id="sess-1",
      access_token="access-token",
      loopback_secret="loopback-secret",
      agent_version="0.1.0",
      runtime_signature="sig-1",
      source_root="/tmp/zhumeng-agent",
      config_hash="cfg-hash",
      server_base_url=str(upstream_server.make_url("")).rstrip("/"),
      refresh_token="refresh-token",
      state_store=state_store,
    )
    proxy = ManagedProxyServer(config)
    proxy_server = TestServer(proxy.create_app())
    await proxy_server.start_server()
    proxy_client = TestClient(proxy_server)
    await proxy_client.start_server()
    return upstream_server, proxy_client, seen, state_store


@pytest.mark.asyncio
async def test_only_codex_gateway_paths_are_accepted(tmp_path):
    upstream, proxy_client, seen, _ = await make_proxy(tmp_path)
    async with proxy_client:
        resp = await proxy_client.post(
            "/v1/responses",
            json={"model": "gpt-5.4", "input": "hello"},
            headers={"Authorization": "Bearer zhumeng-local-managed-loopback-secret"},
        )
        assert resp.status == 200
        assert seen["path"] == "/codex/v1/responses"

        unknown = await proxy_client.post(
            "/v1/chat/completions",
            json={},
            headers={"Authorization": "Bearer zhumeng-local-managed-loopback-secret"},
        )
        assert unknown.status == 404
    await upstream.close()


@pytest.mark.asyncio
async def test_health_endpoint_reports_runtime_identity(tmp_path):
    upstream, proxy_client, _, _ = await make_proxy(tmp_path)
    async with proxy_client:
        resp = await proxy_client.get("/__zhumeng/health")
        assert resp.status == 200
        payload = await resp.json()
        assert payload["ok"] is True
        assert payload["agent_version"] == "0.1.0"
        assert payload["runtime_signature"] == "sig-1"
        assert payload["source_root"] == "/tmp/zhumeng-agent"
    await upstream.close()


@pytest.mark.asyncio
async def test_large_image_payload_is_forwarded(tmp_path):
    upstream, proxy_client, seen, _ = await make_proxy(tmp_path)
    payload = b'{"model":"gpt-5.4","input":"' + (b"x" * (1024 * 1024 + 256 * 1024)) + b'"}'

    async with proxy_client:
        resp = await proxy_client.post(
            "/v1/responses",
            data=payload,
            headers={
                "Authorization": "Bearer zhumeng-local-managed-loopback-secret",
                "Content-Type": "application/json",
            },
        )
        assert resp.status == 200
        assert seen["path"] == "/codex/v1/responses"
        assert seen["body_size"] == len(payload)
    await upstream.close()


@pytest.mark.asyncio
async def test_event_stream_response_is_forwarded_before_upstream_finishes(tmp_path):
    first_chunk_sent = asyncio.Event()
    allow_finish = asyncio.Event()

    async def upstream_sse(request: web.Request):
        await request.read()
        response = web.StreamResponse(status=200, headers={"Content-Type": "text/event-stream"})
        await response.prepare(request)
        await response.write(
            b'event: response.created\n'
            b'data: {"type":"response.created","response":{"id":"resp_stream"}}\n\n'
        )
        first_chunk_sent.set()
        await allow_finish.wait()
        await response.write(
            b'event: response.completed\n'
            b'data: {"type":"response.completed","response":{"id":"resp_stream","status":"completed"}}\n\n'
        )
        await response.write_eof()
        return response

    upstream_app = web.Application(client_max_size=4 * 1024 * 1024)
    upstream_app.router.add_post("/codex/v1/responses", upstream_sse)
    upstream_server = TestServer(upstream_app)
    await upstream_server.start_server()

    state_store = JsonStateStore(tmp_path / "state.json")
    config = ManagedProxyConfig(
      upstream_base_url=str(upstream_server.make_url("")).rstrip("/"),
      device_id=9,
      managed_session_id="sess-1",
      access_token="access-token",
      loopback_secret="loopback-secret",
      agent_version="0.1.0",
      runtime_signature="sig-1",
      source_root="/tmp/zhumeng-agent",
      state_store=state_store,
    )
    proxy = ManagedProxyServer(config)
    proxy_server = TestServer(proxy.create_app())
    await proxy_server.start_server()
    proxy_client = TestClient(proxy_server)
    await proxy_client.start_server()

    response_task: asyncio.Task | None = None
    try:
        async with proxy_client:
            response_task = asyncio.create_task(proxy_client.post(
                "/v1/responses",
                json={"model": "gpt-5.4", "input": "hello", "stream": True},
                headers={"Authorization": "Bearer zhumeng-local-managed-loopback-secret"},
            ))
            await asyncio.wait_for(first_chunk_sent.wait(), timeout=1)
            response = await asyncio.wait_for(asyncio.shield(response_task), timeout=0.5)
            assert response.status == 200
            assert response.headers["Content-Type"].startswith("text/event-stream")
            first_line = await asyncio.wait_for(response.content.readline(), timeout=0.5)
            assert first_line == b"event: response.created\n"
    finally:
        allow_finish.set()
        if response_task is not None and not response_task.done():
            with contextlib.suppress(Exception):
                await asyncio.wait_for(response_task, timeout=1)
        await upstream_server.close()


@pytest.mark.asyncio
async def test_proxy_upstream_session_disables_total_timeout_for_long_streams(tmp_path):
    upstream, proxy_client, _, _ = await make_proxy(tmp_path)
    try:
        async with proxy_client:
            proxy_server = proxy_client.server.app["proxy_server"]
            session = await proxy_server._get_session()
            assert session.timeout.total is None
            assert session.timeout.sock_read is None
    finally:
        await upstream.close()


@pytest.mark.asyncio
async def test_missing_loopback_secret_is_rejected(tmp_path):
    upstream, proxy_client, _, _ = await make_proxy(tmp_path)
    async with proxy_client:
        resp = await proxy_client.post("/v1/responses", json={"x": 1})
        assert resp.status == 401
    await upstream.close()


@pytest.mark.asyncio
async def test_unexpected_origin_is_rejected(tmp_path):
    upstream, proxy_client, _, _ = await make_proxy(tmp_path)
    async with proxy_client:
        resp = await proxy_client.post(
            "/v1/responses",
            json={"x": 1},
            headers={
                "Authorization": "Bearer zhumeng-local-managed-loopback-secret",
                "Origin": "https://evil.example.com",
            },
        )
        assert resp.status == 403
    await upstream.close()


@pytest.mark.asyncio
async def test_managed_headers_and_authorization_are_injected_upstream(tmp_path):
    upstream, proxy_client, seen, _ = await make_proxy(tmp_path)
    async with proxy_client:
        resp = await proxy_client.post(
            "/v1/responses/resp_123",
            json={"model": "gpt-5.4", "input": "hello"},
            headers={
                "Authorization": "Bearer zhumeng-local-managed-loopback-secret",
                "X-Zhumeng-Managed-Session": "spoofed-session",
                "X-Zhumeng-Device-ID": "123456",
                "X-Zhumeng-Agent-Version": "spoofed-version",
                "X-Zhumeng-Config-Hash": "spoofed-hash",
                "X-Zhumeng-Spoofed": "spoofed",
            },
        )
        assert resp.status == 200
        assert seen["path"] == "/codex/v1/responses/resp_123"
        assert seen["authorization"] == "Bearer access-token"
        assert seen["managed_session"] == "sess-1"
        assert seen["device_id"] == "9"
        assert seen["agent_version"] == "0.1.0"
        assert seen["config_hash"] == "cfg-hash"
        assert seen["spoofed_header"] is None
    await upstream.close()


@pytest.mark.asyncio
async def test_models_path_maps_to_codex_gateway_models(tmp_path):
    upstream, proxy_client, seen, _ = await make_proxy(tmp_path)
    async with proxy_client:
        resp = await proxy_client.get(
            "/v1/models",
            headers={"Authorization": "Bearer zhumeng-local-managed-loopback-secret"},
        )
        assert resp.status == 200
        assert seen["path"] == "/codex/v1/models"
    await upstream.close()


@pytest.mark.asyncio
async def test_upstream_401_marks_state_reauthorization_required(tmp_path):
    upstream, proxy_client, _, state_store = await make_proxy(tmp_path, status_code=401, response_body={"error": "unauthorized"})
    async with proxy_client:
        resp = await proxy_client.post(
            "/v1/responses",
            json={"model": "gpt-5.4", "input": "hello"},
            headers={"Authorization": "Bearer zhumeng-local-managed-loopback-secret"},
        )
        assert resp.status == 401
        state = state_store.read()
        assert state["status"] == "reauthorization_required"
    await upstream.close()


@pytest.mark.asyncio
async def test_upstream_401_refreshes_and_retries(tmp_path, monkeypatch):
    def body_for_attempt(count: int):
        if count == 1:
            return 401, {"error": "unauthorized"}
        return 200, {"ok": True}

    upstream, proxy_client, seen, state_store = await make_proxy(tmp_path, status_code=401, response_body=body_for_attempt)

    class FakeRefreshResponse:
        def raise_for_status(self):
            return None

        def json(self):
            return {
                "data": {
                    "access_token": "fresh-access-token",
                    "refresh_token": "fresh-refresh-token",
                    "managed_session_id": "fresh-session",
                }
            }

    monkeypatch.setattr("zhumeng_agent.proxy.server.httpx.post", lambda *args, **kwargs: FakeRefreshResponse())

    async with proxy_client:
        resp = await proxy_client.post(
            "/v1/responses",
            json={"model": "gpt-5.4", "input": "hello"},
            headers={"Authorization": "Bearer zhumeng-local-managed-loopback-secret"},
        )
        assert resp.status == 200
        state = state_store.read()
        assert state["access_token"] == "fresh-access-token"
        assert state["managed_session_id"] == "fresh-session"
        assert state["refresh_token"] == "fresh-refresh-token"
        assert state["status"] == "configured"
        assert seen["authorization"] == "Bearer fresh-access-token"
    await upstream.close()


@pytest.mark.asyncio
async def test_proxy_reloads_latest_credentials_from_state_before_forwarding(tmp_path):
    upstream, proxy_client, seen, state_store = await make_proxy(tmp_path)
    state_store.write({
        "access_token": "fresh-access-token-from-state",
        "managed_session_id": "fresh-session-from-state",
        "refresh_token": "fresh-refresh-token-from-state",
        "status": "configured",
    })

    async with proxy_client:
        resp = await proxy_client.post(
            "/v1/responses",
            json={"model": "gpt-5.4", "input": "hello"},
            headers={"Authorization": "Bearer zhumeng-local-managed-loopback-secret"},
        )
        assert resp.status == 200
        assert seen["authorization"] == "Bearer fresh-access-token-from-state"
        assert seen["managed_session"] == "fresh-session-from-state"
    await upstream.close()


@pytest.mark.asyncio
async def test_proxy_syncs_model_catalog_from_gateway_models(tmp_path):
    upstream, proxy_client, seen, state_store = await make_proxy(tmp_path)
    state_store.write({
        "gateway_base_url": str(upstream.make_url("")).rstrip("/"),
        "config_profile": {"model_provider": "zhumeng-codex"},
        "status": "configured",
    })
    proxy_client.server.app["proxy_server"].config.codex_home = tmp_path / ".codex"

    async with proxy_client:
        await proxy_client.server.app["proxy_server"]._maybe_sync_model_catalog(force=True)

    catalog_path = tmp_path / ".codex" / "zhumeng-codex-models.json"
    payload = json.loads(catalog_path.read_text(encoding="utf-8"))
    deepseek = next(model for model in payload["models"] if model["slug"] == "deepseek-v4-pro")
    assert deepseek["input_modalities"] == ["text", "image"]
    assert deepseek["supports_image_detail_original"] is True
    assert deepseek["supports_search_tool"] is True
    assert deepseek["web_search_tool_type"] == "text_and_image"
    assert deepseek["capabilities"]["image_input"] is True
    assert seen["query_string"] == "catalog_format=codex_cli"
    await upstream.close()


@pytest.mark.asyncio
async def test_websocket_handshake_injects_managed_authorization(tmp_path):
    upstream, proxy_client, seen, _ = await make_proxy(tmp_path)
    async with proxy_client:
        ws = await proxy_client.ws_connect(
            "/v1/responses",
            headers={"Authorization": "Bearer zhumeng-local-managed-loopback-secret"},
        )
        welcome = await ws.receive()
        assert welcome.data == "welcome"
        await ws.send_str("hello")
        msg = await ws.receive()
        assert msg.data == "echo:hello"
        tail = await ws.receive()
        assert tail.data == "done"
        assert seen["ws_path"] == "/codex/v1/responses"
        assert seen["ws_authorization"] == "Bearer access-token"
        await ws.close()
    await upstream.close()


@pytest.mark.asyncio
async def test_websocket_handshake_401_marks_state_reauthorization_required(tmp_path):
    upstream, proxy_client, _, state_store = await make_proxy(tmp_path, ws_status=401)
    async with proxy_client:
        with pytest.raises(Exception):
            await proxy_client.ws_connect(
                "/v1/responses",
                headers={"Authorization": "Bearer zhumeng-local-managed-loopback-secret"},
            )
        state = state_store.read()
        assert state["status"] == "reauthorization_required"
    await upstream.close()


def test_merge_no_proxy_preserves_existing_entries():
    merged = merge_no_proxy({
        "NO_PROXY": "example.com",
        "no_proxy": "internal.local",
        "HTTP_PROXY": "http://127.0.0.1:7890",
        "HTTPS_PROXY": "http://127.0.0.1:7890",
    })

    assert "example.com" in merged["NO_PROXY"]
    assert "internal.local" in merged["no_proxy"]
    assert "127.0.0.1" in merged["NO_PROXY"]
    assert "localhost" in merged["NO_PROXY"]
    assert "::1" in merged["NO_PROXY"]
    assert merged["HTTP_PROXY"] == "http://127.0.0.1:7890"


def test_sanitize_response_headers_drops_hop_by_hop_headers():
    headers = sanitize_response_headers({
        "Content-Type": "application/json",
        "Transfer-Encoding": "chunked",
        "Content-Length": "42",
        "Connection": "keep-alive",
        "X-Request-ID": "req_123",
    })

    assert headers == {
        "Content-Type": "application/json",
        "X-Request-ID": "req_123",
    }


def test_proxy_binds_loopback_only(tmp_path):
    config = ManagedProxyConfig(
      upstream_base_url="https://example.com",
      device_id=9,
      managed_session_id="sess-1",
      access_token="access-token",
      loopback_secret="loopback-secret",
      agent_version="0.1.0",
      runtime_signature="sig-1",
      state_store=JsonStateStore(tmp_path / "state.json"),
    )
    proxy = ManagedProxyServer(config)
    assert proxy.host == "127.0.0.1"
