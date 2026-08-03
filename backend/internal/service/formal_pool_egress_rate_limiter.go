package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

const (
	formalPoolEgressRateLimitFallbackSecret = "sub2api-formal-pool-egress-rate-limit-dev-secret"
	formalPoolEgressRateLimitBucketHexLen   = 16
	formalPoolEgressRateLimitMinuteWindow   = time.Minute
)

type FormalPoolEgressRateLimitDecision struct {
	Allowed     bool
	NonceBucket string
	IPBucket    string
	Reason      string
}

type FormalPoolEgressRateLimiter interface {
	CheckEgressCheck(ctx context.Context, nonce, ip string) FormalPoolEgressRateLimitDecision
}

type InMemoryFormalPoolEgressRateLimiter struct {
	mu sync.Mutex

	nonceRateLimit  int
	ipRateLimit     int
	nonceTotalLimit int
	fallbackIPLimit int
	nonceTTL        time.Duration
	now             func() time.Time
	secret          []byte

	nonceRate  map[string]formalPoolEgressRateLimitCounter
	ipRate     map[string]formalPoolEgressRateLimitCounter
	nonceTotal map[string]formalPoolEgressRateLimitCounter
	fallbackIP map[string]formalPoolEgressRateLimitCounter
}

type formalPoolEgressRateLimitCounter struct {
	count     int
	expiresAt time.Time
}

func NewFormalPoolEgressRateLimiter(cfg FormalPoolConfig, now func() time.Time) *InMemoryFormalPoolEgressRateLimiter {
	defaults := DefaultFormalPoolConfig()
	if cfg.NonceTTL <= 0 {
		cfg.NonceTTL = defaults.NonceTTL
	}
	if cfg.PublicRouteRatePerNonce <= 0 {
		cfg.PublicRouteRatePerNonce = defaults.PublicRouteRatePerNonce
	}
	if cfg.PublicRouteRatePerIP <= 0 {
		cfg.PublicRouteRatePerIP = defaults.PublicRouteRatePerIP
	}
	if cfg.PublicRouteTotalPerNonce <= 0 {
		cfg.PublicRouteTotalPerNonce = defaults.PublicRouteTotalPerNonce
	}
	if cfg.PublicRouteFallbackPerIP <= 0 {
		cfg.PublicRouteFallbackPerIP = defaults.PublicRouteFallbackPerIP
	}
	if now == nil {
		now = time.Now
	}
	secret := cfg.RateLimitHMACSecret
	if len(secret) == 0 {
		secret = []byte(formalPoolEgressRateLimitFallbackSecret)
	}
	secretCopy := append([]byte(nil), secret...)

	return &InMemoryFormalPoolEgressRateLimiter{
		nonceRateLimit:  cfg.PublicRouteRatePerNonce,
		ipRateLimit:     cfg.PublicRouteRatePerIP,
		nonceTotalLimit: cfg.PublicRouteTotalPerNonce,
		fallbackIPLimit: cfg.PublicRouteFallbackPerIP,
		nonceTTL:        cfg.NonceTTL,
		now:             now,
		secret:          secretCopy,
		nonceRate:       make(map[string]formalPoolEgressRateLimitCounter),
		ipRate:          make(map[string]formalPoolEgressRateLimitCounter),
		nonceTotal:      make(map[string]formalPoolEgressRateLimitCounter),
		fallbackIP:      make(map[string]formalPoolEgressRateLimitCounter),
	}
}

func (l *InMemoryFormalPoolEgressRateLimiter) CheckEgressCheck(ctx context.Context, nonce, ip string) FormalPoolEgressRateLimitDecision {
	_ = ctx
	return l.check(nonce, ip, false)
}

func (l *InMemoryFormalPoolEgressRateLimiter) CheckEgressCheckFallback(ctx context.Context, nonce, ip string) FormalPoolEgressRateLimitDecision {
	_ = ctx
	return l.check(nonce, ip, true)
}

func (l *InMemoryFormalPoolEgressRateLimiter) check(nonce, ip string, fallback bool) FormalPoolEgressRateLimitDecision {
	if l == nil {
		return FormalPoolEgressRateLimitDecision{Allowed: false, Reason: "handler_unavailable"}
	}
	now := l.now()
	nonceKey, ipKey, decision := l.keysAndDecision(nonce, ip)

	l.mu.Lock()
	defer l.mu.Unlock()

	if fallback {
		if !l.counterAllows(l.fallbackIP, ipKey, l.fallbackIPLimit, formalPoolEgressRateLimitMinuteWindow, now) {
			decision.Reason = "redis_unavailable_fallback"
			return decision
		}
		l.incrementCounter(l.fallbackIP, ipKey, formalPoolEgressRateLimitMinuteWindow, now)
		decision.Allowed = true
		return decision
	}

	if !l.counterAllows(l.nonceRate, nonceKey, l.nonceRateLimit, formalPoolEgressRateLimitMinuteWindow, now) {
		decision.Reason = "per_nonce"
		return decision
	}
	if !l.counterAllows(l.ipRate, ipKey, l.ipRateLimit, formalPoolEgressRateLimitMinuteWindow, now) {
		decision.Reason = "per_ip"
		return decision
	}
	if !l.counterAllows(l.nonceTotal, nonceKey, l.nonceTotalLimit, l.nonceTTL, now) {
		decision.Reason = "nonce_total"
		return decision
	}

	l.incrementCounter(l.nonceRate, nonceKey, formalPoolEgressRateLimitMinuteWindow, now)
	l.incrementCounter(l.ipRate, ipKey, formalPoolEgressRateLimitMinuteWindow, now)
	l.incrementCounter(l.nonceTotal, nonceKey, l.nonceTTL, now)
	decision.Allowed = true
	return decision
}

func (l *InMemoryFormalPoolEgressRateLimiter) counterAllows(counters map[string]formalPoolEgressRateLimitCounter, key string, limit int, window time.Duration, now time.Time) bool {
	if window <= 0 {
		window = formalPoolEgressRateLimitMinuteWindow
	}
	counter, ok := counters[key]
	if !ok || !now.Before(counter.expiresAt) {
		return true
	}
	return counter.count < limit
}

func (l *InMemoryFormalPoolEgressRateLimiter) incrementCounter(counters map[string]formalPoolEgressRateLimitCounter, key string, window time.Duration, now time.Time) {
	if window <= 0 {
		window = formalPoolEgressRateLimitMinuteWindow
	}
	counter, ok := counters[key]
	if !ok || !now.Before(counter.expiresAt) {
		counters[key] = formalPoolEgressRateLimitCounter{count: 1, expiresAt: now.Add(window)}
		return
	}
	counter.count++
	counters[key] = counter
}

func (l *InMemoryFormalPoolEgressRateLimiter) keysAndDecision(nonce, ip string) (string, string, FormalPoolEgressRateLimitDecision) {
	nonceKey := l.hmacHex("nonce_key", nonce)
	ipKey := l.hmacHex("ip_key", ip)
	return nonceKey, ipKey, FormalPoolEgressRateLimitDecision{
		NonceBucket: "nonce_bucket_" + l.hmacHex("nonce_bucket", nonce)[:formalPoolEgressRateLimitBucketHexLen],
		IPBucket:    "ip_bucket_" + l.hmacHex("ip_bucket", ip)[:formalPoolEgressRateLimitBucketHexLen],
	}
}

func (l *InMemoryFormalPoolEgressRateLimiter) hmacHex(scope, raw string) string {
	secret := l.secret
	if len(secret) == 0 {
		secret = []byte(formalPoolEgressRateLimitFallbackSecret)
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte("formal_pool_egress_rate_limit"))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(scope))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte("v1"))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(raw))
	return hex.EncodeToString(mac.Sum(nil))
}
