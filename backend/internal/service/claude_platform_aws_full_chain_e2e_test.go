package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const (
	cp6AWSGatewayWorkspaceRefSecret = "cp6-local-workspace-ref-hmac-material-123456"
	cp6AWSGatewayBindingSecret      = "cp6-local-workspace-binding-hmac-material-123456"
)

func TestClaudePlatformAWSLocalFullChainE2EUsesCCGatewayAndSafeMockUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("CC_GATEWAY_REPO_ROOT", "/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5")
	require.DirExists(t, os.Getenv("CC_GATEWAY_REPO_ROOT"))
	useClaudeCodeSessionBoundaryLedgerFileForTest(t)

	proxyA := startConnectProxyServer(t)
	proxyB := startConnectProxyServer(t)
	accountA := newCP6ClaudePlatformAWSAccount(t, 0, proxyA.URL())
	accountB := newCP6ClaudePlatformAWSAccount(t, 1, proxyB.URL())
	mockAWS := startCP6AWSMockUpstream(t, accountA, accountB)
	require.NotEqual(t, accountA.GetExtraString(ccGatewayExtraAccountRef), accountB.GetExtraString(ccGatewayExtraAccountRef))
	require.NotEqual(t, accountA.GetExtraString(ClaudePlatformAWSExtraWorkspaceRef), accountB.GetExtraString(ClaudePlatformAWSExtraWorkspaceRef))
	require.NotEqual(t, accountA.GetExtraString(ccGatewayExtraCredentialRef), accountB.GetExtraString(ccGatewayExtraCredentialRef))
	require.NotEqual(t, accountA.GetExtraString(ccGatewayExtraEgressBucket), accountB.GetExtraString(ccGatewayExtraEgressBucket))
	gateway := startCCGatewayProcess(t, cp6AWSGatewayConfigYAML(t, mockAWS.URL(), []*cp6AWSGatewayAccountConfig{
		{account: accountA, proxyURL: proxyA.URL()},
		{account: accountB, proxyURL: proxyB.URL()},
	}))
	gatewayUpstream := &jointGatewayRecordingUpstream{client: &http.Client{Timeout: 10 * time.Second}}
	svc := newCP6ClaudePlatformAWSService(gateway.baseURL, gatewayUpstream)

	c, ctx, rec := newCP6AWSClientContext(t, jointClientStripSessionID)
	body := []byte(`{
		"model":"claude-sonnet-4-6",
		"stream":true,
		"max_tokens":32,
		"metadata":{"user_id":"{\"session_id\":\"123e4567-e89b-42d3-a456-426614174999\"}"},
		"anthropic_beta":["advisor-tool-2026-03-01","prompt-caching-scope-2026-01-05"],
		"context_management":{"edits":[{"type":"clear_thinking_20251015"}]},
		"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]
	}`)

	result, err := svc.Forward(ctx, c, accountA, parseAnthropicRequestForTest(t, body))

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, rec.Code)

	hopToGateway := gatewayUpstream.popSingle(t)
	hopToAWS := mockAWS.popSingle(t)
	requireCP6AWSMockEvidenceSafe(t, hopToAWS, accountA)
	require.True(t, cp6ProxySawMockUpstream(proxyA), "CC Gateway must reach account A mock AWS through proxy A")
	require.False(t, cp6ProxySawMockUpstream(proxyB), "account A request must not use proxy B")
	require.Contains(t, hopToGateway.URL, gateway.baseURL+"/v1/messages")
	require.Empty(t, hopToGateway.ProxyURL, "Sub2API must not bypass CC Gateway with direct account proxy")

	c2, ctx2, rec2 := newCP6AWSClientContext(t, "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee")
	result, err = svc.Forward(ctx2, c2, accountB, parseAnthropicRequestForTest(t, body))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, rec2.Code)
	requireCP6AWSMockEvidenceSafe(t, mockAWS.popSingle(t), accountB)
	require.True(t, cp6ProxySawMockUpstream(proxyB), "CC Gateway must reach account B mock AWS through proxy B")
	require.Len(t, cp6GatewayHops(gatewayUpstream), 1)

	gatewayLogs := gateway.stdout.String() + gateway.stderr.String()
	for _, account := range []*Account{accountA, accountB} {
		require.NotContains(t, gatewayLogs, account.GetCredential("anthropic_workspace_id"))
		require.NotContains(t, gatewayLogs, account.GetCredential("api_key"))
	}
}

func newCP6ClaudePlatformAWSService(baseURL string, upstream *jointGatewayRecordingUpstream) *GatewayService {
	seedGatewayForwardingSettingsForTest()
	cfg := ccGatewayTestConfig(PlatformAnthropic)
	cfg.Gateway.MaxLineSize = defaultMaxLineSize
	cfg.Gateway.CCGateway.BaseURL = baseURL
	cfg.Gateway.CCGateway.Token = jointGatewayToken
	cfg.Gateway.CCGateway.InternalControlToken = jointGatewayInternalControlToken
	cfg.Gateway.CCGateway.ContextAttestationSecret = jointGatewayContextAttestationSecret
	cfg.Gateway.CCGateway.StickySessionHMACKey = cp6AWSGatewayWorkspaceRefSecret
	cfg.Gateway.CCGateway.ClaudePlatformAWSWorkspaceBindingHMACKey = cp6AWSGatewayBindingSecret
	cfg.Gateway.CCGateway.DefaultEgressBucket = ""
	return &GatewayService{
		cfg:             cfg,
		identityService: NewIdentityService(&identityCacheStub{}),
		httpUpstream:    upstream,
	}
}

func newCP6ClaudePlatformAWSAccount(t *testing.T, index int, proxyURL string) *Account {
	t.Helper()
	account := claudePlatformAWSRequestTestAccount(t, ClaudePlatformAWSAuthProfileXAPIKey, true)
	account.ID = int64(5903 + index)
	account.Credentials["api_key"] = fmt.Sprintf("synthetic-cpaws-%02d-%s", index, strings.Repeat("k", 16))
	account.Credentials["anthropic_workspace_id"] = syntheticAWSWorkspaceID(3 + index)
	markClaudePlatformAWSFormalPoolForRequestTest(t, account)
	account.Extra[ccGatewayExtraEgressBucket] = cp6AWSEgressBucket(index)
	account.Extra[ccGatewayExtraEgressBucketEnabled] = "true"
	account.Extra["cc_gateway_enabled"] = "true"
	account.Extra["cc_gateway_canary_only"] = "false"
	account.Extra["cc_gateway_routes"] = "native_messages"
	account.Extra[ccGatewayExtraPolicyVersion] = ccGatewayAnthropicPolicyVersion
	account.Extra[ccGatewayExtraProxyIdentityRef] = fmt.Sprintf("opaque:proxy-ref:v1:cp6-aws-%d", index)
	account.Extra[ccGatewayExtraPersonaProfile] = jointExpectedGatewayPersonaVariant
	account.Extra[ccGatewayExtraTrustedEgressProfile] = ccGatewayDefaultTrustedEgressProfileRef
	account.Extra[ccGatewayExtraProfilePolicyVersion] = ccGatewayDefault2179ProfilePolicyVersion
	account.Extra[ccGatewayExtraBillingShapePolicy] = ccGatewayDefaultBillingShapePolicy
	account.Extra[ccGatewayExtraRequestShapeProfile] = account.GetExtraString(ClaudePlatformAWSExtraRequestShapeProfileRef)
	account.Extra[ccGatewayExtraCacheParityProfile] = account.GetExtraString(ClaudePlatformAWSExtraCacheParityProfileRef)
	delete(account.Extra, ccGatewayExtraAccountRef)
	validation, err := ValidateClaudePlatformAWSAccountWithCCGatewayConfig(account, cp6AWSAuthorityConfig())
	require.NoError(t, err)
	account.Extra[ccGatewayExtraAccountRef] = validation.AccountRef
	account.Extra[ccGatewayExtraCredentialRef] = validation.CredentialRef
	account.Extra[ccGatewayExtraCredentialBindingHMAC] = validation.CredentialBindingHMAC
	account.Extra[ClaudePlatformAWSExtraWorkspaceRef] = validation.WorkspaceRef
	account.Extra[ClaudePlatformAWSExtraWorkspaceBindingHMAC] = validation.WorkspaceBindingHMAC
	account.Extra[ClaudePlatformAWSExtraEndpointRef] = validation.EndpointRef
	account.Extra[ClaudePlatformAWSExtraRegion] = validation.Region
	return account
}

func cp6AWSEgressBucket(index int) string {
	return fmt.Sprintf("egress:cp6-aws-%d", index)
}

func requireCP6AWSMockEvidenceSafe(t *testing.T, evidence cp6AWSMockEvidence, account *Account) {
	t.Helper()
	require.Equal(t, "/v1/messages", evidence.Path)
	require.Empty(t, evidence.RawQuery)
	require.NotEmpty(t, evidence.Host)
	require.Equal(t, "aws-external-anthropic.us-east-1.api.aws", evidence.HostHeader)
	require.Equal(t, "us-east-1", evidence.Region)
	require.True(t, evidence.WorkspaceHeaderPresent)
	require.True(t, evidence.WorkspaceMatchesServerAccount)
	require.Equal(t, account.GetExtraString(ClaudePlatformAWSExtraWorkspaceRef), evidence.WorkspaceRef)
	require.False(t, evidence.ClientForgedWorkspaceSeen)
	require.True(t, evidence.XAPIKeyPresent)
	require.True(t, evidence.AuthMatchesServerCredential)
	require.False(t, evidence.ClientForgedAPIKeySeen)
	require.False(t, evidence.AuthorizationPresent)
	require.False(t, evidence.InternalHeaderPresent)
	require.False(t, evidence.ClientSpoofHeaderPresent)
	require.False(t, evidence.AnthropicBetaPresent)
	require.False(t, evidence.BillingCCHPresent)
	require.True(t, evidence.SafeEvidenceOnly)
	require.False(t, strings.Contains(evidence.BodyShapeSummary, account.GetCredential("anthropic_workspace_id")))
	require.False(t, strings.Contains(evidence.BodyShapeSummary, account.GetCredential("api_key")))
}

func cp6GatewayHops(upstream *jointGatewayRecordingUpstream) []recordingGatewayRequest {
	if upstream == nil {
		return nil
	}
	upstream.mu.Lock()
	defer upstream.mu.Unlock()
	out := append([]recordingGatewayRequest(nil), upstream.requests...)
	upstream.requests = nil
	return out
}

func cp6AWSAuthorityConfig() *config.Config {
	return &config.Config{Gateway: config.GatewayConfig{CCGateway: config.GatewayCCGatewayConfig{
		ContextAttestationSecret:                 jointGatewayContextAttestationSecret,
		StickySessionHMACKey:                     cp6AWSGatewayWorkspaceRefSecret,
		ClaudePlatformAWSWorkspaceBindingHMACKey: cp6AWSGatewayBindingSecret,
	}}}
}

func newCP6AWSClientContext(t *testing.T, sessionID string) (*gin.Context, context.Context, *httptest.ResponseRecorder) {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	ctx := context.Background()
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages?beta=true", nil).WithContext(ctx)
	c.Request.Header.Set("User-Agent", jointExpectedGatewayUserAgent)
	c.Request.Header.Set("Anthropic-Beta", "advisor-tool-2026-03-01,prompt-caching-scope-2026-01-05")
	c.Request.Header.Set("Anthropic-Workspace-Id", syntheticAWSWorkspaceID(12))
	c.Request.Header.Set("Authorization", "Bearer synthetic-client-forged-token")
	c.Request.Header.Set("X-Api-Key", "synthetic-client-forged-api-key")
	c.Request.Header.Set("X-Anthropic-Billing-Header", "synthetic-client-forged-cch")
	c.Request.Header.Set("X-CC-Account-Id", "account:client-forged")
	c.Request.Header.Set("X-Sub2API-Profile", "profile:client-forged")
	c.Request.Header.Set("X-Claude-Code-Session-Id", sessionID)
	return c, ctx, rec
}

type cp6AWSGatewayAccountConfig struct {
	account  *Account
	proxyURL string
}

func cp6AWSGatewayConfigYAML(t *testing.T, upstreamURL string, accounts []*cp6AWSGatewayAccountConfig) string {
	t.Helper()
	require.NotEmpty(t, accounts)
	identityYAML := strings.Builder{}
	egressYAML := strings.Builder{}
	for _, item := range accounts {
		account := item.account
		fmt.Fprintf(&identityYAML, `  "%s":
    device_id: "%s"
    account_uuid_hash: "scoped_hmac_ref:key_id=fixture;scope=account-ref;version=1;value=cp6"
    account_hash: "%s"
    credential_ref: "%s"
    credential_binding_hmac: "%s"
    token_type: apikey
    persona_variant: "%s"
    session_policy: preserve_downstream_session_id
    policy_version: "%s"
    provider_kind: claude_platform_aws
    upstream_auth_scheme: x_api_key
    aws_region: us-east-1
    upstream_host: aws-external-anthropic.us-east-1.api.aws
    allowed_upstream_path: /v1/messages
    upstream_endpoint_ref: "%s"
    workspace_ref: "%s"
    workspace_binding_hmac: "%s"
    beta_policy_ref: "%s"
    request_shape_profile_ref: "%s"
    cache_parity_profile_ref: "%s"
    anthropic_workspace_id: "%s"
`,
			account.GetExtraString(ccGatewayExtraAccountRef),
			strings.Repeat("c", 64),
			account.GetExtraString(ccGatewayExtraAccountRef),
			account.GetExtraString(ccGatewayExtraCredentialRef),
			account.GetExtraString(ccGatewayExtraCredentialBindingHMAC),
			jointExpectedGatewayPersonaVariant,
			ccGatewayAnthropicPolicyVersion,
			account.GetExtraString(ClaudePlatformAWSExtraEndpointRef),
			account.GetExtraString(ClaudePlatformAWSExtraWorkspaceRef),
			account.GetExtraString(ClaudePlatformAWSExtraWorkspaceBindingHMAC),
			account.GetExtraString(ClaudePlatformAWSExtraBetaPolicyRef),
			account.GetExtraString(ClaudePlatformAWSExtraRequestShapeProfileRef),
			account.GetExtraString(ClaudePlatformAWSExtraCacheParityProfileRef),
			account.GetCredential("anthropic_workspace_id"),
		)
		fmt.Fprintf(&egressYAML, `  %q:
    enabled: true
    proxy_url: %q
    proxy_identity_ref: "%s"
    allowed_account_ids: ["%s"]
`,
			account.GetExtraString(ccGatewayExtraEgressBucket),
			item.proxyURL,
			account.GetExtraString(ccGatewayExtraProxyIdentityRef),
			account.GetExtraString(ccGatewayExtraAccountRef),
		)
	}
	return fmt.Sprintf(`mode: sub2api
server:
  port: {{PORT}}
  tls:
    cert: ""
    key: ""
upstream:
  url: %q
providers:
  anthropic: true
auth:
  gateway_token: %q
  internal_control_token: %q
  tokens: []
identity:
  device_id: "%s"
  email: canonical@example.com
env:
  platform: darwin
  platform_raw: darwin
  arch: arm64
  node_version: v24.3.0
  terminal: iTerm2.app
  package_managers: npm,pnpm
  runtimes: node
  is_running_with_bun: false
  is_ci: false
  is_claude_ai_auth: true
  version: "%s"
  version_base: "%s"
  build_time: "2026-06-27T00:00:00Z"
  deployment_environment: unknown-darwin
  vcs: git
prompt_env:
  platform: darwin
  shell: zsh
  os_version: "Darwin 24.4.0"
  working_dir: "/Users/test/project"
process:
  constrained_memory: 34359738368
  rss_range: [300000000, 500000000]
  heap_total_range: [40000000, 80000000]
  heap_used_range: [100000000, 200000000]
shared_pool:
  context_attestation_secret_ref: "%s"
  context_attestation_secret: "%s"
  sticky_session_hmac_key: "%s"
  claude_platform_aws_workspace_binding_hmac_key: "%s"
  max_body_bytes: 2097152
  billing_cch_mode: strip
  message_beta_profile: claude_code_2_1_179_native_degraded
account_identities:
%segress_buckets:
%slogging:
  level: error
  audit: false
`, upstreamURL, jointGatewayToken, jointGatewayInternalControlToken, strings.Repeat("d", 64), ccGatewayAnthropicPolicyVersion, ccGatewayAnthropicPolicyVersion, jointGatewayContextAttestationRef, jointGatewayContextAttestationSecret, cp6AWSGatewayWorkspaceRefSecret, cp6AWSGatewayBindingSecret, identityYAML.String(), egressYAML.String())
}

type cp6AWSMockUpstream struct {
	server *httptest.Server
	seen   chan cp6AWSMockEvidence
}

type cp6AWSMockEvidence struct {
	Path                          string `json:"path"`
	RawQuery                      string `json:"raw_query"`
	Host                          string `json:"host"`
	HostHeader                    string `json:"host_header"`
	Region                        string `json:"region"`
	WorkspaceHeaderPresent        bool   `json:"workspace_header_present"`
	WorkspaceMatchesServerAccount bool   `json:"workspace_matches_server_account"`
	WorkspaceRef                  string `json:"workspace_ref"`
	ClientForgedWorkspaceSeen     bool   `json:"client_forged_workspace_seen"`
	XAPIKeyPresent                bool   `json:"x_api_key_present"`
	AuthMatchesServerCredential   bool   `json:"auth_matches_server_credential"`
	ClientForgedAPIKeySeen        bool   `json:"client_forged_api_key_seen"`
	AuthorizationPresent          bool   `json:"authorization_present"`
	InternalHeaderPresent         bool   `json:"internal_header_present"`
	ClientSpoofHeaderPresent      bool   `json:"client_spoof_header_present"`
	AnthropicBetaPresent          bool   `json:"anthropic_beta_present"`
	BillingCCHPresent             bool   `json:"billing_cch_present"`
	SafeEvidenceOnly              bool   `json:"safe_evidence_only"`
	BodyShapeSummary              string `json:"body_shape_summary"`
}

func startCP6AWSMockUpstream(t *testing.T, accounts ...*Account) *cp6AWSMockUpstream {
	t.Helper()
	workspaceToAccount := map[string]*Account{}
	apiKeyToAccount := map[string]*Account{}
	for _, account := range accounts {
		if account == nil {
			continue
		}
		workspaceToAccount[strings.TrimSpace(account.GetCredential("anthropic_workspace_id"))] = account
		apiKeyToAccount[strings.TrimSpace(account.GetCredential("api_key"))] = account
	}
	mock := &cp6AWSMockUpstream{seen: make(chan cp6AWSMockEvidence, 1)}
	mock.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 0)
		if r.Body != nil {
			defer r.Body.Close()
			limited := http.MaxBytesReader(w, r.Body, 1<<20)
			body, _ = io.ReadAll(limited)
		}
		hostHeader := strings.TrimSpace(r.Host)
		workspaceHeader := strings.TrimSpace(r.Header.Get("anthropic-workspace-id"))
		apiKeyHeader := strings.TrimSpace(r.Header.Get("x-api-key"))
		workspaceAccount := workspaceToAccount[workspaceHeader]
		apiKeyAccount := apiKeyToAccount[apiKeyHeader]
		workspaceRef := ""
		if workspaceAccount != nil {
			workspaceRef = workspaceAccount.GetExtraString(ClaudePlatformAWSExtraWorkspaceRef)
		}
		evidence := cp6AWSMockEvidence{
			Path:                          r.URL.EscapedPath(),
			RawQuery:                      r.URL.RawQuery,
			Host:                          r.Host,
			HostHeader:                    hostHeader,
			Region:                        cp6AWSRegionFromHostHeader(hostHeader),
			WorkspaceHeaderPresent:        workspaceHeader != "",
			WorkspaceMatchesServerAccount: workspaceAccount != nil && workspaceAccount == apiKeyAccount,
			WorkspaceRef:                  workspaceRef,
			ClientForgedWorkspaceSeen:     workspaceHeader == syntheticAWSWorkspaceID(12),
			XAPIKeyPresent:                apiKeyHeader != "",
			AuthMatchesServerCredential:   apiKeyAccount != nil && workspaceAccount == apiKeyAccount,
			ClientForgedAPIKeySeen:        apiKeyHeader == "synthetic-client-forged-api-key",
			AuthorizationPresent:          strings.TrimSpace(r.Header.Get("authorization")) != "",
			AnthropicBetaPresent:          strings.TrimSpace(r.Header.Get("anthropic-beta")) != "",
			BillingCCHPresent:             strings.Contains(strings.ToLower(string(body)), "cch=") || strings.TrimSpace(r.Header.Get("x-anthropic-billing-header")) != "",
			BodyShapeSummary:              cp6AWSBodyShapeSummary(body),
		}
		for key := range r.Header {
			lower := strings.ToLower(key)
			if strings.HasPrefix(lower, "x-sub2api-") || strings.HasPrefix(lower, "x-cc-") {
				evidence.InternalHeaderPresent = true
			}
			if strings.Contains(lower, "client-forged") || lower == "x-profile" || lower == "x-session" {
				evidence.ClientSpoofHeaderPresent = true
			}
		}
		evidence.SafeEvidenceOnly = !strings.Contains(evidence.BodyShapeSummary, "fixture-api-key") &&
			!strings.Contains(evidence.BodyShapeSummary, "workspace-id") &&
			!strings.Contains(evidence.BodyShapeSummary, "raw")
		select {
		case mock.seen <- evidence:
		default:
		}
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "event: message_start\n")
		_, _ = fmt.Fprint(w, `data: {"type":"message_start","message":{"id":"msg_cp6","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-6","usage":{"input_tokens":1,"output_tokens":0}}}`+"\n\n")
		_, _ = fmt.Fprint(w, "event: message_delta\n")
		_, _ = fmt.Fprint(w, `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`+"\n\n")
		_, _ = fmt.Fprint(w, "event: message_stop\n")
		_, _ = fmt.Fprint(w, `data: {"type":"message_stop"}`+"\n\n")
	}))
	t.Cleanup(mock.server.Close)
	return mock
}

func (m *cp6AWSMockUpstream) URL() string {
	return m.server.URL
}

func (m *cp6AWSMockUpstream) popSingle(t *testing.T) cp6AWSMockEvidence {
	t.Helper()
	select {
	case got := <-m.seen:
		return got
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for CP6 AWS mock upstream request")
		return cp6AWSMockEvidence{}
	}
}

func cp6ProxySawMockUpstream(proxy *connectProxyServer) bool {
	if proxy == nil {
		return false
	}
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	for _, target := range proxy.targets {
		if strings.HasPrefix(target, "127.0.0.1:") {
			return true
		}
	}
	return false
}

func cp6AWSRegionFromHostHeader(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	const prefix = "aws-external-anthropic."
	const suffix = ".api.aws"
	if !strings.HasPrefix(host, prefix) || !strings.HasSuffix(host, suffix) {
		return ""
	}
	return strings.TrimSuffix(strings.TrimPrefix(host, prefix), suffix)
}

func cp6AWSBodyShapeSummary(body []byte) string {
	return fmt.Sprintf("model=%s;messages=%d;context_management=%t;anthropic_beta=%t;len=%s",
		gjson.GetBytes(body, "model").String(),
		len(gjson.GetBytes(body, "messages").Array()),
		gjson.GetBytes(body, "context_management").Exists(),
		gjson.GetBytes(body, "anthropic_beta").Exists(),
		strconv.Itoa(len(body)),
	)
}

func TestCP6AWSMockEvidenceFlagsAllInternalHeaders(t *testing.T) {
	mock := startCP6AWSMockUpstream(t)
	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequest(http.MethodPost, mock.URL()+"/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4-6","messages":[]}`))
	require.NoError(t, err)
	req.Host = "aws-external-anthropic.us-east-1.api.aws"
	req.Header.Set("x-cc-cp6-workspace-ref", "workspace:safe-only")
	resp, err := client.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()

	evidence := mock.popSingle(t)
	require.True(t, evidence.InternalHeaderPresent)
}
