package service

import (
	"context"
	"fmt"
	"strings"
	"time"
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
	CCGatewaySeen                bool                          `json:"cc_gateway_seen,omitempty"`
	RawCapturePresent            bool                          `json:"raw_capture_present,omitempty"`
	RawCaptureRef                string                        `json:"raw_capture_ref,omitempty"`
	FallbackDetected             bool                          `json:"fallback_detected,omitempty"`
	ProxyMismatch                bool                          `json:"proxy_mismatch,omitempty"`
	RiskTextDetected             bool                          `json:"risk_text_detected,omitempty"`
	HealthcheckEvidencePersisted bool                          `json:"healthcheck_evidence_persisted,omitempty"`
	QuarantineReason             string                        `json:"quarantine_reason,omitempty"`
	RiskEventRef                 string                        `json:"risk_event_ref,omitempty"`
	Checks                       []FormalPoolAcceptanceCheck   `json:"checks"`
	RecommendedActions           []FormalPoolRecommendedAction `json:"recommended_actions,omitempty"`
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
	Now              func() time.Time
}

type FormalPoolOperationsService struct {
	accounts         FormalPoolOperationsAccountStore
	oauth            FormalPoolOAuthFacade
	proxy            FormalPoolOperationsProxyStore
	ccGatewayRuntime FormalPoolCCGatewayRuntimeRegistrar
	healthcheck      FormalPoolAccountHealthcheckRunner
	quarantine       *AccountQuarantineService
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
	input := &UpdateAccountInput{Schedulable: &schedulable, Extra: merged}
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
	return s.admin.UpdateAccount(ctx, id, &UpdateAccountInput{ProxyID: &proxyID, Extra: merged})
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
	out.CCGatewaySeen = formalPoolOpsBool(account.Extra[FormalPoolExtraHealthcheckCCGatewaySeen])
	out.FallbackDetected = formalPoolOpsBool(account.Extra[FormalPoolExtraHealthcheckFallbackDetected])
	out.ProxyMismatch = formalPoolOpsBool(account.Extra[FormalPoolExtraHealthcheckProxyMismatch])
	out.RiskTextDetected = formalPoolOpsBool(account.Extra[FormalPoolExtraHealthcheckRiskTextDetected])
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
	out.HealthcheckEvidencePersisted = formalPoolHealthcheckEvidencePersisted(account)
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
		StatusCodeBucket:    out.StatusCodeBucket,
		CCGatewaySeen:       out.CCGatewaySeen,
		SafeRawCaptureRef:   out.RawCaptureRef,
		FallbackDetected:    out.FallbackDetected,
		ProxyMismatch:       out.ProxyMismatch,
		RiskTextDetected:    out.RiskTextDetected,
		RuntimeRegistered:   formalPoolOpsBool(account.Extra[FormalPoolExtraRuntimeRegistered]),
		CCGatewayEnabled:    account.GetExtraString("cc_gateway_enabled") == "true",
		CCGatewayRoute:      account.GetExtraString("cc_gateway_routes"),
		InferenceScope:      strings.Contains(account.GetCredential("scope"), "user:inference"),
		ControlPlaneMessage: out.QuarantineReason,
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
	if ledgerUUIDLikeRe.MatchString(s) || ledgerEmailLikeRe.MatchString(s) || ledgerBearerRe.MatchString(s) || ledgerSensitiveKeyRe.MatchString(s) ||
		strings.Contains(strings.ToLower(s), "sk-ant-sid") {
		return ""
	}
	return s
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

func formalPoolHealthcheckEvidencePersisted(account *Account) bool {
	if account == nil || account.Extra == nil {
		return false
	}
	_, seenOK := account.Extra[FormalPoolExtraHealthcheckCCGatewaySeen]
	_, fallbackOK := account.Extra[FormalPoolExtraHealthcheckFallbackDetected]
	_, proxyOK := account.Extra[FormalPoolExtraHealthcheckProxyMismatch]
	_, riskOK := account.Extra[FormalPoolExtraHealthcheckRiskTextDetected]
	rawRef := strings.TrimSpace(account.GetExtraString(FormalPoolExtraHealthcheckRawRef))
	return seenOK && fallbackOK && proxyOK && riskOK && isSafeLedgerRef(rawRef)
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
	if account != nil && (!formalPoolOpsBool(account.Extra[FormalPoolExtraRuntimeRegistered]) ||
		strings.Contains(strings.ToLower(d.FailureCode), "missing_account_identity") ||
		strings.Contains(strings.ToLower(d.FailureCode), "missing_egress_bucket")) {
		add("runtime_register", "Run runtime registration", "warning")
	}
	if account != nil && serviceFormalPoolAccount(account) && formalPoolOpsBool(account.Extra[FormalPoolExtraRuntimeRegistered]) && origin != FormalPoolFailureOriginProxy {
		add("healthcheck", "Run directed healthcheck", "info")
	}
	if d != nil && d.OnboardingStage == FormalPoolStageHealthcheckPassed && d.HealthcheckEvidencePersisted &&
		d.StatusCodeBucket == "status_2xx" && d.CCGatewaySeen && d.RawCapturePresent && !d.FallbackDetected && !d.ProxyMismatch && !d.RiskTextDetected {
		add("start_warming", "Start warming", "info")
	}
	return actions
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
