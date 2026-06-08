from __future__ import annotations

import re
import subprocess
from dataclasses import dataclass, field
from pathlib import Path
from typing import Callable, Mapping, Sequence

from .profile import ClaudeCodeProfile, build_safe_env

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
