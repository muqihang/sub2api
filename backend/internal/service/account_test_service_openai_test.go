//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

// --- shared test helpers ---

type queuedHTTPUpstream struct {
	responses          []*http.Response
	requests           []*http.Request
	plainCalls         int
	tlsCalls           int
	tlsFlags           []bool
	tlsCacheIdentities []string
}

func (u *queuedHTTPUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.requests = append(u.requests, req)
	u.plainCalls++
	u.tlsFlags = append(u.tlsFlags, false)
	u.tlsCacheIdentities = append(u.tlsCacheIdentities, OpenAIHTTPUpstreamTLSCacheIdentity(req.Context()))
	return u.next()
}

func (u *queuedHTTPUpstream) DoWithTLS(req *http.Request, _ string, _ int64, _ int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	u.requests = append(u.requests, req)
	u.tlsCalls++
	u.tlsFlags = append(u.tlsFlags, profile != nil)
	u.tlsCacheIdentities = append(u.tlsCacheIdentities, OpenAIHTTPUpstreamTLSCacheIdentity(req.Context()))
	return u.next()
}

func (u *queuedHTTPUpstream) next() (*http.Response, error) {
	if len(u.responses) == 0 {
		return nil, fmt.Errorf("no mocked response")
	}
	resp := u.responses[0]
	u.responses = u.responses[1:]
	return resp, nil
}

func newJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// --- test functions ---

func newTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/1/test", nil)
	return c, rec
}

func newOpenAIAccountTestTLSConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.TLSBinding.Enabled = true
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{
			Name:    "default",
			Enabled: true,
			TLS: config.OpenAIGatewayBucketTLSConfig{
				Enabled:   true,
				ProfileID: 7,
			},
		},
	}
	return cfg
}

func newOpenAIAccountTestTLSService(resp *http.Response) (*AccountTestService, *queuedHTTPUpstream) {
	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}
	return &AccountTestService{
		httpUpstream:        upstream,
		cfg:                 newOpenAIAccountTestTLSConfig(),
		tlsFPProfileService: testOpenAITLSProfileService(&model.TLSFingerprintProfile{ID: 7, Name: "Account Test TLS"}),
	}, upstream
}

func requireOpenAIAccountTestUsedSharedTLSSender(t *testing.T, upstream *queuedHTTPUpstream) {
	t.Helper()
	require.Len(t, upstream.requests, 1)
	require.Equal(t, 0, upstream.plainCalls)
	require.Equal(t, 1, upstream.tlsCalls)
	require.Equal(t, []bool{true}, upstream.tlsFlags)
	require.Len(t, upstream.tlsCacheIdentities, 1)
	require.Contains(t, upstream.tlsCacheIdentities[0], "bucket=default")
	require.Contains(t, upstream.tlsCacheIdentities[0], "source=bucket")
}

func newOpenAIAccountTestCompletedSSE() *http.Response {
	resp := newJSONResponse(http.StatusOK, "")
	resp.Header.Set("Content-Type", "text/event-stream")
	resp.Body = io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\"}\n\n"))
	return resp
}

func newOpenAIAccountTestImageResponsesSSE() *http.Response {
	resp := newJSONResponse(http.StatusOK, "")
	resp.Header.Set("Content-Type", "text/event-stream")
	resp.Body = io.NopCloser(strings.NewReader(
		"data: {\"type\":\"response.output_item.done\",\"item\":{\"id\":\"ig_123\",\"type\":\"image_generation_call\",\"result\":\"aGVsbG8=\",\"revised_prompt\":\"draw a cat\",\"output_format\":\"png\"}}\n\n" +
			"data: {\"type\":\"response.completed\",\"response\":{\"created_at\":1710000006,\"output\":[]}}\n\n" +
			"data: [DONE]\n\n",
	))
	return resp
}

type openAIAccountTestRepo struct {
	mockAccountRepoForGemini
	updatedExtra   map[string]any
	rateLimitedID  int64
	rateLimitedAt  *time.Time
	clearedErrorID int64
	setErrorID     int64
	setErrorMsg    string
}

func (r *openAIAccountTestRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.updatedExtra = updates
	return nil
}

func (r *openAIAccountTestRepo) SetRateLimited(_ context.Context, id int64, resetAt time.Time) error {
	r.rateLimitedID = id
	r.rateLimitedAt = &resetAt
	return nil
}

func (r *openAIAccountTestRepo) ClearError(_ context.Context, id int64) error {
	r.clearedErrorID = id
	return nil
}

func (r *openAIAccountTestRepo) SetError(_ context.Context, id int64, errorMsg string) error {
	r.setErrorID = id
	r.setErrorMsg = errorMsg
	return nil
}

func TestAccountTestService_OpenAISuccessPersistsSnapshotFromHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, recorder := newTestContext()

	resp := newJSONResponse(http.StatusOK, "")
	resp.Body = io.NopCloser(strings.NewReader(`data: {"type":"response.completed"}

`))
	resp.Header.Set("x-codex-primary-used-percent", "88")
	resp.Header.Set("x-codex-primary-reset-after-seconds", "604800")
	resp.Header.Set("x-codex-primary-window-minutes", "10080")
	resp.Header.Set("x-codex-secondary-used-percent", "42")
	resp.Header.Set("x-codex-secondary-reset-after-seconds", "18000")
	resp.Header.Set("x-codex-secondary-window-minutes", "300")

	repo := &openAIAccountTestRepo{}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
	account := &Account{
		ID:          89,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "test-token"},
	}

	err := svc.testOpenAIAccountConnection(ctx, account, "gpt-5.4", "", "")
	require.NoError(t, err)
	require.NotEmpty(t, repo.updatedExtra)
	require.Equal(t, 42.0, repo.updatedExtra["codex_5h_used_percent"])
	require.Equal(t, 88.0, repo.updatedExtra["codex_7d_used_percent"])
	require.Contains(t, recorder.Body.String(), "test_complete")
}

func TestAccountTestService_OpenAIStreamEOFBeforeCompletedFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, recorder := newTestContext()

	resp := newJSONResponse(http.StatusOK, "")
	resp.Body = io.NopCloser(strings.NewReader(`data: {"type":"response.output_text.delta","delta":"hi"}

`))

	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}
	svc := &AccountTestService{httpUpstream: upstream}
	account := &Account{
		ID:          90,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "test-token"},
	}

	err := svc.testOpenAIAccountConnection(ctx, account, "gpt-5.4", "", "")
	require.Error(t, err)
	require.Contains(t, recorder.Body.String(), "response.completed")
	require.NotContains(t, recorder.Body.String(), `"success":true`)
}

func TestAccountTestService_OpenAIAPIKeyProbeRejectsFailClosedEgressBeforeHTTPUpstream(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.EgressFailClosed = true
	cfg.Gateway.OpenAICore.AllowAccountProxyFallback = false
	cfg.Gateway.OpenAICore.AllowDirectFallback = false
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{Name: "default", Enabled: true},
	}
	account := &Account{
		ID:          19005,
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
	repo := &openAIAccountTestRepo{
		mockAccountRepoForGemini: mockAccountRepoForGemini{
			accountsByID: map[int64]*Account{account.ID: account},
		},
	}
	upstream := &queuedHTTPUpstream{
		responses: []*http.Response{newJSONResponse(http.StatusOK, `{}`)},
	}
	svc := &AccountTestService{
		accountRepo:  repo,
		httpUpstream: upstream,
		cfg:          cfg,
	}

	svc.ProbeOpenAIAPIKeyResponsesSupport(context.Background(), account.ID)

	require.Len(t, upstream.requests, 0)
	require.Nil(t, repo.updatedExtra)
}

func TestAccountTestService_OpenAIAPIKeyProbeUsesSharedTLSAwareSenderMetadata(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.TLSBinding.Enabled = true
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{
			Name:    "default",
			Enabled: true,
			TLS: config.OpenAIGatewayBucketTLSConfig{
				Enabled:   true,
				ProfileID: 7,
			},
		},
	}
	account := &Account{
		ID:          19006,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "test-api-key",
			"base_url": "https://api.openai.com",
		},
	}
	repo := &openAIAccountTestRepo{
		mockAccountRepoForGemini: mockAccountRepoForGemini{
			accountsByID: map[int64]*Account{account.ID: account},
		},
	}
	upstream := &queuedHTTPUpstream{
		responses: []*http.Response{newJSONResponse(http.StatusOK, `{}`)},
	}
	svc := &AccountTestService{
		accountRepo:         repo,
		httpUpstream:        upstream,
		cfg:                 cfg,
		tlsFPProfileService: testOpenAITLSProfileService(&model.TLSFingerprintProfile{ID: 7, Name: "Probe TLS"}),
	}

	svc.ProbeOpenAIAPIKeyResponsesSupport(context.Background(), account.ID)

	require.Len(t, upstream.requests, 1)
	require.Equal(t, []bool{true}, upstream.tlsFlags)
	require.Len(t, upstream.tlsCacheIdentities, 1)
	require.Contains(t, upstream.tlsCacheIdentities[0], "bucket=default")
	require.Contains(t, upstream.tlsCacheIdentities[0], "source=bucket")
	require.NotNil(t, repo.updatedExtra)
}

func TestAccountTestService_OpenAIAccountConnectionUsesSharedTLSAwareSender(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := newTestContext()
	svc, upstream := newOpenAIAccountTestTLSService(newOpenAIAccountTestCompletedSSE())
	account := &Account{
		ID:          19007,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "test-access-token",
			"chatgpt_account_id": "chatgpt-account",
		},
	}

	err := svc.testOpenAIAccountConnection(ctx, account, "gpt-5.4", "", "")

	require.NoError(t, err)
	requireOpenAIAccountTestUsedSharedTLSSender(t, upstream)
	require.Equal(t, "Bearer test-access-token", upstream.requests[0].Header.Get("Authorization"))
	require.Equal(t, "chatgpt.com", upstream.requests[0].Host)
	require.Equal(t, "chatgpt-account", upstream.requests[0].Header.Get("chatgpt-account-id"))
}

func TestAccountTestService_OpenAICompactConnectionUsesSharedTLSAwareSender(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := newTestContext()
	svc, upstream := newOpenAIAccountTestTLSService(newJSONResponse(http.StatusOK, `{"id":"cmp_probe","status":"completed"}`))
	account := &Account{
		ID:          19008,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "test-access-token",
			"chatgpt_account_id": "chatgpt-account",
		},
	}

	err := svc.testOpenAICompactConnection(ctx, account, "gpt-5.4")

	require.NoError(t, err)
	requireOpenAIAccountTestUsedSharedTLSSender(t, upstream)
	require.Equal(t, "Bearer test-access-token", upstream.requests[0].Header.Get("Authorization"))
	require.Equal(t, "responses=experimental", upstream.requests[0].Header.Get("OpenAI-Beta"))
	require.Equal(t, codexCLIUserAgent, upstream.requests[0].Header.Get("User-Agent"))
	require.NotEmpty(t, upstream.requests[0].Header.Get("Session_ID"))
	require.Equal(t, "chatgpt-account", upstream.requests[0].Header.Get("chatgpt-account-id"))
}

func TestAccountTestService_OpenAIImageAPIKeyUsesSharedTLSAwareSender(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := newTestContext()
	svc, upstream := newOpenAIAccountTestTLSService(newJSONResponse(http.StatusOK, `{"data":[{"b64_json":"aGVsbG8=","revised_prompt":"draw a cat"}]}`))
	account := &Account{
		ID:          19009,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "test-api-key",
			"base_url": "https://api.openai.com",
		},
	}

	err := svc.testOpenAIImageAPIKey(ctx, context.Background(), account, "gpt-image-2", "draw a cat")

	require.NoError(t, err)
	requireOpenAIAccountTestUsedSharedTLSSender(t, upstream)
	require.Equal(t, "https://api.openai.com/v1/images/generations", upstream.requests[0].URL.String())
	require.Equal(t, "Bearer test-api-key", upstream.requests[0].Header.Get("Authorization"))
}

func TestAccountTestService_OpenAIImageOAuthUsesSharedTLSAwareSender(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := newTestContext()
	svc, upstream := newOpenAIAccountTestTLSService(newOpenAIAccountTestImageResponsesSSE())
	account := &Account{
		ID:          19010,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "test-access-token",
			"chatgpt_account_id": "chatgpt-account",
		},
	}

	err := svc.testOpenAIImageOAuth(ctx, context.Background(), account, "gpt-image-2", "draw a cat")

	require.NoError(t, err)
	requireOpenAIAccountTestUsedSharedTLSSender(t, upstream)
	require.Equal(t, chatgptCodexAPIURL, upstream.requests[0].URL.String())
	require.Equal(t, "Bearer test-access-token", upstream.requests[0].Header.Get("Authorization"))
	require.Equal(t, "chatgpt.com", upstream.requests[0].Host)
	require.Equal(t, "chatgpt-account", upstream.requests[0].Header.Get("chatgpt-account-id"))
}

func TestAccountTestService_OpenAICompactRejectsFailClosedEgressBeforeHTTPUpstream(t *testing.T) {
	cfg := testOpenAIFailClosedMissingBucketConfig()
	account := testOpenAIMissingBucketOAuthAccount(19011)
	upstream := &queuedHTTPUpstream{
		responses: []*http.Response{newJSONResponse(http.StatusOK, `{}`)},
	}
	svc := &AccountTestService{
		httpUpstream: upstream,
		cfg:          cfg,
	}
	ctx, recorder := newTestContext()

	err := svc.testOpenAICompactConnection(ctx, account, "gpt-5.4")

	require.Error(t, err)
	require.Contains(t, err.Error(), "openai egress policy rejected request")
	require.Contains(t, recorder.Body.String(), "openai egress policy rejected request")
	require.Len(t, upstream.requests, 0)
}

func TestAccountTestService_OpenAI429PersistsSnapshotAndRateLimitState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := newTestContext()

	resp := newJSONResponse(http.StatusTooManyRequests, `{"error":{"type":"usage_limit_reached","message":"limit reached","resets_at":1777283883}}`)
	resp.Header.Set("x-codex-primary-used-percent", "100")
	resp.Header.Set("x-codex-primary-reset-after-seconds", "604800")
	resp.Header.Set("x-codex-primary-window-minutes", "10080")
	resp.Header.Set("x-codex-secondary-used-percent", "100")
	resp.Header.Set("x-codex-secondary-reset-after-seconds", "18000")
	resp.Header.Set("x-codex-secondary-window-minutes", "300")

	repo := &openAIAccountTestRepo{}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
	account := &Account{
		ID:          88,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusError,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "test-token"},
	}

	err := svc.testOpenAIAccountConnection(ctx, account, "gpt-5.4", "", "")
	require.Error(t, err)
	require.NotEmpty(t, repo.updatedExtra)
	require.Equal(t, 100.0, repo.updatedExtra["codex_5h_used_percent"])
	require.Equal(t, account.ID, repo.rateLimitedID)
	require.NotNil(t, repo.rateLimitedAt)
	require.Equal(t, account.ID, repo.clearedErrorID)
	require.Equal(t, StatusActive, account.Status)
	require.Empty(t, account.ErrorMessage)
	require.NotNil(t, account.RateLimitResetAt)
}

func TestAccountTestService_OpenAI429BodyOnlyPersistsRateLimitAndClearsStaleError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := newTestContext()

	resp := newJSONResponse(http.StatusTooManyRequests, `{"error":{"type":"usage_limit_reached","message":"limit reached","resets_at":"1777283883"}}`)

	repo := &openAIAccountTestRepo{}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
	account := &Account{
		ID:           77,
		Platform:     PlatformOpenAI,
		Type:         AccountTypeOAuth,
		Status:       StatusError,
		ErrorMessage: "Access forbidden (403): account may be suspended or lack permissions",
		Concurrency:  1,
		Credentials:  map[string]any{"access_token": "test-token"},
	}

	err := svc.testOpenAIAccountConnection(ctx, account, "gpt-5.4", "", "")
	require.Error(t, err)
	require.Equal(t, account.ID, repo.rateLimitedID)
	require.NotNil(t, repo.rateLimitedAt)
	require.Equal(t, account.ID, repo.clearedErrorID)
	require.Equal(t, StatusActive, account.Status)
	require.Empty(t, account.ErrorMessage)
	require.NotNil(t, account.RateLimitResetAt)
	require.Empty(t, repo.updatedExtra)
}

func TestAccountTestService_OpenAI429ActiveAccountDoesNotClearError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := newTestContext()

	resp := newJSONResponse(http.StatusTooManyRequests, `{"error":{"type":"usage_limit_reached","message":"limit reached","resets_in_seconds":3600}}`)

	repo := &openAIAccountTestRepo{}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
	account := &Account{
		ID:          78,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "test-token"},
	}

	err := svc.testOpenAIAccountConnection(ctx, account, "gpt-5.4", "", "")
	require.Error(t, err)
	require.Equal(t, account.ID, repo.rateLimitedID)
	require.NotNil(t, repo.rateLimitedAt)
	require.Zero(t, repo.clearedErrorID)
	require.Equal(t, StatusActive, account.Status)
	require.NotNil(t, account.RateLimitResetAt)
}

func TestAccountTestService_OpenAI429WithoutResetSignalDoesNotMutateRuntimeState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := newTestContext()

	resp := newJSONResponse(http.StatusTooManyRequests, `{"error":{"type":"usage_limit_reached","message":"limit reached"}}`)

	repo := &openAIAccountTestRepo{}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
	account := &Account{
		ID:           79,
		Platform:     PlatformOpenAI,
		Type:         AccountTypeOAuth,
		Status:       StatusError,
		ErrorMessage: "stale 403",
		Concurrency:  1,
		Credentials:  map[string]any{"access_token": "test-token"},
	}

	err := svc.testOpenAIAccountConnection(ctx, account, "gpt-5.4", "", "")
	require.Error(t, err)
	require.Zero(t, repo.rateLimitedID)
	require.Nil(t, repo.rateLimitedAt)
	require.Zero(t, repo.clearedErrorID)
	require.Equal(t, StatusError, account.Status)
	require.Equal(t, "stale 403", account.ErrorMessage)
	require.Nil(t, account.RateLimitResetAt)
}

func TestAccountTestService_OpenAI401SetsPermanentErrorOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := newTestContext()

	resp := newJSONResponse(http.StatusUnauthorized, `{"error":"bad token"}`)

	repo := &openAIAccountTestRepo{}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
	account := &Account{
		ID:          80,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "test-token"},
	}

	err := svc.testOpenAIAccountConnection(ctx, account, "gpt-5.4", "", "")
	require.Error(t, err)
	require.Equal(t, account.ID, repo.setErrorID)
	require.Contains(t, repo.setErrorMsg, "Authentication failed (401)")
	require.Zero(t, repo.rateLimitedID)
	require.Zero(t, repo.clearedErrorID)
	require.Nil(t, account.RateLimitResetAt)
}
