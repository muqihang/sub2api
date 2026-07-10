package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/model"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type codexModelsManifestUpstreamRecorder struct {
	body       []byte
	header     http.Header
	statusCode int
	doCalls    int
	tlsCalls   int
	request    *http.Request
	proxyURL   string
	profile    *tlsfingerprint.Profile
}

func (u *codexModelsManifestUpstreamRecorder) Do(req *http.Request, proxyURL string, _ int64, _ int) (*http.Response, error) {
	u.doCalls++
	u.request = req
	u.proxyURL = proxyURL
	return u.response(), nil
}

func (u *codexModelsManifestUpstreamRecorder) DoWithTLS(req *http.Request, proxyURL string, _ int64, _ int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	u.tlsCalls++
	u.request = req
	u.proxyURL = proxyURL
	u.profile = profile
	return u.response(), nil
}

func (u *codexModelsManifestUpstreamRecorder) response() *http.Response {
	header := make(http.Header, len(u.header))
	for key, values := range u.header {
		header[key] = append([]string(nil), values...)
	}
	statusCode := u.statusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	return &http.Response{
		StatusCode: statusCode,
		Header:     header,
		Body:       io.NopCloser(bytes.NewReader(u.body)),
	}
}

func TestOpenAIGatewayService_FetchCodexModelsManifestPreservesNotModifiedResponse(t *testing.T) {
	upstream := &codexModelsManifestUpstreamRecorder{
		statusCode: http.StatusNotModified,
		header:     http.Header{"Etag": []string{`W/"manifest-etag"`}},
	}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	account := &Account{
		ID:       43,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "test-access-token",
		},
	}

	manifest, err := svc.FetchCodexModelsManifest(context.Background(), account, "0.150.0", `W/"client-etag"`)
	require.NoError(t, err)
	require.True(t, manifest.NotModified)
	require.Empty(t, manifest.Body)
	require.Equal(t, `W/"manifest-etag"`, manifest.ETag)
	require.Equal(t, `W/"client-etag"`, upstream.request.Header.Get("If-None-Match"))
}

func TestOpenAIGatewayService_FetchCodexModelsManifestUsesManagedEgressAndPreservesManifest(t *testing.T) {
	manifestBody := []byte(`{"models":[{"slug":"gpt-5.5","display_name":"GPT-5.5"}]}`)
	upstream := &codexModelsManifestUpstreamRecorder{
		body:   manifestBody,
		header: http.Header{"Etag": []string{`W/"manifest-etag"`}},
	}
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.TLSBinding.Enabled = true
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "manifest"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{{
		Name:     "manifest",
		Enabled:  true,
		ProxyURL: "socks5://127.0.0.1:9001",
		TLS:      config.OpenAIGatewayBucketTLSConfig{Enabled: true, ProfileID: 7},
	}}
	svc := &OpenAIGatewayService{
		httpUpstream:       upstream,
		gatewayCoreService: NewOpenAIGatewayCoreService(nil, cfg, nil, testOpenAITLSProfileService(&model.TLSFingerprintProfile{ID: 7, Name: "manifest TLS"})),
	}
	account := &Account{
		ID:          44,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 2,
		Credentials: map[string]any{
			"access_token":       "test-access-token",
			"chatgpt_account_id": "account-44",
		},
	}

	originalURL := chatgptCodexModelsURL
	chatgptCodexModelsURL = "https://chatgpt.example/backend-api/codex/models"
	t.Cleanup(func() { chatgptCodexModelsURL = originalURL })

	manifest, err := svc.FetchCodexModelsManifest(context.Background(), account, "0.150.0", `W/"client-etag"`)
	require.NoError(t, err)
	require.Equal(t, manifestBody, manifest.Body)
	require.Equal(t, `W/"manifest-etag"`, manifest.ETag)
	require.Equal(t, 0, upstream.doCalls)
	require.Equal(t, 1, upstream.tlsCalls)
	require.Equal(t, "socks5://127.0.0.1:9001", upstream.proxyURL)
	require.NotNil(t, upstream.profile)
	require.Equal(t, "manifest TLS", upstream.profile.Name)
	require.NotNil(t, upstream.request)
	require.Equal(t, http.MethodGet, upstream.request.Method)
	require.Equal(t, "https://chatgpt.example/backend-api/codex/models", upstream.request.URL.Scheme+"://"+upstream.request.URL.Host+upstream.request.URL.Path)
	require.Equal(t, "0.150.0", upstream.request.URL.Query().Get("client_version"))
	require.Equal(t, "Bearer test-access-token", upstream.request.Header.Get("Authorization"))
	require.Equal(t, "account-44", upstream.request.Header.Get("chatgpt-account-id"))
	require.Equal(t, "codex_cli_rs", upstream.request.Header.Get("Originator"))
	require.Equal(t, "0.150.0", upstream.request.Header.Get("Version"))
	require.Equal(t, `W/"client-etag"`, upstream.request.Header.Get("If-None-Match"))
}

func TestOpenAIGatewayService_FetchCodexModelsManifestFailsClosedBeforeUpstream(t *testing.T) {
	upstream := &codexModelsManifestUpstreamRecorder{header: make(http.Header)}
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.EgressFailClosed = true
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "missing"
	svc := &OpenAIGatewayService{
		httpUpstream:       upstream,
		gatewayCoreService: NewOpenAIGatewayCoreService(nil, cfg, nil),
	}
	account := &Account{
		ID:       45,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "test-access-token",
		},
	}

	_, err := svc.FetchCodexModelsManifest(context.Background(), account, "0.150.0", "")
	require.Error(t, err)
	var policyErr *OpenAIEgressPolicyError
	require.ErrorAs(t, err, &policyErr)
	require.Equal(t, "missing_bucket", policyErr.Code)
	require.Equal(t, http.StatusBadGateway, infraerrors.Code(err))
	require.Equal(t, 0, upstream.doCalls)
	require.Equal(t, 0, upstream.tlsCalls)
}

func TestOpenAIGatewayService_FetchCodexModelsManifestRejectsNonOAuthAccount(t *testing.T) {
	svc := &OpenAIGatewayService{httpUpstream: &codexModelsManifestUpstreamRecorder{header: make(http.Header)}}
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "test-api-key",
		},
	}

	_, err := svc.FetchCodexModelsManifest(context.Background(), account, "0.150.0", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "OAuth")
}

func TestCodexModelsManifestRequestURLEscapesClientVersion(t *testing.T) {
	requestURL := buildCodexModelsManifestURL("https://chatgpt.example/backend-api/codex/models", "0.150.0+build test")
	parsed, err := url.Parse(requestURL)
	require.NoError(t, err)
	require.Equal(t, "0.150.0+build test", parsed.Query().Get("client_version"))
}
