import io
import json
import subprocess
import sys
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
from unittest import mock

from tools import claude_code_local_env_attribution_oracle as env_oracle
from tools import claude_code_tls_oracle as tls_oracle
from tools import oracle_phase3a_adapter as adapter


class OraclePhase3AAdapterTest(unittest.TestCase):
    def assert_envelope(self, value, operation):
        self.assertEqual(
            set(value),
            {"schema_version", "operation", "result", "source_modules"},
        )
        self.assertEqual(value["schema_version"], adapter.SCHEMA_VERSION)
        self.assertEqual(value["operation"], operation)
        self.assertIsInstance(value["result"], dict)
        self.assertIsInstance(value["source_modules"], list)

    def test_guard_without_same_scope_proof_is_blocked(self):
        envelope = adapter.dispatch({"operation": "guard"})

        self.assert_envelope(envelope, "guard")
        self.assertEqual(envelope["result"]["legacy"]["status"], "BLOCKED_DYNAMIC_EGRESS_GUARD")
        self.assertEqual(envelope["result"]["sni_preserving"]["status"], "BLOCKED_DYNAMIC_EGRESS_GUARD")
        self.assertFalse(envelope["result"]["sni_preserving"]["real_cli_executed"])
        self.assertFalse(envelope["result"]["sni_preserving"]["sidecar_executed"])

    def test_guard_wraps_safe_same_scope_evaluation_without_running_probes(self):
        proof = {
            "guard_type": "container_loopback_only",
            "deny_all_except_loopback": True,
            "loopback_collector_reachable": True,
            "ipv4_external_tcp_blocked": True,
            "ipv6_external_tcp_blocked": True,
            "dns_udp_external_blocked": True,
            "proxy_env_only_rejected": True,
            "provider_direct_tcp_blocked": True,
            "provider_tcp_connect_unexpected_success": False,
            "non_loopback_proxy_env_rejected": True,
            "proxy_env_only_external_path_blocked": True,
            "real_provider_through_non_loopback_proxy_blocked": True,
            "npm_proxy_endpoint_trust_env_rejected": True,
        }
        with mock.patch(
            "tools.claude_code_real_oracle_loopback.run_same_scope_self_tests",
            side_effect=AssertionError("adapter must not probe the network"),
        ):
            envelope = adapter.dispatch({"operation": "guard", "same_scope_self_tests": proof})

        self.assertEqual(envelope["result"]["legacy"]["status"], "PASS")
        self.assertEqual(envelope["result"]["sni_preserving"]["status"], "PASS")

    def test_http_summary_accepts_only_synthetic_body_and_does_not_echo_it(self):
        marker = "synthetic-marker-that-must-not-be-emitted"
        envelope = adapter.dispatch(
            {
                "operation": "http-summary",
                "version": "2.1.215",
                "scenario": "messages_simple",
                "method": "POST",
                "target": "/v1/messages?beta=true",
                "headers": {"content-type": "application/json"},
                "synthetic_body": {
                    "system": "Today's date is 2026-07-20.",
                    "messages": [{"role": "user", "content": marker}],
                },
            }
        )

        self.assert_envelope(envelope, "http-summary")
        result = envelope["result"]
        self.assertEqual(result["path"], "/v1/messages")
        self.assertEqual(result["query_keys"], ["beta"])
        self.assertEqual(result["raw_body_omitted_reason"], "raw_body_forbidden")
        self.assertNotIn(marker, json.dumps(envelope, sort_keys=True))

    def test_tls_summary_validates_and_returns_only_existing_safe_shape(self):
        summary = tls_oracle.sub2api_builtin_static_summary().to_safe_dict()
        envelope = adapter.dispatch({"operation": "tls-summary", "summary": summary})

        self.assert_envelope(envelope, "tls-summary")
        self.assertEqual(envelope["result"], summary)

    def test_env_summary_validates_and_returns_only_existing_safe_shape(self):
        summary = env_oracle.classify_request_summary(
            version="2.1.215",
            timezone="UTC",
            base_url="http://127.0.0.1:9",
            proxy_env={},
            method="POST",
            path="/v1/messages",
            headers={},
            body=b'{"system":"synthetic"}',
        )
        envelope = adapter.dispatch({"operation": "env-summary", "summary": summary})

        self.assert_envelope(envelope, "env-summary")
        self.assertEqual(envelope["result"], summary)

    def test_unknown_fields_and_sensitive_or_raw_keys_are_rejected(self):
        tls_summary = tls_oracle.sub2api_builtin_static_summary().to_safe_dict()
        env_summary = env_oracle.classify_request_summary(
            version="2.1.215",
            timezone="UTC",
            base_url="http://127.0.0.1:9",
            proxy_env={},
            method="POST",
            path="/v1/messages",
            headers={},
            body=b"{}",
        )
        rejected = (
            {"operation": "guard", "unexpected": True},
            {"operation": "guard", "same_scope_self_tests": {"authorization": "secret"}},
            {"operation": "http-summary", "raw_body": {}},
            {
                "operation": "http-summary",
                "version": "2.1.215",
                "scenario": "messages_simple",
                "method": "POST",
                "target": "/v1/messages",
                "headers": {},
                "synthetic_body": {},
                "unexpected": True,
            },
            {"operation": "tls-summary", "summary": tls_summary, "unexpected": True},
            {"operation": "env-summary", "summary": env_summary, "unexpected": True},
            {
                "operation": "http-summary",
                "version": "2.1.215",
                "scenario": "messages_simple",
                "method": "POST",
                "target": "/v1/messages",
                "headers": {"Authorization": "Bearer synthetic"},
                "synthetic_body": {},
            },
            {
                "operation": "http-summary",
                "version": "2.1.215",
                "scenario": "messages_simple",
                "method": "POST",
                "target": "/v1/messages",
                "headers": {},
                "synthetic_body": {"prompt": "not accepted"},
            },
            {
                "operation": "http-summary",
                "version": "2.1.215",
                "scenario": "messages_simple",
                "method": "POST",
                "target": "/v1/messages",
                "headers": {},
                "synthetic_body": {"body": {}, "client_secret": "not accepted"},
            },
            {"operation": "tls-summary", "summary": {**tls_summary, "raw_clienthello": "00"}},
            {"operation": "env-summary", "summary": {**env_summary, "credential": "secret"}},
        )
        for payload in rejected:
            with self.subTest(payload_keys=sorted(payload)):
                with self.assertRaises(adapter.AdapterInputError):
                    adapter.dispatch(payload)

    def test_cli_rejects_duplicate_keys_and_emits_no_input_values(self):
        stdout = io.StringIO()
        stderr = io.StringIO()
        raw = '{"operation":"guard","operation":"http-summary","raw_body":"private-marker"}'
        with mock.patch("sys.stdin", io.StringIO(raw)), redirect_stdout(stdout), redirect_stderr(stderr):
            status = adapter.main([])

        self.assertEqual(status, 2)
        self.assertEqual(stdout.getvalue(), "")
        self.assertIn("adapter_input_rejected", stderr.getvalue())
        self.assertNotIn("private-marker", stderr.getvalue())

    def test_cli_rejects_invalid_utf8_as_input_without_echoing_bytes(self):
        repo = Path(__file__).resolve().parents[2]
        result = subprocess.run(
            [sys.executable, "tools/oracle_phase3a_adapter.py"],
            cwd=repo,
            input=b"\xffprivate-marker",
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )

        self.assertEqual(result.returncode, 2)
        self.assertEqual(result.stdout, b"")
        self.assertIn(b"adapter_input_rejected", result.stderr)
        self.assertNotIn(b"private-marker", result.stderr)

    def test_script_accepts_strict_json_when_invoked_by_path(self):
        repo = Path(__file__).resolve().parents[2]
        result = subprocess.run(
            [sys.executable, "tools/oracle_phase3a_adapter.py"],
            cwd=repo,
            input='{"operation":"guard"}',
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        envelope = json.loads(result.stdout)
        self.assert_envelope(envelope, "guard")
        self.assertEqual(envelope["result"]["legacy"]["status"], "BLOCKED_DYNAMIC_EGRESS_GUARD")

    def test_adapter_never_calls_real_cli_or_capture_entrypoints(self):
        with mock.patch(
            "tools.claude_code_real_oracle_loopback.run_real_cli_application_oracle",
            side_effect=AssertionError("real CLI forbidden"),
        ), mock.patch(
            "tools.claude_code_tls_oracle.capture_real_cli_tls",
            side_effect=AssertionError("TLS capture forbidden"),
        ), mock.patch(
            "tools.claude_code_tls_oracle.capture_real_cli_sni_preserving_tls",
            side_effect=AssertionError("TLS capture forbidden"),
        ):
            result = adapter.dispatch({"operation": "guard"})
            self.assertEqual(result["result"]["legacy"]["status"], "BLOCKED_DYNAMIC_EGRESS_GUARD")


if __name__ == "__main__":
    unittest.main()
