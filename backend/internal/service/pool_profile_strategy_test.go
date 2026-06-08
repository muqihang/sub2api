package service

import (
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestPoolProfileStrategy_AggressiveFinalWindowBelow95IsNotOnTrack(t *testing.T) {
	pool := BuildPoolUtilizationBudgetLedger(PoolUtilizationBudgetInput{Profile: PoolProfileAggressive, Utilization7d: 0.94, WindowAge: 3 * 24 * time.Hour})
	if pool.Status == PoolUtilizationStatusOnTrack || pool.Action != PoolUtilizationActionCatchUp {
		t.Fatalf("aggressive 3d utilization below 95%% must still catch up, got %+v", pool)
	}
}

func TestPoolProfileStrategy_NonFiniteUtilizationHeadersAreUnknown(t *testing.T) {
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "NaN")
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "+Inf")
	entry, err := BuildAccountBudgetLedgerEntry(AccountBudgetLedgerInput{Account: &Account{ID: 8, Status: StatusActive, Schedulable: true}, ResponseHeaders: headers})
	if err != nil {
		t.Fatalf("account ledger: %v", err)
	}
	if entry.Utilization5hPercentageBucket != "unknown" || entry.Utilization7dPercentageBucket != "unknown" {
		t.Fatalf("non-finite utilization must be unknown, got %+v", entry)
	}
	decision := BuildBudgetDecision(BudgetDecisionInput{Session: validDecisionSession(), Account: entry, Pool: BuildPoolUtilizationBudgetLedger(PoolUtilizationBudgetInput{Profile: PoolProfileAggressive, Utilization7d: 0.1, WindowAge: 24 * time.Hour}), UtilizationHeadersPresent: true})
	if decision.ReasonCode != "observe_conservative_missing_utilization" || decision.AccountWeight != 1 {
		t.Fatalf("non-finite headers must conservative observe, got %+v", decision)
	}
}

func TestPoolProfileStrategy_LiveObserveUsesResetWindowAgeForCatchUp(t *testing.T) {
	reset := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-status", "allowed")
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "6%")
	headers.Set("anthropic-ratelimit-unified-7d-status", "allowed")
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "20%")
	headers.Set("anthropic-ratelimit-unified-7d-reset", reset)
	upstream := &anthropicHTTPUpstreamRecorder{resp: newAnthropicSuccessResponse()}
	for k, v := range headers {
		upstream.resp.Header[k] = v
	}
	sink := &recordingBudgetLedgerSink{}
	svc := newGatewayBudgetIntegrationService(upstream, sink)
	account := newAnthropicOAuthAccountForClaudeForwardTest()
	account.Extra["pool_profile"] = PoolProfileAggressive
	c, ctx := newAnthropicForwardTestContext("/v1/messages", true)
	body := []byte(`{"model":"claude-sonnet-4-6","stream":false,"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)
	_, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	if len(sink.decisions) != 1 {
		t.Fatalf("expected one decision, got %d", len(sink.decisions))
	}
	if sink.decisions[0].ReasonCode != "observe_pool_catch_up" || sink.decisions[0].AccountWeight <= 1 {
		t.Fatalf("live observe should catch up when aggressive behind target, got %+v", sink.decisions[0])
	}
}

func TestPoolProfileStrategy_UnixResetHeadersDriveBucketsAndWindowAge(t *testing.T) {
	reset5h := time.Now().Add(2 * time.Hour).UTC().Unix()
	reset7d := time.Now().Add(48 * time.Hour).UTC().Unix()
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-status", "allowed")
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "0.06")
	headers.Set("anthropic-ratelimit-unified-5h-reset", strconv.FormatInt(reset5h, 10))
	headers.Set("anthropic-ratelimit-unified-7d-status", "allowed")
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "0.20")
	headers.Set("anthropic-ratelimit-unified-7d-reset", strconv.FormatInt(reset7d, 10))

	account := &Account{ID: 7, Status: StatusActive, Schedulable: true, Extra: map[string]any{"pool_profile": PoolProfileAggressive}}
	accountEntry, err := BuildAccountBudgetLedgerEntry(AccountBudgetLedgerInput{Account: account, ResponseHeaders: headers})
	if err != nil {
		t.Fatalf("account ledger: %v", err)
	}
	if accountEntry.Utilization5hResetBucket != "le_5h" || accountEntry.Utilization7dResetBucket != "gt_24h" {
		t.Fatalf("unix reset headers must be parsed, got 5h=%q 7d=%q", accountEntry.Utilization5hResetBucket, accountEntry.Utilization7dResetBucket)
	}

	age := poolWindowAgeFromReset(PoolProfileAggressive, headers)
	if age < 119*time.Hour || age > 121*time.Hour {
		t.Fatalf("pool age should be derived from elapsed 7d reset window, got %s", age)
	}
}

func TestPoolProfileStrategy_AggressiveUsesWeekAgeForThreeDayTarget(t *testing.T) {
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-7d-reset", strconv.FormatInt(time.Now().Add(6*24*time.Hour).UTC().Unix(), 10))

	age := poolWindowAgeFromReset(PoolProfileAggressive, headers)
	if age < 23*time.Hour || age > 25*time.Hour {
		t.Fatalf("aggressive profile should use elapsed week age, got %s", age)
	}
	pool := BuildPoolUtilizationBudgetLedger(PoolUtilizationBudgetInput{
		Profile:       PoolProfileAggressive,
		Utilization7d: 0.20,
		WindowAge:     age,
	})
	if pool.Action != PoolUtilizationActionCatchUp {
		t.Fatalf("aggressive day-one 20%% utilization should catch up toward 3-day target, got %+v", pool)
	}
}

func TestPoolProfileStrategy_NormalBehindAheadAndAggressiveBehindAhead(t *testing.T) {
	cases := []struct {
		name       string
		profile    string
		util       float64
		age        time.Duration
		wantAction string
		minWeight  float64
		maxWeight  float64
	}{
		{name: "normal behind slight catch up", profile: PoolProfileNormal, util: 0.10, age: 3 * 24 * time.Hour, wantAction: PoolUtilizationActionCatchUp, minWeight: 1.05, maxWeight: 1.35},
		{name: "normal ahead slow down", profile: PoolProfileNormal, util: 0.80, age: 24 * time.Hour, wantAction: PoolUtilizationActionSlowDown, minWeight: 0.5, maxWeight: 0.95},
		{name: "aggressive behind strong catch up", profile: PoolProfileAggressive, util: 0.20, age: 24 * time.Hour, wantAction: PoolUtilizationActionCatchUp, minWeight: 1.5, maxWeight: 2.5},
		{name: "aggressive ahead slow down", profile: PoolProfileAggressive, util: 0.90, age: 24 * time.Hour, wantAction: PoolUtilizationActionSlowDown, minWeight: 0.4, maxWeight: 0.9},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := BuildPoolUtilizationBudgetLedger(PoolUtilizationBudgetInput{Profile: tc.profile, Utilization7d: tc.util, WindowAge: tc.age})
			if pool.Action != tc.wantAction || pool.RecommendedWeight < tc.minWeight || pool.RecommendedWeight > tc.maxWeight || pool.HardBlock {
				t.Fatalf("unexpected strategy: %+v", pool)
			}
			decision := BuildBudgetDecision(BudgetDecisionInput{Session: validDecisionSession(), Account: validDecisionAccount(), Pool: pool, UtilizationHeadersPresent: true})
			if decision.Action != BudgetActionObserve {
				t.Fatalf("profile strategy must not hard action non-P0 decisions: %+v", decision)
			}
			assertDecisionDoesNotLimitCapabilities(t, decision)
		})
	}
}

func TestPoolProfileStrategy_AggressiveCooldownAndP0SafetyWin(t *testing.T) {
	pool := BuildPoolUtilizationBudgetLedger(PoolUtilizationBudgetInput{Profile: PoolProfileAggressive, Utilization7d: 0.05, WindowAge: 48 * time.Hour, CooldownActive: true})
	decision := BuildBudgetDecision(BudgetDecisionInput{Session: validDecisionSession(), Account: validDecisionAccount(), Pool: pool, UpstreamStatus: http.StatusTooManyRequests, CooldownActive: true, UtilizationHeadersPresent: true})
	if decision.Action != BudgetActionCooldown || decision.AccountWeight != 0 || decision.QueuePriority >= 0 {
		t.Fatalf("aggressive cooldown must pause scheduling: %+v", decision)
	}
	assertDecisionDoesNotLimitCapabilities(t, decision)

	p0 := BuildBudgetDecision(BudgetDecisionInput{Session: SessionBudgetLedgerEntry{VerifierResult: BudgetVerifierFail, FallbackResult: BudgetFallbackFalse}, Account: validDecisionAccount(), Pool: BuildPoolUtilizationBudgetLedger(PoolUtilizationBudgetInput{Profile: PoolProfileAggressive, Utilization7d: 0.05, WindowAge: 48 * time.Hour}), UtilizationHeadersPresent: true})
	if p0.Action != BudgetActionP0Block || p0.AccountWeight != 0 {
		t.Fatalf("aggressive profile must not bypass P0 quarantine/block: %+v", p0)
	}
}

func TestPoolProfileStrategy_HeadersDriveProfileFromAccountLedger(t *testing.T) {
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-status", "allowed")
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "6%")
	headers.Set("anthropic-ratelimit-unified-7d-status", "allowed")
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "20%")
	account := &Account{ID: 7, Status: StatusActive, Schedulable: true, Extra: map[string]any{"pool_profile": PoolProfileAggressive}}
	accountEntry, err := BuildAccountBudgetLedgerEntry(AccountBudgetLedgerInput{Account: account, ResponseHeaders: headers})
	if err != nil {
		t.Fatalf("account ledger: %v", err)
	}
	if accountEntry.PoolProfile != PoolProfileAggressive || accountEntry.Utilization5hPercentageBucket != "pct_0_10" || accountEntry.Utilization7dPercentageBucket != "pct_20_30" {
		t.Fatalf("account profile/headers not bucketed: %+v", accountEntry)
	}
	pool := BuildPoolUtilizationBudgetLedger(PoolUtilizationBudgetInput{Profile: accountEntry.PoolProfile, Utilization7d: 0.20, WindowAge: 24 * time.Hour})
	decision := BuildBudgetDecision(BudgetDecisionInput{Session: validDecisionSession(), Account: accountEntry, Pool: pool, UtilizationHeadersPresent: true})
	if decision.AccountWeight <= 1 || decision.Action != BudgetActionObserve {
		t.Fatalf("aggressive valid behind headers should only raise scheduling weight: %+v", decision)
	}
}
