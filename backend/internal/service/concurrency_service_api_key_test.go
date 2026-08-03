package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type apiKeyConcurrencyCacheNoTag struct {
	counts            map[int64]int
	batchErr          error
	trackErr          error
	releaseErr        error
	trackedAPIKeyID   int64
	releasedAPIKeyID  int64
	trackedRequestID  string
	releasedRequestID string
}

var _ ConcurrencyCache = (*apiKeyConcurrencyCacheNoTag)(nil)

func (c *apiKeyConcurrencyCacheNoTag) AcquireAccountSlot(context.Context, int64, int, string) (bool, error) {
	return false, nil
}
func (c *apiKeyConcurrencyCacheNoTag) ReleaseAccountSlot(context.Context, int64, string) error {
	return nil
}
func (c *apiKeyConcurrencyCacheNoTag) GetAccountConcurrency(context.Context, int64) (int, error) {
	return 0, nil
}
func (c *apiKeyConcurrencyCacheNoTag) GetAccountConcurrencyBatch(_ context.Context, ids []int64) (map[int64]int, error) {
	out := make(map[int64]int, len(ids))
	for _, id := range ids {
		out[id] = 0
	}
	return out, nil
}
func (c *apiKeyConcurrencyCacheNoTag) IncrementAccountWaitCount(context.Context, int64, int) (bool, error) {
	return true, nil
}
func (c *apiKeyConcurrencyCacheNoTag) DecrementAccountWaitCount(context.Context, int64) error {
	return nil
}
func (c *apiKeyConcurrencyCacheNoTag) GetAccountWaitingCount(context.Context, int64) (int, error) {
	return 0, nil
}
func (c *apiKeyConcurrencyCacheNoTag) AcquireUserSlot(context.Context, int64, int, string) (bool, error) {
	return false, nil
}
func (c *apiKeyConcurrencyCacheNoTag) ReleaseUserSlot(context.Context, int64, string) error {
	return nil
}
func (c *apiKeyConcurrencyCacheNoTag) GetUserConcurrency(context.Context, int64) (int, error) {
	return 0, nil
}
func (c *apiKeyConcurrencyCacheNoTag) IncrementWaitCount(context.Context, int64, int) (bool, error) {
	return true, nil
}
func (c *apiKeyConcurrencyCacheNoTag) DecrementWaitCount(context.Context, int64) error { return nil }
func (c *apiKeyConcurrencyCacheNoTag) GetAccountsLoadBatch(context.Context, []AccountWithConcurrency) (map[int64]*AccountLoadInfo, error) {
	return map[int64]*AccountLoadInfo{}, nil
}
func (c *apiKeyConcurrencyCacheNoTag) GetUsersLoadBatch(context.Context, []UserWithConcurrency) (map[int64]*UserLoadInfo, error) {
	return map[int64]*UserLoadInfo{}, nil
}
func (c *apiKeyConcurrencyCacheNoTag) CleanupExpiredAccountSlots(context.Context, int64) error {
	return nil
}
func (c *apiKeyConcurrencyCacheNoTag) CleanupExpiredAccountSlotKeys(context.Context) error {
	return nil
}
func (c *apiKeyConcurrencyCacheNoTag) CleanupStaleProcessSlots(context.Context, string) error {
	return nil
}

func (c *apiKeyConcurrencyCacheNoTag) TrackAPIKeySlot(_ context.Context, apiKeyID int64, requestID string) error {
	c.trackedAPIKeyID = apiKeyID
	c.trackedRequestID = requestID
	return c.trackErr
}
func (c *apiKeyConcurrencyCacheNoTag) ReleaseAPIKeySlot(_ context.Context, apiKeyID int64, requestID string) error {
	c.releasedAPIKeyID = apiKeyID
	c.releasedRequestID = requestID
	return c.releaseErr
}
func (c *apiKeyConcurrencyCacheNoTag) GetAPIKeyConcurrencyBatch(_ context.Context, ids []int64) (map[int64]int, error) {
	if c.batchErr != nil {
		return nil, c.batchErr
	}
	out := make(map[int64]int, len(ids))
	for _, id := range ids {
		out[id] = c.counts[id]
	}
	return out, nil
}

func TestConcurrencyServiceAPIKeyConcurrencyBatchReturnsCounts(t *testing.T) {
	cache := &apiKeyConcurrencyCacheNoTag{counts: map[int64]int{10: 2, 11: 4}}
	svc := NewConcurrencyService(cache)

	counts, err := svc.GetAPIKeyConcurrencyBatch(context.Background(), []int64{10, 11, 12})
	require.NoError(t, err)
	require.Equal(t, map[int64]int{10: 2, 11: 4, 12: 0}, counts)
}

func TestConcurrencyServiceAPIKeyConcurrencyBatchFallsBackToZero(t *testing.T) {
	svc := NewConcurrencyService(&apiKeyConcurrencyCacheNoTag{batchErr: errors.New("redis unavailable")})
	counts, err := svc.GetAPIKeyConcurrencyBatch(context.Background(), []int64{12})
	require.NoError(t, err)
	require.Equal(t, map[int64]int{12: 0}, counts)
}

func TestConcurrencyServiceTrackAPIKeySlotTracksAndReleases(t *testing.T) {
	cache := &apiKeyConcurrencyCacheNoTag{}
	svc := NewConcurrencyService(cache)

	release := svc.TrackAPIKeySlot(context.Background(), 42)
	require.Equal(t, int64(42), cache.trackedAPIKeyID)
	require.NotEmpty(t, cache.trackedRequestID)

	release()
	require.Equal(t, int64(42), cache.releasedAPIKeyID)
	require.Equal(t, cache.trackedRequestID, cache.releasedRequestID)
}

func TestConcurrencyServiceTrackAPIKeySlotIsBestEffort(t *testing.T) {
	cache := &apiKeyConcurrencyCacheNoTag{trackErr: errors.New("redis unavailable")}
	svc := NewConcurrencyService(cache)

	release := svc.TrackAPIKeySlot(context.Background(), 42)
	require.NotNil(t, release)
	release()
	require.Zero(t, cache.releasedAPIKeyID)
}
