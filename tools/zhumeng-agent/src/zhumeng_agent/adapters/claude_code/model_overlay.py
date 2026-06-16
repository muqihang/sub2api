from __future__ import annotations

import hashlib
import json
from dataclasses import asdict, dataclass, field
from pathlib import Path
from typing import Mapping

from .runtime_installer import ManagedRuntimeInstallPlan, ensure_managed_runtime_write_path

CLAUDE_NATIVE_MODEL_ALLOWLIST = frozenset({"claude-sonnet-4-6", "claude-opus-4-7"})
CP2_PATCH_POINTS = (
    "model_options",
    "agent_model_options",
    "model_validation",
    "display_labels",
    "model_allowlist",
    "route_hint_injection_stub",
)
CP2_PATCH_PROBE_MARKERS = {
    "model_options": ("model_options",),
    "agent_model_options": ("agent_model_options",),
    "model_validation": ("model_validation",),
    "display_labels": ("display_labels",),
    "model_allowlist": ("model_allowlist",),
    "route_hint_injection_stub": ("route_hint_injection", "route_hint"),
}


class RuntimeOverlayError(RuntimeError):
    pass


@dataclass(frozen=True, slots=True)
class RuntimeModelOverlayEntry:
    model_id: str
    display_label: str
    provider: str
    route: str
    client_type: str
    live_enabled: bool
    formal_pool_eligible: bool
    api_formats: tuple[str, ...] = ()
    anthropic_base_url: str = ""
    openai_base_url: str = ""
    coding_openai_compatible_base_url: str = ""
    reasoning_effort_levels: tuple[str, ...] = ()
    reasoning_mapping: Mapping[str, str] = field(default_factory=dict)
    reasoning_policy: str = ""
    cache_policy: str = ""
    cache_usage_fields: tuple[str, ...] = ()
    cache_key_strategy: str = ""
    context_window: int | None = None
    deprecated_aliases: tuple[str, ...] = ()
    provider_docs_url: str = ""
    provider_docs_urls: tuple[str, ...] = ()
    catalog_source: str = "provider_docs_observed"
    catalog_authoritative: bool = False
    runtime_verified: bool = False
    compatibility_status: str = "docs_observed_not_runtime_verified"

    def to_dict(self) -> dict[str, object]:
        return asdict(self)


@dataclass(frozen=True, slots=True)
class RuntimeModelOverlayProof:
    runtime_hash: str
    overlay_hash: str
    overlay_mode: str
    bridge_live_feature_flag: bool
    route_hint_mode: str
    patch_points: tuple[str, ...]
    models: tuple[RuntimeModelOverlayEntry, ...]
    catalog_source: str = "cp2_docs_observed_not_authoritative"
    catalog_authoritative: bool = False

    @property
    def display_model_ids(self) -> tuple[str, ...]:
        return tuple(entry.model_id for entry in self.models)

    @property
    def model_allowlist(self) -> tuple[str, ...]:
        return self.display_model_ids

    @property
    def models_by_id(self) -> Mapping[str, RuntimeModelOverlayEntry]:
        return {entry.model_id: entry for entry in self.models}

    def to_dict(self) -> dict[str, object]:
        return {
            "runtime_hash": self.runtime_hash,
            "overlay_hash": self.overlay_hash,
            "overlay_mode": self.overlay_mode,
            "bridge_live_feature_flag": self.bridge_live_feature_flag,
            "route_hint_mode": self.route_hint_mode,
            "patch_points": list(self.patch_points),
            "catalog_source": self.catalog_source,
            "catalog_authoritative": self.catalog_authoritative,
            "display_model_ids": list(self.display_model_ids),
            "models": {entry.model_id: entry.to_dict() for entry in self.models},
        }


def build_cp2_model_overlay_proof(
    runtime_plan: ManagedRuntimeInstallPlan,
    *,
    bridge_live_feature_flag: bool = False,
) -> RuntimeModelOverlayProof:
    if bridge_live_feature_flag:
        raise RuntimeOverlayError("bridge model live routing requires CP4 routing trust contract")
    proof = RuntimeModelOverlayProof(
        runtime_hash=runtime_plan.manifest.upstream_hash,
        overlay_hash=runtime_plan.manifest.overlay_hash,
        overlay_mode="proof_only",
        bridge_live_feature_flag=False,
        route_hint_mode="stub_only_cp4_required",
        patch_points=CP2_PATCH_POINTS,
        models=_default_cp2_models(),
    )
    assert_bridge_models_are_offline_only(proof)
    return proof


def assert_bridge_models_are_offline_only(proof: RuntimeModelOverlayProof) -> None:
    if proof.bridge_live_feature_flag:
        raise RuntimeOverlayError("bridge model live routing requires CP4 routing trust contract")
    for entry in proof.models:
        if entry.route == "claude_native":
            if (
                entry.provider != "claude"
                or entry.model_id not in CLAUDE_NATIVE_MODEL_ALLOWLIST
                or entry.client_type != "claude_code_native"
                or not entry.formal_pool_eligible
            ):
                raise RuntimeOverlayError("native formal-pool eligibility requires Claude provider and allowlisted native model")
            continue
        if entry.live_enabled or entry.formal_pool_eligible or entry.client_type == "claude_code_native":
            raise RuntimeOverlayError("CP2 bridge models must remain display-only and formal-pool ineligible")


def render_model_list_capture(proof: RuntimeModelOverlayProof) -> str:
    lines = [
        "/model overlay proof",
        f"overlay_mode={proof.overlay_mode}",
        "bridge display only; live disabled until CP4",
    ]
    for entry in proof.models:
        suffix = "" if entry.route == "claude_native" else " (bridge display only; live disabled until CP4)"
        lines.append(f"- {entry.display_label} [{entry.model_id}] -> {entry.client_type}{suffix}")
    return "\n".join(lines) + "\n"


def build_route_hint_stub(
    proof: RuntimeModelOverlayProof,
    model_id: str,
    *,
    require_live_request: bool = False,
) -> dict[str, object]:
    entry = proof.models_by_id.get(model_id)
    if entry is None:
        raise RuntimeOverlayError(f"unknown overlay model: {model_id}")
    is_native = entry.route == "claude_native"
    if require_live_request:
        if is_native:
            raise RuntimeOverlayError("CP2 route hint stubs never authorize live requests")
        raise RuntimeOverlayError("bridge model selections are display-only until CP4 routing trust contract is green")
    return {
        "model_id": entry.model_id,
        "route": entry.route,
        "client_type": entry.client_type,
        "runtime_hash": proof.runtime_hash,
        "overlay_hash": proof.overlay_hash,
        "catalog_version": "cp2-proof-local",
        "route_hint_mode": proof.route_hint_mode,
        "live_request_allowed": False,
        "formal_pool_allowed": False,
        "native_attestation_allowed": False,
        "requires_cp4_routing_trust_contract": not is_native,
        "fail_closed_reason": "cp2_proof_only_native_verifier_required" if is_native else "cp4_routing_trust_contract_not_green",
    }


def build_cp2_print_smoke_plan(proof: RuntimeModelOverlayProof, *, prompt: str = "/model") -> dict[str, object]:
    assert_bridge_models_are_offline_only(proof)
    return {
        "mode": "mock_only",
        "command": ["claude", "--print", prompt],
        "will_start_process": False,
        "live_bridge_models_enabled": False,
        "expected_model_list_capture": render_model_list_capture(proof),
    }


def run_cp2_print_smoke_with_stubbed_runner(proof: RuntimeModelOverlayProof, runner) -> dict[str, object]:
    plan = build_cp2_print_smoke_plan(proof)
    if getattr(runner, "is_cp2_stub_runner", False) is not True:
        raise RuntimeOverlayError("CP2 --print smoke requires an explicit CP2 stub runner")
    output = str(runner())
    expected_labels = [entry.display_label for entry in proof.models]
    missing = [label for label in expected_labels if label not in output]
    return {
        "mode": "stubbed_runner",
        "command": plan["command"],
        "will_start_process": False,
        "output": output,
        "verified": not missing,
        "missing_labels": missing,
        "live_bridge_models_enabled": False,
    }


def assert_cp2_native_shape_equality(
    proof: RuntimeModelOverlayProof,
    request: Mapping[str, object],
    *,
    baseline_request: Mapping[str, object] | None = None,
    baseline_evidence: Mapping[str, object] | None = None,
) -> None:
    if baseline_request is None:
        raise RuntimeOverlayError("native shape equality requires unmodified 2.1.175 baseline fixture")
    if request is baseline_request:
        raise RuntimeOverlayError("baseline fixture cannot be the request object itself")
    body = request.get("body")
    if not isinstance(body, Mapping):
        raise RuntimeOverlayError("native shape equality fixture requires a request body mapping")
    model_id = str(body.get("model") or "")
    entry = proof.models_by_id.get(model_id)
    if entry is None or entry.route != "claude_native":
        raise RuntimeOverlayError("native shape equality only applies to Claude native models")
    if entry.client_type != "claude_code_native" or not entry.formal_pool_eligible:
        raise RuntimeOverlayError("Claude native shape fixture requires a native formal-pool eligible overlay entry")
    if baseline_evidence is None:
        raise RuntimeOverlayError("native shape equality requires signed 2.1.175 baseline evidence")
    _assert_cp2_native_baseline_evidence(proof, baseline_request, baseline_evidence)
    if _canonical_shape(request) != _canonical_shape(baseline_request):
        raise RuntimeOverlayError("native request shape changed from unmodified 2.1.175 baseline")


def assert_cp2_signing_verifier_gate(proof: RuntimeModelOverlayProof, *, evidence: Mapping[str, object]) -> None:
    assert_bridge_models_are_offline_only(proof)
    _assert_cp2_native_verifier_evidence(proof, evidence)


def assert_cp2_no_live_formal_pool_bridge_path(proof: RuntimeModelOverlayProof, *, evidence: Mapping[str, object]) -> None:
    assert_bridge_models_are_offline_only(proof)
    expected_false = (
        "live_catalog_bridge_models_enabled",
        "launcher_bridge_transport_connected",
        "guard_formal_pool_bridge_admission",
        "backend_formal_pool_bridge_admission",
    )
    for key in expected_false:
        if evidence.get(key) is not False:
            raise RuntimeOverlayError("CP2 bridge selections must not connect to live formal-pool native path")


def disable_model_overlay_proof(runtime_plan: ManagedRuntimeInstallPlan) -> dict[str, object]:
    patches_path = ensure_managed_runtime_write_path(runtime_plan.patches_path, runtime_root=runtime_plan.runtime_root)
    patches_path.parent.mkdir(parents=True, exist_ok=True)
    try:
        patches = json.loads(patches_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        patches = dict(runtime_plan.patches)
    patches["live_bridge_models_enabled"] = False
    patches["cp2_model_overlay"] = {
        **dict(patches.get("cp2_model_overlay", {}) if isinstance(patches.get("cp2_model_overlay"), Mapping) else {}),
        "enabled": False,
        "rollback_action": "disable_overlay_pointer_without_global_delete",
    }
    patches_path.write_bytes(_canonical_json_bytes(patches))
    return {
        "runtime": "claude-code",
        "checkpoint": "CP2",
        "overlay_disabled": True,
        "global_overwrite": False,
        "patches_path": str(patches_path),
    }


def assert_cp2_provider_entries_are_not_runtime_verified(proof: RuntimeModelOverlayProof) -> None:
    for entry in proof.models:
        if entry.provider == "claude":
            continue
        if entry.runtime_verified or entry.catalog_authoritative or not entry.compatibility_status.endswith("_not_runtime_verified"):
            raise RuntimeOverlayError("CP2 provider docs observations must not be treated as runtime-verified compatibility")


def assert_cp2_provider_capabilities_require_probe(proof: RuntimeModelOverlayProof, provider: str) -> None:
    entries = [entry for entry in proof.models if entry.provider == provider]
    if not entries:
        raise RuntimeOverlayError(f"unknown provider: {provider}")
    raise RuntimeOverlayError("provider compatibility requires signed catalog and capability probe after CP2")


def assert_cp2_deprecated_provider_aliases_are_not_display_models(proof: RuntimeModelOverlayProof) -> None:
    displayed = set(proof.model_allowlist)
    deprecated = {alias for entry in proof.models for alias in entry.deprecated_aliases}
    if displayed & deprecated:
        raise RuntimeOverlayError("deprecated provider aliases must not be display model ids in CP2 overlay")


def assert_cp2_exit_gate(proof: RuntimeModelOverlayProof, *, evidence: Mapping[str, object] | None) -> None:
    if evidence is None:
        raise RuntimeOverlayError("CP2 exit gate evidence is required")
    assert_bridge_models_are_offline_only(proof)
    assert_cp2_provider_entries_are_not_runtime_verified(proof)
    assert_cp2_deprecated_provider_aliases_are_not_display_models(proof)
    native_verifier = evidence.get("native_verifier")
    if not isinstance(native_verifier, Mapping):
        raise RuntimeOverlayError("CP2 exit gate evidence is required")
    _assert_cp2_native_verifier_evidence(proof, native_verifier)
    no_live = evidence.get("no_live_formal_pool_bridge_path")
    if not isinstance(no_live, Mapping):
        raise RuntimeOverlayError("CP2 bridge no-live evidence is required")
    assert_cp2_no_live_formal_pool_bridge_path(proof, evidence=no_live)
    probe_status = evidence.get("provider_capability_probe_status")
    if not isinstance(probe_status, Mapping):
        raise RuntimeOverlayError("CP2 provider probe status must remain fail-closed")
    for entry in proof.models:
        if entry.provider == "claude":
            continue
        if probe_status.get(entry.provider) != "not_runtime_verified_fail_closed":
            raise RuntimeOverlayError("CP2 provider probe status must remain fail-closed")


def probe_cp2_patch_points(
    runtime_plan: ManagedRuntimeInstallPlan,
    *,
    expected_file_hashes: Mapping[str, str] | None = None,
) -> dict[str, object]:
    candidates = _runtime_probe_files(runtime_plan)
    file_hashes = {str(path): _hash_file(path) for path in candidates}
    hash_mismatches: list[str] = []
    if expected_file_hashes is not None:
        for file_name, expected_hash in expected_file_hashes.items():
            matched = [digest for path, digest in file_hashes.items() if Path(path).name == file_name]
            if matched != [expected_hash]:
                hash_mismatches.append(file_name)
    probe_text = "\n".join(_read_probe_file(path) for path in candidates)
    patch_points: dict[str, dict[str, object]] = {}
    missing: list[str] = []
    unsafe: list[str] = []
    for point, markers in CP2_PATCH_PROBE_MARKERS.items():
        found = any(marker in probe_text for marker in markers)
        mode = "degraded_stub" if point == "route_hint_injection_stub" else "static_probe"
        patch_points[point] = {
            "found": found,
            "markers": list(markers),
            "mode": mode,
        }
        if not found:
            missing.append(point)
        if point == "route_hint_injection_stub":
            unsafe.append(point)
    status = "degraded_fail_closed" if missing or unsafe or hash_mismatches else "ready"
    return {
        "runtime": "claude-code",
        "runtime_version": runtime_plan.upstream_version,
        "status": status,
        "live_bridge_models_enabled": False,
        "bundle_hash_verified": not hash_mismatches if expected_file_hashes is not None else False,
        "hash_mismatches": hash_mismatches,
        "probe_files": [str(path) for path in candidates],
        "patch_points": patch_points,
        "missing_patch_points": missing,
        "unsafe_patch_points": unsafe,
    }


def write_model_overlay_proof_artifacts(
    runtime_plan: ManagedRuntimeInstallPlan,
    proof: RuntimeModelOverlayProof,
    *,
    exit_gate_evidence: Mapping[str, object] | None = None,
) -> dict[str, Path]:
    assert_cp2_exit_gate(proof, evidence=exit_gate_evidence)
    overlay_dir = ensure_managed_runtime_write_path(runtime_plan.version_dir / "overlay" / "cp2-proof", runtime_root=runtime_plan.runtime_root)
    overlay_dir.mkdir(parents=True, exist_ok=True)
    overlay_proof_path = ensure_managed_runtime_write_path(overlay_dir / "model-overlay-proof.json", runtime_root=runtime_plan.runtime_root)
    model_capture_path = ensure_managed_runtime_write_path(overlay_dir / "model-list-capture.txt", runtime_root=runtime_plan.runtime_root)
    route_hint_path = ensure_managed_runtime_write_path(overlay_dir / "route-hint-stub.json", runtime_root=runtime_plan.runtime_root)
    rollback_path = ensure_managed_runtime_write_path(overlay_dir / "rollback.json", runtime_root=runtime_plan.runtime_root)

    route_hints = {entry.model_id: build_route_hint_stub(proof, entry.model_id) for entry in proof.models}
    rollback = {
        "runtime": "claude-code",
        "checkpoint": "CP2",
        "rollback_action": "disable_overlay_pointer_without_global_delete",
        "global_overwrite": False,
        "overlay_dir": str(overlay_dir),
    }
    overlay_proof_path.write_bytes(_canonical_json_bytes(proof.to_dict()))
    model_capture_path.write_text(render_model_list_capture(proof), encoding="utf-8")
    route_hint_path.write_bytes(_canonical_json_bytes(route_hints))
    rollback_path.write_bytes(_canonical_json_bytes(rollback))
    _write_cp2_patches_metadata(runtime_plan, proof, overlay_dir)
    return {
        "overlay_proof": overlay_proof_path,
        "model_list_capture": model_capture_path,
        "route_hint_stub": route_hint_path,
        "rollback": rollback_path,
    }


def _write_cp2_patches_metadata(runtime_plan: ManagedRuntimeInstallPlan, proof: RuntimeModelOverlayProof, overlay_dir: Path) -> None:
    patches_path = ensure_managed_runtime_write_path(runtime_plan.patches_path, runtime_root=runtime_plan.runtime_root)
    patches_path.parent.mkdir(parents=True, exist_ok=True)
    patch_points = list(dict.fromkeys([*runtime_plan.patches.get("patch_points", []), *proof.patch_points]))
    patches = {
        **dict(runtime_plan.patches),
        "patch_points": patch_points,
        "live_bridge_models_enabled": False,
        "cp2_model_overlay": {
            "artifact_dir": str(overlay_dir),
            "overlay_mode": proof.overlay_mode,
            "route_hint_mode": proof.route_hint_mode,
            "bridge_live_feature_flag": False,
        },
    }
    patches_path.write_bytes(_canonical_json_bytes(patches))


def _default_cp2_models() -> tuple[RuntimeModelOverlayEntry, ...]:
    return (
        RuntimeModelOverlayEntry(
            model_id="claude-sonnet-4-6",
            display_label="Claude Sonnet 4.6",
            provider="claude",
            route="claude_native",
            client_type="claude_code_native",
            live_enabled=True,
            formal_pool_eligible=True,
        ),
        RuntimeModelOverlayEntry(
            model_id="claude-opus-4-7",
            display_label="Claude Opus 4.7",
            provider="claude",
            route="claude_native",
            client_type="claude_code_native",
            live_enabled=True,
            formal_pool_eligible=True,
        ),
        RuntimeModelOverlayEntry(
            model_id="openai-catalog-placeholder",
            display_label="OpenAI catalog placeholder",
            provider="openai",
            route="openai_bridge",
            client_type="claude_code_bridge_openai",
            live_enabled=False,
            formal_pool_eligible=False,
            api_formats=("responses", "openai_chat_completions"),
            provider_docs_url="https://platform.openai.com/docs/models",
            provider_docs_urls=("https://platform.openai.com/docs/models",),
        ),
        RuntimeModelOverlayEntry(
            model_id="deepseek-v4-pro",
            display_label="DeepSeek V4 Pro",
            provider="deepseek",
            route="deepseek_bridge",
            client_type="claude_code_bridge_deepseek",
            live_enabled=False,
            formal_pool_eligible=False,
            api_formats=("anthropic_messages", "openai_chat_completions"),
            anthropic_base_url="https://api.deepseek.com/anthropic",
            openai_base_url="https://api.deepseek.com",
            reasoning_effort_levels=("high", "max"),
            reasoning_mapping={"low": "high", "medium": "high", "high": "high", "xhigh": "max", "max": "max"},
            cache_policy="provider_prefix_kv_cache_automatic_best_effort",
            cache_usage_fields=("prompt_cache_hit_tokens", "prompt_cache_miss_tokens"),
            cache_key_strategy="provider_automatic_prefix_cache_best_effort",
            deprecated_aliases=("deepseek-chat", "deepseek-reasoner"),
            provider_docs_url="https://api-docs.deepseek.com/news/news260424",
            provider_docs_urls=(
                "https://api-docs.deepseek.com/guides/anthropic_api",
                "https://api-docs.deepseek.com/guides/kv_cache",
                "https://api-docs.deepseek.com/quick_start/pricing",
                "https://api-docs.deepseek.com/news/news260424",
            ),
        ),
        RuntimeModelOverlayEntry(
            model_id="deepseek-v4-flash",
            display_label="DeepSeek V4 Flash",
            provider="deepseek",
            route="deepseek_bridge",
            client_type="claude_code_bridge_deepseek",
            live_enabled=False,
            formal_pool_eligible=False,
            api_formats=("anthropic_messages", "openai_chat_completions"),
            anthropic_base_url="https://api.deepseek.com/anthropic",
            openai_base_url="https://api.deepseek.com",
            reasoning_effort_levels=("high", "max"),
            reasoning_mapping={"low": "high", "medium": "high", "high": "high", "xhigh": "max", "max": "max"},
            cache_policy="provider_prefix_kv_cache_automatic_best_effort",
            cache_usage_fields=("prompt_cache_hit_tokens", "prompt_cache_miss_tokens"),
            cache_key_strategy="provider_automatic_prefix_cache_best_effort",
            deprecated_aliases=("deepseek-chat", "deepseek-reasoner"),
            provider_docs_url="https://api-docs.deepseek.com/news/news260424",
            provider_docs_urls=(
                "https://api-docs.deepseek.com/guides/anthropic_api",
                "https://api-docs.deepseek.com/guides/kv_cache",
                "https://api-docs.deepseek.com/quick_start/pricing",
                "https://api-docs.deepseek.com/news/news260424",
            ),
        ),
        RuntimeModelOverlayEntry(
            model_id="agnes-1",
            display_label="AGNES 1",
            provider="agnes",
            route="agnes_bridge",
            client_type="claude_code_bridge_agnes",
            live_enabled=False,
            formal_pool_eligible=False,
            catalog_source="internal_display_placeholder",
            compatibility_status="internal_placeholder_not_runtime_verified",
        ),
        RuntimeModelOverlayEntry(
            model_id="glm-5.2",
            display_label="GLM 5.2",
            provider="glm",
            route="glm_bridge",
            client_type="claude_code_bridge_glm",
            live_enabled=False,
            formal_pool_eligible=False,
            api_formats=("anthropic_messages", "openai_compatible_chat"),
            anthropic_base_url="https://api.z.ai/api/anthropic",
            coding_openai_compatible_base_url="https://api.z.ai/api/coding/paas/v4",
            reasoning_mapping={"low": "high", "medium": "high", "high": "high", "xhigh": "max", "max": "max", "ultracode": "max"},
            cache_key_strategy="unknown_probe_required",
            provider_docs_url="https://docs.z.ai/devpack/tool/others",
            provider_docs_urls=(
                "https://docs.z.ai/devpack/latest-model",
                "https://docs.z.ai/devpack/tool/others",
                "https://docs.z.ai/devpack/overview",
            ),
        ),
        RuntimeModelOverlayEntry(
            model_id="glm-5.2[1m]",
            display_label="GLM 5.2 1M",
            provider="glm",
            route="glm_bridge",
            client_type="claude_code_bridge_glm",
            live_enabled=False,
            formal_pool_eligible=False,
            api_formats=("anthropic_messages", "openai_compatible_chat"),
            anthropic_base_url="https://api.z.ai/api/anthropic",
            coding_openai_compatible_base_url="https://api.z.ai/api/coding/paas/v4",
            reasoning_mapping={"low": "high", "medium": "high", "high": "high", "xhigh": "max", "max": "max", "ultracode": "max"},
            cache_key_strategy="unknown_probe_required",
            context_window=1_000_000,
            provider_docs_url="https://docs.z.ai/devpack/latest-model",
            provider_docs_urls=(
                "https://docs.z.ai/devpack/latest-model",
                "https://docs.z.ai/devpack/tool/others",
                "https://docs.z.ai/devpack/overview",
            ),
        ),
        RuntimeModelOverlayEntry(
            model_id="glm-5-turbo",
            display_label="GLM 5 Turbo",
            provider="glm",
            route="glm_bridge",
            client_type="claude_code_bridge_glm",
            live_enabled=False,
            formal_pool_eligible=False,
            api_formats=("anthropic_messages", "openai_compatible_chat"),
            anthropic_base_url="https://api.z.ai/api/anthropic",
            coding_openai_compatible_base_url="https://api.z.ai/api/coding/paas/v4",
            reasoning_mapping={"low": "high", "medium": "high", "high": "high", "xhigh": "max", "max": "max", "ultracode": "max"},
            cache_key_strategy="unknown_probe_required",
            provider_docs_url="https://docs.z.ai/devpack/overview",
            provider_docs_urls=(
                "https://docs.z.ai/devpack/latest-model",
                "https://docs.z.ai/devpack/tool/others",
                "https://docs.z.ai/devpack/overview",
            ),
        ),
        RuntimeModelOverlayEntry(
            model_id="glm-4.7",
            display_label="GLM 4.7",
            provider="glm",
            route="glm_bridge",
            client_type="claude_code_bridge_glm",
            live_enabled=False,
            formal_pool_eligible=False,
            api_formats=("anthropic_messages", "openai_compatible_chat"),
            anthropic_base_url="https://api.z.ai/api/anthropic",
            coding_openai_compatible_base_url="https://api.z.ai/api/coding/paas/v4",
            reasoning_mapping={"low": "high", "medium": "high", "high": "high", "xhigh": "max", "max": "max", "ultracode": "max"},
            cache_key_strategy="unknown_probe_required",
            provider_docs_url="https://docs.z.ai/devpack/overview",
            provider_docs_urls=(
                "https://docs.z.ai/devpack/latest-model",
                "https://docs.z.ai/devpack/tool/others",
                "https://docs.z.ai/devpack/overview",
            ),
        ),
        RuntimeModelOverlayEntry(
            model_id="glm-4.5-air",
            display_label="GLM 4.5 Air",
            provider="glm",
            route="glm_bridge",
            client_type="claude_code_bridge_glm",
            live_enabled=False,
            formal_pool_eligible=False,
            api_formats=("anthropic_messages", "openai_compatible_chat"),
            anthropic_base_url="https://api.z.ai/api/anthropic",
            coding_openai_compatible_base_url="https://api.z.ai/api/coding/paas/v4",
            reasoning_mapping={"low": "high", "medium": "high", "high": "high", "xhigh": "max", "max": "max", "ultracode": "max"},
            cache_key_strategy="unknown_probe_required",
            provider_docs_url="https://docs.z.ai/devpack/overview",
            provider_docs_urls=(
                "https://docs.z.ai/devpack/latest-model",
                "https://docs.z.ai/devpack/tool/others",
                "https://docs.z.ai/devpack/overview",
            ),
        ),
        RuntimeModelOverlayEntry(
            model_id="kimi-k2.7-code",
            display_label="Kimi K2.7 Code",
            provider="kimi",
            route="kimi_bridge",
            client_type="claude_code_bridge_kimi",
            live_enabled=False,
            formal_pool_eligible=False,
            api_formats=("anthropic_messages", "openai_chat_completions"),
            anthropic_base_url="https://api.moonshot.ai/anthropic",
            openai_base_url="https://api.moonshot.ai/v1",
            reasoning_policy="always_thinks_preserve_reasoning_content",
            cache_key_strategy="provider_cache_metadata_unverified_probe_required",
            deprecated_aliases=(
                "kimi-latest",
                "kimi-thinking-preview",
                "kimi-k2-0905-preview",
                "kimi-k2-0711-preview",
                "kimi-k2-turbo-preview",
                "kimi-k2-thinking",
                "kimi-k2-thinking-turbo",
            ),
            provider_docs_url="https://platform.kimi.ai/",
            provider_docs_urls=(
                "https://platform.kimi.ai/docs/models",
                "https://platform.kimi.ai/docs/guide/claude-code",
                "https://platform.kimi.ai/docs/api/chat",
            ),
        ),
        RuntimeModelOverlayEntry(
            model_id="kimi-k2.7-code-highspeed",
            display_label="Kimi K2.7 Code Highspeed",
            provider="kimi",
            route="kimi_bridge",
            client_type="claude_code_bridge_kimi",
            live_enabled=False,
            formal_pool_eligible=False,
            api_formats=("anthropic_messages", "openai_chat_completions"),
            anthropic_base_url="https://api.moonshot.ai/anthropic",
            openai_base_url="https://api.moonshot.ai/v1",
            reasoning_policy="always_thinks_preserve_reasoning_content",
            cache_key_strategy="provider_cache_metadata_unverified_probe_required",
            deprecated_aliases=(
                "kimi-latest",
                "kimi-thinking-preview",
                "kimi-k2-0905-preview",
                "kimi-k2-0711-preview",
                "kimi-k2-turbo-preview",
                "kimi-k2-thinking",
                "kimi-k2-thinking-turbo",
            ),
            provider_docs_url="https://platform.kimi.ai/docs/models",
            provider_docs_urls=(
                "https://platform.kimi.ai/docs/models",
                "https://platform.kimi.ai/docs/guide/claude-code",
                "https://platform.kimi.ai/docs/api/chat",
            ),
        ),
        RuntimeModelOverlayEntry(
            model_id="kimi-k2.6",
            display_label="Kimi K2.6",
            provider="kimi",
            route="kimi_bridge",
            client_type="claude_code_bridge_kimi",
            live_enabled=False,
            formal_pool_eligible=False,
            api_formats=("anthropic_messages", "openai_chat_completions"),
            anthropic_base_url="https://api.moonshot.ai/anthropic",
            openai_base_url="https://api.moonshot.ai/v1",
            reasoning_policy="thinking_keep_all_supported",
            cache_key_strategy="provider_cache_metadata_unverified_probe_required",
            deprecated_aliases=(
                "kimi-latest",
                "kimi-thinking-preview",
                "kimi-k2-0905-preview",
                "kimi-k2-0711-preview",
                "kimi-k2-turbo-preview",
                "kimi-k2-thinking",
                "kimi-k2-thinking-turbo",
            ),
            provider_docs_url="https://platform.kimi.ai/docs/models",
            provider_docs_urls=(
                "https://platform.kimi.ai/docs/models",
                "https://platform.kimi.ai/docs/guide/claude-code",
                "https://platform.kimi.ai/docs/api/chat",
            ),
        ),
        RuntimeModelOverlayEntry(
            model_id="kimi-k2.5",
            display_label="Kimi K2.5",
            provider="kimi",
            route="kimi_bridge",
            client_type="claude_code_bridge_kimi",
            live_enabled=False,
            formal_pool_eligible=False,
            api_formats=("anthropic_messages", "openai_chat_completions"),
            anthropic_base_url="https://api.moonshot.ai/anthropic",
            openai_base_url="https://api.moonshot.ai/v1",
            reasoning_policy="no_preserved_thinking",
            cache_key_strategy="provider_cache_metadata_unverified_probe_required",
            deprecated_aliases=(
                "kimi-latest",
                "kimi-thinking-preview",
                "kimi-k2-0905-preview",
                "kimi-k2-0711-preview",
                "kimi-k2-turbo-preview",
                "kimi-k2-thinking",
                "kimi-k2-thinking-turbo",
            ),
            provider_docs_url="https://platform.kimi.ai/docs/models",
            provider_docs_urls=(
                "https://platform.kimi.ai/docs/models",
                "https://platform.kimi.ai/docs/guide/claude-code",
                "https://platform.kimi.ai/docs/api/chat",
            ),
        ),
    )


def _runtime_probe_files(runtime_plan: ManagedRuntimeInstallPlan) -> tuple[Path, ...]:
    upstream_dir = runtime_plan.version_dir / "upstream"
    if not upstream_dir.exists():
        return ()
    return tuple(sorted(path for path in upstream_dir.rglob("*") if path.is_file() and path.suffix in {".js", ".mjs", ".cjs", ".json"}))


def _read_probe_file(path: Path) -> str:
    try:
        return path.read_text(encoding="utf-8", errors="ignore")[:2_000_000]
    except OSError:
        return ""


def _hash_file(path: Path) -> str:
    digest = hashlib.sha256()
    try:
        with path.open("rb") as handle:
            for chunk in iter(lambda: handle.read(1024 * 1024), b""):
                digest.update(chunk)
    except OSError:
        return ""
    return "sha256:" + digest.hexdigest()


def _assert_cp2_native_baseline_evidence(
    proof: RuntimeModelOverlayProof,
    baseline_request: Mapping[str, object],
    evidence: Mapping[str, object],
) -> None:
    if (
        evidence.get("baseline_source") != "unmodified_claude_code_2.1.175_native_capture"
        or evidence.get("baseline_runtime_version") != "2.1.175"
        or evidence.get("verifier_green") is not True
        or evidence.get("signing_pipeline") != "cc_gateway"
        or evidence.get("runtime_hash") != proof.runtime_hash
        or evidence.get("overlay_hash") != proof.overlay_hash
        or evidence.get("native_shape_equality") != "passed"
    ):
        raise RuntimeOverlayError("native shape equality requires signed 2.1.175 baseline evidence")
    if evidence.get("baseline_shape_hash") != _canonical_shape_hash(baseline_request):
        raise RuntimeOverlayError("native baseline shape hash mismatch")


def _assert_cp2_native_verifier_evidence(proof: RuntimeModelOverlayProof, evidence: Mapping[str, object]) -> None:
    if (
        evidence.get("baseline_source") != "unmodified_claude_code_2.1.175_native_capture"
        or evidence.get("baseline_runtime_version") != "2.1.175"
        or evidence.get("verifier_green") is not True
        or evidence.get("signing_pipeline") != "cc_gateway"
        or evidence.get("runtime_hash") != proof.runtime_hash
        or evidence.get("overlay_hash") != proof.overlay_hash
        or evidence.get("native_shape_equality") != "passed"
    ):
        raise RuntimeOverlayError("CC Gateway signing/verifier parity is not green; disable Claude formal pool path")
    baseline_shape_hash = str(evidence.get("baseline_shape_hash") or "")
    if not baseline_shape_hash.startswith("sha256:") or len(baseline_shape_hash) != len("sha256:") + 64:
        raise RuntimeOverlayError("native verifier evidence requires a stable 2.1.175 baseline hash")


def _canonical_shape(request: Mapping[str, object]) -> bytes:
    return _canonical_json_bytes(_normalize_shape(request))


def _canonical_shape_hash(request: Mapping[str, object]) -> str:
    return "sha256:" + hashlib.sha256(_canonical_shape(request)).hexdigest()


def _normalize_shape(value: object) -> object:
    if isinstance(value, Mapping):
        return {str(key).lower(): _normalize_shape(nested) for key, nested in sorted(value.items(), key=lambda item: str(item[0]).lower())}
    if isinstance(value, list):
        return [_normalize_shape(item) for item in value]
    return value


def _canonical_json_bytes(payload: object) -> bytes:
    return (json.dumps(payload, ensure_ascii=True, sort_keys=True, separators=(",", ":")) + "\n").encode("utf-8")
