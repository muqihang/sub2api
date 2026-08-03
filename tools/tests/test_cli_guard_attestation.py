import base64
import json
import unittest

from tools.cli_control_plane_intent import build_control_plane_intent
from tools.cli_guard_attestation import (
    AttestationValidationError,
    GuardAttestationConfig,
    NonceReplayCache,
    build_guard_attestation,
    verify_guard_attestation,
)


class CliGuardAttestationTest(unittest.TestCase):
    def _intent(self, **overrides):
        params = {
            "method": "GET",
            "path": "/api/claude_cli/bootstrap?entrypoint=sdk-cli",
            "headers": {"User-Agent": "claude-cli/2.1.150 (external, sdk-cli)"},
            "body": None,
            "classification": "bootstrap_settings_or_feature_flag_stubbed",
            "policy_version": 1,
            "strategy_version": 1,
            "response_schema_version": 1,
            "routing_intent": "local_stub_or_suppress",
        }
        params.update(overrides)
        return build_control_plane_intent(**params)

    def _config(self):
        return GuardAttestationConfig(
            current_key_id="guard_v2",
            keys={"guard_v2": "secret-v2", "guard_v1": "secret-v1"},
            scope="control_plane_intent",
            version=1,
            nonce_ttl_seconds=120,
            clock_skew_seconds=30,
        )

    def test_valid_attestation_accepted(self):
        intent = self._intent()
        config = self._config()
        cache = NonceReplayCache(ttl_seconds=120, now_fn=lambda: 1000)
        attestation, signature = build_guard_attestation(
            intent,
            request_headers={"x-claude-code-session-id": "11111111-2222-4333-8444-555555555555"},
            config=config,
            now=1000,
            nonce="nonce-001",
        )

        payload = verify_guard_attestation(intent, attestation, signature, config=config, nonce_cache=cache, now=1000)
        self.assertEqual(payload["key_id"], "guard_v2")
        self.assertEqual(payload["method"], "GET")
        self.assertEqual(payload["path_template"], "/api/claude_cli/bootstrap")
        self.assertEqual(payload["session_ref"]["scope"], "session_budget_session")

    def test_missing_attestation_rejected(self):
        with self.assertRaises(AttestationValidationError):
            verify_guard_attestation(self._intent(), None, None, config=self._config(), nonce_cache=NonceReplayCache())

    def test_wrong_key_rejected(self):
        intent = self._intent()
        config = self._config()
        attestation, signature = build_guard_attestation(intent, config=config, now=1000, nonce="nonce-002")
        payload = json.loads(base64.urlsafe_b64decode(attestation + "==").decode("utf-8"))
        payload["key_id"] = "unknown-key"
        tampered = base64.urlsafe_b64encode(json.dumps(payload, sort_keys=True, separators=(",", ":")).encode("utf-8")).decode("ascii").rstrip("=")
        with self.assertRaises(AttestationValidationError):
            verify_guard_attestation(intent, tampered, signature, config=config, nonce_cache=NonceReplayCache(), now=1000)

    def test_expired_timestamp_and_clock_skew_rejected(self):
        intent = self._intent()
        config = self._config()
        cache = NonceReplayCache(ttl_seconds=120, now_fn=lambda: 2000)
        attestation, signature = build_guard_attestation(intent, config=config, now=1500, nonce="nonce-003")
        with self.assertRaises(AttestationValidationError):
            verify_guard_attestation(intent, attestation, signature, config=config, nonce_cache=cache, now=2000)

        attestation, signature = build_guard_attestation(intent, config=config, now=2040, nonce="nonce-004")
        with self.assertRaises(AttestationValidationError):
            verify_guard_attestation(intent, attestation, signature, config=config, nonce_cache=NonceReplayCache(ttl_seconds=120, now_fn=lambda: 2000), now=2000)

    def test_replay_nonce_and_ttl(self):
        intent = self._intent()
        config = self._config()
        now_ref = {"value": 1000}
        cache = NonceReplayCache(ttl_seconds=5, now_fn=lambda: now_ref["value"])
        attestation, signature = build_guard_attestation(intent, config=config, now=1000, nonce="nonce-005")
        verify_guard_attestation(intent, attestation, signature, config=config, nonce_cache=cache, now=1000)
        with self.assertRaises(AttestationValidationError):
            verify_guard_attestation(intent, attestation, signature, config=config, nonce_cache=cache, now=1001)
        now_ref["value"] = 1007
        verify_guard_attestation(intent, attestation, signature, config=config, nonce_cache=cache, now=1007)

    def test_rotation_overlap_only_accepts_configured_key_ids(self):
        intent = self._intent()
        config = self._config()
        attestation, signature = build_guard_attestation(intent, config=config, now=1000, nonce="nonce-006", key_id="guard_v1")
        payload = verify_guard_attestation(intent, attestation, signature, config=config, nonce_cache=NonceReplayCache(), now=1000)
        self.assertEqual(payload["key_id"], "guard_v1")

        with self.assertRaises(AttestationValidationError):
            build_guard_attestation(intent, config=config, now=1000, nonce="nonce-007", key_id="missing")

    def test_payload_binds_method_path_and_digest_omission(self):
        intent = self._intent(method="POST", path="/api/event_logging/v2/batch", body=b'{"events":[]}', classification="telemetry_or_eval_suppressed", routing_intent="local_stub_or_suppress")
        config = self._config()
        attestation, signature = build_guard_attestation(intent, config=config, now=1000, nonce="nonce-008")
        tampered = dict(intent)
        tampered["digest_omitted_reason"] = "not_applicable"
        with self.assertRaises(AttestationValidationError):
            verify_guard_attestation(tampered, attestation, signature, config=config, nonce_cache=NonceReplayCache(), now=1000)

        tampered = dict(intent)
        tampered["body_omitted_reason"] = "not_applicable"
        with self.assertRaises(AttestationValidationError):
            verify_guard_attestation(tampered, attestation, signature, config=config, nonce_cache=NonceReplayCache(), now=1000)

        tampered = dict(intent)
        tampered["normalized_query"] = {"entrypoint": "tampered"}
        with self.assertRaises(AttestationValidationError):
            verify_guard_attestation(tampered, attestation, signature, config=config, nonce_cache=NonceReplayCache(), now=1000)

        tampered = dict(intent)
        tampered["schema_summary"] = {"content_kind": "json", "top_level_type": "array"}
        with self.assertRaises(AttestationValidationError):
            verify_guard_attestation(tampered, attestation, signature, config=config, nonce_cache=NonceReplayCache(), now=1000)


if __name__ == "__main__":
    unittest.main()
