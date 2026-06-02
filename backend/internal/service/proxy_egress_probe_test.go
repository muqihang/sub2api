package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type proxyEgressProbeRoundTripFunc func(*http.Request) (*http.Response, error)

func (f proxyEgressProbeRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func proxyEgressProbeResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestSideEffectFreeProxyEgressProbeJSONIP(t *testing.T) {
	const endpoint = "https://egress.example.test/ip"
	var gotMethod string
	var gotURL string

	probe := NewSideEffectFreeProxyEgressProbe(ProxyEgressProbeOptions{
		Endpoint: endpoint,
		Timeout:  500 * time.Millisecond,
		Transport: proxyEgressProbeRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotMethod = req.Method
			gotURL = req.URL.String()
			return proxyEgressProbeResponse(http.StatusOK, `{"ip":"203.0.113.10"}`), nil
		}),
	})

	rawIP, err := probe.Probe(context.Background(), 42, "http://user:secret@127.0.0.1:8080")
	if err != nil {
		t.Fatalf("Probe returned error: %v", err)
	}
	if rawIP != "203.0.113.10" {
		t.Fatalf("rawIP = %q, want %q", rawIP, "203.0.113.10")
	}
	if gotMethod != http.MethodGet {
		t.Fatalf("method = %q, want GET", gotMethod)
	}
	if gotURL != endpoint {
		t.Fatalf("endpoint = %q, want %q", gotURL, endpoint)
	}
}

func TestSideEffectFreeProxyEgressProbeUsesDefaultTimeoutForNonPositiveOption(t *testing.T) {
	probe := NewSideEffectFreeProxyEgressProbe(ProxyEgressProbeOptions{Timeout: -time.Second})
	if probe.timeout != DefaultFormalPoolConfig().ProxyEgressProbeTimeout {
		t.Fatalf("timeout = %v, want %v", probe.timeout, DefaultFormalPoolConfig().ProxyEgressProbeTimeout)
	}
}

func TestSideEffectFreeProxyEgressProbeTimeoutReturnsContextError(t *testing.T) {
	probe := NewSideEffectFreeProxyEgressProbe(ProxyEgressProbeOptions{
		Endpoint: "https://egress.example.test/ip",
		Timeout:  10 * time.Millisecond,
		Transport: proxyEgressProbeRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			<-req.Context().Done()
			return nil, req.Context().Err()
		}),
	})

	_, err := probe.Probe(context.Background(), 42, "http://user:secret@127.0.0.1:8080")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context deadline exceeded", err)
	}
	assertProxyEgressProbeSafeError(t, err, "user", "secret", "127.0.0.1:8080")
}

func TestSideEffectFreeProxyEgressProbeContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	probe := NewSideEffectFreeProxyEgressProbe(ProxyEgressProbeOptions{
		Endpoint: "https://egress.example.test/ip",
		Timeout:  time.Second,
		Transport: proxyEgressProbeRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			called = true
			return nil, req.Context().Err()
		}),
	})

	_, err := probe.Probe(ctx, 42, "http://user:secret@127.0.0.1:8080")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
	if called {
		t.Fatal("transport should not be called after parent context is already canceled")
	}
	assertProxyEgressProbeSafeError(t, err, "user", "secret", "127.0.0.1:8080")
}

func TestSideEffectFreeProxyEgressProbeInvalidEndpointResponsesAreSafe(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "empty_ip_json", status: http.StatusOK, body: `{"ip":""}`},
		{name: "invalid_ip_json", status: http.StatusOK, body: `{"ip":"not-an-ip raw-secret-body"}`},
		{name: "invalid_ip_text", status: http.StatusOK, body: `not-an-ip raw-secret-body`},
		{name: "non_200", status: http.StatusInternalServerError, body: `server leaked raw-secret-body`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe := NewSideEffectFreeProxyEgressProbe(ProxyEgressProbeOptions{
				Endpoint: "https://egress.example.test/ip",
				Timeout:  time.Second,
				Transport: proxyEgressProbeRoundTripFunc(func(req *http.Request) (*http.Response, error) {
					return proxyEgressProbeResponse(tt.status, tt.body), nil
				}),
			})

			_, err := probe.Probe(context.Background(), 42, "http://user:secret@127.0.0.1:8080")
			if err == nil {
				t.Fatal("Probe returned nil error for invalid endpoint response")
			}
			assertProxyEgressProbeSafeError(t, err, "user", "secret", "127.0.0.1:8080", "raw-secret-body", tt.body)
		})
	}
}

func TestSideEffectFreeProxyEgressProbeInvalidNormalizedProxyURLIsSafe(t *testing.T) {
	probe := NewSideEffectFreeProxyEgressProbe(ProxyEgressProbeOptions{
		Endpoint: "https://egress.example.test/ip",
		Timeout:  time.Second,
		Transport: proxyEgressProbeRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			t.Fatal("transport must not be called for invalid proxy URL")
			return nil, nil
		}),
	})

	_, err := probe.Probe(context.Background(), 42, "http://user:secret@%zz")
	if err == nil {
		t.Fatal("Probe returned nil error for invalid proxy URL")
	}
	assertProxyEgressProbeSafeError(t, err, "user", "secret", "%zz", "http://user:secret@%zz")
}

func TestSideEffectFreeProxyEgressProbeRequiresFakeTransportInUnitTests(t *testing.T) {
	called := false
	probe := NewSideEffectFreeProxyEgressProbe(ProxyEgressProbeOptions{
		Endpoint: "https://egress.example.test/ip",
		Timeout:  time.Second,
		Transport: proxyEgressProbeRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			called = true
			return proxyEgressProbeResponse(http.StatusOK, `{"ip":"203.0.113.12"}`), nil
		}),
	})

	rawIP, err := probe.Probe(context.Background(), 42, "http://user:secret@127.0.0.1:8080")
	if err != nil {
		t.Fatalf("Probe returned error: %v", err)
	}
	if rawIP != "203.0.113.12" {
		t.Fatalf("rawIP = %q, want %q", rawIP, "203.0.113.12")
	}
	if !called {
		t.Fatal("fake transport was not called")
	}
}

func TestSideEffectFreeProxyEgressProbePlainTextIP(t *testing.T) {
	probe := NewSideEffectFreeProxyEgressProbe(ProxyEgressProbeOptions{
		Endpoint: "https://egress.example.test/ip",
		Timeout:  time.Second,
		Transport: proxyEgressProbeRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			return proxyEgressProbeResponse(http.StatusOK, "203.0.113.11\n"), nil
		}),
	})

	rawIP, err := probe.Probe(context.Background(), 42, "http://127.0.0.1:8080")
	if err != nil {
		t.Fatalf("Probe returned error: %v", err)
	}
	if rawIP != "203.0.113.11" {
		t.Fatalf("rawIP = %q, want %q", rawIP, "203.0.113.11")
	}
}

func assertProxyEgressProbeSafeError(t *testing.T, err error, forbidden ...string) {
	t.Helper()
	msg := err.Error()
	for _, value := range forbidden {
		if value == "" {
			continue
		}
		if strings.Contains(msg, value) {
			t.Fatalf("error %q contains forbidden value %q", msg, value)
		}
	}
}
