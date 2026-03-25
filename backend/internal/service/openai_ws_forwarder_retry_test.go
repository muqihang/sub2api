package service

import (
	"context"
	"errors"
	"testing"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func TestIsOpenAIWSIngressAccountRetryable(t *testing.T) {
	t.Run("dial_error_retryable", func(t *testing.T) {
		err := &openAIWSDialError{Err: errors.New("dial tcp failed")}
		require.True(t, IsOpenAIWSIngressAccountRetryable(err))
	})

	t.Run("turn_write_upstream_retryable", func(t *testing.T) {
		err := &openAIWSIngressTurnError{stage: "write_upstream", cause: errors.New("write failed"), wroteDownstream: false}
		require.True(t, IsOpenAIWSIngressAccountRetryable(err))
	})

	t.Run("turn_error_after_downstream_not_retryable", func(t *testing.T) {
		err := &openAIWSIngressTurnError{stage: "read_upstream", cause: errors.New("read failed"), wroteDownstream: true}
		require.False(t, IsOpenAIWSIngressAccountRetryable(err))
	})

	t.Run("client_close_error_not_retryable", func(t *testing.T) {
		err := NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "invalid request", nil)
		require.False(t, IsOpenAIWSIngressAccountRetryable(err))
	})

	t.Run("context_canceled_not_retryable", func(t *testing.T) {
		require.False(t, IsOpenAIWSIngressAccountRetryable(context.Canceled))
	})
}

func TestMergeOpenAIBetaValues(t *testing.T) {
	merged := mergeOpenAIBetaValues(openAIWSBetaV2Value, openAIWSBetaCompatV1, openAIWSBetaV2Value)
	require.Equal(t, openAIWSBetaV2Value+", "+openAIWSBetaCompatV1, merged)
}
