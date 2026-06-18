from __future__ import annotations

from contextlib import contextmanager
import importlib.util
import json
import shutil
import socket
import subprocess
import sys
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from types import SimpleNamespace

import pytest

import zhumeng_agent.adapters.claude_code.launcher as launcher
from zhumeng_agent.adapters.claude_code.launcher import (
    build_claude_code_launch_plan,
    detect_claude_code_version,
    run_managed_claude_code,
)
from zhumeng_agent.adapters.claude_code.profile import CaptureMode, ClaudeCodeProfile

REPO_ROOT = Path(__file__).resolve().parents[3]
_ROUTE_TRUST_SPEC = importlib.util.spec_from_file_location("claude_code_route_trust", REPO_ROOT / "tools" / "claude_code_route_trust.py")
assert _ROUTE_TRUST_SPEC is not None and _ROUTE_TRUST_SPEC.loader is not None
_ROUTE_TRUST = importlib.util.module_from_spec(_ROUTE_TRUST_SPEC)
sys.modules[_ROUTE_TRUST_SPEC.name] = _ROUTE_TRUST
_ROUTE_TRUST_SPEC.loader.exec_module(_ROUTE_TRUST)
cp4_fixture_route_catalog = _ROUTE_TRUST.cp4_fixture_route_catalog
route_catalog_content_hash = _ROUTE_TRUST.route_catalog_content_hash


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



def test_managed_launch_starts_native_guard_then_launches_claude_with_ready_base_url(tmp_path: Path):
    events: list[tuple[str, object]] = []
    executable = tmp_path / "claude"
    project_cwd = tmp_path / "workspace"
    project_cwd.mkdir()

    @contextmanager
    def fake_start_guard(plan, *, ready_timeout_seconds: float = 10.0):
        events.append(("guard", plan))
        assert "--native-attestation" in plan.command
        assert "--route-hint-secret-env" in plan.command
        connect_mode_index = plan.command.index("--connect-mode") + 1
        assert plan.command[connect_mode_index] == "stub"
        assert plan.config.connect_mode == "stub"
        assert "--cert-path" in plan.command
        assert "--key-path" in plan.command
        assert plan.config.cert_path is not None
        assert plan.config.key_path is not None
        assert plan.config.cert_path.exists()
        assert plan.env["ZHUMENG_CLAUDE_NATIVE_SUB2API_AUTH"] == "sub2api-entry"
        assert plan.env["ZHUMENG_CLAUDE_ROUTE_HINT_SECRET"] == "route-hint-secret"
        assert plan.env["ZHUMENG_CLAUDE_RUNTIME_HASH"] == "sha256:" + "1" * 64
        assert plan.env["ZHUMENG_CLAUDE_OVERLAY_HASH"] == "sha256:" + "2" * 64
        yield SimpleNamespace(process=SimpleNamespace(pid=12345), ready={"listen": "http://127.0.0.1:43117"})

    def fake_process_runner(command, *, env, cwd):
        events.append(("claude", {"command": command, "env": env, "cwd": cwd}))
        return 17

    result = launcher.run_managed_claude_code(
        executable=executable,
        repo_root=REPO_ROOT,
        upstream_base="http://127.0.0.1:18080",
        sub2api_auth="sub2api-entry",
        attestation_secret="attestation-secret",
        route_hint_secret="route-hint-secret",
        route_hint_catalog_version="cp4-test-v1",
        runtime_hash="sha256:" + "1" * 64,
        overlay_hash="sha256:" + "2" * 64,
        config_root=tmp_path / "zhumeng-state",
        project_cwd=project_cwd,
        guard_listen_port=43117,
        argv=["--print"],
        inherited_env={"ANTHROPIC_API_KEY": "local-user-key", "PATH": "/usr/bin"},
        start_guard=fake_start_guard,
        process_runner=fake_process_runner,
    )

    assert result.returncode == 17
    assert result.guard_ready["listen"] == "http://127.0.0.1:43117"
    assert events[0][0] == "guard"
    assert events[1][0] == "claude"
    launch = events[1][1]
    assert launch["command"] == [str(executable), "--print"]
    assert launch["cwd"] == str(project_cwd)
    assert launch["env"]["ANTHROPIC_BASE_URL"] == "http://127.0.0.1:43117"
    assert launch["env"]["CLAUDE_CODE_API_BASE_URL"] == "http://127.0.0.1:43117"
    assert launch["env"]["ANTHROPIC_API_KEY"] == "sub2api-entry"
    assert launch["env"]["HTTPS_PROXY"] == "http://127.0.0.1:43117"
    assert launch["env"]["NODE_EXTRA_CA_CERTS"].endswith("control-plane-stub-ca.pem")
    assert Path(launch["env"]["NODE_EXTRA_CA_CERTS"]).exists()
    assert launch["env"]["ENABLE_TOOL_SEARCH"] == "auto"
    assert "route-hint-preload.cjs" in launch["env"]["NODE_OPTIONS"]
    assert launch["env"]["ZHUMENG_CLAUDE_ROUTE_HINT_PRELOAD"] == "enabled"
    assert launch["env"]["ZHUMENG_CLAUDE_ROUTE_HINT_SECRET"] == "route-hint-secret"
    catalog = json.loads(Path(launch["env"]["ZHUMENG_CLAUDE_ROUTE_HINT_CATALOG_PATH"]).read_text(encoding="utf-8"))
    expected_catalog = cp4_fixture_route_catalog(catalog_version="cp4-test-v1")
    assert catalog["catalog_version"] == "cp4-test-v1"
    assert catalog["catalog_hash"] == route_catalog_content_hash(expected_catalog)
    assert set(catalog["entries"]) == set(expected_catalog.entries)
    assert catalog["entries"]["deepseek-v4-pro"]["client_type"] == "claude_code_bridge_deepseek"
    assert catalog["entries"]["deepseek-v4-pro"]["formal_pool_allowed"] is False
    assert catalog["entries"]["deepseek-v4-pro[1m]"]["client_type"] == "claude_code_bridge_deepseek"
    assert catalog["entries"]["deepseek-v4-pro[1m]"]["formal_pool_allowed"] is False
    assert catalog["entries"]["claude-sonnet-4-6"]["client_type"] == "claude_code_native"
    assert catalog["entries"]["claude-sonnet-4-6"]["formal_pool_allowed"] is True
    assert "local-user-key" not in "\n".join(launch["env"].values())


def test_managed_launch_can_explicitly_enable_bridge_live_models_without_formal_pool_pollution(tmp_path: Path):
    captured = {}

    class FakeGuard:
        ready = {"listen": "http://127.0.0.1:18181"}

        def __enter__(self):
            return self

        def __exit__(self, exc_type, exc, tb):
            return False

    def fake_start_guard(plan, *, ready_timeout_seconds=10.0):
        captured["guard_plan"] = plan
        return FakeGuard()

    def fake_runner(command, *, env, cwd):
        captured["env"] = dict(env)
        return 0

    result = run_managed_claude_code(
        executable="claude",
        repo_root=REPO_ROOT,
        upstream_base="https://gateway.zhumeng.example",
        sub2api_auth="managed-access-token",
        attestation_secret="native-secret",
        route_hint_secret="route-hint-secret",
        config_root=tmp_path / "zhumeng-state",
        project_cwd=tmp_path,
        guard_listen_port=18181,
        bridge_live_models=("gpt-5.5", "deepseek-v4-pro", "agnes-1"),
        start_guard=fake_start_guard,
        process_runner=fake_runner,
        inherited_env={"PATH": "/usr/bin"},
    )

    assert result.returncode == 0
    catalog = json.loads(Path(captured["env"]["ZHUMENG_CLAUDE_ROUTE_HINT_CATALOG_PATH"]).read_text(encoding="utf-8"))
    assert catalog["entries"]["gpt-5.5"]["live_enabled"] is True
    assert catalog["entries"]["gpt-5.5"]["formal_pool_allowed"] is False
    assert catalog["entries"]["deepseek-v4-pro"]["live_enabled"] is True
    assert catalog["entries"]["deepseek-v4-pro"]["formal_pool_allowed"] is False
    assert catalog["entries"]["agnes-1"]["live_enabled"] is False
    assert catalog["entries"]["agnes-1"]["formal_pool_allowed"] is False


def test_managed_launch_preload_node_options_loads_from_paths_with_spaces(tmp_path: Path):
    node = shutil.which("node")
    if node is None:
        pytest.skip("managed Claude Code route-hint preload requires node")
    captured = {}

    class FakeGuard:
        ready = {"listen": "http://127.0.0.1:18181"}

        def __enter__(self):
            return self

        def __exit__(self, exc_type, exc, tb):
            return False

    def fake_start_guard(plan, *, ready_timeout_seconds=10.0):
        captured["guard_plan"] = plan
        return FakeGuard()

    def fake_runner(command, *, env, cwd):
        captured["env"] = dict(env)
        check = tmp_path / "check-preload.js"
        check.write_text("console.log('loaded');\n", encoding="utf-8")
        completed = subprocess.run(
            [node, str(check)],
            env=dict(env),
            cwd=cwd,
            text=True,
            capture_output=True,
            check=False,
            timeout=10,
        )
        assert completed.returncode == 0, completed.stderr
        assert "loaded" in completed.stdout
        return 0

    result = run_managed_claude_code(
        executable="claude",
        repo_root=REPO_ROOT,
        upstream_base="https://gateway.zhumeng.example",
        sub2api_auth="managed-access-token",
        attestation_secret="native-secret",
        route_hint_secret="route-hint-secret",
        config_root=tmp_path / "zhumeng state with spaces",
        project_cwd=tmp_path,
        guard_listen_port=18181,
        start_guard=fake_start_guard,
        process_runner=fake_runner,
        inherited_env={"PATH": "/usr/bin"},
    )

    assert result.returncode == 0
    assert "route-hint-preload.cjs" in captured["env"]["NODE_OPTIONS"]


def test_managed_launch_sanitizes_profile_id_for_summary_config_and_overlay_paths(tmp_path: Path):
    captured = {}
    config_root = tmp_path / "zhumeng-state"

    class FakeGuard:
        ready = {"listen": "http://127.0.0.1:18181"}

        def __enter__(self):
            return self

        def __exit__(self, exc_type, exc, tb):
            return False

    def fake_start_guard(plan, *, ready_timeout_seconds=10.0):
        captured["guard_plan"] = plan
        return FakeGuard()

    def fake_runner(command, *, env, cwd):
        captured["env"] = dict(env)
        return 0

    result = run_managed_claude_code(
        executable="claude",
        repo_root=REPO_ROOT,
        upstream_base="https://gateway.zhumeng.example",
        sub2api_auth="managed-access-token",
        attestation_secret="native-secret",
        route_hint_secret="route-hint-secret",
        config_root=config_root,
        project_cwd=tmp_path,
        guard_listen_port=18181,
        start_guard=fake_start_guard,
        process_runner=fake_runner,
        profile_id="../../outside profile",
    )

    claude_code_root = (config_root / "claude-code").resolve()
    assert result.returncode == 0
    assert result.guard_plan.config.summary_path.resolve().is_relative_to(claude_code_root)
    assert Path(captured["env"]["CLAUDE_CONFIG_DIR"]).resolve().is_relative_to(claude_code_root)
    assert Path(captured["env"]["ZHUMENG_CLAUDE_ROUTE_HINT_CATALOG_PATH"]).resolve().is_relative_to(claude_code_root)
    assert ".." not in Path(captured["env"]["ZHUMENG_CLAUDE_ROUTE_HINT_CATALOG_PATH"]).parts



def test_managed_launch_requires_route_hint_secret_for_real_runtime(tmp_path: Path):
    with pytest.raises(ValueError, match="route_hint_secret"):
        launcher.run_managed_claude_code(
            executable=tmp_path / "claude",
            repo_root=REPO_ROOT,
            upstream_base="http://127.0.0.1:18080",
            sub2api_auth="sub2api-entry",
            attestation_secret="attestation-secret",
            config_root=tmp_path / "zhumeng-state",
            project_cwd=tmp_path,
            guard_listen_port=43117,
            argv=[],
            inherited_env={"PATH": "/usr/bin"},
            start_guard=lambda *args, **kwargs: (_ for _ in ()).throw(AssertionError("guard must not start")),
            process_runner=lambda command, *, env, cwd: 0,
        )



def test_managed_launch_allows_non_anthropic_cloud_gateway_with_explicit_guard_flag(tmp_path: Path):
    events: list[object] = []

    @contextmanager
    def fake_start_guard(plan, *, ready_timeout_seconds: float = 10.0):
        events.append(plan)
        assert "--allow-nonloopback-upstream" in plan.command
        assert plan.config.upstream_base == "https://gateway.zhumeng.example"
        yield SimpleNamespace(process=SimpleNamespace(pid=12345), ready={"listen": "http://127.0.0.1:43117"})

    result = launcher.run_managed_claude_code(
        executable=tmp_path / "claude",
        repo_root=REPO_ROOT,
        upstream_base="https://gateway.zhumeng.example",
        sub2api_auth="sub2api-entry",
        attestation_secret="attestation-secret",
        route_hint_secret="route-hint-secret",
        config_root=tmp_path / "zhumeng-state",
        project_cwd=tmp_path,
        guard_listen_port=43117,
        argv=[],
        inherited_env={"PATH": "/usr/bin"},
        start_guard=fake_start_guard,
        process_runner=lambda command, *, env, cwd: 0,
    )

    assert result.returncode == 0
    assert events


def test_managed_launch_still_rejects_official_anthropic_upstream(tmp_path: Path):
    with pytest.raises(ValueError, match="official Claude/Anthropic hosts"):
        launcher.run_managed_claude_code(
            executable=tmp_path / "claude",
            repo_root=REPO_ROOT,
            upstream_base="https://api.anthropic.com",
            sub2api_auth="sub2api-entry",
            attestation_secret="attestation-secret",
            route_hint_secret="route-hint-secret",
            config_root=tmp_path / "zhumeng-state",
            project_cwd=tmp_path,
            guard_listen_port=43117,
            argv=[],
            inherited_env={"PATH": "/usr/bin"},
            start_guard=lambda *args, **kwargs: None,
            process_runner=lambda command, *, env, cwd: 0,
        )


class ManagedLaunchCaptureHandler(BaseHTTPRequestHandler):
    requests: list[dict[str, object]] = []

    def log_message(self, *args):
        pass

    def do_POST(self):
        length = int(self.headers.get("content-length", "0") or 0)
        body = self.rfile.read(length) if length else b""
        type(self).requests.append({
            "path": self.path,
            "headers": {key.lower(): value for key, value in self.headers.items()},
            "body": body,
        })
        data = b'{"ok":true}'
        self.send_response(200)
        self.send_header("content-type", "application/json")
        self.send_header("content-length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)


def test_managed_launch_real_guard_routes_base_url_with_native_headers(tmp_path: Path):
    node = shutil.which("node")
    if node is None:
        pytest.skip("managed Claude Code route-hint preload requires node")
    ManagedLaunchCaptureHandler.requests = []
    upstream = _start_server(ManagedLaunchCaptureHandler)
    guard_port = _free_port()
    fake_claude = tmp_path / "fake-claude.js"
    fake_claude.write_text(
        """
const body = JSON.stringify({
  model: "claude-sonnet-4-6",
  messages: [{role: "user", content: "walking-skeleton-prompt"}],
  max_tokens: 8
});
const response = await fetch(process.env.ANTHROPIC_BASE_URL + "/v1/messages?beta=true", {
  method: "POST",
  headers: {
    "content-type": "application/json",
    "x-claude-code-session-id": "11111111-2222-4333-8444-555555555555"
  },
  body
});
if (response.status !== 200) {
  throw new Error("unexpected guard response " + response.status);
}
""",
        encoding="utf-8",
    )

    def fake_claude_process(command, *, env, cwd):
        assert env["ANTHROPIC_BASE_URL"] == f"http://127.0.0.1:{guard_port}"
        assert env["CLAUDE_CODE_API_BASE_URL"] == f"http://127.0.0.1:{guard_port}"
        assert "--require=" in env["NODE_OPTIONS"]
        assert "route-hint-preload.cjs" in env["NODE_OPTIONS"]
        completed = subprocess.run(
            [node, str(fake_claude)],
            env=dict(env),
            cwd=cwd,
            text=True,
            capture_output=True,
            check=False,
            timeout=10,
        )
        assert completed.returncode == 0, completed.stderr
        return 0

    try:
        result = launcher.run_managed_claude_code(
            executable=tmp_path / "claude",
            repo_root=REPO_ROOT,
            upstream_base=f"http://127.0.0.1:{upstream.server_port}",
            sub2api_auth="sub2api-entry",
            attestation_secret="attestation-secret",
            route_hint_secret="route-hint-secret",
            config_root=tmp_path / "zhumeng-state",
            project_cwd=tmp_path,
            guard_listen_port=guard_port,
            argv=[],
            inherited_env={"PATH": "/usr/bin"},
            process_runner=fake_claude_process,
        )
    finally:
        upstream.shutdown()
        upstream.server_close()

    assert result.returncode == 0
    assert result.guard_ready["listen"] == f"http://127.0.0.1:{guard_port}"
    assert "--native-attestation" in result.guard_plan.command
    assert len(ManagedLaunchCaptureHandler.requests) == 1
    headers = ManagedLaunchCaptureHandler.requests[0]["headers"]
    assert headers["x-sub2api-client-type"] == "claude_code_native"
    assert headers["x-sub2api-guard-attested"] == "true"
    assert headers["x-sub2api-native-attestation"]
    assert headers["x-sub2api-native-signature"]
    summary = result.guard_plan.config.summary_path.read_text(encoding="utf-8")
    assert "walking-skeleton-prompt" not in summary


def test_managed_launch_executes_command_path_with_preload_injected(tmp_path: Path):
    node = shutil.which("node")
    if node is None:
        pytest.skip("managed Claude Code route-hint preload requires node")
    ManagedLaunchCaptureHandler.requests = []
    upstream = _start_server(ManagedLaunchCaptureHandler)
    guard_port = _free_port()
    fake_claude = tmp_path / "command-faithful-claude.js"
    fake_claude.write_text(
        """
const body = JSON.stringify({
  model: "claude-sonnet-4-6",
  messages: [{role: "user", content: "command-faithful-prompt"}],
  max_tokens: 8
});
const response = await fetch(process.env.ANTHROPIC_BASE_URL + "/v1/messages?beta=true", {
  method: "POST",
  headers: {
    "content-type": "application/json",
    "x-claude-code-session-id": "11111111-2222-4333-8444-555555555555"
  },
  body
});
if (response.status !== 200) {
  throw new Error("unexpected guard response " + response.status);
}
""",
        encoding="utf-8",
    )

    try:
        result = launcher.run_managed_claude_code(
            executable=node,
            repo_root=REPO_ROOT,
            upstream_base=f"http://127.0.0.1:{upstream.server_port}",
            sub2api_auth="sub2api-entry",
            attestation_secret="attestation-secret",
            route_hint_secret="route-hint-secret",
            config_root=tmp_path / "zhumeng-state",
            project_cwd=tmp_path,
            guard_listen_port=guard_port,
            argv=[str(fake_claude)],
            inherited_env={"PATH": "/usr/bin"},
        )
    finally:
        upstream.shutdown()
        upstream.server_close()

    assert result.returncode == 0
    assert result.launch_plan.command == [node, str(fake_claude)]
    assert len(ManagedLaunchCaptureHandler.requests) == 1
    headers = ManagedLaunchCaptureHandler.requests[0]["headers"]
    assert headers["x-sub2api-client-type"] == "claude_code_native"
    assert headers["x-sub2api-guard-attested"] == "true"
    summary = result.guard_plan.config.summary_path.read_text(encoding="utf-8")
    assert "command-faithful-prompt" not in summary


def test_managed_launch_non_fetch_http_client_fails_closed_without_native_egress(tmp_path: Path):
    node = shutil.which("node")
    if node is None:
        pytest.skip("managed Claude Code route-hint preload requires node")
    ManagedLaunchCaptureHandler.requests = []
    upstream = _start_server(ManagedLaunchCaptureHandler)
    guard_port = _free_port()
    fake_claude = tmp_path / "http-client-claude.js"
    fake_claude.write_text(
        f"""
const http = require('node:http');
const body = JSON.stringify({{
  model: "claude-sonnet-4-6",
  messages: [{{role: "user", content: "unhooked-http-prompt"}}],
  max_tokens: 8
}});
async function main() {{
const status = await new Promise((resolve, reject) => {{
  const req = http.request({{
    hostname: '127.0.0.1',
    port: {guard_port},
    path: '/v1/messages',
    method: 'POST',
    headers: {{
      'content-type': 'application/json',
      'content-length': Buffer.byteLength(body),
      'x-claude-code-session-id': '11111111-2222-4333-8444-555555555555'
    }}
  }}, (res) => {{
    res.resume();
    res.on('end', () => resolve(res.statusCode));
  }});
  req.on('error', reject);
  req.end(body);
}});
if (status !== 403) {{
  throw new Error('expected guard fail-closed 403, got ' + status);
}}
}}
main().catch((err) => {{
  console.error(err);
  process.exit(1);
}});
""",
        encoding="utf-8",
    )

    try:
        result = launcher.run_managed_claude_code(
            executable=node,
            repo_root=REPO_ROOT,
            upstream_base=f"http://127.0.0.1:{upstream.server_port}",
            sub2api_auth="sub2api-entry",
            attestation_secret="attestation-secret",
            route_hint_secret="route-hint-secret",
            config_root=tmp_path / "zhumeng-state",
            project_cwd=tmp_path,
            guard_listen_port=guard_port,
            argv=[str(fake_claude)],
            inherited_env={"PATH": "/usr/bin"},
        )
    finally:
        upstream.shutdown()
        upstream.server_close()

    assert result.returncode == 0
    assert ManagedLaunchCaptureHandler.requests == []
    summary = result.guard_plan.config.summary_path.read_text(encoding="utf-8")
    assert "quarantine_block" in summary or "route_hint_unavailable" in summary or "route_hint_required" in summary
    assert "unhooked-http-prompt" not in summary


def test_managed_launch_preload_routes_bridge_model_to_internal_skeleton_without_native_egress(tmp_path: Path):
    node = shutil.which("node")
    if node is None:
        pytest.skip("managed Claude Code route-hint preload requires node")
    ManagedLaunchCaptureHandler.requests = []
    upstream = _start_server(ManagedLaunchCaptureHandler)
    guard_port = _free_port()
    fake_claude = tmp_path / "fake-claude-bridge.js"
    fake_claude.write_text(
        """
const body = JSON.stringify({
  model: "deepseek-v4-pro[1m]",
  messages: [{role: "user", content: "bridge walking skeleton"}],
  max_tokens: 8
});
const response = await fetch(process.env.ANTHROPIC_BASE_URL + "/v1/messages?beta=true", {
  method: "POST",
  headers: {
    "content-type": "application/json",
    "x-claude-code-session-id": "11111111-2222-4333-8444-555555555555"
  },
  body
});
const text = await response.text();
if (response.status !== 200 || !text.includes("message_start") || !text.includes("deepseek-v4-pro[1m]")) {
  throw new Error("unexpected bridge skeleton " + response.status + " " + text);
}
""",
        encoding="utf-8",
    )

    def fake_claude_process(command, *, env, cwd):
        completed = subprocess.run(
            [node, str(fake_claude)],
            env=dict(env),
            cwd=cwd,
            text=True,
            capture_output=True,
            check=False,
            timeout=10,
        )
        assert completed.returncode == 0, completed.stderr
        return 0

    try:
        result = launcher.run_managed_claude_code(
            executable=tmp_path / "claude",
            repo_root=REPO_ROOT,
            upstream_base=f"http://127.0.0.1:{upstream.server_port}",
            sub2api_auth="sub2api-entry",
            attestation_secret="attestation-secret",
            route_hint_secret="route-hint-secret",
            config_root=tmp_path / "zhumeng-state",
            project_cwd=tmp_path,
            guard_listen_port=guard_port,
            argv=[],
            inherited_env={"PATH": "/usr/bin"},
            process_runner=fake_claude_process,
        )
    finally:
        upstream.shutdown()
        upstream.server_close()

    assert result.returncode == 0
    assert ManagedLaunchCaptureHandler.requests == []
    summary = result.guard_plan.config.summary_path.read_text(encoding="utf-8")
    assert "claude_code_bridge_deepseek" in summary
    assert "deepseek-v4-pro[1m]" in summary
    assert '"native_attested": false' in summary
    assert '"formal_pool_allowed": false' in summary
    assert "bridge walking skeleton" not in summary


def _start_server(handler: type[BaseHTTPRequestHandler]) -> ThreadingHTTPServer:
    server = ThreadingHTTPServer(("127.0.0.1", 0), handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return server


def _free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])

def test_managed_launch_passes_managed_device_headers_to_guard(tmp_path: Path):
    captured = {}

    class FakeGuard:
        ready = {"listen": "http://127.0.0.1:18181"}

        def __enter__(self):
            return self

        def __exit__(self, exc_type, exc, tb):
            return False

    def fake_start_guard(plan, *, ready_timeout_seconds=10.0):
        captured["guard_plan"] = plan
        return FakeGuard()

    def fake_runner(command, *, env, cwd):
        return 0

    result = run_managed_claude_code(
        executable="claude",
        repo_root=REPO_ROOT,
        upstream_base="https://gateway.zhumeng.example",
        sub2api_auth="managed-access-token",
        managed_session_id="managed-session",
        device_id=9,
        attestation_secret="native-secret",
        route_hint_secret="route-hint-secret",
        config_root=tmp_path,
        project_cwd=tmp_path,
        guard_listen_port=18181,
        start_guard=fake_start_guard,
        process_runner=fake_runner,
    )

    command = captured["guard_plan"].command
    assert "--managed-session" in command
    assert command[command.index("--managed-session") + 1] == "managed-session"
    assert "--device-id" in command
    assert command[command.index("--device-id") + 1] == "9"
    assert result.returncode == 0
