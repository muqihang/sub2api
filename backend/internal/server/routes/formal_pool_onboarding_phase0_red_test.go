package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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

type phase0OnboardingPrincipal struct {
	authenticated bool
	subjectID     int64
	adminID       int64
	tenantID      string
	groupID       int64
	creatorID     int64
	role          string
}

var phase0Owner = phase0OnboardingPrincipal{
	authenticated: true,
	subjectID:     1001,
	adminID:       1101,
	tenantID:      "tenant-one",
	groupID:       101,
	creatorID:     1001,
	role:          service.RoleAdmin,
}

type phase0Operation struct {
	name   string
	method string
	path   func(*phase0AuthorizationFixture) string
	body   string
	stage  string
}

var formalPoolAdminOperationCases = []phase0Operation{
	{name: "GetSession", method: http.MethodGet, path: phase0SessionPath(""), stage: "created"},
	{name: "TestProxy", method: http.MethodPost, path: phase0SessionPath("/test-proxy"), stage: "created"},
	{name: "BrowserEgressAttestation", method: http.MethodPost, path: phase0SessionPath("/browser-egress-attestation"), body: `{"confirmed":true,"verification_code":"owner-proof"}`, stage: "proxy-tested"},
	{name: "GenerateOAuth", method: http.MethodPost, path: phase0SessionPath("/generate-auth-url"), stage: "attested"},
	{name: "ExchangeOAuth", method: http.MethodPost, path: phase0SessionPath("/exchange-code-and-create"), body: `{"code":"owner-code"}`, stage: "oauth-url"},
	{name: "ExchangeSetupToken", method: http.MethodPost, path: phase0SessionPath("/setup-token-cookie-auth-and-create"), body: `{"session_key":"owner-session-key"}`, stage: "setup-token-ready"},
	{name: "Acceptance", method: http.MethodPost, path: phase0SessionPath("/acceptance"), stage: "imported"},
	{name: "RefreshOnly", method: http.MethodPost, path: phase0SessionPath("/refresh-only"), stage: "imported"},
	{name: "RuntimeRegistration", method: http.MethodPost, path: phase0SessionPath("/runtime-register"), stage: "refreshed"},
	{name: "SessionHealthcheck", method: http.MethodPost, path: phase0SessionPath("/healthcheck"), stage: "imported"},
	{name: "AccountHealthcheck", method: http.MethodPost, path: func(f *phase0AuthorizationFixture) string {
		return "/api/v1/admin/claude-onboarding/accounts/" + strconv.FormatInt(f.accountID, 10) + "/healthcheck"
	}, stage: "imported"},
	{name: "StartWarming", method: http.MethodPost, path: phase0SessionPath("/start-warming"), stage: "accepted"},
	{name: "Abort", method: http.MethodPost, path: phase0SessionPath("/abort"), stage: "created"},
	{name: "Activation", method: http.MethodPost, path: phase0SessionPath("/activate"), stage: "accepted"},
	{name: "Promotion", method: http.MethodPost, path: phase0SessionPath("/promote-production"), stage: "warming"},
}

type formalPoolAuthorityCase struct {
	name           string
	principal      func() phase0OnboardingPrincipal
	resolverErr    error
	revalidatorErr error
	wantStatus     int
	staleVersion   bool
}

func phase0Principal(mutators ...func(*phase0OnboardingPrincipal)) func() phase0OnboardingPrincipal {
	return func() phase0OnboardingPrincipal {
		principal := phase0Owner
		for _, mutate := range mutators {
			mutate(&principal)
		}
		return principal
	}
}

var formalPoolAuthorityCases = []formalPoolAuthorityCase{
	{name: "authorized system administrator owner", principal: phase0Principal(), wantStatus: http.StatusOK},
	{name: "unauthenticated", principal: phase0Principal(), resolverErr: service.ErrFormalPoolOnboardingAuthenticationRequired, wantStatus: http.StatusUnauthorized},
	{name: "ordinary user creator", principal: phase0Principal(func(p *phase0OnboardingPrincipal) { p.role = service.RoleUser }), wantStatus: http.StatusForbidden},
	{name: "ordinary user non creator", principal: phase0Principal(func(p *phase0OnboardingPrincipal) { p.role = service.RoleUser; p.creatorID++ }), wantStatus: http.StatusForbidden},
	{name: "would-be group administrator allowed same group", principal: phase0Principal(func(p *phase0OnboardingPrincipal) { p.role = service.RoleUser }), wantStatus: http.StatusForbidden},
	{name: "would-be group administrator cross group", principal: phase0Principal(func(p *phase0OnboardingPrincipal) { p.role = service.RoleUser; p.groupID++ }), wantStatus: http.StatusForbidden},
	{name: "would-be group administrator cross tenant", principal: phase0Principal(func(p *phase0OnboardingPrincipal) { p.role = service.RoleUser; p.tenantID = "tenant-two" }), wantStatus: http.StatusForbidden},
	{name: "would-be tenant administrator same tenant label", principal: phase0Principal(func(p *phase0OnboardingPrincipal) { p.role = service.RoleUser }), wantStatus: http.StatusForbidden},
	{name: "would-be tenant administrator cross tenant label", principal: phase0Principal(func(p *phase0OnboardingPrincipal) { p.role = service.RoleUser; p.tenantID = "tenant-two" }), wantStatus: http.StatusForbidden},
	{name: "initially revoked JWT", principal: phase0Principal(), resolverErr: service.ErrFormalPoolOnboardingAuthenticationRequired, wantStatus: http.StatusUnauthorized},
	{name: "initially expired JWT", principal: phase0Principal(), resolverErr: service.ErrFormalPoolOnboardingAuthenticationRequired, wantStatus: http.StatusUnauthorized},
	{name: "service caller admin API key", principal: phase0Principal(), resolverErr: service.ErrFormalPoolOnboardingAuthenticationRequired, wantStatus: http.StatusUnauthorized},
	{name: "post-guard inactive or token drift", principal: phase0Principal(), revalidatorErr: service.ErrFormalPoolOnboardingAuthenticationRequired, wantStatus: http.StatusUnauthorized},
	{name: "post-guard role loss", principal: phase0Principal(), revalidatorErr: service.ErrFormalPoolOnboardingForbidden, wantStatus: http.StatusForbidden},
	{name: "stale browser tab", principal: phase0Principal(), wantStatus: http.StatusConflict, staleVersion: true},
}

func TestFormalPoolOnboardingAuthorizationRejectsCrossBoundaryOperations(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.Len(t, formalPoolAdminOperationCases, 15)
	require.Len(t, formalPoolAuthorityCases, 15)

	for _, operation := range formalPoolAdminOperationCases {
		operation := operation
		for _, authorityCase := range formalPoolAuthorityCases {
			authorityCase := authorityCase
			t.Run(operation.name+"/"+authorityCase.name, func(t *testing.T) {
				fixture := newPhase0AuthorizationFixture(t, operation.stage)
				fixture.resolver.err = authorityCase.resolverErr
				fixture.revalidator.err = authorityCase.revalidatorErr
				version := fixture.version
				wantStatus := authorityCase.wantStatus
				if operation.method == http.MethodGet && authorityCase.staleVersion {
					wantStatus = http.StatusOK
				} else if authorityCase.staleVersion {
					version = 0
				}
				body := operation.body
				if operation.name == "BrowserEgressAttestation" {
					body = `{"confirmed":true,"verification_code":` + strconv.Quote(fixture.browserProof) + `}`
				}
				rec := fixture.request(authorityCase.principal(), operation.method, operation.path(fixture), body, `"`+strconv.FormatInt(version, 10)+`"`)
				require.Equal(t, wantStatus, rec.Code, rec.Body.String())
			})
		}
	}
}

func TestFormalPoolOnboardingAuthorizationDimensionsAreIndependent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("session creation owner positive", func(t *testing.T) {
		fixture := newPhase0AuthorizationFixture(t, "none")
		rec := fixture.request(phase0Owner, http.MethodPost, "/api/v1/admin/claude-onboarding/sessions", phase0CreateBody(phase0Owner.groupID), `"0"`)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})

	for _, tc := range formalPoolAuthorityCases {
		tc := tc
		t.Run("CreateSession/"+tc.name, func(t *testing.T) {
			fixture := newPhase0AuthorizationFixture(t, "none")
			fixture.resolver.err = tc.resolverErr
			fixture.revalidator.err = tc.revalidatorErr
			wantStatus := tc.wantStatus
			if tc.staleVersion {
				wantStatus = http.StatusOK
			}
			rec := fixture.request(tc.principal(), http.MethodPost, "/api/v1/admin/claude-onboarding/sessions", phase0CreateBody(phase0Owner.groupID), `"0"`)
			require.Equal(t, wantStatus, rec.Code, rec.Body.String())
		})
	}

	dimensionCases := []struct {
		name       string
		mutate     func(*phase0OnboardingPrincipal)
		wantStatus int
	}{
		{name: "missing authenticated principal only", mutate: func(p *phase0OnboardingPrincipal) { p.authenticated = false }, wantStatus: http.StatusUnauthorized},
		{name: "different administrator only", mutate: func(p *phase0OnboardingPrincipal) { p.adminID++ }, wantStatus: http.StatusForbidden},
		{name: "different tenant only", mutate: func(p *phase0OnboardingPrincipal) { p.tenantID = "tenant-two" }, wantStatus: http.StatusForbidden},
		{name: "different creator owner only", mutate: func(p *phase0OnboardingPrincipal) { p.creatorID++ }, wantStatus: http.StatusForbidden},
		{name: "non-administrator role only", mutate: func(p *phase0OnboardingPrincipal) { p.role = service.RoleUser }, wantStatus: http.StatusForbidden},
	}
	for _, tc := range dimensionCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			fixture := newPhase0AuthorizationFixture(t, "created")
			principal := phase0Owner
			tc.mutate(&principal)
			rec := fixture.request(principal, http.MethodGet, phase0SessionPath("")(fixture), "", `"`+strconv.FormatInt(fixture.version, 10)+`"`)
			requireAuthorizationDenial(t, rec, tc.wantStatus)
		})
	}

	t.Run("valid owner wrong current state only", func(t *testing.T) {
		fixture := newPhase0AuthorizationFixture(t, "created")
		rec := fixture.request(phase0Owner, http.MethodPost, phase0SessionPath("/activate")(fixture), "", `"`+strconv.FormatInt(fixture.version, 10)+`"`)
		requireStateOrVersionDenial(t, rec, "wrong state")
	})

	t.Run("valid owner stale expected version only", func(t *testing.T) {
		fixture := newPhase0AuthorizationFixture(t, "accepted")
		rec := fixture.request(phase0Owner, http.MethodPost, phase0SessionPath("/activate")(fixture), "", `"0"`)
		requireStateOrVersionDenial(t, rec, "stale version")
	})
}

func TestFormalPoolOnboardingPublicOriginAuthority(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("configured public origin remains authoritative", func(t *testing.T) {
		svc := service.NewFormalPoolOnboardingService(formalPoolOnboardingRoutesDeps(service.FormalPoolOnboardingDeps{
			Proxy: &formalPoolOnboardingRoutesProxy{rawIP: "198.51.100.10"}, PublicURLPrefix: "https://public.example.test",
		}))
		router := newFormalPoolOnboardingRoutesRouter(adminhandler.NewFormalPoolOnboardingHandler(svc), func(c *gin.Context) { c.Next() })
		rec := phase0CreateAndTestProxyWithOrigin(t, router, "hostile-host.example.invalid", "hostile-forwarded.example.invalid", "http")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Contains(t, rec.Body.String(), `"browser_egress_check_url":"https://public.example.test/api/v1/claude-onboarding/browser-egress-check/`)
		require.NotContains(t, rec.Body.String(), "hostile-")
	})

	for _, tc := range []struct {
		name   string
		mutate func(*http.Request)
	}{
		{name: "host is untrusted", mutate: func(req *http.Request) { req.Host = "hostile-host.example.invalid" }},
		{name: "forwarded is untrusted", mutate: func(req *http.Request) {
			req.Header.Set("Forwarded", `for=192.0.2.44;host=hostile-forwarded.example.invalid;proto=http`)
		}},
		{name: "forwarded host is untrusted", mutate: func(req *http.Request) { req.Header.Set("X-Forwarded-Host", "hostile-forwarded.example.invalid") }},
		{name: "forwarded proto is untrusted", mutate: func(req *http.Request) { req.Header.Set("X-Forwarded-Proto", "http") }},
		{name: "forwarded scheme is untrusted", mutate: func(req *http.Request) { req.Header.Set("X-Forwarded-Scheme", "http") }},
		{name: "forwarded ssl is untrusted", mutate: func(req *http.Request) { req.Header.Set("X-Forwarded-Ssl", "on") }},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			svc := service.NewFormalPoolOnboardingService(formalPoolOnboardingRoutesDeps(service.FormalPoolOnboardingDeps{Proxy: &formalPoolOnboardingRoutesProxy{rawIP: "198.51.100.10"}}))
			router := newFormalPoolOnboardingRoutesRouter(adminhandler.NewFormalPoolOnboardingHandler(svc), func(c *gin.Context) { c.Next() })
			testProxyRec := phase0CreateAndTestProxyWithOrigin(t, router, "service.internal", "", "")
			sessionID := extractFormalPoolOnboardingSessionID(t, testProxyRec.Body.String())
			baseline := phase0BrowserURL(t, phase0GetSessionWithRequestMutation(t, router, sessionID, nil))
			mutated := phase0BrowserURL(t, phase0GetSessionWithRequestMutation(t, router, sessionID, tc.mutate))
			require.Equal(t, baseline, mutated, "request-derived authority must not change a stored browser URL")
			for _, browserURL := range []string{baseline, mutated} {
				require.True(t, strings.HasPrefix(browserURL, "/api/v1/claude-onboarding/browser-egress-check/"),
					"without configured origin the browser URL must remain relative across request mutations: %q", browserURL)
				require.NotContains(t, browserURL, "://")
				require.NotContains(t, browserURL, "hostile-")
			}
		})
	}
}

func phase0GetSessionWithRequestMutation(t *testing.T, router *gin.Engine, sessionID string, mutate func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/claude-onboarding/sessions/"+sessionID, nil)
	req.Host = "service.internal"
	if mutate != nil {
		mutate(req)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

type phase0AuthorizationFixture struct {
	router       *gin.Engine
	svc          *service.FormalPoolOnboardingService
	resolver     *phase0PrincipalResolver
	revalidator  *phase0PrincipalRevalidator
	sessionID    string
	accountID    int64
	version      int64
	browserProof string
}

func newPhase0AuthorizationFixture(t *testing.T, stage string) *phase0AuthorizationFixture {
	t.Helper()
	accounts := &phase0AccountReader{}
	oauth := &phase0OAuthFake{fullScope: true}
	runtime := &phase0RuntimeFake{}
	revalidator := &phase0PrincipalRevalidator{}
	svc := service.NewFormalPoolOnboardingService(service.FormalPoolOnboardingDeps{
		Proxy:                &phase0ProxyFake{},
		OAuth:                oauth,
		Refresh:              oauth,
		Accounts:             accounts,
		CCGateway:            phase0CCGatewayFake{},
		CCGatewayRuntime:     runtime,
		Acceptance:           phase0AcceptanceFake{},
		Healthcheck:          phase0HealthcheckFake{},
		Groups:               phase0GroupReader{},
		PrincipalRevalidator: revalidator,
	})
	resolver := &phase0PrincipalResolver{principal: phase0Owner}
	fixture := &phase0AuthorizationFixture{svc: svc, resolver: resolver, revalidator: revalidator}
	fixture.router = newPhase0FormalPoolOnboardingRoutesRouter(adminhandler.NewFormalPoolOnboardingHandler(svc), resolver)
	if stage == "none" {
		return fixture
	}

	created, err := svc.StartSession(phase0AuthorityContext(phase0Owner, 0), service.FormalPoolOnboardingStartRequest{
		ProxyMode: "existing", ProxyID: phase0Int64Ptr(7), GroupID: phase0Owner.groupID, AccountName: "owner-account",
	})
	require.NoError(t, err)
	fixture.sessionID = created.ID
	fixture.version = created.Version

	advance := func(call func() (*service.FormalPoolOnboardingSession, error)) {
		result, err := call()
		require.NoError(t, err)
		fixture.version = result.Version
		if result.AccountID > 0 {
			fixture.accountID = result.AccountID
		}
	}
	if stage == "created" {
		return fixture
	}
	var tested *service.FormalPoolOnboardingSession
	advance(func() (*service.FormalPoolOnboardingSession, error) {
		var testErr error
		tested, testErr = svc.TestProxy(phase0AuthorityContext(phase0Owner, fixture.version), fixture.sessionID)
		return tested, testErr
	})
	parts := strings.Split(strings.TrimRight(tested.BrowserEgressCheckURL, "/"), "/")
	fixture.browserProof = parts[len(parts)-1]
	observed, err := svc.VerifyBrowserEgressByNonce(context.Background(), fixture.browserProof, "198.51.100.10")
	require.NoError(t, err)
	fixture.version = observed.Version
	if stage == "proxy-tested" || stage == "setup-token-ready" {
		if stage == "setup-token-ready" {
			oauth.fullScope = false
		}
		return fixture
	}
	advance(func() (*service.FormalPoolOnboardingSession, error) {
		return svc.AttestBrowserEgress(phase0AuthorityContext(phase0Owner, fixture.version), fixture.sessionID, service.FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: fixture.browserProof})
	})
	if stage == "attested" {
		return fixture
	}
	advance(func() (*service.FormalPoolOnboardingSession, error) {
		return svc.GenerateAuthURL(phase0AuthorityContext(phase0Owner, fixture.version), fixture.sessionID)
	})
	if stage == "oauth-url" {
		return fixture
	}
	advance(func() (*service.FormalPoolOnboardingSession, error) {
		return svc.ExchangeCodeAndCreate(phase0AuthorityContext(phase0Owner, fixture.version), fixture.sessionID, service.FormalPoolExchangeCodeAndCreateRequest{Code: "owner-code"})
	})
	if stage == "imported" {
		return fixture
	}
	advance(func() (*service.FormalPoolOnboardingSession, error) {
		return svc.RefreshOnly(phase0AuthorityContext(phase0Owner, fixture.version), fixture.sessionID)
	})
	if stage == "refreshed" {
		return fixture
	}
	advance(func() (*service.FormalPoolOnboardingSession, error) {
		return svc.RegisterRuntime(phase0AuthorityContext(phase0Owner, fixture.version), fixture.sessionID)
	})
	accepted, err := svc.RunAcceptance(phase0AuthorityContext(phase0Owner, fixture.version), fixture.sessionID)
	require.NoError(t, err)
	fixture.version = accepted.Version
	if stage == "accepted" {
		return fixture
	}
	advance(func() (*service.FormalPoolOnboardingSession, error) {
		return svc.StartWarming(phase0AuthorityContext(phase0Owner, fixture.version), fixture.sessionID)
	})
	require.Equal(t, "warming", stage)
	return fixture
}

func (f *phase0AuthorizationFixture) request(principal phase0OnboardingPrincipal, method, path, body, expectedVersion string) *httptest.ResponseRecorder {
	f.resolver.principal = principal
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", expectedVersion)
	idempotencyKey := "phase0-stable-operation-key"
	if strings.HasSuffix(path, "/promote-production") {
		idempotencyKey = "phase0-promote-operation-key"
	}
	req.Header.Set("Idempotency-Key", idempotencyKey)
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

type phase0PrincipalResolver struct {
	principal phase0OnboardingPrincipal
	err       error
}

func (r *phase0PrincipalResolver) Resolve(*gin.Context) (service.FormalPoolOnboardingPrincipal, error) {
	if r != nil && r.err != nil {
		return service.FormalPoolOnboardingPrincipal{}, r.err
	}
	if r == nil || !r.principal.authenticated {
		return service.FormalPoolOnboardingPrincipal{}, service.ErrFormalPoolOnboardingAuthenticationRequired
	}
	if r.principal.role != service.RoleAdmin {
		return service.FormalPoolOnboardingPrincipal{}, service.ErrFormalPoolOnboardingForbidden
	}
	return phase0ServicePrincipal(r.principal), nil
}

type phase0PrincipalRevalidator struct{ err error }

func (r *phase0PrincipalRevalidator) Revalidate(context.Context, service.FormalPoolOnboardingPrincipal) error {
	return r.err
}

type phase0GroupReader struct{}

func (phase0GroupReader) GetByID(_ context.Context, id int64) (*service.Group, error) {
	return &service.Group{ID: id, Status: service.StatusActive}, nil
}

func phase0ServicePrincipal(principal phase0OnboardingPrincipal) service.FormalPoolOnboardingPrincipal {
	return service.FormalPoolOnboardingPrincipal{
		SubjectID: principal.subjectID, AdministratorID: principal.adminID, TenantID: principal.tenantID,
		CreatorID: principal.creatorID, Role: principal.role, CallerKind: service.CallerKindHumanJWT,
		AuthorityRevision: 1, ExpiresAtUnix: time.Now().Add(time.Hour).Unix(), Active: true, SystemAdmin: principal.role == service.RoleAdmin,
	}
}

func phase0AuthorityContext(principal phase0OnboardingPrincipal, version int64) context.Context {
	return service.WithFormalPoolRequestAuthority(context.Background(), service.FormalPoolRequestAuthority{
		Principal: phase0ServicePrincipal(principal), ExpectedVersion: &version, IdempotencyKey: "phase0-stable-operation-key",
	})
}

func newPhase0FormalPoolOnboardingRoutesRouter(h *adminhandler.FormalPoolOnboardingHandler, resolver adminhandler.FormalPoolOnboardingPrincipalResolver) *gin.Engine {
	router := gin.New()
	v1 := router.Group("/api/v1")
	handlers := &ihandler.Handlers{Admin: &ihandler.AdminHandlers{FormalPoolOnboarding: h}}
	RegisterFormalPoolOnboardingAdminRoutes(v1, handlers, middleware.FormalPoolOnboardingJWTAuthMiddleware(func(c *gin.Context) { c.Next() }), resolver, nil)
	return router
}

func phase0SessionPath(suffix string) func(*phase0AuthorizationFixture) string {
	return func(f *phase0AuthorizationFixture) string {
		return "/api/v1/admin/claude-onboarding/sessions/" + f.sessionID + suffix
	}
}

func phase0CreateBody(groupID int64) string {
	return `{"proxy_mode":"existing","proxy_id":7,"group_id":` + strconv.FormatInt(groupID, 10) + `,"account_name":"owner-account"}`
}

func requireAuthorizationDenial(t *testing.T, rec *httptest.ResponseRecorder, status int) {
	t.Helper()
	require.Equal(t, status, rec.Code, "authorization must deny before lookup, state, version, or dependency handling; body=%s", rec.Body.String())
	require.NotContains(t, strings.ToLower(rec.Body.String()), "invalid state")
	require.NotContains(t, strings.ToLower(rec.Body.String()), "version conflict")
}

func requireStateOrVersionDenial(t *testing.T, rec *httptest.ResponseRecorder, dimension string) {
	t.Helper()
	require.NotEqual(t, http.StatusUnauthorized, rec.Code, "%s is an owner request, not an authentication failure", dimension)
	require.NotEqual(t, http.StatusForbidden, rec.Code, "%s is an owner request, not an authorization failure", dimension)
	require.True(t, rec.Code == http.StatusBadRequest || rec.Code == http.StatusConflict,
		"%s must be rejected as a state/version conflict; status=%d body=%s", dimension, rec.Code, rec.Body.String())
}

func phase0CreateAndTestProxyWithOrigin(t *testing.T, router *gin.Engine, host, forwardedHost, forwardedProto string) *httptest.ResponseRecorder {
	t.Helper()
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/claude-onboarding/sessions", bytes.NewBufferString(phase0CreateBody(101)))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("If-Match", `"0"`)
	createReq.Header.Set("Idempotency-Key", "phase0-origin-create-key")
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusOK, createRec.Code, createRec.Body.String())
	sessionID := extractFormalPoolOnboardingSessionID(t, createRec.Body.String())

	testReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/claude-onboarding/sessions/"+sessionID+"/test-proxy", nil)
	testReq.Header.Set("If-Match", `"2"`)
	if host != "" {
		testReq.Host = host
	}
	if forwardedHost != "" {
		testReq.Header.Set("X-Forwarded-Host", forwardedHost)
	}
	if forwardedProto != "" {
		testReq.Header.Set("X-Forwarded-Proto", forwardedProto)
	}
	testRec := httptest.NewRecorder()
	router.ServeHTTP(testRec, testReq)
	return testRec
}

func phase0BrowserURL(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	require.GreaterOrEqual(t, rec.Code, http.StatusOK, rec.Body.String())
	require.Less(t, rec.Code, http.StatusMultipleChoices, rec.Body.String())
	var envelope struct {
		Data struct {
			BrowserURL string `json:"browser_egress_check_url"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope), rec.Body.String())
	require.NotEmpty(t, envelope.Data.BrowserURL, "a successful response must expose the browser URL")
	return envelope.Data.BrowserURL
}

type phase0ProxyFake struct{}

func (*phase0ProxyFake) ResolveOrCreateProxy(_ context.Context, req service.FormalPoolOnboardingStartRequest) (service.FormalPoolProxyResolution, error) {
	id := int64(7)
	if req.ProxyID != nil {
		id = *req.ProxyID
	}
	return service.FormalPoolProxyResolution{ProxyID: id, ProxyRef: "hmac-sha256:" + strings.Repeat("1", 64), NormalizedProxyURL: "socks5h://proxy.local:1080"}, nil
}

func (*phase0ProxyFake) TestProxy(context.Context, int64) (service.FormalPoolProxyTestSummary, error) {
	return service.FormalPoolProxyTestSummary{Success: true, ProxyRef: "hmac-sha256:" + strings.Repeat("1", 64), ExitIPRef: "hmac-sha256:" + strings.Repeat("2", 64), LatencyBucket: "lt_500ms"}, nil
}

func (*phase0ProxyFake) GetRawEgressIP(context.Context, int64, string) (string, error) {
	return "198.51.100.10", nil
}

type phase0OAuthFake struct{ fullScope bool }

func (*phase0OAuthFake) GenerateFormalAuthURL(context.Context, int64) (service.FormalPoolOAuthURL, error) {
	return service.FormalPoolOAuthURL{AuthURL: "https://oauth.example.test/authorize", SessionID: "owner-oauth-session"}, nil
}

func (f *phase0OAuthFake) credentials() (service.FormalPoolOAuthTokenSummary, map[string]any) {
	scope := "user:inference"
	if f.fullScope {
		scope += " user:sessions:claude_code"
	}
	return service.FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ScopeContainsClaudeCode: f.fullScope, ExpiresInBucket: "gt_1h"}, map[string]any{"access_token": "owner-access", "refresh_token": "owner-refresh", "scope": scope}
}

func (f *phase0OAuthFake) ExchangeCode(context.Context, string, string, int64) (service.FormalPoolOAuthTokenSummary, map[string]any, error) {
	summary, credentials := f.credentials()
	return summary, credentials, nil
}

func (f *phase0OAuthFake) SetupTokenCookieAuth(context.Context, string, int64) (service.FormalPoolOAuthTokenSummary, map[string]any, error) {
	summary, credentials := f.credentials()
	return summary, credentials, nil
}

func (f *phase0OAuthFake) RefreshFormalPoolAccount(context.Context, *service.Account) (service.FormalPoolOAuthTokenSummary, map[string]any, error) {
	summary, credentials := f.credentials()
	return summary, credentials, nil
}

type phase0AccountReader struct{ account *service.Account }

func (f *phase0AccountReader) CreateFormalPoolAccount(_ context.Context, input service.FormalPoolAccountCreateInput) (*service.Account, error) {
	f.account = &service.Account{ID: 123, Name: input.Name, Platform: service.PlatformAnthropic, Type: input.Type, Status: service.StatusActive, Schedulable: input.Schedulable, ProxyID: &input.ProxyID, Concurrency: input.Concurrency, Credentials: input.Credentials, Extra: input.Extra, GroupIDs: []int64{phase0Owner.groupID}}
	f.account.Extra["phase0_owner_subject_id"] = strconv.FormatInt(phase0Owner.subjectID, 10)
	f.account.Extra["phase0_owner_admin_id"] = strconv.FormatInt(phase0Owner.adminID, 10)
	f.account.Extra["phase0_owner_tenant_id"] = phase0Owner.tenantID
	return f.account, nil
}

func (f *phase0AccountReader) GetFormalPoolAccount(_ context.Context, id int64) (*service.Account, error) {
	if f.account == nil || f.account.ID != id {
		return nil, service.ErrAccountNotFound
	}
	return f.account, nil
}

func (f *phase0AccountReader) UpdateFormalPoolAccountCredentials(_ context.Context, _ int64, credentials map[string]any) (*service.Account, error) {
	f.account.Credentials = credentials
	return f.account, nil
}

func (f *phase0AccountReader) UpdateFormalPoolAccountState(_ context.Context, _ int64, schedulable bool, status string, extra map[string]any) (*service.Account, error) {
	f.account.Schedulable = schedulable
	if status != "" {
		f.account.Status = status
	}
	if f.account.Extra == nil {
		f.account.Extra = map[string]any{}
	}
	for key, value := range extra {
		f.account.Extra[key] = value
	}
	return f.account, nil
}

func (f *phase0AccountReader) ActivateFormalPoolAccount(ctx context.Context, id int64, extra map[string]any) (*service.Account, error) {
	return f.UpdateFormalPoolAccountState(ctx, id, true, service.StatusActive, extra)
}

type phase0RuntimeFake struct{}

func (*phase0RuntimeFake) RegisterCCGatewayRuntime(context.Context, service.FormalPoolCCGatewayRuntimeRegistration) error {
	return nil
}

type phase0CCGatewayFake struct{}

func (phase0CCGatewayFake) VerifyCCGatewayReadiness(context.Context, service.FormalPoolAcceptanceInput) ([]service.FormalPoolAcceptanceCheck, error) {
	return []service.FormalPoolAcceptanceCheck{{Name: "cc_gateway_bucket", Status: "pass"}}, nil
}

type phase0AcceptanceFake struct{}

func (phase0AcceptanceFake) RunAcceptance(context.Context, service.FormalPoolAcceptanceInput) (*service.FormalPoolAcceptanceResult, error) {
	return phase0AcceptanceResult(), nil
}

type phase0HealthcheckFake struct{}

func (phase0HealthcheckFake) RunHealthcheck(context.Context, service.FormalPoolAcceptanceInput) (*service.FormalPoolAcceptanceResult, error) {
	return phase0AcceptanceResult(), nil
}

func phase0AcceptanceResult() *service.FormalPoolAcceptanceResult {
	return &service.FormalPoolAcceptanceResult{Status: service.FormalPoolOnboardingStatusHealthcheckPassed, Checks: []service.FormalPoolAcceptanceCheck{{Name: "directed_healthcheck", Status: "pass"}}, ActivationRequired: true, StatusCodeBucket: "status_2xx", CCGatewaySeen: true, RawCapturePresent: true, RawCaptureRef: "hmac-sha256:" + strings.Repeat("8", 64)}
}

func phase0Int64Ptr(value int64) *int64 { return &value }
