from __future__ import annotations

import hashlib
import json
from pathlib import Path
from types import SimpleNamespace

import pytest

from zhumeng_agent.adapters.claude_code.model_overlay import (
    RuntimeOverlayError,
    assert_cp2_exit_gate,
    assert_bridge_models_are_offline_only,
    assert_cp2_deprecated_provider_aliases_are_not_display_models,
    assert_cp2_no_live_formal_pool_bridge_path,
    assert_cp2_provider_capabilities_require_probe,
    assert_cp2_provider_entries_are_not_runtime_verified,
    build_cp2_model_overlay_proof,
    build_route_hint_stub,
    disable_model_overlay_proof,
    render_model_list_capture,
    run_cp2_print_smoke_with_stubbed_runner,
    write_model_overlay_proof_artifacts,
)
from zhumeng_agent.adapters.claude_code.runtime_installer import build_managed_runtime_install_plan


class VersionRunner:
    def __call__(self, command: list[str], **kwargs: object) -> SimpleNamespace:
        return SimpleNamespace(stdout="Claude Code v2.1.175", stderr="", returncode=0)


def _runtime_plan(tmp_path: Path):
    executable = tmp_path / "claude"
    executable.write_bytes(b"fake-claude-code-2.1.175")
    return build_managed_runtime_install_plan(
        executable=executable,
        runtime_root=tmp_path / ".zhumeng" / "runtimes",
        runner=VersionRunner(),
    )


def _shape_hash(request: dict[str, object]) -> str:
    def normalize(value: object) -> object:
        if isinstance(value, dict):
            return {str(key).lower(): normalize(nested) for key, nested in sorted(value.items(), key=lambda item: str(item[0]).lower())}
        if isinstance(value, list):
            return [normalize(item) for item in value]
        return value

    payload = (json.dumps(normalize(request), ensure_ascii=True, sort_keys=True, separators=(",", ":")) + "\n").encode("utf-8")
    return "sha256:" + hashlib.sha256(payload).hexdigest()


def _native_baseline_evidence(proof, baseline_request: dict[str, object]) -> dict[str, object]:
    return {
        "baseline_source": "unmodified_claude_code_2.1.175_native_capture",
        "baseline_runtime_version": "2.1.175",
        "baseline_shape_hash": _shape_hash(baseline_request),
        "verifier_green": True,
        "signing_pipeline": "cc_gateway",
        "runtime_hash": proof.runtime_hash,
        "overlay_hash": proof.overlay_hash,
        "native_shape_equality": "passed",
    }


def _exit_gate_evidence(proof) -> dict[str, object]:
    return {
        "native_verifier": {
            "baseline_source": "unmodified_claude_code_2.1.175_native_capture",
            "baseline_runtime_version": "2.1.175",
            "baseline_shape_hash": "sha256:" + "a" * 64,
            "verifier_green": True,
            "signing_pipeline": "cc_gateway",
            "runtime_hash": proof.runtime_hash,
            "overlay_hash": proof.overlay_hash,
            "native_shape_equality": "passed",
        },
        "no_live_formal_pool_bridge_path": {
            "live_catalog_bridge_models_enabled": False,
            "launcher_bridge_transport_connected": False,
            "guard_formal_pool_bridge_admission": False,
            "backend_formal_pool_bridge_admission": False,
        },
        "provider_capability_probe_status": {
            provider: "not_runtime_verified_fail_closed" for provider in {entry.provider for entry in proof.models if entry.provider != "claude"}
        },
    }


def test_cp2_static_patch_points_and_mixed_model_overlay_are_proof_only(tmp_path: Path):
    plan = _runtime_plan(tmp_path)

    proof = build_cp2_model_overlay_proof(plan)

    assert proof.overlay_mode == "proof_only"
    assert proof.bridge_live_feature_flag is False
    assert proof.route_hint_mode == "stub_only_cp4_required"
    assert proof.patch_points == (
        "model_options",
        "agent_model_options",
        "model_validation",
        "display_labels",
        "model_allowlist",
        "route_hint_injection_stub",
    )
    assert set(proof.display_model_ids) == set(proof.model_allowlist)
    assert {entry.model_id for entry in proof.models} >= {
        "claude-sonnet-4-6",
        "openai-catalog-placeholder",
        "deepseek-v4-pro",
        "deepseek-v4-pro[1m]",
        "agnes-1",
        "glm-5.2",
        "glm-5.2[1m]",
        "glm-5-turbo",
        "glm-4.7",
        "glm-4.5-air",
        "kimi-k2.7-code",
        "kimi-k2.7-code-highspeed",
        "kimi-k2.6",
        "kimi-k2.5",
    }
    assert "openai-catalog-placeholder" in proof.model_allowlist
    assert proof.models_by_id["claude-sonnet-4-6"].client_type == "claude_code_native"
    assert proof.models_by_id["openai-catalog-placeholder"].client_type == "claude_code_bridge_openai"
    assert proof.models_by_id["deepseek-v4-pro"].client_type == "claude_code_bridge_deepseek"
    assert proof.models_by_id["deepseek-v4-pro[1m]"].client_type == "claude_code_bridge_deepseek"
    assert proof.models_by_id["deepseek-v4-pro[1m]"].context_window == 1_000_000
    assert proof.models_by_id["deepseek-v4-flash"].context_window == 1_000_000


def test_cp2_model_list_capture_shows_mixed_models_but_marks_bridges_disabled(tmp_path: Path):
    proof = build_cp2_model_overlay_proof(_runtime_plan(tmp_path))

    capture = render_model_list_capture(proof)

    assert "/model overlay proof" in capture
    assert "Claude Sonnet 4.6" in capture
    assert "OpenAI catalog placeholder" in capture
    assert "DeepSeek V4 Pro" in capture
    assert "AGNES 1" in capture
    assert "GLM 5.2" in capture
    assert "Kimi K2.7 Code" in capture
    assert "bridge display only; live disabled until CP4" in capture


def test_cp2_bridge_models_are_display_only_and_never_formal_pool(tmp_path: Path):
    proof = build_cp2_model_overlay_proof(_runtime_plan(tmp_path))

    assert_bridge_models_are_offline_only(proof)
    bridge_entries = [entry for entry in proof.models if entry.route != "claude_native"]
    assert bridge_entries
    for entry in bridge_entries:
        assert entry.live_enabled is False
        assert entry.formal_pool_eligible is False
        assert entry.client_type.startswith("claude_code_bridge_")
        assert entry.client_type != "claude_code_native"
        hint = build_route_hint_stub(proof, entry.model_id)
        assert hint["live_request_allowed"] is False
        assert hint["formal_pool_allowed"] is False
        assert hint["native_attestation_allowed"] is False
        assert hint["requires_cp4_routing_trust_contract"] is True
        with pytest.raises(RuntimeOverlayError, match="display-only until CP4"):
            build_route_hint_stub(proof, entry.model_id, require_live_request=True)


def test_cp2_provider_entries_record_docs_sourced_protocol_and_cache_constraints(tmp_path: Path):
    docs_snapshot_path = Path(__file__).parent / "fixtures" / "claude_code_cp2" / "provider_docs_snapshot.json"
    docs_snapshot = json.loads(docs_snapshot_path.read_text(encoding="utf-8"))
    proof = build_cp2_model_overlay_proof(_runtime_plan(tmp_path))

    assert docs_snapshot["purpose"].startswith("CP2 docs-observed")
    assert proof.catalog_source == "cp2_docs_observed_not_authoritative"
    assert proof.catalog_authoritative is False
    assert_cp2_provider_entries_are_not_runtime_verified(proof)
    assert_cp2_deprecated_provider_aliases_are_not_display_models(proof)
    for entry in proof.models:
        if entry.provider != "claude":
            assert entry.runtime_verified is False
            assert entry.catalog_authoritative is False
            assert entry.compatibility_status.endswith("_not_runtime_verified")

    deepseek = proof.models_by_id["deepseek-v4-pro"]
    assert deepseek.catalog_source == "provider_docs_observed"
    assert deepseek.catalog_authoritative is False
    assert deepseek.api_formats == ("anthropic_messages", "openai_chat_completions")
    assert deepseek.model_id in docs_snapshot["observations"]["deepseek"]["models"]
    deepseek_1m = proof.models_by_id["deepseek-v4-pro[1m]"]
    assert deepseek_1m.model_id in docs_snapshot["observations"]["deepseek"]["models"]
    assert deepseek_1m.context_window == 1_000_000
    assert deepseek_1m.anthropic_base_url == docs_snapshot["observations"]["deepseek"]["anthropic_base_url"]
    assert deepseek_1m.cache_usage_fields == deepseek.cache_usage_fields
    assert deepseek.anthropic_base_url == docs_snapshot["observations"]["deepseek"]["anthropic_base_url"]
    assert deepseek.openai_base_url == docs_snapshot["observations"]["deepseek"]["openai_base_url"]
    assert deepseek.reasoning_effort_levels == ("high", "max")
    assert deepseek.reasoning_mapping["xhigh"] == "max"
    assert deepseek.cache_policy == "provider_prefix_kv_cache_automatic_best_effort"
    assert list(deepseek.cache_usage_fields) == docs_snapshot["observations"]["deepseek"]["cache_usage_fields"]
    assert deepseek.cache_key_strategy == docs_snapshot["observations"]["deepseek"]["cache_key_strategy"]
    assert list(deepseek.deprecated_aliases) == docs_snapshot["observations"]["deepseek"]["deprecated_aliases"]
    assert "api-docs.deepseek.com" in deepseek.provider_docs_url

    glm = proof.models_by_id["glm-5.2"]
    assert glm.provider == "zai_glm"
    assert glm.route == "zai_glm_bridge"
    assert glm.client_type == "claude_code_bridge_zai_glm"
    assert glm.catalog_authoritative is False
    assert glm.api_formats == ("anthropic_messages", "openai_compatible_chat")
    assert glm.model_id in docs_snapshot["observations"]["zai_glm"]["models"]
    assert glm.anthropic_base_url == docs_snapshot["observations"]["zai_glm"]["anthropic_base_url"]
    assert glm.openai_base_url == ""
    assert glm.coding_openai_compatible_base_url == docs_snapshot["observations"]["zai_glm"]["coding_openai_compatible_base_url"]
    assert glm.cache_key_strategy == docs_snapshot["observations"]["zai_glm"]["cache_key_strategy"]
    assert glm.reasoning_mapping["low"] == "high"
    assert glm.reasoning_mapping["max"] == "max"
    assert proof.models_by_id["glm-5.2[1m]"].context_window == 1_000_000
    assert "docs.z.ai" in glm.provider_docs_url

    kimi = proof.models_by_id["kimi-k2.7-code"]
    assert kimi.catalog_authoritative is False
    assert kimi.api_formats == ("anthropic_messages", "openai_chat_completions")
    assert kimi.model_id in docs_snapshot["observations"]["kimi"]["models"]
    assert kimi.anthropic_base_url == docs_snapshot["observations"]["kimi"]["anthropic_base_url"]
    assert kimi.openai_base_url == docs_snapshot["observations"]["kimi"]["openai_base_url"]
    assert kimi.cache_key_strategy == docs_snapshot["observations"]["kimi"]["cache_key_strategy"]
    assert docs_snapshot["observations"]["kimi"]["prompt_cache_key"] is True
    assert docs_snapshot["observations"]["kimi"]["cache_usage_field"] == "usage.cached_tokens"
    assert kimi.reasoning_policy == "always_thinks_preserve_reasoning_content"
    assert set(docs_snapshot["observations"]["kimi"]["deprecated_aliases"]).issubset(set(kimi.deprecated_aliases))
    assert "platform.kimi" in kimi.provider_docs_url


def test_cp2_refuses_to_enable_live_bridge_catalog_before_cp4(tmp_path: Path):
    with pytest.raises(RuntimeOverlayError, match="CP4 routing trust contract"):
        build_cp2_model_overlay_proof(_runtime_plan(tmp_path), bridge_live_feature_flag=True)


def test_cp2_route_hint_stub_for_bridge_fails_closed_before_cp4(tmp_path: Path):
    proof = build_cp2_model_overlay_proof(_runtime_plan(tmp_path))

    hint = build_route_hint_stub(proof, "deepseek-v4-pro")
    hint_1m = build_route_hint_stub(proof, "deepseek-v4-pro[1m]")

    assert hint["model_id"] == "deepseek-v4-pro"
    assert hint["client_type"] == "claude_code_bridge_deepseek"
    assert hint["route"] == "deepseek_bridge"
    assert hint["live_request_allowed"] is False
    assert hint["formal_pool_allowed"] is False
    assert hint["native_attestation_allowed"] is False
    assert hint["requires_cp4_routing_trust_contract"] is True
    assert hint["fail_closed_reason"] == "cp4_routing_trust_contract_not_green"
    assert hint_1m["model_id"] == "deepseek-v4-pro[1m]"
    assert hint_1m["client_type"] == "claude_code_bridge_deepseek"
    assert hint_1m["route"] == "deepseek_bridge"
    assert hint_1m["live_request_allowed"] is False
    assert hint_1m["formal_pool_allowed"] is False
    assert hint_1m["native_attestation_allowed"] is False
    assert hint_1m["requires_cp4_routing_trust_contract"] is True
    assert hint_1m["fail_closed_reason"] == "cp4_routing_trust_contract_not_green"


def test_cp2_native_route_hint_stub_is_proof_only_until_native_exit_gate_evidence(tmp_path: Path):
    proof = build_cp2_model_overlay_proof(_runtime_plan(tmp_path))

    hint = build_route_hint_stub(proof, "claude-sonnet-4-6")

    assert hint["route"] == "claude_native"
    assert hint["live_request_allowed"] is False
    assert hint["formal_pool_allowed"] is False
    assert hint["native_attestation_allowed"] is False
    assert hint["fail_closed_reason"] == "cp2_proof_only_native_verifier_required"
    with pytest.raises(RuntimeOverlayError, match="CP2 route hint stubs never authorize live requests"):
        build_route_hint_stub(proof, "claude-sonnet-4-6", require_live_request=True)


def test_cp2_writes_overlay_proof_artifacts_and_rollback_metadata_under_runtime_root(tmp_path: Path):
    plan = _runtime_plan(tmp_path)
    proof = build_cp2_model_overlay_proof(plan)

    with pytest.raises(RuntimeOverlayError, match="CP2 exit gate evidence is required"):
        write_model_overlay_proof_artifacts(plan, proof)
    artifacts = write_model_overlay_proof_artifacts(plan, proof, exit_gate_evidence=_exit_gate_evidence(proof))

    for path in artifacts.values():
        assert plan.runtime_root in path.parents
    proof_payload = json.loads(artifacts["overlay_proof"].read_text(encoding="utf-8"))
    assert proof_payload["overlay_mode"] == "proof_only"
    assert proof_payload["bridge_live_feature_flag"] is False
    assert proof_payload["route_hint_mode"] == "stub_only_cp4_required"
    assert proof_payload["models"]["deepseek-v4-pro"]["live_enabled"] is False
    assert proof_payload["models"]["deepseek-v4-pro[1m]"]["live_enabled"] is False
    assert proof_payload["models"]["deepseek-v4-pro[1m]"]["formal_pool_eligible"] is False

    route_hint_payload = json.loads(artifacts["route_hint_stub"].read_text(encoding="utf-8"))
    assert route_hint_payload["deepseek-v4-pro"]["live_request_allowed"] is False
    assert route_hint_payload["deepseek-v4-pro[1m]"]["live_request_allowed"] is False
    assert route_hint_payload["deepseek-v4-pro[1m]"]["formal_pool_allowed"] is False
    assert route_hint_payload["deepseek-v4-pro[1m]"]["native_attestation_allowed"] is False
    assert route_hint_payload["openai-catalog-placeholder"]["formal_pool_allowed"] is False
    assert route_hint_payload["claude-sonnet-4-6"]["live_request_allowed"] is False
    assert route_hint_payload["claude-sonnet-4-6"]["native_attestation_allowed"] is False

    rollback = json.loads(artifacts["rollback"].read_text(encoding="utf-8"))
    assert rollback["runtime"] == "claude-code"
    assert rollback["checkpoint"] == "CP2"
    assert rollback["rollback_action"] == "disable_overlay_pointer_without_global_delete"
    assert rollback["global_overwrite"] is False

    patches = json.loads(plan.patches_path.read_text(encoding="utf-8"))
    assert patches["live_bridge_models_enabled"] is False
    assert patches["cp2_model_overlay"]["overlay_mode"] == "proof_only"
    assert patches["cp2_model_overlay"]["route_hint_mode"] == "stub_only_cp4_required"
    assert patches["cp2_model_overlay"]["artifact_dir"] == str(artifacts["overlay_proof"].parent)
    assert "route_hint_injection_stub" in patches["patch_points"]


def test_cp2_bridge_selection_cannot_build_live_formal_pool_request(tmp_path: Path):
    proof = build_cp2_model_overlay_proof(_runtime_plan(tmp_path))

    with pytest.raises(RuntimeOverlayError, match="display-only until CP4"):
        build_route_hint_stub(proof, "openai-catalog-placeholder", require_live_request=True)


def test_cp2_model_overlay_public_api_is_exported_from_adapter_package():
    from zhumeng_agent.adapters.claude_code import (  # noqa: PLC0415
        RuntimeOverlayError as ExportedError,
        RuntimeModelOverlayEntry as ExportedEntry,
        RuntimeModelOverlayProof as ExportedProof,
        assert_bridge_models_are_offline_only as exported_offline_only,
        assert_cp2_deprecated_provider_aliases_are_not_display_models as exported_no_aliases,
        assert_cp2_native_shape_equality as exported_shape_equality,
        assert_cp2_no_live_formal_pool_bridge_path as exported_no_live_bridge,
        assert_cp2_provider_capabilities_require_probe as exported_require_probe,
        assert_cp2_provider_entries_are_not_runtime_verified as exported_provider_not_verified,
        assert_cp2_signing_verifier_gate as exported_signing_gate,
        build_cp2_model_overlay_proof as exported_build_proof,
        build_cp2_print_smoke_plan as exported_print_smoke,
        build_route_hint_stub as exported_route_hint,
        disable_model_overlay_proof as exported_disable_overlay,
        assert_cp2_exit_gate as exported_exit_gate,
        write_model_overlay_proof_artifacts as exported_write_artifacts,
        probe_cp2_patch_points as exported_probe,
        run_cp2_print_smoke_with_stubbed_runner as exported_print_runner,
    )
    from zhumeng_agent.adapters.claude_code.model_overlay import (  # noqa: PLC0415
        RuntimeModelOverlayEntry,
        RuntimeModelOverlayProof,
        assert_cp2_exit_gate,
        assert_bridge_models_are_offline_only,
        assert_cp2_deprecated_provider_aliases_are_not_display_models,
        assert_cp2_native_shape_equality,
        assert_cp2_no_live_formal_pool_bridge_path,
        assert_cp2_provider_capabilities_require_probe,
        assert_cp2_provider_entries_are_not_runtime_verified,
        assert_cp2_signing_verifier_gate,
        build_cp2_print_smoke_plan,
        build_route_hint_stub,
        disable_model_overlay_proof,
        probe_cp2_patch_points,
        run_cp2_print_smoke_with_stubbed_runner,
        write_model_overlay_proof_artifacts,
    )

    assert ExportedError is RuntimeOverlayError
    assert ExportedEntry is RuntimeModelOverlayEntry
    assert ExportedProof is RuntimeModelOverlayProof
    assert exported_build_proof is build_cp2_model_overlay_proof
    assert exported_print_smoke is build_cp2_print_smoke_plan
    assert exported_shape_equality is assert_cp2_native_shape_equality
    assert exported_signing_gate is assert_cp2_signing_verifier_gate
    assert exported_probe is probe_cp2_patch_points
    assert exported_route_hint is build_route_hint_stub
    assert exported_no_live_bridge is assert_cp2_no_live_formal_pool_bridge_path
    assert exported_disable_overlay is disable_model_overlay_proof
    assert exported_print_runner is run_cp2_print_smoke_with_stubbed_runner
    assert exported_require_probe is assert_cp2_provider_capabilities_require_probe
    assert exported_offline_only is assert_bridge_models_are_offline_only
    assert exported_no_aliases is assert_cp2_deprecated_provider_aliases_are_not_display_models
    assert exported_provider_not_verified is assert_cp2_provider_entries_are_not_runtime_verified
    assert exported_exit_gate is assert_cp2_exit_gate
    assert exported_write_artifacts is write_model_overlay_proof_artifacts


def test_cp2_print_smoke_plan_is_mock_only_and_uses_model_list_capture(tmp_path: Path):
    proof = build_cp2_model_overlay_proof(_runtime_plan(tmp_path))

    from zhumeng_agent.adapters.claude_code.model_overlay import build_cp2_print_smoke_plan  # noqa: PLC0415

    smoke = build_cp2_print_smoke_plan(proof, prompt="/model")

    assert smoke["mode"] == "mock_only"
    assert smoke["command"] == ["claude", "--print", "/model"]
    assert smoke["will_start_process"] is False
    assert smoke["live_bridge_models_enabled"] is False
    assert "OpenAI catalog placeholder" in smoke["expected_model_list_capture"]
    assert "bridge display only; live disabled until CP4" in smoke["expected_model_list_capture"]


def test_cp2_native_shape_equality_fixture_keeps_body_and_headers_unmodified(tmp_path: Path):
    proof = build_cp2_model_overlay_proof(_runtime_plan(tmp_path))
    native_request = {
        "headers": {
            "content-type": "application/json",
            "anthropic-version": "2023-06-01",
            "x-claude-code-session-id": "11111111-2222-4333-8444-555555555555",
        },
        "body": {
            "model": "claude-sonnet-4-6",
            "messages": [{"role": "user", "content": "hello"}],
            "max_tokens": 64,
        },
    }
    baseline_request = json.loads(json.dumps(native_request))

    from zhumeng_agent.adapters.claude_code.model_overlay import assert_cp2_native_shape_equality  # noqa: PLC0415

    assert_cp2_native_shape_equality(
        proof,
        native_request,
        baseline_request=baseline_request,
        baseline_evidence=_native_baseline_evidence(proof, baseline_request),
    )
    assert native_request["body"]["model"] == "claude-sonnet-4-6"


def test_cp2_native_shape_equality_rejects_self_baseline_and_missing_evidence(tmp_path: Path):
    proof = build_cp2_model_overlay_proof(_runtime_plan(tmp_path))
    native_request = {
        "headers": {"content-type": "application/json", "anthropic-version": "2023-06-01"},
        "body": {"model": "claude-sonnet-4-6", "messages": [{"role": "user", "content": "hello"}], "max_tokens": 64},
    }
    baseline_request = json.loads(json.dumps(native_request))

    from zhumeng_agent.adapters.claude_code.model_overlay import assert_cp2_native_shape_equality  # noqa: PLC0415

    with pytest.raises(RuntimeOverlayError, match="baseline fixture cannot be the request object itself"):
        assert_cp2_native_shape_equality(
            proof,
            native_request,
            baseline_request=native_request,
            baseline_evidence=_native_baseline_evidence(proof, baseline_request),
        )
    with pytest.raises(RuntimeOverlayError, match="requires signed 2.1.175 baseline evidence"):
        assert_cp2_native_shape_equality(proof, native_request, baseline_request=baseline_request)
    bad_evidence = {**_native_baseline_evidence(proof, baseline_request), "baseline_runtime_version": "2.1.176"}
    with pytest.raises(RuntimeOverlayError, match="requires signed 2.1.175 baseline evidence"):
        assert_cp2_native_shape_equality(proof, native_request, baseline_request=baseline_request, baseline_evidence=bad_evidence)
    bad_hash = {**_native_baseline_evidence(proof, baseline_request), "baseline_shape_hash": "sha256:" + "0" * 64}
    with pytest.raises(RuntimeOverlayError, match="baseline shape hash mismatch"):
        assert_cp2_native_shape_equality(proof, native_request, baseline_request=baseline_request, baseline_evidence=bad_hash)


def test_cp2_native_shape_equality_rejects_bridge_model_shape(tmp_path: Path):
    proof = build_cp2_model_overlay_proof(_runtime_plan(tmp_path))
    bridge_request = {
        "headers": {"content-type": "application/json"},
        "body": {"model": "openai-catalog-placeholder", "messages": [], "max_tokens": 64},
    }

    from zhumeng_agent.adapters.claude_code.model_overlay import assert_cp2_native_shape_equality  # noqa: PLC0415

    with pytest.raises(RuntimeOverlayError, match="native shape equality only applies to Claude native"):
        assert_cp2_native_shape_equality(
            proof,
            bridge_request,
            baseline_request=json.loads(json.dumps(bridge_request)),
            baseline_evidence=_native_baseline_evidence(proof, bridge_request),
        )


def test_cp2_static_patch_probe_reads_runtime_files_and_reports_found_points(tmp_path: Path):
    plan = _runtime_plan(tmp_path)
    bundle = plan.version_dir / "upstream" / "cli.js"
    bundle.parent.mkdir(parents=True)
    bundle.write_text(
        "const model_options = []; const agent_model_options = [];\n"
        "function model_validation(){}; const display_labels = {};\n"
        "const model_allowlist = []; function route_hint_injection(){};\n",
        encoding="utf-8",
    )

    from zhumeng_agent.adapters.claude_code.model_overlay import probe_cp2_patch_points  # noqa: PLC0415

    probe = probe_cp2_patch_points(plan)

    assert probe["runtime_version"] == "2.1.175"
    assert probe["status"] == "degraded_fail_closed"
    assert probe["missing_patch_points"] == []
    assert probe["patch_points"]["model_options"]["found"] is True
    assert probe["patch_points"]["route_hint_injection_stub"]["mode"] == "degraded_stub"
    assert probe["unsafe_patch_points"] == ["route_hint_injection_stub"]
    assert probe["live_bridge_models_enabled"] is False


def test_cp2_static_patch_probe_missing_route_hint_is_degraded_fail_closed(tmp_path: Path):
    plan = _runtime_plan(tmp_path)
    bundle = plan.version_dir / "upstream" / "cli.js"
    bundle.parent.mkdir(parents=True)
    bundle.write_text("const model_options = [];\n", encoding="utf-8")

    from zhumeng_agent.adapters.claude_code.model_overlay import probe_cp2_patch_points  # noqa: PLC0415

    probe = probe_cp2_patch_points(plan)

    assert probe["status"] == "degraded_fail_closed"
    assert "route_hint_injection_stub" in probe["missing_patch_points"]
    assert probe["live_bridge_models_enabled"] is False


def test_cp2_rejects_non_claude_native_eligibility_spoof(tmp_path: Path):
    from zhumeng_agent.adapters.claude_code.model_overlay import RuntimeModelOverlayEntry, RuntimeModelOverlayProof  # noqa: PLC0415

    plan = _runtime_plan(tmp_path)
    spoof = RuntimeModelOverlayProof(
        runtime_hash=plan.manifest.upstream_hash,
        overlay_hash=plan.manifest.overlay_hash,
        overlay_mode="proof_only",
        bridge_live_feature_flag=False,
        route_hint_mode="stub_only_cp4_required",
        patch_points=("model_options",),
        models=(
            RuntimeModelOverlayEntry(
                model_id="openai-catalog-placeholder",
                display_label="OpenAI catalog placeholder",
                provider="openai",
                route="claude_native",
                client_type="claude_code_native",
                live_enabled=True,
                formal_pool_eligible=True,
            ),
        ),
    )

    with pytest.raises(RuntimeOverlayError, match="native formal-pool eligibility requires Claude provider"):
        assert_bridge_models_are_offline_only(spoof)


def test_cp2_native_shape_equality_compares_against_baseline_and_rejects_mutation(tmp_path: Path):
    proof = build_cp2_model_overlay_proof(_runtime_plan(tmp_path))
    baseline = {
        "headers": {"content-type": "application/json", "anthropic-version": "2023-06-01"},
        "body": {"model": "claude-sonnet-4-6", "messages": [{"role": "user", "content": "hello"}], "max_tokens": 64},
    }
    mutated = {
        "headers": {
            "content-type": "application/json",
            "anthropic-version": "2023-06-01",
            "x-sub2api-route-hint": "should-not-be-on-native-shape",
        },
        "body": {"model": "claude-sonnet-4-6", "messages": [{"role": "user", "content": "hello"}], "max_tokens": 64},
    }

    from zhumeng_agent.adapters.claude_code.model_overlay import assert_cp2_native_shape_equality  # noqa: PLC0415

    with pytest.raises(RuntimeOverlayError, match="native request shape changed"):
        assert_cp2_native_shape_equality(
            proof,
            mutated,
            baseline_request=baseline,
            baseline_evidence=_native_baseline_evidence(proof, baseline),
        )


def test_cp2_signing_verifier_gate_fails_closed_until_verified(tmp_path: Path):
    proof = build_cp2_model_overlay_proof(_runtime_plan(tmp_path))

    from zhumeng_agent.adapters.claude_code.model_overlay import assert_cp2_signing_verifier_gate  # noqa: PLC0415

    with pytest.raises(RuntimeOverlayError, match="CC Gateway signing/verifier parity is not green"):
        assert_cp2_signing_verifier_gate(proof, evidence={"verifier_green": False})
    assert_cp2_signing_verifier_gate(
        proof,
        evidence={
            "baseline_source": "unmodified_claude_code_2.1.175_native_capture",
            "baseline_runtime_version": "2.1.175",
            "baseline_shape_hash": "sha256:" + "b" * 64,
            "verifier_green": True,
            "signing_pipeline": "cc_gateway",
            "runtime_hash": proof.runtime_hash,
            "overlay_hash": proof.overlay_hash,
            "native_shape_equality": "passed",
        },
    )


def test_cp2_provider_capabilities_fail_closed_until_runtime_probe_exists(tmp_path: Path):
    proof = build_cp2_model_overlay_proof(_runtime_plan(tmp_path))

    for provider in {entry.provider for entry in proof.models if entry.provider != "claude"}:
        with pytest.raises(RuntimeOverlayError, match="provider compatibility requires signed catalog and capability probe"):
            assert_cp2_provider_capabilities_require_probe(proof, provider)


def test_cp2_provider_capability_gate_ignores_runtime_verified_spoof_without_probe(tmp_path: Path):
    from zhumeng_agent.adapters.claude_code.model_overlay import RuntimeModelOverlayEntry, RuntimeModelOverlayProof  # noqa: PLC0415

    plan = _runtime_plan(tmp_path)
    spoof = RuntimeModelOverlayProof(
        runtime_hash=plan.manifest.upstream_hash,
        overlay_hash=plan.manifest.overlay_hash,
        overlay_mode="proof_only",
        bridge_live_feature_flag=False,
        route_hint_mode="stub_only_cp4_required",
        patch_points=("model_options",),
        models=(
            RuntimeModelOverlayEntry(
                model_id="openai-catalog-placeholder",
                display_label="OpenAI catalog placeholder",
                provider="openai",
                route="openai_bridge",
                client_type="claude_code_bridge_openai",
                live_enabled=False,
                formal_pool_eligible=False,
                runtime_verified=True,
            ),
        ),
    )

    with pytest.raises(RuntimeOverlayError, match="provider compatibility requires signed catalog and capability probe"):
        assert_cp2_provider_capabilities_require_probe(spoof, "openai")


def test_cp2_exit_gate_requires_all_evidence(tmp_path: Path):
    proof = build_cp2_model_overlay_proof(_runtime_plan(tmp_path))
    evidence = _exit_gate_evidence(proof)

    assert_cp2_exit_gate(proof, evidence=evidence)
    with pytest.raises(RuntimeOverlayError, match="CP2 provider probe status must remain fail-closed"):
        assert_cp2_exit_gate(proof, evidence={**evidence, "provider_capability_probe_status": {"deepseek": "runtime_verified"}})


def test_cp2_no_live_formal_pool_bridge_path_requires_all_live_evidence_false(tmp_path: Path):
    proof = build_cp2_model_overlay_proof(_runtime_plan(tmp_path))

    with pytest.raises(RuntimeOverlayError, match="must not connect to live formal-pool native path"):
        assert_cp2_no_live_formal_pool_bridge_path(
            proof,
            evidence={
                "live_catalog_bridge_models_enabled": False,
                "launcher_bridge_transport_connected": True,
                "guard_formal_pool_bridge_admission": False,
                "backend_formal_pool_bridge_admission": False,
            },
        )
    assert_cp2_no_live_formal_pool_bridge_path(
        proof,
        evidence={
            "live_catalog_bridge_models_enabled": False,
            "launcher_bridge_transport_connected": False,
            "guard_formal_pool_bridge_admission": False,
            "backend_formal_pool_bridge_admission": False,
        },
    )


def test_cp2_print_smoke_uses_stubbed_runner_without_starting_live_process(tmp_path: Path):
    proof = build_cp2_model_overlay_proof(_runtime_plan(tmp_path))

    class StubRunner:
        is_cp2_stub_runner = True

        def __call__(self):
            return render_model_list_capture(proof)

    smoke = run_cp2_print_smoke_with_stubbed_runner(proof, StubRunner())

    assert smoke["mode"] == "stubbed_runner"
    assert smoke["will_start_process"] is False
    assert smoke["live_bridge_models_enabled"] is False
    assert smoke["verified"] is True
    assert smoke["missing_labels"] == []


def test_cp2_print_smoke_rejects_unmarked_runner_without_calling_it(tmp_path: Path):
    proof = build_cp2_model_overlay_proof(_runtime_plan(tmp_path))

    class LiveLikeRunner:
        called = False

        def __call__(self, *_args, **_kwargs):
            self.called = True
            return "would have spawned claude"

    runner = LiveLikeRunner()
    with pytest.raises(RuntimeOverlayError, match="explicit CP2 stub runner"):
        run_cp2_print_smoke_with_stubbed_runner(proof, runner)
    assert runner.called is False


def test_cp2_disable_model_overlay_proof_simulates_rollback_without_global_delete(tmp_path: Path):
    plan = _runtime_plan(tmp_path)
    proof = build_cp2_model_overlay_proof(plan)
    write_model_overlay_proof_artifacts(plan, proof, exit_gate_evidence=_exit_gate_evidence(proof))

    rollback = disable_model_overlay_proof(plan)

    assert rollback["overlay_disabled"] is True
    assert rollback["global_overwrite"] is False
    patches = json.loads(plan.patches_path.read_text(encoding="utf-8"))
    assert patches["live_bridge_models_enabled"] is False
    assert patches["cp2_model_overlay"]["enabled"] is False
    assert patches["cp2_model_overlay"]["rollback_action"] == "disable_overlay_pointer_without_global_delete"


def test_cp2_patch_probe_verifies_expected_bundle_hash_before_ready(tmp_path: Path):
    plan = _runtime_plan(tmp_path)
    bundle = plan.version_dir / "upstream" / "cli.js"
    bundle.parent.mkdir(parents=True)
    bundle.write_text("const model_options = [];\n", encoding="utf-8")

    from zhumeng_agent.adapters.claude_code.model_overlay import probe_cp2_patch_points  # noqa: PLC0415

    probe = probe_cp2_patch_points(plan, expected_file_hashes={"cli.js": "sha256:not-the-real-hash"})

    assert probe["status"] == "degraded_fail_closed"
    assert probe["bundle_hash_verified"] is False
    assert probe["hash_mismatches"] == ["cli.js"]


def test_cp2_overlay_does_not_hardcode_stale_provider_model_aliases(tmp_path: Path):
    proof = build_cp2_model_overlay_proof(_runtime_plan(tmp_path))
    model_ids = set(proof.model_allowlist)

    assert "deepseek-chat" not in model_ids
    assert "glm-4.6" not in model_ids
    assert "kimi-k2" not in model_ids
    assert "gpt-5.4" not in model_ids
    assert "gpt-5.5" not in model_ids
    assert "openai-catalog-placeholder" in model_ids
    capture = render_model_list_capture(proof)
    route_hint_payload = {entry.model_id: build_route_hint_stub(proof, entry.model_id) for entry in proof.models}
    for alias in {alias for entry in proof.models for alias in entry.deprecated_aliases}:
        assert alias not in model_ids
        assert alias not in capture
        assert alias not in route_hint_payload
    openai_ids = [entry.model_id for entry in proof.models if entry.provider == "openai"]
    assert openai_ids == ["openai-catalog-placeholder"]
