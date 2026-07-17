package service

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProvideOpenAIGatewayServiceRemainsVariadicForSourceCompatibility(t *testing.T) {
	_ = ProvideOpenAIGatewayService(
		nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, nil,
		nil,
	)
}

func TestGeneratedOpenAIGatewayGraphRetainsGrokAndPlatformQuotaDependencies(t *testing.T) {
	generated, err := os.ReadFile("../../cmd/server/wire_gen.go")
	require.NoError(t, err)
	var gatewayLine string
	for _, line := range strings.Split(string(generated), "\n") {
		if strings.Contains(line, "openAIGatewayService := service.ProvideOpenAIGatewayServiceForWire(") {
			gatewayLine = line
			break
		}
	}
	require.NotEmpty(t, gatewayLine, "generated Wire graph must use the fixed Wire-only provider")
	require.Contains(t, gatewayLine, "grokTokenProvider")
	require.Contains(t, gatewayLine, "serviceUserPlatformQuotaRepository")
}
