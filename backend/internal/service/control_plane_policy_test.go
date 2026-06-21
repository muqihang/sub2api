package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestControlPlanePathPolicyMatrixCanonicalizesQueryDeterministically(t *testing.T) {
	matrix := NewDefaultControlPlanePathPolicyMatrix()
	decisionA := matrix.Evaluate("GET", "/api/claude_cli/bootstrap", "feature=oauth&entrypoint=sdk%2Dcli&feature=mcp")
	decisionB := matrix.Evaluate("GET", "/api/claude_cli/bootstrap", "ENTRYPOINT=sdk-cli&feature=mcp&feature=oauth")

	require.True(t, decisionA.Allowed)
	require.True(t, decisionB.Allowed)
	require.Equal(t, decisionA.NormalizedQuery, decisionB.NormalizedQuery)
	require.Equal(t, map[string]string{"entrypoint": "sdk-cli", "feature": "mcp,oauth"}, decisionA.NormalizedQuery)
}

func TestControlPlanePathPolicyMatrixQuarantinesUnknownAndNonCanonicalQuery(t *testing.T) {
	matrix := NewDefaultControlPlanePathPolicyMatrix()

	unknown := matrix.Evaluate("GET", "/api/claude_cli/bootstrap", "entrypoint=sdk-cli&account_id=acct")
	require.False(t, unknown.Allowed)
	require.Equal(t, "quarantine_block", unknown.Decision)

	repeated := matrix.Evaluate("GET", "/v1/mcp_servers", "limit=1&limit=2")
	require.False(t, repeated.Allowed)
	require.Contains(t, repeated.Reason, "repeated_query_key")

	nested := matrix.Evaluate("GET", "/api/claude_cli/bootstrap", "feature[]=mcp")
	require.False(t, nested.Allowed)
	require.Contains(t, nested.Reason, "nested_or_empty_query_key")

	empty := matrix.Evaluate("GET", "/api/claude_cli/bootstrap", "entrypoint=")
	require.False(t, empty.Allowed)
	require.Contains(t, empty.Reason, "empty_query_value")
}

func TestControlPlanePathPolicyMatrixDoesNotUseProductionWildcardAllowlist(t *testing.T) {
	matrix := NewDefaultControlPlanePathPolicyMatrix()
	decision := matrix.Evaluate("GET", "/api/claude_code_unknown", "")
	require.False(t, decision.Allowed)
	require.Equal(t, "control_plane:path_not_allowlisted", decision.Reason)
}

func TestControlPlanePathPolicyMatrixSensitiveAccountSettingsNoStaleNoForward(t *testing.T) {
	matrix := NewDefaultControlPlanePathPolicyMatrix()
	decision := matrix.Evaluate("GET", "/api/oauth/account/settings", "")
	require.False(t, decision.Allowed)
	require.Equal(t, ControlPlaneStaleNoStale, decision.Policy.StaleMode)
	require.False(t, decision.Policy.Cacheable)
	require.True(t, decision.Policy.Sensitive)
}

func TestControlPlanePathPolicyMatrixCoversDoc45KnownControlPlanePaths(t *testing.T) {
	matrix := NewDefaultControlPlanePathPolicyMatrix()

	stubbed := []struct {
		method string
		path   string
		query  string
	}{
		{"GET", "/api/hello", ""},
		{"GET", "/v1/oauth/hello", ""},
		{"GET", "/v1/models", "limit=1000"},
		{"GET", "/mcp-registry/v0/servers", "version=latest"},
		{"GET", "/api/claude_code_penguin_mode", ""},
		{"GET", "/api/claude_code_feature_flags", ""},
		{"GET", "/api/claude_code_grove", ""},
		{"GET", "/api/claude_code/organizations/metrics_enabled", ""},
	}
	for _, tc := range stubbed {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			decision := matrix.Evaluate(tc.method, tc.path, tc.query)
			require.True(t, decision.Allowed)
			require.NotNil(t, decision.Policy)
			require.Equal(t, ControlPlaneActionStub, decision.Decision)
			require.True(t, decision.Policy.Cacheable)
			require.Equal(t, ControlPlaneStaleSafe, decision.Policy.StaleMode)
			require.True(t, decision.Policy.RequiresUserPartition)
			require.Contains(t, decision.Policy.PrivateFieldDenylist, "authorization")
		})
	}

	blocked := []struct {
		method string
		path   string
	}{
		{"GET", "/api/claude_code/policy_limits"},
		{"GET", "/api/claude_code/remote_managed_settings"},
		{"GET", "/api/claude_code/settings_sync"},
		{"GET", "/api/claude_code/team_memory"},
		{"GET", "/api/claude_code/model_capabilities"},
		{"GET", "/api/claude_code/growthbook"},
		{"GET", "/api/oauth/organizations/{org}/referral/eligibility"},
	}
	for _, tc := range blocked {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			decision := matrix.Evaluate(tc.method, tc.path, "")
			require.False(t, decision.Allowed)
			require.NotNil(t, decision.Policy)
			require.Equal(t, ControlPlaneActionBlock, decision.Decision)
			require.Equal(t, ControlPlaneStaleNoStale, decision.Policy.StaleMode)
			require.False(t, decision.Policy.Cacheable)
			require.True(t, decision.Policy.Sensitive)
		})
	}
}
