package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
)

type formalPoolRuntimeRegistrationReplayAccountStore struct {
	repo AccountRepository
}

func NewFormalPoolRuntimeRegistrationReplayAccountStore(repo AccountRepository) FormalPoolRuntimeRegistrationReplayAccountStore {
	return &formalPoolRuntimeRegistrationReplayAccountStore{repo: repo}
}

func (s *formalPoolRuntimeRegistrationReplayAccountStore) ListFormalPoolRuntimeReplayCandidates(ctx context.Context) ([]*Account, error) {
	if s == nil || s.repo == nil {
		return nil, infraerrors.ServiceUnavailable("FORMAL_POOL_RUNTIME_REPLAY_UNAVAILABLE", "formal pool runtime replay account store is unavailable")
	}
	accounts, err := s.repo.ListByPlatform(ctx, PlatformAnthropic)
	if err != nil {
		return nil, err
	}
	out := make([]*Account, 0, len(accounts))
	for i := range accounts {
		account := accounts[i]
		if formalPoolRuntimeReplayEligible(&account) {
			copy := account
			out = append(out, &copy)
		}
	}
	return out, nil
}

func (s *formalPoolRuntimeRegistrationReplayAccountStore) UpdateFormalPoolAccountState(ctx context.Context, id int64, schedulable bool, status string, extra map[string]any) (*Account, error) {
	if s == nil || s.repo == nil {
		return nil, infraerrors.ServiceUnavailable("FORMAL_POOL_RUNTIME_REPLAY_UNAVAILABLE", "formal pool runtime replay account store is unavailable")
	}
	account, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	account.Schedulable = schedulable
	if strings.TrimSpace(status) != "" {
		account.Status = status
	}
	merged := cloneCredentials(account.Extra)
	if merged == nil {
		merged = map[string]any{}
	}
	for k, v := range extra {
		merged[k] = v
	}
	account.Extra = merged
	if err := s.repo.Update(ctx, account); err != nil {
		return nil, err
	}
	return s.repo.GetByID(ctx, id)
}

type FormalPoolRuntimeRegistrationReplayAccountStore interface {
	ListFormalPoolRuntimeReplayCandidates(ctx context.Context) ([]*Account, error)
	UpdateFormalPoolAccountState(ctx context.Context, id int64, schedulable bool, status string, extra map[string]any) (*Account, error)
}

type FormalPoolRuntimeRegistrationReplayDeps struct {
	Accounts                                          FormalPoolRuntimeRegistrationReplayAccountStore
	Proxy                                             FormalPoolOperationsProxyStore
	CCGatewayRuntime                                  FormalPoolCCGatewayRuntimeRegistrar
	Now                                               func() time.Time
	CCGatewayContextAttestationSecret                 string
	CCGatewayStickySessionHMACKey                     string
	CCGatewayClaudePlatformAWSWorkspaceBindingHMACKey string
}

type FormalPoolRuntimeRegistrationReplayService struct {
	accounts                                          FormalPoolRuntimeRegistrationReplayAccountStore
	proxy                                             FormalPoolOperationsProxyStore
	ccGatewayRuntime                                  FormalPoolCCGatewayRuntimeRegistrar
	now                                               func() time.Time
	ccGatewayContextAttestationSecret                 string
	ccGatewayStickySessionHMACKey                     string
	ccGatewayClaudePlatformAWSWorkspaceBindingHMACKey string
}

type FormalPoolRuntimeRegistrationReplayResult struct {
	Scanned    int `json:"scanned"`
	Registered int `json:"registered"`
	Failed     int `json:"failed"`
}

func NewFormalPoolRuntimeRegistrationReplayService(deps FormalPoolRuntimeRegistrationReplayDeps) *FormalPoolRuntimeRegistrationReplayService {
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	return &FormalPoolRuntimeRegistrationReplayService{
		accounts:                          deps.Accounts,
		proxy:                             deps.Proxy,
		ccGatewayRuntime:                  deps.CCGatewayRuntime,
		now:                               now,
		ccGatewayContextAttestationSecret: strings.TrimSpace(deps.CCGatewayContextAttestationSecret),
		ccGatewayStickySessionHMACKey:     strings.TrimSpace(deps.CCGatewayStickySessionHMACKey),
		ccGatewayClaudePlatformAWSWorkspaceBindingHMACKey: strings.TrimSpace(deps.CCGatewayClaudePlatformAWSWorkspaceBindingHMACKey),
	}
}

func (s *FormalPoolRuntimeRegistrationReplayService) Replay(ctx context.Context) (FormalPoolRuntimeRegistrationReplayResult, error) {
	var out FormalPoolRuntimeRegistrationReplayResult
	if s == nil || s.accounts == nil {
		return out, infraerrors.ServiceUnavailable("FORMAL_POOL_RUNTIME_REPLAY_UNAVAILABLE", "formal pool runtime replay account store is unavailable")
	}
	accounts, err := s.accounts.ListFormalPoolRuntimeReplayCandidates(ctx)
	if err != nil {
		return out, err
	}
	if s.ccGatewayRuntime == nil {
		for _, account := range accounts {
			if !formalPoolRuntimeReplayEligible(account) {
				continue
			}
			out.Scanned++
			out.Failed++
			_ = s.markReplayFailure(ctx, account, "runtime_replay_registrar_unavailable")
		}
		return out, infraRuntimeRegistrationUnavailable()
	}

	for _, account := range accounts {
		if !formalPoolRuntimeReplayCandidate(account) {
			continue
		}
		out.Scanned++
		reg, err := s.runtimeReplayRegistrationInput(ctx, account)
		if err != nil {
			out.Failed++
			_ = s.markReplayFailure(ctx, account, safeRuntimeReplayFailureCode(err, "runtime_replay_input_missing"))
			continue
		}
		if err := s.ccGatewayRuntime.RegisterCCGatewayRuntime(ctx, reg); err != nil {
			out.Failed++
			_ = s.markReplayFailure(ctx, account, "runtime_replay_registration_failed")
			continue
		}
		stamp := formalPoolTimestamp(s.now())
		_, err = s.accounts.UpdateFormalPoolAccountState(ctx, account.ID, account.Schedulable, StatusActive, formalPoolRuntimeReplaySuccessExtra(account, stamp))
		if err != nil {
			out.Failed++
			continue
		}
		out.Registered++
	}
	return out, nil
}

func safeRuntimeReplayFailureCode(err error, fallback string) string {
	code := strings.TrimSpace(infraerrors.Reason(err))
	if code == "" {
		code = strings.TrimSpace(fallback)
	}
	if code == "" || formalPoolUnsafeDiagnosticText(code) {
		return "runtime_replay_input_missing"
	}
	return code
}

func formalPoolRuntimeReplayCandidate(account *Account) bool {
	return formalPoolRuntimeReplayEligible(account)
}

func formalPoolRuntimeReplayEligible(account *Account) bool {
	if !serviceFormalPoolAccount(account) || account.Status != StatusActive || account.ProxyID == nil || *account.ProxyID <= 0 {
		return false
	}
	switch FormalPoolAccountStage(account) {
	case FormalPoolStageRuntimeRegistered, FormalPoolStageHealthcheckPassed, FormalPoolStageWarming, FormalPoolStageProduction:
		return true
	default:
		return false
	}
}

func formalPoolRuntimeReplaySuccessExtra(account *Account, stamp string) map[string]any {
	extra := map[string]any{
		FormalPoolExtraRuntimeRegistered:        "true",
		FormalPoolExtraRuntimeRegisteredAt:      stamp,
		FormalPoolExtraLastFailureOrigin:        "",
		FormalPoolExtraLastFailureCode:          "",
		FormalPoolExtraLastFailureSource:        "",
		FormalPoolExtraLastCCGatewayErrorCode:   "",
		FormalPoolExtraOnboardingStageUpdatedAt: stamp,
	}
	if formalPoolRuntimeReplayStaleIdentityFailure(account) {
		extra[FormalPoolExtraHealthcheckSafeErrorCode] = ""
		extra[FormalPoolExtraHealthcheckSafeErrorBucket] = ""
		extra[FormalPoolExtraOnboardingLastErrorCode] = ""
		extra[FormalPoolExtraOnboardingLastErrorBucket] = ""
	}
	return extra
}

func formalPoolRuntimeReplayStaleIdentityFailure(account *Account) bool {
	if account == nil {
		return false
	}
	for _, key := range []string{
		FormalPoolExtraLastFailureCode,
		FormalPoolExtraLastCCGatewayErrorCode,
		FormalPoolExtraHealthcheckSafeErrorCode,
		FormalPoolExtraOnboardingLastErrorCode,
		FormalPoolExtraOnboardingLastErrorBucket,
	} {
		value := strings.ToLower(strings.TrimSpace(account.GetExtraString(key)))
		if value == "missing_account_identity" || value == "missing_identity" {
			return true
		}
	}
	return false
}

func (s *FormalPoolRuntimeRegistrationReplayService) ensureRuntimeIdentityEvidence(ctx context.Context, account *Account) (*Account, error) {
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
	proxyIdentityRef := ""
	if account.ProxyID != nil && *account.ProxyID > 0 {
		proxyIdentityRef = formalPoolSafeRef("proxy", fmt.Sprintf("%d", *account.ProxyID))
	}
	generation := strings.TrimSpace(account.GetExtraString(FormalPoolExtraCredentialGeneration))
	if generation == "" {
		generation = "1"
	}
	authorityExtra, err := claudePlatformAWSRuntimeAuthorityExtra(account, s.claudePlatformAWSCCGatewayAuthorityConfig(), accountRef, egressBucket, proxyIdentityRef)
	if err != nil {
		return account, err
	}
	identityAccount := cloneClaudePlatformAWSAccountWithExtraOverlay(account, authorityExtra)
	identity := formalPoolRuntimeIdentityExtraForAccount(identityAccount, accountRef, proxyIdentityRef, s.ccGatewayRuntimeBindingSecret(), generation)
	if len(authorityExtra) == 0 &&
		account.GetExtraString(ccGatewayExtraAccountRef) == accountRef &&
		strings.TrimSpace(resolveCCGatewayEgressBucket(account)) == egressBucket &&
		strings.TrimSpace(account.GetExtraString(ccGatewayExtraCredentialRef)) == stringFromMap(identity, ccGatewayExtraCredentialRef) &&
		strings.TrimSpace(account.GetExtraString(ccGatewayExtraCredentialBindingHMAC)) == stringFromMap(identity, ccGatewayExtraCredentialBindingHMAC) &&
		strings.TrimSpace(account.GetExtraString(ccGatewayExtraProxyIdentityRef)) == stringFromMap(identity, ccGatewayExtraProxyIdentityRef) &&
		strings.TrimSpace(account.GetExtraString(ccGatewayExtraPersonaProfile)) == stringFromMap(identity, ccGatewayExtraPersonaProfile) &&
		strings.TrimSpace(account.GetExtraString("claude_code_device_id")) == stringFromMap(identity, "claude_code_device_id") {
		return account, nil
	}
	extra := map[string]any{
		ccGatewayExtraAccountRef:   accountRef,
		ccGatewayExtraEgressBucket: egressBucket,
	}
	for k, v := range authorityExtra {
		extra[k] = v
	}
	for k, v := range identity {
		extra[k] = v
	}
	updated, err := s.accounts.UpdateFormalPoolAccountState(ctx, account.ID, false, StatusActive, extra)
	if err != nil {
		return account, err
	}
	return updated, nil
}

func (s *FormalPoolRuntimeRegistrationReplayService) runtimeReplayRegistrationInput(ctx context.Context, account *Account) (FormalPoolCCGatewayRuntimeRegistration, error) {
	if account == nil || account.ProxyID == nil {
		return FormalPoolCCGatewayRuntimeRegistration{}, infraerrors.BadRequest("RUNTIME_REPLAY_INPUT_MISSING", "runtime replay requires proxy")
	}
	updated, err := s.ensureRuntimeIdentityEvidence(ctx, account)
	if err != nil {
		return FormalPoolCCGatewayRuntimeRegistration{}, err
	}
	if updated != nil {
		account = updated
	}
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
	credentialRef := strings.TrimSpace(ccGatewayCredentialRef(account))
	if !isSafeLedgerRef(credentialRef) {
		return FormalPoolCCGatewayRuntimeRegistration{}, infraerrors.BadRequest("CC_GATEWAY_CREDENTIAL_REF_REQUIRED", "safe cc gateway credential ref is required")
	}
	credentialBinding := strings.TrimSpace(ccGatewayCredentialBindingHMAC(account))
	if !ccGatewayCredentialBindingHMACRe.MatchString(credentialBinding) {
		return FormalPoolCCGatewayRuntimeRegistration{}, infraerrors.BadRequest("CC_GATEWAY_CREDENTIAL_BINDING_REQUIRED", "safe cc gateway credential binding is required")
	}
	tokenType, credentialProof := ccGatewaySelectedCredentialBindingMaterial(account)
	if strings.TrimSpace(tokenType) == "" || strings.TrimSpace(credentialProof) == "" {
		return FormalPoolCCGatewayRuntimeRegistration{}, infraerrors.BadRequest("CC_GATEWAY_CREDENTIAL_PROOF_REQUIRED", "cc gateway credential proof is required")
	}
	proxyIdentityRef := strings.TrimSpace(ccGatewayProxyIdentityRef(account))
	if !isSafeLedgerRef(proxyIdentityRef) {
		return FormalPoolCCGatewayRuntimeRegistration{}, infraerrors.BadRequest("CC_GATEWAY_PROXY_IDENTITY_REF_REQUIRED", "safe cc gateway proxy identity ref is required")
	}
	deviceID := strings.TrimSpace(ccGatewayDeviceID(account))
	if !claudeCodeDeviceIDRe.MatchString(deviceID) {
		return FormalPoolCCGatewayRuntimeRegistration{}, infraerrors.BadRequest("CC_GATEWAY_DEVICE_ID_REQUIRED", "cc gateway account-owned device id is required")
	}
	reg := FormalPoolCCGatewayRuntimeRegistration{
		AccountRef:            accountRef,
		CredentialRef:         credentialRef,
		CredentialBindingHMAC: credentialBinding,
		TokenType:             tokenType,
		CredentialProof:       credentialProof,
		EgressBucket:          egressBucket,
		ProxyURL:              normalized,
		ProxyRef:              proxyIdentityRef,
		PolicyVersion:         ccGatewayAnthropicPolicyVersion,
		PersonaVariant:        fmt.Sprintf("claude-code-%s-macos-local", ccGatewayAnthropicPolicyVersion),
		SessionPolicy:         "preserve_downstream_session_id",
		DeviceID:              strings.ToLower(deviceID),
	}
	if err := applyClaudePlatformAWSRuntimeRegistrationFieldsWithCCGatewayConfig(account, &reg, s.claudePlatformAWSCCGatewayAuthorityConfig()); err != nil {
		return FormalPoolCCGatewayRuntimeRegistration{}, err
	}
	return reg, nil
}

func (s *FormalPoolRuntimeRegistrationReplayService) claudePlatformAWSCCGatewayAuthorityConfig() *config.Config {
	if s == nil {
		return nil
	}
	contextSecret := strings.TrimSpace(s.ccGatewayContextAttestationSecret)
	stickySecret := strings.TrimSpace(s.ccGatewayStickySessionHMACKey)
	workspaceBindingSecret := strings.TrimSpace(s.ccGatewayClaudePlatformAWSWorkspaceBindingHMACKey)
	return &config.Config{Gateway: config.GatewayConfig{CCGateway: config.GatewayCCGatewayConfig{
		Enabled:                                  true,
		ContextAttestationSecret:                 contextSecret,
		StickySessionHMACKey:                     stickySecret,
		ClaudePlatformAWSWorkspaceBindingHMACKey: workspaceBindingSecret,
	}}}
}

func (s *FormalPoolRuntimeRegistrationReplayService) ccGatewayRuntimeBindingSecret() string {
	if s == nil {
		return ""
	}
	if secret := strings.TrimSpace(s.ccGatewayContextAttestationSecret); secret != "" {
		return secret
	}
	return "formal-pool-runtime-binding-local-test-secret"
}

func (s *FormalPoolRuntimeRegistrationReplayService) markReplayFailure(ctx context.Context, account *Account, code string) error {
	if account == nil || s == nil || s.accounts == nil {
		return nil
	}
	_, err := s.accounts.UpdateFormalPoolAccountState(ctx, account.ID, false, StatusActive, map[string]any{
		FormalPoolExtraRuntimeRegistered:      "false",
		FormalPoolExtraRuntimeRegisteredAt:    "",
		FormalPoolExtraLastFailureOrigin:      string(FormalPoolFailureOriginCCGateway),
		FormalPoolExtraLastFailureCode:        code,
		FormalPoolExtraLastFailureSource:      "formal_pool_runtime_replay",
		FormalPoolExtraLastCCGatewayErrorCode: "",
	})
	return err
}

type FormalPoolRuntimeRegistrationStartupReplay struct {
	replay *FormalPoolRuntimeRegistrationReplayService
	done   bool
}

type FormalPoolRuntimeRegistrationStartupReplayResult struct {
	FormalPoolRuntimeRegistrationReplayResult
	Skipped bool  `json:"skipped"`
	Error   error `json:"-"`
}

func NewFormalPoolRuntimeRegistrationStartupReplay(replay *FormalPoolRuntimeRegistrationReplayService) *FormalPoolRuntimeRegistrationStartupReplay {
	return &FormalPoolRuntimeRegistrationStartupReplay{replay: replay}
}

func (r *FormalPoolRuntimeRegistrationStartupReplay) Start(ctx context.Context) FormalPoolRuntimeRegistrationStartupReplayResult {
	if r == nil || r.replay == nil {
		return FormalPoolRuntimeRegistrationStartupReplayResult{Skipped: true}
	}
	if r.done {
		return FormalPoolRuntimeRegistrationStartupReplayResult{Skipped: true}
	}
	r.done = true
	result, err := r.replay.Replay(ctx)
	return FormalPoolRuntimeRegistrationStartupReplayResult{FormalPoolRuntimeRegistrationReplayResult: result, Error: err}
}
