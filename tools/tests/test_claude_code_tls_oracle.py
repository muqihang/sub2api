import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from tools import claude_code_tls_oracle as tls_oracle


class ClaudeCodeTLSOracleTest(unittest.TestCase):
    def test_safe_summary_rejects_raw_clienthello_material(self):
        with self.assertRaises(ValueError):
            tls_oracle.validate_safe_tls_summary({
                "source": "claude_code_cli",
                "version": "2.1.179",
                "ja3_hash": "0" * 32,
                "ja4": "t13d0000h2_000000000000_000000000000",
                "alpn_protocols": ["h2", "http/1.1"],
                "tls_versions": ["0x0304", "0x0303"],
                "cipher_count": 3,
                "extension_count": 4,
                "grease_present": True,
                "node_version_bucket": "unknown",
                "openssl_version_bucket": "unknown",
                "agent_package_versions": {},
                "raw_clienthello_omitted_reason": "raw_clienthello_forbidden",
                "raw_clienthello": "01020304",
                "timestamp_utc": "2026-06-29T00:00:00Z",
            })

    def test_safe_summary_rejects_certificate_and_private_key_material(self):
        unsafe = {
            "source": "cc_gateway_node_agent",
            "version": "current",
            "ja3_hash": "0" * 32,
            "ja4": "t13d0000h2_000000000000_000000000000",
            "alpn_protocols": ["http/1.1"],
            "tls_versions": ["0x0304"],
            "cipher_count": 1,
            "extension_count": 1,
            "grease_present": False,
            "node_version_bucket": "node-24.x",
            "openssl_version_bucket": "openssl-3.x",
            "agent_package_versions": {},
            "raw_clienthello_omitted_reason": "raw_clienthello_forbidden",
            "timestamp_utc": "2026-06-29T00:00:00Z",
        }
        for key in ("certificate", "private_key", "raw_certificate", "raw_private_key"):
            candidate = dict(unsafe)
            candidate[key] = "-----BEGIN " + "PRIVATE KEY-----\nabc\n-----END " + "PRIVATE KEY-----"
            with self.subTest(key=key):
                with self.assertRaises(ValueError):
                    tls_oracle.validate_safe_tls_summary(candidate)

    def test_compare_profiles_classifies_match_and_explained_and_unexplained(self):
        base = tls_oracle.TLSSummary(
            source="claude_code_cli",
            version="2.1.179",
            ja3_hash="a" * 32,
            ja4="t13d0010h2_aaaaaaaaaaaa_bbbbbbbbbbbb",
            alpn_protocols=("h2", "http/1.1"),
            tls_versions=("0x0304", "0x0303"),
            cipher_count=10,
            extension_count=12,
            grease_present=True,
            node_version_bucket="node-24.x",
            openssl_version_bucket="openssl-3.x",
            agent_package_versions={},
            raw_clienthello_omitted_reason="raw_clienthello_forbidden",
            timestamp_utc="2026-06-29T00:00:00Z",
        )
        same = tls_oracle.compare_tls_profiles(base, base)
        self.assertEqual(same["status"], "MATCH")

        explained = tls_oracle.compare_tls_profiles(
            base,
            tls_oracle.TLSSummary(**{**base.__dict__, "source": "cc_gateway_node_agent", "version": "current", "ja4": "t13d0010h1_aaaaaaaaaaaa_bbbbbbbbbbbb", "alpn_protocols": ("http/1.1",)}),
            explanation="ALPN differs because Node collector used local cert-failure path",
        )
        self.assertEqual(explained["status"], "DIFFERENT_BUT_EXPLAINED")

        different = tls_oracle.compare_tls_profiles(
            base,
            tls_oracle.TLSSummary(**{**base.__dict__, "source": "sub2api_builtin_node24_utls", "ja3_hash": "b" * 32}),
        )
        self.assertEqual(different["status"], "DIFFERENT_UNEXPLAINED")

    def test_compare_profiles_classifies_blocked_guard(self):
        status = tls_oracle.compare_tls_profile_sets([], [], guard_status="BLOCKED_DYNAMIC_EGRESS_GUARD")
        self.assertEqual(status["status"], "BLOCKED_DYNAMIC_EGRESS_GUARD")
        self.assertEqual(status["tls_profile_decision"], "TLS_PROFILE_UNKNOWN_PLUMBING_ONLY")

    def test_compare_profile_sets_emits_required_pair_matrix(self):
        def row(source, version, ja3):
            return tls_oracle.TLSSummary(
                source=source,
                version=version,
                ja3_hash=ja3,
                ja4=f"t13d0017h1_{ja3[:12]}_{ja3[-12:]}",
                alpn_protocols=("http/1.1",),
                tls_versions=("0x0304", "0x0303"),
                cipher_count=17,
                extension_count=12,
                grease_present=False,
                node_version_bucket="bucket",
                openssl_version_bucket="bucket",
                agent_package_versions={},
                raw_clienthello_omitted_reason="raw_clienthello_forbidden",
                timestamp_utc="2026-06-29T00:00:00Z",
            ).to_safe_dict()

        result = tls_oracle.compare_tls_profile_sets(
            [row("claude_code_cli", "2.1.179", "a" * 32), row("claude_code_cli", "2.1.181", "a" * 32), row("claude_code_cli", "2.1.195", "b" * 32)],
            [row("sub2api_utls_builtin", "sub2api-built-in-node24", "c" * 32), row("cc_gateway_node_agent", "node-agent-current", "d" * 32)],
            guard_status="PASS",
        )

        pair_names = {item["pair"] for item in result["comparison_matrix"]}
        self.assertEqual(pair_names, {
            "real_2.1.179_vs_sub2api_builtin",
            "real_2.1.181_vs_real_2.1.179",
            "real_2.1.195_vs_real_2.1.179",
            "cc_gateway_node_agent_vs_real_2.1.179",
        })
        self.assertEqual(result["tls_profile_decision"], "TLS_PROFILE_MISMATCH_REQUIRES_IMPLEMENTATION")

    def test_tcp_clienthello_parser_emits_safe_summary_without_raw_bytes(self):
        # Minimal TLS 1.2 ClientHello record with SNI, supported_versions, ALPN, and GREASE cipher.
        raw = bytes.fromhex(
            "160301006f"  # record header
            "0100006b"  # handshake header
            "0303" + "11" * 32 + "00"  # legacy version + random + session id
            "0004" "0a0a1301"  # ciphers: GREASE, TLS_AES_128_GCM_SHA256
            "01" "00"  # compression null
            "003e"  # extensions length
            "0000" "0010" "000e" "00000b6578616d706c652e636f6d"  # SNI
            "0010" "000e" "000c" "02" "6832" "08" "687474702f312e31"  # ALPN h2,http/1.1
            "002b" "0005" "04" "0304" "0303"  # supported versions
            "000a" "0006" "0004" "001d" "0017"  # supported groups
            "000b" "0002" "01" "00"  # ec point formats
        )
        summary = tls_oracle.summarize_clienthello_bytes(raw, source="unit", version="synthetic")
        dumped = json.dumps(summary.to_safe_dict(), sort_keys=True)

        self.assertEqual(summary.source, "unit")
        self.assertEqual(summary.version, "synthetic")
        self.assertEqual(summary.alpn_protocols, ("h2", "http/1.1"))
        self.assertIn("0x0304", summary.tls_versions)
        self.assertTrue(summary.grease_present)
        self.assertEqual(len(summary.ja3_hash), 32)
        self.assertEqual(summary.raw_clienthello_omitted_reason, "raw_clienthello_forbidden")
        self.assertNotIn("0100006b", dumped)

    def test_write_summary_file_rejects_unsafe_rows(self):
        with tempfile.TemporaryDirectory(dir="/private/tmp") as td:
            out = Path(td) / "tls.json"
            with self.assertRaises(ValueError):
                tls_oracle.write_tls_summary_file(out, [{"raw_clienthello": "abcd"}])
            self.assertFalse(out.exists())

    def test_tls_cli_scratch_root_must_not_be_formal_evidence_root(self):
        with tempfile.TemporaryDirectory(dir="/private/tmp") as td:
            evidence_root = Path(td) / "formal" / "safe"
            with self.assertRaises(ValueError):
                tls_oracle.resolve_tls_scratch_root(evidence_root=evidence_root, scratch_root=evidence_root.parent)
            scratch = tls_oracle.resolve_tls_scratch_root(evidence_root=evidence_root, scratch_root=Path(td) / "scratch")
            self.assertEqual(scratch, Path(td) / "scratch")

    def test_script_help_runs_when_invoked_by_path(self):
        repo = Path(__file__).resolve().parents[2]
        result = subprocess.run(
            [sys.executable, "tools/claude_code_tls_oracle.py", "--help"],
            cwd=repo,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Claude Code TLS ClientHello safe oracle", result.stdout)

    def test_sub2api_capture_uses_local_collector_summary_when_available(self):
        class FakeCollector:
            def __init__(self, **kwargs):
                self.kwargs = kwargs
                self.port = 44444
                self.summaries = [
                    tls_oracle.TLSSummary(
                        source="sub2api_utls_builtin",
                        version="sub2api-built-in-node24",
                        ja3_hash="c" * 32,
                        ja4="t13d0017h1_cccccccccccc_dddddddddddd",
                        alpn_protocols=("http/1.1",),
                        tls_versions=("0x0304", "0x0303"),
                        cipher_count=17,
                        extension_count=12,
                        grease_present=False,
                        node_version_bucket="node-24.x-template",
                        openssl_version_bucket="not_applicable",
                        agent_package_versions={},
                        raw_clienthello_omitted_reason="raw_clienthello_forbidden",
                        timestamp_utc="2026-06-29T00:00:00Z",
                    )
                ]

            def __enter__(self):
                return self

            def __exit__(self, exc_type, exc, tb):
                return None

        calls = []

        def fake_runner(cmd, **kwargs):
            calls.append((cmd, kwargs))
            return subprocess.CompletedProcess(cmd, 1)

        summary = tls_oracle.capture_sub2api_utls_builtin_local(
            Path("/tmp/sub2api/backend"),
            runner=fake_runner,
            collector_factory=FakeCollector,
        )

        self.assertEqual(summary.source, "sub2api_utls_builtin")
        self.assertEqual(summary.ja3_hash, "c" * 32)
        self.assertTrue(calls)
        self.assertIn("TLSFINGERPRINT_CAPTURE_URL", calls[0][1]["env"])
        self.assertIn("127.0.0.1:44444", calls[0][1]["env"]["TLSFINGERPRINT_CAPTURE_URL"])

    def test_cc_gateway_capture_uses_connect_proxy_path_when_available(self):
        class FakeCollector:
            def __init__(self, **kwargs):
                self.kwargs = kwargs
                self.port = 45555
                self.summaries = [
                    tls_oracle.TLSSummary(
                        source="cc_gateway_node_agent",
                        version="node-agent-current",
                        ja3_hash="d" * 32,
                        ja4="t13d0052h2_dddddddddddd_eeeeeeeeeeee",
                        alpn_protocols=("h2", "http/1.1"),
                        tls_versions=("0x0304", "0x0303"),
                        cipher_count=52,
                        extension_count=12,
                        grease_present=False,
                        node_version_bucket="node-24.x",
                        openssl_version_bucket="openssl-3.x",
                        agent_package_versions={"https-proxy-agent": "^9.0.0", "socks-proxy-agent": "^8.0.5"},
                        raw_clienthello_omitted_reason="raw_clienthello_forbidden",
                        timestamp_utc="2026-06-29T00:00:00Z",
                    )
                ]

            def __enter__(self):
                return self

            def __exit__(self, exc_type, exc, tb):
                return None

        calls = []

        def fake_runner(cmd, **kwargs):
            calls.append((cmd, kwargs))
            return subprocess.CompletedProcess(cmd, 0)

        summary = tls_oracle.capture_cc_gateway_node_agent(
            Path("/tmp/cc-gateway"),
            runner=fake_runner,
            collector_factory=FakeCollector,
            proxy_factory=lambda port: ("http://127.0.0.1:49999", object()),
        )

        self.assertEqual(summary.source, "cc_gateway_node_agent")
        self.assertEqual(summary.ja3_hash, "d" * 32)
        self.assertTrue(calls)
        self.assertIn("getProxyAgent", calls[0][0][3])
        self.assertNotIn("require(", calls[0][0][3])
        self.assertEqual(calls[0][0][4], "http://127.0.0.1:49999")


class ClaudeCodeTLSSidecarOracleTest(unittest.TestCase):
    def test_sidecar_summary_source_is_safe_and_compared_to_doc63_oracle(self):
        observed = tls_oracle.TLSSummary(
            source="cc_gateway_utls_sidecar",
            version="tls-profile:claude-code-2.1.179-real-oracle-tcp-v1",
            ja3_hash="dc782a9d905fdcee1223a3d4e8108bc6",
            ja4="t13d0017h1_18560269b2cb_dd86c69b7cb0",
            alpn_protocols=("http/1.1",),
            tls_versions=("0x0304", "0x0303"),
            cipher_count=17,
            extension_count=13,
            grease_present=False,
            node_version_bucket="not_applicable",
            openssl_version_bucket="not_applicable",
            agent_package_versions={},
            raw_clienthello_omitted_reason="raw_clienthello_forbidden",
            timestamp_utc="2026-06-30T00:00:00Z",
        )
        safe = observed.to_safe_dict()
        result = tls_oracle.compare_sidecar_summary_to_doc63_oracle(safe)
        self.assertEqual(result["status"], "BLOCKED_TLS_ENGINE_MISMATCH")
        self.assertIn("ja3_hash", result["difference_fields"])
        self.assertIn("extension_count", result["difference_fields"])
        dumped = json.dumps(result).lower()
        self.assertIn("raw_clienthello_omitted_reason", dumped)
        self.assertNotIn("010000", dumped)
        self.assertNotIn("pcap", dumped)
        self.assertNotIn("private key", dumped)

    def test_sidecar_summary_match_is_safe_summary_equivalence_only(self):
        expected = tls_oracle.expected_doc63_claude_code_2179_summary()
        result = tls_oracle.compare_sidecar_summary_to_doc63_oracle(expected)
        self.assertEqual(result["status"], "SAFE_SUMMARY_EQUIVALENCE_MATCH")
        self.assertEqual(result["claim_scope"], "safe_summary_only")

if __name__ == "__main__":
    unittest.main()
