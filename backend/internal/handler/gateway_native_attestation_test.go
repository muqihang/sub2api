package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGatewayHandlerNativeCountTokensAttestationValidatesAndSetsContext(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "native-attestation-test-secret")
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(string(body)))
	for key, values := range signedNativeHeadersForHandlerTest(t, body, "/v1/messages/count_tokens", time.Now()) {
		for _, value := range values {
			c.Request.Header.Add(key, value)
		}
	}

	h := &GatewayHandler{}
	require.True(t, h.applyClaudeCodeNativeMessagesAttestation(c, body))
	summary, ok := service.ClaudeCodeNativeAuditSummaryFromContext(c.Request.Context())
	require.True(t, ok)
	require.Equal(t, service.ClaudeCodeNativeInboundCountTokens, summary.InboundRoute)
	require.Equal(t, service.ClaudeCodeNativeCCGatewayCount, summary.CCGatewayRoute)
}

func TestGatewayHandlerNativeCountTokensForgedMarkersFailClosed(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "native-attestation-test-secret")
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[]}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(string(body)))
	c.Request.Header.Set(service.ClaudeCodeNativeClientTypeHeader, service.ClaudeCodeNativeClientType)
	c.Request.Header.Set(service.ClaudeCodeNativeGuardAttestedHeader, "true")

	h := &GatewayHandler{}
	require.False(t, h.applyClaudeCodeNativeMessagesAttestation(c, body))
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func signedNativeHeadersForHandlerTest(t *testing.T, body []byte, requestURI string, now time.Time) http.Header {
	t.Helper()
	payload := map[string]any{
		"key_id":                    "guard_v1",
		"scope":                     "claude_code_native_takeover",
		"version":                   1,
		"issued_at":                 now.Unix(),
		"nonce":                     "handler-nonce-001",
		"method":                    http.MethodPost,
		"request_uri":               requestURI,
		"client_type":               service.ClaudeCodeNativeClientType,
		"guard_attested":            true,
		"guard_version":             "guard_v1",
		"claude_code_version":       "2.1.150",
		"local_session_ref":         "hmac-sha256:" + strings.Repeat("a", 64),
		"netwatch_required":         true,
		"shape_healthcheck_profile": service.ClaudeCodeNativeTakeoverHealthProfile,
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256(body)
	mac := hmac.New(sha256.New, []byte("native-attestation-test-secret"))
	_, _ = mac.Write([]byte(encoded))
	_, _ = mac.Write([]byte("\n"))
	_, _ = mac.Write([]byte(http.MethodPost))
	_, _ = mac.Write([]byte("\n"))
	_, _ = mac.Write([]byte(requestURI))
	_, _ = mac.Write([]byte("\n"))
	_, _ = mac.Write([]byte(hex.EncodeToString(digest[:])))

	headers := http.Header{}
	headers.Set(service.ClaudeCodeNativeClientTypeHeader, service.ClaudeCodeNativeClientType)
	headers.Set(service.ClaudeCodeNativeGuardAttestedHeader, "true")
	headers.Set(service.ClaudeCodeNativeLocalSessionRefHeader, "hmac-sha256:"+strings.Repeat("a", 64))
	headers.Set(service.ClaudeCodeNativeNetwatchRequiredHeader, "true")
	headers.Set(service.ClaudeCodeNativeAttestationHeader, encoded)
	headers.Set(service.ClaudeCodeNativeSignatureHeader, base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
	return headers
}
