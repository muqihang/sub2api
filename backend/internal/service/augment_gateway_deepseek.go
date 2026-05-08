package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	augmentGatewayDeepSeekThinkingTypeEnabled = "enabled"
	augmentGatewayDeepSeekReasoningEffortMax  = "max"
)

// BuildAugmentGatewayDeepSeekChatCompletionsJSON returns the exact
// OpenAI-compatible DeepSeek request body that the provider executor can send
// upstream.
func BuildAugmentGatewayDeepSeekChatCompletionsJSON(model AugmentGatewayModel, input map[string]any) ([]byte, error) {
	body, err := SanitizeAugmentGatewayDeepSeekChatCompletionsRequest(model, input)
	if err != nil {
		return nil, err
	}
	return json.Marshal(body)
}

// SanitizeAugmentGatewayDeepSeekChatCompletionsRequest applies Augment
// Gateway's DeepSeek V4 request constraints without using omitempty-backed
// message structs, preserving empty string fields such as reasoning_content.
func SanitizeAugmentGatewayDeepSeekChatCompletionsRequest(model AugmentGatewayModel, input map[string]any) (map[string]any, error) {
	upstreamModel := strings.TrimSpace(model.UpstreamModel)
	if upstreamModel == "" {
		upstreamModel = strings.TrimSpace(model.ID)
	}
	if upstreamModel == "" {
		return nil, fmt.Errorf("augment gateway deepseek request requires a model id")
	}
	if model.Provider != "" && model.Provider != AugmentGatewayProviderDeepSeek {
		return nil, fmt.Errorf("augment gateway deepseek request received non-deepseek provider %q", model.Provider)
	}

	body, err := augmentGatewayDeepSeekNormalizeBody(input)
	if err != nil {
		return nil, err
	}

	body["model"] = upstreamModel
	body["thinking"] = map[string]any{"type": augmentGatewayDeepSeekThinkingTypeEnabled}
	body["reasoning_effort"] = augmentGatewayDeepSeekReasoningEffort(model)

	if augmentGatewayDeepSeekHasArrayItems(body["tools"]) {
		delete(body, "tool_choice")
	}

	if messages, ok := body["messages"].([]any); ok {
		messages = augmentGatewayDeepSeekPairToolCallMessages(messages)
		body["messages"] = messages
		toolLoopActive := augmentGatewayDeepSeekToolLoopActive(messages)
		for _, rawMessage := range messages {
			message, ok := rawMessage.(map[string]any)
			if !ok {
				continue
			}
			if augmentGatewayDeepSeekMessageRole(message) != "assistant" {
				continue
			}
			hasToolCalls := augmentGatewayDeepSeekHasArrayItems(message["tool_calls"])
			if hasToolCalls {
				if _, ok := message["content"]; !ok || message["content"] == nil {
					message["content"] = ""
				}
			}
			if toolLoopActive {
				if _, ok := message["reasoning_content"]; !ok || message["reasoning_content"] == nil {
					message["reasoning_content"] = ""
				}
			} else if hasToolCalls {
				if _, ok := message["reasoning_content"]; !ok || message["reasoning_content"] == nil {
					message["reasoning_content"] = ""
				}
			}
		}
	}

	return body, nil
}

func ApplyAugmentGatewayDeepSeekStableUserID(req *AugmentGatewayProviderRequest) {
	if req == nil || req.RawBody == nil {
		return
	}
	if existing, ok := req.RawBody["user_id"].(string); ok && strings.TrimSpace(existing) != "" {
		return
	}
	parts := []string{"augment_gateway_deepseek"}
	switch {
	case req.User != nil && req.User.ID > 0:
		parts = append(parts, fmt.Sprintf("user=%d", req.User.ID))
	case req.APIKey != nil && req.APIKey.UserID > 0:
		parts = append(parts, fmt.Sprintf("user=%d", req.APIKey.UserID))
	case strings.TrimSpace(req.SessionHash) != "":
		parts = append(parts, "session="+strings.TrimSpace(req.SessionHash))
	case req.Account != nil && req.Account.ID > 0:
		parts = append(parts, fmt.Sprintf("account=%d", req.Account.ID))
	default:
		return
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	req.RawBody["user_id"] = "sub2api_" + hex.EncodeToString(sum[:8])
}

func augmentGatewayDeepSeekPairToolCallMessages(messages []any) []any {
	if len(messages) == 0 {
		return messages
	}
	out := make([]any, 0, len(messages))
	for idx := 0; idx < len(messages); {
		rawMessage := messages[idx]
		message, ok := rawMessage.(map[string]any)
		if !ok {
			out = append(out, rawMessage)
			idx++
			continue
		}
		if augmentGatewayDeepSeekMessageRole(message) != "assistant" || !augmentGatewayDeepSeekHasArrayItems(message["tool_calls"]) {
			out = append(out, rawMessage)
			idx++
			continue
		}

		assistantEnd := idx + 1
		for assistantEnd < len(messages) && augmentGatewayDeepSeekIsAssistantToolCallMessage(messages[assistantEnd]) {
			assistantEnd++
		}
		toolEnd := assistantEnd
		for toolEnd < len(messages) && augmentGatewayDeepSeekIsToolMessage(messages[toolEnd]) {
			toolEnd++
		}
		if toolEnd == assistantEnd {
			out = append(out, rawMessage)
			idx++
			continue
		}
		paired, ok := augmentGatewayDeepSeekPairContiguousToolCallBlock(messages[idx:assistantEnd], messages[assistantEnd:toolEnd])
		if !ok {
			out = append(out, messages[idx:toolEnd]...)
			idx = toolEnd
			continue
		}
		out = append(out, paired...)
		idx = toolEnd
	}
	return out
}

func augmentGatewayDeepSeekIsAssistantToolCallMessage(rawMessage any) bool {
	message, ok := rawMessage.(map[string]any)
	return ok && augmentGatewayDeepSeekMessageRole(message) == "assistant" && augmentGatewayDeepSeekHasArrayItems(message["tool_calls"])
}

func augmentGatewayDeepSeekIsToolMessage(rawMessage any) bool {
	message, ok := rawMessage.(map[string]any)
	return ok && augmentGatewayDeepSeekMessageRole(message) == "tool"
}

func augmentGatewayDeepSeekPairContiguousToolCallBlock(assistantMessages, toolMessages []any) ([]any, bool) {
	if len(assistantMessages) == 0 || len(toolMessages) == 0 {
		return nil, false
	}
	toolsByID := make(map[string][]any, len(toolMessages))
	for _, rawTool := range toolMessages {
		tool, ok := rawTool.(map[string]any)
		if !ok {
			continue
		}
		id, _ := tool["tool_call_id"].(string)
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		toolsByID[id] = append(toolsByID[id], rawTool)
	}

	usedToolIDs := make(map[string]struct{}, len(toolMessages))
	paired := make([]any, 0, len(assistantMessages)+len(toolMessages))
	for _, rawAssistant := range assistantMessages {
		assistant, ok := rawAssistant.(map[string]any)
		if !ok {
			return nil, false
		}
		toolCallIDs := augmentGatewayDeepSeekToolCallIDs(assistant["tool_calls"])
		if len(toolCallIDs) == 0 {
			return nil, false
		}
		assistantTools := make([]any, 0, len(toolCallIDs))
		for _, toolCallID := range toolCallIDs {
			if _, ok := usedToolIDs[toolCallID]; ok {
				continue
			}
			matches := toolsByID[toolCallID]
			if len(matches) == 0 {
				return nil, false
			}
			usedToolIDs[toolCallID] = struct{}{}
			assistantTools = append(assistantTools, matches...)
		}
		if len(assistantTools) == 0 {
			return nil, false
		}
		paired = append(paired, rawAssistant)
		paired = append(paired, assistantTools...)
	}
	if len(usedToolIDs) != len(toolsByID) {
		return nil, false
	}
	return paired, true
}

func augmentGatewayDeepSeekToolCallIDs(value any) []string {
	toolCalls, ok := value.([]any)
	if !ok {
		return nil
	}
	ids := make([]string, 0, len(toolCalls))
	for _, rawToolCall := range toolCalls {
		toolCall, ok := rawToolCall.(map[string]any)
		if !ok {
			continue
		}
		id, _ := toolCall["id"].(string)
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

func augmentGatewayDeepSeekToolLoopActive(messages []any) bool {
	for _, rawMessage := range messages {
		message, ok := rawMessage.(map[string]any)
		if !ok {
			continue
		}
		switch augmentGatewayDeepSeekMessageRole(message) {
		case "tool":
			return true
		case "assistant":
			if augmentGatewayDeepSeekHasArrayItems(message["tool_calls"]) {
				return true
			}
		}
	}
	return false
}

func augmentGatewayDeepSeekNormalizeBody(input map[string]any) (map[string]any, error) {
	if input == nil {
		return map[string]any{}, nil
	}

	raw, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal augment gateway deepseek request: %w", err)
	}

	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("normalize augment gateway deepseek request: %w", err)
	}
	if body == nil {
		body = map[string]any{}
	}
	return body, nil
}

func augmentGatewayDeepSeekReasoningEffort(model AugmentGatewayModel) string {
	if effort := strings.TrimSpace(model.ReasoningEffort); effort != "" {
		return effort
	}
	return augmentGatewayDeepSeekReasoningEffortMax
}

func augmentGatewayDeepSeekMessageRole(message map[string]any) string {
	role, _ := message["role"].(string)
	return strings.TrimSpace(role)
}

func augmentGatewayDeepSeekHasArrayItems(value any) bool {
	switch typed := value.(type) {
	case []any:
		return len(typed) > 0
	default:
		return false
	}
}
