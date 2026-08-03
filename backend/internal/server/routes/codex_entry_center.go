package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func RegisterCodexEntryCenterRoutes(v1 *gin.RouterGroup, h *handler.Handlers, jwtAuth middleware.JWTAuthMiddleware, settingService *service.SettingService) {
	if h.CodexEntryCenter == nil {
		return
	}

	authenticated := v1.Group("/codex")
	authenticated.Use(gin.HandlerFunc(jwtAuth))
	authenticated.Use(middleware.BackendModeUserGuard(settingService))
	{
		authenticated.GET("/summary", h.CodexEntryCenter.GetSummary)
		authenticated.POST("/diagnose", h.CodexEntryCenter.Diagnose)
		authenticated.POST("/setup-sessions", h.CodexEntryCenter.CreateSetupSession)
		authenticated.POST("/setup-sessions/:id/regenerate", h.CodexEntryCenter.RegenerateSetupSession)
		authenticated.POST("/devices/:id/resync", h.CodexEntryCenter.ResyncDevice)
		authenticated.POST("/devices/:id/repair", h.CodexEntryCenter.RepairDevice)
		authenticated.POST("/devices/:id/reattach", h.CodexEntryCenter.ReattachDevice)
		authenticated.POST("/devices/:id/revoke-attachment", h.CodexEntryCenter.RevokeAttachment)
		authenticated.DELETE("/devices/:id", h.CodexEntryCenter.RemoveDevice)
	}
}
