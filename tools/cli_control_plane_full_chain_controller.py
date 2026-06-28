#!/usr/bin/env python3
"""Repo-local localhost-only dual-scenario full-chain controller validation runner."""
from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Any, Mapping, Sequence
from urllib.parse import urlsplit
import argparse
import ipaddress
import json
import os
import re
import socket
import subprocess
import sys
import time
import signal
import urllib.error
import urllib.request

if __package__ in {None, ''}:
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from tools.cli_control_plane_ab_diff import message_shape
from tools.cli_control_plane_guard import (
    ExecutionController,
    GuardConfig,
    RedactingForwarder,
    validate_loopback_url,
)
from tools.claude_code_route_trust import (
    RouteCatalog,
    RouteHintReplayCache,
    build_signed_route_hint_headers,
    cp4_fixture_route_catalog,
)


WORKTREE = Path(__file__).resolve().parents[1]
DEFAULT_CC_GATEWAY_ROOT = Path('/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5')
CC_GATEWAY_ROOT = Path(os.environ.get('CC_GATEWAY_ROOT', str(DEFAULT_CC_GATEWAY_ROOT)))
DEFAULT_CC_HARNESS_SCRIPT = WORKTREE / 'tools/cc_gateway_localhost_harness.mjs'
DEFAULT_SUB2API_HARNESS_DIR = WORKTREE / 'backend/.tmp-harness/cli-through-sub2api'
LOCAL_ROUTE_HINT_SECRET = 'localhost-full-chain-route-hint-secret'
LOCAL_ROUTE_HINT_SESSION_REF = '11111111-2222-4333-8444-555555555555'
LOCAL_NATIVE_ATTESTATION_SECRET = 'localhost-full-chain-native-attestation-secret'
LOCAL_NATIVE_MANAGED_ACCESS_TOKEN = 'localhost-full-chain-managed-access-token'
LOCAL_NATIVE_RUNTIME_HASH = 'sha256:' + ('1' * 64)
LOCAL_NATIVE_OVERLAY_HASH = 'sha256:' + ('2' * 64)
LOCAL_NATIVE_CATALOG_HASH = 'sha256:' + ('3' * 64)
LOCAL_NATIVE_CATALOG_VERSION = 'localhost-full-chain-cp4-v1'
FORBIDDEN_HOST_SNIPPETS = ('anthropic.com', 'claude.ai', 'claude.com')
REAL_UPSTREAM_ENV_VARS = (
    'ANTHROPIC_BASE_URL',
    'CLAUDE_CODE_API_BASE_URL',
    'ANTHROPIC_API_KEY',
    'HTTPS_PROXY',
    'HTTP_PROXY',
    'ALL_PROXY',
)
SENSITIVE_PATTERNS = {
    'authorization_header': re.compile(r'Authori' + r'zation:\s*Bearer\s+\S+', re.IGNORECASE),
    'bearer_token_value': re.compile(r'Bearer\s+[A-Za-z0-9._~+/-]{8,}'),
    'raw_prompt_marker': re.compile(r'raw-prompt', re.IGNORECASE),
    'secret_token_marker': re.compile(r'secret-token', re.IGNORECASE),
    'proxy_credential_marker': re.compile(r'proxy-credential', re.IGNORECASE),
    'selected_token_marker': re.compile(r'synthetic-selected-token', re.IGNORECASE),
    'sub2api_auth_value': re.compile(r'sub2api-entry', re.IGNORECASE),
    'email': re.compile(r'[^@\s]+@[^@\s]+\.[^@\s]+'),
}
SAFE_SCENARIO_KEYS = {
    'scenario',
    'status',
    'run_dir',
    'safe_deliverable_dir',
    'real_anthropic_upstream',
    'mock_request_count',
    'controller_stop_requested_events',
    'sub2api_selected_count',
    'cost_envelope_block',
    'guard_counts',
    'message_shape',
    'client_status',
    'sensitive_scan',
    'sensitive_scan_failures',
    'scanned_paths',
    'readiness',
    'billing_disposition',
    'authority_boundary',
    'upstream_safety',
    'observed',
}
CP4_SCENARIO_SAFE_KEYS = {
    'scenario',
    'status',
    'real_anthropic_upstream',
    'mock_request_count',
    'client_status',
    'sensitive_scan',
    'sensitive_scan_failures',
    'billing_disposition',
    'authority_boundary',
    'upstream_safety',
    'observed',
}


@dataclass(frozen=True)
class SensitiveScanResult:
    status: str
    failures: list[str]
    scanned_paths: list[str]


@dataclass(frozen=True)
class ProcessHandle:
    process: subprocess.Popen[bytes]
    first_lines: list[str]


@dataclass(frozen=True)
class ScenarioExpectation:
    expect_mock_request_count: int
    expect_cost_envelope_block: bool
    expect_stop_events: int
    expect_sub2api_selected_count: int
    expect_response_status: int
    expect_terminated_by_controller: bool
    linger_after_response: bool


@dataclass(frozen=True)
class CP4FormalPoolScenario:
    name: str
    expect_mock_request_count: int
    expect_response_status: int
    real_anthropic_upstream: bool = False


CP4_FORMAL_POOL_SCENARIOS = (
    CP4FormalPoolScenario('valid_trusted_context_strip_cch_present', 1, 200),
    CP4FormalPoolScenario('observed_2_1_181_strip_cch_present', 1, 200),
    CP4FormalPoolScenario('observed_2_1_195_strip_cch_present', 1, 200),
    CP4FormalPoolScenario('forged_authority_headers_ignored', 1, 200),
    CP4FormalPoolScenario('missing_trusted_context_fail_closed', 0, 403),
    CP4FormalPoolScenario('default_strip_cch_present_inbound', 1, 200),
    CP4FormalPoolScenario('default_strip_no_cch_inbound', 1, 200),
    CP4FormalPoolScenario('optional_no_cch_profile_with_proof', 1, 200),
    CP4FormalPoolScenario('optional_signed_cch_profile_requires_proof', 0, 403),
    CP4FormalPoolScenario('cc_gateway_unavailable_no_direct_fallback', 0, 502),
)


def create_run_directory(base_tmp: Path | str = Path('/tmp'), *, now: datetime | None = None, pid: int | None = None) -> Path:
    base = Path(base_tmp)
    moment = now or datetime.now().astimezone()
    if moment.tzinfo is None:
        moment = moment.astimezone()
    run_pid = pid or os.getpid()
    prefix = f"full-chain-controller-{moment.strftime('%Y%m%d')}-{moment.strftime('%H%M%S-%f')}-{run_pid}"
    candidate = base / prefix
    suffix = 0
    while candidate.exists():
        suffix += 1
        candidate = base / f'{prefix}-{suffix}'
    candidate.mkdir(parents=True, mode=0o700)
    return candidate


def prepare_scenario_directories(run_dir: Path, scenario_name: str) -> dict[str, Path]:
    scenario_dir = run_dir / scenario_name
    cc_dir = scenario_dir / 'cc'
    sub2api_dir = scenario_dir / 'sub2api'
    guard_dir = scenario_dir / 'guard'
    safe_dir = scenario_dir / 'safe-deliverable'
    for path in (scenario_dir, cc_dir, sub2api_dir, guard_dir, safe_dir):
        if path.exists():
            if any(path.iterdir()):
                raise RuntimeError(f'stale artifacts detected: {path}')
        else:
            path.mkdir(parents=True, mode=0o700)
    return {
        'scenario_dir': scenario_dir,
        'cc_dir': cc_dir,
        'sub2api_dir': sub2api_dir,
        'guard_dir': guard_dir,
        'safe_dir': safe_dir,
    }


def build_safe_messages_fixture() -> dict[str, Any]:
    return {
        'model': 'claude-sonnet-4-6',
        'messages': [
            {
                'role': 'user',
                'content': [
                    {'type': 'text', 'text': 'Synthetic localhost canary fixture.'},
                ],
            }
        ],
        'max_tokens': 128,
        'stream': False,
        'tools': [],
        'output_config': {'effort': 'low'},
    }


def build_unsafe_messages_fixture() -> dict[str, Any]:
    return {
        'model': 'claude-sonnet-4-6',
        'messages': [
            {
                'role': 'user',
                'content': [
                    {'type': 'text', 'text': 'Synthetic oversized localhost fixture.'},
                ],
            }
        ],
        'max_tokens': 32001,
        'stream': False,
        'tools': [],
        'output_config': {'effort': 'low'},
    }


def build_billing_messages_fixture(shape: str, *, version: str = '2.1.179') -> dict[str, Any]:
    fixture = build_safe_messages_fixture()
    suffix = ''.join(['a', 'b', 'c'])
    if shape == 'cch_present':
        cch_value = ''.join(['a', 'b', 'c', 'd', 'e'])
        billing = f'x-anthropic-billing-header: cc_version={version}.{suffix}; cc_entrypoint=sdk-cli; cch={cch_value};'
    elif shape == 'no_cch':
        billing = f'x-anthropic-billing-header: cc_version={version}.{suffix}; cc_entrypoint=sdk-cli;'
    else:
        return fixture
    fixture['system'] = [{'type': 'text', 'text': billing}]
    return fixture


def default_route_hint_catalog() -> RouteCatalog:
    return cp4_fixture_route_catalog(
        runtime_hash=LOCAL_NATIVE_RUNTIME_HASH,
        overlay_hash=LOCAL_NATIVE_OVERLAY_HASH,
        catalog_hash=LOCAL_NATIVE_CATALOG_HASH,
        catalog_version=LOCAL_NATIVE_CATALOG_VERSION,
    )




def cp4_profile_env_for_scenario(name: str) -> dict[str, str]:
    env = {
        'CC_HARNESS_POLICY_VERSION': '2.1.179',
        'SUB2API_HARNESS_POLICY_VERSION': '2.1.179',
        'CC_HARNESS_OBSERVED_CLI_VERSION': '2.1.179',
        'CC_HARNESS_EGRESS_PROFILE_REF': 'strip_attribution',
        'SUB2API_HARNESS_EGRESS_PROFILE_REF': 'strip_attribution',
        'CC_HARNESS_BILLING_SHAPE_POLICY': 'strip',
        'SUB2API_HARNESS_BILLING_SHAPE_POLICY': 'strip',
    }
    if name == 'observed_2_1_181_strip_cch_present':
        env['CC_HARNESS_OBSERVED_CLI_VERSION'] = '2.1.181'
    elif name == 'observed_2_1_195_strip_cch_present':
        env['CC_HARNESS_OBSERVED_CLI_VERSION'] = '2.1.195'
    if name == 'optional_no_cch_profile_with_proof':
        env.update({
            'CC_HARNESS_EGRESS_PROFILE_REF': 'claude_code_2_1_179_custom_base_no_cch',
            'SUB2API_HARNESS_EGRESS_PROFILE_REF': 'claude_code_2_1_179_custom_base_no_cch',
            'CC_HARNESS_BILLING_SHAPE_POLICY': 'no_cch',
            'SUB2API_HARNESS_BILLING_SHAPE_POLICY': 'no_cch',
            'CC_HARNESS_ENABLE_NO_CCH_PROOF': '1',
        })
    elif name == 'optional_signed_cch_profile_requires_proof':
        env.update({
            'CC_HARNESS_EGRESS_PROFILE_REF': 'claude_code_2_1_179_first_party_signed_cch',
            'SUB2API_HARNESS_EGRESS_PROFILE_REF': 'claude_code_2_1_179_first_party_signed_cch',
            'CC_HARNESS_BILLING_SHAPE_POLICY': 'signed_cch',
            'SUB2API_HARNESS_BILLING_SHAPE_POLICY': 'signed_cch',
        })
    return env

def local_sub2api_native_attestation_env() -> dict[str, str]:
    catalog = [{
        'model_id': 'claude-sonnet-4-6',
        'route': 'claude_code_native',
        'provider_owner': 'zhumeng_managed',
        'credential_scope': 'formal_pool',
        'gateway_location': 'cloud',
        'runtime_hash': LOCAL_NATIVE_RUNTIME_HASH,
        'overlay_hash': LOCAL_NATIVE_OVERLAY_HASH,
        'catalog_hash': LOCAL_NATIVE_CATALOG_HASH,
        'catalog_version': LOCAL_NATIVE_CATALOG_VERSION,
        'catalog_fresh': True,
    }]
    return {
        'SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET': LOCAL_NATIVE_ATTESTATION_SECRET,
        'SUB2API_CLAUDE_CODE_NATIVE_FORMAL_POOL_MODELS': 'claude-sonnet-4-6',
        'SUB2API_CLAUDE_CODE_NATIVE_ROUTE_CATALOG_JSON': json.dumps(catalog, separators=(',', ':')),
        'SUB2API_CLAUDE_CODE_NATIVE_RUNTIME_HASHES': LOCAL_NATIVE_RUNTIME_HASH,
        'SUB2API_CLAUDE_CODE_NATIVE_OVERLAY_HASHES': LOCAL_NATIVE_OVERLAY_HASH,
        'SUB2API_CLAUDE_CODE_NATIVE_CATALOG_HASHES': LOCAL_NATIVE_CATALOG_HASH,
    }


def build_synthetic_client_headers(
    *,
    body: bytes,
    request_path: str,
    now: int | None = None,
    nonce: str | None = None,
) -> dict[str, str]:
    route_headers = build_signed_route_hint_headers(
        body=body,
        request_path=request_path,
        catalog=default_route_hint_catalog(),
        model_id=json.loads(body.decode('utf-8'))['model'],
        session_ref=LOCAL_ROUTE_HINT_SESSION_REF,
        secret=LOCAL_ROUTE_HINT_SECRET,
        now=now,
        nonce=nonce,
    )
    return {
        'content-type': 'application/json',
        'user-agent': 'claude-cli/2.1.179 (external, sdk-cli)',
        'anthropic-beta': 'oauth-2025-04-20',
        'x-claude-code-session-id': LOCAL_ROUTE_HINT_SESSION_REF,
        **route_headers,
    }


def perform_sensitive_scan(paths: Sequence[Path | str]) -> SensitiveScanResult:
    failures: list[str] = []
    scanned_paths: list[str] = []
    for raw_path in paths:
        path = Path(raw_path)
        scanned_paths.append(str(path))
        if not path.exists():
            failures.append(f'missing_scan_target:{path}')
            continue
        try:
            text = path.read_text(encoding='utf-8', errors='replace')
        except OSError:
            failures.append(f'unreadable_scan_target:{path}')
            continue
        for label, pattern in SENSITIVE_PATTERNS.items():
            if pattern.search(text):
                failures.append(f'sensitive_pattern:{label}:{path}')
    return SensitiveScanResult(status='FAIL' if failures else 'PASS', failures=failures, scanned_paths=scanned_paths)


def build_full_chain_report_payload(
    *,
    run_dir: Path,
    run_id: str,
    scenario_a: Mapping[str, Any],
    scenario_b: Mapping[str, Any],
    scan_result: SensitiveScanResult,
    cp4_scenarios: Sequence[Mapping[str, Any]] | None = None,
) -> dict[str, Any]:
    safe_a = _sanitize_scenario_payload(scenario_a)
    safe_b = _sanitize_scenario_payload(scenario_b)
    safe_cp4 = _sanitize_cp4_scenarios(cp4_scenarios or [])
    cp4_status = all(item.get('status') == 'PASS' for item in safe_cp4.values()) if safe_cp4 else True
    status = 'PASS' if safe_a.get('status') == 'PASS' and safe_b.get('status') == 'PASS' and cp4_status and scan_result.status == 'PASS' else 'FAIL'
    payload = {
        'run_id': run_id,
        'run_dir': str(run_dir),
        'safe_deliverable_dir': str(run_dir / 'safe-deliverable'),
        'status': status,
        'real_anthropic_upstream': False,
        'scenario_a': safe_a,
        'scenario_b': safe_b,
        'sensitive_scan': scan_result.status,
        'sensitive_scan_failures': list(scan_result.failures),
        'scanned_paths': list(scan_result.scanned_paths),
    }
    if safe_cp4:
        payload['cp4_scenarios'] = safe_cp4
    return payload


def render_report_markdown(payload: Mapping[str, Any]) -> str:
    lines = [
        '# Localhost-only dual-scenario full-chain controller validation',
        '',
        f"- run_id: {payload.get('run_id')}",
        f"- status: {payload.get('status')}",
        f"- real_anthropic_upstream: {str(bool(payload.get('real_anthropic_upstream'))).lower()}",
        f"- sensitive_scan: {payload.get('sensitive_scan')}",
        '',
    ]
    for scenario_name in ('scenario_a', 'scenario_b'):
        scenario = payload.get(scenario_name, {})
        lines.extend([
            f'## {scenario_name}',
            '',
            f"- status: {scenario.get('status')}",
            f"- mock_request_count: {scenario.get('mock_request_count')}",
            f"- controller_stop_requested_events: {scenario.get('controller_stop_requested_events')}",
            f"- sub2api_selected_count: {scenario.get('sub2api_selected_count')}",
            f"- cost_envelope_block: {scenario.get('cost_envelope_block')}",
            f"- sensitive_scan: {scenario.get('sensitive_scan')}",
            '',
            '### guard_counts',
            '',
        ])
        guard_counts = scenario.get('guard_counts', {})
        lines.extend(f'- {key}: {guard_counts[key]}' for key in sorted(guard_counts))
        lines.extend(['', '### message_shape', ''])
        shape = scenario.get('message_shape', {})
        lines.extend(f'- {key}: {shape[key]}' for key in sorted(shape))
        lines.append('')
    return '\n'.join(lines).rstrip() + '\n'


def free_port() -> int:
    sock = socket.socket()
    sock.bind(('127.0.0.1', 0))
    port = int(sock.getsockname()[1])
    sock.close()
    return port


def read_json_line(process: subprocess.Popen[bytes], timeout: float = 40.0) -> tuple[dict[str, Any], list[str]]:
    deadline = time.time() + timeout
    lines: list[str] = []
    while time.time() < deadline:
        raw = process.stdout.readline() if process.stdout is not None else b''
        if not raw:
            if process.poll() is not None:
                break
            time.sleep(0.1)
            continue
        text = raw.decode('utf-8', 'replace').strip()
        lines.append(text)
        try:
            return json.loads(text), lines
        except json.JSONDecodeError:
            continue
    exit_code = process.poll()
    stdout_tail = b''
    stderr_tail = b''
    if exit_code is not None:
        try:
            stdout_tail, stderr_tail = process.communicate(timeout=0.5)
        except subprocess.TimeoutExpired:
            stdout_tail, stderr_tail = b'', b''
    details = [
        f'exit_code={exit_code if exit_code is not None else "still_running"}',
        f'stdout_lines={_safe_diagnostic_tail("\\n".join(lines[-5:]).encode("utf-8"))!r}',
    ]
    if stdout_tail:
        details.append(f'stdout_tail={_safe_diagnostic_tail(stdout_tail)!r}')
    if stderr_tail:
        details.append(f'stderr_tail={_safe_diagnostic_tail(stderr_tail)!r}')
    raise RuntimeError(f'expected JSON line from process ({", ".join(details)})')


def _safe_diagnostic_tail(data: bytes, *, max_chars: int = 4000) -> str:
    text = data.decode('utf-8', 'replace')
    if len(text) > max_chars:
        text = '...<truncated>...\n' + text[-max_chars:]
    for label, pattern in SENSITIVE_PATTERNS.items():
        text = pattern.sub(f'<redacted:{label}>', text)
    return text


def scrub_real_upstream_env(extra: Mapping[str, str] | None = None) -> dict[str, str]:
    env = dict(os.environ)
    for key in REAL_UPSTREAM_ENV_VARS:
        env.pop(key, None)
    env['NO_PROXY'] = '127.0.0.1,localhost,::1'
    if extra:
        env.update({key: value for key, value in extra.items() if value is not None})
    return env


def assert_loopback_url(url: str, field_name: str) -> str:
    return validate_loopback_url(url, field_name=field_name)


def assert_loopback_connect_targets(targets: Sequence[str]) -> None:
    for target in targets:
        lowered = target.lower()
        if any(snippet in lowered for snippet in FORBIDDEN_HOST_SNIPPETS):
            raise ValueError(f'connect target must stay local: {target}')
        host, _, _port = target.rpartition(':')
        if not host:
            raise ValueError(f'invalid connect target: {target}')
        if host == 'localhost':
            continue
        try:
            ip = ipaddress.ip_address(host)
        except ValueError as exc:
            raise ValueError(f'connect target must use loopback host: {target}') from exc
        if not ip.is_loopback:
            raise ValueError(f'connect target must use loopback host: {target}')


def run_control_plane_probes(guard_url: str) -> None:
    cases = [
        ('GET', '/api/claude_cli/bootstrap?entrypoint=sdk-cli', None, 200),
        ('GET', '/v1/mcp_servers?limit=1000', None, 200),
        ('POST', '/api/event_logging/v2/batch', json.dumps({'event': 'synthetic-local-event'}).encode('utf-8'), 204),
        ('POST', '/api/eval/bootstrap', json.dumps({'eval': 'synthetic-local-eval'}).encode('utf-8'), 204),
        ('GET', '/totally-unknown', None, 403),
    ]
    for method, path, body, expected_status in cases:
        request = urllib.request.Request(
            guard_url.rstrip('/') + path,
            data=body,
            method=method,
            headers={'content-type': 'application/json', 'user-agent': 'synthetic-control-plane-probe/1.0'},
        )
        try:
            with urllib.request.urlopen(request, timeout=10) as response:
                status = response.status
        except urllib.error.HTTPError as exc:
            status = exc.code
        if status != expected_status:
            raise RuntimeError(f'control-plane probe {method} {path} expected {expected_status}, got {status}')


def run_synthetic_client_subprocess(
    guard_url: str,
    response_path: Path,
    fixture: Mapping[str, Any],
    *,
    linger_after_response: bool,
    extra_headers: Mapping[str, str] | None = None,
) -> subprocess.Popen[bytes]:
    body = json.dumps(dict(fixture), separators=(',', ':')).encode('utf-8')
    request_path = '/v1/messages?beta=true'
    headers = build_synthetic_client_headers(body=body, request_path=request_path)
    if extra_headers:
        headers.update({str(key): str(value) for key, value in extra_headers.items()})
    script = """
import json
import os
import time
import signal
import urllib.error
import urllib.request
from pathlib import Path

guard_url = os.environ['GUARD_URL']
response_path = Path(os.environ['RESPONSE_PATH'])
body = os.environ['FIXTURE_JSON'].encode('utf-8')
headers = json.loads(os.environ['REQUEST_HEADERS_JSON'])
linger = os.environ['LINGER_AFTER_RESPONSE'] == '1'
received_sigterm = False
def _handle_sigterm(signum, frame):
    global received_sigterm
    received_sigterm = True
signal.signal(signal.SIGTERM, _handle_sigterm)
response_path.write_text(json.dumps({'response_status': None, 'request_started': True, 'response_status_observed': False}), encoding='utf-8')
request = urllib.request.Request(
    guard_url.rstrip('/') + '/v1/messages?beta=true',
    data=body,
    method='POST',
    headers=headers,
)
status = None
try:
    with urllib.request.urlopen(request, timeout=30) as response:
        status = response.status
        response_path.write_text(json.dumps({'response_status': status, 'response_status_observed': True}), encoding='utf-8')
        response.read()
except urllib.error.HTTPError as exc:
    status = exc.code
    response_path.write_text(json.dumps({'response_status': status, 'response_status_observed': True}), encoding='utf-8')
    exc.read()
if linger:
    time.sleep(30)
"""
    env = scrub_real_upstream_env({
        'GUARD_URL': guard_url,
        'RESPONSE_PATH': str(response_path),
        'FIXTURE_JSON': json.dumps(dict(fixture), separators=(',', ':')),
        'REQUEST_HEADERS_JSON': json.dumps(headers, separators=(',', ':')),
        'LINGER_AFTER_RESPONSE': '1' if linger_after_response else '0',
    })
    return subprocess.Popen(
        [sys.executable, '-c', script],
        cwd=str(response_path.parent),
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        start_new_session=True,
    )


def stop_process(process: subprocess.Popen[bytes] | None, *, timeout: float = 3.0) -> tuple[bytes, bytes]:
    if process is None:
        return b'', b''
    if process.poll() is None:
        process.terminate()
        try:
            return process.communicate(timeout=timeout)
        except subprocess.TimeoutExpired:
            process.kill()
            try:
                return process.communicate(timeout=timeout)
            except subprocess.TimeoutExpired:
                return b'', b''
    try:
        return process.communicate(timeout=timeout)
    except subprocess.TimeoutExpired:
        process.kill()
        try:
            return process.communicate(timeout=timeout)
        except subprocess.TimeoutExpired:
            return b'', b''


def write_process_capture(path: Path, first_lines: Sequence[str], output: bytes) -> None:
    text = '\n'.join(str(line) for line in first_lines)
    tail = output.decode('utf-8', 'replace') if output else ''
    combined = text
    if text and tail:
        combined += '\n'
    combined += tail
    path.write_text(combined, encoding='utf-8')



def load_json_object_or_empty(path: Path) -> dict[str, Any]:
    try:
        text = path.read_text(encoding='utf-8')
    except OSError:
        return {}
    if not text.strip():
        return {}
    try:
        value = json.loads(text)
    except json.JSONDecodeError:
        return {}
    return value if isinstance(value, dict) else {}

def load_jsonl(path: Path) -> list[dict[str, Any]]:
    if not path.exists():
        return []
    records: list[dict[str, Any]] = []
    for line in path.read_text(encoding='utf-8', errors='replace').splitlines():
        stripped = line.strip()
        if not stripped:
            continue
        records.append(json.loads(stripped))
    return records


def run_scenario(
    *,
    run_dir: Path,
    scenario_name: str,
    fixture: Mapping[str, Any],
    expectation: ScenarioExpectation,
    cc_harness_script: Path,
    sub2api_harness_dir: Path,
    extra_env: Mapping[str, str] | None = None,
    extra_client_headers: Mapping[str, str] | None = None,
    cc_gateway_unavailable: bool = False,
) -> dict[str, Any]:
    dirs = prepare_scenario_directories(run_dir, scenario_name)
    processes: list[tuple[str, ProcessHandle]] = []
    forwarder: RedactingForwarder | None = None
    client_proc: subprocess.Popen[bytes] | None = None

    try:
        merged_extra_env = dict(extra_env or {})
        if cc_gateway_unavailable:
            cc_gateway_url = assert_loopback_url(f'http://127.0.0.1:{free_port()}', f'{scenario_name}_cc_gateway_url')
            cc_state = {'mock_url': 'http://127.0.0.1:1'}
        else:
            cc_env = scrub_real_upstream_env({'CC_HARNESS_OUT': str(dirs['cc_dir']), **merged_extra_env})
            cc_proc = subprocess.Popen(
                [str(CC_GATEWAY_ROOT / 'node_modules/.bin/tsx'), str(cc_harness_script)],
                cwd=str(CC_GATEWAY_ROOT),
                env=cc_env,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
            )
            cc_state, cc_lines = read_json_line(cc_proc)
            processes.append((f'{scenario_name}_cc_harness', ProcessHandle(cc_proc, cc_lines)))
            cc_gateway_url = assert_loopback_url(str(cc_state['cc_gateway_url']), f'{scenario_name}_cc_gateway_url')
            _mock_url = assert_loopback_url(str(cc_state['mock_url']), f'{scenario_name}_mock_url')

        sub_summary_path = dirs['sub2api_dir'] / 'summary.jsonl'
        sub_summary_path.write_text('', encoding='utf-8')
        sub_boundary_ledger_path = dirs['sub2api_dir'] / 'claude-code-session-boundary-ledger.json'
        sub_env = scrub_real_upstream_env({
            'CC_GATEWAY_URL': cc_gateway_url,
            'SUB2API_HARNESS_SUMMARY': str(sub_summary_path),
            'SUB2API_CLAUDE_CODE_SESSION_BOUNDARY_LEDGER_FILE': str(sub_boundary_ledger_path),
            **local_sub2api_native_attestation_env(),
            **merged_extra_env,
        })
        sub_proc = subprocess.Popen(
            ['go', 'run', '.'],
            cwd=str(sub2api_harness_dir),
            env=sub_env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
        sub_state, sub_lines = read_json_line(sub_proc)
        processes.append((f'{scenario_name}_sub2api_harness', ProcessHandle(sub_proc, sub_lines)))
        sub2api_url = assert_loopback_url(str(sub_state['listen']), f'{scenario_name}_sub2api_harness_url')

        guard_port = free_port()
        guard_summary_path = dirs['guard_dir'] / 'summary.jsonl'
        controller = ExecutionController(mode='canary_single_message', stop_grace_seconds=1.0)
        forwarder = RedactingForwarder(
            GuardConfig(
                listen_host='127.0.0.1',
                listen_port=guard_port,
                upstream_base=sub2api_url,
                sub2api_auth='sub2api-entry-local-safe',
                summary_path=guard_summary_path,
                connect_mode='stub',
                max_messages=1,
                route_hint_secret=LOCAL_ROUTE_HINT_SECRET,
                route_hint_catalog=default_route_hint_catalog(),
                route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
                native_attestation_secret=LOCAL_NATIVE_ATTESTATION_SECRET,
                native_managed_access_token=LOCAL_NATIVE_MANAGED_ACCESS_TOKEN,
                managed_session_id=LOCAL_ROUTE_HINT_SESSION_REF,
                device_id='localhost-full-chain-device',
                agent_version='localhost-full-chain',
            ),
            execution_controller=controller,
        )
        forwarder.start_background()
        guard_url = assert_loopback_url(f'http://127.0.0.1:{guard_port}', f'{scenario_name}_guard_url')

        run_control_plane_probes(guard_url)
        client_response_path = dirs['scenario_dir'] / 'synthetic-client-response.json'
        client_proc = run_synthetic_client_subprocess(
            guard_url,
            client_response_path,
            fixture,
            linger_after_response=expectation.linger_after_response,
            extra_headers=extra_client_headers,
        )
        controller.register_cli_process(client_proc)

        try:
            client_stdout, client_stderr = client_proc.communicate(timeout=15)
        except subprocess.TimeoutExpired:
            client_proc.kill()
            client_stdout, client_stderr = client_proc.communicate(timeout=5)
        client_stdout_path = dirs['scenario_dir'] / 'synthetic-client.stdout.txt'
        client_stderr_path = dirs['scenario_dir'] / 'synthetic-client.stderr.txt'
        client_stdout_path.write_bytes(client_stdout)
        client_stderr_path.write_bytes(client_stderr)

        if not client_response_path.exists():
            raise RuntimeError(f'{scenario_name}: synthetic client did not initialize response marker')

        for name, handle in reversed(processes):
            stdout, stderr = stop_process(handle.process)
            write_process_capture(dirs['scenario_dir'] / f'{name}.stdout.txt', handle.first_lines, stdout)
            write_process_capture(dirs['scenario_dir'] / f'{name}.stderr.txt', [], stderr)

        cc_summary_path = dirs['cc_dir'] / 'cc_safe_summary.json'
        if not cc_summary_path.exists() and not cc_gateway_unavailable:
            raise RuntimeError(f'{scenario_name}: cc harness did not emit cc_safe_summary.json')

        client_response = load_json_object_or_empty(client_response_path)
        guard_records = load_jsonl(guard_summary_path)
        sub2api_records = load_jsonl(sub_summary_path)
        if cc_summary_path.exists():
            cc_safe_summary = json.loads(cc_summary_path.read_text(encoding='utf-8'))
        else:
            cc_safe_summary = {'mock_request_count': 0, 'mock_requests': [], 'proxy_connect_targets': [], 'profile': {'trusted_egress_profile_ref': 'unavailable'}}
        assert_loopback_connect_targets(cc_safe_summary.get('proxy_connect_targets', []))
        if client_response.get('response_status') is None:
            client_response['response_status_observed'] = False

        scenario_payload = build_scenario_report_payload(
            scenario_name=scenario_name,
            scenario_dir=dirs['scenario_dir'],
            safe_dir=dirs['safe_dir'],
            guard_records=guard_records,
            sub2api_records=sub2api_records,
            cc_safe_summary=cc_safe_summary,
            client_status={
                'response_status': client_response.get('response_status'),
                'response_status_observed': bool(client_response.get('response_status_observed')),
                'returncode': client_proc.returncode,
                'terminated_by_controller': bool(client_proc.returncode not in (None, 0)) and expectation.linger_after_response,
            },
            fixture=fixture,
            scan_result=SensitiveScanResult(status='PENDING', failures=[], scanned_paths=[]),
        )
        scenario_report_json = dirs['safe_dir'] / 'report.json'
        scenario_report_md = dirs['safe_dir'] / 'report.md'
        scenario_report_json.write_text(json.dumps(_sanitize_scenario_payload(scenario_payload), indent=2, sort_keys=True), encoding='utf-8')
        scenario_report_md.write_text(render_scenario_markdown(_sanitize_scenario_payload(scenario_payload)), encoding='utf-8')

        scan_result = perform_sensitive_scan([
            scenario_report_json,
            scenario_report_md,
            guard_summary_path,
            sub_summary_path,
            *( [cc_summary_path] if cc_summary_path.exists() else [] ),
            client_response_path,
            client_stdout_path,
            client_stderr_path,
            *cp4_process_scan_targets(
                dirs['scenario_dir'],
                scenario_name,
                cc_gateway_unavailable=cc_gateway_unavailable,
            ),
        ])
        scenario_payload = build_scenario_report_payload(
            scenario_name=scenario_name,
            scenario_dir=dirs['scenario_dir'],
            safe_dir=dirs['safe_dir'],
            guard_records=guard_records,
            sub2api_records=sub2api_records,
            cc_safe_summary=cc_safe_summary,
            client_status={
                'response_status': client_response.get('response_status'),
                'response_status_observed': bool(client_response.get('response_status_observed')),
                'returncode': client_proc.returncode,
                'terminated_by_controller': bool(client_proc.returncode not in (None, 0)) and expectation.linger_after_response,
            },
            fixture=fixture,
            scan_result=scan_result,
        )
        scenario_payload['status'] = evaluate_scenario_status(scenario_payload, expectation)
        safe_scenario_payload = _sanitize_scenario_payload(scenario_payload)
        scenario_report_json.write_text(json.dumps(safe_scenario_payload, indent=2, sort_keys=True), encoding='utf-8')
        scenario_report_md.write_text(render_scenario_markdown(safe_scenario_payload), encoding='utf-8')
        return safe_scenario_payload
    finally:
        if forwarder is not None:
            forwarder.stop()
        if client_proc is not None and client_proc.poll() is None:
            stop_process(client_proc)
        for _name, handle in reversed(processes):
            if handle.process.poll() is None:
                stop_process(handle.process)


def build_scenario_report_payload(
    *,
    scenario_name: str,
    scenario_dir: Path,
    safe_dir: Path,
    guard_records: Sequence[Mapping[str, Any]],
    sub2api_records: Sequence[Mapping[str, Any]],
    cc_safe_summary: Mapping[str, Any],
    client_status: Mapping[str, Any],
    fixture: Mapping[str, Any],
    scan_result: SensitiveScanResult,
) -> dict[str, Any]:
    mock_request_count = int(cc_safe_summary.get('mock_request_count', 0) or 0)
    controller_stop_requested_events = sum(1 for record in guard_records if record.get('event') == 'execution_controller_stop_requested')
    sub2api_selected_count = sum(1 for record in sub2api_records if record.get('event') == 'sub2api_selected')
    guard_counts = _guard_counts(guard_records)
    return {
        'scenario': scenario_name,
        'status': 'FAIL',
        'run_dir': str(scenario_dir),
        'safe_deliverable_dir': str(safe_dir),
        'real_anthropic_upstream': False,
        'mock_request_count': mock_request_count,
        'controller_stop_requested_events': controller_stop_requested_events,
        'sub2api_selected_count': sub2api_selected_count,
        'cost_envelope_block': guard_counts['messages_cost_envelope_block'] > 0,
        'guard_counts': guard_counts,
        'message_shape': _extract_message_shape(cc_safe_summary, guard_records, fixture),
        'client_status': {
            'response_status': client_status.get('response_status'),
            'response_status_observed': bool(client_status.get('response_status_observed')),
            'returncode': client_status.get('returncode'),
            'terminated_by_controller': bool(client_status.get('terminated_by_controller')),
        },
        'sensitive_scan': scan_result.status,
        'sensitive_scan_failures': list(scan_result.failures),
        'scanned_paths': list(scan_result.scanned_paths),
        'readiness': {
            'status': 'BLOCKED',
            'reason': 'localhost_only_dual_scenario_validation',
        },
        'billing_disposition': _billing_disposition(cc_safe_summary),
        'authority_boundary': _authority_boundary(cc_safe_summary, guard_counts),
        'upstream_safety': _upstream_safety(cc_safe_summary),
        'observed': _observed_summary(cc_safe_summary),
    }



def _billing_disposition(cc_safe_summary: Mapping[str, Any]) -> dict[str, Any]:
    profile = cc_safe_summary.get('profile') if isinstance(cc_safe_summary.get('profile'), Mapping) else {}
    requests = cc_safe_summary.get('mock_requests')
    first = requests[0] if isinstance(requests, list) and requests and isinstance(requests[0], Mapping) else {}
    return {
        'profile_ref': profile.get('trusted_egress_profile_ref'),
        'billing_shape_policy': profile.get('billing_shape_policy'),
        'upstream_has_billing_marker': bool(first.get('has_billing_marker', False)),
        'upstream_has_cch_shape': bool(first.get('has_cch_shape', False)),
    }


def _authority_boundary(cc_safe_summary: Mapping[str, Any], guard_counts: Mapping[str, int]) -> dict[str, Any]:
    profile = cc_safe_summary.get('profile') if isinstance(cc_safe_summary.get('profile'), Mapping) else {}
    return {
        'formal_pool_attested': bool(cc_safe_summary.get('formal_pool_attested', False)),
        'trusted_egress_profile_ref': profile.get('trusted_egress_profile_ref'),
        'guard_forward_messages': int(guard_counts.get('forward_messages', 0) or 0),
    }


def missing_context_authority_boundary(cc_safe_summary: Mapping[str, Any]) -> dict[str, Any]:
    boundary = _authority_boundary(cc_safe_summary, {})
    boundary['formal_pool_attested'] = False
    return boundary


def _upstream_safety(cc_safe_summary: Mapping[str, Any]) -> dict[str, Any]:
    return {
        'real_anthropic_upstream': False,
        'mock_request_count': int(cc_safe_summary.get('mock_request_count', 0) or 0),
        'loopback_proxy_targets_only': True,
    }


def _observed_summary(cc_safe_summary: Mapping[str, Any]) -> dict[str, Any]:
    profile = cc_safe_summary.get('profile') if isinstance(cc_safe_summary.get('profile'), Mapping) else {}
    requests = cc_safe_summary.get('mock_requests')
    first = requests[0] if isinstance(requests, list) and requests and isinstance(requests[0], Mapping) else {}
    version = str(profile.get('observed_cli_version') or '').strip()
    if not version:
        match = re.search(r'\b(\d+\.\d+\.\d+)\b', str(first.get('user_agent') or ''))
        version = match.group(1) if match else 'unknown'
    return {
        'cli_version_bucket': version,
        'route_class': 'messages',
        'billing_shape': 'cch_present' if bool(first.get('has_cch_shape', False)) else ('no_cch' if bool(first.get('has_billing_marker', False)) else 'absent'),
        'safe_summary_only': True,
    }

def evaluate_scenario_status(payload: Mapping[str, Any], expectation: ScenarioExpectation) -> str:
    checks = [
        payload.get('mock_request_count') == expectation.expect_mock_request_count,
        payload.get('cost_envelope_block') == expectation.expect_cost_envelope_block,
        payload.get('controller_stop_requested_events') == expectation.expect_stop_events,
        payload.get('sub2api_selected_count') == expectation.expect_sub2api_selected_count,
        payload.get('client_status', {}).get('response_status') == expectation.expect_response_status,
        payload.get('client_status', {}).get('response_status_observed') is True,
        payload.get('client_status', {}).get('terminated_by_controller') == expectation.expect_terminated_by_controller,
        payload.get('sensitive_scan') == 'PASS',
    ]
    return 'PASS' if all(checks) else 'FAIL'


def render_scenario_markdown(payload: Mapping[str, Any]) -> str:
    lines = [
        f"# {payload.get('scenario')}",
        '',
        f"- status: {payload.get('status')}",
        f"- real_anthropic_upstream: {str(bool(payload.get('real_anthropic_upstream'))).lower()}",
        f"- mock_request_count: {payload.get('mock_request_count')}",
        f"- controller_stop_requested_events: {payload.get('controller_stop_requested_events')}",
        f"- sub2api_selected_count: {payload.get('sub2api_selected_count')}",
        f"- cost_envelope_block: {payload.get('cost_envelope_block')}",
        f"- sensitive_scan: {payload.get('sensitive_scan')}",
        '',
        '## Guard counts',
        '',
    ]
    guard_counts = payload.get('guard_counts', {})
    lines.extend(f'- {key}: {guard_counts[key]}' for key in sorted(guard_counts))
    lines.extend(['', '## Message shape', ''])
    shape = payload.get('message_shape', {})
    lines.extend(f'- {key}: {shape[key]}' for key in sorted(shape))
    return '\n'.join(lines).rstrip() + '\n'



def cp4_fixture_for_scenario(name: str) -> dict[str, Any]:
    if name in {'valid_trusted_context_strip_cch_present', 'default_strip_cch_present_inbound', 'optional_signed_cch_profile_requires_proof'}:
        return build_billing_messages_fixture('cch_present')
    if name == 'observed_2_1_181_strip_cch_present':
        return build_billing_messages_fixture('cch_present', version='2.1.181')
    if name == 'observed_2_1_195_strip_cch_present':
        return build_billing_messages_fixture('cch_present', version='2.1.195')
    if name in {'default_strip_no_cch_inbound', 'optional_no_cch_profile_with_proof'}:
        return build_billing_messages_fixture('no_cch')
    return build_safe_messages_fixture()


def cp4_extra_headers_for_scenario(name: str) -> dict[str, str]:
    if name == 'observed_2_1_181_strip_cch_present':
        return {'user-agent': 'claude-cli/2.1.181 (external, sdk-cli)'}
    if name == 'observed_2_1_195_strip_cch_present':
        return {'user-agent': 'claude-cli/2.1.195 (external, sdk-cli)'}
    if name != 'forged_authority_headers_ignored':
        return {}
    return {
        'x-cc-account-id': 'forged-account-ref',
        'x-cc-egress-bucket': 'forged-egress-bucket',
        'x-sub2api-context-1m': 'true',
        'x-anthropic-billing-header': 'forged-billing-marker',
    }


def cp4_expectation_for_scenario(scenario: CP4FormalPoolScenario) -> ScenarioExpectation:
    expect_stop_events = 0 if scenario.name == 'missing_trusted_context_fail_closed' else 1
    return ScenarioExpectation(
        expect_mock_request_count=scenario.expect_mock_request_count,
        expect_cost_envelope_block=False,
        expect_stop_events=expect_stop_events,
        expect_sub2api_selected_count=0 if scenario.name == 'missing_trusted_context_fail_closed' else 1,
        expect_response_status=scenario.expect_response_status,
        expect_terminated_by_controller=False,
        linger_after_response=False,
    )


def cp4_process_scan_targets(scenario_dir: Path, scenario_name: str, *, cc_gateway_unavailable: bool) -> list[Path]:
    targets = [
        scenario_dir / f'{scenario_name}_sub2api_harness.stdout.txt',
        scenario_dir / f'{scenario_name}_sub2api_harness.stderr.txt',
    ]
    if not cc_gateway_unavailable:
        targets.extend([
            scenario_dir / f'{scenario_name}_cc_harness.stdout.txt',
            scenario_dir / f'{scenario_name}_cc_harness.stderr.txt',
        ])
    return targets


def run_direct_cc_missing_context_scenario(*, run_dir: Path, scenario: CP4FormalPoolScenario, cc_harness_script: Path) -> dict[str, Any]:
    dirs = prepare_scenario_directories(run_dir, scenario.name)
    cc_env = scrub_real_upstream_env({'CC_HARNESS_OUT': str(dirs['cc_dir']), **cp4_profile_env_for_scenario(scenario.name)})
    cc_proc = subprocess.Popen(
        [str(CC_GATEWAY_ROOT / 'node_modules/.bin/tsx'), str(cc_harness_script)],
        cwd=str(CC_GATEWAY_ROOT),
        env=cc_env,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    try:
        cc_state, cc_lines = read_json_line(cc_proc)
        cc_gateway_url = assert_loopback_url(str(cc_state['cc_gateway_url']), f'{scenario.name}_cc_gateway_url')
        body = json.dumps(build_safe_messages_fixture(), separators=(',', ':')).encode('utf-8')
        request = urllib.request.Request(
            cc_gateway_url.rstrip('/') + '/v1/messages?beta=true',
            data=body,
            method='POST',
            headers={
                'content-type': 'application/json',
                'x-cc-gateway-token': 'ccg-token',
                'x-cc-provider': 'anthropic',
                'x-cc-account-id': 'hmac-sha256:' + ('a' * 64),
                'x-cc-egress-bucket': 'bucket-a',
                'x-cc-token-type': 'oauth',
                'x-cc-credential-ref': 'opaque:credential-ref:v1:local-harness-credential',
                'x-cc-policy-version': '2.1.179',
                'authorization': 'Bearer selected-oauth-credential-fixture',
            },
        )
        try:
            with urllib.request.urlopen(request, timeout=10) as response:
                status = response.status
                response.read()
        except urllib.error.HTTPError as exc:
            status = exc.code
            exc.read()
        stdout, stderr = stop_process(cc_proc)
        write_process_capture(dirs['scenario_dir'] / f'{scenario.name}_cc_harness.stdout.txt', cc_lines, stdout)
        write_process_capture(dirs['scenario_dir'] / f'{scenario.name}_cc_harness.stderr.txt', [], stderr)
        cc_summary_path = dirs['cc_dir'] / 'cc_safe_summary.json'
        cc_safe_summary = json.loads(cc_summary_path.read_text(encoding='utf-8')) if cc_summary_path.exists() else {'mock_request_count': 0, 'mock_requests': [], 'proxy_connect_targets': []}
        assert_loopback_connect_targets(cc_safe_summary.get('proxy_connect_targets', []))
        payload = {
            'scenario': scenario.name,
            'status': 'PASS' if status == scenario.expect_response_status and int(cc_safe_summary.get('mock_request_count', 0) or 0) == 0 else 'FAIL',
            'real_anthropic_upstream': False,
            'mock_request_count': int(cc_safe_summary.get('mock_request_count', 0) or 0),
            'client_status': {'response_status': status, 'response_status_observed': True},
            'sensitive_scan': 'PENDING',
            'sensitive_scan_failures': [],
            'billing_disposition': _billing_disposition(cc_safe_summary),
            'authority_boundary': missing_context_authority_boundary(cc_safe_summary),
            'upstream_safety': _upstream_safety(cc_safe_summary),
        }
        report_json = dirs['safe_dir'] / 'report.json'
        report_md = dirs['safe_dir'] / 'report.md'
        report_json.write_text(json.dumps(_sanitize_cp4_scenarios([payload])[scenario.name], indent=2, sort_keys=True), encoding='utf-8')
        report_md.write_text(
            f"# {scenario.name}\n\n"
            f"- status: {payload['status']}\n"
            f"- real_anthropic_upstream: false\n"
            f"- mock_request_count: {payload['mock_request_count']}\n",
            encoding='utf-8',
        )
        scan = perform_sensitive_scan([report_json, report_md, dirs['scenario_dir'] / f'{scenario.name}_cc_harness.stdout.txt', dirs['scenario_dir'] / f'{scenario.name}_cc_harness.stderr.txt'])
        payload['sensitive_scan'] = scan.status
        payload['sensitive_scan_failures'] = list(scan.failures)
        payload['status'] = 'PASS' if payload['status'] == 'PASS' and scan.status == 'PASS' else 'FAIL'
        report_json.write_text(json.dumps(_sanitize_cp4_scenarios([payload])[scenario.name], indent=2, sort_keys=True), encoding='utf-8')
        return _sanitize_cp4_scenarios([payload])[scenario.name]
    finally:
        if cc_proc.poll() is None:
            stop_process(cc_proc)


def run_cp4_formal_pool_scenarios(*, run_dir: Path, cc_harness_script: Path, sub2api_harness_dir: Path) -> list[dict[str, Any]]:
    results: list[dict[str, Any]] = []
    for scenario in CP4_FORMAL_POOL_SCENARIOS:
        if scenario.name == 'missing_trusted_context_fail_closed':
            results.append(run_direct_cc_missing_context_scenario(run_dir=run_dir, scenario=scenario, cc_harness_script=cc_harness_script))
            continue
        payload = run_scenario(
            run_dir=run_dir,
            scenario_name=scenario.name,
            fixture=cp4_fixture_for_scenario(scenario.name),
            expectation=cp4_expectation_for_scenario(scenario),
            cc_harness_script=cc_harness_script,
            sub2api_harness_dir=sub2api_harness_dir,
            extra_env=cp4_profile_env_for_scenario(scenario.name),
            extra_client_headers=cp4_extra_headers_for_scenario(scenario.name),
            cc_gateway_unavailable=scenario.name == 'cc_gateway_unavailable_no_direct_fallback',
        )
        cp4_payload = _sanitize_cp4_scenarios([{**payload, 'scenario': scenario.name}])[scenario.name]
        if scenario.name in {'default_strip_cch_present_inbound', 'valid_trusted_context_strip_cch_present', 'default_strip_no_cch_inbound'}:
            billing = cp4_payload.get('billing_disposition', {})
            if billing.get('upstream_has_billing_marker') or billing.get('upstream_has_cch_shape'):
                cp4_payload['status'] = 'FAIL'
        if scenario.name == 'optional_no_cch_profile_with_proof':
            billing = cp4_payload.get('billing_disposition', {})
            if not billing.get('upstream_has_billing_marker') or billing.get('upstream_has_cch_shape'):
                cp4_payload['status'] = 'FAIL'
        results.append(cp4_payload)
    return results

def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description='Run localhost-only dual-scenario full-chain controller validation without real Claude/Anthropic traffic')
    parser.add_argument('--tmp-root', type=Path, default=Path('/tmp'))
    parser.add_argument('--cc-harness-script', type=Path, default=DEFAULT_CC_HARNESS_SCRIPT)
    parser.add_argument('--sub2api-harness-dir', type=Path, default=DEFAULT_SUB2API_HARNESS_DIR)
    args = parser.parse_args(argv)

    run_dir = create_run_directory(args.tmp_root)
    safe_dir = run_dir / 'safe-deliverable'
    safe_dir.mkdir(mode=0o700, exist_ok=True)
    run_id = run_dir.name

    if not args.cc_harness_script.exists():
        raise FileNotFoundError(f'localhost cc harness script missing: {args.cc_harness_script}')
    if not args.sub2api_harness_dir.exists():
        raise FileNotFoundError(f'localhost sub2api harness dir missing: {args.sub2api_harness_dir}')

    scenario_a = run_scenario(
        run_dir=run_dir,
        scenario_name='scenario_a',
        fixture=build_unsafe_messages_fixture(),
        expectation=ScenarioExpectation(
            expect_mock_request_count=0,
            expect_cost_envelope_block=True,
            expect_stop_events=0,
            expect_sub2api_selected_count=0,
            expect_response_status=422,
            expect_terminated_by_controller=False,
            linger_after_response=False,
        ),
        cc_harness_script=args.cc_harness_script,
        sub2api_harness_dir=args.sub2api_harness_dir,
    )
    scenario_b = run_scenario(
        run_dir=run_dir,
        scenario_name='scenario_b',
        fixture=build_safe_messages_fixture(),
        expectation=ScenarioExpectation(
            expect_mock_request_count=1,
            expect_cost_envelope_block=False,
            expect_stop_events=1,
            expect_sub2api_selected_count=1,
            expect_response_status=200,
            expect_terminated_by_controller=True,
            linger_after_response=True,
        ),
        cc_harness_script=args.cc_harness_script,
        sub2api_harness_dir=args.sub2api_harness_dir,
        extra_env=cp4_profile_env_for_scenario('valid_trusted_context_strip_cch_present'),
    )
    cp4_scenarios = run_cp4_formal_pool_scenarios(run_dir=run_dir, cc_harness_script=args.cc_harness_script, sub2api_harness_dir=args.sub2api_harness_dir)

    report_json = safe_dir / 'report.json'
    report_md = safe_dir / 'report.md'
    pending_payload = build_full_chain_report_payload(
        run_dir=run_dir,
        run_id=run_id,
        scenario_a=scenario_a,
        scenario_b=scenario_b,
        scan_result=SensitiveScanResult(status='PENDING', failures=[], scanned_paths=[]),
        cp4_scenarios=cp4_scenarios,
    )
    report_json.write_text(json.dumps(pending_payload, indent=2, sort_keys=True), encoding='utf-8')
    report_md.write_text(render_report_markdown(pending_payload), encoding='utf-8')

    top_scan = perform_sensitive_scan([
        report_json,
        report_md,
        Path(scenario_a['safe_deliverable_dir']) / 'report.json',
        Path(scenario_a['safe_deliverable_dir']) / 'report.md',
        Path(scenario_b['safe_deliverable_dir']) / 'report.json',
        Path(scenario_b['safe_deliverable_dir']) / 'report.md',
        *[Path(item.get('safe_deliverable_dir', '')) / 'report.json' for item in cp4_scenarios if item.get('safe_deliverable_dir')],
        *[Path(item.get('safe_deliverable_dir', '')) / 'report.md' for item in cp4_scenarios if item.get('safe_deliverable_dir')],
    ])
    final_payload = build_full_chain_report_payload(
        run_dir=run_dir,
        run_id=run_id,
        scenario_a=scenario_a,
        scenario_b=scenario_b,
        scan_result=top_scan,
        cp4_scenarios=cp4_scenarios,
    )
    report_json.write_text(json.dumps(final_payload, indent=2, sort_keys=True), encoding='utf-8')
    report_md.write_text(render_report_markdown(final_payload), encoding='utf-8')

    if final_payload['status'] != 'PASS':
        print(json.dumps({
            'status': 'FAIL',
            'run_dir': str(run_dir),
            'report_json': str(report_json),
            'report_md': str(report_md),
            'scenario_a_status': final_payload['scenario_a']['status'],
            'scenario_b_status': final_payload['scenario_b']['status'],
            'sensitive_scan': final_payload['sensitive_scan'],
            'cp4_scenarios': {key: value.get('status') for key, value in final_payload.get('cp4_scenarios', {}).items()},
        }, sort_keys=True), flush=True)
        return 2

    print(json.dumps({
        'status': 'PASS',
        'run_dir': str(run_dir),
        'report_json': str(report_json),
        'report_md': str(report_md),
        'scenario_a_status': final_payload['scenario_a']['status'],
        'scenario_b_status': final_payload['scenario_b']['status'],
        'scenario_a_mock_request_count': final_payload['scenario_a']['mock_request_count'],
        'scenario_b_mock_request_count': final_payload['scenario_b']['mock_request_count'],
        'scenario_b_controller_stop_requested_events': final_payload['scenario_b']['controller_stop_requested_events'],
        'sensitive_scan': final_payload['sensitive_scan'],
        'cp4_scenarios': {key: value.get('status') for key, value in final_payload.get('cp4_scenarios', {}).items()},
    }, sort_keys=True), flush=True)
    return 0


def _guard_counts(records: Sequence[Mapping[str, Any]]) -> dict[str, int]:
    return {
        'records_count': len(records),
        'forward_messages': sum(1 for record in records if record.get('decision') == 'forward_messages'),
        'messages_gate_block': sum(1 for record in records if record.get('event') == 'messages_gate_block'),
        'messages_cost_envelope_block': sum(1 for record in records if record.get('event') == 'messages_cost_envelope_block'),
        'control_plane_stubbed': sum(1 for record in records if record.get('decision') == 'stub_json'),
        'control_plane_suppressed': sum(1 for record in records if record.get('decision') == 'suppress_204'),
        'control_plane_blocked': sum(1 for record in records if record.get('decision') == 'block_403'),
        'connect_stubbed': sum(1 for record in records if record.get('event') == 'connect_stubbed'),
        'connect_blocked': sum(1 for record in records if record.get('event') == 'connect_blocked'),
    }


def _extract_message_shape(
    cc_safe_summary: Mapping[str, Any],
    guard_records: Sequence[Mapping[str, Any]],
    fixture: Mapping[str, Any],
) -> dict[str, Any]:
    base = dict(message_shape())
    mock_requests = cc_safe_summary.get('mock_requests')
    request: Mapping[str, Any] | None = None
    if isinstance(mock_requests, list) and mock_requests:
        first = mock_requests[0]
        if isinstance(first, Mapping):
            request = first

    if request is not None:
        base['model'] = request.get('model', base['model'])
        base['user_agent'] = request.get('user_agent', base['user_agent'])
        base['body_keys'] = sorted(request.get('body_keys', base['body_keys']))
        base['body_size'] = request.get('body_size', base['body_size'])
        base['max_tokens'] = request.get('max_tokens', base['max_tokens'])
        base['tools_count'] = request.get('tools_count', base['tools_count'])
        base['output_config_keys'] = sorted(request.get('output_config_keys', base['output_config_keys']))
        base['context_1m'] = bool(request.get('beta_contains_context_1m', base['context_1m']))
        base['session_uuid_like'] = bool(request.get('session_uuid_like', False))
        url = str(request.get('url', ''))
        base['beta'] = urlsplit(url).query == 'beta=true'
    else:
        base['model'] = fixture.get('model', base['model'])
        base['body_keys'] = sorted(str(key) for key in fixture.keys())
        base['body_size'] = len(json.dumps(dict(fixture), separators=(',', ':')).encode('utf-8'))
        base['max_tokens'] = fixture.get('max_tokens', base['max_tokens'])
        tools = fixture.get('tools')
        base['tools_count'] = len(tools) if isinstance(tools, list) else 0
        output_config = fixture.get('output_config')
        base['output_config_keys'] = sorted(str(key) for key in output_config.keys()) if isinstance(output_config, dict) else []
        base['beta'] = True
        base['session_uuid_like'] = False
    messages = fixture.get('messages')
    if isinstance(messages, list):
        base['messages_count'] = len(messages)
    base['thinking_present'] = 'thinking' in fixture
    base['context_management_present'] = 'context_management' in fixture
    base['retry_count'] = sum(1 for record in guard_records if record.get('event') == 'messages_upstream_error')
    base['error_count'] = base['retry_count']
    base['extra_message_count'] = max(int(cc_safe_summary.get('mock_request_count', 0) or 0) - 1, 0)
    return {key: base[key] for key in message_shape().keys()}


def _sanitize_cp4_scenarios(scenarios: Sequence[Mapping[str, Any]]) -> dict[str, dict[str, Any]]:
    safe: dict[str, dict[str, Any]] = {}
    for item in scenarios:
        name = str(item.get('scenario') or '')
        if not name:
            continue
        safe[name] = {key: item[key] for key in CP4_SCENARIO_SAFE_KEYS if key in item}
        safe[name]['real_anthropic_upstream'] = bool(safe[name].get('real_anthropic_upstream', False))
    return safe

def _sanitize_scenario_payload(payload: Mapping[str, Any]) -> dict[str, Any]:
    return {key: payload[key] for key in SAFE_SCENARIO_KEYS if key in payload}


if __name__ == '__main__':
    raise SystemExit(main())
