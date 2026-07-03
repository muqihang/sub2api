package service

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type formalProxyFake struct {
	testErr            error
	inactive           bool
	normalizedProxyURL string
}

func (f *formalProxyFake) ResolveOrCreateProxy(ctx context.Context, req FormalPoolOnboardingStartRequest) (FormalPoolProxyResolution, error) {
	if f.inactive {
		return FormalPoolProxyResolution{}, errors.New("proxy inactive")
	}
	id := int64(9)
	if req.ProxyID != nil {
		id = *req.ProxyID
	}
	url := f.normalizedProxyURL
	if url == "" {
		url = "socks5h://proxy.local:1080"
	}
	return FormalPoolProxyResolution{ProxyID: id, ProxyRef: formalPoolSafeRef("proxy", "9"), NormalizedProxyURL: url}, nil
}
func (f *formalProxyFake) TestProxy(ctx context.Context, proxyID int64) (FormalPoolProxyTestSummary, error) {
	if f.testErr != nil {
		return FormalPoolProxyTestSummary{}, f.testErr
	}
	return FormalPoolProxyTestSummary{Success: true, ProxyRef: formalPoolSafeRef("proxy", "9"), ExitIPRef: formalPoolSafeRef("exit_ip", "203.0.113.10"), LatencyBucket: "lt_500ms"}, nil
}

func (f *formalProxyFake) GetRawEgressIP(ctx context.Context, proxyID int64, normalizedProxyURL string) (string, error) {
	return "203.0.113.10", nil
}

type formalOAuthFake struct {
	summary         FormalPoolOAuthTokenSummary
	creds           map[string]any
	err             error
	lastCookieScope string
	refreshCalls    int
	refreshErr      error
	refreshSummary  FormalPoolOAuthTokenSummary
	refreshCreds    map[string]any
}

func (f *formalOAuthFake) GenerateFormalAuthURL(ctx context.Context, proxyID int64) (FormalPoolOAuthURL, error) {
	return FormalPoolOAuthURL{AuthURL: "https://claude.ai/oauth/authorize?state=safe", SessionID: "oauth-session"}, nil
}
func (f *formalOAuthFake) ExchangeCode(ctx context.Context, sessionID, code string, proxyID int64) (FormalPoolOAuthTokenSummary, map[string]any, error) {
	if f.err != nil {
		return FormalPoolOAuthTokenSummary{}, nil, f.err
	}
	return f.summary, f.creds, nil
}
func (f *formalOAuthFake) SetupTokenCookieAuth(ctx context.Context, sessionKey string, proxyID int64) (FormalPoolOAuthTokenSummary, map[string]any, error) {
	f.lastCookieScope = "inference"
	if f.err != nil {
		return FormalPoolOAuthTokenSummary{}, nil, f.err
	}
	return f.summary, f.creds, nil
}

func (f *formalOAuthFake) RefreshFormalPoolAccount(ctx context.Context, account *Account) (FormalPoolOAuthTokenSummary, map[string]any, error) {
	f.refreshCalls++
	if f.refreshErr != nil {
		return FormalPoolOAuthTokenSummary{}, nil, f.refreshErr
	}
	summary := f.refreshSummary
	if summary.ExpiresInBucket == "" {
		summary = f.summary
	}
	creds := f.refreshCreds
	if creds == nil {
		creds = map[string]any{"access_token": "refreshed-access", "refresh_token": "refreshed-refresh", "scope": "user:profile user:inference user:sessions:claude_code"}
	}
	return summary, creds, nil
}

type formalAccountFake struct {
	account        *Account
	created        FormalPoolAccountCreateInput
	activateErr    error
	stateUpdateErr error
	activated      bool
}

func (f *formalAccountFake) CreateFormalPoolAccount(ctx context.Context, input FormalPoolAccountCreateInput) (*Account, error) {
	f.created = input
	a := &Account{ID: 123, Name: input.Name, Platform: PlatformAnthropic, Type: input.Type, Status: StatusActive, Schedulable: input.Schedulable, ProxyID: &input.ProxyID, Concurrency: input.Concurrency, Credentials: input.Credentials, Extra: input.Extra, GroupIDs: []int64{input.GroupID}}
	f.account = a
	return a, nil
}
func (f *formalAccountFake) GetFormalPoolAccount(ctx context.Context, id int64) (*Account, error) {
	if f.account == nil {
		return nil, ErrAccountNotFound
	}
	return f.account, nil
}

func (f *formalAccountFake) UpdateFormalPoolAccountCredentials(ctx context.Context, id int64, credentials map[string]any) (*Account, error) {
	if f.account == nil {
		return nil, ErrAccountNotFound
	}
	f.account.Credentials = cloneCredentials(credentials)
	return f.account, nil
}

func (f *formalAccountFake) UpdateFormalPoolAccountState(ctx context.Context, id int64, schedulable bool, status string, extra map[string]any) (*Account, error) {
	if f.stateUpdateErr != nil {
		return nil, f.stateUpdateErr
	}
	if f.account == nil {
		return nil, ErrAccountNotFound
	}
	f.account.Schedulable = schedulable
	if status != "" {
		f.account.Status = status
	}
	if f.account.Extra == nil {
		f.account.Extra = map[string]any{}
	}
	for k, v := range extra {
		f.account.Extra[k] = v
	}
	return f.account, nil
}

func (f *formalAccountFake) ActivateFormalPoolAccount(ctx context.Context, id int64, extra map[string]any) (*Account, error) {
	if f.activateErr != nil {
		return nil, f.activateErr
	}
	f.activated = true
	return f.UpdateFormalPoolAccountState(ctx, id, true, StatusActive, extra)
}

type formalCCFake struct {
	checks []FormalPoolAcceptanceCheck
	err    error
}

func (f formalCCFake) VerifyCCGatewayReadiness(ctx context.Context, input FormalPoolAcceptanceInput) ([]FormalPoolAcceptanceCheck, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.checks != nil {
		return f.checks, nil
	}
	return []FormalPoolAcceptanceCheck{{Name: "cc_gateway_bucket", Status: "pass"}}, nil
}

type formalAcceptanceFake struct {
	result *FormalPoolAcceptanceResult
	err    error
}

func (f formalAcceptanceFake) RunAcceptance(ctx context.Context, input FormalPoolAcceptanceInput) (*FormalPoolAcceptanceResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	return &FormalPoolAcceptanceResult{Status: FormalPoolOnboardingStatusHealthcheckPassed, Checks: []FormalPoolAcceptanceCheck{{Name: "directed_healthcheck", Status: "pass"}}, NoRealMessagesRequestPerformed: false, ActivationRequired: true, StatusCodeBucket: "status_2xx", CCGatewaySeen: true, RawCapturePresent: true, RawCaptureRef: "hmac-sha256:" + strings.Repeat("8", 64)}, nil
}

type formalHealthcheckFake struct {
	result *FormalPoolAcceptanceResult
	err    error
	calls  int
}

func (f *formalHealthcheckFake) RunHealthcheck(ctx context.Context, input FormalPoolAcceptanceInput) (*FormalPoolAcceptanceResult, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	return &FormalPoolAcceptanceResult{Status: FormalPoolOnboardingStatusHealthcheckPassed, Checks: []FormalPoolAcceptanceCheck{{Name: "directed_healthcheck", Status: "pass"}}, NoRealMessagesRequestPerformed: false, ActivationRequired: true, StatusCodeBucket: "status_2xx", CCGatewaySeen: true, RawCapturePresent: true, RawCaptureRef: "hmac-sha256:" + strings.Repeat("8", 64)}, nil
}

type formalRuntimeFake struct {
	called bool
	calls  int
	input  FormalPoolCCGatewayRuntimeRegistration
	err    error
}

func (f *formalRuntimeFake) RegisterCCGatewayRuntime(ctx context.Context, input FormalPoolCCGatewayRuntimeRegistration) error {
	if f.err != nil {
		return f.err
	}
	f.called = true
	f.calls++
	f.input = input
	return nil
}

func TestFormalPoolProxyTestAndAttestationGatesOAuth(t *testing.T) {
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{}, OAuth: &formalOAuthFake{}})
	sess, err := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "acct"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GenerateAuthURL(context.Background(), sess.ID); err == nil {
		t.Fatalf("oauth url should be blocked before attestation")
	}
	if _, err := svc.TestProxy(context.Background(), sess.ID); err != nil {
		t.Fatalf("test proxy: %v", err)
	}
	if _, err := svc.GenerateAuthURL(context.Background(), sess.ID); err == nil {
		t.Fatalf("oauth url should still be blocked before browser egress attestation")
	}
	if _, err := svc.AttestBrowserEgress(context.Background(), sess.ID, FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: "exit-ip-ref-ok"}); err != nil {
		t.Fatalf("attest: %v", err)
	}
	got, err := svc.GenerateAuthURL(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("generate auth url: %v", err)
	}
	if got.AuthURL == "" || got.OAuthSessionID == "" {
		t.Fatalf("auth url/session missing: %#v", got)
	}
	assertNoFormalPoolSensitive(t, got)
}

func TestFormalPoolProxyFailClosed(t *testing.T) {
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{testErr: errors.New("dial failed")}})
	sess, err := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "acct"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TestProxy(context.Background(), sess.ID); err == nil {
		t.Fatalf("expected proxy test failure")
	}
	if _, err := svc.AttestBrowserEgress(context.Background(), sess.ID, FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: "manual"}); err == nil {
		t.Fatalf("attestation must require proxy test")
	}
}

func TestFormalPoolExchangeCodeAndCreateWritesSafeDefaults(t *testing.T) {
	acct := &formalAccountFake{}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{}, OAuth: &formalOAuthFake{summary: FormalPoolOAuthTokenSummary{EmailPresent: true, AccountUUIDPresent: true, OrganizationUUIDPresent: true, ScopeContainsUserInference: true, ScopeContainsClaudeCode: true, ExpiresInBucket: "gt_1h"}, creds: map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:profile user:inference user:sessions:claude_code"}}, Accounts: acct})
	sess, _ := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "acct", PoolProfile: "aggressive"})
	_, _ = svc.TestProxy(context.Background(), sess.ID)
	_, _ = svc.AttestBrowserEgress(context.Background(), sess.ID, FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: "manual"})
	_, _ = svc.GenerateAuthURL(context.Background(), sess.ID)
	got, err := svc.ExchangeCodeAndCreate(context.Background(), sess.ID, FormalPoolExchangeCodeAndCreateRequest{Code: "oauth-code"})
	if err != nil {
		t.Fatalf("exchange/create: %v", err)
	}
	if acct.created.Schedulable {
		t.Fatalf("account must be created unschedulable")
	}
	if acct.created.Extra["cc_gateway_enabled"] != "true" || acct.created.Extra["cc_gateway_canary_only"] != "false" || acct.created.Extra["cc_gateway_routes"] != "native_messages" || acct.created.Extra["pool_profile"] != PoolProfileNormal || acct.created.Extra[FormalPoolExtraPoolProfileRequested] != PoolProfileAggressive || acct.created.Extra[FormalPoolExtraPoolProfileEffective] != PoolProfileNormal || acct.created.Extra[FormalPoolExtraPoolWeightMode] != FormalPoolWeightLow || acct.created.Extra[FormalPoolExtraOnboardingStage] != FormalPoolStageImported || acct.created.Extra["oauth_refresh_fail_closed"] != "true" {
		t.Fatalf("bad formal extra: %#v", acct.created.Extra)
	}
	if acct.created.Extra["cc_gateway_account_ref"] == "123" || acct.created.Extra["cc_gateway_account_ref"] == "" {
		t.Fatalf("unsafe account ref: %#v", acct.created.Extra["cc_gateway_account_ref"])
	}
	for _, k := range []string{"enable_tls_fingerprint", "session_id_masking_enabled", "cache_ttl_override_enabled", "custom_base_url", "max_sessions", "base_rpm", "window_cost_limit"} {
		if _, ok := acct.created.Extra[k]; ok {
			t.Fatalf("dangerous extra %s present", k)
		}
	}
	if got.OAuthSummary == nil || !got.OAuthSummary.ScopeContainsUserInference {
		t.Fatalf("safe oauth summary missing")
	}
	assertNoFormalPoolSensitive(t, got)
}

func TestFormalPoolExchangeRegistersCCGatewayRuntimeMapping(t *testing.T) {
	acct := &formalAccountFake{}
	runtime := &formalRuntimeFake{}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{normalizedProxyURL: "socks5h://proxy.local:1080"}, OAuth: &formalOAuthFake{summary: FormalPoolOAuthTokenSummary{EmailPresent: true, AccountUUIDPresent: true, OrganizationUUIDPresent: true, ScopeContainsUserInference: true, ScopeContainsClaudeCode: true, ExpiresInBucket: "gt_1h"}, creds: map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:profile user:inference user:sessions:claude_code"}}, Accounts: acct, CCGatewayRuntime: runtime})
	sess, _ := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "acct"})
	_, _ = svc.TestProxy(context.Background(), sess.ID)
	_, _ = svc.AttestBrowserEgress(context.Background(), sess.ID, FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: "manual"})
	_, _ = svc.GenerateAuthURL(context.Background(), sess.ID)
	got, err := svc.ExchangeCodeAndCreate(context.Background(), sess.ID, FormalPoolExchangeCodeAndCreateRequest{Code: "oauth-code"})
	if err != nil {
		t.Fatalf("exchange/create: %v", err)
	}
	if !runtime.called {
		t.Fatalf("expected runtime registrar to be called")
	}
	if runtime.input.AccountRef != got.AccountRef || runtime.input.EgressBucket != got.EgressBucket || runtime.input.ProxyURL != "socks5h://proxy.local:1080" {
		t.Fatalf("bad runtime registration input: %#v got=%#v", runtime.input, got)
	}
	if runtime.input.TokenType != "oauth" || runtime.input.CredentialProof != "Bearer access" {
		t.Fatalf("runtime registration credential proof must come from server-side OAuth credentials: %#v", runtime.input)
	}
	if !got.CCGatewayRuntimeRegistered {
		t.Fatalf("session must expose runtime registration status")
	}
	if acct.created.Extra[FormalPoolExtraOnboardingStage] != FormalPoolStageImported || acct.created.Extra[FormalPoolExtraRuntimeRegistered] != "true" {
		t.Fatalf("runtime registration must be recorded without skipping imported stage: %#v", acct.created.Extra)
	}
	assertNoFormalPoolSensitive(t, got)
}

func TestFormalPoolAcceptanceFailsUntilCCGatewayRuntimeRegistered(t *testing.T) {
	acct := &formalAccountFake{}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{}, OAuth: &formalOAuthFake{summary: FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ScopeContainsClaudeCode: true, ExpiresInBucket: "gt_1h"}, creds: map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:profile user:inference user:sessions:claude_code"}}, Accounts: acct, CCGateway: formalCCFake{}})
	sess, _ := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "acct"})
	_, _ = svc.TestProxy(context.Background(), sess.ID)
	_, _ = svc.AttestBrowserEgress(context.Background(), sess.ID, FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: "manual"})
	_, _ = svc.GenerateAuthURL(context.Background(), sess.ID)
	_, _ = svc.ExchangeCodeAndCreate(context.Background(), sess.ID, FormalPoolExchangeCodeAndCreateRequest{Code: "oauth-code"})
	accepted, err := svc.RunAcceptance(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("acceptance: %v", err)
	}
	if accepted.Status != "failed_acceptance" {
		t.Fatalf("acceptance should fail before runtime registration: %#v", accepted)
	}
	found := false
	for _, check := range accepted.Checks {
		if check.Name == "cc_gateway_runtime_registered" && check.Status == "fail" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected cc_gateway_runtime_registered failure: %#v", accepted.Checks)
	}
}

func TestFormalPoolSetupTokenCookieRequiresProxyTestButNotBrowserEgress(t *testing.T) {
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{}, OAuth: &formalOAuthFake{}, Accounts: &formalAccountFake{}, CCGatewayRuntime: &formalRuntimeFake{}})
	sess, _ := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "setup-acct", PoolProfile: "normal"})
	if _, err := svc.SetupTokenCookieAuthAndCreate(context.Background(), sess.ID, FormalPoolSetupTokenCookieAuthAndCreateRequest{SessionKey: "sk-ant-sid02-test"}); err == nil {
		t.Fatalf("setup-token create must require proxy health test")
	}
}

func TestFormalPoolSetupTokenCookieCreateWrapsUntypedOAuthFailuresAsSafeBadRequest(t *testing.T) {
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{}, OAuth: &formalOAuthFake{err: errors.New("failed to get organizations: status 403, body: <html>sk-ant-sid-secret</html>")}, Accounts: &formalAccountFake{}, CCGatewayRuntime: &formalRuntimeFake{}})
	sess, _ := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "setup-acct", PoolProfile: "normal"})
	_, _ = svc.TestProxy(context.Background(), sess.ID)

	_, err := svc.SetupTokenCookieAuthAndCreate(context.Background(), sess.ID, FormalPoolSetupTokenCookieAuthAndCreateRequest{SessionKey: "sk-ant-sid02-secret"})
	if err == nil {
		t.Fatalf("expected safe setup-token oauth failure")
	}
	if got := strings.ToLower(err.Error()); strings.Contains(got, "internal error") || strings.Contains(got, "sk-ant-sid") || strings.Contains(got, "<html>") {
		t.Fatalf("setup-token failure must be actionable and redacted, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "Setup Token") && !strings.Contains(err.Error(), "setup-token") {
		t.Fatalf("setup-token failure should mention the failed login method, got %q", err.Error())
	}
}

func TestFormalPoolSetupTokenCookieCreateRegistersRuntimeAndKeepsAccountUnschedulable(t *testing.T) {
	acct := &formalAccountFake{}
	runtime := &formalRuntimeFake{}
	oauth := &formalOAuthFake{summary: FormalPoolOAuthTokenSummary{EmailPresent: true, AccountUUIDPresent: true, OrganizationUUIDPresent: true, ScopeContainsUserInference: true, ScopeContainsClaudeCode: false, ExpiresInBucket: "gt_1h"}, creds: map[string]any{"access_token": "setup-access", "refresh_token": "setup-refresh", "token_type": "Bearer", "expires_in": int64(31536000), "scope": "user:inference"}}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{normalizedProxyURL: "http://proxy.local:443"}, OAuth: oauth, Accounts: acct, CCGatewayRuntime: runtime})
	sess, _ := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "setup-acct", PoolProfile: "normal"})
	_, _ = svc.TestProxy(context.Background(), sess.ID)
	got, err := svc.SetupTokenCookieAuthAndCreate(context.Background(), sess.ID, FormalPoolSetupTokenCookieAuthAndCreateRequest{SessionKey: "sk-ant-sid02-test"})
	if err != nil {
		t.Fatalf("setup-token create: %v", err)
	}
	if oauth.lastCookieScope != "inference" {
		t.Fatalf("expected setup-token inference cookie auth, got %q", oauth.lastCookieScope)
	}
	if acct.created.Type != AccountTypeSetupToken || acct.account.Type != AccountTypeSetupToken {
		t.Fatalf("expected setup-token account type, created=%q account=%q", acct.created.Type, acct.account.Type)
	}
	if acct.created.Schedulable || acct.account.Schedulable {
		t.Fatalf("setup-token account must remain unschedulable before acceptance/activation")
	}
	if acct.created.Extra["cc_gateway_account_ref"] != got.AccountRef || acct.created.Extra["cc_gateway_enabled"] != "true" || acct.created.Extra["cc_gateway_canary_only"] != "false" {
		t.Fatalf("bad setup-token formal extra: %#v got=%#v", acct.created.Extra, got)
	}
	if acct.created.Extra[ccGatewayExtraPolicyVersion] != "2.1.197" || acct.created.Extra[ccGatewayExtraPersonaProfile] != ccGateway2197PersonaProfile {
		t.Fatalf("setup-token account must default to primary canonical tuple: %#v", acct.created.Extra)
	}
	if !runtime.called || runtime.input.AccountRef != got.AccountRef || runtime.input.EgressBucket != got.EgressBucket || runtime.input.ProxyURL != "http://proxy.local:443" {
		t.Fatalf("runtime registration missing or wrong: %#v got=%#v", runtime.input, got)
	}
	if runtime.input.TokenType != "oauth" || runtime.input.CredentialProof != "Bearer setup-access" {
		t.Fatalf("setup-token runtime registration proof must come from server-side setup-token credentials: %#v", runtime.input)
	}
	if runtime.input.PolicyVersion != "2.1.197" || runtime.input.PersonaVariant != ccGateway2197PersonaProfile {
		t.Fatalf("setup-token runtime registration must use primary canonical tuple: %#v", runtime.input)
	}
	if !got.CCGatewayRuntimeRegistered || got.OAuthSummary == nil || !got.OAuthSummary.ScopeContainsUserInference || got.OAuthSummary.ScopeContainsClaudeCode {
		t.Fatalf("bad setup-token safe summary/session: %#v", got)
	}
	if acct.created.Extra[FormalPoolExtraOnboardingStage] != FormalPoolStageImported || acct.created.Extra[FormalPoolExtraRuntimeRegistered] != "true" {
		t.Fatalf("setup-token runtime registration must be recorded without skipping imported stage: %#v", acct.created.Extra)
	}
	assertNoFormalPoolSensitive(t, got)
}

func TestFormalPoolSetupTokenCookieRejectsFullClaudeCodeScope(t *testing.T) {
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{}, OAuth: &formalOAuthFake{summary: FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ScopeContainsClaudeCode: true}, creds: map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:profile user:inference user:sessions:claude_code"}}, Accounts: &formalAccountFake{}, CCGatewayRuntime: &formalRuntimeFake{}})
	sess, _ := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "acct"})
	_, _ = svc.TestProxy(context.Background(), sess.ID)
	if _, err := svc.SetupTokenCookieAuthAndCreate(context.Background(), sess.ID, FormalPoolSetupTokenCookieAuthAndCreateRequest{SessionKey: "sk-ant-sid02-test"}); err == nil {
		t.Fatalf("setup-token path must reject full Claude Code OAuth scope")
	}
}

func TestFormalPoolExchangeScopeFailClosed(t *testing.T) {
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{}, OAuth: &formalOAuthFake{summary: FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ScopeContainsClaudeCode: false}, creds: map[string]any{"refresh_token": "refresh", "scope": "user:inference"}}, Accounts: &formalAccountFake{}})
	sess, _ := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "acct"})
	_, _ = svc.TestProxy(context.Background(), sess.ID)
	_, _ = svc.AttestBrowserEgress(context.Background(), sess.ID, FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: "manual"})
	_, _ = svc.GenerateAuthURL(context.Background(), sess.ID)
	if _, err := svc.ExchangeCodeAndCreate(context.Background(), sess.ID, FormalPoolExchangeCodeAndCreateRequest{Code: "oauth-code"}); err == nil {
		t.Fatalf("expected setup-token/inference-only scope to fail closed")
	}
}

func TestFormalPoolAcceptanceAndActivation(t *testing.T) {
	acct := &formalAccountFake{}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{}, OAuth: &formalOAuthFake{summary: FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ScopeContainsClaudeCode: true, ExpiresInBucket: "gt_1h"}, creds: map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:profile user:inference user:sessions:claude_code"}}, Accounts: acct, CCGateway: formalCCFake{}, CCGatewayRuntime: &formalRuntimeFake{}, Acceptance: formalAcceptanceFake{}})
	sess, _ := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "acct"})
	_, _ = svc.TestProxy(context.Background(), sess.ID)
	_, _ = svc.AttestBrowserEgress(context.Background(), sess.ID, FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: "manual"})
	_, _ = svc.GenerateAuthURL(context.Background(), sess.ID)
	_, _ = svc.ExchangeCodeAndCreate(context.Background(), sess.ID, FormalPoolExchangeCodeAndCreateRequest{Code: "oauth-code"})
	accepted, err := svc.RunAcceptance(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("acceptance: %v", err)
	}
	if accepted.Status != FormalPoolOnboardingStatusHealthcheckPassed || !accepted.ActivationRequired || accepted.NoRealMessagesRequestPerformed {
		t.Fatalf("bad acceptance: %#v", accepted)
	}
	if acct.account.Schedulable {
		t.Fatalf("acceptance must not activate account")
	}
	activated, err := svc.Activate(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	if activated.Status != FormalPoolOnboardingStatusWarming || !acct.activated || !acct.account.Schedulable || acct.account.Extra[FormalPoolExtraPoolProfileEffective] != PoolProfileNormal || acct.account.Extra[FormalPoolExtraPoolWeightMode] != FormalPoolWeightLow {
		t.Fatalf("activation failed: %#v account=%#v", activated, acct.account.Extra)
	}
}

func TestFormalPoolActivationFailDoesNotUpdateState(t *testing.T) {
	acct := &formalAccountFake{activateErr: errors.New("db down")}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{}, OAuth: &formalOAuthFake{summary: FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ScopeContainsClaudeCode: true, ExpiresInBucket: "gt_1h"}, creds: map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:profile user:inference user:sessions:claude_code"}}, Accounts: acct, CCGateway: formalCCFake{}, CCGatewayRuntime: &formalRuntimeFake{}, Acceptance: formalAcceptanceFake{}})
	sess, _ := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "acct"})
	_, _ = svc.TestProxy(context.Background(), sess.ID)
	_, _ = svc.AttestBrowserEgress(context.Background(), sess.ID, FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: "manual"})
	_, _ = svc.GenerateAuthURL(context.Background(), sess.ID)
	_, _ = svc.ExchangeCodeAndCreate(context.Background(), sess.ID, FormalPoolExchangeCodeAndCreateRequest{Code: "oauth-code"})
	_, _ = svc.RunAcceptance(context.Background(), sess.ID)
	if _, err := svc.Activate(context.Background(), sess.ID); err == nil {
		t.Fatalf("expected activate error")
	}
	got, _ := svc.GetSession(context.Background(), sess.ID)
	if got.Status == FormalPoolOnboardingStatusReadyForSmallFlow || acct.activated {
		t.Fatalf("activation half-wrote state")
	}
}

func TestFormalPoolPromoteProductionRequiresWarmingAndEnablesRequestedProfile(t *testing.T) {
	acct := &formalAccountFake{}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{}, OAuth: &formalOAuthFake{summary: FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ScopeContainsClaudeCode: true, ExpiresInBucket: "gt_1h"}, creds: map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:profile user:inference user:sessions:claude_code"}}, Accounts: acct, CCGateway: formalCCFake{}, CCGatewayRuntime: &formalRuntimeFake{}, Acceptance: formalAcceptanceFake{}})
	sess, _ := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "acct", PoolProfile: PoolProfileAggressive})
	_, _ = svc.TestProxy(context.Background(), sess.ID)
	_, _ = svc.AttestBrowserEgress(context.Background(), sess.ID, FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: "manual"})
	_, _ = svc.GenerateAuthURL(context.Background(), sess.ID)
	_, _ = svc.ExchangeCodeAndCreate(context.Background(), sess.ID, FormalPoolExchangeCodeAndCreateRequest{Code: "oauth-code"})
	_, _ = svc.RunAcceptance(context.Background(), sess.ID)

	if _, err := svc.PromoteProduction(context.Background(), sess.ID); err == nil {
		t.Fatalf("production promotion must require warming first")
	}
	_, err := svc.StartWarming(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("start warming: %v", err)
	}
	promoted, err := svc.PromoteProduction(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("promote production: %v", err)
	}
	if promoted.Status != FormalPoolOnboardingStatusProduction || acct.account.Extra[FormalPoolExtraPoolProfileEffective] != PoolProfileAggressive || acct.account.Extra[FormalPoolExtraPoolWeightMode] != FormalPoolWeightNormal {
		t.Fatalf("bad production promotion: session=%#v extra=%#v", promoted, acct.account.Extra)
	}
}

func TestFormalPoolRefreshOnlyRequiresRealRefreshRunner(t *testing.T) {
	acct := &formalAccountFake{}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{}, OAuth: &formalOAuthFake{summary: FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ScopeContainsClaudeCode: true, ExpiresInBucket: "gt_1h"}, creds: map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:profile user:inference user:sessions:claude_code"}}, Accounts: acct})
	sess, _ := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "acct"})
	_, _ = svc.TestProxy(context.Background(), sess.ID)
	_, _ = svc.AttestBrowserEgress(context.Background(), sess.ID, FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: "manual"})
	_, _ = svc.GenerateAuthURL(context.Background(), sess.ID)
	_, _ = svc.ExchangeCodeAndCreate(context.Background(), sess.ID, FormalPoolExchangeCodeAndCreateRequest{Code: "oauth-code"})

	if _, err := svc.RefreshOnly(context.Background(), sess.ID); err == nil {
		t.Fatalf("refresh-only must fail closed without real refresh runner")
	}
}

func TestFormalPoolRefreshOnlyPerformsRefreshBeforeMarkingRefreshed(t *testing.T) {
	acct := &formalAccountFake{}
	oauth := &formalOAuthFake{summary: FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ScopeContainsClaudeCode: true, ExpiresInBucket: "gt_1h"}, creds: map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:profile user:inference user:sessions:claude_code"}, refreshSummary: FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ScopeContainsClaudeCode: true, ExpiresInBucket: "gt_1h"}, refreshCreds: map[string]any{"access_token": "new-access", "refresh_token": "new-refresh", "scope": "user:profile user:inference user:sessions:claude_code"}}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{}, OAuth: oauth, Refresh: oauth, Accounts: acct})
	sess, _ := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "acct"})
	_, _ = svc.TestProxy(context.Background(), sess.ID)
	_, _ = svc.AttestBrowserEgress(context.Background(), sess.ID, FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: "manual"})
	_, _ = svc.GenerateAuthURL(context.Background(), sess.ID)
	_, _ = svc.ExchangeCodeAndCreate(context.Background(), sess.ID, FormalPoolExchangeCodeAndCreateRequest{Code: "oauth-code"})

	got, err := svc.RefreshOnly(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("refresh-only: %v", err)
	}
	if oauth.refreshCalls != 1 || got.Status != FormalPoolOnboardingStatusRefreshed || acct.account.Schedulable || acct.account.Extra[FormalPoolExtraOnboardingStage] != FormalPoolStageRefreshed || acct.account.Credentials["access_token"] != "new-access" {
		t.Fatalf("refresh-only did not refresh and gate account: calls=%d session=%#v account=%#v creds=%#v", oauth.refreshCalls, got, acct.account.Extra, acct.account.Credentials)
	}
}

func TestFormalPoolRefreshOnlyInvalidGrantMarksTerminalCredentialBucket(t *testing.T) {
	acct := &formalAccountFake{}
	oauth := &formalOAuthFake{
		summary:        FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ScopeContainsClaudeCode: true, ExpiresInBucket: "gt_1h"},
		creds:          map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:profile user:inference user:sessions:claude_code"},
		refreshErr:     errors.New("invalid_grant: refresh token revoked"),
		refreshSummary: FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ScopeContainsClaudeCode: true, ExpiresInBucket: "gt_1h"},
	}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{}, OAuth: oauth, Refresh: oauth, Accounts: acct})
	sess, _ := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "acct"})
	_, _ = svc.TestProxy(context.Background(), sess.ID)
	_, _ = svc.AttestBrowserEgress(context.Background(), sess.ID, FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: "manual"})
	_, _ = svc.GenerateAuthURL(context.Background(), sess.ID)
	_, _ = svc.ExchangeCodeAndCreate(context.Background(), sess.ID, FormalPoolExchangeCodeAndCreateRequest{Code: "oauth-code"})

	_, err := svc.RefreshOnly(context.Background(), sess.ID)
	if err == nil {
		t.Fatalf("refresh-only invalid_grant must fail")
	}
	if got := acct.account.GetExtraString(FormalPoolExtraOnboardingLastErrorCode); got != "refresh_token_invalid" {
		t.Fatalf("last error code = %q, want refresh_token_invalid", got)
	}
	if got := acct.account.GetExtraString(FormalPoolExtraQuarantineReason); got != "refresh_token_invalid" {
		t.Fatalf("quarantine reason = %q, want refresh_token_invalid", got)
	}
}

func TestFormalPoolRefreshOnlyInvalidatesTokenAndSyncsSchedulerCache(t *testing.T) {
	acct := &formalAccountFake{}
	oauth := &formalOAuthFake{summary: FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ScopeContainsClaudeCode: true, ExpiresInBucket: "gt_1h"}, creds: map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:profile user:inference user:sessions:claude_code"}, refreshSummary: FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ScopeContainsClaudeCode: true, ExpiresInBucket: "gt_1h"}, refreshCreds: map[string]any{"access_token": "new-access", "refresh_token": "new-refresh", "scope": "user:profile user:inference user:sessions:claude_code"}}
	invalidator := &formalPoolTokenInvalidatorFake{}
	scheduler := &formalPoolSchedulerCacheFake{}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{}, OAuth: oauth, Refresh: oauth, Accounts: acct, CacheInvalidator: invalidator, SchedulerCache: scheduler})
	sess, _ := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "acct"})
	_, _ = svc.TestProxy(context.Background(), sess.ID)
	_, _ = svc.AttestBrowserEgress(context.Background(), sess.ID, FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: "manual"})
	_, _ = svc.GenerateAuthURL(context.Background(), sess.ID)
	_, _ = svc.ExchangeCodeAndCreate(context.Background(), sess.ID, FormalPoolExchangeCodeAndCreateRequest{Code: "oauth-code"})

	_, err := svc.RefreshOnly(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("refresh-only: %v", err)
	}
	if len(invalidator.accounts) != 1 || invalidator.accounts[0].ID != acct.account.ID {
		t.Fatalf("token cache invalidation calls = %#v", invalidator.accounts)
	}
	if len(scheduler.setAccountCalls) != 1 || scheduler.setAccountCalls[0].GetCredential("access_token") != "new-access" {
		t.Fatalf("scheduler sync calls = %#v", scheduler.setAccountCalls)
	}
}

func TestFormalPoolAcceptanceUsesHealthcheckRunnerWhenAcceptanceRunnerMissing(t *testing.T) {
	acct := &formalAccountFake{}
	healthcheck := &formalHealthcheckFake{}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{}, OAuth: &formalOAuthFake{summary: FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ScopeContainsClaudeCode: true, ExpiresInBucket: "gt_1h"}, creds: map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:profile user:inference user:sessions:claude_code"}}, Accounts: acct, CCGateway: formalCCFake{}, CCGatewayRuntime: &formalRuntimeFake{}, Healthcheck: healthcheck})
	sess, _ := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "acct"})
	_, _ = svc.TestProxy(context.Background(), sess.ID)
	_, _ = svc.AttestBrowserEgress(context.Background(), sess.ID, FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: "manual"})
	_, _ = svc.GenerateAuthURL(context.Background(), sess.ID)
	_, _ = svc.ExchangeCodeAndCreate(context.Background(), sess.ID, FormalPoolExchangeCodeAndCreateRequest{Code: "oauth-code"})

	accepted, err := svc.RunAcceptance(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("acceptance: %v", err)
	}
	if healthcheck.calls != 1 || accepted.Status != FormalPoolOnboardingStatusHealthcheckPassed || !accepted.CCGatewaySeen || !accepted.RawCapturePresent || acct.account.Schedulable {
		t.Fatalf("healthcheck runner not enforced: calls=%d accepted=%#v account=%#v", healthcheck.calls, accepted, acct.account)
	}
}

func TestFormalPoolAcceptanceRejectsHealthcheckWithoutRawCapture(t *testing.T) {
	acct := &formalAccountFake{}
	healthcheck := &formalHealthcheckFake{result: &FormalPoolAcceptanceResult{Status: FormalPoolOnboardingStatusHealthcheckPassed, Checks: []FormalPoolAcceptanceCheck{{Name: "directed_healthcheck", Status: "pass"}}, StatusCodeBucket: "status_2xx", CCGatewaySeen: true, RawCapturePresent: false}}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{}, OAuth: &formalOAuthFake{summary: FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ScopeContainsClaudeCode: true, ExpiresInBucket: "gt_1h"}, creds: map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:profile user:inference user:sessions:claude_code"}}, Accounts: acct, CCGateway: formalCCFake{}, CCGatewayRuntime: &formalRuntimeFake{}, Healthcheck: healthcheck})
	sess, _ := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "acct"})
	_, _ = svc.TestProxy(context.Background(), sess.ID)
	_, _ = svc.AttestBrowserEgress(context.Background(), sess.ID, FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: "manual"})
	_, _ = svc.GenerateAuthURL(context.Background(), sess.ID)
	_, _ = svc.ExchangeCodeAndCreate(context.Background(), sess.ID, FormalPoolExchangeCodeAndCreateRequest{Code: "oauth-code"})

	accepted, err := svc.RunAcceptance(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("acceptance: %v", err)
	}
	if accepted.Status != "failed_acceptance" || acct.account.Extra[FormalPoolExtraOnboardingStage] == FormalPoolStageHealthcheckPassed || acct.account.Schedulable {
		t.Fatalf("healthcheck without raw capture must not pass: accepted=%#v extra=%#v", accepted, acct.account.Extra)
	}
}

func TestFormalPoolRunAccountHealthcheckRejectsMissingRuntimeTimestampBeforeRunner(t *testing.T) {
	acct := &formalAccountFake{}
	proxyID := int64(6)
	acct.account = &Account{
		ID:          2,
		Name:        "formal-existing",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: false,
		ProxyID:     &proxyID,
		Credentials: map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:profile user:inference user:sessions:claude_code"},
		Extra: map[string]any{
			"cc_gateway_enabled":               "true",
			"cc_gateway_canary_only":           "false",
			"cc_gateway_policy_version":        ccGatewayAnthropicPolicyVersion,
			"cc_gateway_routes":                string(ccGatewayRouteNativeMessages),
			"cc_gateway_egress_bucket_enabled": "true",
			"cc_gateway_egress_bucket":         "bucket-existing",
			"cc_gateway_account_ref":           "hmac-sha256:" + strings.Repeat("b", 64),
			FormalPoolExtraOnboardingStage:     FormalPoolStageRuntimeRegistered,
			FormalPoolExtraRuntimeRegistered:   "true",
			FormalPoolExtraRuntimeRegisteredAt: "",
		},
	}
	healthcheck := &formalHealthcheckFake{}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Accounts: acct, Healthcheck: healthcheck})

	_, err := svc.RunAccountHealthcheck(context.Background(), 2)
	if err == nil {
		t.Fatalf("account healthcheck must reject incomplete runtime evidence")
	}
	if healthcheck.calls != 0 {
		t.Fatalf("healthcheck runner must not be called before runtime timestamp evidence, calls=%d", healthcheck.calls)
	}
}

func TestFormalPoolRunAcceptanceRejectsMissingPersistedRuntimeTimestampBeforeRunner(t *testing.T) {
	acct := &formalAccountFake{}
	healthcheck := &formalHealthcheckFake{}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{}, OAuth: &formalOAuthFake{summary: FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ScopeContainsClaudeCode: true, ExpiresInBucket: "gt_1h"}, creds: map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:profile user:inference user:sessions:claude_code"}}, Accounts: acct, CCGateway: formalCCFake{}, CCGatewayRuntime: &formalRuntimeFake{}, Healthcheck: healthcheck})
	sess, _ := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "acct"})
	_, _ = svc.TestProxy(context.Background(), sess.ID)
	_, _ = svc.AttestBrowserEgress(context.Background(), sess.ID, FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: "manual"})
	_, _ = svc.GenerateAuthURL(context.Background(), sess.ID)
	_, _ = svc.ExchangeCodeAndCreate(context.Background(), sess.ID, FormalPoolExchangeCodeAndCreateRequest{Code: "oauth-code"})
	acct.account.Extra[FormalPoolExtraRuntimeRegistered] = "true"
	acct.account.Extra[FormalPoolExtraRuntimeRegisteredAt] = ""

	accepted, err := svc.RunAcceptance(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("acceptance should fail closed as a result, not infrastructure error: %v", err)
	}
	if accepted.Status != "failed_acceptance" {
		t.Fatalf("expected failed acceptance, got %#v", accepted)
	}
	if healthcheck.calls != 0 {
		t.Fatalf("acceptance healthcheck runner must not be called before runtime timestamp evidence, calls=%d", healthcheck.calls)
	}
}

func TestFormalPoolRunAccountHealthcheckUsesExistingFormalAccountWithoutSession(t *testing.T) {
	acct := &formalAccountFake{}
	proxyID := int64(6)
	accountRef := "hmac-sha256:" + strings.Repeat("a", 64)
	acct.account = &Account{
		ID:          2,
		Name:        "formal-existing",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: false,
		ProxyID:     &proxyID,
		Concurrency: 10,
		Credentials: map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:profile user:inference user:sessions:claude_code"},
		Extra: map[string]any{
			"cc_gateway_enabled":                 "true",
			"cc_gateway_canary_only":             "false",
			"cc_gateway_policy_version":          ccGatewayAnthropicPolicyVersion,
			"cc_gateway_routes":                  string(ccGatewayRouteNativeMessages),
			"cc_gateway_egress_bucket_enabled":   "true",
			"cc_gateway_egress_bucket":           "bucket-existing",
			"cc_gateway_account_ref":             accountRef,
			"cc_gateway_credential_ref":          ccGatewayGeneratedCredentialRef(accountRef, "1"),
			"cc_gateway_credential_binding_hmac": ccGatewayOAuthCredentialBindingHMAC("formal-pool-runtime-binding-local-test-secret", "access"),
			"cc_gateway_proxy_identity_ref":      formalPoolSafeRef("proxy", "6"),
			"cc_gateway_persona_profile":         ccGatewayDefaultPersonaProfile,
			"claude_code_device_id":              ccGatewayGeneratedDeviceID(accountRef),
			FormalPoolExtraOnboardingStage:       FormalPoolStageRuntimeRegistered,
			FormalPoolExtraRuntimeRegistered:     "true",
			FormalPoolExtraRuntimeRegisteredAt:   "2026-05-29T00:00:00Z",
			FormalPoolExtraPoolProfileRequested:  PoolProfileNormal,
			FormalPoolExtraPoolProfileEffective:  PoolProfileNormal,
			FormalPoolExtraPoolWeightMode:        FormalPoolWeightLow,
		},
		GroupIDs:      []int64{42},
		AccountGroups: []AccountGroup{{AccountID: 2, GroupID: 42}},
	}
	healthcheck := &formalHealthcheckFake{}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Accounts: acct, CCGateway: formalCCFake{}, Healthcheck: healthcheck})

	result, err := svc.RunAccountHealthcheck(context.Background(), 2)
	if err != nil {
		t.Fatalf("account healthcheck: %v", err)
	}

	if healthcheck.calls != 1 {
		t.Fatalf("expected one directed healthcheck call, got %d", healthcheck.calls)
	}
	if result.AccountID != 2 || result.AccountRef != accountRef || result.EgressBucket != "bucket-existing" || result.ProxyRef == "" {
		t.Fatalf("account identity was not preserved: %#v", result)
	}
	if result.Status != FormalPoolOnboardingStatusHealthcheckPassed || !result.CCGatewaySeen || !result.RawCapturePresent {
		t.Fatalf("expected healthcheck pass evidence: %#v", result)
	}
	if acct.account.Schedulable {
		t.Fatalf("account-level healthcheck must not make account schedulable")
	}
	if acct.account.Extra[FormalPoolExtraOnboardingStage] != FormalPoolStageHealthcheckPassed {
		t.Fatalf("successful account healthcheck should record healthcheck stage: %#v", acct.account.Extra)
	}
	if acct.account.Extra[FormalPoolExtraHealthcheckStatus] != "passed" || acct.account.Extra[FormalPoolExtraHealthcheckStatusCodeBucket] != "status_2xx" || acct.account.Extra[FormalPoolExtraHealthcheckRawRef] == "" {
		t.Fatalf("successful account healthcheck should persist full evidence: %#v", acct.account.Extra)
	}
	if acct.account.Extra[FormalPoolExtraHealthcheckCCGatewaySeen] != true || acct.account.Extra[FormalPoolExtraHealthcheckFallbackDetected] != false || acct.account.Extra[FormalPoolExtraHealthcheckProxyMismatch] != false || acct.account.Extra[FormalPoolExtraHealthcheckRiskTextDetected] != false {
		t.Fatalf("successful account healthcheck should persist boolean evidence: %#v", acct.account.Extra)
	}
	if acct.account.Extra[FormalPoolExtraLastHealthcheckResult] != "passed" || stringFromAny(acct.account.Extra[FormalPoolExtraLastHealthcheckAt]) == "" {
		t.Fatalf("successful account healthcheck should persist last result/time: %#v", acct.account.Extra)
	}
}

func TestFormalPoolRunAcceptanceWritesFullHealthcheckEvidence(t *testing.T) {
	acct := &formalAccountFake{}
	rawRef := "hmac-sha256:" + strings.Repeat("a", 64)
	healthcheck := &formalHealthcheckFake{result: &FormalPoolAcceptanceResult{Status: FormalPoolOnboardingStatusHealthcheckPassed, Checks: []FormalPoolAcceptanceCheck{{Name: "directed_healthcheck", Status: "pass"}}, StatusCodeBucket: "status_2xx", CCGatewaySeen: true, RawCapturePresent: true, RawCaptureRef: rawRef, FallbackDetected: false, ProxyMismatch: false, RiskTextDetected: false}}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{}, OAuth: &formalOAuthFake{summary: FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ScopeContainsClaudeCode: true, ExpiresInBucket: "gt_1h"}, creds: map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:profile user:inference user:sessions:claude_code"}}, Accounts: acct, CCGateway: formalCCFake{}, CCGatewayRuntime: &formalRuntimeFake{}, Healthcheck: healthcheck})
	sess, _ := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "acct"})
	_, _ = svc.TestProxy(context.Background(), sess.ID)
	_, _ = svc.AttestBrowserEgress(context.Background(), sess.ID, FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: "manual"})
	_, _ = svc.GenerateAuthURL(context.Background(), sess.ID)
	_, _ = svc.ExchangeCodeAndCreate(context.Background(), sess.ID, FormalPoolExchangeCodeAndCreateRequest{Code: "oauth-code"})

	_, err := svc.RunAcceptance(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("acceptance: %v", err)
	}
	extra := acct.account.Extra
	if extra[FormalPoolExtraHealthcheckStatus] != "passed" || extra[FormalPoolExtraHealthcheckStatusCodeBucket] != "status_2xx" || extra[FormalPoolExtraHealthcheckRawRef] != rawRef {
		t.Fatalf("missing persisted healthcheck status/raw evidence: %#v", extra)
	}
	if extra[FormalPoolExtraLastHealthcheckResult] != "passed" || stringFromAny(extra[FormalPoolExtraLastHealthcheckAt]) == "" {
		t.Fatalf("missing persisted last healthcheck result/time: %#v", extra)
	}
	if extra[FormalPoolExtraHealthcheckCCGatewaySeen] != true || extra[FormalPoolExtraHealthcheckFallbackDetected] != false || extra[FormalPoolExtraHealthcheckProxyMismatch] != false || extra[FormalPoolExtraHealthcheckRiskTextDetected] != false {
		t.Fatalf("missing persisted healthcheck boolean evidence: %#v", extra)
	}
}

func TestFormalPoolRunAcceptanceWritesFailedHealthcheckEvidence(t *testing.T) {
	acct := &formalAccountFake{}
	rawRef := "hmac-sha256:" + strings.Repeat("d", 64)
	healthcheck := &formalHealthcheckFake{result: &FormalPoolAcceptanceResult{Status: "failed_acceptance", Checks: []FormalPoolAcceptanceCheck{{Name: "directed_healthcheck_status_200", Status: "fail", Message: "status_4xx"}}, StatusCodeBucket: "status_4xx", CCGatewaySeen: true, RawCapturePresent: true, RawCaptureRef: rawRef, FallbackDetected: false, ProxyMismatch: false, RiskTextDetected: false, SafeErrorCode: "bad_request", SafeErrorBucket: "request_shape"}}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{}, OAuth: &formalOAuthFake{summary: FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ScopeContainsClaudeCode: true, ExpiresInBucket: "gt_1h"}, creds: map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:profile user:inference user:sessions:claude_code"}}, Accounts: acct, CCGateway: formalCCFake{}, CCGatewayRuntime: &formalRuntimeFake{}, Healthcheck: healthcheck})
	sess, _ := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "acct"})
	_, _ = svc.TestProxy(context.Background(), sess.ID)
	_, _ = svc.AttestBrowserEgress(context.Background(), sess.ID, FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: "manual"})
	_, _ = svc.GenerateAuthURL(context.Background(), sess.ID)
	_, _ = svc.ExchangeCodeAndCreate(context.Background(), sess.ID, FormalPoolExchangeCodeAndCreateRequest{Code: "oauth-code"})

	accepted, err := svc.RunAcceptance(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("acceptance: %v", err)
	}
	if accepted.Status != "failed_acceptance" || accepted.ActivationRequired {
		t.Fatalf("failed healthcheck must not activate: %#v", accepted)
	}
	extra := acct.account.Extra
	if extra[FormalPoolExtraHealthcheckStatus] != "failed" || extra[FormalPoolExtraHealthcheckStatusCodeBucket] != "status_4xx" || extra[FormalPoolExtraHealthcheckRawRef] != rawRef {
		t.Fatalf("failed healthcheck evidence not persisted: %#v", extra)
	}
	if extra[FormalPoolExtraHealthcheckSafeErrorCode] != "bad_request" || extra[FormalPoolExtraHealthcheckSafeErrorBucket] != "request_shape" {
		t.Fatalf("safe error classification not persisted: %#v", extra)
	}
	if acct.account.Schedulable || acct.account.GetExtraString(FormalPoolExtraOnboardingStage) == FormalPoolStageHealthcheckPassed {
		t.Fatalf("failed healthcheck must stay blocked: %#v", acct.account)
	}
}

func TestFormalPoolRunAccountHealthcheckWritesFullFailureEvidence(t *testing.T) {
	acct := &formalAccountFake{}
	proxyID := int64(6)
	accountRef := "hmac-sha256:" + strings.Repeat("b", 64)
	acct.account = &Account{ID: 2, Name: "formal-existing", Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: false, ProxyID: &proxyID, Credentials: map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:profile user:inference user:sessions:claude_code"}, Extra: map[string]any{"cc_gateway_enabled": "true", "cc_gateway_routes": string(ccGatewayRouteNativeMessages), "cc_gateway_egress_bucket_enabled": "true", "cc_gateway_egress_bucket": "bucket-existing", "cc_gateway_account_ref": accountRef, "cc_gateway_credential_ref": ccGatewayGeneratedCredentialRef(accountRef, "1"), "cc_gateway_credential_binding_hmac": ccGatewayOAuthCredentialBindingHMAC("formal-pool-runtime-binding-local-test-secret", "access"), "cc_gateway_proxy_identity_ref": formalPoolSafeRef("proxy", "6"), "cc_gateway_persona_profile": ccGatewayDefaultPersonaProfile, "claude_code_device_id": ccGatewayGeneratedDeviceID(accountRef), FormalPoolExtraOnboardingStage: FormalPoolStageRuntimeRegistered, FormalPoolExtraRuntimeRegistered: "true", FormalPoolExtraRuntimeRegisteredAt: "2026-05-29T00:00:00Z"}}
	healthcheck := &formalHealthcheckFake{result: &FormalPoolAcceptanceResult{Status: "failed_acceptance", Checks: []FormalPoolAcceptanceCheck{{Name: "directed_healthcheck", Status: "fail"}}, StatusCodeBucket: "status_4xx", CCGatewaySeen: true, RawCapturePresent: true, RawCaptureRef: "https://sensitive.example/raw", FallbackDetected: true, ProxyMismatch: false, RiskTextDetected: true}}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Accounts: acct, Healthcheck: healthcheck})

	_, err := svc.RunAccountHealthcheck(context.Background(), 2)
	if err != nil {
		t.Fatalf("account healthcheck: %v", err)
	}
	extra := acct.account.Extra
	if extra[FormalPoolExtraHealthcheckStatus] != "failed" || extra[FormalPoolExtraHealthcheckStatusCodeBucket] != "status_4xx" || extra[FormalPoolExtraLastHealthcheckResult] != "failed" {
		t.Fatalf("failure status evidence not persisted safely: %#v", extra)
	}
	if extra[FormalPoolExtraHealthcheckRawRef] != "" || extra[FormalPoolExtraLastFailureCode] != "formal_pool_healthcheck_failed" || extra[FormalPoolExtraLastFailureSource] != "formal_pool_healthcheck" {
		t.Fatalf("unsafe raw ref or failure source not persisted correctly: %#v", extra)
	}
	if extra[FormalPoolExtraHealthcheckCCGatewaySeen] != true || extra[FormalPoolExtraHealthcheckFallbackDetected] != true || extra[FormalPoolExtraHealthcheckRiskTextDetected] != true {
		t.Fatalf("failure boolean evidence not persisted: %#v", extra)
	}
	if acct.account.Schedulable || acct.account.GetExtraString(FormalPoolExtraOnboardingStage) == FormalPoolStageHealthcheckPassed {
		t.Fatalf("failed healthcheck must stay unschedulable and not mark passed: %#v", acct.account)
	}
}

func TestFormalPoolActivateRejectsIncompletePersistedEvidenceEvenWhenSessionPassed(t *testing.T) {
	acct := &formalAccountFake{}
	rawRef := "hmac-sha256:" + strings.Repeat("c", 64)
	healthcheck := &formalHealthcheckFake{result: &FormalPoolAcceptanceResult{Status: FormalPoolOnboardingStatusHealthcheckPassed, Checks: []FormalPoolAcceptanceCheck{{Name: "directed_healthcheck", Status: "pass"}}, StatusCodeBucket: "status_2xx", CCGatewaySeen: true, RawCapturePresent: true, RawCaptureRef: rawRef}}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{}, OAuth: &formalOAuthFake{summary: FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ScopeContainsClaudeCode: true, ExpiresInBucket: "gt_1h"}, creds: map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:profile user:inference user:sessions:claude_code"}}, Accounts: acct, CCGateway: formalCCFake{}, CCGatewayRuntime: &formalRuntimeFake{}, Healthcheck: healthcheck})
	sess, _ := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "acct"})
	_, _ = svc.TestProxy(context.Background(), sess.ID)
	_, _ = svc.AttestBrowserEgress(context.Background(), sess.ID, FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: "manual"})
	_, _ = svc.GenerateAuthURL(context.Background(), sess.ID)
	_, _ = svc.ExchangeCodeAndCreate(context.Background(), sess.ID, FormalPoolExchangeCodeAndCreateRequest{Code: "oauth-code"})
	_, err := svc.RunAcceptance(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("acceptance: %v", err)
	}
	acct.account.Extra[FormalPoolExtraHealthcheckRawRef] = ""

	_, err = svc.Activate(context.Background(), sess.ID)
	if err == nil {
		t.Fatalf("activation must fail closed when persisted healthcheck evidence is incomplete")
	}
	if acct.activated || acct.account.Schedulable || acct.account.GetExtraString(FormalPoolExtraOnboardingStage) == FormalPoolStageWarming {
		t.Fatalf("activation should not warm account with incomplete persisted evidence: %#v", acct.account.Extra)
	}
}

func TestFormalPoolStartWarmingWritesWarmingUntil(t *testing.T) {
	acct := &formalAccountFake{}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{
		Proxy:            &formalProxyFake{},
		OAuth:            &formalOAuthFake{summary: FormalPoolOAuthTokenSummary{EmailPresent: true, AccountUUIDPresent: true, OrganizationUUIDPresent: true, ScopeContainsUserInference: true, ScopeContainsClaudeCode: true, ExpiresInBucket: "gt_1h"}, creds: map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:profile user:inference user:sessions:claude_code"}},
		Accounts:         acct,
		Acceptance:       formalAcceptanceFake{},
		CCGateway:        formalCCFake{},
		CCGatewayRuntime: &formalRuntimeFake{},
	})
	sess, _ := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "acct"})
	_, _ = svc.TestProxy(context.Background(), sess.ID)
	_, _ = svc.AttestBrowserEgress(context.Background(), sess.ID, FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: "manual"})
	_, _ = svc.GenerateAuthURL(context.Background(), sess.ID)
	_, err := svc.ExchangeCodeAndCreate(context.Background(), sess.ID, FormalPoolExchangeCodeAndCreateRequest{Code: "oauth-code"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = svc.RunAcceptance(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("acceptance: %v", err)
	}
	_, err = svc.StartWarming(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("start warming: %v", err)
	}
	if stringFromAny(acct.account.Extra[FormalPoolExtraWarmingUntil]) == "" {
		t.Fatalf("warming_until must be written: %#v", acct.account.Extra)
	}
	if acct.account.Extra[FormalPoolExtraPoolProfileEffective] != PoolProfileNormal || acct.account.Extra[FormalPoolExtraPoolWeightMode] != FormalPoolWeightLow {
		t.Fatalf("warming must stay normal low weight: %#v", acct.account.Extra)
	}
}

func TestFormalPoolRegisterRuntimeRequiresRefreshOnlyGate(t *testing.T) {
	acct := &formalAccountFake{}
	runtime := &formalRuntimeFake{}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{normalizedProxyURL: "socks5h://proxy.local:1080"}, OAuth: &formalOAuthFake{summary: FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ScopeContainsClaudeCode: true, ExpiresInBucket: "gt_1h"}, creds: map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:profile user:inference user:sessions:claude_code"}}, Accounts: acct, CCGatewayRuntime: runtime})
	sess, _ := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "acct"})
	_, _ = svc.TestProxy(context.Background(), sess.ID)
	_, _ = svc.AttestBrowserEgress(context.Background(), sess.ID, FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: "manual"})
	_, _ = svc.GenerateAuthURL(context.Background(), sess.ID)
	_, err := svc.ExchangeCodeAndCreate(context.Background(), sess.ID, FormalPoolExchangeCodeAndCreateRequest{Code: "oauth-code"})
	if err != nil {
		t.Fatalf("exchange/create: %v", err)
	}

	callsBefore := runtime.calls
	_, err = svc.RegisterRuntime(context.Background(), sess.ID)
	if err == nil {
		t.Fatalf("runtime registration must fail before refresh-only gate")
	}
	if runtime.calls != callsBefore {
		t.Fatalf("runtime registrar must not be called again before refresh-only gate: before=%d after=%d", callsBefore, runtime.calls)
	}
}

func TestFormalPoolRefreshOnlyInvalidatesRuntimeRegistrationWhenCredentialRefreshes(t *testing.T) {
	acct := &formalAccountFake{}
	runtime := &formalRuntimeFake{}
	oauth := &formalOAuthFake{summary: FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ScopeContainsClaudeCode: true, ExpiresInBucket: "gt_1h"}, creds: map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:profile user:inference user:sessions:claude_code"}, refreshSummary: FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ScopeContainsClaudeCode: true, ExpiresInBucket: "gt_1h"}, refreshCreds: map[string]any{"access_token": "new-access", "refresh_token": "new-refresh", "scope": "user:profile user:inference user:sessions:claude_code"}}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{normalizedProxyURL: "socks5h://proxy.local:1080"}, OAuth: oauth, Refresh: oauth, Accounts: acct, CCGatewayRuntime: runtime})
	sess, _ := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "acct"})
	_, _ = svc.TestProxy(context.Background(), sess.ID)
	_, _ = svc.AttestBrowserEgress(context.Background(), sess.ID, FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: "manual"})
	_, _ = svc.GenerateAuthURL(context.Background(), sess.ID)
	_, err := svc.ExchangeCodeAndCreate(context.Background(), sess.ID, FormalPoolExchangeCodeAndCreateRequest{Code: "oauth-code"})
	if err != nil {
		t.Fatalf("exchange/create: %v", err)
	}

	got, err := svc.RefreshOnly(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("refresh-only: %v", err)
	}
	if got.Status != FormalPoolStageRefreshed || got.CCGatewayRuntimeRegistered || acct.account.Extra[FormalPoolExtraOnboardingStage] != FormalPoolStageRefreshed || acct.account.GetExtraString(FormalPoolExtraRuntimeRegistered) != "false" || acct.account.GetExtraString(FormalPoolExtraRuntimeRegisteredAt) != "" || acct.account.Credentials["access_token"] != "new-access" {
		t.Fatalf("refresh-only should invalidate stale runtime registration after credential refresh: session=%#v extra=%#v creds=%#v", got, acct.account.Extra, acct.account.Credentials)
	}
	if acct.account.GetExtraString(FormalPoolExtraCredentialGeneration) != "2" || !ccGatewayCredentialBindingHMACRe.MatchString(acct.account.GetExtraString(ccGatewayExtraCredentialBindingHMAC)) {
		t.Fatalf("refresh-only should rotate credential identity evidence: extra=%#v", acct.account.Extra)
	}
}

func TestFormalPoolRefreshOnlyThenRegisterRuntimePromotesRuntimeRegistered(t *testing.T) {
	acct := &formalAccountFake{}
	runtime := &formalRuntimeFake{}
	oauth := &formalOAuthFake{summary: FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ScopeContainsClaudeCode: true, ExpiresInBucket: "gt_1h"}, creds: map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:profile user:inference user:sessions:claude_code"}, refreshSummary: FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ScopeContainsClaudeCode: true, ExpiresInBucket: "gt_1h"}, refreshCreds: map[string]any{"access_token": "new-access", "refresh_token": "new-refresh", "scope": "user:profile user:inference user:sessions:claude_code"}}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{}, OAuth: oauth, Refresh: oauth, Accounts: acct})
	sess, _ := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "acct"})
	_, _ = svc.TestProxy(context.Background(), sess.ID)
	_, _ = svc.AttestBrowserEgress(context.Background(), sess.ID, FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: "manual"})
	_, _ = svc.GenerateAuthURL(context.Background(), sess.ID)
	_, _ = svc.ExchangeCodeAndCreate(context.Background(), sess.ID, FormalPoolExchangeCodeAndCreateRequest{Code: "oauth-code"})
	_, err := svc.RefreshOnly(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("refresh-only: %v", err)
	}
	svc.ccGatewayRuntime = runtime

	got, err := svc.RegisterRuntime(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("register runtime: %v", err)
	}
	if !runtime.called || got.Status != FormalPoolOnboardingStatusRuntimeRegistered || !got.CCGatewayRuntimeRegistered || acct.account.Extra[FormalPoolExtraOnboardingStage] != FormalPoolStageRuntimeRegistered {
		t.Fatalf("runtime registration did not promote after refresh-only: called=%v session=%#v extra=%#v", runtime.called, got, acct.account.Extra)
	}
	if runtime.input.TokenType != "oauth" || runtime.input.CredentialProof != "Bearer new-access" {
		t.Fatalf("runtime registration after refresh-only must use refreshed server-side credential proof: %#v", runtime.input)
	}
}

func TestFormalPoolRunAcceptanceReturnsErrorWhenHealthcheckStateUpdateFails(t *testing.T) {
	acct := &formalAccountFake{stateUpdateErr: errors.New("db down")}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{}, OAuth: &formalOAuthFake{summary: FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ScopeContainsClaudeCode: true, ExpiresInBucket: "gt_1h"}, creds: map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:profile user:inference user:sessions:claude_code"}}, Accounts: acct, CCGateway: formalCCFake{}, CCGatewayRuntime: &formalRuntimeFake{}, Acceptance: formalAcceptanceFake{}})
	sess, _ := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "acct"})
	_, _ = svc.TestProxy(context.Background(), sess.ID)
	_, _ = svc.AttestBrowserEgress(context.Background(), sess.ID, FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: "manual"})
	_, _ = svc.GenerateAuthURL(context.Background(), sess.ID)
	_, _ = svc.ExchangeCodeAndCreate(context.Background(), sess.ID, FormalPoolExchangeCodeAndCreateRequest{Code: "oauth-code"})

	accepted, err := svc.RunAcceptance(context.Background(), sess.ID)
	if err == nil {
		t.Fatalf("RunAcceptance must return DB update error, got result %#v", accepted)
	}
	session, getErr := svc.GetSession(context.Background(), sess.ID)
	if getErr != nil {
		t.Fatalf("get session: %v", getErr)
	}
	if session.Status == FormalPoolOnboardingStatusHealthcheckPassed || session.HealthcheckPassed {
		t.Fatalf("session must not pass healthcheck when DB source-of-truth update fails: %#v", session)
	}
}
