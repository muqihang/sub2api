package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestBillingServiceOpenAINewModelFallbacks(t *testing.T) {
	svc := NewBillingService(&config.Config{}, nil)

	tests := []struct {
		model                                string
		input, output, cacheRead, cacheWrite float64
		longContextThreshold                 int
	}{
		{model: "gpt-5.5-pro", input: 2.5e-6, output: 15e-6, cacheRead: 0.25e-6, cacheWrite: 2.5e-6, longContextThreshold: 272000},
		{model: "gpt-5.6-sol", input: 5e-6, output: 30e-6, cacheRead: 0.5e-6, cacheWrite: 6.25e-6},
		{model: "gpt-5.6-terra", input: 2.5e-6, output: 15e-6, cacheRead: 0.25e-6, cacheWrite: 3.125e-6},
		{model: "gpt-5.6-luna", input: 1e-6, output: 6e-6, cacheRead: 0.1e-6, cacheWrite: 1.25e-6},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			pricing, err := svc.GetModelPricing(tt.model)
			require.NoError(t, err)
			require.NotNil(t, pricing)
			require.InDelta(t, tt.input, pricing.InputPricePerToken, 1e-12)
			require.InDelta(t, tt.output, pricing.OutputPricePerToken, 1e-12)
			require.InDelta(t, tt.cacheRead, pricing.CacheReadPricePerToken, 1e-12)
			require.InDelta(t, tt.cacheWrite, pricing.CacheCreationPricePerToken, 1e-12)
			require.Equal(t, tt.longContextThreshold, pricing.LongContextInputThreshold)
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
