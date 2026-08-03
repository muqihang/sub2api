package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type cchNoopSettingRepo struct{}

func (cchNoopSettingRepo) Get(context.Context, string) (*Setting, error) {
	return nil, ErrSettingNotFound
}
func (cchNoopSettingRepo) GetValue(context.Context, string) (string, error) { return "", nil }
func (cchNoopSettingRepo) Set(context.Context, string, string) error        { return nil }
func (cchNoopSettingRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{}, nil
}
func (cchNoopSettingRepo) SetMultiple(context.Context, map[string]string) error { return nil }
func (cchNoopSettingRepo) GetAll(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}
func (cchNoopSettingRepo) Delete(context.Context, string) error { return nil }

var _ SettingRepository = cchNoopSettingRepo{}

func seedCCHSigningEnabledForNoopTest(t *testing.T) {
	t.Helper()
	gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{
		fingerprintUnification:       true,
		metadataPassthrough:          false,
		cchSigning:                   true,
		anthropicCacheTTL1hInjection: false,
		rewriteMessageCacheControl:   false,
		expiresAt:                    time.Now().Add(time.Hour).UnixNano(),
	})
	t.Cleanup(func() {
		gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{
			fingerprintUnification:       true,
			metadataPassthrough:          false,
			cchSigning:                   false,
			anthropicCacheTTL1hInjection: false,
			rewriteMessageCacheControl:   false,
			expiresAt:                    time.Now().Add(time.Hour).UnixNano(),
		})
	})
}

func newCCHNoopTestContext(path string) *gin.Context {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	return c
}

func readRequestBodyForCCHNoopTest(t *testing.T, req *http.Request) string {
	t.Helper()
	require.NotNil(t, req)
	body, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	return string(body)
}

func newCCHNoopOAuthAccount() *Account {
	return &Account{
		ID:          9101,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "oauth-token"},
		Status:      StatusActive,
		Schedulable: true,
	}
}

func TestBuildUpstreamRequestCCHSigningSettingIsNoop(t *testing.T) {
	seedCCHSigningEnabledForNoopTest(t)

	body := []byte(`{"model":"claude-3-7-sonnet-20250219","system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.197.abc; cc_entrypoint=sdk-cli; cch=00000;"}],"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)
	svc := &GatewayService{cfg: &config.Config{}, settingService: NewSettingService(cchNoopSettingRepo{}, &config.Config{})}
	req, _, err := svc.buildUpstreamRequest(
		context.Background(), newCCHNoopTestContext("/v1/messages"), newCCHNoopOAuthAccount(), body,
		"oauth-token", "oauth", "claude-3-7-sonnet-20250219", false, false, false,
	)
	require.NoError(t, err)

	out := readRequestBodyForCCHNoopTest(t, req)
	require.Contains(t, out, "cch=00000;", "enable_cch_signing is retained for compatibility but must not sign new CLI-incompatible CCH placeholders")
}

func TestBuildCountTokensRequestCCHSigningSettingIsNoop(t *testing.T) {
	seedCCHSigningEnabledForNoopTest(t)

	body := []byte(`{"model":"claude-3-7-sonnet-20250219","system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.197.abc; cc_entrypoint=sdk-cli; cch=00000;"}],"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)
	svc := &GatewayService{cfg: &config.Config{}, settingService: NewSettingService(cchNoopSettingRepo{}, &config.Config{})}
	req, _, err := svc.buildCountTokensRequest(
		context.Background(), newCCHNoopTestContext("/v1/messages/count_tokens"), newCCHNoopOAuthAccount(), body,
		"oauth-token", "oauth", "claude-3-7-sonnet-20250219", false, false,
	)
	require.NoError(t, err)

	out := readRequestBodyForCCHNoopTest(t, req)
	require.Contains(t, out, "cch=00000;", "count_tokens must share the no-op CCH signing semantics")
}
