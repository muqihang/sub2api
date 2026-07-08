package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

// sessionWindowSyncRepo 记录 syncActiveToPassive 触发的所有写操作。
type sessionWindowSyncRepo struct {
	AccountRepository

	mu                sync.Mutex
	extraUpdates      []map[string]any
	sessionWindowEnds []sessionWindowEndCall
}

type sessionWindowEndCall struct {
	AccountID int64
	End       time.Time
}

func (r *sessionWindowSyncRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copied := make(map[string]any, len(updates))
	for k, v := range updates {
		copied[k] = v
	}
	r.extraUpdates = append(r.extraUpdates, copied)
	return nil
}

func (r *sessionWindowSyncRepo) UpdateSessionWindowEnd(_ context.Context, id int64, end time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessionWindowEnds = append(r.sessionWindowEnds, sessionWindowEndCall{AccountID: id, End: end})
	return nil
}

type passiveUsageAccountRepo struct {
	AccountRepository
	account *Account
}

func (r *passiveUsageAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	if r.account == nil || r.account.ID != id {
		return nil, ErrAccountNotFound
	}
	return r.account, nil
}

type usageWindowStatsRepo struct {
	UsageLogRepository
}

func (r *usageWindowStatsRepo) GetAccountWindowStats(context.Context, int64, time.Time) (*usagestats.AccountStats, error) {
	return &usagestats.AccountStats{}, nil
}

func TestEstimateSetupTokenUsage_ExpiredWindowZeroes(t *testing.T) {
	t.Parallel()

	past := time.Now().Add(-2 * time.Hour)
	svc := &AccountUsageService{}
	info := svc.estimateSetupTokenUsage(&Account{
		SessionWindowEnd: &past,
		Extra: map[string]any{
			"session_window_utilization": 0.53,
		},
	})

	if info.FiveHour == nil {
		t.Fatal("expected non-nil FiveHour info")
	}
	if info.FiveHour.Utilization != 0 {
		t.Fatalf("expected Utilization=0 for expired window, got %v", info.FiveHour.Utilization)
	}
	if info.FiveHour.ResetsAt != nil {
		t.Fatalf("expected ResetsAt=nil for expired window, got %v", info.FiveHour.ResetsAt)
	}
	if info.FiveHour.RemainingSeconds != 0 {
		t.Fatalf("expected RemainingSeconds=0 for expired window, got %v", info.FiveHour.RemainingSeconds)
	}
}

func TestEstimateSetupTokenUsage_ActiveWindowPreservesUtilization(t *testing.T) {
	t.Parallel()

	future := time.Now().Add(3 * time.Hour)
	svc := &AccountUsageService{}
	info := svc.estimateSetupTokenUsage(&Account{
		SessionWindowEnd: &future,
		Extra: map[string]any{
			"session_window_utilization": 0.53,
		},
	})

	if info.FiveHour == nil {
		t.Fatal("expected non-nil FiveHour info")
	}
	if info.FiveHour.Utilization != 53 {
		t.Fatalf("expected Utilization=53, got %v", info.FiveHour.Utilization)
	}
	if info.FiveHour.ResetsAt == nil || !info.FiveHour.ResetsAt.Equal(future) {
		t.Fatalf("expected ResetsAt=%v, got %v", future, info.FiveHour.ResetsAt)
	}
	if info.FiveHour.RemainingSeconds <= 0 {
		t.Fatalf("expected positive RemainingSeconds, got %v", info.FiveHour.RemainingSeconds)
	}
}

func TestGetPassiveUsage_IncludesSevenDayFableWindow(t *testing.T) {
	t.Parallel()

	resetAt := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)
	repo := &passiveUsageAccountRepo{account: &Account{
		ID:       515,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"passive_usage_7d_oi_utilization": 0.82,
			"passive_usage_7d_oi_reset":       resetAt.Unix(),
			"passive_usage_sampled_at":        time.Now().UTC().Format(time.RFC3339),
		},
	}}
	svc := &AccountUsageService{accountRepo: repo, usageLogRepo: &usageWindowStatsRepo{}, cache: NewUsageCache()}

	info, err := svc.GetPassiveUsage(context.Background(), 515)
	if err != nil {
		t.Fatalf("GetPassiveUsage failed: %v", err)
	}
	if info.SevenDayFable == nil {
		t.Fatal("expected seven_day_fable passive window")
	}
	if info.SevenDayFable.Utilization != 82 {
		t.Fatalf("expected Fable utilization=82, got %v", info.SevenDayFable.Utilization)
	}
	if info.SevenDayFable.ResetsAt == nil || !info.SevenDayFable.ResetsAt.Equal(resetAt) {
		t.Fatalf("expected Fable reset=%v, got %v", resetAt, info.SevenDayFable.ResetsAt)
	}
}

func TestBuildUsageInfo_IncludesSevenDayFableWindow(t *testing.T) {
	t.Parallel()

	resetAt := time.Now().Add(72 * time.Hour).UTC().Truncate(time.Second)
	svc := &AccountUsageService{}
	info := svc.buildUsageInfo(&ClaudeUsageResponse{
		SevenDayOverageIncluded: ClaudeUsageWindow{
			Utilization: 66,
			ResetsAt:    resetAt.Format(time.RFC3339),
		},
	}, ptrTime(time.Now()))

	if info.SevenDayFable == nil {
		t.Fatal("expected active usage to include seven_day_fable")
	}
	if info.SevenDayFable.Utilization != 66 {
		t.Fatalf("expected Fable utilization=66, got %v", info.SevenDayFable.Utilization)
	}
	if info.SevenDayFable.ResetsAt == nil || !info.SevenDayFable.ResetsAt.Equal(resetAt) {
		t.Fatalf("expected Fable reset=%v, got %v", resetAt, info.SevenDayFable.ResetsAt)
	}
}

func TestSyncActiveToPassive_WritesSevenDayFableWindow(t *testing.T) {
	t.Parallel()

	repo := &sessionWindowSyncRepo{}
	svc := &AccountUsageService{accountRepo: repo}
	resetsAt := time.Now().Add(6 * 24 * time.Hour).UTC().Truncate(time.Second)
	svc.syncActiveToPassive(context.Background(), 616, &UsageInfo{
		SevenDayFable: &UsageProgress{
			Utilization: 91,
			ResetsAt:    &resetsAt,
		},
	})

	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.extraUpdates) != 1 {
		t.Fatalf("expected one UpdateExtra call, got %d", len(repo.extraUpdates))
	}
	updates := repo.extraUpdates[0]
	if got := updates["passive_usage_7d_oi_utilization"]; got != 0.91 {
		t.Fatalf("expected passive_usage_7d_oi_utilization=0.91, got %#v", got)
	}
	if got := updates["passive_usage_7d_oi_reset"]; got != resetsAt.Unix() {
		t.Fatalf("expected passive_usage_7d_oi_reset=%d, got %#v", resetsAt.Unix(), got)
	}
}

func TestSyncActiveToPassive_WritesFiveHourSessionWindowEnd(t *testing.T) {
	t.Parallel()

	repo := &sessionWindowSyncRepo{}
	svc := &AccountUsageService{accountRepo: repo}
	resetsAt := time.Now().Add(3 * time.Hour).UTC().Truncate(time.Second)
	svc.syncActiveToPassive(context.Background(), 42, &UsageInfo{
		FiveHour: &UsageProgress{
			Utilization: 53,
			ResetsAt:    &resetsAt,
		},
	})

	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.sessionWindowEnds) != 1 {
		t.Fatalf("expected 1 UpdateSessionWindowEnd call, got %d", len(repo.sessionWindowEnds))
	}
	call := repo.sessionWindowEnds[0]
	if call.AccountID != 42 {
		t.Fatalf("expected AccountID=42, got %d", call.AccountID)
	}
	if !call.End.Equal(resetsAt) {
		t.Fatalf("expected End=%v, got %v", resetsAt, call.End)
	}
}

func TestSyncActiveToPassive_SkipsSessionWindowEndWhenResetMissing(t *testing.T) {
	t.Parallel()

	repo := &sessionWindowSyncRepo{}
	svc := &AccountUsageService{accountRepo: repo}
	svc.syncActiveToPassive(context.Background(), 99, &UsageInfo{
		FiveHour: &UsageProgress{Utilization: 10},
	})

	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.sessionWindowEnds) != 0 {
		t.Fatalf("expected no UpdateSessionWindowEnd calls when ResetsAt is nil, got %d", len(repo.sessionWindowEnds))
	}
}
