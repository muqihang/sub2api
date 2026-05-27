package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestFormalPoolOnboardingStartValidation(t *testing.T) {
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{})

	cases := []struct {
		name string
		req  FormalPoolOnboardingStartRequest
	}{
		{name: "missing account name", req: FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(1), GroupID: 1}},
		{name: "missing group", req: FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(1), AccountName: "acct"}},
		{name: "invalid pool profile", req: FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(1), GroupID: 1, AccountName: "acct", PoolProfile: "canary"}},
		{name: "invalid concurrency", req: FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(1), GroupID: 1, AccountName: "acct", Concurrency: -1}},
		{name: "too high concurrency", req: FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(1), GroupID: 1, AccountName: "acct", Concurrency: 11}},
		{name: "missing existing proxy", req: FormalPoolOnboardingStartRequest{ProxyMode: "existing", GroupID: 1, AccountName: "acct"}},
		{name: "missing create proxy", req: FormalPoolOnboardingStartRequest{ProxyMode: "create", GroupID: 1, AccountName: "acct"}},
		{name: "dangerous account ref", req: FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(1), GroupID: 1, AccountName: "acct", AccountRef: "123"}},
		{name: "dangerous token", req: FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(1), GroupID: 1, AccountName: "acct", Token: "tok"}},
		{name: "dangerous refresh token", req: FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(1), GroupID: 1, AccountName: "acct", RefreshToken: "refresh"}},
		{name: "dangerous access token", req: FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(1), GroupID: 1, AccountName: "acct", AccessToken: "access"}},
		{name: "dangerous oauth code", req: FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(1), GroupID: 1, AccountName: "acct", Code: "code"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.StartSession(context.Background(), tc.req); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func TestFormalPoolOnboardingStartDefaultsAndSafeSummary(t *testing.T) {
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{})
	got, err := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{
		ProxyMode:   "create",
		GroupID:     42,
		AccountName: "Claude sub account",
		Proxy: &FormalPoolProxyInput{
			Name: "p1", Protocol: "socks5", Host: "127.0.0.1", Port: 1080,
			Username: "operator", Password: "secret-password",
		},
	})
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if got.PoolProfile != PoolProfileNormal {
		t.Fatalf("pool profile = %q", got.PoolProfile)
	}
	if got.Concurrency != FormalPoolOnboardingDefaultConcurrency {
		t.Fatalf("concurrency = %d", got.Concurrency)
	}
	if got.BrowserEgressCheckURL == "" || !strings.Contains(got.BrowserEgressCheckURL, "/api/v1/claude-onboarding/browser-egress-check/") {
		t.Fatalf("browser egress check URL missing public nonce path: %q", got.BrowserEgressCheckURL)
	}
	assertNoFormalPoolSensitive(t, got)
}

func TestFormalPoolOnboardingStoreTTLAbortAndNoSecretInResponse(t *testing.T) {
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	store := NewFormalPoolOnboardingStore(30*time.Minute, func() time.Time { return now })
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Store: store})
	created, err := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{
		ProxyMode:   "existing",
		ProxyID:     formalPtrInt64(7),
		GroupID:     42,
		AccountName: "acct",
	})
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if _, err := svc.AbortSession(context.Background(), created.ID); err != nil {
		t.Fatalf("AbortSession() error = %v", err)
	}
	aborted, err := svc.GetSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if aborted.Status != FormalPoolOnboardingStatusAborted {
		t.Fatalf("status = %q", aborted.Status)
	}
	assertNoFormalPoolSensitive(t, aborted)

	store.now = func() time.Time { return now.Add(31 * time.Minute) }
	if _, err := svc.GetSession(context.Background(), created.ID); err == nil {
		t.Fatalf("expected expired session error")
	}
}

func TestFormalPoolOnboardingBlocksOAuthUntilBrowserEgressVerifiedAndFailsClosed(t *testing.T) {
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{})
	created, err := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{
		ProxyMode:   "existing",
		ProxyID:     formalPtrInt64(7),
		GroupID:     42,
		AccountName: "acct",
	})
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if _, err := svc.GenerateAuthURL(context.Background(), created.ID); err == nil {
		t.Fatalf("expected browser egress gate error")
	}
	if _, err := svc.MarkBrowserEgressVerifiedForTest(context.Background(), created.ID); err != nil {
		t.Fatalf("mark verified: %v", err)
	}
	if _, err := svc.GenerateAuthURL(context.Background(), created.ID); err == nil {
		t.Fatalf("expected nil oauth facade to fail closed")
	}
}

func TestFormalPoolExchangeRejectsFrontendControlledRefsAndSecrets(t *testing.T) {
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{})
	created, err := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(7), GroupID: 42, AccountName: "acct"})
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	bad := []FormalPoolExchangeCodeAndCreateRequest{
		{Code: "code", AccountRef: "raw-ref"},
		{Code: "code", AccessToken: "access"},
		{Code: "code", RefreshToken: "refresh"},
		{Code: "code", ProxyID: formalPtrInt64(8)},
	}
	for _, req := range bad {
		if _, err := svc.ExchangeCodeAndCreate(context.Background(), created.ID, req); err == nil {
			t.Fatalf("expected request %+v to be rejected", req)
		}
	}
}

func TestFormalPoolOnboardingProxyValidation(t *testing.T) {
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{})
	for _, protocol := range []string{"socks5", "socks5h", "http", "https"} {
		_, err := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{
			ProxyMode: "create", GroupID: 42, AccountName: "acct",
			Proxy: &FormalPoolProxyInput{Name: "p", Protocol: protocol, Host: "127.0.0.1", Port: 1080, Password: "secret"},
		})
		if err != nil {
			t.Fatalf("protocol %s should be accepted: %v", protocol, err)
		}
	}
	_, err := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{
		ProxyMode: "create", GroupID: 42, AccountName: "acct",
		Proxy: &FormalPoolProxyInput{Name: "p", Protocol: "direct", Host: "127.0.0.1", Port: 1080},
	})
	if err == nil {
		t.Fatalf("expected unsupported proxy protocol to fail closed")
	}
}

func TestFormalPoolOnboardingRejectsDangerousRouteAndAccountRefInputs(t *testing.T) {
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{})
	for _, req := range []FormalPoolOnboardingStartRequest{
		{ProxyMode: "existing", ProxyID: formalPtrInt64(1), GroupID: 1, AccountName: "acct", AccountRef: "raw-ref"},
		{ProxyMode: "existing", ProxyID: formalPtrInt64(1), GroupID: 1, AccountName: "acct", Code: "oauth-code"},
		{ProxyMode: "existing", ProxyID: formalPtrInt64(1), GroupID: 1, AccountName: "acct", AccessToken: "access"},
	} {
		if _, err := svc.StartSession(context.Background(), req); err == nil {
			t.Fatalf("expected dangerous input %#v to be rejected", req)
		}
	}
}

func TestFormalPoolHTTPCCGatewayRuntimeRegistrarPostsSafeRuntimeMapping(t *testing.T) {
	var gotPath string
	var gotToken string
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotToken = r.Header.Get("X-CC-Gateway-Token")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"registered"}`))
	}))
	defer server.Close()

	registrar := NewFormalPoolHTTPCCGatewayRuntimeRegistrar(&config.Config{
		Gateway: config.GatewayConfig{
			CCGateway: config.GatewayCCGatewayConfig{
				Enabled:        true,
				BaseURL:        server.URL,
				Token:          "gateway-token",
				TimeoutSeconds: 1,
			},
		},
	})
	if registrar == nil {
		t.Fatalf("expected registrar")
	}

	err := registrar.RegisterCCGatewayRuntime(context.Background(), FormalPoolCCGatewayRuntimeRegistration{
		AccountRef:     "hmac-sha256:runtime-account-ref",
		EgressBucket:   "claude-runtime-bucket",
		ProxyURL:       "socks5h://user:pass@proxy.example:443",
		ProxyRef:       "hmac-sha256:runtime-proxy-ref",
		PolicyVersion:  "2.1.150",
		PersonaVariant: "claude-code-2.1.150-macos-local",
		SessionPolicy:  "preserve_downstream_session_id",
	})
	if err != nil {
		t.Fatalf("RegisterCCGatewayRuntime() error = %v", err)
	}

	if gotPath != "/_runtime/register-account" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotToken != "gateway-token" {
		t.Fatalf("gateway token header missing")
	}
	if got["account_id"] != "hmac-sha256:runtime-account-ref" ||
		got["account_ref"] != "hmac-sha256:runtime-account-ref" ||
		got["egress_bucket"] != "claude-runtime-bucket" ||
		got["proxy_url"] != "socks5h://user:pass@proxy.example:443" ||
		got["proxy_identity_ref"] != "hmac-sha256:runtime-proxy-ref" ||
		got["policy_version"] != "2.1.150" ||
		got["session_policy"] != "preserve_downstream_session_id" {
		t.Fatalf("unexpected registration payload: %#v", got)
	}
}

func TestFormalPoolHTTPCCGatewayRuntimeRegistrarFailsClosedOnGatewayError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "missing identity", http.StatusForbidden)
	}))
	defer server.Close()

	registrar := NewFormalPoolHTTPCCGatewayRuntimeRegistrar(&config.Config{
		Gateway: config.GatewayConfig{
			CCGateway: config.GatewayCCGatewayConfig{
				Enabled:        true,
				BaseURL:        server.URL,
				Token:          "gateway-token",
				TimeoutSeconds: 1,
			},
		},
	})
	err := registrar.RegisterCCGatewayRuntime(context.Background(), FormalPoolCCGatewayRuntimeRegistration{
		AccountRef:     "hmac-sha256:runtime-account-ref",
		EgressBucket:   "claude-runtime-bucket",
		ProxyURL:       "socks5h://proxy.example:443",
		ProxyRef:       "hmac-sha256:runtime-proxy-ref",
		PolicyVersion:  "2.1.150",
		PersonaVariant: "claude-code-2.1.150-macos-local",
		SessionPolicy:  "preserve_downstream_session_id",
	})
	if err == nil || !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("expected fail-closed gateway status error, got %v", err)
	}
}

func TestFormalPoolSafeSummaryRecursiveScan(t *testing.T) {
	dangerous := []map[string]any{
		{"nested": map[string]any{"proxy_password": "should-not-appear"}},
		{"nested": map[string]any{"token": "should-not-appear"}},
		{"nested": map[string]any{"refresh_token": "should-not-appear"}},
		{"nested": map[string]any{"oauth_code": "should-not-appear"}},
		{"nested": map[string]any{"raw_email": "person@example.com"}},
		{"nested": map[string]any{"account_uuid": "acct-uuid"}},
		{"nested": map[string]any{"org_uuid": "org-uuid"}},
		{"headers": map[string]any{"Authorization": "Bearer secret"}},
		{"headers": map[string]any{"x-api-key": "sk-secret"}},
		{"nested": map[string]any{"raw_proxy_url": "http://user:pass@127.0.0.1:8080"}},
		{"nested": map[string]any{"raw_cch": "cache-control-helper"}},
	}
	for _, unsafe := range dangerous {
		if !FormalPoolContainsSensitive(unsafe) {
			t.Fatalf("expected recursive sensitive detector to flag %#v", unsafe)
		}
	}
	safe := map[string]any{
		"proxy_ref":            "ref_abc",
		"email_present":        true,
		"account_uuid_present": true,
		"org_uuid_present":     true,
		"expires_in_bucket":    "gt_1h",
		"checks": []map[string]any{
			{"name": "cc_gateway_ready", "status": "not_run"},
		},
	}
	if FormalPoolContainsSensitive(safe) {
		t.Fatalf("expected safe summary to pass")
	}
}

func formalPtrInt64(v int64) *int64 { return &v }

func assertNoFormalPoolSensitive(t *testing.T, v any) {
	t.Helper()
	if path := FormalPoolSensitivePathForTest(v); path != "" {
		t.Fatalf("sensitive data found at %s in %#v", path, v)
	}
}
