package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ihandler "github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFormalPoolOperationsRoutes_AdminProtected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router, authCalls := newFormalPoolOperationsRoutesRouter(t, newFormalPoolOperationsRoutesService(t, nil))

	rec := performFormalPoolOperationsRequest(router, http.MethodGet, "/api/v1/admin/accounts/5/formal-pool/diagnostics", nil)
	require.NotEqual(t, http.StatusNotFound, rec.Code)
	require.Equal(t, 1, *authCalls, "diagnostics route must be admin protected")

	rec = performFormalPoolOperationsRequest(router, http.MethodPost, "/api/v1/admin/accounts/5/setup-token/replace", `{"session_key":"sk-ant-sid-test-secret"}`)
	require.NotEqual(t, http.StatusNotFound, rec.Code)
	require.Equal(t, 2, *authCalls, "setup-token replacement route must be admin protected")
	require.NotContains(t, rec.Body.String(), "sk-ant-sid-test-secret")

	rec = performFormalPoolOperationsRequest(router, http.MethodPost, "/api/v1/admin/accounts/5/formal-pool/promote-production", nil)
	require.NotEqual(t, http.StatusNotFound, rec.Code)
	require.Equal(t, 3, *authCalls, "promote-production route must be admin protected")
}

func TestFormalPoolOperationsRoutes_PromoteProductionSuccessReturnsSafeAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := formalPoolOperationsRoutesAccount()
	account.Status = service.StatusActive
	account.Schedulable = true
	account.Extra[service.FormalPoolExtraOnboardingStage] = service.FormalPoolStageWarming
	account.Extra[service.FormalPoolExtraHealthcheckStatus] = "passed"
	account.Extra[service.FormalPoolExtraHealthcheckStatusCodeBucket] = "status_2xx"
	account.Extra[service.FormalPoolExtraHealthcheckRawRef] = "hmac-sha256:" + strings.Repeat("a", 64)
	account.Extra[service.FormalPoolExtraHealthcheckCCGatewaySeen] = true
	account.Extra[service.FormalPoolExtraHealthcheckFallbackDetected] = false
	account.Extra[service.FormalPoolExtraHealthcheckProxyMismatch] = false
	account.Extra[service.FormalPoolExtraHealthcheckRiskTextDetected] = false
	account.Extra[service.FormalPoolExtraRuntimeRegistered] = "true"
	account.Extra[service.FormalPoolExtraRuntimeRegisteredAt] = "2026-05-30T00:00:00Z"
	account.Extra["cc_gateway_egress_bucket_enabled"] = "true"
	account.Extra[service.FormalPoolExtraQuarantineReason] = "refresh_token_invalid"
	account.Extra[service.FormalPoolExtraQuarantineAt] = "2026-05-29T00:00:00Z"
	account.Credentials = map[string]any{"access_token": "access-secret", "refresh_token": "refresh-secret", "scope": "user:inference"}
	account.Proxy = &service.Proxy{Host: "proxy-secret.example.com", Username: "proxy-user-secret", Password: "proxy-password-secret"}
	router, _ := newFormalPoolOperationsRoutesRouter(t, newFormalPoolOperationsRoutesServiceWithAccount(t, account, nil))

	rec := performFormalPoolOperationsRequest(router, http.MethodPost, "/api/v1/admin/accounts/5/formal-pool/promote-production", nil)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	require.Contains(t, body, service.FormalPoolStageProduction)
	for _, unsafe := range []string{"access-secret", "refresh-secret", "proxy-secret.example.com", "proxy-user-secret", "proxy-password-secret"} {
		require.NotContains(t, body, unsafe)
	}
}

func TestFormalPoolOperationsRoutes_ReplacementErrorDoesNotEchoSessionKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := "sk-ant-sid-test-secret"
	router, _ := newFormalPoolOperationsRoutesRouter(t, newFormalPoolOperationsRoutesService(t, errors.New("raw exchange failed for "+secret)))

	rec := performFormalPoolOperationsRequest(router, http.MethodPost, "/api/v1/admin/accounts/5/setup-token/replace", fmt.Sprintf(`{"session_key":%q}`, secret))

	require.Equal(t, http.StatusBadRequest, rec.Code)
	body := rec.Body.String()
	require.NotContains(t, body, secret)
	require.NotContains(t, strings.ToLower(body), "sk-ant-sid")
	require.NotContains(t, strings.ToLower(body), "raw exchange")
}

func TestFormalPoolOperationsRoutes_OperationFailureIncludesSafeDiagnostics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router, _ := newFormalPoolOperationsRoutesRouter(t, newFormalPoolOperationsRoutesService(t, errors.New("raw token exchange 401 session_key=sk-ant-sid-test-secret")))

	rec := performFormalPoolOperationsRequest(router, http.MethodPost, "/api/v1/admin/accounts/5/setup-token/replace", `{"session_key":"sk-ant-sid-test-secret"}`)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Equal(t, "SETUP_TOKEN_REPLACE_FAILED", payload["error"])
	require.Equal(t, "setup-token credential exchange failed", payload["message"])
	require.NotNil(t, payload["account"])
	diagnostics, ok := payload["diagnostics"].(map[string]any)
	require.True(t, ok, "operation failures must expose structured diagnostics at top level")
	actions, ok := diagnostics["recommended_actions"].([]any)
	require.True(t, ok)
	require.Contains(t, formalPoolOperationsRouteActionKeys(actions), "replace_account_and_proxy")
	body := strings.ToLower(rec.Body.String())
	require.NotContains(t, body, "session_key")
	require.NotContains(t, body, "sk-ant-sid")
	require.NotContains(t, body, "raw token exchange")
}

func TestFormalPoolOperationsRoutes_OperationFailureAccountPayloadIsMinimalAndSafe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := formalPoolOperationsRoutesAccount()
	account.ErrorMessage = "raw exchange error for sk-ant-sid-test-secret and user@example.com 123e4567-e89b-12d3-a456-426614174000"
	account.Proxy = &service.Proxy{
		ID:       7,
		Name:     "unsafe-proxy",
		Protocol: "http",
		Host:     "proxy-secret.example.com",
		Port:     8080,
		Username: "proxy-user-secret",
		Password: "proxy-password-secret",
		Status:   service.StatusActive,
	}
	router, _ := newFormalPoolOperationsRoutesRouter(t, newFormalPoolOperationsRoutesServiceWithAccount(t, account, errors.New("raw exchange failed for sk-ant-sid-test-secret user@example.com 123e4567-e89b-12d3-a456-426614174000 proxy-user-secret")))

	rec := performFormalPoolOperationsRequest(router, http.MethodPost, "/api/v1/admin/accounts/5/setup-token/replace", `{"session_key":"sk-ant-sid-test-secret"}`)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	body := rec.Body.String()
	for _, unsafe := range []string{
		"sk-ant-sid-test-secret",
		"raw exchange",
		"user@example.com",
		"123e4567-e89b-12d3-a456-426614174000",
		"proxy-secret.example.com",
		"proxy-user-secret",
		"proxy-password-secret",
	} {
		require.NotContains(t, body, unsafe)
	}

	var payload map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	accountPayload, ok := payload["account"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, float64(5), accountPayload["id"])
	require.Equal(t, service.StatusError, accountPayload["status"])
	require.Equal(t, service.FormalPoolStageQuarantined, accountPayload["onboarding_stage"])
	require.Equal(t, false, accountPayload["schedulable"])
	require.NotContains(t, accountPayload, "error_message")
	require.NotContains(t, accountPayload, "proxy")
	require.NotContains(t, accountPayload, "credentials")
	require.NotContains(t, accountPayload, "extra")

	diagnostics, ok := payload["diagnostics"].(map[string]any)
	require.True(t, ok)
	actions, ok := diagnostics["recommended_actions"].([]any)
	require.True(t, ok)
	require.Contains(t, formalPoolOperationsRouteActionKeys(actions), "replace_account_and_proxy")
}

func TestFormalPoolOperationsRoutes_NilHandlerIsSafelyAbsent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")
	h := &ihandler.Handlers{Admin: &ihandler.AdminHandlers{}}
	authCalls := 0
	RegisterAdminRoutes(v1, h, middleware.AdminAuthMiddleware(func(c *gin.Context) {
		authCalls++
		c.Next()
	}))

	rec := performFormalPoolOperationsRequest(router, http.MethodGet, "/api/v1/admin/accounts/5/formal-pool/diagnostics", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Equal(t, 0, authCalls, "missing optional handler should leave the route absent without invoking admin handlers")
}

func newFormalPoolOperationsRoutesRouter(t *testing.T, svc *service.FormalPoolOperationsService) (*gin.Engine, *int) {
	t.Helper()
	router := gin.New()
	v1 := router.Group("/api/v1")
	h := &ihandler.Handlers{Admin: &ihandler.AdminHandlers{FormalPoolOperations: adminhandler.NewFormalPoolOperationsHandler(svc)}}
	authCalls := 0
	RegisterAdminRoutes(v1, h, middleware.AdminAuthMiddleware(func(c *gin.Context) {
		authCalls++
		c.Next()
	}))
	return router, &authCalls
}

func newFormalPoolOperationsRoutesService(t *testing.T, oauthErr error) *service.FormalPoolOperationsService {
	t.Helper()
	return newFormalPoolOperationsRoutesServiceWithAccount(t, formalPoolOperationsRoutesAccount(), oauthErr)
}

func newFormalPoolOperationsRoutesServiceWithAccount(t *testing.T, account *service.Account, oauthErr error) *service.FormalPoolOperationsService {
	t.Helper()
	store := &formalPoolOperationsRoutesAccountStore{account: account}
	return service.NewFormalPoolOperationsService(service.FormalPoolOperationsDeps{
		Accounts: store,
		OAuth:    &formalPoolOperationsRoutesOAuth{err: oauthErr},
	})
}

func formalPoolOperationsRoutesAccount() *service.Account {
	proxyID := int64(7)
	return &service.Account{
		ID:          5,
		Name:        "formal-routes",
		Platform:    service.PlatformAnthropic,
		Type:        service.AccountTypeSetupToken,
		Status:      service.StatusError,
		Schedulable: false,
		ProxyID:     &proxyID,
		Extra: map[string]any{
			service.FormalPoolExtraOnboardingStage:      service.FormalPoolStageQuarantined,
			service.FormalPoolExtraHealthcheckStatus:    service.FormalPoolOnboardingStatusQuarantined,
			service.FormalPoolExtraQuarantineReason:     "reason_auth",
			service.FormalPoolExtraRiskEventRef:         "hmac-sha256:" + strings.Repeat("d", 64),
			"cc_gateway_account_ref":                    "hmac-sha256:" + strings.Repeat("e", 64),
			"cc_gateway_credential_ref":                 "hmac-sha256:" + strings.Repeat("f", 64),
			"cc_gateway_credential_binding_hmac":        "hmac-sha256:" + strings.Repeat("1", 64),
			"cc_gateway_proxy_identity_ref":             "hmac-sha256:" + strings.Repeat("2", 64),
			"cc_gateway_persona_profile":                "claude-code-2.1.197-macos-local",
			"claude_code_device_id":                     strings.Repeat("c", 64),
			"cc_gateway_enabled":                        "true",
			"cc_gateway_routes":                         "native_messages",
			"cc_gateway_egress_bucket":                  "claude-1234567890abcdef",
			"cc_gateway_egress_bucket_enabled":          "true",
			service.FormalPoolExtraRuntimeRegistered:    true,
			service.FormalPoolExtraRuntimeRegisteredAt:  "2026-05-30T00:00:00Z",
			service.FormalPoolExtraCredentialGeneration: "1",
		},
		Credentials: map[string]any{"scope": "user:inference"},
	}
}

func performFormalPoolOperationsRequest(router *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	switch b := body.(type) {
	case nil:
		reader = bytes.NewReader(nil)
	case string:
		reader = bytes.NewReader([]byte(b))
	case []byte:
		reader = bytes.NewReader(b)
	default:
		raw, _ := json.Marshal(b)
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

type formalPoolOperationsRoutesAccountStore struct {
	account *service.Account
}

func (s *formalPoolOperationsRoutesAccountStore) GetFormalPoolAccount(context.Context, int64) (*service.Account, error) {
	return s.account, nil
}

func (s *formalPoolOperationsRoutesAccountStore) UpdateFormalPoolAccountCredentials(_ context.Context, _ int64, credentials map[string]any) (*service.Account, error) {
	s.account.Credentials = cloneFormalPoolOperationsRoutesMap(credentials)
	return s.account, nil
}

func (s *formalPoolOperationsRoutesAccountStore) UpdateFormalPoolAccountState(_ context.Context, _ int64, schedulable bool, status string, extra map[string]any) (*service.Account, error) {
	s.account.Schedulable = schedulable
	if strings.TrimSpace(status) != "" {
		s.account.Status = status
	}
	if s.account.Extra == nil {
		s.account.Extra = map[string]any{}
	}
	for k, v := range extra {
		s.account.Extra[k] = v
	}
	return s.account, nil
}

func (s *formalPoolOperationsRoutesAccountStore) ActivateFormalPoolAccount(ctx context.Context, id int64, extra map[string]any) (*service.Account, error) {
	return s.UpdateFormalPoolAccountState(ctx, id, true, service.StatusActive, extra)
}

func (s *formalPoolOperationsRoutesAccountStore) UpdateFormalPoolAccountProxy(ctx context.Context, id int64, proxyID int64, extra map[string]any) (*service.Account, error) {
	s.account.ProxyID = &proxyID
	return s.UpdateFormalPoolAccountState(ctx, id, false, service.StatusActive, extra)
}

type formalPoolOperationsRoutesOAuth struct {
	err error
}

func (f *formalPoolOperationsRoutesOAuth) GenerateFormalAuthURL(context.Context, int64) (service.FormalPoolOAuthURL, error) {
	return service.FormalPoolOAuthURL{}, nil
}

func (f *formalPoolOperationsRoutesOAuth) ExchangeCode(context.Context, string, string, int64) (service.FormalPoolOAuthTokenSummary, map[string]any, error) {
	return service.FormalPoolOAuthTokenSummary{}, nil, nil
}

func (f *formalPoolOperationsRoutesOAuth) SetupTokenCookieAuth(context.Context, string, int64) (service.FormalPoolOAuthTokenSummary, map[string]any, error) {
	if f.err != nil {
		return service.FormalPoolOAuthTokenSummary{}, nil, f.err
	}
	return service.FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true}, map[string]any{"access_token": "new-access", "refresh_token": "new-refresh", "scope": "user:inference"}, nil
}

func cloneFormalPoolOperationsRoutesMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func formalPoolOperationsRouteActionKeys(actions []any) []string {
	keys := make([]string, 0, len(actions))
	for _, item := range actions {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if key, ok := m["key"].(string); ok {
			keys = append(keys, key)
		}
	}
	return keys
}
