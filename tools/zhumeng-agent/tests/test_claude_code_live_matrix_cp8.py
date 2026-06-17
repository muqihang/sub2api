from __future__ import annotations

import hashlib
import json
import urllib.request
from pathlib import Path

import pytest

from zhumeng_agent.adapters.claude_code.live_matrix import (
    CP8LiveMatrixError,
    REQUIRED_CP8_SCENARIOS,
    assemble_cp8_external_live_matrix_evidence,
    verify_cp8_live_matrix,
)

FIXTURE_DIR = Path(__file__).parent / "fixtures" / "claude_code_cp8"


def _fixture(name: str) -> dict[str, object]:
    return json.loads((FIXTURE_DIR / name).read_text(encoding="utf-8"))




def _scenario_provider(name: str) -> str:
    return {
        "claude_native": "claude",
        "gpt_bridge": "openai",
        "deepseek_bridge": "deepseek",
        "subagent": "openai",
        "claude_deepseek_subagent_claude": "deepseek",
        "manual_provider_switch": "deepseek",
        "toolsearch_mcp": "deepseek",
        "workflow": "deepseek",
        "long_context": "deepseek",
        "interruption": "openai",
        "cache_account_audit": "deepseek",
        "netwatch_bypass": "claude",
    }[name]


def _provider_endpoint(provider: str) -> str:
    return {
        "claude": "https://api.anthropic.com/v1/messages",
        "openai": "https://api.openai.com/v1/responses",
        "deepseek": "https://api.deepseek.com/anthropic/v1/messages",
    }[provider]


def _provider_live_artifact(root: Path, provider: str) -> tuple[str, str, str]:
    candidates = [
        root / "artifacts" / f"{provider}_live_provenance.json",
        root / "artifacts" / f"{provider}_live.json",
        root / "artifacts" / f"provider_{provider}.json",
    ]
    for path in candidates:
        if not path.exists():
            continue
        payload = json.loads(path.read_text(encoding="utf-8"))
        if payload.get("provider") == provider and payload.get("schema_version") == "cp8-live-provider-provenance-v1":
            return (
                "artifacts/" + path.name,
                str(payload.get("endpoint") or _provider_endpoint(provider)),
                str(payload.get("upstream_request_id") or f"req_{provider}_live"),
            )
    return (f"artifacts/{provider}_live.json", _provider_endpoint(provider), f"req_{provider}_live")


def _add_strict_live_scenario_artifacts(payload: dict[str, object], root: Path, run_id: str) -> None:
    artifacts_dir = root / "artifacts"
    artifacts_dir.mkdir(exist_ok=True)
    for name, scenario in payload["scenarios"].items():
        provider = _scenario_provider(name)
        provider_ref, endpoint, request_id = _provider_live_artifact(root, provider)
        artifact = artifacts_dir / f"scenario_{name}.json"
        artifact.write_text(
            json.dumps(
                {
                    "schema_version": "cp8-live-scenario-evidence-v1",
                    "checkpoint": "CP8",
                    "run_id": run_id,
                    "scenario": name,
                    "status": "pass",
                    "live_provider_verified": True,
                    "raw_sensitive_stored": False,
                    "loopback": False,
                    "route": scenario.get("route", ""),
                    "client_type": scenario.get("client_type", ""),
                    "provider": provider,
                    "endpoint": endpoint,
                    "upstream_request_id": request_id,
                    "provider_provenance_refs": [provider_ref],
                },
                ensure_ascii=True,
                sort_keys=True,
            ),
            encoding="utf-8",
        )
        scenario["artifact_refs"] = [
            {
                "path": f"artifacts/{artifact.name}",
                "sha256": "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest(),
                "sensitive_scan_clean": True,
            }
        ]



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


def test_cp8_strict_live_requires_external_provenance_artifacts_and_non_loopback_endpoints():
    payload = _fixture("live_matrix_pass.json")
    payload["mode"] = "external_provider_live_matrix"
    for scenario in payload["scenarios"].values():
        scenario["live_provider_verified"] = True
    payload["live_provenance"] = {
        "credential_backed": True,
        "loopback_only": False,
        "providers": {
            "claude": {
                "credential_scope": "formal_pool",
                "live_provider_verified": True,
                "endpoint": "https://api.anthropic.com/v1/messages",
            },
            "openai": {
                "credential_scope": "bridge_pool",
                "live_provider_verified": True,
                "endpoint": "https://api.openai.com/v1/responses",
            },
            "deepseek": {
                "credential_scope": "bridge_pool",
                "live_provider_verified": True,
                "endpoint": "https://api.deepseek.com/anthropic/v1/messages",
            },
        },
    }

    result = verify_cp8_live_matrix(payload, strict_live=True, evidence_root=FIXTURE_DIR)

    assert result.status == "fail"
    assert "live_provenance" in result.failed
    assert result.release_gate == "blocked_missing_external_live"

    for provider in payload["live_provenance"]["providers"].values():
        provider["artifact_refs"] = [
            {
                "path": "artifacts/netwatch_summary.json",
                "sha256": "sha256:" + hashlib.sha256((FIXTURE_DIR / "artifacts" / "netwatch_summary.json").read_bytes()).hexdigest(),
                "sensitive_scan_clean": True,
            }
        ]
    payload["live_provenance"]["providers"]["deepseek"]["endpoint"] = "http://127.0.0.1/deepseek/anthropic/v1/messages"

    result = verify_cp8_live_matrix(payload, strict_live=True, evidence_root=FIXTURE_DIR)

    assert result.status == "fail"
    assert "live_provenance" in result.failed


def test_cp8_strict_live_rejects_reused_fixture_artifact_as_provider_live_proof():
    payload = _fixture("live_matrix_pass.json")
    payload["mode"] = "external_provider_live_matrix"
    for scenario in payload["scenarios"].values():
        scenario["live_provider_verified"] = True
    reused_artifact = {
        "path": "artifacts/netwatch_summary.json",
        "sha256": "sha256:" + hashlib.sha256((FIXTURE_DIR / "artifacts" / "netwatch_summary.json").read_bytes()).hexdigest(),
        "sensitive_scan_clean": True,
    }
    payload["live_provenance"] = {
        "credential_backed": True,
        "loopback_only": False,
        "run_id": "cp8-forgery-attempt",
        "providers": {
            "claude": {
                "credential_scope": "formal_pool",
                "live_provider_verified": True,
                "endpoint": "https://api.anthropic.com/v1/messages",
                "artifact_refs": [dict(reused_artifact)],
            },
            "openai": {
                "credential_scope": "bridge_pool",
                "live_provider_verified": True,
                "endpoint": "https://api.openai.com/v1/responses",
                "artifact_refs": [dict(reused_artifact)],
            },
            "deepseek": {
                "credential_scope": "bridge_pool",
                "live_provider_verified": True,
                "endpoint": "https://api.deepseek.com/anthropic/v1/messages",
                "artifact_refs": [dict(reused_artifact)],
            },
        },
    }

    result = verify_cp8_live_matrix(payload, strict_live=True, evidence_root=FIXTURE_DIR)

    assert result.status == "fail"
    assert result.release_gate == "blocked_missing_external_live"
    assert "live_provenance" in result.failed




def test_cp8_strict_live_rejects_provider_provenance_reused_as_scenario_evidence(tmp_path: Path):
    payload = _fixture("live_matrix_pass.json")
    payload["mode"] = "external_provider_live_matrix"
    run_id = "cp8-provider-only-is-not-scenario-proof"
    payload["live_provenance"] = {
        "credential_backed": True,
        "loopback_only": False,
        "run_id": run_id,
        "providers": {},
    }
    providers = {
        "claude": ("formal_pool", "https://api.anthropic.com/v1/messages"),
        "openai": ("bridge_pool", "https://api.openai.com/v1/responses"),
        "deepseek": ("bridge_pool", "https://api.deepseek.com/anthropic/v1/messages"),
    }
    artifacts_dir = tmp_path / "artifacts"
    artifacts_dir.mkdir()
    for provider, (scope, endpoint) in providers.items():
        proof = {
            "schema_version": "cp8-live-provider-provenance-v1",
            "checkpoint": "CP8",
            "run_id": run_id,
            "provider": provider,
            "credential_scope": scope,
            "endpoint": endpoint,
            "host": endpoint.split("/")[2],
            "external_live_verified": True,
            "loopback": False,
            "response_status": 200,
            "upstream_request_id": f"req_{provider}_live",
        }
        artifact = artifacts_dir / f"{provider}_live.json"
        artifact.write_text(json.dumps(proof, ensure_ascii=True, sort_keys=True), encoding="utf-8")
        payload["live_provenance"]["providers"][provider] = {
            "credential_scope": scope,
            "live_provider_verified": True,
            "endpoint": endpoint,
            "artifact_refs": [
                {
                    "path": f"artifacts/{provider}_live.json",
                    "sha256": "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest(),
                    "sensitive_scan_clean": True,
                }
            ],
        }
    provider_only_ref = dict(payload["live_provenance"]["providers"]["claude"]["artifact_refs"][0])
    for scenario in payload["scenarios"].values():
        scenario["live_provider_verified"] = True
        scenario["artifact_refs"] = [provider_only_ref]

    result = verify_cp8_live_matrix(payload, strict_live=True, evidence_root=tmp_path)

    assert result.status == "fail"
    assert "claude_native" in result.failed
    assert any("external live scenario artifact" in issue for issue in result.scenario_results["claude_native"].issues)


def test_cp8_strict_live_accepts_provider_bound_external_artifacts(tmp_path: Path):
    payload = _fixture("live_matrix_pass.json")
    payload["mode"] = "external_provider_live_matrix"
    run_id = "cp8-live-run-001"
    payload["live_provenance"] = {
        "credential_backed": True,
        "loopback_only": False,
        "run_id": run_id,
        "providers": {},
    }
    providers = {
        "claude": ("formal_pool", "https://api.anthropic.com/v1/messages"),
        "openai": ("bridge_pool", "https://api.openai.com/v1/responses"),
        "deepseek": ("bridge_pool", "https://api.deepseek.com/anthropic/v1/messages"),
    }
    artifacts_dir = tmp_path / "artifacts"
    artifacts_dir.mkdir()
    for provider, (scope, endpoint) in providers.items():
        proof = {
            "schema_version": "cp8-live-provider-provenance-v1",
            "checkpoint": "CP8",
            "run_id": run_id,
            "provider": provider,
            "credential_scope": scope,
            "endpoint": endpoint,
            "host": endpoint.split("/")[2],
            "external_live_verified": True,
            "loopback": False,
            "response_status": 200,
            "upstream_request_id": f"req_{provider}_live",
        }
        artifact = artifacts_dir / f"{provider}_live.json"
        artifact.write_text(json.dumps(proof, ensure_ascii=True, sort_keys=True), encoding="utf-8")
        payload["live_provenance"]["providers"][provider] = {
            "credential_scope": scope,
            "live_provider_verified": True,
            "endpoint": endpoint,
            "artifact_refs": [
                {
                    "path": f"artifacts/{provider}_live.json",
                    "sha256": "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest(),
                    "sensitive_scan_clean": True,
                }
            ],
        }
    for scenario in payload["scenarios"].values():
        scenario["live_provider_verified"] = True
    _add_strict_live_scenario_artifacts(payload, tmp_path, run_id)

    result = verify_cp8_live_matrix(payload, strict_live=True, evidence_root=tmp_path)

    assert result.status == "pass"
    assert result.strict_live_ready is True
    assert result.release_gate == "external_live_passed"


def test_cp8_strict_live_rejects_wrong_endpoint_even_when_provider_and_scenario_match(tmp_path: Path):
    payload = _fixture("live_matrix_pass.json")
    payload["mode"] = "external_provider_live_matrix"
    run_id = "cp8-live-wrong-openai-path"
    payload["live_provenance"] = {
        "credential_backed": True,
        "loopback_only": False,
        "run_id": run_id,
        "providers": {},
    }
    providers = {
        "claude": ("formal_pool", "https://api.anthropic.com/v1/messages"),
        "openai": ("bridge_pool", "https://api.openai.com/v1/chat/completions"),
        "deepseek": ("bridge_pool", "https://api.deepseek.com/anthropic/v1/messages"),
    }
    artifacts_dir = tmp_path / "artifacts"
    artifacts_dir.mkdir()
    for provider, (scope, endpoint) in providers.items():
        proof = {
            "schema_version": "cp8-live-provider-provenance-v1",
            "checkpoint": "CP8",
            "run_id": run_id,
            "provider": provider,
            "credential_scope": scope,
            "endpoint": endpoint,
            "host": endpoint.split("/")[2],
            "external_live_verified": True,
            "loopback": False,
            "response_status": 200,
            "upstream_request_id": f"req_{provider}_live",
        }
        artifact = artifacts_dir / f"{provider}_live.json"
        artifact.write_text(json.dumps(proof, ensure_ascii=True, sort_keys=True), encoding="utf-8")
        payload["live_provenance"]["providers"][provider] = {
            "credential_scope": scope,
            "live_provider_verified": True,
            "endpoint": endpoint,
            "artifact_refs": [
                {
                    "path": f"artifacts/{provider}_live.json",
                    "sha256": "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest(),
                    "sensitive_scan_clean": True,
                }
            ],
        }
    for scenario in payload["scenarios"].values():
        scenario["live_provider_verified"] = True
    _add_strict_live_scenario_artifacts(payload, tmp_path, run_id)

    result = verify_cp8_live_matrix(payload, strict_live=True, evidence_root=tmp_path)

    assert result.status == "fail"
    assert "live_provenance" in result.failed
    assert "gpt_bridge" in result.failed


def test_cp8_strict_live_rejects_minimal_or_mismatched_scenario_artifacts(tmp_path: Path):
    payload = _fixture("live_matrix_pass.json")
    payload["mode"] = "external_provider_live_matrix"
    run_id = "cp8-live-run-forgery"
    payload["live_provenance"] = {"credential_backed": True, "loopback_only": False, "run_id": run_id, "providers": {}}
    providers = {
        "claude": ("formal_pool", "https://api.anthropic.com/v1/messages"),
        "openai": ("bridge_pool", "https://api.openai.com/v1/responses"),
        "deepseek": ("bridge_pool", "https://api.deepseek.com/anthropic/v1/messages"),
    }
    artifacts_dir = tmp_path / "artifacts"
    artifacts_dir.mkdir()
    for provider, (scope, endpoint) in providers.items():
        proof = {
            "schema_version": "cp8-live-provider-provenance-v1",
            "checkpoint": "CP8",
            "run_id": run_id,
            "provider": provider,
            "credential_scope": scope,
            "endpoint": endpoint,
            "host": endpoint.split("/")[2],
            "external_live_verified": True,
            "loopback": False,
            "response_status": 200,
            "upstream_request_id": f"req_{provider}_live",
        }
        artifact = artifacts_dir / f"{provider}_live.json"
        artifact.write_text(json.dumps(proof, ensure_ascii=True, sort_keys=True), encoding="utf-8")
        payload["live_provenance"]["providers"][provider] = {
            "credential_scope": scope,
            "live_provider_verified": True,
            "endpoint": endpoint,
            "artifact_refs": [{"path": f"artifacts/{provider}_live.json", "sha256": "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest(), "sensitive_scan_clean": True}],
        }
    for scenario in payload["scenarios"].values():
        scenario["live_provider_verified"] = True
    _add_strict_live_scenario_artifacts(payload, tmp_path, run_id)

    claude_ref = payload["scenarios"]["claude_native"]["artifact_refs"][0]
    claude_artifact = tmp_path / claude_ref["path"]
    body = json.loads(claude_artifact.read_text(encoding="utf-8"))
    body.pop("endpoint")
    claude_artifact.write_text(json.dumps(body, ensure_ascii=True, sort_keys=True), encoding="utf-8")
    claude_ref["sha256"] = "sha256:" + hashlib.sha256(claude_artifact.read_bytes()).hexdigest()

    result = verify_cp8_live_matrix(payload, strict_live=True, evidence_root=tmp_path)
    assert result.status == "fail"
    assert "claude_native" in result.failed

    body["endpoint"] = "https://api.anthropic.com/v1/messages"
    body["upstream_request_id"] = "req_not_in_provider_proof"
    claude_artifact.write_text(json.dumps(body, ensure_ascii=True, sort_keys=True), encoding="utf-8")
    claude_ref["sha256"] = "sha256:" + hashlib.sha256(claude_artifact.read_bytes()).hexdigest()

    result = verify_cp8_live_matrix(payload, strict_live=True, evidence_root=tmp_path)
    assert result.status == "fail"
    assert "claude_native" in result.failed



def test_cp8_strict_live_rejects_provider_artifact_endpoint_path_drift(tmp_path: Path):
    payload = _fixture("live_matrix_pass.json")
    payload["mode"] = "external_provider_live_matrix"
    run_id = "cp8-live-endpoint-drift"
    payload["live_provenance"] = {"credential_backed": True, "loopback_only": False, "run_id": run_id, "providers": {}}
    providers = {
        "claude": ("formal_pool", "https://api.anthropic.com/v1/messages"),
        "openai": ("bridge_pool", "https://api.openai.com/v1/responses"),
        "deepseek": ("bridge_pool", "https://api.deepseek.com/anthropic/v1/messages"),
    }
    artifacts_dir = tmp_path / "artifacts"
    artifacts_dir.mkdir()
    for provider, (scope, endpoint) in providers.items():
        artifact_endpoint = "https://api.openai.com/v1/chat/completions" if provider == "openai" else endpoint
        proof = {
            "schema_version": "cp8-live-provider-provenance-v1",
            "checkpoint": "CP8",
            "run_id": run_id,
            "provider": provider,
            "credential_scope": scope,
            "endpoint": artifact_endpoint,
            "host": artifact_endpoint.split("/")[2],
            "external_live_verified": True,
            "loopback": False,
            "response_status": 200,
            "upstream_request_id": f"req_{provider}_live",
        }
        artifact = artifacts_dir / f"{provider}_live.json"
        artifact.write_text(json.dumps(proof, ensure_ascii=True, sort_keys=True), encoding="utf-8")
        payload["live_provenance"]["providers"][provider] = {
            "credential_scope": scope,
            "live_provider_verified": True,
            "endpoint": endpoint,
            "artifact_refs": [{"path": f"artifacts/{provider}_live.json", "sha256": "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest(), "sensitive_scan_clean": True}],
        }
    for scenario in payload["scenarios"].values():
        scenario["live_provider_verified"] = True
    _add_strict_live_scenario_artifacts(payload, tmp_path, run_id)

    result = verify_cp8_live_matrix(payload, strict_live=True, evidence_root=tmp_path)

    assert result.status == "fail"
    assert "live_provenance" in result.failed
    assert "gpt_bridge" in result.failed
    assert any("external live scenario artifact" in issue for issue in result.scenario_results["gpt_bridge"].issues)

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
        collect_cp8_live_provider_provenance as exported_collect,
        assemble_cp8_external_live_matrix_evidence as exported_assemble,
        verify_cp8_live_matrix as exported_verify,
        write_cp8_live_scenario_evidence as exported_write_scenario,
    )
    from zhumeng_agent.adapters.claude_code.live_matrix import collect_cp8_live_provider_provenance  # noqa: PLC0415
    from zhumeng_agent.adapters.claude_code.live_matrix import write_cp8_live_scenario_evidence  # noqa: PLC0415

    assert ExportedError is CP8LiveMatrixError
    assert ExportedScenarios is REQUIRED_CP8_SCENARIOS
    assert exported_verify is verify_cp8_live_matrix
    assert exported_collect is collect_cp8_live_provider_provenance
    assert exported_assemble is assemble_cp8_external_live_matrix_evidence
    assert exported_write_scenario is write_cp8_live_scenario_evidence


def test_cp8_live_evidence_default_transport_posts_official_protocol_shapes(monkeypatch, tmp_path: Path):
    from zhumeng_agent.adapters.claude_code.live_matrix import collect_cp8_live_provider_provenance  # noqa: PLC0415

    calls: list[dict[str, object]] = []

    class FakeHTTPResponse:
        status = 200
        headers = {"x-request-id": "req_live_default_transport"}

        def __enter__(self):
            return self

        def __exit__(self, exc_type, exc, tb):
            return False

        def read(self, *_args, **_kwargs):
            return b"{}"

    def fake_urlopen(request, timeout=0):
        body = json.loads((request.data or b"{}").decode("utf-8"))
        calls.append({
            "url": request.full_url,
            "headers": {str(k).lower(): str(v) for k, v in request.header_items()},
            "body": body,
            "timeout": timeout,
        })
        return FakeHTTPResponse()

    monkeypatch.setattr(urllib.request, "urlopen", fake_urlopen)
    monkeypatch.setenv("SUB2API_CLAUDE_CODE_LIVE_CLAUDE_MODEL", "claude-sonnet-4-6")
    monkeypatch.setenv("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_LIVE_MODEL", "gpt-5.4-mini")
    monkeypatch.setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_MODEL", "deepseek-v4-pro")

    provenance = collect_cp8_live_provider_provenance(
        run_id="cp8-default-transport",
        output_root=tmp_path,
        credentials={
            "claude": "anthropic-live-key",
            "openai": "openai-live-key",
            "deepseek": "deepseek-live-key",
        },
    )

    assert provenance["credential_backed"] is True
    assert {call["url"] for call in calls} == {
        "https://api.anthropic.com/v1/messages",
        "https://api.openai.com/v1/responses",
        "https://api.deepseek.com/anthropic/v1/messages",
    }
    by_url = {call["url"]: call for call in calls}

    claude = by_url["https://api.anthropic.com/v1/messages"]
    assert claude["headers"]["x-api-key"] == "anthropic-live-key"
    assert claude["headers"]["anthropic-version"] == "2023-06-01"
    assert claude["body"] == {
        "model": "claude-sonnet-4-6",
        "max_tokens": 1,
        "messages": [{"role": "user", "content": "CP8 live provenance probe."}],
    }

    openai = by_url["https://api.openai.com/v1/responses"]
    assert openai["headers"]["authorization"] == "Bearer openai-live-key"
    assert openai["body"] == {
        "model": "gpt-5.4-mini",
        "input": "CP8 live provenance probe.",
        "max_output_tokens": 1,
        "stream": False,
    }

    deepseek = by_url["https://api.deepseek.com/anthropic/v1/messages"]
    assert deepseek["headers"]["x-api-key"] == "deepseek-live-key"
    assert deepseek["headers"]["anthropic-version"] == "2023-06-01"
    assert deepseek["body"]["model"] == "deepseek-v4-pro"

    for provider in ("claude", "openai", "deepseek"):
        ref = provenance["providers"][provider]["artifact_refs"][0]
        artifact_text = (tmp_path / ref["path"]).read_text(encoding="utf-8")
        assert "live-key" not in artifact_text
        assert "Authorization" not in artifact_text


def test_cp8_assemble_external_live_matrix_binds_provider_provenance_without_promoting_loopback(tmp_path: Path):
    from zhumeng_agent.adapters.claude_code.live_matrix import collect_cp8_live_provider_provenance  # noqa: PLC0415

    provenance = collect_cp8_live_provider_provenance(
        run_id="cp8-assemble-live",
        output_root=tmp_path,
        credentials={"claude": "sk-claude", "openai": "sk-openai", "deepseek": "sk-deepseek"},
        transport=lambda provider, endpoint, credential: {"status": 200, "request_id": f"req_{provider}_assemble"},
    )

    loopback = _fixture("live_matrix_pass.json")
    assembled_loopback = assemble_cp8_external_live_matrix_evidence(loopback, provenance)
    loopback_result = verify_cp8_live_matrix(assembled_loopback, strict_live=True, evidence_root=tmp_path)
    assert loopback_result.status == "fail"
    assert loopback_result.release_gate == "blocked_missing_external_live"
    assert all(scenario.get("live_provider_verified") is False for scenario in loopback["scenarios"].values())

    external = _fixture("live_matrix_pass.json")
    external["live_provenance"] = {"must": "be replaced"}
    for scenario in external["scenarios"].values():
        scenario["live_provider_verified"] = True
    _add_strict_live_scenario_artifacts(external, tmp_path, "cp8-assemble-live")

    assembled_external = assemble_cp8_external_live_matrix_evidence(external, provenance)
    assert assembled_external["mode"] == "external_provider_live_matrix"
    assert assembled_external["live_provenance"] == provenance
    assert assembled_external is not external

    result = verify_cp8_live_matrix(assembled_external, strict_live=True, evidence_root=tmp_path)
    assert result.status == "pass"
    assert result.release_gate == "external_live_passed"


def test_cp8_assemble_external_live_matrix_rejects_inline_sensitive_or_raw_fields():
    payload = _fixture("live_matrix_pass.json")
    provenance = {
        "credential_backed": True,
        "loopback_only": False,
        "run_id": "cp8-sensitive-inline",
        "providers": {
            "claude": {
                "credential_scope": "formal_pool",
                "live_provider_verified": True,
                "endpoint": "https://api.anthropic.com/v1/messages",
                "headers": {"authorization": "Bearer sk-must-not-persist"},
            },
        },
    }

    with pytest.raises(CP8LiveMatrixError, match="sensitive inline"):
        assemble_cp8_external_live_matrix_evidence(payload, provenance)

    payload = _fixture("live_matrix_pass.json")
    payload["raw_body"] = {"messages": [{"role": "user", "content": "must not persist"}]}
    with pytest.raises(CP8LiveMatrixError, match="sensitive inline"):
        assemble_cp8_external_live_matrix_evidence(payload, {"credential_backed": True})


@pytest.mark.parametrize(
    "key",
    [
        "token",
        "access_token",
        "refresh_token",
        "secret",
        "client_secret",
        "raw",
        "payload",
        "raw_payload",
        "request_payload",
        "response_payload",
        "auth_token",
        "session_token",
        "accessToken",
        "refreshToken",
        "clientSecret",
        "secret_key",
        "secretKey",
        "secret_access_key",
        "secrets",
        "rawRequest",
        "requestPayload",
        "responseHeaders",
    ],
)
def test_cp8_assemble_external_live_matrix_rejects_common_secret_and_payload_keys(key: str):
    payload = _fixture("live_matrix_pass.json")
    provenance = {"credential_backed": True, "providers": {"claude": {key: "opaque-secret-value"}}}

    with pytest.raises(CP8LiveMatrixError, match="sensitive inline"):
        assemble_cp8_external_live_matrix_evidence(payload, provenance)


def test_cp8_live_evidence_collector_requires_all_provider_credentials(tmp_path: Path):
    from zhumeng_agent.adapters.claude_code.live_matrix import collect_cp8_live_provider_provenance  # noqa: PLC0415

    with pytest.raises(CP8LiveMatrixError, match="missing live credential"):
        collect_cp8_live_provider_provenance(
            run_id="cp8-live-missing-creds",
            output_root=tmp_path,
            credentials={
                "deepseek": "sk-deepseek-present",
            },
            transport=lambda provider, endpoint, credential: {"status": 200, "request_id": f"req_{provider}"},
        )


def test_cp8_live_evidence_collector_writes_sanitized_provider_bound_artifacts(tmp_path: Path):
    from zhumeng_agent.adapters.claude_code.live_matrix import collect_cp8_live_provider_provenance  # noqa: PLC0415

    calls: list[tuple[str, str, str]] = []

    def transport(provider: str, endpoint: str, credential: str) -> dict[str, object]:
        calls.append((provider, endpoint, credential))
        assert credential.startswith("sk-")
        return {
            "status": 200,
            "request_id": f"req_{provider}_live",
            "response_headers": {
                "x-request-id": f"req_{provider}_header",
                "authorization": "Bearer must-not-be-written",
            },
        }

    provenance = collect_cp8_live_provider_provenance(
        run_id="cp8-live-run-collector",
        output_root=tmp_path,
        credentials={
            "claude": "sk-claude-secret",
            "openai": "sk-openai-secret",
            "deepseek": "sk-deepseek-secret",
        },
        transport=transport,
    )

    assert provenance["credential_backed"] is True
    assert provenance["loopback_only"] is False
    assert set(provenance["providers"]) == {"claude", "openai", "deepseek"}
    assert {call[0] for call in calls} == {"claude", "openai", "deepseek"}

    payload = _fixture("live_matrix_pass.json")
    payload["mode"] = "external_provider_live_matrix"
    payload["live_provenance"] = provenance
    for scenario in payload["scenarios"].values():
        scenario["live_provider_verified"] = True
    _add_strict_live_scenario_artifacts(payload, tmp_path, "cp8-live-run-collector")

    result = verify_cp8_live_matrix(payload, strict_live=True, evidence_root=tmp_path)
    assert result.status == "pass"
    assert result.release_gate == "external_live_passed"

    for provider in ("claude", "openai", "deepseek"):
        ref = provenance["providers"][provider]["artifact_refs"][0]
        artifact_text = (tmp_path / ref["path"]).read_text(encoding="utf-8")
        assert "sk-" not in artifact_text
        assert "Bearer" not in artifact_text
        assert "must-not-be-written" not in artifact_text
        assert provider in artifact_text


def test_cp8_live_evidence_collector_requires_success_and_rejects_sensitive_request_ids(tmp_path: Path):
    from zhumeng_agent.adapters.claude_code.live_matrix import collect_cp8_live_provider_provenance  # noqa: PLC0415

    credentials = {
        "claude": "sk-claude-secret",
        "openai": "sk-openai-secret",
        "deepseek": "sk-deepseek-secret",
    }

    with pytest.raises(CP8LiveMatrixError, match="2xx live request id"):
        collect_cp8_live_provider_provenance(
            run_id="cp8-live-401",
            output_root=tmp_path,
            credentials=credentials,
            transport=lambda provider, endpoint, credential: {"status": 401, "request_id": f"req_{provider}"},
        )

    with pytest.raises(CP8LiveMatrixError, match="2xx live request id"):
        collect_cp8_live_provider_provenance(
            run_id="cp8-live-sensitive-request-id",
            output_root=tmp_path,
            credentials=credentials,
            transport=lambda provider, endpoint, credential: {"status": 200, "request_id": "Bearer sk-live-secret"},
        )


def test_cp8_live_scenario_evidence_writer_creates_hashable_strict_artifact(tmp_path: Path):
    from zhumeng_agent.adapters.claude_code.live_matrix import write_cp8_live_scenario_evidence  # noqa: PLC0415

    payload = _fixture("live_matrix_pass.json")
    scenario = payload["scenarios"]["manual_provider_switch"]
    ref = write_cp8_live_scenario_evidence(
        output_root=tmp_path,
        run_id="cp8-scenario-writer",
        scenario="manual_provider_switch",
        route=scenario.get("route", ""),
        client_type=scenario.get("client_type", ""),
        evidence={
            "status": "pass",
            "live_provider_verified": True,
            "raw_sensitive_stored": False,
            "loopback": False,
            "provider": "deepseek",
            "endpoint": "https://api.deepseek.com/anthropic/v1/messages",
            "upstream_request_id": "req_deepseek",
            "provider_provenance_refs": ["artifacts/provider_deepseek.json"],
            "notes": "safe summary only",
            "authorization": "Bearer must-not-be-written",
        },
    )

    assert ref["path"] == "artifacts/scenario_manual_provider_switch.json"
    assert ref["sensitive_scan_clean"] is True
    artifact_text = (tmp_path / ref["path"]).read_text(encoding="utf-8")
    assert "Bearer" not in artifact_text
    assert "must-not-be-written" not in artifact_text

    payload["mode"] = "external_provider_live_matrix"
    payload["live_provenance"] = {
        "credential_backed": True,
        "loopback_only": False,
        "run_id": "cp8-scenario-writer",
        "providers": {},
    }
    for provider, scope, endpoint in (
        ("claude", "formal_pool", "https://api.anthropic.com/v1/messages"),
        ("openai", "bridge_pool", "https://api.openai.com/v1/responses"),
        ("deepseek", "bridge_pool", "https://api.deepseek.com/anthropic/v1/messages"),
    ):
        provider_artifact = tmp_path / "artifacts" / f"provider_{provider}.json"
        provider_artifact.write_text(json.dumps({
            "schema_version": "cp8-live-provider-provenance-v1",
            "checkpoint": "CP8",
            "run_id": "cp8-scenario-writer",
            "provider": provider,
            "credential_scope": scope,
            "endpoint": endpoint,
            "host": endpoint.split("/")[2],
            "external_live_verified": True,
            "loopback": False,
            "response_status": 200,
            "upstream_request_id": f"req_{provider}",
        }, sort_keys=True), encoding="utf-8")
        payload["live_provenance"]["providers"][provider] = {
            "credential_scope": scope,
            "live_provider_verified": True,
            "endpoint": endpoint,
            "artifact_refs": [{
                "path": f"artifacts/provider_{provider}.json",
                "sha256": "sha256:" + hashlib.sha256(provider_artifact.read_bytes()).hexdigest(),
                "sensitive_scan_clean": True,
            }],
        }
    for name, item in payload["scenarios"].items():
        item["live_provider_verified"] = True
        if name == "manual_provider_switch":
            item["artifact_refs"] = [ref]
    _add_strict_live_scenario_artifacts({
        "scenarios": {k: v for k, v in payload["scenarios"].items() if k != "manual_provider_switch"}
    }, tmp_path, "cp8-scenario-writer")

    result = verify_cp8_live_matrix(payload, strict_live=True, evidence_root=tmp_path)
    assert result.status == "pass"
