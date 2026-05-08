package admin

import (
	"context"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type augmentGatewayAdminSettingsAPI interface {
	ListProviderGroups(ctx context.Context) ([]service.AugmentGatewayProviderRuntime, error)
	UpdateProviderGroup(ctx context.Context, provider service.AugmentGatewayProvider, setting service.AugmentGatewayProviderGroupSetting, meta service.AugmentGatewaySettingsMutationMeta) (*service.AugmentGatewaySettingsVersion, error)
	ListModels(ctx context.Context) ([]service.AugmentGatewayManagedModel, error)
	UpdateModel(ctx context.Context, modelID string, setting service.AugmentGatewayModelSetting, meta service.AugmentGatewaySettingsMutationMeta) (*service.AugmentGatewaySettingsVersion, error)
	GetSourcePriority(ctx context.Context) ([]string, error)
	UpdateSourcePriority(ctx context.Context, sources []string, meta service.AugmentGatewaySettingsMutationMeta) (*service.AugmentGatewaySettingsVersion, error)
}

type augmentGatewayOfficialSessionAdminAPI interface {
	ListAdminSessions(ctx context.Context) ([]service.AugmentOfficialPoolSessionAdminView, error)
	GetAdminSessionDiagnostics(ctx context.Context, sessionID int64) (*service.AugmentOfficialPoolSessionDiagnostics, error)
	RevokeSessionForAdmin(ctx context.Context, sessionID int64) (*service.AugmentOfficialPoolSessionAdminView, error)
	DisableSessionForAdmin(ctx context.Context, sessionID int64) (*service.AugmentOfficialPoolSessionAdminView, error)
	RequireSessionReloginForAdmin(ctx context.Context, sessionID int64) (*service.AugmentOfficialPoolSessionAdminView, error)
	CreateBindIntent(ctx context.Context, adminUserID int64, input service.AugmentOfficialPoolBindIntentRequest) (*service.AugmentOfficialPoolBindIntentResponse, error)
	BindSession(ctx context.Context, adminUserID int64, bindToken string, input service.AugmentOfficialPoolBindRequest) (*service.AugmentOfficialPoolSessionAdminView, error)
}

type augmentGatewayUsageAdminAPI interface {
	ListUsageAdmin(ctx context.Context, params pagination.PaginationParams) ([]service.AugmentGatewayBillingUsageRow, *pagination.PaginationResult, error)
}

type AugmentGatewayHandler struct {
	settingsSvc      augmentGatewayAdminSettingsAPI
	sessionSvc       augmentGatewayOfficialSessionAdminAPI
	usageSvc         augmentGatewayUsageAdminAPI
	vaultPermission  func(*gin.Context) bool
}

func NewAugmentGatewayHandler(
	settingsSvc augmentGatewayAdminSettingsAPI,
	sessionSvc augmentGatewayOfficialSessionAdminAPI,
	usageSvc augmentGatewayUsageAdminAPI,
) *AugmentGatewayHandler {
	return &AugmentGatewayHandler{
		settingsSvc: settingsSvc,
		sessionSvc:  sessionSvc,
		usageSvc:    usageSvc,
		vaultPermission: func(c *gin.Context) bool {
			authMethod, _ := c.Get("auth_method")
			return authMethod == "admin_api_key" || authMethod == "jwt"
		},
	}
}

func (h *AugmentGatewayHandler) SetSessionVaultPermissionChecker(checker func(*gin.Context) bool) {
	if checker != nil {
		h.vaultPermission = checker
	}
}

func (h *AugmentGatewayHandler) Summary(c *gin.Context) {
	providerGroups, err := h.settingsSvc.ListProviderGroups(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	models, err := h.settingsSvc.ListModels(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	sessions, err := h.sessionSvc.ListAdminSessions(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	sourcePriority, err := h.settingsSvc.GetSourcePriority(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	activeCount := 0
	healthyCount := 0
	for _, session := range sessions {
		if session.Status == "active" {
			activeCount++
		}
		if session.Status == "active" && session.HasCredentialPayload {
			healthyCount++
		}
	}
	response.Success(c, gin.H{
		"provider_groups":        providerGroups,
		"models":                 models,
		"official_session_count": len(sessions),
		"active_session_count":   activeCount,
		"healthy_session_count":  healthyCount,
		"source_priority":        sourcePriority,
	})
}

func (h *AugmentGatewayHandler) ProviderGroups(c *gin.Context) {
	rows, err := h.settingsSvc.ListProviderGroups(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"rows": rows})
}

func (h *AugmentGatewayHandler) UpdateProviderGroups(c *gin.Context) {
	var req struct {
		Provider        service.AugmentGatewayProvider `json:"provider"`
		GroupID         int64                          `json:"group_id"`
		ExpectedVersion int64                          `json:"expected_version"`
		RequestID       string                         `json:"request_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	record, err := h.settingsSvc.UpdateProviderGroup(c.Request.Context(), req.Provider, service.AugmentGatewayProviderGroupSetting{
		GroupID: req.GroupID,
	}, service.AugmentGatewaySettingsMutationMeta{
		ExpectedVersion: req.ExpectedVersion,
		ActorAdminID:    getAdminIDFromContext(c),
		RequestID:       strings.TrimSpace(req.RequestID),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, record)
}

func (h *AugmentGatewayHandler) Models(c *gin.Context) {
	rows, err := h.settingsSvc.ListModels(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"rows": rows})
}

func (h *AugmentGatewayHandler) UpdateModel(c *gin.Context) {
	var req struct {
		Enabled         bool   `json:"enabled"`
		SmokeStatus     string `json:"smoke_status"`
		ExpectedVersion int64  `json:"expected_version"`
		RequestID       string `json:"request_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	record, err := h.settingsSvc.UpdateModel(c.Request.Context(), c.Param("id"), service.AugmentGatewayModelSetting{
		Enabled:     req.Enabled,
		SmokeStatus: service.AugmentGatewaySmokeStatus(strings.TrimSpace(req.SmokeStatus)),
	}, service.AugmentGatewaySettingsMutationMeta{
		ExpectedVersion: req.ExpectedVersion,
		ActorAdminID:    getAdminIDFromContext(c),
		RequestID:       strings.TrimSpace(req.RequestID),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, record)
}

func (h *AugmentGatewayHandler) OfficialSessions(c *gin.Context) {
	rows, err := h.sessionSvc.ListAdminSessions(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"rows": rows})
}

func (h *AugmentGatewayHandler) SourcePriority(c *gin.Context) {
	rows, err := h.settingsSvc.GetSourcePriority(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"sources": rows})
}

func (h *AugmentGatewayHandler) UpdateSourcePriority(c *gin.Context) {
	var req struct {
		Sources         []string `json:"sources"`
		ExpectedVersion int64    `json:"expected_version"`
		RequestID       string   `json:"request_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	record, err := h.settingsSvc.UpdateSourcePriority(c.Request.Context(), req.Sources, service.AugmentGatewaySettingsMutationMeta{
		ExpectedVersion: req.ExpectedVersion,
		ActorAdminID:    getAdminIDFromContext(c),
		RequestID:       strings.TrimSpace(req.RequestID),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, record)
}

func (h *AugmentGatewayHandler) PoolSessionBindIntent(c *gin.Context) {
	if h.vaultPermission != nil && !h.vaultPermission(c) {
		response.Forbidden(c, "Augment session vault permission required")
		return
	}
	var req struct {
		Mode            string   `json:"mode"`
		Source          string   `json:"source"`
		TenantAllowlist []string `json:"tenant_allowlist"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	data, err := h.sessionSvc.CreateBindIntent(c.Request.Context(), getAdminIDFromContext(c), service.AugmentOfficialPoolBindIntentRequest{
		Mode:            strings.TrimSpace(req.Mode),
		Source:          strings.TrimSpace(req.Source),
		TenantAllowlist: append([]string(nil), req.TenantAllowlist...),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, data)
}

func (h *AugmentGatewayHandler) PoolSessionBind(c *gin.Context) {
	if h.vaultPermission != nil && !h.vaultPermission(c) {
		response.Forbidden(c, "Augment session vault permission required")
		return
	}
	var req struct {
		BindToken    string         `json:"bind_token"`
		BindIntentID string         `json:"bind_intent_id"`
		State        string         `json:"state"`
		Mode         string         `json:"mode"`
		Source       string         `json:"source"`
		Payload      map[string]any `json:"payload"`
		RequestID    string         `json:"request_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	view, err := h.sessionSvc.BindSession(c.Request.Context(), getAdminIDFromContext(c), strings.TrimSpace(req.BindToken), service.AugmentOfficialPoolBindRequest{
		BindIntentID: strings.TrimSpace(req.BindIntentID),
		State:        strings.TrimSpace(req.State),
		Mode:         strings.TrimSpace(req.Mode),
		Source:       strings.TrimSpace(req.Source),
		Payload:      req.Payload,
		RequestID:    strings.TrimSpace(req.RequestID),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, view)
}

func (h *AugmentGatewayHandler) RevokeOfficialSession(c *gin.Context) {
	h.mutateOfficialSession(c, h.sessionSvc.RevokeSessionForAdmin)
}

func (h *AugmentGatewayHandler) DisableOfficialSession(c *gin.Context) {
	h.mutateOfficialSession(c, h.sessionSvc.DisableSessionForAdmin)
}

func (h *AugmentGatewayHandler) RequireOfficialSessionRelogin(c *gin.Context) {
	h.mutateOfficialSession(c, h.sessionSvc.RequireSessionReloginForAdmin)
}

func (h *AugmentGatewayHandler) OfficialSessionDiagnostics(c *gin.Context) {
	sessionID, ok := parseAdminAugmentUserID(c)
	if !ok {
		return
	}
	diagnostics, err := h.sessionSvc.GetAdminSessionDiagnostics(c.Request.Context(), sessionID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, diagnostics)
}

func (h *AugmentGatewayHandler) Usage(c *gin.Context) {
	rows, page, err := h.usageSvc.ListUsageAdmin(c.Request.Context(), pagination.PaginationParams{
		Page:     parsePositiveInt(c.Query("page"), 1),
		PageSize: parsePositiveInt(c.Query("page_size"), 20),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"rows": rows, "page": page})
}

func (h *AugmentGatewayHandler) mutateOfficialSession(c *gin.Context, action func(context.Context, int64) (*service.AugmentOfficialPoolSessionAdminView, error)) {
	if h.vaultPermission != nil && !h.vaultPermission(c) {
		response.Forbidden(c, "Augment session vault permission required")
		return
	}
	userID, ok := parseAdminAugmentUserID(c)
	if !ok {
		return
	}
	view, err := action(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, view)
}

func parseAdminAugmentUserID(c *gin.Context) (int64, bool) {
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || userID <= 0 {
		response.BadRequest(c, "Invalid user ID")
		return 0, false
	}
	return userID, true
}

func parsePositiveInt(raw string, fallback int) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
