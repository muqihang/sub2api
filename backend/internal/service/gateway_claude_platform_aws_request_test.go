package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGatewayService_BuildClaudePlatformAWSRequestUsesDerivedEndpointServerWorkspaceAndXAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := claudePlatformAWSRequestTestAccount(t, ClaudePlatformAWSAuthProfileXAPIKey, true)
	body := []byte(`{"model":"claude-sonnet-4-6","stream":false,"max_tokens":32,"context_management":{"edits":[{"type":"clear_thinking_20251015"}]},"messages":[{"role":"user","content":"fixture"}]}`)
	c := claudePlatformAWSRequestTestContext()

	req, wireBody, err := (&GatewayService{}).buildUpstreamRequest(
		context.Background(), c, account, body,
		account.GetCredential("api_key"), "claude_platform_aws", "claude-sonnet-4-6", false, false, false,
	)

	require.NoError(t, err)
	require.Equal(t, "https://aws-external-anthropic.us-east-1.api.aws/v1/messages", req.URL.String())
	require.Equal(t, "", req.URL.RawQuery)
	require.Equal(t, account.GetCredential("api_key"), getHeaderRaw(req.Header, "x-api-key"))
	require.Empty(t, getHeaderRaw(req.Header, "authorization"))
	require.Equal(t, account.GetCredential("anthropic_workspace_id"), getHeaderRaw(req.Header, "anthropic-workspace-id"))
	require.Equal(t, "2023-06-01", getHeaderRaw(req.Header, "anthropic-version"))
	require.Contains(t, strings.ToLower(getHeaderRaw(req.Header, "content-type")), "application/json")

	for _, forbidden := range []string{
		"anthropic-beta", "x-cc-account-id", "x-sub2api-profile", "x-amz-security-token",
		"x-anthropic-billing-header", "x-credential-ref", "x-profile", "x-session",
	} {
		require.Empty(t, getHeaderRaw(req.Header, forbidden), forbidden)
	}

	require.Equal(t, "claude-sonnet-4-6", gjson.GetBytes(wireBody, "model").String())
	require.NotContains(t, string(wireBody), "anthropic.claude", "AWS Platform must not use Bedrock model mapping")
	require.False(t, gjson.GetBytes(wireBody, "anthropic_version").Exists(), "AWS Platform must not use Bedrock body conversion")
	require.False(t, gjson.GetBytes(wireBody, "context_management").Exists(), "default AWS beta policy strips unsupported beta-shaped body fields")

	require.NoError(t, VerifyClaudePlatformAWSFinalRequest(ClaudePlatformAWSFinalVerifierInput{
		FinalURL:            req.URL.String(),
		Headers:             req.Header,
		Region:              "us-east-1",
		AuthScheme:          ClaudePlatformAWSAuthProfileXAPIKey,
		WorkspaceFromServer: true,
		AuthFromServer:      true,
		AllowedPath:         "/v1/messages",
	}))
}

func TestGatewayService_BuildClaudePlatformAWSRequestFailsClosedWithoutCP0Evidence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := claudePlatformAWSRequestTestAccount(t, ClaudePlatformAWSAuthProfileXAPIKey, false)

	req, _, err := (&GatewayService{}).buildUpstreamRequest(
		context.Background(), claudePlatformAWSRequestTestContext(), account, []byte(`{"model":"claude-sonnet-4-6","messages":[]}`),
		account.GetCredential("api_key"), "claude_platform_aws", "claude-sonnet-4-6", false, false, false,
	)

	require.ErrorContains(t, err, ClaudePlatformAWSAuthProfileBlocked)
	require.Nil(t, req)
}

func TestGatewayService_BuildClaudePlatformAWSProductionAdmittedRequestCannotBypassCCGateway(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := claudePlatformAWSRequestTestAccount(t, ClaudePlatformAWSAuthProfileXAPIKey, true)
	account.Extra[ClaudePlatformAWSExtraProductionAdmitted] = true

	req, _, err := (&GatewayService{}).buildUpstreamRequest(
		context.Background(), claudePlatformAWSRequestTestContext(), account, []byte(`{"model":"claude-sonnet-4-6","messages":[]}`),
		account.GetCredential("api_key"), "claude_platform_aws", "claude-sonnet-4-6", false, false, false,
	)

	require.ErrorContains(t, err, "CC Gateway")
	require.Nil(t, req)
}

func TestGatewayService_BuildClaudePlatformAWSRequestUsesBearerOnlyWhenExplicitEvidenceSelectsBearer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := claudePlatformAWSRequestTestAccount(t, ClaudePlatformAWSAuthProfileBearerAPIKey, true)

	req, _, err := (&GatewayService{}).buildUpstreamRequest(
		context.Background(), claudePlatformAWSRequestTestContext(), account, []byte(`{"model":"claude-sonnet-4-6","messages":[]}`),
		account.GetCredential("api_key"), "claude_platform_aws", "claude-sonnet-4-6", false, false, false,
	)

	require.NoError(t, err)
	require.Equal(t, "Bearer "+account.GetCredential("api_key"), getHeaderRaw(req.Header, "authorization"))
	require.Empty(t, getHeaderRaw(req.Header, "x-api-key"))
	require.NoError(t, VerifyClaudePlatformAWSFinalRequest(ClaudePlatformAWSFinalVerifierInput{
		FinalURL:            req.URL.String(),
		Headers:             req.Header,
		Region:              "us-east-1",
		AuthScheme:          ClaudePlatformAWSAuthProfileBearerAPIKey,
		WorkspaceFromServer: true,
		AuthFromServer:      true,
		AllowedPath:         "/v1/messages",
	}))
}

func TestGatewayService_GetAccessTokenClaudePlatformAWSIsNotOAuthBedrockVertexOrGenericUpstream(t *testing.T) {
	account := claudePlatformAWSRequestTestAccount(t, ClaudePlatformAWSAuthProfileXAPIKey, true)

	token, tokenType, err := (&GatewayService{}).GetAccessToken(context.Background(), account)

	require.NoError(t, err)
	require.Equal(t, account.GetCredential("api_key"), token)
	require.Equal(t, "claude_platform_aws", tokenType)
	require.False(t, account.IsAnthropicOAuthOrSetupToken())
	require.False(t, account.IsBedrock())
	require.False(t, account.IsAnthropicAPIKeyPassthroughEnabled())
	require.NotEqual(t, AccountTypeServiceAccount, account.Type)
	require.NotEqual(t, AccountTypeUpstream, account.Type)
}

func TestGatewayService_ClaudePlatformAWSModelPreflightKeepsNativeAnthropicModelNames(t *testing.T) {
	account := claudePlatformAWSRequestTestAccount(t, ClaudePlatformAWSAuthProfileXAPIKey, true)
	body := []byte(`{"model":"claude-haiku-4-5","messages":[]}`)

	mappedBody, mappedModel := (&GatewayService{}).applyAnthropicPreflightModelMapping(body, account, "claude-haiku-4-5")

	require.Equal(t, "claude-haiku-4-5", mappedModel)
	require.Equal(t, "claude-haiku-4-5", gjson.GetBytes(mappedBody, "model").String())
	require.NotEqual(t, "claude-haiku-4-5-20251001", gjson.GetBytes(mappedBody, "model").String(), "AWS Platform must not use OAuth prefix normalization")
}

func TestGatewayService_BuildClaudePlatformAWSCountTokensPathFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := claudePlatformAWSRequestTestAccount(t, ClaudePlatformAWSAuthProfileXAPIKey, true)

	req, _, err := (&GatewayService{}).buildCountTokensRequest(
		context.Background(), claudePlatformAWSRequestTestContext(), account, []byte(`{"model":"claude-sonnet-4-6","messages":[]}`),
		account.GetCredential("api_key"), "claude_platform_aws", "claude-sonnet-4-6", false, false,
	)

	require.ErrorContains(t, err, "/v1/messages")
	require.Nil(t, req)
}

func TestAccountTestService_ClaudePlatformAWSUsesDedicatedSafeRequestPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := claudePlatformAWSRequestTestAccount(t, ClaudePlatformAWSAuthProfileXAPIKey, true)
	account.Status = StatusActive
	account.Schedulable = true
	account.Concurrency = 1
	recorder := &claudePlatformAWSAccountTestUpstream{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"ok\"}}\n\ndata: [DONE]\n\n")),
	}}
	svc := &AccountTestService{
		accountRepo:  &claudePlatformAWSAccountTestRepo{account: account},
		httpUpstream: recorder,
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
	}
	responseRecorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(responseRecorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/5903/test", nil)
	c.Request.Header.Set("Anthropic-Workspace-Id", syntheticAWSWorkspaceID(9))
	c.Request.Header.Set("Authorization", "Bearer synthetic-client-forged-token")
	c.Request.Header.Set("X-Api-Key", "synthetic-client-forged-api-key")
	c.Request.Header.Set("Anthropic-Beta", "advisor-tool-2026-03-01")

	err := svc.TestAccountConnection(c, account.ID, "claude-sonnet-4-6", "", "")

	require.NoError(t, err)
	require.NotNil(t, recorder.lastReq)
	require.Equal(t, "https://aws-external-anthropic.us-east-1.api.aws/v1/messages", recorder.lastReq.URL.String())
	require.Equal(t, account.GetCredential("api_key"), getHeaderRaw(recorder.lastReq.Header, "x-api-key"))
	require.Empty(t, getHeaderRaw(recorder.lastReq.Header, "authorization"))
	require.Equal(t, account.GetCredential("anthropic_workspace_id"), getHeaderRaw(recorder.lastReq.Header, "anthropic-workspace-id"))
	require.Empty(t, getHeaderRaw(recorder.lastReq.Header, "anthropic-beta"))
	require.Equal(t, "claude-sonnet-4-6", gjson.GetBytes(recorder.lastBody, "model").String())
	require.NotContains(t, string(recorder.lastBody), "anthropic.claude")
	require.NotContains(t, responseRecorder.Body.String(), "ok", "AWS account test DTO must not echo raw upstream response")
	require.Contains(t, responseRecorder.Body.String(), "safe probe succeeded")
}

func TestAccountTestService_ClaudePlatformAWSRedactsTransportErrorsAndProxyCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := claudePlatformAWSRequestTestAccount(t, ClaudePlatformAWSAuthProfileXAPIKey, true)
	account.Status = StatusActive
	account.Schedulable = true
	proxyUser := "redaction-user-" + strings.Repeat("u", 4)
	proxyPass := "redaction-pass-" + strings.Repeat("p", 4)
	account.Proxy = &Proxy{Protocol: "socks5h", Host: "127.0.0.1", Port: 1080, Username: proxyUser, Password: proxyPass}
	recorder := &claudePlatformAWSAccountTestUpstream{err: errors.New("dial " + account.Proxy.URL() + " failed")}
	svc := &AccountTestService{
		accountRepo:  &claudePlatformAWSAccountTestRepo{account: account},
		httpUpstream: recorder,
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
	}
	responseRecorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(responseRecorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/5903/test", nil)

	err := svc.TestAccountConnection(c, account.ID, "claude-sonnet-4-6", "", "")

	require.Error(t, err)
	out := responseRecorder.Body.String()
	require.NotContains(t, out, proxyUser)
	require.NotContains(t, out, proxyPass)
	require.NotContains(t, out, account.GetCredential("api_key"))
	require.NotContains(t, out, account.GetCredential("anthropic_workspace_id"))
	require.Contains(t, out, "transport_error")
	require.Contains(t, out, "raw_error_omitted")
}

func claudePlatformAWSRequestTestAccount(t *testing.T, authScheme string, evidencePass bool) *Account {
	t.Helper()
	proxyID := int64(5903)
	account := &Account{
		ID:       5903,
		Platform: PlatformAnthropic,
		Type:     AccountTypeClaudePlatformAWS,
		ProxyID:  &proxyID,
		Credentials: map[string]any{
			"auth_mode":              "apikey",
			"api_key":                syntheticAWSAPIKey(),
			"aws_region":             "us-east-1",
			"anthropic_workspace_id": syntheticAWSWorkspaceID(3),
			"base_url":               "https://aws-external-anthropic.us-east-1.api.aws",
		},
		Extra: map[string]any{
			ClaudePlatformAWSExtraAuthScheme:             authScheme,
			ClaudePlatformAWSExtraRequestShapeProfileRef: "request-shape:claude-platform-aws-v1-strip",
			ClaudePlatformAWSExtraCacheParityProfileRef:  "cache-profile:claude-platform-aws-v1-strip",
			ClaudePlatformAWSExtraBetaPolicyRef:          "beta-policy:claude-platform-aws-v1-strip",
		},
	}
	validation, err := ValidateClaudePlatformAWSAccount(account)
	require.NoError(t, err)
	account.Extra[ClaudePlatformAWSExtraWorkspaceRef] = validation.WorkspaceRef
	account.Extra[ClaudePlatformAWSExtraWorkspaceBindingHMAC] = validation.WorkspaceBindingHMAC
	account.Extra[ClaudePlatformAWSExtraEndpointRef] = validation.EndpointRef
	account.Extra[ClaudePlatformAWSExtraRegion] = validation.Region
	account.Extra[ccGatewayExtraCredentialRef] = validation.CredentialRef
	account.Extra[ccGatewayExtraCredentialBindingHMAC] = validation.CredentialBindingHMAC
	if evidencePass {
		account.Extra[ClaudePlatformAWSExtraCP0AuthProfileEvidenceStatus] = "pass"
		account.Extra[ClaudePlatformAWSExtraCP0RegionWorkspaceEvidenceStatus] = "pass"
	} else {
		account.Extra[ClaudePlatformAWSExtraCP0AuthProfileEvidenceStatus] = ClaudePlatformAWSAuthProfileBlocked
		account.Extra[ClaudePlatformAWSExtraCP0RegionWorkspaceEvidenceStatus] = "blocked"
	}
	return account
}

func claudePlatformAWSRequestTestContext() *gin.Context {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages?beta=true", nil)
	c.Request.Header.Set("Anthropic-Workspace-Id", syntheticAWSWorkspaceID(8))
	c.Request.Header.Set("X-Api-Key", "synthetic-client-forged-api-key")
	c.Request.Header.Set("Authorization", "Bearer synthetic-client-forged-token")
	c.Request.Header.Set("Anthropic-Beta", "advisor-tool-2026-03-01,prompt-caching-scope-2026-01-05,redact-thinking-2026-02-12,thinking-token-count-2026-05-13")
	c.Request.Header.Set("X-Cc-Account-Id", "account:client-forged")
	c.Request.Header.Set("X-Sub2api-Profile", "profile:client-forged")
	c.Request.Header.Set("X-Amz-Security-Token", "synthetic-client-forged-token")
	c.Request.Header.Set("X-Anthropic-Billing-Header", "synthetic-client-forged-billing")
	c.Request.Header.Set("X-Credential-Ref", "credential:client-forged")
	c.Request.Header.Set("X-Profile", "profile:client-forged")
	c.Request.Header.Set("X-Session", "session:client-forged")
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Anthropic-Version", "2023-06-01")
	c.Request.Header.Set("Accept", "application/json")
	return c
}

func readClaudePlatformAWSRequestBodyForTest(t *testing.T, req *http.Request) []byte {
	t.Helper()
	body, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	return body
}

type claudePlatformAWSAccountTestRepo struct {
	AccountRepository
	account *Account
}

func (r *claudePlatformAWSAccountTestRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}

type claudePlatformAWSAccountTestUpstream struct {
	resp     *http.Response
	err      error
	lastReq  *http.Request
	lastBody []byte
}

func (u *claudePlatformAWSAccountTestUpstream) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	return u.DoWithTLS(req, proxyURL, accountID, accountConcurrency, nil)
}

func (u *claudePlatformAWSAccountTestUpstream) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	u.lastReq = req
	if req != nil && req.Body != nil {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		u.lastBody = body
		req.Body = io.NopCloser(strings.NewReader(string(body)))
	}
	if u.err != nil {
		return nil, u.err
	}
	return u.resp, nil
}
