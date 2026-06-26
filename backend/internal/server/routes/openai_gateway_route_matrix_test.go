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

type openAIGatewayRouteMatrixCase struct {
	method string
	path   string
	body   string
	ws     bool
}

var openAIGatewayRouteMatrixCases = []openAIGatewayRouteMatrixCase{
	{method: http.MethodPost, path: "/openai/v1/responses", body: `{"model":"gpt-5","input":"hi"}`},
	{method: http.MethodPost, path: "/v1/responses", body: `{"model":"gpt-5","input":"hi"}`},
	{method: http.MethodPost, path: "/responses", body: `{"model":"gpt-5","input":"hi"}`},
	{method: http.MethodPost, path: "/backend-api/codex/responses", body: `{"model":"gpt-5","input":"hi"}`},
	{method: http.MethodPost, path: "/openai/v1/chat/completions", body: `{"model":"gpt-5","messages":[]}`},
	{method: http.MethodPost, path: "/v1/chat/completions", body: `{"model":"gpt-5","messages":[]}`},
	{method: http.MethodPost, path: "/chat/completions", body: `{"model":"gpt-5","messages":[]}`},
	{method: http.MethodPost, path: "/openai/v1/images/generations", body: `{"model":"gpt-image-2","prompt":"draw"}`},
	{method: http.MethodPost, path: "/v1/images/generations", body: `{"model":"gpt-image-2","prompt":"draw"}`},
	{method: http.MethodPost, path: "/images/generations", body: `{"model":"gpt-image-2","prompt":"draw"}`},
	{method: http.MethodGet, path: "/openai/v1/responses", ws: true},
	{method: http.MethodGet, path: "/v1/responses", ws: true},
	{method: http.MethodGet, path: "/responses", ws: true},
	{method: http.MethodGet, path: "/backend-api/codex/responses", ws: true},
}

func newOpenAIGatewayRouteMatrixRouter(platform string, coreEnabled bool, clientTokens []config.OpenAIGatewayClientTokenConfig) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = coreEnabled
	cfg.Gateway.OpenAICore.ClientTokens = clientTokens
	core := service.NewOpenAIGatewayCoreService(nil, cfg, nil)

	groupID := int64(1)
	user := &service.User{ID: 101, Role: service.RoleUser, Status: service.StatusActive, Concurrency: 1}
	group := &service.Group{
		ID:                    groupID,
		Platform:              platform,
		AllowImageGeneration:  true,
		AllowMessagesDispatch: true,
	}

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
			OpenAIGateway: handler.NewOpenAIGatewayHandler(nil, core, nil, nil, nil, nil, nil, cfg),
		},
		servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
			groupCopy := *group
			apiKey := &service.APIKey{
				ID:      11,
				UserID:  user.ID,
				User:    user,
				GroupID: &groupID,
				Group:   &groupCopy,
				Status:  service.StatusActive,
			}
			if strings.HasPrefix(c.Request.URL.Path, "/backend-api/codex/responses") {
				product := service.CodexUsageClientProduct
				apiKey.RestrictedClientProduct = &product
				apiKey.Group.CodexGatewayEntitled = true
			}
			c.Set(string(servermiddleware.ContextKeyAPIKey), apiKey)
			c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{
				UserID:      user.ID,
				Concurrency: user.Concurrency,
			})
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

func newOpenAIGatewayRouteMatrixRequest(tc openAIGatewayRouteMatrixCase) *http.Request {
	body := strings.NewReader(tc.body)
	req := httptest.NewRequest(tc.method, tc.path, body)
	if tc.method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	if tc.ws {
		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Upgrade", "websocket")
		req.Header.Set("Sec-WebSocket-Version", "13")
		req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	}
	return req
}

func TestOpenAIGatewayRouteMatrix_ClientTokenParity(t *testing.T) {
	router := newOpenAIGatewayRouteMatrixRouter(service.PlatformOpenAI, true, []config.OpenAIGatewayClientTokenConfig{
		{Name: "codex", Token: "gateway-token"},
	})

	for _, tc := range openAIGatewayRouteMatrixCases {
		t.Run(tc.method+" "+tc.path+" invalid token", func(t *testing.T) {
			req := newOpenAIGatewayRouteMatrixRequest(tc)
			req.Header.Set(service.OpenAIGatewayClientTokenHeader, "bad-token")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusUnauthorized, rec.Code)
			require.Contains(t, rec.Body.String(), "Invalid OpenAI gateway client token")
		})

		t.Run(tc.method+" "+tc.path+" missing token", func(t *testing.T) {
			req := newOpenAIGatewayRouteMatrixRequest(tc)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusUnauthorized, rec.Code)
			require.Contains(t, rec.Body.String(), "OpenAI gateway client token required")
		})
	}
}

func TestOpenAIGatewayRouteMatrix_MissingClientTokenAllowedWhenUnconfigured(t *testing.T) {
	router := newOpenAIGatewayRouteMatrixRouter(service.PlatformOpenAI, true, nil)

	for _, tc := range openAIGatewayRouteMatrixCases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := newOpenAIGatewayRouteMatrixRequest(tc)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			require.NotEqual(t, http.StatusUnauthorized, rec.Code)
		})
	}
}

func TestOpenAIGatewayRouteMatrix_NonOpenAIGroupDoesNotEnterGateway(t *testing.T) {
	router := newOpenAIGatewayRouteMatrixRouter(service.PlatformAnthropic, true, []config.OpenAIGatewayClientTokenConfig{
		{Name: "codex", Token: "gateway-token"},
	})

	for _, tc := range openAIGatewayRouteMatrixCases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := newOpenAIGatewayRouteMatrixRequest(tc)
			req.Header.Set(service.OpenAIGatewayClientTokenHeader, "bad-token")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			require.NotContains(t, rec.Body.String(), "Invalid OpenAI gateway client token")
		})
	}
}

func TestOpenAIGatewayRouteMatrix_CoreDisabledFailsClosed(t *testing.T) {
	router := newOpenAIGatewayRouteMatrixRouter(service.PlatformOpenAI, false, nil)

	for _, tc := range openAIGatewayRouteMatrixCases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := newOpenAIGatewayRouteMatrixRequest(tc)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusServiceUnavailable, rec.Code)
			require.Contains(t, rec.Body.String(), "OpenAI gateway core disabled")
		})
	}
}

func TestClaudeCodeNativeRouteMatrix_BypassesOpenAIGroupAutoRoute(t *testing.T) {
	headers := http.Header{}
	headers.Set(service.ClaudeCodeNativeClientTypeHeader, service.ClaudeCodeNativeClientType)

	require.False(t, shouldAutoRouteOpenAIGroupToOpenAI(headers))
	require.False(t, shouldRejectOpenAIGroupCountTokens(headers))

	bridgeHeaders := http.Header{}
	bridgeHeaders.Set(service.ClaudeCodeNativeClientTypeHeader, "claude_code_bridge_openai")
	require.False(t, shouldAutoRouteOpenAIGroupToOpenAI(bridgeHeaders), "bridge route must stay on Claude Code bridge skeleton and not enter generic OpenAI auto-route")
	require.True(t, shouldRejectOpenAIGroupCountTokens(bridgeHeaders))

	require.True(t, shouldAutoRouteOpenAIGroupToOpenAI(http.Header{}))
	require.True(t, shouldRejectOpenAIGroupCountTokens(http.Header{}))
}

func TestClaudeCodeNativeMessagesAuthUsesDedicatedAPIKeyWhenBearerIsNotManagedJWT(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	var apiKeyCalled bool
	var nativeCalled bool
	auth := claudeCodeNativeMessagesAuth(
		servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
			apiKeyCalled = true
			c.Next()
		}),
		servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
			nativeCalled = true
			c.Next()
		}),
	)
	router.POST("/v1/messages", gin.HandlerFunc(auth), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4-6"}`))
	req.Header.Set("Authorization", "Bearer sk-claude-code-dedicated-sub2api-key")
	req.Header.Set(service.ClaudeCodeNativeClientTypeHeader, service.ClaudeCodeNativeClientType)
	req.Header.Set("X-Zhumeng-Device-ID", "9")
	req.Header.Set("X-Zhumeng-Managed-Session", "managed-session")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.True(t, apiKeyCalled)
	require.False(t, nativeCalled)
}

func TestClaudeCodeNativeMessagesAuthUsesManagedAuthForJWTBearer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	var apiKeyCalled bool
	var nativeCalled bool
	auth := claudeCodeNativeMessagesAuth(
		servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
			apiKeyCalled = true
			c.Next()
		}),
		servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
			nativeCalled = true
			c.Next()
		}),
	)
	router.POST("/v1/messages", gin.HandlerFunc(auth), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4-6"}`))
	req.Header.Set("Authorization", "Bearer eyJhbGciOiJIUzI1NiJ9.payload.signature")
	req.Header.Set(service.ClaudeCodeNativeClientTypeHeader, service.ClaudeCodeNativeClientType)
	req.Header.Set("X-Zhumeng-Device-ID", "9")
	req.Header.Set("X-Zhumeng-Managed-Session", "managed-session")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.False(t, apiKeyCalled)
	require.True(t, nativeCalled)
}
