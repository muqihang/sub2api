package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestControlPlaneQuarantineKillSwitches(t *testing.T) {
	manager := NewControlPlaneQuarantineManager()
	input := baseControlPlaneCacheInput()

	require.True(t, manager.Check(input).Allowed)
	manager.KillPath(input.PathTemplate, "manual")
	require.False(t, manager.Check(input).Allowed)

	manager = NewControlPlaneQuarantineManager()
	manager.KillAccount(input.AccountScope, "manual")
	require.False(t, manager.Check(input).Allowed)

	manager = NewControlPlaneQuarantineManager()
	manager.KillProfile(input.PersonaProfile, "manual")
	require.False(t, manager.Check(input).Allowed)
}

func TestControlPlaneQuarantineObserveResponseFusesRiskStatuses(t *testing.T) {
	for _, status := range []int{401, 403, 429} {
		manager := NewControlPlaneQuarantineManager()
		input := baseControlPlaneCacheInput()
		decision := manager.ObserveResponse(input, status, false, false)
		require.False(t, decision.Allowed)
		require.False(t, manager.Check(input).Allowed)
	}

	manager := NewControlPlaneQuarantineManager()
	input := baseControlPlaneCacheInput()
	require.False(t, manager.ObserveResponse(input, 200, true, false).Allowed)

	manager = NewControlPlaneQuarantineManager()
	require.False(t, manager.ObserveResponse(input, 200, false, true).Allowed)
}

func TestControlPlaneResponseSchemaAllowlistAndPrivateFieldScan(t *testing.T) {
	policy := ControlPlanePathPolicy{
		AllowedResponseKeys:  stringSet("data", "ok"),
		PrivateFieldDenylist: defaultControlPlanePrivateFieldDenylist(),
	}
	require.NoError(t, ValidateControlPlaneResponseSchema(policy, map[string]any{"data": []any{}, "ok": true}))
	require.Error(t, ValidateControlPlaneResponseSchema(policy, map[string]any{"unexpected": true}))
	require.Error(t, ValidateControlPlaneResponseSchema(policy, map[string]any{"data": []any{map[string]any{"email": "redacted"}}}))
}
