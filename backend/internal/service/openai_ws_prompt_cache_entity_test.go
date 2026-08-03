package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type openAIWSSequenceCaptureDialer struct {
	mu        sync.Mutex
	conns     []*openAIWSCaptureConn
	lastHeads []http.Header
}

func (d *openAIWSSequenceCaptureDialer) Dial(
	ctx context.Context,
	wsURL string,
	headers http.Header,
	proxyURL string,
	effectiveTLS *OpenAIGatewayEffectiveTLS,
) (openAIWSClientConn, int, http.Header, error) {
	_ = ctx
	_ = wsURL
	_ = proxyURL
	_ = effectiveTLS
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.conns) == 0 {
		return nil, 0, nil, errors.New("no capture websocket conn queued")
	}
	conn := d.conns[0]
	d.conns = d.conns[1:]
	d.lastHeads = append(d.lastHeads, cloneHeader(headers))
	return conn, 0, nil, nil
}

func newOpenAIWSPromptCacheEntityTestConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 2
	cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
	cfg.Gateway.OpenAIWS.QueueLimitPerConn = 8
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
	return cfg
}

func TestOpenAIGatewayService_ProxyResponsesWebSocketFromClient_CtxPoolScopesPromptCacheKeyByEntity(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := newOpenAIWSPromptCacheEntityTestConfig()
	cfg.Gateway.OpenAIWS.IngressModeDefault = OpenAIWSIngressModeCtxPool

	alphaConn := &openAIWSCaptureConn{events: [][]byte{
		[]byte(`{"type":"response.completed","response":{"id":"resp_ctx_alpha","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`),
	}}
	betaConn := &openAIWSCaptureConn{events: [][]byte{
		[]byte(`{"type":"response.completed","response":{"id":"resp_ctx_beta","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`),
	}}
	captureDialer := &openAIWSSequenceCaptureDialer{conns: []*openAIWSCaptureConn{alphaConn, betaConn}}
	pool := newOpenAIWSConnPool(cfg)
	pool.setClientDialerForTest(captureDialer)

	svc := &OpenAIGatewayService{
		cfg:              cfg,
		httpUpstream:     &httpUpstreamRecorder{},
		cache:            &stubGatewayCache{},
		openaiWSResolver: NewOpenAIWSProtocolResolver(cfg),
		toolCorrector:    NewCodexToolCorrector(),
		openaiWSPool:     pool,
	}

	account := &Account{
		ID:          931,
		Name:        "openai-ws-ctx-prompt-cache-entity",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 2,
		Credentials: map[string]any{"api_key": "sk-test"},
		Extra:       map[string]any{"openai_apikey_responses_websockets_v2_mode": OpenAIWSIngressModeCtxPool},
	}

	serverErrCh := make(chan error, 2)
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
		if err != nil {
			serverErrCh <- err
			return
		}
		defer func() { _ = conn.CloseNow() }()

		entityKey := strings.TrimSpace(r.URL.Query().Get("entity"))
		rec := httptest.NewRecorder()
		ginCtx, _ := gin.CreateTestContext(rec)
		req := r.Clone(r.Context())
		req.Header = req.Header.Clone()
		req.Header.Set("User-Agent", "unit-test-agent/1.0")
		req = req.WithContext(WithResolvedEntity(req.Context(), &ResolvedEntity{
			Entity: Entity{ID: 10, EntityKey: entityKey, Status: EntityStatusActive},
			Source: EntityResolutionSourceClaimedBinding,
		}))
		ginCtx.Request = req

		readCtx, cancelRead := context.WithTimeout(r.Context(), 3*time.Second)
		msgType, firstMessage, readErr := conn.Read(readCtx)
		cancelRead()
		if readErr != nil {
			serverErrCh <- readErr
			return
		}
		if msgType != coderws.MessageText && msgType != coderws.MessageBinary {
			serverErrCh <- fmt.Errorf("unsupported websocket client message type: %s", msgType)
			return
		}
		serverErrCh <- svc.ProxyResponsesWebSocketFromClient(r.Context(), ginCtx, conn, account, "sk-test", firstMessage, nil)
	}))
	defer wsServer.Close()

	runClient := func(entityKey, expectedResponseID string) {
		dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
		clientConn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(wsServer.URL, "http")+"?entity="+entityKey, nil)
		cancelDial()
		require.NoError(t, err)
		defer func() { _ = clientConn.CloseNow() }()

		writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
		err = clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","stream":false,"store":false,"prompt_cache_key":"shared-ws-cache","input":[{"type":"input_text","text":"hello"}]}`))
		cancelWrite()
		require.NoError(t, err)

		readCtx, cancelRead := context.WithTimeout(context.Background(), 3*time.Second)
		_, event, readErr := clientConn.Read(readCtx)
		cancelRead()
		require.NoError(t, readErr)
		require.Equal(t, expectedResponseID, gjson.GetBytes(event, "response.id").String())
		require.NoError(t, clientConn.Close(coderws.StatusNormalClosure, "done"))

		select {
		case serverErr := <-serverErrCh:
			if serverErr != nil {
				require.Contains(t, serverErr.Error(), "StatusNormalClosure")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("waiting for ctx-pool websocket server shutdown timed out")
		}
	}

	runClient("team-alpha", "resp_ctx_alpha")
	runClient("team-beta", "resp_ctx_beta")

	var seenAlpha, seenBeta bool
	for _, write := range append(alphaConn.writes, betaConn.writes...) {
		key := write["prompt_cache_key"]
		require.NotEqual(t, "shared-ws-cache", key, "upstream WS frames must not receive the raw cross-entity prompt_cache_key")
		switch key {
		case EntityScopedSeed("team-alpha", "shared-ws-cache"):
			seenAlpha = true
		case EntityScopedSeed("team-beta", "shared-ws-cache"):
			seenBeta = true
		}
	}
	require.True(t, seenAlpha, "team-alpha scoped prompt_cache_key must be sent upstream")
	require.True(t, seenBeta, "team-beta scoped prompt_cache_key must be sent upstream")
}

func TestOpenAIGatewayService_ProxyResponsesWebSocketFromClient_PassthroughScopesPromptCacheKeyByEntityOnFirstAndRelayFrames(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := newOpenAIWSPromptCacheEntityTestConfig()
	cfg.Gateway.OpenAIWS.IngressModeDefault = OpenAIWSIngressModePassthrough

	alphaConn := &openAIWSCaptureConn{
		readDelays: []time.Duration{0, 200 * time.Millisecond},
		events: [][]byte{
			[]byte(`{"type":"response.completed","response":{"id":"resp_pass_alpha_1","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`),
			[]byte(`{"type":"response.completed","response":{"id":"resp_pass_alpha_2","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`),
		},
	}
	betaConn := &openAIWSCaptureConn{
		readDelays: []time.Duration{0, 200 * time.Millisecond},
		events: [][]byte{
			[]byte(`{"type":"response.completed","response":{"id":"resp_pass_beta_1","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`),
			[]byte(`{"type":"response.completed","response":{"id":"resp_pass_beta_2","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`),
		},
	}
	captureDialer := &openAIWSSequenceCaptureDialer{conns: []*openAIWSCaptureConn{alphaConn, betaConn}}

	svc := &OpenAIGatewayService{
		cfg:                       cfg,
		httpUpstream:              &httpUpstreamRecorder{},
		cache:                     &stubGatewayCache{},
		openaiWSResolver:          NewOpenAIWSProtocolResolver(cfg),
		toolCorrector:             NewCodexToolCorrector(),
		openaiWSPassthroughDialer: captureDialer,
	}

	account := &Account{
		ID:          932,
		Name:        "openai-ws-passthrough-prompt-cache-entity",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 2,
		Credentials: map[string]any{"api_key": "sk-test"},
		Extra:       map[string]any{"openai_apikey_responses_websockets_v2_mode": OpenAIWSIngressModePassthrough},
	}

	serverErrCh := make(chan error, 2)
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
		if err != nil {
			serverErrCh <- err
			return
		}
		defer func() { _ = conn.CloseNow() }()

		entityKey := strings.TrimSpace(r.URL.Query().Get("entity"))
		rec := httptest.NewRecorder()
		ginCtx, _ := gin.CreateTestContext(rec)
		req := r.Clone(r.Context())
		req.Header = req.Header.Clone()
		req.Header.Set("User-Agent", "unit-test-agent/1.0")
		req = req.WithContext(WithResolvedEntity(req.Context(), &ResolvedEntity{
			Entity: Entity{ID: 20, EntityKey: entityKey, Status: EntityStatusActive},
			Source: EntityResolutionSourceClaimedBinding,
		}))
		ginCtx.Request = req

		readCtx, cancelRead := context.WithTimeout(r.Context(), 3*time.Second)
		msgType, firstMessage, readErr := conn.Read(readCtx)
		cancelRead()
		if readErr != nil {
			serverErrCh <- readErr
			return
		}
		if msgType != coderws.MessageText && msgType != coderws.MessageBinary {
			serverErrCh <- fmt.Errorf("unsupported websocket client message type: %s", msgType)
			return
		}
		serverErrCh <- svc.ProxyResponsesWebSocketFromClient(r.Context(), ginCtx, conn, account, "sk-test", firstMessage, nil)
	}))
	defer wsServer.Close()

	runClient := func(entityKey string, responseIDs ...string) {
		dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
		clientConn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(wsServer.URL, "http")+"?entity="+entityKey, nil)
		cancelDial()
		require.NoError(t, err)
		defer func() { _ = clientConn.CloseNow() }()

		writeMessage := func(payload string) {
			writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancelWrite()
			require.NoError(t, clientConn.Write(writeCtx, coderws.MessageText, []byte(payload)))
		}
		readResponseID := func() string {
			readCtx, cancelRead := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancelRead()
			_, event, readErr := clientConn.Read(readCtx)
			require.NoError(t, readErr)
			return gjson.GetBytes(event, "response.id").String()
		}

		writeMessage(`{"type":"response.create","model":"gpt-5.1","stream":false,"prompt_cache_key":"shared-ws-cache","input":[{"type":"input_text","text":"first"}]}`)
		require.Equal(t, responseIDs[0], readResponseID())
		writeMessage(`{"type":"response.create","model":"gpt-5.1","stream":false,"prompt_cache_key":"shared-ws-cache","input":[{"type":"input_text","text":"second"}]}`)
		require.Equal(t, responseIDs[1], readResponseID())
		_ = clientConn.Close(coderws.StatusNormalClosure, "done")

		select {
		case serverErr := <-serverErrCh:
			if serverErr != nil {
				require.Contains(t, serverErr.Error(), "StatusNormalClosure")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("waiting for passthrough websocket server shutdown timed out")
		}
	}

	runClient("team-alpha", "resp_pass_alpha_1", "resp_pass_alpha_2")
	runClient("team-beta", "resp_pass_beta_1", "resp_pass_beta_2")

	require.Len(t, alphaConn.writes, 2)
	require.Len(t, betaConn.writes, 2)
	for _, write := range alphaConn.writes {
		require.Equal(t, EntityScopedSeed("team-alpha", "shared-ws-cache"), write["prompt_cache_key"])
	}
	for _, write := range betaConn.writes {
		require.Equal(t, EntityScopedSeed("team-beta", "shared-ws-cache"), write["prompt_cache_key"])
	}
	require.NotEqual(t, alphaConn.writes[0]["prompt_cache_key"], betaConn.writes[0]["prompt_cache_key"])
}
