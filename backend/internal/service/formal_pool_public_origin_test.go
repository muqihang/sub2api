package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeFormalPoolPublicOrigin(t *testing.T) {
	t.Parallel()

	valid := []struct {
		name string
		raw  string
		want string
	}{
		{name: "missing optional origin", raw: "", want: ""},
		{name: "whitespace-only optional origin", raw: "  \t\n", want: ""},
		{name: "https domain", raw: "https://public.example.test", want: "https://public.example.test"},
		{name: "https trailing slash", raw: " https://public.example.test/ ", want: "https://public.example.test"},
		{name: "https explicit port", raw: "https://public.example.test:8443/", want: "https://public.example.test:8443"},
		{name: "loopback localhost", raw: "http://localhost:8080/", want: "http://localhost:8080"},
		{name: "loopback IPv4", raw: "http://127.77.0.1:8080", want: "http://127.77.0.1:8080"},
		{name: "loopback IPv6", raw: "http://[::1]:8080/", want: "http://[::1]:8080"},
		{name: "https IPv6", raw: "https://[2001:db8::1]:8443/", want: "https://[2001:db8::1]:8443"},
	}
	for _, tc := range valid {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeFormalPoolPublicOrigin(tc.raw)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}

	invalid := []struct {
		name string
		raw  string
	}{
		{name: "missing host", raw: "https://"},
		{name: "opaque", raw: "https:public.example.test"},
		{name: "userinfo", raw: "https://user:secret@public.example.test"},
		{name: "path", raw: "https://public.example.test/admin"},
		{name: "encoded path", raw: "https://public.example.test/%2f"},
		{name: "query", raw: "https://public.example.test/?next=evil"},
		{name: "empty query marker", raw: "https://public.example.test/?"},
		{name: "fragment", raw: "https://public.example.test/#fragment"},
		{name: "non HTTP scheme", raw: "ftp://public.example.test"},
		{name: "non-loopback HTTP", raw: "http://public.example.test"},
		{name: "IPv4-like non-loopback HTTP", raw: "http://127.0.0.1.example.test"},
		{name: "IPv4-mapped non-loopback HTTP", raw: "http://[::ffff:192.0.2.1]"},
		{name: "IPv6 zone", raw: "http://[::1%25lo0]"},
		{name: "empty port", raw: "https://public.example.test:"},
		{name: "zero port", raw: "https://public.example.test:0"},
		{name: "out of range port", raw: "https://public.example.test:65536"},
		{name: "ambiguous port", raw: "https://public.example.test:080"},
		{name: "unbracketed IPv6", raw: "https://2001:db8::1"},
		{name: "leading authority confusion", raw: "//public.example.test"},
		{name: "trailing authority confusion", raw: "https://public.example.test @evil.example"},
		{name: "backslash authority confusion", raw: `https:\\public.example.test`},
		{name: "encoded host delimiter", raw: "https://public.example.test%2f@evil.example"},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeFormalPoolPublicOrigin(tc.raw)
			require.Error(t, err)
			require.Empty(t, got)
			require.NotContains(t, err.Error(), tc.raw)
			require.NotContains(t, strings.ToLower(err.Error()), "secret")
		})
	}
}

func TestValidateFormalPoolBrowserEgressURL(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"/api/v1/claude-onboarding/browser-egress-check/nonce_abc123",
		"https://public.example.test/api/v1/claude-onboarding/browser-egress-check/nonce_abc123",
		"http://localhost:8080/api/v1/claude-onboarding/browser-egress-check/nonce_abc123",
		"http://127.0.0.1:8080/api/v1/claude-onboarding/browser-egress-check/nonce_abc123",
		"http://[::1]:8080/api/v1/claude-onboarding/browser-egress-check/nonce_abc123",
	} {
		raw := raw
		t.Run("valid "+raw, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, ValidateFormalPoolBrowserEgressURL(raw))
		})
	}

	for _, raw := range []string{
		"",
		"api/v1/claude-onboarding/browser-egress-check/nonce_abc123",
		"//evil.example/api/v1/claude-onboarding/browser-egress-check/nonce_abc123",
		"http://public.example.test/api/v1/claude-onboarding/browser-egress-check/nonce_abc123",
		"https://user:secret@public.example.test/api/v1/claude-onboarding/browser-egress-check/nonce_abc123",
		"https://public.example.test/admin",
		"https://public.example.test/api/v1/claude-onboarding/browser-egress-check/nonce_abc123?next=evil",
		"https://public.example.test/api/v1/claude-onboarding/browser-egress-check/nonce_abc123#fragment",
		"https://public.example.test/api/v1/claude-onboarding/browser-egress-check/nonce_abc123/extra",
	} {
		raw := raw
		t.Run("invalid "+raw, func(t *testing.T) {
			t.Parallel()
			require.Error(t, ValidateFormalPoolBrowserEgressURL(raw))
		})
	}
}
