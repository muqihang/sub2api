package dto

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func syntheticDTOAWSWorkspaceID(seed int) string {
	letter := string(rune('A' + seed))
	return "wrkspc_" + strings.Repeat(letter, 12)
}

func syntheticDTOAWSAPIKey() string {
	return "synthetic-cpaws-" + strings.Repeat("k", 16)
}

func TestRedactCredentials_StripsClaudePlatformAWSWorkspaceID(t *testing.T) {
	rawWorkspace := syntheticDTOAWSWorkspaceID(1)
	rawAPIKey := syntheticDTOAWSAPIKey()
	out, status := RedactCredentials(map[string]any{
		"anthropic_workspace_id": rawWorkspace,
		"api_key":                rawAPIKey,
		"aws_region":             "us-east-1",
		"base_url":               "https://aws-external-anthropic.us-east-1.api.aws",
	})

	require.NotContains(t, out, "anthropic_workspace_id")
	require.NotContains(t, out, "api_key")
	require.Equal(t, "us-east-1", out["aws_region"])
	require.True(t, status["has_anthropic_workspace_id"])
	require.True(t, status["has_api_key"])
}

func TestAccountFromService_ClaudePlatformAWSExposesOnlySafeRefs(t *testing.T) {
	rawWorkspace := syntheticDTOAWSWorkspaceID(2)
	rawExtraWorkspace := syntheticDTOAWSWorkspaceID(3)
	rawAPIKey := syntheticDTOAWSAPIKey()
	rawExtraAPIKey := "synthetic-extra-cpaws-" + strings.Repeat("k", 16)
	rawAuth := "Bearer " + strings.Repeat("a", 16)
	rawBody := "prompt-" + strings.Repeat("b", 16)
	account := &service.Account{
		ID:       59,
		Platform: service.PlatformAnthropic,
		Type:     service.AccountTypeClaudePlatformAWS,
		Credentials: map[string]any{
			"anthropic_workspace_id": rawWorkspace,
			"api_key":                rawAPIKey,
			"aws_region":             "us-east-1",
		},
		Extra: map[string]any{
			service.ClaudePlatformAWSExtraWorkspaceRef:                     "workspace:safe-ref",
			service.ClaudePlatformAWSExtraEndpointRef:                      "endpoint:safe-ref",
			service.ClaudePlatformAWSExtraRegion:                           "us-east-1",
			service.ClaudePlatformAWSExtraAuthScheme:                       service.ClaudePlatformAWSAuthProfileXAPIKey,
			service.ClaudePlatformAWSExtraRequestShapeProfileRef:           "request-shape:aws-v1",
			service.ClaudePlatformAWSExtraCacheParityProfileRef:            "cache-profile:aws-v1",
			service.ClaudePlatformAWSExtraBetaPolicyRef:                    "beta-policy:aws-v1",
			service.ClaudePlatformAWSExtraCP0AuthProfileEvidenceStatus:     "blocked",
			service.ClaudePlatformAWSExtraCP0RegionWorkspaceEvidenceStatus: "pass",
			"anthropic_workspace_id":                                       rawExtraWorkspace,
			"api_key":                                                      rawExtraAPIKey,
			"authorization":                                                rawAuth,
			"raw_body":                                                     rawBody,
		},
	}

	out := AccountFromService(account)
	payload, err := json.Marshal(out)
	require.NoError(t, err)
	text := string(payload)
	require.NotContains(t, text, rawWorkspace)
	require.NotContains(t, text, rawExtraWorkspace)
	require.NotContains(t, text, rawAPIKey)
	require.NotContains(t, text, rawExtraAPIKey)
	require.NotContains(t, strings.ToLower(text), "authorization")
	require.NotContains(t, strings.ToLower(text), "raw_body")
	require.Contains(t, text, "workspace:safe-ref")
	require.Contains(t, text, "us-east-1")
}

func TestAccountFromService_ClaudePlatformAWSExposesSafeBearerAuthSchemeEnum(t *testing.T) {
	account := &service.Account{
		ID:       60,
		Platform: service.PlatformAnthropic,
		Type:     service.AccountTypeClaudePlatformAWS,
		Extra: map[string]any{
			service.ClaudePlatformAWSExtraWorkspaceRef: "workspace:safe-ref",
			service.ClaudePlatformAWSExtraEndpointRef:  "endpoint:safe-ref",
			service.ClaudePlatformAWSExtraRegion:       "us-east-1",
			service.ClaudePlatformAWSExtraAuthScheme:   service.ClaudePlatformAWSAuthProfileBearerAPIKey,
		},
	}

	out := AccountFromService(account)
	payload, err := json.Marshal(out)
	require.NoError(t, err)
	require.Contains(t, string(payload), service.ClaudePlatformAWSAuthProfileBearerAPIKey)
}
