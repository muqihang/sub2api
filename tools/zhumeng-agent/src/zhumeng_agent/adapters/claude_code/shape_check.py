from __future__ import annotations

import json
from dataclasses import dataclass, field
from typing import Any, Mapping, Sequence

NATIVE_SHAPE_FIELDS: tuple[str, ...] = (
    "localhost_only",
    "mock_upstream_only",
    "messages_fixture",
    "tool_search_fixture",
    "thinking_fixture",
    "tools_fixture",
    "count_tokens_fixture",
    "stream_fixture",
    "opus_fixture",
    "sonnet_fixture",
    "control_plane_safe_intent_fixture",
    "netwatch_fixture",
    "native_attestation_profile",
    "body_omitted",
)

KNOWN_NATIVE_PROFILES = {
    "real_claude_code_native_takeover_v1",
    "real_claude_code_native_toolsearch_v1",
    "real_claude_code_native_control_plane_shadow_v1",
    "real_claude_code_native_netwatch_v1",
}


@dataclass(frozen=True, slots=True)
class NativeShapeFixture:
    name: str
    route: str
    model_family: str = ""
    has_tool_search: bool = False
    has_thinking: bool = False
    has_tools: bool = False
    stream: bool = False
    profile: str = ""


@dataclass(frozen=True, slots=True)
class NativeShapeEvidence:
    localhost_only: bool
    mock_upstream_only: bool
    control_plane_safe_intent: Mapping[str, Any] = field(default_factory=dict)
    netwatch_summary: Mapping[str, Any] = field(default_factory=dict)
    body_omitted: bool = True


@dataclass(frozen=True, slots=True)
class NativeShapeHealthcheckResult:
    status: str
    fields: tuple[str, ...]
    passed: int
    denominator: int
    failed_fields: tuple[str, ...] = field(default_factory=tuple)
    routes: tuple[str, ...] = field(default_factory=tuple)
    model_families: tuple[str, ...] = field(default_factory=tuple)
    profiles: tuple[str, ...] = field(default_factory=tuple)

    def to_safe_dict(self) -> dict[str, Any]:
        out: dict[str, Any] = {
            "status": self.status,
            "fields": list(self.fields),
            "passed": self.passed,
            "denominator": self.denominator,
            "failed_fields": list(self.failed_fields),
            "routes": list(self.routes),
            "model_families": list(self.model_families),
            "profiles": list(self.profiles),
        }
        if "control_plane_safe_intent_fixture" not in self.failed_fields and "netwatch_fixture" not in self.failed_fields:
            out["safe_evidence"] = {
                "control_plane": "safe_summary_present",
                "netwatch": "safe_summary_present",
            }
        return out


def native_fixture_from_messages_body(body: bytes, *, name: str, route: str, profile: str) -> NativeShapeFixture:
    try:
        root = json.loads(body.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError):
        root = {}
    model = str(root.get("model", "")).lower()
    tools = root.get("tools")
    has_tools = isinstance(tools, list)
    has_tool_search = _contains_native_marker(root, "tool_reference") or _contains_native_marker(root, "defer_loading")
    return NativeShapeFixture(
        name=name,
        route=route,
        model_family=_model_family(model),
        has_tool_search=has_tool_search and has_tools and bool(tools),
        has_thinking="thinking" in root,
        has_tools=has_tools,
        stream=bool(root.get("stream")),
        profile=profile,
    )


def evaluate_native_shape_healthcheck(fixtures: Sequence[NativeShapeFixture], evidence: NativeShapeEvidence) -> NativeShapeHealthcheckResult:
    checks = {
        "localhost_only": evidence.localhost_only,
        "mock_upstream_only": evidence.mock_upstream_only,
        "body_omitted": evidence.body_omitted,
        "control_plane_safe_intent_fixture": _valid_control_plane_intent(evidence.control_plane_safe_intent),
        "netwatch_fixture": _valid_netwatch(evidence.netwatch_summary),
        "native_attestation_profile": bool(fixtures) and all(item.profile in KNOWN_NATIVE_PROFILES for item in fixtures),
    }
    routes = {item.route for item in fixtures if item.route}
    profiles = {item.profile for item in fixtures if item.profile}
    families = {item.model_family for item in fixtures if item.model_family}
    for item in fixtures:
        if item.route == "/v1/messages":
            checks["messages_fixture"] = True
        if item.route == "/v1/messages/count_tokens":
            checks["count_tokens_fixture"] = True
        if item.has_tool_search:
            checks["tool_search_fixture"] = True
        if item.has_thinking:
            checks["thinking_fixture"] = True
        if item.has_tools:
            checks["tools_fixture"] = True
        if item.stream:
            checks["stream_fixture"] = True
        if item.model_family:
            checks[f"{item.model_family}_fixture"] = True

    failed = tuple(field for field in NATIVE_SHAPE_FIELDS if not checks.get(field, False))
    passed = len(NATIVE_SHAPE_FIELDS) - len(failed)
    return NativeShapeHealthcheckResult(
        status="pass" if not failed else "fail",
        fields=NATIVE_SHAPE_FIELDS,
        passed=passed,
        denominator=len(NATIVE_SHAPE_FIELDS),
        failed_fields=failed,
        routes=tuple(sorted(routes)),
        model_families=tuple(sorted(families)),
        profiles=tuple(sorted(profiles)),
    )


def _contains_native_marker(value: Any, needle: str) -> bool:
    if isinstance(value, dict):
        for key, item in value.items():
            if key in {"input_schema", "schema"}:
                continue
            if key == needle or _contains_native_marker(item, needle):
                return True
        return False
    if isinstance(value, list):
        return any(_contains_native_marker(item, needle) for item in value)
    return False


def _model_family(model: str) -> str:
    if "opus" in model:
        return "opus"
    if "sonnet" in model:
        return "sonnet"
    return ""


def _valid_control_plane_intent(summary: Mapping[str, Any]) -> bool:
    allowed = {
        "safe_intent",
        "method",
        "path_template",
        "decision",
        "status",
        "stores_raw",
        "messages_signing_reused",
        "response_schema_keys",
    }
    keys = summary.get("response_schema_keys")
    return (
        set(summary) == allowed
        and not _contains_unsafe_summary_value(summary)
        and summary.get("safe_intent") is True
        and summary.get("method") == "GET"
        and str(summary.get("path_template", "")).startswith("/")
        and summary.get("decision") in {"stub_json", "suppress_204", "quarantine_block", "block_403", "shadow_stub", "shadow_block"}
        and isinstance(summary.get("status"), int)
        and 200 <= int(summary["status"]) <= 599
        and summary.get("stores_raw") is False
        and summary.get("messages_signing_reused") is False
        and isinstance(keys, list)
        and bool(keys)
        and all(isinstance(item, str) and item and not _unsafe_summary_text(item) for item in keys)
    )


def _valid_netwatch(summary: Mapping[str, Any]) -> bool:
    allowed = {
        "connection_count",
        "potential_guard_bypass_count",
        "official_or_public_bypass_count",
        "loopback_guard_connection_count",
        "remote_host_buckets",
        "stores_payload",
        "stores_headers",
    }
    if set(summary) != allowed or _contains_unsafe_summary_value(summary):
        return False
    buckets = summary.get("remote_host_buckets")
    if not isinstance(buckets, Mapping) or set(buckets) != {"loopback"}:
        return False
    if summary.get("stores_payload") is not False or summary.get("stores_headers") is not False:
        return False
    numeric_fields = (
        "connection_count",
        "potential_guard_bypass_count",
        "official_or_public_bypass_count",
        "loopback_guard_connection_count",
    )
    if not all(isinstance(summary.get(field), int) and summary[field] >= 0 for field in numeric_fields):
        return False
    if summary["potential_guard_bypass_count"] != 0 or summary["official_or_public_bypass_count"] != 0:
        return False
    loopback_bucket = buckets.get("loopback")
    if not isinstance(loopback_bucket, int) or loopback_bucket <= 0:
        return False
    return summary["loopback_guard_connection_count"] > 0


def _contains_unsafe_summary_value(value: Any) -> bool:
    if isinstance(value, Mapping):
        return any(_unsafe_summary_text(str(key)) or _contains_unsafe_summary_value(item) for key, item in value.items())
    if isinstance(value, list):
        return any(_contains_unsafe_summary_value(item) for item in value)
    if isinstance(value, str):
        return _unsafe_summary_text(value)
    return False


def _unsafe_summary_text(value: str) -> bool:
    lower = value.strip().lower()
    markers = (
        "authorization",
        "cookie",
        "proxy_credential",
        "x-api-key",
        "token",
        "secret",
        "prompt",
        "raw_",
        "raw_body",
        "raw_prompt",
        "raw_telemetry",
        "raw_cch",
        "cch=",
        "account_uuid",
        "org_uuid",
        "user_uuid",
        "email",
        "access_token",
        "refresh_token",
        "api.anthropic.com",
        "claude.ai",
        "claude.com",
        "anthropic_or_claude",
        "public_ip",
        "dns_name",
    )
    return any(marker in lower for marker in markers)
