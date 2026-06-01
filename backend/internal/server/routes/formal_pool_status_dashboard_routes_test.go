package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ihandler "github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFormalPoolStatusDashboardRoute_AdminOnlyAndSafe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adminSvc := &formalPoolStatusDashboardRouteAdminStub{accounts: []service.Account{
		formalPoolStatusDashboardRouteAccount(1, "user@example.com sk-ant-secret 123e4567-e89b-12d3-a456-426614174000"),
	}}
	router, authCalls := newFormalPoolStatusDashboardRouter(adminSvc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/formal-pool/status-dashboard", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 1, *authCalls, "dashboard route must be protected by admin middleware")
	body := rec.Body.String()
	for _, unsafe := range []string{"user@example.com", "sk-ant-secret", "123e4567-e89b-12d3-a456-426614174000", "access-secret", "refresh-secret", "proxy-secret.example.com", "proxy-user-secret", "proxy-pass-secret", "raw body", `"prompt"`, "telemetry"} {
		require.NotContains(t, body, unsafe)
	}
	require.Contains(t, body, "账号 #1")
}

func TestFormalPoolStatusDashboardRoute_AllowsOperationalEmailLabel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adminSvc := &formalPoolStatusDashboardRouteAdminStub{accounts: []service.Account{
		formalPoolStatusDashboardRouteAccount(3, "ops-user@example.com"),
	}}
	router, _ := newFormalPoolStatusDashboardRouter(adminSvc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/formal-pool/status-dashboard", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "ops-user@example.com")
}

func TestFormalPoolStatusDashboardRoute_ReturnsAllAccountsNotCurrentPagination(t *testing.T) {
	gin.SetMode(gin.TestMode)
	accounts := make([]service.Account, 0, 25)
	for i := 1; i <= 25; i++ {
		accounts = append(accounts, formalPoolStatusDashboardRouteAccount(int64(i), fmt.Sprintf("formal-%d", i)))
	}
	adminSvc := &formalPoolStatusDashboardRouteAdminStub{accounts: accounts}
	router, _ := newFormalPoolStatusDashboardRouter(adminSvc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/formal-pool/status-dashboard?page=1&page_size=1", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var envelope struct {
		Data struct {
			Accounts []struct {
				AccountID int64 `json:"account_id"`
			} `json:"accounts"`
			Summary struct {
				Total int `json:"total"`
			} `json:"summary"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	require.Len(t, envelope.Data.Accounts, 25)
	require.Equal(t, 25, envelope.Data.Summary.Total)
}

func newFormalPoolStatusDashboardRouter(adminSvc service.AdminService) (*gin.Engine, *int) {
	router := gin.New()
	v1 := router.Group("/api/v1")
	h := &ihandler.Handlers{Admin: &ihandler.AdminHandlers{Account: adminhandler.NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)}}
	authCalls := 0
	RegisterAdminRoutes(v1, h, middleware.AdminAuthMiddleware(func(c *gin.Context) {
		authCalls++
		c.Next()
	}))
	return router, &authCalls
}

type formalPoolStatusDashboardRouteAdminStub struct {
	service.AdminService
	accounts []service.Account
}

func (s *formalPoolStatusDashboardRouteAdminStub) ListAccounts(_ context.Context, page, pageSize int, platform, accountType, status, search string, groupID int64, privacyMode string, sortBy, sortOrder string) ([]service.Account, int64, error) {
	filtered := make([]service.Account, 0, len(s.accounts))
	for _, acc := range s.accounts {
		if platform != "" && acc.Platform != platform {
			continue
		}
		if accountType != "" && acc.Type != accountType {
			continue
		}
		filtered = append(filtered, acc)
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	start := (page - 1) * pageSize
	if start >= len(filtered) {
		return nil, int64(len(filtered)), nil
	}
	end := start + pageSize
	if end > len(filtered) {
		end = len(filtered)
	}
	return filtered[start:end], int64(len(filtered)), nil
}

func formalPoolStatusDashboardRouteAccount(id int64, name string) service.Account {
	stamp := "2026-06-01T11:00:00Z"
	return service.Account{
		ID:          id,
		Name:        name,
		Platform:    service.PlatformAnthropic,
		Type:        service.AccountTypeSetupToken,
		Status:      service.StatusActive,
		Schedulable: true,
		Credentials: map[string]any{"access_token": "access-secret", "refresh_token": "refresh-secret"},
		Proxy:       &service.Proxy{Host: "proxy-secret.example.com", Username: "proxy-user-secret", Password: "proxy-pass-secret"},
		CreatedAt:   time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		Extra: map[string]any{
			service.FormalPoolExtraOnboardingStage:             service.FormalPoolStageProduction,
			service.FormalPoolExtraRuntimeRegistered:           "true",
			service.FormalPoolExtraRuntimeRegisteredAt:         stamp,
			"cc_gateway_account_ref":                           "hmac-sha256:" + strings.Repeat("a", 64),
			"cc_gateway_egress_bucket_enabled":                 "true",
			"cc_gateway_egress_bucket":                         "bucket-safe",
			service.FormalPoolExtraHealthcheckStatus:           "passed",
			service.FormalPoolExtraHealthcheckStatusCodeBucket: "status_2xx",
			service.FormalPoolExtraHealthcheckRawRef:           "hmac-sha256:" + strings.Repeat("b", 64),
			service.FormalPoolExtraHealthcheckCCGatewaySeen:    true,
			service.FormalPoolExtraHealthcheckFallbackDetected: false,
			service.FormalPoolExtraHealthcheckProxyMismatch:    false,
			service.FormalPoolExtraHealthcheckRiskTextDetected: false,
			service.FormalPoolExtraLastHealthcheckAt:           stamp,
			service.FormalPoolExtraLastFailureCode:             `sk-ant-secret access_token=access-secret raw body {"prompt":"secret"}`,
			service.FormalPoolExtraOnboardingLastErrorBucket:   "proxy_password=proxy-pass-secret raw body telemetry",
		},
	}
}
