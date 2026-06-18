package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIRuntimeGuardCapabilityFixturesCaptureReasoningPolicy(t *testing.T) {
	repaired := openAIRuntimeGuardFixtureByID(t, "reasoning_effort_max_repaired_to_xhigh")
	requireOpenAIRuntimeGuardDecision(t, repaired, "repair", 1)
	require.NotNil(t, repaired.Expect.Repair)
	require.Equal(t, "reasoning_effort", repaired.Expect.Repair.Path)
	require.Equal(t, "max", repaired.Expect.Repair.From)
	require.Equal(t, "xhigh", repaired.Expect.Repair.To)
	require.Equal(t, "max", openAIRuntimeGuardFixtureRequest(t, repaired)["reasoning_effort"])
	require.Equal(t, "xhigh", openAIRuntimeGuardFixtureForward(t, repaired)["reasoning_effort"])

	blocked := openAIRuntimeGuardFixtureByID(t, "reasoning_effort_unknown_local_400")
	requireOpenAIRuntimeGuardDecision(t, blocked, "block", 0)
	require.Equal(t, 400, blocked.Expect.Status)
}

func TestOpenAIRuntimeGuardCapabilityFixturesCoverImageAndSchedulerBlocks(t *testing.T) {
	cases := []struct {
		id       string
		decision string
		status   int
		category string
	}{
		{"image_generation_disabled_by_group_local_block", "block", 403, "capability.image_generation_disabled_by_group"},
		{"native_image_request_no_oauth_basic_fallback", "block", 400, "capability.native_image_no_oauth_basic_fallback"},
		{"unsupported_oauth_model_profile_scheduler_reject", "scheduler_reject", 503, "capability.unsupported_oauth_model_profile"},
	}

	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			fixture := openAIRuntimeGuardFixtureByID(t, tc.id)
			requireOpenAIRuntimeGuardDecision(t, fixture, tc.decision, 0)
			require.Equal(t, tc.status, fixture.Expect.Status)
			require.Equal(t, tc.category, fixture.Expect.Category)
		})
	}
}

func TestOpenAIRuntimeGuardCapabilityFixturesCaptureTokenInvalidation(t *testing.T) {
	fixture := openAIRuntimeGuardFixtureByID(t, "token_invalidated_account_terminal_needs_relogin")
	requireOpenAIRuntimeGuardDecision(t, fixture, "account_terminal", 0)
	require.NotNil(t, fixture.UpstreamError)
	require.Equal(t, 401, fixture.UpstreamError.Status)
	require.Equal(t, "token_invalidated", fixture.UpstreamError.Code)
	require.Equal(t, "needs_relogin", fixture.Expect.AccountState)
}

func TestOpenAIRuntimeGuardCapabilityFixturesCaptureContextPolicy(t *testing.T) {
	over := openAIRuntimeGuardFixtureByID(t, "obviously_over_context_local_shadow_decision")
	requireOpenAIRuntimeGuardDecision(t, over, "shadow_block", 0)
	require.NotNil(t, over.Expect.Context)
	require.Equal(t, "high", over.Expect.Context.Confidence)
	require.Greater(t, over.Expect.Context.EstimatedTokens, over.Expect.Context.LimitTokens)

	near := openAIRuntimeGuardFixtureByID(t, "near_boundary_context_uncertain_not_blocked")
	requireOpenAIRuntimeGuardDecision(t, near, "pass", 1)
	require.NotNil(t, near.Expect.Context)
	require.Equal(t, "uncertain", near.Expect.Context.Confidence)
	require.Less(t, near.Expect.Context.EstimatedTokens, near.Expect.Context.LimitTokens)
}
