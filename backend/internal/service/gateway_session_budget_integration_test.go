package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

func TestGatewaySessionBudgetObserveOnly_RecordsUtilizationWithoutMutatingStrictBody(t *testing.T) {
	reset := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	resp := newAnthropicSuccessResponse()
	resp.Header.Set("anthropic-ratelimit-unified-5h-status", "allowed")
	resp.Header.Set("anthropic-ratelimit-unified-5h-utilization", "42%")
	resp.Header.Set("anthropic-ratelimit-unified-5h-reset", reset)
	resp.Header.Set("anthropic-ratelimit-unified-7d-status", "allowed")
	resp.Header.Set("anthropic-ratelimit-unified-7d-utilization", "91%")
	resp.Header.Set("anthropic-ratelimit-unified-7d-reset", reset)
	resp.Header.Set("anthropic-ratelimit-unified-overage-status", "disabled")
	upstream := &anthropicHTTPUpstreamRecorder{resp: resp}
	sink := &recordingBudgetLedgerSink{}
	svc := newGatewayBudgetIntegrationService(upstream, sink)
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	account.Extra["pool_profile"] = PoolProfileNormal
	c, ctx := newAnthropicForwardTestContext("/v1/messages", true)
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.145 (external, sdk-cli)")
	body := []byte(`{"model":"claude-opus-4-6-thinking","max_tokens":32000,"stream":false,"thinking":{"type":"enabled","budget_tokens":32000},"context_management":{"edits":[{"type":"clear_tool_uses_20250919"}]},"messages":[{"role":"user","content":[{"type":"text","text":"raw prompt must not enter ledger"}]}],"tools":[{"name":"Read","input_schema":{"type":"object"}}]}`)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))
	require.NoError(t, err)
	require.True(t, bytes.Equal(body, upstream.lastBody), "observe-only budget must not mutate strict passthrough body")
	require.Len(t, sink.sessions, 1)
	require.Len(t, sink.accounts, 1)
	require.Len(t, sink.decisions, 1)
	require.Equal(t, "pct_40_50", sink.accounts[0].Utilization5hPercentageBucket)
	require.Equal(t, "pct_90_95", sink.accounts[0].Utilization7dPercentageBucket)
	require.Equal(t, BudgetActionObserve, sink.decisions[0].Action)
	require.NoError(t, ValidateNoRawSensitiveLedger(sink.sessions[0]))
	require.NoError(t, ValidateNoRawSensitiveLedger(sink.accounts[0]))
	require.NoError(t, ValidateNoRawSensitiveLedger(sink.decisions[0]))
	assertBudgetSinkDoesNotContain(t, sink, "raw prompt must not enter ledger", "oauth-token", "Authorization")
}

func TestGatewaySessionBudgetObserveOnly_MissingAndMalformedHeadersDoNotHardBlock(t *testing.T) {
	resp := newAnthropicSuccessResponse()
	resp.Header.Set("anthropic-ratelimit-unified-5h-status", "allowed")
	resp.Header.Set("anthropic-ratelimit-unified-5h-utilization", "not-a-number")
	upstream := &anthropicHTTPUpstreamRecorder{resp: resp}
	sink := &recordingBudgetLedgerSink{}
	svc := newGatewayBudgetIntegrationService(upstream, sink)
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	account.Extra["pool_profile"] = PoolProfileAggressive
	c, ctx := newAnthropicForwardTestContext("/v1/messages", true)
	body := []byte(`{"model":"claude-sonnet-4-6","max_tokens":32000,"stream":false,"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))
	require.NoError(t, err)
	require.Len(t, sink.decisions, 1)
	require.Equal(t, BudgetActionObserve, sink.decisions[0].Action)
	require.Equal(t, float64(1), sink.decisions[0].AccountWeight)
	require.Equal(t, "observe_conservative_missing_utilization", sink.decisions[0].ReasonCode)
}

func TestGatewaySessionBudgetObserveOnly_429CooldownRecommendationAnd403RiskEvent(t *testing.T) {
	for _, tc := range []struct {
		name         string
		status       int
		body         string
		wantDecision string
		wantRisk     bool
	}{
		{name: "429", status: http.StatusTooManyRequests, body: `{"error":{"message":"rate limited"}}`, wantDecision: BudgetActionCooldown},
		{name: "403 risk", status: http.StatusForbidden, body: `{"error":{"message":"unusual activity detected"}}`, wantDecision: BudgetActionQuarantine, wantRisk: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &freshBodyBudgetUpstream{status: tc.status, body: tc.body}
			sink := &recordingBudgetLedgerSink{}
			svc := newGatewayBudgetIntegrationService(upstream, sink)
			account := newAnthropicOAuthAccountForClaudeForwardTest()
			c, ctx := newAnthropicForwardTestContext("/v1/messages", true)
			body := []byte(`{"model":"claude-sonnet-4-6","stream":false,"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

			_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))
			require.Error(t, err)
			require.NotEmpty(t, sink.decisions)
			require.Equal(t, tc.wantDecision, sink.decisions[len(sink.decisions)-1].Action)
			if tc.wantRisk {
				require.NotEmpty(t, sink.risks)
				require.Equal(t, RiskEventKindRiskText, sink.risks[0].Kind)
				require.NoError(t, ValidateNoRawSensitiveLedger(sink.risks[0]))
				assertBudgetSinkDoesNotContain(t, sink, "unusual activity detected")
			}
		})
	}
}

func TestGatewaySessionBudgetObserveOnly_RecordsNonFailoverErrorResponse(t *testing.T) {
	upstream := &freshBodyBudgetUpstream{status: http.StatusBadRequest, body: `{"error":{"message":"invalid request"}}`}
	sink := &recordingBudgetLedgerSink{}
	svc := newGatewayBudgetIntegrationService(upstream, sink)
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	c, ctx := newAnthropicForwardTestContext("/v1/messages", true)
	body := []byte(`{"model":"claude-sonnet-4-6","stream":false,"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))
	require.Error(t, err)
	require.NotEmpty(t, sink.sessions)
	require.Equal(t, "status_4xx", sink.sessions[len(sink.sessions)-1].StatusBucket)
	require.NotEmpty(t, sink.decisions)
}

func TestGatewaySessionBudgetObserveOnly_RecordsCCGatewayControlPlaneError(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusUnprocessableEntity,
		Header: http.Header{
			"Content-Type":           []string{"application/json"},
			ccGatewayErrorKindHeader: []string{"control-plane"},
			ccGatewayErrorCodeHeader: []string{"control_plane_rejected"},
		},
		Body: io.NopCloser(strings.NewReader(`{"error":{"message":"control plane rejected"}}`)),
	}
	upstream := &ccGatewayBoundaryUpstreamRecorder{resp: resp}
	sink := &recordingBudgetLedgerSink{}
	seedGatewayForwardingSettingsForTest()
	svc := &GatewayService{
		cfg:                  ccGatewayTestConfig(PlatformAnthropic),
		identityService:      NewIdentityService(&identityCacheStub{}),
		httpUpstream:         upstream,
		sessionBudgetObserve: sink,
	}
	account := newCCGatewayBoundaryAccount()
	c, ctx := newCCGatewayBoundaryContext("/v1/messages")
	body := []byte(`{"model":"claude-sonnet-4-6","stream":false,"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))
	require.Error(t, err)
	require.NotEmpty(t, sink.sessions)
	require.Equal(t, "status_4xx", sink.sessions[len(sink.sessions)-1].StatusBucket)
	require.NotEmpty(t, sink.decisions)
}

type freshBodyBudgetUpstream struct {
	status   int
	body     string
	lastBody []byte
}

func (u *freshBodyBudgetUpstream) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	if req != nil && req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		u.lastBody = b
		_ = req.Body.Close()
		req.Body = io.NopCloser(bytes.NewReader(b))
	}
	return &http.Response{StatusCode: u.status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(u.body))}, nil
}

func (u *freshBodyBudgetUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func TestGatewaySessionBudgetObserveOnly_CCGatewayAccountRefCorrelates(t *testing.T) {
	resp := newAnthropicSuccessResponse()
	resp.Header.Set("anthropic-ratelimit-unified-5h-status", "allowed")
	resp.Header.Set("anthropic-ratelimit-unified-5h-utilization", "42%")
	resp.Header.Set("anthropic-ratelimit-unified-7d-status", "allowed")
	resp.Header.Set("anthropic-ratelimit-unified-7d-utilization", "91%")
	upstream := &anthropicHTTPUpstreamRecorder{resp: resp}
	sink := &recordingBudgetLedgerSink{}
	svc := newGatewayBudgetIntegrationService(upstream, sink)
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	account.Extra[ccGatewayExtraAccountRef] = "opaque:acct:v1:safe-ref"
	c, ctx := newAnthropicForwardTestContext("/v1/messages", true)
	body := []byte(`{"model":"claude-sonnet-4-6","stream":false,"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))
	require.NoError(t, err)
	require.Len(t, sink.sessions, 1)
	require.Len(t, sink.accounts, 1)
	require.Equal(t, sink.accounts[0].AccountRef, sink.sessions[0].AccountRef)
}

func TestGatewaySessionBudgetObserveOnly_NewGatewayServiceWiresDefaultSink(t *testing.T) {
	svc := NewGatewayService(nil, nil, nil, nil, nil, nil, nil, nil, &config.Config{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	require.NotNil(t, svc.sessionBudgetObserve, "real GatewayService construction should wire observe-only sink")
}

func TestGatewaySessionBudgetObserveOnly_NewGatewayServiceUsesExportPathWhenConfigured(t *testing.T) {
	t.Setenv("SUB2API_SESSION_BUDGET_EXPORT_PATH", filepath.Join(t.TempDir(), "budget.jsonl"))
	svc := NewGatewayService(nil, nil, nil, nil, nil, nil, nil, nil, &config.Config{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, ok := svc.sessionBudgetObserve.(*FileSessionBudgetObserveSink)
	require.True(t, ok, "staging must be able to export redacted session budget ledger")
}

func TestGatewaySessionBudgetObserveOnly_ResponseStatusAndRefsAreCorrelated(t *testing.T) {
	resp := newAnthropicSuccessResponse()
	resp.Header.Set("anthropic-ratelimit-unified-5h-status", "allowed")
	resp.Header.Set("anthropic-ratelimit-unified-5h-utilization", "42%")
	resp.Header.Set("anthropic-ratelimit-unified-7d-status", "allowed")
	resp.Header.Set("anthropic-ratelimit-unified-7d-utilization", "91%")
	upstream := &anthropicHTTPUpstreamRecorder{resp: resp}
	sink := &recordingBudgetLedgerSink{}
	svc := newGatewayBudgetIntegrationService(upstream, sink)
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	c, ctx := newAnthropicForwardTestContext("/v1/messages", true)
	body := []byte(`{"model":"claude-sonnet-4-6","stream":false,"metadata":{"user_id":"user_scope_1"},"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))
	require.NoError(t, err)
	require.Len(t, sink.sessions, 1)
	require.Len(t, sink.accounts, 1)
	require.Len(t, sink.users, 1)
	require.Equal(t, "status_2xx", sink.sessions[0].StatusBucket)
	require.Equal(t, sink.sessions[0].UserRef, sink.users[0].UserRef, "user ledger ref must correlate with session user_ref")
	require.Equal(t, sink.sessions[0].AccountRef, sink.accounts[0].AccountRef, "account ledger ref must correlate with session account_ref")
}

func TestGatewaySessionBudgetObserveOnly_RiskRefsCorrelateWithSessionAndAccount(t *testing.T) {
	upstream := &freshBodyBudgetUpstream{status: http.StatusForbidden, body: `{"error":{"message":"unusual activity detected"}}`}
	sink := &recordingBudgetLedgerSink{}
	svc := newGatewayBudgetIntegrationService(upstream, sink)
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	c, ctx := newAnthropicForwardTestContext("/v1/messages", true)
	body := []byte(`{"model":"claude-sonnet-4-6","stream":false,"metadata":{"user_id":"user_scope_1"},"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))
	require.Error(t, err)
	require.NotEmpty(t, sink.sessions)
	require.NotEmpty(t, sink.accounts)
	require.NotEmpty(t, sink.risks)
	require.Equal(t, sink.sessions[0].SessionRef, sink.risks[0].SessionRef)
	require.Equal(t, sink.sessions[0].UserRef, sink.risks[0].UserRef)
	require.Equal(t, sink.accounts[0].AccountRef, sink.risks[0].AccountRef)
}

func newGatewayBudgetIntegrationService(upstream HTTPUpstream, sink SessionBudgetObserveSink) *GatewayService {
	seedGatewayForwardingSettingsForTest()
	return &GatewayService{
		cfg:                  &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}},
		identityService:      NewIdentityService(&identityCacheStub{}),
		httpUpstream:         upstream,
		sessionBudgetObserve: sink,
	}
}

type recordingBudgetLedgerSink struct {
	sessions  []SessionBudgetLedgerEntry
	accounts  []AccountBudgetLedgerEntry
	users     []UserBudgetLedgerEntry
	pools     []PoolUtilizationBudgetLedger
	risks     []RiskEventLedgerEntry
	decisions []BudgetDecision
}

func (s *recordingBudgetLedgerSink) ObserveSessionBudget(_ context.Context, record SessionBudgetObserveRecord) {
	s.sessions = append(s.sessions, record.Session)
	if record.Account.AccountRef != "" {
		s.accounts = append(s.accounts, record.Account)
	}
	if record.User.UserRef != "" {
		s.users = append(s.users, record.User)
	}
	if record.Pool.Profile != "" {
		s.pools = append(s.pools, record.Pool)
	}
	s.risks = append(s.risks, record.RiskEvents...)
	s.decisions = append(s.decisions, record.Decision)
}

func assertBudgetSinkDoesNotContain(t *testing.T, sink *recordingBudgetLedgerSink, forbidden ...string) {
	t.Helper()
	blob := strings.Builder{}
	for _, s := range sink.sessions {
		blob.WriteString(s.ModelFamily)
		blob.WriteString(s.BodySizeBucket)
		blob.WriteString(strings.Join(s.RiskFlags, ","))
	}
	for _, a := range sink.accounts {
		blob.WriteString(a.AccountRef)
		blob.WriteString(a.ProxyRef)
		blob.WriteString(a.LastRiskEventSummary)
	}
	for _, r := range sink.risks {
		blob.WriteString(r.SafeReason)
		blob.WriteString(r.SessionRef)
		blob.WriteString(r.AccountRef)
	}
	text := blob.String()
	for _, f := range forbidden {
		if strings.Contains(text, f) {
			t.Fatalf("budget sink contains forbidden redacted token length=%d", len(f))
		}
	}
}
