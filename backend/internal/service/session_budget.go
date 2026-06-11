package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

const (
	BudgetModeObserveOnly = "observe_only"

	BudgetActionObserve    = "observe"
	BudgetActionP0Block    = "p0_block"
	BudgetActionQuarantine = "quarantine"
	BudgetActionCooldown   = "cooldown_recommendation"

	BudgetVerifierPass    = "pass"
	BudgetVerifierFail    = "fail"
	BudgetVerifierUnknown = "unknown"

	BudgetFallbackFalse   = "false"
	BudgetFallbackTrue    = "true"
	BudgetFallbackUnknown = "unknown"
)

type BudgetDecision struct {
	Mode          string         `json:"mode"`
	Action        string         `json:"action"`
	AccountWeight float64        `json:"account_weight"`
	QueuePriority int            `json:"queue_priority"`
	ReasonCode    string         `json:"reason_code"`
	SafeSummary   map[string]any `json:"safe_summary,omitempty"`
}

type SessionBudgetLedgerInput struct {
	RawSessionID       string
	RawUserID          string
	RawAccountID       string
	SessionRefOverride string
	UserRefOverride    string
	AccountRefOverride string
	RequestBody        []byte
	RequestIsStream    bool
	VerifierResult     string
	FallbackResult     string
	StatusBucket       string
	RiskFlags          []string
}

type SessionBudgetLedgerEntry struct {
	SessionRef                    string   `json:"session_ref"`
	UserRef                       string   `json:"user_ref"`
	AccountRef                    string   `json:"account_ref"`
	ModelFamily                   string   `json:"model_family"`
	ModelNameBucket               string   `json:"model_name_bucket"`
	MessageCount                  int      `json:"message_count"`
	ToolUseCount                  int      `json:"tool_use_count"`
	ToolResultCount               int      `json:"tool_result_count"`
	ToolDefinitionCount           int      `json:"tool_definition_count"`
	ThinkingPresent               bool     `json:"thinking_present"`
	ThinkingBudgetBucket          string   `json:"thinking_budget_bucket"`
	Stream                        bool     `json:"stream"`
	BodySizeBucket                string   `json:"body_size_bucket"`
	MaxTokensBucket               string   `json:"max_tokens_bucket"`
	Context1MPresent              bool     `json:"context_1m_present"`
	OutputConfigShapeSummary      string   `json:"output_config_shape_summary"`
	ContextManagementShapeSummary string   `json:"context_management_shape_summary"`
	StatusBucket                  string   `json:"status_bucket"`
	VerifierResult                string   `json:"verifier_result"`
	FallbackResult                string   `json:"fallback_result"`
	RiskFlags                     []string `json:"risk_flags,omitempty"`
	ObservedAt                    string   `json:"observed_at"`
}

func BuildSessionBudgetLedgerEntry(input SessionBudgetLedgerInput) (SessionBudgetLedgerEntry, BudgetDecision, error) {
	entry := SessionBudgetLedgerEntry{
		SessionRef:                    safeRefOrHMAC("", "session_budget_session", input.RawSessionID),
		UserRef:                       safeRefOrHMAC("", "session_budget_user", input.RawUserID),
		AccountRef:                    safeRefOrHMAC("", "session_budget_account", input.RawAccountID),
		BodySizeBucket:                safeLengthBucket(len(input.RequestBody)),
		StatusBucket:                  normalizeStatusBucket(input.StatusBucket),
		VerifierResult:                normalizeVerifierResult(input.VerifierResult),
		FallbackResult:                normalizeFallbackResult(input.FallbackResult),
		RiskFlags:                     safeIndicatorCodes(input.RiskFlags),
		ObservedAt:                    time.Now().UTC().Format(time.RFC3339),
		ThinkingBudgetBucket:          "absent",
		MaxTokensBucket:               "absent",
		OutputConfigShapeSummary:      "absent",
		ContextManagementShapeSummary: "absent",
	}
	if strings.TrimSpace(input.SessionRefOverride) != "" || strings.TrimSpace(input.UserRefOverride) != "" || strings.TrimSpace(input.AccountRefOverride) != "" {
		return SessionBudgetLedgerEntry{}, BudgetDecision{}, fmt.Errorf("ledger ref overrides are not accepted for session entries")
	}
	if err := validateLedgerRefs(entry.SessionRef, entry.UserRef, entry.AccountRef); err != nil {
		return SessionBudgetLedgerEntry{}, BudgetDecision{}, err
	}

	body := input.RequestBody
	model := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	entry.ModelFamily = modelFamilyBucket(model)
	entry.ModelNameBucket = modelNameBucket(model)
	entry.MessageCount = countArray(body, "messages")
	entry.ToolDefinitionCount = countArray(body, "tools")
	entry.ToolUseCount, entry.ToolResultCount = countToolBlocks(body)
	entry.ThinkingPresent = gjson.GetBytes(body, "thinking").Exists()
	if entry.ThinkingPresent {
		entry.ThinkingBudgetBucket = tokenBucket(gjson.GetBytes(body, "thinking.budget_tokens").Int(), true)
	}
	entry.Stream = input.RequestIsStream || gjson.GetBytes(body, "stream").Bool()
	entry.MaxTokensBucket = tokenBucket(gjson.GetBytes(body, "max_tokens").Int(), gjson.GetBytes(body, "max_tokens").Exists())
	entry.Context1MPresent = requestHasContext1M(body)
	entry.OutputConfigShapeSummary = shapeSummary(gjson.GetBytes(body, "output_config"))
	entry.ContextManagementShapeSummary = shapeSummary(gjson.GetBytes(body, "context_management"))

	if err := ValidateNoRawSensitiveLedger(entry); err != nil {
		return SessionBudgetLedgerEntry{}, BudgetDecision{}, err
	}
	decision := BuildBudgetDecision(BudgetDecisionInput{
		Session:                   entry,
		UtilizationHeadersPresent: false,
	})
	decision.SafeSummary["model_family"] = entry.ModelFamily
	decision.SafeSummary["body_size_bucket"] = entry.BodySizeBucket
	decision.SafeSummary["max_tokens_bucket"] = entry.MaxTokensBucket
	if err := ValidateNoRawSensitiveLedger(decision); err != nil {
		return SessionBudgetLedgerEntry{}, BudgetDecision{}, err
	}
	return entry, decision, nil
}

func countArray(body []byte, path string) int {
	v := gjson.GetBytes(body, path)
	if !v.IsArray() {
		return 0
	}
	return len(v.Array())
}

func countToolBlocks(body []byte) (toolUse int, toolResult int) {
	msgs := gjson.GetBytes(body, "messages")
	if !msgs.IsArray() {
		return 0, 0
	}
	msgs.ForEach(func(_, msg gjson.Result) bool {
		content := msg.Get("content")
		if content.IsArray() {
			content.ForEach(func(_, block gjson.Result) bool {
				switch strings.TrimSpace(block.Get("type").String()) {
				case "tool_use":
					toolUse++
				case "tool_result":
					toolResult++
				}
				return true
			})
		}
		return true
	})
	return toolUse, toolResult
}

func requestHasContext1M(body []byte) bool {
	beta := strings.ToLower(gjson.GetBytes(body, "metadata.beta").String() + " " + gjson.GetBytes(body, "anthropic_beta").String())
	if strings.Contains(beta, "1m") || strings.Contains(beta, "context-1m") || strings.Contains(beta, "context_1m") {
		return true
	}
	cm := gjson.GetBytes(body, "context_management")
	return cm.Exists()
}

func shapeSummary(v gjson.Result) string {
	if !v.Exists() || v.Type == gjson.Null {
		return "absent"
	}
	if !v.IsObject() {
		if v.IsArray() {
			return "array"
		}
		return strings.ToLower(v.Type.String())
	}
	keys := make([]string, 0)
	v.ForEach(func(k, _ gjson.Result) bool {
		keys = append(keys, sanitizeReasonCode(k.String()))
		return true
	})
	sort.Strings(keys)
	if len(keys) > 8 {
		keys = keys[:8]
	}
	return "object_keys:" + strings.Join(keys, ",")
}

func modelFamilyBucket(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.Contains(m, "opus"):
		return "opus"
	case strings.Contains(m, "sonnet"):
		return "sonnet"
	case strings.Contains(m, "haiku"):
		return "haiku"
	case m == "":
		return "unknown"
	default:
		return "other"
	}
}

func modelNameBucket(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	allow := map[string]bool{
		"claude-fable-5":           true,
		"claude-sonnet-4-6":        true,
		"claude-opus-4-6":          true,
		"claude-opus-4-6-thinking": true,
		"claude-opus-4-7":          true,
		"claude-opus-4-7-thinking": true,
	}
	if allow[m] {
		return m
	}
	for allowed := range allow {
		if strings.HasPrefix(m, allowed) {
			return allowed + ":versioned"
		}
	}
	return modelFamilyBucket(model) + ":bucket"
}

func tokenBucket(n int64, present bool) string {
	if !present {
		return "absent"
	}
	switch {
	case n <= 0:
		return "le_0"
	case n == 32000:
		return "eq_32000"
	case n <= 1024:
		return "le_1k"
	case n <= 4096:
		return "le_4k"
	case n <= 8192:
		return "le_8k"
	case n <= 16000:
		return "le_16k"
	case n <= 32000:
		return "le_32k"
	default:
		return "gt_32k"
	}
}

func safeRefOrHMAC(override, scope, raw string) string {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override)
	}
	if strings.TrimSpace(raw) == "" {
		return scopedStickyHMAC(scope, "anonymous")
	}
	return scopedStickyHMAC(scope, raw)
}

var (
	ledgerUUIDLikeRe         = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	ledgerEmailLikeRe        = regexp.MustCompile(`(?i)\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b`)
	ledgerPlainHashRe        = regexp.MustCompile(`(?i)\b[0-9a-f]{32,128}\b`)
	ledgerBearerRe           = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]{10,}`)
	ledgerSensitiveKeyRe     = regexp.MustCompile(`(?i)(authorization|access_token|refresh_token|id_token|x-api-key|cookie|cch|proxy_credential|password|client_secret)`)
	ledgerGeneratedHMACRefRe = regexp.MustCompile(`^hmac-sha256:[0-9a-f]{64}$`)
)

func validateLedgerRefs(refs ...string) error {
	for _, ref := range refs {
		if strings.TrimSpace(ref) == "" {
			return fmt.Errorf("ledger ref is required")
		}
		if !isSafeLedgerRef(ref) {
			return fmt.Errorf("unsafe ledger ref")
		}
	}
	return nil
}

func isSafeLedgerRef(ref string) bool {
	ref = strings.TrimSpace(ref)
	if ledgerGeneratedHMACRefRe.MatchString(ref) {
		return true
	}
	if strings.HasPrefix(ref, "opaque:") || strings.HasPrefix(ref, "scoped:") {
		return !ledgerUUIDLikeRe.MatchString(ref) && !ledgerEmailLikeRe.MatchString(ref) && !ledgerSensitiveKeyRe.MatchString(ref)
	}
	return false
}

func ValidateNoRawSensitiveLedger(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	text := string(b)
	if ledgerEmailLikeRe.MatchString(text) {
		return fmt.Errorf("ledger contains raw email")
	}
	if ledgerUUIDLikeRe.MatchString(text) {
		return fmt.Errorf("ledger contains raw uuid")
	}
	if ledgerBearerRe.MatchString(text) {
		return fmt.Errorf("ledger contains raw bearer token")
	}
	if strings.Contains(strings.ToLower(text), "authorization") || strings.Contains(strings.ToLower(text), "x-api-key") {
		return fmt.Errorf("ledger contains auth field")
	}
	plain := ledgerPlainHashRe.FindAllString(text, -1)
	for _, h := range plain {
		idx := strings.Index(text, h)
		prefix := ""
		if idx >= len("hmac-sha256:") {
			prefix = text[idx-len("hmac-sha256:") : idx]
		}
		if prefix != "hmac-sha256:" {
			return fmt.Errorf("ledger contains plain hash")
		}
	}
	return nil
}

func normalizeStatusBucket(status string) string {
	s := strings.TrimSpace(strings.ToLower(status))
	if s == "" {
		return "unknown"
	}
	return sanitizeReasonCode(s)
}

func normalizeVerifierResult(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case BudgetVerifierPass:
		return BudgetVerifierPass
	case BudgetVerifierFail:
		return BudgetVerifierFail
	default:
		return BudgetVerifierUnknown
	}
}

func normalizeFallbackResult(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case BudgetFallbackFalse, "0", "no":
		return BudgetFallbackFalse
	case BudgetFallbackTrue, "1", "yes":
		return BudgetFallbackTrue
	default:
		return BudgetFallbackUnknown
	}
}

func sanitizeReasonCodes(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if s := sanitizeReasonCode(v); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func sanitizeReasonCode(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == ':' {
			b.WriteRune(r)
		} else if r == ' ' || r == '.' || r == '/' {
			b.WriteRune('_')
		}
	}
	out := b.String()
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

func safeIndicatorCodes(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, raw := range in {
		code := safeIndicatorCode(raw)
		if code == "" {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}
	return out
}

func safeIndicatorCode(raw string) string {
	candidate := sanitizeReasonCode(raw)
	switch candidate {
	case "retry_storm", "tool_loop", "verifier_fail", "fallback", "sign_strip_fallback", "proxy_mismatch", "risk_text", "sensitive_leak", "control_plane_unsafe_upload", "identity_boundary_fail", "cooldown", "quarantine":
		return candidate
	}
	return reasonBucket(raw)
}
