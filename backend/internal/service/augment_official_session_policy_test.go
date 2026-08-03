package service

import (
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestAugmentOfficialTenantPolicyAcceptsAllowlistedHTTPSOrigin(t *testing.T) {
	policy := AugmentOfficialTenantPolicy{
		AllowedOrigins: []string{" https://official.augment.local/ "},
	}

	normalized, err := policy.ValidateOrigin("https://official.augment.local/")
	require.NoError(t, err)
	require.Equal(t, "https://official.augment.local", normalized)

	_, err = policy.ValidateOrigin("https://not-allowlisted.example")
	require.Error(t, err)
	require.Equal(t, "tenant_not_allowlisted", infraerrors.Reason(err))
}

func TestAugmentOfficialTenantPolicyRejectsLocalhostPrivateIPAndHTTP(t *testing.T) {
	policy := AugmentOfficialTenantPolicy{
		AllowedOrigins: []string{"https://official.augment.local"},
	}

	cases := []string{
		"http://official.augment.local",
		"https://localhost",
		"https://127.0.0.1",
		"https://10.0.0.8",
		"https://169.254.169.254",
		"https://[::1]",
		"https://[fe80::1]",
	}

	for _, raw := range cases {
		_, err := policy.ValidateOrigin(raw)
		require.Error(t, err, raw)
		require.Equal(t, "invalid_tenant_url", infraerrors.Reason(err), raw)
	}
}

func TestAugmentOfficialTenantPolicyRejectsUserinfoPathQueryAndPort(t *testing.T) {
	policy := AugmentOfficialTenantPolicy{
		AllowedOrigins: []string{"https://official.augment.local"},
	}

	cases := []string{
		"https://user:pass@official.augment.local",
		"https://official.augment.local/path",
		"https://official.augment.local?foo=bar",
		"https://official.augment.local#fragment",
		"https://official.augment.local:8443",
	}

	for _, raw := range cases {
		_, err := policy.ValidateOrigin(raw)
		require.Error(t, err, raw)
		require.Equal(t, "invalid_tenant_url", infraerrors.Reason(err), raw)
	}
}

func TestAugmentOfficialTenantPolicyCanonicalizesPunycodeBeforeAllowlist(t *testing.T) {
	policy := AugmentOfficialTenantPolicy{
		AllowedOrigins: []string{"https://xn--bcher-kva.example"},
	}

	normalized, err := policy.ValidateOrigin("https://bücher.example/")
	require.NoError(t, err)
	require.Equal(t, "https://xn--bcher-kva.example", normalized)
}

func TestAugmentOfficialSessionSourceRejectsManualImportV1(t *testing.T) {
	require.NoError(t, ValidateAugmentOfficialSource("official_quick_login", AugmentOfficialSourcePolicy{}))
	require.NoError(t, ValidateAugmentOfficialSource("wukong_quick_login", AugmentOfficialSourcePolicy{}))

	err := ValidateAugmentOfficialSource("manual_import", AugmentOfficialSourcePolicy{})
	require.Error(t, err)
	require.Equal(t, "tenant_session_mismatch", infraerrors.Reason(err))
}
