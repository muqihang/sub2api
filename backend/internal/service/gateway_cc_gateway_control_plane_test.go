package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
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

func TestCCGatewayControlPlane_FormalPoolMissingIdentityQuarantines(t *testing.T) {
	upstream := &ccGatewayBoundaryUpstreamRecorder{resp: newCCGatewayControlPlaneResponse(http.StatusForbidden, "missing_account_identity")}
	svc := newCCGatewayBoundaryService(upstream)
	account := newCCGatewayBoundaryAccount()
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
