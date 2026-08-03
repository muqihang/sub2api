import unittest

from tools.import_openai_rt_to_sub2api import (
    RefreshTokenEntry,
    choose_refresh_entry_name,
    find_existing_account_by_email,
    parse_refresh_token_entries,
)


class ImportOpenAIRTScriptTests(unittest.TestCase):
    def test_parse_refresh_token_entries_supports_four_segment_bundle(self):
        raw = (
            "alice@example.com----acc-pass----mail-pass----rt_abc123\n"
            "bob@example.com----pass2----mail2----rt_def456\n"
        )

        entries = parse_refresh_token_entries(raw)

        self.assertEqual(2, len(entries))
        self.assertEqual(
            RefreshTokenEntry(
                refresh_token="rt_abc123",
                name_hint="alice@example.com",
                account_password="acc-pass",
                mail_password="mail-pass",
            ),
            entries[0],
        )
        self.assertEqual("rt_def456", entries[1].refresh_token)
        self.assertEqual("bob@example.com", entries[1].name_hint)

    def test_parse_refresh_token_entries_keeps_plain_rt_compatibility(self):
        raw = "rt_one\n\nrt_two\n"

        entries = parse_refresh_token_entries(raw)

        self.assertEqual(
            [
                RefreshTokenEntry(refresh_token="rt_one"),
                RefreshTokenEntry(refresh_token="rt_two"),
            ],
            entries,
        )

    def test_parse_refresh_token_entries_deduplicates_by_refresh_token(self):
        raw = (
            "alice@example.com----acc-pass----mail-pass----rt_dup\n"
            "another@example.com----pass----mail----rt_dup\n"
        )

        entries = parse_refresh_token_entries(raw)

        self.assertEqual(1, len(entries))
        self.assertEqual("alice@example.com", entries[0].name_hint)

    def test_choose_refresh_entry_name_prefers_name_hint(self):
        entry = RefreshTokenEntry(refresh_token="rt_abc123", name_hint="alice@example.com")
        bundle = {"email": "token@example.com"}

        self.assertEqual("alice@example.com", choose_refresh_entry_name(entry, bundle, "fallback", 1))

    def test_find_existing_account_by_email_matches_credentials_email(self):
        accounts = [
            {"id": 1, "credentials": {"email": "alice@example.com", "refresh_token": "rt_old"}},
            {"id": 2, "credentials": {"email": "bob@example.com", "refresh_token": "rt_other"}},
        ]

        matched = find_existing_account_by_email(accounts, "alice@example.com")

        self.assertIsNotNone(matched)
        self.assertEqual(1, matched["id"])


if __name__ == "__main__":
    unittest.main()
