from __future__ import annotations

import hashlib
import importlib.util
import json
import socket
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

import pytest

from zhumeng_agent.adapters.claude_code.guard import (
    NativeGuardConfig,
    NativeGuardMode,
    build_native_guard_plan,
    start_native_guard,
)


REPO_ROOT = Path(__file__).resolve().parents[3]
_ROUTE_TRUST_SPEC = importlib.util.spec_from_file_location("claude_code_route_trust", REPO_ROOT / "tools" / "claude_code_route_trust.py")
assert _ROUTE_TRUST_SPEC is not None and _ROUTE_TRUST_SPEC.loader is not None
_ROUTE_TRUST = importlib.util.module_from_spec(_ROUTE_TRUST_SPEC)
sys.modules[_ROUTE_TRUST_SPEC.name] = _ROUTE_TRUST
_ROUTE_TRUST_SPEC.loader.exec_module(_ROUTE_TRUST)
build_signed_route_hint_headers = _ROUTE_TRUST.build_signed_route_hint_headers
cp4_fixture_route_catalog = _ROUTE_TRUST.cp4_fixture_route_catalog
route_catalog_content_hash = _ROUTE_TRUST.route_catalog_content_hash


def _cp0_route_hint_headers(*, body: bytes, request_path: str, session_ref: str, secret: str = "route-hint-secret", nonce: str = "route-hint-nonce") -> dict[str, str]:
    catalog = cp4_fixture_route_catalog(
        runtime_hash="sha256:" + hashlib.sha256((REPO_ROOT / "tools" / "cli_control_plane_guard.py").read_bytes()).hexdigest(),
        overlay_hash="sha256:" + hashlib.sha256(b"zhumeng-claude-runtime-overlay:cp0-native-only").hexdigest(),
        catalog_hash="sha256:" + ("0" * 64),
        catalog_version="cp4-cli-fixture-v1",
    )
    catalog = cp4_fixture_route_catalog(
        runtime_hash=catalog.runtime_hash,
        overlay_hash=catalog.overlay_hash,
        catalog_hash=route_catalog_content_hash(catalog),
        catalog_version=catalog.catalog_version,
    )
    return build_signed_route_hint_headers(
        body=body,
        request_path=request_path,
        catalog=catalog,
        model_id="claude-sonnet-4-6",
        session_ref=session_ref,
        secret=secret,
        nonce=nonce,
    )


class CaptureHandler(BaseHTTPRequestHandler):
    requests: list[dict[str, object]] = []

    def log_message(self, *args):
        pass

    def do_POST(self):
        length = int(self.headers.get("content-length", "0") or 0)
        body = self.rfile.read(length) if length else b""
        type(self).requests.append(
            {
                "path": self.path,
                "headers": {key.lower(): value for key, value in self.headers.items()},
                "body": body,
            }
        )
        data = b'{"ok":true}'
        self.send_response(200)
        self.send_header("content-type", "application/json")
        self.send_header("content-length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)


class IntentHandler(BaseHTTPRequestHandler):
    requests: list[dict[str, object]] = []

    def log_message(self, *args):
        pass

    def do_POST(self):
        length = int(self.headers.get("content-length", "0") or 0)
        payload = json.loads(self.rfile.read(length).decode("utf-8") or "{}")
        type(self).requests.append(
            {
                "headers": {key.lower(): value for key, value in self.headers.items()},
                "payload": payload,
            }
        )
        data = json.dumps(
            {
                "decision": "stub_json",
                "reason": "agent_native_guard_test",
                "status": 200,
                "content_type": "application/json",
                "body": {"from": "intent"},
            }
        ).encode("utf-8")
        self.send_response(200)
        self.send_header("content-type", "application/json")
        self.send_header("content-length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)


def test_native_guard_plan_scrubs_proxy_recursion_and_keeps_secrets_out_of_command(tmp_path: Path):
    cfg = NativeGuardConfig(
        mode=NativeGuardMode.PRODUCTION,
        listen_port=43117,
        upstream_base="http://127.0.0.1:18080",
        sub2api_auth="sub2api-entry-secret",
        summary_path=tmp_path / "guard-summary.jsonl",
        repo_root=REPO_ROOT,
        control_plane_intent_url="http://127.0.0.1:18081/backend-api/anthropic/control-plane/intent",
        control_plane_intent_auth="intent-secret",
        attestation_secret="attestation-secret",
        hmac_key="hmac-secret",
    )

    plan = build_native_guard_plan(
        cfg,
        inherited_env={
            "PATH": "/usr/bin",
            "HTTP_PROXY": "http://proxy.example:8080",
            "HTTPS_PROXY": "http://proxy.example:8443",
            "ALL_PROXY": "socks5://proxy.example:1080",
            "ANTHROPIC_API_KEY": "local-anthropic-key",
            "COOKIE": "cookie-secret",
        },
        python_executable=Path("/opt/homebrew/bin/python3"),
    )

    command_text = " ".join(plan.command)
    assert "sub2api-entry-secret" not in command_text
    assert "intent-secret" not in command_text
    assert "attestation-secret" not in command_text
    assert "--native-attestation" in plan.command
    assert "--route-hint-secret-env" not in plan.command
    assert "--allow-nonloopback-upstream" not in plan.command
    assert "--control-plane-intent-auth" not in plan.command
    assert plan.env["ZHUMENG_CLAUDE_NATIVE_SUB2API_AUTH"] == "sub2api-entry-secret"
    assert plan.env["SUB2API_CONTROL_PLANE_INTENT_TOKEN"] == "intent-secret"
    assert plan.env["SUB2API_CONTROL_PLANE_ATTESTATION_SECRET"] == "attestation-secret"
    assert plan.env["SUB2API_CONTROL_PLANE_HMAC_KEY"] == "hmac-secret"
    expected_runtime_hash = "sha256:" + hashlib.sha256((REPO_ROOT / "tools" / "cli_control_plane_guard.py").read_bytes()).hexdigest()
    assert plan.env["ZHUMENG_CLAUDE_RUNTIME_HASH"] == expected_runtime_hash
    assert plan.env["ZHUMENG_CLAUDE_OVERLAY_HASH"].startswith("sha256:")
    assert plan.env["ZHUMENG_CLAUDE_CATALOG_HASH"].startswith("sha256:")
    assert plan.env["ZHUMENG_CLAUDE_OVERLAY_HASH"] != "sha256:" + ("0" * 64)
    assert plan.env["ZHUMENG_CLAUDE_CATALOG_HASH"] != "sha256:" + ("0" * 64)
    assert "HTTP_PROXY" not in plan.env
    assert "HTTPS_PROXY" not in plan.env
    assert "ALL_PROXY" not in plan.env
    assert plan.env["NO_PROXY"] == "127.0.0.1,localhost,::1"
    assert str(REPO_ROOT) in plan.env["PYTHONPATH"].split(":")
    assert plan.cwd == REPO_ROOT
    assert plan.will_start_process is False
    assert "sub2api-entry-secret" not in repr(plan)


def test_cp4_native_guard_plan_can_enable_route_hint_without_leaking_secret(tmp_path: Path):
    cfg = NativeGuardConfig(
        mode=NativeGuardMode.PRODUCTION,
        listen_port=43117,
        upstream_base="http://127.0.0.1:18080",
        sub2api_auth="sub2api-entry-secret",
        summary_path=tmp_path / "guard-summary.jsonl",
        repo_root=REPO_ROOT,
        attestation_secret="attestation-secret",
        route_hint_secret="cp4-route-key",
        route_hint_catalog_version="cp4-test-v1",
    )

    plan = build_native_guard_plan(cfg, python_executable=Path("/opt/homebrew/bin/python3"))

    command_text = " ".join(plan.command)
    assert "--native-attestation" in plan.command
    assert "--route-hint-secret-env" in plan.command
    assert "ZHUMENG_CLAUDE_ROUTE_HINT_SECRET" in plan.command
    assert "--route-hint-catalog-version" in plan.command
    assert "cp4-test-v1" in plan.command
    assert "cp4-route-key" not in command_text
    assert plan.env["ZHUMENG_CLAUDE_ROUTE_HINT_SECRET"] == "cp4-route-key"
    assert "cp4-route-key" not in repr(plan)


def test_native_guard_plan_requires_explicit_attestation_secret(tmp_path: Path):
    cfg = NativeGuardConfig(
        mode=NativeGuardMode.STAGING,
        listen_port=43117,
        upstream_base="http://127.0.0.1:18080",
        sub2api_auth="sub2api-entry",
        summary_path=tmp_path / "guard-summary.jsonl",
        repo_root=REPO_ROOT,
    )

    with pytest.raises(ValueError, match="attestation_secret"):
        build_native_guard_plan(cfg, python_executable=Path(sys.executable))


@pytest.mark.parametrize(
    "url",
    [
        "https://api.anthropic.com",
        "https://platform.claude.com",
        "https://claude.ai",
        "http://192.168.1.10:18080",
        "http://user" + ":pass@127.0.0.1:18080",
    ],
)
def test_native_guard_config_rejects_official_or_non_loopback_urls(tmp_path: Path, url: str):
    with pytest.raises(ValueError):
        NativeGuardConfig(
            mode=NativeGuardMode.STAGING,
            listen_port=43117,
            upstream_base=url,
            sub2api_auth="sub2api-entry",
            summary_path=tmp_path / "guard-summary.jsonl",
            repo_root=REPO_ROOT,
        )


def test_native_guard_forwards_messages_with_replacement_auth_and_redacted_summary(tmp_path: Path):
    CaptureHandler.requests = []
    upstream = _start_server(CaptureHandler)
    listen_port = _free_port()
    summary_path = tmp_path / "guard-summary.jsonl"
    cfg = NativeGuardConfig(
        mode=NativeGuardMode.LAB,
        listen_port=listen_port,
        upstream_base=f"http://127.0.0.1:{upstream.server_port}",
        sub2api_auth="sub2api-entry",
        summary_path=summary_path,
        repo_root=REPO_ROOT,
        attestation_secret="attestation-secret",
        route_hint_secret="route-hint-secret",
    )

    with start_native_guard(build_native_guard_plan(cfg, python_executable=Path(sys.executable))) as guard:
        assert guard.ready["listen"] == f"http://127.0.0.1:{listen_port}"
        payload = {
            "model": "claude-sonnet-4-6",
            "messages": [{"role": "user", "content": "raw-prompt-marker"}],
            "max_tokens": 32,
        }
        body = json.dumps(payload).encode("utf-8")
        request_path = "/v1/messages?beta=true"
        session_ref = "11111111-2222-4333-8444-555555555555"
        req = urllib.request.Request(
            f"http://127.0.0.1:{listen_port}{request_path}",
            data=body,
            method="POST",
            headers={
                "content-type": "application/json",
                "Authorization": "Bearer local-token-marker",
                "x-api-key": "local-api-key-marker",
                "Cookie": "session=cookie-marker",
                "Proxy-Authorization": "Basic proxy-credential-marker",
                "x-claude-code-session-id": session_ref,
                **_cp0_route_hint_headers(body=body, request_path=request_path, session_ref=session_ref, nonce="replacement-auth-nonce"),
            },
        )
        with urllib.request.urlopen(req, timeout=5) as response:
            assert response.status == 200

    upstream.shutdown()
    assert len(CaptureHandler.requests) == 1
    forwarded = CaptureHandler.requests[0]
    headers = forwarded["headers"]
    assert headers["authorization"] == "Bearer sub2api-entry"
    assert "x-api-key" not in headers
    assert "cookie" not in headers
    assert "proxy-authorization" not in headers
    summary = summary_path.read_text(encoding="utf-8")
    assert "auth_shape" in summary
    assert "raw-prompt-marker" not in summary
    assert "local-token-marker" not in summary
    assert "local-api-key-marker" not in summary
    assert "proxy-credential-marker" not in summary


def test_native_guard_forwards_attested_native_markers_without_prompt_leak(tmp_path: Path):
    CaptureHandler.requests = []
    upstream = _start_server(CaptureHandler)
    listen_port = _free_port()
    summary_path = tmp_path / "guard-summary.jsonl"
    cfg = NativeGuardConfig(
        mode=NativeGuardMode.STAGING,
        listen_port=listen_port,
        upstream_base=f"http://127.0.0.1:{upstream.server_port}",
        sub2api_auth="sub2api-entry",
        summary_path=summary_path,
        repo_root=REPO_ROOT,
        attestation_secret="attestation-secret",
        route_hint_secret="route-hint-secret",
    )

    with start_native_guard(build_native_guard_plan(cfg, python_executable=Path(sys.executable))):
        payload = {
            "model": "claude-sonnet-4-6",
            "messages": [{"role": "user", "content": "native-prompt-marker"}],
            "max_tokens": 32,
        }
        body = json.dumps(payload).encode("utf-8")
        request_path = "/v1/messages?beta=true"
        session_ref = "11111111-2222-4333-8444-555555555555"
        req = urllib.request.Request(
            f"http://127.0.0.1:{listen_port}{request_path}",
            data=body,
            method="POST",
            headers={
                "content-type": "application/json",
                "x-claude-code-session-id": session_ref,
                **_cp0_route_hint_headers(body=body, request_path=request_path, session_ref=session_ref, nonce="attested-native-nonce"),
            },
        )
        with urllib.request.urlopen(req, timeout=5) as response:
            assert response.status == 200

    upstream.shutdown()
    assert len(CaptureHandler.requests) == 1
    headers = CaptureHandler.requests[0]["headers"]
    assert headers["x-sub2api-client-type"] == "claude_code_native"
    assert headers["x-sub2api-guard-attested"] == "true"
    assert headers["x-sub2api-netwatch-required"] == "true"
    assert headers["x-sub2api-native-attestation"]
    assert headers["x-sub2api-native-signature"]
    attestation_payload = json.loads(_b64url_decode(headers["x-sub2api-native-attestation"]).decode("utf-8"))
    assert attestation_payload["client_type"] == "claude_code_native"
    assert attestation_payload["route"] == "claude_code_native"
    assert attestation_payload["model_id"] == "claude-sonnet-4-6"
    assert attestation_payload["provider_owner"] == "zhumeng_managed"
    assert attestation_payload["credential_scope"] == "formal_pool"
    assert attestation_payload["gateway_location"] == "cloud"
    assert attestation_payload["runtime_hash"]
    assert attestation_payload["overlay_hash"]
    assert attestation_payload["catalog_hash"]
    assert attestation_payload["session_ref"] == attestation_payload["local_session_ref"]
    assert attestation_payload["body_shape_hash"].startswith("sha256:")
    assert attestation_payload["nonce"]
    assert attestation_payload["issued_at"] > 0
    summary = summary_path.read_text(encoding="utf-8")
    assert "claude_code_native" in summary
    assert "native-prompt-marker" not in summary


def test_native_guard_without_native_attestation_flag_fails_closed(tmp_path: Path):
    CaptureHandler.requests = []
    upstream = _start_server(CaptureHandler)
    listen_port = _free_port()
    summary_path = tmp_path / "guard-summary.jsonl"
    cfg = NativeGuardConfig(
        mode=NativeGuardMode.STAGING,
        listen_port=listen_port,
        upstream_base=f"http://127.0.0.1:{upstream.server_port}",
        sub2api_auth="sub2api-entry",
        summary_path=summary_path,
        repo_root=REPO_ROOT,
        attestation_secret="attestation-secret",
        route_hint_secret="route-hint-secret",
    )
    plan = build_native_guard_plan(cfg, python_executable=Path(sys.executable))
    no_attestation_plan = plan.__class__(
        command=[part for part in plan.command if part != "--native-attestation"],
        env=plan.env,
        cwd=plan.cwd,
        config=plan.config,
        will_start_process=plan.will_start_process,
    )

    with start_native_guard(no_attestation_plan):
        payload = {
            "model": "claude-sonnet-4-6",
            "messages": [{"role": "user", "content": "native-prompt-marker"}],
            "max_tokens": 32,
        }
        body = json.dumps(payload).encode("utf-8")
        request_path = "/v1/messages?beta=true"
        session_ref = "11111111-2222-4333-8444-555555555555"
        req = urllib.request.Request(
            f"http://127.0.0.1:{listen_port}{request_path}",
            data=body,
            method="POST",
            headers={
                "content-type": "application/json",
                "x-claude-code-session-id": session_ref,
                **_cp0_route_hint_headers(body=body, request_path=request_path, session_ref=session_ref, nonce="no-attestation-nonce"),
            },
        )
        with pytest.raises(urllib.error.HTTPError) as exc_info:
            urllib.request.urlopen(req, timeout=5)

    upstream.shutdown()
    assert exc_info.value.code == 403
    assert CaptureHandler.requests == []
    summary = summary_path.read_text(encoding="utf-8")
    assert "native_attestation_unavailable" in summary
    assert "attestation-secret" not in summary


def test_native_guard_control_plane_intent_attestation_and_connect_block(tmp_path: Path):
    IntentHandler.requests = []
    intent_server = _start_server(IntentHandler)
    listen_port = _free_port()
    summary_path = tmp_path / "guard-summary.jsonl"
    cfg = NativeGuardConfig(
        mode=NativeGuardMode.STAGING,
        listen_port=listen_port,
        upstream_base="http://127.0.0.1:9",
        sub2api_auth="sub2api-entry",
        summary_path=summary_path,
        repo_root=REPO_ROOT,
        control_plane_intent_url=f"http://127.0.0.1:{intent_server.server_port}/intent",
        control_plane_intent_auth="intent-secret",
        attestation_secret="attestation-secret",
    )

    with start_native_guard(build_native_guard_plan(cfg, python_executable=Path(sys.executable))):
        with urllib.request.urlopen(
            f"http://127.0.0.1:{listen_port}/api/claude_cli/bootstrap?entrypoint=sdk-cli",
            timeout=5,
        ) as response:
            assert response.status == 200
            assert json.loads(response.read().decode("utf-8")) == {"from": "intent"}

        with socket.create_connection(("127.0.0.1", listen_port), timeout=5) as sock:
            sock.sendall(b"CONNECT api.anthropic.com:443 HTTP/1.1\r\nHost: api.anthropic.com:443\r\n\r\n")
            response = sock.recv(4096)
        assert b"403" in response

    intent_server.shutdown()
    assert len(IntentHandler.requests) == 1
    headers = IntentHandler.requests[0]["headers"]
    payload = IntentHandler.requests[0]["payload"]
    assert "x-sub2api-control-plane-attestation" in headers
    assert "x-sub2api-control-plane-signature" in headers
    assert headers["x-sub2api-intent-auth"] == "intent-secret"
    assert payload["path_template"] == "/api/claude_cli/bootstrap"
    summary = summary_path.read_text(encoding="utf-8")
    assert "connect_blocked" in summary
    assert "api.anthropic.com" in summary
    assert "intent-secret" not in summary
    assert "attestation-secret" not in summary


def _start_server(handler: type[BaseHTTPRequestHandler]) -> ThreadingHTTPServer:
    server = ThreadingHTTPServer(("127.0.0.1", 0), handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return server


def _free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])



def _b64url_decode(value: str) -> bytes:
    padded = value + ("=" * ((4 - len(value) % 4) % 4))
    import base64

    return base64.urlsafe_b64decode(padded.encode("ascii"))
