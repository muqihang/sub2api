from __future__ import annotations

import json
import hashlib
from pathlib import Path

import pytest

from zhumeng_agent.adapters.claude_code.provider_probe import (
    ProviderProtocolProbeError,
    build_cp6_provider_probe_catalog,
    select_cp6_bridge_transport,
)

FIXTURE_DIR = Path(__file__).parent / "fixtures" / "claude_code_cp6"


def _fixture(name: str) -> dict[str, object]:
    return json.loads((FIXTURE_DIR / name).read_text(encoding="utf-8"))


def test_cp6_provider_docs_snapshot_uses_current_official_model_facts_without_glm46_latest():
    snapshot = _fixture("provider_docs_snapshot.json")

    assert snapshot["captured_at"] == "2026-06-16"
    assert snapshot["observations"]["deepseek"]["models"] == ["deepseek-v4-flash", "deepseek-v4-pro", "deepseek-v4-pro[1m]"]
    assert snapshot["observations"]["deepseek"]["context_windows"]["deepseek-v4-pro[1m]"] == 1_000_000
    assert snapshot["observations"]["deepseek"]["anthropic_base_url"] == "https://api.deepseek.com/anthropic"
    assert snapshot["observations"]["deepseek"]["openai_base_url"] == "https://api.deepseek.com"
    assert snapshot["observations"]["deepseek"]["kv_cache"]["hit_rule"] == "full_prefix_cache_unit_match"
    assert snapshot["observations"]["zai_glm"]["latest_coding_model"] == "glm-5.2"
    assert "glm-4.6" not in snapshot["observations"]["zai_glm"]["claude_code_display_models"]
    assert snapshot["observations"]["kimi"]["coding_models"] == ["kimi-k2.7-code", "kimi-k2.7-code-highspeed"]
    assert snapshot["observations"]["kimi"]["prompt_cache_key"] is True
    assert snapshot["observations"]["kimi"]["cache_usage_field"] == "usage.cached_tokens"
    assert snapshot["observations"]["openai"]["recommended_model"] == "gpt-5.5"
    for provider in snapshot["observations"].values():
        assert provider["live_runtime_verified"] is False


def test_cp6_deepseek_defaults_to_anthropic_messages_when_all_fixtures_pass():
    catalog = build_cp6_provider_probe_catalog(_fixture("provider_probe_matrix_pass.json"))

    decision = select_cp6_bridge_transport(catalog, provider="deepseek", model_id="deepseek-v4-pro[1m]")

    assert decision.provider == "deepseek"
    assert decision.model_id == "deepseek-v4-pro[1m]"
    assert decision.selected_protocol == "anthropic_messages"
    assert decision.base_url == "https://api.deepseek.com/anthropic"
    assert decision.fallback_protocol == "openai_chat_completions"
    assert decision.fallback_reason == ""
    assert decision.capabilities["tools"] is True
    assert decision.capabilities["cache"] is True
    assert decision.capabilities["error_passthrough"] is True


@pytest.mark.parametrize("failed_capability", ["tools", "sse", "reasoning", "cache", "error_passthrough"])
def test_cp6_deepseek_falls_back_to_openai_compatible_when_required_fixture_fails(failed_capability: str):
    payload = _fixture("provider_probe_matrix_pass.json")
    payload["providers"]["deepseek"]["anthropic_messages"]["capabilities"][failed_capability] = False
    catalog = build_cp6_provider_probe_catalog(payload)

    decision = select_cp6_bridge_transport(catalog, provider="deepseek", model_id="deepseek-v4-pro")

    assert decision.selected_protocol == "openai_chat_completions"
    assert decision.base_url == "https://api.deepseek.com"
    assert decision.fallback_reason == f"anthropic_{failed_capability}_fixture_failed"


def test_cp6_provider_probe_fails_closed_for_unverified_unknown_or_live_claims():
    payload = _fixture("provider_probe_matrix_pass.json")
    payload["providers"]["deepseek"]["anthropic_messages"]["live_runtime_verified"] = True
    with pytest.raises(ProviderProtocolProbeError, match="live provider probes are not allowed"):
        build_cp6_provider_probe_catalog(payload)

    payload = _fixture("provider_probe_matrix_pass.json")
    payload["providers"]["deepseek"]["anthropic_messages"]["capabilities_verified"] = False
    catalog = build_cp6_provider_probe_catalog(payload)
    with pytest.raises(ProviderProtocolProbeError, match="capabilities are not verified"):
        select_cp6_bridge_transport(catalog, provider="deepseek", model_id="deepseek-v4-pro")

    with pytest.raises(ProviderProtocolProbeError, match="unknown provider"):
        select_cp6_bridge_transport(catalog, provider="unknown", model_id="x")


def test_cp6_codex_gateway_no_regression_evidence_is_bound_to_existing_fixtures():
    evidence = _fixture("codex_gateway_no_regression_cp6.json")
    repo_root = Path(__file__).resolve().parents[3]

    assert evidence["schema_version"] == "cp6-codex-gateway-no-regression-v1"
    assert evidence["mode"] == "fixture_only_no_live_network"
    assert evidence["codex_gateway_no_regression"] is True
    for relpath in evidence["required_fixture_paths"]:
        assert (repo_root / relpath).exists(), relpath
        assert evidence["fixture_sha256"][relpath] == "sha256:" + hashlib.sha256((repo_root / relpath).read_bytes()).hexdigest()
    for relpath, test_names in evidence["required_go_tests"].items():
        source = (repo_root / relpath).read_text(encoding="utf-8")
        for test_name in test_names:
            assert f"func {test_name}(" in source

    deepseek = evidence["providers"]["deepseek"]
    assert deepseek["preferred_bridge_protocol"] == "anthropic_messages"
    assert deepseek["cache_usage_fields"] == ["cache_read_input_tokens", "cache_creation_input_tokens"]
    assert deepseek["reasoning_cleaning"] == "foreign_reasoning_never_native_replay"
    assert {"function_tool_call_stream", "tool_search_call_output_request"}.issubset(set(deepseek["golden_fixtures"]))

    openai = evidence["providers"]["openai"]
    assert openai["bridge_protocol"] == "responses"
    assert openai["cache_usage_field"] == "usage.prompt_tokens_details.cached_tokens"
    assert openai["native_formal_pool_egress"] == 0

    agnes = evidence["providers"]["agnes"]
    assert agnes["beta_path_preserved"] is True
    assert agnes["computer_use_semantic_compression"] is True
    assert agnes["native_formal_pool_egress"] == 0



def test_cp6_provider_probe_public_api_is_exported():
    from zhumeng_agent.adapters.claude_code import (  # noqa: PLC0415
        BridgeTransportDecision as ExportedDecision,
        ProviderProbeCatalog as ExportedCatalog,
        ProviderProtocolProbeError as ExportedError,
        build_cp6_provider_probe_catalog as exported_build,
        select_cp6_bridge_transport as exported_select,
    )
    from zhumeng_agent.adapters.claude_code.provider_probe import (  # noqa: PLC0415
        BridgeTransportDecision,
        ProviderProbeCatalog,
        ProviderProtocolProbeError,
        build_cp6_provider_probe_catalog,
        select_cp6_bridge_transport,
    )

    assert ExportedDecision is BridgeTransportDecision
    assert ExportedCatalog is ProviderProbeCatalog
    assert ExportedError is ProviderProtocolProbeError
    assert exported_build is build_cp6_provider_probe_catalog
    assert exported_select is select_cp6_bridge_transport
