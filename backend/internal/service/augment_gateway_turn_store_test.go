package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAugmentGatewayReasoningTurnStoreStoresAndLooksUpCompoundKey(t *testing.T) {
	store := NewAugmentGatewayReasoningTurnStore()
	key := AugmentGatewayReasoningTurnKey{
		ConversationID:  "conv-1",
		RequestID:       "provider-req-1",
		AssistantTurnID: "assistant-turn-1",
		ToolCallID:      "call-search",
		ModelID:         "deepseek-v4-pro",
	}
	toolCalls := []AugmentGatewayToolCall{
		{
			ID:   "call-search",
			Type: "function",
			Function: AugmentGatewayToolCallFunction{
				Name:      "codebase-retrieval",
				Arguments: `{"query":"turn store"}`,
			},
		},
	}

	store.Store(AugmentGatewayReasoningTurn{
		Key:                     key,
		AssistantContent:        "I will inspect the workspace.",
		ReasoningContent:        "Need repository context before answering.",
		ReasoningContentPresent: true,
		ToolCalls:               toolCalls,
		StreamComplete:          true,
		UpstreamRequestID:       "upstream-chatcmpl-1",
	})

	got, trace := store.Lookup(key)
	require.True(t, trace.Hit)
	require.False(t, trace.Degraded)
	require.Equal(t, AugmentGatewayReasoningTurnLookupFullKey, trace.Lookup)
	require.Empty(t, trace.Reason)
	require.Equal(t, key, got.Key)
	require.Equal(t, "I will inspect the workspace.", got.AssistantContent)
	require.Equal(t, "Need repository context before answering.", got.ReasoningContent)
	require.True(t, got.ReasoningContentPresent)
	require.Equal(t, toolCalls, got.ToolCalls)
	require.True(t, got.StreamComplete)
	require.Equal(t, "upstream-chatcmpl-1", got.UpstreamRequestID)
}

func TestAugmentGatewayReasoningTurnStorePreservesEmptyReasoningPresence(t *testing.T) {
	store := NewAugmentGatewayReasoningTurnStore()
	key := augmentGatewayReasoningTurnTestKey("conv-empty", "provider-req-empty", "turn-empty", "call-empty", "deepseek-v4-flash")

	store.Store(AugmentGatewayReasoningTurn{
		Key:                     key,
		AssistantContent:        "",
		ReasoningContent:        "",
		ReasoningContentPresent: true,
		ToolCalls: []AugmentGatewayToolCall{{
			ID:       "call-empty",
			Type:     "function",
			Function: AugmentGatewayToolCallFunction{Name: "read-file", Arguments: "{}"},
		}},
		StreamComplete:    true,
		UpstreamRequestID: "upstream-empty",
	})

	got, trace := store.Lookup(key)
	require.True(t, trace.Hit)
	require.Equal(t, "", got.ReasoningContent)
	require.True(t, got.ReasoningContentPresent)
}

func TestAugmentGatewayReasoningTurnStoreFullCompoundKeyPreventsCollisions(t *testing.T) {
	store := NewAugmentGatewayReasoningTurnStore()
	base := augmentGatewayReasoningTurnTestKey("conv-1", "provider-req-1", "assistant-turn-1", "call-shared", "deepseek-v4-pro")
	store.Store(augmentGatewayReasoningTurnTestRecord(base, "base reasoning"))

	tests := []struct {
		name string
		key  AugmentGatewayReasoningTurnKey
	}{
		{
			name: "different conversation",
			key:  augmentGatewayReasoningTurnTestKey("conv-2", "provider-req-1", "assistant-turn-1", "call-shared", "deepseek-v4-pro"),
		},
		{
			name: "different request retry",
			key:  augmentGatewayReasoningTurnTestKey("conv-1", "provider-req-2", "assistant-turn-1", "call-shared", "deepseek-v4-pro"),
		},
		{
			name: "different assistant turn",
			key:  augmentGatewayReasoningTurnTestKey("conv-1", "provider-req-1", "assistant-turn-2", "call-shared", "deepseek-v4-pro"),
		},
		{
			name: "different tool call",
			key:  augmentGatewayReasoningTurnTestKey("conv-1", "provider-req-1", "assistant-turn-1", "call-other", "deepseek-v4-pro"),
		},
		{
			name: "different model",
			key:  augmentGatewayReasoningTurnTestKey("conv-1", "provider-req-1", "assistant-turn-1", "call-shared", "gpt-5.4"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, trace := store.Lookup(tt.key)
			require.False(t, trace.Hit)
			require.True(t, trace.Degraded)
			require.Equal(t, AugmentGatewayReasoningTurnLookupMiss, trace.Lookup)
			require.Equal(t, AugmentGatewayReasoningTurnMissNotFound, trace.Reason)
		})
	}
}

func TestAugmentGatewayReasoningTurnStoreMultipleToolCallsDoNotOverwriteEachOther(t *testing.T) {
	store := NewAugmentGatewayReasoningTurnStore()
	first := augmentGatewayReasoningTurnTestKey("conv-1", "provider-req-1", "assistant-turn-1", "call-first", "deepseek-v4-pro")
	second := augmentGatewayReasoningTurnTestKey("conv-1", "provider-req-1", "assistant-turn-1", "call-second", "deepseek-v4-pro")
	toolCalls := []AugmentGatewayToolCall{
		{
			ID:       first.ToolCallID,
			Type:     "function",
			Function: AugmentGatewayToolCallFunction{Name: "read-file", Arguments: `{"path":"README.md"}`},
		},
		{
			ID:       second.ToolCallID,
			Type:     "function",
			Function: AugmentGatewayToolCallFunction{Name: "search", Arguments: `{"query":"reasoning"}`},
		},
	}
	records, _, _, err := BuildAugmentGatewayReasoningTurnRecords(AugmentGatewayReasoningTurnWriteInput{
		ConversationID:          first.ConversationID,
		RequestID:               first.RequestID,
		AssistantTurnID:         first.AssistantTurnID,
		ModelID:                 first.ModelID,
		AssistantContent:        "assistant content",
		ReasoningContent:        "shared assistant turn reasoning",
		ReasoningContentPresent: true,
		ToolCalls:               toolCalls,
		StreamComplete:          true,
		UpstreamRequestID:       "upstream-provider-req-1",
	})
	require.NoError(t, err)
	require.Len(t, records, 2)
	for _, record := range records {
		store.Store(record)
	}

	gotFirst, firstTrace := store.Lookup(first)
	gotSecond, secondTrace := store.Lookup(second)
	require.True(t, firstTrace.Hit)
	require.True(t, secondTrace.Hit)
	require.Equal(t, first, gotFirst.Key)
	require.Equal(t, second, gotSecond.Key)
	require.Equal(t, "shared assistant turn reasoning", gotFirst.ReasoningContent)
	require.Equal(t, "shared assistant turn reasoning", gotSecond.ReasoningContent)
	require.Equal(t, toolCalls, gotFirst.ToolCalls)
	require.Equal(t, toolCalls, gotSecond.ToolCalls)
}

func TestAugmentGatewayReasoningTurnStoreSameToolCallIDAcrossRetriesAndModelsDoNotCollide(t *testing.T) {
	store := NewAugmentGatewayReasoningTurnStore()
	firstRetry := augmentGatewayReasoningTurnTestKey("conv-1", "provider-req-1", "assistant-turn-1", "call-retry", "deepseek-v4-pro")
	secondRetry := augmentGatewayReasoningTurnTestKey("conv-1", "provider-req-2", "assistant-turn-1", "call-retry", "deepseek-v4-pro")
	otherModel := augmentGatewayReasoningTurnTestKey("conv-1", "provider-req-1", "assistant-turn-1", "call-retry", "deepseek-v4-flash")
	otherConversation := augmentGatewayReasoningTurnTestKey("conv-2", "provider-req-1", "assistant-turn-1", "call-retry", "deepseek-v4-pro")

	store.Store(augmentGatewayReasoningTurnTestRecord(firstRetry, "first retry reasoning"))
	store.Store(augmentGatewayReasoningTurnTestRecord(secondRetry, "second retry reasoning"))
	store.Store(augmentGatewayReasoningTurnTestRecord(otherModel, "other model reasoning"))
	store.Store(augmentGatewayReasoningTurnTestRecord(otherConversation, "other conversation reasoning"))

	for _, tt := range []struct {
		key       AugmentGatewayReasoningTurnKey
		reasoning string
	}{
		{key: firstRetry, reasoning: "first retry reasoning"},
		{key: secondRetry, reasoning: "second retry reasoning"},
		{key: otherModel, reasoning: "other model reasoning"},
		{key: otherConversation, reasoning: "other conversation reasoning"},
	} {
		got, trace := store.Lookup(tt.key)
		require.True(t, trace.Hit)
		require.Equal(t, tt.reasoning, got.ReasoningContent)
	}
}

func TestAugmentGatewayReasoningTurnStoreFirstTurnMetadataAndSecondRequestKeyMapping(t *testing.T) {
	toolCalls := []AugmentGatewayToolCall{
		{
			ID:   "call-a",
			Type: "function",
			Function: AugmentGatewayToolCallFunction{
				Name:      "codebase-retrieval",
				Arguments: `{"query":"augment gateway"}`,
			},
		},
	}
	records, metadataByToolCall, trace, err := BuildAugmentGatewayReasoningTurnRecords(AugmentGatewayReasoningTurnWriteInput{
		ConversationID:          "conv-map-1",
		RequestID:               "provider-req-map-1",
		AssistantTurnID:         "assistant-turn-map-1",
		ModelID:                 "deepseek-v4-pro",
		AssistantContent:        "",
		ReasoningContent:        "Use retrieval before answering.",
		ReasoningContentPresent: true,
		ToolCalls:               toolCalls,
		StreamComplete:          true,
		UpstreamRequestID:       "upstream-map-1",
	})
	require.NoError(t, err)
	require.False(t, trace.Degraded)
	require.Len(t, records, 1)
	require.Len(t, metadataByToolCall, 1)

	wantKey := AugmentGatewayReasoningTurnKey{
		ConversationID:  "conv-map-1",
		RequestID:       "provider-req-map-1",
		AssistantTurnID: "assistant-turn-map-1",
		ToolCallID:      "call-a",
		ModelID:         "deepseek-v4-pro",
	}
	require.Equal(t, wantKey, records[0].Key)

	metadata := metadataByToolCall["call-a"]
	require.Equal(t, map[string]any{
		"request_id":        "provider-req-map-1",
		"assistant_turn_id": "assistant-turn-map-1",
		"model_id":          "deepseek-v4-pro",
		"tool_call_id":      "call-a",
	}, metadata)
	requireAugmentGatewayReasoningMetadataIsNonSensitive(t, metadata)

	lookupKey, lookupTrace, err := BuildAugmentGatewayReasoningTurnLookupKey(AugmentGatewayReasoningTurnLookupInput{
		ConversationID: "conv-map-1",
		ModelID:        "deepseek-v4-pro",
		ToolCallID:     "call-a",
		Metadata:       metadata,
		ToolCalls:      toolCalls,
	})
	require.NoError(t, err)
	require.False(t, lookupTrace.Degraded)
	require.Equal(t, wantKey, lookupKey)
}

func TestAugmentGatewayReasoningTurnStoreMissingRequestIDUsesScopedSecondaryLookupWithDegradedTrace(t *testing.T) {
	store := NewAugmentGatewayReasoningTurnStore()
	key := augmentGatewayReasoningTurnTestKey("conv-secondary", "provider-req-secondary", "assistant-turn-secondary", "call-secondary", "deepseek-v4-pro")
	store.Store(augmentGatewayReasoningTurnTestRecord(key, "secondary reasoning"))

	metadata := map[string]any{
		"assistant_turn_id": "assistant-turn-secondary",
		"model_id":          "deepseek-v4-pro",
		"tool_call_id":      "call-secondary",
	}
	lookupKey, mappingTrace, err := BuildAugmentGatewayReasoningTurnLookupKey(AugmentGatewayReasoningTurnLookupInput{
		ConversationID: "conv-secondary",
		ModelID:        "deepseek-v4-pro",
		ToolCallID:     "call-secondary",
		Metadata:       metadata,
	})
	require.NoError(t, err)
	require.True(t, mappingTrace.Degraded)
	require.Equal(t, AugmentGatewayReasoningTurnMissMissingRequestID, mappingTrace.Reason)
	require.Empty(t, lookupKey.RequestID)

	got, lookupTrace := store.LookupForNextRequest(lookupKey)
	require.True(t, lookupTrace.Hit)
	require.True(t, lookupTrace.Degraded)
	require.Equal(t, AugmentGatewayReasoningTurnLookupScopedSecondary, lookupTrace.Lookup)
	require.Equal(t, AugmentGatewayReasoningTurnMissMissingRequestID, lookupTrace.Reason)
	require.Equal(t, "secondary reasoning", got.ReasoningContent)
}

func TestAugmentGatewayReasoningTurnStoreDifferentModelIDDoesNotReplayReasoning(t *testing.T) {
	store := NewAugmentGatewayReasoningTurnStore()
	key := augmentGatewayReasoningTurnTestKey("conv-model", "provider-req-model", "assistant-turn-model", "call-model", "deepseek-v4-pro")
	store.Store(augmentGatewayReasoningTurnTestRecord(key, "deepseek reasoning"))

	metadata := map[string]any{
		"request_id":        "provider-req-model",
		"assistant_turn_id": "assistant-turn-model",
		"model_id":          "deepseek-v4-pro",
		"tool_call_id":      "call-model",
	}
	lookupKey, mappingTrace, err := BuildAugmentGatewayReasoningTurnLookupKey(AugmentGatewayReasoningTurnLookupInput{
		ConversationID: "conv-model",
		ModelID:        "gpt-5.4",
		ToolCallID:     "call-model",
		Metadata:       metadata,
	})
	require.NoError(t, err)
	require.True(t, mappingTrace.Degraded)
	require.Equal(t, AugmentGatewayReasoningTurnMissModelChanged, mappingTrace.Reason)

	_, lookupTrace := store.LookupForNextRequest(lookupKey)
	require.False(t, lookupTrace.Hit)
	require.True(t, lookupTrace.Degraded)
	require.Equal(t, AugmentGatewayReasoningTurnLookupMiss, lookupTrace.Lookup)
}

func augmentGatewayReasoningTurnTestKey(conversationID, requestID, assistantTurnID, toolCallID, modelID string) AugmentGatewayReasoningTurnKey {
	return AugmentGatewayReasoningTurnKey{
		ConversationID:  conversationID,
		RequestID:       requestID,
		AssistantTurnID: assistantTurnID,
		ToolCallID:      toolCallID,
		ModelID:         modelID,
	}
}

func augmentGatewayReasoningTurnTestRecord(key AugmentGatewayReasoningTurnKey, reasoning string) AugmentGatewayReasoningTurn {
	return AugmentGatewayReasoningTurn{
		Key:                     key,
		AssistantContent:        "assistant content",
		ReasoningContent:        reasoning,
		ReasoningContentPresent: true,
		ToolCalls: []AugmentGatewayToolCall{{
			ID:       key.ToolCallID,
			Type:     "function",
			Function: AugmentGatewayToolCallFunction{Name: "tool-" + key.ToolCallID, Arguments: "{}"},
		}},
		StreamComplete:    true,
		UpstreamRequestID: "upstream-" + key.RequestID,
	}
}

func requireAugmentGatewayReasoningMetadataIsNonSensitive(t *testing.T, metadata map[string]any) {
	t.Helper()

	raw, err := json.Marshal(metadata)
	require.NoError(t, err)
	lower := strings.ToLower(string(raw))
	for _, forbidden := range []string{
		"authorization",
		"cookie",
		"access_token",
		"refresh_token",
		"bearer",
		"session",
		"request_body",
		"response_body",
	} {
		require.NotContains(t, lower, forbidden)
	}
}
