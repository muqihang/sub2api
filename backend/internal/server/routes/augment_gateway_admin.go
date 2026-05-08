package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
)

func registerAugmentGatewayAdminRoutes(admin *gin.RouterGroup, h *handler.Handlers) {
	augmentGateway := admin.Group("/augment-gateway")
	{
		augmentGateway.GET("/summary", h.Admin.AugmentGateway.Summary)
		augmentGateway.GET("/provider-groups", h.Admin.AugmentGateway.ProviderGroups)
		augmentGateway.PUT("/provider-groups", h.Admin.AugmentGateway.UpdateProviderGroups)
		augmentGateway.GET("/models", h.Admin.AugmentGateway.Models)
		augmentGateway.PUT("/models/:id", h.Admin.AugmentGateway.UpdateModel)
		augmentGateway.GET("/official-sessions", h.Admin.AugmentGateway.OfficialSessions)
		augmentGateway.POST("/official-sessions/:id/revoke", h.Admin.AugmentGateway.RevokeOfficialSession)
		augmentGateway.POST("/official-sessions/:id/disable", h.Admin.AugmentGateway.DisableOfficialSession)
		augmentGateway.POST("/official-sessions/:id/require-relogin", h.Admin.AugmentGateway.RequireOfficialSessionRelogin)
		augmentGateway.GET("/official-sessions/:id/diagnostics", h.Admin.AugmentGateway.OfficialSessionDiagnostics)
		augmentGateway.GET("/usage", h.Admin.AugmentGateway.Usage)
	}
}
