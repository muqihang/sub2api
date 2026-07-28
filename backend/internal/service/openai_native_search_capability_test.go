package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountSupportsOpenAIEndpointCapability_SearchRequiresNativeProvider(t *testing.T) {
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
		{name: "unconfigured api key", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}},
		{
			name: "api key explicitly supports search",
			account: &Account{
				Platform:    PlatformOpenAI,
				Type:        AccountTypeAPIKey,
				Credentials: map[string]any{"openai_capabilities": []any{"responses", "search"}},
			},
			want: true,
		},
		{
			name: "api key explicitly disables search",
			account: &Account{
				Platform:    PlatformOpenAI,
				Type:        AccountTypeAPIKey,
				Credentials: map[string]any{"openai_capabilities": map[string]any{"search": false}},
			},
		},
		{
			name: "upstream explicitly supports search",
			account: &Account{
				Platform:    PlatformOpenAI,
				Type:        AccountTypeUpstream,
				Credentials: map[string]any{"openai_capabilities": []any{"search"}},
			},
			want: true,
		},
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

func TestAccountSupportsOpenAIEndpointCapability_WebSearchRequiresSuccessfulProbe(t *testing.T) {
	tests := []struct {
		name    string
		account *Account
		want    bool
	}{
		{name: "unprobed API key", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}},
		{name: "successful API key", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: map[string]any{"openai_web_search_supported": true}}, want: true},
		{name: "failed API key", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: map[string]any{"openai_web_search_supported": false}}},
		{name: "successful upstream", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeUpstream, Extra: map[string]any{"openai_web_search_supported": true}}, want: true},
		{name: "successful OAuth", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"openai_web_search_supported": true}}, want: true},
		{name: "other platform", account: &Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Extra: map[string]any{"openai_web_search_supported": true}}},
		{name: "nil"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityWebSearch))
		})
	}
}

func TestAccountSupportsOpenAIEndpointCapability_CustomToolsUsesSemanticProbe(t *testing.T) {
	tests := []struct {
		name    string
		account *Account
		want    bool
	}{
		{name: "unprobed API key preserves existing behavior", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, want: true},
		{name: "successful API key", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: map[string]any{"openai_responses_custom_tools_supported": true}}, want: true},
		{name: "failed API key", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: map[string]any{"openai_responses_custom_tools_supported": false}}},
		{name: "OAuth uses native Codex tools", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, want: true},
		{name: "other platform", account: &Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Extra: map[string]any{"openai_responses_custom_tools_supported": true}}},
		{name: "nil"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityResponsesCustomTools))
		})
	}
}

func TestAccountSupportsOpenAIEndpointCapability_WebSearchAndCustomToolsRequiresBoth(t *testing.T) {
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Extra: map[string]any{
			"openai_web_search_supported":             true,
			"openai_responses_custom_tools_supported": false,
		},
	}
	require.False(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityWebSearchCustomTools))

	account.Extra["openai_responses_custom_tools_supported"] = true
	require.True(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityWebSearchCustomTools))
}

func TestAccountSupportsOpenAIRequestCapabilities_CustomToolProbeIsModelScoped(t *testing.T) {
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"base_url": "https://api.example.com/v1",
			"model_mapping": map[string]any{
				"gpt-5.6-sol":  "upstream-sol",
				"gpt-5.6-luna": "upstream-luna",
			},
		},
		Extra: map[string]any{
			"openai_responses_custom_tools_supported":   false,
			"openai_responses_custom_tools_probe_model": "upstream-sol",
		},
	}
	account.Extra["openai_responses_custom_tools_probe_target"] = account.OpenAIResponsesCustomToolsTargetFingerprint("upstream-sol")

	require.False(t, account.SupportsOpenAIResponsesCustomToolsForModel("gpt-5.6-sol"))
	require.True(t, account.SupportsOpenAIResponsesCustomToolsForModel("gpt-5.6-luna"))

	account.Credentials["base_url"] = "https://new-api.example.com/v1"
	require.True(t, account.SupportsOpenAIResponsesCustomToolsForModel("gpt-5.6-sol"))
}
