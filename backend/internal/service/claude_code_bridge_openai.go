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
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/tidwall/gjson"
)

type ClaudeCodeBridgeOpenAILiveResult struct {
	StatusCode int
	Body       []byte
	Header     http.Header
	Audit      ClaudeCodeBridgeAuditSummary
}

type ClaudeCodeBridgeOpenAILiveStreamResult struct {
	StatusCode int
	Header     http.Header
	Audit      ClaudeCodeBridgeAuditSummary
}

func ClaudeCodeBridgeOpenAIAPIKeyFromEnv() string {
	return ClaudeCodeBridgeOpenAICompatibleAPIKeyFromEnv("openai")
}

func ClaudeCodeBridgeOpenAICompatibleAPIKeyFromEnv(provider string) string {
	switch strings.TrimSpace(provider) {
	case "openai":
		return strings.TrimSpace(os.Getenv("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_API_KEY"))
	case "agnes":
		return strings.TrimSpace(os.Getenv("SUB2API_CLAUDE_CODE_BRIDGE_AGNES_API_KEY"))
	default:
		return ""
	}
}

func ClaudeCodeBridgeOpenAILiveConfigured() bool {
	return claudeCodeBridgeOpenAICompatibleLiveConfigured("openai")
}

func claudeCodeBridgeOpenAICompatibleLiveConfigured(provider string) bool {
	if !claudeCodeBridgeEnvEnabled("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED") {
		return false
	}
	switch strings.TrimSpace(provider) {
	case "openai":
		return claudeCodeBridgeEnvEnabled("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_LIVE_ENABLED")
	case "agnes":
		return claudeCodeBridgeEnvEnabled("SUB2API_CLAUDE_CODE_BRIDGE_AGNES_LIVE_ENABLED")
	default:
		return false
	}
}

func ClaudeCodeBridgeOpenAILiveEligible(decision ClaudeCodeBridgeRouteDecision) bool {
	provider := strings.TrimSpace(decision.Provider)
	return claudeCodeBridgeOpenAICompatibleLiveConfigured(provider) && ClaudeCodeBridgeAnthropicLiveLabBillingBypassEnabled() && ClaudeCodeBridgeOpenAICompatibleAPIKeyFromEnv(provider) != "" && ClaudeCodeBridgeOpenAILiveDecisionValid(decision) == nil && claudeCodeBridgeOpenAIUnsafeLabBaseURLAllowed(decision)
}

func ClaudeCodeBridgeDeepSeekOpenAICompatibleFallbackLiveEligible(decision ClaudeCodeBridgeRouteDecision) bool {
	return claudeCodeBridgeDeepSeekOpenAICompatibleFallbackExplicitlyEnabled() && ClaudeCodeBridgeAnthropicLiveConfigured() && ClaudeCodeBridgeAnthropicLiveLabBillingBypassEnabled() && ClaudeCodeBridgeDeepSeekAPIKeyFromEnv() != "" && ClaudeCodeBridgeDeepSeekOpenAICompatibleFallbackDecisionValid(decision) == nil && claudeCodeBridgeOpenAIUnsafeLabBaseURLAllowed(decision)
}

func ClaudeCodeBridgeOpenAILiveDecisionValid(decision ClaudeCodeBridgeRouteDecision) error {
	provider := strings.TrimSpace(decision.Provider)
	allowed := map[string]struct {
		route      string
		clientType string
	}{
		"openai": {route: "openai_bridge", clientType: "claude_code_bridge_openai"},
		"agnes":  {route: "agnes_bridge", clientType: "claude_code_bridge_agnes"},
	}
	contract, ok := allowed[provider]
	if !ok || strings.TrimSpace(decision.Route) != contract.route || strings.TrimSpace(decision.ClientType) != contract.clientType {
		return fmt.Errorf("claude code OpenAI-compatible bridge live only supports verified responses bridge providers")
	}
	if strings.TrimSpace(decision.PreferredProtocol) != "responses" || strings.TrimSpace(decision.OpenAIBaseURL) == "" {
		return fmt.Errorf("claude code OpenAI-compatible bridge live requires responses")
	}
	if !decision.CapabilitiesVerified || !decision.SupportsText || !decision.SupportsTools || !decision.SupportsStreaming || !decision.SupportsUsage || !decision.SupportsErrorPassthrough {
		return fmt.Errorf("claude code OpenAI bridge live capabilities are not verified")
	}
	return validateClaudeCodeBridgeDecision(decision)
}

func ExecuteClaudeCodeBridgeOpenAILive(ctx context.Context, httpClient *http.Client, decision ClaudeCodeBridgeRouteDecision, body []byte, apiKey string) (ClaudeCodeBridgeOpenAILiveResult, error) {
	var out bytes.Buffer
	result, err := StreamClaudeCodeBridgeOpenAILive(ctx, httpClient, decision, body, apiKey, &out)
	if err != nil {
		return ClaudeCodeBridgeOpenAILiveResult{}, err
	}
	return ClaudeCodeBridgeOpenAILiveResult{
		StatusCode: result.StatusCode,
		Body:       out.Bytes(),
		Header:     result.Header,
		Audit:      result.Audit,
	}, nil
}

func StreamClaudeCodeBridgeOpenAILive(ctx context.Context, httpClient *http.Client, decision ClaudeCodeBridgeRouteDecision, body []byte, apiKey string, dst io.Writer) (ClaudeCodeBridgeOpenAILiveStreamResult, error) {
	if !claudeCodeBridgeOpenAICompatibleLiveConfigured(decision.Provider) {
		return ClaudeCodeBridgeOpenAILiveStreamResult{}, fmt.Errorf("claude code OpenAI-compatible bridge live request is not enabled")
	}
	if !ClaudeCodeBridgeAnthropicLiveLabBillingBypassEnabled() {
		return ClaudeCodeBridgeOpenAILiveStreamResult{}, fmt.Errorf("claude code OpenAI bridge live requires billing/concurrency guard")
	}
	if err := ClaudeCodeBridgeOpenAILiveDecisionValid(decision); err != nil {
		return ClaudeCodeBridgeOpenAILiveStreamResult{}, err
	}
	if !claudeCodeBridgeOpenAIUnsafeLabBaseURLAllowed(decision) {
		return ClaudeCodeBridgeOpenAILiveStreamResult{}, fmt.Errorf("claude code OpenAI bridge unsafe lab bypass requires loopback upstream; external providers require production billing/concurrency guard")
	}
	if strings.TrimSpace(apiKey) == "" {
		return ClaudeCodeBridgeOpenAILiveStreamResult{}, fmt.Errorf("claude code OpenAI bridge live api key is required")
	}
	if err := validateClaudeCodeBridgeBodyBinding(decision, body); err != nil {
		return ClaudeCodeBridgeOpenAILiveStreamResult{}, err
	}
	if dst == nil {
		return ClaudeCodeBridgeOpenAILiveStreamResult{}, fmt.Errorf("claude code OpenAI bridge live stream writer is required")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if ctx == nil {
		ctx = context.Background()
	}
	openAIBody, err := buildClaudeCodeOpenAIResponsesBody(decision, body)
	if err != nil {
		return ClaudeCodeBridgeOpenAILiveStreamResult{}, err
	}
	upstreamURL := buildOpenAIEndpointURL(decision.OpenAIBaseURL, "/v1/responses")
	requestAudit := buildClaudeCodeBridgeOpenAIResponsesRequestAudit(decision, openAIBody, upstreamURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(openAIBody))
	if err != nil {
		return ClaudeCodeBridgeOpenAILiveStreamResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	applyClaudeCodeBridgeOpenAIClientHeaders(req.Header, decision.ClientType)
	resp, err := httpClient.Do(req)
	if err != nil {
		return ClaudeCodeBridgeOpenAILiveStreamResult{}, err
	}
	defer resp.Body.Close()
	header := claudeCodeBridgeCloneHTTPHeader(resp.Header)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		limited, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if shouldFallbackClaudeCodeOpenAIResponsesToChat(decision, resp.StatusCode) {
			return streamClaudeCodeBridgeOpenAIChatCompletionsFallback(ctx, httpClient, decision, body, apiKey, dst)
		}
		return ClaudeCodeBridgeOpenAILiveStreamResult{}, fmt.Errorf("claude code OpenAI bridge upstream status %d: %s", resp.StatusCode, sanitizeClaudeCodeBridgeErrorMessage(string(limited)))
	}
	applyClaudeCodeBridgeLiveResponseHeaders(dst, header)
	cacheReadTokens, err := copyClaudeCodeOpenAIResponsesAsAnthropicSSE(dst, resp.Body, decision.ModelID)
	if err != nil {
		return ClaudeCodeBridgeOpenAILiveStreamResult{}, err
	}
	fixture := ClaudeCodeBridgeProviderFixture{CacheReadTokens: cacheReadTokens}
	return ClaudeCodeBridgeOpenAILiveStreamResult{
		StatusCode: resp.StatusCode,
		Header:     header,
		Audit:      buildClaudeCodeBridgeAuditSummaryWithRequest(decision, fixture, requestAudit),
	}, nil
}

func buildClaudeCodeOpenAIResponsesBody(decision ClaudeCodeBridgeRouteDecision, body []byte) ([]byte, error) {
	var anthropicReq apicompat.AnthropicRequest
	if err := json.Unmarshal(body, &anthropicReq); err != nil {
		return nil, fmt.Errorf("claude code OpenAI bridge request must be Anthropic messages JSON")
	}
	anthropicReq.Model = claudeCodeBridgeEffectiveUpstreamModel(decision)
	explicitEffort := anthropicReq.OutputConfig != nil && strings.TrimSpace(anthropicReq.OutputConfig.Effort) != ""
	requestedEffort := ""
	if explicitEffort {
		if normalized := NormalizeClaudeOutputEffort(anthropicReq.OutputConfig.Effort); normalized != nil {
			requestedEffort = *normalized
		}
	}
	responsesReq, err := apicompat.AnthropicToResponses(&anthropicReq)
	if err != nil {
		return nil, err
	}
	responsesReq.Stream = true
	applyClaudeCodeOpenAIResponsesEffortPolicy(decision, responsesReq, explicitEffort, requestedEffort)
	if strings.TrimSpace(responsesReq.PromptCacheKey) == "" && shouldAutoInjectPromptCacheKeyForCompat(responsesReq.Model) {
		responsesReq.PromptCacheKey = deriveAnthropicCompatPromptCacheKey(&anthropicReq, responsesReq.Model)
	}
	return json.Marshal(responsesReq)
}

func applyClaudeCodeOpenAIResponsesEffortPolicy(decision ClaudeCodeBridgeRouteDecision, req *apicompat.ResponsesRequest, explicitEffort bool, requestedEffort string) {
	if req == nil {
		return
	}
	levels := normalizedClaudeCodeBridgeReasoningEffortLevels(decision)
	if len(levels) == 0 || !explicitEffort || requestedEffort == "" || req.Reasoning == nil || strings.TrimSpace(req.Reasoning.Effort) == "" {
		req.Reasoning = nil
		req.Include = claudeCodeBridgeRemoveReasoningIncludes(req.Include)
		return
	}
	if _, ok := levels[requestedEffort]; !ok {
		req.Reasoning = nil
		req.Include = claudeCodeBridgeRemoveReasoningIncludes(req.Include)
	}
}

func claudeCodeBridgeRemoveReasoningIncludes(in []string) []string {
	out := in[:0]
	for _, item := range in {
		if strings.HasPrefix(strings.TrimSpace(item), "reasoning.") {
			continue
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func applyClaudeCodeBridgeOpenAIClientHeaders(headers http.Header, clientType string) {
	if headers == nil {
		return
	}
	clientType = strings.TrimSpace(clientType)
	if clientType == "" {
		clientType = "claude_code_bridge_openai"
	}
	// This request is an internal Claude Code bridge hop into the local Sub2API
	// OpenAI gateway. Use a stable synthetic Codex-compatible client profile so
	// the gateway can apply its existing OpenAI/Codex account safety rewrites,
	// without forwarding the end user's Claude Code UA or local identity.
	headers.Set("User-Agent", "codex_cli_rs/0.125.0 zhumeng-cc-bridge")
	headers.Set("originator", "codex_cli_rs")
	headers.Set("X-Stainless-Lang", "go")
	headers.Set("X-Stainless-Package-Version", "0.125.0")
	headers.Set("X-Stainless-OS", "Unknown")
	headers.Set("X-Stainless-Arch", "unknown")
	headers.Set("X-Stainless-Runtime", "go")
	headers.Set("X-Stainless-Runtime-Version", "1.26")
	headers.Set("X-Sub2API-Client-Type", clientType)
}

func buildClaudeCodeBridgeOpenAIResponsesRequestAudit(decision ClaudeCodeBridgeRouteDecision, upstreamBody []byte, upstreamURL string) claudeCodeBridgeRequestAudit {
	audit := claudeCodeBridgeRequestAudit{
		SelectedProtocol:        strings.TrimSpace(decision.PreferredProtocol),
		FallbackUsed:            false,
		UpstreamPathKind:        safeClaudeCodeBridgeUpstreamPathKind(upstreamURL),
		CacheControlLocations:   claudeCodeBridgeCacheControlLocations(upstreamBody),
		PromptCacheKeyPresent:   strings.TrimSpace(gjson.GetBytes(upstreamBody, "prompt_cache_key").String()) != "",
		StablePrefixTokenBucket: "",
	}
	audit.CacheControlPresent = len(audit.CacheControlLocations) > 0
	switch strings.TrimSpace(decision.Provider) {
	case "openai":
		audit.ProviderCacheMechanism = "openai_prompt_cache"
	case "agnes":
		audit.ProviderCacheMechanism = "openai_responses_compatible_cache_unverified"
	}
	audit.StablePrefixHMAC, audit.StablePrefixTokenBucket = claudeCodeBridgeStablePrefixAudit(decision, upstreamBody)
	return audit
}

func shouldFallbackClaudeCodeOpenAIResponsesToChat(decision ClaudeCodeBridgeRouteDecision, statusCode int) bool {
	if strings.TrimSpace(decision.Provider) != "openai" {
		return false
	}
	if !claudeCodeBridgeEnvEnabled("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_CHAT_COMPLETIONS_FALLBACK_ENABLED") {
		return false
	}
	switch statusCode {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented, http.StatusBadGateway:
		return true
	default:
		return false
	}
}

func streamClaudeCodeBridgeOpenAIChatCompletionsFallback(ctx context.Context, httpClient *http.Client, decision ClaudeCodeBridgeRouteDecision, body []byte, apiKey string, dst io.Writer) (ClaudeCodeBridgeOpenAILiveStreamResult, error) {
	chatBody, err := buildClaudeCodeOpenAIChatCompletionsBody(decision, body)
	if err != nil {
		return ClaudeCodeBridgeOpenAILiveStreamResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, buildOpenAIEndpointURL(decision.OpenAIBaseURL, "/v1/chat/completions"), bytes.NewReader(chatBody))
	if err != nil {
		return ClaudeCodeBridgeOpenAILiveStreamResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	applyClaudeCodeBridgeOpenAIClientHeaders(req.Header, decision.ClientType)
	resp, err := httpClient.Do(req)
	if err != nil {
		return ClaudeCodeBridgeOpenAILiveStreamResult{}, err
	}
	defer resp.Body.Close()
	header := claudeCodeBridgeCloneHTTPHeader(resp.Header)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		limited, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return ClaudeCodeBridgeOpenAILiveStreamResult{}, fmt.Errorf("claude code OpenAI bridge chat completions fallback upstream status %d: %s", resp.StatusCode, sanitizeClaudeCodeBridgeErrorMessage(string(limited)))
	}
	applyClaudeCodeBridgeLiveResponseHeaders(dst, header)
	fixture, err := copyClaudeCodeChatCompletionsAsAnthropicSSE(dst, resp.Body, decision.ModelID)
	if err != nil {
		return ClaudeCodeBridgeOpenAILiveStreamResult{}, err
	}
	requestAudit := buildClaudeCodeBridgeOpenAIChatFallbackRequestAudit(decision, body, req.URL.String(), true)
	return ClaudeCodeBridgeOpenAILiveStreamResult{
		StatusCode: resp.StatusCode,
		Header:     header,
		Audit:      buildClaudeCodeBridgeAuditSummaryWithRequest(decision, fixture, requestAudit),
	}, nil
}

func buildClaudeCodeOpenAIChatCompletionsBody(decision ClaudeCodeBridgeRouteDecision, body []byte) ([]byte, error) {
	responsesBody, err := buildClaudeCodeOpenAIResponsesBody(decision, body)
	if err != nil {
		return nil, err
	}
	var responsesReq apicompat.ResponsesRequest
	if err := json.Unmarshal(responsesBody, &responsesReq); err != nil {
		return nil, err
	}
	chatReq, err := apicompat.ResponsesToChatCompletionsRequest(&responsesReq)
	if err != nil {
		return nil, err
	}
	chatReq.Stream = true
	chatReq.StreamOptions = &apicompat.ChatStreamOptions{IncludeUsage: true}
	return json.Marshal(chatReq)
}

func copyClaudeCodeOpenAIResponsesAsAnthropicSSE(dst io.Writer, src io.Reader, model string) (int, error) {
	state := apicompat.NewResponsesEventToAnthropicState()
	state.Model = model
	cacheReadTokens := 0
	terminalSeen := false
	terminalError := false
	flusher, _ := dst.(interface{ Flush() })
	emit := func(events []apicompat.AnthropicStreamEvent) error {
		for _, event := range events {
			wire, err := apicompat.ResponsesAnthropicEventToSSE(event)
			if err != nil {
				return err
			}
			if _, err := io.WriteString(dst, wire); err != nil {
				return err
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		return nil
	}
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	parser := claudeCodeBridgeSSEFrameParser{}
	for scanner.Scan() {
		frame, ok := parser.AddLine(strings.TrimRight(scanner.Text(), "\r"))
		if !ok {
			continue
		}
		events, raw, cacheRead, terminal, failed, err := claudeCodeBridgeOpenAIFrameToAnthropic(frame, state)
		if err != nil {
			return cacheReadTokens, err
		}
		if cacheRead > 0 {
			cacheReadTokens = cacheRead
		}
		if terminal {
			terminalSeen = true
		}
		if failed {
			terminalError = true
		}
		if len(raw) > 0 {
			if _, err := dst.Write(raw); err != nil {
				return cacheReadTokens, err
			}
			if flusher != nil {
				flusher.Flush()
			}
			continue
		}
		if err := emit(events); err != nil {
			return cacheReadTokens, err
		}
	}
	if err := scanner.Err(); err != nil {
		return cacheReadTokens, err
	}
	if frame, ok := parser.Finish(); ok {
		events, raw, cacheRead, terminal, failed, err := claudeCodeBridgeOpenAIFrameToAnthropic(frame, state)
		if err != nil {
			return cacheReadTokens, err
		}
		if cacheRead > 0 {
			cacheReadTokens = cacheRead
		}
		if terminal {
			terminalSeen = true
		}
		if failed {
			terminalError = true
		}
		if len(raw) > 0 {
			if _, err := dst.Write(raw); err != nil {
				return cacheReadTokens, err
			}
			if flusher != nil {
				flusher.Flush()
			}
		} else if err := emit(events); err != nil {
			return cacheReadTokens, err
		}
	}
	if !terminalSeen {
		if _, err := dst.Write(buildClaudeCodeBridgeErrorSSE("upstream_stream_closed", "OpenAI bridge upstream stream closed before terminal event")); err != nil {
			return cacheReadTokens, err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return cacheReadTokens, nil
	}
	if !terminalError {
		if err := emit(apicompat.FinalizeResponsesAnthropicStream(state)); err != nil {
			return cacheReadTokens, err
		}
	}
	return cacheReadTokens, nil
}

type claudeCodeBridgeSSEFrame struct {
	EventType string
	Data      string
}

type claudeCodeBridgeSSEFrameParser struct {
	eventType string
	dataLines []string
}

func (p *claudeCodeBridgeSSEFrameParser) AddLine(line string) (claudeCodeBridgeSSEFrame, bool) {
	if line == "" {
		return p.dispatch()
	}
	if strings.HasPrefix(line, ":") {
		return claudeCodeBridgeSSEFrame{}, false
	}
	if eventType, ok := strings.CutPrefix(line, "event:"); ok {
		p.eventType = strings.TrimSpace(eventType)
		return claudeCodeBridgeSSEFrame{}, false
	}
	if data, ok := strings.CutPrefix(line, "data:"); ok {
		p.dataLines = append(p.dataLines, strings.TrimSpace(data))
	}
	return claudeCodeBridgeSSEFrame{}, false
}

func (p *claudeCodeBridgeSSEFrameParser) Finish() (claudeCodeBridgeSSEFrame, bool) {
	return p.dispatch()
}

func (p *claudeCodeBridgeSSEFrameParser) dispatch() (claudeCodeBridgeSSEFrame, bool) {
	frame := claudeCodeBridgeSSEFrame{EventType: p.eventType, Data: strings.Join(p.dataLines, "\n")}
	p.eventType = ""
	p.dataLines = nil
	return frame, strings.TrimSpace(frame.Data) != ""
}

func claudeCodeBridgeOpenAIFrameToAnthropic(frame claudeCodeBridgeSSEFrame, state *apicompat.ResponsesEventToAnthropicState) ([]apicompat.AnthropicStreamEvent, []byte, int, bool, bool, error) {
	if strings.TrimSpace(frame.Data) == "" || strings.TrimSpace(frame.Data) == "[DONE]" {
		return nil, nil, 0, false, false, nil
	}
	var event apicompat.ResponsesStreamEvent
	if err := json.Unmarshal([]byte(frame.Data), &event); err != nil {
		return nil, nil, 0, false, false, err
	}
	if event.Type == "" {
		event.Type = strings.TrimSpace(frame.EventType)
	}
	if claudeCodeOpenAIResponsesEventIsForeignReasoning(event) {
		return nil, nil, 0, false, false, nil
	}
	terminal, failed := claudeCodeBridgeOpenAIEventTerminalState(event)
	if failed {
		errorType, message := claudeCodeBridgeOpenAIEventError(frame.Data, event)
		return nil, buildClaudeCodeBridgeErrorSSE(errorType, message), 0, terminal, true, nil
	}
	cacheRead := 0
	if event.Usage != nil && event.Usage.InputTokensDetails != nil {
		cacheRead = event.Usage.InputTokensDetails.CachedTokens
	}
	if event.Response != nil && event.Response.Usage != nil && event.Response.Usage.InputTokensDetails != nil {
		cacheRead = event.Response.Usage.InputTokensDetails.CachedTokens
	}
	return apicompat.ResponsesEventToAnthropicEvents(&event, state), nil, cacheRead, terminal, false, nil
}

func claudeCodeBridgeOpenAIEventError(rawData string, event apicompat.ResponsesStreamEvent) (string, string) {
	errorType := strings.TrimSpace(event.Code)
	message := "OpenAI bridge provider request failed"
	if topLevelType, topLevelCode, topLevelMessage := claudeCodeBridgeOpenAITopLevelError(rawData); topLevelType != "" || topLevelCode != "" || topLevelMessage != "" {
		if topLevelType != "" {
			errorType = topLevelType
		} else if topLevelCode != "" {
			errorType = topLevelCode
		}
		if topLevelMessage != "" {
			message = topLevelMessage
		}
	}
	if event.Response != nil && event.Response.Error != nil {
		if strings.TrimSpace(event.Response.Error.Code) != "" {
			errorType = strings.TrimSpace(event.Response.Error.Code)
		}
		if strings.TrimSpace(event.Response.Error.Message) != "" {
			message = strings.TrimSpace(event.Response.Error.Message)
		}
	}
	if errorType == "" {
		errorType = "api_error"
	}
	return errorType, message
}

func claudeCodeBridgeOpenAIEventTerminalState(event apicompat.ResponsesStreamEvent) (bool, bool) {
	switch strings.TrimSpace(event.Type) {
	case "error", "response.failed":
		return true, true
	case "response.completed", "response.done", "response.incomplete":
		return true, false
	default:
		return false, false
	}
}

func claudeCodeBridgeOpenAITopLevelError(rawData string) (string, string, string) {
	var payload struct {
		Error *struct {
			Type    string `json:"type"`
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(rawData), &payload); err != nil || payload.Error == nil {
		return "", "", ""
	}
	return strings.TrimSpace(payload.Error.Type), strings.TrimSpace(payload.Error.Code), strings.TrimSpace(payload.Error.Message)
}

func claudeCodeOpenAIResponsesEventIsForeignReasoning(event apicompat.ResponsesStreamEvent) bool {
	if strings.Contains(strings.ToLower(event.Type), "reasoning") {
		return true
	}
	return event.Item != nil && strings.EqualFold(event.Item.Type, "reasoning")
}

func claudeCodeBridgeOpenAIUnsafeLabBaseURLAllowed(decision ClaudeCodeBridgeRouteDecision) bool {
	baseURL := strings.TrimSpace(decision.OpenAIBaseURL)
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

func ExecuteClaudeCodeBridgeDeepSeekOpenAICompatibleFallbackLive(ctx context.Context, httpClient *http.Client, decision ClaudeCodeBridgeRouteDecision, body []byte, apiKey string) (ClaudeCodeBridgeOpenAILiveResult, error) {
	var out bytes.Buffer
	result, err := StreamClaudeCodeBridgeDeepSeekOpenAICompatibleFallbackLive(ctx, httpClient, decision, body, apiKey, &out)
	if err != nil {
		return ClaudeCodeBridgeOpenAILiveResult{}, err
	}
	return ClaudeCodeBridgeOpenAILiveResult{
		StatusCode: result.StatusCode,
		Body:       out.Bytes(),
		Header:     result.Header,
		Audit:      result.Audit,
	}, nil
}

func StreamClaudeCodeBridgeDeepSeekOpenAICompatibleFallbackLive(ctx context.Context, httpClient *http.Client, decision ClaudeCodeBridgeRouteDecision, body []byte, apiKey string, dst io.Writer) (ClaudeCodeBridgeOpenAILiveStreamResult, error) {
	if !claudeCodeBridgeDeepSeekOpenAICompatibleFallbackExplicitlyEnabled() {
		return ClaudeCodeBridgeOpenAILiveStreamResult{}, fmt.Errorf("claude code DeepSeek OpenAI-compatible fallback live request is not explicitly enabled")
	}
	if !ClaudeCodeBridgeAnthropicLiveConfigured() {
		return ClaudeCodeBridgeOpenAILiveStreamResult{}, fmt.Errorf("claude code DeepSeek OpenAI-compatible fallback live request is not enabled")
	}
	if !ClaudeCodeBridgeAnthropicLiveLabBillingBypassEnabled() {
		return ClaudeCodeBridgeOpenAILiveStreamResult{}, fmt.Errorf("claude code DeepSeek OpenAI-compatible fallback live requires billing/concurrency guard")
	}
	if err := ClaudeCodeBridgeDeepSeekOpenAICompatibleFallbackDecisionValid(decision); err != nil {
		return ClaudeCodeBridgeOpenAILiveStreamResult{}, err
	}
	if !claudeCodeBridgeOpenAIUnsafeLabBaseURLAllowed(decision) {
		return ClaudeCodeBridgeOpenAILiveStreamResult{}, fmt.Errorf("claude code DeepSeek OpenAI-compatible fallback unsafe lab bypass requires loopback upstream; external providers require production billing/concurrency guard")
	}
	if strings.TrimSpace(apiKey) == "" {
		return ClaudeCodeBridgeOpenAILiveStreamResult{}, fmt.Errorf("claude code DeepSeek OpenAI-compatible fallback api key is required")
	}
	if err := validateClaudeCodeBridgeBodyBinding(decision, body); err != nil {
		return ClaudeCodeBridgeOpenAILiveStreamResult{}, err
	}
	if dst == nil {
		return ClaudeCodeBridgeOpenAILiveStreamResult{}, fmt.Errorf("claude code DeepSeek OpenAI-compatible fallback stream writer is required")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if ctx == nil {
		ctx = context.Background()
	}
	chatBody, err := buildClaudeCodeDeepSeekChatCompletionsBody(decision, body)
	if err != nil {
		return ClaudeCodeBridgeOpenAILiveStreamResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, buildClaudeCodeBridgeDeepSeekOpenAICompatibleFallbackURL(decision), bytes.NewReader(chatBody))
	if err != nil {
		return ClaudeCodeBridgeOpenAILiveStreamResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	applyClaudeCodeBridgeOpenAIClientHeaders(req.Header, decision.ClientType)
	resp, err := httpClient.Do(req)
	if err != nil {
		return ClaudeCodeBridgeOpenAILiveStreamResult{}, err
	}
	defer resp.Body.Close()
	header := claudeCodeBridgeCloneHTTPHeader(resp.Header)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		limited, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return ClaudeCodeBridgeOpenAILiveStreamResult{}, fmt.Errorf("claude code DeepSeek OpenAI-compatible fallback upstream status %d: %s", resp.StatusCode, sanitizeClaudeCodeBridgeErrorMessage(string(limited)))
	}
	applyClaudeCodeBridgeLiveResponseHeaders(dst, header)
	fixture, err := copyClaudeCodeChatCompletionsAsAnthropicSSE(dst, resp.Body, decision.ModelID)
	if err != nil {
		return ClaudeCodeBridgeOpenAILiveStreamResult{}, err
	}
	requestAudit := buildClaudeCodeBridgeOpenAIChatFallbackRequestAudit(decision, body, req.URL.String(), true)
	return ClaudeCodeBridgeOpenAILiveStreamResult{
		StatusCode: resp.StatusCode,
		Header:     header,
		Audit:      buildClaudeCodeBridgeAuditSummaryWithRequest(decision, fixture, requestAudit),
	}, nil
}

func buildClaudeCodeBridgeDeepSeekOpenAICompatibleFallbackURL(decision ClaudeCodeBridgeRouteDecision) string {
	return buildOpenAIChatCompletionsURL(decision.OpenAIBaseURL)
}

func claudeCodeBridgeDeepSeekOpenAICompatibleFallbackExplicitlyEnabled() bool {
	return claudeCodeBridgeEnvEnabled("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_OPENAI_FALLBACK_ENABLED")
}

func ClaudeCodeBridgeDeepSeekOpenAICompatibleFallbackDecisionValid(decision ClaudeCodeBridgeRouteDecision) error {
	if strings.TrimSpace(decision.Provider) != "deepseek" || strings.TrimSpace(decision.Route) != "deepseek_bridge" || strings.TrimSpace(decision.ClientType) != "claude_code_bridge_deepseek" {
		return fmt.Errorf("claude code DeepSeek OpenAI-compatible fallback only supports deepseek bridge")
	}
	switch strings.TrimSpace(decision.PreferredProtocol) {
	case "openai_chat_completions", "openai_compatible_chat":
	default:
		return fmt.Errorf("claude code DeepSeek OpenAI-compatible fallback requires chat completions protocol")
	}
	if strings.TrimSpace(decision.FallbackProtocol) != strings.TrimSpace(decision.PreferredProtocol) {
		return fmt.Errorf("claude code DeepSeek OpenAI-compatible fallback requires fixture-backed fallback protocol")
	}
	fallbackReason := strings.TrimSpace(decision.FallbackReason)
	if strings.TrimSpace(decision.OpenAIBaseURL) == "" || !strings.HasPrefix(fallbackReason, "anthropic_") || !strings.HasSuffix(fallbackReason, "_fixture_failed") {
		return fmt.Errorf("claude code DeepSeek OpenAI-compatible fallback requires base url and fixture-backed fallback reason")
	}
	if !decision.SupportsCacheAudit || !decision.SupportsReasoningMapping {
		return fmt.Errorf("claude code DeepSeek OpenAI-compatible fallback requires cache and reasoning fixture capabilities")
	}
	if !decision.CapabilitiesVerified || !decision.SupportsText || !decision.SupportsTools || !decision.SupportsStreaming || !decision.SupportsUsage || !decision.SupportsErrorPassthrough {
		return fmt.Errorf("claude code DeepSeek OpenAI-compatible fallback capabilities are not verified")
	}
	return validateClaudeCodeBridgeDecision(decision)
}

func buildClaudeCodeDeepSeekChatCompletionsBody(decision ClaudeCodeBridgeRouteDecision, body []byte) ([]byte, error) {
	responsesBody, err := buildClaudeCodeOpenAIResponsesBody(decision, body)
	if err != nil {
		return nil, err
	}
	var responsesReq apicompat.ResponsesRequest
	if err := json.Unmarshal(responsesBody, &responsesReq); err != nil {
		return nil, err
	}
	chatReq, err := apicompat.ResponsesToChatCompletionsRequest(&responsesReq)
	if err != nil {
		return nil, err
	}
	applyClaudeCodeDeepSeekChatEffortPolicy(decision, chatReq, body)
	chatReq.Stream = true
	chatReq.StreamOptions = &apicompat.ChatStreamOptions{IncludeUsage: true}
	return json.Marshal(chatReq)
}

func buildClaudeCodeBridgeOpenAIChatFallbackRequestAudit(decision ClaudeCodeBridgeRouteDecision, originalBody []byte, upstreamURL string, fallbackUsed bool) claudeCodeBridgeRequestAudit {
	audit := claudeCodeBridgeRequestAudit{
		SelectedProtocol:      strings.TrimSpace(decision.FallbackProtocol),
		FallbackUsed:          fallbackUsed,
		UpstreamPathKind:      safeClaudeCodeBridgeUpstreamPathKind(upstreamURL),
		CacheControlLocations: claudeCodeBridgeCacheControlLocations(originalBody),
	}
	if audit.SelectedProtocol == "" {
		audit.SelectedProtocol = strings.TrimSpace(decision.PreferredProtocol)
	}
	audit.CacheControlPresent = len(audit.CacheControlLocations) > 0
	audit.ProviderCacheMechanism = "none"
	audit.StablePrefixHMAC, audit.StablePrefixTokenBucket = claudeCodeBridgeStablePrefixAudit(decision, originalBody)
	return audit
}

func applyClaudeCodeDeepSeekChatEffortPolicy(decision ClaudeCodeBridgeRouteDecision, req *apicompat.ChatCompletionsRequest, body []byte) {
	if req == nil || strings.TrimSpace(decision.Provider) != "deepseek" {
		return
	}
	effort := claudeCodeBridgeExplicitAnthropicEffort(body)
	if effort == "" {
		req.ReasoningEffort = ""
		return
	}
	levels := normalizedClaudeCodeBridgeReasoningEffortLevels(decision)
	if _, ok := levels[effort]; !ok {
		req.ReasoningEffort = ""
		return
	}
	req.ReasoningEffort = effort
}

func claudeCodeBridgeExplicitAnthropicEffort(body []byte) string {
	var payload struct {
		OutputConfig *struct {
			Effort string `json:"effort"`
		} `json:"output_config"`
	}
	if len(body) == 0 || json.Unmarshal(body, &payload) != nil || payload.OutputConfig == nil {
		return ""
	}
	normalized := NormalizeClaudeOutputEffort(payload.OutputConfig.Effort)
	if normalized == nil {
		return ""
	}
	return *normalized
}

func copyClaudeCodeChatCompletionsAsAnthropicSSE(dst io.Writer, src io.Reader, model string) (ClaudeCodeBridgeProviderFixture, error) {
	chatState := apicompat.NewChatCompletionsToResponsesStreamState(model)
	anthropicState := apicompat.NewResponsesEventToAnthropicState()
	anthropicState.Model = model
	fixture := ClaudeCodeBridgeProviderFixture{}
	terminalSeen := false
	visibleOutputSeen := false
	flusher, _ := dst.(interface{ Flush() })
	emit := func(events []apicompat.ResponsesStreamEvent) error {
		for _, event := range events {
			if claudeCodeOpenAIResponsesEventIsForeignReasoning(event) {
				continue
			}
			for _, anthropicEvent := range apicompat.ResponsesEventToAnthropicEvents(&event, anthropicState) {
				wire, err := apicompat.ResponsesAnthropicEventToSSE(anthropicEvent)
				if err != nil {
					return err
				}
				if _, err := io.WriteString(dst, wire); err != nil {
					return err
				}
				if flusher != nil {
					flusher.Flush()
				}
			}
		}
		return nil
	}
	handleFrame := func(frame claudeCodeBridgeSSEFrame) error {
		if strings.TrimSpace(frame.Data) == "" || strings.TrimSpace(frame.Data) == "[DONE]" {
			return nil
		}
		var chunk apicompat.ChatCompletionsChunk
		if err := json.Unmarshal([]byte(frame.Data), &chunk); err != nil {
			return err
		}
		if claudeCodeChatCompletionsChunkHasVisibleOutput(chunk) {
			visibleOutputSeen = true
		}
		chunk = sanitizeClaudeCodeDeepSeekChatCompletionsChunk(chunk)
		if chunk.Usage != nil {
			usagePayload, _ := json.Marshal(chunk.Usage)
			collectClaudeCodeBridgeProviderUsageNode(gjson.ParseBytes(usagePayload), &fixture)
		}
		for _, choice := range chunk.Choices {
			if choice.FinishReason != nil && strings.TrimSpace(*choice.FinishReason) != "" {
				terminalSeen = true
			}
		}
		return emit(apicompat.ChatCompletionsChunkToResponsesEvents(&chunk, chatState))
	}
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	parser := claudeCodeBridgeSSEFrameParser{}
	for scanner.Scan() {
		frame, ok := parser.AddLine(strings.TrimRight(scanner.Text(), "\r"))
		if ok {
			if err := handleFrame(frame); err != nil {
				return fixture, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fixture, err
	}
	if frame, ok := parser.Finish(); ok {
		if err := handleFrame(frame); err != nil {
			return fixture, err
		}
	}
	if terminalSeen {
		if !visibleOutputSeen {
			if _, err := dst.Write(buildClaudeCodeBridgeErrorSSE("upstream_stream_closed", "OpenAI-compatible bridge fallback produced no replay-safe visible output")); err != nil {
				return fixture, err
			}
			if flusher != nil {
				flusher.Flush()
			}
			return fixture, nil
		}
		return fixture, emit(apicompat.FinalizeChatCompletionsResponsesStream(chatState))
	}
	if _, err := dst.Write(buildClaudeCodeBridgeErrorSSE("upstream_stream_closed", "OpenAI-compatible bridge fallback stream closed before terminal event")); err != nil {
		return fixture, err
	}
	if flusher != nil {
		flusher.Flush()
	}
	return fixture, nil
}

func claudeCodeChatCompletionsChunkHasVisibleOutput(chunk apicompat.ChatCompletionsChunk) bool {
	for _, choice := range chunk.Choices {
		if choice.Delta.Content != nil && strings.TrimSpace(*choice.Delta.Content) != "" {
			return true
		}
		if len(choice.Delta.ToolCalls) > 0 {
			return true
		}
	}
	return false
}

func sanitizeClaudeCodeDeepSeekChatCompletionsChunk(chunk apicompat.ChatCompletionsChunk) apicompat.ChatCompletionsChunk {
	for i := range chunk.Choices {
		chunk.Choices[i].Delta.ReasoningContent = nil
	}
	return chunk
}
