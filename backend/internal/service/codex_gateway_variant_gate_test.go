package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexGatewayAnthropicVariantReadyChecker_DirectRequiresBaseAndThinkingUpstreams(t *testing.T) {
	model := CodexGatewayModel{
		Slug:                  "claude-opus-4-8",
		Provider:              "anthropic",
		ProviderVariant:       "anthropic_direct",
		UpstreamModel:         "claude-opus-4-8",
		UpstreamBaseModel:     "claude-opus-4-8",
		UpstreamThinkingModel: "claude-opus-4-8-thinking",
	}
	runtime := CodexGatewayProviderRuntime{Provider: CodexGatewayProviderAnthropic, GroupID: 3003, Healthy: true}

	readyProbe := &codexGatewayAnthropicVariantProbeStub{supported: map[string]bool{
		"claude-opus-4-8":          true,
		"claude-opus-4-8-thinking": true,
	}}
	checker := newCodexGatewayAnthropicVariantReadyChecker(readyProbe)
	require.True(t, checker.IsReady(context.Background(), model, runtime))
	require.Equal(t, []string{"claude-opus-4-8", "claude-opus-4-8-thinking"}, readyProbe.calls)

	missingThinking := &codexGatewayAnthropicVariantProbeStub{supported: map[string]bool{
		"claude-opus-4-8": true,
	}}
	checker = newCodexGatewayAnthropicVariantReadyChecker(missingThinking)
	require.False(t, checker.IsReady(context.Background(), model, runtime))
	require.Equal(t, []string{"claude-opus-4-8", "claude-opus-4-8-thinking"}, missingThinking.calls)
}

func TestCodexGatewayAnthropicVariantReadyChecker_KeepsLegacyKiroUngated(t *testing.T) {
	probe := &codexGatewayAnthropicVariantProbeStub{}
	checker := newCodexGatewayAnthropicVariantReadyChecker(probe)
	model := CodexGatewayModel{
		Slug:            "claude-opus-4-6",
		Provider:        "anthropic",
		ProviderVariant: "kiro_claude",
		UpstreamModel:   "claude-opus-4-6",
	}
	runtime := CodexGatewayProviderRuntime{Provider: CodexGatewayProviderAnthropic, GroupID: 3003, Healthy: true}

	require.True(t, checker.IsReady(context.Background(), model, runtime))
	require.Empty(t, probe.calls)
}

type codexGatewayAnthropicVariantProbeStub struct {
	supported map[string]bool
	calls     []string
}

func (s *codexGatewayAnthropicVariantProbeStub) SelectAccountForModelWithExclusions(_ context.Context, groupID *int64, _ string, requestedModel string, _ map[int64]struct{}) (*Account, error) {
	if groupID == nil || *groupID <= 0 {
		return nil, ErrNoAvailableAccounts
	}
	s.calls = append(s.calls, requestedModel)
	if s.supported[requestedModel] {
		return &Account{ID: 128, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}, nil
	}
	return nil, ErrNoAvailableAccounts
}
