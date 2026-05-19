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
	req, err := codexGatewayDeepSeekRequestWithHostedVision(ctx, req, cfg)
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

	mapped, err := codexGatewayDeepSeekMapChatCompletionResponse(body, resp.Header.Get("x-request-id"), model, prepared.ToolNameMap, stateStore, reqCtx)
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
			item, stored, ok := codexGatewayDeepSeekToolCallOutputItem(toolCall, toolNameMap)
			if !ok {
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

	if response.Status == "completed" && len(storedCalls) > 0 {
		if err := codexGatewayDeepSeekPersistState(stateStore, responseID, parsed.Model, reqCtx, text, true, result.ReasoningContent, reasoningPresent, !reasoningPresent, storedCalls, toolNameMap); err != nil {
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

func codexGatewayDeepSeekToolCallOutputItem(toolCall apicompat.ChatToolCall, toolNameMap map[string]CodexGatewayToolNameMapEntry) (map[string]any, CodexGatewayStoredToolCall, bool) {
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
	stored := CodexGatewayStoredToolCall{
		ID:        callID,
		Type:      entry.Kind,
		Alias:     alias,
		Name:      codexGatewayClientVisibleToolName(entry),
		Arguments: toolCall.Function.Arguments,
	}

	item := map[string]any{
		"id":      codexGatewayDeepSeekToolItemID(callID),
		"call_id": callID,
		"name":    stored.Name,
		"status":  "completed",
	}
	if entry.Kind == CodexGatewayToolKindCustom {
		item["type"] = "custom_tool_call"
		item["input"] = codexGatewayDeepSeekCustomToolInput(toolCall.Function.Arguments, entry)
	} else {
		item["type"] = "function_call"
		if namespace := strings.TrimSpace(entry.Namespace); namespace != "" {
			item["namespace"] = namespace
		}
		item["arguments"] = toolCall.Function.Arguments
	}
	return item, stored, true
}

func codexGatewayDeepSeekCustomToolInput(arguments string, entry CodexGatewayToolNameMapEntry) string {
	raw := strings.TrimSpace(arguments)
	if raw == "" {
		return arguments
	}

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
	var object map[string]any
	if err := json.Unmarshal([]byte(raw), &object); err != nil {
		return "", false
	}
	return codexGatewayDeepSeekCustomToolInput(arguments, entry), true
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
) error {
	if stateStore == nil || strings.TrimSpace(responseID) == "" || len(toolCalls) == 0 {
		return nil
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
	})
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
