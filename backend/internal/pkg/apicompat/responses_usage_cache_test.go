package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponsesUsageNestedCacheWritePresenceOverridesTopLevelAlias(t *testing.T) {
	tests := []struct {
		name       string
		nestedJSON string
		want       int
	}{
		{name: "explicit zero", nestedJSON: `{"cache_write_tokens":0}`, want: 0},
		{name: "nonzero", nestedJSON: `{"cache_write_tokens":7}`, want: 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var usage ResponsesUsage
			payload := []byte(`{"input_tokens":20,"output_tokens":2,"cache_creation_input_tokens":19,"input_tokens_details":` + tt.nestedJSON + `}`)
			require.NoError(t, json.Unmarshal(payload, &usage))
			require.Equal(t, tt.want, usage.CacheCreationInputTokens)
		})
	}
}
