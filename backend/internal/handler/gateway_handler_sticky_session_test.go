//go:build unit

package handler

import (
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

  c, _ := gin.CreateTestContext(httptest.NewRecorder())
  c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
  c.Request.Header.Set("session_id", "ses_opencode_123")

  want := sha256.Sum256([]byte("ses_opencode_123"))
  wantHex := hex.EncodeToString(want[:])

  got := computeStickySessionHashFromHeaders(c)
  require.Equal(t, wantHex, got)
}
