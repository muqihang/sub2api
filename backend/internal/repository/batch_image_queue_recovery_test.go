//go:build unit

package repository

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestBatchImageRepository_ListBatchImageJobsPendingEnqueueUsesDurableMarker(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(regexp.QuoteMeta(batchImageJobSelectSQL) + `(?s).*status = 'submitted'.*provider_job_name IS NOT NULL.*last_error_code = 'QUEUE_FAILED'.*ORDER BY updated_at ASC, id ASC.*LIMIT \$1`).
		WithArgs(25).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	repo := &batchImageRepository{sql: db}
	jobs, err := repo.ListBatchImageJobsPendingEnqueue(context.Background(), 25)
	require.NoError(t, err)
	require.Empty(t, jobs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBatchImageRepository_MarkBatchImageJobQueueRecoveredClearsMarker(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec(`(?s)UPDATE batch_image_jobs.*last_error_code = NULL.*status = 'submitted'.*last_error_code = 'QUEUE_FAILED'`).
		WithArgs("imgbatch_recovered", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)INSERT INTO batch_image_events.*VALUES \(\$1, \$2, \$3\)`).
		WithArgs("imgbatch_recovered", "queue_recovered", `{"batch_id":"imgbatch_recovered"}`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	repo := &batchImageRepository{sql: db}
	require.NoError(t, repo.MarkBatchImageJobQueueRecovered(context.Background(), "imgbatch_recovered"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBatchImageRepository_MarkBatchImageJobQueueRecoveredNoopsAfterConcurrentAdvance(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec(`(?s)UPDATE batch_image_jobs.*last_error_code = NULL.*status = 'submitted'.*last_error_code = 'QUEUE_FAILED'`).
		WithArgs("imgbatch_advanced", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))

	repo := &batchImageRepository{sql: db}
	require.NoError(t, repo.MarkBatchImageJobQueueRecovered(context.Background(), "imgbatch_advanced"))
	require.NoError(t, mock.ExpectationsWereMet())
}
