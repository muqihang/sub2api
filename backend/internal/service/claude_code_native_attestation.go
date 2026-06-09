package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/gjson"
)

const (
	ClaudeCodeNativeClientType        = "claude_code_native"
	ClaudeCodeUntrustedBetaClientType = "untrusted_beta"

	ClaudeCodeNativeInboundMessages    = "/v1/messages"
	ClaudeCodeNativeCCGatewayMessages  = "/v1/messages?beta=true"
	ClaudeCodeNativeInboundCountTokens = "/v1/messages/count_tokens"
	ClaudeCodeNativeCCGatewayCount     = "/v1/messages/count_tokens?beta=true"

	ClaudeCodeNativeClientTypeHeader         = "x-sub2api-client-type"
	ClaudeCodeNativeGuardAttestedHeader      = "x-sub2api-guard-attested"
	ClaudeCodeNativeGuardVersionHeader       = "x-sub2api-guard-version"
	ClaudeCodeNativeClaudeCodeVersionHeader  = "x-sub2api-claude-code-version"
	ClaudeCodeNativeLocalSessionRefHeader    = "x-sub2api-local-session-ref"
	ClaudeCodeNativeNetwatchRequiredHeader   = "x-sub2api-netwatch-required"
	ClaudeCodeNativeAttestationHeader        = "x-sub2api-native-attestation"
	ClaudeCodeNativeSignatureHeader          = "x-sub2api-native-signature"
	ClaudeCodeNativeInboundRouteHeader       = "x-sub2api-native-inbound-route"
	ClaudeCodeNativeCCGatewayRouteHeader     = "x-sub2api-native-cc-gateway-route"
	ClaudeCodeNativeServerFilledShapeHeader  = "x-sub2api-native-server-filled-shape"
	ClaudeCodeNativeHealthcheckProfileHeader = "x-sub2api-native-shape-healthcheck-profile"
	ClaudeCodeNativeToolSearchModeHeader     = "x-sub2api-native-tool-search-mode"
	ClaudeCodeNativeToolReferenceHeader      = "x-sub2api-native-tool-reference-present"
	ClaudeCodeNativeDeferLoadingHeader       = "x-sub2api-native-defer-loading-present"
	ClaudeCodeNativeEagerInputHeader         = "x-sub2api-native-eager-input-streaming-present"

	ClaudeCodeNativeDefaultScope              = "claude_code_native_takeover"
	ClaudeCodeNativeTakeoverHealthProfile     = "real_claude_code_native_takeover_v1"
	ClaudeCodeNativeToolSearchHealthProfile   = "real_claude_code_native_toolsearch_v1"
	ClaudeCodeNativeControlPlaneHealthProfile = "real_claude_code_native_control_plane_shadow_v1"
	ClaudeCodeNativeNetwatchHealthProfile     = "real_claude_code_native_netwatch_v1"
)

type ClaudeCodeNativeAuditSummary struct {
	ClientType                 string `json:"client_type"`
	NativeAttested             bool   `json:"native_attested"`
	GuardVersion               string `json:"guard_version,omitempty"`
	ClaudeCodeVersion          string `json:"claude_code_version,omitempty"`
	LocalSessionRef            string `json:"local_session_ref,omitempty"`
	InboundRoute               string `json:"inbound_route"`
	CCGatewayRoute             string `json:"cc_gateway_route"`
	NetwatchRequired           bool   `json:"netwatch_required"`
	ServerFilledShape          bool   `json:"server_filled_shape"`
	ShapeHealthcheckProfile    string `json:"shape_healthcheck_profile"`
	ToolSearchMode             string `json:"tool_search_mode"`
	ToolReferencePresent       bool   `json:"tool_reference_present"`
	DeferLoadingPresent        bool   `json:"defer_loading_present"`
	EagerInputStreamingPresent bool   `json:"eager_input_streaming_present"`
}

type claudeCodeNativeAuditSummaryContextKey struct{}

type ClaudeCodeNativeAttestationPayload struct {
	KeyID                   string `json:"key_id"`
	Scope                   string `json:"scope"`
	Version                 int    `json:"version"`
	IssuedAt                int64  `json:"issued_at"`
	Nonce                   string `json:"nonce"`
	Method                  string `json:"method"`
	RequestURI              string `json:"request_uri"`
	ClientType              string `json:"client_type"`
	GuardAttested           bool   `json:"guard_attested"`
	GuardVersion            string `json:"guard_version"`
	ClaudeCodeVersion       string `json:"claude_code_version"`
	LocalSessionRef         string `json:"local_session_ref"`
	NetwatchRequired        bool   `json:"netwatch_required"`
	ShapeHealthcheckProfile string `json:"shape_healthcheck_profile"`
}

type ClaudeCodeNativeAttestationConfig struct {
	CurrentKeyID string
	Keys         map[string]string
	Scope        string
	Version      int
	NonceTTL     time.Duration
	ClockSkew    time.Duration
}

type ClaudeCodeNativeNonceReplayCache struct {
	ttl   time.Duration
	nowFn func() time.Time
	mu    sync.Mutex
	seen  map[string]time.Time
}

type ClaudeCodeNativeAttestationService struct {
	nowFn       func() time.Time
	replayCache *ClaudeCodeNativeNonceReplayCache
}

type ClaudeCodeNativeAttestationOption func(*ClaudeCodeNativeAttestationService)

var claudeCodeNativeAttestationPayloadAllowedFields = map[string]struct{}{
	"key_id":                    {},
	"scope":                     {},
	"version":                   {},
	"issued_at":                 {},
	"nonce":                     {},
	"method":                    {},
	"request_uri":               {},
	"client_type":               {},
	"guard_attested":            {},
	"guard_version":             {},
	"claude_code_version":       {},
	"local_session_ref":         {},
	"netwatch_required":         {},
	"shape_healthcheck_profile": {},
}

var (
	claudeCodeNativeReplayCacheMu  sync.Mutex
	claudeCodeNativeReplayCacheTTL time.Duration
	claudeCodeNativeReplayCache    *ClaudeCodeNativeNonceReplayCache
)

func NewClaudeCodeNativeAttestationService(opts ...ClaudeCodeNativeAttestationOption) *ClaudeCodeNativeAttestationService {
	svc := &ClaudeCodeNativeAttestationService{nowFn: time.Now}
	for _, opt := range opts {
		if opt != nil {
			opt(svc)
		}
	}
	return svc
}

func WithClaudeCodeNativeAttestationNowFunc(nowFn func() time.Time) ClaudeCodeNativeAttestationOption {
	return func(svc *ClaudeCodeNativeAttestationService) {
		if nowFn != nil {
			svc.nowFn = nowFn
		}
	}
}

func WithClaudeCodeNativeAttestationReplayCache(cache *ClaudeCodeNativeNonceReplayCache) ClaudeCodeNativeAttestationOption {
	return func(svc *ClaudeCodeNativeAttestationService) {
		if cache != nil {
			svc.replayCache = cache
		}
	}
}

func NewClaudeCodeNativeNonceReplayCache(ttl time.Duration, nowFn func() time.Time) *ClaudeCodeNativeNonceReplayCache {
	if ttl <= 0 {
		ttl = 120 * time.Second
	}
	if nowFn == nil {
		nowFn = time.Now
	}
	return &ClaudeCodeNativeNonceReplayCache{ttl: ttl, nowFn: nowFn, seen: map[string]time.Time{}}
}

func (c *ClaudeCodeNativeNonceReplayCache) CheckAndRecord(keyID, scope, nonce string, now time.Time) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	current := now
	if current.IsZero() {
		current = c.nowFn()
	}
	for key, expiry := range c.seen {
		if !expiry.After(current) {
			delete(c.seen, key)
		}
	}
	replayKey := strings.Join([]string{scope, keyID, nonce}, ":")
	if expiry, ok := c.seen[replayKey]; ok && expiry.After(current) {
		return fmt.Errorf("claude code native attestation nonce replayed")
	}
	c.seen[replayKey] = current.Add(c.ttl)
	return nil
}

func WithClaudeCodeNativeAuditSummary(ctx context.Context, summary ClaudeCodeNativeAuditSummary) context.Context {
	return context.WithValue(ctx, claudeCodeNativeAuditSummaryContextKey{}, summary)
}

func ClaudeCodeNativeAuditSummaryFromContext(ctx context.Context) (ClaudeCodeNativeAuditSummary, bool) {
	summary, ok := ctx.Value(claudeCodeNativeAuditSummaryContextKey{}).(ClaudeCodeNativeAuditSummary)
	return summary, ok
}

func IsClaudeCodeNativeMarkerPresent(headers http.Header) bool {
	if headers == nil {
		return false
	}
	for _, key := range []string{
		ClaudeCodeNativeClientTypeHeader,
		ClaudeCodeNativeGuardAttestedHeader,
		ClaudeCodeNativeGuardVersionHeader,
		ClaudeCodeNativeClaudeCodeVersionHeader,
		ClaudeCodeNativeLocalSessionRefHeader,
		ClaudeCodeNativeNetwatchRequiredHeader,
		ClaudeCodeNativeAttestationHeader,
		ClaudeCodeNativeSignatureHeader,
	} {
		if strings.TrimSpace(headers.Get(key)) != "" {
			return true
		}
	}
	return false
}

func (s *ClaudeCodeNativeAttestationService) VerifyMessagesRequest(method, rawRoute string, headers http.Header, body []byte) (ClaudeCodeNativeAuditSummary, error) {
	if s == nil {
		s = NewClaudeCodeNativeAttestationService()
	}
	clientType := strings.TrimSpace(headers.Get(ClaudeCodeNativeClientTypeHeader))
	if clientType == ClaudeCodeUntrustedBetaClientType {
		return ClaudeCodeNativeAuditSummary{}, fmt.Errorf("untrusted beta request is not native attested")
	}
	if clientType != ClaudeCodeNativeClientType {
		return ClaudeCodeNativeAuditSummary{}, fmt.Errorf("claude code native attestation client type is required")
	}
	if !headerTruthy(headers.Get(ClaudeCodeNativeGuardAttestedHeader)) {
		return ClaudeCodeNativeAuditSummary{}, fmt.Errorf("claude code native attestation guard flag is required")
	}
	encoded := strings.TrimSpace(headers.Get(ClaudeCodeNativeAttestationHeader))
	signature := strings.TrimSpace(headers.Get(ClaudeCodeNativeSignatureHeader))
	if encoded == "" || signature == "" {
		return ClaudeCodeNativeAuditSummary{}, fmt.Errorf("claude code native attestation is required")
	}
	cfg, err := loadClaudeCodeNativeAttestationConfigFromEnv()
	if err != nil {
		return ClaudeCodeNativeAuditSummary{}, err
	}
	if s.replayCache == nil {
		s.replayCache = sharedClaudeCodeNativeNonceReplayCache(cfg.NonceTTL, s.nowFn)
	}
	payload, err := decodeClaudeCodeNativeAttestationPayload(encoded)
	if err != nil {
		return ClaudeCodeNativeAuditSummary{}, err
	}
	if err := validateClaudeCodeNativeAttestationPayloadShape(payload); err != nil {
		return ClaudeCodeNativeAuditSummary{}, err
	}
	if payload.Scope != cfg.Scope || payload.Version != cfg.Version {
		return ClaudeCodeNativeAuditSummary{}, fmt.Errorf("claude code native attestation scope/version mismatch")
	}
	secret, ok := cfg.Keys[payload.KeyID]
	if !ok {
		return ClaudeCodeNativeAuditSummary{}, fmt.Errorf("claude code native attestation key id is not configured")
	}
	if !hmac.Equal([]byte(signClaudeCodeNativeAttestation(encoded, method, rawRoute, body, secret)), []byte(signature)) {
		return ClaudeCodeNativeAuditSummary{}, fmt.Errorf("claude code native attestation signature mismatch")
	}
	current := s.nowFn()
	if current.IsZero() {
		current = time.Now()
	}
	issuedAt := time.Unix(payload.IssuedAt, 0)
	if issuedAt.Before(current.Add(-cfg.NonceTTL)) {
		return ClaudeCodeNativeAuditSummary{}, fmt.Errorf("claude code native attestation timestamp expired")
	}
	if math.Abs(float64(current.Unix()-payload.IssuedAt)) > cfg.ClockSkew.Seconds() {
		return ClaudeCodeNativeAuditSummary{}, fmt.Errorf("claude code native attestation timestamp is outside the clock skew window")
	}
	if err := s.replayCache.CheckAndRecord(payload.KeyID, payload.Scope, payload.Nonce, current); err != nil {
		return ClaudeCodeNativeAuditSummary{}, err
	}
	if err := compareClaudeCodeNativeRequest(method, rawRoute, payload); err != nil {
		return ClaudeCodeNativeAuditSummary{}, err
	}
	if err := validateClaudeCodeNativeSafeRefs(payload, headers); err != nil {
		return ClaudeCodeNativeAuditSummary{}, err
	}
	return buildClaudeCodeNativeAuditSummary(payload, body), nil
}

func ApplyClaudeCodeNativeAuditHeaders(headers http.Header, audit ClaudeCodeNativeAuditSummary) error {
	if headers == nil {
		return fmt.Errorf("headers are required")
	}
	if audit.ClientType != ClaudeCodeNativeClientType || !audit.NativeAttested {
		return fmt.Errorf("claude code native audit summary is not attested")
	}
	setHeaderRaw(headers, ClaudeCodeNativeClientTypeHeader, ClaudeCodeNativeClientType)
	setHeaderRaw(headers, ClaudeCodeNativeGuardAttestedHeader, strconv.FormatBool(audit.NativeAttested))
	setHeaderRaw(headers, ClaudeCodeNativeGuardVersionHeader, audit.GuardVersion)
	setHeaderRaw(headers, ClaudeCodeNativeClaudeCodeVersionHeader, audit.ClaudeCodeVersion)
	setHeaderRaw(headers, ClaudeCodeNativeLocalSessionRefHeader, audit.LocalSessionRef)
	setHeaderRaw(headers, ClaudeCodeNativeNetwatchRequiredHeader, strconv.FormatBool(audit.NetwatchRequired))
	setHeaderRaw(headers, ClaudeCodeNativeInboundRouteHeader, audit.InboundRoute)
	setHeaderRaw(headers, ClaudeCodeNativeCCGatewayRouteHeader, audit.CCGatewayRoute)
	setHeaderRaw(headers, ClaudeCodeNativeServerFilledShapeHeader, strconv.FormatBool(audit.ServerFilledShape))
	setHeaderRaw(headers, ClaudeCodeNativeHealthcheckProfileHeader, audit.ShapeHealthcheckProfile)
	setHeaderRaw(headers, ClaudeCodeNativeToolSearchModeHeader, audit.ToolSearchMode)
	setHeaderRaw(headers, ClaudeCodeNativeToolReferenceHeader, strconv.FormatBool(audit.ToolReferencePresent))
	setHeaderRaw(headers, ClaudeCodeNativeDeferLoadingHeader, strconv.FormatBool(audit.DeferLoadingPresent))
	setHeaderRaw(headers, ClaudeCodeNativeEagerInputHeader, strconv.FormatBool(audit.EagerInputStreamingPresent))
	return nil
}

func ApplyClaudeCodePathAuditHeaders(headers http.Header, ctx context.Context) error {
	native, hasNative := ClaudeCodeNativeAuditSummaryFromContext(ctx)
	compat, hasCompat := AnthropicCompatAuditSummaryFromContext(ctx)
	if hasNative && hasCompat {
		return fmt.Errorf("claude code native and compat audit summaries are mutually exclusive")
	}
	if hasNative {
		return ApplyClaudeCodeNativeAuditHeaders(headers, native)
	}
	if hasCompat {
		applyAnthropicCompatAuditHeaders(headers, compat)
	}
	return nil
}

func preserveClaudeCodeNativeWireBody(ctx context.Context, req *http.Request, originalBody []byte) {
	if req == nil || len(originalBody) == 0 {
		return
	}
	native, ok := ClaudeCodeNativeAuditSummaryFromContext(ctx)
	if !ok || !native.NativeAttested {
		return
	}
	claudeCodeReplaceRequestBody(req, originalBody)
	deleteHeaderAllForms(req.Header, "X-Claude-Code-Session-Id")
}

type ClaudeCodeNativeDirectedHealthcheckEvidence struct {
	Profile           string
	TemporaryKey      bool
	SingleAccountPin  bool
	CCGatewaySeen     bool
	RawCapturePresent bool
	AccountRef        string
	EgressBucket      string
	VerifierSummary   string
	Fresh             bool
	StatusCode        int
}

type ClaudeCodeNativeDirectedHealthcheckDecision struct {
	HealthcheckPassed    bool
	CanPromoteProduction bool
	NextState            string
	Reason               string
}

func EvaluateClaudeCodeNativeDirectedHealthcheckBoundary(e ClaudeCodeNativeDirectedHealthcheckEvidence) ClaudeCodeNativeDirectedHealthcheckDecision {
	if !e.TemporaryKey {
		return ClaudeCodeNativeDirectedHealthcheckDecision{Reason: "temporary healthcheck key required"}
	}
	if !e.SingleAccountPin {
		return ClaudeCodeNativeDirectedHealthcheckDecision{Reason: "single-account pin required"}
	}
	if !e.CCGatewaySeen || !e.RawCapturePresent || !isSafeLedgerRef(e.AccountRef) || strings.TrimSpace(e.EgressBucket) == "" || strings.TrimSpace(e.VerifierSummary) == "" || !e.Fresh {
		return ClaudeCodeNativeDirectedHealthcheckDecision{Reason: "fresh cc gateway evidence required"}
	}
	if e.StatusCode != http.StatusOK {
		return ClaudeCodeNativeDirectedHealthcheckDecision{Reason: statusBucketFromHTTP(e.StatusCode)}
	}
	return ClaudeCodeNativeDirectedHealthcheckDecision{HealthcheckPassed: true, CanPromoteProduction: false, NextState: "healthcheck_passed", Reason: "native profile evidence only; warming/promote remain separate actions"}
}

func loadClaudeCodeNativeAttestationConfigFromEnv() (*ClaudeCodeNativeAttestationConfig, error) {
	currentKeyID := strings.TrimSpace(os.Getenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_CURRENT_KEY_ID"))
	if currentKeyID == "" {
		currentKeyID = "guard_v1"
	}
	keys := map[string]string{}
	if raw := strings.TrimSpace(os.Getenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_KEYS_JSON")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &keys); err != nil {
			return nil, fmt.Errorf("claude code native attestation key set is invalid")
		}
	} else {
		secret := strings.TrimSpace(os.Getenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET"))
		if secret == "" {
			return nil, fmt.Errorf("claude code native attestation explicit secret is required")
		}
		keys[currentKeyID] = secret
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("claude code native attestation key set is empty")
	}
	return &ClaudeCodeNativeAttestationConfig{
		CurrentKeyID: currentKeyID,
		Keys:         keys,
		Scope:        controlPlaneAttestationFirstNonEmpty(strings.TrimSpace(os.Getenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SCOPE")), ClaudeCodeNativeDefaultScope),
		Version:      controlPlaneAttestationFirstPositiveEnvInt("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_VERSION", 1),
		NonceTTL:     time.Duration(controlPlaneAttestationFirstPositiveEnvInt("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_NONCE_TTL_SECONDS", 120)) * time.Second,
		ClockSkew:    time.Duration(controlPlaneAttestationFirstPositiveEnvInt("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_CLOCK_SKEW_SECONDS", 30)) * time.Second,
	}, nil
}

func decodeClaudeCodeNativeAttestationPayload(encoded string) (*ClaudeCodeNativeAttestationPayload, error) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("claude code native attestation payload is malformed")
	}
	var fieldSet map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fieldSet); err != nil {
		return nil, fmt.Errorf("claude code native attestation payload is malformed")
	}
	for field := range fieldSet {
		if _, ok := claudeCodeNativeAttestationPayloadAllowedFields[field]; !ok {
			return nil, fmt.Errorf("claude code native attestation payload must match the strict allowlist schema")
		}
	}
	if len(fieldSet) != len(claudeCodeNativeAttestationPayloadAllowedFields) {
		return nil, fmt.Errorf("claude code native attestation payload must match the strict allowlist schema")
	}
	var payload ClaudeCodeNativeAttestationPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("claude code native attestation payload is malformed")
	}
	return &payload, nil
}

func validateClaudeCodeNativeAttestationPayloadShape(payload *ClaudeCodeNativeAttestationPayload) error {
	if payload == nil {
		return fmt.Errorf("claude code native attestation payload is required")
	}
	if strings.TrimSpace(payload.KeyID) == "" || strings.TrimSpace(payload.Nonce) == "" || payload.IssuedAt <= 0 || payload.Version <= 0 {
		return fmt.Errorf("claude code native attestation payload shape is invalid")
	}
	if payload.ClientType != ClaudeCodeNativeClientType || !payload.GuardAttested {
		return fmt.Errorf("claude code native attestation payload is not native")
	}
	if !isKnownClaudeCodeNativeHealthProfile(payload.ShapeHealthcheckProfile) {
		return fmt.Errorf("claude code native healthcheck profile is invalid")
	}
	return nil
}

func compareClaudeCodeNativeRequest(method, rawRoute string, payload *ClaudeCodeNativeAttestationPayload) error {
	if strings.ToUpper(strings.TrimSpace(method)) != http.MethodPost || strings.ToUpper(payload.Method) != http.MethodPost {
		return fmt.Errorf("claude code native attestation method mismatch")
	}
	if payload.RequestURI != rawRoute {
		return fmt.Errorf("claude code native attestation route mismatch")
	}
	path, query := splitCompatRoute(rawRoute)
	switch path {
	case ClaudeCodeNativeInboundMessages, ClaudeCodeNativeInboundCountTokens:
	default:
		return fmt.Errorf("claude code native attestation route unsupported")
	}
	if query != "" && query != "beta=true" {
		return fmt.Errorf("claude code native attestation route unsupported")
	}
	return nil
}

func validateClaudeCodeNativeSafeRefs(payload *ClaudeCodeNativeAttestationPayload, headers http.Header) error {
	if !isSafeLedgerRef(payload.LocalSessionRef) {
		return fmt.Errorf("claude code native local session ref must be opaque")
	}
	if strings.TrimSpace(headers.Get(ClaudeCodeNativeLocalSessionRefHeader)) != "" && headers.Get(ClaudeCodeNativeLocalSessionRefHeader) != payload.LocalSessionRef {
		return fmt.Errorf("claude code native session ref header mismatch")
	}
	if !headerTruthy(headers.Get(ClaudeCodeNativeNetwatchRequiredHeader)) || !payload.NetwatchRequired {
		return fmt.Errorf("claude code native netwatch is required")
	}
	return nil
}

func buildClaudeCodeNativeAuditSummary(payload *ClaudeCodeNativeAttestationPayload, body []byte) ClaudeCodeNativeAuditSummary {
	root := gjson.ParseBytes(body)
	toolMode := "not_present"
	if tools := root.Get("tools"); tools.IsArray() && len(tools.Array()) > 0 {
		toolMode = "truthful_pass_through"
	}
	return ClaudeCodeNativeAuditSummary{
		ClientType:                 ClaudeCodeNativeClientType,
		NativeAttested:             true,
		GuardVersion:               safeClaudeCodeNativeLabel(payload.GuardVersion),
		ClaudeCodeVersion:          safeClaudeCodeNativeLabel(payload.ClaudeCodeVersion),
		LocalSessionRef:            payload.LocalSessionRef,
		InboundRoute:               claudeCodeNativeInboundRoute(payload.RequestURI),
		CCGatewayRoute:             claudeCodeNativeCCGatewayRoute(payload.RequestURI),
		NetwatchRequired:           true,
		ServerFilledShape:          false,
		ShapeHealthcheckProfile:    payload.ShapeHealthcheckProfile,
		ToolSearchMode:             toolMode,
		ToolReferencePresent:       root.Get("..tool_reference").Exists() || toolMode == "truthful_pass_through",
		DeferLoadingPresent:        root.Get("..defer_loading").Exists(),
		EagerInputStreamingPresent: root.Get("..eager_input_streaming").Exists(),
	}
}

func signClaudeCodeNativeAttestation(encoded, method, rawRoute string, body []byte, secret string) string {
	digest := sha256.Sum256(body)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(encoded))
	_, _ = mac.Write([]byte("\n"))
	_, _ = mac.Write([]byte(strings.ToUpper(strings.TrimSpace(method))))
	_, _ = mac.Write([]byte("\n"))
	_, _ = mac.Write([]byte(rawRoute))
	_, _ = mac.Write([]byte("\n"))
	_, _ = mac.Write([]byte(hex.EncodeToString(digest[:])))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func isKnownClaudeCodeNativeHealthProfile(profile string) bool {
	switch strings.TrimSpace(profile) {
	case ClaudeCodeNativeTakeoverHealthProfile, ClaudeCodeNativeToolSearchHealthProfile, ClaudeCodeNativeControlPlaneHealthProfile, ClaudeCodeNativeNetwatchHealthProfile:
		return true
	default:
		return false
	}
}

func safeClaudeCodeNativeLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || looksSensitiveText(value) || looksPlainDigest(value) || looksUnsafeDynamicIdentifier(value) {
		return ""
	}
	if len(value) > 64 {
		return ""
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return ""
	}
	return value
}

func claudeCodeNativeInboundRoute(rawRoute string) string {
	path, _ := splitCompatRoute(rawRoute)
	if path == ClaudeCodeNativeInboundCountTokens {
		return ClaudeCodeNativeInboundCountTokens
	}
	return ClaudeCodeNativeInboundMessages
}

func claudeCodeNativeCCGatewayRoute(rawRoute string) string {
	path, _ := splitCompatRoute(rawRoute)
	if path == ClaudeCodeNativeInboundCountTokens {
		return ClaudeCodeNativeCCGatewayCount
	}
	return ClaudeCodeNativeCCGatewayMessages
}

func sharedClaudeCodeNativeNonceReplayCache(ttl time.Duration, nowFn func() time.Time) *ClaudeCodeNativeNonceReplayCache {
	claudeCodeNativeReplayCacheMu.Lock()
	defer claudeCodeNativeReplayCacheMu.Unlock()
	if claudeCodeNativeReplayCache == nil || claudeCodeNativeReplayCacheTTL != ttl {
		claudeCodeNativeReplayCache = NewClaudeCodeNativeNonceReplayCache(ttl, nowFn)
		claudeCodeNativeReplayCacheTTL = ttl
	}
	return claudeCodeNativeReplayCache
}
