//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type openAIGatewayCoreRepoStub struct {
	mockAccountRepoForGemini
	updateExtraCalls int
	lastExtra        map[string]any
}

func (r *openAIGatewayCoreRepoStub) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	r.updateExtraCalls++
	r.lastExtra = cloneCredentials(updates)
	if r.accountsByID != nil {
		if acc, ok := r.accountsByID[id]; ok && acc != nil {
			acc.Extra = mergeMap(acc.Extra, updates)
		}
	}
	return nil
}

func (r *openAIGatewayCoreRepoStub) ListByPlatform(ctx context.Context, platform string) ([]Account, error) {
	if platform == PlatformOpenAI {
		return append([]Account(nil), r.accounts...), nil
	}
	return nil, nil
}

func TestOpenAIGatewayCoreService_AuthenticateClientHeaders(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.ClientTokens = []config.OpenAIGatewayClientTokenConfig{
		{Name: "probe", Token: "tok-123"},
	}

	svc := NewOpenAIGatewayCoreService(&openAIGatewayCoreRepoStub{}, cfg, nil)

	headers := http.Header{}
	headers.Set(OpenAIGatewayClientTokenHeader, "tok-123")
	client, err := svc.AuthenticateClientHeaders(headers)
	require.NoError(t, err)
	require.NotNil(t, client)
	require.True(t, client.Authenticated)
	require.Equal(t, "probe", client.Name)

	headers.Set(OpenAIGatewayClientTokenHeader, "bad")
	client, err = svc.AuthenticateClientHeaders(headers)
	require.Error(t, err)
	require.Nil(t, client)
}

func TestOpenAIGatewayCoreService_ResolveAccountRuntime_StablePerAccount(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.DefaultProfileMode = OpenAIGatewayProfileModeFixed
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"

	repo := &openAIGatewayCoreRepoStub{
		mockAccountRepoForGemini: mockAccountRepoForGemini{
			accountsByID: map[int64]*Account{
				1: {ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Credentials: map[string]any{"chatgpt_account_id": "acct-1"}},
				2: {ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Credentials: map[string]any{"chatgpt_account_id": "acct-2"}},
			},
		},
	}
	svc := NewOpenAIGatewayCoreService(repo, cfg, nil)

	headers := http.Header{}
	headers.Set("User-Agent", "codex_cli_rs/9.9.9")
	headers.Set("X-Stainless-Lang", "python")

	runtime1, err := svc.ResolveAccountRuntime(context.Background(), repo.accountsByID[1], headers, OpenAIClientTransportHTTP)
	require.NoError(t, err)
	require.NotNil(t, runtime1)
	require.Equal(t, "default", runtime1.EgressBucket)
	require.NotEmpty(t, runtime1.Profile.ProfileID)
	require.Equal(t, OpenAIGatewayProfileModeFixed, runtime1.Profile.Mode)

	runtime1Again, err := svc.ResolveAccountRuntime(context.Background(), repo.accountsByID[1], http.Header{"User-Agent": []string{"totally-different/1.0"}}, OpenAIClientTransportHTTP)
	require.NoError(t, err)
	require.Equal(t, runtime1.Profile.ProfileID, runtime1Again.Profile.ProfileID)
	require.Equal(t, runtime1.Profile.UserAgent, runtime1Again.Profile.UserAgent)

	runtime2, err := svc.ResolveAccountRuntime(context.Background(), repo.accountsByID[2], headers, OpenAIClientTransportHTTP)
	require.NoError(t, err)
	require.NotEqual(t, runtime1.Profile.ProfileID, runtime2.Profile.ProfileID)
	require.GreaterOrEqual(t, repo.updateExtraCalls, 2)
}

func TestOpenAIGatewayCoreService_ResolveEgressBucket(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{Name: "default", Enabled: true},
		{Name: "bucket-a", Enabled: true, ProxyURL: "socks5://127.0.0.1:9001"},
	}
	svc := NewOpenAIGatewayCoreService(&openAIGatewayCoreRepoStub{}, cfg, nil)

	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	require.Equal(t, "default", svc.ResolveEgressBucket(account))
	egress, err := svc.ResolveEgress(context.Background(), account, "")
	require.NoError(t, err)
	require.Empty(t, egress.ProxyURL)

	account.Extra = map[string]any{"openai_gateway_egress_bucket": "bucket-a"}
	require.Equal(t, "bucket-a", svc.ResolveEgressBucket(account))
	egress, err = svc.ResolveEgress(context.Background(), account, "")
	require.NoError(t, err)
	require.Equal(t, "socks5://127.0.0.1:9001", egress.ProxyURL)

	account.Extra["openai_gateway_egress_bucket"] = "missing"
	egress, err = svc.ResolveEgress(context.Background(), account, "")
	require.NoError(t, err)
	require.Empty(t, egress.ProxyURL)

	cfg.Gateway.OpenAICore.EgressBuckets = append(cfg.Gateway.OpenAICore.EgressBuckets, config.OpenAIGatewayEgressBucketConfig{
		Name: "bucket-disabled", Enabled: false, ProxyURL: "http://127.0.0.1:8080",
	})
	account.Extra["openai_gateway_egress_bucket"] = "bucket-disabled"
	egress, err = svc.ResolveEgress(context.Background(), account, "fallback-proxy")
	require.NoError(t, err)
	require.Equal(t, "fallback-proxy", egress.ProxyURL)
}

func TestOpenAIGatewayCoreService_BuildHealthSnapshot(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"

	repo := &openAIGatewayCoreRepoStub{
		mockAccountRepoForGemini: mockAccountRepoForGemini{
			accounts: []Account{
				{
					ID:       1,
					Platform: PlatformOpenAI,
					Type:     AccountTypeOAuth,
					Status:   StatusActive,
					Extra: map[string]any{
						"openai_gateway_egress_bucket": "default",
						"openai_token_source":          OpenAITokenSourceRTManaged,
						"openai_auth_state":            OpenAIAuthStateHealthy,
					},
				},
				{
					ID:       2,
					Platform: PlatformOpenAI,
					Type:     AccountTypeOAuth,
					Status:   StatusError,
					Extra: map[string]any{
						"openai_gateway_egress_bucket": "default",
						"openai_token_source":          OpenAITokenSourceRTManaged,
						"openai_auth_state":            OpenAIAuthStateTerminal,
					},
				},
			},
		},
	}
	provider := &OpenAITokenProvider{}
	provider.metrics = &openAITokenRuntimeMetricsStore{}
	provider.metrics.refreshFailure.Store(1)
	provider.metrics.lastObservedUnixMs.Store(time.Now().UnixMilli())

	svc := NewOpenAIGatewayCoreService(repo, cfg, provider)
	health, err := svc.BuildHealthSnapshot(context.Background(), OpenAIWSPerformanceMetricsSnapshot{})
	require.NoError(t, err)
	require.Equal(t, "degraded", health.GatewayStatus)
	require.Equal(t, "degraded", health.OAuthStatus)
	require.Equal(t, int64(2), health.OpenAIOAuthAccountsTotal)
	require.Equal(t, int64(1), health.TerminalAccountsTotal)
	require.Equal(t, int64(2), health.EgressBuckets["default"])
	require.NotEmpty(t, health.DegradedReason)
}

func TestOpenAIGatewayService_ResolveOpenAIEgressUsesBucketProxy(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{Name: "default", Enabled: true},
		{Name: "bucket-a", Enabled: true, ProxyURL: "http://127.0.0.1:8080"},
	}
	core := NewOpenAIGatewayCoreService(&openAIGatewayCoreRepoStub{}, cfg, nil)
	svc := &OpenAIGatewayService{cfg: cfg, gatewayCoreService: core}

	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"openai_gateway_egress_bucket": "bucket-a",
		},
		Proxy: &Proxy{
			Protocol: "socks5",
			Host:     "10.0.0.2",
			Port:     1080,
		},
	}

	egress, err := svc.resolveOpenAIEgress(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:8080", egress.ProxyURL)
}

func TestOpenAIGatewayService_ResolveOpenAIEgressPropagatesFailClosedPolicyError(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.EgressFailClosed = true
	cfg.Gateway.OpenAICore.AllowAccountProxyFallback = false
	cfg.Gateway.OpenAICore.AllowDirectFallback = false
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{Name: "default", Enabled: true},
	}
	core := NewOpenAIGatewayCoreService(&openAIGatewayCoreRepoStub{}, cfg, nil)
	svc := &OpenAIGatewayService{cfg: cfg, gatewayCoreService: core}
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Extra: map[string]any{
			"openai_gateway_egress_bucket": "missing",
		},
		Proxy: &Proxy{
			Protocol: "socks5",
			Host:     "10.0.0.2",
			Port:     1080,
		},
	}

	resolution, err := svc.resolveOpenAIEgress(context.Background(), account)
	require.Nil(t, resolution)
	require.Error(t, err)
	var policyErr *OpenAIEgressPolicyError
	require.ErrorAs(t, err, &policyErr)
	require.Equal(t, "missing_bucket", policyErr.Code)
}

func TestOpenAIGatewayCoreService_ResolveAccountRuntimeAllowsAccountProxyFallback(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.EgressFailClosed = false
	cfg.Gateway.OpenAICore.AllowAccountProxyFallback = true
	cfg.Gateway.OpenAICore.AllowDirectFallback = false
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{Name: "default", Enabled: true},
	}
	svc := NewOpenAIGatewayCoreService(&openAIGatewayCoreRepoStub{}, cfg, nil)
	account := &Account{
		ID:       9301,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"openai_gateway_egress_bucket": "missing",
		},
		Proxy: &Proxy{
			Protocol: "socks5",
			Host:     "10.0.0.2",
			Port:     1080,
		},
	}

	runtime, err := svc.ResolveAccountRuntime(context.Background(), account, http.Header{}, OpenAIClientTransportWS)
	require.NoError(t, err)
	require.NotNil(t, runtime)
	require.Equal(t, "missing", runtime.EgressBucket)
	require.True(t, runtime.ProxySelected)
	require.Equal(t, "socks5h://10.0.0.2:1080", runtime.ProxyLabel)
}

func TestOpenAIGatewayCoreService_BuildAdminStatusSnapshot(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{Name: "default", Enabled: true},
		{Name: "bucket-a", Enabled: true, ProxyURL: "http://127.0.0.1:8080"},
	}

	repo := &openAIGatewayCoreRepoStub{
		mockAccountRepoForGemini: mockAccountRepoForGemini{
			accounts: []Account{
				{
					ID:       1,
					Name:     "acc-1",
					Platform: PlatformOpenAI,
					Type:     AccountTypeOAuth,
					Status:   StatusActive,
					Extra: map[string]any{
						"openai_gateway_profile_id":       "profile-1",
						"openai_gateway_profile_mode":     OpenAIGatewayProfileModeFixed,
						"openai_gateway_egress_bucket":    "bucket-a",
						"openai_gateway_last_verified_at": "2026-04-17T00:00:00Z",
						"openai_gateway_client_family":    openAIGatewayClientFamilyCodexOfficial,
						"openai_auth_state":               OpenAIAuthStateHealthy,
						"openai_pool_role":                OpenAIPoolRoleMain,
						"openai_token_source":             OpenAITokenSourceRTManaged,
						"openai_last_refresh_error_code":  "",
						"openai_last_validated_at":        "2026-04-17T00:00:00Z",
						"openai_last_granted_scope":       "openid email profile api.responses.write",
						"openai_last_access_token_scopes": []string{"openid", "email", "profile", "api.responses.write"},
						"openai_responses_write_capable":  true,
					},
				},
			},
		},
	}
	svc := NewOpenAIGatewayCoreService(repo, cfg, nil)

	snapshot, err := svc.BuildAdminStatusSnapshot(context.Background(), OpenAIWSPerformanceMetricsSnapshot{})
	require.NoError(t, err)
	require.NotNil(t, snapshot.Health)
	require.Len(t, snapshot.Buckets, 2)
	require.Len(t, snapshot.Accounts, 1)
	require.Equal(t, int64(1), snapshot.Accounts[0].AccountID)
	require.Equal(t, "bucket-a", snapshot.Accounts[0].EgressBucket)
	require.Equal(t, "profile-1", snapshot.Accounts[0].ProfileID)
	require.Equal(t, "openid email profile api.responses.write", snapshot.Accounts[0].LastGrantedScope)
	require.Equal(t, []string{"openid", "email", "profile", "api.responses.write"}, snapshot.Accounts[0].LastAccessTokenScopes)
	require.True(t, snapshot.Accounts[0].ResponsesWriteCapable)
}

func TestOpenAIGatewayCoreService_BuildAdminStatusSnapshotRedactsProxyURL(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "bucket-a"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{Name: "bucket-a", Enabled: true, ProxyURL: "http://user:pass@127.0.0.1:8080/path?q=1"},
	}

	repo := &openAIGatewayCoreRepoStub{
		mockAccountRepoForGemini: mockAccountRepoForGemini{
			accounts: []Account{
				{
					ID:       1,
					Name:     "acc-1",
					Platform: PlatformOpenAI,
					Type:     AccountTypeOAuth,
					Status:   StatusActive,
					Extra: map[string]any{
						"openai_gateway_egress_bucket": "bucket-a",
					},
				},
			},
		},
	}

	svc := NewOpenAIGatewayCoreService(repo, cfg, nil)
	snapshot, err := svc.BuildAdminStatusSnapshot(context.Background(), OpenAIWSPerformanceMetricsSnapshot{})
	require.NoError(t, err)

	payload, err := json.Marshal(snapshot)
	require.NoError(t, err)
	body := string(payload)
	require.Contains(t, body, "\"proxy_label\":\"http://127.0.0.1:8080\"")
	require.Contains(t, body, "\"proxy_hash\":\"")
	require.NotContains(t, body, "\"proxy_url\"")
	require.NotContains(t, body, "user:pass")
	require.NotContains(t, body, "q=1")
}

func TestOpenAIGatewayRedactSanitizeUpstreamErrorMessage(t *testing.T) {
	raw := "proxy=http://user:pass@proxy.example.com:8080/path?access_token=tok123&refresh_token=tok456 Authorization: Bearer sk-abcdefghijklmnopqrstuvwxyz123456 api_key=sk-abcdef1234567890"
	redacted := sanitizeUpstreamErrorMessage(raw)

	require.Contains(t, redacted, "proxy.example.com:8080")
	require.Contains(t, redacted, "Bearer ***")
	require.Contains(t, redacted, "api_key=***")
	require.NotContains(t, redacted, "user:pass")
	require.NotContains(t, redacted, "tok123")
	require.NotContains(t, redacted, "tok456")
	require.NotContains(t, redacted, "sk-abcdefghijklmnopqrstuvwxyz123456")
	require.NotContains(t, redacted, "path?")
}
