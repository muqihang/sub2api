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

const codexGatewayDeepSeekComputerUseInstruction = "Computer Use strategy: when operating local apps, prefer bundle identifier values from list_apps (for example com.vendor.App) over localized display names; localized display names may be invalid app arguments. For Electron/chat apps, after get_app_state exposes a settable text input, prefer set_value on that element, then press_key Return, then get_app_state to read visible_text or operable_lines. If an element ID is stale, refresh with get_app_state once and retry with the new element_index. Avoid scrolling or blind clicking unless visible_text/operable_lines show that the needed reply or control is off-screen."

const (
	codexGatewayDeepSeekToolOutputMaxChars           = 3500
	codexGatewayDeepSeekToolOutputStringPreviewChars = 1200
	codexGatewayDeepSeekToolOutputFieldPreviewChars  = 512
	codexGatewayDeepSeekToolOutputMaxArrayItems      = 24
)

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
	toolMapping, err = codexGatewayDeepSeekMergeDeferredToolSearchOutputTools(toolMapping, req.Input, toolCfg)
	if err != nil {
		return CodexGatewayPreparedDeepSeekRequest{}, err
	}
	toolMapping = codexGatewayDeepSeekRestrictHostedToolMapping(toolMapping)
	toolMapping = codexGatewayDeepSeekAdaptToolMapping(toolMapping, toolCfg)
	var toolSchemas []json.RawMessage
	if len(toolMapping.Tools) > 0 {
		tools := make([]any, 0, len(toolMapping.Tools))
		for _, tool := range toolMapping.Tools {
			tools = append(tools, tool)
		}
		body["tools"] = tools
		toolSchemas = codexGatewayRawToolSchemas(tools)
	} else if stateStore != nil && codexGatewayDeepSeekShouldRestoreReplayToolContext(req) {
		if state, err := stateStore.Get(CodexGatewayStateLookupKey{
			ResponseID:    strings.TrimSpace(*req.PreviousResponseID),
			SessionKey:    ctx.SessionKey,
			IsolationKey:  ctx.IsolationKey,
			Provider:      "deepseek",
			UpstreamModel: upstreamModel,
		}); err == nil && len(state.ToolCalls) > 0 {
			if restored := codexGatewayToolSchemasToAny(state.ToolSchemas); len(restored) > 0 {
				body["tools"] = restored
				toolSchemas = cloneCodexGatewayRawMessages(state.ToolSchemas)
				if len(toolMapping.NameMap) == 0 {
					toolMapping.NameMap = cloneCodexGatewayToolNameMap(state.ToolNameMap)
				}
			}
		}
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
		if codexGatewayDeepSeekToolMappingHasComputerUse(toolMapping) && codexGatewayDeepSeekMessagesHaveUserTurn(messages) && !codexGatewayDeepSeekSystemPrefixHasContent(messages, codexGatewayDeepSeekComputerUseInstruction) {
			leadingMessages = append(leadingMessages, map[string]any{
				"role":    "system",
				"content": codexGatewayDeepSeekComputerUseInstruction,
			})
		}
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
	body = codexGatewayDeepSeekFinalizeChatCompletionsBody(body)
	if ctx.CaptureTrace != nil && ctx.CaptureTrace.manager != nil {
		requestDiagnostics := map[string]any{}
		if diagnostics := codexGatewayDeepSeekCaptureDiagnostics(body, userID, userIDDiag, replayDiag, ctx.CaptureTrace.manager.redact); len(diagnostics) > 0 {
			requestDiagnostics["deepseek_cache"] = diagnostics
			if cacheUsage := codexGatewayDeepSeekCacheUsageFields(diagnostics); len(cacheUsage) > 0 {
				ctx.CaptureTrace.manager.mergeCacheUsage(ctx.CaptureTrace, cacheUsage)
			}
		}
		if diagnostics := codexGatewayDeepSeekToolOutputSummaryDiagnostics(body); len(diagnostics) > 0 {
			requestDiagnostics["deepseek_tool_output_summary"] = diagnostics
		}
		ctx.CaptureTrace.manager.mergeRequestDiagnostics(ctx.CaptureTrace, requestDiagnostics)
	}

	return CodexGatewayPreparedDeepSeekRequest{
		Body:           body,
		ToolNameMap:    toolMapping.NameMap,
		ToolSchemas:    toolSchemas,
		ReplayMessages: codexGatewayDeepSeekRawMessages(messages),
	}, nil
}

func codexGatewayRawToolSchemas(tools []any) []json.RawMessage {
	if len(tools) == 0 {
		return nil
	}
	out := make([]json.RawMessage, 0, len(tools))
	for _, tool := range tools {
		raw, err := json.Marshal(canonicalizeCodexGatewayToolSchema(tool))
		if err != nil || len(raw) == 0 {
			continue
		}
		out = append(out, raw)
	}
	return out
}

func codexGatewayToolSchemasToAny(rawSchemas []json.RawMessage) []any {
	if len(rawSchemas) == 0 {
		return nil
	}
	out := make([]any, 0, len(rawSchemas))
	for _, raw := range rawSchemas {
		if len(raw) == 0 {
			continue
		}
		var schema any
		if err := json.Unmarshal(raw, &schema); err != nil {
			continue
		}
		out = append(out, schema)
	}
	return out
}

func codexGatewayDeepSeekShouldRestoreReplayToolContext(req CodexGatewayResponsesCreateRequest) bool {
	if req.PreviousResponseID == nil || strings.TrimSpace(*req.PreviousResponseID) == "" {
		return false
	}
	items, err := decodeCodexGatewayInputItems(req.Input)
	if err != nil {
		return false
	}
	return codexGatewayInputHasToolCallOutput(items)
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

func codexGatewayDeepSeekMessagesHaveUserTurn(messages []any) bool {
	for _, msg := range messages {
		m, ok := msg.(map[string]any)
		if !ok {
			continue
		}
		if strings.TrimSpace(firstCodexGatewayToolString(m["role"])) == "user" {
			return true
		}
	}
	return false
}

func codexGatewayDeepSeekToolMappingHasComputerUse(mapping CodexGatewayToolMappingResult) bool {
	for _, entry := range mapping.NameMap {
		if codexGatewayDeepSeekIsComputerUseToolIdentity(
			entry.Name,
			entry.Alias,
			firstCodexGatewayToolString(entry.Namespace, entry.NamespacePath),
		) {
			return true
		}
	}
	return false
}

func codexGatewayDeepSeekMergeDeferredToolSearchOutputTools(mapping CodexGatewayToolMappingResult, input json.RawMessage, cfg CodexGatewayToolMappingConfig) (CodexGatewayToolMappingResult, error) {
	if len(input) == 0 {
		return mapping, nil
	}
	items, err := decodeCodexGatewayInputItems(input)
	if err != nil {
		return CodexGatewayToolMappingResult{}, err
	}
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok || strings.TrimSpace(firstCodexGatewayToolString(m["type"])) != "tool_search_output" {
			continue
		}
		toolsValue, ok := codexGatewayDeepSeekDeferredToolsValue(m)
		if !ok {
			continue
		}
		rawTools, err := json.Marshal(toolsValue)
		if err != nil {
			return CodexGatewayToolMappingResult{}, fmt.Errorf("encode tool_search_output tools: %w", err)
		}
		deferred, err := BuildCodexGatewayToolMapping(rawTools, cfg)
		if err != nil {
			return CodexGatewayToolMappingResult{}, fmt.Errorf("map tool_search_output tools: %w", err)
		}
		mapping, err = mergeCodexGatewayToolMappings(mapping, deferred)
		if err != nil {
			return CodexGatewayToolMappingResult{}, err
		}
	}
	return mapping, nil
}

func codexGatewayDeepSeekDeferredToolsValue(item map[string]any) (any, bool) {
	if item == nil {
		return nil, false
	}
	if tools, ok := item["tools"]; ok && codexGatewayDeepSeekDeferredToolsValueIsNonEmptyArray(tools) {
		return tools, true
	}
	output := firstCodexGatewayToolValue(item["output"])
	switch typed := output.(type) {
	case string:
		var parsed any
		if err := json.Unmarshal([]byte(strings.TrimSpace(typed)), &parsed); err != nil {
			return nil, false
		}
		if codexGatewayDeepSeekDeferredToolsValueIsNonEmptyArray(parsed) {
			return parsed, true
		}
		if obj, ok := parsed.(map[string]any); ok && codexGatewayDeepSeekDeferredToolsValueIsNonEmptyArray(obj["tools"]) {
			return obj["tools"], true
		}
	case map[string]any:
		if codexGatewayDeepSeekDeferredToolsValueIsNonEmptyArray(typed["tools"]) {
			return typed["tools"], true
		}
	case []any:
		if len(typed) > 0 {
			return typed, true
		}
	}
	return nil, false
}

func codexGatewayDeepSeekDeferredToolsValueIsNonEmptyArray(value any) bool {
	arr, ok := value.([]any)
	return ok && len(arr) > 0
}

func mergeCodexGatewayToolMappings(base, extra CodexGatewayToolMappingResult) (CodexGatewayToolMappingResult, error) {
	if base.NameMap == nil {
		base.NameMap = make(map[string]CodexGatewayToolNameMapEntry, len(extra.NameMap))
	}
	if base.originalToAlias == nil {
		base.originalToAlias = make(map[string]string, len(extra.originalToAlias))
	}
	existingAliases := make(map[string]struct{}, len(base.NameMap))
	for alias := range base.NameMap {
		existingAliases[alias] = struct{}{}
	}
	for alias, entry := range extra.NameMap {
		if existing, ok := base.NameMap[alias]; ok {
			if !codexGatewayToolNameMapEntriesEqual(existing, entry) {
				return CodexGatewayToolMappingResult{}, fmt.Errorf("deferred tool alias collision for %q", alias)
			}
			continue
		}
		base.NameMap[alias] = entry
	}
	for _, tool := range extra.Tools {
		function, _ := tool["function"].(map[string]any)
		alias := strings.TrimSpace(firstCodexGatewayToolString(function["name"]))
		if alias == "" {
			continue
		}
		if _, existed := existingAliases[alias]; existed {
			continue
		}
		base.Tools = append(base.Tools, tool)
		existingAliases[alias] = struct{}{}
	}
	for key, alias := range extra.originalToAlias {
		if existing, ok := base.originalToAlias[key]; ok && existing != alias {
			return CodexGatewayToolMappingResult{}, fmt.Errorf("deferred tool original path collision for %q", key)
		}
		base.originalToAlias[key] = alias
	}
	base.IgnoredHostedToolTypes = uniqueCodexGatewayStrings(append(base.IgnoredHostedToolTypes, extra.IgnoredHostedToolTypes...))
	return base, nil
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

func codexGatewayDeepSeekFinalizeChatCompletionsBody(body map[string]any) map[string]any {
	body = codexGatewayDeepSeekAllowlistedChatCompletionsBody(body)
	if tools, ok := body["tools"]; ok {
		body["tools"] = canonicalizeCodexGatewayToolSchema(tools)
	}
	return body
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
		case "function_call_output", "local_shell_call_output", "custom_tool_call_output", "tool_search_output":
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
	case "tool_search_call":
		return convertCodexGatewayToolSearchCallItem(m, toolMapping)
	case "local_shell_call":
		return convertCodexGatewayFunctionCallItem(m, toolMapping)
	case "custom_tool_call":
		return convertCodexGatewayCustomToolCallItem(m, toolMapping)
	case "function_call_output":
		return convertCodexGatewayFunctionCallOutputItem(m)
	case "tool_search_output":
		return convertCodexGatewayToolSearchOutputItem(m)
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

func convertCodexGatewayToolSearchCallItem(m map[string]any, toolMapping CodexGatewayToolMappingResult) (map[string]any, []string, error) {
	wrapped := make(map[string]any, len(m)+2)
	for key, value := range m {
		wrapped[key] = value
	}
	wrapped["type"] = CodexGatewayOutputItemTypeFunctionCall
	wrapped["name"] = codexGatewayToolSearchType
	return convertCodexGatewayFunctionCallItem(wrapped, toolMapping)
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
	output, err := normalizeCodexGatewayDeepSeekToolOutput(m["output"])
	if err != nil {
		return nil, nil, err
	}
	return map[string]any{
		"role":         "tool",
		"tool_call_id": callID,
		"content":      output,
	}, nil, nil
}

func convertCodexGatewayToolSearchOutputItem(m map[string]any) (map[string]any, []string, error) {
	callID := strings.TrimSpace(firstCodexGatewayToolString(m["call_id"], m["tool_call_id"], m["id"]))
	if callID == "" {
		return nil, nil, fmt.Errorf("tool_search_output requires call_id")
	}
	var output string
	var err error
	if _, hasOutput := m["output"]; hasOutput {
		output, err = normalizeCodexGatewayDeepSeekToolOutput(m["output"])
	} else {
		output, err = normalizeCodexGatewayToolOutput(canonicalizeCodexGatewayToolSchema(m["tools"]))
	}
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

func normalizeCodexGatewayDeepSeekToolOutput(value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", nil
	case string:
		if parsed, ok := codexGatewayDeepSeekParseStructuredToolOutputString(typed); ok {
			return normalizeCodexGatewayDeepSeekToolOutput(parsed)
		}
		if codexGatewayDeepSeekLooksLikeStandaloneVisualTree(typed) {
			return codexGatewayDeepSeekMarshalToolOutputSummary(codexGatewayDeepSeekSummarizeAccessibilityTree("tool_output", typed))
		}
		if codexGatewayDeepSeekIsBinaryLikeToolField("", typed) {
			return codexGatewayDeepSeekMarshalToolOutputSummary(codexGatewayDeepSeekSummarizeBinaryToolField("", typed))
		}
		return typed, nil
	default:
		summarized, changed := codexGatewayDeepSeekSummarizeToolOutputValue("", typed, 0)
		raw, err := json.Marshal(summarized)
		if err != nil {
			return "", err
		}
		out := string(raw)
		if !changed || len(out) <= codexGatewayDeepSeekToolOutputMaxChars {
			return out, nil
		}
		if compacted, ok := codexGatewayDeepSeekCompactSemanticToolSummary(summarized); ok {
			return codexGatewayDeepSeekMarshalSemanticToolOutputSummary(compacted)
		}
		return codexGatewayDeepSeekMarshalToolOutputSummary(map[string]any{
			"truncated":      true,
			"original_chars": len(out),
			"sha256":         codexGatewayDeepSeekTextSHA256(out),
			"preview":        codexGatewayDeepSeekTruncateString(out, codexGatewayDeepSeekToolOutputStringPreviewChars),
		})
	}
}

func codexGatewayDeepSeekCompactSemanticToolSummary(value any) (any, bool) {
	if !codexGatewayDeepSeekContainsSemanticToolSummary(value) {
		return nil, false
	}
	compacted := codexGatewayDeepSeekCompactSemanticToolSummaryValue(value, 0)
	return compacted, true
}

func codexGatewayDeepSeekMarshalSemanticToolOutputSummary(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	if len(raw) <= codexGatewayDeepSeekToolOutputMaxChars {
		return string(raw), nil
	}
	pruned := codexGatewayDeepSeekPruneSemanticToolSummary(value)
	raw, err = json.Marshal(pruned)
	if err != nil {
		return "", err
	}
	if len(raw) <= codexGatewayDeepSeekToolOutputMaxChars {
		return string(raw), nil
	}
	return codexGatewayDeepSeekMarshalToolOutputSummary(pruned)
}

func codexGatewayDeepSeekPruneSemanticToolSummary(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		if class := strings.TrimSpace(firstCodexGatewayToolString(typed["content_class"])); class != "" {
			return codexGatewayDeepSeekCompactSemanticToolSummaryMapWithBudget(typed, class, 160, 4, 120)
		}
		out := make(map[string]any)
		keys := make([]string, 0, len(typed))
		for key, raw := range typed {
			if codexGatewayDeepSeekContainsSemanticToolSummary(raw) {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			out[key] = codexGatewayDeepSeekPruneSemanticToolSummary(typed[key])
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, raw := range typed {
			if codexGatewayDeepSeekContainsSemanticToolSummary(raw) {
				out = append(out, codexGatewayDeepSeekPruneSemanticToolSummary(raw))
			}
		}
		return out
	default:
		return typed
	}
}

func codexGatewayDeepSeekContainsSemanticToolSummary(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		if class := strings.TrimSpace(firstCodexGatewayToolString(typed["content_class"])); class != "" {
			switch class {
			case "computer_screenshot", "binary_or_image", "accessibility_tree", "visual_tree":
				return true
			}
		}
		for _, raw := range typed {
			if codexGatewayDeepSeekContainsSemanticToolSummary(raw) {
				return true
			}
		}
	case []any:
		for _, raw := range typed {
			if codexGatewayDeepSeekContainsSemanticToolSummary(raw) {
				return true
			}
		}
	}
	return false
}

func codexGatewayDeepSeekCompactSemanticToolSummaryValue(value any, depth int) any {
	if depth > 8 {
		return map[string]any{"truncated": true, "reason": "max_depth"}
	}
	switch typed := value.(type) {
	case map[string]any:
		class := strings.TrimSpace(firstCodexGatewayToolString(typed["content_class"]))
		if class != "" {
			return codexGatewayDeepSeekCompactSemanticToolSummaryMap(typed, class)
		}
		out := make(map[string]any, len(typed))
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			out[key] = codexGatewayDeepSeekCompactSemanticToolSummaryValue(typed[key], depth+1)
		}
		return out
	case []any:
		limit := len(typed)
		truncated := false
		if limit > codexGatewayDeepSeekToolOutputMaxArrayItems {
			limit = codexGatewayDeepSeekToolOutputMaxArrayItems
			truncated = true
		}
		out := make([]any, 0, limit)
		for i := 0; i < limit; i++ {
			out = append(out, codexGatewayDeepSeekCompactSemanticToolSummaryValue(typed[i], depth+1))
		}
		if truncated {
			return map[string]any{
				"items":          out,
				"truncated":      true,
				"original_items": len(typed),
			}
		}
		return out
	default:
		return typed
	}
}

func codexGatewayDeepSeekCompactSemanticToolSummaryMap(in map[string]any, class string) map[string]any {
	return codexGatewayDeepSeekCompactSemanticToolSummaryMapWithBudget(in, class, 320, 8, 180)
}

func codexGatewayDeepSeekCompactSemanticToolSummaryMapWithBudget(in map[string]any, class string, textChars, maxLines, lineChars int) map[string]any {
	out := make(map[string]any, len(in))
	for _, key := range []string{"content_class", "field", "truncated", "original_chars", "sha256", "media_type", "extraction_mode"} {
		if value, ok := in[key]; ok {
			out[key] = value
		}
	}
	if value, ok := in["vision_summary"]; ok {
		out["vision_summary"] = codexGatewayDeepSeekTruncateString(firstCodexGatewayToolString(value), textChars)
	}
	if value, ok := in["preview"]; ok {
		out["preview"] = codexGatewayDeepSeekTruncateString(firstCodexGatewayToolString(value), textChars)
	}
	if rawLines, ok := in["operable_lines"]; ok {
		if lines := codexGatewayDeepSeekCompactOperableLines(rawLines, maxLines, lineChars); len(lines) > 0 {
			out["operable_lines"] = lines
		}
	}
	if rawLines, ok := in["visible_text"]; ok {
		visibleLines := maxLines
		if visibleLines > 4 {
			visibleLines = 4
		} else if visibleLines > 2 {
			visibleLines = 2
		}
		if lines := codexGatewayDeepSeekCompactOperableLines(rawLines, visibleLines, lineChars); len(lines) > 0 {
			out["visible_text"] = lines
		}
	}
	return out
}

func codexGatewayDeepSeekCompactOperableLines(value any, maxLines, lineChars int) []string {
	if maxLines <= 0 {
		maxLines = 8
	}
	if lineChars <= 0 {
		lineChars = 180
	}
	switch typed := value.(type) {
	case []string:
		limit := len(typed)
		if limit > maxLines {
			limit = maxLines
		}
		out := make([]string, 0, limit)
		for i := 0; i < limit; i++ {
			line := strings.TrimSpace(typed[i])
			if line == "" {
				continue
			}
			out = append(out, codexGatewayDeepSeekTruncateString(line, lineChars))
		}
		return out
	case []any:
		limit := len(typed)
		if limit > maxLines {
			limit = maxLines
		}
		out := make([]string, 0, limit)
		for i := 0; i < limit; i++ {
			line := strings.TrimSpace(firstCodexGatewayToolString(typed[i]))
			if line == "" {
				continue
			}
			out = append(out, codexGatewayDeepSeekTruncateString(line, lineChars))
		}
		return out
	default:
		return nil
	}
}

func codexGatewayDeepSeekParseStructuredToolOutputString(value string) (any, bool) {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) == 0 {
		return nil, false
	}
	first := trimmed[0]
	if first != '{' && first != '[' {
		return nil, false
	}
	var parsed any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return nil, false
	}
	if !codexGatewayDeepSeekStructuredToolOutputNeedsSummary(parsed) {
		return nil, false
	}
	return parsed, true
}

func codexGatewayDeepSeekStructuredToolOutputNeedsSummary(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, raw := range typed {
			if codexGatewayDeepSeekStructuredVisualStateNeedsSummary(key, raw) {
				return true
			}
			if str, ok := raw.(string); ok {
				if codexGatewayDeepSeekIsBinaryLikeToolField(key, str) || codexGatewayDeepSeekShouldSummarizeToolString(key, str) {
					return true
				}
			}
			if codexGatewayDeepSeekStructuredToolOutputNeedsSummary(raw) {
				return true
			}
		}
	case []any:
		for _, raw := range typed {
			if codexGatewayDeepSeekStructuredToolOutputNeedsSummary(raw) {
				return true
			}
		}
	}
	return false
}

func codexGatewayDeepSeekStructuredVisualStateNeedsSummary(field string, value any) bool {
	field = strings.ToLower(strings.TrimSpace(field))
	if field == "" || value == nil {
		return false
	}
	if !codexGatewayDeepSeekIsAccessibilityTreeField(field) &&
		!codexGatewayDeepSeekIsPageTreeField(field) &&
		!codexGatewayDeepSeekIsDOMLikeField(field) &&
		!strings.Contains(field, "html") &&
		!strings.Contains(field, "page_content") &&
		!strings.Contains(field, "page_source") &&
		!strings.Contains(field, "snapshot") {
		return false
	}
	if _, ok := value.(string); ok {
		return false
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return false
	}
	return len(raw) > codexGatewayDeepSeekToolOutputFieldPreviewChars
}

func codexGatewayDeepSeekSummarizeToolOutputValue(field string, value any, depth int) (any, bool) {
	if depth > 8 {
		return map[string]any{
			"truncated": true,
			"reason":    "max_depth",
		}, true
	}
	if typed, ok := value.(map[string]any); ok {
		if class := strings.TrimSpace(firstCodexGatewayToolString(typed["content_class"])); class != "" {
			return codexGatewayDeepSeekCompactSemanticToolSummaryMap(typed, class), true
		}
	}
	if summary, ok := codexGatewayDeepSeekSummarizeStructuredVisualState(field, value); ok {
		return summary, true
	}
	switch typed := value.(type) {
	case nil, bool, float64, float32, int, int64, int32, uint, uint64, uint32:
		return typed, false
	case string:
		if codexGatewayDeepSeekLooksLikeStandaloneVisualTree(typed) {
			return codexGatewayDeepSeekSummarizeAccessibilityTree(firstCodexGatewayToolString(field, "tool_output"), typed), true
		}
		if codexGatewayDeepSeekIsBinaryLikeToolField(field, typed) {
			return codexGatewayDeepSeekSummarizeBinaryToolField(field, typed), true
		}
		if codexGatewayDeepSeekIsAccessibilityTreeField(field) && codexGatewayDeepSeekShouldSummarizeToolString(field, typed) {
			return codexGatewayDeepSeekSummarizeAccessibilityTree(field, typed), true
		}
		if codexGatewayDeepSeekShouldSummarizeToolString(field, typed) {
			return codexGatewayDeepSeekSummarizeToolString(field, typed, codexGatewayDeepSeekToolOutputFieldPreviewChars), true
		}
		return typed, false
	case map[string]any:
		out := make(map[string]any, len(typed))
		changed := false
		for key, raw := range typed {
			summarized, fieldChanged := codexGatewayDeepSeekSummarizeToolOutputValue(key, raw, depth+1)
			out[key] = summarized
			changed = changed || fieldChanged
		}
		return out, changed
	case []any:
		limit := len(typed)
		truncated := false
		if field != "" && codexGatewayDeepSeekShouldSummarizeToolString(field, "") && limit > codexGatewayDeepSeekToolOutputMaxArrayItems {
			limit = codexGatewayDeepSeekToolOutputMaxArrayItems
			truncated = true
		}
		out := make([]any, 0, limit)
		changed := truncated
		for i := 0; i < limit; i++ {
			summarized, itemChanged := codexGatewayDeepSeekSummarizeToolOutputValue(field, typed[i], depth+1)
			out = append(out, summarized)
			changed = changed || itemChanged
		}
		if truncated {
			return map[string]any{
				"items":          out,
				"truncated":      true,
				"original_items": len(typed),
			}, true
		}
		return out, changed
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed), false
		}
		text := string(raw)
		if codexGatewayDeepSeekShouldSummarizeToolString(field, text) {
			return codexGatewayDeepSeekSummarizeToolString(field, text, codexGatewayDeepSeekToolOutputFieldPreviewChars), true
		}
		var normalized any
		if err := json.Unmarshal(raw, &normalized); err == nil {
			return normalized, false
		}
		return text, false
	}
}

func codexGatewayDeepSeekShouldSummarizeToolString(field, value string) bool {
	field = strings.ToLower(strings.TrimSpace(field))
	if field == "" {
		return false
	}
	if codexGatewayDeepSeekIsAccessibilityTreeField(field) {
		return len(value) > codexGatewayDeepSeekToolOutputFieldPreviewChars
	}
	if codexGatewayDeepSeekIsPageTreeField(field) ||
		codexGatewayDeepSeekIsDOMLikeField(field) ||
		strings.Contains(field, "html") ||
		strings.Contains(field, "page_content") ||
		strings.Contains(field, "page_source") ||
		strings.Contains(field, "snapshot") {
		return len(value) > codexGatewayDeepSeekToolOutputFieldPreviewChars
	}
	return false
}

func codexGatewayDeepSeekSummarizeStructuredVisualState(field string, value any) (any, bool) {
	field = strings.ToLower(strings.TrimSpace(field))
	if field == "" || value == nil {
		return nil, false
	}
	isAccessibility := codexGatewayDeepSeekIsAccessibilityTreeField(field)
	isVisualTree := codexGatewayDeepSeekIsPageTreeField(field) ||
		codexGatewayDeepSeekIsDOMLikeField(field) ||
		strings.Contains(field, "html") ||
		strings.Contains(field, "page_content") ||
		strings.Contains(field, "page_source") ||
		strings.Contains(field, "snapshot")
	if !isAccessibility && !isVisualTree {
		return nil, false
	}
	if _, ok := value.(string); ok {
		return nil, false
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	text := string(raw)
	if len(text) <= codexGatewayDeepSeekToolOutputFieldPreviewChars {
		return nil, false
	}
	if lines := codexGatewayDeepSeekStructuredOperableLines(value); len(lines) > 0 {
		contentClass := "visual_tree"
		if isAccessibility {
			contentClass = "accessibility_tree"
		}
		return map[string]any{
			"content_class":   contentClass,
			"field":           field,
			"truncated":       true,
			"original_chars":  len(text),
			"sha256":          codexGatewayDeepSeekTextSHA256(text),
			"operable_lines":  lines,
			"extraction_mode": "structured_fields",
		}, true
	}
	if isAccessibility {
		return codexGatewayDeepSeekSummarizeAccessibilityTree(field, text), true
	}
	return codexGatewayDeepSeekSummarizeVisualTree(field, text), true
}

func codexGatewayDeepSeekStructuredOperableLines(value any) []string {
	candidates := make([]codexGatewayDeepSeekAccessibilityLineCandidate, 0, 16)
	seen := make(map[string]struct{}, 16)
	index := 0
	codexGatewayDeepSeekCollectStructuredOperableLines(value, &candidates, seen, &index)
	if len(candidates) == 0 {
		return nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].priority != candidates[j].priority {
			return candidates[i].priority > candidates[j].priority
		}
		return candidates[i].index < candidates[j].index
	})
	limit := len(candidates)
	if limit > 8 {
		limit = 8
	}
	selected := append([]codexGatewayDeepSeekAccessibilityLineCandidate(nil), candidates[:limit]...)
	sort.SliceStable(selected, func(i, j int) bool {
		return selected[i].index < selected[j].index
	})
	out := make([]string, 0, len(selected))
	for _, candidate := range selected {
		out = append(out, candidate.line)
	}
	return out
}

func codexGatewayDeepSeekCollectStructuredOperableLines(value any, out *[]codexGatewayDeepSeekAccessibilityLineCandidate, seen map[string]struct{}, index *int) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			codexGatewayDeepSeekCollectStructuredOperableLines(item, out, seen, index)
		}
	case map[string]any:
		if line := codexGatewayDeepSeekStructuredNodeLine(typed); line != "" {
			if _, ok := seen[line]; !ok {
				seen[line] = struct{}{}
				*out = append(*out, codexGatewayDeepSeekAccessibilityLineCandidate{
					index:    *index,
					line:     line,
					priority: codexGatewayDeepSeekAccessibilityOperableLinePriority(line),
				})
			}
		}
		*index++
		for _, key := range []string{"children", "nodes", "items", "elements", "tree"} {
			if child, ok := typed[key]; ok {
				codexGatewayDeepSeekCollectStructuredOperableLines(child, out, seen, index)
			}
		}
	}
}

func codexGatewayDeepSeekStructuredNodeLine(node map[string]any) string {
	role := firstCodexGatewayToolString(
		node["role"],
		node["type"],
		node["kind"],
		node["tag"],
	)
	name := firstCodexGatewayToolString(
		node["name"],
		node["label"],
		node["title"],
		node["text"],
		node["value"],
		node["placeholder"],
	)
	stateParts := make([]string, 0, 4)
	for _, key := range []string{"enabled", "disabled", "focused", "selected", "checked", "pressed", "visible", "settable"} {
		if state, ok := codexGatewayDeepSeekStructuredBoolState(node, key); ok {
			stateParts = append(stateParts, fmt.Sprintf("%s=%t", key, state))
		}
	}
	locatorParts := make([]string, 0, 4)
	for _, key := range []string{"element_index", "elementIndex", "id", "index"} {
		if value := strings.TrimSpace(firstCodexGatewayToolString(node[key])); value != "" {
			locatorParts = append(locatorParts, fmt.Sprintf("%s=%s", key, codexGatewayDeepSeekTruncateString(value, 40)))
		}
	}
	if bounds := strings.TrimSpace(codexGatewayDeepSeekStructuredBounds(node)); bounds != "" {
		locatorParts = append(locatorParts, "bounds="+bounds)
	}
	line := strings.TrimSpace(strings.Join([]string{
		strings.TrimSpace(role),
		strings.TrimSpace(name),
		strings.Join(stateParts, " "),
		strings.Join(locatorParts, " "),
	}, " "))
	if line == "" || !codexGatewayDeepSeekLooksOperableAccessibilityLine(line) {
		return ""
	}
	return codexGatewayDeepSeekTruncateString(line, 180)
}

func codexGatewayDeepSeekStructuredBounds(node map[string]any) string {
	for _, key := range []string{"bounds", "rect", "frame", "bbox"} {
		if value, ok := node[key]; ok {
			if text := strings.TrimSpace(codexGatewayDeepSeekCompactJSONValue(value)); text != "" {
				return codexGatewayDeepSeekTruncateString(text, 80)
			}
		}
	}
	x := firstCodexGatewayToolString(node["x"], node["left"])
	y := firstCodexGatewayToolString(node["y"], node["top"])
	width := firstCodexGatewayToolString(node["width"], node["w"])
	height := firstCodexGatewayToolString(node["height"], node["h"])
	if strings.TrimSpace(x) != "" && strings.TrimSpace(y) != "" {
		parts := []string{"x=" + strings.TrimSpace(x), "y=" + strings.TrimSpace(y)}
		if strings.TrimSpace(width) != "" {
			parts = append(parts, "w="+strings.TrimSpace(width))
		}
		if strings.TrimSpace(height) != "" {
			parts = append(parts, "h="+strings.TrimSpace(height))
		}
		return strings.Join(parts, ",")
	}
	if center, ok := node["center"]; ok {
		return codexGatewayDeepSeekTruncateString(strings.TrimSpace(codexGatewayDeepSeekCompactJSONValue(center)), 80)
	}
	return ""
}

func codexGatewayDeepSeekCompactJSONValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
		return string(raw)
	}
}

func codexGatewayDeepSeekStructuredBoolState(node map[string]any, key string) (bool, bool) {
	raw, ok := node[key]
	if !ok {
		return false, false
	}
	switch typed := raw.(type) {
	case bool:
		return typed, true
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "yes", "enabled", "selected", "focused", "checked", "pressed", "visible":
			return true, true
		case "false", "no", "disabled", "unselected", "unfocused", "unchecked", "hidden":
			return false, true
		}
	}
	return false, false
}

func codexGatewayDeepSeekIsAccessibilityTreeField(field string) bool {
	field = strings.ToLower(strings.TrimSpace(field))
	return strings.Contains(field, "accessibility_tree") ||
		strings.Contains(field, "accessibilitytree") ||
		strings.Contains(field, "accessibility_snapshot") ||
		strings.Contains(field, "accessibilitysnapshot") ||
		strings.Contains(field, "ax_tree") ||
		strings.Contains(field, "ui_tree")
}

func codexGatewayDeepSeekIsPageTreeField(field string) bool {
	field = strings.ToLower(strings.TrimSpace(field))
	return strings.Contains(field, "page_tree") ||
		strings.Contains(field, "pagetree") ||
		strings.Contains(field, "browser_tree") ||
		strings.Contains(field, "browsertree") ||
		strings.Contains(field, "dom_snapshot") ||
		strings.Contains(field, "domsnapshot") ||
		strings.Contains(field, "ui_snapshot") ||
		strings.Contains(field, "uisnapshot")
}

func codexGatewayDeepSeekIsDOMLikeField(field string) bool {
	field = strings.ToLower(strings.TrimSpace(field))
	switch field {
	case "dom", "html":
		return true
	}
	return strings.Contains(field, "dom_snapshot") ||
		strings.Contains(field, "domsnapshot") ||
		strings.Contains(field, "dom_tree") ||
		strings.Contains(field, "domtree") ||
		strings.Contains(field, "dom_nodes") ||
		strings.Contains(field, "domnodes") ||
		strings.Contains(field, "page_dom") ||
		strings.Contains(field, "pagedom")
}

func codexGatewayDeepSeekIsBinaryLikeToolField(field, value string) bool {
	field = strings.ToLower(strings.TrimSpace(field))
	if strings.Contains(value, "data:image/") || strings.Contains(value, ";base64,") {
		return true
	}
	if field == "" {
		return false
	}
	return strings.Contains(field, "screenshot") ||
		strings.Contains(field, "image_base64") ||
		strings.Contains(field, "base64") ||
		strings.Contains(field, "image_url")
}

func codexGatewayDeepSeekLooksLikeStandaloneVisualTree(value string) bool {
	lines := strings.FieldsFunc(value, func(r rune) bool {
		return r == '\n' || r == '\r'
	})
	if len(lines) < 3 {
		return false
	}
	operable := 0
	treeHints := 0
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if codexGatewayDeepSeekLooksOperableAccessibilityLine(line) {
			operable++
		}
		lowered := strings.ToLower(line)
		if strings.Contains(lowered, "ax") ||
			strings.Contains(lowered, "<app_state>") ||
			strings.Contains(lowered, "computer use state") ||
			strings.Contains(lowered, "role=") ||
			strings.Contains(lowered, "children") ||
			strings.Contains(lowered, "bounds") ||
			strings.Contains(lowered, "element_index") ||
			strings.Contains(line, "文本输入区") {
			treeHints++
		}
		if operable >= 2 && treeHints >= 1 {
			return true
		}
	}
	return false
}

func codexGatewayDeepSeekSummarizeVisualTree(field, value string) map[string]any {
	out := map[string]any{
		"content_class":  "visual_tree",
		"field":          field,
		"truncated":      true,
		"original_chars": len(value),
		"sha256":         codexGatewayDeepSeekTextSHA256(value),
	}
	if lines := codexGatewayDeepSeekAccessibilityTreeLines(value); len(lines) > 0 {
		out["operable_lines"] = lines
	}
	if visibleText := codexGatewayDeepSeekAccessibilityVisibleTextLines(value); len(visibleText) > 0 {
		out["visible_text"] = visibleText
	}
	if _, ok := out["operable_lines"]; ok {
		return out
	}
	if _, ok := out["visible_text"]; ok {
		return out
	}
	if preview := codexGatewayDeepSeekAccessibilityPreview(value); preview != "" {
		out["preview"] = preview
	}
	return out
}

func codexGatewayDeepSeekSummarizeBinaryToolField(field, value string) map[string]any {
	summary := map[string]any{
		"content_class":  "binary_or_image",
		"field":          field,
		"truncated":      true,
		"original_chars": len(value),
		"sha256":         codexGatewayDeepSeekTextSHA256(value),
	}
	if strings.HasPrefix(value, "data:image/") {
		if comma := strings.Index(value, ","); comma > 0 {
			summary["media_type"] = value[:comma]
		}
	}
	return summary
}

func codexGatewayDeepSeekSummarizeAccessibilityTree(field, value string) map[string]any {
	out := map[string]any{
		"content_class":  "accessibility_tree",
		"field":          field,
		"truncated":      true,
		"original_chars": len(value),
		"sha256":         codexGatewayDeepSeekTextSHA256(value),
	}
	if lines := codexGatewayDeepSeekAccessibilityTreeLines(value); len(lines) > 0 {
		out["operable_lines"] = lines
	}
	if visibleText := codexGatewayDeepSeekAccessibilityVisibleTextLines(value); len(visibleText) > 0 {
		out["visible_text"] = visibleText
	}
	if _, ok := out["operable_lines"]; ok {
		return out
	}
	if _, ok := out["visible_text"]; ok {
		return out
	}
	if preview := codexGatewayDeepSeekAccessibilityPreview(value); preview != "" {
		out["preview"] = preview
	}
	return out
}

type codexGatewayDeepSeekAccessibilityLineCandidate struct {
	index    int
	line     string
	priority int
}

func codexGatewayDeepSeekAccessibilityTreeLines(value string) []string {
	rawLines := strings.FieldsFunc(value, func(r rune) bool {
		return r == '\n' || r == '\r'
	})
	if len(rawLines) <= 1 {
		rawLines = strings.Split(value, "},")
	}
	candidates := make([]codexGatewayDeepSeekAccessibilityLineCandidate, 0, 16)
	seen := make(map[string]struct{}, 16)
	for index, raw := range rawLines {
		line := strings.TrimSpace(raw)
		if line == "" || !codexGatewayDeepSeekLooksOperableAccessibilityLine(line) {
			continue
		}
		line = codexGatewayDeepSeekTruncateString(line, 160)
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		candidates = append(candidates, codexGatewayDeepSeekAccessibilityLineCandidate{
			index:    index,
			line:     line,
			priority: codexGatewayDeepSeekAccessibilityOperableLinePriority(line),
		})
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].priority != candidates[j].priority {
			return candidates[i].priority > candidates[j].priority
		}
		return candidates[i].index < candidates[j].index
	})
	limit := len(candidates)
	if limit > 8 {
		limit = 8
	}
	selected := append([]codexGatewayDeepSeekAccessibilityLineCandidate(nil), candidates[:limit]...)
	sort.SliceStable(selected, func(i, j int) bool {
		return selected[i].index < selected[j].index
	})
	out := make([]string, 0, len(selected))
	for _, candidate := range selected {
		out = append(out, candidate.line)
	}
	return out
}

func codexGatewayDeepSeekAccessibilityOperableLinePriority(line string) int {
	lowered := strings.ToLower(line)
	score := 0
	if strings.Contains(lowered, "focused ui element") {
		score += 140
	}
	if strings.Contains(lowered, "focused") || strings.Contains(line, "聚焦") {
		score += 90
	}
	inputHints := []string{
		"textbox", "text field", "textfield", "text input", "textarea", "input",
		"placeholder", "发消息", "文本输入区", "输入框", "输入区",
	}
	for _, hint := range inputHints {
		if strings.Contains(lowered, strings.ToLower(hint)) {
			score += 110
			break
		}
	}
	if strings.Contains(lowered, "settable") {
		score += 35
	}
	if strings.Contains(lowered, "button") || strings.Contains(line, "按钮") {
		score += 45
	}
	sendHints := []string{"send", "submit", "发送", "提交"}
	for _, hint := range sendHints {
		if strings.Contains(lowered, strings.ToLower(hint)) {
			score += 100
			break
		}
	}
	if strings.Contains(lowered, "timeout") || strings.Contains(line, "超时") ||
		strings.Contains(lowered, "error") || strings.Contains(line, "错误") || strings.Contains(line, "报错") {
		score += 70
	}
	if strings.Contains(lowered, "disabled") || strings.Contains(line, "禁用") ||
		strings.Contains(lowered, "enabled") || strings.Contains(line, "启用") {
		score += 20
	}
	if strings.Contains(lowered, "link") || strings.Contains(line, "链接") {
		score += 10
	}
	if strings.Contains(lowered, "menu") || strings.Contains(line, "菜单") ||
		strings.Contains(lowered, "tab") || strings.Contains(line, "标签") {
		score += 20
	}
	return score
}

func codexGatewayDeepSeekLooksOperableAccessibilityLine(line string) bool {
	lowered := strings.ToLower(line)
	keywords := []string{
		"button", "checkbox", "radio", "textbox", "text field", "textfield", "textarea", "text area", "input",
		"menu", "menuitem", "tab", "link", "combobox", "slider", "switch", "option",
		"selected", "focused", "enabled", "disabled", "settable", "editable", "placeholder",
		"send", "submit", "error", "warning", "timeout",
		"按钮", "输入", "菜单", "标签", "链接", "复选", "选中", "聚焦", "禁用", "启用", "可编辑", "发送", "提交", "错误", "报错", "警告", "超时",
	}
	for _, keyword := range keywords {
		if strings.Contains(lowered, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func codexGatewayDeepSeekAccessibilityVisibleTextLines(value string) []string {
	rawLines := strings.FieldsFunc(value, func(r rune) bool {
		return r == '\n' || r == '\r'
	})
	if len(rawLines) <= 1 {
		rawLines = strings.Split(value, "},")
	}
	candidates := make([]codexGatewayDeepSeekAccessibilityLineCandidate, 0, 12)
	seen := make(map[string]struct{}, 12)
	for index, raw := range rawLines {
		line := strings.TrimSpace(raw)
		if !codexGatewayDeepSeekLooksVisibleAccessibilityTextLine(line) {
			continue
		}
		line = codexGatewayDeepSeekTruncateString(line, 180)
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		candidates = append(candidates, codexGatewayDeepSeekAccessibilityLineCandidate{
			index:    index,
			line:     line,
			priority: codexGatewayDeepSeekAccessibilityVisibleTextPriority(line),
		})
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].priority != candidates[j].priority {
			return candidates[i].priority > candidates[j].priority
		}
		return candidates[i].index > candidates[j].index
	})
	limit := len(candidates)
	if limit > 6 {
		limit = 6
	}
	selected := append([]codexGatewayDeepSeekAccessibilityLineCandidate(nil), candidates[:limit]...)
	sort.SliceStable(selected, func(i, j int) bool {
		return selected[i].index < selected[j].index
	})
	out := make([]string, 0, len(selected))
	for _, candidate := range selected {
		out = append(out, candidate.line)
	}
	return out
}

func codexGatewayDeepSeekLooksVisibleAccessibilityTextLine(line string) bool {
	if line == "" {
		return false
	}
	lowered := strings.ToLower(line)
	if strings.Contains(lowered, "chrome://") ||
		strings.Contains(lowered, "<app_state") ||
		strings.Contains(lowered, "<app_specific") ||
		strings.Contains(lowered, "computer use state") ||
		strings.HasPrefix(lowered, "app=") ||
		strings.HasPrefix(lowered, "window:") ||
		strings.Contains(lowered, "secondary actions") {
		return false
	}
	if strings.Contains(lowered, "link description") {
		return false
	}
	return strings.Contains(lowered, "statictext") ||
		strings.Contains(lowered, "static text") ||
		strings.Contains(lowered, " text ") ||
		strings.HasPrefix(lowered, "text ") ||
		strings.Contains(line, "文本")
}

func codexGatewayDeepSeekAccessibilityVisibleTextPriority(line string) int {
	score := len([]rune(line))
	if score > 180 {
		score = 180
	}
	if strings.Contains(line, "回答") || strings.Contains(line, "回复") ||
		strings.Contains(line, "结果") || strings.Contains(line, "总结") {
		score += 30
	}
	if strings.Contains(line, "用户") || strings.Contains(line, "助手") ||
		strings.Contains(line, "豆包") {
		score += 10
	}
	lowered := strings.ToLower(line)
	for _, nav := range []string{"新对话", "更多", "云盘", "ai 浏览器", "历史", "登录"} {
		if strings.Contains(lowered, strings.ToLower(nav)) {
			score -= 50
		}
	}
	return score
}

func codexGatewayDeepSeekAccessibilityPreview(value string) string {
	lines := strings.FieldsFunc(value, func(r rune) bool {
		return r == '\n' || r == '\r'
	})
	out := make([]string, 0, 12)
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		out = append(out, codexGatewayDeepSeekTruncateString(line, 180))
		if len(out) >= 12 {
			break
		}
	}
	if len(out) == 0 {
		return codexGatewayDeepSeekTruncateString(value, codexGatewayDeepSeekToolOutputFieldPreviewChars)
	}
	return strings.Join(out, "\n")
}

func codexGatewayDeepSeekSummarizeToolString(field, value string, previewChars int) map[string]any {
	return map[string]any{
		"content_class":  "text",
		"field":          field,
		"truncated":      true,
		"original_chars": len(value),
		"sha256":         codexGatewayDeepSeekTextSHA256(value),
		"preview":        codexGatewayDeepSeekTruncateString(value, previewChars),
	}
}

func codexGatewayDeepSeekMarshalToolOutputSummary(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	out := string(raw)
	if len(out) <= codexGatewayDeepSeekToolOutputMaxChars {
		return out, nil
	}
	fallback := map[string]any{
		"truncated":      true,
		"original_chars": len(out),
		"sha256":         codexGatewayDeepSeekTextSHA256(out),
	}
	raw, err = json.Marshal(fallback)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func codexGatewayDeepSeekTruncateString(value string, maxChars int) string {
	if maxChars <= 0 || len(value) <= maxChars {
		return value
	}
	runes := []rune(value)
	if len(runes) <= maxChars {
		return value
	}
	return string(runes[:maxChars])
}

func codexGatewayDeepSeekTextSHA256(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
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
