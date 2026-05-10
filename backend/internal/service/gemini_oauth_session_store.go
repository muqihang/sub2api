package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/redis/go-redis/v9"
)

const geminiOAuthSessionStoreKeyPrefix = "gemini:oauth:session:"

type GeminiOAuthSessionStore interface {
	Set(sessionID string, session *geminicli.OAuthSession) error
	Get(sessionID string) (*geminicli.OAuthSession, bool, error)
	Consume(sessionID string) (*geminicli.OAuthSession, bool, error)
	Delete(sessionID string) error
	Stop() error
}

type GeminiOAuthMemorySessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*geminicli.OAuthSession
	stopOnce sync.Once
	stopCh   chan struct{}
}

func NewGeminiOAuthMemorySessionStore() *GeminiOAuthMemorySessionStore {
	store := &GeminiOAuthMemorySessionStore{
		sessions: make(map[string]*geminicli.OAuthSession),
		stopCh:   make(chan struct{}),
	}
	go store.cleanup()
	return store
}

func (s *GeminiOAuthMemorySessionStore) Set(sessionID string, session *geminicli.OAuthSession) error {
	if s == nil {
		return errors.New("oauth session store unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sessionID] = session
	return nil
}

func (s *GeminiOAuthMemorySessionStore) Get(sessionID string) (*geminicli.OAuthSession, bool, error) {
	if s == nil {
		return nil, false, errors.New("oauth session store unavailable")
	}
	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	s.mu.RUnlock()
	if !ok {
		return nil, false, nil
	}
	if time.Since(session.CreatedAt) > geminicli.SessionTTL {
		_ = s.Delete(sessionID)
		return nil, false, nil
	}
	return session, true, nil
}

func (s *GeminiOAuthMemorySessionStore) Consume(sessionID string) (*geminicli.OAuthSession, bool, error) {
	if s == nil {
		return nil, false, errors.New("oauth session store unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, false, nil
	}
	delete(s.sessions, sessionID)
	if time.Since(session.CreatedAt) > geminicli.SessionTTL {
		return nil, false, nil
	}
	return session, true, nil
}

func (s *GeminiOAuthMemorySessionStore) Delete(sessionID string) error {
	if s == nil {
		return errors.New("oauth session store unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
	return nil
}

func (s *GeminiOAuthMemorySessionStore) Stop() error {
	if s == nil {
		return nil
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	return nil
}

func (s *GeminiOAuthMemorySessionStore) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.mu.Lock()
			for id, session := range s.sessions {
				if time.Since(session.CreatedAt) > geminicli.SessionTTL {
					delete(s.sessions, id)
				}
			}
			s.mu.Unlock()
		}
	}
}

type geminiOAuthSessionKV interface {
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	Get(ctx context.Context, key string) (string, error)
	GetDel(ctx context.Context, key string) (string, error)
	Del(ctx context.Context, key string) error
	Close() error
}

type geminiOAuthRedisKV struct {
	client *redis.Client
}

func (k *geminiOAuthRedisKV) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	return k.client.Set(ctx, key, value, ttl).Err()
}

func (k *geminiOAuthRedisKV) Get(ctx context.Context, key string) (string, error) {
	return k.client.Get(ctx, key).Result()
}

func (k *geminiOAuthRedisKV) GetDel(ctx context.Context, key string) (string, error) {
	return k.client.GetDel(ctx, key).Result()
}

func (k *geminiOAuthRedisKV) Del(ctx context.Context, key string) error {
	return k.client.Del(ctx, key).Err()
}

func (k *geminiOAuthRedisKV) Close() error { return nil }

type GeminiOAuthRedisSessionStore struct {
	kv        geminiOAuthSessionKV
	keyPrefix string
	ttl       time.Duration
}

func NewGeminiOAuthRedisSessionStore(client *redis.Client) (*GeminiOAuthRedisSessionStore, error) {
	if client == nil {
		return nil, errors.New("redis client is required")
	}
	return NewGeminiOAuthRedisSessionStoreWithKV(&geminiOAuthRedisKV{client: client}), nil
}

func NewGeminiOAuthRedisSessionStoreWithKV(kv geminiOAuthSessionKV) *GeminiOAuthRedisSessionStore {
	return &GeminiOAuthRedisSessionStore{
		kv:        kv,
		keyPrefix: geminiOAuthSessionStoreKeyPrefix,
		ttl:       geminicli.SessionTTL,
	}
}

func (s *GeminiOAuthRedisSessionStore) key(sessionID string) string {
	return s.keyPrefix + strings.TrimSpace(sessionID)
}

func (s *GeminiOAuthRedisSessionStore) Set(sessionID string, session *geminicli.OAuthSession) error {
	if s == nil || s.kv == nil {
		return errors.New("oauth session store unavailable")
	}
	payload, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("marshal oauth session: %w", err)
	}
	return s.kv.Set(context.Background(), s.key(sessionID), string(payload), s.ttl)
}

func (s *GeminiOAuthRedisSessionStore) Get(sessionID string) (*geminicli.OAuthSession, bool, error) {
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
	session, ok := s.decodePayload(payload)
	if !ok {
		_ = s.kv.Del(context.Background(), s.key(sessionID))
		return nil, false, nil
	}
	return session, true, nil
}

func (s *GeminiOAuthRedisSessionStore) Consume(sessionID string) (*geminicli.OAuthSession, bool, error) {
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
	session, ok := s.decodePayload(payload)
	if !ok {
		return nil, false, nil
	}
	return session, true, nil
}

func (s *GeminiOAuthRedisSessionStore) Delete(sessionID string) error {
	if s == nil || s.kv == nil {
		return errors.New("oauth session store unavailable")
	}
	return s.kv.Del(context.Background(), s.key(sessionID))
}

func (s *GeminiOAuthRedisSessionStore) Stop() error {
	if s == nil || s.kv == nil {
		return nil
	}
	return s.kv.Close()
}

func (s *GeminiOAuthRedisSessionStore) decodePayload(payload string) (*geminicli.OAuthSession, bool) {
	var session geminicli.OAuthSession
	if err := json.Unmarshal([]byte(payload), &session); err != nil {
		return nil, false
	}
	if time.Since(session.CreatedAt) > geminicli.SessionTTL {
		return nil, false
	}
	return &session, true
}
