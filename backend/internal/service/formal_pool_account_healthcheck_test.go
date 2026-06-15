package service

import (
	"bytes"
	"context"
	"encoding/json"
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
	lastHeaders   http.Header
}

func (u *formalPoolHealthcheckUpstream) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	return u.DoWithTLS(req, proxyURL, accountID, accountConcurrency, nil)
}

func (u *formalPoolHealthcheckUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	u.requests++
	u.lastURL = req.URL.String()
	u.lastAccountID = accountID
	u.lastProxy = proxyURL
	u.lastHeaders = req.Header.Clone()
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

func TestFormalPoolGatewayHealthcheckRunnerUsesClaudeCodeLiteBodyWithoutOneMillionContext(t *testing.T) {
	t.Parallel()
	account := newFormalPoolHealthcheckAccount()
	repo := &formalPoolHealthcheckRepo{formalRateLimitRepo{accountsByID: map[int64]*Account{account.ID: account}}}
	upstream := &formalPoolHealthcheckUpstream{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"X-Cc-Gateway-Seen": []string{"1"}, "X-Cc-Gateway-Raw-Capture-Ref": []string{"hmac-sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}, Body: io.NopCloser(strings.NewReader("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))}}
	runner := NewFormalPoolGatewayHealthcheckRunner(repo, upstream, &config.Config{Gateway: config.GatewayConfig{CCGateway: config.GatewayCCGatewayConfig{Enabled: true, BaseURL: "http://cc-gateway:8443", Providers: config.GatewayCCGatewayProvidersConfig{Anthropic: true}}}}, nil)

	_, err := runner.RunHealthcheck(context.Background(), FormalPoolAcceptanceInput{AccountID: account.ID, AccountRef: "hmac-sha256:acct-ref", EgressBucket: "bucket-a", ProxyRef: "hmac-sha256:proxy-ref", PoolProfile: PoolProfileNormal})

	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(upstream.lastBody, &body))
	require.Equal(t, "claude-sonnet-4-6", body["model"])
	require.EqualValues(t, 1024, body["max_tokens"])
	require.Equal(t, false, body["stream"])
	require.Contains(t, body, "metadata")
	require.Contains(t, body, "system")
	require.Contains(t, body, "tools")
	require.NotContains(t, body, "thinking")
	require.Contains(t, body, "output_config")
	require.NotContains(t, body, "context_management")
	system, ok := body["system"].([]any)
	require.True(t, ok)
	require.Len(t, system, 2)
	require.Contains(t, system[0].(map[string]any)["text"], "<env>")
	require.Contains(t, system[1].(map[string]any)["text"], "Claude Code")
	tools, ok := body["tools"].([]any)
	require.True(t, ok)
	require.Empty(t, tools)
	outputConfig, ok := body["output_config"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "low", outputConfig["effort"])
	require.NotContains(t, outputConfig, "format")
	require.NotContains(t, strings.ToLower(upstream.lastHeaders.Get("anthropic-beta")), "context-1m")
	require.Equal(t, "claude_code_2_1_175_api_key_non_1m", getHeaderRaw(upstream.lastHeaders, ccGatewayHealthcheckPersonaHeader))
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

func TestFormalPoolGatewayHealthcheckRunnerSafeErrorClassificationMatrix(t *testing.T) {
	t.Parallel()

	rawRef := "hmac-sha256:" + strings.Repeat("a", 64)
	seenAndRaw := func(extra ...[2]string) http.Header {
		h := http.Header{}
		h.Set(formalPoolHealthcheckSeenHeader, "1")
		h.Set(formalPoolHealthcheckRawRefHeader, rawRef)
		for _, kv := range extra {
			h.Set(kv[0], kv[1])
		}
		return h
	}
	cases := []struct {
		name               string
		status             int
		headers            http.Header
		body               string
		wantCode           string
		wantBucket         string
		wantNotQuarantined bool
	}{
		{name: "401 invalid grant auth", status: http.StatusUnauthorized, body: `{"error":{"message":"invalid_grant: refresh token invalid"}}`, wantCode: "invalid_grant", wantBucket: "auth"},
		{name: "403 risk hold", status: http.StatusForbidden, body: `{"error":{"message":"account is on hold due to risk"}}`, wantCode: "account_on_hold", wantBucket: "hold"},
		{name: "429 rate limited", status: http.StatusTooManyRequests, headers: seenAndRaw(), body: `{"error":{"message":"rate limit exceeded"}}`, wantCode: "rate_limited", wantBucket: "rate_limited", wantNotQuarantined: true},
		{name: "429 long context", status: http.StatusTooManyRequests, headers: seenAndRaw(), body: `{"error":{"message":"long context usage credits required"}}`, wantCode: "long_context_usage_credits", wantBucket: "long_context", wantNotQuarantined: true},
		{name: "proxy mismatch", status: http.StatusOK, headers: seenAndRaw([2]string{"X-CC-Gateway-Proxy-Mismatch", "1"}), body: `event: message_stop`, wantCode: "proxy_mismatch", wantBucket: "proxy"},
		{name: "fallback", status: http.StatusOK, headers: seenAndRaw([2]string{"X-CC-Gateway-Fallback-Detected", "true"}), body: `event: message_stop`, wantCode: "fallback", wantBucket: "fallback"},
		{name: "raw capture missing", status: http.StatusOK, headers: http.Header{formalPoolHealthcheckSeenHeader: []string{"1"}}, body: `event: message_stop`, wantCode: "raw_capture_missing", wantBucket: "raw_capture"},
		{name: "cc gateway not seen", status: http.StatusOK, headers: func() http.Header { h := http.Header{}; h.Set(formalPoolHealthcheckRawRefHeader, rawRef); return h }(), body: `event: message_stop`, wantCode: "cc_gateway_not_seen", wantBucket: "cc_gateway"},
		{name: "missing account identity", status: http.StatusUnprocessableEntity, body: `{"error":{"message":"missing_account_identity"}}`, wantCode: "missing_account_identity", wantBucket: "cc_gateway"},
		{name: "egress proxy failure", status: http.StatusBadGateway, body: `{"error":{"message":"egress_proxy_failure"}}`, wantCode: "egress_proxy_failure", wantBucket: "proxy"},
		{name: "unknown", status: http.StatusInternalServerError, headers: seenAndRaw(), body: `{"error":{"message":"temporary upstream failure"}}`, wantCode: "unknown", wantBucket: "unknown"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			account := newFormalPoolHealthcheckAccount()
			repo := &formalPoolHealthcheckRepo{formalRateLimitRepo{accountsByID: map[int64]*Account{account.ID: account}}}
			upstream := &formalPoolHealthcheckUpstream{resp: &http.Response{StatusCode: tc.status, Header: tc.headers, Body: io.NopCloser(strings.NewReader(tc.body))}}
			runner := NewFormalPoolGatewayHealthcheckRunner(repo, upstream, &config.Config{Gateway: config.GatewayConfig{CCGateway: config.GatewayCCGatewayConfig{Enabled: true, BaseURL: "http://cc-gateway:8443", Providers: config.GatewayCCGatewayProvidersConfig{Anthropic: true}}}}, nil)

			got, err := runner.RunHealthcheck(context.Background(), FormalPoolAcceptanceInput{AccountID: account.ID, AccountRef: "hmac-sha256:acct-ref", EgressBucket: "bucket-a", ProxyRef: "hmac-sha256:proxy-ref", PoolProfile: PoolProfileNormal})

			require.NoError(t, err)
			payload := map[string]any{}
			require.NoError(t, json.Unmarshal(mustJSONBytes(t, got), &payload))
			require.Equal(t, tc.wantCode, payload["safe_error_code"])
			require.Equal(t, tc.wantBucket, payload["safe_error_bucket"])
			if tc.wantNotQuarantined {
				require.Equal(t, FormalPoolStageRuntimeRegistered, repo.accountsByID[account.ID].Extra[FormalPoolExtraOnboardingStage])
				require.True(t, repo.accountsByID[account.ID].Status == "" || repo.accountsByID[account.ID].Status == StatusActive)
			}
		})
	}
}

func mustJSONBytes(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return raw
}
