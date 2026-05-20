package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func seedGatewayForwardingSettingsForTest() {
	gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{
		fingerprintUnification:       true,
		metadataPassthrough:          false,
		cchSigning:                   false,
		anthropicCacheTTL1hInjection: false,
		expiresAt:                    time.Now().Add(time.Hour).UnixNano(),
	})
}

func newAnthropicOAuthAccountForClaudeForwardTest() *Account {
	return &Account{
		ID:          301,
		Name:        "anthropic-oauth-forward-test",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token": "oauth-token",
		},
		Extra: map[string]any{
			"account_uuid": "acct-uuid",
		},
		Status:      StatusActive,
		Schedulable: true,
	}
}

func newAnthropicForwardTestService(upstream *anthropicHTTPUpstreamRecorder) *GatewayService {
	seedGatewayForwardingSettingsForTest()
	return &GatewayService{
		cfg:             &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}},
		identityService: NewIdentityService(&identityCacheStub{}),
		httpUpstream:    upstream,
	}
}

func newAnthropicForwardTestContext(path string, strict bool) (*gin.Context, context.Context) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	baseCtx := context.Background()
	if strict {
		baseCtx = SetClaudeCodeClient(baseCtx, true)
	}
	c.Request = httptest.NewRequest(http.MethodPost, path, nil).WithContext(baseCtx)
	return c, baseCtx
}

func newAnthropicSuccessResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"x-request-id": []string{"req_test_1"},
		},
		Body: io.NopCloser(strings.NewReader(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-3-7-sonnet-20250219","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}`)),
	}
}

func newAnthropicCountTokensSuccessResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"x-request-id": []string{"req_ct_1"},
		},
		Body: io.NopCloser(strings.NewReader(`{"input_tokens":42}`)),
	}
}

func parseAnthropicRequestForTest(t *testing.T, body []byte) *ParsedRequest {
	t.Helper()
	parsed, err := ParseGatewayRequest(body, PlatformAnthropic)
	require.NoError(t, err)
	return parsed
}

func TestStrictPassthrough_ForwardBodyBytesUnchanged(t *testing.T) {
	upstream := &anthropicHTTPUpstreamRecorder{resp: newAnthropicSuccessResponse()}
	svc := newAnthropicForwardTestService(upstream)
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	c, ctx := newAnthropicForwardTestContext("/v1/messages", true)
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.145 (external, sdk-cli)")
	c.Request.Header.Set("Anthropic-Beta", "client-beta")
	c.Request.Header.Set("X-Claude-Code-Session-Id", "11111111-2222-4333-8444-555555555555")
	c.Request.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","stream":false,"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.145.abc; cc_entrypoint=sdk-cli; cch=12345;"}],"metadata":{"user_id":"{\"device_id\":\"client-device\",\"account_uuid\":\"acct-client\",\"session_id\":\"11111111-2222-4333-8444-555555555555\"}"},"messages":[{"role":"user","content":[{"type":"text","text":"hello"},{"type":"text","text":""}]}]}`)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))
	require.NoError(t, err)
	require.NotNil(t, upstream.lastReq)
	require.True(t, bytes.Equal(body, upstream.lastBody), "strict passthrough must preserve body bytes exactly")
}

func TestStrictPassthrough_ForwardHeadersNotOverwrittenAndNoAcceptEncoding(t *testing.T) {
	upstream := &anthropicHTTPUpstreamRecorder{resp: newAnthropicSuccessResponse()}
	svc := newAnthropicForwardTestService(upstream)
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	c, ctx := newAnthropicForwardTestContext("/v1/messages", true)
	c.Request.Header.Set("User-Agent", "claude-cli/9.9.9 (external, sdk-cli)")
	c.Request.Header.Set("Anthropic-Beta", "beta-a,beta-b")
	c.Request.Header.Set("X-Claude-Code-Session-Id", "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee")
	c.Request.Header.Set("X-Stainless-Lang", "custom-js")
	c.Request.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","stream":false,"metadata":{"user_id":"{\"device_id\":\"client-device\",\"account_uuid\":\"acct-client\",\"session_id\":\"aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee\"}"},"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))
	require.NoError(t, err)
	require.Equal(t, "claude-cli/9.9.9 (external, sdk-cli)", getHeaderRaw(upstream.lastReq.Header, "User-Agent"))
	require.Equal(t, "beta-a,beta-b", getHeaderRaw(upstream.lastReq.Header, "anthropic-beta"))
	require.Equal(t, "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", getHeaderRaw(upstream.lastReq.Header, "X-Claude-Code-Session-Id"))
	require.Equal(t, "custom-js", getHeaderRaw(upstream.lastReq.Header, "X-Stainless-Lang"))
	require.Empty(t, getHeaderRaw(upstream.lastReq.Header, "Accept-Encoding"))
}

func TestStrictPassthrough_NoBillingSyncOrSign(t *testing.T) {
	upstream := &anthropicHTTPUpstreamRecorder{resp: newAnthropicSuccessResponse()}
	svc := newAnthropicForwardTestService(upstream)
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	c, ctx := newAnthropicForwardTestContext("/v1/messages", true)
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.145 (external, sdk-cli)")
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","stream":false,"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.145.abc; cc_entrypoint=sdk-cli; cch=12345;"}],"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))
	require.NoError(t, err)
	require.Contains(t, string(upstream.lastBody), "cc_version=2.1.145.abc")
	require.Contains(t, string(upstream.lastBody), "cch=12345")
}

func TestMimicry_OverridesFakeMetadataUserIDAndSetsSessionHeader(t *testing.T) {
	upstream := &anthropicHTTPUpstreamRecorder{resp: newAnthropicSuccessResponse()}
	svc := newAnthropicForwardTestService(upstream)
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	c, ctx := newAnthropicForwardTestContext("/v1/messages", false)
	c.Request.Header.Set("User-Agent", "claude-cli/99.9.9 (external, sdk-cli)")
	c.Request.Header.Set("Anthropic-Beta", "suspicious-beta")
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","stream":false,"system":"Be terse","metadata":{"user_id":"{\"device_id\":\"fake-device\",\"account_uuid\":\"fake-acct\",\"session_id\":\"99999999-8888-4777-8666-555555555555\"}"},"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))
	require.NoError(t, err)
	require.Equal(t, claude.DefaultHeaders["User-Agent"], getHeaderRaw(upstream.lastReq.Header, "User-Agent"), "legacy detector hit must still go mimic, not strict")
	require.Equal(t, strings.Join(claude.ClaudeCodeMessagesOAuthBetas(), ","), getHeaderRaw(upstream.lastReq.Header, "anthropic-beta"))
	require.NotContains(t, string(upstream.lastBody), "x-anthropic-billing-header")

	uidRaw := gjson.GetBytes(upstream.lastBody, "metadata.user_id").String()
	require.NotEqual(t, `{"device_id":"fake-device","account_uuid":"fake-acct","session_id":"99999999-8888-4777-8666-555555555555"}`, uidRaw)
	parsedUID := ParseMetadataUserID(uidRaw)
	require.NotNil(t, parsedUID)
	require.Equal(t, "acct-uuid", parsedUID.AccountUUID)
	require.NotEqual(t, "fake-device", parsedUID.DeviceID)
	require.Equal(t, parsedUID.SessionID, getHeaderRaw(upstream.lastReq.Header, "X-Claude-Code-Session-Id"))
}

func TestMimicry_ForwardHeadersUseSafeDefaultFingerprintOnCacheMiss(t *testing.T) {
	upstream := &anthropicHTTPUpstreamRecorder{resp: newAnthropicSuccessResponse()}
	cache := &identityCacheStub{}
	seedGatewayForwardingSettingsForTest()
	svc := &GatewayService{
		cfg:             &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}},
		identityService: NewIdentityService(cache),
		httpUpstream:    upstream,
	}
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	c, ctx := newAnthropicForwardTestContext("/v1/messages", false)
	c.Request.Header.Set("User-Agent", "claude-cli/99.9.9 (external, sdk-cli)")
	c.Request.Header.Set("X-Stainless-OS", "Windows")
	c.Request.Header.Set("X-Stainless-Arch", "x64")
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","metadata":{"user_id":"{\"device_id\":\"fake-device\",\"account_uuid\":\"fake-acct\",\"session_id\":\"99999999-8888-4777-8666-555555555555\"}"},"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))
	require.NoError(t, err)
	require.Equal(t, defaultFingerprint.UserAgent, getHeaderRaw(upstream.lastReq.Header, "User-Agent"))
	require.Equal(t, defaultFingerprint.StainlessOS, getHeaderRaw(upstream.lastReq.Header, "X-Stainless-OS"))
	require.Equal(t, defaultFingerprint.StainlessArch, getHeaderRaw(upstream.lastReq.Header, "X-Stainless-Arch"))
	require.NotNil(t, cache.setFingerprint, "cache miss should synthesize and persist a safe default fingerprint")
	require.Equal(t, defaultFingerprint.UserAgent, cache.setFingerprint.UserAgent)
	require.Equal(t, defaultFingerprint.StainlessOS, cache.setFingerprint.StainlessOS)
	require.Equal(t, defaultFingerprint.StainlessArch, cache.setFingerprint.StainlessArch)
}

func TestMimicry_ForwardHeadersUseCachedFingerprintInsteadOfSpoofedClientHeaders(t *testing.T) {
	upstream := &anthropicHTTPUpstreamRecorder{resp: newAnthropicSuccessResponse()}
	cache := &identityCacheStub{
		fingerprint: &Fingerprint{
			ClientID:                "cached-client-id",
			UserAgent:               "claude-cli/2.1.145 (external, sdk-cli)",
			StainlessLang:           "js",
			StainlessPackageVersion: "0.94.0",
			StainlessOS:             "MacOS",
			StainlessArch:           "x64",
			StainlessRuntime:        "node",
			StainlessRuntimeVersion: "v24.3.0",
			UpdatedAt:               time.Now().Unix(),
		},
	}
	seedGatewayForwardingSettingsForTest()
	svc := &GatewayService{
		cfg:             &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}},
		identityService: NewIdentityService(cache),
		httpUpstream:    upstream,
	}
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	c, ctx := newAnthropicForwardTestContext("/v1/messages", false)
	c.Request.Header.Set("User-Agent", "claude-cli/99.9.9 (external, sdk-cli)")
	c.Request.Header.Set("X-Stainless-OS", "Windows")
	c.Request.Header.Set("X-Stainless-Arch", "armv7")
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","metadata":{"user_id":"{\"device_id\":\"fake-device\",\"account_uuid\":\"fake-acct\",\"session_id\":\"99999999-8888-4777-8666-555555555555\"}"},"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))
	require.NoError(t, err)
	require.Equal(t, cache.fingerprint.UserAgent, getHeaderRaw(upstream.lastReq.Header, "User-Agent"))
	require.Equal(t, "MacOS", getHeaderRaw(upstream.lastReq.Header, "X-Stainless-OS"))
	require.Equal(t, "x64", getHeaderRaw(upstream.lastReq.Header, "X-Stainless-Arch"))
	require.Nil(t, cache.setFingerprint, "cached mimicry fingerprint must not be rewritten from client headers")
}

func TestApplyClaudeCodeOAuthMimicryToBody_OverridesFakeMetadataUserIDAndRemovesBillingBlock(t *testing.T) {
	seedGatewayForwardingSettingsForTest()
	svc := &GatewayService{
		identityService: NewIdentityService(&identityCacheStub{}),
	}
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	c, ctx := newAnthropicForwardTestContext("/v1/messages", false)
	c.Request.Header.Set("User-Agent", "claude-cli/99.9.9 (external, sdk-cli)")
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","system":"Be terse","metadata":{"user_id":"{\"device_id\":\"fake-device\",\"account_uuid\":\"fake-acct\",\"session_id\":\"99999999-8888-4777-8666-555555555555\"}"},"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	out, err := svc.applyClaudeCodeOAuthMimicryToBody(ctx, c, account, body, "Be terse", "claude-3-7-sonnet-20250219")
	require.NoError(t, err)
	uidRaw := gjson.GetBytes(out, "metadata.user_id").String()
	parsedUID := ParseMetadataUserID(uidRaw)
	require.NotNil(t, parsedUID)
	require.Equal(t, "acct-uuid", parsedUID.AccountUUID)
	require.NotEqual(t, "fake-device", parsedUID.DeviceID)
	require.NotContains(t, string(out), "x-anthropic-billing-header")
}

func TestMimicry_MetadataFailClosedWhenIdentityUnavailable(t *testing.T) {
	upstream := &anthropicHTTPUpstreamRecorder{resp: newAnthropicSuccessResponse()}
	svc := &GatewayService{
		cfg:          &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}},
		httpUpstream: upstream,
	}
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	c, ctx := newAnthropicForwardTestContext("/v1/messages", false)
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","metadata":{"user_id":"{\"device_id\":\"fake-device\",\"account_uuid\":\"fake-acct\",\"session_id\":\"99999999-8888-4777-8666-555555555555\"}"},"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))
	require.Error(t, err)
	require.Contains(t, err.Error(), "generated metadata.user_id")
}

func TestOpenAICompatMimicry_MetadataFailClosedWhenIdentityUnavailable(t *testing.T) {
	svc := &GatewayService{}
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	c, ctx := newAnthropicForwardTestContext("/v1/messages", false)
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","system":"Be terse","metadata":{"user_id":"{\"device_id\":\"fake-device\",\"account_uuid\":\"fake-acct\",\"session_id\":\"99999999-8888-4777-8666-555555555555\"}"},"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	_, err := svc.applyClaudeCodeOAuthMimicryToBody(ctx, c, account, body, "Be terse", "claude-3-7-sonnet-20250319")
	require.Error(t, err)
	require.Contains(t, err.Error(), "generated metadata.user_id")
}

func TestApplyClaudeCodeOAuthMimicryToBody_UsesSafeDefaultFingerprintOnCacheMiss(t *testing.T) {
	cache := &identityCacheStub{}
	svc := &GatewayService{
		identityService: NewIdentityService(cache),
	}
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	c, ctx := newAnthropicForwardTestContext("/v1/messages", false)
	c.Request.Header.Set("User-Agent", "claude-cli/99.9.9 (external, sdk-cli)")
	c.Request.Header.Set("X-Stainless-OS", "Windows")
	c.Request.Header.Set("X-Stainless-Arch", "x64")
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","system":"Be terse","metadata":{"user_id":"{\"device_id\":\"fake-device\",\"account_uuid\":\"fake-acct\",\"session_id\":\"99999999-8888-4777-8666-555555555555\"}"},"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	out, err := svc.applyClaudeCodeOAuthMimicryToBody(ctx, c, account, body, "Be terse", "claude-3-7-sonnet-20250219")
	require.NoError(t, err)
	require.NotNil(t, cache.setFingerprint)
	require.Equal(t, defaultFingerprint.UserAgent, cache.setFingerprint.UserAgent)
	require.Equal(t, defaultFingerprint.StainlessOS, cache.setFingerprint.StainlessOS)
	require.Equal(t, defaultFingerprint.StainlessArch, cache.setFingerprint.StainlessArch)
	uidRaw := gjson.GetBytes(out, "metadata.user_id").String()
	parsedUID := ParseMetadataUserID(uidRaw)
	require.NotNil(t, parsedUID)
	require.Equal(t, cache.setFingerprint.ClientID, parsedUID.DeviceID)
}

func TestStrictPassthrough_CountTokensBodyBytesUnchangedAndNoAcceptEncoding(t *testing.T) {
	upstream := &anthropicHTTPUpstreamRecorder{resp: newAnthropicCountTokensSuccessResponse()}
	svc := newAnthropicForwardTestService(upstream)
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	c, ctx := newAnthropicForwardTestContext("/v1/messages/count_tokens", true)
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.145 (external, sdk-cli)")
	c.Request.Header.Set("Anthropic-Beta", "client-beta")
	c.Request.Header.Set("X-Claude-Code-Session-Id", "11111111-2222-4333-8444-555555555555")
	c.Request.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.145.abc; cc_entrypoint=sdk-cli; cch=12345;"}],"metadata":{"user_id":"{\"device_id\":\"client-device\",\"account_uuid\":\"acct-client\",\"session_id\":\"11111111-2222-4333-8444-555555555555\"}"},"messages":[{"role":"user","content":[{"type":"text","text":"hello"},{"type":"text","text":""}]}]}`)

	err := svc.ForwardCountTokens(ctx, c, account, parseAnthropicRequestForTest(t, body))
	require.NoError(t, err)
	require.True(t, bytes.Equal(body, upstream.lastBody), "strict count_tokens must preserve body bytes exactly")
	require.Empty(t, getHeaderRaw(upstream.lastReq.Header, "Accept-Encoding"))
}

func TestStrictPassthrough_CountTokensNoBodyMutatingRetry(t *testing.T) {
	upstream := &anthropicHTTPUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"Invalid signature in thinking block","type":"invalid_request_error"}}`)),
		},
	}
	svc := newAnthropicForwardTestService(upstream)
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	c, ctx := newAnthropicForwardTestContext("/v1/messages/count_tokens", true)
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.145 (external, sdk-cli)")
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","metadata":{"user_id":"{\"device_id\":\"client-device\",\"account_uuid\":\"acct-client\",\"session_id\":\"11111111-2222-4333-8444-555555555555\"}"},"messages":[{"role":"user","content":[{"type":"thinking","thinking":"secret","signature":"abc"},{"type":"text","text":"hello"}]}]}`)

	err := svc.ForwardCountTokens(ctx, c, account, parseAnthropicRequestForTest(t, body))
	require.Error(t, err)
	require.Equal(t, 1, upstream.requests, "strict count_tokens must not send a second mutated retry request")
	require.True(t, bytes.Equal(body, upstream.lastBody), "strict count_tokens retry path must not mutate body")
}

func TestCountTokensMimicry_OverridesFakeMetadataAndSetsSessionHeader(t *testing.T) {
	upstream := &anthropicHTTPUpstreamRecorder{resp: newAnthropicCountTokensSuccessResponse()}
	svc := newAnthropicForwardTestService(upstream)
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	c, ctx := newAnthropicForwardTestContext("/v1/messages/count_tokens", false)
	c.Request.Header.Set("User-Agent", "claude-cli/99.9.9 (external, sdk-cli)")
	c.Request.Header.Set("Anthropic-Beta", "suspicious-beta")
	c.Request.Header.Set("X-Stainless-Helper-Method", "stream")
	c.Request.Header.Set("X-Stainless-Lang", "attacker-lang")
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","metadata":{"user_id":"{\"device_id\":\"fake-device\",\"account_uuid\":\"fake-acct\",\"session_id\":\"99999999-8888-4777-8666-555555555555\"}"},"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	err := svc.ForwardCountTokens(ctx, c, account, parseAnthropicRequestForTest(t, body))
	require.NoError(t, err)
	require.Equal(t, strings.Join(claude.ClaudeCodeCountTokensOAuthBetas(), ","), getHeaderRaw(upstream.lastReq.Header, "anthropic-beta"))
	require.Empty(t, getHeaderRaw(upstream.lastReq.Header, "X-Stainless-Helper-Method"))
	require.Equal(t, claude.DefaultHeaders["X-Stainless-Lang"], getHeaderRaw(upstream.lastReq.Header, "X-Stainless-Lang"))

	uidRaw := gjson.GetBytes(upstream.lastBody, "metadata.user_id").String()
	parsedUID := ParseMetadataUserID(uidRaw)
	require.NotNil(t, parsedUID)
	require.Equal(t, "acct-uuid", parsedUID.AccountUUID)
	require.NotEqual(t, "fake-device", parsedUID.DeviceID)
	require.Equal(t, parsedUID.SessionID, getHeaderRaw(upstream.lastReq.Header, "X-Claude-Code-Session-Id"))
}
