from __future__ import annotations

import ipaddress
import re
from dataclasses import dataclass, field
from typing import Any, Callable, Mapping

_OPERATOR_STATUSES = {
    "not_configured",
    "ready",
    "running",
    "guard_bypass",
    "profile_mismatch",
    "toolsearch_degraded",
    "quarantined",
}
_SAFE_STATUS_RE = re.compile(r"[^a-z0-9_.:-]+")
_EMAIL_RE = re.compile(r"[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}")
_UUID_RE = re.compile(r"\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b")
_SECRETISH_RE = re.compile(r"\b(?:sk-[A-Za-z0-9_-]{8,}|ghp_[A-Za-z0-9_]{8,}|[A-Za-z0-9+/=-]{32,})\b")
_UNSAFE_TEXT_MARKERS = (
    "authorization",
    "bearer",
    "cookie",
    "token",
    "secret",
    "password",
    "api_key",
    "apikey",
    "x-api-key",
    "prompt",
    "raw_",
    "raw body",
    "raw-body",
    "raw telemetry",
    "raw cch",
    "cch=",
    "email",
    "account_uuid",
    "org_uuid",
    "user_uuid",
    "proxy_credential",
    "api.anthropic.com",
    "platform.claude.com",
    "claude.ai",
    "claude.com",
)
_SAFE_BUCKETS = {
    "loopback",
    "private_ip",
    "link_local_ip",
    "unspecified_ip",
    "public_ip",
    "dns_name",
    "anthropic_or_claude",
    "multicast_ip",
    "unknown",
}
_ALLOWED_CONTROL_PLANE_DECISIONS = {
    "safe_intent",
    "suppress",
    "stub",
    "block",
    "shadow",
    "suppress_204",
    "stub_json",
    "block_403",
    "quarantine_block",
    "shadow_stub",
    "shadow_block",
}


@dataclass(frozen=True, slots=True)
class ClaudeCodeOperatorStatus:
    status: str
    configured: bool
    running: bool = False
    reasons: tuple[str, ...] = field(default_factory=tuple)
    profile: Mapping[str, Any] = field(default_factory=dict)
    guard: Mapping[str, Any] = field(default_factory=dict)
    toolsearch: Mapping[str, Any] = field(default_factory=dict)
    netwatch: Mapping[str, Any] = field(default_factory=dict)
    control_plane: Mapping[str, Any] = field(default_factory=dict)
    shape_healthcheck: Mapping[str, Any] = field(default_factory=dict)
    evidence: Mapping[str, Any] = field(default_factory=dict)

    def to_safe_dict(self) -> dict[str, Any]:
        return {
            "status": self.status,
            "configured": self.configured,
            "running": self.running,
            "reasons": list(self.reasons),
            "profile": dict(self.profile),
            "guard": dict(self.guard),
            "toolsearch": dict(self.toolsearch),
            "netwatch": dict(self.netwatch),
            "control_plane": dict(self.control_plane),
            "shape_healthcheck": dict(self.shape_healthcheck),
            "evidence": dict(self.evidence),
        }


def derive_claude_code_operator_status(
    state: Mapping[str, Any],
    *,
    process_alive: Callable[[int | None], bool] | None = None,
) -> ClaudeCodeOperatorStatus:
    native = state.get("claude_code_native", state)
    if not isinstance(native, Mapping):
        native = {}

    configured = bool(native.get("configured") or state.get("client") == "claude_code_native")
    profile = _safe_profile(native.get("profile"))
    guard = _safe_guard(native.get("guard"))
    toolsearch = _safe_toolsearch(native.get("toolsearch"))
    netwatch = _safe_netwatch(native.get("netwatch"))
    control_plane = _safe_control_plane(native.get("control_plane"))
    shape = _safe_shape(native.get("shape_healthcheck"))
    evidence = _safe_evidence_refs(native.get("evidence") if isinstance(native.get("evidence"), Mapping) else native)
    running = _process_running(native.get("process"), process_alive=process_alive)

    reasons: list[str] = []
    status = "not_configured"
    positive_ready = _positive_ready(profile=profile, guard=guard, netwatch=netwatch, control_plane=control_plane, shape=shape)

    profile_status = str(profile.get("status", ""))
    toolsearch_status = str(toolsearch.get("status", ""))

    if configured and _is_quarantined(native, guard=guard, control_plane=control_plane, shape=shape, toolsearch=toolsearch):
        status = "quarantined"
        reasons.extend(_quarantine_reasons(guard=guard, control_plane=control_plane, shape=shape))
    elif configured and _truthy_bypass(netwatch):
        status = "guard_bypass"
        reasons.append("direct_egress_bypass")
    elif configured and profile_status == "profile_mismatch":
        status = "profile_mismatch"
        reasons.append("profile_mismatch")
    elif configured and toolsearch_status == "toolsearch_degraded" and _toolsearch_degraded_allowed(
        profile=profile,
        guard=guard,
        netwatch=netwatch,
        control_plane=control_plane,
        shape=shape,
    ):
        status = "toolsearch_degraded"
        reasons.append("toolsearch_degraded")
        reasons.extend(str(item) for item in toolsearch.get("reasons", ()) if isinstance(item, str))
    elif configured and toolsearch_status == "toolsearch_degraded":
        status = "quarantined"
        reasons.extend(
            _missing_ready_reasons(
                profile=profile,
                guard=guard,
                netwatch=netwatch,
                control_plane=control_plane,
                shape=shape,
            )
        )
        reasons.append("toolsearch_degraded_without_native_evidence")
    elif configured and positive_ready and running:
        status = "running"
    elif configured and positive_ready:
        status = "ready"
    elif configured:
        status = "quarantined"
        reasons.extend(
            _missing_ready_reasons(
                profile=profile,
                guard=guard,
                netwatch=netwatch,
                control_plane=control_plane,
                shape=shape,
            )
        )

    if status not in _OPERATOR_STATUSES:
        status = "quarantined"
        reasons.append("unknown_status")

    return ClaudeCodeOperatorStatus(
        status=status,
        configured=configured,
        running=running,
        reasons=tuple(_dedupe(_safe_reason(reason) for reason in reasons)),
        profile=profile,
        guard=guard,
        toolsearch=toolsearch,
        netwatch=netwatch,
        control_plane=control_plane,
        shape_healthcheck=shape,
        evidence=evidence,
    )


def _safe_evidence_refs(value: Any) -> dict[str, str]:
    src = value if isinstance(value, Mapping) else {}
    allowed = (
        "local_session_ref",
        "guard_summary_ref",
        "netwatch_summary_ref",
        "profile_ref",
        "mock_healthcheck_ref",
        "state_timeline_ref",
        "operator_action_summary_ref",
        "sensitive_scan_summary_ref",
    )
    return _drop_empty({key: _safe_identifier(src.get(key)) for key in allowed})


def _process_running(process: Any, *, process_alive: Callable[[int | None], bool] | None) -> bool:
    if not isinstance(process, Mapping):
        return False
    pid = _int_or_none(process.get("pid"))
    if process_alive is not None:
        return bool(process_alive(pid))
    return bool(process.get("running")) or bool(pid and str(process.get("status", "")).lower() in {"running", "active"})


def _safe_profile(value: Any) -> dict[str, Any]:
    src = value if isinstance(value, Mapping) else {}
    if not src:
        return {}
    return _drop_empty(
        {
            "status": _safe_status(src.get("status")) if "status" in src else "",
            "profile_id": _safe_identifier(src.get("profile_id")),
            "capability_profile_id": _safe_identifier(src.get("capability_profile_id")),
            "persona_profile_id": _safe_identifier(src.get("persona_profile_id")),
            "version_family": _safe_identifier(src.get("version_family") or src.get("claude_code_version_family")),
            "capture_mode": _safe_identifier(src.get("capture_mode")),
        }
    )


def _safe_guard(value: Any) -> dict[str, Any]:
    src = value if isinstance(value, Mapping) else {}
    if not src:
        return {}
    status = _safe_status(src.get("status")) if "status" in src else ""
    active = status in {"ready", "running", "active"} or src.get("active") is True
    return _drop_empty(
        {
            "status": status,
            "active": active,
            "attested": _bool_or_none(src.get("attested") if "attested" in src else src.get("guard_attested")),
            "mode": _safe_identifier(src.get("mode")),
            "loopback": _bool_or_none(src.get("loopback")),
            "connect_mode": _safe_identifier(src.get("connect_mode")),
        }
    )


def _safe_toolsearch(value: Any) -> dict[str, Any]:
    src = value if isinstance(value, Mapping) else {}
    if not src:
        return {}
    return _drop_empty(
        {
            "status": _safe_status(src.get("status")) if "status" in src else "",
            "mode": _safe_identifier(src.get("mode")),
            "env_value": _safe_identifier(src.get("env_value")),
            "degraded": _bool_or_none(src.get("degraded")),
            "reasons": [_safe_reason(str(item)) for item in src.get("reasons", ()) if isinstance(item, str)][:12],
        }
    )


def _safe_netwatch(value: Any) -> dict[str, Any]:
    src = value if isinstance(value, Mapping) else {}
    if not src:
        return {}
    summary = src.get("summary") if isinstance(src.get("summary"), Mapping) else src
    if not isinstance(summary, Mapping):
        summary = {}
    buckets = summary.get("remote_host_buckets")
    safe_buckets: dict[str, int] = {}
    if isinstance(buckets, Mapping):
        for key, count in buckets.items():
            bucket = str(key)
            bucket = _safe_bucket(bucket)
            numeric = _int_or_none(count)
            if numeric is not None:
                safe_buckets[bucket] = safe_buckets.get(bucket, 0) + numeric
    return _drop_empty(
        {
            "status": _safe_status(src.get("status")) if "status" in src else "",
            "connection_count": _int_or_none(summary.get("connection_count")),
            "potential_guard_bypass_count": _int_or_none(summary.get("potential_guard_bypass_count")),
            "official_or_public_bypass_count": _int_or_none(summary.get("official_or_public_bypass_count")),
            "loopback_guard_connection_count": _int_or_none(summary.get("loopback_guard_connection_count")),
            "remote_host_buckets": safe_buckets,
            "stores_payload": _bool_or_none(summary.get("stores_payload")),
            "stores_headers": _bool_or_none(summary.get("stores_headers")),
        }
    )


def _safe_bucket(value: str) -> str:
    text = value.strip().lower()
    if text in _SAFE_BUCKETS:
        return text
    if any(marker in text for marker in ("anthropic.com", "claude.ai", "claude.com")):
        return "anthropic_or_claude"
    try:
        parsed = ipaddress.ip_address(text.strip("[]"))
    except ValueError:
        if "." in text:
            return "dns_name"
        return "unknown"
    if parsed.is_loopback:
        return "loopback"
    if parsed.is_private:
        return "private_ip"
    return "public_ip"


def _safe_control_plane(value: Any) -> dict[str, Any]:
    src = value if isinstance(value, Mapping) else {}
    return _drop_empty(
        {
            "safe_intent": _bool_or_none(src.get("safe_intent")),
            "decision": _safe_identifier(src.get("decision")),
            "status": _int_or_none(src.get("status")),
            "stores_raw": _bool_or_none(src.get("stores_raw")),
            "messages_signing_reused": _bool_or_none(src.get("messages_signing_reused")),
            "unknown_drift": _bool_or_none(src.get("unknown_drift")),
        }
    )


def _safe_shape(value: Any) -> dict[str, Any]:
    src = value if isinstance(value, Mapping) else {}
    if not src:
        return {}
    failed = src.get("failed_fields")
    safe_failed = [_safe_identifier(item) for item in failed if isinstance(item, str)] if isinstance(failed, (list, tuple)) else []
    return _drop_empty(
        {
            "status": _safe_status(src.get("status")) if "status" in src else "",
            "passed": _int_or_none(src.get("passed")),
            "denominator": _int_or_none(src.get("denominator")),
            "failed_fields": safe_failed[:24],
            "fresh": _bool_or_none(src.get("fresh")),
        }
    )


def _positive_ready(
    *,
    profile: Mapping[str, Any],
    guard: Mapping[str, Any],
    netwatch: Mapping[str, Any],
    control_plane: Mapping[str, Any],
    shape: Mapping[str, Any],
) -> bool:
    return _base_safety_ready(profile=profile, guard=guard, netwatch=netwatch, control_plane=control_plane) and str(
        shape.get("status")
    ) == "pass"


def _toolsearch_degraded_allowed(
    *,
    profile: Mapping[str, Any],
    guard: Mapping[str, Any],
    netwatch: Mapping[str, Any],
    control_plane: Mapping[str, Any],
    shape: Mapping[str, Any],
) -> bool:
    if not _base_safety_ready(profile=profile, guard=guard, netwatch=netwatch, control_plane=control_plane):
        return False
    return str(shape.get("status")) == "pass" or _shape_failure_toolsearch_only(shape)


def _base_safety_ready(
    *,
    profile: Mapping[str, Any],
    guard: Mapping[str, Any],
    netwatch: Mapping[str, Any],
    control_plane: Mapping[str, Any],
) -> bool:
    return (
        profile.get("status") == "ready"
        and guard.get("active") is True
        and guard.get("attested") is True
        and bool(netwatch)
        and not _truthy_bypass(netwatch)
        and control_plane.get("safe_intent") is True
        and _control_plane_decision_allowed(control_plane)
        and control_plane.get("messages_signing_reused") is False
        and control_plane.get("stores_raw") is False
        and netwatch.get("stores_payload") is False
        and netwatch.get("stores_headers") is False
    )


def _missing_ready_reasons(
    *,
    profile: Mapping[str, Any],
    guard: Mapping[str, Any],
    netwatch: Mapping[str, Any],
    control_plane: Mapping[str, Any],
    shape: Mapping[str, Any],
) -> list[str]:
    reasons: list[str] = []
    if profile.get("status") != "ready":
        reasons.append("profile_not_ready")
    if guard.get("active") is not True:
        reasons.append("guard_not_ready")
    if guard.get("attested") is not True:
        reasons.append("guard_attestation_missing")
    if control_plane.get("safe_intent") is not True:
        reasons.append("control_plane_safe_intent_missing")
    if not _control_plane_decision_allowed(control_plane):
        reasons.append("control_plane_decision_not_allowed")
    if control_plane.get("messages_signing_reused") is not False:
        reasons.append("control_plane_signing_boundary_missing")
    if control_plane.get("stores_raw") is not False:
        reasons.append("control_plane_raw_boundary_missing")
    if netwatch.get("stores_payload") is not False or netwatch.get("stores_headers") is not False:
        reasons.append("netwatch_raw_boundary_missing")
    if str(shape.get("status")) != "pass":
        reasons.append("shape_healthcheck_missing")
    if not netwatch:
        reasons.append("netwatch_summary_missing")
    return reasons or ["native_evidence_incomplete"]


def _truthy_bypass(netwatch: Mapping[str, Any]) -> bool:
    potential = _int_or_none(netwatch.get("potential_guard_bypass_count")) or 0
    official = _int_or_none(netwatch.get("official_or_public_bypass_count")) or 0
    buckets = netwatch.get("remote_host_buckets")
    has_bypass_bucket = False
    if isinstance(buckets, Mapping):
        has_bypass_bucket = any(
            bucket in buckets and int(buckets[bucket]) > 0
            for bucket in ("anthropic_or_claude", "public_ip", "dns_name")
        )
    return potential > 0 or official > 0 or has_bypass_bucket


def _is_quarantined(
    native: Mapping[str, Any],
    *,
    guard: Mapping[str, Any],
    control_plane: Mapping[str, Any],
    shape: Mapping[str, Any],
    toolsearch: Mapping[str, Any],
) -> bool:
    if _safe_status(native.get("status")) == "quarantined" or native.get("quarantined") is True:
        return True
    if guard.get("attested") is False:
        return True
    if control_plane.get("safe_intent") is False:
        return True
    if not _control_plane_decision_allowed(control_plane):
        return True
    if control_plane.get("stores_raw") is True or control_plane.get("messages_signing_reused") is True:
        return True
    if control_plane.get("unknown_drift") is True:
        return True
    if str(shape.get("status", "")) == "fail":
        return str(toolsearch.get("status", "")) != "toolsearch_degraded" or not _shape_failure_toolsearch_only(shape)
    return False


def _control_plane_decision_allowed(control_plane: Mapping[str, Any]) -> bool:
    decision = control_plane.get("decision")
    if decision in (None, ""):
        return True
    return str(decision) in _ALLOWED_CONTROL_PLANE_DECISIONS


def _shape_failure_toolsearch_only(shape: Mapping[str, Any]) -> bool:
    failed = shape.get("failed_fields")
    if not isinstance(failed, list) or not failed:
        return False
    return set(str(item) for item in failed) <= {"tool_search_fixture"}


def _quarantine_reasons(*, guard: Mapping[str, Any], control_plane: Mapping[str, Any], shape: Mapping[str, Any]) -> list[str]:
    reasons: list[str] = []
    if guard.get("attested") is False:
        reasons.append("guard_attestation_failed")
    if control_plane.get("safe_intent") is False:
        reasons.append("control_plane_unsafe_intent")
    if not _control_plane_decision_allowed(control_plane):
        reasons.append("control_plane_decision_not_allowed")
    if control_plane.get("stores_raw") is True:
        reasons.append("control_plane_raw_storage")
    if control_plane.get("messages_signing_reused") is True:
        reasons.append("control_plane_messages_signing_reused")
    if control_plane.get("unknown_drift") is True:
        reasons.append("control_plane_unknown_drift")
    if str(shape.get("status", "")) == "fail":
        reasons.append("shape_healthcheck_failed")
    return reasons or ["quarantined"]


def _safe_status(value: Any) -> str:
    text = _safe_identifier(value)
    return text or "unknown"


def _safe_identifier(value: Any) -> str:
    if value is None:
        return ""
    text = str(value).strip().lower()
    if not text:
        return ""
    if _unsafe_text(text):
        return "redacted_detail"
    return _SAFE_STATUS_RE.sub("_", text)[:96].strip("_")


def _safe_reason(value: str) -> str:
    text = value.strip().lower()
    if not text:
        return "unknown"
    if _unsafe_text(text):
        return "redacted_detail"
    return _SAFE_STATUS_RE.sub("_", text)[:96].strip("_") or "unknown"


def _unsafe_text(value: str) -> bool:
    lower = value.lower()
    return (
        _EMAIL_RE.search(value) is not None
        or _UUID_RE.search(value) is not None
        or _SECRETISH_RE.search(value) is not None
        or any(marker in lower for marker in _UNSAFE_TEXT_MARKERS)
    )


def _bool_or_none(value: Any) -> bool | None:
    if isinstance(value, bool):
        return value
    return None


def _int_or_none(value: Any) -> int | None:
    if isinstance(value, bool):
        return None
    if isinstance(value, int) and value >= 0:
        return value
    return None


def _drop_empty(value: Mapping[str, Any]) -> dict[str, Any]:
    return {key: item for key, item in value.items() if item not in (None, "", [], {})}


def _dedupe(values: list[str]) -> list[str]:
    seen: set[str] = set()
    out: list[str] = []
    for value in values:
        if value not in seen:
            seen.add(value)
            out.append(value)
    return out
