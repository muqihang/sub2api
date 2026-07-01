import json
import tempfile
import unittest
from pathlib import Path

from tools import claude_code_local_env_attribution_oracle as oracle


class ClaudeCodeLocalEnvAttributionOracleTest(unittest.TestCase):
    def test_timezone_bucket_keeps_asia_buckets_distinct(self):
        self.assertEqual(oracle.timezone_bucket("America/Los_Angeles"), "us_pacific")
        self.assertEqual(oracle.timezone_bucket("America/New_York"), "us_eastern")
        self.assertEqual(oracle.timezone_bucket("UTC"), "utc")
        self.assertEqual(oracle.timezone_bucket("Asia/Taipei"), "taipei")
        self.assertEqual(oracle.timezone_bucket("Asia/Tokyo"), "tokyo")
        self.assertEqual(oracle.timezone_bucket("Asia/Seoul"), "seoul")
        self.assertEqual(oracle.timezone_bucket("Asia/Shanghai"), "shanghai")
        self.assertEqual(oracle.timezone_bucket("Asia/Urumqi"), "urumqi")

    def test_date_and_apostrophe_bucket_classifiers_do_not_return_raw_markers(self):
        self.assertEqual(oracle.date_format_bucket("Today's date is 2026-06-30."), "hyphen")
        self.assertEqual(oracle.date_format_bucket("Today date 06/30/2026"), "slash")
        self.assertEqual(oracle.date_format_bucket("today date June 30 2026"), "other")
        self.assertEqual(oracle.date_format_bucket("no date marker here"), "not_observed")
        self.assertEqual(oracle.apostrophe_bucket("Today's date is 2026-06-30."), "ascii")
        self.assertEqual(oracle.apostrophe_bucket("Today\u2019s date is 2026-06-30."), "unicode_variant_1")
        self.assertEqual(oracle.apostrophe_bucket("Today\u02bcs date is 2026-06-30."), "unicode_variant_2")
        self.assertEqual(oracle.apostrophe_bucket("Today\uff07s date is 2026-06-30."), "unicode_variant_3")
        self.assertEqual(oracle.apostrophe_bucket("no marker"), "not_observed")

    def test_base_url_risk_classifier_uses_safe_buckets_and_match_modes(self):
        cases = {
            "https://api.anthropic.com": ("official_anthropic", "exact_domain"),
            "http://127.0.0.1:4000": ("neutral_gateway", "exact_domain"),
            "https://example-gateway.test": ("neutral_gateway", "substring_keyword"),
            "https://neutral.example.com": ("neutral_gateway", "no_match"),
            "https://relay.example.cn": ("china_tld", "subdomain_suffix"),
            "https://neutral-deepseek.example.com": ("ai_lab_keyword", "substring_keyword"),
            "https://resale.example.net": ("claude_proxy_resale_like", "substring_keyword"),
        }
        for url, expected in cases.items():
            with self.subTest(url=url):
                classified = oracle.classify_base_url(url)
                self.assertEqual((classified["known_domain_category_bucket"], classified["known_domain_match_bucket"]), expected)
                self.assertNotIn("example", json.dumps(classified))
                self.assertNotIn("anthropic.com", json.dumps(classified))

    def test_proxy_bucket_and_env_allowlist_reject_network_influencers(self):
        self.assertEqual(oracle.classify_proxy_env({}), "no_proxy_env")
        self.assertEqual(oracle.classify_proxy_env({"HTTPS_PROXY": "http://127.0.0.1:9999"}), "loopback_proxy_only")
        self.assertEqual(oracle.classify_proxy_env({"HTTPS_PROXY": "http://192.0.2.1:8080"}), "non_loopback_proxy_rejected")
        self.assertEqual(oracle.classify_proxy_env({"NO_PROXY": "*"}), "no_proxy_bypass_guarded")
        with self.assertRaises(ValueError):
            oracle.validate_child_env_allowlist({"NODE_EXTRA_CA_CERTS": "/tmp/cert.pem"})
        with self.assertRaises(ValueError):
            oracle.validate_child_env_allowlist({"npm_config_proxy": "http://127.0.0.1:9"})

    def test_safe_request_classifier_emits_only_buckets_and_no_raw_body(self):
        body = json.dumps({
            "system": "Today's date is 2026-06-30. billing marker cch gateway",
            "messages": [{"role": "user", "content": "hello"}],
        }).encode()
        row = oracle.classify_request_summary(
            version="2.1.196",
            timezone="Asia/Shanghai",
            base_url="https://neutral-deepseek.example.com",
            proxy_env={"HTTPS_PROXY": "http://127.0.0.1:9999"},
            method="POST",
            path="/v1/messages",
            headers={"anthropic-beta": "prompt-caching-scope-2026-01-05"},
            body=body,
        )
        self.assertEqual(row["timezone_bucket"], "shanghai")
        self.assertEqual(row["date_format_bucket"], "hyphen")
        self.assertEqual(row["apostrophe_bucket"], "ascii")
        self.assertEqual(row["known_domain_category_bucket"], "ai_lab_keyword")
        self.assertFalse(row["timezone_signal_residue_present"])
        self.assertFalse(row["base_url_signal_residue_present"])
        self.assertTrue(row["billing_marker_present"])
        self.assertTrue(row["cch_marker_present"])
        self.assertEqual(row["raw_body_omitted_reason"], "raw_body_forbidden")
        dumped = json.dumps(row, sort_keys=True)
        self.assertNotIn("2026-06-30", dumped)
        self.assertNotIn("neutral-deepseek", dumped)
        self.assertNotIn("Today's", dumped)

    def test_safe_path_bucket_omits_query_string(self):
        row = oracle.classify_request_summary(
            version="2.1.196",
            timezone="UTC",
            base_url="http://127.0.0.1:9",
            proxy_env={},
            method="POST",
            path="/v1/messages?beta=true",
            headers={},
            body=b"{}",
        )
        self.assertEqual(row["path"], "/v1/messages")
        self.assertEqual(row["query_keys"], ["beta"])
        self.assertNotIn("beta=true", json.dumps(row, sort_keys=True))

    def test_expected_matrix_contains_required_timezone_rows_and_combination_rows(self):
        manifest = oracle.build_expected_matrix_manifest(["2.1.179", "2.1.185", "2.1.196"])
        required = [row for row in manifest["rows"] if row["requirement"] == "required"]
        self.assertEqual(len([row for row in required if row["dimension"] == "timezone"]), 24)
        combos = {(row["timezone_bucket"], row["base_url_bucket"]) for row in required if row["dimension"] == "timezone_base_url_combo"}
        self.assertIn(("us_pacific", "neutral_gateway"), combos)
        self.assertIn(("us_pacific", "ai_lab_keyword"), combos)
        self.assertIn(("taipei", "neutral_gateway"), combos)
        self.assertIn(("shanghai", "neutral_gateway"), combos)
        self.assertIn(("shanghai", "ai_lab_keyword"), combos)

    def test_actual_vs_expected_coverage_identifies_missing_required_rows(self):
        manifest = oracle.build_expected_matrix_manifest(["2.1.196"])
        actual = [
            {
                "version": "2.1.196",
                "dimension": "timezone",
                "timezone_bucket": "us_pacific",
                "base_url_bucket": "neutral_gateway",
                "proxy_bucket": "no_proxy_env",
            }
        ]
        coverage = oracle.compare_actual_vs_expected(manifest, actual)
        self.assertEqual(coverage["coverage_decision"], "partial_with_blockers")
        self.assertGreater(coverage["missing_required_row_count"], 0)

    def test_no_raw_leak_scanner_reports_path_rule_count_only(self):
        with tempfile.TemporaryDirectory(dir="/private/tmp") as td:
            root = Path(td)
            (root / "safe.json").write_text(json.dumps({"raw_body_omitted_reason": "raw_body_forbidden"}))
            (root / "unsafe.log").write_text("Authorization: Bearer local-secret\n")
            result = oracle.scan_for_raw_leaks([root])
            self.assertEqual(result["blocking_hit_count"], 1)
            dumped = json.dumps(result, sort_keys=True)
            self.assertIn("unsafe.log", dumped)
            self.assertIn("secret_or_token_pattern", dumped)
            self.assertNotIn("local-secret", dumped)
            self.assertNotIn("Bearer", dumped)


if __name__ == "__main__":
    unittest.main()
