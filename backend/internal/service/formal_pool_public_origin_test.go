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
		{name: "ASCII punycode label", raw: "https://xn--bcher-kva.example", want: "https://xn--bcher-kva.example"},
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
		{name: "empty fragment after host", raw: "https://public.example.test#"},
		{name: "empty fragment after slash", raw: "https://public.example.test/#"},
		{name: "composed IDN", raw: "https://éxample.test"},
		{name: "decomposed IDN", raw: "https://e\u0301xample.test"},
		{name: "Unicode ideographic dot", raw: "https://example。test"},
		{name: "Unicode fullwidth dot", raw: "https://example．test"},
		{name: "Unicode halfwidth dot", raw: "https://example｡test"},
		{name: "Unicode separator", raw: "https://example‧test"},
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
			require.Equal(t, "invalid formal_pool public_origin", err.Error())
			require.NotContains(t, strings.ToLower(err.Error()), "secret")
		})
	}
}

func TestValidateFormalPoolBrowserEgressURL(t *testing.T) {
	t.Parallel()
	validNonce := "nonce_" + strings.Repeat("a", 32)
	validPath := formalPoolBrowserEgressPublicPathPrefix + validNonce

	for _, raw := range []string{
		validPath,
		"https://public.example.test" + validPath,
		"https://xn--bcher-kva.example" + validPath,
		"http://localhost:8080" + validPath,
		"http://127.0.0.1:8080" + validPath,
		"http://[::1]:8080" + validPath,
	} {
		raw := raw
		t.Run("valid "+raw, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, ValidateFormalPoolBrowserEgressURL(raw))
		})
	}

	for _, raw := range []string{
		"",
		strings.TrimPrefix(validPath, "/"),
		"//evil.example" + validPath,
		"http://public.example.test" + validPath,
		"https://user:secret@public.example.test" + validPath,
		"https://éxample.test" + validPath,
		"https://e\u0301xample.test" + validPath,
		"https://example。test" + validPath,
		"https://public.example.test/admin",
		"https://public.example.test" + validPath + "?next=evil",
		"https://public.example.test" + validPath + "#fragment",
		"https://public.example.test" + validPath + "#",
		validPath + "#",
		"https://public.example.test" + validPath + "/extra",
		formalPoolBrowserEgressPublicPathPrefix + ".",
		formalPoolBrowserEgressPublicPathPrefix + "..",
		formalPoolBrowserEgressPublicPathPrefix + `nonce_aaaaaaaaaaaaaaaa\\aaaaaaaaaaaaaaaa`,
		formalPoolBrowserEgressPublicPathPrefix + "nonce_" + strings.Repeat("a", 31),
		formalPoolBrowserEgressPublicPathPrefix + "nonce_" + strings.Repeat("a", 33),
		formalPoolBrowserEgressPublicPathPrefix + "nonce_" + strings.Repeat("A", 32),
		formalPoolBrowserEgressPublicPathPrefix + "nonce_" + strings.Repeat("g", 32),
		formalPoolBrowserEgressPublicPathPrefix + strings.Repeat("a", 32),
		formalPoolBrowserEgressPublicPathPrefix + "nonce_%61" + strings.Repeat("a", 30),
		formalPoolBrowserEgressPublicPathPrefix + "nonce_%2e%2e",
	} {
		raw := raw
		t.Run("invalid "+raw, func(t *testing.T) {
			t.Parallel()
			err := ValidateFormalPoolBrowserEgressURL(raw)
			require.EqualError(t, err, "invalid formal pool browser egress URL")
		})
	}
}
