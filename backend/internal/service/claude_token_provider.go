package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
)

const (
	claudeTokenRefreshSkew = 3 * time.Minute
	claudeTokenCacheSkew   = 5 * time.Minute
	claudeLockWaitTime     = 200 * time.Millisecond
)

// ClaudeTokenCache token cache interface.
type ClaudeTokenCache = GeminiTokenCache

// ClaudeTokenProvider manages access_token for Claude OAuth and Vertex service account accounts.
type ClaudeTokenProvider struct {
	accountRepo   AccountRepository
	tokenCache    ClaudeTokenCache
	oauthService  *OAuthService
	refreshAPI    *OAuthRefreshAPI
	executor      OAuthRefreshExecutor
	refreshPolicy ProviderRefreshPolicy
}

func NewClaudeTokenProvider(
	accountRepo AccountRepository,
	tokenCache ClaudeTokenCache,
	oauthService *OAuthService,
) *ClaudeTokenProvider {
	return &ClaudeTokenProvider{
		accountRepo:   accountRepo,
		tokenCache:    tokenCache,
		oauthService:  oauthService,
		refreshPolicy: ClaudeProviderRefreshPolicy(),
	}
}

// SetRefreshAPI injects unified OAuth refresh API and executor.
func (p *ClaudeTokenProvider) SetRefreshAPI(api *OAuthRefreshAPI, executor OAuthRefreshExecutor) {
	p.refreshAPI = api
	p.executor = executor
}

// SetRefreshPolicy injects caller-side refresh policy.
func (p *ClaudeTokenProvider) SetRefreshPolicy(policy ProviderRefreshPolicy) {
	p.refreshPolicy = policy
}

// GetAccessToken returns a valid access_token.
func (p *ClaudeTokenProvider) GetAccessToken(ctx context.Context, account *Account) (string, error) {
	if account == nil {
		return "", errors.New("account is nil")
	}
	if account.Platform != PlatformAnthropic || (account.Type != AccountTypeOAuth && account.Type != AccountTypeSetupToken && account.Type != AccountTypeServiceAccount) {
		return "", errors.New("not an anthropic oauth, setup-token, or service account")
	}
	if account.Type == AccountTypeServiceAccount {
		return p.getServiceAccountAccessToken(ctx, account)
	}

	cacheKey := ClaudeTokenCacheKey(account)
	refreshPolicy := p.effectiveRefreshPolicy(account)

	// 1) Try cache first.
	if p.tokenCache != nil {
		if token, err := p.tokenCache.GetAccessToken(ctx, cacheKey); err == nil && strings.TrimSpace(token) != "" {
			slog.Debug("claude_token_cache_hit", "account_id", account.ID)
			return token, nil
		} else if err != nil {
			slog.Warn("claude_token_cache_get_failed", "account_id", account.ID, "error", err)
		}
	}

	slog.Debug("claude_token_cache_miss", "account_id", account.ID)

	// 2) Refresh if needed (pre-expiry skew).
	expiresAt := account.GetCredentialAsTime("expires_at")
	needsRefresh := expiresAt == nil || time.Until(*expiresAt) <= claudeTokenRefreshSkew
	refreshFailed := false

	if needsRefresh && p.refreshAPI != nil && p.executor != nil {
		result, err := p.refreshAPI.RefreshIfNeeded(ctx, account, p.executor, claudeTokenRefreshSkew)
		if err != nil {
			if refreshPolicy.OnRefreshError == ProviderRefreshErrorReturn {
				return "", err
			}
			slog.Warn("claude_token_refresh_failed", "account_id", account.ID, "error", err)
			refreshFailed = true
		} else if result.LockHeld {
			if refreshPolicy.OnLockHeld == ProviderLockHeldWaitForCache && p.tokenCache != nil {
				time.Sleep(claudeLockWaitTime)
				if token, cacheErr := p.tokenCache.GetAccessToken(ctx, cacheKey); cacheErr == nil && strings.TrimSpace(token) != "" {
					slog.Debug("claude_token_cache_hit_after_wait", "account_id", account.ID)
					return token, nil
				}
			}
		} else {
			account = result.Account
			expiresAt = account.GetCredentialAsTime("expires_at")
		}
	} else if needsRefresh && p.tokenCache != nil {
		// Backward-compatible test path when refreshAPI is not injected.
		locked, lockErr := p.tokenCache.AcquireRefreshLock(ctx, cacheKey, 30*time.Second)
		if lockErr == nil && locked {
			defer func() { _ = p.tokenCache.ReleaseRefreshLock(ctx, cacheKey) }()
		} else if lockErr != nil {
			slog.Warn("claude_token_lock_failed", "account_id", account.ID, "error", lockErr)
		} else {
			time.Sleep(claudeLockWaitTime)
			if token, err := p.tokenCache.GetAccessToken(ctx, cacheKey); err == nil && strings.TrimSpace(token) != "" {
				slog.Debug("claude_token_cache_hit_after_wait", "account_id", account.ID)
				return token, nil
			}
		}
	}

	accessToken := account.GetCredential("access_token")
	if strings.TrimSpace(accessToken) == "" {
		return "", errors.New("access_token not found in credentials")
	}

	// 3) Populate cache with TTL.
	if p.tokenCache != nil {
		latestAccount, isStale := CheckTokenVersion(ctx, account, p.accountRepo)
		if isStale && latestAccount != nil {
			slog.Debug("claude_token_version_stale_use_latest", "account_id", account.ID)
			accessToken = latestAccount.GetCredential("access_token")
			if strings.TrimSpace(accessToken) == "" {
				return "", errors.New("access_token not found after version check")
			}
		} else {
			ttl := 30 * time.Minute
			if refreshFailed {
				if refreshPolicy.FailureTTL > 0 {
					ttl = refreshPolicy.FailureTTL
				} else {
					ttl = time.Minute
				}
				slog.Debug("claude_token_cache_short_ttl", "account_id", account.ID, "reason", "refresh_failed")
			} else if expiresAt != nil {
				until := time.Until(*expiresAt)
				switch {
				case until > claudeTokenCacheSkew:
					ttl = until - claudeTokenCacheSkew
				case until > 0:
					ttl = until
				default:
					ttl = time.Minute
				}
			}
			if err := p.tokenCache.SetAccessToken(ctx, cacheKey, accessToken, ttl); err != nil {
				slog.Warn("claude_token_cache_set_failed", "account_id", account.ID, "error", err)
			}
		}
	}

	return accessToken, nil
}

// ForceRefresh refreshes Claude OAuth/setup-token credentials after an upstream 401.
func (p *ClaudeTokenProvider) ForceRefresh(ctx context.Context, account *Account, staleAccessToken string) (*Account, error) {
	if p == nil || p.refreshAPI == nil || p.executor == nil {
		return nil, errors.New("claude token refresh unavailable")
	}
	if account == nil || account.Platform != PlatformAnthropic || (account.Type != AccountTypeOAuth && account.Type != AccountTypeSetupToken) {
		return nil, errors.New("not an anthropic oauth or setup-token account")
	}
	result, err := p.refreshAPI.ForceRefresh(ctx, account, p.executor, staleAccessToken)
	if err != nil {
		return nil, err
	}
	if result == nil || result.LockHeld {
		return nil, errors.New("claude token refresh lock held")
	}
	refreshed := result.Account
	if refreshed == nil {
		refreshed = account
	}
	if p.tokenCache != nil {
		_ = p.tokenCache.DeleteAccessToken(ctx, ClaudeTokenCacheKey(account))
	}
	return refreshed, nil
}

func (p *ClaudeTokenProvider) getServiceAccountAccessToken(ctx context.Context, account *Account) (string, error) {
	return getVertexServiceAccountAccessToken(ctx, p.tokenCache, account)
}

func (p *ClaudeTokenProvider) effectiveRefreshPolicy(account *Account) ProviderRefreshPolicy {
	policy := p.refreshPolicy
	if claudeOAuthRefreshShouldFailClosed(account) {
		policy.OnRefreshError = ProviderRefreshErrorReturn
	}
	return policy
}

func claudeOAuthRefreshShouldFailClosed(account *Account) bool {
	if account == nil {
		return false
	}
	return parseClaudeRefreshFailClosedFlag(account.GetExtraString("oauth_refresh_fail_closed")) ||
		parseClaudeRefreshFailClosedFlag(account.GetExtraString("cc_gateway_canary_only"))
}

func parseClaudeRefreshFailClosedFlag(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on", "fail_closed", "fail-closed":
		return true
	default:
		return false
	}
}
