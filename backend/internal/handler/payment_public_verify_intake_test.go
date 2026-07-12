package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func newPaymentPublicVerifyIntakeClient(t *testing.T) *dbent.Client {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestVerifyOrderPublicReturnsMinimalAnonymousRefundState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	client := newPaymentPublicVerifyIntakeClient(t)

	user, err := client.User.Create().
		SetEmail("public-minimal@example.com").
		SetPasswordHash("hash").
		SetUsername("public-minimal-user").
		Save(ctx)
	require.NoError(t, err)

	_, err = client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(90.64).
		SetFeeRate(0.03).
		SetRechargeCode("PUBLIC-MINIMAL").
		SetOutTradeNo("legacy-minimal-order-no").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("trade-public-minimal").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(service.OrderStatusRefundPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetPaidAt(time.Now()).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderSnapshot(map[string]any{"currency": "HKD"}).
		Save(ctx)
	require.NoError(t, err)

	paymentSvc := service.NewPaymentService(client, payment.NewRegistry(), nil, nil, nil, nil, nil, nil, nil)
	h := NewPaymentHandler(paymentSvc, nil, nil)

	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/v1/payment/public/orders/verify",
		bytes.NewBufferString(`{"out_trade_no":"legacy-minimal-order-no"}`),
	)
	ginCtx.Request.Header.Set("Content-Type", "application/json")

	h.VerifyOrderPublic(ginCtx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var resp struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, "legacy-minimal-order-no", resp.Data["out_trade_no"])
	require.Equal(t, service.OrderStatusRefundPending, resp.Data["status"])
	require.Equal(t, true, resp.Data["paid"])
	require.Contains(t, resp.Data, "created_at")
	require.Contains(t, resp.Data, "expires_at")
	require.NotContains(t, resp.Data, "id")
	require.NotContains(t, resp.Data, "amount")
	require.NotContains(t, resp.Data, "pay_amount")
	require.NotContains(t, resp.Data, "currency")
	require.NotContains(t, resp.Data, "payment_type")
	require.NotContains(t, resp.Data, "order_type")
	require.NotContains(t, resp.Data, "refund_amount")
}
