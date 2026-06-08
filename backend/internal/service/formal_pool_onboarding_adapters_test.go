package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
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

func timedOutContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	t.Cleanup(cancel)
	<-ctx.Done()
	return ctx
}
