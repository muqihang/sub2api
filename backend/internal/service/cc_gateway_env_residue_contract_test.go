package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCCGatewayFormalPoolEnvResidueRefsAreServerSelectedWithObservedSafeBuckets(t *testing.T) {
	useClaudeCodeSessionBoundaryLedgerFileForTest(t)
	body := []byte(`{"model":"claude-sonnet-4-6","stream":true,"env_residue_profile_ref":"env-residue-profile:client-forged","envResidueProfileRef":"env-residue-profile:client-forged-camel","locale-profile-ref":"locale-profile:client-forged-kebab","base_url_residue_profile_ref":"base-url-residue-profile:client-forged","ANTHROPIC_BASE_URL":"https://neutral-gateway.test.invalid","baseUrl":"fixture-base-url","proxyUrl":"fixture-proxy-url","HTTP_PROXY":"http://127.0.0.1:8080","TZ":"Asia/Tokyo","timeZone":"Pacific/Forged","system":[{"type":"text","text":"Today’s date is 2026/06/30."}],"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}","env_residue_profile_ref":"env-residue-profile:metadata-forged","baseUrlResidueProfileRef":"base-url-residue-profile:metadata-forged","baseUrl":"fixture-base-url","proxyUrl":"fixture-proxy-url","HTTPS_PROXY":"http://127.0.0.1:8081","TZ":"Europe/Paris","timeZone":"Pacific/Forged"},"tools":[{"name":"Bash","description":"safe fixture","metadata":{"locale_profile_ref":"locale-profile:tool-metadata-forged","ANTHROPIC_BASE_URL":"https://neutral-gateway.test.invalid"},"input_schema":{"type":"object","properties":{"cmd":{"type":"string","envResidueProfileRef":"env-residue-profile:tool-field-forged"}}}}],"messages":[{"role":"user","content":[{"type":"text","text":"safe fixture text"}]}]}`)
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
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction

	c := ccGatewayTestContext("/v1/messages")
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.196 (external, cli)")
	c.Request.Header.Set("X-Cc-Env-Residue-Profile-Ref", "env-residue-profile:header-forged")
	c.Request.Header.Set("X-Cc-Locale-Profile-Ref", "locale-profile:header-forged")
	c.Request.Header.Set("X-Cc-Base-Url-Residue-Profile-Ref", "base-url-residue-profile:header-forged")
	c.Request.Header.Set("Anthropic-Base-Url", "https://neutral-gateway.test.invalid")
	c.Request.Header.Set("Base-Url", "fixture-base-url")
	c.Request.Header.Set("Proxy-Url", "fixture-proxy-url")
	c.Request.Header.Set("HTTP-Proxy", "http://127.0.0.1:8080")
	c.Request.Header.Set("TZ", "Asia/Tokyo")
	c.Request.Header.Set("TimeZone", "Pacific/Forged")
	c.Request.URL.RawQuery = "env_residue_profile_ref=env-residue-profile:query-forged&envResidueProfileRef=env-residue-profile:query-camel&locale-profile-ref=locale-profile:query-forged&base_url_residue_profile_ref=base-url-residue-profile:query-forged&ANTHROPIC_BASE_URL=https%3A%2F%2Fneutral-gateway.test.invalid&baseUrl=fixture-base-url&proxyUrl=fixture-proxy-url&HTTP_PROXY=http%3A%2F%2F127.0.0.1%3A8080&TZ=Asia%2FTokyo&timeZone=Pacific%2FForged"

	req, _, err := (&GatewayService{cfg: ccGatewayTestConfig(PlatformAnthropic), identityService: NewIdentityService(ccGatewayIdentityCache{})}).buildUpstreamRequest(
		context.Background(), c, account, body, "oauth-token", "oauth", "claude-sonnet-4-6", true, false, false,
	)
	require.NoError(t, err)

	ctx := decodeCCGatewayFormalPoolContextForTest(t, req)
	require.Equal(t, "env-residue-profile:claude-code-2.1.179-us-pacific-official-anthropic-v1", ctx["env_residue_profile_ref"])
	require.Equal(t, "locale-profile:us-pacific-v1", ctx["locale_profile_ref"])
	require.Equal(t, "base-url-residue-profile:official-anthropic-v1", ctx["base_url_residue_profile_ref"])
	require.Equal(t, ccGatewayAnthropicPolicyVersion, ctx["policy_version"])
	require.Equal(t, ccGatewayDefaultPersonaProfile, ctx["persona_profile"])
	require.Equal(t, ccGatewayDefaultEgressTLSProfileRef, ctx["egress_tls_profile_ref"])

	observed := ctx["observed_client_profile"].(map[string]any)
	require.Equal(t, "2.1.196", observed["cli_version_bucket"])
	require.Equal(t, "cli", observed["client_family_bucket"])
	require.Equal(t, true, observed["local_env_residue_present"])
	require.Equal(t, "slash", observed["date_format_bucket"])
	require.Equal(t, "unicode_variant_1", observed["apostrophe_bucket"])
	require.Equal(t, "neutral_gateway", observed["base_url_category_bucket"])
	require.Equal(t, "loopback_proxy_only", observed["proxy_env_bucket"])

	canonical, err := json.Marshal(ctx)
	require.NoError(t, err)
	for _, forbidden := range []string{"client-forged", "header-forged", "query-forged", "metadata-forged", "tool-metadata-forged", "tool-field-forged", "neutral-gateway.test.invalid", "fixture-base-url", "fixture-proxy-url", "Asia/Tokyo", "Europe/Paris", "Pacific/Forged"} {
		require.NotContains(t, string(canonical), forbidden)
	}
	require.Empty(t, getHeaderRaw(req.Header, "x-cc-env-residue-profile-ref"))
	require.Empty(t, getHeaderRaw(req.Header, "x-cc-locale-profile-ref"))
	require.Empty(t, getHeaderRaw(req.Header, "x-cc-base-url-residue-profile-ref"))
	require.Empty(t, getHeaderRaw(req.Header, "anthropic-base-url"))
	require.Empty(t, getHeaderRaw(req.Header, "base-url"))
	require.Empty(t, getHeaderRaw(req.Header, "proxy-url"))
	require.Empty(t, getHeaderRaw(req.Header, "http-proxy"))
	require.Empty(t, getHeaderRaw(req.Header, "tz"))
	require.Empty(t, getHeaderRaw(req.Header, "timezone"))
	requireValidCCGatewayFormalPoolSignatureForTest(t, req, "formal-pool-attestation-secret-test")
}

func TestCCGatewayObservedEnvResidueExternalIssueAnchorBuckets(t *testing.T) {
	dateCases := []struct {
		name           string
		body           string
		wantDate       string
		wantApostrophe string
	}{
		{
			name:           "ascii apostrophe with hyphen date",
			body:           `{"system":[{"type":"text","text":"Today's date is 2026-06-30."}]}`,
			wantDate:       "hyphen",
			wantApostrophe: "ascii",
		},
		{
			name:           "right single quote with slash date",
			body:           `{"system":[{"type":"text","text":"Today\u2019s date is 2026/06/30."}]}`,
			wantDate:       "slash",
			wantApostrophe: "unicode_variant_1",
		},
		{
			name:           "modifier letter apostrophe with hyphen date",
			body:           `{"system":[{"type":"text","text":"Today\u02bcs date is 2026-06-30."}]}`,
			wantDate:       "hyphen",
			wantApostrophe: "unicode_variant_2",
		},
		{
			name:           "modifier letter prime with slash date",
			body:           `{"system":[{"type":"text","text":"Today\u02b9s date is 2026/06/30."}]}`,
			wantDate:       "slash",
			wantApostrophe: "unicode_variant_3",
		},
	}
	for _, tc := range dateCases {
		t.Run(tc.name, func(t *testing.T) {
			dateFormat, apostrophe, observed := ccGatewayObservedDateMarkerBuckets([]byte(tc.body))
			require.True(t, observed)
			require.Equal(t, tc.wantDate, dateFormat)
			require.Equal(t, tc.wantApostrophe, apostrophe)
		})
	}

	baseURLCases := []struct {
		name string
		url  string
		want string
	}{
		{name: "synthetic China TLD residue", url: "https://fixture.example.cn", want: "china_tld"},
		{name: "synthetic AI keyword residue", url: "https://model-lab.invalid", want: "ai_lab_keyword"},
		{name: "synthetic proxy resale residue", url: "https://fixture-proxy.invalid", want: "claude_proxy_resale_like"},
	}
	for _, tc := range baseURLCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptestRequestForEnvResidueProfileTest()
			req.Header.Set("Anthropic-Base-Url", tc.url)
			bucket, observed := ccGatewayObservedBaseURLCategoryBucket(req, nil)
			require.True(t, observed)
			require.Equal(t, tc.want, bucket)
		})
	}
}

func TestCCGatewayFormalPoolEnvResidueClientFamilyObservedOnly(t *testing.T) {
	useClaudeCodeSessionBoundaryLedgerFileForTest(t)
	cases := []struct {
		name       string
		userAgent  string
		wantFamily string
	}{
		{name: "cli", userAgent: "claude-cli/2.1.196 (external, cli)", wantFamily: "cli"},
		{name: "desktop", userAgent: "Claude Code Desktop/2.1.196", wantFamily: "desktop"},
		{name: "vscode", userAgent: "Claude VSCode extension/2.1.196", wantFamily: "vscode"},
		{name: "unknown", userAgent: "safe-fixture-client/9.9", wantFamily: "unknown"},
	}
	var baselineWireUA *string
	var baselineBeta *string
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`{"model":"claude-sonnet-4-6","stream":true,"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}"},"messages":[{"role":"user","content":"safe fixture text"}]}`)
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
			account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction

			c := ccGatewayTestContext("/v1/messages")
			c.Request.Header.Set("User-Agent", tc.userAgent)
			req, _, err := (&GatewayService{cfg: ccGatewayTestConfig(PlatformAnthropic), identityService: NewIdentityService(ccGatewayIdentityCache{})}).buildUpstreamRequest(
				context.Background(), c, account, body, "oauth-token", "oauth", "claude-sonnet-4-6", true, false, false,
			)
			require.NoError(t, err)

			ctx := decodeCCGatewayFormalPoolContextForTest(t, req)
			observed := ctx["observed_client_profile"].(map[string]any)
			require.Equal(t, tc.wantFamily, observed["client_family_bucket"])
			require.Equal(t, "env-residue-profile:claude-code-2.1.179-us-pacific-official-anthropic-v1", ctx["env_residue_profile_ref"])
			require.Equal(t, "locale-profile:us-pacific-v1", ctx["locale_profile_ref"])
			require.Equal(t, "base-url-residue-profile:official-anthropic-v1", ctx["base_url_residue_profile_ref"])
			require.Equal(t, ccGatewayAnthropicPolicyVersion, ctx["policy_version"])
			require.Equal(t, ccGatewayDefaultEgressTLSProfileRef, ctx["egress_tls_profile_ref"])
			require.Equal(t, "strip", ctx["billing_shape_policy"])
			require.Equal(t, ccGatewayDefault2179RequestShapeProfile, ctx["request_shape_profile_ref"])
			require.Equal(t, ccGatewayDefault2179CacheParityProfile, ctx["cache_parity_profile_ref"])
			require.Equal(t, ccGatewayDefaultTrustedEgressProfileRef, ctx["trusted_egress_profile_ref"])
			require.Equal(t, "2.1.179", getHeaderRaw(req.Header, ccGatewayPolicyVersionHeader))
			wireUA := getHeaderRaw(req.Header, "User-Agent")
			beta := getHeaderRaw(req.Header, "anthropic-beta")
			if baselineWireUA == nil {
				baselineWireUA = &wireUA
			} else {
				require.Equal(t, *baselineWireUA, wireUA)
			}
			if baselineBeta == nil {
				baselineBeta = &beta
			} else {
				require.Equal(t, *baselineBeta, beta)
			}
			require.NotContains(t, beta, "2.1.196")
		})
	}
}

func TestCCGatewayFormalPoolEnvResidueRefsBindClaudePlatformAWSContext(t *testing.T) {
	useClaudeCodeSessionBoundaryLedgerFileForTest(t)
	cfg := ccGatewayTestConfig(PlatformAnthropic)
	account := claudePlatformAWSRequestTestAccount(t, ClaudePlatformAWSAuthProfileXAPIKey, true)
	markClaudePlatformAWSFormalPoolForRequestTest(t, account)
	account.Extra[ccGatewayExtraPersonaProfile] = ccGatewayDefaultPersonaProfile
	account.Extra[ccGatewayExtraTrustedEgressProfile] = ccGatewayDefaultTrustedEgressProfileRef
	account.Extra[ccGatewayExtraProfilePolicyVersion] = ccGatewayDefault2179ProfilePolicyVersion
	account.Extra[ccGatewayExtraBillingShapePolicy] = ccGatewayDefaultBillingShapePolicy
	validation, validationErr := ValidateClaudePlatformAWSAccountWithCCGatewayConfig(account, cfg)
	require.NoError(t, validationErr)
	account.Extra[ccGatewayExtraCredentialBindingHMAC] = validation.CredentialBindingHMAC
	account.Extra[ClaudePlatformAWSExtraWorkspaceBindingHMAC] = validation.WorkspaceBindingHMAC

	body := []byte(`{"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"11111111-2222-4333-8444-555555555555\"}"},"model":"claude-sonnet-4-6","env_residue_profile_ref":"env-residue-profile:client-forged","messages":[{"role":"user","content":"safe fixture"}]}`)
	c := claudePlatformAWSRequestTestContext()
	c.Request.Header.Set("User-Agent", "Claude Code Desktop/2.1.196")
	c.Request.Header.Set("X-Cc-Env-Residue-Profile-Ref", "env-residue-profile:header-forged")
	c.Request.URL.RawQuery = "base_url_residue_profile_ref=base-url-residue-profile:query-forged"
	req, _, err := (&GatewayService{cfg: cfg, identityService: NewIdentityService(ccGatewayIdentityCache{})}).buildUpstreamRequest(
		context.Background(), c, account, body, account.GetCredential("api_key"), "claude_platform_aws", "claude-sonnet-4-6", false, false, false,
	)
	require.NoError(t, err)

	ctx := decodeCCGatewayFormalPoolContextForTest(t, req)
	require.Equal(t, "env-residue-profile:claude-code-2.1.179-us-pacific-official-anthropic-v1", ctx["env_residue_profile_ref"])
	require.Equal(t, "locale-profile:us-pacific-v1", ctx["locale_profile_ref"])
	require.Equal(t, "base-url-residue-profile:official-anthropic-v1", ctx["base_url_residue_profile_ref"])
	require.Equal(t, claudePlatformAWSProviderKind, ctx["provider_kind"])
	require.Equal(t, validation.CredentialBindingHMAC, ctx["credential_binding_hmac"])
	require.Equal(t, validation.WorkspaceBindingHMAC, ctx["workspace_binding_hmac"])
	observed := ctx["observed_client_profile"].(map[string]any)
	require.Equal(t, "desktop", observed["client_family_bucket"])
	requireValidCCGatewayFormalPoolSignatureForTest(t, req, "formal-pool-attestation-secret-test")
}

func TestCCGatewayFormalPoolEnvResidueRejectsUnsafeServerOverride(t *testing.T) {
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
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction
	account.Extra[ccGatewayExtraCredentialRef] = "opaque:credential-ref:v1:cred-a"
	account.Extra[ccGatewayExtraProxyIdentityRef] = "opaque:proxy-ref:v1:bucket-a"
	account.Extra[ccGatewayExtraPersonaProfile] = ccGatewayDefaultPersonaProfile
	account.Extra["claude_code_device_id"] = strings.Repeat("c", 64)
	account.Extra["cc_gateway_env_residue_profile_ref"] = "https://unsafe.example/profile"
	req := httptestRequestForEnvResidueProfileTest()
	require.NoError(t, applyCCGatewayAnthropicHeaders(req, ccGatewayTestConfig(PlatformAnthropic), account, "oauth"))
	err := applyCCGatewayFormalPoolAttestation(req, ccGatewayTestConfig(PlatformAnthropic), account)
	require.Error(t, err)
	require.Contains(t, err.Error(), "env_residue_profile_ref")
	require.NotContains(t, err.Error(), "unsafe.example")
}

func httptestRequestForEnvResidueProfileTest() *http.Request {
	req := ccGatewayTestContext("/v1/messages").Request
	req.Header.Set("X-Claude-Code-Session-Id", "123e4567-e89b-42d3-a456-426614174999")
	return req
}

func TestCCGatewayFormalPoolEnvResidueStripsOnlyAllowedAuthoritySurfacesAndUpdatesWireBody(t *testing.T) {
	body := []byte(`{"system":[{"type":"text","text":"safe system"}],"metadata":{"env_residue_profile_ref":"env-residue-profile:metadata-forged","TZ":"Asia/Shanghai","timeZone":"Asia/Urumqi","baseUrl":"fixture-base-url","proxyUrl":"fixture-proxy-url"},"tools":[{"name":"Tool","description":"safe fixture","metadata":{"ANTHROPIC_BASE_URL":"https://neutral-gateway.test.invalid"},"input_schema":{"type":"object","properties":{"city":{"type":"string","HTTP_PROXY":"http://127.0.0.1:8080"}}}}],"unrelated":{"locale_profile_ref":"locale-profile:must-remain-out-of-scope"},"messages":[{"role":"user","content":[{"type":"text","text":"safe fixture text"}]}]}`)
	rewritten, changed := stripClientEnvResidueProfileHintsFromBody(body)
	require.True(t, changed)
	require.NotContains(t, string(rewritten), "metadata-forged")
	require.NotContains(t, string(rewritten), "Asia/Shanghai")
	require.NotContains(t, string(rewritten), "Asia/Urumqi")
	require.NotContains(t, string(rewritten), "fixture-base-url")
	require.NotContains(t, string(rewritten), "fixture-proxy-url")
	require.NotContains(t, string(rewritten), "neutral-gateway.test.invalid")
	require.NotContains(t, string(rewritten), "127.0.0.1:8080")
	require.Contains(t, string(rewritten), "must-remain-out-of-scope")
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(rewritten, &parsed))
	metadata := parsed["metadata"].(map[string]any)
	require.NotContains(t, metadata, "env_residue_profile_ref")
	require.NotContains(t, metadata, "TZ")
	require.NotContains(t, metadata, "timeZone")
	require.NotContains(t, metadata, "baseUrl")
	require.NotContains(t, metadata, "proxyUrl")
	unrelated := parsed["unrelated"].(map[string]any)
	require.Equal(t, "locale-profile:must-remain-out-of-scope", unrelated["locale_profile_ref"])
}

func TestCCGatewayFormalPoolEnvResidueBuildUpstreamReturnsStrippedWireBody(t *testing.T) {
	useClaudeCodeSessionBoundaryLedgerFileForTest(t)
	body := []byte(`{"model":"claude-sonnet-4-6","stream":true,"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}","env_residue_profile_ref":"env-residue-profile:metadata-forged"},"messages":[{"role":"user","content":"safe fixture text"}]}`)
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
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction

	c := ccGatewayTestContext("/v1/messages")
	req, wireBody, err := (&GatewayService{cfg: ccGatewayTestConfig(PlatformAnthropic), identityService: NewIdentityService(ccGatewayIdentityCache{})}).buildUpstreamRequest(
		context.Background(), c, account, body, "oauth-token", "oauth", "claude-sonnet-4-6", true, false, false,
	)
	require.NoError(t, err)
	require.NotContains(t, string(wireBody), "metadata-forged")
	require.Equal(t, string(claudeCodeReadRequestBody(req)), string(wireBody))
}
