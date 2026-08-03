package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type geminiOAuthHandlerClientStub struct{}

func (s *geminiOAuthHandlerClientStub) ExchangeCode(ctx context.Context, oauthType, code, codeVerifier, redirectURI, proxyURL string) (*geminicli.TokenResponse, error) {
	return &geminicli.TokenResponse{
		AccessToken:  "gem-at",
		RefreshToken: "gem-rt",
		ExpiresIn:    3600,
		TokenType:    "Bearer",
		Scope:        "openid profile",
	}, nil
}

func (s *geminiOAuthHandlerClientStub) RefreshToken(ctx context.Context, oauthType, refreshToken, proxyURL string) (*geminicli.TokenResponse, error) {
	return &geminicli.TokenResponse{
		AccessToken:  "gem-refreshed-at",
		RefreshToken: refreshToken,
		ExpiresIn:    3600,
		TokenType:    "Bearer",
		Scope:        "openid profile",
	}, nil
}

type geminiOAuthHandlerCodeAssistStub struct{}

func (s *geminiOAuthHandlerCodeAssistStub) LoadCodeAssist(ctx context.Context, accessToken, proxyURL string, req *geminicli.LoadCodeAssistRequest) (*geminicli.LoadCodeAssistResponse, error) {
	return &geminicli.LoadCodeAssistResponse{
		CloudAICompanionProject: "projects/test-gemini",
	}, nil
}

func (s *geminiOAuthHandlerCodeAssistStub) OnboardUser(ctx context.Context, accessToken, proxyURL string, req *geminicli.OnboardUserRequest) (*geminicli.OnboardUserResponse, error) {
	return nil, nil
}

func TestSanitizeGeminiTokenInfoForResponseAddsStatusFields(t *testing.T) {
	cfg := &config.Config{}
	svc := service.NewGeminiOAuthService(nil, nil, nil, nil, cfg)
	defer svc.Stop()

	tokenInfo := &service.GeminiTokenInfo{
		OAuthType: "google_one",
		ProjectID: "proj-1",
		TierID:    service.GeminiTierGoogleOneFree,
		Extra: map[string]any{
			"gemini_oauth_reason": "google_one_default_tier_fallback",
		},
	}

	augmented := sanitizeGeminiTokenInfoForResponse(svc, tokenInfo)
	require.NotNil(t, augmented)
	require.Equal(t, "present", augmented.Extra["project_id_status"])
	require.Equal(t, "default_fallback", augmented.Extra["tier_status"])
	require.Equal(t, "memory", augmented.Extra["session_store"])
	payload, err := json.Marshal(augmented)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "access_token")
	require.NotContains(t, string(payload), "refresh_token")
}

func TestSanitizeGeminiTokenInfoForResponseOptionalProjectID(t *testing.T) {
	cfg := &config.Config{}
	svc := service.NewGeminiOAuthService(nil, nil, nil, nil, cfg)
	defer svc.Stop()

	tokenInfo := &service.GeminiTokenInfo{
		OAuthType: "ai_studio",
		Extra:     map[string]any{},
	}

	augmented := sanitizeGeminiTokenInfoForResponse(svc, tokenInfo)
	require.NotNil(t, augmented)
	require.Equal(t, "optional_empty", augmented.Extra["project_id_status"])
	require.Equal(t, "missing", augmented.Extra["tier_status"])
	require.True(t, strings.Contains(augmented.Extra["session_store"].(string), "memory"))
}

func TestSanitizeGeminiTokenInfoForResponseGoogleOneWithoutProjectIDIsOptional(t *testing.T) {
	cfg := &config.Config{}
	svc := service.NewGeminiOAuthService(nil, nil, nil, nil, cfg)
	defer svc.Stop()

	tokenInfo := &service.GeminiTokenInfo{
		OAuthType: "google_one",
		TierID:    service.GeminiTierGoogleOneFree,
	}

	augmented := sanitizeGeminiTokenInfoForResponse(svc, tokenInfo)
	require.NotNil(t, augmented)
	require.Equal(t, "optional_empty", augmented.Extra["project_id_status"])
	require.Equal(t, "present", augmented.Extra["tier_status"])
}

func TestGeminiOAuthHandler_CreateAccountFromOAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	cfg := &config.Config{}
	oauthSvc := service.NewGeminiOAuthService(nil, &geminiOAuthHandlerClientStub{}, &geminiOAuthHandlerCodeAssistStub{}, nil, cfg)
	defer oauthSvc.Stop()

	authResult, err := oauthSvc.GenerateAuthURL(context.Background(), nil, "http://localhost:3000/auth/callback", "", "google_one", service.GeminiTierGoogleOneFree)
	require.NoError(t, err)
	parsed, err := url.Parse(authResult.AuthURL)
	require.NoError(t, err)
	state := parsed.Query().Get("state")
	require.NotEmpty(t, state)

	adminSvc := newStubAdminService()
	h := NewGeminiOAuthHandler(oauthSvc, adminSvc)
	router.POST("/api/v1/admin/gemini/create-from-oauth", h.CreateAccountFromOAuth)

	body := `{"session_id":"` + authResult.SessionID + `","code":"auth-code","state":"` + state + `","oauth_type":"google_one","tier_id":"google_one_free","name":"gemini-live","concurrency":1,"priority":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/gemini/create-from-oauth", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, adminSvc.createdAccounts, 1)
	created := adminSvc.createdAccounts[0]
	require.Equal(t, service.PlatformGemini, created.Platform)
	require.Equal(t, service.AccountTypeOAuth, created.Type)
	require.Equal(t, "projects/test-gemini", created.Credentials["project_id"])
	require.Equal(t, service.GeminiTierGoogleOneFree, created.Credentials["tier_id"])
	require.True(t, strings.HasPrefix(created.Credentials["refresh_token"].(string), "genc:v1:") || created.Credentials["refresh_token"] == "gem-rt")
}

func TestGeminiOAuthHandler_ReauthorizeAccountFromOAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	cfg := &config.Config{}
	cfg.Gemini.ProductionMode = true
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("69", 32)
	oauthSvc := service.NewGeminiOAuthService(nil, &geminiOAuthHandlerClientStub{}, &geminiOAuthHandlerCodeAssistStub{}, nil, cfg)
	defer oauthSvc.Stop()

	authResult, err := oauthSvc.GenerateAuthURL(context.Background(), nil, "http://localhost:3000/auth/callback", "", "google_one", service.GeminiTierGoogleOneFree)
	require.NoError(t, err)
	parsed, err := url.Parse(authResult.AuthURL)
	require.NoError(t, err)
	state := parsed.Query().Get("state")
	require.NotEmpty(t, state)

	adminSvc := newStubAdminService()
	adminSvc.accounts = []service.Account{{
		ID:       9,
		Name:     "gemini-old",
		Platform: service.PlatformGemini,
		Type:     service.AccountTypeOAuth,
		Status:   service.StatusError,
	}}
	h := NewGeminiOAuthHandler(oauthSvc, adminSvc)
	router.POST("/api/v1/admin/gemini/accounts/:id/reauthorize-from-oauth", h.ReauthorizeAccountFromOAuth)

	body := `{"session_id":"` + authResult.SessionID + `","code":"auth-code","state":"` + state + `","oauth_type":"google_one","tier_id":"google_one_free"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/gemini/accounts/9/reauthorize-from-oauth", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, adminSvc.updatedAccounts, 1)
	updated := adminSvc.updatedAccounts[0].input.Credentials
	require.True(t, strings.HasPrefix(updated["refresh_token"].(string), "genc:v1:"))
	require.Equal(t, "projects/test-gemini", updated["project_id"])
	require.Equal(t, service.GeminiTierGoogleOneFree, updated["tier_id"])
}
