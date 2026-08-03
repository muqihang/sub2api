package service

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

const ControlPlanePathMatrixJSONEnv = "SUB2API_CONTROL_PLANE_PATH_MATRIX_JSON"

type ControlPlanePathPolicyMatrixConfig struct {
	Policies []ControlPlanePathPolicyConfig `json:"policies"`
}

type ControlPlanePathPolicyConfig struct {
	Method               string                                 `json:"method"`
	PathTemplate         string                                 `json:"path_template"`
	Classification       string                                 `json:"classification"`
	Action               string                                 `json:"action"`
	CacheScope           string                                 `json:"cache_scope"`
	TTLSeconds           int                                    `json:"ttl_seconds"`
	QuarantineOnMismatch bool                                   `json:"quarantine_on_mismatch"`
	RawForbidden         bool                                   `json:"raw_forbidden"`
	QueryAllowlist       map[string]ControlPlaneQueryRuleConfig `json:"query_allowlist"`
	AllowedResponseKeys  []string                               `json:"allowed_response_keys"`
}

type ControlPlaneQueryRuleConfig struct {
	Kind          string   `json:"kind"`
	EnumValues    []string `json:"enum_values"`
	Min           int      `json:"min"`
	Max           int      `json:"max"`
	AllowRepeated bool     `json:"allow_repeated"`
}

func NewControlPlanePathPolicyMatrixFromConfig(cfg ControlPlanePathPolicyMatrixConfig) (*ControlPlanePathPolicyMatrix, error) {
	matrix := NewDefaultControlPlanePathPolicyMatrix()
	if matrix.policies == nil {
		matrix.policies = map[string]ControlPlanePathPolicy{}
	}
	for _, item := range cfg.Policies {
		policy, err := controlPlanePathPolicyFromConfig(item)
		if err != nil {
			return nil, err
		}
		matrix.policies[controlPlanePolicyKey(policy.Method, policy.PathTemplate)] = policy
	}
	return matrix, nil
}

func NewControlPlanePathPolicyMatrixFromEnv() (*ControlPlanePathPolicyMatrix, error) {
	raw := strings.TrimSpace(os.Getenv(ControlPlanePathMatrixJSONEnv))
	if raw == "" {
		return NewDefaultControlPlanePathPolicyMatrix(), nil
	}
	var cfg ControlPlanePathPolicyMatrixConfig
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("control-plane path matrix config is invalid")
	}
	return NewControlPlanePathPolicyMatrixFromConfig(cfg)
}

func controlPlanePathPolicyFromConfig(cfg ControlPlanePathPolicyConfig) (ControlPlanePathPolicy, error) {
	method := strings.ToUpper(strings.TrimSpace(cfg.Method))
	path := strings.TrimSpace(cfg.PathTemplate)
	classification := strings.TrimSpace(cfg.Classification)
	if method != "GET" {
		return ControlPlanePathPolicy{}, fmt.Errorf("control-plane path matrix only allows safe GET overlays")
	}
	if path == "" || strings.ContainsAny(path, "*{}") || !strings.HasPrefix(path, "/") {
		return ControlPlanePathPolicy{}, fmt.Errorf("control-plane path matrix path must be explicit")
	}
	if err := validateSafeIdentifier(classification, "classification"); err != nil {
		return ControlPlanePathPolicy{}, err
	}
	if cfg.Action != ControlPlaneActionStub && cfg.Action != ControlPlaneActionBlock {
		return ControlPlanePathPolicy{}, fmt.Errorf("control-plane path matrix action must be stub or block")
	}
	if !cfg.RawForbidden {
		return ControlPlanePathPolicy{}, fmt.Errorf("control-plane path matrix raw_forbidden must be true")
	}
	if !cfg.QuarantineOnMismatch {
		return ControlPlanePathPolicy{}, fmt.Errorf("control-plane path matrix quarantine_on_mismatch must be true")
	}
	cacheable, ttl, staleMode, requireUser, requireSession, err := controlPlaneCacheConfig(cfg.CacheScope, cfg.TTLSeconds, cfg.Action)
	if err != nil {
		return ControlPlanePathPolicy{}, err
	}
	allowedResponseKeys, err := controlPlaneAllowedResponseKeySet(cfg.AllowedResponseKeys)
	if err != nil {
		return ControlPlanePathPolicy{}, err
	}
	queryAllowlist, err := controlPlaneQueryRuleConfigMap(cfg.QueryAllowlist)
	if err != nil {
		return ControlPlanePathPolicy{}, err
	}
	return ControlPlanePathPolicy{
		Method:                   method,
		PathTemplate:             path,
		Classification:           classification,
		Action:                   cfg.Action,
		QueryAllowlist:           queryAllowlist,
		TTL:                      ttl,
		StaleMode:                staleMode,
		Cacheable:                cacheable,
		RequiresUserPartition:    requireUser,
		RequiresSessionPartition: requireSession,
		Sensitive:                cfg.Action == ControlPlaneActionBlock,
		ResponseSchemaVersion:    1,
		AllowedResponseKeys:      allowedResponseKeys,
		PrivateFieldDenylist:     defaultControlPlanePrivateFieldDenylist(),
	}, nil
}

func controlPlaneCacheConfig(scope string, ttlSeconds int, action string) (bool, time.Duration, string, bool, bool, error) {
	scope = strings.TrimSpace(strings.ToLower(scope))
	if action == ControlPlaneActionBlock {
		return false, 0, ControlPlaneStaleNoStale, true, true, nil
	}
	if ttlSeconds <= 0 || ttlSeconds > 3600 {
		return false, 0, "", false, false, fmt.Errorf("control-plane path matrix ttl must be 1..3600 seconds")
	}
	switch scope {
	case "session":
		return true, time.Duration(ttlSeconds) * time.Second, ControlPlaneStaleSafe, true, true, nil
	case "user":
		return true, time.Duration(ttlSeconds) * time.Second, ControlPlaneStaleSafe, true, false, nil
	default:
		return false, 0, "", false, false, fmt.Errorf("control-plane path matrix cache_scope must be session or user")
	}
}

func controlPlaneAllowedResponseKeySet(keys []string) (map[string]struct{}, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("control-plane path matrix response schema allowlist is required")
	}
	out := map[string]struct{}{}
	denylist := defaultControlPlanePrivateFieldDenylist()
	for _, raw := range keys {
		key := strings.TrimSpace(raw)
		if _, denied := denylist[strings.ToLower(key)]; denied || key == "" || looksSensitiveText(key) || looksUnsafeDynamicIdentifier(key) || strings.ContainsAny(key, "[]{}.") {
			return nil, fmt.Errorf("control-plane path matrix response key is unsafe")
		}
		out[key] = struct{}{}
	}
	return out, nil
}

func controlPlaneQueryRuleConfigMap(in map[string]ControlPlaneQueryRuleConfig) (map[string]ControlPlaneQueryRule, error) {
	out := map[string]ControlPlaneQueryRule{}
	for rawKey, rawRule := range in {
		key := strings.ToLower(strings.TrimSpace(rawKey))
		if key == "" || strings.ContainsAny(key, "[]{}.") || looksSensitiveText(key) || looksUnsafeDynamicIdentifier(key) {
			return nil, fmt.Errorf("control-plane path matrix query key is unsafe")
		}
		rule := ControlPlaneQueryRule{Kind: rawRule.Kind, EnumValues: append([]string(nil), rawRule.EnumValues...), Min: rawRule.Min, Max: rawRule.Max, AllowRepeated: rawRule.AllowRepeated}
		if rule.Kind != "enum" && rule.Kind != "int" {
			return nil, fmt.Errorf("control-plane path matrix query rule kind is invalid")
		}
		if rule.Kind == "enum" {
			if len(rule.EnumValues) == 0 {
				return nil, fmt.Errorf("control-plane path matrix enum allowlist is required")
			}
			for _, value := range rule.EnumValues {
				trimmed := strings.TrimSpace(value)
				if trimmed == "" || looksSensitiveText(trimmed) || looksPlainDigest(trimmed) || looksUnsafeDynamicIdentifier(trimmed) {
					return nil, fmt.Errorf("control-plane path matrix enum value is unsafe")
				}
			}
		}
		out[key] = rule
	}
	return out, nil
}
