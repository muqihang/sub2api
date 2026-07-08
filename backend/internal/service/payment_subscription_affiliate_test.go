//go:build !unit

package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

type paymentSubscriptionAffiliateAccrueCall struct {
	inviterID     int64
	inviteeUserID int64
	amount        float64
	freezeHours   int
	sourceOrderID *int64
}

type paymentSubscriptionAffiliateRepoStub struct {
	inviteeSummary *AffiliateSummary
	inviterSummary *AffiliateSummary
	accrueCalls    []paymentSubscriptionAffiliateAccrueCall
}

func (r *paymentSubscriptionAffiliateRepoStub) EnsureUserAffiliate(_ context.Context, userID int64) (*AffiliateSummary, error) {
	switch {
	case r.inviteeSummary != nil && r.inviteeSummary.UserID == userID:
		cp := *r.inviteeSummary
		return &cp, nil
	case r.inviterSummary != nil && r.inviterSummary.UserID == userID:
		cp := *r.inviterSummary
		return &cp, nil
	default:
		return &AffiliateSummary{UserID: userID, AffCode: "AFFTEST", CreatedAt: time.Now().Add(-time.Hour)}, nil
	}
}

func (r *paymentSubscriptionAffiliateRepoStub) GetAffiliateByCode(context.Context, string) (*AffiliateSummary, error) {
	panic("unexpected GetAffiliateByCode call")
}
func (r *paymentSubscriptionAffiliateRepoStub) BindInviter(context.Context, int64, int64) (bool, error) {
	panic("unexpected BindInviter call")
}
func (r *paymentSubscriptionAffiliateRepoStub) AccrueQuota(_ context.Context, inviterID, inviteeUserID int64, amount float64, freezeHours int, sourceOrderID *int64) (bool, error) {
	var sourceCopy *int64
	if sourceOrderID != nil {
		v := *sourceOrderID
		sourceCopy = &v
	}
	r.accrueCalls = append(r.accrueCalls, paymentSubscriptionAffiliateAccrueCall{
		inviterID:     inviterID,
		inviteeUserID: inviteeUserID,
		amount:        amount,
		freezeHours:   freezeHours,
		sourceOrderID: sourceCopy,
	})
	return true, nil
}
func (r *paymentSubscriptionAffiliateRepoStub) GetAccruedRebateFromInvitee(context.Context, int64, int64) (float64, error) {
	return 0, nil
}
func (r *paymentSubscriptionAffiliateRepoStub) ThawFrozenQuota(context.Context, int64) (float64, error) {
	panic("unexpected ThawFrozenQuota call")
}
func (r *paymentSubscriptionAffiliateRepoStub) TransferQuotaToBalance(context.Context, int64) (float64, float64, error) {
	panic("unexpected TransferQuotaToBalance call")
}
func (r *paymentSubscriptionAffiliateRepoStub) ListInvitees(context.Context, int64, int) ([]AffiliateInvitee, error) {
	panic("unexpected ListInvitees call")
}
func (r *paymentSubscriptionAffiliateRepoStub) UpdateUserAffCode(context.Context, int64, string) error {
	panic("unexpected UpdateUserAffCode call")
}
func (r *paymentSubscriptionAffiliateRepoStub) ResetUserAffCode(context.Context, int64) (string, error) {
	panic("unexpected ResetUserAffCode call")
}
func (r *paymentSubscriptionAffiliateRepoStub) SetUserRebateRate(context.Context, int64, *float64) error {
	panic("unexpected SetUserRebateRate call")
}
func (r *paymentSubscriptionAffiliateRepoStub) BatchSetUserRebateRate(context.Context, []int64, *float64) error {
	panic("unexpected BatchSetUserRebateRate call")
}
func (r *paymentSubscriptionAffiliateRepoStub) ListUsersWithCustomSettings(context.Context, AffiliateAdminFilter) ([]AffiliateAdminEntry, int64, error) {
	panic("unexpected ListUsersWithCustomSettings call")
}
func (r *paymentSubscriptionAffiliateRepoStub) ListAffiliateInviteRecords(context.Context, AffiliateRecordFilter) ([]AffiliateInviteRecord, int64, error) {
	panic("unexpected ListAffiliateInviteRecords call")
}
func (r *paymentSubscriptionAffiliateRepoStub) ListAffiliateRebateRecords(context.Context, AffiliateRecordFilter) ([]AffiliateRebateRecord, int64, error) {
	panic("unexpected ListAffiliateRebateRecords call")
}
func (r *paymentSubscriptionAffiliateRepoStub) ListAffiliateTransferRecords(context.Context, AffiliateRecordFilter) ([]AffiliateTransferRecord, int64, error) {
	panic("unexpected ListAffiliateTransferRecords call")
}
func (r *paymentSubscriptionAffiliateRepoStub) GetAffiliateUserOverview(context.Context, int64) (*AffiliateUserOverview, error) {
	panic("unexpected GetAffiliateUserOverview call")
}

type paymentSubscriptionSettingRepoStub struct {
	values map[string]string
}

func (s *paymentSubscriptionSettingRepoStub) Get(context.Context, string) (*Setting, error) {
	return nil, ErrSettingNotFound
}
func (s *paymentSubscriptionSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	if s.values == nil {
		return "", ErrSettingNotFound
	}
	value, ok := s.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}
func (s *paymentSubscriptionSettingRepoStub) Set(_ context.Context, key, value string) error {
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[key] = value
	return nil
}
func (s *paymentSubscriptionSettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		out[key] = s.values[key]
	}
	return out, nil
}
func (s *paymentSubscriptionSettingRepoStub) SetMultiple(_ context.Context, values map[string]string) error {
	if s.values == nil {
		s.values = map[string]string{}
	}
	for key, value := range values {
		s.values[key] = value
	}
	return nil
}
func (s *paymentSubscriptionSettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	return s.values, nil
}
func (s *paymentSubscriptionSettingRepoStub) Delete(_ context.Context, key string) error {
	delete(s.values, key)
	return nil
}

func ensureSubscriptionPaymentAuditOrderActionUniqueIndex(t *testing.T, ctx context.Context, client *dbent.Client) {
	t.Helper()
	_, err := client.ExecContext(ctx, "CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_audit_logs_order_action_uniq ON payment_audit_logs(order_id, action)")
	require.NoError(t, err)
}

func TestExecuteSubscriptionFulfillmentAppliesAffiliateRebateForSubscriptionOrder(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	ensureSubscriptionPaymentAuditOrderActionUniqueIndex(t, ctx, client)

	user, err := client.User.Create().
		SetEmail("subscription-affiliate@example.com").
		SetPasswordHash("hash").
		SetUsername("subscription-affiliate-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(120).
		SetPayAmount(120).
		SetFeeRate(0).
		SetRechargeCode("PAY-SUB-AFFILIATE").
		SetOutTradeNo("sub2_subscription_affiliate").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("trade-sub-affiliate").
		SetOrderType(payment.OrderTypeSubscription).
		SetPlanID(99).
		SetSubscriptionGroupID(7).
		SetSubscriptionDays(30).
		SetStatus(OrderStatusPaid).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	inviterID := int64(9001)
	affiliateRepo := &paymentSubscriptionAffiliateRepoStub{
		inviteeSummary: &AffiliateSummary{UserID: user.ID, AffCode: "INVITEE", InviterID: &inviterID, CreatedAt: time.Now().Add(-24 * time.Hour)},
		inviterSummary: &AffiliateSummary{UserID: inviterID, AffCode: "INVITER", CreatedAt: time.Now().Add(-48 * time.Hour)},
	}
	settingSvc := NewSettingService(&paymentSubscriptionSettingRepoStub{values: map[string]string{
		SettingKeyAffiliateEnabled:           "true",
		SettingKeyAffiliateRebateRate:        "20",
		SettingKeyAffiliateRebateFreezeHours: "0",
	}}, nil)
	subRepo := newSubscriptionUserSubRepoStub()
	subscriptionSvc := NewSubscriptionService(&subscriptionGroupRepoStub{
		group: &Group{ID: 7, Status: payment.EntityStatusActive, SubscriptionType: SubscriptionTypeSubscription},
	}, subRepo, nil, nil, nil)
	svc := &PaymentService{
		entClient:        client,
		groupRepo:        &subscriptionGroupRepoStub{group: &Group{ID: 7, Status: payment.EntityStatusActive, SubscriptionType: SubscriptionTypeSubscription}},
		subscriptionSvc:  subscriptionSvc,
		affiliateService: NewAffiliateService(affiliateRepo, settingSvc, nil, nil),
	}

	err = svc.ExecuteSubscriptionFulfillment(ctx, order.ID)
	require.NoError(t, err)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, reloaded.Status)
	require.Len(t, affiliateRepo.accrueCalls, 1)
	require.Equal(t, inviterID, affiliateRepo.accrueCalls[0].inviterID)
	require.Equal(t, user.ID, affiliateRepo.accrueCalls[0].inviteeUserID)
	require.Equal(t, 24.0, affiliateRepo.accrueCalls[0].amount)
	require.NotNil(t, affiliateRepo.accrueCalls[0].sourceOrderID)
	require.Equal(t, order.ID, *affiliateRepo.accrueCalls[0].sourceOrderID)
	require.Equal(t, 1, subRepo.createCalls)

	applied, err := client.PaymentAuditLog.Query().
		Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(order.ID, 10)), paymentauditlog.ActionEQ("AFFILIATE_REBATE_APPLIED")).
		Only(ctx)
	require.NoError(t, err)
	require.Contains(t, applied.Detail, `"baseAmount":120`)
	require.Contains(t, applied.Detail, `"rebateAmount":24`)
}

func TestExecuteSubscriptionFulfillmentSkipsDuplicateAffiliateRebateAfterLegacySuccessAudit(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	ensureSubscriptionPaymentAuditOrderActionUniqueIndex(t, ctx, client)

	user, err := client.User.Create().
		SetEmail("subscription-affiliate-idempotent@example.com").
		SetPasswordHash("hash").
		SetUsername("subscription-affiliate-idempotent-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(80).
		SetPayAmount(80).
		SetFeeRate(0).
		SetRechargeCode("PAY-SUB-AFFILIATE-IDEMPOTENT").
		SetOutTradeNo("sub2_subscription_affiliate_idempotent").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("trade-sub-affiliate-idempotent").
		SetOrderType(payment.OrderTypeSubscription).
		SetPlanID(100).
		SetSubscriptionGroupID(7).
		SetSubscriptionDays(30).
		SetStatus(OrderStatusPaid).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)
	_, err = client.PaymentAuditLog.Create().
		SetOrderID(strconv.FormatInt(order.ID, 10)).
		SetAction("SUBSCRIPTION_SUCCESS").
		SetDetail(`{"groupID":7,"validityDays":30}`).
		SetOperator("system").
		Save(ctx)
	require.NoError(t, err)
	_, err = client.PaymentAuditLog.Create().
		SetOrderID(strconv.FormatInt(order.ID, 10)).
		SetAction("AFFILIATE_REBATE_APPLIED").
		SetDetail(`{"baseAmount":80,"rebateAmount":16}`).
		SetOperator("system").
		Save(ctx)
	require.NoError(t, err)

	inviterID := int64(9001)
	affiliateRepo := &paymentSubscriptionAffiliateRepoStub{
		inviteeSummary: &AffiliateSummary{UserID: user.ID, AffCode: "INVITEE", InviterID: &inviterID, CreatedAt: time.Now().Add(-24 * time.Hour)},
		inviterSummary: &AffiliateSummary{UserID: inviterID, AffCode: "INVITER", CreatedAt: time.Now().Add(-48 * time.Hour)},
	}
	settingSvc := NewSettingService(&paymentSubscriptionSettingRepoStub{values: map[string]string{
		SettingKeyAffiliateEnabled:           "true",
		SettingKeyAffiliateRebateRate:        "20",
		SettingKeyAffiliateRebateFreezeHours: "0",
	}}, nil)
	subRepo := newSubscriptionUserSubRepoStub()
	subscriptionSvc := NewSubscriptionService(&subscriptionGroupRepoStub{
		group: &Group{ID: 7, Status: payment.EntityStatusActive, SubscriptionType: SubscriptionTypeSubscription},
	}, subRepo, nil, nil, nil)
	svc := &PaymentService{
		entClient:        client,
		groupRepo:        &subscriptionGroupRepoStub{group: &Group{ID: 7, Status: payment.EntityStatusActive, SubscriptionType: SubscriptionTypeSubscription}},
		subscriptionSvc:  subscriptionSvc,
		affiliateService: NewAffiliateService(affiliateRepo, settingSvc, nil, nil),
	}

	err = svc.ExecuteSubscriptionFulfillment(ctx, order.ID)
	require.NoError(t, err)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, reloaded.Status)
	require.Empty(t, affiliateRepo.accrueCalls)
	require.Zero(t, subRepo.createCalls)
}

var _ AffiliateRepository = (*paymentSubscriptionAffiliateRepoStub)(nil)
var _ SettingRepository = (*paymentSubscriptionSettingRepoStub)(nil)
