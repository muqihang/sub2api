from __future__ import annotations

from dataclasses import dataclass
from ipaddress import ip_address
from urllib.parse import parse_qs, urlparse


@dataclass(slots=True)
class TrustedOriginPolicy:
    allowed_origins: set[str]
    dev_mode: bool = False

    def validate(self, raw_origin: str) -> str:
        parsed = urlparse(raw_origin.strip())
        scheme = parsed.scheme.lower()
        normalized = parsed._replace(path=parsed.path.rstrip("/")).geturl().rstrip("/")

        if self.dev_mode:
            if scheme not in {"http", "https"}:
                raise ValueError("origin must use http or https")
            return normalized

        if scheme != "https":
            raise ValueError("origin must use https")
        if parsed.username or parsed.password:
            raise ValueError("origin must not include userinfo")
        if parsed.path not in {"", "/"}:
            raise ValueError("origin must not include path")

        host = parsed.hostname or ""
        if host.lower() == "localhost":
            raise ValueError("localhost is not allowed")

        try:
            address = ip_address(host)
        except ValueError:
            address = None

        if address is not None and (address.is_loopback or address.is_private or address.is_link_local):
            raise ValueError("private or loopback ip is not allowed")

        if self.allowed_origins and normalized not in self.allowed_origins:
            raise ValueError("origin is not trusted")
        return normalized


def parse_zhumeng_deeplink(raw_url: str) -> dict[str, str]:
    parsed = urlparse(raw_url.strip())
    if parsed.scheme != "zhumeng-agent":
        raise ValueError("unsupported deeplink scheme")
    action = parsed.netloc
    if action not in {"setup", "reauth", "open"}:
        raise ValueError("unsupported deeplink action")
    query = parse_qs(parsed.query)
    if action == "open":
        app = query.get("app", [""])[0]
        if not app:
            raise ValueError("deeplink is missing required open parameters")
        return {"action": action, "app": app}
    client = query.get("client", [""])[0]
    code = query.get("code", [""])[0]
    server = query.get("server", [""])[0]
    if not client or not code or not server:
        raise ValueError("deeplink is missing required setup parameters")
    return {
        "action": action,
        "client": client,
        "code": code,
        "server": server,
    }
