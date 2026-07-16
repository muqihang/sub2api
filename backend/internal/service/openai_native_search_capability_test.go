package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountSupportsOpenAIEndpointCapability_SearchIsInternalOAuthOnly(t *testing.T) {
	tests := []struct {
		name    string
		account *Account
		want    bool
	}{
		{name: "oauth", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, want: true},
		{
			name: "oauth explicit list cannot disable search",
			account: &Account{
				Platform:    PlatformOpenAI,
				Type:        AccountTypeOAuth,
				Credentials: map[string]any{"openai_capabilities": []any{"chat_completions"}},
			},
			want: true,
		},
		{name: "api key", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}},
		{name: "upstream", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeUpstream}},
		{name: "other platform", account: &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth}},
		{name: "nil"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilitySearch))
		})
	}
}
