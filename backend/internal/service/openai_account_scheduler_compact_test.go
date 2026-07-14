package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// TestOpenAIGatewayService_SelectAccountWithScheduler_CompactPrefersSupportedOverUnknown
// 验证 compact 调度时显式支持 (tier=2) 优先于未探测 (tier=1)。
func TestOpenAIGatewayService_SelectAccountWithScheduler_CompactPrefersSupportedOverUnknown(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(91001)
	accounts := []Account{
		{
			ID:          71001,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			Extra:       map[string]any{}, // unknown
		},
		{
			ID:          71002,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			Extra:       map[string]any{"openai_compact_supported": true}, // tier=2
		},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}
	selection, _, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"",
		"gpt-5.4",
		nil,
		OpenAIUpstreamTransportAny,
		true,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(71002), selection.Account.ID, "compact-supported account should win over unknown")
}

// TestOpenAIGatewayService_SelectAccountWithScheduler_CompactRejectsExplicitlyUnsupported
// 验证 force_off / 已探测不支持 (tier=0) 的账号不会被 compact 请求选中。
func TestOpenAIGatewayService_SelectAccountWithScheduler_CompactRejectsExplicitlyUnsupported(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(91002)
	accounts := []Account{
		{
			ID:          71010,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			Extra:       map[string]any{"openai_compact_mode": OpenAICompactModeForceOff},
		},
		{
			ID:          71011,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			Extra:       map[string]any{"openai_compact_supported": false},
		},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, _, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"",
		"gpt-5.4",
		nil,
		OpenAIUpstreamTransportAny,
		true,
	)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrNoAvailableCompactAccounts), "compact-only accounts should rejected explicitly unsupported and return compact error")
	require.Nil(t, selection)
}

// TestOpenAIGatewayService_SelectAccountWithScheduler_CompactFallsBackToUnknown
// 验证当没有"已知支持"账号时，compact 请求会回退到"未探测"账号。
func TestOpenAIGatewayService_SelectAccountWithScheduler_CompactFallsBackToUnknown(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(91003)
	accounts := []Account{
		{
			ID:          71020,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			Extra:       map[string]any{"openai_compact_supported": false}, // tier=0
		},
		{
			ID:          71021,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			Extra:       map[string]any{}, // unknown -> tier=1
		},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}
	selection, _, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"",
		"gpt-5.4",
		nil,
		OpenAIUpstreamTransportAny,
		true,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(71021), selection.Account.ID, "unknown account should be picked when no supported account available")
}

// A compact-only mapping is an explicit declaration that the OAuth account
// should serve the requested alias through the mapped compact model.
func TestOpenAIGatewayService_SelectAccountWithScheduler_CompactMappingAllowsOAuthAlias(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(91004)
	accounts := []Account{
		{
			ID:          71030,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			GroupIDs:    []int64{groupID},
			Credentials: map[string]any{
				"compact_model_mapping": map[string]any{"gpt-5.6-sol": "gpt-5.4"},
			},
			Extra: map[string]any{
				"openai_compact_mode":              OpenAICompactModeForceOn,
				"openai_gateway_canonical_version": "0.104.0",
			},
		},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}
	selection, _, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"",
		"gpt-5.6-sol",
		nil,
		OpenAIUpstreamTransportAny,
		true,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(71030), selection.Account.ID)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_CompactMappingRejectsUnsupportedOAuthTarget(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	groupID := int64(91005)
	account := Account{
		ID: 71031, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
		Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID},
		Credentials: map[string]any{"compact_model_mapping": map[string]any{"gpt-5.6-sol": "unsupported-compact-model"}},
		Extra:       map[string]any{"openai_compact_mode": OpenAICompactModeForceOn},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
		cache:              &schedulerTestGatewayCache{},
		cfg:                &config.Config{},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, _, err := svc.SelectAccountWithScheduler(context.Background(), &groupID, "", "", "gpt-5.6-sol", nil, OpenAIUpstreamTransportAny, true)
	require.Error(t, err)
	require.Nil(t, selection)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_CompactMappingDoesNotAffectNormalTraffic(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	groupID := int64(91006)
	account := Account{
		ID: 71032, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
		Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID},
		Credentials: map[string]any{"compact_model_mapping": map[string]any{"gpt-5.6-sol": "gpt-5.4"}},
		Extra:       map[string]any{"openai_compact_mode": OpenAICompactModeForceOn},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
		cache:              &schedulerTestGatewayCache{},
		cfg:                &config.Config{},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, _, err := svc.SelectAccountWithScheduler(context.Background(), &groupID, "", "", "gpt-5.6-sol", nil, OpenAIUpstreamTransportAny, false)
	require.Error(t, err)
	require.Nil(t, selection)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_CompactUsesModelScopedSupport(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	groupID := int64(91007)
	accounts := []Account{
		{
			ID: 71040, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive,
			Schedulable: true, Concurrency: 1, Priority: 0,
			Extra: map[string]any{
				"openai_compact_supported": true,
				"openai_compact_model_support": map[string]any{
					"gpt-5.4": map[string]any{"supported": false},
				},
			},
		},
		{
			ID: 71041, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive,
			Schedulable: true, Concurrency: 1, Priority: 0,
			Extra: map[string]any{
				"openai_compact_model_support": map[string]any{
					"gpt-5.4": map[string]any{"supported": true},
				},
			},
		},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                &config.Config{},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, _, err := svc.SelectAccountWithScheduler(context.Background(), &groupID, "", "", "gpt-5.4", nil, OpenAIUpstreamTransportAny, true)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.Equal(t, int64(71041), selection.Account.ID)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_CompactFailoverReachesMappedOAuth(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	groupID := int64(91008)
	compactMapping := map[string]any{"gpt-5.6-sol": "gpt-5.4"}
	accounts := []Account{
		{
			ID: 71050, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive,
			Schedulable: true, Concurrency: 1, Priority: 0,
			Credentials: map[string]any{"compact_model_mapping": compactMapping},
			Extra:       map[string]any{"openai_compact_mode": OpenAICompactModeForceOn},
		},
		{
			ID: 71051, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
			Schedulable: true, Concurrency: 1, Priority: 1,
			Credentials: map[string]any{"compact_model_mapping": compactMapping},
			Extra:       map[string]any{"openai_compact_mode": OpenAICompactModeForceOn, "openai_gateway_canonical_version": "0.104.0"},
		},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                &config.Config{},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	first, _, err := svc.SelectAccountWithScheduler(context.Background(), &groupID, "", "", "gpt-5.6-sol", nil, OpenAIUpstreamTransportAny, true)
	require.NoError(t, err)
	require.Equal(t, int64(71050), first.Account.ID)
	first.ReleaseFunc()

	second, _, err := svc.SelectAccountWithScheduler(context.Background(), &groupID, "", "", "gpt-5.6-sol", map[int64]struct{}{71050: {}}, OpenAIUpstreamTransportAny, true)
	require.NoError(t, err)
	require.Equal(t, int64(71051), second.Account.ID)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_CompactRechecksStaleScopedCapability(t *testing.T) {
	groupID := int64(91009)
	stale := &Account{
		ID: 71060, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive,
		Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID},
		Extra: map[string]any{"openai_compact_model_support": map[string]any{
			"gpt-5.4": map[string]any{"supported": false},
		}},
	}
	fresh := *stale
	fresh.Extra = map[string]any{"openai_compact_model_support": map[string]any{
		"gpt-5.4": map[string]any{"supported": true},
	}}
	for _, tt := range []struct {
		name              string
		advancedScheduler string
	}{
		{name: "legacy load-aware scheduler", advancedScheduler: "false"},
		{name: "advanced scheduler", advancedScheduler: "true"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			snapshotCache := &openAISnapshotCacheStub{
				snapshotAccounts: []*Account{stale},
				accountsByID:     map[int64]*Account{stale.ID: stale},
			}
			svc := &OpenAIGatewayService{
				accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{fresh}},
				cache:              &schedulerTestGatewayCache{},
				cfg:                &config.Config{},
				rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService(tt.advancedScheduler),
				schedulerSnapshot:  &SchedulerSnapshotService{cache: snapshotCache},
				concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
			}

			selection, _, err := svc.SelectAccountWithScheduler(context.Background(), &groupID, "", "", "gpt-5.4", nil, OpenAIUpstreamTransportAny, true)
			require.NoError(t, err)
			require.NotNil(t, selection)
			require.Equal(t, stale.ID, selection.Account.ID)
		})
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_CompactSkipsUnknownStickyAccount(t *testing.T) {
	groupID := int64(91010)
	sessionHash := "compact-unknown-sticky"
	accounts := []Account{
		{
			ID: 71070, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive,
			Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID},
			Extra: map[string]any{},
		},
		{
			ID: 71071, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive,
			Schedulable: true, Concurrency: 1, Priority: 1, GroupIDs: []int64{groupID},
			Extra: map[string]any{"openai_compact_supported": true},
		},
	}

	for _, tt := range []struct {
		name              string
		advancedScheduler string
	}{
		{name: "legacy load-aware scheduler", advancedScheduler: "false"},
		{name: "advanced scheduler", advancedScheduler: "true"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stickyKey := "openai:" + sessionHash
			cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{stickyKey: 71070}}
			svc := &OpenAIGatewayService{
				accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
				cache:              cache,
				cfg:                &config.Config{},
				rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService(tt.advancedScheduler),
				concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
			}

			selection, _, err := svc.SelectAccountWithScheduler(context.Background(), &groupID, "", sessionHash, "gpt-5.4", nil, OpenAIUpstreamTransportAny, true)
			require.NoError(t, err)
			require.NotNil(t, selection)
			require.Equal(t, int64(71071), selection.Account.ID)
			require.GreaterOrEqual(t, cache.deletedSessions[stickyKey], 1)
		})
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_ExcludedStickyBindingIsCleared(t *testing.T) {
	groupID := int64(91011)
	sessionHash := "failed-sticky-account"
	accounts := []Account{
		{
			ID: 71080, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive,
			Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID},
		},
		{
			ID: 71081, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive,
			Schedulable: true, Concurrency: 1, Priority: 1, GroupIDs: []int64{groupID},
		},
	}

	for _, tt := range []struct {
		name              string
		advancedScheduler string
	}{
		{name: "legacy load-aware scheduler", advancedScheduler: "false"},
		{name: "advanced scheduler", advancedScheduler: "true"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stickyKey := "openai:" + sessionHash
			cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{stickyKey: 71080}}
			svc := &OpenAIGatewayService{
				accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
				cache:              cache,
				cfg:                &config.Config{},
				rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService(tt.advancedScheduler),
				concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
			}

			selection, _, err := svc.SelectAccountWithScheduler(context.Background(), &groupID, "", sessionHash, "gpt-5.4", map[int64]struct{}{71080: {}}, OpenAIUpstreamTransportAny, false)
			require.NoError(t, err)
			require.NotNil(t, selection)
			require.Equal(t, int64(71081), selection.Account.ID)
			require.GreaterOrEqual(t, cache.deletedSessions[stickyKey], 1)
		})
	}
}

// TestOpenAICompactSupportTier 验证 tier 分类逻辑。
func TestOpenAICompactSupportTier(t *testing.T) {
	tests := []struct {
		name    string
		account *Account
		want    int
	}{
		{name: "nil", account: nil, want: 0},
		{name: "non openai", account: &Account{Platform: PlatformAnthropic}, want: 0},
		{name: "openai unknown", account: &Account{Platform: PlatformOpenAI, Extra: map[string]any{}}, want: 1},
		{name: "openai supported", account: &Account{Platform: PlatformOpenAI, Extra: map[string]any{"openai_compact_supported": true}}, want: 2},
		{name: "openai unsupported", account: &Account{Platform: PlatformOpenAI, Extra: map[string]any{"openai_compact_supported": false}}, want: 0},
		{name: "force on", account: &Account{Platform: PlatformOpenAI, Extra: map[string]any{"openai_compact_mode": OpenAICompactModeForceOn}}, want: 2},
		{name: "force off overrides probe true", account: &Account{Platform: PlatformOpenAI, Extra: map[string]any{"openai_compact_mode": OpenAICompactModeForceOff, "openai_compact_supported": true}}, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := openAICompactSupportTier(tt.account); got != tt.want {
				t.Fatalf("openAICompactSupportTier(...) = %d, want %d", got, tt.want)
			}
		})
	}
}
