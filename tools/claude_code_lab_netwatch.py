#!/usr/bin/env python3
"""Process-level network destination watcher for Claude Code local lab runs.

The watcher records connection metadata only. It never inspects HTTP payloads,
headers, prompts, telemetry bodies, tokens, or TLS contents.
"""
from __future__ import annotations

import argparse
import ipaddress
import json
import os
import re
import subprocess
import sys
import time
from collections import Counter
from pathlib import Path
from typing import Any, Iterable

_TCP_NAME_RE = re.compile(r"(?P<local>.+?)->(?P<remote>.+)$")
_BRACKET_HOST_RE = re.compile(r"^\[(?P<host>.*)]:(?P<port>\d+)$")
_HOST_PORT_RE = re.compile(r"^(?P<host>.*):(?P<port>\d+)$")
_ALLOWED_RAW_KEYS = {
    "event",
    "ts",
    "pid",
    "process_name",
    "watched_process_tree",
    "remote_host",
    "remote_host_bucket",
    "remote_port",
    "state",
    "potential_guard_bypass",
    "guard_port",
}


def classify_remote(host: str) -> str:
    value = (host or "").strip().lower()
    if not value:
        return "unknown"
    if any(marker in value for marker in ("anthropic.com", "claude.ai", "claude.com")):
        return "anthropic_or_claude"
    try:
        ip = ipaddress.ip_address(value.strip("[]"))
    except ValueError:
        if value in {"localhost"}:
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


def _parse_endpoint(endpoint: str) -> tuple[str, int | None]:
    endpoint = endpoint.strip()
    bracket = _BRACKET_HOST_RE.match(endpoint)
    if bracket:
        return bracket.group("host"), int(bracket.group("port"))
    match = _HOST_PORT_RE.match(endpoint)
    if not match:
        return endpoint, None
    host = match.group("host")
    try:
        port = int(match.group("port"))
    except ValueError:
        port = None
    return host, port


def _parse_remote_from_name(name: str) -> tuple[str, int | None] | None:
    match = _TCP_NAME_RE.search(name)
    if not match:
        return None
    return _parse_endpoint(match.group("remote"))


def parse_lsof_tcp_rows(output: str, *, watched_pids: set[int], guard_port: int | None = None) -> list[dict[str, Any]]:
    rows: list[dict[str, Any]] = []
    current_pid: int | None = None
    current_name = "unknown"
    pending_remote: tuple[str, int | None] | None = None
    pending_state: str | None = None

    def flush() -> None:
        nonlocal pending_remote, pending_state
        if current_pid is None or pending_remote is None:
            pending_remote = None
            pending_state = None
            return
        if current_pid not in watched_pids:
            pending_remote = None
            pending_state = None
            return
        host, port = pending_remote
        bucket = classify_remote(host)
        is_guard = bucket == "loopback" and guard_port is not None and port == guard_port
        potential_bypass = bucket not in {"loopback", "private_ip", "link_local_ip", "unspecified_ip"}
        if is_guard:
            potential_bypass = False
        row = {
            "event": "process_net_connection",
            "ts": time.time(),
            "pid": current_pid,
            "process_name": current_name[:80],
            "watched_process_tree": True,
            "remote_host": host,
            "remote_host_bucket": bucket,
            "remote_port": port,
            "state": pending_state or "unknown",
            "potential_guard_bypass": bool(potential_bypass),
        }
        if guard_port is not None:
            row["guard_port"] = guard_port
        rows.append({key: value for key, value in row.items() if key in _ALLOWED_RAW_KEYS})
        pending_remote = None
        pending_state = None

    for raw in output.splitlines():
        if not raw:
            continue
        tag, value = raw[:1], raw[1:]
        if tag == "p":
            flush()
            try:
                current_pid = int(value)
            except ValueError:
                current_pid = None
            current_name = "unknown"
        elif tag == "c":
            current_name = value or "unknown"
        elif tag == "n":
            flush()
            pending_remote = _parse_remote_from_name(value)
        elif tag == "T" and value.startswith("ST="):
            pending_state = value[3:]
    flush()
    return rows


def _ps_process_tree(root_pid: int) -> set[int]:
    try:
        proc = subprocess.run(
            ["ps", "-axo", "pid=,ppid="],
            check=True,
            capture_output=True,
            text=True,
            timeout=5,
        )
    except Exception:  # noqa: BLE001
        return {root_pid}
    children: dict[int, list[int]] = {}
    for line in proc.stdout.splitlines():
        parts = line.split()
        if len(parts) != 2:
            continue
        try:
            pid, ppid = int(parts[0]), int(parts[1])
        except ValueError:
            continue
        children.setdefault(ppid, []).append(pid)
    tree = {root_pid}
    stack = [root_pid]
    while stack:
        parent = stack.pop()
        for child in children.get(parent, []):
            if child not in tree:
                tree.add(child)
                stack.append(child)
    return tree


def poll_lsof(*, root_pid: int, guard_port: int | None = None) -> list[dict[str, Any]]:
    watched = _ps_process_tree(root_pid)
    try:
        proc = subprocess.run(
            ["lsof", "-nP", "-iTCP", "-FpcnT"],
            check=False,
            capture_output=True,
            text=True,
            timeout=5,
        )
    except FileNotFoundError:
        return [{
            "event": "process_netwatch_error",
            "ts": time.time(),
            "pid": root_pid,
            "process_name": "lsof_missing",
            "watched_process_tree": True,
            "remote_host": "unknown",
            "remote_host_bucket": "unknown",
            "remote_port": None,
            "state": "lsof_missing",
            "potential_guard_bypass": False,
        }]
    except Exception as exc:  # noqa: BLE001
        return [{
            "event": "process_netwatch_error",
            "ts": time.time(),
            "pid": root_pid,
            "process_name": type(exc).__name__[:80],
            "watched_process_tree": True,
            "remote_host": "unknown",
            "remote_host_bucket": "unknown",
            "remote_port": None,
            "state": "poll_error",
            "potential_guard_bypass": False,
        }]
    return parse_lsof_tcp_rows(proc.stdout, watched_pids=watched, guard_port=guard_port)


def append_jsonl(path: Path, rows: Iterable[dict[str, Any]]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("a", encoding="utf-8") as fh:
        for row in rows:
            fh.write(json.dumps(row, ensure_ascii=False, sort_keys=True) + "\n")
    try:
        path.chmod(0o600)
    except OSError:
        pass


def watch_loop(*, root_pid: int, output_path: Path, interval: float, guard_port: int | None = None) -> int:
    while True:
        if not Path(f"/proc/{root_pid}").exists() and sys.platform.startswith("linux"):
            break
        rows = poll_lsof(root_pid=root_pid, guard_port=guard_port)
        if rows:
            append_jsonl(output_path, rows)
        if not _process_exists(root_pid):
            break
        time.sleep(max(interval, 0.5))
    return 0


def _process_exists(pid: int) -> bool:
    try:
        os.kill(pid, 0)
        return True
    except OSError:
        return False


def load_netwatch_rows(path: Path) -> list[dict[str, Any]]:
    if not path.exists():
        return []
    rows: list[dict[str, Any]] = []
    for line in path.read_text(encoding="utf-8", errors="replace").splitlines():
        if not line.strip():
            continue
        try:
            value = json.loads(line)
        except json.JSONDecodeError:
            continue
        if isinstance(value, dict):
            rows.append(value)
    return rows


def summarize_netwatch_rows(rows: list[dict[str, Any]]) -> dict[str, Any]:
    buckets = Counter(str(row.get("remote_host_bucket", "unknown")) for row in rows)
    states = Counter(str(row.get("state", "unknown")) for row in rows)
    ports = Counter(str(row.get("remote_port", "unknown")) for row in rows)
    processes = Counter(str(row.get("process_name", "unknown")) for row in rows)
    bypass = sum(1 for row in rows if row.get("potential_guard_bypass") is True)
    return {
        "connection_count": len(rows),
        "potential_guard_bypass_count": bypass,
        "remote_host_buckets": dict(buckets),
        "states": dict(states),
        "remote_ports_top": dict(ports.most_common(20)),
        "processes_top": dict(processes.most_common(20)),
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Record safe process-level TCP destinations for a Claude Code lab process tree")
    parser.add_argument("--root-pid", type=int, required=True)
    parser.add_argument("--output-path", type=Path, required=True)
    parser.add_argument("--interval", type=float, default=2.0)
    parser.add_argument("--guard-port", type=int)
    parser.add_argument("--once", action="store_true")
    args = parser.parse_args(argv)
    if args.once:
        append_jsonl(args.output_path, poll_lsof(root_pid=args.root_pid, guard_port=args.guard_port))
        return 0
    return watch_loop(root_pid=args.root_pid, output_path=args.output_path, interval=args.interval, guard_port=args.guard_port)


if __name__ == "__main__":
    raise SystemExit(main())
