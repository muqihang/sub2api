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
			service.FormalPoolExtraHealthcheckRawRef:           "hmac-sha256:" + strings.Repeat("d", 64),
			service.FormalPoolExtraHealthcheckCCGatewaySeen:    true,
			service.FormalPoolExtraHealthcheckFallbackDetected: false,
			service.FormalPoolExtraHealthcheckProxyMismatch:    false,
			service.FormalPoolExtraHealthcheckRiskTextDetected: false,
			service.FormalPoolExtraRuntimeRegistered:           "true",
			service.FormalPoolExtraRuntimeRegisteredAt:         "2026-05-28T11:00:00Z",
			service.FormalPoolExtraQuarantineReason:            "reason_proxy",
			service.FormalPoolExtraRiskEventRef:                "hmac-sha256:" + strings.Repeat("e", 64),
			service.FormalPoolExtraWarmingUntil:                "2026-05-28T12:00:00Z",
			"cc_gateway_account_ref":                           "hmac-sha256:" + strings.Repeat("a", 64),
			"cc_gateway_credential_ref":                        "hmac-sha256:" + strings.Repeat("b", 64),
			"cc_gateway_credential_binding_hmac":               "hmac-sha256:" + strings.Repeat("c", 64),
			"cc_gateway_proxy_identity_ref":                    "hmac-sha256:" + strings.Repeat("f", 64),
			"cc_gateway_persona_profile":                       "claude-code-2.1.179-macos-local",
			"claude_code_device_id":                            strings.Repeat("1", 64),
			"cc_gateway_egress_bucket_enabled":                 "true",
			"cc_gateway_egress_bucket":                         "bucket-a",
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
	require.Equal(t, "hmac-sha256:"+strings.Repeat("e", 64), got.RiskEventRef)
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

func TestAccountFromService_ExposesFormalPoolRateLimitAndHealthcheckSafeFields(t *testing.T) {
	t.Parallel()

	account := &service.Account{
		ID:       43,
		Name:     "formal-signals",
		Platform: service.PlatformAnthropic,
		Type:     service.AccountTypeSetupToken,
		Extra: map[string]any{
			service.FormalPoolExtraOnboardingStage: "production",
			"formal_pool_rate_limit_error_class":   "rate_limited",
			"formal_pool_rate_limit_window":        "both",
			"formal_pool_rate_limit_action":        "rate_limited",
			"formal_pool_rate_limit_reset_bucket":  "rfc3339",
			"formal_pool_rate_limit_last_at":       "2026-05-31T12:00:00Z",
			"healthcheck_safe_error_code":          "rate_limited",
			"healthcheck_safe_error_bucket":        "rate_limited",
		},
	}

	got := AccountFromService(account)
	payload, err := json.Marshal(got)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(payload, &body))
	require.Equal(t, "rate_limited", body["formal_pool_rate_limit_error_class"])
	require.Equal(t, "both", body["formal_pool_rate_limit_window"])
	require.Equal(t, "rate_limited", body["formal_pool_rate_limit_action"])
	require.Equal(t, "rfc3339", body["formal_pool_rate_limit_reset_bucket"])
	require.Equal(t, "2026-05-31T12:00:00Z", body["formal_pool_rate_limit_last_at"])
	require.Equal(t, "rate_limited", body["healthcheck_safe_error_code"])
	require.Equal(t, "rate_limited", body["healthcheck_safe_error_bucket"])
	require.Equal(t, "rate_limited", got.Extra["formal_pool_rate_limit_error_class"])
	require.Equal(t, "rate_limited", got.Extra["healthcheck_safe_error_code"])
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

	accessToken := "sk-ant-" + "sid02-" + strings.Repeat("s", 8)
	refreshToken := "refresh-token-" + strings.Repeat("r", 8)
	bearerToken := "Bearer " + strings.Repeat("t", 16)
	email := "user" + "@example.com"
	accountUUID := "99999999-" + "8888-4777-8666-555555555555"
	orgUUID := "88888888-" + "8888-4777-8666-555555555555"
	proxyPassword := "proxy-" + strings.Repeat("p", 8)

	account := &service.Account{ID: 1, Name: "formal", Platform: service.PlatformAnthropic, Type: service.AccountTypeSetupToken, Credentials: map[string]any{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"authorization": bearerToken,
		"email":         email,
	}, Extra: map[string]any{
		service.FormalPoolExtraOnboardingStage:      service.FormalPoolStageProduction,
		service.FormalPoolExtraPoolProfileEffective: service.PoolProfileNormal,
		service.FormalPoolExtraRuntimeRegistered:    true,
		"email_address":                             email,
		"account_uuid":                              accountUUID,
		"organization_uuid":                         orgUUID,
		"raw_cch":                                   "cch=12345",
		"proxy_password":                            proxyPassword,
	}}

	payload, err := json.Marshal(AccountFromService(account))
	require.NoError(t, err)
	body := strings.ToLower(string(payload))
	require.NotContains(t, body, "sk-ant")
	require.NotContains(t, body, "authorization")
	require.NotContains(t, body, "x-api-key")
	require.NotContains(t, body, "raw_cch")
	require.NotContains(t, body, "proxy_password")
	require.NotContains(t, body, strings.ToLower(refreshToken))
	require.Empty(t, AccountFromService(account).Credentials)
	require.True(t, AccountFromService(account).CredentialsStatus["has_access_token"])
	require.True(t, AccountFromService(account).CredentialsStatus["has_refresh_token"])
	require.NotContains(t, body, strings.ToLower(email))
	require.NotContains(t, body, strings.ToLower(accountUUID))
}

func TestAccountFromService_FormalPoolRecoveryFieldsDoNotExposeSecrets(t *testing.T) {
	t.Parallel()

	email := "user" + "@example.com"
	personEmail := "person" + "@example.com"
	adminEmail := "admin" + "@example.com"
	uuid := "99999999-" + "8888-4777-8666-555555555555"
	token := "sk-ant-" + "sid01-" + "synthetic"
	secretValue := "synthetic-" + strings.Repeat("s", 8)
	proxyHost := "proxy" + ".example.com"
	rawToken := "raw-" + strings.Repeat("t", 8)

	account := &service.Account{ID: 1, Name: "formal", Platform: service.PlatformAnthropic, Type: service.AccountTypeSetupToken, Extra: map[string]any{
		service.FormalPoolExtraOnboardingStage:             service.FormalPoolStageQuarantined,
		service.FormalPoolExtraLastFailureOrigin:           "upstream " + email,
		service.FormalPoolExtraLastFailureCode:             "upstream_401 " + uuid,
		service.FormalPoolExtraLastFailureSource:           "formal_pool_healthcheck " + token,
		service.FormalPoolExtraLastCCGatewayErrorCode:      "missing_account_identity raw_cch",
		service.FormalPoolExtraLastHealthcheckAt:           "2026-05-29T00:00:00Z access_token=" + secretValue,
		service.FormalPoolExtraLastHealthcheckResult:       "raw_body " + secretValue,
		service.FormalPoolExtraHealthcheckCCGatewaySeen:    personEmail,
		service.FormalPoolExtraHealthcheckFallbackDetected: "raw_prompt marker",
		service.FormalPoolExtraHealthcheckProxyMismatch:    "http://user:" + secretValue + "@" + proxyHost + ":8080",
		service.FormalPoolExtraHealthcheckRiskTextDetected: "refresh_token=" + secretValue,
		service.FormalPoolExtraCredentialGeneration:        "credential-" + secretValue,
		service.FormalPoolExtraRepairedAt:                  "2026-05-29T00:00:00Z bearer " + rawToken,
		service.FormalPoolExtraRepairedBy:                  adminEmail,
		service.FormalPoolExtraHealthcheckStatus:           "failed",
		service.FormalPoolExtraHealthcheckStatusCodeBucket: "status_4xx",
		service.FormalPoolExtraHealthcheckRawRef:           "hmac-sha256:" + strings.Repeat("f", 64),
	}}

	payload, err := json.Marshal(AccountFromService(account))
	require.NoError(t, err)
	body := strings.ToLower(string(payload))
	for _, unsafe := range []string{
		email,
		uuid,
		token,
		"raw_cch",
		"access_token",
		"raw_body",
		secretValue,
		personEmail,
		"raw_prompt",
		proxyHost,
		"refresh_token",
		"credential-" + secretValue,
		rawToken,
		adminEmail,
	} {
		require.NotContains(t, body, strings.ToLower(unsafe))
	}
	require.Contains(t, body, "failed")
	require.Contains(t, body, "status_4xx")
	require.Contains(t, body, "hmac-sha256:"+strings.Repeat("f", 64))
}

func TestAccountFromService_FormalPoolHardGateFieldsDoNotExposeUnsafeDirectValuesOrProxy(t *testing.T) {
	t.Parallel()

	accessToken := "sk-ant-" + "sid02-" + strings.Repeat("s", 8)
	email := "user" + "@example.com"
	adminEmail := "admin" + "@example.com"
	accountUUID := "99999999-" + "8888-4777-8666-555555555555"
	orgUUID := "88888888-" + "8888-4777-8666-555555555555"
	proxyHost := "proxy" + ".example.com"
	proxyUser := "proxy-user"
	proxyPassword := "proxy-" + strings.Repeat("p", 8)
	rawToken := "raw-" + strings.Repeat("t", 8)

	proxyID := int64(7)
	account := &service.Account{
		ID:       1,
		Name:     "formal",
		Platform: service.PlatformAnthropic,
		Type:     service.AccountTypeSetupToken,
		Credentials: map[string]any{
			"access_token":      accessToken,
			"email":             email,
			"account_uuid":      accountUUID,
			"organization_uuid": orgUUID,
		},
		ProxyID: &proxyID,
		Proxy: &service.Proxy{
			ID:       7,
			Name:     "formal-proxy",
			Protocol: "http",
			Host:     proxyHost,
			Port:     8080,
			Username: proxyUser,
			Password: proxyPassword,
		},
		Extra: map[string]any{
			service.FormalPoolExtraOnboardingStage:  service.FormalPoolStageQuarantined,
			service.FormalPoolExtraQuarantineReason: "reason_auth " + adminEmail + " raw_prompt",
			service.FormalPoolExtraRiskEventRef:     accountUUID + ":" + email,
			service.FormalPoolExtraWarmingUntil:     "2026-05-28T12:00:00Z access_token=" + rawToken,
		},
	}

	got := AccountFromService(account)
	require.Nil(t, got.Proxy)
	require.Empty(t, got.QuarantineReason)
	require.Empty(t, got.RiskEventRef)
	require.Empty(t, got.WarmingUntil)

	payload, err := json.Marshal(got)
	require.NoError(t, err)
	body := strings.ToLower(string(payload))
	for _, unsafe := range []string{
		accessToken,
		email,
		adminEmail,
		accountUUID,
		orgUUID,
		proxyHost,
		proxyUser,
		proxyPassword,
		"raw_prompt",
		rawToken,
	} {
		require.NotContains(t, body, strings.ToLower(unsafe))
	}
	require.Empty(t, got.Credentials)
	require.True(t, got.CredentialsStatus["has_access_token"])
}

func TestAccountFromService_FormalPoolDTOOnlyExposesSafeGatewayRefs(t *testing.T) {
	t.Parallel()

	unsafeUUID := "99999999-" + "8888-4777-8666-555555555555"
	proxySecret := "proxy-" + strings.Repeat("p", 8)
	proxyHost := "proxy" + ".example.com"
	account := &service.Account{ID: 1, Name: "formal", Platform: service.PlatformAnthropic, Type: service.AccountTypeSetupToken, Extra: map[string]any{
		service.FormalPoolExtraOnboardingStage:   service.FormalPoolStageProduction,
		service.FormalPoolExtraHealthcheckRawRef: "hmac-sha256:" + strings.Repeat("a", 64),
		"cc_gateway_account_ref":                 unsafeUUID,
		"cc_gateway_egress_bucket":               "http://user:" + proxySecret + "@" + proxyHost + ":8080",
	}}

	got := AccountFromService(account)
	payload, err := json.Marshal(got)
	require.NoError(t, err)
	body := string(payload)
	require.Contains(t, body, "hmac-sha256:"+strings.Repeat("a", 64))
	require.NotContains(t, body, unsafeUUID)
	require.NotContains(t, body, proxySecret)
	require.NotContains(t, body, proxyHost)
}
