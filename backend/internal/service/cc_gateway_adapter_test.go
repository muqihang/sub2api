package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type ccGatewayIdentityCache struct{}

func (ccGatewayIdentityCache) GetFingerprint(context.Context, int64) (*Fingerprint, error) {
	return &Fingerprint{
		ClientID:                "rewritten-device-id",
		UserAgent:               "claude-cli/9.9.9 (external, cli)",
		StainlessLang:           "js",
		StainlessPackageVersion: "0.70.0",
		StainlessOS:             "Linux",
		StainlessArch:           "arm64",
		StainlessRuntime:        "node",
		StainlessRuntimeVersion: "v24.13.0",
	}, nil
}

func (ccGatewayIdentityCache) SetFingerprint(context.Context, int64, *Fingerprint) error {
	return nil
}

func (ccGatewayIdentityCache) GetMaskedSessionID(context.Context, int64) (string, error) {
	return "", errors.New("not found")
}

func (ccGatewayIdentityCache) SetMaskedSessionID(context.Context, int64, string) error {
	return nil
}

func ccGatewayTestConfig(provider string) *config.Config {
	cfg := &config.Config{}
	cfg.Gateway.MaxLineSize = defaultMaxLineSize
	cfg.Gateway.CCGateway.Enabled = true
	cfg.Gateway.CCGateway.BaseURL = "http://cc-gateway:8443"
	cfg.Gateway.CCGateway.Token = "ccg-token"
	cfg.Gateway.CCGateway.InternalControlToken = "internal-control-material-test"
	cfg.Gateway.CCGateway.ContextAttestationSecret = "formal-pool-attestation-secret-test"
	cfg.Gateway.CCGateway.StickySessionHMACKey = "sub2api-gateway-sticky-session-dev-key"
	cfg.Gateway.CCGateway.ClaudePlatformAWSWorkspaceBindingHMACKey = "sub2api-claude-platform-aws-binding-v1"
	cfg.Gateway.CCGateway.DefaultEgressBucket = "default"
	switch provider {
	case PlatformAnthropic:
		cfg.Gateway.CCGateway.Providers.Anthropic = true
	case PlatformAntigravity:
		cfg.Gateway.CCGateway.Providers.Antigravity = true
	}
	return cfg
}

func ccGatewayCanaryTestConfig() *config.Config {
	cfg := ccGatewayTestConfig(PlatformAnthropic)
	cfg.Gateway.CCGateway.BaseURL = "http://127.0.0.1:18443"
	return cfg
}

func ccGatewayTestContext(path string) *gin.Context {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.131 (external, cli)")
	c.Request.Header.Set("Anthropic-Beta", "client-beta")
	c.Request.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	return c
}

func readRequestBody(t *testing.T, req *http.Request) string {
	t.Helper()
	body, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	req.Body = io.NopCloser(bytes.NewReader(body))
	return string(body)
}

func applyCCGatewayAPIKeyFormalPoolContextForTest(account *Account) {
	account.Extra["cc_gateway_account_ref"] = "hmac-sha256:" + strings.Repeat("d", 64)
	account.Extra[ccGatewayExtraCredentialRef] = "opaque:credential-ref:v1:apikey-cred-a"
	account.Extra[ccGatewayExtraProxyIdentityRef] = "opaque:proxy-ref:v1:bucket-a"
	account.Extra[ccGatewayExtraPersonaProfile] = ccGatewayHealthcheckNon1MProfile
}

func TestValidateExplicitCCGatewayCanaryAccount(t *testing.T) {
	account := &Account{
		ID:          3,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: false,
		Credentials: map[string]any{
			"access_token": "tok",
			"scope":        "user:profile user:inference user:sessions:claude_code",
		},
		Extra: map[string]any{
			"cc_gateway_enabled":               true,
			"cc_gateway_canary_only":           true,
			"cc_gateway_egress_bucket":         "home-ip-canary-2026-05-22",
			"cc_gateway_egress_bucket_enabled": true,
			"cc_gateway_policy_version":        ccGatewayAnthropicPolicyVersion,
			"cc_gateway_routes":                "native_messages",
			"billing_cch_mode":                 "sign",
		},
	}
	req := CCGatewayAnthropicCanaryRequest{
		AccountID:      account.ID,
		EgressBucket:   "home-ip-canary-2026-05-22",
		BillingCCHMode: "sign",
		Method:         http.MethodPost,
		Route:          "/v1/messages",
	}

	require.NoError(t, validateCCGatewayAnthropicCanaryAccountWithConfig(ccGatewayCanaryTestConfig(), account, req))

	missingScope := *account
	missingScope.Credentials = map[string]any{"access_token": "tok", "scope": "user:profile"}
	require.ErrorContains(t, validateCCGatewayAnthropicCanaryAccountWithConfig(ccGatewayCanaryTestConfig(), &missingScope, req), "user:inference")

	normalAccount := *account
	normalAccount.Extra = map[string]any{
		"cc_gateway_enabled":               true,
		"cc_gateway_egress_bucket":         "home-ip-canary-2026-05-22",
		"cc_gateway_egress_bucket_enabled": true,
		"cc_gateway_policy_version":        ccGatewayAnthropicPolicyVersion,
		"cc_gateway_routes":                "native_messages",
		"billing_cch_mode":                 "sign",
	}
	require.ErrorContains(t, validateCCGatewayAnthropicCanaryAccountWithConfig(ccGatewayCanaryTestConfig(), &normalAccount, req), "canary-only")

	wrongBucketReq := req
	wrongBucketReq.EgressBucket = "other-bucket"
	require.ErrorContains(t, validateCCGatewayAnthropicCanaryAccountWithConfig(ccGatewayCanaryTestConfig(), account, wrongBucketReq), "egress bucket")

	wrongBillingReq := req
	wrongBillingReq.BillingCCHMode = "strip"
	require.ErrorContains(t, validateCCGatewayAnthropicCanaryAccountWithConfig(ccGatewayCanaryTestConfig(), account, wrongBillingReq), "billing_cch_mode")

	wrongRouteReq := req
	wrongRouteReq.Route = "/v1/messages/count_tokens"
	require.ErrorContains(t, validateCCGatewayAnthropicCanaryAccountWithConfig(ccGatewayCanaryTestConfig(), account, wrongRouteReq), "POST /v1/messages")

	nonLocalConfig := ccGatewayTestConfig(PlatformAnthropic)
	require.ErrorContains(t, validateCCGatewayAnthropicCanaryAccountWithConfig(nonLocalConfig, account, req), "local")
}

func TestCCGatewayAnthropicPolicyVersionTracksClaudeCode2179ProductionAnchor(t *testing.T) {
	require.Equal(t, "2.1.179", ccGatewayAnthropicPolicyVersion)
}

func TestGatewayService_CCGatewayAnthropicOAuthMapsServerSessionIntoMetadataAndHeader(t *testing.T) {
	body := []byte(`{"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"11111111-2222-4333-8444-555555555555\"}"},"model":"claude-3-7-sonnet-20250219","messages":[{"role":"user","content":"hi"}]}`)
	account := &Account{
		ID:       42,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "selected-oauth-token",
			"email":        "user@example.com",
		},
		Extra: map[string]any{
			"account_uuid":                     "acct-uuid",
			"organization_uuid":                "org-uuid",
			"cc_gateway_enabled":               "true",
			"cc_gateway_canary_only":           "false",
			"cc_gateway_policy_version":        ccGatewayAnthropicPolicyVersion,
			"cc_gateway_routes":                "native_messages,native_count_tokens,chat_completions,responses",
			"cc_gateway_egress_bucket_enabled": "true",
			"cc_gateway_egress_bucket":         "bucket-a",
			"billing_cch_mode":                 "sign",
		},
	}
	svc := &GatewayService{
		cfg:             ccGatewayTestConfig(PlatformAnthropic),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}

	ctx := WithClaudeCodeSessionUserScope(SetClaudeCodeVersion(context.Background(), ccGatewayAnthropicPolicyVersion), "user:alpha")
	c := ccGatewayTestContext("/v1/messages")
	c.Request.Header.Set("X-Claude-Code-Session-Id", "11111111-2222-4333-8444-555555555555")

	req, _, err := svc.buildUpstreamRequest(ctx, c, account, body, "selected-oauth-token", "oauth", "claude-3-7-sonnet-20250219", true, false, false)
	require.NoError(t, err)

	mappedSessionID := getHeaderRaw(req.Header, "X-Claude-Code-Session-Id")
	require.Regexp(t, regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`), mappedSessionID)
	require.NotEqual(t, "11111111-2222-4333-8444-555555555555", mappedSessionID)

	rewrittenBody := readRequestBody(t, req)
	parsedUserID := ParseMetadataUserID(gjson.Get(rewrittenBody, "metadata.user_id").String())
	require.NotNil(t, parsedUserID)
	require.Equal(t, mappedSessionID, parsedUserID.SessionID)
	require.NotContains(t, rewrittenBody, "11111111-2222-4333-8444-555555555555")
}

func TestGatewayService_CCGatewayFormalPoolRequiresSafeAccountRef(t *testing.T) {
	account := &Account{
		ID:          42,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"access_token": "selected-oauth-token",
			"scope":        "user:profile user:inference user:sessions:claude_code",
		},
		Extra: map[string]any{
			FormalPoolExtraOnboardingStage:          FormalPoolStageProduction,
			FormalPoolExtraRuntimeRegistered:        "true",
			FormalPoolExtraHealthcheckCCGatewaySeen: true,
			"cc_gateway_enabled":                    "true",
			"cc_gateway_canary_only":                "false",
			"cc_gateway_policy_version":             ccGatewayAnthropicPolicyVersion,
			"cc_gateway_routes":                     "native_messages,native_count_tokens",
			"cc_gateway_egress_bucket_enabled":      "true",
			"cc_gateway_egress_bucket":              "bucket-a",
		},
	}
	svc := &GatewayService{
		cfg:             ccGatewayTestConfig(PlatformAnthropic),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}

	use, err := svc.selectCCGatewayAnthropicRoute(account, ccGatewayRouteNativeMessages)

	require.False(t, use)
	require.ErrorContains(t, err, "account ref")
}

func TestGatewayService_CCGatewayAnthropicSessionMappingIsolatedAcrossUsers(t *testing.T) {
	body := []byte(`{"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"11111111-2222-4333-8444-555555555555\"}"},"model":"claude-3-7-sonnet-20250219","messages":[{"role":"user","content":"hi"}]}`)
	account := &Account{
		ID:       42,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "selected-oauth-token",
			"email":        "user@example.com",
		},
		Extra: map[string]any{
			"account_uuid":                     "acct-uuid",
			"organization_uuid":                "org-uuid",
			"cc_gateway_enabled":               "true",
			"cc_gateway_canary_only":           "false",
			"cc_gateway_policy_version":        ccGatewayAnthropicPolicyVersion,
			"cc_gateway_routes":                "native_messages,native_count_tokens,chat_completions,responses",
			"cc_gateway_egress_bucket_enabled": "true",
			"cc_gateway_egress_bucket":         "bucket-a",
			"billing_cch_mode":                 "sign",
		},
	}
	svc := &GatewayService{
		cfg:             ccGatewayTestConfig(PlatformAnthropic),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}

	build := func(scope string) string {
		t.Helper()
		c := ccGatewayTestContext("/v1/messages")
		c.Request.Header.Set("X-Claude-Code-Session-Id", "11111111-2222-4333-8444-555555555555")
		req, _, err := svc.buildUpstreamRequest(WithClaudeCodeSessionUserScope(context.Background(), scope), c, account, body, "selected-oauth-token", "oauth", "claude-3-7-sonnet-20250219", true, false, false)
		require.NoError(t, err)
		return getHeaderRaw(req.Header, "X-Claude-Code-Session-Id")
	}

	sessionA1 := build("user:alpha")
	sessionA2 := build("user:alpha")
	sessionB := build("user:beta")

	require.Equal(t, sessionA1, sessionA2)
	require.NotEqual(t, sessionA1, sessionB)
}

func TestGatewayService_CCGatewayCountTokensMapsServerSessionIntoMetadataAndHeader(t *testing.T) {
	account := &Account{
		ID:       43,
		Platform: PlatformAnthropic,
		Type:     AccountTypeSetupToken,
		Extra: map[string]any{
			"cc_gateway_enabled":               "true",
			"cc_gateway_canary_only":           "false",
			"cc_gateway_policy_version":        ccGatewayAnthropicPolicyVersion,
			"cc_gateway_routes":                "native_messages,native_count_tokens,chat_completions,responses",
			"cc_gateway_egress_bucket_enabled": "true",
			"cc_gateway_egress_bucket":         "bucket-a",
			"billing_cch_mode":                 "sign",
		},
	}
	svc := &GatewayService{
		cfg:             ccGatewayTestConfig(PlatformAnthropic),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}

	bodyMap := map[string]any{
		"model": "claude-3-7-sonnet-20250219",
		"metadata": map[string]any{
			"user_id": `{"device_id":"client-device","session_id":"11111111-2222-4333-8444-555555555555"}`,
		},
	}
	body, err := json.Marshal(bodyMap)
	require.NoError(t, err)

	c := ccGatewayTestContext("/v1/messages/count_tokens")
	c.Request.Header.Set("X-Claude-Code-Session-Id", "11111111-2222-4333-8444-555555555555")

	req, _, err := svc.buildCountTokensRequest(WithClaudeCodeSessionUserScope(context.Background(), "user:alpha"), c, account, body, "setup-token", "oauth", "claude-3-7-sonnet-20250219", false, false)
	require.NoError(t, err)

	mappedSessionID := getHeaderRaw(req.Header, "X-Claude-Code-Session-Id")
	require.Regexp(t, regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`), mappedSessionID)
	require.NotEqual(t, "11111111-2222-4333-8444-555555555555", mappedSessionID)

	rewrittenBody := readRequestBody(t, req)
	parsedUserID := ParseMetadataUserID(gjson.Get(rewrittenBody, "metadata.user_id").String())
	require.NotNil(t, parsedUserID)
	require.Equal(t, mappedSessionID, parsedUserID.SessionID)
	require.NotContains(t, rewrittenBody, "11111111-2222-4333-8444-555555555555")
}

func TestGatewayService_CCGatewayAnthropicOAuthBuildsTransparentRequest(t *testing.T) {
	body := []byte(`{"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}"},"model":"claude-3-7-sonnet-20250219","messages":[{"role":"user","content":"hi"}]}`)
	account := &Account{
		ID:       42,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "selected-oauth-token",
			"email":        "user@example.com",
		},
		Extra: map[string]any{
			"account_uuid":                     "acct-uuid",
			"organization_uuid":                "org-uuid",
			"cc_gateway_enabled":               "true",
			"cc_gateway_canary_only":           "false",
			"cc_gateway_policy_version":        ccGatewayAnthropicPolicyVersion,
			"cc_gateway_routes":                "native_messages,native_count_tokens,chat_completions,responses",
			"cc_gateway_egress_bucket_enabled": "true",
			"cc_gateway_egress_bucket":         "bucket-a",
			"billing_cch_mode":                 "sign",
		},
	}
	svc := &GatewayService{
		cfg:             ccGatewayTestConfig(PlatformAnthropic),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}

	ctx := SetClaudeCodeVersion(context.Background(), ccGatewayAnthropicPolicyVersion)
	req, _, err := svc.buildUpstreamRequest(ctx, ccGatewayTestContext("/v1/messages"), account, body, "selected-oauth-token", "oauth", "claude-3-7-sonnet-20250219", true, false, false)
	require.NoError(t, err)

	require.Equal(t, "http://cc-gateway:8443/v1/messages?beta=true", req.URL.String())
	require.Equal(t, "Bearer selected-oauth-token", getHeaderRaw(req.Header, "authorization"))
	require.Equal(t, "", getHeaderRaw(req.Header, "x-api-key"))
	require.Equal(t, "ccg-token", getHeaderRaw(req.Header, "x-cc-gateway-token"))
	require.Equal(t, "42", getHeaderRaw(req.Header, "x-cc-account-id"))
	require.Equal(t, "anthropic", getHeaderRaw(req.Header, "x-cc-provider"))
	require.Equal(t, "oauth", getHeaderRaw(req.Header, "x-cc-token-type"))
	require.Equal(t, ccGatewayAnthropicPolicyVersion, getHeaderRaw(req.Header, "x-cc-policy-version"))
	require.Empty(t, getHeaderRaw(req.Header, "Accept-Encoding"))
	require.Empty(t, getHeaderRaw(req.Header, "x-cc-account-email"))
	require.Empty(t, getHeaderRaw(req.Header, "x-cc-account-uuid"))
	require.Empty(t, getHeaderRaw(req.Header, "x-cc-organization-uuid"))
	require.Equal(t, "bucket-a", getHeaderRaw(req.Header, "x-cc-egress-bucket"))
	require.Empty(t, getHeaderRaw(req.Header, ccGatewayFormalPoolContextHeader))
	require.Empty(t, getHeaderRaw(req.Header, ccGatewayFormalPoolSignatureHeader))
	require.Empty(t, getHeaderRaw(req.Header, ccGatewayTrustedPersonaHeader))
	require.Equal(t, "", getHeaderRaw(req.Header, "x-stainless-os"))
	require.NotContains(t, readRequestBody(t, req), "rewritten-device-id")
}

func TestGatewayService_CCGatewayAnthropicSetupTokenIgnoresClientContext1MHints(t *testing.T) {
	body := []byte(`{"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}"},"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}]}`)
	account := &Account{
		ID:       43,
		Platform: PlatformAnthropic,
		Type:     AccountTypeSetupToken,
		Extra: map[string]any{
			"cc_gateway_enabled":               "true",
			"cc_gateway_canary_only":           "false",
			"cc_gateway_policy_version":        ccGatewayAnthropicPolicyVersion,
			"cc_gateway_routes":                "native_messages,native_count_tokens,chat_completions,responses",
			"cc_gateway_egress_bucket_enabled": "true",
			"cc_gateway_egress_bucket":         "bucket-a",
		},
	}
	c := ccGatewayTestContext("/v1/messages")
	c.Request.Header.Set("anthropic-beta", "claude-code-20250219,context-1m-2025-08-07,interleaved-thinking-2025-05-14")
	c.Request.Header.Set(ccGatewayContext1MHeader, "true")
	req, _, err := (&GatewayService{cfg: ccGatewayTestConfig(PlatformAnthropic)}).
		buildUpstreamRequest(context.Background(), c, account, body, "setup-token", "oauth", "claude-sonnet-4-6", true, false, false)
	require.NoError(t, err)

	require.Empty(t, getHeaderRaw(req.Header, ccGatewayContext1MHeader))
	require.Empty(t, getHeaderRaw(req.Header, ccGatewayInternalControlHeader))
}

func TestGatewayService_CCGatewayAnthropicSetupTokenPlainRequestOmitsContext1MHeader(t *testing.T) {
	body := []byte(`{"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}"},"model":"claude-haiku-4-5-20251001","messages":[{"role":"user","content":"hi"}]}`)
	account := &Account{
		ID:       43,
		Platform: PlatformAnthropic,
		Type:     AccountTypeSetupToken,
		Extra: map[string]any{
			"cc_gateway_enabled":               "true",
			"cc_gateway_canary_only":           "false",
			"cc_gateway_policy_version":        ccGatewayAnthropicPolicyVersion,
			"cc_gateway_routes":                "native_messages,native_count_tokens,chat_completions,responses",
			"cc_gateway_egress_bucket_enabled": "true",
			"cc_gateway_egress_bucket":         "bucket-a",
		},
	}
	c := ccGatewayTestContext("/v1/messages")
	req, _, err := (&GatewayService{cfg: ccGatewayTestConfig(PlatformAnthropic)}).
		buildUpstreamRequest(context.Background(), c, account, body, "setup-token", "oauth", "claude-haiku-4-5-20251001", true, false, false)
	require.NoError(t, err)

	require.Empty(t, getHeaderRaw(req.Header, ccGatewayContext1MHeader))
}

func TestGatewayService_CCGatewayAnthropicSetupTokenCountTokensBuildsTransparentRequest(t *testing.T) {
	account := &Account{
		ID:       43,
		Platform: PlatformAnthropic,
		Type:     AccountTypeSetupToken,
		Extra: map[string]any{
			"cc_gateway_enabled":               "true",
			"cc_gateway_canary_only":           "false",
			"cc_gateway_policy_version":        ccGatewayAnthropicPolicyVersion,
			"cc_gateway_routes":                "native_messages,native_count_tokens,chat_completions,responses",
			"cc_gateway_egress_bucket_enabled": "true",
			"cc_gateway_egress_bucket":         "bucket-a",
		},
	}
	svc := &GatewayService{
		cfg:             ccGatewayTestConfig(PlatformAnthropic),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}

	c := ccGatewayTestContext("/v1/messages/count_tokens")
	c.Request.Header.Set("x-app", "fake-cli")
	c.Request.Header.Set("x-claude-code-session-id", "client-session")
	c.Request.Header.Set("x-sub2api-persona-trusted", "client-trust")
	req, _, err := svc.buildCountTokensRequest(context.Background(), c, account, []byte(`{"model":"claude-3-7-sonnet-20250219"}`), "setup-token", "oauth", "claude-3-7-sonnet-20250219", false, false)
	require.NoError(t, err)

	require.Equal(t, "http://cc-gateway:8443/v1/messages/count_tokens?beta=true", req.URL.String())
	require.Equal(t, "Bearer setup-token", getHeaderRaw(req.Header, "authorization"))
	require.Equal(t, "oauth", getHeaderRaw(req.Header, "x-cc-token-type"))
	require.Equal(t, "43", getHeaderRaw(req.Header, "x-cc-account-id"))
	require.Equal(t, ccGatewayAnthropicPolicyVersion, getHeaderRaw(req.Header, "x-cc-policy-version"))
	require.Contains(t, getHeaderRaw(req.Header, "anthropic-beta"), "token-counting")
	require.NotEqual(t, "client-beta", getHeaderRaw(req.Header, "anthropic-beta"))
	require.Empty(t, getHeaderRaw(req.Header, "Accept-Encoding"))
	require.Empty(t, getHeaderRaw(req.Header, "x-app"))
	require.Empty(t, getHeaderRaw(req.Header, "x-sub2api-persona-trusted"))
	require.Equal(t, "", getHeaderRaw(req.Header, "x-stainless-os"))
}

func TestGatewayService_CCGatewayAnthropicAPIKeyPassthroughBuildsTransparentRequest(t *testing.T) {
	body := []byte(`{"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}"},"model":"x","messages":[{"role":"user","content":"hi"}]}`)
	account := newAnthropicAPIKeyAccountForTest()
	account.Credentials["base_url"] = "https://must-not-be-used.example"
	account.Extra["cc_gateway_egress_bucket"] = "bucket-a"
	account.Extra["cc_gateway_enabled"] = "true"
	account.Extra["cc_gateway_canary_only"] = "false"
	account.Extra["cc_gateway_policy_version"] = ccGatewayAnthropicPolicyVersion
	account.Extra["cc_gateway_routes"] = "native_messages,native_count_tokens"
	account.Extra["cc_gateway_egress_bucket_enabled"] = "true"
	applyCCGatewayAPIKeyFormalPoolContextForTest(account)

	c := ccGatewayTestContext("/v1/messages")
	c.Request.Header.Set("x-app", "fake-cli")
	c.Request.Header.Set("x-claude-code-session-id", "client-session")
	c.Request.Header.Set("x-stainless-runtime", "fake-runtime")
	c.Request.Header.Set("x-sub2api-persona-trusted", "client-trust")
	req, _, err := (&GatewayService{
		cfg:             ccGatewayTestConfig(PlatformAnthropic),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}).buildUpstreamRequestAnthropicAPIKeyPassthrough(context.Background(), c, account, body, "selected-api-key")
	require.NoError(t, err)

	require.Equal(t, "http://cc-gateway:8443/v1/messages?beta=true", req.URL.String())
	require.Equal(t, "selected-api-key", getHeaderRaw(req.Header, "x-api-key"))
	require.Equal(t, "", getHeaderRaw(req.Header, "authorization"))
	require.Equal(t, "ccg-token", getHeaderRaw(req.Header, "x-cc-gateway-token"))
	require.Equal(t, "hmac-sha256:"+strings.Repeat("d", 64), getHeaderRaw(req.Header, "x-cc-account-id"))
	require.Equal(t, "anthropic", getHeaderRaw(req.Header, "x-cc-provider"))
	require.Equal(t, "apikey", getHeaderRaw(req.Header, "x-cc-token-type"))
	require.Equal(t, ccGatewayAnthropicPolicyVersion, getHeaderRaw(req.Header, "x-cc-policy-version"))
	require.Equal(t, "bucket-a", getHeaderRaw(req.Header, "x-cc-egress-bucket"))
	require.Empty(t, getHeaderRaw(req.Header, "anthropic-beta"))
	require.Empty(t, getHeaderRaw(req.Header, "x-app"))
	require.NotEqual(t, "client-session", getHeaderRaw(req.Header, "x-claude-code-session-id"))
	require.Empty(t, getHeaderRaw(req.Header, "x-stainless-runtime"))
	require.Empty(t, getHeaderRaw(req.Header, "x-sub2api-persona-trusted"))
	require.NotEmpty(t, getHeaderRaw(req.Header, ccGatewayFormalPoolContextHeader))
	require.NotEmpty(t, getHeaderRaw(req.Header, ccGatewayFormalPoolSignatureHeader))
}

func TestGatewayService_CCGatewayAnthropicAPIKeyPassthroughFailsClosedWhenFormalPoolContextIncomplete(t *testing.T) {
	body := []byte(`{"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}"},"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}]}`)
	account := newAnthropicAPIKeyAccountForTest()
	account.Credentials["base_url"] = "https://must-not-be-used.example"
	account.Extra["cc_gateway_enabled"] = "true"
	account.Extra["cc_gateway_canary_only"] = "false"
	account.Extra["cc_gateway_policy_version"] = ccGatewayAnthropicPolicyVersion
	account.Extra["cc_gateway_routes"] = "native_messages,native_count_tokens"
	account.Extra["cc_gateway_egress_bucket_enabled"] = "true"
	account.Extra["cc_gateway_egress_bucket"] = "bucket-a"
	account.Extra["cc_gateway_account_ref"] = "hmac-sha256:" + strings.Repeat("d", 64)
	account.Extra[ccGatewayExtraProxyIdentityRef] = "opaque:proxy-ref:v1:bucket-a"
	account.Extra[ccGatewayExtraPersonaProfile] = ccGatewayHealthcheckNon1MProfile

	req, _, err := (&GatewayService{
		cfg:             ccGatewayTestConfig(PlatformAnthropic),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}).buildUpstreamRequestAnthropicAPIKeyPassthrough(context.Background(), ccGatewayTestContext("/v1/messages"), account, body, "test-selected-key")
	require.Error(t, err)
	require.Nil(t, req)
	require.Contains(t, err.Error(), "formal-pool attestation context is incomplete")
	require.Contains(t, err.Error(), "credential_ref")
	require.NotContains(t, err.Error(), "test-selected-key")
}

func TestGatewayService_CCGatewayAnthropicAPIKeyPassthroughBuildsAttestedFormalPoolContext(t *testing.T) {
	body := []byte(`{"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}"},"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}]}`)
	account := newAnthropicAPIKeyAccountForTest()
	account.Credentials["base_url"] = "https://must-not-be-used.example"
	account.Extra["cc_gateway_enabled"] = "true"
	account.Extra["cc_gateway_canary_only"] = "false"
	account.Extra["cc_gateway_policy_version"] = ccGatewayAnthropicPolicyVersion
	account.Extra["cc_gateway_routes"] = "native_messages,native_count_tokens"
	account.Extra["cc_gateway_egress_bucket_enabled"] = "true"
	account.Extra["cc_gateway_egress_bucket"] = "bucket-a"
	account.Extra["cc_gateway_account_ref"] = "hmac-sha256:" + strings.Repeat("d", 64)
	account.Extra[ccGatewayExtraCredentialRef] = "opaque:credential-ref:v1:apikey-cred-a"
	account.Extra[ccGatewayExtraProxyIdentityRef] = "opaque:proxy-ref:v1:bucket-a"
	account.Extra[ccGatewayExtraPersonaProfile] = ccGatewayHealthcheckNon1MProfile

	c := ccGatewayTestContext("/v1/messages")
	c.Request.Header.Set("x-cc-formal-pool-context", "client-forged-context")
	c.Request.Header.Set("x-cc-formal-pool-signature", "client-forged-signature")
	c.Request.Header.Set("x-cc-credential-ref", "opaque:credential-ref:v1:client-forged")
	req, _, err := (&GatewayService{
		cfg:             ccGatewayTestConfig(PlatformAnthropic),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}).buildUpstreamRequestAnthropicAPIKeyPassthrough(context.Background(), c, account, body, "selected-api-key")
	require.NoError(t, err)

	ctx := decodeCCGatewayFormalPoolContextForTest(t, req)
	require.Equal(t, "POST", ctx["method"])
	require.Equal(t, "messages", ctx["route_class"])
	require.Equal(t, "/v1/messages", ctx["path"])
	require.Equal(t, "hmac-sha256:"+strings.Repeat("d", 64), ctx["account_id"])
	require.Equal(t, "apikey", ctx["token_type"])
	require.Equal(t, "opaque:credential-ref:v1:apikey-cred-a", ctx["credential_ref"])
	require.Equal(t, "bucket-a", ctx["egress_bucket"])
	require.Equal(t, "opaque:proxy-ref:v1:bucket-a", ctx["proxy_identity_ref"])
	require.Equal(t, ccGatewayAnthropicPolicyVersion, ctx["policy_version"])
	require.Equal(t, ccGatewayHealthcheckNon1MProfile, ctx["persona_profile"])
	require.NotEmpty(t, ctx["session_id"])
	requireValidCCGatewayFormalPoolSignatureForTest(t, req, "formal-pool-attestation-secret-test")
	require.Equal(t, "opaque:credential-ref:v1:apikey-cred-a", getHeaderRaw(req.Header, "x-cc-credential-ref"))
}

func TestGatewayService_CCGatewayAPIKeyPassthroughObservedVersionCapturedBeforeSanitize(t *testing.T) {
	body := []byte(`{"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}"},"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}]}`)
	account := newAnthropicAPIKeyAccountForTest()
	account.Credentials["base_url"] = "https://must-not-be-used.example"
	account.Extra["cc_gateway_enabled"] = "true"
	account.Extra["cc_gateway_canary_only"] = "false"
	account.Extra["cc_gateway_policy_version"] = ccGatewayAnthropicPolicyVersion
	account.Extra["cc_gateway_routes"] = "native_messages,native_count_tokens"
	account.Extra["cc_gateway_egress_bucket_enabled"] = "true"
	account.Extra["cc_gateway_egress_bucket"] = "bucket-a"
	applyCCGatewayAPIKeyFormalPoolContextForTest(account)

	c := ccGatewayTestContext("/v1/messages")
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.195 (external, sdk-cli)")
	req, _, err := (&GatewayService{
		cfg:             ccGatewayTestConfig(PlatformAnthropic),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}).buildUpstreamRequestAnthropicAPIKeyPassthrough(context.Background(), c, account, body, "selected-api-key")
	require.NoError(t, err)

	ctx := decodeCCGatewayFormalPoolContextForTest(t, req)
	observed := ctx["observed_client_profile"].(map[string]any)
	require.Equal(t, "2.1.195", observed["cli_version_bucket"])
	require.Equal(t, "strip_attribution", ctx["trusted_egress_profile_ref"])
	require.Equal(t, "strip", ctx["billing_shape_policy"])
}

func TestGatewayService_CCGatewayAPIKeyCountTokensObservedVersionCapturedBeforeSanitize(t *testing.T) {
	body := []byte(`{"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}"},"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}]}`)
	account := newAnthropicAPIKeyAccountForTest()
	account.Credentials["base_url"] = "https://must-not-be-used.example"
	account.Extra["cc_gateway_enabled"] = "true"
	account.Extra["cc_gateway_canary_only"] = "false"
	account.Extra["cc_gateway_policy_version"] = ccGatewayAnthropicPolicyVersion
	account.Extra["cc_gateway_routes"] = "native_messages,native_count_tokens"
	account.Extra["cc_gateway_egress_bucket_enabled"] = "true"
	account.Extra["cc_gateway_egress_bucket"] = "bucket-a"
	applyCCGatewayAPIKeyFormalPoolContextForTest(account)

	c := ccGatewayTestContext("/v1/messages/count_tokens")
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.195 (external, sdk-cli)")
	req, err := (&GatewayService{
		cfg:             ccGatewayTestConfig(PlatformAnthropic),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}).buildCountTokensRequestAnthropicAPIKeyPassthrough(context.Background(), c, account, body, "selected-api-key")
	require.NoError(t, err)

	ctx := decodeCCGatewayFormalPoolContextForTest(t, req)
	observed := ctx["observed_client_profile"].(map[string]any)
	require.Equal(t, "2.1.195", observed["cli_version_bucket"])
	require.Equal(t, "count_tokens", ctx["route_class"])
	require.Equal(t, "strip_attribution", ctx["trusted_egress_profile_ref"])
}

func TestGatewayService_CCGatewayAnthropicAPIKeyPassthroughRejectsLegacyEgressFallback(t *testing.T) {
	account := newAnthropicAPIKeyAccountForTest()
	account.Extra["cc_gateway_egress_bucket"] = ""
	account.Extra["openai_gateway_egress_bucket"] = "legacy-bucket"
	account.Extra["cc_gateway_enabled"] = "true"
	account.Extra["cc_gateway_canary_only"] = "false"
	account.Extra["cc_gateway_policy_version"] = ccGatewayAnthropicPolicyVersion
	account.Extra["cc_gateway_routes"] = "native_messages,native_count_tokens"
	account.Extra["cc_gateway_egress_bucket_enabled"] = "true"

	_, _, err := (&GatewayService{cfg: ccGatewayTestConfig(PlatformAnthropic)}).
		buildUpstreamRequestAnthropicAPIKeyPassthrough(context.Background(), ccGatewayTestContext("/v1/messages"), account, []byte(`{"model":"x"}`), "selected-api-key")
	require.Error(t, err)
	require.Contains(t, err.Error(), "egress bucket")
}

func TestGatewayService_CCGatewayAnthropicAPIKeyPassthroughCountTokensBuildsTransparentRequest(t *testing.T) {
	account := newAnthropicAPIKeyAccountForTest()
	account.Extra["cc_gateway_enabled"] = "true"
	account.Extra["cc_gateway_canary_only"] = "false"
	account.Extra["cc_gateway_policy_version"] = ccGatewayAnthropicPolicyVersion
	account.Extra["cc_gateway_routes"] = "native_messages,native_count_tokens"
	account.Extra["cc_gateway_egress_bucket_enabled"] = "true"
	account.Extra["cc_gateway_egress_bucket"] = "bucket-a"
	applyCCGatewayAPIKeyFormalPoolContextForTest(account)

	c := ccGatewayTestContext("/v1/messages/count_tokens")
	c.Request.Header.Set("x-app", "fake-cli")
	c.Request.Header.Set("x-claude-code-session-id", "client-session")
	c.Request.Header.Set("x-sub2api-persona-trusted", "client-trust")
	req, err := (&GatewayService{
		cfg:             ccGatewayTestConfig(PlatformAnthropic),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}).buildCountTokensRequestAnthropicAPIKeyPassthrough(context.Background(), c, account, []byte(`{"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}"},"model":"x"}`), "selected-api-key")
	require.NoError(t, err)

	require.Equal(t, "http://cc-gateway:8443/v1/messages/count_tokens?beta=true", req.URL.String())
	require.Equal(t, "selected-api-key", getHeaderRaw(req.Header, "x-api-key"))
	require.Equal(t, "", getHeaderRaw(req.Header, "authorization"))
	require.Equal(t, "apikey", getHeaderRaw(req.Header, "x-cc-token-type"))
	require.Equal(t, "hmac-sha256:"+strings.Repeat("d", 64), getHeaderRaw(req.Header, "x-cc-account-id"))
	require.Equal(t, ccGatewayAnthropicPolicyVersion, getHeaderRaw(req.Header, "x-cc-policy-version"))
	require.Empty(t, getHeaderRaw(req.Header, "anthropic-beta"))
	require.Empty(t, getHeaderRaw(req.Header, "x-app"))
	require.NotEqual(t, "client-session", getHeaderRaw(req.Header, "x-claude-code-session-id"))
	require.Empty(t, getHeaderRaw(req.Header, "x-sub2api-persona-trusted"))
	require.NotEmpty(t, getHeaderRaw(req.Header, ccGatewayFormalPoolContextHeader))
	require.NotEmpty(t, getHeaderRaw(req.Header, ccGatewayFormalPoolSignatureHeader))
}

func TestGatewayService_CCGatewayClaudePlatformAWSBuildsAttestedContextFromServerState(t *testing.T) {
	useClaudeCodeSessionBoundaryLedgerFileForTest(t)
	gin.SetMode(gin.TestMode)
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

	clientWorkspace := syntheticAWSWorkspaceID(9)
	clientAPIKey := "synthetic-client-forged-api-key-cp4"
	body := []byte(`{"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"11111111-2222-4333-8444-555555555555\"}"},"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}]}`)
	c := claudePlatformAWSRequestTestContext()
	c.Request.Header.Set("Anthropic-Workspace-Id", clientWorkspace)
	c.Request.Header.Set("X-Api-Key", clientAPIKey)
	c.Request.Header.Set("Authorization", "Bearer synthetic-client-forged-bearer-cp4")
	c.Request.Header.Set("X-Amz-Security-Token", "synthetic-client-forged-amz-cp4")
	c.Request.Header.Set("Anthropic-Beta", "client-forged-beta-cp4")
	c.Request.Header.Set("X-Cc-Formal-Pool-Context", "client-forged-context-cp4")
	c.Request.Header.Set("X-Sub2api-Profile", "client-forged-profile-cp4")

	req, _, err := (&GatewayService{cfg: cfg, identityService: NewIdentityService(ccGatewayIdentityCache{})}).buildUpstreamRequest(
		context.Background(), c, account, body,
		account.GetCredential("api_key"), "claude_platform_aws", "claude-sonnet-4-6", false, false, false,
	)
	require.NoError(t, err)
	require.NotNil(t, req)
	require.Equal(t, "http://cc-gateway:8443/v1/messages", req.URL.String())
	require.Equal(t, "", req.URL.RawQuery)

	ctx := decodeCCGatewayFormalPoolContextForTest(t, req)
	require.Equal(t, claudePlatformAWSProviderKind, ctx["provider_kind"])
	require.Equal(t, ClaudePlatformAWSAuthProfileXAPIKey, ctx["upstream_auth_scheme"])
	require.Equal(t, "us-east-1", ctx["aws_region"])
	require.Equal(t, "aws-external-anthropic.us-east-1.api.aws", ctx["upstream_host"])
	require.Equal(t, "/v1/messages", ctx["allowed_upstream_path"])
	require.Equal(t, validation.EndpointRef, ctx["upstream_endpoint_ref"])
	require.Equal(t, validation.WorkspaceRef, ctx["workspace_ref"])
	require.Equal(t, validation.WorkspaceBindingHMAC, ctx["workspace_binding_hmac"])
	require.Equal(t, validation.CredentialRef, ctx["credential_ref"])
	require.Equal(t, validation.CredentialBindingHMAC, ctx["credential_binding_hmac"])
	require.Equal(t, "egress:synthetic-cpaws", ctx["egress_bucket"])
	require.Equal(t, validation.ProxyIdentityRef, ctx["proxy_identity_ref"])
	require.Equal(t, "request-shape:claude-platform-aws-v1-strip", ctx["request_shape_profile_ref"])
	require.Equal(t, "cache-profile:claude-platform-aws-v1-strip", ctx["cache_parity_profile_ref"])
	require.Equal(t, "beta-policy:claude-platform-aws-v1-strip", ctx["beta_policy_ref"])

	encodedContext, marshalErr := json.Marshal(ctx)
	require.NoError(t, marshalErr)
	for _, forbidden := range []string{clientWorkspace, clientAPIKey, account.GetCredential("api_key"), account.GetCredential("anthropic_workspace_id"), "synthetic-client-forged-amz-cp4", "client-forged-beta-cp4", "client-forged-context-cp4", "client-forged-profile-cp4"} {
		require.NotContains(t, string(encodedContext), forbidden)
	}
	headerDump, dumpErr := json.Marshal(req.Header)
	require.NoError(t, dumpErr)
	for _, forbidden := range []string{clientWorkspace, clientAPIKey, account.GetCredential("anthropic_workspace_id"), "synthetic-client-forged-amz-cp4", "client-forged-beta-cp4", "client-forged-context-cp4", "client-forged-profile-cp4"} {
		require.NotContains(t, string(headerDump), forbidden)
	}
	require.Empty(t, getHeaderRaw(req.Header, "anthropic-workspace-id"))
	require.Empty(t, getHeaderRaw(req.Header, "anthropic-beta"))
	require.Empty(t, getHeaderRaw(req.Header, "x-amz-security-token"))
	require.Empty(t, getHeaderRaw(req.Header, "x-sub2api-profile"))
	require.NotEqual(t, "client-forged-context-cp4", getHeaderRaw(req.Header, ccGatewayFormalPoolContextHeader))
	requireValidCCGatewayFormalPoolSignatureForTest(t, req, "formal-pool-attestation-secret-test")
}

func TestCCGatewayClaudePlatformAWSAttestationFailsClosedWhenAuthorityFieldsMissing(t *testing.T) {
	useClaudeCodeSessionBoundaryLedgerFileForTest(t)
	account := claudePlatformAWSRequestTestAccount(t, ClaudePlatformAWSAuthProfileXAPIKey, true)
	markClaudePlatformAWSFormalPoolForRequestTest(t, account)
	account.Extra[ccGatewayExtraPersonaProfile] = ccGatewayDefaultPersonaProfile
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	req.Header.Set("X-Claude-Code-Session-Id", "11111111-2222-4333-8444-555555555555")
	require.NoError(t, applyCCGatewayAnthropicHeaders(req, ccGatewayTestConfig(PlatformAnthropic), account, "apikey"))

	for name, key := range map[string]string{
		"workspace_ref":         ClaudePlatformAWSExtraWorkspaceRef,
		"workspace_binding":     ClaudePlatformAWSExtraWorkspaceBindingHMAC,
		"endpoint_ref":          ClaudePlatformAWSExtraEndpointRef,
		"region":                ClaudePlatformAWSExtraRegion,
		"auth_scheme":           ClaudePlatformAWSExtraAuthScheme,
		"request_shape_profile": ClaudePlatformAWSExtraRequestShapeProfileRef,
		"cache_parity_profile":  ClaudePlatformAWSExtraCacheParityProfileRef,
		"beta_policy":           ClaudePlatformAWSExtraBetaPolicyRef,
	} {
		t.Run(name, func(t *testing.T) {
			attempt := *account
			attempt.Extra = cloneCredentials(account.Extra)
			delete(attempt.Extra, key)
			err := applyCCGatewayFormalPoolAttestation(req.Clone(context.Background()), ccGatewayTestConfig(PlatformAnthropic), &attempt)
			require.Error(t, err)
			require.Contains(t, err.Error(), name)
			require.NotContains(t, err.Error(), attempt.GetCredential("api_key"))
			require.NotContains(t, err.Error(), attempt.GetCredential("anthropic_workspace_id"))
		})
	}
}

func decodeCCGatewayFormalPoolContextForTest(t *testing.T, req *http.Request) map[string]any {
	t.Helper()
	encoded := getHeaderRaw(req.Header, ccGatewayFormalPoolContextHeader)
	require.NotEmpty(t, encoded)
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	require.NoError(t, err)
	var ctx map[string]any
	require.NoError(t, json.Unmarshal(raw, &ctx))
	return ctx
}

func requireValidCCGatewayFormalPoolSignatureForTest(t *testing.T, req *http.Request, secret string) {
	t.Helper()
	encoded := getHeaderRaw(req.Header, ccGatewayFormalPoolContextHeader)
	require.NotEmpty(t, encoded)
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	require.NoError(t, err)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(raw)
	expected := "hmac-sha256:" + hex.EncodeToString(mac.Sum(nil))
	require.Equal(t, expected, getHeaderRaw(req.Header, ccGatewayFormalPoolSignatureHeader))
}

func TestGatewayService_CCGatewayAnthropicOAuthIgnoresForgedInternalHeaders(t *testing.T) {
	useClaudeCodeSessionBoundaryLedgerFileForTest(t)
	const wantPersonaProfile = "claude_code_2_1_179_native_degraded"
	body := []byte(`{"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}"},"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}]}`)
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	account.Extra["cc_gateway_enabled"] = "true"
	account.Extra["cc_gateway_canary_only"] = "false"
	account.Extra["cc_gateway_policy_version"] = ccGatewayAnthropicPolicyVersion
	account.Extra["cc_gateway_routes"] = "native_messages,native_count_tokens,chat_completions,responses"
	account.Extra["cc_gateway_egress_bucket_enabled"] = "true"
	account.Extra["cc_gateway_egress_bucket"] = "bucket-a"
	account.Extra["cc_gateway_account_ref"] = "hmac-sha256:" + strings.Repeat("c", 64)
	formalPoolApplyCompleteSchedulingEvidenceForTest(account)
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction
	account.Extra[FormalPoolExtraPoolProfileEffective] = PoolProfileNormal

	c := ccGatewayTestContext("/v1/messages")
	c.Request.Header.Set("anthropic-beta", "claude-code-20250219,context-1m-2025-08-07")
	c.Request.Header.Set("x-sub2api-context-1m", "false")
	c.Request.Header.Set("x-sub2api-healthcheck-persona", "client-forged-healthcheck")
	c.Request.Header.Set("x-sub2api-persona-trusted", "client-forged-trust")
	c.Request.Header.Set("x-cc-account-id", "999999")
	c.Request.Header.Set("x-cc-provider", "forged")
	c.Request.Header.Set("x-cc-token-type", "forged")
	c.Request.Header.Set("x-cc-egress-bucket", "forged-bucket")
	c.Request.Header.Set("x-cc-policy-version", "0.0.0")

	req, _, err := (&GatewayService{
		cfg:             ccGatewayTestConfig(PlatformAnthropic),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}).buildUpstreamRequest(context.Background(), c, account, body, "oauth-token", "oauth", "claude-sonnet-4-6", true, false, false)
	require.NoError(t, err)

	require.Equal(t, "hmac-sha256:"+strings.Repeat("b", 64), getHeaderRaw(req.Header, "x-cc-account-id"))
	require.Equal(t, "anthropic", getHeaderRaw(req.Header, "x-cc-provider"))
	require.Equal(t, "oauth", getHeaderRaw(req.Header, "x-cc-token-type"))
	require.Equal(t, "claude-safe-bucket", getHeaderRaw(req.Header, "x-cc-egress-bucket"))
	require.Equal(t, ccGatewayAnthropicPolicyVersion, getHeaderRaw(req.Header, "x-cc-policy-version"))
	require.Empty(t, getHeaderRaw(req.Header, ccGatewayContext1MHeader), "native degraded formal-pool persona must not force context-1m")
	require.Empty(t, getHeaderRaw(req.Header, ccGatewayHealthcheckPersonaHeader))
	require.Empty(t, getHeaderRaw(req.Header, ccGatewayTrustedPersonaHeader))
	ctx := decodeCCGatewayFormalPoolContextForTest(t, req)
	require.Equal(t, ccGatewayAnthropicPolicyVersion, ctx["policy_version"])
	require.Equal(t, wantPersonaProfile, ctx["persona_profile"])
}

func TestGatewayService_CCGatewayFormalPoolAttestationRejectsMissingPersonaProfile(t *testing.T) {
	useClaudeCodeSessionBoundaryLedgerFileForTest(t)
	body := []byte(`{"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}"},"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}]}`)
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	account.Extra["cc_gateway_enabled"] = "true"
	account.Extra["cc_gateway_canary_only"] = "false"
	account.Extra["cc_gateway_policy_version"] = ccGatewayAnthropicPolicyVersion
	account.Extra["cc_gateway_routes"] = "native_messages,native_count_tokens,chat_completions,responses"
	account.Extra["cc_gateway_egress_bucket_enabled"] = "true"
	account.Extra["cc_gateway_egress_bucket"] = "bucket-a"
	formalPoolApplyCompleteSchedulingEvidenceForTest(account)
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction
	account.Extra[FormalPoolExtraPoolProfileEffective] = PoolProfileNormal
	delete(account.Extra, ccGatewayExtraPersonaProfile)

	req, _, err := (&GatewayService{
		cfg:             ccGatewayTestConfig(PlatformAnthropic),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}).buildUpstreamRequest(context.Background(), ccGatewayTestContext("/v1/messages"), account, body, "oauth-token", "oauth", "claude-sonnet-4-6", true, false, false)
	require.Error(t, err)
	require.Nil(t, req)
	require.NotContains(t, err.Error(), PoolProfileNormal)
}

func TestCCGatewayPersonaProfileRequiresExplicitFormalPoolProfile(t *testing.T) {
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction
	account.Extra[FormalPoolExtraPoolProfileEffective] = PoolProfileNormal
	account.Extra[ccGatewayExtraCredentialRef] = "opaque:credential-ref:v1:cred-a"
	account.Extra[ccGatewayExtraProxyIdentityRef] = "opaque:proxy-ref:v1:bucket-a"
	delete(account.Extra, ccGatewayExtraPersonaProfile)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	setHeaderRaw(req.Header, ccGatewayAccountIDHeader, "hmac-sha256:"+strings.Repeat("b", 64))
	setHeaderRaw(req.Header, ccGatewayEgressBucketHeader, "claude-safe-bucket")
	setHeaderRaw(req.Header, ccGatewayPolicyVersionHeader, ccGatewayAnthropicPolicyVersion)
	setHeaderRaw(req.Header, ccGatewayTokenTypeHeader, "oauth")
	setHeaderRaw(req.Header, "X-Claude-Code-Session-Id", "server-session")

	err := applyCCGatewayFormalPoolAttestation(req, ccGatewayTestConfig(PlatformAnthropic), account)
	require.Error(t, err)
	require.Contains(t, err.Error(), "persona_profile")
	require.NotContains(t, err.Error(), PoolProfileNormal)
}

func TestGatewayService_CCGatewayAnthropicOAuthBuildsAttestedFormalPoolContextFromServerState(t *testing.T) {
	useClaudeCodeSessionBoundaryLedgerFileForTest(t)
	const wantPersonaProfile = "claude_code_2_1_179_native_degraded"
	body := []byte(`{"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}"},"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}]}`)
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	account.Extra["cc_gateway_enabled"] = "true"
	account.Extra["cc_gateway_canary_only"] = "false"
	account.Extra["cc_gateway_policy_version"] = ccGatewayAnthropicPolicyVersion
	account.Extra["cc_gateway_routes"] = "native_messages,native_count_tokens,chat_completions,responses"
	account.Extra["cc_gateway_egress_bucket_enabled"] = "true"
	account.Extra["cc_gateway_egress_bucket"] = "bucket-a"
	account.Extra["cc_gateway_account_ref"] = "hmac-sha256:" + strings.Repeat("c", 64)
	account.Extra[FormalPoolExtraPoolProfileEffective] = "claude_code_2_1_175_subscription_1m"
	formalPoolApplyCompleteSchedulingEvidenceForTest(account)
	account.Extra[ccGatewayExtraCredentialRef] = "opaque:credential-ref:v1:cred-a"
	account.Extra[ccGatewayExtraProxyIdentityRef] = "opaque:proxy-ref:v1:bucket-a"
	account.Extra[FormalPoolExtraPoolProfileEffective] = "claude_code_2_1_175_subscription_1m"
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction

	cfg := ccGatewayTestConfig(PlatformAnthropic)
	cfg.Gateway.CCGateway.ContextAttestationSecret = "formal-pool-attestation-secret-test"
	c := ccGatewayTestContext("/v1/messages")
	c.Request.Header.Set("x-cc-formal-pool-context", "client-forged-context")
	c.Request.Header.Set("x-cc-formal-pool-signature", "client-forged-signature")
	c.Request.Header.Set("x-cc-credential-ref", "opaque:credential-ref:v1:client-forged")
	c.Request.Header.Set("x-sub2api-persona-trusted", "client-forged-trust")

	req, _, err := (&GatewayService{
		cfg:             cfg,
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}).buildUpstreamRequest(context.Background(), c, account, body, "oauth-token", "oauth", "claude-sonnet-4-6", true, false, false)
	require.NoError(t, err)

	ctx := decodeCCGatewayFormalPoolContextForTest(t, req)
	require.Equal(t, "POST", ctx["method"])
	require.Equal(t, "messages", ctx["route_class"])
	require.Equal(t, "/v1/messages", ctx["path"])
	require.Equal(t, "hmac-sha256:"+strings.Repeat("b", 64), ctx["account_id"])
	require.Equal(t, "oauth", ctx["token_type"])
	require.Equal(t, "opaque:credential-ref:v1:cred-a", ctx["credential_ref"])
	require.Equal(t, "claude-safe-bucket", ctx["egress_bucket"])
	require.Equal(t, "opaque:proxy-ref:v1:bucket-a", ctx["proxy_identity_ref"])
	require.Equal(t, ccGatewayAnthropicPolicyVersion, ctx["policy_version"])
	require.Equal(t, wantPersonaProfile, ctx["persona_profile"])
	require.NotEmpty(t, ctx["session_id"])
	require.NotEmpty(t, ctx["nonce"])
	require.NotZero(t, ctx["timestamp_ms"])
	require.Equal(t, "opaque:credential-ref:v1:cred-a", getHeaderRaw(req.Header, "x-cc-credential-ref"))
	requireValidCCGatewayFormalPoolSignatureForTest(t, req, "formal-pool-attestation-secret-test")
	require.Empty(t, getHeaderRaw(req.Header, ccGatewayTrustedPersonaHeader))
}

func TestGatewayService_CCGatewayFormalPoolContextCarriesServerSelected2179ProfileRefs(t *testing.T) {
	useClaudeCodeSessionBoundaryLedgerFileForTest(t)
	cchMarker := "cch=" + "12345;"
	body := []byte(`{"model":"claude-sonnet-4-6","stream":true,"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.179.abc; cc_entrypoint=sdk-cli; ` + cchMarker + `"}],"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}"},"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],"tools":[{"name":"Bash","input_schema":{"type":"object"}}],"thinking":{"type":"enabled"},"output_config":{"effort":"medium"},"context_management":{"edits":true}}`)
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
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.179 (external, cli)")
	c.Request.Header.Set("x-cc-trusted-egress-profile-ref", "client-signed-cch")
	c.Request.Header.Set("x-cc-profile-policy-version", "client-policy")
	c.Request.Header.Set("x-cc-billing-shape-policy", "signed_cch")
	c.Request.Header.Set("x-cc-request-shape-profile-ref", "client-shape")
	c.Request.Header.Set("x-cc-cache-parity-profile-ref", "client-cache")
	c.Request.Header.Set("x-cc-observed-client-profile", `{"billing_shape":"client"}`)

	req, _, err := (&GatewayService{
		cfg:             ccGatewayTestConfig(PlatformAnthropic),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}).buildUpstreamRequest(context.Background(), c, account, body, "oauth-token", "oauth", "claude-sonnet-4-6", true, false, false)
	require.NoError(t, err)

	ctx := decodeCCGatewayFormalPoolContextForTest(t, req)
	require.Equal(t, "strip_attribution", ctx["trusted_egress_profile_ref"])
	require.Equal(t, "claude_code_2_1_179_cp1_degraded_v1", ctx["profile_policy_version"])
	require.Equal(t, "strip", ctx["billing_shape_policy"])
	require.Equal(t, "claude_code_2_1_179_messages_streaming_tooldefs_degraded_v1", ctx["request_shape_profile_ref"])
	require.Equal(t, "claude_code_2_1_179_cache_parity_degraded_v1", ctx["cache_parity_profile_ref"])
	observed, ok := ctx["observed_client_profile"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "2.1.179", observed["cli_version_bucket"])
	require.Equal(t, "messages", observed["route_class"])
	require.Equal(t, true, observed["stream"])
	require.Equal(t, "cch_present", observed["billing_shape"])
	require.Equal(t, "sdk-cli", observed["cc_entrypoint_bucket"])
	require.Equal(t, float64(1), observed["billing_block_count"])
	require.Contains(t, observed["top_level_body_keys"], "tools")
	require.NotContains(t, getHeaderRaw(req.Header, "x-cc-trusted-egress-profile-ref"), "client-signed-cch")
	requireValidCCGatewayFormalPoolSignatureForTest(t, req, "formal-pool-attestation-secret-test")
}

func TestGatewayService_CCGatewayFormalPoolLocksCanonicalPolicyWhenContextVersionIsDifferent(t *testing.T) {
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	account.Extra["cc_gateway_enabled"] = "true"
	account.Extra["cc_gateway_canary_only"] = "false"
	account.Extra["cc_gateway_policy_version"] = ccGatewayAnthropicPolicyVersion
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	ctx := WithCCGatewayExplicitCanaryRequest(SetClaudeCodeVersion(context.Background(), "2.1.153"), CCGatewayAnthropicCanaryRequest{
		AccountID:      account.ID,
		Route:          "/v1/messages",
		Method:         http.MethodPost,
		BillingCCHMode: "sign",
		EgressBucket:   "bucket-a",
	})

	applyCCGatewayAnthropicPolicyVersion(ctx, req, account)

	require.Equal(t, ccGatewayAnthropicPolicyVersion, getHeaderRaw(req.Header, ccGatewayPolicyVersionHeader))
	require.Empty(t, getHeaderRaw(req.Header, ccGatewayTrustedPersonaHeader))
}

func TestGatewayService_CCGatewayFormalPoolDefaultStripRemovesDownstreamBillingMaterial(t *testing.T) {
	useClaudeCodeSessionBoundaryLedgerFileForTest(t)
	cchMarker := "cch=" + "12345;"
	body := []byte(`{"model":"claude-sonnet-4-6","stream":true,"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.179.abc; cc_entrypoint=sdk-cli; ` + cchMarker + `"},{"type":"text","text":"safe system note"}],"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}"},"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
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

	req, body, err := (&GatewayService{
		cfg:             ccGatewayTestConfig(PlatformAnthropic),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}).buildUpstreamRequest(context.Background(), ccGatewayTestContext("/v1/messages"), account, body, "oauth-token", "oauth", "claude-sonnet-4-6", true, false, false)
	require.NoError(t, err)

	rewrittenBody := readRequestBody(t, req)
	require.NotContains(t, rewrittenBody, "x-anthropic-billing-header:")
	require.NotContains(t, rewrittenBody, cchMarker)
	require.NotContains(t, string(body), "x-anthropic-billing-header:")
	require.Contains(t, rewrittenBody, "safe system note")
	ctx := decodeCCGatewayFormalPoolContextForTest(t, req)
	require.Equal(t, "strip", ctx["billing_shape_policy"])
	observed := ctx["observed_client_profile"].(map[string]any)
	require.Equal(t, "cch_present", observed["billing_shape"])
}

func TestGatewayService_CCGatewayAPIKeyPassthroughDefaultStripRemovesDownstreamBillingMaterial(t *testing.T) {
	cchMarker := "cch=" + "12345;"
	body := []byte(`{"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}"},"model":"claude-sonnet-4-6","system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.179.abc; cc_entrypoint=sdk-cli; ` + cchMarker + `"}],"messages":[{"role":"user","content":"hi"}]}`)
	account := newAnthropicAPIKeyAccountForTest()
	account.Credentials["base_url"] = "https://must-not-be-used.example"
	account.Extra["cc_gateway_enabled"] = "true"
	account.Extra["cc_gateway_canary_only"] = "false"
	account.Extra["cc_gateway_policy_version"] = ccGatewayAnthropicPolicyVersion
	account.Extra["cc_gateway_routes"] = "native_messages,native_count_tokens"
	account.Extra["cc_gateway_egress_bucket_enabled"] = "true"
	account.Extra["cc_gateway_egress_bucket"] = "bucket-a"
	applyCCGatewayAPIKeyFormalPoolContextForTest(account)

	req, body, err := (&GatewayService{
		cfg:             ccGatewayTestConfig(PlatformAnthropic),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}).buildUpstreamRequestAnthropicAPIKeyPassthrough(context.Background(), ccGatewayTestContext("/v1/messages"), account, body, "selected-api-key")
	require.NoError(t, err)

	rewrittenBody := readRequestBody(t, req)
	require.NotContains(t, rewrittenBody, "x-anthropic-billing-header:")
	require.NotContains(t, rewrittenBody, cchMarker)
	require.NotContains(t, string(body), "x-anthropic-billing-header:")
	ctx := decodeCCGatewayFormalPoolContextForTest(t, req)
	require.Equal(t, "strip", ctx["billing_shape_policy"])
	observed := ctx["observed_client_profile"].(map[string]any)
	require.Equal(t, "cch_present", observed["billing_shape"])
}

func TestGatewayService_CCGatewayFormalPoolObservedClientProfileDropsUnknownBodyKeyNames(t *testing.T) {
	useClaudeCodeSessionBoundaryLedgerFileForTest(t)
	const unknownBodyKey = "client_supplied_secret_marker"
	body := []byte(`{"model":"claude-sonnet-4-6","stream":true,"` + unknownBodyKey + `":{"nested":true},"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}"},"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],"tools":[{"name":"Bash","input_schema":{"type":"object"}}]}`)
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

	req, _, err := (&GatewayService{
		cfg:             ccGatewayTestConfig(PlatformAnthropic),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}).buildUpstreamRequest(context.Background(), ccGatewayTestContext("/v1/messages"), account, body, "oauth-token", "oauth", "claude-sonnet-4-6", true, false, false)
	require.NoError(t, err)

	ctx := decodeCCGatewayFormalPoolContextForTest(t, req)
	observed := ctx["observed_client_profile"].(map[string]any)
	require.Contains(t, observed["top_level_body_keys"], "tools")
	require.NotContains(t, observed["top_level_body_keys"], unknownBodyKey)
	require.Equal(t, float64(1), observed["unknown_top_level_body_key_count"])
	encodedContextJSON, err := json.Marshal(ctx)
	require.NoError(t, err)
	require.NotContains(t, string(encodedContextJSON), unknownBodyKey)
}

func TestGatewayService_CCGatewayFormalPoolUnknownObservedVersionDoesNotPromoteProfile(t *testing.T) {
	useClaudeCodeSessionBoundaryLedgerFileForTest(t)
	body := []byte(`{"model":"claude-sonnet-4-6","stream":true,"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}"},"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
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
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.195 (external, sdk-cli)")
	req, _, err := (&GatewayService{
		cfg:             ccGatewayTestConfig(PlatformAnthropic),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}).buildUpstreamRequest(context.Background(), c, account, body, "oauth-token", "oauth", "claude-sonnet-4-6", true, false, false)
	require.NoError(t, err)

	ctx := decodeCCGatewayFormalPoolContextForTest(t, req)
	observed := ctx["observed_client_profile"].(map[string]any)
	require.Equal(t, "2.1.195", observed["cli_version_bucket"])
	require.Equal(t, ccGatewayAnthropicPolicyVersion, ctx["policy_version"])
	require.Equal(t, ccGatewayDefaultPersonaProfile, ctx["persona_profile"])
	require.Equal(t, ccGatewayDefault2179ProfilePolicyVersion, ctx["profile_policy_version"])
	require.Equal(t, ccGatewayDefault2179RequestShapeProfile, ctx["request_shape_profile_ref"])
	require.Equal(t, ccGatewayDefault2179CacheParityProfile, ctx["cache_parity_profile_ref"])
	require.Equal(t, ccGatewayDefaultEgressTLSProfileRef, ctx["egress_tls_profile_ref"])
	require.Equal(t, "strip_attribution", ctx["trusted_egress_profile_ref"])
	require.Equal(t, "strip", ctx["billing_shape_policy"])
}

func TestGatewayService_CCGatewayFormalPoolIgnoresBodyAuthorityHints(t *testing.T) {
	useClaudeCodeSessionBoundaryLedgerFileForTest(t)
	const wantPersonaProfile = "claude_code_2_1_179_native_degraded"
	body := []byte(`{"model":"claude-sonnet-4-6","account":"client-account","credential":{"ref":"client-cred"},"egress":{"bucket":"client-bucket"},"persona":"client-persona","profile":{"trusted_egress_profile_ref":"client-signed-cch","billing_shape_policy":"signed_cch","context_1m":true},"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}"},"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
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

	req, _, err := (&GatewayService{
		cfg:             ccGatewayTestConfig(PlatformAnthropic),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}).buildUpstreamRequest(context.Background(), ccGatewayTestContext("/v1/messages"), account, body, "oauth-token", "oauth", "claude-sonnet-4-6", true, false, false)
	require.NoError(t, err)

	ctx := decodeCCGatewayFormalPoolContextForTest(t, req)
	require.Equal(t, "hmac-sha256:"+strings.Repeat("b", 64), ctx["account_id"])
	require.Equal(t, "opaque:credential-ref:v1:cred-a", ctx["credential_ref"])
	require.Equal(t, "claude-safe-bucket", ctx["egress_bucket"])
	require.Equal(t, wantPersonaProfile, ctx["persona_profile"])
	require.Equal(t, "strip_attribution", ctx["trusted_egress_profile_ref"])
	require.Equal(t, "strip", ctx["billing_shape_policy"])
	require.NotContains(t, ctx, "account")
	require.NotContains(t, ctx, "credential")
	require.NotContains(t, ctx, "egress")
	require.NotContains(t, ctx, "profile")
}

func TestGatewayService_CCGatewayFormalPoolEmitsServerInternalControlAndCredentialSource(t *testing.T) {
	useClaudeCodeSessionBoundaryLedgerFileForTest(t)
	body := []byte(`{"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}"},"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}]}`)
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
	account.Extra[FormalPoolExtraPoolProfileEffective] = "claude_code_2_1_175_subscription_1m"
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction

	cfg := ccGatewayTestConfig(PlatformAnthropic)
	cfg.Gateway.CCGateway.InternalControlToken = "server-internal-control-material-test"
	c := ccGatewayTestContext("/v1/messages")
	c.Request.Header.Set("x-cc-internal-control-token", "client-forged-internal-control")
	c.Request.Header.Set("x-sub2api-context-1m", "false")

	req, _, err := (&GatewayService{
		cfg:             cfg,
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}).buildUpstreamRequest(context.Background(), c, account, body, "oauth-token", "oauth", "claude-sonnet-4-6", true, false, false)
	require.NoError(t, err)

	require.Equal(t, "server-internal-control-material-test", getHeaderRaw(req.Header, "x-cc-internal-control-token"))
	require.NotEqual(t, "client-forged-internal-control", getHeaderRaw(req.Header, "x-cc-internal-control-token"))
	require.Equal(t, "true", getHeaderRaw(req.Header, ccGatewayContext1MHeader))
	ctx := decodeCCGatewayFormalPoolContextForTest(t, req)
	require.Equal(t, "server_account_credentials", ctx["credential_source"])
}

func TestCCGatewayFormalPoolAttestationMatchesSharedContractFixture(t *testing.T) {
	useClaudeCodeSessionBoundaryLedgerFileForTest(t)

	raw, err := os.ReadFile("testdata/cc_gateway_formal_pool_contract/vectors.json")
	require.NoError(t, err)
	var fixture struct {
		Materials    map[string]string `json:"materials"`
		Account      map[string]string `json:"account"`
		ClientInput  map[string]string `json:"client_input"`
		ValidContext map[string]any    `json:"valid_context"`
	}
	require.NoError(t, json.Unmarshal(raw, &fixture))

	cfg := ccGatewayTestConfig(PlatformAnthropic)
	cfg.Gateway.CCGateway.InternalControlToken = fixture.Materials["internal_control_material"]
	cfg.Gateway.CCGateway.ContextAttestationSecret = fixture.Materials["context_attestation_material"]
	t.Setenv("SUB2API_SESSION_BUDGET_HMAC_KEY", fixture.Materials["sub2api_session_budget_material"])
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	account.Extra["cc_gateway_enabled"] = "true"
	account.Extra["cc_gateway_canary_only"] = "false"
	account.Extra["cc_gateway_policy_version"] = fixture.Account["policy_version"]
	account.Extra["cc_gateway_routes"] = "native_messages,native_count_tokens"
	account.Extra["cc_gateway_egress_bucket_enabled"] = "true"
	account.Extra["cc_gateway_account_ref"] = fixture.Account["account_id"]
	account.Extra["cc_gateway_credential_ref"] = fixture.Account["credential_ref"]
	account.Extra["cc_gateway_egress_bucket"] = fixture.Account["egress_bucket"]
	account.Extra["cc_gateway_proxy_identity_ref"] = fixture.Account["proxy_identity_ref"]
	fixture.Account["egress_tls_profile_ref"] = "tls-profile:shared-fixture-contract-nondefault-v1"
	fixture.ValidContext["egress_tls_profile_ref"] = fixture.Account["egress_tls_profile_ref"]
	account.Extra["cc_gateway_egress_tls_profile_ref"] = fixture.Account["egress_tls_profile_ref"]
	account.Extra["cc_gateway_persona_profile"] = fixture.Account["persona_profile"]
	account.Extra["claude_code_device_id"] = fixture.Account["device_id"]
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction

	req := httptest.NewRequest(http.MethodPost, fixture.ValidContext["path"].(string), nil)
	req.Header.Set("X-Claude-Code-Session-Id", fixture.ClientInput["raw_client_session_id"])
	require.NoError(t, applyCCGatewayAnthropicHeaders(req, cfg, account, fixture.ValidContext["token_type"].(string)))
	require.NoError(t, applyCCGatewayFormalPoolAttestation(req, cfg, account))

	ctx := decodeCCGatewayFormalPoolContextForTest(t, req)
	for _, key := range []string{
		"method", "route_class", "path", "account_id", "token_type", "credential_ref", "credential_source",
		"egress_bucket", "proxy_identity_ref", "policy_version", "persona_profile", "session_id",
		"trusted_egress_profile_ref", "egress_tls_profile_ref", "profile_policy_version", "billing_shape_policy",
		"request_shape_profile_ref", "cache_parity_profile_ref",
	} {
		require.Equal(t, fixture.ValidContext[key], ctx[key], key)
	}
	require.NotEqual(t, fixture.ClientInput["raw_client_session_id"], ctx["session_id"])
	requireValidCCGatewayFormalPoolSignatureForTest(t, req, fixture.Materials["context_attestation_material"])
	require.Equal(t, fixture.Materials["internal_control_material"], getHeaderRaw(req.Header, ccGatewayInternalControlHeader))
}

func TestCCGatewayFormalPoolAWSAttestationMatchesSharedCanonicalFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/cc_gateway_formal_pool_contract/vectors.json")
	require.NoError(t, err)
	var fixture struct {
		Materials           map[string]string `json:"materials"`
		AWSValidContext     map[string]any    `json:"aws_valid_context"`
		AWSCanonicalJSON    string            `json:"aws_canonical_json"`
		AWSContextSignature string            `json:"aws_context_signature"`
	}
	require.NoError(t, json.Unmarshal(raw, &fixture))
	canonical, err := json.Marshal(fixture.AWSValidContext)
	require.NoError(t, err)
	require.Equal(t, fixture.AWSCanonicalJSON, string(canonical))
	mac := hmac.New(sha256.New, []byte(fixture.Materials["context_attestation_material"]))
	_, _ = mac.Write(canonical)
	require.Equal(t, fixture.AWSContextSignature, "hmac-sha256:"+hex.EncodeToString(mac.Sum(nil)))
	for _, forbidden := range []string{"wrkspc_", "synthetic-cpaws", "Authorization", "x-api-key", "raw_prompt", "raw_body", "proxy_credential"} {
		require.NotContains(t, string(canonical), forbidden)
	}
}

func TestGatewayService_CCGatewayAnthropicOAuthFailsClosedWithoutAttestationSecret(t *testing.T) {
	useClaudeCodeSessionBoundaryLedgerFileForTest(t)
	body := []byte(`{"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}"},"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}]}`)
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	account.Extra["cc_gateway_enabled"] = "true"
	account.Extra["cc_gateway_canary_only"] = "false"
	account.Extra["cc_gateway_policy_version"] = ccGatewayAnthropicPolicyVersion
	account.Extra["cc_gateway_routes"] = "native_messages,native_count_tokens,chat_completions,responses"
	account.Extra["cc_gateway_egress_bucket_enabled"] = "true"
	account.Extra["cc_gateway_egress_bucket"] = "bucket-a"
	account.Extra["cc_gateway_account_ref"] = "hmac-sha256:" + strings.Repeat("c", 64)
	account.Extra[FormalPoolExtraPoolProfileEffective] = "claude_code_2_1_175_subscription_1m"
	formalPoolApplyCompleteSchedulingEvidenceForTest(account)
	account.Extra[ccGatewayExtraCredentialRef] = "opaque:credential-ref:v1:cred-a"
	account.Extra[ccGatewayExtraProxyIdentityRef] = "opaque:proxy-ref:v1:bucket-a"
	account.Extra[FormalPoolExtraPoolProfileEffective] = "claude_code_2_1_175_subscription_1m"
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction

	cfg := ccGatewayTestConfig(PlatformAnthropic)
	cfg.Gateway.CCGateway.ContextAttestationSecret = ""
	c := ccGatewayTestContext("/v1/messages")

	req, _, err := (&GatewayService{
		cfg:             cfg,
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}).buildUpstreamRequest(context.Background(), c, account, body, "oauth-token", "oauth", "claude-sonnet-4-6", true, false, false)
	require.Error(t, err)
	require.Nil(t, req)
	require.Contains(t, err.Error(), "attestation secret is required")
}

func TestGatewayService_SelectCCGatewayAnthropicRouteBypassesUnmanagedLegacyAnthropicAccount(t *testing.T) {
	svc := &GatewayService{cfg: ccGatewayTestConfig(PlatformAnthropic)}
	account := newAnthropicOAuthAccountForClaudeForwardTest()

	useCCGateway, err := svc.selectCCGatewayAnthropicRoute(account, ccGatewayRouteNativeMessages)
	require.False(t, useCCGateway)
	require.NoError(t, err)
}

func TestGatewayService_SelectCCGatewayAnthropicRouteFailsClosedOnExplicitlyDisabledGateState(t *testing.T) {
	svc := &GatewayService{cfg: ccGatewayTestConfig(PlatformAnthropic)}
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	account.Extra["cc_gateway_enabled"] = "false"

	useCCGateway, err := svc.selectCCGatewayAnthropicRoute(account, ccGatewayRouteNativeMessages)
	require.False(t, useCCGateway)
	require.Error(t, err)
	require.Contains(t, err.Error(), "disabled or missing")
}

func TestGatewayService_SelectCCGatewayAnthropicRouteRespectsPolicyAndCanaryFlags(t *testing.T) {
	svc := &GatewayService{cfg: ccGatewayTestConfig(PlatformAnthropic)}
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	account.Extra["cc_gateway_enabled"] = "true"
	account.Extra["cc_gateway_canary_only"] = "false"
	account.Extra["cc_gateway_policy_version"] = ccGatewayAnthropicPolicyVersion
	account.Extra["cc_gateway_routes"] = "native_messages,chat_completions"
	account.Extra["cc_gateway_egress_bucket"] = "bucket-a"
	account.Extra["cc_gateway_egress_bucket_enabled"] = "true"

	useCCGateway, err := svc.selectCCGatewayAnthropicRoute(account, ccGatewayRouteNativeMessages)
	require.True(t, useCCGateway)
	require.NoError(t, err)

	account.Extra["cc_gateway_canary_only"] = "true"
	useCCGateway, err = svc.selectCCGatewayAnthropicRoute(account, ccGatewayRouteNativeMessages)
	require.False(t, useCCGateway)
	require.Error(t, err)
	require.Contains(t, err.Error(), "canary-only")
}

func TestGatewayService_ExplicitCanaryAllowsCanaryOnlyMessagesWithoutBroadRouting(t *testing.T) {
	svc := &GatewayService{cfg: ccGatewayCanaryTestConfig()}
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	account.Schedulable = false
	account.Concurrency = 0
	account.Credentials["scope"] = "user:file_upload user:inference user:mcp_servers user:profile user:sessions:claude_code"
	account.Extra["cc_gateway_enabled"] = "true"
	account.Extra["cc_gateway_canary_only"] = "true"
	account.Extra["cc_gateway_policy_version"] = ccGatewayAnthropicPolicyVersion
	account.Extra["cc_gateway_routes"] = "native_messages"
	account.Extra["cc_gateway_egress_bucket_enabled"] = "true"
	account.Extra["cc_gateway_egress_bucket"] = "home-ip-canary-2026-05-22"
	account.Extra["billing_cch_mode"] = "sign"
	account.Extra["cc_gateway_account_ref"] = "hmac-sha256:acct-canary-placeholder"

	useCCGateway, err := svc.selectCCGatewayAnthropicRoute(account, ccGatewayRouteNativeMessages)
	require.False(t, useCCGateway)
	require.Error(t, err)
	require.Contains(t, err.Error(), "canary-only")

	useCCGateway, err = svc.selectCCGatewayAnthropicCanaryRoute(account, CCGatewayAnthropicCanaryRequest{
		AccountHash:    "hmac-sha256:acct-canary-placeholder",
		EgressBucket:   "home-ip-canary-2026-05-22",
		BillingCCHMode: "sign",
		Method:         http.MethodPost,
		Route:          "/v1/messages",
	})
	require.True(t, useCCGateway)
	require.NoError(t, err)
}

func TestGatewayService_ExplicitCanaryFailsClosedOnScopeRouteModeAndFallbacks(t *testing.T) {
	svc := &GatewayService{cfg: ccGatewayCanaryTestConfig()}
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	account.Schedulable = false
	account.Concurrency = 0
	account.Credentials["scope"] = "user:file_upload user:inference user:mcp_servers user:profile user:sessions:claude_code"
	account.Extra["cc_gateway_enabled"] = "true"
	account.Extra["cc_gateway_canary_only"] = "true"
	account.Extra["cc_gateway_policy_version"] = ccGatewayAnthropicPolicyVersion
	account.Extra["cc_gateway_routes"] = "native_messages"
	account.Extra["cc_gateway_egress_bucket_enabled"] = "true"
	account.Extra["cc_gateway_egress_bucket"] = "home-ip-canary-2026-05-22"
	account.Extra["billing_cch_mode"] = "sign"
	account.Extra["cc_gateway_account_ref"] = "hmac-sha256:acct-canary-placeholder"

	valid := CCGatewayAnthropicCanaryRequest{
		AccountHash:    "hmac-sha256:acct-canary-placeholder",
		EgressBucket:   "home-ip-canary-2026-05-22",
		BillingCCHMode: "sign",
		Method:         http.MethodPost,
		Route:          "/v1/messages",
	}

	account.Credentials["scope"] = "user:profile user:file_upload"
	ok, err := svc.selectCCGatewayAnthropicCanaryRoute(account, valid)
	require.False(t, ok)
	require.Error(t, err)
	require.Contains(t, err.Error(), "inference_scope_missing")

	account.Credentials["scope"] = "user:file_upload user:inference user:mcp_servers user:profile user:sessions:claude_code"
	for name, mutate := range map[string]func(*CCGatewayAnthropicCanaryRequest){
		"count_tokens":      func(r *CCGatewayAnthropicCanaryRequest) { r.Route = "/v1/messages/count_tokens" },
		"event_logging":     func(r *CCGatewayAnthropicCanaryRequest) { r.Route = "/api/event_logging/v2/batch" },
		"openai_compatible": func(r *CCGatewayAnthropicCanaryRequest) { r.Route = "/v1/chat/completions" },
		"antigravity":       func(r *CCGatewayAnthropicCanaryRequest) { r.Route = "/v1internal/complete" },
		"strip_mode":        func(r *CCGatewayAnthropicCanaryRequest) { r.BillingCCHMode = "strip" },
		"wrong_bucket":      func(r *CCGatewayAnthropicCanaryRequest) { r.EgressBucket = "server-ip" },
	} {
		t.Run(name, func(t *testing.T) {
			req := valid
			mutate(&req)
			ok, err := svc.selectCCGatewayAnthropicCanaryRoute(account, req)
			require.False(t, ok)
			require.Error(t, err)
		})
	}

	delete(account.Extra, "cc_gateway_egress_bucket")
	account.Extra["openai_gateway_egress_bucket"] = "home-ip-canary-2026-05-22"
	ok, err = svc.selectCCGatewayAnthropicCanaryRoute(account, valid)
	require.False(t, ok)
	require.Error(t, err)
	require.Contains(t, err.Error(), "direct cc gateway egress bucket")
}

func TestGatewayService_SelectCCGatewayAnthropicRouteFailsClosedOnRouteAndLifecycleRejects(t *testing.T) {
	svc := &GatewayService{cfg: ccGatewayTestConfig(PlatformAnthropic)}
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	account.Extra["cc_gateway_enabled"] = "true"
	account.Extra["cc_gateway_canary_only"] = "false"
	account.Extra["cc_gateway_policy_version"] = ccGatewayAnthropicPolicyVersion
	account.Extra["cc_gateway_routes"] = "native_messages"
	account.Extra["cc_gateway_egress_bucket"] = "bucket-a"
	account.Extra["cc_gateway_egress_bucket_enabled"] = "true"

	useCCGateway, err := svc.selectCCGatewayAnthropicRoute(account, ccGatewayRouteResponses)
	require.False(t, useCCGateway)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not allowed")

	account.Extra["cc_gateway_routes"] = "native_messages,responses"
	account.Extra["cc_gateway_routes_deny"] = "responses"
	useCCGateway, err = svc.selectCCGatewayAnthropicRoute(account, ccGatewayRouteResponses)
	require.False(t, useCCGateway)
	require.Error(t, err)
	require.Contains(t, err.Error(), "denied")

	account.Extra["cc_gateway_routes_deny"] = ""
	account.Extra["cc_gateway_policy_version"] = "mismatch"
	useCCGateway, err = svc.selectCCGatewayAnthropicRoute(account, ccGatewayRouteResponses)
	require.False(t, useCCGateway)
	require.Error(t, err)
	require.Contains(t, err.Error(), "policy version mismatch")

	account.Extra["cc_gateway_policy_version"] = ccGatewayAnthropicPolicyVersion
	account.Schedulable = false
	useCCGateway, err = svc.selectCCGatewayAnthropicRoute(account, ccGatewayRouteResponses)
	require.False(t, useCCGateway)
	require.Error(t, err)
	require.Contains(t, err.Error(), "lifecycle ineligible")
}

func TestCCGatewayPolicyVersionCompatibleAllowsOldCCHCompatibleVersionsOnly(t *testing.T) {
	require.True(t, ccGatewayPolicyVersionCompatible("2.1.179"), "2.1.179 is the CP1 oracle-verified production anchor")
	require.True(t, ccGatewayPolicyVersionCompatible("2.1.150"))
	require.True(t, ccGatewayPolicyVersionCompatible("2.1.153"))
	require.True(t, ccGatewayPolicyVersionCompatible("2.1.169"))
	require.True(t, ccGatewayPolicyVersionCompatible("2.1.170"))
	require.False(t, ccGatewayPolicyVersionCompatible("2.1.151"), "unverified patches must not be inferred from the corpus")
	require.False(t, ccGatewayPolicyVersionCompatible("2.1.171"), "2.1.171 was not published and must not be treated as a verified profile")
	require.False(t, ccGatewayPolicyVersionCompatible("2.1.172"), "2.1.172 is not a registered Sub2API admission profile")
	require.False(t, ccGatewayPolicyVersionCompatible("2.1.126.test"))
	require.False(t, ccGatewayPolicyVersionCompatible("2.2.0"))
	require.False(t, ccGatewayPolicyVersionCompatible("3.0.0"))
}

func TestGatewayService_CCGatewayAnthropicAPIKeyPassthroughMessagesCarriesNativeAuditHeaders(t *testing.T) {
	body := []byte(`{"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}"},"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}],"stream":false}`)
	account := &Account{
		ID:       132,
		Platform: PlatformAnthropic,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "selected-api-key",
		},
		Extra: map[string]any{
			"anthropic_passthrough":            true,
			"cc_gateway_enabled":               "true",
			"cc_gateway_canary_only":           "false",
			"cc_gateway_policy_version":        ccGatewayAnthropicPolicyVersion,
			"cc_gateway_routes":                "native_messages,native_count_tokens",
			"cc_gateway_egress_bucket_enabled": "true",
			"cc_gateway_egress_bucket":         "bucket-a",
		},
	}
	applyCCGatewayAPIKeyFormalPoolContextForTest(account)
	svc := &GatewayService{
		cfg:             ccGatewayTestConfig(PlatformAnthropic),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}
	ctx := WithClaudeCodeNativeAuditSummary(context.Background(), testClaudeCodeNativeReplaySafeSummary(ClaudeCodeNativeAuditSummary{
		ClientType:              ClaudeCodeNativeClientType,
		NativeAttested:          true,
		GuardVersion:            "guard-test",
		ClaudeCodeVersion:       "2.1.177",
		LocalSessionRef:         "session-ref",
		InboundRoute:            ClaudeCodeNativeInboundMessages,
		CCGatewayRoute:          ClaudeCodeNativeCCGatewayMessages,
		ShapeHealthcheckProfile: ClaudeCodeNativeTakeoverHealthProfile,
		RuntimeHash:             "sha256:runtime",
		OverlayHash:             "sha256:overlay",
		CatalogHash:             "sha256:catalog",
		CatalogVersion:          "2026-06-19",
	}))

	req, _, err := svc.buildUpstreamRequestAnthropicAPIKeyPassthrough(ctx, ccGatewayTestContext("/v1/messages"), account, body, "selected-api-key")
	require.NoError(t, err)
	require.Equal(t, "http://cc-gateway:8443/v1/messages?beta=true", req.URL.String())
	require.Equal(t, ClaudeCodeNativeClientType, getHeaderRaw(req.Header, ClaudeCodeNativeClientTypeHeader))
	require.Equal(t, "true", getHeaderRaw(req.Header, ClaudeCodeNativeGuardAttestedHeader))
	require.Equal(t, "2.1.177", getHeaderRaw(req.Header, ClaudeCodeNativeClaudeCodeVersionHeader))
	require.Equal(t, ClaudeCodeNativeInboundMessages, getHeaderRaw(req.Header, ClaudeCodeNativeInboundRouteHeader))
	require.Equal(t, ClaudeCodeNativeCCGatewayMessages, getHeaderRaw(req.Header, ClaudeCodeNativeCCGatewayRouteHeader))
	require.Equal(t, ClaudeCodeNativeReplaySafetyBoundary, getHeaderRaw(req.Header, ClaudeCodeNativeReplaySafetyBoundaryHeader))
	require.Equal(t, "true", getHeaderRaw(req.Header, ClaudeCodeNativeReplaySafetyAppliedHeader))
	require.Equal(t, "false", getHeaderRaw(req.Header, ClaudeCodeNativeReplaySafetySanitizedHeader))
	require.Equal(t, "0", getHeaderRaw(req.Header, ClaudeCodeNativeReplaySafetyForbiddenPathsHeader))
	require.Equal(t, "sha256:"+strings.Repeat("d", 64), getHeaderRaw(req.Header, ClaudeCodeNativeReplaySafetyBodyShapeHashHeader))
}

func TestGatewayService_CCGatewayAnthropicAPIKeyPassthroughCountTokensCarriesNativeAuditHeaders(t *testing.T) {
	body := []byte(`{"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}"},"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}]}`)
	account := &Account{
		ID:       132,
		Platform: PlatformAnthropic,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "selected-api-key",
		},
		Extra: map[string]any{
			"anthropic_passthrough":            true,
			"cc_gateway_enabled":               "true",
			"cc_gateway_canary_only":           "false",
			"cc_gateway_policy_version":        ccGatewayAnthropicPolicyVersion,
			"cc_gateway_routes":                "native_messages,native_count_tokens",
			"cc_gateway_egress_bucket_enabled": "true",
			"cc_gateway_egress_bucket":         "bucket-a",
		},
	}
	applyCCGatewayAPIKeyFormalPoolContextForTest(account)
	svc := &GatewayService{
		cfg:             ccGatewayTestConfig(PlatformAnthropic),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}
	ctx := WithClaudeCodeNativeAuditSummary(context.Background(), testClaudeCodeNativeReplaySafeSummary(ClaudeCodeNativeAuditSummary{
		ClientType:              ClaudeCodeNativeClientType,
		NativeAttested:          true,
		GuardVersion:            "guard-test",
		ClaudeCodeVersion:       "2.1.177",
		LocalSessionRef:         "session-ref",
		InboundRoute:            ClaudeCodeNativeInboundCountTokens,
		CCGatewayRoute:          ClaudeCodeNativeCCGatewayCount,
		ShapeHealthcheckProfile: ClaudeCodeNativeControlPlaneHealthProfile,
		RuntimeHash:             "sha256:runtime",
		OverlayHash:             "sha256:overlay",
		CatalogHash:             "sha256:catalog",
		CatalogVersion:          "2026-06-19",
	}))

	req, err := svc.buildCountTokensRequestAnthropicAPIKeyPassthrough(ctx, ccGatewayTestContext("/v1/messages/count_tokens"), account, body, "selected-api-key")
	require.NoError(t, err)
	require.Equal(t, "http://cc-gateway:8443/v1/messages/count_tokens?beta=true", req.URL.String())
	require.Equal(t, ClaudeCodeNativeClientType, getHeaderRaw(req.Header, ClaudeCodeNativeClientTypeHeader))
	require.Equal(t, "true", getHeaderRaw(req.Header, ClaudeCodeNativeGuardAttestedHeader))
	require.Equal(t, "2.1.177", getHeaderRaw(req.Header, ClaudeCodeNativeClaudeCodeVersionHeader))
	require.Equal(t, ClaudeCodeNativeInboundCountTokens, getHeaderRaw(req.Header, ClaudeCodeNativeInboundRouteHeader))
	require.Equal(t, ClaudeCodeNativeCCGatewayCount, getHeaderRaw(req.Header, ClaudeCodeNativeCCGatewayRouteHeader))
	require.Equal(t, ClaudeCodeNativeReplaySafetyBoundary, getHeaderRaw(req.Header, ClaudeCodeNativeReplaySafetyBoundaryHeader))
	require.Equal(t, "true", getHeaderRaw(req.Header, ClaudeCodeNativeReplaySafetyAppliedHeader))
}

func TestGatewayService_CCGatewayAnthropicOAuthStaleCompatibleExtraCanonicalizesToFinalPolicyVersion(t *testing.T) {
	for _, staleVersion := range []string{"2.1.150", "2.1.170"} {
		t.Run(staleVersion, func(t *testing.T) {
			body := []byte(`{"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}"},"model":"claude-opus-4-8","messages":[{"role":"user","content":"hi"}],"max_tokens":32000,"stream":true}`)
			account := &Account{
				ID:       54,
				Platform: PlatformAnthropic,
				Type:     AccountTypeOAuth,
				Credentials: map[string]any{
					"access_token": "selected-oauth-token",
					"email":        "user@example.com",
				},
				Extra: map[string]any{
					"account_uuid":                     "acct-uuid",
					"organization_uuid":                "org-uuid",
					"cc_gateway_enabled":               "true",
					"cc_gateway_canary_only":           "false",
					"cc_gateway_policy_version":        staleVersion,
					"cc_gateway_routes":                "native_messages,native_count_tokens,chat_completions,responses",
					"cc_gateway_egress_bucket_enabled": "true",
					"cc_gateway_egress_bucket":         "bucket-a",
				},
			}
			svc := &GatewayService{
				cfg:             ccGatewayTestConfig(PlatformAnthropic),
				identityService: NewIdentityService(ccGatewayIdentityCache{}),
			}

			req, _, err := svc.buildUpstreamRequest(context.Background(), ccGatewayTestContext("/v1/messages"), account, body, "selected-oauth-token", "oauth", "claude-opus-4-8", true, false, false)
			require.NoError(t, err)
			require.Equal(t, staleVersion, account.GetExtraString("cc_gateway_policy_version"), "DB/account metadata remains unchanged")
			require.Equal(t, ccGatewayAnthropicPolicyVersion, getHeaderRaw(req.Header, "x-cc-policy-version"))
		})
	}
}

func TestGatewayService_CCGatewayAnthropicOAuthExactPolicyVersionKeepsKnownOpus46CapabilityFloor(t *testing.T) {
	body := []byte(`{"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}"},"model":"claude-opus-4-6","messages":[{"role":"user","content":"hi"}],"max_tokens":32000,"stream":true,"thinking":{"type":"enabled","budget_tokens":1024},"context_management":{"edits":[]}}`)
	account := &Account{
		ID:       51,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "selected-oauth-token",
			"email":        "user@example.com",
		},
		Extra: map[string]any{
			"account_uuid":                     "acct-uuid",
			"organization_uuid":                "org-uuid",
			"cc_gateway_enabled":               "true",
			"cc_gateway_canary_only":           "false",
			"cc_gateway_policy_version":        ccGatewayAnthropicPolicyVersion,
			"cc_gateway_routes":                "native_messages,native_count_tokens,chat_completions,responses",
			"cc_gateway_egress_bucket_enabled": "true",
			"cc_gateway_egress_bucket":         "bucket-a",
		},
	}
	svc := &GatewayService{
		cfg:             ccGatewayTestConfig(PlatformAnthropic),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}

	ctx := SetClaudeCodeVersion(context.Background(), ccGatewayAnthropicPolicyVersion)
	req, _, err := svc.buildUpstreamRequest(ctx, ccGatewayTestContext("/v1/messages"), account, body, "selected-oauth-token", "oauth", "claude-opus-4-6", true, false, false)
	require.NoError(t, err)
	require.Equal(t, ccGatewayAnthropicPolicyVersion, getHeaderRaw(req.Header, "x-cc-policy-version"))
	require.Contains(t, readRequestBody(t, req), `"max_tokens":32000`)
	require.Contains(t, readRequestBody(t, req), `"stream":true`)
	require.Contains(t, readRequestBody(t, req), `"thinking"`)
	require.Contains(t, readRequestBody(t, req), `"context_management"`)
}

func TestGatewayService_CCGatewayAnthropicOAuthVerifiedLegacyDriftPolicyVersionPasses(t *testing.T) {
	body := []byte(`{"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}"},"model":"claude-opus-4-7","messages":[{"role":"user","content":"hi"}],"max_tokens":32000,"stream":true,"thinking":{"type":"enabled","budget_tokens":1024},"context_management":{"edits":[]}}`)
	account := &Account{
		ID:       52,
		Status:   StatusActive,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "selected-oauth-token",
			"email":        "user@example.com",
			"scope":        "user:file_upload user:inference user:mcp_servers user:profile user:sessions:claude_code",
		},
		Extra: map[string]any{
			"account_uuid":                     "acct-uuid",
			"organization_uuid":                "org-uuid",
			"cc_gateway_enabled":               "true",
			"cc_gateway_canary_only":           "true",
			"cc_gateway_policy_version":        "2.1.153",
			"cc_gateway_routes":                "native_messages,native_count_tokens,chat_completions,responses",
			"cc_gateway_egress_bucket_enabled": "true",
			"cc_gateway_egress_bucket":         "bucket-a",
			"billing_cch_mode":                 "sign",
		},
	}
	svc := &GatewayService{
		cfg:             ccGatewayCanaryTestConfig(),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}

	ctx := WithCCGatewayExplicitCanaryRequest(SetClaudeCodeVersion(context.Background(), "2.1.153"), CCGatewayAnthropicCanaryRequest{
		AccountID:      account.ID,
		EgressBucket:   "bucket-a",
		BillingCCHMode: "sign",
		Method:         http.MethodPost,
		Route:          "/v1/messages",
	})
	req, _, err := svc.buildUpstreamRequest(ctx, ccGatewayTestContext("/v1/messages"), account, body, "selected-oauth-token", "oauth", "claude-opus-4-7", true, false, false)
	require.NoError(t, err)
	require.Equal(t, "2.1.153", getHeaderRaw(req.Header, "x-cc-policy-version"))
	require.Equal(t, "1", getHeaderRaw(req.Header, ccGatewayTrustedPersonaHeader))
	require.Contains(t, readRequestBody(t, req), `"max_tokens":32000`)
	require.Contains(t, readRequestBody(t, req), `"context_management"`)
}

func TestGatewayService_CCGatewayAnthropicOAuthExplicitCanaryBuildAllowsCanaryOnlyAccount(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}],"max_tokens":128,"stream":false}`)
	account := &Account{
		ID:          57,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: false,
		Concurrency: 0,
		Credentials: map[string]any{
			"access_token": "selected-oauth-token",
			"scope":        "user:file_upload user:inference user:mcp_servers user:profile user:sessions:claude_code",
		},
		Extra: map[string]any{
			"cc_gateway_enabled":               "true",
			"cc_gateway_canary_only":           "true",
			"cc_gateway_policy_version":        "2.1.150",
			"cc_gateway_routes":                "native_messages",
			"cc_gateway_egress_bucket_enabled": "true",
			"cc_gateway_egress_bucket":         "bucket-a",
			"billing_cch_mode":                 "sign",
			"cc_gateway_account_ref":           "hmac-sha256:acct-canary-placeholder",
		},
	}
	svc := &GatewayService{cfg: ccGatewayCanaryTestConfig()}
	ctx := WithCCGatewayExplicitCanaryRequest(WithCCGatewayExplicitCanaryLocalOnly(SetClaudeCodeVersion(context.Background(), "2.1.150")), CCGatewayAnthropicCanaryRequest{
		AccountID:      account.ID,
		AccountHash:    "hmac-sha256:acct-canary-placeholder",
		EgressBucket:   "bucket-a",
		BillingCCHMode: "sign",
		Method:         http.MethodPost,
		Route:          "/v1/messages",
	})

	req, _, err := svc.buildUpstreamRequest(ctx, ccGatewayTestContext("/v1/messages"), account, body, "selected-oauth-token", "oauth", "claude-sonnet-4-6", false, false, false)
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:18443/v1/messages?beta=true", req.URL.String())
	require.Equal(t, "hmac-sha256:acct-canary-placeholder", getHeaderRaw(req.Header, ccGatewayAccountIDHeader))
	require.Equal(t, "bucket-a", getHeaderRaw(req.Header, ccGatewayEgressBucketHeader))
	require.Equal(t, "1", getHeaderRaw(req.Header, ccGatewayTrustedPersonaHeader))
	require.Equal(t, "2.1.150", getHeaderRaw(req.Header, ccGatewayPolicyVersionHeader))
}

func TestGatewayService_CCGatewayAnthropicOAuthExplicitCanaryRejectsUnverified2177PolicyVersion(t *testing.T) {
	body := []byte(`{"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}"},"model":"claude-opus-4-8","messages":[{"role":"user","content":"hi"}],"max_tokens":32000,"stream":true}`)
	account := &Account{
		ID:       54,
		Status:   StatusActive,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "selected-oauth-token",
			"email":        "user@example.com",
			"scope":        "user:file_upload user:inference user:mcp_servers user:profile user:sessions:claude_code",
		},
		Extra: map[string]any{
			"account_uuid":                     "acct-uuid",
			"organization_uuid":                "org-uuid",
			"cc_gateway_enabled":               "true",
			"cc_gateway_canary_only":           "true",
			"cc_gateway_policy_version":        ccGatewayAnthropicPolicyVersion,
			"cc_gateway_routes":                "native_messages,native_count_tokens,chat_completions,responses",
			"cc_gateway_egress_bucket_enabled": "true",
			"cc_gateway_egress_bucket":         "bucket-a",
			"billing_cch_mode":                 "sign",
		},
	}
	svc := &GatewayService{
		cfg:             ccGatewayCanaryTestConfig(),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}

	ctx := WithCCGatewayExplicitCanaryRequest(SetClaudeCodeVersion(context.Background(), "2.1.177"), CCGatewayAnthropicCanaryRequest{
		AccountID:      account.ID,
		EgressBucket:   "bucket-a",
		BillingCCHMode: "sign",
		Method:         http.MethodPost,
		Route:          "/v1/messages",
	})
	req, _, err := svc.buildUpstreamRequest(ctx, ccGatewayTestContext("/v1/messages"), account, body, "selected-oauth-token", "oauth", "claude-opus-4-8", true, false, false)
	require.NoError(t, err)
	require.NotEqual(t, "2.1.177", getHeaderRaw(req.Header, "x-cc-policy-version"))
	require.Equal(t, ccGatewayAnthropicPolicyVersion, getHeaderRaw(req.Header, "x-cc-policy-version"))
	require.Empty(t, getHeaderRaw(req.Header, ccGatewayTrustedPersonaHeader))
}

func TestGatewayService_CCGatewayAnthropicOAuthVerifiedLegacyDriftWithoutTrustedContextFallsBackToAnchoredPolicyVersion(t *testing.T) {
	body := []byte(`{"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"client-session\"}"},"model":"claude-opus-4-7","messages":[{"role":"user","content":"hi"}],"max_tokens":32000,"stream":true}`)
	account := &Account{
		ID:       53,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "selected-oauth-token",
			"email":        "user@example.com",
		},
		Extra: map[string]any{
			"account_uuid":                     "acct-uuid",
			"organization_uuid":                "org-uuid",
			"cc_gateway_enabled":               "true",
			"cc_gateway_canary_only":           "false",
			"cc_gateway_policy_version":        ccGatewayAnthropicPolicyVersion,
			"cc_gateway_routes":                "native_messages,native_count_tokens,chat_completions,responses",
			"cc_gateway_egress_bucket_enabled": "true",
			"cc_gateway_egress_bucket":         "bucket-a",
		},
	}
	svc := &GatewayService{
		cfg:             ccGatewayTestConfig(PlatformAnthropic),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}

	ctx := SetClaudeCodeVersion(context.Background(), "2.1.153")
	req, _, err := svc.buildUpstreamRequest(ctx, ccGatewayTestContext("/v1/messages"), account, body, "selected-oauth-token", "oauth", "claude-opus-4-7", true, false, false)
	require.NoError(t, err)
	require.Equal(t, ccGatewayAnthropicPolicyVersion, getHeaderRaw(req.Header, "x-cc-policy-version"))
	require.Empty(t, getHeaderRaw(req.Header, ccGatewayTrustedPersonaHeader))
}

func TestAntigravityGatewayService_CCGatewayRetryLoopBuildsV1InternalRequest(t *testing.T) {
	upstream := &anthropicHTTPUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"response":{}}`)),
		},
	}
	svc := &AntigravityGatewayService{httpUpstream: upstream}
	_, err := svc.antigravityRetryLoop(antigravityRetryLoopParams{
		ctx:                   context.Background(),
		prefix:                "test",
		account:               &Account{ID: 77, Name: "ag", Platform: PlatformAntigravity, Type: AccountTypeOAuth, Concurrency: 1},
		accessToken:           "google-access-token",
		action:                "streamGenerateContent",
		body:                  []byte(`{"project":"projects/p","request":{}}`),
		httpUpstream:          upstream,
		ccGatewayEnabled:      true,
		ccGatewayBaseURL:      "http://cc-gateway:8443",
		ccGatewayToken:        "ccg-token",
		ccGatewayEgressBucket: "bucket-ag",
		ccGatewayProjectID:    "projects/p",
	})
	require.NoError(t, err)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "http://cc-gateway:8443/v1internal:streamGenerateContent?alt=sse", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer google-access-token", upstream.lastReq.Header.Get("authorization"))
	require.Equal(t, "ccg-token", upstream.lastReq.Header.Get("x-cc-gateway-token"))
	require.Equal(t, "77", upstream.lastReq.Header.Get("x-cc-account-id"))
	require.Equal(t, "antigravity", upstream.lastReq.Header.Get("x-cc-provider"))
	require.Equal(t, "oauth", upstream.lastReq.Header.Get("x-cc-token-type"))
	require.Equal(t, "bucket-ag", upstream.lastReq.Header.Get("x-cc-egress-bucket"))
	require.Equal(t, "projects/p", upstream.lastReq.Header.Get("x-cc-project-id"))
}

func TestAntigravityGatewayService_CCGatewayCreditsOveragesRetryAddsControlHeaders(t *testing.T) {
	upstream := &anthropicHTTPUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		},
	}
	account := &Account{
		ID:          78,
		Name:        "ag-credits",
		Platform:    PlatformAntigravity,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Extra:       map[string]any{"allow_overages": true},
	}
	params := antigravityRetryLoopParams{
		ctx:                   context.Background(),
		prefix:                "test",
		account:               account,
		accessToken:           "google-access-token",
		action:                "generateContent",
		body:                  []byte(`{"model":"claude-opus-4-6","request":{}}`),
		httpUpstream:          upstream,
		requestedModel:        "claude-opus-4-6",
		ccGatewayEnabled:      true,
		ccGatewayBaseURL:      "http://cc-gateway:8443",
		ccGatewayToken:        "ccg-token",
		ccGatewayEgressBucket: "bucket-ag",
		ccGatewayProjectID:    "projects/p",
	}

	result := (&AntigravityGatewayService{}).attemptCreditsOveragesRetry(params, "http://cc-gateway:8443", "claude-opus-4-6", 0, http.StatusTooManyRequests, []byte(`{}`))
	require.True(t, result.handled)
	require.NotNil(t, result.resp)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "ccg-token", upstream.lastReq.Header.Get("x-cc-gateway-token"))
	require.Equal(t, "78", upstream.lastReq.Header.Get("x-cc-account-id"))
	require.Equal(t, "antigravity", upstream.lastReq.Header.Get("x-cc-provider"))
	require.Equal(t, "oauth", upstream.lastReq.Header.Get("x-cc-token-type"))
	require.Equal(t, "bucket-ag", upstream.lastReq.Header.Get("x-cc-egress-bucket"))
	require.Contains(t, string(upstream.lastBody), "enabledCreditTypes")
}

func TestAntigravityGatewayService_CCGatewayRequestHelperAddsControlHeaders(t *testing.T) {
	req, err := newAntigravityAPIRequestWithCCGateway(context.Background(), "http://cc-gateway:8443", "streamGenerateContent", "google-access-token", []byte(`{}`), antigravityRetryLoopParams{
		account:               &Account{ID: 79, Platform: PlatformAntigravity},
		ccGatewayEnabled:      true,
		ccGatewayToken:        "ccg-token",
		ccGatewayEgressBucket: "bucket-ag",
		ccGatewayProjectID:    "projects/p",
	})
	require.NoError(t, err)
	require.Equal(t, "http://cc-gateway:8443/v1internal:streamGenerateContent?alt=sse", req.URL.String())
	require.Equal(t, "Bearer google-access-token", req.Header.Get("authorization"))
	require.Equal(t, "ccg-token", req.Header.Get("x-cc-gateway-token"))
	require.Equal(t, "79", req.Header.Get("x-cc-account-id"))
	require.Equal(t, "bucket-ag", req.Header.Get("x-cc-egress-bucket"))
}

func TestSafeHeaderValueForLogRedactsCCGatewayToken(t *testing.T) {
	require.Equal(t, "[redacted]", safeHeaderValueForLog("x-cc-gateway-token", "ccg-token"))
}

func TestApplyCCGatewayAntigravityHeadersDoesNotSendRawAccountEmail(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "http://cc-gateway/v1/messages", nil)
	require.NoError(t, err)
	account := &Account{ID: 77, Platform: PlatformAntigravity}

	applyCCGatewayAntigravityHeaders(req, antigravityRetryLoopParams{
		account:               account,
		ccGatewayToken:        "ccg-token",
		ccGatewayEgressBucket: "bucket-a",
		ccGatewayAccountEmail: "raw-user@example.invalid",
	})

	require.Equal(t, "ccg-token", getHeaderRaw(req.Header, "x-cc-gateway-token"))
	require.Equal(t, "77", getHeaderRaw(req.Header, "x-cc-account-id"))
	require.Equal(t, "antigravity", getHeaderRaw(req.Header, "x-cc-provider"))
	require.Equal(t, "oauth", getHeaderRaw(req.Header, "x-cc-token-type"))
	require.Equal(t, "bucket-a", getHeaderRaw(req.Header, "x-cc-egress-bucket"))
	require.Empty(t, getHeaderRaw(req.Header, "x-cc-account-email"))
}

func TestGatewayService_SelectCCGatewayAnthropicRouteUsesEffectiveFormalPoolSchedulability(t *testing.T) {
	svc := &GatewayService{cfg: ccGatewayTestConfig(PlatformAnthropic)}
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	account.Schedulable = true
	account.Extra["cc_gateway_enabled"] = "true"
	account.Extra["cc_gateway_canary_only"] = "false"
	account.Extra["cc_gateway_policy_version"] = ccGatewayAnthropicPolicyVersion
	account.Extra["cc_gateway_routes"] = "native_messages"
	account.Extra["cc_gateway_egress_bucket_enabled"] = "true"
	account.Extra["cc_gateway_egress_bucket"] = "bucket-a"
	account.Extra["cc_gateway_account_ref"] = "hmac-sha256:" + strings.Repeat("a", 64)
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageRuntimeRegistered

	useCCGateway, err := svc.selectCCGatewayAnthropicRoute(account, ccGatewayRouteNativeMessages)
	require.False(t, useCCGateway)
	require.Error(t, err)
	require.Contains(t, err.Error(), "lifecycle ineligible")
}
