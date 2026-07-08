package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestBillingServiceOpenAINewModelFallbacks(t *testing.T) {
	svc := NewBillingService(&config.Config{}, nil)

	for _, model := range []string{"gpt-5.5-pro", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"} {
		t.Run(model, func(t *testing.T) {
			pricing, err := svc.GetModelPricing(model)
			require.NoError(t, err)
			require.NotNil(t, pricing)
			require.InDelta(t, 2.5e-6, pricing.InputPricePerToken, 1e-12)
			require.InDelta(t, 15e-6, pricing.OutputPricePerToken, 1e-12)
			require.InDelta(t, 0.25e-6, pricing.CacheReadPricePerToken, 1e-12)
			require.Equal(t, 272000, pricing.LongContextInputThreshold)
		})
	}
}

func TestCalculateCostOpenAINewModelLongContext(t *testing.T) {
	svc := NewBillingService(&config.Config{}, nil)
	tokens := UsageTokens{InputTokens: 300000, OutputTokens: 4000}

	cost, err := svc.CalculateCost("gpt-5.5-pro", tokens, 1.0)
	require.NoError(t, err)
	require.InDelta(t, float64(tokens.InputTokens)*2.5e-6*2.0, cost.InputCost, 1e-10)
	require.InDelta(t, float64(tokens.OutputTokens)*15e-6*1.5, cost.OutputCost, 1e-10)
}
