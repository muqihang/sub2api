package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
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

func TestGatewayHandlerNativeCountTokensRespondsLocallyBeforeAccountSelection(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "native-attestation-test-secret")
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_FORMAL_POOL_MODELS", "claude-sonnet-4-6")
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":[{"type":"text","text":"count-token-prompt-must-not-leak"}]}]}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(string(body)))
	for key, values := range signedNativeHeadersForHandlerTest(t, body, "/v1/messages/count_tokens", time.Now()) {
		for _, value := range values {
			c.Request.Header.Add(key, value)
		}
	}
	groupID := int64(8)
	user := &service.User{ID: 1, Role: service.RoleUser, Status: service.StatusActive, Concurrency: 1}
	group := &service.Group{ID: groupID, Platform: service.PlatformAnthropic, Status: service.StatusActive, ClaudeCodeOnly: true}
	c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{ID: 15, UserID: user.ID, User: user, GroupID: &groupID, Group: group, Status: service.StatusActive})
	c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: user.ID, Concurrency: user.Concurrency})

	h := &GatewayHandler{}
	h.CountTokens(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), "count-token-prompt-must-not-leak")
	require.Equal(t, "claude-sonnet-4-6", gjson.Get(rec.Body.String(), "model").String())
	require.Greater(t, gjson.Get(rec.Body.String(), "input_tokens").Int(), int64(1))
}

func TestGatewayHandlerBridgeCountTokensRespondsLocallyWithoutPromptLeak(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_ROUTE_HINT_SECRET", "route-hint-key")
	t.Setenv("SUB2API_CLAUDE_CODE_ROUTE_HINT_CURRENT_KEY_ID", "route_hint_v1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "sk-deepseek-test-key")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", `{"catalog_version":"cp5-route-catalog","runtime_hash":"sha256:1111111111111111111111111111111111111111111111111111111111111111","overlay_hash":"sha256:2222222222222222222222222222222222222222222222222222222222222222","catalog_hash":"sha256:3333333333333333333333333333333333333333333333333333333333333333","models":[{"model_id":"claude-code-bridge-deepseek-v4-flash","upstream_model":"deepseek-v4-flash","provider":"deepseek","route":"deepseek_bridge","client_type":"claude_code_bridge_deepseek","provider_owner":"zhumeng_managed","credential_scope":"bridge_pool","gateway_location":"cloud","catalog_fresh":true,"preferred_protocol":"anthropic_messages","anthropic_base_url":"http://127.0.0.1:9/anthropic","capabilities_verified":true,"supports_text":true,"supports_tools":true,"supports_streaming":true,"supports_usage":true,"supports_error_passthrough":true}]}`)
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"claude-code-bridge-deepseek-v4-flash","messages":[{"role":"user","content":[{"type":"text","text":"bridge-count-token-prompt-must-not-leak"}]}]}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens?beta=true", strings.NewReader(string(body)))
	for key, values := range signedBridgeRouteHintHeadersForHandlerTest(t, body, "/v1/messages/count_tokens?beta=true", time.Now()) {
		for _, value := range values {
			c.Request.Header.Add(key, value)
		}
	}

	h := &GatewayHandler{}
	require.True(t, h.handleClaudeCodeBridgeCountTokensLocal(c, body, "claude-code-bridge-deepseek-v4-flash"))

	require.Equal(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), "bridge-count-token-prompt-must-not-leak")
	require.Equal(t, "claude-code-bridge-deepseek-v4-flash", gjson.Get(rec.Body.String(), "model").String())
	require.Greater(t, gjson.Get(rec.Body.String(), "input_tokens").Int(), int64(1))
}

func TestGatewayHandlerBridgeLiveStoresSafeAuditSummaryOnContext(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_ROUTE_HINT_SECRET", "route-hint-key")
	t.Setenv("SUB2API_CLAUDE_CODE_ROUTE_HINT_CURRENT_KEY_ID", "route_hint_v1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "deepseek-unit-test-key")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_CACHE_AUDIT_HMAC_KEY", "local-cache-audit-unit-test-key")
	t.Setenv("SUB2API_CLAUDE_CODE_CACHE_AUDIT_HMAC_KEY_ID", "cache-test-v1")
	var upstreamPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		upstreamPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_start","message":{"id":"msg_cache","type":"message","role":"assistant","content":[],"model":"deepseek-v4-flash","usage":{"input_tokens":20,"prompt_cache_hit_tokens":7,"prompt_cache_miss_tokens":13}}}` + "\n\n"))
		_, _ = w.Write([]byte("event: message_stop\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_stop"}` + "\n\n"))
	}))
	defer upstream.Close()
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", `{"catalog_version":"cp5-route-catalog","runtime_hash":"sha256:1111111111111111111111111111111111111111111111111111111111111111","overlay_hash":"sha256:2222222222222222222222222222222222222222222222222222222222222222","catalog_hash":"sha256:3333333333333333333333333333333333333333333333333333333333333333","models":[{"model_id":"claude-code-bridge-deepseek-v4-flash","upstream_model":"deepseek-v4-flash","provider":"deepseek","route":"deepseek_bridge","client_type":"claude_code_bridge_deepseek","provider_owner":"zhumeng_managed","credential_scope":"bridge_pool","gateway_location":"cloud","catalog_fresh":true,"preferred_protocol":"anthropic_messages","anthropic_base_url":"`+upstream.URL+`/anthropic","capabilities_verified":true,"supports_text":true,"supports_tools":true,"supports_streaming":true,"supports_usage":true,"supports_cache_audit":true,"supports_error_passthrough":true}]}`)
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"claude-code-bridge-deepseek-v4-flash","system":"stable system prefix sentinel","messages":[{"role":"user","content":"stable context sentinel"},{"role":"user","content":"latest turn sentinel"}],"tools":[{"name":"Agent","description":"subagent","input_schema":{"type":"object"}}],"stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))
	for key, values := range signedBridgeRouteHintHeadersForHandlerTest(t, body, "/v1/messages", time.Now()) {
		for _, value := range values {
			c.Request.Header.Add(key, value)
		}
	}

	(&GatewayHandler{}).handleClaudeCodeBridgeMessagesSkeleton(c, body)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "/anthropic/v1/messages", upstreamPath)
	rawAudit, ok := c.Get("claude_code_bridge_audit_summary")
	require.True(t, ok)
	audit, ok := rawAudit.(service.ClaudeCodeBridgeAuditSummary)
	require.True(t, ok)
	require.Equal(t, "claude_code_bridge_deepseek", audit.ClientType)
	require.Equal(t, "anthropic_messages", audit.SelectedProtocol)
	require.Equal(t, "/anthropic/v1/messages", audit.UpstreamPathKind)
	require.Equal(t, "deepseek_prefix_kv", audit.ProviderCacheMechanism)
	require.True(t, audit.CacheControlProviderIgnored)
	require.Equal(t, 7, audit.CacheReadTokens)
	require.Equal(t, 13, audit.CacheMissTokens)
	dumped, err := json.Marshal(audit)
	require.NoError(t, err)
	require.NotContains(t, string(dumped), "stable system prefix sentinel")
	require.NotContains(t, string(dumped), "latest turn sentinel")
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

func signedBridgeRouteHintHeadersForHandlerTest(t *testing.T, body []byte, requestURI string, now time.Time) http.Header {
	t.Helper()
	digest := sha256.Sum256(body)
	payload := map[string]any{
		"key_id":                     "route_hint_v1",
		"scope":                      service.ClaudeCodeRouteHintScope,
		"version":                    service.ClaudeCodeRouteHintVersion,
		"issued_at":                  now.Unix(),
		"expires_at":                 now.Add(time.Minute).Unix(),
		"nonce":                      "handler-bridge-count-tokens-" + hex.EncodeToString(digest[:4]),
		"method":                     http.MethodPost,
		"request_uri":                requestURI,
		"model_id":                   "claude-code-bridge-deepseek-v4-flash",
		"body_model":                 "claude-code-bridge-deepseek-v4-flash",
		"body_sha256":                "sha256:" + hex.EncodeToString(digest[:]),
		"runtime_hash":               "sha256:" + strings.Repeat("1", 64),
		"overlay_hash":               "sha256:" + strings.Repeat("2", 64),
		"catalog_hash":               "sha256:" + strings.Repeat("3", 64),
		"catalog_version":            "cp5-route-catalog",
		"session_ref":                "11111111-2222-4333-8444-555555555555",
		"route":                      "deepseek_bridge",
		"client_type":                "claude_code_bridge_deepseek",
		"provider":                   "deepseek",
		"live_request_allowed":       true,
		"formal_pool_allowed":        false,
		"native_attestation_allowed": false,
		"provider_owner":             "zhumeng_managed",
		"credential_scope":           "bridge_pool",
		"gateway_location":           "cloud",
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, []byte("route-hint-key"))
	_, _ = mac.Write([]byte(encoded))
	_, _ = mac.Write([]byte("\n"))
	_, _ = mac.Write([]byte(http.MethodPost))
	_, _ = mac.Write([]byte("\n"))
	_, _ = mac.Write([]byte(requestURI))
	_, _ = mac.Write([]byte("\n"))
	_, _ = mac.Write([]byte(hex.EncodeToString(digest[:])))

	headers := http.Header{}
	headers.Set(service.ClaudeCodeRouteHintHeader, encoded)
	headers.Set(service.ClaudeCodeRouteHintSignatureHeader, base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
	headers.Set(service.ClaudeCodeNativeClientTypeHeader, "claude_code_bridge_deepseek")
	headers.Set("x-sub2api-route", "deepseek_bridge")
	headers.Set(service.ClaudeCodeNativeCatalogVersionHeader, "cp5-route-catalog")
	headers.Set("x-claude-code-session-id", "11111111-2222-4333-8444-555555555555")
	return headers
}

func signedNativeHeadersForHandlerTest(t *testing.T, body []byte, requestURI string, now time.Time) http.Header {
	t.Helper()
	localSessionRef := "hmac-sha256:" + strings.Repeat("a", 64)
	digest := sha256.Sum256(body)
	payload := map[string]any{
		"key_id":                    "guard_v1",
		"scope":                     "claude_code_native_takeover",
		"version":                   1,
		"issued_at":                 now.Unix(),
		"nonce":                     "handler-nonce-" + hex.EncodeToString(digest[:4]),
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
		"catalog_version":           "legacy-native",
		"session_ref":               localSessionRef,
		"body_shape_hash":           handlerTestNativeBodyShapeHash(body),
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	encoded := base64.RawURLEncoding.EncodeToString(raw)
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
	headers.Set(service.ClaudeCodeNativeCatalogVersionHeader, "legacy-native")
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
