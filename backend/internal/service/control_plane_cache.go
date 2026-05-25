package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const controlPlaneCacheKeyScope = "control_plane_cache_key"

type ControlPlaneCacheKeyInput struct {
	PathTemplate     string            `json:"path_template"`
	NormalizedQuery  map[string]string `json:"normalized_query"`
	AccountScope     string            `json:"account_scope"`
	UserPartition    string            `json:"user_partition"`
	SessionPartition string            `json:"session_partition"`
	PersonaProfile   string            `json:"persona_profile"`
	ModelVersion     string            `json:"model_version"`
	SchemaVersion    int               `json:"schema_version"`
}

type ControlPlaneCacheKey struct {
	KeyID      string                    `json:"key_id"`
	Scope      string                    `json:"scope"`
	Version    int                       `json:"version"`
	Value      string                    `json:"value"`
	Components ControlPlaneCacheKeyInput `json:"components"`
}

type ControlPlaneCacheEntry struct {
	Key       ControlPlaneCacheKey `json:"key"`
	Response  map[string]any       `json:"response"`
	StoredAt  time.Time            `json:"stored_at"`
	ExpiresAt time.Time            `json:"expires_at"`
	StaleMode string               `json:"stale_mode"`
}

type ControlPlaneCache struct {
	mu      sync.RWMutex
	nowFn   func() time.Time
	entries map[string]ControlPlaneCacheEntry
}

func NewControlPlaneCache(nowFn func() time.Time) *ControlPlaneCache {
	if nowFn == nil {
		nowFn = time.Now
	}
	return &ControlPlaneCache{nowFn: nowFn, entries: map[string]ControlPlaneCacheEntry{}}
}

func NewControlPlaneCacheKey(input ControlPlaneCacheKeyInput) (ControlPlaneCacheKey, error) {
	if err := validateControlPlaneCacheKeyInput(input); err != nil {
		return ControlPlaneCacheKey{}, err
	}
	canonical := canonicalControlPlaneCacheKeyInput(input)
	payload, err := json.Marshal(canonical)
	if err != nil {
		return ControlPlaneCacheKey{}, err
	}
	value := "hmac-sha256:" + hex.EncodeToString(controlPlaneCacheHMAC(controlPlaneCacheKeyScope, payload))
	return ControlPlaneCacheKey{
		KeyID:      controlPlaneCacheKeyID(),
		Scope:      controlPlaneCacheKeyScope,
		Version:    controlPlaneCacheKeyVersion(),
		Value:      value,
		Components: canonical,
	}, nil
}

func (c *ControlPlaneCache) Put(key ControlPlaneCacheKey, response map[string]any, ttl time.Duration, staleMode string) error {
	if c == nil {
		return fmt.Errorf("control-plane cache is nil")
	}
	if key.Scope != controlPlaneCacheKeyScope || !controlPlaneHMACRefRe.MatchString(key.Value) {
		return fmt.Errorf("control-plane cache key is invalid")
	}
	if ttl <= 0 {
		return fmt.Errorf("control-plane cache ttl must be positive")
	}
	now := c.nowFn()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key.Value] = ControlPlaneCacheEntry{Key: key, Response: copyAnyMap(response), StoredAt: now, ExpiresAt: now.Add(ttl), StaleMode: staleMode}
	return nil
}

func (c *ControlPlaneCache) Get(key ControlPlaneCacheKey, policy ControlPlanePathPolicy) (map[string]any, bool, bool) {
	if c == nil {
		return nil, false, false
	}
	c.mu.RLock()
	entry, ok := c.entries[key.Value]
	c.mu.RUnlock()
	if !ok {
		return nil, false, false
	}
	now := c.nowFn()
	if now.Before(entry.ExpiresAt) || now.Equal(entry.ExpiresAt) {
		return copyAnyMap(entry.Response), true, false
	}
	if policy.StaleMode == ControlPlaneStaleSafe && !policy.Sensitive {
		return copyAnyMap(entry.Response), true, true
	}
	return nil, false, false
}

func validateControlPlaneCacheKeyInput(input ControlPlaneCacheKeyInput) error {
	required := map[string]string{
		"path_template":     input.PathTemplate,
		"account_scope":     input.AccountScope,
		"user_partition":    input.UserPartition,
		"session_partition": input.SessionPartition,
		"persona_profile":   input.PersonaProfile,
		"model_version":     input.ModelVersion,
	}
	for name, value := range required {
		if strings.TrimSpace(value) == "" || looksSensitiveText(value) || looksPlainDigest(value) || looksUnsafeDynamicIdentifier(value) {
			return fmt.Errorf("control-plane cache key %s is invalid", name)
		}
	}
	if input.SchemaVersion <= 0 {
		return fmt.Errorf("control-plane cache key schema_version is invalid")
	}
	if err := validatePathTemplate(input.PathTemplate); err != nil {
		return err
	}
	for key, value := range input.NormalizedQuery {
		if strings.TrimSpace(key) == "" || strings.ContainsAny(key, "[]{}.") || looksSensitiveText(key) || looksSensitiveText(value) || looksPlainDigest(value) || looksUnsafeDynamicIdentifier(value) {
			return fmt.Errorf("control-plane cache key normalized_query is invalid")
		}
	}
	return nil
}

func canonicalControlPlaneCacheKeyInput(input ControlPlaneCacheKeyInput) ControlPlaneCacheKeyInput {
	query := map[string]string{}
	keys := make([]string, 0, len(input.NormalizedQuery))
	for key := range input.NormalizedQuery {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		query[key] = input.NormalizedQuery[key]
	}
	return ControlPlaneCacheKeyInput{
		PathTemplate:     strings.TrimSpace(input.PathTemplate),
		NormalizedQuery:  query,
		AccountScope:     strings.TrimSpace(input.AccountScope),
		UserPartition:    strings.TrimSpace(input.UserPartition),
		SessionPartition: strings.TrimSpace(input.SessionPartition),
		PersonaProfile:   strings.TrimSpace(input.PersonaProfile),
		ModelVersion:     strings.TrimSpace(input.ModelVersion),
		SchemaVersion:    input.SchemaVersion,
	}
}

func controlPlaneCacheHMAC(scope string, payload []byte) []byte {
	secret := strings.TrimSpace(os.Getenv("SUB2API_CONTROL_PLANE_CACHE_HMAC_KEY"))
	if secret == "" {
		secret = "sub2api-control-plane-cache-dev-key"
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(scope))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte("v"))
	_, _ = mac.Write([]byte(fmt.Sprint(controlPlaneCacheKeyVersion())))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func controlPlaneCacheKeyID() string {
	if keyID := strings.TrimSpace(os.Getenv("SUB2API_CONTROL_PLANE_CACHE_HMAC_KEY_ID")); keyID != "" {
		return keyID
	}
	return "control_plane_cache_v1"
}

func controlPlaneCacheKeyVersion() int {
	return 1
}
