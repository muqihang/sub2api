package service

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

const (
	geminiOAuthStateKey   = "gemini_oauth_state"
	geminiOAuthReasonKey  = "gemini_oauth_reason"
	geminiOAuthTierSource = "gemini_tier_source"

	geminiOAuthStateDegraded = "degraded"

	geminiTokenCacheStateKey      = "gemini_token_cache_state"
	geminiTokenCacheReasonKey     = "gemini_token_cache_reason"
	geminiTokenCacheStateDegraded = "degraded"

	geminiOAuthReasonGoogleOneDefaultTierFallback = "google_one_default_tier_fallback"
	geminiOAuthReasonUnsafePlaintextCredentials   = "unsafe_plaintext_credentials"
	geminiTierSourceDefaultFallback               = "default_fallback"
	geminiDriveStorageLimitKey                    = "drive_storage_limit"
	geminiDriveStorageUsageKey                    = "drive_storage_usage"
	geminiDriveTierUpdatedAtKey                   = "drive_tier_updated_at"

	geminiTokenCacheReasonPlaintextEntryPresent = "unsafe_plaintext_entry_present"
)

type geminiGoogleOneTierFallbackDecision struct {
	Allow           bool
	VisibleDegraded bool
}

func geminiAllowsProjectIDFallbackToAIStudio(cfg *config.Config) bool {
	if cfg == nil {
		return true
	}
	if geminiProductionModeEnabled(cfg) {
		return cfg.Gemini.AllowProjectIDFallbackToAIStudio
	}
	return true
}

func geminiAllowsUnauthorizedClientRetry(cfg *config.Config) bool {
	if cfg == nil {
		return true
	}
	if geminiProductionModeEnabled(cfg) {
		return cfg.Gemini.AllowUnauthorizedClientRetryFallback
	}
	return true
}

func geminiGoogleOneDefaultTierFallbackPolicy(cfg *config.Config) geminiGoogleOneTierFallbackDecision {
	allow := true
	if cfg != nil && geminiProductionModeEnabled(cfg) {
		allow = cfg.Gemini.AllowGoogleOneDefaultTierFallback
	}
	return geminiGoogleOneTierFallbackDecision{
		Allow:           allow,
		VisibleDegraded: geminiProductionModeEnabled(cfg) && allow,
	}
}

func geminiThoughtSignatureSessionSafetyEnabled(cfg *config.Config) bool {
	if cfg == nil {
		return true
	}
	if geminiProductionModeEnabled(cfg) {
		return cfg.Gemini.RequireThoughtSignatureSessionSafety
	}
	return cfg.Gemini.RequireThoughtSignatureSessionSafety
}

func geminiMarkGoogleOneDefaultTierFallback(tokenInfo *GeminiTokenInfo) {
	if tokenInfo == nil {
		return
	}
	if tokenInfo.Extra == nil {
		tokenInfo.Extra = make(map[string]any)
	}
	tokenInfo.Extra[geminiOAuthStateKey] = geminiOAuthStateDegraded
	tokenInfo.Extra[geminiOAuthReasonKey] = geminiOAuthReasonGoogleOneDefaultTierFallback
	tokenInfo.Extra[geminiOAuthTierSource] = geminiTierSourceDefaultFallback
}

func geminiGoogleOneStoredExtra(account *Account) map[string]any {
	if account == nil {
		return nil
	}
	keys := []string{
		geminiOAuthStateKey,
		geminiOAuthReasonKey,
		geminiOAuthTierSource,
		geminiDriveStorageLimitKey,
		geminiDriveStorageUsageKey,
		geminiDriveTierUpdatedAtKey,
	}
	out := map[string]any{}
	for _, key := range keys {
		if account.Extra != nil {
			if value, ok := account.Extra[key]; ok && value != nil {
				out[key] = value
				continue
			}
		}
		if account.Credentials != nil {
			if value, ok := account.Credentials[key]; ok && value != nil {
				out[key] = value
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func geminiGoogleOneStoredUpdatedAt(account *Account) string {
	if stored := geminiGoogleOneStoredExtra(account); stored != nil {
		if raw, ok := stored[geminiDriveTierUpdatedAtKey].(string); ok {
			return raw
		}
	}
	return ""
}

func geminiMergeStoredExtraIntoTokenInfo(account *Account, tokenInfo *GeminiTokenInfo) {
	if tokenInfo == nil {
		return
	}
	stored := geminiGoogleOneStoredExtra(account)
	if len(stored) == 0 {
		return
	}
	if tokenInfo.Extra == nil {
		tokenInfo.Extra = map[string]any{}
	}
	for key, value := range stored {
		if _, exists := tokenInfo.Extra[key]; !exists {
			tokenInfo.Extra[key] = value
		}
	}
}

func geminiProductionModeEnabled(cfg *config.Config) bool {
	return cfg != nil && cfg.Gemini.ProductionMode
}

func geminiTokenCacheMode(cfg *config.Config) string {
	if cfg == nil {
		return "plaintext"
	}
	mode := strings.ToLower(strings.TrimSpace(cfg.Gemini.TokenCacheMode))
	switch mode {
	case "", "plaintext":
		return "plaintext"
	case "disabled", "encrypted":
		return mode
	default:
		return "plaintext"
	}
}

func geminiAllowsPlaintextTokenCache(cfg *config.Config) bool {
	return !geminiProductionModeEnabled(cfg) && geminiTokenCacheMode(cfg) == "plaintext"
}

func geminiMarkPlaintextTokenCacheDetected(account *Account) {
	if account == nil {
		return
	}
	if account.Extra == nil {
		account.Extra = make(map[string]any)
	}
	for key, value := range geminiPlaintextTokenCacheExtraUpdates() {
		account.Extra[key] = value
	}
}

func geminiPlaintextTokenCacheExtraUpdates() map[string]any {
	return map[string]any{
		geminiTokenCacheStateKey:  geminiTokenCacheStateDegraded,
		geminiTokenCacheReasonKey: geminiTokenCacheReasonPlaintextEntryPresent,
	}
}

func geminiPersistPlaintextTokenCacheDetected(ctx context.Context, repo AccountRepository, account *Account) error {
	geminiMarkPlaintextTokenCacheDetected(account)
	if repo == nil || account == nil || account.ID == 0 {
		return nil
	}
	return repo.UpdateExtra(ctx, account.ID, geminiPlaintextTokenCacheExtraUpdates())
}
