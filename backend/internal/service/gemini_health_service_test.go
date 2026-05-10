//go:build unit

package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type geminiHealthRepoStub struct {
	mockAccountRepoForGemini
}

func (r *geminiHealthRepoStub) ListByPlatform(ctx context.Context, platform string) ([]Account, error) {
	if platform != PlatformGemini {
		return nil, nil
	}
	return append([]Account(nil), r.accounts...), nil
}

func TestGeminiHealthService_BuildHealthSnapshotCountsFamiliesAndWarnings(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gemini.ProductionMode = true
	cfg.Gemini.TokenCacheMode = "encrypted"
	cfg.Gemini.RequireSafeOAuthSessionStore = true
	cfg.Gemini.RequireThoughtSignatureSessionSafety = true

	repo := &geminiHealthRepoStub{
		mockAccountRepoForGemini: mockAccountRepoForGemini{
			accounts: []Account{
				{
					ID:       1,
					Name:     "Code Assist",
					Platform: PlatformGemini,
					Type:     AccountTypeOAuth,
					Credentials: map[string]any{
						"oauth_type": "code_assist",
						"project_id": "proj-1",
					},
					Extra: map[string]any{
						geminiTokenCacheStateKey: geminiTokenCacheStateDegraded,
					},
				},
				{
					ID:       2,
					Name:     "Google One",
					Platform: PlatformGemini,
					Type:     AccountTypeOAuth,
					Credentials: map[string]any{
						"oauth_type":         "google_one",
						"tier_id":            GeminiTierGoogleOneFree,
						geminiOAuthReasonKey: geminiOAuthReasonGoogleOneDefaultTierFallback,
					},
				},
				{
					ID:       3,
					Name:     "Vertex",
					Platform: PlatformGemini,
					Type:     AccountTypeServiceAccount,
					Credentials: map[string]any{
						"service_account_json": `{"type":"service_account","project_id":"vertex-proj","private_key_id":"kid","private_key":"-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----\n","client_email":"svc@vertex-proj.iam.gserviceaccount.com"}`,
					},
				},
			},
		},
	}

	svc := NewGeminiHealthService(repo, NewGeminiOAuthService(nil, nil, nil, nil, cfg), cfg)
	snapshot, err := svc.BuildHealthSnapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(3), snapshot.GeminiAccounts)
	require.Equal(t, int64(1), snapshot.AccountsByFamily[string(GeminiAccountFamilyCodeAssist)])
	require.Equal(t, int64(1), snapshot.AccountsByFamily[string(GeminiAccountFamilyGoogleOne)])
	require.Equal(t, int64(1), snapshot.AccountsByFamily[string(GeminiAccountFamilyVertexServiceAccount)])
	require.Equal(t, geminiHealthStatusDegraded, snapshot.GatewayStatus)
	require.Contains(t, snapshot.WarningCodes, geminiWarningTokenCachePlaintextDetected)
	require.Contains(t, snapshot.WarningCodes, geminiWarningGoogleOneDefaultTier)
	require.Contains(t, snapshot.WarningCodes, geminiWarningMemorySessionStore)
}

func TestGeminiHealthService_BuildHealthSnapshotWarnsOnInvalidRuntimeContract(t *testing.T) {
	repo := &geminiHealthRepoStub{
		mockAccountRepoForGemini: mockAccountRepoForGemini{
			accounts: []Account{{
				ID:       11,
				Name:     "Broken Gemini",
				Platform: PlatformGemini,
				Type:     AccountTypeOAuth,
				Credentials: map[string]any{
					"oauth_type": "legacy_unknown",
				},
			}},
		},
	}
	svc := NewGeminiHealthService(repo, NewGeminiOAuthService(nil, nil, nil, nil, &config.Config{}), &config.Config{})
	snapshot, err := svc.BuildHealthSnapshot(context.Background())
	require.NoError(t, err)
	require.Contains(t, snapshot.WarningCodes, geminiWarningInvalidRuntimeContract)
	require.Equal(t, geminiHealthStatusDegraded, snapshot.GatewayStatus)
}

func TestGeminiHealthService_BuildVerifySnapshotSurfacesStatusesWithoutSecrets(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gemini.ProductionMode = true
	cfg.Gemini.TokenCacheMode = "encrypted"
	cfg.Gemini.RequireThoughtSignatureSessionSafety = true
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("63", 32)

	protector, err := ProvideGeminiSecretProtector(cfg)
	require.NoError(t, err)
	protected, err := protector.ProtectCredentials(map[string]any{
		"refresh_token":             "rt",
		"oauth_type":                "google_one",
		"project_id":                "proj",
		"tier_id":                   GeminiTierGoogleOneFree,
		geminiDriveTierUpdatedAtKey: time.Now().Format(time.RFC3339),
		geminiOAuthStateKey:         geminiOAuthStateDegraded,
		geminiOAuthReasonKey:        geminiOAuthReasonGoogleOneDefaultTierFallback,
		geminiOAuthTierSource:       geminiTierSourceDefaultFallback,
	})
	require.NoError(t, err)

	repo := &geminiHealthRepoStub{
		mockAccountRepoForGemini: mockAccountRepoForGemini{
			accountsByID: map[int64]*Account{
				7: {
					ID:          7,
					Name:        "Gemini Google One",
					Platform:    PlatformGemini,
					Type:        AccountTypeOAuth,
					Credentials: protected,
					Extra: map[string]any{
						geminiTokenCacheStateKey:  geminiTokenCacheStateDegraded,
						geminiTokenCacheReasonKey: geminiTokenCacheReasonPlaintextEntryPresent,
					},
				},
			},
		},
	}

	svc := NewGeminiHealthService(repo, NewGeminiOAuthService(nil, nil, nil, nil, cfg), cfg)
	snapshot, err := svc.BuildVerifySnapshot(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, int64(7), snapshot.AccountID)
	require.Equal(t, "present", snapshot.ProjectIDStatus)
	require.Equal(t, "default_fallback", snapshot.TierStatus)
	require.Equal(t, geminiTokenCacheStateDegraded, snapshot.TokenCacheState)
	require.Equal(t, geminiTokenCacheReasonPlaintextEntryPresent, snapshot.TokenCacheReason)
	require.Equal(t, geminiOAuthStateDegraded, snapshot.OAuthState)
	require.Equal(t, geminiOAuthReasonGoogleOneDefaultTierFallback, snapshot.OAuthReason)
	require.Equal(t, "memory", snapshot.SessionStore)
	require.NotContains(t, snapshot.ProjectID, "rt")
}

func TestGeminiHealthService_BuildVerifySnapshotRejectsInvalidRuntimeContract(t *testing.T) {
	repo := &geminiHealthRepoStub{
		mockAccountRepoForGemini: mockAccountRepoForGemini{
			accountsByID: map[int64]*Account{
				8: {
					ID:       8,
					Name:     "Invalid Gemini",
					Platform: PlatformGemini,
					Type:     AccountTypeOAuth,
					Credentials: map[string]any{
						"oauth_type": "legacy_unknown",
					},
				},
			},
		},
	}

	svc := NewGeminiHealthService(repo, NewGeminiOAuthService(nil, nil, nil, nil, &config.Config{}), &config.Config{})
	snapshot, err := svc.BuildVerifySnapshot(context.Background(), 8)
	require.Error(t, err)
	require.Nil(t, snapshot)
}

func TestGeminiHealthService_BuildVerifySnapshotFlagsUnreadableServiceAccountProjectID(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gemini.ProductionMode = true
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("66", 32)

	repo := &geminiHealthRepoStub{
		mockAccountRepoForGemini: mockAccountRepoForGemini{
			accountsByID: map[int64]*Account{
				9: {
					ID:       9,
					Name:     "Broken Vertex",
					Platform: PlatformGemini,
					Type:     AccountTypeServiceAccount,
					Credentials: map[string]any{
						"service_account_json": "not-json",
					},
				},
			},
		},
	}

	svc := NewGeminiHealthService(repo, NewGeminiOAuthService(nil, nil, nil, nil, cfg), cfg)
	snapshot, err := svc.BuildVerifySnapshot(context.Background(), 9)
	require.NoError(t, err)
	require.Equal(t, "unreadable", snapshot.ProjectIDStatus)
	require.Contains(t, snapshot.ProjectIDReason, "service_account_json")
}
