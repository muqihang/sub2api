from __future__ import annotations

import asyncio
from dataclasses import dataclass
from urllib.parse import urljoin

import httpx
from aiohttp import ClientSession, WSMsgType, WSServerHandshakeError, web

from ..state import JsonStateStore
from .upstream import build_upstream_headers, map_upstream_path


@dataclass(slots=True)
class ManagedProxyConfig:
    upstream_base_url: str
    device_id: int
    managed_session_id: str
    access_token: str
    loopback_secret: str
    agent_version: str
    state_store: JsonStateStore
    config_hash: str | None = None
    server_base_url: str | None = None
    refresh_token: str | None = None


class ManagedProxyServer:
    def __init__(self, config: ManagedProxyConfig):
        self.config = config
        self.host = "127.0.0.1"
        self._session: ClientSession | None = None

    def create_app(self) -> web.Application:
        app = web.Application()
        app.router.add_route("*", "/v1/responses", self.handle_request)
        app.router.add_route("*", "/v1/responses/{tail:.*}", self.handle_request)
        app.router.add_route("*", "/v1/models", self.handle_request)
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
            payload = await response.read()
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
                    retried_payload = await retried.read()
                    if retried.status in {401, 403}:
                        self.config.state_store.update({"status": "reauthorization_required"})
                    return web.Response(status=retried.status, body=retried_payload, headers=retried.headers)
            if response.status in {401, 403}:
                self.config.state_store.update({"status": "reauthorization_required"})
            return web.Response(status=response.status, body=payload, headers=response.headers)

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
            })
            return True
        except Exception:
            return False


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
