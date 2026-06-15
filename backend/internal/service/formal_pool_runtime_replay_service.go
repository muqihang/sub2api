package service

import (
	"context"
	"fmt"
	"strings"
	"time"

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
	Accounts         FormalPoolRuntimeRegistrationReplayAccountStore
	Proxy            FormalPoolOperationsProxyStore
	CCGatewayRuntime FormalPoolCCGatewayRuntimeRegistrar
	Now              func() time.Time
}

type FormalPoolRuntimeRegistrationReplayService struct {
	accounts         FormalPoolRuntimeRegistrationReplayAccountStore
	proxy            FormalPoolOperationsProxyStore
	ccGatewayRuntime FormalPoolCCGatewayRuntimeRegistrar
	now              func() time.Time
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
	return &FormalPoolRuntimeRegistrationReplayService{accounts: deps.Accounts, proxy: deps.Proxy, ccGatewayRuntime: deps.CCGatewayRuntime, now: now}
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
			_ = s.markReplayFailure(ctx, account, "runtime_replay_input_missing")
			continue
		}
		if err := s.ccGatewayRuntime.RegisterCCGatewayRuntime(ctx, reg); err != nil {
			out.Failed++
			_ = s.markReplayFailure(ctx, account, "runtime_replay_registration_failed")
			continue
		}
		stamp := formalPoolTimestamp(s.now())
		_, err = s.accounts.UpdateFormalPoolAccountState(ctx, account.ID, account.Schedulable, StatusActive, map[string]any{
			FormalPoolExtraRuntimeRegistered:        "true",
			FormalPoolExtraRuntimeRegisteredAt:      stamp,
			FormalPoolExtraLastFailureOrigin:        "",
			FormalPoolExtraLastFailureCode:          "",
			FormalPoolExtraLastFailureSource:        "",
			FormalPoolExtraLastCCGatewayErrorCode:   "",
			FormalPoolExtraOnboardingStageUpdatedAt: stamp,
		})
		if err != nil {
			out.Failed++
			continue
		}
		out.Registered++
	}
	return out, nil
}

func formalPoolRuntimeReplayCandidate(account *Account) bool {
	return formalPoolRuntimeReplayEligible(account)
}

func formalPoolRuntimeReplayEligible(account *Account) bool {
	if !serviceFormalPoolAccount(account) || account.Status != StatusActive || account.ProxyID == nil || *account.ProxyID <= 0 {
		return false
	}
	switch FormalPoolAccountStage(account) {
	case FormalPoolStageHealthcheckPassed, FormalPoolStageWarming, FormalPoolStageProduction:
		return true
	default:
		return false
	}
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
