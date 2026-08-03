package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIGatewayHandler_TLSCanaryRequiresClientTokenAndReturnsHTTPDiagnostics(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.ClientTokens = []config.OpenAIGatewayClientTokenConfig{{Name: "probe", Token: "tok-123"}}
	cfg.Gateway.OpenAICore.TLSBinding.Enabled = true
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{
			Name:    "default",
			Enabled: true,
			TLS: config.OpenAIGatewayBucketTLSConfig{
				Enabled:              true,
				AllowDefaultFallback: true,
			},
		},
	}

	repo := &serviceMockAccountRepo{
		accountsByID: map[int64]*service.Account{
			1: {
				ID:       1,
				Name:     "openai-account",
				Platform: service.PlatformOpenAI,
				Type:     service.AccountTypeOAuth,
				Status:   service.StatusActive,
				Credentials: map[string]any{
					"chatgpt_account_id": "acct-1",
				},
			},
		},
	}
	core := service.NewOpenAIGatewayCoreService(repo, cfg, nil)
	h := NewOpenAIGatewayHandler(nil, core, nil, nil, nil, nil, nil, cfg)

	router := gin.New()
	router.GET("/openai/_tls_canary", h.TLSCanary)

	req := httptest.NewRequest(http.MethodGet, "/openai/_tls_canary?account_id=1&bucket=default&transport=http", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	req = httptest.NewRequest(http.MethodGet, "/openai/_tls_canary?account_id=1&bucket=default&transport=http", nil)
	req.Header.Set(service.OpenAIGatewayClientTokenHeader, "tok-123")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "\"account_id\":1")
	require.Contains(t, body, "\"transport\":\"http\"")
	require.Contains(t, body, "\"effective_send_method\":\"DoWithTLS\"")
	require.Contains(t, body, "\"cache_identity\"")
	require.Contains(t, body, "\"http_applicable\":true")
}

func TestOpenAIGatewayHandler_TLSCanaryPostAcceptsJSONContract(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.ClientTokens = []config.OpenAIGatewayClientTokenConfig{{Name: "probe", Token: "tok-123"}}
	cfg.Gateway.OpenAICore.TLSBinding.Enabled = true
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{
			Name:    "default",
			Enabled: true,
			TLS: config.OpenAIGatewayBucketTLSConfig{
				Enabled:              true,
				AllowDefaultFallback: true,
			},
		},
	}

	repo := &serviceMockAccountRepo{
		accountsByID: map[int64]*service.Account{
			1: {
				ID:       1,
				Name:     "openai-account",
				Platform: service.PlatformOpenAI,
				Type:     service.AccountTypeOAuth,
				Status:   service.StatusActive,
				Credentials: map[string]any{
					"chatgpt_account_id": "acct-1",
				},
			},
		},
	}
	core := service.NewOpenAIGatewayCoreService(repo, cfg, nil)
	h := NewOpenAIGatewayHandler(nil, core, nil, nil, nil, nil, nil, cfg)

	router := gin.New()
	router.POST("/openai/_tls/canary", h.TLSCanary)

	req := httptest.NewRequest(http.MethodPost, "/openai/_tls/canary", strings.NewReader(`{"account_id":1,"bucket":"default","transport":"http","route":"/v1/responses"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(service.OpenAIGatewayClientTokenHeader, "tok-123")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "\"account_id\":1")
	require.Contains(t, body, "\"bucket\":\"default\"")
	require.Contains(t, body, "\"transport\":\"http\"")
	require.Contains(t, body, "\"route\":\"/v1/responses\"")
	require.Contains(t, body, "\"effective_send_method\":\"DoWithTLS\"")
}

func TestOpenAIGatewayHandler_TLSCanaryPostRunsLiveHTTPProbeWhenGatewayServiceAvailable(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.ClientTokens = []config.OpenAIGatewayClientTokenConfig{{Name: "probe", Token: "tok-123"}}
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{{Name: "default", Enabled: true}}
	repo := &serviceMockAccountRepo{
		accountsByID: map[int64]*service.Account{
			1: {
				ID:          1,
				Name:        "openai-api",
				Platform:    service.PlatformOpenAI,
				Type:        service.AccountTypeAPIKey,
				Status:      service.StatusActive,
				Credentials: map[string]any{"api_key": "sk-test"},
			},
		},
	}
	core := service.NewOpenAIGatewayCoreService(repo, cfg, nil)
	upstream := &openAITLSCanaryHTTPUpstream{statusCode: http.StatusNoContent}
	gatewaySvc := service.NewOpenAIGatewayService(
		repo, nil, nil, nil, nil, nil, nil, cfg,
		nil, nil, nil, nil, nil, upstream, nil, nil,
		core,
	)
	h := NewOpenAIGatewayHandler(gatewaySvc, core, nil, nil, nil, nil, nil, cfg)

	router := gin.New()
	router.POST("/openai/_tls/canary", h.TLSCanary)

	req := httptest.NewRequest(http.MethodPost, "/openai/_tls/canary", strings.NewReader(`{"account_id":1,"bucket":"default","transport":"http","route":"/v1/responses"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(service.OpenAIGatewayClientTokenHeader, "tok-123")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int32(1), upstream.calls.Load(), "POST canary should execute a live sender probe")
	body := rec.Body.String()
	require.Contains(t, body, `"success":true`)
	require.Contains(t, body, `"probe"`)
	require.Contains(t, body, `"transport":"http"`)
	require.Contains(t, body, `"route":"/v1/responses"`)
	require.Contains(t, body, `"http_status":204`)
	require.Contains(t, body, `"handshake_ms"`)
}

func TestOpenAIGatewayHandler_TLSCanaryPostRunsLiveWSProbeWhenGatewayServiceAvailable(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionDisabled})
		require.NoError(t, err)
		defer conn.CloseNow()
		_ = conn.Close(coderws.StatusNormalClosure, "canary-ok")
	}))
	defer upstream.Close()

	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.ClientTokens = []config.OpenAIGatewayClientTokenConfig{{Name: "probe", Token: "tok-123"}}
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{{Name: "default", Enabled: true}}
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 2
	repo := &serviceMockAccountRepo{
		accountsByID: map[int64]*service.Account{
			1: {
				ID:       1,
				Name:     "openai-api",
				Platform: service.PlatformOpenAI,
				Type:     service.AccountTypeAPIKey,
				Status:   service.StatusActive,
				Credentials: map[string]any{
					"api_key":  "sk-test",
					"base_url": upstream.URL,
				},
			},
		},
	}
	core := service.NewOpenAIGatewayCoreService(repo, cfg, nil)
	gatewaySvc := service.NewOpenAIGatewayService(
		repo, nil, nil, nil, nil, nil, nil, cfg,
		nil, nil, nil, nil, nil, nil, nil, nil,
		core,
	)
	h := NewOpenAIGatewayHandler(gatewaySvc, core, nil, nil, nil, nil, nil, cfg)

	router := gin.New()
	router.POST("/openai/_tls/canary", h.TLSCanary)

	req := httptest.NewRequest(http.MethodPost, "/openai/_tls/canary", strings.NewReader(`{"account_id":1,"bucket":"default","transport":"ws","route":"/v1/responses"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(service.OpenAIGatewayClientTokenHeader, "tok-123")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int32(1), upstreamHits.Load(), "POST WS canary should perform a live upstream handshake")
	body := rec.Body.String()
	require.Contains(t, body, `"success":true`)
	require.Contains(t, body, `"probe"`)
	require.Contains(t, body, `"transport":"ws"`)
	require.Contains(t, body, `"route":"/v1/responses"`)
	require.Contains(t, body, `"handshake_ms"`)
	require.Contains(t, body, `"ws_dialer_strategy":"coder_default"`)
}

func TestOpenAIGatewayHandler_TLSCanaryPostRejectsUnsupportedHTTPRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.ClientTokens = []config.OpenAIGatewayClientTokenConfig{{Name: "probe", Token: "tok-123"}}
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{{Name: "default", Enabled: true}}
	core := service.NewOpenAIGatewayCoreService(&serviceMockAccountRepo{}, cfg, nil)
	h := NewOpenAIGatewayHandler(nil, core, nil, nil, nil, nil, nil, cfg)

	router := gin.New()
	router.POST("/openai/_tls/canary", h.TLSCanary)

	req := httptest.NewRequest(http.MethodPost, "/openai/_tls/canary", strings.NewReader(`{"account_id":1,"bucket":"default","transport":"http","route":"/v1/audio/transcriptions"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(service.OpenAIGatewayClientTokenHeader, "tok-123")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "unsupported route")
}

func TestOpenAIGatewayHandler_TLSCanaryPostUnsupportedLiveRouteReturnsProbeFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.ClientTokens = []config.OpenAIGatewayClientTokenConfig{{Name: "probe", Token: "tok-123"}}
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{{Name: "default", Enabled: true}}
	repo := &serviceMockAccountRepo{
		accountsByID: map[int64]*service.Account{
			1: {
				ID:          1,
				Name:        "openai-api",
				Platform:    service.PlatformOpenAI,
				Type:        service.AccountTypeAPIKey,
				Status:      service.StatusActive,
				Credentials: map[string]any{"api_key": "sk-test"},
			},
		},
	}
	core := service.NewOpenAIGatewayCoreService(repo, cfg, nil)
	gatewaySvc := service.NewOpenAIGatewayService(
		repo, nil, nil, nil, nil, nil, nil, cfg,
		nil, nil, nil, nil, nil, &openAITLSCanaryHTTPUpstream{statusCode: http.StatusOK}, nil, nil,
		core,
	)
	h := NewOpenAIGatewayHandler(gatewaySvc, core, nil, nil, nil, nil, nil, cfg)

	router := gin.New()
	router.POST("/openai/_tls/canary", h.TLSCanary)

	req := httptest.NewRequest(http.MethodPost, "/openai/_tls/canary", strings.NewReader(`{"account_id":1,"bucket":"default","transport":"http","route":"/v1/audio/transcriptions"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(service.OpenAIGatewayClientTokenHeader, "tok-123")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, `"success":false`)
	require.Contains(t, body, `"failure_reason":"unsupported_route"`)
	require.Contains(t, body, `"route":"/v1/audio/transcriptions"`)
}

func TestOpenAIGatewayHandler_TLSCanaryAcceptsWSTransport(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.ClientTokens = []config.OpenAIGatewayClientTokenConfig{{Name: "probe", Token: "tok-123"}}
	cfg.Gateway.OpenAICore.TLSBinding.Enabled = true
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{
			Name:    "default",
			Enabled: true,
			TLS: config.OpenAIGatewayBucketTLSConfig{
				Enabled:              true,
				AllowDefaultFallback: true,
			},
		},
	}
	repo := &serviceMockAccountRepo{
		accountsByID: map[int64]*service.Account{
			1: {
				ID:       1,
				Name:     "openai-account",
				Platform: service.PlatformOpenAI,
				Type:     service.AccountTypeOAuth,
				Status:   service.StatusActive,
				Credentials: map[string]any{
					"chatgpt_account_id": "acct-1",
				},
			},
		},
	}
	core := service.NewOpenAIGatewayCoreService(repo, cfg, nil)
	h := NewOpenAIGatewayHandler(nil, core, nil, nil, nil, nil, nil, cfg)

	router := gin.New()
	router.POST("/openai/_tls/canary", h.TLSCanary)

	req := httptest.NewRequest(http.MethodPost, "/openai/_tls/canary", strings.NewReader(`{"account_id":1,"bucket":"default","transport":"ws","route":"/v1/realtime"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(service.OpenAIGatewayClientTokenHeader, "tok-123")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "\"transport\":\"ws\"")
	require.Contains(t, body, "\"effective_send_method\":\"WSCoderCustomHTTPClient\"")
	require.Contains(t, body, "\"ws_dialer_strategy\":\"coder_custom_http_client\"")
	require.Contains(t, body, "\"ws_transport_supported\":\"true\"")
}

type openAITLSCanaryHTTPUpstream struct {
	calls      atomic.Int32
	statusCode int
}

func (u *openAITLSCanaryHTTPUpstream) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	u.calls.Add(1)
	return u.response(), nil
}

func (u *openAITLSCanaryHTTPUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	u.calls.Add(1)
	return u.response(), nil
}

func (u *openAITLSCanaryHTTPUpstream) response() *http.Response {
	statusCode := u.statusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	return &http.Response{
		StatusCode: statusCode,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{}`)),
	}
}
