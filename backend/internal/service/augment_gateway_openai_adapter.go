package service

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/tidwall/sjson"
)

type augmentGatewayOpenAIAdapter struct {
	provider AugmentGatewayProvider
	gateway  *OpenAIGatewayService
}

func (a *augmentGatewayOpenAIAdapter) Complete(ctx context.Context, req AugmentGatewayProviderRequest) (AugmentGatewayProviderResult, error) {
	provider := a.provider
	if provider == "" {
		provider = AugmentGatewayProviderOpenAI
	}
	resp, err := a.doRawChatCompletions(ctx, req, false)
	if err != nil {
		return AugmentGatewayProviderResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		return AugmentGatewayProviderResult{}, fmt.Errorf("augment gateway %s upstream returned %d: %s", provider, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	respBody, err := ReadUpstreamResponseBody(resp.Body, a.gateway.cfg, nil, nil)
	if err != nil {
		return AugmentGatewayProviderResult{}, fmt.Errorf("read augment gateway %s response: %w", provider, err)
	}

	var parsed apicompat.ChatCompletionsResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return AugmentGatewayProviderResult{}, fmt.Errorf("decode augment gateway %s response: %w", provider, err)
	}
	result := augmentGatewayProviderResultFromChatCompletions(provider, parsed, resp.Header.Get("x-request-id"), respBody)
	return result, nil
}

func (a *augmentGatewayOpenAIAdapter) Stream(ctx context.Context, req AugmentGatewayProviderRequest, emit func(AugmentGatewayProviderChunk) error) error {
	provider := a.provider
	if provider == "" {
		provider = AugmentGatewayProviderOpenAI
	}
	if emit == nil {
		return fmt.Errorf("augment gateway %s stream requires emit callback", provider)
	}
	resp, err := a.doRawChatCompletions(ctx, req, true)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		return fmt.Errorf("augment gateway %s stream upstream returned %d: %s", provider, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	contentType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if !strings.Contains(contentType, "text/event-stream") {
		respBody, err := ReadUpstreamResponseBody(resp.Body, a.gateway.cfg, nil, nil)
		if err != nil {
			return fmt.Errorf("read augment gateway %s stream fallback response: %w", provider, err)
		}
		var parsed apicompat.ChatCompletionsResponse
		if err := json.Unmarshal(respBody, &parsed); err != nil {
			return fmt.Errorf("decode augment gateway %s stream fallback response: %w", provider, err)
		}
		for _, chunk := range augmentGatewayProviderChunksFromChatCompletions(provider, parsed, resp.Header.Get("x-request-id"), respBody) {
			if err := emit(chunk); err != nil {
				return err
			}
		}
		return nil
	}

	scanner := bufio.NewScanner(resp.Body)
	maxLineSize := defaultMaxLineSize
	if a.gateway != nil && a.gateway.cfg != nil && a.gateway.cfg.Gateway.MaxLineSize > 0 {
		maxLineSize = a.gateway.cfg.Gateway.MaxLineSize
	}
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)

	dataLines := make([]string, 0, 4)
	flushEvent := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		data := strings.TrimSpace(strings.Join(dataLines, "\n"))
		dataLines = dataLines[:0]
		if data == "" {
			return nil
		}
		if data == "[DONE]" {
			return emit(AugmentGatewayProviderChunk{
				Provider:          provider,
				UpstreamRequestID: resp.Header.Get("x-request-id"),
				Done:              true,
			})
		}
		var parsed apicompat.ChatCompletionsChunk
		if err := json.Unmarshal([]byte(data), &parsed); err != nil {
			return fmt.Errorf("decode augment gateway %s stream chunk: %w", provider, err)
		}
		for _, chunk := range augmentGatewayProviderChunksFromChatCompletionsChunk(provider, parsed, resp.Header.Get("x-request-id"), []byte(data)) {
			if err := emit(chunk); err != nil {
				return err
			}
		}
		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if err := flushEvent(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(trimmed, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flushEvent()
}

func (a *augmentGatewayOpenAIAdapter) doRawChatCompletions(ctx context.Context, req AugmentGatewayProviderRequest, stream bool) (*http.Response, error) {
	provider := a.provider
	if provider == "" {
		provider = AugmentGatewayProviderOpenAI
	}
	if a == nil || a.gateway == nil {
		return nil, &AugmentGatewayProviderNotImplementedError{Provider: provider, Operation: "raw_chat_completions"}
	}
	if a.gateway.httpUpstream == nil {
		return nil, fmt.Errorf("augment gateway %s adapter has no HTTP upstream", provider)
	}
	if req.Account == nil {
		return nil, fmt.Errorf("augment gateway %s adapter requires selected account", provider)
	}

	body := cloneAugmentGatewayRawMap(req.RawBody)
	if body == nil {
		body = augmentGatewayProviderRequestBodyFromParts(req)
	}
	body["stream"] = stream
	rawBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal augment gateway %s request: %w", provider, err)
	}
	if stream {
		rawBody, err = augmentGatewayEnsureChatStreamUsage(rawBody)
		if err != nil {
			return nil, fmt.Errorf("enable augment gateway %s stream usage: %w", provider, err)
		}
	}
	if provider == AugmentGatewayProviderDeepSeek {
		augmentGatewayTraceDeepSeekUpstreamShape(req, rawBody, stream)
	}

	apiKey := req.Account.GetOpenAIApiKey()
	if apiKey == "" {
		return nil, fmt.Errorf("augment gateway %s account %d missing api_key", provider, req.Account.ID)
	}
	baseURL := req.Account.GetOpenAIBaseURL()
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	validatedURL, err := a.gateway.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid augment gateway %s base_url: %w", provider, err)
	}

	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	upstreamReq, err := http.NewRequestWithContext(upstreamCtx, http.MethodPost, buildOpenAIChatCompletionsURL(validatedURL), bytes.NewReader(rawBody))
	releaseUpstreamCtx()
	if err != nil {
		return nil, fmt.Errorf("build augment gateway %s upstream request: %w", provider, err)
	}
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("Authorization", "Bearer "+apiKey)
	if stream {
		upstreamReq.Header.Set("Accept", "text/event-stream")
	} else {
		upstreamReq.Header.Set("Accept", "application/json")
	}
	if customUA := req.Account.GetOpenAIUserAgent(); customUA != "" {
		upstreamReq.Header.Set("user-agent", customUA)
	}

	proxyURL := ""
	if req.Account.Proxy != nil {
		proxyURL = req.Account.Proxy.URL()
	}
	return a.gateway.httpUpstream.Do(upstreamReq, proxyURL, req.Account.ID, req.Account.Concurrency)
}

func augmentGatewayTraceDeepSeekUpstreamShape(req AugmentGatewayProviderRequest, rawBody []byte, stream bool) {
	var body map[string]any
	if err := json.Unmarshal(rawBody, &body); err != nil {
		logger.LegacyPrintf(
			"service.augment_gateway",
			"deepseek_request_shape endpoint=%s model=%s stream=%t raw_bytes=%d full_hash=%s decode_error=%v",
			req.Endpoint,
			req.ModelID,
			stream,
			len(rawBody),
			augmentGatewayShortHash(rawBody),
			err,
		)
		return
	}

	messages, _ := body["messages"].([]any)
	roles := make([]string, 0, len(messages))
	firstSystemBytes := 0
	firstSystemHash := ""
	for _, rawMessage := range messages {
		message, ok := rawMessage.(map[string]any)
		if !ok {
			roles = append(roles, "?")
			continue
		}
		role, _ := message["role"].(string)
		role = strings.TrimSpace(role)
		if role == "" {
			role = "?"
		}
		roles = append(roles, role)
		if role == "system" && firstSystemHash == "" {
			contentBytes, _ := json.Marshal(message["content"])
			firstSystemBytes = len(contentBytes)
			firstSystemHash = augmentGatewayShortHash(contentBytes)
		}
	}

	tools, _ := body["tools"].([]any)
	prefixHash, prefixBytes := augmentGatewayDeepSeekPrefixBeforeLastMessageHash(body, messages)
	userIDPresent := false
	if userID, ok := body["user_id"].(string); ok && strings.TrimSpace(userID) != "" {
		userIDPresent = true
	}
	cacheKeyPresent := false
	if cacheKey, ok := body["prompt_cache_key"].(string); ok && strings.TrimSpace(cacheKey) != "" {
		cacheKeyPresent = true
	}

	logger.LegacyPrintf(
		"service.augment_gateway",
		"deepseek_request_shape endpoint=%s model=%s upstream_model=%s stream=%t raw_bytes=%d full_hash=%s messages=%d roles=%s tools=%d prefix_before_last_bytes=%d prefix_before_last_hash=%s first_system_bytes=%d first_system_hash=%s user_id_present=%t prompt_cache_key_present=%t",
		req.Endpoint,
		req.ModelID,
		req.UpstreamModel,
		stream,
		len(rawBody),
		augmentGatewayShortHash(rawBody),
		len(messages),
		strings.Join(roles, ","),
		len(tools),
		prefixBytes,
		prefixHash,
		firstSystemBytes,
		firstSystemHash,
		userIDPresent,
		cacheKeyPresent,
	)
}

func augmentGatewayDeepSeekPrefixBeforeLastMessageHash(body map[string]any, messages []any) (string, int) {
	if len(messages) == 0 {
		raw, _ := json.Marshal(body)
		return augmentGatewayShortHash(raw), len(raw)
	}
	prefix := make(map[string]any, len(body))
	for key, value := range body {
		prefix[key] = value
	}
	prefix["messages"] = append([]any(nil), messages[:len(messages)-1]...)
	raw, _ := json.Marshal(prefix)
	return augmentGatewayShortHash(raw), len(raw)
}

func augmentGatewayShortHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:8])
}

func augmentGatewayEnsureChatStreamUsage(body []byte) ([]byte, error) {
	updated, err := sjson.SetBytes(body, "stream_options.include_usage", true)
	if err != nil {
		return body, err
	}
	return updated, nil
}

func augmentGatewayProviderResultFromChatCompletions(provider AugmentGatewayProvider, resp apicompat.ChatCompletionsResponse, upstreamRequestID string, raw []byte) AugmentGatewayProviderResult {
	result := AugmentGatewayProviderResult{
		Provider:          provider,
		UpstreamModel:     resp.Model,
		UpstreamRequestID: upstreamRequestID,
		Raw:               augmentGatewayRawObject(raw),
		Usage:             augmentGatewayProviderUsageFromChatUsage(resp.Usage),
	}
	if resp.ID != "" {
		result.RequestID = resp.ID
	}
	if len(resp.Choices) == 0 {
		return result
	}
	msg := resp.Choices[0].Message
	text, _ := augmentGatewayDecodeOpenAIMessageContent(msg.Content)
	result.Text = text
	result.ToolCalls = augmentGatewayProviderToolCallsFromOpenAI(msg.ToolCalls)
	result.ReasoningContent = msg.ReasoningContent
	result.ReasoningContentPresent = augmentGatewayResponseChoiceMessageHasField(raw, "reasoning_content")
	return result
}

func augmentGatewayProviderChunksFromChatCompletions(provider AugmentGatewayProvider, resp apicompat.ChatCompletionsResponse, upstreamRequestID string, raw []byte) []AugmentGatewayProviderChunk {
	chunks := make([]AugmentGatewayProviderChunk, 0, 4)
	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		msg := choice.Message
		if msg.ReasoningContent != "" || augmentGatewayResponseChoiceMessageHasField(raw, "reasoning_content") {
			chunks = append(chunks, AugmentGatewayProviderChunk{
				Provider:              provider,
				UpstreamModel:         resp.Model,
				UpstreamRequestID:     upstreamRequestID,
				ReasoningContentDelta: msg.ReasoningContent,
				ReasoningContentDone:  true,
				Raw:                   augmentGatewayRawObject(raw),
			})
		}
		if text, _ := augmentGatewayDecodeOpenAIMessageContent(msg.Content); strings.TrimSpace(text) != "" {
			chunks = append(chunks, AugmentGatewayProviderChunk{
				Provider:          provider,
				UpstreamModel:     resp.Model,
				UpstreamRequestID: upstreamRequestID,
				TextDelta:         text,
				Raw:               augmentGatewayRawObject(raw),
			})
		}
		for _, toolCall := range augmentGatewayProviderToolCallsFromOpenAI(msg.ToolCalls) {
			chunks = append(chunks, AugmentGatewayProviderChunk{
				Provider:          provider,
				UpstreamModel:     resp.Model,
				UpstreamRequestID: upstreamRequestID,
				ToolCallDelta:     &toolCall,
				Raw:               augmentGatewayRawObject(raw),
			})
		}
		chunks = append(chunks, AugmentGatewayProviderChunk{
			Provider:             provider,
			UpstreamModel:        resp.Model,
			UpstreamRequestID:    upstreamRequestID,
			ProviderFinishReason: choice.FinishReason,
			Done:                 true,
			Usage:                augmentGatewayProviderUsageFromChatUsage(resp.Usage),
			Raw:                  augmentGatewayRawObject(raw),
		})
	} else if resp.Usage != nil {
		chunks = append(chunks, AugmentGatewayProviderChunk{
			Provider:          provider,
			UpstreamModel:     resp.Model,
			UpstreamRequestID: upstreamRequestID,
			Usage:             augmentGatewayProviderUsageFromChatUsage(resp.Usage),
			Done:              true,
			Raw:               augmentGatewayRawObject(raw),
		})
	}
	return chunks
}

func augmentGatewayProviderChunksFromChatCompletionsChunk(provider AugmentGatewayProvider, chunk apicompat.ChatCompletionsChunk, upstreamRequestID string, raw []byte) []AugmentGatewayProviderChunk {
	out := make([]AugmentGatewayProviderChunk, 0, 4)
	if len(chunk.Choices) > 0 {
		choice := chunk.Choices[0]
		if choice.Delta.Content != nil {
			out = append(out, AugmentGatewayProviderChunk{
				Provider:          provider,
				UpstreamModel:     chunk.Model,
				UpstreamRequestID: upstreamRequestID,
				TextDelta:         *choice.Delta.Content,
				Raw:               augmentGatewayRawObject(raw),
			})
		}
		if choice.Delta.ReasoningContent != nil {
			out = append(out, AugmentGatewayProviderChunk{
				Provider:              provider,
				UpstreamModel:         chunk.Model,
				UpstreamRequestID:     upstreamRequestID,
				ReasoningContentDelta: *choice.Delta.ReasoningContent,
				ReasoningContentDone:  true,
				Raw:                   augmentGatewayRawObject(raw),
			})
		}
		for _, toolCall := range augmentGatewayProviderToolCallsFromOpenAI(choice.Delta.ToolCalls) {
			toolCall := toolCall
			out = append(out, AugmentGatewayProviderChunk{
				Provider:          provider,
				UpstreamModel:     chunk.Model,
				UpstreamRequestID: upstreamRequestID,
				ToolCallDelta:     &toolCall,
				Raw:               augmentGatewayRawObject(raw),
			})
		}
		if choice.FinishReason != nil {
			out = append(out, AugmentGatewayProviderChunk{
				Provider:             provider,
				UpstreamModel:        chunk.Model,
				UpstreamRequestID:    upstreamRequestID,
				ProviderFinishReason: *choice.FinishReason,
				Done:                 true,
				Raw:                  augmentGatewayRawObject(raw),
			})
		}
	}
	if chunk.Usage != nil {
		out = append(out, AugmentGatewayProviderChunk{
			Provider:          provider,
			UpstreamModel:     chunk.Model,
			UpstreamRequestID: upstreamRequestID,
			Usage:             augmentGatewayProviderUsageFromChatUsage(chunk.Usage),
			Raw:               augmentGatewayRawObject(raw),
		})
	}
	return out
}

func augmentGatewayProviderUsageFromChatUsage(usage *apicompat.ChatUsage) AugmentGatewayProviderUsage {
	if usage == nil {
		return AugmentGatewayProviderUsage{}
	}
	out := AugmentGatewayProviderUsage{
		InputTokens:  usage.PromptTokens,
		OutputTokens: usage.CompletionTokens,
		TotalTokens:  usage.TotalTokens,
	}
	if usage.PromptTokensDetails != nil {
		out.CachedInputTokens = usage.PromptTokensDetails.CachedTokens
	}
	if out.CachedInputTokens == 0 && usage.PromptCacheHitTokens > 0 {
		out.CachedInputTokens = usage.PromptCacheHitTokens
	}
	if usage.PromptCacheHitTokens > 0 || usage.PromptCacheMissTokens > 0 {
		out.ProviderUsageExtra = map[string]any{
			"prompt_cache_hit_tokens":  usage.PromptCacheHitTokens,
			"prompt_cache_miss_tokens": usage.PromptCacheMissTokens,
		}
	}
	return out
}

func augmentGatewayProviderToolCallsFromOpenAI(in []apicompat.ChatToolCall) []AugmentGatewayToolCall {
	if len(in) == 0 {
		return nil
	}
	out := make([]AugmentGatewayToolCall, 0, len(in))
	for _, toolCall := range in {
		out = append(out, AugmentGatewayToolCall{
			Index: toolCall.Index,
			ID:    toolCall.ID,
			Type:  toolCall.Type,
			Function: AugmentGatewayToolCallFunction{
				Name:      toolCall.Function.Name,
				Arguments: toolCall.Function.Arguments,
			},
		})
	}
	return out
}

func augmentGatewayDecodeOpenAIMessageContent(raw json.RawMessage) (string, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return "", nil
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString, nil
	}
	var parts []map[string]any
	if err := json.Unmarshal(raw, &parts); err == nil {
		var b strings.Builder
		for _, part := range parts {
			if text, ok := part["text"].(string); ok {
				if b.Len() > 0 {
					b.WriteString("\n")
				}
				b.WriteString(text)
			}
		}
		return b.String(), nil
	}
	return "", fmt.Errorf("unsupported content shape")
}

func augmentGatewayResponseChoiceMessageHasField(raw []byte, field string) bool {
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return false
	}
	choices, _ := body["choices"].([]any)
	if len(choices) == 0 {
		return false
	}
	choice, _ := choices[0].(map[string]any)
	message, _ := choice["message"].(map[string]any)
	_, ok := message[field]
	return ok
}

func augmentGatewayRawObject(raw []byte) map[string]any {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}
