package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexGatewayAdminService_ListModels_UsesVariantCheckerForClaudeVisibility(t *testing.T) {
	svc := NewCodexGatewayAdminService(config.GatewayCodexConfig{
		EnabledModels: []string{
			"claude-opus-4-7",
			"claude-opus-4-7-thinking",
			"claude-opus-4-7-ag",
			"claude-opus-4-7-thinking-ag",
			"claude-opus-4-7-max",
		},
	}, nil)

	svc.providerGroups[CodexGatewayProviderAnthropic] = CodexGatewayProviderRuntime{
		Provider: CodexGatewayProviderAnthropic,
		GroupID:  3003,
		Healthy:  true,
	}
	svc.models["claude-opus-4-7"] = CodexGatewayModelMutation{Enabled: true}
	svc.models["claude-opus-4-7-thinking"] = CodexGatewayModelMutation{Enabled: true}
	svc.models["claude-opus-4-7-ag"] = CodexGatewayModelMutation{Enabled: true}
	svc.models["claude-opus-4-7-thinking-ag"] = CodexGatewayModelMutation{Enabled: true}
	svc.models["claude-opus-4-7-max"] = CodexGatewayModelMutation{Enabled: true}
	svc.variantChecker = codexGatewayVariantReadyCheckerStub{
		ready: map[string]bool{
			"claude-opus-4-7-ag":          false,
			"claude-opus-4-7-thinking-ag": false,
			"claude-opus-4-7-max":         false,
		},
	}

	rows, err := svc.ListModels(context.Background())
	require.NoError(t, err)

	visible := make(map[string]bool, len(rows))
	for _, row := range rows {
		visible[row.Model.Slug] = row.Visible
	}

	require.True(t, visible["claude-opus-4-7"])
	require.True(t, visible["claude-opus-4-7-thinking"])
	require.False(t, visible["claude-opus-4-7-ag"])
	require.False(t, visible["claude-opus-4-7-thinking-ag"])
	require.False(t, visible["claude-opus-4-7-max"])
}
