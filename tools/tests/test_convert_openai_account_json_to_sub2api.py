import unittest
from datetime import datetime, timezone

from tools.convert_openai_account_json_to_sub2api import (
    build_export_payload,
    partition_accounts_for_import,
)


class ConvertOpenAIAccountJSONTests(unittest.TestCase):
    def test_build_export_payload_wraps_accounts_with_sub2api_metadata(self) -> None:
        now = datetime(2026, 4, 25, 6, 5, 0, tzinfo=timezone.utc)
        accounts = [{"name": "alice"}]

        payload = build_export_payload(accounts, now=now)

        self.assertEqual("sub2api-data", payload["type"])
        self.assertEqual(1, payload["version"])
        self.assertEqual("2026-04-25T06:05:00Z", payload["exported_at"])
        self.assertEqual(accounts, payload["accounts"])
        self.assertEqual([], payload["proxies"])

    def test_partition_accounts_for_import_filters_duplicates(self) -> None:
        payload_accounts = [
            {
                "name": "dup-refresh",
                "credentials": {"refresh_token": "rt_dup", "email": "dup-refresh@example.com"},
                "extra": {"email": "dup-refresh@example.com"},
            },
            {
                "name": "dup-email",
                "credentials": {"refresh_token": "rt_new", "email": "existing@example.com"},
                "extra": {"email": "existing@example.com"},
            },
            {
                "name": "fresh",
                "credentials": {"refresh_token": "rt_fresh", "email": "fresh@example.com"},
                "extra": {"email": "fresh@example.com"},
            },
        ]
        existing_accounts = [
            {
                "id": 73,
                "name": "existing-rt",
                "credentials": {"refresh_token": "rt_dup", "email": "other@example.com"},
                "extra": {"email": "other@example.com"},
            },
            {
                "id": 74,
                "name": "existing-email",
                "credentials": {"refresh_token": "rt_other", "email": "existing@example.com"},
                "extra": {"email": "existing@example.com"},
            },
        ]

        filtered, results = partition_accounts_for_import(payload_accounts, existing_accounts)

        self.assertEqual(["fresh"], [account["name"] for account in filtered])
        self.assertEqual(["skipped_duplicate", "skipped_duplicate"], [result.action for result in results])
        self.assertTrue(any("refresh_token" in (result.error or "") for result in results))
        self.assertTrue(any("Duplicate email" in (result.error or "") for result in results))


if __name__ == "__main__":
    unittest.main()
