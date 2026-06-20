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
    assert snapshot["observations"]["deepseek"]["native_reasoning_effort_levels"] == ["high", "max"]
    assert snapshot["observations"]["deepseek"]["thinking_effort_mapping"] == {
        "high": "high",
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
    assert profile.main_model_id == "claude-code-bridge-glm-5.2-1m"
    assert profile.fast_model_id == "claude-code-bridge-glm-5.2-1m"


def test_cp3a_agent_model_options_default_to_inherit(tmp_path: Path):
    contract = _contract(tmp_path)

    options = build_agent_model_options(contract)

    assert options[0].option_id == "inherit"
    assert options[0].is_default is True
    assert options[0].provider == "inherit"
    assert any(option.model_id == "claude-code-bridge-deepseek-v4-pro" and option.provider == "deepseek" for option in options)
    assert any(option.model_id == "claude-code-bridge-deepseek-v4-flash" and option.provider == "deepseek" for option in options)
    assert any(option.model_id == "claude-code-bridge-gpt-5.4-mini" and option.provider == "openai" for option in options)
    assert any(option.model_id == "claude-sonnet-4-6" and option.native_egress_allowed for option in options)


def test_cp3a_deepseek_parent_subagent_inherit_stays_deepseek(tmp_path: Path):
    contract = _contract(tmp_path)

    resolution = resolve_subagent_model(contract, parent_model_id="claude-code-bridge-deepseek-v4-pro", requested_model="inherit")

    assert resolution.resolved_model_id == "claude-code-bridge-deepseek-v4-pro"
    assert resolution.provider == "deepseek"
    assert resolution.route == "deepseek_bridge"
    assert resolution.client_type == "claude_code_bridge_deepseek"
    assert resolution.native_egress_allowed is False
    assert resolution.formal_pool_allowed is False
    assert resolution.replay_boundary == "same_provider"


@pytest.mark.parametrize("requested", ["haiku", "fast", "simple"])
def test_cp3a_provider_local_fast_mapping_for_subagents(tmp_path: Path, requested: str):
    contract = _contract(tmp_path)

    resolution = resolve_subagent_model(contract, parent_model_id="claude-code-bridge-deepseek-v4-pro", requested_model=requested)

    assert resolution.resolved_model_id == "claude-code-bridge-deepseek-v4-flash"
    assert resolution.provider == "deepseek"
    assert resolution.native_egress_allowed is False
    assert resolution.resolution_source == "provider_local_alias"


@pytest.mark.parametrize("requested", ["fast", "simple", "haiku", "claude-haiku-legacy-hardcoded"])
def test_cp3a_workflow_aliases_remap_to_active_non_claude_profile(tmp_path: Path, requested: str):
    contract = _contract(tmp_path)

    resolution = resolve_workflow_model_alias(contract, active_model_id="claude-code-bridge-deepseek-v4-pro", requested_model=requested)

    assert resolution.resolved_model_id == "claude-code-bridge-deepseek-v4-flash"
    assert resolution.provider == "deepseek"
    assert resolution.native_egress_allowed is False
    assert resolution.formal_pool_allowed is False


@pytest.mark.parametrize("task", ["title", "compact", "summary", "probe", "fast", "simple", "haiku"])
def test_cp3a_background_models_resolve_from_active_profile_each_request(tmp_path: Path, task: str):
    contract = _contract(tmp_path)

    started_on_claude = resolve_background_model(contract, active_model_id="claude-sonnet-4-6", task=task)
    after_switch_to_deepseek = resolve_background_model(contract, active_model_id="claude-code-bridge-deepseek-v4-pro", task=task)

    assert started_on_claude.provider == "claude"
    assert started_on_claude.native_egress_allowed is True
    assert after_switch_to_deepseek.resolved_model_id == "claude-code-bridge-deepseek-v4-flash"
    assert after_switch_to_deepseek.provider == "deepseek"
    assert after_switch_to_deepseek.native_egress_allowed is False
    assert after_switch_to_deepseek.formal_pool_allowed is False
    assert after_switch_to_deepseek.dynamic_profile_resolved is True


def test_cp3a_official_agent_aliases_resolve_against_parent_profile(tmp_path: Path):
    contract = _contract(tmp_path)

    claude_opus = resolve_subagent_model(contract, parent_model_id="claude-sonnet-4-6", requested_model="opus")
    claude_sonnet = resolve_subagent_model(contract, parent_model_id="claude-opus-4-8", requested_model="sonnet")
    deepseek_opus = resolve_subagent_model(contract, parent_model_id="claude-code-bridge-deepseek-v4-pro", requested_model="opus")
    gpt_sonnet = resolve_subagent_model(contract, parent_model_id="claude-code-bridge-gpt-5.5", requested_model="sonnet")

    assert claude_opus.resolved_model_id == "claude-opus-4-8"
    assert claude_opus.provider == "claude"
    assert claude_opus.native_egress_allowed is True
    assert claude_sonnet.resolved_model_id == "claude-sonnet-4-6"
    assert claude_sonnet.provider == "claude"
    assert deepseek_opus.resolved_model_id == "claude-code-bridge-deepseek-v4-pro"
    assert deepseek_opus.provider == "deepseek"
    assert deepseek_opus.formal_pool_allowed is False
    assert gpt_sonnet.resolved_model_id == "claude-code-bridge-gpt-5.5"
    assert gpt_sonnet.provider == "openai"


def test_cp3a_fable_agent_alias_does_not_consume_formal_pool_without_curated_model(tmp_path: Path):
    contract = _contract(tmp_path)

    with pytest.raises(RuntimeOverlayError, match="unknown Claude Code runtime model alias for provider: fable"):
        resolve_subagent_model(contract, parent_model_id="claude-opus-4-8", requested_model="fable")


def test_cp3a_explicit_claude_subagent_goes_native_and_is_auditable(tmp_path: Path):
    contract = _contract(tmp_path)

    resolution = resolve_subagent_model(
        contract,
        parent_model_id="claude-code-bridge-deepseek-v4-pro",
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
            active_model_id="claude-code-bridge-deepseek-v4-pro",
            requested_model="claude-opus-4-8",
            allow_hardcoded_claude_remap=False,
        )


def test_cp3a_unknown_model_fails_closed(tmp_path: Path):
    contract = _contract(tmp_path)

    with pytest.raises(RuntimeOverlayError, match="unknown Claude Code runtime model"):
        resolve_subagent_model(contract, parent_model_id="claude-code-bridge-deepseek-v4-pro", requested_model="glm-4.6")


@pytest.mark.parametrize("requested", ["claude-code-bridge-deepseek-v4-pro"])
def test_cp3a_claude_parent_to_deepseek_subagent_requires_boundary(tmp_path: Path, requested: str):
    contract = _contract(tmp_path)

    resolution = resolve_subagent_model(contract, parent_model_id="claude-opus-4-8", requested_model=requested)

    assert resolution.provider == "deepseek"
    assert resolution.native_egress_allowed is False
    assert resolution.formal_pool_allowed is False
    assert resolution.replay_boundary == "safe_tool_result"
    assert resolution.raw_history_replay_allowed is False


def test_cp3a_cross_provider_agent_options_are_mutually_visible_but_isolated(tmp_path: Path):
    contract = _contract(tmp_path)

    options = {option.model_id: option for option in build_agent_model_options(contract) if option.model_id}

    for model_id in (
        "claude-sonnet-4-6",
        "claude-opus-4-8",
        "claude-code-bridge-gpt-5.5",
        "claude-code-bridge-gpt-5.4",
        "claude-code-bridge-gpt-5.4-mini",
        "claude-code-bridge-deepseek-v4-pro",
        "claude-code-bridge-deepseek-v4-flash",
        "claude-code-bridge-agnes-2.0-flash",
        "claude-code-bridge-glm-5.2-1m",
        "claude-code-bridge-kimi-k2.7-code",
    ):
        assert model_id in options

    claude_to_deepseek = resolve_subagent_model(
        contract,
        parent_model_id="claude-opus-4-8",
        requested_model="claude-code-bridge-deepseek-v4-pro",
    )
    assert claude_to_deepseek.provider == "deepseek"
    assert claude_to_deepseek.replay_boundary == "safe_tool_result"
    assert claude_to_deepseek.native_egress_allowed is False
    assert claude_to_deepseek.raw_history_replay_allowed is False

    deepseek_to_claude = resolve_subagent_model(
        contract,
        parent_model_id="claude-code-bridge-deepseek-v4-pro",
        requested_model="claude-opus-4-8",
        explicit_claude_opt_in=True,
    )
    assert deepseek_to_claude.provider == "claude"
    assert deepseek_to_claude.native_egress_allowed is True
    assert deepseek_to_claude.formal_pool_allowed is True
    assert deepseek_to_claude.audit_label == "explicit_claude_formal_pool_subagent"
    assert deepseek_to_claude.replay_boundary == "safe_tool_result"
    assert deepseek_to_claude.raw_history_replay_allowed is False



def test_cp3a_openai_profile_exposes_all_gpt_variants_and_background_uses_mini(tmp_path: Path):
    contract = _contract(tmp_path)
    profile = contract.provider_profiles_by_provider["openai"]

    assert profile.main_model_id == "claude-code-bridge-gpt-5.5"
    assert profile.fast_model_id == "claude-code-bridge-gpt-5.4-mini"
    options = {option.model_id for option in build_agent_model_options(contract)}
    assert {
        "claude-code-bridge-gpt-5.5",
        "claude-code-bridge-gpt-5.4",
        "claude-code-bridge-gpt-5.4-mini",
    }.issubset(options)


def test_cp3a_claude_and_bridge_parents_can_select_each_other_across_boundary(tmp_path: Path):
    contract = _contract(tmp_path)

    claude_to_flash = resolve_subagent_model(
        contract,
        parent_model_id="claude-opus-4-8",
        requested_model="claude-code-bridge-deepseek-v4-flash",
    )
    assert claude_to_flash.provider == "deepseek"
    assert claude_to_flash.replay_boundary == "safe_tool_result"
    assert claude_to_flash.raw_history_replay_allowed is False
    assert claude_to_flash.native_egress_allowed is False

    gpt_to_claude = resolve_subagent_model(
        contract,
        parent_model_id="claude-code-bridge-gpt-5.5",
        requested_model="claude-opus-4-8",
        explicit_claude_opt_in=True,
    )
    assert gpt_to_claude.provider == "claude"
    assert gpt_to_claude.replay_boundary == "safe_tool_result"
    assert gpt_to_claude.formal_pool_allowed is True
    assert gpt_to_claude.raw_history_replay_allowed is False


def test_cp3a_claude_profile_fast_uses_native_haiku_but_can_delegate_to_deepseek_flash(tmp_path: Path):
    contract = _contract(tmp_path)

    fast = resolve_background_model(contract, active_model_id="claude-opus-4-8", task="fast")
    bridge_fast = resolve_background_model(
        contract,
        active_model_id="claude-opus-4-8",
        task="fast",
        fast_preference="bridge",
    )
    haiku = resolve_subagent_model(contract, parent_model_id="claude-opus-4-8", requested_model="haiku")
    deepseek_flash = resolve_subagent_model(
        contract,
        parent_model_id="claude-opus-4-8",
        requested_model="claude-code-bridge-deepseek-v4-flash",
    )

    assert fast.resolved_model_id == "claude-haiku-4-5-20251001"
    assert fast.provider == "claude"
    assert fast.native_egress_allowed is True
    assert bridge_fast.resolved_model_id == "claude-code-bridge-deepseek-v4-flash"
    assert bridge_fast.provider == "deepseek"
    assert bridge_fast.replay_boundary == "safe_tool_result"
    assert bridge_fast.formal_pool_allowed is False
    assert bridge_fast.native_egress_allowed is False
    assert haiku.resolved_model_id == "claude-haiku-4-5-20251001"
    assert haiku.raw_history_replay_allowed is True
    assert deepseek_flash.provider == "deepseek"
    assert deepseek_flash.replay_boundary == "safe_tool_result"
    assert deepseek_flash.native_egress_allowed is False


def test_cp3a_non_claude_fast_never_resolves_to_claude_haiku(tmp_path: Path):
    contract = _contract(tmp_path)

    for active_model_id, expected_fast in (
        ("claude-code-bridge-deepseek-v4-pro", "claude-code-bridge-deepseek-v4-flash"),
        ("claude-code-bridge-gpt-5.5", "claude-code-bridge-gpt-5.4-mini"),
        ("claude-code-bridge-agnes-2.0-flash", "claude-code-bridge-agnes-2.0-flash"),
        ("claude-code-bridge-glm-5.2-1m", "claude-code-bridge-glm-5.2-1m"),
        ("claude-code-bridge-kimi-k2.7-code", "claude-code-bridge-kimi-k2.7-code"),
    ):
        resolution = resolve_background_model(contract, active_model_id=active_model_id, task="haiku")
        assert resolution.resolved_model_id == expected_fast
        assert resolution.resolved_model_id != "claude-haiku-4-5-20251001"
        assert resolution.provider != "claude"
        assert resolution.native_egress_allowed is False
        assert resolution.formal_pool_allowed is False


def test_cp3a_display_ids_resolve_to_distinct_upstream_models(tmp_path: Path):
    contract = _contract(tmp_path)

    expected = {
        "claude-code-bridge-gpt-5.5": ("openai", "openai_bridge", "claude_code_bridge_openai", "gpt-5.5"),
        "claude-code-bridge-gpt-5.4": ("openai", "openai_bridge", "claude_code_bridge_openai", "gpt-5.4"),
        "claude-code-bridge-gpt-5.4-mini": ("openai", "openai_bridge", "claude_code_bridge_openai", "gpt-5.4-mini"),
        "claude-code-bridge-deepseek-v4-pro": ("deepseek", "deepseek_bridge", "claude_code_bridge_deepseek", "deepseek-v4-pro"),
        "claude-code-bridge-deepseek-v4-flash": ("deepseek", "deepseek_bridge", "claude_code_bridge_deepseek", "deepseek-v4-flash"),
        "claude-code-bridge-agnes-2.0-flash": ("agnes", "agnes_bridge", "claude_code_bridge_agnes", "agnes-2.0-flash"),
        "claude-code-bridge-glm-5.2-1m": ("zai_glm", "zai_glm_bridge", "claude_code_bridge_zai_glm", "glm-5.2[1m]"),
        "claude-code-bridge-kimi-k2.7-code": ("kimi", "kimi_bridge", "claude_code_bridge_kimi", "kimi-k2.7-code"),
    }

    assert set(contract.proof.display_model_ids) == set(expected)
    for display_model_id, (provider, route, client_type, upstream_model_id) in expected.items():
        resolution = resolve_subagent_model(
            contract,
            parent_model_id="claude-opus-4-8",
            requested_model=display_model_id,
        )
        assert resolution.resolved_model_id == display_model_id
        assert resolution.provider == provider
        assert resolution.route == route
        assert resolution.client_type == client_type
        assert resolution.upstream_model_id == upstream_model_id
        assert resolution.upstream_model_id != display_model_id
        assert resolution.formal_pool_allowed is False
        assert resolution.native_egress_allowed is False


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
                    main_model_id="claude-code-bridge-deepseek-v4-pro",
                    fast_model_id="claude-sonnet-4-6",
                    family_aliases={"fast": "claude-sonnet-4-6"},
                ),
            ),
        )


def test_cp3a_workflow_explicit_claude_opt_in_has_audit_label(tmp_path: Path):
    contract = _contract(tmp_path)

    resolution = resolve_workflow_model_alias(
        contract,
        active_model_id="claude-code-bridge-deepseek-v4-pro",
        requested_model="claude-opus-4-8",
        explicit_claude_opt_in=True,
    )

    assert resolution.native_egress_allowed is True
    assert resolution.formal_pool_allowed is True
    assert resolution.audit_label == "explicit_claude_formal_pool_workflow"


def test_cp3a_protocol_strategy_keeps_anthropic_compatible_providers_off_openai_fallback(tmp_path: Path):
    contract = _contract(tmp_path)

    anthropic_compatible = {
        "claude-code-bridge-deepseek-v4-pro",
        "claude-code-bridge-deepseek-v4-flash",
        "claude-code-bridge-glm-5.2-1m",
        "claude-code-bridge-kimi-k2.7-code",
    }
    for model_id in anthropic_compatible:
        entry = contract.models_by_id[model_id]
        assert entry.api_formats == ("anthropic_messages",)
        assert entry.anthropic_base_url
        assert entry.openai_base_url == ""
        assert entry.route.endswith("_bridge")
        assert entry.client_type.startswith("claude_code_bridge_")
        assert entry.formal_pool_eligible is False

    for model_id in (
        "claude-code-bridge-gpt-5.5",
        "claude-code-bridge-gpt-5.4",
        "claude-code-bridge-gpt-5.4-mini",
    ):
        entry = contract.models_by_id[model_id]
        assert entry.provider == "openai"
        assert entry.api_formats == ("responses",)
        assert entry.route == "openai_bridge"
        assert entry.client_type == "claude_code_bridge_openai"
        assert entry.formal_pool_eligible is False
