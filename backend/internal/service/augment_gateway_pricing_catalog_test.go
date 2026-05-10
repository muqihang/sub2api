package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/resources"
	"github.com/stretchr/testify/require"
)

func TestAugmentGatewayExplicitPricingChecker_FirstBatchModelsRemainExplicit(t *testing.T) {
	checker := newAugmentGatewayEmbeddedExplicitPricingCatalog(resources.ModelPricingCatalogJSON)

	for _, modelID := range []string{
		"gpt-5.4",
		"gpt-5.5",
		"gpt-5.4-mini",
		"deepseek-v4-pro",
		"deepseek-v4-flash",
	} {
		require.Truef(t, checker.HasExplicitPricing(modelID), "expected %s to have explicit pricing", modelID)
	}
}

func TestAugmentGatewayExplicitPricingChecker_RejectsPartialCatalogRows(t *testing.T) {
	checker := newAugmentGatewayEmbeddedExplicitPricingCatalog([]byte(`{
		"gpt-5.4": {
			"input_cost_per_token": 0.0000025
		},
		"gpt-5.4-mini": {
			"input_cost_per_token": 0.00000075,
			"output_cost_per_token": 0.0000045,
			"supports_prompt_caching": true,
			"cache_read_input_token_cost": 0.000000075
		},
		"deepseek-v4-pro": {
			"input_cost_per_token": 0.00000174,
			"output_cost_per_token": 0.00000348
		}
	}`))

	require.False(t, checker.HasExplicitPricing("gpt-5.4"))
	require.False(t, checker.HasExplicitPricing("gpt-5.4-mini"))
	require.True(t, checker.HasExplicitPricing("deepseek-v4-pro"))
}
