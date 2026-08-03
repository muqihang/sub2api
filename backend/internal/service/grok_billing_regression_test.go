package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestGrok45FallbackPricingUsesOfficialAliases(t *testing.T) {
	svc := NewBillingService(&config.Config{}, nil)

	for _, model := range []string{"grok", "grok-latest", "grok-4.5", "grok-4.5-latest", "grok-build-latest"} {
		model := model
		t.Run(model, func(t *testing.T) {
			pricing, err := svc.GetModelPricing(model)
			require.NoError(t, err)
			require.InDelta(t, 2e-6, pricing.InputPricePerToken, 1e-12)
			require.InDelta(t, 6e-6, pricing.OutputPricePerToken, 1e-12)
			require.InDelta(t, 0.5e-6, pricing.CacheReadPricePerToken, 1e-12)
		})
	}
}
