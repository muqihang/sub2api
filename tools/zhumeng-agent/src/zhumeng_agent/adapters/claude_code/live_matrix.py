from __future__ import annotations

import hashlib
import ipaddress
import json
import re
from dataclasses import dataclass
from pathlib import Path
from typing import Mapping
from urllib.parse import urlparse


class CP8LiveMatrixError(RuntimeError):
    pass


REQUIRED_CP8_SCENARIOS = (
    "claude_native",
    "gpt_bridge",
    "deepseek_bridge",
    "subagent",
    "claude_deepseek_subagent_claude",
    "manual_provider_switch",
    "toolsearch_mcp",
    "workflow",
    "long_context",
    "interruption",
    "cache_account_audit",
    "netwatch_bypass",
)

_BRIDGE_SCENARIOS = frozenset({"gpt_bridge", "deepseek_bridge"})
_SENSITIVE_ARTIFACT_RE = re.compile(r"(Bearer\s+|raw-token|api[_-]?key|cookie|setup[_-]?token|CCH|oauth|Authorization)", re.IGNORECASE)

_DOC_SOURCES = frozenset({
    "deepseek_anthropic_api",
    "deepseek_kv_cache",
    "deepseek_thinking_mode",
    "zai_claude_code",
    "zai_latest_model",
    "kimi_agent_support",
    "openai_reasoning",
    "openai_prompt_caching",
    "anthropic_messages",
})
_STRICT_LIVE_PROVIDER_HOSTS = {
    "claude": frozenset({"api.anthropic.com"}),
    "openai": frozenset({"api.openai.com"}),
    "deepseek": frozenset({"api.deepseek.com"}),
}


@dataclass(frozen=True, slots=True)
class CP8ScenarioResult:
    name: str
    status: str
    issues: tuple[str, ...]
    live_provider_verified: bool
    route: str
    client_type: str

    def to_dict(self) -> dict[str, object]:
        return {
            "name": self.name,
            "status": self.status,
            "issues": list(self.issues),
            "live_provider_verified": self.live_provider_verified,
            "route": self.route,
            "client_type": self.client_type,
        }


@dataclass(frozen=True, slots=True)
class CP8LiveMatrixResult:
    checkpoint: str
    status: str
    release_gate: str
    scenario_results: Mapping[str, CP8ScenarioResult]
    missing: tuple[str, ...]
    failed: tuple[str, ...]
    native_egress_by_route: Mapping[str, int]
    bridge_egress_by_route: Mapping[str, int]
    bridge_egress_by_client_type: Mapping[str, int]
    strict_live_ready: bool
    official_docs_checked: bool
    artifact_evidence_verified: bool

    @property
    def scenarios(self) -> tuple[str, ...]:
        return tuple(self.scenario_results)

    def to_dict(self) -> dict[str, object]:
        return {
            "checkpoint": self.checkpoint,
            "status": self.status,
            "release_gate": self.release_gate,
            "scenarios": {name: result.to_dict() for name, result in self.scenario_results.items()},
            "missing": list(self.missing),
            "failed": list(self.failed),
            "native_egress_by_route": dict(self.native_egress_by_route),
            "bridge_egress_by_route": dict(self.bridge_egress_by_route),
            "bridge_egress_by_client_type": dict(self.bridge_egress_by_client_type),
            "summary": {
                "required_scenarios_passed": not self.missing and not self.failed,
                "strict_live_ready": self.strict_live_ready,
                "official_docs_checked": self.official_docs_checked,
                "artifact_evidence_verified": self.artifact_evidence_verified,
            },
        }


def verify_cp8_live_matrix(
    payload: Mapping[str, object],
    *,
    strict_live: bool = False,
    evidence_root: Path | str | None = None,
) -> CP8LiveMatrixResult:
    if not isinstance(payload, Mapping):
        raise CP8LiveMatrixError("CP8 live matrix evidence must be a JSON object")
    if payload.get("checkpoint") != "CP8":
        raise CP8LiveMatrixError("CP8 live matrix evidence must declare checkpoint=CP8")
    if payload.get("schema_version") != "cp8-live-matrix-v1":
        raise CP8LiveMatrixError("CP8 live matrix evidence requires schema_version=cp8-live-matrix-v1")
    root = Path(evidence_root).expanduser() if evidence_root is not None else None
    scenarios_raw = payload.get("scenarios")
    if not isinstance(scenarios_raw, Mapping):
        raise CP8LiveMatrixError("CP8 live matrix evidence requires scenarios")

    invalid_scenarios = tuple(name for name in REQUIRED_CP8_SCENARIOS if name in scenarios_raw and not isinstance(scenarios_raw.get(name), Mapping))
    missing = tuple(name for name in REQUIRED_CP8_SCENARIOS if name not in scenarios_raw)
    docs_issues = _official_docs_issues(payload.get("official_docs_snapshot"))
    results: dict[str, CP8ScenarioResult] = {}
    native_egress: dict[str, int] = {}
    bridge_egress: dict[str, int] = {}
    bridge_egress_by_client_type: dict[str, int] = {}

    for name in REQUIRED_CP8_SCENARIOS:
        raw = scenarios_raw.get(name)
        if not isinstance(raw, Mapping):
            if name in invalid_scenarios:
                results[name] = CP8ScenarioResult(
                    name=name,
                    status="fail",
                    issues=("scenario evidence must be an object",),
                    live_provider_verified=False,
                    route="",
                    client_type="",
                )
            continue
        result = _verify_scenario(name, raw, strict_live=strict_live, evidence_root=root)
        results[name] = result
        if name == "claude_native" and result.status == "pass":
            _add_count(native_egress, "claude_code_native", _int(raw.get("native_egress_count")))
        if name in _BRIDGE_SCENARIOS and result.status == "pass":
            bridge_count = _int(raw.get("bridge_request_count"))
            _add_count(bridge_egress, result.route, bridge_count)
            _add_count(bridge_egress_by_client_type, result.client_type, bridge_count)

    failed = [name for name, result in results.items() if result.status != "pass"]
    if strict_live and not _strict_live_provenance_verified(payload, evidence_root=root):
        failed.append("live_provenance")
    artifact_evidence_verified = all(
        isinstance(scenarios_raw.get(name), Mapping) and bool(scenarios_raw[name].get("artifact_refs"))
        for name in REQUIRED_CP8_SCENARIOS
        if name in scenarios_raw
    ) and not any(
        issue.startswith("artifact ")
        for result in results.values()
        for issue in result.issues
    )
    if docs_issues:
        failed.append("official_docs")
    strict_live_ready = (
        len(results) == len(REQUIRED_CP8_SCENARIOS)
        and all(result.live_provider_verified for result in results.values())
        and _strict_live_provenance_verified(payload, evidence_root=root)
        and not missing
        and not failed
    )
    if strict_live and not strict_live_ready:
        release_gate = "blocked_missing_external_live"
    elif not missing and not failed:
        release_gate = "external_live_passed" if strict_live_ready else "manual_external_live_required"
    else:
        release_gate = "blocked_cp8_matrix_failed"
    status = "pass" if not missing and not failed and (not strict_live or strict_live_ready) else "fail"
    return CP8LiveMatrixResult(
        checkpoint="CP8",
        status=status,
        release_gate=release_gate,
        scenario_results=results,
        missing=missing,
        failed=tuple(dict.fromkeys(failed)),
        native_egress_by_route=native_egress,
        bridge_egress_by_route=bridge_egress,
        bridge_egress_by_client_type=bridge_egress_by_client_type,
        strict_live_ready=strict_live_ready,
        official_docs_checked=not docs_issues,
        artifact_evidence_verified=artifact_evidence_verified,
    )


def _verify_scenario(
    name: str,
    raw: Mapping[str, object],
    *,
    strict_live: bool,
    evidence_root: Path | None,
) -> CP8ScenarioResult:
    issues: list[str] = []
    live = bool(raw.get("live_provider_verified"))
    if strict_live and not live:
        issues.append("external live provider evidence missing")
    if raw.get("status") != "pass":
        issues.append("scenario did not pass")
    if raw.get("raw_sensitive_stored") is not False:
        issues.append("raw sensitive data storage must be false")
    issues.extend(_artifact_ref_issues(raw.get("artifact_refs"), evidence_root=evidence_root))

    route = str(raw.get("route") or "")
    client_type = str(raw.get("client_type") or "")
    native_count = _int(raw.get("native_egress_count"))
    formal = bool(raw.get("formal_pool_allowed"))
    native_attestation = bool(raw.get("native_attestation"))

    models = _str_list(raw.get("models"))

    if name == "claude_native":
        if not any("opus" in model for model in models) or not any("sonnet" in model for model in models):
            issues.append("Claude native scenario must cover Opus and Sonnet models")
        if client_type != "claude_code_native" or route != "claude_code_native":
            issues.append("Claude native scenario must use claude_code_native route/client_type")
        if not formal or not native_attestation or native_count <= 0:
            issues.append("Claude native scenario must prove formal-pool attested native egress")
        if raw.get("shape_equality_verified") is not True:
            issues.append("Claude native shape equality must be verified")
    elif name in _BRIDGE_SCENARIOS:
        if not client_type.startswith("claude_code_bridge_") or formal or native_attestation or native_count != 0:
            issues.append("bridge scenario has native contamination")
        if _int(raw.get("bridge_request_count")) <= 0:
            issues.append("bridge scenario must prove bridge egress")
        if name == "gpt_bridge":
            if route != "openai_bridge" or client_type != "claude_code_bridge_openai" or not all(model.startswith("gpt-") for model in models) or len(models) < 2:
                issues.append("GPT bridge scenario must cover GPT main/fast OpenAI bridge models")
        if name == "deepseek_bridge":
            if route != "deepseek_bridge" or client_type != "claude_code_bridge_deepseek":
                issues.append("DeepSeek bridge scenario must use DeepSeek bridge route/client_type")
            has_deepseek_pro = "deepseek-v4-pro" in models or "deepseek-v4-pro[1m]" in models
            if not has_deepseek_pro or "deepseek-v4-flash" not in models:
                issues.append("DeepSeek bridge scenario must cover Pro and Flash models")
            if raw.get("preferred_claude_code_protocol") != "anthropic_messages" or raw.get("fallback_reason"):
                issues.append("DeepSeek must default to anthropic_messages unless a fixture-backed fallback_reason is present")
            if raw.get("reasoning_safety") != "foreign_reasoning_never_native_replay":
                issues.append("DeepSeek foreign reasoning safety evidence missing")
    elif name == "subagent":
        if raw.get("default_model_policy") != "inherit_first" or raw.get("provider_local_fast_model") is not True:
            issues.append("subagent default must be inherit-first with provider-local fast mapping")
        if _int(raw.get("unexpected_formal_pool_egress")) != 0:
            issues.append("subagent caused unexpected formal pool egress")
    elif name == "claude_deepseek_subagent_claude":
        artifacts = set(raw.get("returned_artifacts") if isinstance(raw.get("returned_artifacts"), list) else [])
        if not {"safe_final_answer", "safe_tool_result", "evidence_summary"}.issubset(artifacts):
            issues.append("cross-provider subagent must return only safe artifacts")
        if raw.get("child_hidden_reasoning_replayed") is not False or raw.get("provider_private_history_replayed") is not False:
            issues.append("cross-provider subagent replayed private child history")
        if raw.get("anthropic_body_foreign_markers") not in ([], ()):  # JSON list or tuple in tests
            issues.append("foreign markers reached Anthropic body")
    elif name == "manual_provider_switch":
        for field in ("foreign_reasoning_in_anthropic", "claude_private_metadata_in_bridge"):
            if raw.get(field) is not False:
                issues.append(f"{field} must be false")
        for field in ("role_order_tool_pairing_verified", "same_provider_preserved"):
            if raw.get(field) is not True:
                issues.append(f"{field} must be true")
    elif name == "toolsearch_mcp":
        for field in ("mcp_tools_exercised", "bridge_tool_use_sse_golden_diff", "input_json_delta_verified", "stop_reason_tool_use_verified"):
            if raw.get(field) is not True:
                issues.append(f"{field} must be true")
    elif name == "workflow":
        if raw.get("active_profile_dynamic_resolution") is not True or raw.get("workflow_alias_resolved_provider_local") is not True:
            issues.append("workflow must dynamically resolve active provider profile")
        if _int(raw.get("non_claude_background_native_egress")) != 0 or raw.get("hardcoded_claude_model_consumed") is not False:
            issues.append("workflow/background tasks consumed Claude formal pool")
    elif name == "long_context":
        if raw.get("long_context_exercised") is not True or raw.get("cache_prefix_stable") is not True or raw.get("stable_prefix_reordered") is not False:
            issues.append("long context/cache prefix evidence invalid")
    elif name == "interruption":
        if raw.get("interruption_exercised") is not True or raw.get("no_partial_tool_history_replayed") is not True or raw.get("mid_tool_loop_fail_closed_or_summarized") is not True:
            issues.append("interruption/mid-tool-loop evidence invalid")
    elif name == "cache_account_audit":
        for field in ("safe_summary_hash_stable", "safe_tool_result_hash_stable", "usage_accounting_split_by_route", "audit_summary_only"):
            if raw.get(field) is not True:
                issues.append(f"{field} must be true")
        if _int(raw.get("ttl_fast_switch_boundary_miss_count")) > 1 or raw.get("stable_prefix_invalidated") is not False:
            issues.append("cache fast-switch boundary cost exceeded")
    elif name == "netwatch_bypass":
        if _int(raw.get("potential_guard_bypass_count")) != 0 or _int(raw.get("official_or_public_bypass_count")) != 0:
            issues.append("netwatch detected guard bypass")
        if raw.get("stores_payload") is not False or raw.get("stores_headers") is not False:
            issues.append("netwatch captured raw payload or headers")

    return CP8ScenarioResult(
        name=name,
        status="pass" if not issues else "fail",
        issues=tuple(issues),
        live_provider_verified=live,
        route=route,
        client_type=client_type,
    )






def _strict_live_provenance_verified(payload: Mapping[str, object], *, evidence_root: Path | None) -> bool:
    if payload.get("mode") != "external_provider_live_matrix":
        return False
    provenance = payload.get("live_provenance")
    if not isinstance(provenance, Mapping):
        return False
    if provenance.get("credential_backed") is not True or provenance.get("loopback_only") is not False:
        return False
    run_id = str(provenance.get("run_id") or "").strip()
    if not run_id:
        return False
    providers = provenance.get("providers")
    if not isinstance(providers, Mapping):
        return False
    required = {
        "claude": "formal_pool",
        "openai": "bridge_pool",
        "deepseek": "bridge_pool",
    }
    for provider, scope in required.items():
        raw = providers.get(provider)
        if not isinstance(raw, Mapping):
            return False
        if raw.get("credential_scope") != scope or raw.get("live_provider_verified") is not True:
            return False
        endpoint = str(raw.get("endpoint") or "")
        if not _external_live_endpoint(provider, endpoint):
            return False
        if _artifact_ref_issues(raw.get("artifact_refs"), evidence_root=evidence_root):
            return False
        if not _strict_live_provider_artifacts_verified(
            raw.get("artifact_refs"),
            evidence_root=evidence_root,
            provider=provider,
            credential_scope=scope,
            endpoint=endpoint,
            run_id=run_id,
        ):
            return False
    return True


def _external_live_endpoint(provider: str, endpoint: str) -> bool:
    parsed = urlparse(endpoint)
    if parsed.scheme != "https":
        return False
    host = (parsed.hostname or "").strip().lower()
    if not host or host == "localhost" or host.endswith(".localhost"):
        return False
    allowed_hosts = _STRICT_LIVE_PROVIDER_HOSTS.get(provider)
    if not allowed_hosts or host not in allowed_hosts:
        return False
    try:
        ip = ipaddress.ip_address(host)
    except ValueError:
        return True
    return not (ip.is_loopback or ip.is_private or ip.is_link_local or ip.is_unspecified)


def _strict_live_provider_artifacts_verified(
    value: object,
    *,
    evidence_root: Path | None,
    provider: str,
    credential_scope: str,
    endpoint: str,
    run_id: str,
) -> bool:
    if evidence_root is None or not isinstance(value, list):
        return False
    root = evidence_root.resolve(strict=False)
    for raw in value:
        if not isinstance(raw, Mapping):
            continue
        rel = raw.get("path")
        if not isinstance(rel, str) or not rel or rel.startswith("/") or ".." in Path(rel).parts:
            continue
        path = (root / rel).resolve(strict=False)
        if root not in (path, *path.parents) or not path.exists() or not path.is_file():
            continue
        try:
            payload = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError, UnicodeDecodeError):
            continue
        if not isinstance(payload, Mapping):
            continue
        host = (urlparse(endpoint).hostname or "").lower()
        artifact_endpoint = str(payload.get("endpoint") or "")
        artifact_host = str(payload.get("host") or urlparse(artifact_endpoint).hostname or "").lower()
        if (
            payload.get("schema_version") == "cp8-live-provider-provenance-v1"
            and payload.get("checkpoint") == "CP8"
            and str(payload.get("run_id") or "") == run_id
            and str(payload.get("provider") or "") == provider
            and str(payload.get("credential_scope") or "") == credential_scope
            and payload.get("external_live_verified") is True
            and payload.get("loopback") is False
            and artifact_host == host
            and _external_live_endpoint(provider, artifact_endpoint or endpoint)
            and (str(payload.get("upstream_request_id") or "").strip() or _int(payload.get("response_status")) > 0)
        ):
            return True
    return False

def _artifact_ref_issues(value: object, *, evidence_root: Path | None) -> tuple[str, ...]:
    if not isinstance(value, list) or not value:
        return ("artifact refs missing",)
    if evidence_root is None:
        return ("artifact evidence root missing",)
    issues: list[str] = []
    root = evidence_root.resolve(strict=False)
    for index, raw in enumerate(value):
        if not isinstance(raw, Mapping):
            issues.append(f"artifact ref {index} invalid")
            continue
        rel = raw.get("path")
        expected = str(raw.get("sha256") or "")
        if not isinstance(rel, str) or not rel or rel.startswith("/") or ".." in Path(rel).parts:
            issues.append(f"artifact ref {index} path invalid")
            continue
        path = (root / rel).resolve(strict=False)
        if root not in (path, *path.parents):
            issues.append(f"artifact ref {index} escapes evidence root")
            continue
        if not path.exists() or not path.is_file():
            issues.append(f"artifact ref {rel} missing")
            continue
        actual = "sha256:" + hashlib.sha256(path.read_bytes()).hexdigest()
        if expected != actual:
            issues.append(f"artifact hash mismatch: {rel}")
        text = path.read_text(encoding="utf-8", errors="ignore")
        if _SENSITIVE_ARTIFACT_RE.search(text):
            issues.append(f"artifact contains sensitive marker: {rel}")
        if raw.get("sensitive_scan_clean") is not True:
            issues.append(f"artifact sensitive scan not clean: {rel}")
    return tuple(issues)

def _official_docs_issues(value: object) -> tuple[str, ...]:
    if not isinstance(value, Mapping):
        return ("official docs snapshot missing",)
    sources = value.get("sources")
    observations = value.get("observations")
    issues: list[str] = []
    if not isinstance(sources, Mapping) or not _DOC_SOURCES.issubset({str(key) for key in sources}):
        issues.append("official docs sources incomplete")
    if not isinstance(observations, Mapping):
        return tuple([*issues, "official docs observations missing"])
    deepseek = observations.get("deepseek") if isinstance(observations.get("deepseek"), Mapping) else {}
    zai = observations.get("zai_glm") if isinstance(observations.get("zai_glm"), Mapping) else {}
    kimi = observations.get("kimi") if isinstance(observations.get("kimi"), Mapping) else {}
    openai = observations.get("openai") if isinstance(observations.get("openai"), Mapping) else {}
    if "deepseek-v4-pro" not in _str_list(deepseek.get("models")) or deepseek.get("preferred_claude_code_protocol") != "anthropic_messages":
        issues.append("DeepSeek official docs snapshot is stale or not Anthropic-first")
    deepseek_context_windows = deepseek.get("context_windows") if isinstance(deepseek.get("context_windows"), Mapping) else {}
    if "deepseek-v4-pro[1m]" not in _str_list(deepseek.get("models")) or _int(deepseek_context_windows.get("deepseek-v4-pro[1m]")) < 1_000_000:
        issues.append("DeepSeek official docs snapshot is missing 1M Claude Code model metadata")
    if deepseek.get("cache_observability") != "prompt_cache_hit_tokens/prompt_cache_miss_tokens":
        issues.append("DeepSeek official docs cache observability snapshot is stale")
    latest_glm = str(zai.get("latest_coding_model") or "")
    if not latest_glm.startswith("glm-5.") or latest_glm == "glm-4.6":
        issues.append("GLM official docs snapshot is stale")
    if not any(model.startswith("kimi-k2.7-code") for model in _str_list(kimi.get("coding_models"))):
        issues.append("Kimi official docs snapshot is stale")
    if kimi.get("anthropic_base_url") != "https://api.moonshot.ai/anthropic" or kimi.get("openai_base_url") != "https://api.moonshot.ai/v1":
        issues.append("Kimi official docs endpoint snapshot is stale")
    if kimi.get("prompt_cache_key") is not True or kimi.get("cache_usage_field") != "usage.cached_tokens":
        issues.append("Kimi official docs cache snapshot is stale")
    if not str(openai.get("recommended_model") or "").startswith("gpt-") or openai.get("preferred_api") != "responses":
        issues.append("OpenAI official docs snapshot is stale")
    if openai.get("cache_observability") != "usage.prompt_tokens_details.cached_tokens":
        issues.append("OpenAI official docs cache observability snapshot is stale")
    return tuple(issues)


def _str_list(value: object) -> tuple[str, ...]:
    if not isinstance(value, list):
        return ()
    return tuple(str(item) for item in value if isinstance(item, str))


def _int(value: object) -> int:
    if isinstance(value, bool):
        return int(value)
    if isinstance(value, int):
        return value
    return 0


def _add_count(target: dict[str, int], key: str, value: int) -> None:
    if not key or value <= 0:
        return
    target[key] = target.get(key, 0) + value
