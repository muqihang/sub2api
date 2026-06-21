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
	"system_fixture",
	"context_management_fixture",
	"prompt_caching_fixture",
	"prompt_cache_usage_fixture",
	"output_config_fixture",
	"adaptive_thinking_fixture",
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
	LocalhostOnly               bool
	MockUpstreamOnly            bool
	ControlPlaneSafeSummary     []byte
	NetwatchSafeSummary         []byte
	PromptCacheSafeUsageSummary []byte
	RawBodiesOmittedFromAudit   bool
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
	observedPromptCacheLocations := map[string]struct{}{}
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
		if thinking := root.Get("thinking"); thinking.Exists() {
			checks["thinking_fixture"] = true
			if strings.EqualFold(strings.TrimSpace(thinking.Get("type").String()), "adaptive") {
				checks["adaptive_thinking_fixture"] = true
			}
		}
		if claudeCodeNativeHasSystemFixture(root) {
			checks["system_fixture"] = true
		}
		if claudeCodeNativeHasContextManagementFixture(root) {
			checks["context_management_fixture"] = true
		}
		for location := range claudeCodeNativePromptCacheControlLocationsFromRoot(root) {
			observedPromptCacheLocations[location] = struct{}{}
			checks["prompt_caching_fixture"] = true
		}
		if claudeCodeNativeHasOutputConfigFixture(root) {
			checks["output_config_fixture"] = true
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
	checks["prompt_cache_usage_fixture"] = validClaudeCodeNativePromptCacheSafeUsage(evidence.PromptCacheSafeUsageSummary, observedPromptCacheLocations)

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
		if checks["prompt_cache_usage_fixture"] {
			result.SafeEvidence = []byte(`{"control_plane":"safe_summary_present","netwatch":"safe_summary_present","prompt_cache":"safe_usage_summary_present"}`)
		} else {
			result.SafeEvidence = []byte(`{"control_plane":"safe_summary_present","netwatch":"safe_summary_present"}`)
		}
	}
	return result
}

func claudeCodeNativeHasSystemFixture(root gjson.Result) bool {
	system := root.Get("system")
	if !system.Exists() {
		return false
	}
	if system.IsArray() {
		for _, item := range system.Array() {
			if strings.TrimSpace(item.Get("type").String()) == "text" && strings.TrimSpace(item.Get("text").String()) != "" {
				return true
			}
		}
		return false
	}
	return strings.TrimSpace(system.String()) != ""
}

func claudeCodeNativeHasContextManagementFixture(root gjson.Result) bool {
	edits := root.Get("context_management.edits")
	return edits.IsArray() && len(edits.Array()) > 0
}

func claudeCodeNativeHasOutputConfigFixture(root gjson.Result) bool {
	effort := root.Get("output_config.effort")
	return effort.Exists() && strings.TrimSpace(effort.String()) != "" && !claudeCodeNativeUnsafeSummaryText(effort.String())
}

func claudeCodeNativeHasPromptCachingFixture(root gjson.Result) bool {
	return len(claudeCodeNativePromptCacheControlLocationsFromRoot(root)) > 0
}

func claudeCodeNativePromptCacheControlLocationsFromRoot(root gjson.Result) map[string]struct{} {
	var body any
	if root.Raw == "" || json.Unmarshal([]byte(root.Raw), &body) != nil {
		return map[string]struct{}{}
	}
	return claudeCodeNativePromptCacheControlLocations(body)
}

func claudeCodeNativePromptCacheControlLocations(body any) map[string]struct{} {
	locations := map[string]struct{}{}
	root, ok := body.(map[string]any)
	if !ok {
		return locations
	}
	if claudeCodeNativeValidCacheControl(root["cache_control"]) {
		locations["top_level"] = struct{}{}
	}
	if claudeCodeNativeJSONContainsValidCacheControl(root["system"]) {
		locations["system"] = struct{}{}
	}
	if claudeCodeNativeJSONContainsValidCacheControl(root["tools"]) {
		locations["tools"] = struct{}{}
	}
	if claudeCodeNativeJSONContainsValidCacheControl(root["messages"]) {
		locations["history"] = struct{}{}
	}
	return locations
}

func claudeCodeNativeJSONContainsValidCacheControl(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if key == "input_schema" || key == "schema" {
				continue
			}
			if key == "cache_control" {
				if claudeCodeNativeValidCacheControl(item) {
					return true
				}
				continue
			}
			if claudeCodeNativeJSONContainsValidCacheControl(item) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if claudeCodeNativeJSONContainsValidCacheControl(item) {
				return true
			}
		}
	}
	return false
}

func claudeCodeNativeValidCacheControl(value any) bool {
	cacheControl, ok := value.(map[string]any)
	if !ok {
		return false
	}
	cacheType, ok := cacheControl["type"].(string)
	return ok && strings.EqualFold(strings.TrimSpace(cacheType), "ephemeral")
}

func validClaudeCodeNativePromptCacheSafeUsage(summary []byte, observedLocations map[string]struct{}) bool {
	var raw map[string]any
	if len(summary) == 0 || json.Unmarshal(summary, &raw) != nil {
		return false
	}
	allowed := map[string]struct{}{
		"provider_cache_mechanism": {}, "cache_control_present": {}, "cache_control_locations": {},
		"prompt_caching_beta_present": {}, "context_management_beta_present": {}, "cache_usage_fields": {},
		"cache_creation_input_tokens": {}, "cache_read_input_tokens": {}, "stores_raw": {}, "body_omitted": {}, "response_omitted": {},
	}
	if !claudeCodeNativeExactKeys(raw, allowed) {
		return false
	}
	mechanism, ok := raw["provider_cache_mechanism"].(string)
	if !ok || mechanism != "anthropic_cache_control" {
		return false
	}
	if raw["cache_control_present"] != true || raw["prompt_caching_beta_present"] != true || raw["context_management_beta_present"] != true {
		return false
	}
	if raw["stores_raw"] != false || raw["body_omitted"] != true || raw["response_omitted"] != true {
		return false
	}
	if !claudeCodeNativePromptCacheLocationsValid(raw["cache_control_locations"], observedLocations) {
		return false
	}
	if !claudeCodeNativePromptCacheUsageFieldsValid(raw["cache_usage_fields"]) {
		return false
	}
	return claudeCodeNativeNonNegativeJSONInteger(raw["cache_creation_input_tokens"]) && claudeCodeNativeNonNegativeJSONInteger(raw["cache_read_input_tokens"])
}

func claudeCodeNativePromptCacheLocationsValid(value any, observedLocations map[string]struct{}) bool {
	locations, ok := value.([]any)
	if !ok || len(locations) == 0 || len(observedLocations) == 0 {
		return false
	}
	allowed := map[string]struct{}{"top_level": {}, "system": {}, "tools": {}, "history": {}}
	seen := map[string]struct{}{}
	for _, item := range locations {
		location, ok := item.(string)
		if !ok {
			return false
		}
		if _, ok := allowed[location]; !ok {
			return false
		}
		if _, ok := observedLocations[location]; !ok {
			return false
		}
		seen[location] = struct{}{}
	}
	return len(seen) == len(locations) && len(seen) == len(observedLocations)
}

func claudeCodeNativePromptCacheUsageFieldsValid(value any) bool {
	fields, ok := value.([]any)
	if !ok || len(fields) != 2 {
		return false
	}
	seen := map[string]struct{}{}
	for _, item := range fields {
		field, ok := item.(string)
		if !ok {
			return false
		}
		switch field {
		case "cache_creation_input_tokens", "cache_read_input_tokens":
			seen[field] = struct{}{}
		default:
			return false
		}
	}
	return len(seen) == 2
}

func claudeCodeNativeNonNegativeJSONInteger(value any) bool {
	number, ok := value.(float64)
	return ok && number >= 0 && number == float64(int(number))
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
