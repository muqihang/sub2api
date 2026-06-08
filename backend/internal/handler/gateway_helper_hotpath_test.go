package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type helperConcurrencyCacheStub struct {
	mu sync.Mutex

	accountSeq []bool
	userSeq    []bool

	accountAcquireCalls int
	userAcquireCalls    int
	accountReleaseCalls int
	userReleaseCalls    int
}

func (s *helperConcurrencyCacheStub) AcquireAccountSlot(ctx context.Context, accountID int64, maxConcurrency int, requestID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accountAcquireCalls++
	if len(s.accountSeq) == 0 {
		return false, nil
	}
	v := s.accountSeq[0]
	s.accountSeq = s.accountSeq[1:]
	return v, nil
}

func (s *helperConcurrencyCacheStub) ReleaseAccountSlot(ctx context.Context, accountID int64, requestID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accountReleaseCalls++
	return nil
}

func (s *helperConcurrencyCacheStub) GetAccountConcurrency(ctx context.Context, accountID int64) (int, error) {
	return 0, nil
}

func (s *helperConcurrencyCacheStub) GetAccountConcurrencyBatch(ctx context.Context, accountIDs []int64) (map[int64]int, error) {
	out := make(map[int64]int, len(accountIDs))
	for _, accountID := range accountIDs {
		out[accountID] = 0
	}
	return out, nil
}

func (s *helperConcurrencyCacheStub) IncrementAccountWaitCount(ctx context.Context, accountID int64, maxWait int) (bool, error) {
	return true, nil
}

func (s *helperConcurrencyCacheStub) DecrementAccountWaitCount(ctx context.Context, accountID int64) error {
	return nil
}

func (s *helperConcurrencyCacheStub) GetAccountWaitingCount(ctx context.Context, accountID int64) (int, error) {
	return 0, nil
}

func (s *helperConcurrencyCacheStub) AcquireUserSlot(ctx context.Context, userID int64, maxConcurrency int, requestID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.userAcquireCalls++
	if len(s.userSeq) == 0 {
		return false, nil
	}
	v := s.userSeq[0]
	s.userSeq = s.userSeq[1:]
	return v, nil
}

func (s *helperConcurrencyCacheStub) ReleaseUserSlot(ctx context.Context, userID int64, requestID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.userReleaseCalls++
	return nil
}

func (s *helperConcurrencyCacheStub) GetUserConcurrency(ctx context.Context, userID int64) (int, error) {
	return 0, nil
}

func (s *helperConcurrencyCacheStub) IncrementWaitCount(ctx context.Context, userID int64, maxWait int) (bool, error) {
	return true, nil
}

func (s *helperConcurrencyCacheStub) DecrementWaitCount(ctx context.Context, userID int64) error {
	return nil
}

func (s *helperConcurrencyCacheStub) GetAccountsLoadBatch(ctx context.Context, accounts []service.AccountWithConcurrency) (map[int64]*service.AccountLoadInfo, error) {
	out := make(map[int64]*service.AccountLoadInfo, len(accounts))
	for _, acc := range accounts {
		out[acc.ID] = &service.AccountLoadInfo{AccountID: acc.ID}
	}
	return out, nil
}

func (s *helperConcurrencyCacheStub) GetUsersLoadBatch(ctx context.Context, users []service.UserWithConcurrency) (map[int64]*service.UserLoadInfo, error) {
	out := make(map[int64]*service.UserLoadInfo, len(users))
	for _, user := range users {
		out[user.ID] = &service.UserLoadInfo{UserID: user.ID}
	}
	return out, nil
}

func (s *helperConcurrencyCacheStub) CleanupExpiredAccountSlots(ctx context.Context, accountID int64) error {
	return nil
}

func (s *helperConcurrencyCacheStub) CleanupStaleProcessSlots(ctx context.Context, activeRequestPrefix string) error {
	return nil
}

func newHelperTestContext(method, path string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(method, path, nil)
	return c, rec
}

func validClaudeCodeBodyJSON() []byte {
	return []byte(`{
		"model":"claude-3-5-sonnet-20241022",
		"system":[{"text":"You are Claude Code, Anthropic's official CLI for Claude."}],
		"metadata":{"user_id":"user_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa_account__session_aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"}
	}`)
}

func TestSetClaudeCodeClientContext_FastPathAndStrictPath(t *testing.T) {
	t.Run("non_cli_user_agent_sets_false", func(t *testing.T) {
		c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")
		c.Request.Header.Set("User-Agent", "curl/8.6.0")

		SetClaudeCodeClientContext(c, validClaudeCodeBodyJSON(), nil)
		require.False(t, service.IsClaudeCodeClient(c.Request.Context()))
	})

	t.Run("cli_non_messages_path_sets_true", func(t *testing.T) {
		c, _ := newHelperTestContext(http.MethodGet, "/v1/models")
		c.Request.Header.Set("User-Agent", "claude-cli/1.0.1")

		SetClaudeCodeClientContext(c, nil, nil)
		require.True(t, service.IsClaudeCodeClient(c.Request.Context()))
	})

	t.Run("cli_messages_path_valid_body_sets_true", func(t *testing.T) {
		c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")
		c.Request.Header.Set("User-Agent", "claude-cli/1.0.1")
		c.Request.Header.Set("X-App", "claude-code")
		c.Request.Header.Set("anthropic-beta", "message-batches-2024-09-24")
		c.Request.Header.Set("anthropic-version", "2023-06-01")

		SetClaudeCodeClientContext(c, validClaudeCodeBodyJSON(), nil)
		require.True(t, service.IsClaudeCodeClient(c.Request.Context()))
	})

	t.Run("cli_messages_path_invalid_body_sets_false", func(t *testing.T) {
		c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")
		c.Request.Header.Set("User-Agent", "claude-cli/1.0.1")
		// 缺少严格校验所需 header + body 字段
		SetClaudeCodeClientContext(c, []byte(`{"model":"x"}`), nil)
		require.False(t, service.IsClaudeCodeClient(c.Request.Context()))
	})
}

func TestSetClaudeCodeClientContext_ReuseParsedRequest(t *testing.T) {
	t.Run("reuse parsed request without body unmarshal", func(t *testing.T) {
		c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")
		c.Request.Header.Set("User-Agent", "claude-cli/1.0.1")
		c.Request.Header.Set("X-App", "claude-code")
		c.Request.Header.Set("anthropic-beta", "message-batches-2024-09-24")
		c.Request.Header.Set("anthropic-version", "2023-06-01")

		parsedReq, err := service.ParseGatewayRequest(service.NewRequestBodyRef(validClaudeCodeBodyJSON()), "")
		require.NoError(t, err)

		// body 非法 JSON，如果函数复用 parsedReq 成功则仍应判定为 Claude Code。
		SetClaudeCodeClientContext(c, []byte(`{invalid`), parsedReq)
		require.True(t, service.IsClaudeCodeClient(c.Request.Context()))
	})
}

func TestSetClaudeCodeClientContext_ClaudeCodePromptWithoutMetadataSetsFalse(t *testing.T) {
	c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")
	c.Request.Header.Set("User-Agent", "Claude Code/2.1.161")
	body := []byte(`{
		"model":"claude-3-5-sonnet-20241022",
		"system":[{"text":"You are Claude Code, Anthropic's official CLI for Claude."}]
	}`)

	SetClaudeCodeClientContext(c, body, nil)
	require.False(t, service.IsClaudeCodeClient(c.Request.Context()))
}

func TestSetClaudeCodeClientContext_ClaudeCodePromptWithInvalidMetadataSetsFalse(t *testing.T) {
	c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")
	c.Request.Header.Set("User-Agent", "Claude Code/2.1.161")
	body := []byte(`{
		"model":"claude-3-5-sonnet-20241022",
		"system":[{"text":"You are Claude Code, Anthropic's official CLI for Claude."}],
		"metadata":{"user_id":"invalid-format"}
	}`)

	SetClaudeCodeClientContext(c, body, nil)
	require.False(t, service.IsClaudeCodeClient(c.Request.Context()))
}

func TestSetClaudeCodeClientContext_BroaderOfficialUAAndVersion(t *testing.T) {
	tests := []struct {
		name        string
		ua          string
		wantVersion string
	}{
		{"claude cli current", "claude-cli/2.1.161 (external, sdk-cli)", "2.1.161"},
		{"claude code spaced", "Claude Code/2.1.161", "2.1.161"},
		{"claude code hyphenated", "claude-code/2.1.161", "2.1.161"},
		{"claudecode compact", "ClaudeCode/2.1.161", "2.1.161"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")
			c.Request.Header.Set("User-Agent", tt.ua)

			SetClaudeCodeClientContext(c, validClaudeCodeBodyJSON(), nil)
			require.True(t, service.IsClaudeCodeClient(c.Request.Context()))
			require.Equal(t, tt.wantVersion, service.GetClaudeCodeVersion(c.Request.Context()))
		})
	}
}

func TestWaitForSlotWithPingTimeout_AccountAndUserAcquire(t *testing.T) {
	cache := &helperConcurrencyCacheStub{
		accountSeq: []bool{false, true},
		userSeq:    []bool{false, true},
	}
	concurrency := service.NewConcurrencyService(cache)
	helper := NewConcurrencyHelper(concurrency, SSEPingFormatNone, 5*time.Millisecond)

	t.Run("account_slot_acquired_after_retry", func(t *testing.T) {
		c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")
		streamStarted := false
		release, err := helper.waitForSlotWithPingTimeout(c, "account", 101, 2, time.Second, false, &streamStarted, true)
		require.NoError(t, err)
		require.NotNil(t, release)
		require.False(t, streamStarted)
		release()
		require.GreaterOrEqual(t, cache.accountAcquireCalls, 2)
		require.GreaterOrEqual(t, cache.accountReleaseCalls, 1)
	})

	t.Run("user_slot_acquired_after_retry", func(t *testing.T) {
		c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")
		streamStarted := false
		release, err := helper.waitForSlotWithPingTimeout(c, "user", 202, 3, time.Second, false, &streamStarted, true)
		require.NoError(t, err)
		require.NotNil(t, release)
		release()
		require.GreaterOrEqual(t, cache.userAcquireCalls, 2)
		require.GreaterOrEqual(t, cache.userReleaseCalls, 1)
	})
}

func TestWaitForSlotWithPingTimeout_TimeoutAndStreamPing(t *testing.T) {
	cache := &helperConcurrencyCacheStub{
		accountSeq: []bool{false, false, false},
	}
	concurrency := service.NewConcurrencyService(cache)

	t.Run("timeout_returns_concurrency_error", func(t *testing.T) {
		helper := NewConcurrencyHelper(concurrency, SSEPingFormatNone, 5*time.Millisecond)
		c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")
		streamStarted := false
		release, err := helper.waitForSlotWithPingTimeout(c, "account", 101, 2, 130*time.Millisecond, false, &streamStarted, true)
		require.Nil(t, release)
		var cErr *ConcurrencyError
		require.ErrorAs(t, err, &cErr)
		require.True(t, cErr.IsTimeout)
	})

	t.Run("stream_mode_sends_ping_before_timeout", func(t *testing.T) {
		helper := NewConcurrencyHelper(concurrency, SSEPingFormatComment, 10*time.Millisecond)
		c, rec := newHelperTestContext(http.MethodPost, "/v1/messages")
		streamStarted := false
		release, err := helper.waitForSlotWithPingTimeout(c, "account", 101, 2, 70*time.Millisecond, true, &streamStarted, true)
		require.Nil(t, release)
		var cErr *ConcurrencyError
		require.ErrorAs(t, err, &cErr)
		require.True(t, cErr.IsTimeout)
		require.True(t, streamStarted)
		require.Contains(t, rec.Body.String(), ":\n\n")
	})
}

func TestWaitForSlotWithPingTimeout_ParentContextCanceled(t *testing.T) {
	cache := &helperConcurrencyCacheStub{
		accountSeq: []bool{false},
	}
	concurrency := service.NewConcurrencyService(cache)
	helper := NewConcurrencyHelper(concurrency, SSEPingFormatNone, 5*time.Millisecond)
	c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")
	reqCtx, cancel := context.WithCancel(c.Request.Context())
	c.Request = c.Request.WithContext(reqCtx)
	cancel()

	streamStarted := false
	release, err := helper.waitForSlotWithPingTimeout(c, "account", 101, 2, time.Second, false, &streamStarted, true)
	require.Nil(t, release)
	require.ErrorIs(t, err, context.Canceled)
	var cErr *ConcurrencyError
	require.False(t, errors.As(err, &cErr))
}

func TestWaitForSlotWithPingTimeout_AcquireError(t *testing.T) {
	errCache := &helperConcurrencyCacheStubWithError{
		err: errors.New("redis unavailable"),
	}
	concurrency := service.NewConcurrencyService(errCache)
	helper := NewConcurrencyHelper(concurrency, SSEPingFormatNone, 5*time.Millisecond)
	c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")
	streamStarted := false
	release, err := helper.waitForSlotWithPingTimeout(c, "account", 1, 1, 200*time.Millisecond, false, &streamStarted, true)
	require.Nil(t, release)
	require.Error(t, err)
	require.Contains(t, err.Error(), "redis unavailable")
}

func TestAcquireAccountSlotWithWaitTimeout_ImmediateAttemptBeforeBackoff(t *testing.T) {
	cache := &helperConcurrencyCacheStub{
		accountSeq: []bool{false},
	}
	concurrency := service.NewConcurrencyService(cache)
	helper := NewConcurrencyHelper(concurrency, SSEPingFormatNone, 5*time.Millisecond)
	c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")
	streamStarted := false

	release, err := helper.AcquireAccountSlotWithWaitTimeout(c, 301, 1, 30*time.Millisecond, false, &streamStarted)
	require.Nil(t, release)
	var cErr *ConcurrencyError
	require.ErrorAs(t, err, &cErr)
	require.True(t, cErr.IsTimeout)
	require.GreaterOrEqual(t, cache.accountAcquireCalls, 1)
}

type helperConcurrencyCacheStubWithError struct {
	helperConcurrencyCacheStub
	err error
}

func (s *helperConcurrencyCacheStubWithError) AcquireAccountSlot(ctx context.Context, accountID int64, maxConcurrency int, requestID string) (bool, error) {
	return false, s.err
}

func TestForceAnthropicCompatNonNativePreservesOfficialClaudeCodeUA(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"claude-sonnet-4-6","metadata":{"user_id":"{\"device_id\":\"abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd\",\"session_id\":\"123e4567-e89b-42d3-a456-426614174000\"}"},"system":[{"type":"text","text":"You are Claude Code, Anthropic's official CLI for Claude."}],"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.150 (external, sdk-cli)")

	SetClaudeCodeClientContext(c, body, nil)
	require.True(t, service.IsClaudeCodeClient(c.Request.Context()))
	require.Equal(t, "2.1.150", service.GetClaudeCodeVersion(c.Request.Context()))

	decision := service.AnthropicCompatIngressDecision{InboundRoute: service.AnthropicCompatInboundMessages, CCGatewayRoute: service.AnthropicCompatCCGatewayMessages, ClientType: service.AnthropicCompatClientType}
	ctx := service.WithAnthropicCompatAuditSummary(c.Request.Context(), service.NewAnthropicCompatAuditSummary(decision))
	c.Request = c.Request.WithContext(ctx)
	forceAnthropicCompatNonNative(c)

	require.True(t, service.IsClaudeCodeClient(c.Request.Context()))
	require.Equal(t, "2.1.150", service.GetClaudeCodeVersion(c.Request.Context()))
}

func TestSetClaudeCodeClientContextPreservesClaudeCodeToolShapeFromProxyUAWithoutBeta(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"claude-opus-4-8","tools":[{"name":"Bash","input_schema":{"type":"object"}},{"name":"Read","input_schema":{"type":"object"}},{"name":"Grep","input_schema":{"type":"object"}},{"name":"Glob","input_schema":{"type":"object"}},{"name":"TodoWrite","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))
	c.Request.Header.Set("User-Agent", "Go-http-client/1.1")
	c.Request.Header.Set("anthropic-version", "2023-06-01")
	decision := service.AnthropicCompatIngressDecision{InboundRoute: service.AnthropicCompatInboundMessages, CCGatewayRoute: service.AnthropicCompatCCGatewayMessages, ClientType: service.AnthropicCompatClientType}
	ctx := service.WithAnthropicCompatAuditSummary(c.Request.Context(), service.NewAnthropicCompatAuditSummary(decision))
	c.Request = c.Request.WithContext(ctx)

	SetClaudeCodeClientContext(c, body, nil)
	forceAnthropicCompatNonNative(c)

	require.True(t, service.IsClaudeCodeClient(c.Request.Context()))
}

func TestSetClaudeCodeClientContextPreservesLargeForwardedToolSetWithoutKnownNames(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"claude-sonnet-4-6","tools":[{"name":"tool_01"},{"name":"tool_02"},{"name":"tool_03"},{"name":"tool_04"},{"name":"tool_05"},{"name":"tool_06"},{"name":"tool_07"},{"name":"tool_08"},{"name":"tool_09"},{"name":"tool_10"}],"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))
	c.Request.Header.Set("User-Agent", "Go-http-client/1.1")
	c.Request.Header.Set("anthropic-version", "2023-06-01")
	decision := service.AnthropicCompatIngressDecision{InboundRoute: service.AnthropicCompatInboundMessages, CCGatewayRoute: service.AnthropicCompatCCGatewayMessages, ClientType: service.AnthropicCompatClientType}
	ctx := service.WithAnthropicCompatAuditSummary(c.Request.Context(), service.NewAnthropicCompatAuditSummary(decision))
	c.Request = c.Request.WithContext(ctx)

	SetClaudeCodeClientContext(c, body, nil)
	forceAnthropicCompatNonNative(c)

	require.True(t, service.IsClaudeCodeClient(c.Request.Context()))
}

func TestSetClaudeCodeClientContextDowngradesProxyUAWithoutClaudeCodeToolShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"claude-opus-4-8","tools":[{"name":"lookup","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))
	c.Request.Header.Set("User-Agent", "Go-http-client/1.1")
	c.Request.Header.Set("anthropic-version", "2023-06-01")
	decision := service.AnthropicCompatIngressDecision{InboundRoute: service.AnthropicCompatInboundMessages, CCGatewayRoute: service.AnthropicCompatCCGatewayMessages, ClientType: service.AnthropicCompatClientType}
	ctx := service.WithAnthropicCompatAuditSummary(c.Request.Context(), service.NewAnthropicCompatAuditSummary(decision))
	ctx = service.SetClaudeCodeClient(ctx, true)
	c.Request = c.Request.WithContext(ctx)

	SetClaudeCodeClientContext(c, body, nil)
	forceAnthropicCompatNonNative(c)

	require.False(t, service.IsClaudeCodeClient(c.Request.Context()))
}

func TestSetClaudeCodeClientContextPreservesClaudeCodeCompatBetaFromProxyUA(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"claude-opus-4-8","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))
	c.Request.Header.Set("User-Agent", "Go-http-client/2.0")
	c.Request.Header.Set("anthropic-beta", "claude-code-20250219,interleaved-thinking-2025-05-14,mid-conversation-system-2026-04-07,effort-2025-11-24,oauth-2025-04-20")
	c.Request.Header.Set("anthropic-version", "2023-06-01")

	decision := service.AnthropicCompatIngressDecision{InboundRoute: service.AnthropicCompatInboundMessages, CCGatewayRoute: service.AnthropicCompatCCGatewayMessages, ClientType: service.AnthropicCompatClientType}
	ctx := service.WithAnthropicCompatAuditSummary(c.Request.Context(), service.NewAnthropicCompatAuditSummary(decision))
	ctx = service.SetClaudeCodeClient(ctx, true)
	c.Request = c.Request.WithContext(ctx)

	SetClaudeCodeClientContext(c, body, nil)

	require.True(t, service.IsClaudeCodeClient(c.Request.Context()))
}

func TestForceAnthropicCompatNonNativePreservesClaudeCodeCompatBetaFromProxyUA(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-opus-4-8","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`))
	c.Request.Header.Set("User-Agent", "Go-http-client/2.0")
	c.Request.Header.Set("anthropic-beta", "claude-code-20250219,interleaved-thinking-2025-05-14,mid-conversation-system-2026-04-07,effort-2025-11-24,oauth-2025-04-20")
	c.Request.Header.Set("anthropic-version", "2023-06-01")
	c.Request = c.Request.WithContext(service.SetClaudeCodeClient(c.Request.Context(), true))

	decision := service.AnthropicCompatIngressDecision{InboundRoute: service.AnthropicCompatInboundMessages, CCGatewayRoute: service.AnthropicCompatCCGatewayMessages, ClientType: service.AnthropicCompatClientType}
	ctx := service.WithAnthropicCompatAuditSummary(c.Request.Context(), service.NewAnthropicCompatAuditSummary(decision))
	c.Request = c.Request.WithContext(ctx)
	forceAnthropicCompatNonNative(c)

	require.True(t, service.IsClaudeCodeClient(c.Request.Context()))
}

func TestSetClaudeCodeClientContextDowngradesProxyUAWithoutCompatAudit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"claude-opus-4-8","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))
	c.Request.Header.Set("User-Agent", "Go-http-client/2.0")
	c.Request.Header.Set("anthropic-beta", "claude-code-20250219,interleaved-thinking-2025-05-14")
	c.Request = c.Request.WithContext(service.SetClaudeCodeClient(c.Request.Context(), true))

	SetClaudeCodeClientContext(c, body, nil)

	require.False(t, service.IsClaudeCodeClient(c.Request.Context()))
}

func TestSetClaudeCodeClientContextDowngradesProxyUAWithoutExactClaudeCodeBeta(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"claude-opus-4-8","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))
	c.Request.Header.Set("User-Agent", "Go-http-client/2.0")
	c.Request.Header.Set("anthropic-beta", "not-claude-code-20250219,claude-code-20250219x,interleaved-thinking-2025-05-14")
	decision := service.AnthropicCompatIngressDecision{InboundRoute: service.AnthropicCompatInboundMessages, CCGatewayRoute: service.AnthropicCompatCCGatewayMessages, ClientType: service.AnthropicCompatClientType}
	ctx := service.WithAnthropicCompatAuditSummary(c.Request.Context(), service.NewAnthropicCompatAuditSummary(decision))
	ctx = service.SetClaudeCodeClient(ctx, true)
	c.Request = c.Request.WithContext(ctx)

	SetClaudeCodeClientContext(c, body, nil)

	require.False(t, service.IsClaudeCodeClient(c.Request.Context()))
}

func TestForceAnthropicCompatNonNativeStillDowngradesNonOfficialUA(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`))
	c.Request.Header.Set("User-Agent", "Go-http-client/2.0")
	c.Request = c.Request.WithContext(service.SetClaudeCodeClient(c.Request.Context(), true))
	c.Request = c.Request.WithContext(service.SetClaudeCodeVersion(c.Request.Context(), "2.1.150"))

	decision := service.AnthropicCompatIngressDecision{InboundRoute: service.AnthropicCompatInboundMessages, CCGatewayRoute: service.AnthropicCompatCCGatewayMessages, ClientType: service.AnthropicCompatClientType}
	ctx := service.WithAnthropicCompatAuditSummary(c.Request.Context(), service.NewAnthropicCompatAuditSummary(decision))
	c.Request = c.Request.WithContext(ctx)
	forceAnthropicCompatNonNative(c)

	require.False(t, service.IsClaudeCodeClient(c.Request.Context()))
	require.Empty(t, service.GetClaudeCodeVersion(c.Request.Context()))
}
