from __future__ import annotations

import re
import subprocess
from dataclasses import dataclass, field
from pathlib import Path
from typing import Callable, Mapping, Sequence

from .guard import NativeGuardConfig, NativeGuardMode, NativeGuardPlan, build_native_guard_plan, start_native_guard
from .profile import CaptureMode, ClaudeCodeProfile, build_isolated_config_dir, build_safe_env

Runner = Callable[..., object]
_VERSION_RE = re.compile(r"(?:claude(?:-code)?[/ ]+v?|Claude Code\s+v?)(\d+(?:\.\d+){1,3})", re.IGNORECASE)
_FALLBACK_VERSION_RE = re.compile(r"\bv?(\d+\.\d+(?:\.\d+){0,2})\b")


@dataclass(frozen=True, slots=True)
class ClaudeCodeVersion:
    executable: Path
    version: str | None
    raw_output: str


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
) -> ClaudeCodeVersion:
    executable_path = Path(executable)
    runner = runner or subprocess.run
    result = runner(
        [str(executable_path), "--version"],
        capture_output=True,
        text=True,
        check=False,
        timeout=timeout_seconds,
    )
    stdout = str(getattr(result, "stdout", "") or "")
    stderr = str(getattr(result, "stderr", "") or "")
    raw_output = (stdout or stderr).strip()
    return ClaudeCodeVersion(
        executable=executable_path,
        version=_parse_claude_code_version(raw_output),
        raw_output=raw_output,
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
    managed_session_id: str | None = None,
    device_id: int | None = None,
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
    config_root = config_root.expanduser()
    summary_path = config_root / "claude-code" / profile_id / "native-guard-summary.jsonl"
    summary_path.parent.mkdir(parents=True, exist_ok=True)
    guard_config = NativeGuardConfig(
        mode=mode,
        listen_port=guard_listen_port,
        upstream_base=upstream_base,
        sub2api_auth=sub2api_auth,
        summary_path=summary_path,
        repo_root=repo_root,
        attestation_secret=attestation_secret,
        allow_nonloopback_upstream=True,
        managed_session_id=managed_session_id,
        device_id=device_id,
    )
    guard_plan = build_native_guard_plan(guard_config, inherited_env=inherited_env)
    with start_guard(guard_plan, ready_timeout_seconds=ready_timeout_seconds) as guard:
        guard_base_url = str(guard.ready["listen"])
        profile = ClaudeCodeProfile(
            profile_id=profile_id,
            guard_base_url=guard_base_url,
            zhumeng_entry_api_key=sub2api_auth,
            config_dir=build_isolated_config_dir(config_root, profile_id=profile_id),
            capture_mode=CaptureMode.PRODUCTION,
        )
        launch_plan = build_claude_code_launch_plan(
            executable=executable,
            profile=profile,
            inherited_env=inherited_env,
            project_cwd=project_cwd,
            argv=argv,
        )
        launch_plan = ClaudeCodeLaunchPlan(
            command=launch_plan.command,
            env=launch_plan.env,
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
