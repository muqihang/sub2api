#!/usr/bin/env python3
"""Read-only Phase 3A adapter over the existing safe Sub2API oracles.

The adapter classifies caller-supplied synthetic or already-normalized data. It
does not start collectors, probes, a Claude Code binary, or any network client.
"""

from __future__ import annotations

import json
import math
import re
import sys
from collections.abc import Mapping, Sequence
from pathlib import Path
from typing import Any
from urllib.parse import parse_qsl, urlsplit

if __package__ in {None, ""}:
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from tools import claude_code_local_env_attribution_oracle as env_oracle
from tools import claude_code_real_oracle_loopback as loopback_oracle
from tools import claude_code_tls_oracle as tls_oracle


SCHEMA_VERSION = "oracle-lab-phase3a-sub2api-adapter.v1"
MAX_STDIN_BYTES = 1_048_576
MAX_SYNTHETIC_BODY_BYTES = 262_144

SOURCE_MODULES = {
    "guard": ["tools.claude_code_real_oracle_loopback"],
    "http-summary": ["tools.claude_code_real_oracle_loopback"],
    "tls-summary": ["tools.claude_code_tls_oracle"],
    "env-summary": ["tools.claude_code_local_env_attribution_oracle"],
}

GUARD_PROOF_KEYS = {
    "guard_type",
    "deny_all_except_loopback",
    "loopback_collector_reachable",
    "ipv4_external_tcp_blocked",
    "ipv6_external_tcp_blocked",
    "dns_udp_external_blocked",
    "proxy_env_only_rejected",
    "provider_direct_tcp_blocked",
    "provider_tcp_connect_unexpected_success",
    "non_loopback_proxy_env_rejected",
    "proxy_env_only_external_path_blocked",
    "real_provider_through_non_loopback_proxy_blocked",
    "npm_proxy_endpoint_trust_env_rejected",
}
GUARD_BOOL_KEYS = GUARD_PROOF_KEYS - {"guard_type"}

ENV_SAFE_SUMMARY_KEYS = {
    "schema",
    "version",
    "client_family",
    "timezone_bucket",
    "base_url_bucket",
    "proxy_bucket",
    "method",
    "path",
    "query_keys",
    "system_prompt_present",
    "today_date_marker_present",
    "residue_location_bucket",
    "date_format_bucket",
    "apostrophe_bucket",
    "timezone_signal_residue_present",
    "base_url_signal_residue_present",
    "known_domain_match_bucket",
    "known_domain_category_bucket",
    "current_env_only_evidence",
    "proxy_signal_residue_present",
    "billing_marker_present",
    "cch_marker_present",
    "billing_cch_location_bucket",
    "billing_cch_covaries_with_env_residue",
    "raw_body_omitted_reason",
}
ENV_BOOL_KEYS = {
    "system_prompt_present",
    "today_date_marker_present",
    "timezone_signal_residue_present",
    "base_url_signal_residue_present",
    "proxy_signal_residue_present",
    "billing_marker_present",
    "cch_marker_present",
}

FORBIDDEN_INPUT_KEYS = {
    "authorization",
    "cookie",
    "credential",
    "credentials",
    "password",
    "private_key",
    "proxy_authorization",
    "raw_body",
    "raw_prompt",
    "request_body",
    "response_body",
    "prompt",
    "secret",
    "x_api_key",
    "api_key",
    "access_token",
    "auth_token",
    "body",
    "refresh_token",
    "system_prompt",
}
SAFE_OMISSION_KEYS = {"raw_body_omitted_reason", "raw_clienthello_omitted_reason"}
SECRET_VALUE = re.compile(r"(?:authorization\s*:\s*bearer|\bbearer\s+[a-z0-9._-]+|\bsk-[a-z0-9])", re.I)
FORBIDDEN_KEY_SUFFIXES = (
    "_api_key",
    "_credential",
    "_password",
    "_secret",
    "_access_token",
    "_auth_token",
    "_refresh_token",
)


class AdapterInputError(ValueError):
    """The adapter rejected unsafe or structurally invalid input."""


def _normalized_key(value: str) -> str:
    return value.strip().lower().replace("-", "_")


def _is_forbidden_key(value: str) -> bool:
    normalized = _normalized_key(value)
    return normalized in FORBIDDEN_INPUT_KEYS or normalized.endswith(FORBIDDEN_KEY_SUFFIXES)


def _reject_unsafe_material(value: Any, *, allow_synthetic_body_key: bool = False) -> None:
    if isinstance(value, Mapping):
        for key, item in value.items():
            if not isinstance(key, str):
                raise AdapterInputError("object keys must be strings")
            normalized = _normalized_key(key)
            if normalized not in SAFE_OMISSION_KEYS:
                if _is_forbidden_key(key):
                    raise AdapterInputError("raw or credential-bearing key is forbidden")
                if normalized == "synthetic_body" and not allow_synthetic_body_key:
                    raise AdapterInputError("synthetic_body is only valid for http-summary")
            _reject_unsafe_material(item)
        return
    if isinstance(value, list):
        for item in value:
            _reject_unsafe_material(item)
        return
    if isinstance(value, str) and SECRET_VALUE.search(value):
        raise AdapterInputError("credential-like value is forbidden")


def _strict_object(value: Any, *, required: set[str], optional: set[str] | None = None) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise AdapterInputError("input must be a JSON object")
    optional = optional or set()
    keys = set(value)
    missing = required - keys
    extra = keys - required - optional
    if missing or extra:
        raise AdapterInputError("input schema mismatch")
    return value


def _strict_string(value: Any, field: str, *, maximum: int = 4096) -> str:
    if not isinstance(value, str) or not value or len(value.encode("utf-8")) > maximum:
        raise AdapterInputError(f"{field} must be a bounded non-empty string")
    return value


def _validate_json_value(value: Any) -> None:
    if value is None or isinstance(value, (str, bool, int)):
        return
    if isinstance(value, float):
        if not math.isfinite(value):
            raise AdapterInputError("non-finite JSON numbers are forbidden")
        return
    if isinstance(value, list):
        for item in value:
            _validate_json_value(item)
        return
    if isinstance(value, dict):
        for key, item in value.items():
            if not isinstance(key, str):
                raise AdapterInputError("object keys must be strings")
            _validate_json_value(item)
        return
    raise AdapterInputError("synthetic_body must contain only JSON values")


def _envelope(operation: str, result: dict[str, Any]) -> dict[str, Any]:
    return {
        "schema_version": SCHEMA_VERSION,
        "operation": operation,
        "result": result,
        "source_modules": list(SOURCE_MODULES[operation]),
    }


def _adapt_guard(payload: dict[str, Any]) -> dict[str, Any]:
    _strict_object(payload, required={"operation"}, optional={"same_scope_self_tests"})
    proof = payload.get("same_scope_self_tests")
    if proof is not None:
        proof = _strict_object(proof, required=set(), optional=GUARD_PROOF_KEYS)
        _reject_unsafe_material(proof)
        if "guard_type" in proof:
            allowed = loopback_oracle.HARD_GUARD_TYPES | {"not_available", "external_manual_proof"}
            if proof["guard_type"] not in allowed:
                raise AdapterInputError("guard_type is not a safe bucket")
        for key in GUARD_BOOL_KEYS & set(proof):
            if type(proof[key]) is not bool:  # noqa: E721 - bool-like values are unsafe here.
                raise AdapterInputError("guard proof flags must be strict bools")

    legacy = loopback_oracle.evaluate_egress_guard(same_scope_self_tests=proof)
    strict = loopback_oracle.evaluate_sni_preserving_egress_guard(same_scope_self_tests=proof)
    strict["real_cli_executed"] = False
    strict["sidecar_executed"] = False
    return _envelope(
        "guard",
        {
            "legacy": legacy,
            "sni_preserving": loopback_oracle.safe_cp2_egress_guard_evidence(strict),
        },
    )


def _adapt_http_summary(payload: dict[str, Any]) -> dict[str, Any]:
    fields = {"operation", "version", "scenario", "method", "target", "headers", "synthetic_body"}
    _strict_object(payload, required=fields)
    _reject_unsafe_material(payload, allow_synthetic_body_key=True)
    version = _strict_string(payload["version"], "version", maximum=128)
    scenario = _strict_string(payload["scenario"], "scenario", maximum=128)
    method = _strict_string(payload["method"], "method", maximum=16)
    target = _strict_string(payload["target"], "target")
    if not target.startswith("/") or urlsplit(target).scheme or urlsplit(target).netloc:
        raise AdapterInputError("target must be an origin-form path")
    for query_key, _value in parse_qsl(urlsplit(target).query, keep_blank_values=True):
        if _is_forbidden_key(query_key):
            raise AdapterInputError("credential-bearing query key is forbidden")
    headers = payload["headers"]
    if not isinstance(headers, dict) or any(
        not isinstance(key, str) or not isinstance(value, str)
        for key, value in headers.items()
    ):
        raise AdapterInputError("headers must be a string map")
    if any(_is_forbidden_key(key) for key in headers):
        raise AdapterInputError("credential-bearing header is forbidden")
    body = payload["synthetic_body"]
    _validate_json_value(body)
    encoded = json.dumps(
        body,
        sort_keys=True,
        separators=(",", ":"),
        ensure_ascii=False,
        allow_nan=False,
    ).encode("utf-8")
    if len(encoded) > MAX_SYNTHETIC_BODY_BYTES:
        raise AdapterInputError("synthetic_body exceeds the adapter limit")
    result = loopback_oracle.summarize_http_request(
        method=method,
        raw_target=target,
        headers=headers,
        body=encoded,
        version=version,
        scenario=scenario,
    )
    return _envelope("http-summary", result)


def _adapt_tls_summary(payload: dict[str, Any]) -> dict[str, Any]:
    _strict_object(payload, required={"operation", "summary"})
    summary = payload["summary"]
    if not isinstance(summary, dict):
        raise AdapterInputError("summary must be an object")
    _reject_unsafe_material(summary)
    try:
        tls_oracle.validate_safe_tls_summary(summary)
    except (TypeError, ValueError) as error:
        raise AdapterInputError("TLS safe-summary validation failed") from error
    return _envelope("tls-summary", json.loads(json.dumps(summary, sort_keys=True)))


def _adapt_env_summary(payload: dict[str, Any]) -> dict[str, Any]:
    _strict_object(payload, required={"operation", "summary"})
    summary = payload["summary"]
    if not isinstance(summary, dict) or set(summary) != ENV_SAFE_SUMMARY_KEYS:
        raise AdapterInputError("env safe-summary schema mismatch")
    _reject_unsafe_material(summary)
    if summary["schema"] != "claude_code_local_env_attribution_oracle.v1":
        raise AdapterInputError("env safe-summary schema version mismatch")
    if summary["client_family"] != "cli" or summary["raw_body_omitted_reason"] != "raw_body_forbidden":
        raise AdapterInputError("env safe-summary safety markers are invalid")
    if any(type(summary[key]) is not bool for key in ENV_BOOL_KEYS):  # noqa: E721
        raise AdapterInputError("env safe-summary flags must be strict bools")
    if not isinstance(summary["query_keys"], list) or any(not isinstance(key, str) for key in summary["query_keys"]):
        raise AdapterInputError("env safe-summary query_keys must be strings")
    if any(_is_forbidden_key(key) for key in summary["query_keys"]):
        raise AdapterInputError("credential-bearing query key is forbidden")
    for key in ENV_SAFE_SUMMARY_KEYS - ENV_BOOL_KEYS - {"query_keys"}:
        if not isinstance(summary[key], str):
            raise AdapterInputError("env safe-summary scalar fields must be strings")
    if "?" in summary["path"] or not summary["path"].startswith("/"):
        raise AdapterInputError("env safe-summary path must omit its query")
    return _envelope("env-summary", json.loads(json.dumps(summary, sort_keys=True)))


def dispatch(payload: dict[str, Any]) -> dict[str, Any]:
    if not isinstance(payload, dict) or not isinstance(payload.get("operation"), str):
        raise AdapterInputError("operation is required")
    operation = payload["operation"]
    handlers = {
        "guard": _adapt_guard,
        "http-summary": _adapt_http_summary,
        "tls-summary": _adapt_tls_summary,
        "env-summary": _adapt_env_summary,
    }
    handler = handlers.get(operation)
    if handler is None:
        raise AdapterInputError("unknown operation")
    return handler(payload)


def _reject_duplicate_pairs(pairs: Sequence[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise AdapterInputError("duplicate JSON object key")
        result[key] = value
    return result


def _reject_json_constant(_value: str) -> None:
    raise AdapterInputError("non-finite JSON numbers are forbidden")


def load_strict_json(raw: str) -> dict[str, Any]:
    if len(raw.encode("utf-8")) > MAX_STDIN_BYTES:
        raise AdapterInputError("stdin exceeds the adapter limit")
    try:
        value = json.loads(raw, object_pairs_hook=_reject_duplicate_pairs, parse_constant=_reject_json_constant)
    except AdapterInputError:
        raise
    except (UnicodeError, json.JSONDecodeError) as error:
        raise AdapterInputError("stdin is not strict JSON") from error
    if not isinstance(value, dict):
        raise AdapterInputError("input must be a JSON object")
    return value


def main(argv: list[str] | None = None) -> int:
    if argv is None:
        argv = sys.argv[1:]
    if argv:
        print('{"error":"adapter_input_rejected"}', file=sys.stderr)
        return 2
    try:
        envelope = dispatch(load_strict_json(sys.stdin.read(MAX_STDIN_BYTES + 1)))
    except (AdapterInputError, UnicodeError):
        print('{"error":"adapter_input_rejected"}', file=sys.stderr)
        return 2
    except Exception:
        print('{"error":"adapter_internal_failure"}', file=sys.stderr)
        return 3
    print(json.dumps(envelope, sort_keys=True, separators=(",", ":"), ensure_ascii=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
