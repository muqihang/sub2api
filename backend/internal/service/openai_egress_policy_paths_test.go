package service

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func testOpenAIFailClosedMissingBucketConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.EgressFailClosed = true
	cfg.Gateway.OpenAICore.AllowAccountProxyFallback = false
	cfg.Gateway.OpenAICore.AllowDirectFallback = false
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{Name: "default", Enabled: true},
	}
	return cfg
}

func testOpenAIMissingBucketAPIKeyAccount(id int64) *Account {
	return &Account{
		ID:          id,
		Name:        "openai-apikey",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "test-api-key",
			"base_url": "https://api.openai.com",
		},
		Extra: map[string]any{
			"openai_gateway_egress_bucket": "missing",
		},
		Proxy: &Proxy{
			Protocol: "socks5",
			Host:     "10.0.0.2",
			Port:     1080,
		},
	}
}

func testOpenAIMissingBucketOAuthAccount(id int64) *Account {
	return &Account{
		ID:          id,
		Name:        "openai-oauth",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "test-access-token",
			"chatgpt_account_id": "chatgpt-account",
		},
		Extra: map[string]any{
			"openai_gateway_egress_bucket": "missing",
		},
		Proxy: &Proxy{
			Protocol: "socks5",
			Host:     "10.0.0.2",
			Port:     1080,
		},
	}
}

func requireOpenAIEgressPolicyError(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	var policyErr *OpenAIEgressPolicyError
	require.ErrorAs(t, err, &policyErr)
	require.Equal(t, code, policyErr.Code)
}

func TestOpenAIGatewayService_RawChatRejectsFailClosedEgressBeforeHTTPUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"gpt-5.3-codex","messages":[{"role":"user","content":"hello"}],"stream":false}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("content-type", "application/json")

	cfg := testOpenAIFailClosedMissingBucketConfig()
	core := NewOpenAIGatewayCoreService(nil, cfg, nil)
	upstream := &httpUpstreamRecorder{}
	svc := &OpenAIGatewayService{
		cfg:                cfg,
		gatewayCoreService: core,
		httpUpstream:       upstream,
	}

	result, err := svc.forwardAsRawChatCompletions(context.Background(), c, testOpenAIMissingBucketAPIKeyAccount(19101), body, "")
	require.Nil(t, result)
	requireOpenAIEgressPolicyError(t, err, "missing_bucket")
	require.Len(t, upstream.requests, 0)
}

func TestOpenAIGatewayService_ForwardImageGenerationRejectsFailClosedEgressBeforeHTTPUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"gpt-image-2","prompt":"apple","size":"1024x1024"}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	c.Request.Header.Set("content-type", "application/json")

	cfg := testOpenAIFailClosedMissingBucketConfig()
	core := NewOpenAIGatewayCoreService(nil, cfg, nil)
	upstream := &openAIImageHTTPUpstreamStub{}
	svc := &OpenAIGatewayService{
		cfg:                cfg,
		gatewayCoreService: core,
		httpUpstream:       upstream,
	}

	result, responseBody, responseHeaders, err := svc.ForwardImageGeneration(context.Background(), c, testOpenAIMissingBucketAPIKeyAccount(19102), body, "gpt-image-2", "")
	require.Nil(t, result)
	require.Nil(t, responseBody)
	require.Nil(t, responseHeaders)
	requireOpenAIEgressPolicyError(t, err, "missing_bucket")
	require.Empty(t, upstream.lastRequestURL)
}

func TestOpenAIGatewayService_ForwardImagesAPIKeyRejectsFailClosedEgressBeforeHTTPUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"gpt-image-2","prompt":"apple","size":"1024x1024"}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	c.Request.Header.Set("content-type", "application/json")

	cfg := testOpenAIFailClosedMissingBucketConfig()
	core := NewOpenAIGatewayCoreService(nil, cfg, nil)
	upstream := &httpUpstreamRecorder{}
	svc := &OpenAIGatewayService{
		cfg:                cfg,
		gatewayCoreService: core,
		httpUpstream:       upstream,
	}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	result, err := svc.ForwardImages(context.Background(), c, testOpenAIMissingBucketAPIKeyAccount(19103), body, parsed, "")
	require.Nil(t, result)
	requireOpenAIEgressPolicyError(t, err, "missing_bucket")
	require.Len(t, upstream.requests, 0)
}

func TestOpenAIGatewayService_ForwardImagesOAuthRejectsFailClosedEgressBeforeHTTPUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"gpt-image-2","prompt":"apple","size":"1024x1024"}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	c.Request.Header.Set("content-type", "application/json")

	cfg := testOpenAIFailClosedMissingBucketConfig()
	core := NewOpenAIGatewayCoreService(nil, cfg, nil)
	upstream := &httpUpstreamRecorder{}
	svc := &OpenAIGatewayService{
		cfg:                cfg,
		gatewayCoreService: core,
		httpUpstream:       upstream,
	}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	result, err := svc.ForwardImages(context.Background(), c, testOpenAIMissingBucketOAuthAccount(19104), body, parsed, "")
	require.Nil(t, result)
	requireOpenAIEgressPolicyError(t, err, "missing_bucket")
	require.Len(t, upstream.requests, 0)
}
