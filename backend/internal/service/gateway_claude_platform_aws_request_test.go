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
	c := claudePlatformAWSAccountTestContext()

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

func TestGatewayService_BuildClaudePlatformAWSRequestFailsClosedOnCP0BindingMismatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for name, mutate := range map[string]func(*Account){
		"api_key_rotated_after_cp0": func(account *Account) {
			account.Credentials["api_key"] = "synthetic-cpaws-rotated-" + strings.Repeat("r", 16)
		},
		"workspace_rotated_after_cp0": func(account *Account) {
			account.Credentials["anthropic_workspace_id"] = syntheticAWSWorkspaceID(10)
		},
		"stored_endpoint_ref_mismatch": func(account *Account) {
			account.Extra[ClaudePlatformAWSExtraEndpointRef] = formalPoolSafeRef("endpoint", ClaudePlatformAWSEndpointForRegion("eu-west-1"))
		},
		"stored_workspace_hmac_mismatch": func(account *Account) {
			account.Extra[ClaudePlatformAWSExtraWorkspaceBindingHMAC] = "hmac-sha256:" + strings.Repeat("0", 64)
		},
	} {
		t.Run(name, func(t *testing.T) {
			account := claudePlatformAWSRequestTestAccount(t, ClaudePlatformAWSAuthProfileXAPIKey, true)
			mutate(account)

			req, _, err := (&GatewayService{}).buildUpstreamRequest(
				context.Background(), claudePlatformAWSAccountTestContext(), account, []byte(`{"model":"claude-sonnet-4-6","messages":[]}`),
				account.GetCredential("api_key"), "claude_platform_aws", "claude-sonnet-4-6", false, false, false,
			)

			require.ErrorContains(t, err, ClaudePlatformAWSAuthProfileBlocked)
			require.Nil(t, req)
		})
	}
}

func TestGatewayService_BuildClaudePlatformAWSRequestFailsClosedWithoutCP0Evidence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := claudePlatformAWSRequestTestAccount(t, ClaudePlatformAWSAuthProfileXAPIKey, false)

	req, _, err := (&GatewayService{}).buildUpstreamRequest(
		context.Background(), claudePlatformAWSAccountTestContext(), account, []byte(`{"model":"claude-sonnet-4-6","messages":[]}`),
		account.GetCredential("api_key"), "claude_platform_aws", "claude-sonnet-4-6", false, false, false,
	)

	require.ErrorContains(t, err, ClaudePlatformAWSAuthProfileBlocked)
	require.Nil(t, req)
}

func TestGatewayService_CCGatewayClaudePlatformAWSFailsClosedWithoutAuthorityMaterial(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := claudePlatformAWSRequestTestAccount(t, ClaudePlatformAWSAuthProfileXAPIKey, true)
	markClaudePlatformAWSFormalPoolForRequestTest(t, account)
	cfg := ccGatewayTestConfig(PlatformAnthropic)
	cfg.Gateway.CCGateway.StickySessionHMACKey = ""
	cfg.Gateway.CCGateway.ClaudePlatformAWSWorkspaceBindingHMACKey = ""

	req, _, err := (&GatewayService{cfg: cfg}).buildUpstreamRequest(
		context.Background(), claudePlatformAWSRequestTestContext(), account, []byte(`{"model":"claude-sonnet-4-6","messages":[]}`),
		account.GetCredential("api_key"), "claude_platform_aws", "claude-sonnet-4-6", false, false, false,
	)

	require.ErrorContains(t, err, ClaudePlatformAWSAuthProfileBlocked)
	require.ErrorContains(t, err, "authority material")
	require.Nil(t, req)
}

func TestGatewayService_BuildClaudePlatformAWSFormalPoolNormalTrafficCannotBypassCCGatewayWhenProductionFlagFalse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := claudePlatformAWSRequestTestAccount(t, ClaudePlatformAWSAuthProfileXAPIKey, true)
	markClaudePlatformAWSFormalPoolForRequestTest(t, account)
	account.Extra[ClaudePlatformAWSExtraProductionAdmitted] = false

	req, _, err := (&GatewayService{}).buildUpstreamRequest(
		context.Background(), claudePlatformAWSRequestTestContext(), account, []byte(`{"model":"claude-sonnet-4-6","messages":[]}`),
		account.GetCredential("api_key"), "claude_platform_aws", "claude-sonnet-4-6", false, false, false,
	)

	require.ErrorContains(t, err, "CC Gateway")
	require.Nil(t, req)
}

func TestGatewayService_BuildClaudePlatformAWSSpoofedAccountTestPathCannotBypassWithoutServerMarker(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := claudePlatformAWSRequestTestAccount(t, ClaudePlatformAWSAuthProfileXAPIKey, true)
	c := claudePlatformAWSRequestTestContext()
	c.Request.URL.Path = "/api/v1/admin/accounts/5903/test"

	req, _, err := (&GatewayService{}).buildUpstreamRequest(
		context.Background(), c, account, []byte(`{"model":"claude-sonnet-4-6","messages":[]}`),
		account.GetCredential("api_key"), "claude_platform_aws", "claude-sonnet-4-6", false, false, false,
	)

	require.ErrorContains(t, err, "CC Gateway")
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
		context.Background(), claudePlatformAWSAccountTestContext(), account, []byte(`{"model":"claude-sonnet-4-6","messages":[]}`),
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

func TestGatewayService_BuildClaudePlatformAWSRequestStripsBetaAndUnsupportedBodyExtensions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := claudePlatformAWSRequestTestAccount(t, ClaudePlatformAWSAuthProfileXAPIKey, true)
	body := []byte(`{
		"model":"claude-sonnet-4-6",
		"anthropic_beta":["context-management-2025-06-27","interleaved-thinking-2025-05-14"],
		"context_management":{"edits":[{"type":"clear_thinking_20251015"}]},
		"output_config":{"effort":"max"},
		"thinking":{"type":"enabled","budget_tokens":1024},
		"diagnostics":{"trace":true},
		"messages":[{"role":"user","content":"fixture"}]
	}`)

	req, wireBody, err := (&GatewayService{}).buildUpstreamRequest(
		context.Background(), claudePlatformAWSAccountTestContext(), account, body,
		account.GetCredential("api_key"), "claude_platform_aws", "claude-sonnet-4-6", false, false, false,
	)

	require.NoError(t, err)
	require.NotNil(t, req)
	require.Empty(t, getHeaderRaw(req.Header, "anthropic-beta"))
	for _, path := range []string{"anthropic_beta", "context_management", "output_config", "thinking", "diagnostics"} {
		require.False(t, gjson.GetBytes(wireBody, path).Exists(), path)
	}
	require.NoError(t, VerifyClaudePlatformAWSFinalRequest(ClaudePlatformAWSFinalVerifierInput{
		FinalURL:            req.URL.String(),
		Headers:             req.Header,
		Body:                wireBody,
		Region:              "us-east-1",
		AuthScheme:          ClaudePlatformAWSAuthProfileXAPIKey,
		WorkspaceFromServer: true,
		AuthFromServer:      true,
		AllowedPath:         "/v1/messages",
	}))
}

func TestGatewayService_BuildClaudePlatformAWSRequestFailsClosedOnUnknownAWSRequestShapeProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := claudePlatformAWSRequestTestAccount(t, ClaudePlatformAWSAuthProfileXAPIKey, true)
	account.Extra[ClaudePlatformAWSExtraRequestShapeProfileRef] = "request-shape:vertex-profile-is-not-aws"

	req, _, err := (&GatewayService{}).buildUpstreamRequest(
		context.Background(), claudePlatformAWSAccountTestContext(), account, []byte(`{"model":"claude-sonnet-4-6","messages":[]}`),
		account.GetCredential("api_key"), "claude_platform_aws", "claude-sonnet-4-6", false, false, false,
	)

	require.ErrorContains(t, err, ClaudePlatformAWSAuthProfileBlocked)
	require.Nil(t, req)
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

func TestGatewayService_ClaudePlatformAWSCompatRoutesFailClosedBeforeModelMappingOrUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for name, run := range map[string]func(*GatewayService, *gin.Context, *Account) (*ForwardResult, error){
		"chat_completions": func(svc *GatewayService, c *gin.Context, account *Account) (*ForwardResult, error) {
			body := []byte(`{"model":"claude-haiku-4-5","messages":[{"role":"user","content":"hi"}],"stream":false}`)
			return svc.ForwardAsChatCompletions(context.Background(), c, account, body, nil)
		},
		"responses": func(svc *GatewayService, c *gin.Context, account *Account) (*ForwardResult, error) {
			body := []byte(`{"model":"claude-haiku-4-5","input":"hi","stream":false}`)
			return svc.ForwardAsResponses(context.Background(), c, account, body, nil)
		},
	} {
		t.Run(name, func(t *testing.T) {
			account := claudePlatformAWSRequestTestAccount(t, ClaudePlatformAWSAuthProfileXAPIKey, true)
			recorder := &claudePlatformAWSAccountTestUpstream{resp: &http.Response{
				StatusCode: http.StatusTeapot,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"synthetic upstream should not be called"}}`)),
			}}
			responseRecorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(responseRecorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+strings.ReplaceAll(name, "_", "/"), nil)
			svc := &GatewayService{
				httpUpstream:        recorder,
				tlsFPProfileService: &TLSFingerprintProfileService{},
			}

			result, err := run(svc, c, account)

			require.Nil(t, result)
			require.ErrorContains(t, err, "claude-platform-aws compat route")
			require.Nil(t, recorder.lastReq)
		})
	}
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

func claudePlatformAWSAccountTestContext() *gin.Context {
	c := claudePlatformAWSRequestTestContext()
	c.Request.URL.Path = "/api/v1/admin/accounts/5903/test"
	markClaudePlatformAWSDirectBuilderDiagnosticAllowed(c)
	return c
}

func markClaudePlatformAWSFormalPoolForRequestTest(t *testing.T, account *Account) {
	t.Helper()
	account.Status = StatusActive
	account.Schedulable = true
	account.Extra[ccGatewayExtraEgressBucket] = "egress:synthetic-cpaws"
	account.Extra[ccGatewayExtraEgressBucketEnabled] = "true"
	account.Extra[ccGatewayExtraPolicyVersion] = ccGatewayAnthropicPolicyVersion
	validation, err := ValidateClaudePlatformAWSAccount(account)
	require.NoError(t, err)
	account.Extra[ccGatewayExtraAccountRef] = validation.AccountRef
	account.Extra[ccGatewayExtraProxyIdentityRef] = validation.ProxyIdentityRef
	account.Extra[ccGatewayExtraCredentialRef] = validation.CredentialRef
	account.Extra[ccGatewayExtraCredentialBindingHMAC] = validation.CredentialBindingHMAC
	account.Extra[ClaudePlatformAWSExtraWorkspaceRef] = validation.WorkspaceRef
	account.Extra[ClaudePlatformAWSExtraWorkspaceBindingHMAC] = validation.WorkspaceBindingHMAC
	account.Extra[ClaudePlatformAWSExtraEndpointRef] = validation.EndpointRef
	account.Extra[ClaudePlatformAWSExtraRegion] = validation.Region
	account.Extra[FormalPoolExtraRuntimeRegistered] = "true"
	account.Extra[FormalPoolExtraRuntimeRegisteredAt] = "2026-06-27T00:00:00Z"
	account.Extra[ccGatewayExtraPersonaProfile] = ccGatewayDefaultPersonaProfile
	account.Extra["claude_code_device_id"] = strings.Repeat("c", 64)
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
	resp         *http.Response
	err          error
	lastReq      *http.Request
	lastBody     []byte
	lastProxyURL string
	lastProfile  *tlsfingerprint.Profile
}

func (u *claudePlatformAWSAccountTestUpstream) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	return u.DoWithTLS(req, proxyURL, accountID, accountConcurrency, nil)
}

func (u *claudePlatformAWSAccountTestUpstream) DoWithTLS(req *http.Request, proxyURL string, _ int64, _ int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	u.lastReq = req
	u.lastProxyURL = proxyURL
	u.lastProfile = profile
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

func TestGatewayService_ForwardClaudePlatformAWSFormalPoolUsesCCGatewayWithoutAccountProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := claudePlatformAWSRequestTestAccount(t, ClaudePlatformAWSAuthProfileXAPIKey, true)
	markClaudePlatformAWSFormalPoolForRequestTest(t, account)
	account.Proxy = &Proxy{
		ID:       *account.ProxyID,
		Name:     "connect-proxy-should-not-be-used",
		Protocol: "http",
		Host:     "connect-proxy",
		Port:     8080,
		Status:   StatusActive,
	}
	cfg := ccGatewayTestConfig(PlatformAnthropic)
	refreshClaudePlatformAWSFormalPoolAuthorityForRequestTest(t, account, cfg)
	upstream := &claudePlatformAWSAccountTestUpstream{resp: newAnthropicSuccessResponse()}
	svc := &GatewayService{
		cfg:                 cfg,
		httpUpstream:        upstream,
		tlsFPProfileService: &TLSFingerprintProfileService{},
	}
	c := claudePlatformAWSRequestTestContext()
	body := []byte(`{"model":"claude-sonnet-4-6","stream":false,"metadata":{"user_id":"{\"device_id\":\"client-device\",\"account_uuid\":\"client-account\",\"session_id\":\"99999999-8888-4777-8666-555555555555\"}"},"messages":[{"role":"user","content":"fixture"}]}`)

	result, err := svc.Forward(context.Background(), c, account, parseAnthropicRequestForTest(t, body))

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "http://cc-gateway:8443/v1/messages", upstream.lastReq.URL.String())
	require.Empty(t, upstream.lastReq.URL.RawQuery)
	require.Empty(t, upstream.lastProxyURL, "claude-platform-aws formal-pool traffic must not use the account proxy around CC Gateway")
	require.Nil(t, upstream.lastProfile, "claude-platform-aws formal-pool traffic must not use direct account TLS fingerprinting around CC Gateway")
	require.Equal(t, "ccg-token", getHeaderRaw(upstream.lastReq.Header, ccGatewayTokenHeader))
	require.Equal(t, PlatformAnthropic, getHeaderRaw(upstream.lastReq.Header, ccGatewayProviderHeader))
	require.Equal(t, "apikey", getHeaderRaw(upstream.lastReq.Header, ccGatewayTokenTypeHeader))
	require.NotEmpty(t, getHeaderRaw(upstream.lastReq.Header, ccGatewayFormalPoolContextHeader))
	require.NotEmpty(t, getHeaderRaw(upstream.lastReq.Header, ccGatewayFormalPoolSignatureHeader))
	require.Empty(t, getHeaderRaw(upstream.lastReq.Header, "anthropic-workspace-id"), "Sub2API must not send raw workspace to CC Gateway; CC Gateway injects final workspace")
	require.Empty(t, getHeaderRaw(upstream.lastReq.Header, "anthropic-beta"), "AWS Platform provider-scoped beta policy strips downstream beta before CC Gateway")
}

func TestGatewayService_ForwardClaudePlatformAWSFormalPoolStripsDownstreamBillingBeforeCCGateway(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := claudePlatformAWSRequestTestAccount(t, ClaudePlatformAWSAuthProfileXAPIKey, true)
	markClaudePlatformAWSFormalPoolForRequestTest(t, account)
	cfg := ccGatewayTestConfig(PlatformAnthropic)
	refreshClaudePlatformAWSFormalPoolAuthorityForRequestTest(t, account, cfg)
	upstream := &claudePlatformAWSAccountTestUpstream{resp: newAnthropicSuccessResponse()}
	svc := &GatewayService{
		cfg:                 cfg,
		httpUpstream:        upstream,
		tlsFPProfileService: &TLSFingerprintProfileService{},
	}
	c := claudePlatformAWSRequestTestContext()
	body := []byte(`{"model":"claude-opus-4-8","stream":false,"system":[{"type":"text","text":"You are Claude Code.\nX-Anthropic-Billing-Header: cc_version=2.1.197.abc; cc_entrypoint=claude-vscode; cch=12345;\nKeep this system line."}],"metadata":{"user_id":"{\"device_id\":\"client-device\",\"account_uuid\":\"client-account\",\"session_id\":\"99999999-8888-4777-8666-555555555555\"}"},"messages":[{"role":"user","content":"fixture"}]}`)

	result, err := svc.Forward(context.Background(), c, account, parseAnthropicRequestForTest(t, body))

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, upstream.lastReq)
	lower := strings.ToLower(string(upstream.lastBody))
	require.NotContains(t, lower, "x-anthropic-billing-header")
	require.NotContains(t, lower, "cch=12345")
	require.Contains(t, string(upstream.lastBody), "Keep this system line.")
}

func TestGatewayService_ClaudePlatformAWSFormalPoolObservedVersionUsesRawClientHeadersBeforeSanitize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := claudePlatformAWSRequestTestAccount(t, ClaudePlatformAWSAuthProfileXAPIKey, true)
	markClaudePlatformAWSFormalPoolForRequestTest(t, account)
	cfg := ccGatewayTestConfig(PlatformAnthropic)
	refreshClaudePlatformAWSFormalPoolAuthorityForRequestTest(t, account, cfg)
	upstream := &claudePlatformAWSAccountTestUpstream{resp: newAnthropicSuccessResponse()}
	svc := &GatewayService{
		cfg:                 cfg,
		httpUpstream:        upstream,
		tlsFPProfileService: &TLSFingerprintProfileService{},
	}
	c := claudePlatformAWSRequestTestContext()
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.195 (external, sdk-cli)")
	body := []byte(`{"model":"claude-sonnet-4-6","stream":false,"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"99999999-8888-4777-8666-555555555555\"}"},"messages":[{"role":"user","content":"fixture"}]}`)

	result, err := svc.Forward(context.Background(), c, account, parseAnthropicRequestForTest(t, body))

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, upstream.lastReq)
	require.Empty(t, getHeaderRaw(upstream.lastReq.Header, "user-agent"), "client User-Agent must not be forwarded to CC Gateway as authority")
	ctx := decodeCCGatewayFormalPoolContextForTest(t, upstream.lastReq)
	observed := ctx["observed_client_profile"].(map[string]any)
	require.Equal(t, "2.1.195", observed["cli_version_bucket"])
	require.Equal(t, ccGatewayAnthropicPolicyVersion, ctx["policy_version"])
	require.Equal(t, ccGatewayDefault2179ProfilePolicyVersion, ctx["profile_policy_version"])
	require.Equal(t, account.GetExtraString(ClaudePlatformAWSExtraRequestShapeProfileRef), ctx["request_shape_profile_ref"])
	require.Equal(t, account.GetExtraString(ClaudePlatformAWSExtraCacheParityProfileRef), ctx["cache_parity_profile_ref"])
}

func refreshClaudePlatformAWSFormalPoolAuthorityForRequestTest(t *testing.T, account *Account, cfg *config.Config) {
	t.Helper()
	delete(account.Extra, ccGatewayExtraAccountRef)
	delete(account.Extra, ccGatewayExtraProxyIdentityRef)
	delete(account.Extra, ccGatewayExtraCredentialRef)
	delete(account.Extra, ccGatewayExtraCredentialBindingHMAC)
	delete(account.Extra, ClaudePlatformAWSExtraWorkspaceRef)
	delete(account.Extra, ClaudePlatformAWSExtraWorkspaceBindingHMAC)
	delete(account.Extra, ClaudePlatformAWSExtraEndpointRef)
	validation, err := ValidateClaudePlatformAWSAccountWithCCGatewayConfig(account, cfg)
	require.NoError(t, err)
	account.Extra[ccGatewayExtraAccountRef] = validation.AccountRef
	account.Extra[ccGatewayExtraProxyIdentityRef] = validation.ProxyIdentityRef
	account.Extra[ccGatewayExtraCredentialRef] = validation.CredentialRef
	account.Extra[ccGatewayExtraCredentialBindingHMAC] = validation.CredentialBindingHMAC
	account.Extra[ClaudePlatformAWSExtraWorkspaceRef] = validation.WorkspaceRef
	account.Extra[ClaudePlatformAWSExtraWorkspaceBindingHMAC] = validation.WorkspaceBindingHMAC
	account.Extra[ClaudePlatformAWSExtraEndpointRef] = validation.EndpointRef
	account.Extra[ClaudePlatformAWSExtraRegion] = validation.Region
}
