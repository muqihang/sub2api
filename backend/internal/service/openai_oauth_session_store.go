package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/redis/go-redis/v9"
)

const openAIOAuthSessionStoreKeyPrefix = "openai:oauth:session:"

type openAIOAuthSessionKV interface {
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	Get(ctx context.Context, key string) (string, error)
	GetDel(ctx context.Context, key string) (string, error)
	Del(ctx context.Context, key string) error
	Close() error
}

type openAIOAuthRedisKV struct {
	client *redis.Client
}

func (k *openAIOAuthRedisKV) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	return k.client.Set(ctx, key, value, ttl).Err()
}

func (k *openAIOAuthRedisKV) Get(ctx context.Context, key string) (string, error) {
	return k.client.Get(ctx, key).Result()
}

func (k *openAIOAuthRedisKV) GetDel(ctx context.Context, key string) (string, error) {
	return k.client.GetDel(ctx, key).Result()
}

func (k *openAIOAuthRedisKV) Del(ctx context.Context, key string) error {
	return k.client.Del(ctx, key).Err()
}

func (k *openAIOAuthRedisKV) Close() error { return nil }

type OpenAIOAuthRedisSessionStore struct {
	kv        openAIOAuthSessionKV
	keyPrefix string
	ttl       time.Duration
}

func NewOpenAIOAuthRedisSessionStore(client *redis.Client) (*OpenAIOAuthRedisSessionStore, error) {
	if client == nil {
		return nil, errors.New("redis client is required")
	}
	return NewOpenAIOAuthRedisSessionStoreWithKV(&openAIOAuthRedisKV{client: client}), nil
}

func NewOpenAIOAuthRedisSessionStoreWithKV(kv openAIOAuthSessionKV) *OpenAIOAuthRedisSessionStore {
	return &OpenAIOAuthRedisSessionStore{
		kv:        kv,
		keyPrefix: openAIOAuthSessionStoreKeyPrefix,
		ttl:       openai.SessionTTL,
	}
}

func (s *OpenAIOAuthRedisSessionStore) key(sessionID string) string {
	return s.keyPrefix + strings.TrimSpace(sessionID)
}

func (s *OpenAIOAuthRedisSessionStore) Set(sessionID string, session *openai.OAuthSession) error {
	if s == nil || s.kv == nil {
		return errors.New("oauth session store unavailable")
	}
	payload, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("marshal oauth session: %w", err)
	}
	return s.kv.Set(context.Background(), s.key(sessionID), string(payload), s.ttl)
}

func (s *OpenAIOAuthRedisSessionStore) Get(sessionID string) (*openai.OAuthSession, bool, error) {
	if s == nil || s.kv == nil {
		return nil, false, errors.New("oauth session store unavailable")
	}
	payload, err := s.kv.Get(context.Background(), s.key(sessionID))
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var session openai.OAuthSession
	if err := json.Unmarshal([]byte(payload), &session); err != nil {
		_ = s.kv.Del(context.Background(), s.key(sessionID))
		return nil, false, nil
	}
	if time.Since(session.CreatedAt) > openai.SessionTTL {
		_ = s.kv.Del(context.Background(), s.key(sessionID))
		return nil, false, nil
	}
	return &session, true, nil
}

func (s *OpenAIOAuthRedisSessionStore) Consume(sessionID string) (*openai.OAuthSession, bool, error) {
	if s == nil || s.kv == nil {
		return nil, false, errors.New("oauth session store unavailable")
	}
	payload, err := s.kv.GetDel(context.Background(), s.key(sessionID))
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var session openai.OAuthSession
	if err := json.Unmarshal([]byte(payload), &session); err != nil {
		return nil, false, nil
	}
	if time.Since(session.CreatedAt) > openai.SessionTTL {
		return nil, false, nil
	}
	return &session, true, nil
}

func (s *OpenAIOAuthRedisSessionStore) Delete(sessionID string) error {
	if s == nil || s.kv == nil {
		return errors.New("oauth session store unavailable")
	}
	return s.kv.Del(context.Background(), s.key(sessionID))
}

func (s *OpenAIOAuthRedisSessionStore) Stop() error {
	if s == nil || s.kv == nil {
		return nil
	}
	return s.kv.Close()
}

func ProvideOpenAIOAuthSessionStore(redisClient *redis.Client, cfg *config.Config) (openai.OAuthSessionStore, error) {
	if cfg != nil &&
		cfg.Gateway.OpenAICore.ProductionMode &&
		strings.EqualFold(strings.TrimSpace(cfg.Gateway.OpenAICore.OAuthSessionStore), "memory") &&
		!cfg.Gateway.OpenAICore.OAuthCallbackStickySingleInstance {
		return nil, errors.New("openai oauth session store requires redis or sticky single-instance callback in production mode")
	}

	if cfg != nil && strings.EqualFold(strings.TrimSpace(cfg.Gateway.OpenAICore.OAuthSessionStore), "redis") {
		return NewOpenAIOAuthRedisSessionStore(redisClient)
	}
	return openai.NewSessionStore(), nil
}

func ProvideOpenAIOAuthService(
	proxyRepo ProxyRepository,
	oauthClient OpenAIOAuthClient,
	sessionStore openai.OAuthSessionStore,
) *OpenAIOAuthService {
	return NewOpenAIOAuthServiceWithStore(proxyRepo, oauthClient, sessionStore)
}
