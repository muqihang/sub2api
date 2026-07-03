package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
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

const (
	formalPoolHealthcheckAccountRefForTest = "hmac-sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	formalPoolHealthcheckProxyRefForTest   = "hmac-sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func newFormalPoolHealthcheckConfig() *config.Config {
	cfg := &config.Config{Gateway: config.GatewayConfig{CCGateway: config.GatewayCCGatewayConfig{Enabled: true, BaseURL: "http://cc-gateway:8443", Token: "ccg-token", ContextAttestationSecret: "formal-pool-attestation-secret-test", Providers: config.GatewayCCGatewayProvidersConfig{Anthropic: true}}}}
	return cfg
}

func newFormalPoolHealthcheckAccount() *Account {
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	account.ID = 7001
	account.Type = AccountTypeSetupToken
	account.Credentials["scope"] = "user:inference"
	account.Extra["cc_gateway_enabled"] = "true"
	account.Extra["cc_gateway_canary_only"] = "false"
	account.Extra["cc_gateway_policy_version"] = "2.1.197"
	account.Extra["cc_gateway_routes"] = "native_messages"
	account.Extra["cc_gateway_egress_bucket_enabled"] = "true"
	account.Extra["cc_gateway_egress_bucket"] = "bucket-a"
	account.Extra["cc_gateway_account_ref"] = formalPoolHealthcheckAccountRefForTest
	account.Extra[ccGatewayExtraCredentialRef] = "opaque:credential-ref:v1:healthcheck-cred"
	account.Extra[ccGatewayExtraProxyIdentityRef] = formalPoolHealthcheckProxyRefForTest
	account.Extra[ccGatewayExtraPersonaProfile] = ccGateway2197PersonaProfile
	account.Extra["claude_code_device_id"] = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageRuntimeRegistered
	account.Schedulable = false
	return account
}

func TestFormalPoolGatewayHealthcheckRunnerRequiresCCGatewayEvidence(t *testing.T) {
	t.Parallel()
	account := newFormalPoolHealthcheckAccount()
	repo := &formalPoolHealthcheckRepo{formalRateLimitRepo{accountsByID: map[int64]*Account{account.ID: account}}}
	upstream := &formalPoolHealthcheckUpstream{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))}}
	runner := NewFormalPoolGatewayHealthcheckRunner(repo, upstream, newFormalPoolHealthcheckConfig(), nil)

	got, err := runner.RunHealthcheck(context.Background(), FormalPoolAcceptanceInput{AccountID: account.ID, AccountRef: formalPoolHealthcheckAccountRefForTest, EgressBucket: "bucket-a", ProxyRef: formalPoolHealthcheckProxyRefForTest, PoolProfile: PoolProfileNormal})

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
	runner := NewFormalPoolGatewayHealthcheckRunner(repo, upstream, newFormalPoolHealthcheckConfig(), nil)

	got, err := runner.RunHealthcheck(context.Background(), FormalPoolAcceptanceInput{AccountID: account.ID, AccountRef: formalPoolHealthcheckAccountRefForTest, EgressBucket: "bucket-a", ProxyRef: formalPoolHealthcheckProxyRefForTest, PoolProfile: PoolProfileNormal})

	require.NoError(t, err)
	require.True(t, got.FormalPoolHealthcheckPassed(), "%#v", got)
	require.Equal(t, "hmac-sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", got.RawCaptureRef)
}

func TestFormalPoolGatewayHealthcheckRunnerSeedsCanonicalObservedClientProfile(t *testing.T) {
	t.Parallel()
	account := newFormalPoolHealthcheckAccount()
	repo := &formalPoolHealthcheckRepo{formalRateLimitRepo{accountsByID: map[int64]*Account{account.ID: account}}}
	upstream := &formalPoolHealthcheckUpstream{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"X-Cc-Gateway-Seen": []string{"1"}, "X-Cc-Gateway-Raw-Capture-Ref": []string{"hmac-sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}, Body: io.NopCloser(strings.NewReader("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))}}
	runner := NewFormalPoolGatewayHealthcheckRunner(repo, upstream, newFormalPoolHealthcheckConfig(), nil)

	_, err := runner.RunHealthcheck(context.Background(), FormalPoolAcceptanceInput{AccountID: account.ID, AccountRef: formalPoolHealthcheckAccountRefForTest, EgressBucket: "bucket-a", ProxyRef: formalPoolHealthcheckProxyRefForTest, PoolProfile: PoolProfileNormal})

	require.NoError(t, err)
	require.Equal(t, "claude-cli/2.1.197 (formal-pool-healthcheck)", upstream.lastHeaders.Get("User-Agent"))
	require.Equal(t, "2.1.197", getHeaderRaw(upstream.lastHeaders, ClaudeCodeNativeClaudeCodeVersionHeader))
	ctx := decodeCCGatewayFormalPoolContextForTest(t, &http.Request{Header: upstream.lastHeaders})
	observed, ok := ctx["observed_client_profile"].(map[string]any)
	require.True(t, ok, "%#v", ctx["observed_client_profile"])
	require.Equal(t, "2.1.197", observed["cli_version_bucket"])
	require.Equal(t, "messages", observed["route_class"])
	require.NotContains(t, observed, "unknown_top_level_body_key_count")
}

func TestFormalPoolGatewayHealthcheckRunnerPreservesExplicitRollback2179Tuple(t *testing.T) {
	resetClaudeCodeSessionBoundaryLedgerForTest()
	t.Setenv("SUB2API_CLAUDE_CODE_SESSION_BOUNDARY_LEDGER_FILE", filepath.Join(t.TempDir(), "rollback-session-ledger.json"))
	account := newFormalPoolHealthcheckAccount()
	account.Extra[ccGatewayExtraPolicyVersion] = "2.1.179"
	account.Extra[ccGatewayExtraPersonaProfile] = ccGatewayDefaultPersonaProfile
	account.Extra["cc_gateway_account_ref"] = "hmac-sha256:" + strings.Repeat("c", 64)
	account.Extra[ccGatewayExtraCredentialRef] = "opaque:credential-ref:v1:rollback-healthcheck-cred"
	account.Extra[ccGatewayExtraProxyIdentityRef] = "hmac-sha256:" + strings.Repeat("d", 64)
	account.Extra["cc_gateway_egress_bucket"] = "bucket-rollback"
	account.Extra["claude_code_device_id"] = strings.Repeat("e", 64)
	repo := &formalPoolHealthcheckRepo{formalRateLimitRepo{accountsByID: map[int64]*Account{account.ID: account}}}
	upstream := &formalPoolHealthcheckUpstream{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"X-Cc-Gateway-Seen": []string{"1"}, "X-Cc-Gateway-Raw-Capture-Ref": []string{"hmac-sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}, Body: io.NopCloser(strings.NewReader(`event: message_stop
data: {"type":"message_stop"}

`))}}
	runner := NewFormalPoolGatewayHealthcheckRunner(repo, upstream, newFormalPoolHealthcheckConfig(), nil)

	_, err := runner.RunHealthcheck(context.Background(), FormalPoolAcceptanceInput{AccountID: account.ID, AccountRef: account.GetExtraString("cc_gateway_account_ref"), EgressBucket: "bucket-rollback", ProxyRef: account.GetExtraString(ccGatewayExtraProxyIdentityRef), PoolProfile: PoolProfileNormal})

	require.NoError(t, err)
	require.Equal(t, "claude-cli/2.1.179 (formal-pool-healthcheck)", upstream.lastHeaders.Get("User-Agent"))
	require.Equal(t, "2.1.179", getHeaderRaw(upstream.lastHeaders, ClaudeCodeNativeClaudeCodeVersionHeader))
	ctx := decodeCCGatewayFormalPoolContextForTest(t, &http.Request{Header: upstream.lastHeaders})
	require.Equal(t, "2.1.179", ctx["policy_version"])
	require.Equal(t, ccGatewayDefaultPersonaProfile, ctx["persona_profile"])
}

func TestFormalPoolGatewayHealthcheckRunnerSendsAttestedFormalPoolContext(t *testing.T) {
	t.Parallel()
	account := newFormalPoolHealthcheckAccount()
	repo := &formalPoolHealthcheckRepo{formalRateLimitRepo{accountsByID: map[int64]*Account{account.ID: account}}}
	upstream := &formalPoolHealthcheckUpstream{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"X-Cc-Gateway-Seen": []string{"1"}, "X-Cc-Gateway-Raw-Capture-Ref": []string{"hmac-sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}, Body: io.NopCloser(strings.NewReader("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))}}
	cfg := newFormalPoolHealthcheckConfig()
	runner := NewFormalPoolGatewayHealthcheckRunner(repo, upstream, cfg, nil)

	_, err := runner.RunHealthcheck(context.Background(), FormalPoolAcceptanceInput{AccountID: account.ID, AccountRef: formalPoolHealthcheckAccountRefForTest, EgressBucket: "bucket-a", ProxyRef: formalPoolHealthcheckProxyRefForTest, PoolProfile: PoolProfileNormal})

	require.NoError(t, err)
	require.NotEmpty(t, getHeaderRaw(upstream.lastHeaders, ccGatewayFormalPoolContextHeader))
	requireValidCCGatewayFormalPoolSignatureForTest(t, &http.Request{Header: upstream.lastHeaders}, cfg.Gateway.CCGateway.ContextAttestationSecret)
	ctx := decodeCCGatewayFormalPoolContextForTest(t, &http.Request{Header: upstream.lastHeaders})
	require.Equal(t, "messages", ctx["route_class"])
	require.Equal(t, ccGateway2197PersonaProfile, ctx["persona_profile"])
	require.NotEmpty(t, ctx["session_id"])
}

func TestFormalPoolGatewayHealthcheckSessionLedgerBindsHealthcheckPersona(t *testing.T) {
	resetClaudeCodeSessionBoundaryLedgerForTest()
	t.Setenv("SUB2API_CLAUDE_CODE_SESSION_BOUNDARY_LEDGER_FILE", filepath.Join(t.TempDir(), "formal-pool-session-ledger.json"))
	const wantPersonaProfile = "claude-code-2_1_197-macos-local"
	account := newFormalPoolHealthcheckAccount()
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction
	account.Extra[ccGatewayExtraPersonaProfile] = ccGateway2197PersonaProfile
	repo := &formalPoolHealthcheckRepo{formalRateLimitRepo{accountsByID: map[int64]*Account{account.ID: account}}}
	upstream := &formalPoolHealthcheckUpstream{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"X-Cc-Gateway-Seen": []string{"1"}, "X-Cc-Gateway-Raw-Capture-Ref": []string{"hmac-sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}, Body: io.NopCloser(strings.NewReader("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))}}
	runner := NewFormalPoolGatewayHealthcheckRunner(repo, upstream, newFormalPoolHealthcheckConfig(), nil)

	_, err := runner.RunHealthcheck(context.Background(), FormalPoolAcceptanceInput{AccountID: account.ID, AccountRef: formalPoolHealthcheckAccountRefForTest, EgressBucket: "bucket-a", ProxyRef: formalPoolHealthcheckProxyRefForTest, PoolProfile: PoolProfileNormal})

	require.NoError(t, err)
	raw, err := os.ReadFile(os.Getenv("SUB2API_CLAUDE_CODE_SESSION_BOUNDARY_LEDGER_FILE"))
	require.NoError(t, err)
	require.Contains(t, string(raw), wantPersonaProfile)
	require.Contains(t, string(raw), `"policy_version": "2_1_197"`)
}

func TestFormalPoolGatewayHealthcheckUsesAccountScopedSessionForBoundaryLedger(t *testing.T) {
	resetClaudeCodeSessionBoundaryLedgerForTest()
	t.Setenv("SUB2API_CLAUDE_CODE_SESSION_BOUNDARY_LEDGER_FILE", filepath.Join(t.TempDir(), "formal-pool-session-ledger.json"))
	accountA := newFormalPoolHealthcheckAccount()
	accountA.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction
	accountB := newFormalPoolHealthcheckAccount()
	accountB.ID = 7002
	accountB.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction
	accountB.Extra["cc_gateway_account_ref"] = "hmac-sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	accountB.Extra[ccGatewayExtraCredentialRef] = "opaque:credential-ref:v1:healthcheck-cred-b"
	accountB.Extra[ccGatewayExtraProxyIdentityRef] = "hmac-sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	accountB.Extra["cc_gateway_egress_bucket"] = "bucket-b"
	accountB.Extra["claude_code_device_id"] = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	repo := &formalPoolHealthcheckRepo{formalRateLimitRepo{accountsByID: map[int64]*Account{
		accountA.ID: accountA,
		accountB.ID: accountB,
	}}}
	upstream := &formalPoolHealthcheckUpstream{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"X-Cc-Gateway-Seen": []string{"1"}, "X-Cc-Gateway-Raw-Capture-Ref": []string{"hmac-sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}, Body: io.NopCloser(strings.NewReader("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))}}
	runner := NewFormalPoolGatewayHealthcheckRunner(repo, upstream, newFormalPoolHealthcheckConfig(), nil)

	_, err := runner.RunHealthcheck(context.Background(), FormalPoolAcceptanceInput{AccountID: accountA.ID, AccountRef: formalPoolHealthcheckAccountRefForTest, EgressBucket: "bucket-a", ProxyRef: formalPoolHealthcheckProxyRefForTest, PoolProfile: PoolProfileNormal})
	require.NoError(t, err)
	_, err = runner.RunHealthcheck(context.Background(), FormalPoolAcceptanceInput{AccountID: accountB.ID, AccountRef: accountB.GetExtraString("cc_gateway_account_ref"), EgressBucket: "bucket-b", ProxyRef: accountB.GetExtraString(ccGatewayExtraProxyIdentityRef), PoolProfile: PoolProfileNormal})
	require.NoError(t, err)
}

func TestFormalPoolGatewayHealthcheckRotatedCredentialGetsFreshBoundarySession(t *testing.T) {
	resetClaudeCodeSessionBoundaryLedgerForTest()
	t.Setenv("SUB2API_CLAUDE_CODE_SESSION_BOUNDARY_LEDGER_FILE", filepath.Join(t.TempDir(), "formal-pool-session-ledger.json"))
	account := newFormalPoolHealthcheckAccount()
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction
	repo := &formalPoolHealthcheckRepo{formalRateLimitRepo{accountsByID: map[int64]*Account{account.ID: account}}}
	upstream := &formalPoolHealthcheckUpstream{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"X-Cc-Gateway-Seen": []string{"1"}, "X-Cc-Gateway-Raw-Capture-Ref": []string{"hmac-sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}, Body: io.NopCloser(strings.NewReader("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))}}
	runner := NewFormalPoolGatewayHealthcheckRunner(repo, upstream, newFormalPoolHealthcheckConfig(), nil)

	_, err := runner.RunHealthcheck(context.Background(), FormalPoolAcceptanceInput{AccountID: account.ID, AccountRef: formalPoolHealthcheckAccountRefForTest, EgressBucket: "bucket-a", ProxyRef: formalPoolHealthcheckProxyRefForTest, PoolProfile: PoolProfileNormal})
	require.NoError(t, err)
	firstBody := append([]byte(nil), upstream.lastBody...)

	account.Extra[ccGatewayExtraCredentialRef] = "opaque:credential-ref:v1:healthcheck-cred-rotated"
	upstream.resp = &http.Response{StatusCode: http.StatusOK, Header: http.Header{"X-Cc-Gateway-Seen": []string{"1"}, "X-Cc-Gateway-Raw-Capture-Ref": []string{"hmac-sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}, Body: io.NopCloser(strings.NewReader("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))}
	_, err = runner.RunHealthcheck(context.Background(), FormalPoolAcceptanceInput{AccountID: account.ID, AccountRef: formalPoolHealthcheckAccountRefForTest, EgressBucket: "bucket-a", ProxyRef: formalPoolHealthcheckProxyRefForTest, PoolProfile: PoolProfileNormal})
	require.NoError(t, err)
	require.Equal(t, 2, upstream.requests)
	require.NotEqual(t, formalPoolHealthcheckSessionFromBodyForTest(t, firstBody), formalPoolHealthcheckSessionFromBodyForTest(t, upstream.lastBody))
}

func TestFormalPoolGatewayHealthcheckRunnerUsesClaudeCodeLiteBodyWithoutOneMillionContext(t *testing.T) {
	t.Parallel()
	const wantPersonaProfile = "claude-code-2.1.197-macos-local"
	account := newFormalPoolHealthcheckAccount()
	repo := &formalPoolHealthcheckRepo{formalRateLimitRepo{accountsByID: map[int64]*Account{account.ID: account}}}
	upstream := &formalPoolHealthcheckUpstream{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"X-Cc-Gateway-Seen": []string{"1"}, "X-Cc-Gateway-Raw-Capture-Ref": []string{"hmac-sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}, Body: io.NopCloser(strings.NewReader("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))}}
	runner := NewFormalPoolGatewayHealthcheckRunner(repo, upstream, newFormalPoolHealthcheckConfig(), nil)

	_, err := runner.RunHealthcheck(context.Background(), FormalPoolAcceptanceInput{AccountID: account.ID, AccountRef: formalPoolHealthcheckAccountRefForTest, EgressBucket: "bucket-a", ProxyRef: formalPoolHealthcheckProxyRefForTest, PoolProfile: PoolProfileNormal})

	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(upstream.lastBody, &body))
	require.Equal(t, formalPoolHealthcheckModel, body["model"])
	require.Equal(t, "claude-haiku-4-5-20251001", body["model"])
	require.EqualValues(t, 64, body["max_tokens"])
	require.Equal(t, false, body["stream"])
	require.Contains(t, body, "metadata")
	require.Contains(t, body, "system")
	require.Contains(t, body, "tools")
	require.NotContains(t, body, "thinking")
	require.NotContains(t, body, "output_config")
	require.NotContains(t, body, "context_management")
	system, ok := body["system"].([]any)
	require.True(t, ok)
	require.Len(t, system, 2)
	require.Contains(t, system[0].(map[string]any)["text"], "<env>")
	require.Contains(t, system[1].(map[string]any)["text"], "Claude Code")
	require.Contains(t, system[1].(map[string]any)["text"], "lowest-cost")
	tools, ok := body["tools"].([]any)
	require.True(t, ok)
	require.Empty(t, tools)
	require.NotContains(t, strings.ToLower(upstream.lastHeaders.Get("anthropic-beta")), "context-1m")
	require.Equal(t, wantPersonaProfile, getHeaderRaw(upstream.lastHeaders, ccGatewayHealthcheckPersonaHeader))
}

func TestFormalPoolGatewayHealthcheckBodyUsesUUIDSessionID(t *testing.T) {
	t.Parallel()
	body, err := formalPoolHealthcheckBody()
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(body, &parsed))
	metadata, ok := parsed["metadata"].(map[string]any)
	require.True(t, ok)
	userID, ok := metadata["user_id"].(string)
	require.True(t, ok)
	var user map[string]string
	require.NoError(t, json.Unmarshal([]byte(userID), &user))
	require.Regexp(t, `^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`, user["session_id"])
	require.NotEqual(t, "formal-pool-healthcheck", user["session_id"])
}

func formalPoolHealthcheckSessionFromBodyForTest(t *testing.T, body []byte) string {
	t.Helper()
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(body, &parsed))
	metadata, ok := parsed["metadata"].(map[string]any)
	require.True(t, ok)
	userID, ok := metadata["user_id"].(string)
	require.True(t, ok)
	var user map[string]string
	require.NoError(t, json.Unmarshal([]byte(userID), &user))
	return user["session_id"]
}

func TestFormalPoolGatewayHealthcheckRunnerQuarantinesAuthFailure(t *testing.T) {
	t.Parallel()
	account := newFormalPoolHealthcheckAccount()
	repo := &formalPoolHealthcheckRepo{formalRateLimitRepo{accountsByID: map[int64]*Account{account.ID: account}}}
	upstream := &formalPoolHealthcheckUpstream{resp: &http.Response{StatusCode: http.StatusUnauthorized, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"invalid credentials"}}`))}}
	runner := NewFormalPoolGatewayHealthcheckRunner(repo, upstream, newFormalPoolHealthcheckConfig(), nil)

	got, err := runner.RunHealthcheck(context.Background(), FormalPoolAcceptanceInput{AccountID: account.ID, AccountRef: formalPoolHealthcheckAccountRefForTest, EgressBucket: "bucket-a", ProxyRef: formalPoolHealthcheckProxyRefForTest, PoolProfile: PoolProfileNormal})

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
			runner := NewFormalPoolGatewayHealthcheckRunner(repo, upstream, newFormalPoolHealthcheckConfig(), nil)

			got, err := runner.RunHealthcheck(context.Background(), FormalPoolAcceptanceInput{AccountID: account.ID, AccountRef: formalPoolHealthcheckAccountRefForTest, EgressBucket: "bucket-a", ProxyRef: formalPoolHealthcheckProxyRefForTest, PoolProfile: PoolProfileNormal})

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
