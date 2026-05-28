package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type formalPoolHealthcheckRepo struct{ formalRateLimitRepo }

type formalPoolHealthcheckUpstream struct {
	resp          *http.Response
	err           error
	requests      int
	lastURL       string
	lastAccountID int64
	lastProxy     string
	lastBody      []byte
}

func (u *formalPoolHealthcheckUpstream) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	return u.DoWithTLS(req, proxyURL, accountID, accountConcurrency, nil)
}

func (u *formalPoolHealthcheckUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	u.requests++
	u.lastURL = req.URL.String()
	u.lastAccountID = accountID
	u.lastProxy = proxyURL
	if req.Body != nil {
		body, _ := io.ReadAll(req.Body)
		u.lastBody = body
		_ = req.Body.Close()
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	if u.err != nil {
		return nil, u.err
	}
	return u.resp, nil
}

func newFormalPoolHealthcheckAccount() *Account {
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	account.ID = 7001
	account.Type = AccountTypeSetupToken
	account.Credentials["scope"] = "user:inference"
	account.Extra["cc_gateway_enabled"] = "true"
	account.Extra["cc_gateway_canary_only"] = "false"
	account.Extra["cc_gateway_policy_version"] = ccGatewayAnthropicPolicyVersion
	account.Extra["cc_gateway_routes"] = "native_messages"
	account.Extra["cc_gateway_egress_bucket_enabled"] = "true"
	account.Extra["cc_gateway_egress_bucket"] = "bucket-a"
	account.Extra["cc_gateway_account_ref"] = "hmac-sha256:acct-ref"
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageRuntimeRegistered
	account.Schedulable = false
	return account
}

func TestFormalPoolGatewayHealthcheckRunnerRequiresCCGatewayEvidence(t *testing.T) {
	t.Parallel()
	account := newFormalPoolHealthcheckAccount()
	repo := &formalPoolHealthcheckRepo{formalRateLimitRepo{accountsByID: map[int64]*Account{account.ID: account}}}
	upstream := &formalPoolHealthcheckUpstream{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))}}
	runner := NewFormalPoolGatewayHealthcheckRunner(repo, upstream, &config.Config{Gateway: config.GatewayConfig{CCGateway: config.GatewayCCGatewayConfig{Enabled: true, BaseURL: "http://cc-gateway:8443", Providers: config.GatewayCCGatewayProvidersConfig{Anthropic: true}}}}, nil)

	got, err := runner.RunHealthcheck(context.Background(), FormalPoolAcceptanceInput{AccountID: account.ID, AccountRef: "hmac-sha256:acct-ref", EgressBucket: "bucket-a", ProxyRef: "hmac-sha256:proxy-ref", PoolProfile: PoolProfileNormal})

	require.NoError(t, err)
	require.Equal(t, "failed_acceptance", got.Status)
	require.False(t, got.CCGatewaySeen)
	require.False(t, got.RawCapturePresent)
	require.Equal(t, "status_2xx", got.StatusCodeBucket)
	require.Equal(t, 1, upstream.requests)
	require.Equal(t, account.ID, upstream.lastAccountID)
	require.Contains(t, upstream.lastURL, "http://cc-gateway:8443/v1/messages?beta=true")
}

func TestFormalPoolGatewayHealthcheckRunnerPassesWithSafeEvidence(t *testing.T) {
	t.Parallel()
	account := newFormalPoolHealthcheckAccount()
	repo := &formalPoolHealthcheckRepo{formalRateLimitRepo{accountsByID: map[int64]*Account{account.ID: account}}}
	upstream := &formalPoolHealthcheckUpstream{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"X-Cc-Gateway-Seen": []string{"1"}, "X-Cc-Gateway-Raw-Capture-Ref": []string{"hmac-sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}, Body: io.NopCloser(strings.NewReader("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))}}
	runner := NewFormalPoolGatewayHealthcheckRunner(repo, upstream, &config.Config{Gateway: config.GatewayConfig{CCGateway: config.GatewayCCGatewayConfig{Enabled: true, BaseURL: "http://cc-gateway:8443", Providers: config.GatewayCCGatewayProvidersConfig{Anthropic: true}}}}, nil)

	got, err := runner.RunHealthcheck(context.Background(), FormalPoolAcceptanceInput{AccountID: account.ID, AccountRef: "hmac-sha256:acct-ref", EgressBucket: "bucket-a", ProxyRef: "hmac-sha256:proxy-ref", PoolProfile: PoolProfileNormal})

	require.NoError(t, err)
	require.True(t, got.FormalPoolHealthcheckPassed(), "%#v", got)
	require.Equal(t, "hmac-sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", got.RawCaptureRef)
}

func TestFormalPoolGatewayHealthcheckRunnerQuarantinesAuthFailure(t *testing.T) {
	t.Parallel()
	account := newFormalPoolHealthcheckAccount()
	repo := &formalPoolHealthcheckRepo{formalRateLimitRepo{accountsByID: map[int64]*Account{account.ID: account}}}
	upstream := &formalPoolHealthcheckUpstream{resp: &http.Response{StatusCode: http.StatusUnauthorized, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"invalid credentials"}}`))}}
	runner := NewFormalPoolGatewayHealthcheckRunner(repo, upstream, &config.Config{Gateway: config.GatewayConfig{CCGateway: config.GatewayCCGatewayConfig{Enabled: true, BaseURL: "http://cc-gateway:8443", Providers: config.GatewayCCGatewayProvidersConfig{Anthropic: true}}}}, nil)

	got, err := runner.RunHealthcheck(context.Background(), FormalPoolAcceptanceInput{AccountID: account.ID, AccountRef: "hmac-sha256:acct-ref", EgressBucket: "bucket-a", ProxyRef: "hmac-sha256:proxy-ref", PoolProfile: PoolProfileNormal})

	require.NoError(t, err)
	require.Equal(t, "failed_acceptance", got.Status)
	require.Equal(t, FormalPoolStageQuarantined, repo.accountsByID[account.ID].Extra[FormalPoolExtraOnboardingStage])
	require.False(t, repo.accountsByID[account.ID].Schedulable)
	require.NotEmpty(t, repo.accountsByID[account.ID].Extra[FormalPoolExtraRiskEventRef])
}
