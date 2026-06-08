package service

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	PoolProfileNormal     = "normal"
	PoolProfileAggressive = "aggressive"
)

type AccountBudgetLedgerInput struct {
	Account              *Account
	ResponseHeaders      http.Header
	SchedulingWeight     float64
	LastRiskEventSummary string
}

type AccountBudgetLedgerEntry struct {
	AccountRef                    string  `json:"account_ref"`
	ProxyRef                      string  `json:"proxy_ref"`
	PoolProfile                   string  `json:"pool_profile"`
	Utilization5hStatus           string  `json:"utilization_5h_status"`
	Utilization5hPercentageBucket string  `json:"utilization_5h_percentage_bucket"`
	Utilization5hResetBucket      string  `json:"utilization_5h_reset_bucket"`
	Utilization7dStatus           string  `json:"utilization_7d_status"`
	Utilization7dPercentageBucket string  `json:"utilization_7d_percentage_bucket"`
	Utilization7dResetBucket      string  `json:"utilization_7d_reset_bucket"`
	OverageStatus                 string  `json:"overage_status"`
	CooldownState                 string  `json:"cooldown_state"`
	QuarantineState               string  `json:"quarantine_state"`
	LastRiskEventSummary          string  `json:"last_risk_event_summary"`
	SchedulingWeight              float64 `json:"scheduling_weight"`
}

func BuildAccountBudgetLedgerEntry(input AccountBudgetLedgerInput) (AccountBudgetLedgerEntry, error) {
	account := input.Account
	entry := AccountBudgetLedgerEntry{
		AccountRef:                    safeAccountRef(account),
		ProxyRef:                      safeProxyRef(account),
		PoolProfile:                   normalizePoolProfile(accountPoolProfile(account)),
		Utilization5hStatus:           safeHeaderEnum(input.ResponseHeaders.Get("anthropic-ratelimit-unified-5h-status")),
		Utilization5hPercentageBucket: percentageBucket(parseUtilization(input.ResponseHeaders.Get("anthropic-ratelimit-unified-5h-utilization"))),
		Utilization5hResetBucket:      resetBucket(input.ResponseHeaders.Get("anthropic-ratelimit-unified-5h-reset")),
		Utilization7dStatus:           safeHeaderEnum(input.ResponseHeaders.Get("anthropic-ratelimit-unified-7d-status")),
		Utilization7dPercentageBucket: percentageBucket(parseUtilization(input.ResponseHeaders.Get("anthropic-ratelimit-unified-7d-utilization"))),
		Utilization7dResetBucket:      resetBucket(input.ResponseHeaders.Get("anthropic-ratelimit-unified-7d-reset")),
		OverageStatus:                 safeHeaderEnum(input.ResponseHeaders.Get("anthropic-ratelimit-unified-overage-status")),
		CooldownState:                 cooldownState(account),
		QuarantineState:               quarantineState(account),
		LastRiskEventSummary:          reasonBucket(input.LastRiskEventSummary),
		SchedulingWeight:              input.SchedulingWeight,
	}
	if entry.SchedulingWeight == 0 {
		entry.SchedulingWeight = 1
	}
	if err := validateLedgerRefs(entry.AccountRef, entry.ProxyRef); err != nil {
		return AccountBudgetLedgerEntry{}, err
	}
	if err := ValidateNoRawSensitiveLedger(entry); err != nil {
		return AccountBudgetLedgerEntry{}, err
	}
	return entry, nil
}

func safeAccountRef(a *Account) string {
	if a == nil {
		return scopedStickyHMAC("session_budget_account", "unknown")
	}
	if ref := strings.TrimSpace(a.GetExtraString(ccGatewayExtraAccountRef)); ref != "" && isSafeLedgerRef(ref) {
		return ref
	}
	return scopedStickyHMAC("session_budget_account", strconv.FormatInt(a.ID, 10))
}

func safeProxyRef(a *Account) string {
	if a == nil || a.ProxyID == nil {
		return scopedStickyHMAC("session_budget_proxy", "none")
	}
	return scopedStickyHMAC("session_budget_proxy", strconv.FormatInt(*a.ProxyID, 10))
}

func accountPoolProfile(a *Account) string {
	if a == nil {
		return PoolProfileNormal
	}
	return a.GetExtraString("pool_profile")
}

func normalizePoolProfile(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case PoolProfileAggressive:
		return PoolProfileAggressive
	default:
		return PoolProfileNormal
	}
}

func safeHeaderEnum(v string) string {
	if strings.TrimSpace(v) == "" {
		return "unknown"
	}
	return sanitizeReasonCode(v)
}

func parseUtilization(raw string) (float64, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, false
	}
	percent := strings.HasSuffix(s, "%")
	s = strings.TrimSuffix(s, "%")
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	// Bare values are ratios (0.42 or 1.02). Only explicit percentages use percent semantics.
	if percent {
		v = v / 100
	}
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	return v, true
}

func percentageBucket(v float64, ok bool) string {
	if !ok {
		return "unknown"
	}
	pct := int(v*100 + 0.000001)
	switch {
	case pct < 10:
		return "pct_0_10"
	case pct < 20:
		return "pct_10_20"
	case pct < 30:
		return "pct_20_30"
	case pct < 40:
		return "pct_30_40"
	case pct < 50:
		return "pct_40_50"
	case pct < 60:
		return "pct_50_60"
	case pct < 70:
		return "pct_60_70"
	case pct < 80:
		return "pct_70_80"
	case pct < 90:
		return "pct_80_90"
	case pct < 95:
		return "pct_90_95"
	case pct < 100:
		return "pct_95_100"
	default:
		return "pct_100"
	}
}

func resetBucket(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "unknown"
	}
	if ts, ok := parseAnthropicUnifiedReset(raw); ok {
		d := time.Until(ts)
		switch {
		case d <= 0:
			return "past"
		case d <= time.Hour:
			return "le_1h"
		case d <= 5*time.Hour:
			return "le_5h"
		case d <= 24*time.Hour:
			return "le_24h"
		default:
			return "gt_24h"
		}
	}
	return "malformed"
}

func parseAnthropicUnifiedReset(raw string) (time.Time, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, false
	}
	if ts, err := strconv.ParseInt(s, 10, 64); err == nil {
		if ts > 1e11 {
			ts = ts / 1000
		}
		return time.Unix(ts, 0), true
	}
	if ts, err := time.Parse(time.RFC3339, s); err == nil {
		return ts, true
	}
	return time.Time{}, false
}

func cooldownState(a *Account) string {
	if a == nil {
		return "unknown"
	}
	if a.IsRateLimited() {
		return "cooldown_active"
	}
	if a.TempUnschedulableUntil != nil && time.Now().Before(*a.TempUnschedulableUntil) {
		return "temp_unschedulable"
	}
	return "none"
}

func quarantineState(a *Account) string {
	if a == nil {
		return "unknown"
	}
	if !a.IsActive() {
		return "inactive_or_quarantined"
	}
	return "none"
}
