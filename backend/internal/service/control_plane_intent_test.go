package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestControlPlaneIntentServiceEvaluateIntentAcceptsBootstrapSafeIntent(t *testing.T) {
	svc := NewControlPlaneIntentService()
	payload := baseControlPlaneIntentPayload()

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	decision, err := svc.EvaluateIntent(body)
	require.NoError(t, err)
	require.Equal(t, "stub_json", decision.Decision)
	require.Equal(t, 200, decision.Status)
	require.Equal(t, "/api/claude_cli/bootstrap", decision.Audit.PathTemplate)
	require.Equal(t, map[string]string{"entrypoint": "sdk-cli"}, decision.Audit.NormalizedQuery)
	require.Equal(t, "empty", decision.Audit.BodyLengthBucket)
}

func TestControlPlaneIntentServiceUsesConfiguredSafeMatrixOverlay(t *testing.T) {
	matrix, err := NewControlPlanePathPolicyMatrixFromConfig(ControlPlanePathPolicyMatrixConfig{Policies: []ControlPlanePathPolicyConfig{{
		Method: "GET", PathTemplate: "/api/claude_cli/safe_profile", Classification: "safe_profile_stubbed",
		Action: ControlPlaneActionStub, CacheScope: "session", TTLSeconds: 60, QuarantineOnMismatch: true, RawForbidden: true,
		QueryAllowlist:      map[string]ControlPlaneQueryRuleConfig{"entrypoint": {Kind: "enum", EnumValues: []string{"sdk-cli"}}},
		AllowedResponseKeys: []string{"ok", "profile"},
	}}})
	require.NoError(t, err)
	svc := NewControlPlaneIntentService(WithControlPlaneIntentPolicyMatrix(matrix))
	payload := baseControlPlaneIntentPayload()
	payload["path_template"] = "/api/claude_cli/safe_profile"
	payload["classification"] = "safe_profile_stubbed"
	payload["normalized_query"] = map[string]string{"entrypoint": "sdk-cli"}
	payload["query_ref"] = map[string]any{"key_id": "local_guard_v1", "scope": "control_plane_query", "version": 1, "value": "hmac-sha256:" + strings.Repeat("d", 64)}

	body, err := json.Marshal(payload)
	require.NoError(t, err)
	decision, err := svc.EvaluateIntent(body)
	require.NoError(t, err)
	require.Equal(t, ControlPlaneActionStub, decision.Decision)
	require.Equal(t, "control_plane:path_policy_allow", decision.Reason)
	require.Equal(t, "safe_profile_stubbed", decision.Audit.Classification)
}

func TestControlPlaneIntentServiceEvaluateIntentRejectsPlainHashFields(t *testing.T) {
	svc := NewControlPlaneIntentService()
	payload := baseControlPlaneIntentPayload()
	payload["query_hash"] = "sha256:" + strings.Repeat("a", 64)

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	_, err = svc.EvaluateIntent(body)
	require.Error(t, err)

	payload = baseControlPlaneIntentPayload()
	queryRef := payload["query_ref"].(map[string]any)
	queryRef["value"] = "sha256:" + strings.Repeat("b", 64)
	body, err = json.Marshal(payload)
	require.NoError(t, err)

	_, err = svc.EvaluateIntent(body)
	require.Error(t, err)
}

func TestControlPlaneIntentServiceEvaluateIntentSuppressesTelemetryWithoutBodyDigest(t *testing.T) {
	svc := NewControlPlaneIntentService()
	payload := map[string]any{
		"method":                  "POST",
		"path_template":           "/api/event_logging/v2/batch",
		"normalized_query":        map[string]string{},
		"query_ref":               nil,
		"query_omitted_reason":    "no_query",
		"classification":          "telemetry_or_eval_suppressed",
		"policy_version":          1,
		"strategy_version":        1,
		"response_schema_version": 1,
		"routing_intent":          "local_stub_or_suppress",
		"body_length_bucket":      "256_1023_bytes",
		"schema_summary":          map[string]any{"content_kind": "json", "top_level_type": "object", "top_level_keys": []any{"events"}},
		"body_omitted_reason":     "high_risk_body_not_retained",
		"digest_omitted_reason":   "raw_body_digest_forbidden_by_policy",
		"redaction_proof": map[string]any{
			"sensitive_scan":            "clean",
			"path_identifiers_redacted": false,
			"raw_query_persisted":       false,
			"body_persisted":            false,
			"raw_body_digest_persisted": false,
		},
	}

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	decision, err := svc.EvaluateIntent(body)
	require.NoError(t, err)
	require.Equal(t, "suppress_204", decision.Decision)
	require.Equal(t, 204, decision.Status)
	require.Equal(t, "high_risk_body_not_retained", decision.Audit.BodyOmittedReason)
	require.Equal(t, "raw_body_digest_forbidden_by_policy", decision.Audit.DigestOmittedReason)
}

func TestControlPlaneIntentServiceEvaluateIntentQuarantinesUnknownPath(t *testing.T) {
	svc := NewControlPlaneIntentService()
	payload := baseControlPlaneIntentPayload()
	payload["path_template"] = "/totally_unknown"
	payload["normalized_query"] = map[string]string{}
	payload["query_ref"] = nil
	payload["query_omitted_reason"] = "no_query"

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	decision, err := svc.EvaluateIntent(body)
	require.NoError(t, err)
	require.Equal(t, "quarantine_block", decision.Decision)
	require.Equal(t, 403, decision.Status)
}

func TestControlPlaneIntentServiceEvaluateIntentRejectsDynamicSchemaSummaryKeys(t *testing.T) {
	svc := NewControlPlaneIntentService()
	payload := baseControlPlaneIntentPayload()
	payload["method"] = "POST"
	payload["path_template"] = "/api/event_logging/v2/batch"
	payload["normalized_query"] = map[string]string{}
	payload["query_ref"] = nil
	payload["query_omitted_reason"] = "no_query"
	payload["classification"] = "telemetry_or_eval_suppressed"
	payload["body_length_bucket"] = "1_255_bytes"
	payload["schema_summary"] = map[string]any{
		"content_kind":   "json",
		"top_level_type": "object",
		"top_level_keys": []any{
			"11111111-2222-4333-8444-555555555555",
			"session-id-abc123",
			"session_id_abc123",
			"account-abc123",
			"account_id_abc123",
			"org_abc123",
			"project_abc123",
			"user_abc123",
		},
	}
	payload["body_omitted_reason"] = "high_risk_body_not_retained"
	payload["digest_omitted_reason"] = "raw_body_digest_forbidden_by_policy"

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	_, err = svc.EvaluateIntent(body)
	require.Error(t, err)
}

func baseControlPlaneIntentPayload() map[string]any {
	return map[string]any{
		"method":                  "GET",
		"path_template":           "/api/claude_cli/bootstrap",
		"normalized_query":        map[string]string{"entrypoint": "sdk-cli"},
		"query_ref":               map[string]any{"key_id": "local_guard_v1", "scope": "control_plane_query", "version": 1, "value": "hmac-sha256:" + strings.Repeat("a", 64)},
		"query_omitted_reason":    nil,
		"classification":          "bootstrap_settings_or_feature_flag_stubbed",
		"policy_version":          1,
		"strategy_version":        1,
		"response_schema_version": 1,
		"routing_intent":          "local_stub_or_suppress",
		"body_length_bucket":      "empty",
		"schema_summary":          map[string]any{"content_kind": "none", "top_level_type": "none"},
		"body_omitted_reason":     "not_applicable",
		"digest_omitted_reason":   "not_applicable",
		"redaction_proof": map[string]any{
			"sensitive_scan":            "clean",
			"path_identifiers_redacted": false,
			"raw_query_persisted":       false,
			"body_persisted":            false,
			"raw_body_digest_persisted": false,
		},
	}
}

func TestControlPlaneIntentServiceEvaluateIntentSuppressesLegacyTelemetry(t *testing.T) {
	svc := NewControlPlaneIntentService()
	payload := map[string]any{
		"method":                  "POST",
		"path_template":           "/api/event_logging/batch",
		"normalized_query":        map[string]string{},
		"query_ref":               nil,
		"query_omitted_reason":    "no_query",
		"classification":          "telemetry_or_eval_suppressed",
		"policy_version":          1,
		"strategy_version":        1,
		"response_schema_version": 1,
		"routing_intent":          "local_stub_or_suppress",
		"body_length_bucket":      "256_1023_bytes",
		"schema_summary":          map[string]any{"content_kind": "json", "top_level_type": "object", "top_level_keys": []any{"events"}},
		"body_omitted_reason":     "high_risk_body_not_retained",
		"digest_omitted_reason":   "raw_body_digest_forbidden_by_policy",
		"redaction_proof": map[string]any{
			"sensitive_scan":            "clean",
			"path_identifiers_redacted": false,
			"raw_query_persisted":       false,
			"body_persisted":            false,
			"raw_body_digest_persisted": false,
		},
	}

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	decision, err := svc.EvaluateIntent(body)
	require.NoError(t, err)
	require.Equal(t, "suppress_204", decision.Decision)
	require.Equal(t, 204, decision.Status)
}

func TestControlPlaneIntentServiceRejectsMatrixClassificationMismatch(t *testing.T) {
	svc := NewControlPlaneIntentService()
	payload := baseControlPlaneIntentPayload()
	payload["classification"] = "account_settings_sensitive"

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	_, err = svc.EvaluateIntent(body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "classification")
}
