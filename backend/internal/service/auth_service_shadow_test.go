package service

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsureEmailAuthIdentityDoesNotShadowCreateError(t *testing.T) {
	source, err := os.ReadFile("auth_service.go")
	require.NoError(t, err)
	text := string(source)

	require.NotContains(t, text, "if err := client.AuthIdentity.Create()", "create errors must be assigned to the outer err so they are reported before reload")
	require.True(t,
		strings.Contains(text, "if err = client.AuthIdentity.Create()") || strings.Contains(text, "err = client.AuthIdentity.Create()"),
		"email auth identity create should assign to the outer err",
	)
}
