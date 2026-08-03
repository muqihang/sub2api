package service

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	openAIRuntimeGuardContextDefaultLimitTokens = 1_000_000
	openAIRuntimeGuardContextReserveTokens      = 32_000
	openAIRuntimeGuardContextObviousMargin      = 0.20
	openAIRuntimeGuardContextMinMarginTokens    = 64_000
)

type openAIRuntimeGuardContextAction string

const (
	openAIRuntimeGuardContextActionPass   openAIRuntimeGuardContextAction = "pass"
	openAIRuntimeGuardContextActionShadow openAIRuntimeGuardContextAction = "shadow"
	openAIRuntimeGuardContextActionBlock  openAIRuntimeGuardContextAction = "block"
)

type openAIRuntimeGuardContextDecision struct {
	Action          openAIRuntimeGuardContextAction
	Category        string
	Metric          string
	EstimatedTokens int
	LimitTokens     int
	ReserveTokens   int
	Confidence      string
	KnownLimit      bool
	Blocked         bool
	Shadow          bool
}

func evaluateOpenAIRuntimeGuardContext(account *Account, model string, body []byte, compact bool) openAIRuntimeGuardContextDecision {
	if !shouldApplyOpenAIReasoningEffortGuard(account) || compact || len(body) == 0 || openAIRuntimeGuardContextDisabled(account) {
		return openAIRuntimeGuardContextDecision{}
	}
	limit, known := openAIRuntimeGuardContextLimitTokens(account, model)
	if !known || limit <= 0 {
		return openAIRuntimeGuardContextDecision{}
	}
	estimated := estimateOpenAIRuntimeGuardInputTokens(body)
	if estimated <= 0 {
		return openAIRuntimeGuardContextDecision{}
	}
	reserve := openAIRuntimeGuardContextReserveTokens
	withReserve := estimated + reserve
	margin := int(math.Round(float64(limit) * openAIRuntimeGuardContextObviousMargin))
	if margin < openAIRuntimeGuardContextMinMarginTokens {
		margin = openAIRuntimeGuardContextMinMarginTokens
	}
	obvious := withReserve > limit+margin
	if !obvious {
		return openAIRuntimeGuardContextDecision{
			Action:          openAIRuntimeGuardContextActionPass,
			Category:        "context.near_boundary_uncertain_pass",
			Metric:          "openai_runtime_guard.context.passed_uncertain",
			EstimatedTokens: estimated,
			LimitTokens:     limit,
			ReserveTokens:   reserve,
			Confidence:      "uncertain",
			KnownLimit:      true,
		}
	}
	decision := openAIRuntimeGuardContextDecision{
		Action:          openAIRuntimeGuardContextActionBlock,
		Category:        "context.obviously_over_limit",
		Metric:          "openai_runtime_guard.context.blocked",
		EstimatedTokens: estimated,
		LimitTokens:     limit,
		ReserveTokens:   reserve,
		Confidence:      "high",
		KnownLimit:      true,
		Blocked:         true,
	}
	if openAIRuntimeGuardContextShadowOnly(account) {
		decision.Action = openAIRuntimeGuardContextActionShadow
		decision.Metric = "openai_runtime_guard.context.shadow_blocked"
		decision.Blocked = false
		decision.Shadow = true
	}
	return decision
}

func openAIRuntimeGuardContextLimitTokens(account *Account, model string) (int, bool) {
	if account != nil {
		for _, key := range []string{"openai_context_limit_tokens", "context_limit_tokens", "openai_runtime_guard_context_limit_tokens"} {
			if limit, ok := parseOpenAIRuntimeGuardPositiveInt(account.GetExtraString(key)); ok {
				return limit, true
			}
		}
	}
	m := strings.ToLower(strings.TrimSpace(model))
	m = strings.TrimPrefix(m, "openai/")
	m = strings.TrimPrefix(m, "models/")
	switch {
	case m == "gpt-5.5" || strings.HasPrefix(m, "gpt-5.5-"),
		m == "gpt-5.4" || strings.HasPrefix(m, "gpt-5.4-"),
		m == "gpt-5.3-codex" || strings.HasPrefix(m, "gpt-5.3-codex-"),
		m == "gpt-5-codex" || strings.HasPrefix(m, "gpt-5-codex-"),
		m == "gpt-5.1-codex" || strings.HasPrefix(m, "gpt-5.1-codex-"),
		m == "gpt-5.1" || strings.HasPrefix(m, "gpt-5.1-"),
		m == "gpt-5.2" || strings.HasPrefix(m, "gpt-5.2-"),
		m == "gpt-5" || strings.HasPrefix(m, "gpt-5-"),
		m == "codex-auto-review" || strings.HasPrefix(m, "codex-auto-review-"):
		return openAIRuntimeGuardContextDefaultLimitTokens, true
	default:
		return 0, false
	}
}

func openAIRuntimeGuardContextMode(account *Account) string {
	if account == nil {
		return ""
	}
	mode := strings.ToLower(strings.TrimSpace(account.GetExtraString("openai_context_guard_mode")))
	if mode == "" {
		mode = strings.ToLower(strings.TrimSpace(account.GetExtraString("openai_runtime_guard_context_mode")))
	}
	return mode
}

func openAIRuntimeGuardContextShadowOnly(account *Account) bool {
	switch openAIRuntimeGuardContextMode(account) {
	case "shadow", "shadow_only", "dry_run":
		return true
	default:
		return false
	}
}

func openAIRuntimeGuardContextDisabled(account *Account) bool {
	switch openAIRuntimeGuardContextMode(account) {
	case "off", "disabled", "disable":
		return true
	default:
		return false
	}
}

func parseOpenAIRuntimeGuardPositiveInt(raw string) (int, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, false
	}
	return parsed, true
}

func estimateOpenAIRuntimeGuardInputTokens(body []byte) int {
	if len(body) == 0 || !json.Valid(body) {
		return 0
	}
	var decoded any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&decoded); err != nil {
		return 0
	}
	return estimateOpenAIRuntimeGuardJSONValueTokens(decoded, "")
}

func estimateOpenAIRuntimeGuardJSONValueTokens(value any, key string) int {
	switch typed := value.(type) {
	case string:
		return estimateOpenAIRuntimeGuardStringTokensForKey(key, typed)
	case []any:
		total := 2
		for _, item := range typed {
			total += estimateOpenAIRuntimeGuardJSONValueTokens(item, "") + 1
		}
		return total
	case map[string]any:
		total := 4
		partType := strings.TrimSpace(firstNonEmptyString(typed["type"]))
		if isOpenAIRuntimeGuardImageLikePart(partType, typed) {
			total += 1024
		}
		if isOpenAIRuntimeGuardFileLikePart(partType, typed) {
			total += 512
		}
		for childKey, childValue := range typed {
			total += estimateOpenAIRuntimeGuardTextTokens(childKey) + estimateOpenAIRuntimeGuardJSONValueTokens(childValue, childKey) + 1
		}
		return total
	case json.Number:
		return 1
	case bool, nil:
		return 1
	default:
		return 0
	}
}

func estimateOpenAIRuntimeGuardStringTokensForKey(key, value string) int {
	lowerKey := strings.ToLower(strings.TrimSpace(key))
	trimmed := strings.TrimSpace(value)
	if lowerKey == "image_url" || lowerKey == "file_id" || lowerKey == "filename" || lowerKey == "mime_type" || lowerKey == "media_type" {
		return 16
	}
	if (lowerKey == "data" || lowerKey == "image_data" || lowerKey == "file_data") && looksLikeOpenAIRuntimeGuardEncodedBlob(trimmed) {
		return 512
	}
	if strings.HasPrefix(trimmed, "data:image/") || strings.HasPrefix(trimmed, "data:application/") {
		return 512
	}
	return estimateOpenAIRuntimeGuardTextTokens(value)
}

func estimateOpenAIRuntimeGuardTextTokens(text string) int {
	if text == "" {
		return 0
	}
	return (len(text) + 3) / 4
}

func looksLikeOpenAIRuntimeGuardEncodedBlob(value string) bool {
	if strings.HasPrefix(value, "data:") {
		return true
	}
	if len(value) < 256 {
		return false
	}
	checked := 0
	for _, r := range value {
		if r == '=' || r == '+' || r == '/' || r == '-' || r == '_' || ('0' <= r && r <= '9') || ('A' <= r && r <= 'Z') || ('a' <= r && r <= 'z') {
			checked++
			continue
		}
		return false
	}
	return checked == len(value)
}

func isOpenAIRuntimeGuardImageLikePart(partType string, item map[string]any) bool {
	if strings.Contains(partType, "image") {
		return true
	}
	_, hasImageURL := item["image_url"]
	_, hasImageData := item["image_data"]
	return hasImageURL || hasImageData
}

func isOpenAIRuntimeGuardFileLikePart(partType string, item map[string]any) bool {
	if strings.Contains(partType, "file") {
		return true
	}
	_, hasFileID := item["file_id"]
	_, hasFileData := item["file_data"]
	_, hasFilename := item["filename"]
	return hasFileID || hasFileData || hasFilename
}

func openAIRuntimeGuardContextDecisionToReasoningDecision(decision openAIRuntimeGuardContextDecision) openAIReasoningEffortGuardDecision {
	if decision.Category == "" || decision.Metric == "" {
		return openAIReasoningEffortGuardDecision{}
	}
	action := string(decision.Action)
	if action == "" {
		action = "pass"
	}
	return openAIReasoningEffortGuardDecision{
		Action:          action,
		Blocked:         decision.Blocked,
		Present:         true,
		Status:          http.StatusRequestEntityTooLarge,
		Path:            "input",
		Category:        decision.Category,
		Metric:          decision.Metric,
		EstimatedTokens: decision.EstimatedTokens,
		LimitTokens:     decision.LimitTokens,
		ReserveTokens:   decision.ReserveTokens,
		Confidence:      decision.Confidence,
	}
}

func applyOpenAIRuntimeGuardContextToBody(account *Account, model string, body []byte, compact bool) *OpenAIRuntimeGuardBlockedError {
	decision := evaluateOpenAIRuntimeGuardContext(account, model, body, compact)
	if !decision.Blocked {
		return nil
	}
	return newOpenAIRuntimeGuardBlockedError(openAIRuntimeGuardContextDecisionToReasoningDecision(decision))
}

func setOpenAIRuntimeGuardContextMetadata(c *gin.Context, decision openAIRuntimeGuardContextDecision) {
	if c == nil || decision.Category == "" || decision.Metric == "" {
		return
	}
	c.Set(OpenAIRuntimeGuardMetadataKey, OpenAIRuntimeGuardMetadata{
		Action:          string(decision.Action),
		Category:        decision.Category,
		Metric:          decision.Metric,
		Field:           "input",
		Path:            "input",
		Status:          http.StatusRequestEntityTooLarge,
		EstimatedTokens: decision.EstimatedTokens,
		LimitTokens:     decision.LimitTokens,
		ReserveTokens:   decision.ReserveTokens,
		Confidence:      decision.Confidence,
	})
}

func applyOpenAIRuntimeGuardContextToHTTP(c *gin.Context, account *Account, model string, body []byte, compact bool) *OpenAIRuntimeGuardBlockedError {
	decision := evaluateOpenAIRuntimeGuardContext(account, model, body, compact)
	if decision.Category == "" {
		return nil
	}
	if !decision.Blocked {
		if decision.Shadow {
			setOpenAIRuntimeGuardContextMetadata(c, decision)
		}
		return nil
	}
	setOpenAIRuntimeGuardContextMetadata(c, decision)
	blocked := newOpenAIRuntimeGuardBlockedError(openAIRuntimeGuardContextDecisionToReasoningDecision(decision))
	MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalPolicyDenied)
	if c != nil {
		c.Data(blocked.StatusCode, "application/json; charset=utf-8", blocked.Payload)
	}
	return blocked
}
