package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildOpenAIChatCompletionsURLForAccountDeepSeekProviderRole(t *testing.T) {
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Extra: map[string]any{
			"provider_role": "deepseek",
		},
	}

	got := buildOpenAIChatCompletionsURLForAccount("https://api.deepseek.example", account)

	require.Equal(t, "https://api.deepseek.example/chat/completions", got)
}

func TestBuildOpenAIChatCompletionsURLForAccountExplicitPathOverride(t *testing.T) {
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"chat_completions_path": "/chat/completions",
		},
	}

	got := buildOpenAIChatCompletionsURLForAccount("https://compat.example/base", account)

	require.Equal(t, "https://compat.example/base/chat/completions", got)
}
