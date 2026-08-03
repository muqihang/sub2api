package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
)

func registerCodexGatewayAdminRoutes(admin *gin.RouterGroup, h *handler.Handlers) {
	if h == nil || h.Admin == nil || h.Admin.CodexGateway == nil {
		return
	}

	codexGateway := admin.Group("/codex-gateway")
	{
		codexGateway.GET("/summary", h.Admin.CodexGateway.Summary)
		codexGateway.GET("/provider-groups", h.Admin.CodexGateway.ProviderGroups)
		codexGateway.PUT("/provider-groups", h.Admin.CodexGateway.UpdateProviderGroups)
		codexGateway.GET("/models", h.Admin.CodexGateway.Models)
		codexGateway.PUT("/models/:id", h.Admin.CodexGateway.UpdateModel)
		codexGateway.POST("/smoke", h.Admin.CodexGateway.Smoke)
		codexGateway.GET("/state-store/summary", h.Admin.CodexGateway.StateStoreSummary)
	}
}
