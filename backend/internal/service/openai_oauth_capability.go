package service

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

const OpenAIRequiredResponsesWriteScope = "api.responses.write"

type OpenAITokenCapability struct {
	GrantedScope          string
	TokenScopes           []string
	ResponsesWriteCapable bool
	Known                 bool
}

func evaluateOpenAITokenCapability(tokenInfo *OpenAITokenInfo) OpenAITokenCapability {
	if tokenInfo == nil {
		return OpenAITokenCapability{}
	}
	capability := OpenAITokenCapability{
		GrantedScope: strings.TrimSpace(tokenInfo.Scope),
	}
	scopes := normalizeOpenAIScopes(capability.GrantedScope)
	scopes = mergeOpenAIScopes(scopes, tokenInfo.Scopes)
	capability.TokenScopes = scopes
	capability.Known = capability.GrantedScope != "" || len(capability.TokenScopes) > 0
	capability.ResponsesWriteCapable = hasOpenAIScope(capability.TokenScopes, OpenAIRequiredResponsesWriteScope)
	return capability
}

func extractOpenAITokenCapabilityFromCredentials(credentials map[string]any) OpenAITokenCapability {
	if len(credentials) == 0 {
		return OpenAITokenCapability{}
	}
	capability := OpenAITokenCapability{
		GrantedScope: strings.TrimSpace(stringValue(credentials["scope"])),
	}
	scopes := normalizeOpenAIScopes(capability.GrantedScope)
	if rawScopes, ok := credentials["scopes"]; ok {
		scopes = mergeOpenAIScopes(scopes, scopesFromAny(rawScopes))
	}
	capability.TokenScopes = scopes
	capability.Known = capability.GrantedScope != "" || len(capability.TokenScopes) > 0
	capability.ResponsesWriteCapable = hasOpenAIScope(capability.TokenScopes, OpenAIRequiredResponsesWriteScope)
	return capability
}

func buildOpenAITokenCapabilityExtra(capability OpenAITokenCapability) map[string]any {
	return map[string]any{
		"openai_last_granted_scope":       capability.GrantedScope,
		"openai_last_access_token_scopes": append([]string(nil), capability.TokenScopes...),
		"openai_responses_write_capable":  capability.ResponsesWriteCapable,
	}
}

func extractOpenAIScopesFromAccessToken(accessToken string) []string {
	claims, err := openai.DecodeIDToken(strings.TrimSpace(accessToken))
	if err != nil || claims == nil {
		return nil
	}
	return normalizeOpenAIScopeList(claims.SCP)
}

func normalizeOpenAIScopes(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return normalizeOpenAIScopeList(strings.Fields(raw))
}

func scopesFromAny(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		return normalizeOpenAIScopeList(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if scope := strings.TrimSpace(stringValue(item)); scope != "" {
				out = append(out, scope)
			}
		}
		return normalizeOpenAIScopeList(out)
	case string:
		return normalizeOpenAIScopes(typed)
	default:
		return nil
	}
}

func normalizeOpenAIScopeList(scopes []string) []string {
	if len(scopes) == 0 {
		return nil
	}
	out := make([]string, 0, len(scopes))
	seen := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		normalized := strings.TrimSpace(strings.ToLower(scope))
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func mergeOpenAIScopes(existing []string, scopes []string) []string {
	if len(scopes) == 0 {
		return append([]string(nil), existing...)
	}
	return normalizeOpenAIScopeList(append(append([]string(nil), existing...), scopes...))
}

func hasOpenAIScope(scopes []string, required string) bool {
	required = strings.TrimSpace(strings.ToLower(required))
	if required == "" {
		return true
	}
	for _, scope := range scopes {
		if strings.TrimSpace(strings.ToLower(scope)) == required {
			return true
		}
	}
	return false
}
