package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestEnqueueSchedulerOutboxDedupUsesDedupKeyConflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	accountID := int64(42)
	payload := map[string]any{"group_ids": []int64{7, 8}}
	payloadJSON, err := json.Marshal(payload)
	require.NoError(t, err)
	expectedKey := schedulerOutboxDedupKey(service.SchedulerOutboxEventAccountChanged, &accountID, nil, payloadJSON)

	mock.ExpectExec(`INSERT INTO scheduler_outbox \(event_type, account_id, group_id, payload, dedup_key\)(?s).*ON CONFLICT \(dedup_key\) WHERE dedup_key IS NOT NULL DO NOTHING`).
		WithArgs(service.SchedulerOutboxEventAccountChanged, sqlmock.AnyArg(), nil, sqlmock.AnyArg(), expectedKey).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = enqueueSchedulerOutbox(context.Background(), db, service.SchedulerOutboxEventAccountChanged, &accountID, nil, payload)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEnqueueSchedulerOutboxNonDedupEventDoesNotUseDedupKey(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	accountID := int64(42)
	payload := map[string]any{"last_used_at": "2026-07-07T00:00:00Z"}
	mock.ExpectExec(`INSERT INTO scheduler_outbox \(event_type, account_id, group_id, payload\)`).
		WithArgs(service.SchedulerOutboxEventAccountLastUsed, sqlmock.AnyArg(), nil, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = enqueueSchedulerOutbox(context.Background(), db, service.SchedulerOutboxEventAccountLastUsed, &accountID, nil, payload)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSchedulerOutboxListAfterAndReleaseDedupClearsClaimedDedupKeys(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	createdAt := time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`WITH selected AS MATERIALIZED(?s).*SET dedup_key = NULL`).
		WithArgs(int64(7), 100).
		WillReturnRows(sqlmock.NewRows([]string{"id", "event_type", "account_id", "group_id", "payload", "created_at"}).
			AddRow(int64(8), service.SchedulerOutboxEventAccountChanged, int64(42), nil, []byte(`{"group_ids":[7]}`), createdAt))

	repo := &schedulerOutboxRepository{db: db}
	events, err := repo.ListAfterAndReleaseDedup(context.Background(), 7, 100)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, int64(8), events[0].ID)
	require.NotNil(t, events[0].AccountID)
	require.Equal(t, int64(42), *events[0].AccountID)
	require.Equal(t, []any{float64(7)}, events[0].Payload["group_ids"])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSchedulerOutboxDedupKeyIsStableAndPayloadAware(t *testing.T) {
	accountID := int64(42)
	groupID := int64(7)
	payloadA := []byte(`{"group_ids":[7]}`)
	payloadB := []byte(`{"group_ids":[8]}`)

	keyA1 := schedulerOutboxDedupKey(service.SchedulerOutboxEventAccountChanged, &accountID, nil, payloadA)
	keyA2 := schedulerOutboxDedupKey(service.SchedulerOutboxEventAccountChanged, &accountID, nil, payloadA)
	keyB := schedulerOutboxDedupKey(service.SchedulerOutboxEventAccountChanged, &accountID, nil, payloadB)
	groupKey := schedulerOutboxDedupKey(service.SchedulerOutboxEventGroupChanged, nil, &groupID, nil)

	require.NotEmpty(t, keyA1)
	require.Equal(t, keyA1, keyA2)
	require.NotEqual(t, keyA1, keyB)
	require.NotEqual(t, keyA1, groupKey)
}

func TestSchedulerOutboxDedupKeyTreatsEmptyGroupPayloadAsLiteralNil(t *testing.T) {
	accountID := int64(42)
	keyLiteralNil := schedulerOutboxDedupKey(service.SchedulerOutboxEventAccountChanged, &accountID, nil, nil)

	var payload any = buildSchedulerGroupPayload(nil)
	require.Nil(t, payload, "empty group payload must be untyped nil to avoid typed-nil JSON null dedup drift")

	var payloadJSON []byte
	if payload != nil {
		encoded, err := json.Marshal(payload)
		require.NoError(t, err)
		payloadJSON = encoded
	}
	keyEmptyGroups := schedulerOutboxDedupKey(service.SchedulerOutboxEventAccountChanged, &accountID, nil, payloadJSON)
	require.Equal(t, keyLiteralNil, keyEmptyGroups)
}
