package service

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type blockerRegressionTokenCache struct {
	mu           sync.Mutex
	tokens       map[string]string
	getErr       error
	deleteCalled int32
}

func newBlockerRegressionTokenCache() *blockerRegressionTokenCache {
	return &blockerRegressionTokenCache{tokens: map[string]string{}}
}

func (c *blockerRegressionTokenCache) GetAccessToken(context.Context, string) (string, error) {
	if c.getErr != nil {
		return "", c.getErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tokens["openai:account:9001"], nil
}
func (c *blockerRegressionTokenCache) SetAccessToken(context.Context, string, string, time.Duration) error {
	return nil
}
func (c *blockerRegressionTokenCache) DeleteAccessToken(context.Context, string) error {
	atomic.AddInt32(&c.deleteCalled, 1)
	return nil
}
func (c *blockerRegressionTokenCache) AcquireRefreshLock(context.Context, string, time.Duration) (bool, error) {
	return true, nil
}
func (c *blockerRegressionTokenCache) ReleaseRefreshLock(context.Context, string) error { return nil }

type blockerRegressionAccountRepo struct {
	AccountRepository
	account       *Account
	setErrorCalls int
	tempCalls     int
	updateExtra   map[string]any
}

func (r *blockerRegressionAccountRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}
func (r *blockerRegressionAccountRepo) UpdateCredentials(_ context.Context, _ int64, credentials map[string]any) error {
	if r.account != nil {
		r.account.Credentials = cloneCredentials(credentials)
	}
	return nil
}
func (r *blockerRegressionAccountRepo) SetError(context.Context, int64, string) error {
	r.setErrorCalls++
	return nil
}
func (r *blockerRegressionAccountRepo) SetTempUnschedulable(context.Context, int64, time.Time, string) error {
	r.tempCalls++
	return nil
}
func (r *blockerRegressionAccountRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.updateExtra = cloneCredentials(updates)
	return nil
}

type blockerRegressionRefreshExecutor struct{ err error }

func (e *blockerRegressionRefreshExecutor) CanRefresh(*Account) bool                  { return true }
func (e *blockerRegressionRefreshExecutor) NeedsRefresh(*Account, time.Duration) bool { return true }
func (e *blockerRegressionRefreshExecutor) CacheKey(*Account) string                  { return "openai:account:9001" }
func (e *blockerRegressionRefreshExecutor) Refresh(context.Context, *Account) (map[string]any, error) {
	return nil, e.err
}

type blockerRegressionOpsRepo struct {
	OpsRepository
	captured []*OpsInsertErrorLogInput
}

func (r *blockerRegressionOpsRepo) InsertErrorLog(_ context.Context, input *OpsInsertErrorLogInput) (int64, error) {
	r.captured = append(r.captured, input)
	return int64(len(r.captured)), nil
}

type blockerRegressionRuntimeBlocker struct {
	reason string
	until  time.Time
}

func (b *blockerRegressionRuntimeBlocker) BlockAccountScheduling(_ *Account, until time.Time, reason string) {
	b.reason = reason
	b.until = until
}
func (b *blockerRegressionRuntimeBlocker) ClearAccountSchedulingBlock(int64) {}

func TestBlockerRegressionOpenAITokenProviderRefreshErrorFailsClosed(t *testing.T) {
	account := &Account{ID: 9001, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
		"access_token": "stale-access", "refresh_token": "refresh", "expires_at": time.Now().Add(time.Minute).UTC().Format(time.RFC3339),
	}}
	repo := &blockerRegressionAccountRepo{account: account}
	cache := newBlockerRegressionTokenCache()
	cache.getErr = errors.New("force cache miss")
	blocker := &blockerRegressionRuntimeBlocker{}
	provider := NewOpenAITokenProvider(repo, cache, nil)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), &blockerRegressionRefreshExecutor{err: errors.New("upstream timeout")})
	provider.SetAccountRuntimeBlocker(blocker)

	token, err := provider.GetAccessToken(context.Background(), account)

	require.Error(t, err)
	require.Empty(t, token)
	require.Equal(t, int32(1), atomic.LoadInt32(&cache.deleteCalled))
	require.Equal(t, 0, repo.setErrorCalls)
	require.Equal(t, 1, repo.tempCalls)
	require.Equal(t, OpenAIAuthStateCooling, repo.updateExtra["openai_auth_state"])
	require.Equal(t, "openai_refresh_retryable", blocker.reason)
}

func TestBlockerRegressionTemporaryNetworkShortCooldownExpires(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 9002, Platform: PlatformOpenAI, Type: AccountTypeOAuth}

	shouldDisable := svc.handleOpenAIAccountUpstreamError(context.Background(), account, 502, nil, []byte("<html>bad gateway temporary network</html>"), "gpt-5", "responses")

	require.False(t, shouldDisable)
	require.True(t, svc.isOpenAIAccountRuntimeBlocked(account), "temporary upstream/network failures should short-cooldown scheduling")
	value, ok := svc.openaiAccountRuntimeBlockUntil.Load(account.ID)
	require.True(t, ok)
	until, ok := value.(time.Time)
	require.True(t, ok)
	require.WithinDuration(t, time.Now().Add(openAIRuntimeGuardLearnedBlockTemporaryTTL), until, 2*time.Second)

	svc.openaiAccountRuntimeBlockUntil.Store(account.ID, time.Now().Add(-time.Millisecond))
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account), "expired temporary_network cooldown should allow probing again")
}

func TestBlockerRegressionRuntimeGuardLocalOpsEventStructuredAndSanitized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Set(OpenAIRuntimeGuardMetadataKey, OpenAIRuntimeGuardMetadata{
		Action:           "block",
		Category:         "reasoning.unknown_effort",
		Metric:           "openai_runtime_guard.blocked.reasoning_effort",
		Field:            "reasoning_effort",
		Path:             "reasoning_effort",
		From:             "sk-proj-secret raw prompt",
		TextHash:         "hash-123",
		SanitizedSummary: "unsupported reasoning effort",
	})

	AppendOpsOpenAIRuntimeGuardLocalEvent(c)
	rawEvents, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events := rawEvents.([]*OpsUpstreamErrorEvent)
	require.Len(t, events, 1)
	require.Equal(t, "local_runtime_guard", events[0].Kind)
	require.Equal(t, "block", events[0].RuntimeGuardAction)
	require.Equal(t, "reasoning.unknown_effort", events[0].RuntimeGuardCategory)
	require.Equal(t, "openai_runtime_guard.blocked.reasoning_effort", events[0].RuntimeGuardMetric)
	require.NotNil(t, events[0].UpstreamCalled)
	require.False(t, *events[0].UpstreamCalled)
	require.NotNil(t, events[0].RawBodyLogged)
	require.False(t, *events[0].RawBodyLogged)

	entry := &OpsInsertErrorLogInput{Platform: PlatformOpenAI, UpstreamErrors: events}
	repo := &blockerRegressionOpsRepo{}
	svc := NewOpsService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	require.NoError(t, svc.RecordErrorBatch(context.Background(), []*OpsInsertErrorLogInput{entry}))
	require.Len(t, repo.captured, 1)
	require.NotNil(t, repo.captured[0].UpstreamErrorsJSON)
	serialized := *repo.captured[0].UpstreamErrorsJSON
	require.Contains(t, serialized, `"runtime_guard_action":"block"`)
	require.Contains(t, serialized, `"upstream_called":false`)
	require.Contains(t, serialized, `"raw_body_logged":false`)
	require.Contains(t, serialized, `"text_hash":"hash-123"`)
	require.NotContains(t, serialized, "sk-proj-secret")
	require.NotContains(t, serialized, "raw prompt")
}

func TestBlockerRegressionLocalBlockedPayloadPreservesRuntimeGuardCategory(t *testing.T) {
	blocked := newOpenAIRuntimeGuardBlockedError(openAIReasoningEffortGuardDecision{
		Action:   "block",
		Blocked:  true,
		Present:  true,
		Status:   400,
		Path:     "reasoning_effort",
		Category: "reasoning.unknown_effort",
		Metric:   "openai_runtime_guard.blocked.reasoning_effort",
	})
	require.JSONEq(t, `{"error":{"type":"invalid_request_error","code":"local_policy_block","category":"capability.local_policy_block","runtime_guard_category":"reasoning.unknown_effort","message":"Unsupported reasoning_effort value","param":"reasoning_effort"}}`, string(blocked.Payload))
}

func TestBlockerRegressionGatewayRefreshFailureDoesNotCallUpstream(t *testing.T) {
	upstream, _, c, svc, account := newOpenAIRuntimeGuardForwardHarness(t)
	account.ID = 9003
	account.Credentials["refresh_token"] = "refresh-token"
	account.Credentials["expires_at"] = time.Now().Add(time.Minute).UTC().Format(time.RFC3339)
	repo := &blockerRegressionAccountRepo{account: account}
	cache := newBlockerRegressionTokenCache()
	cache.getErr = errors.New("force cache miss")
	provider := NewOpenAITokenProvider(repo, cache, nil)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), &blockerRegressionRefreshExecutor{err: errors.New("oauth refresh failed: timeout")})
	svc.openAITokenProvider = provider

	result, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"gpt-5","stream":false,"input":"hi"}`))

	require.Error(t, err)
	require.Nil(t, result)
	require.Len(t, upstream.requests, 0)
}

func TestBlockerRegressionOpenAITokenProviderTerminalRefreshErrorRequiresRelogin(t *testing.T) {
	account := &Account{ID: 9004, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
		"access_token": "stale-access", "refresh_token": "refresh", "expires_at": time.Now().Add(time.Minute).UTC().Format(time.RFC3339),
	}}
	repo := &blockerRegressionAccountRepo{account: account}
	cache := newBlockerRegressionTokenCache()
	cache.getErr = errors.New("force cache miss")
	blocker := &blockerRegressionRuntimeBlocker{}
	provider := NewOpenAITokenProvider(repo, cache, nil)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), &blockerRegressionRefreshExecutor{err: errors.New("invalid_grant: refresh token revoked")})
	provider.SetAccountRuntimeBlocker(blocker)

	token, err := provider.GetAccessToken(context.Background(), account)

	require.Error(t, err)
	require.Empty(t, token)
	require.Equal(t, 1, repo.setErrorCalls)
	require.Equal(t, 0, repo.tempCalls)
	require.Equal(t, OpenAIAuthStateTerminal, repo.updateExtra["openai_auth_state"])
	require.Equal(t, "openai_refresh_terminal", blocker.reason)
	require.Equal(t, int32(1), atomic.LoadInt32(&cache.deleteCalled))
}

func TestBlockerRegressionForwardShapeRepairAddsLocalOpsEvent(t *testing.T) {
	upstream, _, c, svc, account := newOpenAIRuntimeGuardForwardHarness(t)
	body := []byte(`{"model":"gpt-5","stream":false,"input":[{"type":"message","role":"assistant","content":[{"type":"input_text","text":"assistant history"}]}]}`)

	result, err := svc.Forward(context.Background(), c, account, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.requests, 1)
	requireOpenAIRuntimeGuardMetadataForField(t, c, "shape", "repair", "shape.assistant_content_part_repaired", "openai_runtime_guard.repaired.assistant_content_part")
	AppendOpsOpenAIRuntimeGuardLocalEvent(c)
	rawEvents, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events := rawEvents.([]*OpsUpstreamErrorEvent)
	require.Len(t, events, 1)
	require.Equal(t, "local_runtime_guard", events[0].Kind)
	require.Equal(t, "repair", events[0].RuntimeGuardAction)
	require.Equal(t, "shape.assistant_content_part_repaired", events[0].RuntimeGuardCategory)
	require.NotNil(t, events[0].UpstreamCalled)
	require.False(t, *events[0].UpstreamCalled)
	require.NotNil(t, events[0].RawBodyLogged)
	require.False(t, *events[0].RawBodyLogged)
}

func TestBlockerRegressionForwardShapeBlockAddsLocalOpsEvent(t *testing.T) {
	upstream, _, c, svc, account := newOpenAIRuntimeGuardForwardHarness(t)
	body := []byte(`{"model":"gpt-5","stream":false,"input":[{"type":"message","role":"assistant","content":[{"type":"input_image","image_url":"data:image/png;base64,abcd"}]}]}`)

	result, err := svc.Forward(context.Background(), c, account, body)

	require.Error(t, err)
	require.Nil(t, result)
	require.Len(t, upstream.requests, 0)
	requireOpenAIRuntimeGuardMetadataForField(t, c, "shape", "block", "shape.assistant_input_content_blocked", "openai_runtime_guard.blocked.assistant_content_part")
	AppendOpsOpenAIRuntimeGuardLocalEvent(c)
	rawEvents, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events := rawEvents.([]*OpsUpstreamErrorEvent)
	require.Len(t, events, 1)
	require.Equal(t, "local_runtime_guard", events[0].Kind)
	require.Equal(t, "block", events[0].RuntimeGuardAction)
	require.Equal(t, "shape.assistant_input_content_blocked", events[0].RuntimeGuardCategory)
	require.NotNil(t, events[0].UpstreamCalled)
	require.False(t, *events[0].UpstreamCalled)
	require.NotNil(t, events[0].RawBodyLogged)
	require.False(t, *events[0].RawBodyLogged)
}

func TestBlockerRegressionPreflightShapeMetadataForRepairAndBlock(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &OpenAIGatewayService{}
	account := newOpenAIRuntimeGuardContentSafetyOAuthAccount("")

	repairCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	repaired, err := svc.applyOpenAIOAuthRuntimeGuardPreflightToHTTP(repairCtx, account, "gpt-5.4", "responses", ContentModerationProtocolOpenAIResponses, []byte(`{"model":"gpt-5.4","input":[{"type":"message","role":"assistant","content":[{"type":"input_text","text":"history"}]}]}`), false)
	require.NoError(t, err)
	require.Contains(t, string(repaired), "output_text")
	requireOpenAIRuntimeGuardMetadataForField(t, repairCtx, "shape", "repair", "shape.assistant_content_part_repaired", "openai_runtime_guard.repaired.assistant_content_part")

	blockCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	_, err = svc.applyOpenAIOAuthRuntimeGuardPreflightToHTTP(blockCtx, account, "gpt-5.4", "responses", ContentModerationProtocolOpenAIResponses, []byte(`{"model":"gpt-5.4","input":[{"type":"message","role":"assistant","content":[{"type":"input_image","image_url":"data:image/png;base64,abcd"}]}]}`), false)
	require.Error(t, err)
	requireOpenAIRuntimeGuardMetadataForField(t, blockCtx, "shape", "block", "shape.assistant_input_content_blocked", "openai_runtime_guard.blocked.assistant_content_part")
}

func requireOpenAIRuntimeGuardMetadataForField(t *testing.T, c *gin.Context, field, action, category, metric string) {
	t.Helper()
	rawMeta, ok := c.Get(OpenAIRuntimeGuardMetadataKey)
	require.True(t, ok)
	metadata, ok := rawMeta.(OpenAIRuntimeGuardMetadata)
	require.True(t, ok)
	require.Equal(t, action, metadata.Action)
	require.Equal(t, category, metadata.Category)
	require.Equal(t, metric, metadata.Metric)
	require.Equal(t, field, metadata.Field)
}
