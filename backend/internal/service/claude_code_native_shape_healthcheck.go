package service

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/tidwall/gjson"
)

const (
	ClaudeCodeNativeShapeHealthcheckPass = "pass"
	ClaudeCodeNativeShapeHealthcheckFail = "fail"
)

var ClaudeCodeNativeShapeHealthcheckFields = []string{
	"localhost_only",
	"mock_upstream_only",
	"messages_fixture",
	"tool_search_fixture",
	"thinking_fixture",
	"tools_fixture",
	"count_tokens_fixture",
	"stream_fixture",
	"opus_fixture",
	"sonnet_fixture",
	"control_plane_safe_intent_fixture",
	"netwatch_fixture",
	"native_attestation_profile",
	"body_omitted",
}

func sortedClaudeCodeNativeStringSet(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

type ClaudeCodeNativeShapeFixture struct {
	Name  string
	Route string
	Body  []byte
	Audit ClaudeCodeNativeAuditSummary
}

type ClaudeCodeNativeShapeHealthcheckEvidence struct {
	LocalhostOnly             bool
	MockUpstreamOnly          bool
	ControlPlaneSafeSummary   []byte
	NetwatchSafeSummary       []byte
	RawBodiesOmittedFromAudit bool
}

type ClaudeCodeNativeShapeHealthcheckResult struct {
	Status        string          `json:"status"`
	Fields        []string        `json:"fields"`
	Passed        int             `json:"passed"`
	Denominator   int             `json:"denominator"`
	FailedFields  []string        `json:"failed_fields,omitempty"`
	Routes        []string        `json:"routes,omitempty"`
	ModelFamilies []string        `json:"model_families,omitempty"`
	Profiles      []string        `json:"profiles,omitempty"`
	SafeEvidence  json.RawMessage `json:"safe_evidence,omitempty"`
}

func EvaluateClaudeCodeNativeShapeHealthcheckSuite(fixtures []ClaudeCodeNativeShapeFixture, evidence ClaudeCodeNativeShapeHealthcheckEvidence) ClaudeCodeNativeShapeHealthcheckResult {
	checks := map[string]bool{
		"localhost_only":     evidence.LocalhostOnly,
		"mock_upstream_only": evidence.MockUpstreamOnly,
		"body_omitted":       evidence.RawBodiesOmittedFromAudit,
	}
	routes := map[string]struct{}{}
	modelFamilies := map[string]struct{}{}
	profiles := map[string]struct{}{}
	allProfilesAttested := len(fixtures) > 0

	for _, fixture := range fixtures {
		root := gjson.ParseBytes(fixture.Body)
		route := claudeCodeNativeFixtureRoute(fixture)
		if route != "" {
			routes[route] = struct{}{}
		}
		family := claudeCodeNativeModelFamily(root.Get("model").String())
		if family != "" {
			modelFamilies[family] = struct{}{}
			checks[family+"_fixture"] = true
		}
		if fixture.Audit.ShapeHealthcheckProfile != "" {
			profiles[fixture.Audit.ShapeHealthcheckProfile] = struct{}{}
		}
		if fixture.Audit.ClientType != ClaudeCodeNativeClientType || !fixture.Audit.NativeAttested || fixture.Audit.ServerFilledShape || !isKnownClaudeCodeNativeHealthProfile(fixture.Audit.ShapeHealthcheckProfile) {
			allProfilesAttested = false
		}
		if route == ClaudeCodeNativeInboundMessages && fixture.Audit.InboundRoute == ClaudeCodeNativeInboundMessages {
			checks["messages_fixture"] = true
		}
		if route == ClaudeCodeNativeInboundCountTokens && fixture.Audit.InboundRoute == ClaudeCodeNativeInboundCountTokens {
			checks["count_tokens_fixture"] = true
		}
		if root.Get("thinking").Exists() {
			checks["thinking_fixture"] = true
		}
		if root.Get("stream").Bool() {
			checks["stream_fixture"] = true
		}
		if tools := root.Get("tools"); tools.IsArray() {
			checks["tools_fixture"] = true
			if len(tools.Array()) > 0 && fixture.Audit.ToolSearchMode == "truthful_pass_through" && claudeCodeNativeBodyHasToolSearchMarkers(fixture.Body) {
				checks["tool_search_fixture"] = true
			}
		}
	}
	checks["native_attestation_profile"] = allProfilesAttested
	checks["control_plane_safe_intent_fixture"] = validClaudeCodeNativeControlPlaneSafeIntent(evidence.ControlPlaneSafeSummary)
	checks["netwatch_fixture"] = validClaudeCodeNativeNetwatchSummary(evidence.NetwatchSafeSummary)

	result := ClaudeCodeNativeShapeHealthcheckResult{
		Status:        ClaudeCodeNativeShapeHealthcheckPass,
		Fields:        append([]string(nil), ClaudeCodeNativeShapeHealthcheckFields...),
		Denominator:   len(ClaudeCodeNativeShapeHealthcheckFields),
		Routes:        sortedClaudeCodeNativeStringSet(routes),
		ModelFamilies: sortedClaudeCodeNativeStringSet(modelFamilies),
		Profiles:      sortedClaudeCodeNativeStringSet(profiles),
	}
	for _, field := range ClaudeCodeNativeShapeHealthcheckFields {
		if checks[field] {
			result.Passed++
			continue
		}
		result.FailedFields = append(result.FailedFields, field)
	}
	if result.Passed != result.Denominator {
		result.Status = ClaudeCodeNativeShapeHealthcheckFail
	}
	if checks["control_plane_safe_intent_fixture"] && checks["netwatch_fixture"] {
		result.SafeEvidence = []byte(`{"control_plane":"safe_summary_present","netwatch":"safe_summary_present"}`)
	}
	return result
}

func HasClaudeCodeNativeShapeHealthcheckField(field string) bool {
	field = strings.TrimSpace(field)
	for _, item := range ClaudeCodeNativeShapeHealthcheckFields {
		if item == field {
			return true
		}
	}
	return false
}

func claudeCodeNativeFixtureRoute(fixture ClaudeCodeNativeShapeFixture) string {
	if fixture.Route != "" {
		return fixture.Route
	}
	return fixture.Audit.InboundRoute
}

func claudeCodeNativeModelFamily(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.Contains(model, "opus"):
		return "opus"
	case strings.Contains(model, "sonnet"):
		return "sonnet"
	default:
		return ""
	}
}

func claudeCodeNativeBodyHasToolSearchMarkers(body []byte) bool {
	var root any
	if json.Unmarshal(body, &root) != nil {
		return false
	}
	return claudeCodeNativeJSONContainsNativeToolMarker(root, "tool_reference") || claudeCodeNativeJSONContainsNativeToolMarker(root, "defer_loading")
}

func claudeCodeNativeJSONContainsNativeToolMarker(value any, needle string) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if key == "input_schema" || key == "schema" {
				continue
			}
			if key == needle || claudeCodeNativeJSONContainsNativeToolMarker(item, needle) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if claudeCodeNativeJSONContainsNativeToolMarker(item, needle) {
				return true
			}
		}
	}
	return false
}

func validClaudeCodeNativeControlPlaneSafeIntent(summary []byte) bool {
	var raw map[string]any
	if len(summary) == 0 || json.Unmarshal(summary, &raw) != nil {
		return false
	}
	allowed := map[string]struct{}{
		"safe_intent": {}, "method": {}, "path_template": {}, "decision": {}, "status": {},
		"stores_raw": {}, "messages_signing_reused": {}, "response_schema_keys": {},
	}
	if !claudeCodeNativeExactKeys(raw, allowed) || claudeCodeNativeContainsUnsafeSummaryValue(raw) {
		return false
	}
	safeIntent, ok := raw["safe_intent"].(bool)
	if !ok || !safeIntent {
		return false
	}
	method, ok := raw["method"].(string)
	if !ok || method != "GET" {
		return false
	}
	pathTemplate, ok := raw["path_template"].(string)
	if !ok || !strings.HasPrefix(pathTemplate, "/") {
		return false
	}
	decision, ok := raw["decision"].(string)
	if !ok || !claudeCodeNativeSafeControlPlaneDecision(decision) {
		return false
	}
	status, ok := raw["status"].(float64)
	if !ok || status != float64(int(status)) || status < 200 || status > 599 {
		return false
	}
	storesRaw, ok := raw["stores_raw"].(bool)
	if !ok || storesRaw {
		return false
	}
	reusedSigning, ok := raw["messages_signing_reused"].(bool)
	if !ok || reusedSigning {
		return false
	}
	keys, ok := raw["response_schema_keys"].([]any)
	if !ok || len(keys) == 0 {
		return false
	}
	for _, item := range keys {
		key, ok := item.(string)
		if !ok || key == "" || claudeCodeNativeUnsafeSummaryText(key) {
			return false
		}
	}
	return true
}

func validClaudeCodeNativeNetwatchSummary(summary []byte) bool {
	var raw map[string]any
	if len(summary) == 0 || json.Unmarshal(summary, &raw) != nil {
		return false
	}
	allowed := map[string]struct{}{
		"connection_count": {}, "potential_guard_bypass_count": {}, "official_or_public_bypass_count": {},
		"loopback_guard_connection_count": {}, "remote_host_buckets": {}, "stores_payload": {}, "stores_headers": {},
	}
	if !claudeCodeNativeExactKeys(raw, allowed) || claudeCodeNativeContainsUnsafeSummaryValue(raw) {
		return false
	}
	connectionCount, ok := raw["connection_count"].(float64)
	if !ok || connectionCount < 0 || connectionCount != float64(int(connectionCount)) {
		return false
	}
	bypassCount, ok := raw["potential_guard_bypass_count"].(float64)
	if !ok || bypassCount != 0 {
		return false
	}
	officialBypassCount, ok := raw["official_or_public_bypass_count"].(float64)
	if !ok || officialBypassCount != 0 {
		return false
	}
	loopbackCount, ok := raw["loopback_guard_connection_count"].(float64)
	if !ok || loopbackCount <= 0 || loopbackCount != float64(int(loopbackCount)) {
		return false
	}
	storesPayload, ok := raw["stores_payload"].(bool)
	if !ok || storesPayload {
		return false
	}
	storesHeaders, ok := raw["stores_headers"].(bool)
	if !ok || storesHeaders {
		return false
	}
	buckets, ok := raw["remote_host_buckets"].(map[string]any)
	if !ok || len(buckets) == 0 {
		return false
	}
	for bucket, count := range buckets {
		bucketCount, ok := count.(float64)
		if bucket != "loopback" || !ok || bucketCount <= 0 || bucketCount != float64(int(bucketCount)) {
			return false
		}
	}
	return true
}

func claudeCodeNativeSafeControlPlaneDecision(decision string) bool {
	switch decision {
	case "stub_json", "suppress_204", "quarantine_block", "block_403", "shadow_stub", "shadow_block":
		return true
	default:
		return false
	}
}

func claudeCodeNativeExactKeys(raw map[string]any, allowed map[string]struct{}) bool {
	if len(raw) != len(allowed) {
		return false
	}
	for key := range raw {
		if _, ok := allowed[key]; !ok {
			return false
		}
	}
	return true
}

func claudeCodeNativeContainsUnsafeSummaryValue(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if claudeCodeNativeUnsafeSummaryText(key) || claudeCodeNativeContainsUnsafeSummaryValue(item) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if claudeCodeNativeContainsUnsafeSummaryValue(item) {
				return true
			}
		}
	case string:
		return claudeCodeNativeUnsafeSummaryText(typed)
	}
	return false
}

func claudeCodeNativeUnsafeSummaryText(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	if lower == "" {
		return false
	}
	for _, marker := range []string{"authorization", "cookie", "proxy_credential", "x-api-key", "token", "secret", "prompt", "raw_", "raw_body", "raw_prompt", "raw_telemetry", "raw_cch", "cch=", "account_uuid", "org_uuid", "user_uuid", "email", "access_token", "refresh_token", "api.anthropic.com", "claude.ai", "claude.com", "anthropic_or_claude", "public_ip", "dns_name"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return looksPlainDigest(lower) || controlPlaneUUIDRe.MatchString(lower) || controlPlaneEmailRe.MatchString(lower)
}
