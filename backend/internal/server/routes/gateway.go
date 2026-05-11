package routes

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// RegisterGatewayRoutes 注册 API 网关路由（Claude/OpenAI/Gemini 兼容）
func RegisterGatewayRoutes(
	r *gin.Engine,
	h *handler.Handlers,
	apiKeyAuth middleware.APIKeyAuthMiddleware,
	apiKeyService *service.APIKeyService,
	subscriptionService *service.SubscriptionService,
	opsService *service.OpsService,
	settingService *service.SettingService,
	codexAgentService middleware.ManagedDeviceAccessValidator,
	cfg *config.Config,
) {
	r.GET("/usage/api/balance", h.Auth.AugmentLegacyBalance)
	r.GET("/usage/api/get-models", h.Auth.AugmentLegacyModels)
	r.GET("/usage/api/getLoginToken", h.Auth.AugmentLegacyLoginToken)
	r.POST("/get-models", h.Auth.AugmentLegacyInternalGetModels)
	r.POST("/batch-upload", h.Auth.AugmentLegacyBatchUpload)
	r.POST("/checkpoint-blobs", h.Auth.AugmentLegacyCheckpointBlobs)
	r.POST("/find-missing", h.Auth.AugmentLegacyFindMissing)
	r.POST("/save-chat", h.Auth.AugmentLegacySaveChat)
	r.POST("/chat", h.Auth.AugmentLegacyChat)
	r.POST("/chat-stream", h.Auth.AugmentLegacyChatStream)
	r.POST("/prompt-enhancer", h.Auth.AugmentLegacyPromptEnhancer)
	r.POST("/instruction-stream", h.Auth.AugmentLegacyInstructionStream)
	r.POST("/smart-paste-stream", h.Auth.AugmentLegacySmartPasteStream)
	r.POST("/generate-commit-message-stream", h.Auth.AugmentLegacyGenerateCommitMessageStream)
	r.POST("/next_edit_loc", h.Auth.AugmentLegacyNextEditLocation)
	r.POST("/next-edit-stream", h.Auth.AugmentLegacyNextEditStream)
	r.POST("/remote-agents/list", h.Auth.AugmentLegacyListRemoteAgents)
	r.POST("/agents/codebase-retrieval", h.Auth.AugmentLegacyCodebaseRetrieval)
	r.POST("/agents/list-remote-tools", h.Auth.AugmentLegacyListRemoteTools)
	r.POST("/get-implicit-external-sources", h.Auth.AugmentLegacyGetImplicitExternalSources)
	r.POST("/search-external-sources", h.Auth.AugmentLegacySearchExternalSources)
	r.POST("/context-canvas/list", h.Auth.AugmentLegacyContextCanvasList)
	r.GET("/notifications/read", h.Auth.AugmentLegacyNotificationsRead)
	r.POST("/notifications/read", h.Auth.AugmentLegacyNotificationsRead)
	r.POST("/notifications/mark-as-read", h.Auth.AugmentLegacyNotificationsMarkRead)
	r.GET("/subscription-banner", h.Auth.AugmentLegacySubscriptionBanner)
	r.POST("/subscription-banner", h.Auth.AugmentLegacySubscriptionBanner)
	r.POST("/report-error", h.Auth.AugmentLegacyJSONAck)
	r.POST("/report-feature-vector", h.Auth.AugmentLegacyJSONAck)
	r.POST("/client-metrics", h.Auth.AugmentLegacyJSONAck)
	r.POST("/record-session-events", h.Auth.AugmentLegacyJSONAck)
	r.POST("/record-request-events", h.Auth.AugmentLegacyJSONAck)
	r.POST("/record-user-events", h.Auth.AugmentLegacyJSONAck)
	r.POST("/record-preference-sample", h.Auth.AugmentLegacyJSONAck)
	r.POST("/client-completion-timelines", h.Auth.AugmentLegacyJSONAck)
	r.POST("/chat-feedback", h.Auth.AugmentLegacyJSONAck)
	r.POST("/completion-feedback", h.Auth.AugmentLegacyJSONAck)
	r.POST("/next-edit-feedback", h.Auth.AugmentLegacyJSONAck)
	r.POST("/resolve-completions", h.Auth.AugmentLegacyJSONAck)
	r.POST("/resolve-chat-input-completion", h.Auth.AugmentLegacyJSONAck)
	r.POST("/resolve-edit", h.Auth.AugmentLegacyJSONAck)
	r.POST("/resolve-instruction", h.Auth.AugmentLegacyJSONAck)
	r.POST("/resolve-next-edit", h.Auth.AugmentLegacyJSONAck)
	r.POST("/resolve-smart-paste", h.Auth.AugmentLegacyJSONAck)

	bodyLimit := middleware.RequestBodyLimit(cfg.Gateway.MaxBodySize)
	clientRequestID := middleware.ClientRequestID()
	opsErrorLogger := handler.OpsErrorLoggerMiddleware(opsService)
	endpointNorm := handler.InboundEndpointMiddleware()

	// 未分组 Key 拦截中间件（按协议格式区分错误响应）
	requireGroupAnthropic := middleware.RequireGroupAssignment(settingService, middleware.AnthropicErrorWriter)
	requireGroupOpenAI := middleware.RequireGroupAssignment(settingService, middleware.OpenAIErrorWriter)
	requireGroupGoogle := middleware.RequireGroupAssignment(settingService, middleware.GoogleErrorWriter)
	managedOrAPIKeyAuth := middleware.ManagedDeviceOrAPIKeyAuth(codexAgentService, apiKeyAuth, apiKeyService, subscriptionService, cfg)

	// API网关（Claude API兼容）
	gateway := r.Group("/v1")
	gateway.Use(bodyLimit)
	gateway.Use(clientRequestID)
	gateway.Use(opsErrorLogger)
	gateway.Use(endpointNorm)
	{
		// /v1/messages: auto-route based on group platform
		gateway.POST("/messages", gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, func(c *gin.Context) {
			if getGroupPlatform(c) == service.PlatformOpenAI {
				h.OpenAIGateway.Messages(c)
				return
			}
			h.Gateway.Messages(c)
		})
		// /v1/messages/count_tokens: OpenAI groups get 404
		gateway.POST("/messages/count_tokens", gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, func(c *gin.Context) {
			if getGroupPlatform(c) == service.PlatformOpenAI {
				c.JSON(http.StatusNotFound, gin.H{
					"type": "error",
					"error": gin.H{
						"type":    "not_found_error",
						"message": "Token counting is not supported for this platform",
					},
				})
				return
			}
			h.Gateway.CountTokens(c)
		})
		gateway.GET("/models", managedOrAPIKeyAuth, requireGroupAnthropic, h.Gateway.Models)
		gateway.GET("/usage", gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, h.Gateway.Usage)
		// OpenAI Responses API: auto-route based on group platform
		gateway.POST("/responses", managedOrAPIKeyAuth, requireGroupOpenAI, func(c *gin.Context) {
			if getGroupPlatform(c) == service.PlatformOpenAI {
				h.OpenAIGateway.Responses(c)
				return
			}
			h.Gateway.Responses(c)
		})
		gateway.POST("/responses/*subpath", managedOrAPIKeyAuth, requireGroupOpenAI, func(c *gin.Context) {
			if getGroupPlatform(c) == service.PlatformOpenAI {
				h.OpenAIGateway.Responses(c)
				return
			}
			h.Gateway.Responses(c)
		})
		gateway.GET("/responses", managedOrAPIKeyAuth, requireGroupOpenAI, h.OpenAIGateway.ResponsesWebSocket)
		// OpenAI Chat Completions API: auto-route based on group platform
		gateway.POST("/chat/completions", managedOrAPIKeyAuth, requireGroupOpenAI, func(c *gin.Context) {
			if getGroupPlatform(c) == service.PlatformOpenAI {
				h.OpenAIGateway.ChatCompletions(c)
				return
			}
			h.Gateway.ChatCompletions(c)
		})
	}

	r.POST("/v1/images/generations", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupOpenAI, h.OpenAIGateway.ImageGenerations)
	r.POST("/images/generations", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupOpenAI, h.OpenAIGateway.ImageGenerations)

	// Gemini 原生 API 兼容层（Gemini SDK/CLI 直连）
	gemini := r.Group("/v1beta")
	gemini.Use(bodyLimit)
	gemini.Use(clientRequestID)
	gemini.Use(opsErrorLogger)
	gemini.Use(endpointNorm)
	gemini.Use(middleware.APIKeyAuthWithSubscriptionGoogle(apiKeyService, subscriptionService, cfg))
	gemini.Use(requireGroupGoogle)
	{
		gemini.GET("/models", h.Gateway.GeminiV1BetaListModels)
		gemini.GET("/models/:model", h.Gateway.GeminiV1BetaGetModel)
		// Gin treats ":" as a param marker, but Gemini uses "{model}:{action}" in the same segment.
		gemini.POST("/models/*modelAction", h.Gateway.GeminiV1BetaModels)
	}

	// OpenAI Responses API（不带v1前缀的别名）— auto-route based on group platform
	responsesHandler := func(c *gin.Context) {
		if getGroupPlatform(c) == service.PlatformOpenAI {
			h.OpenAIGateway.Responses(c)
			return
		}
		h.Gateway.Responses(c)
	}
	r.POST("/responses", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, managedOrAPIKeyAuth, requireGroupOpenAI, responsesHandler)
	r.POST("/responses/*subpath", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, managedOrAPIKeyAuth, requireGroupOpenAI, responsesHandler)
	r.GET("/responses", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, managedOrAPIKeyAuth, requireGroupOpenAI, h.OpenAIGateway.ResponsesWebSocket)
	// OpenAI Chat Completions API（不带v1前缀的别名）— auto-route based on group platform
	r.POST("/chat/completions", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, managedOrAPIKeyAuth, requireGroupOpenAI, func(c *gin.Context) {
		if getGroupPlatform(c) == service.PlatformOpenAI {
			h.OpenAIGateway.ChatCompletions(c)
			return
		}
		h.Gateway.ChatCompletions(c)
	})

	// OpenAI Gateway Core 显式前缀入口（供 OpenAI/Codex 客户端直连）
	openaiGateway := r.Group("/openai/v1")
	openaiGateway.Use(bodyLimit)
	openaiGateway.Use(clientRequestID)
	openaiGateway.Use(opsErrorLogger)
	openaiGateway.Use(endpointNorm)
	{
		openaiGateway.POST("/responses", managedOrAPIKeyAuth, requireGroupOpenAI, h.OpenAIGateway.Responses)
		openaiGateway.POST("/responses/*subpath", managedOrAPIKeyAuth, requireGroupOpenAI, h.OpenAIGateway.Responses)
		openaiGateway.GET("/responses", managedOrAPIKeyAuth, requireGroupOpenAI, h.OpenAIGateway.ResponsesWebSocket)
		openaiGateway.POST("/chat/completions", managedOrAPIKeyAuth, requireGroupOpenAI, h.OpenAIGateway.ChatCompletions)
	}
	r.POST("/openai/v1/images/generations", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupOpenAI, h.OpenAIGateway.ImageGenerations)

	r.GET("/openai/_health", h.OpenAIGateway.Health)
	r.GET("/openai/_verify", h.OpenAIGateway.Verify)

	// Antigravity 模型列表
	r.GET("/antigravity/models", gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, h.Gateway.AntigravityModels)

	// Antigravity 专用路由（仅使用 antigravity 账户，不混合调度）
	antigravityV1 := r.Group("/antigravity/v1")
	antigravityV1.Use(bodyLimit)
	antigravityV1.Use(clientRequestID)
	antigravityV1.Use(opsErrorLogger)
	antigravityV1.Use(endpointNorm)
	antigravityV1.Use(middleware.ForcePlatform(service.PlatformAntigravity))
	antigravityV1.Use(gin.HandlerFunc(apiKeyAuth))
	antigravityV1.Use(requireGroupAnthropic)
	{
		antigravityV1.POST("/messages", h.Gateway.Messages)
		antigravityV1.POST("/messages/count_tokens", h.Gateway.CountTokens)
		antigravityV1.GET("/models", h.Gateway.AntigravityModels)
		antigravityV1.GET("/usage", h.Gateway.Usage)
	}

	antigravityV1Beta := r.Group("/antigravity/v1beta")
	antigravityV1Beta.Use(bodyLimit)
	antigravityV1Beta.Use(clientRequestID)
	antigravityV1Beta.Use(opsErrorLogger)
	antigravityV1Beta.Use(endpointNorm)
	antigravityV1Beta.Use(middleware.ForcePlatform(service.PlatformAntigravity))
	antigravityV1Beta.Use(middleware.APIKeyAuthWithSubscriptionGoogle(apiKeyService, subscriptionService, cfg))
	antigravityV1Beta.Use(requireGroupGoogle)
	{
		antigravityV1Beta.GET("/models", h.Gateway.GeminiV1BetaListModels)
		antigravityV1Beta.GET("/models/:model", h.Gateway.GeminiV1BetaGetModel)
		antigravityV1Beta.POST("/models/*modelAction", h.Gateway.GeminiV1BetaModels)
	}

}

// getGroupPlatform extracts the group platform from the API Key stored in context.
func getGroupPlatform(c *gin.Context) string {
	apiKey, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || apiKey.Group == nil {
		return ""
	}
	return apiKey.Group.Platform
}
