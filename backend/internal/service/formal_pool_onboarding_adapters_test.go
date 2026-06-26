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
	"github.com/stretchr/testify/require"
)

type formalPoolProxyVerifierAdminFake struct {
	proxy           *Proxy
	getProxyCalls   int
	testProxyCalls  int
	failOnTestProxy bool
}

func (f *formalPoolProxyVerifierAdminFake) GetProxy(ctx context.Context, id int64) (*Proxy, error) {
	f.getProxyCalls++
	if f.proxy != nil {
		return f.proxy, nil
	}
	return &Proxy{ID: id, Protocol: "http", Host: "proxy.example.test", Port: 8080, Username: "user", Password: "secret", Status: StatusActive}, nil
}

func (f *formalPoolProxyVerifierAdminFake) CreateProxy(ctx context.Context, input *CreateProxyInput) (*Proxy, error) {
	return &Proxy{ID: 42, Protocol: input.Protocol, Host: input.Host, Port: input.Port, Username: input.Username, Password: input.Password, Status: StatusActive}, nil
}

func (f *formalPoolProxyVerifierAdminFake) TestProxy(ctx context.Context, id int64) (*ProxyTestResult, error) {
	f.testProxyCalls++
	if f.failOnTestProxy {
		panic("AdminService.TestProxy must not be called by GetRawEgressIP")
	}
	return &ProxyTestResult{Success: true, IPAddress: "203.0.113.99", LatencyMs: 123}, nil
}

func TestFormalPoolAdminProxyVerifierGetRawEgressIPUsesProbeAndCache(t *testing.T) {
	admin := &formalPoolProxyVerifierAdminFake{failOnTestProxy: true}
	cache := NewProxyEgressCache(ProxyEgressCacheOptions{SuccessTTL: time.Minute, FailureTTL: time.Minute})
	var calls int
	probe := func(ctx context.Context, proxyID int64, normalizedProxyURL string) (string, error) {
		calls++
		if proxyID != 9 {
			t.Fatalf("proxyID = %d, want 9", proxyID)
		}
		if normalizedProxyURL != "http://user:secret@proxy.example.test:8080" {
			t.Fatalf("normalizedProxyURL = %q", normalizedProxyURL)
		}
		return "203.0.113.10", nil
	}
	verifier := NewFormalPoolAdminProxyVerifierWithEgressProbe(admin, cache, probe)

	first, err := verifier.GetRawEgressIP(context.Background(), 9, "http://user:secret@proxy.example.test:8080")
	if err != nil {
		t.Fatalf("first GetRawEgressIP error = %v", err)
	}
	second, err := verifier.GetRawEgressIP(context.Background(), 9, "http://user:secret@proxy.example.test:8080")
	if err != nil {
		t.Fatalf("second GetRawEgressIP error = %v", err)
	}
	if first != "203.0.113.10" || second != first {
		t.Fatalf("unexpected raw IPs: first=%q second=%q", first, second)
	}
	if calls != 1 {
		t.Fatalf("probe calls = %d, want 1", calls)
	}
	if admin.testProxyCalls != 0 {
		t.Fatalf("AdminService.TestProxy calls = %d, want 0", admin.testProxyCalls)
	}
	if admin.getProxyCalls != 0 {
		t.Fatalf("AdminService.GetProxy calls = %d, want 0 for raw egress probe", admin.getProxyCalls)
	}
}

func TestFormalPoolAdminProxyVerifierGetRawEgressIPCachesFailure(t *testing.T) {
	admin := &formalPoolProxyVerifierAdminFake{failOnTestProxy: true}
	cache := NewProxyEgressCache(ProxyEgressCacheOptions{SuccessTTL: time.Minute, FailureTTL: time.Minute})
	errProbe := errors.New("probe unavailable")
	var calls int
	probe := func(ctx context.Context, proxyID int64, normalizedProxyURL string) (string, error) {
		calls++
		return "", errProbe
	}
	verifier := NewFormalPoolAdminProxyVerifierWithEgressProbe(admin, cache, probe)

	_, err := verifier.GetRawEgressIP(context.Background(), 9, "http://user:secret@proxy.example.test:8080")
	if !errors.Is(err, errProbe) {
		t.Fatalf("first error = %v, want probe error", err)
	}
	_, err = verifier.GetRawEgressIP(context.Background(), 9, "http://user:secret@proxy.example.test:8080")
	if !errors.Is(err, errProbe) {
		t.Fatalf("second error = %v, want cached probe error", err)
	}
	if calls != 1 {
		t.Fatalf("probe calls = %d, want 1", calls)
	}
	if admin.testProxyCalls != 0 {
		t.Fatalf("AdminService.TestProxy calls = %d, want 0", admin.testProxyCalls)
	}
}

func TestFormalPoolAdminProxyVerifierGetRawEgressIPSafeInvalidURLAndTimeoutErrors(t *testing.T) {
	admin := &formalPoolProxyVerifierAdminFake{failOnTestProxy: true}
	for _, tt := range []struct {
		name string
		ctx  context.Context
		url  string
	}{
		{name: "invalid_url", ctx: context.Background(), url: "http://user:secret@%zz"},
		{name: "timeout", ctx: timedOutContext(t), url: "http://user:secret@proxy.example.test:8080"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			probe := NewSideEffectFreeProxyEgressProbe(ProxyEgressProbeOptions{Timeout: time.Nanosecond})
			verifier := NewFormalPoolAdminProxyVerifierWithEgressProbe(admin, NewProxyEgressCache(ProxyEgressCacheOptions{SuccessTTL: time.Minute, FailureTTL: time.Minute}), probe.Probe)

			_, err := verifier.GetRawEgressIP(tt.ctx, 9, tt.url)
			if err == nil {
				t.Fatal("GetRawEgressIP returned nil error")
			}
			msg := err.Error()
			for _, forbidden := range []string{"user", "secret", "%zz", "proxy.example.test:8080", tt.url} {
				if forbidden != "" && strings.Contains(msg, forbidden) {
					t.Fatalf("error %q contains forbidden value %q", msg, forbidden)
				}
			}
			if admin.testProxyCalls != 0 {
				t.Fatalf("AdminService.TestProxy calls = %d, want 0", admin.testProxyCalls)
			}
		})
	}
}

func TestFormalPoolAdminProxyVerifierTestProxyInvalidatesRawEgressCache(t *testing.T) {
	admin := &formalPoolProxyVerifierAdminFake{}
	cache := NewProxyEgressCache(ProxyEgressCacheOptions{SuccessTTL: time.Minute, FailureTTL: time.Minute})
	ips := []string{"203.0.113.10", "203.0.113.11"}
	var calls int
	probe := func(ctx context.Context, proxyID int64, normalizedProxyURL string) (string, error) {
		calls++
		return ips[calls-1], nil
	}
	verifier := NewFormalPoolAdminProxyVerifierWithEgressProbe(admin, cache, probe)

	first, err := verifier.GetRawEgressIP(context.Background(), 9, "http://user:secret@proxy.example.test:8080")
	if err != nil {
		t.Fatalf("first GetRawEgressIP error = %v", err)
	}
	if first != "203.0.113.10" {
		t.Fatalf("first raw IP = %q, want 203.0.113.10", first)
	}

	summary, err := verifier.TestProxy(context.Background(), 9)
	if err != nil {
		t.Fatalf("TestProxy error = %v", err)
	}
	if !summary.Success {
		t.Fatal("TestProxy summary.Success = false, want true")
	}
	if admin.testProxyCalls != 1 {
		t.Fatalf("AdminService.TestProxy calls = %d, want 1", admin.testProxyCalls)
	}

	second, err := verifier.GetRawEgressIP(context.Background(), 9, "http://user:secret@proxy.example.test:8080")
	if err != nil {
		t.Fatalf("second GetRawEgressIP error = %v", err)
	}
	if second != "203.0.113.11" {
		t.Fatalf("second raw IP = %q, want 203.0.113.11 after invalidation", second)
	}
	if calls != 2 {
		t.Fatalf("probe calls = %d, want 2", calls)
	}
}

func TestFormalPoolAdminProxyVerifierInvalidateProxyEgressReprobes(t *testing.T) {
	admin := &formalPoolProxyVerifierAdminFake{failOnTestProxy: true}
	cache := NewProxyEgressCache(ProxyEgressCacheOptions{SuccessTTL: time.Minute, FailureTTL: time.Minute})
	ips := []string{"203.0.113.20", "203.0.113.21"}
	var calls int
	probe := func(ctx context.Context, proxyID int64, normalizedProxyURL string) (string, error) {
		calls++
		return ips[calls-1], nil
	}
	verifier := NewFormalPoolAdminProxyVerifierWithEgressProbe(admin, cache, probe)

	first, err := verifier.GetRawEgressIP(context.Background(), 9, "http://user:secret@proxy.example.test:8080")
	if err != nil {
		t.Fatalf("first GetRawEgressIP error = %v", err)
	}
	if first != "203.0.113.20" {
		t.Fatalf("first raw IP = %q, want 203.0.113.20", first)
	}

	verifier.InvalidateProxyEgress(9)

	second, err := verifier.GetRawEgressIP(context.Background(), 9, "http://user:secret@proxy.example.test:8080")
	if err != nil {
		t.Fatalf("second GetRawEgressIP error = %v", err)
	}
	if second != "203.0.113.21" {
		t.Fatalf("second raw IP = %q, want 203.0.113.21 after invalidation", second)
	}
	if calls != 2 {
		t.Fatalf("probe calls = %d, want 2", calls)
	}
	if admin.testProxyCalls != 0 {
		t.Fatalf("AdminService.TestProxy calls = %d, want 0", admin.testProxyCalls)
	}
}

func TestFormalPoolHTTPCCGatewayRuntimeRegistrarSendsCompleteAuthorityContract(t *testing.T) {
	t.Parallel()

	var gotHeader http.Header
	var gotPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/_runtime/register-account", r.URL.Path)
		gotHeader = r.Header.Clone()
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotPayload))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"registered"}`))
	}))
	defer server.Close()

	registrar := NewFormalPoolHTTPCCGatewayRuntimeRegistrar(&config.Config{
		Gateway: config.GatewayConfig{CCGateway: config.GatewayCCGatewayConfig{
			Enabled:              true,
			BaseURL:              server.URL,
			Token:                "gateway-control-material-v1-local-test",
			InternalControlToken: "internal-control-material-v1-local-test",
			TimeoutSeconds:       1,
		}},
	})
	require.NotNil(t, registrar)

	err := registrar.RegisterCCGatewayRuntime(context.Background(), FormalPoolCCGatewayRuntimeRegistration{
		AccountRef:            "hmac-sha256:" + strings.Repeat("a", 64),
		CredentialRef:         "opaque:credential-ref:v1:cred-a",
		CredentialBindingHMAC: "hmac-sha256:" + strings.Repeat("b", 64),
		TokenType:             "oauth",
		CredentialProof:       "Bearer fixture-credential-proof",
		EgressBucket:          "bucket-a",
		ProxyURL:              "http://127.0.0.1:8080",
		ProxyRef:              "opaque:proxy-ref:v1:bucket-a",
		PolicyVersion:         ccGatewayAnthropicPolicyVersion,
		PersonaVariant:        "claude-code-" + ccGatewayAnthropicPolicyVersion + "-macos-local",
		SessionPolicy:         "preserve_downstream_session_id",
		DeviceID:              strings.Repeat("c", 64),
	})
	require.NoError(t, err)

	require.Equal(t, "gateway-control-material-v1-local-test", gotHeader.Get("X-CC-Gateway-Token"))
	require.Equal(t, "internal-control-material-v1-local-test", gotHeader.Get("X-CC-Internal-Control-Token"))
	require.Equal(t, "Bearer fixture-credential-proof", gotHeader.Get("Authorization"))
	require.Equal(t, "hmac-sha256:"+strings.Repeat("a", 64), gotPayload["account_id"])
	require.Equal(t, "opaque:credential-ref:v1:cred-a", gotPayload["credential_ref"])
	require.Equal(t, "hmac-sha256:"+strings.Repeat("b", 64), gotPayload["credential_binding_hmac"])
	require.Equal(t, "oauth", gotPayload["token_type"])
	require.Equal(t, strings.Repeat("c", 64), gotPayload["device_id"])
	require.Equal(t, "bucket-a", gotPayload["egress_bucket"])
	require.Equal(t, "opaque:proxy-ref:v1:bucket-a", gotPayload["proxy_identity_ref"])
	rawPayload, err := json.Marshal(gotPayload)
	require.NoError(t, err)
	require.NotContains(t, string(rawPayload), "fixture-credential-proof")
	require.NotContains(t, string(rawPayload), "Authorization")
	require.Empty(t, gotHeader.Get("X-API-Key"))
}

func TestFormalPoolHTTPCCGatewayRuntimeRegistrarFailureRedactsControlPlaneBody(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		status int
		code   string
	}{
		{name: "missing_internal_control_attestation", status: http.StatusForbidden, code: "missing_internal_control_attestation"},
		{name: "missing_device_id", status: http.StatusBadRequest, code: "missing_device_id"},
		{name: "sign_primary_2177_oracle_missing", status: http.StatusForbidden, code: "sign_primary_2177_oracle_missing"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-CC-Gateway-Error-Kind", "control-plane")
				w.Header().Set("X-CC-Gateway-Error-Code", "authorization=cp4-leaked-header-secret")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"error":{"type":"cc_gateway_control_plane","code":"` + tt.code + `","message":"authorization=cp4-leaked-body-secret raw_prompt cp4 raw_body cp4 raw_telemetry cp4 raw_cch cp4 account acct-email-sentinel acct-uuid-sentinel proxy_credential=cp4-proxy-secret credential=hmac-input-secret"}}`))
			}))
			defer server.Close()

			registrar := NewFormalPoolHTTPCCGatewayRuntimeRegistrar(&config.Config{
				Gateway: config.GatewayConfig{CCGateway: config.GatewayCCGatewayConfig{
					Enabled:              true,
					BaseURL:              server.URL,
					Token:                "gateway-control-material-v1-local-test",
					InternalControlToken: "internal-control-material-v1-local-test",
					TimeoutSeconds:       1,
				}},
			})
			require.NotNil(t, registrar)

			err := registrar.RegisterCCGatewayRuntime(context.Background(), FormalPoolCCGatewayRuntimeRegistration{
				AccountRef:            "hmac-sha256:" + strings.Repeat("a", 64),
				CredentialRef:         "opaque:credential-ref:v1:cred-a",
				CredentialBindingHMAC: "hmac-sha256:" + strings.Repeat("b", 64),
				TokenType:             "oauth",
				CredentialProof:       "fixture-credential-proof-secret",
				EgressBucket:          "bucket-a",
				ProxyURL:              "http://127.0.0.1:8080",
				ProxyRef:              "opaque:proxy-ref:v1:bucket-a",
				PolicyVersion:         ccGatewayAnthropicPolicyVersion,
				PersonaVariant:        "claude-code-" + ccGatewayAnthropicPolicyVersion + "-macos-local",
				SessionPolicy:         "preserve_downstream_session_id",
				DeviceID:              strings.Repeat("c", 64),
			})

			require.Error(t, err)
			msg := err.Error()
			require.Contains(t, msg, "status")
			require.Contains(t, msg, tt.code)
			for _, forbidden := range []string{
				"fixture-credential-proof-secret",
				"cp4-leaked-header-secret",
				"cp4-leaked-body-secret",
				"Authorization",
				"raw_prompt",
				"raw_body",
				"raw_telemetry",
				"raw_cch",
				"acct-email-sentinel",
				"acct-uuid-sentinel",
				"cp4-proxy-secret",
				"hmac-input-secret",
			} {
				require.NotContains(t, msg, forbidden)
			}
		})
	}
}

func TestFormalPoolHTTPCCGatewayRuntimeRegistrarFailsClosedWithoutCredentialProof(t *testing.T) {
	t.Parallel()

	registrar := NewFormalPoolHTTPCCGatewayRuntimeRegistrar(&config.Config{
		Gateway: config.GatewayConfig{CCGateway: config.GatewayCCGatewayConfig{
			Enabled:              true,
			BaseURL:              "http://127.0.0.1:1",
			Token:                "gateway-control-material-v1-local-test",
			InternalControlToken: "internal-control-material-v1-local-test",
			TimeoutSeconds:       1,
		}},
	})
	require.NotNil(t, registrar)

	base := FormalPoolCCGatewayRuntimeRegistration{
		AccountRef:            "hmac-sha256:" + strings.Repeat("a", 64),
		CredentialRef:         "opaque:credential-ref:v1:cred-a",
		CredentialBindingHMAC: "hmac-sha256:" + strings.Repeat("b", 64),
		EgressBucket:          "bucket-a",
		ProxyURL:              "http://127.0.0.1:8080",
		ProxyRef:              "opaque:proxy-ref:v1:bucket-a",
		PolicyVersion:         ccGatewayAnthropicPolicyVersion,
		PersonaVariant:        "claude-code-" + ccGatewayAnthropicPolicyVersion + "-macos-local",
		SessionPolicy:         "preserve_downstream_session_id",
		DeviceID:              strings.Repeat("c", 64),
	}

	for _, tt := range []struct {
		name  string
		input FormalPoolCCGatewayRuntimeRegistration
	}{
		{name: "missing_token_type", input: func() FormalPoolCCGatewayRuntimeRegistration {
			in := base
			in.CredentialProof = "Bearer fixture-credential-proof"
			return in
		}()},
		{name: "missing_proof", input: func() FormalPoolCCGatewayRuntimeRegistration {
			in := base
			in.TokenType = "oauth"
			return in
		}()},
		{name: "unsupported_token_type", input: func() FormalPoolCCGatewayRuntimeRegistration {
			in := base
			in.TokenType = "bearer"
			in.CredentialProof = "Bearer fixture-credential-proof"
			return in
		}()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := registrar.RegisterCCGatewayRuntime(context.Background(), tt.input)
			require.Error(t, err)
			require.Contains(t, err.Error(), "credential proof")
			require.NotContains(t, err.Error(), "fixture-credential-proof")
		})
	}
}

func timedOutContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	t.Cleanup(cancel)
	<-ctx.Done()
	return ctx
}
