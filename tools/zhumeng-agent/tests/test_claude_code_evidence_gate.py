from __future__ import annotations

import json
from pathlib import Path

import pytest

from zhumeng_agent.adapters.claude_code.evidence_gate import (
    CanaryEvidenceError,
    ProviderReleaseStatus,
    build_checkpoint0_decision_freeze,
    hmac_audit_digest,
    prepare_canary_evidence_run,
    scan_evidence_tree_for_sensitive_content,
)


def test_checkpoint0_decision_freeze_records_default_l8_scope_without_secrets(tmp_path: Path):
    run = prepare_canary_evidence_run(
        tmp_path,
        run_id="l8-test-run",
        runtime_hash="runtime-hash",
        overlay_hash="overlay-hash",
        catalog_hash="catalog-hash",
    )

    decision = build_checkpoint0_decision_freeze(
        run,
        accepted_by="user",
        acceptance_source="thread_goal",
        accepted_at_unix=1800000000,
    )

    decision_path = tmp_path / "l8-test-run" / "preflight" / "decision-freeze.json"
    assert decision_path.exists()
    assert decision["schema_version"] == "claude-code-l8-decision-freeze-v1"
    assert decision["run_id"] == "l8-test-run"
    assert decision["provider_scope"] == {
        "strict_live_targets": ["claude_native", "openai", "deepseek"],
        "conditional_targets": {"agnes": "probe_and_strict_live_required"},
        "catalog_visible_live_disabled": ["glm", "kimi"],
    }
    assert decision["runtime_patch_policy"]["default_route"] == "preload_metadata_backend_enforcement"
    assert decision["runtime_patch_policy"]["direct_binary_patch_requires_separate_approval"] is True
    assert decision["deepseek_prefix_kv_policy"] == {
        "preferred_protocol": "anthropic_messages",
        "cache_mechanism": "deepseek_prefix_kv",
        "usage_fields": ["prompt_cache_hit_tokens", "prompt_cache_miss_tokens"],
        "requires_stable_prefix_hmac": True,
    }
    assert decision["deepseek_cache_control_policy"]["treat_cache_control_as_cache_mechanism"] is False
    assert decision["deepseek_cache_control_policy"]["allowed_outcomes"] == ["absent", "provider_ignored_if_present"]
    assert decision["toolsearch_policy"]["healthy_target"] == "ENABLE_TOOL_SEARCH=true"
    assert decision["provider_release_statuses"] == [item.value for item in ProviderReleaseStatus]
    assert decision["phase0_confirmed"] is True
    assert decision["accepted_by"] == "user"
    assert decision["acceptance_source"] == "thread_goal"
    assert decision["accepted_at_unix"] == 1800000000
    assert decision["provider_release_classification"] == {
        "claude_native": {
            "status": "fixture-pass-only",
            "evidence": "checkpoint0_scope_frozen_strict_live_required",
            "reason": "strict_live_evidence_pending",
        },
        "openai": {
            "status": "fixture-pass-only",
            "evidence": "checkpoint0_scope_frozen_strict_live_required",
            "reason": "strict_live_evidence_pending",
        },
        "deepseek": {
            "status": "fixture-pass-only",
            "evidence": "checkpoint0_scope_frozen_strict_live_required",
            "reason": "strict_live_evidence_pending",
        },
        "agnes": {
            "status": "live-disabled",
            "evidence": "checkpoint0_conditional_probe_required",
            "reason": "probe_and_strict_live_required",
        },
        "glm": {
            "status": "live-disabled",
            "evidence": "checkpoint0_catalog_visible_live_disabled",
            "reason": "outside_l8_live_scope",
        },
        "kimi": {
            "status": "live-disabled",
            "evidence": "checkpoint0_catalog_visible_live_disabled",
            "reason": "outside_l8_live_scope",
        },
    }

    serialized = decision_path.read_text(encoding="utf-8")
    assert "secret" not in serialized.lower()
    assert "authorization" not in serialized.lower()
    assert "raw_body" not in serialized.lower()


def test_prepare_canary_evidence_run_creates_required_directories_and_manifest(tmp_path: Path):
    run = prepare_canary_evidence_run(
        tmp_path,
        run_id="run-123",
        runtime_hash="rh",
        overlay_hash="oh",
        catalog_hash="ch",
    )

    expected_dirs = {
        "preflight",
        "unit",
        "ui",
        "cache",
        "replay",
        "live-matrix",
        "rollback",
    }
    assert {path.name for path in (tmp_path / "run-123").iterdir() if path.is_dir()} >= expected_dirs
    manifest = json.loads((tmp_path / "run-123" / "preflight" / "run-manifest.json").read_text(encoding="utf-8"))
    assert manifest == {
        "schema_version": "claude-code-l8-run-manifest-v1",
        "audit_schema_version": "claude_code_l8_repair_v1",
        "run_id": "run-123",
        "runtime_hash": "rh",
        "overlay_hash": "oh",
        "catalog_hash": "ch",
        "evidence_subdirs": sorted(expected_dirs),
    }
    assert run.run_dir == tmp_path / "run-123"


def test_hmac_audit_digest_is_purpose_scoped_and_fail_closed():
    one = hmac_audit_digest(
        b"local-test-key",
        key_id="runA-deepseek-bridge",
        purpose="stable_prefix",
        value="same redacted prefix",
    )
    two = hmac_audit_digest(
        b"local-test-key",
        key_id="runA-deepseek-bridge",
        purpose="safe_summary",
        value="same redacted prefix",
    )
    other_scope = hmac_audit_digest(
        b"local-test-key",
        key_id="runA-claude-formal",
        purpose="stable_prefix",
        value="same redacted prefix",
    )

    assert one.startswith("hmac-sha256:runA-deepseek-bridge:")
    assert two.startswith("hmac-sha256:runA-deepseek-bridge:")
    assert one != two
    assert one != other_scope
    assert one.rsplit(":", 1)[-1] != other_scope.rsplit(":", 1)[-1]
    with pytest.raises(CanaryEvidenceError, match="HMAC key is required"):
        hmac_audit_digest(b"", key_id="runA", purpose="stable_prefix", value="x")
    with pytest.raises(CanaryEvidenceError, match="HMAC key_id is required"):
        hmac_audit_digest(b"key", key_id="", purpose="stable_prefix", value="x")


def test_scan_evidence_tree_rejects_raw_sensitive_material(tmp_path: Path):
    run = prepare_canary_evidence_run(
        tmp_path,
        run_id="scan-run",
        runtime_hash="rh",
        overlay_hash="oh",
        catalog_hash="ch",
    )
    clean = run.run_dir / "preflight" / "clean.json"
    clean.write_text(json.dumps({"status": "pass", "stable_prefix_hmac": "hmac-sha256:k:v"}), encoding="utf-8")

    assert scan_evidence_tree_for_sensitive_content(run.run_dir)["status"] == "pass"

    dirty = run.run_dir / "cache" / "dirty.json"
    dirty.write_text('{"Authorization":"Bearer sk-secret-value", "raw_body":"prompt"}', encoding="utf-8")
    result = scan_evidence_tree_for_sensitive_content(run.run_dir)
    assert result["status"] == "fail"
    assert any(item["path"].endswith("cache/dirty.json") for item in result["findings"])
    with pytest.raises(CanaryEvidenceError):
        scan_evidence_tree_for_sensitive_content(run.run_dir, fail_closed=True)

    secret_field = run.run_dir / "cache" / "secret-field.json"
    secret_field.write_text('{"client_secret":"value", "password":"value"}', encoding="utf-8")
    result = scan_evidence_tree_for_sensitive_content(run.run_dir)
    assert result["status"] == "fail"
    assert any(item["path"].endswith("cache/secret-field.json") for item in result["findings"])
