package service

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

var claudeCodeSessionMapperUUIDLikeRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func TestClaudeCodeSessionMapperReturnsUUIDLikeOpaqueSessionAndSafeRef(t *testing.T) {
	t.Setenv("SUB2API_SESSION_BUDGET_HMAC_KEY", "sub2api-session-budget-test-key")

	mapper := NewClaudeCodeSessionMapperFromEnv()
	mapping, err := mapper.Map(ClaudeCodeSessionMapInput{
		UserScope:    "user:42",
		AccountRef:   "account-42",
		DeviceID:     "device-a",
		AccountUUID:  "acct-uuid-a",
		RawSessionID: "11111111-2222-4333-8444-555555555555",
	})
	require.NoError(t, err)
	require.NotNil(t, mapping)
	require.Regexp(t, claudeCodeSessionMapperUUIDLikeRe, mapping.SessionID)
	require.NotEqual(t, "11111111-2222-4333-8444-555555555555", mapping.SessionID)
	require.NotNil(t, mapping.SessionRef)
	require.Equal(t, "session_budget_session", mapping.SessionRef.Scope)
	require.True(t, regexp.MustCompile(`^hmac-sha256:[0-9a-f]{64}$`).MatchString(mapping.SessionRef.Value))

	dumped, err := json.Marshal(mapping)
	require.NoError(t, err)
	require.NotContains(t, string(dumped), "11111111-2222-4333-8444-555555555555")
}

func TestClaudeCodeSessionMapperScopesSessionsByUserAndRawSession(t *testing.T) {
	t.Setenv("SUB2API_SESSION_BUDGET_HMAC_KEY", "sub2api-session-budget-test-key")

	mapper := NewClaudeCodeSessionMapperFromEnv()
	base := ClaudeCodeSessionMapInput{
		UserScope:    "user:alpha",
		AccountRef:   "account-42",
		DeviceID:     "device-a",
		AccountUUID:  "acct-uuid-a",
		RawSessionID: "11111111-2222-4333-8444-555555555555",
	}

	first, err := mapper.Map(base)
	require.NoError(t, err)

	second, err := mapper.Map(base)
	require.NoError(t, err)
	require.Equal(t, first.SessionID, second.SessionID)
	require.Equal(t, first.SessionRef.Value, second.SessionRef.Value)

	otherUser, err := mapper.Map(ClaudeCodeSessionMapInput{
		UserScope:    "user:beta",
		AccountRef:   base.AccountRef,
		DeviceID:     base.DeviceID,
		AccountUUID:  base.AccountUUID,
		RawSessionID: base.RawSessionID,
	})
	require.NoError(t, err)
	require.NotEqual(t, first.SessionID, otherUser.SessionID)

	otherSession, err := mapper.Map(ClaudeCodeSessionMapInput{
		UserScope:    base.UserScope,
		AccountRef:   base.AccountRef,
		DeviceID:     base.DeviceID,
		AccountUUID:  base.AccountUUID,
		RawSessionID: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee",
	})
	require.NoError(t, err)
	require.NotEqual(t, first.SessionID, otherSession.SessionID)
}

func TestClaudeCodeSessionUserScopeRoundTrip(t *testing.T) {
	ctx := WithClaudeCodeSessionUserScope(context.Background(), "user:99")

	scope, ok := ClaudeCodeSessionUserScopeFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "user:99", scope)
}
