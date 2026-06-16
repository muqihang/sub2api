"""CP4 route-hint trust contract for managed Claude Code /v1/messages.

The route hint is a guard-local contract: it selects native vs bridge headers for
one request, but it never authorizes formal-pool access by itself. The backend
must still derive native/formal-pool admission from its server-side catalog.
"""
from __future__ import annotations

from dataclasses import dataclass
from hashlib import sha256
from typing import Any, Mapping
import base64
import hmac
import json
import re
import secrets
import time

ROUTE_HINT_HEADER = "x-zhumeng-claude-code-route-hint"
ROUTE_HINT_SIGNATURE_HEADER = "x-zhumeng-claude-code-route-signature"
ROUTE_HINT_SCOPE = "claude_code_route_hint_cp4"
ROUTE_HINT_VERSION = 1
_UNKNOWN_HASH = "sha256:" + ("0" * 64)
_HASH_RE = re.compile(r"^sha256:[0-9a-f]{64}$")


@dataclass(frozen=True)
class RouteCatalogEntry:
    model_id: str
    provider: str
    route: str
    client_type: str
    live_enabled: bool
    formal_pool_allowed: bool
    native_attestation_allowed: bool
    provider_owner: str
    credential_scope: str
    gateway_location: str


@dataclass(frozen=True)
class RouteCatalog:
    runtime_hash: str
    overlay_hash: str
    catalog_hash: str
    catalog_version: str
    entries: Mapping[str, RouteCatalogEntry]

    def entry_for(self, model_id: str) -> RouteCatalogEntry | None:
        return self.entries.get(model_id)


@dataclass(frozen=True)
class RouteDecision:
    model_id: str
    provider: str
    route: str
    client_type: str
    live_request_allowed: bool
    formal_pool_allowed: bool
    native_attestation_allowed: bool
    provider_owner: str
    credential_scope: str
    gateway_location: str
    runtime_hash: str
    overlay_hash: str
    catalog_hash: str
    catalog_version: str
    session_ref: str
    nonce: str


class RouteHintReplayCache:
    def __init__(self, ttl_seconds: int = 60, now_fn: Any | None = None):
        self.ttl_seconds = ttl_seconds
        self.now_fn = now_fn or (lambda: int(time.time()))
        self._entries: dict[str, int] = {}

    def check_and_record(self, *, key_id: str, scope: str, nonce: str, now: int | None = None) -> None:
        current = int(self.now_fn() if now is None else now)
        self._evict(current)
        replay_key = f"{scope}:{key_id}:{nonce}"
        expiry = self._entries.get(replay_key)
        if expiry is not None and expiry > current:
            raise RuntimeError("route hint nonce replayed")
        self._entries[replay_key] = current + self.ttl_seconds

    def _evict(self, current: int) -> None:
        expired = [key for key, expiry in self._entries.items() if expiry <= current]
        for key in expired:
            self._entries.pop(key, None)


def cp4_fixture_route_catalog(
    *,
    runtime_hash: str = _UNKNOWN_HASH,
    overlay_hash: str = _UNKNOWN_HASH,
    catalog_hash: str = _UNKNOWN_HASH,
    catalog_version: str = "cp4-fixture-v1",
) -> RouteCatalog:
    runtime_hash = _normalize_hash(runtime_hash, "runtime_hash")
    overlay_hash = _normalize_hash(overlay_hash, "overlay_hash")
    catalog_hash = _normalize_hash(catalog_hash, "catalog_hash")
    def native(model_id: str) -> RouteCatalogEntry:
        return RouteCatalogEntry(
            model_id=model_id,
            provider="claude",
            route="claude_code_native",
            client_type="claude_code_native",
            live_enabled=True,
            formal_pool_allowed=True,
            native_attestation_allowed=True,
            provider_owner="zhumeng_managed",
            credential_scope="formal_pool",
            gateway_location="cloud",
        )

    def bridge(model_id: str, provider: str, route: str, client_type: str) -> RouteCatalogEntry:
        return RouteCatalogEntry(
            model_id=model_id,
            provider=provider,
            route=route,
            client_type=client_type,
            live_enabled=False,
            formal_pool_allowed=False,
            native_attestation_allowed=False,
            provider_owner="zhumeng_managed",
            credential_scope="bridge_pool",
            gateway_location="cloud",
        )

    entries = {
        "claude-sonnet-4-6": native("claude-sonnet-4-6"),
        "claude-opus-4-7": native("claude-opus-4-7"),
        "openai-catalog-placeholder": bridge("openai-catalog-placeholder", "openai", "openai_bridge", "claude_code_bridge_openai"),
        "gpt-5.5": bridge("gpt-5.5", "openai", "openai_bridge", "claude_code_bridge_openai"),
        "deepseek-v4-pro": bridge("deepseek-v4-pro", "deepseek", "deepseek_bridge", "claude_code_bridge_deepseek"),
        "deepseek-v4-flash": bridge("deepseek-v4-flash", "deepseek", "deepseek_bridge", "claude_code_bridge_deepseek"),
        "agnes-1": bridge("agnes-1", "agnes", "agnes_bridge", "claude_code_bridge_agnes"),
        "glm-5.2": bridge("glm-5.2", "zai_glm", "zai_glm_bridge", "claude_code_bridge_zai_glm"),
        "glm-5.2[1m]": bridge("glm-5.2[1m]", "zai_glm", "zai_glm_bridge", "claude_code_bridge_zai_glm"),
        "glm-5-turbo": bridge("glm-5-turbo", "zai_glm", "zai_glm_bridge", "claude_code_bridge_zai_glm"),
        "glm-4.7": bridge("glm-4.7", "zai_glm", "zai_glm_bridge", "claude_code_bridge_zai_glm"),
        "glm-4.5-air": bridge("glm-4.5-air", "zai_glm", "zai_glm_bridge", "claude_code_bridge_zai_glm"),
        "kimi-k2.7-code": bridge("kimi-k2.7-code", "kimi", "kimi_bridge", "claude_code_bridge_kimi"),
        "kimi-k2.7-code-highspeed": bridge("kimi-k2.7-code-highspeed", "kimi", "kimi_bridge", "claude_code_bridge_kimi"),
        "kimi-k2.6": bridge("kimi-k2.6", "kimi", "kimi_bridge", "claude_code_bridge_kimi"),
        "kimi-k2.5": bridge("kimi-k2.5", "kimi", "kimi_bridge", "claude_code_bridge_kimi"),
    }
    return RouteCatalog(
        runtime_hash=runtime_hash,
        overlay_hash=overlay_hash,
        catalog_hash=catalog_hash,
        catalog_version=str(catalog_version),
        entries=entries,
    )


def build_signed_route_hint_headers(
    *,
    body: bytes,
    request_path: str,
    catalog: RouteCatalog,
    model_id: str,
    session_ref: str,
    secret: str,
    now: int | None = None,
    nonce: str | None = None,
    key_id: str = "route_hint_v1",
    ttl_seconds: int = 60,
    route: str | None = None,
    client_type: str | None = None,
    provider: str | None = None,
    live_request_allowed: bool | None = None,
    formal_pool_allowed: bool | None = None,
    native_attestation_allowed: bool | None = None,
    runtime_hash: str | None = None,
    overlay_hash: str | None = None,
    catalog_hash: str | None = None,
    catalog_version: str | None = None,
    provider_owner: str | None = None,
    credential_scope: str | None = None,
    gateway_location: str | None = None,
) -> dict[str, str]:
    if not secret:
        raise RuntimeError("route hint secret is required")
    issued_at = int(time.time() if now is None else now)
    entry = catalog.entry_for(model_id)
    if entry is None:
        raise RuntimeError("unknown route hint model")
    live_default = bool(entry.live_enabled) if entry is not None else False
    payload = {
        "key_id": key_id,
        "scope": ROUTE_HINT_SCOPE,
        "version": ROUTE_HINT_VERSION,
        "issued_at": issued_at,
        "expires_at": issued_at + int(ttl_seconds),
        "nonce": nonce or secrets.token_hex(16),
        "method": "POST",
        "request_uri": request_path,
        "model_id": model_id,
        "body_model": body_model_id(body),
        "body_sha256": "sha256:" + sha256(body).hexdigest(),
        "runtime_hash": runtime_hash or catalog.runtime_hash,
        "overlay_hash": overlay_hash or catalog.overlay_hash,
        "catalog_hash": catalog_hash or catalog.catalog_hash,
        "catalog_version": catalog_version or catalog.catalog_version,
        "session_ref": str(session_ref),
        "route": route if route is not None else entry.route,  # type: ignore[union-attr]
        "client_type": client_type if client_type is not None else entry.client_type,  # type: ignore[union-attr]
        "provider": provider if provider is not None else entry.provider,  # type: ignore[union-attr]
        "live_request_allowed": live_default if live_request_allowed is None else bool(live_request_allowed),
        "formal_pool_allowed": (entry.formal_pool_allowed if formal_pool_allowed is None else bool(formal_pool_allowed)),  # type: ignore[union-attr]
        "native_attestation_allowed": (entry.native_attestation_allowed if native_attestation_allowed is None else bool(native_attestation_allowed)),  # type: ignore[union-attr]
        "provider_owner": provider_owner if provider_owner is not None else entry.provider_owner,  # type: ignore[union-attr]
        "credential_scope": credential_scope if credential_scope is not None else entry.credential_scope,  # type: ignore[union-attr]
        "gateway_location": gateway_location if gateway_location is not None else entry.gateway_location,  # type: ignore[union-attr]
    }
    encoded = _encode_payload(payload)
    return {
        ROUTE_HINT_HEADER: encoded,
        ROUTE_HINT_SIGNATURE_HEADER: _sign_route_hint(encoded, "POST", request_path, body, secret),
    }


def verify_signed_route_hint_headers(
    *,
    source_headers: Mapping[str, str],
    body: bytes,
    request_path: str,
    catalog: RouteCatalog,
    session_ref: str,
    secret: str,
    now: int | None = None,
    replay_cache: RouteHintReplayCache | None = None,
) -> RouteDecision:
    if not secret:
        raise RuntimeError("route hint secret is required")
    hint = _header(source_headers, ROUTE_HINT_HEADER)
    signature = _header(source_headers, ROUTE_HINT_SIGNATURE_HEADER)
    if not hint or not signature:
        raise RuntimeError("route hint is required")
    payload = _decode_payload(hint)
    _validate_payload_shape(payload)
    body_model = body_model_id(body)
    if body_model != payload["body_model"] or body_model != payload["model_id"]:
        raise RuntimeError("route hint model binding mismatch")
    expected = _sign_route_hint(hint, "POST", request_path, body, secret)
    if not hmac.compare_digest(expected, signature):
        raise RuntimeError("route hint signature mismatch")
    current = int(time.time() if now is None else now)
    issued_at = int(payload["issued_at"])
    expires_at = int(payload["expires_at"])
    if current > expires_at or issued_at > current + 30:
        raise RuntimeError("route hint stale")
    if payload["method"] != "POST" or payload["request_uri"] != request_path:
        raise RuntimeError("route hint request binding mismatch")
    if payload["body_sha256"] != "sha256:" + sha256(body).hexdigest():
        raise RuntimeError("route hint body binding mismatch")
    if payload["session_ref"] != str(session_ref):
        raise RuntimeError("route hint session binding mismatch")
    if payload["runtime_hash"] != catalog.runtime_hash:
        raise RuntimeError("route hint runtime binding mismatch")
    if payload["overlay_hash"] != catalog.overlay_hash:
        raise RuntimeError("route hint overlay binding mismatch")
    if payload["catalog_hash"] != catalog.catalog_hash or payload["catalog_version"] != catalog.catalog_version:
        raise RuntimeError("route hint catalog binding mismatch")
    entry = catalog.entry_for(payload["model_id"])
    if entry is None:
        raise RuntimeError("route hint unknown model")
    expected_fields = {
        "provider": entry.provider,
        "route": entry.route,
        "client_type": entry.client_type,
        "provider_owner": entry.provider_owner,
        "credential_scope": entry.credential_scope,
        "gateway_location": entry.gateway_location,
        "live_request_allowed": entry.live_enabled,
        "formal_pool_allowed": entry.formal_pool_allowed,
        "native_attestation_allowed": entry.native_attestation_allowed,
    }
    for key, expected_value in expected_fields.items():
        if payload.get(key) != expected_value:
            raise RuntimeError(f"route hint catalog route binding mismatch for {key}")
    if entry.provider != "claude" and (payload["client_type"] == "claude_code_native" or payload["native_attestation_allowed"] or payload["formal_pool_allowed"]):
        raise RuntimeError("route hint bridge cannot claim native")
    if payload["client_type"] == "claude_code_native" and not payload["native_attestation_allowed"]:
        raise RuntimeError("route hint native attestation binding mismatch")
    cache = replay_cache or RouteHintReplayCache()
    cache.check_and_record(
        key_id=str(payload["key_id"]),
        scope=str(payload["scope"]),
        nonce=str(payload["nonce"]),
        now=current,
    )
    return RouteDecision(
        model_id=payload["model_id"],
        provider=payload["provider"],
        route=payload["route"],
        client_type=payload["client_type"],
        live_request_allowed=bool(payload["live_request_allowed"]),
        formal_pool_allowed=bool(payload["formal_pool_allowed"]),
        native_attestation_allowed=bool(payload["native_attestation_allowed"]),
        provider_owner=payload["provider_owner"],
        credential_scope=payload["credential_scope"],
        gateway_location=payload["gateway_location"],
        runtime_hash=payload["runtime_hash"],
        overlay_hash=payload["overlay_hash"],
        catalog_hash=payload["catalog_hash"],
        catalog_version=payload["catalog_version"],
        session_ref=payload["session_ref"],
        nonce=payload["nonce"],
    )


def body_model_id(body: bytes) -> str:
    try:
        payload = json.loads(body.decode("utf-8")) if body else {}
    except Exception as exc:  # noqa: BLE001
        raise RuntimeError("route hint body must be valid JSON") from exc
    if not isinstance(payload, dict):
        raise RuntimeError("route hint body must be a JSON object")
    model = payload.get("model")
    if not isinstance(model, str) or not model.strip():
        raise RuntimeError("route hint body model is required")
    return model.strip()


def _normalize_hash(value: str, field: str) -> str:
    normalized = str(value).strip().lower()
    if not _HASH_RE.fullmatch(normalized):
        raise RuntimeError(f"{field} must be sha256:<64hex>")
    return normalized


def _header(headers: Mapping[str, str], name: str) -> str | None:
    for key, value in headers.items():
        if key.lower() == name.lower():
            return value
    return None


def _encode_payload(payload: Mapping[str, Any]) -> str:
    raw = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return base64.urlsafe_b64encode(raw).decode("ascii").rstrip("=")


def _decode_payload(encoded: str) -> dict[str, Any]:
    try:
        raw = base64.urlsafe_b64decode(encoded + ("=" * (-len(encoded) % 4)))
        payload = json.loads(raw.decode("utf-8"))
    except Exception as exc:  # noqa: BLE001
        raise RuntimeError("route hint payload decode failed") from exc
    if not isinstance(payload, dict):
        raise RuntimeError("route hint payload must be an object")
    return payload


def _sign_route_hint(encoded: str, method: str, request_path: str, body: bytes, secret: str) -> str:
    material = b"\n".join([
        encoded.encode("ascii"),
        method.upper().encode("ascii"),
        request_path.encode("utf-8"),
        sha256(body).hexdigest().encode("ascii"),
    ])
    digest = hmac.new(secret.encode("utf-8"), material, sha256).digest()
    return base64.urlsafe_b64encode(digest).decode("ascii").rstrip("=")


def _validate_payload_shape(payload: Mapping[str, Any]) -> None:
    expected = {
        "key_id", "scope", "version", "issued_at", "expires_at", "nonce",
        "method", "request_uri", "model_id", "body_model", "body_sha256",
        "runtime_hash", "overlay_hash", "catalog_hash", "catalog_version",
        "session_ref", "route", "client_type", "provider", "live_request_allowed",
        "formal_pool_allowed", "native_attestation_allowed", "provider_owner",
        "credential_scope", "gateway_location",
    }
    if set(payload.keys()) != expected:
        raise RuntimeError("route hint payload shape mismatch")
    if payload["scope"] != ROUTE_HINT_SCOPE or payload["version"] != ROUTE_HINT_VERSION:
        raise RuntimeError("route hint scope/version mismatch")
    for key in (
        "key_id", "nonce", "method", "request_uri", "model_id", "body_model",
        "body_sha256", "runtime_hash", "overlay_hash", "catalog_hash",
        "catalog_version", "session_ref", "route", "client_type", "provider",
        "provider_owner", "credential_scope", "gateway_location",
    ):
        if not isinstance(payload[key], str) or not payload[key]:
            raise RuntimeError(f"route hint {key} is invalid")
    for key in ("issued_at", "expires_at"):
        if not isinstance(payload[key], int):
            raise RuntimeError(f"route hint {key} is invalid")
    for key in ("live_request_allowed", "formal_pool_allowed", "native_attestation_allowed"):
        if not isinstance(payload[key], bool):
            raise RuntimeError(f"route hint {key} is invalid")
    for key in ("body_sha256", "runtime_hash", "overlay_hash", "catalog_hash"):
        if not _HASH_RE.fullmatch(payload[key]):
            raise RuntimeError(f"route hint {key} is invalid")


__all__ = [
    "ROUTE_HINT_HEADER",
    "ROUTE_HINT_SIGNATURE_HEADER",
    "RouteCatalog",
    "RouteCatalogEntry",
    "RouteDecision",
    "RouteHintReplayCache",
    "body_model_id",
    "build_signed_route_hint_headers",
    "cp4_fixture_route_catalog",
    "verify_signed_route_hint_headers",
]
