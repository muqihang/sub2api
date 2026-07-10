package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCalculateVideoCostBillsConfiguredRatePerSecond(t *testing.T) {
	price := 0.2
	svc := NewBillingService(&config.Config{}, nil)

	cost := svc.CalculateVideoCost("grok-imagine-video-1.5", "720p", 2, 12, &VideoPriceConfig{Price720P: &price}, 1.5)

	require.Equal(t, string(BillingModeVideo), cost.BillingMode)
	require.InDelta(t, 4.8, cost.TotalCost, 1e-12)
	require.InDelta(t, 7.2, cost.ActualCost, 1e-12)
}
