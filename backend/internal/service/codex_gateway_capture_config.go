package service

import (
	"fmt"
	"os"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

const codexGatewayCaptureRawUnlockValue = "I_UNDERSTAND_THIS_WRITES_LOCAL_RAW_PROTOCOL_PAYLOADS"

func NormalizeCodexGatewayCaptureConfig(in config.GatewayCodexCaptureConfig) config.GatewayCodexCaptureConfig {
	out := in
	out.Level = strings.ToLower(strings.TrimSpace(out.Level))
	if out.Level == "" {
		out.Level = "summary"
	}
	if strings.TrimSpace(out.BaseDir) == "" {
		out.BaseDir = "data/codex-gateway-captures"
	}
	if out.MaxTraceBytes <= 0 {
		out.MaxTraceBytes = 64 * 1024 * 1024
	}
	if out.MaxBodyBytes <= 0 {
		out.MaxBodyBytes = 2 * 1024 * 1024
	}
	if out.MaxEventBytes <= 0 {
		out.MaxEventBytes = 128 * 1024
	}
	if out.AsyncQueueSize <= 0 {
		out.AsyncQueueSize = 4096
	}
	if strings.TrimSpace(out.HashMode) == "" {
		out.HashMode = "hmac-sha256"
	} else {
		out.HashMode = strings.ToLower(strings.TrimSpace(out.HashMode))
	}
	if strings.TrimSpace(out.HashKeyFile) == "" {
		out.HashKeyFile = strings.TrimRight(out.BaseDir, "/") + "/.capture-hmac-key"
	}
	if strings.TrimSpace(out.RequireRawPayloadsUnlockEnv) == "" {
		out.RequireRawPayloadsUnlockEnv = "SUB2API_CODEX_CAPTURE_RAW_UNLOCK=" + codexGatewayCaptureRawUnlockValue
	}
	if len(out.Redact.HeaderNames) == 0 {
		out.Redact.HeaderNames = []string{
			"Authorization",
			"Cookie",
			"Set-Cookie",
			"X-Api-Key",
			"Api-Key",
			"X-OpenAI-Api-Key",
			"Anthropic-Api-Key",
		}
	}
	if len(out.Redact.JSONKeys) == 0 {
		out.Redact.JSONKeys = []string{
			"authorization",
			"cookie",
			"set-cookie",
			"x-api-key",
			"api-key",
			"api_key",
			"apikey",
			"token",
			"access_token",
			"refresh_token",
			"password",
			"secret",
		}
	}
	if !in.Redact.Enabled && len(in.Redact.HeaderNames) == 0 && len(in.Redact.JSONKeys) == 0 {
		out.Redact.Enabled = true
	}
	if !in.CaptureErrorsAlways {
		out.CaptureErrorsAlways = true
	}
	return out
}

func ValidateCodexGatewayCaptureRuntime(cfg config.GatewayCodexCaptureConfig, serverMode string) error {
	cfg = NormalizeCodexGatewayCaptureConfig(cfg)
	if !cfg.Enabled || !cfg.RawPayloads {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(serverMode), "production") {
		return fmt.Errorf("gateway.codex.capture.raw_payloads is not allowed in production server mode")
	}
	name, value, ok := strings.Cut(cfg.RequireRawPayloadsUnlockEnv, "=")
	if !ok || strings.TrimSpace(name) == "" || strings.TrimSpace(value) == "" {
		return fmt.Errorf("gateway.codex.capture.require_raw_payloads_unlock_env must be NAME=VALUE")
	}
	if os.Getenv(strings.TrimSpace(name)) != strings.TrimSpace(value) {
		return fmt.Errorf("gateway.codex.capture.raw_payloads requires %s", cfg.RequireRawPayloadsUnlockEnv)
	}
	return nil
}
