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
