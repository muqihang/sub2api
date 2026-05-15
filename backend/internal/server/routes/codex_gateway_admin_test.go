package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCodexGatewayAdminRoutes_RequireAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")
	admin := v1.Group("/admin")
	admin.Use(func(c *gin.Context) {
		c.AbortWithStatus(http.StatusUnauthorized)
	})
	registerCodexGatewayAdminRoutes(admin, &handler.Handlers{
		Admin: &handler.AdminHandlers{
			CodexGateway: adminhandler.NewCodexGatewayHandler(nil),
		},
	})

	for _, tc := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/v1/admin/codex-gateway/summary"},
		{method: http.MethodGet, path: "/api/v1/admin/codex-gateway/provider-groups"},
		{method: http.MethodPut, path: "/api/v1/admin/codex-gateway/provider-groups"},
		{method: http.MethodGet, path: "/api/v1/admin/codex-gateway/models"},
		{method: http.MethodPut, path: "/api/v1/admin/codex-gateway/models/gpt-5.5"},
		{method: http.MethodPost, path: "/api/v1/admin/codex-gateway/smoke"},
		{method: http.MethodGet, path: "/api/v1/admin/codex-gateway/state-store/summary"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			router.ServeHTTP(rec, req)
			require.Equal(t, http.StatusUnauthorized, rec.Code)
		})
	}
}
