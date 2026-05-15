package service

import (
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const CodexUsageClientProduct = "codex_gateway"

var (
	ErrCodexScopedAPIKeyRequired = infraerrors.Forbidden(
		"CODEX_SCOPED_API_KEY_REQUIRED",
		"a Codex-only API key bound to a Codex-enabled group is required",
	)
	ErrCodexKeyScopeMismatch = infraerrors.Forbidden(
		"CODEX_KEY_SCOPE_MISMATCH",
		"this Codex API key cannot access the requested route",
	)
)

type CodexKeyScopeDecision struct {
	Path    string
	Allowed bool
	Reason  string
}

var codexScopedAPIKeyAllowedPaths = map[string]struct{}{
	"/codex/v1/models":    {},
	"/codex/v1/responses": {},
}

func isCodexScopedAPIKeyAllowedPath(path string) bool {
	path = normalizeCodexScopedAPIKeyPath(path)
	switch {
	case path == "/codex/v1/models":
		return true
	case path == "/codex/v1/responses":
		return true
	case strings.HasPrefix(path, "/codex/v1/responses/"):
		return true
	case path == "/backend-api/codex/responses":
		return true
	case strings.HasPrefix(path, "/backend-api/codex/responses/"):
		return true
	default:
		return false
	}
}

func normalizeCodexScopedAPIKeyPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/"
	}
	if idx := strings.Index(path, "?"); idx >= 0 {
		path = path[:idx]
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

func EvaluateCodexScopedAPIKeyAccess(apiKey *APIKey, path string) CodexKeyScopeDecision {
	normalizedPath := normalizeCodexScopedAPIKeyPath(path)
	if !isCodexScopedAPIKeyAllowedPath(normalizedPath) {
		return CodexKeyScopeDecision{
			Path:    normalizedPath,
			Allowed: false,
			Reason:  infraerrors.Reason(ErrCodexKeyScopeMismatch),
		}
	}
	if apiKey == nil || !apiKey.IsCodexOnly() {
		return CodexKeyScopeDecision{
			Path:    normalizedPath,
			Allowed: false,
			Reason:  infraerrors.Reason(ErrCodexScopedAPIKeyRequired),
		}
	}
	if apiKey.GroupID == nil || apiKey.Group == nil || !apiKey.Group.CodexGatewayEntitled {
		return CodexKeyScopeDecision{
			Path:    normalizedPath,
			Allowed: false,
			Reason:  infraerrors.Reason(ErrCodexScopedAPIKeyRequired),
		}
	}
	return CodexKeyScopeDecision{
		Path:    normalizedPath,
		Allowed: true,
	}
}

func ValidateCodexScopedAPIKeyAccess(apiKey *APIKey, path string) error {
	decision := EvaluateCodexScopedAPIKeyAccess(apiKey, path)
	if decision.Allowed {
		return nil
	}
	switch decision.Reason {
	case infraerrors.Reason(ErrCodexScopedAPIKeyRequired):
		return ErrCodexScopedAPIKeyRequired
	default:
		return ErrCodexKeyScopeMismatch
	}
}
