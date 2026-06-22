from __future__ import annotations

import base64
import hashlib
import json
import shutil
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




def _decode_cp8_runtime_header(value: str) -> dict[str, object]:
    padded = value + "=" * (-len(value) % 4)
    decoded = base64.urlsafe_b64decode(padded.encode("ascii")).decode("utf-8")
    payload = json.loads(decoded)
    assert isinstance(payload, dict)
    return payload



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


def _scenario_providers(name: str) -> tuple[str, ...]:
    return {
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
    }[name]


def _provider_endpoint(provider: str) -> str:
    return {
        "claude": "https://api.anthropic.com/v1/messages",
        "openai": "https://api.openai.com/v1/responses",
        "deepseek": "https://api.deepseek.com/anthropic/v1/messages",
    }[provider]


def _provider_model(provider: str) -> str:
    return {
        "claude": "claude-sonnet-4-6",
        "openai": "gpt-5.5",
        "deepseek": "deepseek-v4-pro",
    }[provider]


def _provider_route_client(provider: str) -> tuple[str, str]:
    return {
        "claude": ("claude_code_native", "claude_code_native"),
        "openai": ("openai_bridge", "claude_code_bridge_openai"),
        "deepseek": ("deepseek_bridge", "claude_code_bridge_deepseek"),
    }[provider]


def _default_provider_release_classification(*, strict_live: bool = False) -> dict[str, dict[str, str]]:
    core_status = "strict-live-pass" if strict_live else "fixture-pass-only"
    core_reason = "strict_live_evidence_verified" if strict_live else "strict_live_evidence_pending"
    core_evidence = "cp8_live_provenance" if strict_live else "checkpoint0_scope_frozen_strict_live_required"
    return {
        "claude_native": {"status": core_status, "evidence": core_evidence, "reason": core_reason},
        "openai": {"status": core_status, "evidence": core_evidence, "reason": core_reason},
        "deepseek": {"status": core_status, "evidence": core_evidence, "reason": core_reason},
        "agnes": {"status": "live-disabled", "evidence": "checkpoint0_conditional_probe_required", "reason": "probe_and_strict_live_required"},
        "glm": {"status": "live-disabled", "evidence": "checkpoint0_catalog_visible_live_disabled", "reason": "outside_l8_live_scope"},
        "kimi": {"status": "live-disabled", "evidence": "checkpoint0_catalog_visible_live_disabled", "reason": "outside_l8_live_scope"},
    }


def _mark_core_provider_release_strict_live(payload: dict[str, object]) -> None:
    payload["provider_release_classification"] = _default_provider_release_classification(strict_live=True)


def _mark_toolsearch_bridge_strict_live(payload: dict[str, object]) -> None:
    scenario = payload["scenarios"]["toolsearch_mcp"]
    scenario["toolsearch_mode"] = "shim"
    scenario["bridge_provider_toolsearch"] = {
        provider: {
            "mode": "shim",
            "toolsearch_degraded": False,
            "shim_resolution_verified": True,
            "unresolved_tool_reference_upstream": False,
            "unresolved_defer_loading_upstream": False,
        }
        for provider in ("openai", "deepseek")
    }


def _cache_provider_evidence(*, strict_live: bool = False) -> dict[str, dict[str, object]]:
    return {
        "claude_native": {
            "mechanism": "anthropic_cache_control",
            "request_shape_preserved": True,
            "cache_usage_fields": ["cache_creation_input_tokens", "cache_read_input_tokens"],
            "live_usage_fields_observed": strict_live,
            "raw_sensitive_stored": False,
        },
        "openai": {
            "mechanism": "openai_prompt_cache",
            "request_shape_preserved": True,
            "cache_usage_fields": ["usage.prompt_tokens_details.cached_tokens"],
            "live_usage_fields_observed": strict_live,
            "raw_sensitive_stored": False,
        },
        "deepseek": {
            "mechanism": "deepseek_prefix_kv",
            "cache_control_provider_ignored": True,
            "treats_cache_control_as_cache_mechanism": False,
            "request_shape_preserved": True,
            "cache_usage_fields": ["prompt_cache_hit_tokens", "prompt_cache_miss_tokens"],
            "stable_prefix_hmac_present": True,
            "live_usage_fields_observed": strict_live,
            "raw_sensitive_stored": False,
        },
    }


def _mark_cache_account_strict_live(payload: dict[str, object]) -> None:
    payload["scenarios"]["cache_account_audit"]["cache_provider_evidence"] = _cache_provider_evidence(strict_live=True)


def _provider_live_artifact(root: Path, provider: str) -> tuple[str, str, str]:
    candidates = [
        root / "artifacts" / f"{provider}_sub2api_live_provenance.json",
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



def _sub2api_endpoint(provider: str) -> str:
    return {
        "claude": "http://127.0.0.1:3012/v1/messages",
        "openai": "http://127.0.0.1:3012/v1/messages",
        "deepseek": "http://127.0.0.1:3012/v1/messages",
    }[provider]


def _add_sub2api_strict_live_scenario_artifacts(payload: dict[str, object], root: Path, run_id: str) -> None:
    _mark_core_provider_release_strict_live(payload)
    _mark_toolsearch_bridge_strict_live(payload)
    _mark_cache_account_strict_live(payload)
    artifacts_dir = root / "artifacts"
    artifacts_dir.mkdir(exist_ok=True)
    for name, scenario in payload["scenarios"].items():
        refs = []
        if name == "workflow":
            refs.append(_workflow_background_ref(root))
        if name == "cache_account_audit":
            refs.append(_cache_account_ref(root, scenario, strict_live=True))
        for provider in _scenario_providers(name):
            provider_ref, _endpoint, request_id = _provider_live_artifact(root, provider)
            endpoint = _sub2api_endpoint(provider)
            route, client_type = _provider_route_client(provider)
            artifact = artifacts_dir / f"scenario_{name}_{provider}.json"
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
                        "route": route,
                        "client_type": client_type,
                        "provider": provider,
                        "model": _provider_model(provider),
                        "endpoint": endpoint,
                        "upstream_request_id": request_id,
                        "provider_provenance_refs": [provider_ref],
                    },
                    ensure_ascii=True,
                    sort_keys=True,
                ),
                encoding="utf-8",
            )
            refs.append(
                {
                    "path": f"artifacts/{artifact.name}",
                    "sha256": "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest(),
                    "sensitive_scan_clean": True,
                }
            )
        scenario["artifact_refs"] = refs

def _workflow_background_ref(root: Path) -> dict[str, object]:
    tasks = ("title", "compact", "summary", "probe", "fast", "simple", "haiku")
    model_by_task = {
        "title": "deepseek-v4-flash",
        "compact": "deepseek-v4-pro",
        "summary": "deepseek-v4-pro",
        "probe": "deepseek-v4-flash",
        "fast": "deepseek-v4-flash",
        "simple": "deepseek-v4-flash",
        "haiku": "deepseek-v4-flash",
    }
    artifact = root / "artifacts" / "workflow_background.json"
    artifact.write_text(
        json.dumps(
            {
                "schema_version": "cp8-workflow-background-v1",
                "checkpoint": "CP8",
                "scenario": "workflow",
                "status": "pass",
                "active_profile_dynamic_resolution": True,
                "workflow_alias_resolved_provider_local": True,
                "hardcoded_claude_model_consumed": False,
                "non_claude_background_native_egress": 0,
                "required_background_tasks": list(tasks),
                "background_tasks": [
                    {
                        "task": task,
                        "active_profile": "deepseek",
                        "requested_alias": task,
                        "resolved_provider": "deepseek",
                        "resolved_model": model_by_task[task],
                        "dynamic_resolution_at_request_time": True,
                        "provider_local": True,
                        "native_egress_count": 0,
                        "formal_pool_egress_count": 0,
                        "hardcoded_claude_model_consumed": False,
                        "explicit_claude_opt_in": False,
                    }
                    for task in tasks
                ],
            },
            ensure_ascii=True,
            sort_keys=True,
        ),
        encoding="utf-8",
    )
    return {
        "path": "artifacts/workflow_background.json",
        "sha256": "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest(),
        "sensitive_scan_clean": True,
    }


def _cache_account_ref(root: Path, scenario: dict[str, object], *, strict_live: bool) -> dict[str, object]:
    artifact = root / "artifacts" / "cache_account_audit.json"
    artifact.write_text(
        json.dumps(
            {
                "schema_version": "cp8-cache-account-audit-v1",
                "checkpoint": "CP8",
                "scenario": "cache_account_audit",
                "status": "pass",
                "raw_sensitive_stored": False,
                "safe_summary_hash_stable": scenario["safe_summary_hash_stable"],
                "safe_tool_result_hash_stable": scenario["safe_tool_result_hash_stable"],
                "ttl_fast_switch_boundary_miss_count": scenario["ttl_fast_switch_boundary_miss_count"],
                "stable_prefix_invalidated": scenario["stable_prefix_invalidated"],
                "usage_accounting_split_by_route": scenario["usage_accounting_split_by_route"],
                "audit_summary_only": scenario["audit_summary_only"],
                "cache_provider_evidence": _cache_provider_evidence(strict_live=strict_live),
                "bridge_cache_audit_rows": [
                    {
                        "schema_version": "claude-code-bridge-cache-audit-row-v1",
                        "provider": "deepseek",
                        "route": "deepseek_bridge",
                        "client_type": "claude_code_bridge_deepseek",
                        "model_id": "deepseek-v4-pro",
                        "preferred_protocol": "anthropic_messages",
                        "selected_protocol": "anthropic_messages",
                        "fallback_protocol": "openai_chat_completions",
                        "fallback_reason": "",
                        "fallback_used": False,
                        "provider_cache_mechanism": "deepseek_prefix_kv",
                        "upstream_path_kind": "/anthropic/v1/messages",
                        "stable_prefix_hmac": "hmac-sha256:cp8-cache:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
                        "stable_prefix_token_bucket": "1k_4k",
                        "cache_control_present": True,
                        "cache_control_locations": ["history", "system", "tools", "top_level"],
                        "cache_control_provider_ignored": True,
                        "prompt_cache_key_present": False,
                        "prompt_cache_key_strategy": "absent",
                        "cache_usage_fields": ["prompt_cache_hit_tokens", "prompt_cache_miss_tokens"],
                        "cache_read_tokens": 7,
                        "cache_miss_tokens": 13,
                        "raw_sensitive_stored": False,
                    },
                    {
                        "schema_version": "claude-code-bridge-cache-audit-row-v1",
                        "provider": "openai",
                        "route": "openai_bridge",
                        "client_type": "claude_code_bridge_openai",
                        "model_id": "gpt-5.5",
                        "preferred_protocol": "responses",
                        "selected_protocol": "responses",
                        "fallback_protocol": "",
                        "fallback_reason": "",
                        "fallback_used": False,
                        "provider_cache_mechanism": "openai_prompt_cache",
                        "upstream_path_kind": "/v1/responses",
                        "stable_prefix_hmac": "hmac-sha256:cp8-cache:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
                        "stable_prefix_token_bucket": "1k_4k",
                        "cache_control_present": False,
                        "cache_control_locations": [],
                        "cache_control_provider_ignored": False,
                        "prompt_cache_key_present": True,
                        "prompt_cache_key_strategy": "present_redacted",
                        "cache_usage_fields": ["usage.prompt_tokens_details.cached_tokens"],
                        "cache_read_tokens": 9,
                        "cached_tokens": 9,
                        "raw_sensitive_stored": False,
                    },
                ],
            },
            ensure_ascii=True,
            sort_keys=True,
        ),
        encoding="utf-8",
    )
    return {
        "path": "artifacts/cache_account_audit.json",
        "sha256": "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest(),
        "sensitive_scan_clean": True,
    }


def _bridge_cache_row(rows: list[dict[str, object]], provider: str) -> dict[str, object]:
    for row in rows:
        if row.get("provider") == provider:
            return row
    raise AssertionError(f"missing bridge cache row for {provider}")


def _add_strict_live_scenario_artifacts(payload: dict[str, object], root: Path, run_id: str) -> None:
    _mark_core_provider_release_strict_live(payload)
    _mark_toolsearch_bridge_strict_live(payload)
    _mark_cache_account_strict_live(payload)
    artifacts_dir = root / "artifacts"
    artifacts_dir.mkdir(exist_ok=True)
    for name, scenario in payload["scenarios"].items():
        refs = []
        if name == "workflow":
            refs.append(_workflow_background_ref(root))
        if name == "cache_account_audit":
            refs.append(_cache_account_ref(root, scenario, strict_live=True))
        for provider in _scenario_providers(name):
            provider_ref, endpoint, request_id = _provider_live_artifact(root, provider)
            route, client_type = _provider_route_client(provider)
            artifact = artifacts_dir / f"scenario_{name}_{provider}.json"
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
                        "route": route,
                        "client_type": client_type,
                        "provider": provider,
                        "model": _provider_model(provider),
                        "endpoint": endpoint,
                        "upstream_request_id": request_id,
                        "provider_provenance_refs": [provider_ref],
                    },
                    ensure_ascii=True,
                    sort_keys=True,
                ),
                encoding="utf-8",
            )
            refs.append(
                {
                    "path": f"artifacts/{artifact.name}",
                    "sha256": "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest(),
                    "sensitive_scan_clean": True,
                }
            )
        scenario["artifact_refs"] = refs



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


def test_cp8_live_matrix_requires_exact_provider_release_classification():
    payload = _fixture("live_matrix_pass.json")
    payload.pop("provider_release_classification", None)

    result = verify_cp8_live_matrix(payload, evidence_root=FIXTURE_DIR)

    assert result.status == "fail"
    assert "provider_release_classification" in result.failed

    payload = _fixture("live_matrix_pass.json")
    payload["provider_release_classification"].pop("kimi")
    result = verify_cp8_live_matrix(payload, evidence_root=FIXTURE_DIR)
    assert result.status == "fail"
    assert "provider_release_classification" in result.failed

    payload = _fixture("live_matrix_pass.json")
    payload["provider_release_classification"]["glm"]["status"] = "probably-live"
    result = verify_cp8_live_matrix(payload, evidence_root=FIXTURE_DIR)
    assert result.status == "fail"
    assert "provider_release_classification" in result.failed

    payload = _fixture("live_matrix_pass.json")
    payload["provider_release_classification"]["agnes"]["status"] = "strict-live-pass"
    result = verify_cp8_live_matrix(payload, evidence_root=FIXTURE_DIR)
    assert result.status == "fail"
    assert "provider_release_classification" in result.failed

    payload = _fixture("live_matrix_pass.json")
    payload["provider_release_classification"]["kimi"]["status"] = "degraded-pass"
    result = verify_cp8_live_matrix(payload, evidence_root=FIXTURE_DIR)
    assert result.status == "fail"
    assert "provider_release_classification" in result.failed


def test_cp8_provider_release_classification_rejects_sensitive_or_raw_metadata():
    for provider, field, value in (
        ("openai", "evidence", "raw_body_capture"),
        ("deepseek", "reason", "raw_prompt_observed"),
        ("agnes", "evidence", "client_secret_probe"),
        ("glm", "reason", "password-present"),
        ("openai", "evidence", "raw_capture"),
        ("deepseek", "reason", "raw"),
        ("agnes", "evidence", "token_seen"),
        ("glm", "reason", "session_token_seen"),
        ("openai", "evidence", "rawCapture"),
        ("deepseek", "reason", "tokenSeen"),
        ("agnes", "evidence", "sessionTokenSeen"),
    ):
        payload = _fixture("live_matrix_pass.json")
        payload["provider_release_classification"][provider][field] = value
        result = verify_cp8_live_matrix(payload, evidence_root=FIXTURE_DIR)
        assert result.status == "fail"
        assert "provider_release_classification" in result.failed

    payload = _fixture("live_matrix_pass.json")
    payload["provider_release_classification"]["openai"]["raw_body"] = "must-not-be-allowed"
    result = verify_cp8_live_matrix(payload, evidence_root=FIXTURE_DIR)
    assert result.status == "fail"
    assert "provider_release_classification" in result.failed


def test_cp8_strict_live_requires_core_provider_classification_to_match_live_proof(tmp_path: Path):
    payload = _fixture("live_matrix_pass.json")
    payload["mode"] = "external_provider_live_matrix"
    run_id = "cp8-classification-strict-live"
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
            "model": _provider_model(provider),
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
            "model": _provider_model(provider),
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
    payload["provider_release_classification"]["deepseek"]["status"] = "fixture-pass-only"

    result = verify_cp8_live_matrix(payload, strict_live=True, evidence_root=tmp_path)

    assert result.status == "fail"
    assert "provider_release_classification" in result.failed


def test_cp8_strict_live_requires_every_scenario_provider_binding(tmp_path: Path):
    payload = _fixture("live_matrix_pass.json")
    payload["mode"] = "external_provider_live_matrix"
    run_id = "cp8-scenario-all-provider-binding"
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
            "model": _provider_model(provider),
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
            "model": _provider_model(provider),
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
    # manual_provider_switch requires claude/openai/deepseek. Keeping exactly
    # one provider artifact must not be enough to claim strict-live coverage.
    manual = payload["scenarios"]["manual_provider_switch"]
    manual["artifact_refs"] = [
        ref for ref in manual["artifact_refs"] if ref.get("path", "").endswith("scenario_manual_provider_switch_deepseek.json")
    ]
    assert len(manual["artifact_refs"]) == 1

    result = verify_cp8_live_matrix(payload, strict_live=True, evidence_root=tmp_path)

    assert result.status == "fail"
    assert "manual_provider_switch" in result.failed
    assert any("provider binding" in issue for issue in result.scenario_results["manual_provider_switch"].issues)


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


def test_cp8_workflow_requires_all_dynamic_background_tasks_to_be_provider_local():
    payload = _fixture("live_matrix_pass.json")
    workflow = payload["scenarios"]["workflow"]
    assert workflow["required_background_tasks"] == ["title", "compact", "summary", "probe", "fast", "simple", "haiku"]
    workflow["required_background_tasks"] = ["title", "summary"]

    result = verify_cp8_live_matrix(payload, evidence_root=FIXTURE_DIR)

    assert result.status == "fail"
    assert "workflow" in result.failed
    assert any("workflow/background tasks" in issue for issue in result.scenario_results["workflow"].issues)


def test_cp8_toolsearch_mcp_rejects_claimed_shim_without_bridge_provider_evidence():
    payload = _fixture("live_matrix_pass.json")
    toolsearch = payload["scenarios"]["toolsearch_mcp"]
    toolsearch["toolsearch_mode"] = "shim"
    toolsearch.pop("bridge_provider_toolsearch", None)

    result = verify_cp8_live_matrix(payload, evidence_root=FIXTURE_DIR)

    assert result.status == "fail"
    assert "toolsearch_mcp" in result.failed
    assert any("ToolSearch" in issue for issue in result.scenario_results["toolsearch_mcp"].issues)


def test_cp8_toolsearch_mcp_rejects_claimed_shim_with_degraded_provider_entries():
    payload = _fixture("live_matrix_pass.json")
    toolsearch = payload["scenarios"]["toolsearch_mcp"]
    toolsearch["toolsearch_mode"] = "shim"
    toolsearch["bridge_provider_toolsearch"] = {
        provider: {
            "mode": "disabled",
            "toolsearch_degraded": True,
            "degraded_reason": "unresolved_lazy_shapes_fail_closed",
            "unresolved_tool_reference_upstream": False,
            "unresolved_defer_loading_upstream": False,
        }
        for provider in ("openai", "deepseek")
    }

    result = verify_cp8_live_matrix(payload, evidence_root=FIXTURE_DIR)

    assert result.status == "fail"
    assert "toolsearch_mcp" in result.failed
    assert any("provider mode" in issue for issue in result.scenario_results["toolsearch_mcp"].issues)


def test_cp8_strict_live_rejects_degraded_bridge_toolsearch_policy(tmp_path: Path):
    payload = _fixture("live_matrix_pass.json")
    payload["mode"] = "external_provider_live_matrix"
    run_id = "cp8-toolsearch-degraded"
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
            "model": _provider_model(provider),
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
            "model": _provider_model(provider),
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
    payload["scenarios"]["toolsearch_mcp"]["toolsearch_mode"] = "degraded_disabled"
    payload["scenarios"]["toolsearch_mcp"]["bridge_provider_toolsearch"] = {
        provider: {
            "mode": "disabled",
            "toolsearch_degraded": True,
            "degraded_reason": "unresolved_lazy_shapes_fail_closed",
            "unresolved_tool_reference_upstream": False,
            "unresolved_defer_loading_upstream": False,
        }
        for provider in ("openai", "deepseek")
    }

    result = verify_cp8_live_matrix(payload, strict_live=True, evidence_root=tmp_path)

    assert result.status == "fail"
    assert "toolsearch_mcp" in result.failed
    assert any("strict-live" in issue for issue in result.scenario_results["toolsearch_mcp"].issues)


def test_cp8_strict_live_rejects_top_level_degraded_toolsearch_even_with_strict_provider_entries(tmp_path: Path):
    payload = _fixture("live_matrix_pass.json")
    payload["mode"] = "external_provider_live_matrix"
    run_id = "cp8-toolsearch-top-level-degraded"
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
            "model": _provider_model(provider),
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
            "model": _provider_model(provider),
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
    payload["scenarios"]["toolsearch_mcp"]["toolsearch_mode"] = "degraded_disabled"

    result = verify_cp8_live_matrix(payload, strict_live=True, evidence_root=tmp_path)

    assert result.status == "fail"
    assert "toolsearch_mcp" in result.failed
    assert any("strict-live" in issue for issue in result.scenario_results["toolsearch_mcp"].issues)



def test_cp8_workflow_background_artifact_cross_checks_each_dynamic_task(tmp_path: Path):
    root = tmp_path / "cp8"
    shutil.copytree(FIXTURE_DIR, root)
    payload = json.loads((root / "live_matrix_pass.json").read_text(encoding="utf-8"))
    workflow = payload["scenarios"]["workflow"]
    artifact_ref = workflow["artifact_refs"][0]
    artifact = root / artifact_ref["path"]
    body = json.loads(artifact.read_text(encoding="utf-8"))
    body["background_tasks"] = [
        {
            "task": "title",
            "active_profile": "deepseek",
            "resolved_provider": "deepseek",
            "resolved_model": "deepseek-v4-flash",
            "provider_local": True,
            "native_egress_count": 0,
            "formal_pool_egress_count": 0,
        }
    ]
    artifact.write_text(json.dumps(body, ensure_ascii=True, sort_keys=True), encoding="utf-8")
    artifact_ref["sha256"] = "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest()

    result = verify_cp8_live_matrix(payload, evidence_root=root)

    assert result.status == "fail"
    assert "workflow" in result.failed
    assert any("workflow/background artifact" in issue for issue in result.scenario_results["workflow"].issues)



def test_cp8_workflow_background_artifact_rejects_cross_provider_task_remap(tmp_path: Path):
    root = tmp_path / "cp8"
    shutil.copytree(FIXTURE_DIR, root)
    payload = json.loads((root / "live_matrix_pass.json").read_text(encoding="utf-8"))
    workflow = payload["scenarios"]["workflow"]
    artifact_ref = workflow["artifact_refs"][0]
    artifact = root / artifact_ref["path"]
    body = json.loads(artifact.read_text(encoding="utf-8"))
    assert body["background_tasks"][0]["active_profile"] == "deepseek"
    body["background_tasks"][0]["resolved_provider"] = "openai"
    body["background_tasks"][0]["resolved_model"] = "gpt-5.5"
    artifact.write_text(json.dumps(body, ensure_ascii=True, sort_keys=True), encoding="utf-8")
    artifact_ref["sha256"] = "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest()

    result = verify_cp8_live_matrix(payload, evidence_root=root)

    assert result.status == "fail"
    assert "workflow" in result.failed
    assert any("workflow/background artifact" in issue for issue in result.scenario_results["workflow"].issues)



def test_cp8_workflow_background_artifact_rejects_provider_model_family_drift(tmp_path: Path):
    root = tmp_path / "cp8"
    shutil.copytree(FIXTURE_DIR, root)
    payload = json.loads((root / "live_matrix_pass.json").read_text(encoding="utf-8"))
    workflow = payload["scenarios"]["workflow"]
    artifact_ref = workflow["artifact_refs"][0]
    artifact = root / artifact_ref["path"]
    body = json.loads(artifact.read_text(encoding="utf-8"))
    assert body["background_tasks"][0]["active_profile"] == "deepseek"
    assert body["background_tasks"][0]["resolved_provider"] == "deepseek"
    body["background_tasks"][0]["resolved_model"] = "gpt-5.5"
    artifact.write_text(json.dumps(body, ensure_ascii=True, sort_keys=True), encoding="utf-8")
    artifact_ref["sha256"] = "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest()

    result = verify_cp8_live_matrix(payload, evidence_root=root)

    assert result.status == "fail"
    assert "workflow" in result.failed
    assert any("workflow/background artifact" in issue for issue in result.scenario_results["workflow"].issues)


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


def test_cp8_cache_account_audit_requires_provider_truthful_cache_evidence():
    payload = _fixture("live_matrix_pass.json")
    payload["scenarios"]["cache_account_audit"].pop("cache_provider_evidence", None)

    result = verify_cp8_live_matrix(payload, evidence_root=FIXTURE_DIR)

    assert result.status == "fail"
    assert "cache_account_audit" in result.failed
    assert any("cache provider evidence" in issue for issue in result.scenario_results["cache_account_audit"].issues)


def test_cp8_cache_account_audit_rejects_deepseek_cache_control_as_cache_mechanism():
    payload = _fixture("live_matrix_pass.json")
    payload["scenarios"]["cache_account_audit"]["cache_provider_evidence"] = _cache_provider_evidence()
    deepseek = payload["scenarios"]["cache_account_audit"]["cache_provider_evidence"]["deepseek"]
    deepseek["mechanism"] = "anthropic_cache_control"
    deepseek["treats_cache_control_as_cache_mechanism"] = True

    result = verify_cp8_live_matrix(payload, evidence_root=FIXTURE_DIR)

    assert result.status == "fail"
    assert "cache_account_audit" in result.failed
    assert any("DeepSeek" in issue for issue in result.scenario_results["cache_account_audit"].issues)


def test_cp8_cache_account_audit_rejects_sensitive_inline_artifact_keys(tmp_path: Path):
    fixture_root = tmp_path / "cp8"
    shutil.copytree(FIXTURE_DIR, fixture_root)
    payload = json.loads((fixture_root / "live_matrix_pass.json").read_text(encoding="utf-8"))
    artifact = fixture_root / "artifacts" / "cache_account_audit.json"
    artifact_payload = json.loads(artifact.read_text(encoding="utf-8"))
    artifact_payload["raw_body"] = "hello world"
    artifact.write_text(json.dumps(artifact_payload, ensure_ascii=True, sort_keys=True), encoding="utf-8")
    updated_hash = "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest()
    for scenario in payload["scenarios"].values():
        for ref in scenario.get("artifact_refs", []):
            if ref.get("path") == "artifacts/cache_account_audit.json":
                ref["sha256"] = updated_hash

    result = verify_cp8_live_matrix(payload, evidence_root=fixture_root)

    assert result.status == "fail"
    assert "cache_account_audit" in result.failed
    assert any("raw" in issue or "sensitive" in issue or "usage" in issue for issue in result.scenario_results["cache_account_audit"].issues)


def test_cp8_cache_account_audit_rejects_sensitive_extra_artifact_refs(tmp_path: Path):
    fixture_root = tmp_path / "cp8"
    shutil.copytree(FIXTURE_DIR, fixture_root)
    payload = json.loads((fixture_root / "live_matrix_pass.json").read_text(encoding="utf-8"))
    extra = fixture_root / "artifacts" / "cache_account_raw_extra.json"
    extra.write_text(json.dumps({"raw_body": "hello world"}, ensure_ascii=True, sort_keys=True), encoding="utf-8")
    payload["scenarios"]["cache_account_audit"]["artifact_refs"].append(
        {
            "path": "artifacts/cache_account_raw_extra.json",
            "sha256": "sha256:" + hashlib.sha256(extra.read_bytes()).hexdigest(),
            "sensitive_scan_clean": True,
        }
    )

    result = verify_cp8_live_matrix(payload, evidence_root=fixture_root)

    assert result.status == "fail"
    assert "cache_account_audit" in result.failed
    assert any("raw" in issue or "sensitive" in issue for issue in result.scenario_results["cache_account_audit"].issues)


@pytest.mark.parametrize(
    ("filename", "content"),
    (
        ("cache_account_raw_list.json", json.dumps([{"raw_body": "hello world"}], ensure_ascii=True, sort_keys=True)),
        ("cache_account_raw_text.txt", "raw_body: hello world\n"),
        ("cache_account_dot_raw_body.txt", "raw.body: hello world\n"),
        ("cache_account_slash_raw_body.txt", "raw/body: hello world\n"),
        ("cache_account_dot_raw_prompt.txt", "raw.prompt: hello world\n"),
    ),
)
def test_cp8_cache_account_audit_rejects_sensitive_extra_artifact_ref_payload_shapes(tmp_path: Path, filename: str, content: str):
    fixture_root = tmp_path / "cp8"
    shutil.copytree(FIXTURE_DIR, fixture_root)
    payload = json.loads((fixture_root / "live_matrix_pass.json").read_text(encoding="utf-8"))
    extra = fixture_root / "artifacts" / filename
    extra.write_text(content, encoding="utf-8")
    payload["scenarios"]["cache_account_audit"]["artifact_refs"].append(
        {
            "path": "artifacts/" + filename,
            "sha256": "sha256:" + hashlib.sha256(extra.read_bytes()).hexdigest(),
            "sensitive_scan_clean": True,
        }
    )

    result = verify_cp8_live_matrix(payload, evidence_root=fixture_root)

    assert result.status == "fail"
    assert "cache_account_audit" in result.failed
    assert any("raw" in issue or "sensitive" in issue for issue in result.scenario_results["cache_account_audit"].issues)


def test_cp8_strict_live_cache_account_audit_requires_live_usage_fields(tmp_path: Path):
    payload = _fixture("live_matrix_pass.json")
    payload["mode"] = "external_provider_live_matrix"
    run_id = "cp8-cache-strict-live"
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
            "model": _provider_model(provider),
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
            "model": _provider_model(provider),
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
    payload["scenarios"]["cache_account_audit"]["cache_provider_evidence"] = _cache_provider_evidence(strict_live=True)
    payload["scenarios"]["cache_account_audit"]["cache_provider_evidence"]["deepseek"]["live_usage_fields_observed"] = False

    result = verify_cp8_live_matrix(payload, strict_live=True, evidence_root=tmp_path)

    assert result.status == "fail"
    assert "cache_account_audit" in result.failed
    assert any("live usage" in issue for issue in result.scenario_results["cache_account_audit"].issues)



def test_cp8_cache_account_audit_requires_bridge_cache_audit_rows(tmp_path: Path):
    payload = _fixture("live_matrix_pass.json")

    result = verify_cp8_live_matrix(payload, evidence_root=FIXTURE_DIR)

    assert result.status == "pass"
    fixture_root = tmp_path / "cp8"
    shutil.copytree(FIXTURE_DIR, fixture_root)
    copied = json.loads((fixture_root / "live_matrix_pass.json").read_text(encoding="utf-8"))
    artifact_ref = copied["scenarios"]["cache_account_audit"]["artifact_refs"][0]
    artifact = fixture_root / artifact_ref["path"]
    artifact_payload = json.loads(artifact.read_text(encoding="utf-8"))
    artifact_payload.pop("bridge_cache_audit_rows", None)
    artifact.write_text(json.dumps(artifact_payload, ensure_ascii=True, sort_keys=True), encoding="utf-8")
    updated_hash = "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest()
    for scenario in copied["scenarios"].values():
        for ref in scenario.get("artifact_refs", []):
            if ref.get("path") == artifact_ref["path"]:
                ref["sha256"] = updated_hash

    result = verify_cp8_live_matrix(copied, evidence_root=fixture_root)

    assert result.status == "fail"
    assert "cache_account_audit" in result.failed
    assert any("bridge cache audit" in issue for issue in result.scenario_results["cache_account_audit"].issues)


def test_cp8_cache_account_audit_rejects_bridge_cache_audit_row_with_raw_prompt_key(tmp_path: Path):
    fixture_root = tmp_path / "cp8"
    shutil.copytree(FIXTURE_DIR, fixture_root)
    payload = json.loads((fixture_root / "live_matrix_pass.json").read_text(encoding="utf-8"))
    artifact_ref = payload["scenarios"]["cache_account_audit"]["artifact_refs"][0]
    artifact = fixture_root / artifact_ref["path"]
    artifact_payload = json.loads(artifact.read_text(encoding="utf-8"))
    artifact_payload.setdefault("bridge_cache_audit_rows", []).append(
        {
            "schema_version": "claude-code-bridge-cache-audit-row-v1",
            "provider": "deepseek",
            "route": "deepseek_bridge",
            "client_type": "claude_code_bridge_deepseek",
            "selected_protocol": "anthropic_messages",
            "provider_cache_mechanism": "deepseek_prefix_kv",
            "upstream_path_kind": "/anthropic/v1/messages",
            "stable_prefix_hmac": "hmac-sha256:cp8-cache:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
            "stable_prefix_token_bucket": "1k_4k",
            "cache_read_tokens": 7,
            "cache_miss_tokens": 13,
            "cache_control_provider_ignored": True,
            "raw_prompt": "do not store this",
        }
    )
    artifact.write_text(json.dumps(artifact_payload, ensure_ascii=True, sort_keys=True), encoding="utf-8")
    artifact_ref["sha256"] = "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest()

    result = verify_cp8_live_matrix(payload, evidence_root=fixture_root)

    assert result.status == "fail"
    assert "cache_account_audit" in result.failed
    assert any("raw" in issue or "sensitive" in issue for issue in result.scenario_results["cache_account_audit"].issues)


def test_cp8_cache_account_audit_rejects_prompt_cache_key_value_in_bridge_row(tmp_path: Path):
    fixture_root = tmp_path / "cp8"
    shutil.copytree(FIXTURE_DIR, fixture_root)
    payload = json.loads((fixture_root / "live_matrix_pass.json").read_text(encoding="utf-8"))
    artifact_ref = payload["scenarios"]["cache_account_audit"]["artifact_refs"][0]
    artifact = fixture_root / artifact_ref["path"]
    artifact_payload = json.loads(artifact.read_text(encoding="utf-8"))
    artifact_payload["bridge_cache_audit_rows"][1]["prompt_cache_key"] = "session-cache-key-must-not-leak"
    artifact.write_text(json.dumps(artifact_payload, ensure_ascii=True, sort_keys=True), encoding="utf-8")
    updated_hash = "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest()
    for scenario in payload["scenarios"].values():
        for ref in scenario.get("artifact_refs", []):
            if ref.get("path") == artifact_ref["path"]:
                ref["sha256"] = updated_hash

    result = verify_cp8_live_matrix(payload, evidence_root=fixture_root)

    assert result.status == "fail"
    assert "cache_account_audit" in result.failed
    assert any("prompt_cache_key" in issue or "unsupported fields" in issue for issue in result.scenario_results["cache_account_audit"].issues)


def test_cp8_cache_account_audit_rejects_duplicate_bridge_cache_provider_rows(tmp_path: Path):
    fixture_root = tmp_path / "cp8"
    shutil.copytree(FIXTURE_DIR, fixture_root)
    payload = json.loads((fixture_root / "live_matrix_pass.json").read_text(encoding="utf-8"))
    artifact_ref = payload["scenarios"]["cache_account_audit"]["artifact_refs"][0]
    artifact = fixture_root / artifact_ref["path"]
    artifact_payload = json.loads(artifact.read_text(encoding="utf-8"))
    artifact_payload["bridge_cache_audit_rows"].append(dict(artifact_payload["bridge_cache_audit_rows"][0]))
    artifact.write_text(json.dumps(artifact_payload, ensure_ascii=True, sort_keys=True), encoding="utf-8")
    updated_hash = "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest()
    for scenario in payload["scenarios"].values():
        for ref in scenario.get("artifact_refs", []):
            if ref.get("path") == artifact_ref["path"]:
                ref["sha256"] = updated_hash

    result = verify_cp8_live_matrix(payload, evidence_root=fixture_root)

    assert result.status == "fail"
    assert "cache_account_audit" in result.failed
    assert any("duplicate" in issue for issue in result.scenario_results["cache_account_audit"].issues)


@pytest.mark.parametrize(
    ("row_index", "field", "value"),
    (
        (0, "fallback_reason", "raw prompt body should not pass"),
        (0, "model_id", "raw response body should not pass"),
        (1, "fallback_reason", "Authorization header should not pass"),
        (0, "model_id", "rawpromptbody"),
        (0, "fallback_reason", "anthropic_rawpromptbody_fixture_failed"),
    ),
)
def test_cp8_cache_account_audit_rejects_sensitive_allowed_string_values(tmp_path: Path, row_index: int, field: str, value: str):
    fixture_root = tmp_path / "cp8"
    shutil.copytree(FIXTURE_DIR, fixture_root)
    payload = json.loads((fixture_root / "live_matrix_pass.json").read_text(encoding="utf-8"))
    artifact_ref = payload["scenarios"]["cache_account_audit"]["artifact_refs"][0]
    artifact = fixture_root / artifact_ref["path"]
    artifact_payload = json.loads(artifact.read_text(encoding="utf-8"))
    artifact_payload["bridge_cache_audit_rows"][row_index][field] = value
    artifact.write_text(json.dumps(artifact_payload, ensure_ascii=True, sort_keys=True), encoding="utf-8")
    updated_hash = "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest()
    for scenario in payload["scenarios"].values():
        for ref in scenario.get("artifact_refs", []):
            if ref.get("path") == artifact_ref["path"]:
                ref["sha256"] = updated_hash

    result = verify_cp8_live_matrix(payload, evidence_root=fixture_root)

    assert result.status == "fail"
    assert "cache_account_audit" in result.failed
    assert any("unsafe" in issue or "safe" in issue or "wrong" in issue or "sensitive" in issue for issue in result.scenario_results["cache_account_audit"].issues)


def test_cp8_cache_account_audit_rejects_non_enum_cache_control_location(tmp_path: Path):
    fixture_root = tmp_path / "cp8"
    shutil.copytree(FIXTURE_DIR, fixture_root)
    payload = json.loads((fixture_root / "live_matrix_pass.json").read_text(encoding="utf-8"))
    artifact_ref = payload["scenarios"]["cache_account_audit"]["artifact_refs"][0]
    artifact = fixture_root / artifact_ref["path"]
    artifact_payload = json.loads(artifact.read_text(encoding="utf-8"))
    artifact_payload["bridge_cache_audit_rows"][0]["cache_control_locations"].append("raw prompt body")
    artifact.write_text(json.dumps(artifact_payload, ensure_ascii=True, sort_keys=True), encoding="utf-8")
    updated_hash = "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest()
    for scenario in payload["scenarios"].values():
        for ref in scenario.get("artifact_refs", []):
            if ref.get("path") == artifact_ref["path"]:
                ref["sha256"] = updated_hash

    result = verify_cp8_live_matrix(payload, evidence_root=fixture_root)

    assert result.status == "fail"
    assert "cache_account_audit" in result.failed
    assert any("cache_control_locations" in issue or "location" in issue for issue in result.scenario_results["cache_account_audit"].issues)


@pytest.mark.parametrize(
    ("field", "value"),
    (
        ("cache_control_locations", ["history", {"safe": "raw prompt body response header prompt_cache_key value"}]),
        ("cache_usage_fields", ["prompt_cache_hit_tokens", "prompt_cache_miss_tokens", {"safe": "raw prompt body"}]),
        ("cache_read_tokens", {"safe": "raw prompt body"}),
        ("prompt_cache_key_strategy", "rawpromptbody"),
    ),
)
def test_cp8_cache_account_audit_rejects_nested_sensitive_values_in_allowed_fields(tmp_path: Path, field: str, value: object):
    fixture_root = tmp_path / "cp8"
    shutil.copytree(FIXTURE_DIR, fixture_root)
    payload = json.loads((fixture_root / "live_matrix_pass.json").read_text(encoding="utf-8"))
    artifact_ref = payload["scenarios"]["cache_account_audit"]["artifact_refs"][0]
    artifact = fixture_root / artifact_ref["path"]
    artifact_payload = json.loads(artifact.read_text(encoding="utf-8"))
    artifact_payload["bridge_cache_audit_rows"][0][field] = value
    artifact.write_text(json.dumps(artifact_payload, ensure_ascii=True, sort_keys=True), encoding="utf-8")
    updated_hash = "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest()
    for scenario in payload["scenarios"].values():
        for ref in scenario.get("artifact_refs", []):
            if ref.get("path") == artifact_ref["path"]:
                ref["sha256"] = updated_hash

    result = verify_cp8_live_matrix(payload, evidence_root=fixture_root)

    assert result.status == "fail"
    assert "cache_account_audit" in result.failed
    assert any("schema" in issue or "unsafe" in issue or "list" in issue or "integer" in issue or "sensitive" in issue or "usage" in issue for issue in result.scenario_results["cache_account_audit"].issues)


def test_cp8_cache_account_audit_rejects_deepseek_prompt_cache_key_or_cached_tokens_claim(tmp_path: Path):
    fixture_root = tmp_path / "cp8"
    shutil.copytree(FIXTURE_DIR, fixture_root)
    payload = json.loads((fixture_root / "live_matrix_pass.json").read_text(encoding="utf-8"))
    artifact_ref = payload["scenarios"]["cache_account_audit"]["artifact_refs"][0]
    artifact = fixture_root / artifact_ref["path"]
    artifact_payload = json.loads(artifact.read_text(encoding="utf-8"))
    deepseek = _bridge_cache_row(artifact_payload["bridge_cache_audit_rows"], "deepseek")
    deepseek["prompt_cache_key_present"] = True
    deepseek["cached_tokens"] = 3
    artifact.write_text(json.dumps(artifact_payload, ensure_ascii=True, sort_keys=True), encoding="utf-8")
    updated_hash = "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest()
    for scenario in payload["scenarios"].values():
        for ref in scenario.get("artifact_refs", []):
            if ref.get("path") == artifact_ref["path"]:
                ref["sha256"] = updated_hash

    result = verify_cp8_live_matrix(payload, evidence_root=fixture_root)

    assert result.status == "fail"
    assert "cache_account_audit" in result.failed
    assert any("DeepSeek" in issue for issue in result.scenario_results["cache_account_audit"].issues)


@pytest.mark.parametrize(
    ("row_index", "field", "value"),
    (
        (0, "preferred_protocol", "responses"),
        (0, "fallback_protocol", "responses"),
        (1, "preferred_protocol", "anthropic_messages"),
        (1, "fallback_protocol", "anthropic_messages"),
    ),
)
def test_cp8_cache_account_audit_rejects_provider_protocol_drift(tmp_path: Path, row_index: int, field: str, value: str):
    fixture_root = tmp_path / "cp8"
    shutil.copytree(FIXTURE_DIR, fixture_root)
    payload = json.loads((fixture_root / "live_matrix_pass.json").read_text(encoding="utf-8"))
    artifact_ref = payload["scenarios"]["cache_account_audit"]["artifact_refs"][0]
    artifact = fixture_root / artifact_ref["path"]
    artifact_payload = json.loads(artifact.read_text(encoding="utf-8"))
    artifact_payload["bridge_cache_audit_rows"][row_index][field] = value
    artifact.write_text(json.dumps(artifact_payload, ensure_ascii=True, sort_keys=True), encoding="utf-8")
    updated_hash = "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest()
    for scenario in payload["scenarios"].values():
        for ref in scenario.get("artifact_refs", []):
            if ref.get("path") == artifact_ref["path"]:
                ref["sha256"] = updated_hash

    result = verify_cp8_live_matrix(payload, evidence_root=fixture_root)

    assert result.status == "fail"
    assert "cache_account_audit" in result.failed
    assert any("protocol" in issue for issue in result.scenario_results["cache_account_audit"].issues)


def test_cp8_cache_account_audit_rejects_deepseek_fallback_row_as_kv_cache_evidence(tmp_path: Path):
    fixture_root = tmp_path / "cp8"
    shutil.copytree(FIXTURE_DIR, fixture_root)
    payload = json.loads((fixture_root / "live_matrix_pass.json").read_text(encoding="utf-8"))
    artifact_ref = payload["scenarios"]["cache_account_audit"]["artifact_refs"][0]
    artifact = fixture_root / artifact_ref["path"]
    artifact_payload = json.loads(artifact.read_text(encoding="utf-8"))
    deepseek = _bridge_cache_row(artifact_payload["bridge_cache_audit_rows"], "deepseek")
    deepseek["fallback_used"] = True
    deepseek["fallback_reason"] = "anthropic_cache_fixture_failed"
    deepseek["upstream_path_kind"] = "/v1/chat/completions"
    deepseek["selected_protocol"] = "openai_chat_completions"
    deepseek["provider_cache_mechanism"] = "none"
    deepseek["cache_control_provider_ignored"] = False
    artifact.write_text(json.dumps(artifact_payload, ensure_ascii=True, sort_keys=True), encoding="utf-8")
    updated_hash = "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest()
    for scenario in payload["scenarios"].values():
        for ref in scenario.get("artifact_refs", []):
            if ref.get("path") == artifact_ref["path"]:
                ref["sha256"] = updated_hash

    result = verify_cp8_live_matrix(payload, evidence_root=fixture_root)

    assert result.status == "fail"
    assert "cache_account_audit" in result.failed
    assert any("fallback" in issue or "anthropic_messages" in issue for issue in result.scenario_results["cache_account_audit"].issues)


def test_cp8_cache_account_audit_rejects_deepseek_fallback_used_even_with_anthropic_path(tmp_path: Path):
    fixture_root = tmp_path / "cp8"
    shutil.copytree(FIXTURE_DIR, fixture_root)
    payload = json.loads((fixture_root / "live_matrix_pass.json").read_text(encoding="utf-8"))
    artifact_ref = payload["scenarios"]["cache_account_audit"]["artifact_refs"][0]
    artifact = fixture_root / artifact_ref["path"]
    artifact_payload = json.loads(artifact.read_text(encoding="utf-8"))
    deepseek = _bridge_cache_row(artifact_payload["bridge_cache_audit_rows"], "deepseek")
    deepseek["fallback_used"] = True
    deepseek["fallback_reason"] = "anthropic_cache_fixture_failed"
    artifact.write_text(json.dumps(artifact_payload, ensure_ascii=True, sort_keys=True), encoding="utf-8")
    updated_hash = "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest()
    for scenario in payload["scenarios"].values():
        for ref in scenario.get("artifact_refs", []):
            if ref.get("path") == artifact_ref["path"]:
                ref["sha256"] = updated_hash

    result = verify_cp8_live_matrix(payload, evidence_root=fixture_root)

    assert result.status == "fail"
    assert "cache_account_audit" in result.failed
    assert any("fallback" in issue for issue in result.scenario_results["cache_account_audit"].issues)

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
            "model": _provider_model(provider),
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
            "model": _provider_model(provider),
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
    assert any("provider binding" in issue for issue in result.scenario_results["claude_native"].issues)


def test_cp8_strict_live_rejects_extra_live_provider_without_scope_extension(tmp_path: Path):
    payload = _fixture("live_matrix_pass.json")
    payload["mode"] = "external_provider_live_matrix"
    run_id = "cp8-live-extra-provider"
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
            "model": _provider_model(provider),
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
            "model": _provider_model(provider),
            "artifact_refs": [
                {
                    "path": f"artifacts/{provider}_live.json",
                    "sha256": "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest(),
                    "sensitive_scan_clean": True,
                }
            ],
        }
    payload["live_provenance"]["providers"]["agnes"] = {
        "credential_scope": "bridge_pool",
        "live_provider_verified": True,
        "endpoint": "https://api.agnes.example/v1/responses",
        "model": "agnes-2.0-flash",
        "artifact_refs": [],
    }
    for scenario in payload["scenarios"].values():
        scenario["live_provider_verified"] = True
    _add_strict_live_scenario_artifacts(payload, tmp_path, run_id)

    result = verify_cp8_live_matrix(payload, strict_live=True, evidence_root=tmp_path)

    assert result.status == "fail"
    assert result.release_gate == "blocked_missing_external_live"
    assert "live_provenance" in result.failed


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
            "model": _provider_model(provider),
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
            "model": _provider_model(provider),
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


def test_cp8_strict_live_rejects_unallowlisted_or_missing_provider_bound_models(tmp_path: Path):
    payload = _fixture("live_matrix_pass.json")
    payload["mode"] = "external_provider_live_matrix"
    run_id = "cp8-live-model-binding"
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
        model = "gpt-5.4" if provider == "openai" else _provider_model(provider)
        proof = {
            "schema_version": "cp8-live-provider-provenance-v1",
            "checkpoint": "CP8",
            "run_id": run_id,
            "provider": provider,
            "model": model,
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
            "model": model,
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
            "model": _provider_model(provider),
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
            "model": _provider_model(provider),
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
            "model": _provider_model(provider),
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
            "model": _provider_model(provider),
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
            "model": _provider_model(provider),
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
            "model": _provider_model(provider),
            "artifact_refs": [{"path": f"artifacts/{provider}_live.json", "sha256": "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest(), "sensitive_scan_clean": True}],
        }
    for scenario in payload["scenarios"].values():
        scenario["live_provider_verified"] = True
    _add_strict_live_scenario_artifacts(payload, tmp_path, run_id)

    result = verify_cp8_live_matrix(payload, strict_live=True, evidence_root=tmp_path)

    assert result.status == "fail"
    assert "live_provenance" in result.failed
    assert "gpt_bridge" in result.failed
    assert any("provider binding" in issue for issue in result.scenario_results["gpt_bridge"].issues)

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
        collect_cp8_sub2api_gateway_live_provenance,
        assemble_cp8_external_live_matrix_evidence as exported_assemble,
        verify_cp8_live_matrix as exported_verify,
        write_cp8_live_scenario_evidence as exported_write_scenario,
    )
    from zhumeng_agent.adapters.claude_code.live_matrix import collect_cp8_live_provider_provenance  # noqa: PLC0415
    from zhumeng_agent.adapters.claude_code.live_matrix import collect_cp8_sub2api_gateway_live_provenance  # noqa: PLC0415
    from zhumeng_agent.adapters.claude_code.live_matrix import write_cp8_live_scenario_evidence  # noqa: PLC0415

    assert ExportedError is CP8LiveMatrixError
    assert ExportedScenarios is REQUIRED_CP8_SCENARIOS
    assert exported_verify is verify_cp8_live_matrix
    assert exported_collect is collect_cp8_live_provider_provenance
    assert collect_cp8_sub2api_gateway_live_provenance is not None
    assert exported_assemble is assemble_cp8_external_live_matrix_evidence
    assert exported_write_scenario is write_cp8_live_scenario_evidence


def test_cp8_sub2api_gateway_live_collector_uses_single_gateway_token_and_routes(tmp_path: Path):
    from zhumeng_agent.adapters.claude_code.live_matrix import collect_cp8_sub2api_gateway_live_provenance  # noqa: PLC0415

    calls: list[tuple[str, str, str]] = []

    def transport(provider: str, endpoint: str, request: dict[str, object]) -> dict[str, object]:
        headers = request["headers"]
        assert isinstance(headers, dict)
        calls.append((provider, endpoint, headers["Authorization"]))
        return {
            "status": 200,
            "request_id": f"sub2api_{provider}_upstream_req",
            "response_headers": {"x-sub2api-route": f"claude_code_{'native' if provider == 'claude' else 'bridge_' + provider}"},
        }

    provenance = collect_cp8_sub2api_gateway_live_provenance(
        run_id="cp8-sub2api-live",
        output_root=tmp_path,
        base_url="http://127.0.0.1:3012",
        gateway_token="sub2api-gateway-session-token",
        native_attestation_secret="native-attestation-secret",
        route_hint_secret="route-hint-secret",
        runtime_hash="sha256:" + "1" * 64,
        overlay_hash="sha256:" + "2" * 64,
        catalog_hash="sha256:" + "3" * 64,
        catalog_version="cp8-live-catalog",
        transport=transport,
    )

    assert provenance["credential_backed"] is True
    assert provenance["loopback_only"] is False
    assert provenance["mode"] == "sub2api_gateway_live_matrix"
    assert provenance["gateway_base_url"] == "http://127.0.0.1:3012"
    assert {call[2] for call in calls} == {"Bearer sub2api-gateway-session-token"}
    assert {call[1] for call in calls} == {
        "http://127.0.0.1:3012/v1/messages",
    }
    assert provenance["providers"]["claude"]["endpoint"] == "http://127.0.0.1:3012/v1/messages"
    assert provenance["providers"]["claude"]["route"] == "claude_code_native"
    assert provenance["providers"]["openai"]["route"] == "openai_bridge"
    assert provenance["providers"]["openai"]["client_type"] == "claude_code_bridge_openai"
    assert provenance["providers"]["deepseek"]["route"] == "deepseek_bridge"
    assert provenance["providers"]["deepseek"]["client_type"] == "claude_code_bridge_deepseek"

    for provider in ("claude", "openai", "deepseek"):
        ref = provenance["providers"][provider]["artifact_refs"][0]
        artifact_text = (tmp_path / ref["path"]).read_text(encoding="utf-8")
        assert "sub2api-gateway-session-token" not in artifact_text
        assert "api.openai.com" not in artifact_text
        assert "api.deepseek.com" not in artifact_text
        assert "api.anthropic.com" not in artifact_text



def test_cp8_sub2api_gateway_collector_sends_managed_native_headers_for_claude_probe(tmp_path: Path):
    from zhumeng_agent.adapters.claude_code.live_matrix import collect_cp8_sub2api_gateway_live_provenance  # noqa: PLC0415

    seen_headers: dict[str, dict[str, str]] = {}

    def transport(provider: str, endpoint: str, request: dict[str, object]) -> dict[str, object]:
        headers = request["headers"]
        assert isinstance(headers, dict)
        seen_headers[provider] = {str(key): str(value) for key, value in headers.items()}
        return {"status": 200, "request_id": f"sub2api_{provider}_managed_req"}

    collect_cp8_sub2api_gateway_live_provenance(
        run_id="cp8-sub2api-managed-native",
        output_root=tmp_path,
        base_url="http://127.0.0.1:3017",
        gateway_token="sub2api-gateway-token",
        native_attestation_secret="native-attestation-secret",
        route_hint_secret="route-hint-secret",
        runtime_hash="sha256:" + "1" * 64,
        overlay_hash="sha256:" + "2" * 64,
        catalog_hash="sha256:" + "3" * 64,
        catalog_version="cp8-live-catalog",
        managed_session_id="managed-session-cp8",
        device_id=42,
        transport=transport,
    )

    assert seen_headers["claude"]["X-Zhumeng-Managed-Session"] == "managed-session-cp8"
    assert seen_headers["claude"]["X-Zhumeng-Device-ID"] == "42"
    assert seen_headers["claude"]["X-Zhumeng-Agent-Version"]
    encoded = seen_headers["claude"]["x-sub2api-native-attestation"]
    padded = encoded + "=" * (-len(encoded) % 4)
    payload = json.loads(base64.urlsafe_b64decode(padded.encode("ascii")).decode("utf-8"))
    assert payload["scope"] == "claude_code_native_takeover"
    assert payload["replay_safety_boundary"] == "replay_safe_anthropic_transcript"
    assert payload["replay_safety_applied"] is True
    assert payload["replay_safety_sanitized"] is False
    assert payload["replay_safety_forbidden_paths_count"] == 0
    assert payload["replay_safety_body_shape_hash"] == payload["body_shape_hash"]
    for provider in ("openai", "deepseek"):
        assert "X-Zhumeng-Managed-Session" not in seen_headers[provider]
        assert "X-Zhumeng-Device-ID" not in seen_headers[provider]


def test_cp8_sub2api_gateway_collector_accepts_claude_code_bridge_display_models_for_sub2api_mode(tmp_path: Path, monkeypatch):
    from zhumeng_agent.adapters.claude_code.live_matrix import collect_cp8_sub2api_gateway_live_provenance  # noqa: PLC0415

    monkeypatch.setenv("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_LIVE_MODEL", "claude-code-bridge-gpt-5.5")
    monkeypatch.setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_MODEL", "claude-code-bridge-deepseek-v4-pro")
    seen_bodies: dict[str, dict[str, object]] = {}

    def transport(provider: str, endpoint: str, request: dict[str, object]) -> dict[str, object]:
        body = request["body"]
        assert isinstance(body, dict)
        seen_bodies[provider] = body
        return {"status": 200, "request_id": f"sub2api_{provider}_display_req"}

    provenance = collect_cp8_sub2api_gateway_live_provenance(
        run_id="cp8-sub2api-display-models",
        output_root=tmp_path,
        base_url="http://127.0.0.1:3017",
        gateway_token="sub2api-gateway-token",
        native_attestation_secret="native-attestation-secret",
        route_hint_secret="route-hint-secret",
        runtime_hash="sha256:" + "1" * 64,
        overlay_hash="sha256:" + "2" * 64,
        catalog_hash="sha256:" + "3" * 64,
        catalog_version="cp8-live-catalog",
        transport=transport,
    )

    assert seen_bodies["openai"]["model"] == "claude-code-bridge-gpt-5.5"
    assert seen_bodies["deepseek"]["model"] == "claude-code-bridge-deepseek-v4-pro"
    assert provenance["providers"]["openai"]["model"] == "gpt-5.5"
    assert provenance["providers"]["deepseek"]["model"] == "deepseek-v4-pro"
    for provider in ("openai", "deepseek"):
        artifact = json.loads((tmp_path / provenance["providers"][provider]["artifact_refs"][0]["path"]).read_text(encoding="utf-8"))
        assert artifact["model"] == provenance["providers"][provider]["model"]
        assert artifact["request_model"] == seen_bodies[provider]["model"]


def test_cp8_sub2api_gateway_live_collector_rejects_official_provider_hosts(tmp_path: Path):
    from zhumeng_agent.adapters.claude_code.live_matrix import collect_cp8_sub2api_gateway_live_provenance  # noqa: PLC0415

    for base_url in ("https://api.openai.com", "https://api.openai.com.", "https://api.anthropic.com./"):
        with pytest.raises(CP8LiveMatrixError, match="Sub2API gateway base URL"):
            collect_cp8_sub2api_gateway_live_provenance(
                run_id="cp8-sub2api-official-host",
                output_root=tmp_path,
                base_url=base_url,
                gateway_token="sub2api-token",
                transport=lambda provider, endpoint, credential: {"status": 200, "request_id": f"req_{provider}"},
            )


def test_cp8_sub2api_gateway_live_collector_rejects_sensitive_or_non_origin_base_urls(tmp_path: Path):
    from zhumeng_agent.adapters.claude_code.live_matrix import collect_cp8_sub2api_gateway_live_provenance  # noqa: PLC0415

    for base_url in (
        "http://user:pass@127.0.0.1:3012",
        "http://127.0.0.1:3012?token=leak",
        "http://127.0.0.1:3012#frag",
    ):
        with pytest.raises(CP8LiveMatrixError, match="Sub2API gateway base URL"):
            collect_cp8_sub2api_gateway_live_provenance(
                run_id="cp8-sub2api-sensitive-base",
                output_root=tmp_path,
                base_url=base_url,
                gateway_token="sub2api-token",
                native_attestation_secret="native-attestation-secret",
                route_hint_secret="route-hint-secret",
                runtime_hash="sha256:" + "1" * 64,
                overlay_hash="sha256:" + "2" * 64,
                catalog_hash="sha256:" + "3" * 64,
                catalog_version="cp8-live-catalog",
                transport=lambda provider, endpoint, request: {"status": 200, "request_id": f"req_{provider}"},
            )


def test_cp8_sub2api_gateway_live_collector_requires_runtime_trust_secrets(tmp_path: Path, monkeypatch):
    from zhumeng_agent.adapters.claude_code.live_matrix import collect_cp8_sub2api_gateway_live_provenance  # noqa: PLC0415

    monkeypatch.delenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", raising=False)
    monkeypatch.delenv("SUB2API_CLAUDE_CODE_ROUTE_HINT_SECRET", raising=False)

    with pytest.raises(CP8LiveMatrixError, match="native attestation secret"):
        collect_cp8_sub2api_gateway_live_provenance(
            run_id="cp8-sub2api-missing-trust",
            output_root=tmp_path,
            base_url="http://127.0.0.1:3012",
            gateway_token="sub2api-gateway-token",
            transport=lambda provider, endpoint, request: {"status": 200, "request_id": f"req_{provider}"},
        )


def test_cp8_sub2api_gateway_live_collector_sends_gateway_auth_and_signed_runtime_headers(tmp_path: Path):
    from zhumeng_agent.adapters.claude_code.live_matrix import collect_cp8_sub2api_gateway_live_provenance  # noqa: PLC0415

    calls: list[tuple[str, str, dict[str, str], dict[str, object]]] = []

    def transport(provider: str, endpoint: str, request: dict[str, object]) -> dict[str, object]:
        headers = request["headers"]
        body = request["body"]
        assert isinstance(headers, dict)
        assert isinstance(body, dict)
        calls.append((provider, endpoint, headers, body))
        return {"status": 200, "request_id": f"sub2api_{provider}_header_req"}

    collect_cp8_sub2api_gateway_live_provenance(
        run_id="cp8-sub2api-live-headers",
        output_root=tmp_path,
        base_url="http://127.0.0.1:3012",
        gateway_token="sub2api-gateway-token",
        native_attestation_secret="native-attestation-secret",
        route_hint_secret="route-hint-secret",
        runtime_hash="sha256:" + "1" * 64,
        overlay_hash="sha256:" + "2" * 64,
        catalog_hash="sha256:" + "3" * 64,
        catalog_version="cp8-live-catalog",
        transport=transport,
    )

    by_provider = {provider: (endpoint, headers, body) for provider, endpoint, headers, body in calls}
    assert by_provider["claude"][1]["Authorization"] == "Bearer sub2api-gateway-token"
    assert by_provider["openai"][1]["Authorization"] == "Bearer sub2api-gateway-token"
    assert by_provider["deepseek"][1]["Authorization"] == "Bearer sub2api-gateway-token"
    assert by_provider["claude"][1]["x-sub2api-client-type"] == "claude_code_native"
    assert by_provider["claude"][1]["x-sub2api-route"] == "claude_code_native"
    assert by_provider["claude"][1]["x-sub2api-native-attestation"]
    assert by_provider["claude"][1]["x-sub2api-native-signature"]
    assert by_provider["claude"][1]["User-Agent"].startswith("Claude-Code/")
    assert "claude-code-20250219" in by_provider["claude"][1]["anthropic-beta"]
    assert by_provider["claude"][1]["x-claude-code-session-id"].startswith("cp8-")
    assert by_provider["openai"][1]["x-sub2api-client-type"] == "claude_code_bridge_openai"
    assert by_provider["openai"][1]["x-sub2api-route"] == "openai_bridge"
    assert by_provider["openai"][1]["x-zhumeng-claude-code-route-hint"]
    assert by_provider["openai"][1]["x-zhumeng-claude-code-route-signature"]
    assert by_provider["deepseek"][1]["x-sub2api-client-type"] == "claude_code_bridge_deepseek"
    assert by_provider["deepseek"][1]["x-sub2api-route"] == "deepseek_bridge"
    assert by_provider["deepseek"][1]["x-zhumeng-claude-code-route-hint"]
    assert by_provider["deepseek"][1]["x-zhumeng-claude-code-route-signature"]
    claude_session_ref = by_provider["claude"][1]["x-sub2api-local-session-ref"]
    assert claude_session_ref.startswith("hmac-sha256:")
    assert "cp8-sub2api-live-headers" not in claude_session_ref
    native_payload = _decode_cp8_runtime_header(by_provider["claude"][1]["x-sub2api-native-attestation"])
    assert native_payload["local_session_ref"] == claude_session_ref
    assert native_payload["session_ref"] == claude_session_ref
    assert native_payload["replay_safety_boundary"] == "replay_safe_anthropic_transcript"
    assert native_payload["replay_safety_applied"] is True
    assert native_payload["replay_safety_sanitized"] is False
    assert native_payload["replay_safety_forbidden_paths_count"] == 0
    assert native_payload["replay_safety_body_shape_hash"] == native_payload["body_shape_hash"]
    assert "cp8-sub2api-live-headers" not in json.dumps(native_payload, sort_keys=True)
    for provider in ("openai", "deepseek"):
        route_payload = _decode_cp8_runtime_header(by_provider[provider][1]["x-zhumeng-claude-code-route-hint"])
        assert route_payload["session_ref"] == claude_session_ref
        assert route_payload["formal_pool_allowed"] is False
        assert route_payload["native_attestation_allowed"] is False
    assert by_provider["openai"][2]["model"] == "claude-code-bridge-gpt-5.5"
    assert by_provider["deepseek"][2]["model"] == "claude-code-bridge-deepseek-v4-pro"
    assert by_provider["claude"][2]["model"] == "claude-haiku-4-5-20251001"


def test_cp8_sub2api_gateway_live_collector_extracts_sub2api_and_provider_request_id_headers(tmp_path: Path):
    from zhumeng_agent.adapters.claude_code.live_matrix import collect_cp8_sub2api_gateway_live_provenance  # noqa: PLC0415

    def transport(provider: str, endpoint: str, request: dict[str, object]) -> dict[str, object]:
        header_by_provider = {
            "claude": {"x-sub2api-upstream-request-id": "sub2api_claude_upstream"},
            "openai": {"x-sub2api-request-id": "sub2api_openai_gateway"},
            "deepseek": {"x-deepseek-request-id": "deepseek_provider_req"},
        }
        return {"status": 200, "response_headers": header_by_provider[provider]}

    provenance = collect_cp8_sub2api_gateway_live_provenance(
        run_id="cp8-sub2api-live-request-id-headers",
        output_root=tmp_path,
        base_url="http://127.0.0.1:3012",
        gateway_token="sub2api-gateway-token",
        native_attestation_secret="native-attestation-secret",
        route_hint_secret="route-hint-secret",
        runtime_hash="sha256:" + "1" * 64,
        overlay_hash="sha256:" + "2" * 64,
        catalog_hash="sha256:" + "3" * 64,
        catalog_version="cp8-live-catalog",
        transport=transport,
    )

    assert "sub2api_claude_upstream" in (tmp_path / provenance["providers"]["claude"]["artifact_refs"][0]["path"]).read_text(encoding="utf-8")
    assert "sub2api_openai_gateway" in (tmp_path / provenance["providers"]["openai"]["artifact_refs"][0]["path"]).read_text(encoding="utf-8")
    assert "deepseek_provider_req" in (tmp_path / provenance["providers"]["deepseek"]["artifact_refs"][0]["path"]).read_text(encoding="utf-8")



def test_cp8_strict_live_accepts_sub2api_gateway_provenance_only_in_sub2api_mode(tmp_path: Path):
    from zhumeng_agent.adapters.claude_code.live_matrix import collect_cp8_sub2api_gateway_live_provenance  # noqa: PLC0415

    run_id = "cp8-sub2api-strict-live"
    provenance = collect_cp8_sub2api_gateway_live_provenance(
        run_id=run_id,
        output_root=tmp_path,
        base_url="http://127.0.0.1:3012",
        gateway_token="sub2api-gateway-token",
        native_attestation_secret="native-attestation-secret",
        route_hint_secret="route-hint-secret",
        runtime_hash="sha256:" + "1" * 64,
        overlay_hash="sha256:" + "2" * 64,
        catalog_hash="sha256:" + "3" * 64,
        catalog_version="cp8-live-catalog",
        transport=lambda provider, endpoint, request: {"status": 200, "request_id": f"req_{provider}_sub2api"},
    )
    payload = _fixture("live_matrix_pass.json")
    payload["mode"] = "external_provider_live_matrix"
    payload["live_provenance"] = provenance
    for scenario in payload["scenarios"].values():
        scenario["live_provider_verified"] = True
    _add_sub2api_strict_live_scenario_artifacts(payload, tmp_path, run_id)

    result = verify_cp8_live_matrix(payload, strict_live=True, evidence_root=tmp_path)
    assert result.status == "pass"
    assert result.release_gate == "external_live_passed"

    forged = json.loads(json.dumps(payload))
    forged["live_provenance"].pop("mode")
    forged_result = verify_cp8_live_matrix(forged, strict_live=True, evidence_root=tmp_path)
    assert forged_result.status == "fail"
    assert forged_result.release_gate == "blocked_missing_external_live"
    assert "live_provenance" in forged_result.failed


def test_cp8_strict_live_accepts_multi_provider_scenario_artifacts_with_provider_specific_routes(tmp_path: Path):
    from zhumeng_agent.adapters.claude_code.live_matrix import collect_cp8_sub2api_gateway_live_provenance  # noqa: PLC0415

    run_id = "cp8-provider-specific-scenario-routes"
    provenance = collect_cp8_sub2api_gateway_live_provenance(
        run_id=run_id,
        output_root=tmp_path,
        base_url="http://127.0.0.1:3012",
        gateway_token="sub2api-gateway-token",
        native_attestation_secret="native-attestation-secret",
        route_hint_secret="route-hint-secret",
        runtime_hash="sha256:" + "1" * 64,
        overlay_hash="sha256:" + "2" * 64,
        catalog_hash="sha256:" + "3" * 64,
        catalog_version="cp8-live-catalog",
        transport=lambda provider, endpoint, request: {"status": 200, "request_id": f"req_{provider}_sub2api"},
    )
    payload = _fixture("live_matrix_pass.json")
    payload["mode"] = "external_provider_live_matrix"
    payload["live_provenance"] = provenance
    for scenario in payload["scenarios"].values():
        scenario["live_provider_verified"] = True
    _add_sub2api_strict_live_scenario_artifacts(payload, tmp_path, run_id)
    manual_refs = payload["scenarios"]["manual_provider_switch"]["artifact_refs"]
    assert len(manual_refs) == 3
    routes_by_provider = {
        json.loads((tmp_path / ref["path"]).read_text(encoding="utf-8"))["provider"]: (
            json.loads((tmp_path / ref["path"]).read_text(encoding="utf-8"))["route"],
            json.loads((tmp_path / ref["path"]).read_text(encoding="utf-8"))["client_type"],
        )
        for ref in manual_refs
    }
    assert routes_by_provider == {
        provider: _provider_route_client(provider)
        for provider in ("claude", "openai", "deepseek")
    }

    result = verify_cp8_live_matrix(payload, strict_live=True, evidence_root=tmp_path)

    assert result.status == "pass"
    assert result.release_gate == "external_live_passed"


def test_cp8_strict_live_rejects_sub2api_mode_with_official_provider_endpoints(tmp_path: Path):
    payload = _fixture("live_matrix_pass.json")
    payload["mode"] = "external_provider_live_matrix"
    run_id = "cp8-sub2api-official-forgery"
    payload["live_provenance"] = {
        "mode": "sub2api_gateway_live_matrix",
        "credential_backed": True,
        "loopback_only": False,
        "gateway_base_url": "http://127.0.0.1:3012",
        "run_id": run_id,
        "providers": {},
    }
    artifacts_dir = tmp_path / "artifacts"
    artifacts_dir.mkdir()
    for provider, (scope, endpoint, route, client_type) in {
        "claude": ("formal_pool", "https://api.anthropic.com/v1/messages", "claude_code_native", "claude_code_native"),
        "openai": ("bridge_pool", "https://api.openai.com/v1/responses", "openai_bridge", "claude_code_bridge_openai"),
        "deepseek": ("bridge_pool", "https://api.deepseek.com/anthropic/v1/messages", "deepseek_bridge", "claude_code_bridge_deepseek"),
    }.items():
        artifact = artifacts_dir / f"{provider}_sub2api_forged.json"
        artifact.write_text(json.dumps({
            "schema_version": "cp8-live-provider-provenance-v1",
            "checkpoint": "CP8",
            "run_id": run_id,
            "provider": provider,
            "model": _provider_model(provider),
            "credential_scope": scope,
            "endpoint": endpoint,
            "host": endpoint.split("/")[2],
            "route": route,
            "client_type": client_type,
            "sub2api_gateway_verified": True,
            "external_live_verified": True,
            "loopback": False,
            "response_status": 200,
            "upstream_request_id": f"req_{provider}_sub2api",
        }, ensure_ascii=True, sort_keys=True), encoding="utf-8")
        payload["live_provenance"]["providers"][provider] = {
            "credential_scope": scope,
            "live_provider_verified": True,
            "endpoint": endpoint,
            "model": _provider_model(provider),
            "route": route,
            "client_type": client_type,
            "artifact_refs": [{
                "path": f"artifacts/{artifact.name}",
                "sha256": "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest(),
                "sensitive_scan_clean": True,
            }],
        }
    for scenario in payload["scenarios"].values():
        scenario["live_provider_verified"] = True
    _add_strict_live_scenario_artifacts(payload, tmp_path, run_id)

    result = verify_cp8_live_matrix(payload, strict_live=True, evidence_root=tmp_path)
    assert result.status == "fail"
    assert "live_provenance" in result.failed



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
    monkeypatch.setenv("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_LIVE_MODEL", "gpt-5.5")
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
        "model": "gpt-5.5",
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


def test_cp8_live_evidence_default_transport_rejects_unverified_openai_live_model(monkeypatch, tmp_path: Path):
    from zhumeng_agent.adapters.claude_code.live_matrix import collect_cp8_live_provider_provenance  # noqa: PLC0415

    monkeypatch.setenv("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_LIVE_MODEL", "gpt-5.4")

    with pytest.raises(CP8LiveMatrixError, match="not CP4 live-catalog verified"):
        collect_cp8_live_provider_provenance(
            run_id="cp8-openai-live-model-drift",
            output_root=tmp_path,
            credentials={
                "claude": "anthropic-live-key",
                "openai": "openai-live-key",
                "deepseek": "deepseek-live-key",
            },
            transport=lambda provider, endpoint, credential: {"status": 200, "request_id": f"req_{provider}"},
        )


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
    refs = []
    for provider, endpoint in (
        ("claude", "https://api.anthropic.com/v1/messages"),
        ("openai", "https://api.openai.com/v1/responses"),
        ("deepseek", "https://api.deepseek.com/anthropic/v1/messages"),
    ):
        route, client_type = _provider_route_client(provider)
        ref = write_cp8_live_scenario_evidence(
            output_root=tmp_path,
            run_id="cp8-scenario-writer",
            scenario="manual_provider_switch",
            route=route,
            client_type=client_type,
            evidence={
                "status": "pass",
                "live_provider_verified": True,
                "raw_sensitive_stored": False,
                "loopback": False,
                "provider": provider,
                "model": _provider_model(provider),
                "endpoint": endpoint,
                "upstream_request_id": f"req_{provider}",
                "provider_provenance_refs": [f"artifacts/provider_{provider}.json"],
                "notes": "safe summary only",
                "authorization": "Bearer must-not-be-written",
            },
        )
        assert ref["path"] == f"artifacts/scenario_manual_provider_switch_{provider}.json"
        assert ref["sensitive_scan_clean"] is True
        artifact_text = (tmp_path / ref["path"]).read_text(encoding="utf-8")
        assert "Bearer" not in artifact_text
        assert "must-not-be-written" not in artifact_text
        refs.append(ref)

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
            "model": _provider_model(provider),
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
            "model": _provider_model(provider),
            "artifact_refs": [{
                "path": f"artifacts/provider_{provider}.json",
                "sha256": "sha256:" + hashlib.sha256(provider_artifact.read_bytes()).hexdigest(),
                "sensitive_scan_clean": True,
            }],
        }
    for name, item in payload["scenarios"].items():
        item["live_provider_verified"] = True
        if name == "manual_provider_switch":
            item["artifact_refs"] = refs
    _add_strict_live_scenario_artifacts({
        "scenarios": {k: v for k, v in payload["scenarios"].items() if k != "manual_provider_switch"}
    }, tmp_path, "cp8-scenario-writer")
    _mark_core_provider_release_strict_live(payload)

    result = verify_cp8_live_matrix(payload, strict_live=True, evidence_root=tmp_path)
    assert result.status == "pass"


@pytest.mark.parametrize(
    ("route", "client_type", "refs", "summary"),
    (
        ("Bearer_sk_live_secret", "claude_code_bridge_deepseek", ["artifacts/provider_deepseek.json"], "safe summary"),
        ("deepseek_bridge", "api_key_leak", ["artifacts/provider_deepseek.json"], "safe summary"),
        ("deepseek_bridge", "claude_code_bridge_deepseek", ["artifacts/Bearer_sk_live_secret.json"], "safe summary"),
        ("deepseek_bridge", "claude_code_bridge_deepseek", ["/tmp/provider_deepseek.json"], "safe summary"),
        ("deepseek_bridge", "claude_code_bridge_deepseek", ["artifacts/../provider_deepseek.json"], "safe summary"),
        ("deepseek_bridge", "claude_code_bridge_deepseek", ["provider_deepseek.json"], "safe summary"),
        ("deepseek_bridge", "claude_code_bridge_deepseek", ["artifacts/raw_prompt.json"], "safe summary"),
        ("deepseek_bridge", "claude_code_bridge_deepseek", ["artifacts/session_token.json"], "safe summary"),
        ("deepseek_bridge", "claude_code_bridge_deepseek", ["artifacts/provider_deepseek.json"], "raw_body: should fail"),
        ("deepseek_bridge", "claude_code_bridge_deepseek", ["artifacts/provider_deepseek.json"], "raw_prompt redacted"),
        ("deepseek_bridge", "claude_code_bridge_deepseek", ["artifacts/provider_deepseek.json"], "raw_header redacted"),
        ("deepseek_bridge", "claude_code_bridge_deepseek", ["artifacts/provider_deepseek.json"], "api key redacted"),
    ),
)
def test_cp8_live_scenario_evidence_writer_rejects_unsafe_fields_before_claiming_scan_clean(
    tmp_path: Path,
    route: str,
    client_type: str,
    refs: list[str],
    summary: str,
):
    from zhumeng_agent.adapters.claude_code.live_matrix import write_cp8_live_scenario_evidence  # noqa: PLC0415

    with pytest.raises(CP8LiveMatrixError, match="safe|sensitive|provider provenance"):
        write_cp8_live_scenario_evidence(
            output_root=tmp_path,
            run_id="cp8-scenario-writer-unsafe",
            scenario="manual_provider_switch",
            route=route,
            client_type=client_type,
            evidence={
                "status": "pass",
                "live_provider_verified": True,
                "provider": "deepseek",
                "model": "deepseek-v4-pro",
                "endpoint": "https://api.deepseek.com/anthropic/v1/messages",
                "upstream_request_id": "req_deepseek",
                "provider_provenance_refs": refs,
                "safe_evidence_summary": summary,
            },
        )

    assert not (tmp_path / "artifacts" / "scenario_manual_provider_switch_deepseek.json").exists()
