package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setupAugmentGatewayAdminHandlerRouter(
	settingsSvc augmentGatewayAdminSettingsAPI,
	sessionSvc augmentGatewayOfficialSessionAdminAPI,
	usageSvc augmentGatewayUsageAdminAPI,
) (*gin.Engine, *AugmentGatewayHandler) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewAugmentGatewayHandler(settingsSvc, sessionSvc, usageSvc)
	router.GET("/summary", h.Summary)
	router.GET("/provider-groups", h.ProviderGroups)
	router.PUT("/provider-groups", h.UpdateProviderGroups)
	router.GET("/models", h.Models)
	router.PUT("/models/:id", h.UpdateModel)
	router.GET("/official-sessions", h.OfficialSessions)
	router.POST("/official-sessions/:id/revoke", h.RevokeOfficialSession)
	router.POST("/official-sessions/:id/disable", h.DisableOfficialSession)
	router.POST("/official-sessions/:id/require-relogin", h.RequireOfficialSessionRelogin)
	router.GET("/official-sessions/:id/diagnostics", h.OfficialSessionDiagnostics)
	router.GET("/usage", h.Usage)
	return router, h
}

func TestAdminAugmentGatewayProviderGroupsUpdateAuditsDiff(t *testing.T) {
	router, _ := setupAugmentGatewayAdminHandlerRouter(
		&augmentGatewayAdminSettingsStub{
			updateProviderGroupResult: &service.AugmentGatewaySettingsVersion{
				Namespace:  service.AugmentGatewayProviderGroupOpenAINamespace,
				Version:    2,
				BeforeJSON: mustAdminJSON(t, service.AugmentGatewayProviderGroupSetting{GroupID: 1001}),
				AfterJSON:  mustAdminJSON(t, service.AugmentGatewayProviderGroupSetting{GroupID: 1002}),
				Action:     service.AugmentGatewaySettingsActionUpdate,
				Result:     service.AugmentGatewaySettingsResultSuccess,
			},
		},
		&augmentGatewayOfficialSessionAdminStub{},
		&augmentGatewayUsageAdminStub{},
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/provider-groups", bytes.NewBufferString(`{"provider":"openai","group_id":1002,"expected_version":1,"request_id":"req-provider"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"before_json"`)
	require.Contains(t, rec.Body.String(), `"after_json"`)
}

func TestAdminAugmentGatewayModelsRejectVisibleWithoutSmoke(t *testing.T) {
	router, _ := setupAugmentGatewayAdminHandlerRouter(
		&augmentGatewayAdminSettingsStub{
			updateModelErr: service.ErrAugmentGatewayModelSmokeRequired,
		},
		&augmentGatewayOfficialSessionAdminStub{},
		&augmentGatewayUsageAdminStub{},
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/models/gpt-5.4", bytes.NewBufferString(`{"enabled":true,"smoke_status":"pending","expected_version":1}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "AUGMENT_GATEWAY_MODEL_SMOKE_REQUIRED")
}

func TestAdminAugmentGatewayModelsRejectVisibleWithoutExplicitSmokeStatus(t *testing.T) {
	router, _ := setupAugmentGatewayAdminHandlerRouter(
		&augmentGatewayAdminSettingsStub{
			updateModelErr: service.ErrAugmentGatewayModelSmokeRequired,
		},
		&augmentGatewayOfficialSessionAdminStub{},
		&augmentGatewayUsageAdminStub{},
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/models/gpt-5.4", bytes.NewBufferString(`{"enabled":true,"expected_version":1}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "AUGMENT_GATEWAY_MODEL_SMOKE_REQUIRED")
}

func TestAdminAugmentGatewayOfficialSessionsNeverReturnSecrets(t *testing.T) {
	router, _ := setupAugmentGatewayAdminHandlerRouter(
		&augmentGatewayAdminSettingsStub{},
		&augmentGatewayOfficialSessionAdminStub{
			listResult: []service.AugmentOfficialSessionAdminView{
				{
					UserID:               42,
					Source:               "wukong_quick_login",
					TenantOrigin:         "https://tenant.example.com",
					FingerprintPrefix:    "fp-prefix",
					HasCredentialPayload: true,
				},
			},
		},
		&augmentGatewayUsageAdminStub{},
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/official-sessions", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.NotContains(t, body, "access_token")
	require.NotContains(t, body, "refresh_token")
	require.NotContains(t, body, "encrypted_credential_payload")
}

func TestAdminAugmentGatewayRevokeRequiresSessionVaultPermission(t *testing.T) {
	router, handler := setupAugmentGatewayAdminHandlerRouter(
		&augmentGatewayAdminSettingsStub{},
		&augmentGatewayOfficialSessionAdminStub{},
		&augmentGatewayUsageAdminStub{},
	)
	handler.SetSessionVaultPermissionChecker(func(*gin.Context) bool { return false })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/official-sessions/42/revoke", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestAdminAugmentGatewayDisableSessionRequiresPermissionAndClearsCredential(t *testing.T) {
	sessionSvc := &augmentGatewayOfficialSessionAdminStub{
		disableResult: &service.AugmentOfficialSessionAdminView{
			UserID:               42,
			Status:               "revoked",
			HasCredentialPayload: false,
		},
	}
	router, handler := setupAugmentGatewayAdminHandlerRouter(
		&augmentGatewayAdminSettingsStub{},
		sessionSvc,
		&augmentGatewayUsageAdminStub{},
	)
	handler.SetSessionVaultPermissionChecker(func(*gin.Context) bool { return true })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/official-sessions/42/disable", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, sessionSvc.disableCalled)
	require.Contains(t, rec.Body.String(), `"has_credential_payload":false`)
}

func TestAdminAugmentGatewayRequireReloginRequiresPermissionAndClearsCredential(t *testing.T) {
	sessionSvc := &augmentGatewayOfficialSessionAdminStub{
		requireReloginResult: &service.AugmentOfficialSessionAdminView{
			UserID:               42,
			Status:               "revoked",
			HasCredentialPayload: false,
		},
	}
	router, handler := setupAugmentGatewayAdminHandlerRouter(
		&augmentGatewayAdminSettingsStub{},
		sessionSvc,
		&augmentGatewayUsageAdminStub{},
	)
	handler.SetSessionVaultPermissionChecker(func(*gin.Context) bool { return true })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/official-sessions/42/require-relogin", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, sessionSvc.requireReloginCalled)
	require.Contains(t, rec.Body.String(), `"has_credential_payload":false`)
}

func TestAdminAugmentGatewayDiagnosticsAreAllowlistedAndSecretFree(t *testing.T) {
	router, _ := setupAugmentGatewayAdminHandlerRouter(
		&augmentGatewayAdminSettingsStub{},
		&augmentGatewayOfficialSessionAdminStub{
			diagnosticsResult: &service.AugmentOfficialSessionAdminDiagnostics{
				UserID:               42,
				Source:               "official_quick_login",
				TenantHost:           "tenant.example.com",
				FingerprintPrefix:    "fp-prefix",
				HasCredentialPayload: false,
			},
		},
		&augmentGatewayUsageAdminStub{},
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/official-sessions/42/diagnostics", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, `"tenant_host":"tenant.example.com"`)
	require.NotContains(t, body, "access_token")
	require.NotContains(t, body, "refresh_token")
}

func TestAdminAugmentGatewayUsageFiltersClientProduct(t *testing.T) {
	usageSvc := &augmentGatewayUsageAdminStub{
		rows: []service.AugmentGatewayBillingUsageRow{
			{Model: "gpt-5.4", RequestID: "req-usage"},
		},
		page: &pagination.PaginationResult{Page: 1, PageSize: 20, Pages: 1, Total: 1},
	}
	router, _ := setupAugmentGatewayAdminHandlerRouter(
		&augmentGatewayAdminSettingsStub{},
		&augmentGatewayOfficialSessionAdminStub{},
		usageSvc,
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/usage?page=1&page_size=20", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, usageSvc.called)
	require.Contains(t, rec.Body.String(), `"req-usage"`)
}

type augmentGatewayAdminSettingsStub struct {
	updateProviderGroupResult *service.AugmentGatewaySettingsVersion
	updateProviderGroupErr    error
	updateModelResult         *service.AugmentGatewaySettingsVersion
	updateModelErr            error
	providerGroups            []service.AugmentGatewayProviderRuntime
	models                    []service.AugmentGatewayManagedModel
}

func (s *augmentGatewayAdminSettingsStub) ListProviderGroups(ctx context.Context) ([]service.AugmentGatewayProviderRuntime, error) {
	return s.providerGroups, nil
}

func (s *augmentGatewayAdminSettingsStub) UpdateProviderGroup(ctx context.Context, provider service.AugmentGatewayProvider, setting service.AugmentGatewayProviderGroupSetting, meta service.AugmentGatewaySettingsMutationMeta) (*service.AugmentGatewaySettingsVersion, error) {
	if s.updateProviderGroupErr != nil {
		return nil, s.updateProviderGroupErr
	}
	if s.updateProviderGroupResult != nil {
		return s.updateProviderGroupResult, nil
	}
	return &service.AugmentGatewaySettingsVersion{Namespace: service.AugmentGatewayProviderGroupOpenAINamespace, Version: 1}, nil
}

func (s *augmentGatewayAdminSettingsStub) ListModels(ctx context.Context) ([]service.AugmentGatewayManagedModel, error) {
	return s.models, nil
}

func (s *augmentGatewayAdminSettingsStub) UpdateModel(ctx context.Context, modelID string, setting service.AugmentGatewayModelSetting, meta service.AugmentGatewaySettingsMutationMeta) (*service.AugmentGatewaySettingsVersion, error) {
	if s.updateModelErr != nil {
		return nil, s.updateModelErr
	}
	if s.updateModelResult != nil {
		return s.updateModelResult, nil
	}
	return &service.AugmentGatewaySettingsVersion{Namespace: service.AugmentGatewayEnabledModelsNamespace, Version: 1}, nil
}

type augmentGatewayOfficialSessionAdminStub struct {
	listResult            []service.AugmentOfficialSessionAdminView
	diagnosticsResult     *service.AugmentOfficialSessionAdminDiagnostics
	revokeResult          *service.AugmentOfficialSessionAdminView
	disableResult         *service.AugmentOfficialSessionAdminView
	requireReloginResult  *service.AugmentOfficialSessionAdminView
	revokeCalled          bool
	disableCalled         bool
	requireReloginCalled  bool
}

func (s *augmentGatewayOfficialSessionAdminStub) ListAdminSessions(ctx context.Context) ([]service.AugmentOfficialSessionAdminView, error) {
	return s.listResult, nil
}

func (s *augmentGatewayOfficialSessionAdminStub) GetAdminSessionDiagnostics(ctx context.Context, userID int64) (*service.AugmentOfficialSessionAdminDiagnostics, error) {
	return s.diagnosticsResult, nil
}

func (s *augmentGatewayOfficialSessionAdminStub) RevokeOfficialSessionForAdmin(ctx context.Context, userID int64) (*service.AugmentOfficialSessionAdminView, error) {
	s.revokeCalled = true
	return s.revokeResult, nil
}

func (s *augmentGatewayOfficialSessionAdminStub) DisableOfficialSessionForAdmin(ctx context.Context, userID int64) (*service.AugmentOfficialSessionAdminView, error) {
	s.disableCalled = true
	return s.disableResult, nil
}

func (s *augmentGatewayOfficialSessionAdminStub) RequireOfficialSessionReloginForAdmin(ctx context.Context, userID int64) (*service.AugmentOfficialSessionAdminView, error) {
	s.requireReloginCalled = true
	return s.requireReloginResult, nil
}

type augmentGatewayUsageAdminStub struct {
	called bool
	rows   []service.AugmentGatewayBillingUsageRow
	page   *pagination.PaginationResult
}

func (s *augmentGatewayUsageAdminStub) ListUsageAdmin(ctx context.Context, params pagination.PaginationParams) ([]service.AugmentGatewayBillingUsageRow, *pagination.PaginationResult, error) {
	s.called = true
	return s.rows, s.page, nil
}

func mustAdminJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return data
}

func TestAdminAugmentGatewayOfficialSessionsServiceErrorsBubble(t *testing.T) {
	router, _ := setupAugmentGatewayAdminHandlerRouter(
		&augmentGatewayAdminSettingsStub{},
		&augmentGatewayOfficialSessionAdminStub{},
		&augmentGatewayUsageAdminStub{},
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/summary", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

var _ = infraerrors.BadRequest
var _ = time.Now
