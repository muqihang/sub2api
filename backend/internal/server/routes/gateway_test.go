package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newGatewayRoutesTestRouter() *gin.Engine {
	return newGatewayRoutesTestRouterWithAugmentGatewaySpy(nil)
}

func newGatewayRoutesTestRouterWithAugmentGatewaySpy(spy *gatewayRoutesAugmentGatewayExecutorSpy) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	augmentGatewayService := (*service.AugmentGatewayService)(nil)
	if spy != nil {
		augmentCfg := config.GatewayAugmentConfig{
			Enabled:       true,
			EnabledModels: []string{"gpt-5.4", "gpt-5.5", "gpt-5.4-mini", "deepseek-v4-pro", "deepseek-v4-flash"},
			ProviderGroups: config.GatewayAugmentProviderGroupsConfig{
				OpenAI:   1001,
				DeepSeek: 1002,
			},
		}
		registry := service.NewAugmentGatewayModelRegistry(augmentCfg)
		augmentGatewayService = service.NewAugmentGatewayService(
			&config.Config{Gateway: config.GatewayConfig{Augment: augmentCfg}},
			registry,
			service.NewAugmentGatewayRouter(registry),
			spy,
			service.NewAugmentGatewayReasoningTurnStore(),
		)
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
				augmentGatewayService,
			),
			Gateway:       &handler.GatewayHandler{},
			OpenAIGateway: &handler.OpenAIGatewayHandler{},
		},
		servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
			groupID := int64(1)
			c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{
				GroupID: &groupID,
				Group:   &service.Group{Platform: service.PlatformOpenAI},
			})
			c.Next()
		}),
		nil,
		nil,
		nil,
		nil,
		&config.Config{},
	)

	return router
}

type gatewayRoutesAugmentGatewayExecutorSpy struct {
	mu            sync.Mutex
	completeCalls int
	streamCalls   int
}

func (s *gatewayRoutesAugmentGatewayExecutorSpy) Complete(ctx context.Context, req service.AugmentGatewayProviderRequest) (service.AugmentGatewayProviderResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completeCalls++
	return service.AugmentGatewayProviderResult{}, nil
}

func (s *gatewayRoutesAugmentGatewayExecutorSpy) Stream(ctx context.Context, req service.AugmentGatewayProviderRequest, emit func(service.AugmentGatewayProviderChunk) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.streamCalls++
	return nil
}

func (s *gatewayRoutesAugmentGatewayExecutorSpy) CompleteCalls() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.completeCalls
}

func (s *gatewayRoutesAugmentGatewayExecutorSpy) StreamCalls() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.streamCalls
}

func TestGatewayRoutesOpenAIResponsesCompactPathIsRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter()

	for _, path := range []string{
		"/v1/responses/compact",
		"/responses/compact",
		"/backend-api/codex/responses",
		"/backend-api/codex/responses/compact",
		"/openai/v1/responses",
		"/openai/v1/responses/compact",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-5"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.NotEqual(t, http.StatusNotFound, w.Code, "path=%s should hit OpenAI responses handler", path)
	}
}

type gatewayRoutesAugmentAuthStub struct {
	userID int64
}

func (s gatewayRoutesAugmentAuthStub) GenerateTokenPair(ctx context.Context, user *service.User, familyID string) (*service.TokenPair, error) {
	return nil, nil
}

func (s gatewayRoutesAugmentAuthStub) RefreshTokenPair(ctx context.Context, refreshToken string) (*service.TokenPairWithUser, error) {
	return nil, nil
}

func (s gatewayRoutesAugmentAuthStub) ValidateToken(token string) (*service.JWTClaims, error) {
	if token != "augment-local-session" {
		return nil, service.ErrInvalidToken
	}
	return &service.JWTClaims{UserID: s.userID}, nil
}

type gatewayRoutesAugmentUserStub struct {
	user *service.User
}

func (s gatewayRoutesAugmentUserStub) GetByID(ctx context.Context, id int64) (*service.User, error) {
	if s.user != nil && s.user.ID == id {
		return s.user, nil
	}
	return nil, service.ErrUserNotFound
}

type gatewayRoutesAugmentAPIKeyStub struct {
	apiKeyByValue map[string]*service.APIKey
	keysByUser    map[int64][]service.APIKey
}

func (s gatewayRoutesAugmentAPIKeyStub) GetByKey(ctx context.Context, key string) (*service.APIKey, error) {
	if apiKey, ok := s.apiKeyByValue[key]; ok {
		return apiKey, nil
	}
	return nil, service.ErrAPIKeyNotFound
}

func (s gatewayRoutesAugmentAPIKeyStub) List(ctx context.Context, userID int64, params pagination.PaginationParams, filters service.APIKeyListFilters) ([]service.APIKey, *pagination.PaginationResult, error) {
	return s.keysByUser[userID], &pagination.PaginationResult{}, nil
}

func (s gatewayRoutesAugmentAPIKeyStub) GetAvailableGroups(ctx context.Context, userID int64) ([]service.Group, error) {
	return nil, nil
}

func (s gatewayRoutesAugmentAPIKeyStub) Create(ctx context.Context, userID int64, req service.CreateAPIKeyRequest) (*service.APIKey, error) {
	return nil, service.ErrAPIKeyNotFound
}

type gatewayRoutesAugmentSubscriptionStub struct{}

func (gatewayRoutesAugmentSubscriptionStub) ListActiveUserSubscriptions(ctx context.Context, userID int64) ([]service.UserSubscription, error) {
	return nil, nil
}

type gatewayRoutesAugmentSettingStub struct{}

func (gatewayRoutesAugmentSettingStub) GetPublicSettings(ctx context.Context) (*service.PublicSettings, error) {
	return &service.PublicSettings{}, nil
}

func (gatewayRoutesAugmentSettingStub) GetSiteName(ctx context.Context) string {
	return "Sub2API"
}

func TestGatewayRoutesResponsesAcceptsAugmentSessionBearer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	user := &service.User{
		ID:     42,
		Email:  "augment@example.com",
		Role:   service.RoleUser,
		Status: service.StatusActive,
	}
	gatewayKey := service.APIKey{
		ID:     10,
		UserID: user.ID,
		Key:    "sk-gateway-for-augment",
		Status: service.StatusActive,
		User:   user,
	}
	pluginService := service.NewAugmentPluginService(
		&config.Config{},
		gatewayRoutesAugmentAuthStub{userID: user.ID},
		gatewayRoutesAugmentUserStub{user: user},
		gatewayRoutesAugmentAPIKeyStub{keysByUser: map[int64][]service.APIKey{user.ID: []service.APIKey{gatewayKey}}},
		gatewayRoutesAugmentSubscriptionStub{},
		gatewayRoutesAugmentSettingStub{},
	)
	authHandler := handler.NewAuthHandler(
		&config.Config{},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		pluginService,
	)

	var observedAuthorization string
	RegisterGatewayRoutes(
		router,
		&handler.Handlers{
			Auth:          authHandler,
			Gateway:       &handler.GatewayHandler{},
			OpenAIGateway: &handler.OpenAIGatewayHandler{},
		},
		servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
			observedAuthorization = c.GetHeader("Authorization")
			if observedAuthorization != "Bearer "+gatewayKey.Key {
				c.AbortWithStatus(http.StatusUnauthorized)
				return
			}
			c.AbortWithStatus(http.StatusNoContent)
		}),
		nil,
		nil,
		nil,
		nil,
		&config.Config{},
	)

	req := httptest.NewRequest(http.MethodPost, "/responses", strings.NewReader(`{"model":"gpt-5.4","input":"title"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer augment-local-session")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
	require.Equal(t, "Bearer "+gatewayKey.Key, observedAuthorization)
}

func TestAugmentOfficialRoutePolicyGatewayRoutesFailClosedWhenPolicyEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	user := &service.User{
		ID:     42,
		Email:  "augment@example.com",
		Role:   service.RoleUser,
		Status: service.StatusActive,
	}
	apiKey := &service.APIKey{
		ID:        10,
		UserID:    user.ID,
		Key:       "sk-gateway-for-augment",
		Status:    service.StatusActive,
		CreatedAt: time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
		User:      user,
	}
	pluginService := service.NewAugmentPluginService(
		&config.Config{
			Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"},
			Gateway: config.GatewayConfig{
				Augment: config.GatewayAugmentConfig{Enabled: true},
			},
		},
		gatewayRoutesAugmentAuthStub{userID: user.ID},
		gatewayRoutesAugmentUserStub{user: user},
		gatewayRoutesAugmentAPIKeyStub{
			apiKeyByValue: map[string]*service.APIKey{
				apiKey.Key: apiKey,
			},
			keysByUser: map[int64][]service.APIKey{user.ID: []service.APIKey{*apiKey}},
		},
		gatewayRoutesAugmentSubscriptionStub{},
		gatewayRoutesAugmentSettingStub{},
	)
	authHandler := handler.NewAuthHandler(
		&config.Config{
			Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"},
			Gateway: config.GatewayConfig{
				Augment: config.GatewayAugmentConfig{Enabled: true},
			},
		},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		pluginService,
	)

	RegisterGatewayRoutes(
		router,
		&handler.Handlers{
			Auth:          authHandler,
			Gateway:       &handler.GatewayHandler{},
			OpenAIGateway: &handler.OpenAIGatewayHandler{},
		},
		servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) { c.Next() }),
		nil,
		nil,
		nil,
		nil,
		&config.Config{},
	)

	req := httptest.NewRequest(http.MethodPost, "/prompt-enhancer", strings.NewReader(`{"model":"gpt-5.4","nodes":[{"id":1,"type":0,"text_node":{"content":"rewrite this prompt"}}]}`))
	req.Header.Set("Authorization", "Bearer "+apiKey.Key)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, rec.Body.String(), "AUGMENT_OFFICIAL_ROUTE_REQUIRED")
}

func TestGatewayRoutesOpenAIChatCompletionsLoopbackPathIsRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter()

	for _, path := range []string{
		"/v1/chat/completions",
		"/chat/completions",
		"/openai/v1/chat/completions",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-5","messages":[]}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.NotEqual(t, http.StatusNotFound, w.Code, "path=%s should hit OpenAI chat completions handler", path)
	}
}

func TestGatewayRoutesOpenAIImagesPathsAreRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter()

	for _, path := range []string{
		"/v1/images/generations",
		"/v1/images/edits",
		"/images/generations",
		"/images/edits",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-image-2","prompt":"draw a cat"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.NotEqual(t, http.StatusNotFound, w.Code, "path=%s should hit OpenAI images handler", path)
	}
}

func TestGatewayRoutesLegacyAugmentEndpointsAreRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter()

	tests := []struct {
		method string
		path   string
	}{
		{method: http.MethodPost, path: "/batch-upload"},
		{method: http.MethodPost, path: "/checkpoint-blobs"},
		{method: http.MethodPost, path: "/find-missing"},
		{method: http.MethodPost, path: "/chat"},
		{method: http.MethodPost, path: "/chat-stream"},
		{method: http.MethodPost, path: "/prompt-enhancer"},
		{method: http.MethodPost, path: "/instruction-stream"},
		{method: http.MethodPost, path: "/next-edit-stream"},
		{method: http.MethodPost, path: "/remote-agents/list"},
		{method: http.MethodPost, path: "/agents/codebase-retrieval"},
		{method: http.MethodGet, path: "/usage/api/balance"},
		{method: http.MethodGet, path: "/usage/api/get-models"},
		{method: http.MethodGet, path: "/usage/api/getLoginToken"},
	}

	for _, tc := range tests {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)
			require.NotEqual(t, http.StatusNotFound, w.Code, "path=%s should be registered", tc.path)
		})
	}
}

func TestGatewayRoutesOrdinaryPathsDoNotTouchAugmentGateway(t *testing.T) {
	t.Parallel()

	spy := &gatewayRoutesAugmentGatewayExecutorSpy{}
	router := newGatewayRoutesTestRouterWithAugmentGatewaySpy(spy)

	tests := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodPost, path: "/openai/v1/responses", body: `{"model":"gpt-5","input":"title"}`},
		{method: http.MethodPost, path: "/openai/v1/chat/completions", body: `{"model":"gpt-5","messages":[]}`},
		{method: http.MethodPost, path: "/v1/messages", body: `{"model":"claude-sonnet-4-5","messages":[]}`},
		{method: http.MethodGet, path: "/v1beta/models", body: ""},
		{method: http.MethodPost, path: "/backend-api/codex/responses", body: `{"model":"gpt-5","input":"title"}`},
	}

	for _, tc := range tests {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			require.Zero(t, spy.CompleteCalls(), "augment gateway executor must not be used by %s", tc.path)
			require.Zero(t, spy.StreamCalls(), "augment gateway executor must not be used by %s", tc.path)
		})
	}
}
