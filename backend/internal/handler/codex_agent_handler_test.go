package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCodexAgentHandlerCreateSetupGrant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/codex/setup-grants", strings.NewReader(`{"api_key_id":42,"client":"codex","mode":"managed_proxy"}`))
	c.Request.Host = "127.0.0.1:18081"
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Origin", "https://sub2api.example.com")
	c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 7})

	var got service.CreateCodexSetupGrantRequest
	h := &CodexAgentHandler{
		service: &codexAgentServiceStub{
			createSetupGrant: func(ctx context.Context, req service.CreateCodexSetupGrantRequest) (*service.CreateCodexSetupGrantResponse, error) {
				got = req
				return &service.CreateCodexSetupGrantResponse{
					Code:      "grant-1",
					ExpiresAt: time.Now().Add(time.Minute),
					DeepLink:  "zhumeng-agent://setup?client=codex&code=grant-1",
				}, nil
			},
		},
	}

	h.CreateSetupGrant(c)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(7), got.UserID)
	require.Equal(t, int64(42), got.APIKeyID)
	require.Equal(t, "https://sub2api.example.com", got.ServerOrigin)
	require.Equal(t, "http://127.0.0.1:18081", got.GatewayOrigin)
}

func TestCodexAgentHandlerListDevicesRejectsBadAPIKeyID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/codex/devices?api_key_id=bad", nil)
	c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 7})

	h := &CodexAgentHandler{service: &codexAgentServiceStub{}}
	h.ListDevices(c)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

type codexAgentServiceStub struct {
	createSetupGrant   func(ctx context.Context, req service.CreateCodexSetupGrantRequest) (*service.CreateCodexSetupGrantResponse, error)
	exchangeSetupGrant func(ctx context.Context, req service.ExchangeCodexSetupGrantRequest) (*service.ExchangeCodexSetupGrantResponse, error)
	refreshDeviceToken func(ctx context.Context, req service.RefreshCodexDeviceTokenRequest) (*service.RefreshCodexDeviceTokenResponse, error)
	listDevices        func(ctx context.Context, userID int64, apiKeyID *int64) ([]*dbent.CodexManagedDevice, error)
	revokeDevice       func(ctx context.Context, userID, deviceID int64) error
	validateManagedDeviceAccess func(ctx context.Context, req service.ValidateManagedDeviceAccessRequest) (*service.ManagedDeviceAccessContext, error)
}

func (s *codexAgentServiceStub) CreateSetupGrant(ctx context.Context, req service.CreateCodexSetupGrantRequest) (*service.CreateCodexSetupGrantResponse, error) {
	if s.createSetupGrant != nil {
		return s.createSetupGrant(ctx, req)
	}
	return &service.CreateCodexSetupGrantResponse{}, nil
}

func (s *codexAgentServiceStub) ExchangeSetupGrant(ctx context.Context, req service.ExchangeCodexSetupGrantRequest) (*service.ExchangeCodexSetupGrantResponse, error) {
	if s.exchangeSetupGrant != nil {
		return s.exchangeSetupGrant(ctx, req)
	}
	return &service.ExchangeCodexSetupGrantResponse{}, nil
}

func (s *codexAgentServiceStub) RefreshDeviceToken(ctx context.Context, req service.RefreshCodexDeviceTokenRequest) (*service.RefreshCodexDeviceTokenResponse, error) {
	if s.refreshDeviceToken != nil {
		return s.refreshDeviceToken(ctx, req)
	}
	return &service.RefreshCodexDeviceTokenResponse{}, nil
}

func (s *codexAgentServiceStub) ListDevices(ctx context.Context, userID int64, apiKeyID *int64) ([]*dbent.CodexManagedDevice, error) {
	if s.listDevices != nil {
		return s.listDevices(ctx, userID, apiKeyID)
	}
	return []*dbent.CodexManagedDevice{}, nil
}

func (s *codexAgentServiceStub) RevokeDevice(ctx context.Context, userID, deviceID int64) error {
	if s.revokeDevice != nil {
		return s.revokeDevice(ctx, userID, deviceID)
	}
	return nil
}

func (s *codexAgentServiceStub) ValidateManagedDeviceAccess(ctx context.Context, req service.ValidateManagedDeviceAccessRequest) (*service.ManagedDeviceAccessContext, error) {
	if s.validateManagedDeviceAccess != nil {
		return s.validateManagedDeviceAccess(ctx, req)
	}
	return &service.ManagedDeviceAccessContext{
		User: &service.User{ID: 7, Status: service.StatusActive},
	}, nil
}
