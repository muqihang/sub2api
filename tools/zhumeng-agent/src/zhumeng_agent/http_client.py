from __future__ import annotations

from urllib.parse import urljoin

import httpx


class AgentHTTPClient:
    def __init__(self, server_origin: str):
        self.server_origin = server_origin.rstrip("/") + "/"

    def exchange_setup_grant(self, *, code: str, server_origin: str, client: str = "codex") -> dict[str, object]:
        url = urljoin(self.server_origin, "api/v1/codex/setup-grants/exchange")
        response = httpx.post(
            url,
            json={
                "code": code,
                "server_origin": server_origin,
                "device_name": "Zhumeng Agent",
                "platform": "local",
                "arch": "unknown",
                "manager_version": "0.1.0",
            },
            timeout=30,
        )
        response.raise_for_status()
        payload = response.json()
        return payload.get("data", payload)

    def refresh_device_token(self, *, device_id: int, refresh_token: str) -> dict[str, object]:
        url = urljoin(self.server_origin, "api/v1/codex/devices/refresh")
        response = httpx.post(
            url,
            json={"device_id": device_id, "refresh_token": refresh_token},
            timeout=30,
        )
        response.raise_for_status()
        payload = response.json()
        return payload.get("data", payload)

    def list_codex_models(
        self,
        *,
        gateway_base_url: str,
        access_token: str,
        managed_session_id: str,
        device_id: int,
    ) -> dict[str, object]:
        url = urljoin(gateway_base_url.rstrip("/") + "/", "codex/v1/models")
        response = httpx.get(
            url,
            headers={
                "Authorization": f"Bearer {access_token}",
                "X-Zhumeng-Managed-Session": managed_session_id,
                "X-Zhumeng-Device-ID": str(device_id),
                "X-Zhumeng-Agent-Version": "0.1.0",
            },
            timeout=30,
        )
        response.raise_for_status()
        payload = response.json()
        return payload.get("data", payload)

    def revoke_managed_device(self, *, device_id: int, access_token: str, managed_session_id: str) -> dict[str, object]:
        url = urljoin(self.server_origin, "api/v1/codex/devices/revoke-managed")
        response = httpx.post(
            url,
            json={"device_id": device_id},
            headers={
                "Authorization": access_token,
                "X-Zhumeng-Managed-Session": managed_session_id,
            },
            timeout=30,
        )
        response.raise_for_status()
        payload = response.json()
        return payload.get("data", payload)
