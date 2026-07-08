package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func newDoubaoEmbeddingBillingService() *BillingService {
	return NewBillingService(&config.Config{}, nil)
}

func TestGetModelPricing_DoubaoEmbeddingVisionImageInputRate(t *testing.T) {
	svc := newDoubaoEmbeddingBillingService()

	for _, model := range []string{
		"doubao-embedding-vision",
		"doubao-embedding-vision-251215",
		"Doubao-Embedding-Vision",
	} {
		t.Run(model, func(t *testing.T) {
			pricing, err := svc.GetModelPricing(model)
			require.NoError(t, err)
			require.NotNil(t, pricing)
			require.InDelta(t, 0.098e-6, pricing.InputPricePerToken, 1e-12)
			require.InDelta(t, 0.252e-6, pricing.ImageInputPricePerToken, 1e-12)
			require.Zero(t, pricing.OutputPricePerToken)
		})
	}
}

func TestGetModelPricing_DoubaoEmbeddingUnknownAliasesDoNotFallback(t *testing.T) {
	svc := newDoubaoEmbeddingBillingService()

	for _, model := range []string{"doubao-pro", "doubao-embedding-text-240515", "doubao-embedding"} {
		t.Run(model, func(t *testing.T) {
			pricing, err := svc.GetModelPricing(model)
			require.Nil(t, pricing)
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
		})
	}
}

func TestCalculateCost_DoubaoEmbeddingVisionDifferentialInput(t *testing.T) {
	svc := newDoubaoEmbeddingBillingService()

	mixed := UsageTokens{InputTokens: 1340, ImageInputTokens: 28}
	cost, err := svc.CalculateCost("doubao-embedding-vision", mixed, 1.0)
	require.NoError(t, err)
	wantMixed := float64(1312)*0.098e-6 + float64(28)*0.252e-6
	require.InDelta(t, wantMixed, cost.InputCost, 1e-15)
	require.InDelta(t, wantMixed, cost.TotalCost, 1e-15)
	require.Zero(t, cost.OutputCost)

	textOnly := UsageTokens{InputTokens: 1340}
	costText, err := svc.CalculateCost("doubao-embedding-vision", textOnly, 1.0)
	require.NoError(t, err)
	require.InDelta(t, float64(1340)*0.098e-6, costText.InputCost, 1e-15)

	imageTokensExceedInput := UsageTokens{InputTokens: 10, ImageInputTokens: 50}
	costWeird, err := svc.CalculateCost("doubao-embedding-vision", imageTokensExceedInput, 1.0)
	require.NoError(t, err)
	require.InDelta(t, float64(10)*0.252e-6, costWeird.InputCost, 1e-15)
}
