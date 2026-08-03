package service

import (
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/redis/go-redis/v9"
)

func ProvideGeminiOAuthSessionStore(redisClient *redis.Client, cfg *config.Config) (GeminiOAuthSessionStore, error) {
	if cfg != nil &&
		cfg.Gemini.ProductionMode &&
		cfg.Gemini.RequireSafeOAuthSessionStore &&
		(strings.TrimSpace(cfg.Redis.Address()) == "" || redisClient == nil) {
		return nil, errors.New("gemini oauth session store requires redis in production mode when require_safe_oauth_session_store=true")
	}

	if cfg != nil &&
		cfg.Gemini.ProductionMode &&
		cfg.Gemini.RequireSafeOAuthSessionStore &&
		strings.EqualFold(strings.TrimSpace(cfg.Gemini.TokenCacheMode), "encrypted") {
		return NewGeminiOAuthRedisSessionStore(redisClient)
	}

	if redisClient != nil && cfg != nil && cfg.Gemini.RequireSafeOAuthSessionStore {
		return NewGeminiOAuthRedisSessionStore(redisClient)
	}

	return NewGeminiOAuthMemorySessionStore(), nil
}

func ProvideGeminiOAuthService(
	proxyRepo ProxyRepository,
	oauthClient GeminiOAuthClient,
	codeAssist GeminiCliCodeAssistClient,
	driveClient geminicli.DriveClient,
	sessionStore GeminiOAuthSessionStore,
	cfg *config.Config,
) *GeminiOAuthService {
	return NewGeminiOAuthServiceWithStore(proxyRepo, oauthClient, codeAssist, driveClient, sessionStore, cfg)
}
