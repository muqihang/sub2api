package service

import (
	"context"
	"net"
	"strings"
	"time"
)

const (
	RiskEventKindFormalPoolEgressVerified         = "formal_pool_egress_verified"
	RiskEventKindFormalPoolEgressMismatch         = "formal_pool_egress_mismatch"
	RiskEventKindFormalPoolNonceExpired           = "formal_pool_nonce_expired"
	RiskEventKindFormalPoolEgressNoProxy          = "formal_pool_egress_no_proxy"
	RiskEventKindFormalPoolPublicRouteRateLimited = "formal_pool_public_route_rate_limited"
)

type FormalPoolRiskEventWriter interface {
	RecordEgressVerified(ctx context.Context, input FormalPoolRiskEventInput) error
	RecordEgressMismatch(ctx context.Context, input FormalPoolRiskEventInput) error
	RecordNonceExpired(ctx context.Context, input FormalPoolRiskEventInput) error
	RecordEgressNoProxy(ctx context.Context, input FormalPoolRiskEventInput) error
	RecordPublicRouteRateLimited(ctx context.Context, input FormalPoolRiskEventInput) error
	RecordPublicRouteRateLimitedBuckets(ctx context.Context, nonceBucket, ipBucket, reason string) error
}

type FormalPoolRiskEventInput struct {
	RawSessionID       string
	RawUserID          string
	RawAccountID       string
	UnsafeRawReason    string
	SafeReasonCode     string
	SafeContextBuckets []string
	NonceBucket        string
	IPBucket           string
	ObservedAt         time.Time
}

type sessionBudgetFormalPoolRiskEventWriter struct {
	sink SessionBudgetObserveSink
}

func NewFormalPoolRiskEventWriter(sink SessionBudgetObserveSink) FormalPoolRiskEventWriter {
	return &sessionBudgetFormalPoolRiskEventWriter{sink: sink}
}

func (w *sessionBudgetFormalPoolRiskEventWriter) RecordEgressVerified(ctx context.Context, input FormalPoolRiskEventInput) error {
	return w.record(ctx, RiskEventKindFormalPoolEgressVerified, RiskSeverityP2, BudgetActionObserve, input, false)
}

func (w *sessionBudgetFormalPoolRiskEventWriter) RecordEgressMismatch(ctx context.Context, input FormalPoolRiskEventInput) error {
	return w.record(ctx, RiskEventKindFormalPoolEgressMismatch, RiskSeverityP1, BudgetActionQuarantine, input, false)
}

func (w *sessionBudgetFormalPoolRiskEventWriter) RecordNonceExpired(ctx context.Context, input FormalPoolRiskEventInput) error {
	return w.record(ctx, RiskEventKindFormalPoolNonceExpired, RiskSeverityP2, BudgetActionObserve, input, false)
}

func (w *sessionBudgetFormalPoolRiskEventWriter) RecordEgressNoProxy(ctx context.Context, input FormalPoolRiskEventInput) error {
	return w.record(ctx, RiskEventKindFormalPoolEgressNoProxy, RiskSeverityP1, BudgetActionCooldown, input, false)
}

func (w *sessionBudgetFormalPoolRiskEventWriter) RecordPublicRouteRateLimited(ctx context.Context, input FormalPoolRiskEventInput) error {
	orphan := strings.TrimSpace(input.RawSessionID) == "" && strings.TrimSpace(input.RawUserID) == "" && strings.TrimSpace(input.RawAccountID) == ""
	return w.record(ctx, RiskEventKindFormalPoolPublicRouteRateLimited, RiskSeverityP2, BudgetActionObserve, input, orphan)
}

func (w *sessionBudgetFormalPoolRiskEventWriter) RecordPublicRouteRateLimitedBuckets(ctx context.Context, nonceBucket, ipBucket, reason string) error {
	return w.RecordPublicRouteRateLimited(ctx, FormalPoolRiskEventInput{
		SafeReasonCode: reason,
		NonceBucket:    nonceBucket,
		IPBucket:       ipBucket,
	})
}

func (w *sessionBudgetFormalPoolRiskEventWriter) record(ctx context.Context, kind, severity, recommendation string, input FormalPoolRiskEventInput, orphan bool) error {
	if w == nil || w.sink == nil {
		return nil
	}
	entry, err := buildFormalPoolRiskEvent(kind, severity, recommendation, input, orphan)
	if err != nil {
		return err
	}
	record := SessionBudgetObserveRecord{RiskEvents: []RiskEventLedgerEntry{entry}}
	if err := ValidateNoRawSensitiveLedger(record); err != nil {
		return err
	}
	w.sink.ObserveSessionBudget(ctx, record)
	return nil
}

func buildFormalPoolRiskEvent(kind, severity, recommendation string, input FormalPoolRiskEventInput, orphan bool) (RiskEventLedgerEntry, error) {
	observed := input.ObservedAt
	if observed.IsZero() {
		observed = time.Now().UTC()
	}
	rawSessionID := input.RawSessionID
	rawUserID := input.RawUserID
	rawAccountID := input.RawAccountID
	if orphan {
		rawSessionID = "formal_pool_orphan_session"
		rawUserID = "formal_pool_orphan_user"
		rawAccountID = "formal_pool_orphan_account"
	}
	entry := RiskEventLedgerEntry{
		Kind:                 sanitizeReasonCode(kind),
		Severity:             normalizeRiskSeverity(severity),
		SessionRef:           safeRefOrHMAC("", "session_budget_session", rawSessionID),
		UserRef:              safeRefOrHMAC("", "session_budget_user", rawUserID),
		AccountRef:           safeRefOrHMAC("", "session_budget_account", rawAccountID),
		SafeReason:           formalPoolSafeReason(input, orphan, kind == RiskEventKindFormalPoolPublicRouteRateLimited),
		Timestamp:            observed.UTC().Format(time.RFC3339),
		ActionRecommendation: sanitizeReasonCode(recommendation),
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

func formalPoolSafeReason(input FormalPoolRiskEventInput, orphan bool, publicRoute bool) string {
	base := formalPoolReasonCode(input.SafeReasonCode, input.UnsafeRawReason, publicRoute)
	parts := []string{base}
	if orphan {
		parts = append(parts, "orphan")
	}
	parts = appendFormalPoolSafeBucket(parts, input.NonceBucket)
	parts = appendFormalPoolSafeBucket(parts, input.IPBucket)
	for _, bucket := range input.SafeContextBuckets {
		parts = appendFormalPoolSafeBucket(parts, bucket)
	}
	return strings.Join(parts, ":")
}

func formalPoolReasonCode(safeReasonCode, unsafeRawReason string, publicRoute bool) string {
	code := sanitizeReasonCode(safeReasonCode)
	if publicRoute {
		if formalPoolPublicRouteReasonAllowed(code) && !formalPoolLooksRawIdentifier(safeReasonCode) && !formalPoolLooksRawIdentifier(code) {
			return code
		}
		return "rate_limited"
	}
	if code != "" && formalPoolSafeReasonCodeAllowed(code) && !formalPoolLooksRawIdentifier(safeReasonCode) && !formalPoolLooksRawIdentifier(code) {
		return code
	}
	if strings.TrimSpace(unsafeRawReason) != "" {
		return reasonBucket(unsafeRawReason)
	}
	return "reason_other"
}

func formalPoolPublicRouteReasonAllowed(code string) bool {
	switch code {
	case "per_nonce", "per_ip", "nonce_total", "redis_unavailable_fallback", "handler_unavailable", "rate_limited", "unknown":
		return true
	default:
		return false
	}
}

func formalPoolSafeReasonCodeAllowed(code string) bool {
	if code == "" {
		return false
	}
	if formalPoolPublicRouteReasonAllowed(code) || formalPoolSafeBucketAllowed(code) {
		return true
	}
	switch code {
	case "egress_verified", "egress_mismatch", "egress_no_proxy", "network_identifier_filter",
		"refresh_token_invalid", "refresh_failed", "reason_verifier", "reason_fallback", "reason_proxy",
		"reason_risk_text", "reason_sensitive", "reason_control_plane", "reason_identity_boundary", "reason_other",
		RiskEventKindFormalPoolEgressVerified, RiskEventKindFormalPoolEgressMismatch,
		RiskEventKindFormalPoolNonceExpired, RiskEventKindFormalPoolEgressNoProxy,
		RiskEventKindFormalPoolPublicRouteRateLimited:
		return true
	default:
		return false
	}
}

func formalPoolSafeBucketAllowed(code string) bool {
	if code == "per_nonce" || code == "nonce_expired" {
		return true
	}
	for _, prefix := range []string{"nonce_bucket_", "ip_bucket_", "browser_bucket_", "proxy_bucket_"} {
		if strings.HasPrefix(code, prefix) {
			return formalPoolHexBucketSuffixAllowed(strings.TrimPrefix(code, prefix))
		}
	}
	return false
}

func formalPoolHexBucketSuffixAllowed(suffix string) bool {
	if len(suffix) < 8 || len(suffix) > 32 {
		return false
	}
	for _, r := range suffix {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

func appendFormalPoolSafeBucket(parts []string, bucket string) []string {
	if formalPoolLooksRawIdentifier(bucket) {
		return parts
	}
	if safe := sanitizeReasonCode(bucket); safe != "" && formalPoolSafeBucketAllowed(safe) && !formalPoolLooksRawIdentifier(safe) {
		parts = append(parts, safe)
	}
	return parts
}

func formalPoolLooksRawIdentifier(value string) bool {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	if lower == "" {
		return false
	}
	if ip := net.ParseIP(strings.Trim(trimmed, "[]")); ip != nil {
		return true
	}
	if _, _, err := net.ParseCIDR(trimmed); err == nil {
		return true
	}
	if _, _, err := net.SplitHostPort(trimmed); err == nil {
		return true
	}
	if ledgerEmailLikeRe.MatchString(lower) || ledgerUUIDLikeRe.MatchString(lower) || ledgerBearerRe.MatchString(lower) || ledgerTokenishRe.MatchString(lower) || ledgerSensitiveKeyRe.MatchString(lower) {
		return true
	}
	if strings.Contains(lower, ".") || strings.Contains(lower, "://") || strings.Contains(lower, "@") {
		return true
	}
	if strings.Contains(lower, "nonce") && lower != "per_nonce" && lower != "nonce_total" && lower != "nonce_expired" && !strings.HasPrefix(lower, "nonce_bucket_") {
		return true
	}
	if strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "credential") || strings.Contains(lower, "password") {
		return true
	}
	return false
}

var _ FormalPoolRiskEventWriter = (*sessionBudgetFormalPoolRiskEventWriter)(nil)
