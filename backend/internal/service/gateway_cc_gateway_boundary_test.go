package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type ccGatewayBoundaryUpstreamRecorder struct {
	lastReq      *http.Request
	lastBody     []byte
	lastProxyURL string
	lastProfile  *tlsfingerprint.Profile
	requests     int
	resp         *http.Response
	respFactory  func() *http.Response
	err          error
}

func (u *ccGatewayBoundaryUpstreamRecorder) Do(req *http.Request, proxyURL string, _ int64, _ int) (*http.Response, error) {
	return u.DoWithTLS(req, proxyURL, 0, 0, nil)
}

func (u *ccGatewayBoundaryUpstreamRecorder) DoWithTLS(req *http.Request, proxyURL string, _ int64, _ int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	u.requests++
	u.lastReq = req
	u.lastProxyURL = proxyURL
	u.lastProfile = profile
	if req != nil && req.Body != nil {
		body, _ := io.ReadAll(req.Body)
		u.lastBody = body
		_ = req.Body.Close()
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	if u.err != nil {
		return nil, u.err
	}
	if u.respFactory != nil {
		return u.respFactory(), nil
	}
	return u.resp, nil
}

func newCCGatewayBoundaryAccount() *Account {
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	account.ProxyID = int64Ptr(501)
	account.Proxy = &Proxy{
		ID:       501,
		Name:     "proxy-a",
		Protocol: "http",
		Host:     "127.0.0.1",
		Port:     8899,
		Status:   StatusActive,
	}
	account.Extra["cc_gateway_enabled"] = "true"
	account.Extra["cc_gateway_canary_only"] = "false"
	account.Extra["cc_gateway_policy_version"] = ccGatewayAnthropicPolicyVersion
	account.Extra["cc_gateway_routes"] = "native_messages,native_count_tokens,chat_completions,responses"
	account.Extra["cc_gateway_egress_bucket"] = "bucket-a"
	account.Extra["cc_gateway_egress_bucket_enabled"] = "true"
	return account
}

func newCCGatewayBoundaryService(upstream *ccGatewayBoundaryUpstreamRecorder) *GatewayService {
	cfg := ccGatewayTestConfig(PlatformAnthropic)
	cfg.Gateway.MaxLineSize = defaultMaxLineSize
	seedGatewayForwardingSettingsForTest()
	return &GatewayService{
		cfg:             cfg,
		identityService: NewIdentityService(&identityCacheStub{}),
		httpUpstream:    upstream,
	}
}

func newCCGatewayBoundaryContext(path string) (*gin.Context, context.Context) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	ctx := context.Background()
	c.Request = httptest.NewRequest(http.MethodPost, path, nil).WithContext(ctx)
	c.Request.Header.Set("User-Agent", "claude-cli/99.9.9 (external, sdk-cli)")
	c.Request.Header.Set("Anthropic-Beta", "client-beta")
	c.Request.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	c.Request.Header.Set("X-Claude-Code-Session-Id", "99999999-8888-4777-8666-555555555555")
	return c, ctx
}

func newAnthropicErrorResponse(status int) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(`{"error":{"type":"invalid_request_error","message":"local test error"}}`)),
	}
}

func TestCCGatewayBoundary_ForwardSkipsMimicryAndProxy(t *testing.T) {
	upstream := &ccGatewayBoundaryUpstreamRecorder{resp: newAnthropicSuccessResponse()}
	svc := newCCGatewayBoundaryService(upstream)
	account := newCCGatewayBoundaryAccount()
	c, ctx := newCCGatewayBoundaryContext("/v1/messages")
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","stream":false,"system":"Be terse","metadata":{"user_id":"{\"device_id\":\"fake-device\",\"account_uuid\":\"fake-acct\",\"session_id\":\"99999999-8888-4777-8666-555555555555\"}"},"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	decision := AnthropicCompatIngressDecision{InboundRoute: AnthropicCompatInboundMessages, CCGatewayRoute: AnthropicCompatCCGatewayMessages, ClientType: AnthropicCompatClientType}
	ctx = WithAnthropicCompatAuditSummary(ctx, NewAnthropicCompatAuditSummaryWithShape(decision, AnthropicCompatShapeAudit{ClientType: AnthropicCompatClientType, ServerFilledShape: true, ServerFilledFields: []string{"system"}, PersonaSource: "server_selected", CompatFidelityLevel: AnthropicCompatFidelityL2, ToolSearchMode: "truthful_pass_through", CapabilityBacked: false}))
	c.Request = c.Request.WithContext(ctx)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))
	require.NoError(t, err)
	require.Equal(t, "http://cc-gateway:8443/v1/messages?beta=true", upstream.lastReq.URL.String())
	require.Equal(t, "/v1/messages", getHeaderRaw(upstream.lastReq.Header, AnthropicCompatInboundRouteHeader))
	require.Equal(t, "/v1/messages?beta=true", getHeaderRaw(upstream.lastReq.Header, AnthropicCompatCCGatewayRouteHeader))
	require.Equal(t, "claude_code_compat", getHeaderRaw(upstream.lastReq.Header, AnthropicCompatClientTypeHeader))
	require.Equal(t, "server_selected", getHeaderRaw(upstream.lastReq.Header, AnthropicCompatPersonaSourceHeader))
	require.Equal(t, "L2", getHeaderRaw(upstream.lastReq.Header, AnthropicCompatFidelityLevelHeader))
	require.Equal(t, "true", getHeaderRaw(upstream.lastReq.Header, AnthropicCompatServerFilledShapeHeader))
	require.Equal(t, "truthful_pass_through", getHeaderRaw(upstream.lastReq.Header, AnthropicCompatToolSearchModeHeader))
	require.Equal(t, "false", getHeaderRaw(upstream.lastReq.Header, AnthropicCompatCapabilityBackedHeader))
	require.NotEqual(t, "client-beta", getHeaderRaw(upstream.lastReq.Header, "anthropic-beta"), "external anthropic-beta must not be trusted on CC Gateway path")
	require.Empty(t, getHeaderRaw(upstream.lastReq.Header, "x-app"), "external x-app must not be forwarded to CC Gateway path")
	require.False(t, bytes.Equal(body, upstream.lastBody), "formal-pool CC Gateway path must rewrite metadata.user_id session before forwarding")
	parsedUID := ParseMetadataUserID(gjson.GetBytes(upstream.lastBody, "metadata.user_id").String())
	require.NotNil(t, parsedUID)
	require.NotEqual(t, "99999999-8888-4777-8666-555555555555", parsedUID.SessionID)
	require.Equal(t, parsedUID.SessionID, getHeaderRaw(upstream.lastReq.Header, "X-Claude-Code-Session-Id"))
	require.Empty(t, upstream.lastProxyURL, "CC Gateway path must not use account proxy")
	require.Nil(t, upstream.lastProfile, "CC Gateway path must not use account TLS fingerprint profile")
}

func TestCCGatewayBoundary_ForwardNativeAuditMarkersWithoutCompatShape(t *testing.T) {
	upstream := &ccGatewayBoundaryUpstreamRecorder{resp: newAnthropicSuccessResponse()}
	svc := newCCGatewayBoundaryService(upstream)
	account := newCCGatewayBoundaryAccount()
	c, ctx := newCCGatewayBoundaryContext("/v1/messages")
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","stream":false,"metadata":{"user_id":"{\"device_id\":\"fake-device\",\"account_uuid\":\"fake-acct\",\"session_id\":\"99999999-8888-4777-8666-555555555555\"}"},"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}],"tools":[{"name":"Bash","input_schema":{"type":"object"}}]}`)

	ctx = WithClaudeCodeNativeAuditSummary(ctx, testClaudeCodeNativeReplaySafeSummary(ClaudeCodeNativeAuditSummary{
		ClientType:              ClaudeCodeNativeClientType,
		NativeAttested:          true,
		GuardVersion:            "guard_v1",
		ClaudeCodeVersion:       "2.1.175",
		LocalSessionRef:         "hmac-sha256:" + strings.Repeat("a", 64),
		InboundRoute:            AnthropicCompatInboundMessages,
		CCGatewayRoute:          AnthropicCompatCCGatewayMessages,
		NetwatchRequired:        true,
		ServerFilledShape:       false,
		ShapeHealthcheckProfile: ClaudeCodeNativeTakeoverHealthProfile,
		ToolSearchMode:          "truthful_pass_through",
		ToolReferencePresent:    true,
	}))
	ctx = SetClaudeCodeClient(ctx, true)
	c.Request = c.Request.WithContext(ctx)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))
	require.NoError(t, err)
	require.Equal(t, "http://cc-gateway:8443/v1/messages?beta=true", upstream.lastReq.URL.String())
	require.Equal(t, ClaudeCodeNativeClientType, getHeaderRaw(upstream.lastReq.Header, ClaudeCodeNativeClientTypeHeader))
	require.Equal(t, "true", getHeaderRaw(upstream.lastReq.Header, ClaudeCodeNativeGuardAttestedHeader))
	require.Equal(t, "false", getHeaderRaw(upstream.lastReq.Header, ClaudeCodeNativeServerFilledShapeHeader))
	require.Equal(t, ClaudeCodeNativeTakeoverHealthProfile, getHeaderRaw(upstream.lastReq.Header, ClaudeCodeNativeHealthcheckProfileHeader))
	require.Equal(t, ClaudeCodeNativeReplaySafetyBoundary, getHeaderRaw(upstream.lastReq.Header, ClaudeCodeNativeReplaySafetyBoundaryHeader))
	require.Equal(t, "true", getHeaderRaw(upstream.lastReq.Header, ClaudeCodeNativeReplaySafetyAppliedHeader))
	require.Equal(t, "false", getHeaderRaw(upstream.lastReq.Header, ClaudeCodeNativeReplaySafetySanitizedHeader))
	require.Equal(t, "0", getHeaderRaw(upstream.lastReq.Header, ClaudeCodeNativeReplaySafetyForbiddenPathsHeader))
	require.Equal(t, "sha256:"+strings.Repeat("d", 64), getHeaderRaw(upstream.lastReq.Header, ClaudeCodeNativeReplaySafetyBodyShapeHashHeader))
	require.Empty(t, getHeaderRaw(upstream.lastReq.Header, AnthropicCompatClientTypeHeader))
	require.True(t, bytes.Equal(body, upstream.lastBody), "native takeover path must preserve real Claude Code body without server-filled compat shape")
	require.Empty(t, upstream.lastProxyURL, "CC Gateway native path must not use account proxy")
}

func TestCCGatewayBoundary_StripsDownstreamBillingBeforeSignPrimary(t *testing.T) {
	upstream := &ccGatewayBoundaryUpstreamRecorder{resp: newAnthropicSuccessResponse()}
	svc := newCCGatewayBoundaryService(upstream)
	account := newCCGatewayBoundaryAccount()
	account.Extra["billing_cch_mode"] = "sign"
	require.True(t, shouldStripCCGatewayDownstreamBillingMaterial(account))
	c, ctx := newCCGatewayBoundaryContext("/v1/messages")
	body := []byte(`{"model":"claude-opus-4-7","stream":false,"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.145.abc; cc_entrypoint=sdk-cli; cch=12345;"},{"type":"text","text":"You are Claude Code."}],"metadata":{"user_id":"{\"device_id\":\"fake-device\",\"account_uuid\":\"fake-acct\",\"session_id\":\"99999999-8888-4777-8666-555555555555\"}"},"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)
	require.NotContains(t, strings.ToLower(string(stripCCGatewayDownstreamBillingMaterial(body))), "x-anthropic-billing-header")

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))
	require.NoError(t, err)
	require.Equal(t, "http://cc-gateway:8443/v1/messages?beta=true", upstream.lastReq.URL.String())
	require.NotContains(t, strings.ToLower(string(upstream.lastBody)), "x-anthropic-billing-header")
	require.NotContains(t, strings.ToLower(string(upstream.lastBody)), "cch=12345")
	require.Contains(t, string(upstream.lastBody), "You are Claude Code.")
	parsedUID := ParseMetadataUserID(gjson.GetBytes(upstream.lastBody, "metadata.user_id").String())
	require.NotNil(t, parsedUID)
	require.NotEqual(t, "99999999-8888-4777-8666-555555555555", parsedUID.SessionID)
}

func TestCCGatewayBoundary_ForwardCountTokensSkipsMimicryAndProxy(t *testing.T) {
	upstream := &ccGatewayBoundaryUpstreamRecorder{resp: newAnthropicCountTokensSuccessResponse()}
	svc := newCCGatewayBoundaryService(upstream)
	account := newCCGatewayBoundaryAccount()
	c, ctx := newCCGatewayBoundaryContext("/v1/messages/count_tokens")
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","metadata":{"user_id":"{\"device_id\":\"fake-device\",\"account_uuid\":\"fake-acct\",\"session_id\":\"99999999-8888-4777-8666-555555555555\"}"},"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	err := svc.ForwardCountTokens(ctx, c, account, parseAnthropicRequestForTest(t, body))
	require.NoError(t, err)
	require.Equal(t, "http://cc-gateway:8443/v1/messages/count_tokens?beta=true", upstream.lastReq.URL.String())
	require.False(t, bytes.Equal(body, upstream.lastBody), "formal-pool CC Gateway count_tokens path must rewrite metadata.user_id session before forwarding")
	parsedUID := ParseMetadataUserID(gjson.GetBytes(upstream.lastBody, "metadata.user_id").String())
	require.NotNil(t, parsedUID)
	require.NotEqual(t, "99999999-8888-4777-8666-555555555555", parsedUID.SessionID)
	require.Equal(t, parsedUID.SessionID, getHeaderRaw(upstream.lastReq.Header, "X-Claude-Code-Session-Id"))
	require.Empty(t, upstream.lastProxyURL, "CC Gateway count_tokens path must not use account proxy")
	require.Nil(t, upstream.lastProfile, "CC Gateway count_tokens path must not use account TLS fingerprint profile")
}

func TestCCGatewayBoundary_ForwardNativeCountTokensAuditMarkersWithoutCompatShape(t *testing.T) {
	upstream := &ccGatewayBoundaryUpstreamRecorder{resp: newAnthropicCountTokensSuccessResponse()}
	svc := newCCGatewayBoundaryService(upstream)
	account := newCCGatewayBoundaryAccount()
	c, ctx := newCCGatewayBoundaryContext("/v1/messages/count_tokens")
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","metadata":{"user_id":"{\"device_id\":\"fake-device\",\"account_uuid\":\"fake-acct\",\"session_id\":\"99999999-8888-4777-8666-555555555555\"}"},"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	ctx = WithClaudeCodeNativeAuditSummary(ctx, testClaudeCodeNativeReplaySafeSummary(ClaudeCodeNativeAuditSummary{
		ClientType:              ClaudeCodeNativeClientType,
		NativeAttested:          true,
		GuardVersion:            "guard_v1",
		ClaudeCodeVersion:       "2.1.175",
		LocalSessionRef:         "hmac-sha256:" + strings.Repeat("a", 64),
		InboundRoute:            "/v1/messages/count_tokens",
		CCGatewayRoute:          "/v1/messages/count_tokens?beta=true",
		NetwatchRequired:        true,
		ServerFilledShape:       false,
		ShapeHealthcheckProfile: ClaudeCodeNativeTakeoverHealthProfile,
		ToolSearchMode:          "not_present",
	}))
	c.Request = c.Request.WithContext(ctx)

	err := svc.ForwardCountTokens(ctx, c, account, parseAnthropicRequestForTest(t, body))
	require.NoError(t, err)
	require.Equal(t, "http://cc-gateway:8443/v1/messages/count_tokens?beta=true", upstream.lastReq.URL.String())
	require.Equal(t, ClaudeCodeNativeClientType, getHeaderRaw(upstream.lastReq.Header, ClaudeCodeNativeClientTypeHeader))
	require.Equal(t, "false", getHeaderRaw(upstream.lastReq.Header, ClaudeCodeNativeServerFilledShapeHeader))
	require.Equal(t, ClaudeCodeNativeReplaySafetyBoundary, getHeaderRaw(upstream.lastReq.Header, ClaudeCodeNativeReplaySafetyBoundaryHeader))
	require.Equal(t, "true", getHeaderRaw(upstream.lastReq.Header, ClaudeCodeNativeReplaySafetyAppliedHeader))
	require.Empty(t, getHeaderRaw(upstream.lastReq.Header, AnthropicCompatClientTypeHeader))
	require.True(t, bytes.Equal(body, upstream.lastBody), "native count_tokens path must preserve real Claude Code body")
}

func TestCCGatewayBoundary_ForwardAsChatCompletionsSkipsMimicryAndProxy(t *testing.T) {
	upstream := &ccGatewayBoundaryUpstreamRecorder{resp: newAnthropicErrorResponse(http.StatusBadRequest)}
	svc := newCCGatewayBoundaryService(upstream)
	account := newCCGatewayBoundaryAccount()
	c, ctx := newCCGatewayBoundaryContext("/v1/chat/completions")
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","messages":[{"role":"user","content":"hello"}],"stream":false}`)

	var ccReq apicompat.ChatCompletionsRequest
	require.NoError(t, json.Unmarshal(body, &ccReq))
	responsesReq, err := apicompat.ChatCompletionsToResponses(&ccReq)
	require.NoError(t, err)
	anthropicReq, err := apicompat.ResponsesToAnthropicRequest(responsesReq)
	require.NoError(t, err)
	anthropicReq.Stream = true
	anthropicReq.Model = ccReq.Model
	expectedBody, err := json.Marshal(anthropicReq)
	require.NoError(t, err)

	parsed := &ParsedRequest{Body: NewRequestBodyRef(body), Model: ccReq.Model, Stream: false}
	_, err = svc.ForwardAsChatCompletions(ctx, c, account, body, parsed)
	require.Error(t, err)
	require.Equal(t, "http://cc-gateway:8443/v1/messages?beta=true", upstream.lastReq.URL.String())
	require.True(t, bytes.Equal(expectedBody, upstream.lastBody), "CC Gateway chat_completions path must send converted Anthropic body without Sub2API mimicry mutation")
	require.Empty(t, upstream.lastProxyURL, "CC Gateway chat_completions path must not use account proxy")
	require.Nil(t, upstream.lastProfile, "CC Gateway chat_completions path must not use account TLS fingerprint profile")
}

func TestCCGatewayBoundary_ForwardAsResponsesSkipsMimicryAndProxy(t *testing.T) {
	upstream := &ccGatewayBoundaryUpstreamRecorder{resp: newAnthropicErrorResponse(http.StatusBadRequest)}
	svc := newCCGatewayBoundaryService(upstream)
	account := newCCGatewayBoundaryAccount()
	c, ctx := newCCGatewayBoundaryContext("/v1/responses")
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","input":"hello","stream":false}`)

	var responsesReq apicompat.ResponsesRequest
	require.NoError(t, json.Unmarshal(body, &responsesReq))
	anthropicReq, err := apicompat.ResponsesToAnthropicRequest(&responsesReq)
	require.NoError(t, err)
	anthropicReq.Stream = true
	anthropicReq.Model = responsesReq.Model
	expectedBody, err := json.Marshal(anthropicReq)
	require.NoError(t, err)

	parsed := &ParsedRequest{Body: NewRequestBodyRef(body), Model: responsesReq.Model, Stream: false}
	_, err = svc.ForwardAsResponses(ctx, c, account, body, parsed)
	require.Error(t, err)
	require.Equal(t, "http://cc-gateway:8443/v1/messages?beta=true", upstream.lastReq.URL.String())
	require.True(t, bytes.Equal(expectedBody, upstream.lastBody), "CC Gateway responses path must send converted Anthropic body without Sub2API mimicry mutation")
	require.Empty(t, upstream.lastProxyURL, "CC Gateway responses path must not use account proxy")
	require.Nil(t, upstream.lastProfile, "CC Gateway responses path must not use account TLS fingerprint profile")
}

func TestCCGatewayBoundary_FormalPoolCompatMessagesFailClosedBeforeUpstream(t *testing.T) {
	upstream := &ccGatewayBoundaryUpstreamRecorder{resp: newAnthropicSuccessResponse()}
	svc := newCCGatewayBoundaryService(upstream)
	account := newCCGatewayBoundaryAccount()
	formalPoolApplyCompleteSchedulingEvidenceForTest(account)
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction
	account.Extra[FormalPoolExtraPoolProfileEffective] = PoolProfileNormal
	c, ctx := newCCGatewayBoundaryContext("/v1/messages")
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","stream":false,"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	decision := AnthropicCompatIngressDecision{
		InboundRoute:   AnthropicCompatInboundMessages,
		CCGatewayRoute: AnthropicCompatCCGatewayMessages,
		ClientType:     AnthropicCompatClientType,
	}
	ctx = WithAnthropicCompatAuditSummary(ctx, NewAnthropicCompatAuditSummary(decision))
	c.Request = c.Request.WithContext(ctx)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))

	require.Error(t, err)
	require.Contains(t, err.Error(), "formal pool")
	require.Zero(t, upstream.requests, "formal-pool compat requests must fail closed before any CC Gateway or direct Anthropic attempt")
}

func TestCCGatewayBoundary_FormalPoolResponsesFailClosedBeforeUpstream(t *testing.T) {
	upstream := &ccGatewayBoundaryUpstreamRecorder{resp: newAnthropicErrorResponse(http.StatusBadRequest)}
	svc := newCCGatewayBoundaryService(upstream)
	account := newCCGatewayBoundaryAccount()
	formalPoolApplyCompleteSchedulingEvidenceForTest(account)
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction
	account.Extra[FormalPoolExtraPoolProfileEffective] = PoolProfileNormal
	c, ctx := newCCGatewayBoundaryContext("/v1/responses")
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","input":"hello","stream":false}`)

	parsed := &ParsedRequest{Body: NewRequestBodyRef(body), Model: "claude-3-7-sonnet-20250219", Stream: false}
	_, err := svc.ForwardAsResponses(ctx, c, account, body, parsed)

	require.Error(t, err)
	require.Contains(t, err.Error(), "formal pool")
	require.Zero(t, upstream.requests, "formal-pool compat routes must fail closed before any CC Gateway or direct Anthropic attempt")
}

func TestCCGatewayBoundary_FormalPoolGatewayOnlyNativeCLIWithoutLocalGuardReachesCCGateway(t *testing.T) {
	useClaudeCodeSessionBoundaryLedgerFileForTest(t)
	upstream := &ccGatewayBoundaryUpstreamRecorder{resp: newAnthropicSuccessResponse()}
	svc := newCCGatewayBoundaryService(upstream)
	account := newCCGatewayBoundaryAccount()
	formalPoolApplyCompleteSchedulingEvidenceForTest(account)
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction
	account.Extra[FormalPoolExtraPoolProfileEffective] = PoolProfileNormal
	c, ctx := newCCGatewayBoundaryContext("/v1/messages")
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","stream":false,"system":[{"type":"text","text":"You are Claude Code, Anthropic's official CLI for Claude."}],"metadata":{"user_id":"{\"device_id\":\"abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd\",\"session_id\":\"123e4567-e89b-42d3-a456-426614174000\"}"},"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}],"tools":[{"name":"Bash","input_schema":{"type":"object"}}]}`)

	decision := AnthropicCompatIngressDecision{
		InboundRoute:   AnthropicCompatInboundMessages,
		CCGatewayRoute: AnthropicCompatCCGatewayMessages,
		ClientType:     AnthropicCompatClientType,
	}
	ctx = WithAnthropicCompatAuditSummary(ctx, NewAnthropicCompatAuditSummaryWithShape(decision, AnthropicCompatShapeAudit{
		ClientType:          AnthropicCompatClientType,
		ServerFilledShape:   false,
		PersonaSource:       "server_selected",
		CompatFidelityLevel: AnthropicCompatFidelityL2,
		ToolSearchMode:      "truthful_pass_through",
		CapabilityBacked:    false,
	}))
	ctx = SetClaudeCodeClient(ctx, true)
	c.Request = c.Request.WithContext(ctx)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))

	require.NoError(t, err)
	require.Equal(t, 1, upstream.requests)
	require.Equal(t, "http://cc-gateway:8443/v1/messages?beta=true", upstream.lastReq.URL.String())
	require.Empty(t, upstream.lastProxyURL, "gateway-only native CLI formal-pool path must not use direct Anthropic proxy")
	require.Nil(t, upstream.lastProfile, "gateway-only native CLI formal-pool path must not use direct Anthropic TLS profile")
	require.Empty(t, getHeaderRaw(upstream.lastReq.Header, ClaudeCodeNativeGuardAttestedHeader), "missing local guard evidence must not be forged into native attestation")
}

func TestCCGatewayBoundary_FormalPoolCCGatewayOrdinary502FailsClosedWithoutFailover(t *testing.T) {
	useClaudeCodeSessionBoundaryLedgerFileForTest(t)
	upstream := &ccGatewayBoundaryUpstreamRecorder{respFactory: func() *http.Response {
		return newAnthropicErrorResponse(http.StatusBadGateway)
	}}
	svc := newCCGatewayBoundaryService(upstream)
	account := newCCGatewayBoundaryAccount()
	formalPoolApplyCompleteSchedulingEvidenceForTest(account)
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction
	account.Extra[FormalPoolExtraPoolProfileEffective] = PoolProfileNormal
	c, ctx := newCCGatewayBoundaryContext("/v1/messages")
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","stream":false,"system":[{"type":"text","text":"You are Claude Code, Anthropic's official CLI for Claude."}],"metadata":{"user_id":"{\"device_id\":\"abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd\",\"session_id\":\"123e4567-e89b-42d3-a456-426614174000\"}"},"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)
	ctx = SetClaudeCodeClient(ctx, true)
	c.Request = c.Request.WithContext(ctx)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))

	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr), "CC Gateway ordinary 502 for formal-pool native traffic must fail closed without account failover")
	require.GreaterOrEqual(t, upstream.requests, 1)
	require.Equal(t, "http://cc-gateway:8443/v1/messages?beta=true", upstream.lastReq.URL.String())
	require.Empty(t, upstream.lastProxyURL)
	require.Nil(t, upstream.lastProfile)
}

func TestCCGatewayBoundary_FormalPoolCCGatewayTransportUnavailableFailsClosedWithoutFailover(t *testing.T) {
	useClaudeCodeSessionBoundaryLedgerFileForTest(t)
	upstream := &ccGatewayBoundaryUpstreamRecorder{err: errors.New("cc gateway transport unavailable: dial tcp 127.0.0.1:9: connect: connection refused")}
	svc := newCCGatewayBoundaryService(upstream)
	account := newCCGatewayBoundaryAccount()
	formalPoolApplyCompleteSchedulingEvidenceForTest(account)
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction
	account.Extra[FormalPoolExtraPoolProfileEffective] = PoolProfileNormal
	c, ctx := newCCGatewayBoundaryContext("/v1/messages")
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","stream":false,"system":[{"type":"text","text":"You are Claude Code, Anthropic's official CLI for Claude."}],"metadata":{"user_id":"{\"device_id\":\"abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd\",\"session_id\":\"123e4567-e89b-42d3-a456-426614174000\"}"},"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)
	ctx = SetClaudeCodeClient(ctx, true)
	c.Request = c.Request.WithContext(ctx)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))

	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr), "CC Gateway transport failure for formal-pool native traffic must fail closed without account failover")
	require.Equal(t, 1, upstream.requests)
	require.Equal(t, "http://cc-gateway:8443/v1/messages?beta=true", upstream.lastReq.URL.String())
	require.Empty(t, upstream.lastProxyURL)
	require.Nil(t, upstream.lastProfile)
	require.NotContains(t, err.Error(), "123e4567-e89b-42d3-a456-426614174000")
}

func TestCCGatewayBoundary_ForwardNativeRichShapeWithoutDowngradingBodyFields(t *testing.T) {
	upstream := &ccGatewayBoundaryUpstreamRecorder{resp: newAnthropicSuccessResponse()}
	svc := newCCGatewayBoundaryService(upstream)
	account := newCCGatewayBoundaryAccount()
	c, ctx := newCCGatewayBoundaryContext("/v1/messages")
	body := loadNativeFixture(t, "messages_rich_native_shape.json")

	ctx = WithClaudeCodeNativeAuditSummary(ctx, buildClaudeCodeNativeAuditSummary(&ClaudeCodeNativeAttestationPayload{
		RequestURI:                ClaudeCodeNativeInboundMessages,
		GuardVersion:              "guard_v1",
		ClaudeCodeVersion:         "2.1.175",
		LocalSessionRef:           "hmac-sha256:" + strings.Repeat("f", 64),
		ShapeHealthcheckProfile:   ClaudeCodeNativeTakeoverHealthProfile,
		ReplaySafetyBoundary:      ClaudeCodeNativeReplaySafetyBoundary,
		ReplaySafetyApplied:       true,
		ReplaySafetyBodyShapeHash: claudeCodeNativeBodyShapeHash(body),
	}, body))
	c.Request = c.Request.WithContext(ctx)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))
	require.NoError(t, err)
	require.True(t, bytes.Equal(body, upstream.lastBody), "native rich body must reach CC Gateway without Sub2API field downgrade")
	require.Equal(t, ClaudeCodeNativeClientType, getHeaderRaw(upstream.lastReq.Header, ClaudeCodeNativeClientTypeHeader))
	require.Equal(t, "true", getHeaderRaw(upstream.lastReq.Header, ClaudeCodeNativeGuardAttestedHeader))
	require.Equal(t, "false", getHeaderRaw(upstream.lastReq.Header, ClaudeCodeNativeServerFilledShapeHeader))
	require.Empty(t, getHeaderRaw(upstream.lastReq.Header, AnthropicCompatClientTypeHeader))

	out := upstream.lastBody
	require.Equal(t, "adaptive", gjson.GetBytes(out, "thinking.type").String())
	require.True(t, gjson.GetBytes(out, "context_management.edits.0.type").Exists())
	require.Equal(t, "high", gjson.GetBytes(out, "output_config.effort").String())
	require.Len(t, gjson.GetBytes(out, "tools").Array(), 3)
	require.ElementsMatch(t, []string{"Bash", "Edit", "Read"}, []string{
		gjson.GetBytes(out, "tools.0.name").String(),
		gjson.GetBytes(out, "tools.1.name").String(),
		gjson.GetBytes(out, "tools.2.name").String(),
	})
	require.Len(t, gjson.GetBytes(out, "system").Array(), 2)
	require.True(t, gjson.GetBytes(out, "eager_input_streaming").Bool())
	require.Empty(t, upstream.lastProxyURL, "native CC Gateway path must not use account proxy")
	require.Nil(t, upstream.lastProfile, "native CC Gateway path must not use account TLS fingerprint profile")
}

func TestCCGatewayBoundary_FormalPoolNativeAttestedPathBuildsCCGatewayAttestationAfterWireRestore(t *testing.T) {
	useClaudeCodeSessionBoundaryLedgerFileForTest(t)
	upstream := &ccGatewayBoundaryUpstreamRecorder{resp: newAnthropicSuccessResponse()}
	svc := newCCGatewayBoundaryService(upstream)
	account := newCCGatewayBoundaryAccount()
	formalPoolApplyCompleteSchedulingEvidenceForTest(account)
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction
	account.Extra[FormalPoolExtraPoolProfileEffective] = PoolProfileNormal
	c, ctx := newCCGatewayBoundaryContext("/v1/messages")
	body := loadNativeFixture(t, "messages_rich_native_shape.json")

	ctx = WithClaudeCodeNativeAuditSummary(ctx, buildClaudeCodeNativeAuditSummary(&ClaudeCodeNativeAttestationPayload{
		RequestURI:                ClaudeCodeNativeInboundMessages,
		GuardVersion:              "guard_v1",
		ClaudeCodeVersion:         "2.1.175",
		LocalSessionRef:           "hmac-sha256:" + strings.Repeat("f", 64),
		ShapeHealthcheckProfile:   ClaudeCodeNativeTakeoverHealthProfile,
		ReplaySafetyBoundary:      ClaudeCodeNativeReplaySafetyBoundary,
		ReplaySafetyApplied:       true,
		ReplaySafetyBodyShapeHash: claudeCodeNativeBodyShapeHash(body),
	}, body))
	c.Request = c.Request.WithContext(ctx)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))

	require.NoError(t, err)
	require.Equal(t, 1, upstream.requests)
	require.True(t, bytes.Equal(body, upstream.lastBody), "native attested body must remain wire-equivalent while server session authority is retained for attestation")
	require.NotEmpty(t, getHeaderRaw(upstream.lastReq.Header, ccGatewayFormalPoolContextHeader))
	require.NotEmpty(t, getHeaderRaw(upstream.lastReq.Header, ccGatewayFormalPoolSignatureHeader))
	require.NotEmpty(t, getHeaderRaw(upstream.lastReq.Header, "X-Claude-Code-Session-Id"))
}

func TestCCGatewayBoundary_FormalPoolSessionBoundarySwapFailsBeforeUpstream(t *testing.T) {
	useClaudeCodeSessionBoundaryLedgerFileForTest(t)
	upstream := &ccGatewayBoundaryUpstreamRecorder{resp: newAnthropicSuccessResponse()}
	svc := newCCGatewayBoundaryService(upstream)
	accountA := newCCGatewayBoundaryAccount()
	formalPoolApplyCompleteSchedulingEvidenceForTest(accountA)
	accountA.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction
	accountA.Extra[FormalPoolExtraPoolProfileEffective] = PoolProfileNormal
	accountA.Extra[ccGatewayExtraAccountRef] = "opaque:acct:boundary-a"
	accountA.Extra[ccGatewayExtraEgressBucket] = "bucket-a"
	accountA.Extra["claude_code_device_id"] = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	accountB := newCCGatewayBoundaryAccount()
	formalPoolApplyCompleteSchedulingEvidenceForTest(accountB)
	accountB.ID = accountA.ID + 1
	accountB.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction
	accountB.Extra[FormalPoolExtraPoolProfileEffective] = PoolProfileNormal
	accountB.Extra[ccGatewayExtraAccountRef] = "opaque:acct:boundary-b"
	accountB.Extra[ccGatewayExtraEgressBucket] = "bucket-b"
	accountB.Extra["claude_code_device_id"] = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","stream":false,"system":[{"type":"text","text":"You are Claude Code, Anthropic's official CLI for Claude."}],"metadata":{"user_id":"{\"device_id\":\"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff\",\"session_id\":\"123e4567-e89b-42d3-a456-426614174000\"}"},"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	c1, ctx1 := newCCGatewayBoundaryContext("/v1/messages")
	ctx1 = SetClaudeCodeClient(WithClaudeCodeSessionUserScope(ctx1, "user:cp39-boundary"), true)
	c1.Request = c1.Request.WithContext(ctx1)
	_, err := svc.Forward(ctx1, c1, accountA, parseAnthropicRequestForTest(t, body))
	require.NoError(t, err)
	require.Equal(t, 1, upstream.requests)

	c2, ctx2 := newCCGatewayBoundaryContext("/v1/messages")
	ctx2 = SetClaudeCodeClient(WithClaudeCodeSessionUserScope(ctx2, "user:cp39-boundary"), true)
	c2.Request = c2.Request.WithContext(ctx2)
	_, err = svc.Forward(ctx2, c2, accountB, parseAnthropicRequestForTest(t, body))
	require.Error(t, err)
	var boundaryErr *ClaudeCodeSessionBoundaryError
	require.ErrorAs(t, err, &boundaryErr)
	require.Equal(t, "claude_native_session_boundary_failed", boundaryErr.Code)
	require.Equal(t, 1, upstream.requests, "identity boundary failure must fail before CC Gateway/direct Anthropic upstream")
	dumped, marshalErr := json.Marshal(boundaryErr)
	require.NoError(t, marshalErr)
	require.NotContains(t, string(dumped), "123e4567-e89b-42d3-a456-426614174000")
}
