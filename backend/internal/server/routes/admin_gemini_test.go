package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	ihandler "github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type adminGeminiRouteRepoStub struct {
	service.AccountRepository
	account *service.Account
}

func (s *adminGeminiRouteRepoStub) GetByID(ctx context.Context, id int64) (*service.Account, error) {
	if s.account != nil && s.account.ID == id {
		return s.account, nil
	}
	return nil, service.ErrAccountNotFound
}

func (s *adminGeminiRouteRepoStub) ListByPlatform(ctx context.Context, platform string) ([]service.Account, error) {
	if platform != service.PlatformGemini || s.account == nil {
		return nil, nil
	}
	return []service.Account{*s.account}, nil
}

func TestRegisterAdminRoutes_GeminiHealthEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	cfg.Gemini.ProductionMode = true
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("65", 32)

	repo := &adminGeminiRouteRepoStub{
		account: &service.Account{
			ID:       9,
			Name:     "Gemini Route Account",
			Platform: service.PlatformGemini,
			Type:     service.AccountTypeOAuth,
			Credentials: map[string]any{
				"oauth_type": "google_one",
				"project_id": "proj-9",
				"tier_id":    service.GeminiTierGoogleOneFree,
			},
		},
	}

	healthHandler := adminhandler.NewGeminiHealthHandler(service.NewGeminiHealthService(repo, service.NewGeminiOAuthService(nil, nil, nil, nil, cfg), cfg))
	oauthHandler := adminhandler.NewGeminiOAuthHandler(service.NewGeminiOAuthService(nil, nil, nil, nil, cfg), nil)

	handlers := &ihandler.Handlers{
		Admin: &ihandler.AdminHandlers{
			GeminiHealth: healthHandler,
			GeminiOAuth:  oauthHandler,
		},
	}

	router := gin.New()
	v1 := router.Group("/api/v1")
	RegisterAdminRoutes(v1, handlers, middleware.AdminAuthMiddleware(func(c *gin.Context) {
		c.Next()
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/gemini/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/gemini/verify?account_id=9", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "\"account_id\":9")
}
