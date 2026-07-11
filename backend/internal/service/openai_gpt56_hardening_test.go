package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/stretchr/testify/require"
)

func TestCopyOpenAIUsageFromResponsesUsageTrustsCanonicalCacheCreationValue(t *testing.T) {
	usage := &apicompat.ResponsesUsage{
		InputTokens:              20,
		OutputTokens:             2,
		CacheCreationInputTokens: 0,
		InputTokensDetails: &apicompat.ResponsesInputTokensDetails{
			CachedTokens:     3,
			CacheWriteTokens: 19,
		},
	}

	got := copyOpenAIUsageFromResponsesUsage(usage)
	require.Equal(t, 20, got.InputTokens)
	require.Equal(t, 3, got.CacheReadInputTokens)
	require.Zero(t, got.CacheCreationInputTokens)
}

func TestGPT56AliasKeepsRequestedBillingCandidateFirst(t *testing.T) {
	require.Equal(t, "gpt-5.6-sol", normalizeKnownOpenAICodexModel("gpt-5.6"))
	require.Equal(t, []string{"gpt-5.6", "gpt-5.6-sol"}, usageBillingModelCandidates("gpt-5.6"))
}

func TestBillingServiceGPT56HasLongContextPolicy(t *testing.T) {
	service := NewBillingService(&config.Config{}, nil)
	pricing, err := service.GetModelPricing("gpt-5.6-sol")

	require.NoError(t, err)
	require.Equal(t, 272000, pricing.LongContextInputThreshold)
	require.InDelta(t, 2.0, pricing.LongContextInputMultiplier, 1e-12)
	require.InDelta(t, 1.5, pricing.LongContextOutputMultiplier, 1e-12)
}
