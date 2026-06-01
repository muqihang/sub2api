import json
import tempfile
import unittest
from pathlib import Path

from tools.cli_runtime_productization import (
    RuntimeConfigError,
    build_runtime_manifest,
    render_cc_gateway_config,
    validate_runtime_manifest,
    write_runtime_artifacts,
)
from tools.safe_deliverable_sensitive_scan import scan_file


class CliRuntimeProductizationTest(unittest.TestCase):
    def test_preflight_manifest_is_localhost_only_and_uses_2150_1m_profile(self):
        manifest = build_runtime_manifest(mode='localhost-preflight', run_dir=Path('/tmp/run'), raw_dir=Path('/tmp/run/raw'))

        self.assertEqual(manifest['mode'], 'localhost-preflight')
        self.assertEqual(manifest['cc_gateway']['shared_pool']['upstream_mode'], 'preflight')
        self.assertEqual(manifest['cc_gateway']['upstream']['url'], 'http://127.0.0.1:19082')
        self.assertEqual(manifest['cc_gateway']['shared_pool']['message_beta_profile'], 'claude_code_2_1_150_subscription_1m')
        self.assertTrue(manifest['cc_gateway']['shared_pool']['canary_cost_envelope']['allow_context_1m'])
        self.assertEqual(manifest['cc_gateway']['shared_pool']['canary_cost_envelope']['max_context_window_tokens'], 1_000_000)
        self.assertIn('claude-sonnet-4-6', manifest['cc_gateway']['shared_pool']['canary_cost_envelope']['allowed_models'])
        self.assertIn('claude-opus-4-7', manifest['cc_gateway']['shared_pool']['canary_cost_envelope']['allowed_models'])
        self.assertIn('claude-opus-4-7-thinking', manifest['cc_gateway']['shared_pool']['canary_cost_envelope']['allowed_models'])
        self.assertIn('claude-opus-4-6', manifest['cc_gateway']['shared_pool']['canary_cost_envelope']['allowed_models'])
        self.assertIn('claude-opus-4-6-thinking', manifest['cc_gateway']['shared_pool']['canary_cost_envelope']['allowed_models'])
        self.assertIn('claude-haiku-4-5-20251001', manifest['cc_gateway']['shared_pool']['canary_cost_envelope']['allowed_models'])
        self.assertEqual(
            manifest['cc_gateway']['shared_pool']['canary_envelope_role'],
            'localhost_canary_guardrail_not_production_capability_ceiling',
        )
        resolver = manifest['cc_gateway']['shared_pool']['persona_resolver']
        self.assertEqual(resolver['registry_profile'], 'claude_code_2_1_150_subscription_1m')
        self.assertEqual(resolver['exact_version'], '2.1.150')
        self.assertEqual(resolver['trusted_minor_drift']['example_version'], '2.1.151')
        self.assertEqual(resolver['trusted_minor_drift']['decision'], 'observed_minor_drift')
        self.assertFalse(resolver['trusted_minor_drift']['capability_downgrade_allowed'])
        self.assertEqual(resolver['unknown_major']['decision'], 'quarantine_version')
        self.assertEqual(resolver['unknown_major']['action'], 'fail_closed')
        self.assertEqual(resolver['future_candidate_models']['decision'], 'gray_path')
        self.assertFalse(resolver['future_candidate_models']['capability_downgrade_allowed'])
        self.assertIn('claude-sonnet-4-8', manifest['cc_gateway']['shared_pool']['candidate_model_allowlist'])
        self.assertIn('claude-opus-4-8', manifest['cc_gateway']['shared_pool']['candidate_model_allowlist'])
        self.assertEqual(manifest['cc_gateway']['shared_pool']['candidate_model_audit_budgets']['claude-sonnet-4-8'], 25)
        self.assertEqual(manifest['cc_gateway']['shared_pool']['candidate_model_audit_budgets']['claude-opus-4-8'], 25)
        self.assertEqual(manifest['cc_gateway']['env']['version'], '2.1.150')
        account_refs = list(manifest['cc_gateway']['account_identities'])
        self.assertEqual(len(account_refs), 1)
        self.assertEqual(account_refs[0], 'opaque:account-ref:v1:placeholder')
        identity = manifest['cc_gateway']['account_identities'][account_refs[0]]
        self.assertEqual(identity['device_id'], '<from-account-identity>')
        self.assertEqual(identity['account_uuid_ref'], 'opaque:account-ref:v1:placeholder')
        self.assertEqual(identity['account_ref'], 'opaque:account-ref:v1:placeholder')
        self.assertNotIn('account_hash', identity)
        self.assertFalse(manifest['requires_real_anthropic'])

    def test_real_canary_manifest_requires_explicit_flag_and_raw_dir(self):
        with self.assertRaises(RuntimeConfigError):
            build_runtime_manifest(mode='real-canary', run_dir=Path('/tmp/run'), raw_dir=None)

        manifest = build_runtime_manifest(mode='real-canary', run_dir=Path('/tmp/run'), raw_dir=Path('/secure/raw'))

        self.assertTrue(manifest['requires_real_anthropic'])
        self.assertEqual(manifest['required_env']['ALLOW_REAL_ANTHROPIC_CANARY'], '1')
        self.assertEqual(manifest['cc_gateway']['shared_pool']['upstream_mode'], 'real-canary')
        self.assertTrue(manifest['cc_gateway']['shared_pool']['real_canary_user_approved'])
        self.assertEqual(manifest['cc_gateway']['shared_pool']['canary_cost_envelope']['max_tools_count'], 40)
        self.assertTrue(manifest['cc_gateway']['shared_pool']['canary_cost_envelope']['allow_thinking'])

    def test_rendered_config_contains_success_runtime_fields_without_secret_values(self):
        manifest = build_runtime_manifest(mode='real-canary', run_dir=Path('/tmp/run'), raw_dir=Path('/secure/raw'))
        text = render_cc_gateway_config(manifest)

        self.assertIn('message_beta_profile: claude_code_2_1_150_subscription_1m', text)
        self.assertIn('version: 2.1.150', text)
        self.assertIn('billing_cch_mode: sign', text)
        self.assertIn('max_tools_count: 40', text)
        self.assertIn('claude-opus-4-7', text)
        self.assertIn('claude-opus-4-6', text)
        self.assertIn('allow_thinking: true', text)
        self.assertIn('canary_envelope_role: localhost_canary_guardrail_not_production_capability_ceiling', text)
        self.assertIn('trusted_minor_drift:', text)
        self.assertIn('decision: observed_minor_drift', text)
        self.assertIn('action: fail_closed', text)
        self.assertIn('decision: gray_path', text)
        self.assertIn('candidate_model_allowlist:', text)
        self.assertIn('candidate_model_audit_budgets:', text)
        self.assertIn('opaque:account-ref:v1:placeholder', text)
        self.assertNotIn('Bearer ', text)
        self.assertNotIn('sk-', text)

    def test_generated_manifest_and_config_are_sensitive_scan_clean(self):
        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp)
            manifest = build_runtime_manifest(mode='localhost-preflight', run_dir=out, raw_dir=out / 'raw')
            paths = write_runtime_artifacts(manifest, out)

            findings = []
            for path in (paths['manifest'], paths['cc_config']):
                findings.extend(scan_file(path))

        self.assertEqual(findings, [])

    def test_generated_identity_schema_matches_cc_gateway_ref_fields(self):
        manifest = build_runtime_manifest(mode='localhost-preflight', run_dir=Path('/tmp/run'), raw_dir=Path('/tmp/run/raw'))
        account_refs = list(manifest['cc_gateway']['account_identities'])
        identity = manifest['cc_gateway']['account_identities'][account_refs[0]]

        self.assertTrue(identity['device_id'])
        self.assertTrue(identity['account_uuid_ref'])
        self.assertTrue(identity['persona_variant'])
        self.assertTrue(identity['session_policy'])
        self.assertTrue(identity['policy_version'])
        self.assertNotIn('account_uuid_hash', identity)

    def test_validate_manifest_requires_1m_enabled_profile(self):
        manifest = build_runtime_manifest(mode='real-canary', run_dir=Path('/tmp/run'), raw_dir=Path('/secure/raw'))
        validate_runtime_manifest(manifest)

        bad = json.loads(json.dumps(manifest))
        bad['cc_gateway']['shared_pool']['message_beta_profile'] = 'claude_code_2_1_150_subscription'
        with self.assertRaises(RuntimeConfigError):
            validate_runtime_manifest(bad)

    def test_validate_manifest_requires_persona_resolver_candidate_guards_and_known_opus_models(self):
        manifest = build_runtime_manifest(mode='real-canary', run_dir=Path('/tmp/run'), raw_dir=Path('/secure/raw'))

        bad = json.loads(json.dumps(manifest))
        del bad['cc_gateway']['shared_pool']['persona_resolver']
        with self.assertRaises(RuntimeConfigError):
            validate_runtime_manifest(bad)

        bad = json.loads(json.dumps(manifest))
        del bad['cc_gateway']['shared_pool']['candidate_model_replay_proofs']['claude-sonnet-4-8']
        with self.assertRaises(RuntimeConfigError):
            validate_runtime_manifest(bad)

        bad = json.loads(json.dumps(manifest))
        del bad['cc_gateway']['shared_pool']['candidate_model_kill_switches']['claude-opus-4-8']
        with self.assertRaises(RuntimeConfigError):
            validate_runtime_manifest(bad)

        bad = json.loads(json.dumps(manifest))
        bad['cc_gateway']['shared_pool']['candidate_model_audit_budgets'] = {}
        with self.assertRaises(RuntimeConfigError):
            validate_runtime_manifest(bad)

        bad = json.loads(json.dumps(manifest))
        bad['cc_gateway']['shared_pool']['canary_cost_envelope']['allowed_models'].remove('claude-opus-4-6')
        with self.assertRaises(RuntimeConfigError):
            validate_runtime_manifest(bad)

        bad = json.loads(json.dumps(manifest))
        key = next(iter(bad['cc_gateway']['account_identities']))
        bad['cc_gateway']['account_identities'] = {'sha256:' + 'a' * 64: bad['cc_gateway']['account_identities'][key]}
        with self.assertRaises(RuntimeConfigError):
            validate_runtime_manifest(bad)

    def test_write_runtime_artifacts_writes_manifest_config_and_start_script(self):
        with tempfile.TemporaryDirectory() as td:
            out = Path(td)
            manifest = build_runtime_manifest(mode='localhost-preflight', run_dir=out, raw_dir=out / 'raw')
            paths = write_runtime_artifacts(manifest, out)

            self.assertTrue(paths['manifest'].exists())
            self.assertTrue(paths['cc_config'].exists())
            self.assertTrue(paths['start_script'].exists())
            start = paths['start_script'].read_text(encoding='utf-8')
            self.assertIn('ALLOW_REAL_ANTHROPIC_CANARY', start)
            self.assertIn('localhost-preflight', start)

    def test_write_runtime_artifacts_writes_server_staging_mock_runbook(self):
        with tempfile.TemporaryDirectory() as td:
            out = Path(td)
            manifest = build_runtime_manifest(mode='localhost-preflight', run_dir=out, raw_dir=out / 'raw')
            paths = write_runtime_artifacts(manifest, out)

            runbook = paths['server_mock_runbook'].read_text(encoding='utf-8')
            self.assertIn('服务器 staging/mock smoke', runbook)
            self.assertIn('ALLOW_REAL_ANTHROPIC_CANARY=0', runbook)
            self.assertIn('ALLOW_REAL_ANTHROPIC_PRODUCTION=0', runbook)
            self.assertIn('http://127.0.0.1:19082', runbook)
            self.assertIn('SUB2API_SESSION_BUDGET_EXPORT_PATH', runbook)
            self.assertIn('不要发真实 Anthropic 请求', runbook)
            self.assertIn('不要 docker compose down -v', runbook)
            self.assertIn('不要删除数据目录', runbook)
            self.assertIn('python3 tools/safe_deliverable_sensitive_scan.py', runbook)

    def test_production_session_defaults_to_observe_only_budget_and_disables_raw_capture(self):
        with tempfile.TemporaryDirectory() as td:
            out = Path(td)
            manifest = build_runtime_manifest(mode='production-session', run_dir=out, raw_dir=out / 'raw')
            self.assertEqual(manifest['session_budget']['mode'], 'observe_only')
            self.assertFalse(manifest['session_budget']['enforcement_enabled'])
            self.assertEqual(manifest['cc_gateway']['shared_pool']['upstream_mode'], 'production')
            self.assertTrue(manifest['cc_gateway']['shared_pool']['production_upstream_enabled'])
            self.assertEqual(manifest['required_env']['ALLOW_REAL_ANTHROPIC_PRODUCTION'], '1')
            self.assertNotIn('ALLOW_REAL_ANTHROPIC_CANARY', manifest['required_env'])
            self.assertNotIn('real_canary_user_approved', manifest['cc_gateway']['shared_pool'])
            self.assertNotIn('canary_cost_envelope', manifest['cc_gateway']['shared_pool'])
            self.assertNotIn('max_body_bytes', manifest['cc_gateway']['shared_pool'])
            self.assertNotIn('max_messages_per_session', manifest['session_budget'])
            self.assertNotIn('max_rich_messages_per_session', manifest['session_budget'])
            paths = write_runtime_artifacts(manifest, out)
            start = paths['start_script'].read_text(encoding='utf-8')
            self.assertNotIn('CC_GATEWAY_RAW_CAPTURE_DIR', start)
            self.assertIn('ALLOW_REAL_ANTHROPIC_PRODUCTION=1 is required', start)
            self.assertNotIn('ALLOW_REAL_ANTHROPIC_CANARY=1 is required', start)

    def test_validate_manifest_rejects_production_session_hard_message_caps(self):
        manifest = build_runtime_manifest(mode='production-session', run_dir=Path('/tmp/run'), raw_dir=Path('/tmp/run/raw'))
        bad = json.loads(json.dumps(manifest))
        bad['session_budget']['mode'] = 'session_budgeted'
        bad['session_budget']['enforcement_enabled'] = True
        bad['session_budget']['max_messages_per_session'] = 20
        with self.assertRaises(RuntimeConfigError):
            validate_runtime_manifest(bad)

    def test_validate_manifest_rejects_production_session_canary_inheritance(self):
        manifest = build_runtime_manifest(mode='production-session', run_dir=Path('/tmp/run'), raw_dir=Path('/tmp/run/raw'))

        bad = json.loads(json.dumps(manifest))
        bad['cc_gateway']['shared_pool']['upstream_mode'] = 'real-canary'
        with self.assertRaises(RuntimeConfigError):
            validate_runtime_manifest(bad)

        bad = json.loads(json.dumps(manifest))
        bad['cc_gateway']['shared_pool']['real_canary_user_approved'] = True
        with self.assertRaises(RuntimeConfigError):
            validate_runtime_manifest(bad)

        bad = json.loads(json.dumps(manifest))
        bad['cc_gateway']['shared_pool']['canary_cost_envelope'] = {'enabled': True}
        with self.assertRaises(RuntimeConfigError):
            validate_runtime_manifest(bad)

        bad = json.loads(json.dumps(manifest))
        bad['cc_gateway']['shared_pool']['max_body_bytes'] = 2097152
        with self.assertRaises(RuntimeConfigError):
            validate_runtime_manifest(bad)

    def test_checkpoint3_docs_artifacts_exist(self):
        base = Path('/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/docs/anti-ban/runtime-productization/2026-05-24-cli-through')
        self.assertTrue((base / 'manifest.json').exists())
        self.assertTrue((base / 'cc-gateway.config.yaml').exists())

    def test_checkpoint3_docs_artifacts_encode_dynamic_persona_resolver_contract(self):
        base = Path('/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/docs/anti-ban/runtime-productization/2026-05-24-cli-through')
        manifest = json.loads((base / 'manifest.json').read_text(encoding='utf-8'))
        config_text = (base / 'cc-gateway.config.yaml').read_text(encoding='utf-8')

        resolver = manifest['cc_gateway']['shared_pool']['persona_resolver']
        self.assertEqual(resolver['trusted_minor_drift']['example_version'], '2.1.151')
        self.assertEqual(resolver['trusted_minor_drift']['decision'], 'observed_minor_drift')
        self.assertEqual(resolver['unknown_major']['action'], 'fail_closed')
        self.assertEqual(resolver['future_candidate_models']['decision'], 'gray_path')
        self.assertFalse(resolver['future_candidate_models']['capability_downgrade_allowed'])
        self.assertIn('claude-opus-4-7', manifest['cc_gateway']['shared_pool']['canary_cost_envelope']['allowed_models'])
        self.assertIn('claude-opus-4-6', manifest['cc_gateway']['shared_pool']['canary_cost_envelope']['allowed_models'])
        self.assertNotIn('"sha256:', json.dumps(manifest, ensure_ascii=False))
        self.assertIn('localhost_canary_guardrail_not_production_capability_ceiling', config_text)
        self.assertIn('decision: observed_minor_drift', config_text)
        self.assertIn('decision: gray_path', config_text)
        self.assertNotIn('sha256:4e07408562bedb8b60ce05c1decfe3ad16b72230967de01f640b7e4729b49fce', config_text)


if __name__ == '__main__':
    unittest.main()
