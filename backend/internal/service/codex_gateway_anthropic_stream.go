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
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const codexGatewayAnthropicHostedToolMaxTurns = 64

func ExecuteCodexGatewayAnthropicStream(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	apiKey string,
	model CodexGatewayModel,
	req CodexGatewayResponsesCreateRequest,
	stateStore *CodexGatewayStateStore,
	reqCtx CodexGatewayAnthropicRequestContext,
	cfg CodexGatewayAnthropicRequestConfig,
	dst io.Writer,
) (CodexGatewayDeepSeekAdapterResult, error) {
	return executeCodexGatewayAnthropicStreamWithHostedToolTurns(ctx, client, baseURL, apiKey, model, req, stateStore, reqCtx, cfg, dst, 0)
}

func executeCodexGatewayAnthropicStreamWithHostedToolTurns(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	apiKey string,
	model CodexGatewayModel,
	req CodexGatewayResponsesCreateRequest,
	stateStore *CodexGatewayStateStore,
	reqCtx CodexGatewayAnthropicRequestContext,
	cfg CodexGatewayAnthropicRequestConfig,
	dst io.Writer,
	turn int,
) (CodexGatewayDeepSeekAdapterResult, error) {
	if dst == nil {
		return CodexGatewayDeepSeekAdapterResult{}, fmt.Errorf("codex anthropic stream requires destination writer")
	}
	if cfg.HostedToolContext == nil {
		cfg.HostedToolContext = &codexGatewayHostedToolContext{WebSearchResults: make(map[string]string)}
	} else if cfg.HostedToolContext.WebSearchResults == nil {
		cfg.HostedToolContext.WebSearchResults = make(map[string]string)
	}
	if turn > codexGatewayAnthropicHostedToolMaxTurns {
		return CodexGatewayDeepSeekAdapterResult{}, fmt.Errorf("codex anthropic hosted tool loop exceeded %d turns", codexGatewayAnthropicHostedToolMaxTurns)
	}
	prepared, err := BuildCodexGatewayAnthropicRequest(model, req, stateStore, reqCtx, cfg)
	if err != nil {
		return CodexGatewayDeepSeekAdapterResult{}, err
	}
	body := cloneCodexGatewayStreamBody(prepared.Body)
	body["stream"] = true
	rawBody, err := json.Marshal(body)
	if err != nil {
		return CodexGatewayDeepSeekAdapterResult{}, fmt.Errorf("marshal codex anthropic stream request: %w", err)
	}
	if client == nil {
		client = http.DefaultClient
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, buildAnthropicMessagesURL(baseURL), bytes.NewReader(rawBody))
	if err != nil {
		return CodexGatewayDeepSeekAdapterResult{}, fmt.Errorf("build codex anthropic stream request: %w", err)
	}
	setCodexGatewayAnthropicHeaders(httpReq, apiKey, true)
	setCodexGatewayAnthropicWebSearchBetaHeader(httpReq, rawBody)
	codexGatewayCaptureUpstreamRequest(reqCtx.CaptureTrace, "anthropic", httpReq.Header, rawBody)

	resp, err := client.Do(httpReq)
	if err != nil {
		body, _ := json.Marshal(map[string]any{
			"type": "error",
			"error": map[string]string{
				"type":    "upstream_disconnected",
				"message": "upstream stream disconnected: " + sanitizeStreamError(err),
			},
		})
		return CodexGatewayDeepSeekAdapterResult{}, &UpstreamFailoverError{
			StatusCode:             http.StatusBadGateway,
			ResponseBody:           body,
			RetryableOnSameAccount: true,
		}
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
		result.ServiceResponse.Body = codexGatewayAnthropicMapErrorBody(resp.StatusCode, bodyBytes)
		if codexGatewayAnthropicShouldFailoverUpstreamResponse(resp.StatusCode, resp.Header, bodyBytes) {
			return CodexGatewayDeepSeekAdapterResult{}, &UpstreamFailoverError{
				StatusCode:      resp.StatusCode,
				ResponseBody:    result.ServiceResponse.Body,
				ResponseHeaders: resp.Header.Clone(),
			}
		}
		errResp := CodexGatewayResponse{
			Object: "response",
			Status: "failed",
			Output: []json.RawMessage{},
			Error: &CodexGatewayResponseError{
				Code:    "upstream_error",
				Message: gjson.GetBytes(result.ServiceResponse.Body, "error.message").String(),
				RawFields: map[string]json.RawMessage{
					"type": json.RawMessage(fmt.Sprintf("%q", gjson.GetBytes(result.ServiceResponse.Body, "error.type").String())),
				},
			},
		}
		result.ProviderResult.Response = errResp
		if err := writer.WriteResponseFailed(errResp); err != nil {
			return CodexGatewayDeepSeekAdapterResult{}, err
		}
		return result, nil
	}

	state := newCodexGatewayAnthropicStreamState(model, prepared.ToolNameMap, prepared.ReplayMessages)
	state.applyHostedToolContext(cfg.HostedToolContext)
	deferredWriter := newCodexGatewayDeferredStreamWriter(dst)
	writer := NewCodexGatewayResponseEventWriterWithSequence(deferredWriter, cfg.StreamSequenceNumber)
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
				codexGatewayCaptureUpstreamStreamEvent(reqCtx.CaptureTrace, "anthropic.done", []byte(`{"done":true}`))
			}
			return nil
		}
		payloadBytes := []byte(payload)
		eventType := strings.TrimSpace(gjson.GetBytes(payloadBytes, "type").String())
		if eventType == "" {
			eventType = "anthropic.message"
		}
		codexGatewayCaptureUpstreamStreamEvent(reqCtx.CaptureTrace, eventType, payloadBytes)
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
		if err := state.writeDoneEvents(writer); err != nil {
			return CodexGatewayDeepSeekAdapterResult{}, err
		}
		cfg.HostedToolContext.ensureSyntheticResponse(state.responseID, state.nextOutputIndex)
		cfg.HostedToolContext.rememberOutputItems(state.outputItemsByIndex())
		if err := deferredWriter.Flush(); err != nil {
			return CodexGatewayDeepSeekAdapterResult{}, err
		}
		cfg.HostedToolContext.VisibleEventSink = func(event codexGatewayHostedToolVisibleEvent) error {
			outputIndex := cfg.HostedToolContext.hostedOutputIndex(event.CallID, event.Query)
			if err := codexGatewayWriteVisibleHostedWebSearchEvent(writer, cfg.HostedToolContext.RootResponseID, outputIndex, event); err != nil {
				return err
			}
			cfg.HostedToolContext.rememberHostedWebSearchItem(outputIndex, event)
			return nil
		}
		nextReq, err := codexGatewayDeepSeekRequestWithHostedToolResults(ctx, req, calls, cfg.HostedWebSearch, cfg.HostedToolContext)
		if err != nil {
			return CodexGatewayDeepSeekAdapterResult{}, err
		}
		cfg.StreamSequenceNumber = writer.NextSequenceNumber()
		return executeCodexGatewayAnthropicStreamWithHostedToolTurns(ctx, client, baseURL, apiKey, model, nextReq, stateStore, reqCtx, cfg, dst, turn+1)
	}
	if state.terminalSeen {
		if _, err := state.finish(writer); err != nil {
			return CodexGatewayDeepSeekAdapterResult{}, err
		}
	} else if codexGatewayAnthropicCanRetryZeroEventToolReplay(req, cfg, body, state) {
		retryCfg := cfg
		retryCfg.ForceDisableThinking = true
		return ExecuteCodexGatewayAnthropicStream(ctx, client, baseURL, apiKey, model, req, stateStore, reqCtx, retryCfg, dst)
	} else if _, err := state.finishEarly(writer); err != nil {
		return CodexGatewayDeepSeekAdapterResult{}, err
	}
	if err := deferredWriter.Flush(); err != nil {
		return CodexGatewayDeepSeekAdapterResult{}, err
	}
	if state.shouldPersistToolLoopState() {
		if err := codexGatewayAnthropicPersistState(stateStore, state.responseID, codexGatewayAnthropicStateModelKey(model, state.upstreamModel), reqCtx, state.messageText.String(), state.messageAdded, state.reasoningText.String(), state.reasoningPresent, state.storedThinkingBlocks(), state.storedToolCalls(), prepared.ToolNameMap, state.replayMessages); err != nil {
			return CodexGatewayDeepSeekAdapterResult{}, err
		}
	}
	result.ProviderResult = state.providerResult(resp.Header.Get("x-request-id"))
	return result, nil
}

type codexGatewayAnthropicStreamState struct {
	model             CodexGatewayModel
	toolNameMap       map[string]CodexGatewayToolNameMapEntry
	responseID        string
	upstreamModel     string
	messageID         string
	messageText       strings.Builder
	messageAdded      bool
	messageEmitted    bool
	messageIndex      int
	reasoningText     strings.Builder
	reasoningPresent  bool
	nextOutputIndex   int
	toolCalls         map[int]*codexGatewayAnthropicStreamToolCall
	toolOrder         []int
	usage             CodexGatewayProviderUsage
	usageRaw          json.RawMessage
	thinkingBlocks    map[int]*codexGatewayAnthropicStreamThinkingBlock
	thinkingOrder     []int
	finishReason      string
	terminalSeen      bool
	createdSent       bool
	replayMessages    []json.RawMessage
	prefixOutputItems map[int]json.RawMessage
}

type codexGatewayAnthropicStreamThinkingBlock struct {
	Index     int
	Type      string
	Thinking  strings.Builder
	Signature string
	Data      string
}

type codexGatewayAnthropicStreamToolCall struct {
	Index       int
	OutputIndex int
	CallID      string
	Alias       string
	Name        string
	Namespace   string
	Kind        string
	Buffer      strings.Builder
	ItemEmitted bool
	EmittedLen  int
}

func newCodexGatewayAnthropicStreamState(model CodexGatewayModel, toolNameMap map[string]CodexGatewayToolNameMapEntry, replayMessages []json.RawMessage) *codexGatewayAnthropicStreamState {
	return &codexGatewayAnthropicStreamState{
		model:          model,
		toolNameMap:    cloneCodexGatewayToolNameMap(toolNameMap),
		thinkingBlocks: make(map[int]*codexGatewayAnthropicStreamThinkingBlock),
		toolCalls:      make(map[int]*codexGatewayAnthropicStreamToolCall),
		replayMessages: cloneCodexGatewayRawMessages(replayMessages),
	}
}

func (s *codexGatewayAnthropicStreamState) applyHostedToolContext(ctx *codexGatewayHostedToolContext) {
	if s == nil || ctx == nil || strings.TrimSpace(ctx.RootResponseID) == "" {
		return
	}
	s.responseID = strings.TrimSpace(ctx.RootResponseID)
	s.messageID = codexGatewayAnthropicMessageID(s.responseID, ctx.NextOutputIndex)
	s.nextOutputIndex = ctx.NextOutputIndex
	s.createdSent = true
	s.prefixOutputItems = cloneCodexGatewayIndexedRawMessages(ctx.OutputItems)
}

func (s *codexGatewayAnthropicStreamState) consumePayload(payload []byte, writer *CodexGatewayResponseEventWriter) error {
	eventType := gjson.GetBytes(payload, "type").String()
	switch eventType {
	case "message_start":
		message := gjson.GetBytes(payload, "message")
		if s.responseID == "" {
			s.responseID = strings.TrimSpace(message.Get("id").String())
			if s.responseID == "" {
				s.responseID = "msg_stream"
			}
		}
		s.upstreamModel = strings.TrimSpace(message.Get("model").String())
		if s.messageID == "" {
			s.messageID = codexGatewayAnthropicMessageID(s.responseID, s.nextOutputIndex)
		}
		s.mergeUsage(message.Get("usage"))
		return s.ensureCreated(writer)
	case "content_block_start":
		if err := s.ensureCreated(writer); err != nil {
			return err
		}
		index := int(gjson.GetBytes(payload, "index").Int())
		block := gjson.GetBytes(payload, "content_block")
		switch block.Get("type").String() {
		case "text":
			return s.ensureMessage(writer)
		case "thinking":
			s.reasoningPresent = true
			s.startThinkingBlock(index, block)
		case "redacted_thinking":
			s.startRedactedThinkingBlock(index, block)
		case "tool_use":
			return s.startToolUse(index, block, writer)
		}
	case "content_block_delta":
		index := int(gjson.GetBytes(payload, "index").Int())
		delta := gjson.GetBytes(payload, "delta")
		switch delta.Get("type").String() {
		case "text_delta":
			if err := s.ensureMessage(writer); err != nil {
				return err
			}
			text := delta.Get("text").String()
			s.messageText.WriteString(text)
			return nil
		case "thinking_delta":
			s.reasoningPresent = true
			text := delta.Get("thinking").String()
			s.reasoningText.WriteString(text)
			s.appendThinkingDelta(index, text)
		case "signature_delta":
			s.setThinkingSignature(index, delta.Get("signature").String())
		case "input_json_delta":
			return s.consumeToolInputJSONDelta(index, delta.Get("partial_json").String(), writer)
		}
	case "content_block_stop":
		index := int(gjson.GetBytes(payload, "index").Int())
		return s.stopContentBlock(index, writer)
	case "message_delta":
		delta := gjson.GetBytes(payload, "delta")
		if reason := strings.TrimSpace(delta.Get("stop_reason").String()); reason != "" {
			s.finishReason = reason
		}
		s.mergeUsage(gjson.GetBytes(payload, "usage"))
	case "message_stop":
		s.terminalSeen = true
	case "error":
		return fmt.Errorf("codex anthropic stream error: %s", gjson.GetBytes(payload, "error.message").String())
	}
	return nil
}

func codexGatewayAnthropicCanRetryZeroEventToolReplay(req CodexGatewayResponsesCreateRequest, cfg CodexGatewayAnthropicRequestConfig, body map[string]any, state *codexGatewayAnthropicStreamState) bool {
	if cfg.ForceDisableThinking || state == nil || state.terminalSeen || state.hasPartialState() || state.createdSent {
		return false
	}
	if !codexGatewayAnthropicInputHasToolReplay(req.Input) {
		return false
	}
	thinking, _ := body["thinking"].(map[string]any)
	return !strings.EqualFold(strings.TrimSpace(firstCodexGatewayToolString(thinking["type"])), "disabled")
}

func codexGatewayAnthropicInputHasToolReplay(input json.RawMessage) bool {
	return bytes.Contains(input, []byte(`"function_call_output"`)) || bytes.Contains(input, []byte(`"custom_tool_call_output"`))
}

func (s *codexGatewayAnthropicStreamState) ensureCreated(writer *CodexGatewayResponseEventWriter) error {
	if s.createdSent {
		return nil
	}
	if s.responseID == "" {
		s.responseID = "msg_stream"
	}
	if s.messageID == "" {
		s.messageID = codexGatewayAnthropicMessageID(s.responseID, 0)
	}
	created := CodexGatewayResponse{
		ID:     s.responseID,
		Object: "response",
		Model:  codexGatewayAnthropicResponseModel(s.model, s.upstreamModel),
		Status: "in_progress",
		Output: []json.RawMessage{},
	}
	if err := writer.WriteResponseCreated(created); err != nil {
		return err
	}
	if err := writer.WriteResponseInProgress(created); err != nil {
		return err
	}
	s.createdSent = true
	return nil
}

func (s *codexGatewayAnthropicStreamState) ensureMessage(writer *CodexGatewayResponseEventWriter) error {
	if s.messageAdded {
		return nil
	}
	if err := s.ensureCreated(writer); err != nil {
		return err
	}
	s.messageIndex = s.nextOutputIndex
	s.nextOutputIndex++
	s.messageAdded = true
	return nil
}

func (s *codexGatewayAnthropicStreamState) startToolUse(index int, block gjson.Result, writer *CodexGatewayResponseEventWriter) error {
	if err := s.ensureCreated(writer); err != nil {
		return err
	}
	call := s.toolCalls[index]
	if call == nil {
		call = &codexGatewayAnthropicStreamToolCall{Index: index, OutputIndex: -1}
		s.toolCalls[index] = call
		s.toolOrder = append(s.toolOrder, index)
	}
	call.CallID = strings.TrimSpace(block.Get("id").String())
	call.Alias = strings.TrimSpace(block.Get("name").String())
	entry, ok := s.toolNameMap[call.Alias]
	if ok {
		call.Name = codexGatewayClientVisibleToolName(entry)
		call.Namespace = entry.Namespace
		call.Kind = entry.Kind
	} else {
		call.Name = call.Alias
		call.Kind = CodexGatewayToolKindFunction
	}
	if input := strings.TrimSpace(block.Get("input").Raw); input != "" && input != "{}" {
		call.Buffer.WriteString(input)
	}
	if codexGatewayIsServerHandledHostedTool(call.Kind, call.Name) {
		return nil
	}
	if codexGatewayClientVisibleToolItemType(CodexGatewayToolNameMapEntry{
		Alias:     call.Alias,
		Kind:      call.Kind,
		Namespace: call.Namespace,
		Name:      call.Name,
	}) == CodexGatewayOutputItemTypeLocalShellCall && strings.TrimSpace(codexGatewayExtractShellExecCmd(call.Buffer.String())) == "" {
		return nil
	}
	return s.emitToolUseItem(call, writer)
}

func (s *codexGatewayAnthropicStreamState) emitToolUseItem(call *codexGatewayAnthropicStreamToolCall, writer *CodexGatewayResponseEventWriter) error {
	if s == nil || call == nil || call.CallID == "" || call.Name == "" || call.ItemEmitted {
		return nil
	}
	itemType := codexGatewayClientVisibleToolItemType(CodexGatewayToolNameMapEntry{
		Alias:     call.Alias,
		Kind:      call.Kind,
		Namespace: call.Namespace,
		Name:      call.Name,
	})
	if itemType == CodexGatewayOutputItemTypeLocalShellCall && strings.TrimSpace(codexGatewayExtractShellExecCmd(call.Buffer.String())) == "" {
		return nil
	}
	call.OutputIndex = s.nextOutputIndex
	s.nextOutputIndex++
	item := map[string]any{
		"id":      codexGatewayAnthropicToolItemID(call.CallID),
		"call_id": call.CallID,
		"name":    call.Name,
		"status":  "in_progress",
	}
	switch itemType {
	case CodexGatewayOutputItemTypeCustomToolCall:
		item["type"] = CodexGatewayOutputItemTypeCustomToolCall
		item["input"] = ""
	case CodexGatewayOutputItemTypeLocalShellCall:
		codexGatewayApplyLocalShellCallItemFields(item, call.CallID, "in_progress", call.Buffer.String())
	default:
		item["type"] = codexGatewayClientVisibleToolItemType(CodexGatewayToolNameMapEntry{
			Alias:     call.Alias,
			Kind:      call.Kind,
			Namespace: call.Namespace,
			Name:      call.Name,
		})
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
	return nil
}

func (s *codexGatewayAnthropicStreamState) startThinkingBlock(index int, block gjson.Result) {
	if s.thinkingBlocks == nil {
		s.thinkingBlocks = make(map[int]*codexGatewayAnthropicStreamThinkingBlock)
	}
	if s.thinkingBlocks[index] == nil {
		s.thinkingBlocks[index] = &codexGatewayAnthropicStreamThinkingBlock{Index: index, Type: "thinking"}
		s.thinkingOrder = append(s.thinkingOrder, index)
	}
	if text := block.Get("thinking").String(); text != "" {
		s.thinkingBlocks[index].Thinking.WriteString(text)
	}
	if sig := strings.TrimSpace(block.Get("signature").String()); sig != "" {
		s.thinkingBlocks[index].Signature = sig
	}
}

func (s *codexGatewayAnthropicStreamState) startRedactedThinkingBlock(index int, block gjson.Result) {
	if s.thinkingBlocks == nil {
		s.thinkingBlocks = make(map[int]*codexGatewayAnthropicStreamThinkingBlock)
	}
	if s.thinkingBlocks[index] == nil {
		s.thinkingBlocks[index] = &codexGatewayAnthropicStreamThinkingBlock{Index: index, Type: "redacted_thinking"}
		s.thinkingOrder = append(s.thinkingOrder, index)
	}
	s.reasoningPresent = true
	s.thinkingBlocks[index].Data = block.Get("data").String()
}

func (s *codexGatewayAnthropicStreamState) appendThinkingDelta(index int, text string) {
	if text == "" {
		return
	}
	if s.thinkingBlocks == nil || s.thinkingBlocks[index] == nil {
		s.startThinkingBlock(index, gjson.Result{})
	}
	s.thinkingBlocks[index].Thinking.WriteString(text)
}

func (s *codexGatewayAnthropicStreamState) setThinkingSignature(index int, signature string) {
	signature = strings.TrimSpace(signature)
	if signature == "" {
		return
	}
	if s.thinkingBlocks == nil || s.thinkingBlocks[index] == nil {
		s.startThinkingBlock(index, gjson.Result{})
	}
	s.thinkingBlocks[index].Signature = signature
}

func (s *codexGatewayAnthropicStreamState) consumeToolInputJSONDelta(index int, delta string, writer *CodexGatewayResponseEventWriter) error {
	call := s.toolCalls[index]
	if call == nil {
		call = &codexGatewayAnthropicStreamToolCall{Index: index, OutputIndex: -1, Kind: CodexGatewayToolKindFunction}
		s.toolCalls[index] = call
		s.toolOrder = append(s.toolOrder, index)
	}
	call.Buffer.WriteString(delta)
	if !call.ItemEmitted {
		if !codexGatewayIsServerHandledHostedTool(call.Kind, call.Name) {
			if err := s.emitToolUseItem(call, writer); err != nil {
				return err
			}
		}
	}
	if !call.ItemEmitted {
		return nil
	}
	args := call.Buffer.String()
	if call.Kind == CodexGatewayToolKindCustom {
		input, ready := codexGatewayDeepSeekCustomToolStreamInput(args, CodexGatewayToolNameMapEntry{Alias: call.Alias, Kind: call.Kind, Name: call.Name})
		if !ready {
			return nil
		}
		if len(input) > call.EmittedLen {
			deltaText := input[call.EmittedLen:]
			call.EmittedLen = len(input)
			return writer.WriteCustomToolCallInputDelta(s.responseID, codexGatewayAnthropicToolItemID(call.CallID), call.CallID, call.OutputIndex, deltaText)
		}
		return nil
	}
	itemType := codexGatewayClientVisibleToolItemType(CodexGatewayToolNameMapEntry{
		Alias:     call.Alias,
		Kind:      call.Kind,
		Namespace: call.Namespace,
		Name:      call.Name,
	})
	if codexGatewayAnthropicShouldDelayFunctionArgumentDeltas(call) || itemType == CodexGatewayOutputItemTypeLocalShellCall {
		return nil
	}
	if len(args) > call.EmittedLen {
		deltaText := args[call.EmittedLen:]
		call.EmittedLen = len(args)
		return writer.WriteFunctionCallArgumentsDelta(s.responseID, codexGatewayAnthropicToolItemID(call.CallID), call.OutputIndex, deltaText)
	}
	return nil
}

func (s *codexGatewayAnthropicStreamState) stopContentBlock(index int, writer *CodexGatewayResponseEventWriter) error {
	call := s.toolCalls[index]
	if call == nil || !call.ItemEmitted || !codexGatewayAnthropicShouldDelayFunctionArgumentDeltas(call) {
		return nil
	}
	if codexGatewayClientVisibleToolItemType(CodexGatewayToolNameMapEntry{
		Alias:     call.Alias,
		Kind:      call.Kind,
		Namespace: call.Namespace,
		Name:      call.Name,
	}) == CodexGatewayOutputItemTypeLocalShellCall {
		return nil
	}
	args := codexGatewayAnthropicFunctionToolArguments(call)
	if len(args) <= call.EmittedLen {
		return nil
	}
	deltaText := args[call.EmittedLen:]
	call.EmittedLen = len(args)
	return writer.WriteFunctionCallArgumentsDelta(s.responseID, codexGatewayAnthropicToolItemID(call.CallID), call.OutputIndex, deltaText)
}

func (s *codexGatewayAnthropicStreamState) mergeUsage(usage gjson.Result) {
	if !usage.Exists() {
		return
	}
	_, parsed := codexGatewayAnthropicUsageJSONFromResult(usage)
	if parsed.InputTokens > 0 {
		s.usage.InputTokens = parsed.InputTokens
	}
	if parsed.OutputTokens > 0 {
		s.usage.OutputTokens = parsed.OutputTokens
	}
	if parsed.CacheCreationInputTokens > 0 {
		s.usage.CacheCreationInputTokens = parsed.CacheCreationInputTokens
	}
	if parsed.CacheReadInputTokens > 0 {
		s.usage.CacheReadInputTokens = parsed.CacheReadInputTokens
	}
	if parsed.CacheCreation5mTokens > 0 {
		s.usage.CacheCreation5mTokens = parsed.CacheCreation5mTokens
	}
	if parsed.CacheCreation1hTokens > 0 {
		s.usage.CacheCreation1hTokens = parsed.CacheCreation1hTokens
	}
	s.usage.TotalTokens = s.usage.InputTokens + s.usage.OutputTokens + s.usage.CacheCreationInputTokens
	s.usageRaw = codexGatewayAnthropicUsageJSON(s.usage)
}

func (s *codexGatewayAnthropicStreamState) finish(writer *CodexGatewayResponseEventWriter) (string, error) {
	status, reason := codexGatewayAnthropicFinishReasonStatus(s.finishReason)
	response := s.finalResponse(status, reason)
	if err := s.writeDoneEvents(writer); err != nil {
		return "", err
	}
	if status == "completed" {
		return "response.completed", writer.WriteResponseCompleted(response)
	}
	return "response.incomplete", writer.WriteResponseIncomplete(response)
}

func (s *codexGatewayAnthropicStreamState) finishEarly(writer *CodexGatewayResponseEventWriter) (string, error) {
	status := "failed"
	reason := ""
	if s.hasPartialState() {
		status = "incomplete"
		reason = codexGatewayAnthropicStreamClosedReason
	}
	response := s.finalResponse(status, reason)
	if err := s.writeDoneEvents(writer); err != nil {
		return "", err
	}
	if status == "failed" {
		response.Error = &CodexGatewayResponseError{
			Code:    "upstream_error",
			Message: "Anthropic stream ended before completion.",
			RawFields: map[string]json.RawMessage{
				"type": json.RawMessage(`"api_error"`),
			},
		}
		return "response.failed", writer.WriteResponseFailed(response)
	}
	return "response.incomplete", writer.WriteResponseIncomplete(response)
}

func (s *codexGatewayAnthropicStreamState) writeDoneEvents(writer *CodexGatewayResponseEventWriter) error {
	for _, index := range s.sortedOutputIndexes() {
		if s.messageAdded && index == s.messageIndex {
			if !s.shouldExposeMessage() {
				continue
			}
			if err := s.writeMessageEvents(writer); err != nil {
				return err
			}
			continue
		}
		call := s.toolCallByOutputIndex(index)
		if call == nil || !s.shouldExposeToolCall(call) {
			continue
		}
		doneItem := s.toolCallDoneItem(call)
		rawItem, _ := json.Marshal(doneItem)
		itemType := codexGatewayClientVisibleToolItemType(CodexGatewayToolNameMapEntry{
			Alias:     call.Alias,
			Kind:      call.Kind,
			Namespace: call.Namespace,
			Name:      call.Name,
		})
		if call.Kind == CodexGatewayToolKindCustom {
			if err := writer.WriteCustomToolCallInputDone(s.responseID, codexGatewayAnthropicToolItemID(call.CallID), call.OutputIndex, firstCodexGatewayToolString(doneItem["input"])); err != nil {
				return err
			}
		} else if itemType != CodexGatewayOutputItemTypeLocalShellCall {
			if err := writer.WriteFunctionCallArgumentsDone(s.responseID, codexGatewayAnthropicToolItemID(call.CallID), call.OutputIndex, rawItem); err != nil {
				return err
			}
		}
		if err := writer.WriteOutputItemDone(s.responseID, call.OutputIndex, rawItem); err != nil {
			return err
		}
	}
	return nil
}

func (s *codexGatewayAnthropicStreamState) writeMessageEvents(writer *CodexGatewayResponseEventWriter) error {
	if s.messageEmitted {
		return nil
	}
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
	if err := writer.WriteOutputItemAdded(s.responseID, s.messageIndex, rawItem); err != nil {
		return err
	}
	part, _ := json.Marshal(map[string]any{"type": "output_text", "text": ""})
	if err := writer.WriteContentPartAdded(s.responseID, s.messageID, s.messageIndex, 0, part); err != nil {
		return err
	}
	text := s.messageText.String()
	if text != "" {
		if err := writer.WriteOutputTextDelta(s.responseID, s.messageID, s.messageIndex, 0, text); err != nil {
			return err
		}
	}
	if err := writer.WriteOutputTextDone(s.responseID, s.messageID, s.messageIndex, 0, text); err != nil {
		return err
	}
	part, _ = json.Marshal(map[string]any{"type": "output_text", "text": text})
	if err := writer.WriteContentPartDone(s.responseID, s.messageID, s.messageIndex, 0, part); err != nil {
		return err
	}
	rawDone, err := json.Marshal(map[string]any{
		"type":   "message",
		"id":     s.messageID,
		"role":   "assistant",
		"status": "completed",
		"content": []map[string]any{{
			"type": "output_text",
			"text": text,
		}},
	})
	if err != nil {
		return err
	}
	if err := writer.WriteOutputItemDone(s.responseID, s.messageIndex, rawDone); err != nil {
		return err
	}
	s.messageEmitted = true
	return nil
}

func (s *codexGatewayAnthropicStreamState) finalResponse(status, incompleteReason string) CodexGatewayResponse {
	response := CodexGatewayResponse{
		ID:     s.responseID,
		Object: "response",
		Model:  codexGatewayAnthropicResponseModel(s.model, s.upstreamModel),
		Status: status,
		Output: s.outputItems(),
		Usage:  s.usageRaw,
	}
	if status == "incomplete" && incompleteReason != "" {
		response.IncompleteDetails = json.RawMessage(fmt.Sprintf(`{"reason":%q}`, incompleteReason))
	}
	return response
}

func (s *codexGatewayAnthropicStreamState) outputItems() []json.RawMessage {
	byIndex := s.outputItemsByIndex()
	indexes := make([]int, 0, len(byIndex))
	for index := range byIndex {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	out := make([]json.RawMessage, 0, len(indexes))
	for _, index := range indexes {
		out = append(out, byIndex[index])
	}
	return out
}

func (s *codexGatewayAnthropicStreamState) outputItemsByIndex() map[int]json.RawMessage {
	byIndex := cloneCodexGatewayIndexedRawMessages(s.prefixOutputItems)
	if byIndex == nil {
		byIndex = make(map[int]json.RawMessage, 1+len(s.toolCalls))
	}
	if s.shouldExposeMessage() {
		rawItem, _ := json.Marshal(map[string]any{
			"type":   "message",
			"id":     s.messageID,
			"role":   "assistant",
			"status": "completed",
			"content": []map[string]any{{
				"type": "output_text",
				"text": s.messageText.String(),
			}},
		})
		byIndex[s.messageIndex] = rawItem
	}
	for _, call := range s.sortedToolCallsByOutputIndex() {
		if !s.shouldExposeToolCall(call) {
			continue
		}
		rawItem, _ := json.Marshal(s.toolCallDoneItem(call))
		byIndex[call.OutputIndex] = rawItem
	}
	return byIndex
}

func (s *codexGatewayAnthropicStreamState) toolCallDoneItem(call *codexGatewayAnthropicStreamToolCall) map[string]any {
	item := map[string]any{
		"id":      codexGatewayAnthropicToolItemID(call.CallID),
		"call_id": call.CallID,
		"name":    call.Name,
		"status":  "completed",
	}
	switch codexGatewayClientVisibleToolItemType(CodexGatewayToolNameMapEntry{
		Alias:     call.Alias,
		Kind:      call.Kind,
		Namespace: call.Namespace,
		Name:      call.Name,
	}) {
	case CodexGatewayOutputItemTypeCustomToolCall:
		item["type"] = CodexGatewayOutputItemTypeCustomToolCall
		item["input"] = codexGatewayDeepSeekCustomToolInput(call.Buffer.String(), CodexGatewayToolNameMapEntry{Alias: call.Alias, Kind: call.Kind, Name: call.Name})
	case CodexGatewayOutputItemTypeLocalShellCall:
		codexGatewayApplyLocalShellCallItemFields(item, call.CallID, "completed", call.Buffer.String())
	default:
		item["type"] = codexGatewayClientVisibleToolItemType(CodexGatewayToolNameMapEntry{
			Alias:     call.Alias,
			Kind:      call.Kind,
			Namespace: call.Namespace,
			Name:      call.Name,
		})
		if namespace := strings.TrimSpace(call.Namespace); namespace != "" {
			item["namespace"] = namespace
		}
		item["arguments"] = codexGatewayAnthropicFunctionToolArguments(call)
	}
	return item
}

func (s *codexGatewayAnthropicStreamState) providerResult(upstreamRequestID string) CodexGatewayProviderResult {
	status, reason := codexGatewayAnthropicFinishReasonStatus(s.finishReason)
	if !s.terminalSeen {
		if s.hasPartialState() {
			status = "incomplete"
			reason = codexGatewayAnthropicStreamClosedReason
		} else {
			status = "failed"
		}
	}
	response := s.finalResponse(status, reason)
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

func (s *codexGatewayAnthropicStreamState) storedToolCalls() []CodexGatewayStoredToolCall {
	out := make([]CodexGatewayStoredToolCall, 0, len(s.toolCalls))
	for _, call := range s.sortedToolCallsByOutputIndex() {
		if !s.shouldExposeToolCall(call) {
			continue
		}
		out = append(out, CodexGatewayStoredToolCall{
			ID:        call.CallID,
			Type:      call.Kind,
			Alias:     call.Alias,
			Name:      call.Name,
			Arguments: codexGatewayAnthropicFunctionToolArguments(call),
		})
	}
	return out
}

func codexGatewayAnthropicShouldDelayFunctionArgumentDeltas(call *codexGatewayAnthropicStreamToolCall) bool {
	if call == nil || call.Kind == CodexGatewayToolKindCustom {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(call.Name), "wait_agent")
}

func codexGatewayAnthropicFunctionToolArguments(call *codexGatewayAnthropicStreamToolCall) string {
	if call == nil {
		return ""
	}
	args := call.Buffer.String()
	if call.Kind == CodexGatewayToolKindCustom {
		return args
	}
	if next, changed := codexGatewayNormalizeFunctionToolArguments(call.Name, args); changed {
		return next
	}
	return args
}

func codexGatewayNormalizeFunctionToolArguments(toolName, argsJSON string) (string, bool) {
	if !strings.EqualFold(strings.TrimSpace(toolName), "wait_agent") {
		return argsJSON, false
	}
	argsJSON = strings.TrimSpace(argsJSON)
	if argsJSON == "" || !gjson.Valid(argsJSON) || !gjson.Parse(argsJSON).IsObject() {
		return argsJSON, false
	}

	updated := argsJSON
	changed := false
	targets := gjson.Get(updated, "targets")
	if targets.Exists() && targets.Type == gjson.String {
		if next, ok := codexGatewayNormalizeWaitAgentTargets(updated, strings.TrimSpace(targets.Str)); ok {
			updated = next
			changed = true
		}
	}
	timeout := gjson.Get(updated, "timeout_ms")
	if timeout.Exists() && timeout.Type == gjson.String {
		if n, err := strconv.ParseInt(strings.TrimSpace(timeout.Str), 10, 64); err == nil && n >= 0 {
			if next, err := sjson.Set(updated, "timeout_ms", n); err == nil {
				updated = next
				changed = true
			}
		}
	}
	return updated, changed
}

func codexGatewayNormalizeWaitAgentTargets(argsJSON, raw string) (string, bool) {
	if raw == "" {
		return argsJSON, false
	}
	if gjson.Valid(raw) && gjson.Parse(raw).IsArray() {
		next, err := sjson.SetRaw(argsJSON, "targets", raw)
		return next, err == nil
	}
	parts := strings.Split(raw, ",")
	targets := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			targets = append(targets, part)
		}
	}
	if len(targets) == 0 {
		return argsJSON, false
	}
	rawTargets, err := json.Marshal(targets)
	if err != nil {
		return argsJSON, false
	}
	next, err := sjson.SetRaw(argsJSON, "targets", string(rawTargets))
	return next, err == nil
}

func (s *codexGatewayAnthropicStreamState) storedThinkingBlocks() []json.RawMessage {
	if len(s.thinkingBlocks) == 0 {
		return nil
	}
	out := make([]json.RawMessage, 0, len(s.thinkingBlocks))
	for _, index := range s.sortedThinkingOrder() {
		block := s.thinkingBlocks[index]
		if block == nil {
			continue
		}
		var item map[string]any
		switch block.Type {
		case "redacted_thinking":
			if strings.TrimSpace(block.Data) == "" {
				continue
			}
			item = map[string]any{"type": "redacted_thinking", "data": block.Data}
		default:
			thinking := block.Thinking.String()
			if thinking == "" && strings.TrimSpace(block.Signature) == "" {
				continue
			}
			item = map[string]any{"type": "thinking", "thinking": thinking}
			if strings.TrimSpace(block.Signature) != "" {
				item["signature"] = block.Signature
			}
		}
		raw, err := json.Marshal(item)
		if err == nil && len(raw) > 0 {
			out = append(out, raw)
		}
	}
	return out
}

func (s *codexGatewayAnthropicStreamState) hasPartialState() bool {
	return s.messageAdded || len(s.toolCalls) > 0 || s.reasoningPresent || len(s.usageRaw) > 0
}

func (s *codexGatewayAnthropicStreamState) shouldPersistToolLoopState() bool {
	return s.terminalSeen && strings.TrimSpace(s.finishReason) == "tool_use" && s.hasExposedToolCall()
}

func (s *codexGatewayAnthropicStreamState) shouldExposeToolCall(call *codexGatewayAnthropicStreamToolCall) bool {
	if call == nil || codexGatewayIsServerHandledHostedTool(call.Kind, call.Name) {
		return false
	}
	return call.ItemEmitted && call.CallID != "" && call.Name != "" && s.terminalSeen && strings.TrimSpace(s.finishReason) == "tool_use"
}

func (s *codexGatewayAnthropicStreamState) shouldExposeMessage() bool {
	if !s.messageAdded {
		return false
	}
	return !(s.terminalSeen && strings.TrimSpace(s.finishReason) == "tool_use" && s.hasExposedToolCall())
}

func (s *codexGatewayAnthropicStreamState) hasExposedToolCall() bool {
	for _, call := range s.toolCalls {
		if s.shouldExposeToolCall(call) {
			return true
		}
	}
	return false
}

func (s *codexGatewayAnthropicStreamState) hasClientVisibleOutputStarted() bool {
	if s.messageEmitted {
		return true
	}
	for _, call := range s.toolCalls {
		if call != nil && call.ItemEmitted {
			return true
		}
	}
	return false
}

func (s *codexGatewayAnthropicStreamState) serverHandledHostedToolCalls() []codexGatewayHostedToolCall {
	if !s.terminalSeen || strings.TrimSpace(s.finishReason) != "tool_use" {
		return nil
	}
	out := make([]codexGatewayHostedToolCall, 0, len(s.toolCalls))
	for _, call := range s.sortedToolCallsByOutputIndex() {
		if call == nil || !codexGatewayIsServerHandledHostedTool(call.Kind, call.Name) {
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

func (s *codexGatewayAnthropicStreamState) sortedOutputIndexes() []int {
	indexes := make([]int, 0, 1+len(s.toolCalls))
	if s.shouldExposeMessage() {
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

func (s *codexGatewayAnthropicStreamState) sortedToolCallsByOutputIndex() []*codexGatewayAnthropicStreamToolCall {
	calls := make([]*codexGatewayAnthropicStreamToolCall, 0, len(s.toolCalls))
	for _, index := range s.sortedToolOrder() {
		if call := s.toolCalls[index]; call != nil {
			calls = append(calls, call)
		}
	}
	sort.Slice(calls, func(i, j int) bool { return calls[i].OutputIndex < calls[j].OutputIndex })
	return calls
}

func (s *codexGatewayAnthropicStreamState) sortedToolOrder() []int {
	out := append([]int(nil), s.toolOrder...)
	sort.Ints(out)
	return out
}

func (s *codexGatewayAnthropicStreamState) sortedThinkingOrder() []int {
	out := append([]int(nil), s.thinkingOrder...)
	sort.Ints(out)
	return out
}

func (s *codexGatewayAnthropicStreamState) toolCallByOutputIndex(outputIndex int) *codexGatewayAnthropicStreamToolCall {
	for _, call := range s.sortedToolCallsByOutputIndex() {
		if call.OutputIndex == outputIndex {
			return call
		}
	}
	return nil
}
