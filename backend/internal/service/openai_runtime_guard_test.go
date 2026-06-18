package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
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
