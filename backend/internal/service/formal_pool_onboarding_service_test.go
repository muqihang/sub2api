package service

import (
	"context"
	"encoding/json"
	"errors"
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
	if got.BrowserEgressCheckURL != "" {
		t.Fatalf("StartSession browser egress check URL = %q, want empty until proxy test passes", got.BrowserEgressCheckURL)
	}
	if got.BrowserEgressCheckStatus != "idle" {
		t.Fatalf("StartSession browser egress status = %q, want idle", got.BrowserEgressCheckStatus)
	}
	assertNoFormalPoolSensitive(t, got)
}

func TestFormalPoolOnboardingStartDoesNotMintBrowserNonce(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
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
	if created.Status != FormalPoolOnboardingStatusDraft {
		t.Fatalf("StartSession status = %q, want %q", created.Status, FormalPoolOnboardingStatusDraft)
	}
	if created.BrowserEgressCheckURL != "" {
		t.Fatalf("StartSession browser URL = %q, want empty", created.BrowserEgressCheckURL)
	}
	if created.BrowserEgressCheckStatus != "idle" {
		t.Fatalf("StartSession browser egress status = %q, want idle", created.BrowserEgressCheckStatus)
	}
	rec, ok := store.get(created.ID)
	if !ok {
		t.Fatalf("stored session missing")
	}
	if rec.BrowserNonce != "" {
		t.Fatalf("stored BrowserNonce = %q, want empty", rec.BrowserNonce)
	}
	if !rec.NonceExpiresAt.IsZero() {
		t.Fatalf("stored NonceExpiresAt = %v, want zero", rec.NonceExpiresAt)
	}

	got, err := svc.GetSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if got.BrowserEgressCheckURL != "" {
		t.Fatalf("GetSession browser URL = %q, want empty", got.BrowserEgressCheckURL)
	}
}

func TestFormalPoolOnboardingTestProxyMintsBrowserNonceAndUsesNonceTTL(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	store := NewFormalPoolOnboardingStore(30*time.Minute, func() time.Time { return now })
	cfg := DefaultFormalPoolConfig()
	cfg.NonceTTL = 2 * time.Minute
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Store: store, Proxy: &formalProxyFake{}, Config: cfg})

	created, err := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{
		ProxyMode:   "existing",
		ProxyID:     formalPtrInt64(7),
		GroupID:     42,
		AccountName: "acct",
	})
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	_, err = store.update(created.ID, func(rec *formalPoolOnboardingSessionRecord) error {
		rec.BrowserVerified = true
		rec.BrowserVerifiedAt = now.Add(-time.Minute)
		rec.BrowserEgressMismatchAt = now.Add(-30 * time.Second)
		rec.BrowserEgressBrowserIPBucket = "browser_old"
		rec.BrowserEgressProxyIPBucket = "proxy_old"
		rec.BrowserEgressLastErrorCode = "mismatch_old"
		return nil
	})
	if err != nil {
		t.Fatalf("seed browser egress residue: %v", err)
	}

	tested, err := svc.TestProxy(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("TestProxy() error = %v", err)
	}
	if tested.BrowserEgressCheckURL == "" {
		t.Fatalf("TestProxy browser URL empty, want nonce URL")
	}
	if !strings.Contains(tested.BrowserEgressCheckURL, formalPoolBrowserEgressPublicPathPrefix) {
		t.Fatalf("TestProxy browser URL = %q, want public nonce path", tested.BrowserEgressCheckURL)
	}
	got, err := svc.GetSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if got.BrowserEgressCheckURL != tested.BrowserEgressCheckURL {
		t.Fatalf("GetSession browser URL = %q, want TestProxy URL %q", got.BrowserEgressCheckURL, tested.BrowserEgressCheckURL)
	}
	assertFormalPoolJSONContains(t, tested, "browser_egress_check_url", tested.BrowserEgressCheckURL)
	assertFormalPoolJSONDoesNotContainKey(t, tested.SafeSummary, "browser_egress_check_url")
	if tested.BrowserEgressCheckStatus != "waiting" || got.BrowserEgressCheckStatus != "waiting" {
		t.Fatalf("TestProxy/GetSession browser egress status = %q/%q, want waiting", tested.BrowserEgressCheckStatus, got.BrowserEgressCheckStatus)
	}
	assertRFC3339UTCString(t, tested.NonceExpiresAt, "TestProxy nonce_expires_at")
	assertRFC3339UTCString(t, got.NonceExpiresAt, "GetSession nonce_expires_at")

	rec, ok := store.get(created.ID)
	if !ok {
		t.Fatalf("stored session missing")
	}
	if rec.BrowserNonce == "" {
		t.Fatalf("stored BrowserNonce empty after successful TestProxy")
	}
	if rec.BrowserEgressCheckStatus != "waiting" {
		t.Fatalf("BrowserEgressCheckStatus = %q, want waiting", rec.BrowserEgressCheckStatus)
	}
	if rec.BrowserVerified || !rec.BrowserVerifiedAt.IsZero() || !rec.BrowserEgressMismatchAt.IsZero() {
		t.Fatalf("browser verified residue not cleared: verified=%v verified_at=%v mismatch_at=%v", rec.BrowserVerified, rec.BrowserVerifiedAt, rec.BrowserEgressMismatchAt)
	}
	if rec.BrowserEgressBrowserIPBucket != "" || rec.BrowserEgressProxyIPBucket != "" || rec.BrowserEgressLastErrorCode != "" {
		t.Fatalf("browser egress buckets/error not cleared: browser=%q proxy=%q error=%q", rec.BrowserEgressBrowserIPBucket, rec.BrowserEgressProxyIPBucket, rec.BrowserEgressLastErrorCode)
	}
	wantExpiry := now.Add(2 * time.Minute)
	if rec.NonceExpiresAt.Before(wantExpiry.Add(-time.Second)) || rec.NonceExpiresAt.After(wantExpiry.Add(time.Second)) {
		t.Fatalf("NonceExpiresAt = %v, want around %v", rec.NonceExpiresAt, wantExpiry)
	}
}

func TestFormalPoolOnboardingTestProxyDefaultsNonPositiveNonceTTL(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	store := NewFormalPoolOnboardingStore(30*time.Minute, func() time.Time { return now })
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{
		Store:  store,
		Proxy:  &formalProxyFake{},
		Config: FormalPoolConfig{NonceTTL: -time.Minute},
	})

	created, err := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{
		ProxyMode:   "existing",
		ProxyID:     formalPtrInt64(7),
		GroupID:     42,
		AccountName: "acct",
	})
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if _, err := svc.TestProxy(context.Background(), created.ID); err != nil {
		t.Fatalf("TestProxy() error = %v", err)
	}
	rec, ok := store.get(created.ID)
	if !ok {
		t.Fatalf("stored session missing")
	}
	wantExpiry := now.Add(5 * time.Minute)
	if rec.NonceExpiresAt.Before(wantExpiry.Add(-time.Second)) || rec.NonceExpiresAt.After(wantExpiry.Add(time.Second)) {
		t.Fatalf("NonceExpiresAt = %v, want around %v", rec.NonceExpiresAt, wantExpiry)
	}
}

func TestFormalPoolOnboardingBrowserURLEmptyNonce(t *testing.T) {
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{})
	for _, nonce := range []string{"", "   "} {
		if got := svc.browserURL(nonce); got != "" {
			t.Fatalf("browserURL(%q) = %q, want empty", nonce, got)
		}
	}
}

type formalPoolBrowserEgressProxyFake struct {
	testCalls int
	getCalls  int
	rawIP     string
	getErr    error
}

func (f *formalPoolBrowserEgressProxyFake) ResolveOrCreateProxy(ctx context.Context, req FormalPoolOnboardingStartRequest) (FormalPoolProxyResolution, error) {
	id := int64(9)
	if req.ProxyID != nil {
		id = *req.ProxyID
	}
	return FormalPoolProxyResolution{ProxyID: id, ProxyRef: formalPoolSafeRef("proxy", "browser-egress-proxy"), NormalizedProxyURL: "socks5h://proxy.local:1080"}, nil
}

func (f *formalPoolBrowserEgressProxyFake) TestProxy(ctx context.Context, proxyID int64) (FormalPoolProxyTestSummary, error) {
	f.testCalls++
	return FormalPoolProxyTestSummary{Success: true, ProxyRef: formalPoolSafeRef("proxy", "browser-egress-proxy"), ExitIPRef: formalPoolSafeRef("exit_ip", "proxy-exit"), LatencyBucket: "lt_500ms"}, nil
}

func (f *formalPoolBrowserEgressProxyFake) GetRawEgressIP(ctx context.Context, proxyID int64, normalizedProxyURL string) (string, error) {
	f.getCalls++
	if f.getErr != nil {
		return "", f.getErr
	}
	if strings.TrimSpace(f.rawIP) == "" {
		return "198.51.100.10", nil
	}
	return f.rawIP, nil
}

type formalPoolRiskCaptureWriter struct {
	verified []FormalPoolRiskEventInput
	mismatch []FormalPoolRiskEventInput
	expired  []FormalPoolRiskEventInput
	noProxy  []FormalPoolRiskEventInput
}

func (w *formalPoolRiskCaptureWriter) RecordEgressVerified(ctx context.Context, input FormalPoolRiskEventInput) error {
	w.verified = append(w.verified, input)
	return nil
}
func (w *formalPoolRiskCaptureWriter) RecordEgressMismatch(ctx context.Context, input FormalPoolRiskEventInput) error {
	w.mismatch = append(w.mismatch, input)
	return nil
}
func (w *formalPoolRiskCaptureWriter) RecordNonceExpired(ctx context.Context, input FormalPoolRiskEventInput) error {
	w.expired = append(w.expired, input)
	return nil
}
func (w *formalPoolRiskCaptureWriter) RecordEgressNoProxy(ctx context.Context, input FormalPoolRiskEventInput) error {
	w.noProxy = append(w.noProxy, input)
	return nil
}
func (w *formalPoolRiskCaptureWriter) RecordPublicRouteRateLimited(ctx context.Context, input FormalPoolRiskEventInput) error {
	return nil
}
func (w *formalPoolRiskCaptureWriter) RecordPublicRouteRateLimitedBuckets(ctx context.Context, nonceBucket, ipBucket, reason string) error {
	return nil
}

func TestFormalPoolVerifyBrowserEgressByNonceMatchingRemoteIPVerifiesAndRedacts(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	store := NewFormalPoolOnboardingStore(30*time.Minute, func() time.Time { return now })
	proxy := &formalPoolBrowserEgressProxyFake{rawIP: "198.51.100.10"}
	risk := &formalPoolRiskCaptureWriter{}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Store: store, Proxy: proxy, Risk: risk})
	created, nonce := formalPoolCreateSessionWithNonce(t, svc, store)

	got, err := svc.VerifyBrowserEgressByNonce(context.Background(), nonce, "198.51.100.10")
	if err != nil {
		t.Fatalf("VerifyBrowserEgressByNonce() error = %v", err)
	}
	if got.ID != created.ID || got.Status != FormalPoolOnboardingStatusBrowserEgressVerified || !got.BrowserEgressVerified {
		t.Fatalf("verified session = %#v", got)
	}
	if proxy.testCalls != 1 || proxy.getCalls != 1 {
		t.Fatalf("proxy calls test=%d get=%d, want test setup once and get once", proxy.testCalls, proxy.getCalls)
	}
	rec, ok := store.get(created.ID)
	if !ok {
		t.Fatalf("stored session missing")
	}
	if rec.BrowserEgressCheckStatus != "verified" || !rec.BrowserVerified || rec.Status != FormalPoolOnboardingStatusBrowserEgressVerified {
		t.Fatalf("stored verification fields = status:%q verified:%v session:%q", rec.BrowserEgressCheckStatus, rec.BrowserVerified, rec.Status)
	}
	assertFormalPoolEgressBucketsRedacted(t, rec, "198.51.100.10")
	if got.BrowserEgressCheckStatus != "verified" {
		t.Fatalf("response browser egress status = %q, want verified", got.BrowserEgressCheckStatus)
	}
	if got.BrowserEgressBrowserIPBucket != rec.BrowserEgressBrowserIPBucket || got.BrowserEgressProxyIPBucket != rec.BrowserEgressProxyIPBucket {
		t.Fatalf("response buckets = %q/%q, want stored %q/%q", got.BrowserEgressBrowserIPBucket, got.BrowserEgressProxyIPBucket, rec.BrowserEgressBrowserIPBucket, rec.BrowserEgressProxyIPBucket)
	}
	if got.BrowserEgressLastErrorCode != "" || got.BrowserEgressMismatchAt != "" {
		t.Fatalf("verified response error/mismatch = %q/%q, want empty", got.BrowserEgressLastErrorCode, got.BrowserEgressMismatchAt)
	}
	assertNoFormalPoolRawIPInValue(t, got, "198.51.100.10")
	assertNoFormalPoolSensitive(t, got)
	if len(risk.verified) != 1 {
		t.Fatalf("verified risk events = %d, want 1", len(risk.verified))
	}
	assertFormalPoolRiskInputSafe(t, risk.verified[0], nonce, "198.51.100.10")
}

func TestFormalPoolVerifyBrowserEgressByNonceMismatchRecordsMismatchAndRedacts(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	store := NewFormalPoolOnboardingStore(30*time.Minute, func() time.Time { return now })
	proxy := &formalPoolBrowserEgressProxyFake{rawIP: "198.51.100.10"}
	risk := &formalPoolRiskCaptureWriter{}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Store: store, Proxy: proxy, Risk: risk})
	created, nonce := formalPoolCreateSessionWithNonce(t, svc, store)

	_, err := svc.VerifyBrowserEgressByNonce(context.Background(), nonce, "203.0.113.44")
	if !errors.Is(err, ErrFormalPoolOnboardingEgressMismatch) {
		t.Fatalf("VerifyBrowserEgressByNonce() error = %v, want mismatch", err)
	}
	rec, ok := store.get(created.ID)
	if !ok {
		t.Fatalf("stored session missing")
	}
	if rec.BrowserEgressCheckStatus != "mismatch" || rec.BrowserVerified || rec.BrowserEgressLastErrorCode != "mismatch" || rec.BrowserEgressMismatchAt.IsZero() {
		t.Fatalf("stored mismatch fields = status:%q verified:%v error:%q mismatch_at:%v", rec.BrowserEgressCheckStatus, rec.BrowserVerified, rec.BrowserEgressLastErrorCode, rec.BrowserEgressMismatchAt)
	}
	assertFormalPoolEgressBucketsRedacted(t, rec, "198.51.100.10", "203.0.113.44")
	got, err := svc.GetSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if got.BrowserEgressCheckStatus != "mismatch" || got.BrowserEgressLastErrorCode != "mismatch" {
		t.Fatalf("mismatch response status/error = %q/%q", got.BrowserEgressCheckStatus, got.BrowserEgressLastErrorCode)
	}
	assertRFC3339UTCString(t, got.BrowserEgressMismatchAt, "mismatch_at")
	if got.BrowserEgressBrowserIPBucket != rec.BrowserEgressBrowserIPBucket || got.BrowserEgressProxyIPBucket != rec.BrowserEgressProxyIPBucket {
		t.Fatalf("mismatch response buckets = %q/%q, want stored %q/%q", got.BrowserEgressBrowserIPBucket, got.BrowserEgressProxyIPBucket, rec.BrowserEgressBrowserIPBucket, rec.BrowserEgressProxyIPBucket)
	}
	assertNoFormalPoolRawIPInValue(t, got, "198.51.100.10", "203.0.113.44")
	assertNoFormalPoolSensitive(t, got)
	if len(risk.mismatch) != 1 {
		t.Fatalf("mismatch risk events = %d, want 1", len(risk.mismatch))
	}
	assertFormalPoolRiskInputSafe(t, risk.mismatch[0], nonce, "198.51.100.10", "203.0.113.44")
}

func TestFormalPoolVerifyBrowserEgressByNonceExpiredWritesExpiredState(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	store := NewFormalPoolOnboardingStore(30*time.Minute, func() time.Time { return now })
	proxy := &formalPoolBrowserEgressProxyFake{rawIP: "198.51.100.10"}
	risk := &formalPoolRiskCaptureWriter{}
	cfg := DefaultFormalPoolConfig()
	cfg.NonceTTL = time.Minute
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Store: store, Proxy: proxy, Risk: risk, Config: cfg})
	created, nonce := formalPoolCreateSessionWithNonce(t, svc, store)
	now = now.Add(2 * time.Minute)

	_, err := svc.VerifyBrowserEgressByNonce(context.Background(), nonce, "198.51.100.10")
	if !errors.Is(err, ErrFormalPoolOnboardingNonceExpired) {
		t.Fatalf("VerifyBrowserEgressByNonce() error = %v, want nonce expired", err)
	}
	if proxy.getCalls != 0 {
		t.Fatalf("GetRawEgressIP calls = %d, want 0 for expired nonce", proxy.getCalls)
	}
	rec, ok := store.get(created.ID)
	if !ok {
		t.Fatalf("stored session missing")
	}
	if rec.BrowserEgressCheckStatus != "expired" || rec.BrowserEgressLastErrorCode != "nonce_expired" {
		t.Fatalf("expired fields = status:%q error:%q", rec.BrowserEgressCheckStatus, rec.BrowserEgressLastErrorCode)
	}
	got, err := svc.GetSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if got.BrowserEgressCheckStatus != "expired" || got.BrowserEgressLastErrorCode != "nonce_expired" {
		t.Fatalf("expired response status/error = %q/%q", got.BrowserEgressCheckStatus, got.BrowserEgressLastErrorCode)
	}
	assertRFC3339UTCString(t, got.NonceExpiresAt, "expired nonce_expires_at")
	assertNoFormalPoolRawIPInValue(t, got, "198.51.100.10")
	assertFormalPoolAdminNonceOnlyInBrowserURL(t, got, nonce)
	assertNoFormalPoolSensitive(t, got)
	if len(risk.expired) != 1 {
		t.Fatalf("expired risk events = %d, want 1", len(risk.expired))
	}
	assertFormalPoolRiskInputSafe(t, risk.expired[0], nonce, "198.51.100.10")
}

func TestFormalPoolVerifyBrowserEgressByNonceProxyRawEgressFailureWritesNoProxyAndDoesNotTestProxy(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	store := NewFormalPoolOnboardingStore(30*time.Minute, func() time.Time { return now })
	probeErr := errors.New("probe failed without raw identifiers")
	proxy := &formalPoolBrowserEgressProxyFake{getErr: probeErr}
	risk := &formalPoolRiskCaptureWriter{}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Store: store, Proxy: proxy, Risk: risk})
	created, nonce := formalPoolCreateSessionWithNonce(t, svc, store)

	_, err := svc.VerifyBrowserEgressByNonce(context.Background(), nonce, "198.51.100.10")
	if !errors.Is(err, probeErr) {
		t.Fatalf("VerifyBrowserEgressByNonce() error = %v, want probe error", err)
	}
	if proxy.testCalls != 1 || proxy.getCalls != 1 {
		t.Fatalf("proxy calls test=%d get=%d, want TestProxy only during setup and one GetRawEgressIP", proxy.testCalls, proxy.getCalls)
	}
	rec, ok := store.get(created.ID)
	if !ok {
		t.Fatalf("stored session missing")
	}
	if rec.BrowserEgressCheckStatus != "waiting" || rec.BrowserEgressLastErrorCode != "no_proxy_egress" {
		t.Fatalf("no proxy fields = status:%q error:%q", rec.BrowserEgressCheckStatus, rec.BrowserEgressLastErrorCode)
	}
	got, err := svc.GetSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if got.BrowserEgressCheckStatus != "waiting" || got.BrowserEgressLastErrorCode != "no_proxy_egress" {
		t.Fatalf("no proxy response status/error = %q/%q", got.BrowserEgressCheckStatus, got.BrowserEgressLastErrorCode)
	}
	assertNoFormalPoolRawIPInValue(t, got, "198.51.100.10")
	assertFormalPoolAdminNonceOnlyInBrowserURL(t, got, nonce)
	assertNoFormalPoolSensitive(t, got)
	if len(risk.noProxy) != 1 {
		t.Fatalf("no proxy risk events = %d, want 1", len(risk.noProxy))
	}
	assertFormalPoolRiskInputSafe(t, risk.noProxy[0], nonce, "198.51.100.10")
}

func TestFormalPoolVerifyBrowserEgressByNonceAlreadyVerifiedIsIdempotent(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	store := NewFormalPoolOnboardingStore(30*time.Minute, func() time.Time { return now })
	proxy := &formalPoolBrowserEgressProxyFake{rawIP: "198.51.100.10"}
	risk := &formalPoolRiskCaptureWriter{}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Store: store, Proxy: proxy, Risk: risk})
	created, nonce := formalPoolCreateSessionWithNonce(t, svc, store)
	first, err := svc.VerifyBrowserEgressByNonce(context.Background(), nonce, "198.51.100.10")
	if err != nil {
		t.Fatalf("first VerifyBrowserEgressByNonce() error = %v", err)
	}
	second, err := svc.VerifyBrowserEgressByNonce(context.Background(), nonce, "203.0.113.44")
	if err != nil {
		t.Fatalf("second VerifyBrowserEgressByNonce() error = %v", err)
	}
	if first.ID != second.ID || second.ID != created.ID || !second.BrowserEgressVerified {
		t.Fatalf("idempotent response first=%#v second=%#v", first, second)
	}
	if proxy.getCalls != 1 || len(risk.verified) != 1 {
		t.Fatalf("idempotent verify called probe/risk again: get=%d risk=%d", proxy.getCalls, len(risk.verified))
	}
}

func formalPoolCreateSessionWithNonce(t *testing.T, svc *FormalPoolOnboardingService, store *FormalPoolOnboardingStore) (*FormalPoolOnboardingSession, string) {
	t.Helper()
	created, err := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(7), GroupID: 42, AccountName: "acct"})
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if _, err := svc.TestProxy(context.Background(), created.ID); err != nil {
		t.Fatalf("TestProxy() error = %v", err)
	}
	rec, ok := store.get(created.ID)
	if !ok {
		t.Fatalf("stored session missing")
	}
	if rec.BrowserNonce == "" {
		t.Fatalf("TestProxy did not mint nonce")
	}
	return created, rec.BrowserNonce
}

func assertFormalPoolEgressBucketsRedacted(t *testing.T, rec *formalPoolOnboardingSessionRecord, rawIPs ...string) {
	t.Helper()
	if !strings.HasPrefix(rec.BrowserEgressBrowserIPBucket, "browser_bucket_") || !strings.HasPrefix(rec.BrowserEgressProxyIPBucket, "proxy_bucket_") {
		t.Fatalf("bucket prefixes = browser:%q proxy:%q", rec.BrowserEgressBrowserIPBucket, rec.BrowserEgressProxyIPBucket)
	}
	for _, rawIP := range rawIPs {
		if rec.BrowserEgressBrowserIPBucket == rawIP || rec.BrowserEgressProxyIPBucket == rawIP || strings.Contains(rec.BrowserEgressBrowserIPBucket, rawIP) || strings.Contains(rec.BrowserEgressProxyIPBucket, rawIP) {
			t.Fatalf("raw IP %q leaked in buckets browser:%q proxy:%q", rawIP, rec.BrowserEgressBrowserIPBucket, rec.BrowserEgressProxyIPBucket)
		}
	}
}

func assertNoFormalPoolRawIPInValue(t *testing.T, v any, rawIPs ...string) {
	t.Helper()
	encoded, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	body := string(encoded)
	for _, rawIP := range rawIPs {
		if strings.Contains(body, rawIP) {
			t.Fatalf("raw IP %q leaked in %s", rawIP, body)
		}
	}
}

func assertFormalPoolAdminNonceOnlyInBrowserURL(t *testing.T, session *FormalPoolOnboardingSession, nonce string) {
	t.Helper()
	if strings.TrimSpace(nonce) == "" {
		t.Fatalf("nonce is empty")
	}
	if !strings.Contains(session.BrowserEgressCheckURL, nonce) {
		t.Fatalf("browser egress URL = %q, want nonce %q", session.BrowserEgressCheckURL, nonce)
	}
	assertFormalPoolJSONDoesNotContainKey(t, session.SafeSummary, "browser_egress_check_url")
	assertFormalPoolJSONDoesNotContainString(t, session.SafeSummary, nonce)

	encoded, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("marshal session: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(encoded, &obj); err != nil {
		t.Fatalf("unmarshal session JSON %s: %v", string(encoded), err)
	}
	delete(obj, "browser_egress_check_url")
	encodedWithoutURL, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("marshal session without browser URL: %v", err)
	}
	if strings.Contains(string(encodedWithoutURL), nonce) {
		t.Fatalf("nonce %q leaked outside browser_egress_check_url in %s", nonce, string(encodedWithoutURL))
	}
}

func assertFormalPoolRiskInputSafe(t *testing.T, input FormalPoolRiskEventInput, rawValues ...string) {
	t.Helper()
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal risk input: %v", err)
	}
	body := string(encoded)
	for _, raw := range rawValues {
		if raw != "" && strings.Contains(body, raw) {
			t.Fatalf("raw value %q leaked in risk input %s", raw, body)
		}
	}
	for _, bucket := range append([]string{input.NonceBucket, input.IPBucket}, input.SafeContextBuckets...) {
		if strings.TrimSpace(bucket) == "" {
			continue
		}
		if !formalPoolSafeBucketAllowed(bucket) {
			t.Fatalf("unsafe risk bucket %q in %#v", bucket, input)
		}
	}
}

func TestFormalPoolOnboardingSessionResponseNormalizesUnsafeEgressErrorCode(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	store := NewFormalPoolOnboardingStore(30*time.Minute, func() time.Time { return now })
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Store: store})
	created, err := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(7), GroupID: 42, AccountName: "acct"})
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	_, err = store.update(created.ID, func(rec *formalPoolOnboardingSessionRecord) error {
		rec.BrowserEgressCheckStatus = "mismatch"
		rec.BrowserEgressLastErrorCode = "raw 198.51.100.10 bearer secret"
		rec.BrowserEgressBrowserIPBucket = "browser_bucket_safe"
		rec.BrowserEgressProxyIPBucket = "proxy_bucket_safe"
		rec.BrowserEgressMismatchAt = now
		return nil
	})
	if err != nil {
		t.Fatalf("seed unsafe error code: %v", err)
	}

	got, err := svc.GetSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if got.BrowserEgressLastErrorCode != "" {
		t.Fatalf("unsafe last error code echoed as %q, want empty", got.BrowserEgressLastErrorCode)
	}
	assertNoFormalPoolRawIPInValue(t, got, "198.51.100.10", "bearer secret")
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
	var gotAuth string
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotToken = r.Header.Get("X-CC-Gateway-Token")
		gotAuth = r.Header.Get("Authorization")
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
				Enabled:              true,
				BaseURL:              server.URL,
				Token:                "gateway-token",
				InternalControlToken: "internal-control-token",
				TimeoutSeconds:       1,
			},
		},
	})
	if registrar == nil {
		t.Fatalf("expected registrar")
	}

	err := registrar.RegisterCCGatewayRuntime(context.Background(), FormalPoolCCGatewayRuntimeRegistration{
		AccountRef:            "hmac-sha256:runtime-account-ref",
		CredentialRef:         "opaque:credential-ref:v1:runtime",
		CredentialBindingHMAC: "hmac-sha256:" + strings.Repeat("a", 64),
		TokenType:             "oauth",
		CredentialProof:       "Bearer fixture-credential-proof",
		EgressBucket:          "claude-runtime-bucket",
		ProxyURL:              "socks5h://user:pass@proxy.example:443",
		ProxyRef:              "hmac-sha256:runtime-proxy-ref",
		PolicyVersion:         ccGatewayAnthropicPolicyVersion,
		PersonaVariant:        "claude-code-2.1.175-macos-local",
		SessionPolicy:         "preserve_downstream_session_id",
		DeviceID:              strings.Repeat("b", 64),
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
	if gotAuth != "Bearer fixture-credential-proof" {
		t.Fatalf("credential proof header missing")
	}
	if got["account_id"] != "hmac-sha256:runtime-account-ref" ||
		got["account_ref"] != "hmac-sha256:runtime-account-ref" ||
		got["credential_ref"] != "opaque:credential-ref:v1:runtime" ||
		got["credential_binding_hmac"] != "hmac-sha256:"+strings.Repeat("a", 64) ||
		got["token_type"] != "oauth" ||
		got["egress_bucket"] != "claude-runtime-bucket" ||
		got["proxy_url"] != "socks5h://user:pass@proxy.example:443" ||
		got["proxy_identity_ref"] != "hmac-sha256:runtime-proxy-ref" ||
		got["policy_version"] != ccGatewayAnthropicPolicyVersion ||
		got["session_policy"] != "preserve_downstream_session_id" ||
		got["device_id"] != strings.Repeat("b", 64) {
		t.Fatalf("unexpected registration payload: %#v", got)
	}
	rawPayload, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal captured payload: %v", err)
	}
	if strings.Contains(string(rawPayload), "fixture-credential-proof") {
		t.Fatalf("registration payload must not contain credential proof")
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
				Enabled:              true,
				BaseURL:              server.URL,
				Token:                "gateway-token",
				InternalControlToken: "internal-control-token",
				TimeoutSeconds:       1,
			},
		},
	})
	err := registrar.RegisterCCGatewayRuntime(context.Background(), FormalPoolCCGatewayRuntimeRegistration{
		AccountRef:            "hmac-sha256:runtime-account-ref",
		CredentialRef:         "opaque:credential-ref:v1:runtime",
		CredentialBindingHMAC: "hmac-sha256:" + strings.Repeat("a", 64),
		TokenType:             "oauth",
		CredentialProof:       "Bearer fixture-credential-proof",
		EgressBucket:          "claude-runtime-bucket",
		ProxyURL:              "socks5h://proxy.example:443",
		ProxyRef:              "hmac-sha256:runtime-proxy-ref",
		PolicyVersion:         ccGatewayAnthropicPolicyVersion,
		PersonaVariant:        "claude-code-2.1.175-macos-local",
		SessionPolicy:         "preserve_downstream_session_id",
		DeviceID:              strings.Repeat("b", 64),
	})
	if err == nil || !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("expected fail-closed gateway status error, got %v", err)
	}
}

func TestFormalPoolRuntimeIdentityExtraLeavesCredentialBindingEmptyWhenAccessTokenMissing(t *testing.T) {
	extra := formalPoolRuntimeIdentityExtra(
		"hmac-sha256:"+strings.Repeat("a", 64),
		"hmac-sha256:"+strings.Repeat("b", 64),
		map[string]any{"refresh_token": "refresh-only"},
		"formal-pool-runtime-binding-local-test-secret",
		"1",
	)

	if got := extra[ccGatewayExtraCredentialBindingHMAC]; got != "" {
		t.Fatalf("credential binding HMAC must be empty without selected access token, got %q", got)
	}
}

func TestFormalPoolRuntimeIdentityExtraDefaultsToPrimary2197Persona(t *testing.T) {
	extra := formalPoolRuntimeIdentityExtra(
		"hmac-sha256:"+strings.Repeat("a", 64),
		"hmac-sha256:"+strings.Repeat("b", 64),
		map[string]any{"access_token": "access-token"},
		"formal-pool-runtime-binding-local-test-secret",
		"1",
	)

	if got := extra[ccGatewayExtraPersonaProfile]; got != ccGateway2197PersonaProfile {
		t.Fatalf("persona profile = %v, want primary 2.1.197 canonical", got)
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

func assertFormalPoolJSONContains(t *testing.T, v any, key, want string) {
	t.Helper()
	encoded, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(encoded, &obj); err != nil {
		t.Fatalf("unmarshal JSON %s: %v", string(encoded), err)
	}
	if got, ok := obj[key].(string); !ok || got != want {
		t.Fatalf("JSON %s = %#v, want %q in %s", key, obj[key], want, string(encoded))
	}
}

func assertFormalPoolJSONDoesNotContainKey(t *testing.T, v any, key string) {
	t.Helper()
	encoded, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(encoded, &obj); err != nil {
		t.Fatalf("unmarshal JSON %s: %v", string(encoded), err)
	}
	if _, ok := obj[key]; ok {
		t.Fatalf("JSON unexpectedly contains %s in %s", key, string(encoded))
	}
}

func assertFormalPoolJSONDoesNotContainString(t *testing.T, v any, forbidden string) {
	t.Helper()
	encoded, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	if strings.Contains(string(encoded), forbidden) {
		t.Fatalf("JSON unexpectedly contains %q in %s", forbidden, string(encoded))
	}
}

func assertRFC3339UTCString(t *testing.T, got, label string) {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, got)
	if err != nil {
		t.Fatalf("%s = %q, want RFC3339: %v", label, got, err)
	}
	if parsed.Location() != time.UTC {
		t.Fatalf("%s = %q, want UTC Z timestamp", label, got)
	}
}

func formalPtrInt64(v int64) *int64 { return &v }

func assertNoFormalPoolSensitive(t *testing.T, v any) {
	t.Helper()
	if path := FormalPoolSensitivePathForTest(v); path != "" {
		t.Fatalf("sensitive data found at %s in %#v", path, v)
	}
}
