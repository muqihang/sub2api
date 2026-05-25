package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestControlPlaneCacheKeyIncludesRequiredFieldsAndUsesScopedHMAC(t *testing.T) {
	key, err := NewControlPlaneCacheKey(baseControlPlaneCacheInput())
	require.NoError(t, err)
	require.Equal(t, "control_plane_cache_key", key.Scope)
	require.Contains(t, key.Value, "hmac-sha256:")

	encoded, err := json.Marshal(key.Components)
	require.NoError(t, err)
	for _, field := range []string{"path_template", "normalized_query", "account_scope", "user_partition", "session_partition", "persona_profile", "model_version", "schema_version"} {
		require.Contains(t, string(encoded), field)
	}
}

func TestControlPlaneCacheIsolatedByAccountAndUser(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	cache := NewControlPlaneCache(func() time.Time { return now })
	policy := ControlPlanePathPolicy{StaleMode: ControlPlaneStaleSafe}

	accountA := baseControlPlaneCacheInput()
	keyA, err := NewControlPlaneCacheKey(accountA)
	require.NoError(t, err)
	require.NoError(t, cache.Put(keyA, map[string]any{"ok": true}, time.Minute, ControlPlaneStaleSafe))

	accountB := accountA
	accountB.AccountScope = "acctscope-b"
	keyB, err := NewControlPlaneCacheKey(accountB)
	require.NoError(t, err)
	_, hit, _ := cache.Get(keyB, policy)
	require.False(t, hit)

	userB := accountA
	userB.UserPartition = "userpart-b"
	keyUserB, err := NewControlPlaneCacheKey(userB)
	require.NoError(t, err)
	_, hit, _ = cache.Get(keyUserB, policy)
	require.False(t, hit)
}

func TestControlPlaneCacheNoStaleForSensitiveAccountSettings(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	nowRef := now
	cache := NewControlPlaneCache(func() time.Time { return nowRef })
	input := baseControlPlaneCacheInput()
	input.PathTemplate = "/api/oauth/account/settings"
	key, err := NewControlPlaneCacheKey(input)
	require.NoError(t, err)
	require.NoError(t, cache.Put(key, map[string]any{"ok": true}, time.Second, ControlPlaneStaleNoStale))

	nowRef = now.Add(2 * time.Second)
	_, hit, stale := cache.Get(key, ControlPlanePathPolicy{Sensitive: true, StaleMode: ControlPlaneStaleNoStale})
	require.False(t, hit)
	require.False(t, stale)
}

func TestControlPlaneCacheStaleSafeAllowsNonSensitiveStale(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	nowRef := now
	cache := NewControlPlaneCache(func() time.Time { return nowRef })
	key, err := NewControlPlaneCacheKey(baseControlPlaneCacheInput())
	require.NoError(t, err)
	require.NoError(t, cache.Put(key, map[string]any{"ok": true}, time.Second, ControlPlaneStaleSafe))

	nowRef = now.Add(2 * time.Second)
	_, hit, stale := cache.Get(key, ControlPlanePathPolicy{StaleMode: ControlPlaneStaleSafe})
	require.True(t, hit)
	require.True(t, stale)
}

func baseControlPlaneCacheInput() ControlPlaneCacheKeyInput {
	return ControlPlaneCacheKeyInput{
		PathTemplate:     "/api/claude_cli/bootstrap",
		NormalizedQuery:  map[string]string{"entrypoint": "sdk-cli"},
		AccountScope:     "acctscope-a",
		UserPartition:    "userpart-a",
		SessionPartition: "sessionpart-a",
		PersonaProfile:   "claude_code_2_1_150_subscription_1m",
		ModelVersion:     "claude-sonnet-4-6",
		SchemaVersion:    1,
	}
}
