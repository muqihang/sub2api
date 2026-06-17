package routes

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newAnthropicCompatProtocolRouteRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	groupID := int64(44)
	user := &service.User{ID: 4401, Role: service.RoleUser, Status: service.StatusActive, Concurrency: 1}
	group := &service.Group{ID: groupID, Platform: service.PlatformAnthropic, Status: service.StatusActive}

	cfg := &config.Config{}
	cfg.Gateway.MaxBodySize = 1 << 20

	RegisterGatewayRoutes(
		router,
		&handler.Handlers{
			Auth: handler.NewAuthHandler(
				&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
				nil,
				nil,
				nil,
				nil,
				nil,
				nil,
				service.NewAugmentPluginService(nil, nil, nil, nil, nil, nil),
			),
			Gateway:       &handler.GatewayHandler{},
			OpenAIGateway: &handler.OpenAIGatewayHandler{},
		},
		servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
			groupCopy := *group
			apiKey := &service.APIKey{ID: 44, UserID: user.ID, User: user, GroupID: &groupID, Group: &groupCopy, Status: service.StatusActive}
			c.Set(string(servermiddleware.ContextKeyAPIKey), apiKey)
			c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: user.ID, Concurrency: user.Concurrency})
			c.Next()
		}),
		nil,
		nil,
		nil,
		nil,
		cfg,
	)
	return router
}

func cp6OpenAIBridgeRouteCatalogJSON() string {
	return `{"catalog_version":"cp5-route-catalog","runtime_hash":"sha256:1111111111111111111111111111111111111111111111111111111111111111","overlay_hash":"sha256:2222222222222222222222222222222222222222222222222222222222222222","catalog_hash":"sha256:3333333333333333333333333333333333333333333333333333333333333333","models":[{"model_id":"gpt-5.5","provider":"openai","route":"openai_bridge","client_type":"claude_code_bridge_openai","provider_owner":"zhumeng_managed","credential_scope":"bridge_pool","gateway_location":"cloud","catalog_fresh":true,"preferred_protocol":"responses","openai_base_url":"https://api.openai.com/v1","capabilities_verified":true,"supports_text":true,"supports_tools":true,"supports_streaming":true,"supports_usage":true,"supports_error_passthrough":true}]}`
}

func cp6DeepSeekBridgeRouteCatalogJSON() string {
	return `{"catalog_version":"cp5-route-catalog","runtime_hash":"sha256:1111111111111111111111111111111111111111111111111111111111111111","overlay_hash":"sha256:2222222222222222222222222222222222222222222222222222222222222222","catalog_hash":"sha256:3333333333333333333333333333333333333333333333333333333333333333","models":[{"model_id":"deepseek-v4-pro","provider":"deepseek","route":"deepseek_bridge","client_type":"claude_code_bridge_deepseek","provider_owner":"zhumeng_managed","credential_scope":"bridge_pool","gateway_location":"cloud","catalog_fresh":true,"preferred_protocol":"anthropic_messages","anthropic_base_url":"https://api.deepseek.com/anthropic","capabilities_verified":true,"supports_text":true,"supports_tools":true,"supports_streaming":true,"supports_usage":true,"supports_error_passthrough":true}]}`
}

func cp6DeepSeekBridgeRouteCatalogJSONWithBaseURL(baseURL string) string {
	raw, err := json.Marshal(map[string]any{
		"catalog_version": "cp5-route-catalog",
		"runtime_hash":    "sha256:" + strings.Repeat("1", 64),
		"overlay_hash":    "sha256:" + strings.Repeat("2", 64),
		"catalog_hash":    "sha256:" + strings.Repeat("3", 64),
		"models": []map[string]any{{
			"model_id":                   "deepseek-v4-pro",
			"provider":                   "deepseek",
			"route":                      "deepseek_bridge",
			"client_type":                "claude_code_bridge_deepseek",
			"provider_owner":             "zhumeng_managed",
			"credential_scope":           "bridge_pool",
			"gateway_location":           "cloud",
			"catalog_fresh":              true,
			"preferred_protocol":         "anthropic_messages",
			"anthropic_base_url":         baseURL,
			"capabilities_verified":      true,
			"supports_text":              true,
			"supports_tools":             true,
			"supports_streaming":         true,
			"supports_usage":             true,
			"supports_error_passthrough": true,
		}},
	})
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func configureCP6RouteHintEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SUB2API_CLAUDE_CODE_ROUTE_HINT_SECRET", "route-hint-key")
	t.Setenv("SUB2API_CLAUDE_CODE_ROUTE_HINT_KEYS_JSON", "")
	t.Setenv("SUB2API_CLAUDE_CODE_ROUTE_HINT_CURRENT_KEY_ID", "route_hint_v1")
}

func signCP6BridgeRouteHintHeaders(t *testing.T, req *http.Request, body string, fields map[string]any) {
	t.Helper()
	now := time.Now().Unix()
	digest := sha256.Sum256([]byte(body))
	payload := map[string]any{
		"key_id":                     "route_hint_v1",
		"scope":                      service.ClaudeCodeRouteHintScope,
		"version":                    service.ClaudeCodeRouteHintVersion,
		"issued_at":                  now,
		"expires_at":                 now + 60,
		"nonce":                      "route-hint-test-" + fields["model_id"].(string) + "-" + hex.EncodeToString(digest[:4]),
		"method":                     "POST",
		"request_uri":                req.URL.RequestURI(),
		"model_id":                   fields["model_id"],
		"body_model":                 fields["model_id"],
		"body_sha256":                "sha256:" + hex.EncodeToString(digest[:]),
		"runtime_hash":               "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		"overlay_hash":               "sha256:2222222222222222222222222222222222222222222222222222222222222222",
		"catalog_hash":               "sha256:3333333333333333333333333333333333333333333333333333333333333333",
		"catalog_version":            "cp5-route-catalog",
		"session_ref":                "sess-route-hint",
		"route":                      fields["route"],
		"client_type":                fields["client_type"],
		"provider":                   fields["provider"],
		"live_request_allowed":       false,
		"formal_pool_allowed":        false,
		"native_attestation_allowed": false,
		"provider_owner":             "zhumeng_managed",
		"credential_scope":           "bridge_pool",
		"gateway_location":           "cloud",
	}
	for key, value := range fields {
		payload[key] = value
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, []byte("route-hint-key"))
	_, _ = mac.Write([]byte(encoded))
	_, _ = mac.Write([]byte("\nPOST\n"))
	_, _ = mac.Write([]byte(req.URL.RequestURI()))
	_, _ = mac.Write([]byte("\n"))
	_, _ = mac.Write([]byte(hex.EncodeToString(digest[:])))
	req.Header.Set(service.ClaudeCodeRouteHintHeader, encoded)
	req.Header.Set(service.ClaudeCodeRouteHintSignatureHeader, base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
}

func TestClaudeCodeCompatRoutesRejectOpenAICompatibleProtocolsForAnthropicGroups(t *testing.T) {
	router := newAnthropicCompatProtocolRouteRouter()

	tests := []struct {
		path string
		body string
	}{
		{path: "/v1/chat/completions", body: `{"model":"gpt-5","messages":[{"role":"user","content":"should-not-leak"}]}`},
		{path: "/v1/responses", body: `{"model":"gpt-5","input":"should-not-leak"}`},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Contains(t, rec.Body.String(), "unsupported_protocol")
			require.NotContains(t, rec.Body.String(), "should-not-leak")
			require.NotContains(t, rec.Body.String(), "gpt-5")
		})
	}
}

func TestClaudeCodeCompatMessagesRejectOpenAIShapedBodyBeforeForwarding(t *testing.T) {
	router := newAnthropicCompatProtocolRouteRouter()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4-6","input":"should-not-leak","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "unsupported_body_shape")
	require.NotContains(t, rec.Body.String(), "should-not-leak")
	require.NotContains(t, rec.Body.String(), "hello")
}

func TestClaudeCodeBridgeMessagesRouteReturnsSkeletonWithoutAnthropicCompatOrFormalPool(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", cp6OpenAIBridgeRouteCatalogJSON())
	configureCP6RouteHintEnv(t)
	router := newAnthropicCompatProtocolRouteRouter()
	body := `{"model":"gpt-5.5","messages":[{"role":"user","content":"should-not-leak"}],"stream":true,"tools":[{"name":"get_weather","input_schema":{"type":"object"}}],"tool_choice":{"type":"tool","name":"get_weather"}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-sub2api-client-type", "claude_code_bridge_openai")
	req.Header.Set("x-sub2api-route", "openai_bridge")
	req.Header.Set("x-sub2api-route-catalog-version", "cp5-route-catalog")
	signCP6BridgeRouteHintHeaders(t, req, body, map[string]any{
		"model_id":    "gpt-5.5",
		"provider":    "openai",
		"route":       "openai_bridge",
		"client_type": "claude_code_bridge_openai",
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	stream := rec.Body.String()
	require.Contains(t, stream, "event: message_start")
	require.Contains(t, stream, `"type":"tool_use"`)
	require.Contains(t, stream, `"type":"input_json_delta"`)
	require.Contains(t, stream, `"stop_reason":"tool_use"`)
	require.NotContains(t, stream, "unsupported_body_shape")
	require.NotContains(t, stream, "should-not-leak")
}

func TestClaudeCodeBridgeDeepSeekLiveFlagOffKeepsSkeletonAndDoesNotCallProvider(t *testing.T) {
	var upstreamHits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		w.WriteHeader(http.StatusTeapot)
	}))
	defer upstream.Close()
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", cp6DeepSeekBridgeRouteCatalogJSONWithBaseURL(upstream.URL+"/anthropic"))
	configureCP6RouteHintEnv(t)
	router := newAnthropicCompatProtocolRouteRouter()
	body := `{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"flag-off-must-not-hit-provider"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-sub2api-client-type", "claude_code_bridge_deepseek")
	req.Header.Set("x-sub2api-route", "deepseek_bridge")
	req.Header.Set("x-sub2api-route-catalog-version", "cp5-route-catalog")
	signCP6BridgeRouteHintHeaders(t, req, body, map[string]any{
		"model_id":    "deepseek-v4-pro",
		"provider":    "deepseek",
		"route":       "deepseek_bridge",
		"client_type": "claude_code_bridge_deepseek",
		"nonce":       "cp6-deepseek-live-flag-off",
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(0), upstreamHits.Load())
	require.Contains(t, rec.Body.String(), "bridge skeleton")
	require.NotContains(t, rec.Body.String(), "flag-off-must-not-hit-provider")
}

func TestClaudeCodeBridgeDeepSeekLiveRequiresSignedLiveHintEvenWhenFlagEnabled(t *testing.T) {
	var upstreamHits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", cp6DeepSeekBridgeRouteCatalogJSONWithBaseURL(upstream.URL+"/anthropic"))
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "sk-deepseek-test-key")
	configureCP6RouteHintEnv(t)
	router := newAnthropicCompatProtocolRouteRouter()
	body := `{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"hint-false-must-stay-skeleton"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-sub2api-client-type", "claude_code_bridge_deepseek")
	req.Header.Set("x-sub2api-route", "deepseek_bridge")
	req.Header.Set("x-sub2api-route-catalog-version", "cp5-route-catalog")
	signCP6BridgeRouteHintHeaders(t, req, body, map[string]any{
		"model_id":             "deepseek-v4-pro",
		"provider":             "deepseek",
		"route":                "deepseek_bridge",
		"client_type":          "claude_code_bridge_deepseek",
		"nonce":                "cp6-deepseek-live-hint-false",
		"live_request_allowed": false,
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(0), upstreamHits.Load())
	require.Contains(t, rec.Body.String(), "bridge skeleton")
	require.NotContains(t, rec.Body.String(), "hint-false-must-stay-skeleton")
}

func TestClaudeCodeBridgeDeepSeekLiveRequiresBillingGuardBeforeProvider(t *testing.T) {
	var upstreamHits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", cp6DeepSeekBridgeRouteCatalogJSONWithBaseURL(upstream.URL+"/anthropic"))
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "sk-deepseek-test-key")
	configureCP6RouteHintEnv(t)
	router := newAnthropicCompatProtocolRouteRouter()
	body := `{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"billing-guard-must-not-leak"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-sub2api-client-type", "claude_code_bridge_deepseek")
	req.Header.Set("x-sub2api-route", "deepseek_bridge")
	req.Header.Set("x-sub2api-route-catalog-version", "cp5-route-catalog")
	signCP6BridgeRouteHintHeaders(t, req, body, map[string]any{
		"model_id":             "deepseek-v4-pro",
		"provider":             "deepseek",
		"route":                "deepseek_bridge",
		"client_type":          "claude_code_bridge_deepseek",
		"nonce":                "cp6-deepseek-live-missing-billing-guard",
		"live_request_allowed": true,
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, int64(0), upstreamHits.Load())
	require.NotContains(t, rec.Body.String(), "billing-guard-must-not-leak")
	require.NotContains(t, rec.Body.String(), "event: message_start")
}

func TestClaudeCodeBridgeDeepSeekLiveAnthropicMessagesForwardsToV1MessagesOnlyWhenFlagEnabled(t *testing.T) {
	var upstreamHits atomic.Int64
	var upstreamBody string
	var upstreamPath string
	var upstreamAuth string
	var upstreamAuthorization string
	var upstreamClientType string
	var upstreamNativeAttestation string
	var upstreamRouteHint string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		upstreamPath = r.URL.Path
		upstreamAuth = r.Header.Get("x-api-key")
		upstreamAuthorization = r.Header.Get("Authorization")
		upstreamClientType = r.Header.Get("x-sub2api-client-type")
		upstreamNativeAttestation = r.Header.Get(service.ClaudeCodeNativeAttestationHeader)
		upstreamRouteHint = r.Header.Get(service.ClaudeCodeRouteHintHeader)
		bodyBytes, _ := io.ReadAll(r.Body)
		upstreamBody = string(bodyBytes)
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "text/event-stream", r.Header.Get("Accept"))
		require.Equal(t, "2023-06-01", r.Header.Get("Anthropic-Version"))
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Deepseek-Request-Id", "req_provider_live")
		_, _ = w.Write([]byte("event: message_start\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_start","message":{"id":"msg_provider","type":"message","role":"assistant","content":[],"model":"deepseek-v4-pro","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":3,"output_tokens":0}}}` + "\n\n"))
		_, _ = w.Write([]byte("event: content_block_start\n"))
		_, _ = w.Write([]byte(`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n"))
		_, _ = w.Write([]byte("event: content_block_delta\n"))
		_, _ = w.Write([]byte(`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"provider live answer"}}` + "\n\n"))
		_, _ = w.Write([]byte("event: content_block_stop\n"))
		_, _ = w.Write([]byte(`data: {"type":"content_block_stop","index":0}` + "\n\n"))
		_, _ = w.Write([]byte("event: message_delta\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":4}}` + "\n\n"))
		_, _ = w.Write([]byte("event: message_stop\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_stop"}` + "\n\n"))
	}))
	defer upstream.Close()
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", cp6DeepSeekBridgeRouteCatalogJSONWithBaseURL(upstream.URL+"/anthropic"))
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "sk-deepseek-test-key")
	configureCP6RouteHintEnv(t)
	router := newAnthropicCompatProtocolRouteRouter()
	body := `{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"raw body must reach provider"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer user-facing-sub2api-key-must-not-forward")
	req.Header.Set("x-sub2api-client-type", "claude_code_bridge_deepseek")
	req.Header.Set("x-sub2api-route", "deepseek_bridge")
	req.Header.Set("x-sub2api-route-catalog-version", "cp5-route-catalog")
	signCP6BridgeRouteHintHeaders(t, req, body, map[string]any{
		"model_id":             "deepseek-v4-pro",
		"provider":             "deepseek",
		"route":                "deepseek_bridge",
		"client_type":          "claude_code_bridge_deepseek",
		"nonce":                "cp6-deepseek-live-enabled",
		"live_request_allowed": true,
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(1), upstreamHits.Load())
	require.Equal(t, "/anthropic/v1/messages", upstreamPath)
	require.Equal(t, body, upstreamBody)
	require.Equal(t, "sk-deepseek-test-key", upstreamAuth)
	require.Empty(t, upstreamAuthorization)
	require.Empty(t, upstreamClientType)
	require.Empty(t, upstreamNativeAttestation)
	require.Empty(t, upstreamRouteHint)
	require.Equal(t, "req_provider_live", rec.Header().Get("X-Deepseek-Request-Id"))
	require.Contains(t, rec.Body.String(), "provider live answer")
	require.NotContains(t, rec.Body.String(), "bridge skeleton")
	require.NotContains(t, rec.Body.String(), "msg_bridge_skeleton_cp5")
}

func TestClaudeCodeBridgeDeepSeekLiveRejectsNativeAttestationHeadersBeforeProvider(t *testing.T) {
	var upstreamHits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", cp6DeepSeekBridgeRouteCatalogJSONWithBaseURL(upstream.URL+"/anthropic"))
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "sk-deepseek-test-key")
	configureCP6RouteHintEnv(t)
	router := newAnthropicCompatProtocolRouteRouter()
	body := `{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"native-header-must-not-leak"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-sub2api-client-type", "claude_code_bridge_deepseek")
	req.Header.Set("x-sub2api-route", "deepseek_bridge")
	req.Header.Set("x-sub2api-route-catalog-version", "cp5-route-catalog")
	req.Header.Set(service.ClaudeCodeNativeGuardAttestedHeader, "true")
	req.Header.Set(service.ClaudeCodeNativeAttestationHeader, "forged-native-attestation")
	req.Header.Set(service.ClaudeCodeNativeSignatureHeader, "forged-native-signature")
	signCP6BridgeRouteHintHeaders(t, req, body, map[string]any{
		"model_id":             "deepseek-v4-pro",
		"provider":             "deepseek",
		"route":                "deepseek_bridge",
		"client_type":          "claude_code_bridge_deepseek",
		"nonce":                "cp6-deepseek-live-native-header",
		"live_request_allowed": true,
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, int64(0), upstreamHits.Load())
	require.NotContains(t, rec.Body.String(), "native-header-must-not-leak")
	require.NotContains(t, rec.Body.String(), "event: message_start")
}

func TestClaudeCodeBridgeDeepSeekLiveHintWhenDisabledFailsClosedBeforeProvider(t *testing.T) {
	var upstreamHits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", cp6DeepSeekBridgeRouteCatalogJSONWithBaseURL(upstream.URL+"/anthropic"))
	configureCP6RouteHintEnv(t)
	router := newAnthropicCompatProtocolRouteRouter()
	body := `{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"live-disabled-must-not-leak"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-sub2api-client-type", "claude_code_bridge_deepseek")
	req.Header.Set("x-sub2api-route", "deepseek_bridge")
	req.Header.Set("x-sub2api-route-catalog-version", "cp5-route-catalog")
	signCP6BridgeRouteHintHeaders(t, req, body, map[string]any{
		"model_id":             "deepseek-v4-pro",
		"provider":             "deepseek",
		"route":                "deepseek_bridge",
		"client_type":          "claude_code_bridge_deepseek",
		"nonce":                "cp6-deepseek-live-disabled-fail-closed",
		"live_request_allowed": true,
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, int64(0), upstreamHits.Load())
	require.NotContains(t, rec.Body.String(), "live-disabled-must-not-leak")
	require.NotContains(t, rec.Body.String(), "event: message_start")
}

func TestClaudeCodeBridgeLiveFlagDoesNotEnableOpenAIBridge(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", cp6OpenAIBridgeRouteCatalogJSON())
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "sk-deepseek-test-key")
	configureCP6RouteHintEnv(t)
	router := newAnthropicCompatProtocolRouteRouter()
	body := `{"model":"gpt-5.5","messages":[{"role":"user","content":"openai-live-must-not-leak"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-sub2api-client-type", "claude_code_bridge_openai")
	req.Header.Set("x-sub2api-route", "openai_bridge")
	req.Header.Set("x-sub2api-route-catalog-version", "cp5-route-catalog")
	signCP6BridgeRouteHintHeaders(t, req, body, map[string]any{
		"model_id":             "gpt-5.5",
		"provider":             "openai",
		"route":                "openai_bridge",
		"client_type":          "claude_code_bridge_openai",
		"nonce":                "cp6-openai-live-not-enabled",
		"live_request_allowed": true,
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.NotContains(t, rec.Body.String(), "openai-live-must-not-leak")
	require.NotContains(t, rec.Body.String(), "event: message_start")
}

func TestClaudeCodeBridgeMessagesRejectsUnsignedSpoofedBridgeHeadersBeforeSkeleton(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", cp6OpenAIBridgeRouteCatalogJSON())
	router := newAnthropicCompatProtocolRouteRouter()
	body := `{"model":"gpt-5.5","messages":[{"role":"user","content":"unsigned-bridge-must-not-leak"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-sub2api-client-type", "claude_code_bridge_openai")
	req.Header.Set("x-sub2api-route", "openai_bridge")
	req.Header.Set("x-sub2api-route-catalog-version", "cp5-route-catalog")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.NotContains(t, rec.Body.String(), "event: message_start")
	require.NotContains(t, rec.Body.String(), "unsigned-bridge-must-not-leak")
}

func TestClaudeCodeBridgeMessagesRejectsSignedRouteHintMismatchesBeforeSkeleton(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", cp6OpenAIBridgeRouteCatalogJSON())
	configureCP6RouteHintEnv(t)
	router := newAnthropicCompatProtocolRouteRouter()

	cases := []struct {
		name      string
		body      string
		overrides map[string]any
	}{
		{
			name: "body model mismatch",
			body: `{"model":"gpt-5.5","messages":[{"role":"user","content":"model-mismatch-must-not-leak"}],"stream":true}`,
			overrides: map[string]any{
				"model_id":   "deepseek-v4-pro",
				"body_model": "deepseek-v4-pro",
			},
		},
		{
			name: "bridge claims native route",
			body: `{"model":"gpt-5.5","messages":[{"role":"user","content":"native-spoof-must-not-leak"}],"stream":true}`,
			overrides: map[string]any{
				"route":                      service.ClaudeCodeNativeRoute,
				"client_type":                service.ClaudeCodeNativeClientType,
				"formal_pool_allowed":        true,
				"native_attestation_allowed": true,
				"credential_scope":           "formal_pool",
			},
		},
		{
			name: "stale hint",
			body: `{"model":"gpt-5.5","messages":[{"role":"user","content":"stale-hint-must-not-leak"}],"stream":true}`,
			overrides: map[string]any{
				"issued_at":  time.Now().Add(-2 * time.Minute).Unix(),
				"expires_at": time.Now().Add(-1 * time.Minute).Unix(),
			},
		},
		{
			name: "overlong ttl",
			body: `{"model":"gpt-5.5","messages":[{"role":"user","content":"overlong-ttl-must-not-leak"}],"stream":true}`,
			overrides: map[string]any{
				"expires_at": time.Now().Add(10 * time.Minute).Unix(),
			},
		},
		{
			name: "signed stale catalog payload",
			body: `{"model":"gpt-5.5","messages":[{"role":"user","content":"signed-stale-catalog-must-not-leak"}],"stream":true}`,
			overrides: map[string]any{
				"catalog_version": "stale-route-catalog",
			},
		},
		{
			name: "unknown key id",
			body: `{"model":"gpt-5.5","messages":[{"role":"user","content":"unknown-key-must-not-leak"}],"stream":true}`,
			overrides: map[string]any{
				"key_id": "unknown_route_hint_key",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("x-sub2api-client-type", "claude_code_bridge_openai")
			req.Header.Set("x-sub2api-route", "openai_bridge")
			req.Header.Set("x-sub2api-route-catalog-version", "cp5-route-catalog")
			fields := map[string]any{
				"model_id":    "gpt-5.5",
				"provider":    "openai",
				"route":       "openai_bridge",
				"client_type": "claude_code_bridge_openai",
				"nonce":       "route-hint-mismatch-" + strings.ReplaceAll(tc.name, " ", "-"),
			}
			for key, value := range tc.overrides {
				fields[key] = value
			}
			signCP6BridgeRouteHintHeaders(t, req, tc.body, fields)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusForbidden, rec.Code)
			require.NotContains(t, rec.Body.String(), "event: message_start")
			require.NotContains(t, rec.Body.String(), "must-not-leak")
		})
	}
}

func TestClaudeCodeBridgeMessagesRejectsReplayedRouteHintBeforeSkeleton(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", cp6OpenAIBridgeRouteCatalogJSON())
	configureCP6RouteHintEnv(t)
	router := newAnthropicCompatProtocolRouteRouter()
	body := `{"model":"gpt-5.5","messages":[{"role":"user","content":"replay-must-not-leak"}],"stream":true}`

	newSignedRequest := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-sub2api-client-type", "claude_code_bridge_openai")
		req.Header.Set("x-sub2api-route", "openai_bridge")
		req.Header.Set("x-sub2api-route-catalog-version", "cp5-route-catalog")
		signCP6BridgeRouteHintHeaders(t, req, body, map[string]any{
			"model_id":    "gpt-5.5",
			"provider":    "openai",
			"route":       "openai_bridge",
			"client_type": "claude_code_bridge_openai",
			"nonce":       "route-hint-replay-backend-test",
		})
		return req
	}

	first := httptest.NewRecorder()
	router.ServeHTTP(first, newSignedRequest())
	require.Equal(t, http.StatusOK, first.Code)

	second := httptest.NewRecorder()
	router.ServeHTTP(second, newSignedRequest())
	require.Equal(t, http.StatusForbidden, second.Code)
	require.NotContains(t, second.Body.String(), "event: message_start")
	require.NotContains(t, second.Body.String(), "replay-must-not-leak")
}

func TestClaudeCodeBridgeMessagesRouteRejectsSpoofedNativeOrCatalogMismatch(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", cp6DeepSeekBridgeRouteCatalogJSON())
	router := newAnthropicCompatProtocolRouteRouter()
	body := `{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"should-not-leak"}],"stream":true}`
	cases := []struct {
		name        string
		clientType  string
		route       string
		catalogVers string
	}{
		{name: "native client type", clientType: service.ClaudeCodeNativeClientType, route: "deepseek_bridge", catalogVers: "cp5-route-catalog"},
		{name: "native route", clientType: "claude_code_bridge_deepseek", route: service.ClaudeCodeNativeRoute, catalogVers: "cp5-route-catalog"},
		{name: "stale catalog", clientType: "claude_code_bridge_deepseek", route: "deepseek_bridge", catalogVers: "stale-catalog"},
		{name: "missing catalog", clientType: "claude_code_bridge_deepseek", route: "deepseek_bridge", catalogVers: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("x-sub2api-client-type", tc.clientType)
			req.Header.Set("x-sub2api-route", tc.route)
			req.Header.Set("x-sub2api-route-catalog-version", tc.catalogVers)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			require.NotEqual(t, http.StatusOK, rec.Code)
			require.NotContains(t, rec.Body.String(), "should-not-leak")
			require.NotContains(t, rec.Body.String(), "event: message_start")
		})
	}
}

func TestClaudeCodeBridgeCountTokensFailsClosedBeforeFormalPool(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", cp6OpenAIBridgeRouteCatalogJSON())
	router := newAnthropicCompatProtocolRouteRouter()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(`{"model":"gpt-5.5","messages":[{"role":"user","content":"count-token-prompt-must-not-leak"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-sub2api-client-type", "claude_code_bridge_openai")
	req.Header.Set("x-sub2api-route", "openai_bridge")
	req.Header.Set("x-sub2api-route-catalog-version", "cp5-route-catalog")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.NotContains(t, rec.Body.String(), "count-token-prompt-must-not-leak")
	require.NotContains(t, rec.Body.String(), "gpt-5.5")
}

func TestClaudeCodeBridgeCountTokensCatalogBridgeModelFailsClosedWithoutBridgeMarker(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", cp6OpenAIBridgeRouteCatalogJSON())
	router := newAnthropicCompatProtocolRouteRouter()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(`{"model":"gpt-5.5","messages":[{"role":"user","content":"catalog-bridge-count-token-prompt-must-not-leak"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.NotContains(t, rec.Body.String(), "catalog-bridge-count-token-prompt-must-not-leak")
	require.NotContains(t, rec.Body.String(), "gpt-5.5")
}

func TestClaudeCodeBridgeMessagesAllowsAnthropicValidMetadataStopSequencesAndTopK(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", cp6OpenAIBridgeRouteCatalogJSON())
	configureCP6RouteHintEnv(t)
	router := newAnthropicCompatProtocolRouteRouter()
	body := `{"model":"gpt-5.5","messages":[{"role":"user","content":"anthropic-valid-fields-must-not-leak"}],"stream":true,"metadata":{"user_id":"safe-user"},"stop_sequences":["DONE"],"top_k":5,"thinking":{"type":"enabled","budget_tokens":1024}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-sub2api-client-type", "claude_code_bridge_openai")
	req.Header.Set("x-sub2api-route", "openai_bridge")
	req.Header.Set("x-sub2api-route-catalog-version", "cp5-route-catalog")
	signCP6BridgeRouteHintHeaders(t, req, body, map[string]any{
		"model_id":    "gpt-5.5",
		"provider":    "openai",
		"route":       "openai_bridge",
		"client_type": "claude_code_bridge_openai",
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "event: message_start")
	require.NotContains(t, rec.Body.String(), "anthropic-valid-fields-must-not-leak")
}

func TestClaudeCodeBridgeMessagesRejectOpenAIShapedBody(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", cp6OpenAIBridgeRouteCatalogJSON())
	router := newAnthropicCompatProtocolRouteRouter()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"gpt-5.5","input":"bridge-openai-shape-must-not-leak","messages":[{"role":"user","content":"hello"}],"stream":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-sub2api-client-type", "claude_code_bridge_openai")
	req.Header.Set("x-sub2api-route", "openai_bridge")
	req.Header.Set("x-sub2api-route-catalog-version", "cp5-route-catalog")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.NotContains(t, rec.Body.String(), "bridge-openai-shape-must-not-leak")
	require.NotContains(t, rec.Body.String(), "hello")
}

func TestClaudeCodeBridgeMessagesRejectOpenAIFunctionToolShape(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", cp6OpenAIBridgeRouteCatalogJSON())
	router := newAnthropicCompatProtocolRouteRouter()
	body := `{"model":"gpt-5.5","messages":[{"role":"user","content":"function-tool-shape-must-not-leak"}],"stream":true,"tools":[{"type":"function","function":{"name":"leak","parameters":{"type":"object"}}}],"tool_choice":{"type":"function","function":{"name":"leak"}}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-sub2api-client-type", "claude_code_bridge_openai")
	req.Header.Set("x-sub2api-route", "openai_bridge")
	req.Header.Set("x-sub2api-route-catalog-version", "cp5-route-catalog")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.NotContains(t, rec.Body.String(), "function-tool-shape-must-not-leak")
	require.NotContains(t, rec.Body.String(), "leak")
}

func TestClaudeCodeBridgeMessagesRejectOpenAIFunctionToolTypeWithoutFunctionProperty(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", cp6OpenAIBridgeRouteCatalogJSON())
	router := newAnthropicCompatProtocolRouteRouter()
	body := `{"model":"gpt-5.5","messages":[{"role":"user","content":"function-tool-type-must-not-leak"}],"stream":true,"tools":[{"type":"function","name":"leak","parameters":{"type":"object"}}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-sub2api-client-type", "claude_code_bridge_openai")
	req.Header.Set("x-sub2api-route", "openai_bridge")
	req.Header.Set("x-sub2api-route-catalog-version", "cp5-route-catalog")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.NotContains(t, rec.Body.String(), "function-tool-type-must-not-leak")
	require.NotContains(t, rec.Body.String(), "leak")
}

func TestClaudeCodeBridgeMessagesRejectInvalidAnthropicToolShapes(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", cp6OpenAIBridgeRouteCatalogJSON())
	router := newAnthropicCompatProtocolRouteRouter()
	cases := []struct {
		name string
		body string
		leak string
	}{
		{name: "tools not array", body: `{"model":"gpt-5.5","messages":[{"role":"user","content":"tools-object-must-not-leak"}],"stream":true,"tools":{"name":"leak"}}`, leak: "tools-object-must-not-leak"},
		{name: "tool missing name", body: `{"model":"gpt-5.5","messages":[{"role":"user","content":"missing-name-must-not-leak"}],"stream":true,"tools":[{"input_schema":{"type":"object"}}]}`, leak: "missing-name-must-not-leak"},
		{name: "tool missing input_schema", body: `{"model":"gpt-5.5","messages":[{"role":"user","content":"missing-schema-must-not-leak"}],"stream":true,"tools":[{"name":"leak"}]}`, leak: "missing-schema-must-not-leak"},
		{name: "tool_choice missing name", body: `{"model":"gpt-5.5","messages":[{"role":"user","content":"bad-choice-must-not-leak"}],"stream":true,"tools":[{"name":"get_weather","input_schema":{"type":"object"}}],"tool_choice":{"type":"tool"}}`, leak: "bad-choice-must-not-leak"},
		{name: "tool name dot not Anthropic compatible", body: `{"model":"gpt-5.5","messages":[{"role":"user","content":"bad-name-must-not-leak"}],"stream":true,"tools":[{"name":"unsafe.tool","input_schema":{"type":"object"}}]}`, leak: "bad-name-must-not-leak"},
		{name: "tool_choice string not object", body: `{"model":"gpt-5.5","messages":[{"role":"user","content":"choice-string-must-not-leak"}],"stream":true,"tools":[{"name":"get_weather","input_schema":{"type":"object"}}],"tool_choice":"auto"}`, leak: "choice-string-must-not-leak"},
		{name: "tool_choice names unknown tool", body: `{"model":"gpt-5.5","messages":[{"role":"user","content":"unknown-choice-must-not-leak"}],"stream":true,"tools":[{"name":"get_weather","input_schema":{"type":"object"}}],"tool_choice":{"type":"tool","name":"unknown_tool"}}`, leak: "unknown-choice-must-not-leak"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("x-sub2api-client-type", "claude_code_bridge_openai")
			req.Header.Set("x-sub2api-route", "openai_bridge")
			req.Header.Set("x-sub2api-route-catalog-version", "cp5-route-catalog")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusForbidden, rec.Code)
			require.NotContains(t, rec.Body.String(), tc.leak)
			require.NotContains(t, rec.Body.String(), "leak")
			require.NotContains(t, rec.Body.String(), "event: message_start")
		})
	}
}

func TestClaudeCodeBridgeMessagesRejectOpenAIResponsesTopLevelFields(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", cp6OpenAIBridgeRouteCatalogJSON())
	router := newAnthropicCompatProtocolRouteRouter()
	body := `{"model":"gpt-5.5","messages":[{"role":"user","content":"responses-fields-must-not-leak"}],"stream":true,"reasoning":{"effort":"low"},"text":{"format":{"type":"text"}},"include":["message.output_text.logprobs"],"previous_response_id":"resp_leak","truncation":"auto","prompt_cache_key":"cache-leak","max_output_tokens":128,"conversation":"conv_leak","background":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-sub2api-client-type", "claude_code_bridge_openai")
	req.Header.Set("x-sub2api-route", "openai_bridge")
	req.Header.Set("x-sub2api-route-catalog-version", "cp5-route-catalog")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.NotContains(t, rec.Body.String(), "responses-fields-must-not-leak")
	require.NotContains(t, rec.Body.String(), "cache-leak")
	require.NotContains(t, rec.Body.String(), "resp_leak")
}

func TestClaudeCodeBridgeMessagesRejectOpenAIChatTopLevelFields(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", cp6OpenAIBridgeRouteCatalogJSON())
	router := newAnthropicCompatProtocolRouteRouter()
	body := `{"model":"gpt-5.5","messages":[{"role":"user","content":"chat-fields-must-not-leak"}],"stream":true,"n":2,"stop":["secret-stop"],"stream_options":{"include_usage":true},"user":"user-leak"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-sub2api-client-type", "claude_code_bridge_openai")
	req.Header.Set("x-sub2api-route", "openai_bridge")
	req.Header.Set("x-sub2api-route-catalog-version", "cp5-route-catalog")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.NotContains(t, rec.Body.String(), "chat-fields-must-not-leak")
	require.NotContains(t, rec.Body.String(), "secret-stop")
	require.NotContains(t, rec.Body.String(), "user-leak")
}
