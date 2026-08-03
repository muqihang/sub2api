package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const (
	testGuardKey    = "guard-key-that-is-at-least-thirty-two-bytes"
	testUpstreamKey = "upstream-admin-key"
)

func TestGuardRejectsUnauthorizedAndUnknownRoutes(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("upstream must not be called")
	}))
	defer upstream.Close()

	h := newTestGuard(t, upstream.URL)

	for _, tc := range []struct {
		name   string
		method string
		path   string
		key    string
		want   int
	}{
		{name: "missing key", method: http.MethodGet, path: "/api/v1/admin/accounts", want: http.StatusUnauthorized},
		{name: "wrong key", method: http.MethodGet, path: "/api/v1/admin/accounts", key: "wrong", want: http.StatusUnauthorized},
		{name: "unknown path", method: http.MethodPost, path: "/api/v1/admin/settings", key: testGuardKey, want: http.StatusForbidden},
		{name: "wrong method", method: http.MethodDelete, path: "/api/v1/admin/accounts", key: testGuardKey, want: http.StatusMethodNotAllowed},
		{name: "unknown query", method: http.MethodGet, path: "/api/v1/admin/accounts?redirect=https://example.com", key: testGuardKey, want: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.key != "" {
				req.Header.Set("X-Api-Key", tc.key)
			}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != tc.want {
				t.Fatalf("status = %d, want %d; body=%s", rr.Code, tc.want, rr.Body.String())
			}
		})
	}
}

func TestGuardForwardsAllowedGETAndRedactsSecrets(t *testing.T) {
	var gotHeader http.Header
	var gotQuery url.Values
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Clone()
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"data":{"accounts":[{"id":1,"name":"template","status":"active","credentials":{"access_token":"secret","refresh_token":"secret","model_mapping":{"gpt-5.6-sol":"gpt-5.6-sol"}},"extra":{"session_key":"secret","quota":{"remaining":80}},"proxy":{"host":"127.0.0.1","username":"user","password":"secret"}}]}}`)
	}))
	defer upstream.Close()

	h := newTestGuard(t, upstream.URL)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/data?include_proxies=true&page=1&page_size=100&search=template&sort_by=name&sort_order=asc", nil)
	req.Header.Set("X-Api-Key", testGuardKey)
	req.Header.Set("Authorization", "Bearer must-not-forward")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rr.Code, rr.Body.String())
	}
	if gotHeader.Get("X-Api-Key") != testUpstreamKey {
		t.Fatalf("upstream key = %q", gotHeader.Get("X-Api-Key"))
	}
	if gotHeader.Get("Authorization") != "" {
		t.Fatalf("authorization header leaked upstream")
	}
	if gotQuery.Get("include_proxies") != "true" || gotQuery.Get("search") != "template" {
		t.Fatalf("query not preserved: %v", gotQuery)
	}

	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	text := rr.Body.String()
	for _, forbidden := range []string{"access_token", "refresh_token", "session_key", "password", "secret"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("response contains %q: %s", forbidden, text)
		}
	}
	for _, required := range []string{"model_mapping", "gpt-5.6-sol", "remaining", "template", "username"} {
		if !strings.Contains(text, required) {
			t.Fatalf("response lost %q: %s", required, text)
		}
	}
}

func TestGuardForwardsSupportedImportBodies(t *testing.T) {
	tests := []struct {
		name string
		path string
		body string
	}{
		{
			name: "data import",
			path: "/api/v1/admin/accounts/data",
			body: `{"data":{"type":"sub2api-data","version":1,"proxies":[],"accounts":[{"name":"new","platform":"openai","type":"oauth","credentials":{"refresh_token":"incoming"},"extra":{"model_mapping":{"gpt-5.6-sol":"gpt-5.6-sol"}},"concurrency":1,"priority":1}]},"skip_default_group_bind":true}`,
		},
		{
			name: "batch import",
			path: "/api/v1/admin/accounts/batch",
			body: `{"accounts":[{"name":"new","platform":"openai","type":"oauth","credentials":{"refresh_token":"incoming"},"group_ids":[2],"concurrency":1,"priority":1}]}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			var gotBody []byte
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotBody, _ = io.ReadAll(r.Body)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"code":0,"data":{"success":1}}`)
			}))
			defer upstream.Close()

			h := newTestGuard(t, upstream.URL)
			req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewBufferString(tc.body))
			req.Header.Set("X-Api-Key", testGuardKey)
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d; body=%s", rr.Code, rr.Body.String())
			}
			if gotPath != tc.path {
				t.Fatalf("path = %q, want %q", gotPath, tc.path)
			}
			if !bytes.Equal(gotBody, []byte(tc.body)) {
				t.Fatalf("body changed\ngot:  %s\nwant: %s", gotBody, tc.body)
			}
		})
	}
}

func TestGuardRejectsMalformedOrOversizedImports(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("upstream must not be called")
	}))
	defer upstream.Close()

	h := newGuard(guardConfig{
		UpstreamURL:      upstream.URL,
		UpstreamAPIKey:   testUpstreamKey,
		GuardAPIKey:      testGuardKey,
		MaxRequestBytes:  128,
		MaxResponseBytes: 1024 * 1024,
		UpstreamTimeout:  time.Second,
	}, upstream.Client())

	for _, tc := range []struct {
		name string
		path string
		body string
		want int
	}{
		{name: "invalid json", path: "/api/v1/admin/accounts/data", body: `{`, want: http.StatusBadRequest},
		{name: "wrong data shape", path: "/api/v1/admin/accounts/data", body: `{"accounts":[]}`, want: http.StatusBadRequest},
		{name: "unknown top level", path: "/api/v1/admin/accounts/batch", body: `{"accounts":[{}],"operation":"delete"}`, want: http.StatusBadRequest},
		{name: "oversized", path: "/api/v1/admin/accounts/batch", body: `{"accounts":[{"name":"` + strings.Repeat("x", 200) + `"}]}`, want: http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewBufferString(tc.body))
			req.Header.Set("X-Api-Key", testGuardKey)
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != tc.want {
				t.Fatalf("status = %d, want %d; body=%s", rr.Code, tc.want, rr.Body.String())
			}
		})
	}
}

func TestGuardHealthDoesNotRequireAuthentication(t *testing.T) {
	h := newTestGuard(t, "http://127.0.0.1:1")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusOK || strings.TrimSpace(rr.Body.String()) != `{"status":"ok"}` {
		t.Fatalf("unexpected health response: %d %s", rr.Code, rr.Body.String())
	}
}

func TestGuardPinsUpstreamOrigin(t *testing.T) {
	var gotHost string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"data":{"accounts":[]}}`)
	}))
	defer upstream.Close()

	h := newTestGuard(t, upstream.URL)
	req := httptest.NewRequest(http.MethodGet, "http://attacker.invalid/api/v1/admin/accounts?page=1", nil)
	req.Host = "attacker.invalid"
	req.Header.Set("X-Api-Key", testGuardKey)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rr.Code, rr.Body.String())
	}
	wantHost := strings.TrimPrefix(upstream.URL, "http://")
	if gotHost != wantHost {
		t.Fatalf("upstream host = %q, want %q", gotHost, wantHost)
	}
}

func TestValidateConfigRejectsUnsafeUpstreamOrigins(t *testing.T) {
	base := guardConfig{
		UpstreamURL:      "http://sub2api:8080",
		UpstreamAPIKey:   testUpstreamKey,
		GuardAPIKey:      testGuardKey,
		MaxRequestBytes:  1024,
		MaxResponseBytes: 1024,
		UpstreamTimeout:  time.Second,
	}
	for _, upstream := range []string{
		"http://user:password@sub2api:8080",
		"http://sub2api:8080/api/v1",
		"http://sub2api:8080?redirect=evil",
		"file:///etc/passwd",
	} {
		t.Run(upstream, func(t *testing.T) {
			config := base
			config.UpstreamURL = upstream
			if err := validateConfig(config); err == nil {
				t.Fatalf("expected unsafe upstream URL to fail validation")
			}
		})
	}
}

func newTestGuard(t *testing.T, upstreamURL string) http.Handler {
	t.Helper()
	return newGuard(guardConfig{
		UpstreamURL:      upstreamURL,
		UpstreamAPIKey:   testUpstreamKey,
		GuardAPIKey:      testGuardKey,
		MaxRequestBytes:  8 * 1024 * 1024,
		MaxResponseBytes: 32 * 1024 * 1024,
		UpstreamTimeout:  5 * time.Second,
	}, http.DefaultClient)
}
