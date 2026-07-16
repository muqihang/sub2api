package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func NativeSearchNamespaceGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if !isNativeSearchNamespacePath(path) {
			c.Next()
			return
		}
		if c.Request.Method == http.MethodPost && (path == "/alpha/search" || path == "/v1/alpha/search") {
			c.Next()
			return
		}
		OpenAIErrorWriter(c, http.StatusNotFound, "Native Search endpoint not found")
		c.Abort()
	}
}

func isNativeSearchNamespacePath(path string) bool {
	return path == "/alpha" || strings.HasPrefix(path, "/alpha/") ||
		path == "/v1/alpha" || strings.HasPrefix(path, "/v1/alpha/")
}
