package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

type openaiOAuthClientEgressStub struct {
	exchangeCalled int32
	lastProxyURL   string
}

func (s *openaiOAuthClientEgressStub) ExchangeCode(ctx context.Context, code, codeVerifier, redirectURI, proxyURL, clientID string) (*openai.TokenResponse, error) {
	atomic.AddInt32(&s.exchangeCalled, 1)
	s.lastProxyURL = proxyURL
	return &openai.TokenResponse{
		AccessToken:  "at",
		RefreshToken: "rt",
		ExpiresIn:    3600,
	}, nil
}

func (s *openaiOAuthClientEgressStub) RefreshToken(ctx context.Context, refreshToken, proxyURL string) (*openai.TokenResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *openaiOAuthClientEgressStub) RefreshTokenWithClientID(ctx context.Context, refreshToken, proxyURL string, clientID string) (*openai.TokenResponse, error) {
	return nil, errors.New("not implemented")
}

type openaiOAuthProxyRepoEgressStub struct {
	ProxyRepository
	proxies map[int64]*Proxy
}

func (r *openaiOAuthProxyRepoEgressStub) GetByID(ctx context.Context, id int64) (*Proxy, error) {
	if proxy, ok := r.proxies[id]; ok {
		return proxy, nil
	}
	return nil, errors.New("proxy not found")
}

func testOpenAIOAuthEgressConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.EgressFailClosed = true
	cfg.Gateway.OpenAICore.AllowAccountProxyFallback = false
	cfg.Gateway.OpenAICore.AllowDirectFallback = false
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "bucket-a"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{Name: "bucket-a", Enabled: true, ProxyURL: "socks5://user:pass@127.0.0.1:9001"},
	}
	return cfg
}

func TestOpenAIOAuthService_GenerateAuthURLStoresEgressMetadata(t *testing.T) {
	client := &openaiOAuthClientEgressStub{}
	svc := NewOpenAIOAuthService(nil, client)
	defer svc.Stop()
	svc.SetGatewayCoreService(NewOpenAIGatewayCoreService(nil, testOpenAIOAuthEgressConfig(), nil))

	result, err := svc.GenerateAuthURLWithEgress(context.Background(), nil, "", PlatformOpenAI, "")
	require.NoError(t, err)
	require.NotEmpty(t, result.SessionID)
	require.Equal(t, "bucket-a", result.EgressBucket)
	require.True(t, result.ProxySelected)
	require.Equal(t, "socks5h://127.0.0.1:9001", result.ProxyLabel)
	require.NotEmpty(t, result.ProxyHash)
	require.NotContains(t, result.ProxyLabel, "user")
	require.NotContains(t, result.ProxyLabel, "pass")

	session, ok := svc.sessionStore.Get(result.SessionID)
	require.True(t, ok)
	require.Equal(t, "bucket-a", session.EgressBucket)
	require.True(t, session.ProxySelected)
	require.Equal(t, result.ProxyLabel, session.ProxyLabel)
	require.Equal(t, result.ProxyHash, session.ProxyHash)
	require.Equal(t, "socks5://user:pass@127.0.0.1:9001", session.ProxyURL)
}

func TestOpenAIOAuthService_ExchangeCodeRejectsMissingEgressBucketBeforeTokenExchange(t *testing.T) {
	client := &openaiOAuthClientEgressStub{}
	cfg := testOpenAIOAuthEgressConfig()
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "missing"
	svc := NewOpenAIOAuthService(nil, client)
	defer svc.Stop()
	svc.SetGatewayCoreService(NewOpenAIGatewayCoreService(nil, cfg, nil))
	svc.sessionStore.Set("sid", &openai.OAuthSession{
		State:        "expected-state",
		CodeVerifier: "verifier",
		RedirectURI:  openai.DefaultRedirectURI,
		EgressBucket: "missing",
		CreatedAt:    time.Now(),
	})

	_, err := svc.ExchangeCode(context.Background(), &OpenAIExchangeCodeInput{
		SessionID: "sid",
		Code:      "auth-code",
		State:     "expected-state",
	})

	require.Error(t, err)
	var policyErr *OpenAIEgressPolicyError
	require.ErrorAs(t, err, &policyErr)
	require.Equal(t, "missing_bucket", policyErr.Code)
	require.Equal(t, int32(0), atomic.LoadInt32(&client.exchangeCalled))
}

func TestOpenAIOAuthService_ExchangeCodeUsesSessionEgressBucket(t *testing.T) {
	client := &openaiOAuthClientEgressStub{}
	svc := NewOpenAIOAuthService(nil, client)
	defer svc.Stop()
	svc.SetGatewayCoreService(NewOpenAIGatewayCoreService(nil, testOpenAIOAuthEgressConfig(), nil))
	svc.sessionStore.Set("sid", &openai.OAuthSession{
		State:        "expected-state",
		CodeVerifier: "verifier",
		RedirectURI:  openai.DefaultRedirectURI,
		EgressBucket: "bucket-a",
		CreatedAt:    time.Now(),
	})

	info, err := svc.ExchangeCode(context.Background(), &OpenAIExchangeCodeInput{
		SessionID: "sid",
		Code:      "auth-code",
		State:     "expected-state",
	})

	require.NoError(t, err)
	require.Equal(t, "socks5://user:pass@127.0.0.1:9001", client.lastProxyURL)
	require.Equal(t, "bucket-a", info.EgressBucket)
	require.True(t, info.ProxySelected)
	require.Equal(t, "socks5h://127.0.0.1:9001", info.ProxyLabel)
	require.NotEmpty(t, info.ProxyHash)
}
