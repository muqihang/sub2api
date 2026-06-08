package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newClaudePersonaTestContext(path string) *gin.Context {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	return c
}

func newClaudePersonaTestService() *GatewayService {
	return &GatewayService{
		cfg:             &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}},
		identityService: NewIdentityService(&identityCacheStub{}),
	}
}

func newClaudePersonaOAuthAccount() *Account {
	return &Account{
		ID:       42,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"account_uuid": "acct-uuid",
		},
	}
}

func TestGatewayServiceGetBetaHeader_DefaultsToOAuthMessagesBetas(t *testing.T) {
	svc := &GatewayService{}
	want := strings.Join(claude.ClaudeCodeMessagesOAuthBetas(), ",")

	require.Equal(t, want, svc.getBetaHeader("claude-sonnet-4-5-20250929", ""))
}

func TestGatewayServiceGetBetaHeader_AppendsOAuthAtTail(t *testing.T) {
	svc := &GatewayService{}
	incoming := strings.Join([]string{
		claude.BetaClaudeCode,
		claude.BetaInterleavedThinking,
		claude.BetaContextManagement,
		claude.BetaPromptCachingScope,
	}, ",")
	want := strings.Join(claude.ClaudeCodeMessagesOAuthBetas(), ",")

	require.Equal(t, want, svc.getBetaHeader("claude-sonnet-4-5-20250929", incoming))
}

func TestGatewayServiceGetBetaHeader_AppendsOAuthWhenClaudeCodeBetaMissing(t *testing.T) {
	svc := &GatewayService{}
	incoming := strings.Join([]string{
		claude.BetaInterleavedThinking,
		claude.BetaContextManagement,
	}, ",")
	want := incoming + "," + claude.BetaOAuth

	require.Equal(t, want, svc.getBetaHeader("claude-sonnet-4-5-20250929", incoming))
}

func TestApplyClaudeCodeMimicHeaders_UsesUpdatedPersonaAndNoHelperMethod(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
	req.Header.Set("User-Agent", "third-party/1.0")
	req.Header.Set("X-Stainless-OS", "MacOS")
	req.Header.Set("X-Stainless-Arch", "x64")

	applyClaudeCodeMimicHeaders(req, true)

	require.Equal(t, claude.DefaultHeaders["User-Agent"], getHeaderRaw(req.Header, "User-Agent"))
	require.Equal(t, "application/json", getHeaderRaw(req.Header, "Accept"))
	require.Equal(t, "MacOS", getHeaderRaw(req.Header, "X-Stainless-OS"))
	require.Equal(t, "x64", getHeaderRaw(req.Header, "X-Stainless-Arch"))
	require.Equal(t, "600", getHeaderRaw(req.Header, "X-Stainless-Timeout"))
	require.Empty(t, getHeaderRaw(req.Header, "x-stainless-helper-method"))
	require.NotEmpty(t, getHeaderRaw(req.Header, "x-client-request-id"))
}

func TestApplyClaudeCodeMimicHeaders_SetsNonStreamingTimeout(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)

	applyClaudeCodeMimicHeaders(req, false)

	require.Equal(t, "300", getHeaderRaw(req.Header, "X-Stainless-Timeout"))
}

func TestBuildUpstreamRequest_MimicUsesMessagesOAuthBetas(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{
		{
			name: "short body uses structured outputs beta",
			body: []byte(`{"model":"claude-3-7-sonnet-20250219","metadata":{"user_id":"{\"device_id\":\"fake-device\",\"account_uuid\":\"fake-acct\",\"session_id\":\"123e4567-e89b-12d3-a456-426614174000\"}"},"messages":[]}`),
		},
		{
			name: "main body uses extended cache ttl beta",
			body: []byte(`{"model":"claude-3-7-sonnet-20250219","metadata":{"user_id":"{\"device_id\":\"fake-device\",\"account_uuid\":\"fake-acct\",\"session_id\":\"123e4567-e89b-12d3-a456-426614174000\"}"},"thinking":{"type":"enabled","budget_tokens":1024},"context_management":{"edits":[]},"messages":[]}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newClaudePersonaTestService()
			account := newClaudePersonaOAuthAccount()
			ctx := newClaudePersonaTestContext("/v1/messages")

			req, _, err := svc.buildUpstreamRequest(
				context.Background(),
				ctx,
				account,
				tt.body,
				"oauth-token",
				"oauth",
				"claude-3-7-sonnet-20250219",
				true,
				true,
				false,
			)
			require.NoError(t, err)
			require.Equal(t, strings.Join(claude.ClaudeCodeMessagesOAuthBetasForBody(tt.body), ","), getHeaderRaw(req.Header, "anthropic-beta"))
			require.Empty(t, getHeaderRaw(req.Header, "x-stainless-helper-method"))
		})
	}
}

func TestBuildCountTokensRequest_MimicUsesCountTokensOAuthBetas(t *testing.T) {
	svc := newClaudePersonaTestService()
	account := newClaudePersonaOAuthAccount()
	ctx := newClaudePersonaTestContext("/v1/messages/count_tokens")

	req, _, err := svc.buildCountTokensRequest(
		context.Background(),
		ctx,
		account,
		[]byte(`{"model":"claude-3-7-sonnet-20250219","metadata":{"user_id":"{\"device_id\":\"fake-device\",\"account_uuid\":\"fake-acct\",\"session_id\":\"123e4567-e89b-12d3-a456-426614174000\"}"},"messages":[]}`),
		"oauth-token",
		"oauth",
		"claude-3-7-sonnet-20250219",
		true,
		false,
	)
	require.NoError(t, err)
	beta := getHeaderRaw(req.Header, "anthropic-beta")
	for _, token := range append(claude.FullClaudeCodeMimicryBetas(), claude.BetaTokenCounting) {
		require.Contains(t, beta, token)
	}
	require.Empty(t, getHeaderRaw(req.Header, "x-stainless-helper-method"))
}
