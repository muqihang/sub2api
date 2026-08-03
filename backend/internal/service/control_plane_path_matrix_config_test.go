package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestControlPlanePathPolicyMatrixOverlayAllowsOnlySafeStubGet(t *testing.T) {
	matrix, err := NewControlPlanePathPolicyMatrixFromConfig(ControlPlanePathPolicyMatrixConfig{
		Policies: []ControlPlanePathPolicyConfig{
			{
				Method: "GET", PathTemplate: "/api/claude_cli/safe_profile", Classification: "safe_profile_stubbed",
				Action: ControlPlaneActionStub, CacheScope: "session", TTLSeconds: 60, QuarantineOnMismatch: true, RawForbidden: true,
				QueryAllowlist:      map[string]ControlPlaneQueryRuleConfig{"entrypoint": {Kind: "enum", EnumValues: []string{"sdk-cli"}}},
				AllowedResponseKeys: []string{"ok", "profile"},
			},
		},
	})
	require.NoError(t, err)

	decision := matrix.Evaluate("GET", "/api/claude_cli/safe_profile", "entrypoint=sdk-cli")
	require.True(t, decision.Allowed)
	require.Equal(t, ControlPlaneActionStub, decision.Decision)
	require.Equal(t, time.Minute, decision.Policy.TTL)
	require.True(t, decision.Policy.RequiresSessionPartition)
	require.True(t, decision.Policy.RequiresUserPartition)
	require.Contains(t, decision.Policy.AllowedResponseKeys, "profile")
}

func TestControlPlaneIntentServiceFromEnvUsesSafeMatrixOverlay(t *testing.T) {
	cfg := ControlPlanePathPolicyMatrixConfig{Policies: []ControlPlanePathPolicyConfig{
		{
			Method: "GET", PathTemplate: "/api/claude_cli/safe_profile", Classification: "safe_profile_stubbed",
			Action: ControlPlaneActionStub, CacheScope: "session", TTLSeconds: 60, QuarantineOnMismatch: true, RawForbidden: true,
			QueryAllowlist:      map[string]ControlPlaneQueryRuleConfig{"entrypoint": {Kind: "enum", EnumValues: []string{"sdk-cli"}}},
			AllowedResponseKeys: []string{"ok", "profile"},
		},
	}}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	t.Setenv(ControlPlanePathMatrixJSONEnv, string(raw))

	svc, err := NewControlPlaneIntentServiceFromEnv()
	require.NoError(t, err)
	payload := baseControlPlaneIntentPayload()
	payload["path_template"] = "/api/claude_cli/safe_profile"
	payload["classification"] = "safe_profile_stubbed"
	payload["normalized_query"] = map[string]string{"entrypoint": "sdk-cli"}

	body, err := json.Marshal(payload)
	require.NoError(t, err)
	decision, err := svc.EvaluateIntent(body)
	require.NoError(t, err)
	require.Equal(t, ControlPlaneActionStub, decision.Decision)
	require.Equal(t, map[string]any{"ok": true, "profile": map[string]any{}}, decision.Body)
}

func TestControlPlaneIntentServiceFromEnvRejectsUnsafeMatrix(t *testing.T) {
	t.Setenv(ControlPlanePathMatrixJSONEnv, `{"policies":[{"method":"GET","path_template":"/api/claude_cli/safe_profile","classification":"safe_profile_stubbed","action":"stub_json","cache_scope":"session","ttl_seconds":60,"quarantine_on_mismatch":true,"raw_forbidden":false,"allowed_response_keys":["ok"]}]}`)

	_, err := NewControlPlaneIntentServiceFromEnv()
	require.Error(t, err)
}

func TestControlPlanePathPolicyMatrixOverlayRejectsRawWildcardAndUnsafeForward(t *testing.T) {
	cases := []ControlPlanePathPolicyConfig{
		{Method: "GET", PathTemplate: "/api/*", Classification: "wildcard", Action: ControlPlaneActionStub, CacheScope: "session", TTLSeconds: 60, QuarantineOnMismatch: true, RawForbidden: true, AllowedResponseKeys: []string{"ok"}},
		{Method: "POST", PathTemplate: "/api/claude_cli/safe_profile", Classification: "post_forward", Action: ControlPlaneActionForward, CacheScope: "none", TTLSeconds: 0, QuarantineOnMismatch: true, RawForbidden: true, AllowedResponseKeys: []string{"ok"}},
		{Method: "GET", PathTemplate: "/api/claude_cli/safe_profile", Classification: "raw_allowed", Action: ControlPlaneActionStub, CacheScope: "session", TTLSeconds: 60, QuarantineOnMismatch: true, RawForbidden: false, AllowedResponseKeys: []string{"ok"}},
		{Method: "GET", PathTemplate: "/api/claude_cli/safe_profile", Classification: "sensitive_response", Action: ControlPlaneActionStub, CacheScope: "session", TTLSeconds: 60, QuarantineOnMismatch: true, RawForbidden: true, AllowedResponseKeys: []string{"ok", "email"}},
	}

	for _, cfg := range cases {
		t.Run(cfg.Classification, func(t *testing.T) {
			_, err := NewControlPlanePathPolicyMatrixFromConfig(ControlPlanePathPolicyMatrixConfig{Policies: []ControlPlanePathPolicyConfig{cfg}})
			require.Error(t, err)
		})
	}
}
