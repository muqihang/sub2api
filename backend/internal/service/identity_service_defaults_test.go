package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIdentityServiceCreateFingerprintFromHeaders_UsesUpdatedDefaults(t *testing.T) {
	svc := NewIdentityService(&identityCacheStub{})

	fp := svc.createFingerprintFromHeaders(http.Header{})

	require.Equal(t, "claude-cli/2.1.175 (external, sdk-cli)", fp.UserAgent)
	require.Equal(t, "0.94.0", fp.StainlessPackageVersion)
	require.Equal(t, "v24.3.0", fp.StainlessRuntimeVersion)
	require.Equal(t, "js", fp.StainlessLang)
	require.Equal(t, "node", fp.StainlessRuntime)
}

func TestIdentityServiceGetOrCreateMimicryFingerprint_UpgradesLegacyCachedDefaultPersona(t *testing.T) {
	cached := &Fingerprint{
		ClientID:                "cached-client-id",
		UserAgent:               "claude-cli/2.1.146 (external, sdk-cli)",
		StainlessLang:           "js",
		StainlessPackageVersion: "0.94.0",
		StainlessOS:             "Linux",
		StainlessArch:           "arm64",
		StainlessRuntime:        "node",
		StainlessRuntimeVersion: "v24.3.0",
		UpdatedAt:               time.Now().Unix(),
	}
	cache := &identityCacheStub{fingerprint: cached}
	svc := NewIdentityService(cache)

	fp, err := svc.GetOrCreateMimicryFingerprint(context.Background(), 123)

	require.NoError(t, err)
	require.Equal(t, "cached-client-id", fp.ClientID)
	require.Equal(t, defaultFingerprint.UserAgent, fp.UserAgent)
	require.Equal(t, defaultFingerprint.StainlessLang, fp.StainlessLang)
	require.Equal(t, defaultFingerprint.StainlessPackageVersion, fp.StainlessPackageVersion)
	require.Equal(t, defaultFingerprint.StainlessOS, fp.StainlessOS)
	require.Equal(t, defaultFingerprint.StainlessArch, fp.StainlessArch)
	require.Equal(t, defaultFingerprint.StainlessRuntime, fp.StainlessRuntime)
	require.Equal(t, defaultFingerprint.StainlessRuntimeVersion, fp.StainlessRuntimeVersion)
	require.NotNil(t, cache.setFingerprint, "legacy cached default persona should be persisted after upgrade")
	require.Equal(t, defaultFingerprint.UserAgent, cache.setFingerprint.UserAgent)
	require.Equal(t, "cached-client-id", cache.setFingerprint.ClientID)
}

func TestIdentityServiceGetOrCreateMimicryFingerprint_PreservesNonDefaultCachedPersona(t *testing.T) {
	cached := &Fingerprint{
		ClientID:                "cached-client-id",
		UserAgent:               "claude-cli/2.1.146 (external, sdk-cli)",
		StainlessLang:           "js",
		StainlessPackageVersion: "0.94.0",
		StainlessOS:             "MacOS",
		StainlessArch:           "x64",
		StainlessRuntime:        "node",
		StainlessRuntimeVersion: "v24.3.0",
		UpdatedAt:               time.Now().Unix(),
	}
	cache := &identityCacheStub{fingerprint: cached}
	svc := NewIdentityService(cache)

	fp, err := svc.GetOrCreateMimicryFingerprint(context.Background(), 123)

	require.NoError(t, err)
	require.Equal(t, "claude-cli/2.1.146 (external, sdk-cli)", fp.UserAgent)
	require.Equal(t, "MacOS", fp.StainlessOS)
	require.Equal(t, "x64", fp.StainlessArch)
	require.Nil(t, cache.setFingerprint, "non-default cached persona must not be replaced")
}
