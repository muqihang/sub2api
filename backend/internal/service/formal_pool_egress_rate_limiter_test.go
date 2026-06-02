package service

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestFormalPoolEgressRateLimiterLimitsPerNonce(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	limiter := NewFormalPoolEgressRateLimiter(FormalPoolConfig{
		NonceTTL:                 time.Minute,
		PublicRouteRatePerNonce:  2,
		PublicRouteRatePerIP:     100,
		PublicRouteTotalPerNonce: 100,
		RateLimitHMACSecret:      []byte("test-secret"),
	}, func() time.Time { return now })

	ctx := context.Background()
	for i := 0; i < 2; i++ {
		decision := limiter.CheckEgressCheck(ctx, "nonce-secret-123", "198.51.100.10")
		if !decision.Allowed || decision.Reason != "" {
			t.Fatalf("decision %d = %+v, want allowed without reason", i, decision)
		}
	}

	decision := limiter.CheckEgressCheck(ctx, "nonce-secret-123", "198.51.100.10")
	if decision.Allowed || decision.Reason != "per_nonce" {
		t.Fatalf("decision = %+v, want per_nonce denial", decision)
	}
}

func TestFormalPoolEgressRateLimiterLimitsPerIP(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	limiter := NewFormalPoolEgressRateLimiter(FormalPoolConfig{
		NonceTTL:                 time.Minute,
		PublicRouteRatePerNonce:  100,
		PublicRouteRatePerIP:     2,
		PublicRouteTotalPerNonce: 100,
		RateLimitHMACSecret:      []byte("test-secret"),
	}, func() time.Time { return now })

	ctx := context.Background()
	for _, nonce := range []string{"nonce-a", "nonce-b"} {
		decision := limiter.CheckEgressCheck(ctx, nonce, "198.51.100.11")
		if !decision.Allowed {
			t.Fatalf("decision for %q = %+v, want allowed", nonce, decision)
		}
	}

	decision := limiter.CheckEgressCheck(ctx, "nonce-c", "198.51.100.11")
	if decision.Allowed || decision.Reason != "per_ip" {
		t.Fatalf("decision = %+v, want per_ip denial", decision)
	}
}

func TestFormalPoolEgressRateLimiterLimitsNonceTotalWithinTTL(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	limiter := NewFormalPoolEgressRateLimiter(FormalPoolConfig{
		NonceTTL:                 5 * time.Minute,
		PublicRouteRatePerNonce:  100,
		PublicRouteRatePerIP:     100,
		PublicRouteTotalPerNonce: 2,
		RateLimitHMACSecret:      []byte("test-secret"),
	}, func() time.Time { return now })

	ctx := context.Background()
	for i := 0; i < 2; i++ {
		decision := limiter.CheckEgressCheck(ctx, "nonce-total", "198.51.100.12")
		if !decision.Allowed {
			t.Fatalf("decision %d = %+v, want allowed", i, decision)
		}
		now = now.Add(90 * time.Second)
	}

	decision := limiter.CheckEgressCheck(ctx, "nonce-total", "198.51.100.12")
	if decision.Allowed || decision.Reason != "nonce_total" {
		t.Fatalf("decision = %+v, want nonce_total denial", decision)
	}
}

func TestFormalPoolEgressRateLimiterFallbackPerIP(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	limiter := NewFormalPoolEgressRateLimiter(FormalPoolConfig{
		PublicRouteFallbackPerIP: 2,
		RateLimitHMACSecret:      []byte("test-secret"),
	}, func() time.Time { return now })

	ctx := context.Background()
	for _, nonce := range []string{"nonce-a", "nonce-b"} {
		decision := limiter.CheckEgressCheckFallback(ctx, nonce, "198.51.100.13")
		if !decision.Allowed {
			t.Fatalf("fallback decision for %q = %+v, want allowed", nonce, decision)
		}
	}

	decision := limiter.CheckEgressCheckFallback(ctx, "nonce-c", "198.51.100.13")
	if decision.Allowed || decision.Reason != "redis_unavailable_fallback" {
		t.Fatalf("decision = %+v, want redis_unavailable_fallback denial", decision)
	}
}

func TestFormalPoolEgressRateLimiterBucketsAreSafeAndDoNotContainRawInput(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	rawNonce := "raw-nonce-secret@example.test"
	rawIP := "203.0.113.44"
	limiter := NewFormalPoolEgressRateLimiter(FormalPoolConfig{
		RateLimitHMACSecret: []byte("test-secret"),
	}, func() time.Time { return now })

	decision := limiter.CheckEgressCheck(context.Background(), rawNonce, rawIP)
	assertFormalPoolEgressSafeBuckets(t, decision, rawNonce, rawIP)
}

func TestFormalPoolEgressRateLimiterWindowExpiryAllowsAgain(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	limiter := NewFormalPoolEgressRateLimiter(FormalPoolConfig{
		NonceTTL:                 5 * time.Minute,
		PublicRouteRatePerNonce:  1,
		PublicRouteRatePerIP:     1,
		PublicRouteTotalPerNonce: 1,
		RateLimitHMACSecret:      []byte("test-secret"),
	}, func() time.Time { return now })

	ctx := context.Background()
	if decision := limiter.CheckEgressCheck(ctx, "nonce-expiry", "198.51.100.14"); !decision.Allowed {
		t.Fatalf("first decision = %+v, want allowed", decision)
	}
	if decision := limiter.CheckEgressCheck(ctx, "nonce-expiry", "198.51.100.14"); decision.Allowed {
		t.Fatalf("second decision = %+v, want denied before expiry", decision)
	}

	now = now.Add(5*time.Minute + time.Nanosecond)
	if decision := limiter.CheckEgressCheck(ctx, "nonce-expiry", "198.51.100.14"); !decision.Allowed {
		t.Fatalf("third decision = %+v, want allowed after expiry", decision)
	}
}

func TestFormalPoolEgressRateLimiterEmptyNonceAndIPAreSafe(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	limiter := NewFormalPoolEgressRateLimiter(FormalPoolConfig{
		RateLimitHMACSecret: []byte("test-secret"),
	}, func() time.Time { return now })

	decision := limiter.CheckEgressCheck(context.Background(), "", "")
	if !decision.Allowed {
		t.Fatalf("decision = %+v, want allowed", decision)
	}
	assertFormalPoolEgressSafeBuckets(t, decision, "", "")
}

func assertFormalPoolEgressSafeBuckets(t *testing.T, decision FormalPoolEgressRateLimitDecision, rawNonce, rawIP string) {
	t.Helper()
	nonceBucketRe := regexp.MustCompile(`^nonce_bucket_[0-9a-f]{8,32}$`)
	ipBucketRe := regexp.MustCompile(`^ip_bucket_[0-9a-f]{8,32}$`)
	if !nonceBucketRe.MatchString(decision.NonceBucket) {
		t.Fatalf("NonceBucket = %q, want nonce safe bucket", decision.NonceBucket)
	}
	if !ipBucketRe.MatchString(decision.IPBucket) {
		t.Fatalf("IPBucket = %q, want IP safe bucket", decision.IPBucket)
	}
	for _, raw := range []string{rawNonce, rawIP} {
		if raw == "" {
			continue
		}
		if strings.Contains(decision.NonceBucket, raw) || strings.Contains(decision.IPBucket, raw) {
			t.Fatalf("decision buckets %+v contain raw input %q", decision, raw)
		}
	}
}
