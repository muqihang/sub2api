package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type openAIGatewayCoreAccountRepoStubBase struct {
	accounts     []Account
	accountsByID map[int64]*Account
}

func (m *openAIGatewayCoreAccountRepoStubBase) Create(ctx context.Context, account *Account) error {
	return nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) GetByID(ctx context.Context, id int64) (*Account, error) {
	if m.accountsByID != nil {
		if acc, ok := m.accountsByID[id]; ok {
			return acc, nil
		}
	}
	return nil, errors.New("account not found")
}
func (m *openAIGatewayCoreAccountRepoStubBase) GetByIDs(ctx context.Context, ids []int64) ([]*Account, error) {
	var result []*Account
	for _, id := range ids {
		if m.accountsByID != nil {
			if acc, ok := m.accountsByID[id]; ok {
				result = append(result, acc)
			}
		}
	}
	return result, nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) ExistsByID(ctx context.Context, id int64) (bool, error) {
	if m.accountsByID == nil {
		return false, nil
	}
	_, ok := m.accountsByID[id]
	return ok, nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) GetByCRSAccountID(ctx context.Context, crsAccountID string) (*Account, error) {
	return nil, nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) FindByExtraField(ctx context.Context, key string, value any) ([]Account, error) {
	return nil, nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) ListCRSAccountIDs(ctx context.Context) (map[string]int64, error) {
	return nil, nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) Update(ctx context.Context, account *Account) error {
	return nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) Delete(ctx context.Context, id int64) error {
	return nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) List(ctx context.Context, params pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) ListWithFilters(ctx context.Context, params pagination.PaginationParams, platform, accountType, status, search string, groupID int64, privacyMode string) ([]Account, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) ListByGroup(ctx context.Context, groupID int64) ([]Account, error) {
	return nil, nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) ListActive(ctx context.Context) ([]Account, error) {
	return nil, nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) ListByPlatform(ctx context.Context, platform string) ([]Account, error) {
	return nil, nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) UpdateLastUsed(ctx context.Context, id int64) error {
	return nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) BatchUpdateLastUsed(ctx context.Context, updates map[int64]time.Time) error {
	return nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) SetError(ctx context.Context, id int64, errorMsg string) error {
	return nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) ClearError(ctx context.Context, id int64) error {
	return nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) SetSchedulable(ctx context.Context, id int64, schedulable bool) error {
	return nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) AutoPauseExpiredAccounts(ctx context.Context, now time.Time) (int64, error) {
	return 0, nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) BindGroups(ctx context.Context, accountID int64, groupIDs []int64) error {
	return nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) ListSchedulable(ctx context.Context) ([]Account, error) {
	return nil, nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) ListSchedulableByGroupID(ctx context.Context, groupID int64) ([]Account, error) {
	return nil, nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) ListSchedulableByPlatform(ctx context.Context, platform string) ([]Account, error) {
	var result []Account
	for _, acc := range m.accounts {
		if acc.Platform == platform && acc.IsSchedulable() {
			result = append(result, acc)
		}
	}
	return result, nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) ListSchedulableByGroupIDAndPlatform(ctx context.Context, groupID int64, platform string) ([]Account, error) {
	return m.ListSchedulableByPlatform(ctx, platform)
}
func (m *openAIGatewayCoreAccountRepoStubBase) ListSchedulableByPlatforms(ctx context.Context, platforms []string) ([]Account, error) {
	platformSet := make(map[string]bool, len(platforms))
	for _, platform := range platforms {
		platformSet[platform] = true
	}
	var result []Account
	for _, acc := range m.accounts {
		if platformSet[acc.Platform] && acc.IsSchedulable() {
			result = append(result, acc)
		}
	}
	return result, nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) ListSchedulableByGroupIDAndPlatforms(ctx context.Context, groupID int64, platforms []string) ([]Account, error) {
	return m.ListSchedulableByPlatforms(ctx, platforms)
}
func (m *openAIGatewayCoreAccountRepoStubBase) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]Account, error) {
	return m.ListSchedulableByPlatform(ctx, platform)
}
func (m *openAIGatewayCoreAccountRepoStubBase) ListSchedulableUngroupedByPlatforms(ctx context.Context, platforms []string) ([]Account, error) {
	return m.ListSchedulableByPlatforms(ctx, platforms)
}
func (m *openAIGatewayCoreAccountRepoStubBase) SetRateLimited(ctx context.Context, id int64, resetAt time.Time) error {
	return nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) SetModelRateLimit(ctx context.Context, id int64, scope string, resetAt time.Time, reason ...string) error {
	return nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) SetOverloaded(ctx context.Context, id int64, until time.Time) error {
	return nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) SetTempUnschedulable(ctx context.Context, id int64, until time.Time, reason string) error {
	return nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) ClearTempUnschedulable(ctx context.Context, id int64) error {
	return nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) ClearRateLimit(ctx context.Context, id int64) error {
	return nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) ClearAntigravityQuotaScopes(ctx context.Context, id int64) error {
	return nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) ClearModelRateLimits(ctx context.Context, id int64) error {
	return nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) UpdateSessionWindow(ctx context.Context, id int64, start, end *time.Time, status string) error {
	return nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) UpdateSessionWindowEnd(ctx context.Context, id int64, end time.Time) error {
	return nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	return nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) BulkUpdate(ctx context.Context, ids []int64, updates AccountBulkUpdate) (int64, error) {
	return 0, nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) IncrementQuotaUsed(ctx context.Context, id int64, amount float64) error {
	return nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) ResetQuotaUsed(ctx context.Context, id int64) error {
	return nil
}
func (m *openAIGatewayCoreAccountRepoStubBase) RevertProxyFallback(ctx context.Context, accountID int64) error {
	return nil
}

type openAIGatewayCoreRepoStub struct {
	openAIGatewayCoreAccountRepoStubBase
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

func testOpenAITLSProfileService(profiles ...*model.TLSFingerprintProfile) *TLSFingerprintProfileService {
	svc := &TLSFingerprintProfileService{
		localCache: map[int64]*model.TLSFingerprintProfile{},
	}
	for _, profile := range profiles {
		if profile != nil {
			svc.localCache[profile.ID] = profile
		}
	}
	return svc
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
		openAIGatewayCoreAccountRepoStubBase: openAIGatewayCoreAccountRepoStubBase{
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

func TestOpenAIGatewayCoreService_TLSEntityFlagsDisabledPreserveRuntime(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.DefaultProfileMode = OpenAIGatewayProfileModeFixed
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.TLSBinding.Enabled = false
	cfg.Gateway.OpenAICore.EntityOrchestration.Enabled = false
	cfg.Gateway.OpenAICore.EntityProfileOverride.Enabled = false
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{
			Name:    "default",
			Enabled: true,
			TLS: config.OpenAIGatewayBucketTLSConfig{
				Enabled:   true,
				ProfileID: 999,
			},
		},
	}

	repo := &openAIGatewayCoreRepoStub{
		openAIGatewayCoreAccountRepoStubBase: openAIGatewayCoreAccountRepoStubBase{
			accountsByID: map[int64]*Account{
				1: {
					ID:          1,
					Platform:    PlatformOpenAI,
					Type:        AccountTypeOAuth,
					Status:      StatusActive,
					Credentials: map[string]any{"chatgpt_account_id": "acct-1"},
					Extra: map[string]any{
						"openai_gateway_tls": map[string]any{
							"enabled":    true,
							"profile_id": 123,
						},
						"openai_gateway_entity_id": "entity-ignored",
					},
				},
			},
		},
	}
	svc := NewOpenAIGatewayCoreService(repo, cfg, nil)

	runtime, err := svc.ResolveAccountRuntime(context.Background(), repo.accountsByID[1], http.Header{}, OpenAIClientTransportHTTP)
	require.NoError(t, err)
	require.Nil(t, runtime.TLS)
	require.Equal(t, "default", runtime.EgressBucket)
	require.NotEmpty(t, runtime.Profile.ProfileID)
}

func TestOpenAIGatewayCoreService_ResolveAccountRuntimeObserveModeAlignsVersion(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.DefaultProfileMode = OpenAIGatewayProfileModeObserve
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"

	repo := &openAIGatewayCoreRepoStub{
		openAIGatewayCoreAccountRepoStubBase: openAIGatewayCoreAccountRepoStubBase{
			accountsByID: map[int64]*Account{
				1: {
					ID:       1,
					Platform: PlatformOpenAI,
					Type:     AccountTypeOAuth,
					Status:   StatusActive,
					Credentials: map[string]any{
						"chatgpt_account_id": "acct-1",
					},
				},
			},
		},
	}
	svc := NewOpenAIGatewayCoreService(repo, cfg, nil)

	headers := http.Header{}
	headers.Set("User-Agent", "codex_cli_rs/0.200.0")
	runtime, err := svc.ResolveAccountRuntime(context.Background(), repo.accountsByID[1], headers, OpenAIClientTransportHTTP)
	require.NoError(t, err)
	require.NotNil(t, runtime)
	require.Equal(t, "codex_cli_rs/0.200.0", runtime.Profile.UserAgent)
	require.Equal(t, "0.200.0", runtime.Profile.Version)
	require.Equal(t, "0.200.0", repo.accountsByID[1].GetExtraString("openai_gateway_canonical_version"))
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

func TestOpenAIGatewayCoreService_ValidateTLSProfilesAcceptsConfiguredProfile(t *testing.T) {
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
	svc := NewOpenAIGatewayCoreService(&openAIGatewayCoreRepoStub{}, cfg, nil, testOpenAITLSProfileService(&model.TLSFingerprintProfile{
		ID:   7,
		Name: "Chrome 124",
	}))

	require.NoError(t, svc.ValidateConfiguredTLSProfiles(context.Background()))
}

func TestOpenAIGatewayCoreService_ValidateTLSProfilesRejectsMissingProfile(t *testing.T) {
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
				ProfileID: 404,
			},
		},
	}
	svc := NewOpenAIGatewayCoreService(&openAIGatewayCoreRepoStub{}, cfg, nil, testOpenAITLSProfileService())

	err := svc.ValidateConfiguredTLSProfiles(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "gateway.openai_core.egress_buckets[default].tls.profile_id 404 not found")
}

func TestOpenAIGatewayCoreService_ValidateTLSProfilesSkippedWhenBindingDisabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.TLSBinding.Enabled = false
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{
			Name:    "default",
			Enabled: true,
			TLS: config.OpenAIGatewayBucketTLSConfig{
				Enabled:   true,
				ProfileID: 404,
			},
		},
	}
	svc := NewOpenAIGatewayCoreService(&openAIGatewayCoreRepoStub{}, cfg, nil, testOpenAITLSProfileService())

	require.NoError(t, svc.ValidateConfiguredTLSProfiles(context.Background()))
}

func TestOpenAIGatewayCoreService_ResolveAccountRuntimeTLSSelectsBucketProfile(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.TLSBinding.Enabled = true
	cfg.Gateway.OpenAICore.DefaultProfileMode = OpenAIGatewayProfileModeFixed
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
	svc := NewOpenAIGatewayCoreService(&openAIGatewayCoreRepoStub{}, cfg, nil, testOpenAITLSProfileService(&model.TLSFingerprintProfile{
		ID:   7,
		Name: "Chrome 124",
	}))

	runtime, err := svc.ResolveAccountRuntime(context.Background(), &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}, http.Header{}, OpenAIClientTransportHTTP)
	require.NoError(t, err)
	require.NotNil(t, runtime.TLS)
	require.True(t, runtime.TLS.Enabled)
	require.Equal(t, int64(7), runtime.TLS.ProfileID)
	require.Equal(t, "Chrome 124", runtime.TLS.ProfileName)
	require.Equal(t, "bucket", runtime.TLS.Source)
	require.NotEmpty(t, runtime.TLS.ProfileHash)
	require.Contains(t, runtime.TLS.CacheIdentity, "bucket=default")
	require.Contains(t, runtime.TLS.CacheIdentity, "profile_hash="+runtime.TLS.ProfileHash)
	require.True(t, runtime.TLS.HTTPApplicable)
	require.True(t, runtime.TLS.WSApplicable)
}

func TestOpenAIGatewayCoreService_ResolveAccountRuntimeWSFlagsUseActualDialerStrategy(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.TLSBinding.Enabled = true
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{
			Name:     "default",
			Enabled:  true,
			ProxyURL: "https://127.0.0.1:8443",
			TLS: config.OpenAIGatewayBucketTLSConfig{
				Enabled:   true,
				ProfileID: 7,
			},
		},
	}
	svc := NewOpenAIGatewayCoreService(&openAIGatewayCoreRepoStub{}, cfg, nil, testOpenAITLSProfileService(&model.TLSFingerprintProfile{
		ID:   7,
		Name: "Chrome 124",
	}))

	runtime, err := svc.ResolveAccountRuntime(context.Background(), &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}, http.Header{}, OpenAIClientTransportWS)
	require.NoError(t, err)
	require.NotNil(t, runtime.TLS)
	require.True(t, runtime.TLS.Enabled)
	require.False(t, runtime.TLS.WSApplicable, "WSApplicable must reflect actual TLS-bound WS strategy support")
	require.Equal(t, "https_proxy_unsupported_for_tls_bound_ws", runtime.TLS.FallbackReason)
}

func TestOpenAIGatewayCoreService_BuildTLSCanarySnapshotWSIncludesStrategyDiagnostics(t *testing.T) {
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
	repo := &openAIGatewayCoreRepoStub{
		openAIGatewayCoreAccountRepoStubBase: openAIGatewayCoreAccountRepoStubBase{
			accountsByID: map[int64]*Account{
				1: {
					ID:       1,
					Name:     "oauth",
					Platform: PlatformOpenAI,
					Type:     AccountTypeOAuth,
					Credentials: map[string]any{
						"chatgpt_account_id": "acct_123",
					},
				},
			},
		},
	}
	svc := NewOpenAIGatewayCoreService(repo, cfg, nil, testOpenAITLSProfileService(&model.TLSFingerprintProfile{
		ID:   7,
		Name: "Chrome 124",
	}))

	snapshot, err := svc.BuildTLSCanarySnapshot(context.Background(), 1, "default", "/v1/realtime", http.Header{}, OpenAIClientTransportWS)
	require.NoError(t, err)
	require.Equal(t, "ws", snapshot.Transport)
	require.Equal(t, "WSCoderCustomHTTPClient", snapshot.EffectiveSendMethod)
	require.Equal(t, "coder_custom_http_client", snapshot.Diagnostics["ws_dialer_strategy"])
	require.Equal(t, "true", snapshot.Diagnostics["ws_transport_supported"])
	require.Empty(t, snapshot.Diagnostics["ws_transport_unsupported_reason"])
	require.True(t, snapshot.TLS.WSApplicable)
}

func TestOpenAIGatewayService_RunTLSCanaryProbeUsesCanaryOnlyTLSProfileOverride(t *testing.T) {
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
	account := &Account{
		ID:          1,
		Name:        "api",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-test"},
	}
	repo := &openAIGatewayCoreRepoStub{
		openAIGatewayCoreAccountRepoStubBase: openAIGatewayCoreAccountRepoStubBase{
			accountsByID: map[int64]*Account{1: account},
		},
	}
	core := NewOpenAIGatewayCoreService(repo, cfg, nil, testOpenAITLSProfileService(
		&model.TLSFingerprintProfile{ID: 7, Name: "Bucket TLS"},
		&model.TLSFingerprintProfile{ID: 9, Name: "Canary TLS"},
	))
	upstream := &openAIHTTPSenderUpstreamRecorder{}
	svc := &OpenAIGatewayService{
		accountRepo:         repo,
		cfg:                 cfg,
		httpUpstream:        upstream,
		gatewayCoreService:  core,
		openAITokenProvider: nil,
	}

	snapshot, err := svc.RunOpenAITLSCanaryProbe(context.Background(), OpenAIGatewayTLSCanaryProbeInput{
		AccountID:    1,
		Bucket:       "default",
		TLSProfileID: 9,
		Transport:    OpenAIClientTransportHTTP,
		Route:        "/v1/responses",
	})
	require.NoError(t, err)
	require.True(t, snapshot.Success)
	require.NotNil(t, snapshot.Probe)
	require.Equal(t, http.StatusOK, snapshot.Probe.HTTPStatus)
	require.Equal(t, int64(9), snapshot.TLS.ProfileID)
	require.Equal(t, "Canary TLS", snapshot.TLS.ProfileName)
	require.Equal(t, "Canary TLS", upstream.lastProfile.Name)
	require.Nil(t, account.Extra[OpenAIGatewayTLSExtraKey], "canary profile override must not persist to account extra")
}

func TestOpenAIGatewayCoreService_ResolveAccountRuntimeTLSAccountOverrideOnlyWhenAllowed(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.TLSBinding.Enabled = true
	cfg.Gateway.OpenAICore.DefaultProfileMode = OpenAIGatewayProfileModeFixed
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{
			Name:    "default",
			Enabled: true,
			TLS: config.OpenAIGatewayBucketTLSConfig{
				Enabled:              true,
				ProfileID:            7,
				AllowAccountOverride: false,
			},
		},
	}
	svc := NewOpenAIGatewayCoreService(&openAIGatewayCoreRepoStub{}, cfg, nil, testOpenAITLSProfileService(
		&model.TLSFingerprintProfile{ID: 7, Name: "Bucket"},
		&model.TLSFingerprintProfile{ID: 9, Name: "Account"},
	))
	account := &Account{
		ID:       1,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"openai_gateway_tls": map[string]any{
				"enabled":    true,
				"profile_id": 9,
			},
		},
	}

	runtime, err := svc.ResolveAccountRuntime(context.Background(), account, http.Header{}, OpenAIClientTransportHTTP)
	require.NoError(t, err)
	require.Equal(t, int64(7), runtime.TLS.ProfileID)
	require.Equal(t, "bucket", runtime.TLS.Source)

	cfg.Gateway.OpenAICore.EgressBuckets[0].TLS.AllowAccountOverride = true
	runtime, err = svc.ResolveAccountRuntime(context.Background(), account, http.Header{}, OpenAIClientTransportHTTP)
	require.NoError(t, err)
	require.Equal(t, int64(9), runtime.TLS.ProfileID)
	require.Equal(t, "account_override", runtime.TLS.Source)
}

func TestOpenAIGatewayCoreService_ResolveAccountRuntimeTLSDefaultFallback(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.TLSBinding.Enabled = true
	cfg.Gateway.OpenAICore.DefaultProfileMode = OpenAIGatewayProfileModeFixed
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
	svc := NewOpenAIGatewayCoreService(&openAIGatewayCoreRepoStub{}, cfg, nil, testOpenAITLSProfileService())

	runtime, err := svc.ResolveAccountRuntime(context.Background(), &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}, http.Header{}, OpenAIClientTransportHTTP)
	require.NoError(t, err)
	require.True(t, runtime.TLS.Enabled)
	require.Equal(t, int64(0), runtime.TLS.ProfileID)
	require.Equal(t, "default_fallback", runtime.TLS.Source)
	require.Equal(t, "bucket_profile_unset", runtime.TLS.FallbackReason)
	require.NotEmpty(t, runtime.TLS.ProfileName)
}

func TestOpenAIGatewayCoreService_ResolveAccountRuntimeTLSFailsClosedWithoutEffectivePolicy(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.TLSBinding.Enabled = true
	cfg.Gateway.OpenAICore.DefaultProfileMode = OpenAIGatewayProfileModeFixed
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{
			Name:    "default",
			Enabled: true,
			TLS: config.OpenAIGatewayBucketTLSConfig{
				Enabled: true,
			},
		},
	}
	svc := NewOpenAIGatewayCoreService(&openAIGatewayCoreRepoStub{}, cfg, nil, testOpenAITLSProfileService())

	runtime, err := svc.ResolveAccountRuntime(context.Background(), &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}, http.Header{}, OpenAIClientTransportHTTP)
	require.Nil(t, runtime)
	require.Error(t, err)
	var policyErr *OpenAIEgressPolicyError
	require.True(t, errors.As(err, &policyErr))
	require.Equal(t, "tls_policy_no_effective_profile", policyErr.Code)
}

func TestOpenAIGatewayCoreService_ResolveAccountRuntimeTLSOpenAIOverrideDoesNotDependOnAnthropicFlag(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.TLSBinding.Enabled = true
	cfg.Gateway.OpenAICore.DefaultProfileMode = OpenAIGatewayProfileModeFixed
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{
			Name:    "default",
			Enabled: true,
			TLS: config.OpenAIGatewayBucketTLSConfig{
				Enabled:              true,
				ProfileID:            7,
				AllowAccountOverride: true,
			},
		},
	}
	svc := NewOpenAIGatewayCoreService(&openAIGatewayCoreRepoStub{}, cfg, nil, testOpenAITLSProfileService(
		&model.TLSFingerprintProfile{ID: 7, Name: "Bucket"},
		&model.TLSFingerprintProfile{ID: 9, Name: "Account"},
	))
	account := &Account{
		ID:       1,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"enable_tls_fingerprint": false,
			"openai_gateway_tls": map[string]any{
				"enabled":    true,
				"profile_id": 9,
			},
		},
	}
	require.False(t, account.IsTLSFingerprintEnabled())

	runtime, err := svc.ResolveAccountRuntime(context.Background(), account, http.Header{}, OpenAIClientTransportHTTP)
	require.NoError(t, err)
	require.Equal(t, int64(9), runtime.TLS.ProfileID)
	require.Equal(t, "account_override", runtime.TLS.Source)
}

func TestOpenAIGatewayCoreService_BuildHealthSnapshot(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"

	repo := &openAIGatewayCoreRepoStub{
		openAIGatewayCoreAccountRepoStubBase: openAIGatewayCoreAccountRepoStubBase{
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

func TestOpenAIGatewayCoreService_BuildHealthSnapshotIncludesTLSBinding(t *testing.T) {
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
	svc := NewOpenAIGatewayCoreService(&openAIGatewayCoreRepoStub{}, cfg, nil, testOpenAITLSProfileService(&model.TLSFingerprintProfile{
		ID:   7,
		Name: "Chrome 124",
	}))

	health, err := svc.BuildHealthSnapshot(context.Background(), OpenAIWSPerformanceMetricsSnapshot{})
	require.NoError(t, err)
	require.NotNil(t, health.TLSBinding)
	require.True(t, health.TLSBinding.Enabled)
	require.True(t, health.TLSBinding.Buckets["default"].Enabled)
	require.Equal(t, int64(7), health.TLSBinding.Buckets["default"].ProfileID)
	require.Equal(t, "Chrome 124", health.TLSBinding.Buckets["default"].ProfileName)
	require.Equal(t, "bucket", health.TLSBinding.Buckets["default"].Source)
	require.NotEmpty(t, health.TLSBinding.Buckets["default"].CacheIdentity)
	require.True(t, health.TLSBinding.Buckets["default"].HTTPApplicable)
	require.True(t, health.TLSBinding.Buckets["default"].WSApplicable)
}

func TestOpenAIGatewayCoreService_BuildHealthSnapshotIncludesTLSBindingSummary(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.TLSBinding.Enabled = true
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "bound"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{
			Name:    "bound",
			Enabled: true,
			TLS: config.OpenAIGatewayBucketTLSConfig{
				Enabled:   true,
				ProfileID: 7,
			},
		},
		{
			Name:    "default-fallback",
			Enabled: true,
			TLS: config.OpenAIGatewayBucketTLSConfig{
				Enabled:              true,
				AllowDefaultFallback: true,
			},
		},
		{
			Name:    "plain-fallback",
			Enabled: true,
			TLS: config.OpenAIGatewayBucketTLSConfig{
				Enabled:            true,
				AllowPlainFallback: true,
			},
		},
		{
			Name:    "fail-closed",
			Enabled: true,
			TLS: config.OpenAIGatewayBucketTLSConfig{
				Enabled: true,
			},
		},
	}
	repo := &openAIGatewayCoreRepoStub{
		openAIGatewayCoreAccountRepoStubBase: openAIGatewayCoreAccountRepoStubBase{
			accounts: []Account{
				{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Extra: map[string]any{"openai_gateway_egress_bucket": "bound", "openai_token_source": OpenAITokenSourceRTManaged, "openai_auth_state": OpenAIAuthStateHealthy}},
				{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Extra: map[string]any{"openai_gateway_egress_bucket": "default-fallback", "openai_token_source": OpenAITokenSourceRTManaged, "openai_auth_state": OpenAIAuthStateHealthy}},
				{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Extra: map[string]any{"openai_gateway_egress_bucket": "plain-fallback", "openai_token_source": OpenAITokenSourceRTManaged, "openai_auth_state": OpenAIAuthStateHealthy}},
				{ID: 4, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Extra: map[string]any{"openai_gateway_egress_bucket": "fail-closed", "openai_token_source": OpenAITokenSourceRTManaged, "openai_auth_state": OpenAIAuthStateHealthy}},
			},
		},
	}
	svc := NewOpenAIGatewayCoreService(repo, cfg, nil, testOpenAITLSProfileService(&model.TLSFingerprintProfile{ID: 7, Name: "Chrome 124"}))

	health, err := svc.BuildHealthSnapshot(context.Background(), OpenAIWSPerformanceMetricsSnapshot{})
	require.NoError(t, err)
	require.NotNil(t, health.TLSBinding)
	require.NotNil(t, health.TLSBinding.Summary)
	require.Equal(t, int64(4), health.TLSBinding.Summary.AccountsTotal)
	require.Equal(t, int64(1), health.TLSBinding.Summary.BoundAccounts)
	require.Equal(t, int64(1), health.TLSBinding.Summary.DefaultFallbackAccounts)
	require.Equal(t, int64(1), health.TLSBinding.Summary.PlainFallbackAccounts)
	require.Equal(t, int64(1), health.TLSBinding.Summary.FailClosedAccounts)
	require.Equal(t, int64(1), health.TLSBinding.Summary.ProfileUsage["profile_id:7"])
	require.Equal(t, int64(1), health.TLSBinding.Summary.ProfileUsage["builtin_default"])
	require.Contains(t, health.WarningCodes, "tls_policy_no_effective_profile")
}

func TestOpenAIGatewayCoreService_BuildHealthSnapshotDegradesOnMissingConfiguredTLSProfile(t *testing.T) {
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
				ProfileID: 404,
			},
		},
	}
	repo := &openAIGatewayCoreRepoStub{
		openAIGatewayCoreAccountRepoStubBase: openAIGatewayCoreAccountRepoStubBase{
			accounts: []Account{
				{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Extra: map[string]any{"openai_gateway_egress_bucket": "default", "openai_token_source": OpenAITokenSourceRTManaged, "openai_auth_state": OpenAIAuthStateHealthy}},
			},
		},
	}
	svc := NewOpenAIGatewayCoreService(repo, cfg, nil, testOpenAITLSProfileService())

	health, err := svc.BuildHealthSnapshot(context.Background(), OpenAIWSPerformanceMetricsSnapshot{})
	require.NoError(t, err)
	require.Contains(t, health.WarningCodes, "tls_profile_not_found")
	require.Equal(t, "degraded", health.GatewayStatus)
	require.Equal(t, "degraded", health.OAuthStatus)
	require.Equal(t, "tls_profile_not_found", health.DegradedReason)
}

func TestOpenAIGatewayCoreService_BuildHealthSnapshotDegradesOnInvalidConfiguredTLSProfile(t *testing.T) {
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
	repo := &openAIGatewayCoreRepoStub{
		openAIGatewayCoreAccountRepoStubBase: openAIGatewayCoreAccountRepoStubBase{
			accounts: []Account{
				{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Extra: map[string]any{"openai_gateway_egress_bucket": "default", "openai_token_source": OpenAITokenSourceRTManaged, "openai_auth_state": OpenAIAuthStateHealthy}},
			},
		},
	}
	svc := NewOpenAIGatewayCoreService(repo, cfg, nil, testOpenAITLSProfileService(&model.TLSFingerprintProfile{ID: 7}))

	health, err := svc.BuildHealthSnapshot(context.Background(), OpenAIWSPerformanceMetricsSnapshot{})
	require.NoError(t, err)
	require.Contains(t, health.WarningCodes, "tls_profile_invalid")
	require.Equal(t, "degraded", health.GatewayStatus)
	require.Equal(t, "tls_profile_invalid", health.DegradedReason)
}

func TestOpenAIGatewayCoreService_ResolveEffectiveTLSPreservesInvalidProfileCode(t *testing.T) {
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
	svc := NewOpenAIGatewayCoreService(&openAIGatewayCoreRepoStub{}, cfg, nil, testOpenAITLSProfileService(&model.TLSFingerprintProfile{ID: 7}))

	_, err := svc.ResolveEffectiveTLS(context.Background(), &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}, buildOpenAIEgressResolution("default", "", openAIEgressSourceBucket), OpenAIClientTransportWS)
	require.Error(t, err)
	var policyErr *OpenAIEgressPolicyError
	require.ErrorAs(t, err, &policyErr)
	require.Equal(t, "tls_profile_invalid", policyErr.Code)
}

func TestOpenAIGatewayCoreService_ResolveAccountRuntimeTLSIdentityChangesWhenFingerprintChanges(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.TLSBinding.Enabled = true
	cfg.Gateway.OpenAICore.DefaultProfileMode = OpenAIGatewayProfileModeFixed
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
	profile := &model.TLSFingerprintProfile{ID: 7, Name: "Chrome 124", ALPNProtocols: []string{"h2", "http/1.1"}}
	svc := NewOpenAIGatewayCoreService(&openAIGatewayCoreRepoStub{}, cfg, nil, testOpenAITLSProfileService(profile))

	first, err := svc.ResolveAccountRuntime(context.Background(), &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}, http.Header{}, OpenAIClientTransportHTTP)
	require.NoError(t, err)

	profile.ALPNProtocols = []string{"http/1.1"}
	second, err := svc.ResolveAccountRuntime(context.Background(), &Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth}, http.Header{}, OpenAIClientTransportHTTP)
	require.NoError(t, err)

	require.NotEqual(t, first.TLS.ProfileHash, second.TLS.ProfileHash)
	require.NotEqual(t, first.TLS.CacheIdentity, second.TLS.CacheIdentity)
}

func TestOpenAIGatewayCoreService_ResolveAccountRuntimeTLSHashUsesEffectiveDefaults(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.TLSBinding.Enabled = true
	cfg.Gateway.OpenAICore.DefaultProfileMode = OpenAIGatewayProfileModeFixed
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
	implicitDefault := &model.TLSFingerprintProfile{ID: 7, Name: "Implicit default"}
	explicitDefault := &model.TLSFingerprintProfile{ID: 7, Name: "Explicit default", ALPNProtocols: []string{"http/1.1"}}

	svc := NewOpenAIGatewayCoreService(&openAIGatewayCoreRepoStub{}, cfg, nil, testOpenAITLSProfileService(implicitDefault))
	first, err := svc.ResolveAccountRuntime(context.Background(), &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}, http.Header{}, OpenAIClientTransportHTTP)
	require.NoError(t, err)

	svc = NewOpenAIGatewayCoreService(&openAIGatewayCoreRepoStub{}, cfg, nil, testOpenAITLSProfileService(explicitDefault))
	second, err := svc.ResolveAccountRuntime(context.Background(), &Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth}, http.Header{}, OpenAIClientTransportHTTP)
	require.NoError(t, err)

	require.Equal(t, first.TLS.ProfileHash, second.TLS.ProfileHash)
	require.Equal(t, first.TLS.CacheIdentity, second.TLS.CacheIdentity)
}

func TestOpenAIGatewayCoreService_BuildHealthSnapshotDegradesOnUnsafeCredentials(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.ProductionMode = true
	cfg.Gateway.OpenAICore.RequireEncryptedCredentials = true
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("aa", 32)
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{Name: "default", Enabled: true, ProxyURL: "http://127.0.0.1:8080"},
	}

	repo := &openAIGatewayCoreRepoStub{
		openAIGatewayCoreAccountRepoStubBase: openAIGatewayCoreAccountRepoStubBase{
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
					Credentials: map[string]any{
						"access_token": "plain-access-token",
					},
				},
			},
		},
	}
	svc := NewOpenAIGatewayCoreService(repo, cfg, nil)

	health, err := svc.BuildHealthSnapshot(context.Background(), OpenAIWSPerformanceMetricsSnapshot{})
	require.NoError(t, err)
	require.Equal(t, "degraded", health.GatewayStatus)
	require.Equal(t, "degraded", health.OAuthStatus)
	require.Equal(t, int64(1), health.UnsafeCredentialAccounts)
	require.Equal(t, "credential_storage_not_production_safe", health.DegradedReason)
}

func TestOpenAIGatewayCoreService_BuildHealthSnapshotDegradesOnUnsafeAPIKeyCredentials(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.ProductionMode = true
	cfg.Gateway.OpenAICore.RequireEncryptedCredentials = true
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("ac", 32)
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{Name: "default", Enabled: true, ProxyURL: "http://127.0.0.1:8080"},
	}

	repo := &openAIGatewayCoreRepoStub{
		openAIGatewayCoreAccountRepoStubBase: openAIGatewayCoreAccountRepoStubBase{
			accounts: []Account{
				{
					ID:       2,
					Platform: PlatformOpenAI,
					Type:     AccountTypeAPIKey,
					Status:   StatusActive,
					Credentials: map[string]any{
						"api_key": "sk-plain-api-key",
					},
				},
			},
		},
	}
	svc := NewOpenAIGatewayCoreService(repo, cfg, nil)

	health, err := svc.BuildHealthSnapshot(context.Background(), OpenAIWSPerformanceMetricsSnapshot{})
	require.NoError(t, err)
	require.Equal(t, "degraded", health.GatewayStatus)
	require.Equal(t, int64(1), health.UnsafeCredentialAccounts)
	require.Equal(t, "credential_storage_not_production_safe", health.DegradedReason)
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
		openAIGatewayCoreAccountRepoStubBase: openAIGatewayCoreAccountRepoStubBase{
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
		openAIGatewayCoreAccountRepoStubBase: openAIGatewayCoreAccountRepoStubBase{
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

func TestOpenAIGatewayCoreService_BuildHealthSnapshotWarnsOnBucketConcentration(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "bucket-a"
	cfg.Gateway.OpenAICore.BucketWarnAccountThreshold = 1
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{Name: "bucket-a", Enabled: true, ProxyURL: "http://127.0.0.1:8080"},
	}

	repo := &openAIGatewayCoreRepoStub{
		openAIGatewayCoreAccountRepoStubBase: openAIGatewayCoreAccountRepoStubBase{
			accounts: []Account{
				{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Extra: map[string]any{"openai_gateway_egress_bucket": "bucket-a", "openai_token_source": OpenAITokenSourceRTManaged, "openai_auth_state": OpenAIAuthStateHealthy}},
				{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Extra: map[string]any{"openai_gateway_egress_bucket": "bucket-a", "openai_token_source": OpenAITokenSourceRTManaged, "openai_auth_state": OpenAIAuthStateHealthy}},
			},
		},
	}
	svc := NewOpenAIGatewayCoreService(repo, cfg, nil)

	health, err := svc.BuildHealthSnapshot(context.Background(), OpenAIWSPerformanceMetricsSnapshot{})
	require.NoError(t, err)
	require.Contains(t, health.WarningCodes, "bucket_concentration_high")
}

func TestOpenAIGatewayCoreService_BuildHealthSnapshotWarnsOnDirectEgressInProduction(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.ProductionMode = true
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "bucket-a"
	cfg.Gateway.OpenAICore.AllowDirectFallback = true
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{Name: "bucket-a", Enabled: true},
	}

	repo := &openAIGatewayCoreRepoStub{
		openAIGatewayCoreAccountRepoStubBase: openAIGatewayCoreAccountRepoStubBase{
			accounts: []Account{
				{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Extra: map[string]any{"openai_gateway_egress_bucket": "bucket-a", "openai_token_source": OpenAITokenSourceRTManaged, "openai_auth_state": OpenAIAuthStateHealthy}},
			},
		},
	}
	svc := NewOpenAIGatewayCoreService(repo, cfg, nil)

	health, err := svc.BuildHealthSnapshot(context.Background(), OpenAIWSPerformanceMetricsSnapshot{})
	require.NoError(t, err)
	require.Contains(t, health.WarningCodes, "direct_egress_in_production")
	require.Equal(t, "degraded", health.GatewayStatus)
	require.Equal(t, "direct_egress_in_production", health.DegradedReason)
}

func TestOpenAIGatewayCoreService_BuildHealthSnapshotWarnsOnDirectEgressInProductionForAPIKey(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.ProductionMode = true
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "bucket-a"
	cfg.Gateway.OpenAICore.AllowDirectFallback = true
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{Name: "bucket-a", Enabled: true},
	}

	repo := &openAIGatewayCoreRepoStub{
		openAIGatewayCoreAccountRepoStubBase: openAIGatewayCoreAccountRepoStubBase{
			accounts: []Account{
				{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Extra: map[string]any{"openai_gateway_egress_bucket": "bucket-a"}},
			},
		},
	}
	svc := NewOpenAIGatewayCoreService(repo, cfg, nil)

	health, err := svc.BuildHealthSnapshot(context.Background(), OpenAIWSPerformanceMetricsSnapshot{})
	require.NoError(t, err)
	require.Contains(t, health.WarningCodes, "direct_egress_in_production")
	require.Equal(t, "degraded", health.GatewayStatus)
}

func TestOpenAIGatewayCoreService_BuildHealthSnapshotWarnsOnDirectFallbackDisabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.ProductionMode = true
	cfg.Gateway.OpenAICore.EgressFailClosed = true
	cfg.Gateway.OpenAICore.AllowDirectFallback = false
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "bucket-a"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{Name: "bucket-a", Enabled: true},
	}

	repo := &openAIGatewayCoreRepoStub{
		openAIGatewayCoreAccountRepoStubBase: openAIGatewayCoreAccountRepoStubBase{
			accounts: []Account{
				{ID: 4, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Extra: map[string]any{"openai_gateway_egress_bucket": "bucket-a", "openai_token_source": OpenAITokenSourceRTManaged, "openai_auth_state": OpenAIAuthStateHealthy}},
			},
		},
	}
	svc := NewOpenAIGatewayCoreService(repo, cfg, nil)

	health, err := svc.BuildHealthSnapshot(context.Background(), OpenAIWSPerformanceMetricsSnapshot{})
	require.NoError(t, err)
	require.Contains(t, health.WarningCodes, "direct_fallback_disabled")
	require.Equal(t, "degraded", health.GatewayStatus)
	require.Equal(t, "direct_fallback_disabled", health.DegradedReason)
}

func TestOpenAIGatewayCoreService_BuildAdminStatusSnapshotWarnsOnBucketConcentration(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "bucket-a"
	cfg.Gateway.OpenAICore.BucketWarnAccountThreshold = 1
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{Name: "bucket-a", Enabled: true, ProxyURL: "http://127.0.0.1:8080"},
	}

	repo := &openAIGatewayCoreRepoStub{
		openAIGatewayCoreAccountRepoStubBase: openAIGatewayCoreAccountRepoStubBase{
			accounts: []Account{
				{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Extra: map[string]any{"openai_gateway_egress_bucket": "bucket-a", "openai_token_source": OpenAITokenSourceRTManaged, "openai_auth_state": OpenAIAuthStateHealthy}},
				{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Extra: map[string]any{"openai_gateway_egress_bucket": "bucket-a", "openai_token_source": OpenAITokenSourceRTManaged, "openai_auth_state": OpenAIAuthStateHealthy}},
			},
		},
	}
	svc := NewOpenAIGatewayCoreService(repo, cfg, nil)

	snapshot, err := svc.BuildAdminStatusSnapshot(context.Background(), OpenAIWSPerformanceMetricsSnapshot{})
	require.NoError(t, err)
	require.Len(t, snapshot.Buckets, 1)
	require.Equal(t, "bucket_concentration_high", snapshot.Buckets[0].Warning)
}

func TestOpenAIGatewayRedactSanitizeUpstreamErrorMessage(t *testing.T) {
	raw := "proxy=http://user:pass@proxy.example.com:8080/path?access_token=tok123&refresh_token=tok456 Authorization: Bearer abcdefghij/secret+tail== api_key=sk-abcdef1234567890"
	redacted := sanitizeUpstreamErrorMessage(raw)

	require.Contains(t, redacted, "proxy.example.com:8080")
	require.Contains(t, redacted, "Bearer ***")
	require.Contains(t, redacted, "api_key=***")
	require.NotContains(t, redacted, "user:pass")
	require.NotContains(t, redacted, "tok123")
	require.NotContains(t, redacted, "tok456")
	require.NotContains(t, redacted, "abcdefghij/secret+tail==")
	require.NotContains(t, redacted, "path?")
}

func TestOpenAIGatewayRedactSanitizeUpstreamErrorBody(t *testing.T) {
	raw := []byte(`{"access_token":"tok123","refresh_token":"tok456","detail":"Bearer abcdefghij/secret+tail==","proxy":"http://user:pass@proxy.example.com:8080/path?q=1"}`)
	redacted := sanitizeUpstreamErrorBody(raw, 2048)

	require.Contains(t, redacted, `"access_token":"***"`)
	require.Contains(t, redacted, `"refresh_token":"***"`)
	require.Contains(t, redacted, `Bearer ***`)
	require.Contains(t, redacted, `proxy.example.com:8080`)
	require.NotContains(t, redacted, `tok123`)
	require.NotContains(t, redacted, `tok456`)
	require.NotContains(t, redacted, `user:pass`)
	require.NotContains(t, redacted, `q=1`)
}
