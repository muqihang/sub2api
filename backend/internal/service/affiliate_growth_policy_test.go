//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type affiliateGrowthCountRepoStub struct {
	AffiliateRepository
	count            int
	err              error
	alreadyEffective bool
	hasEffectiveErr  error
	since            time.Time
	minAmount        float64
	invitee          *AffiliateSummary
	inviter          *AffiliateSummary
	applyInput       *AffiliateTieredRewardLedgerInput
}

func (r *affiliateGrowthCountRepoStub) CountEffectivePaidInvitees(_ context.Context, _ int64, since time.Time, minAmount float64) (int, error) {
	r.since = since
	r.minAmount = minAmount
	return r.count, r.err
}

func (r *affiliateGrowthCountRepoStub) HasEffectivePaidInvitee(_ context.Context, _, _ int64, since time.Time, minAmount float64) (bool, error) {
	r.since = since
	r.minAmount = minAmount
	return r.alreadyEffective, r.hasEffectiveErr
}

func (r *affiliateGrowthCountRepoStub) EnsureUserAffiliate(_ context.Context, userID int64) (*AffiliateSummary, error) {
	if r.invitee != nil && r.invitee.UserID == userID {
		copy := *r.invitee
		return &copy, nil
	}
	if r.inviter != nil && r.inviter.UserID == userID {
		copy := *r.inviter
		return &copy, nil
	}
	return &AffiliateSummary{UserID: userID, CreatedAt: time.Now()}, nil
}

func TestAffiliateGrowthPolicyRateBoundaries(t *testing.T) {
	t.Parallel()

	policy, err := NewAffiliateGrowthPolicy(DefaultAffiliateTierRules())
	require.NoError(t, err)

	for _, tc := range []struct {
		count int
		rate  float64
	}{
		{count: 0, rate: 8},
		{count: 2, rate: 8},
		{count: 3, rate: 10},
		{count: 9, rate: 10},
		{count: 10, rate: 12},
		{count: 24, rate: 12},
		{count: 25, rate: 15},
		{count: 100, rate: 15},
	} {
		tc := tc
		t.Run(fmt.Sprintf("count_%d", tc.count), func(t *testing.T) {
			t.Parallel()
			require.InDelta(t, tc.rate, policy.RatePercent(tc.count), 1e-9)
		})
	}
}

func TestNewAffiliateGrowthPolicyNormalizesOrder(t *testing.T) {
	t.Parallel()

	policy, err := NewAffiliateGrowthPolicy([]AffiliateTierRule{
		{MinEffectiveInvitees: 10, RatePercent: 12},
		{MinEffectiveInvitees: 0, RatePercent: 8},
		{MinEffectiveInvitees: 3, RatePercent: 10},
	})
	require.NoError(t, err)
	require.Equal(t, []AffiliateTierRule{
		{MinEffectiveInvitees: 0, RatePercent: 8},
		{MinEffectiveInvitees: 3, RatePercent: 10},
		{MinEffectiveInvitees: 10, RatePercent: 12},
	}, policy.Rules())
}

func TestNewAffiliateGrowthPolicyRejectsInvalidRules(t *testing.T) {
	t.Parallel()

	invalid := [][]AffiliateTierRule{
		nil,
		{{MinEffectiveInvitees: 1, RatePercent: 8}},
		{{MinEffectiveInvitees: 0, RatePercent: -1}},
		{{MinEffectiveInvitees: 0, RatePercent: 101}},
		{{MinEffectiveInvitees: 0, RatePercent: math.NaN()}},
		{{MinEffectiveInvitees: 0, RatePercent: 8}, {MinEffectiveInvitees: 0, RatePercent: 10}},
		{{MinEffectiveInvitees: -1, RatePercent: 8}},
	}

	for _, rules := range invalid {
		_, err := NewAffiliateGrowthPolicy(rules)
		require.Error(t, err)
	}
}

func TestParseAffiliateTierRules(t *testing.T) {
	t.Parallel()

	rules, err := ParseAffiliateTierRules(`[
		{"min_effective_invitees": 3, "rate_percent": 10},
		{"min_effective_invitees": 0, "rate_percent": 8}
	]`)
	require.NoError(t, err)
	require.Equal(t, []AffiliateTierRule{
		{MinEffectiveInvitees: 0, RatePercent: 8},
		{MinEffectiveInvitees: 3, RatePercent: 10},
	}, rules)

	for _, raw := range []string{
		``,
		`not-json`,
		`[]`,
		`[{"min_effective_invitees":0,"rate_percent":8,"unknown":true}]`,
		`[{"min_effective_invitees":0,"rate_percent":8}] trailing`,
	} {
		_, err := ParseAffiliateTierRules(raw)
		require.Error(t, err, raw)
	}
}

func TestAffiliateGrowthModeValidation(t *testing.T) {
	t.Parallel()

	require.True(t, IsValidAffiliateGrowthMode(AffiliateGrowthModeLegacy))
	require.True(t, IsValidAffiliateGrowthMode(AffiliateGrowthModeTieredV1))
	require.False(t, IsValidAffiliateGrowthMode("tiered_v2"))
}

func TestSettingServiceAffiliateGrowthDefaultsAndParsing(t *testing.T) {
	t.Parallel()

	repo := &settingRepoStub{values: map[string]string{}}
	svc := &SettingService{settingRepo: repo}
	ctx := context.Background()

	require.Equal(t, AffiliateGrowthModeLegacy, svc.GetAffiliateGrowthMode(ctx))
	require.Equal(t, 90, svc.GetAffiliateTierWindowDays(ctx))
	require.Equal(t, DefaultAffiliateTierRules(), svc.GetAffiliateTierRules(ctx))
	require.InDelta(t, 5, svc.GetAffiliateInviteeBonusRatePercent(ctx), 1e-9)
	require.InDelta(t, 0, svc.GetAffiliateEffectivePaymentMinAmount(ctx), 1e-9)

	repo.values[SettingKeyAffiliateGrowthMode] = "tiered_v1"
	repo.values[SettingKeyAffiliateTierWindowDays] = "120"
	repo.values[SettingKeyAffiliateTierRules] = `[{"min_effective_invitees":0,"rate_percent":7.5}]`
	repo.values[SettingKeyAffiliateInviteeBonusRate] = "6.25"
	repo.values[SettingKeyAffiliateEffectivePaymentMin] = "10"

	require.Equal(t, AffiliateGrowthModeTieredV1, svc.GetAffiliateGrowthMode(ctx))
	require.Equal(t, 120, svc.GetAffiliateTierWindowDays(ctx))
	require.Equal(t, []AffiliateTierRule{{MinEffectiveInvitees: 0, RatePercent: 7.5}}, svc.GetAffiliateTierRules(ctx))
	require.InDelta(t, 6.25, svc.GetAffiliateInviteeBonusRatePercent(ctx), 1e-9)
	require.InDelta(t, 10, svc.GetAffiliateEffectivePaymentMinAmount(ctx), 1e-9)
}

func TestSettingServiceAffiliateGrowthRejectsDirtyRuntimeValues(t *testing.T) {
	t.Parallel()

	repo := &settingRepoStub{values: map[string]string{
		SettingKeyAffiliateGrowthMode:          "tiered_v2",
		SettingKeyAffiliateTierWindowDays:      "-1",
		SettingKeyAffiliateTierRules:           `[{"min_effective_invitees":3,"rate_percent":10}]`,
		SettingKeyAffiliateInviteeBonusRate:    "NaN",
		SettingKeyAffiliateEffectivePaymentMin: "-1",
	}}
	svc := &SettingService{settingRepo: repo}
	ctx := context.Background()

	require.Equal(t, AffiliateGrowthModeLegacy, svc.GetAffiliateGrowthMode(ctx))
	require.Equal(t, AffiliateTierWindowDaysDefault, svc.GetAffiliateTierWindowDays(ctx))
	require.Equal(t, DefaultAffiliateTierRules(), svc.GetAffiliateTierRules(ctx))
	require.InDelta(t, AffiliateInviteeBonusRateDefault, svc.GetAffiliateInviteeBonusRatePercent(ctx), 1e-9)
	require.InDelta(t, AffiliateEffectivePaymentMinDefault, svc.GetAffiliateEffectivePaymentMinAmount(ctx), 1e-9)
}

func TestResolveAccrualRebateRatePercentTiered(t *testing.T) {
	t.Parallel()

	repo := &affiliateGrowthCountRepoStub{count: 10}
	settings := &SettingService{settingRepo: &settingRepoStub{values: map[string]string{
		SettingKeyAffiliateGrowthMode:          "tiered_v1",
		SettingKeyAffiliateTierWindowDays:      "90",
		SettingKeyAffiliateTierRules:           DefaultAffiliateTierRulesJSON(),
		SettingKeyAffiliateEffectivePaymentMin: "10",
	}}}
	svc := &AffiliateService{repo: repo, settingService: settings}

	rate, count, err := svc.resolveAccrualRebateRatePercent(context.Background(), &AffiliateSummary{UserID: 42})
	require.NoError(t, err)
	require.InDelta(t, 12, rate, 1e-9)
	require.Equal(t, 10, count)
	require.InDelta(t, 10, repo.minAmount, 1e-9)
	require.WithinDuration(t, time.Now().AddDate(0, 0, -90), repo.since, time.Second)
}

func TestResolveAccrualRebateRatePercentCustomOverrideSkipsTierLookup(t *testing.T) {
	t.Parallel()

	repo := &affiliateGrowthCountRepoStub{err: errors.New("must not be called")}
	svc := &AffiliateService{
		repo: repo,
		settingService: &SettingService{settingRepo: &settingRepoStub{values: map[string]string{
			SettingKeyAffiliateGrowthMode: "tiered_v1",
		}}},
	}
	customRate := 18.0

	rate, count, err := svc.resolveAccrualRebateRatePercent(context.Background(), &AffiliateSummary{
		UserID:               42,
		AffRebateRatePercent: &customRate,
	})
	require.NoError(t, err)
	require.InDelta(t, 18, rate, 1e-9)
	require.Zero(t, count)
}

func TestApplyOrderRewardsTieredIncludesInviteeFirstPaymentBonus(t *testing.T) {
	t.Parallel()

	inviterID := int64(1001)
	repo := &affiliateGrowthCountRepoStub{
		count:            3,
		alreadyEffective: true,
		invitee:          &AffiliateSummary{UserID: 2002, InviterID: &inviterID},
		inviter:          &AffiliateSummary{UserID: inviterID},
	}
	// Override the method through a small wrapper so the test captures the exact ledger input.
	applyRepo := &affiliateTieredRewardCaptureRepo{affiliateGrowthCountRepoStub: repo}
	settings := &SettingService{settingRepo: &settingRepoStub{values: map[string]string{
		SettingKeyAffiliateEnabled:             "true",
		SettingKeyAffiliateGrowthMode:          "tiered_v1",
		SettingKeyAffiliateTierRules:           DefaultAffiliateTierRulesJSON(),
		SettingKeyAffiliateTierWindowDays:      "90",
		SettingKeyAffiliateRebateFreezeHours:   "24",
		SettingKeyAffiliateInviteeBonusRate:    "5",
		SettingKeyAffiliateEffectivePaymentMin: "10",
		SettingKeyAffiliateRebatePerInviteeCap: "0",
	}}}
	svc := NewAffiliateService(applyRepo, settings, nil, nil)

	result, err := svc.ApplyOrderRewards(context.Background(), AffiliateOrderRewardInput{
		InviteeUserID:    2002,
		BaseAmount:       100,
		NetPaymentAmount: 100,
		SourceOrderID:    9009,
	})
	require.NoError(t, err)
	require.InDelta(t, 10, result.InviterRebateAmount, 1e-9)
	require.InDelta(t, 5, result.InviteeBonusAmount, 1e-9)
	require.InDelta(t, 10, result.RatePercent, 1e-9)
	require.Equal(t, 3, result.EffectiveInvitees)
	require.NotNil(t, applyRepo.input)
	require.Equal(t, 24, applyRepo.input.FreezeHours)
}

func TestApplyOrderRewardsTieredCountsCurrentFirstPaymentAtBoundary(t *testing.T) {
	t.Parallel()

	inviterID := int64(1001)
	repo := &affiliateGrowthCountRepoStub{
		count:   2,
		invitee: &AffiliateSummary{UserID: 2002, InviterID: &inviterID},
		inviter: &AffiliateSummary{UserID: inviterID},
	}
	applyRepo := &affiliateTieredRewardCaptureRepo{affiliateGrowthCountRepoStub: repo}
	settings := &SettingService{settingRepo: &settingRepoStub{values: map[string]string{
		SettingKeyAffiliateEnabled:             "true",
		SettingKeyAffiliateGrowthMode:          "tiered_v1",
		SettingKeyAffiliateTierRules:           DefaultAffiliateTierRulesJSON(),
		SettingKeyAffiliateTierWindowDays:      "90",
		SettingKeyAffiliateInviteeBonusRate:    "5",
		SettingKeyAffiliateEffectivePaymentMin: "0",
	}}}
	svc := NewAffiliateService(applyRepo, settings, nil, nil)

	result, err := svc.ApplyOrderRewards(context.Background(), AffiliateOrderRewardInput{
		InviteeUserID:    2002,
		BaseAmount:       100,
		NetPaymentAmount: 100,
		SourceOrderID:    9009,
	})
	require.NoError(t, err)
	require.InDelta(t, 10, result.RatePercent, 1e-9)
	require.Equal(t, 3, result.EffectiveInvitees)
	require.InDelta(t, 10, result.InviterRebateAmount, 1e-9)
}

type affiliateTieredRewardCaptureRepo struct {
	*affiliateGrowthCountRepoStub
	input *AffiliateTieredRewardLedgerInput
}

func (r *affiliateTieredRewardCaptureRepo) ApplyTieredOrderRewards(_ context.Context, input AffiliateTieredRewardLedgerInput) (AffiliateTieredRewardLedgerResult, error) {
	copy := input
	r.input = &copy
	return AffiliateTieredRewardLedgerResult{InviterRebateApplied: input.InviterRebateAmount > 0, InviteeBonusApplied: input.InviteeBonusAmount > 0}, nil
}
