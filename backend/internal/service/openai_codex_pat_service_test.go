package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func withOpenAICodexPATWhoamiURLForTest(t *testing.T, url string) {
	t.Helper()
	originalURL := openAICodexPATWhoamiURL
	openAICodexPATWhoamiURL = url
	t.Cleanup(func() { openAICodexPATWhoamiURL = originalURL })
}

func TestOpenAIOAuthService_ValidateCodexPersonalAccessToken(t *testing.T) {
	var gotAuthorization string
	var gotOriginator string
	var gotUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("authorization")
		gotOriginator = r.Header.Get("originator")
		gotUserAgent = r.Header.Get("user-agent")
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"email":"user@example.com",
			"chatgpt_user_id":"user-123",
			"chatgpt_account_id":"acct-123",
			"chatgpt_plan_type":"plus",
			"chatgpt_account_is_fedramp":true
		}`))
	}))
	defer server.Close()
	withOpenAICodexPATWhoamiURLForTest(t, server.URL)

	svc := NewOpenAIOAuthService(nil, nil)
	defer svc.Stop()

	info, err := svc.ValidateCodexPersonalAccessToken(context.Background(), " at-test-token ", "")
	require.NoError(t, err)
	require.Equal(t, "Bearer at-test-token", gotAuthorization)
	require.Equal(t, "codex_cli_rs", gotOriginator)
	require.Equal(t, codexCLIUserAgent, gotUserAgent)
	require.Equal(t, OpenAIAuthModePersonalAccessToken, info.AuthMode)
	require.Equal(t, "user@example.com", info.Email)
	require.Equal(t, "user-123", info.ChatGPTUserID)
	require.Equal(t, "acct-123", info.ChatGPTAccountID)
	require.Equal(t, "plus", info.PlanType)
	require.True(t, info.ChatGPTAccountFedRAMP)
	require.Zero(t, info.ExpiresAt)
	require.Empty(t, info.RefreshToken)
}

func TestOpenAIOAuthService_ValidateCodexPersonalAccessTokenRequiresATPrefix(t *testing.T) {
	svc := NewOpenAIOAuthService(nil, nil)
	defer svc.Stop()

	_, err := svc.ValidateCodexPersonalAccessToken(context.Background(), "eyJ.jwt", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "at-")
}

func TestOpenAIOAuthService_ValidateCodexPersonalAccessTokenDoesNotLeakNon2xxBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("raw response body with token=secret-token and account_id=acct-raw"))
	}))
	defer server.Close()
	withOpenAICodexPATWhoamiURLForTest(t, server.URL)

	svc := NewOpenAIOAuthService(nil, nil)
	defer svc.Stop()

	_, err := svc.ValidateCodexPersonalAccessToken(context.Background(), "at-test-token", "")
	require.Error(t, err)
	require.NotContains(t, strings.ToLower(err.Error()), "secret-token")
	require.NotContains(t, strings.ToLower(err.Error()), "acct-raw")
	require.Contains(t, err.Error(), "502")
}

func TestOpenAIOAuthService_BuildAccountCredentialsForPAT(t *testing.T) {
	svc := NewOpenAIOAuthService(nil, nil)
	defer svc.Stop()

	credentials, err := svc.BuildAccountCredentials(&OpenAITokenInfo{
		AccessToken:           "at-test-token",
		AuthMode:              OpenAIAuthModePersonalAccessToken,
		Email:                 "user@example.com",
		ChatGPTAccountID:      "acct-123",
		ChatGPTUserID:         "user-123",
		ChatGPTAccountFedRAMP: true,
		PlanType:              "plus",
	})
	require.NoError(t, err)

	require.Equal(t, "at-test-token", credentials["access_token"])
	require.Equal(t, OpenAIAuthModePersonalAccessToken, credentials["auth_mode"])
	require.Equal(t, "personal_access_token", credentials["openai_auth_mode"])
	require.Equal(t, "Bearer", credentials["token_type"])
	require.Equal(t, true, credentials["chatgpt_account_is_fedramp"])
	require.NotContains(t, credentials, "expires_at")
	require.NotContains(t, credentials, "refresh_token")
	require.NotContains(t, credentials, "id_token")
	require.NotContains(t, credentials, "client_id")
}

func TestNormalizeOpenAIPersonalAccessTokenCredentialsRemovesOAuthFields(t *testing.T) {
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"auth_mode": "personal_access_token",
		},
	}
	credentials := map[string]any{
		"access_token":                "at-test-token",
		"refresh_token":               "stale-refresh-token",
		"id_token":                    "stale-id-token",
		"expires_at":                  "2026-01-01T00:00:00Z",
		"expires_in":                  3600,
		"client_id":                   "stale-client",
		"model_mapping":               map[string]any{"gpt-5": "gpt-5-codex"},
		"chatgpt_account_is_fedramp":  true,
		"subscription_expires_at":     "2026-12-31T00:00:00Z",
		"openai_usage_channel_fields": []any{"custom"},
	}

	got := NormalizeOpenAIPersonalAccessTokenCredentials(account, nil, credentials)

	require.Equal(t, "at-test-token", got["access_token"])
	require.Equal(t, OpenAIAuthModePersonalAccessToken, got["auth_mode"])
	require.Equal(t, "personal_access_token", got["openai_auth_mode"])
	require.Equal(t, "Bearer", got["token_type"])
	require.NotContains(t, got, "refresh_token")
	require.NotContains(t, got, "id_token")
	require.NotContains(t, got, "expires_at")
	require.NotContains(t, got, "expires_in")
	require.NotContains(t, got, "client_id")
	require.Equal(t, map[string]any{"gpt-5": "gpt-5-codex"}, got["model_mapping"])
	require.Equal(t, true, got["chatgpt_account_is_fedramp"])
	require.Equal(t, "2026-12-31T00:00:00Z", got["subscription_expires_at"])
	require.Equal(t, []any{"custom"}, got["openai_usage_channel_fields"])
}

func TestOpenAIOAuthService_RefreshAccountToken_PATIgnoresStaleRefreshToken(t *testing.T) {
	client := &openaiOAuthClientRefreshStub{}
	var whoamiCalls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&whoamiCalls, 1)
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"email":"user@example.com",
			"chatgpt_user_id":"user-123",
			"chatgpt_account_id":"acct-123",
			"chatgpt_plan_type":"plus",
			"chatgpt_account_is_fedramp":false
		}`))
	}))
	defer server.Close()
	withOpenAICodexPATWhoamiURLForTest(t, server.URL)

	svc := NewOpenAIOAuthService(nil, client)
	defer svc.Stop()

	account := &Account{
		ID:       77,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":  "at-test-token",
			"refresh_token": "stale-refresh-token",
			"expires_at":    time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
			"auth_mode":     "personal_access_token",
		},
	}

	info, err := svc.RefreshAccountToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, OpenAIAuthModePersonalAccessToken, info.AuthMode)
	require.Equal(t, "at-test-token", info.AccessToken)
	require.Empty(t, info.RefreshToken)
	require.Equal(t, int32(1), atomic.LoadInt32(&whoamiCalls))
	require.Zero(t, atomic.LoadInt32(&client.refreshCalls), "PAT accounts must not call OAuth refresh even if stale refresh_token remains")
}

func TestOpenAITokenRefresher_NeedsRefresh_SkipsPersonalAccessToken(t *testing.T) {
	refresher := NewOpenAITokenRefresher(nil, nil)
	expiresAt := time.Now().Add(time.Minute).UTC().Format(time.RFC3339)
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":  "at-test-token",
			"refresh_token": "stale-refresh-token",
			"expires_at":    expiresAt,
			"auth_mode":     OpenAIAuthModePersonalAccessToken,
		},
	}

	require.False(t, refresher.NeedsRefresh(account, 5*time.Minute))
}

func TestOpenAITokenRefresher_Refresh_PATRemovesStaleOAuthFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"email":"user@example.com",
			"chatgpt_user_id":"user-123",
			"chatgpt_account_id":"acct-123",
			"chatgpt_plan_type":"plus",
			"chatgpt_account_is_fedramp":true
		}`))
	}))
	defer server.Close()
	withOpenAICodexPATWhoamiURLForTest(t, server.URL)

	svc := NewOpenAIOAuthService(nil, nil)
	defer svc.Stop()
	refresher := NewOpenAITokenRefresher(svc, nil)

	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":  "at-test-token",
			"refresh_token": "stale-refresh-token",
			"id_token":      "stale-id-token",
			"expires_at":    time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
			"expires_in":    3600,
			"client_id":     "stale-client",
			"auth_mode":     OpenAIAuthModePersonalAccessToken,
			"model_mapping": map[string]any{"gpt-5": "gpt-5-codex"},
		},
	}

	credentials, err := refresher.Refresh(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "at-test-token", credentials["access_token"])
	require.Equal(t, OpenAIAuthModePersonalAccessToken, credentials["auth_mode"])
	require.Equal(t, "personal_access_token", credentials["openai_auth_mode"])
	require.NotContains(t, credentials, "refresh_token")
	require.NotContains(t, credentials, "id_token")
	require.NotContains(t, credentials, "expires_at")
	require.NotContains(t, credentials, "expires_in")
	require.NotContains(t, credentials, "client_id")
	require.Equal(t, map[string]any{"gpt-5": "gpt-5-codex"}, credentials["model_mapping"])
}

func TestSetOpenAIChatGPTAccountHeadersAddsFedRAMP(t *testing.T) {
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"chatgpt_account_id":         "acct-123",
			"chatgpt_account_is_fedramp": true,
		},
	}
	headers := http.Header{}

	setOpenAIChatGPTAccountHeaders(headers, account)

	require.Equal(t, "acct-123", headers.Get("chatgpt-account-id"))
	require.Equal(t, "true", headers.Get("x-openai-fedramp"))

	account.Credentials["chatgpt_account_is_fedramp"] = false
	setOpenAIChatGPTAccountHeaders(headers, account)
	require.Empty(t, headers.Get("x-openai-fedramp"))
}
