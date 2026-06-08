package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestProxyEgressCacheHitMiss(t *testing.T) {
	cache := NewProxyEgressCache(ProxyEgressCacheOptions{SuccessTTL: time.Minute, FailureTTL: time.Minute})
	ctx := context.Background()
	var calls int
	probe := func(context.Context, int64, string) (string, error) {
		calls++
		return "203.0.113.10", nil
	}

	rawIP, bucket, err := cache.GetOrProbe(ctx, 10, "http://user:pass@example.test:8080", probe)
	if err != nil {
		t.Fatalf("first GetOrProbe error = %v", err)
	}
	if rawIP != "203.0.113.10" {
		t.Fatalf("first rawIP = %q, want test IP", rawIP)
	}
	if bucket == "" || strings.Contains(bucket, rawIP) {
		t.Fatalf("bucket must be non-empty and must not contain raw IP")
	}

	secondIP, secondBucket, err := cache.GetOrProbe(ctx, 10, "http://user:pass@example.test:8080", probe)
	if err != nil {
		t.Fatalf("second GetOrProbe error = %v", err)
	}
	if secondIP != rawIP || secondBucket != bucket {
		t.Fatalf("second result did not match cached first result")
	}
	if calls != 1 {
		t.Fatalf("probe calls = %d, want 1", calls)
	}
}

func TestProxyEgressCacheSuccessTTLExpiryReprobes(t *testing.T) {
	clock := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	cache := NewProxyEgressCache(ProxyEgressCacheOptions{SuccessTTL: 10 * time.Second, FailureTTL: time.Minute, Now: func() time.Time { return clock }})
	ctx := context.Background()
	var calls int
	probe := func(context.Context, int64, string) (string, error) {
		calls++
		if calls == 1 {
			return "203.0.113.10", nil
		}
		return "203.0.113.11", nil
	}

	firstIP, _, err := cache.GetOrProbe(ctx, 10, "http://proxy-a.example.test:8080", probe)
	if err != nil {
		t.Fatalf("first GetOrProbe error = %v", err)
	}
	clock = clock.Add(9 * time.Second)
	cachedIP, _, err := cache.GetOrProbe(ctx, 10, "http://proxy-a.example.test:8080", probe)
	if err != nil {
		t.Fatalf("cached GetOrProbe error = %v", err)
	}
	clock = clock.Add(2 * time.Second)
	expiredIP, _, err := cache.GetOrProbe(ctx, 10, "http://proxy-a.example.test:8080", probe)
	if err != nil {
		t.Fatalf("expired GetOrProbe error = %v", err)
	}
	if firstIP != "203.0.113.10" || cachedIP != firstIP || expiredIP != "203.0.113.11" {
		t.Fatalf("unexpected TTL sequence")
	}
	if calls != 2 {
		t.Fatalf("probe calls = %d, want 2", calls)
	}
}

func TestProxyEgressCacheFailureTTLExpiryReprobes(t *testing.T) {
	clock := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	cache := NewProxyEgressCache(ProxyEgressCacheOptions{SuccessTTL: time.Minute, FailureTTL: 10 * time.Second, Now: func() time.Time { return clock }})
	ctx := context.Background()
	errProbe := errors.New("probe unavailable")
	var calls int
	probe := func(context.Context, int64, string) (string, error) {
		calls++
		if calls < 3 {
			return "", errProbe
		}
		return "203.0.113.12", nil
	}

	_, _, err := cache.GetOrProbe(ctx, 10, "http://proxy-a.example.test:8080", probe)
	if !errors.Is(err, errProbe) {
		t.Fatalf("first error did not match cached probe error")
	}
	clock = clock.Add(9 * time.Second)
	_, _, err = cache.GetOrProbe(ctx, 10, "http://proxy-a.example.test:8080", probe)
	if !errors.Is(err, errProbe) {
		t.Fatalf("cached failure error did not match probe error")
	}
	clock = clock.Add(2 * time.Second)
	_, _, err = cache.GetOrProbe(ctx, 10, "http://proxy-a.example.test:8080", probe)
	if !errors.Is(err, errProbe) {
		t.Fatalf("expired failure error did not match second probe error")
	}
	clock = clock.Add(11 * time.Second)
	rawIP, bucket, err := cache.GetOrProbe(ctx, 10, "http://proxy-a.example.test:8080", probe)
	if err != nil {
		t.Fatalf("post-failure success error = %v", err)
	}
	if rawIP != "203.0.113.12" || bucket == "" || strings.Contains(bucket, rawIP) {
		t.Fatalf("post-failure success returned unexpected result")
	}
	if calls != 3 {
		t.Fatalf("probe calls = %d, want 3", calls)
	}
}

func TestProxyEgressCacheInvalidateProxy(t *testing.T) {
	cache := NewProxyEgressCache(ProxyEgressCacheOptions{SuccessTTL: time.Minute, FailureTTL: time.Minute})
	ctx := context.Background()
	var calls int
	probe := func(context.Context, int64, string) (string, error) {
		calls++
		return "203.0.113.10", nil
	}

	if _, _, err := cache.GetOrProbe(ctx, 10, "http://proxy-a.example.test:8080", probe); err != nil {
		t.Fatalf("first GetOrProbe error = %v", err)
	}
	cache.InvalidateProxy(10)
	if _, _, err := cache.GetOrProbe(ctx, 10, "http://proxy-a.example.test:8080", probe); err != nil {
		t.Fatalf("after invalidate GetOrProbe error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("probe calls = %d, want 2", calls)
	}
}

func TestProxyEgressCacheKeySeparatesProxyURLsForSameProxyID(t *testing.T) {
	cache := NewProxyEgressCache(ProxyEgressCacheOptions{SuccessTTL: time.Minute, FailureTTL: time.Minute})
	ctx := context.Background()
	var calls int
	probe := func(context.Context, int64, string) (string, error) {
		calls++
		return "203.0.113.10", nil
	}

	if _, _, err := cache.GetOrProbe(ctx, 10, "http://proxy-a.example.test:8080", probe); err != nil {
		t.Fatalf("first GetOrProbe error = %v", err)
	}
	if _, _, err := cache.GetOrProbe(ctx, 10, "http://proxy-b.example.test:8080", probe); err != nil {
		t.Fatalf("second URL GetOrProbe error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("probe calls = %d, want 2", calls)
	}
}

func TestProxyEgressCacheKeySeparatesProxyIDsForSameProxyURL(t *testing.T) {
	cache := NewProxyEgressCache(ProxyEgressCacheOptions{SuccessTTL: time.Minute, FailureTTL: time.Minute})
	ctx := context.Background()
	var calls int
	probe := func(context.Context, int64, string) (string, error) {
		calls++
		return "203.0.113.10", nil
	}

	if _, _, err := cache.GetOrProbe(ctx, 10, "http://proxy-a.example.test:8080", probe); err != nil {
		t.Fatalf("first proxyID GetOrProbe error = %v", err)
	}
	if _, _, err := cache.GetOrProbe(ctx, 11, "http://proxy-a.example.test:8080", probe); err != nil {
		t.Fatalf("second proxyID GetOrProbe error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("probe calls = %d, want 2", calls)
	}
}

func TestProxyEgressCacheSingleflightConcurrentProbe(t *testing.T) {
	cache := NewProxyEgressCache(ProxyEgressCacheOptions{SuccessTTL: time.Minute, FailureTTL: time.Minute})
	ctx := context.Background()
	started := make(chan struct{})
	release := make(chan struct{})
	var calls int64
	probe := func(context.Context, int64, string) (string, error) {
		if atomic.AddInt64(&calls, 1) == 1 {
			close(started)
		}
		<-release
		return "203.0.113.10", nil
	}

	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rawIP, bucket, err := cache.GetOrProbe(ctx, 10, "http://proxy-a.example.test:8080", probe)
			if err != nil {
				errs <- err
				return
			}
			if rawIP != "203.0.113.10" || bucket == "" || strings.Contains(bucket, rawIP) {
				errs <- errors.New("unexpected cached result")
			}
		}()
	}
	<-started
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent GetOrProbe error = %v", err)
		}
	}
	if calls != 1 {
		t.Fatalf("probe calls = %d, want 1", calls)
	}
}

func TestProxyEgressCacheDefaultsNonPositiveTTLs(t *testing.T) {
	cache := NewProxyEgressCache(ProxyEgressCacheOptions{})
	if cache.successTTL != DefaultFormalPoolConfig().ProxyEgressCacheSuccessTTL {
		t.Fatalf("success TTL did not use default")
	}
	if cache.failureTTL != DefaultFormalPoolConfig().ProxyEgressCacheFailureTTL {
		t.Fatalf("failure TTL did not use default")
	}
}
