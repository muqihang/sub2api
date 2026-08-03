package service

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func newFallbackWarnBillingService() *BillingService {
	return NewBillingService(&config.Config{}, nil)
}

func captureFallbackWarnStdLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevOut := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})
	return &buf
}

func fallbackWarnFloat64Ptr(v float64) *float64 { return &v }

func TestBillingServiceFallbackPricingWarnLoggedOncePerNormalizedModel(t *testing.T) {
	svc := newFallbackWarnBillingService()
	buf := captureFallbackWarnStdLog(t)

	for i := 0; i < 3; i++ {
		pricing, err := svc.GetModelPricing("Claude-Sonnet-4")
		require.NoError(t, err)
		require.NotNil(t, pricing)
		require.InDelta(t, 3e-6, pricing.InputPricePerToken, 1e-12)
		require.InDelta(t, 15e-6, pricing.OutputPricePerToken, 1e-12)
	}
	_, err := svc.GetModelPricing("claude-sonnet-4")
	require.NoError(t, err)

	out := buf.String()
	require.Equal(t, 1, strings.Count(out, "Using fallback pricing for model: claude-sonnet-4"), out)
	require.NotContains(t, out, "Claude-Sonnet-4")
}

func TestBillingServiceFallbackPricingWarnPerModelAndKeepsPricing(t *testing.T) {
	svc := newFallbackWarnBillingService()
	buf := captureFallbackWarnStdLog(t)

	for i := 0; i < 2; i++ {
		sonnet, err := svc.GetModelPricing("claude-sonnet-4")
		require.NoError(t, err)
		require.InDelta(t, 3e-6, sonnet.InputPricePerToken, 1e-12)
		require.InDelta(t, 15e-6, sonnet.OutputPricePerToken, 1e-12)

		gpt54, err := svc.GetModelPricing("gpt-5.4")
		require.NoError(t, err)
		require.InDelta(t, 2.5e-6, gpt54.InputPricePerToken, 1e-12)
		require.InDelta(t, 15e-6, gpt54.OutputPricePerToken, 1e-12)
	}

	out := buf.String()
	require.Equal(t, 1, strings.Count(out, "Using fallback pricing for model: claude-sonnet-4"), out)
	require.Equal(t, 1, strings.Count(out, "Using fallback pricing for model: gpt-5.4"), out)
}

func TestBillingServiceFallbackPricingChannelOverrideStillApplies(t *testing.T) {
	svc := newFallbackWarnBillingService()
	buf := captureFallbackWarnStdLog(t)
	channelPricing := &ChannelModelPricing{
		InputPrice:       fallbackWarnFloat64Ptr(10e-6),
		OutputPrice:      fallbackWarnFloat64Ptr(20e-6),
		CacheWritePrice:  fallbackWarnFloat64Ptr(5e-6),
		CacheReadPrice:   fallbackWarnFloat64Ptr(1e-6),
		ImageOutputPrice: fallbackWarnFloat64Ptr(50e-6),
	}

	for i := 0; i < 2; i++ {
		pricing, err := svc.GetModelPricingWithChannel("claude-sonnet-4", channelPricing)
		require.NoError(t, err)
		require.InDelta(t, 10e-6, pricing.InputPricePerToken, 1e-12)
		require.InDelta(t, 10e-6, pricing.InputPricePerTokenPriority, 1e-12)
		require.InDelta(t, 20e-6, pricing.OutputPricePerToken, 1e-12)
		require.InDelta(t, 20e-6, pricing.OutputPricePerTokenPriority, 1e-12)
		require.InDelta(t, 5e-6, pricing.CacheCreationPricePerToken, 1e-12)
		require.InDelta(t, 5e-6, pricing.CacheCreation5mPrice, 1e-12)
		require.InDelta(t, 5e-6, pricing.CacheCreation1hPrice, 1e-12)
		require.InDelta(t, 1e-6, pricing.CacheReadPricePerToken, 1e-12)
		require.InDelta(t, 1e-6, pricing.CacheReadPricePerTokenPriority, 1e-12)
		require.InDelta(t, 50e-6, pricing.ImageOutputPricePerToken, 1e-12)
	}

	out := buf.String()
	require.Equal(t, 1, strings.Count(out, "Using fallback pricing for model: claude-sonnet-4"), out)
}
