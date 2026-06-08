#!/usr/bin/env python3
"""Trusted local guard attestation for control-plane intents."""
from __future__ import annotations

from dataclasses import dataclass
from hashlib import sha256
from typing import Any, Callable, Mapping
import base64
import hmac
import json
import os
import secrets
import time

from tools.cli_session_budget import session_key_from_headers


ATTESTATION_SCOPE = "control_plane_intent"
ATTESTATION_HEADER = "x-sub2api-control-plane-attestation"
ATTESTATION_SIGNATURE_HEADER = "x-sub2api-control-plane-signature"


class AttestationValidationError(ValueError):
    """Raised when a guard attestation is missing, invalid, or replayed."""


@dataclass(frozen=True)
class GuardAttestationConfig:
    current_key_id: str
    keys: dict[str, str]
    scope: str = ATTESTATION_SCOPE
    version: int = 1
    nonce_ttl_seconds: int = 120
    clock_skew_seconds: int = 30


class NonceReplayCache:
    def __init__(self, ttl_seconds: int = 120, now_fn: Callable[[], int] | None = None):
        self.ttl_seconds = ttl_seconds
        self.now_fn = now_fn or (lambda: int(time.time()))
        self._entries: dict[str, int] = {}

    def check_and_record(self, *, key_id: str, scope: str, nonce: str, now: int | None = None) -> None:
        current = int(self.now_fn() if now is None else now)
        self._evict(current)
        replay_key = f"{scope}:{key_id}:{nonce}"
        expiry = self._entries.get(replay_key)
        if expiry is not None and expiry > current:
            raise AttestationValidationError("attestation nonce replayed")
        self._entries[replay_key] = current + self.ttl_seconds

    def _evict(self, current: int) -> None:
        expired = [key for key, expiry in self._entries.items() if expiry <= current]
        for key in expired:
            self._entries.pop(key, None)


def guard_attestation_config_from_env() -> GuardAttestationConfig:
    current_key_id = os.environ.get("SUB2API_CONTROL_PLANE_ATTESTATION_CURRENT_KEY_ID", "guard_v1")
    keys_json = os.environ.get("SUB2API_CONTROL_PLANE_ATTESTATION_KEYS_JSON")
    if keys_json:
        keys = json.loads(keys_json)
        if not isinstance(keys, dict) or not keys:
            raise AttestationValidationError("attestation key set must be a non-empty mapping")
        normalized_keys = {str(key): str(value) for key, value in keys.items() if str(key) and str(value)}
    else:
        secret = os.environ.get("SUB2API_CONTROL_PLANE_ATTESTATION_SECRET", "sub2api-control-plane-attestation-dev-key")
        normalized_keys = {current_key_id: secret}
    return GuardAttestationConfig(
        current_key_id=current_key_id,
        keys=normalized_keys,
        scope=os.environ.get("SUB2API_CONTROL_PLANE_ATTESTATION_SCOPE", ATTESTATION_SCOPE),
        version=int(os.environ.get("SUB2API_CONTROL_PLANE_ATTESTATION_VERSION", "1")),
        nonce_ttl_seconds=int(os.environ.get("SUB2API_CONTROL_PLANE_ATTESTATION_NONCE_TTL_SECONDS", "120")),
        clock_skew_seconds=int(os.environ.get("SUB2API_CONTROL_PLANE_ATTESTATION_CLOCK_SKEW_SECONDS", "30")),
    )


def build_guard_attestation(
    intent: Mapping[str, Any],
    *,
    request_headers: Mapping[str, str] | None = None,
    config: GuardAttestationConfig | None = None,
    now: int | None = None,
    nonce: str | None = None,
    key_id: str | None = None,
) -> tuple[str, str]:
    cfg = config or guard_attestation_config_from_env()
    selected_key_id = key_id or cfg.current_key_id
    secret = cfg.keys.get(selected_key_id)
    if not secret:
        raise AttestationValidationError("attestation key id is not configured")
    issued_at = int(time.time() if now is None else now)
    attestation_payload = {
        "key_id": selected_key_id,
        "scope": cfg.scope,
        "version": cfg.version,
        "issued_at": issued_at,
        "nonce": nonce or secrets.token_hex(16),
        "method": intent.get("method"),
        "path_template": intent.get("path_template"),
        "normalized_query": _normalized_string_map(intent.get("normalized_query")),
        "classification": intent.get("classification"),
        "routing_intent": intent.get("routing_intent"),
        "policy_version": intent.get("policy_version"),
        "strategy_version": intent.get("strategy_version"),
        "response_schema_version": intent.get("response_schema_version"),
        "body_length_bucket": intent.get("body_length_bucket"),
        "body_omitted_reason": intent.get("body_omitted_reason"),
        "digest_omitted_reason": intent.get("digest_omitted_reason"),
        "schema_summary": _normalized_safe_json_value(intent.get("schema_summary")),
        "query_ref": _normalized_query_ref(intent.get("query_ref")),
        "query_omitted_reason": intent.get("query_omitted_reason"),
        "session_ref": session_key_from_headers(request_headers or {}),
    }
    encoded = _encode_attestation_payload(attestation_payload)
    signature = _sign_attestation(encoded, secret)
    return encoded, signature


def verify_guard_attestation(
    intent: Mapping[str, Any],
    attestation: str | None,
    signature: str | None,
    *,
    config: GuardAttestationConfig | None = None,
    nonce_cache: NonceReplayCache | None = None,
    now: int | None = None,
) -> dict[str, Any]:
    if not attestation or not signature:
        raise AttestationValidationError("guard attestation is required")
    cfg = config or guard_attestation_config_from_env()
    payload = _decode_attestation_payload(attestation)
    _validate_payload_shape(payload)
    if payload["scope"] != cfg.scope:
        raise AttestationValidationError("attestation scope mismatch")
    if payload["version"] != cfg.version:
        raise AttestationValidationError("attestation version mismatch")
    secret = cfg.keys.get(payload["key_id"])
    if not secret:
        raise AttestationValidationError("attestation key id is not configured")
    expected = _sign_attestation(attestation, secret)
    if not hmac.compare_digest(expected, signature):
        raise AttestationValidationError("attestation signature mismatch")

    current = int(time.time() if now is None else now)
    issued_at = int(payload["issued_at"])
    if abs(current - issued_at) > cfg.clock_skew_seconds:
        raise AttestationValidationError("attestation timestamp is outside the clock skew window")
    if issued_at < current - cfg.nonce_ttl_seconds:
        raise AttestationValidationError("attestation timestamp expired")

    cache = nonce_cache or NonceReplayCache(ttl_seconds=cfg.nonce_ttl_seconds)
    cache.check_and_record(key_id=payload["key_id"], scope=payload["scope"], nonce=payload["nonce"], now=current)

    expected_payload = {
        "method": intent.get("method"),
        "path_template": intent.get("path_template"),
        "normalized_query": _normalized_string_map(intent.get("normalized_query")),
        "classification": intent.get("classification"),
        "routing_intent": intent.get("routing_intent"),
        "policy_version": intent.get("policy_version"),
        "strategy_version": intent.get("strategy_version"),
        "response_schema_version": intent.get("response_schema_version"),
        "body_length_bucket": intent.get("body_length_bucket"),
        "body_omitted_reason": intent.get("body_omitted_reason"),
        "digest_omitted_reason": intent.get("digest_omitted_reason"),
        "schema_summary": _normalized_safe_json_value(intent.get("schema_summary")),
        "query_ref": _normalized_query_ref(intent.get("query_ref")),
        "query_omitted_reason": intent.get("query_omitted_reason"),
    }
    for field, expected_value in expected_payload.items():
        if payload.get(field) != expected_value:
            raise AttestationValidationError(f"attestation payload mismatch for {field}")
    _validate_session_ref(payload.get("session_ref"))
    return payload


def _normalized_query_ref(value: Any) -> Any:
    if value is None:
        return None
    if not isinstance(value, Mapping):
        raise AttestationValidationError("query_ref must be a mapping")
    return {
        "key_id": value.get("key_id"),
        "scope": value.get("scope"),
        "version": value.get("version"),
        "value": value.get("value"),
    }


def _normalized_string_map(value: Any) -> dict[str, str]:
    if value is None:
        return {}
    if not isinstance(value, Mapping):
        raise AttestationValidationError("normalized_query must be a mapping")
    return {str(key): str(item) for key, item in value.items()}


def _normalized_safe_json_value(value: Any) -> Any:
    return json.loads(json.dumps(value, sort_keys=True, separators=(",", ":")))


def _validate_session_ref(value: Any) -> None:
    if not isinstance(value, Mapping):
        raise AttestationValidationError("attestation session_ref must be a mapping")
    if value.get("scope") != "session_budget_session":
        raise AttestationValidationError("attestation session_ref scope mismatch")
    if not isinstance(value.get("version"), int) or value["version"] <= 0:
        raise AttestationValidationError("attestation session_ref version is invalid")
    if not isinstance(value.get("value"), str) or not value["value"].startswith("hmac-sha256:"):
        raise AttestationValidationError("attestation session_ref value is invalid")


def _validate_payload_shape(payload: Mapping[str, Any]) -> None:
    expected = {
        "key_id",
        "scope",
        "version",
        "issued_at",
        "nonce",
        "method",
        "path_template",
        "normalized_query",
        "classification",
        "routing_intent",
        "policy_version",
        "strategy_version",
        "response_schema_version",
        "body_length_bucket",
        "body_omitted_reason",
        "digest_omitted_reason",
        "schema_summary",
        "query_ref",
        "query_omitted_reason",
        "session_ref",
    }
    if set(payload.keys()) != expected:
        raise AttestationValidationError("attestation payload shape mismatch")
    if not isinstance(payload["nonce"], str) or not payload["nonce"]:
        raise AttestationValidationError("attestation nonce is invalid")
    if not isinstance(payload["issued_at"], int):
        raise AttestationValidationError("attestation issued_at is invalid")


def _encode_attestation_payload(payload: Mapping[str, Any]) -> str:
    encoded = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return base64.urlsafe_b64encode(encoded).decode("ascii").rstrip("=")


def _decode_attestation_payload(encoded: str) -> dict[str, Any]:
    padding = "=" * (-len(encoded) % 4)
    raw = base64.urlsafe_b64decode(encoded + padding)
    payload = json.loads(raw.decode("utf-8"))
    if not isinstance(payload, dict):
        raise AttestationValidationError("attestation payload must decode to an object")
    return payload


def _sign_attestation(encoded_payload: str, secret: str) -> str:
    signature = hmac.new(secret.encode("utf-8"), encoded_payload.encode("ascii"), sha256).hexdigest()
    return f"hmac-sha256:{signature}"


__all__ = [
    "ATTESTATION_HEADER",
    "ATTESTATION_SIGNATURE_HEADER",
    "AttestationValidationError",
    "GuardAttestationConfig",
    "NonceReplayCache",
    "build_guard_attestation",
    "guard_attestation_config_from_env",
    "verify_guard_attestation",
]
