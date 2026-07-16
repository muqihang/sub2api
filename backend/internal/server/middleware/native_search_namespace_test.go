package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestNativeSearchNamespaceGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name     string
		method   string
		path     string
		captured bool
	}{
		{name: "root search post", method: http.MethodPost, path: "/alpha/search"},
		{name: "v1 search post", method: http.MethodPost, path: "/v1/alpha/search"},
		{name: "root search get", method: http.MethodGet, path: "/alpha/search", captured: true},
		{name: "root", method: http.MethodGet, path: "/alpha", captured: true},
		{name: "root slash", method: http.MethodGet, path: "/alpha/", captured: true},
		{name: "root unknown", method: http.MethodPost, path: "/alpha/unknown", captured: true},
		{name: "v1 root", method: http.MethodGet, path: "/v1/alpha", captured: true},
		{name: "v1 root slash", method: http.MethodGet, path: "/v1/alpha/", captured: true},
		{name: "v1 unknown", method: http.MethodPost, path: "/v1/alpha/unknown", captured: true},
		{name: "alphabet boundary", method: http.MethodGet, path: "/alphabet"},
		{name: "v1 alphabet boundary", method: http.MethodGet, path: "/v1/alphabet"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.Use(NativeSearchNamespaceGuard())
			router.Any("/*path", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"next": true}) })
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))

			router.ServeHTTP(rec, req)

			if !tc.captured {
				require.Equal(t, http.StatusOK, rec.Code)
				return
			}
			require.Equal(t, http.StatusNotFound, rec.Code)
			require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
			require.JSONEq(t, `{"error":{"type":"invalid_request_error","message":"Native Search endpoint not found"}}`, rec.Body.String())
		})
	}
}
