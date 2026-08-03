//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type recordingBatchImageQueue struct {
	*fakeBatchImageQueue
	enqueued []string
}

type retryingBatchImageQueue struct {
	*fakeBatchImageQueue
	failures int
	enqueued []string
}

func (q *retryingBatchImageQueue) Enqueue(_ context.Context, batchID string) error {
	q.enqueued = append(q.enqueued, batchID)
	if q.failures > 0 {
		q.failures--
		return errors.New("redis unavailable")
	}
	return nil
}

func (q *recordingBatchImageQueue) Enqueue(_ context.Context, batchID string) error {
	q.enqueued = append(q.enqueued, batchID)
	return nil
}

func TestBatchImageBillingRecoveryService_ReleasesStaleUnsubmittedHold(t *testing.T) {
	repo := newFakeBatchImageRepository()
	apiKeyID := int64(22)
	holdAmount := 0.5
	stale := &BatchImageJob{
		BatchID:       "imgbatch_stale_created",
		UserID:        11,
		APIKeyID:      &apiKeyID,
		Status:        BatchImageJobStatusCreated,
		EstimatedCost: holdAmount,
		HoldAmount:    &holdAmount,
		CreatedAt:     time.Now().Add(-time.Hour),
		UpdatedAt:     time.Now().Add(-time.Hour),
	}
	activeProviderName := "providers/job"
	active := &BatchImageJob{
		BatchID:         "imgbatch_has_provider",
		UserID:          11,
		APIKeyID:        &apiKeyID,
		Status:          BatchImageJobStatusSubmitted,
		ProviderJobName: &activeProviderName,
		EstimatedCost:   holdAmount,
		HoldAmount:      &holdAmount,
		CreatedAt:       time.Now().Add(-time.Hour),
		UpdatedAt:       time.Now().Add(-time.Hour),
	}
	repo.jobs[stale.BatchID] = stale
	repo.jobs[active.BatchID] = active
	billing := &fakeBatchImageBillingRepo{}
	svc := &BatchImageBillingRecoveryService{Repo: repo, Billing: billing, StaleAfter: time.Minute, Limit: 10}

	released, err := svc.ReleaseStaleUnsubmittedOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, released)
	require.Equal(t, BatchImageJobStatusFailed, repo.jobs[stale.BatchID].Status)
	require.Equal(t, "SUBMIT_STALE_BEFORE_PROVIDER", batchImageDerefString(repo.jobs[stale.BatchID].LastErrorCode))
	require.Len(t, billing.releases, 1)
	require.Equal(t, BatchImageReleaseRequestID(stale.BatchID), billing.releases[0].RequestID)
	require.Equal(t, BatchImageJobStatusSubmitted, repo.jobs[active.BatchID].Status)
}

func TestBatchImageBillingRecoveryService_SkipsJobRefreshedByHeartbeat(t *testing.T) {
	repo := newFakeBatchImageRepository()
	apiKeyID := int64(22)
	holdAmount := 0.5
	// updated_at 在 cutoff 之后（慢提交心跳持续续期）：不得误杀退款。
	fresh := &BatchImageJob{
		BatchID:       "imgbatch_fresh_uploading",
		UserID:        11,
		APIKeyID:      &apiKeyID,
		Status:        BatchImageJobStatusUploading,
		EstimatedCost: holdAmount,
		HoldAmount:    &holdAmount,
		CreatedAt:     time.Now().Add(-time.Hour),
		UpdatedAt:     time.Now(),
	}
	repo.jobs[fresh.BatchID] = fresh
	billing := &fakeBatchImageBillingRepo{}
	svc := &BatchImageBillingRecoveryService{Repo: repo, Billing: billing, StaleAfter: time.Minute, Limit: 10}

	released, err := svc.ReleaseStaleUnsubmittedOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, released)
	require.Equal(t, BatchImageJobStatusUploading, repo.jobs[fresh.BatchID].Status)
	require.Empty(t, billing.releases)
}

func TestBatchImageBillingRecoveryService_EnqueuesRetryWhenReleaseFails(t *testing.T) {
	repo := newFakeBatchImageRepository()
	apiKeyID := int64(22)
	holdAmount := 0.5
	stale := &BatchImageJob{
		BatchID:       "imgbatch_stale_release_fail",
		UserID:        11,
		APIKeyID:      &apiKeyID,
		Status:        BatchImageJobStatusCreated,
		EstimatedCost: holdAmount,
		HoldAmount:    &holdAmount,
		CreatedAt:     time.Now().Add(-time.Hour),
		UpdatedAt:     time.Now().Add(-time.Hour),
	}
	repo.jobs[stale.BatchID] = stale
	billing := &fakeBatchImageBillingRepo{releaseErr: errors.New("billing db down")}
	queue := &recordingBatchImageQueue{fakeBatchImageQueue: newFakeBatchImageQueue("")}
	svc := &BatchImageBillingRecoveryService{Repo: repo, Billing: billing, Queue: queue, StaleAfter: time.Minute, Limit: 10}

	released, err := svc.ReleaseStaleUnsubmittedOnce(context.Background())
	// job 已转 failed、不会再出现在 stale 列表：释放失败必须入队重试
	//（由 worker 的 releaseTerminalHold 兜底），否则冻结余额永久泄漏。
	require.Error(t, err)
	require.Equal(t, 0, released)
	require.Equal(t, BatchImageJobStatusFailed, repo.jobs[stale.BatchID].Status)
	require.Equal(t, []string{stale.BatchID}, queue.enqueued)
}

func TestBatchImageBillingRecoveryService_RequeuesSubmittedQueueFailures(t *testing.T) {
	repo := newFakeBatchImageRepository()
	providerJobName := "providers/submitted"
	repo.jobs["imgbatch_queue_recovery"] = &BatchImageJob{
		BatchID:          "imgbatch_queue_recovery",
		Status:           BatchImageJobStatusSubmitted,
		ProviderJobName:  &providerJobName,
		LastErrorCode:    batchImageStringPtr("QUEUE_FAILED"),
		LastErrorMessage: batchImageStringPtr("redis unavailable"),
	}
	queue := &retryingBatchImageQueue{fakeBatchImageQueue: newFakeBatchImageQueue(""), failures: 1}
	svc := &BatchImageBillingRecoveryService{Repo: repo, Queue: queue, Limit: 10}

	requeued, err := svc.RequeueSubmittedFailuresOnce(context.Background())
	require.Error(t, err)
	require.Zero(t, requeued)
	require.Equal(t, "QUEUE_FAILED", batchImageDerefString(repo.jobs["imgbatch_queue_recovery"].LastErrorCode))

	requeued, err = svc.RequeueSubmittedFailuresOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, requeued)
	require.Nil(t, repo.jobs["imgbatch_queue_recovery"].LastErrorCode)
	require.Nil(t, repo.jobs["imgbatch_queue_recovery"].LastErrorMessage)
	require.Equal(t, []string{"imgbatch_queue_recovery", "imgbatch_queue_recovery"}, queue.enqueued)
}

func (r *fakeBatchImageRepository) ListBatchImageJobsPendingEnqueue(_ context.Context, limit int) ([]*BatchImageJob, error) {
	jobs := make([]*BatchImageJob, 0)
	for _, job := range r.jobs {
		if job.Status == BatchImageJobStatusSubmitted &&
			batchImageDerefString(job.ProviderJobName) != "" &&
			batchImageDerefString(job.LastErrorCode) == "QUEUE_FAILED" {
			jobs = append(jobs, job)
		}
		if limit > 0 && len(jobs) >= limit {
			break
		}
	}
	return jobs, nil
}

func (r *fakeBatchImageRepository) MarkBatchImageJobQueueRecovered(_ context.Context, batchID string) error {
	job, ok := r.jobs[batchID]
	if !ok {
		return ErrBatchImageJobNotFound
	}
	job.LastErrorCode = nil
	job.LastErrorMessage = nil
	r.events[batchID] = append(r.events[batchID], "queue_recovered")
	return nil
}
