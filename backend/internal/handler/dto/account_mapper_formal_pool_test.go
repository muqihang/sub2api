package dto

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountFromService_ExposesFormalPoolHardGateFields(t *testing.T) {
	t.Parallel()

	account := &service.Account{
		ID:          1,
		Name:        "formal",
		Status:      service.StatusActive,
		Schedulable: true,
		Platform:    service.PlatformAnthropic,
		Type:        service.AccountTypeSetupToken,
		Extra: map[string]any{
			service.FormalPoolExtraOnboardingStage:             service.FormalPoolStageWarming,
			service.FormalPoolExtraPoolProfileRequested:        service.PoolProfileAggressive,
			service.FormalPoolExtraPoolProfileEffective:        service.PoolProfileNormal,
			service.FormalPoolExtraPoolWeightMode:              service.FormalPoolWeightLow,
			service.FormalPoolExtraHealthcheckStatus:           "passed",
			service.FormalPoolExtraHealthcheckStatusCodeBucket: "status_2xx",
			service.FormalPoolExtraRuntimeRegistered:           "true",
			service.FormalPoolExtraQuarantineReason:            "reason_proxy",
			service.FormalPoolExtraRiskEventRef:                "risk_ref_safe",
			service.FormalPoolExtraWarmingUntil:                "2026-05-28T12:00:00Z",
		},
	}

	got := AccountFromService(account)
	require.True(t, got.IsFormalPool)
	require.True(t, got.EffectiveSchedulable)
	require.Equal(t, service.FormalPoolStageWarming, got.OnboardingStage)
	require.Equal(t, service.PoolProfileAggressive, got.PoolProfileRequested)
	require.Equal(t, service.PoolProfileNormal, got.PoolProfileEffective)
	require.Equal(t, service.FormalPoolWeightLow, got.PoolWeightMode)
	require.Equal(t, "passed", got.HealthcheckStatus)
	require.Equal(t, "status_2xx", got.HealthcheckLastStatusCodeBucket)
	require.True(t, got.CCGatewayRuntimeRegistered)
	require.Equal(t, "reason_proxy", got.QuarantineReason)
	require.Equal(t, "risk_ref_safe", got.RiskEventRef)
	require.Equal(t, "2026-05-28T12:00:00Z", got.WarmingUntil)
	require.False(t, got.ProductionReady)
}

func TestAccountFromService_ExposesFormalPoolRecoveryFields(t *testing.T) {
	t.Parallel()

	account := &service.Account{
		ID:          42,
		Name:        "formal-repair",
		Platform:    service.PlatformAnthropic,
		Type:        service.AccountTypeSetupToken,
		Status:      service.StatusError,
		Schedulable: false,
		Extra: map[string]any{
			service.FormalPoolExtraOnboardingStage:             service.FormalPoolStageQuarantined,
			service.FormalPoolExtraLastFailureOrigin:           "upstream",
			service.FormalPoolExtraLastFailureCode:             "upstream_401",
			service.FormalPoolExtraLastFailureSource:           "formal_pool_healthcheck",
			service.FormalPoolExtraLastCCGatewayErrorCode:      "missing_account_identity",
			service.FormalPoolExtraHealthcheckCCGatewaySeen:    true,
			service.FormalPoolExtraHealthcheckFallbackDetected: false,
			service.FormalPoolExtraHealthcheckProxyMismatch:    false,
			service.FormalPoolExtraHealthcheckRiskTextDetected: false,
			service.FormalPoolExtraCredentialGeneration:        3,
			service.FormalPoolExtraRepairedAt:                  "2026-05-29T00:00:00Z",
			service.FormalPoolExtraRepairedBy:                  "admin_ref_safe",
		},
	}

	got := AccountFromService(account)

	require.Equal(t, "upstream", got.FormalPoolLastFailureOrigin)
	require.Equal(t, "upstream_401", got.FormalPoolLastFailureCode)
	require.Equal(t, "formal_pool_healthcheck", got.FormalPoolLastFailureSource)
	require.Equal(t, "missing_account_identity", got.FormalPoolLastCCGatewayErrorCode)
	require.True(t, got.HealthcheckCCGatewaySeen)
	require.False(t, got.HealthcheckFallbackDetected)
	require.False(t, got.HealthcheckProxyMismatch)
	require.False(t, got.HealthcheckRiskTextDetected)
	require.Equal(t, 3, got.FormalPoolCredentialGeneration)
	require.Equal(t, "2026-05-29T00:00:00Z", got.FormalPoolRepairedAt)
	require.Equal(t, "admin_ref_safe", got.FormalPoolRepairedBy)
}

func TestAccountFromService_FormalPoolLegacyUnknownWhenMissing(t *testing.T) {
	t.Parallel()

	account := &service.Account{ID: 1, Name: "legacy", Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth, Extra: map[string]any{"cc_gateway_enabled": "true"}}

	got := AccountFromService(account)
	require.False(t, got.IsFormalPool)
	require.Equal(t, service.FormalPoolStageLegacyUnknown, got.OnboardingStage)
}

func TestAccountFromService_FormalPoolEffectiveSchedulableUsesLifecycleGate(t *testing.T) {
	t.Parallel()

	account := &service.Account{ID: 1, Name: "imported", Platform: service.PlatformAnthropic, Type: service.AccountTypeSetupToken, Status: service.StatusActive, Schedulable: true, Extra: map[string]any{service.FormalPoolExtraOnboardingStage: service.FormalPoolStageImported}}

	got := AccountFromService(account)
	require.True(t, got.Schedulable)
	require.False(t, got.EffectiveSchedulable)
	require.True(t, got.IsFormalPool)
}

func TestAccountFromService_FormalPoolFieldsDoNotExposeSecrets(t *testing.T) {
	t.Parallel()

	account := &service.Account{ID: 1, Name: "formal", Platform: service.PlatformAnthropic, Type: service.AccountTypeSetupToken, Credentials: map[string]any{
		"access_token":  "sk-ant-sid02-raw-secret",
		"refresh_token": "refresh-token-raw",
		"authorization": "Bearer raw-token",
		"email":         "user@example.com",
	}, Extra: map[string]any{
		service.FormalPoolExtraOnboardingStage:      service.FormalPoolStageProduction,
		service.FormalPoolExtraPoolProfileEffective: service.PoolProfileNormal,
		service.FormalPoolExtraRuntimeRegistered:    true,
		"email_address":                             "user@example.com",
		"account_uuid":                              "99999999-8888-4777-8666-555555555555",
		"organization_uuid":                         "88888888-8888-4777-8666-555555555555",
		"raw_cch":                                   "cch=12345",
		"proxy_password":                            "proxy-secret",
	}}

	payload, err := json.Marshal(AccountFromService(account))
	require.NoError(t, err)
	body := strings.ToLower(string(payload))
	require.NotContains(t, body, "sk-ant")
	require.NotContains(t, body, "authorization")
	require.NotContains(t, body, "x-api-key")
	require.NotContains(t, body, "raw_cch")
	require.NotContains(t, body, "proxy_password")
	require.NotContains(t, body, "refresh_token")
	require.NotContains(t, body, "access_token")
	require.NotContains(t, body, "refresh-token-raw")
	require.NotContains(t, body, "user@example.com")
	require.NotContains(t, body, "99999999-8888-4777-8666-555555555555")
}

func TestAccountFromService_FormalPoolRecoveryFieldsDoNotExposeSecrets(t *testing.T) {
	t.Parallel()

	account := &service.Account{ID: 1, Name: "formal", Platform: service.PlatformAnthropic, Type: service.AccountTypeSetupToken, Extra: map[string]any{
		service.FormalPoolExtraOnboardingStage:             service.FormalPoolStageQuarantined,
		service.FormalPoolExtraLastFailureOrigin:           "upstream user@example.com",
		service.FormalPoolExtraLastFailureCode:             "upstream_401 99999999-8888-4777-8666-555555555555",
		service.FormalPoolExtraLastFailureSource:           "formal_pool_healthcheck sk-ant-sid01-secret",
		service.FormalPoolExtraLastCCGatewayErrorCode:      "missing_account_identity raw_cch",
		service.FormalPoolExtraLastHealthcheckAt:           "2026-05-29T00:00:00Z access_token=secret",
		service.FormalPoolExtraLastHealthcheckResult:       "raw_body prompt-secret",
		service.FormalPoolExtraHealthcheckCCGatewaySeen:    "person@example.com",
		service.FormalPoolExtraHealthcheckFallbackDetected: "raw_prompt marker",
		service.FormalPoolExtraHealthcheckProxyMismatch:    "http://user:proxy-secret@proxy.example.com:8080",
		service.FormalPoolExtraHealthcheckRiskTextDetected: "refresh_token=secret",
		service.FormalPoolExtraCredentialGeneration:        "credential-secret",
		service.FormalPoolExtraRepairedAt:                  "2026-05-29T00:00:00Z bearer raw-token",
		service.FormalPoolExtraRepairedBy:                  "admin@example.com",
		service.FormalPoolExtraHealthcheckStatus:           "failed",
		service.FormalPoolExtraHealthcheckStatusCodeBucket: "status_4xx",
		service.FormalPoolExtraHealthcheckRawRef:           "hmac-sha256:" + strings.Repeat("f", 64),
	}}

	payload, err := json.Marshal(AccountFromService(account))
	require.NoError(t, err)
	body := strings.ToLower(string(payload))
	for _, unsafe := range []string{
		"user@example.com",
		"99999999-8888-4777-8666-555555555555",
		"sk-ant-sid01-secret",
		"raw_cch",
		"access_token",
		"raw_body",
		"prompt-secret",
		"person@example.com",
		"raw_prompt",
		"proxy-secret",
		"proxy.example.com",
		"refresh_token",
		"credential-secret",
		"raw-token",
		"admin@example.com",
	} {
		require.NotContains(t, body, strings.ToLower(unsafe))
	}
	require.Contains(t, body, "failed")
	require.Contains(t, body, "status_4xx")
	require.Contains(t, body, "hmac-sha256:"+strings.Repeat("f", 64))
}

func TestAccountFromService_FormalPoolDTOOnlyExposesSafeGatewayRefs(t *testing.T) {
	t.Parallel()

	account := &service.Account{ID: 1, Name: "formal", Platform: service.PlatformAnthropic, Type: service.AccountTypeSetupToken, Extra: map[string]any{
		service.FormalPoolExtraOnboardingStage:   service.FormalPoolStageProduction,
		service.FormalPoolExtraHealthcheckRawRef: "hmac-sha256:" + strings.Repeat("a", 64),
		"cc_gateway_account_ref":                 "99999999-8888-4777-8666-555555555555",
		"cc_gateway_egress_bucket":               "http://user:proxy-secret@proxy.example.com:8080",
	}}

	got := AccountFromService(account)
	payload, err := json.Marshal(got)
	require.NoError(t, err)
	body := string(payload)
	require.Contains(t, body, "hmac-sha256:"+strings.Repeat("a", 64))
	require.NotContains(t, body, "99999999-8888-4777-8666-555555555555")
	require.NotContains(t, body, "proxy-secret")
	require.NotContains(t, body, "proxy.example.com")
}
