import unittest

from tools.formal_pool_setup_token_import import (
    SetupTokenEntry,
    build_import_plan,
    parse_setup_token_entries_from_directory,
    sanitize_for_log,
)


class FormalPoolSetupTokenImportTests(unittest.TestCase):
    def test_sanitize_for_log_redacts_setup_tokens_and_bearer_tokens(self):
        raw = "failed sk-ant-sid01-abcdefghijklmnopqrstuvwxyz0123456789 Bearer abc.def.ghi access_token=secret"

        sanitized = sanitize_for_log(raw)

        self.assertNotIn("sk-ant-sid01-abcdefghijklmnopqrstuvwxyz0123456789", sanitized)
        self.assertNotIn("abc.def.ghi", sanitized)
        self.assertIn("sk-ant-sid[redacted]", sanitized)
        self.assertIn("Bearer [redacted]", sanitized)

    def test_sanitize_for_log_redacts_json_and_header_sensitive_fields(self):
        raw = (
            '{"authorization":"eyJ.secret", "auth_token":"tok123", "jwt":"jwt123", '
            '"token":"plain", "cookie":"session=abc"} Authorization: eyJ.no-bearer'
        )

        sanitized = sanitize_for_log(raw)

        self.assertNotIn("eyJ.secret", sanitized)
        self.assertNotIn("tok123", sanitized)
        self.assertNotIn("jwt123", sanitized)
        self.assertNotIn("session=abc", sanitized)
        self.assertNotIn("eyJ.no-bearer", sanitized)
        self.assertGreaterEqual(sanitized.count("[redacted]"), 5)

    def test_parse_setup_token_entries_from_directory_uses_filename_email_and_dedupes(self):
        from tempfile import TemporaryDirectory
        from pathlib import Path

        with TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "[max] [alice@example.com].txt").write_text("cookie sk-ant-sid01-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", encoding="utf-8")
            (root / "[max] [bob@example.com].txt").write_text("sk-ant-sid01-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb,", encoding="utf-8")
            (root / "duplicate.txt").write_text("sk-ant-sid01-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", encoding="utf-8")

            entries = parse_setup_token_entries_from_directory(root)

        self.assertEqual(
            [
                ("alice@example.com", "sk-ant-sid01-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
                ("bob@example.com", "sk-ant-sid01-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
            ],
            [(entry.email, entry.session_key) for entry in entries],
        )
        self.assertTrue(entries[0].source_file.endswith("[max] [alice@example.com].txt"))
        self.assertTrue(entries[1].source_file.endswith("[max] [bob@example.com].txt"))

    def test_dry_run_result_never_exposes_session_keys(self):
        import argparse
        from tempfile import TemporaryDirectory
        from pathlib import Path

        with TemporaryDirectory() as tmp:
            root = Path(tmp)
            root.joinpath("[max] [alice@example.com].txt").write_text("sk-ant-sid01-aaaaaaaa", encoding="utf-8")
            result = __import__("tools.formal_pool_setup_token_import", fromlist=["run_import"]).run_import(
                argparse.Namespace(
                    source_dir=str(root),
                    proxy_ids="6",
                    date_prefix="20260604",
                    target_count=1,
                    execute=False,
                    group_id=1,
                    concurrency=5,
                    timeout=180,
                    allow_admin_browser_egress_attestation_bypass=False,
                    attestation_bypass_reason="",
                    api_base="http://127.0.0.1:18080/api/v1",
                    auth_token="",
                )
            )

        encoded = str(result)
        self.assertEqual("dry_run", result["status"])
        self.assertNotIn("sk-ant", encoded)
        self.assertEqual("planned", result["results"][0]["status"])

    def test_build_import_plan_assigns_proxies_until_target_and_reports_skipped(self):
        entries = [
            SetupTokenEntry(email="a@example.com", session_key="sk-ant-sid01-a"),
            SetupTokenEntry(email="b@example.com", session_key="sk-ant-sid01-b"),
            SetupTokenEntry(email="c@example.com", session_key="sk-ant-sid01-c"),
        ]

        plan = build_import_plan(entries, proxy_ids=[6, 8], date_prefix="20260604", target_count=2)

        self.assertEqual(2, len(plan.attempts))
        self.assertEqual("20260604-a@example.com", plan.attempts[0].account_name)
        self.assertEqual(6, plan.attempts[0].proxy_id)
        self.assertEqual("20260604-b@example.com", plan.attempts[1].account_name)
        self.assertEqual(8, plan.attempts[1].proxy_id)
        self.assertEqual(["c@example.com"], plan.skipped_emails)


if __name__ == "__main__":
    unittest.main()
