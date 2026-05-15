from __future__ import annotations

from multidict import CIMultiDict


def map_upstream_path(path: str) -> str:
    if path == "/v1/models":
        return "/codex/v1/models"
    if path == "/v1/responses":
        return "/codex/v1/responses"
    if path.startswith("/v1/responses/"):
        return "/codex" + path
    raise ValueError(f"unsupported path: {path}")


def build_upstream_headers(
    *,
    access_token: str,
    managed_session_id: str,
    device_id: int,
    agent_version: str,
    config_hash: str | None,
    inbound_headers: dict[str, str],
) -> CIMultiDict[str]:
    headers = CIMultiDict()
    for key, value in inbound_headers.items():
        lowered = key.lower()
        if lowered in {"authorization", "host", "content-length"} or lowered.startswith("x-zhumeng-"):
            continue
        headers[key] = value
    headers["Authorization"] = f"Bearer {access_token}"
    headers["X-Zhumeng-Managed-Session"] = managed_session_id
    headers["X-Zhumeng-Device-ID"] = str(device_id)
    headers["X-Zhumeng-Agent-Version"] = agent_version
    if config_hash:
        headers["X-Zhumeng-Config-Hash"] = config_hash
    return headers
