package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	OpenAIRuntimeGuardMetadataKey         = "openai_runtime_guard.metadata"
	openAIRuntimeGuardMetadataValueMaxLen = 64
)

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

type openAIReasoningEffortGuardRepair struct {
	Path string
	From string
	To   string
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
	Repairs  []openAIReasoningEffortGuardRepair
}

type openAIReasoningEffortGuardInput struct {
	Path string
	Raw  string
}

func shouldApplyOpenAIReasoningEffortGuard(account *Account) bool {
	return account != nil && account.Platform == PlatformOpenAI && account.Type == AccountTypeOAuth
}

func evaluateOpenAIReasoningEffortGuard(body []byte) openAIReasoningEffortGuardDecision {
	inputs := openAIReasoningEffortGuardInputs(body)
	if len(inputs) == 0 {
		return openAIReasoningEffortGuardDecision{}
	}

	var repairs []openAIReasoningEffortGuardRepair
	var firstPath, firstFrom, firstTo string
	var normalizedSeen bool
	var normalizedValue string

	for _, input := range inputs {
		value := strings.TrimSpace(input.Raw)
		if firstPath == "" {
			firstPath = input.Path
			firstFrom = safeOpenAIRuntimeGuardMetadataValue(value)
		}
		if value == "" {
			continue
		}
		normalized, ok := normalizeOpenAIRuntimeGuardReasoningEffort(value)
		if !ok {
			return openAIReasoningEffortGuardDecision{
				Action:   "block",
				Blocked:  true,
				Present:  true,
				Status:   http.StatusBadRequest,
				Path:     input.Path,
				From:     safeOpenAIRuntimeGuardMetadataValue(value),
				Category: "reasoning.unknown_effort",
				Metric:   "openai_runtime_guard.blocked.reasoning_effort",
			}
		}
		if normalizedSeen && normalized != normalizedValue {
			return openAIReasoningEffortGuardDecision{
				Action:   "block",
				Blocked:  true,
				Present:  true,
				Status:   http.StatusBadRequest,
				Path:     input.Path,
				From:     safeOpenAIRuntimeGuardMetadataValue(value),
				To:       normalized,
				Category: "reasoning.conflicting_effort",
				Metric:   "openai_runtime_guard.blocked.reasoning_effort",
			}
		}
		normalizedSeen = true
		normalizedValue = normalized
		if firstTo == "" {
			firstTo = normalized
		}
		if openAIReasoningEffortGuardNeedsRepair(value, normalized) {
			repairs = append(repairs, openAIReasoningEffortGuardRepair{Path: input.Path, From: safeOpenAIRuntimeGuardMetadataValue(value), To: normalized})
		}
	}

	if len(repairs) > 0 {
		return openAIReasoningEffortGuardDecision{
			Action:   "repair",
			Repaired: true,
			Present:  true,
			Path:     repairs[0].Path,
			From:     repairs[0].From,
			To:       repairs[0].To,
			Category: "reasoning.unsupported_effort_repaired",
			Metric:   "openai_runtime_guard.repaired.reasoning_effort",
			Repairs:  repairs,
		}
	}

	return openAIReasoningEffortGuardDecision{
		Action:   "pass",
		Present:  true,
		Path:     firstPath,
		From:     firstFrom,
		To:       firstTo,
		Category: "reasoning.valid_effort",
		Metric:   "openai_runtime_guard.passed.reasoning_effort",
	}
}

func openAIReasoningEffortGuardInputs(body []byte) []openAIReasoningEffortGuardInput {
	if len(body) == 0 {
		return nil
	}
	inputs := make([]openAIReasoningEffortGuardInput, 0, 2)
	if nested := gjson.GetBytes(body, "reasoning.effort"); nested.Exists() {
		inputs = append(inputs, openAIReasoningEffortGuardInput{Path: "reasoning.effort", Raw: nested.String()})
	}
	if flat := gjson.GetBytes(body, "reasoning_effort"); flat.Exists() {
		inputs = append(inputs, openAIReasoningEffortGuardInput{Path: "reasoning_effort", Raw: flat.String()})
	}
	return inputs
}

func normalizeOpenAIRuntimeGuardReasoningEffort(raw string) (string, bool) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "low", "medium", "high", "xhigh", "none":
		return value, true
	case "minimal":
		return "none", true
	case "max", "maximum", "x-high", "x_high":
		return "xhigh", true
	default:
		return "", false
	}
}

func openAIReasoningEffortGuardNeedsRepair(raw, normalized string) bool {
	return strings.ToLower(strings.TrimSpace(raw)) != normalized
}

func applyOpenAIReasoningEffortGuardRepairs(body []byte, decision openAIReasoningEffortGuardDecision) ([]byte, error) {
	if !decision.Repaired {
		return body, nil
	}
	updated := body
	for _, repair := range decision.Repairs {
		if strings.TrimSpace(repair.Path) == "" {
			continue
		}
		next, err := sjson.SetBytes(updated, repair.Path, repair.To)
		if err != nil {
			return body, fmt.Errorf("repair openai reasoning_effort: %w", err)
		}
		updated = next
	}
	return updated, nil
}

func writeOpenAIReasoningEffortGuardBlockedResponse(c *gin.Context, decision openAIReasoningEffortGuardDecision) {
	if c == nil {
		return
	}
	status := openAIReasoningEffortGuardBlockedStatus(decision)
	c.Data(status, "application/json; charset=utf-8", openAIReasoningEffortGuardBlockedPayload(decision))
}

func newOpenAIReasoningEffortGuardBlockedHTTPResponse(decision openAIReasoningEffortGuardDecision) *http.Response {
	status := openAIReasoningEffortGuardBlockedStatus(decision)
	payload := openAIReasoningEffortGuardBlockedPayload(decision)
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     http.Header{"Content-Type": []string{"application/json; charset=utf-8"}},
		Body:       io.NopCloser(strings.NewReader(string(payload))),
	}
}

func openAIReasoningEffortGuardBlockedStatus(decision openAIReasoningEffortGuardDecision) int {
	if decision.Status != 0 {
		return decision.Status
	}
	return http.StatusBadRequest
}

func openAIReasoningEffortGuardBlockedPayload(decision openAIReasoningEffortGuardDecision) []byte {
	param := decision.Path
	if param == "" {
		param = "reasoning_effort"
	}
	payload, err := json.Marshal(map[string]any{
		"error": map[string]any{
			"type":    "invalid_request_error",
			"message": "Unsupported reasoning_effort value",
			"param":   param,
		},
	})
	if err != nil {
		return []byte(`{"error":{"type":"invalid_request_error","message":"Unsupported reasoning_effort value","param":"reasoning_effort"}}`)
	}
	return payload
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
		From:     safeOpenAIRuntimeGuardMetadataValue(decision.From),
		To:       safeOpenAIRuntimeGuardMetadataValue(decision.To),
		Status:   decision.Status,
	})
}

func safeOpenAIRuntimeGuardMetadataValue(raw string) string {
	value := sanitizeUpstreamErrorMessage(strings.TrimSpace(raw))
	if value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) > openAIRuntimeGuardMetadataValueMaxLen {
		return string(runes[:openAIRuntimeGuardMetadataValueMaxLen])
	}
	return value
}
