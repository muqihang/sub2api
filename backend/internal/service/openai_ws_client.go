package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	openaiwsv2 "github.com/Wei-Shaw/sub2api/internal/service/openai_ws_v2"
	coderws "github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	gorillaws "github.com/gorilla/websocket"
)

const openAIWSMessageReadLimitBytes int64 = 16 * 1024 * 1024
const (
	openAIWSProxyTransportMaxIdleConns        = 128
	openAIWSProxyTransportMaxIdleConnsPerHost = 64
	openAIWSProxyTransportIdleConnTimeout     = 90 * time.Second
	openAIWSProxyClientCacheMaxEntries        = 256
	openAIWSProxyClientCacheIdleTTL           = 15 * time.Minute
)

type OpenAIWSTransportMetricsSnapshot struct {
	ProxyClientCacheHits   int64   `json:"proxy_client_cache_hits"`
	ProxyClientCacheMisses int64   `json:"proxy_client_cache_misses"`
	TransportReuseRatio    float64 `json:"transport_reuse_ratio"`
}

// openAIWSClientConn 抽象 WS 客户端连接，便于替换底层实现。
type openAIWSClientConn interface {
	WriteJSON(ctx context.Context, value any) error
	ReadMessage(ctx context.Context) ([]byte, error)
	Ping(ctx context.Context) error
	Close() error
}

// openAIWSClientDialer 抽象 WS 建连器。
type openAIWSClientDialer interface {
	Dial(ctx context.Context, wsURL string, headers http.Header, proxyURL string, effectiveTLS *OpenAIGatewayEffectiveTLS) (openAIWSClientConn, int, http.Header, error)
}

type openAIWSTransportMetricsDialer interface {
	SnapshotTransportMetrics() OpenAIWSTransportMetricsSnapshot
}

func newDefaultOpenAIWSClientDialer() openAIWSClientDialer {
	return &coderOpenAIWSClientDialer{
		proxyClients: make(map[string]*openAIWSProxyClientEntry),
	}
}

type coderOpenAIWSClientDialer struct {
	proxyMu      sync.Mutex
	proxyClients map[string]*openAIWSProxyClientEntry
	proxyHits    atomic.Int64
	proxyMisses  atomic.Int64
}

type openAIWSProxyClientEntry struct {
	client           *http.Client
	lastUsedUnixNano int64
}

func (d *coderOpenAIWSClientDialer) Dial(
	ctx context.Context,
	wsURL string,
	headers http.Header,
	proxyURL string,
	effectiveTLS *OpenAIGatewayEffectiveTLS,
) (openAIWSClientConn, int, http.Header, error) {
	targetURL := strings.TrimSpace(wsURL)
	if targetURL == "" {
		return nil, 0, nil, errors.New("ws url is empty")
	}

	if !openAIWSEffectiveTLSEnabled(effectiveTLS) && shouldUseGorillaOpenAIWSDialer(headers) {
		return d.dialWithGorilla(ctx, targetURL, headers, proxyURL)
	}

	opts := &coderws.DialOptions{
		HTTPHeader:      cloneHeader(headers),
		CompressionMode: coderws.CompressionDisabled,
	}
	if openAIWSEffectiveTLSEnabled(effectiveTLS) {
		tlsClient, err := d.openAIWSHTTPClientForTLS(proxyURL, effectiveTLS)
		if err != nil {
			return nil, 0, nil, err
		}
		opts.HTTPClient = tlsClient
	} else if proxy := strings.TrimSpace(proxyURL); proxy != "" {
		proxyClient, err := d.proxyHTTPClient(proxy)
		if err != nil {
			return nil, 0, nil, err
		}
		opts.HTTPClient = proxyClient
	}

	conn, resp, err := coderws.Dial(ctx, targetURL, opts)
	if err != nil {
		status := 0
		respHeaders := http.Header(nil)
		if resp != nil {
			status = resp.StatusCode
			respHeaders = cloneHeader(resp.Header)
		}
		return nil, status, respHeaders, err
	}
	conn.SetReadLimit(openAIWSMessageReadLimitBytes)
	respHeaders := http.Header(nil)
	if resp != nil {
		respHeaders = cloneHeader(resp.Header)
	}
	return &coderOpenAIWSClientConn{conn: conn}, 0, respHeaders, nil
}

func (d *coderOpenAIWSClientDialer) openAIWSHTTPClientForTLS(proxy string, effectiveTLS *OpenAIGatewayEffectiveTLS) (*http.Client, error) {
	return d.proxyHTTPClient(proxy, effectiveTLS)
}

func (d *coderOpenAIWSClientDialer) proxyHTTPClient(proxy string, effectiveTLSValues ...*OpenAIGatewayEffectiveTLS) (*http.Client, error) {
	if d == nil {
		return nil, errors.New("openai ws dialer is nil")
	}
	normalizedProxy := strings.TrimSpace(proxy)
	var effectiveTLS *OpenAIGatewayEffectiveTLS
	if len(effectiveTLSValues) > 0 {
		effectiveTLS = effectiveTLSValues[0]
	}
	tlsEnabled := openAIWSEffectiveTLSEnabled(effectiveTLS)
	if normalizedProxy == "" && !tlsEnabled {
		return nil, errors.New("proxy url is empty")
	}
	var parsedProxyURL *url.URL
	if normalizedProxy != "" {
		var err error
		parsedProxyURL, err = url.Parse(normalizedProxy)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy url: %w", err)
		}
	}
	now := time.Now().UnixNano()
	cacheKey := openAIWSProxyClientCacheKey(normalizedProxy, effectiveTLS)

	d.proxyMu.Lock()
	defer d.proxyMu.Unlock()
	if entry, ok := d.proxyClients[cacheKey]; ok && entry != nil && entry.client != nil {
		entry.lastUsedUnixNano = now
		d.proxyHits.Add(1)
		return entry.client, nil
	}
	d.cleanupProxyClientsLocked(now)
	transport, err := buildOpenAIWSHTTPTransport(parsedProxyURL, effectiveTLS)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Transport: transport}
	d.proxyClients[cacheKey] = &openAIWSProxyClientEntry{
		client:           client,
		lastUsedUnixNano: now,
	}
	d.ensureProxyClientCapacityLocked()
	d.proxyMisses.Add(1)
	return client, nil
}

func buildOpenAIWSHTTPTransport(proxyURL *url.URL, effectiveTLS *OpenAIGatewayEffectiveTLS) (*http.Transport, error) {
	transport := &http.Transport{
		MaxIdleConns:        openAIWSProxyTransportMaxIdleConns,
		MaxIdleConnsPerHost: openAIWSProxyTransportMaxIdleConnsPerHost,
		IdleConnTimeout:     openAIWSProxyTransportIdleConnTimeout,
		TLSHandshakeTimeout: 10 * time.Second,
		ForceAttemptHTTP2:   true,
	}
	if !openAIWSEffectiveTLSEnabled(effectiveTLS) {
		if proxyURL != nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
		return transport, nil
	}
	if !effectiveTLS.WSApplicable {
		return nil, fmt.Errorf("openai ws tls-bound transport unsupported: effective TLS is not WS-applicable cache_identity=%s", strings.TrimSpace(effectiveTLS.CacheIdentity))
	}
	if effectiveTLS.Profile == nil {
		return nil, fmt.Errorf("openai ws tls-bound transport unsupported: missing TLS profile cache_identity=%s", strings.TrimSpace(effectiveTLS.CacheIdentity))
	}
	transport.ForceAttemptHTTP2 = false
	switch {
	case proxyURL == nil:
		dialer := tlsfingerprint.NewDialer(effectiveTLS.Profile, nil)
		transport.DialTLSContext = dialer.DialTLSContext
	default:
		switch strings.ToLower(strings.TrimSpace(proxyURL.Scheme)) {
		case "http", "https":
			if strings.EqualFold(strings.TrimSpace(proxyURL.Scheme), "https") {
				return nil, errors.New("openai ws tls-bound transport unsupported: https proxy unsupported for TLS-bound WS")
			}
			dialer := tlsfingerprint.NewHTTPProxyDialer(effectiveTLS.Profile, proxyURL)
			transport.DialTLSContext = dialer.DialTLSContext
		case "socks5", "socks5h":
			dialer := tlsfingerprint.NewSOCKS5ProxyDialer(effectiveTLS.Profile, proxyURL)
			transport.DialTLSContext = dialer.DialTLSContext
		default:
			return nil, fmt.Errorf("openai ws tls-bound transport unsupported proxy scheme %q", proxyURL.Scheme)
		}
	}
	return transport, nil
}

func openAIWSDialerStrategyDiagnostics(headers http.Header, proxyURL string, effectiveTLS *OpenAIGatewayEffectiveTLS) map[string]string {
	diagnostics := map[string]string{
		"ws_transport_supported":          "true",
		"ws_transport_unsupported_reason": "",
	}
	if openAIWSEffectiveTLSEnabled(effectiveTLS) {
		diagnostics["ws_dialer_strategy"] = "coder_custom_http_client"
		if !effectiveTLS.WSApplicable {
			diagnostics["ws_transport_supported"] = "false"
			diagnostics["ws_transport_unsupported_reason"] = strings.TrimSpace(effectiveTLS.FallbackReason)
		}
		if reason := unsupportedOpenAIWSTLSProxyReason(proxyURL); reason != "" {
			diagnostics["ws_transport_supported"] = "false"
			diagnostics["ws_transport_unsupported_reason"] = reason
		}
		if strings.TrimSpace(diagnostics["ws_transport_unsupported_reason"]) == "" && effectiveTLS.Profile == nil {
			diagnostics["ws_transport_supported"] = "false"
			diagnostics["ws_transport_unsupported_reason"] = "missing_tls_profile"
		}
		return diagnostics
	}
	if shouldUseGorillaOpenAIWSDialer(headers) {
		diagnostics["ws_dialer_strategy"] = "gorilla_fallback"
		return diagnostics
	}
	diagnostics["ws_dialer_strategy"] = "coder_default"
	return diagnostics
}

func unsupportedOpenAIWSTLSProxyReason(proxy string) string {
	proxy = strings.TrimSpace(proxy)
	if proxy == "" {
		return ""
	}
	parsed, err := url.Parse(proxy)
	if err != nil {
		return "invalid_proxy_url"
	}
	switch strings.ToLower(strings.TrimSpace(parsed.Scheme)) {
	case "", "http", "socks5", "socks5h":
		return ""
	case "https":
		return "https_proxy_unsupported_for_tls_bound_ws"
	default:
		return "proxy_scheme_unsupported_for_tls_bound_ws"
	}
}

func openAIWSEffectiveTLSEnabled(effectiveTLS *OpenAIGatewayEffectiveTLS) bool {
	return effectiveTLS != nil && effectiveTLS.Enabled
}

func openAIWSProxyClientCacheKey(proxy string, effectiveTLS *OpenAIGatewayEffectiveTLS) string {
	normalizedProxy := strings.TrimSpace(proxy)
	if normalizedProxy == "" {
		normalizedProxy = "direct"
	}
	if !openAIWSEffectiveTLSEnabled(effectiveTLS) {
		return normalizedProxy
	}
	identity := strings.TrimSpace(effectiveTLS.CacheIdentity)
	if identity == "" {
		identity = strings.TrimSpace(effectiveTLS.ProfileHash)
	}
	if identity == "" && effectiveTLS.ProfileID > 0 {
		identity = fmt.Sprintf("profile_id:%d", effectiveTLS.ProfileID)
	}
	if identity == "" {
		identity = "tls-bound"
	}
	return normalizedProxy + "|tls_identity=" + identity
}

func (d *coderOpenAIWSClientDialer) cleanupProxyClientsLocked(nowUnixNano int64) {
	if d == nil || len(d.proxyClients) == 0 {
		return
	}
	idleTTL := openAIWSProxyClientCacheIdleTTL
	if idleTTL <= 0 {
		return
	}
	now := time.Unix(0, nowUnixNano)
	for key, entry := range d.proxyClients {
		if entry == nil || entry.client == nil {
			delete(d.proxyClients, key)
			continue
		}
		lastUsed := time.Unix(0, entry.lastUsedUnixNano)
		if now.Sub(lastUsed) > idleTTL {
			closeOpenAIWSProxyClient(entry.client)
			delete(d.proxyClients, key)
		}
	}
}

func (d *coderOpenAIWSClientDialer) ensureProxyClientCapacityLocked() {
	if d == nil {
		return
	}
	maxEntries := openAIWSProxyClientCacheMaxEntries
	if maxEntries <= 0 {
		return
	}
	for len(d.proxyClients) > maxEntries {
		var oldestKey string
		var oldestLastUsed int64
		hasOldest := false
		for key, entry := range d.proxyClients {
			lastUsed := int64(0)
			if entry != nil {
				lastUsed = entry.lastUsedUnixNano
			}
			if !hasOldest || lastUsed < oldestLastUsed {
				hasOldest = true
				oldestKey = key
				oldestLastUsed = lastUsed
			}
		}
		if !hasOldest {
			return
		}
		if entry := d.proxyClients[oldestKey]; entry != nil {
			closeOpenAIWSProxyClient(entry.client)
		}
		delete(d.proxyClients, oldestKey)
	}
}

func closeOpenAIWSProxyClient(client *http.Client) {
	if client == nil || client.Transport == nil {
		return
	}
	if transport, ok := client.Transport.(*http.Transport); ok && transport != nil {
		transport.CloseIdleConnections()
	}
}

func shouldUseGorillaOpenAIWSDialer(headers http.Header) bool {
	if headers == nil {
		return false
	}
	return strings.TrimSpace(headers.Get("chatgpt-account-id")) != ""
}

func (d *coderOpenAIWSClientDialer) dialWithGorilla(
	ctx context.Context,
	wsURL string,
	headers http.Header,
	proxyURL string,
) (openAIWSClientConn, int, http.Header, error) {
	gorillaDialer := &gorillaws.Dialer{
		HandshakeTimeout:  40 * time.Second,
		EnableCompression: false,
	}
	if proxy := strings.TrimSpace(proxyURL); proxy != "" {
		parsedProxyURL, err := url.Parse(proxy)
		if err != nil {
			return nil, 0, nil, fmt.Errorf("invalid proxy url: %w", err)
		}
		gorillaDialer.Proxy = http.ProxyURL(parsedProxyURL)
	}
	conn, resp, err := gorillaDialer.DialContext(ctx, wsURL, cloneHeader(headers))
	if err != nil {
		status := 0
		respHeaders := http.Header(nil)
		if resp != nil {
			status = resp.StatusCode
			respHeaders = cloneHeader(resp.Header)
		}
		return nil, status, respHeaders, err
	}
	conn.SetReadLimit(openAIWSMessageReadLimitBytes)
	respHeaders := http.Header(nil)
	if resp != nil {
		respHeaders = cloneHeader(resp.Header)
	}
	return &gorillaOpenAIWSClientConn{conn: conn}, 0, respHeaders, nil
}

func (d *coderOpenAIWSClientDialer) SnapshotTransportMetrics() OpenAIWSTransportMetricsSnapshot {
	if d == nil {
		return OpenAIWSTransportMetricsSnapshot{}
	}
	hits := d.proxyHits.Load()
	misses := d.proxyMisses.Load()
	total := hits + misses
	reuseRatio := 0.0
	if total > 0 {
		reuseRatio = float64(hits) / float64(total)
	}
	return OpenAIWSTransportMetricsSnapshot{
		ProxyClientCacheHits:   hits,
		ProxyClientCacheMisses: misses,
		TransportReuseRatio:    reuseRatio,
	}
}

type coderOpenAIWSClientConn struct {
	conn *coderws.Conn
}

var _ openaiwsv2.FrameConn = (*coderOpenAIWSClientConn)(nil)

func (c *coderOpenAIWSClientConn) WriteJSON(ctx context.Context, value any) error {
	if c == nil || c.conn == nil {
		return errOpenAIWSConnClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return wsjson.Write(ctx, c.conn, value)
}

func (c *coderOpenAIWSClientConn) ReadMessage(ctx context.Context) ([]byte, error) {
	if c == nil || c.conn == nil {
		return nil, errOpenAIWSConnClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}

	msgType, payload, err := c.conn.Read(ctx)
	if err != nil {
		return nil, err
	}
	switch msgType {
	case coderws.MessageText, coderws.MessageBinary:
		return payload, nil
	default:
		return nil, errOpenAIWSConnClosed
	}
}

func (c *coderOpenAIWSClientConn) ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error) {
	if c == nil || c.conn == nil {
		return coderws.MessageText, nil, errOpenAIWSConnClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	msgType, payload, err := c.conn.Read(ctx)
	if err != nil {
		return coderws.MessageText, nil, err
	}
	return msgType, payload, nil
}

func (c *coderOpenAIWSClientConn) WriteFrame(ctx context.Context, msgType coderws.MessageType, payload []byte) error {
	if c == nil || c.conn == nil {
		return errOpenAIWSConnClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return c.conn.Write(ctx, msgType, payload)
}

func (c *coderOpenAIWSClientConn) Ping(ctx context.Context) error {
	if c == nil || c.conn == nil {
		return errOpenAIWSConnClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return c.conn.Ping(ctx)
}

func (c *coderOpenAIWSClientConn) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	// Close 为幂等，忽略重复关闭错误。
	_ = c.conn.Close(coderws.StatusNormalClosure, "")
	_ = c.conn.CloseNow()
	return nil
}

type gorillaOpenAIWSClientConn struct {
	conn *gorillaws.Conn
}

var _ openaiwsv2.FrameConn = (*gorillaOpenAIWSClientConn)(nil)

func (c *gorillaOpenAIWSClientConn) WriteJSON(ctx context.Context, value any) error {
	if c == nil || c.conn == nil {
		return errOpenAIWSConnClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = c.conn.SetWriteDeadline(deadline)
	} else {
		_ = c.conn.SetWriteDeadline(time.Time{})
	}
	return c.conn.WriteJSON(value)
}

func (c *gorillaOpenAIWSClientConn) ReadMessage(ctx context.Context) ([]byte, error) {
	msgType, payload, err := c.ReadFrame(ctx)
	if err != nil {
		return nil, err
	}
	switch msgType {
	case coderws.MessageText, coderws.MessageBinary:
		return payload, nil
	default:
		return nil, errOpenAIWSConnClosed
	}
}

func (c *gorillaOpenAIWSClientConn) ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error) {
	if c == nil || c.conn == nil {
		return coderws.MessageText, nil, errOpenAIWSConnClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = c.conn.SetReadDeadline(deadline)
	} else {
		_ = c.conn.SetReadDeadline(time.Time{})
	}
	msgType, payload, err := c.conn.ReadMessage()
	if err != nil {
		return coderws.MessageText, nil, err
	}
	switch msgType {
	case gorillaws.TextMessage:
		return coderws.MessageText, payload, nil
	case gorillaws.BinaryMessage:
		return coderws.MessageBinary, payload, nil
	default:
		return coderws.MessageText, payload, nil
	}
}

func (c *gorillaOpenAIWSClientConn) WriteFrame(ctx context.Context, msgType coderws.MessageType, payload []byte) error {
	if c == nil || c.conn == nil {
		return errOpenAIWSConnClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = c.conn.SetWriteDeadline(deadline)
	} else {
		_ = c.conn.SetWriteDeadline(time.Time{})
	}
	gType := gorillaws.TextMessage
	if msgType == coderws.MessageBinary {
		gType = gorillaws.BinaryMessage
	}
	return c.conn.WriteMessage(gType, payload)
}

func (c *gorillaOpenAIWSClientConn) Ping(ctx context.Context) error {
	if c == nil || c.conn == nil {
		return errOpenAIWSConnClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	deadline := time.Now().Add(10 * time.Second)
	if d, ok := ctx.Deadline(); ok {
		deadline = d
	}
	return c.conn.WriteControl(gorillaws.PingMessage, nil, deadline)
}

func (c *gorillaOpenAIWSClientConn) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	_ = c.conn.WriteControl(gorillaws.CloseMessage, gorillaws.FormatCloseMessage(gorillaws.CloseNormalClosure, ""), time.Now().Add(time.Second))
	return c.conn.Close()
}
