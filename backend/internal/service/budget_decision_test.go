package service

import (
	"net/http"
	"testing"
	"time"
)

func TestBudgetDecision_RichRequestObserveOnlyDoesNotLimitCapabilities(t *testing.T) {
	session := SessionBudgetLedgerEntry{
		SessionRef:           scopedStickyHMAC("test_session", "rich"),
		UserRef:              scopedStickyHMAC("test_user", "u"),
		AccountRef:           scopedStickyHMAC("test_account", "a"),
		ModelFamily:          "opus",
		ModelNameBucket:      "claude-opus-4-6-thinking",
		MessageCount:         48,
		ToolUseCount:         37,
		ToolResultCount:      37,
		ThinkingPresent:      true,
		ThinkingBudgetBucket: "eq_32000",
		Stream:               true,
		MaxTokensBucket:      "eq_32000",
		Context1MPresent:     true,
		VerifierResult:       BudgetVerifierPass,
		FallbackResult:       BudgetFallbackFalse,
	}
	decision := BuildBudgetDecision(BudgetDecisionInput{
		Session:                   session,
		Account:                   validDecisionAccount(),
		Pool:                      BuildPoolUtilizationBudgetLedger(PoolUtilizationBudgetInput{Profile: PoolProfileNormal, Utilization7d: 0.40, WindowAge: 72 * time.Hour}),
		UtilizationHeadersPresent: true,
	})
	if decision.Mode != BudgetModeObserveOnly || decision.Action != BudgetActionObserve {
		t.Fatalf("rich valid request must only observe, got mode=%q action=%q", decision.Mode, decision.Action)
	}
	if decision.SafeSummary["max_tokens_action"] != "unchanged" || decision.SafeSummary["tools_action"] != "unchanged" || decision.SafeSummary["thinking_action"] != "unchanged" || decision.SafeSummary["stream_action"] != "unchanged" || decision.SafeSummary["context_1m_action"] != "unchanged" || decision.SafeSummary["model_action"] != "unchanged" {
		t.Fatalf("decision must explicitly leave Claude Code capabilities unchanged: %+v", decision.SafeSummary)
	}
	if err := ValidateNoRawSensitiveLedger(decision); err != nil {
		t.Fatalf("decision summary unsafe: %v", err)
	}
}

func TestBudgetDecision_PoolUtilizationAdjustsSchedulingOnly(t *testing.T) {
	aggressiveBehind := BuildBudgetDecision(BudgetDecisionInput{
		Session:                   validDecisionSession(),
		Account:                   validDecisionAccount(),
		Pool:                      BuildPoolUtilizationBudgetLedger(PoolUtilizationBudgetInput{Profile: PoolProfileAggressive, Utilization7d: 0.20, WindowAge: 24 * time.Hour}),
		UtilizationHeadersPresent: true,
	})
	if aggressiveBehind.Action != BudgetActionObserve || aggressiveBehind.AccountWeight <= 1.4 || aggressiveBehind.QueuePriority <= 0 {
		t.Fatalf("aggressive behind target should increase scheduling weight only: %+v", aggressiveBehind)
	}
	assertDecisionDoesNotLimitCapabilities(t, aggressiveBehind)

	aggressiveAhead := BuildBudgetDecision(BudgetDecisionInput{
		Session:                   validDecisionSession(),
		Account:                   validDecisionAccount(),
		Pool:                      BuildPoolUtilizationBudgetLedger(PoolUtilizationBudgetInput{Profile: PoolProfileAggressive, Utilization7d: 0.90, WindowAge: 24 * time.Hour}),
		UtilizationHeadersPresent: true,
	})
	if aggressiveAhead.Action != BudgetActionObserve || aggressiveAhead.AccountWeight >= 1 || aggressiveAhead.QueuePriority >= 0 {
		t.Fatalf("aggressive ahead target should slow scheduling only: %+v", aggressiveAhead)
	}
	assertDecisionDoesNotLimitCapabilities(t, aggressiveAhead)
}

func TestBudgetDecision_CooldownAnd429PauseSchedulingWithoutHardBlock(t *testing.T) {
	decision := BuildBudgetDecision(BudgetDecisionInput{
		Session:                   validDecisionSession(),
		Account:                   validDecisionAccount(),
		Pool:                      BuildPoolUtilizationBudgetLedger(PoolUtilizationBudgetInput{Profile: PoolProfileAggressive, Utilization7d: 0.10, WindowAge: 24 * time.Hour, CooldownActive: true}),
		UpstreamStatus:            http.StatusTooManyRequests,
		CooldownActive:            true,
		UtilizationHeadersPresent: true,
	})
	if decision.Action != BudgetActionCooldown || decision.AccountWeight != 0 || decision.QueuePriority >= 0 {
		t.Fatalf("429/cooldown should pause scheduling via recommendation only: %+v", decision)
	}
	if decision.Mode != BudgetModeObserveOnly {
		t.Fatalf("cooldown must remain observe-only mode, got %q", decision.Mode)
	}
	assertDecisionDoesNotLimitCapabilities(t, decision)
}

func TestBudgetDecision_P0VerifierFailBlocksAndFallbackQuarantines(t *testing.T) {
	verifierFail := BuildBudgetDecision(BudgetDecisionInput{
		Session: SessionBudgetLedgerEntry{VerifierResult: BudgetVerifierFail, FallbackResult: BudgetFallbackFalse},
	})
	if verifierFail.Action != BudgetActionP0Block || verifierFail.AccountWeight != 0 || verifierFail.ReasonCode != "p0_verifier_fail" {
		t.Fatalf("verifier fail must hard block as P0: %+v", verifierFail)
	}

	fallback := BuildBudgetDecision(BudgetDecisionInput{
		Session: SessionBudgetLedgerEntry{VerifierResult: BudgetVerifierPass, FallbackResult: BudgetFallbackTrue},
	})
	if fallback.Action != BudgetActionQuarantine || fallback.AccountWeight != 0 || fallback.ReasonCode != "p0_fallback" {
		t.Fatalf("fallback must quarantine as P0: %+v", fallback)
	}
}

func TestBudgetDecision_MissingOrMalformedUtilizationIsConservativeObserve(t *testing.T) {
	missing := BuildBudgetDecision(BudgetDecisionInput{
		Session:                   validDecisionSession(),
		Pool:                      BuildPoolUtilizationBudgetLedger(PoolUtilizationBudgetInput{Profile: PoolProfileAggressive, Utilization7d: 0.0, WindowAge: 48 * time.Hour}),
		UtilizationHeadersPresent: false,
	})
	if missing.Action != BudgetActionObserve || missing.AccountWeight != 1 || missing.QueuePriority != 0 || missing.ReasonCode != "observe_conservative_missing_utilization" {
		t.Fatalf("missing utilization must conservative observe, got %+v", missing)
	}
	assertDecisionDoesNotLimitCapabilities(t, missing)
}

func TestBudgetDecision_SessionBuilderUsesEngineForP0(t *testing.T) {
	_, decision, err := BuildSessionBudgetLedgerEntry(SessionBudgetLedgerInput{
		RawSessionID:   "session-raw",
		RawUserID:      "user-raw",
		RawAccountID:   "account-raw",
		RequestBody:    []byte(`{"model":"claude-sonnet-4-6","messages":[]}`),
		VerifierResult: BudgetVerifierFail,
		FallbackResult: BudgetFallbackFalse,
	})
	if err != nil {
		t.Fatalf("BuildSessionBudgetLedgerEntry returned error: %v", err)
	}
	if decision.Action != BudgetActionP0Block || decision.ReasonCode != "p0_verifier_fail" {
		t.Fatalf("session builder must use decision engine for verifier P0, got %+v", decision)
	}

	_, decision, err = BuildSessionBudgetLedgerEntry(SessionBudgetLedgerInput{
		RawSessionID:   "session-raw",
		RawUserID:      "user-raw",
		RawAccountID:   "account-raw",
		RequestBody:    []byte(`{"model":"claude-sonnet-4-6","messages":[]}`),
		VerifierResult: BudgetVerifierPass,
		FallbackResult: BudgetFallbackTrue,
	})
	if err != nil {
		t.Fatalf("BuildSessionBudgetLedgerEntry returned error: %v", err)
	}
	if decision.Action != BudgetActionQuarantine || decision.ReasonCode != "p0_fallback" {
		t.Fatalf("session builder must use decision engine for fallback P0, got %+v", decision)
	}
}

func TestBudgetDecision_SafeSummaryRejectsUnsafeRefsAndMalformedUtilization(t *testing.T) {
	unsafe := BuildBudgetDecision(BudgetDecisionInput{
		Session: SessionBudgetLedgerEntry{
			SessionRef:     "opaque:Bearer abcdef1234567890",
			VerifierResult: BudgetVerifierPass,
			FallbackResult: BudgetFallbackFalse,
		},
		Account:                   AccountBudgetLedgerEntry{AccountRef: "opaque:http://user:pass@proxy.local:8080"},
		Pool:                      BuildPoolUtilizationBudgetLedger(PoolUtilizationBudgetInput{Profile: PoolProfileAggressive, Utilization7d: 0.20, WindowAge: 24 * time.Hour}),
		UtilizationHeadersPresent: true,
	})
	if unsafe.SafeSummary["session_ref"] != "omitted" || unsafe.SafeSummary["account_ref"] != "omitted" {
		t.Fatalf("unsafe refs must be omitted from safe summary: %+v", unsafe.SafeSummary)
	}
	if err := ValidateNoRawSensitiveLedger(unsafe); err != nil {
		t.Fatalf("decision with unsafe refs should still validate after omission: %v", err)
	}

	malformed := BuildBudgetDecision(BudgetDecisionInput{
		Session:                   validDecisionSession(),
		Account:                   AccountBudgetLedgerEntry{Utilization7dPercentageBucket: "unknown", Utilization5hPercentageBucket: "malformed"},
		Pool:                      BuildPoolUtilizationBudgetLedger(PoolUtilizationBudgetInput{Profile: PoolProfileAggressive, Utilization7d: 0.20, WindowAge: 24 * time.Hour}),
		UtilizationHeadersPresent: true,
	})
	if malformed.AccountWeight != 1 || malformed.QueuePriority != 0 || malformed.ReasonCode != "observe_conservative_missing_utilization" {
		t.Fatalf("unknown/malformed utilization buckets must conservative observe, got %+v", malformed)
	}
}

func TestBudgetDecision_PartialMalformedUtilizationIsConservativeObserve(t *testing.T) {
	partial := BuildBudgetDecision(BudgetDecisionInput{
		Session:                   validDecisionSession(),
		Account:                   AccountBudgetLedgerEntry{Utilization7dPercentageBucket: "pct_40_50", Utilization5hPercentageBucket: "unknown"},
		Pool:                      BuildPoolUtilizationBudgetLedger(PoolUtilizationBudgetInput{Profile: PoolProfileAggressive, Utilization7d: 0.20, WindowAge: 24 * time.Hour}),
		UtilizationHeadersPresent: true,
	})
	if partial.AccountWeight != 1 || partial.QueuePriority != 0 || partial.ReasonCode != "observe_conservative_missing_utilization" {
		t.Fatalf("partial malformed utilization buckets must conservative observe, got %+v", partial)
	}
}

func validDecisionAccount() AccountBudgetLedgerEntry {
	return AccountBudgetLedgerEntry{Utilization5hPercentageBucket: "pct_40_50", Utilization7dPercentageBucket: "pct_40_50"}
}

func validDecisionSession() SessionBudgetLedgerEntry {
	return SessionBudgetLedgerEntry{VerifierResult: BudgetVerifierPass, FallbackResult: BudgetFallbackFalse, MaxTokensBucket: "eq_32000", Stream: true, ThinkingPresent: true, Context1MPresent: true}
}

func assertDecisionDoesNotLimitCapabilities(t *testing.T, d BudgetDecision) {
	t.Helper()
	for _, key := range []string{"max_tokens_action", "tools_action", "thinking_action", "stream_action", "context_1m_action", "model_action", "body_action", "output_config_action"} {
		if d.SafeSummary[key] != "unchanged" {
			t.Fatalf("decision changed capability %s: %+v", key, d.SafeSummary)
		}
	}
}
