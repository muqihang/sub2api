import json
import os
import unittest

from tools.cli_session_budget import (
    SessionBudgetLedger,
    SessionBudgetPolicy,
    _server_session_key_ref_from_local_uuid,
    session_key_from_headers,
)


class CliSessionBudgetTest(unittest.TestCase):
    def setUp(self):
        self._env = {
            'SUB2API_SESSION_BUDGET_USER_SCOPE': os.environ.get('SUB2API_SESSION_BUDGET_USER_SCOPE'),
            'SUB2API_SESSION_BUDGET_ACCOUNT_REF': os.environ.get('SUB2API_SESSION_BUDGET_ACCOUNT_REF'),
            'SUB2API_SESSION_BUDGET_DEVICE_ID': os.environ.get('SUB2API_SESSION_BUDGET_DEVICE_ID'),
            'SUB2API_SESSION_BUDGET_ACCOUNT_UUID': os.environ.get('SUB2API_SESSION_BUDGET_ACCOUNT_UUID'),
        }
        os.environ['SUB2API_SESSION_BUDGET_USER_SCOPE'] = 'api_key:42'
        os.environ['SUB2API_SESSION_BUDGET_ACCOUNT_REF'] = 'hmac-sha256:acct-ref'
        os.environ['SUB2API_SESSION_BUDGET_DEVICE_ID'] = 'device-a'
        os.environ['SUB2API_SESSION_BUDGET_ACCOUNT_UUID'] = 'acct-uuid-a'

    def tearDown(self):
        for key, value in self._env.items():
            if value is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = value


    def test_default_policy_is_observe_only_for_rich_claude_code_sessions(self):
        ledger = SessionBudgetLedger(SessionBudgetPolicy())
        session_ref = session_key_from_headers({'X-Claude-Code-Session-Id': '11111111-2222-4333-8444-555555555555'})
        rich_metrics = {
            'body_bytes': 512 * 1024,
            'tool_def_bytes': 512 * 1024,
            'tools_count': 30,
            'thinking_present': True,
            'context_management_present': True,
            'max_tokens': 32000,
            'stream': True,
        }

        decisions = [ledger.check_and_record(session_ref, rich_metrics) for _ in range(5)]

        self.assertTrue(all(decision.allowed for decision in decisions))
        self.assertEqual(decisions[-1].summary['messages_used'], 5)
        self.assertEqual(decisions[-1].summary['rich_messages_used'], 5)
        self.assertEqual(decisions[-1].summary['thinking_messages_used'], 5)
        self.assertEqual(decisions[-1].summary['limits']['max_messages_per_session'], -1)

    def test_session_key_uses_hmac_ref_not_raw_session_id(self):
        key = session_key_from_headers({'X-Claude-Code-Session-Id': '11111111-2222-4333-8444-555555555555'})

        self.assertEqual(key['scope'], 'session_budget_session')
        self.assertGreater(key['version'], 0)
        self.assertTrue(key['value'].startswith('hmac-sha256:'))
        self.assertNotIn('11111111-2222-4333-8444-555555555555', json.dumps(key, sort_keys=True))

    def test_invalid_header_session_id_is_not_treated_as_distinct_budget_identity(self):
        ledger = SessionBudgetLedger(SessionBudgetPolicy(max_messages_per_session=1))

        blocked = ledger.check_and_record(
            session_key_from_headers({'X-Claude-Code-Session-Id': 'local-session-alias'}),
            {'body_bytes': 100},
        )

        self.assertFalse(blocked.allowed)
        self.assertEqual(blocked.reason, 'session_identity_invalid')
        self.assertEqual(blocked.status, 409)
        self.assertNotIn('local-session-alias', json.dumps(blocked.summary, sort_keys=True))

    def test_blocks_when_session_message_budget_exceeded(self):
        ledger = SessionBudgetLedger(SessionBudgetPolicy(max_messages_per_session=2))
        metrics = {'body_bytes': 100, 'tools_count': 0, 'thinking_present': False}
        session_ref = session_key_from_headers({'X-Claude-Code-Session-Id': '11111111-2222-4333-8444-555555555555'})

        first = ledger.check_and_record(session_ref, metrics)
        second = ledger.check_and_record(session_ref, metrics)
        third = ledger.check_and_record(session_ref, metrics)

        self.assertTrue(first.allowed)
        self.assertTrue(second.allowed)
        self.assertFalse(third.allowed)
        self.assertEqual(third.reason, 'session_messages_budget_exceeded')
        self.assertEqual(third.summary['messages_used'], 2)

    def test_budget_isolated_between_distinct_server_sessions(self):
        ledger = SessionBudgetLedger(SessionBudgetPolicy(max_messages_per_session=1))
        metrics = {'body_bytes': 100, 'tools_count': 0, 'thinking_present': False}
        session_a = session_key_from_headers({'X-Claude-Code-Session-Id': 'aaaaaaaa-2222-4333-8444-555555555555'})
        session_b = session_key_from_headers({'X-Claude-Code-Session-Id': 'bbbbbbbb-2222-4333-8444-555555555555'})

        self.assertTrue(ledger.check_and_record(session_a, metrics).allowed)
        self.assertTrue(ledger.check_and_record(session_b, metrics).allowed)
        self.assertFalse(ledger.check_and_record(session_a, metrics).allowed)

    def test_blocks_rich_capability_budget_after_limit(self):
        ledger = SessionBudgetLedger(SessionBudgetPolicy(max_messages_per_session=5, max_rich_messages_per_session=1))
        rich = {'body_bytes': 100, 'tools_count': 30, 'thinking_present': True}
        session_ref = session_key_from_headers({'X-Claude-Code-Session-Id': '11111111-2222-4333-8444-555555555555'})

        self.assertTrue(ledger.check_and_record(session_ref, rich).allowed)
        second = ledger.check_and_record(session_ref, rich)

        self.assertFalse(second.allowed)
        self.assertEqual(second.reason, 'session_rich_messages_budget_exceeded')

    def test_blocks_total_body_budget_before_recording(self):
        ledger = SessionBudgetLedger(SessionBudgetPolicy(max_messages_per_session=5, max_total_body_bytes_per_session=150))
        session_ref = session_key_from_headers({'X-Claude-Code-Session-Id': '11111111-2222-4333-8444-555555555555'})

        self.assertTrue(ledger.check_and_record(session_ref, {'body_bytes': 100}).allowed)
        blocked = ledger.check_and_record(session_ref, {'body_bytes': 60})

        self.assertFalse(blocked.allowed)
        self.assertEqual(blocked.reason, 'session_body_bytes_budget_exceeded')
        self.assertEqual(blocked.summary['body_bytes_used'], 100)

    def test_header_ref_and_raw_uuid_canonicalize_to_same_budget_identity(self):
        ledger = SessionBudgetLedger(SessionBudgetPolicy(max_messages_per_session=1))
        metrics = {'body_bytes': 100, 'tools_count': 0, 'thinking_present': False}
        header_ref = session_key_from_headers({'X-Claude-Code-Session-Id': 'AAAAAAAA-2222-4333-8444-555555555555'})

        first = ledger.check_and_record(header_ref, metrics)
        second = ledger.check_and_record('aaaaaaaa-2222-4333-8444-555555555555', metrics)

        self.assertTrue(first.allowed)
        self.assertFalse(second.allowed)
        self.assertEqual(second.reason, 'session_messages_budget_exceeded')
        self.assertNotIn('AAAAAAAA-2222-4333-8444-555555555555', json.dumps(second.summary, sort_keys=True))

    def test_raw_uuid_and_backend_mapping_ref_share_budget_identity(self):
        ledger = SessionBudgetLedger(SessionBudgetPolicy(max_messages_per_session=1))
        metrics = {'body_bytes': 100}
        raw_ref = session_key_from_headers({'X-Claude-Code-Session-Id': '11111111-2222-4333-8444-555555555555'})
        backend_ref = _server_session_key_ref_from_local_uuid('11111111-2222-4333-8444-555555555555')

        first = ledger.check_and_record(raw_ref, metrics)
        second = ledger.check_and_record(backend_ref, metrics)

        self.assertTrue(first.allowed)
        self.assertFalse(second.allowed)
        self.assertEqual(second.reason, 'session_messages_budget_exceeded')

    def test_summary_contains_no_auth_or_prompt_values(self):
        ledger = SessionBudgetLedger(SessionBudgetPolicy(max_messages_per_session=1))
        safe_ref = session_key_from_headers({'X-Claude-Code-Session-Id': '11111111-2222-4333-8444-555555555555'})
        ledger.check_and_record(safe_ref, {'body_bytes': 100, 'tools_count': 30, 'thinking_present': True})
        blocked = ledger.check_and_record(safe_ref, {'body_bytes': 100, 'tools_count': 30, 'thinking_present': True})

        dumped = json.dumps(blocked.summary, sort_keys=True)
        self.assertNotIn('Bearer ', dumped)
        self.assertNotIn('raw-prompt', dumped)
        self.assertNotIn('11111111-2222', dumped)
        self.assertIn('session_key_ref', dumped)
        self.assertIn('scope', dumped)
        self.assertIn('version', dumped)


if __name__ == '__main__':
    unittest.main()
