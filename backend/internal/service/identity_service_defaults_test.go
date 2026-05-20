package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIdentityServiceCreateFingerprintFromHeaders_UsesUpdatedDefaults(t *testing.T) {
	svc := NewIdentityService(&identityCacheStub{})

	fp := svc.createFingerprintFromHeaders(http.Header{})

	require.Equal(t, "claude-cli/2.1.145 (external, sdk-cli)", fp.UserAgent)
	require.Equal(t, "0.94.0", fp.StainlessPackageVersion)
	require.Equal(t, "v24.3.0", fp.StainlessRuntimeVersion)
	require.Equal(t, "js", fp.StainlessLang)
	require.Equal(t, "node", fp.StainlessRuntime)
}
