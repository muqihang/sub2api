import argparse
import base64
import copy
import io
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
    _bridge_rebound_route_hint_headers,
    _cli_session_budget_ledger,
    body_summary,
    build_native_messages_attestation_headers,
    classify_request,
    cp5_bridge_skeleton_sse_body,
    deep_body_summary,
    redact_headers,
    validate_cp5_bridge_body,
    main as guard_main,
)
from tools.cli_control_plane_intent import IntentValidationError
from tools.claude_code_route_trust import (
    RouteCatalog,
    RouteCatalogEntry,
    RouteHintReplayCache,
    build_signed_route_hint_headers,
    cp4_fixture_route_catalog,
    route_catalog_content_hash,
    route_hint_signature_diagnostic,
    verify_signed_route_hint_headers,
    verify_signed_route_hint_headers_with_bridge_body_rebinding,
)
from tools.cli_control_plane_policy import load_default_policy


_ROUTE_HINT_SECRET = 'route-hint-secret'
_ROUTE_HINT_CATALOG = cp4_fixture_route_catalog(catalog_version='cp4-cli-fixture-v1')
_ROUTE_HINT_CATALOG_WITH_BRIDGE_LIVE = cp4_fixture_route_catalog(
    catalog_version='cp4-cli-fixture-v1',
    bridge_live_models=('claude-code-bridge-gpt-5.5',),
)
_DEFAULT_SESSION_REF = '11111111-2222-4333-8444-555555555555'


def _test_jwt_with_exp(exp: int) -> str:
    header = base64.urlsafe_b64encode(b'{"alg":"none"}').decode('ascii').rstrip('=')
    payload = base64.urlsafe_b64encode(json.dumps({'exp': exp}).encode('utf-8')).decode('ascii').rstrip('=')
    return f'{header}.{payload}.signature'


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


def _bridge_route_headers(body: bytes, path: str, catalog, *, session_ref: str = _DEFAULT_SESSION_REF, nonce: str | None = None) -> dict[str, str]:
    return build_signed_route_hint_headers(
        body=body,
        request_path=path,
        catalog=catalog,
        model_id='claude-code-bridge-gpt-5.5',
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



    def test_main_accepts_bridge_live_catalog_hash_from_env(self):
        runtime_hash = 'sha256:' + '1' * 64
        overlay_hash = 'sha256:' + '2' * 64
        bridge_live_models = ('claude-code-bridge-gpt-5.5', 'claude-code-bridge-deepseek-v4-pro')
        catalog = cp4_fixture_route_catalog(
            runtime_hash=runtime_hash,
            overlay_hash=overlay_hash,
            catalog_hash='sha256:' + '0' * 64,
            catalog_version='cp4-cli-fixture-v1',
            bridge_live_models=bridge_live_models,
        )
        catalog_hash = route_catalog_content_hash(catalog)
        env = {
            'ROUTE_HINT_SECRET': 'route-hint-secret',
            'ZHUMENG_CLAUDE_RUNTIME_HASH': runtime_hash,
            'ZHUMENG_CLAUDE_OVERLAY_HASH': overlay_hash,
            'ZHUMENG_CLAUDE_CATALOG_HASH': catalog_hash,
            'ZHUMENG_CLAUDE_BRIDGE_LIVE_MODELS': ','.join(bridge_live_models),
        }
        stdout = io.StringIO()
        stderr = io.StringIO()

        with tempfile.TemporaryDirectory() as td, \
             unittest.mock.patch.dict('os.environ', env, clear=False), \
             unittest.mock.patch('sys.stdout', stdout), \
             unittest.mock.patch('sys.stderr', stderr), \
             unittest.mock.patch('tools.cli_control_plane_guard.time.sleep', side_effect=KeyboardInterrupt):
            code = guard_main([
                '--listen-port', str(_free_port()),
                '--upstream-base', 'http://127.0.0.1:18080',
                '--sub2api-auth', 'managed-access-token',
                '--native-attestation',
                '--route-hint-secret-env', 'ROUTE_HINT_SECRET',
                '--summary-path', str(Path(td) / 'summary.jsonl'),
            ])

        self.assertEqual(code, 0)
        self.assertIn('"listen": "http://127.0.0.1:', stdout.getvalue())
        self.assertNotIn('route hint catalog hash mismatch', stderr.getvalue())

    def test_main_rejects_bridge_live_catalog_hash_without_matching_env(self):
        runtime_hash = 'sha256:' + '1' * 64
        overlay_hash = 'sha256:' + '2' * 64
        catalog = cp4_fixture_route_catalog(
            runtime_hash=runtime_hash,
            overlay_hash=overlay_hash,
            catalog_hash='sha256:' + '0' * 64,
            catalog_version='cp4-cli-fixture-v1',
            bridge_live_models=('claude-code-bridge-gpt-5.5', 'claude-code-bridge-deepseek-v4-pro'),
        )
        env = {
            'ROUTE_HINT_SECRET': 'route-hint-secret',
            'ZHUMENG_CLAUDE_RUNTIME_HASH': runtime_hash,
            'ZHUMENG_CLAUDE_OVERLAY_HASH': overlay_hash,
            'ZHUMENG_CLAUDE_CATALOG_HASH': route_catalog_content_hash(catalog),
            'ZHUMENG_CLAUDE_BRIDGE_LIVE_MODELS': '',
        }
        stderr = io.StringIO()
        with tempfile.TemporaryDirectory() as td, \
             unittest.mock.patch.dict('os.environ', env, clear=False), \
             unittest.mock.patch('sys.stderr', stderr):
            code = guard_main([
                '--listen-port', str(_free_port()),
                '--upstream-base', 'http://127.0.0.1:18080',
                '--sub2api-auth', 'managed-access-token',
                '--route-hint-secret-env', 'ROUTE_HINT_SECRET',
                '--summary-path', str(Path(td) / 'summary.jsonl'),
            ])

        self.assertEqual(code, 2)
        self.assertIn('route hint catalog hash mismatch', stderr.getvalue())

    def test_model_discovery_returns_route_catalog_overlay_without_live_bridge_enablement(self):
        with tempfile.TemporaryDirectory() as td:
            listen_port = _free_port()
            forwarder = RedactingForwarder(GuardConfig(
                listen_host='127.0.0.1',
                listen_port=listen_port,
                upstream_base='http://127.0.0.1:18080',
                sub2api_auth='sk-sub2api-dedicated-claude-code-key',
                summary_path=Path(td) / 'summary.jsonl',
                native_attestation_secret='native-attestation-test-secret',
                route_hint_secret=_ROUTE_HINT_SECRET,
                route_hint_catalog=_ROUTE_HINT_CATALOG,
                route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
            ))
            forwarder.start_background()
            try:
                request = urllib.request.Request(
                    f'http://127.0.0.1:{listen_port}/v1/models?limit=1000',
                    method='GET',
                    headers={'Authorization': 'Bearer local-token-that-must-not-leak'},
                )
                with urllib.request.urlopen(request, timeout=5) as resp:
                    self.assertEqual(resp.status, 200)
                    payload = json.loads(resp.read().decode('utf-8'))
                summary_dump = (Path(td) / 'summary.jsonl').read_text(encoding='utf-8')
            finally:
                forwarder.stop()

        models = {item['id']: item for item in payload['data']}
        expected_bridge_models = {
            'claude-code-bridge-gpt-5.5',
            'claude-code-bridge-gpt-5.4',
            'claude-code-bridge-gpt-5.4-mini',
            'claude-code-bridge-deepseek-v4-pro',
            'claude-code-bridge-deepseek-v4-flash',
            'claude-code-bridge-agnes-2.0-flash',
            'claude-code-bridge-glm-5.2-1m',
            'claude-code-bridge-kimi-k2.7-code',
        }
        self.assertEqual(set(models), expected_bridge_models)
        self.assertNotIn('claude-sonnet-4-6', models)
        self.assertNotIn('claude-opus-4-8', models)
        for model_id, item in models.items():
            self.assertTrue(model_id.startswith('claude-code-bridge-'))
            self.assertTrue(item['x_zhumeng_client_type'].startswith('claude_code_bridge_'))
            self.assertFalse(item['x_zhumeng_live_enabled'])
            self.assertFalse(item['x_zhumeng_formal_pool_allowed'])
            self.assertFalse(item['x_zhumeng_native_attestation_allowed'])
            self.assertEqual(item['x_zhumeng_credential_scope'], 'bridge_pool')
        self.assertEqual(models['claude-code-bridge-gpt-5.5']['x_zhumeng_route'], 'openai_bridge')
        self.assertEqual(models['claude-code-bridge-gpt-5.4']['x_zhumeng_provider'], 'openai')
        self.assertEqual(models['claude-code-bridge-gpt-5.4-mini']['x_zhumeng_provider'], 'openai')
        self.assertEqual(models['claude-code-bridge-deepseek-v4-pro']['x_zhumeng_provider'], 'deepseek')
        self.assertEqual(models['claude-code-bridge-deepseek-v4-flash']['x_zhumeng_provider'], 'deepseek')
        self.assertEqual(models['claude-code-bridge-agnes-2.0-flash']['x_zhumeng_provider'], 'agnes')
        self.assertEqual(models['claude-code-bridge-glm-5.2-1m']['x_zhumeng_provider'], 'zai_glm')
        self.assertEqual(models['claude-code-bridge-kimi-k2.7-code']['x_zhumeng_provider'], 'kimi')
        self.assertIn('model_discovery_overlay', summary_dump)
        self.assertNotIn('local-token-that-must-not-leak', summary_dump)

    def test_root_dir_is_current_repo_root(self):
        self.assertEqual(Path(root_dir()).resolve(), Path(__file__).resolve().parents[2])

    def test_bridge_live_messages_with_dedicated_sub2api_key_omits_managed_device_headers(self):
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
                sub2api_auth='sk-sub2api-dedicated-claude-code-key',
                summary_path=Path(td) / 'summary.jsonl',
                native_attestation_secret='native-attestation-test-secret',
                route_hint_secret=_ROUTE_HINT_SECRET,
                route_hint_catalog=_ROUTE_HINT_CATALOG_WITH_BRIDGE_LIVE,
                route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
                managed_session_id='managed-session',
                device_id='9',
                agent_version='0.1.0',
            ))
            forwarder.start_background()
            try:
                body = b'{"model":"claude-code-bridge-gpt-5.5","messages":[{"role":"user","content":"hello"}],"max_tokens":16}'
                path = '/v1/messages?beta=true'
                request = urllib.request.Request(
                    f'http://127.0.0.1:{listen_port}{path}',
                    data=body,
                    method='POST',
                    headers={
                        'content-type': 'application/json',
                        'User-Agent': 'claude-cli/2.1.150 (external, sdk-cli)',
                        'x-claude-code-session-id': _DEFAULT_SESSION_REF,
                        **_bridge_route_headers(body, path, _ROUTE_HINT_CATALOG_WITH_BRIDGE_LIVE, nonce='dedicated-bridge-no-managed-headers'),
                    },
                )
                with urllib.request.urlopen(request, timeout=5) as resp:
                    self.assertEqual(resp.status, 200)
            finally:
                forwarder.stop()
                upstream.shutdown()
                upstream.server_close()

        self.assertEqual(seen['authorization'], 'Bearer sk-sub2api-dedicated-claude-code-key')
        self.assertIsNone(seen['managed_session'])
        self.assertIsNone(seen['device_id'])

    def test_native_messages_use_managed_access_token_and_add_managed_device_headers(self):
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
                sub2api_auth='sk-sub2api-dedicated-claude-code-key',
                native_managed_access_token='eyJhbGciOiJIUzI1NiJ9.managed-access-token.signature',
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
                        **_native_route_headers(body, path, nonce='managed-access-token-headers'),
                    },
                )
                with urllib.request.urlopen(request, timeout=5) as resp:
                    self.assertEqual(resp.status, 200)
            finally:
                forwarder.stop()
                upstream.shutdown()
                upstream.server_close()

        self.assertEqual(seen['authorization'], 'Bearer sk-sub2api-dedicated-claude-code-key')
        self.assertEqual(seen['managed_session'], 'managed-session')
        self.assertEqual(seen['device_id'], '9')

    def test_native_messages_read_latest_native_managed_token_from_state_file(self):
        seen = {}

        class Upstream(BaseHTTPRequestHandler):
            def do_POST(self):
                _ = self.rfile.read(int(self.headers.get('content-length', '0')))
                seen['authorization'] = self.headers.get('Authorization')
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
            state_path = Path(td) / 'state.json'
            state_path.write_text(
                json.dumps({'claude_code_native_access_token': 'latest-native-token-from-state'}),
                encoding='utf-8',
            )
            listen_port = _free_port()
            forwarder = RedactingForwarder(GuardConfig(
                listen_host='127.0.0.1',
                listen_port=listen_port,
                upstream_base=f'http://127.0.0.1:{upstream_port}',
                sub2api_auth='sk-sub2api-dedicated-claude-code-key',
                native_managed_access_token='stale-native-token-from-env',
                native_managed_state_path=state_path,
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
                        'User-Agent': 'claude-cli/2.1.177 (external, sdk-cli)',
                        'x-claude-code-session-id': _DEFAULT_SESSION_REF,
                        **_native_route_headers(body, path, nonce='latest-state-native-token'),
                    },
                )
                with urllib.request.urlopen(request, timeout=5) as resp:
                    self.assertEqual(resp.status, 200)
            finally:
                forwarder.stop()
                upstream.shutdown()
                upstream.server_close()

        self.assertEqual(seen['authorization'], 'Bearer sk-sub2api-dedicated-claude-code-key')

    def test_native_messages_refresh_expiring_state_token_before_forwarding(self):
        seen = {'refresh_calls': 0}
        now = int(time.time())
        stale_token = _test_jwt_with_exp(now + 10)
        refreshed_token = _test_jwt_with_exp(now + 900)

        class Upstream(BaseHTTPRequestHandler):
            def do_POST(self):
                length = int(self.headers.get('content-length', '0'))
                body = self.rfile.read(length) if length else b''
                if self.path == '/api/v1/codex/devices/refresh':
                    seen['refresh_calls'] += 1
                    seen['refresh_body'] = json.loads(body.decode('utf-8'))
                    payload = {
                        'data': {
                            'access_token': refreshed_token,
                            'refresh_token': 'next-refresh-token',
                            'managed_session_id': 'next-managed-session',
                            'expires_at': '2026-06-18T13:00:00Z',
                        }
                    }
                    data = json.dumps(payload).encode('utf-8')
                    self.send_response(200)
                    self.send_header('content-type', 'application/json')
                    self.send_header('content-length', str(len(data)))
                    self.end_headers()
                    self.wfile.write(data)
                    return
                seen['authorization'] = self.headers.get('Authorization')
                seen['managed_session'] = self.headers.get('X-Zhumeng-Managed-Session')
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
            state_path = Path(td) / 'state.json'
            state_path.write_text(
                json.dumps({
                    'claude_code_native_access_token': stale_token,
                    'claude_code_native_refresh_token': 'old-refresh-token',
                    'claude_code_native_managed_session_id': 'old-managed-session',
                    'claude_code_native_device_id': 32,
                    'claude_code_native_server_base_url': f'http://127.0.0.1:{upstream_port}',
                }),
                encoding='utf-8',
            )
            listen_port = _free_port()
            forwarder = RedactingForwarder(GuardConfig(
                listen_host='127.0.0.1',
                listen_port=listen_port,
                upstream_base=f'http://127.0.0.1:{upstream_port}',
                sub2api_auth='sk-sub2api-dedicated-claude-code-key',
                native_managed_access_token=stale_token,
                native_managed_state_path=state_path,
                summary_path=Path(td) / 'summary.jsonl',
                native_attestation_secret='native-attestation-test-secret',
                route_hint_secret=_ROUTE_HINT_SECRET,
                route_hint_catalog=_ROUTE_HINT_CATALOG,
                route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
                managed_session_id='old-managed-session',
                device_id='32',
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
                        'User-Agent': 'claude-cli/2.1.177 (external, sdk-cli)',
                        'x-claude-code-session-id': _DEFAULT_SESSION_REF,
                        **_native_route_headers(body, path, nonce='refresh-expiring-native-token'),
                    },
                )
                with urllib.request.urlopen(request, timeout=5) as resp:
                    self.assertEqual(resp.status, 200)
            finally:
                forwarder.stop()
                upstream.shutdown()
                upstream.server_close()

            updated_state = json.loads(state_path.read_text(encoding='utf-8'))

        self.assertEqual(seen['refresh_calls'], 1)
        self.assertEqual(seen['refresh_body'], {'device_id': 32, 'refresh_token': 'old-refresh-token'})
        self.assertEqual(seen['authorization'], 'Bearer sk-sub2api-dedicated-claude-code-key')
        self.assertEqual(seen['managed_session'], 'next-managed-session')
        self.assertEqual(updated_state['claude_code_native_access_token'], refreshed_token)
        self.assertEqual(updated_state['claude_code_native_refresh_token'], 'next-refresh-token')

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
                native_managed_access_token='native-managed-access-token',
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
                native_managed_access_token='native-managed-access-token',
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


    def test_native_request_records_replay_safe_audit_without_raw_prompt(self):
        seen = {}

        class Upstream(BaseHTTPRequestHandler):
            def do_POST(self):
                seen['body'] = self.rfile.read(int(self.headers.get('content-length', '0')))
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
            summary_path = Path(td) / 'summary.jsonl'
            listen_port = _free_port()
            forwarder = RedactingForwarder(GuardConfig(
                listen_host='127.0.0.1',
                listen_port=listen_port,
                upstream_base=f'http://127.0.0.1:{upstream_port}',
                sub2api_auth='sk-sub2api-dedicated-claude-code-key',
                native_managed_access_token='native-managed-access-token',
                summary_path=summary_path,
                max_messages=0,
                native_attestation_secret='native-attestation-test-secret',
                route_hint_secret=_ROUTE_HINT_SECRET,
                route_hint_catalog=_ROUTE_HINT_CATALOG,
                route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
                managed_session_id='managed-session',
                device_id='9',
                agent_version='0.1.0',
                cost_envelope_limits={'allow_assistant_messages': True, 'allow_thinking': True, 'allow_tool_content': True, 'max_messages': 2048, 'max_content_blocks': 8192},
            ))
            forwarder.start_background()
            try:
                body_obj = {
                    'model': 'claude-sonnet-4-6',
                    'messages': [
                        {'role': 'user', 'content': 'native-visible-prompt'},
                        {'role': 'assistant', 'content': [
                            {'type': 'text', 'text': 'Bridge final answer visible to Claude.'},
                        ]},
                    ],
                    'max_tokens': 16,
                }
                body = json.dumps(body_obj).encode('utf-8')
                path = '/v1/messages?beta=true'
                request = urllib.request.Request(
                    f'http://127.0.0.1:{listen_port}{path}',
                    data=body,
                    method='POST',
                    headers={
                        'content-type': 'application/json',
                        'User-Agent': 'claude-cli/2.1.177 (external, sdk-cli)',
                        'x-claude-code-session-id': _DEFAULT_SESSION_REF,
                        **_native_route_headers(body, path, nonce='replay-safe-audit'),
                    },
                )
                with urllib.request.urlopen(request, timeout=5) as resp:
                    self.assertEqual(resp.status, 200)
            finally:
                forwarder.stop()
                upstream.shutdown()
                upstream.server_close()

            records = [json.loads(line) for line in summary_path.read_text(encoding='utf-8').splitlines()]

        request_record = next(record for record in records if record.get('event') == 'request')
        replay = request_record['replay_safety']
        self.assertEqual(replay['boundary'], 'replay_safe_anthropic_transcript')
        self.assertEqual(replay['target_provider'], 'claude')
        self.assertTrue(replay['allowed'])
        self.assertFalse(replay['raw_body_persisted'])
        self.assertEqual(replay['forbidden_paths_count'], 0)
        dumped = json.dumps(records)
        self.assertNotIn('native-visible-prompt', dumped)
        self.assertNotIn('Bridge final answer visible to Claude.', dumped)
        self.assertEqual(seen['body'], body)

    def test_native_request_sanitizes_foreign_raw_reasoning_before_formal_pool(self):
        seen = {'calls': 0}

        class Upstream(BaseHTTPRequestHandler):
            def do_POST(self):
                seen['calls'] += 1
                seen['body'] = self.rfile.read(int(self.headers.get('content-length', '0')))
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
            summary_path = Path(td) / 'summary.jsonl'
            listen_port = _free_port()
            forwarder = RedactingForwarder(GuardConfig(
                listen_host='127.0.0.1',
                listen_port=listen_port,
                upstream_base=f'http://127.0.0.1:{upstream_port}',
                sub2api_auth='sk-sub2api-dedicated-claude-code-key',
                native_managed_access_token='native-managed-access-token',
                summary_path=summary_path,
                max_messages=0,
                native_attestation_secret='native-attestation-test-secret',
                route_hint_secret=_ROUTE_HINT_SECRET,
                route_hint_catalog=_ROUTE_HINT_CATALOG,
                route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
                managed_session_id='managed-session',
                device_id='9',
                agent_version='0.1.0',
                cost_envelope_limits={'allow_assistant_messages': True, 'allow_thinking': True, 'allow_tool_content': True, 'max_messages': 2048, 'max_content_blocks': 8192},
            ))
            forwarder.start_background()
            try:
                body_obj = {
                    'model': 'claude-sonnet-4-6',
                    'messages': [
                        {'role': 'user', 'content': 'safe user text'},
                        {'role': 'assistant', 'content': [
                            {'type': 'text', 'text': 'visible answer', 'reasoning_content': 'foreign hidden chain'},
                            {'type': 'tool_use', 'id': 'toolu_foreign', 'name': 'Agent', 'input': {'model': 'claude-code-bridge-deepseek-v4-flash'}},
                        ]},
                    ],
                    'max_tokens': 16,
                }
                body = json.dumps(body_obj).encode('utf-8')
                path = '/v1/messages?beta=true'
                request = urllib.request.Request(
                    f'http://127.0.0.1:{listen_port}{path}',
                    data=body,
                    method='POST',
                    headers={
                        'content-type': 'application/json',
                        'User-Agent': 'claude-cli/2.1.177 (external, sdk-cli)',
                        'x-claude-code-session-id': _DEFAULT_SESSION_REF,
                        **_native_route_headers(body, path, nonce='foreign-raw-reasoning-sanitize'),
                    },
                )
                with urllib.request.urlopen(request, timeout=5) as resp:
                    self.assertEqual(resp.status, 200)
            finally:
                forwarder.stop()
                upstream.shutdown()
                upstream.server_close()

            records = [json.loads(line) for line in summary_path.read_text(encoding='utf-8').splitlines()]

        self.assertEqual(seen['calls'], 1)
        forwarded = json.loads(seen['body'].decode('utf-8'))
        forwarded_dump = json.dumps(forwarded)
        self.assertNotIn('reasoning_content', forwarded_dump)
        self.assertNotIn('foreign hidden chain', forwarded_dump)
        self.assertNotIn('"tool_use"', forwarded_dump)
        self.assertNotIn('claude-code-bridge-deepseek-v4-flash', forwarded_dump)
        self.assertIn('ReplaySafeAnthropicTranscript', forwarded_dump)
        request_record = next(record for record in records if record.get('event') == 'request')
        replay = request_record['replay_safety']
        self.assertTrue(replay['allowed'])
        self.assertTrue(replay['sanitized'])
        self.assertGreaterEqual(replay['forbidden_paths_count'], 2)
        self.assertIn('messages[].content[].reasoning_content', replay['forbidden_path_kinds'])
        self.assertIn('messages[].content[].type:tool_use:bridge_model', replay['forbidden_path_kinds'])
        dumped = json.dumps(records)
        self.assertNotIn('foreign hidden chain', dumped)


    def test_native_request_sanitizes_foreign_thinking_signature_before_formal_pool(self):
        seen = {'body': None, 'calls': 0}

        class Upstream(BaseHTTPRequestHandler):
            def do_POST(self):
                seen['calls'] += 1
                seen['body'] = self.rfile.read(int(self.headers.get('content-length', '0')))
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
            summary_path = Path(td) / 'summary.jsonl'
            listen_port = _free_port()
            forwarder = RedactingForwarder(GuardConfig(
                listen_host='127.0.0.1',
                listen_port=listen_port,
                upstream_base=f'http://127.0.0.1:{upstream_port}',
                sub2api_auth='sk-sub2api-dedicated-claude-code-key',
                native_managed_access_token='native-managed-access-token',
                summary_path=summary_path,
                max_messages=0,
                native_attestation_secret='native-attestation-test-secret',
                route_hint_secret=_ROUTE_HINT_SECRET,
                route_hint_catalog=_ROUTE_HINT_CATALOG,
                route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
                managed_session_id='managed-session',
                device_id='9',
                agent_version='0.1.0',
                cost_envelope_limits={'allow_assistant_messages': True, 'allow_thinking': True, 'allow_tool_content': True, 'max_messages': 2048, 'max_content_blocks': 8192},
            ))
            forwarder.start_background()
            try:
                body_obj = {
                    'model': 'claude-sonnet-4-6',
                    'messages': [
                        {'role': 'user', 'content': 'safe user text'},
                        {'role': 'assistant', 'thinking': {'text': 'DeepSeek top-level hidden chain marker', 'signature': 'foreign-top-sig'}, 'signature': 'foreign-message-sig', 'content': [
                            {'type': 'thinking', 'thinking': 'DeepSeek hidden chain only marker', 'signature': 'foreign-sig-only'},
                            {'type': 'tool_use', 'id': 'toolu_foreign_plain', 'name': 'ToolSearch', 'input': {'query': 'private query after thinking'}},
                            {'type': 'text', 'text': 'visible answer'},
                        ]},
                    ],
                    'max_tokens': 16,
                }
                body = json.dumps(body_obj).encode('utf-8')
                path = '/v1/messages?beta=true'
                request = urllib.request.Request(
                    f'http://127.0.0.1:{listen_port}{path}',
                    data=body,
                    method='POST',
                    headers={
                        'content-type': 'application/json',
                        'User-Agent': 'claude-cli/2.1.177 (external, sdk-cli)',
                        'x-claude-code-session-id': _DEFAULT_SESSION_REF,
                        **_native_route_headers(body, path, nonce='foreign-thinking-signature-sanitize'),
                    },
                )
                with urllib.request.urlopen(request, timeout=5) as resp:
                    self.assertEqual(resp.status, 200)
            finally:
                forwarder.stop()
                upstream.shutdown()
                upstream.server_close()

            records = [json.loads(line) for line in summary_path.read_text(encoding='utf-8').splitlines()]

        self.assertEqual(seen['calls'], 1)
        forwarded = json.loads(seen['body'].decode('utf-8'))
        forwarded_dump = json.dumps(forwarded)
        self.assertNotIn('"thinking"', forwarded_dump)
        self.assertNotIn('DeepSeek hidden chain only marker', forwarded_dump)
        self.assertNotIn('foreign-sig-only', forwarded_dump)
        self.assertNotIn('DeepSeek top-level hidden chain marker', forwarded_dump)
        self.assertNotIn('foreign-top-sig', forwarded_dump)
        self.assertNotIn('foreign-message-sig', forwarded_dump)
        self.assertNotIn('toolu_foreign_plain', forwarded_dump)
        self.assertNotIn('ToolSearch', forwarded_dump)
        self.assertNotIn('private query after thinking', forwarded_dump)
        self.assertIn('ReplaySafeAnthropicTranscript', forwarded_dump)
        self.assertIn('visible answer', forwarded_dump)
        request_record = next(record for record in records if record.get('event') == 'request')
        replay = request_record['replay_safety']
        self.assertTrue(replay['allowed'])
        self.assertTrue(replay['sanitized'])
        self.assertIn('messages[].thinking', replay['forbidden_path_kinds'])
        self.assertIn('messages[].signature', replay['forbidden_path_kinds'])
        self.assertIn('messages[].content[].type:thinking', replay['forbidden_path_kinds'])
        self.assertIn('messages[].content[].type:tool_use:foreign_tainted_message', replay['forbidden_path_kinds'])
        dumped = json.dumps(records)
        self.assertNotIn('DeepSeek hidden chain only marker', dumped)
        self.assertNotIn('foreign-sig-only', dumped)
        self.assertNotIn('DeepSeek top-level hidden chain marker', dumped)
        self.assertNotIn('foreign-top-sig', dumped)
        self.assertNotIn('foreign-message-sig', dumped)
        self.assertNotIn('private query after thinking', dumped)


    def test_native_request_sanitizes_foreign_plain_tool_use_when_message_tainted_before_formal_pool(self):
        seen = {'body': None, 'calls': 0}

        class Upstream(BaseHTTPRequestHandler):
            def do_POST(self):
                seen['calls'] += 1
                seen['body'] = self.rfile.read(int(self.headers.get('content-length', '0')))
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
            summary_path = Path(td) / 'summary.jsonl'
            listen_port = _free_port()
            forwarder = RedactingForwarder(GuardConfig(
                listen_host='127.0.0.1',
                listen_port=listen_port,
                upstream_base=f'http://127.0.0.1:{upstream_port}',
                sub2api_auth='sk-sub2api-dedicated-claude-code-key',
                native_managed_access_token='native-managed-access-token',
                summary_path=summary_path,
                max_messages=0,
                native_attestation_secret='native-attestation-test-secret',
                route_hint_secret=_ROUTE_HINT_SECRET,
                route_hint_catalog=_ROUTE_HINT_CATALOG,
                route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
                managed_session_id='managed-session',
                device_id='9',
                agent_version='0.1.0',
                cost_envelope_limits={'allow_assistant_messages': True, 'allow_thinking': True, 'allow_tool_content': True, 'max_messages': 2048, 'max_content_blocks': 8192},
            ))
            forwarder.start_background()
            try:
                body_obj = {
                    'model': 'claude-sonnet-4-6',
                    'messages': [
                        {'role': 'user', 'content': 'safe user text'},
                        {'role': 'assistant', 'content': [
                            {'type': 'thinking', 'thinking': 'DeepSeek hidden chain', 'signature': 'foreign-sig'},
                            {'type': 'tool_use', 'id': 'toolu_raw', 'name': 'ToolSearch', 'input': {'query': 'private query'}},
                            {'type': 'tool_result', 'tool_use_id': 'toolu_raw', 'content': 'raw provider tool output'},
                        ], 'raw_provider_response': {'reasoning_content': 'provider-private'}},
                    ],
                    'max_tokens': 16,
                }
                body = json.dumps(body_obj).encode('utf-8')
                path = '/v1/messages?beta=true'
                request = urllib.request.Request(
                    f'http://127.0.0.1:{listen_port}{path}',
                    data=body,
                    method='POST',
                    headers={
                        'content-type': 'application/json',
                        'User-Agent': 'claude-cli/2.1.177 (external, sdk-cli)',
                        'x-claude-code-session-id': _DEFAULT_SESSION_REF,
                        **_native_route_headers(body, path, nonce='foreign-plain-tool-sanitize'),
                    },
                )
                with urllib.request.urlopen(request, timeout=5) as resp:
                    self.assertEqual(resp.status, 200)
            finally:
                forwarder.stop()
                upstream.shutdown()
                upstream.server_close()

            records = [json.loads(line) for line in summary_path.read_text(encoding='utf-8').splitlines()]

        self.assertEqual(seen['calls'], 1)
        forwarded = json.loads(seen['body'].decode('utf-8'))
        forwarded_dump = json.dumps(forwarded)
        self.assertNotIn('raw_provider_response', forwarded_dump)
        self.assertNotIn('reasoning_content', forwarded_dump)
        self.assertNotIn('provider-private', forwarded_dump)
        self.assertNotIn('"tool_use"', forwarded_dump)
        self.assertNotIn('"tool_result"', forwarded_dump)
        self.assertNotIn('ToolSearch', forwarded_dump)
        self.assertNotIn('private query', forwarded_dump)
        self.assertNotIn('toolu_raw', forwarded_dump)
        self.assertIn('ReplaySafeAnthropicTranscript', forwarded_dump)
        request_record = next(record for record in records if record.get('event') == 'request')
        replay = request_record['replay_safety']
        self.assertTrue(replay['allowed'])
        self.assertTrue(replay['sanitized'])
        self.assertIn('messages[].raw_provider_response', replay['forbidden_path_kinds'])
        self.assertIn('messages[].content[].type:tool_use:foreign_tainted_message', replay['forbidden_path_kinds'])
        self.assertIn('messages[].content[].type:tool_result:provider_private', replay['forbidden_path_kinds'])

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


    def test_managed_native_binary_claude_model_without_route_hint_uses_catalog_native_attestation(self):
        seen = {}

        class Upstream(BaseHTTPRequestHandler):
            def do_POST(self):
                body = self.rfile.read(int(self.headers.get('content-length', '0')))
                seen['body'] = body
                seen['path'] = self.path
                seen['headers'] = {key.lower(): value for key, value in self.headers.items()}
                self.send_response(200)
                self.send_header('content-type', 'application/json')
                self.end_headers()
                self.wfile.write(b'{"ok":true}')

            def log_message(self, *args):
                pass

        upstream_port = _free_port()
        upstream = ThreadingHTTPServer(('127.0.0.1', upstream_port), Upstream)
        threading.Thread(target=upstream.serve_forever, daemon=True).start()
        try:
            with tempfile.TemporaryDirectory() as td:
                listen_port = _free_port()
                summary = Path(td) / 'summary.jsonl'
                forwarder = RedactingForwarder(GuardConfig(
                    listen_host='127.0.0.1',
                    listen_port=listen_port,
                    upstream_base=f'http://127.0.0.1:{upstream_port}',
                    sub2api_auth='managed-access-token',
                    native_managed_access_token='native-managed-access-token',
                    summary_path=summary,
                    native_attestation_secret='native-attestation-test-secret',
                    route_hint_secret=_ROUTE_HINT_SECRET,
                    route_hint_catalog=_ROUTE_HINT_CATALOG,
                    route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
                ))
                forwarder.start_background()
                try:
                    body = b'{"model":"claude-haiku-4-5-20251001","messages":[{"role":"user","content":"binary-native-prompt"}],"max_tokens":16}'
                    request = urllib.request.Request(
                        f'http://127.0.0.1:{listen_port}/v1/messages?beta=true',
                        data=body,
                        method='POST',
                        headers={
                            'content-type': 'application/json',
                            'User-Agent': 'claude-cli/2.1.177 (external, cli)',
                            'x-claude-code-session-id': _DEFAULT_SESSION_REF,
                        },
                    )
                    with urllib.request.urlopen(request, timeout=5) as resp:
                        self.assertEqual(resp.status, 200)
                finally:
                    forwarder.stop()

                headers = seen['headers']
                self.assertEqual(seen['path'], '/v1/messages?beta=true')
                self.assertEqual(headers['authorization'], 'Bearer managed-access-token')
                self.assertEqual(headers['x-sub2api-client-type'], 'claude_code_native')
                self.assertEqual(headers['x-sub2api-guard-attested'], 'true')
                self.assertIn('x-sub2api-native-attestation', headers)
                self.assertIn('x-sub2api-native-signature', headers)
                self.assertNotIn('x-zhumeng-claude-code-route-hint', headers)
                dumped = summary.read_text(encoding='utf-8')
                self.assertIn('native_catalog_fallback', dumped)
                self.assertNotIn('binary-native-prompt', dumped)
        finally:
            upstream.shutdown()
            upstream.server_close()

    def test_managed_native_binary_non_claude_model_without_route_hint_fails_closed(self):
        class Upstream(BaseHTTPRequestHandler):
            count = 0

            def do_POST(self):
                self.__class__.count += 1
                self.send_response(500)
                self.end_headers()

            def log_message(self, *args):
                pass

        upstream_port = _free_port()
        upstream = ThreadingHTTPServer(('127.0.0.1', upstream_port), Upstream)
        threading.Thread(target=upstream.serve_forever, daemon=True).start()
        try:
            with tempfile.TemporaryDirectory() as td:
                listen_port = _free_port()
                summary = Path(td) / 'summary.jsonl'
                forwarder = RedactingForwarder(GuardConfig(
                    listen_host='127.0.0.1',
                    listen_port=listen_port,
                    upstream_base=f'http://127.0.0.1:{upstream_port}',
                    sub2api_auth='managed-access-token',
                    summary_path=summary,
                    native_attestation_secret='native-attestation-test-secret',
                    route_hint_secret=_ROUTE_HINT_SECRET,
                    route_hint_catalog=_ROUTE_HINT_CATALOG,
                    route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
                ))
                forwarder.start_background()
                try:
                    body = b'{"model":"claude-code-bridge-deepseek-v4-pro","messages":[{"role":"user","content":"bridge-must-not-native"}],"max_tokens":16}'
                    request = urllib.request.Request(
                        f'http://127.0.0.1:{listen_port}/v1/messages?beta=true',
                        data=body,
                        method='POST',
                        headers={
                            'content-type': 'application/json',
                            'User-Agent': 'claude-cli/2.1.177 (external, cli)',
                            'x-claude-code-session-id': _DEFAULT_SESSION_REF,
                        },
                    )
                    with self.assertRaises(urllib.error.HTTPError) as cm:
                        urllib.request.urlopen(request, timeout=5)
                    self.assertEqual(cm.exception.code, 403)
                finally:
                    forwarder.stop()

                self.assertEqual(Upstream.count, 0)
                dumped = summary.read_text(encoding='utf-8')
                self.assertIn('route_hint_unavailable', dumped)
                self.assertNotIn('bridge-must-not-native', dumped)
                self.assertNotIn('native-attestation-test-secret', dumped)
        finally:
            upstream.shutdown()
            upstream.server_close()

    def test_cp4_signed_route_hint_binds_model_route_hashes_session_and_nonce(self):
        catalog = cp4_fixture_route_catalog(
            runtime_hash='sha256:' + '1' * 64,
            overlay_hash='sha256:' + '2' * 64,
            catalog_hash='sha256:' + '3' * 64,
            catalog_version='cp4-test-v1',
        )
        body = b'{"model":"claude-code-bridge-deepseek-v4-pro","messages":[{"role":"user","content":"hello"}],"max_tokens":16}'
        headers = build_signed_route_hint_headers(
            body=body,
            request_path='/v1/messages?beta=true',
            catalog=catalog,
            model_id='claude-code-bridge-deepseek-v4-pro',
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



    def test_route_hint_signature_diagnostics_identify_path_and_body_variants_without_prompt_leak(self):
        catalog = cp4_fixture_route_catalog(
            runtime_hash='sha256:' + '1' * 64,
            overlay_hash='sha256:' + '2' * 64,
            catalog_hash='sha256:' + '3' * 64,
            catalog_version='cp4-test-v1',
        )
        signed_body = b'{"model":"claude-code-bridge-deepseek-v4-flash","messages":[{"role":"user","content":"SECRET_PROMPT"}],"max_tokens":16}'
        sent_body = b'{"model":"claude-code-bridge-deepseek-v4-flash","messages":[{"role":"user","content":"SECRET_PROMPT"}],"max_tokens":16,"stream":true}'
        headers = build_signed_route_hint_headers(
            body=signed_body,
            request_path='/v1/messages',
            catalog=catalog,
            model_id='claude-code-bridge-deepseek-v4-flash',
            session_ref='session-a',
            secret='route-hint-secret',
            now=1000,
            nonce='nonce-diagnostic',
        )

        diagnostic = route_hint_signature_diagnostic(
            source_headers=headers,
            body=sent_body,
            request_path='/v1/messages?beta=true',
            secret='route-hint-secret',
        )

        self.assertEqual(diagnostic['signature_match_variant'], 'payload_request_uri_and_payload_body_sha256')
        self.assertEqual(diagnostic['request_path_matches_payload'], False)
        self.assertEqual(diagnostic['body_matches_payload_sha256'], False)
        dumped = json.dumps(diagnostic, ensure_ascii=False, sort_keys=True)
        self.assertNotIn('SECRET_PROMPT', dumped)
        self.assertNotIn('messages', dumped)
        self.assertNotIn('route-hint-secret', dumped)


    def test_route_hint_invalid_summary_includes_safe_signature_diagnostic_without_prompt_leak(self):
        catalog = cp4_fixture_route_catalog(
            runtime_hash='sha256:' + '1' * 64,
            overlay_hash='sha256:' + '2' * 64,
            catalog_hash='sha256:' + '3' * 64,
            catalog_version='cp4-test-v1',
        )
        signed_body = b'{"model":"claude-code-bridge-deepseek-v4-flash","messages":[{"role":"user","content":"SECRET_PROMPT"}],"max_tokens":16}'
        sent_body = b'{"model":"claude-code-bridge-deepseek-v4-flash","messages":[{"role":"user","content":"SECRET_PROMPT"}],"max_tokens":16,"stream":true}'
        headers = build_signed_route_hint_headers(
            body=signed_body,
            request_path='/v1/messages',
            catalog=catalog,
            model_id='claude-code-bridge-deepseek-v4-flash',
            session_ref='session-a',
            secret='route-hint-secret',
            now=1000,
            nonce='nonce-summary-diagnostic',
        )
        with tempfile.TemporaryDirectory() as td:
            summary = Path(td) / 'summary.jsonl'
            forwarder = RedactingForwarder(GuardConfig(
                listen_host='127.0.0.1',
                listen_port=1,
                upstream_base='http://127.0.0.1:18080',
                sub2api_auth='entry',
                summary_path=summary,
                route_hint_secret='route-hint-secret',
                route_hint_catalog=catalog,
                route_hint_replay_cache=RouteHintReplayCache(ttl_seconds=60),
            ))

            self.assertIsNone(forwarder._messages_route_decision(sent_body, '/v1/messages?beta=true', headers))

            dumped = summary.read_text(encoding='utf-8')
            self.assertIn('route_hint_signature_diagnostic', dumped)
            self.assertIn('payload_request_uri_and_payload_body_sha256', dumped)
            self.assertIn('\"body_size\"', dumped)
            self.assertIn('claude-code-bridge-deepseek-v4-flash', dumped)
            self.assertNotIn('SECRET_PROMPT', dumped)
            self.assertNotIn('route-hint-secret', dumped)


    def test_bridge_route_hint_can_rebind_final_body_when_signature_matches_payload_body_hash(self):
        catalog = cp4_fixture_route_catalog(
            runtime_hash='sha256:' + '1' * 64,
            overlay_hash='sha256:' + '2' * 64,
            catalog_hash='sha256:' + '3' * 64,
            catalog_version='cp4-test-v1',
            bridge_live_models=('claude-code-bridge-deepseek-v4-flash',),
        )
        signed_body = b'{"model":"claude-code-bridge-deepseek-v4-flash","messages":[{"role":"user","content":"SECRET_PROMPT"}],"max_tokens":16}'
        final_body = b'{"model":"claude-code-bridge-deepseek-v4-flash","messages":[{"role":"user","content":"SECRET_PROMPT"}],"max_tokens":16,"stream":true}'
        headers = build_signed_route_hint_headers(
            body=signed_body,
            request_path='/v1/messages?beta=true',
            catalog=catalog,
            model_id='claude-code-bridge-deepseek-v4-flash',
            session_ref='session-a',
            secret='route-hint-secret',
            now=1000,
            nonce='nonce-bridge-rebind',
        )

        decision = verify_signed_route_hint_headers_with_bridge_body_rebinding(
            source_headers=headers,
            body=final_body,
            request_path='/v1/messages?beta=true',
            catalog=catalog,
            session_ref='session-a',
            secret='route-hint-secret',
            now=1000,
            replay_cache=RouteHintReplayCache(ttl_seconds=60),
        )

        self.assertEqual(decision.route, 'deepseek_bridge')
        self.assertEqual(decision.client_type, 'claude_code_bridge_deepseek')
        self.assertFalse(decision.formal_pool_allowed)
        self.assertFalse(decision.native_attestation_allowed)

    def test_native_route_hint_never_rebinds_body_for_formal_pool(self):
        catalog = cp4_fixture_route_catalog(
            runtime_hash='sha256:' + '1' * 64,
            overlay_hash='sha256:' + '2' * 64,
            catalog_hash='sha256:' + '3' * 64,
            catalog_version='cp4-test-v1',
        )
        signed_body = b'{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"SECRET_PROMPT"}],"max_tokens":16}'
        final_body = b'{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"SECRET_PROMPT"}],"max_tokens":16,"stream":true}'
        headers = build_signed_route_hint_headers(
            body=signed_body,
            request_path='/v1/messages?beta=true',
            catalog=catalog,
            model_id='claude-sonnet-4-6',
            session_ref='session-a',
            secret='route-hint-secret',
            now=1000,
            nonce='nonce-native-no-rebind',
        )

        with self.assertRaisesRegex(RuntimeError, 'signature mismatch'):
            verify_signed_route_hint_headers_with_bridge_body_rebinding(
                source_headers=headers,
                body=final_body,
                request_path='/v1/messages?beta=true',
                catalog=catalog,
                session_ref='session-a',
                secret='route-hint-secret',
                now=1000,
                replay_cache=RouteHintReplayCache(ttl_seconds=60),
            )


    def test_bridge_forward_rewrites_route_hint_to_final_body_for_backend_verifier(self):
        catalog = cp4_fixture_route_catalog(
            runtime_hash='sha256:' + '1' * 64,
            overlay_hash='sha256:' + '2' * 64,
            catalog_hash='sha256:' + '3' * 64,
            catalog_version='cp4-test-v1',
            bridge_live_models=('claude-code-bridge-deepseek-v4-flash',),
        )
        signed_body = b'{"model":"claude-code-bridge-deepseek-v4-flash","messages":[{"role":"user","content":"SECRET_PROMPT"}],"max_tokens":16}'
        final_body = b'{"model":"claude-code-bridge-deepseek-v4-flash","messages":[{"role":"user","content":"SECRET_PROMPT"}],"max_tokens":16,"stream":true}'
        original = build_signed_route_hint_headers(
            body=signed_body,
            request_path='/v1/messages?beta=true',
            catalog=catalog,
            model_id='claude-code-bridge-deepseek-v4-flash',
            session_ref='session-a',
            secret='route-hint-secret',
            now=1000,
            nonce='nonce-forward-rebind',
        )
        decision = verify_signed_route_hint_headers_with_bridge_body_rebinding(
            source_headers=original,
            body=final_body,
            request_path='/v1/messages?beta=true',
            catalog=catalog,
            session_ref='session-a',
            secret='route-hint-secret',
            now=1000,
            replay_cache=RouteHintReplayCache(ttl_seconds=60),
        )

        rebound = _bridge_rebound_route_hint_headers(
            body=final_body,
            request_path='/v1/messages?beta=true',
            route_decision=decision,
            secret='route-hint-secret',
        )

        verified = verify_signed_route_hint_headers(
            source_headers=rebound,
            body=final_body,
            request_path='/v1/messages?beta=true',
            catalog=catalog,
            session_ref='session-a',
            secret='route-hint-secret',
            replay_cache=RouteHintReplayCache(ttl_seconds=60),
        )
        self.assertEqual(verified.route, 'deepseek_bridge')
        dumped = json.dumps(rebound, ensure_ascii=False, sort_keys=True)
        self.assertNotIn('SECRET_PROMPT', dumped)
        self.assertNotIn('route-hint-secret', dumped)

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
        body = b'{"model":"claude-code-bridge-deepseek-v4-pro","messages":[{"role":"user","content":"hello"}]}'
        headers = build_signed_route_hint_headers(
            body=body,
            request_path='/v1/messages?beta=true',
            catalog=catalog,
            model_id='claude-code-bridge-deepseek-v4-pro',
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
        overlong_headers = build_signed_route_hint_headers(
            body=body,
            request_path='/v1/messages?beta=true',
            catalog=catalog,
            model_id='claude-code-bridge-deepseek-v4-pro',
            session_ref='session-a',
            secret='route-hint-secret',
            now=1000,
            nonce='nonce-overlong',
            ttl_seconds=600,
        )
        with self.assertRaisesRegex(RuntimeError, 'stale'):
            verify_signed_route_hint_headers(
                source_headers=overlong_headers,
                body=body,
                request_path='/v1/messages?beta=true',
                catalog=catalog,
                session_ref='session-a',
                secret='route-hint-secret',
                now=1000,
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


    def test_cp5_bridge_live_hint_forwards_only_bridge_markers_to_loopback_backend_when_backend_live_gate_is_closed(self):
        base_catalog = cp4_fixture_route_catalog(
            runtime_hash='sha256:' + '1' * 64,
            overlay_hash='sha256:' + '2' * 64,
            catalog_hash='sha256:' + '3' * 64,
            catalog_version='cp4-test-v1',
        )
        live_entries = dict(base_catalog.entries)
        live_entries['claude-code-bridge-deepseek-v4-pro'] = RouteCatalogEntry(
            model_id='claude-code-bridge-deepseek-v4-pro',
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
            runtime_hash=base_catalog.runtime_hash,
            overlay_hash=base_catalog.overlay_hash,
            catalog_hash=base_catalog.catalog_hash,
            catalog_version=base_catalog.catalog_version,
            entries=live_entries,
        )
        body = b'{"model":"claude-code-bridge-deepseek-v4-pro","messages":[{"role":"user","content":"must-reach-backend-not-provider"}],"max_tokens":16,"stream":true}'
        path = '/v1/messages?beta=true'
        captured: dict[str, object] = {}

        class BackendLikeHandler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_POST(self):
                n = int(self.headers.get('content-length', '0') or 0)
                request_body = self.rfile.read(n) if n else b''
                captured['path'] = self.path
                captured['body'] = request_body
                captured['headers'] = {key.lower(): value for key, value in self.headers.items()}
                route_decision = verify_signed_route_hint_headers(
                    source_headers={key.lower(): value for key, value in self.headers.items()},
                    body=request_body,
                    request_path=self.path,
                    catalog=catalog,
                    session_ref='11111111-2222-4333-8444-555555555555',
                    secret='route-hint-secret',
                    replay_cache=RouteHintReplayCache(ttl_seconds=60),
                )
                if route_decision.live_request_allowed:
                    # Backend live gate is intentionally closed in this loopback proof.
                    self.send_response(403)
                    self.send_header('content-type', 'application/json')
                    self.end_headers()
                    self.wfile.write(b'{"type":"error","error":{"type":"invalid_request_error","message":"Invalid Claude Code bridge route"}}')
                    return
                self.send_response(500)
                self.end_headers()

        backend_port = _free_port()
        backend = ThreadingHTTPServer(('127.0.0.1', backend_port), BackendLikeHandler)
        threading.Thread(target=backend.serve_forever, daemon=True).start()
        try:
            with tempfile.TemporaryDirectory() as td:
                listen_port = _free_port()
                summary = Path(td) / 'summary.jsonl'
                forwarder = RedactingForwarder(GuardConfig(
                    listen_host='127.0.0.1',
                    listen_port=listen_port,
                    upstream_base=f'http://127.0.0.1:{backend_port}',
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
                    route_headers = build_signed_route_hint_headers(
                        body=body,
                        request_path=path,
                        catalog=catalog,
                        model_id='claude-code-bridge-deepseek-v4-pro',
                        session_ref='11111111-2222-4333-8444-555555555555',
                        secret='route-hint-secret',
                        nonce='nonce-bridge-loopback-backend',
                    )
                    req = urllib.request.Request(
                        f'http://127.0.0.1:{listen_port}{path}',
                        data=body,
                        method='POST',
                        headers={
                            'content-type': 'application/json',
                            'Authorization': 'Bearer user-token-must-be-replaced',
                            'x-claude-code-session-id': '11111111-2222-4333-8444-555555555555',
                            **route_headers,
                        },
                    )
                    with self.assertRaises(urllib.error.HTTPError) as ctx:
                        urllib.request.urlopen(req, timeout=5)
                    self.assertEqual(ctx.exception.code, 403)
                finally:
                    forwarder.stop()

                if 'path' not in captured:
                    self.fail(summary.read_text(encoding='utf-8'))
                self.assertEqual(captured['path'], path)
                self.assertEqual(captured['body'], body)
                headers = captured['headers']
                self.assertEqual(headers['authorization'], 'Bearer sub2api-entry-key')
                self.assertEqual(headers['x-sub2api-client-type'], 'claude_code_bridge_deepseek')
                self.assertEqual(headers['x-sub2api-route'], 'deepseek_bridge')
                self.assertEqual(headers['x-sub2api-route-catalog-version'], 'cp4-test-v1')
                self.assertIn('x-zhumeng-claude-code-route-hint', headers)
                self.assertIn('x-zhumeng-claude-code-route-signature', headers)
                self.assertNotIn('x-sub2api-native-attestation', headers)
                self.assertNotIn('x-sub2api-native-signature', headers)
                self.assertNotIn('x-sub2api-cc-gateway-route', headers)
                dumped = summary.read_text(encoding='utf-8')
                self.assertIn('"decision": "forward_messages"', dumped)
                self.assertIn('"status": 403', dumped)
                self.assertNotIn('user-token-must-be-replaced', dumped)
                self.assertNotIn('native-attestation-test-secret', dumped)
                self.assertNotIn('must-reach-backend-not-provider', dumped)
        finally:
            backend.shutdown()
            backend.server_close()

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
                body = b'{"model":"claude-code-bridge-deepseek-v4-pro","messages":[{"role":"user","content":"hello"}],"max_tokens":16,"stream":true}'
                path = '/v1/messages?beta=true'
                route_headers = build_signed_route_hint_headers(
                    body=body,
                    request_path=path,
                    catalog=catalog,
                    model_id='claude-code-bridge-deepseek-v4-pro',
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
                    'model': 'claude-code-bridge-gpt-5.5',
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
                    model_id='claude-code-bridge-gpt-5.5',
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
            'model': 'claude-code-bridge-gpt-5.5',
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
            model_id='claude-code-bridge-gpt-5.5',
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


    def test_cp5_bridge_body_accepts_parallel_agent_tool_name_for_live_bridge(self):
        catalog = cp4_fixture_route_catalog(
            runtime_hash='sha256:' + '1' * 64,
            overlay_hash='sha256:' + '2' * 64,
            catalog_hash='sha256:' + '3' * 64,
            catalog_version='cp5-test-v1',
            bridge_live_models=('claude-code-bridge-deepseek-v4-pro',),
        )
        body = json.dumps({
            'model': 'claude-code-bridge-deepseek-v4-pro',
            'messages': [{'role': 'user', 'content': 'launch agents'}],
            'tools': [
                {'name': 'multi_tool_use.parallel', 'description': 'parallel tools', 'input_schema': {'type': 'object'}},
                {'name': 'Agent', 'description': 'subagent', 'input_schema': {'type': 'object'}},
            ],
            'tool_choice': {'type': 'tool', 'name': 'multi_tool_use.parallel'},
            'stream': True,
        }, separators=(',', ':')).encode('utf-8')
        route_headers = build_signed_route_hint_headers(
            body=body,
            request_path='/v1/messages',
            catalog=catalog,
            model_id='claude-code-bridge-deepseek-v4-pro',
            session_ref='session-bridge',
            secret='route-hint-secret',
            now=1000,
            nonce='nonce-parallel-agent-live',
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

        self.assertTrue(decision.live_request_allowed)
        self.assertIsNone(validate_cp5_bridge_body(decision, body))

    def test_cp5_bridge_skeleton_fails_closed_for_parallel_agent_tools_without_fake_tool_use(self):
        catalog = cp4_fixture_route_catalog(
            runtime_hash='sha256:' + '1' * 64,
            overlay_hash='sha256:' + '2' * 64,
            catalog_hash='sha256:' + '3' * 64,
            catalog_version='cp5-test-v1',
        )
        body = json.dumps({
            'model': 'claude-code-bridge-deepseek-v4-pro',
            'messages': [{'role': 'user', 'content': 'launch agents'}],
            'tools': [
                {'name': 'multi_tool_use.parallel', 'description': 'parallel tools', 'input_schema': {'type': 'object'}},
                {'name': 'Agent', 'description': 'subagent', 'input_schema': {'type': 'object'}},
            ],
            'tool_choice': {'type': 'tool', 'name': 'multi_tool_use.parallel'},
            'stream': True,
        }, separators=(',', ':')).encode('utf-8')
        route_headers = build_signed_route_hint_headers(
            body=body,
            request_path='/v1/messages',
            catalog=catalog,
            model_id='claude-code-bridge-deepseek-v4-pro',
            session_ref='session-bridge',
            secret='route-hint-secret',
            now=1000,
            nonce='nonce-parallel-agent-skeleton',
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

        self.assertFalse(decision.live_request_allowed)
        self.assertIsNone(validate_cp5_bridge_body(decision, body))
        stream = cp5_bridge_skeleton_sse_body(decision, body=body).decode('utf-8')
        self.assertIn('event: error', stream)
        self.assertIn('bridge live required', stream)
        self.assertNotIn('content_block_start', stream)
        self.assertNotIn('"name":"multi_tool_use.parallel"', stream)
        self.assertNotIn('San Francisco', stream)

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
                    'model': 'claude-code-bridge-gpt-5.5',
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
                    model_id='claude-code-bridge-gpt-5.5',
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
                    'model': 'claude-code-bridge-gpt-5.5',
                    'messages': [{'role': 'user', 'content': 'count-tokens-bridge-must-not-leak'}],
                }, separators=(',', ':')).encode('utf-8')
                path = '/v1/messages/count_tokens'
                route_headers = build_signed_route_hint_headers(
                    body=body,
                    request_path=path,
                    catalog=catalog,
                    model_id='claude-code-bridge-gpt-5.5',
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
                        'model': 'claude-code-bridge-gpt-5.5',
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
                    'model': 'claude-code-bridge-gpt-5.5',
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
                    model_id='claude-code-bridge-gpt-5.5',
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
                    'model': 'claude-code-bridge-gpt-5.5',
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
                    model_id='claude-code-bridge-gpt-5.5',
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
                                'model': 'claude-code-bridge-gpt-5.5',
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
                                'model': 'claude-code-bridge-gpt-5.5',
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
                                'model': 'claude-code-bridge-gpt-5.5',
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
                                'model': 'claude-code-bridge-gpt-5.5',
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
                                'model': 'claude-code-bridge-gpt-5.5',
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
                                'model': 'claude-code-bridge-gpt-5.5',
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
                                'model': 'claude-code-bridge-gpt-5.5',
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
                                model_id='claude-code-bridge-gpt-5.5',
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
                        data=b'{"model":"claude-code-bridge-deepseek-v4-pro","messages":[{"role":"user","content":"hello"}],"max_tokens":16}',
                        method='POST',
                        headers={'content-type': 'application/json', 'x-claude-code-session-id': '11111111-2222-4333-8444-555555555555'},
                    )
                    with self.assertRaises(urllib.error.HTTPError) as missing_ctx:
                        urllib.request.urlopen(missing_req, timeout=5)
                    self.assertEqual(missing_ctx.exception.code, 403)

                    spoof_body = b'{"model":"claude-code-bridge-deepseek-v4-pro","messages":[{"role":"user","content":"hello"}],"max_tokens":16}'
                    spoof_headers = build_signed_route_hint_headers(
                        body=spoof_body,
                        request_path='/v1/messages?beta=true',
                        catalog=catalog,
                        model_id='claude-code-bridge-deepseek-v4-pro',
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
                        model_id='claude-code-bridge-deepseek-v4-pro',
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
                    native_managed_access_token='native-managed-access-token',
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

    def test_managed_control_plane_startup_reads_are_stubbed_even_with_api_key_header(self):
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
                summary = Path(td) / 'summary.jsonl'
                forwarder = RedactingForwarder(GuardConfig(
                    listen_host='127.0.0.1',
                    listen_port=listen_port,
                    upstream_base=f'http://127.0.0.1:{upstream_port}',
                    sub2api_auth='unused',
                    summary_path=summary,
                ))
                forwarder.start_background()
                try:
                    for path in (
                        '/api/claude_cli/bootstrap',
                        '/api/claude_code_penguin_mode',
                        '/api/claude_code/organizations/metrics_enabled',
                        '/mcp-registry/v0/servers?version=latest',
                    ):
                        req = urllib.request.Request(
                            f'http://127.0.0.1:{listen_port}{path}',
                            method='GET',
                            headers={'x-api-key': 'sk-ant-control-plane-test-secret'},
                        )
                        with urllib.request.urlopen(req, timeout=5) as resp:
                            self.assertEqual(resp.status, 200)
                finally:
                    forwarder.stop()

                self.assertEqual(CaptureHandler.count, 0)
                dumped = summary.read_text(encoding='utf-8')
                self.assertNotIn('sk-ant-control-plane-test-secret', dumped)
                self.assertNotIn('intent_validation_failed', dumped)
        finally:
            upstream.shutdown()
            upstream.server_close()

    def test_local_stub_control_plane_preserved_when_intent_validation_fails(self):
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
                summary = Path(td) / 'summary.jsonl'
                forwarder = RedactingForwarder(GuardConfig(
                    listen_host='127.0.0.1',
                    listen_port=listen_port,
                    upstream_base=f'http://127.0.0.1:{upstream_port}',
                    sub2api_auth='unused',
                    summary_path=summary,
                ))
                forwarder.start_background()
                try:
                    with unittest.mock.patch(
                        'tools.cli_control_plane_guard.build_control_plane_intent',
                        side_effect=IntentValidationError('query parameters are not allowed for this path'),
                    ):
                        for path in (
                            '/api/claude_cli/bootstrap',
                            '/mcp-registry/v0/servers?version=latest',
                        ):
                            with urllib.request.urlopen(f'http://127.0.0.1:{listen_port}{path}', timeout=5) as resp:
                                self.assertEqual(resp.status, 200)
                finally:
                    forwarder.stop()

                self.assertEqual(CaptureHandler.count, 0)
                dumped = summary.read_text(encoding='utf-8')
                self.assertIn('intent_validation_failed_local_stub_preserved', dumped)
                self.assertNotIn('"decision": "quarantine_block"', dumped)
        finally:
            upstream.shutdown()
            upstream.server_close()

    def test_unknown_control_plane_still_quarantines_when_intent_validation_fails(self):
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
                summary = Path(td) / 'summary.jsonl'
                forwarder = RedactingForwarder(GuardConfig(
                    listen_host='127.0.0.1',
                    listen_port=listen_port,
                    upstream_base=f'http://127.0.0.1:{upstream_port}',
                    sub2api_auth='unused',
                    summary_path=summary,
                ))
                forwarder.start_background()
                try:
                    with unittest.mock.patch(
                        'tools.cli_control_plane_guard.build_control_plane_intent',
                        side_effect=IntentValidationError('unsafe path cannot be summarized'),
                    ):
                        with self.assertRaises(urllib.error.HTTPError) as raised:
                            urllib.request.urlopen(f'http://127.0.0.1:{listen_port}/api/private-account-settings', timeout=5)
                        self.assertEqual(raised.exception.code, 403)
                finally:
                    forwarder.stop()

                self.assertEqual(CaptureHandler.count, 0)
                dumped = summary.read_text(encoding='utf-8')
                self.assertIn('intent_validation_failed', dumped)
                self.assertIn('"decision": "quarantine_block"', dumped)
        finally:
            upstream.shutdown()
            upstream.server_close()

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
                        native_managed_access_token='native-managed-access-token',
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
                    native_managed_access_token='native-managed-access-token',
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
                    native_managed_access_token='native-managed-access-token',
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
            model_id='claude-code-bridge-deepseek-v4-flash',
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
            entries={'claude-sonnet-4-6': native, 'claude-code-bridge-deepseek-v4-flash': bridge},
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
                            'model': 'claude-code-bridge-deepseek-v4-flash',
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
                            model_id='claude-code-bridge-deepseek-v4-flash',
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
                            'model': 'claude-code-bridge-deepseek-v4-flash',
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
                            model_id='claude-code-bridge-deepseek-v4-flash',
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
                        model_id='claude-code-bridge-deepseek-v4-flash',
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
                        'model': 'claude-code-bridge-deepseek-v4-flash',
                        'messages': [{'role': 'user', 'content': 'title after switch'}],
                        'metadata': {'zhumeng_active_profile': 'deepseek', 'zhumeng_background_task': 'title'},
                        'max_tokens': 16,
                        'stream': True,
                    }, separators=(',', ':')).encode('utf-8')
                    route_headers = build_signed_route_hint_headers(
                        body=body,
                        request_path=path,
                        catalog=catalog,
                        model_id='claude-code-bridge-deepseek-v4-flash',
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
