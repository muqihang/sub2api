package service

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

const (
	FormalPoolOnboardingStatusDraft                 = "draft"
	FormalPoolOnboardingStatusProxyVerified         = "proxy_verified"
	FormalPoolOnboardingStatusBrowserEgressVerified = "browser_egress_verified"
	FormalPoolOnboardingStatusOAuthURLGenerated     = "oauth_url_generated"
	FormalPoolOnboardingStatusAccountCreated        = "oauth_exchanged_account_created"
	FormalPoolOnboardingStatusPendingAcceptance     = "pending_acceptance"
	FormalPoolOnboardingStatusReadyForSmallFlow     = "ready_for_small_flow"
	FormalPoolOnboardingStatusFailed                = "failed"
	FormalPoolOnboardingStatusAborted               = "aborted"
	FormalPoolOnboardingDefaultTTL                  = 45 * time.Minute
	FormalPoolOnboardingDefaultConcurrency          = 10
	FormalPoolOnboardingMaxConcurrency              = 10
	formalPoolBrowserEgressPublicPathPrefix         = "/api/v1/claude-onboarding/browser-egress-check/"
)

type formalPoolOnboardingSessionRecord struct {
	ID                         string
	Status                     string
	ProxyMode                  string
	ProxyID                    int64
	CreatedProxyInput          *FormalPoolProxyInput
	ProxyRef                   string
	NormalizedProxyURL         string
	GroupID                    int64
	AccountName                string
	Notes                      string
	PoolProfile                string
	Concurrency                int
	EgressBucket               string
	BrowserNonce               string
	BrowserVerified            bool
	OAuthSessionID             string
	AuthURL                    string
	AccountID                  int64
	AccountRef                 string
	OAuthSummary               *FormalPoolOAuthTokenSummary
	AcceptancePassed           bool
	CCGatewayRuntimeRegistered bool
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

type FormalPoolOnboardingStore struct {
	mu       sync.RWMutex
	ttl      time.Duration
	now      func() time.Time
	sessions map[string]*formalPoolOnboardingSessionRecord
}

func NewFormalPoolOnboardingStore(ttl time.Duration, now func() time.Time) *FormalPoolOnboardingStore {
	if ttl <= 0 {
		ttl = FormalPoolOnboardingDefaultTTL
	}
	if now == nil {
		now = time.Now
	}
	return &FormalPoolOnboardingStore{ttl: ttl, now: now, sessions: map[string]*formalPoolOnboardingSessionRecord{}}
}

func (s *FormalPoolOnboardingStore) save(rec *formalPoolOnboardingSessionRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *rec
	s.sessions[rec.ID] = &copy
}

func (s *FormalPoolOnboardingStore) get(id string) (*formalPoolOnboardingSessionRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.sessions[strings.TrimSpace(id)]
	if !ok || rec == nil || s.now().Sub(rec.CreatedAt) > s.ttl {
		return nil, false
	}
	copy := *rec
	return &copy, true
}

func (s *FormalPoolOnboardingStore) update(id string, fn func(*formalPoolOnboardingSessionRecord) error) (*formalPoolOnboardingSessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.sessions[strings.TrimSpace(id)]
	if !ok || rec == nil || s.now().Sub(rec.CreatedAt) > s.ttl {
		return nil, ErrFormalPoolOnboardingNotFound
	}
	if err := fn(rec); err != nil {
		return nil, err
	}
	rec.UpdatedAt = s.now()
	copy := *rec
	return &copy, nil
}

func formalPoolRandomID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return prefix + time.Now().UTC().Format("20060102150405.000000000")
	}
	return prefix + hex.EncodeToString(b[:])
}

func (s *FormalPoolOnboardingStore) findByNonce(nonce string) (*formalPoolOnboardingSessionRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	nonce = strings.TrimSpace(nonce)
	if nonce == "" {
		return nil, false
	}
	for _, rec := range s.sessions {
		if rec != nil && rec.BrowserNonce == nonce && s.now().Sub(rec.CreatedAt) <= s.ttl {
			copy := *rec
			return &copy, true
		}
	}
	return nil, false
}
