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
				ModelID:         "deepseek-v4-pro",
				Provider:        "deepseek",
				Route:           "deepseek_bridge",
				ClientType:      "claude_code_bridge_deepseek",
				ProviderOwner:   "zhumeng_managed",
				CredentialScope: "bridge_pool",
				GatewayLocation: "cloud",
				CatalogFresh:    true,
			},
			{
				ModelID:         "gpt-5.5",
				Provider:        "openai",
				Route:           "openai_bridge",
				ClientType:      "claude_code_bridge_openai",
				ProviderOwner:   "zhumeng_managed",
				CredentialScope: "bridge_pool",
				GatewayLocation: "cloud",
				CatalogFresh:    true,
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
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", `{"catalog_version":"env-catalog-v1","runtime_hash":"sha256:1111111111111111111111111111111111111111111111111111111111111111","overlay_hash":"sha256:2222222222222222222222222222222222222222222222222222222222222222","catalog_hash":"sha256:3333333333333333333333333333333333333333333333333333333333333333","models":[{"model_id":"deepseek-v4-pro","provider":"deepseek","route":"deepseek_bridge","client_type":"claude_code_bridge_deepseek","provider_owner":"zhumeng_managed","credential_scope":"bridge_pool","gateway_location":"cloud","catalog_fresh":true}]}`)

	decision, err := LoadClaudeCodeProviderRegistryFromEnv().Resolve(context.Background(), "deepseek-v4-pro")

	require.NoError(t, err)
	require.Equal(t, "env-catalog-v1", decision.CatalogVersion)
	require.Equal(t, "sha256:"+stringOf('3', 64), decision.CatalogHash)
	require.Equal(t, "claude_code_bridge_deepseek", decision.ClientType)
	require.False(t, decision.FormalPoolAllowed)
}
