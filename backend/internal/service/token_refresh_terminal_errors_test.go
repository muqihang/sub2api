package service

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsNonRetryableRefreshError_TerminalOpenAICodes(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "invalid_refresh_token",
			err:  errors.New(`OPENAI_OAUTH_TOKEN_REFRESH_FAILED: token refresh failed: status 401, body: {"error":{"code":"invalid_refresh_token"}}`),
		},
		{
			name: "app_session_terminated",
			err:  errors.New(`OPENAI_OAUTH_TOKEN_REFRESH_FAILED: token refresh failed: status 401, body: {"error":{"code":"app_session_terminated"}}`),
		},
		{
			name: "refresh_token_invalidated",
			err:  errors.New(`OPENAI_OAUTH_TOKEN_REFRESH_FAILED: token refresh failed: status 401, body: {"error":{"code":"refresh_token_invalidated"}}`),
		},
		{
			name: "token_expired",
			err:  errors.New(`OPENAI_OAUTH_TOKEN_REFRESH_FAILED: token refresh failed: status 401, body: {"error":{"code":"token_expired"}}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.True(t, isNonRetryableRefreshError(tt.err))
		})
	}
}
