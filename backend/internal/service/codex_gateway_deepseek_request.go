package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func BuildCodexGatewayDeepSeekRequest(model CodexGatewayModel, req CodexGatewayResponsesCreateRequest, stateStore *CodexGatewayStateStore, ctx CodexGatewayDeepSeekRequestContext, cfg CodexGatewayDeepSeekRequestConfig) (CodexGatewayPreparedDeepSeekRequest, error) {
	if strings.TrimSpace(model.Provider) != "" && !strings.EqualFold(strings.TrimSpace(model.Provider), "deepseek") {
		return CodexGatewayPreparedDeepSeekRequest{}, fmt.Errorf("codex deepseek request requires a deepseek model")
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

	toolMapping, err := BuildCodexGatewayToolMapping(req.Tools, cfg.ToolMappingConfig)
	if err != nil {
		return CodexGatewayPreparedDeepSeekRequest{}, err
	}
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
	if req.ParallelToolCalls != nil && model.SupportsParallelToolCalls {
		body["parallel_tool_calls"] = *req.ParallelToolCalls
	}
	if len(req.Include) > 0 {
		body["include"] = cloneCodexGatewayRawJSON(req.Include)
	}

	if userID, err := codexGatewayDeepSeekStableUserID(ctx, req.RawFields); err != nil {
		return CodexGatewayPreparedDeepSeekRequest{}, err
	} else if userID != "" {
		body["user_id"] = userID
	}

	rawToolsByAlias := make(map[string]CodexGatewayToolNameMapEntry, len(toolMapping.NameMap))
	for alias, entry := range toolMapping.NameMap {
		rawToolsByAlias[alias] = entry
	}

	messages, _, err := buildCodexGatewayDeepSeekMessages(req, stateStore, ctx, toolMapping, cfg, upstreamModel)
	if err != nil {
		return CodexGatewayPreparedDeepSeekRequest{}, err
	}
	if len(messages) > 0 {
		codexGatewayBackfillDeepSeekAssistantReasoning(messages)
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

	return CodexGatewayPreparedDeepSeekRequest{
		Body:        body,
		ToolNameMap: toolMapping.NameMap,
	}, nil
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

func buildCodexGatewayDeepSeekMessages(req CodexGatewayResponsesCreateRequest, stateStore *CodexGatewayStateStore, ctx CodexGatewayDeepSeekRequestContext, toolMapping CodexGatewayToolMappingResult, cfg CodexGatewayDeepSeekRequestConfig, upstreamModel string) ([]any, map[string]CodexGatewayStoredToolCall, error) {
	items, err := decodeCodexGatewayInputItems(req.Input)
	if err != nil {
		return nil, nil, err
	}

	var storedAssistant any
	seedCalls := make(map[string]CodexGatewayStoredToolCall)
	if req.PreviousResponseID != nil && strings.TrimSpace(*req.PreviousResponseID) != "" && stateStore == nil {
		return nil, nil, fmt.Errorf("%w: previous_response_id requires state store", ErrCodexGatewayStateInvalid)
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
			return nil, nil, err
		}
		if err := validateCodexGatewayResponseState(state); err != nil {
			return nil, nil, err
		}
		storedAssistant = codexGatewayDeepSeekAssistantMessageFromState(state)
		for _, call := range state.ToolCalls {
			seedCalls[strings.TrimSpace(call.ID)] = call
		}
		if len(state.ToolCalls) > 0 && !codexGatewayInputHasToolCallOutput(items) {
			return nil, nil, fmt.Errorf("%w: previous_response_id requires function_call_output items", ErrCodexGatewayStateInvalid)
		}
	}

	messages := make([]any, 0, len(items)+1)
	if storedAssistant != nil {
		messages = append(messages, storedAssistant)
	}
	var pendingToolCallAssistant map[string]any
	flushPendingToolCallAssistant := func() {
		if pendingToolCallAssistant == nil {
			return
		}
		messages = append(messages, pendingToolCallAssistant)
		pendingToolCallAssistant = nil
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
			return nil, nil, err
		}
		if storedAssistant != nil && len(seedCalls) > 0 && len(openCalls) > 0 {
			if msg == nil || strings.TrimSpace(firstCodexGatewayToolString(msg["role"])) != "tool" {
				return nil, nil, fmt.Errorf("%w: replayed tool outputs must precede subsequent turns", ErrCodexGatewayStateInvalid)
			}
		}
		for _, callID := range newCalls {
			if _, exists := seenCallIDs[callID]; exists {
				return nil, nil, fmt.Errorf("codex deepseek request has duplicate call_id %q", callID)
			}
			seenCallIDs[callID] = struct{}{}
			openCalls[callID]++
		}
		if msg != nil && codexGatewayDeepSeekCanMergeAssistantToolCallMessage(msg) {
			if pendingToolCallAssistant == nil {
				pendingToolCallAssistant = msg
			} else if err := codexGatewayDeepSeekMergeAssistantToolCallMessage(pendingToolCallAssistant, msg); err != nil {
				return nil, nil, err
			}
			continue
		}
		if msg != nil {
			flushPendingToolCallAssistant()
			messages = append(messages, msg)
		}
		if msg != nil && strings.TrimSpace(firstCodexGatewayToolString(msg["role"])) == "tool" {
			callID := strings.TrimSpace(firstCodexGatewayToolString(msg["tool_call_id"]))
			if callID == "" {
				return nil, nil, fmt.Errorf("codex deepseek request requires tool_call_id")
			}
			if openCalls[callID] == 0 {
				return nil, nil, fmt.Errorf("codex deepseek request has unpaired function_call_output for %q", callID)
			}
			openCalls[callID]--
			if openCalls[callID] == 0 {
				delete(openCalls, callID)
			}
		}
	}
	flushPendingToolCallAssistant()
	if storedAssistant != nil && len(seedCalls) > 0 && len(openCalls) > 0 {
		return nil, nil, fmt.Errorf("codex deepseek request has incomplete replay for response %q", strings.TrimSpace(*req.PreviousResponseID))
	}

	return messages, seedCalls, nil
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
	if strings.TrimSpace(firstCodexGatewayToolString(dst["reasoning_content"])) == "" {
		if reasoning := strings.TrimSpace(firstCodexGatewayToolString(src["reasoning_content"])); reasoning != "" {
			dst["reasoning_content"] = reasoning
		}
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
		case "function_call_output", "custom_tool_call_output":
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
	case "message":
		return convertCodexGatewayMessageItem(m, toolMapping, cfg)
	case "function_call":
		return convertCodexGatewayFunctionCallItem(m, toolMapping)
	case "custom_tool_call":
		return convertCodexGatewayCustomToolCallItem(m, toolMapping)
	case "function_call_output":
		return convertCodexGatewayFunctionCallOutputItem(m)
	case "custom_tool_call_output":
		return convertCodexGatewayFunctionCallOutputItem(m)
	default:
		return convertCodexGatewayMessageItem(m, toolMapping, cfg)
	}
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

func codexGatewayDeepSeekStableUserID(ctx CodexGatewayDeepSeekRequestContext, rawFields map[string]json.RawMessage) (string, error) {
	if rawFields != nil {
		if raw, ok := rawFields["user_id"]; ok && len(raw) > 0 {
			var userID string
			if err := json.Unmarshal(raw, &userID); err != nil {
				return "", fmt.Errorf("decode user_id: %w", err)
			}
			if !codexGatewayDeepSeekValidUserID(userID) {
				return "", fmt.Errorf("user_id must match [A-Za-z0-9_-]{1,512}")
			}
			return userID, nil
		}
	}
	if strings.TrimSpace(ctx.UserID) != "" {
		if !codexGatewayDeepSeekValidUserID(ctx.UserID) {
			return "", fmt.Errorf("user_id must match [A-Za-z0-9_-]{1,512}")
		}
		return ctx.UserID, nil
	}
	parts := []string{"codex_gateway_deepseek"}
	if strings.TrimSpace(ctx.IsolationKey) != "" {
		parts = append(parts, "isolation="+strings.TrimSpace(ctx.IsolationKey))
	} else if strings.TrimSpace(ctx.SessionKey) != "" {
		parts = append(parts, "session="+strings.TrimSpace(ctx.SessionKey))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return "sub2api_" + hex.EncodeToString(sum[:12]), nil
}

func codexGatewayDeepSeekValidUserID(userID string) bool {
	userID = strings.TrimSpace(userID)
	if userID == "" || len(userID) > 512 {
		return false
	}
	for _, r := range userID {
		if unicodeCodePointInvalid(r) {
			return false
		}
	}
	return true
}

func unicodeCodePointInvalid(r rune) bool {
	return !(r == '-' || r == '_' || (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z'))
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
