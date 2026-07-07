package repository

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountRepository_ListTokenRefreshCandidates_SQLShape(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var capturedSQL string
	mock.ExpectQuery("SELECT id").
		WithArgs(service.TokenRefreshRetryExhaustedReasonPrefix + "%").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	repo := newAccountRepositoryWithSQL(nil, captureRefreshCandidateSQL{db: db, captured: &capturedSQL}, nil)

	accounts, err := repo.ListTokenRefreshCandidates(context.Background())
	require.NoError(t, err)
	require.Empty(t, accounts)

	normalized := normalizeRefreshCandidateSQL(capturedSQL)
	require.Contains(t, normalized, "deleted_at IS NULL")
	require.Contains(t, normalized, "status = 'active'")
	require.Contains(t, normalized, "platform = 'anthropic' AND type IN ('oauth', 'setup-token')")
	require.Contains(t, normalized, "platform IN ('openai', 'gemini', 'antigravity') AND type = 'oauth'")
	require.NotContains(t, normalized, "credentials ? 'refresh_token'")
	require.NotContains(t, normalized, "btrim(credentials->>'refresh_token') <> ''")
	require.Contains(t, normalized, "temp_unschedulable_until IS NOT NULL")
	require.Contains(t, normalized, "temp_unschedulable_until > NOW()")
	require.Contains(t, normalized, "COALESCE(temp_unschedulable_reason, '') LIKE")
	require.Contains(t, normalized, "ORDER BY priority ASC, id ASC")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountRepository_SetTempUnschedulable_NoRowsAffectedDoesNotWriteOutboxOrSync(t *testing.T) {
	exec := &recordingRefreshCandidateSQLExecutor{result: rowsAffectedRefreshCandidateResult(0)}
	cache := &refreshCandidateSchedulerCacheRecorder{}
	repo := newAccountRepositoryWithSQL(nil, exec, cache)

	err := repo.SetTempUnschedulable(context.Background(), 42, time.Now().Add(10*time.Minute), "retry")
	require.NoError(t, err)
	require.Len(t, exec.execQueries, 1)
	require.Contains(t, exec.execQueries[0], "UPDATE accounts")
	require.NotContains(t, strings.Join(exec.execQueries, "\n"), "scheduler_outbox")
	require.Equal(t, 0, cache.setAccountCalls)
}

func TestAccountRepository_SetTempUnschedulable_RowsAffectedError(t *testing.T) {
	exec := &recordingRefreshCandidateSQLExecutor{result: rowsAffectedRefreshCandidateError{err: errors.New("rows affected failed")}}
	repo := newAccountRepositoryWithSQL(nil, exec, nil)

	err := repo.SetTempUnschedulable(context.Background(), 42, time.Now().Add(10*time.Minute), "retry")
	require.ErrorContains(t, err, "rows affected failed")
	require.Len(t, exec.execQueries, 1)
	require.NotContains(t, strings.Join(exec.execQueries, "\n"), "scheduler_outbox")
}

func TestAccountRepository_SetTempUnschedulable_PositiveRowsAffectedKeepsOutbox(t *testing.T) {
	exec := &recordingRefreshCandidateSQLExecutor{result: rowsAffectedRefreshCandidateResult(1)}
	repo := newAccountRepositoryWithSQL(nil, exec, nil)

	err := repo.SetTempUnschedulable(context.Background(), 42, time.Now().Add(10*time.Minute), "retry")
	require.NoError(t, err)
	require.Len(t, exec.execQueries, 2)
	require.Contains(t, exec.execQueries[0], "UPDATE accounts")
	require.Contains(t, exec.execQueries[1], "scheduler_outbox")
}

type captureRefreshCandidateSQL struct {
	db       *sql.DB
	captured *string
}

func (c captureRefreshCandidateSQL) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return c.db.ExecContext(ctx, query, args...)
}

func (c captureRefreshCandidateSQL) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if c.captured != nil {
		*c.captured = query
	}
	return c.db.QueryContext(ctx, query, args...)
}

type recordingRefreshCandidateSQLExecutor struct {
	result      sql.Result
	err         error
	execQueries []string
}

func (e *recordingRefreshCandidateSQLExecutor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	e.execQueries = append(e.execQueries, query)
	if e.err != nil {
		return nil, e.err
	}
	return e.result, nil
}

func (e *recordingRefreshCandidateSQLExecutor) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return nil, sql.ErrNoRows
}

type rowsAffectedRefreshCandidateResult int64

func (r rowsAffectedRefreshCandidateResult) LastInsertId() (int64, error) { return 0, nil }
func (r rowsAffectedRefreshCandidateResult) RowsAffected() (int64, error) { return int64(r), nil }

type rowsAffectedRefreshCandidateError struct{ err error }

func (r rowsAffectedRefreshCandidateError) LastInsertId() (int64, error) { return 0, nil }
func (r rowsAffectedRefreshCandidateError) RowsAffected() (int64, error) { return 0, r.err }

type refreshCandidateSchedulerCacheRecorder struct {
	service.SchedulerCache
	setAccountCalls int
}

func (s *refreshCandidateSchedulerCacheRecorder) SetAccount(context.Context, *service.Account) error {
	s.setAccountCalls++
	return nil
}

func normalizeRefreshCandidateSQL(sqlText string) string {
	return strings.Join(regexp.MustCompile(`\s+`).Split(strings.TrimSpace(sqlText), -1), " ")
}
