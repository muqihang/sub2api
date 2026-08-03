package openai

import (
	"testing"
	"time"
)

func TestOAuthSessionMemoryStore_SetGetDelete(t *testing.T) {
	store := NewSessionStore()
	defer func() { _ = store.Stop() }()

	session := &OAuthSession{
		State:        "state-1",
		CodeVerifier: "verifier-1",
		RedirectURI:  DefaultRedirectURI,
		CreatedAt:    time.Now(),
	}

	if err := store.Set("sid", session); err != nil {
		t.Fatalf("Set() error: %v", err)
	}
	got, ok, err := store.Get("sid")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if !ok || got == nil {
		t.Fatalf("expected session to exist")
	}
	if got.State != "state-1" {
		t.Fatalf("state mismatch: got %q", got.State)
	}
	if err := store.Delete("sid"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
	_, ok, err = store.Get("sid")
	if err != nil {
		t.Fatalf("Get() after delete error: %v", err)
	}
	if ok {
		t.Fatalf("expected deleted session to be absent")
	}
}

func TestOAuthSessionMemoryStore_ExpiredNotFound(t *testing.T) {
	store := NewSessionStore()
	defer func() { _ = store.Stop() }()

	if err := store.Set("sid", &OAuthSession{
		State:        "state-1",
		CodeVerifier: "verifier-1",
		RedirectURI:  DefaultRedirectURI,
		CreatedAt:    time.Now().Add(-SessionTTL - time.Minute),
	}); err != nil {
		t.Fatalf("Set() error: %v", err)
	}
	_, ok, err := store.Get("sid")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if ok {
		t.Fatalf("expected expired session to be absent")
	}
}
