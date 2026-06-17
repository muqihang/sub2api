import json
import os
import socket
import subprocess
import sys
import tempfile
import threading
import time
import unittest
import urllib.error
import urllib.request
from concurrent.futures import ThreadPoolExecutor
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from tools.cli_control_plane_guard import ExecutionController, GuardConfig, RedactingForwarder
from tools.claude_code_route_trust import RouteHintReplayCache, build_signed_route_hint_headers, cp4_fixture_route_catalog
from tools.cli_session_budget import SessionBudgetLedger, SessionBudgetPolicy

STRICT_LOCAL_CANARY_LIMITS = {
    'max_body_bytes': 32768,
    'max_tokens': 2048,
    'max_tools': 3,
    'max_messages': 1,
    'max_content_blocks': 8,
    'max_text_bytes': 8192,
    'max_system_bytes': 4096,
    'max_tool_def_bytes': 8192,
    'allow_stream': False,
    'allow_thinking': False,
    'max_thinking_budget_tokens': 0,
    'allow_assistant_messages': False,
    'allow_tool_content': False,
}

_ROUTE_HINT_SECRET = 'route-hint-secret'
_ROUTE_HINT_CATALOG = cp4_fixture_route_catalog(catalog_version='cp4-cli-fixture-v1')
_DEFAULT_SESSION_REF = '11111111-2222-4333-8444-555555555555'


class CliControlPlaneGuardIntegrationTest(unittest.TestCase):
    def _assert_cost_envelope_block(self, payload, *, reason, cost_envelope_limits=None):
        capture = UpstreamCaptureServer([ResponsePlan(status=200, body=b'{"ok":true}')])
        capture.start()
        try:
            limits = dict(STRICT_LOCAL_CANARY_LIMITS)
            if cost_envelope_limits:
                limits.update(cost_envelope_limits)
            with GuardHarness(capture.server_url, cost_envelope_limits=limits) as harness:
                result = harness.request(
                    'POST',
                    '/v1/messages?beta=true',
                    body=json.dumps(payload).encode('utf-8'),
                    headers={'content-type': 'application/json'},
                )
                self.assertIn(result['status'], {400, 413, 422})
                self.assertEqual(capture.count, 0)
                summary = harness.summary_text()
                self.assertIn('cost_envelope', summary)
                self.assertIn(reason, summary)
                self.assertNotIn('messages_retry', summary)
                self.assertNotIn('fallback', summary)
                self.assertNotIn('raw-prompt-marker', summary)
        finally:
            capture.stop()

    def test_control_plane_routes_never_reach_upstream(self):
        capture = UpstreamCaptureServer([])
        capture.start()
        try:
            with GuardHarness(capture.server_url) as harness:
                cases = [
                    ('POST', '/api/event_logging/v2/batch', b'{"event":"secret-token-marker"}', 204),
                    ('POST', '/api/eval/bootstrap', b'{"eval":"raw-prompt-marker"}', 204),
                    ('GET', '/v1/mcp_servers?limit=1000', None, 200),
                    ('GET', '/api/claude_cli/bootstrap?entrypoint=sdk-cli&token=secret-token-marker', None, 403),
                    ('GET', '/api/oauth/account/settings?email=user@example.com', None, 403),
                    ('GET', '/api/claude_code_feature_flags?cookie=cookie-marker', None, 403),
                    ('GET', '/unknown/path?proxy=proxy-credential-marker', None, 403),
                ]
                for method, path, body, expected_status in cases:
                    with self.subTest(method=method, path=path):
                        result = harness.request(method, path, body=body, headers={
                            'Authorization': 'Bearer secret-token-marker',
                            'Cookie': 'session=cookie-marker',
                            'Proxy-Authorization': 'Basic proxy-credential-marker',
                            'content-type': 'application/json',
                        })
                        self.assertEqual(result['status'], expected_status)
                self.assertEqual(capture.count, 0)
                self.assertNotIn('secret-token-marker', harness.summary_text())
                self.assertNotIn('raw-prompt-marker', harness.summary_text())
                self.assertNotIn('query_hash', harness.summary_text())
                self.assertNotIn('body_hash', harness.summary_text())
        finally:
            capture.stop()

    def test_control_plane_intent_url_posts_safe_intent_to_local_endpoint(self):
        capture = UpstreamCaptureServer([])
        capture.start()
        endpoint_requests = []

        class IntentEndpointHandler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_POST(self):
                length = int(self.headers.get('content-length', '0') or 0)
                payload = json.loads(self.rfile.read(length).decode('utf-8'))
                endpoint_requests.append({
                    'headers': {key.lower(): value for key, value in self.headers.items()},
                    'payload': payload,
                })
                data = json.dumps({
                    'decision': 'stub_json',
                    'reason': 'control_plane:intent_endpoint:path',
                    'status': 200,
                    'content_type': 'application/json',
                    'body': {'from': 'intent-endpoint'},
                }).encode('utf-8')
                self.send_response(200)
                self.send_header('content-type', 'application/json')
                self.send_header('content-length', str(len(data)))
                self.end_headers()
                self.wfile.write(data)

        endpoint_port = free_port()
        endpoint_server = ThreadingHTTPServer(('127.0.0.1', endpoint_port), IntentEndpointHandler)
        threading.Thread(target=endpoint_server.serve_forever, daemon=True).start()
        try:
            with GuardHarness(
                capture.server_url,
                control_plane_intent_url=f'http://127.0.0.1:{endpoint_port}/backend-api/anthropic/control-plane/intent',
            ) as harness:
                result = harness.request(
                    'GET',
                    '/api/claude_cli/bootstrap?entrypoint=sdk-cli',
                    headers={
                        'Authorization': 'Bearer secret-token-marker',
                        'Cookie': 'session=cookie-marker',
                    },
                )
                self.assertEqual(result['status'], 200)
                self.assertEqual(json.loads(result['body'].decode('utf-8')), {'from': 'intent-endpoint'})
                self.assertEqual(capture.count, 0)
                self.assertEqual(len(endpoint_requests), 1)
                posted = endpoint_requests[0]['payload']
                headers = endpoint_requests[0]['headers']
                self.assertEqual(posted['path_template'], '/api/claude_cli/bootstrap')
                self.assertEqual(posted['normalized_query'], {'entrypoint': 'sdk-cli'})
                self.assertTrue(posted['query_ref']['value'].startswith('hmac-sha256:'))
                self.assertIn('x-sub2api-control-plane-attestation', headers)
                self.assertIn('x-sub2api-control-plane-signature', headers)
                self.assertNotIn('authorization', headers)
                self.assertNotIn('cookie', headers)
                dumped = json.dumps(posted, sort_keys=True)
                self.assertNotIn('secret-token-marker', dumped)
                self.assertNotIn('cookie-marker', dumped)
                self.assertNotIn('query_hash', dumped)
                self.assertNotIn('body_hash', dumped)
        finally:
            endpoint_server.shutdown()
            capture.stop()
    def test_messages_forward_only_replacement_auth_and_summary_redacted(self):
        capture = UpstreamCaptureServer([ResponsePlan(status=200, body=b'{"ok":true}')])
        capture.start()
        try:
            with GuardHarness(capture.server_url) as harness:
                payload = {
                    'model': 'claude-sonnet-4-6',
                    'messages': [{'role': 'user', 'content': 'raw-prompt-marker'}],
                    'max_tokens': 32,
                    'tools': [{'name': 'lookup', 'input_schema': {'type': 'object'}}],
                    'output_config': {'format': 'json'},
                }
                result = harness.request(
                    'POST',
                    '/v1/messages?beta=true',
                    body=json.dumps(payload).encode('utf-8'),
                    headers={
                        'Authorization': 'Bearer secret-token-marker',
                        'x-api-key': 'local-api-key-marker',
                        'Cookie': 'session=cookie-marker',
                        'Proxy-Authorization': 'Basic proxy-credential-marker',
                        'content-type': 'application/json',
                    },
                )
                self.assertEqual(result['status'], 200)
                self.assertEqual(capture.count, 1)
                request = capture.requests[0]
                self.assertEqual(request['headers'].get('authorization'), 'Bearer sub2api-entry')
                self.assertNotIn('x-api-key', request['headers'])
                self.assertNotIn('cookie', request['headers'])
                self.assertNotIn('proxy-authorization', request['headers'])
                summary = harness.summary_text()
                self.assertIn('auth_shape', summary)
                self.assertIn('body_keys', summary)
                for marker in (
                    'secret-token-marker',
                    'local-api-key-marker',
                    'raw-prompt-marker',
                    'cookie-marker',
                    'proxy-credential-marker',
                ):
                    self.assertNotIn(marker, summary)
                self.assertNotIn('"messages"', summary)
        finally:
            capture.stop()

    def test_messages_forward_normalizes_absolute_form_proxy_target(self):
        capture = UpstreamCaptureServer([ResponsePlan(status=200, body=b'{"ok":true}')])
        capture.start()
        try:
            with GuardHarness(capture.server_url, cost_envelope_limits={'allow_stream': True}) as harness:
                payload = {
                    'model': 'claude-sonnet-4-6',
                    'messages': [{'role': 'user', 'content': 'hello'}],
                    'max_tokens': 32,
                    'stream': True,
                }
                body = json.dumps(payload).encode('utf-8')
                request_path = '/v1/messages?beta=true'
                route_headers = build_signed_route_hint_headers(
                    body=body,
                    request_path=request_path,
                    catalog=_ROUTE_HINT_CATALOG,
                    model_id='claude-sonnet-4-6',
                    session_ref=_DEFAULT_SESSION_REF,
                    secret=_ROUTE_HINT_SECRET,
                    nonce='absolute-form-route-hint',
                )
                route_header_lines = ''.join(f'{key}: {value}\r\n' for key, value in route_headers.items())
                with socket.create_connection(('127.0.0.1', harness.listen_port), timeout=5) as sock:
                    sock.sendall(
                        (
                            f'POST http://127.0.0.1:{harness.listen_port}{request_path} HTTP/1.1\r\n'
                            f'Host: 127.0.0.1:{harness.listen_port}\r\n'
                            'content-type: application/json\r\n'
                            f'x-claude-code-session-id: {_DEFAULT_SESSION_REF}\r\n'
                            f'{route_header_lines}'
                            f'content-length: {len(body)}\r\n'
                            'connection: close\r\n\r\n'
                        ).encode('ascii') + body
                    )
                    response = b''
                    while True:
                        chunk = sock.recv(4096)
                        if not chunk:
                            break
                        response += chunk
                self.assertIn(b'200 OK', response)
                self.assertEqual(capture.count, 1)
                self.assertEqual(capture.requests[0]['path'], '/v1/messages?beta=true')
                self.assertIn('"path": "/v1/messages?beta=true"', harness.summary_text())
        finally:
            capture.stop()

    def test_messages_summary_redacts_sensitive_header_name_and_body_markers(self):
        capture = UpstreamCaptureServer([ResponsePlan(status=200, body=b'{"ok":true}')])
        capture.start()
        try:
            with GuardHarness(capture.server_url) as harness:
                payload = {
                    'model': 'claude-sonnet-4-6',
                    'secret-token-marker': 'x',
                    'messages': [{'role': 'user', 'content': 'raw-prompt-marker'}],
                }
                result = harness.request(
                    'POST',
                    '/v1/messages?beta=true',
                    body=json.dumps(payload).encode('utf-8'),
                    headers={
                        'Authorization': 'Bearer secret-token-marker',
                        'x-secret-token-marker': '1',
                        'content-type': 'application/json',
                    },
                )
                self.assertEqual(result['status'], 200)
                summary = harness.summary_text()
                self.assertNotIn('secret-token-marker', summary)
                self.assertNotIn('raw-prompt-marker', summary)
                self.assertNotIn('x-secret-token-marker', summary)
        finally:
            capture.stop()

    def test_cost_envelope_blocks_large_max_tokens_before_upstream(self):
        self._assert_cost_envelope_block(
            {
                'model': 'claude-sonnet-4-6',
                'messages': [{'role': 'user', 'content': 'raw-prompt-marker'}],
                'max_tokens': 2049,
            },
            reason='max_tokens_limit_exceeded',
        )

    def test_cost_envelope_blocks_large_raw_body_before_upstream(self):
        self._assert_cost_envelope_block(
            {
                'model': 'claude-sonnet-4-6',
                'messages': [{'role': 'user', 'content': 'x' * 33000}],
                'max_tokens': 32,
            },
            reason='body_size_limit_exceeded',
        )

    def test_cost_envelope_blocks_too_many_tools_before_upstream(self):
        self._assert_cost_envelope_block(
            {
                'model': 'claude-sonnet-4-6',
                'messages': [{'role': 'user', 'content': 'hello'}],
                'max_tokens': 32,
                'tools': [
                    {'name': 'tool-1', 'input_schema': {'type': 'object'}},
                    {'name': 'tool-2', 'input_schema': {'type': 'object'}},
                    {'name': 'tool-3', 'input_schema': {'type': 'object'}},
                    {'name': 'tool-4', 'input_schema': {'type': 'object'}},
                ],
            },
            reason='tools_limit_exceeded',
        )

    def test_cost_envelope_blocks_too_many_messages_before_upstream(self):
        self._assert_cost_envelope_block(
            {
                'model': 'claude-sonnet-4-6',
                'messages': [
                    {'role': 'user', 'content': 'first'},
                    {'role': 'user', 'content': 'second'},
                ],
                'max_tokens': 32,
            },
            reason='messages_limit_exceeded',
        )

    def test_cost_envelope_blocks_too_many_content_blocks_before_upstream(self):
        self._assert_cost_envelope_block(
            {
                'model': 'claude-sonnet-4-6',
                'messages': [{
                    'role': 'user',
                    'content': [{'type': 'text', 'text': f'block-{index}'} for index in range(9)],
                }],
                'max_tokens': 32,
            },
            reason='content_blocks_limit_exceeded',
        )

    def test_cost_envelope_blocks_tool_use_or_tool_result_content_before_upstream(self):
        for block_type in ('tool_use', 'tool_result'):
            with self.subTest(block_type=block_type):
                self._assert_cost_envelope_block(
                    {
                        'model': 'claude-sonnet-4-6',
                        'messages': [{
                            'role': 'user',
                            'content': [{'type': block_type, 'id': 'tool-1', 'content': 'x'}],
                        }],
                        'max_tokens': 32,
                    },
                    reason='tool_content_blocked',
                )

    def test_cost_envelope_blocks_stream_true_when_false_required(self):
        self._assert_cost_envelope_block(
            {
                'model': 'claude-sonnet-4-6',
                'messages': [{'role': 'user', 'content': 'hello'}],
                'max_tokens': 32,
                'stream': True,
            },
            reason='stream_disabled',
        )

    def test_cost_envelope_blocks_unknown_output_config_shape_before_upstream(self):
        self._assert_cost_envelope_block(
            {
                'model': 'claude-sonnet-4-6',
                'messages': [{'role': 'user', 'content': 'hello'}],
                'max_tokens': 32,
                'output_config': {'format': 'json', 'unexpected': {'raw': 'shape'}},
            },
            reason='output_config_shape_blocked',
        )

    def test_cost_envelope_blocks_thinking_presence_or_budget_before_upstream(self):
        for payload in (
            {
                'model': 'claude-sonnet-4-6',
                'messages': [{'role': 'user', 'content': 'hello'}],
                'max_tokens': 32,
                'thinking': {'budget_tokens': 32000},
            },
            {
                'model': 'claude-sonnet-4-6',
                'messages': [{'role': 'user', 'content': 'hello'}],
                'max_tokens': 32,
                'thinking': True,
            },
        ):
            with self.subTest(payload=payload):
                self._assert_cost_envelope_block(payload, reason='thinking_blocked')

    def test_cost_envelope_can_allow_bounded_thinking_for_explicit_capability_preflight(self):
        capture = UpstreamCaptureServer([ResponsePlan(status=200, body=b'{"ok":true}')])
        capture.start()
        try:
            with GuardHarness(
                capture.server_url,
                cost_envelope_limits={
                    'allow_thinking': True,
                    'max_thinking_budget_tokens': 4096,
                },
            ) as harness:
                payload = {
                    'model': 'claude-sonnet-4-6',
                    'messages': [{'role': 'user', 'content': 'hello'}],
                    'max_tokens': 32,
                    'thinking': {'type': 'enabled', 'budget_tokens': 1024},
                }
                result = harness.request(
                    'POST',
                    '/v1/messages?beta=true',
                    body=json.dumps(payload).encode('utf-8'),
                    headers={'content-type': 'application/json'},
                )
                self.assertEqual(result['status'], 200)
                self.assertEqual(capture.count, 1)
                self.assertIn('thinking_present', harness.summary_text())
        finally:
            capture.stop()

    def test_cost_envelope_blocks_unbounded_or_unknown_thinking_when_allowing_thinking(self):
        for payload, reason in (
            (
                {
                    'model': 'claude-sonnet-4-6',
                    'messages': [{'role': 'user', 'content': 'hello'}],
                    'max_tokens': 32,
                    'thinking': {'type': 'enabled', 'budget_tokens': 8192},
                },
                'thinking_budget_limit_exceeded',
            ),
            (
                {
                    'model': 'claude-sonnet-4-6',
                    'messages': [{'role': 'user', 'content': 'hello'}],
                    'max_tokens': 32,
                    'thinking': {'type': 'enabled', 'unknown': True},
                },
                'thinking_shape_blocked',
            ),
        ):
            with self.subTest(reason=reason):
                capture = UpstreamCaptureServer([ResponsePlan(status=200, body=b'{"ok":true}')])
                capture.start()
                try:
                    with GuardHarness(
                        capture.server_url,
                        cost_envelope_limits={
                            'allow_thinking': True,
                            'max_thinking_budget_tokens': 4096,
                        },
                    ) as harness:
                        result = harness.request(
                            'POST',
                            '/v1/messages?beta=true',
                            body=json.dumps(payload).encode('utf-8'),
                            headers={'content-type': 'application/json'},
                        )
                        self.assertEqual(result['status'], 422)
                        self.assertEqual(capture.count, 0)
                        self.assertIn(reason, harness.summary_text())
                finally:
                    capture.stop()

    def test_cost_envelope_blocks_tool_loop_and_append_round_markers_before_upstream(self):
        for payload in (
            {
                'model': 'claude-sonnet-4-6',
                'messages': [{'role': 'user', 'content': 'hello'}],
                'max_tokens': 32,
                'metadata': {'tool_loop': 1},
            },
            {
                'model': 'claude-sonnet-4-6',
                'messages': [{'role': 'user', 'content': 'hello'}],
                'max_tokens': 32,
                'append_round': 2,
            },
            {
                'model': 'claude-sonnet-4-6',
                'messages': [{
                    'role': 'assistant',
                    'content': [{'type': 'text', 'text': 'tool follow-up'}],
                }],
                'max_tokens': 32,
            },
        ):
            with self.subTest(payload=payload):
                self._assert_cost_envelope_block(payload, reason='tool_loop_blocked')

    def test_session_budget_blocks_third_message_without_global_hard_gate(self):
        capture = UpstreamCaptureServer([
            ResponsePlan(status=200, body=b'{"ok":1}'),
            ResponsePlan(status=200, body=b'{"ok":2}'),
        ])
        capture.start()
        try:
            ledger = SessionBudgetLedger(SessionBudgetPolicy(max_messages_per_session=2, max_rich_messages_per_session=2))
            with GuardHarness(
                capture.server_url,
                max_messages=0,
                session_budget_ledger=ledger,
                cost_envelope_limits={'allow_stream': True, 'max_messages': 5},
            ) as harness:
                payload = {'model': 'claude-sonnet-4-6', 'messages': [{'role': 'user', 'content': 'hello'}], 'max_tokens': 32}
                headers = {'content-type': 'application/json', 'X-Claude-Code-Session-Id': '11111111-2222-4333-8444-555555555555'}
                first = harness.request('POST', '/v1/messages?beta=true', body=json.dumps(payload).encode(), headers=headers)
                second = harness.request('POST', '/v1/messages?beta=true', body=json.dumps(payload).encode(), headers=headers)
                third = harness.request('POST', '/v1/messages?beta=true', body=json.dumps(payload).encode(), headers=headers)
                self.assertEqual(first['status'], 200)
                self.assertEqual(second['status'], 200)
                self.assertEqual(third['status'], 409)
                self.assertEqual(capture.count, 2)
                summary = harness.summary_text()
                self.assertIn('session_budget_block', summary)
                self.assertIn('session_messages_budget_exceeded', summary)
                self.assertNotIn('11111111-2222-4333-8444-555555555555', summary)
        finally:
            capture.stop()

    def test_session_budget_allows_same_count_for_distinct_sessions(self):
        capture = UpstreamCaptureServer([
            ResponsePlan(status=200, body=b'{"ok":1}'),
            ResponsePlan(status=200, body=b'{"ok":2}'),
        ])
        capture.start()
        try:
            ledger = SessionBudgetLedger(SessionBudgetPolicy(max_messages_per_session=1))
            with GuardHarness(capture.server_url, max_messages=0, session_budget_ledger=ledger) as harness:
                payload = {'model': 'claude-sonnet-4-6', 'messages': [{'role': 'user', 'content': 'hello'}], 'max_tokens': 32}
                a = harness.request('POST', '/v1/messages?beta=true', body=json.dumps(payload).encode(), headers={'content-type': 'application/json', 'X-Claude-Code-Session-Id': 'aaaaaaaa-2222-4333-8444-555555555555'})
                b = harness.request('POST', '/v1/messages?beta=true', body=json.dumps(payload).encode(), headers={'content-type': 'application/json', 'X-Claude-Code-Session-Id': 'bbbbbbbb-2222-4333-8444-555555555555'})
                self.assertEqual(a['status'], 200)
                self.assertEqual(b['status'], 200)
                self.assertEqual(capture.count, 2)
        finally:
            capture.stop()

    def test_connect_unknown_target_blocked_and_never_tunneled(self):
        capture = UpstreamCaptureServer([])
        capture.start()
        try:
            with GuardHarness(capture.server_url, connect_mode='stub') as harness:
                response = harness.connect_raw('example.com:443')
                self.assertIn(b'403 Forbidden', response)
                self.assertEqual(capture.count, 0)
        finally:
            capture.stop()

    def test_connect_unknown_target_summary_redacts_raw_target(self):
        capture = UpstreamCaptureServer([])
        capture.start()
        try:
            with GuardHarness(capture.server_url, connect_mode='stub') as harness:
                response = harness.connect_raw('secret-token-marker.example:443')
                self.assertIn(b'403 Forbidden', response)
                summary = harness.summary_text()
                self.assertNotIn('secret-token-marker', summary)
                self.assertNotIn('secret-token-marker.example:443', summary)
                self.assertIn('"scope": "control_plane_connect_target"', summary)
                self.assertIn('"version": 1', summary)
        finally:
            capture.stop()
    def test_connect_allowed_stub_target_is_local_only_and_not_real_tunnel(self):
        capture = UpstreamCaptureServer([])
        capture.start()
        try:
            with GuardHarness(capture.server_url, connect_mode='stub') as harness:
                result = harness.connect_tls_request(
                    'api.anthropic.com:443',
                    b'POST /api/event_logging/v2/batch HTTP/1.1\r\n'
                    b'Host: api.anthropic.com\r\n'
                    b'Authorization: Bearer secret-token-marker\r\n'
                    b'Content-Length: 29\r\n\r\n'
                    b'{"event":"raw-prompt-marker"}',
                )
                self.assertIn(b'204 No Content', result)
                self.assertEqual(capture.count, 0)
                summary = harness.summary_text()
                self.assertIn('connect_stubbed', summary)
                self.assertNotIn('secret-token-marker', summary)
                self.assertNotIn('raw-prompt-marker', summary)
        finally:
            capture.stop()
    def test_direct_messages_inside_connect_is_blocked_and_not_forwarded(self):
        capture = UpstreamCaptureServer([])
        capture.start()
        try:
            with GuardHarness(capture.server_url, connect_mode='stub') as harness:
                result = harness.connect_tls_request(
                    'api.anthropic.com:443',
                    b'POST /v1/messages?beta=true HTTP/1.1\r\n'
                    b'Host: api.anthropic.com\r\n'
                    b'Content-Length: 43\r\n\r\n'
                    b'{"model":"claude-sonnet-4-6","messages":[]}',
                )
                self.assertIn(b'403 Forbidden', result)
                self.assertEqual(capture.count, 0)
        finally:
            capture.stop()
    def test_tls_stub_failure_fails_closed_without_tunnel(self):
        capture = UpstreamCaptureServer([])
        capture.start()
        try:
            with GuardHarness(capture.server_url, connect_mode='stub', patch_ssl_failure=True) as harness:
                sock = socket.create_connection(('127.0.0.1', harness.listen_port), timeout=5)
                try:
                    sock.sendall(b'CONNECT api.anthropic.com:443 HTTP/1.1\r\nHost: api.anthropic.com:443\r\n\r\n')
                    self.assertIn(b'200 Connection Established', sock.recv(4096))
                    data = sock.recv(4096)
                    self.assertEqual(data, b'')
                finally:
                    sock.close()
                self.assertEqual(capture.count, 0)
                summary = harness.summary_text()
                self.assertIn('connect_stub_error', summary)
                self.assertIn('error_type', summary)
                self.assertNotIn('secret-token-marker', summary)
        finally:
            capture.stop()
    def test_max_messages_one_is_atomic_under_concurrency(self):
        release = threading.Event()
        capture = UpstreamCaptureServer([ResponsePlan(status=200, body=b'{"ok":true}', wait_event=release)])
        capture.start()
        try:
            with GuardHarness(capture.server_url, max_messages=1) as harness:
                def send_message():
                    return harness.request(
                        'POST',
                        '/v1/messages?beta=true',
                        body=b'{"model":"claude-sonnet-4-6","messages":[]}',
                        headers={'content-type': 'application/json'},
                    )
                with ThreadPoolExecutor(max_workers=2) as executor:
                    future_one = executor.submit(send_message)
                    time.sleep(0.2)
                    future_two = executor.submit(send_message)
                    blocked = future_two.result(timeout=5)
                    release.set()
                    first = future_one.result(timeout=5)
                self.assertEqual(first['status'], 200)
                self.assertEqual(blocked['status'], 409)
                self.assertEqual(capture.count, 1)
        finally:
            capture.stop()
    def test_upstream_http_errors_do_not_release_slot_or_retry(self):
        for status in (429, 503):
            with self.subTest(status=status):
                capture = UpstreamCaptureServer([ResponsePlan(status=status, body=b'{"error":"upstream"}')])
                capture.start()
                try:
                    with GuardHarness(capture.server_url, max_messages=1) as harness:
                        first = harness.request(
                            'POST',
                            '/v1/messages?beta=true',
                            body=b'{"model":"claude-sonnet-4-6","messages":[]}',
                            headers={'content-type': 'application/json'},
                        )
                        second = harness.request(
                            'POST',
                            '/v1/messages?beta=true',
                            body=b'{"model":"claude-sonnet-4-6","messages":[]}',
                            headers={'content-type': 'application/json'},
                        )
                        self.assertEqual(first['status'], status)
                        self.assertEqual(second['status'], 409)
                        self.assertEqual(capture.count, 1)
                finally:
                    capture.stop()
    def test_upstream_disconnect_does_not_release_slot_or_retry(self):
        capture = UpstreamCaptureServer([ResponsePlan(close_connection=True)])
        capture.start()
        try:
            with GuardHarness(capture.server_url, max_messages=1) as harness:
                first = harness.request(
                    'POST',
                    '/v1/messages?beta=true',
                    body=b'{"model":"claude-sonnet-4-6","messages":[]}',
                    headers={'content-type': 'application/json'},
                )
                second = harness.request(
                    'POST',
                    '/v1/messages?beta=true',
                    body=b'{"model":"claude-sonnet-4-6","messages":[]}',
                    headers={'content-type': 'application/json'},
                )
                self.assertEqual(first['status'], 502)
                self.assertEqual(second['status'], 409)
                self.assertEqual(capture.count, 1)
        finally:
            capture.stop()
    def test_execution_controller_stop_event_on_success_and_http_error(self):
        for status in (200, 429):
            with self.subTest(status=status):
                body = b'{"ok":true}' if status == 200 else b'{"error":"denied"}'
                capture = UpstreamCaptureServer([ResponsePlan(status=status, body=body)])
                capture.start()
                try:
                    with tempfile.TemporaryDirectory() as td:
                        sleeper = subprocess.Popen([sys.executable, '-c', 'import time; time.sleep(60)'])
                        controller = ExecutionController(mode='canary_single_message', stop_grace_seconds=0.2)
                        controller.register_cli_process(sleeper)
                        harness = GuardHarness(capture.server_url, max_messages=1, execution_controller=controller, temp_dir=td)
                        with harness:
                            result = harness.request(
                                'POST',
                                '/v1/messages?beta=true',
                                body=b'{"model":"claude-sonnet-4-6","messages":[]}',
                                headers={'content-type': 'application/json'},
                            )
                            self.assertEqual(result['status'], status)
                            deadline = time.time() + 5
                            while sleeper.poll() is None and time.time() < deadline:
                                time.sleep(0.05)
                            self.assertIsNotNone(sleeper.poll())
                            wait_for_text(harness.summary_text, 'execution_controller_stop_requested')
                        if sleeper.poll() is None:
                            sleeper.kill()
                            sleeper.wait(timeout=5)
                finally:
                    capture.stop()
    def test_sensitive_markers_absent_from_summary_and_test_artifacts(self):
        capture = UpstreamCaptureServer([ResponsePlan(status=200, body=b'{"ok":true}')])
        capture.start()
        try:
            with tempfile.TemporaryDirectory() as td:
                with GuardHarness(capture.server_url, temp_dir=td, connect_mode='stub') as harness:
                    harness.request(
                        'GET',
                        '/api/claude_cli/bootstrap?entrypoint=sdk-cli&email=user@example.com&token=secret-token-marker',
                        headers={
                            'Authorization': 'Bearer secret-token-marker',
                            'x-raw-prompt-marker': '1',
                            'x-api-key': 'local-api-key-marker',
                            'Cookie': 'session=cookie-marker',
                            'Proxy-Authorization': 'Basic proxy-credential-marker',
                        },
                    )
                    harness.request(
                        'POST',
                        '/v1/messages?beta=true',
                        body=json.dumps({
                            'model': 'claude-sonnet-4-6',
                            'messages': [{'role': 'user', 'content': 'raw-prompt-marker'}],
                        }).encode('utf-8'),
                        headers={
                            'Authorization': 'Bearer secret-token-marker',
                            'x-api-key': 'local-api-key-marker',
                            'Cookie': 'session=cookie-marker',
                            'Proxy-Authorization': 'Basic proxy-credential-marker',
                            'content-type': 'application/json',
                        },
                    )
                    harness.connect_tls_request(
                        'api.anthropic.com:443',
                        b'GET /unknown/secret-token-marker?email=user@example.com HTTP/1.1\r\n'
                        b'Host: api.anthropic.com\r\n'
                        b'Cookie: session=cookie-marker\r\n\r\n',
                    )
                    artifact_dump = scan_text_artifacts(Path(td))
                    self.assertIn('auth_shape', artifact_dump)
                    for marker in (
                        'secret-token-marker',
                        'local-api-key-marker',
                        'raw-prompt-marker',
                        'cookie-marker',
                        'proxy-credential-marker',
                        'user@example.com',
                    ):
                        self.assertNotIn(marker, artifact_dump)
        finally:
            capture.stop()
class ResponsePlan:
    def __init__(self, *, status=200, body=b'', headers=None, wait_event=None, close_connection=False):
        self.status = status
        self.body = body
        self.headers = headers or {'content-type': 'application/json'}
        self.wait_event = wait_event
        self.close_connection = close_connection
class UpstreamCaptureServer:
    def __init__(self, plans):
        self.plans = list(plans)
        self.requests = []
        self.count = 0
        self._lock = threading.Lock()
        self.port = free_port()
        self._server = None
    @property
    def server_url(self):
        return f'http://127.0.0.1:{self.port}'
    def start(self):
        parent = self
        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass
            def do_GET(self):
                self._handle()
            def do_POST(self):
                self._handle()
            def _handle(self):
                with parent._lock:
                    index = parent.count
                    parent.count += 1
                plan = parent.plans[index] if index < len(parent.plans) else ResponsePlan(status=200, body=b'{"default":true}')
                length = int(self.headers.get('content-length', '0') or 0)
                body = self.rfile.read(length) if length else b''
                parent.requests.append({
                    'method': self.command,
                    'path': self.path,
                    'headers': {key.lower(): value for key, value in self.headers.items()},
                    'body': body,
                })
                if plan.wait_event is not None:
                    plan.wait_event.wait(timeout=5)
                if plan.close_connection:
                    self.connection.shutdown(socket.SHUT_RDWR)
                    self.connection.close()
                    return
                self.send_response(plan.status)
                for key, value in plan.headers.items():
                    self.send_header(key, value)
                self.send_header('content-length', str(len(plan.body)))
                self.end_headers()
                if plan.body:
                    self.wfile.write(plan.body)
        self._server = ThreadingHTTPServer(('127.0.0.1', self.port), Handler)
        threading.Thread(target=self._server.serve_forever, daemon=True).start()
    def stop(self):
        if self._server is not None:
            self._server.shutdown()
            self._server.server_close()
class GuardHarness:
    def __init__(
        self,
        upstream_base,
        *,
        control_plane_intent_url=None,
        connect_mode='block',
        max_messages=0,
        execution_controller=None,
        temp_dir=None,
        patch_ssl_failure=False,
        cost_envelope_limits=None,
        session_budget_ledger=None,
    ):
        self._temp_dir_ctx = None
        if temp_dir is None:
            self._temp_dir_ctx = tempfile.TemporaryDirectory()
            temp_dir = self._temp_dir_ctx.name
        self.temp_dir = Path(temp_dir)
        self.listen_port = free_port()
        self.summary_path = self.temp_dir / 'summary.jsonl'
        self.cert_path = self.temp_dir / 'api.anthropic.com.pem'
        self.key_path = self.temp_dir / 'api.anthropic.com.key'
        self.forwarder = RedactingForwarder(GuardConfig(
            listen_host='127.0.0.1',
            listen_port=self.listen_port,
            upstream_base=upstream_base,
            sub2api_auth='sub2api-entry',
            summary_path=self.summary_path,
            control_plane_intent_url=control_plane_intent_url,
            connect_mode=connect_mode,
            cert_path=self.cert_path,
            key_path=self.key_path,
            max_messages=max_messages,
            cost_envelope_limits=cost_envelope_limits,
            session_budget_ledger=session_budget_ledger,
            native_attestation_secret='native-attestation-test-secret',
            route_hint_secret=_ROUTE_HINT_SECRET,
            route_hint_catalog=_ROUTE_HINT_CATALOG,
            route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
        ), execution_controller=execution_controller)
        self.patch_ssl_failure = patch_ssl_failure
        self._original_ssl_context = None
    def __enter__(self):
        if self.patch_ssl_failure:
            import ssl
            self._original_ssl_context = self.forwarder._ssl_context
            def failing_context():
                raise ssl.SSLError('forced-stub-failure')
            self.forwarder._ssl_context = failing_context
        self.forwarder.start_background()
        return self
    def __exit__(self, exc_type, exc, tb):
        self.forwarder.stop()
        if self._original_ssl_context is not None:
            self.forwarder._ssl_context = self._original_ssl_context
        if self._temp_dir_ctx is not None:
            self._temp_dir_ctx.cleanup()
    def request(self, method, path, *, body=None, headers=None):
        headers = dict(headers or {})
        if method == 'POST' and path.startswith('/v1/messages') and body is not None and not any(key.lower() == 'x-zhumeng-claude-code-route-hint' for key in headers):
            session_ref = headers.get('X-Claude-Code-Session-Id') or headers.get('x-claude-code-session-id') or _DEFAULT_SESSION_REF
            headers.setdefault('x-claude-code-session-id', session_ref)
            route_headers = build_signed_route_hint_headers(
                body=body,
                request_path=path,
                catalog=_ROUTE_HINT_CATALOG,
                model_id='claude-sonnet-4-6',
                session_ref=session_ref,
                secret=_ROUTE_HINT_SECRET,
                nonce=f'harness-{time.time_ns()}',
            )
            headers.update(route_headers)
        req = urllib.request.Request(
            f'http://127.0.0.1:{self.listen_port}{path}',
            data=body,
            method=method,
            headers=headers,
        )
        try:
            with urllib.request.urlopen(req, timeout=5) as resp:
                return {'status': resp.status, 'body': resp.read(), 'headers': dict(resp.headers)}
        except urllib.error.HTTPError as exc:
            return {'status': exc.code, 'body': exc.read(), 'headers': dict(exc.headers)}
    def connect_raw(self, target):
        sock = socket.create_connection(('127.0.0.1', self.listen_port), timeout=5)
        try:
            sock.sendall(f'CONNECT {target} HTTP/1.1\r\nHost: {target}\r\n\r\n'.encode('ascii'))
            return sock.recv(4096)
        finally:
            sock.close()
    def connect_tls_request(self, target, payload):
        import ssl
        sock = socket.create_connection(('127.0.0.1', self.listen_port), timeout=5)
        sock.sendall(f'CONNECT {target} HTTP/1.1\r\nHost: {target}\r\n\r\n'.encode('ascii'))
        response = sock.recv(4096)
        if b'200 Connection Established' not in response:
            sock.close()
            return response
        wait_for_file(self.cert_path)
        ctx = ssl.create_default_context(cafile=str(self.cert_path))
        with ctx.wrap_socket(sock, server_hostname='api.anthropic.com') as tls:
            tls.sendall(payload)
            chunks = []
            while True:
                data = tls.recv(4096)
                if not data:
                    break
                chunks.append(data)
            return b''.join(chunks)
    def summary_text(self):
        if not self.summary_path.exists():
            return ''
        return self.summary_path.read_text(encoding='utf-8')
def wait_for_file(path: Path, timeout: float = 5.0) -> None:
    deadline = time.time() + timeout
    while time.time() < deadline:
        if path.exists():
            return
        time.sleep(0.05)
    raise AssertionError(f'file was not created in time: {path}')


def wait_for_text(reader, needle: str, timeout: float = 5.0) -> None:
    deadline = time.time() + timeout
    while time.time() < deadline:
        haystack = reader()
        if needle in haystack:
            return
        time.sleep(0.05)
    raise AssertionError(f'text was not observed in time: {needle}')


def free_port():
    sock = socket.socket()
    sock.bind(('127.0.0.1', 0))
    port = sock.getsockname()[1]
    sock.close()
    return port
def scan_text_artifacts(path: Path) -> str:
    chunks = []
    for file_path in sorted(path.rglob('*')):
        if file_path.is_file():
            try:
                chunks.append(file_path.read_text(encoding='utf-8'))
            except UnicodeDecodeError:
                chunks.append(file_path.read_bytes().decode('utf-8', 'ignore'))
    return '\n'.join(chunks)
if __name__ == '__main__':
    unittest.main()
