from __future__ import annotations

import json
from pathlib import Path
from types import SimpleNamespace

import pytest

from zhumeng_agent.adapters.claude_code.model_overlay import (
    RuntimeOverlayError,
    build_agent_model_options,
    build_cp2_model_overlay_proof,
    build_cp3a_model_overlay_contract,
    probe_cp3a_patch_points,
    resolve_background_model,
    resolve_subagent_model,
    resolve_workflow_model_alias,
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


def _contract(tmp_path: Path):
    return build_cp3a_model_overlay_contract(build_cp2_model_overlay_proof(_runtime_plan(tmp_path)))


def test_cp3a_uses_official_docs_snapshot_but_keeps_provider_capabilities_unverified():
    snapshot_path = Path(__file__).parent / "fixtures" / "claude_code_cp3" / "provider_docs_snapshot.json"
    snapshot = json.loads(snapshot_path.read_text(encoding="utf-8"))

    assert snapshot["captured_at"] == "2026-06-16"
    assert snapshot["purpose"].startswith("CP3 official-docs snapshot")
    assert snapshot["observations"]["deepseek"]["models"] == ["deepseek-v4-pro", "deepseek-v4-pro[1m]", "deepseek-v4-flash"]
    assert snapshot["observations"]["deepseek"]["context_windows"]["deepseek-v4-pro[1m]"] == 1_000_000
    assert snapshot["observations"]["deepseek"]["thinking_effort_mapping"] == {
        "low": "high",
        "medium": "high",
        "high": "high",
        "xhigh": "max",
        "max": "max",
    }
    assert snapshot["observations"]["deepseek"]["cache_usage_fields"] == [
        "prompt_cache_hit_tokens",
        "prompt_cache_miss_tokens",
    ]
    assert "glm-4.6" not in snapshot["observations"]["zai_glm"]["claude_code_display_models"]
    assert "glm-5.1" in snapshot["observations"]["zai_glm"]["docs_observed_language_models"]
    assert "glm-5.2" in snapshot["observations"]["zai_glm"]["claude_code_display_models"]
    assert snapshot["observations"]["kimi"]["prompt_cache_key"] is True
    assert snapshot["observations"]["kimi"]["cache_usage_field"] == "usage.cached_tokens"
    assert snapshot["observations"]["openai"]["catalog_strategy"] == "signed_dynamic_catalog_required_no_static_gpt_defaults"
    for provider in snapshot["observations"].values():
        assert provider["runtime_verified"] is False


def test_cp3a_zai_glm_provider_profile_uses_zai_glm_route_family(tmp_path: Path):
    contract = _contract(tmp_path)

    profile = contract.provider_profiles_by_provider["zai_glm"]

    assert profile.profile_id == "zai_glm"
    assert profile.provider == "zai_glm"
    assert profile.main_model_id == "glm-5.2[1m]"
    assert profile.fast_model_id == "glm-5-turbo"


def test_cp3a_agent_model_options_default_to_inherit(tmp_path: Path):
    contract = _contract(tmp_path)

    options = build_agent_model_options(contract)

    assert options[0].option_id == "inherit"
    assert options[0].is_default is True
    assert options[0].provider == "inherit"
    assert any(option.model_id == "deepseek-v4-flash" and option.provider == "deepseek" for option in options)
    assert any(option.model_id == "claude-sonnet-4-6" and option.native_egress_allowed for option in options)


def test_cp3a_deepseek_parent_subagent_inherit_stays_deepseek(tmp_path: Path):
    contract = _contract(tmp_path)

    resolution = resolve_subagent_model(contract, parent_model_id="deepseek-v4-pro", requested_model="inherit")

    assert resolution.resolved_model_id == "deepseek-v4-pro"
    assert resolution.provider == "deepseek"
    assert resolution.route == "deepseek_bridge"
    assert resolution.client_type == "claude_code_bridge_deepseek"
    assert resolution.native_egress_allowed is False
    assert resolution.formal_pool_allowed is False
    assert resolution.replay_boundary == "same_provider"


@pytest.mark.parametrize("requested", ["haiku", "fast", "simple"])
def test_cp3a_provider_local_fast_mapping_for_subagents(tmp_path: Path, requested: str):
    contract = _contract(tmp_path)

    resolution = resolve_subagent_model(contract, parent_model_id="deepseek-v4-pro", requested_model=requested)

    assert resolution.resolved_model_id == "deepseek-v4-flash"
    assert resolution.provider == "deepseek"
    assert resolution.native_egress_allowed is False
    assert resolution.resolution_source == "provider_local_alias"


@pytest.mark.parametrize("requested", ["fast", "simple", "haiku", "claude-haiku-legacy-hardcoded"])
def test_cp3a_workflow_aliases_remap_to_active_non_claude_profile(tmp_path: Path, requested: str):
    contract = _contract(tmp_path)

    resolution = resolve_workflow_model_alias(contract, active_model_id="deepseek-v4-pro", requested_model=requested)

    assert resolution.resolved_model_id == "deepseek-v4-flash"
    assert resolution.provider == "deepseek"
    assert resolution.native_egress_allowed is False
    assert resolution.formal_pool_allowed is False


@pytest.mark.parametrize("task", ["title", "compact", "summary", "probe", "fast", "simple", "haiku"])
def test_cp3a_background_models_resolve_from_active_profile_each_request(tmp_path: Path, task: str):
    contract = _contract(tmp_path)

    started_on_claude = resolve_background_model(contract, active_model_id="claude-sonnet-4-6", task=task)
    after_switch_to_deepseek = resolve_background_model(contract, active_model_id="deepseek-v4-pro", task=task)

    assert started_on_claude.provider == "claude"
    assert started_on_claude.native_egress_allowed is True
    assert after_switch_to_deepseek.resolved_model_id == "deepseek-v4-flash"
    assert after_switch_to_deepseek.provider == "deepseek"
    assert after_switch_to_deepseek.native_egress_allowed is False
    assert after_switch_to_deepseek.formal_pool_allowed is False
    assert after_switch_to_deepseek.dynamic_profile_resolved is True


def test_cp3a_explicit_claude_subagent_goes_native_and_is_auditable(tmp_path: Path):
    contract = _contract(tmp_path)

    resolution = resolve_subagent_model(
        contract,
        parent_model_id="deepseek-v4-pro",
        requested_model="claude-sonnet-4-6",
        explicit_claude_opt_in=True,
    )

    assert resolution.resolved_model_id == "claude-sonnet-4-6"
    assert resolution.provider == "claude"
    assert resolution.route == "claude_native"
    assert resolution.client_type == "claude_code_native"
    assert resolution.native_egress_allowed is True
    assert resolution.formal_pool_allowed is True
    assert resolution.explicit_claude_opt_in is True
    assert resolution.audit_label == "explicit_claude_formal_pool_subagent"


def test_cp3a_hardcoded_claude_workflow_fails_closed_without_opt_in_when_no_mapping(tmp_path: Path):
    contract = _contract(tmp_path)

    with pytest.raises(RuntimeOverlayError, match="explicit Claude opt-in"):
        resolve_workflow_model_alias(
            contract,
            active_model_id="deepseek-v4-pro",
            requested_model="claude-opus-4-7",
            allow_hardcoded_claude_remap=False,
        )


def test_cp3a_unknown_model_fails_closed(tmp_path: Path):
    contract = _contract(tmp_path)

    with pytest.raises(RuntimeOverlayError, match="unknown Claude Code runtime model"):
        resolve_subagent_model(contract, parent_model_id="deepseek-v4-pro", requested_model="glm-4.6")


@pytest.mark.parametrize("requested", ["deepseek-v4-pro", "deepseek-v4-flash"])
def test_cp3a_claude_parent_to_deepseek_subagent_requires_boundary(tmp_path: Path, requested: str):
    contract = _contract(tmp_path)

    resolution = resolve_subagent_model(contract, parent_model_id="claude-opus-4-7", requested_model=requested)

    assert resolution.provider == "deepseek"
    assert resolution.native_egress_allowed is False
    assert resolution.formal_pool_allowed is False
    assert resolution.replay_boundary == "safe_tool_result"
    assert resolution.raw_history_replay_allowed is False


def test_cp3a_patch_probe_requires_dynamic_background_resolver_before_ready(tmp_path: Path):
    plan = _runtime_plan(tmp_path)
    bundle = plan.version_dir / "upstream" / "cli.js"
    bundle.parent.mkdir(parents=True)
    bundle.write_text(
        "const getAgentModelOptions = () => agent_model_options;\n"
        "function resolveAgentModel(){ return active_profile_dynamic_model_resolver(); }\n",
        encoding="utf-8",
    )

    probe = probe_cp3a_patch_points(plan)

    assert probe["status"] == "degraded_fail_closed"
    assert "background_model_resolver" in probe["missing_patch_points"]
    assert probe["bridge_live_feature_flag"] is False
    assert probe["native_egress_allowed_when_probe_missing"] is False


def test_cp3a_public_api_is_exported_from_adapter_package():
    from zhumeng_agent.adapters.claude_code import (  # noqa: PLC0415
        RuntimeAgentModelOption as ExportedOption,
        RuntimeModelOverlayContract as ExportedContract,
        RuntimeModelResolution as ExportedResolution,
        RuntimeProviderProfile as ExportedProviderProfile,
        build_agent_model_options as exported_options,
        build_cp3a_model_overlay_contract as exported_contract,
        probe_cp3a_patch_points as exported_probe,
        resolve_background_model as exported_background,
        resolve_subagent_model as exported_subagent,
        resolve_workflow_model_alias as exported_workflow,
    )
    from zhumeng_agent.adapters.claude_code.model_overlay import (  # noqa: PLC0415
        RuntimeAgentModelOption,
        RuntimeModelOverlayContract,
        RuntimeModelResolution,
        RuntimeProviderProfile,
        build_agent_model_options,
        build_cp3a_model_overlay_contract,
        probe_cp3a_patch_points,
        resolve_background_model,
        resolve_subagent_model,
        resolve_workflow_model_alias,
    )

    assert ExportedOption is RuntimeAgentModelOption
    assert ExportedContract is RuntimeModelOverlayContract
    assert ExportedResolution is RuntimeModelResolution
    assert ExportedProviderProfile is RuntimeProviderProfile
    assert exported_options is build_agent_model_options
    assert exported_contract is build_cp3a_model_overlay_contract
    assert exported_probe is probe_cp3a_patch_points
    assert exported_background is resolve_background_model
    assert exported_subagent is resolve_subagent_model
    assert exported_workflow is resolve_workflow_model_alias


def test_cp3a_rejects_provider_profile_alias_that_points_to_claude_native(tmp_path: Path):
    from zhumeng_agent.adapters.claude_code.model_overlay import RuntimeProviderProfile  # noqa: PLC0415

    proof = build_cp2_model_overlay_proof(_runtime_plan(tmp_path))
    with pytest.raises(RuntimeOverlayError, match="provider-local aliases must stay within the provider"):
        build_cp3a_model_overlay_contract(
            proof,
            provider_profiles=(
                RuntimeProviderProfile(
                    profile_id="bad-deepseek",
                    provider="deepseek",
                    main_model_id="deepseek-v4-pro",
                    fast_model_id="claude-sonnet-4-6",
                    family_aliases={"fast": "claude-sonnet-4-6"},
                ),
            ),
        )


def test_cp3a_workflow_explicit_claude_opt_in_has_audit_label(tmp_path: Path):
    contract = _contract(tmp_path)

    resolution = resolve_workflow_model_alias(
        contract,
        active_model_id="deepseek-v4-pro",
        requested_model="claude-opus-4-7",
        explicit_claude_opt_in=True,
    )

    assert resolution.native_egress_allowed is True
    assert resolution.formal_pool_allowed is True
    assert resolution.audit_label == "explicit_claude_formal_pool_workflow"
