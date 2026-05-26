package service

import (
	"context"
	"strings"
	"testing"
	"time"
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
