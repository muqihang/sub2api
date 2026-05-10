package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type openAIHTTPSenderUpstreamRecorder struct {
	plainCalls  int
	tlsCalls    int
	lastProfile *tlsfingerprint.Profile
	resp        *http.Response
}

func (u *openAIHTTPSenderUpstreamRecorder) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	u.plainCalls++
	return cloneOpenAIHTTPSenderResponse(u.resp), nil
}

func (u *openAIHTTPSenderUpstreamRecorder) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	u.tlsCalls++
	u.lastProfile = profile
	return cloneOpenAIHTTPSenderResponse(u.resp), nil
}

func cloneOpenAIHTTPSenderResponse(resp *http.Response) *http.Response {
	if resp == nil {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(`{}`)),
		}
	}
	out := *resp
	if out.Body == nil {
		out.Body = io.NopCloser(strings.NewReader(`{}`))
	}
	return &out
}

func newOpenAIHTTPSenderGinContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(`{}`))
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
	return c
}

func newOpenAIHTTPSenderRequest(t *testing.T) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://api.openai.com/v1/responses", strings.NewReader(`{}`))
	require.NoError(t, err)
	return req
}

func TestOpenAIGatewayService_SendOpenAIHTTPRequestUsesTLSAwareUpstreamWhenEffectiveTLSEnabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.TLSBinding.Enabled = true
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{
			Name:    "default",
			Enabled: true,
			TLS: config.OpenAIGatewayBucketTLSConfig{
				Enabled:   true,
				ProfileID: 7,
			},
		},
	}
	upstream := &openAIHTTPSenderUpstreamRecorder{}
	svc := &OpenAIGatewayService{
		cfg:                cfg,
		httpUpstream:       upstream,
		gatewayCoreService: NewOpenAIGatewayCoreService(nil, cfg, nil, testOpenAITLSProfileService(&model.TLSFingerprintProfile{ID: 7, Name: "HTTP TLS"})),
	}

	resp, err := svc.sendOpenAIHTTPRequest(context.Background(), newOpenAIHTTPSenderGinContext(), newOpenAIHTTPSenderRequest(t), &Account{
		ID:          101,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 2,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, 0, upstream.plainCalls)
	require.Equal(t, 1, upstream.tlsCalls)
	require.NotNil(t, upstream.lastProfile)
	require.Equal(t, "HTTP TLS", upstream.lastProfile.Name)
}

func TestOpenAIGatewayService_SendOpenAIHTTPRequestUsesPlainUpstreamWhenEffectiveTLSDisabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{{Name: "default", Enabled: true}}
	upstream := &openAIHTTPSenderUpstreamRecorder{}
	svc := &OpenAIGatewayService{
		cfg:                cfg,
		httpUpstream:       upstream,
		gatewayCoreService: NewOpenAIGatewayCoreService(nil, cfg, nil),
	}

	resp, err := svc.sendOpenAIHTTPRequest(context.Background(), newOpenAIHTTPSenderGinContext(), newOpenAIHTTPSenderRequest(t), &Account{
		ID:          102,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 2,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, 1, upstream.plainCalls)
	require.Equal(t, 0, upstream.tlsCalls)
}

func TestOpenAIGatewayService_SendOpenAIHTTPRequestFailsClosedBeforeUpstreamWhenEffectiveTLSCannotResolve(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.TLSBinding.Enabled = true
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{
			Name:    "default",
			Enabled: true,
			TLS:     config.OpenAIGatewayBucketTLSConfig{Enabled: true},
		},
	}
	upstream := &openAIHTTPSenderUpstreamRecorder{}
	svc := &OpenAIGatewayService{
		cfg:                cfg,
		httpUpstream:       upstream,
		gatewayCoreService: NewOpenAIGatewayCoreService(nil, cfg, nil, testOpenAITLSProfileService()),
	}

	resp, err := svc.sendOpenAIHTTPRequest(context.Background(), newOpenAIHTTPSenderGinContext(), newOpenAIHTTPSenderRequest(t), &Account{
		ID:          103,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 2,
	})
	require.Error(t, err)
	require.Nil(t, resp)
	require.Equal(t, 0, upstream.plainCalls)
	require.Equal(t, 0, upstream.tlsCalls)
	var policyErr *OpenAIEgressPolicyError
	require.ErrorAs(t, err, &policyErr)
	require.Equal(t, "tls_policy_no_effective_profile", policyErr.Code)
}
