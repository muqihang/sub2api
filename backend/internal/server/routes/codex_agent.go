package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func RegisterCodexAgentRoutes(v1 *gin.RouterGroup, h *handler.Handlers, jwtAuth middleware.JWTAuthMiddleware, settingService *service.SettingService) {
	public := v1.Group("/codex")
	{
		public.POST("/setup-grants/exchange", h.CodexAgent.ExchangeSetupGrant)
		public.POST("/devices/refresh", h.CodexAgent.RefreshDeviceToken)
		public.POST("/devices/revoke-managed", h.CodexAgent.RevokeManagedDevice)
	}

	authenticated := v1.Group("/codex")
	authenticated.Use(gin.HandlerFunc(jwtAuth))
	authenticated.Use(middleware.BackendModeUserGuard(settingService))
	{
		authenticated.POST("/setup-grants", h.CodexAgent.CreateSetupGrant)
		authenticated.GET("/devices", h.CodexAgent.ListDevices)
		authenticated.POST("/devices/revoke", h.CodexAgent.RevokeDevice)
	}
}
