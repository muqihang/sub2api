package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyServiceUpdateReactivatesQuotaExhaustedWhenQuotaUnlimited(t *testing.T) {
	repo := &apiKeyAugmentRepoStub{
		current: &APIKey{
			ID:        10,
			UserID:    7,
			Key:       "sk-test-unlimited",
			Status:    StatusAPIKeyQuotaExhausted,
			Quota:     10,
			QuotaUsed: 12,
		},
	}
	svc := &APIKeyService{apiKeyRepo: repo}
	quota := 0.0

	updated, err := svc.Update(context.Background(), 10, 7, UpdateAPIKeyRequest{Quota: &quota})

	require.NoError(t, err)
	require.Equal(t, StatusActive, updated.Status)
	require.Equal(t, 0.0, updated.Quota)
	require.NotNil(t, repo.updated)
	require.Equal(t, StatusActive, repo.updated.Status)
	require.Equal(t, 0.0, repo.updated.Quota)
}
