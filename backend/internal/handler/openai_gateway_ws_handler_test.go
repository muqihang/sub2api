package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type wsHandlerAccountRepo struct {
	service.AccountRepository
	accounts   []service.Account
	listCalls  atomic.Int32
	getByIDHit atomic.Int32
}

func (r *wsHandlerAccountRepo) GetByID(ctx context.Context, id int64) (*service.Account, error) {
	r.getByIDHit.Add(1)
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			return &r.accounts[i], nil
		}
	}
	return nil, fmt.Errorf("account %d not found", id)
}

func (r *wsHandlerAccountRepo) ListSchedulableByGroupIDAndPlatform(ctx context.Context, groupID int64, platform string) ([]service.Account, error) {
	r.listCalls.Add(1)
	return r.listByPlatform(platform), nil
}

func (r *wsHandlerAccountRepo) ListSchedulableByPlatform(ctx context.Context, platform string) ([]service.Account, error) {
	r.listCalls.Add(1)
	return r.listByPlatform(platform), nil
}

func (r *wsHandlerAccountRepo) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]service.Account, error) {
	r.listCalls.Add(1)
	return r.listByPlatform(platform), nil
}

func (r *wsHandlerAccountRepo) listByPlatform(platform string) []service.Account {
	result := make([]service.Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account.Platform == platform {
			result = append(result, account)
		}
	}
	return result
}

type wsHandlerGatewayCache struct {
	service.GatewayCache
	sessionBindings map[string]int64
}

func (c *wsHandlerGatewayCache) GetSessionAccountID(ctx context.Context, groupID int64, sessionHash string) (int64, error) {
	if c.sessionBindings == nil {
		return 0, fmt.Errorf("not found")
	}
	if accountID, ok := c.sessionBindings[c.key(groupID, sessionHash)]; ok {
		return accountID, nil
	}
	return 0, fmt.Errorf("not found")
}

func (c *wsHandlerGatewayCache) SetSessionAccountID(ctx context.Context, groupID int64, sessionHash string, accountID int64, ttl time.Duration) error {
	if c.sessionBindings == nil {
		c.sessionBindings = make(map[string]int64)
	}
	c.sessionBindings[c.key(groupID, sessionHash)] = accountID
	return nil
}

func (c *wsHandlerGatewayCache) RefreshSessionTTL(ctx context.Context, groupID int64, sessionHash string, ttl time.Duration) error {
	return nil
}

func (c *wsHandlerGatewayCache) DeleteSessionAccountID(ctx context.Context, groupID int64, sessionHash string) error {
	if c.sessionBindings != nil {
		delete(c.sessionBindings, c.key(groupID, sessionHash))
	}
	return nil
}

func (c *wsHandlerGatewayCache) key(groupID int64, sessionHash string) string {
	return fmt.Sprintf("%d:%s", groupID, strings.TrimSpace(sessionHash))
}

func TestOpenAIGatewayWSHandler_ProxyRelayWithFakeUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstreamReceived := make(chan []byte, 1)
	upstreamHeaders := make(chan http.Header, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHeaders <- r.Header.Clone()
		upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		defer conn.Close()

		_, payload, err := conn.ReadMessage()
		require.NoError(t, err)
		upstreamReceived <- append([]byte(nil), payload...)

		err = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"resp_relay_ok","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`))
		require.NoError(t, err)
	}))
	defer upstream.Close()

	accounts := []service.Account{
		{
			ID:          801,
			Name:        "openai-apikey-primary",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeAPIKey,
			Status:      service.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			Credentials: map[string]any{
				"api_key":  "sk-upstream-primary",
				"base_url": upstream.URL,
			},
			Extra: map[string]any{
				"openai_apikey_responses_websockets_v2_enabled": true,
			},
		},
	}

	h, _, cleanup := newOpenAIWSIntegrationHandler(t, accounts)
	defer cleanup()

	sub2apiServer := newOpenAIWSIntegrationServer(t, h)
	defer sub2apiServer.Close()

	clientConn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(sub2apiServer.URL, "http")+"/v1/responses", http.Header{
		"session_id": []string{"sess-relay-001"},
	})
	require.NoError(t, err)
	defer clientConn.Close()

	err = clientConn.WriteJSON(map[string]any{
		"type":   "response.create",
		"model":  "gpt-5.1",
		"stream": false,
		"input":  []any{map[string]any{"type": "input_text", "text": "hello"}},
	})
	require.NoError(t, err)

	_, clientMessage, err := clientConn.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "response.completed", gjson.GetBytes(clientMessage, "type").String())
	require.Equal(t, "resp_relay_ok", gjson.GetBytes(clientMessage, "response.id").String())

	select {
	case upstreamPayload := <-upstreamReceived:
		require.Equal(t, "response.create", gjson.GetBytes(upstreamPayload, "type").String())
		require.Equal(t, "gpt-5.1", gjson.GetBytes(upstreamPayload, "model").String())
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting upstream payload")
	}

	select {
	case headers := <-upstreamHeaders:
		require.Equal(t, "Bearer sk-upstream-primary", headers.Get("Authorization"))
		openAIBeta := headers.Get("OpenAI-Beta")
		require.Contains(t, openAIBeta, "responses_websockets=2026-02-06")
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting upstream headers")
	}
}

func TestOpenAIGatewayWSHandler_RetriesAnotherAccountWhenFirstUpstreamDialFails(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstreamReceived := make(chan []byte, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		defer conn.Close()

		_, payload, err := conn.ReadMessage()
		require.NoError(t, err)
		upstreamReceived <- append([]byte(nil), payload...)

		err = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"resp_retry_ok","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`))
		require.NoError(t, err)
	}))
	defer upstream.Close()

	accounts := []service.Account{
		{
			ID:          901,
			Name:        "openai-apikey-broken",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeAPIKey,
			Status:      service.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			Credentials: map[string]any{
				"api_key":  "sk-upstream-broken",
				"base_url": "http://127.0.0.1:1",
			},
			Extra: map[string]any{
				"openai_apikey_responses_websockets_v2_enabled": true,
			},
		},
		{
			ID:          902,
			Name:        "openai-apikey-backup",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeAPIKey,
			Status:      service.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    1,
			Credentials: map[string]any{
				"api_key":  "sk-upstream-backup",
				"base_url": upstream.URL,
			},
			Extra: map[string]any{
				"openai_apikey_responses_websockets_v2_enabled": true,
			},
		},
	}

	h, accountRepo, cleanup := newOpenAIWSIntegrationHandler(t, accounts)
	defer cleanup()

	sub2apiServer := newOpenAIWSIntegrationServer(t, h)
	defer sub2apiServer.Close()

	clientConn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(sub2apiServer.URL, "http")+"/v1/responses", nil)
	require.NoError(t, err)
	defer clientConn.Close()

	err = clientConn.WriteJSON(map[string]any{
		"type":   "response.create",
		"model":  "gpt-5.1",
		"stream": false,
		"input":  []any{map[string]any{"type": "input_text", "text": "hello retry"}},
	})
	require.NoError(t, err)

	_, clientMessage, err := clientConn.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "response.completed", gjson.GetBytes(clientMessage, "type").String())
	require.Equal(t, "resp_retry_ok", gjson.GetBytes(clientMessage, "response.id").String())

	select {
	case upstreamPayload := <-upstreamReceived:
		require.Equal(t, "response.create", gjson.GetBytes(upstreamPayload, "type").String())
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting upstream payload from backup account")
	}

	require.GreaterOrEqual(t, accountRepo.listCalls.Load(), int32(2), "should re-select account after first upstream dial failure")
}

func TestOpenAIGatewayWSHandler_RetriesAnotherAccountWhenFirstUpstreamPreludeThen1011(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstreamReceived := make(chan []byte, 1)
	brokenUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		defer conn.Close()

		_, _, err = conn.ReadMessage()
		require.NoError(t, err)
		require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"codex.rate_limits","rate_limits":{}}`)))
		require.NoError(t, conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, ""), time.Now().Add(time.Second)))
	}))
	defer brokenUpstream.Close()

	goodUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		defer conn.Close()

		_, payload, err := conn.ReadMessage()
		require.NoError(t, err)
		upstreamReceived <- append([]byte(nil), payload...)
		require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"resp_retry_after_prelude","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`)))
	}))
	defer goodUpstream.Close()

	accounts := []service.Account{
		{
			ID:          931,
			Name:        "openai-apikey-prelude-broken",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeAPIKey,
			Status:      service.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			Credentials: map[string]any{"api_key": "sk-upstream-broken", "base_url": brokenUpstream.URL},
			Extra:       map[string]any{"openai_apikey_responses_websockets_v2_enabled": true},
		},
		{
			ID:          932,
			Name:        "openai-apikey-prelude-backup",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeAPIKey,
			Status:      service.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    1,
			Credentials: map[string]any{"api_key": "sk-upstream-backup", "base_url": goodUpstream.URL},
			Extra:       map[string]any{"openai_apikey_responses_websockets_v2_enabled": true},
		},
	}

	h, accountRepo, cleanup := newOpenAIWSIntegrationHandler(t, accounts)
	defer cleanup()

	sub2apiServer := newOpenAIWSIntegrationServer(t, h)
	defer sub2apiServer.Close()

	clientConn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(sub2apiServer.URL, "http")+"/v1/responses", nil)
	require.NoError(t, err)
	defer clientConn.Close()

	err = clientConn.WriteJSON(map[string]any{
		"type":   "response.create",
		"model":  "gpt-5.1",
		"stream": false,
		"input":  []any{map[string]any{"type": "input_text", "text": "hello retry prelude"}},
	})
	require.NoError(t, err)

	_, clientMessage, err := clientConn.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "response.completed", gjson.GetBytes(clientMessage, "type").String())
	require.Equal(t, "resp_retry_after_prelude", gjson.GetBytes(clientMessage, "response.id").String())

	select {
	case upstreamPayload := <-upstreamReceived:
		require.Equal(t, "response.create", gjson.GetBytes(upstreamPayload, "type").String())
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting upstream payload from backup account after prelude failure")
	}

	require.GreaterOrEqual(t, accountRepo.listCalls.Load(), int32(2), "should re-select account after first upstream prelude close failure")
}

func newOpenAIWSIntegrationHandler(t *testing.T, accounts []service.Account) (*OpenAIGatewayHandler, *wsHandlerAccountRepo, func()) {
	t.Helper()

	cfg := &config.Config{}
	cfg.RunMode = config.RunModeSimple
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = false
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
	cfg.Gateway.OpenAIWS.QueueLimitPerConn = 8
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 1
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
	// Keep account selection deterministic in integration tests so retry scenarios
	// always pick the highest-priority account first before switching.
	cfg.Gateway.OpenAIWS.LBTopK = 1
	cfg.Gateway.OpenAIWS.StickySessionTTLSeconds = 3600

	accountRepo := &wsHandlerAccountRepo{accounts: accounts}
	cache := &wsHandlerGatewayCache{sessionBindings: make(map[string]int64)}

	concurrencyCache := &concurrencyCacheMock{
		acquireUserSlotFn: func(ctx context.Context, userID int64, maxConcurrency int, requestID string) (bool, error) {
			return true, nil
		},
		acquireAccountSlotFn: func(ctx context.Context, accountID int64, maxConcurrency int, requestID string) (bool, error) {
			return true, nil
		},
	}
	concurrencyService := service.NewConcurrencyService(concurrencyCache)
	billingCacheService := service.NewBillingCacheService(nil, nil, nil, nil, cfg)

	gatewayService := service.NewOpenAIGatewayService(
		accountRepo,
		nil,
		nil,
		nil,
		nil,
		nil,
		cache,
		cfg,
		nil,
		concurrencyService,
		nil,
		nil,
		billingCacheService,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	h := &OpenAIGatewayHandler{
		gatewayService:        gatewayService,
		billingCacheService:   billingCacheService,
		apiKeyService:         &service.APIKeyService{},
		usageRecordWorkerPool: &service.UsageRecordWorkerPool{},
		concurrencyHelper:     NewConcurrencyHelper(concurrencyService, SSEPingFormatNone, time.Second),
		maxAccountSwitches:    3,
	}

	cleanup := func() {
		billingCacheService.Stop()
		gatewayService.CloseOpenAIWSPool()
	}

	return h, accountRepo, cleanup
}

func newOpenAIWSIntegrationServer(t *testing.T, h *OpenAIGatewayHandler) *httptest.Server {
	t.Helper()

	apiKey := &service.APIKey{
		ID:   1101,
		User: &service.User{ID: 7001},
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), apiKey)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7001, Concurrency: 2})
		c.Next()
	})
	router.GET("/v1/responses", h.ResponsesWebSocket)
	return httptest.NewServer(router)
}
