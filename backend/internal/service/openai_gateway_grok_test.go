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
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestPatchGrokResponsesBodySanitizesUnsupportedFieldsAndTools(t *testing.T) {
	body := []byte(`{
		"model":"grok",
		"stream":false,
		"prompt_cache_retention":"24h",
		"safety_identifier":"user-1",
		"input":[{"role":"user","content":[{"type":"input_text","text":"hi","external_web_access":true}]}],
		"tools":[
			{"type":"function","name":"allowed","parameters":{"type":"object"}},
			{"type":"image_generation"},
			{"type":"web_search"}
		],
		"tool_choice":{"type":"function","name":"removed"}
	}`)

	patched, err := patchGrokResponsesBody(body, "grok-4.3")
	require.NoError(t, err)
	require.Equal(t, "grok-4.3", gjson.GetBytes(patched, "model").String())
	require.False(t, gjson.GetBytes(patched, "prompt_cache_retention").Exists())
	require.False(t, gjson.GetBytes(patched, "safety_identifier").Exists())
	require.NotContains(t, string(patched), "external_web_access")
	require.Len(t, gjson.GetBytes(patched, "tools").Array(), 2)
	require.True(t, gjson.GetBytes(patched, `tools.#(type=="function")`).Exists())
	require.True(t, gjson.GetBytes(patched, `tools.#(type=="web_search")`).Exists())
	require.False(t, gjson.GetBytes(patched, `tools.#(type=="image_generation")`).Exists())
	require.False(t, gjson.GetBytes(patched, "tool_choice").Exists())
}

func TestExtractGrokResponsesReasoningEffortSupportsOpenAICompatibleField(t *testing.T) {
	t.Parallel()

	effort := extractOpenAIReasoningEffortFromBody(
		[]byte(`{"model":"grok-4.3","reasoning_effort":"high"}`),
		"grok-4.3",
	)
	require.NotNil(t, effort)
	require.Equal(t, "high", *effort)
}

func TestPatchGrokResponsesBodyKeepsValidFunctionToolChoice(t *testing.T) {
	body := []byte(`{
		"model":"grok",
		"tools":[{"type":"function","name":"allowed","parameters":{"type":"object"}}],
		"tool_choice":{"type":"function","name":"allowed"}
	}`)

	patched, err := patchGrokResponsesBody(body, "grok-4.3")

	require.NoError(t, err)
	require.Equal(t, "allowed", gjson.GetBytes(patched, "tool_choice.name").String())
}

func TestPatchGrokResponsesBodyDropsGrok45UnsupportedFields(t *testing.T) {
	body := []byte(`{
		"model":"grok-latest",
		"input":"hello",
		"presence_penalty":0.1,
		"presencePenalty":0.2,
		"frequency_penalty":0.3,
		"frequencyPenalty":0.4,
		"stop":["done"]
	}`)

	patched, err := patchGrokResponsesBody(body, "grok-4.5")

	require.NoError(t, err)
	require.Equal(t, "grok-4.5", gjson.GetBytes(patched, "model").String())
	for _, field := range []string{"presence_penalty", "presencePenalty", "frequency_penalty", "frequencyPenalty", "stop"} {
		require.False(t, gjson.GetBytes(patched, field).Exists(), field)
	}
}

func TestOpenAIGatewayService_ForwardGrokResponsesUsesXAIEndpointAndSanitizedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"grok","stream":false,"input":"hi","reasoning_effort":"high","prompt_cache_retention":"24h","tools":[{"type":"image_generation"},{"type":"function","name":"ok"}],"tool_choice":{"type":"image_generation"}}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("OpenAI-Beta", "responses=v1")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "Xai-Request-Id": []string{"xai-req-1"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"resp_grok","status":"completed","model":"grok-4.3","output":[],"usage":{"input_tokens":2,"output_tokens":3}}`)),
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	account := &Account{
		ID:          901,
		Name:        "grok-oauth",
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":  "grok-access-token",
			"base_url":      "https://api.x.ai/v1",
			"model_mapping": map[string]any{"grok": "grok-4.3"},
		},
		Status:      StatusActive,
		Schedulable: true,
	}

	result, err := svc.Forward(context.Background(), c, account, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, "https://api.x.ai/v1/responses", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer grok-access-token", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "responses=v1", upstream.lastReq.Header.Get("OpenAI-Beta"))
	require.Equal(t, "grok-4.3", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, "high", gjson.GetBytes(upstream.lastBody, "reasoning_effort").String())
	require.False(t, gjson.GetBytes(upstream.lastBody, "prompt_cache_retention").Exists())
	require.False(t, gjson.GetBytes(upstream.lastBody, `tools.#(type=="image_generation")`).Exists())
	require.False(t, gjson.GetBytes(upstream.lastBody, "tool_choice").Exists())
	require.Equal(t, "xai-req-1", result.RequestID)
	require.Equal(t, "grok", result.Model)
	require.Equal(t, "grok-4.3", result.UpstreamModel)
	require.Equal(t, 2, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)
	require.NotNil(t, result.ReasoningEffort)
	require.Equal(t, "high", *result.ReasoningEffort)
}

func TestOpenAIGatewayService_ForwardGrokResponsesFailoverStoresQuotaSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"grok","stream":false,"input":"hi"}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	retryAfter := "17"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header: http.Header{
			"Content-Type":               []string{"application/json"},
			"Xai-Request-Id":             []string{"xai-429"},
			"Retry-After":                []string{retryAfter},
			"X-Ratelimit-Limit-Requests": []string{"100"},
		},
		Body: io.NopCloser(strings.NewReader(`{"error":{"message":"rate limited"}}`)),
	}}
	repo := &grokResponsesAccountRepoStub{}
	svc := &OpenAIGatewayService{httpUpstream: upstream, accountRepo: repo}
	account := &Account{
		ID:          902,
		Name:        "grok-oauth",
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "grok-access-token", "base_url": "https://api.x.ai/v1"},
		Status:      StatusActive,
		Schedulable: true,
	}

	result, err := svc.Forward(context.Background(), c, account, body)

	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr), "expected UpstreamFailoverError, got %T: %v", err, err)
	require.Equal(t, http.StatusTooManyRequests, failoverErr.StatusCode)
	require.NotEmpty(t, failoverErr.ResponseBody)
	require.Equal(t, int64(902), repo.lastUpdateExtraID)
	snapshot, ok := repo.lastUpdateExtra[grokQuotaSnapshotExtraKey].(*xai.QuotaSnapshot)
	require.True(t, ok)
	require.NotNil(t, snapshot.RetryAfterSeconds)
	require.Equal(t, 17, *snapshot.RetryAfterSeconds)
	require.Equal(t, int64(902), repo.lastTempUnschedID)
	require.Contains(t, repo.lastTempUnschedReason, "grok rate limited")
	require.WithinDuration(t, time.Now().Add(17*time.Second), repo.lastTempUnschedUntil, 3*time.Second)
}

func TestOpenAIGatewayService_ForwardAsAnthropicGrokUsesXAIResponsesAndSanitizedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{
		"model":"claude-sonnet-4-5-20250929",
		"stream":true,
		"messages":[{"role":"user","content":"hello"}],
		"metadata":{"user_id":"session-grok"}
	}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":   []string{"text/event-stream"},
			"Xai-Request-Id": []string{"xai-msg-1"},
		},
		Body: io.NopCloser(strings.NewReader(
			"event: response.output_text.delta\n" +
				`data: {"type":"response.output_text.delta","delta":"hello"}` + "\n\n" +
				"event: response.completed\n" +
				`data: {"type":"response.completed","response":{"id":"resp_grok_msg","model":"grok-4.3","status":"completed","usage":{"input_tokens":2,"output_tokens":3}}}` + "\n\n",
		)),
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	account := &Account{
		ID:          903,
		Name:        "grok-oauth",
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":  "grok-access-token",
			"base_url":      xai.DefaultCLIBaseURL,
			"model_mapping": map[string]any{"claude-sonnet-4-5-20250929": "grok-4.3"},
		},
		Status:      StatusActive,
		Schedulable: true,
	}

	result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "prompt-cache-key", "grok-4.3")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, xai.DefaultCLIBaseURL+"/responses", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer grok-access-token", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "grok-4.3", gjson.GetBytes(upstream.lastBody, "model").String())
	require.False(t, gjson.GetBytes(upstream.lastBody, "prompt_cache_key").Exists(), "Grok messages path must not apply Codex OAuth prompt cache transform")
	require.NotContains(t, string(upstream.lastBody), "You are ChatGPT")
	require.Contains(t, rec.Body.String(), "content_block_delta")
	require.Equal(t, "grok-4.3", result.UpstreamModel)
	require.Equal(t, 2, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)
}

func TestOpenAIGatewayService_ForwardAsChatCompletionsGrokOAuthUsesXAIChatCompletions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"grok","stream":false,"messages":[{"role":"user","content":"hi"}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":   []string{"application/json"},
			"Xai-Request-Id": []string{"xai-chat-1"},
		},
		Body: io.NopCloser(strings.NewReader(`{"id":"chatcmpl_grok","object":"chat.completion","model":"grok-4.3","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`)),
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	account := &Account{
		ID:          904,
		Name:        "grok-oauth",
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":  "grok-access-token",
			"base_url":      xai.DefaultCLIBaseURL,
			"model_mapping": map[string]any{"grok": "grok-4.3"},
		},
		Status:      StatusActive,
		Schedulable: true,
	}

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, xai.DefaultCLIBaseURL+"/chat/completions", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer grok-access-token", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "grok-4.3", gjson.GetBytes(upstream.lastBody, "model").String())
	require.NotContains(t, string(upstream.lastBody), "prompt_cache_key")
	require.Equal(t, "grok-4.3", result.UpstreamModel)
	require.Equal(t, 2, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)
}

func TestOpenAIGatewayService_ForwardAsChatCompletionsGrokComposerBridgesImageInput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"grok-composer-2.5-fast","stream":false,"messages":[{"role":"user","content":[{"type":"text","text":"What is shown?"},{"type":"image_url","image_url":{"url":"data:image/png;base64,QUJD"}}]}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"resp_vision","object":"response","model":"grok-build-0.1","output":[{"type":"message","content":[{"type":"output_text","text":"A small diagram with ABC letters."}]}],"usage":{"input_tokens":11,"output_tokens":7}}`)),
		},
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"chatcmpl_composer","object":"chat.completion","model":"grok-composer-2.5-fast","choices":[{"index":0,"message":{"role":"assistant","content":"It shows ABC."},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`)),
		},
	}}
	account := &Account{
		ID:          905,
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "grok-access-token", "base_url": xai.DefaultCLIBaseURL},
	}
	svc := &OpenAIGatewayService{cfg: grokComposerBridgeTestConfig(), httpUpstream: upstream}

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")

	require.NoError(t, err)
	require.Len(t, upstream.requests, 2)
	require.Equal(t, xai.DefaultCLIBaseURL+"/responses", upstream.requests[0].URL.String())
	require.Equal(t, "grok-build-0.1", gjson.GetBytes(upstream.bodies[0], "model").String())
	require.Equal(t, "input_image", gjson.GetBytes(upstream.bodies[0], "input.0.content.1.type").String())
	require.False(t, strings.Contains(string(upstream.bodies[1]), "image_url"))
	require.Contains(t, gjson.GetBytes(upstream.bodies[1], "messages.0.content").String(), "Image 1 description")
	require.Equal(t, 14, result.Usage.InputTokens)
	require.Equal(t, 12, result.Usage.OutputTokens)
}

func TestDescribeGrokComposerImageOpsEventOmitsAccountIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	requestContext, _ := gin.CreateTestContext(recorder)
	requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusInternalServerError,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"temporary failure"}}`)),
	}}
	service := &OpenAIGatewayService{cfg: grokComposerBridgeTestConfig(), httpUpstream: upstream}
	account := &Account{ID: 987654, Name: "private-account-name", Platform: PlatformGrok, Type: AccountTypeOAuth, Concurrency: 1}

	_, _, err := service.describeGrokComposerImage(context.Background(), requestContext, account, "test-token", "https://example.test/image.png", 1)
	require.Error(t, err)

	rawEvents, ok := requestContext.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := rawEvents.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.Zero(t, events[0].AccountID)
	require.Empty(t, events[0].AccountName)
}

func grokComposerBridgeTestConfig() *config.Config {
	return &config.Config{
		Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{Enabled: false, AllowInsecureHTTP: true},
		},
	}
}

type grokResponsesAccountRepoStub struct {
	lastUpdateExtraID     int64
	lastUpdateExtra       map[string]any
	lastTempUnschedID     int64
	lastTempUnschedUntil  time.Time
	lastTempUnschedReason string
}

func (r *grokResponsesAccountRepoStub) Create(context.Context, *Account) error { return nil }
func (r *grokResponsesAccountRepoStub) GetByID(context.Context, int64) (*Account, error) {
	return nil, ErrAccountNotFound
}
func (r *grokResponsesAccountRepoStub) GetByIDs(context.Context, []int64) ([]*Account, error) {
	return nil, nil
}
func (r *grokResponsesAccountRepoStub) ExistsByID(context.Context, int64) (bool, error) {
	return false, nil
}
func (r *grokResponsesAccountRepoStub) GetByCRSAccountID(context.Context, string) (*Account, error) {
	return nil, nil
}
func (r *grokResponsesAccountRepoStub) FindByExtraField(context.Context, string, any) ([]Account, error) {
	return nil, nil
}
func (r *grokResponsesAccountRepoStub) ListCRSAccountIDs(context.Context) (map[string]int64, error) {
	return nil, nil
}
func (r *grokResponsesAccountRepoStub) Update(context.Context, *Account) error { return nil }
func (r *grokResponsesAccountRepoStub) Delete(context.Context, int64) error    { return nil }
func (r *grokResponsesAccountRepoStub) List(context.Context, pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (r *grokResponsesAccountRepoStub) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string, string, int64, string) ([]Account, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (r *grokResponsesAccountRepoStub) ListByGroup(context.Context, int64) ([]Account, error) {
	return nil, nil
}
func (r *grokResponsesAccountRepoStub) ListActive(context.Context) ([]Account, error) {
	return nil, nil
}
func (r *grokResponsesAccountRepoStub) ListByPlatform(context.Context, string) ([]Account, error) {
	return nil, nil
}
func (r *grokResponsesAccountRepoStub) UpdateLastUsed(context.Context, int64) error { return nil }
func (r *grokResponsesAccountRepoStub) BatchUpdateLastUsed(context.Context, map[int64]time.Time) error {
	return nil
}
func (r *grokResponsesAccountRepoStub) SetError(context.Context, int64, string) error     { return nil }
func (r *grokResponsesAccountRepoStub) ClearError(context.Context, int64) error           { return nil }
func (r *grokResponsesAccountRepoStub) SetSchedulable(context.Context, int64, bool) error { return nil }
func (r *grokResponsesAccountRepoStub) AutoPauseExpiredAccounts(context.Context, time.Time) (int64, error) {
	return 0, nil
}
func (r *grokResponsesAccountRepoStub) BindGroups(context.Context, int64, []int64) error { return nil }
func (r *grokResponsesAccountRepoStub) ListSchedulable(context.Context) ([]Account, error) {
	return nil, nil
}
func (r *grokResponsesAccountRepoStub) ListSchedulableByGroupID(context.Context, int64) ([]Account, error) {
	return nil, nil
}
func (r *grokResponsesAccountRepoStub) ListSchedulableByPlatform(context.Context, string) ([]Account, error) {
	return nil, nil
}
func (r *grokResponsesAccountRepoStub) ListSchedulableByGroupIDAndPlatform(context.Context, int64, string) ([]Account, error) {
	return nil, nil
}
func (r *grokResponsesAccountRepoStub) ListSchedulableByPlatforms(context.Context, []string) ([]Account, error) {
	return nil, nil
}
func (r *grokResponsesAccountRepoStub) ListSchedulableByGroupIDAndPlatforms(context.Context, int64, []string) ([]Account, error) {
	return nil, nil
}
func (r *grokResponsesAccountRepoStub) ListSchedulableUngroupedByPlatform(context.Context, string) ([]Account, error) {
	return nil, nil
}
func (r *grokResponsesAccountRepoStub) ListSchedulableUngroupedByPlatforms(context.Context, []string) ([]Account, error) {
	return nil, nil
}
func (r *grokResponsesAccountRepoStub) SetRateLimited(context.Context, int64, time.Time) error {
	return nil
}
func (r *grokResponsesAccountRepoStub) SetModelRateLimit(context.Context, int64, string, time.Time, ...string) error {
	return nil
}
func (r *grokResponsesAccountRepoStub) SetOverloaded(context.Context, int64, time.Time) error {
	return nil
}
func (r *grokResponsesAccountRepoStub) SetTempUnschedulable(_ context.Context, id int64, until time.Time, reason string) error {
	r.lastTempUnschedID = id
	r.lastTempUnschedUntil = until
	r.lastTempUnschedReason = reason
	return nil
}
func (r *grokResponsesAccountRepoStub) ClearTempUnschedulable(context.Context, int64) error {
	return nil
}
func (r *grokResponsesAccountRepoStub) ClearRateLimit(context.Context, int64) error { return nil }
func (r *grokResponsesAccountRepoStub) ClearAntigravityQuotaScopes(context.Context, int64) error {
	return nil
}
func (r *grokResponsesAccountRepoStub) ClearModelRateLimits(context.Context, int64) error { return nil }
func (r *grokResponsesAccountRepoStub) UpdateSessionWindow(context.Context, int64, *time.Time, *time.Time, string) error {
	return nil
}
func (r *grokResponsesAccountRepoStub) UpdateSessionWindowEnd(context.Context, int64, time.Time) error {
	return nil
}
func (r *grokResponsesAccountRepoStub) UpdateExtra(_ context.Context, id int64, updates map[string]any) error {
	r.lastUpdateExtraID = id
	r.lastUpdateExtra = updates
	return nil
}
func (r *grokResponsesAccountRepoStub) BulkUpdate(context.Context, []int64, AccountBulkUpdate) (int64, error) {
	return 0, nil
}
func (r *grokResponsesAccountRepoStub) IncrementQuotaUsed(context.Context, int64, float64) error {
	return nil
}
func (r *grokResponsesAccountRepoStub) ResetQuotaUsed(context.Context, int64) error      { return nil }
func (r *grokResponsesAccountRepoStub) RevertProxyFallback(context.Context, int64) error { return nil }

func requireJSONSerializableForGrokTest(t *testing.T, value any) {
	t.Helper()
	_, err := json.Marshal(value)
	require.NoError(t, err)
}
