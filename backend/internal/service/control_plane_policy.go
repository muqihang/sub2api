package service

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	ControlPlaneStaleNoStale  = "no_stale"
	ControlPlaneStaleSafe     = "stale_safe"
	ControlPlaneActionForward = "forward_dry_run"
	ControlPlaneActionStub    = "stub_json"
	ControlPlaneActionBlock   = "quarantine_block"
)

type ControlPlaneQueryRule struct {
	Kind          string
	EnumValues    []string
	Min           int
	Max           int
	AllowRepeated bool
}

type ControlPlanePathPolicy struct {
	Method                   string
	PathTemplate             string
	Classification           string
	Action                   string
	QueryAllowlist           map[string]ControlPlaneQueryRule
	TTL                      time.Duration
	StaleMode                string
	Cacheable                bool
	RequiresUserPartition    bool
	RequiresSessionPartition bool
	Sensitive                bool
	ResponseSchemaVersion    int
	AllowedResponseKeys      map[string]struct{}
	PrivateFieldDenylist     map[string]struct{}
}

type ControlPlanePathPolicyMatrix struct {
	policies map[string]ControlPlanePathPolicy
}

type ControlPlanePolicyDecision struct {
	Allowed         bool
	Decision        string
	Reason          string
	Status          int
	Policy          *ControlPlanePathPolicy
	NormalizedQuery map[string]string
}

func NewDefaultControlPlanePathPolicyMatrix() *ControlPlanePathPolicyMatrix {
	policies := []ControlPlanePathPolicy{
		{
			Method: "GET", PathTemplate: "/api/claude_cli/bootstrap", Classification: "bootstrap_settings_or_feature_flag_stubbed",
			Action: ControlPlaneActionStub, Cacheable: true, TTL: 5 * time.Minute, StaleMode: ControlPlaneStaleSafe,
			RequiresUserPartition: true, RequiresSessionPartition: true, ResponseSchemaVersion: 1,
			QueryAllowlist: map[string]ControlPlaneQueryRule{
				"entrypoint": {Kind: "enum", EnumValues: []string{"sdk-cli"}},
				"feature":    {Kind: "enum", EnumValues: []string{"mcp", "oauth"}, AllowRepeated: true},
			},
			AllowedResponseKeys:  stringSet("data", "features", "ok"),
			PrivateFieldDenylist: defaultControlPlanePrivateFieldDenylist(),
		},
		{
			Method: "GET", PathTemplate: "/v1/mcp_servers", Classification: "mcp_servers_stubbed",
			Action: ControlPlaneActionStub, Cacheable: true, TTL: time.Minute, StaleMode: ControlPlaneStaleSafe,
			RequiresUserPartition: true, RequiresSessionPartition: true, ResponseSchemaVersion: 1,
			QueryAllowlist:       map[string]ControlPlaneQueryRule{"limit": {Kind: "int", Min: 1, Max: 1000}},
			AllowedResponseKeys:  stringSet("data", "servers"),
			PrivateFieldDenylist: defaultControlPlanePrivateFieldDenylist(),
		},
		{
			Method: "GET", PathTemplate: "/api/oauth/account/settings", Classification: "account_settings_sensitive",
			Action: ControlPlaneActionBlock, Cacheable: false, TTL: 0, StaleMode: ControlPlaneStaleNoStale,
			RequiresUserPartition: true, RequiresSessionPartition: true, Sensitive: true, ResponseSchemaVersion: 1,
			QueryAllowlist:       map[string]ControlPlaneQueryRule{},
			AllowedResponseKeys:  stringSet("settings", "ok"),
			PrivateFieldDenylist: defaultControlPlanePrivateFieldDenylist(),
		},
		{
			Method: "GET", PathTemplate: "/api/hello", Classification: "bootstrap_settings_or_feature_flag_stubbed",
			Action: ControlPlaneActionStub, Cacheable: true, TTL: 5 * time.Minute, StaleMode: ControlPlaneStaleSafe,
			RequiresUserPartition: true, RequiresSessionPartition: true, ResponseSchemaVersion: 1,
			QueryAllowlist:       map[string]ControlPlaneQueryRule{},
			AllowedResponseKeys:  stringSet("ok"),
			PrivateFieldDenylist: defaultControlPlanePrivateFieldDenylist(),
		},
		{
			Method: "GET", PathTemplate: "/v1/oauth/hello", Classification: "bootstrap_settings_or_feature_flag_stubbed",
			Action: ControlPlaneActionStub, Cacheable: true, TTL: 5 * time.Minute, StaleMode: ControlPlaneStaleSafe,
			RequiresUserPartition: true, RequiresSessionPartition: true, ResponseSchemaVersion: 1,
			QueryAllowlist:       map[string]ControlPlaneQueryRule{},
			AllowedResponseKeys:  stringSet("ok"),
			PrivateFieldDenylist: defaultControlPlanePrivateFieldDenylist(),
		},
		{
			Method: "GET", PathTemplate: "/v1/models", Classification: "model_capabilities_stubbed",
			Action: ControlPlaneActionStub, Cacheable: true, TTL: 5 * time.Minute, StaleMode: ControlPlaneStaleSafe,
			RequiresUserPartition: true, RequiresSessionPartition: true, ResponseSchemaVersion: 1,
			QueryAllowlist:       map[string]ControlPlaneQueryRule{"limit": {Kind: "int", Min: 1, Max: 1000}},
			AllowedResponseKeys:  stringSet("data", "models"),
			PrivateFieldDenylist: defaultControlPlanePrivateFieldDenylist(),
		},
		{
			Method: "GET", PathTemplate: "/mcp-registry/v0/servers", Classification: "mcp_registry_public_stubbed",
			Action: ControlPlaneActionStub, Cacheable: true, TTL: time.Hour, StaleMode: ControlPlaneStaleSafe,
			RequiresUserPartition: true, RequiresSessionPartition: false, ResponseSchemaVersion: 1,
			QueryAllowlist:       map[string]ControlPlaneQueryRule{"version": {Kind: "enum", EnumValues: []string{"latest"}}},
			AllowedResponseKeys:  stringSet("data", "servers"),
			PrivateFieldDenylist: defaultControlPlanePrivateFieldDenylist(),
		},
		{
			Method: "GET", PathTemplate: "/api/claude_code_penguin_mode", Classification: "claude_code_feature_flags_stubbed",
			Action: ControlPlaneActionStub, Cacheable: true, TTL: time.Minute, StaleMode: ControlPlaneStaleSafe,
			RequiresUserPartition: true, RequiresSessionPartition: true, ResponseSchemaVersion: 1,
			QueryAllowlist:       map[string]ControlPlaneQueryRule{},
			AllowedResponseKeys:  stringSet("features", "ok"),
			PrivateFieldDenylist: defaultControlPlanePrivateFieldDenylist(),
		},
		{
			Method: "GET", PathTemplate: "/api/claude_code_feature_flags", Classification: "claude_code_feature_flags_stubbed",
			Action: ControlPlaneActionStub, Cacheable: true, TTL: time.Minute, StaleMode: ControlPlaneStaleSafe,
			RequiresUserPartition: true, RequiresSessionPartition: true, ResponseSchemaVersion: 1,
			QueryAllowlist:       map[string]ControlPlaneQueryRule{},
			AllowedResponseKeys:  stringSet("features", "ok"),
			PrivateFieldDenylist: defaultControlPlanePrivateFieldDenylist(),
		},
		{
			Method: "GET", PathTemplate: "/api/claude_code_grove", Classification: "claude_code_feature_flags_stubbed",
			Action: ControlPlaneActionStub, Cacheable: true, TTL: time.Minute, StaleMode: ControlPlaneStaleSafe,
			RequiresUserPartition: true, RequiresSessionPartition: true, ResponseSchemaVersion: 1,
			QueryAllowlist:       map[string]ControlPlaneQueryRule{},
			AllowedResponseKeys:  stringSet("features", "ok"),
			PrivateFieldDenylist: defaultControlPlanePrivateFieldDenylist(),
		},
		{
			Method: "GET", PathTemplate: "/api/claude_code/organizations/metrics_enabled", Classification: "claude_code_feature_flags_stubbed",
			Action: ControlPlaneActionStub, Cacheable: true, TTL: time.Minute, StaleMode: ControlPlaneStaleSafe,
			RequiresUserPartition: true, RequiresSessionPartition: true, ResponseSchemaVersion: 1,
			QueryAllowlist:       map[string]ControlPlaneQueryRule{},
			AllowedResponseKeys:  stringSet("features", "ok"),
			PrivateFieldDenylist: defaultControlPlanePrivateFieldDenylist(),
		},
		{
			Method: "GET", PathTemplate: "/api/claude_code/policy_limits", Classification: "policy_limits_sensitive",
			Action: ControlPlaneActionBlock, Cacheable: false, TTL: 0, StaleMode: ControlPlaneStaleNoStale,
			RequiresUserPartition: true, RequiresSessionPartition: true, Sensitive: true, ResponseSchemaVersion: 1,
			QueryAllowlist:       map[string]ControlPlaneQueryRule{},
			AllowedResponseKeys:  stringSet("ok"),
			PrivateFieldDenylist: defaultControlPlanePrivateFieldDenylist(),
		},
		{
			Method: "GET", PathTemplate: "/api/claude_code/remote_managed_settings", Classification: "remote_managed_settings_sensitive",
			Action: ControlPlaneActionBlock, Cacheable: false, TTL: 0, StaleMode: ControlPlaneStaleNoStale,
			RequiresUserPartition: true, RequiresSessionPartition: true, Sensitive: true, ResponseSchemaVersion: 1,
			QueryAllowlist:       map[string]ControlPlaneQueryRule{},
			AllowedResponseKeys:  stringSet("ok"),
			PrivateFieldDenylist: defaultControlPlanePrivateFieldDenylist(),
		},
		{
			Method: "GET", PathTemplate: "/api/claude_code/settings_sync", Classification: "settings_sync_sensitive",
			Action: ControlPlaneActionBlock, Cacheable: false, TTL: 0, StaleMode: ControlPlaneStaleNoStale,
			RequiresUserPartition: true, RequiresSessionPartition: true, Sensitive: true, ResponseSchemaVersion: 1,
			QueryAllowlist:       map[string]ControlPlaneQueryRule{},
			AllowedResponseKeys:  stringSet("ok"),
			PrivateFieldDenylist: defaultControlPlanePrivateFieldDenylist(),
		},
		{
			Method: "GET", PathTemplate: "/api/claude_code/team_memory", Classification: "team_memory_sensitive",
			Action: ControlPlaneActionBlock, Cacheable: false, TTL: 0, StaleMode: ControlPlaneStaleNoStale,
			RequiresUserPartition: true, RequiresSessionPartition: true, Sensitive: true, ResponseSchemaVersion: 1,
			QueryAllowlist:       map[string]ControlPlaneQueryRule{},
			AllowedResponseKeys:  stringSet("ok"),
			PrivateFieldDenylist: defaultControlPlanePrivateFieldDenylist(),
		},
		{
			Method: "GET", PathTemplate: "/api/claude_code/model_capabilities", Classification: "model_capabilities_sensitive",
			Action: ControlPlaneActionBlock, Cacheable: false, TTL: 0, StaleMode: ControlPlaneStaleNoStale,
			RequiresUserPartition: true, RequiresSessionPartition: true, Sensitive: true, ResponseSchemaVersion: 1,
			QueryAllowlist:       map[string]ControlPlaneQueryRule{},
			AllowedResponseKeys:  stringSet("ok"),
			PrivateFieldDenylist: defaultControlPlanePrivateFieldDenylist(),
		},
		{
			Method: "GET", PathTemplate: "/api/claude_code/growthbook", Classification: "growthbook_sensitive",
			Action: ControlPlaneActionBlock, Cacheable: false, TTL: 0, StaleMode: ControlPlaneStaleNoStale,
			RequiresUserPartition: true, RequiresSessionPartition: true, Sensitive: true, ResponseSchemaVersion: 1,
			QueryAllowlist:       map[string]ControlPlaneQueryRule{},
			AllowedResponseKeys:  stringSet("ok"),
			PrivateFieldDenylist: defaultControlPlanePrivateFieldDenylist(),
		},
		{
			Method: "GET", PathTemplate: "/api/oauth/organizations/{org}/referral/eligibility", Classification: "oauth_org_settings_sensitive",
			Action: ControlPlaneActionBlock, Cacheable: false, TTL: 0, StaleMode: ControlPlaneStaleNoStale,
			RequiresUserPartition: true, RequiresSessionPartition: true, Sensitive: true, ResponseSchemaVersion: 1,
			QueryAllowlist:       map[string]ControlPlaneQueryRule{},
			AllowedResponseKeys:  stringSet("ok"),
			PrivateFieldDenylist: defaultControlPlanePrivateFieldDenylist(),
		},
	}
	matrix := &ControlPlanePathPolicyMatrix{policies: map[string]ControlPlanePathPolicy{}}
	for _, policy := range policies {
		matrix.policies[controlPlanePolicyKey(policy.Method, policy.PathTemplate)] = policy
	}
	return matrix
}

func (m *ControlPlanePathPolicyMatrix) Evaluate(method, pathTemplate, rawQuery string) ControlPlanePolicyDecision {
	if m == nil {
		m = NewDefaultControlPlanePathPolicyMatrix()
	}
	policy, ok := m.policies[controlPlanePolicyKey(method, pathTemplate)]
	if !ok {
		return ControlPlanePolicyDecision{Allowed: false, Decision: ControlPlaneActionBlock, Reason: "control_plane:path_not_allowlisted", Status: 403}
	}
	normalized, err := CanonicalizeControlPlaneQuery(policy.PathTemplate, rawQuery, policy.QueryAllowlist)
	if err != nil {
		return ControlPlanePolicyDecision{Allowed: false, Decision: ControlPlaneActionBlock, Reason: "control_plane:query_quarantine:" + err.Error(), Status: 403, Policy: &policy}
	}
	if policy.Action == ControlPlaneActionBlock {
		return ControlPlanePolicyDecision{Allowed: false, Decision: ControlPlaneActionBlock, Reason: "control_plane:path_sensitive_no_upstream", Status: 403, Policy: &policy, NormalizedQuery: normalized}
	}
	return ControlPlanePolicyDecision{Allowed: true, Decision: policy.Action, Reason: "control_plane:path_policy_allow", Status: 200, Policy: &policy, NormalizedQuery: normalized}
}

func CanonicalizeControlPlaneQuery(pathTemplate, rawQuery string, allowlist map[string]ControlPlaneQueryRule) (map[string]string, error) {
	if rawQuery == "" {
		return map[string]string{}, nil
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return nil, fmt.Errorf("malformed_query")
	}
	normalized := map[string]string{}
	for rawKey, rawValues := range values {
		key := strings.ToLower(strings.TrimSpace(rawKey))
		if key == "" || strings.ContainsAny(key, "[]{}.") {
			return nil, fmt.Errorf("nested_or_empty_query_key")
		}
		rule, ok := allowlist[key]
		if !ok {
			return nil, fmt.Errorf("unknown_query_key")
		}
		if len(rawValues) > 1 && !rule.AllowRepeated {
			return nil, fmt.Errorf("repeated_query_key")
		}
		items := make([]string, 0, len(rawValues))
		for _, item := range rawValues {
			trimmed := strings.TrimSpace(item)
			if trimmed == "" {
				return nil, fmt.Errorf("empty_query_value")
			}
			if err := validateControlPlaneQueryRuleValue(trimmed, rule); err != nil {
				return nil, err
			}
			items = append(items, trimmed)
		}
		sort.Strings(items)
		normalized[key] = strings.Join(items, ",")
	}
	return normalized, nil
}

func validateControlPlaneQueryRuleValue(value string, rule ControlPlaneQueryRule) error {
	if looksSensitiveText(value) || looksPlainDigest(value) || looksUnsafeDynamicIdentifier(value) {
		return fmt.Errorf("sensitive_query_value")
	}
	switch rule.Kind {
	case "enum":
		for _, allowed := range rule.EnumValues {
			if value == allowed {
				return nil
			}
		}
		return fmt.Errorf("enum_query_value")
	case "int":
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < rule.Min || parsed > rule.Max {
			return fmt.Errorf("int_query_value")
		}
		return nil
	default:
		return fmt.Errorf("invalid_query_rule")
	}
}

func controlPlanePolicyKey(method, pathTemplate string) string {
	return strings.ToUpper(strings.TrimSpace(method)) + " " + strings.TrimSpace(pathTemplate)
}

func stringSet(values ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

func defaultControlPlanePrivateFieldDenylist() map[string]struct{} {
	return stringSet("authorization", "cookie", "email", "account_uuid", "org_uuid", "user_uuid", "session_id", "access_token", "refresh_token", "proxy_credential", "cch")
}
