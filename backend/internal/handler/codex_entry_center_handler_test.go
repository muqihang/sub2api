package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type codexEntryCenterServiceStub struct {
	getSummary             func(ctx context.Context, userID int64) (*service.CodexEntrySummary, error)
	createSetupSession     func(ctx context.Context, req service.CodexCreateSetupSessionRequest) (*service.CodexCreateSetupSessionResponse, error)
	regenerateSetupSession func(ctx context.Context, userID int64, sessionID string) (*service.CodexRegenerateSetupSessionResponse, error)
	diagnose               func(ctx context.Context, req service.CodexDiagnoseRequest) (*service.CodexDiagnoseReport, error)
	resyncDevice           func(ctx context.Context, userID int64, deviceID int64) (*service.CodexDeviceActionResponse, error)
	repairDevice           func(ctx context.Context, userID int64, deviceID int64) (*service.CodexDeviceActionResponse, error)
	reattachDevice         func(ctx context.Context, userID int64, deviceID int64) (*service.CodexDeviceActionResponse, error)
	revokeAttachment       func(ctx context.Context, userID int64, deviceID int64) (*service.CodexDeviceActionResponse, error)
	removeDevice           func(ctx context.Context, userID int64, deviceID int64) (*service.CodexDeviceActionResponse, error)
}

func (s *codexEntryCenterServiceStub) GetSummary(ctx context.Context, userID int64) (*service.CodexEntrySummary, error) {
	if s.getSummary != nil {
		return s.getSummary(ctx, userID)
	}
	return &service.CodexEntrySummary{
		PageState: service.CodexPageStateOnboardingCredential,
		Devices:   []service.CodexDeviceDTO{},
	}, nil
}

func (s *codexEntryCenterServiceStub) CreateSetupSession(ctx context.Context, req service.CodexCreateSetupSessionRequest) (*service.CodexCreateSetupSessionResponse, error) {
	if s.createSetupSession != nil {
		return s.createSetupSession(ctx, req)
	}
	return &service.CodexCreateSetupSessionResponse{
		PageState:                service.CodexPageStateOnboardingAttach,
		SetupSessionPresentation: service.CodexSetupSessionPresentationWizard,
	}, nil
}

func (s *codexEntryCenterServiceStub) RegenerateSetupSession(ctx context.Context, userID int64, sessionID string) (*service.CodexRegenerateSetupSessionResponse, error) {
	if s.regenerateSetupSession != nil {
		return s.regenerateSetupSession(ctx, userID, sessionID)
	}
	return &service.CodexRegenerateSetupSessionResponse{
		SetupSession: service.CodexSetupSessionRegenerateDTO{
			ID:        "new-session",
			ExpiresAt: time.Now().Add(10 * time.Minute),
		},
	}, nil
}

func (s *codexEntryCenterServiceStub) Diagnose(ctx context.Context, req service.CodexDiagnoseRequest) (*service.CodexDiagnoseReport, error) {
	if s.diagnose != nil {
		return s.diagnose(ctx, req)
	}
	return &service.CodexDiagnoseReport{OK: true, TargetKind: "device", Checks: []service.CodexDiagnoseCheck{}}, nil
}

func (s *codexEntryCenterServiceStub) ResyncDevice(ctx context.Context, userID int64, deviceID int64) (*service.CodexDeviceActionResponse, error) {
	if s.resyncDevice != nil {
		return s.resyncDevice(ctx, userID, deviceID)
	}
	return &service.CodexDeviceActionResponse{DeviceID: deviceID, Accepted: true}, nil
}

func (s *codexEntryCenterServiceStub) RepairDevice(ctx context.Context, userID int64, deviceID int64) (*service.CodexDeviceActionResponse, error) {
	if s.repairDevice != nil {
		return s.repairDevice(ctx, userID, deviceID)
	}
	return &service.CodexDeviceActionResponse{DeviceID: deviceID, Accepted: true}, nil
}

func (s *codexEntryCenterServiceStub) ReattachDevice(ctx context.Context, userID int64, deviceID int64) (*service.CodexDeviceActionResponse, error) {
	if s.reattachDevice != nil {
		return s.reattachDevice(ctx, userID, deviceID)
	}
	return &service.CodexDeviceActionResponse{DeviceID: deviceID, Accepted: true}, nil
}

func (s *codexEntryCenterServiceStub) RevokeAttachment(ctx context.Context, userID int64, deviceID int64) (*service.CodexDeviceActionResponse, error) {
	if s.revokeAttachment != nil {
		return s.revokeAttachment(ctx, userID, deviceID)
	}
	return &service.CodexDeviceActionResponse{DeviceID: deviceID, Accepted: true}, nil
}

func (s *codexEntryCenterServiceStub) RemoveDevice(ctx context.Context, userID int64, deviceID int64) (*service.CodexDeviceActionResponse, error) {
	if s.removeDevice != nil {
		return s.removeDevice(ctx, userID, deviceID)
	}
	return &service.CodexDeviceActionResponse{DeviceID: deviceID, Accepted: true}, nil
}

func TestCodexEntryCenterHandlerGetSummary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/codex/summary", nil)
	c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 7})

	var gotUserID int64
	h := &CodexEntryCenterHandler{
		service: &codexEntryCenterServiceStub{
			getSummary: func(ctx context.Context, userID int64) (*service.CodexEntrySummary, error) {
				gotUserID = userID
				step := 1
				return &service.CodexEntrySummary{
					PageState:  service.CodexPageStateOnboardingCredential,
					WizardStep: &step,
					Devices:    []service.CodexDeviceDTO{},
				}, nil
			},
		},
	}

	h.GetSummary(c)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(7), gotUserID)
}

func TestCodexEntryCenterHandlerCreateSetupSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/codex/setup-sessions",
		strings.NewReader(`{"attachment_mode":"reused_key","reuse_api_key_id":42}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Origin", "https://sub2api.example.com")
	c.Request.Host = "sub2api.example.com"
	c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 7})

	var gotReq service.CodexCreateSetupSessionRequest
	h := &CodexEntryCenterHandler{
		service: &codexEntryCenterServiceStub{
			createSetupSession: func(ctx context.Context, req service.CodexCreateSetupSessionRequest) (*service.CodexCreateSetupSessionResponse, error) {
				gotReq = req
				return &service.CodexCreateSetupSessionResponse{
					PageState:                service.CodexPageStateOnboardingAttach,
					SetupSessionPresentation: service.CodexSetupSessionPresentationWizard,
				}, nil
			},
		},
	}

	h.CreateSetupSession(c)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(7), gotReq.UserID)
	require.Equal(t, service.CodexAttachmentModeReusedKey, gotReq.AttachmentMode)
	require.NotNil(t, gotReq.ReuseAPIKeyID)
	require.Equal(t, int64(42), *gotReq.ReuseAPIKeyID)
}

func TestCodexEntryCenterHandlerDiagnose(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/codex/diagnose",
		strings.NewReader(`{"device_id":1}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 7})

	var gotReq service.CodexDiagnoseRequest
	h := &CodexEntryCenterHandler{
		service: &codexEntryCenterServiceStub{
			diagnose: func(ctx context.Context, req service.CodexDiagnoseRequest) (*service.CodexDiagnoseReport, error) {
				gotReq = req
				return &service.CodexDiagnoseReport{OK: true, TargetKind: "device", Checks: []service.CodexDiagnoseCheck{}}, nil
			},
		},
	}

	h.Diagnose(c)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(7), gotReq.UserID)
	require.NotNil(t, gotReq.DeviceID)
	require.Equal(t, int64(1), *gotReq.DeviceID)
	require.Nil(t, gotReq.SetupSessionID)
}

func TestCodexEntryCenterHandlerDeviceActions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name    string
		method  string
		path    string
		handler func(h *CodexEntryCenterHandler, c *gin.Context)
	}{
		{"ResyncDevice", http.MethodPost, "/api/v1/codex/devices/1/resync", func(h *CodexEntryCenterHandler, c *gin.Context) { h.ResyncDevice(c) }},
		{"RepairDevice", http.MethodPost, "/api/v1/codex/devices/1/repair", func(h *CodexEntryCenterHandler, c *gin.Context) { h.RepairDevice(c) }},
		{"ReattachDevice", http.MethodPost, "/api/v1/codex/devices/1/reattach", func(h *CodexEntryCenterHandler, c *gin.Context) { h.ReattachDevice(c) }},
		{"RevokeAttachment", http.MethodPost, "/api/v1/codex/devices/1/revoke-attachment", func(h *CodexEntryCenterHandler, c *gin.Context) { h.RevokeAttachment(c) }},
		{"RemoveDevice", http.MethodDelete, "/api/v1/codex/devices/1", func(h *CodexEntryCenterHandler, c *gin.Context) { h.RemoveDevice(c) }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(tc.method, tc.path, nil)
			c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 7})
			c.Params = gin.Params{{Key: "id", Value: "1"}}

			h := &CodexEntryCenterHandler{service: &codexEntryCenterServiceStub{}}
			tc.handler(h, c)
			require.Equal(t, http.StatusAccepted, rec.Code)
		})
	}
}
