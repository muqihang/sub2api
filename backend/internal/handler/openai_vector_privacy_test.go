package handler

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPublicVectorModelUnavailableMessageDoesNotEchoProviderOrModel(t *testing.T) {
	message := strings.ToLower(publicVectorModelUnavailableMessage)

	require.NotContains(t, message, "nvidia")
	require.NotContains(t, message, "nemotron")
	require.NotContains(t, message, "model \"")
}
