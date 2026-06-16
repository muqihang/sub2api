package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClaudeCodeProviderRegistryResolvesNativeAndBridgeFromServerCatalog(t *testing.T) {
	registry := NewClaudeCodeProviderRegistry(ClaudeCodeProviderCatalog{
		CatalogVersion: "cp5-catalog-v1",
		RuntimeHash:    "sha256:" + stringOf('1', 64),
		OverlayHash:    "sha256:" + stringOf('2', 64),
		CatalogHash:    "sha256:" + stringOf('3', 64),
		Models: []ClaudeCodeProviderCatalogEntry{
			{
				ModelID:                  "claude-sonnet-4-6",
				Provider:                 "claude",
				Route:                    ClaudeCodeNativeRoute,
				ClientType:               ClaudeCodeNativeClientType,
				ProviderOwner:            ClaudeCodeNativeProviderOwner,
				CredentialScope:          ClaudeCodeNativeCredentialScope,
				GatewayLocation:          ClaudeCodeNativeGatewayLocation,
				CatalogFresh:             true,
				FormalPoolAllowed:        true,
				NativeAttestationAllowed: true,
			},
			{
				ModelID:                  "deepseek-v4-pro",
				Provider:                 "deepseek",
				Route:                    "deepseek_bridge",
				ClientType:               "claude_code_bridge_deepseek",
				ProviderOwner:            "zhumeng_managed",
				CredentialScope:          "bridge_pool",
				GatewayLocation:          "cloud",
				CatalogFresh:             true,
				PreferredProtocol:        "anthropic_messages",
				AnthropicBaseURL:         "https://api.deepseek.com/anthropic",
				CapabilitiesVerified:     true,
				SupportsText:             true,
				SupportsTools:            true,
				SupportsStreaming:        true,
				SupportsUsage:            true,
				SupportsErrorPassthrough: true,
			},
			{
				ModelID:                  "gpt-5.5",
				Provider:                 "openai",
				Route:                    "openai_bridge",
				ClientType:               "claude_code_bridge_openai",
				ProviderOwner:            "zhumeng_managed",
				CredentialScope:          "bridge_pool",
				GatewayLocation:          "cloud",
				CatalogFresh:             true,
				PreferredProtocol:        "responses",
				OpenAIBaseURL:            "https://api.openai.com/v1",
				CapabilitiesVerified:     true,
				SupportsText:             true,
				SupportsTools:            true,
				SupportsStreaming:        true,
				SupportsUsage:            true,
				SupportsErrorPassthrough: true,
			},
		},
	})

	native, err := registry.Resolve(context.Background(), "claude-sonnet-4-6")
	require.NoError(t, err)
	require.Equal(t, ClaudeCodeNativeRoute, native.Route)
	require.True(t, native.FormalPoolAllowed)
	require.True(t, native.NativeAttestationAllowed)
	require.Equal(t, "cp5-catalog-v1", native.CatalogVersion)

	deepseek, err := registry.Resolve(context.Background(), "deepseek-v4-pro")
	require.NoError(t, err)
	require.Equal(t, "deepseek_bridge", deepseek.Route)
	require.Equal(t, "claude_code_bridge_deepseek", deepseek.ClientType)
	require.False(t, deepseek.FormalPoolAllowed)
	require.False(t, deepseek.NativeAttestationAllowed)
	require.Equal(t, "bridge_pool", deepseek.CredentialScope)

	openai, err := registry.Resolve(context.Background(), "gpt-5.5")
	require.NoError(t, err)
	require.Equal(t, "openai_bridge", openai.Route)
	require.Equal(t, "claude_code_bridge_openai", openai.ClientType)
	require.False(t, openai.FormalPoolAllowed)
	require.False(t, openai.NativeAttestationAllowed)
}

func TestClaudeCodeProviderRegistryRequiresNativeClaudeModelAndCatalogVersion(t *testing.T) {
	miscataloged := NewClaudeCodeProviderRegistry(ClaudeCodeProviderCatalog{CatalogVersion: "cp5-catalog-v1", Models: []ClaudeCodeProviderCatalogEntry{{
		ModelID:                  "gpt-5.5",
		Provider:                 "claude",
		Route:                    ClaudeCodeNativeRoute,
		ClientType:               ClaudeCodeNativeClientType,
		ProviderOwner:            ClaudeCodeNativeProviderOwner,
		CredentialScope:          ClaudeCodeNativeCredentialScope,
		GatewayLocation:          ClaudeCodeNativeGatewayLocation,
		CatalogFresh:             true,
		FormalPoolAllowed:        true,
		NativeAttestationAllowed: true,
	}}})
	_, err := miscataloged.Resolve(context.Background(), "gpt-5.5")
	require.Error(t, err)
	require.Contains(t, err.Error(), "native")

	missingVersion := NewClaudeCodeProviderRegistry(ClaudeCodeProviderCatalog{Models: []ClaudeCodeProviderCatalogEntry{{
		ModelID:                  "claude-sonnet-4-6",
		Provider:                 "claude",
		Route:                    ClaudeCodeNativeRoute,
		ClientType:               ClaudeCodeNativeClientType,
		ProviderOwner:            ClaudeCodeNativeProviderOwner,
		CredentialScope:          ClaudeCodeNativeCredentialScope,
		GatewayLocation:          ClaudeCodeNativeGatewayLocation,
		CatalogFresh:             true,
		FormalPoolAllowed:        true,
		NativeAttestationAllowed: true,
	}}})
	_, err = missingVersion.Resolve(context.Background(), "claude-sonnet-4-6")
	require.Error(t, err)
	require.Contains(t, err.Error(), "native")
}

func TestClaudeCodeProviderRegistryRequiresBridgePoolScope(t *testing.T) {
	registry := NewClaudeCodeProviderRegistry(ClaudeCodeProviderCatalog{Models: []ClaudeCodeProviderCatalogEntry{{
		ModelID:         "gpt-5.5",
		Provider:        "openai",
		Route:           "openai_bridge",
		ClientType:      "claude_code_bridge_openai",
		ProviderOwner:   "zhumeng_managed",
		CredentialScope: "user_pool",
		GatewayLocation: "cloud",
		CatalogFresh:    true,
	}}})

	_, err := registry.Resolve(context.Background(), "gpt-5.5")

	require.Error(t, err)
	require.Contains(t, err.Error(), "bridge")
}

func TestClaudeCodeProviderRegistryFailsClosedForUnknownStaleAndSpoofedBridge(t *testing.T) {
	registry := NewClaudeCodeProviderRegistry(ClaudeCodeProviderCatalog{
		CatalogVersion: "cp5-catalog-v1",
		Models: []ClaudeCodeProviderCatalogEntry{
			{
				ModelID:                  "deepseek-v4-pro",
				Provider:                 "deepseek",
				Route:                    "deepseek_bridge",
				ClientType:               ClaudeCodeNativeClientType,
				ProviderOwner:            "zhumeng_managed",
				CredentialScope:          ClaudeCodeNativeCredentialScope,
				GatewayLocation:          "cloud",
				CatalogFresh:             true,
				FormalPoolAllowed:        true,
				NativeAttestationAllowed: true,
			},
			{
				ModelID:         "gpt-5.5",
				Provider:        "openai",
				Route:           "openai_bridge",
				ClientType:      "claude_code_bridge_openai",
				ProviderOwner:   "zhumeng_managed",
				CredentialScope: "bridge_pool",
				GatewayLocation: "cloud",
				CatalogFresh:    false,
			},
		},
	})

	_, err := registry.Resolve(context.Background(), "missing-model")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown")
	_, err = registry.Resolve(context.Background(), "gpt-5.5")
	require.Error(t, err)
	require.Contains(t, err.Error(), "stale")
	_, err = registry.Resolve(context.Background(), "deepseek-v4-pro")
	require.Error(t, err)
	require.Contains(t, err.Error(), "bridge")
}

func TestClaudeCodeProviderRegistryLoadsEnvCatalogAsSourceOfTruth(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", `{"catalog_version":"env-catalog-v1","runtime_hash":"sha256:1111111111111111111111111111111111111111111111111111111111111111","overlay_hash":"sha256:2222222222222222222222222222222222222222222222222222222222222222","catalog_hash":"sha256:3333333333333333333333333333333333333333333333333333333333333333","models":[{"model_id":"deepseek-v4-pro","provider":"deepseek","route":"deepseek_bridge","client_type":"claude_code_bridge_deepseek","provider_owner":"zhumeng_managed","credential_scope":"bridge_pool","gateway_location":"cloud","catalog_fresh":true,"preferred_protocol":"anthropic_messages","anthropic_base_url":"https://api.deepseek.com/anthropic","capabilities_verified":true,"supports_text":true,"supports_tools":true,"supports_streaming":true,"supports_usage":true,"supports_error_passthrough":true}]}`)

	decision, err := LoadClaudeCodeProviderRegistryFromEnv().Resolve(context.Background(), "deepseek-v4-pro")

	require.NoError(t, err)
	require.Equal(t, "env-catalog-v1", decision.CatalogVersion)
	require.Equal(t, "sha256:"+stringOf('3', 64), decision.CatalogHash)
	require.Equal(t, "claude_code_bridge_deepseek", decision.ClientType)
	require.False(t, decision.FormalPoolAllowed)
}

func TestCP6ProviderRegistryCarriesBridgeProbeTransportContract(t *testing.T) {
	registry := NewClaudeCodeProviderRegistry(ClaudeCodeProviderCatalog{
		CatalogVersion: "cp6-catalog-v1",
		RuntimeHash:    "sha256:" + stringOf('1', 64),
		OverlayHash:    "sha256:" + stringOf('2', 64),
		CatalogHash:    "sha256:" + stringOf('3', 64),
		Models: []ClaudeCodeProviderCatalogEntry{
			{
				ModelID:                  "deepseek-v4-pro",
				Provider:                 "deepseek",
				Route:                    "deepseek_bridge",
				ClientType:               "claude_code_bridge_deepseek",
				ProviderOwner:            "zhumeng_managed",
				CredentialScope:          "bridge_pool",
				GatewayLocation:          "cloud",
				CatalogFresh:             true,
				PreferredProtocol:        "anthropic_messages",
				AnthropicBaseURL:         "https://api.deepseek.com/anthropic",
				OpenAIBaseURL:            "https://api.deepseek.com",
				FallbackProtocol:         "openai_chat_completions",
				FallbackReason:           "anthropic_tool_sse_reasoning_cache_error_fixture_failed",
				CapabilitiesVerified:     true,
				SupportsText:             true,
				SupportsTools:            true,
				SupportsStreaming:        true,
				SupportsUsage:            true,
				SupportsCacheAudit:       true,
				SupportsReasoningMapping: true,
				SupportsErrorPassthrough: true,
				ReasoningEffortLevels:    []string{"high", "max"},
				CachePolicy:              "provider_prefix_kv_cache_automatic_full_prefix_unit_match",
			},
		},
	})

	decision, err := registry.Resolve(context.Background(), "deepseek-v4-pro")

	require.NoError(t, err)
	require.Equal(t, "anthropic_messages", decision.PreferredProtocol)
	require.Equal(t, "https://api.deepseek.com/anthropic", decision.AnthropicBaseURL)
	require.Equal(t, "https://api.deepseek.com", decision.OpenAIBaseURL)
	require.Equal(t, "openai_chat_completions", decision.FallbackProtocol)
	require.Equal(t, "anthropic_tool_sse_reasoning_cache_error_fixture_failed", decision.FallbackReason)
	require.True(t, decision.CapabilitiesVerified)
	require.True(t, decision.SupportsTools)
	require.True(t, decision.SupportsStreaming)
	require.True(t, decision.SupportsCacheAudit)
	require.True(t, decision.SupportsReasoningMapping)
	require.Equal(t, []string{"high", "max"}, decision.ReasoningEffortLevels)
	require.Equal(t, "provider_prefix_kv_cache_automatic_full_prefix_unit_match", decision.CachePolicy)

	bridge := decision.BridgeRouteDecision()
	require.Equal(t, "anthropic_messages", bridge.PreferredProtocol)
	require.Equal(t, "openai_chat_completions", bridge.FallbackProtocol)
	require.Equal(t, "anthropic_tool_sse_reasoning_cache_error_fixture_failed", bridge.FallbackReason)
	require.True(t, bridge.CapabilitiesVerified)
}

func TestCP6ProviderRegistryRejectsUnverifiedBridgeCapabilities(t *testing.T) {
	registry := NewClaudeCodeProviderRegistry(ClaudeCodeProviderCatalog{
		CatalogVersion: "cp6-catalog-v1",
		RuntimeHash:    "sha256:" + stringOf('1', 64),
		OverlayHash:    "sha256:" + stringOf('2', 64),
		CatalogHash:    "sha256:" + stringOf('3', 64),
		Models: []ClaudeCodeProviderCatalogEntry{
			{
				ModelID:              "deepseek-v4-pro",
				Provider:             "deepseek",
				Route:                "deepseek_bridge",
				ClientType:           "claude_code_bridge_deepseek",
				ProviderOwner:        "zhumeng_managed",
				CredentialScope:      "bridge_pool",
				GatewayLocation:      "cloud",
				CatalogFresh:         true,
				PreferredProtocol:    "anthropic_messages",
				AnthropicBaseURL:     "https://api.deepseek.com/anthropic",
				CapabilitiesVerified: false,
				SupportsText:         true,
				SupportsTools:        true,
			},
		},
	})

	_, err := registry.Resolve(context.Background(), "deepseek-v4-pro")

	require.Error(t, err)
	require.Contains(t, err.Error(), "capability")
}

func TestCP6ProviderRegistryRejectsBridgeCapabilityTruthinessMismatches(t *testing.T) {
	base := func() ClaudeCodeProviderCatalogEntry {
		return ClaudeCodeProviderCatalogEntry{
			ModelID:                  "deepseek-v4-pro",
			Provider:                 "deepseek",
			Route:                    "deepseek_bridge",
			ClientType:               "claude_code_bridge_deepseek",
			ProviderOwner:            "zhumeng_managed",
			CredentialScope:          "bridge_pool",
			GatewayLocation:          "cloud",
			CatalogFresh:             true,
			PreferredProtocol:        "anthropic_messages",
			AnthropicBaseURL:         "https://api.deepseek.com/anthropic",
			CapabilitiesVerified:     true,
			SupportsText:             true,
			SupportsTools:            true,
			SupportsStreaming:        true,
			SupportsUsage:            true,
			SupportsErrorPassthrough: true,
		}
	}
	tests := []struct {
		name   string
		mutate func(*ClaudeCodeProviderCatalogEntry)
	}{
		{name: "cache policy requires cache audit", mutate: func(entry *ClaudeCodeProviderCatalogEntry) {
			entry.CachePolicy = "provider_prefix_kv_cache_automatic_full_prefix_unit_match"
		}},
		{name: "reasoning levels require reasoning mapping", mutate: func(entry *ClaudeCodeProviderCatalogEntry) { entry.ReasoningEffortLevels = []string{"high", "max"} }},
		{name: "tools support cannot be missing when capability contract is present", mutate: func(entry *ClaudeCodeProviderCatalogEntry) { entry.SupportsTools = false }},
		{name: "usage support cannot be missing when capability contract is present", mutate: func(entry *ClaudeCodeProviderCatalogEntry) { entry.SupportsUsage = false }},
		{name: "error passthrough flag cannot be missing when capability contract is present", mutate: func(entry *ClaudeCodeProviderCatalogEntry) { entry.SupportsErrorPassthrough = false }},
		{name: "preferred protocol requires base url", mutate: func(entry *ClaudeCodeProviderCatalogEntry) { entry.AnthropicBaseURL = "" }},
		{name: "openai compatible protocol requires streaming", mutate: func(entry *ClaudeCodeProviderCatalogEntry) {
			entry.PreferredProtocol = "responses"
			entry.AnthropicBaseURL = ""
			entry.OpenAIBaseURL = "https://api.openai.com/v1"
			entry.SupportsStreaming = false
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := base()
			tt.mutate(&entry)
			registry := NewClaudeCodeProviderRegistry(ClaudeCodeProviderCatalog{
				CatalogVersion: "cp6-catalog-v1",
				RuntimeHash:    "sha256:" + stringOf('1', 64),
				OverlayHash:    "sha256:" + stringOf('2', 64),
				CatalogHash:    "sha256:" + stringOf('3', 64),
				Models:         []ClaudeCodeProviderCatalogEntry{entry},
			})

			_, err := registry.Resolve(context.Background(), "deepseek-v4-pro")

			require.Error(t, err)
			require.Contains(t, err.Error(), "capability")
		})
	}
}

func TestCP6ProviderRegistryRejectsNativeEntriesWithBridgeOnlyMetadata(t *testing.T) {
	registry := NewClaudeCodeProviderRegistry(ClaudeCodeProviderCatalog{
		CatalogVersion: "cp6-catalog-v1",
		RuntimeHash:    "sha256:" + stringOf('1', 64),
		OverlayHash:    "sha256:" + stringOf('2', 64),
		CatalogHash:    "sha256:" + stringOf('3', 64),
		Models: []ClaudeCodeProviderCatalogEntry{{
			ModelID:                  "claude-sonnet-4-6",
			Provider:                 "claude",
			Route:                    ClaudeCodeNativeRoute,
			ClientType:               ClaudeCodeNativeClientType,
			ProviderOwner:            ClaudeCodeNativeProviderOwner,
			CredentialScope:          ClaudeCodeNativeCredentialScope,
			GatewayLocation:          ClaudeCodeNativeGatewayLocation,
			CatalogFresh:             true,
			FormalPoolAllowed:        true,
			NativeAttestationAllowed: true,
			PreferredProtocol:        "anthropic_messages",
			CapabilitiesVerified:     true,
			SupportsText:             true,
		}},
	})

	_, err := registry.Resolve(context.Background(), "claude-sonnet-4-6")

	require.Error(t, err)
	require.Contains(t, err.Error(), "native")
}
