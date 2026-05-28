package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
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

func ccGatewayTestContext(path string) *gin.Context {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.131 (external, cli)")
	c.Request.Header.Set("Anthropic-Beta", "client-beta")
	return c
}

func readRequestBody(t *testing.T, req *http.Request) string {
	t.Helper()
	body, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	req.Body = io.NopCloser(bytes.NewReader(body))
	return string(body)
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
			"account_uuid":             "acct-uuid",
			"organization_uuid":        "org-uuid",
			"cc_gateway_egress_bucket": "bucket-a",
		},
	}
	svc := &GatewayService{
		cfg:             ccGatewayTestConfig(PlatformAnthropic),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}

	ctx := SetClaudeCodeVersion(context.Background(), "2.1.150")
	req, err := svc.buildUpstreamRequest(ctx, ccGatewayTestContext("/v1/messages"), account, body, "selected-oauth-token", "oauth", "claude-3-7-sonnet-20250219", true, false)
	require.NoError(t, err)

	require.Equal(t, "http://cc-gateway:8443/v1/messages?beta=true", req.URL.String())
	require.Equal(t, "Bearer selected-oauth-token", getHeaderRaw(req.Header, "authorization"))
	require.Equal(t, "", getHeaderRaw(req.Header, "x-api-key"))
	require.Equal(t, "ccg-token", getHeaderRaw(req.Header, "x-cc-gateway-token"))
	require.Equal(t, "42", getHeaderRaw(req.Header, "x-cc-account-id"))
	require.Equal(t, "anthropic", getHeaderRaw(req.Header, "x-cc-provider"))
	require.Equal(t, "oauth", getHeaderRaw(req.Header, "x-cc-token-type"))
	require.Equal(t, "user@example.com", getHeaderRaw(req.Header, "x-cc-account-email"))
	require.Equal(t, "acct-uuid", getHeaderRaw(req.Header, "x-cc-account-uuid"))
	require.Equal(t, "org-uuid", getHeaderRaw(req.Header, "x-cc-organization-uuid"))
	require.Equal(t, "bucket-a", getHeaderRaw(req.Header, "x-cc-egress-bucket"))
	require.Equal(t, "2.1.150", getHeaderRaw(req.Header, "x-cc-policy-version"))
	require.Equal(t, "", getHeaderRaw(req.Header, "x-stainless-os"))
	require.NotContains(t, readRequestBody(t, req), "rewritten-device-id")
}

func TestGatewayService_CCGatewayAnthropicSetupTokenCountTokensBuildsTransparentRequest(t *testing.T) {
	account := &Account{
		ID:       43,
		Platform: PlatformAnthropic,
		Type:     AccountTypeSetupToken,
	}
	svc := &GatewayService{
		cfg:             ccGatewayTestConfig(PlatformAnthropic),
		identityService: NewIdentityService(ccGatewayIdentityCache{}),
	}

	req, err := svc.buildCountTokensRequest(context.Background(), ccGatewayTestContext("/v1/messages/count_tokens"), account, []byte(`{"model":"claude-3-7-sonnet-20250219"}`), "setup-token", "oauth", "claude-3-7-sonnet-20250219", false)
	require.NoError(t, err)

	require.Equal(t, "http://cc-gateway:8443/v1/messages/count_tokens?beta=true", req.URL.String())
	require.Equal(t, "Bearer setup-token", getHeaderRaw(req.Header, "authorization"))
	require.Equal(t, "oauth", getHeaderRaw(req.Header, "x-cc-token-type"))
	require.Equal(t, "43", getHeaderRaw(req.Header, "x-cc-account-id"))
	require.Contains(t, getHeaderRaw(req.Header, "anthropic-beta"), "token-counting")
	require.Equal(t, "", getHeaderRaw(req.Header, "x-stainless-os"))
}

func TestGatewayService_CCGatewayAnthropicAPIKeyPassthroughBuildsTransparentRequest(t *testing.T) {
	account := newAnthropicAPIKeyAccountForTest()
	account.Credentials["base_url"] = "https://must-not-be-used.example"
	account.Extra["cc_gateway_egress_bucket"] = ""
	account.Extra["openai_gateway_egress_bucket"] = "legacy-bucket"

	req, err := (&GatewayService{cfg: ccGatewayTestConfig(PlatformAnthropic)}).
		buildUpstreamRequestAnthropicAPIKeyPassthrough(context.Background(), ccGatewayTestContext("/v1/messages"), account, []byte(`{"model":"x"}`), "selected-api-key")
	require.NoError(t, err)

	require.Equal(t, "http://cc-gateway:8443/v1/messages?beta=true", req.URL.String())
	require.Equal(t, "selected-api-key", getHeaderRaw(req.Header, "x-api-key"))
	require.Equal(t, "", getHeaderRaw(req.Header, "authorization"))
	require.Equal(t, "ccg-token", getHeaderRaw(req.Header, "x-cc-gateway-token"))
	require.Equal(t, "201", getHeaderRaw(req.Header, "x-cc-account-id"))
	require.Equal(t, "anthropic", getHeaderRaw(req.Header, "x-cc-provider"))
	require.Equal(t, "apikey", getHeaderRaw(req.Header, "x-cc-token-type"))
	require.Equal(t, "legacy-bucket", getHeaderRaw(req.Header, "x-cc-egress-bucket"))
}

func TestGatewayService_CCGatewayAnthropicAPIKeyPassthroughCountTokensBuildsTransparentRequest(t *testing.T) {
	account := newAnthropicAPIKeyAccountForTest()

	req, err := (&GatewayService{cfg: ccGatewayTestConfig(PlatformAnthropic)}).
		buildCountTokensRequestAnthropicAPIKeyPassthrough(context.Background(), ccGatewayTestContext("/v1/messages/count_tokens"), account, []byte(`{"model":"x"}`), "selected-api-key")
	require.NoError(t, err)

	require.Equal(t, "http://cc-gateway:8443/v1/messages/count_tokens?beta=true", req.URL.String())
	require.Equal(t, "selected-api-key", getHeaderRaw(req.Header, "x-api-key"))
	require.Equal(t, "", getHeaderRaw(req.Header, "authorization"))
	require.Equal(t, "apikey", getHeaderRaw(req.Header, "x-cc-token-type"))
	require.Equal(t, "201", getHeaderRaw(req.Header, "x-cc-account-id"))
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
