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
