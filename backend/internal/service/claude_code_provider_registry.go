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
	ModelID                  string   `json:"model_id"`
	Provider                 string   `json:"provider"`
	Route                    string   `json:"route"`
	ClientType               string   `json:"client_type"`
	ProviderOwner            string   `json:"provider_owner"`
	CredentialScope          string   `json:"credential_scope"`
	GatewayLocation          string   `json:"gateway_location"`
	CatalogFresh             bool     `json:"catalog_fresh"`
	FormalPoolAllowed        bool     `json:"formal_pool_allowed"`
	NativeAttestationAllowed bool     `json:"native_attestation_allowed"`
	PreferredProtocol        string   `json:"preferred_protocol,omitempty"`
	AnthropicBaseURL         string   `json:"anthropic_base_url,omitempty"`
	OpenAIBaseURL            string   `json:"openai_base_url,omitempty"`
	FallbackProtocol         string   `json:"fallback_protocol,omitempty"`
	FallbackReason           string   `json:"fallback_reason,omitempty"`
	CapabilitiesVerified     bool     `json:"capabilities_verified,omitempty"`
	SupportsText             bool     `json:"supports_text,omitempty"`
	SupportsTools            bool     `json:"supports_tools,omitempty"`
	SupportsStreaming        bool     `json:"supports_streaming,omitempty"`
	SupportsUsage            bool     `json:"supports_usage,omitempty"`
	SupportsCacheAudit       bool     `json:"supports_cache_audit,omitempty"`
	SupportsReasoningMapping bool     `json:"supports_reasoning_mapping,omitempty"`
	SupportsErrorPassthrough bool     `json:"supports_error_passthrough,omitempty"`
	ReasoningEffortLevels    []string `json:"reasoning_effort_levels,omitempty"`
	CachePolicy              string   `json:"cache_policy,omitempty"`
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
	PreferredProtocol        string
	AnthropicBaseURL         string
	OpenAIBaseURL            string
	FallbackProtocol         string
	FallbackReason           string
	CapabilitiesVerified     bool
	SupportsText             bool
	SupportsTools            bool
	SupportsStreaming        bool
	SupportsUsage            bool
	SupportsCacheAudit       bool
	SupportsReasoningMapping bool
	SupportsErrorPassthrough bool
	ReasoningEffortLevels    []string
	CachePolicy              string
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
		PreferredProtocol:        strings.TrimSpace(entry.PreferredProtocol),
		AnthropicBaseURL:         strings.TrimSpace(entry.AnthropicBaseURL),
		OpenAIBaseURL:            strings.TrimSpace(entry.OpenAIBaseURL),
		FallbackProtocol:         strings.TrimSpace(entry.FallbackProtocol),
		FallbackReason:           safeClaudeCodeNativeLabel(entry.FallbackReason),
		CapabilitiesVerified:     entry.CapabilitiesVerified,
		SupportsText:             entry.SupportsText,
		SupportsTools:            entry.SupportsTools,
		SupportsStreaming:        entry.SupportsStreaming,
		SupportsUsage:            entry.SupportsUsage,
		SupportsCacheAudit:       entry.SupportsCacheAudit,
		SupportsReasoningMapping: entry.SupportsReasoningMapping,
		SupportsErrorPassthrough: entry.SupportsErrorPassthrough,
		ReasoningEffortLevels:    append([]string(nil), entry.ReasoningEffortLevels...),
		CachePolicy:              strings.TrimSpace(entry.CachePolicy),
	}
	if !decision.CatalogFresh {
		return ClaudeCodeProviderRouteDecision{}, fmt.Errorf("claude code provider registry stale catalog")
	}
	if decision.Route == ClaudeCodeNativeRoute {
		if !strings.HasPrefix(decision.ModelID, "claude-") || decision.CatalogVersion == "" || decision.RuntimeHash == "" || decision.OverlayHash == "" || decision.CatalogHash == "" || decision.Provider != "claude" || decision.ClientType != ClaudeCodeNativeClientType || !decision.FormalPoolAllowed || !decision.NativeAttestationAllowed || decision.ProviderOwner != ClaudeCodeNativeProviderOwner || decision.CredentialScope != ClaudeCodeNativeCredentialScope || decision.GatewayLocation != ClaudeCodeNativeGatewayLocation || decision.hasBridgeOnlyMetadata() {
			return ClaudeCodeProviderRouteDecision{}, fmt.Errorf("claude code provider registry native binding invalid")
		}
		return decision, nil
	}
	if !strings.HasPrefix(decision.ClientType, "claude_code_bridge_") || decision.FormalPoolAllowed || decision.NativeAttestationAllowed || decision.CredentialScope != ClaudeCodeBridgeCredentialScope || decision.ClientType == ClaudeCodeNativeClientType {
		return ClaudeCodeProviderRouteDecision{}, fmt.Errorf("claude code provider registry bridge binding invalid")
	}
	if !decision.bridgeCapabilitiesAreVerified() {
		return ClaudeCodeProviderRouteDecision{}, fmt.Errorf("claude code provider registry bridge capability contract invalid")
	}
	return decision, nil
}

func (d ClaudeCodeProviderRouteDecision) hasBridgeOnlyMetadata() bool {
	return d.PreferredProtocol != "" || d.AnthropicBaseURL != "" || d.OpenAIBaseURL != "" || d.FallbackProtocol != "" || d.FallbackReason != "" || d.CapabilitiesVerified || d.SupportsText || d.SupportsTools || d.SupportsStreaming || d.SupportsUsage || d.SupportsCacheAudit || d.SupportsReasoningMapping || d.SupportsErrorPassthrough || len(d.ReasoningEffortLevels) > 0 || d.CachePolicy != ""
}

func (d ClaudeCodeProviderRouteDecision) bridgeCapabilitiesAreVerified() bool {
	if !d.CapabilitiesVerified || !d.SupportsText || !d.SupportsTools || !d.SupportsStreaming || !d.SupportsUsage || !d.SupportsErrorPassthrough {
		return false
	}
	switch d.PreferredProtocol {
	case "anthropic_messages":
		if d.AnthropicBaseURL == "" {
			return false
		}
	case "responses", "openai_chat_completions", "openai_compatible_chat":
		if d.OpenAIBaseURL == "" {
			return false
		}
	default:
		return false
	}
	if d.FallbackProtocol != "" && d.FallbackReason == "" {
		return false
	}
	if d.CachePolicy != "" && !d.SupportsCacheAudit {
		return false
	}
	if len(d.ReasoningEffortLevels) > 0 && !d.SupportsReasoningMapping {
		return false
	}
	return true
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
		PreferredProtocol:        d.PreferredProtocol,
		AnthropicBaseURL:         d.AnthropicBaseURL,
		OpenAIBaseURL:            d.OpenAIBaseURL,
		FallbackProtocol:         d.FallbackProtocol,
		FallbackReason:           d.FallbackReason,
		CapabilitiesVerified:     d.CapabilitiesVerified,
		SupportsText:             d.SupportsText,
		SupportsTools:            d.SupportsTools,
		SupportsStreaming:        d.SupportsStreaming,
		SupportsUsage:            d.SupportsUsage,
		SupportsCacheAudit:       d.SupportsCacheAudit,
		SupportsReasoningMapping: d.SupportsReasoningMapping,
		SupportsErrorPassthrough: d.SupportsErrorPassthrough,
		ReasoningEffortLevels:    append([]string(nil), d.ReasoningEffortLevels...),
		CachePolicy:              d.CachePolicy,
	}
}
