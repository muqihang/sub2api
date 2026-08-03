package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func testAnthropicMessagesBody() []byte {
	return []byte(`{"model":"claude-3-7-sonnet-20250219","stream":false,"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)
}

func TestGatewayForward_AnthropicOAuthMissingInferenceScopeFailsClosedBeforeUpstream(t *testing.T) {
	upstream := &anthropicHTTPUpstreamRecorder{resp: newAnthropicSuccessResponse()}
	svc := newAnthropicForwardTestService(upstream)
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	account.Credentials["scope"] = "org:create_api_key user:file_upload user:profile"
	c, ctx := newAnthropicForwardTestContext("/v1/messages", false)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, testAnthropicMessagesBody()))

	require.Error(t, err)
	require.Contains(t, err.Error(), "inference_scope_missing")
	require.Zero(t, upstream.requests, "missing user:inference must fail closed before upstream/CC Gateway")
	require.Equal(t, 403, c.Writer.Status())
}

func TestGatewayForward_AnthropicSetupTokenMissingInferenceScopeFailsClosedBeforeUpstream(t *testing.T) {
	upstream := &anthropicHTTPUpstreamRecorder{resp: newAnthropicSuccessResponse()}
	svc := newAnthropicForwardTestService(upstream)
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	account.Type = AccountTypeSetupToken
	account.Credentials["scope"] = "user:profile"
	c, ctx := newAnthropicForwardTestContext("/v1/messages", false)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, testAnthropicMessagesBody()))

	require.Error(t, err)
	require.Contains(t, err.Error(), "inference_scope_missing")
	require.Zero(t, upstream.requests, "setup-token without user:inference must fail closed before upstream/CC Gateway")
	require.Equal(t, 403, c.Writer.Status())
}

func TestGatewayForward_AnthropicOAuthMissingInferenceScopeFailsClosedBeforeCCGateway(t *testing.T) {
	upstream := &ccGatewayBoundaryUpstreamRecorder{resp: newAnthropicSuccessResponse()}
	svc := newCCGatewayBoundaryService(upstream)
	account := newCCGatewayBoundaryAccount()
	account.Credentials["scope"] = "org:create_api_key user:file_upload user:profile"
	c, ctx := newCCGatewayBoundaryContext("/v1/messages")

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, testAnthropicMessagesBody()))

	require.Error(t, err)
	require.Contains(t, err.Error(), "inference_scope_missing")
	require.Zero(t, upstream.requests, "missing user:inference must fail closed before CC Gateway transport")
	require.Equal(t, 403, c.Writer.Status())
}

func TestGatewayForward_AnthropicOAuthInferenceScopeAllowsLocalMockForward(t *testing.T) {
	upstream := &anthropicHTTPUpstreamRecorder{resp: newAnthropicSuccessResponse()}
	svc := newAnthropicForwardTestService(upstream)
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	account.Credentials["scope"] = "user:profile user:file_upload user:inference"
	c, ctx := newAnthropicForwardTestContext("/v1/messages", false)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, testAnthropicMessagesBody()))

	require.NoError(t, err)
	require.Equal(t, 1, upstream.requests)
}

func TestGatewayForward_AnthropicOAuthInferenceScopeOrderInsensitive(t *testing.T) {
	upstream := &anthropicHTTPUpstreamRecorder{resp: newAnthropicSuccessResponse()}
	svc := newAnthropicForwardTestService(upstream)
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	account.Credentials["scope"] = "user:file_upload user:inference org:create_api_key user:profile"
	c, ctx := newAnthropicForwardTestContext("/v1/messages", false)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, testAnthropicMessagesBody()))

	require.NoError(t, err)
	require.Equal(t, 1, upstream.requests)
}

func TestGatewayForward_AnthropicOAuthEmptyOrMissingScopeFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name  string
		scope any
		set   bool
	}{
		{name: "missing", set: false},
		{name: "empty", set: true, scope: ""},
		{name: "whitespace", set: true, scope: "   \t\n"},
		{name: "non_string", set: true, scope: []string{"user:inference"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &anthropicHTTPUpstreamRecorder{resp: newAnthropicSuccessResponse()}
			svc := newAnthropicForwardTestService(upstream)
			account := newAnthropicOAuthAccountForClaudeForwardTest()
			if tc.set {
				account.Credentials["scope"] = tc.scope
			} else {
				delete(account.Credentials, "scope")
			}
			c, ctx := newAnthropicForwardTestContext("/v1/messages", false)

			_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, testAnthropicMessagesBody()))

			require.Error(t, err)
			require.Contains(t, err.Error(), "inference_scope_missing")
			require.Zero(t, upstream.requests)
		})
	}
}

func TestGatewayForward_AnthropicAPIKeyDoesNotRequireOAuthInferenceScope(t *testing.T) {
	upstream := &anthropicHTTPUpstreamRecorder{resp: newAnthropicSuccessResponse()}
	svc := newAnthropicForwardTestService(upstream)
	account := newAnthropicAPIKeyAccountForTest()
	delete(account.Credentials, "scope")
	c, ctx := newAnthropicForwardTestContext("/v1/messages", false)

	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, testAnthropicMessagesBody()))

	require.NoError(t, err)
	require.Equal(t, 1, upstream.requests)
}

func TestAnthropicMessagesInferenceScopeGate_DoesNotApplyToNonAnthropicOrAPIKeyAccounts(t *testing.T) {
	for _, account := range []*Account{
		{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"scope": ""}},
		{Platform: PlatformGemini, Type: AccountTypeOAuth, Credentials: map[string]any{"scope": ""}},
		{Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Credentials: map[string]any{}},
	} {
		err := validateAnthropicMessagesInferenceScope(account)
		require.NoError(t, err, "scope gate should not apply to platform/type %s/%s", account.Platform, account.Type)
	}
}

func TestAnthropicMessagesInferenceScopeGate_ErrorCodeStable(t *testing.T) {
	err := validateAnthropicMessagesInferenceScope(&Account{
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{"scope": "user:profile"},
	})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "inference_scope_missing"))
	require.True(t, strings.Contains(err.Error(), "user:inference"))
	require.False(t, strings.Contains(err.Error(), "user:profile user:inference"), "error must not echo full raw scope lists")
}

func TestGatewayForward_AnthropicOAuthScopeGateUsesNoNetworkOnBackgroundContext(t *testing.T) {
	upstream := &anthropicHTTPUpstreamRecorder{resp: newAnthropicSuccessResponse()}
	svc := newAnthropicForwardTestService(upstream)
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	account.Credentials["scope"] = "user:profile"
	c, _ := newAnthropicForwardTestContext("/v1/messages", false)

	_, err := svc.Forward(context.Background(), c, account, parseAnthropicRequestForTest(t, testAnthropicMessagesBody()))

	require.Error(t, err)
	require.Zero(t, upstream.requests)
}
