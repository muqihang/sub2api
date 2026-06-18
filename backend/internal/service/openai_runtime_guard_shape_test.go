package service

import (
	"testing"

	"github.com/stretchr/testify/require"
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
