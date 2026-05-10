package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func TestCoderOpenAIWSClientDialer_ProxyHTTPClientReuse(t *testing.T) {
	dialer := newDefaultOpenAIWSClientDialer()
	impl, ok := dialer.(*coderOpenAIWSClientDialer)
	require.True(t, ok)

	c1, err := impl.proxyHTTPClient("http://127.0.0.1:8080")
	require.NoError(t, err)
	c2, err := impl.proxyHTTPClient("http://127.0.0.1:8080")
	require.NoError(t, err)
	require.Same(t, c1, c2, "同一代理地址应复用同一个 HTTP 客户端")

	c3, err := impl.proxyHTTPClient("http://127.0.0.1:8081")
	require.NoError(t, err)
	require.NotSame(t, c1, c3, "不同代理地址应分离客户端")
}

func TestCoderOpenAIWSClientDialer_ProxyHTTPClientSeparatesTLSIdentity(t *testing.T) {
	dialer := newDefaultOpenAIWSClientDialer()
	impl, ok := dialer.(*coderOpenAIWSClientDialer)
	require.True(t, ok)

	tlsA := testOpenAIWSEffectiveTLS("profile-a")
	tlsB := testOpenAIWSEffectiveTLS("profile-b")

	c1, err := impl.proxyHTTPClient("http://127.0.0.1:8080", tlsA)
	require.NoError(t, err)
	c2, err := impl.proxyHTTPClient("http://127.0.0.1:8080", tlsA)
	require.NoError(t, err)
	require.Same(t, c1, c2, "same proxy and TLS cache identity should reuse the HTTP client")

	c3, err := impl.proxyHTTPClient("http://127.0.0.1:8080", tlsB)
	require.NoError(t, err)
	require.NotSame(t, c1, c3, "same proxy must not reuse a client across TLS cache identities")
}

func TestCoderOpenAIWSClientDialer_PlainOAuthUsesGorillaFallback(t *testing.T) {
	var upstreamHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		http.Error(w, "unexpected upstream handshake", http.StatusTeapot)
	}))
	defer server.Close()

	dialer := newDefaultOpenAIWSClientDialer()
	impl, ok := dialer.(*coderOpenAIWSClientDialer)
	require.True(t, ok)

	headers := http.Header{}
	headers.Set("chatgpt-account-id", "acct_123")
	_, _, _, err := impl.Dial(
		context.Background(),
		"ws"+server.URL[len("http"):],
		headers,
		"",
		nil,
	)
	require.Error(t, err)
	require.Equal(t, int32(1), upstreamHits.Load(), "plain OAuth path should retain the gorilla fallback behavior")
}

func TestCoderOpenAIWSClientDialer_TLSBoundOAuthPrefersCoderRouteDespiteChatGPTAccountID(t *testing.T) {
	requestHeaders := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestHeaders <- r.Header.Clone()
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
		require.NoError(t, err)
		defer conn.CloseNow()
		_ = conn.Close(websocket.StatusNormalClosure, "done")
	}))
	defer server.Close()

	dialer := newDefaultOpenAIWSClientDialer()
	impl, ok := dialer.(*coderOpenAIWSClientDialer)
	require.True(t, ok)

	headers := http.Header{}
	headers.Set("chatgpt-account-id", "acct_123")
	conn, _, _, err := impl.Dial(
		context.Background(),
		"ws"+server.URL[len("http"):],
		headers,
		"",
		testOpenAIWSEffectiveTLS("profile-a"),
	)
	require.NoError(t, err)
	require.NotNil(t, conn)
	defer conn.Close()

	select {
	case got := <-requestHeaders:
		require.Equal(t, "acct_123", got.Get("chatgpt-account-id"))
	case <-time.After(3 * time.Second):
		t.Fatal("waiting for upstream WS handshake timed out")
	}
}

func TestCoderOpenAIWSClientDialer_TLSBoundCoderRouteBuildsCustomHTTPClient(t *testing.T) {
	dialer := newDefaultOpenAIWSClientDialer()
	impl, ok := dialer.(*coderOpenAIWSClientDialer)
	require.True(t, ok)

	client, err := impl.openAIWSHTTPClientForTLS("", testOpenAIWSEffectiveTLS("profile-a"))
	require.NoError(t, err)
	require.NotNil(t, client)

	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, transport.DialTLSContext, "TLS-bound coder path should inject a transport with uTLS DialTLSContext")
	require.False(t, transport.ForceAttemptHTTP2, "uTLS WS transport must not silently fall back to the standard HTTP/2 TLS path")

	proxyClient, err := impl.openAIWSHTTPClientForTLS("http://127.0.0.1:8080", testOpenAIWSEffectiveTLS("profile-proxy"))
	require.NoError(t, err)
	require.NotNil(t, proxyClient)
	proxyTransport, ok := proxyClient.Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, proxyTransport.DialTLSContext, "TLS-bound coder path should support uTLS through HTTP CONNECT proxies")
	require.Nil(t, proxyTransport.Proxy, "TLS-bound proxy support must not use the standard proxy TLS path")
}

func TestCoderOpenAIWSClientDialer_TLSBoundHTTPSProxyFailsClosed(t *testing.T) {
	dialer := newDefaultOpenAIWSClientDialer()
	impl, ok := dialer.(*coderOpenAIWSClientDialer)
	require.True(t, ok)

	_, err := impl.openAIWSHTTPClientForTLS("https://127.0.0.1:8443", testOpenAIWSEffectiveTLS("profile-https-proxy"))
	require.Error(t, err)
	require.ErrorContains(t, err, "https proxy unsupported for TLS-bound WS")
}

func TestOpenAIWSDialerStrategyDiagnostics(t *testing.T) {
	oauthHeaders := http.Header{}
	oauthHeaders.Set("chatgpt-account-id", "acct_123")
	plainOAuth := openAIWSDialerStrategyDiagnostics(oauthHeaders, "", nil)
	require.Equal(t, "gorilla_fallback", plainOAuth["ws_dialer_strategy"])
	require.Equal(t, "true", plainOAuth["ws_transport_supported"])

	tlsOAuth := openAIWSDialerStrategyDiagnostics(oauthHeaders, "", testOpenAIWSEffectiveTLS("profile-a"))
	require.Equal(t, "coder_custom_http_client", tlsOAuth["ws_dialer_strategy"])
	require.Equal(t, "true", tlsOAuth["ws_transport_supported"])
	require.Empty(t, tlsOAuth["ws_transport_unsupported_reason"])

	tlsHTTPSProxy := openAIWSDialerStrategyDiagnostics(http.Header{}, "https://127.0.0.1:8443", testOpenAIWSEffectiveTLS("profile-a"))
	require.Equal(t, "coder_custom_http_client", tlsHTTPSProxy["ws_dialer_strategy"])
	require.Equal(t, "false", tlsHTTPSProxy["ws_transport_supported"])
	require.Equal(t, "https_proxy_unsupported_for_tls_bound_ws", tlsHTTPSProxy["ws_transport_unsupported_reason"])
}

func TestCoderOpenAIWSClientDialer_ProxyHTTPClientInvalidURL(t *testing.T) {
	dialer := newDefaultOpenAIWSClientDialer()
	impl, ok := dialer.(*coderOpenAIWSClientDialer)
	require.True(t, ok)

	_, err := impl.proxyHTTPClient("://bad")
	require.Error(t, err)
}

func TestCoderOpenAIWSClientDialer_TransportMetricsSnapshot(t *testing.T) {
	dialer := newDefaultOpenAIWSClientDialer()
	impl, ok := dialer.(*coderOpenAIWSClientDialer)
	require.True(t, ok)

	_, err := impl.proxyHTTPClient("http://127.0.0.1:18080")
	require.NoError(t, err)
	_, err = impl.proxyHTTPClient("http://127.0.0.1:18080")
	require.NoError(t, err)
	_, err = impl.proxyHTTPClient("http://127.0.0.1:18081")
	require.NoError(t, err)

	snapshot := impl.SnapshotTransportMetrics()
	require.Equal(t, int64(1), snapshot.ProxyClientCacheHits)
	require.Equal(t, int64(2), snapshot.ProxyClientCacheMisses)
	require.InDelta(t, 1.0/3.0, snapshot.TransportReuseRatio, 0.0001)
}

func TestCoderOpenAIWSClientDialer_ProxyClientCacheCapacity(t *testing.T) {
	dialer := newDefaultOpenAIWSClientDialer()
	impl, ok := dialer.(*coderOpenAIWSClientDialer)
	require.True(t, ok)

	total := openAIWSProxyClientCacheMaxEntries + 32
	for i := 0; i < total; i++ {
		_, err := impl.proxyHTTPClient(fmt.Sprintf("http://127.0.0.1:%d", 20000+i))
		require.NoError(t, err)
	}

	impl.proxyMu.Lock()
	cacheSize := len(impl.proxyClients)
	impl.proxyMu.Unlock()

	require.LessOrEqual(t, cacheSize, openAIWSProxyClientCacheMaxEntries, "代理客户端缓存应受容量上限约束")
}

func TestCoderOpenAIWSClientDialer_ProxyClientCacheIdleTTL(t *testing.T) {
	dialer := newDefaultOpenAIWSClientDialer()
	impl, ok := dialer.(*coderOpenAIWSClientDialer)
	require.True(t, ok)

	oldProxy := "http://127.0.0.1:28080"
	_, err := impl.proxyHTTPClient(oldProxy)
	require.NoError(t, err)

	impl.proxyMu.Lock()
	oldEntry := impl.proxyClients[oldProxy]
	require.NotNil(t, oldEntry)
	oldEntry.lastUsedUnixNano = time.Now().Add(-openAIWSProxyClientCacheIdleTTL - time.Minute).UnixNano()
	impl.proxyMu.Unlock()

	// 触发一次新的代理获取，驱动 TTL 清理。
	_, err = impl.proxyHTTPClient("http://127.0.0.1:28081")
	require.NoError(t, err)

	impl.proxyMu.Lock()
	_, exists := impl.proxyClients[oldProxy]
	impl.proxyMu.Unlock()

	require.False(t, exists, "超过空闲 TTL 的代理客户端应被回收")
}

func TestCoderOpenAIWSClientDialer_ProxyTransportTLSHandshakeTimeout(t *testing.T) {
	dialer := newDefaultOpenAIWSClientDialer()
	impl, ok := dialer.(*coderOpenAIWSClientDialer)
	require.True(t, ok)

	client, err := impl.proxyHTTPClient("http://127.0.0.1:38080")
	require.NoError(t, err)
	require.NotNil(t, client)

	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, transport)
	require.Equal(t, 10*time.Second, transport.TLSHandshakeTimeout)
}

func TestCoderOpenAIWSClientDialer_DoesNotNegotiateCompression(t *testing.T) {
	requestHeaders := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestHeaders <- r.Header.Clone()
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
		require.NoError(t, err)
		defer conn.CloseNow()
		_ = conn.Close(websocket.StatusNormalClosure, "done")
	}))
	defer server.Close()

	dialer := newDefaultOpenAIWSClientDialer()
	impl, ok := dialer.(*coderOpenAIWSClientDialer)
	require.True(t, ok)

	conn, _, _, err := impl.Dial(t.Context(), "ws"+server.URL[len("http"):], nil, "", nil)
	require.NoError(t, err)
	require.NotNil(t, conn)
	defer conn.Close()

	select {
	case headers := <-requestHeaders:
		require.Empty(t, headers.Get("Sec-WebSocket-Extensions"), "上游 WS 拨号不应协商 permessage-deflate")
	case <-time.After(3 * time.Second):
		t.Fatal("等待上游 WS 握手头超时")
	}
}

func testOpenAIWSEffectiveTLS(identity string) *OpenAIGatewayEffectiveTLS {
	return &OpenAIGatewayEffectiveTLS{
		Enabled:        true,
		ProfileName:    identity,
		ProfileHash:    identity + "-hash",
		Source:         openAIGatewayTLSSourceBucket,
		CacheIdentity:  "bucket=default|proxy=direct|profile_hash=" + identity + "|source=bucket",
		HTTPApplicable: true,
		WSApplicable:   true,
		Profile:        &tlsfingerprint.Profile{Name: identity},
	}
}

func TestShouldUseGorillaOpenAIWSDialer(t *testing.T) {
	headers := http.Header{}
	require.False(t, shouldUseGorillaOpenAIWSDialer(headers))
	headers.Set("chatgpt-account-id", "acct_123")
	require.True(t, shouldUseGorillaOpenAIWSDialer(headers))
}
