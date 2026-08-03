package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnforceCodexIdentityHeaders(t *testing.T) {
	const tuiUA = "codex-tui/0.140.2 (Mac OS X 14.0; arm64) iTerm (codex-tui; 0.140.2)"

	tests := []struct {
		name           string
		originator     string
		userAgent      string
		version        string
		wantOriginator string
		wantUA         string
		wantVersion    string
	}{
		{
			name:           "mismatched originator follows final UA",
			originator:     "codex_cli_rs",
			userAgent:      tuiUA,
			wantOriginator: "codex-tui",
			wantUA:         tuiUA,
		},
		{
			name:           "unofficial UA falls back to default identity",
			originator:     "opencode",
			userAgent:      "luna/1.0.0",
			wantOriginator: "codex_cli_rs",
			wantUA:         codexCLIUserAgent,
		},
		{
			name:           "low version is raised",
			originator:     "codex_cli_rs",
			userAgent:      "codex_cli_rs/0.125.0",
			version:        "0.125.0",
			wantOriginator: "codex_cli_rs",
			wantUA:         "codex_cli_rs/0.125.0",
			wantVersion:    codexCLIVersion,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := make(http.Header)
			headers.Set("originator", tt.originator)
			headers.Set("user-agent", tt.userAgent)
			if tt.version != "" {
				headers.Set("version", tt.version)
			}

			enforceCodexIdentityHeaders(headers)

			require.Equal(t, tt.wantOriginator, headers.Get("originator"))
			require.Equal(t, tt.wantUA, headers.Get("user-agent"))
			require.Equal(t, tt.wantVersion, headers.Get("version"))
		})
	}
}

func TestEnforceCodexIdentityHeadersWithoutOriginatorIsNoop(t *testing.T) {
	headers := make(http.Header)
	headers.Set("user-agent", "luna/1.0.0")

	enforceCodexIdentityHeaders(headers)

	require.Empty(t, headers.Get("originator"))
	require.Equal(t, "luna/1.0.0", headers.Get("user-agent"))
}
