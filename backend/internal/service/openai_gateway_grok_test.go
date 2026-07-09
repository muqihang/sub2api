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

func TestOpenAIGatewayService_ForwardGrokResponsesUsesXAIEndpointAndSanitizedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"grok","stream":false,"input":"hi","prompt_cache_retention":"24h","tools":[{"type":"image_generation"},{"type":"function","name":"ok"}],"tool_choice":{"type":"image_generation"}}`)
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
	require.False(t, gjson.GetBytes(upstream.lastBody, "prompt_cache_retention").Exists())
	require.False(t, gjson.GetBytes(upstream.lastBody, `tools.#(type=="image_generation")`).Exists())
	require.False(t, gjson.GetBytes(upstream.lastBody, "tool_choice").Exists())
	require.Equal(t, "xai-req-1", result.RequestID)
	require.Equal(t, "grok", result.Model)
	require.Equal(t, "grok-4.3", result.UpstreamModel)
	require.Equal(t, 2, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)
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
