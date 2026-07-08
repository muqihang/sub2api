package dto

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserSubscriptionFromServiceAdminIncludesRevokedAt(t *testing.T) {
	revokedAt := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	sub := &service.UserSubscription{
		ID:        1,
		UserID:    10,
		GroupID:   20,
		Status:    service.SubscriptionStatusRevoked,
		CreatedAt: revokedAt.Add(-time.Hour),
		UpdatedAt: revokedAt,
		DeletedAt: &revokedAt,
	}

	got := UserSubscriptionFromServiceAdmin(sub)

	require.NotNil(t, got)
	require.Equal(t, service.SubscriptionStatusRevoked, got.Status)
	require.Equal(t, &revokedAt, got.RevokedAt)
}
