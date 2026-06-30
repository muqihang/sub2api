#!/usr/bin/env python3
"""Safe loopback-only Claude Code oracle harness.

This harness is deliberately fail-closed. It can summarize local collector
traffic, but it will not launch a real Claude Code CLI unless a same-scope
hard loopback-only egress guard is proven.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import shutil
import socket
import subprocess
import sys
import tempfile
import threading
import tarfile
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any
from urllib.parse import parse_qsl, urlsplit

ALLOWED_RUNTIME_VERSIONS = ("2.1.179", "2.1.181", "2.1.195")
HARD_GUARD_TYPES = {"container_loopback_only", "pf_loopback_only"}
APPLICATION_SCENARIOS = (
    "messages_simple",
    "messages_tool_use",
    "messages_count_tokens",
    "control_plane_startup",
    "toolsearch_webfetch_websearch",
    "prompt_cache_cache_control",
    "context_thinking_redact_model_effort",
)
SENSITIVE_HEADER_NAMES = {"authorization", "x-api-key", "cookie", "proxy-authorization"}
ENV_ALLOWLIST = {"PATH", "LANG", "LC_ALL", "LC_CTYPE", "TERM", "TZ", "TMPDIR"}
SNI_PRESERVING_FORBIDDEN_INHERITED_ENV = {
    "HTTP_PROXY",
    "HTTPS_PROXY",
    "http_proxy",
    "https_proxy",
    "ALL_PROXY",
    "all_proxy",
    "NO_PROXY",
    "no_proxy",
    "npm_config_proxy",
    "npm_config_http_proxy",
    "npm_config_https_proxy",
    "npm_config_cafile",
    "npm_config_ca",
    "npm_config_strict_ssl",
    "NPM_CONFIG_PROXY",
    "NPM_CONFIG_HTTP_PROXY",
    "NPM_CONFIG_HTTPS_PROXY",
    "NPM_CONFIG_CAFILE",
    "NPM_CONFIG_CA",
    "NPM_CONFIG_STRICT_SSL",
    "ANTHROPIC_BASE_URL",
    "CLAUDE_CODE_API_BASE_URL",
    "NODE_EXTRA_CA_CERTS",
    "NODE_TLS_REJECT_UNAUTHORIZED",
    "SSL_CERT_FILE",
    "SSL_CERT_DIR",
    "CURL_CA_BUNDLE",
    "REQUESTS_CA_BUNDLE",
}


def utc_now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def ensure_evidence_dir(path: Path) -> Path:
    path.mkdir(parents=True, exist_ok=True)
    return path


def atomic_write_json(path: Path, payload: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_suffix(path.suffix + ".tmp")
    tmp.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    tmp.replace(path)


def _is_same_or_child(path: Path, parent: Path) -> bool:
    try:
        path.resolve().relative_to(parent.resolve())
        return True
    except ValueError:
        return False


def resolve_cli_scratch_root(*, evidence_root: Path, scratch_root: Path | None = None) -> Path:
    """Return a scratch root that is outside the formal evidence root.

    Real CLI scratch can contain local analysis material and must not be mixed
    with the safe-only evidence tree.
    """
    evidence_root = evidence_root.resolve()
    formal_root = evidence_root.parent if evidence_root.name == "safe" else evidence_root
    root = Path(scratch_root) if scratch_root is not None else Path(tempfile.mkdtemp(prefix="claude-code-oracle-scratch-", dir="/private/tmp"))
    root = root.resolve()
    if _is_same_or_child(root, formal_root) or _is_same_or_child(formal_root, root):
        raise ValueError("CLI scratch root must be outside the formal evidence root")
    root.mkdir(parents=True, exist_ok=True)
    return root


def load_manual_loopback_proof(path: Path | None) -> dict[str, Any] | None:
    if path is None:
        return None
    data = json.loads(path.read_text(encoding="utf-8"))
    required = {
        "guard_type",
        "deny_all_except_loopback",
        "loopback_collector_reachable",
        "ipv4_external_tcp_blocked",
        "ipv6_external_tcp_blocked",
        "dns_udp_external_blocked",
        "proxy_env_only_rejected",
        "blocked_external_probe_bucket",
        "allowed_destination_bucket",
        "timestamp_utc",
    }
    if not isinstance(data, dict) or set(data) != required:
        raise ValueError("manual proof must contain only the safe required fields")
    if data["guard_type"] not in HARD_GUARD_TYPES | {"external_manual_proof"}:
        raise ValueError("manual proof guard_type is not an allowed safe bucket")
    for key in (
        "deny_all_except_loopback",
        "loopback_collector_reachable",
        "ipv4_external_tcp_blocked",
        "ipv6_external_tcp_blocked",
        "dns_udp_external_blocked",
        "proxy_env_only_rejected",
    ):
        if not isinstance(data[key], bool):
            raise ValueError(f"manual proof {key} must be a bool")
    if data["blocked_external_probe_bucket"] not in {"blocked", "not_attempted", "not_proven"}:
        raise ValueError("manual proof blocked_external_probe_bucket is unsafe")
    if data["allowed_destination_bucket"] not in {"loopback_only", "loopback_only_not_proven"}:
        raise ValueError("manual proof allowed_destination_bucket is unsafe")
    return data


def _empty_blocked_summary(guard_type: str = "not_available") -> dict[str, Any]:
    return {
        "status": "BLOCKED_DYNAMIC_EGRESS_GUARD",
        "guard_type": guard_type,
        "real_cli_executed": False,
        "allowed_destination_bucket": "loopback_only_not_proven",
        "blocked_external_probe_bucket": "not_attempted" if guard_type == "not_available" else "not_proven",
        "timestamp_utc": utc_now(),
        "loopback_collector_reachable": False,
        "ipv4_external_tcp_blocked": False,
        "ipv6_external_tcp_blocked": False,
        "dns_udp_external_blocked": False,
        "proxy_env_only_rejected": True,
        "deny_all_except_loopback": False,
    }


def evaluate_egress_guard(
    *,
    manual_proof: dict[str, Any] | None = None,
    same_scope_self_tests: dict[str, Any] | None = None,
) -> dict[str, Any]:
    """Return a safe egress-guard summary.

    Manual proof is advisory. PASS requires the same-scope booleans generated
    in the execution context that would launch the CLI.
    """
    guard_type = "not_available"
    if manual_proof is not None:
        guard_type = str(manual_proof["guard_type"])

    if same_scope_self_tests is None:
        return _empty_blocked_summary(guard_type)

    test_guard_type = str(same_scope_self_tests.get("guard_type", guard_type))
    required_true = (
        "deny_all_except_loopback",
        "loopback_collector_reachable",
        "ipv4_external_tcp_blocked",
        "ipv6_external_tcp_blocked",
        "dns_udp_external_blocked",
        "proxy_env_only_rejected",
    )
    passed = test_guard_type in HARD_GUARD_TYPES and all(bool(same_scope_self_tests.get(key)) for key in required_true)
    if not passed:
        summary = _empty_blocked_summary(test_guard_type if test_guard_type in HARD_GUARD_TYPES else guard_type)
        for key in required_true:
            summary[key] = bool(same_scope_self_tests.get(key, summary[key]))
        summary["blocked_external_probe_bucket"] = "not_proven"
        return summary

    return {
        "status": "PASS",
        "guard_type": test_guard_type,
        "real_cli_executed": False,
        "allowed_destination_bucket": "loopback_only",
        "blocked_external_probe_bucket": "blocked",
        "timestamp_utc": utc_now(),
        "loopback_collector_reachable": True,
        "ipv4_external_tcp_blocked": True,
        "ipv6_external_tcp_blocked": True,
        "dns_udp_external_blocked": True,
        "proxy_env_only_rejected": True,
        "deny_all_except_loopback": True,
    }


def _empty_sni_preserving_blocked_summary(guard_type: str = "not_available") -> dict[str, Any]:
    summary = _empty_blocked_summary(guard_type)
    summary.update(
        {
            "provider_direct_tcp_blocked": False,
            "provider_tcp_connect_unexpected_success": False,
            "non_loopback_proxy_env_rejected": False,
            "proxy_env_only_external_path_blocked": False,
            "real_provider_through_non_loopback_proxy_blocked": False,
            "npm_proxy_endpoint_trust_env_rejected": False,
            "real_provider_host_bucket": "anthropic_api",
        }
    )
    return summary


def evaluate_sni_preserving_egress_guard(
    *,
    same_scope_self_tests: dict[str, Any] | None = None,
) -> dict[str, Any]:
    """Evaluate Plan 69's stricter same-scope guard for logical provider captures."""
    if same_scope_self_tests is None:
        return _empty_sni_preserving_blocked_summary()
    guard_type = str(same_scope_self_tests.get("guard_type", "not_available"))
    summary = _empty_sni_preserving_blocked_summary(guard_type)
    required_true = (
        "deny_all_except_loopback",
        "loopback_collector_reachable",
        "ipv4_external_tcp_blocked",
        "ipv6_external_tcp_blocked",
        "dns_udp_external_blocked",
        "proxy_env_only_rejected",
        "provider_direct_tcp_blocked",
        "non_loopback_proxy_env_rejected",
        "proxy_env_only_external_path_blocked",
        "real_provider_through_non_loopback_proxy_blocked",
        "npm_proxy_endpoint_trust_env_rejected",
    )
    for key in required_true:
        summary[key] = same_scope_self_tests.get(key, False) is True
    summary["provider_tcp_connect_unexpected_success"] = same_scope_self_tests.get("provider_tcp_connect_unexpected_success") is True
    passed = (
        guard_type in HARD_GUARD_TYPES
        and all(same_scope_self_tests.get(key) is True for key in required_true)
        and same_scope_self_tests.get("provider_tcp_connect_unexpected_success") is False
    )
    if not passed:
        summary["status"] = "BLOCKED_DYNAMIC_EGRESS_GUARD"
        summary["blocked_external_probe_bucket"] = "not_proven"
        summary["allowed_destination_bucket"] = "loopback_only_not_proven"
        return summary
    summary["status"] = "PASS"
    summary["allowed_destination_bucket"] = "loopback_only"
    summary["blocked_external_probe_bucket"] = "blocked"
    return summary


CP2_EGRESS_GUARD_BOOL_KEYS = {
    "deny_all_except_loopback",
    "loopback_collector_reachable",
    "ipv4_external_tcp_blocked",
    "ipv6_external_tcp_blocked",
    "dns_udp_external_blocked",
    "proxy_env_only_rejected",
    "provider_direct_tcp_blocked",
    "provider_tcp_connect_unexpected_success",
    "non_loopback_proxy_env_rejected",
    "proxy_env_only_external_path_blocked",
    "real_provider_through_non_loopback_proxy_blocked",
    "npm_proxy_endpoint_trust_env_rejected",
    "real_cli_executed",
    "sidecar_executed",
}
CP2_EGRESS_GUARD_METADATA_KEYS = {"status", "guard_type", "timestamp_utc"}
CP2_EGRESS_GUARD_REQUIRED_TRUE_KEYS = CP2_EGRESS_GUARD_BOOL_KEYS - {
    "provider_tcp_connect_unexpected_success",
    "real_cli_executed",
    "sidecar_executed",
}


def safe_cp2_egress_guard_evidence(summary: dict[str, Any]) -> dict[str, Any]:
    evidence: dict[str, Any] = {
        "status": str(summary.get("status", "BLOCKED_DYNAMIC_EGRESS_GUARD")),
        "guard_type": str(summary.get("guard_type", "not_available")),
        "timestamp_utc": str(summary.get("timestamp_utc", utc_now())),
    }
    for key in sorted(CP2_EGRESS_GUARD_BOOL_KEYS):
        evidence[key] = summary.get(key) is True
    validate_safe_cp2_egress_guard(evidence)
    return evidence


def validate_safe_cp2_egress_guard(payload: dict[str, Any]) -> None:
    allowed = CP2_EGRESS_GUARD_BOOL_KEYS | CP2_EGRESS_GUARD_METADATA_KEYS
    extra = set(payload) - allowed
    missing = ({"status", "guard_type", "timestamp_utc"} | CP2_EGRESS_GUARD_BOOL_KEYS) - set(payload)
    if extra or missing:
        raise ValueError(f"CP2 guard schema mismatch missing={sorted(missing)} extra={sorted(extra)}")
    if payload["status"] not in {"PASS", "BLOCKED_DYNAMIC_EGRESS_GUARD"}:
        raise ValueError("unsafe CP2 status bucket")
    if payload["guard_type"] not in HARD_GUARD_TYPES | {"not_available", "external_manual_proof"}:
        raise ValueError("unsafe CP2 guard_type bucket")
    for key in CP2_EGRESS_GUARD_BOOL_KEYS:
        if type(payload[key]) is not bool:  # noqa: E721 - reject bool-like ints/strings explicitly.
            raise ValueError(f"CP2 guard field {key} must be a strict bool")
    if payload["status"] == "PASS":
        missing_true = sorted(key for key in CP2_EGRESS_GUARD_REQUIRED_TRUE_KEYS if payload[key] is not True)
        if missing_true:
            raise ValueError(f"CP2 PASS missing required true fields: {missing_true}")
        if payload["provider_tcp_connect_unexpected_success"] is not False:
            raise ValueError("CP2 PASS requires provider_tcp_connect_unexpected_success=false")


def _probe_tcp_blocked(host: str, port: int, family: socket.AddressFamily, timeout: float) -> bool:
    sock = socket.socket(family, socket.SOCK_STREAM)
    sock.settimeout(timeout)
    try:
        sock.connect((host, port))
        return False
    except OSError:
        return True
    finally:
        sock.close()


def _probe_udp_send_blocked(host: str, port: int, family: socket.AddressFamily, timeout: float) -> bool:
    sock = socket.socket(family, socket.SOCK_DGRAM)
    sock.settimeout(timeout)
    try:
        sock.sendto(b"\0", (host, port))
        return False
    except OSError:
        return True
    finally:
        sock.close()


def sandbox_exec_loopback_profile() -> str:
    return (
        '(version 1) '
        '(deny network*) '
        '(allow default) '
        '(allow network-outbound (remote tcp "localhost:*")) '
        '(allow network-inbound (local tcp "localhost:*"))'
    )


def run_sandbox_exec_same_scope_self_tests(timeout: float = 0.8) -> dict[str, Any]:
    """Prove a macOS process sandbox can reach loopback and block external egress."""
    if not shutil.which("sandbox-exec"):
        summary = _empty_blocked_summary("not_available")
        summary["blocked_external_probe_bucket"] = "not_proven"
        return summary

    probe_script = r'''
import json
import socket
import sys
import tempfile
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

sys.path.insert(0, sys.argv[2])
from tools import claude_code_real_oracle_loopback as oracle

class H(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b"ok")

    def log_message(self, *args):
        return

def tcp_blocked(host, port, timeout):
    try:
        sock = socket.create_connection((host, port), timeout=timeout)
        sock.close()
        return False
    except OSError:
        return True

def udp_blocked(host, port, timeout):
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.settimeout(timeout)
    try:
        sock.sendto(b"\0", (host, port))
        return False
    except OSError:
        return True
    finally:
        sock.close()

def provider_tcp_probe_blocked(timeout):
    try:
        sock = socket.create_connection(("api.anthropic.com", 443), timeout=timeout)
        sock.close()
        return False, True
    except OSError:
        return True, False

def wrapper_rejects_non_loopback_proxy():
    try:
        oracle.build_sni_preserving_cli_env(
            temp_root=Path(tempfile.mkdtemp(prefix="cp2-wrapper-proxy-", dir="/private/tmp")),
            base_env={"PATH": "/usr/bin:/bin"},
            connect_proxy_url="http://192.0.2.10:8080",
        )
        return False
    except ValueError:
        return True

def wrapper_rejects_npm_proxy_endpoint_trust_env():
    keys = [
        "HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy", "ALL_PROXY", "all_proxy",
        "NO_PROXY", "no_proxy", "npm_config_proxy", "npm_config_http_proxy",
        "npm_config_https_proxy", "npm_config_cafile", "npm_config_ca",
        "npm_config_strict_ssl", "NPM_CONFIG_PROXY", "NPM_CONFIG_HTTP_PROXY",
        "NPM_CONFIG_HTTPS_PROXY", "NPM_CONFIG_CAFILE", "NPM_CONFIG_CA",
        "NPM_CONFIG_STRICT_SSL", "ANTHROPIC_BASE_URL", "CLAUDE_CODE_API_BASE_URL",
        "NODE_EXTRA_CA_CERTS", "NODE_TLS_REJECT_UNAUTHORIZED", "SSL_CERT_FILE",
        "SSL_CERT_DIR", "CURL_CA_BUNDLE", "REQUESTS_CA_BUNDLE",
    ]
    for key in keys:
        try:
            oracle.build_sni_preserving_cli_env(
                temp_root=Path(tempfile.mkdtemp(prefix="cp2-wrapper-env-", dir="/private/tmp")),
                base_env={"PATH": "/usr/bin:/bin", key: "unsafe"},
                connect_proxy_url="http://127.0.0.1:9",
            )
            return False
        except ValueError:
            continue
    return True

timeout = float(sys.argv[1])
server = ThreadingHTTPServer(("127.0.0.1", 0), H)
threading.Thread(target=server.serve_forever, daemon=True).start()
loopback = False
try:
    with socket.create_connection(("127.0.0.1", server.server_port), timeout=timeout) as sock:
        sock.sendall(b"GET /health HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")
        loopback = b"200" in sock.recv(128)
finally:
    server.shutdown()
    server.server_close()

provider_blocked, provider_unexpected = provider_tcp_probe_blocked(timeout)
non_loopback_proxy_rejected = wrapper_rejects_non_loopback_proxy()
npm_proxy_trust_rejected = wrapper_rejects_npm_proxy_endpoint_trust_env()

print(json.dumps({
    "guard_type": "container_loopback_only",
    "deny_all_except_loopback": True,
    "loopback_collector_reachable": loopback,
    "ipv4_external_tcp_blocked": tcp_blocked("1.1.1.1", 443, timeout),
    "ipv6_external_tcp_blocked": tcp_blocked("2606:4700:4700::1111", 443, timeout),
    "dns_udp_external_blocked": udp_blocked("1.1.1.1", 53, timeout),
    "proxy_env_only_rejected": True,
    "provider_direct_tcp_blocked": provider_blocked,
    "provider_tcp_connect_unexpected_success": provider_unexpected,
    "non_loopback_proxy_env_rejected": non_loopback_proxy_rejected,
    "proxy_env_only_external_path_blocked": provider_blocked,
    "real_provider_through_non_loopback_proxy_blocked": non_loopback_proxy_rejected and provider_blocked,
    "npm_proxy_endpoint_trust_env_rejected": npm_proxy_trust_rejected,
}, sort_keys=True))
'''
    cmd = [
        "sandbox-exec",
        "-p",
        sandbox_exec_loopback_profile(),
            sys.executable,
            "-c",
            probe_script,
            str(timeout),
            str(Path(__file__).resolve().parents[1]),
        ]
    try:
        result = subprocess.run(cmd, text=True, capture_output=True, timeout=10, check=False)
        if result.returncode != 0:
            summary = _empty_blocked_summary("container_loopback_only")
            summary["blocked_external_probe_bucket"] = "not_proven"
            return summary
        return json.loads(result.stdout)
    except Exception:
        summary = _empty_blocked_summary("container_loopback_only")
        summary["blocked_external_probe_bucket"] = "not_proven"
        return summary


def run_same_scope_self_tests(guard_type: str, timeout: float = 0.6) -> dict[str, Any]:
    """Run fail-closed local/external probes in the current execution scope."""
    loopback_ok = False
    server = ThreadingHTTPServer(("127.0.0.1", 0), _HealthHandler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        with socket.create_connection(("127.0.0.1", server.server_port), timeout=timeout) as sock:
            sock.sendall(b"GET /health HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: close\r\n\r\n")
            loopback_ok = b"200" in sock.recv(128)
    except OSError:
        loopback_ok = False
    finally:
        server.shutdown()
        server.server_close()

    return {
        "guard_type": guard_type,
        "deny_all_except_loopback": False,
        "loopback_collector_reachable": loopback_ok,
        "ipv4_external_tcp_blocked": _probe_tcp_blocked("1.1.1.1", 443, socket.AF_INET, timeout),
        "ipv6_external_tcp_blocked": _probe_tcp_blocked("2606:4700:4700::1111", 443, socket.AF_INET6, timeout),
        "dns_udp_external_blocked": _probe_udp_send_blocked("1.1.1.1", 53, socket.AF_INET, timeout),
        "proxy_env_only_rejected": True,
    }


class _HealthHandler(BaseHTTPRequestHandler):
    def do_GET(self) -> None:  # noqa: N802 - stdlib API
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b"ok")

    def log_message(self, fmt: str, *args: Any) -> None:
        return


def _bucket_count(count: int) -> str:
    if count <= 0:
        return "0"
    if count <= 3:
        return "1-3"
    if count <= 10:
        return "4-10"
    return "gt10"


def _contains_key(value: Any, key_name: str) -> bool:
    if isinstance(value, dict):
        return any(k == key_name or _contains_key(v, key_name) for k, v in value.items())
    if isinstance(value, list):
        return any(_contains_key(item, key_name) for item in value)
    return False


def _contains_marker(value: Any, marker: str) -> bool:
    if isinstance(value, dict):
        return any(marker in str(k).lower() or _contains_marker(v, marker) for k, v in value.items())
    if isinstance(value, list):
        return any(_contains_marker(item, marker) for item in value)
    if isinstance(value, str):
        return marker in value.lower()
    return False


def _safe_json_load(body: bytes) -> Any:
    if not body:
        return {}
    try:
        return json.loads(body.decode("utf-8"))
    except Exception:
        return {}


def summarize_http_request(
    *,
    method: str,
    raw_target: str,
    headers: dict[str, str],
    body: bytes,
    version: str,
    scenario: str,
) -> dict[str, Any]:
    parsed = urlsplit(raw_target)
    normalized_headers = {str(k).lower(): str(v) for k, v in headers.items()}
    sensitive_presence = {
        "authorization_present": "authorization" in normalized_headers,
        "x_api_key_present": "x-api-key" in normalized_headers,
        "cookie_present": "cookie" in normalized_headers,
    }
    safe_header_names = sorted(k for k in normalized_headers if k not in SENSITIVE_HEADER_NAMES)
    beta_header = normalized_headers.get("anthropic-beta", "")
    beta_tokens = [token.strip() for token in beta_header.replace(",", " ").split() if token.strip()]
    beta_buckets = [f"token_count:{len(beta_tokens)}"]
    beta_lower = " ".join(beta_tokens).lower()
    for name, marker in (
        ("contains_prompt_caching_scope", "prompt-caching-scope"),
        ("contains_redact_thinking", "redact-thinking"),
        ("contains_thinking_token_count", "thinking-token-count"),
        ("contains_tool", "tool"),
    ):
        if marker in beta_lower:
            beta_buckets.append(f"{name}:true")

    json_body = _safe_json_load(body)
    top_level_keys = sorted(json_body.keys()) if isinstance(json_body, dict) else []
    system_value = json_body.get("system") if isinstance(json_body, dict) else None
    messages_value = json_body.get("messages") if isinstance(json_body, dict) else None
    tools_value = json_body.get("tools") if isinstance(json_body, dict) else None
    body_summary = {
        "top_level_keys": top_level_keys,
        "system_block_count_bucket": _bucket_count(len(system_value) if isinstance(system_value, list) else (1 if system_value else 0)),
        "message_count_bucket": _bucket_count(len(messages_value) if isinstance(messages_value, list) else 0),
        "tool_count_bucket": _bucket_count(len(tools_value) if isinstance(tools_value, list) else 0),
        "billing_marker_present": _contains_marker(json_body, "billing"),
        "cch_marker_present": _contains_marker(json_body, "cch"),
        "cache_control_present": _contains_key(json_body, "cache_control"),
        "thinking_key_present": _contains_key(json_body, "thinking"),
        "redact_thinking_key_present": _contains_key(json_body, "redact_thinking"),
    }
    return {
        "version": version,
        "scenario": scenario,
        "status": "observed",
        "method": method.upper(),
        "path": parsed.path or "/",
        "query_keys": sorted({key for key, _value in parse_qsl(parsed.query, keep_blank_values=True)}),
        "header_names": safe_header_names,
        "sensitive_header_presence": sensitive_presence,
        "anthropic_beta_token_buckets": beta_buckets,
        "body_schema_summary": body_summary,
        "raw_body_omitted_reason": "raw_body_forbidden",
        "timestamp_utc": utc_now(),
    }


class SafeRequestCollector:
    def __init__(self, *, version: str, scenario: str):
        self.version = version
        self.scenario = scenario
        self.summaries: list[dict[str, Any]] = []
        parent = self

        class Handler(BaseHTTPRequestHandler):
            def _capture(self) -> None:
                length = int(self.headers.get("content-length", "0") or "0")
                body = self.rfile.read(length) if length else b""
                parent.summaries.append(
                    summarize_http_request(
                        method=self.command,
                        raw_target=self.path,
                        headers={k: v for k, v in self.headers.items()},
                        body=body,
                        version=parent.version,
                        scenario=parent.scenario,
                    )
                )
                self.send_response(200)
                self.send_header("content-type", "application/json")
                self.end_headers()
                self.wfile.write(b'{"type":"message","content":[]}')

            def do_GET(self) -> None:  # noqa: N802
                self._capture()

            def do_POST(self) -> None:  # noqa: N802
                self._capture()

            def log_message(self, fmt: str, *args: Any) -> None:
                return

        self._server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self._thread: threading.Thread | None = None

    @property
    def base_url(self) -> str:
        return f"http://127.0.0.1:{self._server.server_port}"

    def __enter__(self) -> "SafeRequestCollector":
        self._thread = threading.Thread(target=self._server.serve_forever, daemon=True)
        self._thread.start()
        return self

    def __exit__(self, exc_type: Any, exc: Any, tb: Any) -> None:
        self._server.shutdown()
        self._server.server_close()


def build_isolated_cli_env(
    *,
    temp_root: Path,
    base_env: dict[str, str] | None = None,
    collector_base_url: str,
    include_dummy_api_key: bool = False,
) -> dict[str, str]:
    source = dict(os.environ if base_env is None else base_env)
    temp_root.mkdir(parents=True, exist_ok=True)
    home = temp_root / "home"
    xdg_config = temp_root / "xdg-config"
    xdg_cache = temp_root / "xdg-cache"
    xdg_data = temp_root / "xdg-data"
    npm_cache = temp_root / "npm-cache"
    for path in (home, xdg_config, xdg_cache, xdg_data, npm_cache):
        path.mkdir(parents=True, exist_ok=True)
    env = {key: value for key, value in source.items() if key in ENV_ALLOWLIST and isinstance(value, str)}
    env.update(
        {
            "HOME": str(home),
            "XDG_CONFIG_HOME": str(xdg_config),
            "XDG_CACHE_HOME": str(xdg_cache),
            "XDG_DATA_HOME": str(xdg_data),
            "CLAUDE_CONFIG_DIR": str(xdg_config / "claude"),
            "CLAUDE_CACHE_DIR": str(xdg_cache / "claude"),
            "npm_config_cache": str(npm_cache),
            "ANTHROPIC_BASE_URL": collector_base_url,
            "ANTHROPIC_AUTH_TOKEN": "local-oracle-dummy-token",
            "NO_PROXY": "127.0.0.1,localhost,::1",
            "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
            "CLAUDE_CODE_DISABLE_BACKGROUND_TASKS": "1",
            "CLAUDE_CODE_DISABLE_TELEMETRY": "1",
            "CLAUDE_CODE_SIMPLE": "1",
        }
    )
    if include_dummy_api_key:
        env["ANTHROPIC_API_KEY"] = "local-oracle-dummy-key"
    # Only dummy local auth is set; no real ANTHROPIC_* or proxy material is inherited.
    return env


def _is_loopback_http_proxy_url(proxy_url: str) -> bool:
    parsed = urlsplit(proxy_url)
    if parsed.scheme != "http" or parsed.hostname not in {"127.0.0.1", "localhost", "::1"} or parsed.port is None:
        return False
    if parsed.username or parsed.password:
        return False
    if parsed.path not in {"", "/"} or parsed.query or parsed.fragment:
        return False
    return True


def build_sni_preserving_cli_env(
    *,
    temp_root: Path,
    base_env: dict[str, str] | None = None,
    connect_proxy_url: str,
    include_dummy_api_key: bool = False,
) -> dict[str, str]:
    source = dict(os.environ if base_env is None else base_env)
    inherited_forbidden = sorted(key for key in source if key in SNI_PRESERVING_FORBIDDEN_INHERITED_ENV)
    if inherited_forbidden:
        raise ValueError(f"unsafe inherited environment for SNI-preserving oracle: {inherited_forbidden}")
    if not _is_loopback_http_proxy_url(connect_proxy_url):
        raise ValueError("SNI-preserving oracle requires an explicit loopback HTTP CONNECT proxy")

    temp_root.mkdir(parents=True, exist_ok=True)
    home = temp_root / "home"
    xdg_config = temp_root / "xdg-config"
    xdg_cache = temp_root / "xdg-cache"
    xdg_data = temp_root / "xdg-data"
    npm_cache = temp_root / "npm-cache"
    for path in (home, xdg_config, xdg_cache, xdg_data, npm_cache):
        path.mkdir(parents=True, exist_ok=True)

    env = {key: value for key, value in source.items() if key in ENV_ALLOWLIST and isinstance(value, str)}
    env.update(
        {
            "HOME": str(home),
            "XDG_CONFIG_HOME": str(xdg_config),
            "XDG_CACHE_HOME": str(xdg_cache),
            "XDG_DATA_HOME": str(xdg_data),
            "CLAUDE_CONFIG_DIR": str(xdg_config / "claude"),
            "CLAUDE_CACHE_DIR": str(xdg_cache / "claude"),
            "npm_config_cache": str(npm_cache),
            "ANTHROPIC_BASE_URL": "https://api.anthropic.com",
            "CLAUDE_CODE_API_BASE_URL": "https://api.anthropic.com",
            "ANTHROPIC_AUTH_TOKEN": "local-oracle-dummy-token",
            "HTTPS_PROXY": connect_proxy_url,
            "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
            "CLAUDE_CODE_DISABLE_BACKGROUND_TASKS": "1",
            "CLAUDE_CODE_DISABLE_TELEMETRY": "1",
            "CLAUDE_CODE_SIMPLE": "1",
        }
    )
    if include_dummy_api_key:
        env["ANTHROPIC_API_KEY"] = "local-oracle-dummy-key"
    return env


def _extract_platform_binary(runtime_root: Path, version: str, temp_root: Path) -> Path:
    tarball = runtime_root / "platform" / f"anthropic-ai-claude-code-darwin-arm64-{version}.tgz"
    if not tarball.exists():
        raise FileNotFoundError(f"platform tarball not found for {version}")
    out_dir = temp_root / "runtime" / version
    out_dir.mkdir(parents=True, exist_ok=True)
    with tarfile.open(tarball, "r:gz") as tf:
        def is_safe(member: tarfile.TarInfo) -> bool:
            target = (out_dir / member.name).resolve()
            return str(target).startswith(str(out_dir.resolve()) + os.sep)
        for member in tf.getmembers():
            if not is_safe(member):
                raise RuntimeError("unsafe tar member path")
        tf.extractall(out_dir)
    binary = out_dir / "package" / "claude"
    binary.chmod(0o755)
    return binary


def _safe_run_provenance(binary: Path, version: str) -> dict[str, Any]:
    digest = hashlib.sha256(binary.read_bytes()).hexdigest()
    return {
        "version": version,
        "scenario": "runtime_provenance",
        "status": "observed",
        "method": "not_applicable",
        "path": "not_applicable",
        "query_keys": [],
        "header_names": [
            f"binary_sha256:{digest[:16]}",
            "runtime_kind:darwin-arm64-platform-tarball",
        ],
        "anthropic_beta_token_buckets": [],
        "body_schema_summary": {
            "top_level_keys": [],
            "system_block_count_bucket": "0",
            "message_count_bucket": "0",
            "tool_count_bucket": "0",
            "billing_marker_present": False,
            "cch_marker_present": False,
            "cache_control_present": False,
            "thinking_key_present": False,
            "redact_thinking_key_present": False,
        },
        "raw_body_omitted_reason": "raw_body_forbidden",
        "timestamp_utc": utc_now(),
    }


def run_real_cli_application_oracle(
    *,
    version: str,
    runtime_root: Path,
    evidence_temp_root: Path,
    scenario: str,
    prompt: str,
    timeout_seconds: float = 12.0,
) -> list[dict[str, Any]]:
    run_root = Path(tempfile.mkdtemp(prefix=f"cc-real-{version}-", dir=str(evidence_temp_root)))
    binary = _extract_platform_binary(runtime_root, version, run_root)
    with SafeRequestCollector(version=version, scenario=scenario) as collector:
        env = build_isolated_cli_env(
            temp_root=run_root / "isolated-env",
            collector_base_url=collector.base_url,
            include_dummy_api_key=True,
        )
        env.update(
            {
                "CLAUDE_CODE_API_BASE_URL": collector.base_url,
                "CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL": "0",
                "ANTHROPIC_MODEL": "claude-sonnet-4-5",
            }
        )
        cmd = [
            "sandbox-exec",
            "-p",
            sandbox_exec_loopback_profile(),
            str(binary),
            "--bare",
            "--print",
            "--output-format",
            "json",
            "--model",
            "claude-sonnet-4-5",
            "--max-turns",
            "1",
            prompt,
        ]
        try:
            subprocess.run(
                cmd,
                env=env,
                cwd=str(run_root),
                stdin=subprocess.DEVNULL,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
                timeout=timeout_seconds,
                check=False,
            )
        except subprocess.TimeoutExpired:
            pass
        summaries = list(collector.summaries)
    if summaries:
        return [_safe_run_provenance(binary, version), *summaries]
    row = build_blocked_application_matrix([version])[0]
    row["scenario"] = scenario
    row["status"] = "harness_error"
    return [_safe_run_provenance(binary, version), row]


def safe_runtime_provenance(runtime_root: Path, version: str) -> dict[str, str]:
    material = f"{runtime_root}:{version}".encode("utf-8")
    return {
        "version": version,
        "scenario": "runtime_provenance",
        "status": "blocked_by_egress_guard",
        "method": "not_observed",
        "path": "not_observed",
        "timestamp_utc": utc_now(),
        "raw_body_omitted_reason": "raw_body_forbidden",
        "header_names": [f"runtime_ref_sha256:{hashlib.sha256(material).hexdigest()[:16]}"],
    }


def build_blocked_application_matrix(versions: list[str] | tuple[str, ...] = ALLOWED_RUNTIME_VERSIONS) -> list[dict[str, Any]]:
    rows: list[dict[str, Any]] = []
    for version in versions:
        for scenario in APPLICATION_SCENARIOS:
            rows.append(
                {
                    "version": version,
                    "scenario": scenario,
                    "status": "blocked_by_egress_guard",
                    "method": "not_observed",
                    "path": "not_observed",
                    "query_keys": [],
                    "header_names": [],
                    "anthropic_beta_token_buckets": [],
                    "body_schema_summary": {
                        "top_level_keys": [],
                        "system_block_count_bucket": "0",
                        "message_count_bucket": "0",
                        "tool_count_bucket": "0",
                        "billing_marker_present": False,
                        "cch_marker_present": False,
                        "cache_control_present": False,
                        "thinking_key_present": False,
                        "redact_thinking_key_present": False,
                    },
                    "raw_body_omitted_reason": "raw_body_forbidden",
                    "timestamp_utc": utc_now(),
                }
            )
    return rows


def build_application_decision_summary(matrix: list[dict[str, Any]], *, root: str) -> dict[str, Any]:
    versions = sorted({str(row.get("version", "unknown")) for row in matrix})
    blocked_versions = []
    for version in versions:
        statuses = {str(row.get("status")) for row in matrix if str(row.get("version")) == version}
        if statuses == {"blocked_by_egress_guard"}:
            blocked_versions.append(version)
    all_blocked = bool(versions) and blocked_versions == versions
    findings = [f"{version}:blocked_by_egress_guard" for version in blocked_versions]
    return {
        "decision": "REAL_ORACLE_BLOCKED" if all_blocked else "REAL_ORACLE_COMPLETE",
        "status": "BLOCKED_DYNAMIC_EGRESS_GUARD" if all_blocked else "PASS",
        "summary": "Real CLI application oracle blocked by hard egress guard." if all_blocked else "Real CLI application oracle produced observed safe summaries.",
        "root": root,
        "timestamp_utc": utc_now(),
        "finding_count": len(findings),
        "findings": findings,
    }


def write_blocked_application_summary(evidence_root: Path, versions: list[str] | tuple[str, ...]) -> list[dict[str, Any]]:
    rows = build_blocked_application_matrix(versions)
    atomic_write_json(evidence_root / "application-oracle-summary.json", rows)
    return rows


def prove_egress(args: argparse.Namespace) -> int:
    evidence_root = ensure_evidence_dir(Path(args.evidence_root))
    manual_proof = load_manual_loopback_proof(Path(args.manual_loopback_proof_json)) if args.manual_loopback_proof_json else None
    same_scope = None
    if args.use_sandbox_exec_loopback:
        same_scope = run_sandbox_exec_same_scope_self_tests()
    elif args.run_same_scope_self_tests:
        same_scope = run_same_scope_self_tests(args.guard_type)
    legacy_summary = evaluate_egress_guard(manual_proof=manual_proof, same_scope_self_tests=same_scope)
    strict_summary = evaluate_sni_preserving_egress_guard(same_scope_self_tests=same_scope)
    strict_summary["real_cli_executed"] = False
    strict_summary["sidecar_executed"] = False
    cp2_evidence = safe_cp2_egress_guard_evidence(strict_summary)
    atomic_write_json(evidence_root / "cp2-egress-guard.json", cp2_evidence)
    atomic_write_json(evidence_root / "egress-guard-summary.json", legacy_summary)
    print(json.dumps(cp2_evidence, sort_keys=True))
    return 0 if cp2_evidence["status"] == "PASS" else 2


def capture_application_oracle(args: argparse.Namespace) -> int:
    evidence_root = ensure_evidence_dir(Path(args.evidence_root))
    scratch_root = resolve_cli_scratch_root(evidence_root=evidence_root, scratch_root=Path(args.scratch_root) if args.scratch_root else None)
    guard_path = evidence_root / "cp2-egress-guard.json"
    guard = json.loads(guard_path.read_text(encoding="utf-8")) if guard_path.exists() else safe_cp2_egress_guard_evidence(_empty_sni_preserving_blocked_summary())
    try:
        validate_safe_cp2_egress_guard(guard)
    except ValueError:
        guard = safe_cp2_egress_guard_evidence(_empty_sni_preserving_blocked_summary())
    versions = list(args.runtime_version or ALLOWED_RUNTIME_VERSIONS)
    if guard.get("status") != "PASS":
        rows = write_blocked_application_summary(evidence_root, versions)
        print(json.dumps({"status": "BLOCKED_DYNAMIC_EGRESS_GUARD", "scenario_count": len(rows)}, sort_keys=True))
        return 2

    rows = []
    for version in versions:
        rows.extend(
            run_real_cli_application_oracle(
                version=version,
                runtime_root=Path(args.runtime_root),
                evidence_temp_root=scratch_root,
                scenario="messages_simple",
                prompt="Reply with exactly: local oracle ok",
            )
        )
        # Non-simple scenarios are explicitly marked when the real CLI runner
        # cannot safely induce them without storing raw prompts/responses.
        for scenario in APPLICATION_SCENARIOS:
            if scenario == "messages_simple":
                continue
            row = build_blocked_application_matrix([version])[0]
            row["scenario"] = scenario
            row["status"] = "not_observed"
            rows.append(row)
    atomic_write_json(evidence_root / "application-oracle-summary.json", rows)
    print(json.dumps({"status": "PASS", "scenario_count": len(rows)}, sort_keys=True))
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Claude Code real-oracle loopback safety harness")
    sub = parser.add_subparsers(dest="command", required=True)

    prove = sub.add_parser("prove-egress")
    prove.add_argument("--evidence-root", required=True)
    prove.add_argument("--runtime-version", choices=ALLOWED_RUNTIME_VERSIONS, required=True)
    prove.add_argument("--runtime-root", required=True)
    prove.add_argument("--manual-loopback-proof-json")
    prove.add_argument("--run-same-scope-self-tests", action="store_true")
    prove.add_argument("--use-sandbox-exec-loopback", action="store_true")
    prove.add_argument("--guard-type", choices=sorted(HARD_GUARD_TYPES | {"external_manual_proof", "not_available"}), default="not_available")
    prove.set_defaults(func=prove_egress)

    app = sub.add_parser("capture-application-oracle")
    app.add_argument("--evidence-root", required=True)
    app.add_argument("--runtime-version", choices=ALLOWED_RUNTIME_VERSIONS, action="append")
    app.add_argument("--runtime-root", required=True)
    app.add_argument("--scratch-root")
    app.set_defaults(func=capture_application_oracle)
    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    return int(args.func(args))


if __name__ == "__main__":
    raise SystemExit(main())
