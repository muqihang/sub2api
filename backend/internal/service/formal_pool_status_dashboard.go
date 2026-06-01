package service

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"golang.org/x/sync/errgroup"
)

const (
	FormalPoolDashboardStateNormal          = "normal"
	FormalPoolDashboardStateWarming         = "warming"
	FormalPoolDashboardStateProduction      = "production"
	FormalPoolDashboardStateRateLimited     = "rate_limited"
	FormalPoolDashboardStateManualRisk      = "manual_risk"
	FormalPoolDashboardStateError           = "error"
	FormalPoolDashboardStateQuarantined     = "quarantined"
	FormalPoolDashboardStateInactive        = "inactive"
	FormalPoolDashboardStateNotSchedulable  = "not_schedulable"
	FormalPoolDashboardStateEvidenceMissing = "evidence_missing"
	FormalPoolDashboardStateDataMissing     = "data_missing"

	formalPoolStatusDashboardPageSize = 1000
)

// FormalPoolStatusDashboard is the sanitized backend contract for the Formal Pool status dashboard.
type FormalPoolStatusDashboard struct {
	Summary  FormalPoolStatusSummary            `json:"summary"`
	Accounts []FormalPoolStatusDashboardAccount `json:"accounts"`
}

type FormalPoolStatusSummary struct {
	Total                   int       `json:"total"`
	Normal                  int       `json:"normal"`
	Warming                 int       `json:"warming"`
	Production              int       `json:"production"`
	RateLimited             int       `json:"rate_limited"`
	ManualRisk              int       `json:"manual_risk"`
	Error                   int       `json:"error"`
	Quarantined             int       `json:"quarantined"`
	Inactive                int       `json:"inactive"`
	NotSchedulable          int       `json:"not_schedulable"`
	EvidenceMissing         int       `json:"evidence_missing"`
	DataMissing             int       `json:"data_missing"`
	Schedulable             int       `json:"schedulable"`
	TotalCurrentRPM         int       `json:"total_current_rpm"`
	TotalRPMLimit           int       `json:"total_rpm_limit"`
	RPMAvailable            bool      `json:"rpm_available"`
	FiveHourRemainingRatio  *float64  `json:"five_hour_remaining_ratio"`
	FiveHourWindowAvailable bool      `json:"five_hour_window_available"`
	GeneratedAt             time.Time `json:"generated_at"`
}

type FormalPoolStatusDashboardAccount struct {
	AccountID            int64                          `json:"account_id"`
	AccountLabel         string                         `json:"account_label"`
	Platform             string                         `json:"platform"`
	Type                 string                         `json:"type"`
	Stage                string                         `json:"stage"`
	State                string                         `json:"state"`
	StateLabel           string                         `json:"state_label"`
	StateSeverity        string                         `json:"state_severity"`
	Schedulable          bool                           `json:"schedulable"`
	EffectiveSchedulable bool                           `json:"effective_schedulable"`
	ProductionReady      bool                           `json:"production_ready"`
	FiveHourWindow       FormalPoolStatusWindow         `json:"five_hour_window"`
	RPM                  FormalPoolStatusRuntime        `json:"rpm"`
	Concurrency          FormalPoolStatusRuntime        `json:"concurrency"`
	Sessions             FormalPoolStatusRuntime        `json:"sessions"`
	LastUsedAt           *time.Time                     `json:"last_used_at"`
	LastSuccessHint      *time.Time                     `json:"last_success_hint"`
	LastFailureCode      string                         `json:"last_failure_code"`
	LastFailureBucket    string                         `json:"last_failure_bucket"`
	Recommendation       FormalPoolStatusRecommendation `json:"recommendation"`
}

type FormalPoolStatusRuntime struct {
	Current     int      `json:"current"`
	Limit       int      `json:"limit"`
	Utilization *float64 `json:"utilization"`
	Available   bool     `json:"available"`
}

type FormalPoolStatusWindow struct {
	Used        float64    `json:"used"`
	Limit       float64    `json:"limit"`
	Remaining   float64    `json:"remaining"`
	Utilization *float64   `json:"utilization"`
	ResetAt     *time.Time `json:"reset_at"`
	Status      string     `json:"status"`
	Available   bool       `json:"available"`
}

type FormalPoolStatusRecommendation struct {
	Label      string `json:"label"`
	Detail     string `json:"detail"`
	ActionKind string `json:"action_kind"`
}

// FormalPoolStatusRuntimeSnapshot contains already-sanitized runtime counters used by pure classification.
type FormalPoolStatusRuntimeSnapshot struct {
	GeneratedAt           time.Time
	ConcurrencyAvailable  bool
	ConcurrencyByAccount  map[int64]int
	RPMAvailable          bool
	RPMByAccount          map[int64]int
	SessionCountAvailable bool
	SessionsByAccount     map[int64]int
	WindowCostAvailable   bool
	WindowCostByAccount   map[int64]float64
}

type FormalPoolStatusDashboardDeps struct {
	Accounts    formalPoolStatusDashboardAccountLister
	Concurrency formalPoolStatusDashboardConcurrencyReader
	RPM         RPMCache
	Sessions    SessionLimitCache
	WindowStats formalPoolStatusDashboardWindowStatsReader
	Now         func() time.Time
}

type FormalPoolStatusDashboardService struct {
	deps FormalPoolStatusDashboardDeps
}

type formalPoolStatusDashboardAccountLister interface {
	ListAccounts(ctx context.Context, page, pageSize int, platform, accountType, status, search string, groupID int64, privacyMode string, sortBy, sortOrder string) ([]Account, int64, error)
}

type formalPoolStatusDashboardConcurrencyReader interface {
	GetAccountConcurrencyBatch(ctx context.Context, accountIDs []int64) (map[int64]int, error)
}

type formalPoolStatusDashboardWindowStatsReader interface {
	GetAccountWindowStats(ctx context.Context, accountID int64, startTime time.Time) (*usagestats.AccountStats, error)
}

func NewFormalPoolStatusDashboardService(deps FormalPoolStatusDashboardDeps) *FormalPoolStatusDashboardService {
	return &FormalPoolStatusDashboardService{deps: deps}
}

func (s *FormalPoolStatusDashboardService) Build(ctx context.Context) (*FormalPoolStatusDashboard, error) {
	if s == nil || s.deps.Accounts == nil {
		dashboard := BuildFormalPoolStatusDashboard(nil, FormalPoolStatusRuntimeSnapshot{GeneratedAt: time.Now().UTC()})
		return &dashboard, nil
	}
	accounts, err := s.listAllFormalPoolAccounts(ctx)
	if err != nil {
		return nil, err
	}
	snapshot := s.readRuntimeSnapshot(ctx, accounts)
	dashboard := BuildFormalPoolStatusDashboard(accounts, snapshot)
	return &dashboard, nil
}

func (s *FormalPoolStatusDashboardService) now() time.Time {
	if s != nil && s.deps.Now != nil {
		return s.deps.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *FormalPoolStatusDashboardService) listAllFormalPoolAccounts(ctx context.Context) ([]Account, error) {
	var out []Account
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeSetupToken} {
		for page := 1; ; page++ {
			accounts, total, err := s.deps.Accounts.ListAccounts(ctx, page, formalPoolStatusDashboardPageSize, PlatformAnthropic, accountType, "", "", 0, "", "id", "asc")
			if err != nil {
				return nil, err
			}
			for _, account := range accounts {
				if account.IsAnthropicOAuthOrSetupToken() && IsFormalPoolAccount(&account) {
					out = append(out, account)
				}
			}
			if len(accounts) == 0 || int64(page*formalPoolStatusDashboardPageSize) >= total {
				break
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *FormalPoolStatusDashboardService) readRuntimeSnapshot(ctx context.Context, accounts []Account) FormalPoolStatusRuntimeSnapshot {
	snapshot := FormalPoolStatusRuntimeSnapshot{
		GeneratedAt:          s.now(),
		ConcurrencyByAccount: map[int64]int{},
		RPMByAccount:         map[int64]int{},
		SessionsByAccount:    map[int64]int{},
		WindowCostByAccount:  map[int64]float64{},
	}
	ids := make([]int64, 0, len(accounts))
	rpmIDs := make([]int64, 0)
	sessionIDs := make([]int64, 0)
	windowIDs := make([]int64, 0)
	idleTimeouts := make(map[int64]time.Duration)
	for i := range accounts {
		acc := &accounts[i]
		ids = append(ids, acc.ID)
		if acc.GetBaseRPM() > 0 {
			rpmIDs = append(rpmIDs, acc.ID)
		}
		if acc.GetMaxSessions() > 0 {
			sessionIDs = append(sessionIDs, acc.ID)
			idleTimeouts[acc.ID] = time.Duration(acc.GetSessionIdleTimeoutMinutes()) * time.Minute
		}
		if acc.GetWindowCostLimit() > 0 {
			windowIDs = append(windowIDs, acc.ID)
		}
	}
	if len(ids) == 0 {
		snapshot.ConcurrencyAvailable = true
		snapshot.RPMAvailable = true
		snapshot.SessionCountAvailable = true
		snapshot.WindowCostAvailable = true
		return snapshot
	}
	if s.deps.Concurrency != nil {
		if counts, err := s.deps.Concurrency.GetAccountConcurrencyBatch(ctx, ids); err == nil && counts != nil {
			snapshot.ConcurrencyAvailable = true
			snapshot.ConcurrencyByAccount = counts
		}
	} else {
		// Without a configured concurrency reader, zero-concurrency rows are still unambiguous.
		snapshot.ConcurrencyAvailable = true
	}
	if len(rpmIDs) == 0 {
		snapshot.RPMAvailable = true
	} else if s.deps.RPM != nil {
		if counts, err := s.deps.RPM.GetRPMBatch(ctx, rpmIDs); err == nil && counts != nil {
			snapshot.RPMAvailable = true
			snapshot.RPMByAccount = counts
		}
	}
	if len(sessionIDs) == 0 {
		snapshot.SessionCountAvailable = true
	} else if s.deps.Sessions != nil {
		if counts, err := s.deps.Sessions.GetActiveSessionCountBatch(ctx, sessionIDs, idleTimeouts); err == nil && counts != nil {
			snapshot.SessionCountAvailable = true
			snapshot.SessionsByAccount = counts
		}
	}
	if len(windowIDs) == 0 {
		snapshot.WindowCostAvailable = true
	} else if s.deps.WindowStats != nil {
		snapshot.WindowCostAvailable = true
		var mu sync.Mutex
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(10)
		for i := range accounts {
			acc := accounts[i]
			if acc.GetWindowCostLimit() <= 0 {
				continue
			}
			g.Go(func() error {
				stats, err := s.deps.WindowStats.GetAccountWindowStats(gctx, acc.ID, acc.GetCurrentWindowStartTime())
				if err == nil && stats != nil {
					mu.Lock()
					snapshot.WindowCostByAccount[acc.ID] = stats.StandardCost
					mu.Unlock()
				}
				return nil
			})
		}
		_ = g.Wait()
	}
	return snapshot
}

func BuildFormalPoolStatusDashboard(accounts []Account, runtime FormalPoolStatusRuntimeSnapshot) FormalPoolStatusDashboard {
	if runtime.GeneratedAt.IsZero() {
		runtime.GeneratedAt = time.Now().UTC()
	}
	sort.SliceStable(accounts, func(i, j int) bool { return accounts[i].ID < accounts[j].ID })
	dashboard := FormalPoolStatusDashboard{
		Summary:  FormalPoolStatusSummary{GeneratedAt: runtime.GeneratedAt.UTC(), RPMAvailable: runtime.RPMAvailable, FiveHourWindowAvailable: runtime.WindowCostAvailable},
		Accounts: make([]FormalPoolStatusDashboardAccount, 0, len(accounts)),
	}
	var windowRemaining, windowLimit float64
	for i := range accounts {
		acc := &accounts[i]
		if !acc.IsAnthropicOAuthOrSetupToken() || !IsFormalPoolAccount(acc) {
			continue
		}
		row := buildFormalPoolStatusDashboardAccount(acc, runtime)
		dashboard.Accounts = append(dashboard.Accounts, row)
		dashboard.Summary.Total++
		if row.EffectiveSchedulable {
			dashboard.Summary.Schedulable++
		}
		switch row.State {
		case FormalPoolDashboardStateProduction:
			dashboard.Summary.Production++
			dashboard.Summary.Normal++
		case FormalPoolDashboardStateNormal:
			dashboard.Summary.Normal++
		case FormalPoolDashboardStateWarming:
			dashboard.Summary.Warming++
		case FormalPoolDashboardStateRateLimited:
			dashboard.Summary.RateLimited++
		case FormalPoolDashboardStateManualRisk:
			dashboard.Summary.ManualRisk++
		case FormalPoolDashboardStateError:
			dashboard.Summary.Error++
		case FormalPoolDashboardStateQuarantined:
			dashboard.Summary.Quarantined++
		case FormalPoolDashboardStateInactive:
			dashboard.Summary.Inactive++
		case FormalPoolDashboardStateNotSchedulable:
			dashboard.Summary.NotSchedulable++
		case FormalPoolDashboardStateEvidenceMissing:
			dashboard.Summary.EvidenceMissing++
		case FormalPoolDashboardStateDataMissing:
			dashboard.Summary.DataMissing++
		}
		if row.RPM.Available {
			dashboard.Summary.TotalCurrentRPM += row.RPM.Current
		}
		dashboard.Summary.TotalRPMLimit += row.RPM.Limit
		if row.FiveHourWindow.Available && row.FiveHourWindow.Limit > 0 {
			windowRemaining += row.FiveHourWindow.Remaining
			windowLimit += row.FiveHourWindow.Limit
		}
	}
	if windowLimit > 0 {
		ratio := clampRatio(windowRemaining / windowLimit)
		dashboard.Summary.FiveHourRemainingRatio = &ratio
	}
	return dashboard
}

func buildFormalPoolStatusDashboardAccount(account *Account, runtime FormalPoolStatusRuntimeSnapshot) FormalPoolStatusDashboardAccount {
	stage := FormalPoolAccountStage(account)
	row := FormalPoolStatusDashboardAccount{
		AccountID:            account.ID,
		AccountLabel:         safeFormalPoolAccountLabel(account),
		Platform:             account.Platform,
		Type:                 account.Type,
		Stage:                stage,
		Schedulable:          account.Schedulable,
		EffectiveSchedulable: account.IsSchedulable(),
		LastUsedAt:           account.LastUsedAt,
		LastSuccessHint:      formalPoolDashboardLastSuccessHint(account),
		LastFailureCode:      sanitizeFormalPoolDashboardFailureField(formalPoolDashboardFirstNonEmpty(account.GetExtraString(FormalPoolExtraLastFailureCode), account.GetExtraString(FormalPoolExtraHealthcheckSafeErrorCode), account.GetExtraString(FormalPoolExtraOnboardingLastErrorCode))),
		LastFailureBucket:    sanitizeFormalPoolDashboardFailureField(formalPoolDashboardFirstNonEmpty(account.GetExtraString(FormalPoolExtraOnboardingLastErrorBucket), account.GetExtraString(FormalPoolExtraHealthcheckSafeErrorBucket), account.GetExtraString(FormalPoolExtraRateLimitErrorClass))),
	}
	row.FiveHourWindow = buildFormalPoolStatusWindow(account, runtime)
	row.RPM = buildFormalPoolStatusRuntimeInt(account.GetBaseRPM(), runtime.RPMByAccount, runtime.RPMAvailable, account.ID)
	row.Concurrency = buildFormalPoolStatusRuntimeInt(account.Concurrency, runtime.ConcurrencyByAccount, runtime.ConcurrencyAvailable, account.ID)
	row.Sessions = buildFormalPoolStatusRuntimeInt(account.GetMaxSessions(), runtime.SessionsByAccount, runtime.SessionCountAvailable, account.ID)
	row.State = classifyFormalPoolDashboardState(account, row, runtime)
	row.StateLabel, row.StateSeverity = formalPoolDashboardStateLabelAndSeverity(row.State)
	row.ProductionReady = row.State == FormalPoolDashboardStateProduction
	row.Recommendation = formalPoolDashboardRecommendation(row.State)
	return row
}

func buildFormalPoolStatusRuntimeInt(limit int, values map[int64]int, sourceAvailable bool, accountID int64) FormalPoolStatusRuntime {
	r := FormalPoolStatusRuntime{Limit: limit}
	if limit <= 0 {
		r.Available = true
		return r
	}
	if sourceAvailable {
		if current, ok := values[accountID]; ok {
			r.Current = current
			r.Available = true
			r.Utilization = ratioPtr(float64(current), float64(limit))
		}
	}
	return r
}

func buildFormalPoolStatusWindow(account *Account, runtime FormalPoolStatusRuntimeSnapshot) FormalPoolStatusWindow {
	limit := account.GetWindowCostLimit()
	w := FormalPoolStatusWindow{Limit: limit, Status: account.SessionWindowStatus}
	if account.SessionWindowEnd != nil {
		w.ResetAt = account.SessionWindowEnd
	} else if account.SessionWindowStart != nil {
		reset := account.SessionWindowStart.Add(5 * time.Hour)
		w.ResetAt = &reset
	}
	if limit <= 0 {
		w.Available = true
		return w
	}
	if runtime.WindowCostAvailable {
		if used, ok := runtime.WindowCostByAccount[account.ID]; ok {
			w.Used = used
			w.Remaining = math.Max(0, limit-used)
			w.Utilization = ratioPtr(used, limit)
			w.Available = true
		}
	}
	return w
}

func classifyFormalPoolDashboardState(account *Account, row FormalPoolStatusDashboardAccount, runtime FormalPoolStatusRuntimeSnapshot) string {
	if formalPoolDashboardHasManualRisk(account) {
		return FormalPoolDashboardStateManualRisk
	}
	if formalPoolDashboardHasRateLimit(account) {
		return FormalPoolDashboardStateRateLimited
	}
	if FormalPoolAccountStage(account) == FormalPoolStageQuarantined {
		return FormalPoolDashboardStateQuarantined
	}
	if account.Status == StatusError {
		return FormalPoolDashboardStateError
	}
	if formalPoolDashboardInactive(account) {
		return FormalPoolDashboardStateInactive
	}
	if !row.EffectiveSchedulable {
		return FormalPoolDashboardStateNotSchedulable
	}
	if !formalPoolDashboardEvidenceComplete(account) {
		return FormalPoolDashboardStateEvidenceMissing
	}
	if formalPoolDashboardRuntimeDataMissing(row) {
		return FormalPoolDashboardStateDataMissing
	}
	switch FormalPoolAccountStage(account) {
	case FormalPoolStageWarming:
		return FormalPoolDashboardStateWarming
	case FormalPoolStageProduction:
		return FormalPoolDashboardStateProduction
	default:
		return FormalPoolDashboardStateNormal
	}
}

func formalPoolDashboardEvidenceComplete(account *Account) bool {
	return runtimeEvidenceComplete(account) && healthcheckEvidenceComplete(account)
}

func formalPoolDashboardRuntimeDataMissing(row FormalPoolStatusDashboardAccount) bool {
	return (row.RPM.Limit > 0 && !row.RPM.Available) ||
		(row.Concurrency.Limit > 0 && !row.Concurrency.Available) ||
		(row.Sessions.Limit > 0 && !row.Sessions.Available) ||
		(row.FiveHourWindow.Limit > 0 && !row.FiveHourWindow.Available)
}

func formalPoolDashboardInactive(account *Account) bool {
	status := strings.ToLower(strings.TrimSpace(account.Status))
	return status == StatusDisabled || status == "inactive" || status == "disabled"
}

func formalPoolDashboardHasRateLimit(account *Account) bool {
	now := time.Now()
	if account.RateLimitResetAt != nil && now.Before(*account.RateLimitResetAt) {
		return true
	}
	if action := strings.ToLower(strings.TrimSpace(account.GetExtraString(FormalPoolExtraRateLimitAction))); action != "" && !formalPoolDashboardRateLimitActionAllowsPassThrough(action) {
		return true
	}
	combined := strings.ToLower(strings.Join([]string{
		account.GetExtraString(FormalPoolExtraRateLimitErrorClass),
		account.GetExtraString(FormalPoolExtraRateLimitWindow),
		account.GetExtraString(FormalPoolExtraRateLimitAction),
		account.GetExtraString(FormalPoolExtraRateLimitResetBucket),
		account.GetExtraString(FormalPoolExtraRateLimitLastAt),
		account.GetExtraString(FormalPoolExtraHealthcheckStatusCodeBucket),
		account.GetExtraString(FormalPoolExtraHealthcheckSafeErrorCode),
		account.GetExtraString(FormalPoolExtraHealthcheckSafeErrorBucket),
		account.GetExtraString(FormalPoolExtraLastFailureCode),
		account.GetExtraString(FormalPoolExtraLastFailureSource),
		account.GetExtraString(FormalPoolExtraLastCCGatewayErrorCode),
		account.GetExtraString(FormalPoolExtraOnboardingLastErrorCode),
		account.GetExtraString(FormalPoolExtraOnboardingLastErrorBucket),
		account.ErrorMessage,
	}, " "))
	return strings.Contains(combined, "429") || strings.Contains(combined, "rate_limit") || strings.Contains(combined, "rate-limit") || strings.Contains(combined, "rate limited")
}

func formalPoolDashboardRateLimitActionAllowsPassThrough(action string) bool {
	switch strings.TrimSpace(action) {
	case "", "none", "allow", "pass_through", "passthrough":
		return true
	default:
		return false
	}
}

func formalPoolDashboardHasManualRisk(account *Account) bool {
	if formalPoolDashboardBoolExtra(account, FormalPoolExtraHealthcheckRiskTextDetected) {
		return true
	}
	combined := strings.ToLower(strings.Join([]string{
		account.GetExtraString(FormalPoolExtraHealthcheckStatusCodeBucket),
		account.GetExtraString(FormalPoolExtraHealthcheckSafeErrorCode),
		account.GetExtraString(FormalPoolExtraHealthcheckSafeErrorBucket),
		account.GetExtraString(FormalPoolExtraLastFailureCode),
		account.GetExtraString(FormalPoolExtraLastFailureSource),
		account.GetExtraString(FormalPoolExtraLastCCGatewayErrorCode),
		account.GetExtraString(FormalPoolExtraOnboardingLastErrorCode),
		account.GetExtraString(FormalPoolExtraOnboardingLastErrorBucket),
		account.GetExtraString(FormalPoolExtraQuarantineReason),
		account.ErrorMessage,
	}, " "))
	markers := []string{"401", "403", "hold", "kyc", "unusual_activity", "unusual activity", "risk_text", "risk text", "account_risk", "manual_risk"}
	for _, marker := range markers {
		if strings.Contains(combined, marker) {
			return true
		}
	}
	return formalPoolDashboardContainsToken(combined, "risk")
}

func formalPoolDashboardContainsToken(value, token string) bool {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_'
	})
	for _, field := range fields {
		if field == token {
			return true
		}
	}
	return false
}

func formalPoolDashboardBoolExtra(account *Account, key string) bool {
	if account == nil || account.Extra == nil {
		return false
	}
	switch v := account.Extra[key].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	default:
		return strings.EqualFold(strings.TrimSpace(account.GetExtraString(key)), "true")
	}
}

func formalPoolDashboardStateLabelAndSeverity(state string) (string, string) {
	switch state {
	case FormalPoolDashboardStateNormal:
		return "正常", "success"
	case FormalPoolDashboardStateWarming:
		return "预热中", "info"
	case FormalPoolDashboardStateProduction:
		return "生产中", "success"
	case FormalPoolDashboardStateRateLimited:
		return "限流冷却中", "warning"
	case FormalPoolDashboardStateEvidenceMissing:
		return "证据不足", "warning"
	case FormalPoolDashboardStateDataMissing:
		return "数据不足", "warning"
	case FormalPoolDashboardStateInactive:
		return "已停用", "muted"
	case FormalPoolDashboardStateNotSchedulable:
		return "不可调度", "warning"
	case FormalPoolDashboardStateError:
		return "错误", "danger"
	case FormalPoolDashboardStateQuarantined:
		return "已隔离", "danger"
	case FormalPoolDashboardStateManualRisk:
		return "账号风险，需人工介入", "danger"
	default:
		return "数据不足", "warning"
	}
}

func formalPoolDashboardRecommendation(state string) FormalPoolStatusRecommendation {
	switch state {
	case FormalPoolDashboardStateNormal, FormalPoolDashboardStateProduction:
		return FormalPoolStatusRecommendation{Label: "保持观察", Detail: "账号证据完整且可调度。", ActionKind: "none"}
	case FormalPoolDashboardStateWarming:
		return FormalPoolStatusRecommendation{Label: "继续预热", Detail: "保持低权重并等待满足生产条件。", ActionKind: "monitor"}
	case FormalPoolDashboardStateRateLimited:
		return FormalPoolStatusRecommendation{Label: "等待恢复", Detail: "限流冷却中，等待 reset 后复查。", ActionKind: "wait_rate_limit"}
	case FormalPoolDashboardStateManualRisk:
		return FormalPoolStatusRecommendation{Label: "人工介入", Detail: "账号存在风险信号，请人工检查账号状态。", ActionKind: "manual_review"}
	case FormalPoolDashboardStateQuarantined:
		return FormalPoolStatusRecommendation{Label: "查看隔离原因", Detail: "已隔离，先查看安全失败桶和操作诊断。", ActionKind: "inspect_quarantine"}
	case FormalPoolDashboardStateError:
		return FormalPoolStatusRecommendation{Label: "诊断错误", Detail: "错误状态需排查安全失败码和运行证据。", ActionKind: "diagnose_error"}
	case FormalPoolDashboardStateInactive:
		return FormalPoolStatusRecommendation{Label: "确认停用", Detail: "账号已停用，不参与调度。", ActionKind: "confirm_inactive"}
	case FormalPoolDashboardStateNotSchedulable:
		return FormalPoolStatusRecommendation{Label: "查看 gate", Detail: "不可调度，检查调度 gate、冷却或临时禁用原因。", ActionKind: "inspect_gate"}
	case FormalPoolDashboardStateEvidenceMissing:
		return FormalPoolStatusRecommendation{Label: "补齐证据", Detail: "运行注册或健康检查证据不足，不能判断正常。", ActionKind: "complete_evidence"}
	case FormalPoolDashboardStateDataMissing:
		return FormalPoolStatusRecommendation{Label: "补齐数据", Detail: "运行指标未读到，不能判断正常。", ActionKind: "recover_runtime_metrics"}
	default:
		return FormalPoolStatusRecommendation{Label: "补齐数据", Detail: "状态未知，不能判断正常。", ActionKind: "recover_runtime_metrics"}
	}
}

func safeFormalPoolAccountLabel(account *Account) string {
	fallback := fmt.Sprintf("账号 #%d", account.ID)
	name := strings.TrimSpace(account.Name)
	if name == "" || formalPoolDashboardUnsafeLabel(name) {
		return fallback
	}
	return name
}

var (
	formalPoolDashboardEmailRe      = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)
	formalPoolDashboardUUIDRe       = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	formalPoolDashboardLongSecretRe = regexp.MustCompile(`(?i)\b[a-z0-9_\-]{32,}\b`)
)

func formalPoolDashboardUnsafeLabel(label string) bool {
	lower := strings.ToLower(label)
	if formalPoolDashboardUUIDRe.MatchString(label) || formalPoolDashboardLongSecretRe.MatchString(label) {
		return true
	}
	markers := []string{"sk-", "token", "access_token", "refresh", "session_key", "setup", "bearer", "http://", "https://", "://", "raw", "prompt", "body", "cch", "telemetry", "proxy", "password", "credential"}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return strings.Contains(label, ":") && strings.Contains(label, "@")
}

func sanitizeFormalPoolDashboardFailureField(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if formalPoolDashboardSensitiveDiagnostic(raw) {
		return "redacted_sensitive"
	}
	out := sanitizeReasonCode(raw)
	if formalPoolDashboardSensitiveDiagnostic(out) {
		return "redacted_sensitive"
	}
	return out
}

func formalPoolDashboardSensitiveDiagnostic(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	if lower == "" {
		return false
	}
	if formalPoolDashboardUUIDRe.MatchString(value) || formalPoolDashboardLongSecretRe.MatchString(value) {
		return true
	}
	markers := []string{
		"sk-ant", "sk-", "access_token", "access token", "access-token",
		"refresh_token", "refresh token", "refresh-token", "setup_token", "setup token", "setup-token", "session_key",
		"bearer ", "authorization", "raw body", "raw_body", "raw prompt", "raw_prompt",
		"raw cch", "raw_cch", "cch=", "telemetry", "proxy_password", "proxy password",
		"proxy-pass", "password=", "passwd", "://",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return strings.Contains(lower, ":") && strings.Contains(lower, "@")
}

func formalPoolDashboardLastSuccessHint(account *Account) *time.Time {
	if account == nil {
		return nil
	}
	for _, key := range []string{FormalPoolExtraLastHealthcheckAt, FormalPoolExtraRuntimeRegisteredAt} {
		if ts := parseFormalPoolDashboardTime(account.GetExtraString(key)); ts != nil {
			return ts
		}
	}
	return nil
}

func parseFormalPoolDashboardTime(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return &t
	}
	return nil
}

func formalPoolDashboardFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func ratioPtr(current, limit float64) *float64 {
	if limit <= 0 {
		return nil
	}
	r := clampRatio(current / limit)
	return &r
}

func clampRatio(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
