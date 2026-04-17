package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIOAuthHandler_GatewayTemplates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewOpenAIOAuthHandler(nil, newStubAdminService())
	router.GET("/api/v1/admin/openai/gateway/templates", h.GatewayTemplates)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/openai/gateway/templates?base_url=https://api.example.com&api_key=sk-user&gateway_token=gw-123", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "X-OpenAI-Gateway-Token")
	require.Contains(t, rec.Body.String(), "codex")
	require.Contains(t, rec.Body.String(), "https://api.example.com")
}
