package service

import (
	"context"
	"testing"
	"time"
)

type slotCleanupCache struct {
	cleanupKeysCalled chan struct{}
}

var _ ConcurrencyCache = (*slotCleanupCache)(nil)

func (c *slotCleanupCache) AcquireAccountSlot(context.Context, int64, int, string) (bool, error) {
	return true, nil
}
func (c *slotCleanupCache) ReleaseAccountSlot(context.Context, int64, string) error   { return nil }
func (c *slotCleanupCache) GetAccountConcurrency(context.Context, int64) (int, error) { return 0, nil }
func (c *slotCleanupCache) GetAccountConcurrencyBatch(context.Context, []int64) (map[int64]int, error) {
	return map[int64]int{}, nil
}
func (c *slotCleanupCache) IncrementAccountWaitCount(context.Context, int64, int) (bool, error) {
	return true, nil
}
func (c *slotCleanupCache) DecrementAccountWaitCount(context.Context, int64) error { return nil }
func (c *slotCleanupCache) GetAccountWaitingCount(context.Context, int64) (int, error) {
	return 0, nil
}
func (c *slotCleanupCache) AcquireUserSlot(context.Context, int64, int, string) (bool, error) {
	return true, nil
}
func (c *slotCleanupCache) ReleaseUserSlot(context.Context, int64, string) error   { return nil }
func (c *slotCleanupCache) GetUserConcurrency(context.Context, int64) (int, error) { return 0, nil }
func (c *slotCleanupCache) IncrementWaitCount(context.Context, int64, int) (bool, error) {
	return true, nil
}
func (c *slotCleanupCache) DecrementWaitCount(context.Context, int64) error { return nil }
func (c *slotCleanupCache) GetAccountsLoadBatch(context.Context, []AccountWithConcurrency) (map[int64]*AccountLoadInfo, error) {
	return map[int64]*AccountLoadInfo{}, nil
}
func (c *slotCleanupCache) GetUsersLoadBatch(context.Context, []UserWithConcurrency) (map[int64]*UserLoadInfo, error) {
	return map[int64]*UserLoadInfo{}, nil
}
func (c *slotCleanupCache) CleanupExpiredAccountSlots(context.Context, int64) error { return nil }
func (c *slotCleanupCache) CleanupStaleProcessSlots(context.Context, string) error  { return nil }
func (c *slotCleanupCache) CleanupExpiredAccountSlotKeys(context.Context) error {
	select {
	case c.cleanupKeysCalled <- struct{}{}:
	default:
	}
	return nil
}

func TestStartSlotCleanupWorker_UsesCacheWideCleanupWithoutAccountRepo(t *testing.T) {
	cache := &slotCleanupCache{cleanupKeysCalled: make(chan struct{}, 1)}
	svc := NewConcurrencyService(cache)

	svc.StartSlotCleanupWorker(nil, time.Hour)

	select {
	case <-cache.cleanupKeysCalled:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("cleanup worker did not call cache-wide account slot cleanup")
	}
}
