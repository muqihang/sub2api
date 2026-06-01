package service

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
)

type FormalPoolFailureOrigin string

const (
	FormalPoolFailureOriginLocalGate     FormalPoolFailureOrigin = "local_gate"
	FormalPoolFailureOriginCCGateway     FormalPoolFailureOrigin = "cc_gateway_control_plane"
	FormalPoolFailureOriginUpstream      FormalPoolFailureOrigin = "upstream"
	FormalPoolFailureOriginProxy         FormalPoolFailureOrigin = "proxy"
	FormalPoolFailureOriginTokenExchange FormalPoolFailureOrigin = "token_exchange"
	FormalPoolFailureOriginUnknown       FormalPoolFailureOrigin = "unknown"
)

type FormalPoolRecommendedAction struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Severity string `json:"severity,omitempty"`
}

type FormalPoolOperationsDiagnostics struct {
	AccountID                    int64                         `json:"account_id"`
	AccountRef                   string                        `json:"account_ref,omitempty"`
	IsFormalPool                 bool                          `json:"is_formal_pool"`
	OnboardingStage              string                        `json:"onboarding_stage,omitempty"`
	Schedulable                  bool                          `json:"schedulable"`
	EffectiveSchedulable         bool                          `json:"effective_schedulable"`
	FailureOrigin                string                        `json:"failure_origin"`
	FailureCode                  string                        `json:"failure_code,omitempty"`
	FailureSource                string                        `json:"failure_source,omitempty"`
	HealthcheckStatus            string                        `json:"healthcheck_status,omitempty"`
	StatusCodeBucket             string                        `json:"status_code_bucket,omitempty"`
	CCGatewayRuntimeRegistered   bool                          `json:"cc_gateway_runtime_registered"`
	CCGatewayRuntimeRegisteredAt string                        `json:"cc_gateway_runtime_registered_at,omitempty"`
	RuntimeEvidenceComplete      bool                          `json:"runtime_evidence_complete"`
	CCGatewaySeen                bool                          `json:"cc_gateway_seen,omitempty"`
	RawCapturePresent            bool                          `json:"raw_capture_present,omitempty"`
	RawCaptureRef                string                        `json:"raw_capture_ref,omitempty"`
	FallbackDetected             bool                          `json:"fallback_detected,omitempty"`
	ProxyMismatch                bool                          `json:"proxy_mismatch,omitempty"`
	RiskTextDetected             bool                          `json:"risk_text_detected,omitempty"`
	HealthcheckSafeErrorCode     string                        `json:"healthcheck_safe_error_code,omitempty"`
	HealthcheckSafeErrorBucket   string                        `json:"healthcheck_safe_error_bucket,omitempty"`
	RateLimitErrorClass          string                        `json:"formal_pool_rate_limit_error_class,omitempty"`
	RateLimitWindow              string                        `json:"formal_pool_rate_limit_window,omitempty"`
	RateLimitAction              string                        `json:"formal_pool_rate_limit_action,omitempty"`
	RateLimitResetBucket         string                        `json:"formal_pool_rate_limit_reset_bucket,omitempty"`
	RateLimitLastAt              string                        `json:"formal_pool_rate_limit_last_at,omitempty"`
	HealthcheckEvidencePersisted bool                          `json:"healthcheck_evidence_persisted,omitempty"`
	QuarantineReason             string                        `json:"quarantine_reason,omitempty"`
	RiskEventRef                 string                        `json:"risk_event_ref,omitempty"`
	Checks                       []FormalPoolAcceptanceCheck   `json:"checks"`
	RecommendedActions           []FormalPoolRecommendedAction `json:"recommended_actions,omitempty"`
}

type FormalPoolSetupTokenReplaceRequest struct {
	SessionKey         string `json:"session_key"`
	RunRuntimeRegister bool   `json:"run_runtime_register"`
	RunHealthcheck     bool   `json:"run_healthcheck"`
}

type FormalPoolProxySwapRequest struct {
	ProxyID            int64 `json:"proxy_id"`
	RunProxyTest       bool  `json:"run_proxy_test"`
	RunRuntimeRegister bool  `json:"run_runtime_register"`
	RunHealthcheck     bool  `json:"run_healthcheck"`
}

type FormalPoolOperationsAccountResult struct {
	Account     *Account                         `json:"-"`
	Diagnostics *FormalPoolOperationsDiagnostics `json:"diagnostics,omitempty"`
}

type FormalPoolOperationFailure struct {
	Code       string                             `json:"code"`
	Message    string                             `json:"message"`
	HTTPStatus int                                `json:"-"`
	Result     *FormalPoolOperationsAccountResult `json:"result,omitempty"`
}

func (e *FormalPoolOperationFailure) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type FormalPoolOperationAuditEvent struct {
	Operator     string `json:"operator"`
	AccountID    int64  `json:"account_id"`
	BeforeStage  string `json:"before_stage"`
	AfterStage   string `json:"after_stage"`
	Action       string `json:"action"`
	ReasonBucket string `json:"reason_bucket,omitempty"`
	Success      bool   `json:"success"`
	FailureCode  string `json:"failure_code,omitempty"`
	Noop         bool   `json:"noop,omitempty"`
	Timestamp    string `json:"timestamp,omitempty"`
}

type FormalPoolOperationStructuredLogAuditWriter struct{}

func NewFormalPoolOperationStructuredLogAuditWriter() *FormalPoolOperationStructuredLogAuditWriter {
	return &FormalPoolOperationStructuredLogAuditWriter{}
}

func (w *FormalPoolOperationStructuredLogAuditWriter) WriteFormalPoolOperationAudit(_ context.Context, event FormalPoolOperationAuditEvent) error {
	slog.Info("formal_pool_operation_audit", "operator", event.Operator, "account_id", event.AccountID, "before_stage", event.BeforeStage, "after_stage", event.AfterStage, "action", event.Action, "reason_bucket", event.ReasonBucket, "success", event.Success, "failure_code", event.FailureCode, "noop", event.Noop, "timestamp", event.Timestamp)
	return nil
}

type FormalPoolOperationAuditWriter interface {
	WriteFormalPoolOperationAudit(ctx context.Context, event FormalPoolOperationAuditEvent) error
}

type formalPoolOperationOperatorContextKey struct{}

func WithFormalPoolOperationOperator(ctx context.Context, operator string) context.Context {
	operator = formalPoolSafeOperator(operator)
	if operator == "" {
		operator = "unknown"
	}
	return context.WithValue(ctx, formalPoolOperationOperatorContextKey{}, operator)
}

func formalPoolOperationOperator(ctx context.Context) string {
	if ctx != nil {
		if operator, ok := ctx.Value(formalPoolOperationOperatorContextKey{}).(string); ok {
			if safe := formalPoolSafeOperator(operator); safe != "" {
				return safe
			}
		}
	}
	return "system"
}

func formalPoolSafeOperator(operator string) string {
	operator = strings.TrimSpace(operator)
	if operator == "" || formalPoolUnsafeDiagnosticText(operator) || formalPoolDiagnosticSensitiveKeyValueRe.MatchString(operator) {
		return ""
	}
	operator = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == ':' || r == '_' || r == '-' {
			return r
		}
		return -1
	}, operator)
	if len(operator) > 80 {
		operator = operator[:80]
	}
	return operator
}

type FormalPoolOperationsAccountStore interface {
	GetFormalPoolAccount(ctx context.Context, id int64) (*Account, error)
	UpdateFormalPoolAccountCredentials(ctx context.Context, id int64, credentials map[string]any) (*Account, error)
	UpdateFormalPoolAccountState(ctx context.Context, id int64, schedulable bool, status string, extra map[string]any) (*Account, error)
	ActivateFormalPoolAccount(ctx context.Context, id int64, extra map[string]any) (*Account, error)
	UpdateFormalPoolAccountProxy(ctx context.Context, id int64, proxyID int64, extra map[string]any) (*Account, error)
}

type FormalPoolOperationsProxyStore interface {
	GetProxy(ctx context.Context, id int64) (*Proxy, error)
	TestProxy(ctx context.Context, proxyID int64) (FormalPoolProxyTestSummary, error)
}

type FormalPoolOperationsDeps struct {
	Accounts         FormalPoolOperationsAccountStore
	OAuth            FormalPoolOAuthFacade
	Proxy            FormalPoolOperationsProxyStore
	CCGatewayRuntime FormalPoolCCGatewayRuntimeRegistrar
	Healthcheck      FormalPoolAccountHealthcheckRunner
	Quarantine       *AccountQuarantineService
	Audit            FormalPoolOperationAuditWriter
	CacheInvalidator TokenCacheInvalidator
	SchedulerCache   SchedulerCache
	Now              func() time.Time
}

type FormalPoolOperationsService struct {
	accounts         FormalPoolOperationsAccountStore
	oauth            FormalPoolOAuthFacade
	proxy            FormalPoolOperationsProxyStore
	ccGatewayRuntime FormalPoolCCGatewayRuntimeRegistrar
	healthcheck      FormalPoolAccountHealthcheckRunner
	quarantine       *AccountQuarantineService
	audit            FormalPoolOperationAuditWriter
	cacheInvalidator TokenCacheInvalidator
	schedulerCache   SchedulerCache
	now              func() time.Time
}

func NewFormalPoolOperationsService(deps FormalPoolOperationsDeps) *FormalPoolOperationsService {
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	return &FormalPoolOperationsService{
		accounts:         deps.Accounts,
		oauth:            deps.OAuth,
		proxy:            deps.Proxy,
		ccGatewayRuntime: deps.CCGatewayRuntime,
		healthcheck:      deps.Healthcheck,
		quarantine:       deps.Quarantine,
		audit:            deps.Audit,
		cacheInvalidator: deps.CacheInvalidator,
		schedulerCache:   deps.SchedulerCache,
		now:              now,
	}
}

func NewFormalPoolOperationsAdminAccountStore(admin AdminService) *FormalPoolOperationsAdminAccountStore {
	return &FormalPoolOperationsAdminAccountStore{admin: admin}
}

type FormalPoolOperationsAdminAccountStore struct {
	admin AdminService
}

func (s *FormalPoolOperationsAdminAccountStore) GetFormalPoolAccount(ctx context.Context, id int64) (*Account, error) {
	if s == nil || s.admin == nil {
		return nil, fmt.Errorf("admin service unavailable")
	}
	return s.admin.GetAccount(ctx, id)
}

func (s *FormalPoolOperationsAdminAccountStore) UpdateFormalPoolAccountCredentials(ctx context.Context, id int64, credentials map[string]any) (*Account, error) {
	if s == nil || s.admin == nil {
		return nil, fmt.Errorf("admin service unavailable")
	}
	return s.admin.UpdateAccount(ctx, id, &UpdateAccountInput{Credentials: cloneCredentials(credentials)})
}

func (s *FormalPoolOperationsAdminAccountStore) UpdateFormalPoolAccountState(ctx context.Context, id int64, schedulable bool, status string, extra map[string]any) (*Account, error) {
	if s == nil || s.admin == nil {
		return nil, fmt.Errorf("admin service unavailable")
	}
	account, err := s.admin.GetAccount(ctx, id)
	if err != nil {
		return nil, err
	}
	merged := cloneCredentials(account.Extra)
	if merged == nil {
		merged = map[string]any{}
	}
	for k, v := range extra {
		merged[k] = v
	}
	input := &UpdateAccountInput{Schedulable: &schedulable, Extra: merged, FormalPoolStateUpdate: true}
	if strings.TrimSpace(status) != "" {
		input.Status = status
	}
	return s.admin.UpdateAccount(ctx, id, input)
}

func (s *FormalPoolOperationsAdminAccountStore) ActivateFormalPoolAccount(ctx context.Context, id int64, extra map[string]any) (*Account, error) {
	return s.UpdateFormalPoolAccountState(ctx, id, true, StatusActive, extra)
}

func (s *FormalPoolOperationsAdminAccountStore) UpdateFormalPoolAccountProxy(ctx context.Context, id int64, proxyID int64, extra map[string]any) (*Account, error) {
	if s == nil || s.admin == nil {
		return nil, fmt.Errorf("admin service unavailable")
	}
	account, err := s.admin.GetAccount(ctx, id)
	if err != nil {
		return nil, err
	}
	merged := cloneCredentials(account.Extra)
	if merged == nil {
		merged = map[string]any{}
	}
	for k, v := range extra {
		merged[k] = v
	}
	schedulable := false
	input := &UpdateAccountInput{ProxyID: &proxyID, Extra: merged, Schedulable: &schedulable, Status: StatusActive, FormalPoolStateUpdate: true}
	if strings.TrimSpace(stringFromAny(extra[FormalPoolExtraOnboardingStage])) == FormalPoolStageQuarantined {
		input.Status = StatusError
	}
	return s.admin.UpdateAccount(ctx, id, input)
}

type FormalPoolOperationsAdminProxyStore struct {
	admin AdminService
}

func NewFormalPoolOperationsAdminProxyStore(admin AdminService) *FormalPoolOperationsAdminProxyStore {
	return &FormalPoolOperationsAdminProxyStore{admin: admin}
}

func (s *FormalPoolOperationsAdminProxyStore) GetProxy(ctx context.Context, id int64) (*Proxy, error) {
	if s == nil || s.admin == nil {
		return nil, fmt.Errorf("admin service unavailable")
	}
	return s.admin.GetProxy(ctx, id)
}

func (s *FormalPoolOperationsAdminProxyStore) TestProxy(ctx context.Context, proxyID int64) (FormalPoolProxyTestSummary, error) {
	if s == nil || s.admin == nil {
		return FormalPoolProxyTestSummary{}, fmt.Errorf("admin service unavailable")
	}
	res, err := s.admin.TestProxy(ctx, proxyID)
	if err != nil {
		return FormalPoolProxyTestSummary{}, err
	}
	if res == nil || !res.Success {
		return FormalPoolProxyTestSummary{}, fmt.Errorf("proxy test failed")
	}
	return FormalPoolProxyTestSummary{Success: true, ProxyRef: formalPoolSafeRef("proxy", fmt.Sprintf("%d", proxyID)), ExitIPRef: formalPoolSafeRef("exit_ip", res.IPAddress), LatencyBucket: formalPoolLatencyBucket(res.LatencyMs)}, nil
}

func (s *FormalPoolOperationsService) Diagnostics(ctx context.Context, accountID int64) (*FormalPoolOperationsDiagnostics, error) {
	if s == nil || s.accounts == nil {
		return nil, fmt.Errorf("formal pool operations account store unavailable")
	}
	account, err := s.accounts.GetFormalPoolAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	return formalPoolDiagnosticsFromAccount(account), nil
}

func (s *FormalPoolOperationsService) ReplaceSetupToken(ctx context.Context, accountID int64, req FormalPoolSetupTokenReplaceRequest) (*FormalPoolOperationsAccountResult, error) {
	return s.withOperationAudit(ctx, accountID, "replace_setup_token", func() (*FormalPoolOperationsAccountResult, error) {
		return s.replaceSetupToken(ctx, accountID, req)
	})
}

func (s *FormalPoolOperationsService) replaceSetupToken(ctx context.Context, accountID int64, req FormalPoolSetupTokenReplaceRequest) (*FormalPoolOperationsAccountResult, error) {
	if s == nil || s.accounts == nil {
		return nil, infraerrors.ServiceUnavailable("FORMAL_POOL_OPERATIONS_UNAVAILABLE", "formal pool operations account store unavailable")
	}
	sessionKey := strings.TrimSpace(req.SessionKey)
	if sessionKey == "" {
		return nil, infraerrors.BadRequest("SETUP_TOKEN_SESSION_KEY_REQUIRED", "setup-token session key is required")
	}
	account, err := s.accounts.GetFormalPoolAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if err := formalPoolRequireSetupTokenAccount(account); err != nil {
		return nil, err
	}
	if s.oauth == nil {
		return nil, infraerrors.ServiceUnavailable("SETUP_TOKEN_REPLACE_UNAVAILABLE", "setup-token replacement is unavailable")
	}
	summary, newCredentials, err := s.oauth.SetupTokenCookieAuth(ctx, sessionKey, *account.ProxyID)
	if err != nil {
		return s.failSetupTokenReplace(ctx, account)
	}
	if !summary.ScopeContainsUserInference {
		return s.failSetupTokenReplace(ctx, account, "setup_token_missing_inference_scope")
	}
	if summary.ScopeContainsClaudeCode {
		return s.failSetupTokenReplace(ctx, account, "setup_token_claude_code_scope_mismatch")
	}
	mergedCredentials := MergeCredentials(cloneCredentials(account.Credentials), cloneCredentials(newCredentials))
	delete(mergedCredentials, "session_key")
	delete(mergedCredentials, "sessionKey")
	updated, err := s.accounts.UpdateFormalPoolAccountCredentials(ctx, account.ID, mergedCredentials)
	if err != nil {
		return nil, err
	}
	account = updated
	if account == nil {
		account, err = s.accounts.GetFormalPoolAccount(ctx, accountID)
		if err != nil {
			return nil, err
		}
	}
	extra := s.refreshedExtra(account)
	account, err = s.accounts.UpdateFormalPoolAccountState(ctx, account.ID, false, StatusActive, extra)
	if err != nil {
		return nil, err
	}
	s.syncRefreshedAccountCaches(ctx, account)
	if req.RunRuntimeRegister {
		result, err := s.runtimeRegister(ctx, account.ID)
		if err != nil {
			return result, err
		}
		if result != nil && result.Account != nil {
			account = result.Account
		}
	}
	if req.RunHealthcheck {
		result, err := s.healthcheckAccount(ctx, account.ID)
		if err != nil {
			return result, err
		}
		if result != nil && result.Account != nil {
			account = result.Account
		}
	}
	return s.accountResult(ctx, account.ID, account)
}

func (s *FormalPoolOperationsService) syncRefreshedAccountCaches(ctx context.Context, account *Account) {
	if s == nil || account == nil {
		return
	}
	if s.cacheInvalidator != nil {
		_ = s.cacheInvalidator.InvalidateToken(ctx, account)
	}
	if s.schedulerCache != nil {
		_ = s.schedulerCache.SetAccount(ctx, account)
	}
}

func (s *FormalPoolOperationsService) RuntimeRegister(ctx context.Context, accountID int64) (*FormalPoolOperationsAccountResult, error) {
	return s.runtimeRegister(ctx, accountID)
}

func (s *FormalPoolOperationsService) Healthcheck(ctx context.Context, accountID int64) (*FormalPoolOperationsAccountResult, error) {
	return s.healthcheckAccount(ctx, accountID)
}

func (s *FormalPoolOperationsService) StartWarming(ctx context.Context, accountID int64) (*FormalPoolOperationsAccountResult, error) {
	return s.withOperationAudit(ctx, accountID, "start_warming", func() (*FormalPoolOperationsAccountResult, error) {
		return s.startWarming(ctx, accountID)
	})
}

func (s *FormalPoolOperationsService) startWarming(ctx context.Context, accountID int64) (*FormalPoolOperationsAccountResult, error) {
	if s == nil || s.accounts == nil {
		return nil, infraerrors.ServiceUnavailable("FORMAL_POOL_OPERATIONS_UNAVAILABLE", "formal pool operations account store unavailable")
	}
	account, err := s.accounts.GetFormalPoolAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if err := formalPoolRequireOperationsAccount(account); err != nil {
		return nil, err
	}
	if !formalPoolStartWarmingEvidenceComplete(account) {
		return nil, infraerrors.BadRequest("HEALTHCHECK_EVIDENCE_INCOMPLETE", "complete persisted healthcheck evidence is required before warming")
	}
	now := s.now()
	extra := map[string]any{
		"onboarding_state":                       FormalPoolOnboardingStatusWarming,
		FormalPoolExtraOnboardingStage:           FormalPoolStageWarming,
		FormalPoolExtraOnboardingStageUpdatedAt:  formalPoolTimestamp(now),
		FormalPoolExtraWarmingStartedAt:          formalPoolTimestamp(now),
		FormalPoolExtraWarmingUntil:              formalPoolTimestamp(now.Add(24 * time.Hour)),
		FormalPoolExtraPoolProfileEffective:      PoolProfileNormal,
		FormalPoolExtraPoolWeightMode:            FormalPoolWeightLow,
		FormalPoolExtraLastFailureOrigin:         "",
		FormalPoolExtraLastFailureCode:           "",
		FormalPoolExtraLastFailureSource:         "",
		FormalPoolExtraOnboardingLastErrorCode:   "",
		FormalPoolExtraOnboardingLastErrorBucket: "",
		FormalPoolExtraQuarantineReason:          "",
		FormalPoolExtraQuarantineAt:              "",
	}
	updated, err := s.accounts.ActivateFormalPoolAccount(ctx, account.ID, extra)
	if err != nil {
		return nil, err
	}
	return s.accountResult(ctx, account.ID, updated)
}

func (s *FormalPoolOperationsService) PromoteProduction(ctx context.Context, accountID int64) (*FormalPoolOperationsAccountResult, error) {
	if s == nil || s.accounts == nil {
		err := infraerrors.ServiceUnavailable("FORMAL_POOL_OPERATIONS_UNAVAILABLE", "formal pool operations account store unavailable")
		if s != nil {
			s.writeOperationAudit(ctx, FormalPoolOperationAuditEvent{AccountID: accountID, Action: "promote_production", Success: false, FailureCode: "FORMAL_POOL_OPERATIONS_UNAVAILABLE"})
		}
		return nil, err
	}
	account, err := s.accounts.GetFormalPoolAccount(ctx, accountID)
	if err != nil {
		s.writeOperationAudit(ctx, FormalPoolOperationAuditEvent{AccountID: accountID, Action: "promote_production", Success: false, FailureCode: formalPoolAuditFailureCode(err)})
		return nil, err
	}
	beforeStage := FormalPoolAccountStage(account)
	reasonBucket := formalPoolOperationReasonBucket(account)
	if err := formalPoolRequireOperationsAccount(account); err != nil {
		s.writeOperationAudit(ctx, FormalPoolOperationAuditEvent{AccountID: accountID, BeforeStage: beforeStage, AfterStage: beforeStage, Action: "promote_production", ReasonBucket: reasonBucket, Success: false, FailureCode: formalPoolAuditFailureCode(err)})
		return nil, err
	}
	if beforeStage == FormalPoolStageProduction {
		result, resultErr := s.accountResult(ctx, account.ID, account)
		s.writeOperationAudit(ctx, FormalPoolOperationAuditEvent{AccountID: account.ID, BeforeStage: beforeStage, AfterStage: beforeStage, Action: "promote_production", ReasonBucket: reasonBucket, Success: resultErr == nil, FailureCode: formalPoolAuditFailureCode(resultErr), Noop: resultErr == nil})
		return result, resultErr
	}
	if beforeStage != FormalPoolStageWarming {
		err := infraerrors.BadRequest("WARMING_NOT_STARTED", "account must be warming before production promotion")
		s.writeOperationAudit(ctx, FormalPoolOperationAuditEvent{AccountID: account.ID, BeforeStage: beforeStage, AfterStage: beforeStage, Action: "promote_production", ReasonBucket: reasonBucket, Success: false, FailureCode: "WARMING_NOT_STARTED"})
		return nil, err
	}
	if !runtimeEvidenceComplete(account) || !healthcheckEvidenceComplete(account) {
		err := infraerrors.BadRequest("PRODUCTION_EVIDENCE_INCOMPLETE", "complete persisted runtime and healthcheck evidence is required before production promotion")
		s.writeOperationAudit(ctx, FormalPoolOperationAuditEvent{AccountID: account.ID, BeforeStage: beforeStage, AfterStage: beforeStage, Action: "promote_production", ReasonBucket: reasonBucket, Success: false, FailureCode: "PRODUCTION_EVIDENCE_INCOMPLETE"})
		return nil, err
	}
	now := s.now()
	effective := normalizePoolProfile(account.GetExtraString(FormalPoolExtraPoolProfileRequested))
	if effective == "" {
		effective = PoolProfileNormal
	}
	extra := map[string]any{
		"onboarding_state":                       FormalPoolOnboardingStatusProduction,
		FormalPoolExtraOnboardingStage:           FormalPoolStageProduction,
		FormalPoolExtraOnboardingStageUpdatedAt:  formalPoolTimestamp(now),
		FormalPoolExtraPoolProfileEffective:      effective,
		FormalPoolExtraPoolWeightMode:            FormalPoolWeightNormal,
		FormalPoolExtraLastFailureOrigin:         "",
		FormalPoolExtraLastFailureCode:           "",
		FormalPoolExtraLastFailureSource:         "",
		FormalPoolExtraLastCCGatewayErrorCode:    "",
		FormalPoolExtraOnboardingLastErrorCode:   "",
		FormalPoolExtraOnboardingLastErrorBucket: "",
		FormalPoolExtraQuarantineReason:          "",
		FormalPoolExtraQuarantineAt:              "",
	}
	updated, err := s.accounts.ActivateFormalPoolAccount(ctx, account.ID, extra)
	if err != nil {
		s.writeOperationAudit(ctx, FormalPoolOperationAuditEvent{AccountID: account.ID, BeforeStage: beforeStage, AfterStage: beforeStage, Action: "promote_production", ReasonBucket: reasonBucket, Success: false, FailureCode: formalPoolAuditFailureCode(err)})
		return nil, err
	}
	result, err := s.accountResult(ctx, account.ID, updated)
	afterStage := FormalPoolStageProduction
	if result != nil && result.Account != nil {
		afterStage = FormalPoolAccountStage(result.Account)
	}
	s.writeOperationAudit(ctx, FormalPoolOperationAuditEvent{AccountID: account.ID, BeforeStage: beforeStage, AfterStage: afterStage, Action: "promote_production", ReasonBucket: reasonBucket, Success: err == nil, FailureCode: formalPoolAuditFailureCode(err)})
	return result, err
}

func (s *FormalPoolOperationsService) SwapProxy(ctx context.Context, accountID int64, req FormalPoolProxySwapRequest) (*FormalPoolOperationsAccountResult, error) {
	return s.withOperationAudit(ctx, accountID, "swap_proxy", func() (*FormalPoolOperationsAccountResult, error) {
		return s.swapProxy(ctx, accountID, req)
	})
}

func (s *FormalPoolOperationsService) swapProxy(ctx context.Context, accountID int64, req FormalPoolProxySwapRequest) (*FormalPoolOperationsAccountResult, error) {
	if s == nil || s.accounts == nil {
		return nil, infraerrors.ServiceUnavailable("FORMAL_POOL_OPERATIONS_UNAVAILABLE", "formal pool operations account store unavailable")
	}
	account, err := s.accounts.GetFormalPoolAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if err := formalPoolRequireOperationsAccount(account); err != nil {
		return nil, err
	}
	if req.ProxyID <= 0 {
		return nil, infraerrors.BadRequest("PROXY_REQUIRED", "proxy id is required")
	}
	if account.ProxyID != nil && *account.ProxyID == req.ProxyID {
		return nil, infraerrors.BadRequest("PROXY_UNCHANGED", "replacement proxy must differ from current proxy")
	}
	if req.RunProxyTest {
		if s.proxy == nil {
			return nil, infraerrors.ServiceUnavailable("PROXY_TEST_UNAVAILABLE", "proxy test is unavailable")
		}
		if _, err := s.proxy.TestProxy(ctx, req.ProxyID); err != nil {
			return nil, infraerrors.BadRequest("PROXY_TEST_FAILED", "proxy test failed")
		}
	}
	extra := s.proxySwappedExtra(account)
	updated, err := s.accounts.UpdateFormalPoolAccountProxy(ctx, account.ID, req.ProxyID, extra)
	if err != nil {
		return nil, err
	}
	account = updated
	if req.RunRuntimeRegister {
		result, err := s.runtimeRegister(ctx, account.ID)
		if err != nil {
			return result, err
		}
		if result != nil && result.Account != nil {
			account = result.Account
		}
	}
	if req.RunHealthcheck {
		result, err := s.healthcheckAccount(ctx, account.ID)
		if err != nil {
			return result, err
		}
		if result != nil && result.Account != nil {
			account = result.Account
		}
	}
	return s.accountResult(ctx, account.ID, account)
}

type formalPoolFailureEvidence struct {
	IsFormalPool        bool
	OnboardingStage     string
	FailureCode         string
	FailureSource       string
	CCGatewayErrorCode  string
	StatusCodeBucket    string
	CCGatewaySeen       bool
	SafeRawCaptureRef   string
	FallbackDetected    bool
	ProxyMismatch       bool
	RiskTextDetected    bool
	RuntimeRegistered   bool
	CCGatewayEnabled    bool
	CCGatewayRoute      string
	InferenceScope      bool
	ControlPlaneMessage string
}

func formalPoolDiagnosticsFromAccount(account *Account) *FormalPoolOperationsDiagnostics {
	out := &FormalPoolOperationsDiagnostics{
		Checks:             []FormalPoolAcceptanceCheck{},
		RecommendedActions: []FormalPoolRecommendedAction{},
	}
	if account == nil {
		out.FailureOrigin = string(FormalPoolFailureOriginLocalGate)
		out.Checks = append(out.Checks, FormalPoolAcceptanceCheck{Name: "account", Status: "fail", Message: "account not found"})
		return out
	}
	out.AccountID = account.ID
	out.IsFormalPool = serviceFormalPoolAccount(account)
	out.Schedulable = account.Schedulable
	out.EffectiveSchedulable = account.IsSchedulable()
	out.OnboardingStage = FormalPoolAccountStage(account)
	out.FailureCode = formalPoolSafeDiagnosticText(account.GetExtraString(FormalPoolExtraLastFailureCode))
	out.FailureSource = formalPoolSafeDiagnosticText(account.GetExtraString(FormalPoolExtraLastFailureSource))
	out.HealthcheckStatus = account.GetExtraString(FormalPoolExtraHealthcheckStatus)
	out.StatusCodeBucket = account.GetExtraString(FormalPoolExtraHealthcheckStatusCodeBucket)
	out.CCGatewayRuntimeRegistered = formalPoolOpsBool(account.Extra[FormalPoolExtraRuntimeRegistered])
	out.CCGatewayRuntimeRegisteredAt = strings.TrimSpace(account.GetExtraString(FormalPoolExtraRuntimeRegisteredAt))
	out.RuntimeEvidenceComplete = runtimeEvidenceComplete(account)
	out.CCGatewaySeen = formalPoolOpsBool(account.Extra[FormalPoolExtraHealthcheckCCGatewaySeen])
	out.FallbackDetected = formalPoolOpsBool(account.Extra[FormalPoolExtraHealthcheckFallbackDetected])
	out.ProxyMismatch = formalPoolOpsBool(account.Extra[FormalPoolExtraHealthcheckProxyMismatch])
	out.RiskTextDetected = formalPoolOpsBool(account.Extra[FormalPoolExtraHealthcheckRiskTextDetected])
	out.HealthcheckSafeErrorCode = formalPoolSafeDiagnosticText(account.GetExtraString(FormalPoolExtraHealthcheckSafeErrorCode))
	out.HealthcheckSafeErrorBucket = formalPoolSafeDiagnosticText(account.GetExtraString(FormalPoolExtraHealthcheckSafeErrorBucket))
	out.RateLimitErrorClass = formalPoolSafeDiagnosticText(account.GetExtraString(FormalPoolExtraRateLimitErrorClass))
	out.RateLimitWindow = formalPoolSafeDiagnosticText(account.GetExtraString(FormalPoolExtraRateLimitWindow))
	out.RateLimitAction = formalPoolSafeDiagnosticText(account.GetExtraString(FormalPoolExtraRateLimitAction))
	out.RateLimitResetBucket = formalPoolSafeDiagnosticText(account.GetExtraString(FormalPoolExtraRateLimitResetBucket))
	out.RateLimitLastAt = formalPoolSafeDiagnosticText(account.GetExtraString(FormalPoolExtraRateLimitLastAt))
	out.QuarantineReason = formalPoolSafeDiagnosticText(account.GetExtraString(FormalPoolExtraQuarantineReason))
	if ref := strings.TrimSpace(account.GetExtraString(FormalPoolExtraRiskEventRef)); isSafeLedgerRef(ref) {
		out.RiskEventRef = ref
	}
	if ref := strings.TrimSpace(ccGatewayAccountRef(account)); isSafeLedgerRef(ref) {
		out.AccountRef = ref
	}
	if rawRef := strings.TrimSpace(account.GetExtraString(FormalPoolExtraHealthcheckRawRef)); isSafeLedgerRef(rawRef) {
		out.RawCapturePresent = true
		out.RawCaptureRef = rawRef
	}
	if !out.IsFormalPool {
		out.OnboardingStage = ""
		out.FailureOrigin = string(FormalPoolFailureOriginLocalGate)
		out.Checks = append(out.Checks, FormalPoolAcceptanceCheck{Name: "not_formal_pool", Status: "fail", Message: "account is not a formal pool Anthropic OAuth/setup-token account"})
		out.RecommendedActions = formalPoolRecommendedActions(FormalPoolFailureOriginLocalGate, account, out)
		return out
	}
	out.Checks = append(out.Checks, formalPoolStageGateCheck(account))
	runtimeRegisteredAt := strings.TrimSpace(account.GetExtraString(FormalPoolExtraRuntimeRegisteredAt))
	if out.CCGatewayRuntimeRegistered && runtimeRegisteredAt != "" {
		out.Checks = append(out.Checks, FormalPoolAcceptanceCheck{Name: "cc_gateway_runtime_registered", Status: "pass"})
	} else if out.CCGatewayRuntimeRegistered {
		out.Checks = append(out.Checks, FormalPoolAcceptanceCheck{Name: "cc_gateway_runtime_registered", Status: "fail", Message: "cc gateway runtime identity/bucket mapping must include registration timestamp before warming"})
	} else {
		out.Checks = append(out.Checks, FormalPoolAcceptanceCheck{Name: "cc_gateway_runtime_registered", Status: "fail", Message: "cc gateway runtime identity/bucket mapping must be registered before warming"})
	}
	out.HealthcheckEvidencePersisted = healthcheckEvidenceComplete(account)
	if !out.HealthcheckEvidencePersisted {
		out.Checks = append(out.Checks, FormalPoolAcceptanceCheck{Name: "healthcheck_evidence_persisted", Status: "warn", Message: "latest healthcheck evidence is required before warming"})
	} else {
		out.Checks = append(out.Checks, FormalPoolAcceptanceCheck{Name: "healthcheck_evidence_persisted", Status: "pass"})
	}
	evidence := formalPoolFailureEvidence{
		IsFormalPool:        out.IsFormalPool,
		OnboardingStage:     out.OnboardingStage,
		FailureCode:         out.FailureCode,
		FailureSource:       out.FailureSource,
		CCGatewayErrorCode:  formalPoolSafeDiagnosticText(account.GetExtraString(FormalPoolExtraLastCCGatewayErrorCode)),
		ControlPlaneMessage: out.QuarantineReason,
		StatusCodeBucket:    out.StatusCodeBucket,
		CCGatewaySeen:       out.CCGatewaySeen,
		SafeRawCaptureRef:   out.RawCaptureRef,
		FallbackDetected:    out.FallbackDetected,
		ProxyMismatch:       out.ProxyMismatch,
		RiskTextDetected:    out.RiskTextDetected,
		RuntimeRegistered:   out.RuntimeEvidenceComplete,
		CCGatewayEnabled:    account.GetExtraString("cc_gateway_enabled") == "true",
		CCGatewayRoute:      account.GetExtraString("cc_gateway_routes"),
		InferenceScope:      strings.Contains(account.GetCredential("scope"), "user:inference"),
	}
	origin := classifyFormalPoolFailureOrigin(evidence)
	out.FailureOrigin = string(origin)
	out.RecommendedActions = formalPoolRecommendedActions(origin, account, out)
	return out
}

func formalPoolSafeDiagnosticText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if formalPoolSafeOperationalDiagnosticCode(strings.ToLower(s)) {
		return s
	}
	if formalPoolDiagnosticSensitiveKeyValueRe.MatchString(s) {
		return ""
	}
	tokens := strings.Fields(s)
	if len(tokens) == 0 {
		return ""
	}
	safe := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if formalPoolUnsafeDiagnosticText(token) {
			continue
		}
		safe = append(safe, token)
	}
	return strings.Join(safe, " ")
}

var (
	formalPoolDiagnosticURLLikeRe           = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*://`)
	formalPoolDiagnosticTokenishRe          = regexp.MustCompile(`(?i)(authorization|access[_ -]?token|refresh[_ -]?token|id[_ -]?token|raw[_ -]?token|token\s*[:=]|api[_ -]?key|x-api-key|cookie|cch|credential|password|passwd|secret|client[_ -]?secret|session[_ -]?key|proxy[_ -]?url|proxy[_ -]?(user|username|pass|password|credential)|account[_ -]?uuid|org[_ -]?uuid|organization[_ -]?uuid|bearer)`)
	formalPoolDiagnosticRawMarkerRe         = regexp.MustCompile(`(?i)(raw[_ -]?body|raw[_ -]?prompt|raw[_ -]?telemetry|raw[_ -]?cch|raw[_ -]?cookie|raw[_ -]?token|sk-ant-sid)`)
	formalPoolDiagnosticSafeCodeRe          = regexp.MustCompile(`^[a-z0-9_:-]+$`)
	formalPoolDiagnosticSensitiveKeyValueRe = regexp.MustCompile(`(?i)(^|[\s,;{\[\(])\s*["'` + "`" + `]?(?:authorization|access[_ -]?token|refresh[_ -]?token|id[_ -]?token|raw[_ -]?token|api[_ -]?key|x-api-key|cookie|raw[_ -]?cookie|cch|credential|password|passwd|secret|client[_ -]?secret|session[_ -]?key|proxy[_ -]?url|proxy[_ -]?(?:user|username|pass|password|credential)|account[_ -]?uuid|org[_ -]?uuid|organization[_ -]?uuid|bearer|raw[_ -]?body|raw[_ -]?prompt|raw[_ -]?telemetry|raw[_ -]?cch)["'` + "`" + `]?\s*(?::|=|\s+)\s*["'` + "`" + `]?\S+`)
)

func formalPoolUnsafeDiagnosticText(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	if strings.ContainsAny(s, "\r\n\t") {
		return true
	}
	lower := strings.ToLower(s)
	if formalPoolSafeOperationalDiagnosticCode(lower) {
		return false
	}
	if formalPoolDiagnosticURLLikeRe.MatchString(s) || strings.Contains(s, "://") || strings.Contains(s, "@") {
		return true
	}
	return ledgerUUIDLikeRe.MatchString(s) ||
		ledgerEmailLikeRe.MatchString(s) ||
		ledgerBearerRe.MatchString(s) ||
		ledgerSensitiveKeyRe.MatchString(s) ||
		formalPoolDiagnosticTokenishRe.MatchString(s) ||
		formalPoolDiagnosticRawMarkerRe.MatchString(lower)
}

func formalPoolSafeOperationalDiagnosticCode(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || len(s) > 128 || !formalPoolDiagnosticSafeCodeRe.MatchString(s) {
		return false
	}
	switch s {
	case "upstream_401", "formal_pool_healthcheck", "missing_account_identity", "reason_auth", "cookie_auth_failed", "setup_token_exchange_failed", "refresh_token_invalid", "invalid_grant", "admin_ref_safe",
		"auth", "forbidden", "risk_text", "risk", "account_on_hold", "hold", "rate_limited", "long_context_usage_credits", "long_context", "proxy_mismatch", "proxy", "fallback", "raw_capture_missing", "raw_capture", "cc_gateway_not_seen", "cc_gateway", "egress_proxy_failure", "unknown":
		return true
	}
	for _, prefix := range []string{"setup_token_", "cookie_auth_", "token_exchange_"} {
		if !strings.HasPrefix(s, prefix) {
			continue
		}
		suffix := strings.TrimPrefix(s, prefix)
		return suffix != "" &&
			!ledgerSensitiveKeyRe.MatchString(suffix) &&
			!formalPoolDiagnosticTokenishRe.MatchString(suffix) &&
			!formalPoolDiagnosticRawMarkerRe.MatchString(suffix)
	}
	return false
}

func formalPoolSetupTokenFailureCode(codes ...string) string {
	for _, code := range codes {
		code = strings.TrimSpace(code)
		if code == "" || formalPoolUnsafeDiagnosticText(code) {
			continue
		}
		switch code {
		case "setup_token_missing_inference_scope", "setup_token_claude_code_scope_mismatch", "setup_token_exchange_failed":
			return code
		}
	}
	return "setup_token_exchange_failed"
}

func formalPoolRiskEventRefFromLedgerEntry(entry RiskEventLedgerEntry) string {
	if !isSafeLedgerRef(entry.AccountRef) || strings.TrimSpace(entry.Timestamp) == "" {
		return ""
	}
	kind := sanitizeReasonCode(entry.Kind)
	reason := sanitizeReasonCode(entry.SafeReason)
	if kind == "" || reason == "" {
		return ""
	}
	return formalPoolSafeRef("risk_event", entry.AccountRef+":"+entry.Timestamp+":"+kind+":"+reason)
}

func serviceFormalPoolAccount(account *Account) bool {
	return account != nil && account.IsAnthropicOAuthOrSetupToken() && IsFormalPoolAccount(account)
}

func formalPoolStageGateCheck(account *Account) FormalPoolAcceptanceCheck {
	stage := FormalPoolAccountStage(account)
	if IsFormalPoolSchedulableStage(stage) {
		return FormalPoolAcceptanceCheck{Name: "stage_gate", Status: "pass"}
	}
	return FormalPoolAcceptanceCheck{Name: "stage_gate", Status: "fail", Message: stage + " accounts cannot be scheduled"}
}

func healthcheckEvidenceComplete(account *Account) bool {
	if account == nil || account.Extra == nil {
		return false
	}
	_, seenOK := account.Extra[FormalPoolExtraHealthcheckCCGatewaySeen]
	_, fallbackOK := account.Extra[FormalPoolExtraHealthcheckFallbackDetected]
	_, proxyOK := account.Extra[FormalPoolExtraHealthcheckProxyMismatch]
	_, riskOK := account.Extra[FormalPoolExtraHealthcheckRiskTextDetected]
	rawRef := strings.TrimSpace(account.GetExtraString(FormalPoolExtraHealthcheckRawRef))
	return account.GetExtraString(FormalPoolExtraHealthcheckStatus) == "passed" &&
		account.GetExtraString(FormalPoolExtraHealthcheckStatusCodeBucket) == "status_2xx" &&
		seenOK && fallbackOK && proxyOK && riskOK && isSafeLedgerRef(rawRef) &&
		formalPoolOpsBool(account.Extra[FormalPoolExtraHealthcheckCCGatewaySeen]) &&
		!formalPoolOpsBool(account.Extra[FormalPoolExtraHealthcheckFallbackDetected]) &&
		!formalPoolOpsBool(account.Extra[FormalPoolExtraHealthcheckProxyMismatch]) &&
		!formalPoolOpsBool(account.Extra[FormalPoolExtraHealthcheckRiskTextDetected])
}

func formalPoolHealthcheckEvidencePersisted(account *Account) bool {
	return healthcheckEvidenceComplete(account)
}

func runtimeEvidenceComplete(account *Account) bool {
	if account == nil || account.Extra == nil {
		return false
	}
	return formalPoolOpsBool(account.Extra[FormalPoolExtraRuntimeRegistered]) &&
		strings.TrimSpace(account.GetExtraString(FormalPoolExtraRuntimeRegisteredAt)) != "" &&
		isSafeLedgerRef(strings.TrimSpace(account.GetExtraString(ccGatewayExtraAccountRef))) &&
		formalPoolEgressBucketEvidenceComplete(account)
}

func formalPoolEgressBucketEvidenceComplete(account *Account) bool {
	if account == nil || account.Extra == nil {
		return false
	}
	bucketEnabled, ok := parseCCGatewayBool(account.GetExtraString(ccGatewayExtraEgressBucketEnabled))
	return ok && bucketEnabled && strings.TrimSpace(resolveCCGatewayEgressBucket(account)) != ""
}

func formalPoolGeneratedRuntimeAccountRef(account *Account) string {
	if account == nil {
		return ""
	}
	if ref := strings.TrimSpace(account.GetExtraString(ccGatewayExtraAccountRef)); isSafeLedgerRef(ref) {
		return ref
	}
	return formalPoolSafeRef("account", strconv.FormatInt(account.ID, 10))
}

func formalPoolGeneratedEgressBucket(account *Account) string {
	if account == nil {
		return ""
	}
	if bucket := strings.TrimSpace(resolveCCGatewayEgressBucket(account)); bucket != "" {
		return bucket
	}
	if account.ProxyID == nil || *account.ProxyID <= 0 {
		return ""
	}
	return formalPoolSafeBucket(formalPoolSafeRef("proxy", strconv.FormatInt(*account.ProxyID, 10)))
}

func classifyFormalPoolFailureOrigin(e formalPoolFailureEvidence) FormalPoolFailureOrigin {
	code := strings.ToLower(strings.TrimSpace(e.FailureCode))
	source := strings.ToLower(strings.TrimSpace(e.FailureSource))
	ccCode := strings.ToLower(strings.TrimSpace(e.CCGatewayErrorCode))
	message := strings.ToLower(strings.TrimSpace(e.ControlPlaneMessage))
	combined := strings.Join([]string{code, source, ccCode, message}, " ")
	if strings.Contains(source, "setup_token") || strings.Contains(source, "cookie_auth") || strings.Contains(source, "token_exchange") ||
		strings.HasPrefix(code, "setup_token_") || strings.HasPrefix(code, "cookie_auth_") || strings.HasPrefix(code, "token_exchange_") {
		return FormalPoolFailureOriginTokenExchange
	}
	if e.ProxyMismatch || strings.Contains(combined, "proxy_mismatch") || strings.Contains(combined, "proxy mismatch") || strings.Contains(combined, "egress_proxy_failure") {
		return FormalPoolFailureOriginProxy
	}
	if formalPoolIsControlPlaneCode(code) || formalPoolIsControlPlaneCode(ccCode) || formalPoolIsControlPlaneSource(source) ||
		e.FallbackDetected || strings.Contains(combined, "verifier") || strings.Contains(combined, "sign_strip") {
		return FormalPoolFailureOriginCCGateway
	}
	if formalPoolHasUpstreamEvidence(e) {
		return FormalPoolFailureOriginUpstream
	}
	if !e.IsFormalPool || !IsFormalPoolSchedulableStage(e.OnboardingStage) || !e.CCGatewayEnabled ||
		strings.TrimSpace(e.CCGatewayRoute) == "" || !e.InferenceScope || !e.RuntimeRegistered || !e.CCGatewaySeen {
		return FormalPoolFailureOriginLocalGate
	}
	return FormalPoolFailureOriginUnknown
}

func formalPoolIsControlPlaneCode(code string) bool {
	switch strings.TrimSpace(code) {
	case "missing_account_identity", "missing_egress_bucket", "runtime_registration_failed", "fallback", "verifier", "sign_strip", "invalid_auth", "forbidden":
		return true
	default:
		return false
	}
}

func formalPoolIsControlPlaneSource(source string) bool {
	switch strings.TrimSpace(source) {
	case "cc_gateway_runtime_register", "cc_gateway_control_plane", "cc_gateway_registration", "runtime_register":
		return true
	default:
		return false
	}
}

func formalPoolHasUpstreamEvidence(e formalPoolFailureEvidence) bool {
	source := strings.ToLower(strings.TrimSpace(e.FailureSource))
	code := strings.ToLower(strings.TrimSpace(e.FailureCode))
	status := strings.ToLower(strings.TrimSpace(e.StatusCodeBucket))
	hasGatewayEvidence := e.CCGatewaySeen && isSafeLedgerRef(e.SafeRawCaptureRef)
	explicitRateLimitUpstream := source == "rate_limit_service" && !formalPoolIsControlPlaneCode(code)
	if !hasGatewayEvidence && !explicitRateLimitUpstream {
		return false
	}
	if status == "status_4xx" || status == "status_5xx" || strings.Contains(code, "401") || strings.Contains(code, "403") ||
		strings.Contains(code, "risk") || strings.Contains(code, "account_hold") || strings.Contains(code, "kyc") ||
		strings.Contains(code, "unusual_activity") || e.RiskTextDetected {
		return true
	}
	return explicitRateLimitUpstream
}

func formalPoolRecommendedActions(origin FormalPoolFailureOrigin, account *Account, d *FormalPoolOperationsDiagnostics) []FormalPoolRecommendedAction {
	var actions []FormalPoolRecommendedAction
	add := func(key, label, severity string) {
		for _, existing := range actions {
			if existing.Key == key {
				return
			}
		}
		actions = append(actions, FormalPoolRecommendedAction{Key: key, Label: label, Severity: severity})
	}
	if account == nil || d == nil {
		return actions
	}
	stage := d.OnboardingStage
	if stage == FormalPoolStageProduction && d.RuntimeEvidenceComplete && d.HealthcheckEvidencePersisted && d.CCGatewaySeen && d.RawCapturePresent &&
		!d.FallbackDetected && !d.ProxyMismatch && !d.RiskTextDetected && account.IsSchedulable() {
		add("monitor", "Monitor only", "info")
		return actions
	}
	if formalPoolTerminalInvalidGrant(account, d) {
		if account.Type == AccountTypeSetupToken {
			add("replace_setup_token", "Replace setup-token login state", "danger")
		} else {
			add("reauthorize_oauth", "Reauthorize OAuth", "danger")
		}
		return actions
	}
	runtimeComplete := runtimeEvidenceComplete(account)
	healthComplete := healthcheckEvidenceComplete(account)
	if stage == FormalPoolStageHealthcheckPassed && runtimeComplete && healthComplete {
		add("start_warming", "Start warming", "info")
		return actions
	}
	if stage == FormalPoolStageWarming && runtimeComplete && healthComplete {
		add("promote_production", "Promote production", "info")
		return actions
	}
	switch origin {
	case FormalPoolFailureOriginTokenExchange:
		add("repair_token", "Repair token first", "warning")
		add("replace_account_and_proxy", "If token repair fails, replace account and proxy", "danger")
	case FormalPoolFailureOriginUpstream:
		add("repair_token", "Repair token first", "warning")
	case FormalPoolFailureOriginProxy:
		add("swap_proxy", "Swap proxy and revalidate", "warning")
	case FormalPoolFailureOriginCCGateway:
		add("runtime_register", "Run runtime registration", "warning")
	}
	if !runtimeComplete || strings.Contains(strings.ToLower(d.FailureCode), "missing_account_identity") || strings.Contains(strings.ToLower(d.FailureCode), "missing_egress_bucket") {
		add("runtime_register", "Run runtime registration", "warning")
	}
	if serviceFormalPoolAccount(account) && origin != FormalPoolFailureOriginProxy && origin != FormalPoolFailureOriginTokenExchange && runtimeComplete && !healthComplete {
		add("healthcheck", "Run directed healthcheck", "info")
	}
	return actions
}

func formalPoolTerminalInvalidGrant(account *Account, d *FormalPoolOperationsDiagnostics) bool {
	if account == nil || d == nil || d.OnboardingStage != FormalPoolStageQuarantined {
		return false
	}
	combined := strings.ToLower(strings.Join([]string{d.FailureCode, d.QuarantineReason, account.GetExtraString(FormalPoolExtraOnboardingLastErrorCode)}, " "))
	return strings.Contains(combined, "invalid_grant") || strings.Contains(combined, "refresh_token_invalid")
}

func formalPoolRequireOperationsAccount(account *Account) error {
	if account == nil {
		return infraerrors.NotFound("ACCOUNT_NOT_FOUND", "account not found")
	}
	if account.Platform != PlatformAnthropic || !account.IsAnthropicOAuthOrSetupToken() || !IsFormalPoolAccount(account) {
		return infraerrors.BadRequest("FORMAL_POOL_ACCOUNT_REQUIRED", "operation requires an Anthropic formal pool OAuth/setup-token account")
	}
	if account.ProxyID == nil || *account.ProxyID <= 0 {
		return infraerrors.BadRequest("FORMAL_POOL_PROXY_REQUIRED", "formal pool account requires a proxy")
	}
	return nil
}

func formalPoolRequireSetupTokenAccount(account *Account) error {
	if err := formalPoolRequireOperationsAccount(account); err != nil {
		return err
	}
	if account.Type != AccountTypeSetupToken {
		return infraerrors.BadRequest("SETUP_TOKEN_ACCOUNT_REQUIRED", "setup-token replacement requires a setup-token account")
	}
	return nil
}

func (s *FormalPoolOperationsService) refreshedExtra(account *Account) map[string]any {
	now := s.now()
	return map[string]any{
		"onboarding_state":                       FormalPoolStageRefreshed,
		FormalPoolExtraOnboardingStage:           FormalPoolStageRefreshed,
		FormalPoolExtraOnboardingStageUpdatedAt:  formalPoolTimestamp(now),
		FormalPoolExtraHealthcheckStatus:         "pending",
		FormalPoolExtraRuntimeRegistered:         "false",
		FormalPoolExtraRuntimeRegisteredAt:       "",
		FormalPoolExtraCredentialGeneration:      formalPoolNextCredentialGeneration(account),
		FormalPoolExtraRepairedAt:                formalPoolTimestamp(now),
		FormalPoolExtraRepairedBy:                "admin",
		FormalPoolExtraLastFailureOrigin:         "",
		FormalPoolExtraLastFailureCode:           "",
		FormalPoolExtraLastFailureSource:         "",
		FormalPoolExtraLastCCGatewayErrorCode:    "",
		FormalPoolExtraOnboardingLastErrorCode:   "",
		FormalPoolExtraOnboardingLastErrorBucket: "",
		FormalPoolExtraQuarantineReason:          "",
		FormalPoolExtraQuarantineAt:              "",
	}
}

func formalPoolNextCredentialGeneration(account *Account) string {
	if account == nil {
		return "1"
	}
	current, err := strconv.Atoi(strings.TrimSpace(account.GetExtraString(FormalPoolExtraCredentialGeneration)))
	if err != nil || current < 0 {
		current = 0
	}
	return strconv.Itoa(current + 1)
}

func (s *FormalPoolOperationsService) runtimeRegister(ctx context.Context, accountID int64) (*FormalPoolOperationsAccountResult, error) {
	return s.withOperationAudit(ctx, accountID, "runtime_register", func() (*FormalPoolOperationsAccountResult, error) {
		return s.runtimeRegisterUnlogged(ctx, accountID)
	})
}

func (s *FormalPoolOperationsService) runtimeRegisterUnlogged(ctx context.Context, accountID int64) (*FormalPoolOperationsAccountResult, error) {
	if s == nil || s.accounts == nil {
		return nil, infraerrors.ServiceUnavailable("FORMAL_POOL_OPERATIONS_UNAVAILABLE", "formal pool operations account store unavailable")
	}
	account, err := s.accounts.GetFormalPoolAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if err := formalPoolRequireOperationsAccount(account); err != nil {
		return nil, err
	}
	if s.ccGatewayRuntime == nil {
		updated, updateErr := s.accounts.UpdateFormalPoolAccountState(ctx, account.ID, false, StatusActive, s.runtimeRegisterFailureExtra("runtime_registration_unavailable"))
		if updateErr != nil {
			return nil, updateErr
		}
		result, _ := s.accountResult(ctx, account.ID, updated)
		return result, infraerrors.ServiceUnavailable("CC_GATEWAY_RUNTIME_REGISTER_UNAVAILABLE", "cc gateway runtime registration is unavailable")
	}
	account, err = s.ensureRuntimeIdentityEvidence(ctx, account)
	if err != nil {
		updated, updateErr := s.accounts.UpdateFormalPoolAccountState(ctx, account.ID, false, StatusActive, s.runtimeRegisterFailureExtra("runtime_identity_evidence_incomplete"))
		if updateErr != nil {
			return nil, updateErr
		}
		result, _ := s.accountResult(ctx, account.ID, updated)
		return result, err
	}
	reg, err := s.runtimeRegistrationInput(ctx, account)
	if err != nil {
		updated, updateErr := s.accounts.UpdateFormalPoolAccountState(ctx, account.ID, false, StatusActive, s.runtimeRegisterFailureExtra("runtime_registration_proxy_unavailable"))
		if updateErr != nil {
			return nil, updateErr
		}
		result, _ := s.accountResult(ctx, account.ID, updated)
		return result, err
	}
	if err := s.ccGatewayRuntime.RegisterCCGatewayRuntime(ctx, reg); err != nil {
		updated, updateErr := s.accounts.UpdateFormalPoolAccountState(ctx, account.ID, false, StatusActive, s.runtimeRegisterFailureExtra("runtime_registration_failed"))
		if updateErr != nil {
			return nil, updateErr
		}
		result, _ := s.accountResult(ctx, account.ID, updated)
		return result, infraerrors.BadRequest("RUNTIME_REGISTRATION_FAILED", "runtime registration failed")
	}
	now := s.now()
	extra := map[string]any{
		"onboarding_state":                      FormalPoolStageRuntimeRegistered,
		FormalPoolExtraOnboardingStage:          FormalPoolStageRuntimeRegistered,
		FormalPoolExtraOnboardingStageUpdatedAt: formalPoolTimestamp(now),
		FormalPoolExtraRuntimeRegistered:        "true",
		FormalPoolExtraRuntimeRegisteredAt:      formalPoolTimestamp(now),
		FormalPoolExtraHealthcheckStatus:        "pending",
		FormalPoolExtraLastFailureOrigin:        "",
		FormalPoolExtraLastFailureCode:          "",
		FormalPoolExtraLastFailureSource:        "",
		FormalPoolExtraLastCCGatewayErrorCode:   "",
		FormalPoolExtraQuarantineReason:         "",
		FormalPoolExtraQuarantineAt:             "",
	}
	updated, err := s.accounts.UpdateFormalPoolAccountState(ctx, account.ID, false, StatusActive, extra)
	if err != nil {
		return nil, err
	}
	return s.accountResult(ctx, account.ID, updated)
}

func (s *FormalPoolOperationsService) runtimeRegistrationInput(ctx context.Context, account *Account) (FormalPoolCCGatewayRuntimeRegistration, error) {
	if s.proxy == nil {
		return FormalPoolCCGatewayRuntimeRegistration{}, infraerrors.ServiceUnavailable("PROXY_STORE_UNAVAILABLE", "proxy store is unavailable")
	}
	proxy, err := s.proxy.GetProxy(ctx, *account.ProxyID)
	if err != nil {
		return FormalPoolCCGatewayRuntimeRegistration{}, err
	}
	if proxy == nil || !proxy.IsActive() {
		return FormalPoolCCGatewayRuntimeRegistration{}, infraerrors.BadRequest("PROXY_INACTIVE", "proxy is inactive")
	}
	normalized, _, err := proxyurl.Parse(proxy.URL())
	if err != nil {
		return FormalPoolCCGatewayRuntimeRegistration{}, infraerrors.BadRequest("PROXY_URL_INVALID", "proxy url is invalid")
	}
	accountRef := strings.TrimSpace(account.GetExtraString(ccGatewayExtraAccountRef))
	if !isSafeLedgerRef(accountRef) {
		return FormalPoolCCGatewayRuntimeRegistration{}, infraerrors.BadRequest("CC_GATEWAY_ACCOUNT_REF_REQUIRED", "safe cc gateway account ref is required")
	}
	egressBucket := strings.TrimSpace(resolveCCGatewayEgressBucket(account))
	if egressBucket == "" {
		return FormalPoolCCGatewayRuntimeRegistration{}, infraerrors.BadRequest("CC_GATEWAY_EGRESS_BUCKET_REQUIRED", "cc gateway egress bucket is required")
	}
	return FormalPoolCCGatewayRuntimeRegistration{
		AccountRef:     accountRef,
		EgressBucket:   egressBucket,
		ProxyURL:       normalized,
		ProxyRef:       formalPoolSafeRef("proxy", fmt.Sprintf("%d", *account.ProxyID)),
		PolicyVersion:  ccGatewayAnthropicPolicyVersion,
		PersonaVariant: fmt.Sprintf("claude-code-%s-macos-local", ccGatewayAnthropicPolicyVersion),
		SessionPolicy:  "preserve_downstream_session_id",
	}, nil
}

func (s *FormalPoolOperationsService) ensureRuntimeIdentityEvidence(ctx context.Context, account *Account) (*Account, error) {
	if account == nil {
		return nil, infraerrors.BadRequest("ACCOUNT_NOT_FOUND", "account not found")
	}
	accountRef := formalPoolGeneratedRuntimeAccountRef(account)
	egressBucket := formalPoolGeneratedEgressBucket(account)
	if !isSafeLedgerRef(accountRef) {
		return account, infraerrors.BadRequest("CC_GATEWAY_ACCOUNT_REF_REQUIRED", "safe cc gateway account ref is required")
	}
	if strings.TrimSpace(egressBucket) == "" {
		return account, infraerrors.BadRequest("CC_GATEWAY_EGRESS_BUCKET_REQUIRED", "cc gateway egress bucket is required")
	}
	if account.GetExtraString(ccGatewayExtraAccountRef) == accountRef && strings.TrimSpace(resolveCCGatewayEgressBucket(account)) == egressBucket {
		return account, nil
	}
	updated, err := s.accounts.UpdateFormalPoolAccountState(ctx, account.ID, false, StatusActive, map[string]any{
		ccGatewayExtraAccountRef:   accountRef,
		ccGatewayExtraEgressBucket: egressBucket,
	})
	if err != nil {
		return account, err
	}
	return updated, nil
}

func (s *FormalPoolOperationsService) runtimeEvidenceFailureExtra(source string) map[string]any {
	now := s.now()
	return map[string]any{
		FormalPoolExtraOnboardingStageUpdatedAt: formalPoolTimestamp(now),
		FormalPoolExtraRuntimeRegistered:        "false",
		FormalPoolExtraRuntimeRegisteredAt:      "",
		FormalPoolExtraLastFailureOrigin:        string(FormalPoolFailureOriginLocalGate),
		FormalPoolExtraLastFailureCode:          "runtime_evidence_incomplete",
		FormalPoolExtraLastFailureSource:        source,
	}
}

func (s *FormalPoolOperationsService) runtimeRegisterFailureExtra(code string) map[string]any {
	now := s.now()
	return map[string]any{
		FormalPoolExtraOnboardingStageUpdatedAt: formalPoolTimestamp(now),
		FormalPoolExtraRuntimeRegistered:        "false",
		FormalPoolExtraRuntimeRegisteredAt:      "",
		FormalPoolExtraHealthcheckStatus:        "pending",
		FormalPoolExtraLastFailureOrigin:        string(FormalPoolFailureOriginCCGateway),
		FormalPoolExtraLastFailureCode:          code,
		FormalPoolExtraLastFailureSource:        "formal_pool_runtime_register",
	}
}

func (s *FormalPoolOperationsService) healthcheckAccount(ctx context.Context, accountID int64) (*FormalPoolOperationsAccountResult, error) {
	return s.withOperationAudit(ctx, accountID, "healthcheck", func() (*FormalPoolOperationsAccountResult, error) {
		return s.healthcheckAccountUnlogged(ctx, accountID)
	})
}

func (s *FormalPoolOperationsService) healthcheckAccountUnlogged(ctx context.Context, accountID int64) (*FormalPoolOperationsAccountResult, error) {
	if s == nil || s.accounts == nil {
		return nil, infraerrors.ServiceUnavailable("FORMAL_POOL_OPERATIONS_UNAVAILABLE", "formal pool operations account store unavailable")
	}
	account, err := s.accounts.GetFormalPoolAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if err := formalPoolRequireOperationsAccount(account); err != nil {
		return nil, err
	}
	if s.healthcheck == nil {
		return nil, infraerrors.ServiceUnavailable("HEALTHCHECK_UNAVAILABLE", "formal pool healthcheck runner is unavailable")
	}
	if !runtimeEvidenceComplete(account) {
		updated, updateErr := s.accounts.UpdateFormalPoolAccountState(ctx, account.ID, false, StatusActive, s.runtimeEvidenceFailureExtra("formal_pool_healthcheck"))
		if updateErr != nil {
			return nil, updateErr
		}
		result, _ := s.accountResult(ctx, account.ID, updated)
		return result, infraerrors.BadRequest("RUNTIME_EVIDENCE_INCOMPLETE", "complete persisted runtime registration evidence is required before healthcheck")
	}
	input := formalPoolAccountHealthcheckInput(account)
	result, err := s.healthcheck.RunHealthcheck(ctx, input)
	if err != nil {
		return nil, err
	}
	formalPoolFillAccountHealthcheckIdentity(result, input)
	if reloaded, reloadErr := s.accounts.GetFormalPoolAccount(ctx, account.ID); reloadErr == nil && reloaded != nil {
		account = reloaded
	}
	extra := s.healthcheckExtra(account, result)
	status := ""
	if result != nil && result.FormalPoolHealthcheckPassed() && !formalPoolAccountAlreadyQuarantined(account) {
		status = StatusActive
	}
	updated, err := s.accounts.UpdateFormalPoolAccountState(ctx, account.ID, false, status, extra)
	if err != nil {
		return nil, err
	}
	reloaded, err := s.accounts.GetFormalPoolAccount(ctx, account.ID)
	if err == nil && reloaded != nil {
		updated = reloaded
	}
	return s.accountResult(ctx, account.ID, updated)
}

func (s *FormalPoolOperationsService) healthcheckExtra(account *Account, result *FormalPoolAcceptanceResult) map[string]any {
	now := s.now()
	passed := result != nil && result.FormalPoolHealthcheckPassed()
	healthStatus := "failed"
	lastResult := "failed"
	if passed {
		healthStatus = "passed"
		lastResult = "passed"
	}
	quarantined := formalPoolAccountAlreadyQuarantined(account)
	if quarantined {
		healthStatus = "quarantined"
		lastResult = "quarantined"
	}
	extra := map[string]any{
		FormalPoolExtraHealthcheckStatus:           healthStatus,
		FormalPoolExtraHealthcheckStatusCodeBucket: "",
		FormalPoolExtraHealthcheckCCGatewaySeen:    false,
		FormalPoolExtraHealthcheckFallbackDetected: false,
		FormalPoolExtraHealthcheckProxyMismatch:    false,
		FormalPoolExtraHealthcheckRiskTextDetected: false,
		FormalPoolExtraHealthcheckSafeErrorCode:    "",
		FormalPoolExtraHealthcheckSafeErrorBucket:  "",
		FormalPoolExtraLastHealthcheckAt:           formalPoolTimestamp(now),
		FormalPoolExtraLastHealthcheckResult:       lastResult,
	}
	if result != nil {
		extra[FormalPoolExtraHealthcheckStatusCodeBucket] = result.StatusCodeBucket
		extra[FormalPoolExtraHealthcheckCCGatewaySeen] = result.CCGatewaySeen
		extra[FormalPoolExtraHealthcheckFallbackDetected] = result.FallbackDetected
		extra[FormalPoolExtraHealthcheckProxyMismatch] = result.ProxyMismatch
		extra[FormalPoolExtraHealthcheckRiskTextDetected] = result.RiskTextDetected
		extra[FormalPoolExtraHealthcheckSafeErrorCode] = sanitizeReasonCode(result.SafeErrorCode)
		extra[FormalPoolExtraHealthcheckSafeErrorBucket] = sanitizeReasonCode(result.SafeErrorBucket)
		if isSafeLedgerRef(result.RawCaptureRef) {
			extra[FormalPoolExtraHealthcheckRawRef] = strings.TrimSpace(result.RawCaptureRef)
		} else {
			extra[FormalPoolExtraHealthcheckRawRef] = ""
		}
	}
	if passed && !quarantined {
		extra["onboarding_state"] = FormalPoolStageHealthcheckPassed
		extra[FormalPoolExtraOnboardingStage] = FormalPoolStageHealthcheckPassed
		extra[FormalPoolExtraOnboardingStageUpdatedAt] = formalPoolTimestamp(now)
		extra[FormalPoolExtraLastFailureOrigin] = ""
		extra[FormalPoolExtraLastFailureCode] = ""
		extra[FormalPoolExtraLastFailureSource] = ""
		extra[FormalPoolExtraQuarantineReason] = ""
		extra[FormalPoolExtraQuarantineAt] = ""
	}
	if !passed {
		extra[FormalPoolExtraLastFailureOrigin] = string(FormalPoolFailureOriginUpstream)
		extra[FormalPoolExtraLastFailureCode] = "formal_pool_healthcheck_failed"
		extra[FormalPoolExtraLastFailureSource] = "formal_pool_healthcheck"
		if strings.TrimSpace(stringFromAny(extra[FormalPoolExtraHealthcheckSafeErrorCode])) == "" {
			extra[FormalPoolExtraHealthcheckSafeErrorCode] = "unknown"
		}
		if strings.TrimSpace(stringFromAny(extra[FormalPoolExtraHealthcheckSafeErrorBucket])) == "" {
			extra[FormalPoolExtraHealthcheckSafeErrorBucket] = "unknown"
		}
	}
	return extra
}

func formalPoolAccountAlreadyQuarantined(account *Account) bool {
	return account != nil && FormalPoolAccountStage(account) == FormalPoolStageQuarantined
}

func formalPoolStartWarmingEvidenceComplete(account *Account) bool {
	if account == nil {
		return false
	}
	return FormalPoolAccountStage(account) == FormalPoolStageHealthcheckPassed &&
		healthcheckEvidenceComplete(account) &&
		runtimeEvidenceComplete(account)
}

func (s *FormalPoolOperationsService) proxySwappedExtra(account *Account) map[string]any {
	now := s.now()
	stage := FormalPoolStageRefreshed
	healthStatus := "pending"
	if !formalPoolHasCredentials(account) {
		stage = FormalPoolStageQuarantined
		healthStatus = "quarantined"
	}
	extra := map[string]any{
		"onboarding_state":                         stage,
		FormalPoolExtraOnboardingStage:             stage,
		FormalPoolExtraOnboardingStageUpdatedAt:    formalPoolTimestamp(now),
		FormalPoolExtraRuntimeRegistered:           "false",
		FormalPoolExtraHealthcheckStatus:           healthStatus,
		FormalPoolExtraHealthcheckStatusCodeBucket: "",
		FormalPoolExtraHealthcheckRawRef:           "",
		FormalPoolExtraHealthcheckCCGatewaySeen:    false,
		FormalPoolExtraHealthcheckFallbackDetected: false,
		FormalPoolExtraHealthcheckProxyMismatch:    false,
		FormalPoolExtraHealthcheckRiskTextDetected: false,
		FormalPoolExtraLastFailureOrigin:           string(FormalPoolFailureOriginProxy),
		FormalPoolExtraLastFailureCode:             "proxy_swapped_revalidation_required",
		FormalPoolExtraLastFailureSource:           "formal_pool_operations",
		FormalPoolExtraOnboardingLastErrorCode:     "proxy_swapped_revalidation_required",
		FormalPoolExtraOnboardingLastErrorBucket:   "",
		FormalPoolExtraWarmingStartedAt:            "",
		FormalPoolExtraWarmingUntil:                "",
		FormalPoolExtraPoolProfileEffective:        PoolProfileNormal,
		FormalPoolExtraPoolWeightMode:              FormalPoolWeightLow,
		FormalPoolExtraLastCCGatewayErrorCode:      "",
		FormalPoolExtraRuntimeRegisteredAt:         "",
	}
	if stage == FormalPoolStageQuarantined {
		extra[FormalPoolExtraQuarantineReason] = "credential_missing_after_proxy_swap"
		extra[FormalPoolExtraQuarantineAt] = formalPoolTimestamp(now)
	} else {
		extra[FormalPoolExtraQuarantineReason] = ""
		extra[FormalPoolExtraQuarantineAt] = ""
	}
	return extra
}

func formalPoolHasCredentials(account *Account) bool {
	return account != nil && strings.TrimSpace(account.GetCredential("access_token")) != "" && strings.TrimSpace(account.GetCredential("refresh_token")) != ""
}

func (s *FormalPoolOperationsService) failSetupTokenReplace(ctx context.Context, account *Account, failureCode ...string) (*FormalPoolOperationsAccountResult, error) {
	if account == nil {
		return nil, &FormalPoolOperationFailure{Code: "SETUP_TOKEN_REPLACE_FAILED", Message: "setup-token credential exchange failed", HTTPStatus: http.StatusBadRequest}
	}
	safeFailureCode := formalPoolSetupTokenFailureCode(failureCode...)
	now := s.now()
	riskRef := ""
	if s.quarantine != nil {
		if entry, err := s.quarantine.Quarantine(ctx, AccountQuarantineInput{
			AccountID:  account.ID,
			Kind:       RiskEventKindIdentityBoundaryFail,
			Reason:     safeFailureCode,
			Source:     "formal_pool_operations",
			StatusCode: http.StatusUnauthorized,
		}); err == nil {
			riskRef = formalPoolRiskEventRefFromLedgerEntry(entry)
		}
	}
	if !isSafeLedgerRef(riskRef) {
		riskRef = strings.TrimSpace(account.GetExtraString(FormalPoolExtraRiskEventRef))
	}
	if !isSafeLedgerRef(riskRef) {
		riskRef = formalPoolSafeRef("risk_event", fmt.Sprintf("%d:%s:%s", account.ID, formalPoolTimestamp(now), safeFailureCode))
	}
	extra := map[string]any{
		"onboarding_state":                      FormalPoolStageQuarantined,
		FormalPoolExtraOnboardingStage:          FormalPoolStageQuarantined,
		FormalPoolExtraOnboardingStageUpdatedAt: formalPoolTimestamp(now),
		FormalPoolExtraHealthcheckStatus:        "quarantined",
		FormalPoolExtraLastFailureOrigin:        string(FormalPoolFailureOriginTokenExchange),
		FormalPoolExtraLastFailureCode:          safeFailureCode,
		FormalPoolExtraLastFailureSource:        "formal_pool_operations",
		FormalPoolExtraQuarantineReason:         "reason_sensitive",
		FormalPoolExtraQuarantineAt:             formalPoolTimestamp(now),
		FormalPoolExtraRiskEventRef:             riskRef,
	}
	updated, err := s.accounts.UpdateFormalPoolAccountState(ctx, account.ID, false, StatusError, extra)
	if err != nil {
		return nil, err
	}
	result, _ := s.accountResult(ctx, account.ID, updated)
	if result == nil {
		result = &FormalPoolOperationsAccountResult{Account: updated, Diagnostics: formalPoolDiagnosticsFromAccount(updated)}
	}
	return result, &FormalPoolOperationFailure{
		Code:       "SETUP_TOKEN_REPLACE_FAILED",
		Message:    "setup-token credential exchange failed",
		HTTPStatus: http.StatusBadRequest,
		Result:     result,
	}
}

func (s *FormalPoolOperationsService) withOperationAudit(ctx context.Context, accountID int64, action string, op func() (*FormalPoolOperationsAccountResult, error)) (*FormalPoolOperationsAccountResult, error) {
	before := s.safeLoadFormalPoolOperationAccount(ctx, accountID)
	beforeStage := FormalPoolAccountStage(before)
	reasonBucket := formalPoolOperationReasonBucket(before)
	result, err := op()
	afterStage := beforeStage
	if result != nil && result.Account != nil {
		afterStage = FormalPoolAccountStage(result.Account)
	} else if after := s.safeLoadFormalPoolOperationAccount(ctx, accountID); after != nil {
		afterStage = FormalPoolAccountStage(after)
	}
	s.writeOperationAudit(ctx, FormalPoolOperationAuditEvent{
		AccountID:    accountID,
		BeforeStage:  beforeStage,
		AfterStage:   afterStage,
		Action:       action,
		ReasonBucket: reasonBucket,
		Success:      err == nil,
		FailureCode:  formalPoolAuditFailureCode(err),
	})
	return result, err
}

func (s *FormalPoolOperationsService) safeLoadFormalPoolOperationAccount(ctx context.Context, accountID int64) *Account {
	if s == nil || s.accounts == nil || accountID <= 0 {
		return nil
	}
	account, err := s.accounts.GetFormalPoolAccount(ctx, accountID)
	if err != nil {
		return nil
	}
	return account
}

func formalPoolOperationReasonBucket(account *Account) string {
	if account == nil {
		return ""
	}
	for _, v := range []string{
		account.GetExtraString(FormalPoolExtraQuarantineReason),
		account.GetExtraString(FormalPoolExtraLastFailureCode),
		account.GetExtraString(FormalPoolExtraOnboardingLastErrorCode),
	} {
		if safe := formalPoolSafeDiagnosticText(v); safe != "" {
			return safe
		}
	}
	return ""
}

func formalPoolAuditFailureCode(err error) string {
	if err == nil {
		return ""
	}
	statusCode, status := infraerrors.ToHTTP(err)
	_ = statusCode
	if strings.TrimSpace(status.Reason) != "" && status.Reason != infraerrors.UnknownReason {
		return formalPoolSafeDiagnosticText(status.Reason)
	}
	return "operation_failed"
}

func (s *FormalPoolOperationsService) writeOperationAudit(ctx context.Context, event FormalPoolOperationAuditEvent) {
	if s == nil {
		return
	}
	event.Operator = formalPoolOperationOperator(ctx)
	if event.Timestamp == "" {
		event.Timestamp = formalPoolTimestamp(s.now())
	}
	event.Action = formalPoolSafeDiagnosticText(event.Action)
	event.BeforeStage = formalPoolOperationSafeStage(event.BeforeStage)
	event.AfterStage = formalPoolOperationSafeStage(event.AfterStage)
	event.ReasonBucket = formalPoolSafeDiagnosticText(event.ReasonBucket)
	event.FailureCode = formalPoolSafeDiagnosticText(event.FailureCode)
	if s.audit != nil {
		if err := s.audit.WriteFormalPoolOperationAudit(ctx, event); err != nil {
			slog.Warn("formal_pool_operation_audit_write_failed", "action", event.Action, "account_id", event.AccountID, "error", err)
		}
		return
	}
	(&FormalPoolOperationStructuredLogAuditWriter{}).WriteFormalPoolOperationAudit(ctx, event)
}

func formalPoolOperationSafeStage(stage string) string {
	switch strings.TrimSpace(stage) {
	case FormalPoolStageImported, FormalPoolStageRefreshed, FormalPoolStageRuntimeRegistered, FormalPoolStageHealthcheckPassed, FormalPoolStageWarming, FormalPoolStageProduction, FormalPoolStageQuarantined, FormalPoolStageLegacyUnknown:
		return strings.TrimSpace(stage)
	default:
		return ""
	}
}

func (s *FormalPoolOperationsService) accountResult(ctx context.Context, accountID int64, account *Account) (*FormalPoolOperationsAccountResult, error) {
	if account == nil && s != nil && s.accounts != nil {
		var err error
		account, err = s.accounts.GetFormalPoolAccount(ctx, accountID)
		if err != nil {
			return nil, err
		}
	}
	return &FormalPoolOperationsAccountResult{Account: account, Diagnostics: formalPoolDiagnosticsFromAccount(account)}, nil
}

func formalPoolOpsBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		x = strings.ToLower(strings.TrimSpace(x))
		return x == "true" || x == "1" || x == "yes"
	case int:
		return x != 0
	case int64:
		return x != 0
	case float64:
		return x != 0
	default:
		return false
	}
}
