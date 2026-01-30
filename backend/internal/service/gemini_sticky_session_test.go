//go:build unit

package service

import (
  "net/http/httptest"
  "testing"

  "github.com/gin-gonic/gin"
  "github.com/stretchr/testify/require"
)

func TestGeminiStickySessionHash_PrefersSessionIDHeader(t *testing.T) {
  gin.SetMode(gin.TestMode)
  c, _ := gin.CreateTestContext(httptest.NewRecorder())
  c.Request = httptest.NewRequest("POST", "/v1beta/models/gemini-3-pro-high:generateContent", nil)
  c.Request.Header.Set("session_id", "ses_opencode_123")

  hash := GenerateGeminiStickySessionHash(c, []byte(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`))
  require.NotEmpty(t, hash)

  // Should be stable for the same session_id regardless of message content.
  hash2 := GenerateGeminiStickySessionHash(c, []byte(`{"contents":[{"role":"user","parts":[{"text":"different"}]}]}`))
  require.Equal(t, hash, hash2)
}

func TestGeminiStickySessionHash_FallsBackToFirstUserText(t *testing.T) {
  gin.SetMode(gin.TestMode)
  c, _ := gin.CreateTestContext(httptest.NewRecorder())
  c.Request = httptest.NewRequest("POST", "/v1beta/models/gemini-3-pro-high:generateContent", nil)

  body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)
  hash := GenerateGeminiStickySessionHash(c, body)
  require.NotEmpty(t, hash)

  // Same body -> same hash (deterministic)
  hash2 := GenerateGeminiStickySessionHash(c, body)
  require.Equal(t, hash, hash2)
}
