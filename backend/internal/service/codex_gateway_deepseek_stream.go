package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/tidwall/gjson"
)

func ExecuteCodexGatewayDeepSeekStream(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	apiKey string,
	model CodexGatewayModel,
	req CodexGatewayResponsesCreateRequest,
	stateStore *CodexGatewayStateStore,
	reqCtx CodexGatewayDeepSeekRequestContext,
	cfg CodexGatewayDeepSeekRequestConfig,
	dst io.Writer,
) (CodexGatewayDeepSeekAdapterResult, error) {
	if dst == nil {
		return CodexGatewayDeepSeekAdapterResult{}, fmt.Errorf("codex deepseek stream requires destination writer")
	}
	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, stateStore, reqCtx, cfg)
	if err != nil {
		return CodexGatewayDeepSeekAdapterResult{}, err
	}
	body := cloneCodexGatewayStreamBody(prepared.Body)
	body["stream"] = true
	body["stream_options"] = map[string]any{"include_usage": true}

	if client == nil {
		client = http.DefaultClient
	}
	rawBody, err := json.Marshal(body)
	if err != nil {
		return CodexGatewayDeepSeekAdapterResult{}, fmt.Errorf("marshal codex deepseek stream request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, buildOpenAIChatCompletionsURL(baseURL), bytes.NewReader(rawBody))
	if err != nil {
		return CodexGatewayDeepSeekAdapterResult{}, fmt.Errorf("build codex deepseek stream request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if strings.TrimSpace(apiKey) != "" {
		httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return CodexGatewayDeepSeekAdapterResult{}, fmt.Errorf("send codex deepseek stream request: %w", err)
	}
	defer resp.Body.Close()

	result := CodexGatewayDeepSeekAdapterResult{
		ServiceResponse: CodexGatewayServiceResponse{
			StatusCode: resp.StatusCode,
			Headers:    cloneCodexGatewayHTTPHeader(resp.Header),
		},
	}
	writer := NewCodexGatewayResponseEventWriter(dst)

	if resp.StatusCode >= 400 {
		bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		if readErr != nil {
			return CodexGatewayDeepSeekAdapterResult{}, readErr
		}
		result.ServiceResponse.Body = codexGatewayDeepSeekMapErrorBody(resp.StatusCode, bodyBytes)
		errorType := gjson.GetBytes(result.ServiceResponse.Body, "error.type").String()
		errorCode := gjson.GetBytes(result.ServiceResponse.Body, "error.code").String()
		errorMessage := gjson.GetBytes(result.ServiceResponse.Body, "error.message").String()
		errResp := CodexGatewayResponse{
			Object: "response",
			Status: "failed",
			Output: []json.RawMessage{},
			Error: &CodexGatewayResponseError{
				Code:    errorCode,
				Message: errorMessage,
				RawFields: map[string]json.RawMessage{
					"type": json.RawMessage(fmt.Sprintf("%q", errorType)),
				},
			},
		}
		result.ProviderResult.Response = errResp
		if err := writer.WriteResponseFailed(errResp); err != nil {
			return CodexGatewayDeepSeekAdapterResult{}, err
		}
		return result, nil
	}

	state := newCodexGatewayDeepSeekStreamState(model, prepared.ToolNameMap)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), defaultMaxLineSize)
	dataLines := make([]string, 0, 4)

	flush := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		payload := strings.TrimSpace(strings.Join(dataLines, "\n"))
		dataLines = dataLines[:0]
		if payload == "" || payload == "[DONE]" {
			return nil
		}
		return state.consumePayload([]byte(payload), writer)
	}

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if err := flush(); err != nil {
				return CodexGatewayDeepSeekAdapterResult{}, err
			}
			continue
		}
		if strings.HasPrefix(trimmed, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return CodexGatewayDeepSeekAdapterResult{}, err
	}
	if err := flush(); err != nil {
		return CodexGatewayDeepSeekAdapterResult{}, err
	}

	finalEvent := ""
	var finishErr error
	if state.terminalSeen {
		finalEvent, finishErr = state.finish(writer)
	} else {
		finalEvent, finishErr = state.finishEarly(writer)
	}
	_ = finalEvent
	if finishErr != nil {
		return CodexGatewayDeepSeekAdapterResult{}, finishErr
	}

	if state.shouldPersistToolLoopState() {
		if err := codexGatewayDeepSeekPersistState(
			stateStore,
			state.responseID,
			state.upstreamModel,
			reqCtx,
			state.messageText.String(),
			state.messageAdded,
			state.reasoningText.String(),
			state.reasoningPresent,
			!state.reasoningPresent,
			state.storedToolCalls(),
			prepared.ToolNameMap,
		); err != nil {
			return CodexGatewayDeepSeekAdapterResult{}, err
		}
	}

	result.ProviderResult = state.providerResult(resp.Header.Get("x-request-id"))
	return result, nil
}

type codexGatewayDeepSeekStreamState struct {
	model            CodexGatewayModel
	toolNameMap      map[string]CodexGatewayToolNameMapEntry
	responseID       string
	upstreamModel    string
	reasoningText    strings.Builder
	reasoningPresent bool
	messageText      strings.Builder
	messageAdded     bool
	messageID        string
	messageIndex     int
	reasoningItemID  string
	reasoningAdded   bool
	reasoningIndex   int
	nextOutputIndex  int
	toolCalls        map[int]*codexGatewayDeepSeekStreamToolCall
	toolOrder        []int
	usage            CodexGatewayProviderUsage
	usageRaw         json.RawMessage
	finishReason     string
	terminalSeen     bool
	createdSent      bool
}

type codexGatewayDeepSeekStreamToolCall struct {
	Index       int
	OutputIndex int
	CallID      string
	Alias       string
	Name        string
	Kind        string
	Buffer      strings.Builder
	Added       bool
	ItemEmitted bool
	EmittedLen  int
}

func newCodexGatewayDeepSeekStreamState(model CodexGatewayModel, toolNameMap map[string]CodexGatewayToolNameMapEntry) *codexGatewayDeepSeekStreamState {
	return &codexGatewayDeepSeekStreamState{
		model:       model,
		toolNameMap: cloneCodexGatewayToolNameMap(toolNameMap),
		toolCalls:   make(map[int]*codexGatewayDeepSeekStreamToolCall),
	}
}

func (s *codexGatewayDeepSeekStreamState) consumePayload(payload []byte, writer *CodexGatewayResponseEventWriter) error {
	var chunk apicompat.ChatCompletionsChunk
	if err := json.Unmarshal(payload, &chunk); err != nil {
		return fmt.Errorf("decode codex deepseek stream chunk: %w", err)
	}
	if s.responseID == "" {
		s.responseID = strings.TrimSpace(chunk.ID)
		if s.responseID == "" {
			s.responseID = "chatcmpl_stream"
		}
		s.messageID = codexGatewayDeepSeekMessageID(s.responseID, 0)
		s.reasoningItemID = codexGatewayDeepSeekReasoningItemID(s.responseID)
	}
	if s.upstreamModel == "" {
		s.upstreamModel = strings.TrimSpace(chunk.Model)
	}
	if !s.createdSent {
		created := CodexGatewayResponse{
			ID:     s.responseID,
			Object: "response",
			Model:  s.responseModel(),
			Status: "in_progress",
			Output: []json.RawMessage{},
		}
		if err := writer.WriteResponseCreated(created); err != nil {
			return err
		}
		s.createdSent = true
	}

	if len(chunk.Choices) > 0 {
		choice := chunk.Choices[0]
		if choice.Delta.ReasoningContent != nil {
			if !s.reasoningAdded {
				item := map[string]any{
					"type":    "reasoning",
					"id":      s.reasoningItemID,
					"status":  "in_progress",
					"content": []map[string]any{},
				}
				rawItem, err := json.Marshal(item)
				if err != nil {
					return err
				}
				if err := writer.WriteOutputItemAdded(s.responseID, s.nextOutputIndex, rawItem); err != nil {
					return err
				}
				part, _ := json.Marshal(map[string]any{"type": "reasoning_text", "text": ""})
				if err := writer.WriteContentPartAdded(s.responseID, s.reasoningItemID, s.nextOutputIndex, 0, part); err != nil {
					return err
				}
				s.reasoningAdded = true
				s.reasoningIndex = s.nextOutputIndex
				s.nextOutputIndex++
			}
			s.reasoningPresent = true
			s.reasoningText.WriteString(*choice.Delta.ReasoningContent)
			if err := writer.WriteReasoningTextDelta(s.responseID, s.reasoningItemID, s.reasoningIndex, 0, *choice.Delta.ReasoningContent); err != nil {
				return err
			}
		}
		if choice.Delta.Content != nil && *choice.Delta.Content != "" {
			if !s.messageAdded {
				item := map[string]any{
					"type":    "message",
					"id":      s.messageID,
					"role":    "assistant",
					"status":  "in_progress",
					"content": []map[string]any{},
				}
				rawItem, err := json.Marshal(item)
				if err != nil {
					return err
				}
				if err := writer.WriteOutputItemAdded(s.responseID, s.nextOutputIndex, rawItem); err != nil {
					return err
				}
				part, _ := json.Marshal(map[string]any{"type": "output_text", "text": ""})
				if err := writer.WriteContentPartAdded(s.responseID, s.messageID, s.nextOutputIndex, 0, part); err != nil {
					return err
				}
				s.messageAdded = true
				s.messageIndex = s.nextOutputIndex
				s.nextOutputIndex++
			}
			s.messageText.WriteString(*choice.Delta.Content)
			if err := writer.WriteOutputTextDelta(s.responseID, s.messageID, s.messageIndex, 0, *choice.Delta.Content); err != nil {
				return err
			}
		}
		for _, delta := range choice.Delta.ToolCalls {
			if err := s.consumeToolCallDelta(delta, writer); err != nil {
				return err
			}
		}
		if choice.FinishReason != nil {
			s.finishReason = strings.TrimSpace(*choice.FinishReason)
			s.terminalSeen = true
		}
	}
	if chunk.Usage != nil {
		s.usageRaw, s.usage = codexGatewayDeepSeekUsageJSON(chunk.Usage)
	}
	return nil
}

func (s *codexGatewayDeepSeekStreamState) consumeToolCallDelta(delta apicompat.ChatToolCall, writer *CodexGatewayResponseEventWriter) error {
	index := 0
	if delta.Index != nil {
		index = *delta.Index
	}
	call := s.toolCalls[index]
	if call == nil {
		call = &codexGatewayDeepSeekStreamToolCall{
			Index:       index,
			OutputIndex: s.nextOutputIndex,
		}
		s.toolCalls[index] = call
		s.toolOrder = append(s.toolOrder, index)
		s.nextOutputIndex++
	}
	if strings.TrimSpace(delta.ID) != "" {
		call.CallID = strings.TrimSpace(delta.ID)
	}
	if strings.TrimSpace(delta.Function.Name) != "" {
		call.Alias = strings.TrimSpace(delta.Function.Name)
		entry, ok := s.toolNameMap[call.Alias]
		if ok {
			call.Name = entry.Name
			call.Kind = entry.Kind
		} else {
			call.Name = call.Alias
			call.Kind = CodexGatewayToolKindFunction
		}
	}
	if !call.Added && call.CallID != "" && call.Name != "" {
		call.Added = true
	}
	if call.Added && !call.ItemEmitted {
		item := map[string]any{
			"id":      codexGatewayDeepSeekToolItemID(call.CallID),
			"call_id": call.CallID,
			"name":    call.Name,
			"status":  "in_progress",
		}
		if call.Kind == CodexGatewayToolKindCustom {
			item["type"] = "custom_tool_call"
			item["input"] = ""
		} else {
			item["type"] = "function_call"
			item["arguments"] = ""
		}
		rawItem, err := json.Marshal(item)
		if err != nil {
			return err
		}
		if err := writer.WriteOutputItemAdded(s.responseID, call.OutputIndex, rawItem); err != nil {
			return err
		}
		call.ItemEmitted = true
	}
	if delta.Function.Arguments != "" {
		call.Buffer.WriteString(delta.Function.Arguments)
	}
	if call.ItemEmitted {
		args := call.Buffer.String()
		if len(args) > call.EmittedLen {
			deltaText := args[call.EmittedLen:]
			call.EmittedLen = len(args)
			return writer.WriteFunctionCallArgumentsDelta(s.responseID, codexGatewayDeepSeekToolItemID(call.CallID), call.OutputIndex, deltaText)
		}
	}
	return nil
}

func (s *codexGatewayDeepSeekStreamState) finish(writer *CodexGatewayResponseEventWriter) (string, error) {
	status, incompleteReason := codexGatewayDeepSeekFinishReasonStatus(s.finishReason)
	response := s.finalResponse(status, incompleteReason)
	if err := s.writeDoneEvents(writer); err != nil {
		return "", err
	}
	switch status {
	case "completed":
		if err := writer.WriteResponseCompleted(response); err != nil {
			return "", err
		}
		return "response.completed", nil
	default:
		if err := writer.WriteResponseIncomplete(response); err != nil {
			return "", err
		}
		return "response.incomplete", nil
	}
}

func (s *codexGatewayDeepSeekStreamState) finishEarly(writer *CodexGatewayResponseEventWriter) (string, error) {
	status := "failed"
	incompleteReason := ""
	if s.hasPartialState() {
		status = "incomplete"
		incompleteReason = codexGatewayDeepSeekStreamClosedReason
	}
	response := s.finalResponse(status, incompleteReason)
	if err := s.writeDoneEvents(writer); err != nil {
		return "", err
	}
	if status == "failed" {
		response.Error = &CodexGatewayResponseError{
			Code:    "upstream_error",
			Message: "DeepSeek stream ended before completion.",
			RawFields: map[string]json.RawMessage{
				"type": json.RawMessage(`"api_error"`),
			},
		}
		if err := writer.WriteResponseFailed(response); err != nil {
			return "", err
		}
		return "response.failed", nil
	}
	if err := writer.WriteResponseIncomplete(response); err != nil {
		return "", err
	}
	return "response.incomplete", nil
}

func (s *codexGatewayDeepSeekStreamState) writeDoneEvents(writer *CodexGatewayResponseEventWriter) error {
	for _, index := range s.sortedOutputIndexes() {
		switch {
		case s.reasoningAdded && index == s.reasoningIndex:
			if err := writer.WriteReasoningTextDone(s.responseID, s.reasoningItemID, s.reasoningIndex, 0, s.reasoningText.String()); err != nil {
				return err
			}
			part, _ := json.Marshal(map[string]any{"type": "reasoning_text", "text": s.reasoningText.String()})
			if err := writer.WriteContentPartDone(s.responseID, s.reasoningItemID, s.reasoningIndex, 0, part); err != nil {
				return err
			}
			item := map[string]any{
				"type":   "reasoning",
				"id":     s.reasoningItemID,
				"status": "completed",
				"content": []map[string]any{{
					"type": "reasoning_text",
					"text": s.reasoningText.String(),
				}},
			}
			if rawItem, err := json.Marshal(item); err == nil {
				if err := writer.WriteOutputItemDone(s.responseID, s.reasoningIndex, rawItem); err != nil {
					return err
				}
			}
		case s.messageAdded && index == s.messageIndex:
			if err := writer.WriteOutputTextDone(s.responseID, s.messageID, s.messageIndex, 0, s.messageText.String()); err != nil {
				return err
			}
			part, _ := json.Marshal(map[string]any{"type": "output_text", "text": s.messageText.String()})
			if err := writer.WriteContentPartDone(s.responseID, s.messageID, s.messageIndex, 0, part); err != nil {
				return err
			}
			item := map[string]any{
				"type":   "message",
				"id":     s.messageID,
				"role":   "assistant",
				"status": "completed",
				"content": []map[string]any{{
					"type": "output_text",
					"text": s.messageText.String(),
				}},
			}
			if rawItem, err := json.Marshal(item); err == nil {
				if err := writer.WriteOutputItemDone(s.responseID, s.messageIndex, rawItem); err != nil {
					return err
				}
			}
		default:
			call := s.toolCallByOutputIndex(index)
			if call == nil || !s.shouldExposeToolCall(call) {
				continue
			}
			doneItem := map[string]any{
				"id":      codexGatewayDeepSeekToolItemID(call.CallID),
				"call_id": call.CallID,
				"name":    call.Name,
				"status":  "completed",
			}
			if call.Kind == CodexGatewayToolKindCustom {
				doneItem["type"] = "custom_tool_call"
				doneItem["input"] = call.Buffer.String()
			} else {
				doneItem["type"] = "function_call"
				doneItem["arguments"] = call.Buffer.String()
			}
			if rawItem, err := json.Marshal(doneItem); err == nil {
				if err := writer.WriteFunctionCallArgumentsDone(s.responseID, codexGatewayDeepSeekToolItemID(call.CallID), call.OutputIndex, rawItem); err != nil {
					return err
				}
				if err := writer.WriteOutputItemDone(s.responseID, call.OutputIndex, rawItem); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (s *codexGatewayDeepSeekStreamState) finalResponse(status, incompleteReason string) CodexGatewayResponse {
	response := CodexGatewayResponse{
		ID:     s.responseID,
		Object: "response",
		Model:  s.responseModel(),
		Status: status,
		Output: s.outputItems(),
		Usage:  s.usageRaw,
	}
	if status == "incomplete" && incompleteReason != "" {
		response.IncompleteDetails = json.RawMessage(fmt.Sprintf(`{"reason":%q}`, incompleteReason))
	}
	return response
}

func (s *codexGatewayDeepSeekStreamState) outputItems() []json.RawMessage {
	byIndex := make(map[int]json.RawMessage, 2+len(s.toolCalls))
	indexes := make([]int, 0, 2+len(s.toolCalls))
	if s.reasoningAdded {
		item, _ := json.Marshal(map[string]any{
			"type":   "reasoning",
			"id":     s.reasoningItemID,
			"status": "completed",
			"content": []map[string]any{{
				"type": "reasoning_text",
				"text": s.reasoningText.String(),
			}},
		})
		byIndex[s.reasoningIndex] = item
		indexes = append(indexes, s.reasoningIndex)
	}
	if s.messageAdded {
		item, _ := json.Marshal(map[string]any{
			"type":   "message",
			"id":     s.messageID,
			"role":   "assistant",
			"status": "completed",
			"content": []map[string]any{{
				"type": "output_text",
				"text": s.messageText.String(),
			}},
		})
		byIndex[s.messageIndex] = item
		indexes = append(indexes, s.messageIndex)
	}
	for _, index := range s.sortedToolOrder() {
		call := s.toolCalls[index]
		if call == nil || !s.shouldExposeToolCall(call) {
			continue
		}
		item := map[string]any{
			"id":      codexGatewayDeepSeekToolItemID(call.CallID),
			"call_id": call.CallID,
			"name":    call.Name,
			"status":  "completed",
		}
		if call.Kind == CodexGatewayToolKindCustom {
			item["type"] = "custom_tool_call"
			item["input"] = call.Buffer.String()
		} else {
			item["type"] = "function_call"
			item["arguments"] = call.Buffer.String()
		}
		rawItem, _ := json.Marshal(item)
		byIndex[call.OutputIndex] = rawItem
		indexes = append(indexes, call.OutputIndex)
	}
	sort.Ints(indexes)
	out := make([]json.RawMessage, 0, len(indexes))
	for _, index := range indexes {
		if rawItem, ok := byIndex[index]; ok {
			out = append(out, rawItem)
		}
	}
	return out
}

func (s *codexGatewayDeepSeekStreamState) storedToolCalls() []CodexGatewayStoredToolCall {
	out := make([]CodexGatewayStoredToolCall, 0, len(s.toolCalls))
	for _, call := range s.sortedToolCallsByOutputIndex() {
		if call == nil || !s.shouldExposeToolCall(call) {
			continue
		}
		out = append(out, CodexGatewayStoredToolCall{
			ID:        call.CallID,
			Type:      call.Kind,
			Name:      call.Name,
			Arguments: call.Buffer.String(),
		})
	}
	return out
}

func (s *codexGatewayDeepSeekStreamState) providerResult(upstreamRequestID string) CodexGatewayProviderResult {
	response := s.finalResponse(codexGatewayDeepSeekFinishReasonStatusValue(s.finishReason, s.terminalSeen, s.hasPartialState()), codexGatewayDeepSeekFinishReasonIncompleteReason(s.finishReason, s.terminalSeen, s.hasPartialState()))
	if !s.terminalSeen && !s.hasPartialState() {
		response.Error = &CodexGatewayResponseError{
			Code:    "upstream_error",
			Message: "DeepSeek stream ended before completion.",
		}
	}
	return CodexGatewayProviderResult{
		ResponseID:              s.responseID,
		UpstreamRequestID:       strings.TrimSpace(upstreamRequestID),
		UpstreamModel:           s.upstreamModel,
		Response:                response,
		Usage:                   s.usage,
		ReasoningContent:        s.reasoningText.String(),
		ReasoningContentPresent: s.reasoningPresent,
		ToolCalls:               s.storedToolCalls(),
	}
}

func codexGatewayDeepSeekReasoningItemID(responseID string) string {
	responseID = strings.TrimSpace(responseID)
	if responseID == "" {
		responseID = "response"
	}
	return "rs_" + responseID
}

func (s *codexGatewayDeepSeekStreamState) responseModel() string {
	if strings.TrimSpace(s.model.Slug) != "" {
		return strings.TrimSpace(s.model.Slug)
	}
	return strings.TrimSpace(s.upstreamModel)
}

func (s *codexGatewayDeepSeekStreamState) hasPartialState() bool {
	return s.messageAdded || len(s.toolCalls) > 0 || s.reasoningAdded || s.reasoningPresent || s.reasoningText.Len() > 0 || len(s.usageRaw) > 0
}

func (s *codexGatewayDeepSeekStreamState) shouldPersistToolLoopState() bool {
	if len(s.toolCalls) == 0 {
		return false
	}
	return s.terminalSeen && strings.TrimSpace(s.finishReason) == "tool_calls"
}

func (s *codexGatewayDeepSeekStreamState) shouldExposeToolCall(call *codexGatewayDeepSeekStreamToolCall) bool {
	if call == nil || !call.Added {
		return false
	}
	if strings.TrimSpace(call.CallID) == "" || strings.TrimSpace(call.Name) == "" {
		return false
	}
	return s.terminalSeen && strings.TrimSpace(s.finishReason) == "tool_calls"
}

func (s *codexGatewayDeepSeekStreamState) sortedToolOrder() []int {
	out := append([]int(nil), s.toolOrder...)
	sort.Ints(out)
	return out
}

func (s *codexGatewayDeepSeekStreamState) toolCallByOutputIndex(outputIndex int) *codexGatewayDeepSeekStreamToolCall {
	for _, index := range s.sortedToolOrder() {
		call := s.toolCalls[index]
		if call != nil && call.OutputIndex == outputIndex {
			return call
		}
	}
	return nil
}

func (s *codexGatewayDeepSeekStreamState) sortedToolCallsByOutputIndex() []*codexGatewayDeepSeekStreamToolCall {
	calls := make([]*codexGatewayDeepSeekStreamToolCall, 0, len(s.toolCalls))
	for _, index := range s.sortedToolOrder() {
		call := s.toolCalls[index]
		if call != nil {
			calls = append(calls, call)
		}
	}
	sort.Slice(calls, func(i, j int) bool {
		return calls[i].OutputIndex < calls[j].OutputIndex
	})
	return calls
}

func (s *codexGatewayDeepSeekStreamState) sortedOutputIndexes() []int {
	indexes := make([]int, 0, 2+len(s.toolCalls))
	if s.reasoningAdded {
		indexes = append(indexes, s.reasoningIndex)
	}
	if s.messageAdded {
		indexes = append(indexes, s.messageIndex)
	}
	for _, call := range s.sortedToolCallsByOutputIndex() {
		indexes = append(indexes, call.OutputIndex)
	}
	sort.Ints(indexes)
	return indexes
}

func codexGatewayDeepSeekFinishReasonStatus(reason string) (string, string) {
	switch strings.TrimSpace(reason) {
	case "", "stop", "tool_calls":
		return "completed", ""
	case "length":
		return "incomplete", "max_output_tokens"
	case "insufficient_system_resource":
		return "incomplete", "insufficient_system_resource"
	case "content_filter":
		return "incomplete", "content_filter"
	default:
		return "incomplete", strings.TrimSpace(reason)
	}
}

func codexGatewayDeepSeekFinishReasonStatusValue(reason string, terminalSeen bool, partial bool) string {
	if terminalSeen {
		status, _ := codexGatewayDeepSeekFinishReasonStatus(reason)
		return status
	}
	if partial {
		return "incomplete"
	}
	return "failed"
}

func codexGatewayDeepSeekFinishReasonIncompleteReason(reason string, terminalSeen bool, partial bool) string {
	if terminalSeen {
		_, incompleteReason := codexGatewayDeepSeekFinishReasonStatus(reason)
		return incompleteReason
	}
	if partial {
		return codexGatewayDeepSeekStreamClosedReason
	}
	return ""
}
