package openai

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPairCodexClientIdentity(t *testing.T) {
	tests := []struct {
		name           string
		ua             string
		wantOriginator string
		wantUA         string
		wantOK         bool
	}{
		{
			name:           "cli prefix pairs directly",
			ua:             "codex_cli_rs/0.144.1 (Ubuntu 22.4.0; x86_64) xterm-256color",
			wantOriginator: "codex_cli_rs",
			wantUA:         "codex_cli_rs/0.144.1 (Ubuntu 22.4.0; x86_64) xterm-256color",
			wantOK:         true,
		},
		{
			name:           "trailer restores override identity",
			ua:             "cccc/0.142.0 (Ubuntu 22.4.0; x86_64) screen (codex-tui; 0.142.0)",
			wantOriginator: "codex-tui",
			wantUA:         "codex-tui/0.142.0 (Ubuntu 22.4.0; x86_64) screen (codex-tui; 0.142.0)",
			wantOK:         true,
		},
		{
			name:           "official identity canonicalizes exact names",
			ua:             "CODEX_CLI_RS/1.0.0",
			wantOriginator: "codex_cli_rs",
			wantUA:         "codex_cli_rs/1.0.0",
			wantOK:         true,
		},
		{name: "trailer slash is rejected", ua: "foo/1.0 (Codex Desktop/2; 1.0)", wantOK: false},
		{name: "control byte is rejected", ua: "Codex \x01evil/1.0.0", wantOK: false},
		{name: "oversized prefix is rejected", ua: "Codex " + strings.Repeat("a", 80) + "/1.0.0", wantOK: false},
		{name: "third party UA is rejected", ua: "luna/1.0.0", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originator, pairedUA, ok := PairCodexClientIdentity(tt.ua)
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.wantOriginator, originator)
			require.Equal(t, tt.wantUA, pairedUA)
		})
	}
}
