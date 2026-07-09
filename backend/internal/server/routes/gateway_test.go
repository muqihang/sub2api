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
	return newGatewayRoutesTestRouterWithPlatformAndAugmentGatewaySpy(service.PlatformOpenAI, nil)
}

func newGatewayRoutesTestRouterWithAugmentGatewaySpy(spy *gatewayRoutesAugmentGatewayExecutorSpy) *gin.Engine {
	return newGatewayRoutesTestRouterWithPlatformAndAugmentGatewaySpy(service.PlatformOpenAI, spy)
}

func newGatewayRoutesTestRouterWithPlatform(platform string) *gin.Engine {
	return newGatewayRoutesTestRouterWithPlatformAndAugmentGatewaySpy(platform, nil)
}

func newGatewayRoutesTestRouterWithPlatformAndAugmentGatewaySpy(platform string, spy *gatewayRoutesAugmentGatewayExecutorSpy) *gin.Engine {
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
				Group:   &service.Group{Platform: platform},
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

func TestGatewayRoutesGrokMediaRoutesAreRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouterWithPlatform(service.PlatformGrok)

	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/v1/images/generations", `{"model":"grok-imagine","prompt":"draw"}`},
		{http.MethodPost, "/v1/images/edits", `{"model":"grok-imagine-edit","prompt":"edit"}`},
		{http.MethodPost, "/v1/videos/generations", `{"model":"grok-imagine-video","prompt":"video"}`},
		{http.MethodGet, "/v1/videos/video_123", ``},
		{http.MethodPost, "/images/generations", `{"model":"grok-imagine","prompt":"draw"}`},
		{http.MethodPost, "/images/edits", `{"model":"grok-imagine-edit","prompt":"edit"}`},
		{http.MethodPost, "/videos/generations", `{"model":"grok-imagine-video","prompt":"video"}`},
		{http.MethodGet, "/videos/video_123", ``},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			require.NotEqual(t, http.StatusNotFound, w.Code, "Grok media route should reach the Grok media handler instead of the platform unsupported gate")
		})
	}
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
	apiKeyByValue   map[string]*service.APIKey
	keysByUser      map[int64][]service.APIKey
	availableByUser map[int64][]service.Group
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
	return append([]service.Group(nil), s.availableByUser[userID]...), nil
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
	restrictedClientProduct := service.AugmentClientProductZhumeng
	gatewayKey.RestrictedClientProduct = &restrictedClientProduct
	entitledGroup := service.Group{
		ID:                     901,
		Name:                   "Augment Entitled",
		Platform:               service.PlatformOpenAI,
		Status:                 service.StatusActive,
		Hydrated:               true,
		AugmentGatewayEntitled: true,
		DefaultMappedModel:     "gpt-5.4",
	}
	gatewayKey.GroupID = &entitledGroup.ID
	gatewayKey.Group = &entitledGroup
	pluginService := service.NewAugmentPluginService(
		&config.Config{},
		gatewayRoutesAugmentAuthStub{userID: user.ID},
		gatewayRoutesAugmentUserStub{user: user},
		gatewayRoutesAugmentAPIKeyStub{
			keysByUser: map[int64][]service.APIKey{user.ID: []service.APIKey{gatewayKey}},
			availableByUser: map[int64][]service.Group{
				user.ID: []service.Group{entitledGroup},
			},
		},
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

func TestGatewayRoutesManagedHeadersDoNotBypassOrdinaryGatewayAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	rawAuthCalls := 0
	observedAuthorization := ""
	observedManagedSession := ""

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
			rawAuthCalls++
			observedAuthorization = c.GetHeader("Authorization")
			observedManagedSession = c.GetHeader("X-Zhumeng-Managed-Session")
			if observedAuthorization != "Bearer normal-key" {
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

	for _, path := range []string{"/responses", "/openai/v1/responses", "/v1/responses"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-5.5","input":"hello"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer normal-key")
			req.Header.Set("X-Zhumeng-Device-ID", "9")
			req.Header.Set("X-Zhumeng-Managed-Session", "sess-1")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			require.Equal(t, http.StatusNoContent, w.Code)
			require.Equal(t, "Bearer normal-key", observedAuthorization)
			require.Equal(t, "sess-1", observedManagedSession)
		})
	}
	require.Equal(t, 3, rawAuthCalls)
}

func TestGatewayRoutesClaudeCodeNativeMarkersUseManagedAuthOnlyForMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	rawAuthCalls := 0
	nativeAuthCalls := 0
	observedNativeAuthorization := ""
	observedNativeSession := ""

	RegisterGatewayRoutesWithClaudeCodeNativeAuth(
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
			rawAuthCalls++
			c.AbortWithStatus(http.StatusUnauthorized)
		}),
		servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
			nativeAuthCalls++
			observedNativeAuthorization = c.GetHeader("Authorization")
			observedNativeSession = c.GetHeader("X-Zhumeng-Managed-Session")
			c.AbortWithStatus(http.StatusNoContent)
		}),
		nil,
		nil,
		nil,
		nil,
		&config.Config{},
	)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages?beta=true", strings.NewReader(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}],"max_tokens":8}`))
	req.Header.Set("Content-Type", "application/json")
	managedJWT := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJtYW5hZ2VkLXNlc3Npb24ifQ.signature"
	req.Header.Set("Authorization", "Bearer "+managedJWT)
	req.Header.Set("X-Zhumeng-Device-ID", "9")
	req.Header.Set("X-Zhumeng-Managed-Session", "managed-session")
	req.Header.Set("x-sub2api-client-type", service.ClaudeCodeNativeClientType)
	req.Header.Set("x-sub2api-guard-attested", "true")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
	require.Equal(t, 0, rawAuthCalls)
	require.Equal(t, 1, nativeAuthCalls)
	require.Equal(t, "Bearer "+managedJWT, observedNativeAuthorization)
	require.Equal(t, "managed-session", observedNativeSession)
}

func TestGatewayRoutesManagedHeadersStillUseRawAuthWithoutNativeMarkers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	rawAuthCalls := 0
	nativeAuthCalls := 0
	observedAuthorization := ""

	RegisterGatewayRoutesWithClaudeCodeNativeAuth(
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
			rawAuthCalls++
			observedAuthorization = c.GetHeader("Authorization")
			c.AbortWithStatus(http.StatusNoContent)
		}),
		servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
			nativeAuthCalls++
			c.AbortWithStatus(http.StatusUnauthorized)
		}),
		nil,
		nil,
		nil,
		nil,
		&config.Config{},
	)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}],"max_tokens":8}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer normal-key")
	req.Header.Set("X-Zhumeng-Device-ID", "9")
	req.Header.Set("X-Zhumeng-Managed-Session", "managed-session")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
	require.Equal(t, 1, rawAuthCalls)
	require.Equal(t, 0, nativeAuthCalls)
	require.Equal(t, "Bearer normal-key", observedAuthorization)
}

func TestGatewayRoutesClaudeCodeNativeMarkersRequireManagedHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	rawAuthCalls := 0
	nativeAuthCalls := 0

	RegisterGatewayRoutesWithClaudeCodeNativeAuth(
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
			rawAuthCalls++
			c.AbortWithStatus(http.StatusNoContent)
		}),
		servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
			nativeAuthCalls++
			c.AbortWithStatus(http.StatusNoContent)
		}),
		nil,
		nil,
		nil,
		nil,
		&config.Config{},
	)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}],"max_tokens":8}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer normal-key")
	req.Header.Set("x-sub2api-client-type", service.ClaudeCodeNativeClientType)
	req.Header.Set("x-sub2api-guard-attested", "true")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Equal(t, 0, rawAuthCalls)
	require.Equal(t, 0, nativeAuthCalls)
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
	restrictedClientProduct := service.AugmentClientProductZhumeng
	apiKey.RestrictedClientProduct = &restrictedClientProduct
	entitledGroup := service.Group{
		ID:                     902,
		Name:                   "Augment Entitled",
		Platform:               service.PlatformOpenAI,
		Status:                 service.StatusActive,
		Hydrated:               true,
		AugmentGatewayEntitled: true,
		DefaultMappedModel:     "gpt-5.4",
	}
	apiKey.GroupID = &entitledGroup.ID
	apiKey.Group = &entitledGroup
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

func TestGatewayRoutesCodexDirectRequiresCodexScopedKey(t *testing.T) {
	t.Parallel()

	router := newGatewayRoutesCodexScopeTestRouter(&service.APIKey{
		ID:      101,
		UserID:  202,
		Key:     "sk-generic",
		Status:  service.StatusActive,
		GroupID: testInt64Ptr(1),
		Group: &service.Group{
			ID:                   1,
			Platform:             service.PlatformOpenAI,
			Status:               service.StatusActive,
			Hydrated:             true,
			CodexGatewayEntitled: true,
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/backend-api/codex/responses/compact", strings.NewReader(`{"model":"gpt-5","input":"title"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), `"type":"invalid_request_error"`)
	require.Contains(t, w.Body.String(), "Codex-only API key")
}

func TestGatewayRoutesCodexDirectAllowsEntitledCodexScopedKey(t *testing.T) {
	t.Parallel()

	product := service.CodexUsageClientProduct
	router := newGatewayRoutesCodexScopeTestRouter(&service.APIKey{
		ID:                      303,
		UserID:                  404,
		Key:                     "sk-codex",
		Status:                  service.StatusActive,
		RestrictedClientProduct: &product,
		GroupID:                 testInt64Ptr(2),
		Group: &service.Group{
			ID:                   2,
			Platform:             service.PlatformOpenAI,
			Status:               service.StatusActive,
			Hydrated:             true,
			CodexGatewayEntitled: true,
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/backend-api/codex/responses/compact", strings.NewReader(`{"model":"gpt-5","input":"title"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Contains(t, w.Body.String(), "OpenAI gateway core disabled")
}

func newGatewayRoutesCodexScopeTestRouter(apiKey *service.APIKey) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	cfg := &config.Config{}
	openAIGatewayHandler := handler.NewOpenAIGatewayHandler(nil, service.NewOpenAIGatewayCoreService(nil, cfg, nil))
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
			OpenAIGateway: openAIGatewayHandler,
		},
		servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
			c.Set(string(servermiddleware.ContextKeyAPIKey), apiKey)
			c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{
				UserID:      apiKey.UserID,
				Concurrency: 1,
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

func testInt64Ptr(v int64) *int64 {
	return &v
}
