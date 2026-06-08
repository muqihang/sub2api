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


WORKTREE = Path(__file__).resolve().parents[1]
CC_GATEWAY_ROOT = Path('/Users/muqihang/chelingxi_workspace/cc-gateway')
DEFAULT_CC_HARNESS_SCRIPT = WORKTREE / 'tools/cc_gateway_localhost_harness.mjs'
DEFAULT_SUB2API_HARNESS_DIR = WORKTREE / 'backend/.tmp-harness/cli-through-sub2api'
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
    'authorization_header': re.compile(r'Authorization:\s*Bearer\s+\S+', re.IGNORECASE),
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
) -> dict[str, Any]:
    safe_a = _sanitize_scenario_payload(scenario_a)
    safe_b = _sanitize_scenario_payload(scenario_b)
    status = 'PASS' if safe_a.get('status') == 'PASS' and safe_b.get('status') == 'PASS' and scan_result.status == 'PASS' else 'FAIL'
    return {
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
    raise RuntimeError(f'expected JSON line from process, got {lines[-5:]}')


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
) -> subprocess.Popen[bytes]:
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
    headers={
        'content-type': 'application/json',
        'user-agent': 'synthetic-localhost-canary/1.0',
        'anthropic-beta': 'oauth-2025-04-20',
    },
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
) -> dict[str, Any]:
    dirs = prepare_scenario_directories(run_dir, scenario_name)
    processes: list[tuple[str, ProcessHandle]] = []
    forwarder: RedactingForwarder | None = None
    client_proc: subprocess.Popen[bytes] | None = None

    try:
        cc_env = scrub_real_upstream_env({'CC_HARNESS_OUT': str(dirs['cc_dir'])})
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
        sub_env = scrub_real_upstream_env({
            'CC_GATEWAY_URL': cc_gateway_url,
            'SUB2API_HARNESS_SUMMARY': str(sub_summary_path),
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
        if not cc_summary_path.exists():
            raise RuntimeError(f'{scenario_name}: cc harness did not emit cc_safe_summary.json')

        client_response = load_json_object_or_empty(client_response_path)
        guard_records = load_jsonl(guard_summary_path)
        sub2api_records = load_jsonl(sub_summary_path)
        cc_safe_summary = json.loads(cc_summary_path.read_text(encoding='utf-8'))
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
            cc_summary_path,
            client_response_path,
            client_stdout_path,
            client_stderr_path,
            dirs['scenario_dir'] / f'{scenario_name}_cc_harness.stdout.txt',
            dirs['scenario_dir'] / f'{scenario_name}_cc_harness.stderr.txt',
            dirs['scenario_dir'] / f'{scenario_name}_sub2api_harness.stdout.txt',
            dirs['scenario_dir'] / f'{scenario_name}_sub2api_harness.stderr.txt',
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
    )

    report_json = safe_dir / 'report.json'
    report_md = safe_dir / 'report.md'
    pending_payload = build_full_chain_report_payload(
        run_dir=run_dir,
        run_id=run_id,
        scenario_a=scenario_a,
        scenario_b=scenario_b,
        scan_result=SensitiveScanResult(status='PENDING', failures=[], scanned_paths=[]),
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
    ])
    final_payload = build_full_chain_report_payload(
        run_dir=run_dir,
        run_id=run_id,
        scenario_a=scenario_a,
        scenario_b=scenario_b,
        scan_result=top_scan,
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


def _sanitize_scenario_payload(payload: Mapping[str, Any]) -> dict[str, Any]:
    return {key: payload[key] for key in SAFE_SCENARIO_KEYS if key in payload}


if __name__ == '__main__':
    raise SystemExit(main())
