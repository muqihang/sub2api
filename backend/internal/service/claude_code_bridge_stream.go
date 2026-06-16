package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

const ClaudeCodeBridgeCredentialScope = "bridge_pool"

type ClaudeCodeBridgeRouteDecision struct {
	ModelID                  string
	Provider                 string
	Route                    string
	ClientType               string
	RuntimeHash              string
	OverlayHash              string
	CatalogHash              string
	CatalogVersion           string
	ProviderOwner            string
	CredentialScope          string
	GatewayLocation          string
	FormalPoolAllowed        bool
	NativeAttestationAllowed bool
}

type ClaudeCodeBridgeAuditSummary struct {
	ClientType        string `json:"client_type"`
	Route             string `json:"route"`
	Provider          string `json:"provider"`
	ModelID           string `json:"model_id"`
	NativeAttested    bool   `json:"native_attested"`
	FormalPoolAllowed bool   `json:"formal_pool_allowed"`
	RuntimeHash       string `json:"runtime_hash,omitempty"`
	OverlayHash       string `json:"overlay_hash,omitempty"`
	CatalogHash       string `json:"catalog_hash,omitempty"`
	CatalogVersion    string `json:"catalog_version,omitempty"`
	ProviderOwner     string `json:"provider_owner,omitempty"`
	CredentialScope   string `json:"credential_scope"`
	GatewayLocation   string `json:"gateway_location,omitempty"`
}

type ClaudeCodeBridgeStreamResult struct {
	Body  []byte
	Audit ClaudeCodeBridgeAuditSummary
}

func BuildClaudeCodeBridgeSkeletonSSE(decision ClaudeCodeBridgeRouteDecision, body []byte) (ClaudeCodeBridgeStreamResult, error) {
	if err := validateClaudeCodeBridgeDecision(decision); err != nil {
		return ClaudeCodeBridgeStreamResult{}, err
	}
	if err := validateClaudeCodeBridgeBodyBinding(decision, body); err != nil {
		return ClaudeCodeBridgeStreamResult{}, err
	}
	toolName := claudeCodeBridgeSkeletonToolName(body)
	var stream []byte
	if toolName != "" {
		stream = buildClaudeCodeBridgeToolUseSSE(decision.ModelID, toolName)
	} else {
		stream = buildClaudeCodeBridgeTextSSE(decision.ModelID, "bridge skeleton")
	}
	return ClaudeCodeBridgeStreamResult{Body: stream, Audit: ClaudeCodeBridgeAuditSummary{
		ClientType:        decision.ClientType,
		Route:             decision.Route,
		Provider:          decision.Provider,
		ModelID:           decision.ModelID,
		NativeAttested:    false,
		FormalPoolAllowed: false,
		RuntimeHash:       decision.RuntimeHash,
		OverlayHash:       decision.OverlayHash,
		CatalogHash:       decision.CatalogHash,
		CatalogVersion:    safeClaudeCodeNativeLabel(decision.CatalogVersion),
		ProviderOwner:     safeClaudeCodeNativeLabel(decision.ProviderOwner),
		CredentialScope:   decision.CredentialScope,
		GatewayLocation:   safeClaudeCodeNativeLabel(decision.GatewayLocation),
	}}, nil
}

func validateClaudeCodeBridgeBodyBinding(decision ClaudeCodeBridgeRouteDecision, body []byte) error {
	var payload map[string]any
	if len(body) == 0 || json.Unmarshal(body, &payload) != nil {
		return fmt.Errorf("claude code bridge model binding requires JSON body")
	}
	model, _ := payload["model"].(string)
	if strings.TrimSpace(model) == "" || strings.TrimSpace(model) != decision.ModelID {
		return fmt.Errorf("claude code bridge model binding mismatch")
	}
	for _, field := range anthropicCompatOpenAIOnlyTopLevelFields {
		if _, exists := payload[field]; exists {
			return fmt.Errorf("claude code bridge body must use Anthropic messages shape")
		}
	}
	messages, ok := payload["messages"].([]any)
	if !ok {
		return fmt.Errorf("claude code bridge body must include messages")
	}
	for _, item := range messages {
		message, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("claude code bridge message shape is invalid")
		}
		role, _ := message["role"].(string)
		switch role {
		case "system", "user", "assistant":
		default:
			return fmt.Errorf("claude code bridge message role is invalid")
		}
	}
	toolNames := map[string]struct{}{}
	if rawTools, exists := payload["tools"]; exists {
		tools, ok := rawTools.([]any)
		if !ok {
			return fmt.Errorf("claude code bridge tool shape is invalid")
		}
		for _, item := range tools {
			tool, ok := item.(map[string]any)
			if !ok {
				return fmt.Errorf("claude code bridge tool shape is invalid")
			}
			if _, exists := tool["function"]; exists || tool["type"] == "function" {
				return fmt.Errorf("claude code bridge body must use Anthropic messages tool shape")
			}
			name, _ := tool["name"].(string)
			_, schemaOK := tool["input_schema"].(map[string]any)
			name = strings.TrimSpace(name)
			if !schemaOK || !anthropicCompatSafeToolNameRE.MatchString(name) {
				return fmt.Errorf("claude code bridge tool shape is invalid")
			}
			toolNames[name] = struct{}{}
		}
	}
	if rawChoice, exists := payload["tool_choice"]; exists {
		choice, ok := rawChoice.(map[string]any)
		if !ok {
			return fmt.Errorf("claude code bridge tool choice shape is invalid")
		}
		if _, exists := choice["function"]; exists || choice["type"] == "function" {
			return fmt.Errorf("claude code bridge body must use Anthropic messages tool choice")
		}
		choiceType, _ := choice["type"].(string)
		switch choiceType {
		case "tool":
			name, _ := choice["name"].(string)
			name = strings.TrimSpace(name)
			if !anthropicCompatSafeToolNameRE.MatchString(name) {
				return fmt.Errorf("claude code bridge tool choice shape is invalid")
			}
			if _, ok := toolNames[name]; !ok {
				return fmt.Errorf("claude code bridge tool choice shape is invalid")
			}
		case "auto", "any", "none":
		default:
			return fmt.Errorf("claude code bridge tool choice shape is invalid")
		}
	}
	return nil
}

func validateClaudeCodeBridgeDecision(decision ClaudeCodeBridgeRouteDecision) error {
	if strings.TrimSpace(decision.ModelID) == "" || strings.TrimSpace(decision.Provider) == "" || strings.TrimSpace(decision.Route) == "" {
		return fmt.Errorf("claude code bridge route decision is incomplete")
	}
	if !strings.HasPrefix(decision.ClientType, "claude_code_bridge_") || decision.ClientType == ClaudeCodeNativeClientType {
		return fmt.Errorf("claude code bridge route decision cannot claim native")
	}
	if decision.NativeAttestationAllowed || decision.FormalPoolAllowed || decision.CredentialScope != ClaudeCodeBridgeCredentialScope {
		return fmt.Errorf("claude code bridge route decision cannot use formal pool")
	}
	return nil
}

func claudeCodeBridgeSkeletonToolName(body []byte) string {
	var payload map[string]any
	if len(body) == 0 || json.Unmarshal(body, &payload) != nil {
		return ""
	}
	if choice, ok := payload["tool_choice"].(map[string]any); ok && choice["type"] == "tool" {
		if name, ok := choice["name"].(string); ok && anthropicCompatSafeToolNameRE.MatchString(strings.TrimSpace(name)) {
			return strings.TrimSpace(name)
		}
	}
	if tools, ok := payload["tools"].([]any); ok && len(tools) > 0 {
		if tool, ok := tools[0].(map[string]any); ok {
			if name, ok := tool["name"].(string); ok && anthropicCompatSafeToolNameRE.MatchString(strings.TrimSpace(name)) {
				return strings.TrimSpace(name)
			}
		}
	}
	return ""
}

func buildClaudeCodeBridgeTextSSE(model string, text string) []byte {
	var buf bytes.Buffer
	writeClaudeCodeBridgeSSE(&buf, "message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_bridge_skeleton_cp5", "type": "message", "role": "assistant", "content": []any{}, "model": model, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1}}})
	writeClaudeCodeBridgeSSE(&buf, "content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
	writeClaudeCodeBridgeSSE(&buf, "content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": text}})
	writeClaudeCodeBridgeSSE(&buf, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	writeClaudeCodeBridgeSSE(&buf, "message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]any{"output_tokens": 1}})
	writeClaudeCodeBridgeSSE(&buf, "message_stop", map[string]any{"type": "message_stop"})
	return buf.Bytes()
}

func buildClaudeCodeBridgeToolUseSSE(model string, toolName string) []byte {
	partial, _ := json.Marshal(map[string]any{"city": "San Francisco"})
	var buf bytes.Buffer
	writeClaudeCodeBridgeSSE(&buf, "message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_bridge_skeleton_cp5", "type": "message", "role": "assistant", "content": []any{}, "model": model, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1}}})
	writeClaudeCodeBridgeSSE(&buf, "content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "tool_use", "id": "toolu_bridge_skeleton_cp5", "name": toolName, "input": map[string]any{}}})
	writeClaudeCodeBridgeSSE(&buf, "content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(partial)}})
	writeClaudeCodeBridgeSSE(&buf, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	writeClaudeCodeBridgeSSE(&buf, "message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "tool_use", "stop_sequence": nil}, "usage": map[string]any{"output_tokens": 1}})
	writeClaudeCodeBridgeSSE(&buf, "message_stop", map[string]any{"type": "message_stop"})
	return buf.Bytes()
}

func writeClaudeCodeBridgeSSE(buf *bytes.Buffer, event string, payload map[string]any) {
	raw, _ := json.Marshal(payload)
	buf.WriteString("event: ")
	buf.WriteString(event)
	buf.WriteString("\n")
	buf.WriteString("data: ")
	buf.Write(raw)
	buf.WriteString("\n\n")
}
