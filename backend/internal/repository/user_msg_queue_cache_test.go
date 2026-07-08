package repository

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newUserMsgQueueCacheWithMiniRedis(t *testing.T) (*userMsgQueueCache, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return NewUserMsgQueueCache(rdb).(*userMsgQueueCache), rdb
}

func TestUserMsgQueueCacheLockIndexTracksAcquireReleaseAndBackfillsContendedLocks(t *testing.T) {
	ctx := context.Background()
	cache, rdb := newUserMsgQueueCacheWithMiniRedis(t)
	accountID := int64(3101)
	member := strconv.FormatInt(accountID, 10)

	acquired, err := cache.AcquireLock(ctx, accountID, "owner", int((10 * time.Second).Milliseconds()))
	require.NoError(t, err)
	require.True(t, acquired)
	_, err = rdb.ZScore(ctx, umqLockIndexKey, member).Result()
	require.NoError(t, err, "successful lock acquisition should write lock index")

	require.NoError(t, rdb.ZRem(ctx, umqLockIndexKey, member).Err(), "simulate lost index")
	acquired, err = cache.AcquireLock(ctx, accountID, "contender", int((10 * time.Second).Milliseconds()))
	require.NoError(t, err)
	require.False(t, acquired)
	_, err = rdb.ZScore(ctx, umqLockIndexKey, member).Result()
	require.NoError(t, err, "failed acquisition should backfill observed holder lock index")

	released, err := cache.ReleaseLock(ctx, accountID, "owner")
	require.NoError(t, err)
	require.True(t, released)
	_, err = rdb.ZScore(ctx, umqLockIndexKey, member).Result()
	require.ErrorIs(t, err, redis.Nil, "release should remove lock index member")
}

func TestUserMsgQueueCacheReconcileExpiredLockCandidatesDeletesOnlyExpiredOrphanLocks(t *testing.T) {
	ctx := context.Background()
	cache, rdb := newUserMsgQueueCacheWithMiniRedis(t)
	nowMs, err := cache.GetCurrentTimeMs(ctx)
	require.NoError(t, err)

	orphanID := int64(3201)
	liveID := int64(3202)
	orphanMember := strconv.FormatInt(orphanID, 10)
	liveMember := strconv.FormatInt(liveID, 10)
	require.NoError(t, rdb.Set(ctx, umqLockKey(orphanID), "orphan", 0).Err(), "no TTL means orphan lock")
	require.NoError(t, rdb.Set(ctx, umqLockKey(liveID), "live", 10*time.Second).Err())
	require.NoError(t, rdb.ZAdd(ctx, umqLockIndexKey,
		redis.Z{Score: float64(nowMs - 1), Member: orphanMember},
		redis.Z{Score: float64(nowMs - 1), Member: liveMember},
		redis.Z{Score: float64(nowMs - 1), Member: "not-an-id"},
	).Err())

	cleaned, err := cache.ReconcileExpiredLockCandidates(ctx, 1000)
	require.NoError(t, err)
	require.Equal(t, 1, cleaned)

	_, err = rdb.Get(ctx, umqLockKey(orphanID)).Result()
	require.ErrorIs(t, err, redis.Nil, "PTTL=-1 orphan lock should be deleted")
	_, err = rdb.ZScore(ctx, umqLockIndexKey, orphanMember).Result()
	require.ErrorIs(t, err, redis.Nil, "orphan index member should be removed")
	_, err = rdb.ZScore(ctx, umqLockIndexKey, "not-an-id").Result()
	require.ErrorIs(t, err, redis.Nil, "malformed index member should be removed")
	_, err = rdb.Get(ctx, umqLockKey(liveID)).Result()
	require.NoError(t, err, "live lock with TTL must not be deleted")
	rescheduled, err := rdb.ZScore(ctx, umqLockIndexKey, liveMember).Result()
	require.NoError(t, err)
	require.Greater(t, int64(rescheduled), nowMs, "live lock index should be rescheduled by PTTL")
}
