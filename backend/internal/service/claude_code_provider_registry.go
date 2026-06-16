package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type ClaudeCodeProviderCatalog struct {
	CatalogVersion string                           `json:"catalog_version"`
	RuntimeHash    string                           `json:"runtime_hash"`
	OverlayHash    string                           `json:"overlay_hash"`
	CatalogHash    string                           `json:"catalog_hash"`
	Models         []ClaudeCodeProviderCatalogEntry `json:"models"`
}

type ClaudeCodeProviderCatalogEntry struct {
	ModelID                  string `json:"model_id"`
	Provider                 string `json:"provider"`
	Route                    string `json:"route"`
	ClientType               string `json:"client_type"`
	ProviderOwner            string `json:"provider_owner"`
	CredentialScope          string `json:"credential_scope"`
	GatewayLocation          string `json:"gateway_location"`
	CatalogFresh             bool   `json:"catalog_fresh"`
	FormalPoolAllowed        bool   `json:"formal_pool_allowed"`
	NativeAttestationAllowed bool   `json:"native_attestation_allowed"`
}

type ClaudeCodeProviderRouteDecision struct {
	ModelID                  string
	Provider                 string
	Route                    string
	ClientType               string
	ProviderOwner            string
	CredentialScope          string
	GatewayLocation          string
	CatalogFresh             bool
	FormalPoolAllowed        bool
	NativeAttestationAllowed bool
	CatalogVersion           string
	RuntimeHash              string
	OverlayHash              string
	CatalogHash              string
}

type ClaudeCodeProviderRegistry struct {
	catalog ClaudeCodeProviderCatalog
	entries map[string]ClaudeCodeProviderCatalogEntry
}

func NewClaudeCodeProviderRegistry(catalog ClaudeCodeProviderCatalog) *ClaudeCodeProviderRegistry {
	entries := make(map[string]ClaudeCodeProviderCatalogEntry, len(catalog.Models))
	for _, entry := range catalog.Models {
		entry.ModelID = strings.TrimSpace(entry.ModelID)
		if entry.ModelID == "" {
			continue
		}
		entries[entry.ModelID] = entry
	}
	return &ClaudeCodeProviderRegistry{catalog: catalog, entries: entries}
}

func LoadClaudeCodeProviderRegistryFromEnv() *ClaudeCodeProviderRegistry {
	raw := strings.TrimSpace(os.Getenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON"))
	if raw == "" {
		return NewClaudeCodeProviderRegistry(ClaudeCodeProviderCatalog{})
	}
	var catalog ClaudeCodeProviderCatalog
	if err := json.Unmarshal([]byte(raw), &catalog); err != nil {
		return NewClaudeCodeProviderRegistry(ClaudeCodeProviderCatalog{})
	}
	return NewClaudeCodeProviderRegistry(catalog)
}

func (r *ClaudeCodeProviderRegistry) Resolve(ctx context.Context, model string) (ClaudeCodeProviderRouteDecision, error) {
	_ = ctx
	if r == nil {
		return ClaudeCodeProviderRouteDecision{}, fmt.Errorf("claude code provider registry is not configured")
	}
	model = strings.TrimSpace(model)
	entry, ok := r.entries[model]
	if !ok || model == "" || looksSensitiveText(model) {
		return ClaudeCodeProviderRouteDecision{}, fmt.Errorf("claude code provider registry unknown model")
	}
	decision := ClaudeCodeProviderRouteDecision{
		ModelID:                  entry.ModelID,
		Provider:                 strings.TrimSpace(entry.Provider),
		Route:                    strings.TrimSpace(entry.Route),
		ClientType:               strings.TrimSpace(entry.ClientType),
		ProviderOwner:            strings.TrimSpace(entry.ProviderOwner),
		CredentialScope:          strings.TrimSpace(entry.CredentialScope),
		GatewayLocation:          strings.TrimSpace(entry.GatewayLocation),
		CatalogFresh:             entry.CatalogFresh,
		FormalPoolAllowed:        entry.FormalPoolAllowed,
		NativeAttestationAllowed: entry.NativeAttestationAllowed,
		CatalogVersion:           safeClaudeCodeNativeLabel(r.catalog.CatalogVersion),
		RuntimeHash:              safeClaudeCodeProviderHash(r.catalog.RuntimeHash),
		OverlayHash:              safeClaudeCodeProviderHash(r.catalog.OverlayHash),
		CatalogHash:              safeClaudeCodeProviderHash(r.catalog.CatalogHash),
	}
	if !decision.CatalogFresh {
		return ClaudeCodeProviderRouteDecision{}, fmt.Errorf("claude code provider registry stale catalog")
	}
	if decision.Route == ClaudeCodeNativeRoute {
		if !strings.HasPrefix(decision.ModelID, "claude-") || decision.CatalogVersion == "" || decision.RuntimeHash == "" || decision.OverlayHash == "" || decision.CatalogHash == "" || decision.Provider != "claude" || decision.ClientType != ClaudeCodeNativeClientType || !decision.FormalPoolAllowed || !decision.NativeAttestationAllowed || decision.ProviderOwner != ClaudeCodeNativeProviderOwner || decision.CredentialScope != ClaudeCodeNativeCredentialScope || decision.GatewayLocation != ClaudeCodeNativeGatewayLocation {
			return ClaudeCodeProviderRouteDecision{}, fmt.Errorf("claude code provider registry native binding invalid")
		}
		return decision, nil
	}
	if !strings.HasPrefix(decision.ClientType, "claude_code_bridge_") || decision.FormalPoolAllowed || decision.NativeAttestationAllowed || decision.CredentialScope != ClaudeCodeBridgeCredentialScope || decision.ClientType == ClaudeCodeNativeClientType {
		return ClaudeCodeProviderRouteDecision{}, fmt.Errorf("claude code provider registry bridge binding invalid")
	}
	return decision, nil
}

func safeClaudeCodeProviderHash(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if claudeCodeNativeSafeHashRe.MatchString(value) && value != claudeCodeNativeUnknownHash {
		return value
	}
	return ""
}

func (d ClaudeCodeProviderRouteDecision) NativeCatalogAdmissionDecision() claudeCodeNativeCatalogAdmissionDecision {
	return claudeCodeNativeCatalogAdmissionDecision{
		ModelID:         d.ModelID,
		Route:           d.Route,
		ProviderOwner:   d.ProviderOwner,
		CredentialScope: d.CredentialScope,
		GatewayLocation: d.GatewayLocation,
		RuntimeHash:     d.RuntimeHash,
		OverlayHash:     d.OverlayHash,
		CatalogHash:     d.CatalogHash,
		CatalogVersion:  d.CatalogVersion,
		CatalogFresh:    d.CatalogFresh,
	}
}

func (d ClaudeCodeProviderRouteDecision) BridgeRouteDecision() ClaudeCodeBridgeRouteDecision {
	return ClaudeCodeBridgeRouteDecision{
		ModelID:                  d.ModelID,
		Provider:                 d.Provider,
		Route:                    d.Route,
		ClientType:               d.ClientType,
		RuntimeHash:              d.RuntimeHash,
		OverlayHash:              d.OverlayHash,
		CatalogHash:              d.CatalogHash,
		CatalogVersion:           d.CatalogVersion,
		ProviderOwner:            d.ProviderOwner,
		CredentialScope:          d.CredentialScope,
		GatewayLocation:          d.GatewayLocation,
		FormalPoolAllowed:        d.FormalPoolAllowed,
		NativeAttestationAllowed: d.NativeAttestationAllowed,
	}
}
