package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGatewayHandlerNativeCountTokensAttestationValidatesAndSetsContext(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "native-attestation-test-secret")
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_FORMAL_POOL_MODELS", "claude-sonnet-4-6")
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
	localSessionRef := "hmac-sha256:" + strings.Repeat("a", 64)
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
		"claude_code_version":       "2.1.175",
		"local_session_ref":         localSessionRef,
		"netwatch_required":         true,
		"shape_healthcheck_profile": service.ClaudeCodeNativeTakeoverHealthProfile,
		"route":                     service.ClaudeCodeNativeRoute,
		"model_id":                  "claude-sonnet-4-6",
		"provider_owner":            service.ClaudeCodeNativeProviderOwner,
		"credential_scope":          service.ClaudeCodeNativeCredentialScope,
		"gateway_location":          service.ClaudeCodeNativeGatewayLocation,
		"runtime_hash":              "sha256:" + strings.Repeat("1", 64),
		"overlay_hash":              "sha256:" + strings.Repeat("2", 64),
		"catalog_hash":              "sha256:" + strings.Repeat("3", 64),
		"session_ref":               localSessionRef,
		"body_shape_hash":           handlerTestNativeBodyShapeHash(body),
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
	headers.Set(service.ClaudeCodeNativeLocalSessionRefHeader, localSessionRef)
	headers.Set(service.ClaudeCodeNativeNetwatchRequiredHeader, "true")
	headers.Set(service.ClaudeCodeNativeAttestationHeader, encoded)
	headers.Set(service.ClaudeCodeNativeSignatureHeader, base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
	return headers
}
func handlerTestNativeBodyShapeHash(body []byte) string {
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		decoded = map[string]any{"body_size": len(body), "type": "invalid_json"}
	}
	shape := handlerTestNativeShapeValue(decoded)
	raw, _ := json.Marshal(shape)
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func handlerTestNativeShapeValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		children := map[string]any{}
		keys := make([]string, 0, len(v))
		for key, child := range v {
			safeKey := handlerTestNativeShapeKey(key)
			if safeKey == "" {
				safeKey = "redacted-key"
			}
			if _, exists := children[safeKey]; !exists {
				keys = append(keys, safeKey)
			}
			children[safeKey] = handlerTestNativeShapeValue(child)
		}
		sort.Strings(keys)
		return map[string]any{"children": children, "keys": keys, "type": "object"}
	case []any:
		items := make([]any, 0, len(v))
		limit := len(v)
		if limit > 32 {
			limit = 32
		}
		for i := 0; i < limit; i++ {
			items = append(items, handlerTestNativeShapeValue(v[i]))
		}
		return map[string]any{"items": items, "len": len(v), "truncated": len(v) > 32, "type": "array"}
	case string:
		return map[string]any{"type": "string"}
	case bool:
		return map[string]any{"type": "bool"}
	case float64, float32, int, int64, int32, json.Number:
		return map[string]any{"type": "number"}
	case nil:
		return map[string]any{"type": "null"}
	default:
		return map[string]any{"type": "unknown"}
	}
}

func handlerTestNativeShapeKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 128 {
		return "redacted-key"
	}
	for _, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return "redacted-key"
	}
	return key
}
