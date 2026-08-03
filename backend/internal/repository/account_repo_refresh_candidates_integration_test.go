//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountRepository_ListTokenRefreshCandidates_Semantics(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newAccountRepositoryWithSQL(client, tx, nil)
	now := time.Now().UTC().Truncate(time.Second)
	future := now.Add(30 * time.Minute)
	past := now.Add(-30 * time.Minute)

	mk := func(name, platform, accountType, status string, priority int) *service.Account {
		return mustCreateAccount(t, client, &service.Account{
			Name:        name,
			Platform:    platform,
			Type:        accountType,
			Status:      status,
			Priority:    priority,
			Credentials: map[string]any{"expires_at": future.Format(time.RFC3339)},
		})
	}

	nullTemp := mk("candidate-null-temp", service.PlatformAnthropic, service.AccountTypeSetupToken, service.StatusActive, 30)
	anthropicOAuth := mk("candidate-anthropic-oauth", service.PlatformAnthropic, service.AccountTypeOAuth, service.StatusActive, 20)
	openaiOAuth := mk("candidate-openai-oauth", service.PlatformOpenAI, service.AccountTypeOAuth, service.StatusActive, 10)
	geminiOAuth := mk("candidate-gemini-oauth", service.PlatformGemini, service.AccountTypeOAuth, service.StatusActive, 40)
	antigravityOAuth := mk("candidate-antigravity-oauth", service.PlatformAntigravity, service.AccountTypeOAuth, service.StatusActive, 50)
	blankRT := mk("candidate-blank-rt", service.PlatformAnthropic, service.AccountTypeSetupToken, service.StatusActive, 60)
	expiredRetry := mk("candidate-expired-retry", service.PlatformAnthropic, service.AccountTypeOAuth, service.StatusActive, 70)
	futureNonRetry := mk("candidate-future-non-retry", service.PlatformAnthropic, service.AccountTypeOAuth, service.StatusActive, 80)
	futureRetry := mk("excluded-future-retry", service.PlatformAnthropic, service.AccountTypeOAuth, service.StatusActive, 90)
	apiKey := mk("excluded-api-key", service.PlatformAnthropic, service.AccountTypeAPIKey, service.StatusActive, 100)
	unrelated := mk("excluded-unrelated", "other", service.AccountTypeOAuth, service.StatusActive, 110)
	inactive := mk("excluded-inactive", service.PlatformAnthropic, service.AccountTypeOAuth, service.StatusDisabled, 120)
	deleted := mk("excluded-deleted", service.PlatformAnthropic, service.AccountTypeOAuth, service.StatusActive, 130)

	_, err := tx.ExecContext(ctx, `UPDATE accounts SET temp_unschedulable_until = $1, temp_unschedulable_reason = $2 WHERE id = $3`, past, service.TokenRefreshRetryExhaustedReasonPrefix+" old", expiredRetry.ID)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `UPDATE accounts SET temp_unschedulable_until = $1, temp_unschedulable_reason = $2 WHERE id = $3`, future, "request path cooldown", futureNonRetry.ID)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `UPDATE accounts SET temp_unschedulable_until = $1, temp_unschedulable_reason = $2 WHERE id = $3`, future, service.TokenRefreshRetryExhaustedReasonPrefix+" transient", futureRetry.ID)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `UPDATE accounts SET deleted_at = NOW() WHERE id = $1`, deleted.ID)
	require.NoError(t, err)
	_ = apiKey
	_ = unrelated
	_ = inactive

	accounts, err := repo.ListTokenRefreshCandidates(ctx)
	require.NoError(t, err)

	ids := make([]int64, 0, len(accounts))
	for _, account := range accounts {
		ids = append(ids, account.ID)
	}
	require.Equal(t, []int64{
		openaiOAuth.ID,
		anthropicOAuth.ID,
		nullTemp.ID,
		geminiOAuth.ID,
		antigravityOAuth.ID,
		blankRT.ID,
		expiredRetry.ID,
		futureNonRetry.ID,
	}, ids)
	require.NotContains(t, ids, futureRetry.ID)
	require.NotContains(t, ids, apiKey.ID)
	require.NotContains(t, ids, unrelated.ID)
	require.NotContains(t, ids, inactive.ID)
	require.NotContains(t, ids, deleted.ID)
}

func TestAccountRepository_SetTempUnschedulable_PositiveRowsAffectedSyncsSchedulerCache(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	cache := &schedulerCacheRecorder{}
	repo := newAccountRepositoryWithSQL(client, tx, cache)
	account := mustCreateAccount(t, client, &service.Account{Name: "temp-positive-sync"})
	_, err := tx.ExecContext(ctx, "TRUNCATE scheduler_outbox")
	require.NoError(t, err)

	err = repo.SetTempUnschedulable(ctx, account.ID, time.Now().Add(10*time.Minute), "retry")
	require.NoError(t, err)

	var outboxCount int
	require.NoError(t, scanSingleRow(ctx, tx, "SELECT COUNT(*) FROM scheduler_outbox", nil, &outboxCount))
	require.Equal(t, 1, outboxCount)
	require.Len(t, cache.setAccounts, 1)
	require.Equal(t, account.ID, cache.setAccounts[0].ID)
	require.NotNil(t, cache.setAccounts[0].TempUnschedulableUntil)
}
