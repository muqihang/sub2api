package service

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

const codexUpstreamMinVersion = "0.144.0"

// enforceCodexIdentityHeaders runs after all profile and account overrides.
// Messages compatibility intentionally omits originator, which remains a no-op.
func enforceCodexIdentityHeaders(headers http.Header) {
	if headers == nil || headers.Get("originator") == "" {
		return
	}
	originator, userAgent, ok := openai.PairCodexClientIdentity(headers.Get("user-agent"))
	if !ok {
		originator, userAgent = "codex_cli_rs", codexCLIUserAgent
	}
	headers.Set("originator", originator)
	headers.Set("user-agent", userAgent)
	if version := strings.TrimSpace(headers.Get("version")); version != "" && CompareVersions(version, codexUpstreamMinVersion) < 0 {
		headers.Set("version", codexCLIVersion)
	}
}
