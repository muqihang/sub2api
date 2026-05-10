package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyRestrictionSnapshotRoundTripPreservesRestrictionAndEntitlement(t *testing.T) {
	svc := NewAPIKeyService(nil, nil, nil, nil, nil, nil, &config.Config{})

	groupID := int64(17)
	restrictedClientProduct := "zhumeng_augment"
	apiKey := &APIKey{
		ID:                      21,
		UserID:                  34,
		GroupID:                 &groupID,
		Key:                     "sk-snapshot-roundtrip",
		Status:                  StatusActive,
		RestrictedClientProduct: &restrictedClientProduct,
		User: &User{
			ID:          34,
			Status:      StatusActive,
			Role:        RoleUser,
			Balance:     12,
			Concurrency: 2,
		},
		Group: &Group{
			ID:                     groupID,
			Name:                   "augment-entitled",
			Platform:               PlatformAnthropic,
			Status:                 StatusActive,
			SubscriptionType:       SubscriptionTypeStandard,
			RateMultiplier:         1,
			AugmentGatewayEntitled: true,
		},
	}

	snapshot := svc.snapshotFromAPIKey(context.Background(), apiKey)
	require.NotNil(t, snapshot)
	require.Equal(t, apiKeyAuthSnapshotVersion, snapshot.Version)
	require.NotNil(t, snapshot.RestrictedClientProduct)
	require.Equal(t, restrictedClientProduct, *snapshot.RestrictedClientProduct)
	require.NotNil(t, snapshot.Group)
	require.True(t, snapshot.Group.AugmentGatewayEntitled)

	roundTrip := svc.snapshotToAPIKey(apiKey.Key, snapshot)
	require.NotNil(t, roundTrip)
	require.NotNil(t, roundTrip.RestrictedClientProduct)
	require.Equal(t, restrictedClientProduct, *roundTrip.RestrictedClientProduct)
	require.NotNil(t, roundTrip.Group)
	require.True(t, roundTrip.Group.AugmentGatewayEntitled)
}

func TestAPIKeyRestrictionSnapshotVersionMismatchBypassesCache(t *testing.T) {
	svc := NewAPIKeyService(nil, nil, nil, nil, nil, nil, &config.Config{})

	groupID := int64(17)
	entry := &APIKeyAuthCacheEntry{
		Snapshot: &APIKeyAuthSnapshot{
			Version:  apiKeyAuthSnapshotVersion - 1,
			APIKeyID: 21,
			UserID:   34,
			GroupID:  &groupID,
			Status:   StatusActive,
			User: APIKeyAuthUserSnapshot{
				ID:          34,
				Status:      StatusActive,
				Role:        RoleUser,
				Balance:     12,
				Concurrency: 2,
			},
		},
	}

	apiKey, ok, err := svc.applyAuthCacheEntry("sk-legacy-snapshot", entry)
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, apiKey)
}
