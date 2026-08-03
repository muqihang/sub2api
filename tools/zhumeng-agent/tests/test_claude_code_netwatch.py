from __future__ import annotations

import json
from pathlib import Path

import pytest

from zhumeng_agent.adapters.claude_code.netwatch import (
    NativeNetwatchPlan,
    build_native_netwatch_plan,
    classify_destination_bucket,
    summarize_native_netwatch_rows,
)


REPO_ROOT = Path(__file__).resolve().parents[3]


def test_classify_destination_bucket_marks_loopback_and_bypass_candidates():
    assert classify_destination_bucket("127.0.0.1") == "loopback"
    assert classify_destination_bucket("localhost") == "loopback"
    assert classify_destination_bucket("10.0.0.2") == "private_ip"
    assert classify_destination_bucket("93.184.216.34") == "public_ip"
    assert classify_destination_bucket("api.anthropic.com") == "anthropic_or_claude"
    assert classify_destination_bucket("claude.ai") == "anthropic_or_claude"


def test_native_netwatch_summary_counts_bypass_without_payload_or_headers():
    rows = [
        {
            "event": "process_net_connection",
            "remote_host": "127.0.0.1",
            "remote_host_bucket": "loopback",
            "remote_port": 43117,
            "process_name": "claude",
            "state": "ESTABLISHED",
            "potential_guard_bypass": False,
            "headers": {"authorization": "Bearer secret"},
        },
        {
            "event": "process_net_connection",
            "remote_host": "api.anthropic.com",
            "remote_host_bucket": "anthropic_or_claude",
            "remote_port": 443,
            "process_name": "claude",
            "state": "ESTABLISHED",
            "potential_guard_bypass": True,
            "payload": "raw-prompt-marker",
        },
        {
            "event": "process_net_connection",
            "remote_host": "93.184.216.34",
            "remote_host_bucket": "public_ip",
            "remote_port": 443,
            "process_name": "helper",
            "state": "SYN_SENT",
            "potential_guard_bypass": True,
        },
    ]

    summary = summarize_native_netwatch_rows(rows, guard_port=43117)

    assert summary["connection_count"] == 3
    assert summary["potential_guard_bypass_count"] == 2
    assert summary["official_or_public_bypass_count"] == 2
    assert summary["loopback_guard_connection_count"] == 1
    assert summary["remote_host_buckets"] == {"loopback": 1, "anthropic_or_claude": 1, "public_ip": 1}
    dumped = json.dumps(summary, sort_keys=True)
    assert "api.anthropic.com" not in dumped
    assert "raw-prompt-marker" not in dumped
    assert "authorization" not in dumped


def test_native_netwatch_plan_uses_repo_tool_and_safe_env(tmp_path: Path):
    plan = build_native_netwatch_plan(
        root_pid=12345,
        output_path=tmp_path / "process-netwatch.jsonl",
        guard_port=43117,
        interval=2.0,
        repo_root=REPO_ROOT,
        inherited_env={
            "PATH": "/usr/bin",
            "HTTP_PROXY": "http://proxy.example:8080",
            "ANTHROPIC_API_KEY": "local-token",
            "PYTHONPATH": "/tmp/extra",
        },
        python_executable=Path("/opt/homebrew/bin/python3"),
    )

    assert isinstance(plan, NativeNetwatchPlan)
    assert plan.command[:2] == [
        "/opt/homebrew/bin/python3",
        str(REPO_ROOT / "tools" / "claude_code_lab_netwatch.py"),
    ]
    assert "--root-pid" in plan.command
    assert "12345" in plan.command
    assert "--guard-port" in plan.command
    assert "43117" in plan.command
    assert "--once" not in plan.command
    assert plan.env["PATH"] == "/usr/bin"
    assert "HTTP_PROXY" not in plan.env
    assert "ANTHROPIC_API_KEY" not in plan.env
    assert str(REPO_ROOT) in plan.env["PYTHONPATH"].split(":")
    assert plan.cwd == REPO_ROOT
    assert plan.will_start_process is False
    assert "local-token" not in repr(plan)


def test_native_netwatch_plan_rejects_invalid_process_or_guard_port(tmp_path: Path):
    with pytest.raises(ValueError):
        build_native_netwatch_plan(root_pid=0, output_path=tmp_path / "out.jsonl", repo_root=REPO_ROOT)
    with pytest.raises(ValueError):
        build_native_netwatch_plan(
            root_pid=123,
            output_path=tmp_path / "out.jsonl",
            guard_port=70000,
            repo_root=REPO_ROOT,
        )


def test_native_netwatch_summary_derives_bypass_from_bucket_and_never_counts_loopback():
    rows = [
        {
            "event": "process_net_connection",
            "remote_host": "127.0.0.1",
            "remote_port": 55555,
            "process_name": "claude",
            "state": "ESTABLISHED",
            "potential_guard_bypass": True,
        },
        {
            "event": "process_net_connection",
            "remote_host": "api.anthropic.com",
            "remote_port": 443,
            "process_name": "claude",
            "state": "ESTABLISHED",
        },
        {
            "event": "process_net_connection",
            "remote_host": "93.184.216.34",
            "remote_port": 443,
            "process_name": "helper",
            "state": "SYN_SENT",
        },
    ]

    summary = summarize_native_netwatch_rows(rows, guard_port=43117)

    assert summary["potential_guard_bypass_count"] == 2
    assert summary["official_or_public_bypass_count"] == 2
    assert summary["remote_host_buckets"] == {"loopback": 1, "anthropic_or_claude": 1, "public_ip": 1}
