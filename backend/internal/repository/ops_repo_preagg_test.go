package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestOpsRepositoryUpsertHourlyMetrics_UsesWidenedUsageLogCompatibleProjection(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := &opsRepository{db: db}
	start := time.Date(2026, 5, 9, 1, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	mock.ExpectExec(`(?s)WITH usage_base AS .*COALESCE\(NULLIF\(g\.platform, ''\), NULLIF\(a\.platform, ''\), 'unknown'\) AS platform.*FROM usage_logs ul\s+LEFT JOIN groups g ON g\.id = ul\.group_id\s+LEFT JOIN accounts a ON a\.id = ul\.account_id.*INSERT INTO ops_metrics_hourly`).
		WithArgs(start, end).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.UpsertHourlyMetrics(context.Background(), start, end))
	require.NoError(t, mock.ExpectationsWereMet())
}
