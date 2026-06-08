package service

import (
	"errors"
	"testing"
	"time"
)

func TestFormalPoolOnboardingStoreSnapshotByNonceReturnsCopy(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := NewFormalPoolOnboardingStore(30*time.Minute, func() time.Time { return now })
	store.save(&formalPoolOnboardingSessionRecord{
		ID:             "session_1",
		Status:         FormalPoolOnboardingStatusProxyVerified,
		BrowserNonce:   "nonce_1",
		NonceExpiresAt: now.Add(5 * time.Minute),
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	snapshot, err := store.snapshotByNonce(" nonce_1 ")
	if err != nil {
		t.Fatalf("snapshotByNonce() error = %v", err)
	}
	snapshot.Status = FormalPoolOnboardingStatusFailed
	snapshot.BrowserNonce = "mutated"

	again, err := store.snapshotByNonce("nonce_1")
	if err != nil {
		t.Fatalf("snapshotByNonce() second error = %v", err)
	}
	if again.Status != FormalPoolOnboardingStatusProxyVerified {
		t.Fatalf("stored status was polluted by snapshot mutation: %q", again.Status)
	}
	if again.BrowserNonce != "nonce_1" {
		t.Fatalf("stored nonce was polluted by snapshot mutation: %q", again.BrowserNonce)
	}
}

func TestFormalPoolOnboardingStoreSnapshotByNonceNotFoundCases(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	current := now
	store := NewFormalPoolOnboardingStore(30*time.Minute, func() time.Time { return current })
	store.save(&formalPoolOnboardingSessionRecord{
		ID:             "session_1",
		BrowserNonce:   "nonce_1",
		NonceExpiresAt: now.Add(5 * time.Minute),
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	store.save(&formalPoolOnboardingSessionRecord{
		ID:             "session_2",
		BrowserNonce:   "nonce_expired",
		NonceExpiresAt: now.Add(-time.Minute),
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	cases := []struct {
		name  string
		nonce string
		shift time.Duration
	}{
		{name: "empty", nonce: "   "},
		{name: "missing", nonce: "missing"},
		{name: "session ttl expired", nonce: "nonce_1", shift: 31 * time.Minute},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			current = now.Add(tc.shift)
			_, err := store.snapshotByNonce(tc.nonce)
			if !errors.Is(err, ErrFormalPoolOnboardingNotFound) {
				t.Fatalf("snapshotByNonce() error = %v, want ErrFormalPoolOnboardingNotFound", err)
			}
		})
	}
}

func TestFormalPoolOnboardingStoreSnapshotByNonceReturnsExpiredNonceWithinSessionTTL(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	nonceExpiresAt := now.Add(-time.Minute)
	store := NewFormalPoolOnboardingStore(30*time.Minute, func() time.Time { return now })
	store.save(&formalPoolOnboardingSessionRecord{
		ID:             "session_1",
		BrowserNonce:   "nonce_expired",
		NonceExpiresAt: nonceExpiresAt,
		CreatedAt:      now.Add(-5 * time.Minute),
		UpdatedAt:      now.Add(-5 * time.Minute),
	})

	snapshot, err := store.snapshotByNonce("nonce_expired")
	if err != nil {
		t.Fatalf("snapshotByNonce() error = %v", err)
	}
	if !snapshot.NonceExpiresAt.Equal(nonceExpiresAt) {
		t.Fatalf("NonceExpiresAt = %v, want %v", snapshot.NonceExpiresAt, nonceExpiresAt)
	}
}

func TestFormalPoolOnboardingStoreCASUpdateSuccessIncrementsVersionAndUpdatedAt(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	current := now
	store := NewFormalPoolOnboardingStore(30*time.Minute, func() time.Time { return current })
	store.save(&formalPoolOnboardingSessionRecord{
		ID:        "session_1",
		Status:    FormalPoolOnboardingStatusDraft,
		Version:   7,
		CreatedAt: now,
		UpdatedAt: now,
	})

	current = now.Add(2 * time.Minute)
	updated, err := store.casUpdate(" session_1 ", 7, func(rec *formalPoolOnboardingSessionRecord) error {
		rec.Status = FormalPoolOnboardingStatusProxyVerified
		return nil
	})
	if err != nil {
		t.Fatalf("casUpdate() error = %v", err)
	}
	if updated.Version != 8 {
		t.Fatalf("version = %d, want 8", updated.Version)
	}
	if !updated.UpdatedAt.Equal(current) {
		t.Fatalf("UpdatedAt = %v, want %v", updated.UpdatedAt, current)
	}
	if updated.Status != FormalPoolOnboardingStatusProxyVerified {
		t.Fatalf("status = %q", updated.Status)
	}
}

func TestFormalPoolOnboardingStoreCASUpdateVersionConflictDoesNotMutate(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := NewFormalPoolOnboardingStore(30*time.Minute, func() time.Time { return now })
	store.save(&formalPoolOnboardingSessionRecord{
		ID:        "session_1",
		Status:    FormalPoolOnboardingStatusDraft,
		Version:   3,
		CreatedAt: now,
		UpdatedAt: now,
	})

	called := false
	_, err := store.casUpdate("session_1", 2, func(rec *formalPoolOnboardingSessionRecord) error {
		called = true
		rec.Status = FormalPoolOnboardingStatusFailed
		return nil
	})
	if !errors.Is(err, ErrFormalPoolOnboardingVersionConflict) {
		t.Fatalf("casUpdate() error = %v, want ErrFormalPoolOnboardingVersionConflict", err)
	}
	if called {
		t.Fatalf("mutate should not be called on version conflict")
	}

	stored, ok := store.get("session_1")
	if !ok {
		t.Fatalf("stored session missing")
	}
	if stored.Status != FormalPoolOnboardingStatusDraft {
		t.Fatalf("status mutated despite conflict: %q", stored.Status)
	}
	if stored.Version != 3 {
		t.Fatalf("version mutated despite conflict: %d", stored.Version)
	}
}

func TestFormalPoolOnboardingStoreCASUpdateReturnsCopy(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := NewFormalPoolOnboardingStore(30*time.Minute, func() time.Time { return now })
	store.save(&formalPoolOnboardingSessionRecord{
		ID:        "session_1",
		Status:    FormalPoolOnboardingStatusDraft,
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
	})

	updated, err := store.casUpdate("session_1", 1, func(rec *formalPoolOnboardingSessionRecord) error {
		rec.Status = FormalPoolOnboardingStatusProxyVerified
		return nil
	})
	if err != nil {
		t.Fatalf("casUpdate() error = %v", err)
	}
	updated.Status = FormalPoolOnboardingStatusFailed
	updated.Version = 99

	stored, ok := store.get("session_1")
	if !ok {
		t.Fatalf("stored session missing")
	}
	if stored.Status != FormalPoolOnboardingStatusProxyVerified {
		t.Fatalf("stored status was polluted by returned copy mutation: %q", stored.Status)
	}
	if stored.Version != 2 {
		t.Fatalf("stored version was polluted by returned copy mutation: %d", stored.Version)
	}
}

func TestFormalPoolOnboardingStoreSaveInitializesVersion(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := NewFormalPoolOnboardingStore(30*time.Minute, func() time.Time { return now })
	store.save(&formalPoolOnboardingSessionRecord{
		ID:        "session_1",
		CreatedAt: now,
		UpdatedAt: now,
	})

	stored, ok := store.get("session_1")
	if !ok {
		t.Fatalf("stored session missing")
	}
	if stored.Version != 1 {
		t.Fatalf("initial version = %d, want 1", stored.Version)
	}
}
