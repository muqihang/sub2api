from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
from types import SimpleNamespace

import pytest

from zhumeng_agent.adapters.claude_code.runtime_installer import (
    RuntimeInstallerError,
    build_managed_runtime_install_plan,
    ensure_managed_runtime_write_path,
    write_managed_runtime_artifacts,
)


class VersionRunner:
    def __init__(self, stdout: str, *, returncode: int = 0):
        self.stdout = stdout
        self.returncode = returncode
        self.calls: list[tuple[list[str], dict[str, object]]] = []

    def __call__(self, command: list[str], **kwargs: object) -> SimpleNamespace:
        self.calls.append((command, kwargs))
        return SimpleNamespace(stdout=self.stdout, stderr="", returncode=self.returncode)


class ExplodingVersionRunner:
    def __call__(self, command: list[str], **kwargs: object) -> SimpleNamespace:
        raise FileNotFoundError("claude executable missing")


def test_runtime_installer_materializes_manifest_hash_lock_and_rollback_metadata(tmp_path: Path):
    runtime_root = tmp_path / ".zhumeng" / "runtimes"
    executable = tmp_path / "node_modules" / ".bin" / "claude"
    runner = VersionRunner("claude-code/2.1.175 darwin-arm64\n")

    plan = build_managed_runtime_install_plan(
        executable=executable,
        runtime_root=runtime_root,
        runner=runner,
        profile_id="prod",
    )

    assert plan.upstream_version == "2.1.175"
    assert plan.version_dir == runtime_root / "claude-code" / "2.1.175"
    assert plan.cache_path == runtime_root / "claude-code" / "cache" / "2.1.175"
    assert plan.manifest.cch_profile == "claude_code_2_1_175"
    assert plan.manifest.status == "ready"
    assert runner.calls[0][0] == [str(executable), "--version"]
    probe_env = runner.calls[0][1]["env"]
    assert probe_env["CLAUDE_CONFIG_DIR"] == str(runtime_root / "claude-code" / "cache" / "version-probe" / "config")
    assert probe_env["HOME"] == str(runtime_root / "claude-code" / "cache" / "version-probe" / "home")
    assert probe_env["XDG_CONFIG_HOME"] == str(runtime_root / "claude-code" / "cache" / "version-probe" / "xdg-config")
    assert probe_env["PATH"] == os.environ.get("PATH", "")
    assert ".claude" not in probe_env["CLAUDE_CONFIG_DIR"]
    assert ".claude" not in probe_env["HOME"]
    assert ".claude" not in probe_env["XDG_CONFIG_HOME"]

    write_managed_runtime_artifacts(plan)

    manifest_bytes = plan.manifest_path.read_bytes()
    manifest = json.loads(manifest_bytes.decode("utf-8"))
    assert manifest == plan.manifest.to_dict()
    assert manifest["runtime"] == "claude-code"
    assert manifest["source"] == "npm:@anthropic-ai/claude-code@2.1.175"
    assert manifest["patch_points"] == [
        "runtime_manifest",
        "hash_lock",
        "isolated_config",
        "guard_env",
    ]

    hash_lock = json.loads(plan.hash_lock_path.read_text(encoding="utf-8"))
    assert hash_lock["runtime"] == "claude-code"
    assert hash_lock["upstream_version"] == "2.1.175"
    assert hash_lock["manifest_hash"] == "sha256:" + hashlib.sha256(manifest_bytes).hexdigest()
    assert hash_lock["upstream_hash"] == manifest["upstream_hash"]
    assert hash_lock["overlay_hash"] == manifest["overlay_hash"]
    assert hash_lock["locked_files"]["manifest.json"] == hash_lock["manifest_hash"]
    assert hash_lock["locked_files"]["patches.json"].startswith("sha256:")

    rollback = json.loads(plan.rollback_metadata_path.read_text(encoding="utf-8"))
    assert rollback["runtime"] == "claude-code"
    assert rollback["from_version"] is None
    assert rollback["to_version"] == "2.1.175"
    assert rollback["active_pointer"] == str(runtime_root / "claude-code" / "active")
    assert rollback["global_overwrite"] is False


def test_runtime_installer_never_targets_global_claude_binary(tmp_path: Path):
    runtime_root = tmp_path / ".zhumeng" / "runtimes"
    plan = build_managed_runtime_install_plan(
        executable=tmp_path / "claude",
        runtime_root=runtime_root,
        runner=VersionRunner("Claude Code v2.1.175"),
    )

    global_claude = Path("/opt/homebrew/bin/claude")
    assert str(global_claude) not in {str(path) for path in plan.planned_write_paths}
    assert all(runtime_root in path.parents for path in plan.planned_write_paths)
    with pytest.raises(RuntimeInstallerError, match="refuses to overwrite global Claude Code binary"):
        ensure_managed_runtime_write_path(global_claude, runtime_root=runtime_root)


def test_runtime_installer_uses_managed_cache_and_does_not_read_default_claude_dir(tmp_path: Path, monkeypatch: pytest.MonkeyPatch):
    home = tmp_path / "home"
    default_claude = home / ".claude"
    default_claude.mkdir(parents=True)
    (default_claude / "secret.json").write_text('{"token":"must-not-read"}', encoding="utf-8")
    runtime_root = home / ".zhumeng" / "runtimes"

    original_read_text = Path.read_text

    def fail_on_default_claude_read(self: Path, *args, **kwargs):
        expanded = self.expanduser()
        if expanded == default_claude or default_claude in expanded.parents:
            raise AssertionError("runtime installer must not read default ~/.claude")
        return original_read_text(self, *args, **kwargs)

    monkeypatch.setattr(Path, "home", classmethod(lambda cls: home))
    monkeypatch.setattr(Path, "read_text", fail_on_default_claude_read)

    plan = build_managed_runtime_install_plan(
        executable=tmp_path / "claude",
        runtime_root=runtime_root,
        runner=VersionRunner("Claude Code v2.1.175"),
    )
    write_managed_runtime_artifacts(plan)

    assert plan.cache_path == runtime_root / "claude-code" / "cache" / "2.1.175"
    assert ".claude" not in plan.cache_path.parts
    assert all(".claude" not in path.parts for path in plan.planned_write_paths)


def test_runtime_installer_unknown_version_fails_closed_without_writing(tmp_path: Path):
    runtime_root = tmp_path / ".zhumeng" / "runtimes"

    with pytest.raises(RuntimeInstallerError, match="unknown Claude Code version"):
        build_managed_runtime_install_plan(
            executable=tmp_path / "claude",
            runtime_root=runtime_root,
            runner=VersionRunner("Claude Code preview-channel without semver"),
        )

    assert not runtime_root.exists()


def test_runtime_installer_unsupported_version_fails_closed_without_writing(tmp_path: Path):
    runtime_root = tmp_path / ".zhumeng" / "runtimes"

    with pytest.raises(RuntimeInstallerError, match="unsupported Claude Code version"):
        build_managed_runtime_install_plan(
            executable=tmp_path / "claude",
            runtime_root=runtime_root,
            runner=VersionRunner("Claude Code v9.9.9"),
        )

    assert not runtime_root.exists()


def test_runtime_installer_public_api_is_exported_from_adapter_package():
    from zhumeng_agent.adapters.claude_code import (  # noqa: PLC0415
        RuntimeInstallerError as ExportedError,
        build_managed_runtime_install_plan as exported_build_plan,
    )

    assert ExportedError is RuntimeInstallerError
    assert exported_build_plan is build_managed_runtime_install_plan


def test_runtime_installer_rejects_path_traversal_out_of_managed_root(tmp_path: Path):
    runtime_root = tmp_path / ".zhumeng" / "runtimes"
    escaped = runtime_root / ".." / "outside" / "manifest.json"

    with pytest.raises(RuntimeInstallerError, match="refuses to write outside managed runtime root"):
        ensure_managed_runtime_write_path(escaped, runtime_root=runtime_root)


def test_runtime_installer_version_detector_errors_fail_closed_without_writing(tmp_path: Path):
    runtime_root = tmp_path / ".zhumeng" / "runtimes"

    with pytest.raises(RuntimeInstallerError, match="unable to detect Claude Code version"):
        build_managed_runtime_install_plan(
            executable=tmp_path / "missing-claude",
            runtime_root=runtime_root,
            runner=ExplodingVersionRunner(),
        )

    assert not runtime_root.exists()


def test_runtime_installer_nonzero_version_probe_fails_closed_without_writing(tmp_path: Path):
    runtime_root = tmp_path / ".zhumeng" / "runtimes"

    with pytest.raises(RuntimeInstallerError, match="Claude Code version probe failed"):
        build_managed_runtime_install_plan(
            executable=tmp_path / "claude",
            runtime_root=runtime_root,
            runner=VersionRunner("Claude Code v2.1.175", returncode=1),
        )

    assert not runtime_root.exists()


def test_runtime_installer_revalidates_actual_write_fields_on_malformed_plan(tmp_path: Path):
    runtime_root = tmp_path / ".zhumeng" / "runtimes"
    plan = build_managed_runtime_install_plan(
        executable=tmp_path / "claude",
        runtime_root=runtime_root,
        runner=VersionRunner("Claude Code v2.1.175"),
    )
    malformed = plan.__class__(
        executable=plan.executable,
        runtime_root=plan.runtime_root,
        upstream_version=plan.upstream_version,
        runtime_dir=plan.runtime_dir,
        version_dir=plan.version_dir,
        cache_path=plan.cache_path,
        manifest_path=runtime_root / ".." / "outside" / "manifest.json",
        patches_path=plan.patches_path,
        hash_lock_path=plan.hash_lock_path,
        rollback_metadata_path=plan.rollback_metadata_path,
        active_pointer=plan.active_pointer,
        manifest=plan.manifest,
        patches=plan.patches,
        rollback_metadata=plan.rollback_metadata,
        planned_write_paths=plan.planned_write_paths,
    )

    with pytest.raises(RuntimeInstallerError, match="refuses to write outside managed runtime root"):
        write_managed_runtime_artifacts(malformed)

    assert not (runtime_root.parent / "outside" / "manifest.json").exists()


def test_runtime_installer_hashes_existing_upstream_executable_contents(tmp_path: Path):
    runtime_root = tmp_path / ".zhumeng" / "runtimes"
    executable = tmp_path / "claude"
    executable.write_bytes(b"fake-claude-code-binary")

    plan = build_managed_runtime_install_plan(
        executable=executable,
        runtime_root=runtime_root,
        runner=VersionRunner("Claude Code v2.1.175"),
    )

    assert plan.manifest.upstream_hash == "sha256:" + hashlib.sha256(b"fake-claude-code-binary").hexdigest()
