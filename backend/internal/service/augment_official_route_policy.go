package service

import (
	"strings"
)

const (
	AugmentOfficialRoutePolicyVersion = "2026-05-08"
	AugmentClientProductZhumeng       = "zhumeng_augment"
)

type AugmentOfficialRouteOwner string

const (
	AugmentOfficialRouteOwnerLocalGateway  AugmentOfficialRouteOwner = "local_gateway"
	AugmentOfficialRouteOwnerOfficialCloud AugmentOfficialRouteOwner = "official_cloud"
	AugmentOfficialRouteOwnerExplicitPolicy AugmentOfficialRouteOwner = "explicit_policy"
	AugmentOfficialRouteOwnerUnknown       AugmentOfficialRouteOwner = "unknown"
)

type AugmentOfficialSessionStatus string

const (
	AugmentOfficialSessionStatusActive             AugmentOfficialSessionStatus = "active"
	AugmentOfficialSessionStatusMissing            AugmentOfficialSessionStatus = "official_session_missing"
	AugmentOfficialSessionStatusExpired            AugmentOfficialSessionStatus = "official_session_expired"
	AugmentOfficialSessionStatusRefreshFailed      AugmentOfficialSessionStatus = "official_session_refresh_failed"
	AugmentOfficialSessionStatusScopeInsufficient  AugmentOfficialSessionStatus = "official_scope_insufficient"
	AugmentOfficialSessionStatusTenantMismatch     AugmentOfficialSessionStatus = "tenant_session_mismatch"
	AugmentOfficialSessionStatusSourceMismatch     AugmentOfficialSessionStatus = "source_mismatch"
	AugmentOfficialSessionStatusFingerprintMismatch AugmentOfficialSessionStatus = "fingerprint_mismatch"
)

type AugmentOfficialRouteResult string

const (
	AugmentOfficialRouteResultAllowed              AugmentOfficialRouteResult = "allowed"
	AugmentOfficialRouteResultFailClosed           AugmentOfficialRouteResult = "fail_closed"
	AugmentOfficialRouteResultEmergencyOff         AugmentOfficialRouteResult = "emergency_off"
	AugmentOfficialRouteResultExplicitPolicy       AugmentOfficialRouteResult = "explicit_policy"
	AugmentOfficialRouteResultUnknownManualReview  AugmentOfficialRouteResult = "unknown_manual_review"
	AugmentOfficialRouteResultLocalGateway         AugmentOfficialRouteResult = "local_gateway"
)

type AugmentOfficialRouteCheckInput struct {
	Path                    string
	SessionStatus           AugmentOfficialSessionStatus
	SessionScopes           []string
	SessionSource           string
	SessionTenantOrigin     string
	SessionFingerprintPrefix string
	PresentedSource         string
	PresentedTenantOrigin   string
	PresentedFingerprint    string
	EmergencyOff            bool
}

type AugmentOfficialRouteDecision struct {
	Path                   string
	Owner                  AugmentOfficialRouteOwner
	RoutePolicyVersion     string
	ClientProduct          string
	OfficialRouteRequired  bool
	OfficialSessionStatus  AugmentOfficialSessionStatus
	OfficialRouteResult    AugmentOfficialRouteResult
	AllowLocalHandler      bool
}

func ClassifyAugmentOfficialRoute(path string) AugmentOfficialRouteOwner {
	path = normalizeAugmentOfficialRoutePath(path)
	switch path {
	case "/chat", "/chat-stream", "/get-models", "/usage/api/get-models":
		return AugmentOfficialRouteOwnerLocalGateway
	case "/batch-upload",
		"/checkpoint-blobs",
		"/find-missing",
		"/agents/codebase-retrieval",
		"/prompt-enhancer",
		"/instruction-stream",
		"/smart-paste-stream",
		"/generate-commit-message-stream",
		"/next_edit_loc",
		"/next-edit-stream",
		"/remote-agents/list",
		"/agents/list-remote-tools",
		"/get-implicit-external-sources",
		"/search-external-sources",
		"/context-canvas/list",
		"/save-chat",
		"/chat-history":
		return AugmentOfficialRouteOwnerOfficialCloud
	case "/notifications/read",
		"/notifications/mark-as-read",
		"/subscription-banner",
		"/report-error",
		"/report-feature-vector",
		"/client-metrics",
		"/record-session-events",
		"/record-request-events",
		"/record-user-events",
		"/record-preference-sample",
		"/client-completion-timelines",
		"/chat-feedback",
		"/completion-feedback",
		"/next-edit-feedback",
		"/resolve-completions",
		"/resolve-chat-input-completion",
		"/resolve-edit",
		"/resolve-instruction",
		"/resolve-next-edit",
		"/resolve-smart-paste":
		return AugmentOfficialRouteOwnerExplicitPolicy
	default:
		return AugmentOfficialRouteOwnerUnknown
	}
}

func EvaluateAugmentOfficialRoute(input AugmentOfficialRouteCheckInput) AugmentOfficialRouteDecision {
	owner := ClassifyAugmentOfficialRoute(input.Path)
	decision := AugmentOfficialRouteDecision{
		Path:                  normalizeAugmentOfficialRoutePath(input.Path),
		Owner:                 owner,
		RoutePolicyVersion:    AugmentOfficialRoutePolicyVersion,
		ClientProduct:         AugmentClientProductZhumeng,
		OfficialSessionStatus: input.SessionStatus,
	}

	switch owner {
	case AugmentOfficialRouteOwnerLocalGateway:
		decision.OfficialRouteResult = AugmentOfficialRouteResultLocalGateway
		decision.AllowLocalHandler = true
		return decision
	case AugmentOfficialRouteOwnerExplicitPolicy:
		decision.OfficialRouteResult = AugmentOfficialRouteResultExplicitPolicy
		decision.AllowLocalHandler = true
		return decision
	case AugmentOfficialRouteOwnerUnknown:
		decision.OfficialRouteResult = AugmentOfficialRouteResultUnknownManualReview
		decision.AllowLocalHandler = false
		return decision
	}

	decision.OfficialRouteRequired = true
	status := evaluateAugmentOfficialRouteStatus(input)
	decision.OfficialSessionStatus = status

	if input.EmergencyOff {
		decision.OfficialRouteResult = AugmentOfficialRouteResultEmergencyOff
		decision.AllowLocalHandler = true
		return decision
	}

	if status == AugmentOfficialSessionStatusActive {
		decision.OfficialRouteResult = AugmentOfficialRouteResultAllowed
		decision.AllowLocalHandler = true
		return decision
	}

	decision.OfficialRouteResult = AugmentOfficialRouteResultFailClosed
	decision.AllowLocalHandler = false
	return decision
}

func (d AugmentOfficialRouteDecision) TraceFields() []any {
	return []any{
		"client_product", d.ClientProduct,
		"route_policy_version", d.RoutePolicyVersion,
		"official_route_required", d.OfficialRouteRequired,
		"official_session_status", string(d.OfficialSessionStatus),
		"official_route_result", string(d.OfficialRouteResult),
	}
}

func evaluateAugmentOfficialRouteStatus(input AugmentOfficialRouteCheckInput) AugmentOfficialSessionStatus {
	status := input.SessionStatus
	if status == "" {
		status = AugmentOfficialSessionStatusMissing
	}
	if status != AugmentOfficialSessionStatusActive {
		return status
	}
	if !hasAugmentOfficialRequiredScope(input.SessionScopes) {
		return AugmentOfficialSessionStatusScopeInsufficient
	}
	if presented := strings.TrimSpace(input.PresentedSource); presented != "" && !strings.EqualFold(presented, strings.TrimSpace(input.SessionSource)) {
		return AugmentOfficialSessionStatusSourceMismatch
	}
	if presented := strings.TrimSpace(input.PresentedTenantOrigin); presented != "" && presented != strings.TrimSpace(input.SessionTenantOrigin) {
		return AugmentOfficialSessionStatusTenantMismatch
	}
	if presented := strings.TrimSpace(input.PresentedFingerprint); presented != "" {
		prefix := strings.TrimSpace(input.SessionFingerprintPrefix)
		if prefix == "" || !strings.HasPrefix(presented, prefix) {
			return AugmentOfficialSessionStatusFingerprintMismatch
		}
	}
	return AugmentOfficialSessionStatusActive
}

func hasAugmentOfficialRequiredScope(scopes []string) bool {
	for _, scope := range scopes {
		if strings.TrimSpace(scope) == "augment:session" {
			return true
		}
	}
	return false
}

func normalizeAugmentOfficialRoutePath(path string) string {
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
