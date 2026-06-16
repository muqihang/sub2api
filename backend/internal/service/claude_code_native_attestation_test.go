package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestClaudeCodeNativeAttestationAcceptsGuardSignedMessagesWithoutServerShape(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "native-attestation-test-secret")
	now := time.Unix(2000, 0)
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":[{"type":"text","text":"prompt must not be audited"}]}],"tools":[{"name":"Bash","input_schema":{"type":"object"}}]}`)
	headers := signedNativeHeadersForTest(t, body, "/v1/messages?beta=true", now, nil)

	svc := NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
		WithClaudeCodeNativeAttestationReplayCache(NewClaudeCodeNativeNonceReplayCache(time.Minute, func() time.Time { return now })),
	)
	summary, err := svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages?beta=true", headers, body)
	require.NoError(t, err)

	require.Equal(t, ClaudeCodeNativeClientType, summary.ClientType)
	require.True(t, summary.NativeAttested)
	require.False(t, summary.ServerFilledShape)
	require.Equal(t, "/v1/messages", summary.InboundRoute)
	require.Equal(t, "/v1/messages?beta=true", summary.CCGatewayRoute)
	require.Equal(t, "real_claude_code_native_takeover_v1", summary.ShapeHealthcheckProfile)
	require.Equal(t, "truthful_pass_through", summary.ToolSearchMode)
	require.True(t, summary.ToolReferencePresent)
	require.Equal(t, "hmac-sha256:"+stringOf('a', 64), summary.LocalSessionRef)
	require.NotContains(t, string(mustNativeJSON(t, summary)), "prompt must not be audited")
}

func TestClaudeCodeNativeAttestationRequiresRuntimeRouteCatalogAndBodyBindings(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "native-attestation-test-secret")
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_RUNTIME_HASHES", "sha256:"+stringOf('1', 64))
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_OVERLAY_HASHES", "sha256:"+stringOf('2', 64))
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_CATALOG_HASHES", "sha256:"+stringOf('3', 64))
	now := time.Unix(2100, 0)
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}`)
	headers := signedNativeHeadersForTest(t, body, "/v1/messages?beta=true", now, map[string]any{
		"route":            "claude_code_native",
		"model_id":         "claude-sonnet-4-6",
		"provider_owner":   "zhumeng_managed",
		"credential_scope": "formal_pool",
		"gateway_location": "cloud",
		"runtime_hash":     "sha256:" + stringOf('1', 64),
		"overlay_hash":     "sha256:" + stringOf('2', 64),
		"catalog_hash":     "sha256:" + stringOf('3', 64),
		"session_ref":      "hmac-sha256:" + stringOf('a', 64),
		"body_shape_hash":  claudeCodeNativeBodyShapeHash(body),
	})

	svc := NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
		WithClaudeCodeNativeAttestationReplayCache(NewClaudeCodeNativeNonceReplayCache(time.Minute, func() time.Time { return now })),
	)
	summary, err := svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages?beta=true", headers, body)
	require.NoError(t, err)
	require.Equal(t, ClaudeCodeNativeClientType, summary.ClientType)
}

func TestClaudeCodeNativeAttestationRejectsUnknownRuntimeCatalogHashAndBridgeModels(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "native-attestation-test-secret")
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_RUNTIME_HASHES", "sha256:"+stringOf('1', 64))
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_OVERLAY_HASHES", "sha256:"+stringOf('2', 64))
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_CATALOG_HASHES", "sha256:"+stringOf('3', 64))
	now := time.Unix(2150, 0)
	svc := NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
		WithClaudeCodeNativeAttestationReplayCache(NewClaudeCodeNativeNonceReplayCache(time.Minute, func() time.Time { return now })),
	)
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}`)
	headers := signedNativeHeadersForTest(t, body, "/v1/messages", now, map[string]any{
		"runtime_hash": "sha256:" + stringOf('9', 64),
		"nonce":        "unknown-runtime-hash",
	})
	_, err := svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages", headers, body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "runtime hash")

	bridgeBody := []byte(`{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"hello"}]}`)
	bridgeHeaders := signedNativeHeadersForTest(t, bridgeBody, "/v1/messages", now, map[string]any{"nonce": "bridge-model"})
	_, err = svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages", bridgeHeaders, bridgeBody)
	require.Error(t, err)
	require.Contains(t, err.Error(), "formal-pool")
}

func TestClaudeCodeNativeAttestationRejectsInvalidConfiguredHashAllowlist(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "native-attestation-test-secret")
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_RUNTIME_HASHES", "sha256:"+stringOf('0', 64))
	now := time.Unix(2160, 0)
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}`)
	headers := signedNativeHeadersForTest(t, body, "/v1/messages", now, map[string]any{"nonce": "invalid-runtime-allowlist"})

	svc := NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
		WithClaudeCodeNativeAttestationReplayCache(NewClaudeCodeNativeNonceReplayCache(time.Minute, func() time.Time { return now })),
	)
	_, err := svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages", headers, body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "runtime hash allowlist")
}

func TestClaudeCodeNativeAttestationAcceptsCountTokensRoute(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "native-attestation-test-secret")
	now := time.Unix(2400, 0)
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"count me"}]}`)
	headers := signedNativeHeadersForTest(t, body, "/v1/messages/count_tokens", now, nil)

	svc := NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
		WithClaudeCodeNativeAttestationReplayCache(NewClaudeCodeNativeNonceReplayCache(time.Minute, func() time.Time { return now })),
	)
	summary, err := svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages/count_tokens", headers, body)
	require.NoError(t, err)
	require.Equal(t, "/v1/messages/count_tokens", summary.InboundRoute)
	require.Equal(t, "/v1/messages/count_tokens?beta=true", summary.CCGatewayRoute)
	require.False(t, summary.ServerFilledShape)
}

func TestClaudeCodeNativeAttestationRejectsExtraPayloadFields(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "native-attestation-test-secret")
	now := time.Unix(2500, 0)
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}`)
	headers := signedNativeHeadersForTest(t, body, "/v1/messages", now, map[string]any{"raw_prompt": "must-not-fit"})

	svc := NewClaudeCodeNativeAttestationService(WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }))
	_, err := svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages", headers, body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "strict allowlist")
}

func TestClaudeCodeNativeAttestationRequiresExplicitServerSecret(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "")
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_KEYS_JSON", "")
	t.Setenv("SUB2API_CONTROL_PLANE_ATTESTATION_SECRET", "")
	issued := time.Unix(2300, 0)
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}`)
	headers := signedNativeHeadersForTestWithSecret(t, body, "/v1/messages", issued, nil, "sub2api-claude-code-native-attestation-dev-key")

	svc := NewClaudeCodeNativeAttestationService(WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return issued }))
	_, err := svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages", headers, body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "explicit")
}

func TestClaudeCodeNativeAttestationRejectsSpoofedCompatAndUntrustedBeta(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "native-attestation-test-secret")
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}`)
	svc := NewClaudeCodeNativeAttestationService()

	spoofed := http.Header{}
	spoofed.Set(ClaudeCodeNativeClientTypeHeader, ClaudeCodeNativeClientType)
	spoofed.Set(ClaudeCodeNativeGuardAttestedHeader, "true")
	_, err := svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages", spoofed, body)
	require.Error(t, err)
	require.True(t, IsClaudeCodeNativeMarkerPresent(spoofed))
	require.Contains(t, err.Error(), "native attestation")

	untrusted := http.Header{}
	untrusted.Set(ClaudeCodeNativeClientTypeHeader, ClaudeCodeUntrustedBetaClientType)
	untrusted.Set("anthropic-beta", "claude-code-20250219")
	_, err = svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages?beta=true", untrusted, body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "untrusted")

	compatHeaders := http.Header{}
	compatHeaders.Set(ClaudeCodeNativeClientTypeHeader, ClaudeCodeNativeClientType)
	compatHeaders.Set(ClaudeCodeNativeGuardAttestedHeader, "true")
	compatHeaders.Set(ClaudeCodeNativeAttestationHeader, "forged")
	compatHeaders.Set(ClaudeCodeNativeSignatureHeader, "forged")
	compatHeaders.Set("content-type", "application/json")
	sanitized := SanitizeAnthropicCompatInboundHeaders(compatHeaders)
	require.Empty(t, sanitized.Get(ClaudeCodeNativeClientTypeHeader))
	require.Empty(t, sanitized.Get(ClaudeCodeNativeAttestationHeader))
	require.Equal(t, "application/json", sanitized.Get("content-type"))
}

func TestClaudeCodeNativeAttestationFreshnessAndReplayFailClosed(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "native-attestation-test-secret")
	issued := time.Unix(2000, 0)
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}`)
	headers := signedNativeHeadersForTest(t, body, "/v1/messages", issued, map[string]any{"nonce": "nonce-replay"})
	cache := NewClaudeCodeNativeNonceReplayCache(time.Minute, func() time.Time { return issued })
	svc := NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return issued }),
		WithClaudeCodeNativeAttestationReplayCache(cache),
	)
	_, err := svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages", headers, body)
	require.NoError(t, err)
	_, err = svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages", headers, body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "replayed")

	expiredSvc := NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return issued.Add(5 * time.Minute) }),
		WithClaudeCodeNativeAttestationReplayCache(NewClaudeCodeNativeNonceReplayCache(time.Minute, func() time.Time { return issued.Add(5 * time.Minute) })),
	)
	_, err = expiredSvc.VerifyMessagesRequest(http.MethodPost, "/v1/messages", headers, body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "expired")
}

func TestClaudeCodeNativeAttestationDefaultReplayCacheIsShared(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "native-attestation-test-secret")
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_NONCE_TTL_SECONDS", "120")
	issued := time.Unix(2200, 0)
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}`)
	headers := signedNativeHeadersForTest(t, body, "/v1/messages", issued, map[string]any{"nonce": "shared-replay-nonce"})

	first := NewClaudeCodeNativeAttestationService(WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return issued }))
	_, err := first.VerifyMessagesRequest(http.MethodPost, "/v1/messages", headers, body)
	require.NoError(t, err)

	second := NewClaudeCodeNativeAttestationService(WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return issued }))
	_, err = second.VerifyMessagesRequest(http.MethodPost, "/v1/messages", headers, body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "replayed")
}

func TestClaudeCodeNativeAuditHeadersAreMutuallyExclusiveWithCompat(t *testing.T) {
	native := ClaudeCodeNativeAuditSummary{
		ClientType:                 ClaudeCodeNativeClientType,
		NativeAttested:             true,
		GuardVersion:               "guard_v1",
		ClaudeCodeVersion:          "2.1.x",
		LocalSessionRef:            "hmac-sha256:" + stringOf('b', 64),
		InboundRoute:               "/v1/messages",
		CCGatewayRoute:             "/v1/messages?beta=true",
		NetwatchRequired:           true,
		ShapeHealthcheckProfile:    "real_claude_code_native_takeover_v1",
		ToolSearchMode:             "truthful_pass_through",
		ToolReferencePresent:       true,
		DeferLoadingPresent:        true,
		EagerInputStreamingPresent: true,
	}
	headers := http.Header{}
	require.NoError(t, ApplyClaudeCodeNativeAuditHeaders(headers, native))
	require.Equal(t, ClaudeCodeNativeClientType, getHeaderRaw(headers, ClaudeCodeNativeClientTypeHeader))
	require.Equal(t, "true", getHeaderRaw(headers, ClaudeCodeNativeGuardAttestedHeader))
	require.Equal(t, "false", getHeaderRaw(headers, ClaudeCodeNativeServerFilledShapeHeader))
	require.Empty(t, getHeaderRaw(headers, AnthropicCompatClientTypeHeader))

	ctx := WithClaudeCodeNativeAuditSummary(context.Background(), native)
	decision := AnthropicCompatIngressDecision{InboundRoute: AnthropicCompatInboundMessages, CCGatewayRoute: AnthropicCompatCCGatewayMessages, ClientType: AnthropicCompatClientType}
	ctx = WithAnthropicCompatAuditSummary(ctx, NewAnthropicCompatAuditSummary(decision))
	require.Error(t, ApplyClaudeCodePathAuditHeaders(http.Header{}, ctx))
}

func TestClaudeCodeNativeDirectedHealthcheckBoundaryDoesNotPromoteFromNativeTakeover(t *testing.T) {
	decision := EvaluateClaudeCodeNativeDirectedHealthcheckBoundary(ClaudeCodeNativeDirectedHealthcheckEvidence{
		Profile:           "real_claude_code_native_takeover_v1",
		TemporaryKey:      false,
		SingleAccountPin:  false,
		CCGatewaySeen:     true,
		RawCapturePresent: true,
		AccountRef:        "hmac-sha256:" + stringOf('c', 64),
		EgressBucket:      "bucket-a",
		VerifierSummary:   "passed",
		Fresh:             true,
		StatusCode:        200,
	})
	require.False(t, decision.HealthcheckPassed)
	require.False(t, decision.CanPromoteProduction)
	require.Contains(t, decision.Reason, "temporary")

	decision = EvaluateClaudeCodeNativeDirectedHealthcheckBoundary(ClaudeCodeNativeDirectedHealthcheckEvidence{
		Profile:           "real_claude_code_native_toolsearch_v1",
		TemporaryKey:      true,
		SingleAccountPin:  true,
		CCGatewaySeen:     true,
		RawCapturePresent: true,
		AccountRef:        "hmac-sha256:" + stringOf('c', 64),
		EgressBucket:      "bucket-a",
		VerifierSummary:   "passed",
		Fresh:             true,
		StatusCode:        200,
	})
	require.True(t, decision.HealthcheckPassed)
	require.False(t, decision.CanPromoteProduction)
	require.Equal(t, "healthcheck_passed", decision.NextState)
}

func signedNativeHeadersForTest(t *testing.T, body []byte, requestURI string, now time.Time, overrides map[string]any) http.Header {
	t.Helper()
	return signedNativeHeadersForTestWithSecret(t, body, requestURI, now, overrides, "native-attestation-test-secret")
}

func signedNativeHeadersForTestWithSecret(t *testing.T, body []byte, requestURI string, now time.Time, overrides map[string]any, secret string) http.Header {
	t.Helper()
	payload := map[string]any{
		"key_id":                    "guard_v1",
		"scope":                     "claude_code_native_takeover",
		"version":                   1,
		"issued_at":                 now.Unix(),
		"nonce":                     "nonce-001",
		"method":                    http.MethodPost,
		"request_uri":               requestURI,
		"client_type":               ClaudeCodeNativeClientType,
		"guard_attested":            true,
		"guard_version":             "guard_v1",
		"claude_code_version":       "2.1.x",
		"local_session_ref":         "hmac-sha256:" + stringOf('a', 64),
		"netwatch_required":         true,
		"shape_healthcheck_profile": "real_claude_code_native_takeover_v1",
		"route":                     ClaudeCodeNativeRoute,
		"model_id":                  gjson.GetBytes(body, "model").String(),
		"provider_owner":            ClaudeCodeNativeProviderOwner,
		"credential_scope":          ClaudeCodeNativeCredentialScope,
		"gateway_location":          ClaudeCodeNativeGatewayLocation,
		"runtime_hash":              "sha256:" + stringOf('1', 64),
		"overlay_hash":              "sha256:" + stringOf('2', 64),
		"catalog_hash":              "sha256:" + stringOf('3', 64),
		"session_ref":               "hmac-sha256:" + stringOf('a', 64),
		"body_shape_hash":           claudeCodeNativeBodyShapeHash(body),
	}
	for key, value := range overrides {
		payload[key] = value
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	bodyDigest := sha256.Sum256(body)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(encoded))
	_, _ = mac.Write([]byte("\n"))
	_, _ = mac.Write([]byte(http.MethodPost))
	_, _ = mac.Write([]byte("\n"))
	_, _ = mac.Write([]byte(requestURI))
	_, _ = mac.Write([]byte("\n"))
	_, _ = mac.Write([]byte(hex.EncodeToString(bodyDigest[:])))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	headers := http.Header{}
	headers.Set(ClaudeCodeNativeClientTypeHeader, ClaudeCodeNativeClientType)
	headers.Set(ClaudeCodeNativeGuardAttestedHeader, "true")
	headers.Set(ClaudeCodeNativeGuardVersionHeader, "guard_v1")
	headers.Set(ClaudeCodeNativeClaudeCodeVersionHeader, "2.1.x")
	headers.Set(ClaudeCodeNativeLocalSessionRefHeader, "hmac-sha256:"+stringOf('a', 64))
	headers.Set(ClaudeCodeNativeNetwatchRequiredHeader, "true")
	headers.Set(ClaudeCodeNativeAttestationHeader, encoded)
	headers.Set(ClaudeCodeNativeSignatureHeader, signature)
	return headers
}

func stringOf(ch byte, n int) string {
	return string(bytes.Repeat([]byte{ch}, n))
}

func mustNativeJSON(t *testing.T, v any) []byte {
	t.Helper()
	out, err := json.Marshal(v)
	require.NoError(t, err)
	return out
}
