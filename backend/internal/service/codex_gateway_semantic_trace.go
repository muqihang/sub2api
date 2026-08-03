package service

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/tidwall/gjson"
)

var codexGatewaySemanticTerminalEvents = map[string]string{
	"response.completed":  "completed",
	"response.failed":     "failed",
	"response.incomplete": "incomplete",
}

type CodexGatewaySemanticUsageShape struct {
	Present              bool `json:"present"`
	HasInputTokens       bool `json:"has_input_tokens"`
	HasOutputTokens      bool `json:"has_output_tokens"`
	HasTotalTokens       bool `json:"has_total_tokens"`
	HasCachedInputTokens bool `json:"has_cached_input_tokens"`
}

type CodexGatewaySemanticTraceEvent struct {
	EventType      string                         `json:"event_type"`
	OutputIndex    *int                           `json:"output_index,omitempty"`
	ItemType       string                         `json:"item_type,omitempty"`
	ItemName       string                         `json:"item_name,omitempty"`
	Namespace      string                         `json:"namespace,omitempty"`
	ItemStatus     string                         `json:"item_status,omitempty"`
	Phase          string                         `json:"phase,omitempty"`
	HasCallID      bool                           `json:"has_call_id"`
	HasItemID      bool                           `json:"has_item_id"`
	TerminalStatus string                         `json:"terminal_status,omitempty"`
	ErrorCode      string                         `json:"error_code,omitempty"`
	Usage          CodexGatewaySemanticUsageShape `json:"usage_shape"`
	raw            json.RawMessage
}

type CodexGatewaySemanticTrace struct {
	RawStream       string                           `json:"-"`
	Events          []CodexGatewaySemanticTraceEvent `json:"events"`
	TerminalEvent   string                           `json:"terminal_event,omitempty"`
	TerminalStatus  string                           `json:"terminal_status,omitempty"`
	Usage           CodexGatewaySemanticUsageShape   `json:"usage_shape"`
	SequenceNumbers []int64                          `json:"sequence_numbers,omitempty"`
}

type CodexGatewaySemanticInvariantResult struct {
	Name    string `json:"name"`
	Pass    bool   `json:"pass"`
	Details string `json:"details,omitempty"`
}

type CodexGatewaySemanticTraceSummary struct {
	EventTypes      []string                       `json:"event_types"`
	EventCount      int                            `json:"event_count"`
	OutputItemTypes []string                       `json:"output_item_types,omitempty"`
	OutputItemNames []string                       `json:"output_item_names,omitempty"`
	MessagePhases   []string                       `json:"message_phases,omitempty"`
	TerminalEvent   string                         `json:"terminal_event,omitempty"`
	TerminalStatus  string                         `json:"terminal_status,omitempty"`
	ErrorCode       string                         `json:"error_code,omitempty"`
	Usage           CodexGatewaySemanticUsageShape `json:"usage_shape"`
}

type CodexGatewaySemanticParityReport struct {
	FoundationPass   bool                                  `json:"foundation_pass"`
	Pass             bool                                  `json:"pass"`
	Fail             bool                                  `json:"fail"`
	BaselineCompared bool                                  `json:"baseline_compared"`
	DegradedReason   string                                `json:"degraded_reason,omitempty"`
	Candidate        CodexGatewaySemanticTraceSummary      `json:"candidate"`
	Baseline         *CodexGatewaySemanticTraceSummary     `json:"baseline,omitempty"`
	Invariants       []CodexGatewaySemanticInvariantResult `json:"invariants,omitempty"`
	Mismatches       []string                              `json:"mismatches,omitempty"`
}

func BuildCodexGatewaySemanticTraceFromSSE(stream string) (CodexGatewaySemanticTrace, error) {
	blocks := strings.Split(strings.TrimSpace(stream), "\n\n")
	trace := CodexGatewaySemanticTrace{RawStream: stream}
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		var (
			eventName string
			dataLines []string
		)
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "event:"):
				eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
		}
		if strings.TrimSpace(eventName) == "" {
			return CodexGatewaySemanticTrace{}, fmt.Errorf("semantic trace block missing event name")
		}
		payload := strings.Join(dataLines, "\n")
		if !json.Valid([]byte(payload)) {
			return CodexGatewaySemanticTrace{}, fmt.Errorf("semantic trace event %s payload is not valid json", eventName)
		}
		result := gjson.Parse(payload)
		event := CodexGatewaySemanticTraceEvent{
			EventType:      eventName,
			OutputIndex:    codexGatewaySemanticTraceIntPointer(result.Get("output_index")),
			ItemType:       codexGatewaySemanticTraceFirstNonEmpty(result.Get("item.type").String(), result.Get("item.item.type").String()),
			ItemName:       codexGatewaySemanticTraceFirstNonEmpty(result.Get("item.name").String(), result.Get("name").String()),
			Namespace:      codexGatewaySemanticTraceFirstNonEmpty(result.Get("item.namespace").String(), result.Get("namespace").String()),
			ItemStatus:     result.Get("item.status").String(),
			Phase:          codexGatewaySemanticTraceFirstNonEmpty(result.Get("item.phase").String(), result.Get("phase").String()),
			HasCallID:      codexGatewaySemanticTraceHasAny(result, "call_id", "item.call_id", "item.action.call_id"),
			HasItemID:      codexGatewaySemanticTraceHasAny(result, "item_id", "item.id"),
			TerminalStatus: result.Get("response.status").String(),
			ErrorCode:      codexGatewaySemanticTraceFirstNonEmpty(result.Get("response.error.code").String(), result.Get("error.code").String()),
			Usage:          codexGatewaySemanticTraceUsageShape(result.Get("response.usage")),
			raw:            append(json.RawMessage(nil), []byte(payload)...),
		}
		if event.ItemType == "" && event.EventType == "response.output_item.done" {
			event.ItemType = result.Get("item.type").String()
		}
		trace.Events = append(trace.Events, event)
		if seq := result.Get("sequence_number"); seq.Exists() {
			trace.SequenceNumbers = append(trace.SequenceNumbers, seq.Int())
		}
		if status, ok := codexGatewaySemanticTerminalEvents[eventName]; ok {
			trace.TerminalEvent = eventName
			trace.TerminalStatus = codexGatewaySemanticTraceFirstNonEmpty(event.TerminalStatus, status)
			trace.Usage = event.Usage
		}
	}
	return trace, nil
}

func BuildCodexGatewaySemanticParityReport(candidate CodexGatewaySemanticTrace, baseline *CodexGatewaySemanticTrace) CodexGatewaySemanticParityReport {
	invariants := codexGatewaySemanticTraceInvariantResults(candidate)
	candidatePass := true
	for _, invariant := range invariants {
		if !invariant.Pass {
			candidatePass = false
			break
		}
	}
	report := CodexGatewaySemanticParityReport{
		Candidate:      codexGatewaySemanticTraceSummary(candidate),
		Invariants:     invariants,
		FoundationPass: candidatePass,
	}
	if !candidatePass {
		report.Fail = true
		report.DegradedReason = "candidate_invariants_failed"
		return report
	}
	if baseline == nil {
		report.DegradedReason = "baseline_missing"
		return report
	}
	baselineSummary := codexGatewaySemanticTraceSummary(*baseline)
	report.BaselineCompared = true
	report.Baseline = &baselineSummary
	report.Mismatches = codexGatewaySemanticTraceSummaryMismatches(report.Candidate, baselineSummary)
	if len(report.Mismatches) > 0 {
		report.Fail = true
		report.DegradedReason = "semantic_mismatch"
		return report
	}
	report.Pass = true
	return report
}

func (r CodexGatewaySemanticParityReport) InvariantPassed(name string) bool {
	for _, invariant := range r.Invariants {
		if invariant.Name == name {
			return invariant.Pass
		}
	}
	return false
}

func codexGatewaySemanticTraceInvariantResults(trace CodexGatewaySemanticTrace) []CodexGatewaySemanticInvariantResult {
	return []CodexGatewaySemanticInvariantResult{
		codexGatewaySemanticSingleTerminalInvariant(trace),
		codexGatewaySemanticTerminalStatusInvariant(trace),
		codexGatewaySemanticNoRawLeakInvariant(trace),
		codexGatewaySemanticReasoningInvisibleInvariant(trace),
		codexGatewaySemanticSequenceInvariant(trace),
		codexGatewaySemanticOutputIndexInvariant(trace),
		codexGatewaySemanticOutputItemIDInvariant(trace),
		codexGatewaySemanticToolCallIDInvariant(trace),
		codexGatewaySemanticOutputItemPairInvariant(trace),
		codexGatewaySemanticToolDeltaInvariant(trace),
	}
}

func codexGatewaySemanticSingleTerminalInvariant(trace CodexGatewaySemanticTrace) CodexGatewaySemanticInvariantResult {
	count := 0
	lastIndex := -1
	for i, event := range trace.Events {
		if _, ok := codexGatewaySemanticTerminalEvents[event.EventType]; ok {
			count++
			lastIndex = i
		}
	}
	pass := count == 1 && lastIndex == len(trace.Events)-1
	details := fmt.Sprintf("terminal_count=%d last_terminal_index=%d event_count=%d", count, lastIndex, len(trace.Events))
	return CodexGatewaySemanticInvariantResult{Name: "single_terminal_event", Pass: pass, Details: details}
}

func codexGatewaySemanticTerminalStatusInvariant(trace CodexGatewaySemanticTrace) CodexGatewaySemanticInvariantResult {
	expected, ok := codexGatewaySemanticTerminalEvents[trace.TerminalEvent]
	pass := ok && trace.TerminalStatus == expected
	details := fmt.Sprintf("terminal_event=%s terminal_status=%s", trace.TerminalEvent, trace.TerminalStatus)
	return CodexGatewaySemanticInvariantResult{Name: "terminal_status_matches_event", Pass: pass, Details: details}
}

func codexGatewaySemanticNoRawLeakInvariant(trace CodexGatewaySemanticTrace) CodexGatewaySemanticInvariantResult {
	pass := !strings.Contains(trace.RawStream, "chat.completion.chunk") && !strings.Contains(trace.RawStream, "data: [DONE]")
	return CodexGatewaySemanticInvariantResult{Name: "no_raw_chat_completions_leak", Pass: pass}
}

func codexGatewaySemanticReasoningInvisibleInvariant(trace CodexGatewaySemanticTrace) CodexGatewaySemanticInvariantResult {
	pass := !strings.Contains(trace.RawStream, "response.reasoning_text.") &&
		!strings.Contains(trace.RawStream, "reasoning_content")
	return CodexGatewaySemanticInvariantResult{Name: "raw_reasoning_not_visible", Pass: pass}
}

func codexGatewaySemanticSequenceInvariant(trace CodexGatewaySemanticTrace) CodexGatewaySemanticInvariantResult {
	pass := len(trace.SequenceNumbers) == len(trace.Events)
	for i, seq := range trace.SequenceNumbers {
		if seq != int64(i) {
			pass = false
			break
		}
	}
	return CodexGatewaySemanticInvariantResult{Name: "sequence_numbers_contiguous", Pass: pass}
}

func codexGatewaySemanticOutputIndexInvariant(trace CodexGatewaySemanticTrace) CodexGatewaySemanticInvariantResult {
	seen := make(map[string]int)
	pass := true
	missing := make([]string, 0)
	for _, event := range trace.Events {
		if codexGatewaySemanticOutputIndexRequired(event) && event.OutputIndex == nil {
			pass = false
			missing = append(missing, event.EventType)
			continue
		}
		if event.OutputIndex == nil {
			continue
		}
		key := codexGatewaySemanticEventKey(event)
		if key == "" {
			continue
		}
		if prev, ok := seen[key]; ok && prev != *event.OutputIndex {
			pass = false
			break
		}
		seen[key] = *event.OutputIndex
	}
	return CodexGatewaySemanticInvariantResult{Name: "stable_output_index", Pass: pass, Details: strings.Join(missing, ",")}
}

func codexGatewaySemanticOutputItemIDInvariant(trace CodexGatewaySemanticTrace) CodexGatewaySemanticInvariantResult {
	pass := true
	missing := make([]string, 0)
	for _, event := range trace.Events {
		switch event.EventType {
		case "response.output_item.added", "response.output_item.done", "response.function_call_arguments.done", "response.custom_tool_call_input.done":
			if !event.HasItemID {
				pass = false
				missing = append(missing, event.EventType)
			}
		}
	}
	return CodexGatewaySemanticInvariantResult{Name: "output_item_id_presence", Pass: pass, Details: strings.Join(missing, ",")}
}

func codexGatewaySemanticToolCallIDInvariant(trace CodexGatewaySemanticTrace) CodexGatewaySemanticInvariantResult {
	seen := make(map[string]string)
	pass := true
	details := make([]string, 0)
	for _, event := range trace.Events {
		if !codexGatewaySemanticToolCallIDRequired(event) {
			continue
		}
		callID := strings.TrimSpace(codexGatewaySemanticEventCallID(event))
		if callID == "" {
			pass = false
			details = append(details, event.EventType+":missing_call_id")
			continue
		}
		itemID := codexGatewaySemanticEventItemID(event)
		if prev, ok := seen[callID]; ok && prev != itemID {
			pass = false
			details = append(details, callID+":duplicated_for_multiple_items")
			continue
		}
		seen[callID] = itemID
	}
	return CodexGatewaySemanticInvariantResult{Name: "tool_call_id_presence", Pass: pass, Details: strings.Join(details, ",")}
}

func codexGatewaySemanticOutputItemPairInvariant(trace CodexGatewaySemanticTrace) CodexGatewaySemanticInvariantResult {
	added := make(map[string]int)
	done := make(map[string]int)
	for _, event := range trace.Events {
		switch event.EventType {
		case "response.output_item.added":
			if key := codexGatewaySemanticEventKey(event); key != "" {
				added[key]++
			}
		case "response.output_item.done":
			if key := codexGatewaySemanticEventKey(event); key != "" {
				done[key]++
			}
		}
	}
	pass := true
	missing := make([]string, 0)
	if trace.TerminalEvent == "response.completed" {
		for key, count := range added {
			if done[key] < count {
				pass = false
				missing = append(missing, key)
			}
		}
	}
	return CodexGatewaySemanticInvariantResult{Name: "output_item_added_done_pairs", Pass: pass, Details: strings.Join(missing, ",")}
}

func codexGatewaySemanticToolDeltaInvariant(trace CodexGatewaySemanticTrace) CodexGatewaySemanticInvariantResult {
	funcDeltas := make(map[string]string)
	funcDone := make(map[string]string)
	customDeltas := make(map[string]string)
	customDone := make(map[string]string)
	for _, event := range trace.Events {
		key := codexGatewaySemanticEventKey(event)
		switch event.EventType {
		case "response.function_call_arguments.delta":
			funcDeltas[key] += codexGatewaySemanticEventDelta(event)
		case "response.function_call_arguments.done":
			funcDone[key] = codexGatewaySemanticEventArguments(event)
		case "response.custom_tool_call_input.delta":
			customDeltas[key] += codexGatewaySemanticEventDelta(event)
		case "response.custom_tool_call_input.done":
			customDone[key] = codexGatewaySemanticEventInput(event)
		}
	}
	pass := true
	mismatches := make([]string, 0)
	for key, delta := range funcDeltas {
		done, ok := funcDone[key]
		if !ok {
			if trace.TerminalEvent == "response.completed" {
				pass = false
				mismatches = append(mismatches, key+":function_missing_done")
			}
			continue
		}
		if done != delta {
			pass = false
			mismatches = append(mismatches, key+":function")
		}
	}
	for key, delta := range customDeltas {
		done, ok := customDone[key]
		if !ok {
			if trace.TerminalEvent == "response.completed" {
				pass = false
				mismatches = append(mismatches, key+":custom_missing_done")
			}
			continue
		}
		if done != delta {
			pass = false
			mismatches = append(mismatches, key+":custom")
		}
	}
	return CodexGatewaySemanticInvariantResult{Name: "tool_delta_reconstructable", Pass: pass, Details: strings.Join(mismatches, ",")}
}

func codexGatewaySemanticTraceSummary(trace CodexGatewaySemanticTrace) CodexGatewaySemanticTraceSummary {
	summary := CodexGatewaySemanticTraceSummary{
		EventTypes:     make([]string, 0, len(trace.Events)),
		EventCount:     len(trace.Events),
		TerminalEvent:  trace.TerminalEvent,
		TerminalStatus: trace.TerminalStatus,
		Usage:          trace.Usage,
	}
	for _, event := range trace.Events {
		summary.EventTypes = append(summary.EventTypes, event.EventType)
		if event.EventType == "response.output_item.added" && event.ItemType != "" {
			summary.OutputItemTypes = append(summary.OutputItemTypes, event.ItemType)
			if event.ItemName != "" {
				summary.OutputItemNames = append(summary.OutputItemNames, codexGatewaySemanticTraceLabel(event.Namespace, event.ItemName))
			}
			if event.ItemType == "message" && event.Phase != "" {
				summary.MessagePhases = append(summary.MessagePhases, event.Phase)
			}
		}
		if summary.ErrorCode == "" && event.ErrorCode != "" {
			summary.ErrorCode = event.ErrorCode
		}
	}
	return summary
}

func codexGatewaySemanticTraceSummaryMismatches(candidate, baseline CodexGatewaySemanticTraceSummary) []string {
	mismatches := make([]string, 0)
	if !reflect.DeepEqual(candidate.EventTypes, baseline.EventTypes) {
		mismatches = append(mismatches, "event_types")
	}
	if !reflect.DeepEqual(candidate.OutputItemTypes, baseline.OutputItemTypes) {
		mismatches = append(mismatches, "output_item_types")
	}
	if !reflect.DeepEqual(candidate.OutputItemNames, baseline.OutputItemNames) {
		mismatches = append(mismatches, "output_item_names")
	}
	if !reflect.DeepEqual(candidate.MessagePhases, baseline.MessagePhases) {
		mismatches = append(mismatches, "message_phases")
	}
	if candidate.TerminalEvent != baseline.TerminalEvent {
		mismatches = append(mismatches, "terminal_event")
	}
	if candidate.TerminalStatus != baseline.TerminalStatus {
		mismatches = append(mismatches, "terminal_status")
	}
	if candidate.ErrorCode != baseline.ErrorCode {
		mismatches = append(mismatches, "error_code")
	}
	if !reflect.DeepEqual(candidate.Usage, baseline.Usage) {
		mismatches = append(mismatches, "usage_shape")
	}
	return mismatches
}

func codexGatewaySemanticTraceUsageShape(value gjson.Result) CodexGatewaySemanticUsageShape {
	if !value.Exists() {
		return CodexGatewaySemanticUsageShape{}
	}
	return CodexGatewaySemanticUsageShape{
		Present:              true,
		HasInputTokens:       value.Get("input_tokens").Exists(),
		HasOutputTokens:      value.Get("output_tokens").Exists(),
		HasTotalTokens:       value.Get("total_tokens").Exists(),
		HasCachedInputTokens: value.Get("input_tokens_details.cached_tokens").Exists(),
	}
}

func codexGatewaySemanticTraceIntPointer(value gjson.Result) *int {
	if !value.Exists() {
		return nil
	}
	v := int(value.Int())
	return &v
}

func codexGatewaySemanticTraceHasAny(result gjson.Result, paths ...string) bool {
	for _, path := range paths {
		value := result.Get(path)
		if value.Exists() && strings.TrimSpace(value.String()) != "" {
			return true
		}
	}
	return false
}

func codexGatewaySemanticTraceFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func codexGatewaySemanticIsToolEvent(event CodexGatewaySemanticTraceEvent) bool {
	switch event.ItemType {
	case CodexGatewayOutputItemTypeFunctionCall, CodexGatewayOutputItemTypeCustomToolCall:
		return true
	}
	switch event.EventType {
	case "response.function_call_arguments.done", "response.custom_tool_call_input.delta", "response.custom_tool_call_input.done":
		return true
	default:
		return false
	}
}

func codexGatewaySemanticToolCallIDRequired(event CodexGatewaySemanticTraceEvent) bool {
	switch event.EventType {
	case "response.output_item.added", "response.output_item.done", "response.function_call_arguments.done", "response.custom_tool_call_input.delta":
		return codexGatewaySemanticIsToolEvent(event) || event.EventType == "response.custom_tool_call_input.delta"
	default:
		return false
	}
}

func codexGatewaySemanticOutputIndexRequired(event CodexGatewaySemanticTraceEvent) bool {
	switch event.EventType {
	case "response.output_item.added",
		"response.output_item.done",
		"response.output_text.delta",
		"response.output_text.done",
		"response.function_call_arguments.delta",
		"response.function_call_arguments.done",
		"response.custom_tool_call_input.delta",
		"response.custom_tool_call_input.done":
		return true
	default:
		return false
	}
}

func codexGatewaySemanticEventKey(event CodexGatewaySemanticTraceEvent) string {
	if id := strings.TrimSpace(codexGatewaySemanticEventItemID(event)); id != "" {
		return id
	}
	if callID := strings.TrimSpace(codexGatewaySemanticEventCallID(event)); callID != "" {
		return "call_id:" + callID
	}
	if event.OutputIndex != nil {
		return fmt.Sprintf("output_index:%d", *event.OutputIndex)
	}
	return ""
}

func codexGatewaySemanticEventCallID(event CodexGatewaySemanticTraceEvent) string {
	if raw := gjson.Get(event.rawJSON(), "call_id").String(); strings.TrimSpace(raw) != "" {
		return raw
	}
	return gjson.Get(event.rawJSON(), "item.call_id").String()
}

func codexGatewaySemanticEventItemID(event CodexGatewaySemanticTraceEvent) string {
	if raw := gjson.Get(event.rawJSON(), "item_id").String(); strings.TrimSpace(raw) != "" {
		return raw
	}
	return gjson.Get(event.rawJSON(), "item.id").String()
}

func codexGatewaySemanticEventDelta(event CodexGatewaySemanticTraceEvent) string {
	return gjson.Get(event.rawJSON(), "delta").String()
}

func codexGatewaySemanticEventArguments(event CodexGatewaySemanticTraceEvent) string {
	if raw := gjson.Get(event.rawJSON(), "arguments").String(); raw != "" {
		return raw
	}
	return gjson.Get(event.rawJSON(), "item.arguments").String()
}

func codexGatewaySemanticEventInput(event CodexGatewaySemanticTraceEvent) string {
	if raw := gjson.Get(event.rawJSON(), "input").String(); raw != "" {
		return raw
	}
	return gjson.Get(event.rawJSON(), "item.input").String()
}

func (e CodexGatewaySemanticTraceEvent) rawJSON() string {
	return string(e.raw)
}

func codexGatewaySemanticTraceLabel(namespace, name string) string {
	namespace = strings.TrimSpace(namespace)
	name = strings.TrimSpace(name)
	if namespace == "" {
		return name
	}
	if name == "" {
		return namespace
	}
	return namespace + "::" + name
}
