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

func newConcurrencyCacheWithMiniRedis(t *testing.T) (*concurrencyCache, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	cache := NewConcurrencyCache(rdb, 1, 60).(*concurrencyCache)
	return cache, rdb
}

func redisNowSecondsForTest(t *testing.T, ctx context.Context, rdb *redis.Client) int64 {
	t.Helper()
	now, err := rdb.Time(ctx).Result()
	require.NoError(t, err)
	return now.Unix()
}

func TestConcurrencyCacheActiveIndexesTrackSlotAndWaitLifecycle(t *testing.T) {
	ctx := context.Background()
	cache, rdb := newConcurrencyCacheWithMiniRedis(t)

	accountID := int64(1101)
	ok, err := cache.AcquireAccountSlot(ctx, accountID, 1, "req-account")
	require.NoError(t, err)
	require.True(t, ok)
	_, err = rdb.ZScore(ctx, accountActiveIndexKey, strconv.FormatInt(accountID, 10)).Result()
	require.NoError(t, err, "account slot acquire should add account to active index")

	require.NoError(t, cache.ReleaseAccountSlot(ctx, accountID, "req-account"))
	_, err = rdb.ZScore(ctx, accountActiveIndexKey, strconv.FormatInt(accountID, 10)).Result()
	require.ErrorIs(t, err, redis.Nil, "release of last account slot should remove active index member")

	ok, err = cache.IncrementAccountWaitCount(ctx, accountID, 2)
	require.NoError(t, err)
	require.True(t, ok)
	_, err = rdb.ZScore(ctx, accountActiveIndexKey, strconv.FormatInt(accountID, 10)).Result()
	require.NoError(t, err, "account wait increment should add account to active index")
	require.NoError(t, cache.DecrementAccountWaitCount(ctx, accountID))
	_, err = rdb.ZScore(ctx, accountActiveIndexKey, strconv.FormatInt(accountID, 10)).Result()
	require.ErrorIs(t, err, redis.Nil, "decrement to zero should remove account active index member")

	userID := int64(2101)
	ok, err = cache.AcquireUserSlot(ctx, userID, 1, "req-user")
	require.NoError(t, err)
	require.True(t, ok)
	_, err = rdb.ZScore(ctx, userActiveIndexKey, strconv.FormatInt(userID, 10)).Result()
	require.NoError(t, err, "user slot acquire should add user to active index")

	require.NoError(t, cache.ReleaseUserSlot(ctx, userID, "req-user"))
	_, err = rdb.ZScore(ctx, userActiveIndexKey, strconv.FormatInt(userID, 10)).Result()
	require.ErrorIs(t, err, redis.Nil, "release of last user slot should remove active index member")

	ok, err = cache.IncrementWaitCount(ctx, userID, 2)
	require.NoError(t, err)
	require.True(t, ok)
	_, err = rdb.ZScore(ctx, userActiveIndexKey, strconv.FormatInt(userID, 10)).Result()
	require.NoError(t, err, "user wait increment should add user to active index")
	require.NoError(t, cache.DecrementWaitCount(ctx, userID))
	_, err = rdb.ZScore(ctx, userActiveIndexKey, strconv.FormatInt(userID, 10)).Result()
	require.ErrorIs(t, err, redis.Nil, "decrement to zero should remove user active index member")
}

func TestConcurrencyCacheCleanupExpiredIndexCandidatesCoversAccountAndUser(t *testing.T) {
	ctx := context.Background()
	cache, rdb := newConcurrencyCacheWithMiniRedis(t)
	now := redisNowSecondsForTest(t, ctx, rdb)
	expiredSlotScore := float64(now - int64(cache.slotTTLSeconds) - 5)
	expiredIndexScore := float64(now - 1)

	accountID := int64(1201)
	userID := int64(2201)
	accountMember := strconv.FormatInt(accountID, 10)
	userMember := strconv.FormatInt(userID, 10)
	require.NoError(t, rdb.ZAdd(ctx, accountSlotKey(accountID), redis.Z{Score: expiredSlotScore, Member: "dead-account-req"}).Err())
	require.NoError(t, rdb.ZAdd(ctx, userSlotKey(userID), redis.Z{Score: expiredSlotScore, Member: "dead-user-req"}).Err())
	require.NoError(t, rdb.ZAdd(ctx, accountActiveIndexKey, redis.Z{Score: expiredIndexScore, Member: accountMember}).Err())
	require.NoError(t, rdb.ZAdd(ctx, userActiveIndexKey, redis.Z{Score: expiredIndexScore, Member: userMember}).Err())

	require.NoError(t, cache.CleanupExpiredAccountSlotKeys(ctx))

	exists, err := rdb.Exists(ctx, accountSlotKey(accountID)).Result()
	require.NoError(t, err)
	require.EqualValues(t, 0, exists)
	exists, err = rdb.Exists(ctx, userSlotKey(userID)).Result()
	require.NoError(t, err)
	require.EqualValues(t, 0, exists)
	_, err = rdb.ZScore(ctx, accountActiveIndexKey, accountMember).Result()
	require.ErrorIs(t, err, redis.Nil)
	_, err = rdb.ZScore(ctx, userActiveIndexKey, userMember).Result()
	require.ErrorIs(t, err, redis.Nil)
}

func TestConcurrencyCacheCleanupStaleProcessSlotsUsesIndexesAndSweepsLegacyWaitOnce(t *testing.T) {
	ctx := context.Background()
	cache, rdb := newConcurrencyCacheWithMiniRedis(t)
	now := redisNowSecondsForTest(t, ctx, rdb)

	legacyAccountID := int64(1301)
	legacyUserID := int64(2301)
	require.NoError(t, rdb.Set(ctx, accountWaitKey(legacyAccountID), 1, time.Minute).Err())
	require.NoError(t, rdb.Set(ctx, waitQueueKey(legacyUserID), 1, time.Minute).Err())
	require.NoError(t, cache.CleanupStaleProcessSlots(ctx, "keep-"))
	_, err := rdb.Get(ctx, accountWaitKey(legacyAccountID)).Result()
	require.ErrorIs(t, err, redis.Nil, "first startup should sweep legacy account wait keys")
	_, err = rdb.Get(ctx, waitQueueKey(legacyUserID)).Result()
	require.ErrorIs(t, err, redis.Nil, "first startup should sweep legacy user wait keys")
	exists, err := rdb.Exists(ctx, legacyWaitSweepMarkerKey).Result()
	require.NoError(t, err)
	require.EqualValues(t, 1, exists)

	require.NoError(t, rdb.Set(ctx, accountWaitKey(legacyAccountID), 1, time.Minute).Err())
	require.NoError(t, rdb.Set(ctx, waitQueueKey(legacyUserID), 1, time.Minute).Err())
	require.NoError(t, cache.CleanupStaleProcessSlots(ctx, "keep-"))
	exists, err = rdb.Exists(ctx, accountWaitKey(legacyAccountID), waitQueueKey(legacyUserID)).Result()
	require.NoError(t, err)
	require.EqualValues(t, 2, exists, "legacy sweep marker should make the broad wait-key scan one-shot")

	accountID := int64(1302)
	userID := int64(2302)
	expiredIndexScore := float64(now - 1)
	freshSlotScore := float64(now)
	require.NoError(t, rdb.ZAdd(ctx, accountSlotKey(accountID),
		redis.Z{Score: freshSlotScore, Member: "oldproc-account"},
		redis.Z{Score: freshSlotScore, Member: "keep-account"},
	).Err())
	require.NoError(t, rdb.ZAdd(ctx, userSlotKey(userID),
		redis.Z{Score: freshSlotScore, Member: "oldproc-user"},
		redis.Z{Score: freshSlotScore, Member: "keep-user"},
	).Err())
	require.NoError(t, rdb.Set(ctx, accountWaitKey(accountID), 1, time.Minute).Err())
	require.NoError(t, rdb.Set(ctx, waitQueueKey(userID), 1, time.Minute).Err())
	require.NoError(t, rdb.ZAdd(ctx, accountActiveIndexKey, redis.Z{Score: expiredIndexScore, Member: strconv.FormatInt(accountID, 10)}).Err())
	require.NoError(t, rdb.ZAdd(ctx, userActiveIndexKey, redis.Z{Score: expiredIndexScore, Member: strconv.FormatInt(userID, 10)}).Err())

	require.NoError(t, cache.CleanupStaleProcessSlots(ctx, "keep-"))

	accountSlots, err := rdb.ZRange(ctx, accountSlotKey(accountID), 0, -1).Result()
	require.NoError(t, err)
	require.Equal(t, []string{"keep-account"}, accountSlots)
	userSlots, err := rdb.ZRange(ctx, userSlotKey(userID), 0, -1).Result()
	require.NoError(t, err)
	require.Equal(t, []string{"keep-user"}, userSlots)
	_, err = rdb.Get(ctx, accountWaitKey(accountID)).Result()
	require.ErrorIs(t, err, redis.Nil)
	_, err = rdb.Get(ctx, waitQueueKey(userID)).Result()
	require.ErrorIs(t, err, redis.Nil)
	_, err = rdb.ZScore(ctx, accountActiveIndexKey, strconv.FormatInt(accountID, 10)).Result()
	require.NoError(t, err, "remaining current-process account slot should keep index member")
	_, err = rdb.ZScore(ctx, userActiveIndexKey, strconv.FormatInt(userID, 10)).Result()
	require.NoError(t, err, "remaining current-process user slot should keep index member")
}
