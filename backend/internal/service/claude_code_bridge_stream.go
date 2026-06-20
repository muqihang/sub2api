package service

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/tidwall/gjson"
)

const ClaudeCodeBridgeCredentialScope = "bridge_pool"

var (
	claudeCodeBridgeSecretTokenRe = regexp.MustCompile(`\b(?:sk|rk|ak)-[A-Za-z0-9._-]+`)
	claudeCodeBridgeRequestIDRe   = regexp.MustCompile(`\b(?:req|request|trace)[_-][A-Za-z0-9._-]+`)
	claudeCodeBridgeURLRe         = regexp.MustCompile(`https?://[^\s"']+`)
)

type ClaudeCodeBridgeRouteDecision struct {
	ModelID                  string
	UpstreamModel            string
	Provider                 string
	Route                    string
	ClientType               string
	RuntimeHash              string
	OverlayHash              string
	CatalogHash              string
	CatalogVersion           string
	ProviderOwner            string
	CredentialScope          string
	GatewayLocation          string
	FormalPoolAllowed        bool
	NativeAttestationAllowed bool
	PreferredProtocol        string
	AnthropicBaseURL         string
	OpenAIBaseURL            string
	FallbackProtocol         string
	FallbackReason           string
	CapabilitiesVerified     bool
	SupportsText             bool
	SupportsTools            bool
	SupportsStreaming        bool
	SupportsUsage            bool
	SupportsCacheAudit       bool
	SupportsReasoningMapping bool
	SupportsErrorPassthrough bool
	ReasoningEffortLevels    []string
	CachePolicy              string
}

type ClaudeCodeBridgeAuditSummary struct {
	ClientType                  string   `json:"client_type"`
	Route                       string   `json:"route"`
	Provider                    string   `json:"provider"`
	ModelID                     string   `json:"model_id"`
	NativeAttested              bool     `json:"native_attested"`
	FormalPoolAllowed           bool     `json:"formal_pool_allowed"`
	RuntimeHash                 string   `json:"runtime_hash,omitempty"`
	OverlayHash                 string   `json:"overlay_hash,omitempty"`
	CatalogHash                 string   `json:"catalog_hash,omitempty"`
	CatalogVersion              string   `json:"catalog_version,omitempty"`
	ProviderOwner               string   `json:"provider_owner,omitempty"`
	CredentialScope             string   `json:"credential_scope"`
	GatewayLocation             string   `json:"gateway_location,omitempty"`
	PreferredProtocol           string   `json:"preferred_protocol,omitempty"`
	SelectedProtocol            string   `json:"selected_protocol,omitempty"`
	FallbackProtocol            string   `json:"fallback_protocol,omitempty"`
	FallbackReason              string   `json:"fallback_reason,omitempty"`
	FallbackUsed                bool     `json:"fallback_used,omitempty"`
	CapabilitiesVerified        bool     `json:"capabilities_verified,omitempty"`
	SupportsText                bool     `json:"supports_text,omitempty"`
	SupportsTools               bool     `json:"supports_tools,omitempty"`
	SupportsStreaming           bool     `json:"supports_streaming,omitempty"`
	SupportsUsage               bool     `json:"supports_usage,omitempty"`
	SupportsCacheAudit          bool     `json:"supports_cache_audit,omitempty"`
	SupportsReasoningMapping    bool     `json:"supports_reasoning_mapping,omitempty"`
	SupportsErrorPassthrough    bool     `json:"supports_error_passthrough,omitempty"`
	CachePolicy                 string   `json:"cache_policy,omitempty"`
	ProviderCacheMechanism      string   `json:"provider_cache_mechanism,omitempty"`
	UpstreamPathKind            string   `json:"upstream_path_kind,omitempty"`
	CacheControlPresent         bool     `json:"cache_control_present,omitempty"`
	CacheControlLocations       []string `json:"cache_control_locations,omitempty"`
	CacheControlProviderIgnored bool     `json:"cache_control_provider_ignored,omitempty"`
	StablePrefixHMAC            string   `json:"stable_prefix_hmac,omitempty"`
	StablePrefixTokenBucket     string   `json:"stable_prefix_token_bucket,omitempty"`
	CacheReadTokens             int      `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens            int      `json:"cache_write_tokens,omitempty"`
	CacheMissTokens             int      `json:"cache_miss_tokens,omitempty"`
}

type ClaudeCodeBridgeStreamResult struct {
	Body  []byte
	Audit ClaudeCodeBridgeAuditSummary
}

type ClaudeCodeBridgeAnthropicLiveResult struct {
	StatusCode int
	Body       []byte
	Header     http.Header
	Audit      ClaudeCodeBridgeAuditSummary
}

type ClaudeCodeBridgeAnthropicLiveStreamResult struct {
	StatusCode int
	Header     http.Header
	Audit      ClaudeCodeBridgeAuditSummary
}

type claudeCodeBridgeRequestAudit struct {
	SelectedProtocol            string
	FallbackUsed                bool
	UpstreamPathKind            string
	ProviderCacheMechanism      string
	CacheControlPresent         bool
	CacheControlLocations       []string
	CacheControlProviderIgnored bool
	StablePrefixHMAC            string
	StablePrefixTokenBucket     string
}

type claudeCodeBridgeAuditSummaryContextKey struct{}

func WithClaudeCodeBridgeAuditSummary(ctx context.Context, summary ClaudeCodeBridgeAuditSummary) context.Context {
	return context.WithValue(ctx, claudeCodeBridgeAuditSummaryContextKey{}, summary)
}

func ClaudeCodeBridgeAuditSummaryFromContext(ctx context.Context) (ClaudeCodeBridgeAuditSummary, bool) {
	summary, ok := ctx.Value(claudeCodeBridgeAuditSummaryContextKey{}).(ClaudeCodeBridgeAuditSummary)
	return summary, ok
}

func ClaudeCodeProviderBridgeLiveRequestAllowed(decision ClaudeCodeProviderRouteDecision) bool {
	if !claudeCodeBridgeEnvEnabled("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED") {
		return false
	}
	bridgeDecision := decision.BridgeRouteDecision()
	switch strings.TrimSpace(bridgeDecision.Provider) {
	case "deepseek":
		return (ClaudeCodeBridgeAnthropicAPIKeyFromEnv(bridgeDecision.Provider) != "" && ClaudeCodeBridgeAnthropicLiveConfigured() && ClaudeCodeBridgeAnthropicLiveLabBillingBypassEnabled() && ClaudeCodeBridgeAnthropicLiveDecisionValid(bridgeDecision) == nil && claudeCodeBridgeAnthropicUnsafeLabBaseURLAllowed(bridgeDecision)) ||
			ClaudeCodeBridgeDeepSeekOpenAICompatibleFallbackLiveEligible(bridgeDecision)
	case "zai_glm", "kimi":
		return ClaudeCodeBridgeAnthropicAPIKeyFromEnv(bridgeDecision.Provider) != "" && ClaudeCodeBridgeAnthropicLiveConfigured() && ClaudeCodeBridgeAnthropicLiveLabBillingBypassEnabled() && ClaudeCodeBridgeAnthropicLiveDecisionValid(bridgeDecision) == nil && claudeCodeBridgeAnthropicUnsafeLabBaseURLAllowed(bridgeDecision)
	case "openai", "agnes":
		return ClaudeCodeBridgeOpenAICompatibleAPIKeyFromEnv(bridgeDecision.Provider) != "" && ClaudeCodeBridgeOpenAILiveEligible(bridgeDecision)
	default:
		return false
	}
}

func ClaudeCodeBridgeAnthropicLiveEligible(decision ClaudeCodeBridgeRouteDecision) bool {
	return ClaudeCodeBridgeAnthropicLiveConfigured() && ClaudeCodeBridgeAnthropicLiveLabBillingBypassEnabled() && ClaudeCodeBridgeAnthropicAPIKeyFromEnv(decision.Provider) != "" && ClaudeCodeBridgeAnthropicLiveDecisionValid(decision) == nil && claudeCodeBridgeAnthropicUnsafeLabBaseURLAllowed(decision)
}

func ClaudeCodeBridgeAnthropicLiveDecisionValid(decision ClaudeCodeBridgeRouteDecision) error {
	provider := strings.TrimSpace(decision.Provider)
	route := strings.TrimSpace(decision.Route)
	clientType := strings.TrimSpace(decision.ClientType)
	allowed := map[string]struct {
		route      string
		clientType string
	}{
		"deepseek": {route: "deepseek_bridge", clientType: "claude_code_bridge_deepseek"},
		"zai_glm":  {route: "zai_glm_bridge", clientType: "claude_code_bridge_zai_glm"},
		"kimi":     {route: "kimi_bridge", clientType: "claude_code_bridge_kimi"},
	}
	contract, ok := allowed[provider]
	if !ok || route != contract.route || clientType != contract.clientType {
		return fmt.Errorf("claude code bridge live only supports verified anthropic-compatible bridge providers")
	}
	if strings.TrimSpace(decision.PreferredProtocol) != "anthropic_messages" || strings.TrimSpace(decision.AnthropicBaseURL) == "" {
		return fmt.Errorf("claude code bridge live requires anthropic messages")
	}
	if !decision.CapabilitiesVerified || !decision.SupportsText || !decision.SupportsTools || !decision.SupportsStreaming || !decision.SupportsUsage || !decision.SupportsErrorPassthrough {
		return fmt.Errorf("claude code bridge live capabilities are not verified")
	}
	return validateClaudeCodeBridgeDecision(decision)
}

func ExecuteClaudeCodeBridgeAnthropicLive(ctx context.Context, httpClient *http.Client, decision ClaudeCodeBridgeRouteDecision, body []byte, apiKey string) (ClaudeCodeBridgeAnthropicLiveResult, error) {
	var out bytes.Buffer
	result, err := StreamClaudeCodeBridgeAnthropicLive(ctx, httpClient, decision, body, apiKey, &out)
	if err != nil {
		return ClaudeCodeBridgeAnthropicLiveResult{}, err
	}
	return ClaudeCodeBridgeAnthropicLiveResult{
		StatusCode: result.StatusCode,
		Body:       out.Bytes(),
		Header:     result.Header,
		Audit:      result.Audit,
	}, nil
}

func StreamClaudeCodeBridgeAnthropicLive(ctx context.Context, httpClient *http.Client, decision ClaudeCodeBridgeRouteDecision, body []byte, apiKey string, dst io.Writer) (ClaudeCodeBridgeAnthropicLiveStreamResult, error) {
	if !ClaudeCodeBridgeAnthropicLiveConfigured() {
		return ClaudeCodeBridgeAnthropicLiveStreamResult{}, fmt.Errorf("claude code bridge live request is not enabled")
	}
	if !ClaudeCodeBridgeAnthropicLiveLabBillingBypassEnabled() {
		return ClaudeCodeBridgeAnthropicLiveStreamResult{}, fmt.Errorf("claude code bridge live requires billing/concurrency guard")
	}
	if err := ClaudeCodeBridgeAnthropicLiveDecisionValid(decision); err != nil {
		return ClaudeCodeBridgeAnthropicLiveStreamResult{}, err
	}
	if !claudeCodeBridgeAnthropicUnsafeLabBaseURLAllowed(decision) {
		return ClaudeCodeBridgeAnthropicLiveStreamResult{}, fmt.Errorf("claude code bridge unsafe lab bypass requires loopback upstream; external providers require production billing/concurrency guard")
	}
	if strings.TrimSpace(apiKey) == "" {
		return ClaudeCodeBridgeAnthropicLiveStreamResult{}, fmt.Errorf("claude code bridge live api key is required")
	}
	if err := validateClaudeCodeBridgeBodyBinding(decision, body); err != nil {
		return ClaudeCodeBridgeAnthropicLiveStreamResult{}, err
	}
	if dst == nil {
		return ClaudeCodeBridgeAnthropicLiveStreamResult{}, fmt.Errorf("claude code bridge live stream writer is required")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if ctx == nil {
		ctx = context.Background()
	}
	upstreamBody, err := rewriteClaudeCodeBridgeAnthropicBodyModel(decision, body)
	if err != nil {
		return ClaudeCodeBridgeAnthropicLiveStreamResult{}, err
	}
	upstreamURL := buildAnthropicMessagesURL(decision.AnthropicBaseURL)
	requestAudit := buildClaudeCodeBridgeAnthropicRequestAudit(decision, upstreamBody, upstreamURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(upstreamBody))
	if err != nil {
		return ClaudeCodeBridgeAnthropicLiveStreamResult{}, err
	}
	setCodexGatewayAnthropicHeaders(req, apiKey, true)
	resp, err := httpClient.Do(req)
	if err != nil {
		return ClaudeCodeBridgeAnthropicLiveStreamResult{}, err
	}
	defer resp.Body.Close()
	header := claudeCodeBridgeCloneHTTPHeader(resp.Header)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		limited, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return ClaudeCodeBridgeAnthropicLiveStreamResult{}, fmt.Errorf("claude code bridge upstream status %d: %s", resp.StatusCode, sanitizeClaudeCodeBridgeErrorMessage(string(limited)))
	}
	applyClaudeCodeBridgeLiveResponseHeaders(dst, header)
	fixture, err := copyClaudeCodeBridgeSSE(dst, resp.Body)
	if err != nil {
		return ClaudeCodeBridgeAnthropicLiveStreamResult{}, err
	}
	return ClaudeCodeBridgeAnthropicLiveStreamResult{
		StatusCode: resp.StatusCode,
		Header:     header,
		Audit:      buildClaudeCodeBridgeAuditSummaryWithRequest(decision, fixture, requestAudit),
	}, nil
}

func ClaudeCodeBridgeDeepSeekAPIKeyFromEnv() string {
	return ClaudeCodeBridgeAnthropicAPIKeyFromEnv("deepseek")
}

func ClaudeCodeBridgeAnthropicAPIKeyFromEnv(provider string) string {
	provider = strings.TrimSpace(provider)
	switch provider {
	case "deepseek":
		return strings.TrimSpace(os.Getenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY"))
	case "zai_glm":
		return strings.TrimSpace(os.Getenv("SUB2API_CLAUDE_CODE_BRIDGE_ZAI_GLM_API_KEY"))
	case "kimi":
		return strings.TrimSpace(os.Getenv("SUB2API_CLAUDE_CODE_BRIDGE_KIMI_API_KEY"))
	default:
		return ""
	}
}

func ClaudeCodeBridgeAnthropicLiveConfigured() bool {
	return claudeCodeBridgeEnvEnabled("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED") && (claudeCodeBridgeEnvEnabled("SUB2API_CLAUDE_CODE_BRIDGE_ANTHROPIC_LIVE_ENABLED") || claudeCodeBridgeEnvEnabled("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED"))
}

func ClaudeCodeBridgeAnthropicLiveLabBillingBypassEnabled() bool {
	return claudeCodeBridgeEnvEnabled("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB")
}

func claudeCodeBridgeAnthropicUnsafeLabBaseURLAllowed(decision ClaudeCodeBridgeRouteDecision) bool {
	baseURL := strings.TrimSpace(decision.AnthropicBaseURL)
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed == nil {
		return false
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme != "http" && scheme != "https" {
		return false
	}
	hostname := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if hostname == "localhost" {
		return true
	}
	if ip := net.ParseIP(hostname); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

func BuildClaudeCodeBridgeSkeletonSSE(decision ClaudeCodeBridgeRouteDecision, body []byte) (ClaudeCodeBridgeStreamResult, error) {
	if err := validateClaudeCodeBridgeDecision(decision); err != nil {
		return ClaudeCodeBridgeStreamResult{}, err
	}
	if err := validateClaudeCodeBridgeBodyBinding(decision, body); err != nil {
		return ClaudeCodeBridgeStreamResult{}, err
	}
	if claudeCodeBridgeSkeletonRequiresLive(body) {
		stream := buildClaudeCodeBridgeErrorSSE("invalid_request_error", "Claude Code bridge live required for multi_tool_use.parallel or Agent tool execution")
		return ClaudeCodeBridgeStreamResult{Body: stream, Audit: buildClaudeCodeBridgeAuditSummary(decision, ClaudeCodeBridgeProviderFixture{})}, nil
	}
	toolName := claudeCodeBridgeSkeletonToolName(body)
	var stream []byte
	if toolName != "" {
		stream = buildClaudeCodeBridgeToolUseSSE(decision.ModelID, toolName)
	} else {
		stream = buildClaudeCodeBridgeTextSSE(decision.ModelID, "bridge skeleton")
	}
	return ClaudeCodeBridgeStreamResult{Body: stream, Audit: buildClaudeCodeBridgeAuditSummary(decision, ClaudeCodeBridgeProviderFixture{})}, nil
}

type ClaudeCodeBridgeProviderFixture struct {
	TextDeltas       []string
	ReasoningDeltas  []string
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
	CacheMissTokens  int
	StopReason       string
	ErrorType        string
	ErrorMessage     string
}

func BuildClaudeCodeBridgeFixtureSSE(decision ClaudeCodeBridgeRouteDecision, body []byte, fixture ClaudeCodeBridgeProviderFixture) (ClaudeCodeBridgeStreamResult, error) {
	if err := validateClaudeCodeBridgeDecision(decision); err != nil {
		return ClaudeCodeBridgeStreamResult{}, err
	}
	if err := validateClaudeCodeBridgeBodyBinding(decision, body); err != nil {
		return ClaudeCodeBridgeStreamResult{}, err
	}
	if strings.TrimSpace(fixture.ErrorType) != "" {
		return ClaudeCodeBridgeStreamResult{
			Body:  buildClaudeCodeBridgeErrorSSE(fixture.ErrorType, fixture.ErrorMessage),
			Audit: buildClaudeCodeBridgeAuditSummary(decision, fixture),
		}, nil
	}
	text := strings.Join(fixture.TextDeltas, "")
	if strings.TrimSpace(text) == "" {
		text = "bridge fixture"
	}
	stopReason := strings.TrimSpace(fixture.StopReason)
	if stopReason == "" {
		stopReason = "end_turn"
	}
	return ClaudeCodeBridgeStreamResult{
		Body:  buildClaudeCodeBridgeTextSSEWithUsage(decision.ModelID, text, stopReason, fixture.InputTokens, fixture.OutputTokens),
		Audit: buildClaudeCodeBridgeAuditSummary(decision, fixture),
	}, nil
}

func buildClaudeCodeBridgeAuditSummary(decision ClaudeCodeBridgeRouteDecision, fixture ClaudeCodeBridgeProviderFixture) ClaudeCodeBridgeAuditSummary {
	return buildClaudeCodeBridgeAuditSummaryWithRequest(decision, fixture, claudeCodeBridgeRequestAudit{})
}

func buildClaudeCodeBridgeAuditSummaryWithRequest(decision ClaudeCodeBridgeRouteDecision, fixture ClaudeCodeBridgeProviderFixture, requestAudit claudeCodeBridgeRequestAudit) ClaudeCodeBridgeAuditSummary {
	selectedProtocol := safeClaudeCodeNativeLabel(requestAudit.SelectedProtocol)
	if selectedProtocol == "" {
		selectedProtocol = safeClaudeCodeNativeLabel(decision.PreferredProtocol)
	}
	return ClaudeCodeBridgeAuditSummary{
		ClientType:                  decision.ClientType,
		Route:                       decision.Route,
		Provider:                    decision.Provider,
		ModelID:                     decision.ModelID,
		NativeAttested:              false,
		FormalPoolAllowed:           false,
		RuntimeHash:                 decision.RuntimeHash,
		OverlayHash:                 decision.OverlayHash,
		CatalogHash:                 decision.CatalogHash,
		CatalogVersion:              safeClaudeCodeNativeLabel(decision.CatalogVersion),
		ProviderOwner:               safeClaudeCodeNativeLabel(decision.ProviderOwner),
		CredentialScope:             decision.CredentialScope,
		GatewayLocation:             safeClaudeCodeNativeLabel(decision.GatewayLocation),
		PreferredProtocol:           safeClaudeCodeNativeLabel(decision.PreferredProtocol),
		SelectedProtocol:            selectedProtocol,
		FallbackProtocol:            safeClaudeCodeNativeLabel(decision.FallbackProtocol),
		FallbackReason:              safeClaudeCodeNativeLabel(decision.FallbackReason),
		FallbackUsed:                requestAudit.FallbackUsed,
		CapabilitiesVerified:        decision.CapabilitiesVerified,
		SupportsText:                decision.SupportsText,
		SupportsTools:               decision.SupportsTools,
		SupportsStreaming:           decision.SupportsStreaming,
		SupportsUsage:               decision.SupportsUsage,
		SupportsCacheAudit:          decision.SupportsCacheAudit,
		SupportsReasoningMapping:    decision.SupportsReasoningMapping,
		SupportsErrorPassthrough:    decision.SupportsErrorPassthrough,
		CachePolicy:                 decision.CachePolicy,
		ProviderCacheMechanism:      safeClaudeCodeNativeLabel(requestAudit.ProviderCacheMechanism),
		UpstreamPathKind:            safeClaudeCodeBridgeAuditPathKind(requestAudit.UpstreamPathKind),
		CacheControlPresent:         requestAudit.CacheControlPresent,
		CacheControlLocations:       requestAudit.CacheControlLocations,
		CacheControlProviderIgnored: requestAudit.CacheControlProviderIgnored,
		StablePrefixHMAC:            safeClaudeCodeBridgeAuditHMAC(requestAudit.StablePrefixHMAC),
		StablePrefixTokenBucket:     safeClaudeCodeNativeLabel(requestAudit.StablePrefixTokenBucket),
		CacheReadTokens:             fixture.CacheReadTokens,
		CacheWriteTokens:            fixture.CacheWriteTokens,
		CacheMissTokens:             fixture.CacheMissTokens,
	}
}

func buildClaudeCodeBridgeAnthropicRequestAudit(decision ClaudeCodeBridgeRouteDecision, upstreamBody []byte, upstreamURL string) claudeCodeBridgeRequestAudit {
	out := claudeCodeBridgeRequestAudit{
		SelectedProtocol: strings.TrimSpace(decision.PreferredProtocol),
		FallbackUsed:     false,
		UpstreamPathKind: safeClaudeCodeBridgeUpstreamPathKind(upstreamURL),
	}
	if strings.TrimSpace(decision.Provider) == "deepseek" && strings.TrimSpace(decision.PreferredProtocol) == "anthropic_messages" {
		out.ProviderCacheMechanism = "deepseek_prefix_kv"
		out.CacheControlProviderIgnored = true
	}
	out.CacheControlLocations = claudeCodeBridgeCacheControlLocations(upstreamBody)
	out.CacheControlPresent = len(out.CacheControlLocations) > 0
	out.StablePrefixHMAC, out.StablePrefixTokenBucket = claudeCodeBridgeStablePrefixAudit(decision, upstreamBody)
	return out
}

func safeClaudeCodeBridgeUpstreamPathKind(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed == nil {
		return ""
	}
	path := strings.TrimSpace(parsed.EscapedPath())
	if path == "" {
		return "/"
	}
	return path
}

func safeClaudeCodeBridgeAuditPathKind(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || len(path) > 128 || strings.Contains(path, "?") || strings.Contains(path, "#") {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		return ""
	}
	for _, r := range path {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' || r == '/' {
			continue
		}
		return ""
	}
	return path
}

func safeClaudeCodeBridgeAuditHMAC(value string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "hmac-sha256:") || len(value) > 128 {
		return ""
	}
	parts := strings.Split(value, ":")
	if len(parts) != 3 || parts[0] != "hmac-sha256" || parts[2] == "" {
		return ""
	}
	if safeClaudeCodeNativeLabel(parts[1]) != parts[1] || len(parts[2]) != 64 {
		return ""
	}
	for _, r := range parts[2] {
		if (r >= 'a' && r <= 'f') || (r >= '0' && r <= '9') {
			continue
		}
		return ""
	}
	return value
}

func claudeCodeBridgeCacheControlLocations(body []byte) []string {
	locations := map[string]struct{}{}
	if gjson.GetBytes(body, "cache_control").Exists() {
		locations["top_level"] = struct{}{}
	}
	if gjson.GetBytes(body, "system.#.cache_control").Exists() {
		locations["system"] = struct{}{}
	}
	if gjson.GetBytes(body, "tools.#.cache_control").Exists() {
		locations["tools"] = struct{}{}
	}
	messages := gjson.GetBytes(body, "messages")
	if messages.IsArray() {
		items := messages.Array()
		lastIndex := len(items) - 1
		for i := range items {
			if gjson.GetBytes(body, fmt.Sprintf("messages.%d.content.#.cache_control", i)).Exists() ||
				gjson.GetBytes(body, fmt.Sprintf("messages.%d.cache_control", i)).Exists() {
				if i == lastIndex {
					locations["current"] = struct{}{}
				} else {
					locations["history"] = struct{}{}
				}
			}
		}
	}
	out := make([]string, 0, len(locations))
	for location := range locations {
		out = append(out, location)
	}
	sort.Strings(out)
	return out
}

func claudeCodeBridgeStablePrefixAudit(decision ClaudeCodeBridgeRouteDecision, body []byte) (string, string) {
	secret := os.Getenv("SUB2API_CLAUDE_CODE_CACHE_AUDIT_HMAC_KEY")
	if strings.TrimSpace(secret) == "" {
		return "", ""
	}
	canonical, ok := claudeCodeBridgeStablePrefixCanonical(decision, body)
	if !ok {
		return "", ""
	}
	keyID := safeClaudeCodeNativeLabel(os.Getenv("SUB2API_CLAUDE_CODE_CACHE_AUDIT_HMAC_KEY_ID"))
	if keyID == "" {
		keyID = "local-cache-audit-v1"
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(canonical)
	return "hmac-sha256:" + keyID + ":" + hex.EncodeToString(mac.Sum(nil)), claudeCodeBridgeTokenBucket(len(canonical))
}

func claudeCodeBridgeStablePrefixCanonical(decision ClaudeCodeBridgeRouteDecision, body []byte) ([]byte, bool) {
	var payload map[string]any
	if len(body) == 0 || json.Unmarshal(body, &payload) != nil {
		return nil, false
	}
	stable := map[string]any{
		"provider": strings.TrimSpace(decision.Provider),
		"model":    claudeCodeBridgeEffectiveUpstreamModel(decision),
	}
	if system, ok := payload["system"]; ok {
		stable["system"] = system
	}
	if tools, ok := payload["tools"]; ok {
		stable["tools"] = tools
	}
	if rawMessages, ok := payload["messages"].([]any); ok && len(rawMessages) > 1 {
		stable["messages"] = rawMessages[:len(rawMessages)-1]
	}
	canonical, err := json.Marshal(stable)
	if err != nil {
		return nil, false
	}
	return canonical, true
}

func claudeCodeBridgeTokenBucket(canonicalBytes int) string {
	estimatedTokens := canonicalBytes / 4
	switch {
	case estimatedTokens < 1000:
		return "lt_1k"
	case estimatedTokens < 4000:
		return "1k_4k"
	case estimatedTokens < 16000:
		return "4k_16k"
	default:
		return "gt_16k"
	}
}

func validateClaudeCodeBridgeBodyBinding(decision ClaudeCodeBridgeRouteDecision, body []byte) error {
	var payload map[string]any
	if len(body) == 0 || json.Unmarshal(body, &payload) != nil {
		return fmt.Errorf("claude code bridge model binding requires JSON body")
	}
	model, _ := payload["model"].(string)
	if strings.TrimSpace(model) == "" || strings.TrimSpace(model) != decision.ModelID {
		return fmt.Errorf("claude code bridge model binding mismatch")
	}
	for _, field := range anthropicCompatOpenAIOnlyTopLevelFields {
		if _, exists := payload[field]; exists {
			return fmt.Errorf("claude code bridge body must use Anthropic messages shape")
		}
	}
	messages, ok := payload["messages"].([]any)
	if !ok {
		return fmt.Errorf("claude code bridge body must include messages")
	}
	for _, item := range messages {
		message, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("claude code bridge message shape is invalid")
		}
		role, _ := message["role"].(string)
		switch role {
		case "system", "user", "assistant":
		default:
			return fmt.Errorf("claude code bridge message role is invalid")
		}
	}
	toolNames := map[string]struct{}{}
	if rawTools, exists := payload["tools"]; exists {
		tools, ok := rawTools.([]any)
		if !ok {
			return fmt.Errorf("claude code bridge tool shape is invalid")
		}
		for _, item := range tools {
			tool, ok := item.(map[string]any)
			if !ok {
				return fmt.Errorf("claude code bridge tool shape is invalid")
			}
			if _, exists := tool["function"]; exists || tool["type"] == "function" {
				return fmt.Errorf("claude code bridge body must use Anthropic messages tool shape")
			}
			name, _ := tool["name"].(string)
			_, schemaOK := tool["input_schema"].(map[string]any)
			name = strings.TrimSpace(name)
			if !schemaOK || !isAnthropicCompatSafeToolName(name) {
				return fmt.Errorf("claude code bridge tool shape is invalid")
			}
			toolNames[name] = struct{}{}
		}
	}
	if rawChoice, exists := payload["tool_choice"]; exists {
		choice, ok := rawChoice.(map[string]any)
		if !ok {
			return fmt.Errorf("claude code bridge tool choice shape is invalid")
		}
		if _, exists := choice["function"]; exists || choice["type"] == "function" {
			return fmt.Errorf("claude code bridge body must use Anthropic messages tool choice")
		}
		choiceType, _ := choice["type"].(string)
		switch choiceType {
		case "tool":
			name, _ := choice["name"].(string)
			name = strings.TrimSpace(name)
			if !isAnthropicCompatSafeToolName(name) {
				return fmt.Errorf("claude code bridge tool choice shape is invalid")
			}
			if _, ok := toolNames[name]; !ok {
				return fmt.Errorf("claude code bridge tool choice shape is invalid")
			}
		case "auto", "any", "none":
		default:
			return fmt.Errorf("claude code bridge tool choice shape is invalid")
		}
	}
	if err := validateClaudeCodeBridgeEffortBinding(decision, payload); err != nil {
		return err
	}
	return nil
}

func validateClaudeCodeBridgeEffortBinding(decision ClaudeCodeBridgeRouteDecision, payload map[string]any) error {
	outputConfig, ok := payload["output_config"].(map[string]any)
	if !ok {
		return nil
	}
	rawEffort, exists := outputConfig["effort"]
	if !exists {
		return nil
	}
	effort, ok := rawEffort.(string)
	if !ok {
		return fmt.Errorf("claude code bridge effort must be a string")
	}
	normalized := NormalizeClaudeOutputEffort(effort)
	if normalized == nil {
		return fmt.Errorf("claude code bridge unsupported effort")
	}
	levels := normalizedClaudeCodeBridgeReasoningEffortLevels(decision)
	if len(levels) == 0 {
		return fmt.Errorf("claude code bridge provider does not support effort")
	}
	if _, ok := levels[*normalized]; !ok {
		return fmt.Errorf("claude code bridge unsupported effort")
	}
	return nil
}

func normalizedClaudeCodeBridgeReasoningEffortLevels(decision ClaudeCodeBridgeRouteDecision) map[string]struct{} {
	policy := claudeCodeBridgeProviderEffortPolicy(decision.Provider)
	if len(policy) == 0 {
		return nil
	}
	levels := make(map[string]struct{}, len(decision.ReasoningEffortLevels))
	for _, raw := range decision.ReasoningEffortLevels {
		normalized := NormalizeClaudeOutputEffort(raw)
		if normalized == nil {
			continue
		}
		if _, ok := policy[*normalized]; !ok {
			continue
		}
		levels[*normalized] = struct{}{}
	}
	if len(levels) == 0 {
		return nil
	}
	return levels
}

func claudeCodeBridgeProviderEffortPolicy(provider string) map[string]struct{} {
	switch strings.TrimSpace(provider) {
	case "deepseek", "zai_glm":
		return map[string]struct{}{"high": {}, "max": {}}
	case "openai":
		return map[string]struct{}{"low": {}, "medium": {}, "high": {}, "xhigh": {}}
	default:
		return nil
	}
}

func validateClaudeCodeBridgeDecision(decision ClaudeCodeBridgeRouteDecision) error {
	if strings.TrimSpace(decision.ModelID) == "" || strings.TrimSpace(decision.Provider) == "" || strings.TrimSpace(decision.Route) == "" {
		return fmt.Errorf("claude code bridge route decision is incomplete")
	}
	if looksSensitiveText(claudeCodeBridgeEffectiveUpstreamModel(decision)) {
		return fmt.Errorf("claude code bridge upstream model is invalid")
	}
	if !strings.HasPrefix(decision.ClientType, "claude_code_bridge_") || decision.ClientType == ClaudeCodeNativeClientType {
		return fmt.Errorf("claude code bridge route decision cannot claim native")
	}
	if decision.NativeAttestationAllowed || decision.FormalPoolAllowed || decision.CredentialScope != ClaudeCodeBridgeCredentialScope {
		return fmt.Errorf("claude code bridge route decision cannot use formal pool")
	}
	return nil
}

func claudeCodeBridgeEffectiveUpstreamModel(decision ClaudeCodeBridgeRouteDecision) string {
	upstream := strings.TrimSpace(decision.UpstreamModel)
	if upstream == "" {
		upstream = strings.TrimSpace(decision.ModelID)
	}
	return upstream
}

func rewriteClaudeCodeBridgeAnthropicBodyModel(decision ClaudeCodeBridgeRouteDecision, body []byte) ([]byte, error) {
	upstreamModel := claudeCodeBridgeEffectiveUpstreamModel(decision)
	if upstreamModel == strings.TrimSpace(decision.ModelID) && !claudeCodeBridgeShouldInjectAnthropicCacheControl(decision) {
		return body, nil
	}
	var payload map[string]any
	if len(body) == 0 || json.Unmarshal(body, &payload) != nil {
		return nil, fmt.Errorf("claude code bridge model rewrite requires JSON body")
	}
	payload["model"] = upstreamModel
	if claudeCodeBridgeShouldInjectAnthropicCacheControl(decision) {
		injectClaudeCodeBridgeAnthropicCacheControl(payload)
	}
	rewritten, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return rewritten, nil
}

func claudeCodeBridgeShouldInjectAnthropicCacheControl(decision ClaudeCodeBridgeRouteDecision) bool {
	provider := strings.TrimSpace(decision.Provider)
	protocol := strings.TrimSpace(decision.PreferredProtocol)
	policy := strings.TrimSpace(decision.CachePolicy)
	return provider == "deepseek" && protocol == "anthropic_messages" && (strings.Contains(policy, "kv_cache") || strings.Contains(policy, "cache_audit"))
}

func injectClaudeCodeBridgeAnthropicCacheControl(payload map[string]any) {
	cacheControl := map[string]any{"type": "ephemeral"}
	switch system := payload["system"].(type) {
	case string:
		payload["system"] = []any{map[string]any{"type": "text", "text": system, "cache_control": cacheControl}}
	case []any:
		for _, item := range system {
			if block, ok := item.(map[string]any); ok && block["cache_control"] == nil {
				block["cache_control"] = cacheControl
			}
		}
	}
	if messages, ok := payload["messages"].([]any); ok {
		limit := len(messages) - 1
		if limit < 0 {
			limit = 0
		}
		for i := 0; i < limit; i++ {
			message, ok := messages[i].(map[string]any)
			if !ok {
				continue
			}
			switch content := message["content"].(type) {
			case string:
				message["content"] = []any{map[string]any{"type": "text", "text": content, "cache_control": cacheControl}}
			case []any:
				for _, item := range content {
					if block, ok := item.(map[string]any); ok && block["cache_control"] == nil {
						block["cache_control"] = cacheControl
					}
				}
			}
		}
	}
	if tools, ok := payload["tools"].([]any); ok {
		for _, item := range tools {
			if tool, ok := item.(map[string]any); ok && tool["cache_control"] == nil {
				tool["cache_control"] = cacheControl
			}
		}
	}
	if _, exists := payload["cache_control"]; !exists {
		payload["cache_control"] = cacheControl
	}
}

func claudeCodeBridgeSkeletonRequiresLive(body []byte) bool {
	var payload map[string]any
	if len(body) == 0 || json.Unmarshal(body, &payload) != nil {
		return false
	}
	if choice, ok := payload["tool_choice"].(map[string]any); ok {
		name, _ := choice["name"].(string)
		if strings.TrimSpace(name) == "multi_tool_use.parallel" {
			return true
		}
	}
	if tools, ok := payload["tools"].([]any); ok {
		if len(tools) > 1 {
			return true
		}
		for _, item := range tools {
			tool, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name, _ := tool["name"].(string)
			switch strings.TrimSpace(name) {
			case "multi_tool_use.parallel", "Agent":
				return true
			}
		}
	}
	return false
}

func claudeCodeBridgeSkeletonToolName(body []byte) string {
	var payload map[string]any
	if len(body) == 0 || json.Unmarshal(body, &payload) != nil {
		return ""
	}
	if choice, ok := payload["tool_choice"].(map[string]any); ok && choice["type"] == "tool" {
		if name, ok := choice["name"].(string); ok && isAnthropicCompatSafeToolName(name) {
			return strings.TrimSpace(name)
		}
	}
	if tools, ok := payload["tools"].([]any); ok && len(tools) > 0 {
		if tool, ok := tools[0].(map[string]any); ok {
			if name, ok := tool["name"].(string); ok && isAnthropicCompatSafeToolName(name) {
				return strings.TrimSpace(name)
			}
		}
	}
	return ""
}

func buildClaudeCodeBridgeTextSSE(model string, text string) []byte {
	return buildClaudeCodeBridgeTextSSEWithUsage(model, text, "end_turn", 1, 1)
}

func buildClaudeCodeBridgeTextSSEWithUsage(model string, text string, stopReason string, inputTokens int, outputTokens int) []byte {
	if inputTokens <= 0 {
		inputTokens = 1
	}
	if outputTokens <= 0 {
		outputTokens = 1
	}
	var buf bytes.Buffer
	writeClaudeCodeBridgeSSE(&buf, "message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_bridge_skeleton_cp5", "type": "message", "role": "assistant", "content": []any{}, "model": model, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]any{"input_tokens": inputTokens, "output_tokens": 0}}})
	writeClaudeCodeBridgeSSE(&buf, "content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
	writeClaudeCodeBridgeSSE(&buf, "content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": text}})
	writeClaudeCodeBridgeSSE(&buf, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	writeClaudeCodeBridgeSSE(&buf, "message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stopReason, "stop_sequence": nil}, "usage": map[string]any{"output_tokens": outputTokens}})
	writeClaudeCodeBridgeSSE(&buf, "message_stop", map[string]any{"type": "message_stop"})
	return buf.Bytes()
}

func buildClaudeCodeBridgeErrorSSE(errorType string, message string) []byte {
	var buf bytes.Buffer
	writeClaudeCodeBridgeSSE(&buf, "error", map[string]any{
		"type":  "error",
		"error": map[string]any{"type": safeClaudeCodeNativeLabel(errorType), "message": sanitizeClaudeCodeBridgeErrorMessage(message)},
	})
	return buf.Bytes()
}

func sanitizeClaudeCodeBridgeErrorMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return "provider bridge request failed"
	}
	message = claudeCodeBridgeSecretTokenRe.ReplaceAllString(message, "[redacted-token]")
	message = claudeCodeBridgeRequestIDRe.ReplaceAllString(message, "[redacted-request-id]")
	message = claudeCodeBridgeURLRe.ReplaceAllString(message, "[redacted-url]")
	return strings.ReplaceAll(message, "req_secret", "[redacted-request-id]")
}

func buildClaudeCodeBridgeToolUseSSE(model string, toolName string) []byte {
	partial, _ := json.Marshal(map[string]any{"city": "San Francisco"})
	var buf bytes.Buffer
	writeClaudeCodeBridgeSSE(&buf, "message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_bridge_skeleton_cp5", "type": "message", "role": "assistant", "content": []any{}, "model": model, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1}}})
	writeClaudeCodeBridgeSSE(&buf, "content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "tool_use", "id": "toolu_bridge_skeleton_cp5", "name": toolName, "input": map[string]any{}}})
	writeClaudeCodeBridgeSSE(&buf, "content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(partial)}})
	writeClaudeCodeBridgeSSE(&buf, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	writeClaudeCodeBridgeSSE(&buf, "message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "tool_use", "stop_sequence": nil}, "usage": map[string]any{"output_tokens": 1}})
	writeClaudeCodeBridgeSSE(&buf, "message_stop", map[string]any{"type": "message_stop"})
	return buf.Bytes()
}

func writeClaudeCodeBridgeSSE(buf *bytes.Buffer, event string, payload map[string]any) {
	raw, _ := json.Marshal(payload)
	buf.WriteString("event: ")
	buf.WriteString(event)
	buf.WriteString("\n")
	buf.WriteString("data: ")
	buf.Write(raw)
	buf.WriteString("\n\n")
}

func claudeCodeBridgeEnvEnabled(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on", "enabled":
		return true
	default:
		return false
	}
}

func claudeCodeBridgeCloneHTTPHeader(headers http.Header) http.Header {
	clone := http.Header{}
	for key, values := range headers {
		for _, value := range values {
			clone.Add(key, value)
		}
	}
	return clone
}

func applyClaudeCodeBridgeLiveResponseHeaders(dst io.Writer, headers http.Header) {
	headerWriter, ok := dst.(interface{ Header() http.Header })
	if !ok || headerWriter.Header() == nil {
		return
	}
	out := headerWriter.Header()
	out.Set("Content-Type", "text/event-stream")
	out.Set("Cache-Control", "no-cache")
	out.Set("X-Accel-Buffering", "no")
	for _, key := range []string{
		"Content-Type",
		"Cache-Control",
		"X-Request-Id",
		"X-Deepseek-Request-Id",
		"Request-Id",
	} {
		for _, value := range headers.Values(key) {
			if strings.TrimSpace(value) != "" {
				if key == "Content-Type" || key == "Cache-Control" {
					out.Set(key, value)
				} else {
					out.Add(key, value)
				}
			}
		}
	}
}

func copyClaudeCodeBridgeSSE(dst io.Writer, src io.Reader) (ClaudeCodeBridgeProviderFixture, error) {
	flusher, _ := dst.(interface{ Flush() })
	state := claudeCodeBridgeSSESanitizerState{indexMap: map[int]int{}}
	fixture := ClaudeCodeBridgeProviderFixture{}
	terminalSeen := false
	reader := bufio.NewReader(src)
	block := make([]string, 0, 8)
	for {
		line, readErr := reader.ReadString('\n')
		if line != "" {
			block = append(block, line)
			if strings.TrimRight(line, "\r\n") == "" {
				terminal, err := writeClaudeCodeBridgeSafeSSEBlock(dst, block, &state, &fixture)
				if err != nil {
					return fixture, err
				}
				terminalSeen = terminalSeen || terminal
				if flusher != nil {
					flusher.Flush()
				}
				block = block[:0]
			}
		}
		if readErr == io.EOF {
			if len(block) > 0 {
				terminal, err := writeClaudeCodeBridgeSafeSSEBlock(dst, block, &state, &fixture)
				if err != nil {
					return fixture, err
				}
				terminalSeen = terminalSeen || terminal
				if flusher != nil {
					flusher.Flush()
				}
			}
			if !terminalSeen {
				if _, err := dst.Write(buildClaudeCodeBridgeErrorSSE("upstream_stream_closed", "Anthropic-compatible bridge upstream stream closed before terminal event")); err != nil {
					return fixture, err
				}
				if flusher != nil {
					flusher.Flush()
				}
			}
			return fixture, nil
		}
		if readErr != nil {
			return fixture, readErr
		}
	}
}

type claudeCodeBridgeSSESanitizerState struct {
	indexMap  map[int]int
	nextIndex int
}

func writeClaudeCodeBridgeSafeSSEBlock(dst io.Writer, block []string, state *claudeCodeBridgeSSESanitizerState, fixture *ClaudeCodeBridgeProviderFixture) (bool, error) {
	payload, hasData := claudeCodeBridgeSSEDataPayload(block)
	if !hasData {
		_, err := io.WriteString(dst, strings.Join(block, ""))
		return false, err
	}
	if strings.TrimSpace(payload) == "[DONE]" {
		_, err := io.WriteString(dst, strings.Join(block, ""))
		return false, err
	}
	collectClaudeCodeBridgeProviderUsage(payload, fixture)
	var data any
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		return false, fmt.Errorf("claude code bridge upstream SSE data is not valid JSON: %w", err)
	}
	if safeError, ok := claudeCodeBridgeSafeUpstreamErrorSSE(block, data); ok {
		_, err := dst.Write(safeError)
		return true, err
	}
	if claudeCodeBridgeContainsForeignPrivateReasoning(data, "") {
		return false, nil
	}
	remapped, keep := remapClaudeCodeBridgeSSEIndex(data, state)
	if !keep {
		return false, nil
	}
	encoded, err := json.Marshal(remapped)
	if err != nil {
		return false, err
	}
	eventName := claudeCodeBridgeSSEEventName(block)
	payloadType := claudeCodeBridgeSSEPayloadType(remapped)
	if !claudeCodeBridgeSSEEventMatchesPayload(eventName, payloadType) {
		return false, nil
	}
	terminal := eventName == "message_stop" && payloadType == "message_stop"
	wroteData := false
	for _, line := range block {
		if strings.HasPrefix(line, "data:") {
			if !wroteData {
				if _, err := fmt.Fprintf(dst, "data: %s\n", encoded); err != nil {
					return false, err
				}
				wroteData = true
			}
			continue
		}
		if _, err := io.WriteString(dst, line); err != nil {
			return false, err
		}
	}
	return terminal, nil
}

func claudeCodeBridgeSafeUpstreamErrorSSE(block []string, data any) ([]byte, bool) {
	if claudeCodeBridgeSSEEventName(block) != "error" && claudeCodeBridgeSSEPayloadType(data) != "error" {
		return nil, false
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return buildClaudeCodeBridgeErrorSSE("upstream_error", "Anthropic-compatible bridge upstream returned an error"), true
	}
	parsed := gjson.ParseBytes(raw)
	errorType := safeClaudeCodeNativeLabel(parsed.Get("error.type").String())
	if errorType == "" {
		errorType = safeClaudeCodeNativeLabel(parsed.Get("error.code").String())
	}
	if errorType == "" {
		errorType = "upstream_error"
	}
	return buildClaudeCodeBridgeErrorSSE(errorType, "Anthropic-compatible bridge upstream returned an error"), true
}

func claudeCodeBridgeSSEEventName(block []string) string {
	for _, line := range block {
		if strings.HasPrefix(line, "event:") {
			return strings.TrimSpace(strings.TrimRight(strings.TrimPrefix(line, "event:"), "\r\n"))
		}
	}
	return ""
}

func claudeCodeBridgeSSEPayloadType(data any) string {
	obj, ok := data.(map[string]any)
	if !ok {
		return ""
	}
	eventType, _ := obj["type"].(string)
	return strings.TrimSpace(eventType)
}

func claudeCodeBridgeSSEEventMatchesPayload(eventName string, payloadType string) bool {
	eventName = strings.TrimSpace(eventName)
	payloadType = strings.TrimSpace(payloadType)
	if eventName == "" || payloadType == "" {
		return true
	}
	return eventName == payloadType
}

func collectClaudeCodeBridgeProviderUsage(payload string, fixture *ClaudeCodeBridgeProviderFixture) {
	if fixture == nil || strings.TrimSpace(payload) == "" || strings.TrimSpace(payload) == "[DONE]" {
		return
	}
	parsed := gjson.Parse(payload)
	collectClaudeCodeBridgeProviderUsageNode(parsed.Get("message.usage"), fixture)
	collectClaudeCodeBridgeProviderUsageNode(parsed.Get("usage"), fixture)
}

func collectClaudeCodeBridgeProviderUsageNode(usageNode gjson.Result, fixture *ClaudeCodeBridgeProviderFixture) {
	if !usageNode.Exists() {
		return
	}
	if v := usageNode.Get("cache_read_input_tokens"); v.Exists() && int(v.Int()) > fixture.CacheReadTokens {
		fixture.CacheReadTokens = int(v.Int())
	}
	if v := usageNode.Get("cached_tokens"); v.Exists() && fixture.CacheReadTokens == 0 && v.Int() > 0 {
		fixture.CacheReadTokens = int(v.Int())
	}
	if v := usageNode.Get("prompt_tokens_details.cached_tokens"); v.Exists() && fixture.CacheReadTokens == 0 && v.Int() > 0 {
		fixture.CacheReadTokens = int(v.Int())
	}
	if v := usageNode.Get("prompt_cache_hit_tokens"); v.Exists() && int(v.Int()) > fixture.CacheReadTokens {
		fixture.CacheReadTokens = int(v.Int())
	}
	if v := usageNode.Get("cache_creation_input_tokens"); v.Exists() && int(v.Int()) > fixture.CacheWriteTokens {
		fixture.CacheWriteTokens = int(v.Int())
	}
	if fixture.CacheWriteTokens == 0 {
		cc5m := usageNode.Get("cache_creation.ephemeral_5m_input_tokens").Int()
		cc1h := usageNode.Get("cache_creation.ephemeral_1h_input_tokens").Int()
		if total := int(cc5m + cc1h); total > 0 {
			fixture.CacheWriteTokens = total
		}
	}
	if v := usageNode.Get("prompt_cache_miss_tokens"); v.Exists() && int(v.Int()) > fixture.CacheMissTokens {
		fixture.CacheMissTokens = int(v.Int())
	}
}

func claudeCodeBridgeSSEDataPayload(block []string) (string, bool) {
	parts := make([]string, 0, 1)
	for _, line := range block {
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		value := strings.TrimRight(strings.TrimPrefix(line, "data:"), "\r\n")
		value = strings.TrimPrefix(value, " ")
		parts = append(parts, value)
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, "\n"), true
}

func claudeCodeBridgeContainsForeignPrivateReasoning(value any, path string) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalizedKey := strings.ToLower(strings.TrimSpace(key))
			childPath := claudeCodeBridgeSSEChildPath(path, normalizedKey)
			if claudeCodeBridgeForeignPrivateFieldPath(childPath) {
				return true
			}
			if normalizedKey == "type" && claudeCodeBridgeForeignPrivateTypePath(childPath) {
				if text, ok := child.(string); ok && claudeCodeBridgeForeignPrivateType(text) {
					return true
				}
			}
			if claudeCodeBridgeContainsForeignPrivateReasoning(child, childPath) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if claudeCodeBridgeContainsForeignPrivateReasoning(child, path+"[]") {
				return true
			}
		}
	}
	return false
}

func claudeCodeBridgeSSEChildPath(parent string, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

func claudeCodeBridgeForeignPrivateFieldPath(path string) bool {
	switch path {
	case "content_block.thinking",
		"content_block.signature",
		"delta.thinking",
		"delta.signature",
		"delta.reasoning_content",
		"reasoning_content":
		return true
	default:
		return false
	}
}

func claudeCodeBridgeForeignPrivateTypePath(path string) bool {
	switch path {
	case "content_block.type", "delta.type":
		return true
	default:
		return false
	}
}

func claudeCodeBridgeForeignPrivateType(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	return normalized == "thinking" ||
		normalized == "redacted_thinking" ||
		strings.Contains(normalized, "thinking_delta") ||
		strings.Contains(normalized, "signature_delta")
}

func remapClaudeCodeBridgeSSEIndex(value any, state *claudeCodeBridgeSSESanitizerState) (any, bool) {
	obj, ok := value.(map[string]any)
	if !ok {
		return value, true
	}
	eventType, _ := obj["type"].(string)
	rawIndex, hasIndex := claudeCodeBridgeSSEIndex(obj["index"])
	if !hasIndex {
		return value, true
	}
	mapped, found := state.indexMap[rawIndex]
	if !found {
		if eventType != "content_block_start" {
			return nil, false
		}
		mapped = state.nextIndex
		state.indexMap[rawIndex] = mapped
		state.nextIndex++
	}
	obj["index"] = mapped
	return obj, true
}

func claudeCodeBridgeSSEIndex(value any) (int, bool) {
	switch typed := value.(type) {
	case float64:
		index := int(typed)
		return index, typed == float64(index) && index >= 0
	case int:
		return typed, typed >= 0
	default:
		return 0, false
	}
}
