package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestManagedDeviceOrAPIKeyAuthFallsThroughToRawAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	rawCalled := 0
	router.Use(ManagedDeviceOrAPIKeyAuth(
		nil,
		APIKeyAuthMiddleware(func(c *gin.Context) {
			rawCalled++
			c.Next()
		}),
		nil,
		nil,
		&config.Config{},
	))
	router.GET("/t", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, 1, rawCalled)
	require.Equal(t, http.StatusNoContent, w.Code)
}

func TestManagedDeviceOrAPIKeyAuthValidManagedHeadersSetContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	rawCalled := 0
	router.Use(ManagedDeviceOrAPIKeyAuth(
		managedAccessValidatorStub{
			validate: func(ctx context.Context, req service.ValidateManagedDeviceAccessRequest) (*service.ManagedDeviceAccessContext, error) {
				return &service.ManagedDeviceAccessContext{
					APIKey: &service.APIKey{
						ID:     42,
						Status: service.StatusActive,
						User: &service.User{
							ID:          7,
							Status:      service.StatusActive,
							Role:        service.RoleUser,
							Balance:     10,
							Concurrency: 3,
						},
					},
					User:             &service.User{ID: 7, Status: service.StatusActive, Role: service.RoleUser, Balance: 10, Concurrency: 3},
					Device:           &dbent.CodexManagedDevice{ID: 9, APIKeyID: 42, Status: "active"},
					ManagedSessionID: "sess-1",
				}, nil
			},
		},
		APIKeyAuthMiddleware(func(c *gin.Context) {
			rawCalled++
			c.Next()
		}),
		nil,
		nil,
		&config.Config{},
	))
	router.GET("/t", func(c *gin.Context) {
		apiKey, ok := GetAPIKeyFromContext(c)
		require.True(t, ok)
		require.Equal(t, int64(42), apiKey.ID)
		subject, ok := GetAuthSubjectFromContext(c)
		require.True(t, ok)
		require.Equal(t, int64(7), subject.UserID)
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	req.Header.Set("Authorization", "Bearer managed-token")
	req.Header.Set(headerZhumengDeviceID, "9")
	req.Header.Set(headerZhumengManagedSession, "sess-1")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, 0, rawCalled)
	require.Equal(t, http.StatusNoContent, w.Code)
}

func TestManagedDeviceOrAPIKeyAuthInvalidManagedHeadersDoNotFallThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	rawCalled := 0
	router.Use(ManagedDeviceOrAPIKeyAuth(
		managedAccessValidatorStub{
			validate: func(ctx context.Context, req service.ValidateManagedDeviceAccessRequest) (*service.ManagedDeviceAccessContext, error) {
				return nil, service.ErrCodexManagedAccessInvalid
			},
		},
		APIKeyAuthMiddleware(func(c *gin.Context) {
			rawCalled++
			c.Next()
		}),
		nil,
		nil,
		&config.Config{},
	))
	router.GET("/t", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	req.Header.Set("Authorization", "Bearer managed-token")
	req.Header.Set(headerZhumengDeviceID, "9")
	req.Header.Set(headerZhumengManagedSession, "sess-1")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, 0, rawCalled)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

type managedAccessValidatorStub struct {
	validate func(ctx context.Context, req service.ValidateManagedDeviceAccessRequest) (*service.ManagedDeviceAccessContext, error)
}

func (s managedAccessValidatorStub) ValidateManagedDeviceAccess(ctx context.Context, req service.ValidateManagedDeviceAccessRequest) (*service.ManagedDeviceAccessContext, error) {
	return s.validate(ctx, req)
}
