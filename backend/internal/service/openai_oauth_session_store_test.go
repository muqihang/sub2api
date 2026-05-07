package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type openAIOAuthSessionKVStub struct {
	values map[string]string
	setErr error
	getErr error
	delErr error
}

func newOpenAIOAuthSessionKVStub() *openAIOAuthSessionKVStub {
	return &openAIOAuthSessionKVStub{values: map[string]string{}}
}

func (s *openAIOAuthSessionKVStub) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	if s.setErr != nil {
		return s.setErr
	}
	s.values[key] = value
	return nil
}

func (s *openAIOAuthSessionKVStub) Get(ctx context.Context, key string) (string, error) {
	if s.getErr != nil {
		return "", s.getErr
	}
	value, ok := s.values[key]
	if !ok {
		return "", redis.Nil
	}
	return value, nil
}

func (s *openAIOAuthSessionKVStub) Del(ctx context.Context, key string) error {
	if s.delErr != nil {
		return s.delErr
	}
	delete(s.values, key)
	return nil
}

func (s *openAIOAuthSessionKVStub) Close() error { return nil }

func TestOpenAIOAuthSessionStore_RedisSetGetDelete(t *testing.T) {
	store := NewOpenAIOAuthRedisSessionStoreWithKV(newOpenAIOAuthSessionKVStub())

	err := store.Set("sid", &openai.OAuthSession{
		State:        "expected-state",
		CodeVerifier: "verifier",
		RedirectURI:  openai.DefaultRedirectURI,
		CreatedAt:    time.Now(),
	})
	require.NoError(t, err)

	session, ok, err := store.Get("sid")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "expected-state", session.State)

	err = store.Delete("sid")
	require.NoError(t, err)
	_, ok, err = store.Get("sid")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestOpenAIOAuthSessionStore_RedisMalformedPayloadBehavesAsNotFound(t *testing.T) {
	kv := newOpenAIOAuthSessionKVStub()
	store := NewOpenAIOAuthRedisSessionStoreWithKV(kv)
	kv.values[store.key("sid")] = "{bad-json"

	_, ok, err := store.Get("sid")
	require.NoError(t, err)
	require.False(t, ok)
	_, exists := kv.values[store.key("sid")]
	require.False(t, exists)
}

func TestOpenAIOAuthSessionStore_ProviderRejectsProductionMemoryWithoutSticky(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.ProductionMode = true
	cfg.Gateway.OpenAICore.OAuthSessionStore = "memory"
	cfg.Gateway.OpenAICore.OAuthCallbackStickySingleInstance = false

	store, err := ProvideOpenAIOAuthSessionStore(nil, cfg)
	require.Error(t, err)
	require.Nil(t, store)
}

func TestOpenAIOAuthSessionStore_ProviderReturnsMemoryStoreByDefault(t *testing.T) {
	store, err := ProvideOpenAIOAuthSessionStore(nil, &config.Config{})
	require.NoError(t, err)
	require.NotNil(t, store)
}

func TestOpenAIOAuthService_ExchangeCode_SessionNotFoundOnMalformedStore(t *testing.T) {
	kv := newOpenAIOAuthSessionKVStub()
	store := NewOpenAIOAuthRedisSessionStoreWithKV(kv)
	kv.values[store.key("sid")] = "{bad-json"

	svc := NewOpenAIOAuthServiceWithStore(nil, &openaiOAuthClientStateStub{}, store)
	defer svc.Stop()

	_, err := svc.ExchangeCode(context.Background(), &OpenAIExchangeCodeInput{
		SessionID: "sid",
		Code:      "auth-code",
		State:     "expected-state",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "OPENAI_OAUTH_SESSION_NOT_FOUND")
}

func TestOpenAIOAuthService_ExchangeCode_DeletesSessionBeforeReuse(t *testing.T) {
	client := &openaiOAuthClientStateStub{}
	store := openai.NewSessionStore()
	svc := NewOpenAIOAuthServiceWithStore(nil, client, store)
	defer svc.Stop()

	require.NoError(t, svc.sessionStore.Set("sid", &openai.OAuthSession{
		State:        "expected-state",
		CodeVerifier: "verifier",
		RedirectURI:  openai.DefaultRedirectURI,
		CreatedAt:    time.Now(),
	}))

	_, err := svc.ExchangeCode(context.Background(), &OpenAIExchangeCodeInput{
		SessionID: "sid",
		Code:      "auth-code",
		State:     "expected-state",
	})
	require.NoError(t, err)
	_, ok, err := svc.sessionStore.Get("sid")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestOpenAIOAuthSessionStore_RedisRequiresClient(t *testing.T) {
	store, err := NewOpenAIOAuthRedisSessionStore(nil)
	require.Error(t, err)
	require.Nil(t, store)
	require.True(t, strings.Contains(err.Error(), "redis client"))
}
