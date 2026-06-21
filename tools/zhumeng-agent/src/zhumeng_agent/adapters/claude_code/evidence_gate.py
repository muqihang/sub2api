from __future__ import annotations

import hashlib
import hmac
import json
import re
from dataclasses import dataclass
from enum import StrEnum
from pathlib import Path
from typing import Any, Mapping


class CanaryEvidenceError(RuntimeError):
    pass


class ProviderReleaseStatus(StrEnum):
    STRICT_LIVE_PASS = "strict-live-pass"
    DEGRADED_PASS = "degraded-pass"
    FIXTURE_PASS_ONLY = "fixture-pass-only"
    LIVE_DISABLED = "live-disabled"


@dataclass(frozen=True, slots=True)
class CanaryEvidenceRun:
    root: Path
    run_id: str
    runtime_hash: str
    overlay_hash: str
    catalog_hash: str

    @property
    def run_dir(self) -> Path:
        return self.root / self.run_id


_EVIDENCE_SUBDIRS = (
    "preflight",
    "unit",
    "ui",
    "cache",
    "replay",
    "live-matrix",
    "rollback",
)
_AUDIT_SCHEMA_VERSION = "claude_code_l8_repair_v1"
_SENSITIVE_RE = re.compile(
    r"(Authorization|Bearer\s+|sk-[A-Za-z0-9][A-Za-z0-9_.:-]{6,}|api[_-]?key|cookie|setup[_-]?token|raw[_-]?(?:body|prompt|request|response|payload)|oauth[_-]?token|access[_-]?token|refresh[_-]?token|client[_-]?secret|password|secret)",
    re.IGNORECASE,
)


def prepare_canary_evidence_run(
    root: Path | str,
    *,
    run_id: str,
    runtime_hash: str,
    overlay_hash: str,
    catalog_hash: str,
) -> CanaryEvidenceRun:
    run_id = _safe_run_id(run_id)
    run = CanaryEvidenceRun(
        root=Path(root).expanduser(),
        run_id=run_id,
        runtime_hash=str(runtime_hash),
        overlay_hash=str(overlay_hash),
        catalog_hash=str(catalog_hash),
    )
    for name in _EVIDENCE_SUBDIRS:
        (run.run_dir / name).mkdir(parents=True, exist_ok=True)
    manifest = {
        "schema_version": "claude-code-l8-run-manifest-v1",
        "audit_schema_version": _AUDIT_SCHEMA_VERSION,
        "run_id": run.run_id,
        "runtime_hash": run.runtime_hash,
        "overlay_hash": run.overlay_hash,
        "catalog_hash": run.catalog_hash,
        "evidence_subdirs": sorted(_EVIDENCE_SUBDIRS),
    }
    _write_json(run.run_dir / "preflight" / "run-manifest.json", manifest)
    return run


def build_checkpoint0_decision_freeze(
    run: CanaryEvidenceRun,
    *,
    accepted_by: str,
    acceptance_source: str,
    accepted_at_unix: int,
) -> dict[str, Any]:
    accepted_by = _safe_hmac_segment(accepted_by, label="accepted_by")
    acceptance_source = _safe_hmac_segment(acceptance_source, label="acceptance_source")
    accepted_at_unix = int(accepted_at_unix)
    provider_classification = _default_provider_release_classification()
    decision: dict[str, Any] = {
        "schema_version": "claude-code-l8-decision-freeze-v1",
        "audit_schema_version": _AUDIT_SCHEMA_VERSION,
        "run_id": run.run_id,
        "runtime_hash": run.runtime_hash,
        "overlay_hash": run.overlay_hash,
        "catalog_hash": run.catalog_hash,
        "provider_scope": {
            "strict_live_targets": ["claude_native", "openai", "deepseek"],
            "conditional_targets": {"agnes": "probe_and_strict_live_required"},
            "catalog_visible_live_disabled": ["glm", "kimi"],
        },
        "runtime_patch_policy": {
            "default_route": "preload_metadata_backend_enforcement",
            "direct_binary_patch_requires_separate_approval": True,
        },
        "deepseek_prefix_kv_policy": {
            "preferred_protocol": "anthropic_messages",
            "cache_mechanism": "deepseek_prefix_kv",
            "usage_fields": ["prompt_cache_hit_tokens", "prompt_cache_miss_tokens"],
            "requires_stable_prefix_hmac": True,
        },
        "deepseek_cache_control_policy": {
            "treat_cache_control_as_cache_mechanism": False,
            "allowed_outcomes": ["absent", "provider_ignored_if_present"],
            "requires_cache_control_provider_ignored_audit": True,
        },
        "model_ui_evidence_policy": {
            "minimum": "machine_readable_runtime_capability_probe",
            "manual_screenshot_required_on_mismatch": True,
        },
        "toolsearch_policy": {
            "healthy_target": "ENABLE_TOOL_SEARCH=true",
            "degraded_states": ["auto", "false"],
            "requires_fixed_mcp_deferred_tool_healthcheck": True,
        },
        "hmac_policy": {
            "algorithm": "hmac-sha256",
            "key_owner": "local_canary_audit",
            "key_scope": "run_provider_account_boundary",
            "minimum_rotation": "per_canary_run_or_audit_epoch",
            "missing_key": "fail_closed",
            "formal_pool_bridge_pool_key_id_reuse_allowed": False,
        },
        "provider_release_statuses": [item.value for item in ProviderReleaseStatus],
        "provider_release_classification": provider_classification,
        "phase0_confirmed": True,
        "accepted_by": accepted_by,
        "acceptance_source": acceptance_source,
        "accepted_at_unix": accepted_at_unix,
    }
    _write_json(run.run_dir / "preflight" / "decision-freeze.json", decision)
    return decision


def hmac_audit_digest(key: bytes, *, key_id: str, purpose: str, value: str | bytes) -> str:
    if not key:
        raise CanaryEvidenceError("HMAC key is required")
    key_id = _safe_hmac_segment(key_id, label="HMAC key_id")
    purpose = _safe_hmac_segment(purpose, label="HMAC purpose")
    data = value if isinstance(value, bytes) else str(value).encode("utf-8")
    msg = key_id.encode("utf-8") + b"\x00" + purpose.encode("utf-8") + b"\x00" + data
    digest = hmac.new(key, msg, hashlib.sha256).hexdigest()
    return f"hmac-sha256:{key_id}:{digest}"


def scan_evidence_tree_for_sensitive_content(root: Path | str, *, fail_closed: bool = False) -> dict[str, Any]:
    root_path = Path(root).expanduser()
    findings: list[dict[str, str]] = []
    for path in sorted(root_path.rglob("*")):
        if not path.is_file():
            continue
        try:
            text = path.read_text(encoding="utf-8")
        except UnicodeDecodeError:
            continue
        match = _SENSITIVE_RE.search(text)
        if match:
            findings.append({"path": str(path), "marker": _safe_marker(match.group(0))})
    result = {
        "schema_version": "claude-code-l8-sensitive-scan-v1",
        "status": "fail" if findings else "pass",
        "findings": findings,
    }
    if findings and fail_closed:
        raise CanaryEvidenceError("sensitive evidence scan failed")
    return result


def _write_json(path: Path, payload: Mapping[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, ensure_ascii=True, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")


def _safe_run_id(value: str) -> str:
    value = re.sub(r"[^A-Za-z0-9_.-]+", "-", str(value)).strip(".-_")
    if not value:
        raise CanaryEvidenceError("run_id is required")
    return value


def _safe_hmac_segment(value: str, *, label: str) -> str:
    segment = re.sub(r"[^A-Za-z0-9_.:-]+", "-", str(value)).strip(".-_:")
    if not segment:
        raise CanaryEvidenceError(f"{label} is required")
    if _SENSITIVE_RE.search(segment):
        raise CanaryEvidenceError(f"{label} must not contain sensitive material")
    return segment


def _safe_marker(value: str) -> str:
    lowered = value.lower()
    if lowered.startswith("bearer"):
        return "bearer"
    if lowered.startswith("sk-"):
        return "api_token"
    if "authorization" in lowered:
        return "authorization"
    if "raw" in lowered:
        return "raw_payload"
    if "key" in lowered:
        return "api_key"
    if "token" in lowered:
        return "token"
    if "cookie" in lowered:
        return "cookie"
    return "sensitive"


def _default_provider_release_classification() -> dict[str, dict[str, str]]:
    return {
        "claude_native": {
            "status": ProviderReleaseStatus.FIXTURE_PASS_ONLY.value,
            "evidence": "checkpoint0_scope_frozen_strict_live_required",
            "reason": "strict_live_evidence_pending",
        },
        "openai": {
            "status": ProviderReleaseStatus.FIXTURE_PASS_ONLY.value,
            "evidence": "checkpoint0_scope_frozen_strict_live_required",
            "reason": "strict_live_evidence_pending",
        },
        "deepseek": {
            "status": ProviderReleaseStatus.FIXTURE_PASS_ONLY.value,
            "evidence": "checkpoint0_scope_frozen_strict_live_required",
            "reason": "strict_live_evidence_pending",
        },
        "agnes": {
            "status": ProviderReleaseStatus.LIVE_DISABLED.value,
            "evidence": "checkpoint0_conditional_probe_required",
            "reason": "probe_and_strict_live_required",
        },
        "glm": {
            "status": ProviderReleaseStatus.LIVE_DISABLED.value,
            "evidence": "checkpoint0_catalog_visible_live_disabled",
            "reason": "outside_l8_live_scope",
        },
        "kimi": {
            "status": ProviderReleaseStatus.LIVE_DISABLED.value,
            "evidence": "checkpoint0_catalog_visible_live_disabled",
            "reason": "outside_l8_live_scope",
        },
    }
