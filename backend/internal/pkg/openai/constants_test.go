package openai

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultModels_ContainsGPT54Series(t *testing.T) {
	modelIDs := make(map[string]struct{}, len(DefaultModels))
	for _, model := range DefaultModels {
		modelIDs[model.ID] = struct{}{}
	}

	_, hasGPT54 := modelIDs["gpt-5.4"]
	_, hasGPT54Pro := modelIDs["gpt-5.4-pro"]
	require.True(t, hasGPT54, "DefaultModels should include gpt-5.4")
	require.True(t, hasGPT54Pro, "DefaultModels should include gpt-5.4-pro")
}
