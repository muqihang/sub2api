package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/zeromicro/go-zero/core/collection"
)

func TestProvideTimingWheelService_ReturnsError(t *testing.T) {
	original := newTimingWheel
	t.Cleanup(func() { newTimingWheel = original })

	newTimingWheel = func(_ time.Duration, _ int, _ collection.Execute) (*collection.TimingWheel, error) {
		return nil, errors.New("boom")
	}

	svc, err := ProvideTimingWheelService()
	if err == nil {
		t.Fatalf("期望返回 error，但得到 nil")
	}
	if svc != nil {
		t.Fatalf("期望返回 nil svc，但得到非空")
	}
}

func TestProvideTimingWheelService_Success(t *testing.T) {
	svc, err := ProvideTimingWheelService()
	if err != nil {
		t.Fatalf("期望 err 为 nil，但得到: %v", err)
	}
	if svc == nil {
		t.Fatalf("期望 svc 非空，但得到 nil")
	}
	svc.Stop()
}

func TestProvideFormalPoolRuntimeRegistrationStartupReplayUsesCCGatewayRegistrar(t *testing.T) {
	account := formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraOnboardingStage:     FormalPoolStageHealthcheckPassed,
		FormalPoolExtraRuntimeRegistered:   "false",
		FormalPoolExtraRuntimeRegisteredAt: "",
		"cc_gateway_account_ref":           "hmac-sha256:" + strings.Repeat("5", 64),
		"cc_gateway_egress_bucket":         "claude-provider-bucket",
	})
	account.Status = StatusActive
	proxyID := int64(66)
	account.ProxyID = &proxyID
	store := &formalPoolRuntimeReplayStore{accounts: []*Account{account}}
	runtime := &formalPoolOperationsRuntimeFake{}
	proxy := &formalPoolOperationsProxyFake{proxy: &Proxy{ID: 66, Protocol: "socks5h", Host: "provider-proxy.local", Port: 1080, Status: StatusActive}}

	runner := ProvideFormalPoolRuntimeRegistrationStartupReplayWithDeps(store, proxy, runtime, func() time.Time { return time.Date(2026, 5, 30, 3, 4, 5, 0, time.UTC) })

	if runner == nil {
		t.Fatalf("expected startup replay runner")
	}
	if !runtime.called {
		t.Fatalf("startup replay should call runtime registrar")
	}
	if runtime.input.EgressBucket != "claude-provider-bucket" || runtime.input.ProxyURL != "socks5h://provider-proxy.local:1080" {
		t.Fatalf("bad replay registration input: %#v", runtime.input)
	}
	if account.Schedulable || account.GetExtraString(FormalPoolExtraRuntimeRegistered) != "true" || account.GetExtraString(FormalPoolExtraRuntimeRegisteredAt) != "2026-05-30T03:04:05Z" {
		t.Fatalf("startup replay should not make account schedulable and should record timestamp: %#v", account)
	}
}

func TestFormalPoolConfigFromAppConfigParsesValidCIDRAllowlist(t *testing.T) {
	got, err := formalPoolConfigFromAppConfig(&config.Config{FormalPool: config.FormalPoolRuntimeConfig{
		NonceTTL:                 2 * time.Minute,
		EgressMatchCIDRWhitelist: []string{"198.51.100.0/24", "2001:db8::/32"},
	}})
	if err != nil {
		t.Fatalf("formalPoolConfigFromAppConfig() error = %v", err)
	}
	if got.NonceTTL != 2*time.Minute {
		t.Fatalf("NonceTTL = %v, want 2m", got.NonceTTL)
	}
	if len(got.EgressMatchCIDRWhitelist) != 2 {
		t.Fatalf("allowlist len = %d, want 2", len(got.EgressMatchCIDRWhitelist))
	}
	if got.EgressMatchCIDRWhitelist[0].String() != "198.51.100.0/24" || got.EgressMatchCIDRWhitelist[1].String() != "2001:db8::/32" {
		t.Fatalf("unexpected allowlist = %#v", got.EgressMatchCIDRWhitelist)
	}
}

func TestFormalPoolConfigFromAppConfigNormalizesPublicOrigin(t *testing.T) {
	got, err := formalPoolConfigFromAppConfig(&config.Config{FormalPool: config.FormalPoolRuntimeConfig{
		PublicOrigin: " https://public.example.test:8443/ ",
	}})
	if err != nil {
		t.Fatalf("formalPoolConfigFromAppConfig() error = %v", err)
	}
	if got.PublicOrigin != "https://public.example.test:8443" {
		t.Fatalf("PublicOrigin = %q, want normalized origin", got.PublicOrigin)
	}
}

func TestFormalPoolConfigFromAppConfigInvalidPublicOriginFailsClosedAndSanitizesError(t *testing.T) {
	rawInvalid := "http://public.example.test/sensitive-origin"
	_, err := formalPoolConfigFromAppConfig(&config.Config{FormalPool: config.FormalPoolRuntimeConfig{
		PublicOrigin: rawInvalid,
	}})
	if err == nil {
		t.Fatal("formalPoolConfigFromAppConfig() error = nil, want invalid public origin error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "invalid formal_pool public_origin") {
		t.Fatalf("error %q missing sanitized reason", msg)
	}
	if strings.Contains(msg, rawInvalid) || strings.Contains(msg, "sensitive-origin") {
		t.Fatalf("error leaked raw public origin: %q", msg)
	}
}

func TestFormalPoolConfigFromAppConfigInvalidCIDRFailsClosedAndSanitizesError(t *testing.T) {
	rawInvalid := "203.0.113.7/not-a-prefix"
	_, err := formalPoolConfigFromAppConfig(&config.Config{FormalPool: config.FormalPoolRuntimeConfig{
		EgressMatchCIDRWhitelist: []string{rawInvalid},
	}})
	if err == nil {
		t.Fatalf("formalPoolConfigFromAppConfig() error = nil, want invalid CIDR error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "invalid formal_pool egress_match_cidr_whitelist") {
		t.Fatalf("error %q missing sanitized reason", msg)
	}
	if strings.Contains(msg, rawInvalid) || strings.Contains(msg, "not-a-prefix") || strings.Contains(msg, "203.0.113.7") {
		t.Fatalf("error leaked raw invalid CIDR: %q", msg)
	}
}

func TestFormalPoolConfigFromAppConfigMapsRateLimitAndSecret(t *testing.T) {
	rawSecret := strings.Repeat("ab", 32)
	got, err := formalPoolConfigFromAppConfig(&config.Config{FormalPool: config.FormalPoolRuntimeConfig{
		NonceTTL:                    7 * time.Minute,
		EgressMatchCIDRWhitelist:    []string{"198.51.100.0/24"},
		ProxyEgressCacheSuccessTTL:  45 * time.Second,
		ProxyEgressCacheFailureTTL:  9 * time.Second,
		ProxyEgressProbeTimeout:     2 * time.Second,
		PublicRouteRatePerNonce:     4,
		PublicRouteRatePerIP:        5,
		PublicRouteTotalPerNonce:    6,
		PublicRouteFallbackPerIP:    7,
		PublicRouteConstantDelayMin: 11 * time.Millisecond,
		PublicRouteConstantDelayMax: 22 * time.Millisecond,
		RateLimitHMACSecret:         rawSecret,
	}})
	if err != nil {
		t.Fatalf("formalPoolConfigFromAppConfig() error = %v", err)
	}
	if len(got.RateLimitHMACSecret) != 32 {
		t.Fatalf("RateLimitHMACSecret len = %d, want 32", len(got.RateLimitHMACSecret))
	}
	if got.PublicRouteRatePerNonce != 4 || got.PublicRouteRatePerIP != 5 || got.PublicRouteTotalPerNonce != 6 || got.PublicRouteFallbackPerIP != 7 {
		t.Fatalf("rate limit values not mapped: %#v", got)
	}
	if got.ProxyEgressCacheSuccessTTL != 45*time.Second || got.ProxyEgressCacheFailureTTL != 9*time.Second || got.ProxyEgressProbeTimeout != 2*time.Second {
		t.Fatalf("proxy egress values not mapped: %#v", got)
	}
	if got.PublicRouteConstantDelayMin != 11*time.Millisecond || got.PublicRouteConstantDelayMax != 22*time.Millisecond {
		t.Fatalf("public route delay values not mapped: %#v", got)
	}
}

func TestFormalPoolConfigFromAppConfigInvalidHMACSecretFailsClosedAndSanitizesError(t *testing.T) {
	rawSecret := "not-a-64-hex-secret-with-sensitive-value"
	_, err := formalPoolConfigFromAppConfig(&config.Config{FormalPool: config.FormalPoolRuntimeConfig{
		RateLimitHMACSecret: rawSecret,
	}})
	if err == nil {
		t.Fatalf("formalPoolConfigFromAppConfig() error = nil, want invalid hmac secret error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "invalid formal_pool rate_limit_hmac_secret") {
		t.Fatalf("error %q missing sanitized reason", msg)
	}
	if strings.Contains(msg, rawSecret) || strings.Contains(msg, "sensitive-value") || strings.Contains(msg, "not-a-64") {
		t.Fatalf("error leaked raw hmac secret: %q", msg)
	}
}

func TestFormalPoolProvidersReturnProductionPublicDeps(t *testing.T) {
	cfg, err := ProvideFormalPoolConfig(&config.Config{FormalPool: config.FormalPoolRuntimeConfig{
		RateLimitHMACSecret: strings.Repeat("cd", 32),
	}})
	if err != nil {
		t.Fatalf("ProvideFormalPoolConfig() error = %v", err)
	}
	if len(cfg.RateLimitHMACSecret) != 32 {
		t.Fatalf("provided hmac secret len = %d, want 32", len(cfg.RateLimitHMACSecret))
	}
	if got := ProvideFormalPoolRiskEventWriter(); got == nil {
		t.Fatal("ProvideFormalPoolRiskEventWriter() returned nil")
	}
	if got := ProvideFormalPoolEgressRateLimiter(cfg); got == nil {
		t.Fatal("ProvideFormalPoolEgressRateLimiter() returned nil")
	}
}
