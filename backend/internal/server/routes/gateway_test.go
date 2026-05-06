package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newGatewayRoutesTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

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
	keysByUser map[int64][]service.APIKey
}

func (s gatewayRoutesAugmentAPIKeyStub) GetByKey(ctx context.Context, key string) (*service.APIKey, error) {
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
