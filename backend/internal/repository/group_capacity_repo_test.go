package repository

import (
	"context"
	"database/sql"
	"regexp"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

type groupCapacityCaptureQueryExecutor struct {
	db       *sql.DB
	captured *string
}

func (e groupCapacityCaptureQueryExecutor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return e.db.ExecContext(ctx, query, args...)
}

func (e groupCapacityCaptureQueryExecutor) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	*e.captured = query
	return e.db.QueryContext(ctx, query, args...)
}

func TestListSchedulableCapacityByGroupIDsSQLUsesTargetSchema(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer db.Close()

	now := time.Now().UTC()
	rows := sqlmock.NewRows([]string{
		"group_id", "account_id", "concurrency", "extra", "session_window_start", "session_window_end", "session_window_status",
	}).AddRow(int64(10), int64(7), 3, `{"max_sessions":2,"base_rpm":30}`, now, now.Add(time.Hour), "active")
	mock.ExpectQuery(regexp.QuoteMeta("SELECT")).
		WithArgs(pq.Array([]int64{20, 10}), service.StatusActive, sqlmock.AnyArg()).
		WillReturnRows(rows)

	var captured string
	repo := newAccountRepositoryWithSQL(nil, groupCapacityCaptureQueryExecutor{db: db, captured: &captured}, nil)
	out, err := repo.ListSchedulableCapacityByGroupIDs(context.Background(), []int64{20, 10, 10, 0, -1})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())

	require.NotContains(t, strings.ToLower(captured), "proxies")
	require.NotContains(t, strings.ToLower(captured), "owner_user_id")
	require.Len(t, out, 1)
	require.Equal(t, int64(10), out[0].GroupID)
	require.Equal(t, int64(7), out[0].AccountID)
	require.Equal(t, 3, out[0].Concurrency)
	require.Equal(t, float64(2), out[0].Extra["max_sessions"])
	require.Equal(t, "active", out[0].SessionWindowStatus)
}

func TestListSchedulableCapacityByGroupIDsEmptySkipsSQL(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	var captured string
	repo := newAccountRepositoryWithSQL(nil, groupCapacityCaptureQueryExecutor{db: db, captured: &captured}, nil)
	out, err := repo.ListSchedulableCapacityByGroupIDs(context.Background(), []int64{0, -1})
	require.NoError(t, err)
	require.Empty(t, out)
	require.Empty(t, captured)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListActiveIDsSQLReturnsOrderedIDs(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery("SELECT id").
		WithArgs(service.StatusActive).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(2)).AddRow(int64(3)))

	repo := newGroupRepositoryWithSQL(nil, db)
	ids, err := repo.ListActiveIDs(context.Background())
	require.NoError(t, err)
	require.Equal(t, []int64{2, 3}, ids)
	require.NoError(t, mock.ExpectationsWereMet())
}
