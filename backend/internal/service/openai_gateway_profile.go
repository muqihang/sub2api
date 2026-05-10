package service

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

const (
	openAIGatewayResponsesBetaValue = "responses=experimental"
	openAIGatewayDefaultOriginator  = "codex_cli_rs"
	openAIWSBetaV1Value             = "responses_websockets=2026-02-04"
	openAIWSBetaV2Value             = "responses_websockets=2026-02-06"
	openAIWSBetaCompatV1            = "responses-websocket=v1"
)

var openAIGatewayVersionFromUserAgentRegex = regexp.MustCompile(`(?i)\bcodex[a-z0-9_]*\/([0-9]+(?:\.[0-9]+){1,3})`)

type OpenAIGatewayProfileRouteKind string

const (
	OpenAIGatewayProfileRouteResponsesHTTP        OpenAIGatewayProfileRouteKind = "responses_http"
	OpenAIGatewayProfileRouteResponsesCompact     OpenAIGatewayProfileRouteKind = "responses_compact"
	OpenAIGatewayProfileRouteCompatMessagesBridge OpenAIGatewayProfileRouteKind = "compat_messages_bridge"
	OpenAIGatewayProfileRouteResponsesWSV1        OpenAIGatewayProfileRouteKind = "responses_ws_v1"
	OpenAIGatewayProfileRouteResponsesWSV2        OpenAIGatewayProfileRouteKind = "responses_ws_v2"
)

type OpenAIGatewayStainlessProfile struct {
	Lang           string
	PackageVersion string
	OS             string
	Arch           string
	Runtime        string
	RuntimeVersion string
}

type OpenAIGatewayProfileArtifactOptions struct {
	RequestedOriginator string
	IsOfficialClient    bool
}

type OpenAIGatewayProfileArtifact struct {
	ProfileID       string
	Mode            string
	RouteKind       OpenAIGatewayProfileRouteKind
	UserAgent       string
	Version         string
	Originator      string
	OpenAIBeta      string
	Stainless       OpenAIGatewayStainlessProfile
	ClearOriginator bool
	ClearOpenAIBeta bool
}

func BuildOpenAIGatewayProfileArtifact(
	profile *OpenAIGatewayCanonicalProfile,
	routeKind OpenAIGatewayProfileRouteKind,
	opts OpenAIGatewayProfileArtifactOptions,
) *OpenAIGatewayProfileArtifact {
	if profile == nil {
		return nil
	}

	artifact := &OpenAIGatewayProfileArtifact{
		ProfileID: profile.ProfileID,
		Mode:      profile.Mode,
		RouteKind: routeKind,
		UserAgent: strings.TrimSpace(profile.UserAgent),
		Version:   alignOpenAIGatewayProfileVersion(strings.TrimSpace(profile.UserAgent), strings.TrimSpace(profile.Version), ""),
		Stainless: OpenAIGatewayStainlessProfile{
			Lang:           strings.TrimSpace(profile.StainlessLang),
			PackageVersion: strings.TrimSpace(profile.StainlessPackageVersion),
			OS:             strings.TrimSpace(profile.StainlessOS),
			Arch:           strings.TrimSpace(profile.StainlessArch),
			Runtime:        strings.TrimSpace(profile.StainlessRuntime),
			RuntimeVersion: strings.TrimSpace(profile.StainlessRuntimeVersion),
		},
	}

	switch routeKind {
	case OpenAIGatewayProfileRouteCompatMessagesBridge:
		artifact.ClearOpenAIBeta = true
		artifact.ClearOriginator = true
	case OpenAIGatewayProfileRouteResponsesCompact:
		artifact.OpenAIBeta = openAIGatewayResponsesBetaValue
		artifact.Originator = resolveOpenAIGatewayArtifactOriginator(opts.RequestedOriginator, opts.IsOfficialClient)
	case OpenAIGatewayProfileRouteResponsesHTTP:
		artifact.OpenAIBeta = openAIGatewayResponsesBetaValue
		artifact.Originator = resolveOpenAIGatewayArtifactOriginator(opts.RequestedOriginator, opts.IsOfficialClient)
	case OpenAIGatewayProfileRouteResponsesWSV1:
		artifact.OpenAIBeta = openAIWSBetaV1Value
		artifact.Originator = resolveOpenAIGatewayArtifactOriginator(opts.RequestedOriginator, opts.IsOfficialClient)
	case OpenAIGatewayProfileRouteResponsesWSV2:
		artifact.OpenAIBeta = openAIWSBetaV2Value
		artifact.Originator = resolveOpenAIGatewayArtifactOriginator(opts.RequestedOriginator, opts.IsOfficialClient)
	}

	return artifact
}

func (a *OpenAIGatewayProfileArtifact) WithUserAgentOverride(userAgent string) *OpenAIGatewayProfileArtifact {
	if a == nil {
		return nil
	}
	override := strings.TrimSpace(userAgent)
	if override == "" {
		return a
	}
	cloned := *a
	cloned.UserAgent = override
	cloned.Version = alignOpenAIGatewayProfileVersion(override, "", "")
	return &cloned
}

func (a *OpenAIGatewayProfileArtifact) ForceCodexCLI() *OpenAIGatewayProfileArtifact {
	if a == nil {
		return nil
	}
	cloned := *a
	cloned.UserAgent = codexCLIUserAgent
	cloned.Version = alignOpenAIGatewayProfileVersion(codexCLIUserAgent, codexCLIVersion, codexCLIVersion)
	return &cloned
}

func (a *OpenAIGatewayProfileArtifact) ApplyHTTP(headers http.Header) {
	if a == nil || headers == nil {
		return
	}
	setIfNotEmpty(headers, "user-agent", a.UserAgent)
	setIfNotEmpty(headers, "X-Stainless-Lang", a.Stainless.Lang)
	setIfNotEmpty(headers, "X-Stainless-Package-Version", a.Stainless.PackageVersion)
	setIfNotEmpty(headers, "X-Stainless-OS", a.Stainless.OS)
	setIfNotEmpty(headers, "X-Stainless-Arch", a.Stainless.Arch)
	setIfNotEmpty(headers, "X-Stainless-Runtime", a.Stainless.Runtime)
	setIfNotEmpty(headers, "X-Stainless-Runtime-Version", a.Stainless.RuntimeVersion)

	if a.RouteKind == OpenAIGatewayProfileRouteResponsesCompact {
		setIfNotEmpty(headers, "version", a.Version)
	} else {
		headers.Del("version")
	}

	if a.ClearOpenAIBeta {
		headers.Del("OpenAI-Beta")
	} else {
		setIfNotEmpty(headers, "OpenAI-Beta", a.OpenAIBeta)
	}
	if a.ClearOriginator {
		headers.Del("originator")
	} else {
		setIfNotEmpty(headers, "originator", a.Originator)
	}
}

func (a *OpenAIGatewayProfileArtifact) ApplyWS(headers http.Header) {
	if a == nil || headers == nil {
		return
	}
	setIfNotEmpty(headers, "user-agent", a.UserAgent)
	setIfNotEmpty(headers, "X-Stainless-Lang", a.Stainless.Lang)
	setIfNotEmpty(headers, "X-Stainless-Package-Version", a.Stainless.PackageVersion)
	setIfNotEmpty(headers, "X-Stainless-OS", a.Stainless.OS)
	setIfNotEmpty(headers, "X-Stainless-Arch", a.Stainless.Arch)
	setIfNotEmpty(headers, "X-Stainless-Runtime", a.Stainless.Runtime)
	setIfNotEmpty(headers, "X-Stainless-Runtime-Version", a.Stainless.RuntimeVersion)
	if a.ClearOpenAIBeta {
		headers.Del("OpenAI-Beta")
	} else {
		setIfNotEmpty(headers, "OpenAI-Beta", a.OpenAIBeta)
	}
	if a.ClearOriginator {
		headers.Del("originator")
	} else {
		setIfNotEmpty(headers, "originator", a.Originator)
	}
	headers.Del("version")
}

func buildOpenAIGatewayFallbackProfile(headers http.Header) *OpenAIGatewayCanonicalProfile {
	if headers == nil {
		headers = http.Header{}
	}
	profile := &OpenAIGatewayCanonicalProfile{
		UserAgent:               strings.TrimSpace(headers.Get("User-Agent")),
		Version:                 strings.TrimSpace(headers.Get("version")),
		StainlessLang:           strings.TrimSpace(headers.Get("X-Stainless-Lang")),
		StainlessPackageVersion: strings.TrimSpace(headers.Get("X-Stainless-Package-Version")),
		StainlessOS:             strings.TrimSpace(headers.Get("X-Stainless-OS")),
		StainlessArch:           strings.TrimSpace(headers.Get("X-Stainless-Arch")),
		StainlessRuntime:        strings.TrimSpace(headers.Get("X-Stainless-Runtime")),
		StainlessRuntimeVersion: strings.TrimSpace(headers.Get("X-Stainless-Runtime-Version")),
	}
	profile.Version = alignOpenAIGatewayProfileVersion(profile.UserAgent, profile.Version, codexCLIVersion)
	return profile
}

func defaultOpenAIGatewayCanonicalProfile(cfg *config.Config) *OpenAIGatewayCanonicalProfile {
	profile := &OpenAIGatewayCanonicalProfile{
		UserAgent:               codexCLIUserAgent,
		Version:                 codexCLIVersion,
		StainlessLang:           "js",
		StainlessPackageVersion: "0.70.0",
		StainlessOS:             "Linux",
		StainlessArch:           "arm64",
		StainlessRuntime:        "node",
		StainlessRuntimeVersion: "v24.13.0",
	}
	if cfg == nil {
		return profile
	}
	coreCfg := cfg.Gateway.OpenAICore
	if v := strings.TrimSpace(coreCfg.CanonicalUserAgent); v != "" {
		profile.UserAgent = v
	}
	if v := strings.TrimSpace(coreCfg.CanonicalStainlessLang); v != "" {
		profile.StainlessLang = v
	}
	if v := strings.TrimSpace(coreCfg.CanonicalStainlessPackageVersion); v != "" {
		profile.StainlessPackageVersion = v
	}
	if v := strings.TrimSpace(coreCfg.CanonicalStainlessOS); v != "" {
		profile.StainlessOS = v
	}
	if v := strings.TrimSpace(coreCfg.CanonicalStainlessArch); v != "" {
		profile.StainlessArch = v
	}
	if v := strings.TrimSpace(coreCfg.CanonicalStainlessRuntime); v != "" {
		profile.StainlessRuntime = v
	}
	if v := strings.TrimSpace(coreCfg.CanonicalStainlessRuntimeVersion); v != "" {
		profile.StainlessRuntimeVersion = v
	}
	profile.Version = alignOpenAIGatewayProfileVersion(profile.UserAgent, profile.Version, codexCLIVersion)
	return profile
}

func alignOpenAIGatewayProfileVersion(userAgent, explicitVersion, fallbackVersion string) string {
	if derived := deriveOpenAIGatewayProfileVersion(userAgent); derived != "" {
		return derived
	}
	if explicit := strings.TrimSpace(explicitVersion); explicit != "" {
		return explicit
	}
	return strings.TrimSpace(fallbackVersion)
}

func deriveOpenAIGatewayProfileVersion(userAgent string) string {
	match := openAIGatewayVersionFromUserAgentRegex.FindStringSubmatch(strings.TrimSpace(userAgent))
	if len(match) != 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func resolveOpenAIGatewayArtifactOriginator(requestedOriginator string, isOfficialClient bool) string {
	if originator := strings.TrimSpace(requestedOriginator); originator != "" {
		return originator
	}
	if isOfficialClient {
		return openAIGatewayDefaultOriginator
	}
	return "opencode"
}

func resolveOpenAIGatewayHTTPProfileRouteKind(isCompact bool, compatMessagesBridge bool) OpenAIGatewayProfileRouteKind {
	if compatMessagesBridge {
		return OpenAIGatewayProfileRouteCompatMessagesBridge
	}
	if isCompact {
		return OpenAIGatewayProfileRouteResponsesCompact
	}
	return OpenAIGatewayProfileRouteResponsesHTTP
}

func resolveOpenAIGatewayWSProfileRouteKind(transport OpenAIUpstreamTransport) OpenAIGatewayProfileRouteKind {
	if transport == OpenAIUpstreamTransportResponsesWebsocket {
		return OpenAIGatewayProfileRouteResponsesWSV1
	}
	return OpenAIGatewayProfileRouteResponsesWSV2
}

func setIfNotEmpty(headers http.Header, key, value string) {
	if headers == nil {
		return
	}
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		headers.Set(key, trimmed)
	}
}

func buildOpenAIGatewayProfileID(accountID int64) string {
	sum := sha256.Sum256([]byte("openai-gateway-profile:" + strconv.FormatInt(accountID, 10)))
	return hex.EncodeToString(sum[:])
}
