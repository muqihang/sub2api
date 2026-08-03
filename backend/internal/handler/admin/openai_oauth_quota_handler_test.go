package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIOAuthHandlerQuotaRoutesReturnServicePayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	quotaSvc := service.NewOpenAIQuotaService(nil, nil, nil, nil)
	h := NewOpenAIOAuthHandler(nil, nil, newStubAdminService(), quotaSvc)

	r := gin.New()
	r.GET("/admin/openai/accounts/:id/quota", h.QueryQuota)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/openai/accounts/123/quota", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Contains(t, w.Body.String(), "OPENAI_QUOTA_NOT_CONFIGURED")
}
