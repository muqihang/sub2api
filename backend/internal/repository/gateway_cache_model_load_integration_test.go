//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// ============ Gateway Cache 模型负载统计集成测试 ============

type GatewayCacheModelLoadSuite struct {
	suite.Suite
}

func TestGatewayCacheModelLoadSuite(t *testing.T) {
	suite.Run(t, new(GatewayCacheModelLoadSuite))
}

func (s *GatewayCacheModelLoadSuite) TestIncrModelCallCount_Basic() {
	t := s.T()
	rdb := testRedis(t)
	cache := &gatewayCache{rdb: rdb}
	ctx := context.Background()

	accountID := int64(123)
	model := "claude-sonnet-4-20250514"

	// 首次调用应返回 1
	count1, err := cache.IncrModelCallCount(ctx, accountID, model)
	require.NoError(t, err)
	require.Equal(t, int64(1), count1)

	// 第二次调用应返回 2
	count2, err := cache.IncrModelCallCount(ctx, accountID, model)
	require.NoError(t, err)
	require.Equal(t, int64(2), count2)

	// 第三次调用应返回 3
	count3, err := cache.IncrModelCallCount(ctx, accountID, model)
	require.NoError(t, err)
	require.Equal(t, int64(3), count3)
}

func (s *GatewayCacheModelLoadSuite) TestIncrModelCallCount_DifferentModels() {
	t := s.T()
	rdb := testRedis(t)
	cache := &gatewayCache{rdb: rdb}
	ctx := context.Background()

	accountID := int64(456)
	model1 := "claude-sonnet-4-20250514"
	model2 := "claude-opus-4-5-20251101"

	// 不同模型应该独立计数
	count1, err := cache.IncrModelCallCount(ctx, accountID, model1)
	require.NoError(t, err)
	require.Equal(t, int64(1), count1)

	count2, err := cache.IncrModelCallCount(ctx, accountID, model2)
	require.NoError(t, err)
	require.Equal(t, int64(1), count2)

	count1Again, err := cache.IncrModelCallCount(ctx, accountID, model1)
	require.NoError(t, err)
	require.Equal(t, int64(2), count1Again)
}

func (s *GatewayCacheModelLoadSuite) TestIncrModelCallCount_DifferentAccounts() {
	t := s.T()
	rdb := testRedis(t)
	cache := &gatewayCache{rdb: rdb}
	ctx := context.Background()

	account1 := int64(111)
	account2 := int64(222)
	model := "gemini-2.5-pro"

	// 不同账号应该独立计数
	count1, err := cache.IncrModelCallCount(ctx, account1, model)
	require.NoError(t, err)
	require.Equal(t, int64(1), count1)

	count2, err := cache.IncrModelCallCount(ctx, account2, model)
	require.NoError(t, err)
	require.Equal(t, int64(1), count2)
}

func (s *GatewayCacheModelLoadSuite) TestGetModelLoadBatch_Empty() {
	t := s.T()
	rdb := testRedis(t)
	cache := &gatewayCache{rdb: rdb}
	ctx := context.Background()

	result, err := cache.GetModelLoadBatch(ctx, []int64{}, "any-model")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result)
}

func (s *GatewayCacheModelLoadSuite) TestGetModelLoadBatch_NonExistent() {
	t := s.T()
	rdb := testRedis(t)
	cache := &gatewayCache{rdb: rdb}
	ctx := context.Background()

	// 查询不存在的账号应返回零值
	result, err := cache.GetModelLoadBatch(ctx, []int64{9999, 9998}, "claude-sonnet-4-20250514")
	require.NoError(t, err)
	require.Len(t, result, 2)

	require.Equal(t, int64(0), result[9999].CallCount)
	require.True(t, result[9999].LastUsedAt.IsZero())
	require.Equal(t, int64(0), result[9998].CallCount)
	require.True(t, result[9998].LastUsedAt.IsZero())
}

func (s *GatewayCacheModelLoadSuite) TestGetModelLoadBatch_AfterIncrement() {
	t := s.T()
	rdb := testRedis(t)
	cache := &gatewayCache{rdb: rdb}
	ctx := context.Background()

	accountID := int64(789)
	model := "claude-sonnet-4-20250514"

	// 先增加调用次数
	beforeIncr := time.Now()
	_, err := cache.IncrModelCallCount(ctx, accountID, model)
	require.NoError(t, err)
	_, err = cache.IncrModelCallCount(ctx, accountID, model)
	require.NoError(t, err)
	_, err = cache.IncrModelCallCount(ctx, accountID, model)
	require.NoError(t, err)
	afterIncr := time.Now()

	// 获取负载信息
	result, err := cache.GetModelLoadBatch(ctx, []int64{accountID}, model)
	require.NoError(t, err)
	require.Len(t, result, 1)

	loadInfo := result[accountID]
	require.NotNil(t, loadInfo)
	require.Equal(t, int64(3), loadInfo.CallCount)
	require.False(t, loadInfo.LastUsedAt.IsZero())
	// LastUsedAt 应该在 beforeIncr 和 afterIncr 之间
	require.True(t, loadInfo.LastUsedAt.After(beforeIncr.Add(-time.Second)) || loadInfo.LastUsedAt.Equal(beforeIncr))
	require.True(t, loadInfo.LastUsedAt.Before(afterIncr.Add(time.Second)) || loadInfo.LastUsedAt.Equal(afterIncr))
}

func (s *GatewayCacheModelLoadSuite) TestGetModelLoadBatch_MultipleAccounts() {
	t := s.T()
	rdb := testRedis(t)
	cache := &gatewayCache{rdb: rdb}
	ctx := context.Background()

	model := "claude-opus-4-5-20251101"
	account1 := int64(1001)
	account2 := int64(1002)
	account3 := int64(1003) // 不调用

	// account1 调用 2 次
	_, err := cache.IncrModelCallCount(ctx, account1, model)
	require.NoError(t, err)
	_, err = cache.IncrModelCallCount(ctx, account1, model)
	require.NoError(t, err)

	// account2 调用 5 次
	for i := 0; i < 5; i++ {
		_, err = cache.IncrModelCallCount(ctx, account2, model)
		require.NoError(t, err)
	}

	// 批量获取
	result, err := cache.GetModelLoadBatch(ctx, []int64{account1, account2, account3}, model)
	require.NoError(t, err)
	require.Len(t, result, 3)

	require.Equal(t, int64(2), result[account1].CallCount)
	require.False(t, result[account1].LastUsedAt.IsZero())

	require.Equal(t, int64(5), result[account2].CallCount)
	require.False(t, result[account2].LastUsedAt.IsZero())

	require.Equal(t, int64(0), result[account3].CallCount)
	require.True(t, result[account3].LastUsedAt.IsZero())
}

func (s *GatewayCacheModelLoadSuite) TestGetModelLoadBatch_ModelIsolation() {
	t := s.T()
	rdb := testRedis(t)
	cache := &gatewayCache{rdb: rdb}
	ctx := context.Background()

	accountID := int64(2001)
	model1 := "claude-sonnet-4-20250514"
	model2 := "gemini-2.5-pro"

	// 对 model1 调用 3 次
	for i := 0; i < 3; i++ {
		_, err := cache.IncrModelCallCount(ctx, accountID, model1)
		require.NoError(t, err)
	}

	// 获取 model1 的负载
	result1, err := cache.GetModelLoadBatch(ctx, []int64{accountID}, model1)
	require.NoError(t, err)
	require.Equal(t, int64(3), result1[accountID].CallCount)

	// 获取 model2 的负载（应该为 0）
	result2, err := cache.GetModelLoadBatch(ctx, []int64{accountID}, model2)
	require.NoError(t, err)
	require.Equal(t, int64(0), result2[accountID].CallCount)
}

func (s *GatewayCacheModelLoadSuite) TestOpenAICacheHealth_RecordAndBatchGet() {
	t := s.T()
	rdb := testRedis(t)
	cache := &gatewayCache{rdb: rdb}
	ctx := context.Background()

	accountID := int64(3001)
	model := "gpt-5.3-codex"

	err := cache.RecordOpenAICacheSample(ctx, accountID, model, 12000, 6000)
	require.NoError(t, err)
	err = cache.RecordOpenAICacheSample(ctx, accountID, model, 24000, 12000)
	require.NoError(t, err)

	result, err := cache.GetOpenAICacheHealthBatch(ctx, []int64{accountID, 3002}, model)
	require.NoError(t, err)
	require.Len(t, result, 2)

	info := result[accountID]
	require.NotNil(t, info)
	require.Equal(t, int64(36000), info.InputTokens)
	require.Equal(t, int64(18000), info.CacheReadTokens)
	require.Equal(t, int64(2), info.Samples)
	require.False(t, info.UpdatedAt.IsZero())

	empty := result[3002]
	require.NotNil(t, empty)
	require.Equal(t, int64(0), empty.InputTokens)
	require.Equal(t, int64(0), empty.CacheReadTokens)
	require.Equal(t, int64(0), empty.Samples)
}

func (s *GatewayCacheModelLoadSuite) TestOpenAICacheHealth_IsolationByAccountAndModel() {
	t := s.T()
	rdb := testRedis(t)
	cache := &gatewayCache{rdb: rdb}
	ctx := context.Background()

	err := cache.RecordOpenAICacheSample(ctx, 4001, "gpt-5.3-codex", 20000, 10000)
	require.NoError(t, err)
	err = cache.RecordOpenAICacheSample(ctx, 4001, "gpt-5.1-codex-mini", 20000, 2000)
	require.NoError(t, err)
	err = cache.RecordOpenAICacheSample(ctx, 4002, "gpt-5.3-codex", 20000, 4000)
	require.NoError(t, err)

	result, err := cache.GetOpenAICacheHealthBatch(ctx, []int64{4001, 4002}, "gpt-5.3-codex")
	require.NoError(t, err)
	require.Len(t, result, 2)

	require.Equal(t, int64(20000), result[4001].InputTokens)
	require.Equal(t, int64(10000), result[4001].CacheReadTokens)
	require.Equal(t, int64(1), result[4001].Samples)

	require.Equal(t, int64(20000), result[4002].InputTokens)
	require.Equal(t, int64(4000), result[4002].CacheReadTokens)
	require.Equal(t, int64(1), result[4002].Samples)

	miniResult, err := cache.GetOpenAICacheHealthBatch(ctx, []int64{4001}, "gpt-5.1-codex-mini")
	require.NoError(t, err)
	require.Equal(t, int64(20000), miniResult[4001].InputTokens)
	require.Equal(t, int64(2000), miniResult[4001].CacheReadTokens)
}

func (s *GatewayCacheModelLoadSuite) TestOpenAICacheHealth_TTLIsApplied() {
	t := s.T()
	rdb := testRedis(t)
	cache := &gatewayCache{rdb: rdb}
	ctx := context.Background()

	accountID := int64(5001)
	model := "gpt-5.3-codex"

	err := cache.RecordOpenAICacheSample(ctx, accountID, model, 12000, 6000)
	require.NoError(t, err)

	ttl, err := rdb.TTL(ctx, openAICacheHealthKey(accountID, model)).Result()
	require.NoError(t, err)
	require.Greater(t, ttl, time.Duration(0))
	require.LessOrEqual(t, ttl, openAICacheHealthTTL)
}

// ============ 辅助函数测试 ============

func (s *GatewayCacheModelLoadSuite) TestModelLoadKey_Format() {
	t := s.T()

	key := modelLoadKey(123, "claude-sonnet-4")
	require.Equal(t, "ag:model_load:123:claude-sonnet-4", key)
}

func (s *GatewayCacheModelLoadSuite) TestModelLastUsedKey_Format() {
	t := s.T()

	key := modelLastUsedKey(456, "gemini-2.5-pro")
	require.Equal(t, "ag:model_last_used:456:gemini-2.5-pro", key)
}
