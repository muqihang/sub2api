package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIGatewayHandler_HealthRequiresClientTokenWhenConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.ClientTokens = []config.OpenAIGatewayClientTokenConfig{
		{Name: "probe", Token: "tok-123"},
	}

	core := service.NewOpenAIGatewayCoreService(&serviceMockAccountRepo{}, cfg, nil)
	h := NewOpenAIGatewayHandler(nil, core, nil, nil, nil, nil, nil, cfg)

	router := gin.New()
	router.GET("/openai/_health", h.Health)

	req := httptest.NewRequest(http.MethodGet, "/openai/_health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	req = httptest.NewRequest(http.MethodGet, "/openai/_health", nil)
	req.Header.Set(service.OpenAIGatewayClientTokenHeader, "tok-123")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestOpenAIGatewayHandler_VerifyReturnsProfileAndBucket(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.ClientTokens = []config.OpenAIGatewayClientTokenConfig{
		{Name: "probe", Token: "tok-123"},
	}

	repo := &serviceMockAccountRepo{
		accountsByID: map[int64]*service.Account{
			1: {
				ID:       1,
				Platform: service.PlatformOpenAI,
				Type:     service.AccountTypeOAuth,
				Status:   service.StatusActive,
				Credentials: map[string]any{
					"chatgpt_account_id": "acct-1",
				},
			},
		},
	}
	core := service.NewOpenAIGatewayCoreService(repo, cfg, nil)
	h := NewOpenAIGatewayHandler(nil, core, nil, nil, nil, nil, nil, cfg)

	router := gin.New()
	router.GET("/openai/_verify", h.Verify)

	req := httptest.NewRequest(http.MethodGet, "/openai/_verify?account_id=1&transport=http", nil)
	req.Header.Set(service.OpenAIGatewayClientTokenHeader, "tok-123")
	req.Header.Set("User-Agent", "codex_cli_rs/0.200.0")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "\"profile_id\"")
	require.Contains(t, rec.Body.String(), "\"egress_bucket\":\"default\"")
}

func TestOpenAIGatewayHandler_VerifyRedactsProxyURLByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.ClientTokens = []config.OpenAIGatewayClientTokenConfig{
		{Name: "probe", Token: "tok-123"},
	}
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "bucket-a"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{Name: "bucket-a", Enabled: true, ProxyURL: "http://user:pass@127.0.0.1:8080/path?q=1"},
	}

	repo := &serviceMockAccountRepo{
		accountsByID: map[int64]*service.Account{
			1: {
				ID:       1,
				Platform: service.PlatformOpenAI,
				Type:     service.AccountTypeOAuth,
				Status:   service.StatusActive,
				Credentials: map[string]any{
					"chatgpt_account_id": "acct-1",
				},
				Extra: map[string]any{
					"openai_gateway_egress_bucket": "bucket-a",
				},
			},
		},
	}
	core := service.NewOpenAIGatewayCoreService(repo, cfg, nil)
	h := NewOpenAIGatewayHandler(nil, core, nil, nil, nil, nil, nil, cfg)

	router := gin.New()
	router.GET("/openai/_verify", h.Verify)

	req := httptest.NewRequest(http.MethodGet, "/openai/_verify?account_id=1&transport=http", nil)
	req.Header.Set(service.OpenAIGatewayClientTokenHeader, "tok-123")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "\"proxy_selected\":true")
	require.Contains(t, body, "\"proxy_label\":\"http://127.0.0.1:8080\"")
	require.Contains(t, body, "\"proxy_hash\":\"")
	require.NotContains(t, body, "\"debug_proxy_url\"")
	require.NotContains(t, body, "\"proxy_url\"")
	require.NotContains(t, body, "user:pass")
	require.NotContains(t, body, "q=1")
}

func TestOpenAIGatewayHandler_EnforceOptionalGatewayClientAuthRejectsInvalidToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.ClientTokens = []config.OpenAIGatewayClientTokenConfig{
		{Name: "probe", Token: "tok-123"},
	}

	core := service.NewOpenAIGatewayCoreService(&serviceMockAccountRepo{}, cfg, nil)
	h := NewOpenAIGatewayHandler(nil, core, nil, nil, nil, nil, nil, cfg)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", nil)
	c.Request.Header.Set(service.OpenAIGatewayClientTokenHeader, "bad-token")

	ok := h.enforceOptionalGatewayClientAuth(c, nil)
	require.False(t, ok)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestOpenAIGatewayHandler_EnforceOptionalGatewayClientAuthRequiresConfiguredToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.ClientTokens = []config.OpenAIGatewayClientTokenConfig{
		{Name: "probe", Token: "tok-123"},
	}

	core := service.NewOpenAIGatewayCoreService(&serviceMockAccountRepo{}, cfg, nil)
	h := NewOpenAIGatewayHandler(nil, core, nil, nil, nil, nil, nil, cfg)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", nil)

	ok := h.enforceOptionalGatewayClientAuth(c, nil)
	require.False(t, ok)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, rec.Body.String(), "OpenAI gateway client token required")
}

type serviceMockAccountRepo struct {
	service.AccountRepository
	accountsByID map[int64]*service.Account
}

func (r *serviceMockAccountRepo) GetByID(ctx context.Context, id int64) (*service.Account, error) {
	if acc, ok := r.accountsByID[id]; ok {
		return acc, nil
	}
	return nil, service.ErrAccountNotFound
}

func (r *serviceMockAccountRepo) ListByPlatform(ctx context.Context, platform string) ([]service.Account, error) {
	var out []service.Account
	for _, acc := range r.accountsByID {
		if acc.Platform == platform {
			out = append(out, *acc)
		}
	}
	return out, nil
}

func (r *serviceMockAccountRepo) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	if acc, ok := r.accountsByID[id]; ok {
		if acc.Extra == nil {
			acc.Extra = map[string]any{}
		}
		for k, v := range updates {
			acc.Extra[k] = v
		}
	}
	return nil
}
