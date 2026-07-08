package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageLogRepositoryCreateBestEffort_QueueFullWaitsForContext(t *testing.T) {
	repo := newUsageLogRepositoryWithSQL(nil, &sql.DB{})
	repo.bestEffortBatchCh = make(chan usageLogBestEffortRequest, 1)
	repo.bestEffortBatchCh <- usageLogBestEffortRequest{}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- repo.CreateBestEffort(ctx, usageLogDurabilityTestLog("best-effort-full"))
	}()

	assertUsageLogQueueCallBlocksUntilContext(t, errCh)
	cancel()

	err := <-errCh
	require.Error(t, err)
	require.True(t, service.IsUsageLogCreateDropped(err))
	require.NotContains(t, err.Error(), "queue full")
}

func TestUsageLogRepositoryCreate_QueueFullWaitsForContext(t *testing.T) {
	repo := newUsageLogRepositoryWithSQL(nil, &sql.DB{})
	repo.createBatchCh = make(chan usageLogCreateRequest, 1)
	repo.createBatchCh <- usageLogCreateRequest{}

	ctx, cancel := context.WithCancel(context.Background())
	type createResult struct {
		inserted bool
		err      error
	}
	resultCh := make(chan createResult, 1)
	go func() {
		inserted, err := repo.Create(ctx, usageLogDurabilityTestLog("create-full"))
		resultCh <- createResult{inserted: inserted, err: err}
	}()

	assertUsageLogQueueCallBlocksUntilContext(t, resultCh)
	cancel()

	res := <-resultCh
	require.False(t, res.inserted)
	require.Error(t, res.err)
	require.True(t, service.IsUsageLogCreateNotPersisted(res.err))
	require.NotContains(t, res.err.Error(), "queue full")
}

func assertUsageLogQueueCallBlocksUntilContext[T any](t *testing.T, ch <-chan T) {
	t.Helper()
	select {
	case got := <-ch:
		t.Fatalf("queue-full call returned before context cancellation: %#v", got)
	case <-time.After(25 * time.Millisecond):
	}
}

func usageLogDurabilityTestLog(requestID string) *service.UsageLog {
	return &service.UsageLog{
		UserID:       1,
		APIKeyID:     2,
		AccountID:    3,
		RequestID:    requestID,
		Model:        "claude-3",
		InputTokens:  10,
		OutputTokens: 20,
		TotalCost:    0.5,
		ActualCost:   0.5,
		CreatedAt:    time.Now().UTC(),
	}
}
