package routes

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	ihandler "github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFormalPoolOnboardingRoutes_AdminAndPublicBrowserEgress(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")
	svc := service.NewFormalPoolOnboardingService(service.FormalPoolOnboardingDeps{})
	h := &ihandler.Handlers{Admin: &ihandler.AdminHandlers{FormalPoolOnboarding: adminhandler.NewFormalPoolOnboardingHandler(svc)}}
	adminAuthCalls := 0

	RegisterFormalPoolOnboardingPublicRoutes(v1, h)
	RegisterAdminRoutes(v1, h, middleware.AdminAuthMiddleware(func(c *gin.Context) {
		adminAuthCalls++
		c.Next()
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/claude-onboarding/browser-egress-check/bad-nonce", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Body.String(), "FORMAL_POOL_ONBOARDING_NOT_FOUND")
	require.Equal(t, 0, adminAuthCalls, "browser egress check must not require admin session")

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/claude-onboarding/sessions", bytes.NewBufferString(`{}`))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.NotEqual(t, http.StatusNotFound, rec.Code)
	require.Equal(t, 1, adminAuthCalls, "mutating onboarding session routes must remain admin protected")

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/claude-onboarding/sessions/fpo_test/setup-token-cookie-auth-and-create", bytes.NewBufferString(`{"session_key":"sk-ant-sid02-test"}`))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, 2, adminAuthCalls, "setup-token onboarding route must remain admin protected")
	require.Contains(t, rec.Body.String(), "FORMAL_POOL_ONBOARDING_NOT_FOUND", "registered route should reach onboarding service")
	require.NotContains(t, rec.Body.String(), "sk-ant-sid02-test", "route errors must not echo setup-token login state")
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/claude-onboarding/sessions/fpo_test/healthcheck", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, 3, adminAuthCalls, "healthcheck route must remain admin protected")
	require.Contains(t, rec.Body.String(), "FORMAL_POOL_ONBOARDING_NOT_FOUND")

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/claude-onboarding/accounts/2/healthcheck", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, 4, adminAuthCalls, "account-level healthcheck route must remain admin protected")
	require.NotContains(t, rec.Body.String(), "FORMAL_POOL_ONBOARDING_NOT_FOUND", "account-level healthcheck must not use expired onboarding sessions")

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/claude-onboarding/sessions/fpo_test/promote-production", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, 5, adminAuthCalls, "production promotion route must remain admin protected")
	require.Contains(t, rec.Body.String(), "FORMAL_POOL_ONBOARDING_NOT_FOUND")

}
