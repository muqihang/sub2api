from __future__ import annotations

import importlib.util
import json
import re
import subprocess
import sys
import time
from dataclasses import dataclass, field
from pathlib import Path
from typing import Callable, Mapping, Sequence

from .guard import NativeGuardConfig, NativeGuardMode, NativeGuardPlan, build_native_guard_plan, start_native_guard
from .profile import CaptureMode, ClaudeCodeProfile, build_isolated_config_dir, build_safe_env, safe_profile_segment

Runner = Callable[..., object]
_VERSION_RE = re.compile(r"(?:claude(?:-code)?[/ ]+v?|Claude Code\s+v?)(\d+(?:\.\d+){1,3})", re.IGNORECASE)
_FALLBACK_VERSION_RE = re.compile(r"\bv?(\d+\.\d+(?:\.\d+){0,2})\b")
_ROUTE_HINT_PRELOAD_NAME = "route-hint-preload.cjs"
_ROUTE_HINT_CATALOG_NAME = "route-hint-catalog.json"


@dataclass(frozen=True, slots=True)
class ClaudeCodeVersion:
    executable: Path
    version: str | None
    raw_output: str
    returncode: int = 0


@dataclass(frozen=True, slots=True)
class ClaudeCodeLaunchPlan:
    command: list[str]
    env: dict[str, str] = field(repr=False)
    cwd: Path | None
    profile: ClaudeCodeProfile
    will_start_process: bool = False


@dataclass(frozen=True, slots=True)
class ManagedClaudeCodeRunResult:
    returncode: int
    guard_ready: dict[str, object]
    guard_plan: NativeGuardPlan
    launch_plan: ClaudeCodeLaunchPlan


def detect_claude_code_version(
    *,
    executable: Path | str = "claude",
    runner: Runner | None = None,
    timeout_seconds: float = 3.0,
    env: Mapping[str, str] | None = None,
) -> ClaudeCodeVersion:
    executable_path = Path(executable)
    runner = runner or subprocess.run
    kwargs: dict[str, object] = {
        "capture_output": True,
        "text": True,
        "check": False,
        "timeout": timeout_seconds,
    }
    if env is not None:
        kwargs["env"] = dict(env)
    result = runner([str(executable_path), "--version"], **kwargs)
    stdout = str(getattr(result, "stdout", "") or "")
    stderr = str(getattr(result, "stderr", "") or "")
    raw_output = (stdout or stderr).strip()
    return ClaudeCodeVersion(
        executable=executable_path,
        version=_parse_claude_code_version(raw_output),
        raw_output=raw_output,
        returncode=int(getattr(result, "returncode", 0) or 0),
    )


def build_claude_code_launch_plan(
    *,
    executable: Path | str,
    profile: ClaudeCodeProfile,
    inherited_env: Mapping[str, str] | None = None,
    project_cwd: Path | None = None,
    argv: Sequence[str] | None = None,
) -> ClaudeCodeLaunchPlan:
    env = build_safe_env(profile, inherited_env=inherited_env, project_cwd=project_cwd)
    command = [str(Path(executable)), *(argv or [])]
    return ClaudeCodeLaunchPlan(
        command=command,
        env=env,
        cwd=project_cwd,
        profile=profile,
        will_start_process=False,
    )


def _parse_claude_code_version(raw_output: str) -> str | None:
    match = _VERSION_RE.search(raw_output) or _FALLBACK_VERSION_RE.search(raw_output)
    return match.group(1) if match else None


def run_managed_claude_code(
    *,
    executable: Path | str,
    repo_root: Path,
    upstream_base: str,
    sub2api_auth: str,
    attestation_secret: str,
    native_managed_access_token: str | None = None,
    route_hint_secret: str | None = None,
    route_hint_catalog_version: str = "cp4-cli-fixture-v1",
    managed_session_id: str | None = None,
    device_id: int | None = None,
    runtime_hash: str | None = None,
    overlay_hash: str | None = None,
    bridge_live_models: tuple[str, ...] = (),
    config_root: Path,
    project_cwd: Path | None = None,
    guard_listen_port: int,
    argv: Sequence[str] | None = None,
    inherited_env: Mapping[str, str] | None = None,
    profile_id: str = "prod",
    mode: NativeGuardMode = NativeGuardMode.PRODUCTION,
    start_guard=start_native_guard,
    process_runner=None,
    ready_timeout_seconds: float = 10.0,
) -> ManagedClaudeCodeRunResult:
    if not str(route_hint_secret or "").strip():
        raise ValueError("managed Claude Code runtime requires route_hint_secret for CP4 routing trust contract")
    config_root = config_root.expanduser()
    safe_profile_id = safe_profile_segment(profile_id)
    summary_path = config_root / "claude-code" / safe_profile_id / "native-guard-summary.jsonl"
    summary_path.parent.mkdir(parents=True, exist_ok=True)
    cert_path, key_path = _ensure_control_plane_stub_cert(config_root, profile_id=safe_profile_id)
    guard_config = NativeGuardConfig(
        mode=mode,
        listen_port=guard_listen_port,
        upstream_base=upstream_base,
        sub2api_auth=sub2api_auth,
        native_managed_access_token=native_managed_access_token,
        native_managed_state_path=config_root / "state.json",
        summary_path=summary_path,
        repo_root=repo_root,
        connect_mode="stub",
        cert_path=cert_path,
        key_path=key_path,
        attestation_secret=attestation_secret,
        route_hint_secret=route_hint_secret,
        route_hint_catalog_version=route_hint_catalog_version,
        allow_nonloopback_upstream=True,
        managed_session_id=managed_session_id,
        device_id=device_id,
        runtime_hash=runtime_hash,
        overlay_hash=overlay_hash,
        bridge_live_models=tuple(bridge_live_models),
    )
    guard_plan = build_native_guard_plan(guard_config, inherited_env=inherited_env)
    with start_guard(guard_plan, ready_timeout_seconds=ready_timeout_seconds) as guard:
        guard_base_url = str(guard.ready["listen"])
        profile = ClaudeCodeProfile(
            profile_id=profile_id,
            guard_base_url=guard_base_url,
            zhumeng_entry_api_key=sub2api_auth,
            config_dir=build_isolated_config_dir(config_root, profile_id=safe_profile_id),
            capture_mode=CaptureMode.PRODUCTION,
            node_extra_ca_certs=cert_path,
        )
        launch_plan = build_claude_code_launch_plan(
            executable=executable,
            profile=profile,
            inherited_env=inherited_env,
            project_cwd=project_cwd,
            argv=argv,
        )
        route_hint_env = _write_route_hint_preload_artifacts(
            config_root=config_root,
            profile_id=safe_profile_id,
            guard_plan=guard_plan,
            route_hint_secret=route_hint_secret,
        )
        _refresh_gateway_model_cache(
            config_root=config_root,
            profile_id=safe_profile_id,
            guard_base_url=guard_base_url,
            guard_plan=guard_plan,
        )
        provider_profile_env = _write_provider_profile_resolver_artifact(
            config_root=config_root,
            profile_id=safe_profile_id,
            executable=Path(executable),
            runtime_hash=guard_plan.env["ZHUMENG_CLAUDE_RUNTIME_HASH"],
            overlay_hash=guard_plan.env["ZHUMENG_CLAUDE_OVERLAY_HASH"],
            bridge_live_models=tuple(guard_plan.config.bridge_live_models),
        )
        launch_plan = ClaudeCodeLaunchPlan(
            command=launch_plan.command,
            env={**launch_plan.env, **route_hint_env, **provider_profile_env},
            cwd=launch_plan.cwd,
            profile=launch_plan.profile,
            will_start_process=True,
        )
        runner = process_runner or _default_process_runner
        returncode = int(runner(
            launch_plan.command,
            env=launch_plan.env,
            cwd=str(launch_plan.cwd) if launch_plan.cwd is not None else None,
        ))
        return ManagedClaudeCodeRunResult(
            returncode=returncode,
            guard_ready=dict(guard.ready),
            guard_plan=guard_plan,
            launch_plan=launch_plan,
        )


def _default_process_runner(command: list[str], *, env: Mapping[str, str], cwd: str | None) -> int:
    return subprocess.call(command, env=dict(env), cwd=cwd)


def _ensure_control_plane_stub_cert(config_root: Path, *, profile_id: str) -> tuple[Path, Path]:
    cert_dir = config_root / "claude-code" / profile_id / "certs"
    cert_path = cert_dir / "control-plane-stub-ca.pem"
    key_path = cert_dir / "control-plane-stub-ca.key"
    if cert_path.exists() and key_path.exists():
        return cert_path, key_path
    cert_dir.mkdir(parents=True, exist_ok=True)
    openssl_config = cert_dir / "control-plane-stub-openssl.cnf"
    openssl_config.write_text(
        """
[req]
default_bits = 2048
prompt = no
distinguished_name = dn
x509_extensions = v3_req

[dn]
CN = Zhumeng Claude Code Control Plane Guard

[v3_req]
subjectAltName = @alt_names
basicConstraints = critical, CA:TRUE, pathlen:0
keyUsage = critical, digitalSignature, keyEncipherment, keyCertSign, cRLSign
extendedKeyUsage = serverAuth

[alt_names]
DNS.1 = api.anthropic.com
DNS.2 = platform.claude.com
DNS.3 = claude.ai
DNS.4 = claude.com
DNS.5 = mcp-proxy.anthropic.com
""".lstrip(),
        encoding="utf-8",
    )
    subprocess.run(
        [
            "openssl",
            "req",
            "-x509",
            "-newkey",
            "rsa:2048",
            "-sha256",
            "-nodes",
            "-days",
            "7",
            "-keyout",
            str(key_path),
            "-out",
            str(cert_path),
            "-config",
            str(openssl_config),
        ],
        check=True,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    try:
        key_path.chmod(0o600)
        openssl_config.chmod(0o600)
        cert_path.chmod(0o644)
    except OSError:
        pass
    return cert_path, key_path




def _refresh_gateway_model_cache(
    *,
    config_root: Path,
    profile_id: str,
    guard_base_url: str,
    guard_plan: NativeGuardPlan,
) -> None:
    cache_dir = build_isolated_config_dir(config_root, profile_id=profile_id) / "cache"
    cache_dir.mkdir(parents=True, exist_ok=True)
    catalog = _route_hint_catalog_payload(guard_plan)
    bridge_entries = [
        entry
        for entry in catalog["entries"].values()
        if isinstance(entry, dict) and str(entry.get("model_id") or "").startswith("claude-code-bridge-")
    ]
    order = {model: index for index, model in enumerate(guard_plan.config.bridge_live_models)}
    bridge_entries.sort(key=lambda entry: order.get(str(entry.get("model_id") or ""), 10_000))
    models = [_gateway_model_cache_entry(entry) for entry in bridge_entries]
    payload = {
        "baseUrl": guard_base_url,
        "fetchedAt": int(time.time() * 1000),
        "models": models,
    }
    (cache_dir / "gateway-models.json").write_text(json.dumps(payload, ensure_ascii=True, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")


def _gateway_model_cache_entry(entry: Mapping[str, object]) -> dict[str, object]:
    model_id = str(entry["model_id"])
    effort_levels = tuple(str(level) for level in entry.get("reasoning_effort_levels", ()) if str(level))
    model: dict[str, object] = {
        "id": model_id,
        "type": "model",
        "display_name": _bridge_display_name(model_id),
        "display_only": not bool(entry.get("live_enabled")),
        "live_enabled": bool(entry.get("live_enabled")),
        "route": str(entry.get("route") or ""),
        "client_type": str(entry.get("client_type") or ""),
        "supportsEffort": bool(effort_levels),
    }
    if effort_levels:
        model["supportedEffortLevels"] = list(effort_levels)
    return model


def _bridge_reasoning_effort_levels(model_id: str, provider: str) -> tuple[str, ...]:
    provider = str(provider or "").strip()
    if provider == "openai":
        return ("low", "medium", "high", "xhigh")
    if provider in {"deepseek", "zai_glm"}:
        return ("high", "max")
    return ()


def _bridge_display_name(model_id: str) -> str:
    labels = {
        "claude-code-bridge-gpt-5.5": "GPT 5.5 (Claude Code Bridge)",
        "claude-code-bridge-gpt-5.4": "GPT 5.4 (Claude Code Bridge)",
        "claude-code-bridge-gpt-5.4-mini": "GPT 5.4 Mini (Claude Code Bridge)",
        "claude-code-bridge-deepseek-v4-pro": "DeepSeek V4 Pro (Claude Code Bridge)",
        "claude-code-bridge-deepseek-v4-flash": "DeepSeek V4 Flash (Claude Code Bridge)",
        "claude-code-bridge-agnes-2.0-flash": "AGNES 2.0 Flash (Claude Code Bridge)",
        "claude-code-bridge-glm-5.2-1m": "GLM 5.2 1M (Claude Code Bridge)",
        "claude-code-bridge-kimi-k2.7-code": "Kimi K2.7 Code (Claude Code Bridge)",
    }
    return labels.get(model_id, model_id)

def _write_provider_profile_resolver_artifact(
    *,
    config_root: Path,
    profile_id: str,
    executable: Path,
    runtime_hash: str,
    overlay_hash: str,
    bridge_live_models: tuple[str, ...],
) -> dict[str, str]:
    from .model_overlay import (  # noqa: PLC0415
        build_agent_model_options,
        build_cp2_model_overlay_proof,
        build_cp3a_model_overlay_contract,
        resolve_background_model,
    )

    overlay_dir = config_root / "claude-code" / profile_id / "overlay" / "cp3a-provider-profile"
    overlay_dir.mkdir(parents=True, exist_ok=True)
    profile_path = overlay_dir / "provider-profile-resolver.json"
    proof = build_cp2_model_overlay_proof(_synthetic_runtime_plan_for_overlay(executable, runtime_hash=runtime_hash, overlay_hash=overlay_hash))
    contract = build_cp3a_model_overlay_contract(proof)
    live_models = {model for model in bridge_live_models if model}
    profiles = {
        profile.provider: {
            "profile_id": profile.profile_id,
            "provider": profile.provider,
            "main_model_id": profile.main_model_id,
            "fast_model_id": profile.fast_model_id,
            "family_aliases": dict(sorted(profile.family_aliases.items())),
            "native_formal_pool": profile.native_formal_pool,
            "live_enabled": _provider_profile_live_enabled(profile, live_models),
            "cross_provider_fast_model_ids": list(profile.cross_provider_fast_model_ids),
            "live_cross_provider_fast_model_ids": [model for model in profile.cross_provider_fast_model_ids if model in live_models],
        }
        for profile in contract.provider_profiles
    }
    background_matrix: dict[str, dict[str, object]] = {}
    for provider, profile in contract.provider_profiles_by_provider.items():
        provider_local: dict[str, object] = {}
        for task in sorted({"title", "compact", "summary", "probe", "fast", "simple", "haiku"}):
            provider_local[task] = _runtime_resolution_payload(resolve_background_model(
                contract,
                active_model_id=profile.main_model_id,
                task=task,
                fast_preference="provider_local",
            ))
        provider_entry: dict[str, object] = {"provider_local": provider_local}
        if provider == "claude":
            provider_entry["fast_bridge"] = _runtime_resolution_payload(resolve_background_model(
                contract,
                active_model_id=profile.main_model_id,
                task="fast",
                fast_preference="bridge",
            ))
        background_matrix[provider] = provider_entry
    payload = {
        "schema_version": "cp3a-provider-profile-runtime-v1",
        "runtime_hash": runtime_hash,
        "overlay_hash": overlay_hash,
        "bridge_live_models": list(bridge_live_models),
        "display_id_prefix_required": "claude-",
        "backend_route_metadata_prefix": "claude_code_bridge_",
        "active_profile_dynamic_resolution": True,
        "non_claude_background_native_egress_allowed": False,
        "claude_cross_provider_fast_allowed": True,
        "provider_profiles": profiles,
        "agent_model_options": [_agent_model_option_payload(option) for option in build_agent_model_options(contract)],
        "background_resolution_matrix": background_matrix,
    }
    profile_path.write_text(json.dumps(payload, ensure_ascii=True, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")
    return {
        "ZHUMENG_CLAUDE_PROVIDER_PROFILE_RESOLVER": "enabled",
        "ZHUMENG_CLAUDE_PROVIDER_PROFILE_PATH": str(profile_path),
    }


def _provider_profile_live_enabled(profile, live_models: set[str]) -> bool:
    if profile.native_formal_pool:
        return True
    if profile.main_model_id not in live_models:
        return False
    return profile.fast_model_id == profile.main_model_id or profile.fast_model_id in live_models


def _runtime_resolution_payload(resolution) -> dict[str, object]:
    return {
        "requested": resolution.requested,
        "resolved_model_id": resolution.resolved_model_id,
        "provider": resolution.provider,
        "provider_profile_id": resolution.provider_profile_id,
        "route": resolution.route,
        "upstream_model_id": resolution.upstream_model_id,
        "client_type": resolution.client_type,
        "native_egress_allowed": resolution.native_egress_allowed,
        "formal_pool_allowed": resolution.formal_pool_allowed,
        "replay_boundary": resolution.replay_boundary,
        "resolution_source": resolution.resolution_source,
        "dynamic_profile_resolved": resolution.dynamic_profile_resolved,
        "explicit_claude_opt_in": resolution.explicit_claude_opt_in,
        "raw_history_replay_allowed": resolution.raw_history_replay_allowed,
        "audit_label": resolution.audit_label,
    }


def _agent_model_option_payload(option) -> dict[str, object]:
    return {
        "option_id": option.option_id,
        "label": option.label,
        "provider": option.provider,
        "model_id": option.model_id,
        "is_default": option.is_default,
        "native_egress_allowed": option.native_egress_allowed,
    }


def _synthetic_runtime_plan_for_overlay(executable: Path, *, runtime_hash: str, overlay_hash: str):
    from .runtime_installer import ManagedRuntimeInstallPlan, ManagedRuntimeManifest  # noqa: PLC0415

    root = executable.parent if str(executable.parent) else Path(".")
    manifest = ManagedRuntimeManifest(
        runtime="claude-code",
        upstream_version="managed",
        zhumeng_runtime_version="managed-launch",
        source="managed-launch",
        upstream_hash=runtime_hash,
        overlay_hash=overlay_hash,
        patch_points=("runtime_manifest", "hash_lock", "isolated_config", "guard_env"),
        cch_profile="native",
        status="ready",
        executable_path=str(executable),
    )
    return ManagedRuntimeInstallPlan(
        executable=executable,
        runtime_root=root,
        upstream_version="managed",
        runtime_dir=root,
        version_dir=root,
        cache_path=executable,
        manifest_path=root / "manifest.json",
        patches_path=root / "patches.json",
        hash_lock_path=root / "hash-lock.json",
        rollback_metadata_path=root / "rollback.json",
        active_pointer=root / "active.json",
        manifest=manifest,
        patches={},
        rollback_metadata={},
        planned_write_paths=(),
    )

def _write_route_hint_preload_artifacts(
    *,
    config_root: Path,
    profile_id: str,
    guard_plan: NativeGuardPlan,
    route_hint_secret: str,
) -> dict[str, str]:
    overlay_dir = config_root / "claude-code" / profile_id / "overlay" / "cp4-route-hint"
    overlay_dir.mkdir(parents=True, exist_ok=True)
    catalog_path = overlay_dir / _ROUTE_HINT_CATALOG_NAME
    preload_path = overlay_dir / _ROUTE_HINT_PRELOAD_NAME
    catalog_payload = _route_hint_catalog_payload(guard_plan)
    catalog_path.write_text(json.dumps(catalog_payload, ensure_ascii=True, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")
    preload_path.write_text(_ROUTE_HINT_PRELOAD_JS, encoding="utf-8")
    node_options = _prepend_node_require("", preload_path)
    bun_options = _prepend_bun_preload("", preload_path)
    return {
        # Claude Code 2.1.177 is a Bun-packed binary, so NODE_OPTIONS alone is
        # ignored. Keep both forms: Node for test/future runtimes, Bun for live.
        "BUN_OPTIONS": bun_options,
        "NODE_OPTIONS": node_options,
        "ZHUMENG_CLAUDE_ROUTE_HINT_PRELOAD": "enabled",
        "ZHUMENG_CLAUDE_ROUTE_HINT_SECRET": route_hint_secret,
        "ZHUMENG_CLAUDE_ROUTE_HINT_CATALOG_PATH": str(catalog_path),
        "ZHUMENG_CLAUDE_ROUTE_HINT_PRELOAD_PATH": str(preload_path),
    }


def _route_hint_catalog_payload(guard_plan: NativeGuardPlan) -> dict[str, object]:
    route_trust = _load_route_trust_module(guard_plan.config.repo_root)
    catalog = route_trust.cp4_fixture_route_catalog(
        runtime_hash=guard_plan.env["ZHUMENG_CLAUDE_RUNTIME_HASH"],
        overlay_hash=guard_plan.env["ZHUMENG_CLAUDE_OVERLAY_HASH"],
        catalog_hash=guard_plan.env["ZHUMENG_CLAUDE_CATALOG_HASH"],
        catalog_version=guard_plan.config.route_hint_catalog_version,
        bridge_live_models=tuple(guard_plan.config.bridge_live_models),
    )
    expected_catalog_hash = route_trust.route_catalog_content_hash(catalog)
    if catalog.catalog_hash != expected_catalog_hash:
        raise RuntimeError("managed Claude Code route hint catalog hash mismatch")
    entries: dict[str, dict[str, object]] = {}
    for model_id, entry in catalog.entries.items():
        normalized = {
            "model_id": entry.model_id,
            "provider": entry.provider,
            "route": entry.route,
            "client_type": entry.client_type,
            "live_enabled": entry.live_enabled,
            "formal_pool_allowed": entry.formal_pool_allowed,
            "native_attestation_allowed": entry.native_attestation_allowed,
            "provider_owner": entry.provider_owner,
            "credential_scope": entry.credential_scope,
            "gateway_location": entry.gateway_location,
        }
        effort_levels = _bridge_reasoning_effort_levels(entry.model_id, entry.provider)
        if effort_levels:
            normalized["reasoning_effort_levels"] = list(effort_levels)
        entries[model_id] = normalized
    return {
        "schema_version": "cp4-route-hint-preload-v1",
        "runtime_hash": catalog.runtime_hash,
        "overlay_hash": catalog.overlay_hash,
        "catalog_hash": catalog.catalog_hash,
        "catalog_version": catalog.catalog_version,
        "entries": entries,
    }


def _load_route_trust_module(repo_root: Path):
    module_path = repo_root / "tools" / "claude_code_route_trust.py"
    spec = importlib.util.spec_from_file_location("zhumeng_launcher_route_trust", module_path)
    if spec is None or spec.loader is None:
        raise RuntimeError("unable to load Claude Code route trust module")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def _prepend_node_require(existing_node_options: str, preload_path: Path) -> str:
    require_arg = f"--require={_quote_node_options_value(str(preload_path))}"
    existing = str(existing_node_options or "").strip()
    return f"{require_arg} {existing}".strip()


def _prepend_bun_preload(existing_bun_options: str, preload_path: Path) -> str:
    # Bun's option parser does not accept --preload="/path with spaces"; it
    # does accept a separate --preload argument with shell-style escaped spaces.
    preload_arg = f"--preload {_escape_bun_options_value(str(preload_path))}"
    existing = str(existing_bun_options or "").strip()
    return f"{preload_arg} {existing}".strip()


def _quote_node_options_value(value: str) -> str:
    escaped = value.replace("\\", "\\\\").replace('"', '\\"')
    return f'"{escaped}"'


def _escape_bun_options_value(value: str) -> str:
    escaped = value.replace("\\", "\\\\").replace(" ", "\\ ")
    return escaped


_ROUTE_HINT_PRELOAD_JS = r"""'use strict';
const crypto = require('node:crypto');
const fs = require('node:fs');
const http = require('node:http');
const https = require('node:https');

const HINT_HEADER = 'x-zhumeng-claude-code-route-hint';
const SIGNATURE_HEADER = 'x-zhumeng-claude-code-route-signature';
const SCOPE = 'claude_code_route_hint_cp4';
const VERSION = 1;
const secret = process.env.ZHUMENG_CLAUDE_ROUTE_HINT_SECRET || '';
const catalogPath = process.env.ZHUMENG_CLAUDE_ROUTE_HINT_CATALOG_PATH || '';
const providerProfilePath = process.env.ZHUMENG_CLAUDE_PROVIDER_PROFILE_PATH || '';
const catalog = JSON.parse(fs.readFileSync(catalogPath, 'utf8'));
const providerProfiles = providerProfilePath && fs.existsSync(providerProfilePath)
  ? JSON.parse(fs.readFileSync(providerProfilePath, 'utf8'))
  : {provider_profiles: {}, background_resolution_matrix: {}};
let activeModelId = (process.env.ZHUMENG_CLAUDE_ACTIVE_MODEL_ID || process.env.CLAUDE_CODE_MODEL || '').trim();
const originalFetch = globalThis.fetch;
const originalHttpRequest = http.request;
const originalHttpsRequest = https.request;

if (!secret) {
  throw new Error('ZHUMENG Claude route hint secret is required');
}
if (typeof originalFetch !== 'function') {
  throw new Error('global fetch is required for ZHUMENG Claude route hints');
}

function b64url(data) {
  return Buffer.from(data).toString('base64').replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '');
}

function canonicalJson(value) {
  if (Array.isArray(value)) {
    return '[' + value.map(canonicalJson).join(',') + ']';
  }
  if (value && typeof value === 'object') {
    return '{' + Object.keys(value).sort().map((key) => JSON.stringify(key) + ':' + canonicalJson(value[key])).join(',') + '}';
  }
  return JSON.stringify(value);
}

function sha256Hex(data) {
  return crypto.createHash('sha256').update(data).digest('hex');
}

function sign(encoded, requestPath, body) {
  const material = Buffer.concat([
    Buffer.from(encoded),
    Buffer.from('\nPOST\n'),
    Buffer.from(requestPath),
    Buffer.from('\n'),
    Buffer.from(sha256Hex(body)),
  ]);
  return b64url(crypto.createHmac('sha256', secret).update(material).digest());
}

function routePath(urlValue) {
  const base = process.env.ANTHROPIC_BASE_URL || process.env.CLAUDE_CODE_API_BASE_URL || 'http://127.0.0.1';
  const parsed = new URL(String(urlValue), base);
  return parsed.pathname + parsed.search;
}

function shouldSign(method, requestPath) {
  return String(method || '').toUpperCase() === 'POST' && (
    requestPath === '/v1/messages' ||
    requestPath === '/v1/messages?beta=true' ||
    requestPath === '/v1/messages/count_tokens' ||
    requestPath === '/v1/messages/count_tokens?beta=true'
  );
}

function bodyBuffer(body) {
  if (typeof body === 'string') return Buffer.from(body);
  if (Buffer.isBuffer(body)) return Buffer.from(body);
  if (body instanceof Uint8Array) return Buffer.from(body);
  if (body instanceof ArrayBuffer) return Buffer.from(body);
  throw new Error('ZHUMENG route hint requires a replayable request body');
}

function parseBodyObject(body) {
  const parsed = JSON.parse(Buffer.from(body).toString('utf8'));
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed) || typeof parsed.model !== 'string' || !parsed.model.trim()) {
    throw new Error('ZHUMENG route hint requires body.model');
  }
  return parsed;
}

function activeProvider() {
  const activeModel = activeModelId;
  const entry = activeModel ? catalog.entries[activeModel] : undefined;
  return entry && entry.provider ? String(entry.provider) : '';
}

function rememberActiveModelFromBody(body) {
  const parsed = parseBodyObject(body);
  const modelId = String(parsed.model || '').trim();
  if (catalog.entries[modelId]) {
    activeModelId = modelId;
  }
}

function remapHardcodedClaudeBackgroundModel(parsed) {
  const requestedModel = String(parsed.model || '').trim();
  const active = activeProvider();
  if (!active || active === 'claude') {
    return {parsed, remapped: false};
  }
  const matrix = providerProfiles.background_resolution_matrix || {};
  const providerMatrix = matrix[active] || {};
  const providerLocal = providerMatrix.provider_local || {};
  const aliases = new Set(['claude-haiku-4-5-20251001', 'claude-haiku', 'claude-3-haiku', 'claude-3-5-haiku', 'fast', 'simple', 'haiku']);
  if (!aliases.has(requestedModel)) {
    return {parsed, remapped: false};
  }
  const resolution = providerLocal.haiku || providerLocal.fast || providerLocal.simple;
  const targetModel = resolution && resolution.resolved_model_id ? String(resolution.resolved_model_id) : '';
  const targetEntry = targetModel ? catalog.entries[targetModel] : undefined;
  if (!targetEntry || !targetEntry.live_enabled || targetEntry.provider !== active || targetEntry.formal_pool_allowed || targetEntry.native_attestation_allowed) {
    throw new Error('ZHUMENG provider profile refused unsafe background remap for ' + active);
  }
  return {parsed: {...parsed, model: targetModel}, remapped: true};
}

function sanitizeBridgeEffort(parsed) {
  const modelId = String(parsed.model || '').trim();
  const entry = catalog.entries[modelId];
  if (!entry || entry.provider === 'claude') {
    return {parsed, changed: false};
  }
  const outputConfig = parsed.output_config && typeof parsed.output_config === 'object' && !Array.isArray(parsed.output_config)
    ? parsed.output_config
    : undefined;
  if (!outputConfig || typeof outputConfig.effort !== 'string') {
    return {parsed, changed: false};
  }
  const requested = String(outputConfig.effort || '').trim().toLowerCase();
  const levels = Array.isArray(entry.reasoning_effort_levels) ? entry.reasoning_effort_levels.map((level) => String(level)) : [];
  if (!levels.length) {
    const nextOutputConfig = {...outputConfig};
    delete nextOutputConfig.effort;
    const nextParsed = {...parsed};
    if (Object.keys(nextOutputConfig).length) {
      nextParsed.output_config = nextOutputConfig;
    } else {
      delete nextParsed.output_config;
    }
    return {parsed: nextParsed, changed: true};
  }
  let resolved = requested;
  if (entry.provider === 'openai' && requested === 'max') {
    resolved = 'xhigh';
  } else if ((entry.provider === 'deepseek' || entry.provider === 'zai_glm') && (requested === 'low' || requested === 'medium')) {
    resolved = 'high';
  } else if ((entry.provider === 'deepseek' || entry.provider === 'zai_glm') && requested === 'xhigh') {
    resolved = 'max';
  } else if (entry.provider === 'zai_glm' && requested === 'ultracode') {
    resolved = 'max';
  }
  if (!levels.includes(resolved)) {
    resolved = levels[0];
  }
  if (resolved === requested) {
    return {parsed, changed: false};
  }
  return {parsed: {...parsed, output_config: {...outputConfig, effort: resolved}}, changed: true};
}

function normalizedBodyBuffer(body) {
  const parsed = parseBodyObject(body);
  const remapped = remapHardcodedClaudeBackgroundModel(parsed);
  const sanitized = sanitizeBridgeEffort(remapped.parsed);
  if (!remapped.remapped && !sanitized.changed) return {body: Buffer.from(body), remapped: false, active_provider: activeProvider(), resolved_model_id: String(parsed.model || '').trim()};
  return {body: Buffer.from(canonicalJson(sanitized.parsed)), remapped: remapped.remapped, active_provider: activeProvider(), resolved_model_id: String(sanitized.parsed.model || '').trim()};
}

function routeEntryForBody(body) {
  const parsed = parseBodyObject(body);
  const modelId = String(parsed.model || '').trim();
  const entry = catalog.entries[modelId];
  if (!entry) {
    throw new Error('ZHUMENG route hint unknown model: ' + modelId);
  }
  return {modelId, entry};
}

function requiresRouteHint(entry) {
  // Claude native formal-pool requests are guarded by the server-side catalog
  // fallback plus native attestation over the final bytes. Keeping route hints
  // off this path avoids trusting a pre-send body snapshot from the packed CLI.
  return !(entry.provider === 'claude' && entry.route === 'claude_code_native' && entry.client_type === 'claude_code_native');
}

function signedHeaders(headers, body, requestPath) {
  const {modelId, entry} = routeEntryForBody(body);
  if (!requiresRouteHint(entry)) {
    return headers;
  }
  const sessionRef = headers.get('x-claude-code-session-id') || process.env.ZHUMENG_CLAUDE_ROUTE_HINT_SESSION_REF || '';
  if (!sessionRef) {
    throw new Error('ZHUMENG route hint requires x-claude-code-session-id');
  }
  const issuedAt = Math.floor(Date.now() / 1000);
  const payload = {
    body_model: modelId,
    body_sha256: 'sha256:' + sha256Hex(body),
    catalog_hash: catalog.catalog_hash,
    catalog_version: catalog.catalog_version,
    client_type: entry.client_type,
    credential_scope: entry.credential_scope,
    expires_at: issuedAt + 60,
    formal_pool_allowed: Boolean(entry.formal_pool_allowed),
    gateway_location: entry.gateway_location,
    issued_at: issuedAt,
    key_id: 'route_hint_v1',
    live_request_allowed: Boolean(entry.live_enabled),
    method: 'POST',
    model_id: modelId,
    native_attestation_allowed: Boolean(entry.native_attestation_allowed),
    nonce: crypto.randomBytes(16).toString('hex'),
    provider: entry.provider,
    provider_owner: entry.provider_owner,
    request_uri: requestPath,
    route: entry.route,
    runtime_hash: catalog.runtime_hash,
    scope: SCOPE,
    session_ref: sessionRef,
    overlay_hash: catalog.overlay_hash,
    version: VERSION
  };
  const encoded = b64url(Buffer.from(canonicalJson(payload)));
  headers.set(HINT_HEADER, encoded);
  headers.set(SIGNATURE_HEADER, sign(encoded, requestPath, body));
  return headers;
}

globalThis.fetch = async function zhumengRouteHintFetch(input, init) {
  const requestUrl = typeof input === 'string' || input instanceof URL ? String(input) : input.url;
  const requestPath = routePath(requestUrl);
  const method = (init && init.method) || (typeof input === 'object' && input && input.method) || 'GET';
  if (!shouldSign(method, requestPath)) {
    return originalFetch.call(this, input, init);
  }
  const finalRequest = typeof Request === 'function' ? new Request(input, init) : null;
  if (!finalRequest) {
    const headers = new Headers((typeof input === 'object' && input && input.headers) || undefined);
    if (init && init.headers) {
      new Headers(init.headers).forEach((value, key) => headers.set(key, value));
    }
    let body = init && Object.prototype.hasOwnProperty.call(init, 'body') ? init.body : undefined;
    if (body === undefined && typeof input === 'object' && input && typeof input.clone === 'function') {
      body = Buffer.from(await input.clone().arrayBuffer());
    }
    const normalized = normalizedBodyBuffer(bodyBuffer(body));
    const bodyBytes = normalized.body;
    signedHeaders(headers, bodyBytes, requestPath);
    rememberActiveModelFromBody(bodyBytes);
    return originalFetch.call(this, input, {...(init || {}), method: 'POST', headers, body: bodyBytes});
  }
  const normalized = normalizedBodyBuffer(Buffer.from(await finalRequest.clone().arrayBuffer()));
  const bodyBytes = normalized.body;
  const headers = new Headers(finalRequest.headers);
  signedHeaders(headers, bodyBytes, requestPath);
  rememberActiveModelFromBody(bodyBytes);
  headers.set('content-length', String(bodyBytes.length));
  return originalFetch.call(this, finalRequest.url, {method: 'POST', headers, body: bodyBytes});
};

function requestOptionsToUrl(input, options, defaultProtocol) {
  if (typeof input === 'string' || input instanceof URL) {
    return new URL(String(input));
  }
  const merged = {...(input || {}), ...(options || {})};
  const protocol = merged.protocol || defaultProtocol || 'http:';
  const host = merged.hostname || merged.host || '127.0.0.1';
  const port = merged.port ? ':' + String(merged.port) : '';
  const path = merged.path || merged.pathname || '/';
  return new URL(protocol + '//' + host + port + path);
}

function normalizeHeadersObject(value) {
  const headers = new Headers();
  if (!value) return headers;
  if (Array.isArray(value)) {
    for (const pair of value) {
      if (Array.isArray(pair) && pair.length >= 2) headers.set(String(pair[0]), String(pair[1]));
    }
    return headers;
  }
  if (typeof value[Symbol.iterator] === 'function' && typeof value !== 'string') {
    for (const pair of value) {
      if (Array.isArray(pair) && pair.length >= 2) headers.set(String(pair[0]), String(pair[1]));
    }
    return headers;
  }
  for (const [key, item] of Object.entries(value)) {
    if (Array.isArray(item)) headers.set(key, item.join(', '));
    else if (item !== undefined) headers.set(key, String(item));
  }
  return headers;
}

function headersToPlainObject(headers) {
  const result = {};
  headers.forEach((value, key) => { result[key] = value; });
  return result;
}

function splitRequestArgs(args) {
  let input = args[0];
  let options = {};
  let callback;
  if (typeof args[1] === 'function') {
    callback = args[1];
  } else {
    options = args[1] || {};
    callback = typeof args[2] === 'function' ? args[2] : undefined;
  }
  if (input === undefined || typeof input === 'function') {
    input = {};
    options = {};
    callback = typeof args[0] === 'function' ? args[0] : undefined;
  }
  return {input, options, callback};
}

function patchedRequest(originalRequest, protocol) {
  return function zhumengRouteHintRequest(...args) {
    const {input, options, callback} = splitRequestArgs(args);
    const url = requestOptionsToUrl(input, options, protocol);
    if (!url.protocol) url.protocol = protocol;
    const requestPath = url.pathname + url.search;
    const method = String((options && options.method) || (input && input.method) || 'GET').toUpperCase();
    const shouldPatch = shouldSign(method, requestPath);
    if (!shouldPatch) {
      return originalRequest.apply(this, args);
    }
    const existingHeaders = normalizeHeadersObject((input && input.headers) || undefined);
    normalizeHeadersObject((options && options.headers) || undefined).forEach((value, key) => existingHeaders.set(key, value));
    let chunks = [];
    let finalized = false;
    const baseOptions = {
      ...(typeof input === 'object' && !(input instanceof URL) ? input : {}),
      ...(options || {}),
      protocol: url.protocol,
      hostname: url.hostname,
      port: url.port,
      path: requestPath,
      method: 'POST',
      headers: headersToPlainObject(existingHeaders)
    };
    const req = originalRequest.call(this, baseOptions, callback);
    const originalWrite = req.write.bind(req);
    const originalEnd = req.end.bind(req);
    function appendChunk(chunk, encoding) {
      if (chunk === undefined || chunk === null) return;
      if (typeof chunk === 'string') chunks.push(Buffer.from(chunk, encoding));
      else if (Buffer.isBuffer(chunk)) chunks.push(Buffer.from(chunk));
      else if (chunk instanceof Uint8Array) chunks.push(Buffer.from(chunk));
      else chunks.push(Buffer.from(String(chunk), encoding));
    }
    req.write = function zhumengRouteHintWrite(chunk, encoding, cb) {
      appendChunk(chunk, typeof encoding === 'string' ? encoding : undefined);
      if (typeof encoding === 'function') encoding();
      if (typeof cb === 'function') cb();
      return true;
    };
    function normalizeEndArgs(chunk, encoding, cb) {
      if (typeof chunk === 'function') {
        return {chunk: undefined, encoding: undefined, callback: chunk};
      }
      if (typeof encoding === 'function') {
        return {chunk, encoding: undefined, callback: encoding};
      }
      return {chunk, encoding, callback: cb};
    }
    req.end = function zhumengRouteHintEnd(chunk, encoding, cb) {
      const endArgs = normalizeEndArgs(chunk, encoding, cb);
      if (finalized) return originalEnd(endArgs.chunk, endArgs.encoding, endArgs.callback);
      finalized = true;
      appendChunk(endArgs.chunk, typeof endArgs.encoding === 'string' ? endArgs.encoding : undefined);
      try {
        const normalized = normalizedBodyBuffer(Buffer.concat(chunks));
        const body = normalized.body;
        const headers = normalizeHeadersObject(baseOptions.headers);
        signedHeaders(headers, body, requestPath);
        rememberActiveModelFromBody(body);
        headers.set('content-length', String(body.length));
        for (const [key, value] of Object.entries(headersToPlainObject(headers))) {
          req.setHeader(key, value);
        }
        originalEnd(body, endArgs.callback);
      } catch (err) {
        process.nextTick(() => req.emit('error', err));
        originalEnd();
      }
      return req;
    };
    return req;
  };
}

http.request = patchedRequest(originalHttpRequest, 'http:');
https.request = patchedRequest(originalHttpsRequest, 'https:');
"""
