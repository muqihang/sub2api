package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
)

const codexGatewayAnthropicStreamClosedReason = "upstream_stream_closed"

func ExecuteCodexGatewayAnthropicAdapter(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	apiKey string,
	model CodexGatewayModel,
	req CodexGatewayResponsesCreateRequest,
	stateStore *CodexGatewayStateStore,
	reqCtx CodexGatewayAnthropicRequestContext,
	cfg CodexGatewayAnthropicRequestConfig,
) (CodexGatewayDeepSeekAdapterResult, error) {
	prepared, err := BuildCodexGatewayAnthropicRequest(model, req, stateStore, reqCtx, cfg)
	if err != nil {
		return CodexGatewayDeepSeekAdapterResult{}, err
	}
	body := cloneCodexGatewayStreamBody(prepared.Body)
	body["stream"] = false
	resp, bodyBytes, err := doCodexGatewayAnthropicMessagesRequest(ctx, client, baseURL, apiKey, body, reqCtx)
	if err != nil {
		return CodexGatewayDeepSeekAdapterResult{}, err
	}
	defer resp.Body.Close()

	result := CodexGatewayDeepSeekAdapterResult{
		ServiceResponse: CodexGatewayServiceResponse{
			StatusCode: resp.StatusCode,
			Headers:    cloneCodexGatewayHTTPHeader(resp.Header),
		},
	}
	if resp.StatusCode >= 400 {
		result.ServiceResponse.Body = codexGatewayAnthropicMapErrorBody(resp.StatusCode, bodyBytes)
		if codexGatewayAnthropicShouldFailoverUpstreamResponse(resp.StatusCode, resp.Header, bodyBytes) {
			return CodexGatewayDeepSeekAdapterResult{}, &UpstreamFailoverError{
				StatusCode:      resp.StatusCode,
				ResponseBody:    result.ServiceResponse.Body,
				ResponseHeaders: resp.Header.Clone(),
			}
		}
		return result, nil
	}
	mapped, err := codexGatewayAnthropicMapMessageResponse(bodyBytes, resp.Header.Get("x-request-id"), model, prepared.ToolNameMap, prepared.ReplayMessages, stateStore, reqCtx)
	if err != nil {
		return CodexGatewayDeepSeekAdapterResult{}, err
	}
	result.ProviderResult = mapped
	result.ServiceResponse.StatusCode = http.StatusOK
	result.ServiceResponse.Headers.Set("Content-Type", "application/json")
	result.ServiceResponse.Body, err = json.Marshal(mapped.Response)
	if err != nil {
		return CodexGatewayDeepSeekAdapterResult{}, err
	}
	return result, nil
}

func doCodexGatewayAnthropicMessagesRequest(ctx context.Context, client *http.Client, baseURL, apiKey string, body map[string]any, reqCtx CodexGatewayAnthropicRequestContext) (*http.Response, []byte, error) {
	if client == nil {
		client = http.DefaultClient
	}
	rawBody, err := json.Marshal(body)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal codex anthropic request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, buildAnthropicMessagesURL(baseURL), bytes.NewReader(rawBody))
	if err != nil {
		return nil, nil, fmt.Errorf("build codex anthropic request: %w", err)
	}
	setCodexGatewayAnthropicHeaders(httpReq, apiKey, false)
	setCodexGatewayAnthropicWebSearchBetaHeader(httpReq, rawBody)
	codexGatewayCaptureUpstreamRequest(reqCtx.CaptureTrace, "anthropic", httpReq.Header, rawBody)
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, nil, fmt.Errorf("send codex anthropic request: %w", err)
	}
	bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if readErr != nil {
		_ = resp.Body.Close()
		return nil, nil, fmt.Errorf("read codex anthropic response: %w", readErr)
	}
	resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	codexGatewayCaptureUpstreamResponse(reqCtx.CaptureTrace, resp.Header, resp.StatusCode, bodyBytes)
	return resp, bodyBytes, nil
}

func setCodexGatewayAnthropicHeaders(req *http.Request, apiKey string, stream bool) {
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	req.Header.Set("Anthropic-Version", "2023-06-01")
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("x-api-key", strings.TrimSpace(apiKey))
	}
}

func setCodexGatewayAnthropicWebSearchBetaHeader(req *http.Request, body []byte) {
	if req == nil || !bytes.Contains(body, []byte(`"web_search_20250305"`)) {
		return
	}
	req.Header.Set("Anthropic-Beta", "web-search-2025-03-05")
}

func buildAnthropicMessagesURL(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = "https://api.anthropic.com"
	}
	if strings.HasSuffix(base, "/v1/messages") {
		return base
	}
	if strings.HasSuffix(base, "/messages") {
		return base
	}
	if strings.HasSuffix(base, "/v1") {
		return base + "/messages"
	}
	return base + "/v1/messages"
}

func codexGatewayAnthropicMapMessageResponse(raw []byte, upstreamRequestID string, model CodexGatewayModel, toolNameMap map[string]CodexGatewayToolNameMapEntry, replayMessages []json.RawMessage, stateStore *CodexGatewayStateStore, reqCtx CodexGatewayAnthropicRequestContext) (CodexGatewayProviderResult, error) {
	responseID := strings.TrimSpace(gjson.GetBytes(raw, "id").String())
	upstreamModel := strings.TrimSpace(gjson.GetBytes(raw, "model").String())
	usageRaw, usage := codexGatewayAnthropicUsageJSONFromBytes(raw)
	response := CodexGatewayResponse{
		ID:     responseID,
		Object: "response",
		Model:  codexGatewayAnthropicResponseModel(model, upstreamModel),
		Status: codexGatewayAnthropicFinishStatus(gjson.GetBytes(raw, "stop_reason").String()),
		Output: []json.RawMessage{},
		Usage:  usageRaw,
	}
	result := CodexGatewayProviderResult{
		ResponseID:        responseID,
		UpstreamRequestID: strings.TrimSpace(upstreamRequestID),
		UpstreamModel:     upstreamModel,
		Response:          response,
		Usage:             usage,
	}
	contents := gjson.GetBytes(raw, "content").Array()
	var textParts []string
	var stored []CodexGatewayStoredToolCall
	var thinkingBlocks []json.RawMessage
	for _, block := range contents {
		switch block.Get("type").String() {
		case "text":
			if text := block.Get("text").String(); text != "" {
				textParts = append(textParts, text)
			}
		case "thinking":
			result.ReasoningContentPresent = true
			result.ReasoningContent += block.Get("thinking").String()
			thinkingBlocks = append(thinkingBlocks, json.RawMessage(block.Raw))
		case "redacted_thinking":
			thinkingBlocks = append(thinkingBlocks, json.RawMessage(block.Raw))
		case "tool_use":
			item, call, ok := codexGatewayAnthropicToolUseOutputItem(block.Raw, toolNameMap)
			if !ok {
				continue
			}
			rawItem, err := json.Marshal(item)
			if err != nil {
				return CodexGatewayProviderResult{}, err
			}
			response.Output = append(response.Output, rawItem)
			stored = append(stored, call)
		}
	}
	if text := strings.Join(textParts, "\n"); strings.TrimSpace(text) != "" {
		item := map[string]any{
			"type":   "message",
			"id":     codexGatewayAnthropicMessageID(responseID, 0),
			"role":   "assistant",
			"status": "completed",
			"content": []map[string]any{{
				"type": "output_text",
				"text": text,
			}},
		}
		rawItem, err := json.Marshal(item)
		if err != nil {
			return CodexGatewayProviderResult{}, err
		}
		response.Output = append([]json.RawMessage{rawItem}, response.Output...)
	}
	if response.Status == "incomplete" {
		response.IncompleteDetails = json.RawMessage(`{"reason":"max_output_tokens"}`)
	}
	result.ToolCalls = stored
	result.Response = response
	if response.Status == "completed" && len(stored) > 0 {
		if err := codexGatewayAnthropicPersistState(stateStore, responseID, codexGatewayAnthropicStateModelKey(model, upstreamModel), reqCtx, strings.Join(textParts, "\n"), len(textParts) > 0, result.ReasoningContent, result.ReasoningContentPresent, thinkingBlocks, stored, toolNameMap, replayMessages); err != nil {
			return CodexGatewayProviderResult{}, err
		}
	}
	return result, nil
}

func codexGatewayAnthropicUsageJSONFromBytes(raw []byte) (json.RawMessage, CodexGatewayProviderUsage) {
	usage := gjson.GetBytes(raw, "usage")
	return codexGatewayAnthropicUsageJSONFromResult(usage)
}

func codexGatewayAnthropicUsageJSONFromResult(usage gjson.Result) (json.RawMessage, CodexGatewayProviderUsage) {
	if !usage.Exists() {
		return nil, CodexGatewayProviderUsage{}
	}
	rawInput := int(usage.Get("input_tokens").Int())
	cacheRead := int(firstNonZeroInt64(usage.Get("cache_read_input_tokens").Int(), usage.Get("cached_tokens").Int()))
	cacheCreation := int(usage.Get("cache_creation_input_tokens").Int())
	cacheCreation5m := int(usage.Get("cache_creation.ephemeral_5m_input_tokens").Int())
	cacheCreation1h := int(usage.Get("cache_creation.ephemeral_1h_input_tokens").Int())
	if cacheCreation == 0 {
		cacheCreation = cacheCreation5m + cacheCreation1h
	}
	out := CodexGatewayProviderUsage{
		InputTokens:              rawInput + cacheRead,
		OutputTokens:             int(usage.Get("output_tokens").Int()),
		CacheCreationInputTokens: cacheCreation,
		CacheReadInputTokens:     cacheRead,
		CacheCreation5mTokens:    cacheCreation5m,
		CacheCreation1hTokens:    cacheCreation1h,
	}
	out.TotalTokens = out.InputTokens + out.OutputTokens + out.CacheCreationInputTokens
	raw := codexGatewayAnthropicUsageJSON(out)
	if cacheCreation > 0 || out.CacheReadInputTokens > 0 {
		out.ProviderUsageExtra = map[string]any{
			"anthropic_input_tokens":         float64(rawInput),
			"cache_creation_input_tokens":    float64(cacheCreation),
			"cache_read_input_tokens":        float64(out.CacheReadInputTokens),
			"cache_creation_5m_input_tokens": float64(cacheCreation5m),
			"cache_creation_1h_input_tokens": float64(cacheCreation1h),
		}
	}
	return raw, out
}

func codexGatewayAnthropicUsageJSON(usage CodexGatewayProviderUsage) json.RawMessage {
	displayInputTokens := usage.InputTokens + usage.CacheCreationInputTokens
	body := map[string]any{
		"input_tokens":  displayInputTokens,
		"output_tokens": usage.OutputTokens,
		"total_tokens":  displayInputTokens + usage.OutputTokens,
	}
	if usage.CacheReadInputTokens > 0 {
		body["input_tokens_details"] = map[string]any{"cached_tokens": usage.CacheReadInputTokens}
	}
	if usage.CacheCreationInputTokens > 0 {
		body["cache_creation_input_tokens"] = usage.CacheCreationInputTokens
		if details, ok := body["input_tokens_details"].(map[string]any); ok {
			details["cache_creation_tokens"] = usage.CacheCreationInputTokens
		} else {
			body["input_tokens_details"] = map[string]any{"cache_creation_tokens": usage.CacheCreationInputTokens}
		}
	}
	raw, _ := json.Marshal(body)
	return raw
}

func firstNonZeroInt64(values ...int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func codexGatewayAnthropicMapErrorBody(status int, raw []byte) []byte {
	message := strings.TrimSpace(gjson.GetBytes(raw, "error.message").String())
	if message == "" {
		if codexGatewayAnthropicLooksLikeHTMLError(raw) {
			message = codexGatewayAnthropicHTMLStatusMessage(status, raw)
		} else {
			message = strings.TrimSpace(string(raw))
		}
	}
	if message == "" {
		message = http.StatusText(status)
	}
	errType := strings.TrimSpace(gjson.GetBytes(raw, "error.type").String())
	if errType == "" {
		errType = CodexGatewayErrorTypeAPI
	}
	errorCode := "upstream_error"
	if codexGatewayAnthropicIsGatewayTimeoutStatus(status) {
		errorCode = "upstream_timeout"
	}
	body, _ := MarshalCodexGatewayErrorJSON(errType, errorCode, message)
	return body
}

func codexGatewayAnthropicShouldFailoverUpstreamResponse(status int, header http.Header, body []byte) bool {
	if codexGatewayAnthropicIsGatewayTimeoutStatus(status) {
		return true
	}
	switch status {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, 529:
		return true
	default:
		return strings.Contains(strings.ToLower(strings.TrimSpace(header.Get("Content-Type"))), "text/html") && status >= 500 && codexGatewayAnthropicLooksLikeHTMLError(body)
	}
}

func codexGatewayAnthropicIsGatewayTimeoutStatus(status int) bool {
	switch status {
	case 520, 522, 524:
		return true
	default:
		return false
	}
}

func codexGatewayAnthropicLooksLikeHTMLError(raw []byte) bool {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	return strings.HasPrefix(lower, "<!doctype html") || strings.HasPrefix(lower, "<html") || strings.Contains(lower, "<title>")
}

func codexGatewayAnthropicHTMLStatusMessage(status int, raw []byte) string {
	if status == 524 {
		return "Anthropic upstream returned Cloudflare 524 timeout."
	}
	if status == 522 {
		return "Anthropic upstream returned Cloudflare 522 connection timeout."
	}
	if status == 520 {
		return "Anthropic upstream returned Cloudflare 520 origin error."
	}
	return fmt.Sprintf("Anthropic upstream returned HTTP %d HTML error page.", status)
}

func codexGatewayAnthropicToolUseOutputItem(raw string, toolNameMap map[string]CodexGatewayToolNameMapEntry) (map[string]any, CodexGatewayStoredToolCall, bool) {
	block := gjson.Parse(raw)
	callID := strings.TrimSpace(block.Get("id").String())
	alias := strings.TrimSpace(block.Get("name").String())
	if callID == "" || alias == "" {
		return nil, CodexGatewayStoredToolCall{}, false
	}
	entry, ok := toolNameMap[alias]
	if !ok {
		entry = CodexGatewayToolNameMapEntry{Alias: alias, Kind: CodexGatewayToolKindFunction, Name: alias}
	}
	args := block.Get("input").Raw
	if strings.TrimSpace(args) == "" {
		args = "{}"
	}
	stored := CodexGatewayStoredToolCall{
		ID:        callID,
		Type:      entry.Kind,
		Alias:     alias,
		Name:      codexGatewayClientVisibleToolName(entry),
		Arguments: args,
	}
	if codexGatewayIsToolSearchEntry(entry) {
		stored.Name = codexGatewayToolSearchType
	}
	item := map[string]any{
		"id":      codexGatewayAnthropicToolItemID(callID),
		"call_id": callID,
		"name":    stored.Name,
		"status":  "completed",
	}
	switch codexGatewayAnthropicClientVisibleToolItemType(entry) {
	case CodexGatewayOutputItemTypeCustomToolCall:
		item["type"] = CodexGatewayOutputItemTypeCustomToolCall
		item["input"] = codexGatewayDeepSeekCustomToolInput(args, entry)
	case CodexGatewayOutputItemTypeLocalShellCall:
		codexGatewayApplyLocalShellCallItemFields(item, callID, "completed", args)
	case CodexGatewayOutputItemTypeToolSearchCall:
		item = codexGatewayAnthropicToolSearchCallItem(callID, "completed", args)
	default:
		item["type"] = codexGatewayAnthropicClientVisibleToolItemType(entry)
		if namespace := strings.TrimSpace(entry.Namespace); namespace != "" {
			item["namespace"] = namespace
		}
		item["arguments"] = args
	}
	return item, stored, true
}

func codexGatewayAnthropicPersistState(stateStore *CodexGatewayStateStore, responseID, upstreamModel string, reqCtx CodexGatewayAnthropicRequestContext, assistantText string, assistantPresent bool, reasoningText string, reasoningPresent bool, thinkingBlocks []json.RawMessage, calls []CodexGatewayStoredToolCall, toolNameMap map[string]CodexGatewayToolNameMapEntry, replayMessages []json.RawMessage) error {
	if stateStore == nil {
		return nil
	}
	state := CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    strings.TrimSpace(responseID),
			SessionKey:    strings.TrimSpace(reqCtx.SessionKey),
			IsolationKey:  strings.TrimSpace(reqCtx.IsolationKey),
			Provider:      "anthropic",
			UpstreamModel: strings.TrimSpace(upstreamModel),
		},
		AssistantContent:        assistantText,
		AssistantContentPresent: assistantPresent,
		ReasoningContent:        reasoningText,
		ReasoningContentPresent: reasoningPresent,
		AnthropicThinkingBlocks: cloneCodexGatewayRawMessages(thinkingBlocks),
		ToolCalls:               calls,
		ToolNameMap:             toolNameMap,
	}
	if len(calls) > 0 {
		state.ReplayMessages = codexGatewayAnthropicStateReplayMessages(replayMessages, state)
	}
	return stateStore.Put(CodexGatewayResponseState{
		Key:                         state.Key,
		AssistantContent:            state.AssistantContent,
		AssistantContentPresent:     state.AssistantContentPresent,
		ReasoningContent:            state.ReasoningContent,
		ReasoningContentPresent:     state.ReasoningContentPresent,
		ReasoningContentSynthesized: state.ReasoningContentSynthesized,
		AnthropicThinkingBlocks:     state.AnthropicThinkingBlocks,
		ToolCalls:                   state.ToolCalls,
		ToolNameMap:                 state.ToolNameMap,
		ReplayMessages:              state.ReplayMessages,
	})
}

func codexGatewayAnthropicStateReplayMessages(base []json.RawMessage, state CodexGatewayResponseState) []json.RawMessage {
	assistant := codexGatewayAnthropicAssistantMessageFromState(state)
	raw, err := json.Marshal(assistant)
	if err == nil && len(raw) > 0 {
		return []json.RawMessage{raw}
	}
	return nil
}

func codexGatewayAnthropicFinishStatus(reason string) string {
	status, _ := codexGatewayAnthropicFinishReasonStatus(reason)
	return status
}

func codexGatewayAnthropicFinishReasonStatus(reason string) (string, string) {
	switch strings.TrimSpace(reason) {
	case "", "end_turn", "tool_use", "stop_sequence":
		return "completed", ""
	case "max_tokens":
		return "incomplete", "max_output_tokens"
	default:
		return "incomplete", strings.TrimSpace(reason)
	}
}

func codexGatewayAnthropicResponseModel(model CodexGatewayModel, upstreamModel string) string {
	if strings.TrimSpace(model.Slug) != "" {
		return strings.TrimSpace(model.Slug)
	}
	return strings.TrimSpace(upstreamModel)
}

func codexGatewayAnthropicMessageID(responseID string, index int) string {
	responseID = strings.TrimSpace(responseID)
	if responseID == "" {
		responseID = "response"
	}
	return fmt.Sprintf("msg_%s_%d", responseID, index)
}

func codexGatewayAnthropicToolItemID(callID string) string {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		callID = "tool"
	}
	return "fc_" + callID
}
