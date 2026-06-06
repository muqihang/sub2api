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
	case "", "anthropic_direct", "kiro_claude", "kiro_claude_thinking":
		return true
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

func codexGatewayInt64Ptr(v int64) *int64 {
	return &v
}
