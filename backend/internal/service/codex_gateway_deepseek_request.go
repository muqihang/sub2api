package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var codexGatewayDeepSeekChatCompletionsAllowlist = map[string]struct{}{
	"model":             {},
	"messages":          {},
	"tools":             {},
	"tool_choice":       {},
	"reasoning_effort":  {},
	"thinking":          {},
	"max_tokens":        {},
	"stream":            {},
	"stream_options":    {},
	"temperature":       {},
	"top_p":             {},
	"presence_penalty":  {},
	"frequency_penalty": {},
	"user_id":           {},
}

const codexGatewayDeepSeekSerialToolInstruction = "Serial tool calling is required for this request: before receiving tool output, emit at most one tool call. After the tool output is provided, you may decide whether another tool call is needed."

type codexGatewayDeepSeekReplayDiagnostics struct {
	PreviousResponseIDPresent bool
	StateLookupStatus         string
	ReplayMode                string
}

func BuildCodexGatewayDeepSeekRequest(model CodexGatewayModel, req CodexGatewayResponsesCreateRequest, stateStore *CodexGatewayStateStore, ctx CodexGatewayDeepSeekRequestContext, cfg CodexGatewayDeepSeekRequestConfig) (CodexGatewayPreparedDeepSeekRequest, error) {
	if strings.TrimSpace(model.Provider) != "" && !strings.EqualFold(strings.TrimSpace(model.Provider), "deepseek") {
		return CodexGatewayPreparedDeepSeekRequest{}, fmt.Errorf("codex deepseek request requires a deepseek model")
	}
	if err := normalizeCodexGatewayLegacyToolRefs(&req); err != nil {
		return CodexGatewayPreparedDeepSeekRequest{}, err
	}
	upstreamModel := strings.TrimSpace(model.UpstreamModel)
	if upstreamModel == "" {
		upstreamModel = strings.TrimSpace(model.Slug)
	}
	if upstreamModel == "" {
		return CodexGatewayPreparedDeepSeekRequest{}, fmt.Errorf("codex deepseek request requires an upstream model")
	}

	body := map[string]any{
		"model": upstreamModel,
	}
	var leadingMessages []any

	if instructions, ok := parseCodexGatewayJSONString(req.Instructions); ok {
		leadingMessages = append(leadingMessages, map[string]any{
			"role":    "system",
			"content": instructions,
		})
	}
	toolCfg := cfg.ToolMappingConfig
	if toolCfg.DisableDeepSeekSchemaFlattening {
		toolCfg.EnableDeepSeekSchemaFlattening = false
	}
	if toolCfg.DeepSeekFlattenMinDepth <= 0 {
		toolCfg.DeepSeekFlattenMinDepth = 3
	}
	if toolCfg.DeepSeekFlattenMinLeaves <= 0 {
		toolCfg.DeepSeekFlattenMinLeaves = 4
	}

	toolMapping, err := BuildCodexGatewayToolMapping(req.Tools, toolCfg)
	if err != nil {
		return CodexGatewayPreparedDeepSeekRequest{}, err
	}
	toolMapping = codexGatewayDeepSeekRestrictHostedToolMapping(toolMapping)
	toolMapping = codexGatewayDeepSeekAdaptToolMapping(toolMapping, toolCfg)
	if len(toolMapping.Tools) > 0 {
		tools := make([]any, 0, len(toolMapping.Tools))
		for _, tool := range toolMapping.Tools {
			tools = append(tools, tool)
		}
		body["tools"] = tools
	}
	if len(toolMapping.IgnoredHostedToolTypes) > 0 {
		leadingMessages = append(leadingMessages, map[string]any{
			"role":    "system",
			"content": codexGatewayDeepSeekHostedToolNotice(toolMapping.IgnoredHostedToolTypes),
		})
	}

	reasoningEffort, thinkingEnabled := codexGatewayDeepSeekReasoningConfig(req.Reasoning, model, cfg.AllowReasoningDisable)
	body["reasoning_effort"] = reasoningEffort
	if thinkingEnabled {
		body["thinking"] = map[string]any{"type": "enabled"}
	} else {
		body["thinking"] = map[string]any{"type": "disabled"}
	}

	if req.MaxOutputTokens != nil {
		body["max_tokens"] = float64(*req.MaxOutputTokens)
	}
	if req.Store != nil {
		body["store"] = *req.Store
	}
	if req.Stream != nil {
		body["stream"] = *req.Stream
	}
	if len(req.Include) > 0 {
		body["include"] = cloneCodexGatewayRawJSON(req.Include)
	}

	userID, userIDDiag, err := codexGatewayDeepSeekStableUserID(ctx, req.RawFields)
	if err != nil {
		return CodexGatewayPreparedDeepSeekRequest{}, err
	} else if userID != "" {
		body["user_id"] = userID
	}

	rawToolsByAlias := make(map[string]CodexGatewayToolNameMapEntry, len(toolMapping.NameMap))
	for alias, entry := range toolMapping.NameMap {
		rawToolsByAlias[alias] = entry
	}

	messages, _, replayDiag, err := buildCodexGatewayDeepSeekMessages(req, stateStore, ctx, toolMapping, cfg, upstreamModel)
	if err != nil {
		if ctx.CaptureTrace != nil && ctx.CaptureTrace.manager != nil {
			diagnostics := replayDiag.toCaptureMap()
			for key, value := range codexGatewayDeepSeekUserScopeDiagnostics(userID, userIDDiag, ctx.CaptureTrace.manager.redact) {
				diagnostics[key] = value
			}
			ctx.CaptureTrace.manager.mergeRequestDiagnostics(ctx.CaptureTrace, map[string]any{
				"deepseek_cache": diagnostics,
			})
		}
		return CodexGatewayPreparedDeepSeekRequest{}, err
	}
	if len(messages) > 0 {
		codexGatewayBackfillDeepSeekAssistantReasoning(messages)
		if req.ParallelToolCalls != nil && !*req.ParallelToolCalls && !codexGatewayDeepSeekSystemPrefixHasContent(messages, codexGatewayDeepSeekSerialToolInstruction) {
			leadingMessages = append(leadingMessages, map[string]any{
				"role":    "system",
				"content": codexGatewayDeepSeekSerialToolInstruction,
			})
		}
		leadingMessages = codexGatewayDeepSeekDeduplicateLeadingSystemMessages(leadingMessages, messages)
		if len(leadingMessages) > 0 {
			messages = append(leadingMessages, messages...)
		}
		body["messages"] = messages
	}

	if choice, ok, err := buildCodexGatewayDeepSeekToolChoice(req.ToolChoice, toolMapping, rawToolsByAlias); err != nil {
		return CodexGatewayPreparedDeepSeekRequest{}, err
	} else if ok {
		body["tool_choice"] = choice
	}

	if thinkingEnabled {
		delete(body, "temperature")
		delete(body, "top_p")
		delete(body, "presence_penalty")
		delete(body, "frequency_penalty")
	} else {
		if v, ok := parseCodexGatewayRawFloat(req.RawFields["temperature"]); ok {
			body["temperature"] = v
		}
		if v, ok := parseCodexGatewayRawFloat(req.RawFields["top_p"]); ok {
			body["top_p"] = v
		}
		if v, ok := parseCodexGatewayRawFloat(req.RawFields["presence_penalty"]); ok {
			body["presence_penalty"] = v
		}
		if v, ok := parseCodexGatewayRawFloat(req.RawFields["frequency_penalty"]); ok {
			body["frequency_penalty"] = v
		}
	}

	if tools, ok := body["tools"].([]any); ok && len(tools) == 0 {
		delete(body, "tools")
	}
	if len(toolMapping.Tools) > 0 && body["tool_choice"] == nil {
		delete(body, "tool_choice")
	}
	body = codexGatewayDeepSeekAllowlistedChatCompletionsBody(body)
	if ctx.CaptureTrace != nil && ctx.CaptureTrace.manager != nil {
		if diagnostics := codexGatewayDeepSeekCaptureDiagnostics(body, userID, userIDDiag, replayDiag, ctx.CaptureTrace.manager.redact); len(diagnostics) > 0 {
			ctx.CaptureTrace.manager.mergeRequestDiagnostics(ctx.CaptureTrace, map[string]any{
				"deepseek_cache": diagnostics,
			})
			if cacheUsage := codexGatewayDeepSeekCacheUsageFields(diagnostics); len(cacheUsage) > 0 {
				ctx.CaptureTrace.manager.mergeCacheUsage(ctx.CaptureTrace, cacheUsage)
			}
		}
	}

	return CodexGatewayPreparedDeepSeekRequest{
		Body:           body,
		ToolNameMap:    toolMapping.NameMap,
		ReplayMessages: codexGatewayDeepSeekRawMessages(messages),
	}, nil
}

func (d codexGatewayDeepSeekReplayDiagnostics) toCaptureMap() map[string]any {
	out := map[string]any{
		"previous_response_id_present": d.PreviousResponseIDPresent,
	}
	if status := strings.TrimSpace(d.StateLookupStatus); status != "" {
		out["state_lookup_status"] = status
	}
	if mode := strings.TrimSpace(d.ReplayMode); mode != "" {
		out["previous_response_replay_mode"] = mode
	}
	return out
}

func codexGatewayDeepSeekRawMessages(messages []any) []json.RawMessage {
	if len(messages) == 0 {
		return nil
	}
	out := make([]json.RawMessage, 0, len(messages))
	for _, msg := range messages {
		raw, err := json.Marshal(msg)
		if err != nil || len(raw) == 0 {
			continue
		}
		out = append(out, raw)
	}
	return out
}

func codexGatewayDeepSeekDeduplicateLeadingSystemMessages(leadingMessages, replayMessages []any) []any {
	if len(leadingMessages) == 0 || len(replayMessages) == 0 {
		return leadingMessages
	}
	out := leadingMessages[:0]
	for _, msg := range leadingMessages {
		m, ok := msg.(map[string]any)
		content, contentOK := m["content"].(string)
		if ok && m["role"] == "system" && contentOK && codexGatewayDeepSeekSystemPrefixHasContent(replayMessages, content) {
			continue
		}
		out = append(out, msg)
	}
	return out
}

func codexGatewayDeepSeekSystemPrefixHasContent(messages []any, content string) bool {
	for _, msg := range messages {
		m, ok := msg.(map[string]any)
		if !ok || m["role"] != "system" {
			return false
		}
		if m["content"] == content {
			return true
		}
	}
	return false
}

func codexGatewayDeepSeekAllowlistedChatCompletionsBody(body map[string]any) map[string]any {
	if len(body) == 0 {
		return body
	}
	out := make(map[string]any, len(body))
	for key, value := range body {
		if _, ok := codexGatewayDeepSeekChatCompletionsAllowlist[key]; !ok {
			continue
		}
		out[key] = value
	}
	return out
}

func codexGatewayDeepSeekRestrictHostedToolMapping(mapping CodexGatewayToolMappingResult) CodexGatewayToolMappingResult {
	if len(mapping.Tools) == 0 || len(mapping.NameMap) == 0 {
		mapping.IgnoredHostedToolTypes = uniqueCodexGatewayStrings(mapping.IgnoredHostedToolTypes)
		return mapping
	}

	filtered := CodexGatewayToolMappingResult{
		Tools:                  make([]map[string]any, 0, len(mapping.Tools)),
		NameMap:                make(map[string]CodexGatewayToolNameMapEntry, len(mapping.NameMap)),
		IgnoredHostedToolTypes: append([]string(nil), mapping.IgnoredHostedToolTypes...),
		originalToAlias:        make(map[string]string, len(mapping.originalToAlias)),
	}

	keepAlias := make(map[string]struct{}, len(mapping.NameMap))
	for _, tool := range mapping.Tools {
		function, _ := tool["function"].(map[string]any)
		alias := strings.TrimSpace(firstCodexGatewayToolString(function["name"]))
		entry, ok := mapping.NameMap[alias]
		if !ok {
			filtered.Tools = append(filtered.Tools, tool)
			continue
		}
		if entry.Kind == CodexGatewayToolKindHosted && !strings.EqualFold(strings.TrimSpace(entry.Name), "web_search") {
			filtered.IgnoredHostedToolTypes = append(filtered.IgnoredHostedToolTypes, entry.Name)
			continue
		}
		filtered.Tools = append(filtered.Tools, tool)
		filtered.NameMap[alias] = entry
		keepAlias[alias] = struct{}{}
	}

	for alias, entry := range mapping.NameMap {
		if _, ok := keepAlias[alias]; ok {
			continue
		}
		if entry.Kind != CodexGatewayToolKindHosted {
			filtered.NameMap[alias] = entry
			keepAlias[alias] = struct{}{}
		}
	}
	for key, alias := range mapping.originalToAlias {
		if _, ok := keepAlias[alias]; ok {
			filtered.originalToAlias[key] = alias
		}
	}
	filtered.IgnoredHostedToolTypes = uniqueCodexGatewayStrings(filtered.IgnoredHostedToolTypes)
	return filtered
}

func codexGatewayDeepSeekHostedToolNotice(toolTypes []string) string {
	types := uniqueCodexGatewayStrings(toolTypes)
	if len(types) == 0 {
		return ""
	}
	return "OpenAI hosted tools are not available through the DeepSeek Chat Completions upstream and were not forwarded: " + strings.Join(types, ", ") + ". Use the available local, function, custom, namespace, MCP, browser, or computer-use tools when they are present in this request."
}

func uniqueCodexGatewayStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func buildCodexGatewayMessagesPlaceholder() []any { return nil }

func parseCodexGatewayJSONString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

func buildCodexGatewayDeepSeekMessages(req CodexGatewayResponsesCreateRequest, stateStore *CodexGatewayStateStore, ctx CodexGatewayDeepSeekRequestContext, toolMapping CodexGatewayToolMappingResult, cfg CodexGatewayDeepSeekRequestConfig, upstreamModel string) ([]any, map[string]CodexGatewayStoredToolCall, codexGatewayDeepSeekReplayDiagnostics, error) {
	replayDiag := codexGatewayDeepSeekReplayDiagnostics{
		PreviousResponseIDPresent: req.PreviousResponseID != nil && strings.TrimSpace(*req.PreviousResponseID) != "",
		StateLookupStatus:         "not_requested",
		ReplayMode:                "none",
	}
	items, err := decodeCodexGatewayInputItems(req.Input)
	if err != nil {
		return nil, nil, replayDiag, err
	}

	var storedMessages []any
	seedCalls := make(map[string]CodexGatewayStoredToolCall)
	if req.PreviousResponseID != nil && strings.TrimSpace(*req.PreviousResponseID) != "" && stateStore == nil {
		replayDiag.StateLookupStatus = "validation_failure"
		return nil, nil, replayDiag, fmt.Errorf("%w: previous_response_id requires state store", ErrCodexGatewayStateInvalid)
	}
	if stateStore != nil && req.PreviousResponseID != nil && strings.TrimSpace(*req.PreviousResponseID) != "" {
		state, err := stateStore.Get(CodexGatewayStateLookupKey{
			ResponseID:    strings.TrimSpace(*req.PreviousResponseID),
			SessionKey:    ctx.SessionKey,
			IsolationKey:  ctx.IsolationKey,
			Provider:      "deepseek",
			UpstreamModel: upstreamModel,
		})
		if err != nil {
			replayDiag.StateLookupStatus = codexGatewayDeepSeekStateLookupStatus(err)
			return nil, nil, replayDiag, err
		}
		if err := validateCodexGatewayResponseState(state); err != nil {
			replayDiag.StateLookupStatus = "validation_failure"
			return nil, nil, replayDiag, err
		}
		storedMessages, err = codexGatewayDeepSeekMessagesFromState(state)
		if err != nil {
			replayDiag.StateLookupStatus = "validation_failure"
			return nil, nil, replayDiag, err
		}
		replayDiag.StateLookupStatus = "hit"
		replayDiag.ReplayMode = codexGatewayDeepSeekReplayModeFromState(state)
		for _, call := range state.ToolCalls {
			seedCalls[strings.TrimSpace(call.ID)] = call
		}
		if len(state.ToolCalls) > 0 && !codexGatewayInputHasToolCallOutput(items) {
			replayDiag.StateLookupStatus = "validation_failure"
			return nil, nil, replayDiag, fmt.Errorf("%w: previous_response_id requires function_call_output items", ErrCodexGatewayStateInvalid)
		}
	}

	messages := make([]any, 0, len(items)+len(storedMessages))
	if len(storedMessages) > 0 {
		messages = append(messages, storedMessages...)
	}
	var pendingToolCallAssistant map[string]any
	var pendingReasoning string
	mergePendingReasoning := func(msg map[string]any) {
		if msg == nil || strings.TrimSpace(pendingReasoning) == "" || strings.TrimSpace(firstCodexGatewayToolString(msg["role"])) != "assistant" {
			return
		}
		existing := strings.TrimSpace(firstCodexGatewayToolString(msg["reasoning_content"]))
		if existing == "" {
			msg["reasoning_content"] = pendingReasoning
		} else {
			msg["reasoning_content"] = existing + "\n" + pendingReasoning
		}
		pendingReasoning = ""
	}
	flushPendingToolCallAssistant := func() {
		if pendingToolCallAssistant == nil {
			return
		}
		mergePendingReasoning(pendingToolCallAssistant)
		messages = append(messages, pendingToolCallAssistant)
		pendingToolCallAssistant = nil
	}
	flushPendingReasoning := func() {
		if strings.TrimSpace(pendingReasoning) == "" {
			return
		}
		messages = append(messages, map[string]any{
			"role":              "assistant",
			"content":           "",
			"reasoning_content": pendingReasoning,
		})
		pendingReasoning = ""
	}

	openCalls := make(map[string]int, len(seedCalls))
	seenCallIDs := make(map[string]struct{}, len(seedCalls))
	for id := range seedCalls {
		openCalls[id]++
		seenCallIDs[id] = struct{}{}
	}

	for _, item := range items {
		msg, newCalls, err := convertCodexGatewayInputItem(item, toolMapping, cfg)
		if err != nil {
			return nil, nil, replayDiag, err
		}
		if msg != nil && strings.TrimSpace(firstCodexGatewayToolString(msg["role"])) == "assistant" {
			mergePendingReasoning(msg)
		}
		if len(storedMessages) > 0 && len(seedCalls) > 0 && len(openCalls) > 0 {
			if msg == nil || strings.TrimSpace(firstCodexGatewayToolString(msg["role"])) != "tool" {
				replayDiag.StateLookupStatus = "validation_failure"
				return nil, nil, replayDiag, fmt.Errorf("%w: replayed tool outputs must precede subsequent turns", ErrCodexGatewayStateInvalid)
			}
		}
		for _, callID := range newCalls {
			if _, exists := seenCallIDs[callID]; exists {
				return nil, nil, replayDiag, fmt.Errorf("codex deepseek request has duplicate call_id %q", callID)
			}
			seenCallIDs[callID] = struct{}{}
			openCalls[callID]++
		}
		if msg != nil && codexGatewayDeepSeekCanMergeAssistantToolCallMessage(msg) {
			if pendingToolCallAssistant == nil {
				pendingToolCallAssistant = msg
			} else if err := codexGatewayDeepSeekMergeAssistantToolCallMessage(pendingToolCallAssistant, msg); err != nil {
				return nil, nil, replayDiag, err
			}
			continue
		}
		if msg != nil {
			flushPendingToolCallAssistant()
			if strings.TrimSpace(firstCodexGatewayToolString(msg["role"])) != "assistant" {
				flushPendingReasoning()
			}
			messages = append(messages, msg)
		} else if reasoning := codexGatewayDeepSeekReasoningContentFromItem(item); reasoning != "" {
			if pendingToolCallAssistant != nil {
				if existing := strings.TrimSpace(firstCodexGatewayToolString(pendingToolCallAssistant["reasoning_content"])); existing != "" {
					pendingToolCallAssistant["reasoning_content"] = existing + "\n" + reasoning
				} else {
					pendingToolCallAssistant["reasoning_content"] = reasoning
				}
			} else if len(messages) > 0 {
				if previous, ok := messages[len(messages)-1].(map[string]any); ok && strings.TrimSpace(firstCodexGatewayToolString(previous["role"])) == "assistant" {
					if existing := strings.TrimSpace(firstCodexGatewayToolString(previous["reasoning_content"])); existing != "" {
						previous["reasoning_content"] = existing + "\n" + reasoning
					} else {
						previous["reasoning_content"] = reasoning
					}
				} else {
					pendingReasoning = codexGatewayDeepSeekAppendReasoning(pendingReasoning, reasoning)
				}
			} else {
				pendingReasoning = codexGatewayDeepSeekAppendReasoning(pendingReasoning, reasoning)
			}
		}
		if msg != nil && strings.TrimSpace(firstCodexGatewayToolString(msg["role"])) == "tool" {
			callID := strings.TrimSpace(firstCodexGatewayToolString(msg["tool_call_id"]))
			if callID == "" {
				return nil, nil, replayDiag, fmt.Errorf("codex deepseek request requires tool_call_id")
			}
			if openCalls[callID] == 0 {
				return nil, nil, replayDiag, fmt.Errorf("codex deepseek request has unpaired function_call_output for %q", callID)
			}
			openCalls[callID]--
			if openCalls[callID] == 0 {
				delete(openCalls, callID)
			}
		}
	}
	flushPendingToolCallAssistant()
	flushPendingReasoning()
	if len(storedMessages) > 0 && len(seedCalls) > 0 && len(openCalls) > 0 {
		replayDiag.StateLookupStatus = "validation_failure"
		return nil, nil, replayDiag, fmt.Errorf("codex deepseek request has incomplete replay for response %q", strings.TrimSpace(*req.PreviousResponseID))
	}

	return messages, seedCalls, replayDiag, nil
}

func codexGatewayDeepSeekStateLookupStatus(err error) string {
	switch {
	case err == nil:
		return "hit"
	case errors.Is(err, ErrCodexGatewayStateNotFound):
		return "miss"
	case errors.Is(err, ErrCodexGatewayStateConflict):
		return "conflict"
	case errors.Is(err, ErrCodexGatewayStateInvalid):
		return "validation_failure"
	default:
		return "error"
	}
}

func codexGatewayDeepSeekReplayModeFromState(state CodexGatewayResponseState) string {
	if len(state.ReplayMessages) > 0 {
		return "full_replay_messages"
	}
	if state.AssistantContentPresent || state.ReasoningContentPresent || len(state.ToolCalls) > 0 {
		return "assistant_fallback"
	}
	return "none"
}

func codexGatewayDeepSeekMessagesFromState(state CodexGatewayResponseState) ([]any, error) {
	if len(state.ReplayMessages) > 0 {
		out := make([]any, 0, len(state.ReplayMessages))
		for _, raw := range state.ReplayMessages {
			if len(raw) == 0 {
				continue
			}
			var msg any
			if err := json.Unmarshal(raw, &msg); err != nil {
				return nil, fmt.Errorf("%w: invalid deepseek replay message", ErrCodexGatewayStateInvalid)
			}
			out = append(out, msg)
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	return []any{codexGatewayDeepSeekAssistantMessageFromState(state)}, nil
}

func codexGatewayDeepSeekCanMergeAssistantToolCallMessage(msg map[string]any) bool {
	if strings.TrimSpace(firstCodexGatewayToolString(msg["role"])) != "assistant" {
		return false
	}
	calls, ok := msg["tool_calls"].([]any)
	if !ok || len(calls) == 0 {
		return false
	}
	return strings.TrimSpace(firstCodexGatewayToolString(msg["content"])) == ""
}

func codexGatewayDeepSeekMergeAssistantToolCallMessage(dst, src map[string]any) error {
	dstCalls, ok := dst["tool_calls"].([]any)
	if !ok {
		return fmt.Errorf("codex deepseek request has invalid pending tool_calls")
	}
	srcCalls, ok := src["tool_calls"].([]any)
	if !ok {
		return fmt.Errorf("codex deepseek request has invalid tool_calls")
	}
	dst["tool_calls"] = append(dstCalls, srcCalls...)
	dstReasoning := strings.TrimSpace(firstCodexGatewayToolString(dst["reasoning_content"]))
	srcReasoning := strings.TrimSpace(firstCodexGatewayToolString(src["reasoning_content"]))
	if srcReasoning != "" {
		dst["reasoning_content"] = codexGatewayDeepSeekAppendReasoning(dstReasoning, srcReasoning)
	}
	return nil
}

func codexGatewayInputHasToolCallOutput(items []any) bool {
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		switch strings.TrimSpace(firstCodexGatewayToolString(m["type"])) {
		case "function_call_output", "local_shell_call_output", "custom_tool_call_output":
			return true
		}
	}
	return false
}

func decodeCodexGatewayInputItems(raw json.RawMessage) ([]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var items []any
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("decode input: %w", err)
	}
	return items, nil
}

func convertCodexGatewayInputItem(item any, toolMapping CodexGatewayToolMappingResult, cfg CodexGatewayDeepSeekRequestConfig) (map[string]any, []string, error) {
	m, ok := item.(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("input item must be an object")
	}
	typ := strings.TrimSpace(firstCodexGatewayToolString(m["type"]))
	if typ == "" {
		typ = "message"
	}

	switch typ {
	case "reasoning":
		return nil, nil, nil
	case "message":
		return convertCodexGatewayMessageItem(m, toolMapping, cfg)
	case "function_call":
		return convertCodexGatewayFunctionCallItem(m, toolMapping)
	case "local_shell_call":
		return convertCodexGatewayFunctionCallItem(m, toolMapping)
	case "custom_tool_call":
		return convertCodexGatewayCustomToolCallItem(m, toolMapping)
	case "function_call_output":
		return convertCodexGatewayFunctionCallOutputItem(m)
	case "local_shell_call_output":
		return convertCodexGatewayFunctionCallOutputItem(m)
	case "custom_tool_call_output":
		return convertCodexGatewayFunctionCallOutputItem(m)
	default:
		return convertCodexGatewayMessageItem(m, toolMapping, cfg)
	}
}

func codexGatewayDeepSeekReasoningContentFromItem(item any) string {
	m, ok := item.(map[string]any)
	if !ok || strings.TrimSpace(firstCodexGatewayToolString(m["type"])) != "reasoning" {
		return ""
	}
	var parts []string
	codexGatewayDeepSeekCollectReasoningText(&parts, m["summary_text"])
	codexGatewayDeepSeekCollectReasoningText(&parts, m["summary"])
	codexGatewayDeepSeekCollectReasoningText(&parts, m["content"])
	return strings.Join(parts, "\n")
}

func codexGatewayDeepSeekCollectReasoningText(parts *[]string, value any) {
	switch typed := value.(type) {
	case nil:
		return
	case string:
		if text := strings.TrimSpace(typed); text != "" {
			*parts = append(*parts, text)
		}
	case []any:
		for _, part := range typed {
			codexGatewayDeepSeekCollectReasoningText(parts, part)
		}
	case map[string]any:
		codexGatewayDeepSeekCollectReasoningText(parts, typed["text"])
		codexGatewayDeepSeekCollectReasoningText(parts, typed["summary_text"])
	}
}

func codexGatewayDeepSeekAppendReasoning(existing, next string) string {
	existing = strings.TrimSpace(existing)
	next = strings.TrimSpace(next)
	if existing == "" {
		return next
	}
	if next == "" {
		return existing
	}
	return existing + "\n" + next
}

func convertCodexGatewayMessageItem(m map[string]any, toolMapping CodexGatewayToolMappingResult, cfg CodexGatewayDeepSeekRequestConfig) (map[string]any, []string, error) {
	role := strings.TrimSpace(firstCodexGatewayToolString(m["role"]))
	if role == "" {
		role = "user"
	}
	role = normalizeCodexGatewayDeepSeekMessageRole(role)
	msg := map[string]any{
		"role": role,
	}
	if role == "assistant" {
		if content, ok, err := convertCodexGatewayContentValue(m["content"], cfg); err != nil {
			return nil, nil, err
		} else if ok {
			msg["content"] = content
		} else {
			msg["content"] = ""
		}
		if reasoning := strings.TrimSpace(firstCodexGatewayToolString(m["reasoning_content"])); reasoning != "" {
			msg["reasoning_content"] = reasoning
		} else if _, has := m["reasoning_content"]; has {
			msg["reasoning_content"] = ""
		}
		if calls, err := normalizeCodexGatewayFunctionCalls(m["tool_calls"], toolMapping); err != nil {
			return nil, nil, err
		} else if len(calls) > 0 {
			if _, has := msg["content"]; !has {
				msg["content"] = ""
			}
			msg["tool_calls"] = calls
			return msg, toolCallIDsFromChatToolCalls(calls), nil
		}
		return msg, nil, nil
	}
	if content, ok, err := convertCodexGatewayContentValue(m["content"], cfg); err != nil {
		return nil, nil, err
	} else if ok {
		msg["content"] = content
	} else {
		msg["content"] = ""
	}
	return msg, nil, nil
}

func normalizeCodexGatewayDeepSeekMessageRole(role string) string {
	switch strings.TrimSpace(role) {
	case "developer", "latest_reminder":
		return "system"
	case "system", "user", "assistant", "tool":
		return strings.TrimSpace(role)
	default:
		return "user"
	}
}

func convertCodexGatewayFunctionCallItem(m map[string]any, toolMapping CodexGatewayToolMappingResult) (map[string]any, []string, error) {
	callID := strings.TrimSpace(firstCodexGatewayToolString(m["call_id"], m["tool_call_id"], m["id"]))
	if callID == "" {
		return nil, nil, fmt.Errorf("function_call requires call_id")
	}
	name := strings.TrimSpace(firstCodexGatewayToolString(m["name"]))
	alias, err := resolveCodexGatewayToolAlias(toolMapping, name)
	if err != nil {
		return nil, nil, err
	}
	if alias == "" {
		alias = sanitizeCodexGatewayToolName(name)
	}
	arguments := normalizeCodexGatewayToolArguments(m["arguments"])
	if strings.TrimSpace(firstCodexGatewayToolString(m["type"])) == CodexGatewayOutputItemTypeLocalShellCall {
		arguments = codexGatewayExtractShellArgumentsFromItem(m)
	}
	msg := map[string]any{
		"role":    "assistant",
		"content": "",
		"tool_calls": []any{
			map[string]any{
				"id":   callID,
				"type": "function",
				"function": map[string]any{
					"name":      alias,
					"arguments": arguments,
				},
			},
		},
	}
	if reasoning := strings.TrimSpace(firstCodexGatewayToolString(m["reasoning_content"])); reasoning != "" {
		msg["reasoning_content"] = reasoning
	} else {
		msg["reasoning_content"] = ""
	}
	return msg, []string{callID}, nil
}

func convertCodexGatewayCustomToolCallItem(m map[string]any, toolMapping CodexGatewayToolMappingResult) (map[string]any, []string, error) {
	callID := strings.TrimSpace(firstCodexGatewayToolString(m["call_id"], m["tool_call_id"], m["id"]))
	if callID == "" {
		return nil, nil, fmt.Errorf("custom_tool_call requires call_id")
	}
	name := strings.TrimSpace(firstCodexGatewayToolString(m["name"]))
	alias, err := resolveCodexGatewayToolChoiceAlias(toolMapping, CodexGatewayToolKindCustom, name)
	if err != nil {
		if fallback, ok := codexGatewayFallbackLegacyCustomToolAlias(name); ok {
			alias = fallback
			err = nil
		}
	}
	if err != nil {
		return nil, nil, err
	}
	arguments := normalizeCodexGatewayToolArguments(m["input"])
	msg := map[string]any{
		"role":    "assistant",
		"content": "",
		"tool_calls": []any{
			map[string]any{
				"id":   callID,
				"type": "function",
				"function": map[string]any{
					"name":      alias,
					"arguments": arguments,
				},
			},
		},
		"reasoning_content": "",
	}
	return msg, []string{callID}, nil
}

func convertCodexGatewayFunctionCallOutputItem(m map[string]any) (map[string]any, []string, error) {
	callID := strings.TrimSpace(firstCodexGatewayToolString(m["call_id"], m["tool_call_id"], m["id"]))
	if callID == "" {
		return nil, nil, fmt.Errorf("function_call_output requires call_id")
	}
	output, err := normalizeCodexGatewayToolOutput(m["output"])
	if err != nil {
		return nil, nil, err
	}
	return map[string]any{
		"role":         "tool",
		"tool_call_id": callID,
		"content":      output,
	}, nil, nil
}

func convertCodexGatewayContentValue(value any, cfg CodexGatewayDeepSeekRequestConfig) (string, bool, error) {
	switch typed := value.(type) {
	case nil:
		return "", true, nil
	case string:
		return typed, true, nil
	case []any:
		var parts []string
		for _, partAny := range typed {
			part, ok := partAny.(map[string]any)
			if !ok {
				continue
			}
			switch strings.TrimSpace(firstCodexGatewayToolString(part["type"])) {
			case "input_text", "text", "output_text":
				parts = append(parts, stringifyCodexGatewayContentText(part["text"]))
			case "input_image":
				if strings.EqualFold(strings.TrimSpace(cfg.ImageInputMode), CodexGatewayDeepSeekImageInputModeReject) {
					return "", false, fmt.Errorf("codex deepseek request does not support image input")
				}
				parts = append(parts, codexGatewayDeepSeekImagePlaceholder())
			}
		}
		return strings.Join(parts, "\n"), true, nil
	default:
		if b, err := json.Marshal(typed); err == nil {
			return string(b), true, nil
		}
		return fmt.Sprint(typed), true, nil
	}
}

func stringifyCodexGatewayContentText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		if b, err := json.Marshal(typed); err == nil {
			return string(b)
		}
		return fmt.Sprint(typed)
	}
}

func normalizeCodexGatewayFunctionCalls(value any, toolMapping CodexGatewayToolMappingResult) ([]any, error) {
	calls, ok := value.([]any)
	if !ok {
		return nil, nil
	}
	out := make([]any, 0, len(calls))
	for _, rawCall := range calls {
		call, ok := rawCall.(map[string]any)
		if !ok {
			continue
		}
		id := strings.TrimSpace(firstCodexGatewayToolString(call["id"], call["call_id"]))
		function, _ := call["function"].(map[string]any)
		name := strings.TrimSpace(firstCodexGatewayToolString(call["name"], firstCodexGatewayToolValue(function["name"])))
		if id == "" || name == "" {
			continue
		}
		alias, err := resolveCodexGatewayToolAlias(toolMapping, name)
		if err != nil {
			return nil, err
		}
		if alias == "" {
			alias = sanitizeCodexGatewayToolName(name)
		}
		args := normalizeCodexGatewayToolArguments(firstCodexGatewayToolValue(call["arguments"], function["arguments"]))
		out = append(out, map[string]any{
			"id":   id,
			"type": "function",
			"function": map[string]any{
				"name":      alias,
				"arguments": args,
			},
		})
	}
	return out, nil
}

func toolCallIDsFromChatToolCalls(toolCalls []any) []string {
	ids := make([]string, 0, len(toolCalls))
	for _, callAny := range toolCalls {
		call, ok := callAny.(map[string]any)
		if !ok {
			continue
		}
		id := strings.TrimSpace(firstCodexGatewayToolString(call["id"]))
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func codexGatewayBackfillDeepSeekAssistantReasoning(messages []any) {
	toolLoopActive := false
	for _, rawMessage := range messages {
		message, ok := rawMessage.(map[string]any)
		if !ok {
			continue
		}
		role := strings.TrimSpace(firstCodexGatewayToolString(message["role"]))
		switch role {
		case "tool":
			toolLoopActive = true
		case "assistant":
			if calls, ok := message["tool_calls"].([]any); ok && len(calls) > 0 {
				toolLoopActive = true
			}
		}
	}
	if !toolLoopActive {
		return
	}
	for _, rawMessage := range messages {
		message, ok := rawMessage.(map[string]any)
		if !ok {
			continue
		}
		if strings.TrimSpace(firstCodexGatewayToolString(message["role"])) != "assistant" {
			continue
		}
		if _, has := message["reasoning_content"]; !has {
			message["reasoning_content"] = ""
		}
	}
}

func normalizeCodexGatewayToolArguments(value any) string {
	switch typed := value.(type) {
	case nil:
		return "{}"
	case string:
		if strings.TrimSpace(typed) == "" {
			return "{}"
		}
		return typed
	default:
		b, err := json.Marshal(typed)
		if err != nil {
			return "{}"
		}
		return string(b)
	}
}

func normalizeCodexGatewayToolOutput(value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", nil
	case string:
		return typed, nil
	default:
		b, err := json.Marshal(typed)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
}

func buildCodexGatewayDeepSeekToolChoice(raw json.RawMessage, toolMapping CodexGatewayToolMappingResult, lookup map[string]CodexGatewayToolNameMapEntry) (any, bool, error) {
	if len(raw) == 0 {
		return nil, false, nil
	}
	var choice any
	if err := json.Unmarshal(raw, &choice); err != nil {
		return nil, false, fmt.Errorf("decode tool_choice: %w", err)
	}
	switch typed := choice.(type) {
	case string:
		if strings.TrimSpace(typed) == "" || strings.EqualFold(strings.TrimSpace(typed), "auto") {
			return nil, false, nil
		}
		normalized := strings.TrimSpace(strings.ToLower(typed))
		if normalized == "none" || normalized == "required" {
			return typed, true, nil
		}
		alias, err := resolveCodexGatewayToolAlias(toolMapping, typed)
		if err != nil {
			return nil, false, err
		}
		if alias == "" {
			return nil, false, fmt.Errorf("tool_choice references unknown tool %q", typed)
		}
		return map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": alias,
			},
		}, true, nil
	case map[string]any:
		choiceType := strings.TrimSpace(firstCodexGatewayToolString(typed["type"]))
		if choiceType == "" {
			return nil, false, fmt.Errorf("tool_choice.type is required")
		}
		if strings.EqualFold(choiceType, "auto") {
			return nil, false, nil
		}
		if choiceType == "function" || choiceType == CodexGatewayToolKindCustom || choiceType == CodexGatewayToolKindNamespace {
			name := strings.TrimSpace(firstCodexGatewayToolString(typed["name"]))
			if name == "" {
				if fn, ok := typed["function"].(map[string]any); ok {
					name = strings.TrimSpace(firstCodexGatewayToolString(fn["name"]))
				}
			}
			alias, err := resolveCodexGatewayToolChoiceAlias(toolMapping, choiceType, name)
			if err != nil {
				return nil, false, err
			}
			return map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": alias,
				},
			}, true, nil
		}
		return nil, false, fmt.Errorf("tool_choice has unsupported type %q", choiceType)
	default:
		return nil, false, nil
	}
}

func toolMappingAliasForName(mapping CodexGatewayToolMappingResult, name string) string {
	alias, err := resolveCodexGatewayToolAlias(mapping, name)
	if err != nil {
		return ""
	}
	return alias
}

func resolveCodexGatewayToolAlias(mapping CodexGatewayToolMappingResult, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil
	}
	if alias := resolveCodexGatewayToolAliasCompat(mapping, name); alias != "" {
		return alias, nil
	}
	matches := make([]string, 0, 1)
	for alias, entry := range mapping.NameMap {
		if entry.Name == name || entry.Alias == name || codexGatewayOriginalToolPath(entry) == name {
			matches = append(matches, alias)
		}
	}
	switch len(matches) {
	case 0:
		return "", nil
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("ambiguous tool name %q", name)
	}
}

func resolveCodexGatewayToolAliasCompat(mapping CodexGatewayToolMappingResult, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if strings.Contains(name, ".") {
		if alias := resolveCodexGatewayToolAliasByExactForms(mapping, name, strings.ReplaceAll(name, ".", "__")); alias != "" {
			return alias
		}
	}
	if strings.HasPrefix(name, "custom__") {
		legacyName := strings.TrimPrefix(name, "custom__")
		if normalized := codexGatewayCanonicalToolName(legacyName); normalized != legacyName {
			if alias := resolveCodexGatewayToolAliasByExactForms(mapping, buildCodexGatewayToolAlias("", "custom", normalized), normalized); alias != "" {
				return alias
			}
		}
	}
	if normalized := codexGatewayCanonicalToolName(name); normalized != name {
		if alias := resolveCodexGatewayToolAliasByExactForms(mapping, normalized, buildCodexGatewayToolAlias("", "custom", normalized)); alias != "" {
			return alias
		}
	}
	return ""
}

func resolveCodexGatewayToolAliasByExactForms(mapping CodexGatewayToolMappingResult, forms ...string) string {
	for _, form := range forms {
		form = strings.TrimSpace(form)
		if form == "" {
			continue
		}
		for alias, entry := range mapping.NameMap {
			if entry.Name == form || entry.Alias == form || codexGatewayOriginalToolPath(entry) == form {
				return alias
			}
		}
	}
	return ""
}

func codexGatewayOriginalToolPath(entry CodexGatewayToolNameMapEntry) string {
	if entry.NamespacePath != "" {
		return codexGatewayNamespaceDisplay(entry.NamespacePath) + "__" + entry.Name
	}
	if entry.Namespace != "" {
		return entry.Namespace + "__" + entry.Name
	}
	return entry.Name
}

func resolveCodexGatewayToolChoiceAlias(mapping CodexGatewayToolMappingResult, kind, name string) (string, error) {
	if kind == "" || kind == "function" {
		alias, err := resolveCodexGatewayToolAlias(mapping, name)
		if err != nil {
			return "", err
		}
		if alias == "" {
			return "", fmt.Errorf("tool_choice references unknown tool %q", name)
		}
		return alias, nil
	}

	name = strings.TrimSpace(name)
	matches := make([]string, 0, 1)
	for alias, entry := range mapping.NameMap {
		if entry.Kind != kind {
			continue
		}
		if name == "" || entry.Name == name || entry.Alias == name || codexGatewayOriginalToolPath(entry) == name {
			matches = append(matches, alias)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("tool_choice references unknown tool %q", name)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("ambiguous tool_choice %q", name)
	}
}

func codexGatewayDeepSeekAssistantMessageFromState(state CodexGatewayResponseState) map[string]any {
	msg := map[string]any{
		"role":    "assistant",
		"content": "",
	}
	if state.AssistantContentPresent {
		msg["content"] = state.AssistantContent
	}
	if state.ReasoningContentPresent || state.ReasoningContentSynthesized {
		msg["reasoning_content"] = state.ReasoningContent
	} else {
		msg["reasoning_content"] = ""
	}
	if len(state.ToolCalls) > 0 {
		toolCalls := make([]any, 0, len(state.ToolCalls))
		for _, call := range state.ToolCalls {
			toolCalls = append(toolCalls, map[string]any{
				"id":   call.ID,
				"type": "function",
				"function": map[string]any{
					"name":      codexGatewayDeepSeekStoredToolCallAlias(call, state.ToolNameMap),
					"arguments": call.Arguments,
				},
			})
		}
		msg["tool_calls"] = toolCalls
	}
	return msg
}

func codexGatewayDeepSeekStoredToolCallAlias(call CodexGatewayStoredToolCall, toolNameMap map[string]CodexGatewayToolNameMapEntry) string {
	if alias := strings.TrimSpace(call.Alias); alias != "" {
		return alias
	}
	name := strings.TrimSpace(call.Name)
	if len(toolNameMap) == 0 {
		return name
	}
	matches := make([]string, 0, 1)
	for alias, entry := range toolNameMap {
		if entry.Kind != "" && call.Type != "" && entry.Kind != call.Type {
			continue
		}
		if entry.Name == name || entry.Alias == name || codexGatewayOriginalToolPath(entry) == name {
			matches = append(matches, alias)
		}
	}
	if len(matches) == 1 {
		return matches[0]
	}
	return name
}

func codexGatewayDeepSeekReasoningConfig(raw json.RawMessage, model CodexGatewayModel, allowDisable bool) (string, bool) {
	effort := "max"
	if len(raw) > 0 {
		var parsed map[string]any
		if err := json.Unmarshal(raw, &parsed); err == nil {
			if rawEffort, ok := parsed["effort"].(string); ok {
				switch strings.TrimSpace(strings.ToLower(rawEffort)) {
				case "low", "medium", "high":
					effort = "high"
				case "xhigh", "max":
					effort = "max"
				case "none", "minimal":
					if allowDisable {
						return "max", false
					}
				}
			}
		}
	}
	return effort, true
}

type codexGatewayDeepSeekUserIDDiagnostics struct {
	Scope                string
	Source               string
	WorkspaceScopeKey    string
	ManagedSessionBucket string
}

func codexGatewayDeepSeekStableUserID(ctx CodexGatewayDeepSeekRequestContext, rawFields map[string]json.RawMessage) (string, codexGatewayDeepSeekUserIDDiagnostics, error) {
	diag := codexGatewayDeepSeekUserIDDiagnostics{
		Scope:                "actor_workspace",
		Source:               "derived_actor_workspace",
		WorkspaceScopeKey:    strings.TrimSpace(ctx.WorkspaceKey),
		ManagedSessionBucket: strings.TrimSpace(ctx.ManagedSessionBucket),
	}
	actorScopeKey := strings.TrimSpace(ctx.IsolationKey)
	if actorScopeKey == "" {
		actorScopeKey = "shared_actor"
	}
	workspaceScopeKey := strings.TrimSpace(ctx.WorkspaceKey)
	if workspaceScopeKey == "" {
		workspaceScopeKey = "shared_workspace"
	}
	if diag.ManagedSessionBucket != "" {
		diag.Scope = "actor_workspace_session_bucket"
	}
	providedSeedDigest := ""
	if rawFields != nil {
		if raw, ok := rawFields["user_id"]; ok && len(raw) > 0 {
			var userSeed string
			if err := json.Unmarshal(raw, &userSeed); err != nil {
				return "", codexGatewayDeepSeekUserIDDiagnostics{}, fmt.Errorf("decode user_id: %w", err)
			}
			userSeed = strings.TrimSpace(userSeed)
			if userSeed != "" {
				providedSeedDigest = codexGatewayDeepSeekUserIDSeedDigest(userSeed)
				diag.Source = "raw_fields.user_id"
			}
		}
	}
	if providedSeedDigest == "" && strings.TrimSpace(ctx.UserID) != "" {
		providedSeedDigest = codexGatewayDeepSeekUserIDSeedDigest(strings.TrimSpace(ctx.UserID))
		diag.Source = "request_context.user_id"
	}
	parts := []string{
		"scope=" + diag.Scope,
		"actor_scope=" + actorScopeKey,
		"workspace_scope=" + workspaceScopeKey,
	}
	if providedSeedDigest != "" {
		parts = append(parts, "provided_seed_sha256="+providedSeedDigest)
	}
	if diag.ManagedSessionBucket != "" {
		parts = append(parts, "managed_session_bucket="+diag.ManagedSessionBucket)
	}
	keyMaterial := strings.Join([]string{
		"codex_gateway_deepseek_user_id",
		actorScopeKey,
		workspaceScopeKey,
	}, "|")
	mac := hmac.New(sha256.New, []byte(keyMaterial))
	_, _ = mac.Write([]byte(strings.Join(parts, "|")))
	return "sub2api_" + hex.EncodeToString(mac.Sum(nil)[:16]), diag, nil
}

func codexGatewayDeepSeekUserIDSeedDigest(seed string) string {
	seed = strings.TrimSpace(seed)
	if seed == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:16])
}

func parseCodexGatewayRawFloat(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var v float64
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, false
	}
	return v, true
}

func codexGatewayDeepSeekImagePlaceholder() string {
	return "[unsupported input_image omitted]"
}
