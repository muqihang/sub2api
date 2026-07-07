package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func formalPoolCompleteSchedulingEvidenceForTest() map[string]any {
	return map[string]any{
		FormalPoolExtraHealthcheckStatus:           "passed",
		FormalPoolExtraHealthcheckStatusCodeBucket: "status_2xx",
		FormalPoolExtraHealthcheckRawRef:           "hmac-sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		FormalPoolExtraHealthcheckCCGatewaySeen:    true,
		FormalPoolExtraHealthcheckFallbackDetected: false,
		FormalPoolExtraHealthcheckProxyMismatch:    false,
		FormalPoolExtraHealthcheckRiskTextDetected: false,
		FormalPoolExtraRuntimeRegistered:           "true",
		FormalPoolExtraRuntimeRegisteredAt:         "2026-05-30T00:00:00Z",
		"cc_gateway_account_ref":                   "hmac-sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"cc_gateway_credential_ref":                "opaque:credential-ref:v1:cred-a",
		"cc_gateway_credential_binding_hmac":       "hmac-sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		"cc_gateway_egress_bucket_enabled":         "true",
		"cc_gateway_egress_bucket":                 "claude-safe-bucket",
		"cc_gateway_proxy_identity_ref":            "opaque:proxy-ref:v1:claude-safe-bucket",
		"cc_gateway_persona_profile":               ccGatewayDefaultPersonaProfile,
		"claude_code_device_id":                    "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
	}
}

func mergeFormalPoolTestExtra(stage string) map[string]any {
	extra := formalPoolCompleteSchedulingEvidenceForTest()
	extra[FormalPoolExtraOnboardingStage] = stage
	return extra
}

func formalPoolApplyCompleteSchedulingEvidenceForTest(account *Account) {
	if account == nil {
		return
	}
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	for k, v := range formalPoolCompleteSchedulingEvidenceForTest() {
		account.Extra[k] = v
	}
}

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

	formalPoolApplyCompleteSchedulingEvidenceForTest(account)
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageWarming
	require.True(t, account.IsSchedulable(), "warming is eligible when persisted evidence is complete")

	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction
	require.True(t, account.IsSchedulable(), "production is eligible when persisted evidence is complete")

	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageQuarantined
	require.False(t, account.IsSchedulable(), "quarantined formal-pool account must not be schedulable")
}

func TestFormalPoolQuarantineServiceMarksAccountUnsafeAndWritesRisk(t *testing.T) {
	repo := &formalPoolQuarantineRepo{accounts: map[int64]*Account{7: {ID: 7, Platform: PlatformAnthropic, Type: AccountTypeSetupToken, Status: StatusActive, Schedulable: true, Extra: map[string]any{FormalPoolExtraOnboardingStage: FormalPoolStageProduction}}}}
	sink := &recordingBudgetLedgerSink{}
	svc := NewAccountQuarantineService(repo, sink)

	entry, err := svc.Quarantine(context.Background(), AccountQuarantineInput{AccountID: 7, Kind: RiskEventKindIdentityBoundaryFail, Reason: "missing_account_identity raw-token unsafe-token-prefix-redacted", Source: "cc_gateway", StatusCode: http.StatusUnprocessableEntity})
	require.NoError(t, err)
	require.Equal(t, FormalPoolStageQuarantined, repo.accounts[7].Extra[FormalPoolExtraOnboardingStage])
	require.False(t, repo.accounts[7].Schedulable)
	require.Equal(t, StatusError, repo.accounts[7].Status)
	require.NotEmpty(t, repo.accounts[7].Extra[FormalPoolExtraRiskEventRef])
	require.NoError(t, ValidateNoRawSensitiveLedger(entry))
	require.NotEmpty(t, sink.risks)
}

func TestFormalPoolShouldQuarantineHTTPStatusHardSignalsAcrossStatuses(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{name: "403 hold", status: http.StatusForbidden, body: `{"error":{"message":"account on hold"}}`},
		{name: "422 proxy mismatch", status: http.StatusUnprocessableEntity, body: `{"error":{"message":"proxy_mismatch"}}`},
		{name: "422 fallback", status: http.StatusUnprocessableEntity, body: `{"error":{"message":"fallback detected"}}`},
		{name: "422 verifier with proxy mismatch", status: http.StatusUnprocessableEntity, body: `{"error":{"message":"verifier failed with proxy_mismatch"}}`},
		{name: "502 egress proxy", status: http.StatusBadGateway, body: `{"error":{"message":"egress_proxy_failure"}}`},
		{name: "500 kyc", status: http.StatusInternalServerError, body: `{"error":{"message":"KYC verification required"}}`},
		{name: "500 risk", status: http.StatusInternalServerError, body: `{"error":{"message":"risk text detected"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.True(t, FormalPoolShouldQuarantineHTTPStatus(tc.status, []byte(tc.body)))
		})
	}
	require.False(t, FormalPoolShouldQuarantineHTTPStatus(http.StatusBadGateway, []byte(`{"error":{"message":"temporary upstream unavailable"}}`)))
	require.False(t, FormalPoolShouldQuarantineHTTPStatus(http.StatusBadRequest, []byte(`{"error":{"message":"verifier failed"}}`)))
	require.False(t, FormalPoolShouldQuarantineHTTPStatus(http.StatusForbidden, []byte(`{"error":{"message":"strip verifier failed"}}`)))
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

func TestFormalPoolSchedulableRequiresPersistedRuntimeAndHealthcheckEvidence(t *testing.T) {
	baseExtra := formalPoolCompleteSchedulingEvidenceForTest()
	baseExtra[FormalPoolExtraOnboardingStage] = FormalPoolStageWarming
	mk := func(extra map[string]any) *Account {
		merged := cloneCredentials(baseExtra)
		for k, v := range extra {
			merged[k] = v
		}
		return &Account{ID: 9, Platform: PlatformAnthropic, Type: AccountTypeSetupToken, Status: StatusActive, Schedulable: true, Extra: merged}
	}

	require.False(t, mk(map[string]any{FormalPoolExtraRuntimeRegisteredAt: ""}).IsSchedulable(), "missing runtime timestamp must hard-block warming scheduling")
	require.False(t, mk(map[string]any{FormalPoolExtraHealthcheckRawRef: ""}).IsSchedulable(), "missing raw ref must hard-block warming scheduling")
	require.False(t, mk(map[string]any{FormalPoolExtraHealthcheckCCGatewaySeen: false}).IsSchedulable(), "missing gateway-seen evidence must hard-block warming scheduling")
	require.True(t, mk(nil).IsSchedulable(), "complete runtime and healthcheck evidence permits warming scheduling")
	require.True(t, mk(map[string]any{FormalPoolExtraOnboardingStage: FormalPoolStageProduction}).IsSchedulable(), "complete runtime and healthcheck evidence permits production scheduling")
}

func TestFormalPoolSchedulableEvidenceGateDoesNotAffectLegacyOrNonFormalPoolAccounts(t *testing.T) {
	legacyAnthropic := &Account{ID: 10, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}
	nonFormalOpenAI := &Account{ID: 11, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Extra: map[string]any{FormalPoolExtraOnboardingStage: FormalPoolStageWarming}}

	require.True(t, legacyAnthropic.IsSchedulable(), "legacy Anthropic OAuth accounts without formal-pool stage must keep existing behavior")
	require.True(t, nonFormalOpenAI.IsSchedulable(), "non formal-pool accounts must not be gated by Formal Pool evidence")
}
