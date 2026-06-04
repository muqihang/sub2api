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

const codexGatewayDeepSeekHostedToolMaxTurns = 64

const (
	codexGatewayHostedWebSearchCheckpointThreshold  = 12
	codexGatewayHostedWebSearchCheckpointKeepRecent = 4
	codexGatewayHostedWebSearchCheckpointMarker     = "Hosted web search checkpoint summary"
)

var codexGatewayExecuteHostedWebSearchFunc = codexGatewayExecuteHostedWebSearch

type codexGatewayHostedToolContext struct {
	WebSearchResults     map[string]string
	WebSearchCheckpoints []codexGatewayHostedWebSearchCheckpoint
	VisibleEventSink     func(codexGatewayHostedToolVisibleEvent) error
	RootResponseID       string
	NextOutputIndex      int
	OutputItems          map[int]json.RawMessage
	OutputIndexes        map[string]int
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

type codexGatewayHostedWebSearchCheckpoint struct {
	CallID  string
	Query   string
	Summary string
	Reused  bool
}

func (c *codexGatewayHostedToolContext) ensureSyntheticResponse(responseID string, nextOutputIndex int) {
	if c == nil {
		return
	}
	responseID = strings.TrimSpace(responseID)
	if c.RootResponseID == "" && responseID != "" {
		c.RootResponseID = responseID
	}
	if c.NextOutputIndex < nextOutputIndex {
		c.NextOutputIndex = nextOutputIndex
	}
	if c.OutputItems == nil {
		c.OutputItems = make(map[int]json.RawMessage)
	}
	if c.OutputIndexes == nil {
		c.OutputIndexes = make(map[string]int)
	}
}

func (c *codexGatewayHostedToolContext) hostedOutputIndex(callID, query string) int {
	if c == nil {
		return 0
	}
	if c.OutputIndexes == nil {
		c.OutputIndexes = make(map[string]int)
	}
	key := codexGatewayHostedWebSearchItemID(callID, query)
	if idx, ok := c.OutputIndexes[key]; ok {
		return idx
	}
	idx := c.NextOutputIndex
	c.NextOutputIndex++
	c.OutputIndexes[key] = idx
	return idx
}

func (c *codexGatewayHostedToolContext) rememberOutputItems(items map[int]json.RawMessage) {
	if c == nil || len(items) == 0 {
		return
	}
	if c.OutputItems == nil {
		c.OutputItems = make(map[int]json.RawMessage, len(items))
	}
	for index, raw := range items {
		c.OutputItems[index] = cloneCodexGatewayRawJSON(raw)
	}
}

func (c *codexGatewayHostedToolContext) rememberHostedWebSearchItem(outputIndex int, event codexGatewayHostedToolVisibleEvent) {
	if c == nil || strings.TrimSpace(event.Phase) != "completed" {
		return
	}
	item := codexGatewayHostedWebSearchItem(event, "completed")
	raw, err := json.Marshal(item)
	if err != nil {
		return
	}
	if c.OutputItems == nil {
		c.OutputItems = make(map[int]json.RawMessage)
	}
	c.OutputItems[outputIndex] = raw
}

func (c *codexGatewayHostedToolContext) rememberHostedWebSearchCheckpoint(event codexGatewayHostedToolVisibleEvent) {
	if c == nil || strings.TrimSpace(event.Phase) != "completed" {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(event.Name), "web_search") {
		return
	}
	entry := codexGatewayHostedWebSearchCheckpoint{
		CallID:  strings.TrimSpace(event.CallID),
		Query:   strings.TrimSpace(event.Query),
		Summary: codexGatewayHostedWebSearchCheckpointSummary(event.Query, event.Output, event.Reused),
		Reused:  event.Reused,
	}
	if entry.CallID == "" && entry.Query == "" {
		return
	}
	for i := range c.WebSearchCheckpoints {
		if entry.CallID != "" && c.WebSearchCheckpoints[i].CallID == entry.CallID {
			c.WebSearchCheckpoints[i] = entry
			return
		}
	}
	c.WebSearchCheckpoints = append(c.WebSearchCheckpoints, entry)
}

func cloneCodexGatewayIndexedRawMessages(in map[int]json.RawMessage) map[int]json.RawMessage {
	if len(in) == 0 {
		return nil
	}
	out := make(map[int]json.RawMessage, len(in))
	for index, raw := range in {
		out[index] = cloneCodexGatewayRawJSON(raw)
	}
	return out
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
	upstreamModel := strings.TrimSpace(model.UpstreamModel)
	if upstreamModel == "" {
		upstreamModel = strings.TrimSpace(model.Slug)
	}
	req, err := codexGatewayDeepSeekRequestWithHostedVision(ctx, req, stateStore, reqCtx, upstreamModel, cfg)
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
	body = codexGatewayDeepSeekFinalizeChatCompletionsBody(body)

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
		return CodexGatewayDeepSeekAdapterResult{}, codexGatewayDeepSeekStreamFailoverError(
			http.StatusBadGateway,
			nil,
			"upstream_disconnected",
			"DeepSeek upstream stream disconnected: "+sanitizeStreamError(err),
			true,
		)
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
		if codexGatewayDeepSeekShouldFailoverUpstreamResponse(resp.StatusCode) {
			return CodexGatewayDeepSeekAdapterResult{}, &UpstreamFailoverError{
				StatusCode:      resp.StatusCode,
				ResponseBody:    append([]byte(nil), result.ServiceResponse.Body...),
				ResponseHeaders: resp.Header.Clone(),
			}
		}
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
			continue
		}
		if len(dataLines) > 0 {
			dataLines = append(dataLines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		if !state.hasClientVisibleOutputStarted() {
			return CodexGatewayDeepSeekAdapterResult{}, codexGatewayDeepSeekStreamFailoverError(
				http.StatusBadGateway,
				nil,
				"upstream_disconnected",
				"DeepSeek upstream stream disconnected: "+sanitizeStreamError(err),
				true,
			)
		}
		if err := state.finishStreamTruncated(writer, deferredWriter); err != nil {
			return CodexGatewayDeepSeekAdapterResult{}, err
		}
		result.ProviderResult = state.providerResult(resp.Header.Get("x-request-id"))
		return result, nil
	}
	if err := flush(); err != nil {
		return CodexGatewayDeepSeekAdapterResult{}, err
	}

	if calls := state.serverHandledHostedToolCalls(); len(calls) > 0 {
		if err := state.writeReasoningBeforeClientAction(writer); err != nil {
			return CodexGatewayDeepSeekAdapterResult{}, err
		}
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
			if termErr := state.finishHostedToolError(writer, err); termErr != nil {
				return CodexGatewayDeepSeekAdapterResult{}, termErr
			}
			if flushErr := deferredWriter.Flush(); flushErr != nil {
				return CodexGatewayDeepSeekAdapterResult{}, flushErr
			}
			result.ProviderResult = state.providerResult(resp.Header.Get("x-request-id"))
			return result, nil
		}
		cfg.StreamSequenceNumber = writer.NextSequenceNumber()
		return executeCodexGatewayDeepSeekStreamWithHostedToolTurns(ctx, client, baseURL, apiKey, model, nextReq, stateStore, reqCtx, cfg, dst, turn+1)
	}

	finalEvent := ""
	var finishErr error
	if state.terminalSeen {
		finalEvent, finishErr = state.finish(writer)
	} else if !state.hasClientVisibleOutputStarted() {
		return CodexGatewayDeepSeekAdapterResult{}, codexGatewayDeepSeekStreamFailoverError(
			http.StatusBadGateway,
			nil,
			"upstream_missing_terminal",
			"DeepSeek stream ended before completion (missing terminal event).",
			false,
		)
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

	if state.shouldPersistResponseState(prepared.ReplayMessages) {
		storedToolCalls := state.storedToolCalls()
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
			storedToolCalls,
			prepared.ToolNameMap,
			prepared.ToolSchemas,
			codexGatewayDeepSeekStateReplayMessages(prepared.ReplayMessages, state.messageText.String(), state.messageAdded, state.reasoningText.String(), state.reasoningPresent, !state.reasoningPresent, storedToolCalls, prepared.ToolNameMap),
		); err != nil {
			return CodexGatewayDeepSeekAdapterResult{}, err
		}
	}

	result.ProviderResult = state.providerResult(resp.Header.Get("x-request-id"))
	return result, nil
}

type codexGatewayDeepSeekStreamState struct {
	model             CodexGatewayModel
	toolNameMap       map[string]CodexGatewayToolNameMapEntry
	responseID        string
	upstreamModel     string
	reasoningText     strings.Builder
	reasoningPresent  bool
	reasoningAdded    bool
	reasoningEmitted  bool
	reasoningID       string
	reasoningIndex    int
	messageText       strings.Builder
	messageAdded      bool
	messageStarted    bool
	messageEmitted    bool
	messagePhase      string
	messageID         string
	messageIndex      int
	nextOutputIndex   int
	toolCalls         map[int]*codexGatewayDeepSeekStreamToolCall
	toolOrder         []int
	usage             CodexGatewayProviderUsage
	usageRaw          json.RawMessage
	finishReason      string
	terminalSeen      bool
	createdSent       bool
	prefixOutputItems map[int]json.RawMessage
	blockedReason     string
	hostedToolError   error
}

type codexGatewayDeepSeekStreamToolCall struct {
	Index         int
	OutputIndex   int
	CallID        string
	Alias         string
	Name          string
	Namespace     string
	Kind          string
	FlattenedArgs []CodexGatewayToolArgumentPath
	Buffer        strings.Builder
	Added         bool
	ItemEmitted   bool
	EmittedLen    int
	Blocked       bool
	BlockReason   string
}

type codexGatewayDeepSeekPreparedToolCall struct {
	Call      *codexGatewayDeepSeekStreamToolCall
	Arguments string
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
		Alias:         c.Alias,
		Kind:          c.Kind,
		Namespace:     c.Namespace,
		Name:          c.Name,
		FlattenedArgs: append([]CodexGatewayToolArgumentPath(nil), c.FlattenedArgs...),
	}
}

func newCodexGatewayDeepSeekStreamState(model CodexGatewayModel, toolNameMap map[string]CodexGatewayToolNameMapEntry) *codexGatewayDeepSeekStreamState {
	return &codexGatewayDeepSeekStreamState{
		model:       model,
		toolNameMap: cloneCodexGatewayToolNameMap(toolNameMap),
		toolCalls:   make(map[int]*codexGatewayDeepSeekStreamToolCall),
	}
}

func (s *codexGatewayDeepSeekStreamState) applyHostedToolContext(ctx *codexGatewayHostedToolContext) {
	if s == nil || ctx == nil || strings.TrimSpace(ctx.RootResponseID) == "" {
		return
	}
	s.responseID = strings.TrimSpace(ctx.RootResponseID)
	s.messageID = codexGatewayDeepSeekMessageID(s.responseID, ctx.NextOutputIndex)
	s.nextOutputIndex = ctx.NextOutputIndex
	s.createdSent = true
	s.prefixOutputItems = cloneCodexGatewayIndexedRawMessages(ctx.OutputItems)
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
		s.reasoningID = codexGatewayDeepSeekReasoningID(s.responseID)
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
		if err := writer.WriteResponseInProgress(created); err != nil {
			return err
		}
		s.createdSent = true
	}

	if len(chunk.Choices) > 0 {
		choice := chunk.Choices[0]
		if choice.Delta.ReasoningContent != nil {
			s.reasoningPresent = true
			s.reasoningText.WriteString(*choice.Delta.ReasoningContent)
			if strings.TrimSpace(*choice.Delta.ReasoningContent) != "" {
				if err := s.writeReasoningAdded(writer); err != nil {
					return err
				}
			}
		}
		if choice.Delta.Content != nil && *choice.Delta.Content != "" {
			if err := s.writeMessageTextDelta(writer, *choice.Delta.Content); err != nil {
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

func (s *codexGatewayDeepSeekStreamState) writeReasoningAdded(writer *CodexGatewayResponseEventWriter) error {
	if s.reasoningAdded {
		return nil
	}
	if s.reasoningID == "" {
		s.reasoningID = codexGatewayDeepSeekReasoningID(s.responseID)
	}
	s.reasoningAdded = true
	s.reasoningIndex = s.nextOutputIndex
	s.nextOutputIndex++
	item, err := json.Marshal(codexGatewayDeepSeekReasoningItem(s.reasoningID, "in_progress"))
	if err != nil {
		return err
	}
	return writer.WriteOutputItemAdded(s.responseID, s.reasoningIndex, item)
}

func (s *codexGatewayDeepSeekStreamState) writeReasoningBeforeClientAction(writer *CodexGatewayResponseEventWriter) error {
	if s.reasoningAdded {
		return nil
	}
	if s.reasoningID == "" {
		s.reasoningID = codexGatewayDeepSeekReasoningID(s.responseID)
	}
	// If DeepSeek sent assistant preamble text before a tool call, no message
	// events have been emitted yet. Shift that pending message after a synthetic
	// reasoning shell so Desktop groups the action trace like GPT native turns.
	if s.messageAdded && !s.messageStarted && !s.messageEmitted {
		s.reasoningIndex = s.messageIndex
		s.messageIndex++
		s.messageID = codexGatewayDeepSeekMessageID(s.responseID, s.messageIndex)
		if s.nextOutputIndex <= s.messageIndex {
			s.nextOutputIndex = s.messageIndex + 1
		}
	} else {
		s.reasoningIndex = s.nextOutputIndex
		s.nextOutputIndex++
	}
	s.reasoningAdded = true
	item, err := json.Marshal(codexGatewayDeepSeekReasoningItem(s.reasoningID, "in_progress"))
	if err != nil {
		return err
	}
	return writer.WriteOutputItemAdded(s.responseID, s.reasoningIndex, item)
}

func (s *codexGatewayDeepSeekStreamState) ensureMessageStarted(writer *CodexGatewayResponseEventWriter) error {
	if !s.messageAdded {
		s.messageAdded = true
		s.messageIndex = s.nextOutputIndex
		s.messageID = codexGatewayDeepSeekMessageID(s.responseID, s.messageIndex)
		s.nextOutputIndex++
	}
	if s.messageStarted {
		return nil
	}
	phase := s.currentMessagePhase()
	if strings.TrimSpace(s.messagePhase) == "" {
		s.messagePhase = phase
	}
	item := map[string]any{
		"type":    "message",
		"id":      s.messageID,
		"role":    "assistant",
		"status":  "in_progress",
		"phase":   phase,
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
	s.messageStarted = true
	return nil
}

func (s *codexGatewayDeepSeekStreamState) writeMessageTextDelta(writer *CodexGatewayResponseEventWriter, delta string) error {
	if delta == "" {
		return nil
	}
	if err := s.ensureMessageStarted(writer); err != nil {
		return err
	}
	s.messageText.WriteString(delta)
	return writer.WriteOutputTextDelta(s.responseID, s.messageID, s.messageIndex, 0, delta)
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
		if !ok {
			if compatAlias := resolveCodexGatewayToolAliasCompat(
				CodexGatewayToolMappingResult{NameMap: s.toolNameMap},
				call.Alias,
			); compatAlias != "" {
				call.Alias = compatAlias
				entry, ok = s.toolNameMap[call.Alias]
			}
		}
		if ok {
			call.Name = codexGatewayClientVisibleToolName(entry)
			call.Namespace = entry.Namespace
			call.Kind = entry.Kind
			call.FlattenedArgs = append([]CodexGatewayToolArgumentPath(nil), entry.FlattenedArgs...)
		} else {
			call.Name = call.Alias
			call.Namespace = ""
			call.Kind = CodexGatewayToolKindFunction
			call.FlattenedArgs = nil
		}
	}
	if !call.Added && call.CallID != "" && call.Name != "" {
		call.Added = true
	}
	if delta.Function.Arguments != "" {
		call.Buffer.WriteString(delta.Function.Arguments)
		call.Blocked = false
		call.BlockReason = ""
	}
	itemType := codexGatewayDeepSeekClientVisibleToolItemType(call.toolNameMapEntry())
	if call.Added && !call.ItemEmitted && !codexGatewayIsServerHandledHostedTool(call.Kind, call.Name) {
		if codexGatewayDeepSeekIsMutatingTool(call.toolNameMapEntry()) {
			if _, ok, _ := codexGatewayPrepareDeepSeekToolArguments(call.toolNameMapEntry(), call.Buffer.String()); !ok {
				return nil
			}
		}
		if itemType == CodexGatewayOutputItemTypeToolSearchCall {
			if strings.TrimSpace(call.Buffer.String()) == "" {
				return nil
			}
			if !codexGatewayDeepSeekStreamHasCompleteToolSearchArguments(call.Buffer.String()) {
				return nil
			}
			if _, ok, _ := codexGatewayPrepareDeepSeekToolArguments(call.toolNameMapEntry(), call.Buffer.String()); !ok {
				return nil
			}
		}
		if itemType == CodexGatewayOutputItemTypeLocalShellCall && strings.TrimSpace(codexGatewayExtractShellExecCmd(call.Buffer.String())) == "" {
			return nil
		}
		if err := s.writeReasoningBeforeClientAction(writer); err != nil {
			return err
		}
		if call.OutputIndex < 0 {
			call.OutputIndex = s.nextOutputIndex
			s.nextOutputIndex++
		}
		if s.messageAdded && !s.messageStarted {
			s.messagePhase = "commentary"
		}
		if s.messageAdded && !s.messageEmitted {
			if err := s.writeMessageEvents(writer); err != nil {
				return err
			}
		}
		item := map[string]any{
			"id":      codexGatewayDeepSeekToolItemID(call.CallID),
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
		case CodexGatewayOutputItemTypeToolSearchCall:
			arguments, _, _ := codexGatewayPrepareDeepSeekToolArguments(call.toolNameMapEntry(), call.Buffer.String())
			item = codexGatewayDeepSeekToolSearchCallItem(call.CallID, "in_progress", arguments)
		default:
			item["type"] = itemType
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
		if codexGatewayDeepSeekShouldDelayFunctionArgumentDeltas(call) {
			return nil
		}
		if itemType == CodexGatewayOutputItemTypeLocalShellCall || itemType == CodexGatewayOutputItemTypeToolSearchCall {
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

func codexGatewayDeepSeekStreamHasCompleteToolSearchArguments(raw string) bool {
	normalized := normalizeCodexGatewayToolArguments(raw)
	return json.Valid([]byte(normalized))
}

func (s *codexGatewayDeepSeekStreamState) finish(writer *CodexGatewayResponseEventWriter) (string, error) {
	status, incompleteReason := codexGatewayDeepSeekFinishReasonStatus(s.finishReason)
	if blockedReason := s.blockedToolCallReason(); blockedReason != "" {
		status = "incomplete"
		incompleteReason = blockedReason
	}
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

func (s *codexGatewayDeepSeekStreamState) finishHostedToolError(writer *CodexGatewayResponseEventWriter, err error) error {
	s.hostedToolError = err
	response := s.finalResponse("failed", "")
	response.Error = &CodexGatewayResponseError{
		Code:    "hosted_tool_error",
		Message: "DeepSeek hosted web_search failed: " + sanitizeStreamError(err),
		RawFields: map[string]json.RawMessage{
			"type": json.RawMessage(`"api_error"`),
		},
	}
	return writer.WriteResponseFailed(response)
}

func (s *codexGatewayDeepSeekStreamState) finishStreamTruncated(writer *CodexGatewayResponseEventWriter, deferredWriter *codexGatewayDeferredStreamWriter) error {
	if _, err := s.finishEarly(writer); err != nil {
		return err
	}
	if deferredWriter != nil {
		return deferredWriter.Flush()
	}
	return nil
}

func (s *codexGatewayDeepSeekStreamState) writeDoneEvents(writer *CodexGatewayResponseEventWriter) error {
	preparedCalls, _, _ := s.preparedToolCallsByOutputIndex()
	for _, index := range s.sortedOutputIndexes() {
		switch {
		case s.reasoningAdded && index == s.reasoningIndex:
			if s.reasoningEmitted {
				continue
			}
			item, err := json.Marshal(codexGatewayDeepSeekReasoningItem(s.reasoningID, "completed"))
			if err != nil {
				return err
			}
			if err := writer.WriteOutputItemDone(s.responseID, s.reasoningIndex, item); err != nil {
				return err
			}
			s.reasoningEmitted = true
		case s.shouldExposeMessage() && index == s.messageIndex:
			if err := s.writeMessageEvents(writer); err != nil {
				return err
			}
		default:
			prepared, ok := preparedCalls[index]
			if !ok {
				continue
			}
			call := prepared.Call
			doneItem := map[string]any{
				"id":      codexGatewayDeepSeekToolItemID(call.CallID),
				"call_id": call.CallID,
				"name":    call.Name,
				"status":  "completed",
			}
			itemType := codexGatewayDeepSeekClientVisibleToolItemType(call.toolNameMapEntry())
			switch itemType {
			case CodexGatewayOutputItemTypeCustomToolCall:
				doneItem["type"] = CodexGatewayOutputItemTypeCustomToolCall
				doneItem["input"] = codexGatewayDeepSeekCustomToolInput(prepared.Arguments, call.toolNameMapEntry())
			case CodexGatewayOutputItemTypeLocalShellCall:
				codexGatewayApplyLocalShellCallItemFields(doneItem, call.CallID, "completed", prepared.Arguments)
			case CodexGatewayOutputItemTypeToolSearchCall:
				doneItem = codexGatewayDeepSeekToolSearchCallItem(call.CallID, "completed", prepared.Arguments)
			default:
				doneItem["type"] = itemType
				if namespace := strings.TrimSpace(call.Namespace); namespace != "" {
					doneItem["namespace"] = namespace
				}
				doneItem["arguments"] = prepared.Arguments
			}
			if rawItem, err := json.Marshal(doneItem); err == nil {
				if call.Kind == CodexGatewayToolKindCustom {
					if err := writer.WriteCustomToolCallInputDone(s.responseID, codexGatewayDeepSeekToolItemID(call.CallID), call.OutputIndex, firstCodexGatewayToolString(doneItem["input"])); err != nil {
						return err
					}
				} else if itemType != CodexGatewayOutputItemTypeLocalShellCall && itemType != CodexGatewayOutputItemTypeToolSearchCall {
					if codexGatewayDeepSeekShouldDelayFunctionArgumentDeltas(call) {
						args := prepared.Arguments
						if len(args) > call.EmittedLen {
							deltaText := args[call.EmittedLen:]
							call.EmittedLen = len(args)
							if err := writer.WriteFunctionCallArgumentsDelta(s.responseID, codexGatewayDeepSeekToolItemID(call.CallID), call.OutputIndex, deltaText); err != nil {
								return err
							}
						}
					}
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

func (s *codexGatewayDeepSeekStreamState) writeMessageEvents(writer *CodexGatewayResponseEventWriter) error {
	if s.messageEmitted {
		return nil
	}
	phase := s.currentMessagePhase()
	alreadyStarted := s.messageStarted
	if err := s.ensureMessageStarted(writer); err != nil {
		return err
	}
	text := s.messageText.String()
	if text != "" && !alreadyStarted {
		if err := writer.WriteOutputTextDelta(s.responseID, s.messageID, s.messageIndex, 0, text); err != nil {
			return err
		}
	}
	if err := writer.WriteOutputTextDone(s.responseID, s.messageID, s.messageIndex, 0, text); err != nil {
		return err
	}
	part, _ := json.Marshal(map[string]any{"type": "output_text", "text": text})
	if err := writer.WriteContentPartDone(s.responseID, s.messageID, s.messageIndex, 0, part); err != nil {
		return err
	}
	doneItem, err := json.Marshal(map[string]any{
		"type":   "message",
		"id":     s.messageID,
		"role":   "assistant",
		"status": "completed",
		"phase":  phase,
		"content": []map[string]any{{
			"type": "output_text",
			"text": text,
		}},
	})
	if err != nil {
		return err
	}
	if err := writer.WriteOutputItemDone(s.responseID, s.messageIndex, doneItem); err != nil {
		return err
	}
	s.messageEmitted = true
	return nil
}

func (s *codexGatewayDeepSeekStreamState) finalResponse(status, incompleteReason string) CodexGatewayResponse {
	if blockedReason := s.blockedToolCallReason(); blockedReason != "" {
		status = "incomplete"
		incompleteReason = blockedReason
	}
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
	byIndex := s.outputItemsByIndex()
	indexes := make([]int, 0, len(byIndex))
	for index := range byIndex {
		indexes = append(indexes, index)
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

func (s *codexGatewayDeepSeekStreamState) outputItemsByIndex() map[int]json.RawMessage {
	byIndex := cloneCodexGatewayIndexedRawMessages(s.prefixOutputItems)
	if byIndex == nil {
		byIndex = make(map[int]json.RawMessage, 2+len(s.toolCalls))
	}
	preparedCalls, _, _ := s.preparedToolCallsByOutputIndex()
	if s.reasoningAdded {
		item, _ := json.Marshal(codexGatewayDeepSeekReasoningItem(s.reasoningID, "completed"))
		byIndex[s.reasoningIndex] = item
	}
	if s.shouldExposeMessage() {
		item, _ := json.Marshal(map[string]any{
			"type":   "message",
			"id":     s.messageID,
			"role":   "assistant",
			"status": "completed",
			"phase":  s.currentMessagePhase(),
			"content": []map[string]any{{
				"type": "output_text",
				"text": s.messageText.String(),
			}},
		})
		byIndex[s.messageIndex] = item
	}
	for outputIndex, prepared := range preparedCalls {
		call := prepared.Call
		item := map[string]any{
			"id":      codexGatewayDeepSeekToolItemID(call.CallID),
			"call_id": call.CallID,
			"name":    call.Name,
			"status":  "completed",
		}
		itemType := codexGatewayDeepSeekClientVisibleToolItemType(call.toolNameMapEntry())
		switch itemType {
		case CodexGatewayOutputItemTypeCustomToolCall:
			item["type"] = CodexGatewayOutputItemTypeCustomToolCall
			item["input"] = codexGatewayDeepSeekCustomToolInput(prepared.Arguments, call.toolNameMapEntry())
		case CodexGatewayOutputItemTypeLocalShellCall:
			codexGatewayApplyLocalShellCallItemFields(item, call.CallID, "completed", prepared.Arguments)
		case CodexGatewayOutputItemTypeToolSearchCall:
			item = codexGatewayDeepSeekToolSearchCallItem(call.CallID, "completed", prepared.Arguments)
		default:
			item["type"] = itemType
			if namespace := strings.TrimSpace(call.Namespace); namespace != "" {
				item["namespace"] = namespace
			}
			item["arguments"] = prepared.Arguments
		}
		rawItem, _ := json.Marshal(item)
		byIndex[outputIndex] = rawItem
	}
	return byIndex
}

func (s *codexGatewayDeepSeekStreamState) storedToolCalls() []CodexGatewayStoredToolCall {
	preparedCalls, order, _ := s.preparedToolCallsByOutputIndex()
	out := make([]CodexGatewayStoredToolCall, 0, len(preparedCalls))
	for _, outputIndex := range order {
		prepared, ok := preparedCalls[outputIndex]
		if !ok {
			continue
		}
		call := prepared.Call
		out = append(out, CodexGatewayStoredToolCall{
			ID:        call.CallID,
			Type:      call.Kind,
			Alias:     call.Alias,
			Name:      call.Name,
			Arguments: codexGatewayDeepSeekStoredToolArguments(call, prepared.Arguments),
		})
	}
	return out
}

func codexGatewayDeepSeekShouldDelayFunctionArgumentDeltas(call *codexGatewayDeepSeekStreamToolCall) bool {
	if call == nil || call.Kind == CodexGatewayToolKindCustom {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(call.Name), "wait_agent")
}

func codexGatewayDeepSeekFunctionToolArguments(call *codexGatewayDeepSeekStreamToolCall) string {
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

func codexGatewayDeepSeekStoredToolArguments(call *codexGatewayDeepSeekStreamToolCall, prepared string) string {
	if call == nil {
		return ""
	}
	if call.Kind == CodexGatewayToolKindCustom || codexGatewayClientVisibleToolItemType(call.toolNameMapEntry()) == CodexGatewayOutputItemTypeLocalShellCall {
		return prepared
	}
	return prepared
}

func (s *codexGatewayDeepSeekStreamState) providerResult(upstreamRequestID string) CodexGatewayProviderResult {
	status := codexGatewayDeepSeekFinishReasonStatusValue(s.finishReason, s.terminalSeen, s.hasPartialState())
	incompleteReason := codexGatewayDeepSeekFinishReasonIncompleteReason(s.finishReason, s.terminalSeen, s.hasPartialState())
	var responseError *CodexGatewayResponseError
	if s.hostedToolError != nil {
		status = "failed"
		incompleteReason = ""
		responseError = &CodexGatewayResponseError{
			Code:    "hosted_tool_error",
			Message: "DeepSeek hosted web_search failed: " + sanitizeStreamError(s.hostedToolError),
			RawFields: map[string]json.RawMessage{
				"type": json.RawMessage(`"api_error"`),
			},
		}
	}
	if blockedReason := s.blockedToolCallReason(); blockedReason != "" {
		status = "incomplete"
		incompleteReason = blockedReason
	}
	response := s.finalResponse(status, incompleteReason)
	if responseError != nil {
		response.Error = responseError
	}
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

func (s *codexGatewayDeepSeekStreamState) shouldPersistResponseState(replayMessages []json.RawMessage) bool {
	if !s.terminalSeen {
		return false
	}
	return codexGatewayDeepSeekShouldPersistResponseState(s.messageText.String(), s.messageAdded, s.reasoningText.String(), s.reasoningPresent, !s.reasoningPresent, s.storedToolCalls(), replayMessages)
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

func (s *codexGatewayDeepSeekStreamState) shouldExposeMessage() bool {
	if !s.messageAdded {
		return false
	}
	return true
}

func (s *codexGatewayDeepSeekStreamState) currentMessagePhase() string {
	if phase := strings.TrimSpace(s.messagePhase); phase != "" {
		return phase
	}
	if s.terminalSeen && strings.TrimSpace(s.finishReason) == "tool_calls" && s.hasExposedToolCall() {
		return "commentary"
	}
	return "final_answer"
}

func (s *codexGatewayDeepSeekStreamState) hasExposedToolCall() bool {
	preparedCalls, _, _ := s.preparedToolCallsByOutputIndex()
	return len(preparedCalls) > 0
}

func (s *codexGatewayDeepSeekStreamState) hasClientVisibleOutputStarted() bool {
	if s.messageStarted || s.messageEmitted {
		return true
	}
	for _, call := range s.toolCalls {
		if call != nil && call.ItemEmitted {
			return true
		}
	}
	return s.reasoningAdded
}

func (s *codexGatewayDeepSeekStreamState) blockedToolCallReason() string {
	if strings.TrimSpace(s.blockedReason) != "" {
		return strings.TrimSpace(s.blockedReason)
	}
	for _, call := range s.sortedToolCallsByOutputIndex() {
		if call != nil && call.Blocked && strings.TrimSpace(call.BlockReason) != "" {
			return strings.TrimSpace(call.BlockReason)
		}
	}
	_, _, reason := s.preparedToolCallsByOutputIndex()
	return reason
}

func (s *codexGatewayDeepSeekStreamState) preparedToolCallsByOutputIndex() (map[int]codexGatewayDeepSeekPreparedToolCall, []int, string) {
	prepared := make(map[int]codexGatewayDeepSeekPreparedToolCall, len(s.toolCalls))
	order := make([]int, 0, len(s.toolCalls))
	blockedReason := ""
	for _, call := range s.sortedToolCallsByOutputIndex() {
		if call == nil {
			continue
		}
		call.Blocked = false
		call.BlockReason = ""
		if !s.shouldExposeToolCall(call) {
			continue
		}
		arguments, ok, reason := codexGatewayPrepareDeepSeekToolArguments(call.toolNameMapEntry(), call.Buffer.String())
		if !ok {
			call.Blocked = true
			call.BlockReason = reason
			if s.blockedReason == "" {
				s.blockedReason = reason
			}
			if blockedReason == "" {
				blockedReason = reason
			}
			continue
		}
		if call.OutputIndex < 0 {
			continue
		}
		prepared[call.OutputIndex] = codexGatewayDeepSeekPreparedToolCall{
			Call:      call,
			Arguments: arguments,
		}
		order = append(order, call.OutputIndex)
	}
	sort.Ints(order)
	return prepared, order, blockedReason
}

func codexGatewayDeepSeekShouldFailoverUpstreamResponse(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

func codexGatewayDeepSeekStreamFailoverError(statusCode int, headers http.Header, code, message string, retryableOnSameAccount bool) error {
	body, err := MarshalCodexGatewayErrorJSON(CodexGatewayErrorTypeAPI, strings.TrimSpace(code), strings.TrimSpace(message))
	if err != nil {
		body = []byte(`{"error":{"type":"api_error","code":"upstream_error","message":"DeepSeek stream failed."}}`)
	}
	return &UpstreamFailoverError{
		StatusCode:             statusCode,
		ResponseBody:           body,
		ResponseHeaders:        cloneCodexGatewayHTTPHeader(headers),
		RetryableOnSameAccount: retryableOnSameAccount,
	}
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
		completedEvent := codexGatewayHostedToolVisibleEvent{
			Phase:     "completed",
			CallID:    call.CallID,
			Name:      call.Name,
			Arguments: call.Arguments,
			Query:     query,
			Output:    output,
			Reused:    reused,
		}
		toolCtx.rememberHostedWebSearchCheckpoint(completedEvent)
		if toolCtx.VisibleEventSink != nil {
			if err := toolCtx.VisibleEventSink(completedEvent); err != nil {
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
	items = codexGatewayCompactHostedWebSearchInputItems(items, toolCtx)
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

func codexGatewayCompactHostedWebSearchInputItems(items []any, toolCtx *codexGatewayHostedToolContext) []any {
	if len(items) == 0 || toolCtx == nil {
		return items
	}
	checkpoints := toolCtx.WebSearchCheckpoints
	if len(checkpoints) <= codexGatewayHostedWebSearchCheckpointThreshold {
		return items
	}
	if codexGatewayHostedWebSearchCheckpointKeepRecent <= 0 || len(checkpoints) <= codexGatewayHostedWebSearchCheckpointKeepRecent {
		return items
	}
	compacted := checkpoints[:len(checkpoints)-codexGatewayHostedWebSearchCheckpointKeepRecent]
	if len(compacted) == 0 {
		return items
	}
	compactedCallIDs := make(map[string]struct{}, len(compacted))
	for _, checkpoint := range compacted {
		if callID := strings.TrimSpace(checkpoint.CallID); callID != "" {
			compactedCallIDs[callID] = struct{}{}
		}
	}
	if len(compactedCallIDs) == 0 {
		return items
	}
	summaryItem := map[string]any{
		"type": "message",
		"role": "assistant",
		"content": []map[string]any{{
			"type": "input_text",
			"text": codexGatewayHostedWebSearchCheckpointMessage(compacted, codexGatewayHostedWebSearchCheckpointKeepRecent),
		}},
	}
	filtered := make([]any, 0, len(items))
	inserted := false
	for _, item := range items {
		if codexGatewayIsHostedWebSearchCheckpointMessageItem(item) {
			continue
		}
		if codexGatewayHostedWebSearchShouldCompactItem(item, compactedCallIDs) {
			if !inserted {
				filtered = append(filtered, summaryItem)
				inserted = true
			}
			continue
		}
		filtered = append(filtered, item)
	}
	if !inserted {
		filtered = append(filtered, summaryItem)
	}
	return filtered
}

func codexGatewayHostedWebSearchShouldCompactItem(item any, compactedCallIDs map[string]struct{}) bool {
	m, ok := item.(map[string]any)
	if !ok || len(compactedCallIDs) == 0 {
		return false
	}
	callID := strings.TrimSpace(firstCodexGatewayToolString(m["call_id"], m["tool_call_id"], m["id"]))
	if callID == "" {
		return false
	}
	if _, ok := compactedCallIDs[callID]; !ok {
		return false
	}
	switch strings.TrimSpace(firstCodexGatewayToolString(m["type"])) {
	case "function_call", "function_call_output":
		return true
	default:
		return false
	}
}

func codexGatewayIsHostedWebSearchCheckpointMessageItem(item any) bool {
	m, ok := item.(map[string]any)
	if !ok {
		return false
	}
	itemType := strings.TrimSpace(firstCodexGatewayToolString(m["type"]))
	if itemType != "" && itemType != "message" {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(firstCodexGatewayToolString(m["role"])), "assistant") {
		return false
	}
	return strings.HasPrefix(codexGatewayHostedWebSearchMessageText(m["content"]), codexGatewayHostedWebSearchCheckpointMarker)
}

func codexGatewayHostedWebSearchMessageText(content any) string {
	switch typed := content.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, rawPart := range typed {
			part, ok := rawPart.(map[string]any)
			if !ok {
				continue
			}
			if text := strings.TrimSpace(firstCodexGatewayToolString(part["text"])); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	default:
		return ""
	}
}

func codexGatewayHostedWebSearchCheckpointMessage(checkpoints []codexGatewayHostedWebSearchCheckpoint, recentCount int) string {
	var b strings.Builder
	b.WriteString(codexGatewayHostedWebSearchCheckpointMarker)
	fmt.Fprintf(&b, "\nCompacted %d completed hosted web_search turns. Queries and short summaries:", len(checkpoints))
	for i, checkpoint := range checkpoints {
		query := codexGatewayHostedWebSearchCheckpointClamp(checkpoint.Query, 80)
		summary := codexGatewayHostedWebSearchCheckpointClamp(checkpoint.Summary, 140)
		if summary == "" {
			summary = "summary unavailable"
		}
		if checkpoint.Reused {
			fmt.Fprintf(&b, "\n- turn %d | query=%q | reused cached result | %s", i+1, query, summary)
			continue
		}
		fmt.Fprintf(&b, "\n- turn %d | query=%q | %s", i+1, query, summary)
	}
	if recentCount > 0 {
		fmt.Fprintf(&b, "\nOnly the most recent %d hosted web_search raw results remain in full below for continuation.", recentCount)
	}
	return strings.TrimSpace(b.String())
}

func codexGatewayHostedWebSearchCheckpointSummary(query, output string, reused bool) string {
	if reused {
		if summary := strings.TrimSpace(gjson.Get(output, "summary").String()); summary != "" {
			return "reused cached result; prior summary: " + summary
		}
		if note := strings.TrimSpace(gjson.Get(output, "note").String()); note != "" {
			return note
		}
		if previous := strings.TrimSpace(gjson.Get(output, "previous_result").String()); previous != "" {
			if summary := codexGatewayHostedWebSearchCheckpointSummary(query, previous, false); summary != "" {
				return "reused cached result; prior summary: " + summary
			}
		}
		return "reused cached result"
	}
	if summary := strings.TrimSpace(gjson.Get(output, "summary").String()); summary != "" {
		return summary
	}
	if note := strings.TrimSpace(gjson.Get(output, "note").String()); note != "" {
		return note
	}
	for _, path := range []string{"results.0.snippet", "results.0.title", "query"} {
		if value := strings.TrimSpace(gjson.Get(output, path).String()); value != "" {
			return value
		}
	}
	return codexGatewayHostedWebSearchCheckpointClamp(output, 160)
}

func codexGatewayHostedWebSearchCheckpointClamp(text string, max int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if max <= 0 || len(text) <= max {
		return text
	}
	if max <= 3 {
		return text[:max]
	}
	return text[:max-3] + "..."
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
	item := codexGatewayHostedWebSearchItem(event, status)
	itemID := firstCodexGatewayToolString(item["id"])
	rawItem, err := json.Marshal(item)
	if err != nil {
		return err
	}
	switch phase {
	case "started":
		if err := writer.WriteOutputItemAdded(responseID, outputIndex, rawItem); err != nil {
			return err
		}
		if err := writer.WriteWebSearchCallEvent("in_progress", responseID, itemID, outputIndex); err != nil {
			return err
		}
		return writer.WriteWebSearchCallEvent("searching", responseID, itemID, outputIndex)
	case "completed":
		if err := writer.WriteWebSearchCallEvent("completed", responseID, itemID, outputIndex); err != nil {
			return err
		}
		return writer.WriteOutputItemDone(responseID, outputIndex, rawItem)
	}
	if err := writer.WriteOutputItemAdded(responseID, outputIndex, rawItem); err != nil {
		return err
	}
	return writer.WriteOutputItemDone(responseID, outputIndex, rawItem)
}

func codexGatewayHostedWebSearchItem(event codexGatewayHostedToolVisibleEvent, status string) map[string]any {
	return map[string]any{
		"id":      codexGatewayHostedWebSearchItemID(event.CallID, event.Query),
		"type":    "web_search_call",
		"status":  status,
		"call_id": event.CallID,
		"action": map[string]any{
			"type":  "search",
			"query": event.Query,
		},
	}
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
	if s.reasoningAdded {
		indexes = append(indexes, s.reasoningIndex)
	}
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

func codexGatewayDeepSeekReasoningID(responseID string) string {
	responseID = strings.TrimSpace(responseID)
	if responseID == "" {
		responseID = "chatcmpl_stream"
	}
	return "rs_" + responseID
}

func codexGatewayDeepSeekReasoningItem(itemID, status string) map[string]any {
	item := map[string]any{
		"type":    "reasoning",
		"id":      strings.TrimSpace(itemID),
		"summary": []any{},
		"content": nil,
	}
	if strings.TrimSpace(status) != "" {
		item["status"] = strings.TrimSpace(status)
	}
	return item
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
