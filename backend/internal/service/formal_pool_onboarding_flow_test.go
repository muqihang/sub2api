package service

import (
	"context"
	"errors"
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

type formalOAuthFake struct {
	summary         FormalPoolOAuthTokenSummary
	creds           map[string]any
	err             error
	lastCookieScope string
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

type formalAccountFake struct {
	account     *Account
	created     FormalPoolAccountCreateInput
	activateErr error
	activated   bool
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
func (f *formalAccountFake) ActivateFormalPoolAccount(ctx context.Context, id int64, extra map[string]any) (*Account, error) {
	if f.activateErr != nil {
		return nil, f.activateErr
	}
	f.activated = true
	f.account.Schedulable = true
	for k, v := range extra {
		f.account.Extra[k] = v
	}
	return f.account, nil
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

type formalRuntimeFake struct {
	called bool
	input  FormalPoolCCGatewayRuntimeRegistration
	err    error
}

func (f *formalRuntimeFake) RegisterCCGatewayRuntime(ctx context.Context, input FormalPoolCCGatewayRuntimeRegistration) error {
	if f.err != nil {
		return f.err
	}
	f.called = true
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
	if acct.created.Extra["cc_gateway_enabled"] != "true" || acct.created.Extra["cc_gateway_canary_only"] != "false" || acct.created.Extra["cc_gateway_routes"] != "native_messages" || acct.created.Extra["pool_profile"] != "aggressive" || acct.created.Extra["oauth_refresh_fail_closed"] != "true" {
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
	if !got.CCGatewayRuntimeRegistered {
		t.Fatalf("session must expose runtime registration status")
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

func TestFormalPoolSetupTokenCookieCreateRegistersRuntimeAndKeepsAccountUnschedulable(t *testing.T) {
	acct := &formalAccountFake{}
	runtime := &formalRuntimeFake{}
	oauth := &formalOAuthFake{summary: FormalPoolOAuthTokenSummary{EmailPresent: true, AccountUUIDPresent: true, OrganizationUUIDPresent: true, ScopeContainsUserInference: true, ScopeContainsClaudeCode: false, ExpiresInBucket: "gt_1h"}, creds: map[string]any{"access_token": "setup-access", "refresh_token": "setup-refresh", "token_type": "Bearer", "expires_in": int64(31536000), "scope": "user:inference"}}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{normalizedProxyURL: "http://proxy.local:443"}, OAuth: oauth, Accounts: acct, CCGatewayRuntime: runtime})
	sess, _ := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "setup-acct", PoolProfile: "normal"})
	_, _ = svc.TestProxy(context.Background(), sess.ID)
	_, _ = svc.AttestBrowserEgress(context.Background(), sess.ID, FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: "manual"})
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
	if !runtime.called || runtime.input.AccountRef != got.AccountRef || runtime.input.EgressBucket != got.EgressBucket || runtime.input.ProxyURL != "http://proxy.local:443" {
		t.Fatalf("runtime registration missing or wrong: %#v got=%#v", runtime.input, got)
	}
	if !got.CCGatewayRuntimeRegistered || got.OAuthSummary == nil || !got.OAuthSummary.ScopeContainsUserInference || got.OAuthSummary.ScopeContainsClaudeCode {
		t.Fatalf("bad setup-token safe summary/session: %#v", got)
	}
	assertNoFormalPoolSensitive(t, got)
}

func TestFormalPoolSetupTokenCookieRejectsFullClaudeCodeScope(t *testing.T) {
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{}, OAuth: &formalOAuthFake{summary: FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ScopeContainsClaudeCode: true}, creds: map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:profile user:inference user:sessions:claude_code"}}, Accounts: &formalAccountFake{}, CCGatewayRuntime: &formalRuntimeFake{}})
	sess, _ := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "acct"})
	_, _ = svc.TestProxy(context.Background(), sess.ID)
	_, _ = svc.AttestBrowserEgress(context.Background(), sess.ID, FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: "manual"})
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
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{}, OAuth: &formalOAuthFake{summary: FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ScopeContainsClaudeCode: true, ExpiresInBucket: "gt_1h"}, creds: map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:profile user:inference user:sessions:claude_code"}}, Accounts: acct, CCGateway: formalCCFake{}, CCGatewayRuntime: &formalRuntimeFake{}})
	sess, _ := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "acct"})
	_, _ = svc.TestProxy(context.Background(), sess.ID)
	_, _ = svc.AttestBrowserEgress(context.Background(), sess.ID, FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: "manual"})
	_, _ = svc.GenerateAuthURL(context.Background(), sess.ID)
	_, _ = svc.ExchangeCodeAndCreate(context.Background(), sess.ID, FormalPoolExchangeCodeAndCreateRequest{Code: "oauth-code"})
	accepted, err := svc.RunAcceptance(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("acceptance: %v", err)
	}
	if accepted.Status != "pending_activation" || !accepted.ActivationRequired || !accepted.NoRealMessagesRequestPerformed {
		t.Fatalf("bad acceptance: %#v", accepted)
	}
	if acct.account.Schedulable {
		t.Fatalf("acceptance must not activate account")
	}
	activated, err := svc.Activate(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	if activated.Status != FormalPoolOnboardingStatusReadyForSmallFlow || !acct.activated || !acct.account.Schedulable {
		t.Fatalf("activation failed: %#v", activated)
	}
}

func TestFormalPoolActivationFailDoesNotUpdateState(t *testing.T) {
	acct := &formalAccountFake{activateErr: errors.New("db down")}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Proxy: &formalProxyFake{}, OAuth: &formalOAuthFake{summary: FormalPoolOAuthTokenSummary{ScopeContainsUserInference: true, ScopeContainsClaudeCode: true, ExpiresInBucket: "gt_1h"}, creds: map[string]any{"access_token": "access", "refresh_token": "refresh", "scope": "user:profile user:inference user:sessions:claude_code"}}, Accounts: acct, CCGateway: formalCCFake{}, CCGatewayRuntime: &formalRuntimeFake{}})
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
