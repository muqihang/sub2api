package service

import (
	"context"
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
				acc.TempUnschedulableUntil = formalPoolDashboardPtrTime(now.Add(time.Hour))
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
				acc.TempUnschedulableUntil = formalPoolDashboardPtrTime(now.Add(time.Hour))
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

func TestFormalPoolStatusDashboard_RateLimitSourcesIncludeErrorMessageAndOnboarding(t *testing.T) {
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

			require.Equal(t, FormalPoolDashboardStateRateLimited, dashboard.Accounts[0].State)
			require.Equal(t, 1, dashboard.Summary.RateLimited)
		})
	}
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

func TestFormalPoolStatusDashboard_PassThroughActionWithExplicit429IsRateLimited(t *testing.T) {
	acc := formalPoolDashboardTestAccount(67, FormalPoolStageProduction)
	acc.Extra[FormalPoolExtraRateLimitAction] = "pass_through"
	acc.Extra[FormalPoolExtraOnboardingLastErrorBucket] = "status_429"

	dashboard := BuildFormalPoolStatusDashboard([]Account{acc}, formalPoolDashboardCompleteRuntime(67))

	require.Equal(t, FormalPoolDashboardStateRateLimited, dashboard.Accounts[0].State)
	require.Equal(t, 1, dashboard.Summary.RateLimited)
}

func TestFormalPoolStatusDashboard_RateLimitPriority(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
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
			name: "rate limit outranks quarantine",
			mutate: func(acc *Account) {
				acc.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageQuarantined
				acc.Extra[FormalPoolExtraQuarantineReason] = "reason_proxy"
				acc.Extra[FormalPoolExtraOnboardingLastErrorBucket] = "status_429"
			},
			wantState: FormalPoolDashboardStateRateLimited,
		},
		{
			name: "rate limit outranks error",
			mutate: func(acc *Account) {
				acc.Status = StatusError
				acc.Extra[FormalPoolExtraOnboardingLastErrorBucket] = "status_429"
			},
			wantState: FormalPoolDashboardStateRateLimited,
		},
		{
			name: "rate limit outranks inactive",
			mutate: func(acc *Account) {
				acc.Status = StatusDisabled
				acc.Extra[FormalPoolExtraOnboardingLastErrorBucket] = "status_429"
			},
			wantState: FormalPoolDashboardStateRateLimited,
		},
		{
			name: "rate limit outranks not schedulable",
			mutate: func(acc *Account) {
				acc.TempUnschedulableUntil = formalPoolDashboardPtrTime(now.Add(time.Hour))
				acc.Extra[FormalPoolExtraOnboardingLastErrorBucket] = "status_429"
			},
			wantState: FormalPoolDashboardStateRateLimited,
		},
		{
			name: "rate limit outranks evidence missing",
			mutate: func(acc *Account) {
				delete(acc.Extra, FormalPoolExtraHealthcheckRawRef)
				acc.Extra[FormalPoolExtraOnboardingLastErrorBucket] = "status_429"
			},
			wantState: FormalPoolDashboardStateRateLimited,
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
	return FormalPoolStatusRuntimeSnapshot{
		GeneratedAt:           time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
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

type formalPoolDashboardRPMReader struct{ err error }

func (r formalPoolDashboardRPMReader) IncrementRPM(context.Context, int64) (int, error) {
	return 0, r.err
}
func (r formalPoolDashboardRPMReader) GetRPM(context.Context, int64) (int, error) { return 0, r.err }
func (r formalPoolDashboardRPMReader) GetRPMBatch(context.Context, []int64) (map[int64]int, error) {
	return nil, r.err
}
