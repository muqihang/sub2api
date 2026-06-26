import contextlib
import socket
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from tools.cli_control_plane_guard import GuardConfig, validate_loopback_url


WORKTREE = str(Path(__file__).resolve().parents[2])


@contextlib.contextmanager
def loopback_only_tripwire():
    allowed_hosts = {'127.0.0.1', 'localhost', '::1'}
    original = socket.create_connection

    def guarded(address, *args, **kwargs):
        host = address[0] if isinstance(address, tuple) else address
        if host not in allowed_hosts:
            raise AssertionError(f'external socket blocked: {host}')
        return original(address, *args, **kwargs)

    with mock.patch('socket.create_connection', side_effect=guarded):
        yield


class CliControlPlaneNetworkSafetyTest(unittest.TestCase):
    def test_worktree_constant_is_current_repo_root(self):
        self.assertEqual(Path(WORKTREE).resolve(), Path(__file__).resolve().parents[2])

    def test_validate_loopback_url_allows_loopback_hosts_only(self):
        self.assertEqual(validate_loopback_url('http://127.0.0.1:8080'), 'http://127.0.0.1:8080')
        self.assertEqual(validate_loopback_url('http://localhost:8080/base'), 'http://localhost:8080/base')
        self.assertEqual(validate_loopback_url('http://[::1]:8080'), 'http://[::1]:8080')

    def test_validate_loopback_url_rejects_non_loopback_and_claude_hosts(self):
        for url in (
            'http://192.168.1.10:8080',
            'https://example.com',
            'https://api.anthropic.com',
            'https://platform.claude.com',
            'https://claude.ai',
        ):
            with self.subTest(url=url):
                with self.assertRaises(ValueError):
                    validate_loopback_url(url)

    def test_validate_url_can_allow_nonloopback_zhumeng_upstream_but_never_claude_hosts(self):
        self.assertEqual(
            validate_loopback_url('http://198.12.67.185:18080', allow_nonloopback=True),
            'http://198.12.67.185:18080',
        )
        self.assertEqual(
            validate_loopback_url('https://example.com/base', allow_nonloopback=True),
            'https://example.com/base',
        )
        for url in (
            'https://api.anthropic.com',
            'https://platform.claude.com',
            'https://claude.ai',
        ):
            with self.subTest(url=url):
                with self.assertRaises(ValueError):
                    validate_loopback_url(url, allow_nonloopback=True)

    def test_guard_config_rejects_invalid_upstream_before_startup(self):
        with tempfile.TemporaryDirectory() as td:
            with self.assertRaises(ValueError):
                GuardConfig(
                    listen_host='127.0.0.1',
                    listen_port=18080,
                    upstream_base='https://api.anthropic.com',
                    sub2api_auth='sub2api-entry',
                    summary_path=Path(td) / 'summary.jsonl',
                )
            with self.assertRaises(ValueError):
                GuardConfig(
                    listen_host='127.0.0.1',
                    listen_port=18080,
                    upstream_base='http://127.0.0.1:18081',
                    sub2api_auth='sub2api-entry',
                    summary_path=Path(td) / 'summary.jsonl',
                    control_plane_intent_url='https://claude.ai/backend-api/anthropic/control-plane/intent',
                )

    def test_guard_config_allows_nonloopback_upstream_only_when_explicit(self):
        with tempfile.TemporaryDirectory() as td:
            cfg = GuardConfig(
                listen_host='127.0.0.1',
                listen_port=18080,
                upstream_base='http://198.12.67.185:18080',
                sub2api_auth='sub2api-entry',
                summary_path=Path(td) / 'summary.jsonl',
                allow_nonloopback_upstream=True,
            )
            self.assertEqual(cfg.upstream_base, 'http://198.12.67.185:18080')
            with self.assertRaises(ValueError):
                GuardConfig(
                    listen_host='127.0.0.1',
                    listen_port=18080,
                    upstream_base='https://api.anthropic.com',
                    sub2api_auth='sub2api-entry',
                    summary_path=Path(td) / 'summary.jsonl',
                    allow_nonloopback_upstream=True,
                )

    def test_cli_main_rejects_invalid_upstream_before_serving(self):
        with tempfile.TemporaryDirectory() as td:
            summary_path = Path(td) / 'summary.jsonl'
            proc = subprocess.run(
                [
                    sys.executable,
                    '-m',
                    'tools.cli_control_plane_guard',
                    '--listen-port',
                    '18081',
                    '--upstream-base',
                    'https://platform.claude.com',
                    '--sub2api-auth',
                    'sub2api-entry',
                    '--summary-path',
                    str(summary_path),
                ],
                cwd=WORKTREE,
                capture_output=True,
                text=True,
                timeout=10,
            )
            self.assertNotEqual(proc.returncode, 0)
            self.assertFalse(summary_path.exists())
            self.assertNotIn('"listen"', proc.stdout)

    def test_loopback_tripwire_blocks_external_socket_attempts(self):
        with loopback_only_tripwire():
            with self.assertRaises(AssertionError):
                socket.create_connection(('example.com', 443), timeout=0.1)

    def test_loopback_tripwire_allows_local_socket_attempts(self):
        listener = socket.socket()
        listener.bind(('127.0.0.1', 0))
        listener.listen(1)
        port = listener.getsockname()[1]
        try:
            with loopback_only_tripwire():
                client = socket.create_connection(('127.0.0.1', port), timeout=1)
                server_conn, _ = listener.accept()
                client.close()
                server_conn.close()
        finally:
            listener.close()


if __name__ == '__main__':
    unittest.main()
