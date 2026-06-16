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
	SessionRef              string `json:"session_ref"`
	BodyShapeHash           string `json:"body_shape_hash"`
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

type ClaudeCodeNativeNonceReplayCache struct {
	ttl   time.Duration
	nowFn func() time.Time
	mu    sync.Mutex
	seen  map[string]time.Time
}

type ClaudeCodeNativeAttestationService struct {
	nowFn                    func() time.Time
	replayCache              *ClaudeCodeNativeNonceReplayCache
	catalogAdmissionResolver claudeCodeNativeCatalogAdmissionResolver
}

type ClaudeCodeNativeAttestationOption func(*ClaudeCodeNativeAttestationService)

type claudeCodeNativeCatalogAdmissionDecision struct {
	ModelID         string `json:"model_id"`
	Route           string `json:"route"`
	ProviderOwner   string `json:"provider_owner"`
	CredentialScope string `json:"credential_scope"`
	GatewayLocation string `json:"gateway_location"`
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
	"session_ref":               {},
	"body_shape_hash":           {},
}

var (
	claudeCodeNativeSafeHashRe     = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	claudeCodeNativeUnknownHash    = "sha256:" + strings.Repeat("0", 64)
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
	if err := validateClaudeCodeNativeCatalogAdmission(payload.ModelID, catalogAdmission); err != nil {
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
		decisions[decision.ModelID] = decision
	}
	return decisions, nil
}

func splitClaudeCodeNativeCatalogModels(raw string) []string {
	models := []string{}
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' || r == '\t' || r == ' ' }) {
		model := strings.TrimSpace(part)
		if model != "" && !looksSensitiveText(model) {
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

func validateClaudeCodeNativeCatalogAdmission(model string, decision claudeCodeNativeCatalogAdmissionDecision) error {
	model = strings.TrimSpace(model)
	if model == "" || looksSensitiveText(model) || strings.TrimSpace(decision.ModelID) != model || !decision.CatalogFresh {
		return fmt.Errorf("claude code native catalog admission is invalid")
	}
	if decision.Route != ClaudeCodeNativeRoute || decision.ProviderOwner != ClaudeCodeNativeProviderOwner || decision.CredentialScope != ClaudeCodeNativeCredentialScope || decision.GatewayLocation != ClaudeCodeNativeGatewayLocation {
		return fmt.Errorf("claude code native catalog admission is invalid")
	}
	return nil
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
