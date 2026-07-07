#!/usr/bin/env python3
"""Launch Claude Code through the local redacting guard and capture safe summaries.

This lab runner is for local observation only. It does not store raw tokens,
raw prompts, raw bodies, or raw control-plane payloads.
"""
from __future__ import annotations

import argparse
import json
import os
import shlex
import shutil
import signal
import socket
import subprocess
import sys
import time
from pathlib import Path
from typing import Any

REPO_ROOT = Path(__file__).resolve().parents[1]
DEFAULT_API_BASE = "http://198.12.67.185:18080"
DEFAULT_LAB_HOME = Path.home() / ".zhumeng" / "claude-code-lab"
PLACEHOLDER_CLIENT_KEY = "zhumeng-local-capture-placeholder"


def _free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


def _safe_command_display(command: list[str]) -> list[str]:
    return ["<empty>"] if not command else [command[0], *command[1:]]


def _normalize_command(raw_command: list[str]) -> list[str]:
    command = list(raw_command)
    while command[:1] == ["--"]:
        command = command[1:]
    return command


def _missing_native_attestation_secret(env: dict[str, str]) -> bool:
    return not env.get("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "").strip()


def _native_attestation_secret_state(env: dict[str, str], api_key_env: str, *, require_native_attestation: bool) -> str:
    if not require_native_attestation:
        return "disabled"
    secret = env.get("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "").strip()
    if not secret:
        return "missing"
    api_key = env.get(api_key_env, "").strip()
    if api_key and secret == api_key:
        return "same_as_api_key"
    return "present"


def _write_json(path: Path, payload: dict[str, Any], *, mode: int = 0o600) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, ensure_ascii=False, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    try:
        path.chmod(mode)
    except OSError:
        pass


def _read_guard_ready(proc: subprocess.Popen[str], timeout: float = 10.0) -> dict[str, Any]:
    assert proc.stdout is not None
    deadline = time.time() + timeout
    lines: list[str] = []
    while time.time() < deadline:
        line = proc.stdout.readline()
        if not line:
            if proc.poll() is not None:
                break
            time.sleep(0.05)
            continue
        lines.append(line.rstrip("\n"))
        try:
            return json.loads(line)
        except json.JSONDecodeError:
            continue
    stderr = ""
    if proc.stderr is not None:
        try:
            stderr = proc.stderr.read()
        except Exception:  # noqa: BLE001
            stderr = ""
    raise RuntimeError(f"guard did not become ready; stdout={lines!r}; stderr={stderr[:1000]}")


def _build_env(args: argparse.Namespace, port: int, run_dir: Path) -> dict[str, str]:
    env = dict(os.environ)
    api_key = env.get(args.api_key_env)
    if api_key is not None:
        env[args.api_key_env] = api_key.strip()
    config_dir = Path(args.config_dir).expanduser() if args.config_dir else DEFAULT_LAB_HOME / "claude-config"
    config_dir.mkdir(parents=True, exist_ok=True)
    capture_home = run_dir.parent
    capture_home.mkdir(parents=True, exist_ok=True)

    # Isolate Claude Code auth/config while still allowing normal use in the cwd.
    env["CLAUDE_CONFIG_DIR"] = str(config_dir)
    env["ANTHROPIC_BASE_URL"] = f"http://127.0.0.1:{port}"
    env["CLAUDE_CODE_API_BASE_URL"] = f"http://127.0.0.1:{port}"
    env["ANTHROPIC_API_KEY"] = args.client_api_key or PLACEHOLDER_CLIENT_KEY

    # Do not let local personal bearer/custom credentials leak into this lab.
    for key in (
        "ANTHROPIC_AUTH_TOKEN",
        "ANTHROPIC_CUSTOM_HEADERS",
        "CLAUDE_CODE_OAUTH_TOKEN",
        "CLAUDE_CODE_API_KEY_FILE_DESCRIPTOR",
        "AWS_BEARER_TOKEN_BEDROCK",
    ):
        env.pop(key, None)

    if args.egress_guard:
        proxy = f"http://127.0.0.1:{port}"
        env["HTTP_PROXY"] = proxy
        env["HTTPS_PROXY"] = proxy
        env["ALL_PROXY"] = proxy
        env["http_proxy"] = proxy
        env["https_proxy"] = proxy
        env["all_proxy"] = proxy
        # Main messages go to localhost; guard forwards upstream with proxies disabled.
        env["NO_PROXY"] = "127.0.0.1,localhost,::1"
        env["no_proxy"] = env["NO_PROXY"]
        env["NODE_EXTRA_CA_CERTS"] = str(_cert_path(run_dir))
    else:
        # Messages are still captured through ANTHROPIC_BASE_URL, but direct
        # hard-coded HTTPS control-plane egress may bypass the lab in this mode.
        for key in ("HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"):
            env.pop(key, None)

    env["ZHUMENG_CLAUDE_CAPTURE_RUN_DIR"] = str(run_dir)
    env["ZHUMENG_CLAUDE_CAPTURE_MODE"] = "egress_guard" if args.egress_guard else "messages_only"
    existing_pythonpath = env.get("PYTHONPATH")
    env["PYTHONPATH"] = str(REPO_ROOT) if not existing_pythonpath else f"{REPO_ROOT}{os.pathsep}{existing_pythonpath}"
    return env


def _cert_path(run_dir: Path) -> Path:
    return run_dir / "certs" / "zhumeng-claude-code-local-capture.pem"


def _key_path(run_dir: Path) -> Path:
    return run_dir / "certs" / "zhumeng-claude-code-local-capture.key"


def _ensure_local_capture_cert(run_dir: Path) -> tuple[Path, Path]:
    cert = _cert_path(run_dir)
    key = _key_path(run_dir)
    if cert.exists() and key.exists():
        return cert, key
    cert.parent.mkdir(parents=True, exist_ok=True)
    config_path = cert.parent / "openssl-san.cnf"
    config_path.write_text(
        """
[req]
default_bits = 2048
prompt = no
distinguished_name = dn
x509_extensions = v3_req

[dn]
CN = Zhumeng Claude Code Local Capture

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
            str(key),
            "-out",
            str(cert),
            "-config",
            str(config_path),
        ],
        check=True,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    try:
        key.chmod(0o600)
        cert.chmod(0o644)
        config_path.chmod(0o600)
    except OSError:
        pass
    return cert, key


def _start_guard(args: argparse.Namespace, port: int, run_dir: Path, env: dict[str, str]) -> subprocess.Popen[str]:
    summary = run_dir / "guard-summary.jsonl"
    local_raw_dir = run_dir / "raw-secure"
    cert, key = _ensure_local_capture_cert(run_dir) if args.egress_guard else (None, None)
    guard_cmd = [
        sys.executable,
        str(REPO_ROOT / "tools" / "cli_control_plane_guard.py"),
        "--listen-host",
        "127.0.0.1",
        "--listen-port",
        str(port),
        "--upstream-base",
        args.api_base.rstrip("/"),
        "--sub2api-auth-env",
        args.api_key_env,
        "--summary-path",
        str(summary),
        "--connect-mode",
        "stub" if args.egress_guard else "block",
        "--capture-level",
        args.capture_level,
        "--local-raw-dir",
        str(local_raw_dir),
        "--allow-nonloopback-upstream",
        "--max-messages",
        "0",
        "--cost-max-tokens",
        str(args.cost_max_tokens),
        "--cost-max-body-bytes",
        str(args.cost_max_body_bytes),
        "--cost-max-tools",
        str(args.cost_max_tools),
        "--cost-max-messages",
        str(args.cost_max_messages),
        "--cost-max-content-blocks",
        str(args.cost_max_content_blocks),
        "--cost-max-text-bytes",
        str(args.cost_max_text_bytes),
        "--cost-max-system-bytes",
        str(args.cost_max_system_bytes),
        "--cost-max-tool-def-bytes",
        str(args.cost_max_tool_def_bytes),
        "--cost-max-thinking-budget-tokens",
        str(args.cost_max_thinking_budget_tokens),
        "--cost-allow-stream",
        "--cost-allow-thinking",
        "--cost-allow-assistant-messages",
        "--cost-allow-tool-content",
    ]
    if cert is not None and key is not None:
        guard_cmd.extend(["--cert-path", str(cert), "--key-path", str(key)])
    if args.native_attestation:
        guard_cmd.append("--native-attestation")
    proc = subprocess.Popen(
        guard_cmd,
        cwd=str(REPO_ROOT),
        env=env,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    _read_guard_ready(proc)
    return proc


def _start_netwatch(root_pid: int, run_dir: Path, port: int, interval: float) -> subprocess.Popen[str] | None:
    if interval <= 0:
        return None
    output_path = run_dir / "process-netwatch.jsonl"
    cmd = [
        sys.executable,
        str(REPO_ROOT / "tools" / "claude_code_lab_netwatch.py"),
        "--root-pid",
        str(root_pid),
        "--output-path",
        str(output_path),
        "--interval",
        str(interval),
        "--guard-port",
        str(port),
    ]
    try:
        return subprocess.Popen(
            cmd,
            cwd=str(REPO_ROOT),
            text=True,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
    except Exception:  # noqa: BLE001
        return None


def _write_readme(run_dir: Path, args: argparse.Namespace, command: list[str], port: int) -> None:
    text = f"""# Claude Code local capture run

- started_at: {time.strftime('%Y-%m-%dT%H:%M:%S%z')}
- mode: {'egress_guard' if args.egress_guard else 'messages_only'}
- capture_level: {args.capture_level}
- guard: http://127.0.0.1:{port}
- upstream_base: {args.api_base.rstrip('/')}
- isolated_config_dir: {args.config_dir or str(DEFAULT_LAB_HOME / 'claude-config')}
- command: `{shlex.join(_safe_command_display(command))}`

Files:

- `guard-summary.jsonl`: safe JSONL capture summary only.
- `process-netwatch.jsonl`: safe process-level TCP destination metadata only.
- `run-metadata.json`: run metadata, no API key.
- `report.json` / `report.md`: generated by `tools/claude_code_lab_report.py`.
- `raw-secure/`: only present for local-raw mode; redacted local-only envelopes.

Safety notes:

- This lab sets `CLAUDE_CONFIG_DIR` to an isolated directory and does not modify `~/.claude`.
- Local Claude `Authorization`, `x-api-key`, cookies, and proxy credentials are redacted by the guard.
- Raw prompts, raw request bodies, raw telemetry bodies, and raw tokens are not persisted by this capture path.
- Deep mode records field trees and event names. Local-raw mode still redacts token values and string payloads by default.
- In egress-guard mode, direct CONNECT to Anthropic/Claude domains is stubbed or blocked locally.
- Process netwatch records destination host buckets, ports, process names, and guard-bypass flags only; it does not inspect payloads or headers.
"""
    (run_dir / "README.md").write_text(text, encoding="utf-8")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Run isolated Claude Code CLI through Zhumeng local capture guard")
    parser.add_argument("--api-base", default=os.environ.get("ZHUMENG_API_BASE", DEFAULT_API_BASE))
    parser.add_argument("--api-key-env", default="ZHUMENG_API_KEY")
    parser.add_argument("--client-api-key", help="Placeholder key visible only to local Claude Code; defaults to a non-secret placeholder")
    parser.add_argument("--lab-home", default=str(DEFAULT_LAB_HOME))
    parser.add_argument("--config-dir", help="Override isolated CLAUDE_CONFIG_DIR. Do not point this at ~/.claude.")
    parser.add_argument("--no-egress-guard", dest="egress_guard", action="store_false", help="Only capture /v1/messages via base URL; hard-coded HTTPS control plane may bypass capture.")
    parser.set_defaults(egress_guard=True)
    parser.add_argument("--native-attestation", action="store_true", help="Enable internal native-attestation signing. Off by default for local smoke tests.")
    parser.add_argument("--capture-level", choices=["summary", "deep", "local-raw"], default="deep", help="summary=safe minimal, deep=field trees/event names, local-raw=redacted local-only artifacts")
    parser.add_argument("--netwatch-interval", type=float, default=float(os.environ.get("ZHUMENG_NETWATCH_INTERVAL", "2.0")), help="Seconds between process-level TCP destination snapshots; set 0 to disable.")
    parser.add_argument("--cost-max-tokens", type=int, default=200000)
    parser.add_argument("--cost-max-body-bytes", type=int, default=50 * 1024 * 1024)
    parser.add_argument("--cost-max-tools", type=int, default=512)
    parser.add_argument("--cost-max-messages", type=int, default=2048)
    parser.add_argument("--cost-max-content-blocks", type=int, default=8192)
    parser.add_argument("--cost-max-text-bytes", type=int, default=32 * 1024 * 1024)
    parser.add_argument("--cost-max-system-bytes", type=int, default=8 * 1024 * 1024)
    parser.add_argument("--cost-max-tool-def-bytes", type=int, default=16 * 1024 * 1024)
    parser.add_argument("--cost-max-thinking-budget-tokens", type=int, default=200000)
    parser.add_argument("cmd", nargs=argparse.REMAINDER, help="Command after --; default: claude")
    args = parser.parse_args(argv)

    api_key = os.environ.get(args.api_key_env)
    if not api_key:
        print(f"error: set {args.api_key_env} to your Zhumeng/Sub2API API key before starting", file=sys.stderr)
        return 2
    if "anthropic.com" in args.api_base or "claude.ai" in args.api_base or "claude.com" in args.api_base:
        print("error: --api-base must point to Zhumeng/Sub2API, not Anthropic/Claude", file=sys.stderr)
        return 2

    command = _normalize_command(args.cmd)
    if not command:
        command = ["claude"]
    if shutil.which(command[0]) is None:
        print(f"error: command not found: {command[0]}", file=sys.stderr)
        return 2

    lab_home = Path(args.lab_home).expanduser()
    run_dir = lab_home / "captures" / time.strftime("%Y%m%d-%H%M%S")
    run_dir.mkdir(parents=True, exist_ok=False)
    try:
        run_dir.chmod(0o700)
    except OSError:
        pass

    port = _free_port()
    env = _build_env(args, port, run_dir)
    native_attestation_state = _native_attestation_secret_state(env, args.api_key_env, require_native_attestation=args.native_attestation)
    missing_native_attestation = native_attestation_state == "missing"
    _write_json(run_dir / "run-metadata.json", {
        "started_at": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
        "mode": "egress_guard" if args.egress_guard else "messages_only",
        "capture_level": args.capture_level,
        "api_base": args.api_base.rstrip("/"),
        "guard_url": f"http://127.0.0.1:{port}",
        "isolated_config_dir": env["CLAUDE_CONFIG_DIR"],
        "command": _safe_command_display(command),
        "api_key_env_present": True,
        "stores_request_payload": False,
        "stores_prompt_text": False,
        "stores_raw_token": False,
        "process_netwatch_enabled": args.netwatch_interval > 0,
        "process_netwatch_stores_payload": False,
        "native_attestation_enabled": args.native_attestation,
        "native_attestation_secret_present": not missing_native_attestation,
        "native_attestation_secret_state": native_attestation_state,
    })
    _write_readme(run_dir, args, command, port)

    guard_proc: subprocess.Popen[str] | None = None
    cli_proc: subprocess.Popen[Any] | None = None
    netwatch_proc: subprocess.Popen[str] | None = None
    try:
        guard_proc = _start_guard(args, port, run_dir, env)
        print(f"[zhumeng-lab] capture run: {run_dir}")
        print(f"[zhumeng-lab] isolated CLAUDE_CONFIG_DIR: {env['CLAUDE_CONFIG_DIR']}")
        print(f"[zhumeng-lab] guard: http://127.0.0.1:{port}")
        if args.native_attestation and missing_native_attestation:
            print(
                "[zhumeng-lab] WARNING: 缺少 SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET；"
                "生产的 Claude Code only 原生证明链路会在本机 guard 处返回 403。",
                file=sys.stderr,
            )
        elif args.native_attestation and native_attestation_state == "same_as_api_key":
            print(
                "[zhumeng-lab] WARNING: SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET 不能填写逐梦 API Key；"
                "它必须是生产服务端配置的原生证明共享密钥，否则生产会返回 403。",
                file=sys.stderr,
            )
        print("[zhumeng-lab] starting Claude Code; press Ctrl+C/exit Claude when done")
        cli_proc = subprocess.Popen(command, env=env, cwd=os.getcwd())
        netwatch_proc = _start_netwatch(cli_proc.pid, run_dir, port, args.netwatch_interval)
        return_code = cli_proc.wait()
        _write_json(run_dir / "exit.json", {
            "finished_at": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
            "returncode": return_code,
        })
        return int(return_code or 0)
    except KeyboardInterrupt:
        if cli_proc and cli_proc.poll() is None:
            cli_proc.send_signal(signal.SIGINT)
            try:
                cli_proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                cli_proc.terminate()
        return 130
    finally:
        if netwatch_proc and netwatch_proc.poll() is None:
            netwatch_proc.terminate()
            try:
                netwatch_proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                netwatch_proc.kill()
        if guard_proc and guard_proc.poll() is None:
            guard_proc.terminate()
            try:
                guard_proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                guard_proc.kill()
        try:
            subprocess.run(
                [sys.executable, str(REPO_ROOT / "tools" / "claude_code_lab_report.py"), "--run-dir", str(run_dir)],
                cwd=str(REPO_ROOT),
                check=False,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )
        except Exception:  # noqa: BLE001
            pass
        print(f"[zhumeng-lab] safe capture saved: {run_dir}")
        print(f"[zhumeng-lab] report: {run_dir / 'report.md'}")


if __name__ == "__main__":
    raise SystemExit(main())
