package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIRuntimeGuardShapeFixturesDescribeRoleAwareContentRepairs(t *testing.T) {
	assistant := openAIRuntimeGuardFixtureByID(t, "assistant_history_input_text_repaired_to_output_text")
	requireOpenAIRuntimeGuardDecision(t, assistant, "repair", 1)
	require.Equal(t, "input_text", openAIRuntimeGuardContentPart(t, openAIRuntimeGuardInputItem(t, openAIRuntimeGuardFixtureRequest(t, assistant), 0), 0)["type"])
	require.Equal(t, "output_text", openAIRuntimeGuardContentPart(t, openAIRuntimeGuardInputItem(t, openAIRuntimeGuardFixtureForward(t, assistant), 0), 0)["type"])

	nonAssistant := openAIRuntimeGuardFixtureByID(t, "non_assistant_input_text_roles_not_converted")
	requireOpenAIRuntimeGuardDecision(t, nonAssistant, "pass", 1)
	forward := openAIRuntimeGuardFixtureForward(t, nonAssistant)
	for i, role := range []string{"user", "developer", "system"} {
		item := openAIRuntimeGuardInputItem(t, forward, i)
		require.Equal(t, role, item["role"])
		require.Equal(t, "input_text", openAIRuntimeGuardContentPart(t, item, 0)["type"])
	}
}

func TestOpenAIRuntimeGuardShapeFixturesScopeToolWrapperRepair(t *testing.T) {
	chatgptInternal := openAIRuntimeGuardFixtureByID(t, "tools_function_wrapper_removed_for_chatgpt_internal_codex_oauth")
	requireOpenAIRuntimeGuardDecision(t, chatgptInternal, "repair", 1)
	_, requestWrapped := openAIRuntimeGuardFirstTool(t, openAIRuntimeGuardFixtureRequest(t, chatgptInternal))["function"]
	require.True(t, requestWrapped)
	forwardTool := openAIRuntimeGuardFirstTool(t, openAIRuntimeGuardFixtureForward(t, chatgptInternal))
	require.Equal(t, "run_check", forwardTool["name"])
	require.NotContains(t, forwardTool, "function")

	native := openAIRuntimeGuardFixtureByID(t, "tools_function_wrapper_preserved_for_native_responses_profile")
	requireOpenAIRuntimeGuardDecision(t, native, "pass", 1)
	_, nativeForwardWrapped := openAIRuntimeGuardFirstTool(t, openAIRuntimeGuardFixtureForward(t, native))["function"]
	require.True(t, nativeForwardWrapped)
}

func TestOpenAIRuntimeGuardShapeFixturesPreserveFunctionCallArgumentsString(t *testing.T) {
	fixture := openAIRuntimeGuardFixtureByID(t, "function_call_arguments_string_preserved_native_shapes")
	requireOpenAIRuntimeGuardDecision(t, fixture, "pass", 1)

	profiles, ok := fixture.Profile["profiles"].([]any)
	require.True(t, ok)
	require.ElementsMatch(t, []any{"responses_native", "codex_native", "wsv2_native"}, profiles)

	requestCall := openAIRuntimeGuardInputItem(t, openAIRuntimeGuardFixtureRequest(t, fixture), 1)
	forwardCall := openAIRuntimeGuardInputItem(t, openAIRuntimeGuardFixtureForward(t, fixture), 1)
	require.IsType(t, "", requestCall["arguments"])
	require.IsType(t, "", forwardCall["arguments"])
	require.Equal(t, requestCall["arguments"], forwardCall["arguments"])
}

func TestOpenAIRuntimeGuardShapeFixturesBlockMissingToolOutputLocally(t *testing.T) {
	fixture := openAIRuntimeGuardFixtureByID(t, "missing_tool_output_local_block")
	requireOpenAIRuntimeGuardDecision(t, fixture, "block", 0)
	require.Equal(t, 400, fixture.Expect.Status)
	require.Equal(t, "shape.missing_tool_output", fixture.Expect.Category)
}

func TestOpenAIRuntimeGuardShape_ObservedAssistantInputTextRepairedBeforeNativeUpstream(t *testing.T) {
	upstream := newOpenAIRuntimeGuardShapeUpstream()
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := newOpenAIRuntimeGuardShapeOAuthAccount()
	inputItems := make([]string, 23)
	for i := range inputItems {
		inputItems[i] = `{"type":"message","role":"user","content":[{"type":"input_text","text":"u"}]}`
	}
	inputItems[22] = `{"type":"message","role":"assistant","content":[{"type":"input_text","text":"observed assistant history"}]}`
	body := []byte(`{"model":"gpt-5.4","input":[` + strings.Join(inputItems, ",") + `]}`)

	resp, err := svc.DoNativeResponsesRequest(context.Background(), account, nil, body, false)

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, upstream.bodies, 1, "repair path must call upstream exactly once")
	require.Equal(t, "output_text", gjson.GetBytes(upstream.lastBody, "input.22.content.0.type").String())
	require.Equal(t, "observed assistant history", gjson.GetBytes(upstream.lastBody, "input.22.content.0.text").String())
}

func TestOpenAIRuntimeGuardShape_UserDeveloperSystemInputTextPreservedAndAssistantRefusalPreserved(t *testing.T) {
	upstream := newOpenAIRuntimeGuardShapeUpstream()
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := newOpenAIRuntimeGuardShapeOAuthAccount()
	body := []byte(`{"model":"gpt-5.4","input":[` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"user text"}]},` +
		`{"type":"message","role":"developer","content":[{"type":"input_text","text":"developer text"}]},` +
		`{"type":"message","role":"system","content":[{"type":"input_text","text":"system text"}]},` +
		`{"type":"message","role":"assistant","content":[{"type":"refusal","refusal":"cannot comply"}]}` +
		`]}`)

	resp, err := svc.DoNativeResponsesRequest(context.Background(), account, nil, body, false)

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, upstream.bodies, 1)
	for i, role := range []string{"user", "developer", "system"} {
		require.Equal(t, role, gjson.GetBytes(upstream.lastBody, fmt.Sprintf("input.%d.role", i)).String())
		require.Equal(t, "input_text", gjson.GetBytes(upstream.lastBody, fmt.Sprintf("input.%d.content.0.type", i)).String())
	}
	require.Equal(t, "refusal", gjson.GetBytes(upstream.lastBody, "input.3.content.0.type").String())
	require.Equal(t, "cannot comply", gjson.GetBytes(upstream.lastBody, "input.3.content.0.refusal").String())
}

func TestOpenAIRuntimeGuardShape_AssistantInputImageBlockedBeforeNativeUpstream(t *testing.T) {
	upstream := newOpenAIRuntimeGuardShapeUpstream()
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := newOpenAIRuntimeGuardShapeOAuthAccount()
	body := []byte(`{"model":"gpt-5.4","input":[{"type":"message","role":"assistant","content":[{"type":"input_image","image_url":"data:image/png;base64,abcd"}]}]}`)

	resp, err := svc.DoNativeResponsesRequest(context.Background(), account, nil, body, false)

	require.Nil(t, resp)
	require.Error(t, err)
	var blocked *OpenAIRuntimeGuardBlockedError
	require.ErrorAs(t, err, &blocked)
	require.Equal(t, http.StatusBadRequest, blocked.StatusCode)
	require.Equal(t, "shape.assistant_input_content_blocked", blocked.Decision.Category)
	require.Equal(t, "local_policy_block", gjson.GetBytes(blocked.Payload, "error.code").String())
	require.Equal(t, "capability.local_policy_block", gjson.GetBytes(blocked.Payload, "error.category").String())
	serviceResp := codexGatewayOpenAIRuntimeGuardServiceResponse(blocked)
	require.Equal(t, "local_policy_block", gjson.GetBytes(serviceResp.Body, "error.code").String())
	require.Equal(t, "capability.local_policy_block", gjson.GetBytes(serviceResp.Body, "error.category").String())
	require.Len(t, upstream.bodies, 0, "local shape block must not call upstream")
}

func TestOpenAIRuntimeGuardShape_AssistantInputImageHTTPPayloadIncludesStableLocalBlockMetadata(t *testing.T) {
	upstream, rec, c, svc, account := newOpenAIRuntimeGuardForwardHarness(t)
	body := []byte(`{"model":"gpt-5.4","input":[{"type":"message","role":"assistant","content":[{"type":"input_image","image_url":"data:image/png;base64,abcd"}]}]}`)

	result, err := svc.Forward(context.Background(), c, account, body)

	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "local_policy_block", gjson.Get(rec.Body.String(), "error.code").String())
	require.Equal(t, "capability.local_policy_block", gjson.Get(rec.Body.String(), "error.category").String())
	require.Len(t, upstream.bodies, 0, "local shape block must not call upstream")
}

func TestOpenAIRuntimeGuardShape_ToolOutputMissingContextBlockedBeforeNativeUpstream(t *testing.T) {
	upstream := newOpenAIRuntimeGuardShapeUpstream()
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := newOpenAIRuntimeGuardShapeOAuthAccount()
	body := []byte(`{"model":"gpt-5.4","input":[{"type":"function_call_output","call_id":"call_missing","output":"{}"}]}`)

	resp, err := svc.DoNativeResponsesRequest(context.Background(), account, nil, body, false)

	require.Nil(t, resp)
	require.Error(t, err)
	var blocked *OpenAIRuntimeGuardBlockedError
	require.ErrorAs(t, err, &blocked)
	require.Equal(t, "shape.tool_output_missing_context", blocked.Decision.Category)
	require.Len(t, upstream.bodies, 0, "local missing-context block must not call upstream")
}

func TestOpenAIRuntimeGuardShape_MixedPairedAndOrphanToolOutputBlockedBeforeNativeUpstream(t *testing.T) {
	upstream := newOpenAIRuntimeGuardShapeUpstream()
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := newOpenAIRuntimeGuardShapeOAuthAccount()
	body := []byte(`{"model":"gpt-5.4","input":[` +
		`{"type":"function_call","call_id":"call_ok","name":"run_check","arguments":"{}"},` +
		`{"type":"function_call_output","call_id":"call_ok","output":"{}"},` +
		`{"type":"function_call_output","call_id":"call_orphan","output":"{}"}` +
		`]}`)

	resp, err := svc.DoNativeResponsesRequest(context.Background(), account, nil, body, false)

	require.Nil(t, resp)
	require.Error(t, err)
	var blocked *OpenAIRuntimeGuardBlockedError
	require.ErrorAs(t, err, &blocked)
	require.Equal(t, "shape.tool_output_missing_context", blocked.Decision.Category)
	require.Len(t, upstream.bodies, 0, "mixed paired/orphan tool output must not call upstream")
}

func TestOpenAIRuntimeGuardShape_ToolOutputWithPreviousResponseIDPassesNativeUpstream(t *testing.T) {
	upstream := newOpenAIRuntimeGuardShapeUpstream()
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := newOpenAIRuntimeGuardShapeOAuthAccount()
	body := []byte(`{"model":"gpt-5.4","previous_response_id":"resp_prev","input":[{"type":"function_call_output","call_id":"call_prev","output":"{}"}]}`)

	resp, err := svc.DoNativeResponsesRequest(context.Background(), account, nil, body, false)

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, upstream.bodies, 1, "previous_response_id provides the missing tool-call context")
	require.Equal(t, "function_call_output", gjson.GetBytes(upstream.lastBody, "input.0.type").String())
	require.Equal(t, "call_prev", gjson.GetBytes(upstream.lastBody, "input.0.call_id").String())
}

func TestOpenAIRuntimeGuardShape_UnpairedToolCallBlockedBeforeNativeUpstream(t *testing.T) {
	upstream := newOpenAIRuntimeGuardShapeUpstream()
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := newOpenAIRuntimeGuardShapeOAuthAccount()
	body := []byte(`{"model":"gpt-5.4","input":[{"type":"function_call","call_id":"call_missing","name":"run_check","arguments":"{}"}]}`)

	resp, err := svc.DoNativeResponsesRequest(context.Background(), account, nil, body, false)

	require.Nil(t, resp)
	require.Error(t, err)
	var blocked *OpenAIRuntimeGuardBlockedError
	require.ErrorAs(t, err, &blocked)
	require.Equal(t, "shape.missing_tool_output", blocked.Decision.Category)
	require.Len(t, upstream.bodies, 0, "local unpaired tool call block must not call upstream")
}

func TestOpenAIRuntimeGuardShape_WSHelperRepairsAssistantInputText(t *testing.T) {
	account := newOpenAIRuntimeGuardWSOAuthAccount(OpenAIWSIngressModeCtxPool)
	payload := []byte(`{"type":"response.create","model":"gpt-5.4","input":[{"type":"message","role":"assistant","content":[{"type":"input_text","text":"history"}]}]}`)

	repaired, blocked, err := applyOpenAIReasoningEffortGuardToWSResponseCreatePayload(account, payload)

	require.NoError(t, err)
	require.Nil(t, blocked)
	require.Equal(t, "output_text", gjson.GetBytes(repaired, "input.0.content.0.type").String())
}

func TestOpenAIRuntimeGuardShape_WSHelperBlocksAssistantInputImage(t *testing.T) {
	account := newOpenAIRuntimeGuardWSOAuthAccount(OpenAIWSIngressModeCtxPool)
	payload := []byte(`{"type":"response.create","model":"gpt-5.4","input":[{"type":"message","role":"assistant","content":[{"type":"input_image","image_url":"data:image/png;base64,abcd"}]}]}`)

	repaired, blocked, err := applyOpenAIReasoningEffortGuardToWSResponseCreatePayload(account, payload)

	require.NoError(t, err)
	require.NotNil(t, blocked)
	require.Equal(t, "shape.assistant_input_content_blocked", blocked.Decision.Category)
	event := buildOpenAIRuntimeGuardBlockedWSEvent(blocked)
	require.Equal(t, "local_policy_block", gjson.GetBytes(event, "error.code").String())
	require.Equal(t, "capability.local_policy_block", gjson.GetBytes(event, "error.category").String())
	require.Equal(t, string(payload), string(repaired))
}

func newOpenAIRuntimeGuardShapeUpstream() *httpUpstreamRecorder {
	return &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"rid_shape"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"resp_shape","object":"response","usage":{"input_tokens":1,"output_tokens":1}}`)),
	}}
}

func newOpenAIRuntimeGuardShapeOAuthAccount() *Account {
	return &Account{
		ID:          6401,
		Name:        "openai-oauth-shape",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token", "chatgpt_account_id": "chatgpt-acc"},
	}
}
