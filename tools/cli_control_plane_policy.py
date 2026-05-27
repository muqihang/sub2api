#!/usr/bin/env python3
"""Schema-driven policy classifier for the CLI control-plane guard."""
from __future__ import annotations

from dataclasses import dataclass
from hashlib import sha256
from typing import Any
from urllib.parse import urlsplit
import copy
import json
import re


_ALLOWED_TOP_LEVEL_KEYS = {
    "schema_version",
    "mode",
    "summary_path",
    "redaction",
    "messages",
    "control_plane",
    "unknown",
    "connect",
}
_ALLOWED_MODES = {
    "localhost_preflight",
    "canary_single_message",
    "session_budgeted",
    "block_all_real",
}
_ALLOWED_ACTIONS = {
    "forward_messages",
    "suppress_204",
    "stub_json",
    "block_403",
    "quarantine_block",
}
_ALLOWED_RESPONSE_POLICIES = {"sanitized_schema", "suppress", "cache_only"}

_DEFAULT_POLICY_DICT = {
    "schema_version": 1,
    "mode": "localhost_preflight",
    "summary_path": "artifacts/cli-control-plane-summary.jsonl",
    "redaction": {
        "headers": ["authorization", "cookie", "proxy-authorization", "set-cookie", "x-api-key"],
        "json_fields": [
            "authorization",
            "body",
            "cch",
            "cookie",
            "email",
            "messages",
            "prompt",
        ],
    },
    "messages": {
        "allowed_routes": [
            {"method": "POST", "path": "/v1/messages", "query": "beta=true"},
        ],
        "max_messages": 1,
        "stop_cli_after_first_response": False,
        "cost_envelope": {
            "max_input_tokens": 0,
            "max_output_tokens": 0,
            "max_usd_micros": 0,
        },
    },
    "control_plane": {
        "defaults": {
            "upload_strategy": "disabled",
            "auth_source": "none",
            "cache_scope": "none",
            "body_policy": "forbidden",
            "response_policy": "sanitized_schema",
            "upload_kill_switch": True,
            "ttl_seconds": 0,
            "policy_version": 1,
            "strategy_version": 1,
            "response_schema_version": 1,
        },
        "telemetry": {
            "match": [
                {"method": "POST", "path": "/api/event_logging/v2/batch"},
                {"method": "POST", "path_prefix": "/api/eval/"},
            ],
            "action": "suppress_204",
        },
        "mcp": {
            "match": [
                {"method": "GET", "path": "/v1/mcp_servers"},
            ],
            "action": "stub_json",
            "response": {
                "status": 200,
                "content_type": "application/json",
                "body": {"data": [], "servers": []},
            },
        },
        "bootstrap_settings": {
            "match": [
                {"method": "GET", "path": "/api/hello"},
                {"method": "GET", "path": "/v1/oauth/hello"},
                {"method": "GET", "path": "/api/claude_cli/bootstrap"},
            ],
            "action": "stub_json",
            "response": {
                "status": 200,
                "content_type": "application/json",
                "body": {},
            },
        },
        "sensitive_get_candidates": {
            "match": [
                {"method": "GET", "path": "/api/oauth/account/settings"},
            ],
            "action": "quarantine_block",
        },
        "readiness_only_candidate_families": {
            "match": [
                {"method": "GET", "path_prefix": "/api/claude_code_"},
                {"method": "GET", "path_prefix": "/mcp-registry/"},
            ],
            "action": "quarantine_block",
        },
    },
    "unknown": {"action": "quarantine_block"},
    "connect": {
        "allowed_stub_targets": [
            "api.anthropic.com:443",
            "platform.claude.com:443",
            "claude.ai:443",
            "claude.com:443",
            "mcp-proxy.anthropic.com:443",
        ],
        "unknown_target_action": "block_403",
    },
}


class PolicyConfigError(ValueError):
    """Raised when the control-plane policy schema is invalid."""


@dataclass(frozen=True)
class PolicyDecision:
    action: str
    reason: str
    status: int
    content_type: str | None = None
    body: Any | None = None


@dataclass(frozen=True)
class _Matcher:
    method: str
    kind: str
    value: str
    compiled_regex: re.Pattern[str] | None = None
    query: str | None = None

    def matches(self, method: str, raw_path: str) -> bool:
        parsed = urlsplit(raw_path)
        if method.upper() != self.method:
            return False
        if self.kind == "path":
            if parsed.path != self.value:
                return False
        elif self.kind == "path_prefix":
            if not parsed.path.startswith(self.value):
                return False
        elif self.kind == "path_regex":
            if self.compiled_regex is None or self.compiled_regex.fullmatch(parsed.path) is None:
                return False
        else:
            return False
        if self.query is not None and parsed.query != self.query:
            return False
        return True

    def signature(self) -> tuple[str, str, str, str | None]:
        return (self.method, self.kind, self.value, self.query)


@dataclass(frozen=True)
class _Bucket:
    name: str
    action: str
    matchers: tuple[_Matcher, ...]
    response: dict[str, Any] | None = None


class ControlPlanePolicy:
    def __init__(
        self,
        *,
        schema_version: int,
        mode: str,
        summary_path: str,
        redaction: dict[str, list[str]],
        message_routes: tuple[_Matcher, ...],
        control_plane_buckets: tuple[_Bucket, ...],
        control_plane_defaults: dict[str, Any],
        unknown_action: str,
        connect: dict[str, Any],
        source_dict: dict[str, Any],
    ):
        self.schema_version = schema_version
        self.mode = mode
        self.summary_path = summary_path
        self.redaction = copy.deepcopy(redaction)
        self._message_routes = message_routes
        self._control_plane_buckets = control_plane_buckets
        self._control_plane_defaults = copy.deepcopy(control_plane_defaults)
        self.unknown_action = unknown_action
        self.connect = copy.deepcopy(connect)
        self._source_dict = copy.deepcopy(source_dict)

    @property
    def control_plane_defaults(self) -> dict[str, Any]:
        return copy.deepcopy(self._control_plane_defaults)

    @classmethod
    def from_dict(cls, config: dict[str, Any]) -> "ControlPlanePolicy":
        if not isinstance(config, dict):
            raise PolicyConfigError("policy config must be a dict")
        _require_exact_keys("policy", config, _ALLOWED_TOP_LEVEL_KEYS)

        schema_version = _require_positive_int(config.get("schema_version"), "schema_version")
        mode = config.get("mode")
        if mode not in _ALLOWED_MODES:
            raise PolicyConfigError(f"invalid mode: {mode!r}")
        summary_path = _require_non_empty_string(config.get("summary_path"), "summary_path")
        redaction = _parse_redaction(config.get("redaction"))
        message_routes, messages_shape = _parse_messages(config.get("messages"))
        control_plane_buckets, defaults, control_plane_shape = _parse_control_plane(config.get("control_plane"))
        unknown_shape = _parse_unknown(config.get("unknown"))
        connect = _parse_connect(config.get("connect"))
        _ensure_unique_matchers(message_routes, control_plane_buckets)

        normalized = {
            "schema_version": schema_version,
            "mode": mode,
            "summary_path": summary_path,
            "redaction": redaction,
            "messages": messages_shape,
            "control_plane": control_plane_shape,
            "unknown": unknown_shape,
            "connect": connect,
        }
        return cls(
            schema_version=schema_version,
            mode=mode,
            summary_path=summary_path,
            redaction=redaction,
            message_routes=message_routes,
            control_plane_buckets=control_plane_buckets,
            control_plane_defaults=defaults,
            unknown_action=unknown_shape["action"],
            connect=connect,
            source_dict=normalized,
        )

    def decide(self, method: str, path: str) -> PolicyDecision:
        normalized_method = method.upper()
        for matcher in self._message_routes:
            if matcher.matches(normalized_method, path):
                return PolicyDecision(
                    action="forward_messages",
                    reason=f"messages:{matcher.value}?{matcher.query}",
                    status=200,
                )
        for bucket in self._control_plane_buckets:
            for matcher in bucket.matchers:
                if matcher.matches(normalized_method, path):
                    status = 204 if bucket.action == "suppress_204" else 403
                    content_type = None
                    body = None
                    if bucket.action == "stub_json":
                        assert bucket.response is not None
                        status = bucket.response["status"]
                        content_type = bucket.response["content_type"]
                        body = copy.deepcopy(bucket.response["body"])
                    elif bucket.action == "block_403":
                        status = 403
                    elif bucket.action == "quarantine_block":
                        status = 403
                    return PolicyDecision(
                        action=bucket.action,
                        reason=f"control_plane:{bucket.name}:{matcher.kind}",
                        status=status,
                        content_type=content_type,
                        body=body,
                    )
        return PolicyDecision(
            action=self.unknown_action,
            reason="unknown:quarantine",
            status=403,
        )

    def fingerprint(self) -> str:
        encoded = json.dumps(self._source_dict, ensure_ascii=True, separators=(",", ":"), sort_keys=True).encode("utf-8")
        return f"sha256:{sha256(encoded).hexdigest()}"


def load_default_policy() -> ControlPlanePolicy:
    return ControlPlanePolicy.from_dict(copy.deepcopy(_DEFAULT_POLICY_DICT))


def _parse_redaction(redaction: Any) -> dict[str, list[str]]:
    if not isinstance(redaction, dict):
        raise PolicyConfigError("redaction must be a dict")
    _require_exact_keys("redaction", redaction, {"headers", "json_fields"})
    return {
        "headers": _require_string_list(redaction.get("headers"), "redaction.headers"),
        "json_fields": _require_string_list(redaction.get("json_fields"), "redaction.json_fields"),
    }


def _parse_messages(messages: Any) -> tuple[tuple[_Matcher, ...], dict[str, Any]]:
    if not isinstance(messages, dict):
        raise PolicyConfigError("messages must be a dict")
    _require_exact_keys(
        "messages",
        messages,
        {"allowed_routes", "max_messages", "stop_cli_after_first_response", "cost_envelope"},
    )
    allowed_routes = messages.get("allowed_routes")
    if not isinstance(allowed_routes, list) or not allowed_routes:
        raise PolicyConfigError("messages.allowed_routes must be a non-empty list")
    matchers = []
    normalized_routes = []
    for index, route in enumerate(allowed_routes):
        if not isinstance(route, dict):
            raise PolicyConfigError(f"messages.allowed_routes[{index}] must be a dict")
        _require_exact_keys(
            f"messages.allowed_routes[{index}]",
            route,
            {"method", "path", "query"},
        )
        method = _normalize_method(route.get("method"), f"messages.allowed_routes[{index}].method")
        path = _require_path(route.get("path"), f"messages.allowed_routes[{index}].path")
        query = _require_non_empty_string(route.get("query"), f"messages.allowed_routes[{index}].query")
        matcher = _Matcher(method=method, kind="path", value=path, query=query)
        matchers.append(matcher)
        normalized_routes.append({"method": method, "path": path, "query": query})
    max_messages = _require_non_negative_int(messages.get("max_messages"), "messages.max_messages")
    stop_after = messages.get("stop_cli_after_first_response")
    if not isinstance(stop_after, bool):
        raise PolicyConfigError("messages.stop_cli_after_first_response must be a bool")
    cost_envelope = messages.get("cost_envelope")
    if not isinstance(cost_envelope, dict):
        raise PolicyConfigError("messages.cost_envelope must be a dict")
    _require_exact_keys(
        "messages.cost_envelope",
        cost_envelope,
        {"max_input_tokens", "max_output_tokens", "max_usd_micros"},
    )
    normalized_cost = {
        "max_input_tokens": _require_non_negative_int(cost_envelope.get("max_input_tokens"), "messages.cost_envelope.max_input_tokens"),
        "max_output_tokens": _require_non_negative_int(cost_envelope.get("max_output_tokens"), "messages.cost_envelope.max_output_tokens"),
        "max_usd_micros": _require_non_negative_int(cost_envelope.get("max_usd_micros"), "messages.cost_envelope.max_usd_micros"),
    }
    return (
        tuple(matchers),
        {
            "allowed_routes": normalized_routes,
            "max_messages": max_messages,
            "stop_cli_after_first_response": stop_after,
            "cost_envelope": normalized_cost,
        },
    )


def _parse_control_plane(control_plane: Any) -> tuple[tuple[_Bucket, ...], dict[str, Any], dict[str, Any]]:
    if not isinstance(control_plane, dict):
        raise PolicyConfigError("control_plane must be a dict")
    _require_exact_keys(
        "control_plane",
        control_plane,
        {"defaults", "telemetry", "mcp", "bootstrap_settings", "sensitive_get_candidates", "readiness_only_candidate_families"},
    )
    defaults = _parse_defaults(control_plane.get("defaults"))
    buckets = []
    normalized = {"defaults": defaults}
    for bucket_name in ("telemetry", "mcp", "bootstrap_settings", "sensitive_get_candidates", "readiness_only_candidate_families"):
        bucket, bucket_shape = _parse_bucket(bucket_name, control_plane.get(bucket_name))
        buckets.append(bucket)
        normalized[bucket_name] = bucket_shape
    return tuple(buckets), defaults, normalized


def _parse_defaults(defaults: Any) -> dict[str, Any]:
    if not isinstance(defaults, dict):
        raise PolicyConfigError("control_plane.defaults must be a dict")
    _require_exact_keys(
        "control_plane.defaults",
        defaults,
        {
            "upload_strategy",
            "auth_source",
            "cache_scope",
            "body_policy",
            "response_policy",
            "upload_kill_switch",
            "ttl_seconds",
            "policy_version",
            "strategy_version",
            "response_schema_version",
        },
    )
    upload_strategy = defaults.get("upload_strategy")
    if upload_strategy != "disabled":
        raise PolicyConfigError("B1 upload_strategy must remain disabled")
    auth_source = defaults.get("auth_source")
    if auth_source != "none":
        raise PolicyConfigError("B1 auth_source must remain none")
    cache_scope = defaults.get("cache_scope")
    if cache_scope != "none":
        raise PolicyConfigError("B1 cache_scope must remain none")
    body_policy = defaults.get("body_policy")
    if body_policy != "forbidden":
        raise PolicyConfigError("B1 body_policy must remain forbidden")
    response_policy = defaults.get("response_policy")
    if response_policy not in _ALLOWED_RESPONSE_POLICIES:
        raise PolicyConfigError(f"invalid response_policy: {response_policy!r}")
    upload_kill_switch = defaults.get("upload_kill_switch")
    if upload_kill_switch is not True:
        raise PolicyConfigError("upload_kill_switch must stay true in B1")
    ttl_seconds = defaults.get("ttl_seconds")
    if ttl_seconds != 0:
        raise PolicyConfigError("ttl_seconds must stay 0 in B1")
    normalized = {
        "upload_strategy": upload_strategy,
        "auth_source": auth_source,
        "cache_scope": cache_scope,
        "body_policy": body_policy,
        "response_policy": response_policy,
        "upload_kill_switch": True,
        "ttl_seconds": 0,
        "policy_version": _require_positive_int(defaults.get("policy_version"), "control_plane.defaults.policy_version"),
        "strategy_version": _require_positive_int(defaults.get("strategy_version"), "control_plane.defaults.strategy_version"),
        "response_schema_version": _require_positive_int(defaults.get("response_schema_version"), "control_plane.defaults.response_schema_version"),
    }
    return normalized


def _parse_bucket(name: str, bucket: Any) -> tuple[_Bucket, dict[str, Any]]:
    if not isinstance(bucket, dict):
        raise PolicyConfigError(f"control_plane.{name} must be a dict")
    allowed_keys = {"match", "action"}
    if bucket.get("action") == "stub_json" or "response" in bucket:
        allowed_keys = {"match", "action", "response"}
    _require_exact_keys(f"control_plane.{name}", bucket, allowed_keys)
    action = bucket.get("action")
    _validate_action(action, f"control_plane.{name}.action")
    match_value = bucket.get("match")
    if not isinstance(match_value, list) or not match_value:
        raise PolicyConfigError(f"control_plane.{name}.match must be a non-empty list")
    matchers = []
    normalized_match = []
    for index, entry in enumerate(match_value):
        matcher, matcher_shape = _parse_matcher(entry, f"control_plane.{name}.match[{index}]")
        matchers.append(matcher)
        normalized_match.append(matcher_shape)
    response = None
    normalized_bucket: dict[str, Any] = {"match": normalized_match, "action": action}
    if action == "stub_json":
        response = _parse_stub_response(bucket.get("response"), f"control_plane.{name}.response")
        normalized_bucket["response"] = copy.deepcopy(response)
    elif "response" in bucket:
        raise PolicyConfigError(f"control_plane.{name}.response is only allowed for stub_json")
    return _Bucket(name=name, action=action, matchers=tuple(matchers), response=response), normalized_bucket


def _parse_stub_response(response: Any, field_name: str) -> dict[str, Any]:
    if not isinstance(response, dict):
        raise PolicyConfigError(f"{field_name} must be a dict")
    _require_exact_keys(field_name, response, {"status", "content_type", "body"})
    return {
        "status": _require_positive_int(response.get("status"), f"{field_name}.status"),
        "content_type": _require_non_empty_string(response.get("content_type"), f"{field_name}.content_type"),
        "body": copy.deepcopy(response.get("body")),
    }


def _parse_unknown(unknown: Any) -> dict[str, Any]:
    if not isinstance(unknown, dict):
        raise PolicyConfigError("unknown must be a dict")
    _require_exact_keys("unknown", unknown, {"action"})
    action = unknown.get("action")
    if action != "quarantine_block":
        raise PolicyConfigError("unknown.action must be quarantine_block in B2-P0")
    return {"action": action}


def _parse_connect(connect: Any) -> dict[str, Any]:
    if not isinstance(connect, dict):
        raise PolicyConfigError("connect must be a dict")
    _require_exact_keys(connect_name := "connect", connect, {"allowed_stub_targets", "unknown_target_action"})
    action = connect.get("unknown_target_action")
    if action != "block_403":
        raise PolicyConfigError("connect.unknown_target_action must be block_403 in B1")
    return {
        "allowed_stub_targets": _require_string_list(connect.get("allowed_stub_targets"), f"{connect_name}.allowed_stub_targets"),
        "unknown_target_action": action,
    }


def _parse_matcher(entry: Any, field_name: str) -> tuple[_Matcher, dict[str, str]]:
    if not isinstance(entry, dict):
        raise PolicyConfigError(f"{field_name} must be a dict")
    allowed_keys = {"method", "path", "path_prefix", "path_regex"}
    _require_exact_keys(field_name, entry, allowed_keys, allow_missing=True)
    method = _normalize_method(entry.get("method"), f"{field_name}.method")
    kinds = [name for name in ("path", "path_prefix", "path_regex") if name in entry]
    if len(kinds) != 1:
        raise PolicyConfigError(f"{field_name} must define exactly one of path/path_prefix/path_regex")
    kind = kinds[0]
    value = _require_path(entry.get(kind), f"{field_name}.{kind}")
    compiled = None
    if kind == "path_regex":
        try:
            compiled = re.compile(value)
        except re.error as exc:
            raise PolicyConfigError(f"invalid regex for {field_name}.path_regex: {exc}") from exc
    matcher = _Matcher(method=method, kind=kind, value=value, compiled_regex=compiled)
    return matcher, {"method": method, kind: value}


def _ensure_unique_matchers(message_routes: tuple[_Matcher, ...], buckets: tuple[_Bucket, ...]) -> None:
    seen: list[_Matcher] = []
    for matcher in message_routes:
        _raise_if_overlapping_matcher(matcher, seen)
        seen.append(matcher)
    for bucket in buckets:
        for matcher in bucket.matchers:
            _raise_if_overlapping_matcher(matcher, seen)
            seen.append(matcher)


def _raise_if_overlapping_matcher(matcher: _Matcher, seen: list[_Matcher]) -> None:
    for existing in seen:
        if _matchers_overlap(existing, matcher):
            raise PolicyConfigError(
                f"overlapping match: {existing.signature()} vs {matcher.signature()}"
            )


def _matchers_overlap(left: _Matcher, right: _Matcher) -> bool:
    if left.method != right.method:
        return False
    if not _queries_can_overlap(left.query, right.query):
        return False
    if left.signature() == right.signature():
        return True
    if left.kind == "path" and right.kind == "path":
        return left.value == right.value
    if left.kind == "path" and right.kind == "path_prefix":
        return left.value.startswith(right.value)
    if left.kind == "path_prefix" and right.kind == "path":
        return right.value.startswith(left.value)
    if left.kind == "path_prefix" and right.kind == "path_prefix":
        return left.value.startswith(right.value) or right.value.startswith(left.value)
    if left.kind == "path_regex" and right.kind == "path_regex":
        return left.value == right.value
    return False


def _queries_can_overlap(left: str | None, right: str | None) -> bool:
    return left == right or left is None or right is None


def _validate_action(action: Any, field_name: str) -> None:
    if action not in _ALLOWED_ACTIONS:
        raise PolicyConfigError(f"invalid action for {field_name}: {action!r}")


def _require_exact_keys(field_name: str, value: dict[str, Any], allowed_keys: set[str], allow_missing: bool = False) -> None:
    keys = set(value.keys())
    unknown = keys - allowed_keys
    if unknown:
        raise PolicyConfigError(f"unknown field(s) in {field_name}: {sorted(unknown)}")
    if not allow_missing:
        missing = allowed_keys - keys
        if missing:
            raise PolicyConfigError(f"missing field(s) in {field_name}: {sorted(missing)}")


def _normalize_method(value: Any, field_name: str) -> str:
    method = _require_non_empty_string(value, field_name).upper()
    return method


def _require_path(value: Any, field_name: str) -> str:
    path = _require_non_empty_string(value, field_name)
    if not path.startswith("/") and not path.startswith("^"):
        raise PolicyConfigError(f"{field_name} must start with '/' or '^'")
    return path


def _require_non_empty_string(value: Any, field_name: str) -> str:
    if not isinstance(value, str) or not value:
        raise PolicyConfigError(f"{field_name} must be a non-empty string")
    return value


def _require_string_list(value: Any, field_name: str) -> list[str]:
    if not isinstance(value, list) or any(not isinstance(item, str) or not item for item in value):
        raise PolicyConfigError(f"{field_name} must be a list of non-empty strings")
    return list(value)


def _require_positive_int(value: Any, field_name: str) -> int:
    if not isinstance(value, int) or isinstance(value, bool) or value <= 0:
        raise PolicyConfigError(f"{field_name} must be a positive integer")
    return value


def _require_non_negative_int(value: Any, field_name: str) -> int:
    if not isinstance(value, int) or isinstance(value, bool) or value < 0:
        raise PolicyConfigError(f"{field_name} must be a non-negative integer")
    return value
