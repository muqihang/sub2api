package service

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
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
	cfg         config.GatewayCodexCaptureConfig
	headerKeys  map[string]struct{}
	jsonKeys    map[string]struct{}
	hashKey     []byte
	redactValue string
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
	mode := strings.ToLower(strings.TrimSpace(r.cfg.HashMode))
	if mode == "sha256" {
		sum := sha256.Sum256([]byte(value))
		return fmt.Sprintf("sha256:%s chars=%d", hex.EncodeToString(sum[:]), len([]rune(value)))
	}
	if len(r.hashKey) < 32 {
		return fmt.Sprintf("hmac-sha256:unavailable chars=%d", len([]rune(value)))
	}
	mac := hmac.New(sha256.New, r.hashKey)
	_, _ = mac.Write([]byte(value))
	return fmt.Sprintf("hmac-sha256:%s chars=%d", hex.EncodeToString(mac.Sum(nil)), len([]rune(value)))
}

func (r *CodexGatewayCaptureRedactor) redactJSONValue(key string, value any) any {
	if r.shouldRedactJSONKey(key) {
		return r.redactValue
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
		return r.RedactString(typed)
	default:
		return value
	}
}

func (r *CodexGatewayCaptureRedactor) shouldRedactHeader(key string) bool {
	if r == nil || !r.cfg.Redact.Enabled {
		return false
	}
	_, ok := r.headerKeys[strings.ToLower(strings.TrimSpace(key))]
	return ok
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
