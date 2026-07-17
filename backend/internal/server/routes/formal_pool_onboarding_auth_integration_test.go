package routes

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	ihandler "github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type formalPoolProductionRouteOperation struct {
	name   string
	method string
	stage  string
	body   string
	path   func(*formalPoolProductionRouteFixture) string
}

var formalPoolProductionRouteOperations = []formalPoolProductionRouteOperation{
	{name: "CreateSession", method: http.MethodPost, stage: "none", body: phase0CreateBody(phase0Owner.groupID), path: func(*formalPoolProductionRouteFixture) string { return "/api/v1/admin/claude-onboarding/sessions" }},
	{name: "GetSession", method: http.MethodGet, stage: "created", path: formalPoolProductionSessionPath("")},
	{name: "TestProxy", method: http.MethodPost, stage: "created", path: formalPoolProductionSessionPath("/test-proxy")},
	{name: "BrowserEgressAttestation", method: http.MethodPost, stage: "proxy-tested", body: `{"confirmed":true,"verification_code":"owner-proof"}`, path: formalPoolProductionSessionPath("/browser-egress-attestation")},
	{name: "GenerateOAuth", method: http.MethodPost, stage: "attested", path: formalPoolProductionSessionPath("/generate-auth-url")},
	{name: "ExchangeOAuth", method: http.MethodPost, stage: "oauth-url", body: `{"code":"owner-code"}`, path: formalPoolProductionSessionPath("/exchange-code-and-create")},
	{name: "ExchangeSetupToken", method: http.MethodPost, stage: "setup-token-ready", body: `{"session_key":"owner-session-key"}`, path: formalPoolProductionSessionPath("/setup-token-cookie-auth-and-create")},
	{name: "Acceptance", method: http.MethodPost, stage: "imported", path: formalPoolProductionSessionPath("/acceptance")},
	{name: "RefreshOnly", method: http.MethodPost, stage: "imported", path: formalPoolProductionSessionPath("/refresh-only")},
	{name: "RuntimeRegistration", method: http.MethodPost, stage: "refreshed", path: formalPoolProductionSessionPath("/runtime-register")},
	{name: "SessionHealthcheck", method: http.MethodPost, stage: "imported", path: formalPoolProductionSessionPath("/healthcheck")},
	{name: "AccountHealthcheck", method: http.MethodPost, stage: "imported", path: func(f *formalPoolProductionRouteFixture) string {
		return "/api/v1/admin/claude-onboarding/accounts/" + strconv.FormatInt(f.accountID, 10) + "/healthcheck"
	}},
	{name: "StartWarming", method: http.MethodPost, stage: "accepted", path: formalPoolProductionSessionPath("/start-warming")},
	{name: "Abort", method: http.MethodPost, stage: "created", path: formalPoolProductionSessionPath("/abort")},
	{name: "Activation", method: http.MethodPost, stage: "accepted", path: formalPoolProductionSessionPath("/activate")},
	{name: "Promotion", method: http.MethodPost, stage: "warming", path: formalPoolProductionSessionPath("/promote-production")},
}

func TestFormalPoolOnboardingProductionRouteAuthenticationAndCompliance(t *testing.T) {
	t.Run("acknowledged system admin reaches stored-principal handler", func(t *testing.T) {
		fixture := newFormalPoolProductionRouteFixture(t, "created", true, "tenant-one")
		rec := fixture.request(http.MethodGet, formalPoolProductionSessionPath("")(fixture), "", fixture.token, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, 1, fixture.revalidator.calls)
	})

	t.Run("unacknowledged system admin receives exact compliance lock", func(t *testing.T) {
		fixture := newFormalPoolProductionRouteFixture(t, "created", false, "tenant-one")
		rec := fixture.request(http.MethodGet, formalPoolProductionSessionPath("")(fixture), "", fixture.token, "")
		require.Equal(t, http.StatusLocked, rec.Code, rec.Body.String())
		require.Contains(t, rec.Body.String(), "ADMIN_COMPLIANCE_ACK_REQUIRED")
		require.Equal(t, 0, fixture.revalidator.calls)
		require.Equal(t, 0, fixture.dependencies.calls)
	})

	for _, acknowledged := range []bool{false, true} {
		acknowledged := acknowledged
		t.Run("ordinary JWT with group and request labels is forbidden before compliance acknowledged="+strconv.FormatBool(acknowledged), func(t *testing.T) {
			fixture := newFormalPoolProductionRouteFixture(t, "none", acknowledged, "tenant-one")
			fixture.userRepo.user.Role = service.RoleUser
			fixture.userRepo.user.AllowedGroups = []int64{phase0Owner.groupID}
			fixture.token = fixture.generateToken(t)
			rec := fixture.request(http.MethodPost, "/api/v1/admin/claude-onboarding/sessions", phase0CreateBody(phase0Owner.groupID), fixture.token, "same-tenant-cross-group")
			requireFormalPoolProductionDenial(t, fixture, rec, http.StatusForbidden, "FORMAL_POOL_FORBIDDEN", 0)
		})
	}

	for _, tc := range []struct {
		name   string
		mutate func(*formalPoolProductionRouteFixture)
	}{
		{name: "revoked JWT", mutate: func(f *formalPoolProductionRouteFixture) { f.userRepo.user.TokenVersion++ }},
		{name: "inactive JWT", mutate: func(f *formalPoolProductionRouteFixture) { f.userRepo.user.Status = service.StatusDisabled }},
		{name: "deleted JWT", mutate: func(f *formalPoolProductionRouteFixture) { f.userRepo.markDeleted() }},
		{name: "expired JWT", mutate: func(f *formalPoolProductionRouteFixture) { f.token = f.generateExpiredToken(t) }},
	} {
		tc := tc
		t.Run(tc.name+" receives common 401", func(t *testing.T) {
			fixture := newFormalPoolProductionRouteFixture(t, "none", true, "tenant-one")
			tc.mutate(fixture)
			rec := fixture.request(http.MethodPost, "/api/v1/admin/claude-onboarding/sessions", phase0CreateBody(phase0Owner.groupID), fixture.token, "")
			requireFormalPoolProductionDenial(t, fixture, rec, http.StatusUnauthorized, "FORMAL_POOL_AUTH_REQUIRED", 0)
		})
	}

	t.Run("Admin API Key does not authenticate", func(t *testing.T) {
		fixture := newFormalPoolProductionRouteFixture(t, "none", true, "tenant-one")
		rec := fixture.requestWithAdminAPIKey(http.MethodPost, "/api/v1/admin/claude-onboarding/sessions", phase0CreateBody(phase0Owner.groupID))
		requireFormalPoolProductionDenial(t, fixture, rec, http.StatusUnauthorized, "FORMAL_POOL_AUTH_REQUIRED", 0)
	})

	t.Run("empty configured tenant denies before compliance and service", func(t *testing.T) {
		fixture := newFormalPoolProductionRouteFixture(t, "none", false, "")
		rec := fixture.request(http.MethodPost, "/api/v1/admin/claude-onboarding/sessions", phase0CreateBody(phase0Owner.groupID), fixture.token, "")
		requireFormalPoolProductionDenial(t, fixture, rec, http.StatusForbidden, "FORMAL_POOL_FORBIDDEN", 0)
	})

	for _, tc := range formalPoolProductionAuthorityMutations() {
		tc := tc
		t.Run("durable "+tc.name+" after JWT before principal guard", func(t *testing.T) {
			fixture := newFormalPoolProductionRouteFixture(t, "none", true, "tenant-one")
			fixture.userRepo.afterSnapshot = func(call int, user *service.User) {
				if call == 1 {
					tc.mutate(user)
				}
			}
			rec := fixture.request(http.MethodPost, "/api/v1/admin/claude-onboarding/sessions", phase0CreateBody(phase0Owner.groupID), fixture.token, "")
			requireFormalPoolProductionDenial(t, fixture, rec, tc.wantStatus, tc.wantCode, 0)
		})
	}
}

func TestFormalPoolOnboardingProductionRouteRevalidatesAllRoutes(t *testing.T) {
	require.Len(t, formalPoolProductionRouteOperations, 16)

	for _, operation := range formalPoolProductionRouteOperations {
		operation := operation
		for _, mutation := range formalPoolProductionAuthorityMutations() {
			mutation := mutation
			t.Run(operation.name+"/durable_"+mutation.name+"_after_guard", func(t *testing.T) {
				fixture := newFormalPoolProductionRouteFixture(t, operation.stage, true, "tenant-one")
				fixture.settings.afterGet = func() { mutation.mutate(&fixture.userRepo.user) }
				body := operation.body
				if operation.name == "BrowserEgressAttestation" {
					body = `{"confirmed":true,"verification_code":` + strconv.Quote(fixture.browserProof) + `}`
				}
				rec := fixture.request(operation.method, operation.path(fixture), body, fixture.token, "")
				requireFormalPoolProductionDenial(t, fixture, rec, mutation.wantStatus, mutation.wantCode, 1)
			})
		}
	}
}

func TestFormalPoolOnboardingCombinedOwnerStaleAndWrongStateDeniesBeforeDependencies(t *testing.T) {
	for _, operation := range formalPoolProductionRouteOperations {
		operation := operation
		if operation.name == "CreateSession" || operation.name == "GetSession" {
			continue
		}
		t.Run(operation.name, func(t *testing.T) {
			wrongStage := "warming"
			if operation.name == "Promotion" || operation.name == "Abort" {
				wrongStage = "accepted"
			}
			fixture := newFormalPoolProductionRouteFixture(t, wrongStage, true, "tenant-one")
			fixture.userRepo.user.ID++
			fixture.userRepo.user.Email = "cross-owner-admin@example.test"
			fixture.token = fixture.generateToken(t)
			settings := service.NewSettingService(fixture.settings, &config.Config{})
			_, err := settings.AcceptAdminCompliance(context.Background(), service.AdminComplianceAcceptInput{
				AdminUserID: fixture.userRepo.user.ID, Language: "en", Phrase: service.AdminComplianceAckPhraseEN,
			})
			require.NoError(t, err)
			fixture.version = 0
			fixture.resetObservedCalls()

			rec := fixture.request(operation.method, operation.path(fixture), operation.body, fixture.token, "")
			require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), "FORMAL_POOL_FORBIDDEN")
			require.Equal(t, 1, fixture.settings.getCalls)
			require.Equal(t, 0, fixture.revalidator.calls)
			require.Equal(t, 0, fixture.dependencies.calls)
		})
	}
}

func TestFormalPoolOnboardingProductionRouteCreateRevalidatesAfterGroupLookup(t *testing.T) {
	for _, mutation := range formalPoolProductionAuthorityMutations() {
		mutation := mutation
		t.Run(mutation.name, func(t *testing.T) {
			fixture := newFormalPoolProductionRouteFixture(t, "none", true, "tenant-one")
			fixture.groups.afterGet = func() { mutation.mutate(&fixture.userRepo.user) }
			rec := fixture.request(http.MethodPost, "/api/v1/admin/claude-onboarding/sessions", phase0CreateBody(phase0Owner.groupID), fixture.token, "")
			require.Equal(t, mutation.wantStatus, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), mutation.wantCode)
			require.Equal(t, 2, fixture.revalidator.calls, "create must revalidate before and after group lookup")
			require.Equal(t, 1, fixture.groups.calls)
			require.Equal(t, 0, fixture.dependencies.calls)
			require.NotContains(t, rec.Body.String(), `"id":"fpo_`)
		})
	}
}

func TestFormalPoolOnboardingProductionRouteInventory(t *testing.T) {
	fixture := newFormalPoolProductionRouteFixture(t, "none", true, "tenant-one")
	got := make([]string, 0, len(fixture.router.Routes()))
	for _, route := range fixture.router.Routes() {
		got = append(got, route.Method+" "+route.Path)
	}
	sort.Strings(got)
	want := formalPoolProductionRouteInventory()
	sort.Strings(want)
	require.Equal(t, want, got)
}

type formalPoolProductionAuthorityMutation struct {
	name       string
	mutate     func(*service.User)
	wantStatus int
	wantCode   string
}

func formalPoolProductionAuthorityMutations() []formalPoolProductionAuthorityMutation {
	return []formalPoolProductionAuthorityMutation{
		{
			name: "inactive status", wantStatus: http.StatusUnauthorized, wantCode: "FORMAL_POOL_AUTH_REQUIRED",
			mutate: func(user *service.User) { user.Status = service.StatusDisabled },
		},
		{
			name: "role loss", wantStatus: http.StatusForbidden, wantCode: "FORMAL_POOL_FORBIDDEN",
			mutate: func(user *service.User) { user.Role = service.RoleUser },
		},
		{
			name: "token version drift", wantStatus: http.StatusUnauthorized, wantCode: "FORMAL_POOL_AUTH_REQUIRED",
			mutate: func(user *service.User) { user.TokenVersion++ },
		},
	}
}

type formalPoolProductionRouteFixture struct {
	t              *testing.T
	userRepo       *formalPoolProductionUserRepo
	settings       *formalPoolProductionSettingRepo
	groups         *formalPoolProductionGroupReader
	dependencies   *formalPoolProductionDependencies
	revalidator    *formalPoolProductionRevalidator
	authService    *service.AuthService
	userService    *service.UserService
	service        *service.FormalPoolOnboardingService
	router         *gin.Engine
	token          string
	sessionID      string
	accountID      int64
	version        int64
	browserProof   string
	tenantID       string
	configuredTime func() time.Time
}

func newFormalPoolProductionRouteFixture(t *testing.T, stage string, acknowledged bool, tenantID string) *formalPoolProductionRouteFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()
	lastActiveAt := now
	userRepo := &formalPoolProductionUserRepo{user: service.User{
		ID: 1001, Email: "formal-pool-admin@example.test", Role: service.RoleAdmin,
		Status: service.StatusActive, AllowedGroups: []int64{phase0Owner.groupID},
		TokenVersion: 1, TokenVersionResolved: true, LastActiveAt: &lastActiveAt,
	}}
	cfg := &config.Config{}
	cfg.JWT.Secret = "formal-pool-production-route-secret-32bytes"
	cfg.JWT.AccessTokenExpireMinutes = 60
	authService := service.NewAuthService(nil, userRepo, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	userService := service.NewUserService(userRepo, nil, nil, nil)
	nowFn := func() time.Time { return now }
	productionRevalidator := adminhandler.NewFormalPoolOnboardingPrincipalRevalidator(userService, tenantID, nowFn)
	revalidator := &formalPoolProductionRevalidator{delegate: productionRevalidator}
	settings := &formalPoolProductionSettingRepo{}
	settingService := service.NewSettingService(settings, cfg)
	if acknowledged {
		_, err := settingService.AcceptAdminCompliance(context.Background(), service.AdminComplianceAcceptInput{
			AdminUserID: userRepo.user.ID, Language: "en", Phrase: service.AdminComplianceAckPhraseEN,
		})
		require.NoError(t, err)
	}
	groups := &formalPoolProductionGroupReader{}
	dependencies := &formalPoolProductionDependencies{oauth: phase0OAuthFake{fullScope: true}}
	onboardingService := service.NewFormalPoolOnboardingService(service.FormalPoolOnboardingDeps{
		OAuth: dependencies, Proxy: dependencies, Accounts: dependencies,
		CCGateway: dependencies, CCGatewayRuntime: dependencies,
		Acceptance: dependencies, Healthcheck: dependencies, Refresh: dependencies,
		CacheInvalidator: dependencies, SchedulerCache: dependencies,
		Groups: groups, PrincipalRevalidator: revalidator,
	})
	fixture := &formalPoolProductionRouteFixture{
		t: t, userRepo: userRepo, settings: settings, groups: groups, dependencies: dependencies,
		revalidator: revalidator, authService: authService, userService: userService,
		service: onboardingService, tenantID: tenantID, configuredTime: nowFn,
	}
	fixture.token = fixture.generateToken(t)
	fixture.prepareStage(stage)

	h := &ihandler.Handlers{Admin: &ihandler.AdminHandlers{FormalPoolOnboarding: adminhandler.NewFormalPoolOnboardingHandler(onboardingService)}}
	fixture.router = gin.New()
	v1 := fixture.router.Group("/api/v1")
	resolver := adminhandler.NewFormalPoolOnboardingPrincipalResolver(userService, tenantID, nowFn)
	formalJWT := middleware.NewFormalPoolOnboardingJWTAuthMiddleware(authService, userService)
	RegisterFormalPoolOnboardingAdminRoutes(v1, h, formalJWT, resolver, settingService)
	fixture.resetObservedCalls()
	return fixture
}

func (f *formalPoolProductionRouteFixture) prepareStage(stage string) {
	f.t.Helper()
	if stage == "none" {
		return
	}
	created, err := f.service.StartSession(f.authorityContext(0), service.FormalPoolOnboardingStartRequest{
		ProxyMode: "existing", ProxyID: phase0Int64Ptr(7), GroupID: phase0Owner.groupID, AccountName: "production-route-account",
	})
	require.NoError(f.t, err)
	f.sessionID, f.version = created.ID, created.Version
	if stage == "created" {
		return
	}
	advance := func(call func() (*service.FormalPoolOnboardingSession, error)) {
		result, callErr := call()
		require.NoError(f.t, callErr)
		require.NotNil(f.t, result)
		f.version = result.Version
		if result.AccountID > 0 {
			f.accountID = result.AccountID
		}
	}
	var tested *service.FormalPoolOnboardingSession
	advance(func() (*service.FormalPoolOnboardingSession, error) {
		var testErr error
		tested, testErr = f.service.TestProxy(f.authorityContext(f.version), f.sessionID)
		return tested, testErr
	})
	parts := strings.Split(strings.TrimRight(tested.BrowserEgressCheckURL, "/"), "/")
	f.browserProof = parts[len(parts)-1]
	observed, err := f.service.VerifyBrowserEgressByNonce(context.Background(), f.browserProof, "198.51.100.10")
	require.NoError(f.t, err)
	f.version = observed.Version
	if stage == "proxy-tested" || stage == "setup-token-ready" {
		if stage == "setup-token-ready" {
			f.dependencies.oauth.fullScope = false
		}
		return
	}
	advance(func() (*service.FormalPoolOnboardingSession, error) {
		return f.service.AttestBrowserEgress(f.authorityContext(f.version), f.sessionID, service.FormalPoolBrowserEgressAttestationRequest{
			Confirmed: true, VerificationCode: f.browserProof,
		})
	})
	if stage == "attested" {
		return
	}
	advance(func() (*service.FormalPoolOnboardingSession, error) {
		return f.service.GenerateAuthURL(f.authorityContext(f.version), f.sessionID)
	})
	if stage == "oauth-url" {
		return
	}
	advance(func() (*service.FormalPoolOnboardingSession, error) {
		return f.service.ExchangeCodeAndCreate(f.authorityContext(f.version), f.sessionID, service.FormalPoolExchangeCodeAndCreateRequest{Code: "owner-code"})
	})
	if stage == "imported" {
		return
	}
	advance(func() (*service.FormalPoolOnboardingSession, error) {
		return f.service.RefreshOnly(f.authorityContext(f.version), f.sessionID)
	})
	if stage == "refreshed" {
		return
	}
	advance(func() (*service.FormalPoolOnboardingSession, error) {
		return f.service.RegisterRuntime(f.authorityContext(f.version), f.sessionID)
	})
	accepted, err := f.service.RunAcceptance(f.authorityContext(f.version), f.sessionID)
	require.NoError(f.t, err)
	require.NotNil(f.t, accepted)
	f.version = accepted.Version
	if stage == "accepted" {
		return
	}
	advance(func() (*service.FormalPoolOnboardingSession, error) {
		return f.service.StartWarming(f.authorityContext(f.version), f.sessionID)
	})
	require.Equal(f.t, "warming", stage)
}

func (f *formalPoolProductionRouteFixture) authorityContext(version int64) context.Context {
	principal := service.FormalPoolOnboardingPrincipal{
		SubjectID: f.userRepo.user.ID, AdministratorID: f.userRepo.user.ID,
		TenantID: f.tenantID, CreatorID: f.userRepo.user.ID, Role: service.RoleAdmin,
		CallerKind: service.CallerKindHumanJWT, AuthorityRevision: 1,
		ExpiresAtUnix: f.configuredTime().Add(time.Hour).Unix(), Active: true, SystemAdmin: true,
	}
	return service.WithFormalPoolRequestAuthority(context.Background(), service.FormalPoolRequestAuthority{
		Principal: principal, ExpectedVersion: &version, IdempotencyKey: "production-route-operation-key",
	})
}

func (f *formalPoolProductionRouteFixture) resetObservedCalls() {
	f.userRepo.getCalls = 0
	f.userRepo.afterSnapshot = nil
	f.settings.getCalls = 0
	f.settings.afterGet = nil
	f.groups.calls = 0
	f.groups.afterGet = nil
	f.dependencies.calls = 0
	f.revalidator.calls = 0
}

func (f *formalPoolProductionRouteFixture) generateToken(t *testing.T) string {
	t.Helper()
	token, err := f.authService.GenerateToken(&f.userRepo.user)
	require.NoError(t, err)
	return token
}

func (f *formalPoolProductionRouteFixture) generateExpiredToken(t *testing.T) string {
	t.Helper()
	cfg := &config.Config{}
	cfg.JWT.Secret = "formal-pool-production-route-secret-32bytes"
	cfg.JWT.ExpireHour = -1
	authService := service.NewAuthService(nil, f.userRepo, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	token, err := authService.GenerateToken(&f.userRepo.user)
	require.NoError(t, err)
	return token
}

func (f *formalPoolProductionRouteFixture) request(method, path, body, token, labels string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if method == http.MethodPost {
		version := f.version
		if path == "/api/v1/admin/claude-onboarding/sessions" {
			version = 0
		}
		req.Header.Set("If-Match", `"`+strconv.FormatInt(version, 10)+`"`)
		req.Header.Set("Idempotency-Key", "production-route-operation-key")
	}
	if labels != "" {
		req.Header.Set("X-Tenant-ID", labels)
		req.Header.Set("X-Group-ID", strconv.FormatInt(phase0Owner.groupID, 10))
		req.Header.Set("X-Allowed-Groups", strconv.FormatInt(phase0Owner.groupID, 10))
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func (f *formalPoolProductionRouteFixture) requestWithAdminAPIKey(method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-API-Key", "production-route-admin-api-key")
	req.Header.Set("If-Match", `"0"`)
	req.Header.Set("Idempotency-Key", "production-route-operation-key")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func formalPoolProductionSessionPath(suffix string) func(*formalPoolProductionRouteFixture) string {
	return func(f *formalPoolProductionRouteFixture) string {
		return "/api/v1/admin/claude-onboarding/sessions/" + f.sessionID + suffix
	}
}

func requireFormalPoolProductionDenial(t *testing.T, fixture *formalPoolProductionRouteFixture, rec *httptest.ResponseRecorder, wantStatus int, wantCode string, wantRevalidations int) {
	t.Helper()
	require.Equal(t, wantStatus, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), wantCode)
	require.Equal(t, wantRevalidations, fixture.revalidator.calls)
	if wantRevalidations == 0 {
		require.Equal(t, 0, fixture.settings.getCalls, "principal denial must happen before compliance")
	} else {
		require.Equal(t, 1, fixture.settings.getCalls, "service denial must happen after one compliance check")
	}
	require.Equal(t, 0, fixture.dependencies.calls, "authority denial must precede every business dependency")
}

func formalPoolProductionRouteInventory() []string {
	return []string{
		"POST /api/v1/admin/claude-onboarding/sessions",
		"GET /api/v1/admin/claude-onboarding/sessions/:id",
		"POST /api/v1/admin/claude-onboarding/sessions/:id/test-proxy",
		"POST /api/v1/admin/claude-onboarding/sessions/:id/browser-egress-attestation",
		"POST /api/v1/admin/claude-onboarding/sessions/:id/generate-auth-url",
		"POST /api/v1/admin/claude-onboarding/sessions/:id/exchange-code-and-create",
		"POST /api/v1/admin/claude-onboarding/sessions/:id/setup-token-cookie-auth-and-create",
		"POST /api/v1/admin/claude-onboarding/sessions/:id/acceptance",
		"POST /api/v1/admin/claude-onboarding/sessions/:id/activate",
		"POST /api/v1/admin/claude-onboarding/sessions/:id/refresh-only",
		"POST /api/v1/admin/claude-onboarding/sessions/:id/runtime-register",
		"POST /api/v1/admin/claude-onboarding/sessions/:id/healthcheck",
		"POST /api/v1/admin/claude-onboarding/sessions/:id/start-warming",
		"POST /api/v1/admin/claude-onboarding/sessions/:id/promote-production",
		"POST /api/v1/admin/claude-onboarding/sessions/:id/abort",
		"POST /api/v1/admin/claude-onboarding/accounts/:id/healthcheck",
	}
}

type formalPoolProductionUserRepo struct {
	service.UserRepository
	user          service.User
	getCalls      int
	afterSnapshot func(int, *service.User)
}

func (r *formalPoolProductionUserRepo) GetByID(_ context.Context, id int64) (*service.User, error) {
	if id != r.user.ID {
		return nil, service.ErrUserNotFound
	}
	r.getCalls++
	snapshot := r.user
	if r.afterSnapshot != nil {
		r.afterSnapshot(r.getCalls, &r.user)
	}
	return &snapshot, nil
}

func (r *formalPoolProductionUserRepo) GetUserAvatar(context.Context, int64) (*service.UserAvatar, error) {
	return nil, nil
}

func (r *formalPoolProductionUserRepo) UpdateUserLastActiveAt(_ context.Context, id int64, at time.Time) error {
	if id != r.user.ID {
		return service.ErrUserNotFound
	}
	r.user.LastActiveAt = &at
	return nil
}

func (r *formalPoolProductionUserRepo) markDeleted() {
	deletedAt := time.Now().UTC()
	r.user.DeletedAt = &deletedAt
}

type formalPoolProductionSettingRepo struct {
	values   map[string]string
	getCalls int
	afterGet func()
}

func (r *formalPoolProductionSettingRepo) Get(ctx context.Context, key string) (*service.Setting, error) {
	value, err := r.GetValue(ctx, key)
	if err != nil {
		return nil, err
	}
	return &service.Setting{Key: key, Value: value}, nil
}

func (r *formalPoolProductionSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	r.getCalls++
	if hook := r.afterGet; hook != nil {
		r.afterGet = nil
		hook()
	}
	if value, ok := r.values[key]; ok {
		return value, nil
	}
	return "", service.ErrSettingNotFound
}

func (r *formalPoolProductionSettingRepo) Set(_ context.Context, key, value string) error {
	if r.values == nil {
		r.values = map[string]string{}
	}
	r.values[key] = value
	return nil
}

func (r *formalPoolProductionSettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			values[key] = value
		}
	}
	return values, nil
}

func (r *formalPoolProductionSettingRepo) SetMultiple(ctx context.Context, values map[string]string) error {
	for key, value := range values {
		if err := r.Set(ctx, key, value); err != nil {
			return err
		}
	}
	return nil
}

func (r *formalPoolProductionSettingRepo) GetAll(context.Context) (map[string]string, error) {
	values := make(map[string]string, len(r.values))
	for key, value := range r.values {
		values[key] = value
	}
	return values, nil
}

func (r *formalPoolProductionSettingRepo) Delete(_ context.Context, key string) error {
	delete(r.values, key)
	return nil
}

type formalPoolProductionRevalidator struct {
	delegate service.FormalPoolOnboardingPrincipalRevalidator
	calls    int
}

func (r *formalPoolProductionRevalidator) Revalidate(ctx context.Context, principal service.FormalPoolOnboardingPrincipal) error {
	r.calls++
	return r.delegate.Revalidate(ctx, principal)
}

type formalPoolProductionGroupReader struct {
	calls    int
	afterGet func()
}

func (r *formalPoolProductionGroupReader) GetByID(_ context.Context, id int64) (*service.Group, error) {
	r.calls++
	if hook := r.afterGet; hook != nil {
		r.afterGet = nil
		hook()
	}
	return &service.Group{ID: id, Status: service.StatusActive}, nil
}

type formalPoolProductionDependencies struct {
	service.SchedulerCache
	calls    int
	oauth    phase0OAuthFake
	accounts phase0AccountReader
}

func (d *formalPoolProductionDependencies) called() { d.calls++ }

func (d *formalPoolProductionDependencies) ResolveOrCreateProxy(ctx context.Context, req service.FormalPoolOnboardingStartRequest) (service.FormalPoolProxyResolution, error) {
	d.called()
	return (&phase0ProxyFake{}).ResolveOrCreateProxy(ctx, req)
}

func (d *formalPoolProductionDependencies) TestProxy(ctx context.Context, proxyID int64) (service.FormalPoolProxyTestSummary, error) {
	d.called()
	return (&phase0ProxyFake{}).TestProxy(ctx, proxyID)
}

func (d *formalPoolProductionDependencies) GetRawEgressIP(ctx context.Context, proxyID int64, proxyURL string) (string, error) {
	d.called()
	return (&phase0ProxyFake{}).GetRawEgressIP(ctx, proxyID, proxyURL)
}

func (d *formalPoolProductionDependencies) GenerateFormalAuthURL(ctx context.Context, proxyID int64) (service.FormalPoolOAuthURL, error) {
	d.called()
	return d.oauth.GenerateFormalAuthURL(ctx, proxyID)
}

func (d *formalPoolProductionDependencies) ExchangeCode(ctx context.Context, code, sessionID string, proxyID int64) (service.FormalPoolOAuthTokenSummary, map[string]any, error) {
	d.called()
	return d.oauth.ExchangeCode(ctx, code, sessionID, proxyID)
}

func (d *formalPoolProductionDependencies) SetupTokenCookieAuth(ctx context.Context, sessionKey string, proxyID int64) (service.FormalPoolOAuthTokenSummary, map[string]any, error) {
	d.called()
	return d.oauth.SetupTokenCookieAuth(ctx, sessionKey, proxyID)
}

func (d *formalPoolProductionDependencies) RefreshFormalPoolAccount(ctx context.Context, account *service.Account) (service.FormalPoolOAuthTokenSummary, map[string]any, error) {
	d.called()
	return d.oauth.RefreshFormalPoolAccount(ctx, account)
}

func (d *formalPoolProductionDependencies) CreateFormalPoolAccount(ctx context.Context, input service.FormalPoolAccountCreateInput) (*service.Account, error) {
	d.called()
	return d.accounts.CreateFormalPoolAccount(ctx, input)
}

func (d *formalPoolProductionDependencies) GetFormalPoolAccount(ctx context.Context, id int64) (*service.Account, error) {
	d.called()
	return d.accounts.GetFormalPoolAccount(ctx, id)
}

func (d *formalPoolProductionDependencies) UpdateFormalPoolAccountCredentials(ctx context.Context, id int64, credentials map[string]any) (*service.Account, error) {
	d.called()
	return d.accounts.UpdateFormalPoolAccountCredentials(ctx, id, credentials)
}

func (d *formalPoolProductionDependencies) UpdateFormalPoolAccountState(ctx context.Context, id int64, schedulable bool, status string, extra map[string]any) (*service.Account, error) {
	d.called()
	return d.accounts.UpdateFormalPoolAccountState(ctx, id, schedulable, status, extra)
}

func (d *formalPoolProductionDependencies) ActivateFormalPoolAccount(ctx context.Context, id int64, extra map[string]any) (*service.Account, error) {
	d.called()
	return d.accounts.ActivateFormalPoolAccount(ctx, id, extra)
}

func (d *formalPoolProductionDependencies) VerifyCCGatewayReadiness(ctx context.Context, input service.FormalPoolAcceptanceInput) ([]service.FormalPoolAcceptanceCheck, error) {
	d.called()
	return phase0CCGatewayFake{}.VerifyCCGatewayReadiness(ctx, input)
}

func (d *formalPoolProductionDependencies) RegisterCCGatewayRuntime(ctx context.Context, input service.FormalPoolCCGatewayRuntimeRegistration) error {
	d.called()
	return (&phase0RuntimeFake{}).RegisterCCGatewayRuntime(ctx, input)
}

func (d *formalPoolProductionDependencies) RunAcceptance(ctx context.Context, input service.FormalPoolAcceptanceInput) (*service.FormalPoolAcceptanceResult, error) {
	d.called()
	return phase0AcceptanceFake{}.RunAcceptance(ctx, input)
}

func (d *formalPoolProductionDependencies) RunHealthcheck(ctx context.Context, input service.FormalPoolAcceptanceInput) (*service.FormalPoolAcceptanceResult, error) {
	d.called()
	return phase0HealthcheckFake{}.RunHealthcheck(ctx, input)
}

func (d *formalPoolProductionDependencies) InvalidateToken(context.Context, *service.Account) error {
	d.called()
	return nil
}

func (d *formalPoolProductionDependencies) SetAccount(context.Context, *service.Account) error {
	d.called()
	return nil
}

var _ service.SettingRepository = (*formalPoolProductionSettingRepo)(nil)
var _ service.UserRepository = (*formalPoolProductionUserRepo)(nil)
var _ service.FormalPoolOnboardingPrincipalRevalidator = (*formalPoolProductionRevalidator)(nil)
var _ service.FormalPoolOnboardingGroupReader = (*formalPoolProductionGroupReader)(nil)
var _ service.FormalPoolProxyVerifier = (*formalPoolProductionDependencies)(nil)
var _ service.FormalPoolOAuthFacade = (*formalPoolProductionDependencies)(nil)
var _ service.FormalPoolAccountCreator = (*formalPoolProductionDependencies)(nil)
var _ service.FormalPoolCCGatewayReadinessVerifier = (*formalPoolProductionDependencies)(nil)
var _ service.FormalPoolCCGatewayRuntimeRegistrar = (*formalPoolProductionDependencies)(nil)
var _ service.FormalPoolAcceptanceRunner = (*formalPoolProductionDependencies)(nil)
var _ service.FormalPoolAccountHealthcheckRunner = (*formalPoolProductionDependencies)(nil)
var _ service.TokenCacheInvalidator = (*formalPoolProductionDependencies)(nil)
