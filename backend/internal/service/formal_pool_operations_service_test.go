package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

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
		FormalPoolExtraQuarantineReason:  "reason_auth",
		FormalPoolExtraQuarantineAt:      "2026-05-29T00:00:00Z",
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

func TestFormalPoolOperationsDiagnostics_StronglySanitizesPersistedFailureFields(t *testing.T) {
	t.Parallel()

	safeRef := "hmac-sha256:" + strings.Repeat("4", 64)
	account := formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraHealthcheckRawRef:           safeRef,
		FormalPoolExtraHealthcheckCCGatewaySeen:    true,
		FormalPoolExtraHealthcheckStatusCodeBucket: "status_4xx",
		FormalPoolExtraLastFailureOrigin:           "upstream raw_body access_token=generic-secret",
		FormalPoolExtraLastFailureCode:             "upstream_401 missing_account_identity raw_prompt",
		FormalPoolExtraLastFailureSource:           "formal_pool_healthcheck raw_telemetry",
		FormalPoolExtraLastCCGatewayErrorCode:      "missing_account_identity raw_cch",
		FormalPoolExtraQuarantineReason:            "reason_auth 2026-05-29T00:00:00Z admin_ref_safe http://proxy-user:proxy-pass@proxy.example.com:8080",
	})

	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: formalPoolOperationsAccountFake{account: account}})
	got, err := svc.Diagnostics(context.Background(), 42)
	require.NoError(t, err)
	payload := strings.ToLower(mustJSON(t, got))
	for _, unsafe := range []string{
		"raw_body",
		"raw_prompt",
		"raw_telemetry",
		"raw_cch",
		"http://proxy-user:proxy-pass@proxy.example.com:8080",
		"proxy-pass",
		"access_token=generic-secret",
		"secret",
		"99999999-8888-4777-8666-555555555555",
	} {
		require.NotContains(t, payload, strings.ToLower(unsafe))
	}
	for _, safe := range []string{
		"upstream_401",
		"formal_pool_healthcheck",
		"missing_account_identity",
		"reason_auth",
		"2026-05-29t00:00:00z",
		"admin_ref_safe",
		safeRef,
	} {
		require.Contains(t, payload, strings.ToLower(safe))
	}
}

func TestFormalPoolOperationsDiagnostics_OmitsWholeFieldsWithSensitiveMarkersAndValues(t *testing.T) {
	t.Parallel()

	account := formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraLastFailureCode:        `"access_token": "abc123"`,
		FormalPoolExtraLastFailureSource:      `"proxy_password": "pass"`,
		FormalPoolExtraQuarantineReason:       "'session_key': 'sk-ant-sid01-secret'",
		FormalPoolExtraLastCCGatewayErrorCode: "`refresh_token`: `refresh-secret`",
	})

	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: formalPoolOperationsAccountFake{account: account}})
	got, err := svc.Diagnostics(context.Background(), 42)
	require.NoError(t, err)
	payload := strings.ToLower(mustJSON(t, got))

	require.Empty(t, got.FailureCode)
	require.Empty(t, got.FailureSource)
	require.Empty(t, got.QuarantineReason)
	for _, unsafe := range []string{
		"access_token",
		"abc123",
		"proxy_password",
		"session_key",
		"sk-ant-sid01-secret",
		"refresh_token",
		"refresh-secret",
	} {
		require.NotContains(t, payload, strings.ToLower(unsafe))
	}
}

func TestFormalPoolOperationsDiagnostics_PreservesCookieAuthClassificationWithoutSecrets(t *testing.T) {
	t.Parallel()

	account := formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraLastFailureCode:   "cookie_auth_failed",
		FormalPoolExtraLastFailureSource: "token_exchange",
		FormalPoolExtraQuarantineReason:  "reason_auth raw_cookie session_key sk-ant-sid01-secret access_token abc123",
		"raw_cookie":                     "session=raw-cookie-secret",
		"access_token":                   "raw-access-secret",
	})
	account.Credentials = map[string]any{
		"scope":         "user:inference",
		"access_token":  "raw-access-secret",
		"refresh_token": "raw-refresh-secret",
		"session_key":   "sk-ant-sid01-secret",
	}

	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: formalPoolOperationsAccountFake{account: account}})
	got, err := svc.Diagnostics(context.Background(), 42)
	require.NoError(t, err)
	payload := strings.ToLower(mustJSON(t, got))

	require.Equal(t, "cookie_auth_failed", got.FailureCode)
	require.Equal(t, "token_exchange", got.FailureSource)
	require.Equal(t, string(FormalPoolFailureOriginTokenExchange), got.FailureOrigin)
	require.Contains(t, actionKeys(got.RecommendedActions), "repair_token")
	require.Contains(t, actionKeys(got.RecommendedActions), "replace_account_and_proxy")
	for _, unsafe := range []string{
		"raw_cookie",
		"raw-cookie-secret",
		"session_key",
		"sk-ant-sid01-secret",
		"access_token",
		"abc123",
		"raw-access-secret",
		"raw-refresh-secret",
	} {
		require.NotContains(t, payload, strings.ToLower(unsafe))
	}
}

func TestFormalPoolOperationsReplaceSetupToken_UpdatesCredentialsAndMarksRefreshed(t *testing.T) {
	t.Parallel()

	store := newFormalPoolOperationsMutableStore(formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraCredentialGeneration: "2",
		FormalPoolExtraLastFailureOrigin:    "upstream",
		FormalPoolExtraLastFailureCode:      "upstream_401",
		FormalPoolExtraHealthcheckStatus:    "failed",
	}))
	store.account.Credentials = map[string]any{"refresh_token": "old-refresh", "access_token": "old-access", "scope": "user:inference", "keep": "preserve"}
	oauth := &formalPoolOperationsOAuthFake{
		summary:     FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ExpiresInBucket: "gt_1h"},
		credentials: map[string]any{"access_token": "new-access", "refresh_token": "new-refresh", "scope": "user:inference", "token_type": "Bearer"},
	}
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: store, OAuth: oauth, Now: func() time.Time { return now }})

	got, err := svc.ReplaceSetupToken(context.Background(), store.account.ID, FormalPoolSetupTokenReplaceRequest{SessionKey: "  sk-ant-sid01-new-secret  "})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.Account)
	require.Equal(t, int64(7), oauth.proxyID)
	require.Equal(t, "sk-ant-sid01-new-secret", oauth.sessionKey)
	require.Equal(t, "new-access", store.account.GetCredential("access_token"))
	require.Equal(t, "new-refresh", store.account.GetCredential("refresh_token"))
	require.Equal(t, "preserve", store.account.GetCredential("keep"))
	require.Empty(t, store.account.GetCredential("session_key"))
	require.False(t, store.account.Schedulable)
	require.Equal(t, StatusActive, store.account.Status)
	require.Equal(t, FormalPoolStageRefreshed, store.account.GetExtraString(FormalPoolExtraOnboardingStage))
	require.Equal(t, "pending", store.account.GetExtraString(FormalPoolExtraHealthcheckStatus))
	require.Equal(t, "3", store.account.GetExtraString(FormalPoolExtraCredentialGeneration))
	require.Equal(t, formalPoolTimestamp(now), store.account.GetExtraString(FormalPoolExtraRepairedAt))
	require.Equal(t, "admin", store.account.GetExtraString(FormalPoolExtraRepairedBy))
	require.Empty(t, store.account.GetExtraString(FormalPoolExtraLastFailureOrigin))
	require.Empty(t, store.account.GetExtraString(FormalPoolExtraLastFailureCode))
	require.NotContains(t, mustJSON(t, got), "sk-ant-sid")
}

func TestFormalPoolOperationsReplaceSetupTokenInvalidatesTokenAndSyncsSchedulerCache(t *testing.T) {
	t.Parallel()

	store := newFormalPoolOperationsMutableStore(formalPoolDiagnosticsAccount(nil))
	store.account.Credentials = map[string]any{"refresh_token": "old-refresh", "access_token": "old-access", "scope": "user:inference"}
	oauth := &formalPoolOperationsOAuthFake{
		summary:     FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ExpiresInBucket: "gt_1h"},
		credentials: map[string]any{"access_token": "new-access", "refresh_token": "new-refresh", "scope": "user:inference"},
	}
	invalidator := &formalPoolTokenInvalidatorFake{}
	scheduler := &formalPoolSchedulerCacheFake{}
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: store, OAuth: oauth, CacheInvalidator: invalidator, SchedulerCache: scheduler})

	_, err := svc.ReplaceSetupToken(context.Background(), store.account.ID, FormalPoolSetupTokenReplaceRequest{SessionKey: "sk-ant-sid01-new-secret"})
	require.NoError(t, err)
	require.Len(t, invalidator.accounts, 1)
	require.Equal(t, store.account.ID, invalidator.accounts[0].ID)
	require.Len(t, scheduler.setAccountCalls, 1)
	require.Equal(t, "new-access", scheduler.setAccountCalls[0].GetCredential("access_token"))
	require.Equal(t, FormalPoolStageRefreshed, scheduler.setAccountCalls[0].GetExtraString(FormalPoolExtraOnboardingStage))
}

func TestFormalPoolOperationsReplaceSetupToken_RejectsNonSetupToken(t *testing.T) {
	t.Parallel()

	account := formalPoolDiagnosticsAccount(nil)
	account.Type = AccountTypeOAuth
	store := newFormalPoolOperationsMutableStore(account)
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: store, OAuth: &formalPoolOperationsOAuthFake{}})

	_, err := svc.ReplaceSetupToken(context.Background(), account.ID, FormalPoolSetupTokenReplaceRequest{SessionKey: "sk-ant-sid01-secret"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "SETUP_TOKEN_ACCOUNT_REQUIRED")
}

func TestFormalPoolOperationsReplaceSetupToken_MissingInferenceScopeQuarantinesWithTypedFailure(t *testing.T) {
	t.Parallel()

	secret := "sk-ant-sid01-missing-inference-secret"
	store := newFormalPoolOperationsMutableStore(formalPoolDiagnosticsAccount(nil))
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: store, OAuth: &formalPoolOperationsOAuthFake{
		summary:     FormalPoolOAuthTokenSummary{ScopeContainsUserInference: false},
		credentials: map[string]any{"access_token": "new-access", "refresh_token": "new-refresh", "scope": "user:profile"},
	}})

	result, err := svc.ReplaceSetupToken(context.Background(), store.account.ID, FormalPoolSetupTokenReplaceRequest{SessionKey: secret})
	require.Error(t, err)
	var opErr *FormalPoolOperationFailure
	require.True(t, errors.As(err, &opErr))
	require.Equal(t, "SETUP_TOKEN_REPLACE_FAILED", opErr.Code)
	require.Equal(t, http.StatusBadRequest, opErr.HTTPStatus)
	require.NotNil(t, result)
	require.NotNil(t, result.Diagnostics)
	require.Equal(t, StatusError, store.account.Status)
	require.False(t, store.account.Schedulable)
	require.Equal(t, FormalPoolStageQuarantined, store.account.GetExtraString(FormalPoolExtraOnboardingStage))
	require.Equal(t, "quarantined", store.account.GetExtraString(FormalPoolExtraHealthcheckStatus))
	require.Equal(t, string(FormalPoolFailureOriginTokenExchange), store.account.GetExtraString(FormalPoolExtraLastFailureOrigin))
	require.Equal(t, "setup_token_missing_inference_scope", store.account.GetExtraString(FormalPoolExtraLastFailureCode))
	require.Equal(t, "formal_pool_operations", store.account.GetExtraString(FormalPoolExtraLastFailureSource))
	require.Equal(t, string(FormalPoolFailureOriginTokenExchange), result.Diagnostics.FailureOrigin)
	require.Contains(t, actionKeys(result.Diagnostics.RecommendedActions), "replace_account_and_proxy")
	require.Equal(t, 0, store.credentialsUpdates)
	for _, body := range []string{strings.ToLower(err.Error()), strings.ToLower(mustJSON(t, result)), strings.ToLower(mustJSON(t, opErr.Result))} {
		require.NotContains(t, body, strings.ToLower(secret))
		require.NotContains(t, body, "sk-ant-sid")
	}
}

func TestFormalPoolOperationsReplaceSetupToken_ClaudeCodeScopeMismatchQuarantinesWithTypedFailure(t *testing.T) {
	t.Parallel()

	secret := "sk-ant-sid01-claude-code-scope-secret"
	store := newFormalPoolOperationsMutableStore(formalPoolDiagnosticsAccount(nil))
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: store, OAuth: &formalPoolOperationsOAuthFake{
		summary:     FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ScopeContainsClaudeCode: true},
		credentials: map[string]any{"access_token": "new-access", "refresh_token": "new-refresh", "scope": "user:inference claude_code"},
	}})

	result, err := svc.ReplaceSetupToken(context.Background(), store.account.ID, FormalPoolSetupTokenReplaceRequest{SessionKey: secret})
	require.Error(t, err)
	var opErr *FormalPoolOperationFailure
	require.True(t, errors.As(err, &opErr))
	require.Equal(t, "SETUP_TOKEN_REPLACE_FAILED", opErr.Code)
	require.NotNil(t, result)
	require.NotNil(t, result.Diagnostics)
	require.Equal(t, StatusError, store.account.Status)
	require.False(t, store.account.Schedulable)
	require.Equal(t, FormalPoolStageQuarantined, store.account.GetExtraString(FormalPoolExtraOnboardingStage))
	require.Equal(t, "setup_token_claude_code_scope_mismatch", store.account.GetExtraString(FormalPoolExtraLastFailureCode))
	require.Equal(t, string(FormalPoolFailureOriginTokenExchange), result.Diagnostics.FailureOrigin)
	require.Contains(t, actionKeys(result.Diagnostics.RecommendedActions), "replace_account_and_proxy")
	require.Equal(t, 0, store.credentialsUpdates)
	for _, body := range []string{strings.ToLower(err.Error()), strings.ToLower(mustJSON(t, result)), strings.ToLower(mustJSON(t, opErr.Result))} {
		require.NotContains(t, body, strings.ToLower(secret))
		require.NotContains(t, body, "sk-ant-sid")
	}
}

func TestFormalPoolOperationsReplaceSetupToken_DoesNotEchoSessionKeyOnError(t *testing.T) {
	t.Parallel()

	secret := "sk-ant-sid01-super-secret"
	store := newFormalPoolOperationsMutableStore(formalPoolDiagnosticsAccount(nil))
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: store, OAuth: &formalPoolOperationsOAuthFake{err: fmt.Errorf("upstream failed for %s", secret)}})

	result, err := svc.ReplaceSetupToken(context.Background(), store.account.ID, FormalPoolSetupTokenReplaceRequest{SessionKey: secret})
	require.Error(t, err)
	require.NotContains(t, strings.ToLower(err.Error()), strings.ToLower(secret))
	require.NotContains(t, strings.ToLower(err.Error()), "sk-ant-sid")
	require.NotContains(t, strings.ToLower(mustJSON(t, result)), strings.ToLower(secret))
	require.NotContains(t, strings.ToLower(mustJSON(t, result)), "sk-ant-sid")
	var opErr *FormalPoolOperationFailure
	require.True(t, errors.As(err, &opErr))
	require.NotNil(t, opErr.Result)
	require.NotContains(t, strings.ToLower(mustJSON(t, opErr.Result)), strings.ToLower(secret))
}

func TestFormalPoolOperationsReplaceSetupToken_FailureReturnsSafeDiagnosticsAndReplacementRecommendation(t *testing.T) {
	t.Parallel()

	store := newFormalPoolOperationsMutableStore(formalPoolDiagnosticsAccount(nil))
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: store, OAuth: &formalPoolOperationsOAuthFake{err: fmt.Errorf("raw token exchange 401")}})

	result, err := svc.ReplaceSetupToken(context.Background(), store.account.ID, FormalPoolSetupTokenReplaceRequest{SessionKey: "sk-ant-sid01-secret"})
	require.Error(t, err)
	var opErr *FormalPoolOperationFailure
	require.True(t, errors.As(err, &opErr))
	require.Equal(t, "SETUP_TOKEN_REPLACE_FAILED", opErr.Code)
	require.Equal(t, http.StatusBadRequest, opErr.HTTPStatus)
	require.Equal(t, "setup-token credential exchange failed", opErr.Message)
	require.NotNil(t, result)
	require.NotNil(t, result.Diagnostics)
	require.Equal(t, string(FormalPoolFailureOriginTokenExchange), result.Diagnostics.FailureOrigin)
	require.Contains(t, actionKeys(result.Diagnostics.RecommendedActions), "replace_account_and_proxy")
	require.NotContains(t, mustJSON(t, result), "raw token exchange")
}

func TestFormalPoolOperationsReplaceSetupToken_FailureWritesRiskEventRef(t *testing.T) {
	t.Parallel()

	store := newFormalPoolOperationsMutableStore(formalPoolDiagnosticsAccount(map[string]any{FormalPoolExtraRiskEventRef: ""}))
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: store, OAuth: &formalPoolOperationsOAuthFake{err: fmt.Errorf("cookie auth failed")}, Now: func() time.Time { return time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC) }})

	result, err := svc.ReplaceSetupToken(context.Background(), store.account.ID, FormalPoolSetupTokenReplaceRequest{SessionKey: "sk-ant-sid01-secret"})
	require.Error(t, err)
	require.NotNil(t, result)
	require.Equal(t, StatusError, store.account.Status)
	require.False(t, store.account.Schedulable)
	require.Equal(t, FormalPoolStageQuarantined, store.account.GetExtraString(FormalPoolExtraOnboardingStage))
	require.Equal(t, "quarantined", store.account.GetExtraString(FormalPoolExtraHealthcheckStatus))
	require.Equal(t, string(FormalPoolFailureOriginTokenExchange), store.account.GetExtraString(FormalPoolExtraLastFailureOrigin))
	require.Equal(t, "setup_token_exchange_failed", store.account.GetExtraString(FormalPoolExtraLastFailureCode))
	require.Equal(t, "formal_pool_operations", store.account.GetExtraString(FormalPoolExtraLastFailureSource))
	require.Equal(t, "reason_sensitive", store.account.GetExtraString(FormalPoolExtraQuarantineReason))
	riskRef := store.account.GetExtraString(FormalPoolExtraRiskEventRef)
	require.NotEmpty(t, riskRef)
	require.True(t, isSafeLedgerRef(riskRef), riskRef)
	require.Equal(t, riskRef, result.Diagnostics.RiskEventRef)
}

func TestFormalPoolOperationsReplaceSetupToken_RiskEventRefUsesQuarantineLedgerEntry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	store := newFormalPoolOperationsMutableStore(formalPoolDiagnosticsAccount(map[string]any{FormalPoolExtraRiskEventRef: ""}))
	repo := &formalPoolQuarantineRepo{accounts: map[int64]*Account{store.account.ID: formalPoolDiagnosticsAccount(nil)}}
	quarantine := NewAccountQuarantineService(repo, nil)
	quarantine.now = func() time.Time { return now }
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{
		Accounts:   store,
		OAuth:      &formalPoolOperationsOAuthFake{err: fmt.Errorf("cookie auth failed")},
		Quarantine: quarantine,
		Now:        func() time.Time { return now },
	})

	result, err := svc.ReplaceSetupToken(context.Background(), store.account.ID, FormalPoolSetupTokenReplaceRequest{SessionKey: "sk-ant-sid01-secret"})
	require.Error(t, err)
	require.NotNil(t, result)
	entry, buildErr := BuildRiskEventLedgerEntry(RiskEventLedgerInput{
		Kind:            RiskEventKindIdentityBoundaryFail,
		Severity:        RiskSeverityP0,
		RawAccountID:    fmt.Sprintf("%d", store.account.ID),
		UnsafeRawReason: "setup_token_exchange_failed",
		ObservedAt:      now,
		Recommendation:  BudgetActionQuarantine,
	})
	require.NoError(t, buildErr)
	expected := formalPoolSafeRef("risk_event", entry.AccountRef+":"+entry.Timestamp+":"+entry.Kind+":"+entry.SafeReason)
	rawLedgerRef := entry.AccountRef + ":" + entry.Timestamp
	riskRef := store.account.GetExtraString(FormalPoolExtraRiskEventRef)
	require.Equal(t, expected, riskRef)
	require.NotEqual(t, rawLedgerRef, riskRef)
	require.True(t, isSafeLedgerRef(riskRef), riskRef)
	require.Equal(t, expected, result.Diagnostics.RiskEventRef)
}

func TestFormalPoolOperationsReplaceSetupToken_CanRunRuntimeAndHealthcheck(t *testing.T) {
	t.Parallel()

	store := newFormalPoolOperationsMutableStore(formalPoolDiagnosticsAccount(nil))
	runtime := &formalPoolOperationsRuntimeFake{}
	health := &formalPoolOperationsHealthcheckFake{result: &FormalPoolAcceptanceResult{Status: FormalPoolOnboardingStatusHealthcheckPassed, StatusCodeBucket: "status_2xx", CCGatewaySeen: true, RawCapturePresent: true, RawCaptureRef: "hmac-sha256:" + strings.Repeat("a", 64)}}
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{
		Accounts:         store,
		OAuth:            &formalPoolOperationsOAuthFake{summary: FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true}, credentials: map[string]any{"access_token": "new-access", "refresh_token": "new-refresh", "scope": "user:inference"}},
		Proxy:            &formalPoolOperationsProxyFake{proxy: &Proxy{ID: 7, Protocol: "http", Host: "127.0.0.1", Port: 8080, Status: StatusActive}},
		CCGatewayRuntime: runtime,
		Healthcheck:      health,
	})

	got, err := svc.ReplaceSetupToken(context.Background(), store.account.ID, FormalPoolSetupTokenReplaceRequest{SessionKey: "sk-ant-sid01-secret", RunRuntimeRegister: true, RunHealthcheck: true})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, FormalPoolStageHealthcheckPassed, store.account.GetExtraString(FormalPoolExtraOnboardingStage))
	require.Equal(t, "passed", store.account.GetExtraString(FormalPoolExtraHealthcheckStatus))
	require.True(t, runtime.called)
	require.True(t, health.called)
	require.False(t, store.account.Schedulable)
}

func TestFormalPoolOperationsRuntimeRegister_BackfillsMissingSafeRefWithoutDBID(t *testing.T) {
	t.Parallel()

	store := newFormalPoolOperationsMutableStore(formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraOnboardingStage:   FormalPoolStageRefreshed,
		FormalPoolExtraRuntimeRegistered: "false",
		"cc_gateway_account_ref":         "",
		"cc_gateway_egress_bucket":       "claude-existing-bucket",
	}))
	runtime := &formalPoolOperationsRuntimeFake{}
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{
		Accounts:         store,
		Proxy:            &formalPoolOperationsProxyFake{proxy: &Proxy{ID: 7, Protocol: "http", Host: "127.0.0.1", Port: 8080, Status: StatusActive}},
		CCGatewayRuntime: runtime,
	})

	_, err := svc.RuntimeRegister(context.Background(), store.account.ID)
	require.NoError(t, err)
	require.True(t, runtime.called)
	require.NotEqual(t, "42", runtime.input.AccountRef)
	require.True(t, isSafeLedgerRef(runtime.input.AccountRef), runtime.input.AccountRef)
	require.Equal(t, runtime.input.AccountRef, store.account.GetExtraString("cc_gateway_account_ref"))
	require.Equal(t, "claude-existing-bucket", runtime.input.EgressBucket)
}

func TestFormalPoolOperationsRuntimeRegister_BackfillsMissingEgressBucket(t *testing.T) {
	t.Parallel()

	store := newFormalPoolOperationsMutableStore(formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraOnboardingStage:   FormalPoolStageRefreshed,
		FormalPoolExtraRuntimeRegistered: "false",
		"cc_gateway_egress_bucket":       "",
	}))
	runtime := &formalPoolOperationsRuntimeFake{}
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{
		Accounts:         store,
		Proxy:            &formalPoolOperationsProxyFake{proxy: &Proxy{ID: 7, Protocol: "http", Host: "127.0.0.1", Port: 8080, Status: StatusActive}},
		CCGatewayRuntime: runtime,
	})

	_, err := svc.RuntimeRegister(context.Background(), store.account.ID)
	require.NoError(t, err)
	require.True(t, runtime.called)
	require.NotEmpty(t, runtime.input.EgressBucket)
	require.Equal(t, runtime.input.EgressBucket, store.account.GetExtraString("cc_gateway_egress_bucket"))
}

func TestFormalPoolOperationsHealthcheck_RejectsIncompleteRuntimeEvidenceBeforeRunner(t *testing.T) {
	t.Parallel()

	store := newFormalPoolOperationsMutableStore(formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraOnboardingStage:     FormalPoolStageRuntimeRegistered,
		FormalPoolExtraRuntimeRegistered:   "true",
		FormalPoolExtraRuntimeRegisteredAt: "",
	}))
	health := &formalPoolOperationsHealthcheckFake{}
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: store, Healthcheck: health})

	result, err := svc.Healthcheck(context.Background(), store.account.ID)
	require.Error(t, err)
	require.False(t, health.called)
	require.NotNil(t, result)
	require.Equal(t, "runtime_evidence_incomplete", store.account.GetExtraString(FormalPoolExtraLastFailureCode))
	require.False(t, store.account.Schedulable)
}

func TestFormalPoolOperationsRuntimeRegister_UsesAccountProxyAndBucket(t *testing.T) {
	t.Parallel()

	store := newFormalPoolOperationsMutableStore(formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraOnboardingStage:   FormalPoolStageRefreshed,
		"cc_gateway_egress_bucket":       "claude-bucket-from-account",
		FormalPoolExtraRuntimeRegistered: "false",
		FormalPoolExtraHealthcheckStatus: "failed",
		FormalPoolExtraLastFailureCode:   "missing_account_identity",
		FormalPoolExtraLastFailureSource: "cc_gateway_runtime_register",
	}))
	proxyID := int64(77)
	store.account.ProxyID = &proxyID
	runtime := &formalPoolOperationsRuntimeFake{}
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{
		Accounts:         store,
		Proxy:            &formalPoolOperationsProxyFake{proxy: &Proxy{ID: 77, Protocol: "http", Host: "127.0.0.1", Port: 8080, Username: "user", Password: "pass", Status: StatusActive}},
		CCGatewayRuntime: runtime,
	})

	got, err := svc.RuntimeRegister(context.Background(), store.account.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.True(t, runtime.called)
	require.Equal(t, ccGatewayAccountRef(store.account), runtime.input.AccountRef)
	require.Equal(t, "claude-bucket-from-account", runtime.input.EgressBucket)
	require.Equal(t, formalPoolSafeRef("proxy", "77"), runtime.input.ProxyRef)
	require.Equal(t, ccGatewayAnthropicPolicyVersion, runtime.input.PolicyVersion)
	require.Equal(t, "preserve_downstream_session_id", runtime.input.SessionPolicy)
	require.NotEmpty(t, runtime.input.ProxyURL)
	require.False(t, store.account.Schedulable)
	require.Equal(t, StatusActive, store.account.Status)
	require.Equal(t, FormalPoolStageRuntimeRegistered, store.account.GetExtraString(FormalPoolExtraOnboardingStage))
	require.Empty(t, store.account.GetExtraString(FormalPoolExtraQuarantineReason))
	require.Empty(t, store.account.GetExtraString(FormalPoolExtraQuarantineAt))
	require.Equal(t, "true", store.account.GetExtraString(FormalPoolExtraRuntimeRegistered))
	require.Equal(t, "pending", store.account.GetExtraString(FormalPoolExtraHealthcheckStatus))
}

func TestFormalPoolOperationsReplaceSetupToken_ClearsRuntimeRegisteredTimestamp(t *testing.T) {
	t.Parallel()

	store := newFormalPoolOperationsMutableStore(formalPoolDiagnosticsAccount(completeHealthcheckEvidenceExtra()))
	store.account.Type = AccountTypeSetupToken
	store.account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction
	oauth := &formalPoolOperationsOAuthFake{summary: FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true}, credentials: map[string]any{"access_token": "new-access", "refresh_token": "new-refresh", "scope": "user:inference"}}
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: store, OAuth: oauth})

	_, err := svc.ReplaceSetupToken(context.Background(), store.account.ID, FormalPoolSetupTokenReplaceRequest{SessionKey: "sk-ant-sid01-new-secret"})
	require.NoError(t, err)
	require.Equal(t, "false", store.account.GetExtraString(FormalPoolExtraRuntimeRegistered))
	require.Empty(t, store.account.GetExtraString(FormalPoolExtraRuntimeRegisteredAt))
}

func TestFormalPoolOperationsHealthcheck_UpdatesPassedEvidence(t *testing.T) {
	t.Parallel()

	rawRef := "hmac-sha256:" + strings.Repeat("1", 64)
	store := newFormalPoolOperationsMutableStore(formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraOnboardingStage:     FormalPoolStageRuntimeRegistered,
		FormalPoolExtraRuntimeRegistered:   "true",
		FormalPoolExtraRuntimeRegisteredAt: "2026-05-29T00:00:00Z",
		FormalPoolExtraQuarantineReason:    "reason_auth",
		FormalPoolExtraQuarantineAt:        "2026-05-29T00:00:00Z",
	}))
	health := &formalPoolOperationsHealthcheckFake{result: &FormalPoolAcceptanceResult{Status: FormalPoolOnboardingStatusHealthcheckPassed, StatusCodeBucket: "status_2xx", CCGatewaySeen: true, RawCapturePresent: true, RawCaptureRef: rawRef, FallbackDetected: false, ProxyMismatch: false, RiskTextDetected: false}}
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: store, Healthcheck: health})

	got, err := svc.Healthcheck(context.Background(), store.account.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.True(t, health.called)
	require.Equal(t, FormalPoolStageHealthcheckPassed, store.account.GetExtraString(FormalPoolExtraOnboardingStage))
	require.Equal(t, "passed", store.account.GetExtraString(FormalPoolExtraHealthcheckStatus))
	require.Equal(t, "status_2xx", store.account.GetExtraString(FormalPoolExtraHealthcheckStatusCodeBucket))
	require.Equal(t, rawRef, store.account.GetExtraString(FormalPoolExtraHealthcheckRawRef))
	require.Empty(t, store.account.GetExtraString(FormalPoolExtraQuarantineReason))
	require.Empty(t, store.account.GetExtraString(FormalPoolExtraQuarantineAt))
	require.True(t, formalPoolOpsBool(store.account.Extra[FormalPoolExtraHealthcheckCCGatewaySeen]))
	require.False(t, formalPoolOpsBool(store.account.Extra[FormalPoolExtraHealthcheckFallbackDetected]))
	require.False(t, formalPoolOpsBool(store.account.Extra[FormalPoolExtraHealthcheckProxyMismatch]))
	require.False(t, formalPoolOpsBool(store.account.Extra[FormalPoolExtraHealthcheckRiskTextDetected]))
	require.Equal(t, "passed", store.account.GetExtraString(FormalPoolExtraLastHealthcheckResult))
	require.False(t, store.account.Schedulable)
	require.Equal(t, StatusActive, store.account.Status)
}

func TestFormalPoolOperationsHealthcheck_QuarantineResultStaysUnschedulable(t *testing.T) {
	t.Parallel()

	rawRef := "hmac-sha256:" + strings.Repeat("2", 64)
	store := newFormalPoolOperationsMutableStore(formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraOnboardingStage:     FormalPoolStageRuntimeRegistered,
		FormalPoolExtraRuntimeRegistered:   "true",
		FormalPoolExtraRuntimeRegisteredAt: "2026-05-29T00:00:00Z",
	}))
	health := &formalPoolOperationsHealthcheckFake{
		mutate: func() {
			store.account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageQuarantined
			store.account.Extra[FormalPoolExtraHealthcheckStatus] = "quarantined"
			store.account.Status = StatusError
			store.account.Schedulable = false
		},
		result: &FormalPoolAcceptanceResult{Status: FormalPoolOnboardingStatusHealthcheckPassed, StatusCodeBucket: "status_2xx", CCGatewaySeen: true, RawCapturePresent: true, RawCaptureRef: rawRef},
	}
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: store, Healthcheck: health})

	got, err := svc.Healthcheck(context.Background(), store.account.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.False(t, store.account.Schedulable)
	require.Equal(t, FormalPoolStageQuarantined, store.account.GetExtraString(FormalPoolExtraOnboardingStage))
	require.Equal(t, "quarantined", store.account.GetExtraString(FormalPoolExtraHealthcheckStatus))
}

func TestFormalPoolOperationsDiagnostics_RequiresRuntimeRegisteredEvidenceBeforeStartWarmingRecommendation(t *testing.T) {
	t.Parallel()

	account := formalPoolDiagnosticsAccount(completeHealthcheckEvidenceExtra())
	account.Extra[FormalPoolExtraRuntimeRegistered] = "false"
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: formalPoolOperationsAccountFake{account: account}})

	got, err := svc.Diagnostics(context.Background(), account.ID)
	require.NoError(t, err)
	require.NotContains(t, actionKeys(got.RecommendedActions), "start_warming")
	require.Contains(t, actionKeys(got.RecommendedActions), "runtime_register")
	require.NotContains(t, actionKeys(got.RecommendedActions), "healthcheck")
	require.Contains(t, got.Checks, FormalPoolAcceptanceCheck{Name: "cc_gateway_runtime_registered", Status: "fail", Message: "cc gateway runtime identity/bucket mapping must be registered before warming"})
}

func TestFormalPoolOperationsDiagnostics_RequiresRuntimeRegisteredTimestampBeforeStartWarmingRecommendation(t *testing.T) {
	t.Parallel()

	account := formalPoolDiagnosticsAccount(completeHealthcheckEvidenceExtra())
	account.Extra[FormalPoolExtraRuntimeRegistered] = "true"
	account.Extra[FormalPoolExtraRuntimeRegisteredAt] = ""
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: formalPoolOperationsAccountFake{account: account}})

	got, err := svc.Diagnostics(context.Background(), account.ID)
	require.NoError(t, err)
	require.NotContains(t, actionKeys(got.RecommendedActions), "start_warming")
	require.Contains(t, actionKeys(got.RecommendedActions), "runtime_register")
	require.Contains(t, got.Checks, FormalPoolAcceptanceCheck{Name: "cc_gateway_runtime_registered", Status: "fail", Message: "cc gateway runtime identity/bucket mapping must include registration timestamp before warming"})
}

func TestFormalPoolOperationsDiagnostics_RecommendsStartWarmingWithRuntimeRegisteredAndFullEvidence(t *testing.T) {
	t.Parallel()

	account := formalPoolDiagnosticsAccount(completeHealthcheckEvidenceExtra())
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: formalPoolOperationsAccountFake{account: account}})

	got, err := svc.Diagnostics(context.Background(), account.ID)
	require.NoError(t, err)
	require.Contains(t, actionKeys(got.RecommendedActions), "start_warming")
	require.Contains(t, got.Checks, FormalPoolAcceptanceCheck{Name: "cc_gateway_runtime_registered", Status: "pass"})
}

func TestFormalPoolOperationsDiagnostics_HealthyProductionRecommendsMonitorOnly(t *testing.T) {
	t.Parallel()

	account := formalPoolDiagnosticsAccount(completeHealthcheckEvidenceExtra())
	account.Status = StatusActive
	account.Schedulable = true
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction
	account.Extra[FormalPoolExtraHealthcheckStatus] = "passed"
	account.Extra[FormalPoolExtraLastFailureCode] = ""
	account.Extra[FormalPoolExtraLastFailureSource] = ""
	account.Extra[FormalPoolExtraQuarantineReason] = ""
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: formalPoolOperationsAccountFake{account: account}})

	got, err := svc.Diagnostics(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, string(FormalPoolFailureOriginUnknown), got.FailureOrigin)
	require.Equal(t, []string{"monitor"}, actionKeys(got.RecommendedActions))
	require.NotContains(t, actionKeys(got.RecommendedActions), "healthcheck")
}

func TestFormalPoolOperationsDiagnostics_WarmingRecommendsPromoteProduction(t *testing.T) {
	t.Parallel()

	account := formalPoolDiagnosticsAccount(completeHealthcheckEvidenceExtra())
	account.Status = StatusActive
	account.Schedulable = true
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageWarming
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: formalPoolOperationsAccountFake{account: account}})

	got, err := svc.Diagnostics(context.Background(), account.ID)
	require.NoError(t, err)
	require.Contains(t, actionKeys(got.RecommendedActions), "promote_production")
	require.NotContains(t, actionKeys(got.RecommendedActions), "healthcheck")
}

func TestFormalPoolOperationsDiagnostics_RateLimitRecommendsWaitingNotHealthcheck(t *testing.T) {
	t.Parallel()

	account := formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraOnboardingStage:             FormalPoolStageQuarantined,
		FormalPoolExtraHealthcheckStatus:           FormalPoolOnboardingStatusQuarantined,
		FormalPoolExtraHealthcheckStatusCodeBucket: "status_429",
		FormalPoolExtraLastFailureCode:             "long_context_usage_credits",
		FormalPoolExtraLastFailureSource:           "rate_limit_service",
		FormalPoolExtraRateLimitErrorClass:         "long_context_usage_credits",
		FormalPoolExtraRateLimitWindow:             "5h",
		FormalPoolExtraRateLimitAction:             "cooldown",
		FormalPoolExtraRateLimitResetBucket:        "5h",
	})
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: formalPoolOperationsAccountFake{account: account}})

	got, err := svc.Diagnostics(context.Background(), account.ID)

	require.NoError(t, err)
	require.Equal(t, string(FormalPoolFailureOriginUpstream), got.FailureOrigin)
	require.Equal(t, "long_context_usage_credits", got.RateLimitErrorClass)
	require.Equal(t, "5h", got.RateLimitWindow)
	require.Contains(t, actionKeys(got.RecommendedActions), "wait_rate_limit")
	require.NotContains(t, actionKeys(got.RecommendedActions), "healthcheck")
	require.NotContains(t, actionKeys(got.RecommendedActions), "repair_token")
}

func TestFormalPoolOperationsDiagnostics_InvalidGrantRecommendationsByAccountType(t *testing.T) {
	t.Parallel()

	setup := formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraOnboardingStage:   FormalPoolStageQuarantined,
		FormalPoolExtraHealthcheckStatus: FormalPoolOnboardingStatusQuarantined,
		FormalPoolExtraLastFailureOrigin: string(FormalPoolFailureOriginTokenExchange),
		FormalPoolExtraLastFailureCode:   "refresh_token_invalid",
		FormalPoolExtraLastFailureSource: "rate_limit_service",
		FormalPoolExtraQuarantineReason:  "refresh_token_invalid",
	})
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: formalPoolOperationsAccountFake{account: setup}})
	got, err := svc.Diagnostics(context.Background(), setup.ID)
	require.NoError(t, err)
	require.Contains(t, actionKeys(got.RecommendedActions), "replace_setup_token")
	require.NotContains(t, actionKeys(got.RecommendedActions), "healthcheck")

	oauth := formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraOnboardingStage:   FormalPoolStageQuarantined,
		FormalPoolExtraHealthcheckStatus: FormalPoolOnboardingStatusQuarantined,
		FormalPoolExtraLastFailureOrigin: string(FormalPoolFailureOriginTokenExchange),
		FormalPoolExtraLastFailureCode:   "refresh_token_invalid",
		FormalPoolExtraLastFailureSource: "rate_limit_service",
		FormalPoolExtraQuarantineReason:  "refresh_token_invalid",
	})
	oauth.Type = AccountTypeOAuth
	svc = NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: formalPoolOperationsAccountFake{account: oauth}})
	got, err = svc.Diagnostics(context.Background(), oauth.ID)
	require.NoError(t, err)
	require.Contains(t, actionKeys(got.RecommendedActions), "reauthorize_oauth")
	require.NotContains(t, actionKeys(got.RecommendedActions), "healthcheck")
}

func TestFormalPoolOperationsDiagnostics_ExposesSafeGatewayAndOnboardingSignals(t *testing.T) {
	t.Parallel()

	account := formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraOnboardingStage:           FormalPoolStageQuarantined,
		FormalPoolExtraLastCCGatewayErrorCode:    "missing_account_identity",
		FormalPoolExtraOnboardingLastErrorCode:   "rate_limit_exceeded",
		FormalPoolExtraOnboardingLastErrorBucket: "status_429",
	})
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: formalPoolOperationsAccountFake{account: account}})

	got, err := svc.Diagnostics(context.Background(), account.ID)

	require.NoError(t, err)
	require.Equal(t, "missing_account_identity", got.LastCCGatewayErrorCode)
	require.Equal(t, "rate_limit_exceeded", got.OnboardingLastErrorCode)
	require.Equal(t, "status_429", got.OnboardingLastErrorBucket)
	require.Contains(t, actionKeys(got.RecommendedActions), "wait_rate_limit")
}

func TestFormalPoolOperationsDiagnostics_SanitizesGatewayAndOnboardingSignals(t *testing.T) {
	t.Parallel()

	account := formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraOnboardingStage:           FormalPoolStageQuarantined,
		FormalPoolExtraLastCCGatewayErrorCode:    "raw_cch",
		FormalPoolExtraOnboardingLastErrorCode:   "admin@example.com sk-ant-sid01-secret",
		FormalPoolExtraOnboardingLastErrorBucket: "status_401 access_token=secret",
	})
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: formalPoolOperationsAccountFake{account: account}})

	got, err := svc.Diagnostics(context.Background(), account.ID)

	require.NoError(t, err)
	require.Empty(t, got.LastCCGatewayErrorCode)
	require.Empty(t, got.OnboardingLastErrorCode)
	require.Empty(t, got.OnboardingLastErrorBucket)
}

func TestFormalPoolOperationsDiagnostics_PassThroughNoReset429IsNotRateLimited(t *testing.T) {
	t.Parallel()

	account := formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraOnboardingStage:           FormalPoolStageProduction,
		FormalPoolExtraRateLimitAction:           "pass_through",
		FormalPoolExtraRateLimitWindow:           "no_reset",
		FormalPoolExtraRateLimitResetBucket:      "missing",
		FormalPoolExtraOnboardingLastErrorBucket: "status_429",
		FormalPoolExtraOnboardingLastErrorCode:   "unknown_no_reset_429",
	})
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: formalPoolOperationsAccountFake{account: account}})

	got, err := svc.Diagnostics(context.Background(), account.ID)

	require.NoError(t, err)
	require.NotContains(t, actionKeys(got.RecommendedActions), "wait_rate_limit")
}

func TestFormalPoolOperationsDiagnostics_OnboardingOnlyStatusBucketsClassifyOrigin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		bucket     string
		code       string
		wantOrigin string
		wantAction string
	}{
		{name: "401", bucket: "status_401", code: "refresh_required", wantOrigin: string(FormalPoolFailureOriginUpstream), wantAction: "manual_review"},
		{name: "403", bucket: "status_403", code: "forbidden", wantOrigin: string(FormalPoolFailureOriginUpstream), wantAction: "manual_review"},
		{name: "429", bucket: "status_429", code: "rate_limited", wantOrigin: string(FormalPoolFailureOriginUpstream), wantAction: "wait_rate_limit"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			account := formalPoolDiagnosticsAccount(map[string]any{
				FormalPoolExtraOnboardingStage:           FormalPoolStageQuarantined,
				FormalPoolExtraOnboardingLastErrorCode:   tt.code,
				FormalPoolExtraOnboardingLastErrorBucket: tt.bucket,
			})
			svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: formalPoolOperationsAccountFake{account: account}})

			got, err := svc.Diagnostics(context.Background(), account.ID)

			require.NoError(t, err)
			require.Equal(t, tt.wantOrigin, got.FailureOrigin)
			require.Contains(t, actionKeys(got.RecommendedActions), tt.wantAction)
		})
	}
}

func TestFormalPoolOperationsHealthcheckFailurePersistsSafeClassificationAndDiagnostics(t *testing.T) {
	t.Parallel()

	account := formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraOnboardingStage:     FormalPoolStageRuntimeRegistered,
		FormalPoolExtraRuntimeRegistered:   "true",
		FormalPoolExtraRuntimeRegisteredAt: "2026-05-29T00:00:00Z",
	})
	account.Status = StatusActive
	store := newFormalPoolOperationsMutableStore(account)
	rawRef := "hmac-sha256:" + strings.Repeat("7", 64)
	healthcheck := &formalPoolOperationsHealthcheckFake{result: &FormalPoolAcceptanceResult{
		Status:            "failed_acceptance",
		StatusCodeBucket:  "status_4xx",
		CCGatewaySeen:     true,
		RawCapturePresent: true,
		RawCaptureRef:     rawRef,
	}}
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: store, Healthcheck: healthcheck})

	result, err := svc.healthcheckAccountUnlogged(context.Background(), account.ID)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "unknown", store.account.Extra["healthcheck_safe_error_code"])
	require.Equal(t, "unknown", store.account.Extra["healthcheck_safe_error_bucket"])
	payload := mustJSON(t, result.Diagnostics)
	require.Contains(t, payload, `"healthcheck_safe_error_code":"unknown"`)
	require.Contains(t, payload, `"healthcheck_safe_error_bucket":"unknown"`)
}

func TestFormalPoolOperationsPromoteProduction_EarlyFailureWritesAudit(t *testing.T) {
	t.Parallel()

	audit := &formalPoolOperationsAuditFake{}
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Audit: audit})
	_, err := svc.PromoteProduction(context.Background(), 42)
	require.Error(t, err)
	require.Len(t, audit.events, 1)
	require.Equal(t, "promote_production", audit.events[0].Action)
	require.Equal(t, int64(42), audit.events[0].AccountID)
	require.False(t, audit.events[0].Success)
	require.Equal(t, "FORMAL_POOL_OPERATIONS_UNAVAILABLE", audit.events[0].FailureCode)

	audit = &formalPoolOperationsAuditFake{}
	svc = NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: formalPoolOperationsErrorStore{err: errors.New("database unavailable")}, Audit: audit})
	_, err = svc.PromoteProduction(context.Background(), 43)
	require.Error(t, err)
	require.Len(t, audit.events, 1)
	require.Equal(t, "promote_production", audit.events[0].Action)
	require.Equal(t, int64(43), audit.events[0].AccountID)
	require.False(t, audit.events[0].Success)
	require.Equal(t, "operation_failed", audit.events[0].FailureCode)
}

func TestFormalPoolOperationsPromoteProduction_RequiresWarmingAndCompleteEvidence(t *testing.T) {
	t.Parallel()

	store := newFormalPoolOperationsMutableStore(formalPoolDiagnosticsAccount(completeHealthcheckEvidenceExtra()))
	store.account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageHealthcheckPassed
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: store})

	_, err := svc.PromoteProduction(context.Background(), store.account.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "WARMING_NOT_STARTED")
	require.NotEqual(t, FormalPoolStageProduction, store.account.GetExtraString(FormalPoolExtraOnboardingStage))

	store.account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageWarming
	store.account.Extra[FormalPoolExtraHealthcheckRawRef] = ""
	_, err = svc.PromoteProduction(context.Background(), store.account.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "PRODUCTION_EVIDENCE_INCOMPLETE")
	require.NotEqual(t, FormalPoolStageProduction, store.account.GetExtraString(FormalPoolExtraOnboardingStage))
}

func TestFormalPoolSafeOperator_AllowsOperationalEmailAndRejectsSecrets(t *testing.T) {
	t.Parallel()

	require.Equal(t, "ops-user@example.com", formalPoolSafeOperator("ops-user@example.com"))
	require.Equal(t, "admin:99", formalPoolSafeOperator("admin:99"))
	require.Empty(t, formalPoolSafeOperator("ops-user@example.com sk-ant-sid-secret"))
	require.Empty(t, formalPoolSafeOperator("http://proxy-user:proxy-pass@example.com"))
}

func TestFormalPoolOperationsPromoteProduction_SetsProductionClearsCurrentQuarantineAndAudits(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 30, 9, 10, 11, 0, time.UTC)
	store := newFormalPoolOperationsMutableStore(formalPoolDiagnosticsAccount(completeHealthcheckEvidenceExtra()))
	store.account.Status = StatusActive
	store.account.Schedulable = true
	store.account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageWarming
	store.account.Extra[FormalPoolExtraQuarantineReason] = "refresh_token_invalid"
	store.account.Extra[FormalPoolExtraQuarantineAt] = "2026-05-29T00:00:00Z"
	store.account.Extra[FormalPoolExtraRiskEventRef] = "hmac-sha256:" + strings.Repeat("f", 64)
	audit := &formalPoolOperationsAuditFake{}
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: store, Audit: audit, Now: func() time.Time { return now }})

	ctx := WithFormalPoolOperationOperator(context.Background(), "admin:99")
	got, err := svc.PromoteProduction(ctx, store.account.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.True(t, store.account.Schedulable)
	require.Equal(t, StatusActive, store.account.Status)
	require.Equal(t, FormalPoolStageProduction, store.account.GetExtraString(FormalPoolExtraOnboardingStage))
	require.Equal(t, PoolProfileNormal, store.account.GetExtraString(FormalPoolExtraPoolProfileEffective))
	require.Equal(t, FormalPoolWeightNormal, store.account.GetExtraString(FormalPoolExtraPoolWeightMode))
	require.Empty(t, store.account.GetExtraString(FormalPoolExtraQuarantineReason))
	require.Empty(t, store.account.GetExtraString(FormalPoolExtraQuarantineAt))
	require.NotEmpty(t, store.account.GetExtraString(FormalPoolExtraRiskEventRef), "historical safe risk ref must be preserved")
	require.Len(t, audit.events, 1)
	require.Equal(t, "admin:99", audit.events[0].Operator)
	require.Equal(t, store.account.ID, audit.events[0].AccountID)
	require.Equal(t, "promote_production", audit.events[0].Action)
	require.Equal(t, FormalPoolStageWarming, audit.events[0].BeforeStage)
	require.Equal(t, FormalPoolStageProduction, audit.events[0].AfterStage)
	require.Equal(t, "refresh_token_invalid", audit.events[0].ReasonBucket)
	require.True(t, audit.events[0].Success)
}

func TestFormalPoolOperationsPromoteProduction_AlreadyProductionNoopDoesNotMutateEvidence(t *testing.T) {
	t.Parallel()

	store := newFormalPoolOperationsMutableStore(formalPoolDiagnosticsAccount(completeHealthcheckEvidenceExtra()))
	store.account.Status = StatusActive
	store.account.Schedulable = true
	store.account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction
	beforeUpdates := store.stateUpdates
	audit := &formalPoolOperationsAuditFake{}
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: store, Audit: audit})

	got, err := svc.PromoteProduction(context.Background(), store.account.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, beforeUpdates, store.stateUpdates, "idempotent production no-op must not rewrite evidence")
	require.Len(t, audit.events, 1)
	require.Equal(t, "promote_production", audit.events[0].Action)
	require.Equal(t, FormalPoolStageProduction, audit.events[0].BeforeStage)
	require.Equal(t, FormalPoolStageProduction, audit.events[0].AfterStage)
	require.True(t, audit.events[0].Success)
	require.True(t, audit.events[0].Noop)
}

func TestFormalPoolOperationsMutatingOperations_WriteAuditEvents(t *testing.T) {
	t.Parallel()

	run := func(name string, wantAction, wantBefore, wantAfter string, setup func(*formalPoolOperationsMutableStore) *FormalPoolOperationsService, call func(*FormalPoolOperationsService, *formalPoolOperationsMutableStore) error) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := newFormalPoolOperationsMutableStore(formalPoolDiagnosticsAccount(completeHealthcheckEvidenceExtra()))
			audit := &formalPoolOperationsAuditFake{}
			svc := setup(store)
			svc.audit = audit

			err := call(svc, store)
			require.NoError(t, err)
			require.NotEmpty(t, audit.events)
			event := audit.events[len(audit.events)-1]
			require.Equal(t, wantAction, event.Action)
			require.Equal(t, store.account.ID, event.AccountID)
			require.Equal(t, wantBefore, event.BeforeStage)
			require.Equal(t, wantAfter, event.AfterStage)
			require.True(t, event.Success)
			require.NotEmpty(t, event.Operator)
			require.NotEmpty(t, event.Timestamp)
		})
	}

	run("replace setup token", "replace_setup_token", FormalPoolStageQuarantined, FormalPoolStageRefreshed,
		func(store *formalPoolOperationsMutableStore) *FormalPoolOperationsService {
			store.account.Type = AccountTypeSetupToken
			store.account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageQuarantined
			store.account.Extra[FormalPoolExtraHealthcheckStatus] = FormalPoolOnboardingStatusQuarantined
			store.account.Status = StatusError
			store.account.Schedulable = false
			return NewFormalPoolOperationsService(FormalPoolOperationsDeps{
				Accounts: store,
				OAuth: &formalPoolOperationsOAuthFake{
					summary:     FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true},
					credentials: map[string]any{"access_token": "new-access", "refresh_token": "new-refresh", "scope": "user:inference"},
				},
			})
		},
		func(svc *FormalPoolOperationsService, store *formalPoolOperationsMutableStore) error {
			_, err := svc.ReplaceSetupToken(context.Background(), store.account.ID, FormalPoolSetupTokenReplaceRequest{SessionKey: "sk-ant-sid01-new-secret"})
			return err
		})

	run("runtime register", "runtime_register", FormalPoolStageRefreshed, FormalPoolStageRuntimeRegistered,
		func(store *formalPoolOperationsMutableStore) *FormalPoolOperationsService {
			store.account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageRefreshed
			store.account.Extra[FormalPoolExtraRuntimeRegistered] = "false"
			proxyID := int64(77)
			store.account.ProxyID = &proxyID
			return NewFormalPoolOperationsService(FormalPoolOperationsDeps{
				Accounts:         store,
				Proxy:            &formalPoolOperationsProxyFake{proxy: &Proxy{ID: 77, Protocol: "http", Host: "127.0.0.1", Port: 8080, Status: StatusActive}},
				CCGatewayRuntime: &formalPoolOperationsRuntimeFake{},
			})
		},
		func(svc *FormalPoolOperationsService, store *formalPoolOperationsMutableStore) error {
			_, err := svc.RuntimeRegister(context.Background(), store.account.ID)
			return err
		})

	run("healthcheck", "healthcheck", FormalPoolStageRuntimeRegistered, FormalPoolStageHealthcheckPassed,
		func(store *formalPoolOperationsMutableStore) *FormalPoolOperationsService {
			store.account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageRuntimeRegistered
			return NewFormalPoolOperationsService(FormalPoolOperationsDeps{
				Accounts: store,
				Healthcheck: &formalPoolOperationsHealthcheckFake{result: &FormalPoolAcceptanceResult{
					Status: FormalPoolOnboardingStatusHealthcheckPassed, StatusCodeBucket: "status_2xx", CCGatewaySeen: true,
					RawCapturePresent: true, RawCaptureRef: "hmac-sha256:" + strings.Repeat("a", 64),
				}},
			})
		},
		func(svc *FormalPoolOperationsService, store *formalPoolOperationsMutableStore) error {
			_, err := svc.Healthcheck(context.Background(), store.account.ID)
			return err
		})

	run("start warming", "start_warming", FormalPoolStageHealthcheckPassed, FormalPoolStageWarming,
		func(store *formalPoolOperationsMutableStore) *FormalPoolOperationsService {
			return NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: store})
		},
		func(svc *FormalPoolOperationsService, store *formalPoolOperationsMutableStore) error {
			_, err := svc.StartWarming(context.Background(), store.account.ID)
			return err
		})

	run("swap proxy", "swap_proxy", FormalPoolStageWarming, FormalPoolStageRefreshed,
		func(store *formalPoolOperationsMutableStore) *FormalPoolOperationsService {
			store.account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageWarming
			store.account.Schedulable = true
			store.account.Status = StatusActive
			store.account.Credentials = map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:inference"}
			return NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: store})
		},
		func(svc *FormalPoolOperationsService, store *formalPoolOperationsMutableStore) error {
			_, err := svc.SwapProxy(context.Background(), store.account.ID, FormalPoolProxySwapRequest{ProxyID: 88})
			return err
		})
}

func TestFormalPoolOperationsStartWarming_RequiresHealthcheckPassedEvidence(t *testing.T) {
	t.Parallel()

	store := newFormalPoolOperationsMutableStore(formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraOnboardingStage:             FormalPoolStageHealthcheckPassed,
		FormalPoolExtraHealthcheckStatus:           "passed",
		FormalPoolExtraHealthcheckStatusCodeBucket: "status_2xx",
		FormalPoolExtraRuntimeRegistered:           "true",
		FormalPoolExtraHealthcheckCCGatewaySeen:    true,
	}))
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: store})

	_, err := svc.StartWarming(context.Background(), store.account.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "HEALTHCHECK_EVIDENCE_INCOMPLETE")
	require.False(t, store.account.Schedulable)
	require.NotEqual(t, FormalPoolStageWarming, store.account.GetExtraString(FormalPoolExtraOnboardingStage))
}

func TestFormalPoolOperationsStartWarming_RequiresRuntimeEgressBucketEnabled(t *testing.T) {
	t.Parallel()

	store := newFormalPoolOperationsMutableStore(formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraOnboardingStage:             FormalPoolStageHealthcheckPassed,
		FormalPoolExtraRuntimeRegistered:           "true",
		FormalPoolExtraRuntimeRegisteredAt:         "2026-05-29T00:00:00Z",
		"cc_gateway_egress_bucket_enabled":         "true",
		"cc_gateway_egress_bucket":                 "",
		FormalPoolExtraHealthcheckStatus:           "passed",
		FormalPoolExtraHealthcheckStatusCodeBucket: "status_2xx",
		FormalPoolExtraHealthcheckRawRef:           "hmac-sha256:" + strings.Repeat("a", 64),
		FormalPoolExtraHealthcheckCCGatewaySeen:    true,
		FormalPoolExtraHealthcheckFallbackDetected: false,
		FormalPoolExtraHealthcheckProxyMismatch:    false,
		FormalPoolExtraHealthcheckRiskTextDetected: false,
	}))
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: store})

	_, err := svc.StartWarming(context.Background(), store.account.ID)

	require.Error(t, err)
	require.False(t, store.account.Schedulable, "warming must fail closed without persisted egress bucket")
}

func TestFormalPoolOperationsStartWarming_RequiresRuntimeRegisteredTimestamp(t *testing.T) {
	t.Parallel()

	extra := completeHealthcheckEvidenceExtra()
	extra[FormalPoolExtraRuntimeRegistered] = "true"
	extra[FormalPoolExtraRuntimeRegisteredAt] = ""
	store := newFormalPoolOperationsMutableStore(formalPoolDiagnosticsAccount(extra))
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: store})

	_, err := svc.StartWarming(context.Background(), store.account.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "HEALTHCHECK_EVIDENCE_INCOMPLETE")
	require.False(t, store.account.Schedulable)
	require.NotEqual(t, FormalPoolStageWarming, store.account.GetExtraString(FormalPoolExtraOnboardingStage))
}

func TestFormalPoolOperationsStartWarming_SetsLowWeightNormalProfile(t *testing.T) {
	t.Parallel()

	store := newFormalPoolOperationsMutableStore(formalPoolDiagnosticsAccount(completeHealthcheckEvidenceExtra()))
	store.account.Extra[FormalPoolExtraQuarantineReason] = "reason_auth"
	store.account.Extra[FormalPoolExtraQuarantineAt] = "2026-05-29T00:00:00Z"
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: store, Now: func() time.Time { return time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC) }})

	got, err := svc.StartWarming(context.Background(), store.account.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.True(t, store.account.Schedulable)
	require.Equal(t, StatusActive, store.account.Status)
	require.Equal(t, FormalPoolStageWarming, store.account.GetExtraString(FormalPoolExtraOnboardingStage))
	require.NotEqual(t, FormalPoolStageProduction, store.account.GetExtraString(FormalPoolExtraOnboardingStage))
	require.Equal(t, PoolProfileNormal, store.account.GetExtraString(FormalPoolExtraPoolProfileEffective))
	require.Equal(t, FormalPoolWeightLow, store.account.GetExtraString(FormalPoolExtraPoolWeightMode))
	require.Empty(t, store.account.GetExtraString(FormalPoolExtraQuarantineReason))
	require.Empty(t, store.account.GetExtraString(FormalPoolExtraQuarantineAt))
	require.NotEmpty(t, store.account.GetExtraString(FormalPoolExtraWarmingStartedAt))
	require.NotEmpty(t, store.account.GetExtraString(FormalPoolExtraWarmingUntil))
}

func TestFormalPoolOperationsSwapProxy_SetsProxyAndResetsRuntimeAndHealthcheck(t *testing.T) {
	t.Parallel()

	store := newFormalPoolOperationsMutableStore(formalPoolDiagnosticsAccount(completeHealthcheckEvidenceExtra()))
	store.account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageWarming
	store.account.Schedulable = true
	store.account.Status = StatusActive
	store.account.Credentials = map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:inference"}
	store.account.Extra[FormalPoolExtraQuarantineReason] = "reason_proxy"
	store.account.Extra[FormalPoolExtraQuarantineAt] = "2026-05-29T00:00:00Z"
	svc := NewFormalPoolOperationsService(FormalPoolOperationsDeps{Accounts: store})

	got, err := svc.SwapProxy(context.Background(), store.account.ID, FormalPoolProxySwapRequest{ProxyID: 88})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, store.account.ProxyID)
	require.Equal(t, int64(88), *store.account.ProxyID)
	require.False(t, store.account.Schedulable)
	require.Equal(t, FormalPoolStageRefreshed, store.account.GetExtraString(FormalPoolExtraOnboardingStage))
	require.Empty(t, store.account.GetExtraString(FormalPoolExtraQuarantineReason))
	require.Empty(t, store.account.GetExtraString(FormalPoolExtraQuarantineAt))
	require.Equal(t, "false", store.account.GetExtraString(FormalPoolExtraRuntimeRegistered))
	require.Equal(t, "pending", store.account.GetExtraString(FormalPoolExtraHealthcheckStatus))
	require.Empty(t, store.account.GetExtraString(FormalPoolExtraHealthcheckStatusCodeBucket))
	require.Empty(t, store.account.GetExtraString(FormalPoolExtraHealthcheckRawRef))
	require.False(t, formalPoolOpsBool(store.account.Extra[FormalPoolExtraHealthcheckCCGatewaySeen]))
	require.False(t, formalPoolOpsBool(store.account.Extra[FormalPoolExtraHealthcheckFallbackDetected]))
	require.False(t, formalPoolOpsBool(store.account.Extra[FormalPoolExtraHealthcheckProxyMismatch]))
	require.False(t, formalPoolOpsBool(store.account.Extra[FormalPoolExtraHealthcheckRiskTextDetected]))
	require.Equal(t, string(FormalPoolFailureOriginProxy), store.account.GetExtraString(FormalPoolExtraLastFailureOrigin))
	require.Equal(t, "proxy_swapped_revalidation_required", store.account.GetExtraString(FormalPoolExtraLastFailureCode))
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
		FormalPoolExtraOnboardingStage:     FormalPoolStageQuarantined,
		FormalPoolExtraHealthcheckStatus:   FormalPoolOnboardingStatusQuarantined,
		FormalPoolExtraQuarantineReason:    "reason_auth",
		FormalPoolExtraRiskEventRef:        "hmac-sha256:" + strings.Repeat("d", 64),
		"cc_gateway_account_ref":           "hmac-sha256:" + strings.Repeat("e", 64),
		"cc_gateway_enabled":               "true",
		"cc_gateway_routes":                string(ccGatewayRouteNativeMessages),
		"cc_gateway_egress_bucket_enabled": "true",
		"cc_gateway_egress_bucket":         "claude-1234567890abcdef",
		FormalPoolExtraRuntimeRegistered:   true,
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

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return string(raw)
}

type formalPoolOperationsErrorStore struct {
	err error
}

func (s formalPoolOperationsErrorStore) GetFormalPoolAccount(context.Context, int64) (*Account, error) {
	return nil, s.err
}

func (s formalPoolOperationsErrorStore) UpdateFormalPoolAccountCredentials(context.Context, int64, map[string]any) (*Account, error) {
	return nil, s.err
}

func (s formalPoolOperationsErrorStore) UpdateFormalPoolAccountState(context.Context, int64, bool, string, map[string]any) (*Account, error) {
	return nil, s.err
}

func (s formalPoolOperationsErrorStore) ActivateFormalPoolAccount(context.Context, int64, map[string]any) (*Account, error) {
	return nil, s.err
}

func (s formalPoolOperationsErrorStore) UpdateFormalPoolAccountProxy(context.Context, int64, int64, map[string]any) (*Account, error) {
	return nil, s.err
}

type formalPoolOperationsMutableStore struct {
	account            *Account
	credentialsUpdates int
	stateUpdates       int
	proxyUpdates       int
}

func newFormalPoolOperationsMutableStore(account *Account) *formalPoolOperationsMutableStore {
	return &formalPoolOperationsMutableStore{account: account}
}

func (f *formalPoolOperationsMutableStore) GetFormalPoolAccount(context.Context, int64) (*Account, error) {
	return f.account, nil
}

func (f *formalPoolOperationsMutableStore) UpdateFormalPoolAccountCredentials(_ context.Context, _ int64, credentials map[string]any) (*Account, error) {
	f.credentialsUpdates++
	f.account.Credentials = cloneCredentials(credentials)
	return f.account, nil
}

func (f *formalPoolOperationsMutableStore) UpdateFormalPoolAccountState(_ context.Context, _ int64, schedulable bool, status string, extra map[string]any) (*Account, error) {
	f.stateUpdates++
	f.account.Schedulable = schedulable
	if strings.TrimSpace(status) != "" {
		f.account.Status = status
	}
	if f.account.Extra == nil {
		f.account.Extra = map[string]any{}
	}
	for k, v := range extra {
		f.account.Extra[k] = v
	}
	return f.account, nil
}

func (f *formalPoolOperationsMutableStore) ActivateFormalPoolAccount(ctx context.Context, id int64, extra map[string]any) (*Account, error) {
	return f.UpdateFormalPoolAccountState(ctx, id, true, StatusActive, extra)
}

func (f *formalPoolOperationsMutableStore) UpdateFormalPoolAccountProxy(ctx context.Context, id int64, proxyID int64, extra map[string]any) (*Account, error) {
	f.proxyUpdates++
	f.account.ProxyID = &proxyID
	return f.UpdateFormalPoolAccountState(ctx, id, false, StatusActive, extra)
}

type formalPoolOperationsOAuthFake struct {
	summary     FormalPoolOAuthTokenSummary
	credentials map[string]any
	err         error
	sessionKey  string
	proxyID     int64
}

func (f *formalPoolOperationsOAuthFake) GenerateFormalAuthURL(context.Context, int64) (FormalPoolOAuthURL, error) {
	return FormalPoolOAuthURL{}, nil
}

func (f *formalPoolOperationsOAuthFake) ExchangeCode(context.Context, string, string, int64) (FormalPoolOAuthTokenSummary, map[string]any, error) {
	return FormalPoolOAuthTokenSummary{}, nil, nil
}

func (f *formalPoolOperationsOAuthFake) SetupTokenCookieAuth(_ context.Context, sessionKey string, proxyID int64) (FormalPoolOAuthTokenSummary, map[string]any, error) {
	f.sessionKey = sessionKey
	f.proxyID = proxyID
	if f.err != nil {
		return FormalPoolOAuthTokenSummary{}, nil, f.err
	}
	return f.summary, cloneCredentials(f.credentials), nil
}

type formalPoolOperationsRuntimeFake struct {
	called bool
	input  FormalPoolCCGatewayRuntimeRegistration
	err    error
}

func (f *formalPoolOperationsRuntimeFake) RegisterCCGatewayRuntime(_ context.Context, input FormalPoolCCGatewayRuntimeRegistration) error {
	f.called = true
	f.input = input
	return f.err
}

type formalPoolOperationsHealthcheckFake struct {
	called bool
	input  FormalPoolAcceptanceInput
	result *FormalPoolAcceptanceResult
	err    error
	mutate func()
}

func (f *formalPoolOperationsHealthcheckFake) RunHealthcheck(_ context.Context, input FormalPoolAcceptanceInput) (*FormalPoolAcceptanceResult, error) {
	f.called = true
	f.input = input
	if f.err != nil {
		return nil, f.err
	}
	if f.mutate != nil {
		f.mutate()
	}
	return f.result, nil
}

type formalPoolOperationsProxyFake struct {
	proxy      *Proxy
	testCalled bool
	testErr    error
}

func (f *formalPoolOperationsProxyFake) GetProxy(context.Context, int64) (*Proxy, error) {
	return f.proxy, nil
}

func (f *formalPoolOperationsProxyFake) TestProxy(context.Context, int64) (FormalPoolProxyTestSummary, error) {
	f.testCalled = true
	if f.testErr != nil {
		return FormalPoolProxyTestSummary{}, f.testErr
	}
	return FormalPoolProxyTestSummary{Success: true, ProxyRef: formalPoolSafeRef("proxy", "test")}, nil
}

func completeHealthcheckEvidenceExtra() map[string]any {
	return map[string]any{
		FormalPoolExtraOnboardingStage:             FormalPoolStageHealthcheckPassed,
		FormalPoolExtraHealthcheckStatus:           "passed",
		FormalPoolExtraHealthcheckStatusCodeBucket: "status_2xx",
		FormalPoolExtraHealthcheckRawRef:           "hmac-sha256:" + strings.Repeat("3", 64),
		FormalPoolExtraHealthcheckCCGatewaySeen:    true,
		FormalPoolExtraHealthcheckFallbackDetected: false,
		FormalPoolExtraHealthcheckProxyMismatch:    false,
		FormalPoolExtraHealthcheckRiskTextDetected: false,
		FormalPoolExtraRuntimeRegistered:           "true",
		FormalPoolExtraRuntimeRegisteredAt:         "2026-05-29T00:00:00Z",
	}
}

func TestFormalPoolRuntimeRegistrationReplayService_BackfillsMissingIdentityWithoutRegisteringDBID(t *testing.T) {
	t.Parallel()

	account := formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraOnboardingStage:     FormalPoolStageHealthcheckPassed,
		FormalPoolExtraRuntimeRegistered:   "false",
		FormalPoolExtraRuntimeRegisteredAt: "",
		"cc_gateway_account_ref":           "",
		"cc_gateway_egress_bucket":         "",
	})
	account.Status = StatusActive
	proxyID := int64(78)
	account.ProxyID = &proxyID
	store := &formalPoolRuntimeReplayStore{accounts: []*Account{account}}
	runtime := &formalPoolOperationsRuntimeFake{}
	proxy := &formalPoolOperationsProxyFake{proxy: &Proxy{ID: 78, Protocol: "socks5h", Host: "replay-proxy.local", Port: 1080, Status: StatusActive}}
	svc := NewFormalPoolRuntimeRegistrationReplayService(FormalPoolRuntimeRegistrationReplayDeps{Accounts: store, Proxy: proxy, CCGatewayRuntime: runtime})

	result, err := svc.Replay(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, result.Scanned)
	require.Equal(t, 1, result.Registered)
	require.True(t, runtime.called)
	require.NotEqual(t, "42", runtime.input.AccountRef)
	require.True(t, isSafeLedgerRef(runtime.input.AccountRef), runtime.input.AccountRef)
	require.Equal(t, runtime.input.AccountRef, account.GetExtraString("cc_gateway_account_ref"))
	require.NotEmpty(t, runtime.input.EgressBucket)
	require.Equal(t, runtime.input.EgressBucket, account.GetExtraString("cc_gateway_egress_bucket"))
}

func TestFormalPoolRuntimeRegistrationStartupReplay_RegistrarUnavailableFailClosesEligibleCandidates(t *testing.T) {
	t.Parallel()

	account := formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraOnboardingStage:     FormalPoolStageWarming,
		FormalPoolExtraRuntimeRegistered:   "true",
		FormalPoolExtraRuntimeRegisteredAt: "2026-05-29T00:00:00Z",
	})
	account.Status = StatusActive
	account.Schedulable = true
	store := &formalPoolRuntimeReplayStore{accounts: []*Account{account}}
	replay := NewFormalPoolRuntimeRegistrationReplayService(FormalPoolRuntimeRegistrationReplayDeps{Accounts: store, Proxy: &formalPoolOperationsProxyFake{}, CCGatewayRuntime: nil})
	runner := NewFormalPoolRuntimeRegistrationStartupReplay(replay)

	result := runner.Start(context.Background())
	require.Error(t, result.Error)
	require.Equal(t, 1, result.Scanned)
	require.Equal(t, 1, result.Failed)
	require.False(t, account.Schedulable)
	require.Equal(t, "false", account.GetExtraString(FormalPoolExtraRuntimeRegistered))
	require.Empty(t, account.GetExtraString(FormalPoolExtraRuntimeRegisteredAt))
	require.Equal(t, "runtime_replay_registrar_unavailable", account.GetExtraString(FormalPoolExtraLastFailureCode))
}

func TestFormalPoolRuntimeRegistrationReplayService_RebuildsRegistrationAndUpdatesState(t *testing.T) {
	t.Parallel()

	account := formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraOnboardingStage:     FormalPoolStageHealthcheckPassed,
		FormalPoolExtraRuntimeRegistered:   "false",
		FormalPoolExtraRuntimeRegisteredAt: "",
		"cc_gateway_account_ref":           "hmac-sha256:" + strings.Repeat("9", 64),
		"cc_gateway_egress_bucket":         "claude-replay-bucket",
	})
	account.Status = StatusActive
	proxyID := int64(77)
	account.ProxyID = &proxyID
	store := &formalPoolRuntimeReplayStore{accounts: []*Account{account}}
	runtime := &formalPoolOperationsRuntimeFake{}
	proxy := &formalPoolOperationsProxyFake{proxy: &Proxy{ID: 77, Protocol: "socks5h", Host: "proxy.local", Port: 1080, Status: StatusActive}}
	svc := NewFormalPoolRuntimeRegistrationReplayService(FormalPoolRuntimeRegistrationReplayDeps{Accounts: store, Proxy: proxy, CCGatewayRuntime: runtime, Now: func() time.Time { return time.Date(2026, 5, 30, 1, 2, 3, 0, time.UTC) }})

	result, err := svc.Replay(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, result.Scanned)
	require.Equal(t, 1, result.Registered)
	require.True(t, runtime.called)
	require.Equal(t, "hmac-sha256:"+strings.Repeat("9", 64), runtime.input.AccountRef)
	require.Equal(t, "claude-replay-bucket", runtime.input.EgressBucket)
	require.Equal(t, formalPoolSafeRef("proxy", "77"), runtime.input.ProxyRef)
	require.Equal(t, "socks5h://proxy.local:1080", runtime.input.ProxyURL)
	require.False(t, account.Schedulable)
	require.Equal(t, "true", account.GetExtraString(FormalPoolExtraRuntimeRegistered))
	require.Equal(t, "2026-05-30T01:02:03Z", account.GetExtraString(FormalPoolExtraRuntimeRegisteredAt))
}

type formalPoolRuntimeReplayStore struct {
	accounts []*Account
}

func (s *formalPoolRuntimeReplayStore) ListFormalPoolRuntimeReplayCandidates(context.Context) ([]*Account, error) {
	return s.accounts, nil
}

func (s *formalPoolRuntimeReplayStore) UpdateFormalPoolAccountState(_ context.Context, id int64, schedulable bool, status string, extra map[string]any) (*Account, error) {
	for _, account := range s.accounts {
		if account.ID != id {
			continue
		}
		account.Schedulable = schedulable
		if strings.TrimSpace(status) != "" {
			account.Status = status
		}
		if account.Extra == nil {
			account.Extra = map[string]any{}
		}
		for k, v := range extra {
			account.Extra[k] = v
		}
		return account, nil
	}
	return nil, ErrAccountNotFound
}

func TestFormalPoolRuntimeRegistrationStartupReplay_ReplaysMissingEgressBucketEvenWhenRuntimeFlagTrue(t *testing.T) {
	t.Parallel()

	account := formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraOnboardingStage:     FormalPoolStageWarming,
		FormalPoolExtraRuntimeRegistered:   "true",
		FormalPoolExtraRuntimeRegisteredAt: "2026-05-30T00:00:00Z",
		"cc_gateway_account_ref":           "hmac-sha256:" + strings.Repeat("8", 64),
		"cc_gateway_egress_bucket_enabled": "true",
		"cc_gateway_egress_bucket":         "",
	})
	account.Status = StatusActive
	account.Schedulable = true
	proxyID := int64(57)
	account.ProxyID = &proxyID
	store := &formalPoolRuntimeReplayStore{accounts: []*Account{account}}
	runtime := &formalPoolOperationsRuntimeFake{}
	proxy := &formalPoolOperationsProxyFake{proxy: &Proxy{ID: 57, Protocol: "http", Host: "startup-proxy.local", Port: 8080, Status: StatusActive}}
	replay := NewFormalPoolRuntimeRegistrationReplayService(FormalPoolRuntimeRegistrationReplayDeps{Accounts: store, Proxy: proxy, CCGatewayRuntime: runtime})

	result, err := replay.Replay(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, result.Scanned)
	require.Equal(t, 1, result.Registered)
	require.True(t, runtime.called)
	require.NotEmpty(t, runtime.input.EgressBucket)
	require.Equal(t, runtime.input.EgressBucket, account.GetExtraString("cc_gateway_egress_bucket"))
	require.False(t, account.Schedulable, "replay repairs missing runtime mapping fail-closed until healthcheck/warming is rerun")
}

func TestFormalPoolRuntimeRegistrationStartupReplay_RunsOnceAndRegistersCandidates(t *testing.T) {
	t.Parallel()

	account := formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraOnboardingStage:     FormalPoolStageHealthcheckPassed,
		FormalPoolExtraRuntimeRegistered:   "false",
		FormalPoolExtraRuntimeRegisteredAt: "",
		"cc_gateway_account_ref":           "hmac-sha256:" + strings.Repeat("7", 64),
		"cc_gateway_egress_bucket":         "claude-startup-bucket",
	})
	account.Status = StatusActive
	proxyID := int64(55)
	account.ProxyID = &proxyID
	store := &formalPoolRuntimeReplayStore{accounts: []*Account{account}}
	runtime := &formalPoolOperationsRuntimeFake{}
	proxy := &formalPoolOperationsProxyFake{proxy: &Proxy{ID: 55, Protocol: "socks5h", Host: "startup-proxy.local", Port: 1080, Status: StatusActive}}
	replay := NewFormalPoolRuntimeRegistrationReplayService(FormalPoolRuntimeRegistrationReplayDeps{Accounts: store, Proxy: proxy, CCGatewayRuntime: runtime, Now: func() time.Time { return time.Date(2026, 5, 30, 2, 3, 4, 0, time.UTC) }})
	runner := NewFormalPoolRuntimeRegistrationStartupReplay(replay)

	result := runner.Start(context.Background())
	require.NoError(t, result.Error)
	require.Equal(t, 1, result.Scanned)
	require.Equal(t, 1, result.Registered)
	require.True(t, runtime.called)
	require.Equal(t, "hmac-sha256:"+strings.Repeat("7", 64), runtime.input.AccountRef)
	require.Equal(t, "claude-startup-bucket", runtime.input.EgressBucket)
	require.Equal(t, "socks5h://startup-proxy.local:1080", runtime.input.ProxyURL)
	require.False(t, account.Schedulable)
	require.Equal(t, "true", account.GetExtraString(FormalPoolExtraRuntimeRegistered))
	require.Equal(t, "2026-05-30T02:03:04Z", account.GetExtraString(FormalPoolExtraRuntimeRegisteredAt))

	second := runner.Start(context.Background())
	require.True(t, second.Skipped)
}

func TestFormalPoolRuntimeRegistrationStartupReplay_RegisterFailureStaysUnschedulable(t *testing.T) {
	t.Parallel()

	account := formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraOnboardingStage:     FormalPoolStageHealthcheckPassed,
		FormalPoolExtraRuntimeRegistered:   "false",
		FormalPoolExtraRuntimeRegisteredAt: "",
		"cc_gateway_account_ref":           "hmac-sha256:" + strings.Repeat("6", 64),
		"cc_gateway_egress_bucket":         "claude-failure-bucket",
	})
	account.Status = StatusActive
	account.Schedulable = false
	proxyID := int64(56)
	account.ProxyID = &proxyID
	store := &formalPoolRuntimeReplayStore{accounts: []*Account{account}}
	runtime := &formalPoolOperationsRuntimeFake{err: errors.New("cc gateway unavailable")}
	proxy := &formalPoolOperationsProxyFake{proxy: &Proxy{ID: 56, Protocol: "socks5h", Host: "startup-proxy.local", Port: 1080, Status: StatusActive}}
	replay := NewFormalPoolRuntimeRegistrationReplayService(FormalPoolRuntimeRegistrationReplayDeps{Accounts: store, Proxy: proxy, CCGatewayRuntime: runtime})
	runner := NewFormalPoolRuntimeRegistrationStartupReplay(replay)

	result := runner.Start(context.Background())
	require.NoError(t, result.Error)
	require.Equal(t, 1, result.Scanned)
	require.Equal(t, 0, result.Registered)
	require.Equal(t, 1, result.Failed)
	require.True(t, runtime.called)
	require.False(t, account.Schedulable)
	require.Equal(t, "false", account.GetExtraString(FormalPoolExtraRuntimeRegistered))
	require.Empty(t, account.GetExtraString(FormalPoolExtraRuntimeRegisteredAt))
	require.Equal(t, "runtime_replay_registration_failed", account.GetExtraString(FormalPoolExtraLastFailureCode))
	require.NotEqual(t, FormalPoolStageWarming, account.GetExtraString(FormalPoolExtraOnboardingStage))
}

type formalPoolTokenInvalidatorFake struct {
	accounts []*Account
}

func (f *formalPoolTokenInvalidatorFake) InvalidateToken(_ context.Context, account *Account) error {
	f.accounts = append(f.accounts, account)
	return nil
}

type formalPoolSchedulerCacheFake struct {
	SchedulerCache
	setAccountCalls []*Account
}

func (f *formalPoolSchedulerCacheFake) SetAccount(_ context.Context, account *Account) error {
	f.setAccountCalls = append(f.setAccountCalls, account)
	return nil
}

type formalPoolOperationsAuditFake struct {
	events []FormalPoolOperationAuditEvent
	err    error
}

func (f *formalPoolOperationsAuditFake) WriteFormalPoolOperationAudit(_ context.Context, event FormalPoolOperationAuditEvent) error {
	f.events = append(f.events, event)
	return f.err
}

func actionKeys(actions []FormalPoolRecommendedAction) []string {
	keys := make([]string, 0, len(actions))
	for _, action := range actions {
		keys = append(keys, action.Key)
	}
	return keys
}
