//go:build phase0red

package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

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

func TestFormalPoolOnboardingAuthorizationRejectsCrossBoundaryOperations(t *testing.T) {
	gin.SetMode(gin.TestMode)

	operations := []phase0Operation{
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

	for _, operation := range operations {
		operation := operation
		t.Run(operation.name, func(t *testing.T) {
			t.Run("owner positive", func(t *testing.T) {
				fixture := newPhase0AuthorizationFixture(t, operation.stage)
				rec := fixture.request(phase0Owner, operation.method, operation.path(fixture), operation.body, `"`+strconv.FormatInt(fixture.version, 10)+`"`)
				require.Equal(t, http.StatusOK, rec.Code, "owner fixture must reach and complete the operation; body=%s", rec.Body.String())
			})

			t.Run("different authenticated principal only", func(t *testing.T) {
				fixture := newPhase0AuthorizationFixture(t, operation.stage)
				other := phase0Owner
				other.subjectID++
				rec := fixture.request(other, operation.method, operation.path(fixture), operation.body, `"`+strconv.FormatInt(fixture.version, 10)+`"`)
				requireAuthorizationDenial(t, rec, http.StatusForbidden)
			})
		})
	}
}

func TestFormalPoolOnboardingAuthorizationDimensionsAreIndependent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("session creation owner positive", func(t *testing.T) {
		fixture := newPhase0AuthorizationFixture(t, "none")
		rec := fixture.request(phase0Owner, http.MethodPost, "/api/v1/admin/claude-onboarding/sessions", phase0CreateBody(phase0Owner.groupID), `"0"`)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})

	creationCases := []struct {
		name       string
		principal  func() phase0OnboardingPrincipal
		groupID    int64
		wantStatus int
	}{
		{name: "missing authenticated principal only", principal: func() phase0OnboardingPrincipal { p := phase0Owner; p.authenticated = false; return p }, groupID: phase0Owner.groupID, wantStatus: http.StatusUnauthorized},
		{name: "non-administrator role only", principal: func() phase0OnboardingPrincipal { p := phase0Owner; p.role = service.RoleUser; return p }, groupID: phase0Owner.groupID, wantStatus: http.StatusForbidden},
		{name: "different group only", principal: func() phase0OnboardingPrincipal { return phase0Owner }, groupID: phase0Owner.groupID + 1, wantStatus: http.StatusForbidden},
	}
	for _, tc := range creationCases {
		tc := tc
		t.Run("session creation "+tc.name, func(t *testing.T) {
			fixture := newPhase0AuthorizationFixture(t, "none")
			rec := fixture.request(tc.principal(), http.MethodPost, "/api/v1/admin/claude-onboarding/sessions", phase0CreateBody(tc.groupID), `"0"`)
			requireAuthorizationDenial(t, rec, tc.wantStatus)
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
		{name: "different group only", mutate: func(p *phase0OnboardingPrincipal) { p.groupID++ }, wantStatus: http.StatusForbidden},
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
		svc := service.NewFormalPoolOnboardingService(service.FormalPoolOnboardingDeps{
			Proxy: &formalPoolOnboardingRoutesProxy{rawIP: "198.51.100.10"}, PublicURLPrefix: "https://public.example.test",
		})
		router := newFormalPoolOnboardingRoutesRouter(adminhandler.NewFormalPoolOnboardingHandler(svc), func(c *gin.Context) { c.Next() })
		rec := phase0CreateAndTestProxyWithOrigin(t, router, "hostile-host.example.invalid", "hostile-forwarded.example.invalid", "http")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Contains(t, rec.Body.String(), `"browser_egress_check_url":"https://public.example.test/api/v1/claude-onboarding/browser-egress-check/`)
		require.NotContains(t, rec.Body.String(), "hostile-")
	})

	for _, tc := range []struct {
		name, host, forwardedHost, forwardedProto string
	}{
		{name: "host is untrusted", host: "hostile-host.example.invalid"},
		{name: "forwarded host is untrusted", host: "service.internal", forwardedHost: "hostile-forwarded.example.invalid"},
		{name: "forwarded proto is untrusted", host: "service.internal", forwardedProto: "https"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			svc := service.NewFormalPoolOnboardingService(service.FormalPoolOnboardingDeps{Proxy: &formalPoolOnboardingRoutesProxy{rawIP: "198.51.100.10"}})
			router := newFormalPoolOnboardingRoutesRouter(adminhandler.NewFormalPoolOnboardingHandler(svc), func(c *gin.Context) { c.Next() })
			rec := phase0CreateAndTestProxyWithOrigin(t, router, tc.host, tc.forwardedHost, tc.forwardedProto)
			requireRejectedOrNonAuthoritativeURL(t, rec)
		})
	}
}

type phase0AuthorizationFixture struct {
	router    *gin.Engine
	svc       *service.FormalPoolOnboardingService
	sessionID string
	accountID int64
	version   int64
}

func newPhase0AuthorizationFixture(t *testing.T, stage string) *phase0AuthorizationFixture {
	t.Helper()
	accounts := &phase0AccountReader{}
	oauth := &phase0OAuthFake{fullScope: true}
	runtime := &phase0RuntimeFake{}
	svc := service.NewFormalPoolOnboardingService(service.FormalPoolOnboardingDeps{
		Proxy:            &phase0ProxyFake{},
		OAuth:            oauth,
		Refresh:          oauth,
		Accounts:         accounts,
		CCGateway:        phase0CCGatewayFake{},
		CCGatewayRuntime: runtime,
		Acceptance:       phase0AcceptanceFake{},
		Healthcheck:      phase0HealthcheckFake{},
	})
	fixture := &phase0AuthorizationFixture{svc: svc}
	fixture.router = newFormalPoolOnboardingRoutesRouter(adminhandler.NewFormalPoolOnboardingHandler(svc), phase0PrincipalMiddleware)
	if stage == "none" {
		return fixture
	}

	created, err := svc.StartSession(context.Background(), service.FormalPoolOnboardingStartRequest{
		ProxyMode: "existing", ProxyID: phase0Int64Ptr(7), GroupID: phase0Owner.groupID, AccountName: "owner-account",
	})
	require.NoError(t, err)
	fixture.sessionID = created.ID

	advance := func(call func() (*service.FormalPoolOnboardingSession, error)) {
		result, err := call()
		require.NoError(t, err)
		fixture.version++
		if result.AccountID > 0 {
			fixture.accountID = result.AccountID
		}
	}
	fixture.version = 1
	if stage == "created" {
		return fixture
	}
	advance(func() (*service.FormalPoolOnboardingSession, error) {
		return svc.TestProxy(context.Background(), fixture.sessionID)
	})
	if stage == "proxy-tested" || stage == "setup-token-ready" {
		if stage == "setup-token-ready" {
			oauth.fullScope = false
		}
		return fixture
	}
	advance(func() (*service.FormalPoolOnboardingSession, error) {
		return svc.AttestBrowserEgress(context.Background(), fixture.sessionID, service.FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: "owner-proof"})
	})
	if stage == "attested" {
		return fixture
	}
	advance(func() (*service.FormalPoolOnboardingSession, error) {
		return svc.GenerateAuthURL(context.Background(), fixture.sessionID)
	})
	if stage == "oauth-url" {
		return fixture
	}
	advance(func() (*service.FormalPoolOnboardingSession, error) {
		return svc.ExchangeCodeAndCreate(context.Background(), fixture.sessionID, service.FormalPoolExchangeCodeAndCreateRequest{Code: "owner-code"})
	})
	if stage == "imported" {
		return fixture
	}
	advance(func() (*service.FormalPoolOnboardingSession, error) {
		return svc.RefreshOnly(context.Background(), fixture.sessionID)
	})
	if stage == "refreshed" {
		return fixture
	}
	advance(func() (*service.FormalPoolOnboardingSession, error) {
		return svc.RegisterRuntime(context.Background(), fixture.sessionID)
	})
	_, err = svc.RunAcceptance(context.Background(), fixture.sessionID)
	require.NoError(t, err)
	fixture.version++
	if stage == "accepted" {
		return fixture
	}
	advance(func() (*service.FormalPoolOnboardingSession, error) {
		return svc.StartWarming(context.Background(), fixture.sessionID)
	})
	require.Equal(t, "warming", stage)
	return fixture
}

func (f *phase0AuthorizationFixture) request(principal phase0OnboardingPrincipal, method, path, body, expectedVersion string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if principal.authenticated {
		req.Header.Set("X-Phase0-Subject", strconv.FormatInt(principal.subjectID, 10))
	}
	req.Header.Set("X-Phase0-Admin", strconv.FormatInt(principal.adminID, 10))
	req.Header.Set("X-Phase0-Role", principal.role)
	req.Header.Set("X-Phase0-Tenant", principal.tenantID)
	req.Header.Set("X-Phase0-Group", strconv.FormatInt(principal.groupID, 10))
	req.Header.Set("X-Phase0-Creator", strconv.FormatInt(principal.creatorID, 10))
	req.Header.Set("If-Match", expectedVersion)
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func phase0PrincipalMiddleware(c *gin.Context) {
	if id, err := strconv.ParseInt(c.GetHeader("X-Phase0-Subject"), 10, 64); err == nil && id > 0 {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: id})
	}
	c.Set(string(middleware.ContextKeyUserRole), c.GetHeader("X-Phase0-Role"))
	c.Set("phase0_admin_id", c.GetHeader("X-Phase0-Admin"))
	c.Set("phase0_tenant_id", c.GetHeader("X-Phase0-Tenant"))
	c.Set("phase0_group_id", c.GetHeader("X-Phase0-Group"))
	c.Set("phase0_creator_id", c.GetHeader("X-Phase0-Creator"))
	c.Next()
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
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusOK, createRec.Code, createRec.Body.String())
	sessionID := extractFormalPoolOnboardingSessionID(t, createRec.Body.String())

	testReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/claude-onboarding/sessions/"+sessionID+"/test-proxy", nil)
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

func requireRejectedOrNonAuthoritativeURL(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code < http.StatusOK || rec.Code >= http.StatusMultipleChoices {
		return
	}
	var envelope struct {
		Data struct {
			BrowserURL string `json:"browser_egress_check_url"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope), rec.Body.String())
	require.NotEmpty(t, envelope.Data.BrowserURL, "a successful response must expose the browser URL")
	parsed, err := url.Parse(envelope.Data.BrowserURL)
	require.NoError(t, err)
	require.False(t, parsed.IsAbs(), "without configured origin or trusted ingress, a successful result must remain relative: %s", envelope.Data.BrowserURL)
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
