import argparse
import base64
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
    validate_cp5_bridge_body,
)
from tools.claude_code_route_trust import (
    RouteCatalog,
    RouteCatalogEntry,
    RouteHintReplayCache,
    build_signed_route_hint_headers,
    cp4_fixture_route_catalog,
    verify_signed_route_hint_headers,
)
from tools.cli_control_plane_policy import load_default_policy


_ROUTE_HINT_SECRET = 'route-hint-secret'
_ROUTE_HINT_CATALOG = cp4_fixture_route_catalog(catalog_version='cp4-cli-fixture-v1')
_DEFAULT_SESSION_REF = '11111111-2222-4333-8444-555555555555'


def _native_route_headers(body: bytes, path: str = '/v1/messages?beta=true', *, session_ref: str = _DEFAULT_SESSION_REF, nonce: str | None = None) -> dict[str, str]:
    return build_signed_route_hint_headers(
        body=body,
        request_path=path,
        catalog=_ROUTE_HINT_CATALOG,
        model_id='claude-sonnet-4-6',
        session_ref=session_ref,
        secret=_ROUTE_HINT_SECRET,
        nonce=nonce or f'unit-{time.time_ns()}',
    )


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


    def test_root_dir_is_current_repo_root(self):
        self.assertEqual(Path(root_dir()).resolve(), Path(__file__).resolve().parents[2])

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
                route_hint_secret=_ROUTE_HINT_SECRET,
                route_hint_catalog=_ROUTE_HINT_CATALOG,
                route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
                managed_session_id='managed-session',
                device_id='9',
                agent_version='0.1.0',
            ))
            forwarder.start_background()
            try:
                body = b'{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}],"max_tokens":16}'
                path = '/v1/messages?beta=true'
                request = urllib.request.Request(
                    f'http://127.0.0.1:{listen_port}{path}',
                    data=body,
                    method='POST',
                    headers={
                        'content-type': 'application/json',
                        'User-Agent': 'claude-cli/2.1.150 (external, sdk-cli)',
                        'x-claude-code-session-id': _DEFAULT_SESSION_REF,
                        **_native_route_headers(body, path, nonce='managed-device-headers'),
                    },
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
                route_hint_secret=_ROUTE_HINT_SECRET,
                route_hint_catalog=_ROUTE_HINT_CATALOG,
                route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
                managed_session_id='managed-session',
                device_id='9',
                agent_version='0.1.0',
            ))
            forwarder.start_background()
            try:
                body = b'{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}],"max_tokens":16}'
                path = '/v1/messages?beta=true'
                request = urllib.request.Request(
                    f'http://127.0.0.1:{listen_port}{path}',
                    data=body,
                    method='POST',
                    headers={
                        'content-type': 'application/json',
                        'User-Agent': 'claude-cli/2.1.150 (external, sdk-cli)',
                        'anthropic-version': '2023-06-01',
                        'x-stainless-lang': 'js',
                        'x-access-token': 'local-token-leak',
                        'x-prompt': 'local-prompt-leak',
                        'x-claude-code-session-id': _DEFAULT_SESSION_REF,
                        'x-random-local-debug': 'debug-leak',
                        **_native_route_headers(body, path, nonce='strict-header-allowlist'),
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
                route_hint_secret=_ROUTE_HINT_SECRET,
                route_hint_catalog=_ROUTE_HINT_CATALOG,
                route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
                extra_forward_headers={
                    'anthropic-version': '2023-06-01',
                    'x-prompt': 'extra-prompt-leak',
                    'x-access-token': 'extra-token-leak',
                    'x-random-local-debug': 'extra-debug-leak',
                },
            ))
            forwarder.start_background()
            try:
                body = b'{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}],"max_tokens":16}'
                path = '/v1/messages?beta=true'
                request = urllib.request.Request(
                    f'http://127.0.0.1:{listen_port}{path}',
                    data=body,
                    method='POST',
                    headers={
                        'content-type': 'application/json',
                        'User-Agent': 'claude-cli/2.1.150 (external, sdk-cli)',
                        'x-claude-code-session-id': _DEFAULT_SESSION_REF,
                        **_native_route_headers(body, path, nonce='extra-forward-headers'),
                    },
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

    def test_cp4_messages_route_decision_requires_route_hint_catalog_even_for_claude_native(self):
        summary_path = tempfile.NamedTemporaryFile(delete=True)
        summary_path.close()
        forwarder = RedactingForwarder(GuardConfig(
            listen_host='127.0.0.1',
            listen_port=0,
            upstream_base='http://127.0.0.1:9',
            sub2api_auth='entry',
            summary_path=Path(summary_path.name),
            native_attestation_secret='native-attestation-test-secret',
        ))

        decision = forwarder._messages_route_decision(
            b'{"model":"claude-sonnet-4-6","messages":[]}',
            '/v1/messages',
            {'x-claude-code-session-id': 'session-a'},
        )

        self.assertIsNone(decision)
        summary = Path(summary_path.name).read_text(encoding='utf-8')
        self.assertIn('route_hint_required', summary)

    def test_cp4_signed_route_hint_binds_model_route_hashes_session_and_nonce(self):
        catalog = cp4_fixture_route_catalog(
            runtime_hash='sha256:' + '1' * 64,
            overlay_hash='sha256:' + '2' * 64,
            catalog_hash='sha256:' + '3' * 64,
            catalog_version='cp4-test-v1',
        )
        body = b'{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"hello"}],"max_tokens":16}'
        headers = build_signed_route_hint_headers(
            body=body,
            request_path='/v1/messages?beta=true',
            catalog=catalog,
            model_id='deepseek-v4-pro',
            session_ref='session-a',
            secret='route-hint-secret',
            now=1000,
            nonce='nonce-a',
        )

        decision = verify_signed_route_hint_headers(
            source_headers=headers,
            body=body,
            request_path='/v1/messages?beta=true',
            catalog=catalog,
            session_ref='session-a',
            secret='route-hint-secret',
            now=1000,
            replay_cache=RouteHintReplayCache(ttl_seconds=60),
        )

        self.assertEqual(decision.route, 'deepseek_bridge')
        self.assertEqual(decision.client_type, 'claude_code_bridge_deepseek')
        self.assertFalse(decision.native_attestation_allowed)
        self.assertFalse(decision.formal_pool_allowed)


    def test_cp5_native_route_hint_hashes_and_catalog_version_are_signed_into_attestation(self):
        catalog = cp4_fixture_route_catalog(
            runtime_hash='sha256:' + '1' * 64,
            overlay_hash='sha256:' + '2' * 64,
            catalog_hash='sha256:' + '3' * 64,
            catalog_version='cp5-native-catalog-v1',
        )
        body = b'{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}'
        route_headers = build_signed_route_hint_headers(
            body=body,
            request_path='/v1/messages',
            catalog=catalog,
            model_id='claude-sonnet-4-6',
            session_ref='session-native',
            secret='route-hint-secret',
            now=1000,
            nonce='nonce-native-attestation-bindings',
        )
        decision = verify_signed_route_hint_headers(
            source_headers=route_headers,
            body=body,
            request_path='/v1/messages',
            catalog=catalog,
            session_ref='session-native',
            secret='route-hint-secret',
            now=1000,
            replay_cache=RouteHintReplayCache(ttl_seconds=60),
        )
        with unittest.mock.patch.dict(
            'os.environ',
            {
                'ZHUMENG_CLAUDE_RUNTIME_HASH': 'sha256:' + '9' * 64,
                'ZHUMENG_CLAUDE_OVERLAY_HASH': 'sha256:' + '8' * 64,
                'ZHUMENG_CLAUDE_CATALOG_HASH': 'sha256:' + '7' * 64,
            },
            clear=False,
        ):
            native_headers = build_native_messages_attestation_headers(
                body,
                '/v1/messages',
                {'x-claude-code-session-id': 'session-native'},
                secret='native-attestation-test-secret',
                route_decision=decision,
            )
        payload = json.loads(base64.urlsafe_b64decode(native_headers['x-sub2api-native-attestation'] + '=='))
        self.assertEqual(payload['runtime_hash'], 'sha256:' + '1' * 64)
        self.assertEqual(payload['overlay_hash'], 'sha256:' + '2' * 64)
        self.assertEqual(payload['catalog_hash'], 'sha256:' + '3' * 64)
        self.assertEqual(payload['catalog_version'], 'cp5-native-catalog-v1')

    def test_cp4_route_hint_fails_closed_for_model_mismatch_stale_and_replay(self):
        catalog = cp4_fixture_route_catalog(
            runtime_hash='sha256:' + '1' * 64,
            overlay_hash='sha256:' + '2' * 64,
            catalog_hash='sha256:' + '3' * 64,
            catalog_version='cp4-test-v1',
        )
        body = b'{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"hello"}]}'
        headers = build_signed_route_hint_headers(
            body=body,
            request_path='/v1/messages?beta=true',
            catalog=catalog,
            model_id='deepseek-v4-pro',
            session_ref='session-a',
            secret='route-hint-secret',
            now=1000,
            nonce='nonce-a',
        )
        mismatched_body = b'{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}'
        with self.assertRaisesRegex(RuntimeError, 'model binding'):
            verify_signed_route_hint_headers(
                source_headers=headers,
                body=mismatched_body,
                request_path='/v1/messages?beta=true',
                catalog=catalog,
                session_ref='session-a',
                secret='route-hint-secret',
                now=1000,
                replay_cache=RouteHintReplayCache(ttl_seconds=60),
            )
        with self.assertRaisesRegex(RuntimeError, 'stale'):
            verify_signed_route_hint_headers(
                source_headers=headers,
                body=body,
                request_path='/v1/messages?beta=true',
                catalog=catalog,
                session_ref='session-a',
                secret='route-hint-secret',
                now=1200,
                replay_cache=RouteHintReplayCache(ttl_seconds=60),
            )
        cache = RouteHintReplayCache(ttl_seconds=60)
        verify_signed_route_hint_headers(
            source_headers=headers,
            body=body,
            request_path='/v1/messages?beta=true',
            catalog=catalog,
            session_ref='session-a',
            secret='route-hint-secret',
            now=1000,
            replay_cache=cache,
        )
        with self.assertRaisesRegex(RuntimeError, 'replayed'):
            verify_signed_route_hint_headers(
                source_headers=headers,
                body=body,
                request_path='/v1/messages?beta=true',
                catalog=catalog,
                session_ref='session-a',
                secret='route-hint-secret',
                now=1000,
                replay_cache=cache,
            )
        with self.assertRaisesRegex(RuntimeError, 'unknown route hint model'):
            build_signed_route_hint_headers(
                body=b'{"model":"glm-4.6","messages":[]}',
                request_path='/v1/messages?beta=true',
                catalog=catalog,
                model_id='glm-4.6',
                session_ref='session-a',
                secret='route-hint-secret',
                now=1000,
                nonce='nonce-unknown',
                route='claude_code_native',
                client_type='claude_code_native',
                provider='claude',
                native_attestation_allowed=True,
                formal_pool_allowed=True,
            )

    def test_cp4_route_hint_fails_closed_when_body_claude_claims_bridge_route(self):
        catalog = cp4_fixture_route_catalog(
            runtime_hash='sha256:' + '1' * 64,
            overlay_hash='sha256:' + '2' * 64,
            catalog_hash='sha256:' + '3' * 64,
            catalog_version='cp4-test-v1',
        )
        body = b'{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}'
        headers = build_signed_route_hint_headers(
            body=body,
            request_path='/v1/messages?beta=true',
            catalog=catalog,
            model_id='claude-sonnet-4-6',
            session_ref='session-a',
            secret='route-hint-secret',
            now=1000,
            nonce='nonce-claude-claims-bridge',
            route='deepseek_bridge',
            client_type='claude_code_bridge_deepseek',
            native_attestation_allowed=False,
            formal_pool_allowed=False,
        )

        with self.assertRaisesRegex(RuntimeError, 'catalog route binding'):
            verify_signed_route_hint_headers(
                source_headers=headers,
                body=body,
                request_path='/v1/messages?beta=true',
                catalog=catalog,
                session_ref='session-a',
                secret='route-hint-secret',
                now=1000,
                replay_cache=RouteHintReplayCache(ttl_seconds=60),
            )


    def test_cp5_bridge_route_hint_returns_internal_skeleton_anthropic_sse_without_upstream_or_native_attestation(self):
        catalog = cp4_fixture_route_catalog(
            runtime_hash='sha256:' + '1' * 64,
            overlay_hash='sha256:' + '2' * 64,
            catalog_hash='sha256:' + '3' * 64,
            catalog_version='cp4-test-v1',
        )

        class CaptureHandler(BaseHTTPRequestHandler):
            requests = []

            def log_message(self, *args):
                pass

            def do_POST(self):
                n = int(self.headers.get('content-length', '0') or 0)
                if n:
                    self.rfile.read(n)
                self.__class__.requests.append({
                    'path': self.path,
                    'headers': {key.lower(): value for key, value in self.headers.items()},
                })
                self.send_response(599)
                self.send_header('content-length', '0')
                self.end_headers()

        upstream_port = _free_port()
        upstream = ThreadingHTTPServer(('127.0.0.1', upstream_port), CaptureHandler)
        threading.Thread(target=upstream.serve_forever, daemon=True).start()
        try:
            with tempfile.TemporaryDirectory() as td:
                body = b'{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"hello"}],"max_tokens":16,"stream":true}'
                path = '/v1/messages?beta=true'
                route_headers = build_signed_route_hint_headers(
                    body=body,
                    request_path=path,
                    catalog=catalog,
                    model_id='deepseek-v4-pro',
                    session_ref='11111111-2222-4333-8444-555555555555',
                    secret='route-hint-secret',
                    now=None,
                    nonce='nonce-bridge-forward',
                )
                listen_port = _free_port()
                summary = Path(td) / 'summary.jsonl'
                forwarder = RedactingForwarder(GuardConfig(
                    listen_host='127.0.0.1',
                    listen_port=listen_port,
                    upstream_base=f'http://127.0.0.1:{upstream_port}',
                    sub2api_auth='sub2api-entry-key',
                    summary_path=summary,
                    native_attestation_secret='native-attestation-test-secret',
                    route_hint_secret='route-hint-secret',
                    route_hint_catalog=catalog,
                    route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
                    max_messages=0,
                ))
                forwarder.start_background()
                try:
                    req = urllib.request.Request(
                        f'http://127.0.0.1:{listen_port}{path}',
                        data=body,
                        method='POST',
                        headers={
                            'content-type': 'application/json',
                            'x-claude-code-session-id': '11111111-2222-4333-8444-555555555555',
                            **route_headers,
                        },
                    )
                    with urllib.request.urlopen(req, timeout=5) as resp:
                        data = resp.read()
                        self.assertEqual(resp.status, 200)
                        self.assertEqual(resp.headers.get('content-type'), 'text/event-stream')
                    self.assertIn(b'event: message_start', data)
                    self.assertIn(b'bridge skeleton', data)
                    self.assertEqual(len(CaptureHandler.requests), 0)
                    replay_req = urllib.request.Request(
                        f'http://127.0.0.1:{listen_port}{path}',
                        data=body,
                        method='POST',
                        headers={
                            'content-type': 'application/json',
                            'x-claude-code-session-id': '11111111-2222-4333-8444-555555555555',
                            **route_headers,
                        },
                    )
                    with self.assertRaises(urllib.error.HTTPError) as replay_ctx:
                        urllib.request.urlopen(replay_req, timeout=5)
                    self.assertEqual(replay_ctx.exception.code, 403)
                    self.assertEqual(len(CaptureHandler.requests), 0)
                    dumped = summary.read_text(encoding='utf-8')
                    self.assertIn('"client_type": "claude_code_bridge_deepseek"', dumped)
                    self.assertIn('"native_attested": false', dumped)
                    self.assertIn('"event": "messages_bridge_skeleton_response"', dumped)
                    self.assertIn('"decision": "bridge_skeleton_cp5"', dumped)
                    self.assertNotIn('route-hint-secret', dumped)
                    self.assertNotIn('native-attestation-test-secret', dumped)
                    self.assertNotIn('hello', dumped)
                finally:
                    forwarder.stop()
        finally:
            upstream.shutdown()

    def test_cp5_bridge_skeleton_tool_use_sse_golden_and_safe_audit_without_upstream_or_native(self):
        catalog = cp4_fixture_route_catalog(
            runtime_hash='sha256:' + '1' * 64,
            overlay_hash='sha256:' + '2' * 64,
            catalog_hash='sha256:' + '3' * 64,
            catalog_version='cp5-test-v1',
        )

        class CaptureHandler(BaseHTTPRequestHandler):
            requests = []

            def log_message(self, *args):
                pass

            def do_POST(self):
                n = int(self.headers.get('content-length', '0') or 0)
                if n:
                    self.rfile.read(n)
                self.__class__.requests.append({
                    'path': self.path,
                    'headers': {key.lower(): value for key, value in self.headers.items()},
                })
                self.send_response(599)
                self.send_header('content-length', '0')
                self.end_headers()

        upstream_port = _free_port()
        upstream = ThreadingHTTPServer(('127.0.0.1', upstream_port), CaptureHandler)
        threading.Thread(target=upstream.serve_forever, daemon=True).start()
        try:
            with tempfile.TemporaryDirectory() as td:
                body = json.dumps({
                    'model': 'gpt-5.5',
                    'messages': [{'role': 'user', 'content': 'please call weather'}],
                    'max_tokens': 16,
                    'stream': True,
                    'tools': [{
                        'name': 'get_weather',
                        'description': 'weather lookup',
                        'input_schema': {
                            'type': 'object',
                            'properties': {'city': {'type': 'string'}},
                            'required': ['city'],
                        },
                    }],
                    'tool_choice': {'type': 'tool', 'name': 'get_weather'},
                }, separators=(',', ':')).encode('utf-8')
                path = '/v1/messages?beta=true'
                route_headers = build_signed_route_hint_headers(
                    body=body,
                    request_path=path,
                    catalog=catalog,
                    model_id='gpt-5.5',
                    session_ref='11111111-2222-4333-8444-555555555555',
                    secret='route-hint-secret',
                    now=None,
                    nonce='nonce-cp5-bridge-tool-use',
                )
                listen_port = _free_port()
                summary = Path(td) / 'summary.jsonl'
                forwarder = RedactingForwarder(GuardConfig(
                    listen_host='127.0.0.1',
                    listen_port=listen_port,
                    upstream_base=f'http://127.0.0.1:{upstream_port}',
                    sub2api_auth='sub2api-entry-key',
                    summary_path=summary,
                    native_attestation_secret='native-attestation-test-secret',
                    route_hint_secret='route-hint-secret',
                    route_hint_catalog=catalog,
                    route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
                    max_messages=0,
                ))
                forwarder.start_background()
                try:
                    req = urllib.request.Request(
                        f'http://127.0.0.1:{listen_port}{path}',
                        data=body,
                        method='POST',
                        headers={
                            'content-type': 'application/json',
                            'x-claude-code-session-id': '11111111-2222-4333-8444-555555555555',
                            **route_headers,
                        },
                    )
                    with urllib.request.urlopen(req, timeout=5) as resp:
                        data = resp.read().decode('utf-8')
                        self.assertEqual(resp.status, 200)
                        self.assertEqual(resp.headers.get('content-type'), 'text/event-stream')
                    self.assertEqual(len(CaptureHandler.requests), 0)
                    expected_order = [
                        'event: message_start',
                        'event: content_block_start',
                        'event: content_block_delta',
                        'event: content_block_stop',
                        'event: message_delta',
                        'event: message_stop',
                    ]
                    positions = [data.index(marker) for marker in expected_order]
                    self.assertEqual(positions, sorted(positions))
                    self.assertIn('"type":"tool_use"', data)
                    self.assertIn('"name":"get_weather"', data)
                    self.assertIn('"type":"input_json_delta"', data)
                    self.assertIn('"partial_json":"{', data)
                    self.assertIn('"stop_reason":"tool_use"', data)
                    self.assertNotIn('bridge stub', data)

                    dumped = summary.read_text(encoding='utf-8')
                    self.assertIn('"event": "messages_bridge_skeleton_response"', dumped)
                    self.assertIn('"decision": "bridge_skeleton_cp5"', dumped)
                    self.assertIn('"provider": "openai"', dumped)
                    self.assertIn('"route": "openai_bridge"', dumped)
                    self.assertIn('"catalog_version": "cp5-test-v1"', dumped)
                    self.assertIn('"runtime_hash": "sha256:' + '1' * 64 + '"', dumped)
                    self.assertIn('"overlay_hash": "sha256:' + '2' * 64 + '"', dumped)
                    self.assertIn('"catalog_hash": "sha256:' + '3' * 64 + '"', dumped)
                    self.assertIn('"credential_scope": "bridge_pool"', dumped)
                    self.assertIn('"formal_pool_allowed": false', dumped)
                    self.assertIn('"native_attested": false', dumped)
                    self.assertNotIn('route-hint-secret', dumped)
                    self.assertNotIn('native-attestation-test-secret', dumped)
                    self.assertNotIn('please call weather', dumped)
                finally:
                    forwarder.stop()
        finally:
            upstream.shutdown()


    def test_cp5_bridge_body_allows_anthropic_valid_metadata_stop_sequences_top_k_and_thinking(self):
        catalog = cp4_fixture_route_catalog(
            runtime_hash='sha256:' + '1' * 64,
            overlay_hash='sha256:' + '2' * 64,
            catalog_hash='sha256:' + '3' * 64,
            catalog_version='cp5-test-v1',
        )
        body = json.dumps({
            'model': 'gpt-5.5',
            'messages': [{'role': 'user', 'content': 'hello'}],
            'stream': True,
            'metadata': {'user_id': 'safe-user'},
            'stop_sequences': ['DONE'],
            'top_k': 5,
            'thinking': {'type': 'enabled', 'budget_tokens': 1024},
        }, separators=(',', ':')).encode('utf-8')
        route_headers = build_signed_route_hint_headers(
            body=body,
            request_path='/v1/messages',
            catalog=catalog,
            model_id='gpt-5.5',
            session_ref='session-bridge',
            secret='route-hint-secret',
            now=1000,
            nonce='nonce-anthropic-valid-fields',
        )
        decision = verify_signed_route_hint_headers(
            source_headers=route_headers,
            body=body,
            request_path='/v1/messages',
            catalog=catalog,
            session_ref='session-bridge',
            secret='route-hint-secret',
            now=1000,
            replay_cache=RouteHintReplayCache(ttl_seconds=60),
        )

        self.assertIsNone(validate_cp5_bridge_body(decision, body))

    def test_cp5_bridge_skeleton_rejects_openai_function_tool_shape_without_upstream_or_prompt_leak(self):
        catalog = cp4_fixture_route_catalog(
            runtime_hash='sha256:' + '1' * 64,
            overlay_hash='sha256:' + '2' * 64,
            catalog_hash='sha256:' + '3' * 64,
            catalog_version='cp5-test-v1',
        )

        class CaptureHandler(BaseHTTPRequestHandler):
            requests = []

            def log_message(self, *args):
                pass

            def do_POST(self):
                n = int(self.headers.get('content-length', '0') or 0)
                if n:
                    self.rfile.read(n)
                self.__class__.requests.append({
                    'path': self.path,
                    'headers': {key.lower(): value for key, value in self.headers.items()},
                })
                self.send_response(599)
                self.send_header('content-length', '0')
                self.end_headers()

        upstream_port = _free_port()
        upstream = ThreadingHTTPServer(('127.0.0.1', upstream_port), CaptureHandler)
        threading.Thread(target=upstream.serve_forever, daemon=True).start()
        try:
            with tempfile.TemporaryDirectory() as td:
                body = json.dumps({
                    'model': 'gpt-5.5',
                    'messages': [{'role': 'user', 'content': 'function-tool-shape-must-not-leak'}],
                    'stream': True,
                    'tools': [{
                        'type': 'function',
                        'function': {'name': 'leak', 'parameters': {'type': 'object'}},
                    }],
                    'tool_choice': {'type': 'function', 'function': {'name': 'leak'}},
                }, separators=(',', ':')).encode('utf-8')
                path = '/v1/messages?beta=true'
                route_headers = build_signed_route_hint_headers(
                    body=body,
                    request_path=path,
                    catalog=catalog,
                    model_id='gpt-5.5',
                    session_ref='11111111-2222-4333-8444-555555555555',
                    secret='route-hint-secret',
                    now=None,
                    nonce='nonce-cp5-bridge-function-tool-shape',
                )
                listen_port = _free_port()
                summary = Path(td) / 'summary.jsonl'
                forwarder = RedactingForwarder(GuardConfig(
                    listen_host='127.0.0.1',
                    listen_port=listen_port,
                    upstream_base=f'http://127.0.0.1:{upstream_port}',
                    sub2api_auth='sub2api-entry-key',
                    summary_path=summary,
                    native_attestation_secret='native-attestation-test-secret',
                    route_hint_secret='route-hint-secret',
                    route_hint_catalog=catalog,
                    route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
                ))
                forwarder.start_background()
                try:
                    req = urllib.request.Request(
                        f'http://127.0.0.1:{listen_port}{path}',
                        data=body,
                        method='POST',
                        headers={
                            'content-type': 'application/json',
                            'x-claude-code-session-id': '11111111-2222-4333-8444-555555555555',
                            **route_headers,
                        },
                    )
                    with self.assertRaises(urllib.error.HTTPError) as ctx:
                        urllib.request.urlopen(req, timeout=5)
                    self.assertEqual(ctx.exception.code, 403)
                    self.assertEqual(len(CaptureHandler.requests), 0)
                    dumped = summary.read_text(encoding='utf-8')
                    self.assertIn('"event": "messages_gate_block"', dumped)
                    self.assertIn('"reason": "messages_body_invalid"', dumped)
                    self.assertIn('"validation_error": "openai_function_tool_shape"', dumped)
                    self.assertNotIn('function-tool-shape-must-not-leak', dumped)
                    self.assertNotIn('"leak"', dumped)
                    self.assertNotIn('route-hint-secret', dumped)
                    self.assertNotIn('native-attestation-test-secret', dumped)
                finally:
                    forwarder.stop()
        finally:
            upstream.shutdown()

    def test_cp5_bridge_count_tokens_fails_closed_before_skeleton_or_upstream(self):
        catalog = cp4_fixture_route_catalog(
            runtime_hash='sha256:' + '1' * 64,
            overlay_hash='sha256:' + '2' * 64,
            catalog_hash='sha256:' + '3' * 64,
            catalog_version='cp5-test-v1',
        )

        class CaptureHandler(BaseHTTPRequestHandler):
            requests = []

            def log_message(self, *args):
                pass

            def do_POST(self):
                n = int(self.headers.get('content-length', '0') or 0)
                if n:
                    self.rfile.read(n)
                self.__class__.requests.append({'path': self.path})
                self.send_response(599)
                self.send_header('content-length', '0')
                self.end_headers()

        upstream_port = _free_port()
        upstream = ThreadingHTTPServer(('127.0.0.1', upstream_port), CaptureHandler)
        threading.Thread(target=upstream.serve_forever, daemon=True).start()
        try:
            with tempfile.TemporaryDirectory() as td:
                body = json.dumps({
                    'model': 'gpt-5.5',
                    'messages': [{'role': 'user', 'content': 'count-tokens-bridge-must-not-leak'}],
                }, separators=(',', ':')).encode('utf-8')
                path = '/v1/messages/count_tokens'
                route_headers = build_signed_route_hint_headers(
                    body=body,
                    request_path=path,
                    catalog=catalog,
                    model_id='gpt-5.5',
                    session_ref='11111111-2222-4333-8444-555555555555',
                    secret='route-hint-secret',
                    now=None,
                    nonce='nonce-cp5-bridge-count-tokens',
                )
                listen_port = _free_port()
                summary = Path(td) / 'summary.jsonl'
                forwarder = RedactingForwarder(GuardConfig(
                    listen_host='127.0.0.1',
                    listen_port=listen_port,
                    upstream_base=f'http://127.0.0.1:{upstream_port}',
                    sub2api_auth='sub2api-entry-key',
                    summary_path=summary,
                    native_attestation_secret='native-attestation-test-secret',
                    route_hint_secret='route-hint-secret',
                    route_hint_catalog=catalog,
                    route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
                ))
                forwarder.start_background()
                try:
                    req = urllib.request.Request(
                        f'http://127.0.0.1:{listen_port}{path}',
                        data=body,
                        method='POST',
                        headers={
                            'content-type': 'application/json',
                            'x-claude-code-session-id': '11111111-2222-4333-8444-555555555555',
                            **route_headers,
                        },
                    )
                    with self.assertRaises(urllib.error.HTTPError) as ctx:
                        urllib.request.urlopen(req, timeout=5)
                    self.assertEqual(ctx.exception.code, 403)
                    self.assertEqual(CaptureHandler.requests, [])
                    dumped = summary.read_text(encoding='utf-8')
                    self.assertIn('"reason": "bridge_count_tokens_unavailable"', dumped)
                    self.assertNotIn('count-tokens-bridge-must-not-leak', dumped)
                    self.assertNotIn('event: message_start', dumped)
                finally:
                    forwarder.stop()
        finally:
            upstream.shutdown()

    def test_cp5_no_catalog_non_claude_model_fails_closed_before_upstream_or_native_attestation(self):
        class CaptureHandler(BaseHTTPRequestHandler):
            requests = []

            def log_message(self, *args):
                pass

            def do_POST(self):
                n = int(self.headers.get('content-length', '0') or 0)
                if n:
                    self.rfile.read(n)
                self.__class__.requests.append({
                    'path': self.path,
                    'headers': {key.lower(): value for key, value in self.headers.items()},
                })
                self.send_response(599)
                self.send_header('content-length', '0')
                self.end_headers()

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
                    native_attestation_secret='native-attestation-test-secret',
                ))
                forwarder.start_background()
                try:
                    body = json.dumps({
                        'model': 'gpt-5.5',
                        'messages': [{'role': 'user', 'content': 'no-catalog-gpt-must-not-leak'}],
                        'stream': True,
                    }, separators=(',', ':')).encode('utf-8')
                    req = urllib.request.Request(
                        f'http://127.0.0.1:{listen_port}/v1/messages?beta=true',
                        data=body,
                        method='POST',
                        headers={'content-type': 'application/json'},
                    )
                    with self.assertRaises(urllib.error.HTTPError) as ctx:
                        urllib.request.urlopen(req, timeout=5)
                    self.assertEqual(ctx.exception.code, 403)
                    self.assertEqual(CaptureHandler.requests, [])
                    dumped = summary.read_text(encoding='utf-8')
                    self.assertIn('"reason": "route_hint_required"', dumped)
                    self.assertNotIn('no-catalog-gpt-must-not-leak', dumped)
                    self.assertNotIn('native-attestation-test-secret', dumped)
                finally:
                    forwarder.stop()
        finally:
            upstream.shutdown()

    def test_cp5_native_route_rejects_openai_chat_fields_before_upstream_or_native_attestation(self):
        catalog = cp4_fixture_route_catalog(
            runtime_hash='sha256:' + '1' * 64,
            overlay_hash='sha256:' + '2' * 64,
            catalog_hash='sha256:' + '3' * 64,
            catalog_version='cp5-test-v1',
        )

        class CaptureHandler(BaseHTTPRequestHandler):
            requests = []

            def log_message(self, *args):
                pass

            def do_POST(self):
                n = int(self.headers.get('content-length', '0') or 0)
                if n:
                    self.rfile.read(n)
                self.__class__.requests.append({
                    'path': self.path,
                    'headers': {key.lower(): value for key, value in self.headers.items()},
                })
                self.send_response(599)
                self.send_header('content-length', '0')
                self.end_headers()

        upstream_port = _free_port()
        upstream = ThreadingHTTPServer(('127.0.0.1', upstream_port), CaptureHandler)
        threading.Thread(target=upstream.serve_forever, daemon=True).start()
        try:
            with tempfile.TemporaryDirectory() as td:
                body = json.dumps({
                    'model': 'claude-sonnet-4-6',
                    'messages': [{'role': 'user', 'content': 'native-chat-fields-must-not-leak'}],
                    'stream': True,
                    'max_tokens': 16,
                    'n': 2,
                    'stop': ['native-secret-stop'],
                    'stream_options': {'include_usage': True},
                    'user': 'native-user-leak',
                }, separators=(',', ':')).encode('utf-8')
                path = '/v1/messages?beta=true'
                route_headers = build_signed_route_hint_headers(
                    body=body,
                    request_path=path,
                    catalog=catalog,
                    model_id='claude-sonnet-4-6',
                    session_ref='11111111-2222-4333-8444-555555555555',
                    secret='route-hint-secret',
                    now=None,
                    nonce='nonce-cp5-native-chat-fields',
                )
                listen_port = _free_port()
                summary = Path(td) / 'summary.jsonl'
                forwarder = RedactingForwarder(GuardConfig(
                    listen_host='127.0.0.1',
                    listen_port=listen_port,
                    upstream_base=f'http://127.0.0.1:{upstream_port}',
                    sub2api_auth='sub2api-entry-key',
                    summary_path=summary,
                    native_attestation_secret='native-attestation-test-secret',
                    route_hint_secret='route-hint-secret',
                    route_hint_catalog=catalog,
                    route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
                ))
                forwarder.start_background()
                try:
                    req = urllib.request.Request(
                        f'http://127.0.0.1:{listen_port}{path}',
                        data=body,
                        method='POST',
                        headers={
                            'content-type': 'application/json',
                            'x-claude-code-session-id': '11111111-2222-4333-8444-555555555555',
                            **route_headers,
                        },
                    )
                    with self.assertRaises(urllib.error.HTTPError) as ctx:
                        urllib.request.urlopen(req, timeout=5)
                    self.assertEqual(ctx.exception.code, 403)
                    self.assertEqual(CaptureHandler.requests, [])
                    dumped = summary.read_text(encoding='utf-8')
                    self.assertIn('"reason": "messages_body_invalid"', dumped)
                    self.assertIn('"validation_error": "openai_only_body_shape"', dumped)
                    self.assertNotIn('native-chat-fields-must-not-leak', dumped)
                    self.assertNotIn('native-secret-stop', dumped)
                    self.assertNotIn('native-user-leak', dumped)
                    self.assertNotIn('native-attestation-test-secret', dumped)
                    self.assertNotIn('route-hint-secret', dumped)
                finally:
                    forwarder.stop()
        finally:
            upstream.shutdown()

    def test_cp5_bridge_skeleton_rejects_openai_chat_fields_without_upstream_or_prompt_leak(self):
        catalog = cp4_fixture_route_catalog(
            runtime_hash='sha256:' + '1' * 64,
            overlay_hash='sha256:' + '2' * 64,
            catalog_hash='sha256:' + '3' * 64,
            catalog_version='cp5-test-v1',
        )

        class CaptureHandler(BaseHTTPRequestHandler):
            requests = []

            def log_message(self, *args):
                pass

            def do_POST(self):
                n = int(self.headers.get('content-length', '0') or 0)
                if n:
                    self.rfile.read(n)
                self.__class__.requests.append({'path': self.path})
                self.send_response(599)
                self.send_header('content-length', '0')
                self.end_headers()

        upstream_port = _free_port()
        upstream = ThreadingHTTPServer(('127.0.0.1', upstream_port), CaptureHandler)
        threading.Thread(target=upstream.serve_forever, daemon=True).start()
        try:
            with tempfile.TemporaryDirectory() as td:
                body = json.dumps({
                    'model': 'gpt-5.5',
                    'messages': [{'role': 'user', 'content': 'chat-fields-must-not-leak'}],
                    'stream': True,
                    'n': 2,
                    'stop': ['secret-stop'],
                    'stream_options': {'include_usage': True},
                    'user': 'user-leak',
                }, separators=(',', ':')).encode('utf-8')
                path = '/v1/messages?beta=true'
                route_headers = build_signed_route_hint_headers(
                    body=body,
                    request_path=path,
                    catalog=catalog,
                    model_id='gpt-5.5',
                    session_ref='11111111-2222-4333-8444-555555555555',
                    secret='route-hint-secret',
                    now=None,
                    nonce='nonce-cp5-openai-chat-fields',
                )
                listen_port = _free_port()
                summary = Path(td) / 'summary.jsonl'
                forwarder = RedactingForwarder(GuardConfig(
                    listen_host='127.0.0.1',
                    listen_port=listen_port,
                    upstream_base=f'http://127.0.0.1:{upstream_port}',
                    sub2api_auth='sub2api-entry-key',
                    summary_path=summary,
                    native_attestation_secret='native-attestation-test-secret',
                    route_hint_secret='route-hint-secret',
                    route_hint_catalog=catalog,
                    route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
                ))
                forwarder.start_background()
                try:
                    req = urllib.request.Request(
                        f'http://127.0.0.1:{listen_port}{path}',
                        data=body,
                        method='POST',
                        headers={
                            'content-type': 'application/json',
                            'x-claude-code-session-id': '11111111-2222-4333-8444-555555555555',
                            **route_headers,
                        },
                    )
                    with self.assertRaises(urllib.error.HTTPError) as ctx:
                        urllib.request.urlopen(req, timeout=5)
                    self.assertEqual(ctx.exception.code, 403)
                    self.assertEqual(CaptureHandler.requests, [])
                    dumped = summary.read_text(encoding='utf-8')
                    self.assertIn('"validation_error": "openai_only_body_shape"', dumped)
                    self.assertNotIn('chat-fields-must-not-leak', dumped)
                    self.assertNotIn('secret-stop', dumped)
                    self.assertNotIn('user-leak', dumped)
                finally:
                    forwarder.stop()
        finally:
            upstream.shutdown()

    def test_cp5_bridge_skeleton_rejects_openai_responses_fields_without_upstream_or_prompt_leak(self):
        catalog = cp4_fixture_route_catalog(
            runtime_hash='sha256:' + '1' * 64,
            overlay_hash='sha256:' + '2' * 64,
            catalog_hash='sha256:' + '3' * 64,
            catalog_version='cp5-test-v1',
        )

        class CaptureHandler(BaseHTTPRequestHandler):
            requests = []

            def log_message(self, *args):
                pass

            def do_POST(self):
                n = int(self.headers.get('content-length', '0') or 0)
                if n:
                    self.rfile.read(n)
                self.__class__.requests.append({'path': self.path})
                self.send_response(599)
                self.send_header('content-length', '0')
                self.end_headers()

        upstream_port = _free_port()
        upstream = ThreadingHTTPServer(('127.0.0.1', upstream_port), CaptureHandler)
        threading.Thread(target=upstream.serve_forever, daemon=True).start()
        try:
            with tempfile.TemporaryDirectory() as td:
                body = json.dumps({
                    'model': 'gpt-5.5',
                    'messages': [{'role': 'user', 'content': 'responses-fields-must-not-leak'}],
                    'stream': True,
                    'reasoning': {'effort': 'low'},
                    'text': {'format': {'type': 'text'}},
                    'include': ['message.output_text.logprobs'],
                    'previous_response_id': 'resp_leak',
                    'truncation': 'auto',
                    'prompt_cache_key': 'cache-leak',
                    'max_output_tokens': 128,
                    'conversation': 'conv_leak',
                    'background': False,
                }, separators=(',', ':')).encode('utf-8')
                path = '/v1/messages?beta=true'
                route_headers = build_signed_route_hint_headers(
                    body=body,
                    request_path=path,
                    catalog=catalog,
                    model_id='gpt-5.5',
                    session_ref='11111111-2222-4333-8444-555555555555',
                    secret='route-hint-secret',
                    now=None,
                    nonce='nonce-cp5-openai-responses-fields',
                )
                listen_port = _free_port()
                summary = Path(td) / 'summary.jsonl'
                forwarder = RedactingForwarder(GuardConfig(
                    listen_host='127.0.0.1',
                    listen_port=listen_port,
                    upstream_base=f'http://127.0.0.1:{upstream_port}',
                    sub2api_auth='sub2api-entry-key',
                    summary_path=summary,
                    native_attestation_secret='native-attestation-test-secret',
                    route_hint_secret='route-hint-secret',
                    route_hint_catalog=catalog,
                    route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
                ))
                forwarder.start_background()
                try:
                    req = urllib.request.Request(
                        f'http://127.0.0.1:{listen_port}{path}',
                        data=body,
                        method='POST',
                        headers={
                            'content-type': 'application/json',
                            'x-claude-code-session-id': '11111111-2222-4333-8444-555555555555',
                            **route_headers,
                        },
                    )
                    with self.assertRaises(urllib.error.HTTPError) as ctx:
                        urllib.request.urlopen(req, timeout=5)
                    self.assertEqual(ctx.exception.code, 403)
                    self.assertEqual(CaptureHandler.requests, [])
                    dumped = summary.read_text(encoding='utf-8')
                    self.assertIn('"validation_error": "openai_only_body_shape"', dumped)
                    self.assertNotIn('responses-fields-must-not-leak', dumped)
                    self.assertNotIn('cache-leak', dumped)
                    self.assertNotIn('resp_leak', dumped)
                    self.assertNotIn('route-hint-secret', dumped)
                finally:
                    forwarder.stop()
        finally:
            upstream.shutdown()

    def test_cp5_bridge_skeleton_rejects_invalid_anthropic_tool_shapes_without_upstream_or_prompt_leak(self):
        catalog = cp4_fixture_route_catalog(
            runtime_hash='sha256:' + '1' * 64,
            overlay_hash='sha256:' + '2' * 64,
            catalog_hash='sha256:' + '3' * 64,
            catalog_version='cp5-test-v1',
        )

        class CaptureHandler(BaseHTTPRequestHandler):
            requests = []

            def log_message(self, *args):
                pass

            def do_POST(self):
                n = int(self.headers.get('content-length', '0') or 0)
                if n:
                    self.rfile.read(n)
                self.__class__.requests.append({'path': self.path})
                self.send_response(599)
                self.send_header('content-length', '0')
                self.end_headers()

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
                    max_messages=10,
                    native_attestation_secret='native-attestation-test-secret',
                    route_hint_secret='route-hint-secret',
                    route_hint_catalog=catalog,
                    route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
                ))
                forwarder.start_background()
                try:
                    cases = [
                        (
                            'tools_not_array',
                            {
                                'model': 'gpt-5.5',
                                'messages': [{'role': 'user', 'content': 'tools-object-must-not-leak'}],
                                'stream': True,
                                'tools': {'name': 'leak'},
                            },
                            'tool_shape_invalid',
                            'tools-object-must-not-leak',
                        ),
                        (
                            'tool_missing_name',
                            {
                                'model': 'gpt-5.5',
                                'messages': [{'role': 'user', 'content': 'missing-name-must-not-leak'}],
                                'stream': True,
                                'tools': [{'input_schema': {'type': 'object'}}],
                            },
                            'tool_shape_invalid',
                            'missing-name-must-not-leak',
                        ),
                        (
                            'tool_missing_input_schema',
                            {
                                'model': 'gpt-5.5',
                                'messages': [{'role': 'user', 'content': 'missing-schema-must-not-leak'}],
                                'stream': True,
                                'tools': [{'name': 'leak'}],
                            },
                            'tool_shape_invalid',
                            'missing-schema-must-not-leak',
                        ),
                        (
                            'tool_choice_tool_missing_name',
                            {
                                'model': 'gpt-5.5',
                                'messages': [{'role': 'user', 'content': 'bad-choice-must-not-leak'}],
                                'stream': True,
                                'tools': [{'name': 'get_weather', 'input_schema': {'type': 'object'}}],
                                'tool_choice': {'type': 'tool'},
                            },
                            'tool_choice_shape_invalid',
                            'bad-choice-must-not-leak',
                        ),
                        (
                            'tool_name_dot_not_anthropic_compatible',
                            {
                                'model': 'gpt-5.5',
                                'messages': [{'role': 'user', 'content': 'bad-name-must-not-leak'}],
                                'stream': True,
                                'tools': [{'name': 'unsafe.tool', 'input_schema': {'type': 'object'}}],
                            },
                            'tool_shape_invalid',
                            'bad-name-must-not-leak',
                        ),
                        (
                            'tool_choice_string_not_object',
                            {
                                'model': 'gpt-5.5',
                                'messages': [{'role': 'user', 'content': 'choice-string-must-not-leak'}],
                                'stream': True,
                                'tools': [{'name': 'get_weather', 'input_schema': {'type': 'object'}}],
                                'tool_choice': 'auto',
                            },
                            'tool_choice_shape_invalid',
                            'choice-string-must-not-leak',
                        ),
                        (
                            'tool_choice_names_unknown_tool',
                            {
                                'model': 'gpt-5.5',
                                'messages': [{'role': 'user', 'content': 'unknown-choice-must-not-leak'}],
                                'stream': True,
                                'tools': [{'name': 'get_weather', 'input_schema': {'type': 'object'}}],
                                'tool_choice': {'type': 'tool', 'name': 'unknown_tool'},
                            },
                            'tool_choice_shape_invalid',
                            'unknown-choice-must-not-leak',
                        ),
                    ]
                    path = '/v1/messages?beta=true'
                    for index, (name, payload, expected_error, secret_text) in enumerate(cases):
                        with self.subTest(name):
                            body = json.dumps(payload, separators=(',', ':')).encode('utf-8')
                            route_headers = build_signed_route_hint_headers(
                                body=body,
                                request_path=path,
                                catalog=catalog,
                                model_id='gpt-5.5',
                                session_ref='11111111-2222-4333-8444-555555555555',
                                secret='route-hint-secret',
                                now=None,
                                nonce=f'nonce-cp5-invalid-tool-shape-{index}',
                            )
                            req = urllib.request.Request(
                                f'http://127.0.0.1:{listen_port}{path}',
                                data=body,
                                method='POST',
                                headers={
                                    'content-type': 'application/json',
                                    'x-claude-code-session-id': '11111111-2222-4333-8444-555555555555',
                                    **route_headers,
                                },
                            )
                            with self.assertRaises(urllib.error.HTTPError) as ctx:
                                urllib.request.urlopen(req, timeout=5)
                            self.assertEqual(ctx.exception.code, 403)
                            dumped = summary.read_text(encoding='utf-8')
                            self.assertIn('"validation_error": "' + expected_error + '"', dumped)
                            self.assertNotIn(secret_text, dumped)
                            self.assertNotIn('"leak"', dumped)
                    self.assertEqual(CaptureHandler.requests, [])
                finally:
                    forwarder.stop()
        finally:
            upstream.shutdown()

    def test_cp4_route_hint_missing_or_spoofed_native_blocks_before_upstream(self):
        catalog = cp4_fixture_route_catalog(
            runtime_hash='sha256:' + '1' * 64,
            overlay_hash='sha256:' + '2' * 64,
            catalog_hash='sha256:' + '3' * 64,
            catalog_version='cp4-test-v1',
        )

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
            with tempfile.TemporaryDirectory() as td:
                listen_port = _free_port()
                summary = Path(td) / 'summary.jsonl'
                forwarder = RedactingForwarder(GuardConfig(
                    listen_host='127.0.0.1',
                    listen_port=listen_port,
                    upstream_base=f'http://127.0.0.1:{upstream_port}',
                    sub2api_auth='sub2api-entry-key',
                    summary_path=summary,
                    native_attestation_secret='native-attestation-test-secret',
                    route_hint_secret='route-hint-secret',
                    route_hint_catalog=catalog,
                    route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
                ))
                forwarder.start_background()
                try:
                    missing_req = urllib.request.Request(
                        f'http://127.0.0.1:{listen_port}/v1/messages?beta=true',
                        data=b'{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"hello"}],"max_tokens":16}',
                        method='POST',
                        headers={'content-type': 'application/json', 'x-claude-code-session-id': '11111111-2222-4333-8444-555555555555'},
                    )
                    with self.assertRaises(urllib.error.HTTPError) as missing_ctx:
                        urllib.request.urlopen(missing_req, timeout=5)
                    self.assertEqual(missing_ctx.exception.code, 403)

                    spoof_body = b'{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"hello"}],"max_tokens":16}'
                    spoof_headers = build_signed_route_hint_headers(
                        body=spoof_body,
                        request_path='/v1/messages?beta=true',
                        catalog=catalog,
                        model_id='deepseek-v4-pro',
                        session_ref='11111111-2222-4333-8444-555555555555',
                        secret='route-hint-secret',
                        now=None,
                        nonce='nonce-spoof-native',
                        route='claude_code_native',
                        client_type='claude_code_native',
                        native_attestation_allowed=True,
                        formal_pool_allowed=True,
                    )
                    spoof_req = urllib.request.Request(
                        f'http://127.0.0.1:{listen_port}/v1/messages?beta=true',
                        data=spoof_body,
                        method='POST',
                        headers={'content-type': 'application/json', 'x-claude-code-session-id': '11111111-2222-4333-8444-555555555555', **spoof_headers},
                    )
                    with self.assertRaises(urllib.error.HTTPError) as spoof_ctx:
                        urllib.request.urlopen(spoof_req, timeout=5)
                    self.assertEqual(spoof_ctx.exception.code, 403)
                    mismatch_body = b'{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}],"max_tokens":16}'
                    mismatch_headers = build_signed_route_hint_headers(
                        body=spoof_body,
                        request_path='/v1/messages?beta=true',
                        catalog=catalog,
                        model_id='deepseek-v4-pro',
                        session_ref='11111111-2222-4333-8444-555555555555',
                        secret='route-hint-secret',
                        now=None,
                        nonce='nonce-model-mismatch',
                    )
                    mismatch_req = urllib.request.Request(
                        f'http://127.0.0.1:{listen_port}/v1/messages?beta=true',
                        data=mismatch_body,
                        method='POST',
                        headers={'content-type': 'application/json', 'x-claude-code-session-id': '11111111-2222-4333-8444-555555555555', **mismatch_headers},
                    )
                    with self.assertRaises(urllib.error.HTTPError) as mismatch_ctx:
                        urllib.request.urlopen(mismatch_req, timeout=5)
                    self.assertEqual(mismatch_ctx.exception.code, 403)
                    self.assertEqual(CaptureHandler.count, 0)
                    dumped = summary.read_text(encoding='utf-8')
                    self.assertIn('route_hint_unavailable', dumped)
                    self.assertIn('route_hint_invalid', dumped)
                    self.assertNotIn('route-hint-secret', dumped)
                    self.assertNotIn('hello', dumped)
                finally:
                    forwarder.stop()
        finally:
            upstream.shutdown()

    def test_cp4_native_route_hint_adds_native_attestation_only_for_native(self):
        catalog = cp4_fixture_route_catalog(
            runtime_hash='sha256:' + '1' * 64,
            overlay_hash='sha256:' + '2' * 64,
            catalog_hash='sha256:' + '3' * 64,
            catalog_version='cp4-test-v1',
        )

        class CaptureHandler(BaseHTTPRequestHandler):
            requests = []

            def log_message(self, *args):
                pass

            def do_POST(self):
                n = int(self.headers.get('content-length', '0') or 0)
                if n:
                    self.rfile.read(n)
                self.__class__.requests.append({key.lower(): value for key, value in self.headers.items()})
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
                body = b'{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}],"max_tokens":16}'
                path = '/v1/messages?beta=true'
                route_headers = build_signed_route_hint_headers(
                    body=body,
                    request_path=path,
                    catalog=catalog,
                    model_id='claude-sonnet-4-6',
                    session_ref='11111111-2222-4333-8444-555555555555',
                    secret='route-hint-secret',
                    now=None,
                    nonce='nonce-native-forward',
                )
                listen_port = _free_port()
                forwarder = RedactingForwarder(GuardConfig(
                    listen_host='127.0.0.1',
                    listen_port=listen_port,
                    upstream_base=f'http://127.0.0.1:{upstream_port}',
                    sub2api_auth='sub2api-entry-key',
                    summary_path=Path(td) / 'summary.jsonl',
                    native_attestation_secret='native-attestation-test-secret',
                    route_hint_secret='route-hint-secret',
                    route_hint_catalog=catalog,
                    route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
                ))
                forwarder.start_background()
                try:
                    req = urllib.request.Request(
                        f'http://127.0.0.1:{listen_port}{path}',
                        data=body,
                        method='POST',
                        headers={'content-type': 'application/json', 'x-claude-code-session-id': '11111111-2222-4333-8444-555555555555', **route_headers},
                    )
                    with urllib.request.urlopen(req, timeout=5) as resp:
                        self.assertEqual(resp.status, 200)
                    self.assertEqual(len(CaptureHandler.requests), 1)
                    forwarded = CaptureHandler.requests[0]
                    self.assertEqual(forwarded['x-sub2api-client-type'], 'claude_code_native')
                    self.assertEqual(forwarded['x-sub2api-route'], 'claude_code_native')
                    self.assertIn('x-sub2api-native-attestation', forwarded)
                    self.assertIn('x-sub2api-native-signature', forwarded)
                    self.assertEqual(forwarded['x-sub2api-guard-attested'], 'true')
                finally:
                    forwarder.stop()
        finally:
            upstream.shutdown()

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
                        route_hint_secret=_ROUTE_HINT_SECRET,
                        route_hint_catalog=_ROUTE_HINT_CATALOG,
                        route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
                    ))
                    forwarder.start_background()
                    try:
                        body = b'{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}],"max_tokens":16}'
                        path = '/v1/messages?beta=true'
                        req = urllib.request.Request(
                            f'http://127.0.0.1:{listen_port}{path}',
                            data=body,
                            method='POST',
                            headers={
                                'content-type': 'application/json',
                                'x-claude-code-session-id': _DEFAULT_SESSION_REF,
                                **_native_route_headers(body, path, nonce='missing-native-attestation'),
                            },
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
                    native_attestation_secret='native-attestation-test-secret',
                    route_hint_secret=_ROUTE_HINT_SECRET,
                    route_hint_catalog=_ROUTE_HINT_CATALOG,
                    route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
                ))
                forwarder.start_background()
                try:
                    body = json.dumps({
                        'model': 'claude-sonnet-4-6',
                        'messages': [{'role': 'user', 'content': 'raw-prompt-marker'}],
                        'max_tokens': 64,
                        'tools': [{'name': 'calculator', 'input_schema': {'type': 'object'}}],
                        'output_config': {'format': 'json'},
                    }).encode('utf-8')
                    path = '/v1/messages?beta=true'
                    req = urllib.request.Request(
                        f'http://127.0.0.1:{listen_port}{path}',
                        data=body,
                        method='POST',
                        headers={
                            'Authorization': 'Bearer secret-token-marker',
                            'x-api-key': 'local-api-key-marker',
                            'Cookie': 'session=cookie-marker',
                            'Proxy-Authorization': 'Basic proxy-credential-marker',
                            'content-type': 'application/json',
                            'x-claude-code-session-id': _DEFAULT_SESSION_REF,
                            **_native_route_headers(body, path, nonce='deep-summary-forward'),
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
                    native_attestation_secret='native-attestation-test-secret',
                    route_hint_secret=_ROUTE_HINT_SECRET,
                    route_hint_catalog=_ROUTE_HINT_CATALOG,
                    route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
                ), execution_controller=controller)
                forwarder.start_background()
                try:
                    body = b'{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}],"max_tokens":16}'
                    path = '/v1/messages?beta=true'
                    req = urllib.request.Request(
                        f'http://127.0.0.1:{listen_port}{path}',
                        data=body,
                        method='POST',
                        headers={
                            'content-type': 'application/json',
                            'x-claude-code-session-id': _DEFAULT_SESSION_REF,
                            **_native_route_headers(body, path, nonce='canary-http-error'),
                        },
                    )
                    with self.assertRaises(urllib.error.HTTPError) as ctx:
                        urllib.request.urlopen(req, timeout=5)
                    self.assertEqual(ctx.exception.code, 429)
                    deadline = time.time() + 5
                    while sleeper.poll() is None and time.time() < deadline:
                        time.sleep(0.05)
                    self.assertIsNotNone(sleeper.poll())
                    summary_path = Path(td) / 'summary.jsonl'
                    summary_text = summary_path.read_text(encoding='utf-8')
                    deadline = time.time() + 5
                    while 'execution_controller_stop_requested' not in summary_text and time.time() < deadline:
                        time.sleep(0.05)
                        summary_text = summary_path.read_text(encoding='utf-8')
                    self.assertIn('execution_controller_stop_requested', summary_text)
                    self.assertEqual(CaptureHandler.count, 1)
                finally:
                    forwarder.stop()
                    if sleeper.poll() is None:
                        sleeper.kill()
                        sleeper.wait(timeout=5)
        finally:
            upstream.shutdown()


    def test_cp6_deepseek_background_live_bridge_forward_has_zero_native_egress(self):
        native = RouteCatalogEntry(
            model_id='claude-sonnet-4-6',
            provider='claude',
            route='claude_code_native',
            client_type='claude_code_native',
            live_enabled=True,
            formal_pool_allowed=True,
            native_attestation_allowed=True,
            provider_owner='zhumeng_managed',
            credential_scope='formal_pool',
            gateway_location='cloud',
        )
        bridge = RouteCatalogEntry(
            model_id='deepseek-v4-flash',
            provider='deepseek',
            route='deepseek_bridge',
            client_type='claude_code_bridge_deepseek',
            live_enabled=True,
            formal_pool_allowed=False,
            native_attestation_allowed=False,
            provider_owner='zhumeng_managed',
            credential_scope='bridge_pool',
            gateway_location='cloud',
        )
        catalog = RouteCatalog(
            runtime_hash='sha256:' + '1' * 64,
            overlay_hash='sha256:' + '2' * 64,
            catalog_hash='sha256:' + '3' * 64,
            catalog_version='cp6-live-bridge-loopback-v1',
            entries={'claude-sonnet-4-6': native, 'deepseek-v4-flash': bridge},
        )

        class CaptureHandler(BaseHTTPRequestHandler):
            requests = []

            def log_message(self, *args):
                pass

            def do_POST(self):
                n = int(self.headers.get('content-length', '0') or 0)
                body = self.rfile.read(n) if n else b''
                self.__class__.requests.append({
                    'path': self.path,
                    'headers': {key.lower(): value for key, value in self.headers.items()},
                    'body': body,
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
                    native_attestation_secret='native-attestation-test-secret',
                    route_hint_secret='route-hint-secret',
                    route_hint_catalog=catalog,
                    route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
                    max_messages=0,
                ))
                forwarder.start_background()
                try:
                    for index, task in enumerate(['title', 'compact', 'summary', 'probe', 'fast', 'simple', 'haiku']):
                        body = json.dumps({
                            'model': 'deepseek-v4-flash',
                            'messages': [{'role': 'user', 'content': f'background {task}'}],
                            'metadata': {'zhumeng_background_task': task},
                            'max_tokens': 16,
                            'stream': True,
                        }, separators=(',', ':')).encode('utf-8')
                        path = '/v1/messages?beta=true'
                        route_headers = build_signed_route_hint_headers(
                            body=body,
                            request_path=path,
                            catalog=catalog,
                            model_id='deepseek-v4-flash',
                            session_ref='11111111-2222-4333-8444-555555555555',
                            secret='route-hint-secret',
                            nonce=f'nonce-cp6-live-background-{index}',
                        )
                        req = urllib.request.Request(
                            f'http://127.0.0.1:{listen_port}{path}',
                            data=body,
                            method='POST',
                            headers={
                                'content-type': 'application/json',
                                'x-claude-code-session-id': '11111111-2222-4333-8444-555555555555',
                                **route_headers,
                            },
                        )
                        with urllib.request.urlopen(req, timeout=5) as resp:
                            self.assertEqual(resp.status, 200)
                    self.assertEqual(len(CaptureHandler.requests), 7)
                    for captured in CaptureHandler.requests:
                        headers = captured['headers']
                        self.assertEqual(headers.get('x-sub2api-client-type'), 'claude_code_bridge_deepseek')
                        self.assertEqual(headers.get('x-sub2api-route'), 'deepseek_bridge')
                        self.assertNotIn('x-sub2api-native-attestation', headers)
                        self.assertNotIn('x-sub2api-native-signature', headers)
                    rows = [json.loads(line) for line in summary.read_text(encoding='utf-8').splitlines() if line.strip()]
                    native_egress = [row for row in rows if row.get('client_type') == 'claude_code_native' or row.get('native_attested') is True or row.get('formal_pool_allowed') is True]
                    self.assertEqual(native_egress, [])
                    forwarded = [row for row in rows if row.get('event') == 'messages_upstream_response']
                    self.assertEqual(len(forwarded), 7)
                finally:
                    forwarder.stop()
        finally:
            upstream.shutdown()


    def test_cp6_deepseek_background_tasks_have_zero_native_egress(self):
        catalog = cp4_fixture_route_catalog(
            runtime_hash='sha256:' + '1' * 64,
            overlay_hash='sha256:' + '2' * 64,
            catalog_hash='sha256:' + '3' * 64,
            catalog_version='cp6-test-v1',
        )

        class CaptureHandler(BaseHTTPRequestHandler):
            requests = []

            def log_message(self, *args):
                pass

            def do_POST(self):
                n = int(self.headers.get('content-length', '0') or 0)
                if n:
                    self.rfile.read(n)
                self.__class__.requests.append({'path': self.path})
                self.send_response(599)
                self.send_header('content-length', '0')
                self.end_headers()

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
                    native_attestation_secret='native-attestation-test-secret',
                    route_hint_secret='route-hint-secret',
                    route_hint_catalog=catalog,
                    route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
                    max_messages=0,
                ))
                forwarder.start_background()
                try:
                    for index, task in enumerate(['title', 'compact', 'summary', 'probe', 'fast', 'simple', 'haiku']):
                        body = json.dumps({
                            'model': 'deepseek-v4-flash',
                            'messages': [{'role': 'user', 'content': f'background {task}'}],
                            'metadata': {'zhumeng_background_task': task},
                            'max_tokens': 16,
                            'stream': True,
                        }, separators=(',', ':')).encode('utf-8')
                        path = '/v1/messages?beta=true'
                        route_headers = build_signed_route_hint_headers(
                            body=body,
                            request_path=path,
                            catalog=catalog,
                            model_id='deepseek-v4-flash',
                            session_ref='11111111-2222-4333-8444-555555555555',
                            secret='route-hint-secret',
                            nonce=f'nonce-cp6-background-{index}',
                        )
                        req = urllib.request.Request(
                            f'http://127.0.0.1:{listen_port}{path}',
                            data=body,
                            method='POST',
                            headers={
                                'content-type': 'application/json',
                                'x-claude-code-session-id': '11111111-2222-4333-8444-555555555555',
                                **route_headers,
                            },
                        )
                        with urllib.request.urlopen(req, timeout=5) as resp:
                            self.assertEqual(resp.status, 200)
                            self.assertEqual(resp.headers.get('content-type'), 'text/event-stream')
                    self.assertEqual(len(CaptureHandler.requests), 0)
                    rows = [json.loads(line) for line in summary.read_text(encoding='utf-8').splitlines() if line.strip()]
                    native_egress = [row for row in rows if row.get('client_type') == 'claude_code_native' or row.get('native_attested') is True or row.get('formal_pool_allowed') is True]
                    self.assertEqual(native_egress, [])
                    audit_rows = [row for row in rows if row.get('event') in {'request', 'messages_bridge_skeleton_response'}]
                    self.assertGreaterEqual(len(audit_rows), 14)
                    self.assertTrue(all(row.get('client_type') == 'claude_code_bridge_deepseek' for row in audit_rows))
                    self.assertTrue(all(row.get('credential_scope') == 'bridge_pool' for row in audit_rows))
                finally:
                    forwarder.stop()
        finally:
            upstream.shutdown()

    def test_cp6_claude_profile_then_deepseek_background_switch_has_zero_native_egress(self):
        catalog = cp4_fixture_route_catalog(
            runtime_hash='sha256:' + '1' * 64,
            overlay_hash='sha256:' + '2' * 64,
            catalog_hash='sha256:' + '3' * 64,
            catalog_version='cp6-test-v1',
        )

        class CaptureHandler(BaseHTTPRequestHandler):
            requests = []

            def log_message(self, *args):
                pass

            def do_POST(self):
                n = int(self.headers.get('content-length', '0') or 0)
                if n:
                    self.rfile.read(n)
                self.__class__.requests.append({'path': self.path})
                self.send_response(599)
                self.send_header('content-length', '0')
                self.end_headers()

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
                    native_attestation_secret='native-attestation-test-secret',
                    route_hint_secret='route-hint-secret',
                    route_hint_catalog=catalog,
                    route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
                ))
                forwarder.start_background()
                try:
                    path = '/v1/messages?beta=true'
                    mismatch_body = json.dumps({
                        'model': 'claude-sonnet-4-6',
                        'messages': [{'role': 'user', 'content': 'stale hardcoded haiku'}],
                        'max_tokens': 16,
                        'stream': True,
                    }, separators=(',', ':')).encode('utf-8')
                    mismatch_headers = build_signed_route_hint_headers(
                        body=mismatch_body,
                        request_path=path,
                        catalog=catalog,
                        model_id='deepseek-v4-flash',
                        session_ref='11111111-2222-4333-8444-555555555555',
                        secret='route-hint-secret',
                        nonce='nonce-cp6-switch-mismatch',
                    )
                    mismatch_req = urllib.request.Request(
                        f'http://127.0.0.1:{listen_port}{path}',
                        data=mismatch_body,
                        method='POST',
                        headers={
                            'content-type': 'application/json',
                            'x-claude-code-session-id': '11111111-2222-4333-8444-555555555555',
                            **mismatch_headers,
                        },
                    )
                    with self.assertRaises(urllib.error.HTTPError) as mismatch_ctx:
                        urllib.request.urlopen(mismatch_req, timeout=5)
                    self.assertEqual(mismatch_ctx.exception.code, 403)
                    body = json.dumps({
                        'model': 'deepseek-v4-flash',
                        'messages': [{'role': 'user', 'content': 'title after switch'}],
                        'metadata': {'zhumeng_active_profile': 'deepseek', 'zhumeng_background_task': 'title'},
                        'max_tokens': 16,
                        'stream': True,
                    }, separators=(',', ':')).encode('utf-8')
                    route_headers = build_signed_route_hint_headers(
                        body=body,
                        request_path=path,
                        catalog=catalog,
                        model_id='deepseek-v4-flash',
                        session_ref='11111111-2222-4333-8444-555555555555',
                        secret='route-hint-secret',
                        nonce='nonce-cp6-switch-deepseek',
                    )
                    req = urllib.request.Request(
                        f'http://127.0.0.1:{listen_port}{path}',
                        data=body,
                        method='POST',
                        headers={
                            'content-type': 'application/json',
                            'x-claude-code-session-id': '11111111-2222-4333-8444-555555555555',
                            **route_headers,
                        },
                    )
                    with urllib.request.urlopen(req, timeout=5) as resp:
                        self.assertEqual(resp.status, 200)
                    self.assertEqual(len(CaptureHandler.requests), 0)
                    rows = [json.loads(line) for line in summary.read_text(encoding='utf-8').splitlines() if line.strip()]
                    native_egress = [row for row in rows if row.get('client_type') == 'claude_code_native' or row.get('native_attested') is True or row.get('formal_pool_allowed') is True]
                    self.assertEqual(native_egress, [])
                    self.assertIn('"provider": "deepseek"', summary.read_text(encoding='utf-8'))
                finally:
                    forwarder.stop()
        finally:
            upstream.shutdown()


def _free_port():
    sock = socket.socket()
    sock.bind(('127.0.0.1', 0))
    port = sock.getsockname()[1]
    sock.close()
    return port


def root_dir() -> str:
    return str(Path(__file__).resolve().parents[2])


if __name__ == '__main__':
    unittest.main()


def _wait_for_file(path: Path, timeout: float = 5.0) -> None:
    deadline = time.time() + timeout
    while time.time() < deadline:
        if path.exists():
            return
        time.sleep(0.05)
    raise AssertionError(f'file was not created in time: {path}')
