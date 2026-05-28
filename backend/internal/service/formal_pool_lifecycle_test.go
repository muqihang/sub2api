package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFormalPoolLifecycleDefaultsForNewAccount(t *testing.T) {
	extra := FormalPoolImportedAccountExtra(map[string]any{"pool_profile": PoolProfileAggressive}, time.Date(2026, 5, 28, 1, 2, 3, 0, time.UTC))

	require.Equal(t, FormalPoolStageImported, extra[FormalPoolExtraOnboardingStage])
	require.Equal(t, PoolProfileAggressive, extra[FormalPoolExtraPoolProfileRequested])
	require.Equal(t, PoolProfileNormal, extra[FormalPoolExtraPoolProfileEffective])
	require.Equal(t, FormalPoolWeightLow, extra[FormalPoolExtraPoolWeightMode])
	require.Equal(t, "pending", extra[FormalPoolExtraHealthcheckStatus])
}

func TestFormalPoolEligibilityGatesStages(t *testing.T) {
	account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeSetupToken, Status: StatusActive, Schedulable: true, Extra: map[string]any{FormalPoolExtraOnboardingStage: FormalPoolStageImported}}
	require.False(t, account.IsSchedulable(), "imported formal-pool account must not be schedulable even if column is true")

	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageWarming
	require.True(t, account.IsSchedulable(), "warming is eligible but low-weight")

	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction
	require.True(t, account.IsSchedulable(), "production is eligible")

	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageQuarantined
	require.False(t, account.IsSchedulable(), "quarantined formal-pool account must not be schedulable")
}

func TestFormalPoolQuarantineServiceMarksAccountUnsafeAndWritesRisk(t *testing.T) {
	repo := &formalPoolQuarantineRepo{accounts: map[int64]*Account{7: {ID: 7, Platform: PlatformAnthropic, Type: AccountTypeSetupToken, Status: StatusActive, Schedulable: true, Extra: map[string]any{FormalPoolExtraOnboardingStage: FormalPoolStageProduction}}}}
	sink := &recordingBudgetLedgerSink{}
	svc := NewAccountQuarantineService(repo, sink)

	entry, err := svc.Quarantine(context.Background(), AccountQuarantineInput{AccountID: 7, Kind: RiskEventKindIdentityBoundaryFail, Reason: "missing_account_identity raw-token sk-ant-sid02-redacted", Source: "cc_gateway", StatusCode: http.StatusUnprocessableEntity})
	require.NoError(t, err)
	require.Equal(t, FormalPoolStageQuarantined, repo.accounts[7].Extra[FormalPoolExtraOnboardingStage])
	require.False(t, repo.accounts[7].Schedulable)
	require.Equal(t, StatusError, repo.accounts[7].Status)
	require.NotEmpty(t, repo.accounts[7].Extra[FormalPoolExtraRiskEventRef])
	require.NoError(t, ValidateNoRawSensitiveLedger(entry))
	require.NotEmpty(t, sink.risks)
}

type formalPoolQuarantineRepo struct {
	stubOpenAIAccountRepo
	accounts map[int64]*Account
	updated  []*Account
}

func (r *formalPoolQuarantineRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	if a := r.accounts[id]; a != nil {
		copy := *a
		copy.Extra = cloneCredentials(a.Extra)
		return &copy, nil
	}
	return nil, ErrAccountNotFound
}

func (r *formalPoolQuarantineRepo) Update(_ context.Context, account *Account) error {
	copy := *account
	copy.Extra = cloneCredentials(account.Extra)
	r.accounts[account.ID] = &copy
	r.updated = append(r.updated, &copy)
	return nil
}
