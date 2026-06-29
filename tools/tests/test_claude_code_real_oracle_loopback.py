import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from tools import claude_code_real_oracle_loopback as oracle


class ClaudeCodeRealOracleLoopbackTest(unittest.TestCase):
    def test_cli_blocks_without_hard_guard_and_never_executes_real_cli(self):
        with tempfile.TemporaryDirectory(dir="/private/tmp") as td:
            evidence = Path(td) / "safe"
            cmd = [
                sys.executable,
                "tools/claude_code_real_oracle_loopback.py",
                "prove-egress",
                "--evidence-root",
                str(evidence),
                "--runtime-version",
                "2.1.179",
                "--runtime-root",
                str(Path(td) / "runtime"),
            ]
            result = subprocess.run(cmd, cwd=Path(__file__).resolve().parents[2], text=True, capture_output=True, check=False)

            self.assertEqual(result.returncode, 2, result.stderr)
            payload = json.loads((evidence / "egress-guard-summary.json").read_text())
            self.assertEqual(payload["status"], "BLOCKED_DYNAMIC_EGRESS_GUARD")
            self.assertEqual(payload["guard_type"], "not_available")
            self.assertFalse(payload["real_cli_executed"])
            self.assertEqual(payload["allowed_destination_bucket"], "loopback_only_not_proven")
            self.assertEqual(payload["blocked_external_probe_bucket"], "not_attempted")
            self.assertFalse(payload["deny_all_except_loopback"])
            self.assertFalse(payload["loopback_collector_reachable"])
            self.assertFalse(payload["ipv4_external_tcp_blocked"])
            self.assertFalse(payload["ipv6_external_tcp_blocked"])
            self.assertFalse(payload["dns_udp_external_blocked"])
            self.assertTrue(payload["proxy_env_only_rejected"])
            self.assertNotIn("ANTHROPIC_AUTH_TOKEN", result.stdout + result.stderr)

    def test_manual_proof_is_advisory_and_cannot_unlock_without_same_scope_self_tests(self):
        with tempfile.TemporaryDirectory(dir="/private/tmp") as td:
            proof = Path(td) / "manual-proof.json"
            proof.write_text(json.dumps({
                "guard_type": "external_manual_proof",
                "deny_all_except_loopback": True,
                "loopback_collector_reachable": True,
                "ipv4_external_tcp_blocked": True,
                "ipv6_external_tcp_blocked": True,
                "dns_udp_external_blocked": True,
                "proxy_env_only_rejected": True,
                "blocked_external_probe_bucket": "blocked",
                "allowed_destination_bucket": "loopback_only",
                "timestamp_utc": "2026-06-29T00:00:00Z",
            }))
            summary = oracle.evaluate_egress_guard(manual_proof=oracle.load_manual_loopback_proof(proof), same_scope_self_tests=None)

            self.assertEqual(summary["status"], "BLOCKED_DYNAMIC_EGRESS_GUARD")
            self.assertEqual(summary["guard_type"], "external_manual_proof")
            self.assertFalse(summary["real_cli_executed"])
            self.assertEqual(summary["allowed_destination_bucket"], "loopback_only_not_proven")
            self.assertEqual(summary["blocked_external_probe_bucket"], "not_proven")
            self.assertTrue(summary["proxy_env_only_rejected"])

    def test_safe_request_summary_redacts_headers_and_body_values(self):
        summary = oracle.summarize_http_request(
            method="POST",
            raw_target="/v1/messages?beta=true&debug=1",
            headers={
                "Authorization": "Bearer x",
                "X-Api-Key": "local-key",
                "Cookie": "session=should-not-leak",
                "Anthropic-Beta": "prompt-caching-scope-2026-01-05, redact-thinking-2026-02-12",
                "Content-Type": "application/json",
            },
            body=json.dumps({
                "model": "claude-sonnet",
                "messages": [{"role": "user", "content": "raw prompt should not leak"}],
                "system": [{"type": "text", "text": "raw system should not leak", "cache_control": {"type": "ephemeral"}}],
                "tools": [],
                "thinking": {"type": "enabled"},
                "x-cc-billing": {"cch": "raw-cch-should-not-leak"},
            }).encode(),
            version="2.1.179",
            scenario="messages_simple",
        )
        dumped = json.dumps(summary, sort_keys=True)

        self.assertEqual(summary["method"], "POST")
        self.assertEqual(summary["path"], "/v1/messages")
        self.assertEqual(summary["query_keys"], ["beta", "debug"])
        self.assertEqual(summary["header_names"], ["anthropic-beta", "content-type"])
        self.assertEqual(summary["sensitive_header_presence"], {
            "authorization_present": True,
            "x_api_key_present": True,
            "cookie_present": True,
        })
        self.assertIn("token_count:2", summary["anthropic_beta_token_buckets"])
        self.assertTrue(summary["body_schema_summary"]["billing_marker_present"])
        self.assertTrue(summary["body_schema_summary"]["cch_marker_present"])
        self.assertTrue(summary["body_schema_summary"]["cache_control_present"])
        self.assertTrue(summary["body_schema_summary"]["thinking_key_present"])
        for forbidden in ("should-not-leak", "raw prompt", "raw system", "raw-cch", "Bearer"):
            self.assertNotIn(forbidden, dumped)

    def test_isolated_environment_allowlist_drops_real_credentials_and_proxy_material(self):
        with tempfile.TemporaryDirectory(dir="/private/tmp") as td:
            env = oracle.build_isolated_cli_env(
                temp_root=Path(td),
                base_env={
                    "PATH": "/usr/bin:/bin",
                    "LANG": "en_US.UTF-8",
                    "ANTHROPIC_API_KEY": "local-anthropic-placeholder",
                    "AWS_SECRET_ACCESS_KEY": "aws-secret",
                    "OPENAI_API_KEY": "local-openai-placeholder",
                    "HTTPS_PROXY": "http://127.0.0.1:9",
                    "NPM_TOKEN": "npm-secret",
                    "COOKIE": "cookie-secret",
                },
                collector_base_url="http://127.0.0.1:41000",
            )

        self.assertEqual(env["PATH"], "/usr/bin:/bin")
        self.assertEqual(env["ANTHROPIC_AUTH_TOKEN"], "local-oracle-dummy-token")
        self.assertEqual(env["ANTHROPIC_BASE_URL"], "http://127.0.0.1:41000")
        self.assertTrue(env["HOME"].startswith(td))
        self.assertTrue(env["npm_config_cache"].startswith(td))
        for forbidden_key in ("ANTHROPIC_API_KEY", "AWS_SECRET_ACCESS_KEY", "OPENAI_API_KEY", "HTTPS_PROXY", "HTTP_PROXY", "NPM_TOKEN", "COOKIE"):
            self.assertNotIn(forbidden_key, env)
        self.assertNotIn("local-anthropic-placeholder", json.dumps(env))
        self.assertNotIn("local-openai-placeholder", json.dumps(env))

    def test_blocked_application_matrix_has_explicit_status_for_each_version_and_scenario(self):
        matrix = oracle.build_blocked_application_matrix(["2.1.179", "2.1.181", "2.1.195"])
        expected_scenarios = set(oracle.APPLICATION_SCENARIOS)

        self.assertEqual({row["version"] for row in matrix}, {"2.1.179", "2.1.181", "2.1.195"})
        for version in ("2.1.179", "2.1.181", "2.1.195"):
            rows = [row for row in matrix if row["version"] == version]
            self.assertEqual({row["scenario"] for row in rows}, expected_scenarios)
            self.assertTrue(all(row["status"] == "blocked_by_egress_guard" for row in rows))
            self.assertTrue(all(row["raw_body_omitted_reason"] == "raw_body_forbidden" for row in rows))

    def test_capture_application_oracle_writes_blocked_summary_when_guard_blocked(self):
        with tempfile.TemporaryDirectory(dir="/private/tmp") as td:
            evidence = Path(td) / "safe"
            cmd_guard = [
                sys.executable,
                "tools/claude_code_real_oracle_loopback.py",
                "prove-egress",
                "--evidence-root",
                str(evidence),
                "--runtime-version",
                "2.1.179",
                "--runtime-root",
                str(Path(td) / "runtime"),
            ]
            subprocess.run(cmd_guard, cwd=Path(__file__).resolve().parents[2], text=True, capture_output=True, check=False)

            cmd = [
                sys.executable,
                "tools/claude_code_real_oracle_loopback.py",
                "capture-application-oracle",
                "--evidence-root",
                str(evidence),
                "--runtime-version",
                "2.1.179",
                "--runtime-version",
                "2.1.181",
                "--runtime-version",
                "2.1.195",
                "--runtime-root",
                str(Path(td) / "runtime"),
            ]
            result = subprocess.run(cmd, cwd=Path(__file__).resolve().parents[2], text=True, capture_output=True, check=False)

            self.assertEqual(result.returncode, 2, result.stderr)
            matrix = json.loads((evidence / "application-oracle-summary.json").read_text())
            self.assertEqual(len(matrix), len(oracle.APPLICATION_SCENARIOS) * 3)
            self.assertEqual({row["status"] for row in matrix}, {"blocked_by_egress_guard"})
            self.assertEqual({row["version"] for row in matrix}, {"2.1.179", "2.1.181", "2.1.195"})
            dumped = json.dumps(matrix, sort_keys=True)
            self.assertNotIn("raw prompt", dumped)
            self.assertNotIn("Authorization", dumped)
            self.assertNotIn("Bearer", dumped)

    def test_build_application_decision_summary_marks_all_blocked_as_real_oracle_blocked(self):
        matrix = oracle.build_blocked_application_matrix(["2.1.179", "2.1.181", "2.1.195"])

        summary = oracle.build_application_decision_summary(matrix, root="/private/tmp/local-evidence")

        self.assertEqual(summary["decision"], "REAL_ORACLE_BLOCKED")
        self.assertEqual(summary["status"], "BLOCKED_DYNAMIC_EGRESS_GUARD")
        self.assertEqual(summary["root"], "/private/tmp/local-evidence")
        self.assertEqual(summary["finding_count"], 3)
        self.assertIn("2.1.179:blocked_by_egress_guard", summary["findings"])
        self.assertIn("2.1.181:blocked_by_egress_guard", summary["findings"])
        self.assertIn("2.1.195:blocked_by_egress_guard", summary["findings"])
        self.assertNotIn("REAL_ORACLE_COMPLETE", json.dumps(summary))

    @unittest.skipUnless(shutil.which("sandbox-exec"), "sandbox-exec is required for macOS process-level loopback proof")
    def test_cli_proves_hard_loopback_with_sandbox_exec_same_scope_self_tests(self):
        with tempfile.TemporaryDirectory(dir="/private/tmp") as td:
            evidence = Path(td) / "safe"
            cmd = [
                sys.executable,
                "tools/claude_code_real_oracle_loopback.py",
                "prove-egress",
                "--evidence-root",
                str(evidence),
                "--runtime-version",
                "2.1.179",
                "--runtime-root",
                str(Path(td) / "runtime"),
                "--use-sandbox-exec-loopback",
            ]
            result = subprocess.run(cmd, cwd=Path(__file__).resolve().parents[2], text=True, capture_output=True, check=False)

            self.assertEqual(result.returncode, 0, result.stderr)
            payload = json.loads((evidence / "egress-guard-summary.json").read_text())
            self.assertEqual(payload["status"], "PASS")
            self.assertEqual(payload["guard_type"], "container_loopback_only")
            self.assertFalse(payload["real_cli_executed"])
            self.assertEqual(payload["allowed_destination_bucket"], "loopback_only")
            self.assertEqual(payload["blocked_external_probe_bucket"], "blocked")
            self.assertTrue(payload["deny_all_except_loopback"])
            self.assertTrue(payload["loopback_collector_reachable"])
            self.assertTrue(payload["ipv4_external_tcp_blocked"])
            self.assertTrue(payload["ipv6_external_tcp_blocked"])
            self.assertTrue(payload["dns_udp_external_blocked"])
            self.assertTrue(payload["proxy_env_only_rejected"])

    def test_isolated_environment_can_add_dummy_api_key_without_inheriting_real_value(self):
        with tempfile.TemporaryDirectory(dir="/private/tmp") as td:
            env = oracle.build_isolated_cli_env(
                temp_root=Path(td),
                base_env={"PATH": "/usr/bin:/bin", "ANTHROPIC_API_KEY": "local-real-placeholder"},
                collector_base_url="http://127.0.0.1:41000",
                include_dummy_api_key=True,
            )

        self.assertEqual(env["ANTHROPIC_API_KEY"], "local-oracle-dummy-key")
        self.assertNotIn("local-real-placeholder", json.dumps(env))

    def test_real_cli_scratch_root_must_not_be_evidence_root_or_safe_dir(self):
        with tempfile.TemporaryDirectory(dir="/private/tmp") as td:
            evidence_root = Path(td) / "evidence" / "safe"
            with self.assertRaises(ValueError):
                oracle.resolve_cli_scratch_root(evidence_root=evidence_root, scratch_root=evidence_root)
            with self.assertRaises(ValueError):
                oracle.resolve_cli_scratch_root(evidence_root=evidence_root, scratch_root=evidence_root.parent)

            scratch = oracle.resolve_cli_scratch_root(evidence_root=evidence_root, scratch_root=Path(td) / "scratch")

            self.assertEqual(scratch, Path(td) / "scratch")


if __name__ == "__main__":
    unittest.main()
