package service

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const OpenAIRuntimeGuardMetadataKey = "openai_runtime_guard.metadata"

type OpenAIRuntimeGuardMetadata struct {
	Action   string `json:"action"`
	Category string `json:"category"`
	Metric   string `json:"metric"`
	Field    string `json:"field"`
	Path     string `json:"path,omitempty"`
	From     string `json:"from,omitempty"`
	To       string `json:"to,omitempty"`
	Status   int    `json:"status,omitempty"`
}

type openAIReasoningEffortGuardDecision struct {
	Action   string
	Blocked  bool
	Repaired bool
	Present  bool
	Status   int
	Path     string
	From     string
	To       string
	Category string
	Metric   string
}

func evaluateOpenAIReasoningEffortGuard(body []byte) openAIReasoningEffortGuardDecision {
	path, raw, present := openAIReasoningEffortGuardInput(body)
	if !present {
		return openAIReasoningEffortGuardDecision{}
	}

	value := strings.TrimSpace(raw)
	if value == "" {
		return openAIReasoningEffortGuardDecision{
			Action:   "pass",
			Present:  true,
			Path:     path,
			Category: "reasoning.valid_effort",
			Metric:   "openai_runtime_guard.passed.reasoning_effort",
		}
	}
	normalized := normalizeOpenAIReasoningEffort(value)
	if normalized == "" {
		return openAIReasoningEffortGuardDecision{
			Action:   "block",
			Blocked:  true,
			Present:  true,
			Status:   http.StatusBadRequest,
			Path:     path,
			From:     value,
			Category: "reasoning.unknown_effort",
			Metric:   "openai_runtime_guard.blocked.reasoning_effort",
		}
	}

	if openAIReasoningEffortGuardNeedsRepair(path, value, normalized) {
		return openAIReasoningEffortGuardDecision{
			Action:   "repair",
			Repaired: true,
			Present:  true,
			Path:     path,
			From:     value,
			To:       normalized,
			Category: "reasoning.unsupported_effort_repaired",
			Metric:   "openai_runtime_guard.repaired.reasoning_effort",
		}
	}

	return openAIReasoningEffortGuardDecision{
		Action:   "pass",
		Present:  true,
		Path:     path,
		From:     value,
		To:       normalized,
		Category: "reasoning.valid_effort",
		Metric:   "openai_runtime_guard.passed.reasoning_effort",
	}
}

func openAIReasoningEffortGuardInput(body []byte) (path string, raw string, present bool) {
	if len(body) == 0 {
		return "", "", false
	}
	if nested := gjson.GetBytes(body, "reasoning.effort"); nested.Exists() {
		return "reasoning.effort", nested.String(), true
	}
	if flat := gjson.GetBytes(body, "reasoning_effort"); flat.Exists() {
		return "reasoning_effort", flat.String(), true
	}
	return "", "", false
}

func openAIReasoningEffortGuardNeedsRepair(path, raw, normalized string) bool {
	value := normalizeOpenAIReasoningEffortToken(raw)
	if value == "" {
		return false
	}
	if path == "reasoning.effort" && value == "minimal" && normalized == "none" {
		return true
	}
	if value == "minimal" && normalized == "none" {
		return false
	}
	return strings.ToLower(strings.TrimSpace(raw)) != normalized
}

func writeOpenAIReasoningEffortGuardBlockedResponse(c *gin.Context, decision openAIReasoningEffortGuardDecision) {
	if c == nil {
		return
	}
	status := decision.Status
	if status == 0 {
		status = http.StatusBadRequest
	}
	param := decision.Path
	if param == "reasoning.effort" {
		param = "reasoning.effort"
	}
	c.JSON(status, gin.H{
		"error": gin.H{
			"type":    "invalid_request_error",
			"message": "Unsupported reasoning_effort value",
			"param":   param,
		},
	})
}

func setOpenAIRuntimeGuardReasoningMetadata(c *gin.Context, decision openAIReasoningEffortGuardDecision) {
	if c == nil || !decision.Present || decision.Category == "" || decision.Metric == "" {
		return
	}
	c.Set(OpenAIRuntimeGuardMetadataKey, OpenAIRuntimeGuardMetadata{
		Action:   decision.Action,
		Category: decision.Category,
		Metric:   decision.Metric,
		Field:    "reasoning_effort",
		Path:     decision.Path,
		From:     decision.From,
		To:       decision.To,
		Status:   decision.Status,
	})
}

func normalizeOpenAIReasoningEffortToken(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return ""
	}
	return strings.NewReplacer("-", "", "_", "", " ", "").Replace(value)
}
