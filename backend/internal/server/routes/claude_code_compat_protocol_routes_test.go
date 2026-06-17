package routes

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
