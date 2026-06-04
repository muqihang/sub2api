package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
)

const (
	codexGatewayDeepSeekDefaultRejectMessage = "DeepSeek request was rejected."
	codexGatewayDeepSeekStreamClosedReason   = "upstream_stream_closed"
)

const (
	codexGatewayDeepSeekToolReplayMaxSystemMessages = 2
	codexGatewayDeepSeekToolReplayMaxContextChars   = 2000
)

func ExecuteCodexGatewayDeepSeekAdapter(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	apiKey string,
	model CodexGatewayModel,
	req CodexGatewayResponsesCreateRequest,
	stateStore *CodexGatewayStateStore,
	reqCtx CodexGatewayDeepSeekRequestContext,
	cfg CodexGatewayDeepSeekRequestConfig,
) (CodexGatewayDeepSeekAdapterResult, error) {
	upstreamModel := strings.TrimSpace(model.UpstreamModel)
	if upstreamModel == "" {
		upstreamModel = strings.TrimSpace(model.Slug)
	}
	req, err := codexGatewayDeepSeekRequestWithHostedVision(ctx, req, stateStore, reqCtx, upstreamModel, cfg)
	if err != nil {
		return CodexGatewayDeepSeekAdapterResult{}, err
	}
	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, stateStore, reqCtx, cfg)
	if err != nil {
		return CodexGatewayDeepSeekAdapterResult{}, err
	}
	resp, body, err := doCodexGatewayDeepSeekChatCompletionsRequest(ctx, client, baseURL, apiKey, prepared.Body, reqCtx)
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
		result.ServiceResponse.Body = codexGatewayDeepSeekMapErrorBody(resp.StatusCode, body)
		return result, nil
	}

	mapped, err := codexGatewayDeepSeekMapChatCompletionResponse(body, resp.Header.Get("x-request-id"), model, prepared.ToolNameMap, prepared.ToolSchemas, prepared.ReplayMessages, stateStore, reqCtx)
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

func doCodexGatewayDeepSeekChatCompletionsRequest(ctx context.Context, client *http.Client, baseURL, apiKey string, body map[string]any, reqCtx CodexGatewayDeepSeekRequestContext) (*http.Response, []byte, error) {
	if client == nil {
		client = http.DefaultClient
	}
	rawBody, err := json.Marshal(body)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal codex deepseek request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, buildOpenAIChatCompletionsURL(baseURL), bytes.NewReader(rawBody))
	if err != nil {
		return nil, nil, fmt.Errorf("build codex deepseek request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	}
	req.Header.Set("Accept", "application/json")
	codexGatewayCaptureUpstreamRequest(reqCtx.CaptureTrace, "deepseek", req.Header, rawBody)

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("send codex deepseek request: %w", err)
	}
	bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if readErr != nil {
		_ = resp.Body.Close()
		return nil, nil, fmt.Errorf("read codex deepseek response: %w", readErr)
	}
	resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	codexGatewayCaptureUpstreamResponse(reqCtx.CaptureTrace, resp.Header, resp.StatusCode, bodyBytes)
	return resp, bodyBytes, nil
}

func codexGatewayDeepSeekMapChatCompletionResponse(
	raw []byte,
	upstreamRequestID string,
	model CodexGatewayModel,
	toolNameMap map[string]CodexGatewayToolNameMapEntry,
	toolSchemas []json.RawMessage,
	replayMessages []json.RawMessage,
	stateStore *CodexGatewayStateStore,
	reqCtx CodexGatewayDeepSeekRequestContext,
) (CodexGatewayProviderResult, error) {
	var parsed apicompat.ChatCompletionsResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return CodexGatewayProviderResult{}, fmt.Errorf("decode codex deepseek response: %w", err)
	}

	responseModel := strings.TrimSpace(model.Slug)
	if responseModel == "" {
		responseModel = strings.TrimSpace(parsed.Model)
	}
	responseID := strings.TrimSpace(parsed.ID)
	usageRaw, usage := codexGatewayDeepSeekUsageJSON(parsed.Usage)
	response := CodexGatewayResponse{
		ID:     responseID,
		Object: "response",
		Model:  responseModel,
		Status: "completed",
		Output: []json.RawMessage{},
		Usage:  usageRaw,
	}
	result := CodexGatewayProviderResult{
		ResponseID:        responseID,
		UpstreamRequestID: strings.TrimSpace(upstreamRequestID),
		UpstreamModel:     strings.TrimSpace(parsed.Model),
		Response:          response,
		Usage:             usage,
	}
	if len(parsed.Choices) == 0 {
		return result, nil
	}

	choice := parsed.Choices[0]
	text, err := augmentGatewayDecodeOpenAIMessageContent(choice.Message.Content)
	if err != nil {
		return CodexGatewayProviderResult{}, err
	}
	reasoningPresent := augmentGatewayResponseChoiceMessageHasField(raw, "reasoning_content")
	result.ReasoningContent = choice.Message.ReasoningContent
	result.ReasoningContentPresent = reasoningPresent

	output := make([]json.RawMessage, 0, 1+len(choice.Message.ToolCalls))
	if strings.TrimSpace(text) != "" {
		item := map[string]any{
			"type":   "message",
			"id":     codexGatewayDeepSeekMessageID(responseID, 0),
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
		output = append(output, rawItem)
	}

	status, incompleteReason := codexGatewayDeepSeekFinishReasonStatus(strings.TrimSpace(choice.FinishReason))
	response.Status = status
	if status == "incomplete" && incompleteReason != "" {
		response.IncompleteDetails = json.RawMessage(fmt.Sprintf(`{"reason":%q}`, incompleteReason))
	}
	storedCalls := make([]CodexGatewayStoredToolCall, 0, len(choice.Message.ToolCalls))
	if response.Status == "completed" {
		for _, toolCall := range choice.Message.ToolCalls {
			item, stored, ok := codexGatewayDeepSeekToolCallOutputItem(toolCall, toolNameMap, nil)
			if !ok {
				response.Status = "incomplete"
				response.IncompleteDetails = json.RawMessage(`{"reason":"malformed_tool_arguments"}`)
				continue
			}
			rawItem, err := json.Marshal(item)
			if err != nil {
				return CodexGatewayProviderResult{}, err
			}
			output = append(output, rawItem)
			storedCalls = append(storedCalls, stored)
		}
	}
	response.Output = output
	result.ToolCalls = storedCalls
	result.Response = response

	if response.Status == "completed" {
		if err := codexGatewayDeepSeekPersistState(stateStore, responseID, parsed.Model, reqCtx, text, true, result.ReasoningContent, reasoningPresent, !reasoningPresent, storedCalls, toolNameMap, toolSchemas, codexGatewayDeepSeekStateReplayMessages(replayMessages, text, true, result.ReasoningContent, reasoningPresent, !reasoningPresent, storedCalls, toolNameMap)); err != nil {
			return CodexGatewayProviderResult{}, err
		}
	}
	return result, nil
}

func codexGatewayDeepSeekUsageJSON(usage *apicompat.ChatUsage) (json.RawMessage, CodexGatewayProviderUsage) {
	if usage == nil {
		return nil, CodexGatewayProviderUsage{}
	}
	out := CodexGatewayProviderUsage{
		InputTokens:  usage.PromptTokens,
		OutputTokens: usage.CompletionTokens,
		TotalTokens:  usage.TotalTokens,
	}
	usageBody := map[string]any{
		"input_tokens":  usage.PromptTokens,
		"output_tokens": usage.CompletionTokens,
		"total_tokens":  usage.TotalTokens,
	}
	if usage.PromptTokensDetails != nil {
		out.CacheReadInputTokens = usage.PromptTokensDetails.CachedTokens
	}
	if out.CacheReadInputTokens == 0 && usage.PromptCacheHitTokens > 0 {
		out.CacheReadInputTokens = usage.PromptCacheHitTokens
	}
	if out.CacheReadInputTokens > 0 {
		usageBody["input_tokens_details"] = map[string]any{
			"cached_tokens": out.CacheReadInputTokens,
		}
	}
	if usage.PromptCacheHitTokens > 0 || usage.PromptCacheMissTokens > 0 {
		out.ProviderUsageExtra = map[string]any{
			"prompt_cache_hit_tokens":  float64(usage.PromptCacheHitTokens),
			"prompt_cache_miss_tokens": float64(usage.PromptCacheMissTokens),
		}
	}
	raw, _ := json.Marshal(usageBody)
	return raw, out
}

func codexGatewayDeepSeekToolCallOutputItem(toolCall apicompat.ChatToolCall, toolNameMap map[string]CodexGatewayToolNameMapEntry, _ map[string]struct{}) (map[string]any, CodexGatewayStoredToolCall, bool) {
	callID := strings.TrimSpace(toolCall.ID)
	alias := strings.TrimSpace(toolCall.Function.Name)
	if callID == "" || alias == "" {
		return nil, CodexGatewayStoredToolCall{}, false
	}
	entry, ok := toolNameMap[alias]
	if !ok {
		entry = CodexGatewayToolNameMapEntry{
			Alias: alias,
			Kind:  CodexGatewayToolKindFunction,
			Name:  alias,
		}
	}
	arguments, ok, _ := codexGatewayPrepareDeepSeekToolArguments(entry, toolCall.Function.Arguments)
	if !ok {
		return nil, CodexGatewayStoredToolCall{}, false
	}
	stored := CodexGatewayStoredToolCall{
		ID:        callID,
		Type:      entry.Kind,
		Alias:     alias,
		Name:      codexGatewayClientVisibleToolName(entry),
		Arguments: arguments,
	}

	item := map[string]any{
		"id":      codexGatewayDeepSeekToolItemID(callID),
		"call_id": callID,
		"name":    stored.Name,
		"status":  "completed",
	}
	itemType := codexGatewayDeepSeekClientVisibleToolItemType(entry)
	switch itemType {
	case CodexGatewayOutputItemTypeCustomToolCall:
		item["type"] = CodexGatewayOutputItemTypeCustomToolCall
		item["input"] = codexGatewayDeepSeekCustomToolInput(arguments, entry)
	case CodexGatewayOutputItemTypeLocalShellCall:
		codexGatewayApplyLocalShellCallItemFields(item, callID, "completed", arguments)
	case CodexGatewayOutputItemTypeToolSearchCall:
		item = codexGatewayDeepSeekToolSearchCallItem(callID, "completed", arguments)
	default:
		item["type"] = itemType
		if namespace := strings.TrimSpace(entry.Namespace); namespace != "" {
			item["namespace"] = namespace
		}
		item["arguments"] = arguments
	}
	return item, stored, true
}

func codexGatewayDeepSeekClientVisibleToolItemType(entry CodexGatewayToolNameMapEntry) string {
	if codexGatewayIsToolSearchEntry(entry) {
		return CodexGatewayOutputItemTypeToolSearchCall
	}
	return codexGatewayClientVisibleToolItemType(entry)
}

func codexGatewayDeepSeekToolSearchCallItem(callID, status, arguments string) map[string]any {
	return map[string]any{
		"type":      CodexGatewayOutputItemTypeToolSearchCall,
		"call_id":   strings.TrimSpace(callID),
		"status":    strings.TrimSpace(status),
		"execution": "client",
		"arguments": codexGatewayDeepSeekToolSearchArgumentsValue(arguments),
	}
}

func codexGatewayDeepSeekToolSearchArgumentsValue(arguments string) any {
	arguments = strings.TrimSpace(arguments)
	if arguments == "" {
		return map[string]any{}
	}
	var parsed any
	if err := json.Unmarshal([]byte(arguments), &parsed); err != nil || parsed == nil {
		return map[string]any{}
	}
	return parsed
}

func codexGatewayDeepSeekCustomToolInput(arguments string, entry CodexGatewayToolNameMapEntry) string {
	raw := strings.TrimSpace(arguments)
	if raw == "" {
		return arguments
	}
	raw = codexGatewayNormalizeLiteralNewlinesInJSONStrings(raw)

	var object map[string]any
	if err := json.Unmarshal([]byte(raw), &object); err != nil {
		return arguments
	}

	for _, key := range []string{
		entry.Alias,
		entry.Name,
		codexGatewayOriginalToolPath(entry),
		"input",
		"patch",
		"content",
		"text",
	} {
		if value, ok := codexGatewayDeepSeekCustomToolStringField(object, key); ok {
			return value
		}
	}
	if len(object) == 1 {
		for _, value := range object {
			if s, ok := value.(string); ok {
				return s
			}
		}
	}
	return arguments
}

func codexGatewayDeepSeekCustomToolStreamInput(arguments string, entry CodexGatewayToolNameMapEntry) (string, bool) {
	raw := strings.TrimSpace(arguments)
	if raw == "" {
		return arguments, true
	}
	if !strings.HasPrefix(raw, "{") {
		return arguments, true
	}
	raw = codexGatewayNormalizeLiteralNewlinesInJSONStrings(raw)
	var object map[string]any
	if err := json.Unmarshal([]byte(raw), &object); err != nil {
		return "", false
	}
	return codexGatewayDeepSeekCustomToolInput(arguments, entry), true
}

func codexGatewayNormalizeLiteralNewlinesInJSONStrings(raw string) string {
	if raw == "" {
		return raw
	}
	var out strings.Builder
	out.Grow(len(raw))
	inString := false
	escape := false
	for _, r := range raw {
		if inString {
			if escape {
				out.WriteRune(r)
				escape = false
				continue
			}
			switch r {
			case '\\':
				out.WriteRune(r)
				escape = true
				continue
			case '"':
				out.WriteRune(r)
				inString = false
				continue
			case '\n':
				out.WriteString(`\n`)
				continue
			case '\r':
				out.WriteString(`\r`)
				continue
			}
			out.WriteRune(r)
			continue
		}
		if r == '"' {
			inString = true
		}
		out.WriteRune(r)
	}
	return out.String()
}

func codexGatewayDeepSeekCustomToolStringField(object map[string]any, key string) (string, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", false
	}
	value, ok := object[key]
	if !ok {
		return "", false
	}
	s, ok := value.(string)
	return s, ok
}

func codexGatewayDeepSeekPersistState(
	stateStore *CodexGatewayStateStore,
	responseID string,
	upstreamModel string,
	reqCtx CodexGatewayDeepSeekRequestContext,
	assistantContent string,
	assistantContentPresent bool,
	reasoningContent string,
	reasoningContentPresent bool,
	reasoningContentSynthesized bool,
	toolCalls []CodexGatewayStoredToolCall,
	toolNameMap map[string]CodexGatewayToolNameMapEntry,
	toolSchemas []json.RawMessage,
	replayMessages []json.RawMessage,
) error {
	shouldPersist := codexGatewayDeepSeekShouldPersistResponseState(assistantContent, assistantContentPresent, reasoningContent, reasoningContentPresent, reasoningContentSynthesized, toolCalls, replayMessages)
	if stateStore == nil || strings.TrimSpace(responseID) == "" || !shouldPersist {
		return nil
	}
	if len(replayMessages) == 0 {
		replayMessages = codexGatewayDeepSeekStateReplayMessages(nil, assistantContent, assistantContentPresent, reasoningContent, reasoningContentPresent, reasoningContentSynthesized, toolCalls, toolNameMap)
	}
	return stateStore.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    strings.TrimSpace(responseID),
			SessionKey:    strings.TrimSpace(reqCtx.SessionKey),
			IsolationKey:  strings.TrimSpace(reqCtx.IsolationKey),
			Provider:      "deepseek",
			UpstreamModel: strings.TrimSpace(upstreamModel),
		},
		AssistantContent:            assistantContent,
		AssistantContentPresent:     assistantContentPresent,
		ReasoningContent:            reasoningContent,
		ReasoningContentPresent:     reasoningContentPresent,
		ReasoningContentSynthesized: reasoningContentSynthesized,
		ToolCalls:                   cloneCodexGatewayStoredToolCalls(toolCalls),
		ToolNameMap:                 cloneCodexGatewayToolNameMap(toolNameMap),
		ToolSchemas:                 cloneCodexGatewayRawMessages(toolSchemas),
		ReplayMessages:              cloneCodexGatewayRawMessages(replayMessages),
	})
}

func codexGatewayDeepSeekShouldPersistResponseState(assistantContent string, assistantContentPresent bool, reasoningContent string, reasoningContentPresent bool, reasoningContentSynthesized bool, toolCalls []CodexGatewayStoredToolCall, replayMessages []json.RawMessage) bool {
	if len(toolCalls) > 0 || len(replayMessages) > 0 {
		return true
	}
	if assistantContentPresent && strings.TrimSpace(assistantContent) != "" {
		return true
	}
	if (reasoningContentPresent || reasoningContentSynthesized) && strings.TrimSpace(reasoningContent) != "" {
		return true
	}
	return false
}

func codexGatewayDeepSeekStateReplayMessages(base []json.RawMessage, assistantContent string, assistantContentPresent bool, reasoningContent string, reasoningContentPresent bool, reasoningContentSynthesized bool, toolCalls []CodexGatewayStoredToolCall, toolNameMap map[string]CodexGatewayToolNameMapEntry) []json.RawMessage {
	state := CodexGatewayResponseState{
		AssistantContent:            assistantContent,
		AssistantContentPresent:     assistantContentPresent,
		ReasoningContent:            reasoningContent,
		ReasoningContentPresent:     reasoningContentPresent,
		ReasoningContentSynthesized: reasoningContentSynthesized,
		ToolCalls:                   toolCalls,
		ToolNameMap:                 toolNameMap,
	}
	assistant := codexGatewayDeepSeekAssistantMessageFromState(state)
	raw, err := json.Marshal(assistant)
	if err != nil || len(raw) == 0 {
		return cloneCodexGatewayRawMessages(base)
	}
	if len(toolCalls) > 0 {
		out := codexGatewayDeepSeekToolReplayContextMessages(base)
		out = append(out, raw)
		return out
	}
	out := cloneCodexGatewayRawMessages(base)
	out = append(out, raw)
	return out
}

func codexGatewayDeepSeekToolReplayContextMessages(base []json.RawMessage) []json.RawMessage {
	if len(base) == 0 {
		return nil
	}
	out := make([]json.RawMessage, 0, codexGatewayDeepSeekToolReplayMaxSystemMessages+1)
	systemCount := 0
	var lastUser json.RawMessage
	for _, raw := range base {
		var msg map[string]any
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		role := strings.TrimSpace(firstCodexGatewayToolString(msg["role"]))
		switch role {
		case "system":
			if systemCount >= codexGatewayDeepSeekToolReplayMaxSystemMessages {
				continue
			}
			if clipped, ok := codexGatewayDeepSeekClipReplayContextMessage(msg); ok {
				out = append(out, clipped)
				systemCount++
			}
		case "user":
			if clipped, ok := codexGatewayDeepSeekClipReplayContextMessage(msg); ok {
				lastUser = clipped
			}
		}
	}
	if len(lastUser) > 0 {
		out = append(out, lastUser)
	}
	return out
}

func codexGatewayDeepSeekClipReplayContextMessage(msg map[string]any) (json.RawMessage, bool) {
	role := strings.TrimSpace(firstCodexGatewayToolString(msg["role"]))
	if role != "system" && role != "user" {
		return nil, false
	}
	content := strings.TrimSpace(firstCodexGatewayToolString(msg["content"]))
	if content == "" {
		return nil, false
	}
	content = codexGatewayDeepSeekTruncateString(content, codexGatewayDeepSeekToolReplayMaxContextChars)
	out, err := json.Marshal(map[string]any{
		"role":    role,
		"content": content,
	})
	if err != nil || len(out) == 0 {
		return nil, false
	}
	return out, true
}

func codexGatewayDeepSeekMapErrorBody(statusCode int, raw []byte) []byte {
	errorType := CodexGatewayErrorTypeAPI
	errorCode := "upstream_error"
	message := "DeepSeek request failed."
	if statusCode >= 400 && statusCode < 500 {
		errorType = CodexGatewayErrorTypeInvalidRequest
		errorCode = CodexGatewayErrorCodeInvalidRequest
		message = codexGatewayDeepSeekDefaultRejectMessage
	}
	var payload struct {
		Error struct {
			Type    string `json:"type"`
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &payload); err == nil {
		if strings.TrimSpace(payload.Error.Type) != "" {
			errorType = strings.TrimSpace(payload.Error.Type)
		}
		if strings.TrimSpace(payload.Error.Code) != "" {
			errorCode = strings.TrimSpace(payload.Error.Code)
		}
		if msg := strings.TrimSpace(payload.Error.Message); msg != "" && !strings.Contains(msg, "chat.completion.chunk") {
			message = msg
		}
	}
	body, err := MarshalCodexGatewayErrorJSON(errorType, errorCode, message)
	if err != nil {
		return []byte(`{"error":{"type":"invalid_request_error","code":"invalid_request","message":"failed to encode error response"}}`)
	}
	return body
}

func codexGatewayDeepSeekMessageID(responseID string, index int) string {
	responseID = strings.TrimSpace(responseID)
	if responseID == "" {
		responseID = "response"
	}
	return fmt.Sprintf("msg_%s_%d", responseID, index)
}

func codexGatewayDeepSeekToolItemID(callID string) string {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		callID = "tool"
	}
	return "fc_" + callID
}

func cloneCodexGatewayHTTPHeader(in http.Header) http.Header {
	if in == nil {
		return make(http.Header)
	}
	out := make(http.Header, len(in))
	for key, values := range in {
		copied := make([]string, len(values))
		copy(copied, values)
		out[key] = copied
	}
	return out
}
