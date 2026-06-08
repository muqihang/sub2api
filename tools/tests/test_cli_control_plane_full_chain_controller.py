import json
import tempfile
import unittest
from datetime import datetime, timezone
from pathlib import Path

from tools.cli_control_plane_ab_diff import message_shape
from tools.cli_control_plane_guard import evaluate_cost_envelope
from tools.cli_control_plane_policy import load_default_policy
from tools.cli_control_plane_full_chain_controller import (
    SensitiveScanResult,
    ScenarioExpectation,
    build_full_chain_report_payload,
    build_safe_messages_fixture,
    build_unsafe_messages_fixture,
    create_run_directory,
    evaluate_scenario_status,
    perform_sensitive_scan,
    prepare_scenario_directories,
    load_json_object_or_empty,
)


class CliControlPlaneFullChainControllerTest(unittest.TestCase):
    def test_create_run_directory_uses_unique_run_id_and_preserves_existing_fixed_dir(self):
        with tempfile.TemporaryDirectory() as td:
            base = Path(td)
            legacy = base / 'full-chain-controller-20260523'
            legacy.mkdir()
            sentinel = legacy / 'sentinel.txt'
            sentinel.write_text('keep-me', encoding='utf-8')

            first = create_run_directory(
                base,
                now=datetime(2026, 5, 23, 12, 0, 1, tzinfo=timezone.utc),
                pid=111,
            )
            second = create_run_directory(
                base,
                now=datetime(2026, 5, 23, 12, 0, 2, tzinfo=timezone.utc),
                pid=222,
            )

            self.assertNotEqual(first, legacy)
            self.assertNotEqual(second, legacy)
            self.assertNotEqual(first, second)
            self.assertTrue(first.name.startswith('full-chain-controller-20260523-'))
            self.assertTrue(second.name.startswith('full-chain-controller-20260523-'))
            self.assertEqual(sentinel.read_text(encoding='utf-8'), 'keep-me')

    def test_prepare_scenario_directories_fails_closed_on_existing_files_and_preserves_run_dir(self):
        with tempfile.TemporaryDirectory() as td:
            run_dir = create_run_directory(Path(td), now=datetime(2026, 5, 23, 12, 0, 1, tzinfo=timezone.utc), pid=111)
            scenario = prepare_scenario_directories(run_dir, 'scenario_a')
            self.assertEqual(scenario['scenario_dir'].name, 'scenario_a')
            self.assertEqual(scenario['cc_dir'].parent, scenario['scenario_dir'])
            self.assertTrue(run_dir.exists())

            stale = scenario['cc_dir'] / 'stale.txt'
            stale.write_text('stale', encoding='utf-8')
            with self.assertRaises(RuntimeError):
                prepare_scenario_directories(run_dir, 'scenario_a')
            self.assertTrue(run_dir.exists())
            self.assertEqual(stale.read_text(encoding='utf-8'), 'stale')

    def test_safe_fixture_stays_within_guard_cost_envelope(self):
        fixture = build_safe_messages_fixture()

        self.assertLessEqual(fixture['max_tokens'], 2048)
        self.assertFalse(fixture['stream'])
        self.assertEqual(fixture['tools'], [])
        self.assertEqual(len(fixture['messages']), 1)
        self.assertNotIn('thinking', fixture)
        self.assertNotIn('context_management', fixture)

        decision = evaluate_cost_envelope(
            json.dumps(fixture).encode('utf-8'),
            load_default_policy(),
        )
        self.assertTrue(decision.allowed, decision.reason)

    def test_safe_fixture_output_config_matches_real_canary_gate_shape(self):
        fixture = build_safe_messages_fixture()

        output_config = fixture.get('output_config')
        if output_config is None:
            return
        self.assertEqual(list(output_config.keys()), ['effort'])
        self.assertIsInstance(output_config['effort'], str)
        self.assertTrue(output_config['effort'])

    def test_unsafe_fixture_triggers_guard_cost_envelope(self):
        fixture = build_unsafe_messages_fixture()

        self.assertGreater(fixture['max_tokens'], 32000)

        decision = evaluate_cost_envelope(
            json.dumps(fixture).encode('utf-8'),
            load_default_policy(),
        )
        self.assertFalse(decision.allowed)
        self.assertEqual(decision.reason, 'max_tokens_limit_exceeded')

    def test_full_report_payload_includes_scenario_a_and_b_without_raw_values(self):
        scenario_a = {
            'scenario': 'scenario_a',
            'status': 'PASS',
            'run_dir': '/tmp/root/scenario_a',
            'mock_request_count': 0,
            'controller_stop_requested_events': 0,
            'sub2api_selected_count': 0,
            'cost_envelope_block': True,
            'guard_counts': {'messages_cost_envelope_block': 1},
            'message_shape': dict(message_shape()),
            'client_status': {'response_status': 422, 'returncode': 0, 'terminated_by_controller': False},
            'sensitive_scan': 'PASS',
            'sensitive_scan_failures': [],
            'readiness': {'status': 'BLOCKED', 'reason': 'localhost_only'},
            'raw_prompt': 'raw-prompt-marker',
        }
        scenario_b = {
            'scenario': 'scenario_b',
            'status': 'PASS',
            'run_dir': '/tmp/root/scenario_b',
            'mock_request_count': 1,
            'controller_stop_requested_events': 1,
            'sub2api_selected_count': 1,
            'cost_envelope_block': False,
            'guard_counts': {'forward_messages': 1},
            'message_shape': dict(message_shape()),
            'client_status': {'response_status': 200, 'returncode': -15, 'terminated_by_controller': True},
            'sensitive_scan': 'PASS',
            'sensitive_scan_failures': [],
            'readiness': {'status': 'BLOCKED', 'reason': 'localhost_only'},
            'authorization': 'Bearer should-not-leak',
        }

        payload = build_full_chain_report_payload(
            run_dir=Path('/tmp/full-chain-controller-20260523-120001-111'),
            run_id='full-chain-controller-20260523-120001-111',
            scenario_a=scenario_a,
            scenario_b=scenario_b,
            scan_result=SensitiveScanResult(status='PASS', failures=[], scanned_paths=['/tmp/report.json']),
        )

        dumped = json.dumps(payload, sort_keys=True)
        self.assertEqual(payload['run_id'], 'full-chain-controller-20260523-120001-111')
        self.assertEqual(payload['status'], 'PASS')
        self.assertEqual(payload['scenario_a']['mock_request_count'], 0)
        self.assertTrue(payload['scenario_a']['cost_envelope_block'])
        self.assertEqual(payload['scenario_a']['status'], 'PASS')
        self.assertEqual(payload['scenario_b']['mock_request_count'], 1)
        self.assertEqual(payload['scenario_b']['controller_stop_requested_events'], 1)
        self.assertEqual(payload['scenario_b']['status'], 'PASS')
        self.assertEqual(payload['sensitive_scan'], 'PASS')
        for forbidden in (
            'should-not-leak',
            'raw-prompt-marker',
            'Authorization',
            'Cookie',
            'Proxy-Authorization',
            '"authorization":',
            '"raw_prompt":',
        ):
            self.assertNotIn(forbidden, dumped)



    def test_scenario_status_requires_observed_client_status_not_inferred(self):
        payload = {
            'mock_request_count': 1,
            'cost_envelope_block': False,
            'controller_stop_requested_events': 1,
            'sub2api_selected_count': 1,
            'client_status': {'response_status': 200, 'response_status_observed': False, 'terminated_by_controller': True},
            'sensitive_scan': 'PASS',
        }
        expectation = ScenarioExpectation(
            expect_mock_request_count=1,
            expect_cost_envelope_block=False,
            expect_stop_events=1,
            expect_sub2api_selected_count=1,
            expect_response_status=200,
            expect_terminated_by_controller=True,
            linger_after_response=True,
        )
        self.assertEqual(evaluate_scenario_status(payload, expectation), 'FAIL')
        payload['client_status']['response_status_observed'] = True
        self.assertEqual(evaluate_scenario_status(payload, expectation), 'PASS')

    def test_empty_or_partial_client_response_is_treated_as_missing_status(self):
        with tempfile.TemporaryDirectory() as td:
            empty = Path(td) / 'empty.json'
            empty.write_text('', encoding='utf-8')
            bad = Path(td) / 'bad.json'
            bad.write_text('{', encoding='utf-8')
            self.assertEqual(load_json_object_or_empty(empty), {})
            self.assertEqual(load_json_object_or_empty(bad), {})

    def test_sensitive_scan_fails_closed(self):
        with tempfile.TemporaryDirectory() as td:
            base = Path(td)
            clean = base / 'clean.json'
            clean.write_text('{"ok": true}', encoding='utf-8')
            self.assertEqual(perform_sensitive_scan([clean]).status, 'PASS')

            bad = base / 'bad.txt'
            bad.write_text('Authorization: Bearer secret-token-marker', encoding='utf-8')
            failed = perform_sensitive_scan([clean, bad])
            self.assertEqual(failed.status, 'FAIL')
            self.assertTrue(any('Authorization' in item or 'secret' in item for item in failed.failures))

            missing = base / 'missing.txt'
            missing_result = perform_sensitive_scan([missing])
            self.assertEqual(missing_result.status, 'FAIL')
            self.assertTrue(any('missing' in item.lower() for item in missing_result.failures))


if __name__ == '__main__':
    unittest.main()
