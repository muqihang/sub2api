package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUsePreRoutingProtocolMiddleware_NativeSearchGuardPrecedesCORS(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	usePreRoutingProtocolMiddleware(router, config.CORSConfig{AllowedOrigins: []string{"*"}})
	router.Any("/*path", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"next": true}) })

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodOptions, "/alpha/search", strings.NewReader(`{}`))
	request.Header.Set("Origin", "https://example.test")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.Contains(t, recorder.Header().Get("Content-Type"), "application/json")
	require.JSONEq(t, `{"error":{"type":"invalid_request_error","message":"Native Search endpoint not found"}}`, recorder.Body.String())
}
