package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestControlPlaneAttestationServiceValidAccepted(t *testing.T) {
	setControlPlaneAttestationEnv(t)
	now := time.Unix(1000, 0)
	svc := NewControlPlaneAttestationService(
		WithControlPlaneAttestationNowFunc(func() time.Time { return now }),
		WithControlPlaneAttestationReplayCache(NewControlPlaneNonceReplayCache(120*time.Second, func() time.Time { return now })),
	)
	body := mustJSONControlPlaneIntentBody(t)
	headers := buildControlPlaneAttestationHeaders(t, body, attestationBuildOptions{now: now.Unix(), nonce: "nonce-001", keyID: "guard_v2"})
	headers.Set("x-sub2api-intent-auth", "sub2api-intent-token")

	payload, err := svc.VerifyRequest(body, headers)
	require.NoError(t, err)
	require.Equal(t, "guard_v2", payload.KeyID)
	require.Equal(t, "/api/claude_cli/bootstrap", payload.PathTemplate)
	require.Equal(t, "session_budget_session", payload.SessionRef.Scope)
}

func TestControlPlaneAttestationServiceMissingRejected(t *testing.T) {
	setControlPlaneAttestationEnv(t)
	now := time.Unix(1000, 0)
	svc := NewControlPlaneAttestationService(WithControlPlaneAttestationNowFunc(func() time.Time { return now }))
	_, err := svc.VerifyRequest(mustJSONControlPlaneIntentBody(t), http.Header{})
	require.Error(t, err)
}

func TestControlPlaneAttestationServiceWrongKeyAndExpiredRejected(t *testing.T) {
	setControlPlaneAttestationEnv(t)
	now := time.Unix(2000, 0)
	svc := NewControlPlaneAttestationService(
		WithControlPlaneAttestationNowFunc(func() time.Time { return now }),
		WithControlPlaneAttestationReplayCache(NewControlPlaneNonceReplayCache(120*time.Second, func() time.Time { return now })),
	)
	body := mustJSONControlPlaneIntentBody(t)

	headers := buildControlPlaneAttestationHeaders(t, body, attestationBuildOptions{now: 2000, nonce: "nonce-002", keyID: "missing"})
	_, err := svc.VerifyRequest(body, headers)
	require.Error(t, err)

	headers = buildControlPlaneAttestationHeaders(t, body, attestationBuildOptions{now: 1500, nonce: "nonce-003", keyID: "guard_v2"})
	_, err = svc.VerifyRequest(body, headers)
	require.Error(t, err)
}

func TestControlPlaneAttestationServiceClockSkewReplayAndRotation(t *testing.T) {
	setControlPlaneAttestationEnv(t)
	nowRef := nowPointer(time.Unix(3000, 0))
	cache := NewControlPlaneNonceReplayCache(5*time.Second, func() time.Time { return *nowRef })
	svc := NewControlPlaneAttestationService(
		WithControlPlaneAttestationNowFunc(func() time.Time { return *nowRef }),
		WithControlPlaneAttestationReplayCache(cache),
	)
	body := mustJSONControlPlaneIntentBody(t)

	headers := buildControlPlaneAttestationHeaders(t, body, attestationBuildOptions{now: 3040, nonce: "nonce-004", keyID: "guard_v2"})
	_, err := svc.VerifyRequest(body, headers)
	require.Error(t, err)

	headers = buildControlPlaneAttestationHeaders(t, body, attestationBuildOptions{now: 3000, nonce: "nonce-005", keyID: "guard_v1"})
	payload, err := svc.VerifyRequest(body, headers)
	require.NoError(t, err)
	require.Equal(t, "guard_v1", payload.KeyID)
	_, err = svc.VerifyRequest(body, headers)
	require.Error(t, err)

	*nowRef = time.Unix(3007, 0)
	headers = buildControlPlaneAttestationHeaders(t, body, attestationBuildOptions{now: 3007, nonce: "nonce-005", keyID: "guard_v1"})
	_, err = svc.VerifyRequest(body, headers)
	require.NoError(t, err)
}

func TestControlPlaneAttestationServiceBindsDigestOmission(t *testing.T) {
	setControlPlaneAttestationEnv(t)
	now := time.Unix(4000, 0)
	svc := NewControlPlaneAttestationService(
		WithControlPlaneAttestationNowFunc(func() time.Time { return now }),
		WithControlPlaneAttestationReplayCache(NewControlPlaneNonceReplayCache(120*time.Second, func() time.Time { return now })),
	)
	body := mustJSONControlPlaneIntentBodyWithOverrides(t, map[string]any{
		"method":                "POST",
		"path_template":         "/api/event_logging/v2/batch",
		"normalized_query":      map[string]string{},
		"query_ref":             nil,
		"query_omitted_reason":  "no_query",
		"classification":        "telemetry_or_eval_suppressed",
		"body_length_bucket":    "1_255_bytes",
		"body_omitted_reason":   "high_risk_body_not_retained",
		"digest_omitted_reason": "raw_body_digest_forbidden_by_policy",
		"schema_summary":        map[string]any{"content_kind": "json", "top_level_type": "object", "top_level_keys": []any{"events"}},
	})
	headers := buildControlPlaneAttestationHeaders(t, body, attestationBuildOptions{now: 4000, nonce: "nonce-006", keyID: "guard_v2"})

	var payload map[string]any
	require.NoError(t, json.Unmarshal(body, &payload))
	payload["digest_omitted_reason"] = "not_applicable"
	tamperedBody, err := json.Marshal(payload)
	require.NoError(t, err)

	_, err = svc.VerifyRequest(tamperedBody, headers)
	require.Error(t, err)

	payload = mustUnmarshalMap(t, body)
	payload["normalized_query"] = map[string]string{"entrypoint": "tampered"}
	tamperedBody, err = json.Marshal(payload)
	require.NoError(t, err)
	_, err = svc.VerifyRequest(tamperedBody, headers)
	require.Error(t, err)

	payload = mustUnmarshalMap(t, body)
	payload["schema_summary"] = map[string]any{"content_kind": "json", "top_level_type": "array"}
	tamperedBody, err = json.Marshal(payload)
	require.NoError(t, err)
	_, err = svc.VerifyRequest(tamperedBody, headers)
	require.Error(t, err)
}

func TestControlPlaneAttestationServiceDefaultReplayCacheUsesConfiguredTTL(t *testing.T) {
	setControlPlaneAttestationEnv(t)
	t.Setenv("SUB2API_CONTROL_PLANE_ATTESTATION_NONCE_TTL_SECONDS", "1")
	nowRef := nowPointer(time.Unix(5000, 0))
	resetSharedControlPlaneReplayCacheForTest()
	svc := NewControlPlaneAttestationService(
		WithControlPlaneAttestationNowFunc(func() time.Time { return *nowRef }),
	)
	body := mustJSONControlPlaneIntentBody(t)
	headers := buildControlPlaneAttestationHeaders(t, body, attestationBuildOptions{now: 5000, nonce: "nonce-ttl-001", keyID: "guard_v2"})

	_, err := svc.VerifyRequest(body, headers)
	require.NoError(t, err)
	_, err = svc.VerifyRequest(body, headers)
	require.Error(t, err)

	*nowRef = time.Unix(5002, 0)
	headers = buildControlPlaneAttestationHeaders(t, body, attestationBuildOptions{now: 5002, nonce: "nonce-ttl-001", keyID: "guard_v2"})
	_, err = svc.VerifyRequest(body, headers)
	require.NoError(t, err)
}

func TestControlPlaneAttestationServiceRejectsExtraAttestationFields(t *testing.T) {
	setControlPlaneAttestationEnv(t)
	now := time.Unix(6000, 0)
	svc := NewControlPlaneAttestationService(
		WithControlPlaneAttestationNowFunc(func() time.Time { return now }),
		WithControlPlaneAttestationReplayCache(NewControlPlaneNonceReplayCache(120*time.Second, func() time.Time { return now })),
	)
	body := mustJSONControlPlaneIntentBody(t)
	headers := buildControlPlaneAttestationHeaders(t, body, attestationBuildOptions{now: 6000, nonce: "nonce-extra-001", keyID: "guard_v2"})

	encoded := headers.Get(controlPlaneAttestationHeader)
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	require.NoError(t, err)
	payload := mustUnmarshalMap(t, raw)
	payload["extra"] = "unexpected"
	encodedTamperedBytes, err := json.Marshal(payload)
	require.NoError(t, err)
	encodedTampered := base64.RawURLEncoding.EncodeToString(encodedTamperedBytes)
	headers.Set(controlPlaneAttestationHeader, encodedTampered)
	headers.Set(controlPlaneAttestationSignatureHeader, signControlPlaneAttestation(encodedTampered, attestationSecretForKey("guard_v2")))

	_, err = svc.VerifyRequest(body, headers)
	require.Error(t, err)
}

type attestationBuildOptions struct {
	now   int64
	nonce string
	keyID string
}

func buildControlPlaneAttestationHeaders(t *testing.T, intentBody []byte, opts attestationBuildOptions) http.Header {
	t.Helper()
	var intent map[string]any
	require.NoError(t, json.Unmarshal(intentBody, &intent))
	payload := map[string]any{
		"key_id":                  opts.keyID,
		"scope":                   "control_plane_intent",
		"version":                 1,
		"issued_at":               opts.now,
		"nonce":                   opts.nonce,
		"method":                  intent["method"],
		"path_template":           intent["path_template"],
		"normalized_query":        intent["normalized_query"],
		"classification":          intent["classification"],
		"routing_intent":          intent["routing_intent"],
		"policy_version":          intent["policy_version"],
		"strategy_version":        intent["strategy_version"],
		"response_schema_version": intent["response_schema_version"],
		"body_length_bucket":      intent["body_length_bucket"],
		"body_omitted_reason":     intent["body_omitted_reason"],
		"digest_omitted_reason":   intent["digest_omitted_reason"],
		"schema_summary":          intent["schema_summary"],
		"query_ref":               intent["query_ref"],
		"query_omitted_reason":    intent["query_omitted_reason"],
		"session_ref": map[string]any{
			"key_id":  "session_budget_v1",
			"scope":   "session_budget_session",
			"version": 1,
			"value":   "hmac-sha256:" + strings.Repeat("b", 64),
		},
	}
	encodedJSON, err := json.Marshal(payload)
	require.NoError(t, err)
	attestation := base64.RawURLEncoding.EncodeToString(encodedJSON)
	secret := attestationSecretForKey(opts.keyID)
	sig := hmac.New(sha256.New, []byte(secret))
	_, err = sig.Write([]byte(attestation))
	require.NoError(t, err)
	headers := http.Header{}
	headers.Set("x-sub2api-control-plane-attestation", attestation)
	headers.Set("x-sub2api-control-plane-signature", fmt.Sprintf("hmac-sha256:%x", sig.Sum(nil)))
	return headers
}

func attestationSecretForKey(keyID string) string {
	switch keyID {
	case "guard_v1":
		return "secret-v1"
	case "guard_v2":
		return "secret-v2"
	default:
		return "secret-missing"
	}
}

func setControlPlaneAttestationEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SUB2API_CONTROL_PLANE_INTENT_TOKEN", "sub2api-intent-token")
	t.Setenv("SUB2API_CONTROL_PLANE_ATTESTATION_CURRENT_KEY_ID", "guard_v2")
	t.Setenv("SUB2API_CONTROL_PLANE_ATTESTATION_KEYS_JSON", `{"guard_v2":"secret-v2","guard_v1":"secret-v1"}`)
	t.Setenv("SUB2API_CONTROL_PLANE_ATTESTATION_SCOPE", "control_plane_intent")
	t.Setenv("SUB2API_CONTROL_PLANE_ATTESTATION_VERSION", "1")
	t.Setenv("SUB2API_CONTROL_PLANE_ATTESTATION_NONCE_TTL_SECONDS", "120")
	t.Setenv("SUB2API_CONTROL_PLANE_ATTESTATION_CLOCK_SKEW_SECONDS", "30")
}

func mustJSONControlPlaneIntentBody(t *testing.T) []byte {
	t.Helper()
	return mustJSONControlPlaneIntentBodyWithOverrides(t, nil)
}

func mustJSONControlPlaneIntentBodyWithOverrides(t *testing.T, overrides map[string]any) []byte {
	t.Helper()
	payload := map[string]any{
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
	for key, value := range overrides {
		payload[key] = value
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	return body
}

func nowPointer(v time.Time) *time.Time { return &v }

func mustUnmarshalMap(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var payload map[string]any
	require.NoError(t, json.Unmarshal(body, &payload))
	return payload
}

func resetSharedControlPlaneReplayCacheForTest() {
	controlPlaneReplayCacheMu.Lock()
	defer controlPlaneReplayCacheMu.Unlock()
	controlPlaneReplayCache = nil
	controlPlaneReplayCacheTTL = 0
}
