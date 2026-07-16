package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestParseOpenAINativeSearchEnvelope(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantID    string
		wantModel string
		wantErr   bool
	}{
		{name: "commands absent", body: `{"id":"session","model":"gpt-5.4"}`, wantID: "session", wantModel: "gpt-5.4"},
		{name: "commands empty object", body: `{"id":"session","model":"gpt-5.4","commands":{}}`, wantID: "session", wantModel: "gpt-5.4"},
		{name: "commands populated", body: `{"id":"session","model":"gpt-5.4","commands":{"search_query":[{"q":"opaque"}]}}`, wantID: "session", wantModel: "gpt-5.4"},
		{name: "empty body", body: ``, wantErr: true},
		{name: "array", body: `[]`, wantErr: true},
		{name: "null", body: `null`, wantErr: true},
		{name: "missing id", body: `{"model":"gpt-5.4"}`, wantErr: true},
		{name: "empty id", body: `{"id":" ","model":"gpt-5.4"}`, wantErr: true},
		{name: "missing model", body: `{"id":"session"}`, wantErr: true},
		{name: "empty model", body: `{"id":"session","model":""}`, wantErr: true},
		{name: "commands null", body: `{"id":"session","model":"gpt-5.4","commands":null}`, wantErr: true},
		{name: "commands array", body: `{"id":"session","model":"gpt-5.4","commands":[]}`, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, model, err := parseOpenAINativeSearchEnvelope([]byte(tc.body))
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantID, id)
			require.Equal(t, tc.wantModel, model)
		})
	}
}

type nativeSearchHandlerAccountRepo struct {
	service.AccountRepository
	accounts []service.Account
}

func (r nativeSearchHandlerAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			account := r.accounts[i]
			return &account, nil
		}
	}
	return nil, service.ErrNoAvailableAccounts
}

func (r nativeSearchHandlerAccountRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, _ int64, platform string) ([]service.Account, error) {
	return r.forPlatform(platform), nil
}

func (r nativeSearchHandlerAccountRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	return r.forPlatform(platform), nil
}

func (r nativeSearchHandlerAccountRepo) ListSchedulableUngroupedByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	return r.forPlatform(platform), nil
}

func (r nativeSearchHandlerAccountRepo) forPlatform(platform string) []service.Account {
	var result []service.Account
	for _, account := range r.accounts {
		if account.Platform == platform {
			result = append(result, account)
		}
	}
	return result
}

type nativeSearchHandlerUpstream struct {
	service.HTTPUpstream
	mu         sync.Mutex
	accountIDs []int64
}

type nativeSearchBillingCache struct {
	service.BillingCache
	balance float64
}

func (c *nativeSearchBillingCache) GetUserBalance(context.Context, int64) (float64, error) {
	return c.balance, nil
}

func (c *nativeSearchBillingCache) GetSubscriptionCache(context.Context, int64, int64) (*service.SubscriptionCacheData, error) {
	return &service.SubscriptionCacheData{
		Status:    service.SubscriptionStatusActive,
		ExpiresAt: time.Now().Add(time.Hour),
	}, nil
}

func (u *nativeSearchHandlerUpstream) Do(_ *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	return u.response(accountID)
}

func (u *nativeSearchHandlerUpstream) DoWithTLS(_ *http.Request, _ string, accountID int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.response(accountID)
}

func (u *nativeSearchHandlerUpstream) response(accountID int64) (*http.Response, error) {
	u.mu.Lock()
	u.accountIDs = append(u.accountIDs, accountID)
	u.mu.Unlock()
	if accountID == 1 {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{"error":{"type":"server_error","message":"temporary"}}`)),
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(`{"encrypted_output":"opaque","output":"native result"}`)),
	}, nil
}

func (u *nativeSearchHandlerUpstream) calls() []int64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]int64(nil), u.accountIDs...)
}

func TestNativeSearch_FailsOverAndReleasesConcurrencySlots(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(501)
	accounts := []service.Account{
		{ID: 1, Name: "search-1", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, Credentials: map[string]any{"access_token": "token-1", "chatgpt_account_id": "acct-1"}},
		{ID: 2, Name: "search-2", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1, Credentials: map[string]any{"access_token": "token-2", "chatgpt_account_id": "acct-2"}},
	}
	repo := nativeSearchHandlerAccountRepo{accounts: accounts}
	upstream := &nativeSearchHandlerUpstream{}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.OpenAICore.Enabled = true
	cache := &concurrencyCacheMock{
		acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
		acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
	}
	concurrencyService := service.NewConcurrencyService(cache)
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	gatewayCore := service.NewOpenAIGatewayCoreService(repo, cfg, nil)
	gatewayService := service.NewOpenAIGatewayService(
		repo, nil, nil, nil, nil, nil, nil, cfg, nil, concurrencyService, nil, nil,
		billingCache, upstream, nil, nil,
	)
	h := NewOpenAIGatewayHandler(
		gatewayService,
		concurrencyService,
		gatewayCore,
		billingCache,
		service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg),
		cfg,
	)
	h.maxAccountSwitches = 3

	req := httptest.NewRequest(http.MethodPost, "/alpha/search", bytes.NewBufferString(`{"id":"session","model":"gpt-5.4","commands":{}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		ID:      901,
		GroupID: &groupID,
		Group:   &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive},
		User:    &service.User{ID: 902, Status: service.StatusActive},
	})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 902, Concurrency: 1})

	h.NativeSearch(c)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String(), upstream.calls())
	require.JSONEq(t, `{"encrypted_output":"opaque","output":"native result"}`, rec.Body.String())
	require.Equal(t, []int64{1, 2}, upstream.calls())
	require.Equal(t, int32(1), atomic.LoadInt32(&cache.releaseUserCalled))
	require.Equal(t, int32(2), atomic.LoadInt32(&cache.releaseAccountCalled))
	require.Equal(t, int32(1), atomic.LoadInt32(&cache.releaseAPIKeyCalled))
}

func TestNativeSearch_RechecksSubscriptionAfterUserSlotWait(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(601)
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true

	req := httptest.NewRequest(http.MethodPost, "/alpha/search", bytes.NewBufferString(`{"id":"session","model":"gpt-5.4","commands":{}}`))
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		ID:      602,
		GroupID: &groupID,
		Group: &service.Group{
			ID:               groupID,
			Platform:         service.PlatformOpenAI,
			Status:           service.StatusActive,
			SubscriptionType: service.SubscriptionTypeSubscription,
		},
		User: &service.User{ID: 603, Status: service.StatusActive},
	})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 603, Concurrency: 1})
	c.Set(string(middleware2.ContextKeySubscription), &service.UserSubscription{
		ID:        604,
		UserID:    603,
		GroupID:   groupID,
		Status:    service.SubscriptionStatusActive,
		ExpiresAt: time.Now().Add(time.Hour),
	})

	cache := &concurrencyCacheMock{
		acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) {
			c.Set(string(middleware2.ContextKeySubscription), (*service.UserSubscription)(nil))
			return true, nil
		},
	}
	concurrencyService := service.NewConcurrencyService(cache)
	billingCache := service.NewBillingCacheService(&nativeSearchBillingCache{balance: -1}, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	repo := nativeSearchHandlerAccountRepo{}
	h := NewOpenAIGatewayHandler(
		service.NewOpenAIGatewayService(
			repo, nil, nil, nil, nil, nil, nil, cfg, nil, concurrencyService, nil, nil,
			billingCache, &nativeSearchHandlerUpstream{}, nil, nil,
		),
		concurrencyService,
		service.NewOpenAIGatewayCoreService(repo, cfg, nil),
		billingCache,
		service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg),
		cfg,
	)

	h.NativeSearch(c)

	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "insufficient balance")
	require.Equal(t, int32(1), atomic.LoadInt32(&cache.releaseUserCalled))
}

func TestNativeSearch_RejectsInvalidEnvelopeBeforeAdmission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cache := &concurrencyCacheMock{
		acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) {
			t.Fatal("invalid request must not acquire a user slot")
			return false, nil
		},
	}
	h := &OpenAIGatewayHandler{
		concurrencyHelper: NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, time.Second),
	}
	req := httptest.NewRequest(http.MethodPost, "/alpha/search", bytes.NewBufferString(`{"id":"session","model":"gpt-5.4","commands":[]}`))
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	groupID := int64(701)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		ID:      702,
		GroupID: &groupID,
		Group:   &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive},
		User:    &service.User{ID: 703, Status: service.StatusActive},
	})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 703, Concurrency: 1})

	h.NativeSearch(c)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}
