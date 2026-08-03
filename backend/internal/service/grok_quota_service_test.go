package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type grokQuotaAccountRepo struct {
	AccountRepository
	accounts map[int64]*Account
	updates  map[int64]map[string]any
}

func (r *grokQuotaAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	if r.accounts != nil {
		if account, ok := r.accounts[id]; ok {
			return account, nil
		}
	}
	return nil, ErrAccountNotFound
}

func (r *grokQuotaAccountRepo) UpdateExtra(_ context.Context, id int64, updates map[string]any) error {
	if r.updates == nil {
		r.updates = make(map[int64]map[string]any)
	}
	r.updates[id] = updates
	return nil
}

type grokQuotaProxyRepo struct {
	ProxyRepository
	proxies map[int64]*Proxy
	calls   int
}

func (r *grokQuotaProxyRepo) GetByID(_ context.Context, id int64) (*Proxy, error) {
	r.calls++
	return r.proxies[id], nil
}

type grokQuotaHTTPUpstreamRecorder struct {
	resp         *http.Response
	err          error
	lastReq      *http.Request
	lastBody     []byte
	lastProxyURL string
}

func (u *grokQuotaHTTPUpstreamRecorder) Do(req *http.Request, proxyURL string, _ int64, _ int) (*http.Response, error) {
	u.lastReq = req
	u.lastProxyURL = proxyURL
	if req != nil && req.Body != nil {
		u.lastBody, _ = io.ReadAll(req.Body)
	}
	if u.err != nil {
		return nil, u.err
	}
	return u.resp, nil
}

func (u *grokQuotaHTTPUpstreamRecorder) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func TestGrokQuotaServiceProbeUsageStoresHeaders(t *testing.T) {
	t.Parallel()

	account := &Account{ID: 42, Platform: PlatformGrok, Type: AccountTypeOAuth, Concurrency: 1, Credentials: map[string]any{
		"access_token": "access-token",
		"expires_at":   time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}}
	repo := &grokQuotaAccountRepo{accounts: map[int64]*Account{42: account}}
	upstream := &grokQuotaHTTPUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{
		"X-Ratelimit-Limit-Requests":     []string{"10"},
		"X-Ratelimit-Remaining-Requests": []string{"7"},
		"X-Ratelimit-Reset-Requests":     []string{"2000000000"},
		"X-Ratelimit-Limit-Tokens":       []string{"1000"},
		"X-Ratelimit-Remaining-Tokens":   []string{"900"},
	}, Body: io.NopCloser(strings.NewReader(`{"id":"resp_probe"}`))}}
	svc := NewGrokQuotaService(repo, nil, NewGrokTokenProvider(repo, nil), upstream)

	result, err := svc.ProbeUsage(context.Background(), 42)
	require.NoError(t, err)
	require.Equal(t, grokQuotaDefaultModel, result.Model)
	require.Equal(t, http.StatusOK, result.StatusCode)
	require.True(t, result.HeadersObserved)
	require.NotNil(t, result.Snapshot)
	require.True(t, result.Snapshot.HeadersObserved)
	require.Equal(t, "active_probe", result.Snapshot.ObservationSource)
	require.NotEmpty(t, result.Snapshot.LastProbeAt)
	require.NotEmpty(t, result.Snapshot.LastHeadersSeenAt)
	require.NotNil(t, result.Snapshot.Requests)
	require.EqualValues(t, 10, *result.Snapshot.Requests.Limit)
	require.EqualValues(t, 7, *result.Snapshot.Requests.Remaining)
	require.Equal(t, "https://api.x.ai/v1/responses", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer access-token", upstream.lastReq.Header.Get("Authorization"))
	require.Contains(t, string(upstream.lastBody), `"max_output_tokens":1`)
	require.Contains(t, string(upstream.lastBody), `"store":false`)
	require.NotNil(t, repo.updates[42][grokQuotaSnapshotExtraKey])
}

func TestGrokQuotaServiceProbeUsageUsesStableDefaultModel(t *testing.T) {
	t.Parallel()

	account := &Account{ID: 47, Platform: PlatformGrok, Type: AccountTypeOAuth, Concurrency: 1, Credentials: map[string]any{
		"access_token": "access-token",
		"expires_at":   time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		"model_mapping": map[string]any{
			"grok": "grok-composer-2.5-fast",
		},
	}}
	repo := &grokQuotaAccountRepo{accounts: map[int64]*Account{47: account}}
	upstream := &grokQuotaHTTPUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"id":"resp_probe"}`))}}
	svc := NewGrokQuotaService(repo, nil, NewGrokTokenProvider(repo, nil), upstream)

	_, err := svc.ProbeUsage(context.Background(), 47)

	require.NoError(t, err)
	require.Equal(t, grokQuotaDefaultModel, gjson.GetBytes(upstream.lastBody, "model").String())
}

func TestGrokQuotaServiceProbeUsageLoadsProxyWhenAccountEdgeMissing(t *testing.T) {
	t.Parallel()

	proxyID := int64(7)
	account := &Account{ID: 46, Platform: PlatformGrok, Type: AccountTypeOAuth, Concurrency: 1, ProxyID: &proxyID, Credentials: map[string]any{
		"access_token": "access-token",
		"expires_at":   time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}}
	repo := &grokQuotaAccountRepo{accounts: map[int64]*Account{46: account}}
	proxyRepo := &grokQuotaProxyRepo{proxies: map[int64]*Proxy{proxyID: {ID: proxyID, Protocol: "http", Host: "proxy.test", Port: 3128}}}
	upstream := &grokQuotaHTTPUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"id":"resp_probe"}`))}}
	svc := NewGrokQuotaService(repo, proxyRepo, NewGrokTokenProvider(repo, nil), upstream)

	_, err := svc.ProbeUsage(context.Background(), 46)
	require.NoError(t, err)
	require.Equal(t, 1, proxyRepo.calls)
	require.Equal(t, "http://proxy.test:3128", upstream.lastProxyURL)
}

func TestGrokQuotaServiceProbeUsageStoresNoHeadersState(t *testing.T) {
	t.Parallel()

	account := &Account{ID: 45, Platform: PlatformGrok, Type: AccountTypeOAuth, Concurrency: 1, Credentials: map[string]any{
		"access_token": "access-token",
		"expires_at":   time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}}
	repo := &grokQuotaAccountRepo{accounts: map[int64]*Account{45: account}}
	upstream := &grokQuotaHTTPUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"id":"resp_probe"}`))}}
	svc := NewGrokQuotaService(repo, nil, NewGrokTokenProvider(repo, nil), upstream)

	result, err := svc.ProbeUsage(context.Background(), 45)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.StatusCode)
	require.False(t, result.HeadersObserved)
	require.NotNil(t, result.Snapshot)
	require.False(t, result.Snapshot.HeadersObserved)
	require.Equal(t, "active_probe", result.Snapshot.ObservationSource)
	require.NotEmpty(t, result.Snapshot.LastProbeAt)
	require.Empty(t, result.Snapshot.LastHeadersSeenAt)

	stored, ok := repo.updates[45][grokQuotaSnapshotExtraKey].(*xai.QuotaSnapshot)
	require.True(t, ok)
	require.False(t, stored.HeadersObserved)
	require.Equal(t, http.StatusOK, stored.StatusCode)
}

func TestGrokQuotaServiceProbeUsageReturnsRateLimitedSnapshot(t *testing.T) {
	t.Parallel()

	account := &Account{ID: 43, Platform: PlatformGrok, Type: AccountTypeOAuth, Credentials: map[string]any{
		"access_token": "access-token",
		"expires_at":   time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}}
	repo := &grokQuotaAccountRepo{accounts: map[int64]*Account{43: account}}
	upstream := &grokQuotaHTTPUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": []string{"45"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"rate limited"}}`))}}
	svc := NewGrokQuotaService(repo, nil, NewGrokTokenProvider(repo, nil), upstream)

	result, err := svc.ProbeUsage(context.Background(), 43)
	require.NoError(t, err)
	require.Equal(t, http.StatusTooManyRequests, result.StatusCode)
	require.NotNil(t, result.Snapshot)
	require.NotNil(t, result.Snapshot.RetryAfterSeconds)
	require.Equal(t, 45, *result.Snapshot.RetryAfterSeconds)
}

func TestGrokQuotaServiceResetQuotaUnsupported(t *testing.T) {
	t.Parallel()

	account := &Account{ID: 44, Platform: PlatformGrok, Type: AccountTypeOAuth}
	repo := &grokQuotaAccountRepo{accounts: map[int64]*Account{44: account}}
	svc := NewGrokQuotaService(repo, nil, nil, nil)

	_, err := svc.ResetQuota(context.Background(), 44)
	require.Error(t, err)
	require.Equal(t, http.StatusNotImplemented, infraerrors.Code(err))
	require.Equal(t, "GROK_QUOTA_RESET_UNSUPPORTED", infraerrors.Reason(err))
}

func TestShouldAutoPauseGrokAccountByQuota(t *testing.T) {
	t.Parallel()

	zero := int64(0)
	limit := int64(10)
	resetFuture := time.Now().Add(time.Minute).Unix()
	retryAfter := 30
	updatedNow := time.Now().UTC().Format(time.RFC3339)
	updatedPast := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	tests := []struct {
		name     string
		snapshot xai.QuotaSnapshot
		want     bool
	}{
		{name: "remaining requests exhausted", snapshot: xai.QuotaSnapshot{Requests: &xai.QuotaWindow{Limit: &limit, Remaining: &zero, ResetUnix: &resetFuture}, UpdatedAt: updatedNow}, want: true},
		{name: "active retry after", snapshot: xai.QuotaSnapshot{RetryAfterSeconds: &retryAfter, UpdatedAt: updatedNow}, want: true},
		{name: "expired retry after", snapshot: xai.QuotaSnapshot{RetryAfterSeconds: &retryAfter, UpdatedAt: updatedPast}, want: false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			account := &Account{ID: 9, Platform: PlatformGrok, Type: AccountTypeOAuth, Extra: map[string]any{grokQuotaSnapshotExtraKey: &tt.snapshot}}
			paused, _ := shouldAutoPauseGrokAccountByQuota(account)
			require.Equal(t, tt.want, paused)
		})
	}
}
