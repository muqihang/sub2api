package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIGatewayCanonicalProfile_HTTPCompactHeaders(t *testing.T) {
	profile := &OpenAIGatewayCanonicalProfile{
		ProfileID:               "profile-1",
		Mode:                    OpenAIGatewayProfileModeFixed,
		UserAgent:               "codex_cli_rs/0.104.0",
		Version:                 "0.104.0",
		StainlessLang:           "js",
		StainlessPackageVersion: "0.70.0",
		StainlessOS:             "Linux",
		StainlessArch:           "arm64",
		StainlessRuntime:        "node",
		StainlessRuntimeVersion: "v24.13.0",
	}

	artifact := BuildOpenAIGatewayProfileArtifact(
		profile,
		OpenAIGatewayProfileRouteResponsesCompact,
		OpenAIGatewayProfileArtifactOptions{RequestedOriginator: "", IsOfficialClient: true},
	)

	headers := http.Header{}
	artifact.ApplyHTTP(headers)

	require.Equal(t, "codex_cli_rs/0.104.0", headers.Get("User-Agent"))
	require.Equal(t, "0.104.0", headers.Get("Version"))
	require.Equal(t, "responses=experimental", headers.Get("OpenAI-Beta"))
	require.Equal(t, "codex_cli_rs", headers.Get("originator"))
	require.Equal(t, "js", headers.Get("X-Stainless-Lang"))
	require.Equal(t, "0.70.0", headers.Get("X-Stainless-Package-Version"))
}

func TestOpenAIGatewayCanonicalProfile_HTTPMessagesBridgeClearsRouteHeaders(t *testing.T) {
	profile := &OpenAIGatewayCanonicalProfile{
		ProfileID:               "profile-2",
		Mode:                    OpenAIGatewayProfileModeFixed,
		UserAgent:               "codex_cli_rs/0.104.0",
		Version:                 "0.104.0",
		StainlessLang:           "js",
		StainlessPackageVersion: "0.70.0",
		StainlessOS:             "Linux",
		StainlessArch:           "arm64",
		StainlessRuntime:        "node",
		StainlessRuntimeVersion: "v24.13.0",
	}

	artifact := BuildOpenAIGatewayProfileArtifact(
		profile,
		OpenAIGatewayProfileRouteCompatMessagesBridge,
		OpenAIGatewayProfileArtifactOptions{RequestedOriginator: "Codex Desktop", IsOfficialClient: false},
	)

	headers := http.Header{}
	headers.Set("OpenAI-Beta", "responses=experimental")
	headers.Set("originator", "Codex Desktop")
	artifact.ApplyHTTP(headers)

	require.Equal(t, "codex_cli_rs/0.104.0", headers.Get("User-Agent"))
	require.Empty(t, headers.Get("Version"))
	require.Equal(t, "js", headers.Get("X-Stainless-Lang"))
	require.Empty(t, headers.Get("OpenAI-Beta"))
	require.Empty(t, headers.Get("originator"))
}

func TestOpenAIGatewayCanonicalProfile_WSV2Headers(t *testing.T) {
	profile := &OpenAIGatewayCanonicalProfile{
		ProfileID:               "profile-3",
		Mode:                    OpenAIGatewayProfileModeFixed,
		UserAgent:               "codex_cli_rs/0.104.0",
		Version:                 "0.104.0",
		StainlessLang:           "js",
		StainlessPackageVersion: "0.70.0",
		StainlessOS:             "Linux",
		StainlessArch:           "arm64",
		StainlessRuntime:        "node",
		StainlessRuntimeVersion: "v24.13.0",
	}

	artifact := BuildOpenAIGatewayProfileArtifact(
		profile,
		OpenAIGatewayProfileRouteResponsesWSV2,
		OpenAIGatewayProfileArtifactOptions{RequestedOriginator: "codex_vscode", IsOfficialClient: false},
	)

	headers := http.Header{}
	artifact.ApplyWS(headers)

	require.Equal(t, "codex_cli_rs/0.104.0", headers.Get("User-Agent"))
	require.Equal(t, openAIWSBetaV2Value, headers.Get("OpenAI-Beta"))
	require.Equal(t, "codex_vscode", headers.Get("originator"))
	require.Empty(t, headers.Get("Version"))
	require.Equal(t, "js", headers.Get("X-Stainless-Lang"))
}

func TestOpenAIGatewayCanonicalProfile_UserAgentOverrideRealignsVersion(t *testing.T) {
	profile := &OpenAIGatewayCanonicalProfile{
		ProfileID:               "profile-4",
		Mode:                    OpenAIGatewayProfileModeFixed,
		UserAgent:               "codex_cli_rs/0.104.0",
		Version:                 "0.104.0",
		StainlessLang:           "js",
		StainlessPackageVersion: "0.70.0",
		StainlessOS:             "Linux",
		StainlessArch:           "arm64",
		StainlessRuntime:        "node",
		StainlessRuntimeVersion: "v24.13.0",
	}

	artifact := BuildOpenAIGatewayProfileArtifact(
		profile,
		OpenAIGatewayProfileRouteResponsesCompact,
		OpenAIGatewayProfileArtifactOptions{RequestedOriginator: "", IsOfficialClient: true},
	).WithUserAgentOverride("codex_cli_rs/0.200.0")

	headers := http.Header{}
	artifact.ApplyHTTP(headers)

	require.Equal(t, "codex_cli_rs/0.200.0", headers.Get("User-Agent"))
	require.Equal(t, "0.200.0", headers.Get("Version"))
}
