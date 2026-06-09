from __future__ import annotations

from pathlib import Path

from zhumeng_agent.adapters.claude_code.doctor import (
    ClaudeCodeDoctorContext,
    evaluate_toolsearch_profile,
)
from zhumeng_agent.adapters.claude_code.profile import (
    CaptureMode,
    ClaudeCodeCapabilityProfile,
    ClaudeCodeProfile,
    ToolSearchMode,
    build_safe_env,
    apply_capability_profile,
)


def test_raw_profile_defaults_toolsearch_to_auto_not_unset(tmp_path: Path):
    profile = ClaudeCodeProfile(
        profile_id="prod",
        guard_base_url="http://127.0.0.1:43117",
        zhumeng_entry_api_key="entry-key",
        config_dir=tmp_path / "config",
        capture_mode=CaptureMode.PRODUCTION,
    )

    env = build_safe_env(profile, inherited_env={})

    assert env["ENABLE_TOOL_SEARCH"] == "auto"
    assert env["ZHUMENG_CLAUDE_TOOL_SEARCH_MODE"] == "auto"


def test_capability_profile_auto_sets_explicit_toolsearch_env_not_unset(tmp_path: Path):
    capability = ClaudeCodeCapabilityProfile(
        profile_id="native-prod",
        claude_code_version_family="1.x",
        persona_profile_id="claude-code-native-prod",
        tool_search_mode=ToolSearchMode.AUTO,
        control_plane_policy_version="cp-v1",
        server_shape_healthcheck_version="shape-v1",
    )
    base = ClaudeCodeProfile(
        profile_id="prod",
        guard_base_url="http://127.0.0.1:43117",
        zhumeng_entry_api_key="entry-key",
        config_dir=tmp_path / "config",
        capture_mode=CaptureMode.PRODUCTION,
    )

    profile = apply_capability_profile(base, capability)
    env = build_safe_env(profile, inherited_env={"ENABLE_TOOL_SEARCH": "false"})

    assert env["ENABLE_TOOL_SEARCH"] == "auto"
    assert env["ZHUMENG_CLAUDE_CAPABILITY_PROFILE_ID"] == "native-prod"
    assert env["ZHUMENG_CLAUDE_TOOL_SEARCH_MODE"] == "auto"


def test_capability_profile_true_degrades_to_auto_until_healthcheck_passes(tmp_path: Path):
    capability = ClaudeCodeCapabilityProfile(
        profile_id="native-prod",
        claude_code_version_family="1.x",
        persona_profile_id="claude-code-native-prod",
        tool_search_mode=ToolSearchMode.TRUE,
        tool_search_healthcheck_passed=False,
        control_plane_policy_version="cp-v1",
        server_shape_healthcheck_version="shape-v1",
    )
    base = ClaudeCodeProfile(
        profile_id="prod",
        guard_base_url="http://127.0.0.1:43117",
        zhumeng_entry_api_key="entry-key",
        config_dir=tmp_path / "config",
        capture_mode=CaptureMode.PRODUCTION,
    )

    profile = apply_capability_profile(base, capability)
    env = build_safe_env(profile, inherited_env={})

    assert env["ENABLE_TOOL_SEARCH"] == "auto"
    assert env["ZHUMENG_CLAUDE_TOOL_SEARCH_MODE"] == "auto"


def test_capability_profile_true_sets_env_after_healthcheck_and_doctor_pass(tmp_path: Path):
    capability = ClaudeCodeCapabilityProfile(
        profile_id="native-prod",
        claude_code_version_family="1.x",
        persona_profile_id="claude-code-native-prod",
        tool_search_mode=ToolSearchMode.TRUE,
        tool_search_healthcheck_passed=True,
        control_plane_policy_version="cp-v1",
        server_shape_healthcheck_version="shape-v1",
    )
    context = ClaudeCodeDoctorContext(
        model="claude-sonnet-4-6",
        claude_code_version="1.2.3",
        has_mcp_deferred_tools=True,
        has_pending_mcp_server=False,
        disallowed_tools=set(),
        model_supports_tool_reference=True,
    )
    base = ClaudeCodeProfile(
        profile_id="prod",
        guard_base_url="http://127.0.0.1:43117",
        zhumeng_entry_api_key="entry-key",
        config_dir=tmp_path / "config",
        capture_mode=CaptureMode.PRODUCTION,
    )

    decision = evaluate_toolsearch_profile(capability, context)
    env = build_safe_env(apply_capability_profile(base, capability, tool_search_env_value=decision.env_value), inherited_env={})

    assert env["ENABLE_TOOL_SEARCH"] == "true"
    assert env["ZHUMENG_CLAUDE_TOOL_SEARCH_MODE"] == "true"


def test_capability_profile_true_without_doctor_decision_stays_auto(tmp_path: Path):
    capability = ClaudeCodeCapabilityProfile(
        profile_id="native-prod",
        claude_code_version_family="1.x",
        persona_profile_id="claude-code-native-prod",
        tool_search_mode=ToolSearchMode.TRUE,
        tool_search_healthcheck_passed=True,
        control_plane_policy_version="cp-v1",
        server_shape_healthcheck_version="shape-v1",
    )
    base = ClaudeCodeProfile(
        profile_id="prod",
        guard_base_url="http://127.0.0.1:43117",
        zhumeng_entry_api_key="entry-key",
        config_dir=tmp_path / "config",
        capture_mode=CaptureMode.PRODUCTION,
    )

    env = build_safe_env(apply_capability_profile(base, capability), inherited_env={})

    assert env["ENABLE_TOOL_SEARCH"] == "auto"


def test_toolsearch_true_requires_healthcheck_and_deferred_tools(tmp_path: Path):
    capability = ClaudeCodeCapabilityProfile(
        profile_id="native-prod",
        claude_code_version_family="1.x",
        persona_profile_id="claude-code-native-prod",
        tool_search_mode=ToolSearchMode.TRUE,
        tool_search_healthcheck_passed=False,
        control_plane_policy_version="cp-v1",
        server_shape_healthcheck_version="shape-v1",
    )
    context = ClaudeCodeDoctorContext(
        model="claude-sonnet-4-6",
        claude_code_version="1.2.3",
        has_mcp_deferred_tools=True,
        has_pending_mcp_server=False,
        disallowed_tools=set(),
        model_supports_tool_reference=True,
    )

    decision = evaluate_toolsearch_profile(capability, context)

    assert decision.env_value == "auto"
    assert decision.degraded is True
    assert decision.status == "toolsearch_degraded"
    assert "healthcheck" in decision.reasons


def test_toolsearch_true_allowed_after_healthcheck_with_mcp_deferred_tools():
    capability = ClaudeCodeCapabilityProfile(
        profile_id="native-prod",
        claude_code_version_family="1.x",
        persona_profile_id="claude-code-native-prod",
        tool_search_mode=ToolSearchMode.TRUE,
        tool_search_healthcheck_passed=True,
        control_plane_policy_version="cp-v1",
        server_shape_healthcheck_version="shape-v1",
    )
    context = ClaudeCodeDoctorContext(
        model="claude-opus-4-7",
        claude_code_version="1.2.3",
        has_mcp_deferred_tools=True,
        has_pending_mcp_server=False,
        disallowed_tools=set(),
        model_supports_tool_reference=True,
    )

    decision = evaluate_toolsearch_profile(capability, context)

    assert decision.env_value == "true"
    assert decision.degraded is False
    assert decision.status == "ready"


def test_toolsearch_falls_back_for_haiku_or_disallowed_toolsearch():
    capability = ClaudeCodeCapabilityProfile(
        profile_id="native-prod",
        claude_code_version_family="1.x",
        persona_profile_id="claude-code-native-prod",
        tool_search_mode=ToolSearchMode.TRUE,
        tool_search_healthcheck_passed=True,
        control_plane_policy_version="cp-v1",
        server_shape_healthcheck_version="shape-v1",
    )
    context = ClaudeCodeDoctorContext(
        model="claude-haiku-4-5-20251001",
        claude_code_version="1.2.3",
        has_mcp_deferred_tools=True,
        has_pending_mcp_server=False,
        disallowed_tools={"ToolSearchTool"},
        model_supports_tool_reference=False,
    )

    decision = evaluate_toolsearch_profile(capability, context)

    assert decision.env_value == "auto"
    assert decision.degraded is True
    assert decision.status == "toolsearch_degraded"
    assert "model_unsupported" in decision.reasons
    assert "toolsearch_disallowed" in decision.reasons


def test_standard_toolsearch_mode_disables_native_toolsearch():
    capability = ClaudeCodeCapabilityProfile(
        profile_id="native-standard",
        claude_code_version_family="1.x",
        persona_profile_id="claude-code-native-standard",
        tool_search_mode=ToolSearchMode.STANDARD,
        tool_search_healthcheck_passed=True,
        control_plane_policy_version="cp-v1",
        server_shape_healthcheck_version="shape-v1",
    )
    context = ClaudeCodeDoctorContext(
        model="claude-sonnet-4-6",
        claude_code_version="1.2.3",
        has_mcp_deferred_tools=True,
        has_pending_mcp_server=True,
        disallowed_tools=set(),
        model_supports_tool_reference=True,
    )

    decision = evaluate_toolsearch_profile(capability, context)

    assert decision.env_value == "false"
    assert decision.status == "ready"
    assert decision.degraded is False


def test_doctor_degrades_profile_version_family_mismatch():
    capability = ClaudeCodeCapabilityProfile(
        profile_id="native-prod",
        claude_code_version_family="2.x",
        persona_profile_id="claude-code-native-prod",
        tool_search_mode=ToolSearchMode.TRUE,
        tool_search_healthcheck_passed=True,
        control_plane_policy_version="cp-v1",
        server_shape_healthcheck_version="shape-v1",
    )
    context = ClaudeCodeDoctorContext(
        model="claude-sonnet-4-6",
        claude_code_version="1.2.3",
        has_mcp_deferred_tools=True,
        has_pending_mcp_server=False,
        disallowed_tools=set(),
        model_supports_tool_reference=True,
    )

    decision = evaluate_toolsearch_profile(capability, context)

    assert decision.env_value == "auto"
    assert decision.status == "profile_mismatch"
    assert "version_family_mismatch" in decision.reasons
