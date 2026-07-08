package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type revokeCacheUserSubRepoStub struct {
	userSubRepoNoop

	sub            *UserSubscription
	deleted        bool
	getActiveCalls int
}

func (r *revokeCacheUserSubRepoStub) GetByID(_ context.Context, id int64) (*UserSubscription, error) {
	if r.sub == nil || r.sub.ID != id || r.deleted {
		return nil, ErrSubscriptionNotFound
	}
	cp := *r.sub
	return &cp, nil
}

func (r *revokeCacheUserSubRepoStub) Delete(_ context.Context, id int64) error {
	if r.sub == nil || r.sub.ID != id || r.deleted {
		return ErrSubscriptionNotFound
	}
	r.deleted = true
	return nil
}

func (r *revokeCacheUserSubRepoStub) GetActiveByUserIDAndGroupID(_ context.Context, userID, groupID int64) (*UserSubscription, error) {
	r.getActiveCalls++
	if r.deleted || r.sub == nil || r.sub.UserID != userID || r.sub.GroupID != groupID {
		return nil, ErrSubscriptionNotFound
	}
	cp := *r.sub
	return &cp, nil
}

func TestRevokeSubscription_InvalidatesL1CacheSynchronously(t *testing.T) {
	repo := &revokeCacheUserSubRepoStub{
		sub: &UserSubscription{
			ID:        1,
			UserID:    10,
			GroupID:   20,
			Status:    SubscriptionStatusActive,
			ExpiresAt: time.Now().Add(time.Hour),
		},
	}
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, &config.Config{
		SubscriptionCache: config.SubscriptionCacheConfig{
			L1Size:       16,
			L1TTLSeconds: 60,
		},
	})
	t.Cleanup(svc.Stop)

	_, err := svc.GetActiveSubscription(context.Background(), 10, 20)
	require.NoError(t, err)
	svc.subCacheL1.Wait()
	require.Equal(t, 1, repo.getActiveCalls)

	err = svc.RevokeSubscription(context.Background(), 1)
	require.NoError(t, err)

	_, err = svc.GetActiveSubscription(context.Background(), 10, 20)
	require.ErrorIs(t, err, ErrSubscriptionNotFound)
	require.Equal(t, 2, repo.getActiveCalls, "revocation should evict old L1 entry before returning")
}

type subscriptionInvalidationCacheStub struct {
	BillingCache
	invalidated []string
	published   []string
}

func (s *subscriptionInvalidationCacheStub) InvalidateSubscriptionCache(_ context.Context, userID, groupID int64) error {
	s.invalidated = append(s.invalidated, subCacheKey(userID, groupID))
	return nil
}

func (s *subscriptionInvalidationCacheStub) PublishSubscriptionCacheInvalidation(_ context.Context, cacheKey string) error {
	s.published = append(s.published, cacheKey)
	return nil
}

func (s *subscriptionInvalidationCacheStub) SubscribeSubscriptionCacheInvalidation(_ context.Context, _ func(cacheKey string)) error {
	return nil
}

func TestRevokeSubscription_InvalidatesBillingCacheAndPublishes(t *testing.T) {
	repo := &revokeCacheUserSubRepoStub{
		sub: &UserSubscription{ID: 2, UserID: 11, GroupID: 21, Status: SubscriptionStatusActive, ExpiresAt: time.Now().Add(time.Hour)},
	}
	cache := &subscriptionInvalidationCacheStub{}
	svc := NewSubscriptionService(groupRepoNoop{}, repo, &BillingCacheService{cache: cache}, nil, nil)
	t.Cleanup(svc.Stop)

	err := svc.RevokeSubscription(context.Background(), 2)
	require.NoError(t, err)

	require.Equal(t, []string{subCacheKey(11, 21)}, cache.invalidated)
	require.Equal(t, []string{subCacheKey(11, 21)}, cache.published)
}
