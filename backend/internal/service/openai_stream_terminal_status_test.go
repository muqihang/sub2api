//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeResponsesStreamingTerminalOutput_AddsMissingCompletedStatus(t *testing.T) {
	input := []byte(`{"type":"response.completed","response":{"object":"response.compaction","output":[{"type":"compaction_summary"}]}}`)

	got, normalized := normalizeResponsesStreamingTerminalOutput(input, nil, nil)

	require.True(t, normalized)
	require.Equal(t, "completed", gjson.GetBytes(got, "response.status").String())
	require.Equal(t, "compaction_summary", gjson.GetBytes(got, "response.output.0.type").String())
}
