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
	"regexp"
	"sort"
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
	ClaudeCodeNativeRuntimeHashHeader        = "x-sub2api-native-runtime-hash"
	ClaudeCodeNativeOverlayHashHeader        = "x-sub2api-native-overlay-hash"
	ClaudeCodeNativeCatalogHashHeader        = "x-sub2api-native-catalog-hash"
	ClaudeCodeNativeCatalogVersionHeader     = "x-sub2api-route-catalog-version"

	ClaudeCodeNativeDefaultScope              = "claude_code_native_takeover"
	ClaudeCodeNativeTakeoverHealthProfile     = "real_claude_code_native_takeover_v1"
	ClaudeCodeNativeToolSearchHealthProfile   = "real_claude_code_native_toolsearch_v1"
	ClaudeCodeNativeControlPlaneHealthProfile = "real_claude_code_native_control_plane_shadow_v1"
	ClaudeCodeNativeNetwatchHealthProfile     = "real_claude_code_native_netwatch_v1"

	ClaudeCodeNativeRoute           = "claude_code_native"
	ClaudeCodeNativeProviderOwner   = "zhumeng_managed"
	ClaudeCodeNativeCredentialScope = "formal_pool"
	ClaudeCodeNativeGatewayLocation = "cloud"
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
	RuntimeHash                string `json:"runtime_hash,omitempty"`
	OverlayHash                string `json:"overlay_hash,omitempty"`
	CatalogHash                string `json:"catalog_hash,omitempty"`
	CatalogVersion             string `json:"catalog_version,omitempty"`
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
	Route                   string `json:"route"`
	ModelID                 string `json:"model_id"`
	ProviderOwner           string `json:"provider_owner"`
	CredentialScope         string `json:"credential_scope"`
	GatewayLocation         string `json:"gateway_location"`
	RuntimeHash             string `json:"runtime_hash"`
	OverlayHash             string `json:"overlay_hash"`
	CatalogHash             string `json:"catalog_hash"`
	CatalogVersion          string `json:"catalog_version"`
	SessionRef              string `json:"session_ref"`
	BodyShapeHash           string `json:"body_shape_hash"`
}

type ClaudeCodeRouteHintPayload struct {
	KeyID                    string `json:"key_id"`
	Scope                    string `json:"scope"`
	Version                  int    `json:"version"`
	IssuedAt                 int64  `json:"issued_at"`
	ExpiresAt                int64  `json:"expires_at"`
	Nonce                    string `json:"nonce"`
	Method                   string `json:"method"`
	RequestURI               string `json:"request_uri"`
	ModelID                  string `json:"model_id"`
	BodyModel                string `json:"body_model"`
	BodySHA256               string `json:"body_sha256"`
	RuntimeHash              string `json:"runtime_hash"`
	OverlayHash              string `json:"overlay_hash"`
	CatalogHash              string `json:"catalog_hash"`
	CatalogVersion           string `json:"catalog_version"`
	SessionRef               string `json:"session_ref"`
	Route                    string `json:"route"`
	ClientType               string `json:"client_type"`
	Provider                 string `json:"provider"`
	LiveRequestAllowed       bool   `json:"live_request_allowed"`
	FormalPoolAllowed        bool   `json:"formal_pool_allowed"`
	NativeAttestationAllowed bool   `json:"native_attestation_allowed"`
	ProviderOwner            string `json:"provider_owner"`
	CredentialScope          string `json:"credential_scope"`
	GatewayLocation          string `json:"gateway_location"`
}

type ClaudeCodeNativeAttestationConfig struct {
	CurrentKeyID  string
	Keys          map[string]string
	Scope         string
	Version       int
	NonceTTL      time.Duration
	ClockSkew     time.Duration
	RuntimeHashes map[string]struct{}
	OverlayHashes map[string]struct{}
	CatalogHashes map[string]struct{}
}

type ClaudeCodeRouteHintConfig struct {
	CurrentKeyID string
	Keys         map[string]string
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
	nowFn                    func() time.Time
	replayCache              *ClaudeCodeNativeNonceReplayCache
	routeHintReplayCache     *ClaudeCodeNativeNonceReplayCache
	catalogAdmissionResolver claudeCodeNativeCatalogAdmissionResolver
}

type ClaudeCodeNativeAttestationOption func(*ClaudeCodeNativeAttestationService)

type claudeCodeNativeCatalogAdmissionDecision struct {
	ModelID         string `json:"model_id"`
	Route           string `json:"route"`
	ProviderOwner   string `json:"provider_owner"`
	CredentialScope string `json:"credential_scope"`
	GatewayLocation string `json:"gateway_location"`
	RuntimeHash     string `json:"runtime_hash,omitempty"`
	OverlayHash     string `json:"overlay_hash,omitempty"`
	CatalogHash     string `json:"catalog_hash,omitempty"`
	CatalogVersion  string `json:"catalog_version,omitempty"`
	CatalogFresh    bool   `json:"catalog_fresh"`
}

type claudeCodeNativeCatalogAdmissionResolver interface {
	ResolveClaudeCodeNativeCatalogAdmission(model string) (claudeCodeNativeCatalogAdmissionDecision, error)
}

type claudeCodeNativeEnvCatalogAdmissionResolver struct{}

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
	"route":                     {},
	"model_id":                  {},
	"provider_owner":            {},
	"credential_scope":          {},
	"gateway_location":          {},
	"runtime_hash":              {},
	"overlay_hash":              {},
	"catalog_hash":              {},
	"catalog_version":           {},
	"session_ref":               {},
	"body_shape_hash":           {},
}

var claudeCodeRouteHintPayloadAllowedFields = map[string]struct{}{
	"key_id":                     {},
	"scope":                      {},
	"version":                    {},
	"issued_at":                  {},
	"expires_at":                 {},
	"nonce":                      {},
	"method":                     {},
	"request_uri":                {},
	"model_id":                   {},
	"body_model":                 {},
	"body_sha256":                {},
	"runtime_hash":               {},
	"overlay_hash":               {},
	"catalog_hash":               {},
	"catalog_version":            {},
	"session_ref":                {},
	"route":                      {},
	"client_type":                {},
	"provider":                   {},
	"live_request_allowed":       {},
	"formal_pool_allowed":        {},
	"native_attestation_allowed": {},
	"provider_owner":             {},
	"credential_scope":           {},
	"gateway_location":           {},
}

var (
	claudeCodeNativeSafeHashRe        = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	claudeCodeNativeUnknownHash       = "sha256:" + strings.Repeat("0", 64)
	claudeCodeNativeReplayCacheMu     sync.Mutex
	claudeCodeNativeReplayCacheTTL    time.Duration
	claudeCodeNativeReplayCache       *ClaudeCodeNativeNonceReplayCache
	claudeCodeRouteHintReplayCacheMu  sync.Mutex
	claudeCodeRouteHintReplayCacheTTL time.Duration
	claudeCodeRouteHintReplayCache    *ClaudeCodeNativeNonceReplayCache
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

func withClaudeCodeNativeCatalogAdmissionResolver(resolver claudeCodeNativeCatalogAdmissionResolver) ClaudeCodeNativeAttestationOption {
	return func(svc *ClaudeCodeNativeAttestationService) {
		if resolver != nil {
			svc.catalogAdmissionResolver = resolver
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
	if strings.TrimSpace(headers.Get(ClaudeCodeNativeClientTypeHeader)) == ClaudeCodeNativeClientType {
		return true
	}
	for _, key := range []string{
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

func IsClaudeCodeBridgeMarkerPresent(headers http.Header) bool {
	if headers == nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(headers.Get(ClaudeCodeNativeClientTypeHeader)), "claude_code_bridge_")
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
	if err := validateClaudeCodeNativeTrustedBindings(payload, cfg); err != nil {
		return ClaudeCodeNativeAuditSummary{}, err
	}
	if err := validateClaudeCodeNativeSafeRefs(payload, headers); err != nil {
		return ClaudeCodeNativeAuditSummary{}, err
	}
	if err := validateClaudeCodeNativeBodyBinding(payload, body); err != nil {
		return ClaudeCodeNativeAuditSummary{}, err
	}
	catalogAdmission, err := s.resolveClaudeCodeNativeCatalogAdmission(strings.TrimSpace(gjson.GetBytes(body, "model").String()))
	if err != nil {
		return ClaudeCodeNativeAuditSummary{}, err
	}
	if err := validateClaudeCodeNativeCatalogAdmission(payload.ModelID, payload, headers, catalogAdmission); err != nil {
		return ClaudeCodeNativeAuditSummary{}, err
	}
	return buildClaudeCodeNativeAuditSummaryWithHeaders(payload, headers, body), nil
}

func (s *ClaudeCodeNativeAttestationService) VerifyBridgeRouteHintRequest(method, rawRoute string, headers http.Header, body []byte, decision ClaudeCodeProviderRouteDecision) (*ClaudeCodeRouteHintPayload, error) {
	if s == nil {
		s = NewClaudeCodeNativeAttestationService()
	}
	encoded := strings.TrimSpace(headers.Get(ClaudeCodeRouteHintHeader))
	signature := strings.TrimSpace(headers.Get(ClaudeCodeRouteHintSignatureHeader))
	if encoded == "" || signature == "" {
		return nil, fmt.Errorf("claude code route hint is required")
	}
	cfg, err := loadClaudeCodeRouteHintConfigFromEnv()
	if err != nil {
		return nil, err
	}
	if s.routeHintReplayCache == nil {
		s.routeHintReplayCache = sharedClaudeCodeRouteHintNonceReplayCache(cfg.NonceTTL, s.nowFn)
	}
	payload, err := decodeClaudeCodeRouteHintPayload(encoded)
	if err != nil {
		return nil, err
	}
	if err := validateClaudeCodeRouteHintPayloadShape(payload); err != nil {
		return nil, err
	}
	if payload.Scope != ClaudeCodeRouteHintScope || payload.Version != ClaudeCodeRouteHintVersion {
		return nil, fmt.Errorf("claude code route hint scope/version mismatch")
	}
	secret, ok := cfg.Keys[payload.KeyID]
	if !ok {
		return nil, fmt.Errorf("claude code route hint key id is not configured")
	}
	if !hmac.Equal([]byte(signClaudeCodeRouteHint(encoded, method, rawRoute, body, secret)), []byte(signature)) {
		return nil, fmt.Errorf("claude code route hint signature mismatch")
	}
	current := s.nowFn()
	if current.IsZero() {
		current = time.Now()
	}
	issuedAt := time.Unix(payload.IssuedAt, 0)
	expiresAt := time.Unix(payload.ExpiresAt, 0)
	if payload.ExpiresAt <= current.Unix() ||
		payload.IssuedAt > current.Add(cfg.ClockSkew).Unix() ||
		issuedAt.Before(current.Add(-cfg.NonceTTL)) ||
		expiresAt.After(issuedAt.Add(cfg.NonceTTL)) {
		return nil, fmt.Errorf("claude code route hint stale")
	}
	if err := s.routeHintReplayCache.CheckAndRecord(payload.KeyID, payload.Scope, payload.Nonce, current); err != nil {
		return nil, err
	}
	if err := validateClaudeCodeRouteHintBinding(method, rawRoute, body, payload, decision); err != nil {
		return nil, err
	}
	return payload, nil
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
	setHeaderRaw(headers, ClaudeCodeNativeRuntimeHashHeader, audit.RuntimeHash)
	setHeaderRaw(headers, ClaudeCodeNativeOverlayHashHeader, audit.OverlayHash)
	setHeaderRaw(headers, ClaudeCodeNativeCatalogHashHeader, audit.CatalogHash)
	setHeaderRaw(headers, ClaudeCodeNativeCatalogVersionHeader, audit.CatalogVersion)
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
	runtimeHashes, err := parseClaudeCodeNativeHashAllowlistEnv("SUB2API_CLAUDE_CODE_NATIVE_RUNTIME_HASHES")
	if err != nil {
		return nil, err
	}
	overlayHashes, err := parseClaudeCodeNativeHashAllowlistEnv("SUB2API_CLAUDE_CODE_NATIVE_OVERLAY_HASHES")
	if err != nil {
		return nil, err
	}
	catalogHashes, err := parseClaudeCodeNativeHashAllowlistEnv("SUB2API_CLAUDE_CODE_NATIVE_CATALOG_HASHES")
	if err != nil {
		return nil, err
	}
	return &ClaudeCodeNativeAttestationConfig{
		CurrentKeyID:  currentKeyID,
		Keys:          keys,
		Scope:         controlPlaneAttestationFirstNonEmpty(strings.TrimSpace(os.Getenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SCOPE")), ClaudeCodeNativeDefaultScope),
		Version:       controlPlaneAttestationFirstPositiveEnvInt("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_VERSION", 1),
		NonceTTL:      time.Duration(controlPlaneAttestationFirstPositiveEnvInt("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_NONCE_TTL_SECONDS", 120)) * time.Second,
		ClockSkew:     time.Duration(controlPlaneAttestationFirstPositiveEnvInt("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_CLOCK_SKEW_SECONDS", 30)) * time.Second,
		RuntimeHashes: runtimeHashes,
		OverlayHashes: overlayHashes,
		CatalogHashes: catalogHashes,
	}, nil
}

func loadClaudeCodeRouteHintConfigFromEnv() (*ClaudeCodeRouteHintConfig, error) {
	currentKeyID := strings.TrimSpace(os.Getenv("SUB2API_CLAUDE_CODE_ROUTE_HINT_CURRENT_KEY_ID"))
	if currentKeyID == "" {
		currentKeyID = "route_hint_v1"
	}
	keys := map[string]string{}
	if raw := strings.TrimSpace(os.Getenv("SUB2API_CLAUDE_CODE_ROUTE_HINT_KEYS_JSON")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &keys); err != nil {
			return nil, fmt.Errorf("claude code route hint key set is invalid")
		}
	} else {
		secret := strings.TrimSpace(os.Getenv("SUB2API_CLAUDE_CODE_ROUTE_HINT_SECRET"))
		if secret == "" {
			return nil, fmt.Errorf("claude code route hint explicit secret is required")
		}
		keys[currentKeyID] = secret
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("claude code route hint key set is empty")
	}
	return &ClaudeCodeRouteHintConfig{
		CurrentKeyID: currentKeyID,
		Keys:         keys,
		NonceTTL:     time.Duration(controlPlaneAttestationFirstPositiveEnvInt("SUB2API_CLAUDE_CODE_ROUTE_HINT_NONCE_TTL_SECONDS", 60)) * time.Second,
		ClockSkew:    time.Duration(controlPlaneAttestationFirstPositiveEnvInt("SUB2API_CLAUDE_CODE_ROUTE_HINT_CLOCK_SKEW_SECONDS", 30)) * time.Second,
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

func decodeClaudeCodeRouteHintPayload(encoded string) (*ClaudeCodeRouteHintPayload, error) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("claude code route hint payload is malformed")
	}
	var fieldSet map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fieldSet); err != nil {
		return nil, fmt.Errorf("claude code route hint payload is malformed")
	}
	for field := range fieldSet {
		if _, ok := claudeCodeRouteHintPayloadAllowedFields[field]; !ok {
			return nil, fmt.Errorf("claude code route hint payload must match the strict allowlist schema")
		}
	}
	if len(fieldSet) != len(claudeCodeRouteHintPayloadAllowedFields) {
		return nil, fmt.Errorf("claude code route hint payload must match the strict allowlist schema")
	}
	var payload ClaudeCodeRouteHintPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("claude code route hint payload is malformed")
	}
	return &payload, nil
}

func parseClaudeCodeNativeHashAllowlistEnv(name string) (map[string]struct{}, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return nil, nil
	}
	result := map[string]struct{}{}
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' || r == '\t' || r == ' ' }) {
		value := strings.ToLower(strings.TrimSpace(part))
		if claudeCodeNativeSafeHashRe.MatchString(value) && value != claudeCodeNativeUnknownHash {
			result[value] = struct{}{}
		}
	}
	if len(result) == 0 {
		label := strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(name, "SUB2API_CLAUDE_CODE_NATIVE_"), "_HASHES"))
		return nil, fmt.Errorf("claude code native %s hash allowlist is invalid", label)
	}
	return result, nil
}

func claudeCodeNativeHashAllowed(value string, allowlist map[string]struct{}) bool {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if !claudeCodeNativeSafeHashRe.MatchString(trimmed) || trimmed == claudeCodeNativeUnknownHash {
		return false
	}
	if len(allowlist) == 0 {
		return true
	}
	_, ok := allowlist[trimmed]
	return ok
}

func (claudeCodeNativeEnvCatalogAdmissionResolver) ResolveClaudeCodeNativeCatalogAdmission(model string) (claudeCodeNativeCatalogAdmissionDecision, error) {
	model = strings.TrimSpace(model)
	if model == "" || looksSensitiveText(model) {
		return claudeCodeNativeCatalogAdmissionDecision{}, nil
	}
	if strings.TrimSpace(os.Getenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON")) != "" {
		decision, err := LoadClaudeCodeProviderRegistryFromEnv().Resolve(context.Background(), model)
		if err != nil {
			return claudeCodeNativeCatalogAdmissionDecision{}, nil
		}
		return decision.NativeCatalogAdmissionDecision(), nil
	}
	decisions, err := loadClaudeCodeNativeCatalogAdmissionDecisionsFromEnv()
	if err != nil {
		return claudeCodeNativeCatalogAdmissionDecision{}, err
	}
	decision, ok := decisions[model]
	if !ok {
		return claudeCodeNativeCatalogAdmissionDecision{}, nil
	}
	return decision, nil
}

func loadClaudeCodeNativeCatalogAdmissionDecisionsFromEnv() (map[string]claudeCodeNativeCatalogAdmissionDecision, error) {
	decisions := map[string]claudeCodeNativeCatalogAdmissionDecision{}
	if raw := strings.TrimSpace(os.Getenv("SUB2API_CLAUDE_CODE_NATIVE_ROUTE_CATALOG_JSON")); raw != "" {
		parsed, err := parseClaudeCodeNativeCatalogAdmissionJSON(raw)
		if err != nil {
			return nil, err
		}
		for model, decision := range parsed {
			decisions[model] = decision
		}
	}
	for _, model := range splitClaudeCodeNativeCatalogModels(os.Getenv("SUB2API_CLAUDE_CODE_NATIVE_FORMAL_POOL_MODELS")) {
		if _, exists := decisions[model]; exists {
			continue
		}
		decisions[model] = claudeCodeNativeCatalogAdmissionDecision{
			ModelID:         model,
			Route:           ClaudeCodeNativeRoute,
			ProviderOwner:   ClaudeCodeNativeProviderOwner,
			CredentialScope: ClaudeCodeNativeCredentialScope,
			GatewayLocation: ClaudeCodeNativeGatewayLocation,
			CatalogFresh:    true,
		}
	}
	if len(decisions) == 0 {
		return nil, fmt.Errorf("claude code native catalog admission is not configured")
	}
	return decisions, nil
}

func parseClaudeCodeNativeCatalogAdmissionJSON(raw string) (map[string]claudeCodeNativeCatalogAdmissionDecision, error) {
	var entries []claudeCodeNativeCatalogAdmissionDecision
	if err := json.Unmarshal([]byte(raw), &entries); err == nil {
		return normalizeClaudeCodeNativeCatalogAdmissionEntries(entries)
	}
	var byModel map[string]claudeCodeNativeCatalogAdmissionDecision
	if err := json.Unmarshal([]byte(raw), &byModel); err != nil {
		return nil, fmt.Errorf("claude code native catalog admission JSON is invalid")
	}
	entries = make([]claudeCodeNativeCatalogAdmissionDecision, 0, len(byModel))
	for model, decision := range byModel {
		if strings.TrimSpace(decision.ModelID) == "" {
			decision.ModelID = model
		}
		entries = append(entries, decision)
	}
	return normalizeClaudeCodeNativeCatalogAdmissionEntries(entries)
}

func normalizeClaudeCodeNativeCatalogAdmissionEntries(entries []claudeCodeNativeCatalogAdmissionDecision) (map[string]claudeCodeNativeCatalogAdmissionDecision, error) {
	decisions := make(map[string]claudeCodeNativeCatalogAdmissionDecision, len(entries))
	for _, decision := range entries {
		decision.ModelID = strings.TrimSpace(decision.ModelID)
		decision.Route = strings.TrimSpace(decision.Route)
		decision.ProviderOwner = strings.TrimSpace(decision.ProviderOwner)
		decision.CredentialScope = strings.TrimSpace(decision.CredentialScope)
		decision.GatewayLocation = strings.TrimSpace(decision.GatewayLocation)
		if decision.ModelID == "" || looksSensitiveText(decision.ModelID) {
			return nil, fmt.Errorf("claude code native catalog admission model is invalid")
		}
		if !strings.HasPrefix(decision.ModelID, "claude-") {
			continue
		}
		decisions[decision.ModelID] = decision
	}
	return decisions, nil
}

func splitClaudeCodeNativeCatalogModels(raw string) []string {
	models := []string{}
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' || r == '\t' || r == ' ' }) {
		model := strings.TrimSpace(part)
		if model != "" && strings.HasPrefix(model, "claude-") && !looksSensitiveText(model) {
			models = append(models, model)
		}
	}
	return models
}

func (s *ClaudeCodeNativeAttestationService) resolveClaudeCodeNativeCatalogAdmission(model string) (claudeCodeNativeCatalogAdmissionDecision, error) {
	resolver := s.catalogAdmissionResolver
	if resolver == nil {
		resolver = claudeCodeNativeEnvCatalogAdmissionResolver{}
	}
	return resolver.ResolveClaudeCodeNativeCatalogAdmission(model)
}

func validateClaudeCodeNativeCatalogAdmission(model string, payload *ClaudeCodeNativeAttestationPayload, headers http.Header, decision claudeCodeNativeCatalogAdmissionDecision) error {
	model = strings.TrimSpace(model)
	if payload == nil || model == "" || looksSensitiveText(model) || strings.TrimSpace(decision.ModelID) != model || !decision.CatalogFresh {
		return fmt.Errorf("claude code native catalog admission is invalid")
	}
	if decision.Route != ClaudeCodeNativeRoute || decision.ProviderOwner != ClaudeCodeNativeProviderOwner || decision.CredentialScope != ClaudeCodeNativeCredentialScope || decision.GatewayLocation != ClaudeCodeNativeGatewayLocation {
		return fmt.Errorf("claude code native catalog admission is invalid")
	}
	if !claudeCodeNativeCatalogBindingMatches(payload.RuntimeHash, decision.RuntimeHash) || !claudeCodeNativeCatalogBindingMatches(payload.OverlayHash, decision.OverlayHash) || !claudeCodeNativeCatalogBindingMatches(payload.CatalogHash, decision.CatalogHash) {
		return fmt.Errorf("claude code native catalog admission hash binding is invalid")
	}
	if strings.TrimSpace(decision.CatalogVersion) != "" {
		if payload.CatalogVersion != decision.CatalogVersion || strings.TrimSpace(headers.Get(ClaudeCodeNativeCatalogVersionHeader)) != decision.CatalogVersion {
			return fmt.Errorf("claude code native catalog admission version binding is invalid")
		}
	}
	return nil
}

func claudeCodeNativeCatalogBindingMatches(payloadValue, catalogValue string) bool {
	catalogValue = strings.ToLower(strings.TrimSpace(catalogValue))
	if catalogValue == "" {
		return true
	}
	return strings.ToLower(strings.TrimSpace(payloadValue)) == catalogValue
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
	if payload.Route != ClaudeCodeNativeRoute || payload.ProviderOwner != ClaudeCodeNativeProviderOwner || payload.CredentialScope != ClaudeCodeNativeCredentialScope || payload.GatewayLocation != ClaudeCodeNativeGatewayLocation {
		return fmt.Errorf("claude code native attestation route binding is invalid")
	}
	if strings.TrimSpace(payload.ModelID) == "" || looksSensitiveText(payload.ModelID) {
		return fmt.Errorf("claude code native attestation model binding is invalid")
	}
	for _, value := range []string{payload.RuntimeHash, payload.OverlayHash, payload.CatalogHash, payload.BodyShapeHash} {
		trimmed := strings.TrimSpace(value)
		if !claudeCodeNativeSafeHashRe.MatchString(trimmed) || trimmed == claudeCodeNativeUnknownHash {
			return fmt.Errorf("claude code native attestation hash binding is invalid")
		}
	}
	return nil
}

func validateClaudeCodeRouteHintPayloadShape(payload *ClaudeCodeRouteHintPayload) error {
	if payload == nil {
		return fmt.Errorf("claude code route hint payload is required")
	}
	if strings.TrimSpace(payload.KeyID) == "" || strings.TrimSpace(payload.Nonce) == "" || payload.IssuedAt <= 0 || payload.ExpiresAt <= payload.IssuedAt || payload.Version <= 0 {
		return fmt.Errorf("claude code route hint payload shape is invalid")
	}
	for _, value := range []string{payload.BodySHA256, payload.RuntimeHash, payload.OverlayHash, payload.CatalogHash} {
		trimmed := strings.TrimSpace(value)
		if !claudeCodeNativeSafeHashRe.MatchString(trimmed) || trimmed == claudeCodeNativeUnknownHash {
			return fmt.Errorf("claude code route hint hash binding is invalid")
		}
	}
	for _, value := range []string{
		payload.Method,
		payload.RequestURI,
		payload.ModelID,
		payload.BodyModel,
		payload.CatalogVersion,
		payload.SessionRef,
		payload.Route,
		payload.ClientType,
		payload.Provider,
		payload.ProviderOwner,
		payload.CredentialScope,
		payload.GatewayLocation,
	} {
		if strings.TrimSpace(value) == "" || looksSensitiveText(value) {
			return fmt.Errorf("claude code route hint payload shape is invalid")
		}
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

func validateClaudeCodeRouteHintBinding(method, rawRoute string, body []byte, payload *ClaudeCodeRouteHintPayload, decision ClaudeCodeProviderRouteDecision) error {
	if strings.ToUpper(strings.TrimSpace(method)) != http.MethodPost || strings.ToUpper(strings.TrimSpace(payload.Method)) != http.MethodPost {
		return fmt.Errorf("claude code route hint method mismatch")
	}
	if payload.RequestURI != rawRoute {
		return fmt.Errorf("claude code route hint route mismatch")
	}
	if path, query := splitCompatRoute(rawRoute); path != ClaudeCodeNativeInboundMessages || query != "" {
		return fmt.Errorf("claude code route hint route unsupported")
	}
	model := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	if model == "" || looksSensitiveText(model) || payload.ModelID != model || payload.BodyModel != model || decision.ModelID != model {
		return fmt.Errorf("claude code route hint model binding mismatch")
	}
	digest := sha256.Sum256(body)
	if payload.BodySHA256 != "sha256:"+hex.EncodeToString(digest[:]) {
		return fmt.Errorf("claude code route hint body binding mismatch")
	}
	if payload.Provider != decision.Provider || payload.Route != decision.Route || payload.ClientType != decision.ClientType {
		return fmt.Errorf("claude code route hint catalog route binding mismatch")
	}
	if payload.ProviderOwner != decision.ProviderOwner || payload.CredentialScope != decision.CredentialScope || payload.GatewayLocation != decision.GatewayLocation {
		return fmt.Errorf("claude code route hint catalog account binding mismatch")
	}
	if payload.RuntimeHash != decision.RuntimeHash || payload.OverlayHash != decision.OverlayHash || payload.CatalogHash != decision.CatalogHash || payload.CatalogVersion != decision.CatalogVersion {
		return fmt.Errorf("claude code route hint catalog hash binding mismatch")
	}
	if payload.FormalPoolAllowed || payload.NativeAttestationAllowed || payload.ClientType == ClaudeCodeNativeClientType || payload.Route == ClaudeCodeNativeRoute {
		return fmt.Errorf("claude code route hint bridge cannot claim native")
	}
	if payload.LiveRequestAllowed {
		return fmt.Errorf("claude code route hint bridge live request is not enabled")
	}
	return nil
}

func validateClaudeCodeNativeSafeRefs(payload *ClaudeCodeNativeAttestationPayload, headers http.Header) error {
	if !isSafeLedgerRef(payload.LocalSessionRef) {
		return fmt.Errorf("claude code native local session ref must be opaque")
	}
	if payload.SessionRef != payload.LocalSessionRef || !isSafeLedgerRef(payload.SessionRef) {
		return fmt.Errorf("claude code native session binding mismatch")
	}
	if strings.TrimSpace(headers.Get(ClaudeCodeNativeLocalSessionRefHeader)) != "" && headers.Get(ClaudeCodeNativeLocalSessionRefHeader) != payload.LocalSessionRef {
		return fmt.Errorf("claude code native session ref header mismatch")
	}
	if !headerTruthy(headers.Get(ClaudeCodeNativeNetwatchRequiredHeader)) || !payload.NetwatchRequired {
		return fmt.Errorf("claude code native netwatch is required")
	}
	return nil
}

func validateClaudeCodeNativeTrustedBindings(payload *ClaudeCodeNativeAttestationPayload, cfg *ClaudeCodeNativeAttestationConfig) error {
	if payload == nil {
		return fmt.Errorf("claude code native attestation payload is required")
	}
	if !claudeCodeNativeHashAllowed(payload.RuntimeHash, cfg.RuntimeHashes) {
		return fmt.Errorf("claude code native runtime hash binding is invalid")
	}
	if !claudeCodeNativeHashAllowed(payload.OverlayHash, cfg.OverlayHashes) {
		return fmt.Errorf("claude code native overlay hash binding is invalid")
	}
	if !claudeCodeNativeHashAllowed(payload.CatalogHash, cfg.CatalogHashes) {
		return fmt.Errorf("claude code native catalog hash binding is invalid")
	}
	return nil
}

func validateClaudeCodeNativeBodyBinding(payload *ClaudeCodeNativeAttestationPayload, body []byte) error {
	if payload == nil {
		return fmt.Errorf("claude code native attestation payload is required")
	}
	model := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	if model == "" || looksSensitiveText(model) || payload.ModelID != model {
		return fmt.Errorf("claude code native attestation model binding mismatch")
	}
	if payload.BodyShapeHash != claudeCodeNativeBodyShapeHash(body) {
		return fmt.Errorf("claude code native attestation body shape binding mismatch")
	}
	return nil
}

func claudeCodeNativeBodyShapeHash(body []byte) string {
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		decoded = map[string]any{"body_size": len(body), "type": "invalid_json"}
	}
	shape := claudeCodeNativeShapeValue(decoded)
	raw, _ := json.Marshal(shape)
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func claudeCodeNativeShapeValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		children := map[string]any{}
		keys := make([]string, 0, len(v))
		for key, child := range v {
			safeKey := sanitizeClaudeCodeNativeShapeKey(key)
			if safeKey == "" {
				safeKey = "redacted-key"
			}
			if _, exists := children[safeKey]; !exists {
				keys = append(keys, safeKey)
			}
			children[safeKey] = claudeCodeNativeShapeValue(child)
		}
		sort.Strings(keys)
		return map[string]any{"children": children, "keys": keys, "type": "object"}
	case []any:
		items := make([]any, 0, len(v))
		limit := len(v)
		if limit > 32 {
			limit = 32
		}
		for i := 0; i < limit; i++ {
			items = append(items, claudeCodeNativeShapeValue(v[i]))
		}
		return map[string]any{"items": items, "len": len(v), "truncated": len(v) > 32, "type": "array"}
	case string:
		return map[string]any{"type": "string"}
	case bool:
		return map[string]any{"type": "bool"}
	case float64, float32, int, int64, int32, json.Number:
		return map[string]any{"type": "number"}
	case nil:
		return map[string]any{"type": "null"}
	default:
		return map[string]any{"type": "unknown"}
	}
}

func sanitizeClaudeCodeNativeShapeKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" || looksSensitiveText(key) || len(key) > 128 {
		return "redacted-key"
	}
	for _, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return "redacted-key"
	}
	return key
}

func buildClaudeCodeNativeAuditSummary(payload *ClaudeCodeNativeAttestationPayload, body []byte) ClaudeCodeNativeAuditSummary {
	return buildClaudeCodeNativeAuditSummaryWithHeaders(payload, nil, body)
}

func buildClaudeCodeNativeAuditSummaryWithHeaders(payload *ClaudeCodeNativeAttestationPayload, headers http.Header, body []byte) ClaudeCodeNativeAuditSummary {
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
		RuntimeHash:                payload.RuntimeHash,
		OverlayHash:                payload.OverlayHash,
		CatalogHash:                payload.CatalogHash,
		CatalogVersion:             safeClaudeCodeNativeLabel(headers.Get(ClaudeCodeNativeCatalogVersionHeader)),
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

func signClaudeCodeRouteHint(encoded, method, rawRoute string, body []byte, secret string) string {
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

func sharedClaudeCodeRouteHintNonceReplayCache(ttl time.Duration, nowFn func() time.Time) *ClaudeCodeNativeNonceReplayCache {
	claudeCodeRouteHintReplayCacheMu.Lock()
	defer claudeCodeRouteHintReplayCacheMu.Unlock()
	if claudeCodeRouteHintReplayCache == nil || claudeCodeRouteHintReplayCacheTTL != ttl {
		claudeCodeRouteHintReplayCache = NewClaudeCodeNativeNonceReplayCache(ttl, nowFn)
		claudeCodeRouteHintReplayCacheTTL = ttl
	}
	return claudeCodeRouteHintReplayCache
}
