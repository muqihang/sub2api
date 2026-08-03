package service

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

type ControlPlaneKillSwitchKey struct {
	PathTemplate   string
	AccountScope   string
	PersonaProfile string
}

type ControlPlaneQuarantineManager struct {
	mu           sync.RWMutex
	pathKills    map[string]string
	accountKills map[string]string
	profileKills map[string]string
}

type ControlPlaneQuarantineDecision struct {
	Allowed bool
	Reason  string
	Status  int
}

func NewControlPlaneQuarantineManager() *ControlPlaneQuarantineManager {
	return &ControlPlaneQuarantineManager{pathKills: map[string]string{}, accountKills: map[string]string{}, profileKills: map[string]string{}}
}

func (m *ControlPlaneQuarantineManager) KillPath(pathTemplate, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pathKills[strings.TrimSpace(pathTemplate)] = safeQuarantineReason(reason)
}

func (m *ControlPlaneQuarantineManager) KillAccount(accountScope, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.accountKills[strings.TrimSpace(accountScope)] = safeQuarantineReason(reason)
}

func (m *ControlPlaneQuarantineManager) KillProfile(personaProfile, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.profileKills[strings.TrimSpace(personaProfile)] = safeQuarantineReason(reason)
}

func (m *ControlPlaneQuarantineManager) Check(input ControlPlaneCacheKeyInput) ControlPlaneQuarantineDecision {
	if m == nil {
		return ControlPlaneQuarantineDecision{Allowed: true, Reason: "no_kill_switch", Status: 200}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if reason := m.pathKills[input.PathTemplate]; reason != "" {
		return ControlPlaneQuarantineDecision{Allowed: false, Reason: "path_kill_switch:" + reason, Status: 403}
	}
	if reason := m.accountKills[input.AccountScope]; reason != "" {
		return ControlPlaneQuarantineDecision{Allowed: false, Reason: "account_kill_switch:" + reason, Status: 403}
	}
	if reason := m.profileKills[input.PersonaProfile]; reason != "" {
		return ControlPlaneQuarantineDecision{Allowed: false, Reason: "profile_kill_switch:" + reason, Status: 403}
	}
	return ControlPlaneQuarantineDecision{Allowed: true, Reason: "allowed", Status: 200}
}

func (m *ControlPlaneQuarantineManager) ObserveResponse(input ControlPlaneCacheKeyInput, status int, risk bool, schemaDrift bool) ControlPlaneQuarantineDecision {
	if status == 401 || status == 403 || status == 429 {
		m.KillAccount(input.AccountScope, fmt.Sprintf("status_%d", status))
		return ControlPlaneQuarantineDecision{Allowed: false, Reason: fmt.Sprintf("account_kill_switch:status_%d", status), Status: 403}
	}
	if risk {
		m.KillAccount(input.AccountScope, "risk_signal")
		return ControlPlaneQuarantineDecision{Allowed: false, Reason: "account_kill_switch:risk_signal", Status: 403}
	}
	if schemaDrift {
		m.KillPath(input.PathTemplate, "schema_drift")
		return ControlPlaneQuarantineDecision{Allowed: false, Reason: "path_kill_switch:schema_drift", Status: 403}
	}
	return ControlPlaneQuarantineDecision{Allowed: true, Reason: "observed_safe", Status: 200}
}

func ValidateControlPlaneResponseSchema(policy ControlPlanePathPolicy, body map[string]any) error {
	if len(policy.AllowedResponseKeys) == 0 {
		return fmt.Errorf("control-plane response schema allowlist is empty")
	}
	for key, value := range body {
		if _, ok := policy.AllowedResponseKeys[key]; !ok {
			return fmt.Errorf("control-plane response schema drift")
		}
		if err := ScanControlPlanePrivateFields(policy.PrivateFieldDenylist, key, value); err != nil {
			return err
		}
	}
	return nil
}

func ScanControlPlanePrivateFields(denylist map[string]struct{}, path string, value any) error {
	lowerPath := strings.ToLower(path)
	last := lowerPath
	if idx := strings.LastIndexAny(lowerPath, ".[]"); idx >= 0 && idx+1 < len(lowerPath) {
		last = lowerPath[idx+1:]
	}
	if _, denied := denylist[last]; denied {
		return fmt.Errorf("control-plane private field detected")
	}
	switch typed := value.(type) {
	case string:
		if looksSensitiveText(typed) || controlPlaneUUIDRe.MatchString(typed) || privateFieldValueRe.MatchString(strings.ToLower(typed)) {
			return fmt.Errorf("control-plane private field value detected")
		}
	case map[string]any:
		for key, child := range typed {
			if err := ScanControlPlanePrivateFields(denylist, path+"."+key, child); err != nil {
				return err
			}
		}
	case []any:
		for idx, child := range typed {
			if err := ScanControlPlanePrivateFields(denylist, fmt.Sprintf("%s[%d]", path, idx), child); err != nil {
				return err
			}
		}
	}
	return nil
}

func safeQuarantineReason(reason string) string {
	reason = strings.ToLower(strings.TrimSpace(reason))
	if reason == "" || looksSensitiveText(reason) || looksPlainDigest(reason) || looksUnsafeDynamicIdentifier(reason) {
		return "redacted"
	}
	return reason
}

var privateFieldValueRe = regexp.MustCompile(`(?i)(bearer\s+|x-api-key|access[_-]?token|refresh[_-]?token|cookie=|cch=)`)
