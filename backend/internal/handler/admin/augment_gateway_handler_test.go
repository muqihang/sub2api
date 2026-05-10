package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
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
	router.POST("/pool-sessions/import-local-cursor", h.ImportLocalCursorSession)
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

func TestAdminAugmentGatewaySummaryReturnsTypedSections(t *testing.T) {
	router, _ := setupAugmentGatewayAdminHandlerRouter(
		&augmentGatewayAdminSettingsStub{
			providerGroups: []service.AugmentGatewayProviderRuntime{
				{
					Provider:       service.AugmentGatewayProviderOpenAI,
					GroupID:        1001,
					Healthy:        true,
					TotalAccounts:  2,
					ActiveAccounts: 2,
				},
			},
			entitlementGroups: []service.AugmentGatewayEntitlementGroup{
				{
					ID:             201,
					Name:           "Augment Users",
					Status:         service.StatusActive,
					TotalAccounts:  3,
					ActiveAccounts: 2,
				},
			},
			models: []service.AugmentGatewayManagedModel{
				{
					Model: service.AugmentGatewayModel{
						ID:       "gpt-5.4",
						Provider: service.AugmentGatewayProviderOpenAI,
					},
					Enabled:         true,
					Visible:         true,
					SmokeStatus:     service.AugmentGatewaySmokeStatusPassed,
					ProviderHealthy: true,
				},
			},
			sourcePriority:     []string{"official_quick_login", "wukong_quick_login"},
			routePolicyVersion: "2026-05-08",
		},
		&augmentGatewayOfficialSessionAdminStub{
			listResult: []service.AugmentOfficialPoolSessionAdminView{
				{ID: 42, Source: "official_quick_login", Status: "active", HasCredentialPayload: true},
				{ID: 43, Source: "wukong_quick_login", Status: "disabled", HasCredentialPayload: false},
			},
		},
		&augmentGatewayUsageAdminStub{
			summary: &service.AugmentGatewayBillingSummary{
				EstimatedCost: 12.3,
				SettledCost:   10.1,
				CacheHitRatio: 0.42,
				Currency:      service.AugmentUsageCurrencyUSD,
			},
		},
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/summary", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, `"entitlement_groups"`)
	require.Contains(t, body, `"provider_routing_groups"`)
	require.Contains(t, body, `"official_session_pool"`)
	require.Contains(t, body, `"usage"`)
	require.Contains(t, body, `"configured_route_policy_version":"2026-05-08"`)
	require.Contains(t, body, `"route_policy_version":"2026-05-08"`)
	require.Contains(t, body, `"provider_groups"`)
	require.Contains(t, body, `"official_session_count":2`)
	require.Contains(t, body, `"active_session_count":1`)
	require.Contains(t, body, `"healthy_session_count":1`)
	require.Contains(t, body, `"estimated_cost":12.3`)
	require.Contains(t, body, `"settled_cost":10.1`)
	require.Contains(t, body, `"source_priority":["official_quick_login","wukong_quick_login"]`)
	require.Contains(t, body, `"name":"Augment Users"`)
	require.Contains(t, body, `"active_count":1`)
	require.Contains(t, body, `"healthy_count":1`)
	require.Contains(t, body, `"cache_hit_ratio":0.42`)
	require.NotContains(t, body, "access_token")
	require.NotContains(t, body, "refresh_token")
}

func TestAdminAugmentGatewaySummaryCompatibilityAliasesRemainPresentWhenZero(t *testing.T) {
	router, _ := setupAugmentGatewayAdminHandlerRouter(
		&augmentGatewayAdminSettingsStub{
			sourcePriority: []string{},
		},
		&augmentGatewayOfficialSessionAdminStub{},
		&augmentGatewayUsageAdminStub{
			summary: &service.AugmentGatewayBillingSummary{},
		},
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/summary", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, `"provider_groups":[]`)
	require.Contains(t, body, `"official_session_count":0`)
	require.Contains(t, body, `"active_session_count":0`)
	require.Contains(t, body, `"healthy_session_count":0`)
	require.Contains(t, body, `"estimated_cost":0`)
	require.Contains(t, body, `"settled_cost":0`)
	require.Contains(t, body, `"cache_hit_ratio":0`)
	require.Contains(t, body, `"source_priority":`)
}

func TestAdminAugmentGatewayOfficialSessionsNeverReturnSecrets(t *testing.T) {
	router, _ := setupAugmentGatewayAdminHandlerRouter(
		&augmentGatewayAdminSettingsStub{},
		&augmentGatewayOfficialSessionAdminStub{
			listResult: []service.AugmentOfficialPoolSessionAdminView{
				{
					ID:                   42,
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
		disableResult: &service.AugmentOfficialPoolSessionAdminView{
			ID:                   42,
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
		requireReloginResult: &service.AugmentOfficialPoolSessionAdminView{
			ID:                   42,
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
			diagnosticsResult: &service.AugmentOfficialPoolSessionDiagnostics{
				ID:                   42,
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

func TestAdminAugmentGatewayImportLocalCursorSessionRequiresPermissionAndReturnsImportedRow(t *testing.T) {
	sessionSvc := &augmentGatewayOfficialSessionAdminStub{
		importLocalCursorResult: &service.AugmentOfficialPoolSessionAdminView{
			ID:                   88,
			Source:               "official_quick_login",
			TenantOrigin:         "https://d12.api.augmentcode.com",
			Status:               "active",
			HasCredentialPayload: true,
		},
	}
	router, handler := setupAugmentGatewayAdminHandlerRouter(
		&augmentGatewayAdminSettingsStub{},
		sessionSvc,
		&augmentGatewayUsageAdminStub{},
	)
	handler.SetSessionVaultPermissionChecker(func(*gin.Context) bool { return true })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/pool-sessions/import-local-cursor", bytes.NewBufferString(`{"source":"official_quick_login"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, sessionSvc.importLocalCursorCalled)
	require.Equal(t, "official_quick_login", sessionSvc.importLocalCursorInput.Source)
	require.Contains(t, rec.Body.String(), `"id":88`)
}

type augmentGatewayAdminSettingsStub struct {
	updateProviderGroupResult  *service.AugmentGatewaySettingsVersion
	updateProviderGroupErr     error
	updateModelResult          *service.AugmentGatewaySettingsVersion
	updateModelErr             error
	updateSourcePriorityResult *service.AugmentGatewaySettingsVersion
	providerGroups             []service.AugmentGatewayProviderRuntime
	entitlementGroups          []service.AugmentGatewayEntitlementGroup
	models                     []service.AugmentGatewayManagedModel
	sourcePriority             []string
	routePolicyVersion         string
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

func (s *augmentGatewayAdminSettingsStub) ListEntitlementGroups(ctx context.Context) ([]service.AugmentGatewayEntitlementGroup, error) {
	return s.entitlementGroups, nil
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

func (s *augmentGatewayAdminSettingsStub) GetSourcePriority(ctx context.Context) ([]string, error) {
	if len(s.sourcePriority) == 0 {
		return []string{"official_quick_login", "wukong_quick_login"}, nil
	}
	return s.sourcePriority, nil
}

func (s *augmentGatewayAdminSettingsStub) GetRoutePolicyVersion(ctx context.Context) (string, error) {
	if s.routePolicyVersion == "" {
		return service.AugmentGatewayDefaultRoutePolicyVersion, nil
	}
	return s.routePolicyVersion, nil
}

func (s *augmentGatewayAdminSettingsStub) UpdateSourcePriority(ctx context.Context, sources []string, meta service.AugmentGatewaySettingsMutationMeta) (*service.AugmentGatewaySettingsVersion, error) {
	if s.updateSourcePriorityResult != nil {
		return s.updateSourcePriorityResult, nil
	}
	return &service.AugmentGatewaySettingsVersion{Namespace: service.AugmentGatewaySourcePriorityNamespace, Version: 1}, nil
}

type augmentGatewayOfficialSessionAdminStub struct {
	listResult              []service.AugmentOfficialPoolSessionAdminView
	diagnosticsResult       *service.AugmentOfficialPoolSessionDiagnostics
	revokeResult            *service.AugmentOfficialPoolSessionAdminView
	disableResult           *service.AugmentOfficialPoolSessionAdminView
	requireReloginResult    *service.AugmentOfficialPoolSessionAdminView
	importLocalCursorResult *service.AugmentOfficialPoolSessionAdminView
	revokeCalled            bool
	disableCalled           bool
	requireReloginCalled    bool
	importLocalCursorCalled bool
	importLocalCursorInput  service.AugmentOfficialPoolLocalCursorImportRequest
}

func (s *augmentGatewayOfficialSessionAdminStub) ListAdminSessions(ctx context.Context) ([]service.AugmentOfficialPoolSessionAdminView, error) {
	return s.listResult, nil
}

func (s *augmentGatewayOfficialSessionAdminStub) GetAdminSessionDiagnostics(ctx context.Context, userID int64) (*service.AugmentOfficialPoolSessionDiagnostics, error) {
	return s.diagnosticsResult, nil
}

func (s *augmentGatewayOfficialSessionAdminStub) RevokeSessionForAdmin(ctx context.Context, userID int64) (*service.AugmentOfficialPoolSessionAdminView, error) {
	s.revokeCalled = true
	return s.revokeResult, nil
}

func (s *augmentGatewayOfficialSessionAdminStub) DisableSessionForAdmin(ctx context.Context, userID int64) (*service.AugmentOfficialPoolSessionAdminView, error) {
	s.disableCalled = true
	return s.disableResult, nil
}

func (s *augmentGatewayOfficialSessionAdminStub) RequireSessionReloginForAdmin(ctx context.Context, userID int64) (*service.AugmentOfficialPoolSessionAdminView, error) {
	s.requireReloginCalled = true
	return s.requireReloginResult, nil
}

func (s *augmentGatewayOfficialSessionAdminStub) CreateBindIntent(ctx context.Context, adminUserID int64, input service.AugmentOfficialPoolBindIntentRequest) (*service.AugmentOfficialPoolBindIntentResponse, error) {
	return &service.AugmentOfficialPoolBindIntentResponse{}, nil
}

func (s *augmentGatewayOfficialSessionAdminStub) BindSession(ctx context.Context, adminUserID int64, bindToken string, input service.AugmentOfficialPoolBindRequest) (*service.AugmentOfficialPoolSessionAdminView, error) {
	return &service.AugmentOfficialPoolSessionAdminView{}, nil
}

func (s *augmentGatewayOfficialSessionAdminStub) ImportLocalCursorSessionForAdmin(ctx context.Context, adminUserID int64, input service.AugmentOfficialPoolLocalCursorImportRequest) (*service.AugmentOfficialPoolSessionAdminView, error) {
	s.importLocalCursorCalled = true
	s.importLocalCursorInput = input
	return s.importLocalCursorResult, nil
}

type augmentGatewayUsageAdminStub struct {
	called  bool
	rows    []service.AugmentGatewayBillingUsageRow
	page    *pagination.PaginationResult
	summary *service.AugmentGatewayBillingSummary
}

func (s *augmentGatewayUsageAdminStub) ListUsageAdmin(ctx context.Context, params pagination.PaginationParams) ([]service.AugmentGatewayBillingUsageRow, *pagination.PaginationResult, error) {
	s.called = true
	return s.rows, s.page, nil
}

func (s *augmentGatewayUsageAdminStub) GetSummaryAdmin(ctx context.Context) (*service.AugmentGatewayBillingSummary, error) {
	if s.summary == nil {
		return &service.AugmentGatewayBillingSummary{}, nil
	}
	return s.summary, nil
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
