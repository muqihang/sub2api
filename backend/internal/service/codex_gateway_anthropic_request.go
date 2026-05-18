package service

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const codexGatewayAnthropicDefaultCacheTTL = "1h"

func BuildCodexGatewayAnthropicRequest(model CodexGatewayModel, req CodexGatewayResponsesCreateRequest, stateStore *CodexGatewayStateStore, ctx CodexGatewayAnthropicRequestContext, cfg CodexGatewayAnthropicRequestConfig) (CodexGatewayPreparedAnthropicRequest, error) {
	if strings.TrimSpace(model.Provider) != "" && !strings.EqualFold(strings.TrimSpace(model.Provider), "anthropic") {
		return CodexGatewayPreparedAnthropicRequest{}, fmt.Errorf("codex anthropic request requires an anthropic model")
	}
	upstreamModel := codexGatewayAnthropicResolveUpstreamModel(req.Reasoning, model)
	if upstreamModel == "" {
		return CodexGatewayPreparedAnthropicRequest{}, fmt.Errorf("codex anthropic request requires an upstream model")
	}
	stateModelKey := codexGatewayAnthropicStateModelKey(model, upstreamModel)

	toolMapping, err := BuildCodexGatewayToolMapping(req.Tools, cfg.ToolMappingConfig)
	if err != nil {
		return CodexGatewayPreparedAnthropicRequest{}, err
	}
	body := map[string]any{
		"model":      upstreamModel,
		"max_tokens": codexGatewayAnthropicMaxTokens(req.MaxOutputTokens, model),
	}
	body["cache_control"] = codexGatewayAnthropicCacheControl(cfg)
	if req.Stream != nil {
		body["stream"] = *req.Stream
	}
	if v, ok := parseCodexGatewayRawFloat(req.RawFields["temperature"]); ok {
		body["temperature"] = v
	}
	if v, ok := parseCodexGatewayRawFloat(req.RawFields["top_p"]); ok {
		body["top_p"] = v
	}

	systemBlocks := codexGatewayAnthropicSystemBlocks(req, toolMapping.IgnoredHostedToolTypes, cfg)
	if len(systemBlocks) > 0 {
		body["system"] = systemBlocks
	}

	tools := codexGatewayAnthropicTools(toolMapping, cfg)
	if len(tools) > 0 {
		body["tools"] = tools
	}
	forcedToolChoice := false
	if choice, ok, err := buildCodexGatewayAnthropicToolChoice(req.ToolChoice, toolMapping); err != nil {
		return CodexGatewayPreparedAnthropicRequest{}, err
	} else if ok {
		body["tool_choice"] = choice
		forcedToolChoice = codexGatewayAnthropicToolChoiceDisablesThinking(choice)
	}

	if thinking, outputConfig := codexGatewayAnthropicThinkingConfig(req.Reasoning, model); len(thinking) > 0 {
		if cfg.ForceDisableThinking || forcedToolChoice {
			thinking = map[string]any{"type": "disabled"}
			outputConfig = nil
		}
		body["thinking"] = thinking
		if len(outputConfig) > 0 {
			body["output_config"] = outputConfig
		}
	}

	messages, err := buildCodexGatewayAnthropicMessages(req, stateStore, ctx, toolMapping, cfg, stateModelKey)
	if err != nil {
		return CodexGatewayPreparedAnthropicRequest{}, err
	}
	if len(messages) == 0 {
		messages = []any{map[string]any{
			"role":    "user",
			"content": []any{map[string]any{"type": "text", "text": ""}},
		}}
	}
	body["messages"] = messages

	return CodexGatewayPreparedAnthropicRequest{
		Body:           body,
		ToolNameMap:    toolMapping.NameMap,
		ReplayMessages: codexGatewayAnthropicRawMessages(messages),
	}, nil
}

func codexGatewayAnthropicMaxTokens(max *int, model CodexGatewayModel) int {
	if max != nil && *max > 0 {
		return *max
	}
	if model.MaxOutputTokens > 0 && model.MaxOutputTokens < 8192 {
		return model.MaxOutputTokens
	}
	return 8192
}

func codexGatewayAnthropicSystemBlocks(req CodexGatewayResponsesCreateRequest, ignored []string, cfg CodexGatewayAnthropicRequestConfig) []any {
	var blocks []any
	if instructions, ok := parseCodexGatewayJSONString(req.Instructions); ok && strings.TrimSpace(instructions) != "" {
		blocks = append(blocks, map[string]any{
			"type":          "text",
			"text":          instructions,
			"cache_control": codexGatewayAnthropicCacheControl(cfg),
		})
	}
	if len(ignored) > 0 {
		blocks = append(blocks, map[string]any{
			"type": "text",
			"text": codexGatewayAnthropicHostedToolNotice(ignored),
		})
	}
	return blocks
}

func codexGatewayAnthropicHostedToolNotice(toolTypes []string) string {
	types := uniqueCodexGatewayStrings(toolTypes)
	if len(types) == 0 {
		return ""
	}
	return "OpenAI hosted tools are not available through the Anthropic Messages upstream and were not forwarded: " + strings.Join(types, ", ") + ". Use the available local, function, custom, namespace, MCP, browser, or computer-use tools when they are present in this request."
}

func codexGatewayAnthropicTools(mapping CodexGatewayToolMappingResult, cfg CodexGatewayAnthropicRequestConfig) []any {
	if len(mapping.Tools) == 0 {
		return nil
	}
	tools := make([]any, 0, len(mapping.Tools))
	for _, rawTool := range mapping.Tools {
		fn, _ := rawTool["function"].(map[string]any)
		name := strings.TrimSpace(firstCodexGatewayToolString(fn["name"]))
		if name == "" {
			continue
		}
		schema := firstCodexGatewayToolValue(fn["parameters"])
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		tool := map[string]any{
			"name":         name,
			"input_schema": schema,
		}
		if desc := strings.TrimSpace(firstCodexGatewayToolString(fn["description"])); desc != "" {
			tool["description"] = desc
		}
		tools = append(tools, tool)
	}
	if len(tools) > 0 {
		if last, ok := tools[len(tools)-1].(map[string]any); ok {
			last["cache_control"] = codexGatewayAnthropicCacheControl(cfg)
		}
	}
	return tools
}

func codexGatewayAnthropicCacheControl(cfg CodexGatewayAnthropicRequestConfig) map[string]any {
	ttl := strings.TrimSpace(cfg.CacheTTL)
	if ttl == "" {
		ttl = codexGatewayAnthropicDefaultCacheTTL
	}
	return map[string]any{"type": "ephemeral", "ttl": ttl}
}

func buildCodexGatewayAnthropicMessages(req CodexGatewayResponsesCreateRequest, stateStore *CodexGatewayStateStore, ctx CodexGatewayAnthropicRequestContext, toolMapping CodexGatewayToolMappingResult, cfg CodexGatewayAnthropicRequestConfig, stateModelKey string) ([]any, error) {
	items, err := decodeCodexGatewayInputItems(req.Input)
	if err != nil {
		return nil, err
	}
	if req.PreviousResponseID != nil && strings.TrimSpace(*req.PreviousResponseID) != "" && stateStore == nil {
		return nil, fmt.Errorf("%w: previous_response_id requires state store", ErrCodexGatewayStateInvalid)
	}
	var storedMessages []any
	seedCalls := make(map[string]CodexGatewayStoredToolCall)
	if stateStore != nil && req.PreviousResponseID != nil && strings.TrimSpace(*req.PreviousResponseID) != "" {
		state, err := stateStore.Get(CodexGatewayStateLookupKey{
			ResponseID:    strings.TrimSpace(*req.PreviousResponseID),
			SessionKey:    ctx.SessionKey,
			IsolationKey:  ctx.IsolationKey,
			Provider:      "anthropic",
			UpstreamModel: stateModelKey,
		})
		if err != nil {
			return nil, err
		}
		if err := validateCodexGatewayResponseState(state); err != nil {
			return nil, err
		}
		storedMessages, err = codexGatewayAnthropicMessagesFromState(state)
		if err != nil {
			return nil, err
		}
		for _, call := range state.ToolCalls {
			seedCalls[strings.TrimSpace(call.ID)] = call
		}
		if len(state.ToolCalls) > 0 && !codexGatewayInputHasToolCallOutput(items) {
			return nil, fmt.Errorf("%w: previous_response_id requires function_call_output items", ErrCodexGatewayStateInvalid)
		}
	}

	messages := make([]any, 0, len(items)+len(storedMessages))
	if len(storedMessages) > 0 {
		messages = append(messages, storedMessages...)
	}
	openCalls := make(map[string]int, len(seedCalls))
	for id := range seedCalls {
		openCalls[id]++
	}
	for _, item := range items {
		if len(storedMessages) > 0 && len(seedCalls) > 0 && len(openCalls) > 0 {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("input item must be an object")
			}
			switch strings.TrimSpace(firstCodexGatewayToolString(m["type"])) {
			case "function_call_output", "custom_tool_call_output":
			default:
				return nil, fmt.Errorf("%w: replayed tool outputs must precede subsequent turns", ErrCodexGatewayStateInvalid)
			}
		}
		next, err := convertCodexGatewayInputItemToAnthropicMessages(item, toolMapping, cfg)
		if err != nil {
			return nil, err
		}
		for _, msg := range next {
			if strings.TrimSpace(firstCodexGatewayToolString(msg["role"])) != "assistant" {
				continue
			}
			parts, _ := msg["content"].([]any)
			for _, partAny := range parts {
				part, _ := partAny.(map[string]any)
				if strings.TrimSpace(firstCodexGatewayToolString(part["type"])) != "tool_use" {
					continue
				}
				callID := strings.TrimSpace(firstCodexGatewayToolString(part["id"]))
				if callID == "" {
					return nil, fmt.Errorf("codex anthropic request requires tool_use id")
				}
				openCalls[callID]++
			}
		}
		messages = appendCodexGatewayAnthropicMessages(messages, next...)
		for _, msg := range next {
			if strings.TrimSpace(firstCodexGatewayToolString(msg["role"])) != "user" {
				continue
			}
			parts, _ := msg["content"].([]any)
			for _, partAny := range parts {
				part, _ := partAny.(map[string]any)
				if strings.TrimSpace(firstCodexGatewayToolString(part["type"])) != "tool_result" {
					continue
				}
				callID := strings.TrimSpace(firstCodexGatewayToolString(part["tool_use_id"]))
				if callID == "" {
					return nil, fmt.Errorf("codex anthropic request requires tool_use_id")
				}
				if openCalls[callID] == 0 {
					return nil, fmt.Errorf("codex anthropic request has unpaired function_call_output for %q", callID)
				}
				openCalls[callID]--
				if openCalls[callID] == 0 {
					delete(openCalls, callID)
				}
			}
		}
	}
	if len(storedMessages) > 0 && len(seedCalls) > 0 && len(openCalls) > 0 {
		return nil, fmt.Errorf("codex anthropic request has incomplete replay for response %q", strings.TrimSpace(*req.PreviousResponseID))
	}
	return messages, nil
}

func appendCodexGatewayAnthropicMessages(messages []any, next ...map[string]any) []any {
	for _, msg := range next {
		if msg == nil {
			continue
		}
		role := strings.TrimSpace(firstCodexGatewayToolString(msg["role"]))
		content, _ := msg["content"].([]any)
		if len(messages) > 0 {
			if prev, ok := messages[len(messages)-1].(map[string]any); ok {
				prevRole := strings.TrimSpace(firstCodexGatewayToolString(prev["role"]))
				prevContent, _ := prev["content"].([]any)
				if role == prevRole && len(content) > 0 && codexGatewayAnthropicCanMergeRole(role) {
					prev["content"] = append(prevContent, content...)
					continue
				}
			}
		}
		messages = append(messages, msg)
	}
	return messages
}

func codexGatewayAnthropicCanMergeRole(role string) bool {
	return role == "user" || role == "assistant"
}

func convertCodexGatewayInputItemToAnthropicMessages(item any, toolMapping CodexGatewayToolMappingResult, cfg CodexGatewayAnthropicRequestConfig) ([]map[string]any, error) {
	m, ok := item.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("input item must be an object")
	}
	typ := strings.TrimSpace(firstCodexGatewayToolString(m["type"]))
	if typ == "" {
		typ = "message"
	}
	switch typ {
	case "message":
		msg, err := convertCodexGatewayMessageItemToAnthropic(m, cfg)
		if err != nil {
			return nil, err
		}
		return []map[string]any{msg}, nil
	case "function_call":
		msg, err := convertCodexGatewayFunctionCallItemToAnthropic(m, toolMapping, CodexGatewayToolKindFunction)
		if err != nil {
			return nil, err
		}
		return []map[string]any{msg}, nil
	case "custom_tool_call":
		msg, err := convertCodexGatewayFunctionCallItemToAnthropic(m, toolMapping, CodexGatewayToolKindCustom)
		if err != nil {
			return nil, err
		}
		return []map[string]any{msg}, nil
	case "function_call_output", "custom_tool_call_output":
		msg, err := convertCodexGatewayFunctionCallOutputItemToAnthropic(m)
		if err != nil {
			return nil, err
		}
		return []map[string]any{msg}, nil
	default:
		msg, err := convertCodexGatewayMessageItemToAnthropic(m, cfg)
		if err != nil {
			return nil, err
		}
		return []map[string]any{msg}, nil
	}
}

func convertCodexGatewayMessageItemToAnthropic(m map[string]any, cfg CodexGatewayAnthropicRequestConfig) (map[string]any, error) {
	role := strings.TrimSpace(firstCodexGatewayToolString(m["role"]))
	switch role {
	case "assistant":
	default:
		role = "user"
	}
	blocks, err := convertCodexGatewayContentValueToAnthropicBlocks(m["content"], role, cfg)
	if err != nil {
		return nil, err
	}
	return map[string]any{"role": role, "content": blocks}, nil
}

func convertCodexGatewayContentValueToAnthropicBlocks(value any, role string, cfg CodexGatewayAnthropicRequestConfig) ([]any, error) {
	textType := "text"
	if role == "assistant" {
		textType = "text"
	}
	switch typed := value.(type) {
	case nil:
		return []any{map[string]any{"type": textType, "text": ""}}, nil
	case string:
		return []any{map[string]any{"type": textType, "text": typed}}, nil
	case []any:
		blocks := make([]any, 0, len(typed))
		for _, partAny := range typed {
			part, ok := partAny.(map[string]any)
			if !ok {
				continue
			}
			switch strings.TrimSpace(firstCodexGatewayToolString(part["type"])) {
			case "input_text", "text", "output_text":
				blocks = append(blocks, map[string]any{
					"type": "text",
					"text": stringifyCodexGatewayContentText(part["text"]),
				})
			case "input_image":
				block, ok, err := codexGatewayAnthropicImageBlock(part, cfg)
				if err != nil {
					return nil, err
				}
				if ok {
					blocks = append(blocks, block)
				}
			}
		}
		if len(blocks) == 0 {
			blocks = append(blocks, map[string]any{"type": "text", "text": ""})
		}
		return blocks, nil
	default:
		return []any{map[string]any{"type": textType, "text": stringifyCodexGatewayContentText(typed)}}, nil
	}
}

func codexGatewayAnthropicImageBlock(part map[string]any, cfg CodexGatewayAnthropicRequestConfig) (map[string]any, bool, error) {
	if strings.EqualFold(strings.TrimSpace(cfg.ImageInputMode), CodexGatewayDeepSeekImageInputModeReject) {
		return nil, false, fmt.Errorf("codex anthropic request does not support image input")
	}
	imageURL := strings.TrimSpace(firstCodexGatewayToolString(part["image_url"]))
	if imageURL == "" {
		if nested, ok := part["image_url"].(map[string]any); ok {
			imageURL = strings.TrimSpace(firstCodexGatewayToolString(nested["url"]))
		}
	}
	mediaType, data, ok := splitCodexGatewayDataImageURL(imageURL)
	if !ok {
		return nil, false, nil
	}
	return map[string]any{
		"type": "image",
		"source": map[string]any{
			"type":       "base64",
			"media_type": mediaType,
			"data":       data,
		},
	}, true, nil
}

func splitCodexGatewayDataImageURL(uri string) (string, string, bool) {
	uri = strings.TrimSpace(uri)
	if !strings.HasPrefix(uri, "data:") {
		return "", "", false
	}
	meta, data, ok := strings.Cut(strings.TrimPrefix(uri, "data:"), ",")
	if !ok || !strings.Contains(meta, ";base64") {
		return "", "", false
	}
	mediaType := strings.TrimSuffix(meta, ";base64")
	if mediaType == "" {
		mediaType = "image/png"
	}
	if _, err := base64.StdEncoding.DecodeString(data); err != nil {
		return "", "", false
	}
	return mediaType, data, true
}

func convertCodexGatewayFunctionCallItemToAnthropic(m map[string]any, toolMapping CodexGatewayToolMappingResult, kind string) (map[string]any, error) {
	callID := strings.TrimSpace(firstCodexGatewayToolString(m["call_id"], m["tool_call_id"], m["id"]))
	if callID == "" {
		return nil, fmt.Errorf("function_call requires call_id")
	}
	name := strings.TrimSpace(firstCodexGatewayToolString(m["name"]))
	var alias string
	var err error
	if kind == CodexGatewayToolKindCustom {
		alias, err = resolveCodexGatewayToolChoiceAlias(toolMapping, CodexGatewayToolKindCustom, name)
	} else {
		alias, err = resolveCodexGatewayAnthropicToolAlias(toolMapping, name)
	}
	if err != nil {
		return nil, err
	}
	if alias == "" {
		alias = sanitizeCodexGatewayToolName(name)
	}
	args := normalizeCodexGatewayToolArguments(firstCodexGatewayToolValue(m["arguments"], m["input"]))
	input := codexGatewayAnthropicToolInputRawMessage(args)
	return map[string]any{
		"role": "assistant",
		"content": []any{map[string]any{
			"type":  "tool_use",
			"id":    callID,
			"name":  alias,
			"input": input,
		}},
	}, nil
}

func convertCodexGatewayFunctionCallOutputItemToAnthropic(m map[string]any) (map[string]any, error) {
	callID := strings.TrimSpace(firstCodexGatewayToolString(m["call_id"], m["tool_call_id"], m["id"]))
	if callID == "" {
		return nil, fmt.Errorf("function_call_output requires call_id")
	}
	output, err := normalizeCodexGatewayToolOutput(m["output"])
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"role": "user",
		"content": []any{map[string]any{
			"type":        "tool_result",
			"tool_use_id": callID,
			"content":     output,
		}},
	}, nil
}

func buildCodexGatewayAnthropicToolChoice(raw json.RawMessage, toolMapping CodexGatewayToolMappingResult) (any, bool, error) {
	if len(raw) == 0 {
		return nil, false, nil
	}
	var choice any
	if err := json.Unmarshal(raw, &choice); err != nil {
		return nil, false, fmt.Errorf("decode tool_choice: %w", err)
	}
	switch typed := choice.(type) {
	case string:
		normalized := strings.TrimSpace(strings.ToLower(typed))
		switch normalized {
		case "", "auto":
			return nil, false, nil
		case "none":
			return map[string]any{"type": "none"}, true, nil
		case "required":
			return map[string]any{"type": "any"}, true, nil
		default:
			alias, err := resolveCodexGatewayAnthropicToolAlias(toolMapping, typed)
			if err != nil {
				return nil, false, err
			}
			if alias == "" {
				return nil, false, fmt.Errorf("tool_choice references unknown tool %q", typed)
			}
			return map[string]any{"type": "tool", "name": alias}, true, nil
		}
	case map[string]any:
		choiceType := strings.TrimSpace(firstCodexGatewayToolString(typed["type"]))
		if strings.EqualFold(choiceType, "auto") {
			return nil, false, nil
		}
		if choiceType == "none" {
			return map[string]any{"type": "none"}, true, nil
		}
		if choiceType == "required" {
			return map[string]any{"type": "any"}, true, nil
		}
		if choiceType == "function" || choiceType == CodexGatewayToolKindNamespace || choiceType == CodexGatewayToolKindCustom {
			name := strings.TrimSpace(firstCodexGatewayToolString(typed["name"]))
			if name == "" {
				if fn, ok := typed["function"].(map[string]any); ok {
					name = strings.TrimSpace(firstCodexGatewayToolString(fn["name"]))
				}
			}
			if choiceType == "function" && strings.Contains(name, ".") {
				if alias, err := resolveCodexGatewayAnthropicToolAlias(toolMapping, name); err != nil {
					return nil, false, err
				} else if alias != "" {
					return map[string]any{"type": "tool", "name": alias}, true, nil
				}
			}
			alias, err := resolveCodexGatewayToolChoiceAlias(toolMapping, choiceType, name)
			if err != nil {
				return nil, false, err
			}
			return map[string]any{"type": "tool", "name": alias}, true, nil
		}
		return nil, false, fmt.Errorf("tool_choice has unsupported type %q", choiceType)
	default:
		return nil, false, nil
	}
}

func codexGatewayAnthropicToolChoiceDisablesThinking(choice any) bool {
	m, ok := choice.(map[string]any)
	if !ok {
		return false
	}
	switch strings.TrimSpace(firstCodexGatewayToolString(m["type"])) {
	case "any", "tool":
		return true
	default:
		return false
	}
}

func codexGatewayAnthropicThinkingConfig(raw json.RawMessage, model CodexGatewayModel) (map[string]any, map[string]any) {
	effort := strings.TrimSpace(model.DefaultReasoningLevel)
	if effort == "" {
		effort = "none"
	}
	if len(raw) > 0 {
		var parsed map[string]any
		if err := json.Unmarshal(raw, &parsed); err == nil {
			if rawEffort, ok := parsed["effort"].(string); ok && strings.TrimSpace(rawEffort) != "" {
				effort = strings.TrimSpace(strings.ToLower(rawEffort))
			}
		}
	}
	if !strings.Contains(strings.ToLower(model.Slug), "thinking") {
		return map[string]any{"type": "disabled"}, nil
	}
	switch effort {
	case "none", "minimal":
		return map[string]any{"type": "disabled"}, nil
	case "low", "medium", "high":
		return map[string]any{"type": "adaptive"}, map[string]any{"effort": effort}
	case "xhigh", "max":
		return map[string]any{"type": "adaptive"}, map[string]any{"effort": "max"}
	default:
		if strings.Contains(strings.ToLower(model.Slug), "thinking") {
			return map[string]any{"type": "adaptive"}, map[string]any{"effort": "max"}
		}
		return map[string]any{"type": "disabled"}, nil
	}
}

func codexGatewayAnthropicReasoningEffort(raw json.RawMessage, model CodexGatewayModel) string {
	effort := strings.TrimSpace(strings.ToLower(model.DefaultReasoningLevel))
	if effort == "" {
		effort = "high"
	}
	if len(raw) == 0 {
		return effort
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return effort
	}
	rawEffort, _ := parsed["effort"].(string)
	rawEffort = strings.TrimSpace(strings.ToLower(rawEffort))
	if rawEffort == "" {
		return effort
	}
	return rawEffort
}

func codexGatewayAnthropicResolveUpstreamModel(raw json.RawMessage, model CodexGatewayModel) string {
	fallback := strings.TrimSpace(model.UpstreamModel)
	if fallback == "" {
		fallback = strings.TrimSpace(model.Slug)
	}
	return fallback
}

func codexGatewayAnthropicStateModelKey(model CodexGatewayModel, upstreamModel string) string {
	if strings.TrimSpace(model.Slug) != "" {
		return strings.TrimSpace(model.Slug)
	}
	return strings.TrimSpace(upstreamModel)
}

func resolveCodexGatewayAnthropicToolAlias(mapping CodexGatewayToolMappingResult, name string) (string, error) {
	alias, err := resolveCodexGatewayToolAlias(mapping, name)
	if err != nil || alias != "" {
		return alias, err
	}
	if strings.Contains(name, ".") {
		return resolveCodexGatewayToolAlias(mapping, strings.ReplaceAll(name, ".", "__"))
	}
	return "", nil
}

func sortedCodexGatewayAnthropicToolAliases(mapping map[string]CodexGatewayToolNameMapEntry) []string {
	aliases := make([]string, 0, len(mapping))
	for alias := range mapping {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	return aliases
}

func codexGatewayAnthropicAssistantMessageFromState(state CodexGatewayResponseState) map[string]any {
	content := make([]any, 0, 1+len(state.ToolCalls))
	for _, raw := range state.AnthropicThinkingBlocks {
		if len(raw) == 0 {
			continue
		}
		var block map[string]any
		if err := json.Unmarshal(raw, &block); err != nil {
			continue
		}
		switch strings.TrimSpace(firstCodexGatewayToolString(block["type"])) {
		case "thinking", "redacted_thinking":
			content = append(content, block)
		}
	}
	if state.AssistantContentPresent {
		content = append(content, map[string]any{
			"type": "text",
			"text": state.AssistantContent,
		})
	}
	for _, call := range state.ToolCalls {
		args := strings.TrimSpace(call.Arguments)
		if args == "" {
			args = "{}"
		}
		content = append(content, map[string]any{
			"type":  "tool_use",
			"id":    call.ID,
			"name":  codexGatewayAnthropicStoredToolCallAlias(call, state.ToolNameMap),
			"input": codexGatewayAnthropicToolInputRawMessage(args),
		})
	}
	if len(content) == 0 {
		content = append(content, map[string]any{"type": "text", "text": ""})
	}
	return map[string]any{
		"role":    "assistant",
		"content": content,
	}
}

func codexGatewayAnthropicMessagesFromState(state CodexGatewayResponseState) ([]any, error) {
	if len(state.ReplayMessages) > 0 {
		out := make([]any, 0, len(state.ReplayMessages))
		for _, raw := range state.ReplayMessages {
			if len(raw) == 0 {
				continue
			}
			var msg any
			if err := json.Unmarshal(raw, &msg); err != nil {
				return nil, fmt.Errorf("%w: invalid anthropic replay message", ErrCodexGatewayStateInvalid)
			}
			out = append(out, msg)
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	return []any{codexGatewayAnthropicAssistantMessageFromState(state)}, nil
}

func codexGatewayAnthropicToolInputRawMessage(args string) json.RawMessage {
	args = strings.TrimSpace(args)
	if args == "" {
		return json.RawMessage(`{}`)
	}
	if json.Valid([]byte(args)) {
		var value any
		if err := json.Unmarshal([]byte(args), &value); err == nil {
			if _, ok := value.(map[string]any); ok {
				return json.RawMessage(args)
			}
			if raw, err := json.Marshal(map[string]any{"value": value}); err == nil {
				return raw
			}
		}
	}
	raw, err := json.Marshal(map[string]any{"text": args})
	if err != nil {
		return json.RawMessage(`{"text":""}`)
	}
	return raw
}

func codexGatewayAnthropicRawMessages(messages []any) []json.RawMessage {
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

func codexGatewayAnthropicStoredToolCallAlias(call CodexGatewayStoredToolCall, toolNameMap map[string]CodexGatewayToolNameMapEntry) string {
	if alias := strings.TrimSpace(call.Alias); alias != "" {
		return alias
	}
	name := strings.TrimSpace(call.Name)
	if len(toolNameMap) == 0 {
		return name
	}
	matches := make([]string, 0, 1)
	for alias, entry := range toolNameMap {
		if strings.EqualFold(strings.TrimSpace(entry.Name), name) {
			matches = append(matches, alias)
		}
	}
	if len(matches) == 1 {
		return matches[0]
	}
	return name
}
