//go:build phase0red

package routes

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type phase0OnboardingPrincipal struct {
	adminID  int64
	tenantID string
	groupID  int64
	role     string
}

func TestFormalPoolOnboardingAuthorizationRejectsCrossBoundaryOperations(t *testing.T) {
	gin.SetMode(gin.TestMode)
	owner := phase0OnboardingPrincipal{adminID: 1101, tenantID: "tenant-one", groupID: 101, role: service.RoleAdmin}
	attacker := phase0OnboardingPrincipal{adminID: 2202, tenantID: "tenant-two", groupID: 202, role: service.RoleAdmin}
	svc := service.NewFormalPoolOnboardingService(service.FormalPoolOnboardingDeps{
		Proxy: &formalPoolOnboardingRoutesProxy{rawIP: "198.51.100.10"},
	})
	router := newFormalPoolOnboardingRoutesRouter(adminhandler.NewFormalPoolOnboardingHandler(svc), func(c *gin.Context) {
		if id, err := strconv.ParseInt(c.GetHeader("X-Phase0-Admin"), 10, 64); err == nil {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: id})
		}
		c.Set(string(middleware.ContextKeyUserRole), c.GetHeader("X-Phase0-Role"))
		c.Set("phase0_tenant_id", c.GetHeader("X-Phase0-Tenant"))
		c.Set("phase0_group_id", c.GetHeader("X-Phase0-Group"))
		c.Next()
	})

	create := func(t *testing.T, principal phase0OnboardingPrincipal, groupID int64) *httptest.ResponseRecorder {
		t.Helper()
		body := bytes.NewBufferString(`{"proxy_mode":"existing","proxy_id":7,"group_id":` + strconv.FormatInt(groupID, 10) + `,"account_name":"phase0-boundary"}`)
		return phase0OnboardingRequest(router, principal, http.MethodPost, "/api/v1/admin/claude-onboarding/sessions", body)
	}
	created := create(t, owner, owner.groupID)
	require.Equal(t, http.StatusOK, created.Code, created.Body.String())
	sessionID := extractFormalPoolOnboardingSessionID(t, created.Body.String())

	t.Run("cross-tenant group session creation", func(t *testing.T) {
		rec := create(t, attacker, owner.groupID)
		require.Equal(t, http.StatusForbidden, rec.Code, "an administrator must not create an object in another tenant or group")
	})

	operations := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "GetSession", method: http.MethodGet, path: "/sessions/" + sessionID},
		{name: "TestProxy", method: http.MethodPost, path: "/sessions/" + sessionID + "/test-proxy"},
		{name: "BrowserEgressAttestation", method: http.MethodPost, path: "/sessions/" + sessionID + "/browser-egress-attestation", body: `{"confirmed":true,"verification_code":"untrusted-proof"}`},
		{name: "GenerateOAuth", method: http.MethodPost, path: "/sessions/" + sessionID + "/generate-auth-url"},
		{name: "ExchangeOAuth", method: http.MethodPost, path: "/sessions/" + sessionID + "/exchange-code-and-create", body: `{}`},
		{name: "ExchangeSetupToken", method: http.MethodPost, path: "/sessions/" + sessionID + "/setup-token-cookie-auth-and-create", body: `{"session_key":"opaque-input"}`},
		{name: "Acceptance", method: http.MethodPost, path: "/sessions/" + sessionID + "/acceptance"},
		{name: "RefreshOnly", method: http.MethodPost, path: "/sessions/" + sessionID + "/refresh-only"},
		{name: "RuntimeRegistration", method: http.MethodPost, path: "/sessions/" + sessionID + "/runtime-register"},
		{name: "SessionHealthcheck", method: http.MethodPost, path: "/sessions/" + sessionID + "/healthcheck"},
		{name: "AccountHealthcheck", method: http.MethodPost, path: "/accounts/1/healthcheck"},
		{name: "StartWarming", method: http.MethodPost, path: "/sessions/" + sessionID + "/start-warming"},
		{name: "Abort", method: http.MethodPost, path: "/sessions/" + sessionID + "/abort"},
		{name: "Activation", method: http.MethodPost, path: "/sessions/" + sessionID + "/activate"},
		{name: "Promotion", method: http.MethodPost, path: "/sessions/" + sessionID + "/promote-production"},
	}
	for _, operation := range operations {
		operation := operation
		t.Run(operation.name, func(t *testing.T) {
			path := "/api/v1/admin/claude-onboarding" + operation.path
			rec := phase0OnboardingRequest(router, attacker, operation.method, path, bytes.NewBufferString(operation.body))
			require.Equal(t, http.StatusForbidden, rec.Code,
				"cross-principal operation must be rejected before object lookup, state, or version handling; body=%s", rec.Body.String())
		})
	}

	t.Run("non-admin role cannot read an onboarding session", func(t *testing.T) {
		unprivileged := attacker
		unprivileged.role = "user"
		rec := phase0OnboardingRequest(router, unprivileged, http.MethodGet, "/api/v1/admin/claude-onboarding/sessions/"+sessionID, nil)
		require.Equal(t, http.StatusForbidden, rec.Code, "role authorization must be enforced independently of random-ID knowledge")
	})
}

func TestFormalPoolOnboardingPublicOriginAuthority(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("configured public origin remains authoritative", func(t *testing.T) {
		svc := service.NewFormalPoolOnboardingService(service.FormalPoolOnboardingDeps{
			Proxy: &formalPoolOnboardingRoutesProxy{rawIP: "198.51.100.10"}, PublicURLPrefix: "https://public.example.test",
		})
		router := newFormalPoolOnboardingRoutesRouter(adminhandler.NewFormalPoolOnboardingHandler(svc), func(c *gin.Context) { c.Next() })
		rec := phase0CreateAndTestProxyWithHostileOrigin(t, router)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Contains(t, rec.Body.String(), `"browser_egress_check_url":"https://public.example.test/api/v1/claude-onboarding/browser-egress-check/`)
		require.NotContains(t, rec.Body.String(), "hostile.example.invalid")
	})

	t.Run("untrusted forwarded origin cannot become authority", func(t *testing.T) {
		svc := service.NewFormalPoolOnboardingService(service.FormalPoolOnboardingDeps{
			Proxy: &formalPoolOnboardingRoutesProxy{rawIP: "198.51.100.10"},
		})
		router := newFormalPoolOnboardingRoutesRouter(adminhandler.NewFormalPoolOnboardingHandler(svc), func(c *gin.Context) { c.Next() })
		rec := phase0CreateAndTestProxyWithHostileOrigin(t, router)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.NotContains(t, rec.Body.String(), "hostile.example.invalid", "Host and forwarded headers are attacker input without an explicit trusted-ingress policy")
		require.Contains(t, rec.Body.String(), `"browser_egress_check_url":"https://public.example.test/api/v1/claude-onboarding/browser-egress-check/`,
			"the browser URL must use an explicitly configured public origin")
	})
}

func phase0CreateAndTestProxyWithHostileOrigin(t *testing.T, router *gin.Engine) *httptest.ResponseRecorder {
	t.Helper()
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/claude-onboarding/sessions",
		bytes.NewBufferString(`{"proxy_mode":"existing","proxy_id":7,"group_id":101,"account_name":"phase0-origin"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusOK, createRec.Code, createRec.Body.String())
	sessionID := extractFormalPoolOnboardingSessionID(t, createRec.Body.String())

	testReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/claude-onboarding/sessions/"+sessionID+"/test-proxy", nil)
	testReq.Host = "hostile.example.invalid"
	testReq.Header.Set("X-Forwarded-Host", "hostile.example.invalid")
	testReq.Header.Set("X-Forwarded-Proto", "https")
	testRec := httptest.NewRecorder()
	router.ServeHTTP(testRec, testReq)
	return testRec
}

func phase0OnboardingRequest(router *gin.Engine, principal phase0OnboardingPrincipal, method, path string, body *bytes.Buffer) *httptest.ResponseRecorder {
	if body == nil {
		body = &bytes.Buffer{}
	}
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Phase0-Admin", strconv.FormatInt(principal.adminID, 10))
	req.Header.Set("X-Phase0-Role", principal.role)
	req.Header.Set("X-Phase0-Tenant", principal.tenantID)
	req.Header.Set("X-Phase0-Group", strconv.FormatInt(principal.groupID, 10))
	req.Header.Set("If-Match", `"0"`)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}
