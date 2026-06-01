package service

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/tidwall/gjson"
)

func (m *CodexGatewayCaptureManager) codexGatewayCaptureRequestDiagnostics(body []byte) map[string]any {
	if m == nil || m.redact == nil {
		return nil
	}
	out := map[string]any{}
	if background := m.codexGatewayCaptureDesktopBackgroundTaskDiagnostics(body); len(background) > 0 {
		out["desktop_background_task"] = background
	}
	return out
}

func (m *CodexGatewayCaptureManager) codexGatewayCaptureDesktopBackgroundTaskDiagnostics(body []byte) map[string]any {
	model := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	format := gjson.GetBytes(body, "text.format")
	formatType := strings.TrimSpace(format.Get("type").String())
	formatName := strings.TrimSpace(format.Get("name").String())
	schema := format.Get("schema")
	if !strings.EqualFold(model, "gpt-5.4-mini") && !strings.EqualFold(formatType, "json_schema") {
		return nil
	}
	out := map[string]any{
		"detected":            true,
		"model":               model,
		"text_format_type":    formatType,
		"reasoning_effort":    strings.TrimSpace(gjson.GetBytes(body, "reasoning.effort").String()),
		"parallel_tool_calls": gjson.GetBytes(body, "parallel_tool_calls").Bool(),
	}
	if formatName != "" {
		out["text_format_name_hash"] = m.redact.CorrelationHash("text_format_name", formatName)
	}
	keys := codexGatewayCaptureJSONSchemaPropertyKeys(schema.Raw)
	if len(keys) > 0 {
		hashes := make([]string, 0, len(keys))
		known := make([]string, 0, len(keys))
		for _, key := range keys {
			hashes = append(hashes, m.redact.CorrelationHash("json_schema_property", key))
			if codexGatewayCaptureAllowedSchemaKey(key) {
				known = append(known, key)
			}
		}
		sort.Strings(hashes)
		sort.Strings(known)
		out["text_format_schema_key_hashes"] = hashes
		out["text_format_schema_key_count"] = len(keys)
		out["text_format_schema_known_keys"] = known
	}
	required := codexGatewayCaptureJSONSchemaRequiredKeys(schema.Raw)
	if len(required) > 0 {
		knownRequired := make([]string, 0, len(required))
		for _, key := range required {
			if codexGatewayCaptureAllowedSchemaKey(key) {
				knownRequired = append(knownRequired, key)
			}
		}
		sort.Strings(knownRequired)
		out["text_format_required_known_keys"] = knownRequired
		out["text_format_required_key_count"] = len(required)
	}
	return out
}

func codexGatewayCaptureJSONSchemaPropertyKeys(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var schema map[string]any
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		return nil
	}
	props, _ := schema["properties"].(map[string]any)
	keys := make([]string, 0, len(props))
	for key := range props {
		key = strings.TrimSpace(key)
		if key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func codexGatewayCaptureJSONSchemaRequiredKeys(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var schema map[string]any
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		return nil
	}
	required, _ := schema["required"].([]any)
	keys := make([]string, 0, len(required))
	for _, item := range required {
		if key, ok := item.(string); ok && strings.TrimSpace(key) != "" {
			keys = append(keys, strings.TrimSpace(key))
		}
	}
	sort.Strings(keys)
	return keys
}

func codexGatewayCaptureAllowedSchemaKey(key string) bool {
	switch strings.TrimSpace(key) {
	case "title":
		return true
	default:
		return false
	}
}

const (
	codexGatewayDeepSeekCacheSerializationVersion = "deepseek-cache-v1"
	codexGatewayDeepSeekMessagePrefixLimit        = 8
)

var codexGatewayDeepSeekRequestShapeExcludedFields = []string{
	"stream",
	"stream_options",
	"user_id",
}

type codexGatewayDeepSeekCacheSnapshot struct {
	ScopeKey           string
	ExactKey           string
	Provider           string
	UpstreamModel      string
	RequestPrefixHash  string
	StaticPrefixHash   string
	ToolSchemaHash     string
	MessagePrefixHash  string
	RequestShapeHash   string
	UserIDHash         string
	UserIDScope        string
	WorkspaceScopeHash string
	ManagedSessionHash string
}

func (m *CodexGatewayCaptureManager) mergeRequestDiagnostics(trace *CodexGatewayTrace, values map[string]any) {
	if !m.enabledTrace(trace) || len(values) == 0 {
		return
	}
	trace.mu.Lock()
	if trace.requestDiag == nil {
		trace.requestDiag = make(map[string]any, len(values))
	}
	for key, value := range values {
		trace.requestDiag[key] = value
	}
	snapshot := cloneCaptureMap(trace.requestDiag)
	trace.mu.Unlock()
	m.writeJSON(trace, "client_request.diagnostics.json", snapshot)
}

func codexGatewayDeepSeekCaptureDiagnostics(body map[string]any, userID string, userDiag codexGatewayDeepSeekUserIDDiagnostics, replayDiag codexGatewayDeepSeekReplayDiagnostics, redactor *CodexGatewayCaptureRedactor) map[string]any {
	if redactor == nil || len(body) == 0 {
		return nil
	}
	messages, _ := body["messages"].([]any)
	messagePrefix := codexGatewayDeepSeekMessagePrefix(messages, codexGatewayDeepSeekMessagePrefixLimit)
	messageSuffix := codexGatewayDeepSeekMessageSuffix(messages, codexGatewayDeepSeekMessagePrefixLimit)
	messageLast := codexGatewayDeepSeekMessageLast(messages)
	staticPrefix := codexGatewayDeepSeekStaticPrefix(body)
	toolSchema := codexGatewayDeepSeekToolSchema(body)
	requestShape := codexGatewayDeepSeekRequestShape(body)
	out := map[string]any{
		"detected":            true,
		"raw_body_hash":       codexGatewayDeepSeekStableHash(redactor, body),
		"messages_full_hash":  codexGatewayDeepSeekStableHash(redactor, messages),
		"message_suffix_hash": codexGatewayDeepSeekStableHash(redactor, messageSuffix),
		"message_last_hash":   codexGatewayDeepSeekStableHash(redactor, messageLast),
		"request_prefix_hash": codexGatewayDeepSeekStableHash(redactor, map[string]any{"static_prefix": staticPrefix, "tool_schema": toolSchema, "message_prefix": messagePrefix}),
		"static_prefix_hash":  codexGatewayDeepSeekStableHash(redactor, staticPrefix),
		"tool_schema_hash":    codexGatewayDeepSeekStableHash(redactor, toolSchema),
		"message_prefix_hash": codexGatewayDeepSeekStableHash(redactor, messagePrefix),
		"request_shape_hash":  codexGatewayDeepSeekStableHash(redactor, requestShape),
		"message_count":       len(messages),
		"message_prefix_count": func() int {
			if len(messages) < codexGatewayDeepSeekMessagePrefixLimit {
				return len(messages)
			}
			return codexGatewayDeepSeekMessagePrefixLimit
		}(),
		"stable_serialization": map[string]any{
			"version":                       codexGatewayDeepSeekCacheSerializationVersion,
			"request_shape_excluded_fields": append([]string(nil), codexGatewayDeepSeekRequestShapeExcludedFields...),
			"message_prefix_limit":          codexGatewayDeepSeekMessagePrefixLimit,
			"user_id_scope":                 strings.TrimSpace(userDiag.Scope),
			"user_id_source":                strings.TrimSpace(userDiag.Source),
		},
	}
	for key, value := range replayDiag.toCaptureMap() {
		out[key] = value
	}
	for key, value := range codexGatewayDeepSeekUserScopeDiagnostics(userID, userDiag, redactor) {
		out[key] = value
	}
	return out
}

func codexGatewayDeepSeekUserScopeDiagnostics(userID string, userDiag codexGatewayDeepSeekUserIDDiagnostics, redactor *CodexGatewayCaptureRedactor) map[string]any {
	if redactor == nil {
		return nil
	}
	out := map[string]any{}
	if scope := strings.TrimSpace(userDiag.Scope); scope != "" {
		out["user_id_scope"] = scope
	}
	if source := strings.TrimSpace(userDiag.Source); source != "" {
		out["user_id_source"] = source
	}
	if strings.TrimSpace(userID) != "" {
		out["user_id_hash"] = redactor.CorrelationHash("deepseek_user_id", userID)
	}
	if strings.TrimSpace(userDiag.WorkspaceScopeKey) != "" {
		out["workspace_scope_hash"] = redactor.CorrelationHash("deepseek_workspace_scope", userDiag.WorkspaceScopeKey)
	}
	if strings.TrimSpace(userDiag.ManagedSessionBucket) != "" {
		out["managed_session_bucket_hash"] = redactor.CorrelationHash("deepseek_managed_session_bucket", userDiag.ManagedSessionBucket)
	}
	return out
}

func codexGatewayDeepSeekCacheUsageFields(diagnostics map[string]any) map[string]any {
	if len(diagnostics) == 0 {
		return nil
	}
	out := map[string]any{}
	copyKey := func(key string) {
		if value, ok := diagnostics[key]; ok {
			out[key] = value
		}
	}
	copyKey("request_prefix_hash")
	copyKey("raw_body_hash")
	copyKey("messages_full_hash")
	copyKey("message_suffix_hash")
	copyKey("message_last_hash")
	copyKey("static_prefix_hash")
	copyKey("tool_schema_hash")
	copyKey("message_prefix_hash")
	copyKey("request_shape_hash")
	copyKey("previous_response_id_present")
	copyKey("previous_response_replay_mode")
	copyKey("state_lookup_status")
	copyKey("user_id_hash")
	copyKey("workspace_scope_hash")
	copyKey("managed_session_bucket_hash")
	copyKey("message_count")
	copyKey("message_prefix_count")
	if stable, ok := diagnostics["stable_serialization"].(map[string]any); ok {
		if value, ok := stable["version"]; ok {
			out["stable_serialization_version"] = value
		}
		if value, ok := stable["request_shape_excluded_fields"]; ok {
			out["request_shape_excluded_fields"] = value
		}
		if value, ok := stable["message_prefix_limit"]; ok {
			out["message_prefix_limit"] = value
		}
		if value, ok := stable["user_id_scope"]; ok {
			out["user_id_scope"] = value
		}
		if value, ok := stable["user_id_source"]; ok {
			out["user_id_source"] = value
		}
	}
	return out
}

func codexGatewayDeepSeekStableHash(redactor *CodexGatewayCaptureRedactor, value any) string {
	if redactor == nil {
		return ""
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return redactor.HashText(string(payload))
}

func codexGatewayDeepSeekStaticPrefix(body map[string]any) map[string]any {
	keys := []string{
		"model",
		"reasoning_effort",
		"thinking",
		"max_tokens",
		"temperature",
		"top_p",
		"presence_penalty",
		"frequency_penalty",
		"tool_choice",
	}
	out := make(map[string]any)
	for _, key := range keys {
		if value, ok := body[key]; ok {
			out[key] = value
		}
	}
	return out
}

func codexGatewayDeepSeekToolSchema(body map[string]any) any {
	if value, ok := body["tools"]; ok {
		return value
	}
	return []any{}
}

func codexGatewayDeepSeekMessagePrefix(messages []any, limit int) []any {
	if limit <= 0 || len(messages) == 0 {
		return []any{}
	}
	if len(messages) < limit {
		limit = len(messages)
	}
	out := make([]any, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, messages[i])
	}
	return out
}

func codexGatewayDeepSeekMessageSuffix(messages []any, limit int) []any {
	if limit <= 0 || len(messages) == 0 {
		return []any{}
	}
	if len(messages) < limit {
		limit = len(messages)
	}
	start := len(messages) - limit
	out := make([]any, 0, limit)
	for i := start; i < len(messages); i++ {
		out = append(out, messages[i])
	}
	return out
}

func codexGatewayDeepSeekMessageLast(messages []any) any {
	if len(messages) == 0 {
		return map[string]any{}
	}
	return messages[len(messages)-1]
}

func codexGatewayDeepSeekRequestShape(body map[string]any) map[string]any {
	if len(body) == 0 {
		return nil
	}
	out := make(map[string]any, len(body))
	for key, value := range body {
		if codexGatewayDeepSeekExcludedRequestShapeField(key) {
			continue
		}
		out[key] = value
	}
	return out
}

func codexGatewayDeepSeekExcludedRequestShapeField(key string) bool {
	key = strings.TrimSpace(key)
	for _, excluded := range codexGatewayDeepSeekRequestShapeExcludedFields {
		if key == excluded {
			return true
		}
	}
	return false
}

func (m *CodexGatewayCaptureManager) cacheEfficiency(trace *CodexGatewayTrace, provider, upstreamModel any) map[string]any {
	if !m.enabledTrace(trace) {
		return nil
	}
	trace.mu.Lock()
	cacheUsage := cloneCaptureMap(trace.cacheUsage)
	requestDiag := cloneCaptureMap(trace.requestDiag)
	trace.mu.Unlock()
	efficiency := codexGatewayCaptureCacheEfficiency(cacheUsage, provider)
	snapshot, ok := codexGatewayDeepSeekCacheSnapshotFromDiagnostics(requestDiag, provider, upstreamModel)
	if ok {
		cacheUpdates := map[string]any{}
		if len(efficiency) > 0 {
			if missAttribution := m.deepSeekCacheMissAttribution(snapshot, efficiency); len(missAttribution) > 0 {
				efficiency["miss_attribution"] = missAttribution
				cacheUpdates["cache_miss_attribution"] = missAttribution
				if reason := codexGatewayDeepSeekPrefixHashChangedReason(missAttribution); reason != "" {
					efficiency["prefix_hash_changed_reason"] = reason
					cacheUpdates["prefix_hash_changed_reason"] = reason
				}
			}
		}
		if len(cacheUpdates) > 0 {
			m.mergeCacheUsage(trace, cacheUpdates)
		}
		m.rememberDeepSeekCacheSnapshot(snapshot)
	}
	return efficiency
}

func codexGatewayDeepSeekCacheSnapshotFromDiagnostics(requestDiag map[string]any, provider, upstreamModel any) (codexGatewayDeepSeekCacheSnapshot, bool) {
	if !strings.EqualFold(strings.TrimSpace(codexGatewayCaptureStringValue(provider)), string(CodexGatewayProviderDeepSeek)) {
		return codexGatewayDeepSeekCacheSnapshot{}, false
	}
	raw, ok := requestDiag["deepseek_cache"].(map[string]any)
	if !ok {
		return codexGatewayDeepSeekCacheSnapshot{}, false
	}
	stable, _ := raw["stable_serialization"].(map[string]any)
	version := strings.TrimSpace(codexGatewayCaptureStringValue(stable["version"]))
	if version == "" {
		version = codexGatewayDeepSeekCacheSerializationVersion
	}
	userScope := strings.TrimSpace(codexGatewayCaptureStringValue(stable["user_id_scope"]))
	upstream := strings.TrimSpace(codexGatewayCaptureStringValue(upstreamModel))
	if upstream == "" {
		upstream = strings.TrimSpace(codexGatewayCaptureStringValue(raw["model"]))
	}
	snapshot := codexGatewayDeepSeekCacheSnapshot{
		Provider:           strings.TrimSpace(codexGatewayCaptureStringValue(provider)),
		UpstreamModel:      upstream,
		RequestPrefixHash:  strings.TrimSpace(codexGatewayCaptureStringValue(raw["request_prefix_hash"])),
		StaticPrefixHash:   strings.TrimSpace(codexGatewayCaptureStringValue(raw["static_prefix_hash"])),
		ToolSchemaHash:     strings.TrimSpace(codexGatewayCaptureStringValue(raw["tool_schema_hash"])),
		MessagePrefixHash:  strings.TrimSpace(codexGatewayCaptureStringValue(raw["message_prefix_hash"])),
		RequestShapeHash:   strings.TrimSpace(codexGatewayCaptureStringValue(raw["request_shape_hash"])),
		UserIDHash:         strings.TrimSpace(codexGatewayCaptureStringValue(raw["user_id_hash"])),
		UserIDScope:        userScope,
		WorkspaceScopeHash: strings.TrimSpace(codexGatewayCaptureStringValue(raw["workspace_scope_hash"])),
		ManagedSessionHash: strings.TrimSpace(codexGatewayCaptureStringValue(raw["managed_session_bucket_hash"])),
	}
	if snapshot.RequestPrefixHash == "" || snapshot.RequestShapeHash == "" {
		return codexGatewayDeepSeekCacheSnapshot{}, false
	}
	snapshot.ScopeKey = strings.Join([]string{
		snapshot.Provider,
		snapshot.UpstreamModel,
		version,
		firstDeepSeekCacheScopeValue(snapshot.WorkspaceScopeHash, "shared_workspace"),
		firstDeepSeekCacheScopeValue(snapshot.ManagedSessionHash, "shared_session"),
	}, "|")
	snapshot.ExactKey = strings.Join([]string{
		snapshot.RequestPrefixHash,
		snapshot.ToolSchemaHash,
		snapshot.MessagePrefixHash,
		snapshot.UserIDHash,
		snapshot.RequestShapeHash,
	}, "|")
	return snapshot, true
}

func (m *CodexGatewayCaptureManager) deepSeekCacheMissAttribution(snapshot codexGatewayDeepSeekCacheSnapshot, efficiency map[string]any) []string {
	if len(efficiency) == 0 {
		return nil
	}
	missTokens, ok := codexGatewayCaptureIntValue(efficiency["prompt_cache_miss_tokens"])
	if !ok || missTokens <= 0 {
		return nil
	}
	m.deepSeekCacheMu.Lock()
	defer m.deepSeekCacheMu.Unlock()
	attribution := make([]string, 0, 6)
	if _, seen := m.deepSeekSeenRequestExact[snapshot.ExactKey]; !seen {
		attribution = append(attribution, "request_not_warmed")
	}
	if previous, ok := m.deepSeekLastRequestByKey[snapshot.ScopeKey]; ok {
		if previous.StaticPrefixHash != "" && previous.StaticPrefixHash != snapshot.StaticPrefixHash {
			attribution = append(attribution, "static_prefix_changed")
		}
		if previous.ToolSchemaHash != "" && previous.ToolSchemaHash != snapshot.ToolSchemaHash {
			attribution = append(attribution, "tool_schema_changed")
		}
		if previous.MessagePrefixHash != "" && previous.MessagePrefixHash != snapshot.MessagePrefixHash {
			attribution = append(attribution, "message_prefix_changed")
		}
		if previous.UserIDHash != "" && previous.UserIDHash != snapshot.UserIDHash {
			attribution = append(attribution, "user_id_changed")
		}
		if previous.RequestShapeHash != "" && previous.RequestShapeHash != snapshot.RequestShapeHash {
			attribution = append(attribution, "request_shape_changed")
			if previous.StaticPrefixHash == snapshot.StaticPrefixHash &&
				previous.ToolSchemaHash == snapshot.ToolSchemaHash &&
				previous.MessagePrefixHash == snapshot.MessagePrefixHash &&
				previous.UserIDHash == snapshot.UserIDHash {
				attribution = append(attribution, "context_compaction_changed_prefix")
			}
		}
	} else {
		attribution = append(attribution, "no_prior_scope_sample")
	}
	if len(attribution) == 0 {
		attribution = append(attribution, "upstream_best_effort_or_unknown")
	}
	return uniqueCodexGatewayStrings(attribution)
}

func (m *CodexGatewayCaptureManager) rememberDeepSeekCacheSnapshot(snapshot codexGatewayDeepSeekCacheSnapshot) {
	if snapshot.ExactKey == "" || snapshot.ScopeKey == "" {
		return
	}
	m.deepSeekCacheMu.Lock()
	defer m.deepSeekCacheMu.Unlock()
	m.deepSeekSeenRequestExact[snapshot.ExactKey] = struct{}{}
	m.deepSeekLastRequestByKey[snapshot.ScopeKey] = snapshot
}

func codexGatewayDeepSeekPrefixHashChangedReason(missAttribution []string) string {
	for _, key := range []string{
		"static_prefix_changed",
		"tool_schema_changed",
		"message_prefix_changed",
		"context_compaction_changed_prefix",
		"request_shape_changed",
	} {
		for _, item := range missAttribution {
			if item == key {
				return key
			}
		}
	}
	return ""
}

func firstDeepSeekCacheScopeValue(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return fallback
}

func codexGatewayDeepSeekSessionDiagnostics(requestDiag map[string]any) map[string]any {
	raw, ok := requestDiag["deepseek_cache"].(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := map[string]any{}
	copyKey := func(key string) {
		if value, ok := raw[key]; ok {
			out[key] = value
		}
	}
	for _, key := range []string{
		"request_prefix_hash",
		"messages_full_hash",
		"message_suffix_hash",
		"message_last_hash",
		"static_prefix_hash",
		"tool_schema_hash",
		"message_prefix_hash",
		"request_shape_hash",
		"previous_response_id_present",
		"previous_response_replay_mode",
		"state_lookup_status",
		"user_id_hash",
		"user_id_scope",
		"user_id_source",
		"workspace_scope_hash",
		"managed_session_bucket_hash",
		"message_count",
		"message_prefix_count",
	} {
		copyKey(key)
	}
	if stable, ok := raw["stable_serialization"].(map[string]any); ok {
		if value, ok := stable["version"]; ok {
			out["stable_serialization_version"] = value
		}
	}
	return out
}
