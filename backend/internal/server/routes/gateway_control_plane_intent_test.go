package routes

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGatewayRoutesControlPlaneIntentLoopbackOnly(t *testing.T) {
	t.Setenv("SUB2API_CONTROL_PLANE_INTENT_TOKEN", "sub2api-intent-token")
	setControlPlaneAttestationEnv(t)
	router := newGatewayRoutesTestRouter()
	body := mustJSONControlPlaneIntent(t, map[string]any{})
	attestationHeaders := buildRouteControlPlaneAttestationHeaders(t, body, routeAttestationOptions{now: time.Now().Unix(), nonce: "route-nonce-001", keyID: "guard_v2"})

	req := httptest.NewRequest(http.MethodPost, "/backend-api/anthropic/control-plane/intent", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:18080"
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-sub2api-intent-auth", "sub2api-intent-token")
	req.Header.Set("x-sub2api-control-plane-attestation", attestationHeaders.Get("x-sub2api-control-plane-attestation"))
	req.Header.Set("x-sub2api-control-plane-signature", attestationHeaders.Get("x-sub2api-control-plane-signature"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"decision":"stub_json"`)
	require.Contains(t, rec.Body.String(), `"path_template":"/api/claude_cli/bootstrap"`)

	req = httptest.NewRequest(http.MethodPost, "/backend-api/anthropic/control-plane/intent", bytes.NewReader(body))
	req.RemoteAddr = "203.0.113.10:18080"
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-sub2api-intent-auth", "sub2api-intent-token")
	req.Header.Set("x-sub2api-control-plane-attestation", attestationHeaders.Get("x-sub2api-control-plane-attestation"))
	req.Header.Set("x-sub2api-control-plane-signature", attestationHeaders.Get("x-sub2api-control-plane-signature"))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "control_plane_quarantine")
}

func TestGatewayRoutesControlPlaneIntentRejectsForgedHeadersAndPlainHashes(t *testing.T) {
	t.Setenv("SUB2API_CONTROL_PLANE_INTENT_TOKEN", "sub2api-intent-token")
	setControlPlaneAttestationEnv(t)
	router := newGatewayRoutesTestRouter()
	body := mustJSONControlPlaneIntent(t, map[string]any{})
	attestationHeaders := buildRouteControlPlaneAttestationHeaders(t, body, routeAttestationOptions{now: time.Now().Unix(), nonce: "route-nonce-002", keyID: "guard_v2"})

	req := httptest.NewRequest(http.MethodPost, "/backend-api/anthropic/control-plane/intent", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:18080"
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-sub2api-intent-auth", "sub2api-intent-token")
	req.Header.Set("x-sub2api-control-plane-attestation", attestationHeaders.Get("x-sub2api-control-plane-attestation"))
	req.Header.Set("x-sub2api-control-plane-signature", attestationHeaders.Get("x-sub2api-control-plane-signature"))
	req.Header.Set("X-Anthropic-Billing-Header", "cch=00000")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "invalid_request_error")

	body = mustJSONControlPlaneIntent(t, map[string]any{
		"query_ref": map[string]any{
			"key_id":  "local_guard_v1",
			"scope":   "control_plane_query",
			"version": 1,
			"value":   "sha256:" + strings.Repeat("a", 64),
		},
	})
	req = httptest.NewRequest(http.MethodPost, "/backend-api/anthropic/control-plane/intent", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:18080"
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-sub2api-intent-auth", "sub2api-intent-token")
	req.Header.Set("x-sub2api-control-plane-attestation", attestationHeaders.Get("x-sub2api-control-plane-attestation"))
	req.Header.Set("x-sub2api-control-plane-signature", attestationHeaders.Get("x-sub2api-control-plane-signature"))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "invalid_request_error")
}

func TestGatewayRoutesControlPlaneIntentRejectsMissingAuth(t *testing.T) {
	t.Setenv("SUB2API_CONTROL_PLANE_INTENT_TOKEN", "sub2api-intent-token")
	setControlPlaneAttestationEnv(t)
	router := newGatewayRoutesTestRouter()
	body := mustJSONControlPlaneIntent(t, map[string]any{})
	attestationHeaders := buildRouteControlPlaneAttestationHeaders(t, body, routeAttestationOptions{now: time.Now().Unix(), nonce: "route-nonce-003", keyID: "guard_v2"})

	req := httptest.NewRequest(http.MethodPost, "/backend-api/anthropic/control-plane/intent", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:18080"
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-sub2api-control-plane-attestation", attestationHeaders.Get("x-sub2api-control-plane-attestation"))
	req.Header.Set("x-sub2api-control-plane-signature", attestationHeaders.Get("x-sub2api-control-plane-signature"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, rec.Body.String(), "authentication_error")
}

func TestGatewayRoutesControlPlaneIntentRejectsMissingAndInvalidAttestation(t *testing.T) {
	t.Setenv("SUB2API_CONTROL_PLANE_INTENT_TOKEN", "sub2api-intent-token")
	setControlPlaneAttestationEnv(t)
	router := newGatewayRoutesTestRouter()
	body := mustJSONControlPlaneIntent(t, map[string]any{})

	req := httptest.NewRequest(http.MethodPost, "/backend-api/anthropic/control-plane/intent", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:18080"
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-sub2api-intent-auth", "sub2api-intent-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "control_plane_quarantine")

	headers := buildRouteControlPlaneAttestationHeaders(t, body, routeAttestationOptions{now: time.Now().Unix(), nonce: "route-nonce-004", keyID: "guard_v2"})
	req = httptest.NewRequest(http.MethodPost, "/backend-api/anthropic/control-plane/intent", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:18080"
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-sub2api-intent-auth", "sub2api-intent-token")
	req.Header.Set("x-sub2api-control-plane-attestation", headers.Get("x-sub2api-control-plane-attestation"))
	req.Header.Set("x-sub2api-control-plane-signature", "hmac-sha256:"+strings.Repeat("0", 64))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "control_plane_quarantine")
}

func TestGatewayRoutesControlPlaneIntentRejectsReplay(t *testing.T) {
	t.Setenv("SUB2API_CONTROL_PLANE_INTENT_TOKEN", "sub2api-intent-token")
	setControlPlaneAttestationEnv(t)
	router := newGatewayRoutesTestRouter()
	body := mustJSONControlPlaneIntent(t, map[string]any{})
	headers := buildRouteControlPlaneAttestationHeaders(t, body, routeAttestationOptions{now: time.Now().Unix(), nonce: "route-nonce-005", keyID: "guard_v2"})

	req := httptest.NewRequest(http.MethodPost, "/backend-api/anthropic/control-plane/intent", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:18080"
	req.Header = headers.Clone()
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-sub2api-intent-auth", "sub2api-intent-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	req = httptest.NewRequest(http.MethodPost, "/backend-api/anthropic/control-plane/intent", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:18080"
	req.Header = headers.Clone()
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-sub2api-intent-auth", "sub2api-intent-token")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "control_plane_quarantine")
}

func mustJSONControlPlaneIntent(t *testing.T, overrides map[string]any) []byte {
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

type routeAttestationOptions struct {
	now   int64
	nonce string
	keyID string
}

func buildRouteControlPlaneAttestationHeaders(t *testing.T, intentBody []byte, opts routeAttestationOptions) http.Header {
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
	secret := routeAttestationSecretForKey(opts.keyID)
	mac := hmac.New(sha256.New, []byte(secret))
	_, err = mac.Write([]byte(attestation))
	require.NoError(t, err)
	headers := http.Header{}
	headers.Set("x-sub2api-control-plane-attestation", attestation)
	headers.Set("x-sub2api-control-plane-signature", fmt.Sprintf("hmac-sha256:%x", mac.Sum(nil)))
	return headers
}

func routeAttestationSecretForKey(keyID string) string {
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
	t.Setenv("SUB2API_CONTROL_PLANE_ATTESTATION_CURRENT_KEY_ID", "guard_v2")
	t.Setenv("SUB2API_CONTROL_PLANE_ATTESTATION_KEYS_JSON", `{"guard_v2":"secret-v2","guard_v1":"secret-v1"}`)
	t.Setenv("SUB2API_CONTROL_PLANE_ATTESTATION_SCOPE", "control_plane_intent")
	t.Setenv("SUB2API_CONTROL_PLANE_ATTESTATION_VERSION", "1")
	t.Setenv("SUB2API_CONTROL_PLANE_ATTESTATION_NONCE_TTL_SECONDS", "120")
	t.Setenv("SUB2API_CONTROL_PLANE_ATTESTATION_CLOCK_SKEW_SECONDS", "30")
}
