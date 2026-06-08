package service

import (
	"fmt"
	"strings"
)

type UserBudgetLedgerInput struct {
	RawUserID          string
	UserRefOverride    string
	ActiveSessionCount int
	UsageShare         float64
	RiskScore          float64
	AbuseIndicators    []string
}

type UserBudgetLedgerEntry struct {
	UserRef                  string   `json:"user_ref"`
	ActiveSessionCountBucket string   `json:"active_session_count_bucket"`
	UsageShareBucket         string   `json:"usage_share_bucket"`
	RiskScoreBucket          string   `json:"risk_score_bucket"`
	AbuseIndicators          []string `json:"abuse_indicators,omitempty"`
}

func BuildUserBudgetLedgerEntry(input UserBudgetLedgerInput) (UserBudgetLedgerEntry, error) {
	if strings.TrimSpace(input.UserRefOverride) != "" {
		return UserBudgetLedgerEntry{}, fmt.Errorf("ledger ref overrides are not accepted for user entries")
	}
	entry := UserBudgetLedgerEntry{
		UserRef:                  safeRefOrHMAC("", "session_budget_user", input.RawUserID),
		ActiveSessionCountBucket: countBucket(input.ActiveSessionCount),
		UsageShareBucket:         percentageBucket(budgetClamp01(input.UsageShare), true),
		RiskScoreBucket:          riskScoreBucket(input.RiskScore),
		AbuseIndicators:          safeIndicatorCodes(input.AbuseIndicators),
	}
	if err := validateLedgerRefs(entry.UserRef); err != nil {
		return UserBudgetLedgerEntry{}, err
	}
	if err := ValidateNoRawSensitiveLedger(entry); err != nil {
		return UserBudgetLedgerEntry{}, err
	}
	return entry, nil
}

func countBucket(n int) string {
	switch {
	case n <= 0:
		return "count_0"
	case n <= 1:
		return "count_1"
	case n <= 5:
		return "count_2_5"
	case n <= 10:
		return "count_6_10"
	case n <= 20:
		return "count_11_20"
	case n <= 50:
		return "count_20_50"
	default:
		return "count_gt_50"
	}
}

func budgetClamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func riskScoreBucket(v float64) string {
	v = budgetClamp01(v)
	switch {
	case v < 0.1:
		return "pct_0_10"
	case v < 0.2:
		return "pct_10_20"
	case v < 0.3:
		return "pct_20_30"
	case v < 0.4:
		return "pct_30_40"
	case v < 0.5:
		return "pct_40_50"
	case v < 0.6:
		return "pct_50_60"
	case v < 0.7:
		return "pct_60_70"
	case v < 0.8:
		return "pct_70_80"
	case v < 0.9:
		return "pct_80_90"
	default:
		return "pct_90_100"
	}
}
