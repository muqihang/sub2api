package service

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

type codexGatewayVariantReadyAllowAll struct{}

func (codexGatewayVariantReadyAllowAll) IsReady(context.Context, CodexGatewayModel, CodexGatewayProviderRuntime) bool {
	return true
}

type codexGatewayAnthropicVariantAvailabilityProbe interface {
	SelectAccountForModelWithExclusions(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}) (*Account, error)
}

type codexGatewayAnthropicVariantReadyChecker struct {
	probe codexGatewayAnthropicVariantAvailabilityProbe
}

func newCodexGatewayAnthropicVariantReadyChecker(probe codexGatewayAnthropicVariantAvailabilityProbe) CodexGatewayVariantReadyChecker {
	if probe == nil {
		return codexGatewayVariantReadyAllowAll{}
	}
	return codexGatewayAnthropicVariantReadyChecker{probe: probe}
}

func (c codexGatewayAnthropicVariantReadyChecker) IsReady(ctx context.Context, model CodexGatewayModel, providerRuntime CodexGatewayProviderRuntime) bool {
	if normalizeCodexGatewayProvider(CodexGatewayProvider(model.Provider)) != CodexGatewayProviderAnthropic {
		return true
	}
	if providerRuntime.GroupID <= 0 || !providerRuntime.Healthy {
		return false
	}
	variant := strings.TrimSpace(model.ProviderVariant)
	switch variant {
	case "", "kiro_claude", "kiro_claude_thinking":
		return true
	case "anthropic_direct":
		return c.allAnthropicDirectUpstreamsReady(ctx, model, providerRuntime)
	case "antigravity_claude", "antigravity_claude_thinking":
		if c.probe == nil {
			return false
		}
		probeCtx := context.WithValue(ctx, ctxkey.ForcePlatform, PlatformAntigravity)
		requestedModel := strings.TrimSpace(model.UpstreamModel)
		if requestedModel == "" {
			requestedModel = strings.TrimSpace(model.Slug)
		}
		account, err := c.probe.SelectAccountForModelWithExclusions(probeCtx, codexGatewayInt64Ptr(providerRuntime.GroupID), "", requestedModel, nil)
		return err == nil && account != nil
	case "claude_code_max":
		return false
	default:
		return false
	}
}

func (c codexGatewayAnthropicVariantReadyChecker) allAnthropicDirectUpstreamsReady(ctx context.Context, model CodexGatewayModel, providerRuntime CodexGatewayProviderRuntime) bool {
	if c.probe == nil {
		return false
	}
	for _, requestedModel := range codexGatewayAnthropicVariantUpstreamModels(model) {
		account, err := c.probe.SelectAccountForModelWithExclusions(ctx, codexGatewayInt64Ptr(providerRuntime.GroupID), "", requestedModel, nil)
		if err != nil || account == nil {
			return false
		}
	}
	return true
}

func codexGatewayAnthropicVariantUpstreamModels(model CodexGatewayModel) []string {
	base := strings.TrimSpace(model.UpstreamBaseModel)
	if base == "" {
		base = strings.TrimSpace(model.UpstreamModel)
	}
	if base == "" {
		base = strings.TrimSpace(model.Slug)
	}
	models := []string{}
	if base != "" {
		models = append(models, base)
	}
	thinking := strings.TrimSpace(model.UpstreamThinkingModel)
	if thinking != "" && thinking != base && codexGatewayAnthropicModelSupportsAdaptiveThinking(model) {
		models = append(models, thinking)
	}
	return models
}

func codexGatewayInt64Ptr(v int64) *int64 {
	return &v
}
