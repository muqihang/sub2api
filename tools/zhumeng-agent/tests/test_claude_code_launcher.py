from __future__ import annotations

from pathlib import Path
from types import SimpleNamespace

import pytest

from zhumeng_agent.adapters.claude_code.launcher import (
    build_claude_code_launch_plan,
    detect_claude_code_version,
)
from zhumeng_agent.adapters.claude_code.profile import CaptureMode, ClaudeCodeProfile


class RecordingRunner:
    def __init__(self, stdout: str = "Claude Code 1.2.3\n"):
        self.stdout = stdout
        self.calls: list[tuple[list[str], dict[str, object]]] = []

    def __call__(self, command: list[str], **kwargs: object) -> SimpleNamespace:
        self.calls.append((command, kwargs))
        return SimpleNamespace(stdout=self.stdout, stderr="", returncode=0)


def test_detect_version_uses_explicit_executable_and_injected_runner(tmp_path: Path):
    executable = tmp_path / "claude"
    runner = RecordingRunner(stdout="claude-code/1.2.3 darwin-arm64\n")

    detected = detect_claude_code_version(executable=executable, runner=runner)

    assert detected.executable == executable
    assert detected.version == "1.2.3"
    assert detected.raw_output == "claude-code/1.2.3 darwin-arm64"
    assert runner.calls[0][0] == [str(executable), "--version"]
    assert runner.calls[0][1]["timeout"] <= 5


def test_detect_version_does_not_require_real_execution_when_runner_is_supplied(tmp_path: Path):
    missing_executable = tmp_path / "missing-claude"
    runner = RecordingRunner(stdout="Claude Code v0.9.0")

    detected = detect_claude_code_version(executable=missing_executable, runner=runner)

    assert detected.version == "0.9.0"
    assert len(runner.calls) == 1


def test_launch_plan_builds_command_and_env_but_does_not_start_process(tmp_path: Path):
    executable = tmp_path / "claude"
    profile = ClaudeCodeProfile(
        profile_id="prod",
        guard_base_url="http://127.0.0.1:43117",
        zhumeng_entry_api_key="entry-key",
        config_dir=tmp_path / "config",
        capture_mode=CaptureMode.PRODUCTION,
    )

    plan = build_claude_code_launch_plan(
        executable=executable,
        profile=profile,
        inherited_env={"ANTHROPIC_API_KEY": "real-key", "PATH": "/usr/bin"},
        project_cwd=tmp_path / "workspace",
        argv=["--print"],
    )

    assert plan.command == [str(executable), "--print"]
    assert plan.cwd == tmp_path / "workspace"
    assert plan.env["ANTHROPIC_API_KEY"] == "entry-key"
    assert plan.env["ANTHROPIC_BASE_URL"] == "http://127.0.0.1:43117"
    assert "real-key" not in "\n".join(plan.env.values())
    assert plan.will_start_process is False


def test_launch_plan_rejects_non_loopback_guard_before_building_env(tmp_path: Path):
    profile = ClaudeCodeProfile(
        profile_id="bad",
        guard_base_url="https://api.anthropic.com",
        zhumeng_entry_api_key="entry-key",
        config_dir=tmp_path / "config",
        capture_mode=CaptureMode.STAGING,
    )

    with pytest.raises(ValueError, match="loopback"):
        build_claude_code_launch_plan(
            executable=tmp_path / "claude",
            profile=profile,
            inherited_env={},
            project_cwd=tmp_path,
        )


def test_launch_plan_repr_does_not_expose_env_api_key(tmp_path: Path):
    profile = ClaudeCodeProfile(
        profile_id="prod",
        guard_base_url="http://127.0.0.1:43117",
        zhumeng_entry_api_key="secret-entry-key",
        config_dir=tmp_path / "config",
        capture_mode=CaptureMode.PRODUCTION,
    )

    plan = build_claude_code_launch_plan(
        executable=tmp_path / "claude",
        profile=profile,
        inherited_env={},
        project_cwd=tmp_path,
    )

    assert "secret-entry-key" not in repr(plan)
