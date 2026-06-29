#!/usr/bin/env python3
"""Safe TLS ClientHello oracle for Claude Code and local egress profiles.

The collector reads ClientHello bytes in memory, emits only safe summaries, and
never writes raw ClientHello, pcap, keys, certificates, prompts, or responses.
"""

from __future__ import annotations

import argparse
import dataclasses
import hashlib
import json
import os
import shutil
import socket
import socketserver
import subprocess
import sys
import tempfile
import threading
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Iterable

if __package__ in {None, ""}:
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from tools import claude_code_real_oracle_loopback as app_oracle

ALLOWED_RUNTIME_VERSIONS = app_oracle.ALLOWED_RUNTIME_VERSIONS
SUMMARY_SOURCES = {"claude_code_cli", "sub2api_utls_builtin", "cc_gateway_node_agent", "unit"}
FORBIDDEN_TLS_KEYS = {
    "raw_clienthello",
    "raw_client_hello",
    "clienthello",
    "pcap",
    "certificate",
    "raw_certificate",
    "private_key",
    "raw_private_key",
    "key",
    "cert",
}
REQUIRED_SAFE_SUMMARY_KEYS = {
    "source",
    "version",
    "ja3_hash",
    "ja4",
    "alpn_protocols",
    "tls_versions",
    "cipher_count",
    "extension_count",
    "grease_present",
    "node_version_bucket",
    "openssl_version_bucket",
    "agent_package_versions",
    "raw_clienthello_omitted_reason",
    "timestamp_utc",
}
GREASE_VALUES = {0x0A0A + (i << 8) + i for i in range(0, 0x100, 0x10)}
TLS_VERSION_NAMES = {
    0x0301: "0x0301",
    0x0302: "0x0302",
    0x0303: "0x0303",
    0x0304: "0x0304",
}


def utc_now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


@dataclasses.dataclass(frozen=True)
class TLSSummary:
    source: str
    version: str
    ja3_hash: str
    ja4: str
    alpn_protocols: tuple[str, ...]
    tls_versions: tuple[str, ...]
    cipher_count: int
    extension_count: int
    grease_present: bool
    node_version_bucket: str
    openssl_version_bucket: str
    agent_package_versions: dict[str, str]
    raw_clienthello_omitted_reason: str
    timestamp_utc: str

    def to_safe_dict(self) -> dict[str, Any]:
        payload = {
            "source": self.source,
            "version": self.version,
            "ja3_hash": self.ja3_hash,
            "ja4": self.ja4,
            "alpn_protocols": list(self.alpn_protocols),
            "tls_versions": list(self.tls_versions),
            "cipher_count": self.cipher_count,
            "extension_count": self.extension_count,
            "grease_present": self.grease_present,
            "node_version_bucket": self.node_version_bucket,
            "openssl_version_bucket": self.openssl_version_bucket,
            "agent_package_versions": dict(self.agent_package_versions),
            "raw_clienthello_omitted_reason": self.raw_clienthello_omitted_reason,
            "timestamp_utc": self.timestamp_utc,
        }
        validate_safe_tls_summary(payload)
        return payload


def _u8(data: bytes, pos: int) -> int:
    if pos >= len(data):
        raise ValueError("truncated ClientHello")
    return data[pos]


def _u16(data: bytes, pos: int) -> int:
    if pos + 2 > len(data):
        raise ValueError("truncated ClientHello")
    return int.from_bytes(data[pos : pos + 2], "big")


def _u24(data: bytes, pos: int) -> int:
    if pos + 3 > len(data):
        raise ValueError("truncated ClientHello")
    return int.from_bytes(data[pos : pos + 3], "big")


def _is_grease(value: int) -> bool:
    return value in GREASE_VALUES or (value & 0x0F0F == 0x0A0A and (value >> 8) == (value & 0xFF))


def _safe_hex_version(value: int) -> str:
    return TLS_VERSION_NAMES.get(value, f"0x{value:04x}")


def _parse_clienthello(data: bytes) -> dict[str, Any]:
    if len(data) < 9 or data[0] != 0x16:
        raise ValueError("not a TLS handshake record")
    record_len = _u16(data, 3)
    if len(data) < 5 + record_len:
        raise ValueError("truncated TLS record")
    handshake = data[5 : 5 + record_len]
    if _u8(handshake, 0) != 0x01:
        raise ValueError("first handshake is not ClientHello")
    hello_len = _u24(handshake, 1)
    hello = handshake[4 : 4 + hello_len]
    pos = 0
    legacy_version = _u16(hello, pos)
    pos += 2
    pos += 32  # random
    session_len = _u8(hello, pos)
    pos += 1 + session_len
    cipher_len = _u16(hello, pos)
    pos += 2
    cipher_suites = [_u16(hello, i) for i in range(pos, pos + cipher_len, 2)]
    pos += cipher_len
    comp_len = _u8(hello, pos)
    pos += 1 + comp_len
    extensions: list[int] = []
    groups: list[int] = []
    point_formats: list[int] = []
    supported_versions: list[int] = []
    alpn_protocols: list[str] = []
    if pos < len(hello):
        ext_total_len = _u16(hello, pos)
        pos += 2
        end = pos + ext_total_len
        while pos + 4 <= end and pos + 4 <= len(hello):
            ext_type = _u16(hello, pos)
            ext_len = _u16(hello, pos + 2)
            pos += 4
            ext_data = hello[pos : pos + ext_len]
            pos += ext_len
            extensions.append(ext_type)
            if ext_type == 10 and len(ext_data) >= 2:
                glen = _u16(ext_data, 0)
                groups = [_u16(ext_data, i) for i in range(2, min(2 + glen, len(ext_data)), 2)]
            elif ext_type == 11 and len(ext_data) >= 1:
                plen = ext_data[0]
                point_formats = [int(x) for x in ext_data[1 : 1 + plen]]
            elif ext_type == 43 and len(ext_data) >= 1:
                vlen = ext_data[0]
                supported_versions = [_u16(ext_data, i) for i in range(1, min(1 + vlen, len(ext_data)), 2)]
            elif ext_type == 16 and len(ext_data) >= 2:
                alen = _u16(ext_data, 0)
                p = 2
                while p < min(2 + alen, len(ext_data)):
                    n = ext_data[p]
                    p += 1
                    raw = ext_data[p : p + n]
                    p += n
                    try:
                        alpn_protocols.append(raw.decode("ascii"))
                    except UnicodeDecodeError:
                        alpn_protocols.append("non_ascii_alpn")
    return {
        "legacy_version": legacy_version,
        "cipher_suites": cipher_suites,
        "extensions": extensions,
        "groups": groups,
        "point_formats": point_formats,
        "supported_versions": supported_versions,
        "alpn_protocols": alpn_protocols,
    }


def _ja3_hash(parsed: dict[str, Any]) -> str:
    def filt(values: Iterable[int]) -> list[str]:
        return [str(v) for v in values if not _is_grease(int(v))]

    ja3 = ",".join(
        [
            str(parsed["legacy_version"]),
            "-".join(filt(parsed["cipher_suites"])),
            "-".join(filt(parsed["extensions"])),
            "-".join(filt(parsed["groups"])),
            "-".join(filt(parsed["point_formats"])),
        ]
    )
    return hashlib.md5(ja3.encode("ascii")).hexdigest()


def _safe_ja4(parsed: dict[str, Any]) -> str:
    versions = [v for v in parsed["supported_versions"] if not _is_grease(v)]
    tls_max = max(versions) if versions else parsed["legacy_version"]
    tls_tag = "t13" if tls_max >= 0x0304 else "t12"
    alpn = "h2" if "h2" in parsed["alpn_protocols"] else ("h1" if "http/1.1" in parsed["alpn_protocols"] else "00")
    cipher_ids = ",".join(str(v) for v in parsed["cipher_suites"] if not _is_grease(v))
    ext_ids = ",".join(str(v) for v in parsed["extensions"] if not _is_grease(v))
    cipher_hash = hashlib.sha256(cipher_ids.encode("ascii")).hexdigest()[:12]
    ext_hash = hashlib.sha256(ext_ids.encode("ascii")).hexdigest()[:12]
    return f"{tls_tag}d{len(parsed['cipher_suites']):04d}{alpn}_{cipher_hash}_{ext_hash}"


def summarize_clienthello_bytes(
    data: bytes,
    *,
    source: str,
    version: str,
    node_version_bucket: str = "not_applicable",
    openssl_version_bucket: str = "not_applicable",
    agent_package_versions: dict[str, str] | None = None,
) -> TLSSummary:
    parsed = _parse_clienthello(data)
    versions = parsed["supported_versions"] or [parsed["legacy_version"]]
    grease_present = any(_is_grease(v) for values in (parsed["cipher_suites"], parsed["extensions"], parsed["groups"], versions) for v in values)
    summary = TLSSummary(
        source=source,
        version=version,
        ja3_hash=_ja3_hash(parsed),
        ja4=_safe_ja4(parsed),
        alpn_protocols=tuple(parsed["alpn_protocols"]),
        tls_versions=tuple(_safe_hex_version(v) for v in versions if not _is_grease(v)),
        cipher_count=len(parsed["cipher_suites"]),
        extension_count=len(parsed["extensions"]),
        grease_present=grease_present,
        node_version_bucket=node_version_bucket,
        openssl_version_bucket=openssl_version_bucket,
        agent_package_versions=agent_package_versions or {},
        raw_clienthello_omitted_reason="raw_clienthello_forbidden",
        timestamp_utc=utc_now(),
    )
    validate_safe_tls_summary(summary.to_safe_dict())
    return summary


def validate_safe_tls_summary(payload: dict[str, Any]) -> None:
    if not isinstance(payload, dict):
        raise ValueError("TLS summary must be a dict")
    keys = set(payload)
    unsafe = keys & FORBIDDEN_TLS_KEYS
    if unsafe:
        raise ValueError(f"unsafe TLS summary keys: {sorted(unsafe)}")
    missing = REQUIRED_SAFE_SUMMARY_KEYS - keys
    extra = keys - REQUIRED_SAFE_SUMMARY_KEYS
    if missing or extra:
        raise ValueError(f"TLS summary schema mismatch missing={sorted(missing)} extra={sorted(extra)}")
    if payload["raw_clienthello_omitted_reason"] != "raw_clienthello_forbidden":
        raise ValueError("raw ClientHello omission reason is required")
    if payload["source"] not in SUMMARY_SOURCES:
        raise ValueError("unsafe TLS source bucket")
    if not isinstance(payload["ja3_hash"], str) or len(payload["ja3_hash"]) != 32:
        raise ValueError("ja3_hash must be an md5 hex digest")
    material = json.dumps(payload, sort_keys=True)
    forbidden_markers = (
        "-----BEGIN " + "PRIVATE KEY-----",
        "-----BEGIN " + "CERTIFICATE-----",
        "clienthello_raw",
        "pcap",
    )
    if any(marker.lower() in material.lower() for marker in forbidden_markers):
        raise ValueError("TLS summary contains forbidden material marker")


def write_tls_summary_file(path: Path, rows: list[dict[str, Any]]) -> None:
    for row in rows:
        validate_safe_tls_summary(row)
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_suffix(path.suffix + ".tmp")
    tmp.write_text(json.dumps(rows, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    tmp.replace(path)


def resolve_tls_scratch_root(*, evidence_root: Path, scratch_root: Path | None = None) -> Path:
    return app_oracle.resolve_cli_scratch_root(evidence_root=evidence_root, scratch_root=scratch_root)


def compare_tls_profiles(left: TLSSummary, right: TLSSummary, *, explanation: str = "") -> dict[str, Any]:
    differences: list[str] = []
    for field in ("ja3_hash", "ja4", "alpn_protocols", "tls_versions", "cipher_count", "extension_count", "grease_present"):
        if getattr(left, field) != getattr(right, field):
            differences.append(field)
    if not differences:
        status = "MATCH"
    elif explanation:
        status = "DIFFERENT_BUT_EXPLAINED"
    else:
        status = "DIFFERENT_UNEXPLAINED"
    return {
        "status": status,
        "left_source": left.source,
        "left_version": left.version,
        "right_source": right.source,
        "right_version": right.version,
        "difference_fields": differences,
        "explanation_bucket": "provided" if explanation else "not_provided",
        "timestamp_utc": utc_now(),
    }


def compare_tls_profile_sets(real_rows: list[dict[str, Any]], reference_rows: list[dict[str, Any]], *, guard_status: str) -> dict[str, Any]:
    if guard_status != "PASS" or not real_rows:
        return {
            "status": "BLOCKED_DYNAMIC_EGRESS_GUARD",
            "tls_profile_decision": "TLS_PROFILE_UNKNOWN_PLUMBING_ONLY",
            "summary": "Real TLS oracle blocked or unavailable; parity cannot be claimed.",
            "comparison_matrix": [],
            "timestamp_utc": utc_now(),
        }
    real_by_version = {row["version"]: row for row in real_rows if row.get("source") == "claude_code_cli"}
    ref_by_source = {row["source"]: row for row in reference_rows}
    compared = ["ja3_hash", "ja4", "alpn_protocols", "tls_versions", "cipher_count", "extension_count", "grease_present"]

    def pair_status(left: dict[str, Any] | None, right: dict[str, Any] | None) -> tuple[str, list[str]]:
        if left is None or right is None:
            return "MISSING", []
        diffs = [key for key in compared if left.get(key) != right.get(key)]
        return ("MATCH" if not diffs else "DIFFERENT_UNEXPLAINED", diffs)

    pair_specs = [
        ("real_2.1.179_vs_sub2api_builtin", real_by_version.get("2.1.179"), ref_by_source.get("sub2api_utls_builtin")),
        ("real_2.1.181_vs_real_2.1.179", real_by_version.get("2.1.181"), real_by_version.get("2.1.179")),
        ("real_2.1.195_vs_real_2.1.179", real_by_version.get("2.1.195"), real_by_version.get("2.1.179")),
        ("cc_gateway_node_agent_vs_real_2.1.179", ref_by_source.get("cc_gateway_node_agent"), real_by_version.get("2.1.179")),
    ]
    matrix = []
    for pair, left, right in pair_specs:
        status, diffs = pair_status(left, right)
        matrix.append({"pair": pair, "status": status, "difference_fields": diffs})

    if "2.1.179" not in real_by_version or "cc_gateway_node_agent" not in ref_by_source:
        return {
            "status": "DIFFERENT_UNEXPLAINED",
            "tls_profile_decision": "TLS_PROFILE_MISMATCH_REQUIRES_IMPLEMENTATION",
            "summary": "Required baseline rows missing; parity cannot be confirmed.",
            "comparison_matrix": matrix,
            "timestamp_utc": utc_now(),
        }
    baseline = real_by_version["2.1.179"]
    gateway = ref_by_source["cc_gateway_node_agent"]
    if all(baseline.get(k) == gateway.get(k) for k in compared):
        return {
            "status": "MATCH",
            "tls_profile_decision": "TLS_PROFILE_MATCH_CONFIRMED",
            "summary": "CC Gateway Node/agent summary matched the real 2.1.179 TLS oracle summary.",
            "comparison_matrix": matrix,
            "timestamp_utc": utc_now(),
        }
    return {
        "status": "DIFFERENT_UNEXPLAINED",
        "tls_profile_decision": "TLS_PROFILE_MISMATCH_REQUIRES_IMPLEMENTATION",
        "summary": "CC Gateway Node/agent differs from real Claude Code TLS oracle; do not claim transport parity.",
        "comparison_matrix": matrix,
        "timestamp_utc": utc_now(),
    }


class ClientHelloCollector:
    def __init__(self, *, source: str, version: str, node_version_bucket: str = "not_applicable", openssl_version_bucket: str = "not_applicable", agent_package_versions: dict[str, str] | None = None):
        self.source = source
        self.version = version
        self.node_version_bucket = node_version_bucket
        self.openssl_version_bucket = openssl_version_bucket
        self.agent_package_versions = agent_package_versions or {}
        self.summaries: list[TLSSummary] = []
        parent = self

        class Handler(socketserver.BaseRequestHandler):
            def handle(self) -> None:
                try:
                    data = self.request.recv(8192)
                    if data:
                        parent.summaries.append(
                            summarize_clienthello_bytes(
                                data,
                                source=parent.source,
                                version=parent.version,
                                node_version_bucket=parent.node_version_bucket,
                                openssl_version_bucket=parent.openssl_version_bucket,
                                agent_package_versions=parent.agent_package_versions,
                            )
                        )
                except Exception:
                    return
                finally:
                    try:
                        self.request.close()
                    except Exception:
                        pass

        class Server(socketserver.ThreadingTCPServer):
            allow_reuse_address = True
            daemon_threads = True

        self._server = Server(("127.0.0.1", 0), Handler)
        self._thread: threading.Thread | None = None

    @property
    def port(self) -> int:
        return int(self._server.server_address[1])

    def __enter__(self) -> "ClientHelloCollector":
        self._thread = threading.Thread(target=self._server.serve_forever, daemon=True)
        self._thread.start()
        return self

    def __exit__(self, exc_type: Any, exc: Any, tb: Any) -> None:
        self._server.shutdown()
        self._server.server_close()


def _node_bucket(node_path: str = "node") -> tuple[str, str]:
    if not shutil.which(node_path):
        return "node-unavailable", "openssl-unavailable"
    script = "console.log(process.versions.node); console.log(process.versions.openssl || 'unknown')"
    result = subprocess.run([node_path, "-e", script], text=True, capture_output=True, timeout=10, check=False)
    lines = [line.strip() for line in result.stdout.splitlines() if line.strip()]
    node = lines[0] if lines else "unknown"
    openssl = lines[1] if len(lines) > 1 else "unknown"
    node_major = node.split(".")[0] if node and node != "unknown" else "unknown"
    openssl_major = openssl.split(".")[0] if openssl and openssl != "unknown" else "unknown"
    return f"node-{node_major}.x", f"openssl-{openssl_major}.x"


def _cc_gateway_agent_versions(cc_gateway_root: Path) -> dict[str, str]:
    package_json = cc_gateway_root / "package.json"
    if not package_json.exists():
        return {"https-proxy-agent": "unknown", "socks-proxy-agent": "unknown"}
    data = json.loads(package_json.read_text(encoding="utf-8"))
    deps = {**data.get("dependencies", {}), **data.get("devDependencies", {})}
    return {
        "https-proxy-agent": str(deps.get("https-proxy-agent", "not_declared")),
        "socks-proxy-agent": str(deps.get("socks-proxy-agent", "not_declared")),
    }


class LocalConnectProxy:
    def __init__(self, target_port: int):
        self.target_port = target_port
        parent = self

        class Handler(socketserver.StreamRequestHandler):
            def handle(self) -> None:
                first = self.rfile.readline(4096)
                if not first.startswith(b"CONNECT "):
                    return
                while True:
                    line = self.rfile.readline(4096)
                    if line in {b"\r\n", b"\n", b""}:
                        break
                self.wfile.write(b"HTTP/1.1 200 Connection Established\r\n\r\n")
                self.wfile.flush()
                upstream = socket.create_connection(("127.0.0.1", parent.target_port), timeout=2.0)
                try:
                    data = self.connection.recv(8192)
                    if data:
                        upstream.sendall(data)
                finally:
                    upstream.close()

        class Server(socketserver.ThreadingTCPServer):
            allow_reuse_address = True
            daemon_threads = True

        self._server = Server(("127.0.0.1", 0), Handler)
        self._thread: threading.Thread | None = None

    @property
    def url(self) -> str:
        return f"http://127.0.0.1:{int(self._server.server_address[1])}"

    def __enter__(self) -> "LocalConnectProxy":
        self._thread = threading.Thread(target=self._server.serve_forever, daemon=True)
        self._thread.start()
        return self

    def __exit__(self, exc_type: Any, exc: Any, tb: Any) -> None:
        self._server.shutdown()
        self._server.server_close()


def default_connect_proxy_factory(target_port: int) -> tuple[str, LocalConnectProxy]:
    proxy = LocalConnectProxy(target_port)
    proxy.__enter__()
    return proxy.url, proxy


def capture_cc_gateway_node_agent(
    cc_gateway_root: Path,
    timeout_seconds: float = 8.0,
    *,
    runner: Any = subprocess.run,
    collector_factory: Any = ClientHelloCollector,
    proxy_factory: Any = default_connect_proxy_factory,
) -> TLSSummary | None:
    node_bucket, openssl_bucket = _node_bucket()
    agent_versions = _cc_gateway_agent_versions(cc_gateway_root)
    with collector_factory(source="cc_gateway_node_agent", version="node-agent-current", node_version_bucket=node_bucket, openssl_version_bucket=openssl_bucket, agent_package_versions=agent_versions) as collector:
        proxy_url, proxy_ctx = proxy_factory(collector.port)
        script = """
import https from 'node:https';
const proxyUrl = process.argv[1];
import { getProxyAgent } from './dist/proxy-agent.js';
const agent = getProxyAgent('tls-oracle-local', proxyUrl);
const req = https.request({hostname:'127.0.0.1', port:443, path:'/v1/messages', method:'POST', rejectUnauthorized:false, ALPNProtocols:['h2','http/1.1'], servername:'localhost', agent}, res => { res.resume(); });
req.on('error', () => {});
req.end();
setTimeout(() => process.exit(0), 1200);
"""
        try:
            runner(["node", "--input-type=module", "-e", script, proxy_url], cwd=str(cc_gateway_root), text=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=timeout_seconds, check=False)
        finally:
            if hasattr(proxy_ctx, "__exit__"):
                proxy_ctx.__exit__(None, None, None)
        return collector.summaries[0] if collector.summaries else None


def capture_sub2api_utls_builtin_local(
    backend_root: Path,
    *,
    runner: Any = subprocess.run,
    collector_factory: Any = ClientHelloCollector,
    timeout_seconds: float = 20.0,
) -> TLSSummary | None:
    """Capture Sub2API built-in uTLS profile against a local collector.

    This reuses the package's integration capture test with a loopback capture
    URL. The test is allowed to fail after the ClientHello because this
    collector intentionally does not decrypt or return fingerprint JSON.
    """
    with collector_factory(
        source="sub2api_utls_builtin",
        version="sub2api-built-in-node24",
        node_version_bucket="node-24.x-template",
        openssl_version_bucket="not_applicable",
        agent_package_versions={},
    ) as collector:
        env = {k: v for k, v in os.environ.items() if k in {"PATH", "HOME", "TMPDIR", "LANG", "LC_ALL", "LC_CTYPE"}}
        env["TLSFINGERPRINT_CAPTURE_URL"] = f"https://127.0.0.1:{collector.port}"
        cmd = [
            "go",
            "test",
            "-tags=integration",
            "./internal/pkg/tlsfingerprint",
            "-run",
            "TestDialerAgainstCaptureServer/default_profile",
            "-count=1",
        ]
        try:
            runner(
                cmd,
                cwd=str(backend_root),
                env=env,
                text=True,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
                timeout=timeout_seconds,
                check=False,
            )
        except Exception:
            pass
        return collector.summaries[0] if collector.summaries else None


def capture_real_cli_tls(version: str, runtime_root: Path, evidence_temp_root: Path, timeout_seconds: float = 12.0) -> TLSSummary | None:
    # Re-prove inside the same sandbox-exec profile family before launching the CLI.
    guard = app_oracle.evaluate_egress_guard(same_scope_self_tests=app_oracle.run_sandbox_exec_same_scope_self_tests())
    if guard.get("status") != "PASS":
        return None
    run_root = Path(tempfile.mkdtemp(prefix=f"cc-tls-{version}-", dir=str(evidence_temp_root)))
    binary = app_oracle._extract_platform_binary(runtime_root, version, run_root)  # noqa: SLF001 - local harness reuse
    with ClientHelloCollector(source="claude_code_cli", version=version, node_version_bucket="claude-code-bundled", openssl_version_bucket="claude-code-bundled", agent_package_versions={}) as collector:
        env = app_oracle.build_isolated_cli_env(
            temp_root=run_root / "isolated-env",
            collector_base_url=f"https://127.0.0.1:{collector.port}",
            include_dummy_api_key=True,
        )
        env.update({
            "CLAUDE_CODE_API_BASE_URL": f"https://127.0.0.1:{collector.port}",
            "CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL": "0",
            "ANTHROPIC_MODEL": "claude-sonnet-4-5",
        })
        cmd = [
            "sandbox-exec",
            "-p",
            app_oracle.sandbox_exec_loopback_profile(),
            str(binary),
            "--bare",
            "--print",
            "--output-format",
            "json",
            "--model",
            "claude-sonnet-4-5",
            "--max-turns",
            "1",
            "Reply with exactly: local tls oracle ok",
        ]
        try:
            subprocess.run(cmd, cwd=str(run_root), env=env, stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=timeout_seconds, check=False)
        except subprocess.TimeoutExpired:
            pass
        return collector.summaries[0] if collector.summaries else None


def sub2api_builtin_static_summary() -> TLSSummary:
    # Safe summary derived from backend/internal/pkg/tlsfingerprint/dialer.go comments/defaults.
    ja3_hash = "44f88fca027f27bab4bb08d4af15f23e"
    ja4 = "t13d1714h1_5b57614c22b0_7baf387fc6ff"
    return TLSSummary(
        source="sub2api_utls_builtin",
        version="sub2api-built-in-node24",
        ja3_hash=ja3_hash,
        ja4=ja4,
        alpn_protocols=("http/1.1",),
        tls_versions=("0x0304", "0x0303"),
        cipher_count=17,
        extension_count=14,
        grease_present=False,
        node_version_bucket="node-24.x-template",
        openssl_version_bucket="not_applicable",
        agent_package_versions={},
        raw_clienthello_omitted_reason="raw_clienthello_forbidden",
        timestamp_utc=utc_now(),
    )


def capture_tls(args: argparse.Namespace) -> int:
    evidence_root = Path(args.evidence_root)
    evidence_root.mkdir(parents=True, exist_ok=True)
    scratch_root = resolve_tls_scratch_root(evidence_root=evidence_root, scratch_root=Path(args.scratch_root) if args.scratch_root else None)
    guard_path = evidence_root / "egress-guard-summary.json"
    guard = json.loads(guard_path.read_text(encoding="utf-8")) if guard_path.exists() else {}
    guard_status = str(guard.get("status", "BLOCKED_DYNAMIC_EGRESS_GUARD"))
    rows: list[dict[str, Any]] = []
    blocked_rows: list[dict[str, Any]] = []

    for version in args.runtime_version or ALLOWED_RUNTIME_VERSIONS:
        if guard_status == "PASS":
            summary = capture_real_cli_tls(version, Path(args.runtime_root), scratch_root)
            if summary is not None:
                rows.append(summary.to_safe_dict())
            else:
                blocked_rows.append({"source": "claude_code_cli", "version": version, "status": "BLOCKED_DYNAMIC_EGRESS_GUARD"})
        else:
            blocked_rows.append({"source": "claude_code_cli", "version": version, "status": "BLOCKED_DYNAMIC_EGRESS_GUARD"})

    sub2api_summary = capture_sub2api_utls_builtin_local(Path(args.sub2api_backend_root))
    if sub2api_summary is None:
        sub2api_summary = sub2api_builtin_static_summary()
    rows.append(sub2api_summary.to_safe_dict())
    gateway_summary = capture_cc_gateway_node_agent(Path(args.cc_gateway_root))
    if gateway_summary is not None:
        rows.append(gateway_summary.to_safe_dict())

    write_tls_summary_file(evidence_root / "tls-oracle-summary.json", rows)
    decision = compare_tls_profile_sets(
        [row for row in rows if row.get("source") == "claude_code_cli"],
        [row for row in rows if row.get("source") != "claude_code_cli"],
        guard_status=guard_status,
    )
    decision["blocked_rows"] = blocked_rows
    decision["observed_summary_count"] = len(rows)
    (evidence_root / "tls-profile-comparison-summary.json").write_text(json.dumps(decision, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps({"status": decision["status"], "observed_summary_count": len(rows), "blocked_count": len(blocked_rows)}, sort_keys=True))
    return 0 if rows else 2


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Claude Code TLS ClientHello safe oracle")
    sub = parser.add_subparsers(dest="command", required=True)
    cap = sub.add_parser("capture-tls-oracle")
    cap.add_argument("--evidence-root", required=True)
    cap.add_argument("--runtime-version", choices=ALLOWED_RUNTIME_VERSIONS, action="append")
    cap.add_argument("--runtime-root", required=True)
    cap.add_argument("--cc-gateway-root", required=True)
    cap.add_argument("--sub2api-backend-root", default="backend")
    cap.add_argument("--scratch-root")
    cap.set_defaults(func=capture_tls)
    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    return int(args.func(args))


if __name__ == "__main__":
    raise SystemExit(main())
