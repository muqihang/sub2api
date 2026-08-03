//go:build unit

package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type geminiOAuthSessionKVStub struct {
	values map[string]string
	setErr error
	getErr error
	delErr error
}

func newGeminiOAuthSessionKVStub() *geminiOAuthSessionKVStub {
	return &geminiOAuthSessionKVStub{values: map[string]string{}}
}

func (s *geminiOAuthSessionKVStub) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	if s.setErr != nil {
		return s.setErr
	}
	s.values[key] = value
	return nil
}

func (s *geminiOAuthSessionKVStub) Get(ctx context.Context, key string) (string, error) {
	if s.getErr != nil {
		return "", s.getErr
	}
	value, ok := s.values[key]
	if !ok {
		return "", redis.Nil
	}
	return value, nil
}

func (s *geminiOAuthSessionKVStub) GetDel(ctx context.Context, key string) (string, error) {
	value, err := s.Get(ctx, key)
	if err != nil {
		return "", err
	}
	delete(s.values, key)
	return value, nil
}

func (s *geminiOAuthSessionKVStub) Del(ctx context.Context, key string) error {
	if s.delErr != nil {
		return s.delErr
	}
	delete(s.values, key)
	return nil
}

func (s *geminiOAuthSessionKVStub) Close() error { return nil }

func TestGeminiOAuthSessionStore_MemorySetGetConsume(t *testing.T) {
	t.Parallel()

	store := NewGeminiOAuthMemorySessionStore()
	defer func() { require.NoError(t, store.Stop()) }()

	require.NoError(t, store.Set("sid", &geminicli.OAuthSession{
		State:        "expected-state",
		CodeVerifier: "verifier",
		CreatedAt:    time.Now(),
	}))

	session, ok, err := store.Get("sid")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "expected-state", session.State)

	consumed, ok, err := store.Consume("sid")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "expected-state", consumed.State)

	_, ok, err = store.Get("sid")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestGeminiOAuthSessionStore_MemoryConsumeIsOneTimeUse(t *testing.T) {
	t.Parallel()

	store := NewGeminiOAuthMemorySessionStore()
	defer func() { require.NoError(t, store.Stop()) }()

	require.NoError(t, store.Set("sid", &geminicli.OAuthSession{
		State:        "expected-state",
		CodeVerifier: "verifier",
		CreatedAt:    time.Now(),
	}))

	_, ok, err := store.Consume("sid")
	require.NoError(t, err)
	require.True(t, ok)

	_, ok, err = store.Consume("sid")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestGeminiOAuthSessionStore_MemoryRejectsExpiredSession(t *testing.T) {
	t.Parallel()

	store := NewGeminiOAuthMemorySessionStore()
	defer func() { require.NoError(t, store.Stop()) }()

	require.NoError(t, store.Set("sid", &geminicli.OAuthSession{
		State:        "expired-state",
		CodeVerifier: "verifier",
		CreatedAt:    time.Now().Add(-geminicli.SessionTTL - time.Minute),
	}))

	_, ok, err := store.Get("sid")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestGeminiOAuthSessionStore_RedisMalformedPayloadBehavesAsNotFound(t *testing.T) {
	t.Parallel()

	kv := newGeminiOAuthSessionKVStub()
	store := NewGeminiOAuthRedisSessionStoreWithKV(kv)
	kv.values[store.key("sid")] = "{bad-json"

	_, ok, err := store.Get("sid")
	require.NoError(t, err)
	require.False(t, ok)
	_, exists := kv.values[store.key("sid")]
	require.False(t, exists)
}

func TestGeminiOAuthSessionStore_RedisConsumeDeletesSession(t *testing.T) {
	t.Parallel()

	kv := newGeminiOAuthSessionKVStub()
	store := NewGeminiOAuthRedisSessionStoreWithKV(kv)

	require.NoError(t, store.Set("sid", &geminicli.OAuthSession{
		State:        "expected-state",
		CodeVerifier: "verifier",
		CreatedAt:    time.Now(),
	}))

	session, ok, err := store.Consume("sid")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "expected-state", session.State)

	_, ok, err = store.Get("sid")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestGeminiOAuthSessionStore_RedisRequiresClient(t *testing.T) {
	t.Parallel()

	store, err := NewGeminiOAuthRedisSessionStore(nil)
	require.Error(t, err)
	require.Nil(t, store)
	require.True(t, strings.Contains(err.Error(), "redis client"))
}

func TestProvideGeminiOAuthSessionStore_ProductionRequiresRedisWhenSafeStoreEnabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gemini.ProductionMode = true
	cfg.Gemini.RequireSafeOAuthSessionStore = true
	cfg.Gemini.TokenCacheMode = "encrypted"

	store, err := ProvideGeminiOAuthSessionStore(nil, cfg)
	require.Error(t, err)
	require.Nil(t, store)
}

func TestProvideGeminiOAuthSessionStore_UsesRedisWhenConfigured(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gemini.RequireSafeOAuthSessionStore = true
	cfg.Gemini.TokenCacheMode = "encrypted"
	cfg.Redis.Host = "127.0.0.1"
	cfg.Redis.Port = 6379

	store, err := ProvideGeminiOAuthSessionStore(&redis.Client{}, cfg)
	require.NoError(t, err)
	require.IsType(t, &GeminiOAuthRedisSessionStore{}, store)
}
