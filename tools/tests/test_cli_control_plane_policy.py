import copy
import json
import unittest

from tools.cli_control_plane_policy import (
    ControlPlanePolicy,
    PolicyConfigError,
    load_default_policy,
)


def _base_policy_dict():
    return {
        "schema_version": 1,
        "mode": "localhost_preflight",
        "summary_path": "artifacts/control-plane-summary.jsonl",
        "redaction": {
            "headers": ["authorization", "cookie"],
            "json_fields": ["prompt", "messages", "email"],
        },
        "messages": {
            "allowed_routes": [
                {"method": "POST", "path": "/v1/messages", "query": "beta=true"}
            ],
            "max_messages": 1,
            "stop_cli_after_first_response": False,
            "cost_envelope": {
                "max_input_tokens": 0,
                "max_output_tokens": 0,
                "max_usd_micros": 0,
            },
        },
        "control_plane": {
            "defaults": {
                "upload_strategy": "disabled",
                "auth_source": "none",
                "cache_scope": "none",
                "body_policy": "forbidden",
                "response_policy": "sanitized_schema",
                "upload_kill_switch": True,
                "ttl_seconds": 0,
                "policy_version": 1,
                "strategy_version": 1,
                "response_schema_version": 1,
            },
            "telemetry": {
                "match": [
                    {"method": "POST", "path": "/api/event_logging/v2/batch"},
                    {"method": "POST", "path_prefix": "/api/eval/"},
                ],
                "action": "suppress_204",
            },
            "mcp": {
                "match": [
                    {"method": "GET", "path": "/v1/mcp_servers"},
                ],
                "action": "stub_json",
                "response": {
                    "status": 200,
                    "content_type": "application/json",
                    "body": {"data": [], "servers": []},
                },
            },
            "bootstrap_settings": {
                "match": [
                    {"method": "GET", "path": "/api/claude_cli/bootstrap"},
                ],
                "action": "stub_json",
                "response": {
                    "status": 200,
                    "content_type": "application/json",
                    "body": {},
                },
            },
            "sensitive_get_candidates": {
                "match": [
                    {"method": "GET", "path": "/api/oauth/account/settings"},
                ],
                "action": "quarantine_block",
            },
            "readiness_only_candidate_families": {
                "match": [
                    {"method": "GET", "path_prefix": "/api/claude_code_"},
                    {"method": "GET", "path_prefix": "/mcp-registry/"},
                ],
                "action": "quarantine_block",
            },
        },
        "unknown": {"action": "quarantine_block"},
        "connect": {
            "allowed_stub_targets": ["api.anthropic.com:443"],
            "unknown_target_action": "block_403",
        },
    }


class CliControlPlanePolicyTest(unittest.TestCase):
    def test_load_default_policy_classifies_required_routes(self):
        policy = load_default_policy()

        self.assertEqual(policy.decide("POST", "/v1/messages?beta=true").action, "forward_messages")
        self.assertEqual(policy.decide("POST", "/api/event_logging/v2/batch").action, "suppress_204")
        self.assertEqual(policy.decide("POST", "/api/eval/redacted").action, "suppress_204")
        self.assertEqual(policy.decide("POST", "/api/eval/other").action, "suppress_204")

        mcp = policy.decide("GET", "/v1/mcp_servers?limit=1000")
        self.assertEqual(mcp.action, "stub_json")
        self.assertEqual(mcp.status, 200)
        self.assertEqual(mcp.content_type, "application/json")
        self.assertEqual(mcp.body, {"data": [], "servers": []})
        self.assertTrue(mcp.reason)

        self.assertEqual(policy.decide("GET", "/mcp-registry/v0/servers?version=latest").action, "quarantine_block")
        self.assertEqual(policy.decide("GET", "/mcp-registry/anything").action, "quarantine_block")
        self.assertEqual(policy.decide("GET", "/api/claude_cli/bootstrap?entrypoint=sdk-cli").action, "stub_json")
        self.assertEqual(policy.decide("GET", "/api/oauth/account/settings").action, "quarantine_block")
        self.assertEqual(policy.decide("GET", "/api/claude_code_grove").action, "quarantine_block")
        self.assertEqual(policy.decide("GET", "/api/claude_code_penguin_mode").action, "quarantine_block")
        self.assertEqual(
            policy.decide("GET", "/api/oauth/organizations/local-org/referral/eligibility").action,
            "quarantine_block",
        )
        self.assertEqual(policy.decide("GET", "/unknown").action, "quarantine_block")

    def test_messages_route_requires_exact_query(self):
        policy = load_default_policy()
        self.assertEqual(policy.decide("POST", "/v1/messages?beta=true").action, "forward_messages")

        invalid_paths = [
            "/v1/messages",
            "/v1/messages?beta=false",
            "/v1/messages?beta=true&x=1",
            "/v1/messages?x=1&beta=true",
            "/v1/messages?Beta=true",
            "/v1/messages?beta=True",
            "/v1/messages?beta%3Dtrue",
        ]
        for path in invalid_paths:
            with self.subTest(path=path):
                self.assertEqual(policy.decide("POST", path).action, "quarantine_block")

    def test_from_dict_rejects_unknown_top_level_and_nested_fields(self):
        policy_dict = _base_policy_dict()
        policy_dict["extra"] = True
        with self.assertRaises(PolicyConfigError):
            ControlPlanePolicy.from_dict(policy_dict)

        nested = _base_policy_dict()
        nested["messages"]["unexpected"] = True
        with self.assertRaises(PolicyConfigError):
            ControlPlanePolicy.from_dict(nested)

        nested_defaults = _base_policy_dict()
        nested_defaults["control_plane"]["defaults"]["extra_field"] = 1
        with self.assertRaises(PolicyConfigError):
            ControlPlanePolicy.from_dict(nested_defaults)

    def test_from_dict_rejects_invalid_mode_regex_duplicate_and_action(self):
        invalid_mode = _base_policy_dict()
        invalid_mode["mode"] = "not-a-mode"
        with self.assertRaises(PolicyConfigError):
            ControlPlanePolicy.from_dict(invalid_mode)

        invalid_regex = _base_policy_dict()
        invalid_regex["control_plane"]["bootstrap_settings"]["match"].append(
            {"method": "GET", "path_regex": "("}
        )
        with self.assertRaises(PolicyConfigError):
            ControlPlanePolicy.from_dict(invalid_regex)

        duplicate = _base_policy_dict()
        duplicate["control_plane"]["telemetry"]["match"].append(
            {"method": "POST", "path": "/api/event_logging/v2/batch"}
        )
        with self.assertRaises(PolicyConfigError):
            ControlPlanePolicy.from_dict(duplicate)

        overlap_exact_prefix = _base_policy_dict()
        overlap_exact_prefix["control_plane"]["telemetry"]["match"].append(
            {"method": "POST", "path": "/api/eval/redacted"}
        )
        with self.assertRaises(PolicyConfigError):
            ControlPlanePolicy.from_dict(overlap_exact_prefix)

        overlap_prefix_prefix = _base_policy_dict()
        overlap_prefix_prefix["control_plane"]["mcp"]["match"].append(
            {"method": "GET", "path_prefix": "/mcp-registry/v0/"}
        )
        with self.assertRaises(PolicyConfigError):
            ControlPlanePolicy.from_dict(overlap_prefix_prefix)

        invalid_action = _base_policy_dict()
        invalid_action["unknown"]["action"] = "allow"
        with self.assertRaises(PolicyConfigError):
            ControlPlanePolicy.from_dict(invalid_action)

        for action in ("forward_messages", "suppress_204"):
            invalid_unknown_action = _base_policy_dict()
            invalid_unknown_action["unknown"]["action"] = action
            with self.subTest(unknown_action=action):
                with self.assertRaises(PolicyConfigError):
                    ControlPlanePolicy.from_dict(invalid_unknown_action)

        for action in ("forward_messages", "suppress_204"):
            invalid_connect_action = _base_policy_dict()
            invalid_connect_action["connect"]["unknown_target_action"] = action
            with self.subTest(connect_unknown_target_action=action):
                with self.assertRaises(PolicyConfigError):
                    ControlPlanePolicy.from_dict(invalid_connect_action)

    def test_stub_json_requires_complete_response(self):
        policy_dict = _base_policy_dict()
        del policy_dict["control_plane"]["mcp"]["response"]["body"]
        with self.assertRaises(PolicyConfigError):
            ControlPlanePolicy.from_dict(policy_dict)

    def test_future_upload_defaults_are_b1_disabled_and_invalid_values_rejected(self):
        policy = load_default_policy()
        defaults = policy.control_plane_defaults
        self.assertEqual(defaults["upload_strategy"], "disabled")
        self.assertEqual(defaults["auth_source"], "none")
        self.assertEqual(defaults["cache_scope"], "none")
        self.assertEqual(defaults["body_policy"], "forbidden")
        self.assertEqual(defaults["response_policy"], "sanitized_schema")
        self.assertTrue(defaults["upload_kill_switch"])
        self.assertEqual(defaults["ttl_seconds"], 0)
        self.assertGreater(defaults["policy_version"], 0)
        self.assertGreater(defaults["strategy_version"], 0)
        self.assertGreater(defaults["response_schema_version"], 0)

        rejected_variants = [
            ("upload_strategy", "account_scoped_passthrough"),
            ("upload_strategy", "account_scoped_cached_fetch"),
            ("upload_strategy", "public_cached_fetch"),
            ("upload_strategy", "synthetic_telemetry"),
            ("auth_source", "selected_pool_account"),
            ("auth_source", "local_user_auth"),
            ("cache_scope", "account"),
            ("cache_scope", "org"),
            ("cache_scope", "user_session"),
            ("cache_scope", "public"),
            ("body_policy", "forward_allowlisted_fields"),
            ("body_policy", "synthesize"),
            ("body_policy", "raw_passthrough"),
            ("response_policy", "raw_to_client"),
            ("response_policy", "public_raw_allowlisted"),
        ]
        for field, value in rejected_variants:
            invalid = _base_policy_dict()
            invalid["control_plane"]["defaults"][field] = value
            with self.subTest(field=field, value=value):
                with self.assertRaises(PolicyConfigError):
                    ControlPlanePolicy.from_dict(invalid)

        for field, value in [("upload_kill_switch", False), ("ttl_seconds", 10)]:
            invalid = _base_policy_dict()
            invalid["control_plane"]["defaults"][field] = value
            with self.subTest(field=field, value=value):
                with self.assertRaises(PolicyConfigError):
                    ControlPlanePolicy.from_dict(invalid)

    def test_fingerprint_is_stable_sha256_and_secret_free(self):
        policy = ControlPlanePolicy.from_dict(_base_policy_dict())
        fingerprint_one = policy.fingerprint()
        fingerprint_two = ControlPlanePolicy.from_dict(copy.deepcopy(_base_policy_dict())).fingerprint()

        self.assertTrue(fingerprint_one.startswith("sha256:"))
        self.assertEqual(fingerprint_one, fingerprint_two)
        self.assertNotIn("authorization", fingerprint_one.lower())
        self.assertNotIn("cookie", fingerprint_one.lower())
        self.assertNotIn("prompt", fingerprint_one.lower())
        self.assertLessEqual(len(fingerprint_one), 71)


if __name__ == "__main__":
    unittest.main()
