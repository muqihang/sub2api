from __future__ import annotations

import asyncio
from dataclasses import dataclass
from pathlib import Path
from urllib.parse import urljoin

import httpx
from aiohttp import ClientSession, WSMsgType, WSServerHandshakeError, web
from aiohttp.client_exceptions import ClientPayloadError

from ..state import JsonStateStore
from .upstream import build_upstream_headers, map_upstream_path

HOP_BY_HOP_RESPONSE_HEADERS = {
    "connection",
    "content-length",
    "keep-alive",
    "proxy-authenticate",
    "proxy-authorization",
    "te",
    "trailer",
    "transfer-encoding",
    "upgrade",
}

MANAGED_PROXY_CLIENT_MAX_SIZE = 128 * 1024 * 1024


@dataclass(slots=True)
class ManagedProxyConfig:
    upstream_base_url: str
    device_id: int
    managed_session_id: str
    access_token: str
    loopback_secret: str
    agent_version: str
    runtime_signature: str
    state_store: JsonStateStore
    config_hash: str | None = None
    server_base_url: str | None = None
    refresh_token: str | None = None
    source_root: str | None = None


class ManagedProxyServer:
    def __init__(self, config: ManagedProxyConfig):
        self.config = config
        self.host = "127.0.0.1"
        self._session: ClientSession | None = None

    def create_app(self) -> web.Application:
        app = web.Application(client_max_size=MANAGED_PROXY_CLIENT_MAX_SIZE)
        app.router.add_route("*", "/v1/responses", self.handle_request)
        app.router.add_route("*", "/v1/responses/{tail:.*}", self.handle_request)
        app.router.add_route("*", "/v1/models", self.handle_request)
        app.router.add_get("/__zhumeng/health", self.handle_health)
        app.on_cleanup.append(self._close_session)
        return app

    async def _get_session(self) -> ClientSession:
        if self._session is None:
            self._session = ClientSession()
        return self._session

    async def _close_session(self, app: web.Application) -> None:
        if self._session is not None:
            await self._session.close()
            self._session = None

    async def serve_forever(self, port: int) -> None:
        runner = web.AppRunner(self.create_app())
        await runner.setup()
        site = web.TCPSite(runner, self.host, port)
        await site.start()
        try:
            await asyncio.Event().wait()
        finally:
            await runner.cleanup()

    async def handle_request(self, request: web.Request) -> web.StreamResponse:
        self._sync_credentials_from_state()

        if request.method == "OPTIONS":
            raise web.HTTPNotFound()

        if request.path not in {"/v1/responses", "/v1/models"} and not request.path.startswith("/v1/responses/"):
            raise web.HTTPNotFound()

        origin = request.headers.get("Origin")
        if origin and origin not in {"null", "http://127.0.0.1", "https://127.0.0.1"}:
            raise web.HTTPForbidden(text="unexpected origin")

        expected = f"Bearer zhumeng-local-managed-{self.config.loopback_secret}"
        if request.headers.get("Authorization") != expected:
            raise web.HTTPUnauthorized(text="loopback authorization required")

        if request.method == "GET" and request.path == "/v1/responses" and request.headers.get("Upgrade", "").lower() == "websocket":
            return await self._proxy_websocket(request)

        return await self._proxy_http(request)

    async def handle_health(self, request: web.Request) -> web.Response:
        return web.json_response({
            "ok": True,
            "agent_version": self.config.agent_version,
            "runtime_signature": self.config.runtime_signature,
            "source_root": self.config.source_root or "",
            "proxy_port": request.url.port,
        })

    async def _proxy_http(self, request: web.Request) -> web.Response:
        session = await self._get_session()
        upstream_url = urljoin(self.config.upstream_base_url.rstrip("/") + "/", map_upstream_path(request.path).lstrip("/"))
        body = await request.read()
        inbound_headers = dict(request.headers)
        return await self._forward_http_with_optional_refresh(session, request.method, upstream_url, body, inbound_headers)

    async def _proxy_websocket(self, request: web.Request) -> web.StreamResponse:
        session = await self._get_session()
        upstream_url = urljoin(self.config.upstream_base_url.rstrip("/") + "/", map_upstream_path(request.path).lstrip("/"))
        headers = build_upstream_headers(
            access_token=self.config.access_token,
            managed_session_id=self.config.managed_session_id,
            device_id=self.config.device_id,
            agent_version=self.config.agent_version,
            config_hash=self.config.config_hash,
            inbound_headers=dict(request.headers),
        )
        try:
            try:
                upstream_ws = await session.ws_connect(upstream_url, headers=headers)
            except WSServerHandshakeError as err:
                if err.status in {401, 403} and await self._refresh_credentials():
                    headers = build_upstream_headers(
                        access_token=self.config.access_token,
                        managed_session_id=self.config.managed_session_id,
                        device_id=self.config.device_id,
                        agent_version=self.config.agent_version,
                        config_hash=self.config.config_hash,
                        inbound_headers=dict(request.headers),
                    )
                    upstream_ws = await session.ws_connect(upstream_url, headers=headers)
                else:
                    raise
            try:
                ws_client = web.WebSocketResponse()
                await ws_client.prepare(request)

                async def client_to_upstream() -> None:
                    async for msg in ws_client:
                        if msg.type == WSMsgType.TEXT:
                            await upstream_ws.send_str(msg.data)
                        elif msg.type == WSMsgType.BINARY:
                            await upstream_ws.send_bytes(msg.data)
                        elif msg.type == WSMsgType.PING:
                            await upstream_ws.ping(msg.data)
                        elif msg.type == WSMsgType.PONG:
                            await upstream_ws.pong(msg.data)
                        elif msg.type in {WSMsgType.CLOSE, WSMsgType.CLOSING, WSMsgType.CLOSED}:
                            await upstream_ws.close()
                            break
                        elif msg.type == WSMsgType.ERROR:
                            break

                async def upstream_to_client() -> None:
                    async for msg in upstream_ws:
                        if msg.type == WSMsgType.TEXT:
                            await ws_client.send_str(msg.data)
                        elif msg.type == WSMsgType.BINARY:
                            await ws_client.send_bytes(msg.data)
                        elif msg.type == WSMsgType.PING:
                            await ws_client.ping(msg.data)
                        elif msg.type == WSMsgType.PONG:
                            await ws_client.pong(msg.data)
                        elif msg.type in {WSMsgType.CLOSE, WSMsgType.CLOSING, WSMsgType.CLOSED}:
                            await ws_client.close()
                            break
                        elif msg.type == WSMsgType.ERROR:
                            break

                await asyncio.gather(client_to_upstream(), upstream_to_client())
                self._mark_state_configured()
            finally:
                await upstream_ws.close()
        except WSServerHandshakeError as err:
            if err.status in {401, 403}:
                self.config.state_store.update({"status": "reauthorization_required"})
                if err.status == 401:
                    raise web.HTTPUnauthorized(text="reauthorization required")
                raise web.HTTPForbidden(text="reauthorization required")
            raise
        return ws_client

    async def _forward_http_with_optional_refresh(
        self,
        session: ClientSession,
        method: str,
        upstream_url: str,
        body: bytes,
        inbound_headers: dict[str, str],
    ) -> web.Response:
        headers = build_upstream_headers(
            access_token=self.config.access_token,
            managed_session_id=self.config.managed_session_id,
            device_id=self.config.device_id,
            agent_version=self.config.agent_version,
            config_hash=self.config.config_hash,
            inbound_headers=inbound_headers,
        )
        async with session.request(method, upstream_url, data=body, headers=headers) as response:
            try:
                payload = await response.read()
            except ClientPayloadError:
                payload = b""
                if response.status < 400:
                    return web.Response(status=502, text="upstream response payload was truncated")
            if response.status in {401, 403} and await self._refresh_credentials():
                headers = build_upstream_headers(
                    access_token=self.config.access_token,
                    managed_session_id=self.config.managed_session_id,
                    device_id=self.config.device_id,
                    agent_version=self.config.agent_version,
                    config_hash=self.config.config_hash,
                    inbound_headers=inbound_headers,
                )
                async with session.request(method, upstream_url, data=body, headers=headers) as retried:
                    try:
                        retried_payload = await retried.read()
                    except ClientPayloadError:
                        retried_payload = b""
                        return web.Response(status=502, text="upstream response payload was truncated")
                    if retried.status in {401, 403}:
                        self.config.state_store.update({"status": "reauthorization_required"})
                    elif retried.status < 400:
                        self._mark_state_configured()
                    return web.Response(status=retried.status, body=retried_payload, headers=sanitize_response_headers(retried.headers))
            if response.status in {401, 403}:
                self.config.state_store.update({"status": "reauthorization_required"})
            elif response.status < 400:
                self._mark_state_configured()
            return web.Response(status=response.status, body=payload, headers=sanitize_response_headers(response.headers))

    async def _refresh_credentials(self) -> bool:
        if not self.config.server_base_url or not self.config.refresh_token:
            return False
        try:
            response = httpx.post(
                urljoin(self.config.server_base_url.rstrip("/") + "/", "api/v1/codex/devices/refresh"),
                json={
                    "device_id": self.config.device_id,
                    "refresh_token": self.config.refresh_token,
                },
                timeout=30,
            )
            response.raise_for_status()
            payload = response.json()
            data = payload.get("data", payload)
            self.config.access_token = str(data["access_token"])
            self.config.managed_session_id = str(data["managed_session_id"])
            self.config.refresh_token = str(data.get("refresh_token", self.config.refresh_token))
            self.config.state_store.update({
                "access_token": self.config.access_token,
                "managed_session_id": self.config.managed_session_id,
                "refresh_token": self.config.refresh_token,
                "status": "configured",
            })
            return True
        except Exception:
            return False

    def _sync_credentials_from_state(self) -> None:
        try:
            state = self.config.state_store.read()
        except Exception:
            return
        access_token = str(state.get("access_token", "") or "")
        managed_session_id = str(state.get("managed_session_id", "") or "")
        refresh_token = str(state.get("refresh_token", "") or "")
        if access_token:
            self.config.access_token = access_token
        if managed_session_id:
            self.config.managed_session_id = managed_session_id
        if refresh_token:
            self.config.refresh_token = refresh_token

    def _mark_state_configured(self) -> None:
        try:
            state = self.config.state_store.read()
        except Exception:
            return
        if state.get("status") != "configured":
            self.config.state_store.update({"status": "configured"})


def merge_no_proxy(env: dict[str, str]) -> dict[str, str]:
    merged = dict(env)
    required = ["127.0.0.1", "localhost", "::1"]
    for key in ("NO_PROXY", "no_proxy"):
        current = [item.strip() for item in merged.get(key, "").split(",") if item.strip()]
        for item in required:
            if item not in current:
                current.append(item)
        merged[key] = ",".join(current)
    return merged


def sanitize_response_headers(headers) -> dict[str, str]:
    sanitized: dict[str, str] = {}
    for key, value in headers.items():
        if key.lower() in HOP_BY_HOP_RESPONSE_HEADERS:
            continue
        sanitized[key] = value
    return sanitized
