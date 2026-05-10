package repository

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpsRepositoryListRequestDetails_IncludesAndFiltersEntityFields(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := &opsRepository{db: db}
	start := time.Date(2026, 5, 9, 1, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	entityID := int64(123)

	countQuery := regexp.QuoteMeta("SELECT COUNT(1) FROM combined") + `(?s).*entity_id = \$3.*entity_type = \$4.*claimed_entity_id = \$5`
	mock.ExpectQuery(countQuery).
		WithArgs(start, end, entityID, service.EntityTypeWorkspace, "workspace-alpha").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(1)))

	listQuery := `(?s)SELECT\s+kind,\s+created_at,\s+request_id,\s+platform,\s+model,\s+duration_ms,\s+status_code,\s+error_id,\s+phase,\s+severity,\s+message,\s+user_id,\s+api_key_id,\s+account_id,\s+group_id,\s+entity_id,\s+entity_type,\s+claimed_entity_id,\s+stream\s+FROM combined.*entity_id = \$3.*entity_type = \$4.*claimed_entity_id = \$5`
	rows := sqlmock.NewRows([]string{
		"kind",
		"created_at",
		"request_id",
		"platform",
		"model",
		"duration_ms",
		"status_code",
		"error_id",
		"phase",
		"severity",
		"message",
		"user_id",
		"api_key_id",
		"account_id",
		"group_id",
		"entity_id",
		"entity_type",
		"claimed_entity_id",
		"stream",
	}).AddRow(
		"success",
		start,
		"req-entity",
		"openai",
		"gpt-4.1",
		sql.NullInt64{Int64: 42, Valid: true},
		sql.NullInt64{},
		sql.NullInt64{},
		sql.NullString{},
		sql.NullString{},
		sql.NullString{},
		sql.NullInt64{Int64: 7, Valid: true},
		sql.NullInt64{Int64: 8, Valid: true},
		sql.NullInt64{Int64: 9, Valid: true},
		sql.NullInt64{Int64: 10, Valid: true},
		sql.NullInt64{Int64: entityID, Valid: true},
		sql.NullString{String: service.EntityTypeWorkspace, Valid: true},
		sql.NullString{String: "workspace-alpha", Valid: true},
		false,
	)
	mock.ExpectQuery(listQuery).
		WithArgs(start, end, entityID, service.EntityTypeWorkspace, "workspace-alpha", 50, 0).
		WillReturnRows(rows)

	got, total, err := repo.ListRequestDetails(context.Background(), &service.OpsRequestDetailFilter{
		StartTime:       &start,
		EndTime:         &end,
		EntityID:        &entityID,
		EntityType:      service.EntityTypeWorkspace,
		ClaimedEntityID: "workspace-alpha",
		Page:            1,
		PageSize:        50,
	})

	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].EntityID)
	require.Equal(t, entityID, *got[0].EntityID)
	require.Equal(t, service.EntityTypeWorkspace, got[0].EntityType)
	require.Equal(t, "workspace-alpha", got[0].ClaimedEntityID)
	require.NoError(t, mock.ExpectationsWereMet())
}
