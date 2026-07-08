package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type openAIOAuthHandlerClientStub struct{}

func (s *openAIOAuthHandlerClientStub) ExchangeCode(ctx context.Context, code, codeVerifier, redirectURI, proxyURL, clientID string) (*openai.TokenResponse, error) {
	return &openai.TokenResponse{
		AccessToken:  "at",
		RefreshToken: "rt",
		ExpiresIn:    3600,
	}, nil
}

func (s *openAIOAuthHandlerClientStub) RefreshToken(ctx context.Context, refreshToken, proxyURL string) (*openai.TokenResponse, error) {
	return nil, nil
}

func (s *openAIOAuthHandlerClientStub) RefreshTokenWithClientID(ctx context.Context, refreshToken, proxyURL string, clientID string) (*openai.TokenResponse, error) {
	return &openai.TokenResponse{
		AccessToken:  "refreshed-at",
		RefreshToken: refreshToken,
		ExpiresIn:    3600,
	}, nil
}

func TestOpenAIOAuthHandler_GatewayTemplates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewOpenAIOAuthHandler(nil, nil, newStubAdminService(), nil)
	router.GET("/api/v1/admin/openai/gateway/templates", h.GatewayTemplates)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/openai/gateway/templates?base_url=https://api.example.com&api_key=sk-user&gateway_token=gw-123", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "X-OpenAI-Gateway-Token")
	require.Contains(t, rec.Body.String(), "codex")
	require.Contains(t, rec.Body.String(), "https://api.example.com")
}

func TestOpenAIOAuthHandler_GatewayStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{Name: "default", Enabled: true},
	}
	repo := &openAIGatewayCoreAdminRepo{
		accountsByID: map[int64]*service.Account{
			1: {
				ID:       1,
				Name:     "acc-1",
				Platform: service.PlatformOpenAI,
				Type:     service.AccountTypeOAuth,
				Status:   service.StatusActive,
				Extra: map[string]any{
					"openai_gateway_profile_id":    "profile-1",
					"openai_gateway_profile_mode":  service.OpenAIGatewayProfileModeFixed,
					"openai_gateway_egress_bucket": "default",
					"openai_auth_state":            service.OpenAIAuthStateHealthy,
					"openai_pool_role":             service.OpenAIPoolRoleMain,
					"openai_token_source":          service.OpenAITokenSourceRTManaged,
				},
			},
		},
	}
	core := service.NewOpenAIGatewayCoreService(repo, cfg, nil)
	gateway := service.NewOpenAIGatewayService(nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil, core, nil, nil, nil)
	h := NewOpenAIOAuthHandler(nil, gateway, newStubAdminService(), nil)
	router.GET("/api/v1/admin/openai/gateway/status", h.GatewayStatus)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/openai/gateway/status", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "\"profile_id\":\"profile-1\"")
	require.Contains(t, rec.Body.String(), "\"gateway_status\"")
}

func TestOpenAIOAuthHandler_UpdateGatewayRuntime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{Name: "default", Enabled: true},
		{Name: "bucket-a", Enabled: true, ProxyURL: "http://127.0.0.1:8080"},
	}
	core := service.NewOpenAIGatewayCoreService(&openAIGatewayCoreAdminRepo{}, cfg, nil)
	adminSvc := newStubAdminService()
	adminSvc.accounts = []service.Account{
		{
			ID:       3,
			Name:     "openai-acc",
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
			Extra:    map[string]any{},
		},
	}
	gateway := service.NewOpenAIGatewayService(nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil, core, nil, nil, nil)
	h := NewOpenAIOAuthHandler(nil, gateway, adminSvc, nil)
	router.POST("/api/v1/admin/openai/gateway/accounts/:id/runtime", h.UpdateGatewayRuntime)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/openai/gateway/accounts/3/runtime", strings.NewReader(`{"egress_bucket":"bucket-a","profile_mode":"frozen"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, adminSvc.updatedAccounts, 1)
	require.Equal(t, "bucket-a", adminSvc.updatedAccounts[0].input.Extra["openai_gateway_egress_bucket"])
	require.Equal(t, "frozen", adminSvc.updatedAccounts[0].input.Extra["openai_gateway_profile_mode"])
}

func TestOpenAIOAuthHandler_UpdateGatewayRuntimeSetsTLSPolicyAndPreservesRuntimeFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	core := testOpenAIAccountTLSGatewayCore(true, 7)
	adminSvc := newStubAdminService()
	adminSvc.accounts = []service.Account{
		{
			ID:       3,
			Name:     "openai-acc",
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
			Extra: map[string]any{
				"openai_gateway_egress_bucket": "default",
				"openai_gateway_profile_mode":  service.OpenAIGatewayProfileModeFrozen,
			},
		},
	}
	gateway := service.NewOpenAIGatewayService(nil, nil, nil, nil, nil, nil, nil, &config.Config{}, nil, nil, nil, nil, nil, nil, nil, nil, core, nil, nil, nil)
	h := NewOpenAIOAuthHandler(nil, gateway, adminSvc, nil)
	router.POST("/api/v1/admin/openai/gateway/accounts/:id/runtime", h.UpdateGatewayRuntime)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/openai/gateway/accounts/3/runtime", strings.NewReader(`{"openai_gateway_tls":{"enabled":true,"profile_id":7}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, adminSvc.updatedAccounts, 1)
	extra := adminSvc.updatedAccounts[0].input.Extra
	require.Equal(t, "default", extra["openai_gateway_egress_bucket"])
	require.Equal(t, service.OpenAIGatewayProfileModeFrozen, extra["openai_gateway_profile_mode"])
	require.Equal(t, map[string]any{"enabled": true, "profile_id": int64(7)}, extra["openai_gateway_tls"])
}

func TestOpenAIOAuthHandler_UpdateGatewayRuntimeRejectsTLSOverrideWhenBucketDisallows(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	core := testOpenAIAccountTLSGatewayCore(false, 7)
	adminSvc := newStubAdminService()
	adminSvc.accounts = []service.Account{
		{
			ID:       3,
			Name:     "openai-acc",
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
			Extra: map[string]any{
				"openai_gateway_egress_bucket": "default",
			},
		},
	}
	gateway := service.NewOpenAIGatewayService(nil, nil, nil, nil, nil, nil, nil, &config.Config{}, nil, nil, nil, nil, nil, nil, nil, nil, core, nil, nil, nil)
	h := NewOpenAIOAuthHandler(nil, gateway, adminSvc, nil)
	router.POST("/api/v1/admin/openai/gateway/accounts/:id/runtime", h.UpdateGatewayRuntime)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/openai/gateway/accounts/3/runtime", strings.NewReader(`{"openai_gateway_tls":{"enabled":true,"profile_id":7}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Empty(t, adminSvc.updatedAccounts)
}

func TestOpenAIOAuthHandler_CreateAccountFromOAuthPersistsEgressBucket(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.EgressFailClosed = true
	cfg.Gateway.OpenAICore.AllowAccountProxyFallback = false
	cfg.Gateway.OpenAICore.AllowDirectFallback = false
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "bucket-a"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{Name: "bucket-a", Enabled: true, ProxyURL: "http://127.0.0.1:8080"},
	}
	core := service.NewOpenAIGatewayCoreService(&openAIGatewayCoreAdminRepo{}, cfg, nil)
	oauthSvc := service.NewOpenAIOAuthService(nil, &openAIOAuthHandlerClientStub{})
	defer oauthSvc.Stop()
	oauthSvc.SetGatewayCoreService(core)

	authResult, err := oauthSvc.GenerateAuthURLWithEgress(context.Background(), nil, "", service.PlatformOpenAI, "bucket-a")
	require.NoError(t, err)
	parsed, err := url.Parse(authResult.AuthURL)
	require.NoError(t, err)
	state := parsed.Query().Get("state")
	require.NotEmpty(t, state)

	adminSvc := newStubAdminService()
	h := NewOpenAIOAuthHandler(oauthSvc, nil, adminSvc, nil)
	router.POST("/api/v1/admin/openai/create-from-oauth", h.CreateAccountFromOAuth)

	body := `{"session_id":"` + authResult.SessionID + `","code":"auth-code","state":"` + state + `","name":"oauth-account"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/openai/create-from-oauth", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, adminSvc.createdAccounts, 1)
	require.Equal(t, service.PlatformOpenAI, adminSvc.createdAccounts[0].Platform)
	require.Equal(t, service.AccountTypeOAuth, adminSvc.createdAccounts[0].Type)
	require.Equal(t, "bucket-a", adminSvc.createdAccounts[0].Extra["openai_gateway_egress_bucket"])
	require.Equal(t, "disabled", adminSvc.createdAccounts[0].Extra["openai_runtime_guard_content_safety_mode"])
}

func TestOpenAIOAuthHandler_RefreshTokenReturnsEgressBucket(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.EgressFailClosed = true
	cfg.Gateway.OpenAICore.AllowAccountProxyFallback = false
	cfg.Gateway.OpenAICore.AllowDirectFallback = false
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "bucket-a"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{Name: "bucket-a", Enabled: true, ProxyURL: "http://user:pass@127.0.0.1:8080"},
	}
	core := service.NewOpenAIGatewayCoreService(&openAIGatewayCoreAdminRepo{}, cfg, nil)
	oauthSvc := service.NewOpenAIOAuthService(nil, &openAIOAuthHandlerClientStub{})
	defer oauthSvc.Stop()
	oauthSvc.SetGatewayCoreService(core)
	h := NewOpenAIOAuthHandler(oauthSvc, nil, newStubAdminService(), nil)
	router.POST("/api/v1/admin/openai/refresh-token", h.RefreshToken)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/openai/refresh-token", strings.NewReader(`{"refresh_token":"rt","egress_bucket":"bucket-a"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"egress_bucket":"bucket-a"`)
	require.Contains(t, rec.Body.String(), `"proxy_label":"http://127.0.0.1:8080"`)
	require.NotContains(t, rec.Body.String(), "user:pass")
}

func TestOpenAIOAuthHandler_GatewayTemplatesDownload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewOpenAIOAuthHandler(nil, nil, newStubAdminService(), nil)
	router.GET("/api/v1/admin/openai/gateway/templates/download", h.DownloadGatewayTemplate)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/openai/gateway/templates/download?base_url=https://api.example.com&api_key=sk-user&gateway_token=gw-123&format=codex-wrapper.sh", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "attachment; filename=codex-wrapper.sh", rec.Header().Get("Content-Disposition"))
	require.Contains(t, rec.Body.String(), "OPENAI_GATEWAY_TOKEN")
}

type openAIGatewayCoreAdminRepo struct {
	service.AccountRepository
	accountsByID map[int64]*service.Account
}

func (r *openAIGatewayCoreAdminRepo) GetByID(ctx context.Context, id int64) (*service.Account, error) {
	if acc, ok := r.accountsByID[id]; ok {
		return acc, nil
	}
	return nil, service.ErrAccountNotFound
}

func (r *openAIGatewayCoreAdminRepo) ListByPlatform(ctx context.Context, platform string) ([]service.Account, error) {
	var out []service.Account
	for _, acc := range r.accountsByID {
		if acc.Platform == platform {
			out = append(out, *acc)
		}
	}
	return out, nil
}

func (r *openAIGatewayCoreAdminRepo) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
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
