package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

type openAIQuotaAccountRepoStub struct {
	AccountRepository
	accounts map[int64]*Account
}

func (r *openAIQuotaAccountRepoStub) GetByID(_ context.Context, id int64) (*Account, error) {
	if r == nil || r.accounts == nil {
		return nil, fmt.Errorf("account %d not found", id)
	}
	acc, ok := r.accounts[id]
	if !ok {
		return nil, fmt.Errorf("account %d not found", id)
	}
	return acc, nil
}

type openAIQuotaTokenCacheStub struct {
	tokens map[string]string
}

func (c *openAIQuotaTokenCacheStub) GetAccessToken(_ context.Context, key string) (string, error) {
	if c != nil && c.tokens != nil {
		if token := strings.TrimSpace(c.tokens[key]); token != "" {
			return token, nil
		}
	}
	return "", errors.New("token not found")
}

func (c *openAIQuotaTokenCacheStub) SetAccessToken(_ context.Context, _ string, _ string, _ time.Duration) error {
	return nil
}

func (c *openAIQuotaTokenCacheStub) DeleteAccessToken(_ context.Context, _ string) error { return nil }
func (c *openAIQuotaTokenCacheStub) AcquireRefreshLock(_ context.Context, _ string, _ time.Duration) (bool, error) {
	return true, nil
}
func (c *openAIQuotaTokenCacheStub) ReleaseRefreshLock(_ context.Context, _ string) error { return nil }

func newOpenAIQuotaRedirectingFactory(t *testing.T, srv *httptest.Server) PrivacyClientFactory {
	t.Helper()
	targetURL, err := url.Parse(srv.URL)
	require.NoError(t, err)
	return func(_ string) (*req.Client, error) {
		client := req.C().WrapRoundTripFunc(func(rt req.RoundTripper) req.RoundTripFunc {
			return func(r *req.Request) (*req.Response, error) {
				r.URL.Scheme = targetURL.Scheme
				r.URL.Host = targetURL.Host
				return rt.RoundTrip(r)
			}
		})
		return client, nil
	}
}

func newOpenAIQuotaTestService(t *testing.T, account *Account, srv *httptest.Server) *OpenAIQuotaService {
	t.Helper()
	repo := &openAIQuotaAccountRepoStub{accounts: map[int64]*Account{account.ID: account}}
	tokenCache := &openAIQuotaTokenCacheStub{tokens: map[string]string{
		OpenAITokenCacheKey(account): "test-access-token",
	}}
	tokenProvider := NewOpenAITokenProvider(repo, tokenCache, nil)
	return NewOpenAIQuotaService(repo, nil, tokenProvider, newOpenAIQuotaRedirectingFactory(t, srv))
}

func TestParseOpenAIRateLimitResetCreditDetailsSanitizesIDs(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{name: "credits", body: `{"credits":[{"id":"secret-credit-id","expires_at":"2026-07-03T04:05:06Z"}]}`, want: []string{"2026-07-03T04:05:06Z"}},
		{name: "rate_limit_reset_credits", body: `{"rate_limit_reset_credits":[{"expiresAt":"2026-07-04T04:05:06Z"}]}`, want: []string{"2026-07-04T04:05:06Z"}},
		{name: "items", body: `{"items":[{"expires_at":"2026-07-05T04:05:06Z"}]}`, want: []string{"2026-07-05T04:05:06Z"}},
		{name: "data", body: `{"data":[{"expires_at":"2026-07-06T04:05:06Z"}]}`, want: []string{"2026-07-06T04:05:06Z"}},
		{name: "array", body: `[{"expires_at":"2026-07-07T04:05:06Z"}]`, want: []string{"2026-07-07T04:05:06Z"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseOpenAIRateLimitResetCreditDetails([]byte(tt.body))
			require.NoError(t, err)
			require.Len(t, got, len(tt.want))
			for i := range tt.want {
				require.Equal(t, tt.want[i], got[i].ExpiresAt)
			}
			encoded, err := json.Marshal(got)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "secret-credit-id")
		})
	}
}

func TestOpenAIQuotaServiceQueryUsageIncludesResetCreditExpirationsSafely(t *testing.T) {
	account := &Account{
		ID:       100,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"chatgpt_account_id": "org-parent123",
		},
	}

	var sawUsage bool
	var sawDetails bool
	var capturedAuth string
	var capturedAccountID string
	var capturedBeta string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		switch r.URL.Path {
		case "/backend-api/wham/usage":
			sawUsage = true
			capturedAuth = r.Header.Get("Authorization")
			capturedAccountID = r.Header.Get("ChatGPT-Account-ID")
			capturedBeta = r.Header.Get("OpenAI-Beta")
			_ = json.NewEncoder(w).Encode(OpenAIQuotaUsage{
				RateLimitResetCredits: &OpenAIRateLimitResetCredits{AvailableCount: 2},
			})
		case "/backend-api/wham/rate-limit-reset-credits":
			sawDetails = true
			_, _ = w.Write([]byte(`{"credits":[{"id":"secret-credit-id","expires_at":"2026-07-03T04:05:06Z"},{"expiresAt":"2026-07-04T04:05:06Z"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	svc := newOpenAIQuotaTestService(t, account, srv)
	usage, err := svc.QueryUsage(context.Background(), 100)
	require.NoError(t, err)
	require.True(t, sawUsage)
	require.True(t, sawDetails)
	require.Equal(t, "Bearer test-access-token", capturedAuth)
	require.Equal(t, "org-parent123", capturedAccountID)
	require.Equal(t, openaiQuotaCodexBeta, capturedBeta)
	require.NotNil(t, usage.RateLimitResetCredits)
	require.Equal(t, 2, usage.RateLimitResetCredits.AvailableCount)
	require.Equal(t, []OpenAIRateLimitResetCreditDetail{
		{ExpiresAt: "2026-07-03T04:05:06Z"},
		{ExpiresAt: "2026-07-04T04:05:06Z"},
	}, usage.RateLimitResetCredits.Credits)

	encoded, err := json.Marshal(usage)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "secret-credit-id")
	require.NotContains(t, string(encoded), "org-parent123")
	require.NotContains(t, string(encoded), "test-access-token")
}

func TestOpenAIQuotaServiceResetCreditConsumesOneCredit(t *testing.T) {
	account := &Account{
		ID:       101,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"chatgpt_account_id": "org-reset123",
		},
	}

	var body map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		require.Equal(t, "/backend-api/wham/rate-limit-reset-credits/consume", r.URL.Path)
		require.Equal(t, "org-reset123", r.Header.Get("ChatGPT-Account-ID"))
		require.Equal(t, openaiQuotaCodexBeta, r.Header.Get("OpenAI-Beta"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.NotEmpty(t, body["redeem_request_id"])
		_, _ = w.Write([]byte(`{"code":"ok","credit":{"id":"secret-reset-id","status":"redeemed","expires_at":"2026-07-03T04:05:06Z"},"windows_reset":1}`))
	}))
	defer srv.Close()

	svc := newOpenAIQuotaTestService(t, account, srv)
	result, err := svc.ResetCredit(context.Background(), 101)
	require.NoError(t, err)
	require.Equal(t, "ok", result.Code)
	require.Equal(t, 1, result.WindowsReset)
	require.NotNil(t, result.Credit)
	require.Equal(t, "redeemed", result.Credit.Status)

	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "secret-reset-id")
	require.NotContains(t, string(encoded), "org-reset123")
}

func TestOpenAIQuotaServiceRejectsNonOpenAIOAuthAccounts(t *testing.T) {
	account := &Account{ID: 102, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive}
	repo := &openAIQuotaAccountRepoStub{accounts: map[int64]*Account{102: account}}
	svc := NewOpenAIQuotaService(repo, nil, NewOpenAITokenProvider(repo, nil, nil), func(_ string) (*req.Client, error) {
		return req.C(), nil
	})

	_, err := svc.QueryUsage(context.Background(), 102)
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
	require.Contains(t, err.Error(), "OAuth")
}
