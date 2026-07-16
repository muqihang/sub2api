package service

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	FormalPoolOnboardingStatusCreatingProxy           = "creating_proxy"
	FormalPoolOnboardingStatusOperationOutcomeUnknown = "operation_outcome_unknown"
	FormalPoolOnboardingStatusDraft                   = "draft"
	FormalPoolOnboardingStatusProxyVerified           = "proxy_verified"
	FormalPoolOnboardingStatusBrowserEgressVerified   = "browser_egress_verified"
	FormalPoolOnboardingStatusOAuthURLGenerated       = "oauth_url_generated"
	FormalPoolOnboardingStatusAccountCreated          = "oauth_exchanged_account_created"
	FormalPoolOnboardingStatusImported                = FormalPoolStageImported
	FormalPoolOnboardingStatusRefreshed               = FormalPoolStageRefreshed
	FormalPoolOnboardingStatusRuntimeRegistered       = FormalPoolStageRuntimeRegistered
	FormalPoolOnboardingStatusHealthcheckPassed       = FormalPoolStageHealthcheckPassed
	FormalPoolOnboardingStatusWarming                 = FormalPoolStageWarming
	FormalPoolOnboardingStatusProduction              = FormalPoolStageProduction
	FormalPoolOnboardingStatusQuarantined             = FormalPoolStageQuarantined
	FormalPoolOnboardingStatusPendingAcceptance       = "pending_acceptance"
	FormalPoolOnboardingStatusReadyForSmallFlow       = FormalPoolStageWarming
	FormalPoolOnboardingStatusFailed                  = "failed"
	FormalPoolOnboardingStatusAborted                 = "aborted"
	FormalPoolOnboardingDefaultTTL                    = 45 * time.Minute
	FormalPoolOnboardingDefaultConcurrency            = 10
	FormalPoolOnboardingMaxConcurrency                = 10
	formalPoolBrowserEgressPublicPathPrefix           = "/api/v1/claude-onboarding/browser-egress-check/"
)

type formalPoolOnboardingSessionRecord struct {
	ID                           string
	Version                      int64
	OwnerSubjectID               int64
	OwnerAdministratorID         int64
	OwnerTenantID                string
	OwnerCreatorID               int64
	OwnerRole                    string
	OwnerGroupID                 int64
	CreateKeySafeRef             string
	CreateRequestFingerprint     string
	ActiveOperation              *FormalPoolOperationReservation
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
	mu         sync.RWMutex
	ttl        time.Duration
	now        func() time.Time
	sessions   map[string]*formalPoolOnboardingSessionRecord
	createKeys map[string]string
}

func NewFormalPoolOnboardingStore(ttl time.Duration, now func() time.Time) *FormalPoolOnboardingStore {
	if ttl <= 0 {
		ttl = FormalPoolOnboardingDefaultTTL
	}
	if now == nil {
		now = time.Now
	}
	return &FormalPoolOnboardingStore{
		ttl: ttl, now: now,
		sessions:   map[string]*formalPoolOnboardingSessionRecord{},
		createKeys: map[string]string{},
	}
}

func (s *FormalPoolOnboardingStore) save(rec *formalPoolOnboardingSessionRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := cloneFormalPoolOnboardingSessionRecord(rec)
	if copy.Version <= 0 {
		copy.Version = 1
	}
	s.sessions[rec.ID] = copy
}

func (s *FormalPoolOnboardingStore) get(id string) (*formalPoolOnboardingSessionRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.sessions[strings.TrimSpace(id)]
	if !ok || rec == nil || s.now().Sub(rec.CreatedAt) > s.ttl {
		return nil, false
	}
	return cloneFormalPoolOnboardingSessionRecord(rec), true
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
	return cloneFormalPoolOnboardingSessionRecord(rec), nil
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
			return cloneFormalPoolOnboardingSessionRecord(rec), nil
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
	next := cloneFormalPoolOnboardingSessionRecord(rec)
	if err := mutate(next); err != nil {
		return nil, err
	}
	next.Version = rec.Version + 1
	next.UpdatedAt = now
	s.sessions[rec.ID] = next
	return cloneFormalPoolOnboardingSessionRecord(next), nil
}

func (s *FormalPoolOnboardingStore) completeReservedMutation(id string, reservation *FormalPoolOperationReservation, mutate func(*formalPoolOnboardingSessionRecord) error) (*formalPoolOnboardingSessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.sessions[strings.TrimSpace(id)]
	if !ok || rec == nil {
		return nil, ErrFormalPoolOnboardingNotFound
	}
	active := rec.ActiveOperation
	if reservation == nil || rec.Version != reservation.ReservationVersion ||
		active == nil || active.OperationID != reservation.OperationID || active.ReservationVersion != reservation.ReservationVersion {
		return nil, ErrFormalPoolOnboardingVersionConflict
	}
	next := cloneFormalPoolOnboardingSessionRecord(rec)
	if mutate != nil {
		if err := mutate(next); err != nil {
			return nil, err
		}
	}
	next.ActiveOperation = nil
	next.Version = rec.Version + 1
	next.UpdatedAt = s.now()
	s.sessions[rec.ID] = next
	return cloneFormalPoolOnboardingSessionRecord(next), nil
}

func (s *FormalPoolOnboardingStore) snapshotByAccountID(accountID int64) (*formalPoolOnboardingSessionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if accountID <= 0 {
		return nil, ErrFormalPoolOnboardingNotFound
	}
	now := s.now()
	for _, rec := range s.sessions {
		if rec != nil && rec.AccountID == accountID && !s.sessionExpired(rec, now) {
			return cloneFormalPoolOnboardingSessionRecord(rec), nil
		}
	}
	return nil, ErrFormalPoolOnboardingNotFound
}

func (s *FormalPoolOnboardingStore) snapshotByCreateKey(tenantID string, administratorID, creatorID int64, keySafeRef string) (*formalPoolOnboardingSessionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	lookupKey := formalPoolCreateKeyIndex(tenantID, administratorID, creatorID, keySafeRef)
	id := s.createKeys[lookupKey]
	rec := s.sessions[id]
	if rec == nil || s.sessionExpired(rec, s.now()) {
		return nil, ErrFormalPoolOnboardingNotFound
	}
	return cloneFormalPoolOnboardingSessionRecord(rec), nil
}

func (s *FormalPoolOnboardingStore) beginCreateReservation(rec *formalPoolOnboardingSessionRecord) (*formalPoolOnboardingSessionRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rec == nil {
		return nil, false, ErrFormalPoolOnboardingVersionConflict
	}
	if s.createKeys == nil {
		s.createKeys = map[string]string{}
	}
	indexKey := formalPoolCreateKeyIndex(rec.OwnerTenantID, rec.OwnerAdministratorID, rec.OwnerCreatorID, rec.CreateKeySafeRef)
	if existingID := s.createKeys[indexKey]; existingID != "" {
		existing := s.sessions[existingID]
		if existing != nil {
			if !formalPoolCreateOwnerMatches(existing, rec) {
				return nil, false, ErrFormalPoolOnboardingForbidden
			}
			if existing.CreateRequestFingerprint != rec.CreateRequestFingerprint || existing.ActiveOperation != nil {
				return nil, false, ErrFormalPoolOnboardingVersionConflict
			}
			if !s.createReservationExpired(existing, s.now()) {
				return cloneFormalPoolOnboardingSessionRecord(existing), true, nil
			}
		}
		delete(s.createKeys, indexKey)
	}
	stored := cloneFormalPoolOnboardingSessionRecord(rec)
	stored.Version = 1
	s.sessions[stored.ID] = stored
	s.createKeys[indexKey] = stored.ID
	return cloneFormalPoolOnboardingSessionRecord(stored), false, nil
}

func (s *FormalPoolOnboardingStore) sessionExpired(rec *formalPoolOnboardingSessionRecord, now time.Time) bool {
	return rec == nil || now.Sub(rec.CreatedAt) > s.ttl
}

func (s *FormalPoolOnboardingStore) createReservationExpired(rec *formalPoolOnboardingSessionRecord, now time.Time) bool {
	if rec == nil {
		return true
	}
	if rec.ActiveOperation != nil || rec.Status == FormalPoolOnboardingStatusOperationOutcomeUnknown {
		return false
	}
	retainedAt := rec.UpdatedAt
	if retainedAt.IsZero() {
		retainedAt = rec.CreatedAt
	}
	return now.Sub(retainedAt) > s.ttl
}

func formalPoolCreateOwnerMatches(existing, candidate *formalPoolOnboardingSessionRecord) bool {
	return existing != nil && candidate != nil &&
		existing.OwnerSubjectID == candidate.OwnerSubjectID &&
		existing.OwnerAdministratorID == candidate.OwnerAdministratorID &&
		existing.OwnerTenantID == candidate.OwnerTenantID &&
		existing.OwnerCreatorID == candidate.OwnerCreatorID &&
		existing.OwnerRole == candidate.OwnerRole &&
		existing.OwnerGroupID > 0 && existing.OwnerGroupID == existing.GroupID &&
		candidate.OwnerGroupID > 0 && candidate.OwnerGroupID == candidate.GroupID &&
		existing.OwnerGroupID == candidate.OwnerGroupID
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
			return cloneFormalPoolOnboardingSessionRecord(rec), true
		}
	}
	return nil, false
}

func formalPoolCreateKeyIndex(tenantID string, administratorID, creatorID int64, keySafeRef string) string {
	return strings.TrimSpace(tenantID) + "\x00" +
		fmt.Sprintf("%d\x00%d\x00%s", administratorID, creatorID, strings.TrimSpace(keySafeRef))
}

func cloneFormalPoolOnboardingSessionRecord(rec *formalPoolOnboardingSessionRecord) *formalPoolOnboardingSessionRecord {
	if rec == nil {
		return nil
	}
	copy := *rec
	if rec.ActiveOperation != nil {
		operation := *rec.ActiveOperation
		copy.ActiveOperation = &operation
	}
	if rec.CreatedProxyInput != nil {
		proxy := *rec.CreatedProxyInput
		copy.CreatedProxyInput = &proxy
	}
	if rec.OAuthSummary != nil {
		summary := *rec.OAuthSummary
		copy.OAuthSummary = &summary
	}
	return &copy
}
