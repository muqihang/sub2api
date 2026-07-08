package service

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func timePtrForOpenAISchedulerTest(t time.Time) *time.Time { return &t }

func newOpenAIWeightedSchedulerForTest(weights config.GatewayOpenAIWSSchedulerScoreWeights) *defaultOpenAIAccountScheduler {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights = weights
	return &defaultOpenAIAccountScheduler{service: &OpenAIGatewayService{cfg: cfg}}
}

func openAISchedulerPlanScores(plan openAIAccountLoadPlan) map[int64]float64 {
	scores := make(map[int64]float64, len(plan.candidates))
	for _, candidate := range plan.candidates {
		if candidate.account != nil {
			scores[candidate.account.ID] = candidate.score
		}
	}
	return scores
}

func TestBuildOpenAIAccountLoadPlan_ResetWeightPrefersSoonestFutureWindow(t *testing.T) {
	now := time.Now()
	sched := newOpenAIWeightedSchedulerForTest(config.GatewayOpenAIWSSchedulerScoreWeights{Reset: 1})
	plan := sched.buildOpenAIAccountLoadPlan(OpenAIAccountScheduleRequest{}, []*Account{
		{ID: 1, Priority: 0, SessionWindowEnd: timePtrForOpenAISchedulerTest(now.Add(8 * time.Hour))},
		{ID: 2, Priority: 0, SessionWindowEnd: timePtrForOpenAISchedulerTest(now.Add(30 * time.Minute))},
		{ID: 3, Priority: 0, SessionWindowEnd: nil},
	}, nil)

	scores := openAISchedulerPlanScores(plan)
	require.Greater(t, scores[2], scores[1], "soonest active reset window should outrank later active windows")
	require.Greater(t, scores[2], scores[3], "soonest active reset window should outrank missing windows")
}

func TestBuildOpenAIAccountLoadPlan_ResetWeightZeroNoEffect(t *testing.T) {
	now := time.Now()
	sched := newOpenAIWeightedSchedulerForTest(config.GatewayOpenAIWSSchedulerScoreWeights{Reset: 0})
	plan := sched.buildOpenAIAccountLoadPlan(OpenAIAccountScheduleRequest{}, []*Account{
		{ID: 1, Priority: 0, SessionWindowEnd: timePtrForOpenAISchedulerTest(now.Add(8 * time.Hour))},
		{ID: 2, Priority: 0, SessionWindowEnd: timePtrForOpenAISchedulerTest(now.Add(30 * time.Minute))},
	}, nil)

	scores := openAISchedulerPlanScores(plan)
	require.Equal(t, scores[1], scores[2], "reset weight default/zero must not affect scheduler score")
}

func TestOpenAIQuotaHeadroomFactor(t *testing.T) {
	now := time.Date(2026, 3, 11, 10, 0, 0, 0, time.UTC)

	t.Run("primary remaining", func(t *testing.T) {
		account := &Account{Extra: map[string]any{
			"codex_primary_used_percent": 20.0,
			"codex_primary_reset_at":     now.Add(24 * time.Hour).Format(time.RFC3339),
			"codex_usage_updated_at":     now.Add(-time.Minute).Format(time.RFC3339),
		}}
		require.InDelta(t, 0.8, openAIQuotaHeadroomFactor(account, now), 0.0001)
	})

	t.Run("missing primary is neutral", func(t *testing.T) {
		account := &Account{Extra: map[string]any{
			"codex_usage_updated_at": now.Add(-time.Minute).Format(time.RFC3339),
		}}
		require.Equal(t, openAIQuotaHeadroomNeutralFactor, openAIQuotaHeadroomFactor(account, now))
	})

	t.Run("expired primary window is neutral", func(t *testing.T) {
		account := &Account{Extra: map[string]any{
			"codex_primary_used_percent": 20.0,
			"codex_primary_reset_at":     now.Add(-time.Minute).Format(time.RFC3339),
			"codex_usage_updated_at":     now.Add(-time.Minute).Format(time.RFC3339),
		}}
		require.Equal(t, openAIQuotaHeadroomNeutralFactor, openAIQuotaHeadroomFactor(account, now))
	})

	t.Run("stale snapshot is neutral", func(t *testing.T) {
		account := &Account{Extra: map[string]any{
			"codex_primary_used_percent": 20.0,
			"codex_primary_reset_at":     now.Add(24 * time.Hour).Format(time.RFC3339),
			"codex_usage_updated_at":     now.Add(-9 * time.Hour).Format(time.RFC3339),
		}}
		require.Equal(t, openAIQuotaHeadroomNeutralFactor, openAIQuotaHeadroomFactor(account, now))
	})

	t.Run("secondary low headroom discounts primary", func(t *testing.T) {
		account := &Account{Extra: map[string]any{
			"codex_primary_used_percent":   20.0,
			"codex_primary_reset_at":       now.Add(24 * time.Hour).Format(time.RFC3339),
			"codex_secondary_used_percent": 95.0,
			"codex_secondary_reset_at":     now.Add(time.Hour).Format(time.RFC3339),
			"codex_usage_updated_at":       now.Add(-time.Minute).Format(time.RFC3339),
		}}
		require.InDelta(t, 0.4, openAIQuotaHeadroomFactor(account, now), 0.0001)
	})
}

func TestBuildOpenAIAccountLoadPlan_QuotaHeadroomWeightPrefersHigher7dRemaining(t *testing.T) {
	now := time.Now()
	sched := newOpenAIWeightedSchedulerForTest(config.GatewayOpenAIWSSchedulerScoreWeights{QuotaHeadroom: 1})
	plan := sched.buildOpenAIAccountLoadPlan(OpenAIAccountScheduleRequest{}, []*Account{
		{ID: 1, Priority: 0, Extra: map[string]any{
			"codex_primary_used_percent": 80.0,
			"codex_primary_reset_at":     now.Add(24 * time.Hour).Format(time.RFC3339),
			"codex_usage_updated_at":     now.Add(-time.Minute).Format(time.RFC3339),
		}},
		{ID: 2, Priority: 0, Extra: map[string]any{
			"codex_primary_used_percent": 20.0,
			"codex_primary_reset_at":     now.Add(24 * time.Hour).Format(time.RFC3339),
			"codex_usage_updated_at":     now.Add(-time.Minute).Format(time.RFC3339),
		}},
	}, nil)

	scores := openAISchedulerPlanScores(plan)
	require.Greater(t, scores[2], scores[1], "7d remaining headroom should improve score only when explicitly weighted")
}

func TestBuildOpenAIAccountLoadPlan_QuotaHeadroomWeightZeroNoEffect(t *testing.T) {
	now := time.Now()
	sched := newOpenAIWeightedSchedulerForTest(config.GatewayOpenAIWSSchedulerScoreWeights{QuotaHeadroom: 0})
	plan := sched.buildOpenAIAccountLoadPlan(OpenAIAccountScheduleRequest{}, []*Account{
		{ID: 1, Priority: 0, Extra: map[string]any{
			"codex_primary_used_percent": 80.0,
			"codex_primary_reset_at":     now.Add(24 * time.Hour).Format(time.RFC3339),
			"codex_usage_updated_at":     now.Add(-time.Minute).Format(time.RFC3339),
		}},
		{ID: 2, Priority: 0, Extra: map[string]any{
			"codex_primary_used_percent": 20.0,
			"codex_primary_reset_at":     now.Add(24 * time.Hour).Format(time.RFC3339),
			"codex_usage_updated_at":     now.Add(-time.Minute).Format(time.RFC3339),
		}},
	}, nil)

	scores := openAISchedulerPlanScores(plan)
	require.Equal(t, scores[1], scores[2], "quota_headroom default/zero must not affect scheduler score")
}

func TestFilterBySoonestReset(t *testing.T) {
	now := time.Now()
	soon := now.Add(30 * time.Minute)
	later := now.Add(8 * time.Hour)
	expired := now.Add(-time.Minute)

	t.Run("picks soonest active reset", func(t *testing.T) {
		got := filterBySoonestReset([]accountWithLoad{
			{account: &Account{ID: 1, SessionWindowEnd: &later}},
			{account: &Account{ID: 2, SessionWindowEnd: &soon}},
			{account: &Account{ID: 3, SessionWindowEnd: nil}},
		})
		require.Len(t, got, 1)
		require.Equal(t, int64(2), got[0].account.ID)
	})

	t.Run("keeps all when no active windows", func(t *testing.T) {
		got := filterBySoonestReset([]accountWithLoad{
			{account: &Account{ID: 1, SessionWindowEnd: nil}},
			{account: &Account{ID: 2, SessionWindowEnd: &expired}},
		})
		require.Len(t, got, 2)
	})
}
