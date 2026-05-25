#!/usr/bin/env python3
"""Strict safe control-plane intent envelopes with no plain body/query digests."""
from __future__ import annotations

from hashlib import sha256
from typing import Mapping, Any
from urllib.parse import parse_qsl, urlsplit
import hmac
import json
import os
import re


_ALLOWED_TOP_LEVEL_FIELDS = {
    "method",
    "path_template",
    "normalized_query",
    "query_ref",
    "query_omitted_reason",
    "classification",
    "policy_version",
    "strategy_version",
    "response_schema_version",
    "routing_intent",
    "body_length_bucket",
    "schema_summary",
    "body_omitted_reason",
    "digest_omitted_reason",
    "redaction_proof",
}
_FORBIDDEN_FIELDS = {
    "account_uuid",
    "authorization",
    "body",
    "body_hash",
    "cch",
    "cookie",
    "email",
    "org_uuid",
    "prompt",
    "proxy_credential",
    "query_hash",
    "raw_body",
    "raw_cch",
    "raw_prompt",
    "raw_query",
    "raw_telemetry",
    "telemetry",
    "user_uuid",
    "x_api_key",
}
_ALLOWED_QUERY_RULES = {
    "/api/claude_cli/bootstrap": {
        "entrypoint": "enum:sdk-cli",
    },
    "/v1/mcp_servers": {
        "limit": "int:1:1000",
    },
}
_COLLECTION_PLACEHOLDERS = {
    "accounts": "account",
    "organizations": "org",
    "users": "user",
}
_ALLOWED_PLACEHOLDERS = {"account", "org", "user"}
_ALLOWED_BODY_LENGTH_BUCKETS = {
    "empty",
    "1_255_bytes",
    "256_1023_bytes",
    "1024_4095_bytes",
    "4096_16383_bytes",
    "16384_plus_bytes",
}
_ALLOWED_QUERY_OMITTED_REASONS = {"no_query"}
_ALLOWED_BODY_OMITTED_REASONS = {"not_applicable", "high_risk_body_not_retained", "empty_high_risk_body"}
_ALLOWED_DIGEST_OMITTED_REASONS = {"not_applicable", "raw_body_digest_forbidden_by_policy"}
_FORBIDDEN_CONTROL_PLANE_HEADERS = {
    "x-sub2api-control-plane-attestation",
    "x-sub2api-control-plane-intent",
    "x-sub2api-control-plane-signature",
    "x-sub2api-control-plane-token",
    "x-anthropic-billing-header",
    "x-sub2api-canary-billing-cch-mode",
}
_HMAC_PREFIX = "hmac-sha256:"
_HMAC_RE = re.compile(r"^hmac-sha256:[0-9a-f]{64}$")
_MD5_RE = re.compile(r"^md5:[0-9a-f]{32}$", re.IGNORECASE)
_PLAIN_SHA_RE = re.compile(r"^sha(?:1|224|256|384|512):[0-9a-f]{40,128}$", re.IGNORECASE)
_SAFE_HEADER_NAME_RE = re.compile(r"^[A-Za-z0-9-]+$")
_SAFE_IDENTIFIER_RE = re.compile(r"^[a-z][a-z0-9_]*$")
_SAFE_REF_IDENTIFIER_RE = re.compile(r"^[a-z0-9][a-z0-9_-]*$")
_NON_ALNUM_RE = re.compile(r"[^a-z0-9]+")
_UUID_RE = re.compile(
    r"^(?:[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}|[0-9a-fA-F]{32})$"
)
_EMAIL_RE = re.compile(r"[^@\s]+@[^@\s]+\.[^@\s]+")


class IntentValidationError(ValueError):
    """Raised when a control-plane intent envelope is unsafe or invalid."""


def build_control_plane_intent(
    *,
    method: str,
    path: str,
    headers: Mapping[str, str] | None,
    body: bytes | bytearray | memoryview | str | None,
    classification: str,
    policy_version: int,
    strategy_version: int,
    response_schema_version: int,
    routing_intent: str = "local_stub_or_suppress",
) -> dict[str, Any]:
    normalized_method = _normalize_method(method)
    parsed = urlsplit(path)
    path_template = _template_path(parsed.path)
    _reject_forbidden_transport_headers(headers or {})
    normalized_query, query_ref, query_omitted_reason = _build_query_fields(path_template, parsed.query)
    body_bytes = _coerce_body_bytes(body)
    body_length_bucket = _bucket_body_length(len(body_bytes))
    if normalized_method == "GET" and body_bytes:
        raise IntentValidationError("GET control-plane bodies are forbidden")
    if normalized_method == "POST":
        body_omitted_reason = "high_risk_body_not_retained" if body_bytes else "empty_high_risk_body"
        digest_omitted_reason = "raw_body_digest_forbidden_by_policy"
        schema_summary = _build_schema_summary(body_bytes)
    else:
        body_omitted_reason = "not_applicable"
        digest_omitted_reason = "not_applicable"
        schema_summary = {"content_kind": "none", "top_level_type": "none"}

    envelope = {
        "method": normalized_method,
        "path_template": path_template,
        "normalized_query": normalized_query,
        "query_ref": query_ref,
        "query_omitted_reason": query_omitted_reason,
        "classification": _validate_safe_identifier(classification, "classification"),
        "policy_version": _require_positive_int(policy_version, "policy_version"),
        "strategy_version": _require_positive_int(strategy_version, "strategy_version"),
        "response_schema_version": _require_positive_int(response_schema_version, "response_schema_version"),
        "routing_intent": _validate_safe_identifier(routing_intent, "routing_intent"),
        "body_length_bucket": body_length_bucket,
        "schema_summary": schema_summary,
        "body_omitted_reason": body_omitted_reason,
        "digest_omitted_reason": digest_omitted_reason,
        "redaction_proof": {
            "sensitive_scan": "clean",
            "path_identifiers_redacted": path_template != parsed.path,
            "raw_query_persisted": False,
            "body_persisted": False,
            "raw_body_digest_persisted": False,
        },
    }
    validate_control_plane_intent(envelope)
    return envelope


def validate_control_plane_intent(envelope: Mapping[str, Any]) -> None:
    if not isinstance(envelope, Mapping):
        raise IntentValidationError("intent envelope must be a mapping")

    keys = set(envelope.keys())
    forbidden = keys & _FORBIDDEN_FIELDS
    if forbidden:
        raise IntentValidationError(f"forbidden fields present: {sorted(forbidden)}")
    if keys != _ALLOWED_TOP_LEVEL_FIELDS:
        missing = sorted(_ALLOWED_TOP_LEVEL_FIELDS - keys)
        extra = sorted(keys - _ALLOWED_TOP_LEVEL_FIELDS)
        raise IntentValidationError(f"intent envelope keys must match allowlist; missing={missing}, extra={extra}")

    path_template = envelope["path_template"]
    normalized_query = envelope["normalized_query"]
    _normalize_method(envelope["method"])
    _validate_path_template(path_template)
    _validate_query_fields(path_template, normalized_query, envelope["query_ref"], envelope["query_omitted_reason"])
    _validate_safe_identifier(envelope["classification"], "classification")
    _require_positive_int(envelope["policy_version"], "policy_version")
    _require_positive_int(envelope["strategy_version"], "strategy_version")
    _require_positive_int(envelope["response_schema_version"], "response_schema_version")
    _validate_safe_identifier(envelope["routing_intent"], "routing_intent")
    _validate_body_metadata(envelope["body_length_bucket"], envelope["schema_summary"], envelope["body_omitted_reason"], envelope["digest_omitted_reason"])
    _validate_redaction_proof(envelope["redaction_proof"])


def _normalize_method(method: Any) -> str:
    if not isinstance(method, str) or not method.strip():
        raise IntentValidationError("method must be a non-empty string")
    normalized = method.upper()
    if normalized not in {"GET", "POST"}:
        raise IntentValidationError("method must be GET or POST")
    return normalized


def _template_path(path: str) -> str:
    if not isinstance(path, str) or not path.startswith("/"):
        raise IntentValidationError("path must be an absolute path")
    parts = path.split("/")
    templated = [""]
    for index, segment in enumerate(parts[1:], start=1):
        if not segment:
            templated.append(segment)
            continue
        previous = parts[index - 1]
        placeholder = _COLLECTION_PLACEHOLDERS.get(previous)
        if placeholder is not None:
            templated.append(f"{{{placeholder}}}")
            continue
        if _looks_sensitive_identifier(segment):
            raise IntentValidationError("sensitive path segment is forbidden")
        if _looks_like_unsafe_dynamic_identifier(segment):
            raise IntentValidationError("unsafe dynamic path identifier cannot be templated safely")
        templated.append(segment)
    return "/".join(templated)


def _build_query_fields(path_template: str, raw_query: str) -> tuple[dict[str, str], dict[str, Any] | None, str | None]:
    if not raw_query:
        return {}, None, "no_query"
    allowlist = _ALLOWED_QUERY_RULES.get(path_template)
    if allowlist is None:
        raise IntentValidationError("query parameters are not allowed for this path")
    pairs = parse_qsl(raw_query, keep_blank_values=True, strict_parsing=False)
    normalized: dict[str, str] = {}
    for key, value in pairs:
        if key in normalized:
            raise IntentValidationError("repeated query parameters are forbidden")
        rule = allowlist.get(key)
        if rule is None:
            raise IntentValidationError("query parameters must be path-allowlisted")
        normalized[key] = _normalize_query_value(key, value, rule)
    if not normalized:
        raise IntentValidationError("query parameters must not collapse to empty state")
    payload = json.dumps(normalized, ensure_ascii=True, separators=(",", ":"), sort_keys=True).encode("utf-8")
    return normalized, _build_scoped_hmac_ref(payload, scope="control_plane_query"), None


def _normalize_query_value(key: str, value: str, rule: str) -> str:
    candidate = value.strip()
    if _looks_sensitive_text(candidate):
        raise IntentValidationError("query parameters must not contain sensitive markers")
    if rule.startswith("enum:"):
        allowed = set(rule.split(":", 1)[1].split("|"))
        if candidate not in allowed:
            raise IntentValidationError(f"query parameter {key!r} is not allowlisted")
        return candidate
    if rule.startswith("int:"):
        _, lower_raw, upper_raw = rule.split(":", 2)
        if not candidate.isdigit():
            raise IntentValidationError(f"query parameter {key!r} must be decimal digits")
        parsed = int(candidate)
        lower = int(lower_raw)
        upper = int(upper_raw)
        if parsed < lower or parsed > upper:
            raise IntentValidationError(f"query parameter {key!r} is outside the allowlisted range")
        return str(parsed)
    raise IntentValidationError(f"unsupported query allowlist rule for {key!r}")


def _validate_path_template(path_template: Any) -> None:
    if not isinstance(path_template, str) or not path_template.startswith("/"):
        raise IntentValidationError("path_template must be an absolute path string")
    for segment in path_template.split("/")[1:]:
        if not segment:
            continue
        if segment.startswith("{") and segment.endswith("}"):
            placeholder = segment[1:-1]
            if placeholder not in _ALLOWED_PLACEHOLDERS:
                raise IntentValidationError(f"invalid path placeholder: {placeholder!r}")
            continue
        if _looks_sensitive_identifier(segment):
            raise IntentValidationError("sensitive literal leaked into path_template")
        if _looks_like_unsafe_dynamic_identifier(segment):
            raise IntentValidationError("raw dynamic identifier leaked into path_template")


def _validate_query_fields(
    path_template: Any,
    normalized_query: Any,
    query_ref: Any,
    query_omitted_reason: Any,
) -> None:
    if not isinstance(normalized_query, dict):
        raise IntentValidationError("normalized_query must be a dict")
    allowlist = _ALLOWED_QUERY_RULES.get(path_template)
    if normalized_query:
        if allowlist is None:
            raise IntentValidationError("normalized_query is not allowed for this path")
        for key, value in normalized_query.items():
            rule = allowlist.get(key)
            if not isinstance(key, str) or rule is None:
                raise IntentValidationError("normalized_query contains a non-allowlisted key")
            if not isinstance(value, str):
                raise IntentValidationError("normalized_query values must be strings")
            if _looks_sensitive_text(value):
                raise IntentValidationError("normalized_query contains sensitive content")
            _normalize_query_value(key, value, rule)
        _validate_scoped_hmac_ref(query_ref, scope="control_plane_query")
        if query_omitted_reason is not None:
            raise IntentValidationError("query_omitted_reason must be omitted when normalized_query is present")
        return
    if query_ref is not None:
        raise IntentValidationError("query_ref must be omitted when normalized_query is empty")
    if query_omitted_reason not in _ALLOWED_QUERY_OMITTED_REASONS:
        raise IntentValidationError("query_omitted_reason must be a supported safe reason")


def _validate_body_metadata(
    body_length_bucket: Any,
    schema_summary: Any,
    body_omitted_reason: Any,
    digest_omitted_reason: Any,
) -> None:
    if body_length_bucket not in _ALLOWED_BODY_LENGTH_BUCKETS:
        raise IntentValidationError("body_length_bucket must be a supported bucket")
    if body_omitted_reason not in _ALLOWED_BODY_OMITTED_REASONS:
        raise IntentValidationError("body_omitted_reason must be a supported safe reason")
    if digest_omitted_reason not in _ALLOWED_DIGEST_OMITTED_REASONS:
        raise IntentValidationError("digest_omitted_reason must be a supported safe reason")
    if body_length_bucket == "empty" and body_omitted_reason not in {"not_applicable", "empty_high_risk_body"}:
        raise IntentValidationError("empty bodies must not claim retained high-risk content")
    _validate_safe_json_structure(schema_summary, field_name="schema_summary")


def _validate_redaction_proof(redaction_proof: Any) -> None:
    if not isinstance(redaction_proof, dict):
        raise IntentValidationError("redaction_proof must be a dict")
    expected_keys = {
        "sensitive_scan",
        "path_identifiers_redacted",
        "raw_query_persisted",
        "body_persisted",
        "raw_body_digest_persisted",
    }
    if set(redaction_proof.keys()) != expected_keys:
        raise IntentValidationError("redaction_proof keys must match expected safe fields")
    if redaction_proof["sensitive_scan"] != "clean":
        raise IntentValidationError("redaction_proof.sensitive_scan must be 'clean'")
    for key in ("path_identifiers_redacted", "raw_query_persisted", "body_persisted", "raw_body_digest_persisted"):
        if not isinstance(redaction_proof[key], bool):
            raise IntentValidationError(f"redaction_proof.{key} must be bool")
    if redaction_proof["raw_query_persisted"]:
        raise IntentValidationError("redaction_proof must assert raw_query_persisted is false")
    if redaction_proof["body_persisted"]:
        raise IntentValidationError("redaction_proof must assert body_persisted is false")
    if redaction_proof["raw_body_digest_persisted"]:
        raise IntentValidationError("redaction_proof must assert raw_body_digest_persisted is false")


def _build_schema_summary(body: bytes) -> dict[str, Any]:
    if not body:
        return {"content_kind": "none", "top_level_type": "none"}
    try:
        payload = json.loads(body.decode("utf-8"))
    except Exception:
        return {
            "content_kind": "non_json_bytes",
            "top_level_type": "bytes",
            "size_bucket": _bucket_body_length(len(body)),
        }
    return _safe_json_shape(payload)


def _safe_json_shape(value: Any) -> dict[str, Any]:
    if isinstance(value, dict):
        top_level_keys = sorted({_sanitize_body_key(key) for key in value.keys()})
        value_types = {
            _sanitize_body_key(key): _json_type_name(item)
            for key, item in value.items()
        }
        nested_object_keys = {
            _sanitize_body_key(key): sorted({_sanitize_body_key(child) for child in item.keys()})
            for key, item in value.items()
            if isinstance(item, dict)
        }
        array_fields = sorted(
            _sanitize_body_key(key)
            for key, item in value.items()
            if isinstance(item, list)
        )
        summary: dict[str, Any] = {
            "content_kind": "json",
            "top_level_type": "object",
            "top_level_keys": top_level_keys,
            "value_types": value_types,
        }
        if nested_object_keys:
            summary["nested_object_keys"] = nested_object_keys
        if array_fields:
            summary["array_fields"] = array_fields
        return summary
    if isinstance(value, list):
        return {
            "content_kind": "json",
            "top_level_type": "array",
            "length_bucket": _bucket_collection_length(len(value)),
            "element_types": sorted({_json_type_name(item) for item in value}),
        }
    return {
        "content_kind": "json",
        "top_level_type": _json_type_name(value),
    }


def _validate_safe_json_structure(value: Any, *, field_name: str) -> None:
    if isinstance(value, dict):
        for key, item in value.items():
            if not isinstance(key, str):
                raise IntentValidationError(f"{field_name} keys must be strings")
            if _looks_sensitive_text(key) or _looks_plain_digest(key) or _looks_like_unsafe_dynamic_identifier(key):
                raise IntentValidationError(f"{field_name} contains sensitive content")
            _validate_safe_json_structure(item, field_name=field_name)
        return
    if isinstance(value, list):
        for item in value:
            _validate_safe_json_structure(item, field_name=field_name)
        return
    if value is None or isinstance(value, (int, float, bool)):
        return
    if isinstance(value, str):
        if _looks_sensitive_text(value) or _looks_plain_digest(value) or _looks_like_unsafe_dynamic_identifier(value):
            raise IntentValidationError(f"{field_name} contains sensitive content")
        return
    raise IntentValidationError(f"{field_name} must contain JSON-serializable safe values")


def _build_scoped_hmac_ref(payload: bytes, *, scope: str) -> dict[str, Any]:
    key_id = os.environ.get("SUB2API_CONTROL_PLANE_HMAC_KEY_ID", "local_guard_v1")
    version = int(os.environ.get("SUB2API_CONTROL_PLANE_HMAC_VERSION", "1"))
    secret = os.environ.get("SUB2API_CONTROL_PLANE_HMAC_KEY", "sub2api-control-plane-dev-key")
    material = scope.encode("utf-8") + b"\x00v" + str(version).encode("ascii") + b"\x00" + payload
    mac = hmac.new(secret.encode("utf-8"), material, sha256).hexdigest()
    return {
        "key_id": key_id,
        "scope": scope,
        "version": version,
        "value": f"{_HMAC_PREFIX}{mac}",
    }


def _validate_scoped_hmac_ref(value: Any, *, scope: str) -> None:
    if not isinstance(value, dict):
        raise IntentValidationError("query_ref must be a dict")
    if set(value.keys()) != {"key_id", "scope", "version", "value"}:
        raise IntentValidationError("query_ref keys must match the scoped HMAC schema")
    key_id = value.get("key_id")
    if not isinstance(key_id, str) or _SAFE_REF_IDENTIFIER_RE.fullmatch(key_id) is None or _looks_sensitive_text(key_id):
        raise IntentValidationError("query_ref.key_id must be a safe identifier")
    if value.get("scope") != scope:
        raise IntentValidationError("query_ref.scope must match the expected scope")
    if not isinstance(value.get("version"), int) or value["version"] <= 0:
        raise IntentValidationError("query_ref.version must be a positive int")
    ref_value = value.get("value")
    if not isinstance(ref_value, str) or _HMAC_RE.fullmatch(ref_value) is None:
        raise IntentValidationError("query_ref.value must be a scoped hmac-sha256 reference")


def _bucket_body_length(size: int) -> str:
    if size <= 0:
        return "empty"
    if size <= 255:
        return "1_255_bytes"
    if size <= 1023:
        return "256_1023_bytes"
    if size <= 4095:
        return "1024_4095_bytes"
    if size <= 16383:
        return "4096_16383_bytes"
    return "16384_plus_bytes"


def _bucket_collection_length(size: int) -> str:
    if size <= 0:
        return "empty"
    if size == 1:
        return "1"
    if size <= 4:
        return "2_4"
    if size <= 16:
        return "5_16"
    return "17_plus"


def _coerce_body_bytes(body: bytes | bytearray | memoryview | str | None) -> bytes:
    if body is None:
        return b""
    if isinstance(body, bytes):
        return body
    if isinstance(body, bytearray):
        return bytes(body)
    if isinstance(body, memoryview):
        return body.tobytes()
    if isinstance(body, str):
        return body.encode("utf-8")
    raise IntentValidationError("body must be bytes-like, string, or None")


def _reject_forbidden_transport_headers(headers: Mapping[str, str]) -> None:
    for key in headers.keys():
        if not isinstance(key, str):
            raise IntentValidationError("header names must be strings")
        if _SAFE_HEADER_NAME_RE.fullmatch(key) is None:
            raise IntentValidationError("header names must match the safe header schema")
        normalized = key.strip().lower()
        if normalized in _FORBIDDEN_CONTROL_PLANE_HEADERS or "cch" in normalized:
            raise IntentValidationError("control-plane transport markers must be stripped or rejected")


def _sanitize_body_key(key: object) -> str:
    if not isinstance(key, str):
        return "redacted_key"
    normalized = key.strip()
    if not normalized:
        return "redacted_key"
    lowered = normalized.lower()
    if _looks_sensitive_text(lowered) or _looks_like_unsafe_dynamic_identifier(normalized):
        return "redacted_key"
    return re.sub(r"[^a-z0-9_]+", "_", lowered).strip("_") or "redacted_key"


def _json_type_name(value: Any) -> str:
    if value is None:
        return "null"
    if isinstance(value, bool):
        return "bool"
    if isinstance(value, int) and not isinstance(value, bool):
        return "int"
    if isinstance(value, float):
        return "float"
    if isinstance(value, str):
        return "string"
    if isinstance(value, list):
        return "array"
    if isinstance(value, dict):
        return "object"
    return "other"


def _require_non_empty_string(value: Any, field_name: str) -> str:
    if not isinstance(value, str) or not value.strip():
        raise IntentValidationError(f"{field_name} must be a non-empty string")
    return value


def _validate_safe_identifier(value: Any, field_name: str) -> str:
    normalized = _require_non_empty_string(value, field_name)
    if _SAFE_IDENTIFIER_RE.fullmatch(normalized) is None:
        raise IntentValidationError(f"{field_name} must be a normalized safe identifier")
    if _looks_sensitive_text(normalized) or _looks_plain_digest(normalized):
        raise IntentValidationError(f"{field_name} contains sensitive content")
    return normalized


def _require_positive_int(value: Any, field_name: str) -> int:
    if not isinstance(value, int) or value <= 0:
        raise IntentValidationError(f"{field_name} must be a positive int")
    return value


def _looks_like_unsafe_dynamic_identifier(value: str) -> bool:
    lowered = value.lower()
    if _UUID_RE.fullmatch(value):
        return True
    if _EMAIL_RE.search(value) is not None:
        return True
    if lowered.startswith(("local-org-", "local-account-", "local-user-")):
        return True
    if any(marker in lowered for marker in ("org-secret", "account-secret", "user-secret")):
        return True
    if re.match(r"^(?:account|org|organization|user|session|project)(?:[_-].+)$", lowered):
        return True
    return False


def _looks_plain_digest(value: str) -> bool:
    return _PLAIN_SHA_RE.fullmatch(value) is not None or _MD5_RE.fullmatch(value) is not None


def _looks_sensitive_text(value: str) -> bool:
    if _EMAIL_RE.search(value) is not None:
        return True
    return _contains_sensitive_marker(value)


def _looks_sensitive_identifier(value: str) -> bool:
    if _EMAIL_RE.search(value) is not None:
        return True
    return _contains_sensitive_marker(value)


def _contains_sensitive_marker(value: str) -> bool:
    lowered = value.lower()
    if lowered.startswith("sk-"):
        return True
    parts = tuple(part for part in _NON_ALNUM_RE.split(lowered) if part)
    joined = "".join(parts)
    sensitive_parts = {"prompt", "token", "secret", "cookie", "credential", "authorization"}
    if any(part in sensitive_parts for part in parts):
        return True
    if "rawprompt" in joined or "accesstoken" in joined or "xapikey" in joined:
        return True
    return False


__all__ = [
    "IntentValidationError",
    "build_control_plane_intent",
    "validate_control_plane_intent",
]
