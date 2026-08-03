package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type cleanupReconcileCache struct {
	called chan int
}

func (c *cleanupReconcileCache) AcquireLock(context.Context, int64, string, int) (bool, error) {
	return true, nil
}
func (c *cleanupReconcileCache) ReleaseLock(context.Context, int64, string) (bool, error) {
	return true, nil
}
func (c *cleanupReconcileCache) GetLastCompletedMs(context.Context, int64) (int64, error) {
	return 0, nil
}
func (c *cleanupReconcileCache) GetCurrentTimeMs(context.Context) (int64, error) {
	return time.Now().UnixMilli(), nil
}
func (c *cleanupReconcileCache) ReconcileExpiredLockCandidates(_ context.Context, maxCount int) (int, error) {
	select {
	case c.called <- maxCount:
	default:
	}
	return 0, nil
}

func TestUserMessageQueueCleanupWorkerUsesIndexedReconcile(t *testing.T) {
	cache := &cleanupReconcileCache{called: make(chan int, 1)}
	svc := NewUserMessageQueueService(cache, nil, &config.UserMessageQueueConfig{})
	svc.StartCleanupWorker(time.Millisecond)
	defer svc.Stop()

	select {
	case maxCount := <-cache.called:
		require.Equal(t, 1000, maxCount)
	case <-time.After(250 * time.Millisecond):
		t.Fatal("cleanup worker did not call indexed reconcile")
	}
}
