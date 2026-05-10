package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpsServiceListRequestDetails_PropagatesEntityFiltersAndFields(t *testing.T) {
	var gotFilter *OpsRequestDetailFilter
	entityID := int64(123)
	repo := &opsRepoMock{
		ListRequestDetailsFn: func(ctx context.Context, filter *OpsRequestDetailFilter) ([]*OpsRequestDetail, int64, error) {
			gotFilter = filter
			return []*OpsRequestDetail{
				{
					Kind:            OpsRequestKindSuccess,
					RequestID:       "req-entity",
					EntityID:        &entityID,
					EntityType:      EntityTypeWorkspace,
					ClaimedEntityID: "workspace-alpha",
				},
			}, 1, nil
		},
	}
	svc := NewOpsService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	start := time.Date(2026, 5, 9, 1, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	out, err := svc.ListRequestDetails(context.Background(), &OpsRequestDetailFilter{
		StartTime:       &start,
		EndTime:         &end,
		EntityID:        &entityID,
		EntityType:      EntityTypeWorkspace,
		ClaimedEntityID: "workspace-alpha",
	})

	require.NoError(t, err)
	require.NotNil(t, gotFilter)
	require.NotNil(t, gotFilter.EntityID)
	require.Equal(t, int64(123), *gotFilter.EntityID)
	require.Equal(t, EntityTypeWorkspace, gotFilter.EntityType)
	require.Equal(t, "workspace-alpha", gotFilter.ClaimedEntityID)
	require.Len(t, out.Items, 1)
	require.NotNil(t, out.Items[0].EntityID)
	require.Equal(t, int64(123), *out.Items[0].EntityID)
	require.Equal(t, EntityTypeWorkspace, out.Items[0].EntityType)
	require.Equal(t, "workspace-alpha", out.Items[0].ClaimedEntityID)
}
