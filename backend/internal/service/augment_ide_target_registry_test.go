package service

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAugmentIDETargetNormalize(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "vs code alias", input: "VS Code", expected: "vscode"},
		{name: "cursor lower", input: "cursor", expected: "cursor"},
		{name: "trae uppercase", input: "TRAE", expected: "trae"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			target, err := ResolveAugmentIDETarget(tc.input)
			require.NoError(t, err)
			require.Equal(t, tc.expected, target.ID)
		})
	}
}

func TestAugmentIDETargetRejectsUnknown(t *testing.T) {
	t.Parallel()

	_, err := ResolveAugmentIDETarget("unknown-editor")
	require.ErrorIs(t, err, ErrAugmentIDETargetInvalid)
}

func TestAugmentIDETargetDefaultsToVSCode(t *testing.T) {
	t.Parallel()

	target, err := ResolveAugmentIDETarget("")
	require.NoError(t, err)
	require.Equal(t, "vscode", target.ID)
	require.True(t, target.EnabledByDefault)
	require.True(t, target.SchemeVerified)
	require.True(t, target.HandlerVerified)
	require.True(t, target.LaunchEligible)
	require.Empty(t, target.Warning)
}

func TestAugmentIDETargetBuildDeeplinkUsesRegistryMetadata(t *testing.T) {
	t.Parallel()

	target, err := ResolveAugmentIDETarget("cursor")
	require.NoError(t, err)

	deeplink := FormatAugmentIDEQuickLoginDeeplink(
		target,
		"grant-123",
		"state-456",
		"https://issuer.example/path",
		"https://portal.example/login?token=abc",
	)

	parsed, err := url.Parse(deeplink)
	require.NoError(t, err)
	require.Equal(t, "cursor", parsed.Scheme)
	require.Equal(t, "Augment.vscode-augment", parsed.Host)
	require.Equal(t, "/autoAuth", parsed.Path)
	require.Equal(t, "grant-123", parsed.Query().Get("grant"))
	require.Equal(t, "state-456", parsed.Query().Get("state"))
	require.Equal(t, "quick_login", parsed.Query().Get("source"))
	require.Equal(t, "https://issuer.example", parsed.Query().Get("issuer"))
	require.Equal(t, "https://portal.example/login?token=abc", parsed.Query().Get("portal"))
}

func TestAugmentIDETargetOverrideEnablesLaunchEligibility(t *testing.T) {
	withoutOverride, err := ResolveAugmentIDETarget("kiro")
	require.NoError(t, err)
	require.Equal(t, "kiro", withoutOverride.Scheme)
	require.False(t, withoutOverride.LaunchEligible)
	require.Equal(t, "scheme_unverified", withoutOverride.Warning)

	t.Setenv("AUGMENT_IDE_SCHEME_KIRO", "kiro-preview")

	withOverride, err := ResolveAugmentIDETarget("kiro")
	require.NoError(t, err)
	require.Equal(t, "kiro-preview", withOverride.Scheme)
	require.True(t, withOverride.OverrideActive)
	require.True(t, withOverride.LaunchEligible)
	require.Empty(t, withOverride.Warning)
}

func TestAugmentIDETargetCatalogKeepsUnverifiedTargetsVisibleButDisabled(t *testing.T) {
	t.Parallel()

	targets := ListAugmentIDETargets()
	require.Len(t, targets, 8)

	byID := make(map[string]AugmentIDETarget, len(targets))
	for _, target := range targets {
		byID[target.ID] = target
	}

	require.Contains(t, byID, "vscode")
	require.Contains(t, byID, "cursor")
	require.Contains(t, byID, "kiro")
	require.Contains(t, byID, "trae")
	require.Contains(t, byID, "windsurf")
	require.Contains(t, byID, "qodo")
	require.Contains(t, byID, "codebuddy")
	require.Contains(t, byID, "antigravity")

	require.True(t, byID["vscode"].EnabledByDefault)
	require.True(t, byID["vscode"].SchemeVerified)
	require.True(t, byID["vscode"].HandlerVerified)
	require.True(t, byID["vscode"].LaunchEligible)
	require.Empty(t, byID["vscode"].Warning)

	require.False(t, byID["cursor"].EnabledByDefault)
	require.True(t, byID["cursor"].OverrideRequired)
	require.True(t, byID["cursor"].SchemeVerified)
	require.False(t, byID["cursor"].HandlerVerified)
	require.False(t, byID["cursor"].LaunchEligible)
	require.Equal(t, AugmentIDETargetWarningHandlerUnverified, byID["cursor"].Warning)

	require.False(t, byID["kiro"].EnabledByDefault)
	require.True(t, byID["kiro"].OverrideRequired)
	require.False(t, byID["kiro"].SchemeVerified)
	require.False(t, byID["kiro"].HandlerVerified)
	require.False(t, byID["kiro"].LaunchEligible)
	require.Equal(t, AugmentIDETargetWarningSchemeUnverified, byID["kiro"].Warning)
}

func TestAugmentIDETargetListReflectsRuntimeOverrideState(t *testing.T) {
	t.Setenv("AUGMENT_IDE_SCHEME_KIRO", "kiro-preview")

	targets := ListAugmentIDETargets()
	byID := make(map[string]AugmentIDETarget, len(targets))
	for _, target := range targets {
		byID[target.ID] = target
	}

	require.Equal(t, "kiro-preview", byID["kiro"].Scheme)
	require.True(t, byID["kiro"].OverrideActive)
	require.True(t, byID["kiro"].LaunchEligible)
	require.Empty(t, byID["kiro"].Warning)
}
