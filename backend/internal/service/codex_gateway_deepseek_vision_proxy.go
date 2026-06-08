package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func codexGatewayDeepSeekRequestWithHostedVision(ctx context.Context, req CodexGatewayResponsesCreateRequest, stateStore *CodexGatewayStateStore, reqCtx CodexGatewayDeepSeekRequestContext, upstreamModel string, cfg CodexGatewayDeepSeekRequestConfig) (CodexGatewayResponsesCreateRequest, error) {
	if cfg.HostedImageVision == nil || len(req.Input) == 0 {
		return req, nil
	}
	items, err := decodeCodexGatewayInputItems(req.Input)
	if err != nil {
		return CodexGatewayResponsesCreateRequest{}, err
	}
	if len(items) == 0 {
		return req, nil
	}

	computerUseOutputs := codexGatewayDeepSeekComputerUseOutputCallIDs(items, req, stateStore, reqCtx, upstreamModel, cfg)
	rewritten, changed := false, false
	for _, itemAny := range items {
		item, ok := itemAny.(map[string]any)
		if !ok {
			continue
		}
		if codexGatewayDeepSeekIsToolOutputItem(item) {
			callID := strings.TrimSpace(firstCodexGatewayToolString(item["call_id"], item["tool_call_id"], item["id"]))
			if _, ok := computerUseOutputs[callID]; ok && codexGatewayDeepSeekRewriteToolOutputImages(ctx, item, cfg) {
				rewritten = true
				changed = true
			}
		}
	}
	if !changed || !rewritten {
		return req, nil
	}

	rawInput, err := json.Marshal(items)
	if err != nil {
		return CodexGatewayResponsesCreateRequest{}, fmt.Errorf("marshal deepseek hosted vision input: %w", err)
	}
	req.Input = rawInput
	return req, nil
}

func codexGatewayDeepSeekComputerUseOutputCallIDs(items []any, req CodexGatewayResponsesCreateRequest, stateStore *CodexGatewayStateStore, reqCtx CodexGatewayDeepSeekRequestContext, upstreamModel string, cfg CodexGatewayDeepSeekRequestConfig) map[string]struct{} {
	out := make(map[string]struct{})
	for _, itemAny := range items {
		item, ok := itemAny.(map[string]any)
		if !ok {
			continue
		}
		switch strings.TrimSpace(firstCodexGatewayToolString(item["type"])) {
		case "function_call", "local_shell_call", "custom_tool_call":
		default:
			continue
		}
		callID := strings.TrimSpace(firstCodexGatewayToolString(item["call_id"], item["tool_call_id"], item["id"]))
		if callID == "" {
			continue
		}
		if codexGatewayDeepSeekIsComputerUseToolName(firstCodexGatewayToolString(item["name"])) {
			out[callID] = struct{}{}
		}
	}
	if stateStore == nil || req.PreviousResponseID == nil || strings.TrimSpace(*req.PreviousResponseID) == "" {
		return out
	}
	state, err := stateStore.Get(CodexGatewayStateLookupKey{
		ResponseID:    strings.TrimSpace(*req.PreviousResponseID),
		SessionKey:    reqCtx.SessionKey,
		IsolationKey:  reqCtx.IsolationKey,
		Provider:      codexGatewayChatCompatProviderName(cfg),
		UpstreamModel: upstreamModel,
	})
	if err != nil {
		return out
	}
	for _, call := range state.ToolCalls {
		callID := strings.TrimSpace(call.ID)
		if callID == "" {
			continue
		}
		if codexGatewayDeepSeekIsComputerUseToolCall(call, state.ToolNameMap) {
			out[callID] = struct{}{}
		}
	}
	return out
}

func codexGatewayDeepSeekIsComputerUseToolCall(call CodexGatewayStoredToolCall, toolNameMap map[string]CodexGatewayToolNameMapEntry) bool {
	if codexGatewayDeepSeekIsComputerUseToolIdentity(call.Name, call.Alias, "") {
		return true
	}
	if entry, ok := toolNameMap[strings.TrimSpace(call.Alias)]; ok {
		return codexGatewayDeepSeekIsComputerUseToolIdentity(entry.Name, entry.Alias, firstCodexGatewayToolString(entry.Namespace, entry.NamespacePath))
	}
	return false
}

func codexGatewayDeepSeekIsComputerUseToolName(name string) bool {
	return codexGatewayDeepSeekIsComputerUseToolIdentity(name, "", "")
}

func codexGatewayDeepSeekIsComputerUseToolIdentity(name, alias, namespace string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	alias = strings.ToLower(strings.TrimSpace(alias))
	namespace = strings.ToLower(strings.TrimSpace(namespace))
	if strings.Contains(namespace, "computer_use") || strings.Contains(namespace, "computer-use") {
		return true
	}
	for _, value := range []string{name, alias} {
		if value == "" {
			continue
		}
		if strings.Contains(value, "computer_use") || strings.Contains(value, "computer-use") {
			return true
		}
		if strings.HasPrefix(value, "mcp__computer_use__") || strings.HasPrefix(value, "mcp_computer_use_") {
			return true
		}
	}
	return false
}

func codexGatewayDeepSeekIsToolOutputItem(item map[string]any) bool {
	switch strings.TrimSpace(firstCodexGatewayToolString(item["type"])) {
	case "function_call_output", "local_shell_call_output", "custom_tool_call_output":
		return true
	default:
		return false
	}
}

func codexGatewayDeepSeekRewriteToolOutputImages(ctx context.Context, item map[string]any, cfg CodexGatewayDeepSeekRequestConfig) bool {
	if cfg.HostedImageVision == nil {
		return false
	}
	output := item["output"]
	if text, ok := output.(string); ok {
		if parsed, parsedOK := codexGatewayDeepSeekParseStructuredToolOutputString(text); parsedOK {
			rewritten, changed := codexGatewayDeepSeekRewriteToolOutputImagesValue(ctx, "", parsed, cfg)
			if changed {
				item["output"] = rewritten
				return true
			}
		}
		return false
	}
	rewritten, changed := codexGatewayDeepSeekRewriteToolOutputImagesValue(ctx, "", output, cfg)
	if changed {
		item["output"] = rewritten
	}
	return changed
}

func codexGatewayDeepSeekRewriteToolOutputImagesValue(ctx context.Context, field string, value any, cfg CodexGatewayDeepSeekRequestConfig) (any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		if len(typed) == 0 {
			return typed, false
		}
		out := make(map[string]any, len(typed))
		changed := false
		for key, raw := range typed {
			rewritten, fieldChanged := codexGatewayDeepSeekRewriteToolOutputImagesValue(ctx, key, raw, cfg)
			out[key] = rewritten
			changed = changed || fieldChanged
		}
		return out, changed
	case []any:
		if len(typed) == 0 {
			return typed, false
		}
		out := make([]any, 0, len(typed))
		changed := false
		for _, raw := range typed {
			rewritten, itemChanged := codexGatewayDeepSeekRewriteToolOutputImagesValue(ctx, field, raw, cfg)
			out = append(out, rewritten)
			changed = changed || itemChanged
		}
		return out, changed
	case string:
		imageURL := typed
		if !codexGatewayDeepSeekShouldSendToolOutputImageToVision(field, imageURL) {
			return typed, false
		}
		summary, err := cfg.HostedImageVision(ctx, imageURL)
		if err != nil || strings.TrimSpace(summary) == "" {
			return typed, false
		}
		return map[string]any{
			"content_class":  "computer_screenshot",
			"vision_summary": strings.TrimSpace(summary),
			"truncated":      true,
			"original_chars": len(imageURL),
			"sha256":         codexGatewayDeepSeekTextSHA256(imageURL),
		}, true
	default:
		return typed, false
	}
}

func codexGatewayDeepSeekShouldSendToolOutputImageToVision(field, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || !codexGatewayDeepSeekIsBinaryLikeToolField(field, value) {
		return false
	}
	if strings.HasPrefix(value, "data:image/") {
		return true
	}
	lowered := strings.ToLower(value)
	if !strings.HasPrefix(lowered, "https://") && !strings.HasPrefix(lowered, "http://") {
		return false
	}
	return strings.Contains(lowered, ".png") ||
		strings.Contains(lowered, ".jpg") ||
		strings.Contains(lowered, ".jpeg") ||
		strings.Contains(lowered, ".webp") ||
		strings.Contains(lowered, ".gif")
}
