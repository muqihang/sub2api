package service

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

func TestPeakMultiplierAtAppliesOnlyInsideSubscriptionWindow(t *testing.T) {
	group := &Group{
		SubscriptionType:   SubscriptionTypeSubscription,
		PeakRateEnabled:    true,
		PeakStart:          "9:30",
		PeakEnd:            "11:00",
		PeakRateMultiplier: 1.5,
	}

	loc := timezone.Location()
	require.Equal(t, 1.5, group.PeakMultiplierAt(time.Date(2026, 7, 8, 10, 0, 0, 0, loc)))
	require.Equal(t, 1.0, group.PeakMultiplierAt(time.Date(2026, 7, 8, 11, 0, 0, 0, loc)))

	group.SubscriptionType = SubscriptionTypeStandard
	require.Equal(t, 1.0, group.PeakMultiplierAt(time.Date(2026, 7, 8, 10, 0, 0, 0, loc)))
}

func TestNormalizePeakRateConfigClearsNonSubscriptionAndScrubsDisabledDirtyValues(t *testing.T) {
	enabled, start, end, multiplier := NormalizePeakRateConfig(SubscriptionTypeStandard, true, "09:00", "10:00", 2)
	require.False(t, enabled)
	require.Empty(t, start)
	require.Empty(t, end)
	require.Equal(t, 1.0, multiplier)

	enabled, start, end, multiplier = NormalizePeakRateConfig(SubscriptionTypeSubscription, false, "bad", "10:00", -2)
	require.False(t, enabled)
	require.Empty(t, start)
	require.Equal(t, "10:00", end)
	require.Equal(t, 1.0, multiplier)
}

func TestComputePeakAwareMultipliersDoesNotApplyPeakToImageMultiplier(t *testing.T) {
	apiKey := &APIKey{Group: &Group{
		SubscriptionType:     SubscriptionTypeSubscription,
		RateMultiplier:       2,
		ImageRateIndependent: true,
		ImageRateMultiplier:  3,
		PeakRateEnabled:      true,
		PeakStart:            "00:00",
		PeakEnd:              "23:59",
		PeakRateMultiplier:   4,
	}}

	text, image := computePeakAwareMultipliers(apiKey, 2, time.Date(2026, 7, 8, 12, 0, 0, 0, timezone.Location()))
	require.Equal(t, 8.0, text)
	require.Equal(t, 3.0, image)
}
