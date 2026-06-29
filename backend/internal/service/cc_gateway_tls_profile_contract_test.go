package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const ccGatewayRealOracleTLSProfileRefForTest = "tls-profile:claude-code-2.1.179-real-oracle-tcp-v1"

func TestCCGatewayFormalPoolAttestationIncludesServerSelectedTLSProfileRef(t *testing.T) {
	useClaudeCodeSessionBoundaryLedgerFileForTest(t)
	body := []byte(`{"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}"},"model":"claude-sonnet-4-6","egress_tls_profile_ref":"tls-profile:client-forged-body","tls_profile":{"ref":"tls-profile:client-forged-nested"},"messages":[{"role":"user","content":"hi"}]}`)
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	account.Extra["cc_gateway_enabled"] = "true"
	account.Extra["cc_gateway_canary_only"] = "false"
	account.Extra["cc_gateway_policy_version"] = ccGatewayAnthropicPolicyVersion
	account.Extra["cc_gateway_routes"] = "native_messages,native_count_tokens,chat_completions,responses"
	account.Extra["cc_gateway_egress_bucket_enabled"] = "true"
	account.Extra["cc_gateway_egress_bucket"] = "bucket-a"
	account.Extra["cc_gateway_account_ref"] = "hmac-sha256:" + strings.Repeat("c", 64)
	formalPoolApplyCompleteSchedulingEvidenceForTest(account)
	account.Extra[ccGatewayExtraCredentialRef] = "opaque:credential-ref:v1:cred-a"
	account.Extra[ccGatewayExtraProxyIdentityRef] = "opaque:proxy-ref:v1:bucket-a"
	account.Extra[ccGatewayExtraPersonaProfile] = ccGatewayDefaultPersonaProfile
	account.Extra["cc_gateway_egress_tls_profile_ref"] = ccGatewayRealOracleTLSProfileRefForTest
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction

	c := ccGatewayTestContext("/v1/messages")
	c.Request.Header.Set("x-cc-egress-tls-profile-ref", "tls-profile:client-forged-header")
	c.Request.Header.Set("x-sub2api-tls-profile", "tls-profile:client-forged-sub2api")
	c.Request.URL.RawQuery = "egress_tls_profile_ref=tls-profile:client-forged-query"
	req, _, err := (&GatewayService{
		cfg:             ccGatewayTestConfig(PlatformAnthropic),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}).buildUpstreamRequest(context.Background(), c, account, body, "oauth-token", "oauth", "claude-sonnet-4-6", true, false, false)
	require.NoError(t, err)

	ctx := decodeCCGatewayFormalPoolContextForTest(t, req)
	require.Equal(t, ccGatewayRealOracleTLSProfileRefForTest, ctx["egress_tls_profile_ref"])
	require.NotContains(t, ctx, "tls_profile")
	require.NotContains(t, ctx, "client_tls_profile")
	requireValidCCGatewayFormalPoolSignatureForTest(t, req, "formal-pool-attestation-secret-test")

	canonical, err := json.Marshal(ctx)
	require.NoError(t, err)
	require.Contains(t, string(canonical), ccGatewayRealOracleTLSProfileRefForTest)
	require.NotContains(t, string(canonical), "client-forged")
	observed, ok := ctx["observed_client_profile"].(map[string]any)
	require.True(t, ok)
	require.NotContains(t, observed, "unknown_top_level_body_key_count")
	if keys, ok := observed["top_level_body_keys"].([]any); ok {
		for _, key := range keys {
			require.NotContains(t, key, "tls")
		}
	}
	require.Empty(t, getHeaderRaw(req.Header, "x-cc-egress-tls-profile-ref"))
	require.Empty(t, getHeaderRaw(req.Header, "x-sub2api-tls-profile"))
}

func TestCCGatewayFormalPoolAttestationIgnoresNestedTLSHintBillingMaterial(t *testing.T) {
	useClaudeCodeSessionBoundaryLedgerFileForTest(t)
	forgedBillingHeader := "x-" + "anthropic" + "-billing-" + "header:" + " cc_" + "entrypoint=cli;"
	bodyMap := map[string]any{
		"metadata": map[string]any{"user_id": "{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}"},
		"model":    "claude-sonnet-4-6",
		"messages": []map[string]any{{"role": "user", "content": "hi"}},
		"tls_profile": map[string]any{
			"ref":       "tls-profile:client-forged-nested",
			"safe_text": forgedBillingHeader,
		},
	}
	body, err := json.Marshal(bodyMap)
	require.NoError(t, err)

	account := newAnthropicOAuthAccountForClaudeForwardTest()
	account.Extra["cc_gateway_enabled"] = "true"
	account.Extra["cc_gateway_canary_only"] = "false"
	account.Extra["cc_gateway_policy_version"] = ccGatewayAnthropicPolicyVersion
	account.Extra["cc_gateway_routes"] = "native_messages,native_count_tokens,chat_completions,responses"
	account.Extra["cc_gateway_egress_bucket_enabled"] = "true"
	account.Extra["cc_gateway_egress_bucket"] = "bucket-a"
	account.Extra["cc_gateway_account_ref"] = "hmac-sha256:" + strings.Repeat("c", 64)
	formalPoolApplyCompleteSchedulingEvidenceForTest(account)
	account.Extra[ccGatewayExtraCredentialRef] = "opaque:credential-ref:v1:cred-a"
	account.Extra[ccGatewayExtraProxyIdentityRef] = "opaque:proxy-ref:v1:bucket-a"
	account.Extra[ccGatewayExtraPersonaProfile] = ccGatewayDefaultPersonaProfile
	account.Extra["cc_gateway_egress_tls_profile_ref"] = ccGatewayRealOracleTLSProfileRefForTest
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction

	c := ccGatewayTestContext("/v1/messages")
	req, _, err := (&GatewayService{
		cfg:             ccGatewayTestConfig(PlatformAnthropic),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}).buildUpstreamRequest(context.Background(), c, account, body, "oauth-token", "oauth", "claude-sonnet-4-6", true, false, false)
	require.NoError(t, err)

	ctx := decodeCCGatewayFormalPoolContextForTest(t, req)
	observed, ok := ctx["observed_client_profile"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, float64(0), observed["billing_block_count"])
	require.Equal(t, "absent", observed["billing_shape"])
	require.Equal(t, "absent", observed["cc_entrypoint_bucket"])
	canonical, err := json.Marshal(ctx)
	require.NoError(t, err)
	require.NotContains(t, string(canonical), "client-forged")
}

func TestCCGatewayFormalPoolAttestationRejectsUnsafeTLSProfileRef(t *testing.T) {
	useClaudeCodeSessionBoundaryLedgerFileForTest(t)
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	account.Extra["cc_gateway_enabled"] = "true"
	account.Extra["cc_gateway_canary_only"] = "false"
	account.Extra["cc_gateway_policy_version"] = ccGatewayAnthropicPolicyVersion
	account.Extra["cc_gateway_routes"] = "native_messages"
	account.Extra["cc_gateway_egress_bucket_enabled"] = "true"
	account.Extra["cc_gateway_egress_bucket"] = "bucket-a"
	account.Extra["cc_gateway_account_ref"] = "hmac-sha256:" + strings.Repeat("c", 64)
	formalPoolApplyCompleteSchedulingEvidenceForTest(account)
	account.Extra[ccGatewayExtraCredentialRef] = "opaque:credential-ref:v1:cred-a"
	account.Extra[ccGatewayExtraProxyIdentityRef] = "opaque:proxy-ref:v1:bucket-a"
	account.Extra[ccGatewayExtraPersonaProfile] = ccGatewayDefaultPersonaProfile
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction

	for name, ref := range map[string]string{
		"raw_url":      "https://example.invalid/profile",
		"embedded_key": "tls-profile:sk-test-secret",
		"newline":      "tls-profile:good\nforged",
		"plain_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	} {
		t.Run(name, func(t *testing.T) {
			attempt := *account
			attempt.Extra = cloneCredentials(account.Extra)
			attempt.Extra["cc_gateway_egress_tls_profile_ref"] = ref
			req := httptestRequestForTLSProfileTest()
			require.NoError(t, applyCCGatewayAnthropicHeaders(req, ccGatewayTestConfig(PlatformAnthropic), &attempt, "oauth"))
			err := applyCCGatewayFormalPoolAttestation(req, ccGatewayTestConfig(PlatformAnthropic), &attempt)
			require.Error(t, err)
			require.Contains(t, err.Error(), "egress_tls_profile_ref")
			require.NotContains(t, err.Error(), ref)
		})
	}
}

func httptestRequestForTLSProfileTest() *http.Request {
	req := ccGatewayTestContext("/v1/messages").Request
	req.Header.Set("X-Claude-Code-Session-Id", "123e4567-e89b-42d3-a456-426614174999")
	return req
}
