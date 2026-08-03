package service

import "testing"

import "github.com/stretchr/testify/require"

func TestAugmentOfficialRoutePolicyClassifiesRouteOwners(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want AugmentOfficialRouteOwner
	}{
		{path: "/chat", want: AugmentOfficialRouteOwnerLocalGateway},
		{path: "/chat-stream", want: AugmentOfficialRouteOwnerLocalGateway},
		{path: "/usage/api/get-models", want: AugmentOfficialRouteOwnerLocalGateway},
		{path: "/batch-upload", want: AugmentOfficialRouteOwnerOfficialCloud},
		{path: "/agents/codebase-retrieval", want: AugmentOfficialRouteOwnerOfficialCloud},
		{path: "/prompt-enhancer", want: AugmentOfficialRouteOwnerOfficialCloud},
		{path: "/save-chat", want: AugmentOfficialRouteOwnerOfficialCloud},
		{path: "/notifications/read", want: AugmentOfficialRouteOwnerExplicitPolicy},
		{path: "/subscription-banner", want: AugmentOfficialRouteOwnerExplicitPolicy},
		{path: "/resolve-next-edit", want: AugmentOfficialRouteOwnerExplicitPolicy},
		{path: "/unknown-endpoint", want: AugmentOfficialRouteOwnerUnknown},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			require.Equal(t, tc.want, ClassifyAugmentOfficialRoute(tc.path))
		})
	}
}

func TestAugmentOfficialRoutePolicyFailClosedStateMatrix(t *testing.T) {
	t.Parallel()

	cases := []AugmentOfficialSessionStatus{
		AugmentOfficialSessionStatusMissing,
		AugmentOfficialSessionStatusExpired,
		AugmentOfficialSessionStatusRefreshFailed,
		AugmentOfficialSessionStatusScopeInsufficient,
		AugmentOfficialSessionStatusTenantMismatch,
		AugmentOfficialSessionStatusSourceMismatch,
		AugmentOfficialSessionStatusFingerprintMismatch,
	}

	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			decision := EvaluateAugmentOfficialRoute(AugmentOfficialRouteCheckInput{
				Path:          "/agents/codebase-retrieval",
				SessionStatus: status,
			})
			require.True(t, decision.OfficialRouteRequired)
			require.Equal(t, AugmentOfficialRouteResultFailClosed, decision.OfficialRouteResult)
			require.False(t, decision.AllowLocalHandler)
			require.Equal(t, status, decision.OfficialSessionStatus)
		})
	}
}

func TestAugmentOfficialRoutePolicyAllowsActiveOfficialSession(t *testing.T) {
	t.Parallel()

	decision := EvaluateAugmentOfficialRoute(AugmentOfficialRouteCheckInput{
		Path:                   "/prompt-enhancer",
		SessionStatus:          AugmentOfficialSessionStatusActive,
		SessionScopes:          []string{"augment:session"},
		SessionSource:          "official_quick_login",
		SessionTenantOrigin:    "https://official.augment.local",
		SessionFingerprintPrefix: "abc123",
		PresentedSource:        "official_quick_login",
		PresentedTenantOrigin:  "https://official.augment.local",
		PresentedFingerprint:   "abc123deadbeef",
	})
	require.True(t, decision.OfficialRouteRequired)
	require.Equal(t, AugmentOfficialRouteResultAllowed, decision.OfficialRouteResult)
	require.True(t, decision.AllowLocalHandler)
	require.Equal(t, AugmentOfficialSessionStatusActive, decision.OfficialSessionStatus)
}

func TestAugmentOfficialRoutePolicyEnforcesMismatchChecks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input AugmentOfficialRouteCheckInput
		want  AugmentOfficialSessionStatus
	}{
		{
			name: "scope insufficient",
			input: AugmentOfficialRouteCheckInput{
				Path:          "/prompt-enhancer",
				SessionStatus: AugmentOfficialSessionStatusActive,
				SessionScopes: []string{"augment:summary"},
			},
			want: AugmentOfficialSessionStatusScopeInsufficient,
		},
		{
			name: "source mismatch",
			input: AugmentOfficialRouteCheckInput{
				Path:            "/prompt-enhancer",
				SessionStatus:   AugmentOfficialSessionStatusActive,
				SessionScopes:   []string{"augment:session"},
				SessionSource:   "official_quick_login",
				PresentedSource: "wukong_quick_login",
			},
			want: AugmentOfficialSessionStatusSourceMismatch,
		},
		{
			name: "tenant mismatch",
			input: AugmentOfficialRouteCheckInput{
				Path:                 "/prompt-enhancer",
				SessionStatus:        AugmentOfficialSessionStatusActive,
				SessionScopes:        []string{"augment:session"},
				SessionTenantOrigin:  "https://official.augment.local",
				PresentedTenantOrigin: "https://other.augment.local",
			},
			want: AugmentOfficialSessionStatusTenantMismatch,
		},
		{
			name: "fingerprint mismatch",
			input: AugmentOfficialRouteCheckInput{
				Path:                    "/prompt-enhancer",
				SessionStatus:           AugmentOfficialSessionStatusActive,
				SessionScopes:           []string{"augment:session"},
				SessionFingerprintPrefix: "abc123",
				PresentedFingerprint:    "zzz999",
			},
			want: AugmentOfficialSessionStatusFingerprintMismatch,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decision := EvaluateAugmentOfficialRoute(tc.input)
			require.Equal(t, tc.want, decision.OfficialSessionStatus)
			require.Equal(t, AugmentOfficialRouteResultFailClosed, decision.OfficialRouteResult)
			require.False(t, decision.AllowLocalHandler)
		})
	}
}

func TestAugmentOfficialRoutePolicyEmergencyOffBypassesFailClosedExplicitly(t *testing.T) {
	t.Parallel()

	decision := EvaluateAugmentOfficialRoute(AugmentOfficialRouteCheckInput{
		Path:          "/agents/codebase-retrieval",
		SessionStatus: AugmentOfficialSessionStatusMissing,
		EmergencyOff:  true,
	})
	require.True(t, decision.OfficialRouteRequired)
	require.Equal(t, AugmentOfficialRouteResultEmergencyOff, decision.OfficialRouteResult)
	require.True(t, decision.AllowLocalHandler)
	require.Equal(t, AugmentOfficialSessionStatusMissing, decision.OfficialSessionStatus)
}
