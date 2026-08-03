package admin

import (
	"context"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type codexGatewayAdminAPI interface {
	GetSummary(ctx context.Context) (*service.CodexGatewayAdminSummary, error)
	ListProviderGroups(ctx context.Context) ([]service.CodexGatewayProviderRuntime, error)
	UpdateProviderGroup(ctx context.Context, provider service.CodexGatewayProvider, groupID int64) (*service.CodexGatewayProviderRuntime, error)
	ListModels(ctx context.Context) ([]service.CodexGatewayManagedModel, error)
	UpdateModel(ctx context.Context, modelID string, input service.CodexGatewayModelMutation) (*service.CodexGatewayManagedModel, error)
	Smoke(ctx context.Context, input service.CodexGatewaySmokeRequest) (*service.CodexGatewaySmokeResult, error)
	GetStateStoreSummary(ctx context.Context) (*service.CodexGatewayStateStoreSummary, error)
}

type codexGatewayProviderRoutingSection struct {
	TotalCount int                                   `json:"total_count"`
	Rows       []service.CodexGatewayProviderRuntime `json:"rows"`
}

type codexGatewayModelsSection struct {
	TotalCount int                                `json:"total_count"`
	Rows       []service.CodexGatewayManagedModel `json:"rows"`
}

type codexGatewaySummaryResponse struct {
	ProviderRoutingGroups codexGatewayProviderRoutingSection    `json:"provider_routing_groups"`
	ModelsSection         codexGatewayModelsSection             `json:"models_section"`
	StateStore            service.CodexGatewayStateStoreSummary `json:"state_store"`
	ProviderGroups        []service.CodexGatewayProviderRuntime `json:"provider_groups"`
	Models                []service.CodexGatewayManagedModel    `json:"models"`
	ProviderGroupCount    int                                   `json:"provider_group_count"`
	EnabledModelCount     int                                   `json:"enabled_model_count"`
	VisibleModelCount     int                                   `json:"visible_model_count"`
	StateEntryCount       int                                   `json:"state_entry_count"`
}

type CodexGatewayHandler struct {
	service codexGatewayAdminAPI
}

func NewCodexGatewayHandler(svc codexGatewayAdminAPI) *CodexGatewayHandler {
	return &CodexGatewayHandler{service: svc}
}

func (h *CodexGatewayHandler) Summary(c *gin.Context) {
	if h == nil || h.service == nil {
		response.Error(c, http.StatusNotImplemented, "Codex gateway admin service is not configured")
		return
	}
	summary, err := h.service.GetSummary(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if summary == nil {
		summary = &service.CodexGatewayAdminSummary{}
	}
	if summary.ProviderGroups == nil {
		summary.ProviderGroups = []service.CodexGatewayProviderRuntime{}
	}
	if summary.Models == nil {
		summary.Models = []service.CodexGatewayManagedModel{}
	}
	providerGroupCount := summary.ProviderGroupCount
	if providerGroupCount == 0 {
		providerGroupCount = len(summary.ProviderGroups)
	}
	enabledModelCount := summary.EnabledModelCount
	visibleModelCount := summary.VisibleModelCount
	if enabledModelCount == 0 || visibleModelCount == 0 {
		enabledModelCount = 0
		visibleModelCount = 0
		for _, model := range summary.Models {
			if model.Enabled {
				enabledModelCount++
			}
			if model.Visible {
				visibleModelCount++
			}
		}
	}
	stateEntryCount := summary.StateEntryCount
	if stateEntryCount == 0 {
		stateEntryCount = summary.StateStore.EntryCount
	}
	response.Success(c, codexGatewaySummaryResponse{
		ProviderRoutingGroups: codexGatewayProviderRoutingSection{
			TotalCount: len(summary.ProviderGroups),
			Rows:       summary.ProviderGroups,
		},
		ModelsSection: codexGatewayModelsSection{
			TotalCount: len(summary.Models),
			Rows:       summary.Models,
		},
		StateStore:         summary.StateStore,
		ProviderGroups:     summary.ProviderGroups,
		Models:             summary.Models,
		ProviderGroupCount: providerGroupCount,
		EnabledModelCount:  enabledModelCount,
		VisibleModelCount:  visibleModelCount,
		StateEntryCount:    stateEntryCount,
	})
}

func (h *CodexGatewayHandler) ProviderGroups(c *gin.Context) {
	if h == nil || h.service == nil {
		response.Error(c, http.StatusNotImplemented, "Codex gateway admin service is not configured")
		return
	}
	rows, err := h.service.ListProviderGroups(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if rows == nil {
		rows = []service.CodexGatewayProviderRuntime{}
	}
	response.Success(c, gin.H{"rows": rows})
}

func (h *CodexGatewayHandler) UpdateProviderGroups(c *gin.Context) {
	if h == nil || h.service == nil {
		response.Error(c, http.StatusNotImplemented, "Codex gateway admin service is not configured")
		return
	}
	var req struct {
		Provider service.CodexGatewayProvider `json:"provider"`
		GroupID  int64                        `json:"group_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	record, err := h.service.UpdateProviderGroup(c.Request.Context(), req.Provider, req.GroupID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, record)
}

func (h *CodexGatewayHandler) Models(c *gin.Context) {
	if h == nil || h.service == nil {
		response.Error(c, http.StatusNotImplemented, "Codex gateway admin service is not configured")
		return
	}
	rows, err := h.service.ListModels(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if rows == nil {
		rows = []service.CodexGatewayManagedModel{}
	}
	response.Success(c, gin.H{"rows": rows})
}

func (h *CodexGatewayHandler) UpdateModel(c *gin.Context) {
	if h == nil || h.service == nil {
		response.Error(c, http.StatusNotImplemented, "Codex gateway admin service is not configured")
		return
	}
	var req service.CodexGatewayModelMutation
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	record, err := h.service.UpdateModel(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, record)
}

func (h *CodexGatewayHandler) Smoke(c *gin.Context) {
	if h == nil || h.service == nil {
		response.Error(c, http.StatusNotImplemented, "Codex gateway admin service is not configured")
		return
	}
	var req service.CodexGatewaySmokeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	result, err := h.service.Smoke(c.Request.Context(), req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *CodexGatewayHandler) StateStoreSummary(c *gin.Context) {
	if h == nil || h.service == nil {
		response.Error(c, http.StatusNotImplemented, "Codex gateway admin service is not configured")
		return
	}
	summary, err := h.service.GetStateStoreSummary(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if summary == nil {
		summary = &service.CodexGatewayStateStoreSummary{}
	}
	response.Success(c, summary)
}
