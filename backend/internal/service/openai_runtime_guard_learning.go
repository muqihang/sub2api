package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const (
	OpenAIRuntimeGuardBucketIllegalReasoningEffort       = "illegal_reasoning_effort"
	OpenAIRuntimeGuardBucketUnsupportedOAuthModelChannel = "unsupported_oauth_model_channel"
	OpenAIRuntimeGuardBucketImageGenerationDisabled      = "image_generation_disabled"
	OpenAIRuntimeGuardBucketContextOverflow              = "context_overflow"
	OpenAIRuntimeGuardBucketShapeTranscriptError         = "shape_transcript_error"
	OpenAIRuntimeGuardBucketTokenInvalidated             = "token_invalidated"
	OpenAIRuntimeGuardBucketInvalidEncryptedContent      = "invalid_encrypted_content"
	OpenAIRuntimeGuardBucketTemporaryNetwork             = "temporary_network"

	openAIRuntimeGuardLearnedBlockDefaultTTL   = 3 * time.Minute
	openAIRuntimeGuardLearnedBlockTemporaryTTL = 10 * time.Second
	openAIRuntimeGuardSanitizedMessageMaxBytes = 512
)

type OpenAIRuntimeGuardUpstreamErrorClassification struct {
	Bucket    string
	Category  string
	Metric    string
	Action    string
	Retryable bool
	Terminal  bool
	Scope     string
	TTL       time.Duration
	Status    int
	Message   string
	Code      string
	Param     string
	Type      string
}

type OpenAIRuntimeGuardLearnedBlockScope struct {
	AccountType       string
	Model             string
	Endpoint          string
	Profile           string
	CapabilityVersion string
}

type openAIRuntimeGuardLearnedBlockEntry struct {
	Classification OpenAIRuntimeGuardUpstreamErrorClassification
	ExpiresAt      time.Time
}

func ClassifyOpenAIRuntimeGuardUpstreamError(status int, headers http.Header, body []byte, message string) OpenAIRuntimeGuardUpstreamErrorClassification {
	_ = headers
	errorMessage, code, param, errType := openAIRuntimeGuardErrorFields(body, message)
	combined := strings.ToLower(strings.Join([]string{errorMessage, code, param, errType, string(body)}, " "))
	out := OpenAIRuntimeGuardUpstreamErrorClassification{
		Status:  status,
		Message: openAIRuntimeGuardSanitizeClassificationMessage(errorMessage),
		Code:    safeOpenAIRuntimeGuardMetadataValue(code),
		Param:   safeOpenAIRuntimeGuardMetadataValue(param),
		Type:    safeOpenAIRuntimeGuardMetadataValue(errType),
		Scope:   "account_type_model_endpoint_profile",
	}
	if strings.Contains(combined, "invalid_encrypted_content") ||
		(strings.Contains(combined, "encrypted content") && strings.Contains(combined, "could not be verified")) {
		return out.withRuntimeGuardBucket(OpenAIRuntimeGuardBucketInvalidEncryptedContent, "content.invalid_encrypted_content", "terminal", false, true, openAIRuntimeGuardLearnedBlockDefaultTTL)
	}
	if status == http.StatusUnauthorized && (strings.Contains(combined, "invalidated") || strings.Contains(combined, "revoked") || strings.Contains(combined, "needs relogin") || strings.Contains(combined, "relogin")) {
		return out.withRuntimeGuardBucket(OpenAIRuntimeGuardBucketTokenInvalidated, "auth.token_invalidated", "terminal", false, true, openAIRuntimeGuardLearnedBlockDefaultTTL)
	}
	if strings.Contains(param, "reasoning_effort") || strings.Contains(combined, "reasoning_effort") || strings.Contains(combined, "reasoning.effort") {
		if strings.Contains(combined, "unsupported") || strings.Contains(combined, "invalid") || strings.Contains(combined, "unknown") || strings.Contains(combined, "max") {
			return out.withRuntimeGuardBucket(OpenAIRuntimeGuardBucketIllegalReasoningEffort, "reasoning.illegal_effort", "terminal", false, true, openAIRuntimeGuardLearnedBlockDefaultTTL)
		}
	}
	if strings.Contains(combined, "image generation is not enabled") || strings.Contains(combined, "image generation disabled") ||
		(strings.Contains(combined, "image generation") && strings.Contains(combined, "not enabled")) ||
		(strings.Contains(combined, "wrong image") && strings.Contains(combined, "capability")) {
		return out.withRuntimeGuardBucket(OpenAIRuntimeGuardBucketImageGenerationDisabled, "capability.image_generation_disabled", "learn_block", false, false, openAIRuntimeGuardLearnedBlockDefaultTTL)
	}
	if strings.Contains(combined, "context length") || strings.Contains(combined, "context window") || strings.Contains(combined, "context too large") ||
		strings.Contains(combined, "maximum context") || strings.Contains(combined, "tokens/context too large") ||
		(strings.Contains(combined, "max tokens") && (strings.Contains(combined, "context") || strings.Contains(combined, "too large") || strings.Contains(combined, "exceeded"))) {
		return out.withRuntimeGuardBucket(OpenAIRuntimeGuardBucketContextOverflow, "context.upstream_overflow", "terminal", false, true, openAIRuntimeGuardLearnedBlockDefaultTTL)
	}
	if code == "invalid_value" && strings.Contains(combined, "input_text") && (strings.Contains(combined, "output_text") || strings.Contains(combined, "refusal")) {
		return out.withRuntimeGuardBucket(OpenAIRuntimeGuardBucketShapeTranscriptError, "shape.transcript_error", "observe", false, false, openAIRuntimeGuardLearnedBlockDefaultTTL)
	}
	if strings.Contains(param, "input.") && strings.Contains(param, ".content.") && (strings.Contains(errType, "invalid_request") || strings.Contains(combined, "invalid_request_error")) {
		return out.withRuntimeGuardBucket(OpenAIRuntimeGuardBucketShapeTranscriptError, "shape.transcript_error", "observe", false, false, openAIRuntimeGuardLearnedBlockDefaultTTL)
	}
	if strings.Contains(combined, "unsupported model") || strings.Contains(combined, "unsupported profile") || strings.Contains(combined, "unsupported channel") ||
		(strings.Contains(combined, "oauth") && strings.Contains(combined, "does not support")) ||
		(strings.Contains(combined, "chatgpt") && strings.Contains(combined, "does not support")) {
		return out.withRuntimeGuardBucket(OpenAIRuntimeGuardBucketUnsupportedOAuthModelChannel, "capability.unsupported_oauth_model_profile_channel", "learn_block", false, false, openAIRuntimeGuardLearnedBlockDefaultTTL)
	}
	if status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout || status == 529 ||
		strings.Contains(combined, "cloudflare") || strings.Contains(combined, "bad gateway") || strings.Contains(combined, "temporar") ||
		strings.Contains(combined, "timeout") || strings.Contains(combined, "network") || strings.Contains(combined, "<html") {
		return out.withRuntimeGuardBucket(OpenAIRuntimeGuardBucketTemporaryNetwork, "temporary.network", "observe", true, false, openAIRuntimeGuardLearnedBlockTemporaryTTL)
	}
	return out
}

func (c OpenAIRuntimeGuardUpstreamErrorClassification) withRuntimeGuardBucket(bucket, category, action string, retryable, terminal bool, ttl time.Duration) OpenAIRuntimeGuardUpstreamErrorClassification {
	c.Bucket = bucket
	c.Category = category
	c.Metric = "openai_runtime_guard.upstream." + bucket
	c.Action = action
	c.Retryable = retryable
	c.Terminal = terminal
	c.TTL = ttl
	return c
}

func openAIRuntimeGuardErrorFields(body []byte, fallback string) (message, code, param, errType string) {
	if len(body) > 0 {
		for _, path := range []string{"error.message", "message", "error"} {
			if value := strings.TrimSpace(gjson.GetBytes(body, path).String()); value != "" {
				message = value
				break
			}
		}
		code = strings.TrimSpace(gjson.GetBytes(body, "error.code").String())
		param = strings.TrimSpace(gjson.GetBytes(body, "error.param").String())
		errType = strings.TrimSpace(gjson.GetBytes(body, "error.type").String())
		if message != "" && strings.HasPrefix(message, "{") && json.Valid([]byte(message)) {
			wrappedMessage, wrappedCode, wrappedParam, wrappedType := openAIRuntimeGuardErrorFields([]byte(message), "")
			message = firstNonBlankString(wrappedMessage, message)
			code = firstNonBlankString(wrappedCode, code)
			param = firstNonBlankString(wrappedParam, param)
			errType = firstNonBlankString(wrappedType, errType)
		}
	}
	if strings.TrimSpace(fallback) != "" {
		if message == "" {
			message = fallback
		} else {
			message = message + " " + fallback
		}
	}
	return strings.TrimSpace(message), strings.TrimSpace(code), strings.TrimSpace(param), strings.TrimSpace(errType)
}

func openAIRuntimeGuardSanitizeClassificationMessage(message string) string {
	message = sanitizeOpenAIRuntimeGuardMessage(message)
	return truncateString(message, openAIRuntimeGuardSanitizedMessageMaxBytes)
}

func sanitizeOpenAIRuntimeGuardMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	message = redactOpenAIRuntimeGuardOpaquePayloadMarkers(message)
	return sanitizeUpstreamErrorMessage(message)
}

func redactOpenAIRuntimeGuardOpaquePayloadMarkers(value string) string {
	if strings.TrimSpace(value) == "" {
		return strings.TrimSpace(value)
	}
	var decoded any
	if json.Unmarshal([]byte(value), &decoded) == nil {
		decoded = redactOpenAIRuntimeGuardOpaqueJSON(decoded)
		if raw, err := json.Marshal(decoded); err == nil {
			return string(raw)
		}
	}
	return redactOpenAIRuntimeGuardOpaqueFallback(value)
}

func redactOpenAIRuntimeGuardOpaqueJSON(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			lower := strings.ToLower(strings.TrimSpace(key))
			if openAIRuntimeGuardOpaqueJSONKey(lower) {
				typed[key] = "[redacted]"
				continue
			}
			if isSensitiveKey(lower) {
				typed[key] = "[redacted]"
				continue
			}
			typed[key] = redactOpenAIRuntimeGuardOpaqueJSON(child)
		}
		return typed
	case []any:
		for i, child := range typed {
			typed[i] = redactOpenAIRuntimeGuardOpaqueJSON(child)
		}
		return typed
	case string:
		return redactOpenAIRuntimeGuardOpaqueString(typed)
	}
	return value
}

func redactOpenAIRuntimeGuardOpaqueString(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed != "" && (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) {
		var decoded any
		if json.Unmarshal([]byte(trimmed), &decoded) == nil {
			decoded = redactOpenAIRuntimeGuardOpaqueJSON(decoded)
			if raw, err := json.Marshal(decoded); err == nil {
				return string(raw)
			}
		}
	}
	out := redactOpenAIRuntimeGuardOpaqueFallback(value)
	return sanitizeUpstreamErrorMessage(out)
}

func openAIRuntimeGuardOpaqueJSONKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "encrypted_content", "prompt", "input", "messages", "instructions":
		return true
	default:
		return false
	}
}

var openAIRuntimeGuardOpaqueFallbackFieldRegex = regexp.MustCompile(`(?i)(\\?")(encrypted_content|prompt|input|messages|instructions|access_token|refresh_token|id_token|api_key|apikey|x-api-key|client_secret|authorization|password|token)(\\?")\s*:\s*`)

func redactOpenAIRuntimeGuardOpaqueFallback(value string) string {
	out := value
	pos := 0
	for {
		loc := openAIRuntimeGuardOpaqueFallbackFieldRegex.FindStringSubmatchIndex(out[pos:])
		if loc == nil {
			break
		}
		prefixEnd := pos + loc[1]
		quotePrefix := out[pos+loc[2] : pos+loc[3]]
		key := out[pos+loc[4] : pos+loc[5]]
		escapedQuotes := strings.HasPrefix(quotePrefix, `\"`)
		end := openAIRuntimeGuardJSONLikeValueEnd(out, prefixEnd, escapedQuotes)
		replacement := `"[redacted]"`
		if !openAIRuntimeGuardOpaqueJSONKey(key) && isSensitiveKey(key) {
			replacement = `"***"`
		}
		if escapedQuotes {
			replacement = `\` + replacement[:1] + replacement[1:len(replacement)-1] + `\"`
		}
		out = out[:prefixEnd] + replacement + out[end:]
		pos = prefixEnd + len(replacement)
		if pos > len(out) {
			pos = len(out)
		}
	}
	return out
}

func openAIRuntimeGuardJSONLikeValueEnd(value string, start int, escapedQuotes bool) int {
	for start < len(value) && (value[start] == ' ' || value[start] == '\t' || value[start] == '\r' || value[start] == '\n') {
		start++
	}
	if start >= len(value) {
		return start
	}
	if start+1 < len(value) && value[start] == '\\' && value[start+1] == '"' {
		escapedQuotes = true
		for i := start + 2; i < len(value); i++ {
			if value[i] == '"' && openAIRuntimeGuardEscapedJSONQuoteDelimiter(value, i) {
				return i + 1
			}
		}
		return len(value)
	}
	switch value[start] {
	case '"':
		for i := start + 1; i < len(value); i++ {
			if value[i] == '\\' {
				i++
				continue
			}
			if value[i] == '"' {
				return i + 1
			}
		}
		return len(value)
	case '{', '[':
		stack := []byte{value[start]}
		inString := false
		for i := start + 1; i < len(value); i++ {
			ch := value[i]
			if inString {
				if escapedQuotes {
					if ch == '"' && openAIRuntimeGuardEscapedJSONQuoteDelimiter(value, i) {
						inString = false
					}
					continue
				}
				if ch == '\\' {
					i++
					continue
				}
				if ch == '"' {
					inString = false
				}
				continue
			}
			switch ch {
			case '"':
				if !escapedQuotes || openAIRuntimeGuardEscapedJSONQuoteDelimiter(value, i) {
					inString = true
				}
			case '{', '[':
				stack = append(stack, ch)
			case '}', ']':
				if len(stack) == 0 {
					return i
				}
				open := stack[len(stack)-1]
				if (open == '{' && ch == '}') || (open == '[' && ch == ']') {
					stack = stack[:len(stack)-1]
					if len(stack) == 0 {
						return i + 1
					}
				}
			}
		}
		return len(value)
	default:
		for i := start; i < len(value); i++ {
			switch value[i] {
			case ',', '}', ']':
				return i
			}
		}
		return len(value)
	}
}

func openAIRuntimeGuardEscapedJSONQuoteDelimiter(value string, quote int) bool {
	backslashes := 0
	for i := quote - 1; i >= 0 && value[i] == '\\'; i-- {
		backslashes++
	}
	return backslashes%4 == 1
}

func (s *OpenAIGatewayService) RecordOpenAIRuntimeGuardLearnedBlock(scope OpenAIRuntimeGuardLearnedBlockScope, classification OpenAIRuntimeGuardUpstreamErrorClassification) bool {
	if s == nil || !openAIRuntimeGuardClassificationCanLearnBlock(classification) {
		return false
	}
	key := openAIRuntimeGuardLearnedBlockKey(scope, classification.Bucket)
	if key == "" {
		return false
	}
	classification.Message = openAIRuntimeGuardSanitizeClassificationMessage(classification.Message)
	s.openaiRuntimeGuardLearnedBlocks.Store(key, openAIRuntimeGuardLearnedBlockEntry{Classification: classification, ExpiresAt: time.Now().Add(classification.TTL)})
	return true
}

func openAIRuntimeGuardClassificationCanLearnBlock(classification OpenAIRuntimeGuardUpstreamErrorClassification) bool {
	if classification.Bucket == "" || classification.Retryable || classification.Terminal || classification.TTL <= 0 {
		return false
	}
	switch classification.Bucket {
	case OpenAIRuntimeGuardBucketUnsupportedOAuthModelChannel, OpenAIRuntimeGuardBucketImageGenerationDisabled:
		return true
	default:
		// Shape, transcript, context, encrypted-content, auth, and temporary
		// failures are request- or account-state-specific. They are classified for
		// ops visibility but must not poison all subsequent requests for the same
		// model+endpoint.
		return false
	}
}

func (s *OpenAIGatewayService) IsOpenAIRuntimeGuardLearnedBlocked(scope OpenAIRuntimeGuardLearnedBlockScope) (OpenAIRuntimeGuardUpstreamErrorClassification, bool) {
	if s == nil {
		return OpenAIRuntimeGuardUpstreamErrorClassification{}, false
	}
	prefix := openAIRuntimeGuardLearnedBlockScopePrefix(scope)
	if prefix == "" {
		return OpenAIRuntimeGuardUpstreamErrorClassification{}, false
	}
	now := time.Now()
	var out OpenAIRuntimeGuardUpstreamErrorClassification
	var blocked bool
	s.openaiRuntimeGuardLearnedBlocks.Range(func(key, value any) bool {
		keyString, _ := key.(string)
		if !strings.HasPrefix(keyString, prefix+"|") {
			return true
		}
		entry, ok := value.(openAIRuntimeGuardLearnedBlockEntry)
		if !ok || entry.ExpiresAt.IsZero() || !now.Before(entry.ExpiresAt) {
			s.openaiRuntimeGuardLearnedBlocks.Delete(key)
			return true
		}
		out = entry.Classification
		blocked = true
		return false
	})
	return out, blocked
}

func openAIRuntimeGuardLearnedBlockKey(scope OpenAIRuntimeGuardLearnedBlockScope, bucket string) string {
	prefix := openAIRuntimeGuardLearnedBlockScopePrefix(scope)
	bucket = strings.ToLower(strings.TrimSpace(bucket))
	if prefix == "" || bucket == "" {
		return ""
	}
	return prefix + "|bucket=" + bucket
}

func openAIRuntimeGuardLearnedBlockScopePrefix(scope OpenAIRuntimeGuardLearnedBlockScope) string {
	accountType := strings.ToLower(strings.TrimSpace(scope.AccountType))
	model := strings.ToLower(strings.TrimSpace(scope.Model))
	endpoint := strings.ToLower(strings.TrimSpace(scope.Endpoint))
	profile := strings.ToLower(strings.TrimSpace(scope.Profile))
	capabilityVersion := strings.ToLower(strings.TrimSpace(scope.CapabilityVersion))
	if accountType == "" || model == "" || endpoint == "" {
		return ""
	}
	return fmt.Sprintf("account_type=%s|model=%s|endpoint=%s|profile=%s|capability_version=%s", accountType, model, endpoint, profile, capabilityVersion)
}

func openAIRuntimeGuardLearnedBlockScopeForAccount(account *Account, model, endpoint string) OpenAIRuntimeGuardLearnedBlockScope {
	var accountType, profile, capabilityVersion string
	if account != nil {
		accountType = account.Type
		profile = firstNonBlankString(
			account.GetExtraString("openai_gateway_profile_id"),
			account.GetExtraString("openai_runtime_guard_profile"),
			account.GetExtraString("profile"),
		)
		capabilityVersion = openAIRuntimeGuardCapabilityVersionForAccount(account)
	}
	return OpenAIRuntimeGuardLearnedBlockScope{AccountType: accountType, Model: model, Endpoint: endpoint, Profile: profile, CapabilityVersion: capabilityVersion}
}

func openAIRuntimeGuardCapabilityVersionForAccount(account *Account) string {
	if account == nil {
		return ""
	}
	for _, key := range []string{"openai_runtime_guard_capability_version", "openai_capability_version", "credential_version", "credentials_version", "token_version", "updated_at"} {
		if value := strings.TrimSpace(account.GetExtraString(key)); value != "" {
			return value
		}
	}
	for _, key := range []string{"openai_runtime_guard_capability_version", "openai_capability_version", "credential_version", "credentials_version", "token_version", "_token_version", "updated_at"} {
		if value := strings.TrimSpace(account.GetCredential(key)); value != "" {
			return value
		}
	}
	if !account.UpdatedAt.IsZero() {
		return account.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	return ""
}

func openAIRuntimeGuardEndpointFromModelHint(hint string) string {
	hint = strings.ToLower(strings.TrimSpace(hint))
	switch {
	case strings.Contains(hint, "image"):
		return "images"
	case strings.Contains(hint, "chat"):
		return "chat_completions"
	case strings.Contains(hint, "message"):
		return "messages"
	default:
		return "responses"
	}
}

func (s *OpenAIGatewayService) blockOpenAIRuntimeGuardLearnedRequest(c *gin.Context, account *Account, model, endpoint string) *OpenAIRuntimeGuardBlockedError {
	classification, ok := s.IsOpenAIRuntimeGuardLearnedBlocked(openAIRuntimeGuardLearnedBlockScopeForAccount(account, model, endpoint))
	if !ok {
		return nil
	}
	decision := openAIReasoningEffortGuardDecision{
		Action:   "learned_block",
		Blocked:  true,
		Present:  true,
		Status:   http.StatusBadRequest,
		Path:     "model",
		Category: classification.Category,
		Metric:   classification.Metric,
	}
	if decision.Category == "" {
		decision.Category = "runtime_guard.learned_block"
	}
	if decision.Metric == "" {
		decision.Metric = "openai_runtime_guard.learned_block"
	}
	if c != nil {
		c.Set(OpenAIRuntimeGuardMetadataKey, OpenAIRuntimeGuardMetadata{
			Action:   decision.Action,
			Category: decision.Category,
			Metric:   decision.Metric,
			Field:    "model",
			Path:     "model",
			Status:   decision.Status,
		})
		MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalPolicyDenied)
		c.Data(decision.Status, "application/json; charset=utf-8", openAIRuntimeGuardLearnedBlockPayload(decision.Category))
	}
	return &OpenAIRuntimeGuardBlockedError{StatusCode: decision.Status, Payload: openAIRuntimeGuardLearnedBlockPayload(decision.Category), Decision: decision}
}

func openAIRuntimeGuardLearnedBlockPayload(categoryOverride ...string) []byte {
	runtimeGuardCategory := ""
	if len(categoryOverride) > 0 {
		runtimeGuardCategory = strings.TrimSpace(categoryOverride[0])
	}
	payload, err := json.Marshal(map[string]any{
		"error": map[string]any{
			"type":                   "invalid_request_error",
			"code":                   string(OpenAIRuntimeGuardErrorCodeLocalPolicyBlock),
			"category":               openAIRuntimeGuardCapabilityCategoryLocalPolicyBlock,
			"runtime_guard_category": runtimeGuardCategory,
			"message":                "OpenAI OAuth request is temporarily blocked by runtime guard learning",
			"param":                  "model",
		},
	})
	if err != nil {
		return []byte(`{"error":{"type":"invalid_request_error","code":"local_policy_block","category":"capability.local_policy_block","message":"OpenAI OAuth request is temporarily blocked by runtime guard learning","param":"model"}}`)
	}
	return payload
}
