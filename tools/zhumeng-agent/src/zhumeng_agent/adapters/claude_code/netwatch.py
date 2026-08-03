from __future__ import annotations

import ipaddress
import os
import subprocess
import sys
from collections import Counter
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Mapping

_SENSITIVE_ENV_MARKERS = ("TOKEN", "API_KEY", "COOKIE", "SESSION", "PROXY", "BASE_URL")


@dataclass(frozen=True, slots=True)
class NativeNetwatchPlan:
    command: list[str]
    env: dict[str, str] = field(repr=False)
    cwd: Path
    output_path: Path
    root_pid: int
    guard_port: int | None = None
    will_start_process: bool = False


def classify_destination_bucket(host: str) -> str:
    value = (host or "").strip().lower()
    if not value:
        return "unknown"
    if any(marker in value for marker in ("anthropic.com", "claude.ai", "claude.com")):
        return "anthropic_or_claude"
    try:
        ip = ipaddress.ip_address(value.strip("[]"))
    except ValueError:
        if value == "localhost":
            return "loopback"
        return "dns_name"
    if ip.is_loopback:
        return "loopback"
    if ip.is_private:
        return "private_ip"
    if ip.is_link_local:
        return "link_local_ip"
    if ip.is_multicast:
        return "multicast_ip"
    if ip.is_unspecified:
        return "unspecified_ip"
    return "public_ip"


def build_native_netwatch_plan(
    *,
    root_pid: int,
    output_path: Path,
    repo_root: Path,
    guard_port: int | None = None,
    interval: float = 2.0,
    inherited_env: Mapping[str, str] | None = None,
    python_executable: Path | str | None = None,
    once: bool = False,
) -> NativeNetwatchPlan:
    if root_pid <= 0:
        raise ValueError("native netwatch root_pid must be positive")
    if guard_port is not None and (guard_port <= 0 or guard_port > 65535):
        raise ValueError("native netwatch guard_port must be a valid TCP port")
    if interval <= 0:
        raise ValueError("native netwatch interval must be positive")

    env = _build_netwatch_env(repo_root, inherited_env=inherited_env)
    command = [
        str(python_executable or sys.executable),
        str(repo_root / "tools" / "claude_code_lab_netwatch.py"),
        "--root-pid",
        str(root_pid),
        "--output-path",
        str(output_path),
        "--interval",
        str(interval),
    ]
    if guard_port is not None:
        command.extend(["--guard-port", str(guard_port)])
    if once:
        command.append("--once")
    return NativeNetwatchPlan(
        command=command,
        env=env,
        cwd=repo_root,
        output_path=output_path,
        root_pid=root_pid,
        guard_port=guard_port,
    )


def start_native_netwatch(plan: NativeNetwatchPlan) -> subprocess.Popen[str]:
    return subprocess.Popen(
        plan.command,
        cwd=str(plan.cwd),
        env=plan.env,
        text=True,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )


def summarize_native_netwatch_rows(rows: list[Mapping[str, Any]], *, guard_port: int | None = None) -> dict[str, Any]:
    safe_rows = [_safe_netwatch_row(row, guard_port=guard_port) for row in rows]
    buckets = Counter(str(row.get("remote_host_bucket", "unknown")) for row in safe_rows)
    states = Counter(str(row.get("state", "unknown")) for row in safe_rows)
    ports = Counter(str(row.get("remote_port", "unknown")) for row in safe_rows)
    processes = Counter(str(row.get("process_name", "unknown")) for row in safe_rows)
    bypass_count = sum(1 for row in safe_rows if row.get("potential_guard_bypass") is True)
    official_or_public_bypass = sum(
        1
        for row in safe_rows
        if row.get("potential_guard_bypass") is True
        and row.get("remote_host_bucket") in {"anthropic_or_claude", "public_ip", "dns_name"}
    )
    loopback_guard_count = sum(1 for row in safe_rows if row.get("loopback_guard_connection") is True)
    return {
        "connection_count": len(safe_rows),
        "potential_guard_bypass_count": bypass_count,
        "official_or_public_bypass_count": official_or_public_bypass,
        "loopback_guard_connection_count": loopback_guard_count,
        "remote_host_buckets": dict(buckets),
        "states": dict(states),
        "remote_ports_top": dict(ports.most_common(20)),
        "processes_top": dict(processes.most_common(20)),
        "stores_payload": False,
        "stores_headers": False,
    }


def _safe_netwatch_row(row: Mapping[str, Any], *, guard_port: int | None) -> dict[str, Any]:
    host_bucket = str(row.get("remote_host_bucket") or classify_destination_bucket(str(row.get("remote_host", ""))))
    remote_port = row.get("remote_port")
    loopback_guard = host_bucket == "loopback" and guard_port is not None and remote_port == guard_port
    bucket_bypass = host_bucket in {"anthropic_or_claude", "public_ip", "dns_name", "multicast_ip"}
    potential_bypass = bucket_bypass or bool(row.get("potential_guard_bypass"))
    if host_bucket in {"loopback", "private_ip", "link_local_ip", "unspecified_ip"}:
        potential_bypass = False
    return {
        "remote_host_bucket": host_bucket,
        "remote_port": remote_port,
        "process_name": str(row.get("process_name", "unknown"))[:80],
        "state": str(row.get("state", "unknown"))[:80],
        "potential_guard_bypass": potential_bypass,
        "loopback_guard_connection": loopback_guard,
    }


def _build_netwatch_env(repo_root: Path, *, inherited_env: Mapping[str, str] | None) -> dict[str, str]:
    env: dict[str, str] = {}
    for key, value in (inherited_env or {}).items():
        if _can_inherit_env_key(key):
            env[key] = value
    env["PYTHONPATH"] = _prepend_pythonpath(str(repo_root), env.get("PYTHONPATH"))
    return env


def _can_inherit_env_key(key: str) -> bool:
    upper_key = key.upper()
    if upper_key in {"PATH", "HOME", "SHELL", "TERM", "LANG"} or key.startswith("LC_"):
        return True
    if upper_key == "PYTHONPATH":
        return True
    if any(marker in upper_key for marker in _SENSITIVE_ENV_MARKERS):
        return False
    return False


def _prepend_pythonpath(repo_root: str, current: str | None) -> str:
    if not current:
        return repo_root
    parts = current.split(os.pathsep)
    if repo_root in parts:
        return current
    return repo_root + os.pathsep + current
