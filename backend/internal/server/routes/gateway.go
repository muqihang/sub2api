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
	cfg *config.Config,
) {
	bodyLimit := middleware.RequestBodyLimit(cfg.Gateway.MaxBodySize)
	clientRequestID := middleware.ClientRequestID()
	opsErrorLogger := handler.OpsErrorLoggerMiddleware(opsService)
	endpointNorm := handler.InboundEndpointMiddleware()

	augmentCompat := r.Group("")
	augmentCompat.Use(bodyLimit)
	augmentCompat.Use(clientRequestID)
	augmentCompat.Use(opsErrorLogger)
	augmentCompat.Use(endpointNorm)
	{
		augmentCompat.GET("/usage/api/balance", h.Auth.AugmentLegacyBalance)
		augmentCompat.GET("/usage/api/get-models", h.Auth.AugmentLegacyModels)
		augmentCompat.GET("/usage/api/getLoginToken", h.Auth.AugmentLegacyLoginToken)
		augmentCompat.POST("/get-models", h.Auth.AugmentLegacyInternalGetModels)
		augmentCompat.POST("/batch-upload", h.Auth.AugmentLegacyBatchUpload)
		augmentCompat.POST("/checkpoint-blobs", h.Auth.AugmentLegacyCheckpointBlobs)
		augmentCompat.POST("/find-missing", h.Auth.AugmentLegacyFindMissing)
		augmentCompat.POST("/save-chat", h.Auth.AugmentLegacySaveChat)
		augmentCompat.POST("/chat", h.Auth.AugmentLegacyChat)
		augmentCompat.POST("/chat-stream", h.Auth.AugmentLegacyChatStream)
		augmentCompat.POST("/prompt-enhancer", h.Auth.AugmentLegacyPromptEnhancer)
		augmentCompat.POST("/instruction-stream", h.Auth.AugmentLegacyInstructionStream)
		augmentCompat.POST("/smart-paste-stream", h.Auth.AugmentLegacySmartPasteStream)
		augmentCompat.POST("/generate-commit-message-stream", h.Auth.AugmentLegacyGenerateCommitMessageStream)
		augmentCompat.POST("/next_edit_loc", h.Auth.AugmentLegacyNextEditLocation)
		augmentCompat.POST("/next-edit-stream", h.Auth.AugmentLegacyNextEditStream)
		augmentCompat.POST("/remote-agents/list", h.Auth.AugmentLegacyListRemoteAgents)
		augmentCompat.POST("/agents/codebase-retrieval", h.Auth.AugmentLegacyCodebaseRetrieval)
		augmentCompat.POST("/agents/list-remote-tools", h.Auth.AugmentLegacyListRemoteTools)
		augmentCompat.POST("/get-implicit-external-sources", h.Auth.AugmentLegacyGetImplicitExternalSources)
		augmentCompat.POST("/search-external-sources", h.Auth.AugmentLegacySearchExternalSources)
		augmentCompat.POST("/context-canvas/list", h.Auth.AugmentLegacyContextCanvasList)
		augmentCompat.GET("/notifications/read", h.Auth.AugmentLegacyNotificationsRead)
		augmentCompat.POST("/notifications/read", h.Auth.AugmentLegacyNotificationsRead)
		augmentCompat.POST("/notifications/mark-as-read", h.Auth.AugmentLegacyNotificationsMarkRead)
		augmentCompat.GET("/subscription-banner", h.Auth.AugmentLegacySubscriptionBanner)
		augmentCompat.POST("/subscription-banner", h.Auth.AugmentLegacySubscriptionBanner)
		augmentCompat.POST("/report-error", h.Auth.AugmentLegacyJSONAck)
		augmentCompat.POST("/report-feature-vector", h.Auth.AugmentLegacyJSONAck)
		augmentCompat.POST("/client-metrics", h.Auth.AugmentLegacyJSONAck)
		augmentCompat.POST("/record-session-events", h.Auth.AugmentLegacyJSONAck)
		augmentCompat.POST("/record-request-events", h.Auth.AugmentLegacyJSONAck)
		augmentCompat.POST("/record-user-events", h.Auth.AugmentLegacyJSONAck)
		augmentCompat.POST("/record-preference-sample", h.Auth.AugmentLegacyJSONAck)
		augmentCompat.POST("/client-completion-timelines", h.Auth.AugmentLegacyJSONAck)
		augmentCompat.POST("/chat-feedback", h.Auth.AugmentLegacyJSONAck)
		augmentCompat.POST("/completion-feedback", h.Auth.AugmentLegacyJSONAck)
		augmentCompat.POST("/next-edit-feedback", h.Auth.AugmentLegacyJSONAck)
		augmentCompat.POST("/resolve-completions", h.Auth.AugmentLegacyJSONAck)
		augmentCompat.POST("/resolve-chat-input-completion", h.Auth.AugmentLegacyJSONAck)
		augmentCompat.POST("/resolve-edit", h.Auth.AugmentLegacyJSONAck)
		augmentCompat.POST("/resolve-instruction", h.Auth.AugmentLegacyJSONAck)
		augmentCompat.POST("/resolve-next-edit", h.Auth.AugmentLegacyJSONAck)
		augmentCompat.POST("/resolve-smart-paste", h.Auth.AugmentLegacyJSONAck)
	}

	// 未分组 Key 拦截中间件（按协议格式区分错误响应）
	requireGroupAnthropic := middleware.RequireGroupAssignment(settingService, middleware.AnthropicErrorWriter)
	requireGroupGoogle := middleware.RequireGroupAssignment(settingService, middleware.GoogleErrorWriter)

	// API网关（Claude API兼容）
	gateway := r.Group("/v1")
	gateway.Use(bodyLimit)
	gateway.Use(clientRequestID)
	gateway.Use(opsErrorLogger)
	gateway.Use(endpointNorm)
	gateway.Use(gin.HandlerFunc(apiKeyAuth))
	gateway.Use(requireGroupAnthropic)
	{
		// /v1/messages: auto-route based on group platform
		gateway.POST("/messages", func(c *gin.Context) {
			if getGroupPlatform(c) == service.PlatformOpenAI {
				h.OpenAIGateway.Messages(c)
				return
			}
			h.Gateway.Messages(c)
		})
		// /v1/messages/count_tokens: OpenAI groups get 404
		gateway.POST("/messages/count_tokens", func(c *gin.Context) {
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
		gateway.GET("/models", h.Gateway.Models)
		gateway.GET("/usage", h.Gateway.Usage)
		// OpenAI Responses API: auto-route based on group platform
		gateway.POST("/responses", func(c *gin.Context) {
			if getGroupPlatform(c) == service.PlatformOpenAI {
				h.OpenAIGateway.Responses(c)
				return
			}
			h.Gateway.Responses(c)
		})
		gateway.POST("/responses/*subpath", func(c *gin.Context) {
			if getGroupPlatform(c) == service.PlatformOpenAI {
				h.OpenAIGateway.Responses(c)
				return
			}
			h.Gateway.Responses(c)
		})
		gateway.GET("/responses", h.OpenAIGateway.ResponsesWebSocket)
		// OpenAI Chat Completions API: auto-route based on group platform
		gateway.POST("/chat/completions", func(c *gin.Context) {
			if getGroupPlatform(c) == service.PlatformOpenAI {
				h.OpenAIGateway.ChatCompletions(c)
				return
			}
			h.Gateway.ChatCompletions(c)
		})
	}

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
	r.POST("/responses", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, responsesHandler)
	r.POST("/responses/*subpath", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, responsesHandler)
	r.GET("/responses", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, h.OpenAIGateway.ResponsesWebSocket)
	// OpenAI Chat Completions API（不带v1前缀的别名）— auto-route based on group platform
	r.POST("/chat/completions", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, func(c *gin.Context) {
		if getGroupPlatform(c) == service.PlatformOpenAI {
			h.OpenAIGateway.ChatCompletions(c)
			return
		}
		h.Gateway.ChatCompletions(c)
	})
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
