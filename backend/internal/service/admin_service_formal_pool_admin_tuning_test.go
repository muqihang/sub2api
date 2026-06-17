package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergeFormalPoolAdminState_AdminTuningPreservesOAuthAndRuntimeState(t *testing.T) {
	t.Parallel()

	extra := map[string]any{
		"onboarding_state":                         FormalPoolOnboardingStatusWarming,
		FormalPoolExtraOnboardingStage:             FormalPoolStageWarming,
		FormalPoolExtraOnboardingStageUpdatedAt:    "2026-05-30T00:00:00Z",
		FormalPoolExtraHealthcheckStatus:           "passed",
		FormalPoolExtraHealthcheckStatusCodeBucket: "status_2xx",
		FormalPoolExtraHealthcheckRawRef:           "safe-raw-ref",
		FormalPoolExtraLastHealthcheckAt:           "2026-05-30T00:00:00Z",
		FormalPoolExtraLastHealthcheckResult:       "passed",
		FormalPoolExtraRuntimeRegistered:           "true",
		FormalPoolExtraRuntimeRegisteredAt:         "2026-05-30T00:00:00Z",
		"cc_gateway_enabled":                       "true",
		"cc_gateway_routes":                        "native_messages",
		"cc_gateway_policy_version":                "2.1.175",
		"cc_gateway_account_ref":                   "hmac-sha256:" + strings.Repeat("c", 64),
		"cc_gateway_egress_bucket_enabled":         "true",
		"cc_gateway_egress_bucket":                 "claude-hmac-sha256:155",
		"oauth_refresh_fail_closed":                "true",
		"risk_event_ref":                           "opaque:risk:v1:keep",
	}

	mergedExtra := mergeFormalPoolAdminExtra(extra, map[string]any{
		"base_rpm":                     17,
		"rpm_strategy":                 "tiered",
		"max_sessions":                 5,
		"session_idle_timeout_minutes": 5,
		// Simulate a stale/desynced admin form trying to blank runtime keys.
		"cc_gateway_account_ref":           "",
		"cc_gateway_runtime_registered":    "false",
		"cc_gateway_runtime_registered_at": "",
		"onboarding_stage":                 "imported",
	})
	mergedCreds := mergeFormalPoolAdminCredentials(map[string]any{
		"access_token":  "access-secret",
		"refresh_token": "refresh-secret",
		"scope":         "user:profile user:inference user:sessions:claude_code",
	}, map[string]any{"intercept_warmup_requests": true})

	require.Equal(t, 17, mergedExtra["base_rpm"])
	require.Equal(t, "tiered", mergedExtra["rpm_strategy"])
	require.Equal(t, 5, mergedExtra["max_sessions"])
	require.Equal(t, 5, mergedExtra["session_idle_timeout_minutes"])
	require.Equal(t, "access-secret", mergedCreds["access_token"])
	require.Equal(t, "refresh-secret", mergedCreds["refresh_token"])
	require.Equal(t, "user:profile user:inference user:sessions:claude_code", mergedCreds["scope"])
	require.Equal(t, true, mergedCreds["intercept_warmup_requests"])
	require.Equal(t, FormalPoolStageWarming, mergedExtra[FormalPoolExtraOnboardingStage])
	require.Equal(t, "true", mergedExtra[FormalPoolExtraRuntimeRegistered])
	require.Equal(t, "2026-05-30T00:00:00Z", mergedExtra[FormalPoolExtraRuntimeRegisteredAt])
	require.Equal(t, "hmac-sha256:"+strings.Repeat("c", 64), mergedExtra["cc_gateway_account_ref"])
	require.Equal(t, "claude-hmac-sha256:155", mergedExtra["cc_gateway_egress_bucket"])
	require.Equal(t, "true", mergedExtra["oauth_refresh_fail_closed"])
	require.Equal(t, "opaque:risk:v1:keep", mergedExtra["risk_event_ref"])
}
