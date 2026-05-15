package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrCodexGatewayStateNotFound = errors.New("codex gateway state not found")
	ErrCodexGatewayStateConflict = errors.New("codex gateway state conflict")
	ErrCodexGatewayStateInvalid  = errors.New("codex gateway state invalid")
)

type codexGatewayStateEntry struct {
	state     CodexGatewayResponseState
	createdAt time.Time
	expiresAt time.Time
}

type CodexGatewayStateStore struct {
	mu      sync.Mutex
	entries map[string]codexGatewayStateEntry
	ttl     time.Duration
	max     int
	now     func() time.Time
}

func NewCodexGatewayStateStore(cfg CodexGatewayStateStoreConfig) *CodexGatewayStateStore {
	if cfg.TTL <= 0 {
		cfg.TTL = 24 * time.Hour
	}
	if cfg.MaxItems <= 0 {
		cfg.MaxItems = 200
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &CodexGatewayStateStore{
		entries: make(map[string]codexGatewayStateEntry),
		ttl:     cfg.TTL,
		max:     cfg.MaxItems,
		now:     now,
	}
}

func (s *CodexGatewayStateStore) Put(state CodexGatewayResponseState) error {
	if s == nil {
		return ErrCodexGatewayStateInvalid
	}
	state.Key = normalizeCodexGatewayStateLookupKey(state.Key)
	if err := validateCodexGatewayResponseState(state); err != nil {
		return err
	}

	now := s.nowTime()
	entry := codexGatewayStateEntry{
		state:     cloneCodexGatewayResponseState(state),
		createdAt: now,
		expiresAt: now.Add(s.ttl),
	}
	storageKey := codexGatewayStateStorageKey(state.Key)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	s.entries[storageKey] = entry
	s.trimLocked(now)
	return nil
}

func (s *CodexGatewayStateStore) Get(key CodexGatewayStateLookupKey) (CodexGatewayResponseState, error) {
	if s == nil {
		return CodexGatewayResponseState{}, ErrCodexGatewayStateNotFound
	}
	key = normalizeCodexGatewayStateLookupKey(key)
	if strings.TrimSpace(key.ResponseID) == "" {
		return CodexGatewayResponseState{}, ErrCodexGatewayStateNotFound
	}

	now := s.nowTime()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)

	storageKey := codexGatewayStateStorageKey(key)
	if entry, ok := s.entries[storageKey]; ok {
		if now.After(entry.expiresAt) {
			delete(s.entries, storageKey)
			return CodexGatewayResponseState{}, ErrCodexGatewayStateNotFound
		}
		if err := validateCodexGatewayResponseState(entry.state); err != nil {
			return CodexGatewayResponseState{}, err
		}
		return cloneCodexGatewayResponseState(entry.state), nil
	}

	for existingKey, entry := range s.entries {
		if now.After(entry.expiresAt) {
			delete(s.entries, existingKey)
			continue
		}
		if strings.TrimSpace(entry.state.Key.ResponseID) == key.ResponseID {
			return CodexGatewayResponseState{}, ErrCodexGatewayStateConflict
		}
	}
	return CodexGatewayResponseState{}, ErrCodexGatewayStateNotFound
}

func (s *CodexGatewayStateStore) nowTime() time.Time {
	if s == nil || s.now == nil {
		return time.Now()
	}
	return s.now().UTC()
}

func (s *CodexGatewayStateStore) pruneLocked(now time.Time) {
	if s == nil {
		return
	}
	for key, entry := range s.entries {
		if now.After(entry.expiresAt) {
			delete(s.entries, key)
		}
	}
	s.trimLocked(now)
}

func (s *CodexGatewayStateStore) trimLocked(now time.Time) {
	if s == nil || len(s.entries) <= s.max {
		return
	}
	for len(s.entries) > s.max {
		oldestKey := ""
		var oldest time.Time
		for key, entry := range s.entries {
			if oldestKey == "" || entry.createdAt.Before(oldest) {
				oldestKey = key
				oldest = entry.createdAt
			}
		}
		if oldestKey == "" {
			return
		}
		delete(s.entries, oldestKey)
	}
}

func normalizeCodexGatewayStateLookupKey(key CodexGatewayStateLookupKey) CodexGatewayStateLookupKey {
	key.ResponseID = strings.TrimSpace(key.ResponseID)
	key.SessionKey = strings.TrimSpace(key.SessionKey)
	key.IsolationKey = strings.TrimSpace(key.IsolationKey)
	key.Provider = strings.TrimSpace(key.Provider)
	key.UpstreamModel = strings.TrimSpace(key.UpstreamModel)
	return key
}

func codexGatewayStateKeysMatch(stored, lookup CodexGatewayStateLookupKey) bool {
	return stored.ResponseID == lookup.ResponseID &&
		stored.SessionKey == lookup.SessionKey &&
		stored.IsolationKey == lookup.IsolationKey &&
		stored.Provider == lookup.Provider &&
		stored.UpstreamModel == lookup.UpstreamModel
}

func codexGatewayStateStorageKey(key CodexGatewayStateLookupKey) string {
	key = normalizeCodexGatewayStateLookupKey(key)
	return strings.Join([]string{
		key.ResponseID,
		key.SessionKey,
		key.IsolationKey,
		key.Provider,
		key.UpstreamModel,
	}, "|")
}

func validateCodexGatewayResponseState(state CodexGatewayResponseState) error {
	if strings.TrimSpace(state.Key.ResponseID) == "" {
		return fmt.Errorf("%w: response_id is required", ErrCodexGatewayStateInvalid)
	}
	if strings.TrimSpace(state.Key.SessionKey) == "" {
		return fmt.Errorf("%w: session_key is required", ErrCodexGatewayStateInvalid)
	}
	if strings.TrimSpace(state.Key.IsolationKey) == "" {
		return fmt.Errorf("%w: isolation_key is required", ErrCodexGatewayStateInvalid)
	}
	if strings.TrimSpace(state.Key.Provider) == "" {
		return fmt.Errorf("%w: provider is required", ErrCodexGatewayStateInvalid)
	}
	if strings.TrimSpace(state.Key.UpstreamModel) == "" {
		return fmt.Errorf("%w: upstream_model is required", ErrCodexGatewayStateInvalid)
	}
	seenToolCallIDs := make(map[string]struct{}, len(state.ToolCalls))
	for _, call := range state.ToolCalls {
		callID := strings.TrimSpace(call.ID)
		if callID == "" {
			return fmt.Errorf("%w: tool_call id is required", ErrCodexGatewayStateInvalid)
		}
		if _, exists := seenToolCallIDs[callID]; exists {
			return fmt.Errorf("%w: duplicate tool_call id %q", ErrCodexGatewayStateInvalid, callID)
		}
		seenToolCallIDs[callID] = struct{}{}
	}
	if strings.EqualFold(strings.TrimSpace(state.Key.Provider), "deepseek") && len(state.ToolCalls) > 0 && !state.ReasoningContentPresent && !state.ReasoningContentSynthesized {
		return fmt.Errorf("%w: missing reasoning_content for deepseek tool loop", ErrCodexGatewayStateInvalid)
	}
	return nil
}

func cloneCodexGatewayResponseState(state CodexGatewayResponseState) CodexGatewayResponseState {
	state.ToolCalls = cloneCodexGatewayStoredToolCalls(state.ToolCalls)
	if len(state.ToolNameMap) > 0 {
		state.ToolNameMap = cloneCodexGatewayToolNameMap(state.ToolNameMap)
	}
	state.AnthropicThinkingBlocks = cloneCodexGatewayRawMessages(state.AnthropicThinkingBlocks)
	state.ReplayMessages = cloneCodexGatewayRawMessages(state.ReplayMessages)
	return state
}

func cloneCodexGatewayStoredToolCalls(in []CodexGatewayStoredToolCall) []CodexGatewayStoredToolCall {
	if len(in) == 0 {
		return nil
	}
	out := make([]CodexGatewayStoredToolCall, len(in))
	copy(out, in)
	return out
}

func cloneCodexGatewayToolNameMap(in map[string]CodexGatewayToolNameMapEntry) map[string]CodexGatewayToolNameMapEntry {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]CodexGatewayToolNameMapEntry, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneCodexGatewayRawMessages(in []json.RawMessage) []json.RawMessage {
	if len(in) == 0 {
		return nil
	}
	out := make([]json.RawMessage, 0, len(in))
	for _, raw := range in {
		if len(raw) == 0 {
			continue
		}
		out = append(out, append(json.RawMessage(nil), raw...))
	}
	return out
}

func codexGatewayStateIsolationKey(userID, sessionKey string) string {
	parts := []string{"codex_gateway"}
	if strings.TrimSpace(userID) != "" {
		parts = append(parts, "user="+strings.TrimSpace(userID))
	}
	if strings.TrimSpace(sessionKey) != "" {
		parts = append(parts, "session="+strings.TrimSpace(sessionKey))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return "sub2api_" + hex.EncodeToString(sum[:8])
}
