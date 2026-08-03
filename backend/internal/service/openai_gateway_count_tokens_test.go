package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type countTokensRuntimeStateRepo struct {
	AccountRepository
	tempUnschedCalls int
	setErrorCalls    int
}

func (r *countTokensRuntimeStateRepo) SetTempUnschedulable(context.Context, int64, time.Time, string) error {
	r.tempUnschedCalls++
	return nil
}

func (r *countTokensRuntimeStateRepo) SetError(context.Context, int64, string) error {
	r.setErrorCalls++
	return nil
}

func TestOpenAIGatewayService_ForwardCountTokensAsAnthropic_APIKeyUsesResponsesInputTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"claude-sonnet-4-5","system":"You are helpful.","messages":[{"role":"user","content":"hello"}],"tools":[{"name":"lookup","input_schema":{"type":"object"}}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("User-Agent", "count-tokens-test")

	upstream := &captureHTTPUpstream{resp: jsonHTTPResponse(http.StatusOK, `{"object":"response.input_tokens","input_tokens":42}`)}
	svc := &OpenAIGatewayService{
		cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
			Enabled:           false,
			AllowInsecureHTTP: true,
		}}},
		httpUpstream: upstream,
	}
	account := &Account{
		ID:          101,
		Name:        "openai-apikey",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "http://upstream.example",
		},
		Status:      StatusActive,
		Schedulable: true,
	}

	err := svc.ForwardCountTokensAsAnthropic(context.Background(), c, account, body, "gpt-5.3-codex")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"input_tokens":42}`, rec.Body.String())
	require.Equal(t, 1, upstream.calls)
	require.Equal(t, "http://upstream.example/v1/responses/input_tokens", upstream.lastReq.URL.String())
	require.Equal(t, HTTPUpstreamProfileOpenAI, HTTPUpstreamProfileFromContext(upstream.lastReq.Context()))
	require.Equal(t, "Bearer sk-test", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "count-tokens-test", upstream.lastReq.Header.Get("User-Agent"))
	require.Equal(t, "gpt-5.3-codex", gjson.GetBytes(upstream.lastBody, "model").String())
	require.True(t, gjson.GetBytes(upstream.lastBody, "input").Exists())
	require.True(t, gjson.GetBytes(upstream.lastBody, "tools").Exists())
	require.False(t, gjson.GetBytes(upstream.lastBody, "messages").Exists())
	require.False(t, gjson.GetBytes(upstream.lastBody, "max_tokens").Exists())
}

func TestOpenAIGatewayService_ForwardCountTokensAsAnthropic_DoesNotEchoRawUpstreamBodyOnError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"claude-opus-4-1","messages":[{"role":"user","content":"hello"}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &captureHTTPUpstream{resp: &http.Response{
		StatusCode: http.StatusInternalServerError,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"server failed with raw-secret-body"}}`)),
	}}
	svc := &OpenAIGatewayService{
		cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
			Enabled:           false,
			AllowInsecureHTTP: true,
		}}},
		httpUpstream: upstream,
	}
	account := &Account{
		ID:          202,
		Name:        "openai-apikey",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "http://upstream.example",
		},
		Status:      StatusActive,
		Schedulable: true,
	}

	err := svc.ForwardCountTokensAsAnthropic(context.Background(), c, account, body, "gpt-5.4")
	require.Error(t, err)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.NotContains(t, rec.Body.String(), "raw-secret-body")
	require.NotContains(t, err.Error(), "raw-secret-body")
	if msg, ok := c.Get(OpsUpstreamErrorMessageKey); ok {
		require.NotContains(t, msg, "raw-secret-body")
	}
}

func TestOpenAIGatewayService_ForwardCountTokensAsAnthropic_OAuthScopeErrorFallsBackWithoutMutatingAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"claude-opus-4-1","system":"You are concise.","messages":[{"role":"user","content":"hello from count tokens"}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstreamBody := `{"error":{"type":"invalid_request_error","code":"missing_scope","message":"Missing scopes: api.responses.write raw-scope-marker"}}`
	upstream := &captureHTTPUpstream{resp: &http.Response{
		StatusCode: http.StatusForbidden,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	repo := &countTokensRuntimeStateRepo{}
	svc := &OpenAIGatewayService{
		cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
			Enabled:           false,
			AllowInsecureHTTP: true,
		}}},
		httpUpstream:     upstream,
		rateLimitService: &RateLimitService{accountRepo: repo, cfg: &config.Config{}},
	}
	account := &Account{
		ID:          303,
		Name:        "openai-oauth",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":  "oauth-access-token",
			"refresh_token": "oauth-refresh-token",
		},
		Status:      StatusActive,
		Schedulable: true,
	}

	err := svc.ForwardCountTokensAsAnthropic(context.Background(), c, account, body, "gpt-5.4")

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Greater(t, int(gjson.Get(rec.Body.String(), "input_tokens").Int()), 0)
	require.NotContains(t, rec.Body.String(), "api.responses.write")
	require.NotContains(t, rec.Body.String(), "raw-scope-marker")
	require.Zero(t, repo.tempUnschedCalls, "count_tokens OAuth scope fallback must not temp-unschedule the account")
	require.Zero(t, repo.setErrorCalls, "count_tokens OAuth scope fallback must not mark the account error")
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "https://api.openai.com/v1/responses/input_tokens", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer oauth-access-token", upstream.lastReq.Header.Get("Authorization"))
}

func TestOpenAIGatewayService_ForwardCountTokensAsAnthropic_APIKeyInputTokensUnsupportedKeepsNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"claude-opus-4-1","messages":[{"role":"user","content":"hello"}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &captureHTTPUpstream{resp: &http.Response{
		StatusCode: http.StatusNotFound,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"The /v1/responses/input_tokens endpoint was not found"}}`)),
	}}
	svc := &OpenAIGatewayService{
		cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
			Enabled:           false,
			AllowInsecureHTTP: true,
		}}},
		httpUpstream: upstream,
	}
	account := &Account{
		ID:          404,
		Name:        "openai-apikey",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "http://upstream.example",
		},
		Status:      StatusActive,
		Schedulable: true,
	}

	err := svc.ForwardCountTokensAsAnthropic(context.Background(), c, account, body, "gpt-5.4")

	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Body.String(), "Token counting is not supported by upstream")
}
