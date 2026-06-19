package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type openAIRuntimeGuardFixtureCatalog struct {
	Version  int                         `json:"version"`
	Fixtures []openAIRuntimeGuardFixture `json:"fixtures"`
}

type openAIRuntimeGuardFixture struct {
	ID            string                               `json:"id"`
	Area          string                               `json:"area"`
	Summary       string                               `json:"summary"`
	Tags          []string                             `json:"tags"`
	Profile       map[string]any                       `json:"profile"`
	Request       json.RawMessage                      `json:"request"`
	UpstreamError *openAIRuntimeGuardUpstreamError     `json:"upstream_error,omitempty"`
	Expect        openAIRuntimeGuardFixtureExpectation `json:"expect"`
}

type openAIRuntimeGuardUpstreamError struct {
	Status int    `json:"status"`
	Code   string `json:"code"`
	Type   string `json:"type"`
}

type openAIRuntimeGuardFixtureExpectation struct {
	Decision      string                            `json:"decision"`
	Status        int                               `json:"status"`
	UpstreamCalls int                               `json:"upstream_calls"`
	Category      string                            `json:"category"`
	Metric        string                            `json:"metric"`
	AccountState  string                            `json:"account_state,omitempty"`
	Repair        *openAIRuntimeGuardRepair         `json:"repair,omitempty"`
	Retry         *openAIRuntimeGuardRetry          `json:"retry,omitempty"`
	Context       *openAIRuntimeGuardContext        `json:"context,omitempty"`
	Forward       json.RawMessage                   `json:"forward_request,omitempty"`
	Extra         map[string]map[string]interface{} `json:"-"`
}

type openAIRuntimeGuardRepair struct {
	Path string `json:"path"`
	From string `json:"from"`
	To   string `json:"to"`
}

type openAIRuntimeGuardRetry struct {
	MaxAttempts int             `json:"max_attempts"`
	TrimPath    string          `json:"trim_path"`
	Request     json.RawMessage `json:"request"`
}

type openAIRuntimeGuardContext struct {
	EstimatedTokens int    `json:"estimated_tokens"`
	LimitTokens     int    `json:"limit_tokens"`
	Confidence      string `json:"confidence"`
}

type openAIRuntimeGuardMockUpstream struct {
	calls  int
	bodies []json.RawMessage
}

func (u *openAIRuntimeGuardMockUpstream) send(body json.RawMessage) {
	u.calls++
	copied := append(json.RawMessage(nil), body...)
	u.bodies = append(u.bodies, copied)
}

func loadOpenAIRuntimeGuardFixtureCatalog(t *testing.T) openAIRuntimeGuardFixtureCatalog {
	t.Helper()

	path := filepath.Join("testdata", "openai_runtime_guard", "catalog.json")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	requireNoRuntimeGuardFixtureSecrets(t, raw)

	var catalog openAIRuntimeGuardFixtureCatalog
	require.NoError(t, json.Unmarshal(raw, &catalog))
	require.Equal(t, 1, catalog.Version)
	require.NotEmpty(t, catalog.Fixtures)
	return catalog
}

func requireNoRuntimeGuardFixtureSecrets(t *testing.T, raw []byte) {
	t.Helper()

	lower := strings.ToLower(string(raw))
	for _, marker := range []string{
		"sk-proj-",
		"sk-live-",
		"access_token",
		"refresh_token",
		"authorization:",
		"cookie:",
		"sessionid=",
	} {
		require.NotContains(t, lower, marker, "fixture catalog must not contain real token/cookie material")
	}
}

func openAIRuntimeGuardFixtureByID(t *testing.T, id string) openAIRuntimeGuardFixture {
	t.Helper()

	catalog := loadOpenAIRuntimeGuardFixtureCatalog(t)
	for _, fixture := range catalog.Fixtures {
		if fixture.ID == id {
			return fixture
		}
	}
	t.Fatalf("openai runtime guard fixture %q not found", id)
	return openAIRuntimeGuardFixture{}
}

func openAIRuntimeGuardFixtureMap(t *testing.T) map[string]openAIRuntimeGuardFixture {
	t.Helper()

	fixtures := make(map[string]openAIRuntimeGuardFixture)
	for _, fixture := range loadOpenAIRuntimeGuardFixtureCatalog(t).Fixtures {
		require.NotContains(t, fixtures, fixture.ID)
		fixtures[fixture.ID] = fixture
	}
	return fixtures
}

func decodeOpenAIRuntimeGuardJSON(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()

	var out map[string]any
	require.NoError(t, json.Unmarshal(raw, &out))
	require.NotNil(t, out)
	return out
}

func openAIRuntimeGuardFixtureRequest(t *testing.T, fixture openAIRuntimeGuardFixture) map[string]any {
	t.Helper()
	return decodeOpenAIRuntimeGuardJSON(t, fixture.Request)
}

func openAIRuntimeGuardFixtureForward(t *testing.T, fixture openAIRuntimeGuardFixture) map[string]any {
	t.Helper()
	require.NotEmpty(t, fixture.Expect.Forward, "fixture %s should define expected forward_request", fixture.ID)
	return decodeOpenAIRuntimeGuardJSON(t, fixture.Expect.Forward)
}

func openAIRuntimeGuardInputItem(t *testing.T, body map[string]any, index int) map[string]any {
	t.Helper()

	input, ok := body["input"].([]any)
	require.True(t, ok)
	require.Greater(t, len(input), index)
	item, ok := input[index].(map[string]any)
	require.True(t, ok)
	return item
}

func openAIRuntimeGuardContentPart(t *testing.T, item map[string]any, index int) map[string]any {
	t.Helper()

	content, ok := item["content"].([]any)
	require.True(t, ok)
	require.Greater(t, len(content), index)
	part, ok := content[index].(map[string]any)
	require.True(t, ok)
	return part
}

func openAIRuntimeGuardFirstTool(t *testing.T, body map[string]any) map[string]any {
	t.Helper()

	tools, ok := body["tools"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, tools)
	tool, ok := tools[0].(map[string]any)
	require.True(t, ok)
	return tool
}

func requireOpenAIRuntimeGuardDecision(t *testing.T, fixture openAIRuntimeGuardFixture, decision string, upstreamCalls int) {
	t.Helper()

	require.Equal(t, decision, fixture.Expect.Decision, fixture.ID)
	require.Equal(t, upstreamCalls, fixture.Expect.UpstreamCalls, fixture.ID)
	if upstreamCalls == 0 {
		require.GreaterOrEqual(t, fixture.Expect.Status, 400, fixture.ID)
	}
	require.NotEmpty(t, fixture.Expect.Category, fixture.ID)
	require.NotEmpty(t, fixture.Expect.Metric, fixture.ID)
}

func TestOpenAIRuntimeGuardBaselineFixtureCatalogComplete(t *testing.T) {
	fixtures := openAIRuntimeGuardFixtureMap(t)

	expectedIDs := []string{
		"reasoning_effort_max_repaired_to_xhigh",
		"reasoning_effort_unknown_local_400",
		"assistant_history_input_text_repaired_to_output_text",
		"non_assistant_input_text_roles_not_converted",
		"tools_function_wrapper_removed_for_chatgpt_internal_codex_oauth",
		"tools_function_wrapper_preserved_for_native_responses_profile",
		"function_call_arguments_string_preserved_native_shapes",
		"missing_tool_output_local_block",
		"invalid_encrypted_content_trim_retry_once",
		"token_invalidated_account_terminal_needs_relogin",
		"image_generation_disabled_by_group_local_block",
		"native_image_request_no_oauth_basic_fallback",
		"unsupported_oauth_model_profile_scheduler_reject",
		"obviously_over_context_local_shadow_decision",
		"near_boundary_context_uncertain_not_blocked",
		"content_safety_clear_sexual_block",
		"content_safety_credential_theft_block",
		"content_safety_malware_block",
		"content_safety_illicit_instruction_block",
		"content_safety_defensive_security_not_blocked",
	}
	for _, id := range expectedIDs {
		require.Contains(t, fixtures, id)
	}
}

func TestOpenAIRuntimeGuardBaselineFixtureCatalogSchema(t *testing.T) {
	allowedDecisions := map[string]bool{
		"account_terminal": true,
		"block":            true,
		"pass":             true,
		"repair":           true,
		"retry_repair":     true,
		"scheduler_reject": true,
		"shadow_block":     true,
	}

	for _, fixture := range loadOpenAIRuntimeGuardFixtureCatalog(t).Fixtures {
		t.Run(fixture.ID, func(t *testing.T) {
			require.NotEmpty(t, fixture.Area)
			require.NotEmpty(t, fixture.Summary)
			require.NotEmpty(t, fixture.Tags)
			require.NotEmpty(t, fixture.Profile)
			require.True(t, json.Valid(fixture.Request), "request JSON must be valid")
			require.True(t, allowedDecisions[fixture.Expect.Decision], fixture.Expect.Decision)
			require.NotEmpty(t, fixture.Expect.Category)
			require.NotEmpty(t, fixture.Expect.Metric)
			require.GreaterOrEqual(t, fixture.Expect.Status, 200)
			require.GreaterOrEqual(t, fixture.Expect.UpstreamCalls, 0)
			if fixture.Expect.UpstreamCalls == 0 {
				require.GreaterOrEqual(t, fixture.Expect.Status, 400)
			}
			if len(fixture.Expect.Forward) > 0 {
				require.True(t, json.Valid(fixture.Expect.Forward), "forward_request JSON must be valid")
			}
			if fixture.Expect.Retry != nil {
				require.True(t, json.Valid(fixture.Expect.Retry.Request), "retry request JSON must be valid")
				require.Equal(t, 1, fixture.Expect.Retry.MaxAttempts)
				require.NotEmpty(t, fixture.Expect.Retry.TrimPath)
			}
		})
	}
}

func TestOpenAIRuntimeGuardMockUpstreamHarnessCountsCalls(t *testing.T) {
	for _, fixture := range loadOpenAIRuntimeGuardFixtureCatalog(t).Fixtures {
		t.Run(fixture.ID, func(t *testing.T) {
			upstream := &openAIRuntimeGuardMockUpstream{}

			switch fixture.Expect.Decision {
			case "block", "scheduler_reject", "shadow_block", "account_terminal":
				// Local guard/scheduler/account decisions must stop before upstream.
			case "retry_repair":
				upstream.send(fixture.Request)
				require.NotNil(t, fixture.Expect.Retry)
				upstream.send(fixture.Expect.Retry.Request)
			case "repair", "pass":
				if len(fixture.Expect.Forward) > 0 {
					upstream.send(fixture.Expect.Forward)
				} else {
					upstream.send(fixture.Request)
				}
			default:
				t.Fatalf("unhandled fixture decision %q", fixture.Expect.Decision)
			}

			require.Equal(t, fixture.Expect.UpstreamCalls, upstream.calls)
			require.Len(t, upstream.bodies, fixture.Expect.UpstreamCalls)
			if fixture.Expect.UpstreamCalls > 0 && len(fixture.Expect.Forward) > 0 {
				require.JSONEq(t, string(fixture.Expect.Forward), string(upstream.bodies[0]))
			}
		})
	}
}

func TestOpenAIRuntimeGuardReasoningEffortDecisionAliases(t *testing.T) {
	tests := []struct {
		name string
		body []byte
		path string
		want string
	}{
		{name: "flat max", body: []byte(`{"reasoning_effort":"max"}`), path: "reasoning_effort", want: "xhigh"},
		{name: "flat maximum", body: []byte(`{"reasoning_effort":"maximum"}`), path: "reasoning_effort", want: "xhigh"},
		{name: "flat x-high", body: []byte(`{"reasoning_effort":"x-high"}`), path: "reasoning_effort", want: "xhigh"},
		{name: "flat x_high", body: []byte(`{"reasoning_effort":"x_high"}`), path: "reasoning_effort", want: "xhigh"},
		{name: "nested minimal", body: []byte(`{"reasoning":{"effort":"minimal"}}`), path: "reasoning.effort", want: "none"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := evaluateOpenAIReasoningEffortGuard(tt.body)
			require.True(t, decision.Repaired, "%#v", decision)
			require.False(t, decision.Blocked, "%#v", decision)
			require.Equal(t, tt.path, decision.Path)
			require.Equal(t, tt.want, decision.To)
			require.Equal(t, "reasoning.unsupported_effort_repaired", decision.Category)
			require.Equal(t, "openai_runtime_guard.repaired.reasoning_effort", decision.Metric)
		})
	}
}

func TestOpenAIRuntimeGuardReasoningEffortDecisionValidValuesPass(t *testing.T) {
	for _, effort := range []string{"low", "medium", "high", "xhigh", "none"} {
		t.Run(effort, func(t *testing.T) {
			body := []byte(`{"reasoning_effort":"` + effort + `"}`)
			decision := evaluateOpenAIReasoningEffortGuard(body)
			require.False(t, decision.Blocked, "%#v", decision)
			require.False(t, decision.Repaired, "%#v", decision)
		})
	}
}

func TestOpenAIRuntimeGuardReasoningEffortDecisionBlocksUnknown(t *testing.T) {
	decision := evaluateOpenAIReasoningEffortGuard([]byte(`{"reasoning_effort":"hyperdrive"}`))

	require.True(t, decision.Blocked)
	require.False(t, decision.Repaired)
	require.Equal(t, http.StatusBadRequest, decision.Status)
	require.Equal(t, "reasoning.unknown_effort", decision.Category)
	require.Equal(t, "openai_runtime_guard.blocked.reasoning_effort", decision.Metric)
	require.Equal(t, "reasoning_effort", decision.Path)
}

func TestOpenAIRuntimeGuardReasoningEffortDecisionFlatMinimalRepairs(t *testing.T) {
	decision := evaluateOpenAIReasoningEffortGuard([]byte(`{"reasoning_effort":"minimal"}`))

	require.True(t, decision.Repaired, "%#v", decision)
	require.False(t, decision.Blocked, "%#v", decision)
	require.Equal(t, "reasoning_effort", decision.Path)
	require.Equal(t, "minimal", decision.From)
	require.Equal(t, "none", decision.To)
}

func TestOpenAIRuntimeGuardReasoningEffortDecisionRejectsUnlistedAliases(t *testing.T) {
	for _, effort := range []string{"extra-high", "extra_high", "extra high", "extrahigh"} {
		t.Run(effort, func(t *testing.T) {
			decision := evaluateOpenAIReasoningEffortGuard([]byte(`{"reasoning_effort":"` + effort + `"}`))

			require.True(t, decision.Blocked, "%#v", decision)
			require.False(t, decision.Repaired, "%#v", decision)
			require.Equal(t, "reasoning_effort", decision.Path)
		})
	}
}

func TestOpenAIRuntimeGuardReasoningEffortDecisionChecksNestedAndFlat(t *testing.T) {
	decision := evaluateOpenAIReasoningEffortGuard([]byte(`{"reasoning":{"effort":"low"},"reasoning_effort":"hyperdrive"}`))

	require.True(t, decision.Blocked, "%#v", decision)
	require.False(t, decision.Repaired, "%#v", decision)
	require.Equal(t, "reasoning_effort", decision.Path)
}

func TestOpenAIRuntimeGuardReasoningEffortDecisionBlocksNestedFlatConflict(t *testing.T) {
	decision := evaluateOpenAIReasoningEffortGuard([]byte(`{"reasoning":{"effort":"low"},"reasoning_effort":"high"}`))

	require.True(t, decision.Blocked, "%#v", decision)
	require.False(t, decision.Repaired, "%#v", decision)
	require.Equal(t, "reasoning_effort", decision.Path)
	require.Equal(t, "reasoning.conflicting_effort", decision.Category)
}

func TestOpenAIGatewayService_Forward_RuntimeGuardRepairsReasoningEffortBeforeUpstream(t *testing.T) {
	for _, effort := range []string{"max", "maximum", "x-high", "x_high"} {
		t.Run(effort, func(t *testing.T) {
			upstream, _, c, svc, account := newOpenAIRuntimeGuardForwardHarness(t)
			body := []byte(`{"model":"gpt-5.4","stream":false,"instructions":"keep","prompt_cache_key":"cache-a","reasoning_effort":"` + effort + `","input":"hi"}`)

			result, err := svc.Forward(context.Background(), c, account, body)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Len(t, upstream.bodies, 1)
			require.Equal(t, "xhigh", gjson.GetBytes(upstream.lastBody, "reasoning_effort").String())
			require.Equal(t, "xhigh", derefString(result.ReasoningEffort))
			require.Equal(t, "cache-a", gjson.GetBytes(upstream.lastBody, "prompt_cache_key").String())
			requireOpenAIRuntimeGuardMetadata(t, c, "repair", "reasoning.unsupported_effort_repaired", "openai_runtime_guard.repaired.reasoning_effort")
		})
	}
}

func TestOpenAIGatewayService_Forward_RuntimeGuardRepairsNestedMinimalToNone(t *testing.T) {
	upstream, _, c, svc, account := newOpenAIRuntimeGuardForwardHarness(t)
	body := []byte(`{"model":"gpt-5.4","stream":false,"instructions":"keep","reasoning":{"effort":"minimal"},"input":"hi"}`)

	result, err := svc.Forward(context.Background(), c, account, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 1)
	require.Equal(t, "none", gjson.GetBytes(upstream.lastBody, "reasoning.effort").String())
	require.Equal(t, "none", derefString(result.ReasoningEffort))
	requireOpenAIRuntimeGuardMetadata(t, c, "repair", "reasoning.unsupported_effort_repaired", "openai_runtime_guard.repaired.reasoning_effort")
}

func TestOpenAIGatewayService_Forward_RuntimeGuardRepairsFlatMinimalToNone(t *testing.T) {
	upstream, _, c, svc, account := newOpenAIRuntimeGuardForwardHarness(t)
	body := []byte(`{"model":"gpt-5.4","stream":false,"instructions":"keep","reasoning_effort":"minimal","input":"hi"}`)

	result, err := svc.Forward(context.Background(), c, account, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 1)
	require.Equal(t, "none", gjson.GetBytes(upstream.lastBody, "reasoning_effort").String())
	require.Equal(t, "none", derefString(result.ReasoningEffort))
	requireOpenAIRuntimeGuardMetadata(t, c, "repair", "reasoning.unsupported_effort_repaired", "openai_runtime_guard.repaired.reasoning_effort")
}

func TestOpenAIGatewayService_Forward_RuntimeGuardBlocksUnlistedAliasBeforeUpstream(t *testing.T) {
	upstream, _, c, svc, account := newOpenAIRuntimeGuardForwardHarness(t)
	body := []byte(`{"model":"gpt-5.4","stream":false,"instructions":"keep","reasoning_effort":"extra-high","input":"hi"}`)

	result, err := svc.Forward(context.Background(), c, account, body)

	require.Error(t, err)
	require.Nil(t, result)
	require.Len(t, upstream.bodies, 0)
	require.Equal(t, http.StatusBadRequest, c.Writer.Status())
	requireOpenAIRuntimeGuardMetadata(t, c, "block", "reasoning.unknown_effort", "openai_runtime_guard.blocked.reasoning_effort")
}

func TestOpenAIGatewayService_Forward_RuntimeGuardBlocksNestedFlatUnknownBeforeUpstream(t *testing.T) {
	upstream, _, c, svc, account := newOpenAIRuntimeGuardForwardHarness(t)
	body := []byte(`{"model":"gpt-5.4","stream":false,"instructions":"keep","reasoning":{"effort":"low"},"reasoning_effort":"hyperdrive","input":"hi"}`)

	result, err := svc.Forward(context.Background(), c, account, body)

	require.Error(t, err)
	require.Nil(t, result)
	require.Len(t, upstream.bodies, 0)
	require.Equal(t, http.StatusBadRequest, c.Writer.Status())
	requireOpenAIRuntimeGuardMetadata(t, c, "block", "reasoning.unknown_effort", "openai_runtime_guard.blocked.reasoning_effort")
}

func TestOpenAIGatewayService_Forward_RuntimeGuardBlocksUnknownReasoningEffortBeforeUpstream(t *testing.T) {
	upstream, rec, c, svc, account := newOpenAIRuntimeGuardForwardHarness(t)
	body := []byte(`{"model":"gpt-5.4","stream":false,"instructions":"keep","reasoning_effort":"hyperdrive","input":"hi"}`)

	result, err := svc.Forward(context.Background(), c, account, body)

	require.Error(t, err)
	require.Nil(t, result)
	require.Len(t, upstream.bodies, 0)
	require.Equal(t, http.StatusBadRequest, c.Writer.Status())
	require.JSONEq(t, `{"error":{"type":"invalid_request_error","code":"local_policy_block","category":"capability.local_policy_block","message":"Unsupported reasoning_effort value","param":"reasoning_effort"}}`, rec.Body.String())
	requireOpenAIRuntimeGuardMetadata(t, c, "block", "reasoning.unknown_effort", "openai_runtime_guard.blocked.reasoning_effort")
}

func TestOpenAIGatewayService_Forward_RuntimeGuardLocalBlockDoesNotReportScheduleFailure(t *testing.T) {
	upstream, _, c, svc, account := newOpenAIRuntimeGuardForwardHarness(t)
	scheduler := &openAIRuntimeGuardRecordingScheduler{}
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	openAIAdvancedSchedulerSettingCache.Store(&cachedOpenAIAdvancedSchedulerSetting{
		enabled:   true,
		expiresAt: time.Now().Add(time.Minute).UnixNano(),
	})
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
	svc.openaiScheduler = scheduler

	result, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"gpt-5.4","stream":false,"reasoning_effort":"hyperdrive","input":"hi"}`))
	require.Error(t, err)
	require.Nil(t, result)
	require.Len(t, upstream.bodies, 0)

	var localBlock *OpenAIRuntimeGuardBlockedError
	if !errors.As(err, &localBlock) {
		svc.ReportOpenAIAccountScheduleResult(account.ID, false, nil)
	}

	require.Zero(t, scheduler.reportCalls, "local runtime guard block must not be reported as account/upstream failure")
}

func TestOpenAIGatewayService_Forward_RuntimeGuardLeavesValidEffortAndScopesPromptCacheKey(t *testing.T) {
	upstream, _, c, svc, account := newOpenAIRuntimeGuardForwardHarness(t)
	entityCtx := WithResolvedEntity(c.Request.Context(), &ResolvedEntity{Entity: Entity{EntityKey: "team-alpha"}})
	c.Request = c.Request.WithContext(entityCtx)
	body := []byte(`{"model":"gpt-5.4","stream":false,"instructions":"keep","prompt_cache_key":"shared-cache","reasoning_effort":"high","input":"hi"}`)

	result, err := svc.Forward(context.Background(), c, account, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 1)
	require.Equal(t, "high", gjson.GetBytes(upstream.lastBody, "reasoning_effort").String())
	require.Equal(t, "high", derefString(result.ReasoningEffort))
	require.Equal(t, EntityScopedSeed("team-alpha", "shared-cache"), gjson.GetBytes(upstream.lastBody, "prompt_cache_key").String())
	requireOpenAIRuntimeGuardMetadata(t, c, "pass", "reasoning.valid_effort", "openai_runtime_guard.passed.reasoning_effort")
}

func TestOpenAIGatewayService_Forward_RuntimeGuardSkipsAPIKeyRawFallback(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"rid_raw"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"chatcmpl_1","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)),
	}}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
	account := &Account{
		ID:          6201,
		Name:        "openai-apikey-raw",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "https://example.com"},
		Extra:       map[string]any{"use_responses_api": false},
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewReader(nil))
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
	body := []byte(`{"model":"gpt-5.4","stream":false,"instructions":"keep","reasoning_effort":"hyperdrive","input":"hi"}`)

	result, err := svc.Forward(context.Background(), c, account, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 1)
	require.False(t, hasOpenAIRuntimeGuardMetadata(c))
}

func TestOpenAIGatewayService_Forward_RuntimeGuardSkipsAPIKeyPassthrough(t *testing.T) {
	upstream, _, c, svc, account := newOpenAIRuntimeGuardForwardHarness(t)
	account.Name = "openai-apikey-passthrough"
	account.Type = AccountTypeAPIKey
	account.Credentials = map[string]any{"api_key": "sk-test", "base_url": "https://example.com"}
	account.Extra = map[string]any{"openai_passthrough": true, "use_responses_api": true}
	body := []byte(`{"model":"gpt-5.4","stream":false,"instructions":"keep","reasoning_effort":"hyperdrive","input":"hi"}`)

	result, err := svc.Forward(context.Background(), c, account, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 1)
	require.Equal(t, "hyperdrive", gjson.GetBytes(upstream.lastBody, "reasoning_effort").String())
	require.False(t, hasOpenAIRuntimeGuardMetadata(c))
}

func TestOpenAIGatewayService_DoNativeResponsesRequest_RuntimeGuardOAuth(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"rid_native"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"resp_1","object":"response","usage":{"input_tokens":1,"output_tokens":1}}`)),
	}}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
	account := &Account{
		ID:          6301,
		Name:        "openai-oauth-native",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token", "chatgpt_account_id": "chatgpt-acc"},
	}

	resp, err := svc.DoNativeResponsesRequest(context.Background(), account, nil, []byte(`{"model":"gpt-5.4","reasoning_effort":"max","input":"hi"}`), false)

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, upstream.bodies, 1)
	require.Equal(t, "xhigh", gjson.GetBytes(upstream.lastBody, "reasoning_effort").String())
}

func TestOpenAIGatewayService_DoNativeResponsesRequest_RuntimeGuardOAuthBlocksUnknownBeforeUpstream(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`))}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := &Account{
		ID:          6302,
		Name:        "openai-oauth-native",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token", "chatgpt_account_id": "chatgpt-acc"},
	}

	resp, err := svc.DoNativeResponsesRequest(context.Background(), account, nil, []byte(`{"model":"gpt-5.4","reasoning_effort":"extrahigh","input":"hi"}`), false)

	require.Nil(t, resp)
	require.Error(t, err)
	var blocked *OpenAIRuntimeGuardBlockedError
	require.ErrorAs(t, err, &blocked)
	require.Equal(t, http.StatusBadRequest, blocked.StatusCode)
	require.Equal(t, "local_policy_block", gjson.GetBytes(blocked.Payload, "error.code").String())
	require.Equal(t, "capability.local_policy_block", gjson.GetBytes(blocked.Payload, "error.category").String())
	require.Len(t, upstream.bodies, 0)
}

func TestOpenAIGatewayService_DoNativeResponsesRequest_RuntimeGuardSkipsAPIKey(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`))}}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
	account := &Account{
		ID:          6303,
		Name:        "openai-apikey-native",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "https://example.com"},
	}

	resp, err := svc.DoNativeResponsesRequest(context.Background(), account, nil, []byte(`{"model":"gpt-5.4","reasoning_effort":"hyperdrive","input":"hi"}`), false)

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, upstream.bodies, 1)
	require.Equal(t, "hyperdrive", gjson.GetBytes(upstream.lastBody, "reasoning_effort").String())
}

func newOpenAIRuntimeGuardForwardHarness(t *testing.T) (*httpUpstreamRecorder, *httptest.ResponseRecorder, *gin.Context, *OpenAIGatewayService, *Account) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"rid_runtime_guard"}},
		Body:       io.NopCloser(strings.NewReader(`{"usage":{"input_tokens":11,"output_tokens":7,"input_tokens_details":{"cached_tokens":3}}}`)),
	}}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
	account := &Account{
		ID:          6101,
		Name:        "openai-oauth-runtime-guard",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-acc",
		},
		Extra: map[string]any{"openai_passthrough": false, "openai_oauth_responses_websockets_v2_mode": OpenAIWSIngressModeOff},
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewReader(nil))
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
	return upstream, rec, c, svc, account
}

func requireOpenAIRuntimeGuardMetadata(t *testing.T, c *gin.Context, action, category, metric string) {
	t.Helper()
	rawMeta, ok := c.Get(OpenAIRuntimeGuardMetadataKey)
	require.True(t, ok)
	metadata, ok := rawMeta.(OpenAIRuntimeGuardMetadata)
	require.True(t, ok)
	require.Equal(t, action, metadata.Action)
	require.Equal(t, category, metadata.Category)
	require.Equal(t, metric, metadata.Metric)
	require.Equal(t, "reasoning_effort", metadata.Field)
	require.LessOrEqual(t, len(metadata.From), openAIRuntimeGuardMetadataValueMaxLen)
	raw, err := json.Marshal(metadata)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "input")
	require.NotContains(t, string(raw), "access_token")
	require.NotContains(t, string(raw), "sk-test")
}

func hasOpenAIRuntimeGuardMetadata(c *gin.Context) bool {
	_, ok := c.Get(OpenAIRuntimeGuardMetadataKey)
	return ok
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func TestOpenAIRuntimeGuardReasoningEffortDecisionRepairsEmptyFlatWhenNestedValid(t *testing.T) {
	decision := evaluateOpenAIReasoningEffortGuard([]byte(`{"reasoning":{"effort":"low"},"reasoning_effort":""}`))

	require.True(t, decision.Repaired, "%#v", decision)
	require.False(t, decision.Blocked, "%#v", decision)
	require.Equal(t, "reasoning.effort", decision.Path)
	require.Equal(t, "low", decision.From)
	require.Equal(t, "low", decision.To)
	require.Equal(t, "reasoning.empty_effort_removed", decision.Category)
	updated, err := applyOpenAIReasoningEffortGuardRepairs([]byte(`{"reasoning":{"effort":"low"},"reasoning_effort":""}`), decision)
	require.NoError(t, err)
	require.Equal(t, "low", gjson.GetBytes(updated, "reasoning.effort").String())
	require.False(t, gjson.GetBytes(updated, "reasoning_effort").Exists(), "empty flat effort must not be forwarded upstream")
}

func TestOpenAIRuntimeGuardReasoningEffortDecisionRepairsNullFlatWhenNestedValid(t *testing.T) {
	decision := evaluateOpenAIReasoningEffortGuard([]byte(`{"reasoning":{"effort":"low"},"reasoning_effort":null}`))

	require.True(t, decision.Repaired, "%#v", decision)
	require.False(t, decision.Blocked, "%#v", decision)
	require.Equal(t, "reasoning.effort", decision.Path)
	require.Equal(t, "low", decision.From)
	require.Equal(t, "low", decision.To)
	updated, err := applyOpenAIReasoningEffortGuardRepairs([]byte(`{"reasoning":{"effort":"low"},"reasoning_effort":null}`), decision)
	require.NoError(t, err)
	require.Equal(t, "low", gjson.GetBytes(updated, "reasoning.effort").String())
	require.False(t, gjson.GetBytes(updated, "reasoning_effort").Exists(), "null flat effort must not be forwarded upstream")
}

func TestOpenAIRuntimeGuardReasoningEffortMetadataUsesValidFieldWhenFirstEmpty(t *testing.T) {
	decision := evaluateOpenAIReasoningEffortGuard([]byte(`{"reasoning_effort":"","reasoning":{"effort":"high"}}`))

	require.True(t, decision.Repaired, "%#v", decision)
	require.Equal(t, "reasoning.effort", decision.Path, "repair metadata should point at the valid effort field, not the removed empty one")
	updated, err := applyOpenAIReasoningEffortGuardRepairs([]byte(`{"reasoning_effort":"","reasoning":{"effort":"high"}}`), decision)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(updated, "reasoning_effort").Exists())
	require.Equal(t, "high", gjson.GetBytes(updated, "reasoning.effort").String())
}

func TestOpenAIGatewayService_Forward_RuntimeGuardBlocksNestedFlatConflictBeforeUpstream(t *testing.T) {
	upstream, _, c, svc, account := newOpenAIRuntimeGuardForwardHarness(t)
	body := []byte(`{"model":"gpt-5.4","stream":false,"instructions":"keep","reasoning":{"effort":"low"},"reasoning_effort":"high","input":"hi"}`)

	result, err := svc.Forward(context.Background(), c, account, body)

	require.Error(t, err)
	require.Nil(t, result)
	require.Len(t, upstream.bodies, 0)
	require.Equal(t, http.StatusBadRequest, c.Writer.Status())
	requireOpenAIRuntimeGuardMetadata(t, c, "block", "reasoning.conflicting_effort", "openai_runtime_guard.blocked.reasoning_effort")
}

func TestOpenAIGatewayService_Forward_RuntimeGuardRemovesEmptyFlatEffortBeforeUpstream(t *testing.T) {
	upstream, _, c, svc, account := newOpenAIRuntimeGuardForwardHarness(t)
	body := []byte(`{"model":"gpt-5.4","stream":false,"instructions":"keep","reasoning":{"effort":"low"},"reasoning_effort":"","input":"hi"}`)

	result, err := svc.Forward(context.Background(), c, account, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 1)
	require.Equal(t, "low", gjson.GetBytes(upstream.lastBody, "reasoning.effort").String())
	require.False(t, gjson.GetBytes(upstream.lastBody, "reasoning_effort").Exists(), "empty flat effort must not be forwarded upstream")
	requireOpenAIRuntimeGuardMetadata(t, c, "repair", "reasoning.empty_effort_removed", "openai_runtime_guard.repaired.reasoning_effort")
}

func TestOpenAIGatewayService_DoNativeResponsesRequest_RuntimeGuardBlocksWithTypedLocalError(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`))}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := &Account{
		ID:          6304,
		Name:        "openai-oauth-native",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token", "chatgpt_account_id": "chatgpt-acc"},
	}

	resp, err := svc.DoNativeResponsesRequest(context.Background(), account, nil, []byte(`{"model":"gpt-5.4","reasoning_effort":"extrahigh","input":"hi"}`), false)

	require.Nil(t, resp)
	require.Error(t, err)
	var blocked *OpenAIRuntimeGuardBlockedError
	require.ErrorAs(t, err, &blocked)
	require.Equal(t, http.StatusBadRequest, blocked.StatusCode)
	require.Equal(t, "reasoning.unknown_effort", blocked.Decision.Category)
	require.Equal(t, "local_policy_block", gjson.GetBytes(blocked.Payload, "error.code").String())
	require.Equal(t, "capability.local_policy_block", gjson.GetBytes(blocked.Payload, "error.category").String())
	require.Len(t, upstream.bodies, 0)
}

func TestCodexGatewayOpenAIResponsesAdapter_CompleteRuntimeGuardLocalBlockNotCapturedAsUpstream(t *testing.T) {
	adapter, upstream := newCodexGatewayNativeResponsesAdapterForTest(&http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`))}, nil)
	account := newCodexGatewayOpenAIOAuthAccountForTest()
	captureBaseDir := t.TempDir()
	capture := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                  true,
		BaseDir:                  captureBaseDir,
		HashKeyFile:              filepath.Join(captureBaseDir, ".key"),
		CaptureSuccessSampleRate: 1,
	})
	defer capture.Close()
	trace := capture.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{TraceID: "openai_runtime_guard_local_block"})
	require.NotNil(t, trace)

	result, err := adapter.Complete(context.Background(), account, CodexGatewayProviderRequest{
		Request:      CodexGatewayResponsesRequest{Body: []byte(`{"model":"gpt-5.5","reasoning_effort":"hyperdrive","input":"hi"}`)},
		Model:        CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
		CaptureTrace: trace,
	})

	require.Error(t, err)
	var localResp *codexGatewayLocalServiceResponseError
	require.ErrorAs(t, err, &localResp)
	require.Equal(t, http.StatusBadRequest, localResp.Response.StatusCode)
	require.Equal(t, "local_policy_block", gjson.GetBytes(localResp.Response.Body, "error.code").String())
	require.Equal(t, "capability.local_policy_block", gjson.GetBytes(localResp.Response.Body, "error.category").String())
	require.Empty(t, result.ProviderResult.UpstreamRequestID)
	require.Zero(t, result.ProviderResult.Usage.TotalTokens)
	require.Nil(t, upstream.lastRequest)
	capture.FinishTrace(trace, CodexGatewayCaptureFinishSummary{Status: "blocked"})
	require.NoError(t, capture.Close())
	_, statErr := os.Stat(filepath.Join(captureBaseDir, time.Now().Format("2006-01-02"), "openai_runtime_guard_local_block", "upstream_response.shape.json"))
	require.True(t, os.IsNotExist(statErr), "local runtime guard block must not be captured as an upstream response")
}

func TestCodexGatewayOpenAIResponsesAdapter_StreamRuntimeGuardLocalBlockNotCapturedAsUpstream(t *testing.T) {
	adapter, upstream := newCodexGatewayNativeResponsesAdapterForTest(&http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`))}, nil)
	account := newCodexGatewayOpenAIOAuthAccountForTest()
	captureBaseDir := t.TempDir()
	capture := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                  true,
		BaseDir:                  captureBaseDir,
		HashKeyFile:              filepath.Join(captureBaseDir, ".key"),
		CaptureSuccessSampleRate: 1,
	})
	defer capture.Close()
	trace := capture.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{TraceID: "openai_runtime_guard_stream_local_block"})
	require.NotNil(t, trace)
	var writer bytes.Buffer

	result, err := adapter.Stream(context.Background(), account, CodexGatewayProviderRequest{
		Request: CodexGatewayResponsesRequest{
			Body:         []byte(`{"model":"gpt-5.5","stream":true,"reasoning_effort":"hyperdrive","input":"hi"}`),
			StreamWriter: &writer,
			Flush:        func() {},
		},
		Model:        CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
		CaptureTrace: trace,
	})

	require.Error(t, err)
	require.True(t, errors.Is(err, errCodexGatewayRuntimeGuardStreamHandled))
	require.Empty(t, result.UpstreamRequestID)
	require.Zero(t, result.Usage.TotalTokens)
	require.Contains(t, writer.String(), "Unsupported reasoning_effort value")
	require.Contains(t, writer.String(), `"code":"local_policy_block"`)
	require.Contains(t, writer.String(), `"category":"capability.local_policy_block"`)
	require.Nil(t, upstream.lastRequest)
	capture.FinishTrace(trace, CodexGatewayCaptureFinishSummary{Status: "blocked"})
	require.NoError(t, capture.Close())
	_, statErr := os.Stat(filepath.Join(captureBaseDir, time.Now().Format("2006-01-02"), "openai_runtime_guard_stream_local_block", "upstream_response.shape.json"))
	require.True(t, os.IsNotExist(statErr), "local runtime guard block must not be captured as an upstream response")
}

func newCodexGatewayOpenAIOAuthAccountForTest() *Account {
	return &Account{
		ID:          202,
		Name:        "openai-oauth",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-acc",
		},
	}
}

func TestOpenAIRuntimeGuard_WSIngressRepairsUnknownAliasBeforeUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := newOpenAIRuntimeGuardWSTestConfig()
	captureConn := &openAIWSCaptureConn{
		events: [][]byte{[]byte(`{"type":"response.completed","response":{"id":"resp_runtime_guard_ws_repair","model":"gpt-5.5","usage":{"input_tokens":1,"output_tokens":1}}}`)},
	}
	pool := newOpenAIWSConnPool(cfg)
	pool.setClientDialerForTest(&openAIWSCaptureDialer{conn: captureConn})
	svc := newOpenAIRuntimeGuardWSService(cfg, pool)
	account := newOpenAIRuntimeGuardWSOAuthAccount(OpenAIWSIngressModeCtxPool)

	serverErrCh, clientConn, closeServer := startOpenAIRuntimeGuardWSServer(t, svc, account)
	defer closeServer()
	defer func() { _ = clientConn.CloseNow() }()

	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	require.NoError(t, clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.5","stream":false,"reasoning_effort":"max"}`)))
	cancelWrite()

	readCtx, cancelRead := context.WithTimeout(context.Background(), 3*time.Second)
	_, event, readErr := clientConn.Read(readCtx)
	cancelRead()
	require.NoError(t, readErr)
	require.Equal(t, "response.completed", gjson.GetBytes(event, "type").String())
	_ = clientConn.Close(coderws.StatusNormalClosure, "done")

	select {
	case serverErr := <-serverErrCh:
		require.NoError(t, serverErr)
	case <-time.After(5 * time.Second):
		t.Fatal("waiting for ws ingress server")
	}
	require.Len(t, captureConn.writes, 1)
	require.Equal(t, "xhigh", captureConn.writes[0]["reasoning_effort"])
}

func TestOpenAIRuntimeGuard_WSIngressBlocksUnknownBeforeUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := newOpenAIRuntimeGuardWSTestConfig()
	captureConn := &openAIWSCaptureConn{}
	pool := newOpenAIWSConnPool(cfg)
	pool.setClientDialerForTest(&openAIWSCaptureDialer{conn: captureConn})
	svc := newOpenAIRuntimeGuardWSService(cfg, pool)
	account := newOpenAIRuntimeGuardWSOAuthAccount(OpenAIWSIngressModeCtxPool)

	serverErrCh, clientConn, closeServer := startOpenAIRuntimeGuardWSServer(t, svc, account)
	defer closeServer()
	defer func() { _ = clientConn.CloseNow() }()

	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	require.NoError(t, clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.5","stream":false,"reasoning_effort":"hyperdrive"}`)))
	cancelWrite()

	readCtx, cancelRead := context.WithTimeout(context.Background(), 3*time.Second)
	_, event, readErr := clientConn.Read(readCtx)
	cancelRead()
	require.NoError(t, readErr)
	require.Equal(t, "error", gjson.GetBytes(event, "type").String())
	require.Equal(t, "local_policy_block", gjson.GetBytes(event, "error.code").String())
	require.Equal(t, "capability.local_policy_block", gjson.GetBytes(event, "error.category").String())
	require.Equal(t, "reasoning_effort", gjson.GetBytes(event, "error.param").String())

	readCtx2, cancelRead2 := context.WithTimeout(context.Background(), 3*time.Second)
	_, _, closeErr := clientConn.Read(readCtx2)
	cancelRead2()
	require.Error(t, closeErr)
	require.Equal(t, coderws.StatusPolicyViolation, coderws.CloseStatus(closeErr))

	select {
	case serverErr := <-serverErrCh:
		require.Error(t, serverErr)
		var wsCloseErr *OpenAIWSClientCloseError
		require.ErrorAs(t, serverErr, &wsCloseErr)
		require.Equal(t, coderws.StatusPolicyViolation, wsCloseErr.StatusCode())
	case <-time.After(5 * time.Second):
		t.Fatal("waiting for ws ingress server")
	}
	require.Empty(t, captureConn.writes, "unknown reasoning_effort must not be sent upstream")
}

func TestOpenAIRuntimeGuard_WSPassthroughRepairsAndBlocksOAuthFrames(t *testing.T) {
	cfg := newOpenAIRuntimeGuardWSTestConfig()
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.IngressModeDefault = OpenAIWSIngressModePassthrough
	captureConn := &openAIWSCaptureConn{
		events: [][]byte{[]byte(`{"type":"response.completed","response":{"id":"resp_passthrough_guard","model":"gpt-5.5","usage":{"input_tokens":1,"output_tokens":1}}}`)},
	}
	dialer := &openAIWSCaptureDialer{conn: captureConn}
	svc := newOpenAIRuntimeGuardWSService(cfg, nil)
	svc.openaiWSPassthroughDialer = dialer
	account := newOpenAIRuntimeGuardWSOAuthAccount(OpenAIWSIngressModePassthrough)

	serverErrCh, clientConn, closeServer := startOpenAIRuntimeGuardWSServer(t, svc, account)
	defer closeServer()
	defer func() { _ = clientConn.CloseNow() }()

	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	require.NoError(t, clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.5","stream":false,"reasoning_effort":"max"}`)))
	cancelWrite()
	readCtx, cancelRead := context.WithTimeout(context.Background(), 3*time.Second)
	_, _, readErr := clientConn.Read(readCtx)
	cancelRead()
	require.NoError(t, readErr)
	_ = clientConn.Close(coderws.StatusNormalClosure, "done")

	select {
	case serverErr := <-serverErrCh:
		require.NoError(t, serverErr)
	case <-time.After(5 * time.Second):
		t.Fatal("waiting for passthrough server")
	}
	require.Len(t, captureConn.writes, 1)
	require.Equal(t, "xhigh", captureConn.writes[0]["reasoning_effort"])

	blockedConn := &openAIWSCaptureConn{}
	svc.openaiWSPassthroughDialer = &openAIWSCaptureDialer{conn: blockedConn}
	serverErrCh2, clientConn2, closeServer2 := startOpenAIRuntimeGuardWSServer(t, svc, account)
	defer closeServer2()
	defer func() { _ = clientConn2.CloseNow() }()
	writeCtx2, cancelWrite2 := context.WithTimeout(context.Background(), 3*time.Second)
	require.NoError(t, clientConn2.Write(writeCtx2, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.5","stream":false,"reasoning_effort":"hyperdrive"}`)))
	cancelWrite2()
	readCtx2, cancelRead2 := context.WithTimeout(context.Background(), 3*time.Second)
	_, event, readErr2 := clientConn2.Read(readCtx2)
	cancelRead2()
	require.NoError(t, readErr2)
	require.Equal(t, "error", gjson.GetBytes(event, "type").String())
	readCtx3, cancelRead3 := context.WithTimeout(context.Background(), 3*time.Second)
	_, _, closeErr := clientConn2.Read(readCtx3)
	cancelRead3()
	require.Error(t, closeErr)
	require.Equal(t, coderws.StatusPolicyViolation, coderws.CloseStatus(closeErr))
	select {
	case serverErr := <-serverErrCh2:
		require.Error(t, serverErr)
	case <-time.After(5 * time.Second):
		t.Fatal("waiting for blocked passthrough server")
	}
	require.Empty(t, blockedConn.writes)
}

func TestOpenAIRuntimeGuard_WSIngressBlocksOAuthUnsupportedFollowupModelBeforeUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := newOpenAIRuntimeGuardWSTestConfig()
	captureConn := &openAIWSCaptureConn{
		events: [][]byte{
			[]byte(`{"type":"response.completed","response":{"id":"resp_ws_capability_turn_1","model":"gpt-5.5","usage":{"input_tokens":1,"output_tokens":1}}}`),
		},
	}
	pool := newOpenAIWSConnPool(cfg)
	pool.setClientDialerForTest(&openAIWSCaptureDialer{conn: captureConn})
	svc := newOpenAIRuntimeGuardWSService(cfg, pool)
	account := newOpenAIRuntimeGuardWSOAuthAccount(OpenAIWSIngressModeCtxPool)

	serverErrCh, clientConn, closeServer := startOpenAIRuntimeGuardWSServer(t, svc, account)
	defer closeServer()
	defer func() { _ = clientConn.CloseNow() }()

	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	require.NoError(t, clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.5","stream":false}`)))
	cancelWrite()

	readCtx, cancelRead := context.WithTimeout(context.Background(), 3*time.Second)
	_, firstEvent, readErr := clientConn.Read(readCtx)
	cancelRead()
	require.NoError(t, readErr)
	require.Equal(t, "response.completed", gjson.GetBytes(firstEvent, "type").String())

	writeCtx2, cancelWrite2 := context.WithTimeout(context.Background(), 3*time.Second)
	require.NoError(t, clientConn.Write(writeCtx2, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.4-nano","stream":false}`)))
	cancelWrite2()

	readCtx2, cancelRead2 := context.WithTimeout(context.Background(), 3*time.Second)
	_, event, readErr2 := clientConn.Read(readCtx2)
	cancelRead2()
	require.NoError(t, readErr2)
	require.Equal(t, "error", gjson.GetBytes(event, "type").String())
	require.Equal(t, "unsupported_oauth_capability", gjson.GetBytes(event, "error.code").String())
	require.Equal(t, "capability.unsupported_oauth_model_profile", gjson.GetBytes(event, "error.category").String())

	readCtx3, cancelRead3 := context.WithTimeout(context.Background(), 3*time.Second)
	_, _, closeErr := clientConn.Read(readCtx3)
	cancelRead3()
	require.Error(t, closeErr)
	require.Equal(t, coderws.StatusPolicyViolation, coderws.CloseStatus(closeErr))

	select {
	case serverErr := <-serverErrCh:
		require.Error(t, serverErr)
		var wsCloseErr *OpenAIWSClientCloseError
		require.ErrorAs(t, serverErr, &wsCloseErr)
		var selectionErr *OpenAIRuntimeGuardSelectionError
		require.ErrorAs(t, serverErr, &selectionErr)
		require.Equal(t, OpenAIRuntimeGuardErrorCodeUnsupportedOAuthCapability, selectionErr.Code)
	case <-time.After(5 * time.Second):
		t.Fatal("waiting for ws ingress server")
	}
	require.Len(t, captureConn.writes, 1, "unsupported follow-up model must not be sent upstream")
	require.Equal(t, "gpt-5.5", captureConn.writes[0]["model"])
}

func TestOpenAIRuntimeGuard_WSPassthroughBlocksSessionFallbackUnsupportedModelBeforeUpstream(t *testing.T) {
	cfg := newOpenAIRuntimeGuardWSTestConfig()
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.IngressModeDefault = OpenAIWSIngressModePassthrough
	captureConn := &openAIWSCaptureConn{
		events: [][]byte{
			[]byte(`{"type":"response.completed","response":{"id":"resp_passthrough_capability_turn_1","model":"gpt-5.5","usage":{"input_tokens":1,"output_tokens":1}}}`),
		},
		blockWhenEmpty: true,
	}
	svc := newOpenAIRuntimeGuardWSService(cfg, nil)
	svc.openaiWSPassthroughDialer = &openAIWSCaptureDialer{conn: captureConn}
	account := newOpenAIRuntimeGuardWSOAuthAccount(OpenAIWSIngressModePassthrough)

	serverErrCh, clientConn, closeServer := startOpenAIRuntimeGuardWSServer(t, svc, account)
	defer closeServer()
	defer func() { _ = clientConn.CloseNow() }()

	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	require.NoError(t, clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.5","stream":false}`)))
	cancelWrite()
	readCtx, cancelRead := context.WithTimeout(context.Background(), 3*time.Second)
	_, firstEvent, readErr := clientConn.Read(readCtx)
	cancelRead()
	require.NoError(t, readErr)
	require.Equal(t, "response.completed", gjson.GetBytes(firstEvent, "type").String())

	writeCtx2, cancelWrite2 := context.WithTimeout(context.Background(), 3*time.Second)
	require.NoError(t, clientConn.Write(writeCtx2, coderws.MessageText, []byte(`{"type":"session.update","session":{"model":"gpt-4o"}}`)))
	cancelWrite2()

	readCtx2, cancelRead2 := context.WithTimeout(context.Background(), 3*time.Second)
	_, event, readErr2 := clientConn.Read(readCtx2)
	cancelRead2()
	require.NoError(t, readErr2)
	require.Equal(t, "error", gjson.GetBytes(event, "type").String())
	require.Equal(t, "unsupported_oauth_capability", gjson.GetBytes(event, "error.code").String())
	require.Equal(t, "capability.unsupported_oauth_model_profile", gjson.GetBytes(event, "error.category").String())

	readCtx3, cancelRead3 := context.WithTimeout(context.Background(), 3*time.Second)
	_, _, closeErr := clientConn.Read(readCtx3)
	cancelRead3()
	require.Error(t, closeErr)
	require.Equal(t, coderws.StatusPolicyViolation, coderws.CloseStatus(closeErr))

	select {
	case serverErr := <-serverErrCh:
		require.Error(t, serverErr)
		var selectionErr *OpenAIRuntimeGuardSelectionError
		require.ErrorAs(t, serverErr, &selectionErr)
		require.Equal(t, OpenAIRuntimeGuardErrorCodeUnsupportedOAuthCapability, selectionErr.Code)
	case <-time.After(5 * time.Second):
		t.Fatal("waiting for blocked passthrough server")
	}
	require.Len(t, captureConn.writes, 1, "unsupported session.update must not be sent upstream")
	require.Equal(t, "response.create", captureConn.writes[0]["type"])
}

func TestOpenAIRuntimeGuard_WSPassthroughBlocksExplicitUnsupportedFollowupModelBeforeUpstream(t *testing.T) {
	cfg := newOpenAIRuntimeGuardWSTestConfig()
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.IngressModeDefault = OpenAIWSIngressModePassthrough
	captureConn := &openAIWSCaptureConn{
		events: [][]byte{
			[]byte(`{"type":"response.completed","response":{"id":"resp_passthrough_explicit_turn_1","model":"gpt-5.5","usage":{"input_tokens":1,"output_tokens":1}}}`),
		},
		blockWhenEmpty: true,
	}
	svc := newOpenAIRuntimeGuardWSService(cfg, nil)
	svc.openaiWSPassthroughDialer = &openAIWSCaptureDialer{conn: captureConn}
	account := newOpenAIRuntimeGuardWSOAuthAccount(OpenAIWSIngressModePassthrough)

	serverErrCh, clientConn, closeServer := startOpenAIRuntimeGuardWSServer(t, svc, account)
	defer closeServer()
	defer func() { _ = clientConn.CloseNow() }()

	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	require.NoError(t, clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.5","stream":false}`)))
	cancelWrite()
	readCtx, cancelRead := context.WithTimeout(context.Background(), 3*time.Second)
	_, firstEvent, readErr := clientConn.Read(readCtx)
	cancelRead()
	require.NoError(t, readErr)
	require.Equal(t, "response.completed", gjson.GetBytes(firstEvent, "type").String())

	writeCtx2, cancelWrite2 := context.WithTimeout(context.Background(), 3*time.Second)
	require.NoError(t, clientConn.Write(writeCtx2, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-4o","stream":false}`)))
	cancelWrite2()

	readCtx2, cancelRead2 := context.WithTimeout(context.Background(), 3*time.Second)
	_, event, readErr2 := clientConn.Read(readCtx2)
	cancelRead2()
	require.NoError(t, readErr2)
	require.Equal(t, "error", gjson.GetBytes(event, "type").String())
	require.Equal(t, "unsupported_oauth_capability", gjson.GetBytes(event, "error.code").String())
	require.Equal(t, "capability.unsupported_oauth_model_profile", gjson.GetBytes(event, "error.category").String())

	readCtx3, cancelRead3 := context.WithTimeout(context.Background(), 3*time.Second)
	_, _, closeErr := clientConn.Read(readCtx3)
	cancelRead3()
	require.Error(t, closeErr)
	require.Equal(t, coderws.StatusPolicyViolation, coderws.CloseStatus(closeErr))

	select {
	case serverErr := <-serverErrCh:
		require.Error(t, serverErr)
		var selectionErr *OpenAIRuntimeGuardSelectionError
		require.ErrorAs(t, serverErr, &selectionErr)
	case <-time.After(5 * time.Second):
		t.Fatal("waiting for explicit blocked passthrough server")
	}
	require.Len(t, captureConn.writes, 1, "explicit unsupported follow-up model must not be sent upstream")
}

func newOpenAIRuntimeGuardWSTestConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
	cfg.Gateway.OpenAIWS.QueueLimitPerConn = 8
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
	return cfg
}

func newOpenAIRuntimeGuardWSService(cfg *config.Config, pool *openAIWSConnPool) *OpenAIGatewayService {
	return &OpenAIGatewayService{
		cfg:              cfg,
		httpUpstream:     &httpUpstreamRecorder{},
		cache:            &stubGatewayCache{},
		openaiWSResolver: NewOpenAIWSProtocolResolver(cfg),
		toolCorrector:    NewCodexToolCorrector(),
		openaiWSPool:     pool,
	}
}

func newOpenAIRuntimeGuardWSOAuthAccount(mode string) *Account {
	return &Account{
		ID:          6401,
		Name:        "openai-oauth-ws-runtime-guard",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-acc",
		},
		Extra: map[string]any{
			"responses_websockets_v2_enabled":           true,
			"openai_oauth_responses_websockets_v2_mode": mode,
		},
	}
}

func startOpenAIRuntimeGuardWSServer(t *testing.T, svc *OpenAIGatewayService, account *Account) (<-chan error, *coderws.Conn, func()) {
	t.Helper()
	serverErrCh := make(chan error, 1)
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
		if err != nil {
			serverErrCh <- err
			return
		}
		defer func() { _ = conn.CloseNow() }()
		rec := httptest.NewRecorder()
		ginCtx, _ := gin.CreateTestContext(rec)
		req := r.Clone(r.Context())
		req.Header = req.Header.Clone()
		req.Header.Set("User-Agent", "unit-test-agent/1.0")
		ginCtx.Request = req
		readCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		_, firstMessage, readErr := conn.Read(readCtx)
		cancel()
		if readErr != nil {
			serverErrCh <- readErr
			return
		}
		proxyErr := svc.ProxyResponsesWebSocketFromClient(r.Context(), ginCtx, conn, account, "oauth-token", firstMessage, nil)
		var closeErr *OpenAIWSClientCloseError
		if errors.As(proxyErr, &closeErr) {
			_ = conn.Close(closeErr.StatusCode(), closeErr.Reason())
		}
		serverErrCh <- proxyErr
	}))
	dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
	clientConn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(wsServer.URL, "http"), nil)
	cancelDial()
	require.NoError(t, err)
	return serverErrCh, clientConn, wsServer.Close
}

type openAIRuntimeGuardRecordingScheduler struct {
	reportCalls int
}

func (s *openAIRuntimeGuardRecordingScheduler) Select(ctx context.Context, req OpenAIAccountScheduleRequest) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	return nil, OpenAIAccountScheduleDecision{}, errors.New("unexpected scheduler select")
}

func (s *openAIRuntimeGuardRecordingScheduler) ReportResult(accountID int64, success bool, firstTokenMs *int) {
	s.reportCalls++
}

func (s *openAIRuntimeGuardRecordingScheduler) ReportSwitch() {}

func (s *openAIRuntimeGuardRecordingScheduler) SnapshotMetrics() OpenAIAccountSchedulerMetricsSnapshot {
	return OpenAIAccountSchedulerMetricsSnapshot{}
}

func TestCodexGatewayProviderExecutor_OpenAIRuntimeGuardLocalBlockDoesNotRecordProviderSuccessOrUsage(t *testing.T) {
	baseDir := t.TempDir()
	capture := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                  true,
		BaseDir:                  baseDir,
		HashKeyFile:              filepath.Join(baseDir, ".key"),
		CaptureSuccessSampleRate: 1,
	})
	defer capture.Close()
	trace := capture.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{TraceID: "executor_runtime_guard_local_block", Provider: "openai", Model: "gpt-5.5"})
	require.NotNil(t, trace)

	account := newCodexGatewayOpenAIOAuthAccountForTest()
	usageRecorder := &codexGatewayUsageRecorderStub{}
	executor := newCodexGatewayProviderExecutorForTest()
	executor.usageRecorder = usageRecorder
	executor.accountSelector = &codexGatewayProviderExecutorSelectorStub{
		selectFn: func(_ context.Context, _ *int64, _ string, _ string, _ map[int64]struct{}) (*Account, error) {
			return account, nil
		},
	}
	executor.openaiAdapter = &codexGatewayOpenAIResponsesAdapter{gateway: &OpenAIGatewayService{
		cfg:          &config.Config{},
		httpUpstream: &httpUpstreamRecorder{},
	}}

	resp, err := executor.Complete(context.Background(), CodexGatewayProviderRequest{
		Request: CodexGatewayResponsesRequest{
			APIKey: validCodexGatewayAPIKeyForTest(),
			Body:   []byte(`{"model":"gpt-5.5","reasoning_effort":"hyperdrive","input":"hi"}`),
		},
		Model:        CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
		Parsed:       CodexGatewayResponsesCreateRequest{Model: "gpt-5.5"},
		CaptureTrace: trace,
	})

	require.Error(t, err)
	var localResp *codexGatewayLocalServiceResponseError
	require.ErrorAs(t, err, &localResp)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Contains(t, string(resp.Body), "Unsupported reasoning_effort value")
	require.Empty(t, usageRecorder.inputs, "local runtime guard block must not record provider usage")
	capture.FinishTrace(trace, CodexGatewayCaptureFinishSummary{Status: "blocked", HTTPStatus: http.StatusBadRequest})
	require.NoError(t, capture.Close())

	traceDir := filepath.Join(baseDir, time.Now().Format("2006-01-02"), "executor_runtime_guard_local_block")
	for _, name := range []string{"upstream_request.shape.json", "upstream_response.shape.json", "cache_usage.json"} {
		_, statErr := os.Stat(filepath.Join(traceDir, name))
		require.True(t, os.IsNotExist(statErr), "%s must not be written for local runtime guard blocks", name)
	}
}

func TestCodexGatewayService_StreamOpenAIRuntimeGuardLocalBlockDoesNotWriteGenericErrorTwice(t *testing.T) {
	account := newCodexGatewayOpenAIOAuthAccountForTest()
	executor := newCodexGatewayProviderExecutorForTest()
	executor.accountSelector = &codexGatewayProviderExecutorSelectorStub{
		selectFn: func(_ context.Context, _ *int64, _ string, _ string, _ map[int64]struct{}) (*Account, error) {
			return account, nil
		},
	}
	executor.openaiAdapter = &codexGatewayOpenAIResponsesAdapter{gateway: &OpenAIGatewayService{
		cfg:          &config.Config{},
		httpUpstream: &httpUpstreamRecorder{},
	}}

	registry := NewDefaultCodexGatewayModelRegistry()
	svc := NewCodexGatewayService(registry, executor)
	var writer bytes.Buffer
	resp, err := svc.Responses(context.Background(), CodexGatewayResponsesRequest{
		APIKey:       validCodexGatewayAPIKeyForTest(),
		Headers:      http.Header{},
		Body:         []byte(`{"model":"gpt-5.5","stream":true,"reasoning_effort":"hyperdrive","input":"hi"}`),
		StreamWriter: &writer,
		Flush:        func() {},
	})

	require.NoError(t, err)
	require.Nil(t, resp)
	out := writer.String()
	require.Equal(t, 1, strings.Count(out, "event: response.failed"), out)
	require.Contains(t, out, "Unsupported reasoning_effort value")
	require.NotContains(t, out, "upstream request failed", "service layer must not append a generic stream error after local block")
}

func TestCodexGatewayService_CompleteOpenAIRuntimeGuardLocalBlockTraceIsNotOK(t *testing.T) {
	baseDir := t.TempDir()
	capture := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                  true,
		BaseDir:                  baseDir,
		HashKeyFile:              filepath.Join(baseDir, ".key"),
		CaptureSuccessSampleRate: 1,
	})
	defer capture.Close()

	account := newCodexGatewayOpenAIOAuthAccountForTest()
	usageRecorder := &codexGatewayUsageRecorderStub{}
	executor := newCodexGatewayProviderExecutorForTest()
	executor.usageRecorder = usageRecorder
	executor.accountSelector = &codexGatewayProviderExecutorSelectorStub{
		selectFn: func(_ context.Context, _ *int64, _ string, _ string, _ map[int64]struct{}) (*Account, error) {
			return account, nil
		},
	}
	executor.openaiAdapter = &codexGatewayOpenAIResponsesAdapter{gateway: &OpenAIGatewayService{
		cfg:          &config.Config{},
		httpUpstream: &httpUpstreamRecorder{},
	}}

	svc := NewCodexGatewayService(NewDefaultCodexGatewayModelRegistry(), executor, capture)
	resp, err := svc.Responses(context.Background(), CodexGatewayResponsesRequest{
		APIKey:  validCodexGatewayAPIKeyForTest(),
		Headers: http.Header{},
		Body:    []byte(`{"model":"gpt-5.5","stream":false,"reasoning_effort":"hyperdrive","input":"hi"}`),
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Empty(t, usageRecorder.inputs, "local runtime guard block must not record provider usage")
	require.NoError(t, capture.Close())

	dateDir := filepath.Join(baseDir, time.Now().Format("2006-01-02"))
	traceDirs := codexGatewayCaptureTraceDirsForTest(t, dateDir)
	require.Len(t, traceDirs, 1)
	traceDir := filepath.Join(dateDir, traceDirs[0])
	summary, err := os.ReadFile(filepath.Join(traceDir, "summary.json"))
	require.NoError(t, err)
	require.Contains(t, string(summary), `"status": "blocked"`)
	require.NotContains(t, string(summary), `"status": "ok"`, "local runtime guard block must not be traced as provider success")
	for _, name := range []string{"upstream_request.shape.json", "upstream_response.shape.json"} {
		_, statErr := os.Stat(filepath.Join(traceDir, name))
		require.True(t, os.IsNotExist(statErr), "%s must not be written for local runtime guard blocks", name)
	}
}

func TestOpenAIRuntimeGuard_WSHelperGuardsMissingTypeResponseCreateLikePayload(t *testing.T) {
	account := newOpenAIRuntimeGuardWSOAuthAccount(OpenAIWSIngressModeCtxPool)

	_, blocked, err := (&OpenAIGatewayService{}).ApplyOpenAIRuntimeGuardToWSResponseCreatePayload(account, []byte(`{"model":"gpt-5.5","reasoning_effort":"hyperdrive","input":"hi"}`))
	require.NoError(t, err)
	require.NotNil(t, blocked)
	require.Equal(t, http.StatusBadRequest, blocked.StatusCode)

	repaired, blocked, err := (&OpenAIGatewayService{}).ApplyOpenAIRuntimeGuardToWSResponseCreatePayload(account, []byte(`{"model":"gpt-5.5","reasoning_effort":"max","input":"hi"}`))
	require.NoError(t, err)
	require.Nil(t, blocked)
	require.Equal(t, "xhigh", gjson.GetBytes(repaired, "reasoning_effort").String())

	sessionUpdate := []byte(`{"type":"session.update","session":{"model":"gpt-5.5"},"reasoning_effort":"hyperdrive"}`)
	untouched, blocked, err := (&OpenAIGatewayService{}).ApplyOpenAIRuntimeGuardToWSResponseCreatePayload(account, sessionUpdate)
	require.NoError(t, err)
	require.Nil(t, blocked)
	require.JSONEq(t, string(sessionUpdate), string(untouched))
}

func TestOpenAIRuntimeGuard_WSIngressMissingTypeRepairsAndBlocksBeforeUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := newOpenAIRuntimeGuardWSTestConfig()
	captureConn := &openAIWSCaptureConn{
		events: [][]byte{[]byte(`{"type":"response.completed","response":{"id":"resp_missing_type_guard","model":"gpt-5.5","usage":{"input_tokens":1,"output_tokens":1}}}`)},
	}
	pool := newOpenAIWSConnPool(cfg)
	pool.setClientDialerForTest(&openAIWSCaptureDialer{conn: captureConn})
	svc := newOpenAIRuntimeGuardWSService(cfg, pool)
	account := newOpenAIRuntimeGuardWSOAuthAccount(OpenAIWSIngressModeCtxPool)

	serverErrCh, clientConn, closeServer := startOpenAIRuntimeGuardWSServer(t, svc, account)
	defer closeServer()
	defer func() { _ = clientConn.CloseNow() }()
	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	require.NoError(t, clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"model":"gpt-5.5","stream":false,"reasoning_effort":"max","input":"hi"}`)))
	cancelWrite()
	readCtx, cancelRead := context.WithTimeout(context.Background(), 3*time.Second)
	_, _, readErr := clientConn.Read(readCtx)
	cancelRead()
	require.NoError(t, readErr)
	_ = clientConn.Close(coderws.StatusNormalClosure, "done")
	select {
	case serverErr := <-serverErrCh:
		require.NoError(t, serverErr)
	case <-time.After(5 * time.Second):
		t.Fatal("waiting for missing type repair ws server")
	}
	require.Len(t, captureConn.writes, 1)
	require.Equal(t, "response.create", captureConn.writes[0]["type"])
	require.Equal(t, "xhigh", captureConn.writes[0]["reasoning_effort"])

	blockedConn := &openAIWSCaptureConn{}
	pool = newOpenAIWSConnPool(cfg)
	pool.setClientDialerForTest(&openAIWSCaptureDialer{conn: blockedConn})
	svc = newOpenAIRuntimeGuardWSService(cfg, pool)
	serverErrCh2, clientConn2, closeServer2 := startOpenAIRuntimeGuardWSServer(t, svc, account)
	defer closeServer2()
	defer func() { _ = clientConn2.CloseNow() }()
	writeCtx2, cancelWrite2 := context.WithTimeout(context.Background(), 3*time.Second)
	require.NoError(t, clientConn2.Write(writeCtx2, coderws.MessageText, []byte(`{"model":"gpt-5.5","stream":false,"reasoning_effort":"hyperdrive","input":"hi"}`)))
	cancelWrite2()
	readCtx2, cancelRead2 := context.WithTimeout(context.Background(), 3*time.Second)
	_, event, readErr2 := clientConn2.Read(readCtx2)
	cancelRead2()
	require.NoError(t, readErr2)
	require.Equal(t, "error", gjson.GetBytes(event, "type").String())
	readCtx3, cancelRead3 := context.WithTimeout(context.Background(), 3*time.Second)
	_, _, closeErr := clientConn2.Read(readCtx3)
	cancelRead3()
	require.Error(t, closeErr)
	require.Equal(t, coderws.StatusPolicyViolation, coderws.CloseStatus(closeErr))
	select {
	case serverErr := <-serverErrCh2:
		require.Error(t, serverErr)
	case <-time.After(5 * time.Second):
		t.Fatal("waiting for missing type block ws server")
	}
	require.Empty(t, blockedConn.writes)
}
