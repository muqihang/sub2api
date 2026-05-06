package service

import (
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
		for _, rawMessage := range messages {
			message, ok := rawMessage.(map[string]any)
			if !ok {
				continue
			}
			if augmentGatewayDeepSeekMessageRole(message) != "assistant" {
				continue
			}
			if !augmentGatewayDeepSeekHasArrayItems(message["tool_calls"]) {
				continue
			}
			if _, ok := message["content"]; !ok || message["content"] == nil {
				message["content"] = ""
			}
			if _, ok := message["reasoning_content"]; !ok || message["reasoning_content"] == nil {
				message["reasoning_content"] = ""
			}
		}
	}

	return body, nil
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
