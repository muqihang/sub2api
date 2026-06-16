import argparse
import copy
import json
import socket
import subprocess
import sys
import tempfile
import threading
import time
import unittest
import unittest.mock
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

from tools.cli_control_plane_guard import (
    ExecutionController,
    GuardConfig,
    RedactingForwarder,
    _cli_session_budget_ledger,
    body_summary,
    build_native_messages_attestation_headers,
    classify_request,
    deep_body_summary,
    redact_headers,
)
from tools.cli_control_plane_policy import load_default_policy


class CliControlPlaneGuardTest(unittest.TestCase):
    def setUp(self):
        self._native_secret_patch = unittest.mock.patch.dict(
            'os.environ',
            {'SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET': 'native-attestation-test-secret'},
            clear=False,
        )
        self._native_secret_patch.start()

    def tearDown(self):
        self._native_secret_patch.stop()


    def test_forward_messages_adds_managed_device_headers(self):
        seen = {}

        class Upstream(BaseHTTPRequestHandler):
            def do_POST(self):
                _ = self.rfile.read(int(self.headers.get('content-length', '0')))
                seen['authorization'] = self.headers.get('Authorization')
                seen['managed_session'] = self.headers.get('X-Zhumeng-Managed-Session')
                seen['device_id'] = self.headers.get('X-Zhumeng-Device-ID')
                self.send_response(200)
                self.send_header('content-type', 'application/json')
                self.end_headers()
                self.wfile.write(b'{"ok":true}')

            def log_message(self, *args):
                pass

        upstream_port = _free_port()
        upstream = ThreadingHTTPServer(('127.0.0.1', upstream_port), Upstream)
        upstream_thread = threading.Thread(target=upstream.serve_forever, daemon=True)
        upstream_thread.start()
        with tempfile.TemporaryDirectory() as td:
            listen_port = _free_port()
            forwarder = RedactingForwarder(GuardConfig(
                listen_host='127.0.0.1',
                listen_port=listen_port,
                upstream_base=f'http://127.0.0.1:{upstream_port}',
                sub2api_auth='managed-access-token',
                summary_path=Path(td) / 'summary.jsonl',
                native_attestation_secret='native-attestation-test-secret',
                managed_session_id='managed-session',
                device_id='9',
                agent_version='0.1.0',
            ))
            forwarder.start_background()
            try:
                request = urllib.request.Request(
                    f'http://127.0.0.1:{listen_port}/v1/messages?beta=true',
                    data=b'{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}],"max_tokens":16}',
                    method='POST',
                    headers={'content-type': 'application/json', 'User-Agent': 'claude-cli/2.1.150 (external, sdk-cli)'},
                )
                with urllib.request.urlopen(request, timeout=5) as resp:
                    self.assertEqual(resp.status, 200)
            finally:
                forwarder.stop()
                upstream.shutdown()
                upstream.server_close()

        self.assertEqual(seen['authorization'], 'Bearer managed-access-token')
        self.assertEqual(seen['managed_session'], 'managed-session')
        self.assertEqual(seen['device_id'], '9')

    def test_forward_messages_uses_strict_header_allowlist(self):
        seen = {}

        class Upstream(BaseHTTPRequestHandler):
            def do_POST(self):
                _ = self.rfile.read(int(self.headers.get('content-length', '0')))
                seen['headers'] = {key.lower(): value for key, value in self.headers.items()}
                self.send_response(200)
                self.send_header('content-type', 'application/json')
                self.end_headers()
                self.wfile.write(b'{"ok":true}')

            def log_message(self, *args):
                pass

        upstream_port = _free_port()
        upstream = ThreadingHTTPServer(('127.0.0.1', upstream_port), Upstream)
        upstream_thread = threading.Thread(target=upstream.serve_forever, daemon=True)
        upstream_thread.start()
        with tempfile.TemporaryDirectory() as td:
            listen_port = _free_port()
            forwarder = RedactingForwarder(GuardConfig(
                listen_host='127.0.0.1',
                listen_port=listen_port,
                upstream_base=f'http://127.0.0.1:{upstream_port}',
                sub2api_auth='managed-access-token',
                summary_path=Path(td) / 'summary.jsonl',
                native_attestation_secret='native-attestation-test-secret',
                managed_session_id='managed-session',
                device_id='9',
                agent_version='0.1.0',
            ))
            forwarder.start_background()
            try:
                request = urllib.request.Request(
                    f'http://127.0.0.1:{listen_port}/v1/messages?beta=true',
                    data=b'{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}],"max_tokens":16}',
                    method='POST',
                    headers={
                        'content-type': 'application/json',
                        'User-Agent': 'claude-cli/2.1.150 (external, sdk-cli)',
                        'anthropic-version': '2023-06-01',
                        'x-stainless-lang': 'js',
                        'x-access-token': 'local-token-leak',
                        'x-prompt': 'local-prompt-leak',
                        'x-claude-code-session-id': '11111111-2222-4333-8444-555555555555',
                        'x-random-local-debug': 'debug-leak',
                    },
                )
                with urllib.request.urlopen(request, timeout=5) as resp:
                    self.assertEqual(resp.status, 200)
            finally:
                forwarder.stop()
                upstream.shutdown()
                upstream.server_close()

        headers = seen['headers']
        self.assertEqual(headers['authorization'], 'Bearer managed-access-token')
        self.assertEqual(headers['user-agent'], 'claude-cli/2.1.150 (external, sdk-cli)')
        self.assertEqual(headers['anthropic-version'], '2023-06-01')
        self.assertEqual(headers['x-stainless-lang'], 'js')
        self.assertNotIn('x-access-token', headers)
        self.assertNotIn('x-prompt', headers)
        self.assertNotIn('x-claude-code-session-id', headers)
        self.assertNotIn('x-random-local-debug', headers)

    def test_extra_forward_headers_are_also_allowlisted(self):
        seen = {}

        class Upstream(BaseHTTPRequestHandler):
            def do_POST(self):
                _ = self.rfile.read(int(self.headers.get('content-length', '0')))
                seen['headers'] = {key.lower(): value for key, value in self.headers.items()}
                self.send_response(200)
                self.send_header('content-type', 'application/json')
                self.end_headers()
                self.wfile.write(b'{"ok":true}')

            def log_message(self, *args):
                pass

        upstream_port = _free_port()
        upstream = ThreadingHTTPServer(('127.0.0.1', upstream_port), Upstream)
        upstream_thread = threading.Thread(target=upstream.serve_forever, daemon=True)
        upstream_thread.start()
        with tempfile.TemporaryDirectory() as td:
            listen_port = _free_port()
            forwarder = RedactingForwarder(GuardConfig(
                listen_host='127.0.0.1',
                listen_port=listen_port,
                upstream_base=f'http://127.0.0.1:{upstream_port}',
                sub2api_auth='managed-access-token',
                summary_path=Path(td) / 'summary.jsonl',
                native_attestation_secret='native-attestation-test-secret',
                extra_forward_headers={
                    'anthropic-version': '2023-06-01',
                    'x-prompt': 'extra-prompt-leak',
                    'x-access-token': 'extra-token-leak',
                    'x-random-local-debug': 'extra-debug-leak',
                },
            ))
            forwarder.start_background()
            try:
                request = urllib.request.Request(
                    f'http://127.0.0.1:{listen_port}/v1/messages?beta=true',
                    data=b'{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}],"max_tokens":16}',
                    method='POST',
                    headers={'content-type': 'application/json', 'User-Agent': 'claude-cli/2.1.150 (external, sdk-cli)'},
                )
                with urllib.request.urlopen(request, timeout=5) as resp:
                    self.assertEqual(resp.status, 200)
            finally:
                forwarder.stop()
                upstream.shutdown()
                upstream.server_close()

        headers = seen['headers']
        self.assertEqual(headers['anthropic-version'], '2023-06-01')
        self.assertNotIn('x-prompt', headers)
        self.assertNotIn('x-access-token', headers)
        self.assertNotIn('x-random-local-debug', headers)

    def test_cli_main_does_not_enforce_session_budget_by_default(self):
        args = argparse.Namespace(
            enforce_session_budget=False,
            disable_session_budget=False,
            session_budget_max_messages=7,
            session_budget_max_rich_messages=None,
            session_budget_max_body_bytes=None,
            session_budget_max_tool_def_bytes=None,
            session_budget_max_thinking_messages=None,
        )
        self.assertIsNone(_cli_session_budget_ledger(args))

    def test_cli_main_builds_session_budget_ledger_only_when_explicitly_enforced(self):
        args = argparse.Namespace(
            enforce_session_budget=True,
            disable_session_budget=False,
            session_budget_max_messages=7,
            session_budget_max_rich_messages=None,
            session_budget_max_body_bytes=None,
            session_budget_max_tool_def_bytes=None,
            session_budget_max_thinking_messages=None,
        )
        ledger = _cli_session_budget_ledger(args)
        self.assertIsNotNone(ledger)
        self.assertEqual(ledger.policy.max_messages_per_session, 7)

    def test_cli_main_can_disable_session_budget_explicitly(self):
        args = argparse.Namespace(disable_session_budget=True, enforce_session_budget=True)
        self.assertIsNone(_cli_session_budget_ledger(args))

    def test_native_messages_attestation_requires_explicit_secret(self):
        with unittest.mock.patch.dict(
            'os.environ',
            {
                'SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET': '',
                'SUB2API_CONTROL_PLANE_ATTESTATION_SECRET': '',
            },
            clear=False,
        ):
            with self.assertRaisesRegex(RuntimeError, 'explicit'):
                build_native_messages_attestation_headers(b'{"messages":[]}', '/v1/messages', {})

    def test_classify_request_uses_default_policy_and_quarantines_unknown_routes(self):
        self.assertEqual(classify_request('POST', '/v1/messages?beta=true').action, 'forward_messages')
        self.assertEqual(classify_request('POST', '/api/event_logging/v2/batch').action, 'suppress_204')
        self.assertEqual(classify_request('GET', '/v1/mcp_servers?limit=1000').action, 'stub_json')
        self.assertEqual(classify_request('GET', '/totally-unknown').action, 'quarantine_block')

    def test_redaction_keeps_auth_shape_not_value(self):
        headers = {
            'Authorization': 'Bearer secret-token-value',
            'x-api-key': 'secret-api-key',
            'Cookie': 'a=b',
            'Proxy-Authorization': 'Basic proxy-credential-marker',
            'User-Agent': 'claude-cli/2.1.150 (external, sdk-cli)',
            'anthropic-beta': 'oauth-2025-04-20',
        }
        redacted = redact_headers(headers)
        self.assertEqual(
            redacted['auth_shape'],
            {
                'authorization': 'Bearer',
                'x-api-key': 'present',
                'cookie': 'present',
                'proxy-authorization': 'present',
            },
        )
        dumped = json.dumps(redacted)
        self.assertNotIn('secret-token-value', dumped)
        self.assertNotIn('secret-api-key', dumped)
        self.assertNotIn('proxy-credential-marker', dumped)
        self.assertTrue(redacted['selected']['user-agent'].startswith('claude-cli/'))

    def test_redact_headers_sanitizes_sensitive_header_names(self):
        redacted = redact_headers({
            'x-secret-token-marker': '1',
            'x-raw-prompt-marker': '1',
            'User-Agent': 'claude-cli/2.1.150 (external, sdk-cli)',
        })
        dumped = json.dumps(redacted, sort_keys=True)
        self.assertIn('user-agent', dumped)
        self.assertNotIn('x-secret-token-marker', dumped)
        self.assertNotIn('x-raw-prompt-marker', dumped)

    def test_redact_headers_sanitizes_sensitive_selected_header_values(self):
        redacted = redact_headers({
            'User-Agent': 'claude-cli secret-token-marker',
            'anthropic-beta': 'raw-prompt-marker',
        })
        dumped = json.dumps(redacted, sort_keys=True)
        self.assertNotIn('secret-token-marker', dumped)
        self.assertNotIn('raw-prompt-marker', dumped)
        self.assertEqual(redacted['selected']['user-agent'], 'redacted-header-value')
        self.assertEqual(redacted['selected']['anthropic-beta'], 'redacted-header-value')

    def test_body_summary_redacts_sensitive_model_and_key_names(self):
        summary = body_summary(json.dumps({
            'model': 'secret-token-marker',
            'secret-token-marker': 'x',
            'messages': [{'role': 'user', 'content': 'raw-prompt-marker'}],
            'max_tokens': 1,
        }).encode('utf-8'))
        dumped = json.dumps(summary, sort_keys=True)
        self.assertIn('body_keys', summary)
        self.assertNotIn('secret-token-marker', dumped)
        self.assertNotIn('raw-prompt-marker', dumped)

    def test_body_summary_sanitizes_non_integer_max_tokens(self):
        summary = body_summary(json.dumps({
            'model': 'claude-sonnet-4-6',
            'max_tokens': 'secret-token-marker',
        }).encode('utf-8'))
        dumped = json.dumps(summary, sort_keys=True)
        self.assertNotIn('secret-token-marker', dumped)
        self.assertEqual(summary['max_tokens'], 'redacted-non-int')

    def test_deep_body_summary_records_shape_without_string_values(self):
        summary = deep_body_summary(json.dumps({
            'model': 'claude-sonnet-4-6',
            'messages': [{'role': 'user', 'content': 'raw-prompt-marker'}],
            'events': [{'event_type': 'ClaudeCodeInternalEvent', 'event_name': 'tengu_api_success'}],
        }).encode('utf-8'))
        dumped = json.dumps(summary, sort_keys=True)
        self.assertIn('json_tree', dumped)
        self.assertIn('tengu_api_success', dumped)
        self.assertNotIn('raw-prompt-marker', dumped)

    def test_explicit_policy_on_guard_config_controls_local_stub_response(self):
        policy_dict = load_default_policy()._source_dict
        custom_dict = copy.deepcopy(policy_dict)
        custom_dict['control_plane']['mcp']['response']['body'] = {'servers': ['from-policy']}
        policy = load_default_policy().from_dict(custom_dict)

        with tempfile.TemporaryDirectory() as td:
            listen_port = _free_port()
            forwarder = RedactingForwarder(GuardConfig(
                listen_host='127.0.0.1',
                listen_port=listen_port,
                upstream_base='http://127.0.0.1:9',
                sub2api_auth='unused',
                summary_path=Path(td) / 'summary.jsonl',
                policy=policy,
            ))
            forwarder.start_background()
            try:
                with urllib.request.urlopen(
                    f'http://127.0.0.1:{listen_port}/v1/mcp_servers?limit=1000',
                    timeout=5,
                ) as resp:
                    body = json.loads(resp.read().decode('utf-8'))
                self.assertEqual(body, {'servers': ['from-policy']})
            finally:
                forwarder.stop()

    def test_api_hello_preflight_is_stubbed_locally(self):
        with tempfile.TemporaryDirectory() as td:
            listen_port = _free_port()
            summary = Path(td) / 'summary.jsonl'
            forwarder = RedactingForwarder(GuardConfig(
                listen_host='127.0.0.1',
                listen_port=listen_port,
                upstream_base='http://127.0.0.1:9',
                sub2api_auth='unused',
                summary_path=summary,
            ))
            forwarder.start_background()
            try:
                with urllib.request.urlopen(
                    f'http://127.0.0.1:{listen_port}/api/hello',
                    timeout=5,
                ) as resp:
                    self.assertEqual(resp.status, 200)
                dumped = summary.read_text(encoding='utf-8')
                self.assertIn('/api/hello', dumped)
                self.assertIn('stub_json', dumped)
            finally:
                forwarder.stop()

    def test_oauth_hello_preflight_is_stubbed_locally(self):
        with tempfile.TemporaryDirectory() as td:
            listen_port = _free_port()
            summary = Path(td) / 'summary.jsonl'
            forwarder = RedactingForwarder(GuardConfig(
                listen_host='127.0.0.1',
                listen_port=listen_port,
                upstream_base='http://127.0.0.1:9',
                sub2api_auth='unused',
                summary_path=summary,
            ))
            forwarder.start_background()
            try:
                with urllib.request.urlopen(
                    f'http://127.0.0.1:{listen_port}/v1/oauth/hello',
                    timeout=5,
                ) as resp:
                    self.assertEqual(resp.status, 200)
                dumped = summary.read_text(encoding='utf-8')
                self.assertIn('/v1/oauth/hello', dumped)
                self.assertIn('stub_json', dumped)
            finally:
                forwarder.stop()

    def test_cli_policy_path_invalid_config_exits_nonzero_before_serving(self):
        with tempfile.TemporaryDirectory() as td:
            policy_path = Path(td) / 'policy.json'
            summary_path = Path(td) / 'summary.jsonl'
            policy_path.write_text(json.dumps({'schema_version': 1}), encoding='utf-8')
            proc = subprocess.run(
                [
                    sys.executable,
                    '-m',
                    'tools.cli_control_plane_guard',
                    '--listen-port',
                    str(_free_port()),
                    '--upstream-base',
                    'http://127.0.0.1:9',
                    '--sub2api-auth',
                    'sub2api-entry',
                    '--summary-path',
                    str(summary_path),
                    '--policy-path',
                    str(policy_path),
                ],
                cwd=root_dir(),
                capture_output=True,
                text=True,
                timeout=10,
            )
            self.assertNotEqual(proc.returncode, 0)
            self.assertFalse(summary_path.exists())
            self.assertNotIn('"listen"', proc.stdout)

    def test_control_plane_summary_uses_safe_path_templates_and_omission_fields(self):
        with tempfile.TemporaryDirectory() as td:
            listen_port = _free_port()
            summary = Path(td) / 'summary.jsonl'
            forwarder = RedactingForwarder(GuardConfig(
                listen_host='127.0.0.1',
                listen_port=listen_port,
                upstream_base='http://127.0.0.1:9',
                sub2api_auth='unused',
                summary_path=summary,
            ))
            forwarder.start_background()
            try:
                req = urllib.request.Request(
                    f'http://127.0.0.1:{listen_port}/api/oauth/organizations/local-org-secret/referral/eligibility',
                    method='GET',
                    headers={
                        'Authorization': 'Bearer secret-token-marker',
                        'Cookie': 'session=cookie-marker',
                    },
                )
                with self.assertRaises(urllib.error.HTTPError) as ctx:
                    urllib.request.urlopen(req, timeout=5)
                self.assertEqual(ctx.exception.code, 403)
                dumped = summary.read_text(encoding='utf-8')
                self.assertIn('/api/oauth/organizations/{org}/referral/eligibility', dumped)
                self.assertIn('query_omitted_reason', dumped)
                self.assertIn('body_length_bucket', dumped)
                self.assertIn('transport_summary', dumped)
                self.assertIn('auth_shape', dumped)
                self.assertNotIn('local-org-secret', dumped)
                self.assertNotIn('secret-token-marker', dumped)
                self.assertNotIn('cookie-marker', dumped)
                self.assertNotIn('query_hash', dumped)
                self.assertNotIn('body_hash', dumped)
            finally:
                forwarder.stop()

    def test_future_upload_defaults_remain_disabled_noop_and_never_forward(self):
        class CaptureHandler(BaseHTTPRequestHandler):
            count = 0

            def log_message(self, *args):
                pass

            def do_GET(self):
                self.__class__.count += 1
                self.send_response(500)
                self.send_header('content-length', '0')
                self.end_headers()

        upstream_port = _free_port()
        upstream = ThreadingHTTPServer(('127.0.0.1', upstream_port), CaptureHandler)
        threading.Thread(target=upstream.serve_forever, daemon=True).start()
        try:
            with tempfile.TemporaryDirectory() as td:
                listen_port = _free_port()
                forwarder = RedactingForwarder(GuardConfig(
                    listen_host='127.0.0.1',
                    listen_port=listen_port,
                    upstream_base=f'http://127.0.0.1:{upstream_port}',
                    sub2api_auth='unused',
                    summary_path=Path(td) / 'summary.jsonl',
                ))
                forwarder.start_background()
                try:
                    with urllib.request.urlopen(
                        f'http://127.0.0.1:{listen_port}/api/claude_cli/bootstrap?entrypoint=sdk-cli',
                        timeout=5,
                    ) as resp:
                        self.assertEqual(resp.status, 200)
                    self.assertEqual(CaptureHandler.count, 0)
                finally:
                    forwarder.stop()
        finally:
            upstream.shutdown()

    def test_messages_forward_without_native_attestation_secret_fails_closed(self):
        class CaptureHandler(BaseHTTPRequestHandler):
            count = 0

            def log_message(self, *args):
                pass

            def do_POST(self):
                self.__class__.count += 1
                self.send_response(200)
                self.send_header('content-length', '0')
                self.end_headers()

        upstream_port = _free_port()
        upstream = ThreadingHTTPServer(('127.0.0.1', upstream_port), CaptureHandler)
        threading.Thread(target=upstream.serve_forever, daemon=True).start()
        try:
            with unittest.mock.patch.dict('os.environ', {'SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET': ''}, clear=False):
                with tempfile.TemporaryDirectory() as td:
                    listen_port = _free_port()
                    summary = Path(td) / 'summary.jsonl'
                    forwarder = RedactingForwarder(GuardConfig(
                        listen_host='127.0.0.1',
                        listen_port=listen_port,
                        upstream_base=f'http://127.0.0.1:{upstream_port}',
                        sub2api_auth='sub2api-entry-key',
                        summary_path=summary,
                        native_attestation_secret=None,
                    ))
                    forwarder.start_background()
                    try:
                        req = urllib.request.Request(
                            f'http://127.0.0.1:{listen_port}/v1/messages?beta=true',
                            data=b'{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}],"max_tokens":16}',
                            method='POST',
                            headers={'content-type': 'application/json'},
                        )
                        with self.assertRaises(urllib.error.HTTPError) as ctx:
                            urllib.request.urlopen(req, timeout=5)
                        self.assertEqual(ctx.exception.code, 403)
                        self.assertEqual(CaptureHandler.count, 0)
                        dumped = summary.read_text(encoding='utf-8')
                        self.assertIn('native_attestation_unavailable', dumped)
                        self.assertNotIn('native-attestation-test-secret', dumped)
                    finally:
                        forwarder.stop()
        finally:
            upstream.shutdown()

    def test_forwarder_strips_local_auth_and_preserves_safe_messages_summary(self):
        class CaptureHandler(BaseHTTPRequestHandler):
            requests = []

            def log_message(self, *args):
                pass

            def do_POST(self):
                n = int(self.headers.get('content-length', '0') or 0)
                body = self.rfile.read(n)
                self.__class__.requests.append({
                    'path': self.path,
                    'headers': {key.lower(): value for key, value in self.headers.items()},
                    'body': body.decode('utf-8'),
                })
                data = b'{"ok":true}'
                self.send_response(200)
                self.send_header('content-type', 'application/json')
                self.send_header('content-length', str(len(data)))
                self.end_headers()
                self.wfile.write(data)

        upstream_port = _free_port()
        upstream = ThreadingHTTPServer(('127.0.0.1', upstream_port), CaptureHandler)
        threading.Thread(target=upstream.serve_forever, daemon=True).start()
        try:
            with tempfile.TemporaryDirectory() as td:
                listen_port = _free_port()
                summary = Path(td) / 'summary.jsonl'
                forwarder = RedactingForwarder(GuardConfig(
                    listen_host='127.0.0.1',
                    listen_port=listen_port,
                    upstream_base=f'http://127.0.0.1:{upstream_port}',
                    sub2api_auth='sub2api-entry-key',
                    summary_path=summary,
                    capture_level='deep',
                ))
                forwarder.start_background()
                try:
                    req = urllib.request.Request(
                        f'http://127.0.0.1:{listen_port}/v1/messages?beta=true',
                        data=json.dumps({
                            'model': 'claude-sonnet-4-6',
                            'messages': [{'role': 'user', 'content': 'raw-prompt-marker'}],
                            'max_tokens': 64,
                            'tools': [{'name': 'calculator'}],
                            'output_config': {'format': 'json'},
                        }).encode('utf-8'),
                        method='POST',
                        headers={
                            'Authorization': 'Bearer secret-token-marker',
                            'x-api-key': 'local-api-key-marker',
                            'Cookie': 'session=cookie-marker',
                            'Proxy-Authorization': 'Basic proxy-credential-marker',
                            'content-type': 'application/json',
                        },
                    )
                    with urllib.request.urlopen(req, timeout=5) as resp:
                        self.assertEqual(resp.status, 200)
                    self.assertEqual(len(CaptureHandler.requests), 1)
                    forwarded = CaptureHandler.requests[0]
                    self.assertEqual(forwarded['headers'].get('authorization'), 'Bearer sub2api-entry-key')
                    self.assertNotIn('x-api-key', forwarded['headers'])
                    self.assertNotIn('cookie', forwarded['headers'])
                    self.assertNotIn('proxy-authorization', forwarded['headers'])
                    dumped = summary.read_text(encoding='utf-8')
                    self.assertIn('auth_shape', dumped)
                    self.assertIn('body_keys', dumped)
                    self.assertIn('messages_upstream_response', dumped)
                    self.assertIn('"status": 200', dumped)
                    self.assertIn('deep_body_summary', dumped)
                    self.assertIn('response_deep_body_summary', dumped)
                    self.assertNotIn('raw-prompt-marker', dumped)
                    self.assertNotIn('secret-token-marker', dumped)
                    self.assertNotIn('local-api-key-marker', dumped)
                    self.assertNotIn('cookie-marker', dumped)
                    self.assertNotIn('proxy-credential-marker', dumped)
                finally:
                    forwarder.stop()
        finally:
            upstream.shutdown()

    def test_local_raw_capture_writes_redacted_artifact_only(self):
        with tempfile.TemporaryDirectory() as td:
            listen_port = _free_port()
            summary = Path(td) / 'summary.jsonl'
            raw_dir = Path(td) / 'raw-secure'
            forwarder = RedactingForwarder(GuardConfig(
                listen_host='127.0.0.1',
                listen_port=listen_port,
                upstream_base='http://127.0.0.1:9',
                sub2api_auth='unused',
                summary_path=summary,
                capture_level='local-raw',
                local_raw_dir=raw_dir,
            ))
            forwarder.start_background()
            try:
                req = urllib.request.Request(
                    f'http://127.0.0.1:{listen_port}/api/event_logging/v2/batch',
                    data=json.dumps({
                        'events': [{
                            'event_type': 'ClaudeCodeInternalEvent',
                            'event_name': 'tengu_api_success',
                            'secret-token-marker': 'must-not-persist',
                            'content': 'raw-prompt-marker',
                        }]
                    }).encode('utf-8'),
                    method='POST',
                    headers={
                        'Authorization': 'Bearer secret-token-marker',
                        'content-type': 'application/json',
                    },
                )
                with urllib.request.urlopen(req, timeout=5) as resp:
                    self.assertEqual(resp.status, 204)
                artifacts = list(raw_dir.glob('*.json'))
                self.assertEqual(len(artifacts), 1)
                dumped = artifacts[0].read_text(encoding='utf-8')
                self.assertIn('tengu_api_success', dumped)
                self.assertIn('ClaudeCodeInternalEvent', dumped)
                self.assertNotIn('secret-token-marker', dumped)
                self.assertNotIn('must-not-persist', dumped)
                self.assertNotIn('raw-prompt-marker', dumped)
                self.assertIn('local_raw_ref', summary.read_text(encoding='utf-8'))
            finally:
                forwarder.stop()

    def test_connect_stub_blocks_direct_messages_inside_tls(self):
        with tempfile.TemporaryDirectory() as td:
            listen_port = _free_port()
            summary = Path(td) / 'summary.jsonl'
            cert = Path(td) / 'api.anthropic.com.pem'
            key = Path(td) / 'api.anthropic.com.key'
            forwarder = RedactingForwarder(GuardConfig(
                listen_host='127.0.0.1',
                listen_port=listen_port,
                upstream_base='http://127.0.0.1:9',
                sub2api_auth='unused',
                summary_path=summary,
                connect_mode='stub',
                cert_path=cert,
                key_path=key,
            ))
            forwarder.start_background()
            try:
                import ssl
                sock = socket.create_connection(('127.0.0.1', listen_port), timeout=5)
                sock.sendall(b'CONNECT api.anthropic.com:443 HTTP/1.1\r\nHost: api.anthropic.com:443\r\n\r\n')
                self.assertIn(b'200 Connection Established', sock.recv(4096))
                _wait_for_file(cert)
                ctx = ssl.create_default_context(cafile=str(cert))
                with ctx.wrap_socket(sock, server_hostname='api.anthropic.com') as tls:
                    tls.sendall(
                        b'POST /v1/messages?beta=true HTTP/1.1\r\n'
                        b'Host: api.anthropic.com\r\n'
                        b'Content-Length: 2\r\n\r\n{}'
                    )
                    data = tls.recv(4096)
                self.assertIn(b'403 Forbidden', data)
                dumped = summary.read_text(encoding='utf-8')
                self.assertIn('direct_messages_route_blocked', dumped)
            finally:
                forwarder.stop()

    def test_connect_summary_identifies_known_claude_control_plane_hosts(self):
        with tempfile.TemporaryDirectory() as td:
            listen_port = _free_port()
            summary = Path(td) / 'summary.jsonl'
            forwarder = RedactingForwarder(GuardConfig(
                listen_host='127.0.0.1',
                listen_port=listen_port,
                upstream_base='http://127.0.0.1:9',
                sub2api_auth='unused',
                summary_path=summary,
            ))
            forwarder.start_background()
            try:
                sock = socket.create_connection(('127.0.0.1', listen_port), timeout=5)
                try:
                    sock.sendall(b'CONNECT platform.claude.com:443 HTTP/1.1\r\nHost: platform.claude.com:443\r\n\r\n')
                    self.assertIn(b'403 Forbidden', sock.recv(4096))
                finally:
                    sock.close()
                dumped = summary.read_text(encoding='utf-8')
                self.assertIn('"target_host": "platform.claude.com"', dumped)
                self.assertIn('"target_port": 443', dumped)
            finally:
                forwarder.stop()

    def test_canary_single_message_controller_stops_cli_after_first_http_error(self):
        class CaptureHandler(BaseHTTPRequestHandler):
            count = 0

            def log_message(self, *args):
                pass

            def do_POST(self):
                self.__class__.count += 1
                n = int(self.headers.get('content-length', '0') or 0)
                if n:
                    self.rfile.read(n)
                data = b'{"error":"denied"}'
                self.send_response(429)
                self.send_header('content-type', 'application/json')
                self.send_header('content-length', str(len(data)))
                self.end_headers()
                self.wfile.write(data)

        upstream_port = _free_port()
        upstream = ThreadingHTTPServer(('127.0.0.1', upstream_port), CaptureHandler)
        threading.Thread(target=upstream.serve_forever, daemon=True).start()
        try:
            with tempfile.TemporaryDirectory() as td:
                sleeper = subprocess.Popen([sys.executable, '-c', 'import time; time.sleep(60)'])
                controller = ExecutionController(mode='canary_single_message', stop_grace_seconds=0.2)
                controller.register_cli_process(sleeper)
                listen_port = _free_port()
                forwarder = RedactingForwarder(GuardConfig(
                    listen_host='127.0.0.1',
                    listen_port=listen_port,
                    upstream_base=f'http://127.0.0.1:{upstream_port}',
                    sub2api_auth='entry',
                    summary_path=Path(td) / 'summary.jsonl',
                    max_messages=1,
                ), execution_controller=controller)
                forwarder.start_background()
                try:
                    req = urllib.request.Request(
                        f'http://127.0.0.1:{listen_port}/v1/messages?beta=true',
                        data=b'{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}],"max_tokens":16}',
                        method='POST',
                        headers={'content-type': 'application/json'},
                    )
                    with self.assertRaises(urllib.error.HTTPError) as ctx:
                        urllib.request.urlopen(req, timeout=5)
                    self.assertEqual(ctx.exception.code, 429)
                    deadline = time.time() + 5
                    while sleeper.poll() is None and time.time() < deadline:
                        time.sleep(0.05)
                    self.assertIsNotNone(sleeper.poll())
                    summary_text = (Path(td) / 'summary.jsonl').read_text(encoding='utf-8')
                    self.assertIn('execution_controller_stop_requested', summary_text)
                    self.assertEqual(CaptureHandler.count, 1)
                finally:
                    forwarder.stop()
                    if sleeper.poll() is None:
                        sleeper.kill()
                        sleeper.wait(timeout=5)
        finally:
            upstream.shutdown()


def _free_port():
    sock = socket.socket()
    sock.bind(('127.0.0.1', 0))
    port = sock.getsockname()[1]
    sock.close()
    return port


def root_dir() -> str:
    return '/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation'


if __name__ == '__main__':
    unittest.main()


def _wait_for_file(path: Path, timeout: float = 5.0) -> None:
    deadline = time.time() + timeout
    while time.time() < deadline:
        if path.exists():
            return
        time.sleep(0.05)
    raise AssertionError(f'file was not created in time: {path}')
