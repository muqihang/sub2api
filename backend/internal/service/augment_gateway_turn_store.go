package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

type AugmentGatewayReasoningTurnKey struct {
	ConversationID  string
	RequestID       string
	AssistantTurnID string
	ToolCallID      string
	ModelID         string
}

type AugmentGatewayReasoningTurn struct {
	Key                     AugmentGatewayReasoningTurnKey
	AssistantContent        string
	ReasoningContent        string
	ReasoningContentPresent bool
	ToolCalls               []AugmentGatewayToolCall
	StreamComplete          bool
	UpstreamRequestID       string
}

type AugmentGatewayReasoningTurnLookup string

const (
	AugmentGatewayReasoningTurnLookupFullKey         AugmentGatewayReasoningTurnLookup = "full_key"
	AugmentGatewayReasoningTurnLookupScopedSecondary AugmentGatewayReasoningTurnLookup = "scoped_secondary"
	AugmentGatewayReasoningTurnLookupMiss            AugmentGatewayReasoningTurnLookup = "miss"
)

type AugmentGatewayReasoningTurnMissReason string

const (
	AugmentGatewayReasoningTurnMissNotFound               AugmentGatewayReasoningTurnMissReason = "not_found"
	AugmentGatewayReasoningTurnMissMissingRequestID       AugmentGatewayReasoningTurnMissReason = "missing_request_id"
	AugmentGatewayReasoningTurnMissMissingAssistantTurnID AugmentGatewayReasoningTurnMissReason = "missing_assistant_turn_id"
	AugmentGatewayReasoningTurnMissMissingToolCallID      AugmentGatewayReasoningTurnMissReason = "missing_tool_call_id"
	AugmentGatewayReasoningTurnMissModelChanged           AugmentGatewayReasoningTurnMissReason = "model_changed"
	AugmentGatewayReasoningTurnMissAmbiguousSecondary     AugmentGatewayReasoningTurnMissReason = "ambiguous_secondary_lookup"
)

type AugmentGatewayReasoningTurnLookupTrace struct {
	Hit      bool
	Degraded bool
	Lookup   AugmentGatewayReasoningTurnLookup
	Reason   AugmentGatewayReasoningTurnMissReason
}

type AugmentGatewayReasoningTurnWriteInput struct {
	ConversationID          string
	RequestID               string
	AssistantTurnID         string
	ModelID                 string
	AssistantContent        string
	ReasoningContent        string
	ReasoningContentPresent bool
	ToolCalls               []AugmentGatewayToolCall
	StreamComplete          bool
	UpstreamRequestID       string
}

type AugmentGatewayReasoningTurnLookupInput struct {
	ConversationID string
	ModelID        string
	ToolCallID     string
	Metadata       map[string]any
	ToolCalls      []AugmentGatewayToolCall
}

type augmentGatewayReasoningTurnSecondaryKey struct {
	ConversationID  string
	AssistantTurnID string
	ToolCallID      string
	ModelID         string
}

// AugmentGatewayReasoningTurnStore is the MVP DeepSeek replay store. It is
// intentionally in-memory/session-scoped for now and stores only assistant turn
// replay summaries: no tokens, cookies, Authorization headers, or full request
// bodies.
type AugmentGatewayReasoningTurnStore struct {
	mu        sync.RWMutex
	turns     map[AugmentGatewayReasoningTurnKey]AugmentGatewayReasoningTurn
	secondary map[augmentGatewayReasoningTurnSecondaryKey]map[string]AugmentGatewayReasoningTurnKey
	latest    map[string]AugmentGatewayReasoningTurnKey
}

func NewAugmentGatewayReasoningTurnStore() *AugmentGatewayReasoningTurnStore {
	return &AugmentGatewayReasoningTurnStore{
		turns:     make(map[AugmentGatewayReasoningTurnKey]AugmentGatewayReasoningTurn),
		secondary: make(map[augmentGatewayReasoningTurnSecondaryKey]map[string]AugmentGatewayReasoningTurnKey),
		latest:    make(map[string]AugmentGatewayReasoningTurnKey),
	}
}

func (s *AugmentGatewayReasoningTurnStore) Store(turn AugmentGatewayReasoningTurn) {
	if s == nil {
		return
	}
	turn = augmentGatewayReasoningTurnClone(turn)
	turn.Key = augmentGatewayReasoningTurnNormalizeKey(turn.Key)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.turns[turn.Key] = turn
	secondaryKey := augmentGatewayReasoningTurnSecondaryKeyFromKey(turn.Key)
	if s.secondary[secondaryKey] == nil {
		s.secondary[secondaryKey] = make(map[string]AugmentGatewayReasoningTurnKey)
	}
	s.secondary[secondaryKey][turn.Key.RequestID] = turn.Key
	s.latest[augmentGatewayReasoningTurnLatestKey(turn.Key.ConversationID, turn.Key.ModelID, turn.Key.ToolCallID)] = turn.Key
}

func (s *AugmentGatewayReasoningTurnStore) Lookup(key AugmentGatewayReasoningTurnKey) (AugmentGatewayReasoningTurn, AugmentGatewayReasoningTurnLookupTrace) {
	if s == nil {
		return AugmentGatewayReasoningTurn{}, augmentGatewayReasoningTurnMissTrace(AugmentGatewayReasoningTurnMissNotFound)
	}
	key = augmentGatewayReasoningTurnNormalizeKey(key)

	s.mu.RLock()
	defer s.mu.RUnlock()

	turn, ok := s.turns[key]
	if !ok {
		return AugmentGatewayReasoningTurn{}, augmentGatewayReasoningTurnMissTrace(AugmentGatewayReasoningTurnMissNotFound)
	}
	return augmentGatewayReasoningTurnClone(turn), AugmentGatewayReasoningTurnLookupTrace{
		Hit:    true,
		Lookup: AugmentGatewayReasoningTurnLookupFullKey,
	}
}

func (s *AugmentGatewayReasoningTurnStore) LookupForNextRequest(key AugmentGatewayReasoningTurnKey) (AugmentGatewayReasoningTurn, AugmentGatewayReasoningTurnLookupTrace) {
	key = augmentGatewayReasoningTurnNormalizeKey(key)
	if key.RequestID != "" {
		return s.Lookup(key)
	}
	if s == nil {
		return AugmentGatewayReasoningTurn{}, augmentGatewayReasoningTurnMissTrace(AugmentGatewayReasoningTurnMissMissingRequestID)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	candidates := s.secondary[augmentGatewayReasoningTurnSecondaryKeyFromKey(key)]
	if len(candidates) == 0 {
		return AugmentGatewayReasoningTurn{}, augmentGatewayReasoningTurnMissTrace(AugmentGatewayReasoningTurnMissMissingRequestID)
	}
	if len(candidates) > 1 {
		return AugmentGatewayReasoningTurn{}, AugmentGatewayReasoningTurnLookupTrace{
			Degraded: true,
			Lookup:   AugmentGatewayReasoningTurnLookupMiss,
			Reason:   AugmentGatewayReasoningTurnMissAmbiguousSecondary,
		}
	}

	for _, fullKey := range candidates {
		turn, ok := s.turns[fullKey]
		if !ok {
			break
		}
		return augmentGatewayReasoningTurnClone(turn), AugmentGatewayReasoningTurnLookupTrace{
			Hit:      true,
			Degraded: true,
			Lookup:   AugmentGatewayReasoningTurnLookupScopedSecondary,
			Reason:   AugmentGatewayReasoningTurnMissMissingRequestID,
		}
	}

	return AugmentGatewayReasoningTurn{}, augmentGatewayReasoningTurnMissTrace(AugmentGatewayReasoningTurnMissNotFound)
}

func (s *AugmentGatewayReasoningTurnStore) LookupLatestForConversationToolCall(conversationID, modelID, toolCallID string) (AugmentGatewayReasoningTurn, bool) {
	if s == nil {
		return AugmentGatewayReasoningTurn{}, false
	}

	key := augmentGatewayReasoningTurnLatestKey(conversationID, modelID, toolCallID)

	s.mu.RLock()
	defer s.mu.RUnlock()

	fullKey, ok := s.latest[key]
	if !ok {
		return AugmentGatewayReasoningTurn{}, false
	}
	turn, ok := s.turns[fullKey]
	if !ok {
		return AugmentGatewayReasoningTurn{}, false
	}
	return augmentGatewayReasoningTurnClone(turn), true
}

func BuildAugmentGatewayReasoningTurnRecords(input AugmentGatewayReasoningTurnWriteInput) ([]AugmentGatewayReasoningTurn, map[string]map[string]any, AugmentGatewayReasoningTurnLookupTrace, error) {
	input.ConversationID = strings.TrimSpace(input.ConversationID)
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.AssistantTurnID = strings.TrimSpace(input.AssistantTurnID)
	input.ModelID = strings.TrimSpace(input.ModelID)

	if input.ConversationID == "" {
		return nil, nil, AugmentGatewayReasoningTurnLookupTrace{}, fmt.Errorf("augment gateway reasoning turn requires conversation_id")
	}
	if input.ModelID == "" {
		return nil, nil, AugmentGatewayReasoningTurnLookupTrace{}, fmt.Errorf("augment gateway reasoning turn requires model_id")
	}
	if len(input.ToolCalls) == 0 {
		return nil, nil, AugmentGatewayReasoningTurnLookupTrace{}, fmt.Errorf("augment gateway reasoning turn requires at least one tool call")
	}

	toolCalls := augmentGatewayReasoningTurnCloneToolCalls(input.ToolCalls)
	trace := AugmentGatewayReasoningTurnLookupTrace{}
	if input.AssistantTurnID == "" {
		input.AssistantTurnID = augmentGatewayReasoningTurnDeterministicID("augturn", input.ConversationID, input.ModelID, "", toolCalls)
		trace = augmentGatewayReasoningTurnMarkDegraded(trace, AugmentGatewayReasoningTurnMissMissingAssistantTurnID)
	}
	if input.RequestID == "" {
		input.RequestID = augmentGatewayReasoningTurnDeterministicID("augreq", input.ConversationID, input.ModelID, input.AssistantTurnID, toolCalls)
		trace = augmentGatewayReasoningTurnMarkDegraded(trace, AugmentGatewayReasoningTurnMissMissingRequestID)
	}

	records := make([]AugmentGatewayReasoningTurn, 0, len(toolCalls))
	metadataByToolCall := make(map[string]map[string]any, len(toolCalls))
	for _, toolCall := range toolCalls {
		toolCallID := strings.TrimSpace(toolCall.ID)
		if toolCallID == "" {
			return nil, nil, trace, fmt.Errorf("augment gateway reasoning turn requires tool_call_id")
		}
		key := AugmentGatewayReasoningTurnKey{
			ConversationID:  input.ConversationID,
			RequestID:       input.RequestID,
			AssistantTurnID: input.AssistantTurnID,
			ToolCallID:      toolCallID,
			ModelID:         input.ModelID,
		}
		records = append(records, AugmentGatewayReasoningTurn{
			Key:                     key,
			AssistantContent:        input.AssistantContent,
			ReasoningContent:        input.ReasoningContent,
			ReasoningContentPresent: input.ReasoningContentPresent,
			ToolCalls:               toolCalls,
			StreamComplete:          input.StreamComplete,
			UpstreamRequestID:       strings.TrimSpace(input.UpstreamRequestID),
		})
		metadataByToolCall[toolCallID] = BuildAugmentGatewayReasoningTurnMetadata(key)
	}

	return records, metadataByToolCall, trace, nil
}

func BuildAugmentGatewayReasoningTurnMetadata(key AugmentGatewayReasoningTurnKey) map[string]any {
	key = augmentGatewayReasoningTurnNormalizeKey(key)
	return map[string]any{
		"request_id":        key.RequestID,
		"assistant_turn_id": key.AssistantTurnID,
		"model_id":          key.ModelID,
		"tool_call_id":      key.ToolCallID,
	}
}

func BuildAugmentGatewayReasoningTurnLookupKey(input AugmentGatewayReasoningTurnLookupInput) (AugmentGatewayReasoningTurnKey, AugmentGatewayReasoningTurnLookupTrace, error) {
	conversationID := strings.TrimSpace(input.ConversationID)
	if conversationID == "" {
		return AugmentGatewayReasoningTurnKey{}, AugmentGatewayReasoningTurnLookupTrace{}, fmt.Errorf("augment gateway reasoning lookup requires conversation_id")
	}

	metadata := input.Metadata
	requestID := augmentGatewayReasoningTurnMetadataString(metadata, "request_id")
	assistantTurnID := augmentGatewayReasoningTurnMetadataString(metadata, "assistant_turn_id")
	metadataModelID := augmentGatewayReasoningTurnMetadataString(metadata, "model_id")
	metadataToolCallID := augmentGatewayReasoningTurnMetadataString(metadata, "tool_call_id")
	modelID := strings.TrimSpace(input.ModelID)
	toolCallID := strings.TrimSpace(input.ToolCallID)
	if toolCallID == "" {
		toolCallID = metadataToolCallID
	}

	trace := AugmentGatewayReasoningTurnLookupTrace{}
	if modelID == "" {
		modelID = metadataModelID
	}
	if metadataModelID != "" && modelID != "" && metadataModelID != modelID {
		trace = augmentGatewayReasoningTurnMarkDegraded(trace, AugmentGatewayReasoningTurnMissModelChanged)
	}
	if toolCallID == "" {
		trace = augmentGatewayReasoningTurnMarkDegraded(trace, AugmentGatewayReasoningTurnMissMissingToolCallID)
		return AugmentGatewayReasoningTurnKey{}, trace, fmt.Errorf("augment gateway reasoning lookup requires tool_call_id")
	}
	if assistantTurnID == "" {
		assistantTurnID = augmentGatewayReasoningTurnDeterministicID("augturn", conversationID, modelID, toolCallID, input.ToolCalls)
		trace = augmentGatewayReasoningTurnMarkDegraded(trace, AugmentGatewayReasoningTurnMissMissingAssistantTurnID)
	}
	if requestID == "" {
		trace = augmentGatewayReasoningTurnMarkDegraded(trace, AugmentGatewayReasoningTurnMissMissingRequestID)
	}

	return augmentGatewayReasoningTurnNormalizeKey(AugmentGatewayReasoningTurnKey{
		ConversationID:  conversationID,
		RequestID:       requestID,
		AssistantTurnID: assistantTurnID,
		ToolCallID:      toolCallID,
		ModelID:         modelID,
	}), trace, nil
}

func augmentGatewayReasoningTurnNormalizeKey(key AugmentGatewayReasoningTurnKey) AugmentGatewayReasoningTurnKey {
	key.ConversationID = strings.TrimSpace(key.ConversationID)
	key.RequestID = strings.TrimSpace(key.RequestID)
	key.AssistantTurnID = strings.TrimSpace(key.AssistantTurnID)
	key.ToolCallID = strings.TrimSpace(key.ToolCallID)
	key.ModelID = strings.TrimSpace(key.ModelID)
	return key
}

func augmentGatewayReasoningTurnSecondaryKeyFromKey(key AugmentGatewayReasoningTurnKey) augmentGatewayReasoningTurnSecondaryKey {
	key = augmentGatewayReasoningTurnNormalizeKey(key)
	return augmentGatewayReasoningTurnSecondaryKey{
		ConversationID:  key.ConversationID,
		AssistantTurnID: key.AssistantTurnID,
		ToolCallID:      key.ToolCallID,
		ModelID:         key.ModelID,
	}
}

func augmentGatewayReasoningTurnLatestKey(conversationID, modelID, toolCallID string) string {
	return strings.TrimSpace(conversationID) + "\x00" + strings.TrimSpace(modelID) + "\x00" + strings.TrimSpace(toolCallID)
}

func augmentGatewayReasoningTurnClone(turn AugmentGatewayReasoningTurn) AugmentGatewayReasoningTurn {
	turn.ToolCalls = augmentGatewayReasoningTurnCloneToolCalls(turn.ToolCalls)
	return turn
}

func augmentGatewayReasoningTurnCloneToolCalls(toolCalls []AugmentGatewayToolCall) []AugmentGatewayToolCall {
	if len(toolCalls) == 0 {
		return nil
	}
	out := make([]AugmentGatewayToolCall, len(toolCalls))
	copy(out, toolCalls)
	return out
}

func augmentGatewayReasoningTurnMissTrace(reason AugmentGatewayReasoningTurnMissReason) AugmentGatewayReasoningTurnLookupTrace {
	return AugmentGatewayReasoningTurnLookupTrace{
		Degraded: true,
		Lookup:   AugmentGatewayReasoningTurnLookupMiss,
		Reason:   reason,
	}
}

func augmentGatewayReasoningTurnMarkDegraded(trace AugmentGatewayReasoningTurnLookupTrace, reason AugmentGatewayReasoningTurnMissReason) AugmentGatewayReasoningTurnLookupTrace {
	trace.Degraded = true
	if trace.Reason == "" {
		trace.Reason = reason
	}
	return trace
}

func augmentGatewayReasoningTurnMetadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func augmentGatewayReasoningTurnDeterministicID(prefix, conversationID, modelID, toolCallID string, toolCalls []AugmentGatewayToolCall) string {
	normalizedToolCalls, _ := json.Marshal(augmentGatewayReasoningTurnCloneToolCalls(toolCalls))
	sum := sha256.Sum256([]byte(strings.Join([]string{
		prefix,
		strings.TrimSpace(conversationID),
		strings.TrimSpace(modelID),
		strings.TrimSpace(toolCallID),
		string(normalizedToolCalls),
	}, "\x00")))
	return prefix + "_" + hex.EncodeToString(sum[:])[:24]
}
