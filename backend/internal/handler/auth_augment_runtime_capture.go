package handler

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
	"github.com/gin-gonic/gin"
)

type augmentLegacyFinalEnvelopeCaptureMeta struct {
	Enabled             bool
	CaseID              string
	InformationRequest  string
	ResolvedInputSource string
	HasToolResults      bool
	CheckpointID        string
	AddedBlobsCount     int
	DeletedBlobsCount   int
	DialogCount         int
	DisableRetrieval    bool
	HasDisableRetrieval bool
}

var (
	augmentLegacyCaseMarkerPattern  = regexp.MustCompile(`\[(CE-[A-Za-z0-9._-]+)\]`)
	augmentLegacyOpenAITokenPattern = regexp.MustCompile(`(?i)\bsk-[A-Za-z0-9_-]{20,}\b`)
	augmentLegacyGitHubTokenPattern = regexp.MustCompile(`(?i)\bgh[pousr]_[A-Za-z0-9_]{20,}\b`)
	augmentLegacyBearerTokenPattern = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/\-=]{20,}\b`)
)

func (h *AuthHandler) augmentLegacyCaptureFinalEnvelope(
	c *gin.Context,
	endpoint string,
	requestBody []byte,
	responseBody []byte,
	statusCode int,
	captureErr error,
	startedAt time.Time,
	meta augmentLegacyFinalEnvelopeCaptureMeta,
) {
	if !augmentLegacyShouldCaptureFinalEnvelope() {
		return
	}
	if !meta.Enabled {
		return
	}

	captureDir := augmentLegacyCaptureDir()
	if captureDir == "" {
		tryWarnCaptureSkip("[augment-context-engine-final-capture] skipped unsafe or missing capture dir")
		return
	}

	caseID := augmentLegacyCaptureCaseID(requestBody, meta)
	requestID := augmentLegacyCaptureRequestID(c)
	rawDir := filepath.Join(captureDir, "raw", caseID)

	var rawRequestPath string
	var rawResponsePath string
	if augmentLegacyCaptureRawEnabled() {
		if err := os.MkdirAll(rawDir, 0o755); err != nil {
			tryWarnCaptureSkip("[augment-context-engine-final-capture] failed to prepare raw dir", err)
			return
		}

		requestFilename := fmt.Sprintf("%s.%s.final.request.json", requestID, endpoint)
		requestPath := filepath.Join(rawDir, requestFilename)
		if err := os.WriteFile(requestPath, augmentLegacyRedactBytes(requestBody), 0o644); err == nil {
			rawRequestPath = filepath.Join("raw", caseID, requestFilename)
		}

		if len(responseBody) > 0 {
			responseExt := "json"
			if !json.Valid(bytes.TrimSpace(responseBody)) {
				responseExt = "txt"
			}
			responseFilename := fmt.Sprintf("%s.%s.final.response.%s", requestID, endpoint, responseExt)
			responsePath := filepath.Join(rawDir, responseFilename)
			if err := os.WriteFile(responsePath, augmentLegacyRedactBytes(responseBody), 0o644); err == nil {
				rawResponsePath = filepath.Join("raw", caseID, responseFilename)
			}
		}
	}

	summary := augmentLegacyBuildFinalEnvelopeSummary(
		c,
		endpoint,
		requestBody,
		responseBody,
		statusCode,
		captureErr,
		startedAt,
		meta,
		rawRequestPath,
		rawResponsePath,
	)

	if err := os.MkdirAll(captureDir, 0o755); err != nil {
		tryWarnCaptureSkip("[augment-context-engine-final-capture] failed to prepare summary dir", err)
		return
	}
	summaryPath := filepath.Join(captureDir, "safe-summary.jsonl")
	if err := appendJSONLine(summaryPath, summary); err != nil {
		tryWarnCaptureSkip("[augment-context-engine-final-capture] failed to append summary", err)
	}
}

func augmentLegacyShouldCaptureFinalEnvelope() bool {
	return os.Getenv("AUGMENT_CAPTURE_CONTEXT_ENGINE_ENVELOPE") == "1" &&
		os.Getenv("AUGMENT_CAPTURE_CONTEXT_ENGINE_FINAL") == "1"
}

func augmentLegacyCaptureRawEnabled() bool {
	return os.Getenv("AUGMENT_CAPTURE_CONTEXT_ENGINE_RAW") == "1"
}

func augmentLegacyCaptureDir() string {
	rawDir := strings.TrimSpace(os.Getenv("AUGMENT_CAPTURE_CONTEXT_ENGINE_DIR"))
	if rawDir == "" {
		return ""
	}

	absDir, err := filepath.Abs(rawDir)
	if err != nil {
		return ""
	}
	parts := strings.Split(filepath.Clean(absDir), string(filepath.Separator))
	if len(parts) < 2 || parts[len(parts)-2] != "captures" || parts[len(parts)-1] != "context-engine-envelope" {
		return ""
	}
	return absDir
}

func augmentLegacyCaptureCaseID(requestBody []byte, meta augmentLegacyFinalEnvelopeCaptureMeta) string {
	if caseID := sanitizeCaptureComponent(meta.CaseID); caseID != "" {
		return caseID
	}
	if match := augmentLegacyCaseMarkerPattern.FindStringSubmatch(string(requestBody)); len(match) == 2 {
		return sanitizeCaptureComponent(match[1])
	}
	if envCaseID := sanitizeCaptureComponent(os.Getenv("AUGMENT_CAPTURE_CONTEXT_ENGINE_CASE_ID")); envCaseID != "" {
		return envCaseID
	}
	return "unknown-case"
}

func augmentLegacyCaptureRequestID(c *gin.Context) string {
	if c != nil && c.Request != nil {
		if requestID := strings.TrimSpace(c.GetHeader("x-request-id")); requestID != "" {
			return sanitizeCaptureComponent(requestID)
		}
		if requestID := strings.TrimSpace(c.GetHeader("x-request-session-id")); requestID != "" {
			return sanitizeCaptureComponent(requestID)
		}
	}
	return sanitizeCaptureComponent(fmt.Sprintf("%d", time.Now().UnixNano()))
}

func augmentLegacyBuildFinalEnvelopeSummary(
	c *gin.Context,
	endpoint string,
	requestBody []byte,
	responseBody []byte,
	statusCode int,
	captureErr error,
	startedAt time.Time,
	meta augmentLegacyFinalEnvelopeCaptureMeta,
	rawRequestPath string,
	rawResponsePath string,
) map[string]any {
	requestEnvelope := augmentLegacyParseJSONObject(requestBody)
	messages := augmentLegacyEnvelopeMessages(requestEnvelope)
	requestTopLevelKeys := augmentLegacySortedTopLevelKeys(requestEnvelope)

	responseEnvelope, responseIsArray, responseArrayLength := augmentLegacyParseResponseEnvelope(responseBody)
	responseTopLevelKeys := augmentLegacySortedTopLevelKeys(responseEnvelope)

	requestJSON := string(requestBody)
	messageContentTotalBytes := augmentLegacyMessageContentTotalBytes(messages)
	roleOrder := augmentLegacyMessageRoleOrder(messages)
	formattedRetrievalBytes := augmentLegacyFormattedRetrievalBytes(messages)

	return map[string]any{
		"schema_version":                           1,
		"captured_at":                              time.Now().UTC().Format(time.RFC3339Nano),
		"case_id":                                  augmentLegacyCaptureCaseID(requestBody, meta),
		"request_id":                               augmentLegacyCaptureRequestID(c),
		"endpoint":                                 endpoint,
		"route":                                    "local_gateway",
		"reason":                                   "final_envelope",
		"final_capture_stage":                      "pre_post",
		"host":                                     augmentLegacyCaptureHost(c),
		"status_code":                              statusCode,
		"latency_ms":                               augmentLegacyLatencyMillis(startedAt),
		"error_class":                              nullIfEmpty(augmentLegacyErrorClass(captureErr)),
		"request_body_bytes":                       augmentLegacyByteLength(requestBody),
		"response_body_bytes":                      augmentLegacyByteLength(responseBody),
		"request_top_level_keys":                   requestTopLevelKeys,
		"request_is_array":                         false,
		"request_array_length":                     nil,
		"response_top_level_keys":                  responseTopLevelKeys,
		"response_is_array":                        responseIsArray,
		"response_array_length":                    responseArrayLength,
		"information_request":                      nullIfEmpty(augmentLegacyRedactText(meta.InformationRequest)),
		"information_request_sha256":               augmentLegacySHA256OrNil(meta.InformationRequest),
		"resolved_user_input_source":               nullIfEmpty(meta.ResolvedInputSource),
		"has_tool_results":                         meta.HasToolResults,
		"checkpoint_id":                            nullIfEmpty(meta.CheckpointID),
		"checkpoint_id_present":                    strings.TrimSpace(meta.CheckpointID) != "",
		"blobs_checkpoint_id_present":              strings.TrimSpace(meta.CheckpointID) != "",
		"added_blobs_count":                        meta.AddedBlobsCount,
		"deleted_blobs_count":                      meta.DeletedBlobsCount,
		"dialog_count":                             meta.DialogCount,
		"max_output_length":                        nil,
		"blob_count":                               nil,
		"blob_name_count":                          nil,
		"hash_like_key_count":                      nil,
		"missing_blob_count":                       nil,
		"uploaded_blob_count":                      nil,
		"already_uploaded_blob_count":              nil,
		"formatted_retrieval_bytes":                formattedRetrievalBytes,
		"final_envelope_capture_enabled":           true,
		"final_message_array_present":              len(messages) > 0,
		"final_message_count":                      len(messages),
		"final_message_role_order":                 roleOrder,
		"final_message_content_total_bytes":        messageContentTotalBytes,
		"final_codebase_retrieval_marker_roles":    augmentLegacyMarkerRoles(messages, "[CODEBASE_RETRIEVAL]"),
		"final_information_request_roles":          augmentLegacyMarkerRoles(messages, "information_request"),
		"final_tool_result_count":                  augmentLegacyCountToolResults(messages),
		"final_tool_call_count":                    augmentLegacyCountToolCalls(messages),
		"final_contains_codebase_retrieval":        strings.Contains(requestJSON, "codebase-retrieval"),
		"final_contains_codebase_retrieval_marker": strings.Contains(requestJSON, "[CODEBASE_RETRIEVAL]"),
		"final_contains_formatted_retrieval":       strings.Contains(requestJSON, "formattedRetrieval") || strings.Contains(requestJSON, "formatted_retrieval"),
		"final_contains_information_request":       strings.Contains(requestJSON, "information_request") || strings.Contains(requestJSON, "informationRequest"),
		"final_disable_retrieval":                  meta.DisableRetrieval,
		"raw_request_path":                         nullIfEmpty(rawRequestPath),
		"raw_response_path":                        nullIfEmpty(rawResponsePath),
		"error_message":                            errorMessageOrNilRedacted(captureErr),
	}
}

func augmentLegacyParseJSONObject(raw []byte) map[string]any {
	var out map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(raw), &out); err != nil {
		return map[string]any{}
	}
	return out
}

func augmentLegacyParseResponseEnvelope(raw []byte) (map[string]any, bool, any) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || !json.Valid(trimmed) {
		return map[string]any{}, false, nil
	}

	var decoded any
	if err := json.Unmarshal(trimmed, &decoded); err != nil {
		return map[string]any{}, false, nil
	}
	switch v := decoded.(type) {
	case map[string]any:
		return v, false, nil
	case []any:
		return map[string]any{}, true, len(v)
	default:
		return map[string]any{}, false, nil
	}
}

func augmentLegacyEnvelopeMessages(envelope map[string]any) []any {
	if envelope == nil {
		return nil
	}
	if messages, ok := envelope["messages"].([]any); ok {
		return messages
	}
	return nil
}

func augmentLegacySortedTopLevelKeys(envelope map[string]any) []string {
	if len(envelope) == 0 {
		return []string{}
	}
	keys := make([]string, 0, len(envelope))
	for key := range envelope {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func augmentLegacyMessageRoleOrder(messages []any) []string {
	if len(messages) == 0 {
		return []string{}
	}
	roles := make([]string, 0, len(messages))
	for _, message := range messages {
		role := augmentLegacyMessageRole(message)
		if role == "" {
			continue
		}
		roles = append(roles, role)
	}
	return roles
}

func augmentLegacyMessageRole(message any) string {
	msg, ok := message.(map[string]any)
	if !ok {
		return ""
	}
	if role, ok := msg["role"].(string); ok && strings.TrimSpace(role) != "" {
		return strings.TrimSpace(role)
	}
	if speaker, ok := msg["speaker"].(string); ok && strings.TrimSpace(speaker) != "" {
		return strings.TrimSpace(speaker)
	}
	if kind, ok := msg["type"].(string); ok && strings.TrimSpace(kind) != "" {
		return strings.TrimSpace(kind)
	}
	if msg["toolResultNode"] != nil || msg["tool_result"] != nil || msg["tool_call_id"] != nil {
		return "tool"
	}
	return ""
}

func augmentLegacyMarkerRoles(messages []any, marker string) []string {
	if len(messages) == 0 || strings.TrimSpace(marker) == "" {
		return []string{}
	}
	seen := map[string]bool{}
	roles := make([]string, 0, len(messages))
	for _, message := range messages {
		msg, ok := message.(map[string]any)
		if !ok {
			continue
		}
		content := augmentLegacyMessageContentString(msg)
		if !strings.Contains(content, marker) {
			continue
		}
		role := augmentLegacyMessageRole(msg)
		if role == "" || seen[role] {
			continue
		}
		seen[role] = true
		roles = append(roles, role)
	}
	return roles
}

func augmentLegacyMessageContentTotalBytes(messages []any) int {
	total := 0
	for _, message := range messages {
		total += augmentLegacyMessageContentBytes(message)
	}
	return total
}

func augmentLegacyMessageContentBytes(message any) int {
	msg, ok := message.(map[string]any)
	if !ok {
		return 0
	}
	content := msg["content"]
	if content == nil {
		content = msg["text"]
	}
	if content == nil {
		content = msg["message"]
	}
	if content == nil {
		content = msg["toolResultNode"]
	}
	if content == nil {
		content = msg["tool_result"]
	}
	return augmentLegacyByteLength(content)
}

func augmentLegacyFormattedRetrievalBytes(messages []any) int {
	for _, message := range messages {
		msg, ok := message.(map[string]any)
		if !ok {
			continue
		}
		role := augmentLegacyMessageRole(msg)
		if role != "assistant" && role != "system" {
			continue
		}
		content := augmentLegacyMessageContentString(msg)
		if strings.Contains(content, "[CODEBASE_RETRIEVAL]") ||
			strings.Contains(content, "formattedRetrieval") ||
			strings.Contains(content, "formatted_retrieval") {
			return augmentLegacyByteLength(content)
		}
	}
	return 0
}

func augmentLegacyCountToolResults(messages []any) int {
	count := 0
	for _, message := range messages {
		msg, ok := message.(map[string]any)
		if !ok {
			continue
		}
		if augmentLegacyMessageRole(msg) == "tool" || msg["toolResultNode"] != nil || msg["tool_result"] != nil || msg["tool_call_id"] != nil {
			count++
		}
	}
	return count
}

func augmentLegacyCountToolCalls(messages []any) int {
	count := 0
	for _, message := range messages {
		msg, ok := message.(map[string]any)
		if !ok {
			continue
		}
		if calls, ok := msg["tool_calls"].([]any); ok {
			count += len(calls)
			continue
		}
		if calls, ok := msg["toolCalls"].([]any); ok {
			count += len(calls)
			continue
		}
		if msg["toolUseNode"] != nil || msg["tool_use"] != nil {
			count++
		}
	}
	return count
}

func augmentLegacyMessageContentString(message map[string]any) string {
	if message == nil {
		return ""
	}
	content := message["content"]
	if content == nil {
		content = message["text"]
	}
	if content == nil {
		content = message["message"]
	}
	if content == nil {
		content = message["toolResultNode"]
	}
	if content == nil {
		content = message["tool_result"]
	}
	if content == nil {
		return ""
	}
	switch v := content.(type) {
	case string:
		return v
	case json.RawMessage:
		return string(v)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(b)
	}
}

func augmentLegacyByteLength(value any) int {
	if value == nil {
		return 0
	}
	switch v := value.(type) {
	case string:
		return len([]byte(v))
	case []byte:
		return len(v)
	case json.RawMessage:
		return len(v)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return len([]byte(fmt.Sprint(v)))
		}
		return len(b)
	}
}

func augmentLegacyRedactBytes(raw []byte) []byte {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return []byte{}
	}

	if json.Valid(trimmed) {
		var decoded any
		if err := json.Unmarshal(trimmed, &decoded); err == nil {
			redacted := augmentLegacyRedactValue(decoded)
			encoded, err := json.MarshalIndent(redacted, "", "  ")
			if err == nil {
				return encoded
			}
		}
	}

	return []byte(augmentLegacyRedactText(string(raw)))
}

func augmentLegacyRedactValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			if augmentLegacyIsSecretKey(key) {
				out[key] = "[REDACTED]"
				continue
			}
			out[key] = augmentLegacyRedactValue(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = augmentLegacyRedactValue(item)
		}
		return out
	case string:
		return augmentLegacyRedactText(v)
	default:
		return v
	}
}

func augmentLegacyRedactText(input string) string {
	out := logredact.RedactText(input)
	out = augmentLegacyOpenAITokenPattern.ReplaceAllString(out, "[REDACTED]")
	out = augmentLegacyGitHubTokenPattern.ReplaceAllString(out, "[REDACTED]")
	out = augmentLegacyBearerTokenPattern.ReplaceAllString(out, "Bearer [REDACTED]")
	return out
}

func augmentLegacyIsSecretKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	normalized = strings.NewReplacer("-", "", "_", "", " ", "").Replace(normalized)
	switch normalized {
	case "authorization", "cookie", "token", "session", "secret", "password", "passwd", "credential", "credentials", "privatekey", "apikey", "authorizationcode", "codeverifier", "accesstoken", "refreshtoken", "idtoken", "clientsecret":
		return true
	default:
		return strings.Contains(normalized, "authorization") ||
			strings.Contains(normalized, "cookie") ||
			strings.Contains(normalized, "token") ||
			strings.Contains(normalized, "secret") ||
			strings.Contains(normalized, "password") ||
			strings.Contains(normalized, "passwd") ||
			strings.Contains(normalized, "credential") ||
			strings.Contains(normalized, "privatekey") ||
			strings.Contains(normalized, "apikey")
	}
}

func augmentLegacySHA256(input string) string {
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}

func augmentLegacySHA256OrNil(input string) any {
	if strings.TrimSpace(input) == "" {
		return nil
	}
	return augmentLegacySHA256(input)
}

func augmentLegacyLatencyMillis(startedAt time.Time) float64 {
	if startedAt.IsZero() {
		return 0
	}
	return float64(time.Since(startedAt).Milliseconds())
}

func augmentLegacyCaptureHost(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	return strings.TrimSpace(c.Request.Host)
}

func augmentLegacyErrorClass(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimPrefix(fmt.Sprintf("%T", err), "*")
}

func errorMessageOrNilRedacted(err error) any {
	if err == nil {
		return nil
	}
	return strings.TrimSpace(augmentLegacyRedactText(err.Error()))
}

func nullIfEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}

func sanitizeCaptureComponent(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	return strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_").Replace(trimmed)
}

func appendJSONLine(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}

func augmentLegacyBuildFinalEnvelopeCaptureMeta(req augmentLegacyChatRequest, resolved augmentLegacyResolvedChatUserInput) augmentLegacyFinalEnvelopeCaptureMeta {
	return augmentLegacyFinalEnvelopeCaptureMeta{
		Enabled:             true,
		CaseID:              augmentLegacyCaptureCaseIDFromChatRequest(req),
		InformationRequest:  resolved.Text,
		ResolvedInputSource: resolved.Source,
		HasToolResults:      resolved.HasToolResults,
		CheckpointID:        req.Blobs.CheckpointID,
		AddedBlobsCount:     len(req.Blobs.AddedBlobs),
		DeletedBlobsCount:   len(req.Blobs.DeletedBlobs),
		DialogCount:         len(req.ChatHistory),
		DisableRetrieval:    req.DisableRetrieval,
		HasDisableRetrieval: req.DisableRetrieval || req.DisableRetrievalCamel != nil,
	}
}

func augmentLegacyCaptureMetaOrZero(meta *augmentLegacyFinalEnvelopeCaptureMeta) augmentLegacyFinalEnvelopeCaptureMeta {
	if meta == nil {
		return augmentLegacyFinalEnvelopeCaptureMeta{}
	}
	return *meta
}

func tryWarnCaptureSkip(message string, err ...error) {
	if len(err) > 0 && err[0] != nil {
		slog.Warn(message, "error", err[0])
		return
	}
	slog.Warn(message)
}
