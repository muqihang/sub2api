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
