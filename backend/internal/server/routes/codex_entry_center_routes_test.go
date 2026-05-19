package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCodexEntryCenterRoutesAuthSplit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")

	h := &handler.Handlers{
		CodexEntryCenter: handler.NewCodexEntryCenterHandlerWithServiceForTest(&codexEntryCenterRoutesStub{}),
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

	RegisterCodexEntryCenterRoutes(v1, h, jwtAuth, nil)

	// All entry center routes require authentication.
	authCases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/v1/codex/summary", ""},
		{http.MethodPost, "/api/v1/codex/diagnose", `{"device_id":1}`},
		{http.MethodPost, "/api/v1/codex/setup-sessions", `{"attachment_mode":"reused_key","reuse_api_key_id":42}`},
		{http.MethodPost, "/api/v1/codex/setup-sessions/abc/regenerate", ""},
		{http.MethodPost, "/api/v1/codex/devices/1/resync", ""},
		{http.MethodPost, "/api/v1/codex/devices/1/repair", ""},
		{http.MethodPost, "/api/v1/codex/devices/1/reattach", ""},
		{http.MethodPost, "/api/v1/codex/devices/1/revoke-attachment", ""},
		{http.MethodDelete, "/api/v1/codex/devices/1", ""},
	}

	for _, tc := range authCases {
		// Without auth -> 401.
		var bodyReader *strings.Reader
		if tc.body != "" {
			bodyReader = strings.NewReader(tc.body)
		} else {
			bodyReader = strings.NewReader("")
		}
		req := httptest.NewRequest(tc.method, tc.path, bodyReader)
		if tc.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusUnauthorized, w.Code, "%s %s should require auth", tc.method, tc.path)

		// With auth -> not 401 and not 404.
		if tc.body != "" {
			bodyReader = strings.NewReader(tc.body)
		} else {
			bodyReader = strings.NewReader("")
		}
		req = httptest.NewRequest(tc.method, tc.path, bodyReader)
		if tc.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("X-Test-Auth", "ok")
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.NotEqual(t, http.StatusUnauthorized, w.Code, "%s %s with auth", tc.method, tc.path)
		require.NotEqual(t, http.StatusNotFound, w.Code, "%s %s with auth", tc.method, tc.path)
	}
}

type codexEntryCenterRoutesStub struct{}

func (s *codexEntryCenterRoutesStub) GetSummary(ctx context.Context, userID int64) (*service.CodexEntrySummary, error) {
	step := 1
	return &service.CodexEntrySummary{
		PageState:  service.CodexPageStateOnboardingCredential,
		WizardStep: &step,
		Devices:    []service.CodexDeviceDTO{},
	}, nil
}

func (s *codexEntryCenterRoutesStub) CreateSetupSession(ctx context.Context, req service.CodexCreateSetupSessionRequest) (*service.CodexCreateSetupSessionResponse, error) {
	return &service.CodexCreateSetupSessionResponse{
		PageState:                service.CodexPageStateOnboardingAttach,
		SetupSessionPresentation: service.CodexSetupSessionPresentationWizard,
	}, nil
}

func (s *codexEntryCenterRoutesStub) RegenerateSetupSession(ctx context.Context, userID int64, sessionID string) (*service.CodexRegenerateSetupSessionResponse, error) {
	return &service.CodexRegenerateSetupSessionResponse{
		SetupSession: service.CodexSetupSessionRegenerateDTO{
			ID:        "new",
			ExpiresAt: time.Now().Add(10 * time.Minute),
		},
	}, nil
}

func (s *codexEntryCenterRoutesStub) Diagnose(ctx context.Context, req service.CodexDiagnoseRequest) (*service.CodexDiagnoseReport, error) {
	return &service.CodexDiagnoseReport{OK: true, TargetKind: "device", Checks: []service.CodexDiagnoseCheck{}}, nil
}

func (s *codexEntryCenterRoutesStub) ResyncDevice(ctx context.Context, userID int64, deviceID int64) (*service.CodexDeviceActionResponse, error) {
	return &service.CodexDeviceActionResponse{DeviceID: deviceID, Accepted: true}, nil
}

func (s *codexEntryCenterRoutesStub) RepairDevice(ctx context.Context, userID int64, deviceID int64) (*service.CodexDeviceActionResponse, error) {
	return &service.CodexDeviceActionResponse{DeviceID: deviceID, Accepted: true}, nil
}

func (s *codexEntryCenterRoutesStub) ReattachDevice(ctx context.Context, userID int64, deviceID int64) (*service.CodexDeviceActionResponse, error) {
	return &service.CodexDeviceActionResponse{DeviceID: deviceID, Accepted: true}, nil
}

func (s *codexEntryCenterRoutesStub) RevokeAttachment(ctx context.Context, userID int64, deviceID int64) (*service.CodexDeviceActionResponse, error) {
	return &service.CodexDeviceActionResponse{DeviceID: deviceID, Accepted: true}, nil
}

func (s *codexEntryCenterRoutesStub) RemoveDevice(ctx context.Context, userID int64, deviceID int64) (*service.CodexDeviceActionResponse, error) {
	return &service.CodexDeviceActionResponse{DeviceID: deviceID, Accepted: true}, nil
}
