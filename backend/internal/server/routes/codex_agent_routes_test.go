package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCodexAgentRoutesAuthSplit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")

	h := &handler.Handlers{
		CodexAgent: handler.NewCodexAgentHandlerWithServiceForTest(&codexRoutesServiceStub{}),
	}

	jwtAuth := servermiddleware.JWTAuthMiddleware(func(c *gin.Context) {
		if c.GetHeader("X-Test-Auth") != "ok" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 7})
		c.Set(string(servermiddleware.ContextKeyUserRole), service.RoleUser)
		c.Next()
	})

	RegisterCodexAgentRoutes(v1, h, jwtAuth, nil)

	publicCases := []struct {
		path string
		body string
	}{
		{path: "/api/v1/codex/setup-grants/exchange", body: `{"code":"abc","server_origin":"https://sub2api.example.com"}`},
		{path: "/api/v1/codex/devices/refresh", body: `{"device_id":1,"refresh_token":"rt"}`},
		{path: "/api/v1/codex/devices/revoke-managed", body: `{"device_id":1}`},
	}
	for _, tc := range publicCases {
		req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.NotEqual(t, http.StatusUnauthorized, w.Code, tc.path)
		require.NotEqual(t, http.StatusNotFound, w.Code, tc.path)
	}

	authCases := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodPost, path: "/api/v1/codex/setup-grants", body: `{"api_key_id":42}`},
		{method: http.MethodGet, path: "/api/v1/codex/devices"},
		{method: http.MethodPost, path: "/api/v1/codex/devices/revoke", body: `{"device_id":1}`},
	}
	for _, tc := range authCases {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		if tc.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusUnauthorized, w.Code, tc.path)

		req = httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		if tc.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("X-Test-Auth", "ok")
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.NotEqual(t, http.StatusUnauthorized, w.Code, tc.path)
		require.NotEqual(t, http.StatusNotFound, w.Code, tc.path)
	}
}

type codexRoutesServiceStub struct{}

func (s *codexRoutesServiceStub) CreateSetupGrant(ctx context.Context, req service.CreateCodexSetupGrantRequest) (*service.CreateCodexSetupGrantResponse, error) {
	return &service.CreateCodexSetupGrantResponse{Code: "grant", ExpiresAt: time.Now().Add(time.Minute), DeepLink: "zhumeng-agent://setup?code=grant"}, nil
}

func (s *codexRoutesServiceStub) ExchangeSetupGrant(ctx context.Context, req service.ExchangeCodexSetupGrantRequest) (*service.ExchangeCodexSetupGrantResponse, error) {
	return &service.ExchangeCodexSetupGrantResponse{AccessToken: "at", RefreshToken: "rt", ManagedSessionID: "sess", DeviceID: 1, ExpiresAt: time.Now().Add(time.Minute)}, nil
}

func (s *codexRoutesServiceStub) RefreshDeviceToken(ctx context.Context, req service.RefreshCodexDeviceTokenRequest) (*service.RefreshCodexDeviceTokenResponse, error) {
	return &service.RefreshCodexDeviceTokenResponse{AccessToken: "at", RefreshToken: "rt", ManagedSessionID: "sess", ExpiresAt: time.Now().Add(time.Minute)}, nil
}

func (s *codexRoutesServiceStub) ListDevices(ctx context.Context, userID int64, apiKeyID *int64) ([]*dbent.CodexManagedDevice, error) {
	return []*dbent.CodexManagedDevice{}, nil
}

func (s *codexRoutesServiceStub) RevokeDevice(ctx context.Context, userID, deviceID int64) error {
	return nil
}

func (s *codexRoutesServiceStub) ValidateManagedDeviceAccess(ctx context.Context, req service.ValidateManagedDeviceAccessRequest) (*service.ManagedDeviceAccessContext, error) {
	return &service.ManagedDeviceAccessContext{
		User: &service.User{ID: 7, Status: service.StatusActive},
	}, nil
}
