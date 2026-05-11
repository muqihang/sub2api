package service

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCodexGatewayStateStore_PutGetConflictAndMissing(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{
		TTL:      time.Minute,
		MaxItems: 8,
		Now: func() time.Time {
			return now
		},
	})

	state := CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_1",
			SessionKey:    "session_1",
			IsolationKey:  "user_1",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		},
		AssistantContent:          "",
		AssistantContentPresent:   true,
		ReasoningContent:          "need tool context",
		ReasoningContentPresent:   true,
		ToolCalls:                 []CodexGatewayStoredToolCall{{ID: "call_1", Name: "shell", Arguments: `{}`}},
		ToolNameMap:               map[string]CodexGatewayToolNameMapEntry{"shell": {Alias: "shell", Kind: "function", Name: "shell"}},
		ReasoningContentSynthesized: false,
	}

	require.NoError(t, store.Put(state))
	otherSessionState := state
	otherSessionState.Key.SessionKey = "session_2"
	otherSessionState.ReasoningContent = "other session reasoning"
	require.NoError(t, store.Put(otherSessionState))

	got, err := store.Get(state.Key)
	require.NoError(t, err)
	require.Equal(t, state.Key, got.Key)
	require.Equal(t, "need tool context", got.ReasoningContent)
	require.Equal(t, state.ToolCalls, got.ToolCalls)
	require.Equal(t, state.ToolNameMap, got.ToolNameMap)

	got, err = store.Get(otherSessionState.Key)
	require.NoError(t, err)
	require.Equal(t, "other session reasoning", got.ReasoningContent)

	_, err = store.Get(CodexGatewayStateLookupKey{
		ResponseID:    "resp_1",
		SessionKey:    "session_3",
		IsolationKey:  "user_1",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCodexGatewayStateConflict))

	_, err = store.Get(CodexGatewayStateLookupKey{
		ResponseID:    "resp_missing",
		SessionKey:    "session_1",
		IsolationKey:  "user_1",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCodexGatewayStateNotFound))
}

func TestCodexGatewayStateStore_ExpiresAndBoundsEntries(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{
		TTL:      2 * time.Second,
		MaxItems: 2,
		Now: func() time.Time {
			return now
		},
	})

	require.NoError(t, store.Put(codexGatewayTestState("resp_1", "call_1")))
	now = now.Add(time.Second)
	require.NoError(t, store.Put(codexGatewayTestState("resp_2", "call_2")))
	now = now.Add(time.Second)
	require.NoError(t, store.Put(codexGatewayTestState("resp_3", "call_3")))

	_, err := store.Get(codexGatewayTestState("resp_1", "call_1").Key)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCodexGatewayStateNotFound))

	now = now.Add(2 * time.Second)
	_, err = store.Get(codexGatewayTestState("resp_2", "call_2").Key)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCodexGatewayStateNotFound))
}

func TestCodexGatewayStateStore_RejectsInvalidDeepSeekToolLoopState(t *testing.T) {
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{
		TTL:      time.Minute,
		MaxItems: 4,
		Now:      time.Now,
	})

	err := store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_invalid",
			SessionKey:    "session_1",
			IsolationKey:  "user_1",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		},
		ToolCalls: []CodexGatewayStoredToolCall{{ID: "call_1", Name: "shell", Arguments: `{}`}},
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCodexGatewayStateInvalid))

	err = store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_synth",
			SessionKey:    "session_1",
			IsolationKey:  "user_1",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		},
		AssistantContentPresent:      true,
		ReasoningContentSynthesized:  true,
		ToolCalls:                    []CodexGatewayStoredToolCall{{ID: "call_1", Name: "shell", Arguments: `{}`}},
		ToolNameMap:                  map[string]CodexGatewayToolNameMapEntry{"shell": {Alias: "shell", Kind: "function", Name: "shell"}},
	})
	require.NoError(t, err)

	err = store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_dup",
			SessionKey:    "session_1",
			IsolationKey:  "user_1",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		},
		AssistantContentPresent: true,
		ReasoningContent:        "reasoning",
		ReasoningContentPresent: true,
		ToolCalls: []CodexGatewayStoredToolCall{
			{ID: "call_dup", Name: "shell", Arguments: `{}`},
			{ID: "call_dup", Name: "shell", Arguments: `{}`},
		},
		ToolNameMap: map[string]CodexGatewayToolNameMapEntry{
			"shell": {Alias: "shell", Kind: "function", Name: "shell"},
		},
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCodexGatewayStateInvalid))
}

func codexGatewayTestState(responseID, callID string) CodexGatewayResponseState {
	return CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    responseID,
			SessionKey:    "session_1",
			IsolationKey:  "user_1",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		},
		AssistantContent:        "",
		AssistantContentPresent: true,
		ReasoningContent:        "reasoning",
		ReasoningContentPresent: true,
		ToolCalls:               []CodexGatewayStoredToolCall{{ID: callID, Name: "shell", Arguments: `{}`}},
		ToolNameMap:             map[string]CodexGatewayToolNameMapEntry{"shell": {Alias: "shell", Kind: "function", Name: "shell"}},
	}
}
