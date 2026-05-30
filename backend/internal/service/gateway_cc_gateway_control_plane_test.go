package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newCCGatewayControlPlaneResponse(status int, code string) *http.Response {
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	header.Set("X-CC-Gateway-Error-Kind", "control-plane")
	header.Set("X-CC-Gateway-Error-Code", code)
	header.Set("x-request-id", "ccg-req-1")
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       ccGatewayIOReadCloser(`{"error":{"type":"cc_gateway_control_plane","code":"` + code + `","message":"local control-plane reject"}}`),
	}
}

func ccGatewayIOReadCloser(s string) io.ReadCloser {
	return io.NopCloser(bytes.NewBufferString(s))
}

func TestCCGatewayControlPlane_ForwardFailsClosedWithoutFailover(t *testing.T) {
	upstream := &ccGatewayBoundaryUpstreamRecorder{resp: newCCGatewayControlPlaneResponse(http.StatusForbidden, "missing_identity")}
	svc := newCCGatewayBoundaryService(upstream)
	account := newCCGatewayBoundaryAccount()
	c, ctx := newCCGatewayBoundaryContext("/v1/messages")
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","stream":false,"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr), "CC Gateway control-plane errors must fail closed without account failover")
}

func TestCCGatewayControlPlane_ForwardCountTokensFailsClosedWithoutHealthSideEffects(t *testing.T) {
	upstream := &ccGatewayBoundaryUpstreamRecorder{resp: newCCGatewayControlPlaneResponse(http.StatusForbidden, "missing_egress_bucket")}
	svc := newCCGatewayBoundaryService(upstream)
	account := newCCGatewayBoundaryAccount()
	c, ctx := newCCGatewayBoundaryContext("/v1/messages/count_tokens")
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	err := svc.ForwardCountTokens(ctx, c, account, parseAnthropicRequestForTest(t, body))
	require.Error(t, err)
	require.Contains(t, err.Error(), "cc gateway control-plane error")
}

func TestCCGatewayControlPlane_ForwardAsChatCompletionsFailsClosedWithoutFailover(t *testing.T) {
	upstream := &ccGatewayBoundaryUpstreamRecorder{resp: newCCGatewayControlPlaneResponse(http.StatusForbidden, "route_reject")}
	svc := newCCGatewayBoundaryService(upstream)
	account := newCCGatewayBoundaryAccount()
	c, ctx := newCCGatewayBoundaryContext("/v1/chat/completions")
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","messages":[{"role":"user","content":"hello"}],"stream":false}`)

	var ccReq apicompat.ChatCompletionsRequest
	require.NoError(t, json.Unmarshal(body, &ccReq))
	parsed := &ParsedRequest{Body: body, Model: ccReq.Model, Stream: false}

	_, err := svc.ForwardAsChatCompletions(ctx, c, account, body, parsed)
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr), "CC Gateway control-plane errors must fail closed without account failover")
}

func TestCCGatewayControlPlane_AnthropicAPIKeyPassthroughFailsClosedWithoutTLSProfile(t *testing.T) {
	upstream := &ccGatewayBoundaryUpstreamRecorder{resp: newCCGatewayControlPlaneResponse(http.StatusForbidden, "missing_identity")}
	svc := newCCGatewayBoundaryService(upstream)
	account := newAnthropicAPIKeyAccountForTest()
	account.Extra["cc_gateway_enabled"] = "true"
	account.Extra["cc_gateway_canary_only"] = "false"
	account.Extra["cc_gateway_policy_version"] = ccGatewayAnthropicPolicyVersion
	account.Extra["cc_gateway_routes"] = "native_messages,native_count_tokens"
	account.Extra["cc_gateway_egress_bucket_enabled"] = "true"
	account.Extra["cc_gateway_egress_bucket"] = "bucket-a"
	account.ProxyID = int64Ptr(901)
	account.Proxy = &Proxy{ID: 901, Name: "proxy-a", Protocol: "http", Host: "127.0.0.1", Port: 8899, Status: StatusActive}
	c, ctx := newCCGatewayBoundaryContext("/v1/messages")

	_, err := svc.forwardAnthropicAPIKeyPassthroughWithInput(ctx, c, account, anthropicPassthroughForwardInput{
		Body:          []byte(`{"model":"claude-3-7-sonnet-20250219","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`),
		RequestModel:  "claude-3-7-sonnet-20250219",
		OriginalModel: "claude-3-7-sonnet-20250219",
		RequestStream: false,
	})
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr), "API-key passthrough control-plane errors must fail closed without account failover")
	require.Nil(t, upstream.lastProfile, "API-key passthrough CC Gateway path must not use account TLS profile")
}

func TestCCGatewayControlPlane_AnthropicAPIKeyCountTokensFailsClosedWithoutTLSProfile(t *testing.T) {
	upstream := &ccGatewayBoundaryUpstreamRecorder{resp: newCCGatewayControlPlaneResponse(http.StatusForbidden, "missing_egress_bucket")}
	svc := newCCGatewayBoundaryService(upstream)
	account := newAnthropicAPIKeyAccountForTest()
	account.Extra["cc_gateway_enabled"] = "true"
	account.Extra["cc_gateway_canary_only"] = "false"
	account.Extra["cc_gateway_policy_version"] = ccGatewayAnthropicPolicyVersion
	account.Extra["cc_gateway_routes"] = "native_messages,native_count_tokens"
	account.Extra["cc_gateway_egress_bucket_enabled"] = "true"
	account.Extra["cc_gateway_egress_bucket"] = "bucket-a"
	account.ProxyID = int64Ptr(902)
	account.Proxy = &Proxy{ID: 902, Name: "proxy-b", Protocol: "http", Host: "127.0.0.1", Port: 8899, Status: StatusActive}
	c, ctx := newCCGatewayBoundaryContext("/v1/messages/count_tokens")

	err := svc.forwardCountTokensAnthropicAPIKeyPassthrough(ctx, c, account, []byte(`{"model":"claude-3-7-sonnet-20250219","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`))
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr), "API-key passthrough count_tokens control-plane errors must fail closed without account failover")
	require.Nil(t, upstream.lastProfile, "API-key passthrough count_tokens CC Gateway path must not use account TLS profile")
}

func TestCCGatewayControlPlane_FormalPoolUntrustedModelDoesNotQuarantine(t *testing.T) {
	upstream := &ccGatewayBoundaryUpstreamRecorder{resp: newCCGatewayControlPlaneResponse(http.StatusUnprocessableEntity, "persona_reject_untrusted_model")}
	svc := newCCGatewayBoundaryService(upstream)
	account := newCCGatewayBoundaryAccount()
	formalPoolApplyCompleteSchedulingEvidenceForTest(account)
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction
	account.Extra[FormalPoolExtraPoolProfileEffective] = PoolProfileNormal
	repo := &formalRateLimitRepo{accountsByID: map[int64]*Account{account.ID: account}}
	sink := &recordingBudgetLedgerSink{}
	svc.accountRepo = repo
	svc.sessionBudgetObserve = sink
	c, ctx := newCCGatewayBoundaryContext("/v1/messages")
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","stream":false,"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))

	require.Error(t, err)
	require.Contains(t, err.Error(), "cc gateway control-plane error")
	require.Equal(t, FormalPoolStageProduction, repo.accountsByID[account.ID].Extra[FormalPoolExtraOnboardingStage])
	require.True(t, repo.accountsByID[account.ID].Schedulable)
	require.NotEqual(t, StatusError, repo.accountsByID[account.ID].Status)
	require.Empty(t, repo.accountsByID[account.ID].Extra[FormalPoolExtraRiskEventRef])
	require.NotEmpty(t, sink.risks)
	require.Equal(t, RiskEventKindControlPlaneModelPolicy, sink.risks[len(sink.risks)-1].Kind)
	require.Equal(t, RiskSeverityP2, sink.risks[len(sink.risks)-1].Severity)
	require.Equal(t, BudgetActionObserve, sink.risks[len(sink.risks)-1].ActionRecommendation)
}

func TestCCGatewayControlPlaneQuarantineClassifier(t *testing.T) {
	tests := []struct {
		name    string
		code    string
		message string
		status  int
		want    bool
	}{
		{name: "persona untrusted model", code: "persona_reject_untrusted_model", status: http.StatusUnprocessableEntity, want: false},
		{name: "untrusted model", code: "reject_untrusted_model", status: http.StatusForbidden, want: false},
		{name: "cost envelope model blocked", code: "canary_cost_envelope_model_blocked", status: http.StatusUnprocessableEntity, want: false},
		{name: "request body too large", code: "body_too_large", message: "Shared-pool request body exceeds configured cap", status: http.StatusRequestEntityTooLarge, want: false},
		{name: "body too large with identity signal quarantines", code: "body_too_large", message: "missing_account_identity", status: http.StatusRequestEntityTooLarge, want: true},
		{name: "candidate model blocked", code: "candidate_model_opus_blocked", status: http.StatusUnprocessableEntity, want: false},
		{name: "candidate model rejected", code: "candidate_model_opus_rejected", status: http.StatusUnprocessableEntity, want: false},
		{name: "candidate model not enabled message", code: "route_reject", message: "candidate model is not enabled", status: http.StatusUnprocessableEntity, want: false},
		{name: "candidate model with proxy mismatch still quarantines", code: "route_reject", message: "candidate model proxy mismatch", status: http.StatusForbidden, want: true},
		{name: "persona untrusted model with missing identity quarantines", code: "persona_reject_untrusted_model", message: "missing_account_identity", status: http.StatusUnprocessableEntity, want: true},
		{name: "untrusted model with proxy mismatch quarantines", code: "reject_untrusted_model", message: "proxy_mismatch", status: http.StatusForbidden, want: true},
		{name: "candidate model blocked with fallback quarantines", code: "candidate_model_opus_blocked", message: "fallback detected", status: http.StatusUnprocessableEntity, want: true},
		{name: "candidate model blocked with verifier quarantines", code: "candidate_model_opus_blocked", message: "verifier failed", status: http.StatusUnprocessableEntity, want: true},
		{name: "candidate model blocked with risk quarantines", code: "candidate_model_opus_blocked", message: "risk text detected", status: http.StatusUnprocessableEntity, want: true},
		{name: "forbidden with model not trusted quarantines", code: "forbidden", message: "model not trusted", status: http.StatusForbidden, want: true},
		{name: "untrusted billing input is request-level block", code: "signing_untrusted_billing_input", status: http.StatusForbidden, want: false},
		{name: "missing account identity", code: "missing_account_identity", status: http.StatusForbidden, want: true},
		{name: "missing identity", code: "missing_identity", status: http.StatusForbidden, want: true},
		{name: "missing egress", code: "missing_egress", status: http.StatusForbidden, want: true},
		{name: "egress proxy failure", code: "egress_proxy_failure", status: http.StatusBadGateway, want: true},
		{name: "proxy mismatch", code: "proxy_mismatch", status: http.StatusForbidden, want: true},
		{name: "fallback message", code: "route_reject", message: "fallback detected", status: http.StatusForbidden, want: true},
		{name: "verifier message", code: "route_reject", message: "verifier failed", status: http.StatusForbidden, want: true},
		{name: "sign strip message", code: "route_reject", message: "sign_strip fallback", status: http.StatusForbidden, want: true},
		{name: "invalid auth", code: "invalid_auth", status: http.StatusUnauthorized, want: true},
		{name: "forbidden", code: "forbidden", status: http.StatusForbidden, want: true},
		{name: "risk text", code: "route_reject", message: "risk text detected", status: http.StatusForbidden, want: true},
		{name: "unknown defaults quarantine", code: "new_control_plane_reject", status: http.StatusUnprocessableEntity, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, shouldQuarantineCCGatewayControlPlane(tt.code, tt.message, tt.status))
		})
	}
}

func TestCCGatewayControlPlane_EstablishedTransientProxyFailureDoesNotQuarantine(t *testing.T) {
	account := newCCGatewayBoundaryAccount()
	formalPoolApplyCompleteSchedulingEvidenceForTest(account)
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageWarming

	require.False(t, shouldQuarantineCCGatewayControlPlaneForAccount(account, "egress_proxy_failure", "", http.StatusBadGateway))
}

func TestCCGatewayControlPlane_OnboardingProxyFailureStillQuarantines(t *testing.T) {
	account := newCCGatewayBoundaryAccount()
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageRuntimeRegistered

	require.True(t, shouldQuarantineCCGatewayControlPlaneForAccount(account, "egress_proxy_failure", "", http.StatusBadGateway))
}

func TestCCGatewayControlPlane_FormalPoolMissingIdentityQuarantines(t *testing.T) {
	upstream := &ccGatewayBoundaryUpstreamRecorder{resp: newCCGatewayControlPlaneResponse(http.StatusForbidden, "missing_account_identity")}
	svc := newCCGatewayBoundaryService(upstream)
	account := newCCGatewayBoundaryAccount()
	formalPoolApplyCompleteSchedulingEvidenceForTest(account)
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction
	account.Extra[FormalPoolExtraPoolProfileEffective] = PoolProfileNormal
	repo := &formalRateLimitRepo{accountsByID: map[int64]*Account{account.ID: account}}
	sink := &recordingBudgetLedgerSink{}
	svc.accountRepo = repo
	svc.sessionBudgetObserve = sink
	c, ctx := newCCGatewayBoundaryContext("/v1/messages")
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","stream":false,"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))

	require.Error(t, err)
	require.Equal(t, FormalPoolStageQuarantined, repo.accountsByID[account.ID].Extra[FormalPoolExtraOnboardingStage])
	require.False(t, repo.accountsByID[account.ID].Schedulable)
	require.Equal(t, StatusError, repo.accountsByID[account.ID].Status)
	require.NotEmpty(t, repo.accountsByID[account.ID].Extra[FormalPoolExtraRiskEventRef])
	require.NotEmpty(t, sink.risks)
}

func TestCCGatewayControlPlane_FormalPoolUntrustedModelResponsesAndCountTokensDoNotQuarantine(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		run  func(*GatewayService, context.Context, *gin.Context, *Account) error
	}{
		{
			name: "chat completions",
			path: "/v1/chat/completions",
			run: func(svc *GatewayService, ctx context.Context, c *gin.Context, account *Account) error {
				body := []byte(`{"model":"claude-3-7-sonnet-20250219","messages":[{"role":"user","content":"hello"}],"stream":false}`)
				var ccReq apicompat.ChatCompletionsRequest
				require.NoError(t, json.Unmarshal(body, &ccReq))
				parsed := &ParsedRequest{Body: body, Model: ccReq.Model, Stream: false}
				_, err := svc.ForwardAsChatCompletions(ctx, c, account, body, parsed)
				return err
			},
		},
		{
			name: "responses",
			path: "/v1/responses",
			run: func(svc *GatewayService, ctx context.Context, c *gin.Context, account *Account) error {
				body := []byte(`{"model":"claude-3-7-sonnet-20250219","input":"hello","stream":false}`)
				parsed := &ParsedRequest{Body: body, Model: "claude-3-7-sonnet-20250219", Stream: false}
				_, err := svc.ForwardAsResponses(ctx, c, account, body, parsed)
				return err
			},
		},
		{
			name: "count tokens",
			path: "/v1/messages/count_tokens",
			run: func(svc *GatewayService, ctx context.Context, c *gin.Context, account *Account) error {
				body := []byte(`{"model":"claude-3-7-sonnet-20250219","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)
				return svc.ForwardCountTokens(ctx, c, account, parseAnthropicRequestForTest(t, body))
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &ccGatewayBoundaryUpstreamRecorder{resp: newCCGatewayControlPlaneResponse(http.StatusUnprocessableEntity, "persona_reject_untrusted_model")}
			svc := newCCGatewayBoundaryService(upstream)
			account := newCCGatewayBoundaryAccount()
			formalPoolApplyCompleteSchedulingEvidenceForTest(account)
			account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction
			account.Extra[FormalPoolExtraPoolProfileEffective] = PoolProfileNormal
			repo := &formalRateLimitRepo{accountsByID: map[int64]*Account{account.ID: account}}
			sink := &recordingBudgetLedgerSink{}
			svc.accountRepo = repo
			svc.sessionBudgetObserve = sink
			c, ctx := newCCGatewayBoundaryContext(tc.path)

			err := tc.run(svc, ctx, c, account)

			require.Error(t, err)
			require.Equal(t, FormalPoolStageProduction, repo.accountsByID[account.ID].Extra[FormalPoolExtraOnboardingStage])
			require.True(t, repo.accountsByID[account.ID].Schedulable)
			require.NotEqual(t, StatusError, repo.accountsByID[account.ID].Status)
			require.Empty(t, repo.accountsByID[account.ID].Extra[FormalPoolExtraRiskEventRef])
			require.NotEmpty(t, sink.risks)
			require.Equal(t, RiskEventKindControlPlaneModelPolicy, sink.risks[len(sink.risks)-1].Kind)
			require.Equal(t, RiskSeverityP2, sink.risks[len(sink.risks)-1].Severity)
			require.Equal(t, BudgetActionObserve, sink.risks[len(sink.risks)-1].ActionRecommendation)
		})
	}
}

func TestCCGatewayControlPlane_APIKeyPassthroughUntrustedModelDoesNotQuarantine(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		run  func(*GatewayService, context.Context, *gin.Context, *Account) error
	}{
		{
			name: "messages",
			path: "/v1/messages",
			run: func(svc *GatewayService, ctx context.Context, c *gin.Context, account *Account) error {
				_, err := svc.forwardAnthropicAPIKeyPassthroughWithInput(ctx, c, account, anthropicPassthroughForwardInput{
					Body:          []byte(`{"model":"claude-3-7-sonnet-20250219","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`),
					RequestModel:  "claude-3-7-sonnet-20250219",
					OriginalModel: "claude-3-7-sonnet-20250219",
					RequestStream: false,
				})
				return err
			},
		},
		{
			name: "count tokens",
			path: "/v1/messages/count_tokens",
			run: func(svc *GatewayService, ctx context.Context, c *gin.Context, account *Account) error {
				return svc.forwardCountTokensAnthropicAPIKeyPassthrough(ctx, c, account, []byte(`{"model":"claude-3-7-sonnet-20250219","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`))
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &ccGatewayBoundaryUpstreamRecorder{resp: newCCGatewayControlPlaneResponse(http.StatusUnprocessableEntity, "persona_reject_untrusted_model")}
			svc := newCCGatewayBoundaryService(upstream)
			account := newAnthropicAPIKeyAccountForTest()
			account.Extra["cc_gateway_enabled"] = "true"
			account.Extra["cc_gateway_canary_only"] = "false"
			account.Extra["cc_gateway_policy_version"] = ccGatewayAnthropicPolicyVersion
			account.Extra["cc_gateway_routes"] = "native_messages,native_count_tokens"
			account.Extra["cc_gateway_egress_bucket_enabled"] = "true"
			account.Extra["cc_gateway_egress_bucket"] = "bucket-a"
			account.ProxyID = int64Ptr(903)
			account.Proxy = &Proxy{ID: 903, Name: "proxy-c", Protocol: "http", Host: "127.0.0.1", Port: 8899, Status: StatusActive}
			repo := &formalRateLimitRepo{accountsByID: map[int64]*Account{account.ID: account}}
			sink := &recordingBudgetLedgerSink{}
			svc.accountRepo = repo
			svc.sessionBudgetObserve = sink
			c, ctx := newCCGatewayBoundaryContext(tc.path)

			err := tc.run(svc, ctx, c, account)

			require.Error(t, err)
			require.True(t, repo.accountsByID[account.ID].Schedulable)
			require.NotEqual(t, StatusError, repo.accountsByID[account.ID].Status)
			require.Empty(t, repo.accountsByID[account.ID].Extra[FormalPoolExtraRiskEventRef])
			require.Empty(t, sink.risks)
		})
	}
}

func TestCCGatewayControlPlane_NonFormalDoesNotQuarantine(t *testing.T) {
	upstream := &ccGatewayBoundaryUpstreamRecorder{resp: newCCGatewayControlPlaneResponse(http.StatusForbidden, "missing_account_identity")}
	svc := newCCGatewayBoundaryService(upstream)
	account := newCCGatewayBoundaryAccount()
	repo := &formalRateLimitRepo{accountsByID: map[int64]*Account{account.ID: account}}
	svc.accountRepo = repo
	c, ctx := newCCGatewayBoundaryContext("/v1/messages")
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","stream":false,"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))

	require.Error(t, err)
	require.Empty(t, repo.accountsByID[account.ID].Extra[FormalPoolExtraOnboardingStage])
	require.NotEqual(t, StatusError, repo.accountsByID[account.ID].Status)
}
