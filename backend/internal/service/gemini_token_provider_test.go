//go:build unit

package service

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type geminiProviderTokenCacheStub struct {
	mu         sync.Mutex
	tokens     map[string]string
	getCalled  int32
	setCalled  int32
	lockCalled int32
}

func newGeminiProviderTokenCacheStub() *geminiProviderTokenCacheStub {
	return &geminiProviderTokenCacheStub{tokens: make(map[string]string)}
}

func (s *geminiProviderTokenCacheStub) GetAccessToken(ctx context.Context, cacheKey string) (string, error) {
	atomic.AddInt32(&s.getCalled, 1)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tokens[cacheKey], nil
}

func (s *geminiProviderTokenCacheStub) SetAccessToken(ctx context.Context, cacheKey string, token string, ttl time.Duration) error {
	atomic.AddInt32(&s.setCalled, 1)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[cacheKey] = token
	return nil
}

func (s *geminiProviderTokenCacheStub) DeleteAccessToken(ctx context.Context, cacheKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, cacheKey)
	return nil
}

func (s *geminiProviderTokenCacheStub) AcquireRefreshLock(ctx context.Context, cacheKey string, ttl time.Duration) (bool, error) {
	atomic.AddInt32(&s.lockCalled, 1)
	return true, nil
}

func (s *geminiProviderTokenCacheStub) ReleaseRefreshLock(ctx context.Context, cacheKey string) error {
	return nil
}

type geminiProviderAccountRepoStub struct {
	mockAccountRepoForGemini
	extraUpdates map[int64]map[string]any
}

func (s *geminiProviderAccountRepoStub) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	if s.extraUpdates == nil {
		s.extraUpdates = map[int64]map[string]any{}
	}
	copied := map[string]any{}
	for key, value := range updates {
		copied[key] = value
	}
	s.extraUpdates[id] = copied
	return nil
}

func TestGeminiTokenProvider_CacheMissFromEncryptedCredentials(t *testing.T) {
	cache := newGeminiProviderTokenCacheStub()
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("55", 32)
	protector, err := ProvideGeminiSecretProtector(cfg)
	require.NoError(t, err)

	expiresAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	protected, err := protector.ProtectCredentials(map[string]any{
		"access_token": "credential-token",
		"expires_at":   expiresAt,
		"oauth_type":   "ai_studio",
	})
	require.NoError(t, err)

	account := &Account{
		ID:          101,
		Platform:    PlatformGemini,
		Type:        AccountTypeOAuth,
		Credentials: protected,
	}

	provider := NewGeminiTokenProvider(nil, cache, NewGeminiOAuthService(nil, nil, nil, nil, cfg))

	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "credential-token", token)
	require.Equal(t, "credential-token", cache.tokens[GeminiTokenCacheKey(account)])
}

func TestGeminiTokenProvider_ProductionIgnoresPlaintextCacheAndMarksDegraded(t *testing.T) {
	cache := newGeminiProviderTokenCacheStub()
	repo := &geminiProviderAccountRepoStub{}
	cfg := &config.Config{}
	cfg.Gemini.ProductionMode = true
	cfg.Gemini.TokenCacheMode = "encrypted"
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("59", 32)
	protector, err := ProvideGeminiSecretProtector(cfg)
	require.NoError(t, err)

	expiresAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	protected, err := protector.ProtectCredentials(map[string]any{
		"access_token": "credential-token",
		"expires_at":   expiresAt,
		"oauth_type":   "ai_studio",
	})
	require.NoError(t, err)

	account := &Account{
		ID:          103,
		Platform:    PlatformGemini,
		Type:        AccountTypeOAuth,
		Credentials: protected,
	}
	cacheKey := GeminiTokenCacheKey(account)
	cache.tokens[cacheKey] = "plaintext-cache-token"

	provider := NewGeminiTokenProvider(repo, cache, NewGeminiOAuthService(nil, nil, nil, nil, cfg))

	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "credential-token", token)
	require.Equal(t, int32(0), atomic.LoadInt32(&cache.setCalled))
	require.Equal(t, "plaintext-cache-token", cache.tokens[cacheKey])
	require.Equal(t, geminiTokenCacheStateDegraded, account.Extra[geminiTokenCacheStateKey])
	require.Equal(t, geminiTokenCacheReasonPlaintextEntryPresent, account.Extra[geminiTokenCacheReasonKey])
	require.Equal(t, geminiTokenCacheStateDegraded, repo.extraUpdates[account.ID][geminiTokenCacheStateKey])
	require.Equal(t, geminiTokenCacheReasonPlaintextEntryPresent, repo.extraUpdates[account.ID][geminiTokenCacheReasonKey])
}

func TestGeminiTokenProvider_ProductionDoesNotWritePlaintextCache(t *testing.T) {
	cache := newGeminiProviderTokenCacheStub()
	cfg := &config.Config{}
	cfg.Gemini.ProductionMode = true
	cfg.Gemini.TokenCacheMode = "encrypted"
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("60", 32)
	protector, err := ProvideGeminiSecretProtector(cfg)
	require.NoError(t, err)

	expiresAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	protected, err := protector.ProtectCredentials(map[string]any{
		"access_token": "credential-token",
		"expires_at":   expiresAt,
		"oauth_type":   "ai_studio",
	})
	require.NoError(t, err)

	account := &Account{
		ID:          104,
		Platform:    PlatformGemini,
		Type:        AccountTypeOAuth,
		Credentials: protected,
	}

	provider := NewGeminiTokenProvider(nil, cache, NewGeminiOAuthService(nil, nil, nil, nil, cfg))

	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "credential-token", token)
	require.Equal(t, int32(0), atomic.LoadInt32(&cache.setCalled))
	_, ok := cache.tokens[GeminiTokenCacheKey(account)]
	require.False(t, ok)
}

func TestGeminiTokenProvider_PlaintextCacheAllowedOutsideProduction(t *testing.T) {
	cache := newGeminiProviderTokenCacheStub()
	cfg := &config.Config{}
	cfg.Gemini.TokenCacheMode = "plaintext"

	account := &Account{
		ID:       105,
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "credential-token",
			"expires_at":   time.Now().Add(time.Hour).Format(time.RFC3339),
			"oauth_type":   "ai_studio",
		},
	}
	cacheKey := GeminiTokenCacheKey(account)
	cache.tokens[cacheKey] = "plaintext-cache-token"

	provider := NewGeminiTokenProvider(nil, cache, NewGeminiOAuthService(nil, nil, nil, nil, cfg))

	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "plaintext-cache-token", token)
	require.Nil(t, account.Extra)
}

func TestGeminiTokenProvider_ServiceAccountPlaintextCacheAllowedOutsideProduction(t *testing.T) {
	cache := newGeminiProviderTokenCacheStub()
	cfg := &config.Config{}
	cfg.Gemini.TokenCacheMode = "plaintext"
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("62", 32)
	protector, err := ProvideGeminiSecretProtector(cfg)
	require.NoError(t, err)

	raw := `{
		"type": "service_account",
		"project_id": "vertex-proj",
		"private_key_id": "kid",
		"private_key": "not-a-real-private-key",
		"client_email": "svc@vertex-proj.iam.gserviceaccount.com"
	}`
	protected, err := protector.ProtectCredentials(map[string]any{
		"service_account_json": raw,
	})
	require.NoError(t, err)

	account := &Account{
		ID:          204,
		Platform:    PlatformGemini,
		Type:        AccountTypeServiceAccount,
		Credentials: protected,
	}
	key, err := parseVertexServiceAccountJSON([]byte(raw))
	require.NoError(t, err)
	cache.tokens[vertexServiceAccountCacheKey(account, key)] = "plaintext-cache-token"

	provider := NewGeminiTokenProvider(nil, cache, NewGeminiOAuthService(nil, nil, nil, nil, cfg))

	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "plaintext-cache-token", token)
	require.Nil(t, account.Extra)
}

func TestGeminiTokenProvider_UsesEncryptedLatestAccountWhenVersionIsStale(t *testing.T) {
	cache := newGeminiProviderTokenCacheStub()
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("56", 32)
	protector, err := ProvideGeminiSecretProtector(cfg)
	require.NoError(t, err)

	expiresAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	latestProtected, err := protector.ProtectCredentials(map[string]any{
		"access_token":   "latest-token",
		"expires_at":     expiresAt,
		"oauth_type":     "ai_studio",
		"_token_version": "2",
	})
	require.NoError(t, err)

	repo := &mockAccountRepoForGemini{
		accountsByID: map[int64]*Account{
			7: {
				ID:          7,
				Platform:    PlatformGemini,
				Type:        AccountTypeOAuth,
				Credentials: latestProtected,
			},
		},
	}
	account := &Account{
		ID:       7,
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":   "old-token",
			"expires_at":     expiresAt,
			"oauth_type":     "ai_studio",
			"_token_version": "1",
		},
	}

	provider := NewGeminiTokenProvider(repo, cache, NewGeminiOAuthService(nil, nil, nil, nil, cfg))

	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "latest-token", token)
}

func TestGeminiTokenProvider_ServiceAccountReadsEncryptedCredentialsOnLivePath(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("57", 32)
	protector, err := ProvideGeminiSecretProtector(cfg)
	require.NoError(t, err)

	raw := `{
		"type": "service_account",
		"project_id": "vertex-proj",
		"private_key_id": "kid",
		"private_key": "not-a-real-private-key",
		"client_email": "svc@vertex-proj.iam.gserviceaccount.com"
	}`
	protected, err := protector.ProtectCredentials(map[string]any{
		"service_account_json": raw,
	})
	require.NoError(t, err)

	account := &Account{
		ID:          202,
		Platform:    PlatformGemini,
		Type:        AccountTypeServiceAccount,
		Credentials: protected,
	}

	provider := NewGeminiTokenProvider(nil, newGeminiProviderTokenCacheStub(), NewGeminiOAuthService(nil, nil, nil, nil, cfg))

	_, err = provider.GetAccessToken(context.Background(), account)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse service account private key")
	require.NotContains(t, err.Error(), "invalid service account json")
}

func TestGeminiTokenProvider_ServiceAccountProductionIgnoresPlaintextCacheAndMarksDegraded(t *testing.T) {
	cache := newGeminiProviderTokenCacheStub()
	repo := &geminiProviderAccountRepoStub{}
	cfg := &config.Config{}
	cfg.Gemini.ProductionMode = true
	cfg.Gemini.TokenCacheMode = "encrypted"
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("61", 32)
	protector, err := ProvideGeminiSecretProtector(cfg)
	require.NoError(t, err)

	raw := `{
		"type": "service_account",
		"project_id": "vertex-proj",
		"private_key_id": "kid",
		"private_key": "not-a-real-private-key",
		"client_email": "svc@vertex-proj.iam.gserviceaccount.com"
	}`
	protected, err := protector.ProtectCredentials(map[string]any{
		"service_account_json": raw,
	})
	require.NoError(t, err)

	account := &Account{
		ID:          203,
		Platform:    PlatformGemini,
		Type:        AccountTypeServiceAccount,
		Credentials: protected,
	}
	key, err := parseVertexServiceAccountJSON([]byte(raw))
	require.NoError(t, err)
	cache.tokens[vertexServiceAccountCacheKey(account, key)] = "plaintext-cache-token"

	provider := NewGeminiTokenProvider(repo, cache, NewGeminiOAuthService(nil, nil, nil, nil, cfg))

	_, err = provider.GetAccessToken(context.Background(), account)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse service account private key")
	require.Equal(t, int32(0), atomic.LoadInt32(&cache.setCalled))
	require.Equal(t, geminiTokenCacheStateDegraded, account.Extra[geminiTokenCacheStateKey])
	require.Equal(t, geminiTokenCacheReasonPlaintextEntryPresent, account.Extra[geminiTokenCacheReasonKey])
	require.Equal(t, geminiTokenCacheStateDegraded, repo.extraUpdates[account.ID][geminiTokenCacheStateKey])
	require.Equal(t, geminiTokenCacheReasonPlaintextEntryPresent, repo.extraUpdates[account.ID][geminiTokenCacheReasonKey])
}

func TestGeminiTokenProvider_ProjectIDAutodetectFailureFallsBackToAIStudioInCompatMode(t *testing.T) {
	cache := newGeminiProviderTokenCacheStub()
	svc := NewGeminiOAuthService(nil, nil, nil, nil, &config.Config{})
	defer svc.Stop()

	account := &Account{
		ID:       301,
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":           "compat-token",
			"expires_at":             time.Now().Add(time.Hour).Format(time.RFC3339),
			"oauth_type":             "code_assist",
			"auto_detect_project_id": "true",
		},
	}

	provider := NewGeminiTokenProvider(nil, cache, svc)

	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "compat-token", token)
	require.Empty(t, strings.TrimSpace(account.GetCredential("project_id")))
}

func TestGeminiTokenProvider_ProjectIDAutodetectFailureRejectedInProduction(t *testing.T) {
	cache := newGeminiProviderTokenCacheStub()
	cfg := &config.Config{}
	cfg.Gemini.ProductionMode = true
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("58", 32)
	svc := NewGeminiOAuthService(nil, nil, nil, nil, cfg)
	defer svc.Stop()
	protector, err := ProvideGeminiSecretProtector(cfg)
	require.NoError(t, err)

	protected, err := protector.ProtectCredentials(map[string]any{
		"access_token":           "prod-token",
		"expires_at":             time.Now().Add(time.Hour).Format(time.RFC3339),
		"oauth_type":             "code_assist",
		"auto_detect_project_id": "true",
	})
	require.NoError(t, err)

	account := &Account{
		ID:          302,
		Platform:    PlatformGemini,
		Type:        AccountTypeOAuth,
		Credentials: protected,
	}

	provider := NewGeminiTokenProvider(nil, cache, svc)

	_, err = provider.GetAccessToken(context.Background(), account)
	require.Error(t, err)
	require.Contains(t, err.Error(), "project_id auto-detect failed")
}
