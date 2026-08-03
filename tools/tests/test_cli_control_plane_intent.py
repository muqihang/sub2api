import importlib
import json
import unittest


class CliControlPlaneIntentTest(unittest.TestCase):
    def _load_module(self):
        try:
            return importlib.import_module("tools.cli_control_plane_intent")
        except Exception as exc:  # noqa: BLE001 - fail as test red signal
            self.fail(f"tools.cli_control_plane_intent import failed: {exc}")

    def _build_base_envelope(self, **overrides):
        module = self._load_module()
        params = {
            "method": "GET",
            "path": "/api/claude_cli/bootstrap?entrypoint=sdk-cli",
            "headers": {
                "Authorization": "Bearer secret-token-marker",
                "x-api-key": "secret-api-key-marker",
                "Cookie": "session=secret-cookie-marker",
                "Proxy-Authorization": "Basic proxy-credential-marker",
                "User-Agent": "claude-cli/2.1.150 (external, sdk-cli)",
            },
            "body": None,
            "classification": "bootstrap_settings_or_feature_flag_stubbed",
            "policy_version": 1,
            "strategy_version": 2,
            "response_schema_version": 3,
            "routing_intent": "stub_json",
        }
        params.update(overrides)
        return module.build_control_plane_intent(**params)

    def test_intent_envelope_contains_only_safe_checkpoint1_fields(self):
        module = self._load_module()
        envelope = self._build_base_envelope()

        self.assertEqual(
            set(envelope.keys()),
            {
                "method",
                "path_template",
                "normalized_query",
                "query_ref",
                "query_omitted_reason",
                "classification",
                "policy_version",
                "strategy_version",
                "response_schema_version",
                "routing_intent",
                "body_length_bucket",
                "schema_summary",
                "body_omitted_reason",
                "digest_omitted_reason",
                "redaction_proof",
            },
        )
        self.assertEqual(envelope["method"], "GET")
        self.assertEqual(envelope["path_template"], "/api/claude_cli/bootstrap")
        self.assertEqual(envelope["normalized_query"], {"entrypoint": "sdk-cli"})
        self.assertEqual(envelope["query_omitted_reason"], None)
        self.assertEqual(envelope["query_ref"]["scope"], "control_plane_query")
        self.assertTrue(envelope["query_ref"]["value"].startswith("hmac-sha256:"))
        self.assertEqual(envelope["body_length_bucket"], "empty")
        self.assertEqual(envelope["body_omitted_reason"], "not_applicable")
        self.assertEqual(envelope["digest_omitted_reason"], "not_applicable")
        self.assertEqual(envelope["schema_summary"], {"content_kind": "none", "top_level_type": "none"})
        self.assertEqual(envelope["classification"], "bootstrap_settings_or_feature_flag_stubbed")
        self.assertEqual(envelope["routing_intent"], "stub_json")
        self.assertEqual(envelope["redaction_proof"]["sensitive_scan"], "clean")

        dumped = json.dumps(envelope, sort_keys=True)
        for marker in (
            "secret-token-marker",
            "secret-api-key-marker",
            "secret-cookie-marker",
            "proxy-credential-marker",
            '"query_hash"',
            '"body_hash"',
        ):
            self.assertNotIn(marker, dumped)

        module.validate_control_plane_intent(envelope)

    def test_post_high_risk_control_plane_body_uses_bucket_and_schema_summary_only(self):
        module = self._load_module()
        envelope = self._build_base_envelope(
            method="POST",
            path="/api/event_logging/v2/batch",
            body=json.dumps({
                "11111111-2222-4333-8444-555555555555": "opaque",
                "session-id-abc123": "opaque",
                "session_id_abc123": "opaque",
                "account-abc123": "opaque",
                "account_id_abc123": "opaque",
                "org_abc123": "opaque",
                "project_abc123": "opaque",
                "user_abc123": "opaque",
                "events": [{"type": "raw-prompt-marker", "token": "secret-token-marker"}],
                "metadata": {"prompt": "raw-prompt-marker"},
            }).encode("utf-8"),
            classification="telemetry_or_eval_suppressed",
            routing_intent="suppress_204",
        )

        self.assertEqual(envelope["normalized_query"], {})
        self.assertEqual(envelope["query_ref"], None)
        self.assertEqual(envelope["query_omitted_reason"], "no_query")
        self.assertEqual(envelope["body_omitted_reason"], "high_risk_body_not_retained")
        self.assertEqual(envelope["digest_omitted_reason"], "raw_body_digest_forbidden_by_policy")
        self.assertNotEqual(envelope["body_length_bucket"], "empty")
        self.assertEqual(envelope["schema_summary"]["content_kind"], "json")
        dumped = json.dumps(envelope, sort_keys=True)
        self.assertNotIn("secret-token-marker", dumped)
        self.assertNotIn("raw-prompt-marker", dumped)
        self.assertNotIn("11111111-2222-4333-8444-555555555555", dumped)
        for marker in (
            "session-id-abc123",
            "session_id_abc123",
            "account-abc123",
            "account_id_abc123",
            "org_abc123",
            "project_abc123",
            "user_abc123",
        ):
            self.assertNotIn(marker, dumped)
        self.assertNotIn('"body_hash"', dumped)
        self.assertNotIn('"query_hash"', dumped)

    def test_intent_rejects_plain_hash_fields_and_forged_transport_markers(self):
        module = self._load_module()
        envelope = self._build_base_envelope()
        envelope["query_hash"] = "sha256:" + "a" * 64
        with self.assertRaises(module.IntentValidationError):
            module.validate_control_plane_intent(envelope)

        envelope = self._build_base_envelope()
        envelope["query_ref"]["value"] = "sha256:" + "b" * 64
        with self.assertRaises(module.IntentValidationError):
            module.validate_control_plane_intent(envelope)

        with self.assertRaises(module.IntentValidationError):
            self._build_base_envelope(headers={"X-Anthropic-Billing-Header": "cch=00000"})

        with self.assertRaises(module.IntentValidationError):
            self._build_base_envelope(headers={"X-Sub2API-Control-Plane-Intent": "spoofed"})

    def test_intent_rejects_non_allowlisted_query_and_safe_intent_unknown_fields(self):
        module = self._load_module()
        for path in (
            "/api/claude_cli/bootstrap?entrypoint=sdk-cli&token=secret-token-marker",
            "/api/oauth/account/settings?email=user@example.com",
            "/api/claude_code_feature_flags?cookie=cookie-marker",
        ):
            with self.subTest(path=path):
                with self.assertRaises(module.IntentValidationError):
                    self._build_base_envelope(path=path)

        envelope = self._build_base_envelope()
        envelope["raw_body"] = "do-not-store"
        with self.assertRaises(module.IntentValidationError):
            module.validate_control_plane_intent(envelope)

    def test_intent_rejects_local_account_org_user_identifiers(self):
        module = self._load_module()

        org_envelope = self._build_base_envelope(
            path="/api/oauth/organizations/local-org-secret/referral/eligibility"
        )
        self.assertEqual(
            org_envelope["path_template"],
            "/api/oauth/organizations/{org}/referral/eligibility",
        )
        self.assertNotIn("local-org-secret", json.dumps(org_envelope, sort_keys=True))

        user_envelope = self._build_base_envelope(
            path="/api/users/00000000-0000-0000-0000-000000000000/settings"
        )
        self.assertEqual(user_envelope["path_template"], "/api/users/{user}/settings")
        self.assertNotIn("00000000-0000-0000-0000-000000000000", json.dumps(user_envelope, sort_keys=True))

        with self.assertRaises(module.IntentValidationError):
            self._build_base_envelope(path="/api/tenants/local-org-secret/settings")


    def test_claude_code_organizations_metrics_path_is_not_templated_as_org_id(self):
        module = self._load_module()
        envelope = self._build_base_envelope(
            path="/api/claude_code/organizations/metrics_enabled",
            classification="claude_code_feature_flags_stubbed",
        )
        self.assertEqual(envelope["path_template"], "/api/claude_code/organizations/metrics_enabled")

    def test_build_control_plane_intent_uses_safe_default_routing_intent(self):
        module = self._load_module()
        envelope = module.build_control_plane_intent(
            method="GET",
            path="/api/oauth/account/settings",
            headers={"User-Agent": "claude-cli/2.1.150 (external, sdk-cli)"},
            body=None,
            classification="bootstrap_settings_or_feature_flag_stubbed",
            policy_version=1,
            strategy_version=1,
            response_schema_version=1,
        )
        self.assertEqual(envelope["routing_intent"], "local_stub_or_suppress")
        self.assertEqual(envelope["query_omitted_reason"], "no_query")
        module.validate_control_plane_intent(envelope)

    def test_validate_control_plane_intent_rejects_non_positive_versions(self):
        module = self._load_module()
        with self.assertRaises(module.IntentValidationError):
            self._build_base_envelope(policy_version=0)
        with self.assertRaises(module.IntentValidationError):
            self._build_base_envelope(strategy_version=-1)
        with self.assertRaises(module.IntentValidationError):
            self._build_base_envelope(response_schema_version=0)


if __name__ == "__main__":
    unittest.main()
