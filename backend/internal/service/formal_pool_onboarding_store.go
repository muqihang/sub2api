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
	FormalPoolOnboardingStatusImported              = FormalPoolStageImported
	FormalPoolOnboardingStatusRefreshed             = FormalPoolStageRefreshed
	FormalPoolOnboardingStatusRuntimeRegistered     = FormalPoolStageRuntimeRegistered
	FormalPoolOnboardingStatusHealthcheckPassed     = FormalPoolStageHealthcheckPassed
	FormalPoolOnboardingStatusWarming               = FormalPoolStageWarming
	FormalPoolOnboardingStatusProduction            = FormalPoolStageProduction
	FormalPoolOnboardingStatusQuarantined           = FormalPoolStageQuarantined
	FormalPoolOnboardingStatusPendingAcceptance     = "pending_acceptance"
	FormalPoolOnboardingStatusReadyForSmallFlow     = FormalPoolStageWarming
	FormalPoolOnboardingStatusFailed                = "failed"
	FormalPoolOnboardingStatusAborted               = "aborted"
	FormalPoolOnboardingDefaultTTL                  = 45 * time.Minute
	FormalPoolOnboardingDefaultConcurrency          = 10
	FormalPoolOnboardingMaxConcurrency              = 10
	formalPoolBrowserEgressPublicPathPrefix         = "/api/v1/claude-onboarding/browser-egress-check/"
)

type formalPoolOnboardingSessionRecord struct {
	ID                           string
	Version                      int64
	Status                       string
	ProxyMode                    string
	ProxyID                      int64
	CreatedProxyInput            *FormalPoolProxyInput
	ProxyRef                     string
	NormalizedProxyURL           string
	GroupID                      int64
	AccountName                  string
	Notes                        string
	PoolProfile                  string
	Concurrency                  int
	EgressBucket                 string
	BrowserNonce                 string
	NonceExpiresAt               time.Time
	BrowserVerified              bool
	BrowserEgressCheckStatus     string
	BrowserVerifiedAt            time.Time
	BrowserEgressMismatchAt      time.Time
	BrowserEgressBrowserIPBucket string
	BrowserEgressProxyIPBucket   string
	BrowserEgressLastErrorCode   string
	OAuthSessionID               string
	AuthURL                      string
	AccountID                    int64
	AccountRef                   string
	OAuthSummary                 *FormalPoolOAuthTokenSummary
	AcceptancePassed             bool
	HealthcheckPassed            bool
	CCGatewayRuntimeRegistered   bool
	CreatedAt                    time.Time
	UpdatedAt                    time.Time
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
	if copy.Version <= 0 {
		copy.Version = 1
	}
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

func (s *FormalPoolOnboardingStore) snapshotByNonce(nonce string) (*formalPoolOnboardingSessionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	nonce = strings.TrimSpace(nonce)
	if nonce == "" {
		return nil, ErrFormalPoolOnboardingNotFound
	}
	now := s.now()
	for _, rec := range s.sessions {
		if rec != nil && rec.BrowserNonce == nonce && !s.sessionExpired(rec, now) {
			copy := *rec
			return &copy, nil
		}
	}
	return nil, ErrFormalPoolOnboardingNotFound
}

func (s *FormalPoolOnboardingStore) casUpdate(id string, expectedVersion int64, mutate func(*formalPoolOnboardingSessionRecord) error) (*formalPoolOnboardingSessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.sessions[strings.TrimSpace(id)]
	now := s.now()
	if !ok || rec == nil || s.sessionExpired(rec, now) {
		return nil, ErrFormalPoolOnboardingNotFound
	}
	if rec.Version != expectedVersion {
		return nil, ErrFormalPoolOnboardingVersionConflict
	}
	next := *rec
	if err := mutate(&next); err != nil {
		return nil, err
	}
	next.Version = rec.Version + 1
	next.UpdatedAt = now
	s.sessions[rec.ID] = &next
	copy := next
	return &copy, nil
}

func (s *FormalPoolOnboardingStore) sessionExpired(rec *formalPoolOnboardingSessionRecord, now time.Time) bool {
	return rec == nil || now.Sub(rec.CreatedAt) > s.ttl
}

func nonceExpired(rec *formalPoolOnboardingSessionRecord, now time.Time) bool {
	return rec != nil && !rec.NonceExpiresAt.IsZero() && now.After(rec.NonceExpiresAt)
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
