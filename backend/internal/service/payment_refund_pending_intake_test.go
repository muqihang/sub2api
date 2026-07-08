package service

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

const refundPendingProviderEncryptionKey = "0123456789abcdef0123456789abcdef"

type refundPendingIntakeProvider struct {
	refundStatus string
	refundID     string
	queryStatus  string
	queryID      string
	queryReq     payment.RefundQueryRequest
}

func (p *refundPendingIntakeProvider) Name() string        { return "refund-pending-intake" }
func (p *refundPendingIntakeProvider) ProviderKey() string { return payment.TypeEasyPay }
func (p *refundPendingIntakeProvider) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{payment.TypeEasyPay}
}
func (p *refundPendingIntakeProvider) CreatePayment(context.Context, payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	panic("unexpected CreatePayment call")
}
func (p *refundPendingIntakeProvider) QueryOrder(context.Context, string) (*payment.QueryOrderResponse, error) {
	panic("unexpected QueryOrder call")
}
func (p *refundPendingIntakeProvider) VerifyNotification(context.Context, string, map[string]string) (*payment.PaymentNotification, error) {
	panic("unexpected VerifyNotification call")
}
func (p *refundPendingIntakeProvider) Refund(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
	return &payment.RefundResponse{RefundID: p.refundID, Status: p.refundStatus}, nil
}
func (p *refundPendingIntakeProvider) QueryRefund(_ context.Context, req payment.RefundQueryRequest) (*payment.RefundResponse, error) {
	p.queryReq = req
	return &payment.RefundResponse{RefundID: p.queryID, Status: p.queryStatus}, nil
}

func encryptRefundPendingProviderConfig(t *testing.T, config map[string]string) string {
	t.Helper()
	data, err := json.Marshal(config)
	require.NoError(t, err)
	encrypted, err := payment.Encrypt(string(data), []byte(refundPendingProviderEncryptionKey))
	require.NoError(t, err)
	return encrypted
}

func TestExecuteRefundMarksProviderPendingWithoutFinalizingUntilQuery(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)

	providerDouble := &refundPendingIntakeProvider{
		refundStatus: payment.ProviderStatusPending,
		refundID:     "refund-pending-123",
		queryStatus:  payment.ProviderStatusSuccess,
		queryID:      "refund-pending-123",
	}
	oldFactory := createPaymentProviderFromInstance
	createPaymentProviderFromInstance = func(string, string, map[string]string) (payment.Provider, error) {
		return providerDouble, nil
	}
	t.Cleanup(func() { createPaymentProviderFromInstance = oldFactory })

	user, err := client.User.Create().
		SetEmail("refund-pending@example.com").
		SetPasswordHash("hash").
		SetUsername("refund-pending-user").
		SetBalance(100).
		Save(ctx)
	require.NoError(t, err)

	inst, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeEasyPay).
		SetName("refund-pending-instance").
		SetConfig(encryptRefundPendingProviderConfig(t, map[string]string{"pid": "pid", "pkey": "pkey"})).
		SetSupportedTypes(payment.TypeEasyPay).
		SetEnabled(true).
		SetRefundEnabled(true).
		Save(ctx)
	require.NoError(t, err)
	instID := strconv.FormatInt(inst.ID, 10)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("REFUND-PENDING-ORDER").
		SetOutTradeNo("sub2_refund_pending_order").
		SetPaymentType(payment.TypeEasyPay).
		SetPaymentTradeNo("gateway-trade-refund-pending").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusCompleted).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetPaidAt(time.Now()).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderInstanceID(instID).
		SetProviderKey(payment.TypeEasyPay).
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{
		entClient:    client,
		loadBalancer: payment.NewDefaultLoadBalancer(client, []byte(refundPendingProviderEncryptionKey)),
		userRepo:     &openAIRecordUsageUserRepoStub{},
	}

	plan, early, err := svc.PrepareRefund(ctx, order.ID, 0, "customer request", false, false)
	require.NoError(t, err)
	require.Nil(t, early)

	result, err := svc.ExecuteRefund(ctx, plan)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.Contains(t, result.Warning, "pending")

	pendingOrder, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefundPending, pendingOrder.Status)
	require.Nil(t, pendingOrder.RefundAt)
	require.Equal(t, 88.0, pendingOrder.RefundAmount)

	finalResult, err := svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.NoError(t, err)
	require.True(t, finalResult.Success)
	require.Equal(t, "refund-pending-123", providerDouble.queryReq.RefundID)
	require.Equal(t, "gateway-trade-refund-pending", providerDouble.queryReq.TradeNo)

	finalOrder, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefunded, finalOrder.Status)
	require.NotNil(t, finalOrder.RefundAt)
}

func TestQueryAndFinalizeRefundKeepsPendingWhenGatewayStillPending(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)

	providerDouble := &refundPendingIntakeProvider{
		refundStatus: payment.ProviderStatusPending,
		refundID:     "refund-still-pending",
		queryStatus:  payment.ProviderStatusPending,
		queryID:      "refund-still-pending",
	}
	oldFactory := createPaymentProviderFromInstance
	createPaymentProviderFromInstance = func(string, string, map[string]string) (payment.Provider, error) {
		return providerDouble, nil
	}
	t.Cleanup(func() { createPaymentProviderFromInstance = oldFactory })

	user, err := client.User.Create().
		SetEmail("refund-still-pending@example.com").
		SetPasswordHash("hash").
		SetUsername("refund-still-pending-user").
		SetBalance(100).
		Save(ctx)
	require.NoError(t, err)

	inst, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeEasyPay).
		SetName("refund-still-pending-instance").
		SetConfig(encryptRefundPendingProviderConfig(t, map[string]string{"pid": "pid", "pkey": "pkey"})).
		SetSupportedTypes(payment.TypeEasyPay).
		SetEnabled(true).
		SetRefundEnabled(true).
		Save(ctx)
	require.NoError(t, err)
	instID := strconv.FormatInt(inst.ID, 10)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(42).
		SetPayAmount(42).
		SetFeeRate(0).
		SetRechargeCode("REFUND-STILL-PENDING-ORDER").
		SetOutTradeNo("sub2_refund_still_pending_order").
		SetPaymentType(payment.TypeEasyPay).
		SetPaymentTradeNo("gateway-trade-still-pending").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusCompleted).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetPaidAt(time.Now()).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderInstanceID(instID).
		SetProviderKey(payment.TypeEasyPay).
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{
		entClient:    client,
		loadBalancer: payment.NewDefaultLoadBalancer(client, []byte(refundPendingProviderEncryptionKey)),
		userRepo:     &openAIRecordUsageUserRepoStub{},
	}

	plan, early, err := svc.PrepareRefund(ctx, order.ID, 0, "customer request", false, false)
	require.NoError(t, err)
	require.Nil(t, early)
	_, err = svc.ExecuteRefund(ctx, plan)
	require.NoError(t, err)

	result, err := svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.True(t, strings.Contains(result.Warning, "still pending") || strings.Contains(result.Warning, "pending confirmation"))

	stillPendingOrder, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefundPending, stillPendingOrder.Status)
	require.Nil(t, stillPendingOrder.RefundAt)
}
