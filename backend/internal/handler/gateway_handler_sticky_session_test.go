//go:build unit

package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMessages_StickySession_UsesSessionIDHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(gatewayStickySessionHMACEnv, "test-sticky-session-key")

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("session_id", "ses_opencode_123")

	mac := hmac.New(sha256.New, []byte("test-sticky-session-key"))
	_, _ = mac.Write([]byte("gateway_sticky_session"))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte("ses_opencode_123"))
	want := "hmac-sha256:" + hex.EncodeToString(mac.Sum(nil))

	got := computeStickySessionHashFromHeaders(c)
	require.Equal(t, want, got)
}
