package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormalPoolOperationsDiagnostics_ClassifiesLocalGate(t *testing.T) {
	t.Parallel()

	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: formalPoolOperationsAccountFake{account: &Account{
		ID:          11,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}}})

	got, err := svc.Diagnostics(context.Background(), 11)
	require.NoError(t, err)
	require.False(t, got.IsFormalPool)
	require.Equal(t, string(FormalPoolFailureOriginLocalGate), got.FailureOrigin)
	require.Contains(t, got.Checks, FormalPoolAcceptanceCheck{Name: "not_formal_pool", Status: "fail", Message: "account is not a formal pool Anthropic OAuth/setup-token account"})
}

func TestFormalPoolOperationsDiagnostics_ClassifiesCCGatewayControlPlane(t *testing.T) {
	t.Parallel()

	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: formalPoolOperationsAccountFake{account: formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraLastFailureCode:   "missing_account_identity",
		FormalPoolExtraLastFailureSource: "cc_gateway_runtime_register",
	})}})

	got, err := svc.Diagnostics(context.Background(), 42)
	require.NoError(t, err)
	require.Equal(t, string(FormalPoolFailureOriginCCGateway), got.FailureOrigin)
	require.Equal(t, "missing_account_identity", got.FailureCode)
	require.Equal(t, "cc_gateway_runtime_register", got.FailureSource)
	require.Contains(t, actionKeys(got.RecommendedActions), "runtime_register")
}

func TestFormalPoolOperationsDiagnostics_ClassifiesUpstreamOnlyWithGatewayEvidence(t *testing.T) {
	t.Parallel()

	safeRef := "hmac-sha256:" + strings.Repeat("a", 64)
	withEvidence := formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraHealthcheckStatusCodeBucket: "status_4xx",
		FormalPoolExtraHealthcheckRawRef:           safeRef,
		FormalPoolExtraHealthcheckCCGatewaySeen:    true,
		FormalPoolExtraHealthcheckFallbackDetected: false,
		FormalPoolExtraHealthcheckProxyMismatch:    false,
		FormalPoolExtraHealthcheckRiskTextDetected: true,
		FormalPoolExtraLastFailureCode:             "upstream_401",
		FormalPoolExtraLastFailureSource:           "formal_pool_healthcheck",
	})
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: formalPoolOperationsAccountFake{account: withEvidence}})
	got, err := svc.Diagnostics(context.Background(), 42)
	require.NoError(t, err)
	require.Equal(t, string(FormalPoolFailureOriginUpstream), got.FailureOrigin)
	require.True(t, got.RawCapturePresent)
	require.Equal(t, safeRef, got.RawCaptureRef)
	require.Contains(t, actionKeys(got.RecommendedActions), "repair_token")

	withoutGateway := formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraHealthcheckStatusCodeBucket: "status_4xx",
		FormalPoolExtraHealthcheckRawRef:           safeRef,
		FormalPoolExtraHealthcheckCCGatewaySeen:    false,
		FormalPoolExtraHealthcheckRiskTextDetected: true,
		FormalPoolExtraLastFailureCode:             "upstream_401",
		FormalPoolExtraLastFailureSource:           "formal_pool_healthcheck",
	})
	svc = NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: formalPoolOperationsAccountFake{account: withoutGateway}})
	got, err = svc.Diagnostics(context.Background(), 42)
	require.NoError(t, err)
	require.NotEqual(t, string(FormalPoolFailureOriginUpstream), got.FailureOrigin)
	require.Equal(t, string(FormalPoolFailureOriginLocalGate), got.FailureOrigin)
}

func TestFormalPoolOperationsDiagnostics_ClassifiesProxyMismatch(t *testing.T) {
	t.Parallel()

	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: formalPoolOperationsAccountFake{account: formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraHealthcheckProxyMismatch: true,
		FormalPoolExtraLastFailureCode:          "egress_proxy_failure",
	})}})

	got, err := svc.Diagnostics(context.Background(), 42)
	require.NoError(t, err)
	require.Equal(t, string(FormalPoolFailureOriginProxy), got.FailureOrigin)
	require.True(t, got.ProxyMismatch)
	require.Contains(t, actionKeys(got.RecommendedActions), "swap_proxy")
}

func TestFormalPoolOperationsDiagnostics_DoesNotExposeSecrets(t *testing.T) {
	t.Parallel()

	account := formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraHealthcheckRawRef:           "hmac-sha256:" + strings.Repeat("b", 64),
		FormalPoolExtraHealthcheckCCGatewaySeen:    true,
		FormalPoolExtraHealthcheckStatusCodeBucket: "status_5xx",
		FormalPoolExtraLastFailureCode:             "upstream user@example.com sk-ant-sid01-secret",
		FormalPoolExtraQuarantineReason:            "account 99999999-8888-4777-8666-555555555555 held",
		"raw_body":                                 "sk-ant-sid01-raw-body-secret",
		"raw_prompt":                               "prompt-secret",
		"raw_cch":                                  "cch-secret",
		"email":                                    "person@example.com",
		"account_uuid":                             "99999999-8888-4777-8666-555555555555",
		"proxy_password":                           "proxy-secret",
		"access_token":                             "access-token-secret",
		"refresh_token":                            "refresh-token-secret",
		"sk-ant-sid":                               "sk-ant-sid01-secret",
	})
	account.Credentials = map[string]any{
		"access_token":  "access-token-secret",
		"refresh_token": "refresh-token-secret",
		"session_key":   "sk-ant-sid01-secret",
	}
	account.Proxy = &Proxy{Password: "proxy-secret"}

	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: formalPoolOperationsAccountFake{account: account}})
	got, err := svc.Diagnostics(context.Background(), 42)
	require.NoError(t, err)
	payload, err := json.Marshal(got)
	require.NoError(t, err)
	body := strings.ToLower(string(payload))
	for _, secret := range []string{
		"sk-ant-sid", "access-token-secret", "refresh-token-secret", "proxy-secret",
		"prompt-secret", "cch-secret", "person@example.com", "99999999-8888-4777-8666-555555555555",
	} {
		require.NotContains(t, body, strings.ToLower(secret))
	}
	require.Contains(t, body, "hmac-sha256:"+strings.Repeat("b", 64))
}

func TestClassifyFormalPoolFailureOrigin_Priority(t *testing.T) {
	t.Parallel()

	require.Equal(t, FormalPoolFailureOriginTokenExchange, classifyFormalPoolFailureOrigin(formalPoolFailureEvidence{
		FailureCode:       "setup_token_invalid",
		ProxyMismatch:     true,
		CCGatewaySeen:     true,
		SafeRawCaptureRef: "hmac-sha256:" + strings.Repeat("c", 64),
		StatusCodeBucket:  "status_4xx",
	}))
	require.Equal(t, FormalPoolFailureOriginProxy, classifyFormalPoolFailureOrigin(formalPoolFailureEvidence{
		FailureCode:   "missing_account_identity",
		ProxyMismatch: true,
	}))
}

func TestClassifyFormalPoolFailureOrigin_DoesNotTreatAnyCCGatewaySourceAsControlPlane(t *testing.T) {
	t.Parallel()

	base := formalPoolFailureEvidence{
		IsFormalPool:       true,
		OnboardingStage:    FormalPoolStageQuarantined,
		CCGatewayEnabled:   true,
		CCGatewayRoute:     string(ccGatewayRouteNativeMessages),
		InferenceScope:     true,
		RuntimeRegistered:  true,
		CCGatewaySeen:      true,
		SafeRawCaptureRef:  "hmac-sha256:" + strings.Repeat("c", 64),
		StatusCodeBucket:   "status_4xx",
		FailureCode:        "upstream_401",
		CCGatewayErrorCode: "",
	}

	healthcheck := base
	healthcheck.FailureSource = "formal_pool_healthcheck_cc_gateway_data_plane"
	require.Equal(t, FormalPoolFailureOriginUpstream, classifyFormalPoolFailureOrigin(healthcheck))

	dataPlane := base
	dataPlane.FailureSource = "cc_gateway_data_plane"
	require.Equal(t, FormalPoolFailureOriginUpstream, classifyFormalPoolFailureOrigin(dataPlane))

	controlPlane := base
	controlPlane.FailureSource = "cc_gateway_runtime_register"
	require.Equal(t, FormalPoolFailureOriginCCGateway, classifyFormalPoolFailureOrigin(controlPlane))
}

type formalPoolOperationsAccountFake struct {
	account *Account
}

func (f formalPoolOperationsAccountFake) GetFormalPoolAccount(context.Context, int64) (*Account, error) {
	return f.account, nil
}

func (f formalPoolOperationsAccountFake) UpdateFormalPoolAccountCredentials(context.Context, int64, map[string]any) (*Account, error) {
	return f.account, nil
}

func (f formalPoolOperationsAccountFake) UpdateFormalPoolAccountState(context.Context, int64, bool, string, map[string]any) (*Account, error) {
	return f.account, nil
}

func (f formalPoolOperationsAccountFake) ActivateFormalPoolAccount(context.Context, int64, map[string]any) (*Account, error) {
	return f.account, nil
}

func (f formalPoolOperationsAccountFake) UpdateFormalPoolAccountProxy(context.Context, int64, int64, map[string]any) (*Account, error) {
	return f.account, nil
}

func formalPoolDiagnosticsAccount(extra map[string]any) *Account {
	merged := map[string]any{
		FormalPoolExtraOnboardingStage:   FormalPoolStageQuarantined,
		FormalPoolExtraHealthcheckStatus: FormalPoolOnboardingStatusQuarantined,
		FormalPoolExtraQuarantineReason:  "reason_auth",
		FormalPoolExtraRiskEventRef:      "hmac-sha256:" + strings.Repeat("d", 64),
		"cc_gateway_account_ref":         "hmac-sha256:" + strings.Repeat("e", 64),
		"cc_gateway_enabled":             "true",
		"cc_gateway_routes":              string(ccGatewayRouteNativeMessages),
		"cc_gateway_egress_bucket":       "claude-1234567890abcdef",
		FormalPoolExtraRuntimeRegistered: true,
	}
	for k, v := range extra {
		merged[k] = v
	}
	proxyID := int64(7)
	return &Account{
		ID:          42,
		Name:        "formal",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeSetupToken,
		Status:      StatusError,
		Schedulable: false,
		ProxyID:     &proxyID,
		Extra:       merged,
		Credentials: map[string]any{"scope": "user:inference"},
	}
}

func actionKeys(actions []FormalPoolRecommendedAction) []string {
	keys := make([]string, 0, len(actions))
	for _, action := range actions {
		keys = append(keys, action.Key)
	}
	return keys
}
