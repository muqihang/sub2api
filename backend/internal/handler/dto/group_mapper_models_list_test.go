package dto

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupFromServiceExposesPublicModelsList(t *testing.T) {
	got := GroupFromService(&service.Group{
		ID:       11,
		Name:     "vector group",
		Platform: service.PlatformOpenAI,
		ModelsListConfig: service.GroupModelsListConfig{
			Enabled: true,
			Models: []string{
				"Zhumeng-embeddings-1536",
				"Zhumeng-embeddings-1024",
				"Zhumeng-erank",
			},
		},
	})

	require.NotNil(t, got)
	require.True(t, got.ModelsListConfig.Enabled)
	require.Equal(t, []string{
		"Zhumeng-embeddings-1536",
		"Zhumeng-embeddings-1024",
		"Zhumeng-erank",
	}, got.ModelsListConfig.Models)
}
