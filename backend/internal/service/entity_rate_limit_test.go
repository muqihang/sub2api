package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type entityRateLimitPolicyRepoStub struct {
	policy *EntityRateLimitPolicy
	err    error
	calls  int
}

func (s *entityRateLimitPolicyRepoStub) GetActiveByEntityID(ctx context.Context, entityID int64) (*EntityRateLimitPolicy, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.policy, nil
}

type entityRateLimitCacheStub struct {
	acquireCalls int
	releaseCalls int
	rpmCalls     int
	tpmCalls     int
	costCalls    int

	acquired  bool
	rpmCount  int
	tpmTotal  int
	costTotal float64

	acquireErr error
	rpmErr     error
	tpmErr     error
	costErr    error

	lastAcquireEntityID int64
	lastAcquireLimit    int
	lastReleaseEntityID int64
	lastRPMEntityID     int64
	lastTPMEntityID     int64
	lastTPMTokens       int
	lastCostEntityID    int64
	lastCostAmount      float64
}

func (s *entityRateLimitCacheStub) AcquireEntitySlot(ctx context.Context, entityID int64, maxConcurrency int, requestID string) (bool, error) {
	s.acquireCalls++
	s.lastAcquireEntityID = entityID
	s.lastAcquireLimit = maxConcurrency
	if s.acquireErr != nil {
		return false, s.acquireErr
	}
	return s.acquired, nil
}

func (s *entityRateLimitCacheStub) ReleaseEntitySlot(ctx context.Context, entityID int64, requestID string) error {
	s.releaseCalls++
	s.lastReleaseEntityID = entityID
	return nil
}

func (s *entityRateLimitCacheStub) IncrementEntityRPM(ctx context.Context, entityID int64) (int, error) {
	s.rpmCalls++
	s.lastRPMEntityID = entityID
	if s.rpmErr != nil {
		return 0, s.rpmErr
	}
	return s.rpmCount, nil
}

func (s *entityRateLimitCacheStub) AddEntityTPM(ctx context.Context, entityID int64, tokens int) (int, error) {
	s.tpmCalls++
	s.lastTPMEntityID = entityID
	s.lastTPMTokens = tokens
	if s.tpmErr != nil {
		return 0, s.tpmErr
	}
	return s.tpmTotal, nil
}

func (s *entityRateLimitCacheStub) AddEntityCost(ctx context.Context, entityID int64, amount float64) (float64, error) {
	s.costCalls++
	s.lastCostEntityID = entityID
	s.lastCostAmount = amount
	if s.costErr != nil {
		return 0, s.costErr
	}
	return s.costTotal, nil
}

func resolvedEntityForRateLimitTest() *ResolvedEntity {
	return &ResolvedEntity{
		Entity: Entity{
			ID:         123,
			EntityKey:  "workspace-alpha",
			EntityType: EntityTypeWorkspace,
			Status:     EntityStatusActive,
		},
		Source: EntityResolutionSourceClaimedBinding,
	}
}

func TestEntityRateLimitAdmissionBlocksWhenRPMExceeded(t *testing.T) {
	repo := &entityRateLimitPolicyRepoStub{policy: &EntityRateLimitPolicy{
		ID:       7,
		EntityID: 123,
		Status:   EntityRateLimitPolicyStatusActive,
		RPMLimit: 1,
	}}
	cache := &entityRateLimitCacheStub{rpmCount: 2}
	svc := NewEntityRateLimitService(repo, cache)
	ctx := WithResolvedEntity(context.Background(), resolvedEntityForRateLimitTest())

	decision, release, err := svc.Admit(ctx)

	require.ErrorIs(t, err, ErrEntityRPMExceeded)
	require.Nil(t, decision)
	require.Nil(t, release)
	require.Equal(t, 1, repo.calls)
	require.Equal(t, 1, cache.rpmCalls)
	require.Equal(t, int64(123), cache.lastRPMEntityID)
	require.Zero(t, cache.acquireCalls)
}

func TestEntityRateLimitAdmissionAcquiresAndReleasesConcurrencyOnce(t *testing.T) {
	repo := &entityRateLimitPolicyRepoStub{policy: &EntityRateLimitPolicy{
		ID:               8,
		EntityID:         123,
		Status:           EntityRateLimitPolicyStatusActive,
		ConcurrencyLimit: 1,
	}}
	cache := &entityRateLimitCacheStub{acquired: true}
	svc := NewEntityRateLimitService(repo, cache)
	ctx := WithResolvedEntity(context.Background(), resolvedEntityForRateLimitTest())

	decision, release, err := svc.Admit(ctx)
	require.NoError(t, err)
	require.NotNil(t, decision)
	require.NotNil(t, release)
	require.Equal(t, int64(8), decision.PolicyID)
	require.Equal(t, int64(123), decision.EntityID)
	require.Equal(t, 1, cache.acquireCalls)
	require.Equal(t, int64(123), cache.lastAcquireEntityID)
	require.Equal(t, 1, cache.lastAcquireLimit)

	release()
	release()
	require.Equal(t, 1, cache.releaseCalls)
	require.Equal(t, int64(123), cache.lastReleaseEntityID)
}

func TestEntityRateLimitAdmissionRedisFailureIsExplicitFailOpen(t *testing.T) {
	repo := &entityRateLimitPolicyRepoStub{policy: &EntityRateLimitPolicy{
		ID:               9,
		EntityID:         123,
		Status:           EntityRateLimitPolicyStatusActive,
		RPMLimit:         10,
		ConcurrencyLimit: 2,
	}}
	cache := &entityRateLimitCacheStub{
		acquireErr: errors.New("redis down"),
		rpmErr:     errors.New("redis down"),
	}
	svc := NewEntityRateLimitService(repo, cache)
	ctx := WithResolvedEntity(context.Background(), resolvedEntityForRateLimitTest())

	decision, release, err := svc.Admit(ctx)

	require.NoError(t, err)
	require.NotNil(t, decision)
	require.True(t, decision.RedisFailureFailOpen)
	require.NotEmpty(t, decision.RedisFailureReason)
	require.Nil(t, release)
	require.Equal(t, 1, cache.acquireCalls)
	require.Equal(t, 1, cache.rpmCalls)
}

func TestEntityRateLimitAdmissionDoesNotReadmitExistingDecision(t *testing.T) {
	repo := &entityRateLimitPolicyRepoStub{policy: &EntityRateLimitPolicy{
		ID:       10,
		EntityID: 123,
		Status:   EntityRateLimitPolicyStatusActive,
		RPMLimit: 1,
	}}
	cache := &entityRateLimitCacheStub{rpmCount: 1}
	svc := NewEntityRateLimitService(repo, cache)
	ctx := WithResolvedEntity(context.Background(), resolvedEntityForRateLimitTest())

	decision, release, err := svc.Admit(ctx)
	require.NoError(t, err)
	require.NotNil(t, decision)
	require.Nil(t, release)

	ctx = WithEntityRateLimitDecision(ctx, decision)
	secondDecision, secondRelease, err := svc.Admit(ctx)
	require.NoError(t, err)
	require.Equal(t, decision.DecisionID, secondDecision.DecisionID)
	require.Nil(t, secondRelease)
	require.Equal(t, 1, repo.calls)
	require.Equal(t, 1, cache.rpmCalls)
}

func TestEntityRateLimitReconcileAccountsTokensAndCostByEntity(t *testing.T) {
	repo := &entityRateLimitPolicyRepoStub{policy: &EntityRateLimitPolicy{
		ID:           11,
		EntityID:     123,
		Status:       EntityRateLimitPolicyStatusActive,
		TPMLimit:     1000,
		CostLimitUSD: 5,
	}}
	cache := &entityRateLimitCacheStub{tpmTotal: 42, costTotal: 1.25}
	svc := NewEntityRateLimitService(repo, cache)
	decision := &EntityRateLimitDecision{
		PolicyID:     11,
		EntityID:     123,
		EntityKey:    "workspace-alpha",
		TPMLimit:     1000,
		CostLimitUSD: 5,
	}
	ctx := WithEntityRateLimitDecision(context.Background(), decision)
	log := &UsageLog{
		InputTokens:         10,
		OutputTokens:        20,
		CacheCreationTokens: 7,
		CacheReadTokens:     5,
		ActualCost:          1.25,
	}

	require.NoError(t, svc.ReconcileUsage(ctx, log))

	require.Equal(t, 1, cache.tpmCalls)
	require.Equal(t, int64(123), cache.lastTPMEntityID)
	require.Equal(t, 42, cache.lastTPMTokens)
	require.Equal(t, 1, cache.costCalls)
	require.Equal(t, int64(123), cache.lastCostEntityID)
	require.Equal(t, 1.25, cache.lastCostAmount)
}

func TestEntityRateLimitReconcileBlocksWhenTPMExceeded(t *testing.T) {
	cache := &entityRateLimitCacheStub{tpmTotal: 1001}
	svc := NewEntityRateLimitService(nil, cache)
	ctx := WithEntityRateLimitDecision(context.Background(), &EntityRateLimitDecision{
		PolicyID: 11,
		EntityID: 123,
		TPMLimit: 1000,
	})
	log := &UsageLog{
		InputTokens:  600,
		OutputTokens: 401,
	}

	err := svc.ReconcileUsage(ctx, log)

	require.ErrorIs(t, err, ErrEntityTPMExceeded)
	require.Equal(t, 1, cache.tpmCalls)
	require.Zero(t, cache.costCalls)
}

func TestEntityRateLimitReconcileBlocksWhenCostExceeded(t *testing.T) {
	cache := &entityRateLimitCacheStub{costTotal: 5.01}
	svc := NewEntityRateLimitService(nil, cache)
	ctx := WithEntityRateLimitDecision(context.Background(), &EntityRateLimitDecision{
		PolicyID:     11,
		EntityID:     123,
		CostLimitUSD: 5,
	})
	log := &UsageLog{
		InputTokens: 10,
		ActualCost:  5.01,
	}

	err := svc.ReconcileUsage(ctx, log)

	require.ErrorIs(t, err, ErrEntityCostExceeded)
	require.Zero(t, cache.tpmCalls)
	require.Equal(t, 1, cache.costCalls)
}

func TestEntityRateLimitReconcileRedisFailureIsFailOpen(t *testing.T) {
	cache := &entityRateLimitCacheStub{
		tpmErr:  errors.New("redis down"),
		costErr: errors.New("redis down"),
	}
	svc := NewEntityRateLimitService(nil, cache)
	ctx := WithEntityRateLimitDecision(context.Background(), &EntityRateLimitDecision{
		PolicyID:     11,
		EntityID:     123,
		EntityKey:    "workspace-alpha",
		TPMLimit:     1000,
		CostLimitUSD: 5,
	})
	log := &UsageLog{
		InputTokens:  10,
		OutputTokens: 20,
		ActualCost:   1.25,
	}

	require.NoError(t, svc.ReconcileUsage(ctx, log))
	require.Equal(t, 1, cache.tpmCalls)
	require.Equal(t, 1, cache.costCalls)
}

func TestEntityContextPropagationCopiesResolvedAndDecisionMetadata(t *testing.T) {
	resolved := resolvedEntityForRateLimitTest()
	source := WithResolvedEntity(context.Background(), resolved)
	source = WithClaimedEntityID(source, "workspace-alpha")
	source = WithEntityRateLimitDecision(source, &EntityRateLimitDecision{
		PolicyID:   12,
		EntityID:   123,
		EntityKey:  "workspace-alpha",
		DecisionID: "decision-1",
	})

	copied := ContextWithEntityMetadataFrom(context.Background(), source)

	gotResolved, ok := ResolvedEntityFromContext(copied)
	require.True(t, ok)
	require.Equal(t, int64(123), gotResolved.Entity.ID)
	require.Equal(t, "workspace-alpha", ClaimedEntityIDFromContext(copied))
	gotDecision, ok := EntityRateLimitDecisionFromContext(copied)
	require.True(t, ok)
	require.Equal(t, int64(12), gotDecision.PolicyID)
	require.Equal(t, "decision-1", gotDecision.DecisionID)
}
