from __future__ import annotations

import hashlib
import json
import os
import shutil
import subprocess
from dataclasses import asdict, dataclass, field
from pathlib import Path
from typing import Mapping

from .launcher import Runner, detect_claude_code_version

RUNTIME_NAME = "claude-code"
ZHUMENG_RUNTIME_VERSION = "0.1.0"
SUPPORTED_UPSTREAM_VERSIONS = frozenset({"2.1.175", "2.1.177"})
DEFAULT_PATCH_POINTS = (
    "runtime_manifest",
    "hash_lock",
    "isolated_config",
    "guard_env",
)
AGENT_MODEL_SCHEMA_PATCH_POINT = "agent_model_schema"
EFFORT_CAPABILITY_PATCH_POINT = "effort_capability_hook"
EXACT_EFFORT_LEVEL_UI_PATCH_POINT = "exact_effort_level_ui_patch"
EFFORT_CAPABILITY_ENV_VAR = "ZHUMENG_CLAUDE_MODEL_CAPABILITIES_JSON"
AGENT_MODEL_SCHEMA_ENUM_NEEDLE = b'k.enum(["sonnet","opus","haiku","fable"]).optional()'
AGENT_MODEL_SCHEMA_STRING_PATCH = b"k.string().min(1).max(128).optional()               "
EFFORT_CAPABILITY_HOOK_NEEDLE = b'var QP5,us;var oK6=L(()=>{c7();V7();QP5=[{modelEnvVar:"ANTHROPIC_DEFAULT_FABLE_MODEL",capabilitiesEnvVar:"ANTHROPIC_DEFAULT_FABLE_MODEL_SUPPORTED_CAPABILITIES"},{modelEnvVar:"ANTHROPIC_DEFAULT_OPUS_MODEL",capabilitiesEnvVar:"ANTHROPIC_DEFAULT_OPUS_MODEL_SUPPORTED_CAPABILITIES"},{modelEnvVar:"ANTHROPIC_DEFAULT_SONNET_MODEL",capabilitiesEnvVar:"ANTHROPIC_DEFAULT_SONNET_MODEL_SUPPORTED_CAPABILITIES"},{modelEnvVar:"ANTHROPIC_DEFAULT_HAIKU_MODEL",capabilitiesEnvVar:"ANTHROPIC_DEFAULT_HAIKU_MODEL_SUPPORTED_CAPABILITIES"},{modelEnvVar:"ANTHROPIC_CUSTOM_MODEL_OPTION",capabilitiesEnvVar:"ANTHROPIC_CUSTOM_MODEL_OPTION_SUPPORTED_CAPABILITIES"}],us=V6((H,_)=>{if(OO())return;let q=H.toLowerCase();for(let K of QP5){let O=process.env[K.modelEnvVar],T=process.env[K.capabilitiesEnvVar];if(!O||T===void 0)continue;if(q!==O.toLowerCase())continue;return T.toLowerCase().split(",").map((z)=>z.trim()).includes(_)}return},(H,_)=>`${H.toLowerCase()}:${_}`)});'
EFFORT_CAPABILITY_HOOK_REPLACEMENT_BASE = b'var QP5,us;var oK6=L(()=>{c7();V7();QP5=[["ANTHROPIC_DEFAULT_FABLE_MODEL","ANTHROPIC_DEFAULT_FABLE_MODEL_SUPPORTED_CAPABILITIES"],["ANTHROPIC_DEFAULT_OPUS_MODEL","ANTHROPIC_DEFAULT_OPUS_MODEL_SUPPORTED_CAPABILITIES"],["ANTHROPIC_DEFAULT_SONNET_MODEL","ANTHROPIC_DEFAULT_SONNET_MODEL_SUPPORTED_CAPABILITIES"],["ANTHROPIC_DEFAULT_HAIKU_MODEL","ANTHROPIC_DEFAULT_HAIKU_MODEL_SUPPORTED_CAPABILITIES"],["ANTHROPIC_CUSTOM_MODEL_OPTION","ANTHROPIC_CUSTOM_MODEL_OPTION_SUPPORTED_CAPABILITIES"]],us=V6((H,_)=>{let q=H.toLowerCase(),J=process.env.ZHUMENG_CLAUDE_MODEL_CAPABILITIES_JSON;if(J)try{let M=JSON.parse(J)[q];if(M&&M[_]!=null)return!!M[_]}catch{}if(OO())return;for(let K of QP5){let O=process.env[K[0]],T=process.env[K[1]];if(!O||T===void 0)continue;if(q!==O.toLowerCase())continue;return T.toLowerCase().split(",").map((z)=>z.trim()).includes(_)}return},(H,_)=>`${H.toLowerCase()}:${_}`)});'
EFFORT_CAPABILITY_HOOK_SLACK_BYTES = len(EFFORT_CAPABILITY_HOOK_NEEDLE) - len(EFFORT_CAPABILITY_HOOK_REPLACEMENT_BASE)
EXACT_EFFORT_LEVEL_UI_PATCH_MIN_EXTRA_BYTES = 96
EFFORT_CAPABILITY_HOOK_REPLACEMENT = EFFORT_CAPABILITY_HOOK_REPLACEMENT_BASE + (b" " * (len(EFFORT_CAPABILITY_HOOK_NEEDLE) - len(EFFORT_CAPABILITY_HOOK_REPLACEMENT_BASE)))
GLOBAL_CLAUDE_BINARY_PATHS = frozenset({
    Path("/opt/homebrew/bin/claude"),
    Path("/usr/local/bin/claude"),
})
MANDATORY_HASH_LOCK_FILES = frozenset({"manifest.json", "patches.json"})


class RuntimeInstallerError(RuntimeError):
    pass


@dataclass(frozen=True, slots=True)
class ManagedRuntimeManifest:
    runtime: str
    upstream_version: str
    zhumeng_runtime_version: str
    source: str
    upstream_hash: str
    overlay_hash: str
    patch_points: tuple[str, ...]
    cch_profile: str
    status: str
    executable_path: str = ""

    def to_dict(self) -> dict[str, object]:
        data = asdict(self)
        data["patch_points"] = list(self.patch_points)
        return data


@dataclass(frozen=True, slots=True)
class ActiveManagedRuntime:
    executable: Path
    runtime_root: Path
    upstream_version: str
    manifest_path: Path
    manifest: Mapping[str, object]
    patches: Mapping[str, object]
    runtime_hash: str
    overlay_hash: str
    cch_profile: str


@dataclass(frozen=True, slots=True)
class ManagedRuntimeInstallPlan:
    executable: Path
    runtime_root: Path
    upstream_version: str
    runtime_dir: Path
    version_dir: Path
    cache_path: Path
    manifest_path: Path
    patches_path: Path
    hash_lock_path: Path
    rollback_metadata_path: Path
    active_pointer: Path
    manifest: ManagedRuntimeManifest
    patches: Mapping[str, object] = field(repr=False)
    rollback_metadata: Mapping[str, object]
    planned_write_paths: tuple[Path, ...]


@dataclass(frozen=True, slots=True)
class ShellAliasPlan:
    action: str
    shell_rc: Path
    alias_name: str
    target_command: str
    marker_start: str = "# >>> zhumeng-claude managed alias >>>"
    marker_end: str = "# <<< zhumeng-claude managed alias <<<"


def build_managed_runtime_install_plan(
    *,
    executable: Path | str,
    runtime_root: Path,
    runner: Runner | None = None,
    profile_id: str = "prod",
    supported_versions: frozenset[str] = SUPPORTED_UPSTREAM_VERSIONS,
) -> ManagedRuntimeInstallPlan:
    runtime_root = runtime_root.expanduser()
    executable_path = _resolve_runtime_executable(Path(executable))
    version_probe_root = runtime_root / RUNTIME_NAME / "cache" / "version-probe"
    version_probe_config = version_probe_root / "config"
    version_probe_home = version_probe_root / "home"
    version_probe_xdg_config = version_probe_root / "xdg-config"
    ensure_managed_runtime_write_path(version_probe_config, runtime_root=runtime_root)
    ensure_managed_runtime_write_path(version_probe_home, runtime_root=runtime_root)
    ensure_managed_runtime_write_path(version_probe_xdg_config, runtime_root=runtime_root)
    try:
        detected = detect_claude_code_version(
            executable=executable_path,
            runner=runner,
            env={
                "CLAUDE_CONFIG_DIR": str(version_probe_config),
                "HOME": str(version_probe_home),
                "PATH": os.environ.get("PATH", ""),
                "XDG_CONFIG_HOME": str(version_probe_xdg_config),
            },
        )
    except Exception as exc:
        raise RuntimeInstallerError("unable to detect Claude Code version; refusing managed runtime install") from exc
    if detected.returncode != 0:
        raise RuntimeInstallerError("Claude Code version probe failed; refusing managed runtime install")
    upstream_version = detected.version
    if upstream_version is None:
        raise RuntimeInstallerError("unknown Claude Code version; refusing managed runtime install")
    if upstream_version not in supported_versions:
        raise RuntimeInstallerError(f"unsupported Claude Code version: {upstream_version}")

    runtime_dir = runtime_root / RUNTIME_NAME
    version_dir = runtime_dir / upstream_version
    cache_path = runtime_dir / "cache" / upstream_version
    manifest_path = version_dir / "manifest.json"
    patches_path = version_dir / "patches.json"
    hash_lock_path = version_dir / "hash.lock"
    rollback_metadata_path = version_dir / "rollback.json"
    active_pointer = runtime_dir / "active"
    cch_profile = "claude_code_" + upstream_version.replace(".", "_")
    source = f"npm:@anthropic-ai/claude-code@{upstream_version}"
    executable_resolved = executable_path.expanduser().resolve(strict=False)
    upstream_hash = _hash_existing_file(executable_resolved) or _stable_sha256(
        {
            "executable": str(executable_resolved),
            "raw_version": detected.raw_output,
            "source": source,
        }
    )
    overlay_hash = _stable_sha256(
        {
            "patch_points": DEFAULT_PATCH_POINTS,
            "profile_id": profile_id,
            "runtime_version": ZHUMENG_RUNTIME_VERSION,
        }
    )
    manifest = ManagedRuntimeManifest(
        runtime=RUNTIME_NAME,
        upstream_version=upstream_version,
        zhumeng_runtime_version=ZHUMENG_RUNTIME_VERSION,
        source=source,
        upstream_hash=upstream_hash,
        overlay_hash=overlay_hash,
        patch_points=DEFAULT_PATCH_POINTS,
        cch_profile=cch_profile,
        status="ready",
        executable_path=str(executable_resolved),
    )
    patches: dict[str, object] = {
        "runtime": RUNTIME_NAME,
        "upstream_version": upstream_version,
        "patch_points": list(DEFAULT_PATCH_POINTS),
        "live_bridge_models_enabled": False,
    }
    rollback_metadata: dict[str, object] = {
        "runtime": RUNTIME_NAME,
        "from_version": None,
        "to_version": upstream_version,
        "active_pointer": str(active_pointer),
        "global_overwrite": False,
    }
    planned_write_paths = (
        manifest_path,
        patches_path,
        hash_lock_path,
        rollback_metadata_path,
    )
    for path in planned_write_paths:
        ensure_managed_runtime_write_path(path, runtime_root=runtime_root)

    return ManagedRuntimeInstallPlan(
        executable=executable_path,
        runtime_root=runtime_root,
        upstream_version=upstream_version,
        runtime_dir=runtime_dir,
        version_dir=version_dir,
        cache_path=cache_path,
        manifest_path=manifest_path,
        patches_path=patches_path,
        hash_lock_path=hash_lock_path,
        rollback_metadata_path=rollback_metadata_path,
        active_pointer=active_pointer,
        manifest=manifest,
        patches=patches,
        rollback_metadata=rollback_metadata,
        planned_write_paths=planned_write_paths,
    )


def write_managed_runtime_artifacts(plan: ManagedRuntimeInstallPlan) -> None:
    manifest_path = ensure_managed_runtime_write_path(plan.manifest_path, runtime_root=plan.runtime_root)
    patches_path = ensure_managed_runtime_write_path(plan.patches_path, runtime_root=plan.runtime_root)
    rollback_metadata_path = ensure_managed_runtime_write_path(plan.rollback_metadata_path, runtime_root=plan.runtime_root)
    hash_lock_path = ensure_managed_runtime_write_path(plan.hash_lock_path, runtime_root=plan.runtime_root)
    active_pointer = ensure_managed_runtime_write_path(plan.active_pointer, runtime_root=plan.runtime_root)
    version_dir = ensure_managed_runtime_write_path(plan.version_dir, runtime_root=plan.runtime_root)
    cache_path = ensure_managed_runtime_write_path(plan.cache_path, runtime_root=plan.runtime_root)
    for path in plan.planned_write_paths:
        ensure_managed_runtime_write_path(path, runtime_root=plan.runtime_root)
    version_dir.mkdir(parents=True, exist_ok=True)
    cache_path.mkdir(parents=True, exist_ok=True)

    manifest_bytes = _canonical_json_bytes(plan.manifest.to_dict())
    patches_bytes = _canonical_json_bytes(dict(plan.patches))
    rollback_bytes = _canonical_json_bytes(dict(plan.rollback_metadata))
    hash_lock = {
        "runtime": RUNTIME_NAME,
        "upstream_version": plan.upstream_version,
        "manifest_hash": _sha256_bytes(manifest_bytes),
        "overlay_hash": plan.manifest.overlay_hash,
        "upstream_hash": plan.manifest.upstream_hash,
        "locked_files": {
            "manifest.json": _sha256_bytes(manifest_bytes),
            "patches.json": _sha256_bytes(patches_bytes),
            "rollback.json": _sha256_bytes(rollback_bytes),
        },
    }

    manifest_path.write_bytes(manifest_bytes)
    patches_path.write_bytes(patches_bytes)
    rollback_metadata_path.write_bytes(rollback_bytes)
    hash_lock_path.write_bytes(_canonical_json_bytes(hash_lock))
    active_pointer.write_bytes(_canonical_json_bytes({
        "runtime": RUNTIME_NAME,
        "status": "enabled",
        "active_version": plan.upstream_version,
        "manifest_path": str(plan.manifest_path),
        "global_overwrite": False,
        "official_claude_unaffected": True,
    }))


def apply_managed_runtime_agent_model_schema_patch(runtime_root: Path, executable: Path | str) -> dict[str, object]:
    """Widen the managed Claude Code Agent model schema inside the isolated runtime.

    Claude Code 2.1.177 validates the Agent tool's `model` field with a packed
    enum of Claude aliases. The managed runtime needs the field to accept our
    signed overlay display IDs; route trust and the guard still decide whether a
    model can go live. This patch is intentionally scoped to the active managed
    executable and never targets the globally installed `claude` binary.
    """
    runtime_root = runtime_root.expanduser()
    manifest_path = _active_manifest_path(runtime_root)
    executable_path = _active_manifest_executable_path(manifest_path, runtime_root=runtime_root)
    requested_executable = Path(executable).expanduser().resolve(strict=False)
    if requested_executable != executable_path:
        raise RuntimeInstallerError("managed Claude Code runtime executable drift before Agent schema patch")
    before_hash = _hash_existing_file(executable_path)
    if not before_hash:
        raise RuntimeInstallerError("managed Claude Code runtime executable is missing")
    data = executable_path.read_bytes()
    status = "already_patched"
    if AGENT_MODEL_SCHEMA_ENUM_NEEDLE in data:
        data = data.replace(AGENT_MODEL_SCHEMA_ENUM_NEEDLE, AGENT_MODEL_SCHEMA_STRING_PATCH, 1)
        executable_path.write_bytes(data)
        _codesign_ad_hoc_if_macho(executable_path)
        status = "patched"
    elif AGENT_MODEL_SCHEMA_STRING_PATCH.rstrip() not in data:
        raise RuntimeInstallerError("managed Claude Code Agent model schema patch point not found")

    after_hash = _hash_existing_file(executable_path)
    if not after_hash:
        raise RuntimeInstallerError("managed Claude Code runtime executable is missing after patch")
    _update_managed_runtime_hash_metadata(
        runtime_root=runtime_root,
        manifest_path=manifest_path,
        runtime_hash=after_hash,
        patch_status=status,
        before_hash=before_hash,
    )
    return {
        "status": status,
        "patched_executable": str(executable_path),
        "runtime_hash_before": before_hash,
        "runtime_hash_after": after_hash,
        "patch_point": AGENT_MODEL_SCHEMA_PATCH_POINT,
        "official_claude_unaffected": True,
    }


def apply_managed_runtime_effort_capability_patch(runtime_root: Path, executable: Path | str, *, approved: bool = False) -> dict[str, object]:
    """Patch managed Claude Code 2.1.177 to read per-model effort capabilities.

    The packed 2.1.177 /model UI strips gateway model metadata and recomputes
    effort support through its internal `us(model, key)` hook. This patch adds
    a managed-runtime-only JSON env hook before the original Anthropic custom
    model capability fallback. It is not applied by default; rollout remains a
    separate approval checkpoint.
    """
    if not approved:
        raise RuntimeInstallerError("managed Claude Code effort capability patch requires explicit approval")
    if len(EFFORT_CAPABILITY_HOOK_REPLACEMENT) != len(EFFORT_CAPABILITY_HOOK_NEEDLE):
        raise RuntimeInstallerError("managed Claude Code effort capability patch is not length preserving")
    runtime_root = runtime_root.expanduser()
    manifest_path = _active_manifest_path(runtime_root)
    executable_path = _active_manifest_executable_path(manifest_path, runtime_root=runtime_root)
    requested_executable = Path(executable).expanduser().resolve(strict=False)
    if requested_executable != executable_path:
        raise RuntimeInstallerError("managed Claude Code runtime executable drift before effort capability patch")
    before_hash = _hash_existing_file(executable_path)
    if not before_hash:
        raise RuntimeInstallerError("managed Claude Code runtime executable is missing")
    data = executable_path.read_bytes()
    status = "already_patched"
    if EFFORT_CAPABILITY_HOOK_NEEDLE in data:
        data = data.replace(EFFORT_CAPABILITY_HOOK_NEEDLE, EFFORT_CAPABILITY_HOOK_REPLACEMENT, 1)
        executable_path.write_bytes(data)
        _codesign_ad_hoc_if_macho(executable_path)
        status = "patched"
    elif EFFORT_CAPABILITY_HOOK_REPLACEMENT.rstrip() not in data:
        raise RuntimeInstallerError("managed Claude Code effort capability patch point not found")

    after_hash = _hash_existing_file(executable_path)
    if not after_hash:
        raise RuntimeInstallerError("managed Claude Code runtime executable is missing after patch")
    _update_managed_runtime_hash_metadata(
        runtime_root=runtime_root,
        manifest_path=manifest_path,
        runtime_hash=after_hash,
        patch_status=status,
        before_hash=before_hash,
        patch_point=EFFORT_CAPABILITY_PATCH_POINT,
        patch_metadata={
            "env": EFFORT_CAPABILITY_ENV_VAR,
            "schema": "boolean_effort_flags_v1",
            "hook": "us_model_capabilities",
            "ui_probe": "boolean_only_insufficient_for_exact_effort_levels",
            "exact_effort_levels_supported": False,
            "boolean_only_hook_rejected": True,
            "direct_binary_patch_requires_approval": True,
            "global_binary_touched": False,
        },
    )
    return {
        "status": status,
        "patched_executable": str(executable_path),
        "runtime_hash_before": before_hash,
        "runtime_hash_after": after_hash,
        "patch_point": EFFORT_CAPABILITY_PATCH_POINT,
        "env": EFFORT_CAPABILITY_ENV_VAR,
        "official_claude_unaffected": True,
        "direct_binary_patch_requires_approval": True,
    }


def read_managed_runtime_status(runtime_root: Path) -> dict[str, object]:
    runtime_root = runtime_root.expanduser()
    runtime_dir = runtime_root / RUNTIME_NAME
    active_pointer = runtime_dir / "active"
    if not active_pointer.exists():
        return {
            "runtime": RUNTIME_NAME,
            "status": "not_installed",
            "active_version": None,
            "integrity": {"status": "missing_active_pointer"},
            "official_claude_unaffected": True,
            "destructive_cleanup_requires_confirmation": True,
        }
    try:
        active = json.loads(active_pointer.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return {
            "runtime": RUNTIME_NAME,
            "status": "invalid_active_pointer",
            "active_version": None,
            "integrity": {"status": "invalid_active_pointer"},
            "official_claude_unaffected": True,
            "destructive_cleanup_requires_confirmation": True,
        }
    if not isinstance(active, dict):
        active = {}
    active_version = str(active.get("active_version") or "") or None
    manifest_path = active.get("manifest_path")
    status = str(active.get("status") or ("enabled" if active_version else "not_installed"))
    integrity = _runtime_integrity(manifest_path)
    if status == "enabled" and integrity.get("status") != "pass":
        status = "integrity_failed"
    return {
        "runtime": RUNTIME_NAME,
        "status": status,
        "active_version": active_version,
        "active_pointer": str(active_pointer),
        "manifest_path": str(manifest_path) if manifest_path else "",
        "integrity": integrity,
        "official_claude_unaffected": True,
        "destructive_cleanup_requires_confirmation": True,
    }



def resolve_active_managed_runtime(runtime_root: Path) -> ActiveManagedRuntime:
    """Resolve the active managed Claude Code runtime and fail closed on drift."""
    runtime_root = runtime_root.expanduser()
    status = read_managed_runtime_status(runtime_root)
    if status.get("status") != "enabled":
        raise RuntimeInstallerError("managed Claude Code runtime is not enabled; run zhumeng-claude install first")
    manifest_path_value = status.get("manifest_path")
    if not manifest_path_value:
        raise RuntimeInstallerError("managed Claude Code runtime manifest is missing")
    manifest_path = ensure_managed_runtime_write_path(Path(str(manifest_path_value)), runtime_root=runtime_root)
    try:
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise RuntimeInstallerError("managed Claude Code runtime manifest is unreadable") from exc
    if not isinstance(manifest, dict):
        raise RuntimeInstallerError("managed Claude Code runtime manifest is invalid")
    if manifest.get("runtime") != RUNTIME_NAME or manifest.get("status") != "ready":
        raise RuntimeInstallerError("managed Claude Code runtime manifest is invalid")
    upstream_version = str(manifest.get("upstream_version") or "")
    if upstream_version not in SUPPORTED_UPSTREAM_VERSIONS:
        raise RuntimeInstallerError("managed Claude Code runtime version is unsupported")
    executable_value = str(manifest.get("executable_path") or "").strip()
    if not executable_value:
        raise RuntimeInstallerError("managed Claude Code runtime executable is missing from manifest")
    raw_executable = Path(executable_value).expanduser()
    if not raw_executable.is_absolute():
        raise RuntimeInstallerError("managed Claude Code runtime manifest must contain an absolute executable path")
    executable = raw_executable.resolve(strict=False)
    if raw_executable in GLOBAL_CLAUDE_BINARY_PATHS or executable in GLOBAL_CLAUDE_BINARY_PATHS:
        raise RuntimeInstallerError("managed Claude Code runtime refuses to use global Claude Code binary")
    runtime_hash = _hash_existing_file(executable)
    if not runtime_hash:
        raise RuntimeInstallerError("managed Claude Code runtime executable is missing")
    if runtime_hash != str(manifest.get("upstream_hash") or ""):
        raise RuntimeInstallerError("managed Claude Code runtime executable hash mismatch")
    overlay_hash = str(manifest.get("overlay_hash") or "")
    if not overlay_hash.startswith("sha256:"):
        raise RuntimeInstallerError("managed Claude Code runtime overlay hash is invalid")
    patches_path = manifest_path.parent / "patches.json"
    patches: Mapping[str, object] = {}
    try:
        loaded_patches = json.loads(patches_path.read_text(encoding="utf-8")) if patches_path.exists() else {}
        if isinstance(loaded_patches, dict):
            patches = loaded_patches
    except (OSError, json.JSONDecodeError):
        raise RuntimeInstallerError("managed Claude Code runtime patches are unreadable")
    return ActiveManagedRuntime(
        executable=executable,
        runtime_root=runtime_root,
        upstream_version=upstream_version,
        manifest_path=manifest_path,
        manifest=manifest,
        patches=patches,
        runtime_hash=runtime_hash,
        overlay_hash=overlay_hash,
        cch_profile=str(manifest.get("cch_profile") or ""),
    )


def disable_managed_runtime(runtime_root: Path) -> dict[str, object]:
    runtime_root = runtime_root.expanduser()
    active_pointer = ensure_managed_runtime_write_path(runtime_root / RUNTIME_NAME / "active", runtime_root=runtime_root)
    previous = read_managed_runtime_status(runtime_root)
    active_version = previous.get("active_version")
    manifest_path = previous.get("manifest_path") or ""
    active_pointer.parent.mkdir(parents=True, exist_ok=True)
    payload = {
        "runtime": RUNTIME_NAME,
        "status": "disabled",
        "active_version": active_version,
        "manifest_path": manifest_path,
        "rollback_action": "disable_active_pointer_without_delete",
        "global_overwrite": False,
        "official_claude_unaffected": True,
        "requires_user_confirmation_for_delete": True,
    }
    active_pointer.write_bytes(_canonical_json_bytes(payload))
    return dict(payload)


def build_shell_alias_plan(
    *,
    action: str,
    shell_rc: Path,
    alias_name: str = "zhumeng-claude",
    target_command: str = "zhumeng-claude",
) -> ShellAliasPlan:
    action = str(action).strip().lower()
    alias_name = str(alias_name).strip()
    target_command = str(target_command).strip()
    if action not in {"enable", "disable", "status"}:
        raise RuntimeInstallerError(f"unsupported shell alias action: {action}")
    if alias_name == "claude" or target_command == "claude" or target_command.endswith("/claude"):
        raise RuntimeInstallerError("refuses to alias official Claude Code")
    return ShellAliasPlan(action=action, shell_rc=Path(shell_rc).expanduser(), alias_name=alias_name, target_command=target_command)


def apply_shell_alias_plan(plan: ShellAliasPlan) -> dict[str, object]:
    shell_rc = plan.shell_rc
    existing = shell_rc.read_text(encoding="utf-8") if shell_rc.exists() else ""
    managed_block = _shell_alias_block(plan) if plan.action == "enable" else _disabled_shell_alias_block(plan)
    if plan.action == "status":
        return {
            "status": "enabled" if _extract_managed_block(existing, plan) and f"alias {plan.alias_name}=" in existing else "disabled",
            "shell_rc": str(shell_rc),
            "alias_name": plan.alias_name,
            "official_claude_unaffected": True,
        }
    updated = _replace_managed_block(existing, plan, managed_block)
    shell_rc.parent.mkdir(parents=True, exist_ok=True)
    shell_rc.write_text(updated, encoding="utf-8")
    return {
        "status": "enabled" if plan.action == "enable" else "disabled",
        "shell_rc": str(shell_rc),
        "alias_name": plan.alias_name,
        "target_command": plan.target_command,
        "official_claude_unaffected": True,
        "deleted": False,
    }


def _shell_alias_block(plan: ShellAliasPlan) -> str:
    return "\n".join((
        plan.marker_start,
        "# Managed by zhumeng-agent; do not alias the official `claude` binary.",
        f"alias {plan.alias_name}=\"{plan.target_command}\"",
        plan.marker_end,
    ))


def _disabled_shell_alias_block(plan: ShellAliasPlan) -> str:
    return "\n".join((
        plan.marker_start,
        "# zhumeng-claude alias disabled; enable with `zhumeng-claude alias enable`.",
        plan.marker_end,
    ))


def _extract_managed_block(content: str, plan: ShellAliasPlan) -> str | None:
    start = content.find(plan.marker_start)
    end = content.find(plan.marker_end)
    if start < 0 or end < start:
        return None
    return content[start:end + len(plan.marker_end)]


def _replace_managed_block(content: str, plan: ShellAliasPlan, block: str) -> str:
    existing_block = _extract_managed_block(content, plan)
    block_with_newline = block.rstrip() + "\n"
    if existing_block is not None:
        return content.replace(existing_block, block.rstrip(), 1).rstrip() + "\n"
    prefix = content
    if prefix and not prefix.endswith("\n"):
        prefix += "\n"
    return prefix + block_with_newline


def _runtime_integrity(manifest_path_value: object) -> dict[str, object]:
    if not manifest_path_value:
        return {"status": "missing_manifest", "manifest_hash_matches": False, "locked_files_match": False}
    manifest_path = Path(str(manifest_path_value))
    hash_lock_path = manifest_path.parent / "hash.lock"
    if not manifest_path.exists() or not hash_lock_path.exists():
        return {"status": "missing_manifest", "manifest_hash_matches": False, "locked_files_match": False}
    try:
        hash_lock = json.loads(hash_lock_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return {"status": "invalid_hash_lock", "manifest_hash_matches": False, "locked_files_match": False}
    expected = ""
    locked_files: Mapping[str, object] = {}
    if isinstance(hash_lock, dict):
        expected = str(hash_lock.get("manifest_hash") or "")
        raw_locked_files = hash_lock.get("locked_files")
        if isinstance(raw_locked_files, dict):
            locked_files = raw_locked_files
    actual = _hash_existing_file(manifest_path)
    manifest_matches = bool(actual and expected and actual == expected)
    locked_file_status: dict[str, dict[str, object]] = {}
    missing_required_locks = MANDATORY_HASH_LOCK_FILES - set(str(name) for name in locked_files)
    locked_files_match = bool(locked_files) and not missing_required_locks
    for name in sorted(missing_required_locks):
        locked_file_status[name] = {"status": "missing_required_lock", "matches": False}
    for relative_name, expected_hash in locked_files.items():
        name = str(relative_name or "").strip()
        if not name or "/" in name or "\\" in name or name in {".", ".."}:
            locked_files_match = False
            locked_file_status[name or "<empty>"] = {"status": "invalid_name", "matches": False}
            continue
        actual_hash = _hash_existing_file(manifest_path.parent / name)
        expected_hash_text = str(expected_hash or "")
        matches = bool(actual_hash and expected_hash_text and actual_hash == expected_hash_text)
        locked_files_match = locked_files_match and matches
        locked_file_status[name] = {
            "status": "pass" if matches else "hash_mismatch",
            "matches": matches,
            "hash": actual_hash or "",
            "expected_hash": expected_hash_text,
        }
    matches = manifest_matches and locked_files_match
    return {
        "status": "pass" if matches else "hash_mismatch",
        "manifest_hash_matches": manifest_matches,
        "locked_files_match": locked_files_match,
        "manifest_hash": actual or "",
        "expected_manifest_hash": expected,
        "locked_files": locked_file_status,
    }


def _active_manifest_path(runtime_root: Path) -> Path:
    active_pointer = ensure_managed_runtime_write_path(runtime_root / RUNTIME_NAME / "active", runtime_root=runtime_root)
    try:
        active = json.loads(active_pointer.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise RuntimeInstallerError("managed Claude Code runtime active pointer is unreadable") from exc
    manifest_value = str(active.get("manifest_path") or "") if isinstance(active, dict) else ""
    if not manifest_value:
        raise RuntimeInstallerError("managed Claude Code runtime manifest is missing")
    return ensure_managed_runtime_write_path(Path(manifest_value), runtime_root=runtime_root)


def _active_manifest_executable_path(manifest_path: Path, *, runtime_root: Path) -> Path:
    try:
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise RuntimeInstallerError("managed Claude Code runtime manifest is unreadable") from exc
    executable_value = str(manifest.get("executable_path") or "") if isinstance(manifest, dict) else ""
    if not executable_value:
        raise RuntimeInstallerError("managed Claude Code runtime executable is missing from manifest")
    raw_executable = Path(executable_value).expanduser()
    if not raw_executable.is_absolute():
        raise RuntimeInstallerError("managed Claude Code runtime manifest must contain an absolute executable path")
    executable = raw_executable.resolve(strict=False)
    if raw_executable in GLOBAL_CLAUDE_BINARY_PATHS or executable in GLOBAL_CLAUDE_BINARY_PATHS:
        raise RuntimeInstallerError("managed Claude Code runtime refuses to patch global Claude Code binary")
    return executable


def _update_managed_runtime_hash_metadata(
    *,
    runtime_root: Path,
    manifest_path: Path,
    runtime_hash: str,
    patch_status: str,
    before_hash: str,
    patch_point: str = AGENT_MODEL_SCHEMA_PATCH_POINT,
    patch_metadata: Mapping[str, object] | None = None,
) -> None:
    try:
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise RuntimeInstallerError("managed Claude Code runtime manifest is unreadable") from exc
    if not isinstance(manifest, dict):
        raise RuntimeInstallerError("managed Claude Code runtime manifest is invalid")
    manifest["upstream_hash"] = runtime_hash
    patch_points = [str(item) for item in manifest.get("patch_points", []) if str(item)]
    if patch_point not in patch_points:
        patch_points.append(patch_point)
    manifest["patch_points"] = patch_points

    patches_path = ensure_managed_runtime_write_path(manifest_path.parent / "patches.json", runtime_root=runtime_root)
    try:
        patches = json.loads(patches_path.read_text(encoding="utf-8")) if patches_path.exists() else {}
    except (OSError, json.JSONDecodeError) as exc:
        raise RuntimeInstallerError("managed Claude Code runtime patches are unreadable") from exc
    if not isinstance(patches, dict):
        patches = {}
    patch_file_points = [str(item) for item in patches.get("patch_points", []) if str(item)]
    if patch_point not in patch_file_points:
        patch_file_points.append(patch_point)
    patches["patch_points"] = patch_file_points
    metadata = {
        "status": patch_status,
        "patch_point": patch_point,
        "runtime_hash_before": before_hash,
        "runtime_hash_after": runtime_hash,
        "global_binary_touched": False,
    }
    if patch_metadata:
        metadata.update(dict(patch_metadata))
    if patch_point == AGENT_MODEL_SCHEMA_PATCH_POINT:
        metadata.setdefault("schema", "string_min_1_max_128")
        patches["agent_model_schema_patch"] = metadata
    elif patch_point == EFFORT_CAPABILITY_PATCH_POINT:
        patches["effort_capability_patch"] = metadata
    else:
        patches[f"{patch_point}_patch"] = metadata

    rollback_path = ensure_managed_runtime_write_path(manifest_path.parent / "rollback.json", runtime_root=runtime_root)
    try:
        rollback = json.loads(rollback_path.read_text(encoding="utf-8")) if rollback_path.exists() else {}
    except (OSError, json.JSONDecodeError):
        rollback = {}
    if not isinstance(rollback, dict):
        rollback = {}

    manifest_bytes = _canonical_json_bytes(manifest)
    patches_bytes = _canonical_json_bytes(patches)
    rollback_bytes = _canonical_json_bytes(rollback)
    hash_lock = {
        "runtime": RUNTIME_NAME,
        "upstream_version": str(manifest.get("upstream_version") or ""),
        "manifest_hash": _sha256_bytes(manifest_bytes),
        "overlay_hash": str(manifest.get("overlay_hash") or ""),
        "upstream_hash": runtime_hash,
        "locked_files": {
            "manifest.json": _sha256_bytes(manifest_bytes),
            "patches.json": _sha256_bytes(patches_bytes),
            "rollback.json": _sha256_bytes(rollback_bytes),
        },
    }
    manifest_path.write_bytes(manifest_bytes)
    patches_path.write_bytes(patches_bytes)
    rollback_path.write_bytes(rollback_bytes)
    ensure_managed_runtime_write_path(manifest_path.parent / "hash.lock", runtime_root=runtime_root).write_bytes(_canonical_json_bytes(hash_lock))


def _codesign_ad_hoc_if_macho(path: Path) -> None:
    try:
        with path.open("rb") as handle:
            header = handle.read(4)
    except OSError:
        return
    if header not in {b"\xcf\xfa\xed\xfe", b"\xfe\xed\xfa\xcf", b"\xca\xfe\xba\xbe", b"\xbe\xba\xfe\xca"}:
        return
    try:
        subprocess.run(["codesign", "--force", "--sign", "-", str(path)], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    except (OSError, subprocess.CalledProcessError) as exc:
        raise RuntimeInstallerError("managed Claude Code runtime Agent schema patch could not re-sign executable") from exc


def _resolve_runtime_executable(executable: Path) -> Path:
    expanded = executable.expanduser()
    if not expanded.is_absolute() and len(expanded.parts) == 1:
        resolved = shutil.which(str(expanded))
        if resolved:
            expanded = Path(resolved)
    resolved_path = expanded.resolve(strict=False)
    if expanded in GLOBAL_CLAUDE_BINARY_PATHS or resolved_path in GLOBAL_CLAUDE_BINARY_PATHS:
        raise RuntimeInstallerError("refuses to use global Claude Code binary as managed runtime")
    return resolved_path


def ensure_managed_runtime_write_path(path: Path, *, runtime_root: Path) -> Path:
    raw_path = path.expanduser()
    if raw_path in GLOBAL_CLAUDE_BINARY_PATHS:
        raise RuntimeInstallerError("refuses to overwrite global Claude Code binary")
    expanded_root = runtime_root.expanduser().resolve(strict=False)
    expanded_path = raw_path.resolve(strict=False)
    if expanded_path in GLOBAL_CLAUDE_BINARY_PATHS:
        raise RuntimeInstallerError("refuses to overwrite global Claude Code binary")
    if expanded_path == expanded_root:
        return expanded_path
    if expanded_root not in expanded_path.parents:
        raise RuntimeInstallerError(f"refuses to write outside managed runtime root: {expanded_path}")
    return expanded_path


def _stable_sha256(payload: object) -> str:
    return _sha256_bytes(_canonical_json_bytes(payload))


def _sha256_bytes(data: bytes) -> str:
    return "sha256:" + hashlib.sha256(data).hexdigest()


def _hash_existing_file(path: Path) -> str | None:
    try:
        if not path.is_file():
            return None
        digest = hashlib.sha256()
        with path.open("rb") as handle:
            for chunk in iter(lambda: handle.read(1024 * 1024), b""):
                digest.update(chunk)
        return "sha256:" + digest.hexdigest()
    except OSError:
        return None


def _canonical_json_bytes(payload: object) -> bytes:
    return (json.dumps(payload, ensure_ascii=True, sort_keys=True, separators=(",", ":")) + "\n").encode("utf-8")
