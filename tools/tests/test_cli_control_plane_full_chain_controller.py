import json
import subprocess
import sys
import tempfile
import unittest
from datetime import datetime, timezone
from pathlib import Path

from tools.cli_control_plane_ab_diff import message_shape
from tools.cli_control_plane_guard import evaluate_cost_envelope
from tools.claude_code_route_trust import ROUTE_HINT_HEADER, ROUTE_HINT_SIGNATURE_HEADER
from tools.cli_control_plane_policy import load_default_policy
from tools.cli_control_plane_full_chain_controller import (
    LOCAL_ROUTE_HINT_SESSION_REF,
    LOCAL_ROUTE_HINT_SECRET,
    SensitiveScanResult,
    ScenarioExpectation,
    CP4_FORMAL_POOL_SCENARIOS,
    cp4_extra_headers_for_scenario,
    cp4_fixture_for_scenario,
    cp4_profile_env_for_scenario,
    build_full_chain_report_payload,
    build_safe_messages_fixture,
    build_synthetic_client_headers,
    build_unsafe_messages_fixture,
    default_route_hint_catalog,
    create_run_directory,
    evaluate_scenario_status,
    perform_sensitive_scan,
    prepare_scenario_directories,
    load_json_object_or_empty,
    read_json_line,
    missing_context_authority_boundary,
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

    def test_synthetic_client_headers_include_signed_route_hint_bound_to_session(self):
        fixture = build_safe_messages_fixture()
        body = json.dumps(fixture, separators=(',', ':')).encode('utf-8')
        path = '/v1/messages?beta=true'

        headers = build_synthetic_client_headers(body=body, request_path=path, now=1000, nonce='nonce-local')

        self.assertEqual(headers['x-claude-code-session-id'], LOCAL_ROUTE_HINT_SESSION_REF)
        self.assertIn(ROUTE_HINT_HEADER, headers)
        self.assertIn(ROUTE_HINT_SIGNATURE_HEADER, headers)

        from tools.claude_code_route_trust import verify_signed_route_hint_headers
        decision = verify_signed_route_hint_headers(
            source_headers=headers,
            body=body,
            request_path=path,
            catalog=default_route_hint_catalog(),
            session_ref=LOCAL_ROUTE_HINT_SESSION_REF,
            secret=LOCAL_ROUTE_HINT_SECRET,
            now=1000,
        )
        self.assertEqual(decision.model_id, fixture['model'])
        self.assertTrue(decision.native_attestation_allowed)

    def test_unsafe_fixture_triggers_guard_cost_envelope(self):
        fixture = build_unsafe_messages_fixture()

        self.assertGreater(fixture['max_tokens'], 32000)

        decision = evaluate_cost_envelope(
            json.dumps(fixture).encode('utf-8'),
            load_default_policy(),
        )
        self.assertFalse(decision.allowed)
        self.assertEqual(decision.reason, 'max_tokens_limit_exceeded')


    def test_cp4_formal_pool_scenario_matrix_is_complete_and_safe_named(self):
        expected = {
            'valid_trusted_context_strip_cch_present',
            'observed_2_1_181_strip_cch_present',
            'observed_2_1_195_strip_cch_present',
            'forged_authority_headers_ignored',
            'missing_trusted_context_fail_closed',
            'default_strip_cch_present_inbound',
            'default_strip_no_cch_inbound',
            'optional_no_cch_profile_with_proof',
            'optional_signed_cch_profile_requires_proof',
            'cc_gateway_unavailable_no_direct_fallback',
        }
        self.assertEqual({scenario.name for scenario in CP4_FORMAL_POOL_SCENARIOS}, expected)
        self.assertTrue(all(scenario.real_anthropic_upstream is False for scenario in CP4_FORMAL_POOL_SCENARIOS))
        self.assertTrue(any(scenario.expect_mock_request_count == 0 for scenario in CP4_FORMAL_POOL_SCENARIOS))
        self.assertTrue(any(scenario.expect_mock_request_count == 1 for scenario in CP4_FORMAL_POOL_SCENARIOS))


    def test_cp4_profile_env_for_optional_scenarios_uses_exact_2179_proofs(self):
        no_cch = cp4_profile_env_for_scenario('optional_no_cch_profile_with_proof')
        self.assertEqual(no_cch['CC_HARNESS_EGRESS_PROFILE_REF'], 'claude_code_2_1_179_custom_base_no_cch')
        self.assertEqual(no_cch['SUB2API_HARNESS_EGRESS_PROFILE_REF'], 'claude_code_2_1_179_custom_base_no_cch')
        self.assertEqual(no_cch['CC_HARNESS_ENABLE_NO_CCH_PROOF'], '1')
        self.assertEqual(no_cch['CC_HARNESS_POLICY_VERSION'], '2.1.179')

        signed = cp4_profile_env_for_scenario('optional_signed_cch_profile_requires_proof')
        self.assertEqual(signed['CC_HARNESS_EGRESS_PROFILE_REF'], 'claude_code_2_1_179_first_party_signed_cch')
        self.assertEqual(signed['SUB2API_HARNESS_EGRESS_PROFILE_REF'], 'claude_code_2_1_179_first_party_signed_cch')
        self.assertNotEqual(signed.get('CC_HARNESS_ENABLE_SIGNED_CCH_PROOF'), '1')
        self.assertEqual(signed['CC_HARNESS_POLICY_VERSION'], '2.1.179')

        observed_181 = cp4_profile_env_for_scenario('observed_2_1_181_strip_cch_present')
        self.assertEqual(observed_181['CC_HARNESS_OBSERVED_CLI_VERSION'], '2.1.181')
        self.assertEqual(observed_181['CC_HARNESS_EGRESS_PROFILE_REF'], 'strip_attribution')
        self.assertEqual(observed_181['CC_HARNESS_BILLING_SHAPE_POLICY'], 'strip')

        observed_195 = cp4_profile_env_for_scenario('observed_2_1_195_strip_cch_present')
        self.assertEqual(observed_195['CC_HARNESS_OBSERVED_CLI_VERSION'], '2.1.195')
        self.assertEqual(observed_195['CC_HARNESS_EGRESS_PROFILE_REF'], 'strip_attribution')
        self.assertEqual(observed_195['CC_HARNESS_BILLING_SHAPE_POLICY'], 'strip')

    def test_cp4_observed_future_scenarios_are_strip_only_safe_native_shapes(self):
        for version, scenario in (
            ('2.1.181', 'observed_2_1_181_strip_cch_present'),
            ('2.1.195', 'observed_2_1_195_strip_cch_present'),
        ):
            headers = cp4_extra_headers_for_scenario(scenario)
            self.assertIn(version, headers['user-agent'])
            fixture = cp4_fixture_for_scenario(scenario)
            self.assertIn(version, json.dumps(fixture, sort_keys=True))
            env = cp4_profile_env_for_scenario(scenario)
            self.assertEqual(env['CC_HARNESS_EGRESS_PROFILE_REF'], 'strip_attribution')
            self.assertEqual(env['CC_HARNESS_BILLING_SHAPE_POLICY'], 'strip')
            self.assertNotIn('CC_HARNESS_ENABLE_NO_CCH_PROOF', env)
            self.assertNotIn('CC_HARNESS_ENABLE_SIGNED_CCH_PROOF', env)

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




    def test_full_report_payload_includes_cp4_formal_pool_scenarios(self):
        scenarios = []
        for scenario in CP4_FORMAL_POOL_SCENARIOS:
            scenarios.append({
                'scenario': scenario.name,
                'status': 'PASS',
                'real_anthropic_upstream': False,
                'mock_request_count': scenario.expect_mock_request_count,
                'client_status': {'response_status': scenario.expect_response_status, 'response_status_observed': True},
                'sensitive_scan': 'PASS',
                'sensitive_scan_failures': [],
                'observed': {'cli_version_bucket': '2.1.181', 'safe_summary_only': True},
            })
        payload = build_full_chain_report_payload(
            run_dir=Path('/tmp/full-chain-controller-20260523-120001-111'),
            run_id='full-chain-controller-20260523-120001-111',
            scenario_a={'scenario': 'scenario_a', 'status': 'PASS'},
            scenario_b={'scenario': 'scenario_b', 'status': 'PASS'},
            scan_result=SensitiveScanResult(status='PASS', failures=[], scanned_paths=[]),
            cp4_scenarios=scenarios,
        )
        self.assertEqual(payload['status'], 'PASS')
        self.assertEqual(set(payload['cp4_scenarios']), {scenario.name for scenario in CP4_FORMAL_POOL_SCENARIOS})
        self.assertTrue(all(item['status'] == 'PASS' for item in payload['cp4_scenarios'].values()))
        self.assertTrue(all(item['real_anthropic_upstream'] is False for item in payload['cp4_scenarios'].values()))
        self.assertEqual(payload['cp4_scenarios']['observed_2_1_181_strip_cch_present']['observed']['cli_version_bucket'], '2.1.181')

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

    def test_read_json_line_reports_redacted_stderr_when_process_exits_before_json(self):
        process = subprocess.Popen(
            [
                sys.executable,
                '-c',
                "import sys; sys.stderr.write('Authori' + 'zation' + ': Bearer redacted\\n'); sys.exit(7)",
            ],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )

        with self.assertRaises(RuntimeError) as ctx:
            read_json_line(process, timeout=1.0)

        message = str(ctx.exception)
        self.assertIn('exit_code=7', message)
        self.assertIn('stderr_tail=', message)
        self.assertNotIn('secret-token', message)

    def test_sensitive_scan_fails_closed(self):
        with tempfile.TemporaryDirectory() as td:
            base = Path(td)
            clean = base / 'clean.json'
            clean.write_text('{"ok": true}', encoding='utf-8')
            self.assertEqual(perform_sensitive_scan([clean]).status, 'PASS')

            bad = base / 'bad.txt'
            bad.write_text('secret-token', encoding='utf-8')
            failed = perform_sensitive_scan([clean, bad])
            self.assertEqual(failed.status, 'FAIL')
            self.assertTrue(any('secret_token_marker' in item for item in failed.failures))

            missing = base / 'missing.txt'
            missing_result = perform_sensitive_scan([missing])
            self.assertEqual(missing_result.status, 'FAIL')
            self.assertTrue(any('missing' in item.lower() for item in missing_result.failures))

    def test_cp4_positive_scenarios_expect_guard_stop_after_single_forward(self):
        from tools.cli_control_plane_full_chain_controller import cp4_expectation_for_scenario
        expected_stop = {
            'valid_trusted_context_strip_cch_present',
            'observed_2_1_181_strip_cch_present',
            'observed_2_1_195_strip_cch_present',
            'forged_authority_headers_ignored',
            'default_strip_cch_present_inbound',
            'default_strip_no_cch_inbound',
            'optional_no_cch_profile_with_proof',
            'optional_signed_cch_profile_requires_proof',
            'cc_gateway_unavailable_no_direct_fallback',
        }
        for scenario in CP4_FORMAL_POOL_SCENARIOS:
            expectation = cp4_expectation_for_scenario(scenario)
            if scenario.name in expected_stop:
                self.assertEqual(expectation.expect_stop_events, 1, scenario.name)
            else:
                self.assertEqual(expectation.expect_stop_events, 0, scenario.name)

    def test_cp4_process_scan_targets_skip_absent_cc_harness_when_gateway_unavailable(self):
        from tools.cli_control_plane_full_chain_controller import cp4_process_scan_targets
        base = Path('/tmp/cp4-unavailable')
        targets = cp4_process_scan_targets(base, 'cc_gateway_unavailable_no_direct_fallback', cc_gateway_unavailable=True)
        dumped = '\n'.join(str(target) for target in targets)
        self.assertNotIn('cc_gateway_unavailable_no_direct_fallback_cc_harness.stdout.txt', dumped)
        self.assertNotIn('cc_gateway_unavailable_no_direct_fallback_cc_harness.stderr.txt', dumped)
        self.assertIn('cc_gateway_unavailable_no_direct_fallback_sub2api_harness.stdout.txt', dumped)

    def test_direct_missing_context_probe_sends_provider_headers_without_attestation(self):
        source = (Path(__file__).resolve().parents[1] / 'cli_control_plane_full_chain_controller.py').read_text(encoding='utf-8')
        start = source.index('def run_direct_cc_missing_context_scenario')
        direct = source[start:source.index('def run_cp4_formal_pool_scenarios', start)]
        self.assertIn("'x-cc-provider': 'anthropic'", direct)
        self.assertIn("'x-cc-account-id':", direct)
        self.assertIn("'x-cc-token-type': 'oauth'", direct)
        self.assertIn("'x-cc-credential-ref':", direct)
        self.assertNotIn('signedFormalPoolHeaders', direct)

    def test_missing_context_authority_boundary_reports_unattested_request(self):
        boundary = missing_context_authority_boundary({
            'formal_pool_attested': True,
            'profile': {'trusted_egress_profile_ref': 'strip_attribution'},
        })

        self.assertFalse(boundary['formal_pool_attested'])
        self.assertEqual(boundary['trusted_egress_profile_ref'], 'strip_attribution')


    def test_cc_gateway_harness_uses_attested_formal_pool_production_config(self):
        harness = (Path(__file__).resolve().parents[1] / 'cc_gateway_localhost_harness.mjs').read_text(encoding='utf-8')
        self.assertIn("internal_control_token", harness)
        self.assertIn("context_attestation_secret_ref", harness)
        self.assertIn("credential_binding_hmac", harness)
        self.assertIn("CC_GATEWAY_FORMAL_POOL_SESSION_LEDGER_FILE", harness)
        self.assertIn("upstream_mode: 'production'", harness)
        self.assertIn("production_upstream_enabled: true", harness)
        self.assertIn("authorization_present", harness)
        self.assertNotIn("auth_shape: { authori" + "zation:", harness)



    def test_cc_gateway_harness_supports_cp4_2179_profile_env_overrides(self):
        harness = (Path(__file__).resolve().parents[1] / 'cc_gateway_localhost_harness.mjs').read_text(encoding='utf-8')
        for required in (
            'CC_HARNESS_EGRESS_PROFILE_REF',
            'CC_HARNESS_BILLING_SHAPE_POLICY',
            'CC_HARNESS_POLICY_VERSION',
            'CC_HARNESS_ENABLE_NO_CCH_PROOF',
            'CC_HARNESS_ENABLE_SIGNED_CCH_PROOF',
            'CC_HARNESS_OBSERVED_CLI_VERSION',
            'signed_cch_2179_oracle_profile_ref',
            'no_cch_2179_oracle_profile_ref',
            'strip_attribution',
        ):
            self.assertIn(required, harness)

    def test_controller_configures_sub2api_native_attestation_env_for_harness(self):
        source = (Path(__file__).resolve().parents[1] / 'cli_control_plane_full_chain_controller.py').read_text(encoding='utf-8')
        for required in (
            'SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET',
            'SUB2API_CLAUDE_CODE_NATIVE_FORMAL_POOL_MODELS',
            'SUB2API_CLAUDE_CODE_NATIVE_ROUTE_CATALOG_JSON',
        ):
            self.assertIn(required, source)

    def test_controller_configures_sub2api_session_boundary_ledger_for_harness(self):
        source = (Path(__file__).resolve().parents[1] / 'cli_control_plane_full_chain_controller.py').read_text(encoding='utf-8')
        self.assertIn('SUB2API_CLAUDE_CODE_SESSION_BOUNDARY_LEDGER_FILE', source)
        self.assertIn('claude-code-session-boundary-ledger.json', source)


    def test_controller_and_harness_default_to_declared_cp5_cc_gateway_worktree(self):
        controller = (Path(__file__).resolve().parents[1] / 'cli_control_plane_full_chain_controller.py').read_text(encoding='utf-8')
        harness = (Path(__file__).resolve().parents[1] / 'cc_gateway_localhost_harness.mjs').read_text(encoding='utf-8')
        self.assertIn('cc-gateway-claude-platform-aws-cp5', controller)
        self.assertIn('CC_GATEWAY_ROOT', controller)
        self.assertIn('cc-gateway-claude-platform-aws-cp5', harness)
        self.assertIn('CC_GATEWAY_ROOT', harness)
        self.assertNotIn("Path('/Users/muqihang/chelingxi_workspace/cc-gateway')", controller)
        self.assertNotIn("from '/Users/muqihang/chelingxi_workspace/cc-gateway/src/proxy.ts'", harness)

    def test_cc_gateway_localhost_harness_uses_2179_native_persona(self):
        harness = (Path(__file__).resolve().parents[1] / 'cc_gateway_localhost_harness.mjs').read_text(encoding='utf-8')
        self.assertIn('claude_code_2_1_179_native_degraded', harness)
        self.assertNotIn("const personaProfile = 'claude_code_2_1_175_subscription_1m'", harness)
        self.assertIn("persona_variant: 'claude-code-2.1.179-macos-local'", harness)



if __name__ == '__main__':
    unittest.main()
