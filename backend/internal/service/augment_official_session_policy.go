package service

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"golang.org/x/net/idna"
)

const (
	augmentOfficialSessionSourceOfficialQuickLogin = "official_quick_login"
	augmentOfficialSessionSourceWukongQuickLogin   = "wukong_quick_login"
	augmentOfficialSessionSourceManualImport       = "manual_import"
)

var (
	ErrAugmentOfficialInvalidTenantURL      = infraerrors.BadRequest("invalid_tenant_url", "augment official tenant url is invalid")
	ErrAugmentOfficialTenantNotAllowlisted  = infraerrors.Forbidden("tenant_not_allowlisted", "augment official tenant origin is not allowlisted")
	ErrAugmentOfficialTenantSessionMismatch = infraerrors.Conflict(
		"tenant_session_mismatch",
		"augment official session source or scopes do not match the v1 policy",
	)
)

type AugmentOfficialTenantPolicy struct {
	AllowedOrigins []string
}

type AugmentOfficialSourcePolicy struct {
	AllowManualImport bool
}

func NormalizeAugmentOfficialOrigin(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil {
		return "", ErrAugmentOfficialInvalidTenantURL
	}

	if !strings.EqualFold(parsed.Scheme, "https") {
		return "", ErrAugmentOfficialInvalidTenantURL
	}
	if parsed.User != nil || strings.TrimSpace(parsed.Host) == "" {
		return "", ErrAugmentOfficialInvalidTenantURL
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", ErrAugmentOfficialInvalidTenantURL
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", ErrAugmentOfficialInvalidTenantURL
	}

	host, err := idna.Lookup.ToASCII(strings.ToLower(strings.TrimSpace(parsed.Hostname())))
	if err != nil {
		return "", ErrAugmentOfficialInvalidTenantURL
	}
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return "", ErrAugmentOfficialInvalidTenantURL
	}
	if isAugmentOfficialBlockedHost(host) {
		return "", ErrAugmentOfficialInvalidTenantURL
	}
	if ip := net.ParseIP(host); ip != nil {
		return "", ErrAugmentOfficialInvalidTenantURL
	}

	port := parsed.Port()
	if port != "" && port != "443" {
		return "", ErrAugmentOfficialInvalidTenantURL
	}

	return "https://" + host, nil
}

func (p AugmentOfficialTenantPolicy) ValidateOrigin(raw string) (string, error) {
	normalized, err := NormalizeAugmentOfficialOrigin(raw)
	if err != nil {
		return "", err
	}
	if len(p.AllowedOrigins) == 0 {
		return normalized, nil
	}

	allowset := make(map[string]struct{}, len(p.AllowedOrigins))
	for _, allowed := range p.AllowedOrigins {
		allowedOrigin, err := NormalizeAugmentOfficialOrigin(allowed)
		if err != nil {
			return "", fmt.Errorf("normalize augment official tenant allowlist: %w", err)
		}
		allowset[allowedOrigin] = struct{}{}
	}
	if _, ok := allowset[normalized]; !ok {
		return "", ErrAugmentOfficialTenantNotAllowlisted
	}
	return normalized, nil
}

func ValidateAugmentOfficialSource(source string, cfg AugmentOfficialSourcePolicy) error {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case augmentOfficialSessionSourceOfficialQuickLogin, augmentOfficialSessionSourceWukongQuickLogin:
		return nil
	case augmentOfficialSessionSourceManualImport:
		if cfg.AllowManualImport {
			return nil
		}
		return ErrAugmentOfficialTenantSessionMismatch
	default:
		return ErrAugmentOfficialTenantSessionMismatch
	}
}

func ValidateAugmentOfficialScopes(scopes []string) error {
	seen := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			return ErrAugmentOfficialTenantSessionMismatch
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
	}
	return nil
}

func isAugmentOfficialBlockedHost(host string) bool {
	return host == "localhost" || strings.HasSuffix(host, ".localhost")
}
