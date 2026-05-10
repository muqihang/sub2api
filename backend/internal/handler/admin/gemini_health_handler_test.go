package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type geminiHealthHandlerRepoStub struct {
	service.AccountRepository
	account *service.Account
}

func (s *geminiHealthHandlerRepoStub) GetByID(ctx context.Context, id int64) (*service.Account, error) {
	if s.account != nil && s.account.ID == id {
		return s.account, nil
	}
	return nil, service.ErrAccountNotFound
}

func (s *geminiHealthHandlerRepoStub) ListByPlatform(ctx context.Context, platform string) ([]service.Account, error) {
	if platform != service.PlatformGemini || s.account == nil {
		return nil, nil
	}
	return []service.Account{*s.account}, nil
}

func TestGeminiHealthHandler_HealthAndVerify(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	cfg.Gemini.ProductionMode = true
	cfg.Gemini.TokenCacheMode = "encrypted"
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("64", 32)

	protector, err := service.ProvideGeminiSecretProtector(cfg)
	require.NoError(t, err)
	protected, err := protector.ProtectCredentials(map[string]any{
		"refresh_token": "rt",
		"oauth_type":    "google_one",
		"project_id":    "proj",
		"tier_id":       service.GeminiTierGoogleOneFree,
	})
	require.NoError(t, err)

	repo := &geminiHealthHandlerRepoStub{
		account: &service.Account{
			ID:          7,
			Name:        "Gemini Account",
			Platform:    service.PlatformGemini,
			Type:        service.AccountTypeOAuth,
			Credentials: protected,
			Extra: map[string]any{
				"gemini_oauth_reason":      "google_one_default_tier_fallback",
				"gemini_token_cache_state": "degraded",
			},
		},
	}
	healthService := service.NewGeminiHealthService(repo, service.NewGeminiOAuthService(nil, nil, nil, nil, cfg), cfg)
	handler := NewGeminiHealthHandler(healthService)

	router := gin.New()
	router.GET("/health", handler.Health)
	router.GET("/verify", handler.Verify)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "\"gateway_status\":\"degraded\"")
	require.Contains(t, rec.Body.String(), "\"session_store\":\"memory\"")

	req = httptest.NewRequest(http.MethodGet, "/verify?account_id=7", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "\"project_id_status\":\"present\"")
	require.Contains(t, rec.Body.String(), "\"tier_status\":\"default_fallback\"")
	require.NotContains(t, rec.Body.String(), "refresh_token")
}

func TestGeminiHealthHandler_VerifyRejectsInvalidRuntimeContract(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	repo := &geminiHealthHandlerRepoStub{
		account: &service.Account{
			ID:       8,
			Name:     "Invalid Gemini",
			Platform: service.PlatformGemini,
			Type:     service.AccountTypeOAuth,
			Credentials: map[string]any{
				"oauth_type": "legacy_unknown",
			},
		},
	}
	healthService := service.NewGeminiHealthService(repo, service.NewGeminiOAuthService(nil, nil, nil, nil, cfg), cfg)
	handler := NewGeminiHealthHandler(healthService)

	router := gin.New()
	router.GET("/verify", handler.Verify)

	req := httptest.NewRequest(http.MethodGet, "/verify?account_id=8", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "unsupported gemini oauth_type")
}
