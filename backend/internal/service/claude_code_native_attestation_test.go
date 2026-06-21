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

type staticClaudeCodeNativeCatalogAdmissionResolver struct {
	decisions map[string]claudeCodeNativeCatalogAdmissionDecision
}

func (r staticClaudeCodeNativeCatalogAdmissionResolver) ResolveClaudeCodeNativeCatalogAdmission(model string) (claudeCodeNativeCatalogAdmissionDecision, error) {
	if decision, ok := r.decisions[model]; ok {
		return decision, nil
	}
	return claudeCodeNativeCatalogAdmissionDecision{}, nil
}

func testClaudeCodeNativeFormalPoolResolver(models ...string) claudeCodeNativeCatalogAdmissionResolver {
	decisions := make(map[string]claudeCodeNativeCatalogAdmissionDecision, len(models))
	for _, model := range models {
		decisions[model] = claudeCodeNativeCatalogAdmissionDecision{
			ModelID:         model,
			Route:           ClaudeCodeNativeRoute,
			ProviderOwner:   ClaudeCodeNativeProviderOwner,
			CredentialScope: ClaudeCodeNativeCredentialScope,
			GatewayLocation: ClaudeCodeNativeGatewayLocation,
			CatalogFresh:    true,
		}
	}
	return staticClaudeCodeNativeCatalogAdmissionResolver{decisions: decisions}
}

func TestClaudeCodeNativeAttestationDefaultCatalogAdmissionFailsClosed(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "native-attestation-test-secret")
	now := time.Unix(1990, 0)
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}`)
	headers := signedNativeHeadersForTest(t, body, "/v1/messages", now, map[string]any{"nonce": "default-catalog-fail-closed"})
	svc := NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
		WithClaudeCodeNativeAttestationReplayCache(NewClaudeCodeNativeNonceReplayCache(time.Minute, func() time.Time { return now })),
	)

	_, err := svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages", headers, body)

	require.Error(t, err)
	require.Contains(t, err.Error(), "catalog admission")
}

func TestClaudeCodeNativeAttestationAcceptsGuardSignedMessagesWithoutServerShape(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "native-attestation-test-secret")
	now := time.Unix(2000, 0)
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":[{"type":"text","text":"prompt must not be audited"}]}],"tools":[{"name":"Bash","input_schema":{"type":"object"}}]}`)
	headers := signedNativeHeadersForTest(t, body, "/v1/messages?beta=true", now, nil)

	svc := NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
		WithClaudeCodeNativeAttestationReplayCache(NewClaudeCodeNativeNonceReplayCache(time.Minute, func() time.Time { return now })),
		withClaudeCodeNativeCatalogAdmissionResolver(testClaudeCodeNativeFormalPoolResolver("claude-sonnet-4-6")),
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

func TestClaudeCodeNativeAuditSummaryIncludesOnlySafeRouteEvidence(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "native-attestation-test-secret")
	now := time.Unix(2050, 0)
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"prompt must stay out of audit"}]}`)
	headers := signedNativeHeadersForTest(t, body, "/v1/messages?beta=true", now, map[string]any{
		"nonce":        "safe-route-evidence",
		"runtime_hash": "sha256:" + stringOf('1', 64),
		"overlay_hash": "sha256:" + stringOf('2', 64),
		"catalog_hash": "sha256:" + stringOf('3', 64),
	})
	headers.Set(ClaudeCodeNativeCatalogVersionHeader, "catalog-v1")
	svc := NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
		WithClaudeCodeNativeAttestationReplayCache(NewClaudeCodeNativeNonceReplayCache(time.Minute, func() time.Time { return now })),
		withClaudeCodeNativeCatalogAdmissionResolver(testClaudeCodeNativeFormalPoolResolver("claude-sonnet-4-6")),
	)

	summary, err := svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages?beta=true", headers, body)
	require.NoError(t, err)
	raw := string(mustNativeJSON(t, summary))
	require.Contains(t, raw, "sha256:"+stringOf('1', 64))
	require.Contains(t, raw, "sha256:"+stringOf('2', 64))
	require.Contains(t, raw, "sha256:"+stringOf('3', 64))
	require.Contains(t, raw, "catalog-v1")
	require.NotContains(t, raw, "prompt must stay out of audit")
	require.NotContains(t, raw, "native-attestation-test-secret")
	require.NotContains(t, raw, getHeaderRaw(headers, ClaudeCodeNativeAttestationHeader))
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
		withClaudeCodeNativeCatalogAdmissionResolver(testClaudeCodeNativeFormalPoolResolver("claude-sonnet-4-6")),
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
	require.Contains(t, err.Error(), "catalog admission")
}

func TestClaudeCodeNativeEnvJSONCatalogRejectsNonClaudeNativeEntries(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "native-attestation-test-secret")
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ROUTE_CATALOG_JSON", `[{"model_id":"gpt-5.5","route":"claude_code_native","provider_owner":"zhumeng_managed","credential_scope":"formal_pool","gateway_location":"cloud","catalog_fresh":true},{"model_id":"claude-sonnet-4-6","route":"claude_code_native","provider_owner":"zhumeng_managed","credential_scope":"formal_pool","gateway_location":"cloud","catalog_fresh":true}]`)
	now := time.Unix(2152, 0)
	svc := NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
		WithClaudeCodeNativeAttestationReplayCache(NewClaudeCodeNativeNonceReplayCache(time.Minute, func() time.Time { return now })),
	)

	bridgeBody := []byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"must not be native"}]}`)
	bridgeHeaders := signedNativeHeadersForTest(t, bridgeBody, "/v1/messages", now, map[string]any{"nonce": "env-json-gpt"})
	_, err := svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages", bridgeHeaders, bridgeBody)
	require.Error(t, err)
	require.Contains(t, err.Error(), "catalog admission")

	claudeBody := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}`)
	claudeHeaders := signedNativeHeadersForTest(t, claudeBody, "/v1/messages", now, map[string]any{"nonce": "env-json-claude"})
	_, err = svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages", claudeHeaders, claudeBody)
	require.NoError(t, err)
}

func TestClaudeCodeNativeEnvFormalPoolModelsRejectNonClaudeEntries(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "native-attestation-test-secret")
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_FORMAL_POOL_MODELS", "gpt-5.5,deepseek-v4-pro,claude-sonnet-4-6")
	now := time.Unix(2155, 0)
	svc := NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
		WithClaudeCodeNativeAttestationReplayCache(NewClaudeCodeNativeNonceReplayCache(time.Minute, func() time.Time { return now })),
	)

	bridgeBody := []byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"must not be native"}]}`)
	bridgeHeaders := signedNativeHeadersForTest(t, bridgeBody, "/v1/messages", now, map[string]any{"nonce": "env-formal-pool-gpt"})
	_, err := svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages", bridgeHeaders, bridgeBody)
	require.Error(t, err)
	require.Contains(t, err.Error(), "catalog admission")

	claudeBody := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}`)
	claudeHeaders := signedNativeHeadersForTest(t, claudeBody, "/v1/messages", now, map[string]any{"nonce": "env-formal-pool-claude"})
	_, err = svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages", claudeHeaders, claudeBody)
	require.NoError(t, err)
}

func TestClaudeCodeNativeAttestationRequiresReplaySafetyContract(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "native-attestation-test-secret")
	now := time.Unix(2170, 0)
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}`)
	svc := NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
		WithClaudeCodeNativeAttestationReplayCache(NewClaudeCodeNativeNonceReplayCache(time.Minute, func() time.Time { return now })),
		withClaudeCodeNativeCatalogAdmissionResolver(testClaudeCodeNativeFormalPoolResolver("claude-sonnet-4-6")),
	)

	missing := signedNativeHeadersForTest(t, body, "/v1/messages", now, map[string]any{
		"nonce":                       "missing-replay-safety",
		"omit_replay_safety_contract": true,
	})
	_, err := svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages", missing, body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "replay safety")

	mismatch := signedNativeHeadersForTest(t, body, "/v1/messages", now, map[string]any{
		"nonce":                         "mismatch-replay-safety",
		"replay_safety_body_shape_hash": "sha256:" + stringOf('9', 64),
	})
	_, err = svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages", mismatch, body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "replay safety")

	valid := signedNativeHeadersForTest(t, body, "/v1/messages", now, map[string]any{"nonce": "valid-replay-safety"})
	summary, err := svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages", valid, body)
	require.NoError(t, err)
	require.True(t, summary.ReplaySafetyApplied)
	require.False(t, summary.ReplaySafetySanitized)
	require.Equal(t, "replay_safe_anthropic_transcript", summary.ReplaySafetyBoundary)
}

func TestClaudeCodeNativeAttestationRejectsInconsistentSanitizedReplaySafety(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "native-attestation-test-secret")
	now := time.Unix(2175, 0)
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}`)
	svc := NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
		WithClaudeCodeNativeAttestationReplayCache(NewClaudeCodeNativeNonceReplayCache(time.Minute, func() time.Time { return now })),
		withClaudeCodeNativeCatalogAdmissionResolver(testClaudeCodeNativeFormalPoolResolver("claude-sonnet-4-6")),
	)

	headers := signedNativeHeadersForTest(t, body, "/v1/messages", now, map[string]any{
		"nonce":                               "sanitized-count-missing",
		"replay_safety_sanitized":             true,
		"replay_safety_forbidden_paths_count": 0,
	})
	_, err := svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages", headers, body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "replay safety")
}

func TestClaudeCodeNativeAttestationRejectsNullReplaySafetyTypes(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "native-attestation-test-secret")
	now := time.Unix(2176, 0)
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}`)
	svc := NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
		WithClaudeCodeNativeAttestationReplayCache(NewClaudeCodeNativeNonceReplayCache(time.Minute, func() time.Time { return now })),
		withClaudeCodeNativeCatalogAdmissionResolver(testClaudeCodeNativeFormalPoolResolver("claude-sonnet-4-6")),
	)

	for _, tc := range []struct {
		name      string
		overrides map[string]any
	}{
		{
			name: "boundary null",
			overrides: map[string]any{
				"nonce":                  "null-replay-safety-boundary",
				"replay_safety_boundary": nil,
			},
		},
		{
			name: "applied null",
			overrides: map[string]any{
				"nonce":                 "null-replay-safety-applied",
				"replay_safety_applied": nil,
			},
		},
		{
			name: "sanitized null",
			overrides: map[string]any{
				"nonce":                   "null-replay-safety-sanitized",
				"replay_safety_sanitized": nil,
			},
		},
		{
			name: "forbidden count null",
			overrides: map[string]any{
				"nonce":                               "null-replay-safety-forbidden-count",
				"replay_safety_forbidden_paths_count": nil,
			},
		},
		{
			name: "body shape hash null",
			overrides: map[string]any{
				"nonce":                         "null-replay-safety-body-shape-hash",
				"replay_safety_body_shape_hash": nil,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			headers := signedNativeHeadersForTest(t, body, "/v1/messages", now, tc.overrides)
			_, err := svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages", headers, body)
			require.Error(t, err)
			require.Contains(t, err.Error(), "replay safety")
		})
	}
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
		withClaudeCodeNativeCatalogAdmissionResolver(testClaudeCodeNativeFormalPoolResolver("claude-sonnet-4-6")),
	)
	summary, err := svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages/count_tokens", headers, body)
	require.NoError(t, err)
	require.Equal(t, "/v1/messages/count_tokens", summary.InboundRoute)
	require.Equal(t, "/v1/messages/count_tokens?beta=true", summary.CCGatewayRoute)
	require.False(t, summary.ServerFilledShape)
}

func TestClaudeCodeNativeAttestationAcceptsCountTokensBetaRoute(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "native-attestation-test-secret")
	now := time.Unix(2160, 0)
	body := []byte(`{"model":"claude-haiku-4-5-20251001","messages":[{"role":"user","content":"probe"}],"tools":[{"name":"Read","input_schema":{"type":"object","properties":{}}}]}`)
	headers := signedNativeHeadersForTest(t, body, "/v1/messages/count_tokens?beta=true", now, map[string]any{"nonce": "count-tokens-beta-route"})
	svc := NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
		WithClaudeCodeNativeAttestationReplayCache(NewClaudeCodeNativeNonceReplayCache(time.Minute, func() time.Time { return now })),
		withClaudeCodeNativeCatalogAdmissionResolver(testClaudeCodeNativeFormalPoolResolver("claude-haiku-4-5-20251001")),
	)

	summary, err := svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages/count_tokens?beta=true", headers, body)
	require.NoError(t, err)
	require.Equal(t, "/v1/messages/count_tokens", summary.InboundRoute)
	require.Equal(t, "/v1/messages/count_tokens?beta=true", summary.CCGatewayRoute)
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
		withClaudeCodeNativeCatalogAdmissionResolver(testClaudeCodeNativeFormalPoolResolver("claude-sonnet-4-6")),
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

	first := NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return issued }),
		withClaudeCodeNativeCatalogAdmissionResolver(testClaudeCodeNativeFormalPoolResolver("claude-sonnet-4-6")),
	)
	_, err := first.VerifyMessagesRequest(http.MethodPost, "/v1/messages", headers, body)
	require.NoError(t, err)

	second := NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return issued }),
		withClaudeCodeNativeCatalogAdmissionResolver(testClaudeCodeNativeFormalPoolResolver("claude-sonnet-4-6")),
	)
	_, err = second.VerifyMessagesRequest(http.MethodPost, "/v1/messages", headers, body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "replayed")
}

func TestClaudeCodeRouteHintReplayCacheDoesNotResetNativeReplayCache(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "native-attestation-test-secret")
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_NONCE_TTL_SECONDS", "120")
	t.Setenv("SUB2API_CLAUDE_CODE_ROUTE_HINT_SECRET", "route-hint-key")
	t.Setenv("SUB2API_CLAUDE_CODE_ROUTE_HINT_NONCE_TTL_SECONDS", "60")
	issued := time.Unix(2600, 0)
	nativeBody := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}`)
	nativeHeaders := signedNativeHeadersForTest(t, nativeBody, "/v1/messages", issued, map[string]any{"nonce": "native-replay-isolated"})

	firstNative := NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return issued }),
		withClaudeCodeNativeCatalogAdmissionResolver(testClaudeCodeNativeFormalPoolResolver("claude-sonnet-4-6")),
	)
	_, err := firstNative.VerifyMessagesRequest(http.MethodPost, "/v1/messages", nativeHeaders, nativeBody)
	require.NoError(t, err)

	bridgeBody := []byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"bridge"}],"stream":true}`)
	bridgeHeaders := signedRouteHintHeadersForTest(t, bridgeBody, "/v1/messages", issued, map[string]any{"nonce": "route-hint-cache-isolated"})
	bridgeDecision := ClaudeCodeProviderRouteDecision{
		ModelID:                  "gpt-5.5",
		Provider:                 "openai",
		Route:                    "openai_bridge",
		ClientType:               "claude_code_bridge_openai",
		ProviderOwner:            "zhumeng_managed",
		CredentialScope:          "bridge_pool",
		GatewayLocation:          "cloud",
		CatalogFresh:             true,
		CatalogVersion:           "cp5-route-catalog",
		RuntimeHash:              "sha256:" + stringOf('1', 64),
		OverlayHash:              "sha256:" + stringOf('2', 64),
		CatalogHash:              "sha256:" + stringOf('3', 64),
		CapabilitiesVerified:     true,
		SupportsText:             true,
		SupportsTools:            true,
		SupportsStreaming:        true,
		SupportsUsage:            true,
		SupportsErrorPassthrough: true,
		PreferredProtocol:        "responses",
		OpenAIBaseURL:            "https://api.openai.com/v1",
	}
	routeHint := NewClaudeCodeNativeAttestationService(WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return issued }))
	_, err = routeHint.VerifyBridgeRouteHintRequest(http.MethodPost, "/v1/messages", bridgeHeaders, bridgeBody, bridgeDecision)
	require.NoError(t, err)

	secondNative := NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return issued }),
		withClaudeCodeNativeCatalogAdmissionResolver(testClaudeCodeNativeFormalPoolResolver("claude-sonnet-4-6")),
	)
	_, err = secondNative.VerifyMessagesRequest(http.MethodPost, "/v1/messages", nativeHeaders, nativeBody)
	require.Error(t, err)
	require.Contains(t, err.Error(), "replayed")
}

func TestCP6RouteHintAllowsClaudeCodeBetaMessagesRouteButRejectsOtherQuery(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_ROUTE_HINT_SECRET", "route-hint-key")
	t.Setenv("SUB2API_CLAUDE_CODE_ROUTE_HINT_CURRENT_KEY_ID", "route_hint_v1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "sk-deepseek-test-key")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	now := time.Unix(2690, 0)
	body := []byte(`{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"bridge beta route"}],"stream":true}`)
	decision := ClaudeCodeProviderRouteDecision{
		ModelID:                  "deepseek-v4-pro",
		Provider:                 "deepseek",
		Route:                    "deepseek_bridge",
		ClientType:               "claude_code_bridge_deepseek",
		ProviderOwner:            "zhumeng_managed",
		CredentialScope:          "bridge_pool",
		GatewayLocation:          "cloud",
		CatalogFresh:             true,
		CatalogVersion:           "cp5-route-catalog",
		RuntimeHash:              "sha256:" + stringOf('1', 64),
		OverlayHash:              "sha256:" + stringOf('2', 64),
		CatalogHash:              "sha256:" + stringOf('3', 64),
		PreferredProtocol:        "anthropic_messages",
		AnthropicBaseURL:         "http://127.0.0.1:9/anthropic",
		CapabilitiesVerified:     true,
		SupportsText:             true,
		SupportsTools:            true,
		SupportsStreaming:        true,
		SupportsUsage:            true,
		SupportsErrorPassthrough: true,
	}
	betaHeaders := signedRouteHintHeadersForTest(t, body, "/v1/messages?beta=true", now, map[string]any{
		"nonce":                "cp6-beta-route-ok",
		"model_id":             "deepseek-v4-pro",
		"body_model":           "deepseek-v4-pro",
		"route":                "deepseek_bridge",
		"client_type":          "claude_code_bridge_deepseek",
		"provider":             "deepseek",
		"live_request_allowed": false,
	})
	_, err := NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
	).VerifyBridgeRouteHintRequest(http.MethodPost, "/v1/messages?beta=true", betaHeaders, body, decision)
	require.NoError(t, err)

	otherQueryHeaders := signedRouteHintHeadersForTest(t, body, "/v1/messages?foo=bar", now, map[string]any{
		"nonce":                "cp6-other-query-reject",
		"model_id":             "deepseek-v4-pro",
		"body_model":           "deepseek-v4-pro",
		"route":                "deepseek_bridge",
		"client_type":          "claude_code_bridge_deepseek",
		"provider":             "deepseek",
		"live_request_allowed": false,
	})
	_, err = NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
	).VerifyBridgeRouteHintRequest(http.MethodPost, "/v1/messages?foo=bar", otherQueryHeaders, body, decision)
	require.Error(t, err)
	require.Contains(t, err.Error(), "route unsupported")
}

func TestCP6RouteHintAllowsBridgeCountTokensForLocalAuxiliaryEstimate(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_ROUTE_HINT_SECRET", "route-hint-key")
	t.Setenv("SUB2API_CLAUDE_CODE_ROUTE_HINT_CURRENT_KEY_ID", "route_hint_v1")
	now := time.Unix(2695, 0)
	body := []byte(`{"model":"claude-code-bridge-deepseek-v4-flash","messages":[{"role":"user","content":"bridge count tokens"}]}`)
	decision := ClaudeCodeProviderRouteDecision{
		ModelID:                  "claude-code-bridge-deepseek-v4-flash",
		UpstreamModel:            "deepseek-v4-flash",
		Provider:                 "deepseek",
		Route:                    "deepseek_bridge",
		ClientType:               "claude_code_bridge_deepseek",
		ProviderOwner:            "zhumeng_managed",
		CredentialScope:          "bridge_pool",
		GatewayLocation:          "cloud",
		CatalogFresh:             true,
		CatalogVersion:           "cp5-route-catalog",
		RuntimeHash:              "sha256:" + stringOf('1', 64),
		OverlayHash:              "sha256:" + stringOf('2', 64),
		CatalogHash:              "sha256:" + stringOf('3', 64),
		PreferredProtocol:        "anthropic_messages",
		AnthropicBaseURL:         "https://api.deepseek.com/anthropic",
		CapabilitiesVerified:     true,
		SupportsText:             true,
		SupportsTools:            true,
		SupportsStreaming:        true,
		SupportsUsage:            true,
		SupportsErrorPassthrough: true,
	}
	headers := signedRouteHintHeadersForTest(t, body, "/v1/messages/count_tokens?beta=true", now, map[string]any{
		"nonce":                "cp6-count-tokens-bridge-auxiliary",
		"model_id":             "claude-code-bridge-deepseek-v4-flash",
		"body_model":           "claude-code-bridge-deepseek-v4-flash",
		"route":                "deepseek_bridge",
		"client_type":          "claude_code_bridge_deepseek",
		"provider":             "deepseek",
		"live_request_allowed": true,
	})

	_, err := NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
	).VerifyBridgeRouteHintRequest(http.MethodPost, "/v1/messages/count_tokens?beta=true", headers, body, decision)

	require.NoError(t, err)
}

func TestCP6RouteHintLiveRequestAllowedRequiresServerBridgeLiveGate(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_ROUTE_HINT_SECRET", "route-hint-key")
	t.Setenv("SUB2API_CLAUDE_CODE_ROUTE_HINT_CURRENT_KEY_ID", "route_hint_v1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "")
	now := time.Unix(2700, 0)
	body := []byte(`{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"bridge live gate"}],"stream":true}`)
	decision := ClaudeCodeProviderRouteDecision{
		ModelID:                  "deepseek-v4-pro",
		Provider:                 "deepseek",
		Route:                    "deepseek_bridge",
		ClientType:               "claude_code_bridge_deepseek",
		ProviderOwner:            "zhumeng_managed",
		CredentialScope:          "bridge_pool",
		GatewayLocation:          "cloud",
		CatalogFresh:             true,
		CatalogVersion:           "cp5-route-catalog",
		RuntimeHash:              "sha256:" + stringOf('1', 64),
		OverlayHash:              "sha256:" + stringOf('2', 64),
		CatalogHash:              "sha256:" + stringOf('3', 64),
		PreferredProtocol:        "anthropic_messages",
		AnthropicBaseURL:         "http://127.0.0.1:9/anthropic",
		CapabilitiesVerified:     true,
		SupportsText:             true,
		SupportsTools:            true,
		SupportsStreaming:        true,
		SupportsUsage:            true,
		SupportsErrorPassthrough: true,
	}

	offlineHeaders := signedRouteHintHeadersForTest(t, body, "/v1/messages", now, map[string]any{
		"nonce":                "cp6-live-gate-offline-skeleton",
		"model_id":             "deepseek-v4-pro",
		"body_model":           "deepseek-v4-pro",
		"route":                "deepseek_bridge",
		"client_type":          "claude_code_bridge_deepseek",
		"provider":             "deepseek",
		"live_request_allowed": false,
	})
	svc := NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
	)
	_, err := svc.VerifyBridgeRouteHintRequest(http.MethodPost, "/v1/messages", offlineHeaders, body, decision)
	require.NoError(t, err)

	liveHeadersGateOff := signedRouteHintHeadersForTest(t, body, "/v1/messages", now, map[string]any{
		"nonce":                "cp6-live-gate-disabled",
		"model_id":             "deepseek-v4-pro",
		"body_model":           "deepseek-v4-pro",
		"route":                "deepseek_bridge",
		"client_type":          "claude_code_bridge_deepseek",
		"provider":             "deepseek",
		"live_request_allowed": true,
	})
	_, err = svc.VerifyBridgeRouteHintRequest(http.MethodPost, "/v1/messages", liveHeadersGateOff, body, decision)
	require.Error(t, err)
	require.Contains(t, err.Error(), "live request")

	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED", "1")
	liveHeadersMissingKey := signedRouteHintHeadersForTest(t, body, "/v1/messages", now, map[string]any{
		"nonce":                "cp6-live-gate-missing-key",
		"model_id":             "deepseek-v4-pro",
		"body_model":           "deepseek-v4-pro",
		"route":                "deepseek_bridge",
		"client_type":          "claude_code_bridge_deepseek",
		"provider":             "deepseek",
		"live_request_allowed": true,
	})
	_, err = NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
	).VerifyBridgeRouteHintRequest(http.MethodPost, "/v1/messages", liveHeadersMissingKey, body, decision)
	require.Error(t, err)
	require.Contains(t, err.Error(), "live request")

	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "sk-deepseek-test-key")
	liveHeadersMissingBillingGuard := signedRouteHintHeadersForTest(t, body, "/v1/messages", now, map[string]any{
		"nonce":                "cp6-live-gate-missing-billing-guard",
		"model_id":             "deepseek-v4-pro",
		"body_model":           "deepseek-v4-pro",
		"route":                "deepseek_bridge",
		"client_type":          "claude_code_bridge_deepseek",
		"provider":             "deepseek",
		"live_request_allowed": true,
	})
	_, err = NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
	).VerifyBridgeRouteHintRequest(http.MethodPost, "/v1/messages", liveHeadersMissingBillingGuard, body, decision)
	require.Error(t, err)
	require.Contains(t, err.Error(), "live request")

	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	liveHeadersGateOn := signedRouteHintHeadersForTest(t, body, "/v1/messages", now, map[string]any{
		"nonce":                "cp6-live-gate-enabled",
		"model_id":             "deepseek-v4-pro",
		"body_model":           "deepseek-v4-pro",
		"route":                "deepseek_bridge",
		"client_type":          "claude_code_bridge_deepseek",
		"provider":             "deepseek",
		"live_request_allowed": true,
	})
	_, err = NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
	).VerifyBridgeRouteHintRequest(http.MethodPost, "/v1/messages", liveHeadersGateOn, body, decision)
	require.NoError(t, err)

	externalDecision := decision
	externalDecision.AnthropicBaseURL = "https://api.deepseek.com/anthropic"
	liveHeadersExternalBase := signedRouteHintHeadersForTest(t, body, "/v1/messages", now, map[string]any{
		"nonce":                "cp6-live-gate-external-base-requires-production-billing",
		"model_id":             "deepseek-v4-pro",
		"body_model":           "deepseek-v4-pro",
		"route":                "deepseek_bridge",
		"client_type":          "claude_code_bridge_deepseek",
		"provider":             "deepseek",
		"live_request_allowed": true,
	})
	_, err = NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
	).VerifyBridgeRouteHintRequest(http.MethodPost, "/v1/messages", liveHeadersExternalBase, body, externalDecision)
	require.Error(t, err)
	require.Contains(t, err.Error(), "live request")

	nativeSpoofHeaders := signedRouteHintHeadersForTest(t, body, "/v1/messages", now, map[string]any{
		"nonce":                      "cp6-live-gate-native-spoof",
		"model_id":                   "deepseek-v4-pro",
		"body_model":                 "deepseek-v4-pro",
		"route":                      ClaudeCodeNativeRoute,
		"client_type":                ClaudeCodeNativeClientType,
		"provider":                   "claude",
		"live_request_allowed":       true,
		"formal_pool_allowed":        true,
		"native_attestation_allowed": true,
		"provider_owner":             ClaudeCodeNativeProviderOwner,
		"credential_scope":           ClaudeCodeNativeCredentialScope,
		"gateway_location":           ClaudeCodeNativeGatewayLocation,
	})
	nativeDecision := decision
	nativeDecision.Provider = "claude"
	nativeDecision.Route = ClaudeCodeNativeRoute
	nativeDecision.ClientType = ClaudeCodeNativeClientType
	nativeDecision.FormalPoolAllowed = true
	nativeDecision.NativeAttestationAllowed = true
	nativeDecision.CredentialScope = ClaudeCodeNativeCredentialScope
	_, err = NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
	).VerifyBridgeRouteHintRequest(http.MethodPost, "/v1/messages", nativeSpoofHeaders, body, nativeDecision)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot claim native")
}

func TestCP6RouteHintRejectsBridgeRequestWithNativeAttestationHeaders(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_ROUTE_HINT_SECRET", "route-hint-key")
	t.Setenv("SUB2API_CLAUDE_CODE_ROUTE_HINT_CURRENT_KEY_ID", "route_hint_v1")
	now := time.Unix(2710, 0)
	body := []byte(`{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"native header spoof"}],"stream":true}`)
	decision := ClaudeCodeProviderRouteDecision{
		ModelID:                  "deepseek-v4-pro",
		Provider:                 "deepseek",
		Route:                    "deepseek_bridge",
		ClientType:               "claude_code_bridge_deepseek",
		ProviderOwner:            "zhumeng_managed",
		CredentialScope:          "bridge_pool",
		GatewayLocation:          "cloud",
		CatalogFresh:             true,
		CatalogVersion:           "cp5-route-catalog",
		RuntimeHash:              "sha256:" + stringOf('1', 64),
		OverlayHash:              "sha256:" + stringOf('2', 64),
		CatalogHash:              "sha256:" + stringOf('3', 64),
		PreferredProtocol:        "anthropic_messages",
		AnthropicBaseURL:         "https://api.deepseek.com/anthropic",
		CapabilitiesVerified:     true,
		SupportsText:             true,
		SupportsTools:            true,
		SupportsStreaming:        true,
		SupportsUsage:            true,
		SupportsErrorPassthrough: true,
	}
	headers := signedRouteHintHeadersForTest(t, body, "/v1/messages", now, map[string]any{
		"nonce":                "cp6-native-header-spoof",
		"model_id":             "deepseek-v4-pro",
		"body_model":           "deepseek-v4-pro",
		"route":                "deepseek_bridge",
		"client_type":          "claude_code_bridge_deepseek",
		"provider":             "deepseek",
		"live_request_allowed": false,
	})
	headers.Set(ClaudeCodeNativeClientTypeHeader, "claude_code_bridge_deepseek")
	headers.Set(ClaudeCodeNativeGuardAttestedHeader, "true")
	headers.Set(ClaudeCodeNativeAttestationHeader, "forged-native-attestation")
	headers.Set(ClaudeCodeNativeSignatureHeader, "forged-native-signature")
	headers.Set(ClaudeCodeNativeInboundRouteHeader, "/v1/messages")
	headers.Set(ClaudeCodeNativeRuntimeHashHeader, "sha256:"+stringOf('9', 64))

	_, err := NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
	).VerifyBridgeRouteHintRequest(http.MethodPost, "/v1/messages", headers, body, decision)

	require.Error(t, err)
	require.Contains(t, err.Error(), "native attestation")
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
		"key_id":                              "guard_v1",
		"scope":                               "claude_code_native_takeover",
		"version":                             1,
		"issued_at":                           now.Unix(),
		"nonce":                               "nonce-001",
		"method":                              http.MethodPost,
		"request_uri":                         requestURI,
		"client_type":                         ClaudeCodeNativeClientType,
		"guard_attested":                      true,
		"guard_version":                       "guard_v1",
		"claude_code_version":                 "2.1.x",
		"local_session_ref":                   "hmac-sha256:" + stringOf('a', 64),
		"netwatch_required":                   true,
		"shape_healthcheck_profile":           "real_claude_code_native_takeover_v1",
		"route":                               ClaudeCodeNativeRoute,
		"model_id":                            gjson.GetBytes(body, "model").String(),
		"provider_owner":                      ClaudeCodeNativeProviderOwner,
		"credential_scope":                    ClaudeCodeNativeCredentialScope,
		"gateway_location":                    ClaudeCodeNativeGatewayLocation,
		"runtime_hash":                        "sha256:" + stringOf('1', 64),
		"overlay_hash":                        "sha256:" + stringOf('2', 64),
		"catalog_hash":                        "sha256:" + stringOf('3', 64),
		"catalog_version":                     "legacy-native",
		"session_ref":                         "hmac-sha256:" + stringOf('a', 64),
		"body_shape_hash":                     claudeCodeNativeBodyShapeHash(body),
		"replay_safety_boundary":              ClaudeCodeNativeReplaySafetyBoundary,
		"replay_safety_applied":               true,
		"replay_safety_sanitized":             false,
		"replay_safety_forbidden_paths_count": 0,
		"replay_safety_body_shape_hash":       claudeCodeNativeBodyShapeHash(body),
	}
	omitReplaySafetyContract, _ := overrides["omit_replay_safety_contract"].(bool)
	for key, value := range overrides {
		if key == "omit_replay_safety_contract" {
			continue
		}
		payload[key] = value
	}
	if omitReplaySafetyContract {
		for _, key := range []string{"replay_safety_boundary", "replay_safety_applied", "replay_safety_sanitized", "replay_safety_forbidden_paths_count", "replay_safety_body_shape_hash"} {
			delete(payload, key)
		}
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
	if catalogVersion, _ := payload["catalog_version"].(string); catalogVersion != "" {
		headers.Set(ClaudeCodeNativeCatalogVersionHeader, catalogVersion)
	}
	return headers
}

func signedRouteHintHeadersForTest(t *testing.T, body []byte, requestURI string, now time.Time, overrides map[string]any) http.Header {
	t.Helper()
	digest := sha256.Sum256(body)
	payload := map[string]any{
		"key_id":                     "route_hint_v1",
		"scope":                      ClaudeCodeRouteHintScope,
		"version":                    ClaudeCodeRouteHintVersion,
		"issued_at":                  now.Unix(),
		"expires_at":                 now.Add(time.Minute).Unix(),
		"nonce":                      "route-hint-nonce-001",
		"method":                     http.MethodPost,
		"request_uri":                requestURI,
		"model_id":                   gjson.GetBytes(body, "model").String(),
		"body_model":                 gjson.GetBytes(body, "model").String(),
		"body_sha256":                "sha256:" + hex.EncodeToString(digest[:]),
		"runtime_hash":               "sha256:" + stringOf('1', 64),
		"overlay_hash":               "sha256:" + stringOf('2', 64),
		"catalog_hash":               "sha256:" + stringOf('3', 64),
		"catalog_version":            "cp5-route-catalog",
		"session_ref":                "sess-route-hint",
		"route":                      "openai_bridge",
		"client_type":                "claude_code_bridge_openai",
		"provider":                   "openai",
		"live_request_allowed":       false,
		"formal_pool_allowed":        false,
		"native_attestation_allowed": false,
		"provider_owner":             "zhumeng_managed",
		"credential_scope":           "bridge_pool",
		"gateway_location":           "cloud",
	}
	for key, value := range overrides {
		payload[key] = value
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	headers := http.Header{}
	headers.Set(ClaudeCodeRouteHintHeader, encoded)
	headers.Set(ClaudeCodeRouteHintSignatureHeader, signClaudeCodeRouteHint(encoded, http.MethodPost, requestURI, body, "route-hint-key"))
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
