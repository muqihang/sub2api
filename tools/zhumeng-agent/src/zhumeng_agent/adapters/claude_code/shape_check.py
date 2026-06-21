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
    "system_fixture",
    "context_management_fixture",
    "prompt_caching_fixture",
    "prompt_cache_usage_fixture",
    "eager_input_streaming_fixture",
    "fgts_trace_fixture",
    "output_config_fixture",
    "adaptive_thinking_fixture",
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
    has_adaptive_thinking: bool = False
    has_system: bool = False
    has_context_management: bool = False
    has_prompt_caching: bool = False
    prompt_cache_locations: tuple[str, ...] = field(default_factory=tuple)
    has_eager_input_streaming: bool = False
    has_output_config: bool = False
    has_tools: bool = False
    stream: bool = False
    profile: str = ""


@dataclass(frozen=True, slots=True)
class NativeShapeEvidence:
    localhost_only: bool
    mock_upstream_only: bool
    control_plane_safe_intent: Mapping[str, Any] = field(default_factory=dict)
    netwatch_summary: Mapping[str, Any] = field(default_factory=dict)
    prompt_cache_safe_usage: Mapping[str, Any] = field(default_factory=dict)
    fgts_safe_trace: Mapping[str, Any] = field(default_factory=dict)
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
            safe_evidence = {
                "control_plane": "safe_summary_present",
                "netwatch": "safe_summary_present",
            }
            if "prompt_cache_usage_fixture" not in self.failed_fields:
                safe_evidence["prompt_cache"] = "safe_usage_summary_present"
            if "fgts_trace_fixture" not in self.failed_fields:
                safe_evidence["fgts"] = "safe_trace_summary_present"
            out["safe_evidence"] = safe_evidence
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
    thinking = root.get("thinking")
    prompt_cache_locations = tuple(sorted(_prompt_cache_control_locations(root)))
    return NativeShapeFixture(
        name=name,
        route=route,
        model_family=_model_family(model),
        has_tool_search=has_tool_search and has_tools and bool(tools),
        has_thinking="thinking" in root,
        has_adaptive_thinking=isinstance(thinking, Mapping) and str(thinking.get("type", "")).strip().lower() == "adaptive",
        has_system=_has_system_fixture(root),
        has_context_management=_has_context_management_fixture(root),
        has_prompt_caching=bool(prompt_cache_locations),
        prompt_cache_locations=prompt_cache_locations,
        has_eager_input_streaming=root.get("eager_input_streaming") is True,
        has_output_config=_has_output_config_fixture(root),
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
    observed_prompt_cache_locations = {
        location
        for item in fixtures
        for location in item.prompt_cache_locations
    }
    checks["prompt_cache_usage_fixture"] = _valid_prompt_cache_safe_usage(
        evidence.prompt_cache_safe_usage,
        observed_prompt_cache_locations,
    )
    observed_eager_input_streaming = any(item.has_eager_input_streaming for item in fixtures)
    checks["fgts_trace_fixture"] = _valid_fgts_safe_trace(
        evidence.fgts_safe_trace,
        observed_eager_input_streaming,
    )
    for item in fixtures:
        if item.route == "/v1/messages":
            checks["messages_fixture"] = True
        if item.route == "/v1/messages/count_tokens":
            checks["count_tokens_fixture"] = True
        if item.has_tool_search:
            checks["tool_search_fixture"] = True
        if item.has_thinking:
            checks["thinking_fixture"] = True
        if item.has_adaptive_thinking:
            checks["adaptive_thinking_fixture"] = True
        if item.has_system:
            checks["system_fixture"] = True
        if item.has_context_management:
            checks["context_management_fixture"] = True
        if item.has_prompt_caching:
            checks["prompt_caching_fixture"] = True
        if item.has_eager_input_streaming:
            checks["eager_input_streaming_fixture"] = True
        if item.has_output_config:
            checks["output_config_fixture"] = True
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


def _has_system_fixture(root: Mapping[str, Any]) -> bool:
    system = root.get("system")
    if isinstance(system, list):
        return any(isinstance(item, Mapping) and str(item.get("type", "")).strip() == "text" and bool(str(item.get("text", "")).strip()) for item in system)
    if isinstance(system, str):
        return bool(system.strip())
    return False


def _has_context_management_fixture(root: Mapping[str, Any]) -> bool:
    context_management = root.get("context_management")
    if not isinstance(context_management, Mapping):
        return False
    edits = context_management.get("edits")
    return isinstance(edits, list) and bool(edits)


def _has_output_config_fixture(root: Mapping[str, Any]) -> bool:
    output_config = root.get("output_config")
    if not isinstance(output_config, Mapping):
        return False
    effort = str(output_config.get("effort", "")).strip()
    return bool(effort) and not _unsafe_summary_text(effort)


def _prompt_cache_control_locations(root: Mapping[str, Any]) -> set[str]:
    locations: set[str] = set()
    if _valid_cache_control(root.get("cache_control")):
        locations.add("top_level")
    if _contains_valid_cache_control(root.get("system")):
        locations.add("system")
    if _contains_valid_cache_control(root.get("tools")):
        locations.add("tools")
    if _contains_valid_cache_control(root.get("messages")):
        locations.add("history")
    return locations


def _contains_valid_cache_control(value: Any) -> bool:
    if isinstance(value, Mapping):
        for key, item in value.items():
            if key in {"input_schema", "schema"}:
                continue
            if key == "cache_control":
                if _valid_cache_control(item):
                    return True
                continue
            if _contains_valid_cache_control(item):
                return True
        return False
    if isinstance(value, list):
        return any(_contains_valid_cache_control(item) for item in value)
    return False


def _valid_cache_control(value: Any) -> bool:
    return isinstance(value, Mapping) and str(value.get("type", "")).strip().lower() == "ephemeral"


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


def _valid_prompt_cache_safe_usage(summary: Mapping[str, Any], observed_locations: set[str]) -> bool:
    allowed = {
        "provider_cache_mechanism",
        "cache_control_present",
        "cache_control_locations",
        "prompt_caching_beta_present",
        "context_management_beta_present",
        "cache_usage_fields",
        "cache_creation_input_tokens",
        "cache_read_input_tokens",
        "stores_raw",
        "body_omitted",
        "response_omitted",
    }
    if set(summary) != allowed:
        return False
    if summary.get("provider_cache_mechanism") != "anthropic_cache_control":
        return False
    if summary.get("cache_control_present") is not True:
        return False
    if summary.get("prompt_caching_beta_present") is not True or summary.get("context_management_beta_present") is not True:
        return False
    if summary.get("stores_raw") is not False or summary.get("body_omitted") is not True or summary.get("response_omitted") is not True:
        return False
    locations = summary.get("cache_control_locations")
    if not isinstance(locations, list) or not locations or not observed_locations:
        return False
    allowed_locations = {"top_level", "system", "tools", "history"}
    if any(not isinstance(item, str) or item not in allowed_locations or item not in observed_locations for item in locations):
        return False
    if len(set(locations)) != len(locations) or set(locations) != observed_locations:
        return False
    fields = summary.get("cache_usage_fields")
    if not isinstance(fields, list) or set(fields) != {"cache_creation_input_tokens", "cache_read_input_tokens"}:
        return False
    return _non_negative_int(summary.get("cache_creation_input_tokens")) and _non_negative_int(summary.get("cache_read_input_tokens"))


def _non_negative_int(value: Any) -> bool:
    return isinstance(value, int) and not isinstance(value, bool) and value >= 0


def _valid_fgts_safe_trace(summary: Mapping[str, Any], eager_input_streaming_present: bool) -> bool:
    allowed = {
        "mode",
        "requested_mode",
        "env_value",
        "eager_input_streaming_present",
        "direct_official_egress",
        "stores_raw",
        "body_omitted",
    }
    if set(summary) != allowed:
        return False
    if summary.get("mode") != "observe_only":
        return False
    if summary.get("requested_mode") not in {"enabled", "observe_only"}:
        return False
    if summary.get("env_value") != "unset":
        return False
    if summary.get("eager_input_streaming_present") is not eager_input_streaming_present or not eager_input_streaming_present:
        return False
    if summary.get("direct_official_egress") is not False:
        return False
    if summary.get("stores_raw") is not False or summary.get("body_omitted") is not True:
        return False
    return True


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
