package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFormalPoolStatusDashboard_ClassifiesRequiredStates(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	reset := now.Add(30 * time.Minute)

	tests := []struct {
		name       string
		account    Account
		runtime    FormalPoolStatusRuntimeSnapshot
		wantState  string
		wantCounts map[string]int
	}{
		{
			name:      "production with complete evidence is production and counted as normal",
			account:   formalPoolDashboardTestAccount(1, FormalPoolStageProduction),
			runtime:   formalPoolDashboardCompleteRuntime(1),
			wantState: FormalPoolDashboardStateProduction,
			wantCounts: map[string]int{
				FormalPoolDashboardStateProduction: 1,
				FormalPoolDashboardStateNormal:     1,
			},
		},
		{
			name:      "warming with complete evidence is warming",
			account:   formalPoolDashboardTestAccount(2, FormalPoolStageWarming),
			runtime:   formalPoolDashboardCompleteRuntime(2),
			wantState: FormalPoolDashboardStateWarming,
			wantCounts: map[string]int{
				FormalPoolDashboardStateWarming: 1,
			},
		},
		{
			name: "rate limit active outranks normal",
			account: func() Account {
				acc := formalPoolDashboardTestAccount(3, FormalPoolStageProduction)
				acc.RateLimitResetAt = &reset
				acc.Extra[FormalPoolExtraRateLimitAction] = "cooldown"
				return acc
			}(),
			runtime:   formalPoolDashboardCompleteRuntime(3),
			wantState: FormalPoolDashboardStateRateLimited,
			wantCounts: map[string]int{
				FormalPoolDashboardStateRateLimited: 1,
			},
		},
		{
			name: "manual risk outranks quarantine",
			account: func() Account {
				acc := formalPoolDashboardTestAccount(4, FormalPoolStageQuarantined)
				acc.Status = StatusError
				acc.Extra[FormalPoolExtraHealthcheckRiskTextDetected] = true
				acc.Extra[FormalPoolExtraHealthcheckStatusCodeBucket] = "status_403"
				acc.Extra[FormalPoolExtraQuarantineReason] = "unusual_activity"
				return acc
			}(),
			runtime:   formalPoolDashboardCompleteRuntime(4),
			wantState: FormalPoolDashboardStateManualRisk,
			wantCounts: map[string]int{
				FormalPoolDashboardStateManualRisk: 1,
			},
		},
		{
			name: "explicit quarantine without high risk marker is quarantined",
			account: func() Account {
				acc := formalPoolDashboardTestAccount(5, FormalPoolStageQuarantined)
				acc.Status = StatusError
				acc.Extra[FormalPoolExtraQuarantineReason] = "reason_proxy"
				return acc
			}(),
			runtime:   formalPoolDashboardCompleteRuntime(5),
			wantState: FormalPoolDashboardStateQuarantined,
			wantCounts: map[string]int{
				FormalPoolDashboardStateQuarantined: 1,
			},
		},
		{
			name: "other error is error",
			account: func() Account {
				acc := formalPoolDashboardTestAccount(6, FormalPoolStageProduction)
				acc.Status = StatusError
				acc.Extra[FormalPoolExtraLastFailureCode] = "proxy_timeout"
				return acc
			}(),
			runtime:   formalPoolDashboardCompleteRuntime(6),
			wantState: FormalPoolDashboardStateError,
			wantCounts: map[string]int{
				FormalPoolDashboardStateError: 1,
			},
		},
		{
			name: "disabled account is inactive and never normal",
			account: func() Account {
				acc := formalPoolDashboardTestAccount(7, FormalPoolStageProduction)
				acc.Status = StatusDisabled
				return acc
			}(),
			runtime:   formalPoolDashboardCompleteRuntime(7),
			wantState: FormalPoolDashboardStateInactive,
			wantCounts: map[string]int{
				FormalPoolDashboardStateInactive: 1,
			},
		},
		{
			name: "effective unschedulable account is not_schedulable and never normal",
			account: func() Account {
				acc := formalPoolDashboardTestAccount(8, FormalPoolStageProduction)
				acc.TempUnschedulableUntil = formalPoolDashboardPtrTime(time.Now().Add(time.Hour))
				return acc
			}(),
			runtime:   formalPoolDashboardCompleteRuntime(8),
			wantState: FormalPoolDashboardStateNotSchedulable,
			wantCounts: map[string]int{
				FormalPoolDashboardStateNotSchedulable: 1,
			},
		},
		{
			name: "effective unschedulable account with missing evidence is not_schedulable",
			account: func() Account {
				acc := formalPoolDashboardTestAccount(18, FormalPoolStageProduction)
				acc.TempUnschedulableUntil = formalPoolDashboardPtrTime(time.Now().Add(time.Hour))
				delete(acc.Extra, FormalPoolExtraHealthcheckRawRef)
				return acc
			}(),
			runtime:   formalPoolDashboardCompleteRuntime(18),
			wantState: FormalPoolDashboardStateNotSchedulable,
			wantCounts: map[string]int{
				FormalPoolDashboardStateNotSchedulable: 1,
			},
		},
		{
			name: "missing persisted healthcheck or runtime evidence is evidence_missing",
			account: func() Account {
				acc := formalPoolDashboardTestAccount(9, FormalPoolStageLegacyUnknown)
				delete(acc.Extra, FormalPoolExtraHealthcheckRawRef)
				return acc
			}(),
			runtime:   formalPoolDashboardCompleteRuntime(9),
			wantState: FormalPoolDashboardStateEvidenceMissing,
			wantCounts: map[string]int{
				FormalPoolDashboardStateEvidenceMissing: 1,
			},
		},
		{
			name: "enabled runtime metric without a read value is data_missing",
			account: func() Account {
				acc := formalPoolDashboardTestAccount(10, FormalPoolStageProduction)
				acc.Extra["base_rpm"] = 60
				return acc
			}(),
			runtime: FormalPoolStatusRuntimeSnapshot{
				GeneratedAt:           now,
				ConcurrencyAvailable:  true,
				ConcurrencyByAccount:  map[int64]int{10: 0},
				RPMAvailable:          true,
				RPMByAccount:          map[int64]int{},
				SessionCountAvailable: true,
				SessionsByAccount:     map[int64]int{},
				WindowCostAvailable:   true,
				WindowCostByAccount:   map[int64]float64{},
			},
			wantState: FormalPoolDashboardStateDataMissing,
			wantCounts: map[string]int{
				FormalPoolDashboardStateDataMissing: 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dashboard := BuildFormalPoolStatusDashboard([]Account{tt.account}, tt.runtime)
			require.Len(t, dashboard.Accounts, 1)
			row := dashboard.Accounts[0]
			require.Equal(t, tt.wantState, row.State)
			require.NotEmpty(t, row.StateLabel)
			require.NotEmpty(t, row.StateSeverity)
			require.NotEmpty(t, row.Recommendation.Label)
			require.NotEqual(t, FormalPoolDashboardStateNormal, row.State, "only explicit normal/production cases may be normal")
			for state, want := range tt.wantCounts {
				require.Equal(t, want, formalPoolDashboardSummaryCount(dashboard.Summary, state), "summary count for %s", state)
			}
		})
	}
}

func TestFormalPoolStatusDashboard_ManualRiskSourcesIncludeOnboardingBucket(t *testing.T) {
	for _, bucket := range []string{"status_401", "status_403"} {
		t.Run(bucket, func(t *testing.T) {
			acc := formalPoolDashboardTestAccount(51, FormalPoolStageProduction)
			acc.Extra[FormalPoolExtraOnboardingLastErrorBucket] = bucket

			dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, formalPoolDashboardCompleteRuntime(51))

			require.Equal(t, FormalPoolDashboardStateManualRisk, dashboard.Accounts[0].State)
			require.Equal(t, 1, dashboard.Summary.ManualRisk)
		})
	}
}

func TestFormalPoolStatusDashboard_ManualRiskSourcesIncludeBareRisk(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Account)
	}{
		{
			name: "healthcheck safe bucket is bare risk",
			mutate: func(acc *Account) {
				acc.Extra[FormalPoolExtraHealthcheckSafeErrorBucket] = "risk"
			},
		},
		{
			name: "onboarding bucket is bare risk",
			mutate: func(acc *Account) {
				acc.Extra[FormalPoolExtraOnboardingLastErrorBucket] = "risk"
			},
		},
		{
			name: "error message is bare risk",
			mutate: func(acc *Account) {
				acc.ErrorMessage = "risk"
			},
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := int64(55 + i)
			acc := formalPoolDashboardTestAccount(id, FormalPoolStageProduction)
			tt.mutate(&acc)

			dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, formalPoolDashboardCompleteRuntime(id))

			require.Equal(t, FormalPoolDashboardStateManualRisk, dashboard.Accounts[0].State)
			require.Equal(t, 1, dashboard.Summary.ManualRisk)
		})
	}
}

func TestFormalPoolStatusDashboard_InactiveOutranksStaleRiskAndRateLimitSignals(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Account)
	}{
		{
			name: "disabled with stale 429",
			mutate: func(acc *Account) {
				acc.Status = StatusDisabled
				acc.Extra[FormalPoolExtraOnboardingLastErrorBucket] = "status_429"
			},
		},
		{
			name: "disabled with stale 403",
			mutate: func(acc *Account) {
				acc.Status = StatusDisabled
				acc.Extra[FormalPoolExtraOnboardingLastErrorBucket] = "status_403"
			},
		},
		{
			name: "inactive with invalid auth",
			mutate: func(acc *Account) {
				acc.Status = "inactive"
				acc.Extra[FormalPoolExtraLastFailureCode] = "invalid_auth"
			},
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := int64(110 + i)
			acc := formalPoolDashboardTestAccount(id, FormalPoolStageProduction)
			tt.mutate(&acc)

			dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, formalPoolDashboardCompleteRuntime(id))

			require.Equal(t, FormalPoolDashboardStateInactive, dashboard.Accounts[0].State)
			require.Equal(t, 1, dashboard.Summary.Inactive)
			require.Equal(t, 0, dashboard.Summary.ManualRisk)
			require.Equal(t, 0, dashboard.Summary.RateLimited)
		})
	}
}

func TestFormalPoolStatusDashboard_ManualRiskSourcesIncludeIdentityBoundarySignals(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Account)
	}{
		{
			name: "missing account identity",
			mutate: func(acc *Account) {
				acc.Extra[FormalPoolExtraLastCCGatewayErrorCode] = "missing_account_identity"
			},
		},
		{
			name: "missing egress bucket",
			mutate: func(acc *Account) {
				acc.Extra[FormalPoolExtraLastFailureCode] = "missing_egress_bucket"
			},
		},
		{
			name: "proxy mismatch",
			mutate: func(acc *Account) {
				acc.Extra[FormalPoolExtraHealthcheckSafeErrorCode] = "proxy_mismatch"
			},
		},
		{
			name: "direct fallback",
			mutate: func(acc *Account) {
				acc.Extra[FormalPoolExtraHealthcheckSafeErrorCode] = "direct_fallback"
			},
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := int64(120 + i)
			acc := formalPoolDashboardTestAccount(id, FormalPoolStageProduction)
			tt.mutate(&acc)

			dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, formalPoolDashboardCompleteRuntime(id))

			require.Equal(t, FormalPoolDashboardStateManualRisk, dashboard.Accounts[0].State)
			require.Equal(t, 1, dashboard.Summary.ManualRisk)
		})
	}
}

func TestFormalPoolStatusDashboard_PassThroughUsageCredit429IsNotAccountRateLimited(t *testing.T) {
	acc := formalPoolDashboardTestAccount(130, FormalPoolStageProduction)
	acc.Extra[FormalPoolExtraRateLimitAction] = "pass_through"
	acc.Extra[FormalPoolExtraRateLimitErrorClass] = "long_context_usage_credits"
	acc.Extra[FormalPoolExtraHealthcheckStatusCodeBucket] = "status_429"
	acc.ErrorMessage = "Usage credits are required for long context requests"

	dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, formalPoolDashboardCompleteRuntime(130))

	require.Equal(t, FormalPoolDashboardStateNotSchedulable, dashboard.Accounts[0].State)
	require.Equal(t, 0, dashboard.Summary.RateLimited)
}

func TestFormalPoolStatusDashboard_WindowRejectedWithFutureResetIsRateLimited(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	reset := now.Add(2 * time.Hour)
	acc := formalPoolDashboardTestAccount(131, FormalPoolStageProduction)
	acc.SessionWindowStatus = "rejected"
	acc.SessionWindowEnd = &reset

	dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, FormalPoolStatusRuntimeSnapshot{
		GeneratedAt:           now,
		ConcurrencyAvailable:  true,
		ConcurrencyByAccount:  map[int64]int{131: 0},
		RPMAvailable:          true,
		RPMByAccount:          map[int64]int{131: 0},
		SessionCountAvailable: true,
		SessionsByAccount:     map[int64]int{131: 0},
		WindowCostAvailable:   true,
		WindowCostByAccount:   map[int64]float64{131: 0},
	})

	require.Equal(t, FormalPoolDashboardStateRateLimited, dashboard.Accounts[0].State)
	require.Equal(t, 1, dashboard.Summary.RateLimited)
}

func TestFormalPoolStatusDashboard_ManualRiskSourcesIncludeHealthcheckBoundaryBooleans(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Account)
	}{
		{
			name: "fallback boolean",
			mutate: func(acc *Account) {
				acc.Extra[FormalPoolExtraHealthcheckFallbackDetected] = true
			},
		},
		{
			name: "proxy mismatch boolean",
			mutate: func(acc *Account) {
				acc.Extra[FormalPoolExtraHealthcheckProxyMismatch] = true
			},
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := int64(140 + i)
			acc := formalPoolDashboardTestAccount(id, FormalPoolStageProduction)
			tt.mutate(&acc)

			dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, formalPoolDashboardCompleteRuntime(id))

			require.Equal(t, FormalPoolDashboardStateManualRisk, dashboard.Accounts[0].State)
			require.Equal(t, 1, dashboard.Summary.ManualRisk)
		})
	}
}

func TestFormalPoolStatusDashboard_UsageCredit429WithoutCooldownSignalIsNotRateLimited(t *testing.T) {
	acc := formalPoolDashboardTestAccount(142, FormalPoolStageProduction)
	acc.Extra[FormalPoolExtraRateLimitErrorClass] = "long_context_usage_credits"
	acc.Extra[FormalPoolExtraHealthcheckStatusCodeBucket] = "status_429"
	acc.ErrorMessage = "Usage credits are required for long context requests"

	dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, formalPoolDashboardCompleteRuntime(142))

	require.Equal(t, FormalPoolDashboardStateNotSchedulable, dashboard.Accounts[0].State)
	require.Equal(t, 0, dashboard.Summary.RateLimited)
}

func TestFormalPoolStatusDashboard_ManualRiskSourcesIncludeAuthAndForbiddenText(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Account)
	}{
		{
			name: "last failure code invalid auth",
			mutate: func(acc *Account) {
				acc.Extra[FormalPoolExtraLastFailureCode] = "invalid_auth"
			},
		},
		{
			name: "cc gateway error unauthorized",
			mutate: func(acc *Account) {
				acc.Extra[FormalPoolExtraLastCCGatewayErrorCode] = "unauthorized"
			},
		},
		{
			name: "error message forbidden",
			mutate: func(acc *Account) {
				acc.ErrorMessage = "upstream forbidden response"
			},
		},
		{
			name: "healthcheck safe code authentication error",
			mutate: func(acc *Account) {
				acc.Extra[FormalPoolExtraHealthcheckSafeErrorCode] = "authentication_error"
			},
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := int64(90 + i)
			acc := formalPoolDashboardTestAccount(id, FormalPoolStageProduction)
			tt.mutate(&acc)

			dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, formalPoolDashboardCompleteRuntime(id))

			require.Equal(t, FormalPoolDashboardStateManualRisk, dashboard.Accounts[0].State)
			require.Equal(t, 1, dashboard.Summary.ManualRisk)
		})
	}
}

func TestFormalPoolStatusDashboard_HistoricalRateLimitTextDoesNotDriveCurrentRateLimit(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Account)
	}{
		{
			name: "last failure code too many requests",
			mutate: func(acc *Account) {
				acc.Extra[FormalPoolExtraLastFailureCode] = "too_many_requests"
			},
		},
		{
			name: "error message too many requests",
			mutate: func(acc *Account) {
				acc.ErrorMessage = "Too Many Requests from upstream"
			},
		},
		{
			name: "healthcheck safe bucket quota exceeded",
			mutate: func(acc *Account) {
				acc.Extra[FormalPoolExtraHealthcheckSafeErrorBucket] = "quota_exceeded"
			},
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := int64(100 + i)
			acc := formalPoolDashboardTestAccount(id, FormalPoolStageProduction)
			tt.mutate(&acc)

			dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, formalPoolDashboardCompleteRuntime(id))

			require.Equal(t, FormalPoolDashboardStateProduction, dashboard.Accounts[0].State)
			require.Equal(t, 0, dashboard.Summary.RateLimited)
		})
	}
}

func TestFormalPoolStatusDashboard_HistoricalErrorMessageAndOnboardingDoNotDriveCurrentRateLimit(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Account)
	}{
		{
			name: "error message contains 429",
			mutate: func(acc *Account) {
				acc.ErrorMessage = "upstream returned 429 Too Many Requests"
			},
		},
		{
			name: "onboarding bucket contains 429",
			mutate: func(acc *Account) {
				acc.Extra[FormalPoolExtraOnboardingLastErrorBucket] = "status_429"
			},
		},
		{
			name: "onboarding code contains rate limit",
			mutate: func(acc *Account) {
				acc.Extra[FormalPoolExtraOnboardingLastErrorCode] = "rate_limit_exceeded"
			},
		},
		{
			name: "onboarding bucket contains rate limit",
			mutate: func(acc *Account) {
				acc.Extra[FormalPoolExtraOnboardingLastErrorBucket] = "rate_limited"
			},
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := int64(61 + i)
			acc := formalPoolDashboardTestAccount(id, FormalPoolStageProduction)
			tt.mutate(&acc)

			dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, formalPoolDashboardCompleteRuntime(id))

			require.Equal(t, FormalPoolDashboardStateProduction, dashboard.Accounts[0].State)
			require.Equal(t, 0, dashboard.Summary.RateLimited)
		})
	}
}

func TestFormalPoolStatusDashboard_RateLimitCurrentSignals(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)

	t.Run("future RateLimitResetAt is rate limited", func(t *testing.T) {
		acc := formalPoolDashboardTestAccount(260, FormalPoolStageProduction)
		reset := now.Add(time.Hour)
		acc.RateLimitResetAt = &reset
		dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, formalPoolDashboardCompleteRuntimeAt(260, now))

		require.Equal(t, FormalPoolDashboardStateRateLimited, dashboard.Accounts[0].State)
		require.Equal(t, 1, dashboard.Summary.RateLimited)
	})

	t.Run("expired reset with old action and last_at is not rate limited", func(t *testing.T) {
		acc := formalPoolDashboardTestAccount(261, FormalPoolStageProduction)
		reset := now.Add(-22 * time.Hour)
		acc.RateLimitResetAt = &reset
		acc.Extra[FormalPoolExtraRateLimitAction] = "rate_limited"
		acc.Extra[FormalPoolExtraRateLimitLastAt] = now.Add(-23 * time.Hour).Format(time.RFC3339)
		dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, formalPoolDashboardCompleteRuntimeAt(261, now))

		require.Equal(t, FormalPoolDashboardStateProduction, dashboard.Accounts[0].State)
		require.Equal(t, 0, dashboard.Summary.RateLimited)
	})

	t.Run("recent fallback rate limited action is rate limited", func(t *testing.T) {
		acc := formalPoolDashboardTestAccount(264, FormalPoolStageProduction)
		acc.Extra[FormalPoolExtraRateLimitAction] = "fallback_rate_limited"
		acc.Extra[FormalPoolExtraRateLimitLastAt] = now.Add(-30 * time.Minute).Format(time.RFC3339)
		dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, formalPoolDashboardCompleteRuntimeAt(264, now))

		require.Equal(t, FormalPoolDashboardStateRateLimited, dashboard.Accounts[0].State)
		require.Equal(t, 1, dashboard.Summary.RateLimited)
	})

	t.Run("historical onboarding 429 without active cooldown is not rate limited", func(t *testing.T) {
		acc := formalPoolDashboardTestAccount(262, FormalPoolStageProduction)
		acc.Extra[FormalPoolExtraOnboardingLastErrorBucket] = "status_429"
		acc.Extra[FormalPoolExtraOnboardingLastErrorCode] = "rate_limited"
		dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, formalPoolDashboardCompleteRuntimeAt(262, now))

		require.Equal(t, FormalPoolDashboardStateProduction, dashboard.Accounts[0].State)
		require.Equal(t, 0, dashboard.Summary.RateLimited)
		require.Equal(t, "rate_limited", dashboard.Accounts[0].LastFailureCode)
		require.Equal(t, "status_429", dashboard.Accounts[0].LastFailureBucket)
	})

	t.Run("current rejected session window before end is rate limited", func(t *testing.T) {
		acc := formalPoolDashboardTestAccount(263, FormalPoolStageProduction)
		windowEnd := now.Add(30 * time.Minute)
		acc.SessionWindowStatus = "rejected"
		acc.SessionWindowEnd = &windowEnd
		dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, formalPoolDashboardCompleteRuntimeAt(263, now))

		require.Equal(t, FormalPoolDashboardStateRateLimited, dashboard.Accounts[0].State)
		require.Equal(t, 1, dashboard.Summary.RateLimited)
	})
}

func TestFormalPoolStatusDashboard_PassThroughActionIsNotRateLimitedWithoutCooldownSignal(t *testing.T) {
	for _, action := range []string{"pass_through", "passthrough"} {
		t.Run(action, func(t *testing.T) {
			acc := formalPoolDashboardTestAccount(66, FormalPoolStageProduction)
			acc.Extra[FormalPoolExtraRateLimitAction] = action

			dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, formalPoolDashboardCompleteRuntime(66))

			require.Equal(t, FormalPoolDashboardStateProduction, dashboard.Accounts[0].State)
			require.Equal(t, 0, dashboard.Summary.RateLimited)
		})
	}
}

func TestFormalPoolStatusDashboard_PassThroughActionWithHistorical429IsNotRateLimited(t *testing.T) {
	acc := formalPoolDashboardTestAccount(67, FormalPoolStageProduction)
	acc.Extra[FormalPoolExtraRateLimitAction] = "pass_through"
	acc.Extra[FormalPoolExtraOnboardingLastErrorBucket] = "status_429"

	dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, formalPoolDashboardCompleteRuntime(67))

	require.Equal(t, FormalPoolDashboardStateProduction, dashboard.Accounts[0].State)
	require.Equal(t, 0, dashboard.Summary.RateLimited)
}

func TestFormalPoolStatusDashboard_RateLimitPriority(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*Account)
		wantState string
	}{
		{
			name: "manual risk outranks rate limit",
			mutate: func(acc *Account) {
				acc.Extra[FormalPoolExtraOnboardingLastErrorBucket] = "status_403"
				acc.ErrorMessage = "upstream returned 429"
			},
			wantState: FormalPoolDashboardStateManualRisk,
		},
		{
			name: "historical 429 does not outrank quarantine",
			mutate: func(acc *Account) {
				acc.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageQuarantined
				acc.Extra[FormalPoolExtraQuarantineReason] = "reason_proxy"
				acc.Extra[FormalPoolExtraOnboardingLastErrorBucket] = "status_429"
			},
			wantState: FormalPoolDashboardStateQuarantined,
		},
		{
			name: "historical 429 does not outrank error",
			mutate: func(acc *Account) {
				acc.Status = StatusError
				acc.Extra[FormalPoolExtraOnboardingLastErrorBucket] = "status_429"
			},
			wantState: FormalPoolDashboardStateError,
		},
		{
			name: "inactive outranks stale rate limit",
			mutate: func(acc *Account) {
				acc.Status = StatusDisabled
				acc.Extra[FormalPoolExtraOnboardingLastErrorBucket] = "status_429"
			},
			wantState: FormalPoolDashboardStateInactive,
		},
		{
			name: "historical 429 does not outrank not schedulable",
			mutate: func(acc *Account) {
				acc.TempUnschedulableUntil = formalPoolDashboardPtrTime(time.Now().Add(time.Hour))
				acc.Extra[FormalPoolExtraOnboardingLastErrorBucket] = "status_429"
			},
			wantState: FormalPoolDashboardStateNotSchedulable,
		},
		{
			name: "historical 429 does not drive rate limit for evidence missing account",
			mutate: func(acc *Account) {
				delete(acc.Extra, FormalPoolExtraHealthcheckRawRef)
				acc.Extra[FormalPoolExtraOnboardingLastErrorBucket] = "status_429"
			},
			wantState: FormalPoolDashboardStateNotSchedulable,
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := int64(71 + i)
			acc := formalPoolDashboardTestAccount(id, FormalPoolStageProduction)
			tt.mutate(&acc)

			dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, formalPoolDashboardCompleteRuntime(id))

			require.Equal(t, tt.wantState, dashboard.Accounts[0].State)
		})
	}
}

func TestFormalPoolStatusDashboard_StatePriorityFullOrder(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*Account)
		wantState string
	}{
		{
			name: "inactive outranks all lower states",
			mutate: func(acc *Account) {
				acc.Status = StatusDisabled
				acc.Extra[FormalPoolExtraHealthcheckSafeErrorCode] = "forbidden"
				acc.Extra[FormalPoolExtraOnboardingLastErrorBucket] = "status_429"
				acc.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageQuarantined
			},
			wantState: FormalPoolDashboardStateInactive,
		},
		{
			name: "manual risk outranks rate limit and quarantine",
			mutate: func(acc *Account) {
				acc.Extra[FormalPoolExtraHealthcheckSafeErrorCode] = "forbidden"
				acc.Extra[FormalPoolExtraOnboardingLastErrorBucket] = "status_429"
				acc.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageQuarantined
			},
			wantState: FormalPoolDashboardStateManualRisk,
		},
		{
			name: "historical 429 does not outrank quarantine and error",
			mutate: func(acc *Account) {
				acc.Extra[FormalPoolExtraOnboardingLastErrorBucket] = "status_429"
				acc.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageQuarantined
				acc.Status = StatusError
			},
			wantState: FormalPoolDashboardStateQuarantined,
		},
		{
			name: "quarantine outranks error and not schedulable",
			mutate: func(acc *Account) {
				acc.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageQuarantined
				acc.Status = StatusError
				acc.TempUnschedulableUntil = formalPoolDashboardPtrTime(time.Now().Add(time.Hour))
			},
			wantState: FormalPoolDashboardStateQuarantined,
		},
		{
			name: "error outranks not schedulable and evidence missing",
			mutate: func(acc *Account) {
				acc.Status = StatusError
				acc.TempUnschedulableUntil = formalPoolDashboardPtrTime(time.Now().Add(time.Hour))
				delete(acc.Extra, FormalPoolExtraHealthcheckRawRef)
			},
			wantState: FormalPoolDashboardStateError,
		},
		{
			name: "not schedulable outranks evidence missing",
			mutate: func(acc *Account) {
				acc.TempUnschedulableUntil = formalPoolDashboardPtrTime(time.Now().Add(time.Hour))
				delete(acc.Extra, FormalPoolExtraHealthcheckRawRef)
			},
			wantState: FormalPoolDashboardStateNotSchedulable,
		},
		{
			name: "evidence missing outranks runtime data missing for legacy schedulable stage",
			mutate: func(acc *Account) {
				acc.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageLegacyUnknown
				acc.Extra[FormalPoolExtraHealthcheckStatus] = "failed"
				acc.Extra[FormalPoolExtraHealthcheckRawRef] = "hmac-sha256:" + strings.Repeat("b", 64)
				acc.Extra["base_rpm"] = 60
			},
			wantState: FormalPoolDashboardStateEvidenceMissing,
		},
		{
			name: "data missing outranks warming",
			mutate: func(acc *Account) {
				acc.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageWarming
				acc.Extra["base_rpm"] = 60
			},
			wantState: FormalPoolDashboardStateDataMissing,
		},
		{
			name: "warming outranks production fallback",
			mutate: func(acc *Account) {
				acc.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageWarming
			},
			wantState: FormalPoolDashboardStateWarming,
		},
		{
			name: "production outranks normal fallback",
			mutate: func(acc *Account) {
				acc.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction
			},
			wantState: FormalPoolDashboardStateProduction,
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := int64(200 + i)
			acc := formalPoolDashboardTestAccount(id, FormalPoolStageProduction)
			tt.mutate(&acc)
			runtime := formalPoolDashboardCompleteRuntime(id)
			if tt.wantState == FormalPoolDashboardStateDataMissing {
				runtime.RPMByAccount = map[int64]int{}
			}

			dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, runtime)

			require.Equal(t, tt.wantState, dashboard.Accounts[0].State)
		})
	}
}

func TestFormalPoolStatusDashboard_LastFailureBucketPrefersExplicitStatusBucket(t *testing.T) {
	for _, bucket := range []string{"status_401", "status_403", "status_429"} {
		t.Run(bucket, func(t *testing.T) {
			acc := formalPoolDashboardTestAccount(220, FormalPoolStageProduction)
			acc.Extra[FormalPoolExtraLastFailureCode] = "formal_pool_healthcheck_failed"
			acc.Extra[FormalPoolExtraHealthcheckStatusCodeBucket] = bucket
			acc.Extra[FormalPoolExtraHealthcheckSafeErrorBucket] = "auth"

			dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, formalPoolDashboardCompleteRuntime(220))

			require.Equal(t, bucket, dashboard.Accounts[0].LastFailureBucket)
		})
	}
}

func TestFormalPoolStatusDashboard_PassThroughNoReset429IsNotRateLimited(t *testing.T) {
	acc := formalPoolDashboardTestAccount(230, FormalPoolStageProduction)
	acc.Extra[FormalPoolExtraRateLimitErrorClass] = "unknown"
	acc.Extra[FormalPoolExtraRateLimitWindow] = "no_reset"
	acc.Extra[FormalPoolExtraRateLimitAction] = "pass_through"
	acc.Extra[FormalPoolExtraRateLimitResetBucket] = "missing"
	acc.Extra[FormalPoolExtraOnboardingLastErrorCode] = "unknown"
	acc.Extra[FormalPoolExtraOnboardingLastErrorBucket] = "status_429"

	dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, formalPoolDashboardCompleteRuntime(230))

	require.Equal(t, FormalPoolDashboardStateProduction, dashboard.Accounts[0].State)
	require.Equal(t, 0, dashboard.Summary.RateLimited)
}

func TestFormalPoolStatusDashboard_IncludesPassiveUsageFromExtra(t *testing.T) {
	reset5h := time.Date(2026, 6, 1, 16, 0, 0, 0, time.UTC)
	reset7d := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	sampled := "2026-06-01T12:34:56Z"
	acc := formalPoolDashboardTestAccount(240, FormalPoolStageProduction)
	acc.SessionWindowEnd = &reset5h
	acc.SessionWindowStatus = "allowed"
	acc.Extra["session_window_utilization"] = "0.42"
	acc.Extra["passive_usage_7d_utilization"] = json.Number("91%")
	acc.Extra["passive_usage_7d_reset"] = reset7d.Unix()
	acc.Extra["passive_usage_sampled_at"] = sampled

	dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, formalPoolDashboardCompleteRuntime(240))

	require.Len(t, dashboard.Accounts, 1)
	row := dashboard.Accounts[0]
	require.True(t, row.PassiveUsage5h.Available)
	require.InDelta(t, 0.42, *row.PassiveUsage5h.Utilization, 0.0001)
	require.InDelta(t, 0.58, *row.PassiveUsage5h.RemainingRatio, 0.0001)
	require.Equal(t, &reset5h, row.PassiveUsage5h.ResetAt)
	require.NotNil(t, row.PassiveUsage5h.SampledAt)
	require.Equal(t, sampled, row.PassiveUsage5h.SampledAt.Format(time.RFC3339))
	require.Equal(t, "allowed", row.PassiveUsage5h.Status)

	require.True(t, row.PassiveUsage7d.Available)
	require.InDelta(t, 0.91, *row.PassiveUsage7d.Utilization, 0.0001)
	require.InDelta(t, 0.09, *row.PassiveUsage7d.RemainingRatio, 0.0001)
	require.Equal(t, &reset7d, row.PassiveUsage7d.ResetAt)
	require.NotNil(t, row.PassiveUsage7d.SampledAt)
	require.Equal(t, sampled, row.PassiveUsage7d.SampledAt.Format(time.RFC3339))
	require.Equal(t, "sampled", row.PassiveUsage7d.Status)
}

func TestFormalPoolStatusDashboard_PassiveUsageClampsAndMarksMissingDataUnavailable(t *testing.T) {
	accWithData := formalPoolDashboardTestAccount(241, FormalPoolStageProduction)
	accWithData.Extra["session_window_utilization"] = json.Number("1.25")
	accWithData.Extra["passive_usage_7d_utilization"] = -0.5
	accWithoutData := formalPoolDashboardTestAccount(242, FormalPoolStageProduction)
	accWithoutData.Extra["passive_usage_sampled_at"] = "not-a-time"

	dashboard := BuildFormalPoolStatusDashboard([]Account{accWithData, accWithoutData}, formalPoolDashboardCompleteRuntime(241))

	require.Len(t, dashboard.Accounts, 2)
	require.True(t, dashboard.Accounts[0].PassiveUsage5h.Available)
	require.InDelta(t, 1, *dashboard.Accounts[0].PassiveUsage5h.Utilization, 0.0001)
	require.InDelta(t, 0, *dashboard.Accounts[0].PassiveUsage5h.RemainingRatio, 0.0001)
	require.False(t, dashboard.Accounts[0].PassiveUsage7d.Available)
	require.Nil(t, dashboard.Accounts[0].PassiveUsage7d.Utilization)
	require.Nil(t, dashboard.Accounts[0].PassiveUsage7d.RemainingRatio)
	require.Equal(t, "not_sampled", dashboard.Accounts[0].PassiveUsage7d.Status)
	require.False(t, dashboard.Accounts[1].PassiveUsage5h.Available)
	require.Nil(t, dashboard.Accounts[1].PassiveUsage5h.Utilization)
	require.Nil(t, dashboard.Accounts[1].PassiveUsage5h.RemainingRatio)
	require.Equal(t, "not_sampled", dashboard.Accounts[1].PassiveUsage5h.Status)
}

func TestFormalPoolStatusDashboard_PassiveUsageRejectsNonFiniteRatios(t *testing.T) {
	acc := formalPoolDashboardTestAccount(243, FormalPoolStageProduction)
	acc.Extra["session_window_utilization"] = "NaN"
	acc.Extra["passive_usage_7d_utilization"] = "Inf"

	dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, formalPoolDashboardCompleteRuntime(243))

	require.Len(t, dashboard.Accounts, 1)
	require.False(t, dashboard.Accounts[0].PassiveUsage5h.Available)
	require.Nil(t, dashboard.Accounts[0].PassiveUsage5h.Utilization)
	require.Nil(t, dashboard.Accounts[0].PassiveUsage5h.RemainingRatio)
	require.False(t, dashboard.Accounts[0].PassiveUsage7d.Available)
	require.Nil(t, dashboard.Accounts[0].PassiveUsage7d.Utilization)
	require.Nil(t, dashboard.Accounts[0].PassiveUsage7d.RemainingRatio)
}

func TestFormalPoolStatusDashboard_PassiveUsageSummaryAggregatesRemainingRatios(t *testing.T) {
	acc1 := formalPoolDashboardTestAccount(250, FormalPoolStageProduction)
	acc1.Extra["session_window_utilization"] = 0.20
	acc1.Extra["passive_usage_7d_utilization"] = "0.40"
	acc2 := formalPoolDashboardTestAccount(251, FormalPoolStageProduction)
	acc2.Extra["session_window_utilization"] = json.Number("80%")
	acc2.Extra["passive_usage_7d_utilization"] = 0.90
	acc3 := formalPoolDashboardTestAccount(252, FormalPoolStageProduction)

	dashboard := BuildFormalPoolStatusDashboard([]Account{acc1, acc2, acc3}, formalPoolDashboardCompleteRuntime(250))

	require.True(t, dashboard.Summary.PassiveUsage5hAvailable)
	require.True(t, dashboard.Summary.PassiveUsage7dAvailable)
	require.NotNil(t, dashboard.Summary.PassiveUsage5hRemainingRatio)
	require.NotNil(t, dashboard.Summary.PassiveUsage7dRemainingRatio)
	require.InDelta(t, 0.50, *dashboard.Summary.PassiveUsage5hRemainingRatio, 0.0001)
	require.InDelta(t, 0.35, *dashboard.Summary.PassiveUsage7dRemainingRatio, 0.0001)
}

func TestFormalPoolStatusDashboard_NormalLegacyRequiresCompleteEvidenceAndRuntime(t *testing.T) {
	acc := formalPoolDashboardTestAccount(21, FormalPoolStageLegacyUnknown)
	dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, formalPoolDashboardCompleteRuntime(21))

	require.Len(t, dashboard.Accounts, 1)
	require.Equal(t, FormalPoolDashboardStateNormal, dashboard.Accounts[0].State)
	require.Equal(t, 1, dashboard.Summary.Normal)
}

func TestFormalPoolStatusDashboard_AllowsOperationalEmailAccountLabel(t *testing.T) {
	acc := formalPoolDashboardTestAccount(30, FormalPoolStageProduction)
	acc.Name = "ops-user@example.com"

	dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, formalPoolDashboardCompleteRuntime(30))

	require.Equal(t, "ops-user@example.com", dashboard.Accounts[0].AccountLabel)
}

func TestFormalPoolStatusDashboard_AllowsOperationalSetupNamedAccountLabel(t *testing.T) {
	acc := formalPoolDashboardTestAccount(34, FormalPoolStageProduction)
	acc.Name = "setup max 01"

	dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, formalPoolDashboardCompleteRuntime(34))

	require.Equal(t, "setup max 01", dashboard.Accounts[0].AccountLabel)
}

func TestFormalPoolStatusDashboard_SanitizesSchemeLessUserinfoAccountLabel(t *testing.T) {
	acc := formalPoolDashboardTestAccount(33, FormalPoolStageProduction)
	acc.Name = "proxyuser:secret@example.com"

	dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, formalPoolDashboardCompleteRuntime(33))
	body := fmt.Sprintf("%+v", dashboard)

	require.Equal(t, "账号 #33", dashboard.Accounts[0].AccountLabel)
	require.NotContains(t, body, "proxyuser:secret@example.com")
}

func TestFormalPoolStatusDashboard_SanitizesUnsafeAccountLabelsAndFields(t *testing.T) {
	acc := formalPoolDashboardTestAccount(31, FormalPoolStageProduction)
	acc.Name = "ops-user@example.com sk-ant-secret 123e4567-e89b-12d3-a456-426614174000 http://proxy-user:proxy-pass@example.com"
	acc.Credentials = map[string]any{"access_token": "access-secret", "refresh_token": "refresh-secret"}
	acc.Proxy = &Proxy{Host: "proxy-secret.example.com", Username: "proxy-user-secret", Password: "proxy-pass-secret"}
	acc.ErrorMessage = "raw body secret"

	dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, formalPoolDashboardCompleteRuntime(31))
	body := fmt.Sprintf("%+v", dashboard)

	require.Equal(t, "账号 #31", dashboard.Accounts[0].AccountLabel)
	for _, unsafe := range []string{"sk-ant-secret", "123e4567-e89b-12d3-a456-426614174000", "proxy-user", "proxy-pass", "access-secret", "refresh-secret", "proxy-secret.example.com"} {
		require.NotContains(t, body, unsafe)
	}
}

func TestFormalPoolStatusDashboard_LastFailureCodePrefersSpecificSafeCodeOverGenericHealthcheckFailure(t *testing.T) {
	acc := formalPoolDashboardTestAccount(231, FormalPoolStageProduction)
	acc.Extra[FormalPoolExtraLastFailureCode] = "formal_pool_healthcheck_failed"
	acc.Extra[FormalPoolExtraHealthcheckSafeErrorCode] = "forbidden"

	dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, formalPoolDashboardCompleteRuntime(231))

	require.Equal(t, "forbidden", dashboard.Accounts[0].LastFailureCode)
}

func TestFormalPoolStatusDashboard_RedactsSensitiveFailureFields(t *testing.T) {
	acc := formalPoolDashboardTestAccount(32, FormalPoolStageProduction)
	acc.Extra[FormalPoolExtraLastFailureCode] = `sk-ant-secret access_token=access-secret raw body {"prompt":"secret"}`
	acc.Extra[FormalPoolExtraOnboardingLastErrorBucket] = "proxy_password=proxy-pass raw body telemetry"

	dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, formalPoolDashboardCompleteRuntime(32))
	body := fmt.Sprintf("%+v", dashboard)

	require.Equal(t, "redacted_sensitive", dashboard.Accounts[0].LastFailureCode)
	require.Equal(t, "redacted_sensitive", dashboard.Accounts[0].LastFailureBucket)
	for _, unsafe := range []string{"sk-ant-secret", "access-secret", "raw body", `"prompt"`, "proxy-pass", "telemetry"} {
		require.NotContains(t, body, unsafe)
	}
}

func TestFormalPoolStatusDashboardService_IncludesLegacyRuntimeSetupTokenAndExcludesOrdinarySetupToken(t *testing.T) {
	legacyRuntime := formalPoolDashboardLegacyRuntimeCandidate(300, AccountTypeSetupToken)
	legacyRuntime.Name = "anthropic-setup-204.1.108.104"
	ordinary := Account{
		ID:          301,
		Name:        "ordinary setup token",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeSetupToken,
		Status:      StatusActive,
		Schedulable: true,
		Extra:       map[string]any{FormalPoolExtraOnboardingStage: nil},
	}
	recognized := formalPoolDashboardTestAccount(302, FormalPoolStageProduction)
	legacyOAuthRuntime := formalPoolDashboardLegacyRuntimeCandidate(303, AccountTypeOAuth)
	legacyOAuthRuntime.Name = "oauth legacy runtime"

	require.False(t, IsFormalPoolAccount(&legacyRuntime), "dashboard fallback must not alter global formal-pool gates")
	require.False(t, IsFormalPoolAccount(&legacyOAuthRuntime), "dashboard fallback must not alter global formal-pool gates")
	require.False(t, IsFormalPoolAccount(&ordinary), "ordinary setup-token accounts without lifecycle stage remain non-formal globally")

	lister := &formalPoolDashboardPagedLister{accounts: []Account{legacyRuntime, ordinary, recognized, legacyOAuthRuntime}}
	svc := NewFormalPoolStatusDashboardService(FormalPoolStatusDashboardDeps{
		Accounts: lister,
		RPM:      formalPoolDashboardStaticRPMReader{counts: map[int64]int{300: 0, 303: 0}},
	})

	dashboard, err := svc.Build(context.Background())

	require.NoError(t, err)
	require.Len(t, dashboard.Accounts, 3)
	require.Equal(t, []int64{300, 302, 303}, []int64{dashboard.Accounts[0].AccountID, dashboard.Accounts[1].AccountID, dashboard.Accounts[2].AccountID})
	require.Equal(t, FormalPoolStageLegacyUnknown, dashboard.Accounts[0].Stage)
	require.Equal(t, FormalPoolStageLegacyUnknown, dashboard.Accounts[2].Stage)
}

func TestFormalPoolStatusDashboardService_RejectsLegacyRuntimeFallbackWithPartialMarkers(t *testing.T) {
	tests := []struct {
		name  string
		extra map[string]any
	}{
		{name: "enabled and base rpm only", extra: map[string]any{"base_rpm": 60}},
		{name: "enabled and account ref only", extra: map[string]any{ccGatewayExtraAccountRef: "hmac-sha256:" + strings.Repeat("c", 64)}},
		{name: "enabled and egress bucket only", extra: map[string]any{ccGatewayExtraEgressBucket: "bucket-safe"}},
		{name: "enabled base rpm and account ref missing bucket", extra: map[string]any{"base_rpm": 60, ccGatewayExtraAccountRef: "hmac-sha256:" + strings.Repeat("d", 64)}},
		{name: "enabled base rpm and bucket missing ref", extra: map[string]any{"base_rpm": 60, ccGatewayExtraEgressBucket: "bucket-safe"}},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := formalPoolDashboardLegacyRuntimePartialCandidate(int64(320+i), tt.extra)
			recognized := formalPoolDashboardTestAccount(400+int64(i), FormalPoolStageProduction)
			lister := &formalPoolDashboardPagedLister{accounts: []Account{candidate, recognized}}
			svc := NewFormalPoolStatusDashboardService(FormalPoolStatusDashboardDeps{
				Accounts: lister,
				RPM:      formalPoolDashboardStaticRPMReader{counts: map[int64]int{candidate.ID: 0}},
			})

			dashboard, err := svc.Build(context.Background())

			require.NoError(t, err)
			require.Len(t, dashboard.Accounts, 1)
			require.Equal(t, recognized.ID, dashboard.Accounts[0].AccountID)
		})
	}
}

func TestFormalPoolStatusDashboardService_ReturnsAllFormalPoolAccountsAcrossPages(t *testing.T) {
	accounts := make([]Account, 0, formalPoolStatusDashboardPageSize+25)
	for i := 1; i <= formalPoolStatusDashboardPageSize+25; i++ {
		accounts = append(accounts, formalPoolDashboardTestAccount(int64(i), FormalPoolStageProduction))
	}
	accounts = append(accounts, Account{ID: 99999, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive})
	lister := &formalPoolDashboardPagedLister{accounts: accounts}

	svc := NewFormalPoolStatusDashboardService(FormalPoolStatusDashboardDeps{Accounts: lister})
	dashboard, err := svc.Build(context.Background())

	require.NoError(t, err)
	require.Len(t, dashboard.Accounts, formalPoolStatusDashboardPageSize+25)
	require.GreaterOrEqual(t, lister.calls, 2, "OAuth and setup-token formal pool account types should be enumerated")
}

func TestFormalPoolStatusDashboardService_MissingConcurrencyReaderMarksLimitedAccountDataMissing(t *testing.T) {
	acc := formalPoolDashboardTestAccount(40, FormalPoolStageProduction)
	acc.Concurrency = 2
	lister := &formalPoolDashboardPagedLister{accounts: []Account{acc}}
	svc := NewFormalPoolStatusDashboardService(FormalPoolStatusDashboardDeps{
		Accounts: lister,
		Now:      func() time.Time { return time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC) },
	})

	dashboard, err := svc.Build(context.Background())

	require.NoError(t, err)
	require.Len(t, dashboard.Accounts, 1)
	require.Equal(t, FormalPoolDashboardStateDataMissing, dashboard.Accounts[0].State)
	require.False(t, dashboard.Accounts[0].Concurrency.Available)
}

func TestFormalPoolStatusDashboardService_MissingConcurrencyReaderAllowsUnlimitedAccount(t *testing.T) {
	acc := formalPoolDashboardTestAccount(42, FormalPoolStageProduction)
	acc.Concurrency = 0
	lister := &formalPoolDashboardPagedLister{accounts: []Account{acc}}
	svc := NewFormalPoolStatusDashboardService(FormalPoolStatusDashboardDeps{Accounts: lister})

	dashboard, err := svc.Build(context.Background())

	require.NoError(t, err)
	require.Len(t, dashboard.Accounts, 1)
	require.Equal(t, FormalPoolDashboardStateProduction, dashboard.Accounts[0].State)
	require.True(t, dashboard.Accounts[0].Concurrency.Available)
}

func TestFormalPoolStatusDashboardService_RuntimeReadFailureMarksDataMissing(t *testing.T) {
	acc := formalPoolDashboardTestAccount(41, FormalPoolStageProduction)
	acc.Extra["base_rpm"] = 30
	lister := &formalPoolDashboardPagedLister{accounts: []Account{acc}}
	svc := NewFormalPoolStatusDashboardService(FormalPoolStatusDashboardDeps{
		Accounts: lister,
		RPM:      formalPoolDashboardRPMReader{err: errors.New("redis unavailable")},
	})

	dashboard, err := svc.Build(context.Background())

	require.NoError(t, err)
	require.Equal(t, FormalPoolDashboardStateDataMissing, dashboard.Accounts[0].State)
	require.False(t, dashboard.Accounts[0].RPM.Available)
}

func formalPoolDashboardLegacyRuntimeCandidate(id int64, accountType string) Account {
	acc := formalPoolDashboardLegacyRuntimePartialCandidate(id, map[string]any{
		"base_rpm":                 60,
		ccGatewayExtraAccountRef:   "hmac-sha256:" + strings.Repeat("a", 64),
		ccGatewayExtraEgressBucket: "bucket-safe",
	})
	acc.Type = accountType
	return acc
}

func formalPoolDashboardLegacyRuntimePartialCandidate(id int64, extra map[string]any) Account {
	merged := map[string]any{
		FormalPoolExtraOnboardingStage: nil,
		"cc_gateway_enabled":           "true",
	}
	for k, v := range extra {
		merged[k] = v
	}
	return Account{
		ID:          id,
		Name:        fmt.Sprintf("legacy-runtime-%d", id),
		Platform:    PlatformAnthropic,
		Type:        AccountTypeSetupToken,
		Status:      StatusActive,
		Schedulable: true,
		CreatedAt:   time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		Extra:       merged,
	}
}

func formalPoolDashboardTestAccount(id int64, stage string) Account {
	stamp := "2026-06-01T11:00:00Z"
	return Account{
		ID:          id,
		Name:        fmt.Sprintf("formal-%d", id),
		Platform:    PlatformAnthropic,
		Type:        AccountTypeSetupToken,
		Status:      StatusActive,
		Schedulable: true,
		CreatedAt:   time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		Extra: map[string]any{
			FormalPoolExtraOnboardingStage:             stage,
			FormalPoolExtraRuntimeRegistered:           "true",
			FormalPoolExtraRuntimeRegisteredAt:         stamp,
			"cc_gateway_account_ref":                   "hmac-sha256:" + strings.Repeat("a", 64),
			"cc_gateway_credential_ref":                "opaque:credential-ref:v1:dashboard",
			"cc_gateway_credential_binding_hmac":       "hmac-sha256:" + strings.Repeat("c", 64),
			"cc_gateway_proxy_identity_ref":            "hmac-sha256:" + strings.Repeat("d", 64),
			"cc_gateway_persona_profile":               ccGatewayDefaultPersonaProfile,
			"claude_code_device_id":                    strings.Repeat("e", 64),
			"cc_gateway_egress_bucket_enabled":         "true",
			"cc_gateway_egress_bucket":                 "bucket-safe",
			FormalPoolExtraHealthcheckStatus:           "passed",
			FormalPoolExtraHealthcheckStatusCodeBucket: "status_2xx",
			FormalPoolExtraHealthcheckRawRef:           "hmac-sha256:" + strings.Repeat("b", 64),
			FormalPoolExtraHealthcheckCCGatewaySeen:    true,
			FormalPoolExtraHealthcheckFallbackDetected: false,
			FormalPoolExtraHealthcheckProxyMismatch:    false,
			FormalPoolExtraHealthcheckRiskTextDetected: false,
			FormalPoolExtraLastHealthcheckAt:           stamp,
		},
	}
}

func formalPoolDashboardCompleteRuntime(id int64) FormalPoolStatusRuntimeSnapshot {
	return formalPoolDashboardCompleteRuntimeAt(id, time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC))
}

func formalPoolDashboardCompleteRuntimeAt(id int64, generatedAt time.Time) FormalPoolStatusRuntimeSnapshot {
	return FormalPoolStatusRuntimeSnapshot{
		GeneratedAt:           generatedAt,
		ConcurrencyAvailable:  true,
		ConcurrencyByAccount:  map[int64]int{id: 0},
		RPMAvailable:          true,
		RPMByAccount:          map[int64]int{id: 0},
		SessionCountAvailable: true,
		SessionsByAccount:     map[int64]int{id: 0},
		WindowCostAvailable:   true,
		WindowCostByAccount:   map[int64]float64{id: 0},
	}
}

func formalPoolDashboardSummaryCount(summary FormalPoolStatusSummary, state string) int {
	switch state {
	case FormalPoolDashboardStateNormal:
		return summary.Normal
	case FormalPoolDashboardStateWarming:
		return summary.Warming
	case FormalPoolDashboardStateProduction:
		return summary.Production
	case FormalPoolDashboardStateRateLimited:
		return summary.RateLimited
	case FormalPoolDashboardStateManualRisk:
		return summary.ManualRisk
	case FormalPoolDashboardStateError:
		return summary.Error
	case FormalPoolDashboardStateQuarantined:
		return summary.Quarantined
	case FormalPoolDashboardStateInactive:
		return summary.Inactive
	case FormalPoolDashboardStateNotSchedulable:
		return summary.NotSchedulable
	case FormalPoolDashboardStateEvidenceMissing:
		return summary.EvidenceMissing
	case FormalPoolDashboardStateDataMissing:
		return summary.DataMissing
	default:
		return -1
	}
}

func formalPoolDashboardPtrTime(t time.Time) *time.Time { return &t }

type formalPoolDashboardPagedLister struct {
	accounts []Account
	calls    int
}

func (l *formalPoolDashboardPagedLister) ListAccounts(_ context.Context, page, pageSize int, platform, accountType, status, search string, groupID int64, privacyMode string, sortBy, sortOrder string) ([]Account, int64, error) {
	l.calls++
	filtered := make([]Account, 0, len(l.accounts))
	for _, acc := range l.accounts {
		if platform != "" && acc.Platform != platform {
			continue
		}
		if accountType != "" && acc.Type != accountType {
			continue
		}
		filtered = append(filtered, acc)
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	start := (page - 1) * pageSize
	if start >= len(filtered) {
		return nil, int64(len(filtered)), nil
	}
	end := start + pageSize
	if end > len(filtered) {
		end = len(filtered)
	}
	return filtered[start:end], int64(len(filtered)), nil
}

type formalPoolDashboardStaticRPMReader struct {
	counts map[int64]int
}

func (r formalPoolDashboardStaticRPMReader) IncrementRPM(context.Context, int64) (int, error) {
	return 0, nil
}
func (r formalPoolDashboardStaticRPMReader) GetRPM(_ context.Context, accountID int64) (int, error) {
	return r.counts[accountID], nil
}
func (r formalPoolDashboardStaticRPMReader) GetRPMBatch(_ context.Context, accountIDs []int64) (map[int64]int, error) {
	out := make(map[int64]int, len(accountIDs))
	for _, id := range accountIDs {
		out[id] = r.counts[id]
	}
	return out, nil
}

type formalPoolDashboardRPMReader struct{ err error }

func (r formalPoolDashboardRPMReader) IncrementRPM(context.Context, int64) (int, error) {
	return 0, r.err
}
func (r formalPoolDashboardRPMReader) GetRPM(context.Context, int64) (int, error) { return 0, r.err }
func (r formalPoolDashboardRPMReader) GetRPMBatch(context.Context, []int64) (map[int64]int, error) {
	return nil, r.err
}
