package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
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
	UpstreamModel            string
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

type ClaudeCodeBridgeAnthropicLiveResult struct {
	StatusCode int
	Body       []byte
	Header     http.Header
	Audit      ClaudeCodeBridgeAuditSummary
}

type ClaudeCodeBridgeAnthropicLiveStreamResult struct {
	StatusCode int
	Header     http.Header
	Audit      ClaudeCodeBridgeAuditSummary
}

func ClaudeCodeProviderBridgeLiveRequestAllowed(decision ClaudeCodeProviderRouteDecision) bool {
	if !claudeCodeBridgeEnvEnabled("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED") {
		return false
	}
	bridgeDecision := decision.BridgeRouteDecision()
	switch strings.TrimSpace(bridgeDecision.Provider) {
	case "deepseek":
		return (ClaudeCodeBridgeDeepSeekAPIKeyFromEnv() != "" && ClaudeCodeBridgeAnthropicLiveConfigured() && ClaudeCodeBridgeAnthropicLiveLabBillingBypassEnabled() && ClaudeCodeBridgeAnthropicLiveDecisionValid(bridgeDecision) == nil && claudeCodeBridgeAnthropicUnsafeLabBaseURLAllowed(bridgeDecision)) ||
			ClaudeCodeBridgeDeepSeekOpenAICompatibleFallbackLiveEligible(bridgeDecision)
	case "openai":
		return ClaudeCodeBridgeOpenAIAPIKeyFromEnv() != "" && ClaudeCodeBridgeOpenAILiveConfigured() && ClaudeCodeBridgeAnthropicLiveLabBillingBypassEnabled() && ClaudeCodeBridgeOpenAILiveDecisionValid(bridgeDecision) == nil && claudeCodeBridgeOpenAIUnsafeLabBaseURLAllowed(bridgeDecision)
	default:
		return false
	}
}

func ClaudeCodeBridgeAnthropicLiveEligible(decision ClaudeCodeBridgeRouteDecision) bool {
	return ClaudeCodeBridgeAnthropicLiveConfigured() && ClaudeCodeBridgeAnthropicLiveLabBillingBypassEnabled() && ClaudeCodeBridgeDeepSeekAPIKeyFromEnv() != "" && ClaudeCodeBridgeAnthropicLiveDecisionValid(decision) == nil && claudeCodeBridgeAnthropicUnsafeLabBaseURLAllowed(decision)
}

func ClaudeCodeBridgeAnthropicLiveDecisionValid(decision ClaudeCodeBridgeRouteDecision) error {
	provider := strings.TrimSpace(decision.Provider)
	route := strings.TrimSpace(decision.Route)
	clientType := strings.TrimSpace(decision.ClientType)
	allowed := map[string]struct {
		route      string
		clientType string
	}{
		"deepseek": {route: "deepseek_bridge", clientType: "claude_code_bridge_deepseek"},
		"zai_glm":  {route: "zai_glm_bridge", clientType: "claude_code_bridge_zai_glm"},
		"kimi":     {route: "kimi_bridge", clientType: "claude_code_bridge_kimi"},
	}
	contract, ok := allowed[provider]
	if !ok || route != contract.route || clientType != contract.clientType {
		return fmt.Errorf("claude code bridge live only supports verified anthropic-compatible bridge providers")
	}
	if strings.TrimSpace(decision.PreferredProtocol) != "anthropic_messages" || strings.TrimSpace(decision.AnthropicBaseURL) == "" {
		return fmt.Errorf("claude code bridge live requires anthropic messages")
	}
	if !decision.CapabilitiesVerified || !decision.SupportsText || !decision.SupportsTools || !decision.SupportsStreaming || !decision.SupportsUsage || !decision.SupportsErrorPassthrough {
		return fmt.Errorf("claude code bridge live capabilities are not verified")
	}
	return validateClaudeCodeBridgeDecision(decision)
}

func ExecuteClaudeCodeBridgeAnthropicLive(ctx context.Context, httpClient *http.Client, decision ClaudeCodeBridgeRouteDecision, body []byte, apiKey string) (ClaudeCodeBridgeAnthropicLiveResult, error) {
	var out bytes.Buffer
	result, err := StreamClaudeCodeBridgeAnthropicLive(ctx, httpClient, decision, body, apiKey, &out)
	if err != nil {
		return ClaudeCodeBridgeAnthropicLiveResult{}, err
	}
	return ClaudeCodeBridgeAnthropicLiveResult{
		StatusCode: result.StatusCode,
		Body:       out.Bytes(),
		Header:     result.Header,
		Audit:      result.Audit,
	}, nil
}

func StreamClaudeCodeBridgeAnthropicLive(ctx context.Context, httpClient *http.Client, decision ClaudeCodeBridgeRouteDecision, body []byte, apiKey string, dst io.Writer) (ClaudeCodeBridgeAnthropicLiveStreamResult, error) {
	if !ClaudeCodeBridgeAnthropicLiveConfigured() {
		return ClaudeCodeBridgeAnthropicLiveStreamResult{}, fmt.Errorf("claude code bridge live request is not enabled")
	}
	if !ClaudeCodeBridgeAnthropicLiveLabBillingBypassEnabled() {
		return ClaudeCodeBridgeAnthropicLiveStreamResult{}, fmt.Errorf("claude code bridge live requires billing/concurrency guard")
	}
	if err := ClaudeCodeBridgeAnthropicLiveDecisionValid(decision); err != nil {
		return ClaudeCodeBridgeAnthropicLiveStreamResult{}, err
	}
	if !claudeCodeBridgeAnthropicUnsafeLabBaseURLAllowed(decision) {
		return ClaudeCodeBridgeAnthropicLiveStreamResult{}, fmt.Errorf("claude code bridge unsafe lab bypass requires loopback upstream; external providers require production billing/concurrency guard")
	}
	if strings.TrimSpace(apiKey) == "" {
		return ClaudeCodeBridgeAnthropicLiveStreamResult{}, fmt.Errorf("claude code bridge live api key is required")
	}
	if err := validateClaudeCodeBridgeBodyBinding(decision, body); err != nil {
		return ClaudeCodeBridgeAnthropicLiveStreamResult{}, err
	}
	if dst == nil {
		return ClaudeCodeBridgeAnthropicLiveStreamResult{}, fmt.Errorf("claude code bridge live stream writer is required")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if ctx == nil {
		ctx = context.Background()
	}
	upstreamBody, err := rewriteClaudeCodeBridgeAnthropicBodyModel(decision, body)
	if err != nil {
		return ClaudeCodeBridgeAnthropicLiveStreamResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, buildAnthropicMessagesURL(decision.AnthropicBaseURL), bytes.NewReader(upstreamBody))
	if err != nil {
		return ClaudeCodeBridgeAnthropicLiveStreamResult{}, err
	}
	setCodexGatewayAnthropicHeaders(req, apiKey, true)
	resp, err := httpClient.Do(req)
	if err != nil {
		return ClaudeCodeBridgeAnthropicLiveStreamResult{}, err
	}
	defer resp.Body.Close()
	header := claudeCodeBridgeCloneHTTPHeader(resp.Header)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		limited, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return ClaudeCodeBridgeAnthropicLiveStreamResult{}, fmt.Errorf("claude code bridge upstream status %d: %s", resp.StatusCode, sanitizeClaudeCodeBridgeErrorMessage(string(limited)))
	}
	applyClaudeCodeBridgeLiveResponseHeaders(dst, header)
	if err := copyClaudeCodeBridgeSSE(dst, resp.Body); err != nil {
		return ClaudeCodeBridgeAnthropicLiveStreamResult{}, err
	}
	return ClaudeCodeBridgeAnthropicLiveStreamResult{
		StatusCode: resp.StatusCode,
		Header:     header,
		Audit:      buildClaudeCodeBridgeAuditSummary(decision, ClaudeCodeBridgeProviderFixture{}),
	}, nil
}

func ClaudeCodeBridgeDeepSeekAPIKeyFromEnv() string {
	return strings.TrimSpace(os.Getenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY"))
}

func ClaudeCodeBridgeAnthropicLiveConfigured() bool {
	return claudeCodeBridgeEnvEnabled("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED") && claudeCodeBridgeEnvEnabled("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED")
}

func ClaudeCodeBridgeAnthropicLiveLabBillingBypassEnabled() bool {
	return claudeCodeBridgeEnvEnabled("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB")
}

func claudeCodeBridgeAnthropicUnsafeLabBaseURLAllowed(decision ClaudeCodeBridgeRouteDecision) bool {
	baseURL := strings.TrimSpace(decision.AnthropicBaseURL)
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed == nil {
		return false
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme != "http" && scheme != "https" {
		return false
	}
	hostname := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if hostname == "localhost" {
		return true
	}
	if ip := net.ParseIP(hostname); ip != nil {
		return ip.IsLoopback()
	}
	return false
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
	if looksSensitiveText(claudeCodeBridgeEffectiveUpstreamModel(decision)) {
		return fmt.Errorf("claude code bridge upstream model is invalid")
	}
	if !strings.HasPrefix(decision.ClientType, "claude_code_bridge_") || decision.ClientType == ClaudeCodeNativeClientType {
		return fmt.Errorf("claude code bridge route decision cannot claim native")
	}
	if decision.NativeAttestationAllowed || decision.FormalPoolAllowed || decision.CredentialScope != ClaudeCodeBridgeCredentialScope {
		return fmt.Errorf("claude code bridge route decision cannot use formal pool")
	}
	return nil
}

func claudeCodeBridgeEffectiveUpstreamModel(decision ClaudeCodeBridgeRouteDecision) string {
	upstream := strings.TrimSpace(decision.UpstreamModel)
	if upstream == "" {
		upstream = strings.TrimSpace(decision.ModelID)
	}
	return upstream
}

func rewriteClaudeCodeBridgeAnthropicBodyModel(decision ClaudeCodeBridgeRouteDecision, body []byte) ([]byte, error) {
	upstreamModel := claudeCodeBridgeEffectiveUpstreamModel(decision)
	if upstreamModel == strings.TrimSpace(decision.ModelID) {
		return body, nil
	}
	var payload map[string]any
	if len(body) == 0 || json.Unmarshal(body, &payload) != nil {
		return nil, fmt.Errorf("claude code bridge model rewrite requires JSON body")
	}
	payload["model"] = upstreamModel
	rewritten, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return rewritten, nil
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

func claudeCodeBridgeEnvEnabled(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on", "enabled":
		return true
	default:
		return false
	}
}

func claudeCodeBridgeCloneHTTPHeader(headers http.Header) http.Header {
	clone := http.Header{}
	for key, values := range headers {
		for _, value := range values {
			clone.Add(key, value)
		}
	}
	return clone
}

func applyClaudeCodeBridgeLiveResponseHeaders(dst io.Writer, headers http.Header) {
	headerWriter, ok := dst.(interface{ Header() http.Header })
	if !ok || headerWriter.Header() == nil {
		return
	}
	out := headerWriter.Header()
	out.Set("Content-Type", "text/event-stream")
	out.Set("Cache-Control", "no-cache")
	out.Set("X-Accel-Buffering", "no")
	for _, key := range []string{
		"Content-Type",
		"Cache-Control",
		"X-Request-Id",
		"X-Deepseek-Request-Id",
		"Request-Id",
	} {
		for _, value := range headers.Values(key) {
			if strings.TrimSpace(value) != "" {
				if key == "Content-Type" || key == "Cache-Control" {
					out.Set(key, value)
				} else {
					out.Add(key, value)
				}
			}
		}
	}
}

func copyClaudeCodeBridgeSSE(dst io.Writer, src io.Reader) error {
	flusher, _ := dst.(interface{ Flush() })
	state := claudeCodeBridgeSSESanitizerState{indexMap: map[int]int{}}
	reader := bufio.NewReader(src)
	block := make([]string, 0, 8)
	for {
		line, readErr := reader.ReadString('\n')
		if line != "" {
			block = append(block, line)
			if strings.TrimRight(line, "\r\n") == "" {
				if err := writeClaudeCodeBridgeSafeSSEBlock(dst, block, &state); err != nil {
					return err
				}
				if flusher != nil {
					flusher.Flush()
				}
				block = block[:0]
			}
		}
		if readErr == io.EOF {
			if len(block) > 0 {
				if err := writeClaudeCodeBridgeSafeSSEBlock(dst, block, &state); err != nil {
					return err
				}
				if flusher != nil {
					flusher.Flush()
				}
			}
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

type claudeCodeBridgeSSESanitizerState struct {
	indexMap  map[int]int
	nextIndex int
}

func writeClaudeCodeBridgeSafeSSEBlock(dst io.Writer, block []string, state *claudeCodeBridgeSSESanitizerState) error {
	payload, hasData := claudeCodeBridgeSSEDataPayload(block)
	if !hasData {
		_, err := io.WriteString(dst, strings.Join(block, ""))
		return err
	}
	if strings.TrimSpace(payload) == "[DONE]" {
		_, err := io.WriteString(dst, strings.Join(block, ""))
		return err
	}
	var data any
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		return fmt.Errorf("claude code bridge upstream SSE data is not valid JSON: %w", err)
	}
	if claudeCodeBridgeContainsForeignPrivateReasoning(data, "") {
		return nil
	}
	remapped, keep := remapClaudeCodeBridgeSSEIndex(data, state)
	if !keep {
		return nil
	}
	encoded, err := json.Marshal(remapped)
	if err != nil {
		return err
	}
	wroteData := false
	for _, line := range block {
		if strings.HasPrefix(line, "data:") {
			if !wroteData {
				if _, err := fmt.Fprintf(dst, "data: %s\n", encoded); err != nil {
					return err
				}
				wroteData = true
			}
			continue
		}
		if _, err := io.WriteString(dst, line); err != nil {
			return err
		}
	}
	return nil
}

func claudeCodeBridgeSSEDataPayload(block []string) (string, bool) {
	parts := make([]string, 0, 1)
	for _, line := range block {
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		value := strings.TrimRight(strings.TrimPrefix(line, "data:"), "\r\n")
		value = strings.TrimPrefix(value, " ")
		parts = append(parts, value)
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, "\n"), true
}

func claudeCodeBridgeContainsForeignPrivateReasoning(value any, path string) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalizedKey := strings.ToLower(strings.TrimSpace(key))
			childPath := claudeCodeBridgeSSEChildPath(path, normalizedKey)
			if claudeCodeBridgeForeignPrivateFieldPath(childPath) {
				return true
			}
			if normalizedKey == "type" && claudeCodeBridgeForeignPrivateTypePath(childPath) {
				if text, ok := child.(string); ok && claudeCodeBridgeForeignPrivateType(text) {
					return true
				}
			}
			if claudeCodeBridgeContainsForeignPrivateReasoning(child, childPath) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if claudeCodeBridgeContainsForeignPrivateReasoning(child, path+"[]") {
				return true
			}
		}
	}
	return false
}

func claudeCodeBridgeSSEChildPath(parent string, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

func claudeCodeBridgeForeignPrivateFieldPath(path string) bool {
	switch path {
	case "content_block.thinking",
		"content_block.signature",
		"delta.thinking",
		"delta.signature",
		"delta.reasoning_content",
		"reasoning_content":
		return true
	default:
		return false
	}
}

func claudeCodeBridgeForeignPrivateTypePath(path string) bool {
	switch path {
	case "content_block.type", "delta.type":
		return true
	default:
		return false
	}
}

func claudeCodeBridgeForeignPrivateType(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	return normalized == "thinking" ||
		normalized == "redacted_thinking" ||
		strings.Contains(normalized, "thinking_delta") ||
		strings.Contains(normalized, "signature_delta")
}

func remapClaudeCodeBridgeSSEIndex(value any, state *claudeCodeBridgeSSESanitizerState) (any, bool) {
	obj, ok := value.(map[string]any)
	if !ok {
		return value, true
	}
	eventType, _ := obj["type"].(string)
	rawIndex, hasIndex := claudeCodeBridgeSSEIndex(obj["index"])
	if !hasIndex {
		return value, true
	}
	mapped, found := state.indexMap[rawIndex]
	if !found {
		if eventType != "content_block_start" {
			return nil, false
		}
		mapped = state.nextIndex
		state.indexMap[rawIndex] = mapped
		state.nextIndex++
	}
	obj["index"] = mapped
	return obj, true
}

func claudeCodeBridgeSSEIndex(value any) (int, bool) {
	switch typed := value.(type) {
	case float64:
		index := int(typed)
		return index, typed == float64(index) && index >= 0
	case int:
		return typed, typed >= 0
	default:
		return 0, false
	}
}
