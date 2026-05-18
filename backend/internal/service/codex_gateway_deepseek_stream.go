package service

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/websearch"
	"github.com/tidwall/gjson"
)

const codexGatewayDeepSeekHostedToolMaxTurns = 12

var codexGatewayExecuteHostedWebSearchFunc = codexGatewayExecuteHostedWebSearch

type codexGatewayHostedToolContext struct {
	WebSearchResults map[string]string
	VisibleEventSink func(codexGatewayHostedToolVisibleEvent) error
}

type codexGatewayHostedToolVisibleEvent struct {
	Phase     string
	CallID    string
	Name      string
	Arguments string
	Query     string
	Output    string
	Reused    bool
}

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
	return executeCodexGatewayDeepSeekStreamWithHostedToolTurns(ctx, client, baseURL, apiKey, model, req, stateStore, reqCtx, cfg, dst, 0)
}

func executeCodexGatewayDeepSeekStreamWithHostedToolTurns(
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
	turn int,
) (CodexGatewayDeepSeekAdapterResult, error) {
	if dst == nil {
		return CodexGatewayDeepSeekAdapterResult{}, fmt.Errorf("codex deepseek stream requires destination writer")
	}
	if cfg.HostedToolContext == nil {
		cfg.HostedToolContext = &codexGatewayHostedToolContext{WebSearchResults: make(map[string]string)}
	} else if cfg.HostedToolContext.WebSearchResults == nil {
		cfg.HostedToolContext.WebSearchResults = make(map[string]string)
	}
	if turn > codexGatewayDeepSeekHostedToolMaxTurns {
		return CodexGatewayDeepSeekAdapterResult{}, fmt.Errorf("codex deepseek hosted tool loop exceeded %d turns", codexGatewayDeepSeekHostedToolMaxTurns)
	}
	req, err := codexGatewayDeepSeekRequestWithHostedVision(ctx, req, cfg)
	if err != nil {
		return CodexGatewayDeepSeekAdapterResult{}, err
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
	codexGatewayCaptureUpstreamRequest(reqCtx.CaptureTrace, "deepseek", httpReq.Header, rawBody)

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
	codexGatewayCaptureUpstreamResponse(reqCtx.CaptureTrace, resp.Header, resp.StatusCode, nil)
	if resp.StatusCode >= 400 {
		writer := NewCodexGatewayResponseEventWriter(dst)
		bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		if readErr != nil {
			return CodexGatewayDeepSeekAdapterResult{}, readErr
		}
		codexGatewayCaptureUpstreamResponse(reqCtx.CaptureTrace, resp.Header, resp.StatusCode, bodyBytes)
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
	deferredWriter := newCodexGatewayDeferredStreamWriter(dst)
	writer := NewCodexGatewayResponseEventWriter(deferredWriter)
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
			if payload == "[DONE]" {
				codexGatewayCaptureUpstreamStreamEvent(reqCtx.CaptureTrace, "deepseek.done", []byte(`{"done":true}`))
			}
			return nil
		}
		payloadBytes := []byte(payload)
		codexGatewayCaptureUpstreamStreamEvent(reqCtx.CaptureTrace, "chat.completion.chunk", payloadBytes)
		if err := state.consumePayload(payloadBytes, writer); err != nil {
			return err
		}
		if state.hasClientVisibleOutputStarted() {
			return deferredWriter.Flush()
		}
		return nil
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

	if calls := state.serverHandledHostedToolCalls(); len(calls) > 0 {
		if err := deferredWriter.Flush(); err != nil {
			return CodexGatewayDeepSeekAdapterResult{}, err
		}
		writer := NewCodexGatewayResponseEventWriter(dst)
		cfg.HostedToolContext.VisibleEventSink = func(event codexGatewayHostedToolVisibleEvent) error {
			return codexGatewayWriteVisibleHostedWebSearchEvent(writer, state.responseID, state.nextOutputIndex, event)
		}
		nextReq, err := codexGatewayDeepSeekRequestWithHostedToolResults(ctx, req, calls, cfg.HostedWebSearch, cfg.HostedToolContext)
		if err != nil {
			return CodexGatewayDeepSeekAdapterResult{}, err
		}
		return executeCodexGatewayDeepSeekStreamWithHostedToolTurns(ctx, client, baseURL, apiKey, model, nextReq, stateStore, reqCtx, cfg, dst, turn+1)
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

	if err := deferredWriter.Flush(); err != nil {
		return CodexGatewayDeepSeekAdapterResult{}, err
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
	Namespace   string
	Kind        string
	Buffer      strings.Builder
	Added       bool
	ItemEmitted bool
	EmittedLen  int
}

type codexGatewayHostedToolCall struct {
	CallID    string
	Name      string
	Arguments string
}

func (c *codexGatewayDeepSeekStreamToolCall) toolNameMapEntry() CodexGatewayToolNameMapEntry {
	if c == nil {
		return CodexGatewayToolNameMapEntry{}
	}
	return CodexGatewayToolNameMapEntry{
		Alias:     c.Alias,
		Kind:      c.Kind,
		Namespace: c.Namespace,
		Name:      c.Name,
	}
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
			s.reasoningPresent = true
			s.reasoningText.WriteString(*choice.Delta.ReasoningContent)
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
			OutputIndex: -1,
		}
		s.toolCalls[index] = call
		s.toolOrder = append(s.toolOrder, index)
	}
	if strings.TrimSpace(delta.ID) != "" {
		call.CallID = strings.TrimSpace(delta.ID)
	}
	if strings.TrimSpace(delta.Function.Name) != "" {
		call.Alias = strings.TrimSpace(delta.Function.Name)
		entry, ok := s.toolNameMap[call.Alias]
		if ok {
			call.Name = entry.Name
			call.Namespace = entry.Namespace
			call.Kind = entry.Kind
		} else {
			call.Name = call.Alias
			call.Namespace = ""
			call.Kind = CodexGatewayToolKindFunction
		}
	}
	if !call.Added && call.CallID != "" && call.Name != "" {
		call.Added = true
	}
	if call.Added && !call.ItemEmitted && !codexGatewayIsServerHandledHostedTool(call.Kind, call.Name) {
		if call.OutputIndex < 0 {
			call.OutputIndex = s.nextOutputIndex
			s.nextOutputIndex++
		}
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
			if namespace := strings.TrimSpace(call.Namespace); namespace != "" {
				item["namespace"] = namespace
			}
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
		if call.Kind == CodexGatewayToolKindCustom {
			input, ready := codexGatewayDeepSeekCustomToolStreamInput(args, call.toolNameMapEntry())
			if !ready {
				return nil
			}
			if len(input) > call.EmittedLen {
				deltaText := input[call.EmittedLen:]
				call.EmittedLen = len(input)
				return writer.WriteCustomToolCallInputDelta(s.responseID, codexGatewayDeepSeekToolItemID(call.CallID), call.CallID, call.OutputIndex, deltaText)
			}
			return nil
		}
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
			if call.OutputIndex < 0 {
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
				doneItem["input"] = codexGatewayDeepSeekCustomToolInput(call.Buffer.String(), call.toolNameMapEntry())
			} else {
				doneItem["type"] = "function_call"
				if namespace := strings.TrimSpace(call.Namespace); namespace != "" {
					doneItem["namespace"] = namespace
				}
				doneItem["arguments"] = call.Buffer.String()
			}
			if rawItem, err := json.Marshal(doneItem); err == nil {
				if call.Kind == CodexGatewayToolKindCustom {
					if err := writer.WriteCustomToolCallInputDone(s.responseID, codexGatewayDeepSeekToolItemID(call.CallID), call.OutputIndex, firstCodexGatewayToolString(doneItem["input"])); err != nil {
						return err
					}
				} else {
					if err := writer.WriteFunctionCallArgumentsDone(s.responseID, codexGatewayDeepSeekToolItemID(call.CallID), call.OutputIndex, rawItem); err != nil {
						return err
					}
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
		if call.OutputIndex < 0 {
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
			item["input"] = codexGatewayDeepSeekCustomToolInput(call.Buffer.String(), call.toolNameMapEntry())
		} else {
			item["type"] = "function_call"
			if namespace := strings.TrimSpace(call.Namespace); namespace != "" {
				item["namespace"] = namespace
			}
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
		if call.OutputIndex < 0 {
			continue
		}
		out = append(out, CodexGatewayStoredToolCall{
			ID:        call.CallID,
			Type:      call.Kind,
			Alias:     call.Alias,
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

func (s *codexGatewayDeepSeekStreamState) responseModel() string {
	if strings.TrimSpace(s.model.Slug) != "" {
		return strings.TrimSpace(s.model.Slug)
	}
	return strings.TrimSpace(s.upstreamModel)
}

func (s *codexGatewayDeepSeekStreamState) hasPartialState() bool {
	return s.messageAdded || len(s.toolCalls) > 0 || s.reasoningPresent || s.reasoningText.Len() > 0 || len(s.usageRaw) > 0
}

func (s *codexGatewayDeepSeekStreamState) shouldPersistToolLoopState() bool {
	if len(s.toolCalls) == 0 {
		return false
	}
	if !s.hasExposedToolCall() {
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
	if codexGatewayIsServerHandledHostedTool(call.Kind, call.Name) {
		return false
	}
	return s.terminalSeen && strings.TrimSpace(s.finishReason) == "tool_calls"
}

func (s *codexGatewayDeepSeekStreamState) hasExposedToolCall() bool {
	for _, call := range s.toolCalls {
		if s.shouldExposeToolCall(call) {
			return true
		}
	}
	return false
}

func (s *codexGatewayDeepSeekStreamState) hasClientVisibleOutputStarted() bool {
	if s.messageAdded {
		return true
	}
	for _, call := range s.toolCalls {
		if call != nil && call.ItemEmitted {
			return true
		}
	}
	return false
}

func (s *codexGatewayDeepSeekStreamState) serverHandledHostedToolCalls() []codexGatewayHostedToolCall {
	if !s.terminalSeen || strings.TrimSpace(s.finishReason) != "tool_calls" {
		return nil
	}
	out := make([]codexGatewayHostedToolCall, 0, len(s.toolCalls))
	for _, call := range s.sortedToolCallsByOutputIndex() {
		if call == nil || !call.Added {
			continue
		}
		if !codexGatewayIsServerHandledHostedTool(call.Kind, call.Name) {
			continue
		}
		out = append(out, codexGatewayHostedToolCall{
			CallID:    call.CallID,
			Name:      call.Name,
			Arguments: call.Buffer.String(),
		})
	}
	return out
}

func codexGatewayDeepSeekRequestWithHostedToolResults(ctx context.Context, req CodexGatewayResponsesCreateRequest, calls []codexGatewayHostedToolCall, search func(context.Context, string) (string, error), toolCtx *codexGatewayHostedToolContext) (CodexGatewayResponsesCreateRequest, error) {
	if len(calls) == 0 {
		return req, nil
	}
	if search == nil {
		search = codexGatewayExecuteHostedWebSearchFunc
	}
	if toolCtx == nil {
		toolCtx = &codexGatewayHostedToolContext{}
	}
	if toolCtx.WebSearchResults == nil {
		toolCtx.WebSearchResults = make(map[string]string)
	}
	items, err := decodeCodexGatewayInputItems(req.Input)
	if err != nil {
		return CodexGatewayResponsesCreateRequest{}, err
	}
	reusedAny := false
	for _, call := range calls {
		if !strings.EqualFold(strings.TrimSpace(call.Name), "web_search") {
			continue
		}
		query := codexGatewayHostedWebSearchQuery(call.Arguments)
		if query == "" {
			query = "web search"
		}
		if toolCtx.VisibleEventSink != nil {
			if err := toolCtx.VisibleEventSink(codexGatewayHostedToolVisibleEvent{
				Phase:     "started",
				CallID:    call.CallID,
				Name:      call.Name,
				Arguments: call.Arguments,
				Query:     query,
			}); err != nil {
				return CodexGatewayResponsesCreateRequest{}, err
			}
		}
		normalizedQuery := normalizeCodexGatewayHostedWebSearchQuery(query)
		output, reused := toolCtx.WebSearchResults[normalizedQuery]
		if !reused {
			output, err = search(ctx, query)
			if err != nil {
				return CodexGatewayResponsesCreateRequest{}, err
			}
			toolCtx.WebSearchResults[normalizedQuery] = output
		} else {
			reusedAny = true
			output = codexGatewayHostedWebSearchRepeatedOutput(query, output)
		}
		if toolCtx.VisibleEventSink != nil {
			if err := toolCtx.VisibleEventSink(codexGatewayHostedToolVisibleEvent{
				Phase:     "completed",
				CallID:    call.CallID,
				Name:      call.Name,
				Arguments: call.Arguments,
				Query:     query,
				Output:    output,
				Reused:    reused,
			}); err != nil {
				return CodexGatewayResponsesCreateRequest{}, err
			}
		}
		items = append(items,
			map[string]any{
				"type":      "function_call",
				"call_id":   call.CallID,
				"name":      call.Name,
				"arguments": call.Arguments,
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": call.CallID,
				"output":  output,
			},
		)
	}
	rawInput, err := json.Marshal(items)
	if err != nil {
		return CodexGatewayResponsesCreateRequest{}, err
	}
	next := req
	next.Input = rawInput
	if reusedAny {
		next.Tools = codexGatewayRemoveHostedWebSearchTool(next.Tools)
		next.ToolChoice = nil
	}
	return next, nil
}

func codexGatewayHostedWebSearchQuery(arguments string) string {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(arguments)), &parsed); err != nil {
		return strings.TrimSpace(arguments)
	}
	for _, key := range []string{"query", "q", "search_query"} {
		if value := strings.TrimSpace(firstCodexGatewayToolString(parsed[key])); value != "" {
			return value
		}
	}
	return strings.TrimSpace(arguments)
}

func normalizeCodexGatewayHostedWebSearchQuery(query string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(query)), " "))
}

func codexGatewayHostedWebSearchRepeatedOutput(query, previousOutput string) string {
	payload := map[string]any{
		"query":           query,
		"note":            "This exact web search query has already been executed in this response. Use the previous search result below and continue the answer without requesting the same search again.",
		"previous_result": previousOutput,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "This exact web search query has already been executed. Use the previous search result and continue without requesting the same search again.\n\n" + previousOutput
	}
	return string(raw)
}

func codexGatewayRemoveHostedWebSearchTool(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var tools []any
	if err := json.Unmarshal(raw, &tools); err != nil {
		return raw
	}
	filtered := make([]any, 0, len(tools))
	for _, tool := range tools {
		if !codexGatewayToolIsHostedWebSearch(tool) {
			filtered = append(filtered, tool)
		}
	}
	next, err := json.Marshal(filtered)
	if err != nil {
		return raw
	}
	return next
}

func codexGatewayToolIsHostedWebSearch(raw any) bool {
	tool, ok := raw.(map[string]any)
	if !ok {
		return false
	}
	toolType := strings.TrimSpace(firstCodexGatewayToolString(tool["type"]))
	if toolType == "" {
		return false
	}
	return normalizeCodexGatewayHostedToolName(toolType) == "web_search" && isCodexGatewayHostedResponsesToolType(toolType)
}

func codexGatewayWriteVisibleHostedWebSearchEvent(writer *CodexGatewayResponseEventWriter, responseID string, outputIndex int, event codexGatewayHostedToolVisibleEvent) error {
	if writer == nil {
		return nil
	}
	phase := strings.TrimSpace(event.Phase)
	status := "completed"
	if phase == "started" {
		status = "in_progress"
	}
	itemID := codexGatewayHostedWebSearchItemID(event.CallID, event.Query)
	item := map[string]any{
		"id":      itemID,
		"type":    "web_search_call",
		"status":  status,
		"call_id": event.CallID,
		"action": map[string]any{
			"type":  "search",
			"query": event.Query,
		},
	}
	rawItem, err := json.Marshal(item)
	if err != nil {
		return err
	}
	switch phase {
	case "started":
		if err := writer.WriteOutputItemAdded(responseID, outputIndex, rawItem); err != nil {
			return err
		}
		if err := writer.WriteWebSearchCallEvent("in_progress", responseID, itemID, outputIndex, outputIndex); err != nil {
			return err
		}
		return writer.WriteWebSearchCallEvent("searching", responseID, itemID, outputIndex, outputIndex)
	case "completed":
		if err := writer.WriteWebSearchCallEvent("completed", responseID, itemID, outputIndex, outputIndex); err != nil {
			return err
		}
		return writer.WriteOutputItemDone(responseID, outputIndex, rawItem)
	}
	if err := writer.WriteOutputItemAdded(responseID, outputIndex, rawItem); err != nil {
		return err
	}
	return writer.WriteOutputItemDone(responseID, outputIndex, rawItem)
}

func codexGatewayHostedWebSearchItemID(callID, query string) string {
	base := strings.TrimSpace(callID)
	if base != "" {
		return "ws_" + sanitizeCodexGatewayToolName(base)
	}
	sum := sha1.Sum([]byte(strings.TrimSpace(query)))
	return "ws_" + fmt.Sprintf("%x", sum[:6])
}

func codexGatewayExecuteHostedWebSearch(ctx context.Context, query string) (string, error) {
	resp, provider, err := doWebSearch(ctx, nil, query)
	if err != nil {
		return "", err
	}
	return codexGatewayHostedWebSearchOutput(query, provider, resp), nil
}

func codexGatewayHostedWebSearchOutput(query, provider string, resp *websearch.SearchResponse) string {
	if resp == nil {
		return "No search results found for: " + query
	}
	payload := map[string]any{
		"query":    query,
		"provider": provider,
		"results":  resp.Results,
		"summary":  buildTextSummary(query, resp.Results),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return buildTextSummary(query, resp.Results)
	}
	return string(raw)
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
	if s.messageAdded {
		indexes = append(indexes, s.messageIndex)
	}
	for _, call := range s.sortedToolCallsByOutputIndex() {
		if call.OutputIndex >= 0 {
			indexes = append(indexes, call.OutputIndex)
		}
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

type codexGatewayDeferredStreamWriter struct {
	target  io.Writer
	buffer  bytes.Buffer
	flushed bool
}

func newCodexGatewayDeferredStreamWriter(target io.Writer) *codexGatewayDeferredStreamWriter {
	return &codexGatewayDeferredStreamWriter{target: target}
}

func (w *codexGatewayDeferredStreamWriter) Write(p []byte) (int, error) {
	if w.flushed {
		return w.target.Write(p)
	}
	return w.buffer.Write(p)
}

func (w *codexGatewayDeferredStreamWriter) Flush() error {
	if w.flushed {
		return nil
	}
	w.flushed = true
	if w.buffer.Len() == 0 {
		return nil
	}
	_, err := io.Copy(w.target, &w.buffer)
	return err
}

func (w *codexGatewayDeferredStreamWriter) Flushed() bool {
	return w != nil && w.flushed
}
