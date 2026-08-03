package repository

import (
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserSubscriptionEntityToService_MapsSoftDeletedAsRevoked(t *testing.T) {
	deletedAt := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	m := &dbent.UserSubscription{
		ID:        42,
		UserID:    7,
		GroupID:   9,
		StartsAt:  deletedAt.Add(-24 * time.Hour),
		ExpiresAt: deletedAt.Add(24 * time.Hour),
		Status:    service.SubscriptionStatusActive,
		DeletedAt: &deletedAt,
	}

	got := userSubscriptionEntityToService(m)

	require.NotNil(t, got)
	require.Equal(t, service.SubscriptionStatusRevoked, got.Status)
	require.Equal(t, &deletedAt, got.DeletedAt)
}
