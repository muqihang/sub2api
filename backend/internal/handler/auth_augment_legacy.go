package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type augmentLegacyBalanceLoginToken struct {
	TenantURL     string `json:"tenantUrl"`
	AccessToken   string `json:"accessToken"`
	SessionSource string `json:"sessionSource,omitempty"`
}

type augmentLegacyBalanceData struct {
	RemainAmount float64                         `json:"remain_amount"`
	Name         string                          `json:"name,omitempty"`
	Unlimited    bool                            `json:"unlimited,omitempty"`
	StatusText   string                          `json:"status_text,omitempty"`
	ExpiredTime  int64                           `json:"expired_time,omitempty"`
	LoginToken   *augmentLegacyBalanceLoginToken `json:"login_token,omitempty"`
}

type augmentLegacyInternalModel struct {
	Name                     string `json:"name"`
	SuggestedPrefixCharCount int    `json:"suggested_prefix_char_count"`
	SuggestedSuffixCharCount int    `json:"suggested_suffix_char_count"`
	CompletionTimeoutMS      int    `json:"completion_timeout_ms"`
}

type augmentLegacyCheckpointBlobsPayload struct {
	CheckpointID string   `json:"checkpoint_id"`
	AddedBlobs   []string `json:"added_blobs"`
	DeletedBlobs []string `json:"deleted_blobs"`
}

type augmentLegacyCheckpointBlobsRequest struct {
	Blobs augmentLegacyCheckpointBlobsPayload `json:"blobs"`
}

func (h *AuthHandler) AugmentLegacyBalance(c *gin.Context) {
	if h.augmentPluginService == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "augment plugin service is unavailable"})
		return
	}

	principal, ok := h.augmentLegacyOfficialRoutePrincipal(c, "/checkpoint-blobs")
	if !ok {
		return
	}

	summary, err := h.augmentPluginService.BuildSummary(c.Request.Context(), *principal)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	loginToken, err := h.augmentLegacyResolvedLoginToken(c.Request.Context(), principal, h.augmentTenantURL(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	data := augmentLegacyBalanceData{}
	if summary != nil && summary.User != nil {
		data.RemainAmount = summary.User.Balance
		if summary.User.Email != "" {
			data.Name = summary.User.Email
		} else {
			data.Name = summary.User.Username
		}
		if summary.User.Status != "" {
			data.StatusText = summary.User.Status
		}
	}
	if summary != nil && summary.Plan != nil {
		data.Unlimited = summary.Plan.ActiveCount > 0
		if summary.Plan.ExpiresAt != nil && *summary.Plan.ExpiresAt != "" {
			if parsed, parseErr := time.Parse(time.RFC3339, *summary.Plan.ExpiresAt); parseErr == nil {
				data.ExpiredTime = parsed.Unix()
			}
		}
	}
	if loginToken != nil && loginToken.TenantURL != "" && loginToken.AccessToken != "" {
		data.LoginToken = &augmentLegacyBalanceLoginToken{
			TenantURL:     loginToken.TenantURL,
			AccessToken:   loginToken.AccessToken,
			SessionSource: loginToken.SessionSource,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    data,
	})
}

func (h *AuthHandler) AugmentLegacyModels(c *gin.Context) {
	if h.augmentPluginService == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "augment plugin service is unavailable"})
		return
	}

	if _, ok := h.augmentPrincipalFromBearer(c); !ok {
		return
	}

	visibleModels := h.augmentGatewayModelRegistry().VisibleModels()
	defaultModel := augmentGatewayDefaultModelID(visibleModels)
	models := map[string]map[string]any{}
	for index, model := range visibleModels {
		entry := map[string]any{
			"displayName": model.ID,
			"description": "",
			"priority":    index,
		}
		if model.ID == defaultModel {
			entry["isDefault"] = true
		}
		models[model.ID] = entry
	}

	if len(models) == 0 {
		c.JSON(http.StatusOK, map[string]any{})
		return
	}

	c.JSON(http.StatusOK, models)
}

func (h *AuthHandler) AugmentLegacyLoginToken(c *gin.Context) {
	if h.augmentPluginService == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "augment plugin service is unavailable"})
		return
	}

	principal, ok := h.augmentPrincipalFromBearer(c)
	if !ok {
		return
	}

	loginToken, err := h.augmentLegacyResolvedLoginToken(c.Request.Context(), principal, h.augmentTenantURL(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, loginToken)
}

func (h *AuthHandler) augmentLegacyResolvedLoginToken(
	ctx context.Context,
	principal *service.AugmentPluginPrincipal,
	tenantURL string,
) (*service.AugmentLegacyLoginToken, error) {
	if principal != nil {
		if bundle, release, err := h.augmentLegacyOfficialSessionBundleForExecution(ctx, principal); err == nil && bundle != nil {
			release(true, "")
			return &service.AugmentLegacyLoginToken{
				TenantURL:     bundle.TenantURL,
				AccessToken:   bundle.AccessToken,
				SessionSource: service.AugmentSessionSourceOfficial,
			}, nil
		}
	}
	return h.augmentPluginService.IssueLegacyLoginToken(ctx, *principal, tenantURL)
}

func (h *AuthHandler) AugmentLegacyInternalGetModels(c *gin.Context) {
	if h.augmentPluginService == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "augment plugin service is unavailable"})
		return
	}

	if _, ok := h.augmentPrincipalFromBearer(c); !ok {
		return
	}

	visibleModels := h.augmentGatewayModelRegistry().VisibleModels()
	defaultModel := augmentGatewayDefaultModelID(visibleModels)
	models := make([]augmentLegacyInternalModel, 0, len(visibleModels))
	registry := make(map[string]string, len(visibleModels))
	infoRegistry := make(map[string]map[string]any, len(visibleModels))
	for index, model := range visibleModels {
		displayName := augmentGatewayModelDisplayName(model.ID)
		models = append(models, augmentLegacyInternalModel{
			Name:                     model.ID,
			SuggestedPrefixCharCount: 0,
			SuggestedSuffixCharCount: 0,
			CompletionTimeoutMS:      120000,
		})
		registry[displayName] = model.ID
		infoRegistry[model.ID] = map[string]any{
			"description": "",
			"disabled":    false,
			"displayName": displayName,
			"shortName":   augmentGatewayModelShortName(model.ID),
			"priority":    index,
		}
	}
	registryJSON, _ := json.Marshal(registry)
	infoRegistryJSON, _ := json.Marshal(infoRegistry)

	featureFlags := gin.H{
		"additional_chat_models": string(registryJSON),
		"additionalChatModels":   string(registryJSON),
		"agent_chat_model":       defaultModel,
		"agentChatModel":         defaultModel,
		"vscode_agent_mode_min_version":        "1.96.0",
		"vscodeAgentModeMinVersion":            "1.96.0",
		"vscode_agent_mode_min_stable_version": "1.96.0",
		"vscodeAgentModeMinStableVersion":      "1.96.0",
		"vscode_chat_with_tools_min_version":   "1.96.0",
		"vscodeChatWithToolsMinVersion":        "1.96.0",
		"enable_agent_auto_mode":              true,
		"enableAgentAutoMode":                 true,
		"ide_enable_ask_user_tool":            true,
		"ideEnableAskUserTool":                true,
		"enable_model_registry":  true,
		"enableModelRegistry":    true,
		"model_registry":         string(registryJSON),
		"modelRegistry":          string(registryJSON),
		"model_info_registry":    string(infoRegistryJSON),
		"modelInfoRegistry":      string(infoRegistryJSON),
		"show_thinking_summary":  true,
		"showThinkingSummary":    true,
		"fraud_sign_endpoints":   false,
		"fraudSignEndpoints":     false,
	}

	c.JSON(http.StatusOK, gin.H{
		"default_model": defaultModel,
		"models":        models,
		"feature_flags": featureFlags,
	})
}

func (h *AuthHandler) augmentGatewayModelRegistry() *service.AugmentGatewayModelRegistry {
	if h != nil && h.augmentGatewayService != nil {
		if registry := h.augmentGatewayService.Registry(); registry != nil {
			return registry
		}
	}
	if h == nil || h.cfg == nil {
		return service.NewDefaultAugmentGatewayModelRegistry()
	}
	cfg := h.cfg.Gateway.Augment
	if !cfg.Enabled &&
		len(cfg.EnabledModels) == 0 &&
		cfg.ProviderGroups.OpenAI == 0 &&
		cfg.ProviderGroups.DeepSeek == 0 &&
		cfg.ProviderGroups.Anthropic == 0 &&
		cfg.ProviderGroups.Gemini == 0 {
		return service.NewDefaultAugmentGatewayModelRegistry()
	}
	return service.NewAugmentGatewayModelRegistry(cfg)
}

func augmentGatewayDefaultModelID(models []service.AugmentGatewayModel) string {
	if len(models) == 0 {
		return ""
	}
	return models[0].ID
}

func augmentGatewayModelDisplayName(modelID string) string {
	switch modelID {
	case "gpt-5.4":
		return "GPT-5.4"
	case "gpt-5.5":
		return "GPT-5.5"
	case "gpt-5.4-mini":
		return "GPT-5.4 Mini"
	case "deepseek-v4-pro":
		return "DeepSeek V4 Pro"
	case "deepseek-v4-flash":
		return "DeepSeek V4 Flash"
	case "claude-sonnet-4-5":
		return "Claude Sonnet 4.5"
	case "gemini-2.5-pro":
		return "Gemini 2.5 Pro"
	default:
		return modelID
	}
}

func augmentGatewayModelShortName(modelID string) string {
	switch modelID {
	case "gpt-5.4-mini":
		return "GPT Mini"
	case "deepseek-v4-pro":
		return "DeepSeek Pro"
	case "deepseek-v4-flash":
		return "DeepSeek Flash"
	case "claude-sonnet-4-5":
		return "Sonnet 4.5"
	case "gemini-2.5-pro":
		return "Gemini Pro"
	default:
		return augmentGatewayModelDisplayName(modelID)
	}
}

func (h *AuthHandler) AugmentLegacyCheckpointBlobs(c *gin.Context) {
	if h.augmentPluginService == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "augment plugin service is unavailable"})
		return
	}

	principal, ok := h.augmentPrincipalFromBearer(c)
	if !ok {
		return
	}

	var req augmentLegacyCheckpointBlobsRequest
	if !augmentLegacyDecodeRequest(c, &req) {
		return
	}

	namespace := h.augmentLegacyNamespace(c, principal)
	newCheckpointID, err := h.augmentPluginService.AdvanceLegacyCheckpointForNamespace(
		namespace,
		req.Blobs.CheckpointID,
		req.Blobs.AddedBlobs,
		req.Blobs.DeletedBlobs,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	augmentLegacyTrace(c, "checkpoint_blobs", "namespace", namespace, "checkpoint_id", req.Blobs.CheckpointID, "added_blobs", len(req.Blobs.AddedBlobs), "deleted_blobs", len(req.Blobs.DeletedBlobs))
	c.JSON(http.StatusOK, gin.H{
		"new_checkpoint_id": newCheckpointID,
	})
}

func (h *AuthHandler) AugmentLegacyNoContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}
