package service

import (
	"context"
	"strings"
	"time"
)

const modelRateLimitsKey = "model_rate_limits"

// isRateLimitActiveForKey 检查指定 key 的限流是否生效
func (a *Account) isRateLimitActiveForKey(key string) bool {
	resetAt := a.modelRateLimitResetAt(key)
	return resetAt != nil && time.Now().Before(*resetAt)
}

// getRateLimitRemainingForKey 获取指定 key 的限流剩余时间，0 表示未限流或已过期
func (a *Account) getRateLimitRemainingForKey(key string) time.Duration {
	resetAt := a.modelRateLimitResetAt(key)
	if resetAt == nil {
		return 0
	}
	remaining := time.Until(*resetAt)
	if remaining > 0 {
		return remaining
	}
	return 0
}

func (a *Account) isModelRateLimitedWithContext(ctx context.Context, requestedModel string) bool {
	if a == nil {
		return false
	}

	for _, modelKey := range a.modelRateLimitLookupKeys(ctx, requestedModel) {
		if a.isRateLimitActiveForKey(modelKey) {
			return true
		}
	}
	return false
}

// GetModelRateLimitRemainingTime 获取模型限流剩余时间
// 返回 0 表示未限流或已过期
func (a *Account) GetModelRateLimitRemainingTime(requestedModel string) time.Duration {
	return a.GetModelRateLimitRemainingTimeWithContext(context.Background(), requestedModel)
}

func (a *Account) GetModelRateLimitRemainingTimeWithContext(ctx context.Context, requestedModel string) time.Duration {
	if a == nil {
		return 0
	}

	var maxRemaining time.Duration
	for _, modelKey := range a.modelRateLimitLookupKeys(ctx, requestedModel) {
		if remaining := a.getRateLimitRemainingForKey(modelKey); remaining > maxRemaining {
			maxRemaining = remaining
		}
	}
	return maxRemaining
}

func resolveFinalAntigravityModelKey(ctx context.Context, account *Account, requestedModel string) string {
	modelKey := mapAntigravityModel(account, requestedModel)
	if modelKey == "" {
		return ""
	}
	// thinking 会影响 Antigravity 最终模型名（例如 claude-sonnet-4-5 -> claude-sonnet-4-5-thinking）
	if enabled, ok := ThinkingEnabledFromContext(ctx); ok {
		modelKey = applyThinkingModelSuffix(modelKey, enabled)
	}
	return modelKey
}

func (a *Account) modelRateLimitLookupKeys(ctx context.Context, requestedModel string) []string {
	if a == nil {
		return nil
	}

	if a.Platform != PlatformAntigravity {
		return appendUniqueModelRateLimitKey(nil, a.GetMappedModel(requestedModel))
	}

	var keys []string
	mappedKey := resolveFinalAntigravityModelKey(ctx, a, requestedModel)
	keys = appendUniqueModelRateLimitKey(keys, mappedKey)

	// Antigravity reset metadata has historically been stored under the model
	// reported by the upstream error, which can be the requested alias rather
	// than the current default-mapped upstream model. Keep lookup backward
	// compatible without falling back to old coarse scopes such as claude_sonnet.
	requestedKey := normalizeAntigravityModelName(requestedModel)
	if enabled, ok := ThinkingEnabledFromContext(ctx); ok {
		requestedKey = applyThinkingModelSuffix(requestedKey, enabled)
	}
	keys = appendUniqueModelRateLimitKey(keys, requestedKey)

	return keys
}

func appendUniqueModelRateLimitKey(keys []string, key string) []string {
	key = strings.TrimSpace(key)
	if key == "" {
		return keys
	}
	for _, existing := range keys {
		if existing == key {
			return keys
		}
	}
	return append(keys, key)
}

func (a *Account) modelRateLimitResetAt(scope string) *time.Time {
	if a == nil || a.Extra == nil || scope == "" {
		return nil
	}
	rawLimits, ok := a.Extra[modelRateLimitsKey].(map[string]any)
	if !ok {
		return nil
	}
	rawLimit, ok := rawLimits[scope].(map[string]any)
	if !ok {
		return nil
	}
	resetAtRaw, ok := rawLimit["rate_limit_reset_at"].(string)
	if !ok || strings.TrimSpace(resetAtRaw) == "" {
		return nil
	}
	resetAt, err := time.Parse(time.RFC3339, resetAtRaw)
	if err != nil {
		return nil
	}
	return &resetAt
}
