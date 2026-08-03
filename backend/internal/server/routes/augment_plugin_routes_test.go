package routes

import (
	"context"
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
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newAugmentPluginRoutesTestRouter(t *testing.T) *gin.Engine {
	t.Helper()

	rdb := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  50 * time.Millisecond,
		ReadTimeout:  50 * time.Millisecond,
		WriteTimeout: 50 * time.Millisecond,
	})
	t.Cleanup(func() {
		_ = rdb.Close()
	})

	return newAugmentPluginRoutesTestRouterWithRedis(rdb)
}

func newAugmentPluginRoutesTestRouterWithRedis(redisClient *redis.Client) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")

	authHandler := handler.NewAuthHandler(
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		service.NewAugmentPluginService(nil, nil, nil, nil, nil, nil),
	)

	RegisterAuthRoutes(
		v1,
		&handler.Handlers{
			Auth:    authHandler,
			Setting: &handler.SettingHandler{},
		},
		servermiddleware.JWTAuthMiddleware(func(c *gin.Context) {
			c.AbortWithStatus(http.StatusUnauthorized)
		}),
		redisClient,
		nil,
	)

	return router
}

type augmentPluginRoutesSettingRepoStub struct {
	values map[string]string
}

func (s *augmentPluginRoutesSettingRepoStub) Get(ctx context.Context, key string) (*service.Setting, error) {
	value, ok := s.values[key]
	if !ok {
		return nil, service.ErrSettingNotFound
	}
	return &service.Setting{Key: key, Value: value}, nil
}

func (s *augmentPluginRoutesSettingRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	value, ok := s.values[key]
	if !ok {
		return "", service.ErrSettingNotFound
	}
	return value, nil
}

func (s *augmentPluginRoutesSettingRepoStub) Set(ctx context.Context, key, value string) error {
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[key] = value
	return nil
}

func (s *augmentPluginRoutesSettingRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	out := map[string]string{}
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func (s *augmentPluginRoutesSettingRepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	if s.values == nil {
		s.values = map[string]string{}
	}
	for key, value := range settings {
		s.values[key] = value
	}
	return nil
}

func (s *augmentPluginRoutesSettingRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	out := map[string]string{}
	for key, value := range s.values {
		out[key] = value
	}
	return out, nil
}

func (s *augmentPluginRoutesSettingRepoStub) Delete(ctx context.Context, key string) error {
	delete(s.values, key)
	return nil
}

func TestAugmentPluginRoutesAreRegistered(t *testing.T) {
	t.Parallel()

	router := newAugmentPluginRoutesTestRouter(t)

	testCases := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{
			name:   "grant requires jwt auth",
			method: http.MethodPost,
			path:   "/api/v1/plugin/augment/quick-login/grant",
			body:   `{}`,
			want:   http.StatusUnauthorized,
		},
		{
			name:   "official session bind intent requires jwt auth",
			method: http.MethodPost,
			path:   "/api/v1/plugin/augment/official-session/bind-intents",
			body:   `{}`,
			want:   http.StatusUnauthorized,
		},
		{
			name:   "official session bind requires jwt auth",
			method: http.MethodPost,
			path:   "/api/v1/plugin/augment/official-session/bind",
			body:   `{}`,
			want:   http.StatusUnauthorized,
		},
		{
			name:   "official session status requires jwt auth",
			method: http.MethodGet,
			path:   "/api/v1/plugin/augment/official-session",
			want:   http.StatusUnauthorized,
		},
		{
			name:   "official session revoke requires jwt auth",
			method: http.MethodPost,
			path:   "/api/v1/plugin/augment/official-session/revoke",
			body:   `{}`,
			want:   http.StatusUnauthorized,
		},
		{
			name:   "augment billing summary requires jwt auth",
			method: http.MethodGet,
			path:   "/api/v1/plugin/augment/billing/summary",
			want:   http.StatusUnauthorized,
		},
		{
			name:   "augment billing usage requires jwt auth",
			method: http.MethodGet,
			path:   "/api/v1/plugin/augment/billing/usage",
			want:   http.StatusUnauthorized,
		},
		{
			name:   "augment billing recent errors requires jwt auth",
			method: http.MethodGet,
			path:   "/api/v1/plugin/augment/billing/recent-errors",
			want:   http.StatusUnauthorized,
		},
		{
			name:   "exchange route exists",
			method: http.MethodPost,
			path:   "/api/v1/plugin/augment/callback/exchange",
			body:   `{}`,
			want:   http.StatusBadRequest,
		},
		{
			name:   "refresh route exists",
			method: http.MethodPost,
			path:   "/api/v1/plugin/augment/session/refresh",
			body:   `{}`,
			want:   http.StatusTooManyRequests,
		},
		{
			name:   "api key verify route exists",
			method: http.MethodPost,
			path:   "/api/v1/plugin/augment/api-key/verify",
			body:   `{}`,
			want:   http.StatusBadRequest,
		},
		{
			name:   "summary route exists and requires bearer auth",
			method: http.MethodGet,
			path:   "/api/v1/plugin/augment/summary",
			want:   http.StatusUnauthorized,
		},
		{
			name:   "compat route exists and requires bearer auth",
			method: http.MethodGet,
			path:   "/api/v1/plugin/augment/compat/metadata",
			want:   http.StatusUnauthorized,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			require.Equal(t, tc.want, w.Code)
		})
	}
}

func TestAugmentPluginSessionRefreshRouteFailsClosedWhenRedisUnavailable(t *testing.T) {
	t.Parallel()

	rdb := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  50 * time.Millisecond,
		ReadTimeout:  50 * time.Millisecond,
		WriteTimeout: 50 * time.Millisecond,
	})
	t.Cleanup(func() {
		_ = rdb.Close()
	})

	router := newAugmentPluginRoutesTestRouterWithRedis(rdb)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugin/augment/session/refresh", strings.NewReader(`{"refresh_token":"refresh-token"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "198.51.100.42:34567"

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.Contains(t, w.Body.String(), "rate limit exceeded")
}

func TestAugmentQuickLoginGrantRouteAllowsAuthenticatedNonAdminInBackendMode(t *testing.T) {
	t.Parallel()

	rdb := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  50 * time.Millisecond,
		ReadTimeout:  50 * time.Millisecond,
		WriteTimeout: 50 * time.Millisecond,
	})
	t.Cleanup(func() {
		_ = rdb.Close()
	})

	router := gin.New()
	v1 := router.Group("/api/v1")
	authHandler := handler.NewAuthHandler(
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		service.NewAugmentPluginService(nil, nil, nil, nil, nil, nil),
	)
	settingService := service.NewSettingService(
		&augmentPluginRoutesSettingRepoStub{
			values: map[string]string{
				service.SettingKeyBackendModeEnabled: "true",
			},
		},
		&config.Config{},
	)

	RegisterAuthRoutes(
		v1,
		&handler.Handlers{
			Auth:    authHandler,
			Setting: &handler.SettingHandler{},
		},
		servermiddleware.JWTAuthMiddleware(func(c *gin.Context) {
			c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 7})
			c.Set(string(servermiddleware.ContextKeyUserRole), "user")
			c.Next()
		}),
		rdb,
		settingService,
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugin/augment/quick-login/grant", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.NotEqual(t, http.StatusForbidden, w.Code)
	require.NotEqual(t, http.StatusUnauthorized, w.Code)
}
