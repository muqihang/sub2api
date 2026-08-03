//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/stretchr/testify/require"
)

func TestCRSSyncService_GeminiAPIKeyProtectsCredentialsOnCreate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/web/auth/login":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "token": "admin-token"})
		case "/admin/sync/export-accounts":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"exportedAt":              "2026-04-16T00:00:00Z",
					"claudeAccounts":          []any{},
					"claudeConsoleAccounts":   []any{},
					"openaiOAuthAccounts":     []any{},
					"openaiResponsesAccounts": []any{},
					"geminiOAuthAccounts":     []any{},
					"geminiApiKeyAccounts": []map[string]any{
						{
							"kind":        "gemini-apikey",
							"id":          "crs-gemini-apikey-1",
							"name":        "CRS Gemini APIKey",
							"platform":    PlatformGemini,
							"authType":    AccountTypeAPIKey,
							"isActive":    true,
							"schedulable": true,
							"priority":    50,
							"status":      StatusActive,
							"credentials": map[string]any{
								"api_key": "AIza-live-secret",
							},
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	repo := &crsSyncOpenAIRepoStub{}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("5a", 32)

	svc := NewCRSSyncService(repo, &crsSyncProxyRepoStub{}, nil, nil, nil, cfg)

	result, err := svc.SyncFromCRS(context.Background(), SyncFromCRSInput{
		BaseURL:     server.URL,
		Username:    "admin",
		Password:    "pass",
		SyncProxies: false,
	})

	require.NoError(t, err)
	require.Equal(t, 1, result.Created)
	require.Len(t, repo.created, 1)
	require.True(t, strings.HasPrefix(repo.created[0].Credentials["api_key"].(string), geminiSecretProtectorPrefix))
}

func TestCRSSyncService_GeminiOAuthProtectsCredentialsOnUpdate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/web/auth/login":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "token": "admin-token"})
		case "/admin/sync/export-accounts":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"exportedAt":              "2026-04-16T00:00:00Z",
					"claudeAccounts":          []any{},
					"claudeConsoleAccounts":   []any{},
					"openaiOAuthAccounts":     []any{},
					"openaiResponsesAccounts": []any{},
					"geminiApiKeyAccounts":    []any{},
					"geminiOAuthAccounts": []map[string]any{
						{
							"kind":        "gemini-oauth",
							"id":          "crs-gemini-oauth-1",
							"name":        "CRS Gemini OAuth",
							"platform":    PlatformGemini,
							"authType":    AccountTypeOAuth,
							"isActive":    true,
							"schedulable": true,
							"priority":    50,
							"status":      StatusActive,
							"credentials": map[string]any{
								"access_token":  "new-access",
								"refresh_token": "new-refresh",
								"oauth_type":    "ai_studio",
							},
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	repo := &crsSyncOpenAIRepoStub{
		byCRSID: map[string]*Account{
			"crs-gemini-oauth-1": {
				ID:       88,
				Platform: PlatformGemini,
				Type:     AccountTypeOAuth,
				Credentials: map[string]any{
					"access_token": "old-access",
					"oauth_type":   "ai_studio",
				},
			},
		},
	}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("5b", 32)

	svc := NewCRSSyncService(repo, &crsSyncProxyRepoStub{}, nil, nil, nil, cfg)

	result, err := svc.SyncFromCRS(context.Background(), SyncFromCRSInput{
		BaseURL:     server.URL,
		Username:    "admin",
		Password:    "pass",
		SyncProxies: false,
	})

	require.NoError(t, err)
	require.NotEmpty(t, result.Items)
	require.Equal(t, "updated", result.Items[0].Action, "item=%+v", result.Items[0])
	require.Len(t, repo.updated, 1)
	require.Equal(t, 1, result.Updated)
	require.True(t, strings.HasPrefix(repo.updated[0].Credentials["access_token"].(string), geminiSecretProtectorPrefix))
	require.True(t, strings.HasPrefix(repo.updated[0].Credentials["refresh_token"].(string), geminiSecretProtectorPrefix))
}

func TestCRSSyncService_GeminiOAuthUpdateReprotectsLegacyPlaintextSecretsBeforeRefresh(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/web/auth/login":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "token": "admin-token"})
		case "/admin/sync/export-accounts":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"exportedAt":              "2026-04-16T00:00:00Z",
					"claudeAccounts":          []any{},
					"claudeConsoleAccounts":   []any{},
					"openaiOAuthAccounts":     []any{},
					"openaiResponsesAccounts": []any{},
					"geminiApiKeyAccounts":    []any{},
					"geminiOAuthAccounts": []map[string]any{
						{
							"kind":        "gemini-oauth",
							"id":          "crs-gemini-oauth-2",
							"name":        "CRS Gemini OAuth Partial",
							"platform":    PlatformGemini,
							"authType":    AccountTypeOAuth,
							"isActive":    true,
							"schedulable": true,
							"priority":    50,
							"status":      StatusActive,
							"credentials": map[string]any{
								"refresh_token": "new-refresh",
								"oauth_type":    "ai_studio",
							},
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	repo := &crsSyncOpenAIRepoStub{
		byCRSID: map[string]*Account{
			"crs-gemini-oauth-2": {
				ID:       89,
				Platform: PlatformGemini,
				Type:     AccountTypeOAuth,
				Credentials: map[string]any{
					"access_token":  "legacy-plain-access",
					"refresh_token": "old-refresh",
					"oauth_type":    "ai_studio",
				},
			},
		},
	}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("5e", 32)

	svc := NewCRSSyncService(repo, &crsSyncProxyRepoStub{}, nil, nil, nil, cfg)

	result, err := svc.SyncFromCRS(context.Background(), SyncFromCRSInput{
		BaseURL:     server.URL,
		Username:    "admin",
		Password:    "pass",
		SyncProxies: false,
	})

	require.NoError(t, err)
	require.Len(t, repo.updated, 1)
	require.Equal(t, 1, result.Updated)
	require.True(t, strings.HasPrefix(repo.updated[0].Credentials["access_token"].(string), geminiSecretProtectorPrefix))
	require.True(t, strings.HasPrefix(repo.updated[0].Credentials["refresh_token"].(string), geminiSecretProtectorPrefix))
}

func TestCRSSyncService_RefreshOAuthTokenProtectsGeminiSecrets(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("5c", 32)
	protector, err := ProvideGeminiSecretProtector(cfg)
	require.NoError(t, err)

	protected, err := protector.ProtectCredentials(map[string]any{
		"access_token":  "old-access",
		"refresh_token": "old-refresh",
		"oauth_type":    "ai_studio",
	})
	require.NoError(t, err)

	client := &mockGeminiOAuthClient{
		refreshTokenFunc: func(ctx context.Context, oauthType, refreshToken, proxyURL string) (*geminicli.TokenResponse, error) {
			return &geminicli.TokenResponse{
				AccessToken:  "new-access",
				RefreshToken: "new-refresh",
				ExpiresIn:    3600,
				TokenType:    "Bearer",
				Scope:        "openid",
			}, nil
		},
	}
	svc := NewCRSSyncService(
		nil,
		nil,
		nil,
		nil,
		NewGeminiOAuthService(&mockGeminiProxyRepo{}, client, nil, nil, cfg),
		cfg,
	)
	account := &Account{
		ID:          99,
		Platform:    PlatformGemini,
		Type:        AccountTypeOAuth,
		Credentials: protected,
	}

	refreshed := svc.refreshOAuthToken(context.Background(), account)
	require.NotNil(t, refreshed)
	require.True(t, strings.HasPrefix(refreshed["access_token"].(string), geminiSecretProtectorPrefix))
	require.True(t, strings.HasPrefix(refreshed["refresh_token"].(string), geminiSecretProtectorPrefix))
}
