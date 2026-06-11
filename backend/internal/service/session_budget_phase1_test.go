package service

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionBudgetLedger_RichClaudeCodeRequestObserveOnlySafeSummary(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-6-20260514",
		"max_tokens":32000,
		"stream":true,
		"thinking":{"type":"enabled","budget_tokens":32000},
		"context_management":{"edits":[{"type":"clear_tool_uses_20250919"}]},
		"output_config":{"type":"json_schema","schema":{"type":"object"}},
		"messages":[
			{"role":"user","content":[{"type":"text","text":"do not store me"}]},
			{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/secret"}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"secret output"}]}
		],
		"tools":[{"name":"Read","input_schema":{"type":"object"}},{"name":"Bash","input_schema":{"type":"object"}}]
	}`)

	entry, decision, err := BuildSessionBudgetLedgerEntry(SessionBudgetLedgerInput{
		RawSessionID:    "123e4567-e89b-12d3-a456-426614174000",
		RawUserID:       "person@example.test",
		RawAccountID:    "acct-123e4567-e89b-12d3-a456-426614174111",
		RequestBody:     body,
		VerifierResult:  BudgetVerifierPass,
		FallbackResult:  BudgetFallbackFalse,
		RequestIsStream: true,
	})
	if err != nil {
		t.Fatalf("BuildSessionBudgetLedgerEntry returned error: %v", err)
	}
	if decision.Mode != BudgetModeObserveOnly || decision.Action != BudgetActionObserve {
		t.Fatalf("rich Claude Code request must only observe, got mode=%q action=%q", decision.Mode, decision.Action)
	}
	if entry.MaxTokensBucket != "eq_32000" {
		t.Fatalf("max_tokens=32000 must be recorded as a bucket, got %q", entry.MaxTokensBucket)
	}
	if entry.ToolUseCount != 1 || entry.ToolResultCount != 1 || entry.ToolDefinitionCount != 2 {
		t.Fatalf("unexpected tool counts: use=%d result=%d defs=%d", entry.ToolUseCount, entry.ToolResultCount, entry.ToolDefinitionCount)
	}
	if !entry.ThinkingPresent || entry.ThinkingBudgetBucket != "eq_32000" || !entry.Context1MPresent || !entry.Stream {
		t.Fatalf("rich request shape was not summarized correctly: %+v", entry)
	}
	assertSafeLedgerJSON(t, entry)
	assertJSONDoesNotContain(t, entry, "do not store me", "secret output", "123e4567-e89b-12d3-a456-426614174000", "person@example.test")
}

func TestSessionBudgetLedger_Fable5ModelBucket(t *testing.T) {
	if got := modelNameBucket("claude-fable-5"); got != "claude-fable-5" {
		t.Fatalf("modelNameBucket(claude-fable-5) = %q", got)
	}
}

func TestSessionBudgetLedger_RejectsRawScopedRefsAndSensitiveText(t *testing.T) {
	_, _, err := BuildSessionBudgetLedgerEntry(SessionBudgetLedgerInput{
		SessionRefOverride: "123e4567-e89b-12d3-a456-426614174000",
		UserRefOverride:    "person@example.test",
		AccountRefOverride: strings.Repeat("a", 64),
		RequestBody:        []byte(`{"model":"claude-opus-4-6","messages":[]}`),
	})
	if err == nil {
		t.Fatal("expected raw uuid/email/plain hash ledger refs to be rejected")
	}
}

func TestAccountBudgetLedger_UtilizationHeadersAreBucketedAndNoRawIdentity(t *testing.T) {
	reset := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-status", "ok")
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "0.42")
	headers.Set("anthropic-ratelimit-unified-5h-reset", reset)
	headers.Set("anthropic-ratelimit-unified-7d-status", "ok")
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "91%")
	headers.Set("anthropic-ratelimit-unified-7d-reset", reset)
	headers.Set("anthropic-ratelimit-unified-overage-status", "disabled")

	account := &Account{ID: 42, Name: "do-not-log", Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Extra: map[string]any{"pool_profile": "normal", ccGatewayExtraAccountRef: "opaque:acct:v1:safe"}, ProxyID: budgetInt64Ptr(7), Proxy: &Proxy{ID: 7, Host: "127.0.0.1", Username: "raw-user", Password: "raw-pass"}}
	entry, err := BuildAccountBudgetLedgerEntry(AccountBudgetLedgerInput{Account: account, ResponseHeaders: headers, SchedulingWeight: 1.25})
	if err != nil {
		t.Fatalf("BuildAccountBudgetLedgerEntry returned error: %v", err)
	}
	if entry.AccountRef != "opaque:acct:v1:safe" {
		t.Fatalf("expected configured opaque account ref, got %q", entry.AccountRef)
	}
	if entry.ProxyRef == "" || strings.Contains(entry.ProxyRef, "raw") || strings.Contains(entry.ProxyRef, "127.0.0.1") {
		t.Fatalf("proxy ref must be opaque and omit proxy credential/host, got %q", entry.ProxyRef)
	}
	if entry.Utilization7dPercentageBucket != "pct_90_95" || entry.Utilization5hPercentageBucket != "pct_40_50" {
		t.Fatalf("unexpected utilization buckets: 5h=%q 7d=%q", entry.Utilization5hPercentageBucket, entry.Utilization7dPercentageBucket)
	}
	assertSafeLedgerJSON(t, entry)
	assertJSONDoesNotContain(t, entry, "do-not-log", "raw-user", "raw-pass", "127.0.0.1")
}

func TestAccountBudgetLedger_OverOneDecimalUtilizationMeansExceeded(t *testing.T) {
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "1.02")
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "102%")

	entry, err := BuildAccountBudgetLedgerEntry(AccountBudgetLedgerInput{
		Account:         &Account{ID: 43, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true},
		ResponseHeaders: headers,
	})
	if err != nil {
		t.Fatalf("BuildAccountBudgetLedgerEntry returned error: %v", err)
	}
	if entry.Utilization5hPercentageBucket != "pct_100" || entry.Utilization7dPercentageBucket != "pct_100" {
		t.Fatalf("over-limit utilization must bucket as 100%%, got 5h=%q 7d=%q", entry.Utilization5hPercentageBucket, entry.Utilization7dPercentageBucket)
	}
}

func TestUserAndRiskLedger_DoNotStoreRawIdentityOrReason(t *testing.T) {
	userEntry, err := BuildUserBudgetLedgerEntry(UserBudgetLedgerInput{
		RawUserID:          "person@example.test",
		ActiveSessionCount: 37,
		UsageShare:         0.83,
		RiskScore:          0.91,
		AbuseIndicators:    []string{"retry_storm", "contains person@example.test and Bearer abcdefghijklmnop"},
	})
	if err != nil {
		t.Fatalf("BuildUserBudgetLedgerEntry returned error: %v", err)
	}
	if userEntry.ActiveSessionCountBucket != "count_20_50" || userEntry.UsageShareBucket != "pct_80_90" || userEntry.RiskScoreBucket != "pct_90_100" {
		t.Fatalf("unexpected user buckets: %+v", userEntry)
	}

	riskEntry, err := BuildRiskEventLedgerEntry(RiskEventLedgerInput{
		Kind:            RiskEventKindVerifierFail,
		Severity:        RiskSeverityP0,
		RawSessionID:    "123e4567-e89b-12d3-a456-426614174000",
		RawUserID:       "person@example.test",
		RawAccountID:    "acct-raw",
		UnsafeRawReason: "verifier failed for person@example.test Authorization: Bearer abcdefghijklmnop",
		ObservedAt:      time.Unix(1700000000, 0).UTC(),
		Recommendation:  BudgetActionP0Block,
	})
	if err != nil {
		t.Fatalf("BuildRiskEventLedgerEntry returned error: %v", err)
	}
	if !strings.HasPrefix(riskEntry.SafeReason, "reason_") {
		t.Fatalf("risk reason should be a reason bucket/code, got length=%d", len(riskEntry.SafeReason))
	}
	assertSafeLedgerJSON(t, userEntry)
	assertSafeLedgerJSON(t, riskEntry)
	assertJSONDoesNotContain(t, userEntry, "person@example.test", "abcdefghijklmnop")
	assertJSONDoesNotContain(t, riskEntry, "person@example.test", "abcdefghijklmnop", "123e4567-e89b-12d3-a456-426614174000")
}

func TestPoolUtilizationLedger_ProfileRecommendationsAreSchedulingOnly(t *testing.T) {
	behind := BuildPoolUtilizationBudgetLedger(PoolUtilizationBudgetInput{Profile: PoolProfileAggressive, Utilization7d: 0.20, WindowAge: 24 * time.Hour, CooldownActive: false})
	if behind.Action != PoolUtilizationActionCatchUp || behind.RecommendedWeight <= 1.4 || behind.HardBlock {
		t.Fatalf("aggressive behind target should catch up via weight only: %+v", behind)
	}
	ahead := BuildPoolUtilizationBudgetLedger(PoolUtilizationBudgetInput{Profile: PoolProfileNormal, Utilization7d: 0.80, WindowAge: 24 * time.Hour, CooldownActive: false})
	if ahead.Action != PoolUtilizationActionSlowDown || ahead.RecommendedWeight >= 1 || ahead.HardBlock {
		t.Fatalf("normal ahead target should slow down via weight only: %+v", ahead)
	}
	cooldown := BuildPoolUtilizationBudgetLedger(PoolUtilizationBudgetInput{Profile: PoolProfileAggressive, Utilization7d: 0.10, WindowAge: 24 * time.Hour, CooldownActive: true})
	if cooldown.RecommendedWeight != 0 || cooldown.Action != PoolUtilizationActionCooldown || cooldown.HardBlock {
		t.Fatalf("cooldown should pause scheduling but not hard block request ability: %+v", cooldown)
	}
}

func TestBudgetLedger_RejectsSpoofedHMACOverrideAndBucketsRawReasons(t *testing.T) {
	_, _, err := BuildSessionBudgetLedgerEntry(SessionBudgetLedgerInput{
		SessionRefOverride: "hmac-sha256:" + strings.Repeat("a", 64),
		RequestBody:        []byte(`{"model":"claude-sonnet-4-6","messages":[]}`),
	})
	if err == nil {
		t.Fatal("expected caller-supplied hmac override to be rejected")
	}

	entry, err := BuildRiskEventLedgerEntry(RiskEventLedgerInput{
		Kind:            RiskEventKindRiskText,
		Severity:        RiskSeverityP0,
		RawSessionID:    "session-raw",
		RawUserID:       "user-raw",
		RawAccountID:    "account-raw",
		UnsafeRawReason: "tool output leaked /Users/local/project/secret.txt via http://user:pass@proxy.local:8080 and secret abcdef1234567890abcdef1234567890 with prompt words",
		Recommendation:  BudgetActionQuarantine,
	})
	if err != nil {
		t.Fatalf("BuildRiskEventLedgerEntry returned error: %v", err)
	}
	if strings.Contains(entry.SafeReason, "secret.txt") || strings.Contains(entry.SafeReason, "user:pass") || strings.Contains(entry.SafeReason, "/Users/") || strings.Contains(entry.SafeReason, "abcdef") || strings.Contains(entry.SafeReason, "prompt words") {
		t.Fatalf("risk safe reason must omit raw reason details, got redacted length=%d", len(entry.SafeReason))
	}
	if entry.SafeReason == "" || !strings.HasPrefix(entry.SafeReason, "reason_") {
		t.Fatalf("risk safe reason should be a reason bucket/code, got %q", entry.SafeReason)
	}

	accountEntry, err := BuildAccountBudgetLedgerEntry(AccountBudgetLedgerInput{
		Account:              &Account{ID: 99, Status: StatusActive, Schedulable: true},
		LastRiskEventSummary: "raw prompt /tmp/private.txt http://user:pass@proxy.local:8080 Bearer abcdef1234567890",
	})
	if err != nil {
		t.Fatalf("BuildAccountBudgetLedgerEntry returned error: %v", err)
	}
	if accountEntry.LastRiskEventSummary == "" || !strings.HasPrefix(accountEntry.LastRiskEventSummary, "reason_") {
		t.Fatalf("account last risk summary should be bucket/code only, got %q", accountEntry.LastRiskEventSummary)
	}
	assertSafeLedgerJSON(t, entry)
	assertSafeLedgerJSON(t, accountEntry)
}

func TestBudgetLedger_RejectsUserRefOverrideAndBucketsIndicators(t *testing.T) {
	_, err := BuildUserBudgetLedgerEntry(UserBudgetLedgerInput{
		UserRefOverride:    "hmac-sha256:" + strings.Repeat("b", 64),
		ActiveSessionCount: 1,
	})
	if err == nil {
		t.Fatal("expected user ref override to be rejected")
	}

	entry, _, err := BuildSessionBudgetLedgerEntry(SessionBudgetLedgerInput{
		RawSessionID: "session-raw",
		RawUserID:    "user-raw",
		RawAccountID: "account-raw",
		RequestBody:  []byte(`{"model":"claude-sonnet-4-6","messages":[]}`),
		RiskFlags:    []string{"raw prompt says build a private thing", "retry_storm", "Bearer abcdef1234567890"},
	})
	if err != nil {
		t.Fatalf("BuildSessionBudgetLedgerEntry returned error: %v", err)
	}
	joined := strings.Join(entry.RiskFlags, ",")
	if strings.Contains(joined, "prompt") || strings.Contains(joined, "private") || strings.Contains(joined, "abcdef") || strings.Contains(joined, "bearer") {
		t.Fatalf("risk flags should be allowlisted or bucketed, got count=%d", len(entry.RiskFlags))
	}
	if !containsString(entry.RiskFlags, "reason_other") || !containsString(entry.RiskFlags, "retry_storm") || !containsString(entry.RiskFlags, "reason_sensitive") {
		t.Fatalf("risk flags missing expected buckets/codes: count=%d", len(entry.RiskFlags))
	}

	userEntry, err := BuildUserBudgetLedgerEntry(UserBudgetLedgerInput{
		RawUserID:       "user-raw",
		AbuseIndicators: []string{"raw prompt text with /tmp/private.txt", "retry_storm"},
	})
	if err != nil {
		t.Fatalf("BuildUserBudgetLedgerEntry returned error: %v", err)
	}
	joined = strings.Join(userEntry.AbuseIndicators, ",")
	if strings.Contains(joined, "prompt") || strings.Contains(joined, "private") || strings.Contains(joined, "/tmp") {
		t.Fatalf("abuse indicators should be allowlisted or bucketed, got count=%d", len(userEntry.AbuseIndicators))
	}
	if !containsString(userEntry.AbuseIndicators, "reason_other") || !containsString(userEntry.AbuseIndicators, "retry_storm") {
		t.Fatalf("abuse indicators missing expected buckets/codes: count=%d", len(userEntry.AbuseIndicators))
	}
}

func TestSessionBudgetObserveSink_ExportsRedactedJSONLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-budget.jsonl")
	sink, err := NewFileSessionBudgetObserveSink(path)
	if err != nil {
		t.Fatalf("NewFileSessionBudgetObserveSink returned error: %v", err)
	}
	record := SessionBudgetObserveRecord{
		Session: SessionBudgetLedgerEntry{
			SessionRef:   "hmac-sha256:" + strings.Repeat("a", 64),
			UserRef:      "hmac-sha256:" + strings.Repeat("b", 64),
			AccountRef:   "hmac-sha256:" + strings.Repeat("c", 64),
			ModelFamily:  "claude-sonnet",
			StatusBucket: "status_2xx",
		},
		Account: AccountBudgetLedgerEntry{
			AccountRef: "hmac-sha256:" + strings.Repeat("c", 64),
			ProxyRef:   "hmac-sha256:" + strings.Repeat("d", 64),
		},
		User: UserBudgetLedgerEntry{
			UserRef: "hmac-sha256:" + strings.Repeat("b", 64),
		},
		Decision: BudgetDecision{Mode: BudgetModeObserveOnly, Action: BudgetActionObserve},
	}
	sink.ObserveSessionBudget(t.Context(), record)

	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("exported ledger was not written: %v", err)
	}
	if !strings.HasSuffix(string(blob), "\n") {
		t.Fatalf("export must be JSON Lines, got %q", string(blob))
	}
	if strings.Contains(string(blob), "Authorization") || strings.Contains(string(blob), "Bearer") || strings.Contains(string(blob), "person@example") {
		t.Fatalf("exported ledger contains sensitive marker")
	}
	var decoded SessionBudgetObserveRecord
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(blob))), &decoded); err != nil {
		t.Fatalf("exported ledger line is not JSON: %v", err)
	}
	if decoded.Session.StatusBucket != "status_2xx" || decoded.Decision.Action != BudgetActionObserve {
		t.Fatalf("exported ledger lost status/decision: %+v", decoded)
	}
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func assertSafeLedgerJSON(t *testing.T, v any) {
	t.Helper()
	if err := ValidateNoRawSensitiveLedger(v); err != nil {
		t.Fatalf("ledger contains unsafe data: %v", err)
	}
}

func assertJSONDoesNotContain(t *testing.T, v any, forbidden ...string) {
	t.Helper()
	b, _ := json.Marshal(v)
	text := string(b)
	for _, token := range forbidden {
		if token != "" && strings.Contains(text, token) {
			t.Fatalf("ledger JSON contains forbidden token at redacted_index=%d json_length=%d", strings.Index(text, token), len(text))
		}
	}
}

func budgetInt64Ptr(v int64) *int64 { return &v }
