package routes

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func RegisterCodexGatewayRoutes(
	r *gin.Engine,
	h *handler.Handlers,
	apiKeyAuth middleware.APIKeyAuthMiddleware,
	opsService *service.OpsService,
	settingService *service.SettingService,
	cfg *config.Config,
) {
	if r == nil || h == nil || h.CodexGateway == nil {
		return
	}

	maxBodySize := int64(1 << 20)
	if cfg != nil && cfg.Gateway.MaxBodySize > 0 {
		maxBodySize = cfg.Gateway.MaxBodySize
	}
	bodyLimit := middleware.RequestBodyLimit(maxBodySize)
	clientRequestID := middleware.ClientRequestID()
	opsErrorLogger := handler.OpsErrorLoggerMiddleware(opsService)
	endpointNorm := handler.InboundEndpointMiddleware()

	base := r.Group("/codex/v1")
	base.Use(bodyLimit, clientRequestID, opsErrorLogger, endpointNorm)
	{
		base.GET("/responses", func(c *gin.Context) {
			service.WriteCodexGatewayErrorJSON(c.Writer, http.StatusMethodNotAllowed, service.CodexGatewayErrorTypeInvalidRequest, "method_not_allowed", "GET /codex/v1/responses is not supported")
		})
		base.POST("/responses/compact", func(c *gin.Context) {
			service.WriteCodexGatewayErrorJSON(c.Writer, http.StatusNotImplemented, service.CodexGatewayErrorTypeInvalidRequest, "not_implemented", "POST /codex/v1/responses/compact is not implemented")
		})
	}

	authed := base.Group("")
	authed.Use(gin.HandlerFunc(apiKeyAuth))
	authed.Use(middleware.RequireGroupAssignment(settingService, middleware.CodexGatewayErrorWriter))
	{
		authed.POST("/responses", h.CodexGateway.Responses)
		if cfg != nil && cfg.Gateway.Codex.Enabled {
			authed.GET("/models", h.CodexGateway.Models)
		}
	}
}
