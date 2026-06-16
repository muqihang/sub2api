package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", `{"catalog_version":"cp5-route-catalog","runtime_hash":"sha256:1111111111111111111111111111111111111111111111111111111111111111","overlay_hash":"sha256:2222222222222222222222222222222222222222222222222222222222222222","catalog_hash":"sha256:3333333333333333333333333333333333333333333333333333333333333333","models":[{"model_id":"gpt-5.5","provider":"openai","route":"openai_bridge","client_type":"claude_code_bridge_openai","provider_owner":"zhumeng_managed","credential_scope":"bridge_pool","gateway_location":"cloud","catalog_fresh":true}]}`)
	router := newAnthropicCompatProtocolRouteRouter()
	body := `{"model":"gpt-5.5","messages":[{"role":"user","content":"should-not-leak"}],"stream":true,"tools":[{"name":"get_weather","input_schema":{"type":"object"}}],"tool_choice":{"type":"tool","name":"get_weather"}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-sub2api-client-type", "claude_code_bridge_openai")
	req.Header.Set("x-sub2api-route", "openai_bridge")
	req.Header.Set("x-sub2api-route-catalog-version", "cp5-route-catalog")
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

func TestClaudeCodeBridgeMessagesRouteRejectsSpoofedNativeOrCatalogMismatch(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", `{"catalog_version":"cp5-route-catalog","models":[{"model_id":"deepseek-v4-pro","provider":"deepseek","route":"deepseek_bridge","client_type":"claude_code_bridge_deepseek","provider_owner":"zhumeng_managed","credential_scope":"bridge_pool","gateway_location":"cloud","catalog_fresh":true}]}`)
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
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", `{"catalog_version":"cp5-route-catalog","models":[{"model_id":"gpt-5.5","provider":"openai","route":"openai_bridge","client_type":"claude_code_bridge_openai","provider_owner":"zhumeng_managed","credential_scope":"bridge_pool","gateway_location":"cloud","catalog_fresh":true}]}`)
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
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", `{"catalog_version":"cp5-route-catalog","models":[{"model_id":"gpt-5.5","provider":"openai","route":"openai_bridge","client_type":"claude_code_bridge_openai","provider_owner":"zhumeng_managed","credential_scope":"bridge_pool","gateway_location":"cloud","catalog_fresh":true}]}`)
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
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", `{"catalog_version":"cp5-route-catalog","models":[{"model_id":"gpt-5.5","provider":"openai","route":"openai_bridge","client_type":"claude_code_bridge_openai","provider_owner":"zhumeng_managed","credential_scope":"bridge_pool","gateway_location":"cloud","catalog_fresh":true}]}`)
	router := newAnthropicCompatProtocolRouteRouter()
	body := `{"model":"gpt-5.5","messages":[{"role":"user","content":"anthropic-valid-fields-must-not-leak"}],"stream":true,"metadata":{"user_id":"safe-user"},"stop_sequences":["DONE"],"top_k":5,"thinking":{"type":"enabled","budget_tokens":1024}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-sub2api-client-type", "claude_code_bridge_openai")
	req.Header.Set("x-sub2api-route", "openai_bridge")
	req.Header.Set("x-sub2api-route-catalog-version", "cp5-route-catalog")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "event: message_start")
	require.NotContains(t, rec.Body.String(), "anthropic-valid-fields-must-not-leak")
}

func TestClaudeCodeBridgeMessagesRejectOpenAIShapedBody(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", `{"catalog_version":"cp5-route-catalog","models":[{"model_id":"gpt-5.5","provider":"openai","route":"openai_bridge","client_type":"claude_code_bridge_openai","provider_owner":"zhumeng_managed","credential_scope":"bridge_pool","gateway_location":"cloud","catalog_fresh":true}]}`)
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
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", `{"catalog_version":"cp5-route-catalog","models":[{"model_id":"gpt-5.5","provider":"openai","route":"openai_bridge","client_type":"claude_code_bridge_openai","provider_owner":"zhumeng_managed","credential_scope":"bridge_pool","gateway_location":"cloud","catalog_fresh":true}]}`)
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
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", `{"catalog_version":"cp5-route-catalog","models":[{"model_id":"gpt-5.5","provider":"openai","route":"openai_bridge","client_type":"claude_code_bridge_openai","provider_owner":"zhumeng_managed","credential_scope":"bridge_pool","gateway_location":"cloud","catalog_fresh":true}]}`)
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
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", `{"catalog_version":"cp5-route-catalog","models":[{"model_id":"gpt-5.5","provider":"openai","route":"openai_bridge","client_type":"claude_code_bridge_openai","provider_owner":"zhumeng_managed","credential_scope":"bridge_pool","gateway_location":"cloud","catalog_fresh":true}]}`)
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
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", `{"catalog_version":"cp5-route-catalog","models":[{"model_id":"gpt-5.5","provider":"openai","route":"openai_bridge","client_type":"claude_code_bridge_openai","provider_owner":"zhumeng_managed","credential_scope":"bridge_pool","gateway_location":"cloud","catalog_fresh":true}]}`)
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
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", `{"catalog_version":"cp5-route-catalog","models":[{"model_id":"gpt-5.5","provider":"openai","route":"openai_bridge","client_type":"claude_code_bridge_openai","provider_owner":"zhumeng_managed","credential_scope":"bridge_pool","gateway_location":"cloud","catalog_fresh":true}]}`)
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
