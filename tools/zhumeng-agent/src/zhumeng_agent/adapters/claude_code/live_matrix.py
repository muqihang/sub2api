from __future__ import annotations

import base64
import copy
import hashlib
import hmac
import secrets
import time
import ipaddress
import json
import os
import re
import urllib.error
import urllib.request
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
_SENSITIVE_ARTIFACT_RE = re.compile(
    r"(Bearer(?:\s+|_+)|sk-[A-Za-z0-9][A-Za-z0-9_.:-]{6,}|raw-token|api[_-]?key|cookie|setup[_-]?token|CCH|oauth|Authorization)",
    re.IGNORECASE,
)
_SENSITIVE_INLINE_KEYS = frozenset({
    "api_key",
    "apikey",
    "access_token",
    "authorization",
    "auth_token",
    "body",
    "client_secret",
    "cookie",
    "cookies",
    "headers",
    "input",
    "messages",
    "output",
    "payload",
    "prompt",
    "raw",
    "raw_body",
    "raw_payload",
    "raw_prompt",
    "raw_request",
    "raw_response",
    "refresh_token",
    "request",
    "request_body",
    "request_headers",
    "request_payload",
    "response",
    "response_body",
    "response_headers",
    "response_payload",
    "secret",
    "session_token",
    "token",
    "x_api_key",
})

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
_STRICT_LIVE_PROVIDER_ENDPOINTS = {
    "claude": frozenset({"https://api.anthropic.com/v1/messages"}),
    "openai": frozenset({"https://api.openai.com/v1/responses"}),
    "deepseek": frozenset({"https://api.deepseek.com/anthropic/v1/messages"}),
}
_STRICT_LIVE_PROVIDER_MODEL_ALLOWLIST = {
    "claude": frozenset({"claude-sonnet-4-6", "claude-opus-4-8"}),
    "openai": frozenset({"gpt-5.5", "gpt-5.4-mini"}),
    "deepseek": frozenset({"deepseek-v4-pro", "deepseek-v4-pro[1m]", "deepseek-v4-flash"}),
}
_STRICT_LIVE_SCENARIO_PROVIDERS: dict[str, tuple[str, ...]] = {
    "claude_native": ("claude",),
    "gpt_bridge": ("openai",),
    "deepseek_bridge": ("deepseek",),
    "subagent": ("openai", "deepseek"),
    "claude_deepseek_subagent_claude": ("claude", "deepseek"),
    "manual_provider_switch": ("claude", "openai", "deepseek"),
    "toolsearch_mcp": ("openai", "deepseek"),
    "workflow": ("openai", "deepseek"),
    "long_context": ("deepseek",),
    "interruption": ("claude", "openai", "deepseek"),
    "cache_account_audit": ("claude", "openai", "deepseek"),
    "netwatch_bypass": ("claude", "openai", "deepseek"),
}
_WORKFLOW_BACKGROUND_TASKS = ("title", "compact", "summary", "probe", "fast", "simple", "haiku")


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
        result = _verify_scenario(name, raw, strict_live=strict_live, evidence_root=root, payload=payload, run_id=_strict_live_run_id(payload) if strict_live else "")
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


def assemble_cp8_external_live_matrix_evidence(
    payload: Mapping[str, object],
    provenance: Mapping[str, object],
) -> dict[str, object]:
    """Bind separately collected provider provenance without promoting scenario evidence."""
    if not isinstance(payload, Mapping):
        raise CP8LiveMatrixError("CP8 live matrix evidence must be a JSON object")
    if not isinstance(provenance, Mapping):
        raise CP8LiveMatrixError("CP8 live provider provenance must be a JSON object")
    issue = _sensitive_inline_evidence_issue(payload, path="matrix") or _sensitive_inline_evidence_issue(provenance, path="provenance")
    if issue:
        raise CP8LiveMatrixError("sensitive inline CP8 live matrix evidence is not allowed: " + issue)
    assembled = copy.deepcopy(dict(payload))
    assembled["mode"] = "external_provider_live_matrix"
    assembled["live_provenance"] = copy.deepcopy(dict(provenance))
    return assembled


def _sensitive_inline_evidence_issue(value: object, *, path: str) -> str:
    if isinstance(value, Mapping):
        for key, child in value.items():
            key_text = str(key)
            normalized = _normalize_inline_evidence_key(key_text)
            if _sensitive_inline_key(normalized) or _SENSITIVE_ARTIFACT_RE.search(key_text):
                return path + "." + key_text
            child_issue = _sensitive_inline_evidence_issue(child, path=path + "." + key_text)
            if child_issue:
                return child_issue
        return ""
    if isinstance(value, list):
        for index, child in enumerate(value):
            child_issue = _sensitive_inline_evidence_issue(child, path=f"{path}[{index}]")
            if child_issue:
                return child_issue
        return ""
    if isinstance(value, str) and _SENSITIVE_ARTIFACT_RE.search(value):
        return path
    return ""


def _normalize_inline_evidence_key(key: str) -> str:
    key = re.sub(r"([a-z0-9])([A-Z])", r"\1_\2", key)
    return re.sub(r"[^a-z0-9]+", "_", key.lower()).strip("_")


def _sensitive_inline_key(normalized: str) -> bool:
    if normalized in _SENSITIVE_INLINE_KEYS:
        return True
    parts = normalized.split("_")
    return bool({"secret", "secrets"} & set(parts)) or normalized.endswith("_token")


def _verify_scenario(
    name: str,
    raw: Mapping[str, object],
    *,
    strict_live: bool,
    evidence_root: Path | None,
    payload: Mapping[str, object],
    run_id: str = "",
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
    if strict_live and not _strict_live_scenario_artifact_verified(
        raw.get("artifact_refs"),
        evidence_root=evidence_root,
        scenario=name,
        run_id=run_id,
        route=route,
        client_type=client_type,
        provider_proofs=_strict_live_provider_proof_index(payload, evidence_root=evidence_root),
    ):
        issues.append("external live scenario artifact missing or invalid")
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
        required_tasks = _WORKFLOW_BACKGROUND_TASKS
        if tuple(_str_list(raw.get("required_background_tasks"))) != required_tasks:
            issues.append("workflow/background tasks must prove provider-local remap for title/compact/summary/probe/fast/simple/haiku")
        issues.extend(_workflow_background_artifact_issues(raw.get("artifact_refs"), evidence_root=evidence_root, scenario=raw, required_tasks=required_tasks))
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







def _strict_live_run_id(payload: Mapping[str, object]) -> str:
    provenance = payload.get("live_provenance")
    if not isinstance(provenance, Mapping):
        return ""
    return str(provenance.get("run_id") or "").strip()


def _strict_live_scenario_artifact_verified(
    value: object,
    *,
    evidence_root: Path | None,
    scenario: str,
    run_id: str,
    route: str,
    client_type: str,
    provider_proofs: Mapping[str, tuple[Mapping[str, object], ...]],
) -> bool:
    if evidence_root is None or not isinstance(value, list) or not run_id:
        return False
    root = evidence_root.resolve(strict=False)
    required_providers = _STRICT_LIVE_SCENARIO_PROVIDERS.get(scenario, ())
    if not required_providers:
        return False
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
        provider_refs = tuple(str(item).strip() for item in payload.get("provider_provenance_refs") or () if str(item).strip())
        provider = str(payload.get("provider") or "").strip()
        model = str(payload.get("model") or "").strip()
        endpoint = str(payload.get("endpoint") or "").strip()
        upstream_request_id = str(payload.get("upstream_request_id") or "").strip()
        if not (
            payload.get("schema_version") == "cp8-live-scenario-evidence-v1"
            and payload.get("checkpoint") == "CP8"
            and str(payload.get("run_id") or "") == run_id
            and str(payload.get("scenario") or "") == scenario
            and payload.get("status") == "pass"
            and payload.get("live_provider_verified") is True
            and payload.get("raw_sensitive_stored") is False
            and payload.get("loopback") is False
            and str(payload.get("route") or "") == route
            and str(payload.get("client_type") or "") == client_type
            and provider in required_providers
            and _strict_live_model_allowed(provider, model)
            and upstream_request_id
            and provider_refs
            and any(proof.get("endpoint") == endpoint for proof in provider_proofs.get(provider, ()))
        ):
            continue
        for proof in provider_proofs.get(provider, ()):
            proof_request_id = str(proof.get("upstream_request_id") or "").strip()
            proof_endpoint = str(proof.get("endpoint") or "").strip()
            proof_model = str(proof.get("model") or "").strip()
            proof_refs = tuple(str(item).strip() for item in proof.get("artifact_paths") or () if str(item).strip())
            if proof_request_id == upstream_request_id and proof_endpoint == endpoint and proof_model == model and bool(set(provider_refs).intersection(proof_refs)):
                return True
    return False


def _strict_live_provider_proof_index(payload: Mapping[str, object], *, evidence_root: Path | None) -> dict[str, tuple[Mapping[str, object], ...]]:
    provenance = payload.get("live_provenance")
    if evidence_root is None or not isinstance(provenance, Mapping):
        return {}
    run_id = str(provenance.get("run_id") or "").strip()
    providers = provenance.get("providers")
    if not run_id or not isinstance(providers, Mapping):
        return {}
    out: dict[str, list[Mapping[str, object]]] = {}
    for provider, raw in providers.items():
        if not isinstance(raw, Mapping):
            continue
        provenance_mode = str(provenance.get("mode") or "external_provider_live_matrix").strip()
        gateway_base_url = str(provenance.get("gateway_base_url") or "").strip() if provenance_mode == "sub2api_gateway_live_matrix" else ""
        proofs = _strict_live_provider_artifact_payloads(
            raw.get("artifact_refs"),
            evidence_root=evidence_root,
            provider=str(provider),
            credential_scope=str(raw.get("credential_scope") or ""),
            endpoint=str(raw.get("endpoint") or ""),
            model=str(raw.get("model") or ""),
            run_id=run_id,
            provenance_mode=provenance_mode,
            gateway_base_url=gateway_base_url,
            route=str(raw.get("route") or ""),
            client_type=str(raw.get("client_type") or ""),
        )
        if proofs:
            out[str(provider)] = list(proofs)
    return {key: tuple(value) for key, value in out.items()}


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
    provenance_mode = str(provenance.get("mode") or "external_provider_live_matrix").strip()
    if provenance_mode not in {"external_provider_live_matrix", "sub2api_gateway_live_matrix"}:
        return False
    if provenance_mode == "sub2api_gateway_live_matrix":
        gateway_base_url = str(provenance.get("gateway_base_url") or "").strip()
        if not gateway_base_url:
            return False
        try:
            normalized_gateway_base_url = _validate_sub2api_gateway_base_url(gateway_base_url)
        except CP8LiveMatrixError:
            return False
    else:
        normalized_gateway_base_url = ""
    for provider, scope in required.items():
        raw = providers.get(provider)
        if not isinstance(raw, Mapping):
            return False
        if raw.get("credential_scope") != scope or raw.get("live_provider_verified") is not True:
            return False
        endpoint = str(raw.get("endpoint") or "").strip().rstrip("/")
        if not _strict_live_endpoint_allowed(provider, endpoint, provenance_mode=provenance_mode, gateway_base_url=normalized_gateway_base_url):
            return False
        model = str(raw.get("model") or "").strip()
        if not _strict_live_model_allowed(provider, model):
            return False
        route = str(raw.get("route") or "").strip()
        client_type = str(raw.get("client_type") or "").strip()
        if provenance_mode == "sub2api_gateway_live_matrix" and not _sub2api_gateway_provider_route(provider, route, client_type):
            return False
        if _artifact_ref_issues(raw.get("artifact_refs"), evidence_root=evidence_root):
            return False
        if not _strict_live_provider_artifacts_verified(
            raw.get("artifact_refs"),
            evidence_root=evidence_root,
            provider=provider,
            credential_scope=scope,
            endpoint=endpoint,
            model=model,
            run_id=run_id,
            provenance_mode=provenance_mode,
            gateway_base_url=normalized_gateway_base_url,
            route=route,
            client_type=client_type,
        ):
            return False
    return True


def _external_live_endpoint(provider: str, endpoint: str) -> bool:
    endpoint = str(endpoint or "").strip().rstrip("/")
    allowed_endpoints = _STRICT_LIVE_PROVIDER_ENDPOINTS.get(provider)
    if not allowed_endpoints or endpoint not in allowed_endpoints:
        return False
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


def _strict_live_endpoint_allowed(provider: str, endpoint: str, *, provenance_mode: str, gateway_base_url: str) -> bool:
    if provenance_mode == "sub2api_gateway_live_matrix":
        return _sub2api_gateway_provider_endpoint(provider, endpoint, gateway_base_url=gateway_base_url)
    return _external_live_endpoint(provider, endpoint)


def _sub2api_gateway_provider_endpoint(provider: str, endpoint: str, *, gateway_base_url: str = "") -> bool:
    endpoint = str(endpoint or "").strip().rstrip("/")
    parsed = urlparse(endpoint)
    if parsed.scheme not in {"http", "https"}:
        return False
    if parsed.username or parsed.password or parsed.params or parsed.query or parsed.fragment:
        return False
    host = _normalized_url_host(parsed.hostname)
    official_hosts = _official_provider_hosts()
    if not host or host in official_hosts:
        return False
    if gateway_base_url:
        base = _validate_sub2api_gateway_base_url(gateway_base_url)
        if not endpoint.startswith(base + "/"):
            return False
    path = parsed.path.rstrip("/")
    if provider in {"claude", "deepseek"}:
        return path == "/v1/messages"
    if provider == "openai":
        return path == "/v1/messages"
    return False


def _sub2api_gateway_provider_route(provider: str, route: str, client_type: str) -> bool:
    expected = {
        "claude": ("claude_code_native", "claude_code_native"),
        "openai": ("openai_bridge", "claude_code_bridge_openai"),
        "deepseek": ("deepseek_bridge", "claude_code_bridge_deepseek"),
    }.get(provider)
    return bool(expected and (route, client_type) == expected)


def _validate_sub2api_gateway_base_url(base_url: str) -> str:
    base_url = str(base_url or "").strip().rstrip("/")
    if not base_url:
        raise CP8LiveMatrixError("CP8 Sub2API gateway base URL is required")
    parsed = urlparse(base_url)
    if parsed.scheme not in {"http", "https"} or not parsed.hostname:
        raise CP8LiveMatrixError("CP8 Sub2API gateway base URL is invalid")
    if parsed.username or parsed.password or parsed.params or parsed.query or parsed.fragment:
        raise CP8LiveMatrixError("CP8 Sub2API gateway base URL must be an origin without credentials/query/fragment")
    path = parsed.path.rstrip("/")
    if path:
        raise CP8LiveMatrixError("CP8 Sub2API gateway base URL must be an origin without path/query")
    host = _normalized_url_host(parsed.hostname)
    if host in _official_provider_hosts():
        raise CP8LiveMatrixError("CP8 Sub2API gateway base URL must not be an official provider host")
    return base_url


def _official_provider_hosts() -> set[str]:
    return {_normalized_url_host(host) for hosts in _STRICT_LIVE_PROVIDER_HOSTS.values() for host in hosts}


def _normalized_url_host(host: str | None) -> str:
    return str(host or "").strip().lower().rstrip(".")

def _strict_live_provider_artifacts_verified(
    value: object,
    *,
    evidence_root: Path | None,
    provider: str,
    credential_scope: str,
    endpoint: str,
    model: str,
    run_id: str,
    provenance_mode: str = "external_provider_live_matrix",
    gateway_base_url: str = "",
    route: str = "",
    client_type: str = "",
) -> bool:
    return bool(_strict_live_provider_artifact_payloads(
        value,
        evidence_root=evidence_root,
        provider=provider,
        credential_scope=credential_scope,
        endpoint=endpoint,
        model=model,
        run_id=run_id,
        provenance_mode=provenance_mode,
        gateway_base_url=gateway_base_url,
        route=route,
        client_type=client_type,
    ))


def _strict_live_provider_artifact_payloads(
    value: object,
    *,
    evidence_root: Path | None,
    provider: str,
    credential_scope: str,
    endpoint: str,
    model: str,
    run_id: str,
    provenance_mode: str = "external_provider_live_matrix",
    gateway_base_url: str = "",
    route: str = "",
    client_type: str = "",
) -> tuple[Mapping[str, object], ...]:
    if evidence_root is None or not isinstance(value, list):
        return ()
    expected_model = str(model or "").strip()
    if not _strict_live_model_allowed(provider, expected_model):
        return ()
    root = evidence_root.resolve(strict=False)
    verified: list[Mapping[str, object]] = []
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
        expected_endpoint = str(endpoint or "").strip()
        host = (urlparse(expected_endpoint).hostname or "").lower()
        artifact_endpoint = str(payload.get("endpoint") or "").strip()
        artifact_host = str(payload.get("host") or urlparse(artifact_endpoint).hostname or "").lower()
        artifact_model = str(payload.get("model") or "").strip()
        mode_ok = True
        if provenance_mode == "sub2api_gateway_live_matrix":
            mode_ok = (
                payload.get("sub2api_gateway_verified") is True
                and str(payload.get("route") or "").strip() == route
                and str(payload.get("client_type") or "").strip() == client_type
                and _sub2api_gateway_provider_route(provider, route, client_type)
            )
        if (
            payload.get("schema_version") == "cp8-live-provider-provenance-v1"
            and payload.get("checkpoint") == "CP8"
            and str(payload.get("run_id") or "") == run_id
            and str(payload.get("provider") or "") == provider
            and str(payload.get("credential_scope") or "") == credential_scope
            and payload.get("external_live_verified") is True
            and payload.get("loopback") is False
            and mode_ok
            and artifact_host == host
            and artifact_endpoint == expected_endpoint
            and artifact_model == expected_model
            and _strict_live_endpoint_allowed(provider, artifact_endpoint, provenance_mode=provenance_mode, gateway_base_url=gateway_base_url)
            and _success_status(_int(payload.get("response_status")))
            and bool(str(payload.get("upstream_request_id") or "").strip())
        ):
            enriched = dict(payload)
            enriched["artifact_paths"] = (rel,)
            verified.append(enriched)
    return tuple(verified)


def _strict_live_model_allowed(provider: str, model: str) -> bool:
    allowed = _STRICT_LIVE_PROVIDER_MODEL_ALLOWLIST.get(str(provider or "").strip(), frozenset())
    return str(model or "").strip() in allowed


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


def _workflow_background_artifact_issues(
    value: object,
    *,
    evidence_root: Path | None,
    scenario: Mapping[str, object],
    required_tasks: tuple[str, ...],
) -> tuple[str, ...]:
    payloads = _artifact_payloads(value, evidence_root=evidence_root)
    if not payloads:
        return ("workflow/background artifact evidence missing",)
    for payload in payloads:
        if not isinstance(payload, Mapping):
            continue
        if _workflow_background_artifact_valid(payload, scenario=scenario, required_tasks=required_tasks):
            return ()
    return ("workflow/background artifact must prove provider-local dynamic remap for every background task",)


def _workflow_background_artifact_valid(
    payload: Mapping[str, object],
    *,
    scenario: Mapping[str, object],
    required_tasks: tuple[str, ...],
) -> bool:
    if not (
        payload.get("schema_version") == "cp8-workflow-background-v1"
        and payload.get("checkpoint") == "CP8"
        and payload.get("scenario") == "workflow"
        and payload.get("status") == "pass"
        and payload.get("active_profile_dynamic_resolution") is True
        and payload.get("workflow_alias_resolved_provider_local") is True
        and payload.get("hardcoded_claude_model_consumed") is False
        and _int(payload.get("non_claude_background_native_egress")) == 0
        and payload.get("active_profile_dynamic_resolution") == scenario.get("active_profile_dynamic_resolution")
        and payload.get("workflow_alias_resolved_provider_local") == scenario.get("workflow_alias_resolved_provider_local")
        and payload.get("hardcoded_claude_model_consumed") == scenario.get("hardcoded_claude_model_consumed")
        and _int(payload.get("non_claude_background_native_egress")) == _int(scenario.get("non_claude_background_native_egress"))
        and tuple(_str_list(payload.get("required_background_tasks"))) == required_tasks
        and tuple(_str_list(scenario.get("required_background_tasks"))) == required_tasks
    ):
        return False
    tasks = payload.get("background_tasks")
    if not isinstance(tasks, list) or len(tasks) != len(required_tasks):
        return False
    seen: list[str] = []
    for task in tasks:
        if not isinstance(task, Mapping):
            return False
        task_name = str(task.get("task") or "").strip()
        seen.append(task_name)
        active_profile = str(task.get("active_profile") or "").strip().lower()
        resolved_provider = str(task.get("resolved_provider") or "").strip().lower()
        resolved_model = str(task.get("resolved_model") or "").strip().lower()
        if (
            task_name not in required_tasks
            or not active_profile
            or not resolved_provider
            or not resolved_model
            or active_profile == "claude"
            or resolved_provider == "claude"
            or resolved_model.startswith("claude-")
            or not _workflow_profile_matches_provider(active_profile, resolved_provider)
            or not _workflow_provider_model_allowed(resolved_provider, resolved_model)
            or task.get("dynamic_resolution_at_request_time") is not True
            or task.get("provider_local") is not True
            or task.get("hardcoded_claude_model_consumed") is not False
            or task.get("explicit_claude_opt_in") is not False
            or _int(task.get("native_egress_count")) != 0
            or _int(task.get("formal_pool_egress_count")) != 0
        ):
            return False
    return tuple(seen) == required_tasks


def _workflow_profile_matches_provider(active_profile: str, resolved_provider: str) -> bool:
    profile_provider = _workflow_provider_family(active_profile)
    provider_family = _workflow_provider_family(resolved_provider)
    if not profile_provider or not provider_family:
        return False
    return profile_provider == provider_family


def _workflow_provider_model_allowed(provider: str, model: str) -> bool:
    provider_family = _workflow_provider_family(provider)
    model = str(model or "").strip()
    if not provider_family or not model:
        return False
    if provider_family in _STRICT_LIVE_PROVIDER_MODEL_ALLOWLIST:
        return _strict_live_model_allowed(provider_family, model)
    prefixes = {
        "zai": ("glm-",),
        "kimi": ("kimi-",),
    }.get(provider_family)
    return bool(prefixes and model.startswith(prefixes))


def _workflow_provider_family(value: str) -> str:
    normalized = re.sub(r"[^a-z0-9]+", "_", str(value or "").lower()).strip("_")
    aliases = {
        "gpt": "openai",
        "chatgpt": "openai",
        "openai": "openai",
        "deepseek": "deepseek",
        "glm": "zai",
        "zai_glm": "zai",
        "zai": "zai",
        "kimi": "kimi",
        "moonshot": "kimi",
    }
    return aliases.get(normalized, normalized.split("_", 1)[0])


def _artifact_payloads(value: object, *, evidence_root: Path | None) -> tuple[Mapping[str, object], ...]:
    if evidence_root is None or not isinstance(value, list):
        return ()
    root = evidence_root.resolve(strict=False)
    payloads: list[Mapping[str, object]] = []
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
        if isinstance(payload, Mapping):
            payloads.append(payload)
    return tuple(payloads)

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





def write_cp8_live_scenario_evidence(
    *,
    output_root: Path | str,
    run_id: str,
    scenario: str,
    route: str,
    client_type: str,
    evidence: Mapping[str, object] | None = None,
) -> dict[str, object]:
    run_id = str(run_id or "").strip()
    scenario = str(scenario or "").strip()
    if not run_id or scenario not in REQUIRED_CP8_SCENARIOS:
        raise CP8LiveMatrixError("CP8 live scenario evidence requires valid run_id and scenario")
    root = Path(output_root).expanduser()
    artifacts_dir = root / "artifacts"
    artifacts_dir.mkdir(parents=True, exist_ok=True)
    raw = evidence or {}
    artifact_payload = {
        "schema_version": "cp8-live-scenario-evidence-v1",
        "checkpoint": "CP8",
        "run_id": run_id,
        "scenario": scenario,
        "status": "pass" if raw.get("status") == "pass" else "fail",
        "live_provider_verified": raw.get("live_provider_verified") is True,
        "raw_sensitive_stored": False,
        "loopback": raw.get("loopback") is True if "loopback" in raw else False,
        "route": str(route or ""),
        "client_type": str(client_type or ""),
    }
    for key in ("provider", "endpoint", "upstream_request_id"):
        value = raw.get(key)
        if isinstance(value, str) and value.strip() and not _SENSITIVE_ARTIFACT_RE.search(value):
            artifact_payload[key] = value.strip()
    model = raw.get("model")
    provider = str(artifact_payload.get("provider") or "")
    if isinstance(model, str) and _strict_live_model_allowed(provider, model):
        artifact_payload["model"] = model.strip()
    refs = raw.get("provider_provenance_refs")
    if isinstance(refs, list) and all(isinstance(item, str) and item.strip() and ".." not in Path(item).parts and not item.startswith("/") for item in refs):
        artifact_payload["provider_provenance_refs"] = sorted({item.strip() for item in refs})
    for key in ("summary", "notes", "artifact_summary", "safe_evidence_summary"):
        value = raw.get(key)
        if isinstance(value, str) and value.strip() and not _SENSITIVE_ARTIFACT_RE.search(value):
            artifact_payload[key] = value[:4000]
    artifact = artifacts_dir / f"scenario_{scenario}.json"
    artifact.write_text(json.dumps(artifact_payload, ensure_ascii=True, sort_keys=True, indent=2), encoding="utf-8")
    return {
        "path": f"artifacts/{artifact.name}",
        "sha256": "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest(),
        "sensitive_scan_clean": True,
    }


def collect_cp8_sub2api_gateway_live_provenance(
    *,
    run_id: str,
    output_root: Path | str,
    base_url: str,
    gateway_token: str,
    native_attestation_secret: str | None = None,
    route_hint_secret: str | None = None,
    runtime_hash: str | None = None,
    overlay_hash: str | None = None,
    catalog_hash: str | None = None,
    catalog_version: str | None = None,
    transport: object | None = None,
) -> dict[str, object]:
    run_id = str(run_id or "").strip()
    if not run_id:
        raise CP8LiveMatrixError("CP8 Sub2API gateway live provenance requires run_id")
    base = _validate_sub2api_gateway_base_url(base_url)
    token = str(gateway_token or "").strip()
    if not token:
        raise CP8LiveMatrixError("CP8 Sub2API gateway live provenance requires gateway token")
    native_secret = str(native_attestation_secret or os.getenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET") or "").strip()
    if not native_secret:
        raise CP8LiveMatrixError("CP8 Sub2API gateway live provenance requires native attestation secret from managed setup")
    route_secret = str(route_hint_secret or os.getenv("SUB2API_CLAUDE_CODE_ROUTE_HINT_SECRET") or "").strip()
    if not route_secret:
        raise CP8LiveMatrixError("CP8 Sub2API gateway live provenance requires route hint secret from managed setup")
    runtime_hash_value = _required_live_hash(runtime_hash or os.getenv("ZHUMENG_CLAUDE_RUNTIME_HASH") or os.getenv("SUB2API_CLAUDE_CODE_RUNTIME_HASH"), "runtime_hash")
    overlay_hash_value = _required_live_hash(overlay_hash or os.getenv("ZHUMENG_CLAUDE_OVERLAY_HASH") or os.getenv("SUB2API_CLAUDE_CODE_OVERLAY_HASH"), "overlay_hash")
    catalog_hash_value = _required_live_hash(catalog_hash or os.getenv("ZHUMENG_CLAUDE_CATALOG_HASH") or os.getenv("SUB2API_CLAUDE_CODE_CATALOG_HASH"), "catalog_hash")
    catalog_version_value = str(catalog_version or os.getenv("SUB2API_CLAUDE_CODE_ROUTE_HINT_CATALOG_VERSION") or os.getenv("ZHUMENG_CLAUDE_CATALOG_VERSION") or "").strip()
    if not catalog_version_value:
        raise CP8LiveMatrixError("CP8 Sub2API gateway live provenance requires catalog_version from managed setup")
    root = Path(output_root).expanduser()
    artifacts_dir = root / "artifacts"
    artifacts_dir.mkdir(parents=True, exist_ok=True)
    provider_specs = {
        "claude": ("formal_pool", f"{base}/v1/messages", _cp8_live_model("claude"), "claude_code_native", "claude_code_native"),
        "openai": ("bridge_pool", f"{base}/v1/messages", _cp8_live_model("openai"), "openai_bridge", "claude_code_bridge_openai"),
        "deepseek": ("bridge_pool", f"{base}/v1/messages", _cp8_live_model("deepseek"), "deepseek_bridge", "claude_code_bridge_deepseek"),
    }
    session_ref = _cp8_live_session_ref(run_id, native_secret=native_secret, route_secret=route_secret)
    probe = transport or _sub2api_gateway_transport
    providers: dict[str, object] = {}
    for provider, (scope, endpoint, model, route, client_type) in provider_specs.items():
        body, headers = _cp8_sub2api_gateway_probe_request(
            provider,
            model,
            token,
            route=route,
            client_type=client_type,
            native_attestation_secret=native_secret,
            route_hint_secret=route_secret,
            runtime_hash=runtime_hash_value,
            overlay_hash=overlay_hash_value,
            catalog_hash=catalog_hash_value,
            catalog_version=catalog_version_value,
            session_ref=session_ref,
        )
        result = probe(provider, endpoint, {"body": body, "headers": headers})  # type: ignore[misc]
        if not isinstance(result, Mapping):
            raise CP8LiveMatrixError(f"CP8 Sub2API gateway live probe for {provider} did not return evidence")
        status = _int(result.get("status"))
        request_id = _safe_live_request_id(result)
        if not _success_status(status) or not request_id:
            raise CP8LiveMatrixError(f"CP8 Sub2API gateway live probe for {provider} did not return a sanitized 2xx live request id")
        artifact_payload = {
            "schema_version": "cp8-live-provider-provenance-v1",
            "checkpoint": "CP8",
            "run_id": run_id,
            "provider": provider,
            "model": model,
            "credential_scope": scope,
            "endpoint": endpoint,
            "host": urlparse(endpoint).hostname or "",
            "route": route,
            "client_type": client_type,
            "sub2api_gateway_verified": True,
            "external_live_verified": True,
            "loopback": False,
            "response_status": status,
            "upstream_request_id": request_id,
        }
        artifact = artifacts_dir / f"{provider}_sub2api_live_provenance.json"
        artifact.write_text(json.dumps(artifact_payload, ensure_ascii=True, sort_keys=True, indent=2), encoding="utf-8")
        providers[provider] = {
            "credential_scope": scope,
            "live_provider_verified": True,
            "endpoint": endpoint,
            "model": model,
            "route": route,
            "client_type": client_type,
            "artifact_refs": [
                {
                    "path": f"artifacts/{artifact.name}",
                    "sha256": "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest(),
                    "sensitive_scan_clean": True,
                }
            ],
        }
    return {
        "mode": "sub2api_gateway_live_matrix",
        "credential_backed": True,
        "loopback_only": False,
        "gateway_base_url": base,
        "run_id": run_id,
        "providers": providers,
    }



def _cp8_sub2api_gateway_probe_request(
    provider: str,
    model: str,
    gateway_token: str,
    *,
    route: str,
    client_type: str,
    native_attestation_secret: str,
    route_hint_secret: str,
    runtime_hash: str,
    overlay_hash: str,
    catalog_hash: str,
    catalog_version: str,
    session_ref: str,
) -> tuple[dict[str, object], dict[str, str]]:
    body = {
        "model": model,
        "max_tokens": 1,
        "messages": [{"role": "user", "content": "CP8 Sub2API gateway live provenance probe."}],
        "stream": False,
    }
    headers = {
        "Authorization": "Bearer " + gateway_token.strip(),
        "Content-Type": "application/json",
        "x-sub2api-client-type": client_type,
        "x-sub2api-route": route,
        "x-sub2api-route-catalog-version": catalog_version,
    }
    body_bytes = json.dumps(body, ensure_ascii=True, sort_keys=True).encode("utf-8")
    if provider == "claude":
        headers.update(_cp8_native_attestation_headers(
            body_bytes,
            "/v1/messages",
            native_attestation_secret,
            runtime_hash=runtime_hash,
            overlay_hash=overlay_hash,
            catalog_hash=catalog_hash,
            catalog_version=catalog_version,
            session_ref=session_ref,
        ))
    else:
        headers.update(_cp8_bridge_route_hint_headers(
            body_bytes,
            "/v1/messages",
            model,
            route_hint_secret,
            provider=provider,
            route=route,
            client_type=client_type,
            runtime_hash=runtime_hash,
            overlay_hash=overlay_hash,
            catalog_hash=catalog_hash,
            catalog_version=catalog_version,
            session_ref=session_ref,
        ))
    return body, headers


def _cp8_live_session_ref(run_id: str, *, native_secret: str, route_secret: str) -> str:
    material = f"cp8-sub2api-live:{str(run_id).strip()}".encode("utf-8")
    key = (native_secret + "\0" + route_secret).encode("utf-8")
    return "hmac-sha256:" + hmac.new(key, material, hashlib.sha256).hexdigest()


def _required_live_hash(value: object, field: str) -> str:
    text = str(value or "").strip().lower()
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", text) or text == "sha256:" + "0" * 64:
        raise CP8LiveMatrixError(f"CP8 Sub2API gateway live provenance requires {field} from managed runtime")
    return text


def _cp8_body_shape_hash(body: bytes) -> str:
    try:
        decoded = json.loads(body.decode("utf-8")) if body else {}
    except Exception:  # noqa: BLE001 - shape hash must not preserve raw invalid body.
        decoded = {"body_size": len(body), "type": "invalid_json"}
    digest = hashlib.sha256(json.dumps(_cp8_shape_value(decoded), sort_keys=True, separators=(",", ":")).encode("utf-8")).hexdigest()
    return "sha256:" + digest


def _cp8_shape_value(value: object) -> object:
    if isinstance(value, Mapping):
        children: dict[str, object] = {}
        keys: list[str] = []
        for key, child in value.items():
            safe_key = _cp8_shape_key(str(key))
            if safe_key not in children:
                keys.append(safe_key)
            children[safe_key] = _cp8_shape_value(child)
        return {"children": children, "keys": sorted(keys), "type": "object"}
    if isinstance(value, list):
        return {
            "items": [_cp8_shape_value(item) for item in value[:32]],
            "len": len(value),
            "truncated": len(value) > 32,
            "type": "array",
        }
    if isinstance(value, str):
        return {"type": "string"}
    if isinstance(value, bool):
        return {"type": "bool"}
    if isinstance(value, (int, float)) and not isinstance(value, bool):
        return {"type": "number"}
    if value is None:
        return {"type": "null"}
    return {"type": "unknown"}


def _cp8_shape_key(key: str) -> str:
    key = key.strip()
    if not key or _SENSITIVE_ARTIFACT_RE.search(key) or len(key) > 128:
        return "redacted-key"
    for char in key:
        if char.isascii() and (char.isalnum() or char in "_-." ):
            continue
        return "redacted-key"
    return key


def _cp8_native_attestation_headers(
    body: bytes,
    request_path: str,
    secret: str,
    *,
    runtime_hash: str,
    overlay_hash: str,
    catalog_hash: str,
    catalog_version: str,
    session_ref: str,
) -> dict[str, str]:
    now = int(time.time())
    key_id = os.getenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_CURRENT_KEY_ID", "guard_v1")
    payload = {
        "key_id": key_id,
        "scope": os.getenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SCOPE", "claude_code_native_v1"),
        "version": int(os.getenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_VERSION", "1")),
        "issued_at": now,
        "nonce": secrets.token_hex(16),
        "method": "POST",
        "request_uri": request_path,
        "client_type": "claude_code_native",
        "guard_attested": True,
        "guard_version": key_id,
        "claude_code_version": "cp8-live-matrix",
        "local_session_ref": session_ref,
        "netwatch_required": True,
        "shape_healthcheck_profile": "real_claude_code_native_takeover_v1",
        "route": "claude_code_native",
        "model_id": _cp8_body_model(body),
        "provider_owner": "zhumeng_managed",
        "credential_scope": "formal_pool",
        "gateway_location": "cloud",
        "runtime_hash": runtime_hash,
        "overlay_hash": overlay_hash,
        "catalog_hash": catalog_hash,
        "catalog_version": catalog_version,
        "session_ref": session_ref,
        "body_shape_hash": _cp8_body_shape_hash(body),
    }
    encoded = _b64url_json(payload)
    signature = _sign_cp8_runtime_header(encoded, "POST", request_path, body, secret)
    return {
        "x-sub2api-client-type": "claude_code_native",
        "x-sub2api-guard-attested": "true",
        "x-sub2api-guard-version": key_id,
        "x-sub2api-claude-code-version": "cp8-live-matrix",
        "x-sub2api-local-session-ref": session_ref,
        "x-sub2api-netwatch-required": "true",
        "x-sub2api-native-attestation": encoded,
        "x-sub2api-native-signature": signature,
    }


def _cp8_bridge_route_hint_headers(
    body: bytes,
    request_path: str,
    model: str,
    secret: str,
    *,
    provider: str,
    route: str,
    client_type: str,
    runtime_hash: str,
    overlay_hash: str,
    catalog_hash: str,
    catalog_version: str,
    session_ref: str,
) -> dict[str, str]:
    now = int(time.time())
    payload = {
        "key_id": os.getenv("SUB2API_CLAUDE_CODE_ROUTE_HINT_CURRENT_KEY_ID", "route_hint_v1"),
        "scope": "claude_code_route_hint_cp4",
        "version": 1,
        "issued_at": now,
        "expires_at": now + int(os.getenv("SUB2API_CLAUDE_CODE_ROUTE_HINT_NONCE_TTL_SECONDS", "60")),
        "nonce": secrets.token_hex(16),
        "method": "POST",
        "request_uri": request_path,
        "model_id": model,
        "body_model": _cp8_body_model(body),
        "body_sha256": "sha256:" + hashlib.sha256(body).hexdigest(),
        "runtime_hash": runtime_hash,
        "overlay_hash": overlay_hash,
        "catalog_hash": catalog_hash,
        "catalog_version": catalog_version,
        "session_ref": session_ref,
        "route": route,
        "client_type": client_type,
        "provider": provider,
        "live_request_allowed": True,
        "formal_pool_allowed": False,
        "native_attestation_allowed": False,
        "provider_owner": "zhumeng_managed",
        "credential_scope": "bridge_pool",
        "gateway_location": "cloud",
    }
    encoded = _b64url_json(payload)
    return {
        "x-zhumeng-claude-code-route-hint": encoded,
        "x-zhumeng-claude-code-route-signature": _sign_cp8_runtime_header(encoded, "POST", request_path, body, secret),
    }


def _cp8_body_model(body: bytes) -> str:
    try:
        payload = json.loads(body.decode("utf-8")) if body else {}
    except Exception as exc:  # noqa: BLE001
        raise CP8LiveMatrixError("CP8 runtime trust body must be valid JSON") from exc
    model = payload.get("model") if isinstance(payload, Mapping) else None
    if not isinstance(model, str) or not model.strip():
        raise CP8LiveMatrixError("CP8 runtime trust body model is required")
    return model.strip()


def _b64url_json(payload: Mapping[str, object]) -> str:
    return base64.urlsafe_b64encode(json.dumps(payload, ensure_ascii=True, sort_keys=True, separators=(",", ":")).encode("utf-8")).decode("ascii").rstrip("=")


def _sign_cp8_runtime_header(encoded: str, method: str, request_path: str, body: bytes, secret: str) -> str:
    material = "\n".join((encoded, str(method).upper().strip(), request_path, hashlib.sha256(body).hexdigest())).encode("utf-8")
    digest = hmac.new(secret.encode("utf-8"), material, hashlib.sha256).digest()
    return base64.urlsafe_b64encode(digest).decode("ascii").rstrip("=")


def _sub2api_gateway_transport(provider: str, endpoint: str, request: Mapping[str, object]) -> dict[str, object]:
    body = request.get("body")
    headers = request.get("headers")
    if not isinstance(body, Mapping) or not isinstance(headers, Mapping):
        raise CP8LiveMatrixError(f"CP8 Sub2API gateway live probe for {provider} is missing request body or headers")
    http_request = urllib.request.Request(
        endpoint,
        data=json.dumps(body, ensure_ascii=True, sort_keys=True).encode("utf-8"),
        headers={str(key): str(value) for key, value in headers.items()},
        method="POST",
    )
    try:
        with urllib.request.urlopen(http_request, timeout=30) as response:  # noqa: S310 - explicit CP8 live opt-in path.
            response.read(4096)
            return {
                "status": int(getattr(response, "status", 0) or 0),
                "response_headers": _headers_to_dict(getattr(response, "headers", {})),
            }
    except urllib.error.HTTPError as err:
        err.read(4096)
        return {
            "status": int(getattr(err, "code", 0) or 0),
            "response_headers": _headers_to_dict(getattr(err, "headers", {})),
        }
    except urllib.error.URLError as err:
        raise CP8LiveMatrixError(f"CP8 Sub2API gateway live probe for {provider} failed: {err.reason}") from err


def collect_cp8_live_provider_provenance(
    *,
    run_id: str,
    output_root: Path | str,
    credentials: Mapping[str, str] | None = None,
    transport: object | None = None,
) -> dict[str, object]:
    """Collect CP8 external-live provider provenance without persisting secrets.

    The transport is injectable so tests can prove the artifact contract without
    making network calls. Production callers must pass real credentials through
    env/config and explicitly opt into external live collection.
    """
    run_id = str(run_id or "").strip()
    if not run_id:
        raise CP8LiveMatrixError("CP8 live provider provenance requires run_id")
    root = Path(output_root).expanduser()
    artifacts_dir = root / "artifacts"
    artifacts_dir.mkdir(parents=True, exist_ok=True)
    credential_source = credentials if credentials is not None else _cp8_live_credentials_from_env()
    credential_map = {str(k): str(v) for k, v in dict(credential_source).items()}
    provider_specs = {
        provider: (scope, endpoint, _cp8_live_model(provider))
        for provider, (scope, endpoint) in {
            "claude": ("formal_pool", "https://api.anthropic.com/v1/messages"),
            "openai": ("bridge_pool", "https://api.openai.com/v1/responses"),
            "deepseek": ("bridge_pool", "https://api.deepseek.com/anthropic/v1/messages"),
        }.items()
    }
    missing = [provider for provider in provider_specs if not credential_map.get(provider)]
    if missing:
        raise CP8LiveMatrixError("missing live credential for provider(s): " + ", ".join(missing))
    probe = transport or _default_cp8_live_transport
    providers: dict[str, object] = {}
    for provider, (scope, endpoint, model) in provider_specs.items():
        result = probe(provider, endpoint, credential_map[provider])  # type: ignore[misc]
        if not isinstance(result, Mapping):
            raise CP8LiveMatrixError(f"CP8 live provider probe for {provider} did not return evidence")
        status = _int(result.get("status"))
        request_id = _safe_live_request_id(result)
        if not _success_status(status) or not request_id:
            raise CP8LiveMatrixError(f"CP8 live provider probe for {provider} did not return a sanitized 2xx live request id")
        artifact_payload = {
            "schema_version": "cp8-live-provider-provenance-v1",
            "checkpoint": "CP8",
            "run_id": run_id,
            "provider": provider,
            "model": model,
            "credential_scope": scope,
            "endpoint": endpoint,
            "host": urlparse(endpoint).hostname or "",
            "external_live_verified": True,
            "loopback": False,
            "response_status": status,
            "upstream_request_id": request_id,
        }
        artifact = artifacts_dir / f"{provider}_live_provenance.json"
        artifact.write_text(json.dumps(artifact_payload, ensure_ascii=True, sort_keys=True, indent=2), encoding="utf-8")
        providers[provider] = {
            "credential_scope": scope,
            "live_provider_verified": True,
            "endpoint": endpoint,
            "model": model,
            "artifact_refs": [
                {
                    "path": f"artifacts/{artifact.name}",
                    "sha256": "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest(),
                    "sensitive_scan_clean": True,
                }
            ],
        }
    return {
        "credential_backed": True,
        "loopback_only": False,
        "run_id": run_id,
        "providers": providers,
    }


def _cp8_live_credentials_from_env() -> dict[str, str]:
    return {
        "claude": os.getenv("ANTHROPIC_API_KEY") or os.getenv("SUB2API_CLAUDE_CODE_LIVE_ANTHROPIC_API_KEY") or "",
        "openai": os.getenv("OPENAI_API_KEY") or os.getenv("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_API_KEY") or "",
        "deepseek": os.getenv("DEEPSEEK_API_KEY") or os.getenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY") or "",
    }


def _default_cp8_live_transport(provider: str, endpoint: str, credential: str) -> dict[str, object]:
    body, headers = _cp8_live_probe_request(provider, credential)
    request = urllib.request.Request(
        endpoint,
        data=json.dumps(body, ensure_ascii=True, sort_keys=True).encode("utf-8"),
        headers=headers,
        method="POST",
    )
    try:
        with urllib.request.urlopen(request, timeout=30) as response:  # noqa: S310 - explicit CP8 live opt-in path.
            response.read(4096)
            return {
                "status": int(getattr(response, "status", 0) or 0),
                "response_headers": _headers_to_dict(getattr(response, "headers", {})),
            }
    except urllib.error.HTTPError as err:
        err.read(4096)
        return {
            "status": int(getattr(err, "code", 0) or 0),
            "response_headers": _headers_to_dict(getattr(err, "headers", {})),
        }
    except urllib.error.URLError as err:
        raise CP8LiveMatrixError(f"CP8 external live probe for {provider} failed: {err.reason}") from err


def _cp8_live_probe_request(provider: str, credential: str) -> tuple[dict[str, object], dict[str, str]]:
    model = _cp8_live_model(provider)
    if provider == "openai":
        return (
            {
                "model": model,
                "input": "CP8 live provenance probe.",
                "max_output_tokens": 1,
                "stream": False,
            },
            {
                "Authorization": "Bearer " + credential.strip(),
                "Content-Type": "application/json",
            },
        )
    if provider in {"claude", "deepseek"}:
        model_env = (
            "SUB2API_CLAUDE_CODE_LIVE_CLAUDE_MODEL"
            if provider == "claude"
            else "SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_MODEL"
        )
        return (
            {
                "model": model,
                "max_tokens": 1,
                "messages": [{"role": "user", "content": "CP8 live provenance probe."}],
            },
            {
                "X-Api-Key": credential.strip(),
                "anthropic-version": "2023-06-01",
                "Content-Type": "application/json",
            },
        )
    raise CP8LiveMatrixError(f"CP8 external live probe provider is unsupported: {provider}")


def _cp8_live_model(provider: str) -> str:
    model_env = {
        "claude": "SUB2API_CLAUDE_CODE_LIVE_CLAUDE_MODEL",
        "openai": "SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_LIVE_MODEL",
        "deepseek": "SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_MODEL",
    }.get(provider)
    default_model = {
        "claude": "claude-sonnet-4-6",
        "openai": "gpt-5.5",
        "deepseek": "deepseek-v4-pro",
    }.get(provider)
    if not model_env or not default_model:
        raise CP8LiveMatrixError(f"CP8 external live probe provider is unsupported: {provider}")
    model = (os.getenv(model_env) or default_model).strip()
    allowed = _STRICT_LIVE_PROVIDER_MODEL_ALLOWLIST.get(provider, frozenset())
    if model not in allowed:
        raise CP8LiveMatrixError(f"CP8 live probe model for {provider} is not CP4 live-catalog verified: {model}")
    return model


def _headers_to_dict(headers: object) -> dict[str, str]:
    if isinstance(headers, Mapping):
        return {str(key): str(value) for key, value in headers.items()}
    items = getattr(headers, "items", None)
    if callable(items):
        return {str(key): str(value) for key, value in items()}
    return {}


def _safe_live_request_id(result: Mapping[str, object]) -> str:
    request_id = str(result.get("request_id") or "").strip()
    if request_id:
        return _safe_live_label(request_id)
    headers = result.get("response_headers")
    if isinstance(headers, Mapping):
        for key in (
            "x-request-id",
            "request-id",
            "openai-request-id",
            "anthropic-request-id",
            "x-deepseek-request-id",
            "x-sub2api-request-id",
            "x-sub2api-upstream-request-id",
        ):
            value = headers.get(key) or headers.get(key.title()) or headers.get(key.upper())
            if isinstance(value, str) and value.strip():
                return _safe_live_label(value)
    return ""


def _safe_live_label(value: str) -> str:
    if _SENSITIVE_ARTIFACT_RE.search(value):
        return ""
    value = re.sub(r"[^A-Za-z0-9_.:-]", "_", value.strip())
    if _SENSITIVE_ARTIFACT_RE.search(value):
        return ""
    return value[:160]


def _success_status(value: int) -> bool:
    return 200 <= value < 300

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
