package service

import (
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestAugmentScopedAPIKeyAccessAllowsExplicitRuntimeRoutes(t *testing.T) {
	t.Parallel()

	groupID := int64(1)
	product := AugmentClientProductZhumeng
	apiKey := &APIKey{
		ID:                      10,
		UserID:                  20,
		Key:                     "sk-augment",
		Status:                  StatusActive,
		GroupID:                 &groupID,
		RestrictedClientProduct: &product,
		Group: &Group{
			ID:                     groupID,
			Status:                 StatusActive,
			Hydrated:               true,
			Platform:               PlatformOpenAI,
			AugmentGatewayEntitled: true,
		},
	}

	for _, path := range []string{
		"/api/v1/plugin/augment/summary",
		"/chat",
		"/prompt-enhancer",
		"/report-error",
	} {
		t.Run(path, func(t *testing.T) {
			decision := EvaluateAugmentScopedAPIKeyAccess(apiKey, path)
			require.True(t, decision.Allowed)
			require.NoError(t, ValidateAugmentScopedAPIKeyAccess(apiKey, path))
		})
	}
}

func TestAugmentScopedAPIKeyAccessRejectsGenericOrNonEntitledKeys(t *testing.T) {
	t.Parallel()

	groupID := int64(2)
	product := AugmentClientProductZhumeng

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
					ID:                     groupID,
					Status:                 StatusActive,
					Hydrated:               true,
					Platform:               PlatformOpenAI,
					AugmentGatewayEntitled: true,
				},
			},
			want: ErrAugmentScopedAPIKeyRequired,
		},
		{
			name: "non entitled group",
			apiKey: &APIKey{
				Key:                     "sk-augment-no-entitlement",
				Status:                  StatusActive,
				GroupID:                 &groupID,
				RestrictedClientProduct: &product,
				Group: &Group{
					ID:       groupID,
					Status:   StatusActive,
					Hydrated: true,
					Platform: PlatformOpenAI,
				},
			},
			want: ErrAugmentScopedAPIKeyRequired,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decision := EvaluateAugmentScopedAPIKeyAccess(tc.apiKey, "/chat")
			require.False(t, decision.Allowed)
			require.Equal(t, infraerrors.Reason(tc.want), decision.Reason)
			require.ErrorIs(t, ValidateAugmentScopedAPIKeyAccess(tc.apiKey, "/chat"), tc.want)
		})
	}
}

func TestAugmentScopedAPIKeyAccessRejectsUnknownRoutes(t *testing.T) {
	t.Parallel()

	groupID := int64(3)
	product := AugmentClientProductZhumeng
	apiKey := &APIKey{
		Key:                     "sk-augment",
		Status:                  StatusActive,
		GroupID:                 &groupID,
		RestrictedClientProduct: &product,
		Group: &Group{
			ID:                     groupID,
			Status:                 StatusActive,
			Hydrated:               true,
			Platform:               PlatformOpenAI,
			AugmentGatewayEntitled: true,
		},
	}

	decision := EvaluateAugmentScopedAPIKeyAccess(apiKey, "/responses")
	require.False(t, decision.Allowed)
	require.Equal(t, infraerrors.Reason(ErrAugmentKeyScopeMismatch), decision.Reason)
	require.ErrorIs(t, ValidateAugmentScopedAPIKeyAccess(apiKey, "/responses"), ErrAugmentKeyScopeMismatch)
}
