package service

import (
	"net/url"
	"os"
	"strings"
	"unicode"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	augmentDefaultIDETargetID      = "vscode"
	augmentDefaultIDEEplinkHost    = "Augment.vscode-augment"
	augmentDefaultIDEQuickAuthPath = "/autoAuth"

	AugmentIDETargetWarningSchemeUnverified  = "scheme_unverified"
	AugmentIDETargetWarningHandlerUnverified = "handler_unverified"
)

var ErrAugmentIDETargetInvalid = infraerrors.BadRequest(
	"AUGMENT_IDE_TARGET_INVALID",
	"unsupported augment IDE target",
)

type AugmentIDETarget struct {
	ID                  string
	Label               string
	Scheme              string
	DeeplinkHost        string
	AuthPath            string
	SchemeVerified      bool
	HandlerVerified     bool
	EnabledByDefault    bool
	OverrideRequired    bool
	AllowSchemeOverride bool
	OverrideEnvKey      string

	OverrideActive bool
	LaunchEligible bool
	Warning        string
}

// Evidence status as of 2026-05-09:
//   - vscode:// + Augment.vscode-augment/autoAuth is existing production behavior in this repo.
//   - cursor:// was verified from a local Cursor app bundle, but the Augment handler host/path is still unverified there.
//   - trae:// was verified from a local Trae app bundle, but the Augment handler host/path is still unverified there.
//   - kiro / windsurf / qodo / codebuddy / antigravity use tentative scheme defaults only and stay disabled
//     for auto-launch unless an operator explicitly opts in with an env override.
var augmentIDETargetCatalog = []AugmentIDETarget{
	{
		ID:               "vscode",
		Label:            "VS Code",
		Scheme:           "vscode",
		DeeplinkHost:     augmentDefaultIDEEplinkHost,
		AuthPath:         augmentDefaultIDEQuickAuthPath,
		SchemeVerified:   true,
		HandlerVerified:  true,
		EnabledByDefault: true,
	},
	{
		ID:                  "cursor",
		Label:               "Cursor",
		Scheme:              "cursor",
		DeeplinkHost:        augmentDefaultIDEEplinkHost,
		AuthPath:            augmentDefaultIDEQuickAuthPath,
		SchemeVerified:      true,
		HandlerVerified:     false,
		EnabledByDefault:    false,
		OverrideRequired:    true,
		AllowSchemeOverride: true,
		OverrideEnvKey:      "AUGMENT_IDE_SCHEME_CURSOR",
	},
	{
		ID:                  "kiro",
		Label:               "Kiro",
		Scheme:              "kiro",
		DeeplinkHost:        augmentDefaultIDEEplinkHost,
		AuthPath:            augmentDefaultIDEQuickAuthPath,
		SchemeVerified:      false,
		HandlerVerified:     false,
		EnabledByDefault:    false,
		OverrideRequired:    true,
		AllowSchemeOverride: true,
		OverrideEnvKey:      "AUGMENT_IDE_SCHEME_KIRO",
	},
	{
		ID:                  "trae",
		Label:               "Trae",
		Scheme:              "trae",
		DeeplinkHost:        augmentDefaultIDEEplinkHost,
		AuthPath:            augmentDefaultIDEQuickAuthPath,
		SchemeVerified:      true,
		HandlerVerified:     false,
		EnabledByDefault:    false,
		OverrideRequired:    true,
		AllowSchemeOverride: true,
		OverrideEnvKey:      "AUGMENT_IDE_SCHEME_TRAE",
	},
	{
		ID:                  "windsurf",
		Label:               "Windsurf",
		Scheme:              "windsurf",
		DeeplinkHost:        augmentDefaultIDEEplinkHost,
		AuthPath:            augmentDefaultIDEQuickAuthPath,
		SchemeVerified:      false,
		HandlerVerified:     false,
		EnabledByDefault:    false,
		OverrideRequired:    true,
		AllowSchemeOverride: true,
		OverrideEnvKey:      "AUGMENT_IDE_SCHEME_WINDSURF",
	},
	{
		ID:                  "qodo",
		Label:               "Qodo",
		Scheme:              "qodo",
		DeeplinkHost:        augmentDefaultIDEEplinkHost,
		AuthPath:            augmentDefaultIDEQuickAuthPath,
		SchemeVerified:      false,
		HandlerVerified:     false,
		EnabledByDefault:    false,
		OverrideRequired:    true,
		AllowSchemeOverride: true,
		OverrideEnvKey:      "AUGMENT_IDE_SCHEME_QODO",
	},
	{
		ID:                  "codebuddy",
		Label:               "CodeBuddy",
		Scheme:              "codebuddy",
		DeeplinkHost:        augmentDefaultIDEEplinkHost,
		AuthPath:            augmentDefaultIDEQuickAuthPath,
		SchemeVerified:      false,
		HandlerVerified:     false,
		EnabledByDefault:    false,
		OverrideRequired:    true,
		AllowSchemeOverride: true,
		OverrideEnvKey:      "AUGMENT_IDE_SCHEME_CODEBUDDY",
	},
	{
		ID:                  "antigravity",
		Label:               "Antigravity",
		Scheme:              "antigravity",
		DeeplinkHost:        augmentDefaultIDEEplinkHost,
		AuthPath:            augmentDefaultIDEQuickAuthPath,
		SchemeVerified:      false,
		HandlerVerified:     false,
		EnabledByDefault:    false,
		OverrideRequired:    true,
		AllowSchemeOverride: true,
		OverrideEnvKey:      "AUGMENT_IDE_SCHEME_ANTIGRAVITY",
	},
}

var augmentIDETargetAliases = map[string]string{
	"vscode":           "vscode",
	"visualstudiocode": "vscode",
	"cursor":           "cursor",
	"kiro":             "kiro",
	"trae":             "trae",
	"windsurf":         "windsurf",
	"qodo":             "qodo",
	"codebuddy":        "codebuddy",
	"antigravity":      "antigravity",
}

func ListAugmentIDETargets() []AugmentIDETarget {
	targets := make([]AugmentIDETarget, 0, len(augmentIDETargetCatalog))
	for _, target := range augmentIDETargetCatalog {
		targets = append(targets, resolveAugmentIDETargetRuntime(target))
	}
	return targets
}

func ResolveAugmentIDETarget(raw string) (AugmentIDETarget, error) {
	trimmed := strings.TrimSpace(raw)
	normalized := normalizeAugmentIDETargetID(trimmed)
	if trimmed == "" {
		normalized = augmentDefaultIDETargetID
	} else if normalized == "" {
		return AugmentIDETarget{}, ErrAugmentIDETargetInvalid
	}

	for _, target := range augmentIDETargetCatalog {
		if target.ID != normalized {
			continue
		}
		return resolveAugmentIDETargetRuntime(target), nil
	}

	return AugmentIDETarget{}, ErrAugmentIDETargetInvalid
}

func FormatAugmentIDEQuickLoginDeeplink(target AugmentIDETarget, grant, state, issuer, portal string) string {
	values := url.Values{}
	values.Set("grant", strings.TrimSpace(grant))
	values.Set("state", strings.TrimSpace(state))
	values.Set("source", "quick_login")
	if normalizedIssuer := normalizeAugmentIDEAbsoluteURL(issuer, true); normalizedIssuer != "" {
		values.Set("issuer", normalizedIssuer)
	}
	if normalizedPortal := normalizeAugmentIDEAbsoluteURL(portal, false); normalizedPortal != "" {
		values.Set("portal", normalizedPortal)
	}

	host := strings.TrimSpace(target.DeeplinkHost)
	if host == "" {
		host = augmentDefaultIDEEplinkHost
	}
	authPath := strings.TrimSpace(target.AuthPath)
	if authPath == "" {
		authPath = augmentDefaultIDEQuickAuthPath
	}
	if !strings.HasPrefix(authPath, "/") {
		authPath = "/" + authPath
	}

	return strings.TrimSpace(target.Scheme) + "://" + host + authPath + "?" + values.Encode()
}

func resolveAugmentIDETargetRuntime(target AugmentIDETarget) AugmentIDETarget {
	resolved := target
	if override := augmentIDETargetSchemeOverride(target); override != "" {
		resolved.Scheme = override
		resolved.OverrideActive = true
	}

	resolved.LaunchEligible = resolved.EnabledByDefault || resolved.OverrideActive
	if resolved.LaunchEligible {
		resolved.Warning = ""
		return resolved
	}

	switch {
	case !resolved.SchemeVerified:
		resolved.Warning = AugmentIDETargetWarningSchemeUnverified
	case !resolved.HandlerVerified:
		resolved.Warning = AugmentIDETargetWarningHandlerUnverified
	}

	return resolved
}

func augmentIDETargetSchemeOverride(target AugmentIDETarget) string {
	if !target.AllowSchemeOverride || strings.TrimSpace(target.OverrideEnvKey) == "" {
		return ""
	}
	return normalizeAugmentIDEScheme(os.Getenv(target.OverrideEnvKey))
}

func normalizeAugmentIDETargetID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	sanitized := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, raw)

	if targetID, ok := augmentIDETargetAliases[sanitized]; ok {
		return targetID
	}
	return ""
}

func normalizeAugmentIDEScheme(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	return strings.Map(func(r rune) rune {
		switch {
		case unicode.IsLetter(r):
			return unicode.ToLower(r)
		case unicode.IsDigit(r), r == '+', r == '-', r == '.':
			return r
		default:
			return -1
		}
	}, raw)
}

func normalizeAugmentIDEAbsoluteURL(raw string, originOnly bool) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	if originOnly {
		return parsed.Scheme + "://" + parsed.Host
	}
	return strings.TrimRight(parsed.String(), "/")
}
