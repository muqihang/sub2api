package routes

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ihandler "github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFormalPoolOnboardingAdminRoutesDeriveAbsoluteBrowserEgressURLFromForwardedHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := service.NewFormalPoolOnboardingService(formalPoolOnboardingRoutesDeps(service.FormalPoolOnboardingDeps{Proxy: &formalPoolOnboardingRoutesProxy{rawIP: "198.51.100.10"}}))
	router := newFormalPoolOnboardingRoutesRouter(adminhandler.NewFormalPoolOnboardingHandler(svc), func(c *gin.Context) { c.Next() })

	body := bytes.NewBufferString(`{"proxy_mode":"existing","proxy_id":7,"group_id":42,"account_name":"acct"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/claude-onboarding/sessions", body)
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("If-Match", `"0"`)
	createReq.Header.Set("Idempotency-Key", "routes-create-stable-key")
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusOK, createRec.Code, createRec.Body.String())
	sessionID := extractFormalPoolOnboardingSessionID(t, createRec.Body.String())

	testReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/claude-onboarding/sessions/"+sessionID+"/test-proxy", nil)
	testReq.Header.Set("If-Match", `"2"`)
	testReq.Header.Set("X-Forwarded-Proto", "https")
	testReq.Header.Set("X-Forwarded-Host", "admin.example.test")
	testRec := httptest.NewRecorder()
	router.ServeHTTP(testRec, testReq)

	require.Equal(t, http.StatusOK, testRec.Code, testRec.Body.String())
	require.Contains(t, testRec.Body.String(), `"browser_egress_check_url":"https://admin.example.test/api/v1/claude-onboarding/browser-egress-check/`)
}

func extractFormalPoolOnboardingSessionID(t *testing.T, body string) string {
	t.Helper()
	marker := `"id":"`
	idx := strings.Index(body, marker)
	require.NotEqual(t, -1, idx, body)
	start := idx + len(marker)
	end := strings.Index(body[start:], `"`)
	require.NotEqual(t, -1, end, body)
	return body[start : start+end]
}

func TestFormalPoolOnboardingRoutes_AdminAndPublicBrowserEgress(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")
	svc := service.NewFormalPoolOnboardingService(formalPoolOnboardingRoutesDeps(service.FormalPoolOnboardingDeps{}))
	h := &ihandler.Handlers{Admin: &ihandler.AdminHandlers{FormalPoolOnboarding: adminhandler.NewFormalPoolOnboardingHandler(svc)}}
	adminAuthCalls := 0

	RegisterFormalPoolOnboardingPublicRoutes(v1, h)
	RegisterFormalPoolOnboardingAdminRoutes(v1, h, middleware.FormalPoolOnboardingJWTAuthMiddleware(func(c *gin.Context) {
		adminAuthCalls++
		c.Next()
	}), formalPoolOnboardingRoutesPrincipalResolver{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/claude-onboarding/browser-egress-check/bad-nonce", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"ok":false,"message":"Browser egress check received."}`, rec.Body.String())
	require.NotContains(t, rec.Body.String(), "FORMAL_POOL_ONBOARDING_NOT_FOUND")
	require.NotContains(t, rec.Body.String(), "bad-nonce")
	require.Equal(t, 0, adminAuthCalls, "browser egress check must not require admin session")

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/claude-onboarding/sessions", bytes.NewBufferString(`{}`))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.NotEqual(t, http.StatusNotFound, rec.Code)
	require.Equal(t, 1, adminAuthCalls, "mutating onboarding session routes must remain admin protected")

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/claude-onboarding/sessions/fpo_test/setup-token-cookie-auth-and-create", bytes.NewBufferString(`{"session_key":"sk-ant-sid02-test"}`))
	req.Header.Set("If-Match", `"1"`)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, 2, adminAuthCalls, "setup-token onboarding route must remain admin protected")
	require.Contains(t, rec.Body.String(), "FORMAL_POOL_ONBOARDING_NOT_FOUND", "registered route should reach onboarding service")
	require.NotContains(t, rec.Body.String(), "sk-ant-sid02-test", "route errors must not echo setup-token login state")
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/claude-onboarding/sessions/fpo_test/healthcheck", nil)
	req.Header.Set("If-Match", `"1"`)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, 3, adminAuthCalls, "healthcheck route must remain admin protected")
	require.Contains(t, rec.Body.String(), "FORMAL_POOL_ONBOARDING_NOT_FOUND")

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/claude-onboarding/accounts/2/healthcheck", nil)
	req.Header.Set("If-Match", `"1"`)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, 4, adminAuthCalls, "account-level healthcheck route must remain admin protected")
	require.Contains(t, rec.Body.String(), "FORMAL_POOL_ONBOARDING_NOT_FOUND", "account-level healthcheck must resolve its owning onboarding session")

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/claude-onboarding/sessions/fpo_test/promote-production", nil)
	req.Header.Set("If-Match", `"1"`)
	req.Header.Set("Idempotency-Key", "routes-promote-stable-key")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, 5, adminAuthCalls, "production promotion route must remain admin protected")
	require.Contains(t, rec.Body.String(), "FORMAL_POOL_ONBOARDING_NOT_FOUND")

}

func TestFormalPoolOnboardingBrowserEgressPublicRouteSafeFailureBodiesAreEqual(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	store := service.NewFormalPoolOnboardingStore(30*time.Minute, func() time.Time { return now })
	proxy := &formalPoolOnboardingRoutesProxy{rawIP: "198.51.100.10"}
	cfg := service.DefaultFormalPoolConfig()
	cfg.NonceTTL = time.Minute
	svc := service.NewFormalPoolOnboardingService(formalPoolOnboardingRoutesDeps(service.FormalPoolOnboardingDeps{Store: store, Proxy: proxy, Config: cfg}))
	router := newFormalPoolOnboardingRoutesRouter(adminhandler.NewFormalPoolOnboardingHandler(svc), nil)

	mismatchNonce := createFormalPoolOnboardingRoutesNonce(t, svc)
	expiredNonce := createFormalPoolOnboardingRoutesNonce(t, svc)

	unknown := performFormalPoolOnboardingRoutesRequest(router, http.MethodGet, "/api/v1/claude-onboarding/browser-egress-check/raw-unknown-nonce", "203.0.113.44")
	mismatch := performFormalPoolOnboardingRoutesRequest(router, http.MethodGet, "/api/v1/claude-onboarding/browser-egress-check/"+mismatchNonce, "203.0.113.44")
	now = now.Add(2 * time.Minute)
	expired := performFormalPoolOnboardingRoutesRequest(router, http.MethodGet, "/api/v1/claude-onboarding/browser-egress-check/"+expiredNonce, "198.51.100.10")

	for _, rec := range []*httptest.ResponseRecorder{unknown, mismatch, expired} {
		require.Equal(t, http.StatusOK, rec.Code)
		require.JSONEq(t, `{"ok":false,"message":"Browser egress check received."}`, rec.Body.String())
		body := rec.Body.String()
		require.NotContains(t, body, "raw-unknown-nonce")
		require.NotContains(t, body, mismatchNonce)
		require.NotContains(t, body, expiredNonce)
		require.NotContains(t, body, "203.0.113.44")
		require.NotContains(t, body, "198.51.100.10")
		require.NotContains(t, body, "FORMAL_POOL_ONBOARDING_NOT_FOUND")
		require.NotContains(t, body, "FORMAL_POOL_ONBOARDING_NONCE_EXPIRED")
		require.NotContains(t, body, "FORMAL_POOL_ONBOARDING_EGRESS_MISMATCH")
	}
	require.Equal(t, unknown.Body.String(), mismatch.Body.String())
	require.Equal(t, unknown.Body.String(), expired.Body.String())
}

func TestFormalPoolOnboardingBrowserEgressPublicRouteLimiterDeniedRecordsSafeRisk(t *testing.T) {
	gin.SetMode(gin.TestMode)
	limiter := &formalPoolOnboardingRoutesLimiter{decision: service.FormalPoolEgressRateLimitDecision{
		Allowed: false, NonceBucket: "nonce_bucket_safe1234", IPBucket: "ip_bucket_safe5678", Reason: "per_ip",
	}}
	risk := &formalPoolOnboardingRoutesRiskWriter{}
	svc := service.NewFormalPoolOnboardingService(formalPoolOnboardingRoutesDeps(service.FormalPoolOnboardingDeps{}))
	h := adminhandler.NewFormalPoolOnboardingHandlerWithPublicDeps(svc, limiter, risk)
	router := newFormalPoolOnboardingRoutesRouter(h, nil)

	rec := performFormalPoolOnboardingRoutesRequest(router, http.MethodGet, "/api/v1/claude-onboarding/browser-egress-check/raw-denied-nonce", "198.51.100.99")

	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.JSONEq(t, `{"ok":false}`, rec.Body.String())
	require.Len(t, risk.rateLimited, 1)
	require.Equal(t, "nonce_bucket_safe1234", risk.rateLimited[0].nonceBucket)
	require.Equal(t, "ip_bucket_safe5678", risk.rateLimited[0].ipBucket)
	require.Equal(t, "per_ip", risk.rateLimited[0].reason)
	require.NotContains(t, rec.Body.String(), "raw-denied-nonce")
	require.NotContains(t, rec.Body.String(), "198.51.100.99")
}

func TestFormalPoolOnboardingBrowserEgressPublicRouteSuccessOnlyReturnsOK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := service.NewFormalPoolOnboardingService(formalPoolOnboardingRoutesDeps(service.FormalPoolOnboardingDeps{Proxy: &formalPoolOnboardingRoutesProxy{rawIP: "198.51.100.10"}}))
	nonce := createFormalPoolOnboardingRoutesNonce(t, svc)
	router := newFormalPoolOnboardingRoutesRouter(adminhandler.NewFormalPoolOnboardingHandler(svc), nil)

	rec := performFormalPoolOnboardingRoutesRequest(router, http.MethodGet, "/api/v1/claude-onboarding/browser-egress-check/"+nonce, "198.51.100.10")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"ok":true}`, rec.Body.String())
	body := strings.ToLower(rec.Body.String())
	for _, forbidden := range []string{"session", "id", "safe_summary", "proxy", "account", "browser_egress_check_url", nonce} {
		require.NotContains(t, body, strings.ToLower(forbidden))
	}
}

func TestFormalPoolOnboardingBrowserEgressPublicRouteAppliesConstantDelay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	minDelay := 20 * time.Millisecond

	newDelayedService := func(deps service.FormalPoolOnboardingDeps) *service.FormalPoolOnboardingService {
		cfg := service.DefaultFormalPoolConfig()
		cfg.PublicRouteConstantDelayMin = minDelay
		cfg.PublicRouteConstantDelayMax = minDelay
		deps.Config = cfg
		deps = formalPoolOnboardingRoutesDeps(deps)
		return service.NewFormalPoolOnboardingService(deps)
	}

	assertDelayed := func(t *testing.T, name string, router *gin.Engine, path string, remoteIP string, wantStatus int) {
		t.Helper()
		started := time.Now()
		rec := performFormalPoolOnboardingRoutesRequest(router, http.MethodGet, path, remoteIP)
		elapsed := time.Since(started)
		require.Equal(t, wantStatus, rec.Code, name)
		require.GreaterOrEqual(t, elapsed, minDelay, "%s route returned before configured constant delay", name)
	}

	successSvc := newDelayedService(service.FormalPoolOnboardingDeps{Proxy: &formalPoolOnboardingRoutesProxy{rawIP: "198.51.100.10"}})
	successNonce := createFormalPoolOnboardingRoutesNonce(t, successSvc)
	successRouter := newFormalPoolOnboardingRoutesRouter(adminhandler.NewFormalPoolOnboardingHandler(successSvc), nil)
	assertDelayed(t, "success", successRouter, "/api/v1/claude-onboarding/browser-egress-check/"+successNonce, "198.51.100.10", http.StatusOK)

	failureSvc := newDelayedService(service.FormalPoolOnboardingDeps{})
	failureRouter := newFormalPoolOnboardingRoutesRouter(adminhandler.NewFormalPoolOnboardingHandler(failureSvc), nil)
	assertDelayed(t, "safe failure", failureRouter, "/api/v1/claude-onboarding/browser-egress-check/raw-unknown-nonce", "203.0.113.44", http.StatusOK)

	limiter := &formalPoolOnboardingRoutesLimiter{decision: service.FormalPoolEgressRateLimitDecision{Allowed: false, Reason: "per_ip"}}
	rateLimitedRouter := newFormalPoolOnboardingRoutesRouter(adminhandler.NewFormalPoolOnboardingHandlerWithPublicDeps(failureSvc, limiter, nil), nil)
	assertDelayed(t, "rate limit", rateLimitedRouter, "/api/v1/claude-onboarding/browser-egress-check/raw-denied-nonce", "198.51.100.99", http.StatusTooManyRequests)
}

func newFormalPoolOnboardingRoutesRouter(h *adminhandler.FormalPoolOnboardingHandler, adminAuth gin.HandlerFunc) *gin.Engine {
	router := gin.New()
	v1 := router.Group("/api/v1")
	handlers := &ihandler.Handlers{Admin: &ihandler.AdminHandlers{FormalPoolOnboarding: h}}
	RegisterFormalPoolOnboardingPublicRoutes(v1, handlers)
	if adminAuth != nil {
		RegisterFormalPoolOnboardingAdminRoutes(v1, handlers, middleware.FormalPoolOnboardingJWTAuthMiddleware(adminAuth), formalPoolOnboardingRoutesPrincipalResolver{}, nil)
	}
	return router
}

func performFormalPoolOnboardingRoutesRequest(router *gin.Engine, method, path, remoteIP string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if strings.TrimSpace(remoteIP) != "" {
		req.RemoteAddr = fmt.Sprintf("%s:1234", remoteIP)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func createFormalPoolOnboardingRoutesNonce(t *testing.T, svc *service.FormalPoolOnboardingService) string {
	t.Helper()
	version := int64(0)
	ctx := service.WithFormalPoolRequestAuthority(context.Background(), service.FormalPoolRequestAuthority{
		Principal: formalPoolOnboardingRoutesPrincipal(), ExpectedVersion: &version, IdempotencyKey: "routes-direct-create-key",
	})
	created, err := svc.StartSession(ctx, service.FormalPoolOnboardingStartRequest{
		ProxyMode: "existing", ProxyID: formalPoolOnboardingRoutesPtrInt64(7), GroupID: 42, AccountName: "acct",
	})
	require.NoError(t, err)
	version = created.Version
	ctx = service.WithFormalPoolRequestAuthority(context.Background(), service.FormalPoolRequestAuthority{
		Principal: formalPoolOnboardingRoutesPrincipal(), ExpectedVersion: &version,
	})
	tested, err := svc.TestProxy(ctx, created.ID)
	require.NoError(t, err)
	parts := strings.Split(strings.TrimRight(tested.BrowserEgressCheckURL, "/"), "/")
	nonce := parts[len(parts)-1]
	require.NotEmpty(t, nonce)
	return nonce
}

func formalPoolOnboardingRoutesPtrInt64(v int64) *int64 { return &v }

type formalPoolOnboardingRoutesProxy struct{ rawIP string }

type formalPoolOnboardingRoutesPrincipalResolver struct{}

func (formalPoolOnboardingRoutesPrincipalResolver) Resolve(*gin.Context) (service.FormalPoolOnboardingPrincipal, error) {
	return formalPoolOnboardingRoutesPrincipal(), nil
}

type formalPoolOnboardingRoutesPrincipalRevalidator struct{}

func (formalPoolOnboardingRoutesPrincipalRevalidator) Revalidate(context.Context, service.FormalPoolOnboardingPrincipal) error {
	return nil
}

type formalPoolOnboardingRoutesGroupReader struct{}

func (formalPoolOnboardingRoutesGroupReader) GetByID(_ context.Context, id int64) (*service.Group, error) {
	return &service.Group{ID: id, Status: service.StatusActive}, nil
}

func formalPoolOnboardingRoutesPrincipal() service.FormalPoolOnboardingPrincipal {
	return service.FormalPoolOnboardingPrincipal{
		SubjectID: 1, AdministratorID: 1, TenantID: "routes-tenant", CreatorID: 1,
		Role: service.RoleAdmin, CallerKind: service.CallerKindHumanJWT, AuthorityRevision: 1,
		ExpiresAtUnix: time.Now().Add(time.Hour).Unix(), Active: true, SystemAdmin: true,
	}
}

func formalPoolOnboardingRoutesDeps(deps service.FormalPoolOnboardingDeps) service.FormalPoolOnboardingDeps {
	deps.Groups = formalPoolOnboardingRoutesGroupReader{}
	deps.PrincipalRevalidator = formalPoolOnboardingRoutesPrincipalRevalidator{}
	return deps
}

func (p *formalPoolOnboardingRoutesProxy) ResolveOrCreateProxy(ctx context.Context, req service.FormalPoolOnboardingStartRequest) (service.FormalPoolProxyResolution, error) {
	id := int64(7)
	if req.ProxyID != nil {
		id = *req.ProxyID
	}
	return service.FormalPoolProxyResolution{ProxyID: id, ProxyRef: "proxy_ref_safe", NormalizedProxyURL: "socks5h://proxy.local:1080"}, nil
}

func (p *formalPoolOnboardingRoutesProxy) TestProxy(ctx context.Context, proxyID int64) (service.FormalPoolProxyTestSummary, error) {
	return service.FormalPoolProxyTestSummary{Success: true, ProxyRef: "proxy_ref_safe", ExitIPRef: "exit_ip_safe", LatencyBucket: "lt_500ms"}, nil
}

func (p *formalPoolOnboardingRoutesProxy) GetRawEgressIP(ctx context.Context, proxyID int64, normalizedProxyURL string) (string, error) {
	if strings.TrimSpace(p.rawIP) == "" {
		return "198.51.100.10", nil
	}
	return p.rawIP, nil
}

type formalPoolOnboardingRoutesLimiter struct {
	decision service.FormalPoolEgressRateLimitDecision
}

func (l *formalPoolOnboardingRoutesLimiter) CheckEgressCheck(ctx context.Context, nonce, ip string) service.FormalPoolEgressRateLimitDecision {
	return l.decision
}

type formalPoolOnboardingRoutesRiskWriter struct {
	rateLimited []struct{ nonceBucket, ipBucket, reason string }
}

func (w *formalPoolOnboardingRoutesRiskWriter) RecordEgressVerified(ctx context.Context, input service.FormalPoolRiskEventInput) error {
	return nil
}
func (w *formalPoolOnboardingRoutesRiskWriter) RecordEgressMismatch(ctx context.Context, input service.FormalPoolRiskEventInput) error {
	return nil
}
func (w *formalPoolOnboardingRoutesRiskWriter) RecordNonceExpired(ctx context.Context, input service.FormalPoolRiskEventInput) error {
	return nil
}
func (w *formalPoolOnboardingRoutesRiskWriter) RecordEgressNoProxy(ctx context.Context, input service.FormalPoolRiskEventInput) error {
	return nil
}
func (w *formalPoolOnboardingRoutesRiskWriter) RecordPublicRouteRateLimited(ctx context.Context, input service.FormalPoolRiskEventInput) error {
	w.rateLimited = append(w.rateLimited, struct{ nonceBucket, ipBucket, reason string }{input.NonceBucket, input.IPBucket, input.SafeReasonCode})
	return nil
}
func (w *formalPoolOnboardingRoutesRiskWriter) RecordPublicRouteRateLimitedBuckets(ctx context.Context, nonceBucket, ipBucket, reason string) error {
	w.rateLimited = append(w.rateLimited, struct{ nonceBucket, ipBucket, reason string }{nonceBucket, ipBucket, reason})
	return nil
}
