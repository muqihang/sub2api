package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestGetModelPricing_OpenAIImage2Pricing(t *testing.T) {
	svc := NewBillingService(&config.Config{}, nil)

	pricing, err := svc.GetModelPricing("gpt-image-2")
	require.NoError(t, err)
	require.NotNil(t, pricing)
	require.InDelta(t, 5e-6, pricing.InputPricePerToken, 1e-12)
	require.InDelta(t, 1.25e-6, pricing.CacheReadPricePerToken, 1e-12)
	require.InDelta(t, 30e-6, pricing.ImageOutputPricePerToken, 1e-12)
}

func TestCalculateImageCost_UsesOutputCostPerImageTokenFallback(t *testing.T) {
	svc := NewBillingService(&config.Config{}, &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-image-2": {
				OutputCostPerImageToken: 3e-5,
			},
		},
	})

	cost := svc.CalculateImageCost("gpt-image-2", "1K", 2, nil, 1.0)
	require.InDelta(t, 6e-5, cost.TotalCost, 1e-12)
	require.InDelta(t, 6e-5, cost.ActualCost, 1e-12)
}
