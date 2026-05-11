package routes

import (
	"context"
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

type gatewayAugmentAuthStub struct{}

func (gatewayAugmentAuthStub) GenerateTokenPair(ctx context.Context, user *service.User, familyID string) (*service.TokenPair, error) {
	return &service.TokenPair{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		ExpiresIn:    3600,
	}, nil
}

func (gatewayAugmentAuthStub) RefreshTokenPair(ctx context.Context, refreshToken string) (*service.TokenPairWithUser, error) {
	return nil, service.ErrRefreshTokenInvalid
}

func (gatewayAugmentAuthStub) ValidateToken(token string) (*service.JWTClaims, error) {
	return nil, service.ErrInvalidToken
}

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
				service.NewAugmentPluginService(
					&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
					gatewayAugmentAuthStub{},
					nil,
					nil,
					nil,
					nil,
				),
			),
			Gateway:       &handler.GatewayHandler{},
			OpenAIGateway: &handler.OpenAIGatewayHandler{},
		},
		servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
			c.Next()
		}),
		nil,
		nil,
		nil,
		nil,
		nil,
		&config.Config{},
	)

	return router
}

type gatewaySettingRepoStub struct {
	values map[string]string
}

func (s *gatewaySettingRepoStub) Get(ctx context.Context, key string) (*service.Setting, error) {
	return &service.Setting{Key: key, Value: s.values[key]}, nil
}
func (s *gatewaySettingRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	return s.values[key], nil
}
func (s *gatewaySettingRepoStub) Set(ctx context.Context, key, value string) error { return nil }
func (s *gatewaySettingRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		out[key] = s.values[key]
	}
	return out, nil
}
func (s *gatewaySettingRepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	return nil
}
func (s *gatewaySettingRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	return s.values, nil
}
func (s *gatewaySettingRepoStub) Delete(ctx context.Context, key string) error { return nil }

func newGatewayRoutesTestRouterWithAuth(rawAuth gin.HandlerFunc, validator servermiddleware.ManagedDeviceAccessValidator) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	settingService := service.NewSettingService(&gatewaySettingRepoStub{
		values: map[string]string{
			service.SettingKeyAllowUngroupedKeyScheduling: "false",
		},
	}, &config.Config{})

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
				service.NewAugmentPluginService(
					&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
					gatewayAugmentAuthStub{},
					nil,
					nil,
					nil,
					nil,
				),
			),
			Gateway:       &handler.GatewayHandler{},
			OpenAIGateway: &handler.OpenAIGatewayHandler{},
		},
		servermiddleware.APIKeyAuthMiddleware(rawAuth),
		nil,
		nil,
		nil,
		settingService,
		validator,
		&config.Config{},
	)

	return router
}

func TestGatewayRoutesOpenAIResponsesCompactPathIsRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter()

	for _, path := range []string{"/v1/responses/compact", "/responses/compact"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-5"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.NotEqual(t, http.StatusNotFound, w.Code, "path=%s should hit OpenAI responses handler", path)
	}
}

func TestGatewayRoutesOpenAIPrefixedRoutesAreRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter()

	tests := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/openai/_health"},
		{method: http.MethodGet, path: "/openai/_verify?account_id=1&transport=http"},
		{method: http.MethodGet, path: "/openai/v1/responses"},
		{method: http.MethodPost, path: "/openai/v1/responses", body: `{"model":"gpt-5.4"}`},
		{method: http.MethodPost, path: "/openai/v1/responses/compact", body: `{"model":"gpt-5.4"}`},
		{method: http.MethodPost, path: "/openai/v1/chat/completions", body: `{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}]}`},
		{method: http.MethodPost, path: "/openai/v1/images/generations", body: `{"model":"gpt-image-2","prompt":"apple"}`},
	}

	for _, tc := range tests {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)
			require.NotEqual(t, http.StatusNotFound, w.Code, "path=%s should be registered", tc.path)
		})
	}
}

func TestGatewayRoutesOpenAIImageGenerationRoutesAreRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter()

	for _, path := range []string{
		"/v1/images/generations",
		"/images/generations",
		"/openai/v1/images/generations",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-image-2","prompt":"apple"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.NotEqual(t, http.StatusNotFound, w.Code, "path=%s should be registered", path)
	}
}

func TestGatewayRoutesLegacyWukongUsageRoutesAreRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter()

	tests := []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/usage/api/balance"},
		{method: http.MethodGet, path: "/usage/api/get-models"},
		{method: http.MethodGet, path: "/usage/api/getLoginToken"},
		{method: http.MethodPost, path: "/get-models"},
		{method: http.MethodPost, path: "/batch-upload"},
		{method: http.MethodPost, path: "/checkpoint-blobs"},
		{method: http.MethodPost, path: "/find-missing"},
		{method: http.MethodPost, path: "/save-chat"},
		{method: http.MethodPost, path: "/chat"},
		{method: http.MethodPost, path: "/chat-stream"},
		{method: http.MethodPost, path: "/prompt-enhancer"},
		{method: http.MethodPost, path: "/instruction-stream"},
		{method: http.MethodPost, path: "/smart-paste-stream"},
		{method: http.MethodPost, path: "/generate-commit-message-stream"},
		{method: http.MethodPost, path: "/next_edit_loc"},
		{method: http.MethodPost, path: "/next-edit-stream"},
		{method: http.MethodPost, path: "/remote-agents/list"},
		{method: http.MethodPost, path: "/agents/codebase-retrieval"},
		{method: http.MethodPost, path: "/agents/list-remote-tools"},
		{method: http.MethodPost, path: "/get-implicit-external-sources"},
		{method: http.MethodPost, path: "/search-external-sources"},
		{method: http.MethodPost, path: "/context-canvas/list"},
		{method: http.MethodGet, path: "/notifications/read"},
		{method: http.MethodPost, path: "/notifications/read"},
		{method: http.MethodPost, path: "/notifications/mark-as-read"},
		{method: http.MethodGet, path: "/subscription-banner"},
		{method: http.MethodPost, path: "/subscription-banner"},
		{method: http.MethodPost, path: "/report-error"},
		{method: http.MethodPost, path: "/report-feature-vector"},
		{method: http.MethodPost, path: "/client-metrics"},
		{method: http.MethodPost, path: "/record-session-events"},
		{method: http.MethodPost, path: "/record-request-events"},
		{method: http.MethodPost, path: "/record-user-events"},
		{method: http.MethodPost, path: "/record-preference-sample"},
		{method: http.MethodPost, path: "/client-completion-timelines"},
		{method: http.MethodPost, path: "/chat-feedback"},
		{method: http.MethodPost, path: "/completion-feedback"},
		{method: http.MethodPost, path: "/next-edit-feedback"},
		{method: http.MethodPost, path: "/resolve-completions"},
		{method: http.MethodPost, path: "/resolve-chat-input-completion"},
		{method: http.MethodPost, path: "/resolve-edit"},
		{method: http.MethodPost, path: "/resolve-instruction"},
		{method: http.MethodPost, path: "/resolve-next-edit"},
		{method: http.MethodPost, path: "/resolve-smart-paste"},
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

func TestGatewayRoutesResponsesRawAuthStillRuns(t *testing.T) {
	rawCalls := 0
	managedCalls := 0
	router := newGatewayRoutesTestRouterWithAuth(func(c *gin.Context) {
		rawCalls++
		c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{
			ID:     42,
			UserID: 7,
			Status: service.StatusActive,
			User:   &service.User{ID: 7, Status: service.StatusActive, Balance: 10, Role: service.RoleUser, Concurrency: 3},
			GroupID: func() *int64 {
				id := int64(9)
				return &id
			}(),
			Group: &service.Group{ID: 9, Platform: service.PlatformOpenAI, Hydrated: true},
		})
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 7, Concurrency: 3})
		c.Set(string(servermiddleware.ContextKeyUserRole), service.RoleUser)
		c.Next()
	}, servermiddleware.ManagedDeviceAccessValidatorFunc(func(ctx context.Context, req service.ValidateManagedDeviceAccessRequest) (*service.ManagedDeviceAccessContext, error) {
		managedCalls++
		groupID := int64(9)
		return &service.ManagedDeviceAccessContext{
			APIKey: &service.APIKey{
				ID:      42,
				UserID:  7,
				Status:  service.StatusActive,
				GroupID: &groupID,
				Group:   &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Hydrated: true},
				User:    &service.User{ID: 7, Status: service.StatusActive, Balance: 10, Role: service.RoleUser, Concurrency: 3},
			},
			User:             &service.User{ID: 7, Status: service.StatusActive, Balance: 10, Role: service.RoleUser, Concurrency: 3},
			ManagedSessionID: "sess-1",
		}, nil
	}))

	for _, path := range []string{"/v1/responses", "/responses", "/openai/v1/responses"} {
		rawCalls = 0
		managedCalls = 0
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-5.4","input":"hello"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, 1, rawCalls, path)
		require.Equal(t, 0, managedCalls, path)
		require.NotEqual(t, http.StatusUnauthorized, w.Code, path)
	}

	rawCalls = 0
	managedCalls = 0
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer managed-token")
	req.Header.Set("X-Zhumeng-Device-ID", "9")
	req.Header.Set("X-Zhumeng-Managed-Session", "sess-1")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, 0, rawCalls)
	require.Equal(t, 1, managedCalls)
	require.NotEqual(t, http.StatusUnauthorized, w.Code)
}

func TestGatewayRoutesUngroupedRawKeyStillRejected(t *testing.T) {
	router := newGatewayRoutesTestRouterWithAuth(func(c *gin.Context) {
		c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{
			ID:     42,
			UserID: 7,
			Status: service.StatusActive,
			User:   &service.User{ID: 7, Status: service.StatusActive, Balance: 10, Role: service.RoleUser, Concurrency: 3},
		})
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 7, Concurrency: 3})
		c.Set(string(servermiddleware.ContextKeyUserRole), service.RoleUser)
		c.Next()
	}, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-3-5-sonnet","messages":[]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), `"type":"error"`)
}

func TestGatewayRoutesUngroupedOpenAIPathsUseOpenAIErrorEnvelope(t *testing.T) {
	router := newGatewayRoutesTestRouterWithAuth(func(c *gin.Context) {
		c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{
			ID:     42,
			UserID: 7,
			Status: service.StatusActive,
			User:   &service.User{ID: 7, Status: service.StatusActive, Balance: 10, Role: service.RoleUser, Concurrency: 3},
		})
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 7, Concurrency: 3})
		c.Set(string(servermiddleware.ContextKeyUserRole), service.RoleUser)
		c.Next()
	}, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","input":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), `"error":`)
	require.NotContains(t, w.Body.String(), `"type":"error"`)
}

func TestGatewayRoutesManagedHeadersDoNotHitRawLookupFirst(t *testing.T) {
	rawCalls := 0
	managedCalls := 0
	router := newGatewayRoutesTestRouterWithAuth(func(c *gin.Context) {
		rawCalls++
		c.Next()
	}, servermiddleware.ManagedDeviceAccessValidatorFunc(func(ctx context.Context, req service.ValidateManagedDeviceAccessRequest) (*service.ManagedDeviceAccessContext, error) {
		managedCalls++
		groupID := int64(9)
		return &service.ManagedDeviceAccessContext{
			APIKey: &service.APIKey{
				ID:      42,
				UserID:  7,
				Status:  service.StatusActive,
				GroupID: &groupID,
				Group:   &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Hydrated: true},
				User:    &service.User{ID: 7, Status: service.StatusActive, Balance: 10, Role: service.RoleUser, Concurrency: 3},
			},
			User:             &service.User{ID: 7, Status: service.StatusActive, Balance: 10, Role: service.RoleUser, Concurrency: 3},
			ManagedSessionID: "sess-1",
		}, nil
	}))

	req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(`{"model":"gpt-5.4","input":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer managed-token")
	req.Header.Set("X-Zhumeng-Device-ID", "9")
	req.Header.Set("X-Zhumeng-Managed-Session", "sess-1")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, 0, rawCalls)
	require.Equal(t, 1, managedCalls)
	require.NotEqual(t, http.StatusUnauthorized, w.Code)
	require.NotEqual(t, http.StatusNotFound, w.Code)
}
