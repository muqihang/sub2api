//go:build unit

package handler

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func newObservedLogger(t *testing.T) (*zap.Logger, *observer.ObservedLogs) {
	t.Helper()
	core, logs := observer.New(zap.WarnLevel)
	return zap.New(core), logs
}

func loggedFields(t *testing.T, logs *observer.ObservedLogs) map[string]any {
	t.Helper()
	entries := logs.All()
	require.Len(t, entries, 1)
	fields := map[string]any{}
	for _, f := range entries[0].Context {
		switch f.Key {
		case "body_len":
			fields[f.Key] = int(f.Integer)
		case "error":
			fields[f.Key] = f.Interface.(error).Error()
		default:
			fields[f.Key] = f.String
		}
	}
	return fields
}

func TestLogRequestBodyParseFailure_DerivesErrorWhenNil(t *testing.T) {
	log, logs := newObservedLogger(t)
	body := []byte(`{"model": bad}`)

	logRequestBodyParseFailure(log, body, nil)

	fields := loggedFields(t, logs)
	require.Equal(t, len(body), fields["body_len"])
	require.Contains(t, fields["error"], "invalid json")
	require.Contains(t, fields["error"], "offset=11")
}

func TestLogRequestBodyParseFailure_DoesNotLogPayloadSnippets(t *testing.T) {
	log, logs := newObservedLogger(t)
	body := []byte(`{"prompt":"private prompt","api_key":"sk-test-secret","broken":`)

	logRequestBodyParseFailure(log, body, nil)

	fields := loggedFields(t, logs)
	require.NotContains(t, fields, "body_head")
	require.NotContains(t, fields, "body_tail")
	require.NotContains(t, fields["error"], "private prompt")
	require.NotContains(t, fields["error"], "sk-test-secret")
}

func TestLogRequestBodyParseFailure_LargeBodyLogsOnlyLength(t *testing.T) {
	log, logs := newObservedLogger(t)
	body := []byte(`{"model":"claude-sonnet-4-6","big":"private-body-tail"`)

	logRequestBodyParseFailure(log, body, nil)

	fields := loggedFields(t, logs)
	require.Equal(t, len(body), fields["body_len"])
	require.NotContains(t, fields, "body_head")
	require.NotContains(t, fields, "body_tail")
	require.NotContains(t, fields["error"], "private-body-tail")
}

func TestLogRequestBodyParseFailure_ControlCharactersDoNotCreatePayloadFields(t *testing.T) {
	log, logs := newObservedLogger(t)
	body := []byte("{\"model\":\x01\n\"x\"}")

	logRequestBodyParseFailure(log, body, nil)

	fields := loggedFields(t, logs)
	require.NotContains(t, fields, "body_head")
	require.NotContains(t, fields, "body_tail")
}

func TestLogRequestBodyParseFailure_NilLoggerNoPanic(t *testing.T) {
	require.NotPanics(t, func() {
		logRequestBodyParseFailure(nil, []byte(`{`), nil)
	})
}
