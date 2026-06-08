package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	proxyEgressCacheKeyScope    = "proxy_config"
	proxyEgressCacheBucketScope = "proxy_egress"
)

// ProxyEgressProbe resolves the raw exit IP for a normalized proxy URL.
type ProxyEgressProbe func(ctx context.Context, proxyID int64, normalizedProxyURL string) (rawIP string, err error)

type ProxyEgressCacheOptions struct {
	SuccessTTL time.Duration
	FailureTTL time.Duration
	Now        func() time.Time
}

type ProxyEgressCache struct {
	mu         sync.Mutex
	entries    map[proxyEgressCacheKey]proxyEgressCacheEntry
	inflight   map[proxyEgressCacheKey]*proxyEgressCacheCall
	successTTL time.Duration
	failureTTL time.Duration
	now        func() time.Time
}

type proxyEgressCacheKey struct {
	proxyID      int64
	proxyURLHash string
}

type proxyEgressCacheEntry struct {
	rawIP     string
	bucket    string
	err       error
	expiresAt time.Time
}

type proxyEgressCacheCall struct {
	done   chan struct{}
	rawIP  string
	bucket string
	err    error
}

func NewProxyEgressCache(opts ProxyEgressCacheOptions) *ProxyEgressCache {
	defaults := DefaultFormalPoolConfig()
	successTTL := opts.SuccessTTL
	if successTTL <= 0 {
		successTTL = defaults.ProxyEgressCacheSuccessTTL
	}
	failureTTL := opts.FailureTTL
	if failureTTL <= 0 {
		failureTTL = defaults.ProxyEgressCacheFailureTTL
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &ProxyEgressCache{
		entries:    make(map[proxyEgressCacheKey]proxyEgressCacheEntry),
		inflight:   make(map[proxyEgressCacheKey]*proxyEgressCacheCall),
		successTTL: successTTL,
		failureTTL: failureTTL,
		now:        now,
	}
}

func (c *ProxyEgressCache) GetOrProbe(ctx context.Context, proxyID int64, normalizedProxyURL string, probe ProxyEgressProbe) (rawIP string, bucket string, err error) {
	if c == nil {
		return "", "", errors.New("proxy egress cache is nil")
	}
	if probe == nil {
		return "", "", errors.New("proxy egress probe is nil")
	}
	key := proxyEgressCacheKeyFor(proxyID, normalizedProxyURL)

	c.mu.Lock()
	if entry, ok := c.entries[key]; ok && c.now().Before(entry.expiresAt) {
		c.mu.Unlock()
		return entry.rawIP, entry.bucket, entry.err
	}
	if call, ok := c.inflight[key]; ok {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-call.done:
			return call.rawIP, call.bucket, call.err
		}
	}
	call := &proxyEgressCacheCall{done: make(chan struct{})}
	c.inflight[key] = call
	c.mu.Unlock()

	rawIP, bucket, err = c.probeAndStore(ctx, key, proxyID, normalizedProxyURL, probe)

	c.mu.Lock()
	call.rawIP = rawIP
	call.bucket = bucket
	call.err = err
	delete(c.inflight, key)
	close(call.done)
	c.mu.Unlock()

	return rawIP, bucket, err
}

func (c *ProxyEgressCache) InvalidateProxy(proxyID int64) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for key := range c.entries {
		if key.proxyID == proxyID {
			delete(c.entries, key)
		}
	}
}

func (c *ProxyEgressCache) probeAndStore(ctx context.Context, key proxyEgressCacheKey, proxyID int64, normalizedProxyURL string, probe ProxyEgressProbe) (string, string, error) {
	rawIP, err := probe(ctx, proxyID, normalizedProxyURL)
	bucket := ""
	ttl := c.failureTTL
	if err == nil {
		bucket = formalPoolSafeRef(proxyEgressCacheBucketScope, rawIP)
		ttl = c.successTTL
	}
	if ttl <= 0 {
		if err == nil {
			ttl = DefaultFormalPoolConfig().ProxyEgressCacheSuccessTTL
		} else {
			ttl = DefaultFormalPoolConfig().ProxyEgressCacheFailureTTL
		}
	}
	entry := proxyEgressCacheEntry{rawIP: rawIP, bucket: bucket, err: err, expiresAt: c.now().Add(ttl)}

	c.mu.Lock()
	c.entries[key] = entry
	c.mu.Unlock()

	return rawIP, bucket, err
}

func proxyEgressCacheKeyFor(proxyID int64, normalizedProxyURL string) proxyEgressCacheKey {
	return proxyEgressCacheKey{
		proxyID:      proxyID,
		proxyURLHash: formalPoolSafeRef(proxyEgressCacheKeyScope, normalizedProxyURL),
	}
}

func (k proxyEgressCacheKey) String() string {
	return fmt.Sprintf("proxy_id:%d:%s", k.proxyID, k.proxyURLHash)
}
