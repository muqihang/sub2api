package oauth

import (
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBuildAuthorizationURL_TracksClaudeCode2146EndpointAndClient(t *testing.T) {
	state := "state-123"
	challenge := "challenge-123"
	authURL := BuildAuthorizationURL(state, challenge, ScopeOAuth)

	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse auth url: %v", err)
	}

	if got := parsed.Scheme + "://" + parsed.Host + parsed.Path; got != "https://claude.ai/oauth/authorize" {
		t.Fatalf("authorize endpoint mismatch: got=%q", got)
	}
	q := parsed.Query()
	if got := q.Get("client_id"); got != ClientID {
		t.Fatalf("client_id mismatch: got=%q want=%q", got, ClientID)
	}
	if got := q.Get("redirect_uri"); got != RedirectURI {
		t.Fatalf("redirect_uri mismatch: got=%q want=%q", got, RedirectURI)
	}
	if got := q.Get("scope"); got != ScopeOAuth {
		t.Fatalf("scope mismatch: got=%q want=%q", got, ScopeOAuth)
	}
	if strings.Contains(q.Get("scope"), "org:create_api_key") {
		t.Fatalf("scope must not request org:create_api_key for Claude Code messages OAuth: got=%q", q.Get("scope"))
	}
	if !strings.Contains(" "+q.Get("scope")+" ", " user:inference ") {
		t.Fatalf("scope must include user:inference for messages OAuth: got=%q", q.Get("scope"))
	}
	if got := q.Get("code_challenge"); got != challenge {
		t.Fatalf("code_challenge mismatch: got=%q", got)
	}
	if got := q.Get("code_challenge_method"); got != "S256" {
		t.Fatalf("code_challenge_method mismatch: got=%q", got)
	}
	if got := q.Get("state"); got != state {
		t.Fatalf("state mismatch: got=%q", got)
	}
}

func TestSessionStore_Stop_Idempotent(t *testing.T) {
	store := NewSessionStore()

	store.Stop()
	store.Stop()

	select {
	case <-store.stopCh:
		// ok
	case <-time.After(time.Second):
		t.Fatal("stopCh 未关闭")
	}
}

func TestSessionStore_Stop_Concurrent(t *testing.T) {
	store := NewSessionStore()

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.Stop()
		}()
	}

	wg.Wait()

	select {
	case <-store.stopCh:
		// ok
	case <-time.After(time.Second):
		t.Fatal("stopCh 未关闭")
	}
}
