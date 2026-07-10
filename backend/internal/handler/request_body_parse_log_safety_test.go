package handler

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestLogRequestBodyParseFailureOmitsRawPayload(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	payload := []byte("{\"prompt\":\"private prompt\",\"api_key\":\"sk-test-secret\",\"broken\":")

	logRequestBodyParseFailure(zap.New(core), payload, nil)

	entries := logs.All()
	require.Len(t, entries, 1)
	for _, field := range entries[0].Context {
		require.NotEqual(t, "body_head", field.Key)
		require.NotEqual(t, "body_tail", field.Key)
		require.NotContains(t, field.String, "private prompt")
		require.NotContains(t, field.String, "sk-test-secret")
	}
}
