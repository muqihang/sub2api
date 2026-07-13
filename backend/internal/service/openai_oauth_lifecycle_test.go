//go:build unit

package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

type openaiLifecycleClientStub struct {
	refreshResp  *openai.TokenResponse
	refreshErr   error
	lastProxyURL string
	refreshCalls int
}

func (s *openaiLifecycleClientStub) ExchangeCode(ctx context.Context, code, codeVerifier, redirectURI, proxyURL, clientID string) (*openai.TokenResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *openaiLifecycleClientStub) RefreshToken(ctx context.Context, refreshToken, proxyURL string) (*openai.TokenResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *openaiLifecycleClientStub) RefreshTokenWithClientID(ctx context.Context, refreshToken, proxyURL string, clientID string) (*openai.TokenResponse, error) {
	s.refreshCalls++
	s.lastProxyURL = proxyURL
	if s.refreshErr != nil {
		return nil, s.refreshErr
	}
	return s.refreshResp, nil
}

func TestEvaluateOpenAIImportLifecycle_RTValidated(t *testing.T) {
	svc := NewOpenAIOAuthService(nil, &openaiLifecycleClientStub{
		refreshResp: &openai.TokenResponse{
			AccessToken:  "new-at",
			RefreshToken: "new-rt",
			ExpiresIn:    3600,
			Scope:        "openid email profile api.responses.write",
		},
	})

	decision, err := EvaluateOpenAIImportLifecycle(context.Background(), svc, "", map[string]any{
		"access_token":  "old-at",
		"refresh_token": "old-rt",
		"client_id":     "client-1",
		"id_token":      "id-token",
	})

	require.NoError(t, err)
	require.Equal(t, OpenAIPoolRoleMain, decision.PoolRole)
	require.Equal(t, OpenAIAuthStateHealthy, decision.AuthState)
	require.Equal(t, OpenAITokenSourceRTManaged, decision.TokenSource)
	require.Equal(t, OpenAIValidationOutcomeRTValidated, decision.ValidationOutcome)
	require.Equal(t, StatusActive, decision.Status)
	require.True(t, decision.Schedulable)
	require.Equal(t, "new-at", decision.Credentials["access_token"])
	require.Equal(t, "new-rt", decision.Credentials["refresh_token"])
	require.Equal(t, "client-1", decision.Credentials["client_id"])
	require.NotEmpty(t, decision.Extra["openai_last_validated_at"])
	require.Equal(t, true, decision.Extra["openai_responses_write_capable"])
	require.Equal(t, "openid email profile api.responses.write", decision.Extra["openai_last_granted_scope"])
}

func TestEvaluateOpenAIImportLifecycle_RTValidatedProtectsOriginalRefreshTokenWhenRefreshResponseOmitsIt(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("ad", 32)
	svc := NewOpenAIOAuthService(nil, &openaiLifecycleClientStub{
		refreshResp: &openai.TokenResponse{
			AccessToken: "new-at",
			ExpiresIn:   3600,
			Scope:       "openid email profile api.responses.write",
		},
	})
	svc.SetGatewayCoreService(NewOpenAIGatewayCoreService(nil, cfg, nil))

	decision, err := EvaluateOpenAIImportLifecycle(context.Background(), svc, "", map[string]any{
		"access_token":  "old-at",
		"refresh_token": "old-rt",
		"client_id":     "client-1",
		"id_token":      "old-id",
	})

	require.NoError(t, err)
	require.True(t, strings.HasPrefix(decision.Credentials["access_token"].(string), openAISecretProtectorPrefix))
	require.True(t, strings.HasPrefix(decision.Credentials["refresh_token"].(string), openAISecretProtectorPrefix))
	require.True(t, strings.HasPrefix(decision.Credentials["id_token"].(string), openAISecretProtectorPrefix))
}

func TestEvaluateOpenAIImportLifecycle_UsesEgressBucketAndPersistsIt(t *testing.T) {
	client := &openaiLifecycleClientStub{
		refreshResp: &openai.TokenResponse{
			AccessToken:  "new-at",
			RefreshToken: "new-rt",
			ExpiresIn:    3600,
			Scope:        "openid email profile api.responses.write",
		},
	}
	svc := NewOpenAIOAuthService(nil, client)
	defer svc.Stop()
	cfg := testOpenAIOAuthEgressConfig()
	svc.SetGatewayCoreService(NewOpenAIGatewayCoreService(nil, cfg, nil))

	decision, err := EvaluateOpenAIImportLifecycle(context.Background(), svc, "", map[string]any{
		"refresh_token":                "old-rt",
		"client_id":                    "client-1",
		"openai_gateway_egress_bucket": "bucket-a",
	})

	require.NoError(t, err)
	require.Equal(t, StatusActive, decision.Status)
	require.Equal(t, "socks5h://127.0.0.1:9001", MaskOpenAIProxyURL(client.lastProxyURL))
	require.Equal(t, "bucket-a", decision.Extra["openai_gateway_egress_bucket"])
}

func TestEvaluateOpenAIImportLifecycle_RejectsMissingEgressBucketBeforeRefresh(t *testing.T) {
	client := &openaiLifecycleClientStub{
		refreshResp: &openai.TokenResponse{
			AccessToken:  "new-at",
			RefreshToken: "new-rt",
			ExpiresIn:    3600,
		},
	}
	svc := NewOpenAIOAuthService(nil, client)
	defer svc.Stop()
	cfg := testOpenAIOAuthEgressConfig()
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "missing"
	svc.SetGatewayCoreService(NewOpenAIGatewayCoreService(nil, cfg, nil))

	decision, err := EvaluateOpenAIImportLifecycle(context.Background(), svc, "", map[string]any{
		"refresh_token": "old-rt",
		"client_id":     "client-1",
	})

	require.NoError(t, err)
	require.NotNil(t, decision)
	require.Equal(t, OpenAIPoolRoleQuarantine, decision.PoolRole)
	require.Equal(t, OpenAIValidationOutcomeRTValidationRetryableFailure, decision.ValidationOutcome)
	require.Equal(t, "missing_bucket", decision.RefreshErrorCode)
	require.Empty(t, client.lastProxyURL)
	require.Zero(t, client.refreshCalls)
}

func TestEvaluateOpenAIImportLifecycleWithExtra_RejectsExtraEgressBucketBeforeRefresh(t *testing.T) {
	client := &openaiLifecycleClientStub{
		refreshResp: &openai.TokenResponse{
			AccessToken:  "new-at",
			RefreshToken: "new-rt",
			ExpiresIn:    3600,
		},
	}
	svc := NewOpenAIOAuthService(nil, client)
	defer svc.Stop()
	cfg := testOpenAIOAuthEgressConfig()
	svc.SetGatewayCoreService(NewOpenAIGatewayCoreService(nil, cfg, nil))

	decision, err := EvaluateOpenAIImportLifecycleWithExtra(context.Background(), svc, "", map[string]any{
		"refresh_token": "old-rt",
		"client_id":     "client-1",
	}, map[string]any{
		"openai_gateway_egress_bucket": "missing",
	})

	require.NoError(t, err)
	require.NotNil(t, decision)
	require.Equal(t, OpenAIPoolRoleQuarantine, decision.PoolRole)
	require.Equal(t, OpenAIValidationOutcomeRTValidationRetryableFailure, decision.ValidationOutcome)
	require.Equal(t, "missing_bucket", decision.RefreshErrorCode)
	require.Empty(t, client.lastProxyURL)
	require.Zero(t, client.refreshCalls)
}

func TestEvaluateOpenAIImportLifecycle_ScopeInsufficientQuarantined(t *testing.T) {
	svc := NewOpenAIOAuthService(nil, &openaiLifecycleClientStub{
		refreshResp: &openai.TokenResponse{
			AccessToken:  "new-at",
			RefreshToken: "new-rt",
			ExpiresIn:    3600,
			Scope:        "openid email profile model.request model.read",
		},
	})

	decision, err := EvaluateOpenAIImportLifecycle(context.Background(), svc, "", map[string]any{
		"access_token":  "old-at",
		"refresh_token": "old-rt",
		"client_id":     "client-1",
	})

	require.NoError(t, err)
	require.Equal(t, OpenAIPoolRoleQuarantine, decision.PoolRole)
	require.Equal(t, OpenAIAuthStateTerminal, decision.AuthState)
	require.Equal(t, OpenAIValidationOutcomeRTValidationScopeInsufficient, decision.ValidationOutcome)
	require.Equal(t, StatusDisabled, decision.Status)
	require.False(t, decision.Schedulable)
	require.Equal(t, openAIAuthErrorCodeResponsesWriteMissing, decision.RefreshErrorCode)
	require.Equal(t, false, decision.Extra["openai_responses_write_capable"])
	require.Equal(t, "openid email profile model.request model.read", decision.Extra["openai_last_granted_scope"])
}

func TestEvaluateOpenAIImportLifecycle_ATOnlyQuarantine(t *testing.T) {
	decision, err := EvaluateOpenAIImportLifecycle(context.Background(), nil, "", map[string]any{
		"access_token": "at-only",
		"expires_at":   time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339),
	})

	require.NoError(t, err)
	require.Equal(t, OpenAIPoolRoleQuarantine, decision.PoolRole)
	require.Equal(t, OpenAIAuthStateATOnly, decision.AuthState)
	require.Equal(t, OpenAITokenSourceATOnly, decision.TokenSource)
	require.Equal(t, OpenAIValidationOutcomeATOnlyQuarantined, decision.ValidationOutcome)
	require.Equal(t, StatusDisabled, decision.Status)
	require.False(t, decision.Schedulable)
	require.Equal(t, "at-only", decision.Credentials["access_token"])
}

func TestEvaluateOpenAIImportLifecycle_ATOnlyWithUsableLifetimeIsSchedulable(t *testing.T) {
	decision, err := EvaluateOpenAIImportLifecycle(context.Background(), nil, "", map[string]any{
		"access_token": "at-only",
		"expires_at":   time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
	})

	require.NoError(t, err)
	require.Equal(t, OpenAIPoolRoleMain, decision.PoolRole)
	require.Equal(t, OpenAIAuthStateATOnly, decision.AuthState)
	require.Equal(t, OpenAITokenSourceATOnly, decision.TokenSource)
	require.Equal(t, OpenAIValidationOutcomeATOnlyAccepted, decision.ValidationOutcome)
	require.Equal(t, StatusActive, decision.Status)
	require.True(t, decision.Schedulable)
	require.Equal(t, "at-only", decision.Credentials["access_token"])
	require.Empty(t, decision.Extra["openai_last_validated_at"])
}

func TestEvaluateOpenAIImportLifecycle_RetryableValidationFailure(t *testing.T) {
	svc := NewOpenAIOAuthService(nil, &openaiLifecycleClientStub{
		refreshErr: errors.New("request failed: dial tcp timeout"),
	})

	decision, err := EvaluateOpenAIImportLifecycle(context.Background(), svc, "", map[string]any{
		"access_token":  "old-at",
		"refresh_token": "old-rt",
	})

	require.NoError(t, err)
	require.Equal(t, OpenAIPoolRoleQuarantine, decision.PoolRole)
	require.Equal(t, OpenAIAuthStateCooling, decision.AuthState)
	require.Equal(t, OpenAITokenSourceRTManaged, decision.TokenSource)
	require.Equal(t, OpenAIValidationOutcomeRTValidationRetryableFailure, decision.ValidationOutcome)
	require.Equal(t, StatusDisabled, decision.Status)
	require.False(t, decision.Schedulable)
	require.Equal(t, "oauth_refresh_failed", decision.RefreshErrorCode)
}

func TestEvaluateOpenAIImportLifecycle_TerminalValidationFailure(t *testing.T) {
	svc := NewOpenAIOAuthService(nil, &openaiLifecycleClientStub{
		refreshErr: errors.New("token refresh failed: status 400, body: {\"error\":\"refresh_token_expired\"}"),
	})

	decision, err := EvaluateOpenAIImportLifecycle(context.Background(), svc, "", map[string]any{
		"access_token":  "old-at",
		"refresh_token": "old-rt",
	})

	require.NoError(t, err)
	require.Equal(t, OpenAIPoolRoleQuarantine, decision.PoolRole)
	require.Equal(t, OpenAIAuthStateTerminal, decision.AuthState)
	require.Equal(t, OpenAITokenSourceRTManaged, decision.TokenSource)
	require.Equal(t, OpenAIValidationOutcomeRTValidationTerminalFailure, decision.ValidationOutcome)
	require.Equal(t, StatusError, decision.Status)
	require.False(t, decision.Schedulable)
	require.Equal(t, "refresh_token_expired", decision.RefreshErrorCode)
}

func TestAccount_OpenAIManagedRefreshEligibility(t *testing.T) {
	main := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Status:   StatusActive,
		Extra: map[string]any{
			"openai_pool_role":    OpenAIPoolRoleMain,
			"openai_auth_state":   OpenAIAuthStateHealthy,
			"openai_token_source": OpenAITokenSourceRTManaged,
		},
		Credentials: map[string]any{"refresh_token": "rt"},
	}
	require.True(t, main.ShouldParticipateInOpenAIManagedRefresh())

	quarantine := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Status:   StatusDisabled,
		Extra: map[string]any{
			"openai_pool_role":    OpenAIPoolRoleQuarantine,
			"openai_auth_state":   OpenAIAuthStateCooling,
			"openai_token_source": OpenAITokenSourceRTManaged,
		},
		Credentials: map[string]any{"refresh_token": "rt"},
	}
	require.True(t, quarantine.ShouldParticipateInOpenAIManagedRefresh())

	atOnly := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Status:   StatusDisabled,
		Extra: map[string]any{
			"openai_pool_role":    OpenAIPoolRoleQuarantine,
			"openai_auth_state":   OpenAIAuthStateATOnly,
			"openai_token_source": OpenAITokenSourceATOnly,
		},
		Credentials: map[string]any{"access_token": "at"},
	}
	require.False(t, atOnly.ShouldParticipateInOpenAIManagedRefresh())
}

func TestFindMatchingOpenAIOAuthAccount_PrefersRefreshToken(t *testing.T) {
	accounts := []Account{
		{
			ID:       1,
			Platform: PlatformOpenAI,
			Type:     AccountTypeOAuth,
			Credentials: map[string]any{
				"refresh_token":      "rt-1",
				"chatgpt_account_id": "acct-1",
			},
		},
		{
			ID:       2,
			Platform: PlatformOpenAI,
			Type:     AccountTypeOAuth,
			Credentials: map[string]any{
				"refresh_token":      "rt-2",
				"chatgpt_account_id": "acct-1",
			},
		},
	}

	account, matchKey := FindMatchingOpenAIOAuthAccount(accounts, map[string]any{
		"refresh_token":      "rt-2",
		"chatgpt_account_id": "acct-1",
	})

	require.NotNil(t, account)
	require.Equal(t, int64(2), account.ID)
	require.Equal(t, "refresh_token", matchKey)
}

func TestFindMatchingOpenAIOAuthAccountWithAccessor_MatchesEncryptedStoredToken(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("99", 32)
	protector, err := ProvideOpenAISecretProtector(cfg)
	require.NoError(t, err)
	encrypted, err := protector.ProtectCredentials(map[string]any{
		"refresh_token": "rt-2",
	})
	require.NoError(t, err)

	accounts := []Account{
		{
			ID:       1,
			Platform: PlatformOpenAI,
			Type:     AccountTypeOAuth,
			Credentials: map[string]any{
				"refresh_token": "rt-1",
			},
		},
		{
			ID:          2,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Credentials: encrypted,
		},
	}

	account, matchKey := FindMatchingOpenAIOAuthAccountWithAccessor(accounts, map[string]any{
		"refresh_token": "rt-2",
	}, NewOpenAIGatewayCredentials(cfg, protector))

	require.NotNil(t, account)
	require.Equal(t, int64(2), account.ID)
	require.Equal(t, "refresh_token", matchKey)
}

func TestShouldOverwriteMatchedOpenAIAccount(t *testing.T) {
	existing := &Account{
		ID:       1,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Status:   StatusActive,
		Extra: map[string]any{
			"openai_pool_role":    OpenAIPoolRoleMain,
			"openai_auth_state":   OpenAIAuthStateHealthy,
			"openai_token_source": OpenAITokenSourceRTManaged,
		},
		Credentials: map[string]any{
			"refresh_token":      "new-rt",
			"chatgpt_account_id": "acct-1",
		},
	}

	validated := &OpenAIImportLifecycleDecision{
		TokenSource:       OpenAITokenSourceRTManaged,
		ValidationOutcome: OpenAIValidationOutcomeRTValidated,
		Credentials: map[string]any{
			"refresh_token":      "rotated-rt",
			"chatgpt_account_id": "acct-1",
		},
	}
	require.True(t, ShouldOverwriteMatchedOpenAIAccount(existing, "chatgpt_account_id", validated))

	retryableOld := &OpenAIImportLifecycleDecision{
		TokenSource:       OpenAITokenSourceRTManaged,
		ValidationOutcome: OpenAIValidationOutcomeRTValidationRetryableFailure,
		Credentials: map[string]any{
			"refresh_token":      "old-rt",
			"chatgpt_account_id": "acct-1",
		},
	}
	require.False(t, ShouldOverwriteMatchedOpenAIAccount(existing, "chatgpt_account_id", retryableOld))

	atOnly := &OpenAIImportLifecycleDecision{
		TokenSource:       OpenAITokenSourceATOnly,
		ValidationOutcome: OpenAIValidationOutcomeATOnlyQuarantined,
		Credentials: map[string]any{
			"access_token":       "at-only",
			"chatgpt_account_id": "acct-1",
		},
	}
	require.False(t, ShouldOverwriteMatchedOpenAIAccount(existing, "chatgpt_account_id", atOnly))
}

func TestEvaluateOpenAIImportLifecycle_TokenInvalidatedValidationFailureIsTerminal(t *testing.T) {
	svc := NewOpenAIOAuthService(nil, &openaiLifecycleClientStub{
		refreshErr: errors.New("token refresh failed: status 401, body: {\"error\":{\"code\":\"token_invalidated\"}}"),
	})

	decision, err := EvaluateOpenAIImportLifecycle(context.Background(), svc, "", map[string]any{
		"access_token":  "old-at",
		"refresh_token": "old-rt",
	})

	require.NoError(t, err)
	require.Equal(t, OpenAIAuthStateTerminal, decision.AuthState)
	require.Equal(t, OpenAIValidationOutcomeRTValidationTerminalFailure, decision.ValidationOutcome)
	require.Equal(t, StatusError, decision.Status)
	require.False(t, decision.Schedulable)
	require.Equal(t, openAIAuthErrorCodeTokenInvalidated, decision.RefreshErrorCode)
}
