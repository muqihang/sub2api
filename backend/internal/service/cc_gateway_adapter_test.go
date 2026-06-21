package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestCCGatewayAnthropicPolicyVersionTracksClaudeCode2175Final(t *testing.T) {
	require.Equal(t, "2.1.175", ccGatewayAnthropicPolicyVersion)
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
	svc := &GatewayService{cfg: ccGatewayTestConfig(PlatformAnthropic)}

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
	require.Empty(t, getHeaderRaw(req.Header, ccGatewayTrustedPersonaHeader))
	require.Equal(t, "", getHeaderRaw(req.Header, "x-stainless-os"))
	require.NotContains(t, readRequestBody(t, req), "rewritten-device-id")
}

func TestGatewayService_CCGatewayAnthropicSetupTokenContext1MHeaderFollowsClientBeta(t *testing.T) {
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
	req, _, err := (&GatewayService{cfg: ccGatewayTestConfig(PlatformAnthropic)}).
		buildUpstreamRequest(context.Background(), c, account, body, "setup-token", "oauth", "claude-sonnet-4-6", true, false, false)
	require.NoError(t, err)

	require.Equal(t, "true", getHeaderRaw(req.Header, ccGatewayContext1MHeader))
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
	account := newAnthropicAPIKeyAccountForTest()
	account.Credentials["base_url"] = "https://must-not-be-used.example"
	account.Extra["cc_gateway_egress_bucket"] = "bucket-a"
	account.Extra["cc_gateway_enabled"] = "true"
	account.Extra["cc_gateway_canary_only"] = "false"
	account.Extra["cc_gateway_policy_version"] = ccGatewayAnthropicPolicyVersion
	account.Extra["cc_gateway_routes"] = "native_messages,native_count_tokens"
	account.Extra["cc_gateway_egress_bucket_enabled"] = "true"

	c := ccGatewayTestContext("/v1/messages")
	c.Request.Header.Set("x-app", "fake-cli")
	c.Request.Header.Set("x-claude-code-session-id", "client-session")
	c.Request.Header.Set("x-stainless-runtime", "fake-runtime")
	c.Request.Header.Set("x-sub2api-persona-trusted", "client-trust")
	req, _, err := (&GatewayService{cfg: ccGatewayTestConfig(PlatformAnthropic)}).
		buildUpstreamRequestAnthropicAPIKeyPassthrough(context.Background(), c, account, []byte(`{"model":"x"}`), "selected-api-key")
	require.NoError(t, err)

	require.Equal(t, "http://cc-gateway:8443/v1/messages?beta=true", req.URL.String())
	require.Equal(t, "selected-api-key", getHeaderRaw(req.Header, "x-api-key"))
	require.Equal(t, "", getHeaderRaw(req.Header, "authorization"))
	require.Equal(t, "ccg-token", getHeaderRaw(req.Header, "x-cc-gateway-token"))
	require.Equal(t, "201", getHeaderRaw(req.Header, "x-cc-account-id"))
	require.Equal(t, "anthropic", getHeaderRaw(req.Header, "x-cc-provider"))
	require.Equal(t, "apikey", getHeaderRaw(req.Header, "x-cc-token-type"))
	require.Equal(t, ccGatewayAnthropicPolicyVersion, getHeaderRaw(req.Header, "x-cc-policy-version"))
	require.Equal(t, "bucket-a", getHeaderRaw(req.Header, "x-cc-egress-bucket"))
	require.Empty(t, getHeaderRaw(req.Header, "anthropic-beta"))
	require.Empty(t, getHeaderRaw(req.Header, "x-app"))
	require.Empty(t, getHeaderRaw(req.Header, "x-claude-code-session-id"))
	require.Empty(t, getHeaderRaw(req.Header, "x-stainless-runtime"))
	require.Empty(t, getHeaderRaw(req.Header, "x-sub2api-persona-trusted"))
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

	c := ccGatewayTestContext("/v1/messages/count_tokens")
	c.Request.Header.Set("x-app", "fake-cli")
	c.Request.Header.Set("x-claude-code-session-id", "client-session")
	c.Request.Header.Set("x-sub2api-persona-trusted", "client-trust")
	req, err := (&GatewayService{cfg: ccGatewayTestConfig(PlatformAnthropic)}).
		buildCountTokensRequestAnthropicAPIKeyPassthrough(context.Background(), c, account, []byte(`{"model":"x"}`), "selected-api-key")
	require.NoError(t, err)

	require.Equal(t, "http://cc-gateway:8443/v1/messages/count_tokens?beta=true", req.URL.String())
	require.Equal(t, "selected-api-key", getHeaderRaw(req.Header, "x-api-key"))
	require.Equal(t, "", getHeaderRaw(req.Header, "authorization"))
	require.Equal(t, "apikey", getHeaderRaw(req.Header, "x-cc-token-type"))
	require.Equal(t, "201", getHeaderRaw(req.Header, "x-cc-account-id"))
	require.Equal(t, ccGatewayAnthropicPolicyVersion, getHeaderRaw(req.Header, "x-cc-policy-version"))
	require.Empty(t, getHeaderRaw(req.Header, "anthropic-beta"))
	require.Empty(t, getHeaderRaw(req.Header, "x-app"))
	require.Empty(t, getHeaderRaw(req.Header, "x-claude-code-session-id"))
	require.Empty(t, getHeaderRaw(req.Header, "x-sub2api-persona-trusted"))
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
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}],"stream":false}`)
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
	svc := &GatewayService{cfg: ccGatewayTestConfig(PlatformAnthropic)}
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
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}]}`)
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
	svc := &GatewayService{cfg: ccGatewayTestConfig(PlatformAnthropic)}
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
			"cc_gateway_policy_version":        "2.1.153",
			"cc_gateway_routes":                "native_messages,native_count_tokens,chat_completions,responses",
			"cc_gateway_egress_bucket_enabled": "true",
			"cc_gateway_egress_bucket":         "bucket-a",
		},
	}
	svc := &GatewayService{
		cfg:             ccGatewayTestConfig(PlatformAnthropic),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}

	ctx := WithCCGatewayExplicitCanaryRequest(SetClaudeCodeVersion(context.Background(), "2.1.153"), CCGatewayAnthropicCanaryRequest{
		AccountID:      account.ID,
		AccountHash:    "hmac-sha256:acct",
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
