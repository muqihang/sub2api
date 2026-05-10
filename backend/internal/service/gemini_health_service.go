package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	geminiHealthStatusHealthy  = "healthy"
	geminiHealthStatusDegraded = "degraded"

	geminiWarningTokenCachePlaintextDetected = "token_cache_plaintext_detected"
	geminiWarningGoogleOneDefaultTier        = "google_one_default_tier_fallback"
	geminiWarningMemorySessionStore          = "memory_session_store"
	geminiWarningMissingRequiredProjectID    = "missing_required_project_id"
	geminiWarningUnsafePlaintextCredentials  = "unsafe_plaintext_credentials"
)

type GeminiPolicySnapshot struct {
	ProductionMode                  bool   `json:"production_mode"`
	TokenCacheMode                  string `json:"token_cache_mode"`
	SessionStore                    string `json:"session_store"`
	ProjectIDFallbackToAIStudio     bool   `json:"project_id_fallback_to_ai_studio"`
	UnauthorizedClientRetryFallback bool   `json:"unauthorized_client_retry_fallback"`
	GoogleOneDefaultTierFallback    bool   `json:"google_one_default_tier_fallback"`
	GoogleOneDefaultTierVisible     bool   `json:"google_one_default_tier_visible"`
	ThoughtSignatureSessionSafety   bool   `json:"thought_signature_session_safety"`
	RequireSafeOAuthSessionStore    bool   `json:"require_safe_oauth_session_store"`
}

type GeminiHealthSnapshot struct {
	GatewayStatus    string               `json:"gateway_status"`
	OAuthStatus      string               `json:"oauth_status"`
	GeminiAccounts   int64                `json:"gemini_accounts_total"`
	AccountsByFamily map[string]int64     `json:"accounts_by_family"`
	Policy           GeminiPolicySnapshot `json:"policy"`
	WarningCodes     []string             `json:"warning_codes"`
	DegradedReason   string               `json:"degraded_reason,omitempty"`
}

type GeminiVerifySnapshot struct {
	AccountID                   int64                  `json:"account_id"`
	AccountName                 string                 `json:"account_name"`
	RuntimeContract             *GeminiRuntimeContract `json:"runtime_contract,omitempty"`
	ProjectID                   string                 `json:"project_id,omitempty"`
	ProjectIDStatus             string                 `json:"project_id_status"`
	ProjectIDReason             string                 `json:"project_id_reason,omitempty"`
	TierID                      string                 `json:"tier_id,omitempty"`
	TierStatus                  string                 `json:"tier_status"`
	TokenCacheMode              string                 `json:"token_cache_mode"`
	TokenCacheState             string                 `json:"token_cache_state"`
	TokenCacheReason            string                 `json:"token_cache_reason"`
	OAuthState                  string                 `json:"oauth_state"`
	OAuthReason                 string                 `json:"oauth_reason"`
	SessionStore                string                 `json:"session_store"`
	StickySessionSafetyRequired bool                   `json:"sticky_session_safety_required"`
	WarningCodes                []string               `json:"warning_codes,omitempty"`
}

type GeminiHealthService struct {
	accountRepo        AccountRepository
	geminiOAuthService *GeminiOAuthService
	cfg                *config.Config
}

func NewGeminiHealthService(accountRepo AccountRepository, geminiOAuthService *GeminiOAuthService, cfg *config.Config) *GeminiHealthService {
	return &GeminiHealthService{
		accountRepo:        accountRepo,
		geminiOAuthService: geminiOAuthService,
		cfg:                cfg,
	}
}

func (s *GeminiHealthService) BuildHealthSnapshot(ctx context.Context) (*GeminiHealthSnapshot, error) {
	accounts, err := s.accountRepo.ListByPlatform(ctx, PlatformGemini)
	if err != nil {
		return nil, err
	}

	snapshot := &GeminiHealthSnapshot{
		GatewayStatus:    geminiHealthStatusHealthy,
		OAuthStatus:      geminiHealthStatusHealthy,
		GeminiAccounts:   int64(len(accounts)),
		AccountsByFamily: map[string]int64{},
		Policy:           s.policySnapshot(),
		WarningCodes:     []string{},
	}

	warningSet := map[string]struct{}{}
	for _, account := range accounts {
		accountCopy := account
		contract, contractErr := ResolveGeminiRuntimeContract(&accountCopy)
		family := "unknown"
		if contractErr == nil && contract != nil {
			family = string(contract.AccountFamily)
		}
		snapshot.AccountsByFamily[family]++

		for _, warning := range s.accountWarningCodes(&accountCopy, contract) {
			warningSet[warning] = struct{}{}
		}
	}

	if s.cfg != nil && s.cfg.Gemini.ProductionMode && s.sessionStoreMode() == "memory" {
		warningSet[geminiWarningMemorySessionStore] = struct{}{}
	}

	snapshot.WarningCodes = sortedWarningCodes(warningSet)
	if snapshot.WarningCodes == nil {
		snapshot.WarningCodes = []string{}
	}
	if len(snapshot.WarningCodes) > 0 {
		snapshot.GatewayStatus = geminiHealthStatusDegraded
		snapshot.OAuthStatus = geminiHealthStatusDegraded
		snapshot.DegradedReason = snapshot.WarningCodes[0]
	}

	return snapshot, nil
}

func (s *GeminiHealthService) BuildVerifySnapshot(ctx context.Context, accountID int64) (*GeminiVerifySnapshot, error) {
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, ErrAccountNotFound
	}
	if account.Platform != PlatformGemini {
		return nil, fmt.Errorf("account is not a gemini account")
	}

	contract, err := ResolveGeminiRuntimeContract(account)
	if err != nil {
		return nil, infraerrors.BadRequest("INVALID_GEMINI_RUNTIME_CONTRACT", err.Error())
	}
	projectID, projectStatus, projectReason := s.projectIDStatus(account, contract)
	tierID, tierStatus := s.tierStatus(account)
	warnings := s.accountWarningCodes(account, contract)
	accessor := NewGeminiCredentialsAccessor(s.cfg, nil)
	unsafePlaintext := accessor.HasUnsafePlaintextCredentials(account)

	return &GeminiVerifySnapshot{
		AccountID:                   account.ID,
		AccountName:                 account.Name,
		RuntimeContract:             contract,
		ProjectID:                   projectID,
		ProjectIDStatus:             projectStatus,
		ProjectIDReason:             projectReason,
		TierID:                      tierID,
		TierStatus:                  tierStatus,
		TokenCacheMode:              geminiTokenCacheMode(s.cfg),
		TokenCacheState:             defaultGeminiState(readGeminiAccountState(account, geminiTokenCacheStateKey)),
		TokenCacheReason:            readGeminiAccountState(account, geminiTokenCacheReasonKey),
		OAuthState:                  s.oauthState(account, unsafePlaintext),
		OAuthReason:                 s.oauthReason(account, unsafePlaintext),
		SessionStore:                s.sessionStoreMode(),
		StickySessionSafetyRequired: s.cfg != nil && s.cfg.Gemini.RequireThoughtSignatureSessionSafety,
		WarningCodes:                warnings,
	}, nil
}

func (s *GeminiHealthService) policySnapshot() GeminiPolicySnapshot {
	return GeminiPolicySnapshot{
		ProductionMode:                  geminiProductionModeEnabled(s.cfg),
		TokenCacheMode:                  geminiTokenCacheMode(s.cfg),
		SessionStore:                    s.sessionStoreMode(),
		ProjectIDFallbackToAIStudio:     geminiAllowsProjectIDFallbackToAIStudio(s.cfg),
		UnauthorizedClientRetryFallback: geminiAllowsUnauthorizedClientRetry(s.cfg),
		GoogleOneDefaultTierFallback:    geminiGoogleOneDefaultTierFallbackPolicy(s.cfg).Allow,
		GoogleOneDefaultTierVisible:     geminiGoogleOneDefaultTierFallbackPolicy(s.cfg).VisibleDegraded,
		ThoughtSignatureSessionSafety:   s.cfg != nil && s.cfg.Gemini.RequireThoughtSignatureSessionSafety,
		RequireSafeOAuthSessionStore:    s.cfg != nil && s.cfg.Gemini.RequireSafeOAuthSessionStore,
	}
}

func (s *GeminiHealthService) sessionStoreMode() string {
	if s == nil || s.geminiOAuthService == nil {
		return "unknown"
	}
	return s.geminiOAuthService.SessionStoreMode()
}

func (s *GeminiHealthService) accountWarningCodes(account *Account, contract *GeminiRuntimeContract) []string {
	set := map[string]struct{}{}
	if readGeminiAccountState(account, geminiTokenCacheStateKey) == geminiTokenCacheStateDegraded {
		set[geminiWarningTokenCachePlaintextDetected] = struct{}{}
	}
	if readGeminiAccountState(account, geminiOAuthReasonKey) == geminiOAuthReasonGoogleOneDefaultTierFallback {
		set[geminiWarningGoogleOneDefaultTier] = struct{}{}
	}
	if NewGeminiCredentialsAccessor(s.cfg, nil).HasUnsafePlaintextCredentials(account) {
		set[geminiWarningUnsafePlaintextCredentials] = struct{}{}
	}
	if contract != nil && contract.RequiresProjectID {
		if _, status, _ := s.projectIDStatus(account, contract); status == "required_missing" {
			set[geminiWarningMissingRequiredProjectID] = struct{}{}
		}
	}
	return sortedWarningCodes(set)
}

func (s *GeminiHealthService) projectIDStatus(account *Account, contract *GeminiRuntimeContract) (string, string, string) {
	projectID := ""
	accessor := NewGeminiCredentialsAccessor(s.cfg, nil)
	if account != nil {
		var err error
		projectID, err = accessor.GeminiProjectID(account)
		projectID = strings.TrimSpace(projectID)
		if err != nil && account.Type == AccountTypeServiceAccount {
			return "", "unreadable", err.Error()
		}
		if projectID == "" {
			projectID = strings.TrimSpace(account.GetCredential("project_id"))
		}
	}
	switch {
	case projectID != "":
		return projectID, "present", ""
	case contract != nil && contract.RequiresProjectID:
		return "", "required_missing", ""
	default:
		return "", "optional_empty", ""
	}
}

func (s *GeminiHealthService) tierStatus(account *Account) (string, string) {
	tierID := ""
	if account != nil {
		tierID = strings.TrimSpace(account.GetCredential("tier_id"))
	}
	if readGeminiAccountState(account, geminiOAuthReasonKey) == geminiOAuthReasonGoogleOneDefaultTierFallback {
		if tierID == "" {
			return GeminiTierGoogleOneFree, "default_fallback"
		}
		return tierID, "default_fallback"
	}
	if tierID == "" {
		return "", "missing"
	}
	return tierID, "present"
}

func readGeminiAccountState(account *Account, key string) string {
	if account == nil {
		return ""
	}
	if account.Extra != nil {
		if raw, ok := account.Extra[key].(string); ok && strings.TrimSpace(raw) != "" {
			return strings.TrimSpace(raw)
		}
	}
	return strings.TrimSpace(account.GetCredential(key))
}

func sortedWarningCodes(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for code := range set {
		out = append(out, code)
	}
	sort.Strings(out)
	return out
}

func defaultGeminiState(value string) string {
	if strings.TrimSpace(value) == "" {
		return "ok"
	}
	return strings.TrimSpace(value)
}

func (s *GeminiHealthService) oauthState(account *Account, unsafePlaintext bool) string {
	if unsafePlaintext {
		return geminiOAuthStateDegraded
	}
	return defaultGeminiState(readGeminiAccountState(account, geminiOAuthStateKey))
}

func (s *GeminiHealthService) oauthReason(account *Account, unsafePlaintext bool) string {
	if unsafePlaintext {
		return geminiOAuthReasonUnsafePlaintextCredentials
	}
	return readGeminiAccountState(account, geminiOAuthReasonKey)
}
