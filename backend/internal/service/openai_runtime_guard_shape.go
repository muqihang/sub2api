package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func applyOpenAIRuntimeGuardShapeGuardToBody(body []byte) ([]byte, *OpenAIRuntimeGuardBlockedError, error) {
	if len(body) == 0 {
		return body, nil, nil
	}
	var reqBody map[string]any
	if err := json.Unmarshal(body, &reqBody); err != nil {
		return body, nil, nil
	}
	decision := applyOpenAIRuntimeGuardShapeGuard(reqBody)
	if decision.Blocked {
		return body, newOpenAIRuntimeGuardBlockedError(decision), nil
	}
	if !decision.Repaired {
		return body, nil, nil
	}
	repaired, err := marshalOpenAIUpstreamJSON(reqBody)
	if err != nil {
		return body, nil, fmt.Errorf("serialize openai runtime shape guard repair: %w", err)
	}
	return repaired, nil, nil
}

func applyOpenAIRuntimeGuardShapeGuard(reqBody map[string]any) openAIReasoningEffortGuardDecision {
	if reqBody == nil {
		return openAIReasoningEffortGuardDecision{}
	}
	assistantDecision := applyOpenAIRuntimeGuardAssistantContentShape(reqBody)
	if assistantDecision.Blocked {
		return assistantDecision
	}
	toolDecision := evaluateOpenAIRuntimeGuardToolContinuationShape(reqBody)
	if toolDecision.Blocked {
		return toolDecision
	}
	if assistantDecision.Repaired {
		return assistantDecision
	}
	return toolDecision
}

func applyOpenAIRuntimeGuardAssistantContentShape(reqBody map[string]any) openAIReasoningEffortGuardDecision {
	input, ok := reqBody["input"].([]any)
	if !ok {
		return openAIReasoningEffortGuardDecision{}
	}
	var repairs []openAIReasoningEffortGuardRepair
	for itemIndex, rawItem := range input {
		item, ok := rawItem.(map[string]any)
		if !ok || strings.TrimSpace(firstNonEmptyString(item["type"])) != "message" {
			continue
		}
		if strings.TrimSpace(firstNonEmptyString(item["role"])) != "assistant" {
			continue
		}
		parts, ok := item["content"].([]any)
		if !ok {
			continue
		}
		for partIndex, rawPart := range parts {
			part, ok := rawPart.(map[string]any)
			if !ok {
				continue
			}
			partType := strings.TrimSpace(firstNonEmptyString(part["type"]))
			path := fmt.Sprintf("input[%d].content[%d].type", itemIndex, partIndex)
			switch partType {
			case "input_text", "text":
				if _, ok := part["text"].(string); !ok {
					return openAIReasoningEffortGuardDecision{
						Action:   "block",
						Blocked:  true,
						Present:  true,
						Status:   http.StatusBadRequest,
						Path:     path,
						From:     safeOpenAIRuntimeGuardMetadataValue(partType),
						Category: "shape.assistant_input_content_blocked",
						Metric:   "openai_runtime_guard.blocked.assistant_content_part",
					}
				}
				part["type"] = "output_text"
				repairs = append(repairs, openAIReasoningEffortGuardRepair{Path: path, From: partType, To: "output_text"})
			case "output_text", "refusal", "":
				continue
			default:
				if isOpenAIRuntimeGuardAssistantInputOnlyContentPart(partType) {
					return openAIReasoningEffortGuardDecision{
						Action:   "block",
						Blocked:  true,
						Present:  true,
						Status:   http.StatusBadRequest,
						Path:     path,
						From:     safeOpenAIRuntimeGuardMetadataValue(partType),
						Category: "shape.assistant_input_content_blocked",
						Metric:   "openai_runtime_guard.blocked.assistant_content_part",
					}
				}
			}
		}
	}
	if len(repairs) == 0 {
		return openAIReasoningEffortGuardDecision{}
	}
	return openAIReasoningEffortGuardDecision{
		Action:   "repair",
		Repaired: true,
		Present:  true,
		Path:     repairs[0].Path,
		From:     repairs[0].From,
		To:       repairs[0].To,
		Category: "shape.assistant_content_part_repaired",
		Metric:   "openai_runtime_guard.repaired.assistant_content_part",
		Repairs:  repairs,
	}
}

func isOpenAIRuntimeGuardAssistantInputOnlyContentPart(partType string) bool {
	switch strings.TrimSpace(partType) {
	case "input_image", "input_audio", "input_file":
		return true
	default:
		return false
	}
}

func evaluateOpenAIRuntimeGuardToolContinuationShape(reqBody map[string]any) openAIReasoningEffortGuardDecision {
	input, ok := reqBody["input"].([]any)
	if !ok {
		return openAIReasoningEffortGuardDecision{}
	}
	hasPreviousResponseID := hasNonEmptyString(reqBody["previous_response_id"])
	contextCallIDs := make(map[string]struct{})
	outputCallIDs := make(map[string]struct{})
	referenceIDs := make(map[string]struct{})
	var firstContextPath string
	var firstOutputPath string

	for index, rawItem := range input {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		itemType := strings.TrimSpace(firstNonEmptyString(item["type"]))
		switch {
		case isCodexToolCallContextItemType(itemType):
			callID := strings.TrimSpace(firstNonEmptyString(item["call_id"]))
			if callID != "" {
				contextCallIDs[callID] = struct{}{}
				if firstContextPath == "" {
					firstContextPath = fmt.Sprintf("input[%d]", index)
				}
			}
		case isCodexToolCallOutputItemType(itemType):
			callID := strings.TrimSpace(firstNonEmptyString(item["call_id"]))
			if firstOutputPath == "" {
				firstOutputPath = fmt.Sprintf("input[%d]", index)
			}
			if callID == "" {
				return openAIRuntimeGuardShapeBlock("shape.tool_output_missing_context", "openai_runtime_guard.blocked.tool_output_context", fmt.Sprintf("input[%d].call_id", index), itemType)
			}
			outputCallIDs[callID] = struct{}{}
		case itemType == "item_reference":
			id := strings.TrimSpace(firstNonEmptyString(item["id"]))
			if id != "" {
				referenceIDs[id] = struct{}{}
			}
		}
	}

	if len(outputCallIDs) > 0 && !hasPreviousResponseID && !openAIRuntimeGuardIDsCoveredByAny(outputCallIDs, contextCallIDs, referenceIDs) {
		return openAIRuntimeGuardShapeBlock("shape.tool_output_missing_context", "openai_runtime_guard.blocked.tool_output_context", firstNonBlankString(firstOutputPath, "input"), "")
	}
	if len(contextCallIDs) > 0 && !openAIRuntimeGuardIDsCovered(contextCallIDs, outputCallIDs) && !openAIRuntimeGuardIDsCovered(contextCallIDs, referenceIDs) {
		return openAIRuntimeGuardShapeBlock("shape.missing_tool_output", "openai_runtime_guard.blocked.missing_tool_output", firstNonBlankString(firstContextPath, "input"), "")
	}
	return openAIReasoningEffortGuardDecision{}
}

func openAIRuntimeGuardIDsCoveredByAny(required map[string]struct{}, availableSets ...map[string]struct{}) bool {
	if len(required) == 0 || len(availableSets) == 0 {
		return false
	}
	for id := range required {
		covered := false
		for _, available := range availableSets {
			if _, ok := available[id]; ok {
				covered = true
				break
			}
		}
		if !covered {
			return false
		}
	}
	return true
}

func openAIRuntimeGuardIDsCovered(required map[string]struct{}, available map[string]struct{}) bool {
	if len(required) == 0 || len(available) == 0 {
		return false
	}
	for id := range required {
		if _, ok := available[id]; !ok {
			return false
		}
	}
	return true
}

func openAIRuntimeGuardShapeBlock(category, metric, path, from string) openAIReasoningEffortGuardDecision {
	return openAIReasoningEffortGuardDecision{
		Action:   "block",
		Blocked:  true,
		Present:  true,
		Status:   http.StatusBadRequest,
		Path:     path,
		From:     safeOpenAIRuntimeGuardMetadataValue(from),
		Category: category,
		Metric:   metric,
	}
}
