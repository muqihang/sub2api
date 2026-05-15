package service

import (
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestCodexScopedAPIKeyAccessAllowsCodexMVPPaths(t *testing.T) {
	t.Parallel()

	groupID := int64(1)
	product := CodexUsageClientProduct
	apiKey := &APIKey{
		ID:                      10,
		UserID:                  20,
		Key:                     "sk-codex",
		Status:                  StatusActive,
		GroupID:                 &groupID,
		RestrictedClientProduct: &product,
		Group: &Group{
			ID:                   groupID,
			Status:               StatusActive,
			Hydrated:             true,
			Platform:             PlatformOpenAI,
			CodexGatewayEntitled: true,
		},
	}

	for _, path := range []string{
		"/codex/v1/models",
		"/codex/v1/responses",
		"codex/v1/responses?stream=true",
		"/backend-api/codex/responses",
		"/backend-api/codex/responses/compact",
		"/backend-api/codex/responses?stream=true",
	} {
		t.Run(path, func(t *testing.T) {
			decision := EvaluateCodexScopedAPIKeyAccess(apiKey, path)
			require.True(t, decision.Allowed)
			require.NoError(t, ValidateCodexScopedAPIKeyAccess(apiKey, path))
		})
	}
}

func TestCodexScopedAPIKeyAccessRejectsGenericAugmentOrNonEntitledKeys(t *testing.T) {
	t.Parallel()

	groupID := int64(2)
	augmentProduct := AugmentClientProductZhumeng
	codexProduct := CodexUsageClientProduct

	tests := []struct {
		name   string
		apiKey *APIKey
		want   error
	}{
		{
			name: "generic key",
			apiKey: &APIKey{
				Key:     "sk-generic",
				Status:  StatusActive,
				GroupID: &groupID,
				Group: &Group{
					ID:                   groupID,
					Status:               StatusActive,
					Hydrated:             true,
					Platform:             PlatformOpenAI,
					CodexGatewayEntitled: true,
				},
			},
			want: ErrCodexScopedAPIKeyRequired,
		},
		{
			name: "augment key",
			apiKey: &APIKey{
				Key:                     "sk-augment",
				Status:                  StatusActive,
				GroupID:                 &groupID,
				RestrictedClientProduct: &augmentProduct,
				Group: &Group{
					ID:                   groupID,
					Status:               StatusActive,
					Hydrated:             true,
					Platform:             PlatformOpenAI,
					CodexGatewayEntitled: true,
				},
			},
			want: ErrCodexScopedAPIKeyRequired,
		},
		{
			name: "non entitled group",
			apiKey: &APIKey{
				Key:                     "sk-codex-no-entitlement",
				Status:                  StatusActive,
				GroupID:                 &groupID,
				RestrictedClientProduct: &codexProduct,
				Group: &Group{
					ID:       groupID,
					Status:   StatusActive,
					Hydrated: true,
					Platform: PlatformOpenAI,
				},
			},
			want: ErrCodexScopedAPIKeyRequired,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decision := EvaluateCodexScopedAPIKeyAccess(tc.apiKey, "/backend-api/codex/responses")
			require.False(t, decision.Allowed)
			require.Equal(t, infraerrors.Reason(tc.want), decision.Reason)
			require.ErrorIs(t, ValidateCodexScopedAPIKeyAccess(tc.apiKey, "/backend-api/codex/responses"), tc.want)
		})
	}
}

func TestCodexScopedAPIKeyAccessRejectsNonCodexPaths(t *testing.T) {
	t.Parallel()

	groupID := int64(3)
	product := CodexUsageClientProduct
	apiKey := &APIKey{
		Key:                     "sk-codex",
		Status:                  StatusActive,
		GroupID:                 &groupID,
		RestrictedClientProduct: &product,
		Group: &Group{
			ID:                   groupID,
			Status:               StatusActive,
			Hydrated:             true,
			Platform:             PlatformOpenAI,
			CodexGatewayEntitled: true,
		},
	}

	decision := EvaluateCodexScopedAPIKeyAccess(apiKey, "/v1/responses")
	require.False(t, decision.Allowed)
	require.Equal(t, infraerrors.Reason(ErrCodexKeyScopeMismatch), decision.Reason)
	require.ErrorIs(t, ValidateCodexScopedAPIKeyAccess(apiKey, "/v1/responses"), ErrCodexKeyScopeMismatch)
}
