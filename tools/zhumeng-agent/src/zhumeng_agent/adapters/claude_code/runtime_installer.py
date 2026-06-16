from __future__ import annotations

import hashlib
import json
import os
from dataclasses import asdict, dataclass, field
from pathlib import Path
from typing import Mapping

from .launcher import Runner, detect_claude_code_version

RUNTIME_NAME = "claude-code"
ZHUMENG_RUNTIME_VERSION = "0.1.0"
SUPPORTED_UPSTREAM_VERSIONS = frozenset({"2.1.175"})
DEFAULT_PATCH_POINTS = (
    "runtime_manifest",
    "hash_lock",
    "isolated_config",
    "guard_env",
)


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

    def to_dict(self) -> dict[str, object]:
        data = asdict(self)
        data["patch_points"] = list(self.patch_points)
        return data


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


def build_managed_runtime_install_plan(
    *,
    executable: Path | str,
    runtime_root: Path,
    runner: Runner | None = None,
    profile_id: str = "prod",
    supported_versions: frozenset[str] = SUPPORTED_UPSTREAM_VERSIONS,
) -> ManagedRuntimeInstallPlan:
    runtime_root = runtime_root.expanduser()
    executable_path = Path(executable)
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
    upstream_hash = _hash_existing_file(executable_path) or _stable_sha256(
        {
            "executable": str(executable_path),
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


def ensure_managed_runtime_write_path(path: Path, *, runtime_root: Path) -> Path:
    raw_path = path.expanduser()
    if raw_path == Path("/opt/homebrew/bin/claude"):
        raise RuntimeInstallerError("refuses to overwrite global Claude Code binary")
    expanded_root = runtime_root.expanduser().resolve(strict=False)
    expanded_path = raw_path.resolve(strict=False)
    if expanded_path == Path("/opt/homebrew/bin/claude"):
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
