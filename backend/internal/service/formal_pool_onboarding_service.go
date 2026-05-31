package service

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrFormalPoolOnboardingNotFound = infraerrors.NotFound("FORMAL_POOL_ONBOARDING_NOT_FOUND", "formal pool onboarding session not found")
)

type FormalPoolOAuthFacade interface {
	GenerateFormalAuthURL(ctx context.Context, proxyID int64) (FormalPoolOAuthURL, error)
	ExchangeCode(ctx context.Context, sessionID, code string, proxyID int64) (FormalPoolOAuthTokenSummary, map[string]any, error)
	SetupTokenCookieAuth(ctx context.Context, sessionKey string, proxyID int64) (FormalPoolOAuthTokenSummary, map[string]any, error)
}

type FormalPoolRefreshOnlyRunner interface {
	RefreshFormalPoolAccount(ctx context.Context, account *Account) (FormalPoolOAuthTokenSummary, map[string]any, error)
}

type FormalPoolProxyVerifier interface {
	ResolveOrCreateProxy(ctx context.Context, req FormalPoolOnboardingStartRequest) (FormalPoolProxyResolution, error)
	TestProxy(ctx context.Context, proxyID int64) (FormalPoolProxyTestSummary, error)
}

type FormalPoolAccountCreator interface {
	CreateFormalPoolAccount(ctx context.Context, input FormalPoolAccountCreateInput) (*Account, error)
	GetFormalPoolAccount(ctx context.Context, id int64) (*Account, error)
	UpdateFormalPoolAccountCredentials(ctx context.Context, id int64, credentials map[string]any) (*Account, error)
	UpdateFormalPoolAccountState(ctx context.Context, id int64, schedulable bool, status string, extra map[string]any) (*Account, error)
	ActivateFormalPoolAccount(ctx context.Context, id int64, extra map[string]any) (*Account, error)
}

type FormalPoolCCGatewayReadinessVerifier interface {
	VerifyCCGatewayReadiness(ctx context.Context, input FormalPoolAcceptanceInput) ([]FormalPoolAcceptanceCheck, error)
}

type FormalPoolCCGatewayRuntimeRegistrar interface {
	RegisterCCGatewayRuntime(ctx context.Context, input FormalPoolCCGatewayRuntimeRegistration) error
}

type FormalPoolAcceptanceRunner interface {
	RunAcceptance(ctx context.Context, input FormalPoolAcceptanceInput) (*FormalPoolAcceptanceResult, error)
}

type FormalPoolAccountHealthcheckRunner interface {
	RunHealthcheck(ctx context.Context, input FormalPoolAcceptanceInput) (*FormalPoolAcceptanceResult, error)
}

type FormalPoolOnboardingDeps struct {
	Store            *FormalPoolOnboardingStore
	OAuth            FormalPoolOAuthFacade
	Proxy            FormalPoolProxyVerifier
	Accounts         FormalPoolAccountCreator
	CCGateway        FormalPoolCCGatewayReadinessVerifier
	CCGatewayRuntime FormalPoolCCGatewayRuntimeRegistrar
	Acceptance       FormalPoolAcceptanceRunner
	Healthcheck      FormalPoolAccountHealthcheckRunner
	Refresh          FormalPoolRefreshOnlyRunner
	CacheInvalidator TokenCacheInvalidator
	SchedulerCache   SchedulerCache
	PublicURLPrefix  string
}

type FormalPoolOnboardingService struct {
	store            *FormalPoolOnboardingStore
	oauth            FormalPoolOAuthFacade
	proxy            FormalPoolProxyVerifier
	accounts         FormalPoolAccountCreator
	ccGateway        FormalPoolCCGatewayReadinessVerifier
	ccGatewayRuntime FormalPoolCCGatewayRuntimeRegistrar
	acceptance       FormalPoolAcceptanceRunner
	healthcheck      FormalPoolAccountHealthcheckRunner
	refresh          FormalPoolRefreshOnlyRunner
	cacheInvalidator TokenCacheInvalidator
	schedulerCache   SchedulerCache
	publicURLPrefix  string
}

type FormalPoolOnboardingStartRequest struct {
	ProxyMode    string                `json:"proxy_mode"`
	ProxyID      *int64                `json:"proxy_id,omitempty"`
	Proxy        *FormalPoolProxyInput `json:"proxy,omitempty"`
	PoolProfile  string                `json:"pool_profile"`
	GroupID      int64                 `json:"group_id"`
	AccountName  string                `json:"account_name"`
	Notes        string                `json:"notes,omitempty"`
	Concurrency  int                   `json:"concurrency"`
	AccountRef   string                `json:"account_ref,omitempty"`
	Token        string                `json:"token,omitempty"`
	RefreshToken string                `json:"refresh_token,omitempty"`
	AccessToken  string                `json:"access_token,omitempty"`
	Code         string                `json:"code,omitempty"`
}

type FormalPoolProxyInput struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type FormalPoolOnboardingSession struct {
	ID                         string                       `json:"id"`
	Status                     string                       `json:"status"`
	ProxyID                    int64                        `json:"proxy_id,omitempty"`
	ProxyRef                   string                       `json:"proxy_ref,omitempty"`
	EgressBucket               string                       `json:"egress_bucket"`
	PoolProfile                string                       `json:"pool_profile"`
	GroupID                    int64                        `json:"group_id"`
	AccountName                string                       `json:"account_name"`
	Concurrency                int                          `json:"concurrency"`
	AuthURL                    string                       `json:"auth_url,omitempty"`
	OAuthSessionID             string                       `json:"oauth_session_id,omitempty"`
	BrowserEgressCheckURL      string                       `json:"browser_egress_check_url,omitempty"`
	BrowserEgressVerified      bool                         `json:"browser_egress_verified"`
	AccountID                  int64                        `json:"account_id,omitempty"`
	AccountRef                 string                       `json:"account_ref,omitempty"`
	OAuthSummary               *FormalPoolOAuthTokenSummary `json:"oauth_summary,omitempty"`
	SafeSummary                map[string]any               `json:"safe_summary"`
	Checks                     []FormalPoolAcceptanceCheck  `json:"checks,omitempty"`
	CCGatewayRuntimeRegistered bool                         `json:"cc_gateway_runtime_registered"`
	HealthcheckPassed          bool                         `json:"healthcheck_passed"`
	ProductionReady            bool                         `json:"production_ready"`
}

type FormalPoolOAuthURL struct {
	AuthURL   string
	SessionID string
}

type FormalPoolOAuthTokenSummary struct {
	EmailPresent               bool   `json:"email_present"`
	AccountUUIDPresent         bool   `json:"account_uuid_present"`
	OrganizationUUIDPresent    bool   `json:"organization_uuid_present"`
	ScopeContainsUserInference bool   `json:"scope_contains_user_inference"`
	ScopeContainsClaudeCode    bool   `json:"scope_contains_claude_code"`
	ExpiresInBucket            string `json:"expires_in_bucket"`
}

type FormalPoolBrowserEgressAttestationRequest struct {
	Confirmed        bool   `json:"confirmed"`
	VerificationCode string `json:"verification_code"`
}

type FormalPoolExchangeCodeAndCreateRequest struct {
	Code         string `json:"code"`
	ProxyID      *int64 `json:"proxy_id,omitempty"`
	AccountRef   string `json:"account_ref,omitempty"`
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Token        string `json:"token,omitempty"`
}

type FormalPoolSetupTokenCookieAuthAndCreateRequest struct {
	SessionKey   string `json:"session_key"`
	Code         string `json:"code,omitempty"`
	ProxyID      *int64 `json:"proxy_id,omitempty"`
	AccountRef   string `json:"account_ref,omitempty"`
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Token        string `json:"token,omitempty"`
}

type FormalPoolProxyResolution struct {
	ProxyID            int64
	ProxyRef           string
	NormalizedProxyURL string
}

type FormalPoolProxyTestSummary struct {
	Success       bool   `json:"success"`
	ProxyRef      string `json:"proxy_ref,omitempty"`
	ExitIPRef     string `json:"exit_ip_ref,omitempty"`
	LatencyBucket string `json:"latency_bucket,omitempty"`
}

type FormalPoolAccountCreateInput struct {
	Type        string
	Name        string
	Notes       string
	Credentials map[string]any
	Extra       map[string]any
	ProxyID     int64
	GroupID     int64
	Concurrency int
	Schedulable bool
}

type FormalPoolAcceptanceInput struct {
	SessionID    string
	AccountID    int64
	AccountRef   string
	AccountName  string
	ProxyID      int64
	ProxyRef     string
	GroupID      int64
	EgressBucket string
	PoolProfile  string
}

type FormalPoolCCGatewayRuntimeRegistration struct {
	AccountRef     string
	EgressBucket   string
	ProxyURL       string
	ProxyRef       string
	PolicyVersion  string
	PersonaVariant string
	SessionPolicy  string
}

type FormalPoolAcceptanceCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type FormalPoolAcceptanceResult struct {
	Status                         string                      `json:"status"`
	AccountID                      int64                       `json:"account_id"`
	AccountRef                     string                      `json:"account_ref"`
	ProxyRef                       string                      `json:"proxy_ref"`
	EgressBucket                   string                      `json:"egress_bucket"`
	PoolProfile                    string                      `json:"pool_profile"`
	Checks                         []FormalPoolAcceptanceCheck `json:"checks"`
	NoRealMessagesRequestPerformed bool                        `json:"no_real_messages_request_performed"`
	ActivationRequired             bool                        `json:"activation_required"`
	StatusCodeBucket               string                      `json:"status_code_bucket,omitempty"`
	CCGatewaySeen                  bool                        `json:"cc_gateway_seen,omitempty"`
	RawCapturePresent              bool                        `json:"raw_capture_present,omitempty"`
	RawCaptureRef                  string                      `json:"raw_capture_ref,omitempty"`
	FallbackDetected               bool                        `json:"fallback_detected,omitempty"`
	ProxyMismatch                  bool                        `json:"proxy_mismatch,omitempty"`
	RiskTextDetected               bool                        `json:"risk_text_detected,omitempty"`
}

type formalPoolHealthcheckAcceptanceAdapter struct {
	runner FormalPoolAccountHealthcheckRunner
}

func (a formalPoolHealthcheckAcceptanceAdapter) RunAcceptance(ctx context.Context, input FormalPoolAcceptanceInput) (*FormalPoolAcceptanceResult, error) {
	return a.runner.RunHealthcheck(ctx, input)
}

func (r *FormalPoolAcceptanceResult) FormalPoolHealthcheckPassed() bool {
	if r == nil {
		return false
	}
	statusOK := r.Status == FormalPoolOnboardingStatusHealthcheckPassed || r.Status == "passed" || r.Status == "healthcheck_passed"
	return statusOK && strings.TrimSpace(r.StatusCodeBucket) == "status_2xx" && r.CCGatewaySeen && r.RawCapturePresent && !r.FallbackDetected && !r.ProxyMismatch && !r.RiskTextDetected
}

func NewFormalPoolOnboardingService(deps FormalPoolOnboardingDeps) *FormalPoolOnboardingService {
	store := deps.Store
	if store == nil {
		store = NewFormalPoolOnboardingStore(FormalPoolOnboardingDefaultTTL, time.Now)
	}
	prefix := strings.TrimRight(strings.TrimSpace(deps.PublicURLPrefix), "/")
	return &FormalPoolOnboardingService{store: store, oauth: deps.OAuth, proxy: deps.Proxy, accounts: deps.Accounts, ccGateway: deps.CCGateway, ccGatewayRuntime: deps.CCGatewayRuntime, acceptance: deps.Acceptance, healthcheck: deps.Healthcheck, refresh: deps.Refresh, cacheInvalidator: deps.CacheInvalidator, schedulerCache: deps.SchedulerCache, publicURLPrefix: prefix}
}

func (s *FormalPoolOnboardingService) StartSession(ctx context.Context, req FormalPoolOnboardingStartRequest) (*FormalPoolOnboardingSession, error) {
	if err := validateFormalPoolStartRequest(req); err != nil {
		return nil, err
	}
	profile := normalizeFormalPoolProfile(req.PoolProfile)
	concurrency := req.Concurrency
	if concurrency == 0 {
		concurrency = FormalPoolOnboardingDefaultConcurrency
	}
	proxyID := int64(0)
	proxyRef := ""
	normalizedProxyURL := ""
	if req.ProxyID != nil {
		proxyID = *req.ProxyID
		proxyRef = formalPoolSafeRef("proxy", fmt.Sprintf("%d", proxyID))
	}
	if s.proxy != nil {
		resolved, err := s.proxy.ResolveOrCreateProxy(ctx, req)
		if err != nil {
			return nil, err
		}
		proxyID = resolved.ProxyID
		proxyRef = resolved.ProxyRef
		normalizedProxyURL = resolved.NormalizedProxyURL
	}
	now := s.store.now()
	rec := &formalPoolOnboardingSessionRecord{
		ID: formalPoolRandomID("fpo_"), Status: FormalPoolOnboardingStatusDraft,
		ProxyMode: strings.ToLower(strings.TrimSpace(req.ProxyMode)), ProxyID: proxyID, ProxyRef: proxyRef,
		NormalizedProxyURL: normalizedProxyURL,
		GroupID:            req.GroupID, AccountName: strings.TrimSpace(req.AccountName), Notes: strings.TrimSpace(req.Notes),
		PoolProfile: profile, Concurrency: concurrency,
		EgressBucket: formalPoolSafeBucket(proxyRef), BrowserNonce: formalPoolRandomID("nonce_"),
		CreatedAt: now, UpdatedAt: now,
	}
	if req.Proxy != nil {
		copy := *req.Proxy
		copy.Password = ""
		rec.CreatedProxyInput = &copy
	}
	s.store.save(rec)
	return s.sessionResponse(rec, nil), nil
}

func (s *FormalPoolOnboardingService) GetSession(_ context.Context, id string) (*FormalPoolOnboardingSession, error) {
	rec, ok := s.store.get(id)
	if !ok {
		return nil, ErrFormalPoolOnboardingNotFound
	}
	return s.sessionResponse(rec, nil), nil
}

func (s *FormalPoolOnboardingService) AbortSession(_ context.Context, id string) (*FormalPoolOnboardingSession, error) {
	rec, err := s.store.update(id, func(rec *formalPoolOnboardingSessionRecord) error {
		rec.Status = FormalPoolOnboardingStatusAborted
		rec.AuthURL = ""
		rec.OAuthSessionID = ""
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.sessionResponse(rec, nil), nil
}

func (s *FormalPoolOnboardingService) MarkBrowserEgressVerifiedForTest(_ context.Context, id string) (*FormalPoolOnboardingSession, error) {
	rec, err := s.store.update(id, func(rec *formalPoolOnboardingSessionRecord) error {
		rec.BrowserVerified = true
		rec.Status = FormalPoolOnboardingStatusBrowserEgressVerified
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.sessionResponse(rec, nil), nil
}

func (s *FormalPoolOnboardingService) VerifyBrowserEgressByNonce(_ context.Context, nonce, remoteIP string) (*FormalPoolOnboardingSession, error) {
	_ = remoteIP
	_, ok := s.store.findByNonce(nonce)
	if !ok {
		return nil, ErrFormalPoolOnboardingNotFound
	}
	return nil, infraerrors.BadRequest("BROWSER_EGRESS_ATTESTATION_REQUIRED", "automatic browser egress matching is not yet available; use explicit attestation")
}

func (s *FormalPoolOnboardingService) TestProxy(ctx context.Context, id string) (*FormalPoolOnboardingSession, error) {
	rec, ok := s.store.get(id)
	if !ok {
		return nil, ErrFormalPoolOnboardingNotFound
	}
	if s.proxy == nil {
		return nil, infraerrors.ServiceUnavailable("PROXY_VERIFIER_UNAVAILABLE", "formal pool proxy verifier is unavailable")
	}
	summary, err := s.proxy.TestProxy(ctx, rec.ProxyID)
	if err != nil {
		return nil, err
	}
	if !summary.Success {
		return nil, infraerrors.BadRequest("PROXY_TEST_FAILED", "proxy test failed")
	}
	rec, err = s.store.update(id, func(rec *formalPoolOnboardingSessionRecord) error {
		rec.Status = FormalPoolOnboardingStatusProxyVerified
		if strings.TrimSpace(summary.ProxyRef) != "" {
			rec.ProxyRef = summary.ProxyRef
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.sessionResponse(rec, []FormalPoolAcceptanceCheck{{Name: "proxy_test", Status: "pass"}}), nil
}

func (s *FormalPoolOnboardingService) AttestBrowserEgress(_ context.Context, id string, req FormalPoolBrowserEgressAttestationRequest) (*FormalPoolOnboardingSession, error) {
	if !req.Confirmed || strings.TrimSpace(req.VerificationCode) == "" {
		return nil, infraerrors.BadRequest("BROWSER_EGRESS_ATTESTATION_REQUIRED", "browser egress attestation is required")
	}
	rec, err := s.store.update(id, func(rec *formalPoolOnboardingSessionRecord) error {
		if rec.Status != FormalPoolOnboardingStatusProxyVerified {
			return infraerrors.BadRequest("PROXY_NOT_VERIFIED", "proxy test must pass before browser egress attestation")
		}
		rec.BrowserVerified = true
		rec.Status = FormalPoolOnboardingStatusBrowserEgressVerified
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.sessionResponse(rec, []FormalPoolAcceptanceCheck{{Name: "browser_egress_attestation", Status: "pass"}}), nil
}

func (s *FormalPoolOnboardingService) GenerateAuthURL(ctx context.Context, id string) (*FormalPoolOnboardingSession, error) {
	rec, ok := s.store.get(id)
	if !ok {
		return nil, ErrFormalPoolOnboardingNotFound
	}
	if !rec.BrowserVerified {
		return nil, infraerrors.BadRequest("BROWSER_EGRESS_UNVERIFIED", "browser egress verification is required before generating OAuth URL")
	}
	if s.oauth == nil {
		return nil, infraerrors.ServiceUnavailable("OAUTH_FACADE_UNAVAILABLE", "formal pool OAuth facade is unavailable")
	}
	res, err := s.oauth.GenerateFormalAuthURL(ctx, rec.ProxyID)
	if err != nil {
		return nil, err
	}
	rec, err = s.store.update(id, func(rec *formalPoolOnboardingSessionRecord) error {
		rec.AuthURL = res.AuthURL
		rec.OAuthSessionID = res.SessionID
		rec.Status = FormalPoolOnboardingStatusOAuthURLGenerated
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.sessionResponse(rec, nil), nil
}

func (s *FormalPoolOnboardingService) ExchangeCodeAndCreate(ctx context.Context, id string, req FormalPoolExchangeCodeAndCreateRequest) (*FormalPoolOnboardingSession, error) {
	if strings.TrimSpace(req.AccountRef) != "" || strings.TrimSpace(req.AccessToken) != "" || strings.TrimSpace(req.RefreshToken) != "" || strings.TrimSpace(req.Token) != "" {
		return nil, infraerrors.BadRequest("FRONTEND_SECRET_FIELD_FORBIDDEN", "frontend-controlled account refs and tokens are forbidden")
	}
	rec, ok := s.store.get(id)
	if !ok {
		return nil, ErrFormalPoolOnboardingNotFound
	}
	if req.ProxyID != nil && *req.ProxyID != rec.ProxyID {
		return nil, infraerrors.BadRequest("PROXY_MISMATCH", "exchange proxy must match onboarding session proxy")
	}
	if strings.TrimSpace(req.Code) == "" {
		return nil, infraerrors.BadRequest("OAUTH_CODE_REQUIRED", "oauth code is required")
	}
	if !rec.BrowserVerified {
		return nil, infraerrors.BadRequest("BROWSER_EGRESS_UNVERIFIED", "browser egress verification is required before exchanging OAuth code")
	}
	if s.oauth == nil || s.accounts == nil {
		return nil, infraerrors.ServiceUnavailable("FORMAL_POOL_CREATE_UNAVAILABLE", "formal pool exchange/create dependencies are unavailable")
	}
	summary, credentials, err := s.oauth.ExchangeCode(ctx, rec.OAuthSessionID, strings.TrimSpace(req.Code), rec.ProxyID)
	if err != nil {
		return nil, err
	}
	if !summary.ScopeContainsUserInference || !summary.ScopeContainsClaudeCode {
		return nil, infraerrors.BadRequest("INVALID_CLAUDE_CODE_OAUTH_SCOPE", "formal pool requires full Claude Code OAuth scope")
	}
	if strings.TrimSpace(stringFromAny(credentials["refresh_token"])) == "" {
		return nil, infraerrors.BadRequest("REFRESH_TOKEN_REQUIRED", "formal pool OAuth account requires refresh token")
	}
	accountRef := formalPoolSafeRef("account", rec.ID+":"+rec.ProxyRef+":"+rec.AccountName)
	runtimeRegistered := false
	if s.ccGatewayRuntime != nil {
		if strings.TrimSpace(rec.NormalizedProxyURL) == "" {
			return nil, infraerrors.BadRequest("CC_GATEWAY_RUNTIME_PROXY_URL_MISSING", "cc gateway runtime registration requires a normalized proxy url")
		}
		if err := s.ccGatewayRuntime.RegisterCCGatewayRuntime(ctx, FormalPoolCCGatewayRuntimeRegistration{
			AccountRef:     accountRef,
			EgressBucket:   rec.EgressBucket,
			ProxyURL:       rec.NormalizedProxyURL,
			ProxyRef:       rec.ProxyRef,
			PolicyVersion:  ccGatewayAnthropicPolicyVersion,
			PersonaVariant: fmt.Sprintf("claude-code-%s-macos-local", ccGatewayAnthropicPolicyVersion),
			SessionPolicy:  "preserve_downstream_session_id",
		}); err != nil {
			return nil, err
		}
		runtimeRegistered = true
	}
	extra := FormalPoolImportedAccountExtra(formalPoolDefaultExtra(rec, accountRef), s.store.now())
	formalPoolMarkRuntimeRegisteredExtra(extra, runtimeRegistered, s.store.now())
	account, err := s.accounts.CreateFormalPoolAccount(ctx, FormalPoolAccountCreateInput{
		Type: AccountTypeOAuth, Name: rec.AccountName, Notes: rec.Notes, Credentials: credentials, Extra: extra,
		ProxyID: rec.ProxyID, GroupID: rec.GroupID, Concurrency: rec.Concurrency, Schedulable: false,
	})
	if err != nil {
		return nil, err
	}
	rec, err = s.store.update(id, func(rec *formalPoolOnboardingSessionRecord) error {
		rec.AccountID = account.ID
		rec.AccountRef = accountRef
		rec.OAuthSummary = &summary
		rec.CCGatewayRuntimeRegistered = runtimeRegistered
		rec.Status = FormalPoolOnboardingStatusImported
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.sessionResponse(rec, nil), nil
}

func (s *FormalPoolOnboardingService) SetupTokenCookieAuthAndCreate(ctx context.Context, id string, req FormalPoolSetupTokenCookieAuthAndCreateRequest) (*FormalPoolOnboardingSession, error) {
	if strings.TrimSpace(req.AccountRef) != "" || strings.TrimSpace(req.AccessToken) != "" || strings.TrimSpace(req.RefreshToken) != "" || strings.TrimSpace(req.Token) != "" {
		return nil, infraerrors.BadRequest("FRONTEND_SECRET_FIELD_FORBIDDEN", "frontend-controlled account refs and tokens are forbidden")
	}
	rec, ok := s.store.get(id)
	if !ok {
		return nil, ErrFormalPoolOnboardingNotFound
	}
	if req.ProxyID != nil && *req.ProxyID != rec.ProxyID {
		return nil, infraerrors.BadRequest("PROXY_MISMATCH", "setup-token proxy must match onboarding session proxy")
	}
	sessionKey := strings.TrimSpace(req.SessionKey)
	if sessionKey == "" {
		sessionKey = strings.TrimSpace(req.Code)
	}
	if sessionKey == "" {
		return nil, infraerrors.BadRequest("SETUP_TOKEN_SESSION_KEY_REQUIRED", "setup-token session key is required")
	}
	if !rec.BrowserVerified {
		return nil, infraerrors.BadRequest("BROWSER_EGRESS_UNVERIFIED", "browser egress verification is required before setup-token cookie auth")
	}
	if s.oauth == nil || s.accounts == nil {
		return nil, infraerrors.ServiceUnavailable("FORMAL_POOL_CREATE_UNAVAILABLE", "formal pool setup-token/create dependencies are unavailable")
	}
	summary, credentials, err := s.oauth.SetupTokenCookieAuth(ctx, sessionKey, rec.ProxyID)
	if err != nil {
		return nil, err
	}
	if !summary.ScopeContainsUserInference {
		return nil, infraerrors.BadRequest("INVALID_SETUP_TOKEN_SCOPE", "setup-token account requires user inference scope")
	}
	if summary.ScopeContainsClaudeCode {
		return nil, infraerrors.BadRequest("SETUP_TOKEN_SCOPE_MISMATCH", "setup-token cookie flow must not import full Claude Code OAuth scope")
	}
	if strings.TrimSpace(stringFromAny(credentials["refresh_token"])) == "" {
		return nil, infraerrors.BadRequest("REFRESH_TOKEN_REQUIRED", "formal pool setup-token account requires refresh token")
	}
	accountRef := formalPoolSafeRef("account", rec.ID+":"+rec.ProxyRef+":"+rec.AccountName)
	runtimeRegistered := false
	if s.ccGatewayRuntime != nil {
		if strings.TrimSpace(rec.NormalizedProxyURL) == "" {
			return nil, infraerrors.BadRequest("CC_GATEWAY_RUNTIME_PROXY_URL_MISSING", "cc gateway runtime registration requires a normalized proxy url")
		}
		if err := s.ccGatewayRuntime.RegisterCCGatewayRuntime(ctx, FormalPoolCCGatewayRuntimeRegistration{
			AccountRef:     accountRef,
			EgressBucket:   rec.EgressBucket,
			ProxyURL:       rec.NormalizedProxyURL,
			ProxyRef:       rec.ProxyRef,
			PolicyVersion:  ccGatewayAnthropicPolicyVersion,
			PersonaVariant: fmt.Sprintf("claude-code-%s-macos-local", ccGatewayAnthropicPolicyVersion),
			SessionPolicy:  "preserve_downstream_session_id",
		}); err != nil {
			return nil, err
		}
		runtimeRegistered = true
	}
	extra := FormalPoolImportedAccountExtra(formalPoolDefaultExtra(rec, accountRef), s.store.now())
	formalPoolMarkRuntimeRegisteredExtra(extra, runtimeRegistered, s.store.now())
	account, err := s.accounts.CreateFormalPoolAccount(ctx, FormalPoolAccountCreateInput{
		Type: AccountTypeSetupToken, Name: rec.AccountName, Notes: rec.Notes, Credentials: credentials, Extra: extra,
		ProxyID: rec.ProxyID, GroupID: rec.GroupID, Concurrency: rec.Concurrency, Schedulable: false,
	})
	if err != nil {
		return nil, err
	}
	rec, err = s.store.update(id, func(rec *formalPoolOnboardingSessionRecord) error {
		rec.AccountID = account.ID
		rec.AccountRef = accountRef
		rec.OAuthSummary = &summary
		rec.CCGatewayRuntimeRegistered = runtimeRegistered
		rec.Status = FormalPoolOnboardingStatusImported
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.sessionResponse(rec, nil), nil
}

func (s *FormalPoolOnboardingService) RunAcceptance(ctx context.Context, id string) (*FormalPoolAcceptanceResult, error) {
	rec, ok := s.store.get(id)
	if !ok {
		return nil, ErrFormalPoolOnboardingNotFound
	}
	if rec.AccountID <= 0 || s.accounts == nil {
		return nil, infraerrors.BadRequest("ACCOUNT_NOT_CREATED", "account must be created before acceptance")
	}
	account, err := s.accounts.GetFormalPoolAccount(ctx, rec.AccountID)
	if err != nil {
		return nil, err
	}
	checks := formalPoolLocalAcceptanceChecks(account, rec)
	if !runtimeEvidenceComplete(account) {
		return &FormalPoolAcceptanceResult{Status: "failed_acceptance", AccountID: rec.AccountID, AccountRef: rec.AccountRef, ProxyRef: rec.ProxyRef, EgressBucket: rec.EgressBucket, PoolProfile: rec.PoolProfile, Checks: checks, NoRealMessagesRequestPerformed: true, ActivationRequired: false}, nil
	}
	var healthResult *FormalPoolAcceptanceResult
	runner := s.acceptance
	if runner == nil && s.healthcheck != nil {
		runner = formalPoolHealthcheckAcceptanceAdapter{s.healthcheck}
	}
	if runner != nil {
		result, err := runner.RunAcceptance(ctx, formalPoolAcceptanceInput(rec))
		if err != nil {
			checks = append(checks, FormalPoolAcceptanceCheck{Name: "directed_healthcheck", Status: "fail", Message: "directed healthcheck failed"})
		} else if result != nil {
			healthResult = result
			checks = append(checks, result.Checks...)
			if result.FormalPoolHealthcheckPassed() {
				rec.HealthcheckPassed = true
			}
		}
	} else {
		checks = append(checks, FormalPoolAcceptanceCheck{Name: "directed_healthcheck", Status: "fail", Message: "directed healthcheck runner unavailable"})
	}
	if s.ccGateway == nil {
		checks = append(checks, FormalPoolAcceptanceCheck{Name: "cc_gateway_readiness", Status: "fail", Message: "cc gateway readiness verifier unavailable"})
	} else {
		ccChecks, err := s.ccGateway.VerifyCCGatewayReadiness(ctx, formalPoolAcceptanceInput(rec))
		if err != nil {
			checks = append(checks, FormalPoolAcceptanceCheck{Name: "cc_gateway_readiness", Status: "fail", Message: "cc gateway readiness failed"})
		} else {
			checks = append(checks, ccChecks...)
		}
	}
	if !rec.HealthcheckPassed {
		checks = append(checks, FormalPoolAcceptanceCheck{Name: "healthcheck_200_required", Status: "fail", Message: "directed healthcheck must return 200 before activation"})
	}
	if !formalPoolChecksAllPass(checks) {
		return &FormalPoolAcceptanceResult{Status: "failed_acceptance", AccountID: rec.AccountID, AccountRef: rec.AccountRef, ProxyRef: rec.ProxyRef, EgressBucket: rec.EgressBucket, PoolProfile: rec.PoolProfile, Checks: checks, NoRealMessagesRequestPerformed: true, ActivationRequired: false}, nil
	}
	if s.accounts != nil {
		healthExtra := formalPoolOnboardingHealthcheckExtra(account, healthResult, s.store.now())
		if _, err := s.accounts.UpdateFormalPoolAccountState(ctx, rec.AccountID, false, StatusActive, healthExtra); err != nil {
			return nil, err
		}
	}
	_, _ = s.store.update(id, func(rec *formalPoolOnboardingSessionRecord) error {
		rec.AcceptancePassed = true
		rec.HealthcheckPassed = true
		rec.Status = FormalPoolOnboardingStatusHealthcheckPassed
		return nil
	})
	out := &FormalPoolAcceptanceResult{Status: FormalPoolOnboardingStatusHealthcheckPassed, AccountID: rec.AccountID, AccountRef: rec.AccountRef, ProxyRef: rec.ProxyRef, EgressBucket: rec.EgressBucket, PoolProfile: rec.PoolProfile, Checks: checks, NoRealMessagesRequestPerformed: false, ActivationRequired: true, StatusCodeBucket: "status_2xx", CCGatewaySeen: true, RawCapturePresent: true}
	if healthResult != nil {
		out.StatusCodeBucket = healthResult.StatusCodeBucket
		out.CCGatewaySeen = healthResult.CCGatewaySeen
		out.RawCapturePresent = healthResult.RawCapturePresent
		out.RawCaptureRef = healthResult.RawCaptureRef
		out.FallbackDetected = healthResult.FallbackDetected
		out.ProxyMismatch = healthResult.ProxyMismatch
		out.RiskTextDetected = healthResult.RiskTextDetected
	}
	return out, nil
}

func (s *FormalPoolOnboardingService) RunAccountHealthcheck(ctx context.Context, accountID int64) (*FormalPoolAcceptanceResult, error) {
	if accountID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_ACCOUNT_ID", "account id must be positive")
	}
	if s.accounts == nil {
		return nil, infraerrors.ServiceUnavailable("ACCOUNT_READER_UNAVAILABLE", "formal pool account reader is unavailable")
	}
	if s.healthcheck == nil {
		return nil, infraerrors.ServiceUnavailable("HEALTHCHECK_UNAVAILABLE", "formal pool healthcheck runner is unavailable")
	}
	account, err := s.accounts.GetFormalPoolAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if !IsFormalPoolAccount(account) {
		return nil, infraerrors.BadRequest("FORMAL_POOL_ACCOUNT_REQUIRED", "account healthcheck requires a formal pool account")
	}
	if !runtimeEvidenceComplete(account) {
		_, _ = s.accounts.UpdateFormalPoolAccountState(ctx, accountID, false, StatusActive, map[string]any{
			FormalPoolExtraRuntimeRegistered:   "false",
			FormalPoolExtraRuntimeRegisteredAt: "",
			FormalPoolExtraLastFailureOrigin:   string(FormalPoolFailureOriginLocalGate),
			FormalPoolExtraLastFailureCode:     "runtime_evidence_incomplete",
			FormalPoolExtraLastFailureSource:   "formal_pool_account_healthcheck",
		})
		return nil, infraerrors.BadRequest("RUNTIME_EVIDENCE_INCOMPLETE", "complete persisted runtime registration evidence is required before healthcheck")
	}
	input := formalPoolAccountHealthcheckInput(account)
	result, err := s.healthcheck.RunHealthcheck(ctx, input)
	if err != nil {
		return nil, err
	}
	formalPoolFillAccountHealthcheckIdentity(result, input)
	healthExtra := formalPoolOnboardingHealthcheckExtra(account, result, s.store.now())
	status := ""
	if result != nil && result.FormalPoolHealthcheckPassed() {
		status = StatusActive
	}
	if _, err := s.accounts.UpdateFormalPoolAccountState(ctx, accountID, false, status, healthExtra); err != nil {
		return nil, err
	}
	return result, nil
}

func formalPoolOnboardingHealthcheckExtra(account *Account, result *FormalPoolAcceptanceResult, now time.Time) map[string]any {
	passed := result != nil && result.FormalPoolHealthcheckPassed()
	healthStatus := "failed"
	lastResult := "failed"
	if passed {
		healthStatus = "passed"
		lastResult = "passed"
	}
	if formalPoolAccountAlreadyQuarantined(account) {
		healthStatus = "quarantined"
		lastResult = "quarantined"
	}
	extra := map[string]any{
		FormalPoolExtraHealthcheckStatus:           healthStatus,
		FormalPoolExtraHealthcheckStatusCodeBucket: "",
		FormalPoolExtraHealthcheckRawRef:           "",
		FormalPoolExtraHealthcheckCCGatewaySeen:    false,
		FormalPoolExtraHealthcheckFallbackDetected: false,
		FormalPoolExtraHealthcheckProxyMismatch:    false,
		FormalPoolExtraHealthcheckRiskTextDetected: false,
		FormalPoolExtraLastHealthcheckAt:           formalPoolTimestamp(now),
		FormalPoolExtraLastHealthcheckResult:       lastResult,
	}
	if result != nil {
		extra[FormalPoolExtraHealthcheckStatusCodeBucket] = result.StatusCodeBucket
		extra[FormalPoolExtraHealthcheckCCGatewaySeen] = result.CCGatewaySeen
		extra[FormalPoolExtraHealthcheckFallbackDetected] = result.FallbackDetected
		extra[FormalPoolExtraHealthcheckProxyMismatch] = result.ProxyMismatch
		extra[FormalPoolExtraHealthcheckRiskTextDetected] = result.RiskTextDetected
		if isSafeLedgerRef(result.RawCaptureRef) {
			extra[FormalPoolExtraHealthcheckRawRef] = strings.TrimSpace(result.RawCaptureRef)
		}
	}
	if passed && !formalPoolAccountAlreadyQuarantined(account) {
		extra["onboarding_state"] = FormalPoolStageHealthcheckPassed
		extra[FormalPoolExtraOnboardingStage] = FormalPoolStageHealthcheckPassed
		extra[FormalPoolExtraOnboardingStageUpdatedAt] = formalPoolTimestamp(now)
		extra[FormalPoolExtraLastFailureOrigin] = ""
		extra[FormalPoolExtraLastFailureCode] = ""
		extra[FormalPoolExtraLastFailureSource] = ""
	} else if !passed {
		extra[FormalPoolExtraLastFailureOrigin] = string(FormalPoolFailureOriginUpstream)
		extra[FormalPoolExtraLastFailureCode] = "formal_pool_healthcheck_failed"
		extra[FormalPoolExtraLastFailureSource] = "formal_pool_healthcheck"
	}
	return extra
}

func formalPoolFillAccountHealthcheckIdentity(result *FormalPoolAcceptanceResult, input FormalPoolAcceptanceInput) {
	if result == nil {
		return
	}
	if result.AccountID == 0 {
		result.AccountID = input.AccountID
	}
	if strings.TrimSpace(result.AccountRef) == "" {
		result.AccountRef = input.AccountRef
	}
	if strings.TrimSpace(result.ProxyRef) == "" {
		result.ProxyRef = input.ProxyRef
	}
	if strings.TrimSpace(result.EgressBucket) == "" {
		result.EgressBucket = input.EgressBucket
	}
	if strings.TrimSpace(result.PoolProfile) == "" {
		result.PoolProfile = input.PoolProfile
	}
}

func (s *FormalPoolOnboardingService) Activate(ctx context.Context, id string) (*FormalPoolOnboardingSession, error) {
	rec, ok := s.store.get(id)
	if !ok {
		return nil, ErrFormalPoolOnboardingNotFound
	}
	if !rec.AcceptancePassed {
		accepted, err := s.RunAcceptance(ctx, id)
		if err != nil {
			return nil, err
		}
		if accepted.Status != "pending_activation" && accepted.Status != FormalPoolOnboardingStatusHealthcheckPassed {
			return nil, infraerrors.BadRequest("ACCEPTANCE_NOT_PASSED", "acceptance must pass before activation")
		}
		rec, _ = s.store.get(id)
	}
	if s.accounts == nil {
		return nil, infraerrors.ServiceUnavailable("ACCOUNT_ACTIVATOR_UNAVAILABLE", "formal pool account activator is unavailable")
	}
	account, err := s.accounts.GetFormalPoolAccount(ctx, rec.AccountID)
	if err != nil {
		return nil, err
	}
	if !formalPoolStartWarmingEvidenceComplete(account) {
		return nil, infraerrors.BadRequest("HEALTHCHECK_EVIDENCE_INCOMPLETE", "complete persisted healthcheck evidence is required before warming")
	}
	now := s.store.now()
	warmingUntil := now.Add(24 * time.Hour)
	if _, err := s.accounts.ActivateFormalPoolAccount(ctx, rec.AccountID, map[string]any{"onboarding_state": FormalPoolOnboardingStatusWarming, FormalPoolExtraOnboardingStage: FormalPoolStageWarming, FormalPoolExtraOnboardingStageUpdatedAt: formalPoolTimestamp(now), FormalPoolExtraWarmingStartedAt: formalPoolTimestamp(now), FormalPoolExtraWarmingUntil: formalPoolTimestamp(warmingUntil), FormalPoolExtraPoolProfileEffective: PoolProfileNormal, FormalPoolExtraPoolWeightMode: FormalPoolWeightLow}); err != nil {
		return nil, err
	}
	rec, err = s.store.update(id, func(rec *formalPoolOnboardingSessionRecord) error {
		rec.Status = FormalPoolOnboardingStatusWarming
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.sessionResponse(rec, []FormalPoolAcceptanceCheck{{Name: "manual_activation", Status: "pass"}}), nil
}

func formalPoolMarkRuntimeRegisteredExtra(extra map[string]any, runtimeRegistered bool, now time.Time) {
	if !runtimeRegistered || extra == nil {
		return
	}
	stamp := formalPoolTimestamp(now)
	extra[FormalPoolExtraRuntimeRegistered] = "true"
	extra[FormalPoolExtraRuntimeRegisteredAt] = stamp
}

func formalPoolRuntimeRegistration(rec *formalPoolOnboardingSessionRecord) FormalPoolCCGatewayRuntimeRegistration {
	if rec == nil {
		return FormalPoolCCGatewayRuntimeRegistration{}
	}
	return FormalPoolCCGatewayRuntimeRegistration{
		AccountRef:     rec.AccountRef,
		EgressBucket:   rec.EgressBucket,
		ProxyURL:       rec.NormalizedProxyURL,
		ProxyRef:       rec.ProxyRef,
		PolicyVersion:  ccGatewayAnthropicPolicyVersion,
		PersonaVariant: fmt.Sprintf("claude-code-%s-macos-local", ccGatewayAnthropicPolicyVersion),
		SessionPolicy:  "preserve_downstream_session_id",
	}
}

func (s *FormalPoolOnboardingService) RefreshOnly(ctx context.Context, id string) (*FormalPoolOnboardingSession, error) {
	rec, ok := s.store.get(id)
	if !ok {
		return nil, ErrFormalPoolOnboardingNotFound
	}
	if rec.AccountID <= 0 || s.accounts == nil {
		return nil, infraerrors.BadRequest("ACCOUNT_NOT_CREATED", "account must be created before refresh-only")
	}
	if s.refresh == nil {
		return nil, infraerrors.ServiceUnavailable("REFRESH_ONLY_UNAVAILABLE", "formal pool refresh-only runner is unavailable")
	}
	account, err := s.accounts.GetFormalPoolAccount(ctx, rec.AccountID)
	if err != nil {
		return nil, err
	}
	summary, credentials, err := s.refresh.RefreshFormalPoolAccount(ctx, account)
	if err != nil {
		if s.accounts != nil {
			bucket := formalPoolRefreshFailureBucket(err)
			_, _ = s.accounts.UpdateFormalPoolAccountState(ctx, rec.AccountID, false, StatusError, map[string]any{
				FormalPoolExtraOnboardingStage:           FormalPoolStageQuarantined,
				FormalPoolExtraOnboardingStageUpdatedAt:  formalPoolTimestamp(s.store.now()),
				FormalPoolExtraOnboardingLastCheck:       FormalPoolStageRefreshed,
				FormalPoolExtraOnboardingLastCheckAt:     formalPoolTimestamp(s.store.now()),
				FormalPoolExtraOnboardingLastErrorCode:   bucket,
				FormalPoolExtraOnboardingLastErrorBucket: bucket,
				FormalPoolExtraLastFailureOrigin:         string(FormalPoolFailureOriginTokenExchange),
				FormalPoolExtraLastFailureCode:           bucket,
				FormalPoolExtraLastFailureSource:         "formal_pool_refresh_only",
				FormalPoolExtraQuarantineReason:          bucket,
				FormalPoolExtraQuarantineAt:              formalPoolTimestamp(s.store.now()),
			})
		}
		return nil, err
	}
	if strings.TrimSpace(stringFromAny(credentials["access_token"])) == "" || strings.TrimSpace(stringFromAny(credentials["refresh_token"])) == "" {
		return nil, infraerrors.BadRequest("REFRESH_ONLY_CREDENTIALS_INCOMPLETE", "refresh-only must return access and refresh tokens")
	}
	if _, err := s.accounts.UpdateFormalPoolAccountCredentials(ctx, rec.AccountID, credentials); err != nil {
		return nil, err
	}
	stamp := formalPoolTimestamp(s.store.now())
	targetStage := FormalPoolStageRefreshed
	targetStatus := FormalPoolOnboardingStatusRefreshed
	if rec.CCGatewayRuntimeRegistered || stringFromAny(account.Extra[FormalPoolExtraRuntimeRegistered]) == "true" {
		targetStage = FormalPoolStageRuntimeRegistered
		targetStatus = FormalPoolOnboardingStatusRuntimeRegistered
	}
	updated, err := s.accounts.UpdateFormalPoolAccountState(ctx, rec.AccountID, false, StatusActive, map[string]any{
		"onboarding_state":                       targetStatus,
		FormalPoolExtraOnboardingStage:           targetStage,
		FormalPoolExtraOnboardingStageUpdatedAt:  stamp,
		FormalPoolExtraOnboardingLastCheck:       targetStage,
		FormalPoolExtraOnboardingLastCheckAt:     stamp,
		FormalPoolExtraOnboardingLastErrorCode:   "",
		FormalPoolExtraOnboardingLastErrorBucket: "",
	})
	if err != nil {
		return nil, err
	}
	s.syncRefreshedFormalPoolAccountCaches(ctx, updated)
	rec, err = s.store.update(id, func(rec *formalPoolOnboardingSessionRecord) error {
		rec.Status = targetStatus
		rec.CCGatewayRuntimeRegistered = rec.CCGatewayRuntimeRegistered || targetStage == FormalPoolStageRuntimeRegistered
		rec.OAuthSummary = &summary
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.sessionResponse(rec, nil), nil
}

func formalPoolRefreshFailureBucket(err error) string {
	if isInvalidGrantError(err) {
		return "refresh_token_invalid"
	}
	return "refresh_failed"
}

func (s *FormalPoolOnboardingService) syncRefreshedFormalPoolAccountCaches(ctx context.Context, account *Account) {
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

func (s *FormalPoolOnboardingService) RegisterRuntime(ctx context.Context, id string) (*FormalPoolOnboardingSession, error) {
	rec, ok := s.store.get(id)
	if !ok {
		return nil, ErrFormalPoolOnboardingNotFound
	}
	if rec.AccountID <= 0 {
		return nil, infraerrors.BadRequest("ACCOUNT_NOT_CREATED", "account must be created before runtime registration")
	}
	if rec.Status != FormalPoolOnboardingStatusRefreshed {
		return nil, infraerrors.BadRequest("REFRESH_ONLY_REQUIRED", "refresh-only must pass before runtime registration")
	}
	if strings.TrimSpace(rec.AccountRef) == "" || strings.TrimSpace(rec.NormalizedProxyURL) == "" || strings.TrimSpace(rec.ProxyRef) == "" || strings.TrimSpace(rec.EgressBucket) == "" {
		return nil, infraerrors.BadRequest("RUNTIME_REGISTRATION_INPUT_MISSING", "runtime registration requires account ref, proxy ref, proxy url and egress bucket")
	}
	if s.ccGatewayRuntime == nil {
		return nil, infraRuntimeRegistrationUnavailable()
	}
	if err := s.ccGatewayRuntime.RegisterCCGatewayRuntime(ctx, formalPoolRuntimeRegistration(rec)); err != nil {
		if s.accounts != nil {
			_, _ = s.accounts.UpdateFormalPoolAccountState(ctx, rec.AccountID, false, StatusError, map[string]any{
				FormalPoolExtraOnboardingStage:          FormalPoolStageQuarantined,
				FormalPoolExtraOnboardingStageUpdatedAt: formalPoolTimestamp(s.store.now()),
				FormalPoolExtraQuarantineReason:         "runtime_registration_failed",
				FormalPoolExtraQuarantineAt:             formalPoolTimestamp(s.store.now()),
			})
		}
		return nil, err
	}
	if s.accounts != nil {
		_, _ = s.accounts.UpdateFormalPoolAccountState(ctx, rec.AccountID, false, StatusActive, map[string]any{
			"onboarding_state":                      FormalPoolOnboardingStatusRuntimeRegistered,
			FormalPoolExtraOnboardingStage:          FormalPoolStageRuntimeRegistered,
			FormalPoolExtraOnboardingStageUpdatedAt: formalPoolTimestamp(s.store.now()),
			FormalPoolExtraRuntimeRegistered:        "true",
			FormalPoolExtraRuntimeRegisteredAt:      formalPoolTimestamp(s.store.now()),
		})
	}
	rec, err := s.store.update(id, func(rec *formalPoolOnboardingSessionRecord) error {
		rec.CCGatewayRuntimeRegistered = true
		rec.Status = FormalPoolOnboardingStatusRuntimeRegistered
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.sessionResponse(rec, nil), nil
}

func (s *FormalPoolOnboardingService) StartWarming(ctx context.Context, id string) (*FormalPoolOnboardingSession, error) {
	return s.Activate(ctx, id)
}

func (s *FormalPoolOnboardingService) PromoteProduction(ctx context.Context, id string) (*FormalPoolOnboardingSession, error) {
	rec, ok := s.store.get(id)
	if !ok {
		return nil, ErrFormalPoolOnboardingNotFound
	}
	if rec.Status != FormalPoolOnboardingStatusWarming {
		return nil, infraerrors.BadRequest("WARMING_NOT_STARTED", "account must be warming before production promotion")
	}
	if s.accounts == nil {
		return nil, infraerrors.ServiceUnavailable("ACCOUNT_UPDATER_UNAVAILABLE", "formal pool account updater is unavailable")
	}
	effective := normalizePoolProfile(rec.PoolProfile)
	if effective == "" {
		effective = PoolProfileNormal
	}
	if _, err := s.accounts.UpdateFormalPoolAccountState(ctx, rec.AccountID, true, StatusActive, map[string]any{
		"onboarding_state":                      FormalPoolOnboardingStatusProduction,
		FormalPoolExtraOnboardingStage:          FormalPoolStageProduction,
		FormalPoolExtraOnboardingStageUpdatedAt: formalPoolTimestamp(s.store.now()),
		FormalPoolExtraPoolProfileEffective:     effective,
		FormalPoolExtraPoolWeightMode:           FormalPoolWeightNormal,
	}); err != nil {
		return nil, err
	}
	rec, err := s.store.update(id, func(rec *formalPoolOnboardingSessionRecord) error {
		rec.Status = FormalPoolOnboardingStatusProduction
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.sessionResponse(rec, nil), nil
}

func formalPoolDefaultExtra(rec *formalPoolOnboardingSessionRecord, accountRef string) map[string]any {
	return map[string]any{
		"cc_gateway_enabled":                "true",
		"cc_gateway_canary_only":            "false",
		"cc_gateway_policy_version":         ccGatewayAnthropicPolicyVersion,
		"cc_gateway_routes":                 string(ccGatewayRouteNativeMessages),
		"cc_gateway_egress_bucket_enabled":  "true",
		"cc_gateway_egress_bucket":          rec.EgressBucket,
		"cc_gateway_account_ref":            accountRef,
		"pool_profile":                      PoolProfileNormal,
		FormalPoolExtraPoolProfileRequested: rec.PoolProfile,
		FormalPoolExtraPoolProfileEffective: PoolProfileNormal,
		FormalPoolExtraPoolWeightMode:       FormalPoolWeightLow,
		"oauth_refresh_fail_closed":         "true",
		"onboarding_state":                  FormalPoolOnboardingStatusPendingAcceptance,
	}
}

func formalPoolAcceptanceInput(rec *formalPoolOnboardingSessionRecord) FormalPoolAcceptanceInput {
	return FormalPoolAcceptanceInput{SessionID: rec.ID, AccountID: rec.AccountID, AccountRef: rec.AccountRef, AccountName: rec.AccountName, ProxyID: rec.ProxyID, ProxyRef: rec.ProxyRef, GroupID: rec.GroupID, EgressBucket: rec.EgressBucket, PoolProfile: rec.PoolProfile}
}

func formalPoolAccountHealthcheckInput(account *Account) FormalPoolAcceptanceInput {
	if account == nil {
		return FormalPoolAcceptanceInput{}
	}
	input := FormalPoolAcceptanceInput{
		AccountID:    account.ID,
		AccountRef:   ccGatewayAccountRef(account),
		AccountName:  account.Name,
		GroupID:      formalPoolFirstAccountGroupID(account),
		EgressBucket: resolveCCGatewayEgressBucket(account),
		PoolProfile:  normalizePoolProfile(account.GetExtraString(FormalPoolExtraPoolProfileRequested)),
	}
	if input.PoolProfile == "" {
		input.PoolProfile = normalizePoolProfile(account.GetExtraString("pool_profile"))
	}
	if input.PoolProfile == "" {
		input.PoolProfile = PoolProfileNormal
	}
	if account.ProxyID != nil {
		input.ProxyID = *account.ProxyID
		input.ProxyRef = formalPoolSafeRef("proxy", fmt.Sprintf("%d", *account.ProxyID))
	}
	return input
}

func formalPoolFirstAccountGroupID(account *Account) int64 {
	if account == nil {
		return 0
	}
	for _, id := range account.GroupIDs {
		if id > 0 {
			return id
		}
	}
	for _, group := range account.AccountGroups {
		if group.GroupID > 0 {
			return group.GroupID
		}
	}
	return 0
}

func formalPoolLocalAcceptanceChecks(account *Account, rec *formalPoolOnboardingSessionRecord) []FormalPoolAcceptanceCheck {
	checks := []FormalPoolAcceptanceCheck{}
	add := func(name string, pass bool, msg string) {
		status := "pass"
		if !pass {
			status = "fail"
		}
		checks = append(checks, FormalPoolAcceptanceCheck{Name: name, Status: status, Message: msg})
	}
	add("account_active", account != nil && account.Status == StatusActive, "account must be active")
	add("account_unschedulable_before_activation", account != nil && !account.Schedulable, "account must remain unschedulable before activation")
	add("proxy_bound", account != nil && account.ProxyID != nil && *account.ProxyID == rec.ProxyID, "proxy must match onboarding session")
	add("group_bound", formalPoolAccountHasGroup(account, rec.GroupID), "group must match onboarding session")
	add("refresh_token_present", account != nil && strings.TrimSpace(account.GetCredential("refresh_token")) != "", "refresh token required")
	scope := ""
	if account != nil {
		scope = account.GetCredential("scope")
	}
	if account != nil && account.Type == AccountTypeSetupToken {
		add("user_inference_scope", strings.Contains(scope, "user:inference") && !strings.Contains(scope, "user:sessions:claude_code"), "setup-token inference-only scope required")
	} else {
		add("user_inference_scope", strings.Contains(scope, "user:inference") && strings.Contains(scope, "user:sessions:claude_code"), "full Claude Code OAuth scope required")
	}
	if account != nil {
		add("cc_gateway_enabled", account.GetExtraString("cc_gateway_enabled") == "true", "cc gateway enabled required")
		add("cc_gateway_canary_only_false", account.GetExtraString("cc_gateway_canary_only") == "false", "formal pool must not be canary-only")
		add("cc_gateway_routes_native_messages", strings.TrimSpace(account.GetExtraString("cc_gateway_routes")) == string(ccGatewayRouteNativeMessages), "routes must be native_messages only")
		add("egress_bucket_present", strings.TrimSpace(account.GetExtraString("cc_gateway_egress_bucket")) != "", "egress bucket required")
		ref := strings.TrimSpace(account.GetExtraString("cc_gateway_account_ref"))
		add("account_ref_safe", ref != "" && ref != fmt.Sprintf("%d", account.ID) && isSafeLedgerRef(ref), "server-generated safe account ref required")
		add("pool_profile_requested_valid", normalizePoolProfile(account.GetExtraString(FormalPoolExtraPoolProfileRequested)) == rec.PoolProfile, "requested pool profile must match")
		add("pool_profile_effective_normal", normalizePoolProfile(account.GetExtraString(FormalPoolExtraPoolProfileEffective)) == PoolProfileNormal, "new account effective profile must remain normal before production")
		add("pool_weight_low", account.GetExtraString(FormalPoolExtraPoolWeightMode) == FormalPoolWeightLow, "new account must start low weight")
		add("oauth_refresh_fail_closed", account.GetExtraString("oauth_refresh_fail_closed") == "true", "refresh must fail closed")
		add("no_dangerous_extra", !formalPoolHasDangerousExtra(account.Extra), "dangerous formal pool extras are forbidden")
	}
	add("cc_gateway_runtime_registered", runtimeEvidenceComplete(account), "cc gateway runtime identity/bucket mapping must be registered before activation")
	checks = append(checks, FormalPoolAcceptanceCheck{Name: "ledger_probe_safe", Status: "pass", Message: "localhost-only redacted ledger probe placeholder; no upstream request performed"})
	return checks
}

func formalPoolAccountHasGroup(account *Account, groupID int64) bool {
	if account == nil || groupID <= 0 {
		return false
	}
	for _, id := range account.GroupIDs {
		if id == groupID {
			return true
		}
	}
	for _, g := range account.Groups {
		if g != nil && g.ID == groupID {
			return true
		}
	}
	for _, ag := range account.AccountGroups {
		if ag.GroupID == groupID {
			return true
		}
	}
	return false
}

func formalPoolHasDangerousExtra(extra map[string]any) bool {
	for _, k := range []string{"enable_tls_fingerprint", "session_id_masking_enabled", "cache_ttl_override_enabled", "cache_ttl_override_target", "custom_base_url", "custom_base_url_enabled", "billing_cch_mode", "max_sessions", "base_rpm", "window_cost_limit"} {
		if _, ok := extra[k]; ok {
			return true
		}
	}
	return false
}

func formalPoolChecksAllPass(checks []FormalPoolAcceptanceCheck) bool {
	for _, c := range checks {
		if c.Status != "pass" && c.Status != "warn" {
			return false
		}
	}
	return true
}

func stringFromAny(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func validateFormalPoolStartRequest(req FormalPoolOnboardingStartRequest) error {
	if strings.TrimSpace(req.AccountName) == "" {
		return infraerrors.BadRequest("ACCOUNT_NAME_REQUIRED", "account_name is required")
	}
	if req.GroupID <= 0 {
		return infraerrors.BadRequest("GROUP_REQUIRED", "group_id is required")
	}
	if strings.TrimSpace(req.AccountRef) != "" || strings.TrimSpace(req.Token) != "" || strings.TrimSpace(req.AccessToken) != "" || strings.TrimSpace(req.RefreshToken) != "" || strings.TrimSpace(req.Code) != "" {
		return infraerrors.BadRequest("FRONTEND_SECRET_FIELD_FORBIDDEN", "frontend-controlled refs, code, or token fields are forbidden")
	}
	profile := strings.ToLower(strings.TrimSpace(req.PoolProfile))
	if profile != "" && profile != PoolProfileNormal && profile != PoolProfileAggressive {
		return infraerrors.BadRequest("INVALID_POOL_PROFILE", "pool_profile must be normal or aggressive")
	}
	if req.Concurrency < 0 {
		return infraerrors.BadRequest("INVALID_CONCURRENCY", "concurrency must be positive")
	}
	if req.Concurrency > FormalPoolOnboardingMaxConcurrency {
		return infraerrors.BadRequest("CONCURRENCY_TOO_HIGH", "concurrency exceeds formal pool onboarding maximum")
	}
	switch strings.ToLower(strings.TrimSpace(req.ProxyMode)) {
	case "existing":
		if req.ProxyID == nil || *req.ProxyID <= 0 {
			return infraerrors.BadRequest("PROXY_REQUIRED", "proxy_id is required for existing proxy mode")
		}
	case "create":
		if req.Proxy == nil {
			return infraerrors.BadRequest("PROXY_REQUIRED", "proxy is required for create proxy mode")
		}
		if err := validateFormalPoolProxyInput(*req.Proxy); err != nil {
			return err
		}
	default:
		return infraerrors.BadRequest("INVALID_PROXY_MODE", "proxy_mode must be existing or create")
	}
	return nil
}

func validateFormalPoolProxyInput(p FormalPoolProxyInput) error {
	switch strings.ToLower(strings.TrimSpace(p.Protocol)) {
	case "http", "https", "socks5", "socks5h":
	default:
		return infraerrors.BadRequest("INVALID_PROXY_PROTOCOL", "proxy protocol must be http, https, socks5, or socks5h")
	}
	if strings.TrimSpace(p.Host) == "" {
		return infraerrors.BadRequest("PROXY_HOST_REQUIRED", "proxy host is required")
	}
	if p.Port <= 0 || p.Port > 65535 {
		return infraerrors.BadRequest("INVALID_PROXY_PORT", "proxy port is invalid")
	}
	return nil
}

func normalizeFormalPoolProfile(v string) string {
	if strings.EqualFold(strings.TrimSpace(v), PoolProfileAggressive) {
		return PoolProfileAggressive
	}
	return PoolProfileNormal
}

func (s *FormalPoolOnboardingService) sessionResponse(rec *formalPoolOnboardingSessionRecord, checks []FormalPoolAcceptanceCheck) *FormalPoolOnboardingSession {
	summary := map[string]any{
		"session_ref":                   formalPoolSafeRef("session", rec.ID),
		"proxy_ref":                     rec.ProxyRef,
		"pool_profile":                  rec.PoolProfile,
		"browser_egress_verified":       rec.BrowserVerified,
		"oauth_url_generated":           rec.AuthURL != "",
		"cc_gateway_runtime_registered": rec.CCGatewayRuntimeRegistered,
		"healthcheck_passed":            rec.HealthcheckPassed,
	}
	return &FormalPoolOnboardingSession{
		ID: rec.ID, Status: rec.Status, ProxyID: rec.ProxyID, ProxyRef: rec.ProxyRef, EgressBucket: rec.EgressBucket,
		PoolProfile: rec.PoolProfile, GroupID: rec.GroupID, AccountName: rec.AccountName, Concurrency: rec.Concurrency,
		AuthURL: rec.AuthURL, OAuthSessionID: rec.OAuthSessionID, BrowserEgressCheckURL: s.browserURL(rec.BrowserNonce),
		BrowserEgressVerified: rec.BrowserVerified, AccountID: rec.AccountID, AccountRef: rec.AccountRef, OAuthSummary: rec.OAuthSummary, SafeSummary: summary, Checks: checks,
		CCGatewayRuntimeRegistered: rec.CCGatewayRuntimeRegistered,
		HealthcheckPassed:          rec.HealthcheckPassed,
		ProductionReady:            rec.Status == FormalPoolOnboardingStatusProduction,
	}
}

func (s *FormalPoolOnboardingService) browserURL(nonce string) string {
	path := formalPoolBrowserEgressPublicPathPrefix + strings.TrimSpace(nonce)
	if s.publicURLPrefix == "" {
		return path
	}
	return s.publicURLPrefix + path
}

func formalPoolSafeBucket(proxyRef string) string {
	suffix := strings.TrimPrefix(formalPoolSafeRef("bucket", proxyRef), "ref_")
	if len(suffix) > 16 {
		suffix = suffix[:16]
	}
	return "claude-" + suffix
}

func formalPoolSafeRef(scope, raw string) string {
	return scopedStickyHMAC("formal_pool_"+scope, strings.TrimSpace(raw))
}

var formalPoolSensitiveKeyFragments = []string{
	"password", "token", "refresh", "authorization", "x-api-key", "api_key", "cookie", "sessionkey", "session_key", "code", "email", "account_uuid", "org_uuid", "organization_uuid", "proxy_url", "raw_body", "raw_prompt", "raw_cch", "cch",
}

func FormalPoolContainsSensitive(v any) bool {
	return formalPoolContainsSensitive(reflect.ValueOf(v), "")
}

func FormalPoolSensitivePathForTest(v any) string {
	return formalPoolSensitivePath(reflect.ValueOf(v), "")
}

func formalPoolSensitivePath(v reflect.Value, key string) string {
	if key != "" {
		lk := strings.ToLower(key)
		if !(strings.HasSuffix(lk, "_present") || strings.HasSuffix(lk, "_bucket") || strings.HasSuffix(lk, "_ref") || strings.Contains(lk, "_contains_")) {
			for _, frag := range formalPoolSensitiveKeyFragments {
				if strings.Contains(lk, frag) {
					return key
				}
			}
		}
	}
	if !v.IsValid() {
		return ""
	}
	if v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return ""
		}
		return formalPoolSensitivePath(v.Elem(), key)
	}
	switch v.Kind() {
	case reflect.Map:
		for _, mk := range v.MapKeys() {
			if p := formalPoolSensitivePath(v.MapIndex(mk), fmt.Sprint(mk.Interface())); p != "" {
				return key + "." + p
			}
		}
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			if t.Field(i).PkgPath != "" {
				continue
			}
			name := t.Field(i).Name
			if tag := t.Field(i).Tag.Get("json"); tag != "" {
				name = strings.Split(tag, ",")[0]
			}
			if p := formalPoolSensitivePath(v.Field(i), name); p != "" {
				return key + "." + p
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if p := formalPoolSensitivePath(v.Index(i), key); p != "" {
				return p
			}
		}
	case reflect.String:
		val := strings.ToLower(v.String())
		if strings.Contains(val, "bearer ") || strings.Contains(val, "sk-") || strings.Contains(val, "refresh_token") || strings.Contains(val, "access_token") {
			return key
		}
	}
	return ""
}

func formalPoolContainsSensitive(v reflect.Value, key string) bool {
	if key != "" {
		lk := strings.ToLower(key)
		if strings.HasSuffix(lk, "_present") || strings.HasSuffix(lk, "_bucket") || strings.HasSuffix(lk, "_ref") || strings.Contains(lk, "_contains_") {
			// Presence/bucket/ref fields are the allowed redacted form for identity checks.
		} else {
			for _, frag := range formalPoolSensitiveKeyFragments {
				if strings.Contains(lk, frag) {
					return true
				}
			}
		}
	}
	if !v.IsValid() {
		return false
	}
	if v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return false
		}
		return formalPoolContainsSensitive(v.Elem(), key)
	}
	switch v.Kind() {
	case reflect.Map:
		for _, mk := range v.MapKeys() {
			if formalPoolContainsSensitive(v.MapIndex(mk), fmt.Sprint(mk.Interface())) {
				return true
			}
		}
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			if t.Field(i).PkgPath != "" {
				continue
			}
			name := t.Field(i).Name
			if tag := t.Field(i).Tag.Get("json"); tag != "" {
				name = strings.Split(tag, ",")[0]
			}
			if formalPoolContainsSensitive(v.Field(i), name) {
				return true
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if formalPoolContainsSensitive(v.Index(i), key) {
				return true
			}
		}
	case reflect.String:
		val := strings.ToLower(v.String())
		if strings.Contains(val, "bearer ") || strings.Contains(val, "sk-") || strings.Contains(val, "refresh_token") || strings.Contains(val, "access_token") {
			return true
		}
	}
	return false
}
