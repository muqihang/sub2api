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

func TestSchedulerOutboxRepositoryDeleteConsumedUpToUsesBoundedCTEWithGrace(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := &schedulerOutboxRepository{db: db}
	mock.ExpectExec(`WITH doomed AS \((?s).*WHERE id <= \$1\s+AND created_at < NOW\(\) - INTERVAL '10 seconds'(?s).*DELETE FROM scheduler_outbox`).
		WithArgs(int64(42), 5000).
		WillReturnResult(sqlmock.NewResult(0, 17))

	deleted, err := repo.DeleteConsumedUpTo(context.Background(), 42, 5000)

	require.NoError(t, err)
	require.EqualValues(t, 17, deleted)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSchedulerOutboxRepositoryDeleteConsumedUpToSkipsNonPositiveWatermark(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := &schedulerOutboxRepository{db: db}

	deleted, err := repo.DeleteConsumedUpTo(context.Background(), 0, 5000)

	require.NoError(t, err)
	require.EqualValues(t, 0, deleted)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSchedulerOutboxRepositoryTryAcquireCleanupLock(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := &schedulerOutboxRepository{db: db}
	mock.ExpectQuery(`SELECT pg_try_advisory_lock\(hashtext\('scheduler_outbox_cleanup'\)\)`).
		WillReturnRows(sqlmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(true))
	mock.ExpectExec(`SELECT pg_advisory_unlock\(hashtext\('scheduler_outbox_cleanup'\)\)`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	lease, acquired, err := repo.TryAcquireCleanupLock(context.Background())
	require.NoError(t, err)
	require.True(t, acquired)
	require.NotNil(t, lease)

	lease.Release()

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSchedulerOutboxRepositoryTryAcquireCleanupLockUnavailable(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := &schedulerOutboxRepository{db: db}
	mock.ExpectQuery(`SELECT pg_try_advisory_lock\(hashtext\('scheduler_outbox_cleanup'\)\)`).
		WillReturnRows(sqlmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(false))

	lease, acquired, err := repo.TryAcquireCleanupLock(context.Background())
	require.NoError(t, err)
	require.False(t, acquired)
	require.Nil(t, lease)
	require.NoError(t, mock.ExpectationsWereMet())
}
