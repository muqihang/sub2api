package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	OpenAIRuntimeGuardMetadataKey         = "openai_runtime_guard.metadata"
	openAIRuntimeGuardMetadataValueMaxLen = 64
)

type OpenAIRuntimeGuardMetadata struct {
	Action           string `json:"action"`
	Category         string `json:"category"`
	Metric           string `json:"metric"`
	Field            string `json:"field"`
	Path             string `json:"path,omitempty"`
	From             string `json:"from,omitempty"`
	To               string `json:"to,omitempty"`
	Status           int    `json:"status,omitempty"`
	EstimatedTokens  int    `json:"estimated_tokens,omitempty"`
	LimitTokens      int    `json:"limit_tokens,omitempty"`
	ReserveTokens    int    `json:"reserve_tokens,omitempty"`
	Confidence       string `json:"confidence,omitempty"`
	TextHash         string `json:"text_hash,omitempty"`
	SanitizedSummary string `json:"sanitized_summary,omitempty"`
}

type openAIReasoningEffortGuardRepair struct {
	Path   string
	From   string
	To     string
	Delete bool
}

type openAIReasoningEffortGuardDecision struct {
	Action          string
	Blocked         bool
	Repaired        bool
	Present         bool
	Status          int
	Path            string
	From            string
	To              string
	Category        string
	Metric          string
	Repairs         []openAIReasoningEffortGuardRepair
	EstimatedTokens int
	LimitTokens     int
	ReserveTokens   int
	Confidence      string
}

type openAIReasoningEffortGuardInput struct {
	Path  string
	Raw   string
	Empty bool
}

// OpenAIRuntimeGuardBlockedError is a local guard rejection. It intentionally
// does not masquerade as an upstream http.Response, so callers do not record
// provider success, usage, or upstream response captures for local blocks.
type OpenAIRuntimeGuardBlockedError struct {
	StatusCode int
	Payload    []byte
	Decision   openAIReasoningEffortGuardDecision
}

func (e *OpenAIRuntimeGuardBlockedError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("openai runtime guard blocked request: status=%d category=%s", e.StatusCode, e.Decision.Category)
}

func newOpenAIRuntimeGuardBlockedError(decision openAIReasoningEffortGuardDecision) *OpenAIRuntimeGuardBlockedError {
	return &OpenAIRuntimeGuardBlockedError{
		StatusCode: openAIReasoningEffortGuardBlockedStatus(decision),
		Payload:    openAIReasoningEffortGuardBlockedPayload(decision),
		Decision:   decision,
	}
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
	var emptyRepairs []openAIReasoningEffortGuardRepair
	var firstPath, firstFrom, firstTo string
	var normalizedSeen bool
	var normalizedValue string

	for _, input := range inputs {
		value := strings.TrimSpace(input.Raw)
		if value == "" || input.Empty {
			emptyRepairs = append(emptyRepairs, openAIReasoningEffortGuardRepair{Path: input.Path, From: safeOpenAIRuntimeGuardMetadataValue(value), Delete: true})
			continue
		}
		if firstPath == "" {
			firstPath = input.Path
			firstFrom = safeOpenAIRuntimeGuardMetadataValue(value)
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

	if !normalizedSeen && len(emptyRepairs) > 0 {
		return openAIReasoningEffortGuardDecision{
			Action:   "repair",
			Repaired: true,
			Present:  true,
			Path:     emptyRepairs[0].Path,
			From:     emptyRepairs[0].From,
			Category: "reasoning.empty_effort_removed",
			Metric:   "openai_runtime_guard.repaired.reasoning_effort",
			Repairs:  emptyRepairs,
		}
	}
	if len(emptyRepairs) > 0 {
		repairs = append(emptyRepairs, repairs...)
	}

	if len(repairs) > 0 {
		category := "reasoning.unsupported_effort_repaired"
		decisionPath := repairs[0].Path
		decisionFrom := repairs[0].From
		decisionTo := repairs[0].To
		if repairs[0].Delete {
			category = "reasoning.empty_effort_removed"
			if firstPath != "" {
				decisionPath = firstPath
				decisionFrom = firstFrom
				decisionTo = firstTo
			}
		}
		return openAIReasoningEffortGuardDecision{
			Action:   "repair",
			Repaired: true,
			Present:  true,
			Path:     decisionPath,
			From:     decisionFrom,
			To:       decisionTo,
			Category: category,
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
		inputs = append(inputs, openAIReasoningEffortGuardInput{Path: "reasoning.effort", Raw: nested.String(), Empty: nested.Type == gjson.Null})
	}
	if flat := gjson.GetBytes(body, "reasoning_effort"); flat.Exists() {
		inputs = append(inputs, openAIReasoningEffortGuardInput{Path: "reasoning_effort", Raw: flat.String(), Empty: flat.Type == gjson.Null})
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
		var next []byte
		var err error
		if repair.Delete {
			next, err = sjson.DeleteBytes(updated, repair.Path)
		} else {
			next, err = sjson.SetBytes(updated, repair.Path, repair.To)
		}
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

func applyOpenAIReasoningEffortGuardToWSResponseCreatePayload(account *Account, payload []byte) ([]byte, *OpenAIRuntimeGuardBlockedError, error) {
	return applyOpenAIReasoningEffortGuardToWSResponseCreatePayloadWithProvider(context.Background(), account, payload, "", nil)
}

func applyOpenAIReasoningEffortGuardToWSResponseCreatePayloadWithModel(account *Account, payload []byte, resolvedModel string) ([]byte, *OpenAIRuntimeGuardBlockedError, error) {
	return applyOpenAIReasoningEffortGuardToWSResponseCreatePayloadWithProvider(context.Background(), account, payload, resolvedModel, nil)
}

func applyOpenAIReasoningEffortGuardToWSResponseCreatePayloadWithProvider(ctx context.Context, account *Account, payload []byte, resolvedModel string, provider OpenAIContentSafetyProvider) ([]byte, *OpenAIRuntimeGuardBlockedError, error) {
	if !shouldApplyOpenAIReasoningEffortGuard(account) {
		return payload, nil, nil
	}
	if !openAIRuntimeGuardShouldTreatWSFrameAsResponseCreate(payload) {
		return payload, nil, nil
	}
	shapeApplied, shapeBlocked, shapeErr := applyOpenAIRuntimeGuardShapeGuardToBody(payload)
	if shapeErr != nil {
		return payload, nil, shapeErr
	}
	if shapeBlocked != nil {
		return payload, shapeBlocked, nil
	}
	payload = shapeApplied
	decision := evaluateOpenAIReasoningEffortGuard(payload)
	if decision.Blocked {
		return payload, newOpenAIRuntimeGuardBlockedError(decision), nil
	}
	if decision.Repaired {
		repaired, err := applyOpenAIReasoningEffortGuardRepairs(payload, decision)
		if err != nil {
			return payload, nil, err
		}
		payload = repaired
	}
	contentSafetyDecision := evaluateOpenAIRuntimeGuardContentSafetyWithProvider(ctx, account, ContentModerationProtocolOpenAIResponses, payload, provider)
	if blocked := openAIRuntimeGuardContentSafetyDecisionToBlockedError(contentSafetyDecision); blocked != nil {
		return payload, blocked, nil
	}
	model := strings.TrimSpace(resolvedModel)
	if model == "" {
		model = strings.TrimSpace(gjson.GetBytes(payload, "model").String())
	}
	if model != "" {
		model = openAIAccountRuntimeGuardResolvedUpstreamModel(account, model)
	}
	if blocked := applyOpenAIRuntimeGuardContextToBody(account, model, payload, false); blocked != nil {
		return payload, blocked, nil
	}
	return payload, nil, nil
}

func openAIRuntimeGuardShouldTreatWSFrameAsResponseCreate(payload []byte) bool {
	if len(payload) == 0 {
		return false
	}
	frameType := strings.TrimSpace(gjson.GetBytes(payload, "type").String())
	if frameType == "response.create" {
		return true
	}
	if frameType != "" {
		return false
	}
	for _, path := range []string{
		"model",
		"input",
		"instructions",
		"previous_response_id",
		"prompt_cache_key",
		"reasoning_effort",
		"reasoning.effort",
		"tools",
		"tool_choice",
		"stream",
	} {
		if gjson.GetBytes(payload, path).Exists() {
			return true
		}
	}
	return false
}

func (s *OpenAIGatewayService) applyOpenAIRuntimeGuardToWSResponseCreatePayloadWithModel(ctx context.Context, account *Account, payload []byte, resolvedModel string) ([]byte, *OpenAIRuntimeGuardBlockedError, error) {
	provider := OpenAIContentSafetyProvider(nil)
	if s != nil {
		provider = s.openAIContentSafetyProvider
	}
	return applyOpenAIReasoningEffortGuardToWSResponseCreatePayloadWithProvider(ctx, account, payload, resolvedModel, provider)
}

func (s *OpenAIGatewayService) ApplyOpenAIRuntimeGuardToWSResponseCreatePayload(account *Account, payload []byte) ([]byte, *OpenAIRuntimeGuardBlockedError, error) {
	return s.applyOpenAIRuntimeGuardToWSResponseCreatePayloadWithModel(context.Background(), account, payload, "")
}

func BuildOpenAIRuntimeGuardSelectionWSEvent(selectionErr *OpenAIRuntimeGuardSelectionError) []byte {
	if selectionErr == nil {
		return nil
	}
	message := strings.TrimSpace(selectionErr.Message)
	if message == "" {
		message = "No available compatible OpenAI accounts"
	}
	code := selectionErr.Code
	if code == "" {
		code = OpenAIRuntimeGuardErrorCodeUnsupportedOAuthCapability
	}
	category := strings.TrimSpace(selectionErr.Category)
	if category == "" {
		category = openAIRuntimeGuardCapabilityCategoryUnsupportedOAuthModel
	}
	payload, err := json.Marshal(map[string]any{
		"event_id": newOpenAIFastPolicyWSEventID(),
		"type":     "error",
		"error": map[string]any{
			"type":     "invalid_request_error",
			"code":     string(code),
			"category": category,
			"message":  message,
			"param":    "model",
		},
	})
	if err != nil {
		return []byte(`{"type":"error","error":{"type":"invalid_request_error","code":"unsupported_oauth_capability","category":"capability.unsupported_oauth_model_profile","message":"No available compatible OpenAI accounts","param":"model"}}`)
	}
	return payload
}

func OpenAIRuntimeGuardSelectionWSReason(selectionErr *OpenAIRuntimeGuardSelectionError) string {
	if selectionErr == nil {
		return "unsupported OpenAI OAuth capability"
	}
	if msg := strings.TrimSpace(selectionErr.Message); msg != "" {
		return msg
	}
	return "unsupported OpenAI OAuth capability"
}

func writeOpenAIRuntimeGuardSelectionWSEvent(ctx context.Context, conn *coderws.Conn, timeout time.Duration, selectionErr *OpenAIRuntimeGuardSelectionError) {
	if conn == nil || selectionErr == nil {
		return
	}
	eventBytes := BuildOpenAIRuntimeGuardSelectionWSEvent(selectionErr)
	if eventBytes == nil {
		return
	}
	writeCtx, cancel := context.WithTimeout(ctx, timeout)
	_ = conn.Write(writeCtx, coderws.MessageText, eventBytes)
	cancel()
}

func buildOpenAIRuntimeGuardBlockedWSEvent(blocked *OpenAIRuntimeGuardBlockedError) []byte {
	if blocked == nil {
		return nil
	}
	message := "Unsupported reasoning_effort value"
	if len(blocked.Payload) > 0 {
		if msg := strings.TrimSpace(gjson.GetBytes(blocked.Payload, "error.message").String()); msg != "" {
			message = msg
		}
	}
	payload, err := json.Marshal(map[string]any{
		"event_id": newOpenAIFastPolicyWSEventID(),
		"type":     "error",
		"error": map[string]any{
			"type":     "invalid_request_error",
			"code":     string(OpenAIRuntimeGuardErrorCodeLocalPolicyBlock),
			"category": openAIRuntimeGuardCapabilityCategoryLocalPolicyBlock,
			"message":  message,
			"param":    firstNonBlankString(blocked.Decision.Path, "reasoning_effort"),
		},
	})
	if err != nil {
		return []byte(`{"type":"error","error":{"type":"invalid_request_error","code":"local_policy_block","category":"capability.local_policy_block","message":"Unsupported reasoning_effort value","param":"reasoning_effort"}}`)
	}
	return payload
}

func OpenAIRuntimeGuardBlockedWSReason(blocked *OpenAIRuntimeGuardBlockedError) string {
	if blocked == nil {
		return "Unsupported reasoning_effort value"
	}
	if len(blocked.Payload) > 0 {
		if msg := strings.TrimSpace(gjson.GetBytes(blocked.Payload, "error.message").String()); msg != "" {
			return msg
		}
	}
	return "Unsupported reasoning_effort value"
}

func openAIRuntimeGuardBlockedWSReason(blocked *OpenAIRuntimeGuardBlockedError) string {
	return OpenAIRuntimeGuardBlockedWSReason(blocked)
}

func writeOpenAIRuntimeGuardBlockedWSEvent(ctx context.Context, conn *coderws.Conn, timeout time.Duration, blocked *OpenAIRuntimeGuardBlockedError) {
	if conn == nil || blocked == nil {
		return
	}
	eventBytes := buildOpenAIRuntimeGuardBlockedWSEvent(blocked)
	if eventBytes == nil {
		return
	}
	writeCtx, cancel := context.WithTimeout(ctx, timeout)
	_ = conn.Write(writeCtx, coderws.MessageText, eventBytes)
	cancel()
}

func (s *OpenAIGatewayService) WriteOpenAIRuntimeGuardBlockedWSEvent(ctx context.Context, conn *coderws.Conn, blocked *OpenAIRuntimeGuardBlockedError) {
	writeOpenAIRuntimeGuardBlockedWSEvent(ctx, conn, s.openAIWSWriteTimeout(), blocked)
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
	message := "Unsupported reasoning_effort value"
	category := openAIRuntimeGuardCapabilityCategoryLocalPolicyBlock
	runtimeGuardCategory := strings.TrimSpace(decision.Category)
	if strings.HasPrefix(decision.Category, "shape.") {
		message = "Invalid OpenAI Responses request shape"
	}
	if strings.HasPrefix(decision.Category, "context.") {
		message = "OpenAI request context is too large for the selected model"
	}
	if strings.HasPrefix(decision.Category, "content_safety.") {
		message = "Request blocked by local OpenAI OAuth content-safety guard"
	}
	if decision.Category == openAIRuntimeGuardCapabilityCategoryUnsupportedOAuthPersona {
		message = "Codex persona version is too old for this OpenAI OAuth model"
		category = decision.Category
	}
	payload, err := json.Marshal(map[string]any{
		"error": map[string]any{
			"type":                   "invalid_request_error",
			"code":                   string(OpenAIRuntimeGuardErrorCodeLocalPolicyBlock),
			"category":               category,
			"runtime_guard_category": runtimeGuardCategory,
			"message":                message,
			"param":                  param,
		},
	})
	if err != nil {
		return []byte(`{"error":{"type":"invalid_request_error","code":"local_policy_block","category":"capability.local_policy_block","message":"Unsupported reasoning_effort value","param":"reasoning_effort"}}`)
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
