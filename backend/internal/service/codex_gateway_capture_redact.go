package service

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

var codexGatewayCaptureBearerPattern = regexp.MustCompile(`(?i)\bBearer\s+[-._~+/=A-Za-z0-9]+`)

type CodexGatewayCaptureRedactor struct {
	cfg                config.GatewayCodexCaptureConfig
	headerKeys         map[string]struct{}
	jsonKeys           map[string]struct{}
	hashKey            []byte
	correlationHashKey []byte
	redactValue        string
}

func NewCodexGatewayCaptureRedactor(cfg config.GatewayCodexCaptureConfig) *CodexGatewayCaptureRedactor {
	cfg = NormalizeCodexGatewayCaptureConfig(cfg)
	r := &CodexGatewayCaptureRedactor{
		cfg:         cfg,
		headerKeys:  make(map[string]struct{}, len(cfg.Redact.HeaderNames)),
		jsonKeys:    make(map[string]struct{}, len(cfg.Redact.JSONKeys)),
		redactValue: "[REDACTED]",
	}
	for _, name := range cfg.Redact.HeaderNames {
		r.headerKeys[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	for _, key := range cfg.Redact.JSONKeys {
		r.jsonKeys[strings.ToLower(strings.TrimSpace(key))] = struct{}{}
	}
	r.hashKey = codexGatewayCaptureLoadOrCreateHashKey(cfg.HashKeyFile)
	if strings.TrimSpace(cfg.CorrelationHashKeyFile) != "" {
		r.correlationHashKey = codexGatewayCaptureLoadOrCreateHashKey(cfg.CorrelationHashKeyFile)
	}
	return r
}

func (r *CodexGatewayCaptureRedactor) RedactHeaders(headers http.Header) http.Header {
	out := http.Header{}
	if headers == nil {
		return out
	}
	for key, values := range headers {
		if r.shouldRedactHeader(key) {
			out[key] = []string{r.redactValue}
			continue
		}
		cloned := make([]string, 0, len(values))
		for _, value := range values {
			if r.shouldHashHeader(key) {
				cloned = append(cloned, r.CorrelationHash("header:"+strings.ToLower(strings.TrimSpace(key)), value))
				continue
			}
			cloned = append(cloned, r.RedactString(value))
		}
		out[key] = cloned
	}
	return out
}

func (r *CodexGatewayCaptureRedactor) RedactJSONValue(value any) any {
	return r.redactJSONValue("", value)
}

func (r *CodexGatewayCaptureRedactor) RedactString(value string) string {
	return codexGatewayCaptureBearerPattern.ReplaceAllString(value, "Bearer "+r.redactValue)
}

func (r *CodexGatewayCaptureRedactor) HashText(value string) string {
	return r.hashTextWithKey(r.hashKey, value)
}

func (r *CodexGatewayCaptureRedactor) CorrelationHash(kind, value string) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "unknown"
	}
	if len(r.correlationHashKey) < 32 {
		return r.HashText(kind + "\x00" + value)
	}
	return r.hashTextWithKey(r.correlationHashKey, kind+"\x00"+value)
}

func (r *CodexGatewayCaptureRedactor) hashTextWithKey(key []byte, value string) string {
	mode := strings.ToLower(strings.TrimSpace(r.cfg.HashMode))
	if mode == "sha256" {
		sum := sha256.Sum256([]byte(value))
		return fmt.Sprintf("sha256:%s chars=%d", hex.EncodeToString(sum[:]), len([]rune(value)))
	}
	if len(key) < 32 {
		return fmt.Sprintf("hmac-sha256:unavailable chars=%d", len([]rune(value)))
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(value))
	return fmt.Sprintf("hmac-sha256:%s chars=%d", hex.EncodeToString(mac.Sum(nil)), len([]rune(value)))
}

func (r *CodexGatewayCaptureRedactor) redactJSONValue(key string, value any) any {
	if r.shouldRedactJSONKey(key) {
		return r.redactValue
	}
	if r.shouldSummarizeRawJSONKey(key) {
		return r.summarizeRawJSONValue(key, value)
	}
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for childKey, childValue := range typed {
			out[childKey] = r.redactJSONValue(childKey, childValue)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, r.redactJSONValue("", item))
		}
		return out
	case string:
		if parsed, ok := r.redactStructuredJSONString(key, typed); ok {
			return parsed
		}
		return r.RedactString(typed)
	default:
		return value
	}
}

func (r *CodexGatewayCaptureRedactor) redactStructuredJSONString(key, value string) (any, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, false
	}
	first := trimmed[0]
	if first != '{' && first != '[' {
		return nil, false
	}
	var parsed any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return nil, false
	}
	if !r.rawJSONValueNeedsSummary(key, parsed) {
		return nil, false
	}
	return r.redactJSONValue(key, parsed), true
}

func (r *CodexGatewayCaptureRedactor) rawJSONValueNeedsSummary(key string, value any) bool {
	if r.shouldSummarizeRawJSONKey(key) {
		return true
	}
	switch typed := value.(type) {
	case map[string]any:
		for childKey, childValue := range typed {
			if r.rawJSONValueNeedsSummary(childKey, childValue) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if r.rawJSONValueNeedsSummary("", item) {
				return true
			}
		}
	}
	return false
}

func (r *CodexGatewayCaptureRedactor) shouldSummarizeRawJSONKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return false
	}
	return strings.Contains(key, "screenshot") ||
		strings.Contains(key, "image_url") ||
		strings.Contains(key, "image_base64") ||
		strings.Contains(key, "base64") ||
		strings.Contains(key, "accessibility_tree") ||
		strings.Contains(key, "accessibilitytree") ||
		strings.Contains(key, "accessibility_snapshot") ||
		strings.Contains(key, "accessibilitysnapshot") ||
		strings.Contains(key, "ax_tree") ||
		strings.Contains(key, "ui_tree") ||
		strings.Contains(key, "page_tree") ||
		strings.Contains(key, "pagetree") ||
		strings.Contains(key, "browser_tree") ||
		strings.Contains(key, "browsertree") ||
		strings.Contains(key, "dom_snapshot") ||
		strings.Contains(key, "domsnapshot") ||
		strings.Contains(key, "ui_snapshot") ||
		strings.Contains(key, "uisnapshot") ||
		strings.Contains(key, "page_source") ||
		strings.Contains(key, "page_content")
}

func (r *CodexGatewayCaptureRedactor) summarizeRawJSONValue(key string, value any) any {
	raw, err := json.Marshal(value)
	if err != nil {
		return map[string]any{
			"redacted": true,
			"field":    strings.TrimSpace(key),
		}
	}
	return map[string]any{
		"redacted": true,
		"field":    strings.TrimSpace(key),
		"hash":     r.HashText(string(raw)),
	}
}

func (r *CodexGatewayCaptureRedactor) shouldRedactHeader(key string) bool {
	if r == nil || !r.cfg.Redact.Enabled {
		return false
	}
	_, ok := r.headerKeys[strings.ToLower(strings.TrimSpace(key))]
	return ok
}

func (r *CodexGatewayCaptureRedactor) shouldHashHeader(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	return strings.Contains(normalized, "request-id") ||
		strings.Contains(normalized, "response-id") ||
		strings.Contains(normalized, "trace-id") ||
		strings.Contains(normalized, "correlation-id")
}

func (r *CodexGatewayCaptureRedactor) shouldRedactJSONKey(key string) bool {
	if r == nil || !r.cfg.Redact.Enabled || strings.TrimSpace(key) == "" {
		return false
	}
	normalized := strings.ToLower(strings.TrimSpace(key))
	if _, ok := r.jsonKeys[normalized]; ok {
		return true
	}
	return strings.Contains(normalized, "api_key") ||
		strings.Contains(normalized, "apikey") ||
		strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "token") ||
		strings.Contains(normalized, "password")
}

func codexGatewayCaptureLoadOrCreateHashKey(path string) []byte {
	path = strings.TrimSpace(path)
	if path != "" {
		if key, err := os.ReadFile(path); err == nil && len(key) >= 32 {
			_ = os.Chmod(path, 0o600)
			return key
		}
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil
	}
	if path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err == nil {
			_ = os.WriteFile(path, key, 0o600)
		}
	}
	return key
}
