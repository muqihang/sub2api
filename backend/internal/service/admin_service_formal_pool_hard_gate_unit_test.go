//go:build unit

package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type formalPoolCreateRepoStub struct {
	mockAccountRepoForGemini
	created     *Account
	boundGroups []int64
}

func (r *formalPoolCreateRepoStub) Create(_ context.Context, account *Account) error {
	r.created = account
	account.ID = 101
	return nil
}

func (r *formalPoolCreateRepoStub) BindGroups(_ context.Context, _ int64, groupIDs []int64) error {
	r.boundGroups = append([]int64(nil), groupIDs...)
	return nil
}

func TestAdminServiceCreateAccount_AnthropicOAuthSetupTokenDefaultsUnschedulableImported(t *testing.T) {
	t.Parallel()

	cases := []string{AccountTypeOAuth, AccountTypeSetupToken}
	for _, accountType := range cases {
		accountType := accountType
		t.Run(accountType, func(t *testing.T) {
			t.Parallel()
			repo := &formalPoolCreateRepoStub{}
			svc := &adminServiceImpl{accountRepo: repo}
			requestedSchedulable := true

			got, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
				Name:        "new-formal",
				Platform:    PlatformAnthropic,
				Type:        accountType,
				Credentials: map[string]any{"access_token": "token", "refresh_token": "refresh"},
				Extra:       map[string]any{FormalPoolExtraPoolProfileRequested: PoolProfileAggressive},
				GroupIDs:    []int64{42},
				Schedulable: &requestedSchedulable,
			})

			require.NoError(t, err)
			require.NotNil(t, got)
			require.NotNil(t, repo.created)
			require.False(t, repo.created.Schedulable, "manual create must not let new formal pool account enter scheduling")
			require.Equal(t, FormalPoolStageImported, repo.created.Extra[FormalPoolExtraOnboardingStage])
			require.Equal(t, PoolProfileAggressive, repo.created.Extra[FormalPoolExtraPoolProfileRequested])
			require.Equal(t, PoolProfileNormal, repo.created.Extra[FormalPoolExtraPoolProfileEffective])
			require.Equal(t, FormalPoolWeightLow, repo.created.Extra[FormalPoolExtraPoolWeightMode])
			require.Equal(t, "pending", repo.created.Extra[FormalPoolExtraHealthcheckStatus])
		})
	}
}

func TestAdminServiceCreateAccount_NonAnthropicKeepsExplicitSchedulable(t *testing.T) {
	t.Parallel()

	repo := &formalPoolCreateRepoStub{}
	svc := &adminServiceImpl{accountRepo: repo}
	requestedSchedulable := true

	_, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
		Name:                 "regular",
		Platform:             PlatformOpenAI,
		Type:                 AccountTypeOAuth,
		Credentials:          map[string]any{"access_token": "token"},
		SkipDefaultGroupBind: true,
		Schedulable:          &requestedSchedulable,
	})

	require.NoError(t, err)
	require.True(t, repo.created.Schedulable)
	require.Empty(t, repo.created.Extra[FormalPoolExtraOnboardingStage])
}

func TestAdminServiceSetAccountSchedulable_FormalPoolRequiresWarmingOrProduction(t *testing.T) {
	t.Parallel()

	repo := &formalPoolCreateRepoStub{mockAccountRepoForGemini: mockAccountRepoForGemini{accountsByID: map[int64]*Account{201: {
		ID:          201,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeSetupToken,
		Status:      StatusActive,
		Schedulable: false,
		Extra:       map[string]any{FormalPoolExtraOnboardingStage: FormalPoolStageImported},
	}}}}
	svc := &adminServiceImpl{accountRepo: repo}

	got, err := svc.SetAccountSchedulable(context.Background(), 201, true)

	require.Error(t, err)
	require.Nil(t, got)
	require.False(t, repo.accountsByID[201].Schedulable)
}

func TestAdminServiceSetAccountSchedulable_FormalPoolAllowsWarming(t *testing.T) {
	t.Parallel()

	repo := &formalPoolCreateRepoStub{mockAccountRepoForGemini: mockAccountRepoForGemini{accountsByID: map[int64]*Account{202: {
		ID:          202,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeSetupToken,
		Status:      StatusActive,
		Schedulable: false,
		Extra:       mergeFormalPoolTestExtra(FormalPoolStageWarming),
	}}}}
	svc := &adminServiceImpl{accountRepo: repo}

	got, err := svc.SetAccountSchedulable(context.Background(), 202, true)

	require.NoError(t, err)
	require.NotNil(t, got)
}

func TestAdminServiceQuarantineFormalPoolAccount_WritesSafeLifecycleState(t *testing.T) {
	t.Parallel()

	repo := &formalPoolCreateRepoStub{mockAccountRepoForGemini: mockAccountRepoForGemini{accountsByID: map[int64]*Account{203: {
		ID:          203,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Extra:       mergeFormalPoolTestExtra(FormalPoolStageProduction),
	}}}}
	svc := &adminServiceImpl{accountRepo: repo}

	got, err := svc.QuarantineFormalPoolAccount(context.Background(), 203, "egress_proxy_failure")

	require.NoError(t, err)
	require.NotNil(t, got)
	require.False(t, got.Schedulable)
	require.Equal(t, StatusError, got.Status)
	require.Equal(t, FormalPoolStageQuarantined, got.Extra[FormalPoolExtraOnboardingStage])
	require.Equal(t, "reason_proxy", got.Extra[FormalPoolExtraQuarantineReason])
	require.NotEmpty(t, got.Extra[FormalPoolExtraRiskEventRef])
}

func TestAdminServiceUpdateAccount_FormalPoolImportedCannotBeMadeSchedulable(t *testing.T) {
	t.Parallel()

	repo := &formalPoolCreateRepoStub{mockAccountRepoForGemini: mockAccountRepoForGemini{accountsByID: map[int64]*Account{204: {
		ID:          204,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeSetupToken,
		Status:      StatusActive,
		Schedulable: false,
		Extra:       map[string]any{FormalPoolExtraOnboardingStage: FormalPoolStageImported},
	}}}}
	svc := &adminServiceImpl{accountRepo: repo}
	schedulable := true

	got, err := svc.UpdateAccount(context.Background(), 204, &UpdateAccountInput{Schedulable: &schedulable})

	require.Error(t, err)
	require.Nil(t, got)
	require.False(t, repo.accountsByID[204].Schedulable)
}

func TestAdminServiceUpdateAccount_FormalPoolAllowsAtomicWarmingTransition(t *testing.T) {
	t.Parallel()

	repo := &formalPoolCreateRepoStub{mockAccountRepoForGemini: mockAccountRepoForGemini{accountsByID: map[int64]*Account{205: {
		ID:          205,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: false,
		Extra:       mergeFormalPoolTestExtra(FormalPoolStageHealthcheckPassed),
	}}}}
	svc := &adminServiceImpl{accountRepo: repo}
	schedulable := true

	got, err := svc.UpdateAccount(context.Background(), 205, &UpdateAccountInput{
		Schedulable: &schedulable,
		Extra:       mergeFormalPoolTestExtra(FormalPoolStageWarming),
	})

	require.NoError(t, err)
	require.NotNil(t, got)
}

func TestAdminServiceUpdateAccount_FormalPoolCredentialPartialUpdatePreservesOAuthSecrets(t *testing.T) {
	t.Parallel()

	repo := &formalPoolCreateRepoStub{mockAccountRepoForGemini: mockAccountRepoForGemini{accountsByID: map[int64]*Account{206: {
		ID:          206,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"access_token":  "access-secret",
			"refresh_token": "refresh-secret",
			"scope":         "user:profile user:inference user:sessions:claude_code",
			"expires_at":    "1893456000",
		},
		Extra: mergeFormalPoolTestExtra(FormalPoolStageWarming),
	}}}}
	svc := &adminServiceImpl{accountRepo: repo}

	got, err := svc.UpdateAccount(context.Background(), 206, &UpdateAccountInput{
		Credentials: map[string]any{"intercept_warmup_requests": true},
	})

	require.NoError(t, err)
	require.Equal(t, "access-secret", got.Credentials["access_token"])
	require.Equal(t, "refresh-secret", got.Credentials["refresh_token"])
	require.Equal(t, "user:profile user:inference user:sessions:claude_code", got.Credentials["scope"])
	require.Equal(t, true, got.Credentials["intercept_warmup_requests"])
}

func TestAdminServiceUpdateAccount_FormalPoolExtraPartialUpdatePreservesRuntimeEvidence(t *testing.T) {
	t.Parallel()

	extra := mergeFormalPoolTestExtra(FormalPoolStageWarming)
	extra["cc_gateway_enabled"] = "true"
	extra["cc_gateway_routes"] = "native_messages"
	extra["cc_gateway_account_ref"] = "hmac-sha256:" + strings.Repeat("b", 64)
	extra["cc_gateway_egress_bucket_enabled"] = "true"
	extra["cc_gateway_egress_bucket"] = "claude-hmac-sha256:4ad0"

	repo := &formalPoolCreateRepoStub{mockAccountRepoForGemini: mockAccountRepoForGemini{accountsByID: map[int64]*Account{207: {
		ID:          207,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeSetupToken,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:inference"},
		Extra:       extra,
	}}}}
	svc := &adminServiceImpl{accountRepo: repo}

	got, err := svc.UpdateAccount(context.Background(), 207, &UpdateAccountInput{
		Extra: map[string]any{"base_rpm": 3, "cc_gateway_egress_bucket": "", FormalPoolExtraRuntimeRegisteredAt: ""},
	})

	require.NoError(t, err)
	require.Equal(t, 3, got.Extra["base_rpm"])
	require.Equal(t, FormalPoolStageWarming, got.Extra[FormalPoolExtraOnboardingStage])
	require.Equal(t, "true", got.Extra[FormalPoolExtraRuntimeRegistered])
	require.Equal(t, "2026-05-30T00:00:00Z", got.Extra[FormalPoolExtraRuntimeRegisteredAt])
	require.Equal(t, "claude-hmac-sha256:4ad0", got.Extra["cc_gateway_egress_bucket"])
	require.Equal(t, "hmac-sha256:"+strings.Repeat("b", 64), got.Extra["cc_gateway_account_ref"])
	require.Equal(t, "passed", got.Extra[FormalPoolExtraHealthcheckStatus])
}
