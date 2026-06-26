package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
)

type formalPoolProxyAdminService interface {
	GetProxy(ctx context.Context, id int64) (*Proxy, error)
	CreateProxy(ctx context.Context, input *CreateProxyInput) (*Proxy, error)
	TestProxy(ctx context.Context, id int64) (*ProxyTestResult, error)
}

type FormalPoolAdminProxyVerifier struct {
	admin       formalPoolProxyAdminService
	egressCache *ProxyEgressCache
	egressProbe ProxyEgressProbe
}

func NewFormalPoolAdminProxyVerifier(admin AdminService) *FormalPoolAdminProxyVerifier {
	probe := NewSideEffectFreeProxyEgressProbe(ProxyEgressProbeOptions{})
	return NewFormalPoolAdminProxyVerifierWithEgressProbe(
		admin,
		NewProxyEgressCache(ProxyEgressCacheOptions{}),
		probe.Probe,
	)
}

func NewFormalPoolAdminProxyVerifierWithEgressProbe(admin formalPoolProxyAdminService, cache *ProxyEgressCache, probe ProxyEgressProbe) *FormalPoolAdminProxyVerifier {
	if cache == nil {
		cache = NewProxyEgressCache(ProxyEgressCacheOptions{})
	}
	if probe == nil {
		defaultProbe := NewSideEffectFreeProxyEgressProbe(ProxyEgressProbeOptions{})
		probe = defaultProbe.Probe
	}
	return &FormalPoolAdminProxyVerifier{admin: admin, egressCache: cache, egressProbe: probe}
}

func (v *FormalPoolAdminProxyVerifier) ResolveOrCreateProxy(ctx context.Context, req FormalPoolOnboardingStartRequest) (FormalPoolProxyResolution, error) {
	if v == nil || v.admin == nil {
		return FormalPoolProxyResolution{}, ErrFormalPoolOnboardingNotFound
	}
	if strings.EqualFold(req.ProxyMode, "create") {
		protocol := strings.ToLower(strings.TrimSpace(req.Proxy.Protocol))
		probe := Proxy{Name: req.Proxy.Name, Protocol: protocol, Host: req.Proxy.Host, Port: req.Proxy.Port, Username: req.Proxy.Username, Password: req.Proxy.Password}
		normalized, parsed, err := proxyurl.Parse(probe.URL())
		if err != nil {
			return FormalPoolProxyResolution{}, err
		}
		created, err := v.admin.CreateProxy(ctx, &CreateProxyInput{Name: strings.TrimSpace(req.Proxy.Name), Protocol: parsed.Scheme, Host: strings.TrimSpace(req.Proxy.Host), Port: req.Proxy.Port, Username: strings.TrimSpace(req.Proxy.Username), Password: strings.TrimSpace(req.Proxy.Password)})
		if err != nil {
			return FormalPoolProxyResolution{}, err
		}
		return FormalPoolProxyResolution{ProxyID: created.ID, ProxyRef: formalPoolSafeRef("proxy", fmt.Sprintf("%d", created.ID)), NormalizedProxyURL: normalized}, nil
	}
	proxy, err := v.admin.GetProxy(ctx, *req.ProxyID)
	if err != nil {
		return FormalPoolProxyResolution{}, err
	}
	if proxy == nil || !proxy.IsActive() {
		return FormalPoolProxyResolution{}, fmt.Errorf("proxy inactive")
	}
	normalized, _, err := proxyurl.Parse(proxy.URL())
	if err != nil {
		return FormalPoolProxyResolution{}, err
	}
	return FormalPoolProxyResolution{ProxyID: proxy.ID, ProxyRef: formalPoolSafeRef("proxy", fmt.Sprintf("%d", proxy.ID)), NormalizedProxyURL: normalized}, nil
}
func (v *FormalPoolAdminProxyVerifier) TestProxy(ctx context.Context, proxyID int64) (FormalPoolProxyTestSummary, error) {
	if v == nil || v.admin == nil {
		return FormalPoolProxyTestSummary{}, ErrFormalPoolOnboardingNotFound
	}
	v.InvalidateProxyEgress(proxyID)
	proxy, err := v.admin.GetProxy(ctx, proxyID)
	if err != nil {
		return FormalPoolProxyTestSummary{}, err
	}
	if proxy == nil || !proxy.IsActive() {
		return FormalPoolProxyTestSummary{}, fmt.Errorf("proxy inactive")
	}
	if _, _, err := proxyurl.Parse(proxy.URL()); err != nil {
		return FormalPoolProxyTestSummary{}, err
	}
	res, err := v.admin.TestProxy(ctx, proxyID)
	if err != nil {
		return FormalPoolProxyTestSummary{}, err
	}
	if res == nil || !res.Success {
		return FormalPoolProxyTestSummary{}, fmt.Errorf("proxy test failed")
	}
	return FormalPoolProxyTestSummary{Success: true, ProxyRef: formalPoolSafeRef("proxy", fmt.Sprintf("%d", proxyID)), ExitIPRef: formalPoolSafeRef("exit_ip", res.IPAddress), LatencyBucket: formalPoolLatencyBucket(res.LatencyMs)}, nil
}

func (v *FormalPoolAdminProxyVerifier) InvalidateProxyEgress(proxyID int64) {
	if v == nil || v.egressCache == nil {
		return
	}
	v.egressCache.InvalidateProxy(proxyID)
}

func (v *FormalPoolAdminProxyVerifier) GetRawEgressIP(ctx context.Context, proxyID int64, normalizedProxyURL string) (string, error) {
	if v == nil {
		return "", ErrFormalPoolOnboardingNotFound
	}
	cache := v.egressCache
	if cache == nil {
		cache = NewProxyEgressCache(ProxyEgressCacheOptions{})
	}
	probe := v.egressProbe
	if probe == nil {
		defaultProbe := NewSideEffectFreeProxyEgressProbe(ProxyEgressProbeOptions{})
		probe = defaultProbe.Probe
	}
	rawIP, _, err := cache.GetOrProbe(ctx, proxyID, normalizedProxyURL, probe)
	return rawIP, err
}

func formalPoolLatencyBucket(ms int64) string {
	switch {
	case ms <= 0:
		return "unknown"
	case ms < 500:
		return "lt_500ms"
	case ms < 1500:
		return "lt_1500ms"
	default:
		return "gte_1500ms"
	}
}

type FormalPoolClaudeOAuthFacade struct{ oauth *OAuthService }

func NewFormalPoolClaudeOAuthFacade(oauthService *OAuthService) *FormalPoolClaudeOAuthFacade {
	return &FormalPoolClaudeOAuthFacade{oauth: oauthService}
}
func (f *FormalPoolClaudeOAuthFacade) GenerateFormalAuthURL(ctx context.Context, proxyID int64) (FormalPoolOAuthURL, error) {
	if f == nil || f.oauth == nil {
		return FormalPoolOAuthURL{}, fmt.Errorf("oauth service unavailable")
	}
	res, err := f.oauth.GenerateAuthURL(ctx, &proxyID)
	if err != nil {
		return FormalPoolOAuthURL{}, err
	}
	return FormalPoolOAuthURL{AuthURL: res.AuthURL, SessionID: res.SessionID}, nil
}
func (f *FormalPoolClaudeOAuthFacade) ExchangeCode(ctx context.Context, sessionID, code string, proxyID int64) (FormalPoolOAuthTokenSummary, map[string]any, error) {
	if f == nil || f.oauth == nil {
		return FormalPoolOAuthTokenSummary{}, nil, fmt.Errorf("oauth service unavailable")
	}
	tok, err := f.oauth.ExchangeCode(ctx, &ExchangeCodeInput{SessionID: sessionID, Code: code})
	if err != nil {
		return FormalPoolOAuthTokenSummary{}, nil, err
	}
	summary, creds := formalPoolTokenInfoSummaryAndCredentials(tok)
	return summary, creds, nil
}

func (f *FormalPoolClaudeOAuthFacade) SetupTokenCookieAuth(ctx context.Context, sessionKey string, proxyID int64) (FormalPoolOAuthTokenSummary, map[string]any, error) {
	if f == nil || f.oauth == nil {
		return FormalPoolOAuthTokenSummary{}, nil, fmt.Errorf("oauth service unavailable")
	}
	tok, err := f.oauth.CookieAuth(ctx, &CookieAuthInput{
		SessionKey: sessionKey,
		ProxyID:    &proxyID,
		Scope:      "inference",
	})
	if err != nil {
		return FormalPoolOAuthTokenSummary{}, nil, err
	}
	summary, creds := formalPoolTokenInfoSummaryAndCredentials(tok)
	return summary, creds, nil
}

func (f *FormalPoolClaudeOAuthFacade) RefreshFormalPoolAccount(ctx context.Context, account *Account) (FormalPoolOAuthTokenSummary, map[string]any, error) {
	if f == nil || f.oauth == nil {
		return FormalPoolOAuthTokenSummary{}, nil, fmt.Errorf("oauth service unavailable")
	}
	if account == nil {
		return FormalPoolOAuthTokenSummary{}, nil, fmt.Errorf("account is required")
	}
	tok, err := f.oauth.RefreshAccountToken(ctx, account)
	if err != nil {
		return FormalPoolOAuthTokenSummary{}, nil, err
	}
	summary, refreshed := formalPoolTokenInfoSummaryAndCredentials(tok)
	credentials := MergeCredentials(account.Credentials, refreshed)
	return summary, credentials, nil
}

func formalPoolTokenInfoSummaryAndCredentials(tok *TokenInfo) (FormalPoolOAuthTokenSummary, map[string]any) {
	scope := strings.TrimSpace(tok.Scope)
	summary := FormalPoolOAuthTokenSummary{EmailPresent: strings.TrimSpace(tok.EmailAddress) != "", AccountUUIDPresent: strings.TrimSpace(tok.AccountUUID) != "", OrganizationUUIDPresent: strings.TrimSpace(tok.OrgUUID) != "", ScopeContainsUserInference: strings.Contains(scope, "user:inference"), ScopeContainsClaudeCode: strings.Contains(scope, "user:sessions:claude_code") && scope != oauth.ScopeInference, ExpiresInBucket: formalPoolExpiresBucket(tok.ExpiresIn)}
	creds := map[string]any{"access_token": tok.AccessToken, "refresh_token": tok.RefreshToken, "token_type": tok.TokenType, "expires_in": tok.ExpiresIn, "expires_at": tok.ExpiresAt, "scope": tok.Scope}
	return summary, creds
}
func formalPoolExpiresBucket(v int64) string {
	if v <= 0 {
		return "unknown"
	}
	if v <= 3600 {
		return "le_1h"
	}
	return "gt_1h"
}

type FormalPoolAdminAccountManager struct{ admin AdminService }

func NewFormalPoolAdminAccountManager(admin AdminService) *FormalPoolAdminAccountManager {
	return &FormalPoolAdminAccountManager{admin: admin}
}
func (m *FormalPoolAdminAccountManager) CreateFormalPoolAccount(ctx context.Context, input FormalPoolAccountCreateInput) (*Account, error) {
	if m == nil || m.admin == nil {
		return nil, fmt.Errorf("admin service unavailable")
	}
	accountType := strings.TrimSpace(input.Type)
	if accountType == "" {
		accountType = AccountTypeOAuth
	}
	return m.admin.CreateAccount(ctx, &CreateAccountInput{Name: input.Name, Notes: &input.Notes, Platform: PlatformAnthropic, Type: accountType, Credentials: input.Credentials, Extra: input.Extra, ProxyID: &input.ProxyID, Concurrency: input.Concurrency, GroupIDs: []int64{input.GroupID}, SkipDefaultGroupBind: true, Schedulable: &input.Schedulable})
}
func (m *FormalPoolAdminAccountManager) GetFormalPoolAccount(ctx context.Context, id int64) (*Account, error) {
	if m == nil || m.admin == nil {
		return nil, fmt.Errorf("admin service unavailable")
	}
	return m.admin.GetAccount(ctx, id)
}

func (m *FormalPoolAdminAccountManager) UpdateFormalPoolAccountCredentials(ctx context.Context, id int64, credentials map[string]any) (*Account, error) {
	if m == nil || m.admin == nil {
		return nil, fmt.Errorf("admin service unavailable")
	}
	return m.admin.UpdateAccount(ctx, id, &UpdateAccountInput{Credentials: cloneCredentials(credentials)})
}

func (m *FormalPoolAdminAccountManager) UpdateFormalPoolAccountState(ctx context.Context, id int64, schedulable bool, status string, extra map[string]any) (*Account, error) {
	if m == nil || m.admin == nil {
		return nil, fmt.Errorf("admin service unavailable")
	}
	account, err := m.admin.GetAccount(ctx, id)
	if err != nil {
		return nil, err
	}
	merged := cloneCredentials(account.Extra)
	if merged == nil {
		merged = map[string]any{}
	}
	for k, v := range extra {
		merged[k] = v
	}
	input := &UpdateAccountInput{Schedulable: &schedulable, Extra: merged}
	if strings.TrimSpace(status) != "" {
		input.Status = status
	}
	return m.admin.UpdateAccount(ctx, id, input)
}

func (m *FormalPoolAdminAccountManager) ActivateFormalPoolAccount(ctx context.Context, id int64, extra map[string]any) (*Account, error) {
	return m.UpdateFormalPoolAccountState(ctx, id, true, StatusActive, extra)
}

type FormalPoolStaticCCGatewayReadinessVerifier struct{}

func NewFormalPoolStaticCCGatewayReadinessVerifier() *FormalPoolStaticCCGatewayReadinessVerifier {
	return &FormalPoolStaticCCGatewayReadinessVerifier{}
}

type FormalPoolHTTPCCGatewayRuntimeRegistrar struct {
	baseURL              string
	token                string
	internalControlToken string
	client               *http.Client
}

func NewFormalPoolHTTPCCGatewayRuntimeRegistrar(cfg *config.Config) *FormalPoolHTTPCCGatewayRuntimeRegistrar {
	if cfg == nil || !cfg.Gateway.CCGateway.Enabled || strings.TrimSpace(cfg.Gateway.CCGateway.BaseURL) == "" || strings.TrimSpace(cfg.Gateway.CCGateway.Token) == "" || strings.TrimSpace(cfg.Gateway.CCGateway.InternalControlToken) == "" {
		return nil
	}
	timeout := time.Duration(cfg.Gateway.CCGateway.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &FormalPoolHTTPCCGatewayRuntimeRegistrar{
		baseURL:              strings.TrimRight(strings.TrimSpace(cfg.Gateway.CCGateway.BaseURL), "/"),
		token:                strings.TrimSpace(cfg.Gateway.CCGateway.Token),
		internalControlToken: strings.TrimSpace(cfg.Gateway.CCGateway.InternalControlToken),
		client:               &http.Client{Timeout: timeout},
	}
}

func (r *FormalPoolHTTPCCGatewayRuntimeRegistrar) RegisterCCGatewayRuntime(ctx context.Context, input FormalPoolCCGatewayRuntimeRegistration) error {
	if r == nil || strings.TrimSpace(r.baseURL) == "" || strings.TrimSpace(r.token) == "" || strings.TrimSpace(r.internalControlToken) == "" {
		return infraRuntimeRegistrationUnavailable()
	}
	if strings.TrimSpace(input.AccountRef) == "" ||
		strings.TrimSpace(input.CredentialRef) == "" ||
		strings.TrimSpace(input.CredentialBindingHMAC) == "" ||
		strings.TrimSpace(input.EgressBucket) == "" ||
		strings.TrimSpace(input.ProxyURL) == "" ||
		strings.TrimSpace(input.ProxyRef) == "" ||
		strings.TrimSpace(input.DeviceID) == "" {
		return fmt.Errorf("cc gateway runtime registration requires safe account, credential, binding, bucket, proxy url, proxy ref and device id")
	}
	tokenType := strings.ToLower(strings.TrimSpace(input.TokenType))
	credentialProof := strings.TrimSpace(input.CredentialProof)
	if credentialProof == "" {
		return fmt.Errorf("cc gateway runtime registration credential proof is required")
	}
	if tokenType != "oauth" && tokenType != "apikey" {
		return fmt.Errorf("cc gateway runtime registration credential proof token_type must be oauth or apikey")
	}
	endpoint, err := url.JoinPath(r.baseURL, "/_runtime/register-account")
	if err != nil {
		return err
	}
	payload := map[string]any{
		"account_id":              input.AccountRef,
		"account_ref":             input.AccountRef,
		"account_uuid_ref":        input.AccountRef,
		"credential_ref":          input.CredentialRef,
		"credential_binding_hmac": input.CredentialBindingHMAC,
		"token_type":              tokenType,
		"egress_bucket":           input.EgressBucket,
		"proxy_url":               input.ProxyURL,
		"proxy_identity_ref":      input.ProxyRef,
		"policy_version":          input.PolicyVersion,
		"persona_variant":         input.PersonaVariant,
		"session_policy":          input.SessionPolicy,
		"device_id":               input.DeviceID,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CC-Gateway-Token", r.token)
	req.Header.Set("X-CC-Internal-Control-Token", r.internalControlToken)
	switch tokenType {
	case "oauth":
		req.Header.Set("Authorization", credentialProof)
	case "apikey":
		req.Header.Set("X-API-Key", credentialProof)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("cc gateway runtime registration failed with status %d code %s", resp.StatusCode, ccGatewayControlPlaneCode(resp, body))
	}
	return nil
}

func infraRuntimeRegistrationUnavailable() error {
	return fmt.Errorf("cc gateway runtime registrar unavailable")
}

func (v *FormalPoolStaticCCGatewayReadinessVerifier) VerifyCCGatewayReadiness(ctx context.Context, input FormalPoolAcceptanceInput) ([]FormalPoolAcceptanceCheck, error) {
	_ = ctx
	checks := []FormalPoolAcceptanceCheck{
		{Name: "cc_gateway_bucket_present", Status: "pass", Message: "bucket ref recorded locally; runtime bucket smoke requires separate approval"},
		{Name: "cc_gateway_account_ref_present", Status: "pass", Message: "safe account ref recorded locally"},
	}
	if strings.TrimSpace(input.EgressBucket) == "" {
		checks[0].Status = "fail"
		checks[0].Message = "egress bucket missing"
	}
	if strings.TrimSpace(input.AccountRef) == "" || input.AccountRef == fmt.Sprintf("%d", input.AccountID) || !isSafeLedgerRef(input.AccountRef) {
		checks[1].Status = "fail"
		checks[1].Message = "safe account ref missing"
	}
	return checks, nil
}
