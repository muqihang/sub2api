package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func newChineseFallbackTestBillingService() *BillingService {
	return NewBillingService(&config.Config{}, nil)
}

func TestGetModelPricing_ChineseProviderFallbacks(t *testing.T) {
	svc := newChineseFallbackTestBillingService()

	tests := []struct {
		model       string
		inputPrice  float64
		outputPrice float64
		cacheRead   float64
	}{
		{model: "glm-5.1", inputPrice: 1.4e-6, outputPrice: 4.4e-6, cacheRead: 0.26e-6},
		{model: "glm-5-turbo", inputPrice: 1.2e-6, outputPrice: 4.0e-6, cacheRead: 0.24e-6},
		{model: "glm-4.7-flashx", inputPrice: 0.07e-6, outputPrice: 0.4e-6, cacheRead: 0.01e-6},
		{model: "glm-4.5-x", inputPrice: 2.2e-6, outputPrice: 8.9e-6, cacheRead: 0.45e-6},
		{model: "glm-4.5-flash", inputPrice: 0, outputPrice: 0, cacheRead: 0},
		{model: "kimi-k2.6", inputPrice: 0.95e-6, outputPrice: 4.0e-6, cacheRead: 0.15e-6},
		{model: "kimi-k2-thinking", inputPrice: 0.56e-6, outputPrice: 2.24e-6, cacheRead: 0.14e-6},
		{model: "kimi-for-coding", inputPrice: 0.95e-6, outputPrice: 4.00e-6, cacheRead: 0.15e-6},
		{model: "minimax-m3", inputPrice: 0.60e-6, outputPrice: 2.40e-6, cacheRead: 0.12e-6},
		{model: "minimax-m2.7-highspeed", inputPrice: 0.60e-6, outputPrice: 2.40e-6, cacheRead: 0.06e-6},
		{model: "deepseek-chat", inputPrice: 0.14e-6, outputPrice: 0.28e-6, cacheRead: 0.0028e-6},
		{model: "deepseek-reasoner", inputPrice: 0.14e-6, outputPrice: 0.28e-6, cacheRead: 0.0028e-6},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			pricing, err := svc.GetModelPricing(tt.model)
			require.NoError(t, err)
			require.NotNil(t, pricing)
			require.InDelta(t, tt.inputPrice, pricing.InputPricePerToken, 1e-12)
			require.InDelta(t, tt.outputPrice, pricing.OutputPricePerToken, 1e-12)
			require.InDelta(t, tt.cacheRead, pricing.CacheReadPricePerToken, 1e-12)
		})
	}
}

func TestGetModelPricing_ChineseProviderUnknownAliasesDoNotFallback(t *testing.T) {
	svc := newChineseFallbackTestBillingService()

	for _, model := range []string{"qwen3-max", "doubao-seed-1.6", "hunyuan-turbos-latest", "kimi-k2-0905"} {
		t.Run(model, func(t *testing.T) {
			pricing, err := svc.GetModelPricing(model)
			require.Nil(t, pricing)
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
		})
	}
}
