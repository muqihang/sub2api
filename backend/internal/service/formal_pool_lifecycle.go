package service

import (
	"strings"
	"time"
)

const (
	FormalPoolStageImported          = "imported"
	FormalPoolStageRefreshed         = "refreshed"
	FormalPoolStageRuntimeRegistered = "runtime_registered"
	FormalPoolStageHealthcheckPassed = "healthcheck_passed"
	FormalPoolStageWarming           = "warming"
	FormalPoolStageProduction        = "production"
	FormalPoolStageQuarantined       = "quarantined"
	FormalPoolStageLegacyUnknown     = "legacy_unknown"

	FormalPoolWeightLow    = "low"
	FormalPoolWeightNormal = "normal"

	FormalPoolExtraOnboardingStage             = "onboarding_stage"
	FormalPoolExtraOnboardingStageUpdatedAt    = "onboarding_stage_updated_at"
	FormalPoolExtraOnboardingLastCheck         = "onboarding_last_check"
	FormalPoolExtraOnboardingLastCheckAt       = "onboarding_last_check_at"
	FormalPoolExtraOnboardingLastErrorCode     = "onboarding_last_error_code"
	FormalPoolExtraOnboardingLastErrorBucket   = "onboarding_last_error_bucket"
	FormalPoolExtraHealthcheckStatus           = "healthcheck_status"
	FormalPoolExtraHealthcheckStatusCodeBucket = "healthcheck_last_status_code_bucket"
	FormalPoolExtraHealthcheckRawRef           = "healthcheck_last_raw_ref"
	FormalPoolExtraLastFailureOrigin           = "formal_pool_last_failure_origin"
	FormalPoolExtraLastFailureCode             = "formal_pool_last_failure_code"
	FormalPoolExtraLastFailureSource           = "formal_pool_last_failure_source"
	FormalPoolExtraLastCCGatewayErrorCode      = "formal_pool_last_cc_gateway_error_code"
	FormalPoolExtraLastHealthcheckAt           = "formal_pool_last_healthcheck_at"
	FormalPoolExtraLastHealthcheckResult       = "formal_pool_last_healthcheck_result"
	FormalPoolExtraHealthcheckCCGatewaySeen    = "healthcheck_cc_gateway_seen"
	FormalPoolExtraHealthcheckFallbackDetected = "healthcheck_fallback_detected"
	FormalPoolExtraHealthcheckProxyMismatch    = "healthcheck_proxy_mismatch"
	FormalPoolExtraHealthcheckRiskTextDetected = "healthcheck_risk_text_detected"
	FormalPoolExtraHealthcheckSafeErrorCode    = "healthcheck_safe_error_code"
	FormalPoolExtraHealthcheckSafeErrorBucket  = "healthcheck_safe_error_bucket"
	FormalPoolExtraRateLimitErrorClass         = "formal_pool_rate_limit_error_class"
	FormalPoolExtraRateLimitWindow             = "formal_pool_rate_limit_window"
	FormalPoolExtraRateLimitAction             = "formal_pool_rate_limit_action"
	FormalPoolExtraRateLimitResetBucket        = "formal_pool_rate_limit_reset_bucket"
	FormalPoolExtraRateLimitLastAt             = "formal_pool_rate_limit_last_at"
	FormalPoolExtraCredentialGeneration        = "formal_pool_credential_generation"
	FormalPoolExtraRepairedAt                  = "formal_pool_repaired_at"
	FormalPoolExtraRepairedBy                  = "formal_pool_repaired_by"
	FormalPoolExtraRuntimeRegistered           = "cc_gateway_runtime_registered"
	FormalPoolExtraRuntimeRegisteredAt         = "cc_gateway_runtime_registered_at"
	FormalPoolExtraWarmingStartedAt            = "warming_started_at"
	FormalPoolExtraWarmingUntil                = "warming_until"
	FormalPoolExtraPoolProfileRequested        = "pool_profile_requested"
	FormalPoolExtraPoolProfileEffective        = "pool_profile_effective"
	FormalPoolExtraPoolWeightMode              = "pool_weight_mode"
	FormalPoolExtraRiskEventRef                = "risk_event_ref"
	FormalPoolExtraQuarantineReason            = "quarantine_reason"
	FormalPoolExtraQuarantineAt                = "quarantine_at"
)

func FormalPoolImportedAccountExtra(base map[string]any, now time.Time) map[string]any {
	out := cloneCredentials(base)
	if out == nil {
		out = map[string]any{}
	}
	requested := normalizePoolProfile(stringFromAny(out[FormalPoolExtraPoolProfileRequested]))
	if strings.TrimSpace(requested) == "" || requested == PoolProfileNormal {
		if legacy := normalizePoolProfile(stringFromAny(out["pool_profile"])); strings.TrimSpace(legacy) != "" {
			requested = legacy
		}
	}
	if strings.TrimSpace(requested) == "" {
		requested = PoolProfileNormal
	}
	stamp := formalPoolTimestamp(now)
	out[FormalPoolExtraOnboardingStage] = FormalPoolStageImported
	out[FormalPoolExtraOnboardingStageUpdatedAt] = stamp
	out[FormalPoolExtraOnboardingLastCheck] = FormalPoolStageImported
	out[FormalPoolExtraOnboardingLastCheckAt] = stamp
	out[FormalPoolExtraHealthcheckStatus] = "pending"
	out[FormalPoolExtraRuntimeRegistered] = "false"
	out[FormalPoolExtraPoolProfileRequested] = requested
	out[FormalPoolExtraPoolProfileEffective] = PoolProfileNormal
	out[FormalPoolExtraPoolWeightMode] = FormalPoolWeightLow
	out["pool_profile"] = PoolProfileNormal
	return out
}

func formalPoolTimestamp(now time.Time) string {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return now.UTC().Format(time.RFC3339)
}

func IsFormalPoolAccount(account *Account) bool {
	if account == nil || !account.IsAnthropicOAuthOrSetupToken() {
		return false
	}
	return strings.TrimSpace(account.GetExtraString(FormalPoolExtraOnboardingStage)) != ""
}

func FormalPoolAccountStage(account *Account) string {
	if account == nil || !account.IsAnthropicOAuthOrSetupToken() {
		return ""
	}
	stage := strings.TrimSpace(account.GetExtraString(FormalPoolExtraOnboardingStage))
	if stage == "" {
		return FormalPoolStageLegacyUnknown
	}
	return stage
}

func FormalPoolSchedulableEvidenceComplete(account *Account) bool {
	if !IsFormalPoolAccount(account) {
		return true
	}
	stage := FormalPoolAccountStage(account)
	if stage != FormalPoolStageWarming && stage != FormalPoolStageProduction {
		return true
	}
	return runtimeEvidenceComplete(account) && healthcheckEvidenceComplete(account)
}

func IsFormalPoolSchedulableStage(stage string) bool {
	switch strings.TrimSpace(stage) {
	case "", FormalPoolStageLegacyUnknown:
		return true
	case FormalPoolStageWarming, FormalPoolStageProduction:
		return true
	default:
		return false
	}
}
