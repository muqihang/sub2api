package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
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
	require.JSONEq(t, `{"error":{"type":"invalid_request_error","message":"Unsupported reasoning_effort value","param":"reasoning_effort"}}`, rec.Body.String())
	requireOpenAIRuntimeGuardMetadata(t, c, "block", "reasoning.unknown_effort", "openai_runtime_guard.blocked.reasoning_effort")
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

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
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
