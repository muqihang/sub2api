package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const ClaudeCodeBridgeCredentialScope = "bridge_pool"

var (
	claudeCodeBridgeSecretTokenRe = regexp.MustCompile(`\b(?:sk|rk|ak)-[A-Za-z0-9._-]+`)
	claudeCodeBridgeRequestIDRe   = regexp.MustCompile(`\b(?:req|request|trace)[_-][A-Za-z0-9._-]+`)
	claudeCodeBridgeURLRe         = regexp.MustCompile(`https?://[^\s"']+`)
)

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
	PreferredProtocol        string
	AnthropicBaseURL         string
	OpenAIBaseURL            string
	FallbackProtocol         string
	FallbackReason           string
	CapabilitiesVerified     bool
	SupportsText             bool
	SupportsTools            bool
	SupportsStreaming        bool
	SupportsUsage            bool
	SupportsCacheAudit       bool
	SupportsReasoningMapping bool
	SupportsErrorPassthrough bool
	ReasoningEffortLevels    []string
	CachePolicy              string
}

type ClaudeCodeBridgeAuditSummary struct {
	ClientType               string `json:"client_type"`
	Route                    string `json:"route"`
	Provider                 string `json:"provider"`
	ModelID                  string `json:"model_id"`
	NativeAttested           bool   `json:"native_attested"`
	FormalPoolAllowed        bool   `json:"formal_pool_allowed"`
	RuntimeHash              string `json:"runtime_hash,omitempty"`
	OverlayHash              string `json:"overlay_hash,omitempty"`
	CatalogHash              string `json:"catalog_hash,omitempty"`
	CatalogVersion           string `json:"catalog_version,omitempty"`
	ProviderOwner            string `json:"provider_owner,omitempty"`
	CredentialScope          string `json:"credential_scope"`
	GatewayLocation          string `json:"gateway_location,omitempty"`
	PreferredProtocol        string `json:"preferred_protocol,omitempty"`
	FallbackProtocol         string `json:"fallback_protocol,omitempty"`
	FallbackReason           string `json:"fallback_reason,omitempty"`
	CapabilitiesVerified     bool   `json:"capabilities_verified,omitempty"`
	SupportsText             bool   `json:"supports_text,omitempty"`
	SupportsTools            bool   `json:"supports_tools,omitempty"`
	SupportsStreaming        bool   `json:"supports_streaming,omitempty"`
	SupportsUsage            bool   `json:"supports_usage,omitempty"`
	SupportsCacheAudit       bool   `json:"supports_cache_audit,omitempty"`
	SupportsReasoningMapping bool   `json:"supports_reasoning_mapping,omitempty"`
	SupportsErrorPassthrough bool   `json:"supports_error_passthrough,omitempty"`
	CachePolicy              string `json:"cache_policy,omitempty"`
	CacheReadTokens          int    `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens         int    `json:"cache_write_tokens,omitempty"`
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
	return ClaudeCodeBridgeStreamResult{Body: stream, Audit: buildClaudeCodeBridgeAuditSummary(decision, ClaudeCodeBridgeProviderFixture{})}, nil
}

type ClaudeCodeBridgeProviderFixture struct {
	TextDeltas       []string
	ReasoningDeltas  []string
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
	StopReason       string
	ErrorType        string
	ErrorMessage     string
}

func BuildClaudeCodeBridgeFixtureSSE(decision ClaudeCodeBridgeRouteDecision, body []byte, fixture ClaudeCodeBridgeProviderFixture) (ClaudeCodeBridgeStreamResult, error) {
	if err := validateClaudeCodeBridgeDecision(decision); err != nil {
		return ClaudeCodeBridgeStreamResult{}, err
	}
	if err := validateClaudeCodeBridgeBodyBinding(decision, body); err != nil {
		return ClaudeCodeBridgeStreamResult{}, err
	}
	if strings.TrimSpace(fixture.ErrorType) != "" {
		return ClaudeCodeBridgeStreamResult{
			Body:  buildClaudeCodeBridgeErrorSSE(fixture.ErrorType, fixture.ErrorMessage),
			Audit: buildClaudeCodeBridgeAuditSummary(decision, fixture),
		}, nil
	}
	text := strings.Join(fixture.TextDeltas, "")
	if strings.TrimSpace(text) == "" {
		text = "bridge fixture"
	}
	stopReason := strings.TrimSpace(fixture.StopReason)
	if stopReason == "" {
		stopReason = "end_turn"
	}
	return ClaudeCodeBridgeStreamResult{
		Body:  buildClaudeCodeBridgeTextSSEWithUsage(decision.ModelID, text, stopReason, fixture.InputTokens, fixture.OutputTokens),
		Audit: buildClaudeCodeBridgeAuditSummary(decision, fixture),
	}, nil
}

func buildClaudeCodeBridgeAuditSummary(decision ClaudeCodeBridgeRouteDecision, fixture ClaudeCodeBridgeProviderFixture) ClaudeCodeBridgeAuditSummary {
	return ClaudeCodeBridgeAuditSummary{
		ClientType:               decision.ClientType,
		Route:                    decision.Route,
		Provider:                 decision.Provider,
		ModelID:                  decision.ModelID,
		NativeAttested:           false,
		FormalPoolAllowed:        false,
		RuntimeHash:              decision.RuntimeHash,
		OverlayHash:              decision.OverlayHash,
		CatalogHash:              decision.CatalogHash,
		CatalogVersion:           safeClaudeCodeNativeLabel(decision.CatalogVersion),
		ProviderOwner:            safeClaudeCodeNativeLabel(decision.ProviderOwner),
		CredentialScope:          decision.CredentialScope,
		GatewayLocation:          safeClaudeCodeNativeLabel(decision.GatewayLocation),
		PreferredProtocol:        safeClaudeCodeNativeLabel(decision.PreferredProtocol),
		FallbackProtocol:         safeClaudeCodeNativeLabel(decision.FallbackProtocol),
		FallbackReason:           safeClaudeCodeNativeLabel(decision.FallbackReason),
		CapabilitiesVerified:     decision.CapabilitiesVerified,
		SupportsText:             decision.SupportsText,
		SupportsTools:            decision.SupportsTools,
		SupportsStreaming:        decision.SupportsStreaming,
		SupportsUsage:            decision.SupportsUsage,
		SupportsCacheAudit:       decision.SupportsCacheAudit,
		SupportsReasoningMapping: decision.SupportsReasoningMapping,
		SupportsErrorPassthrough: decision.SupportsErrorPassthrough,
		CachePolicy:              decision.CachePolicy,
		CacheReadTokens:          fixture.CacheReadTokens,
		CacheWriteTokens:         fixture.CacheWriteTokens,
	}
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
	return buildClaudeCodeBridgeTextSSEWithUsage(model, text, "end_turn", 1, 1)
}

func buildClaudeCodeBridgeTextSSEWithUsage(model string, text string, stopReason string, inputTokens int, outputTokens int) []byte {
	if inputTokens <= 0 {
		inputTokens = 1
	}
	if outputTokens <= 0 {
		outputTokens = 1
	}
	var buf bytes.Buffer
	writeClaudeCodeBridgeSSE(&buf, "message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_bridge_skeleton_cp5", "type": "message", "role": "assistant", "content": []any{}, "model": model, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]any{"input_tokens": inputTokens, "output_tokens": 0}}})
	writeClaudeCodeBridgeSSE(&buf, "content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
	writeClaudeCodeBridgeSSE(&buf, "content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": text}})
	writeClaudeCodeBridgeSSE(&buf, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	writeClaudeCodeBridgeSSE(&buf, "message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stopReason, "stop_sequence": nil}, "usage": map[string]any{"output_tokens": outputTokens}})
	writeClaudeCodeBridgeSSE(&buf, "message_stop", map[string]any{"type": "message_stop"})
	return buf.Bytes()
}

func buildClaudeCodeBridgeErrorSSE(errorType string, message string) []byte {
	var buf bytes.Buffer
	writeClaudeCodeBridgeSSE(&buf, "error", map[string]any{
		"type":  "error",
		"error": map[string]any{"type": safeClaudeCodeNativeLabel(errorType), "message": sanitizeClaudeCodeBridgeErrorMessage(message)},
	})
	return buf.Bytes()
}

func sanitizeClaudeCodeBridgeErrorMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return "provider bridge request failed"
	}
	message = claudeCodeBridgeSecretTokenRe.ReplaceAllString(message, "[redacted-token]")
	message = claudeCodeBridgeRequestIDRe.ReplaceAllString(message, "[redacted-request-id]")
	message = claudeCodeBridgeURLRe.ReplaceAllString(message, "[redacted-url]")
	return strings.ReplaceAll(message, "req_secret", "[redacted-request-id]")
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
