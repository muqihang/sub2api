package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	controlPlaneAttestationHeader          = "x-sub2api-control-plane-attestation"
	controlPlaneAttestationSignatureHeader = "x-sub2api-control-plane-signature"
)

type ControlPlaneAttestationPayload struct {
	KeyID                 string                     `json:"key_id"`
	Scope                 string                     `json:"scope"`
	Version               int                        `json:"version"`
	IssuedAt              int64                      `json:"issued_at"`
	Nonce                 string                     `json:"nonce"`
	Method                string                     `json:"method"`
	PathTemplate          string                     `json:"path_template"`
	NormalizedQuery       map[string]string          `json:"normalized_query"`
	Classification        string                     `json:"classification"`
	RoutingIntent         string                     `json:"routing_intent"`
	PolicyVersion         int                        `json:"policy_version"`
	StrategyVersion       int                        `json:"strategy_version"`
	ResponseSchemaVersion int                        `json:"response_schema_version"`
	BodyLengthBucket      string                     `json:"body_length_bucket"`
	BodyOmittedReason     string                     `json:"body_omitted_reason"`
	DigestOmittedReason   string                     `json:"digest_omitted_reason"`
	SchemaSummary         map[string]any             `json:"schema_summary"`
	QueryRef              *ControlPlaneScopedHMACRef `json:"query_ref"`
	QueryOmittedReason    *string                    `json:"query_omitted_reason"`
	SessionRef            *ControlPlaneScopedHMACRef `json:"session_ref"`
}

type ControlPlaneAttestationConfig struct {
	CurrentKeyID string
	Keys         map[string]string
	Scope        string
	Version      int
	NonceTTL     time.Duration
	ClockSkew    time.Duration
}

type ControlPlaneNonceReplayCache struct {
	ttl   time.Duration
	nowFn func() time.Time
	mu    sync.Mutex
	seen  map[string]time.Time
}

type ControlPlaneAttestationService struct {
	nowFn       func() time.Time
	replayCache *ControlPlaneNonceReplayCache
}

type ControlPlaneAttestationOption func(*ControlPlaneAttestationService)

var (
	controlPlaneReplayCacheMu  sync.Mutex
	controlPlaneReplayCacheTTL time.Duration
	controlPlaneReplayCache    *ControlPlaneNonceReplayCache
)

func NewControlPlaneAttestationService(opts ...ControlPlaneAttestationOption) *ControlPlaneAttestationService {
	svc := &ControlPlaneAttestationService{
		nowFn: time.Now,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(svc)
		}
	}
	return svc
}

func WithControlPlaneAttestationNowFunc(nowFn func() time.Time) ControlPlaneAttestationOption {
	return func(svc *ControlPlaneAttestationService) {
		if nowFn != nil {
			svc.nowFn = nowFn
		}
	}
}

func WithControlPlaneAttestationReplayCache(cache *ControlPlaneNonceReplayCache) ControlPlaneAttestationOption {
	return func(svc *ControlPlaneAttestationService) {
		if cache != nil {
			svc.replayCache = cache
		}
	}
}

func NewControlPlaneNonceReplayCache(ttl time.Duration, nowFn func() time.Time) *ControlPlaneNonceReplayCache {
	if ttl <= 0 {
		ttl = 120 * time.Second
	}
	if nowFn == nil {
		nowFn = time.Now
	}
	return &ControlPlaneNonceReplayCache{ttl: ttl, nowFn: nowFn, seen: map[string]time.Time{}}
}

func (c *ControlPlaneNonceReplayCache) CheckAndRecord(keyID, scope, nonce string, now time.Time) error {
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
		return fmt.Errorf("control-plane attestation nonce replayed")
	}
	c.seen[replayKey] = current.Add(c.ttl)
	return nil
}

func (s *ControlPlaneAttestationService) VerifyRequest(body []byte, headers http.Header) (*ControlPlaneAttestationPayload, error) {
	intent, err := parseAndValidateControlPlaneIntent(body)
	if err != nil {
		return nil, err
	}
	return s.VerifyIntent(intent, headers)
}

func (s *ControlPlaneAttestationService) VerifyIntent(intent *ControlPlaneIntent, headers http.Header) (*ControlPlaneAttestationPayload, error) {
	if intent == nil {
		return nil, fmt.Errorf("control-plane attestation requires a parsed intent")
	}
	attestation := strings.TrimSpace(headers.Get(controlPlaneAttestationHeader))
	signature := strings.TrimSpace(headers.Get(controlPlaneAttestationSignatureHeader))
	if attestation == "" || signature == "" {
		return nil, fmt.Errorf("control-plane attestation is required")
	}
	cfg, err := loadControlPlaneAttestationConfigFromEnv()
	if err != nil {
		return nil, err
	}
	if s.replayCache == nil {
		s.replayCache = sharedControlPlaneNonceReplayCache(cfg.NonceTTL, s.nowFn)
	}
	payload, err := decodeControlPlaneAttestationPayload(attestation)
	if err != nil {
		return nil, err
	}
	if err := validateControlPlaneAttestationPayloadShape(payload); err != nil {
		return nil, err
	}
	if payload.Scope != cfg.Scope {
		return nil, fmt.Errorf("control-plane attestation scope mismatch")
	}
	if payload.Version != cfg.Version {
		return nil, fmt.Errorf("control-plane attestation version mismatch")
	}
	secret, ok := cfg.Keys[payload.KeyID]
	if !ok {
		return nil, fmt.Errorf("control-plane attestation key id is not configured")
	}
	if !hmac.Equal([]byte(signControlPlaneAttestation(attestation, secret)), []byte(signature)) {
		return nil, fmt.Errorf("control-plane attestation signature mismatch")
	}
	current := s.nowFn()
	if current.IsZero() {
		current = time.Now()
	}
	issuedAt := time.Unix(payload.IssuedAt, 0)
	if math.Abs(float64(current.Unix()-payload.IssuedAt)) > cfg.ClockSkew.Seconds() {
		return nil, fmt.Errorf("control-plane attestation timestamp is outside the clock skew window")
	}
	if issuedAt.Before(current.Add(-cfg.NonceTTL)) {
		return nil, fmt.Errorf("control-plane attestation timestamp expired")
	}
	if err := s.replayCache.CheckAndRecord(payload.KeyID, payload.Scope, payload.Nonce, current); err != nil {
		return nil, err
	}
	if err := compareControlPlaneAttestationIntent(intent, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func loadControlPlaneAttestationConfigFromEnv() (*ControlPlaneAttestationConfig, error) {
	currentKeyID := strings.TrimSpace(os.Getenv("SUB2API_CONTROL_PLANE_ATTESTATION_CURRENT_KEY_ID"))
	if currentKeyID == "" {
		currentKeyID = "guard_v1"
	}
	keysRaw := strings.TrimSpace(os.Getenv("SUB2API_CONTROL_PLANE_ATTESTATION_KEYS_JSON"))
	keys := map[string]string{}
	if keysRaw != "" {
		if err := json.Unmarshal([]byte(keysRaw), &keys); err != nil {
			return nil, fmt.Errorf("control-plane attestation key set is invalid")
		}
	} else {
		secret := strings.TrimSpace(os.Getenv("SUB2API_CONTROL_PLANE_ATTESTATION_SECRET"))
		if secret == "" {
			secret = "sub2api-control-plane-attestation-dev-key"
		}
		keys[currentKeyID] = secret
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("control-plane attestation key set is empty")
	}
	cfg := &ControlPlaneAttestationConfig{
		CurrentKeyID: currentKeyID,
		Keys:         keys,
		Scope:        controlPlaneAttestationFirstNonEmpty(strings.TrimSpace(os.Getenv("SUB2API_CONTROL_PLANE_ATTESTATION_SCOPE")), "control_plane_intent"),
		Version:      controlPlaneAttestationFirstPositiveEnvInt("SUB2API_CONTROL_PLANE_ATTESTATION_VERSION", 1),
		NonceTTL:     time.Duration(controlPlaneAttestationFirstPositiveEnvInt("SUB2API_CONTROL_PLANE_ATTESTATION_NONCE_TTL_SECONDS", 120)) * time.Second,
		ClockSkew:    time.Duration(controlPlaneAttestationFirstPositiveEnvInt("SUB2API_CONTROL_PLANE_ATTESTATION_CLOCK_SKEW_SECONDS", 30)) * time.Second,
	}
	return cfg, nil
}

func decodeControlPlaneAttestationPayload(encoded string) (*ControlPlaneAttestationPayload, error) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("control-plane attestation payload is malformed")
	}
	var payloadShape map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payloadShape); err != nil {
		return nil, fmt.Errorf("control-plane attestation payload is malformed")
	}
	expected := map[string]struct{}{
		"key_id": {}, "scope": {}, "version": {}, "issued_at": {}, "nonce": {}, "method": {},
		"path_template": {}, "normalized_query": {}, "classification": {}, "routing_intent": {},
		"policy_version": {}, "strategy_version": {}, "response_schema_version": {}, "body_length_bucket": {},
		"body_omitted_reason": {}, "digest_omitted_reason": {}, "schema_summary": {}, "query_ref": {},
		"query_omitted_reason": {}, "session_ref": {},
	}
	if len(payloadShape) != len(expected) {
		return nil, fmt.Errorf("control-plane attestation payload shape mismatch")
	}
	for key := range payloadShape {
		if _, ok := expected[key]; !ok {
			return nil, fmt.Errorf("control-plane attestation payload shape mismatch")
		}
	}
	var payload ControlPlaneAttestationPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("control-plane attestation payload is malformed")
	}
	return &payload, nil
}

func validateControlPlaneAttestationPayloadShape(payload *ControlPlaneAttestationPayload) error {
	if payload == nil {
		return fmt.Errorf("control-plane attestation payload is required")
	}
	if payload.KeyID == "" || payload.Scope == "" || payload.Version <= 0 || payload.IssuedAt <= 0 || payload.Nonce == "" {
		return fmt.Errorf("control-plane attestation payload shape mismatch")
	}
	if payload.SessionRef == nil || payload.SessionRef.Scope != "session_budget_session" || payload.SessionRef.Version <= 0 || !controlPlaneHMACRefRe.MatchString(payload.SessionRef.Value) {
		return fmt.Errorf("control-plane attestation session_ref is invalid")
	}
	return nil
}

func compareControlPlaneAttestationIntent(intent *ControlPlaneIntent, payload *ControlPlaneAttestationPayload) error {
	if payload.Method != intent.Method || payload.PathTemplate != intent.PathTemplate || payload.Classification != intent.Classification || payload.RoutingIntent != intent.RoutingIntent {
		return fmt.Errorf("control-plane attestation payload mismatch")
	}
	if !controlPlaneEqualStringMap(payload.NormalizedQuery, intent.NormalizedQuery) {
		return fmt.Errorf("control-plane attestation payload mismatch")
	}
	if payload.PolicyVersion != intent.PolicyVersion || payload.StrategyVersion != intent.StrategyVersion || payload.ResponseSchemaVersion != intent.ResponseSchemaVersion {
		return fmt.Errorf("control-plane attestation payload mismatch")
	}
	if payload.BodyLengthBucket != intent.BodyLengthBucket || payload.BodyOmittedReason != intent.BodyOmittedReason || payload.DigestOmittedReason != intent.DigestOmittedReason {
		return fmt.Errorf("control-plane attestation payload mismatch")
	}
	if !controlPlaneEqualJSONValue(payload.SchemaSummary, intent.SchemaSummary) {
		return fmt.Errorf("control-plane attestation payload mismatch")
	}
	if !equalControlPlaneScopedHMACRef(payload.QueryRef, intent.QueryRef) {
		return fmt.Errorf("control-plane attestation payload mismatch")
	}
	if !equalOptionalString(payload.QueryOmittedReason, intent.QueryOmittedReason) {
		return fmt.Errorf("control-plane attestation payload mismatch")
	}
	return nil
}

func signControlPlaneAttestation(encoded, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(encoded))
	return fmt.Sprintf("hmac-sha256:%x", mac.Sum(nil))
}

func equalControlPlaneScopedHMACRef(left, right *ControlPlaneScopedHMACRef) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.KeyID == right.KeyID && left.Scope == right.Scope && left.Version == right.Version && left.Value == right.Value
}

func equalOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func controlPlaneEqualStringMap(left, right map[string]string) bool {
	if len(left) == 0 && len(right) == 0 {
		return true
	}
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func controlPlaneEqualJSONValue(left, right any) bool {
	leftBytes, leftErr := json.Marshal(left)
	rightBytes, rightErr := json.Marshal(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return string(leftBytes) == string(rightBytes)
}

func sharedControlPlaneNonceReplayCache(ttl time.Duration, nowFn func() time.Time) *ControlPlaneNonceReplayCache {
	controlPlaneReplayCacheMu.Lock()
	defer controlPlaneReplayCacheMu.Unlock()
	if controlPlaneReplayCache == nil || controlPlaneReplayCacheTTL != ttl {
		controlPlaneReplayCache = NewControlPlaneNonceReplayCache(ttl, nowFn)
		controlPlaneReplayCacheTTL = ttl
	}
	return controlPlaneReplayCache
}

func controlPlaneAttestationFirstNonEmpty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func controlPlaneAttestationFirstPositiveEnvInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
