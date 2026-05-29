package service

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	RiskEventKindVerifierFail             = "verifier_fail"
	RiskEventKindFallback                 = "fallback"
	RiskEventKindSignStripFallback        = "sign_strip_fallback"
	RiskEventKindProxyMismatch            = "proxy_mismatch"
	RiskEventKindRiskText                 = "risk_text"
	RiskEventKindSensitiveLeak            = "sensitive_leak"
	RiskEventKindControlPlaneUnsafeUpload = "control_plane_unsafe_upload"
	RiskEventKindControlPlaneModelPolicy  = "control_plane_model_policy"
	RiskEventKindIdentityBoundaryFail     = "identity_boundary_fail"

	RiskSeverityP0 = "p0"
	RiskSeverityP1 = "p1"
	RiskSeverityP2 = "p2"
)

type RiskEventLedgerInput struct {
	Kind               string
	Severity           string
	SessionRefOverride string
	UserRefOverride    string
	AccountRefOverride string
	RawSessionID       string
	RawUserID          string
	RawAccountID       string
	UnsafeRawReason    string
	ObservedAt         time.Time
	Recommendation     string
}

type RiskEventLedgerEntry struct {
	Kind                 string `json:"kind"`
	Severity             string `json:"severity"`
	SessionRef           string `json:"session_ref"`
	UserRef              string `json:"user_ref"`
	AccountRef           string `json:"account_ref"`
	SafeReason           string `json:"safe_reason"`
	Timestamp            string `json:"timestamp"`
	ActionRecommendation string `json:"action_recommendation"`
}

func BuildRiskEventLedgerEntry(input RiskEventLedgerInput) (RiskEventLedgerEntry, error) {
	observed := input.ObservedAt
	if observed.IsZero() {
		observed = time.Now().UTC()
	}
	entry := RiskEventLedgerEntry{
		Kind:                 sanitizeReasonCode(input.Kind),
		Severity:             normalizeRiskSeverity(input.Severity),
		SessionRef:           safeRefOrHMAC("", "session_budget_session", input.RawSessionID),
		UserRef:              safeRefOrHMAC("", "session_budget_user", input.RawUserID),
		AccountRef:           safeRefOrHMAC("", "session_budget_account", input.RawAccountID),
		SafeReason:           reasonBucket(input.UnsafeRawReason),
		Timestamp:            observed.UTC().Format(time.RFC3339),
		ActionRecommendation: sanitizeReasonCode(input.Recommendation),
	}
	if strings.TrimSpace(input.SessionRefOverride) != "" || strings.TrimSpace(input.UserRefOverride) != "" || strings.TrimSpace(input.AccountRefOverride) != "" {
		return RiskEventLedgerEntry{}, fmt.Errorf("ledger ref overrides are not accepted for risk events")
	}
	if entry.Kind == "" {
		entry.Kind = "unknown"
	}
	if entry.ActionRecommendation == "" {
		entry.ActionRecommendation = BudgetActionObserve
	}
	if err := validateLedgerRefs(entry.SessionRef, entry.UserRef, entry.AccountRef); err != nil {
		return RiskEventLedgerEntry{}, err
	}
	if err := ValidateNoRawSensitiveLedger(entry); err != nil {
		return RiskEventLedgerEntry{}, err
	}
	return entry, nil
}

func normalizeRiskSeverity(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case RiskSeverityP0:
		return RiskSeverityP0
	case RiskSeverityP1:
		return RiskSeverityP1
	case RiskSeverityP2:
		return RiskSeverityP2
	default:
		return RiskSeverityP2
	}
}

var ledgerTokenishRe = regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9._~+/=-]{6,}`)

func redactLedgerText(raw string) string {
	out := strings.TrimSpace(raw)
	out = ledgerEmailLikeRe.ReplaceAllString(out, "[redacted-email]")
	out = ledgerUUIDLikeRe.ReplaceAllString(out, "[redacted-uuid]")
	out = ledgerTokenishRe.ReplaceAllString(out, "$1[redacted-token]")
	out = ledgerPlainHashRe.ReplaceAllString(out, "[redacted-hash]")
	out = ledgerSensitiveKeyRe.ReplaceAllString(out, "[redacted-key]")
	return out
}

func reasonBucket(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "none"
	}
	lower := strings.ToLower(trimmed)
	switch {
	case strings.Contains(lower, "verifier"):
		return "reason_verifier"
	case strings.Contains(lower, "fallback"):
		return "reason_fallback"
	case strings.Contains(lower, "proxy"):
		return "reason_proxy"
	case strings.Contains(lower, "kyc") || strings.Contains(lower, "unusual") || strings.Contains(lower, "risk"):
		return "reason_risk_text"
	case strings.Contains(lower, "token") || strings.Contains(lower, "authorization") || strings.Contains(lower, "bearer") || strings.Contains(lower, "secret"):
		return "reason_sensitive"
	case strings.Contains(lower, "control") || strings.Contains(lower, "telemetry") || strings.Contains(lower, "upload"):
		return "reason_control_plane"
	case strings.Contains(lower, "identity") || strings.Contains(lower, "persona") || strings.Contains(lower, "cch"):
		return "reason_identity_boundary"
	default:
		return "reason_other"
	}
}
