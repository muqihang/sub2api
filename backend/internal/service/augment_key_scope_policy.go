package service

import (
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrAugmentEntitlementRequired = infraerrors.Forbidden(
		"AUGMENT_ENTITLEMENT_REQUIRED",
		"an Augment-enabled group is required to use Augment quick login",
	)
	ErrAugmentScopedAPIKeyRequired = infraerrors.Forbidden(
		"AUGMENT_SCOPED_API_KEY_REQUIRED",
		"an Augment-only API key bound to an Augment-enabled group is required",
	)
	ErrAugmentScopedAPIKeyAmbiguous = infraerrors.Forbidden(
		"AUGMENT_SCOPED_API_KEY_AMBIGUOUS",
		"multiple Augment-only API keys are active; choose a single Augment key before using Augment quick login",
	)
	ErrAugmentKeyScopeMismatch = infraerrors.Forbidden(
		"AUGMENT_KEY_SCOPE_MISMATCH",
		"this Augment API key cannot access the requested route",
	)
)

type AugmentKeyScopeDecision struct {
	Path    string
	Allowed bool
	Reason  string
}

var augmentScopedAPIKeyAllowedPaths = map[string]struct{}{
	"/api/v1/plugin/augment/summary":         {},
	"/api/v1/plugin/augment/compat/metadata": {},
	"/api/v1/plugin/augment/session/refresh": {},
	"/usage/api/balance":                     {},
	"/usage/api/get-models":                  {},
	"/usage/api/getLoginToken":               {},
	"/get-models":                            {},
	"/chat":                                  {},
	"/chat-stream":                           {},
	"/batch-upload":                          {},
	"/checkpoint-blobs":                      {},
	"/find-missing":                          {},
	"/agents/codebase-retrieval":             {},
	"/prompt-enhancer":                       {},
	"/instruction-stream":                    {},
	"/smart-paste-stream":                    {},
	"/generate-commit-message-stream":        {},
	"/next_edit_loc":                         {},
	"/next-edit-stream":                      {},
	"/remote-agents/list":                    {},
	"/agents/list-remote-tools":              {},
	"/get-implicit-external-sources":         {},
	"/search-external-sources":               {},
	"/context-canvas/list":                   {},
	"/save-chat":                             {},
	"/chat-history":                          {},
	"/notifications/read":                    {},
	"/notifications/mark-as-read":            {},
	"/subscription-banner":                   {},
	"/report-error":                          {},
	"/report-feature-vector":                 {},
	"/client-metrics":                        {},
	"/record-session-events":                 {},
	"/record-request-events":                 {},
	"/record-user-events":                    {},
	"/record-preference-sample":              {},
	"/client-completion-timelines":           {},
	"/chat-feedback":                         {},
	"/completion-feedback":                   {},
	"/next-edit-feedback":                    {},
	"/resolve-completions":                   {},
	"/resolve-chat-input-completion":         {},
	"/resolve-edit":                          {},
	"/resolve-instruction":                   {},
	"/resolve-next-edit":                     {},
	"/resolve-smart-paste":                   {},
}

func normalizeAugmentScopedAPIKeyPath(path string) string {
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

func EvaluateAugmentScopedAPIKeyAccess(apiKey *APIKey, path string) AugmentKeyScopeDecision {
	normalizedPath := normalizeAugmentScopedAPIKeyPath(path)
	if _, ok := augmentScopedAPIKeyAllowedPaths[normalizedPath]; !ok {
		return AugmentKeyScopeDecision{
			Path:    normalizedPath,
			Allowed: false,
			Reason:  infraerrors.Reason(ErrAugmentKeyScopeMismatch),
		}
	}
	if apiKey == nil || !apiKey.IsAugmentOnly() {
		return AugmentKeyScopeDecision{
			Path:    normalizedPath,
			Allowed: false,
			Reason:  infraerrors.Reason(ErrAugmentScopedAPIKeyRequired),
		}
	}
	if apiKey.GroupID == nil || apiKey.Group == nil || !apiKey.Group.AugmentGatewayEntitled {
		return AugmentKeyScopeDecision{
			Path:    normalizedPath,
			Allowed: false,
			Reason:  infraerrors.Reason(ErrAugmentScopedAPIKeyRequired),
		}
	}
	return AugmentKeyScopeDecision{
		Path:    normalizedPath,
		Allowed: true,
	}
}

func ValidateAugmentScopedAPIKeyAccess(apiKey *APIKey, path string) error {
	decision := EvaluateAugmentScopedAPIKeyAccess(apiKey, path)
	if decision.Allowed {
		return nil
	}
	switch decision.Reason {
	case infraerrors.Reason(ErrAugmentScopedAPIKeyRequired):
		return ErrAugmentScopedAPIKeyRequired
	default:
		return ErrAugmentKeyScopeMismatch
	}
}
