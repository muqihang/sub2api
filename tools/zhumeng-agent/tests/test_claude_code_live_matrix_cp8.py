from __future__ import annotations

import hashlib
import json
from pathlib import Path

import pytest

from zhumeng_agent.adapters.claude_code.live_matrix import (
    CP8LiveMatrixError,
    REQUIRED_CP8_SCENARIOS,
    verify_cp8_live_matrix,
)

FIXTURE_DIR = Path(__file__).parent / "fixtures" / "claude_code_cp8"


def _fixture(name: str) -> dict[str, object]:
    return json.loads((FIXTURE_DIR / name).read_text(encoding="utf-8"))


def test_cp8_live_matrix_fixture_covers_all_required_scenarios_without_native_contamination():
    fixture = _fixture("live_matrix_pass.json")
    result = verify_cp8_live_matrix(fixture, evidence_root=FIXTURE_DIR)

    assert result.status == "pass"
    assert result.checkpoint == "CP8"
    assert set(result.scenarios) == set(REQUIRED_CP8_SCENARIOS)
    assert result.native_egress_by_route == {"claude_code_native": 2}
    assert result.bridge_egress_by_route == {
        "openai_bridge": 2,
        "deepseek_bridge": 3,
    }
    assert result.bridge_egress_by_client_type == {
        "claude_code_bridge_openai": 2,
        "claude_code_bridge_deepseek": 3,
    }
    assert result.release_gate == "manual_external_live_required"
    assert result.to_dict()["summary"]["strict_live_ready"] is False
    assert result.to_dict()["summary"]["official_docs_checked"] is True
    assert result.to_dict()["summary"]["artifact_evidence_verified"] is True


def test_cp8_live_matrix_strict_live_requires_real_provider_evidence():
    result = verify_cp8_live_matrix(_fixture("live_matrix_pass.json"), strict_live=True, evidence_root=FIXTURE_DIR)

    assert result.status == "fail"
    assert result.release_gate == "blocked_missing_external_live"
    assert "claude_native" in result.failed


def test_cp8_live_matrix_fails_closed_for_missing_scenario_or_bridge_native_pollution():
    payload = _fixture("live_matrix_pass.json")
    payload["scenarios"].pop("netwatch_bypass")
    result = verify_cp8_live_matrix(payload, evidence_root=FIXTURE_DIR)
    assert result.status == "fail"
    assert result.missing == ("netwatch_bypass",)

    payload = _fixture("live_matrix_pass.json")
    payload["scenarios"]["deepseek_bridge"]["native_egress_count"] = 1
    payload["scenarios"]["deepseek_bridge"]["formal_pool_allowed"] = True
    result = verify_cp8_live_matrix(payload, evidence_root=FIXTURE_DIR)
    assert result.status == "fail"
    assert "deepseek_bridge" in result.failed
    assert any("native contamination" in issue for issue in result.scenario_results["deepseek_bridge"].issues)


def test_cp8_official_docs_snapshot_rejects_stale_glm46_or_unprobed_anthropic_claims():
    payload = _fixture("live_matrix_pass.json")
    docs = payload["official_docs_snapshot"]
    docs["observations"]["zai_glm"]["latest_coding_model"] = "glm-4.6"
    result = verify_cp8_live_matrix(payload, evidence_root=FIXTURE_DIR)
    assert result.status == "fail"
    assert "official_docs" in result.failed

    payload = _fixture("live_matrix_pass.json")
    payload["scenarios"]["deepseek_bridge"]["preferred_claude_code_protocol"] = "openai_chat_completions"
    payload["scenarios"]["deepseek_bridge"]["fallback_reason"] = ""
    result = verify_cp8_live_matrix(payload, evidence_root=FIXTURE_DIR)
    assert result.status == "fail"
    assert "deepseek_bridge" in result.failed
    assert any("DeepSeek must default to anthropic_messages" in issue for issue in result.scenario_results["deepseek_bridge"].issues)



def test_cp8_official_docs_snapshot_rejects_kimi_endpoint_drift():
    payload = _fixture("live_matrix_pass.json")
    docs = payload["official_docs_snapshot"]
    assert docs["observations"]["kimi"]["anthropic_base_url"] == "https://api.moonshot.ai/anthropic"
    assert docs["observations"]["kimi"]["openai_base_url"] == "https://api.moonshot.ai/v1"

    docs["observations"]["kimi"]["anthropic_base_url"] = "https://api.moonshot.cn/anthropic"
    docs["observations"]["kimi"]["openai_base_url"] = "https://api.moonshot.cn/v1"

    result = verify_cp8_live_matrix(payload, evidence_root=FIXTURE_DIR)

    assert result.status == "fail"
    assert "official_docs" in result.failed




def test_cp8_official_docs_snapshot_requires_deepseek_1m_and_kimi_cache_fields():
    payload = _fixture("live_matrix_pass.json")
    docs = payload["official_docs_snapshot"]
    assert "deepseek-v4-pro[1m]" in docs["observations"]["deepseek"]["models"]
    assert docs["observations"]["deepseek"]["context_windows"]["deepseek-v4-pro[1m]"] == 1_000_000
    assert docs["observations"]["kimi"]["prompt_cache_key"] is True
    assert docs["observations"]["kimi"]["cache_usage_field"] == "usage.cached_tokens"

    docs["observations"]["deepseek"]["models"].remove("deepseek-v4-pro[1m]")
    docs["observations"]["kimi"].pop("prompt_cache_key")
    result = verify_cp8_live_matrix(payload, evidence_root=FIXTURE_DIR)
    assert result.status == "fail"
    assert "official_docs" in result.failed

def test_cp8_live_matrix_requires_artifact_hashes_and_rejects_sensitive_artifacts():
    payload = _fixture("live_matrix_pass.json")
    payload["scenarios"]["gpt_bridge"]["artifact_refs"][0]["sha256"] = "sha256:" + "0" * 64
    result = verify_cp8_live_matrix(payload, evidence_root=FIXTURE_DIR)
    assert result.status == "fail"
    assert "gpt_bridge" in result.failed
    assert any("artifact hash mismatch" in issue for issue in result.scenario_results["gpt_bridge"].issues)

    payload = _fixture("live_matrix_pass.json")
    sensitive = FIXTURE_DIR / "sensitive_artifact.jsonl"
    payload["scenarios"]["deepseek_bridge"]["artifact_refs"][0]["path"] = "sensitive_artifact.jsonl"
    payload["scenarios"]["deepseek_bridge"]["artifact_refs"][0]["sha256"] = "sha256:" + hashlib.sha256(sensitive.read_bytes()).hexdigest()
    result = verify_cp8_live_matrix(payload, evidence_root=FIXTURE_DIR)
    assert result.status == "fail"
    assert "deepseek_bridge" in result.failed
    assert any("artifact contains sensitive marker" in issue for issue in result.scenario_results["deepseek_bridge"].issues)


def test_cp8_live_matrix_rejects_unknown_schema_version():
    payload = _fixture("live_matrix_pass.json")
    payload["schema_version"] = "cp7"
    with pytest.raises(CP8LiveMatrixError, match="schema_version"):
        verify_cp8_live_matrix(payload, evidence_root=FIXTURE_DIR)


def test_cp8_strict_live_cannot_be_forged_by_flipping_fixture_live_flags():
    payload = _fixture("live_matrix_pass.json")
    for scenario in payload["scenarios"].values():
        scenario["live_provider_verified"] = True

    result = verify_cp8_live_matrix(payload, strict_live=True, evidence_root=FIXTURE_DIR)

    assert result.status == "fail"
    assert result.release_gate == "blocked_missing_external_live"
    assert "live_provenance" in result.failed


def test_cp8_live_matrix_fails_closed_for_non_object_scenarios():
    payload = _fixture("live_matrix_pass.json")
    payload["scenarios"] = {name: "pass" for name in REQUIRED_CP8_SCENARIOS}

    result = verify_cp8_live_matrix(payload, strict_live=True, evidence_root=FIXTURE_DIR)

    assert result.status == "fail"
    assert set(REQUIRED_CP8_SCENARIOS).issubset(set(result.failed))
    assert "live_provenance" in result.failed
    assert result.release_gate == "blocked_missing_external_live"



def test_cp8_deepseek_bridge_accepts_claude_code_1m_pro_as_pro_coverage():
    payload = _fixture("live_matrix_pass.json")
    payload["scenarios"]["deepseek_bridge"]["models"] = ["deepseek-v4-pro[1m]", "deepseek-v4-flash"]

    result = verify_cp8_live_matrix(payload, evidence_root=FIXTURE_DIR)

    assert result.status == "pass"
    assert "deepseek_bridge" not in result.failed

def test_cp8_live_matrix_rejects_model_family_semantic_drift():
    payload = _fixture("live_matrix_pass.json")
    payload["scenarios"]["claude_native"]["models"] = ["claude-sonnet-4-6"]
    result = verify_cp8_live_matrix(payload, evidence_root=FIXTURE_DIR)
    assert result.status == "fail"
    assert "claude_native" in result.failed
    assert any("Opus" in issue for issue in result.scenario_results["claude_native"].issues)

    payload = _fixture("live_matrix_pass.json")
    payload["scenarios"]["gpt_bridge"]["models"] = ["deepseek-v4-pro"]
    result = verify_cp8_live_matrix(payload, evidence_root=FIXTURE_DIR)
    assert result.status == "fail"
    assert "gpt_bridge" in result.failed
    assert any("GPT" in issue for issue in result.scenario_results["gpt_bridge"].issues)

    payload = _fixture("live_matrix_pass.json")
    payload["scenarios"]["deepseek_bridge"]["models"] = ["deepseek-v4-pro[1m]"]
    result = verify_cp8_live_matrix(payload, evidence_root=FIXTURE_DIR)
    assert result.status == "fail"
    assert "deepseek_bridge" in result.failed
    assert any("Pro and Flash" in issue for issue in result.scenario_results["deepseek_bridge"].issues)

def test_cp8_live_matrix_public_api_is_exported():
    from zhumeng_agent.adapters.claude_code import (  # noqa: PLC0415
        CP8LiveMatrixError as ExportedError,
        REQUIRED_CP8_SCENARIOS as ExportedScenarios,
        verify_cp8_live_matrix as exported_verify,
    )

    assert ExportedError is CP8LiveMatrixError
    assert ExportedScenarios is REQUIRED_CP8_SCENARIOS
    assert exported_verify is verify_cp8_live_matrix
