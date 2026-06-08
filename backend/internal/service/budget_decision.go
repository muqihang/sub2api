package service

import "net/http"

type BudgetDecisionInput struct {
	Session                   SessionBudgetLedgerEntry
	Account                   AccountBudgetLedgerEntry
	Pool                      PoolUtilizationBudgetLedger
	RiskEvents                []RiskEventLedgerEntry
	UpstreamStatus            int
	CooldownActive            bool
	UtilizationHeadersPresent bool
}

func BuildBudgetDecision(input BudgetDecisionInput) BudgetDecision {
	base := BudgetDecision{
		Mode:          BudgetModeObserveOnly,
		Action:        BudgetActionObserve,
		AccountWeight: 1,
		QueuePriority: 0,
		ReasonCode:    "observe_only",
		SafeSummary:   budgetDecisionSafeSummary(input),
	}

	if input.Session.VerifierResult == BudgetVerifierFail {
		base.Action = BudgetActionP0Block
		base.AccountWeight = 0
		base.QueuePriority = -100
		base.ReasonCode = "p0_verifier_fail"
		base.SafeSummary["p0_action"] = BudgetActionP0Block
		return base
	}
	if input.Session.FallbackResult == BudgetFallbackTrue {
		base.Action = BudgetActionQuarantine
		base.AccountWeight = 0
		base.QueuePriority = -100
		base.ReasonCode = "p0_fallback"
		base.SafeSummary["p0_action"] = BudgetActionQuarantine
		return base
	}
	for _, ev := range input.RiskEvents {
		if ev.Severity == RiskSeverityP0 {
			if ev.ActionRecommendation == BudgetActionQuarantine {
				base.Action = BudgetActionQuarantine
				base.ReasonCode = "p0_" + safeIndicatorCode(ev.Kind)
			} else {
				base.Action = BudgetActionP0Block
				base.ReasonCode = "p0_" + safeIndicatorCode(ev.Kind)
			}
			base.AccountWeight = 0
			base.QueuePriority = -100
			base.SafeSummary["p0_action"] = base.Action
			return base
		}
	}

	if input.CooldownActive || input.UpstreamStatus == http.StatusTooManyRequests || input.Pool.Action == PoolUtilizationActionCooldown {
		base.Action = BudgetActionCooldown
		base.AccountWeight = 0
		base.QueuePriority = -50
		base.ReasonCode = "cooldown_recommendation"
		base.SafeSummary["scheduling_action"] = "cooldown"
		return base
	}

	if !input.UtilizationHeadersPresent || !accountUtilizationBucketsValid(input.Account) {
		base.ReasonCode = "observe_conservative_missing_utilization"
		base.SafeSummary["utilization_headers"] = "missing_or_malformed"
		return base
	}

	if input.Pool.RecommendedWeight > 0 {
		base.AccountWeight = input.Pool.RecommendedWeight
		base.QueuePriority = input.Pool.QueuePriority
	}
	switch input.Pool.Action {
	case PoolUtilizationActionCatchUp:
		base.ReasonCode = "observe_pool_catch_up"
		base.SafeSummary["scheduling_action"] = PoolUtilizationActionCatchUp
	case PoolUtilizationActionSlowDown:
		base.ReasonCode = "observe_pool_slow_down"
		base.SafeSummary["scheduling_action"] = PoolUtilizationActionSlowDown
	case PoolUtilizationActionMaintain:
		base.ReasonCode = "observe_pool_on_track"
		base.SafeSummary["scheduling_action"] = PoolUtilizationActionMaintain
	}
	return base
}

func budgetDecisionSafeSummary(input BudgetDecisionInput) map[string]any {
	return map[string]any{
		"mode":                 BudgetModeObserveOnly,
		"session_ref":          safeDecisionRef(input.Session.SessionRef),
		"account_ref":          safeDecisionRef(input.Account.AccountRef),
		"pool_profile":         normalizePoolProfile(input.Pool.Profile),
		"pool_status":          safeIndicatorCode(input.Pool.Status),
		"max_tokens_action":    "unchanged",
		"tools_action":         "unchanged",
		"thinking_action":      "unchanged",
		"stream_action":        "unchanged",
		"context_1m_action":    "unchanged",
		"model_action":         "unchanged",
		"body_action":          "unchanged",
		"prompt_action":        "unchanged",
		"tool_schema_action":   "unchanged",
		"output_config_action": "unchanged",
	}
}

func safeDecisionRef(ref string) string {
	// Decision summaries only expose internally generated HMAC refs. Opaque/scoped
	// external refs are safe in ledgers after validation, but omitting them here
	// avoids carrying credential-like text across decision boundaries.
	if ledgerGeneratedHMACRefRe.MatchString(ref) {
		return ref
	}
	return "omitted"
}

func accountUtilizationBucketsValid(account AccountBudgetLedgerEntry) bool {
	return utilizationBucketKnown(account.Utilization5hPercentageBucket) && utilizationBucketKnown(account.Utilization7dPercentageBucket)
}

func utilizationBucketKnown(bucket string) bool {
	switch bucket {
	case "pct_0_10", "pct_10_20", "pct_20_30", "pct_30_40", "pct_40_50", "pct_50_60", "pct_60_70", "pct_70_80", "pct_80_90", "pct_90_95", "pct_95_100", "pct_100":
		return true
	default:
		return false
	}
}
