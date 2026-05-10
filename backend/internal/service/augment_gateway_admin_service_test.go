package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAugmentGatewaySettingsUsesGatewayAugmentNamespace(t *testing.T) {
	t.Parallel()

	store := &augmentGatewaySettingsStoreStub{
		latest: map[string]*AugmentGatewaySettingsVersion{
			"gateway.augment.provider_groups.openai": {
				Namespace:    "gateway.augment.provider_groups.openai",
				SettingsJSON: mustServiceJSON(t, AugmentGatewayProviderGroupSetting{GroupID: 1001}),
				Version:      1,
			},
			"gateway.augment.enabled_models": {
				Namespace: "gateway.augment.enabled_models",
				SettingsJSON: mustServiceJSON(t, map[string]AugmentGatewayModelSetting{
					"gpt-5.4": {Enabled: true, SmokeStatus: AugmentGatewaySmokeStatusPassed},
				}),
				Version: 2,
			},
			"gateway.other.models": {
				Namespace:    "gateway.other.models",
				SettingsJSON: mustServiceJSON(t, map[string]any{"ignored": true}),
				Version:      9,
			},
		},
	}
	groupReader := &augmentGatewayGroupReaderStub{
		counts: map[int64]augmentGatewayGroupCount{
			1001: {total: 2, active: 1},
		},
	}
	svc := NewAugmentGatewayAdminService(store, groupReader, config.GatewayAugmentConfig{
		Enabled:       true,
		EnabledModels: []string{"gpt-5.4"},
	})

	state, err := svc.LoadAugmentGatewayRegistryState(context.Background())
	require.NoError(t, err)
	require.Equal(t, "gateway.augment.", store.lastPrefix)
	require.Equal(t, int64(1001), state.ProviderGroups[AugmentGatewayProviderOpenAI].GroupID)
	require.True(t, state.Models["gpt-5.4"].Enabled)
}

func TestAugmentGatewaySettingsRejectsProviderGroupWithoutAccounts(t *testing.T) {
	t.Parallel()

	store := &augmentGatewaySettingsStoreStub{}
	groupReader := &augmentGatewayGroupReaderStub{
		counts: map[int64]augmentGatewayGroupCount{
			1001: {total: 0, active: 0},
		},
	}
	svc := NewAugmentGatewayAdminService(store, groupReader, config.GatewayAugmentConfig{Enabled: true})

	_, err := svc.UpdateProviderGroup(context.Background(), AugmentGatewayProviderOpenAI, AugmentGatewayProviderGroupSetting{
		GroupID: 1001,
	}, AugmentGatewaySettingsMutationMeta{
		ExpectedVersion: 0,
		ActorAdminID:    7,
		RequestID:       "req-provider",
	})
	require.ErrorIs(t, err, ErrAugmentGatewayProviderGroupWithoutAccounts)
}

func TestAugmentGatewaySettingsRequiresSmokeBeforeVisible(t *testing.T) {
	t.Parallel()

	store := &augmentGatewaySettingsStoreStub{}
	svc := NewAugmentGatewayAdminService(store, &augmentGatewayGroupReaderStub{}, config.GatewayAugmentConfig{Enabled: true})

	_, err := svc.UpdateModel(context.Background(), "gpt-5.4", AugmentGatewayModelSetting{
		Enabled:     true,
		SmokeStatus: AugmentGatewaySmokeStatusPending,
	}, AugmentGatewaySettingsMutationMeta{
		ExpectedVersion: 0,
		ActorAdminID:    8,
		RequestID:       "req-model",
	})
	require.ErrorIs(t, err, ErrAugmentGatewayModelSmokeRequired)
}

func TestAugmentGatewaySettingsRejectsEnableWithoutExplicitPricing(t *testing.T) {
	t.Parallel()

	store := &augmentGatewaySettingsStoreStub{}
	svc := NewAugmentGatewayAdminService(store, &augmentGatewayGroupReaderStub{}, config.GatewayAugmentConfig{Enabled: true})
	svc.pricingChecker = augmentGatewayExplicitPricingCheckerStub{
		priced: map[string]bool{
			"gpt-5.5": false,
		},
	}

	_, err := svc.UpdateModel(context.Background(), "gpt-5.5", AugmentGatewayModelSetting{
		Enabled:     true,
		SmokeStatus: AugmentGatewaySmokeStatusPassed,
	}, AugmentGatewaySettingsMutationMeta{
		ExpectedVersion: 0,
		ActorAdminID:    8,
		RequestID:       "req-model-pricing",
	})
	require.ErrorIs(t, err, ErrAugmentGatewayModelExplicitPricingRequired)
}

func TestAugmentGatewaySettingsVersionConflict(t *testing.T) {
	t.Parallel()

	store := &augmentGatewaySettingsStoreStub{
		putErr: ErrAugmentGatewaySettingsVersionConflict,
	}
	groupReader := &augmentGatewayGroupReaderStub{
		counts: map[int64]augmentGatewayGroupCount{
			1001: {total: 1, active: 1},
		},
	}
	svc := NewAugmentGatewayAdminService(store, groupReader, config.GatewayAugmentConfig{Enabled: true})

	_, err := svc.UpdateProviderGroup(context.Background(), AugmentGatewayProviderOpenAI, AugmentGatewayProviderGroupSetting{
		GroupID: 1001,
	}, AugmentGatewaySettingsMutationMeta{
		ExpectedVersion: 1,
		ActorAdminID:    9,
		RequestID:       "req-conflict",
	})
	require.ErrorIs(t, err, ErrAugmentGatewaySettingsVersionConflict)
}

func TestAugmentGatewaySettingsRejectsUnknownProvider(t *testing.T) {
	t.Parallel()

	store := &augmentGatewaySettingsStoreStub{}
	svc := NewAugmentGatewayAdminService(store, &augmentGatewayGroupReaderStub{}, config.GatewayAugmentConfig{Enabled: true})

	_, err := svc.UpdateProviderGroup(context.Background(), AugmentGatewayProvider("unknown"), AugmentGatewayProviderGroupSetting{
		GroupID: 1,
	}, AugmentGatewaySettingsMutationMeta{})
	require.ErrorIs(t, err, ErrAugmentGatewayProviderUnknown)
}

func TestAugmentGatewaySettingsUpdateModelUsesFallbackConfigBaseline(t *testing.T) {
	t.Parallel()

	store := &augmentGatewaySettingsStoreStub{}
	svc := NewAugmentGatewayAdminService(store, &augmentGatewayGroupReaderStub{}, config.GatewayAugmentConfig{
		Enabled: true,
		EnabledModels: []string{
			"claude-sonnet-4-5",
		},
	})

	_, err := svc.UpdateModel(context.Background(), "gpt-5.4", AugmentGatewayModelSetting{
		Enabled:     true,
		SmokeStatus: AugmentGatewaySmokeStatusPassed,
	}, AugmentGatewaySettingsMutationMeta{})
	require.NoError(t, err)

	var saved map[string]AugmentGatewayModelSetting
	require.NoError(t, json.Unmarshal(store.putInput.SettingsJSON, &saved))
	require.True(t, saved["claude-sonnet-4-5"].Enabled)
}

func TestAugmentGatewaySettingsListModelsExposeSettingsVersion(t *testing.T) {
	t.Parallel()

	store := &augmentGatewaySettingsStoreStub{
		latest: map[string]*AugmentGatewaySettingsVersion{
			AugmentGatewayEnabledModelsNamespace: {
				Namespace: AugmentGatewayEnabledModelsNamespace,
				Version:   7,
				SettingsJSON: mustServiceJSON(t, map[string]AugmentGatewayModelSetting{
					"gpt-5.4": {Enabled: true, SmokeStatus: AugmentGatewaySmokeStatusPassed},
				}),
			},
		},
	}
	svc := NewAugmentGatewayAdminService(store, &augmentGatewayGroupReaderStub{}, config.GatewayAugmentConfig{
		Enabled:       true,
		EnabledModels: []string{"gpt-5.4"},
	})

	rows, err := svc.ListModels(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	require.Equal(t, int64(7), rows[0].SettingsVersion)
	require.Equal(t, AugmentGatewayEnabledModelsNamespace, rows[0].SettingsNamespace)
}

func TestAugmentGatewaySettingsListModelsExposeExplicitPricingState(t *testing.T) {
	t.Parallel()

	store := &augmentGatewaySettingsStoreStub{
		latest: map[string]*AugmentGatewaySettingsVersion{
			AugmentGatewayEnabledModelsNamespace: {
				Namespace: AugmentGatewayEnabledModelsNamespace,
				Version:   7,
				SettingsJSON: mustServiceJSON(t, map[string]AugmentGatewayModelSetting{
					"gpt-5.4": {Enabled: true, SmokeStatus: AugmentGatewaySmokeStatusPassed},
					"gpt-5.5": {Enabled: true, SmokeStatus: AugmentGatewaySmokeStatusPassed},
				}),
			},
		},
	}
	svc := NewAugmentGatewayAdminService(store, &augmentGatewayGroupReaderStub{}, config.GatewayAugmentConfig{
		Enabled: true,
		EnabledModels: []string{
			"gpt-5.4",
			"gpt-5.5",
		},
		ProviderGroups: config.GatewayAugmentProviderGroupsConfig{
			OpenAI: 1001,
		},
	})
	svc.pricingChecker = augmentGatewayExplicitPricingCheckerStub{
		priced: map[string]bool{
			"gpt-5.4": true,
			"gpt-5.5": false,
		},
	}

	rows, err := svc.ListModels(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 7)

	byID := make(map[string]AugmentGatewayManagedModel, len(rows))
	for _, row := range rows {
		byID[row.Model.ID] = row
	}

	require.True(t, byID["gpt-5.4"].ExplicitPricing)
	require.True(t, byID["gpt-5.4"].Enabled)
	require.False(t, byID["gpt-5.5"].ExplicitPricing)
	require.False(t, byID["gpt-5.5"].Enabled)
	require.False(t, byID["gpt-5.5"].Visible)
}

func TestAugmentGatewaySettingsListEntitlementGroupsFiltersAugmentEntitledActiveGroups(t *testing.T) {
	t.Parallel()

	store := &augmentGatewaySettingsStoreStub{}
	groupReader := &augmentGatewayGroupReaderStub{
		counts: map[int64]augmentGatewayGroupCount{
			201: {total: 4, active: 3},
		},
		groups: []Group{
			{ID: 201, Name: "Augment Users", Status: StatusActive, AugmentGatewayEntitled: true, AccountCount: 4, ActiveAccountCount: 3},
			{ID: 202, Name: "Plain Users", Status: StatusActive, AugmentGatewayEntitled: false, AccountCount: 6, ActiveAccountCount: 6},
			{ID: 203, Name: "Paused Augment", Status: StatusDisabled, AugmentGatewayEntitled: true, AccountCount: 1, ActiveAccountCount: 0},
		},
	}
	svc := NewAugmentGatewayAdminService(store, groupReader, config.GatewayAugmentConfig{Enabled: true})

	rows, err := svc.ListEntitlementGroups(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, int64(201), rows[0].ID)
	require.Equal(t, "Augment Users", rows[0].Name)
	require.Equal(t, int64(4), rows[0].TotalAccounts)
	require.Equal(t, int64(3), rows[0].ActiveAccounts)
}

func TestAugmentGatewaySettingsRollback(t *testing.T) {
	t.Parallel()

	rolledBack := &AugmentGatewaySettingsVersion{
		Namespace:    "gateway.augment.enabled_models",
		SettingsJSON: mustServiceJSON(t, map[string]AugmentGatewayModelSetting{"gpt-5.4": {Enabled: false, SmokeStatus: AugmentGatewaySmokeStatusPending}}),
		Version:      3,
	}
	store := &augmentGatewaySettingsStoreStub{rollbackResult: rolledBack}
	svc := NewAugmentGatewayAdminService(store, &augmentGatewayGroupReaderStub{}, config.GatewayAugmentConfig{Enabled: true})

	record, err := svc.RollbackNamespace(context.Background(), "gateway.augment.enabled_models", 1, AugmentGatewaySettingsMutationMeta{
		ExpectedVersion: 2,
		ActorAdminID:    99,
		RequestID:       "req-rollback",
	})
	require.NoError(t, err)
	require.Equal(t, rolledBack.Version, record.Version)
	require.Equal(t, "gateway.augment.enabled_models", store.rollbackInput.Namespace)
	require.Equal(t, int64(1), store.rollbackInput.TargetVersion)
}

func TestAugmentGatewaySettingsAuditStoresBeforeAfterDiff(t *testing.T) {
	t.Parallel()

	beforeJSON := mustServiceJSON(t, AugmentGatewayProviderGroupSetting{GroupID: 1001})
	afterJSON := mustServiceJSON(t, AugmentGatewayProviderGroupSetting{GroupID: 1002})
	store := &augmentGatewaySettingsStoreStub{
		putResult: &AugmentGatewaySettingsVersion{
			Namespace:    "gateway.augment.provider_groups.openai",
			SettingsJSON: afterJSON,
			Version:      2,
			BeforeJSON:   beforeJSON,
			AfterJSON:    afterJSON,
			Action:       AugmentGatewaySettingsActionUpdate,
			Result:       AugmentGatewaySettingsResultSuccess,
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		},
	}
	groupReader := &augmentGatewayGroupReaderStub{
		counts: map[int64]augmentGatewayGroupCount{
			1002: {total: 2, active: 1},
		},
	}
	svc := NewAugmentGatewayAdminService(store, groupReader, config.GatewayAugmentConfig{Enabled: true})

	record, err := svc.UpdateProviderGroup(context.Background(), AugmentGatewayProviderOpenAI, AugmentGatewayProviderGroupSetting{
		GroupID: 1002,
	}, AugmentGatewaySettingsMutationMeta{
		ExpectedVersion: 1,
		ActorAdminID:    10,
		RequestID:       "req-audit",
	})
	require.NoError(t, err)
	require.JSONEq(t, string(beforeJSON), string(record.BeforeJSON))
	require.JSONEq(t, string(afterJSON), string(record.AfterJSON))
}

type augmentGatewaySettingsStoreStub struct {
	latest         map[string]*AugmentGatewaySettingsVersion
	lastPrefix     string
	putErr         error
	putInput       AugmentGatewaySettingsWriteInput
	putResult      *AugmentGatewaySettingsVersion
	rollbackInput  AugmentGatewaySettingsRollbackInput
	rollbackResult *AugmentGatewaySettingsVersion
}

func (s *augmentGatewaySettingsStoreStub) ListLatest(ctx context.Context, namespacePrefix string) ([]AugmentGatewaySettingsVersion, error) {
	s.lastPrefix = namespacePrefix
	out := make([]AugmentGatewaySettingsVersion, 0, len(s.latest))
	for _, record := range s.latest {
		out = append(out, *record)
	}
	return out, nil
}

func (s *augmentGatewaySettingsStoreStub) GetLatest(ctx context.Context, namespace string) (*AugmentGatewaySettingsVersion, error) {
	if record, ok := s.latest[namespace]; ok {
		copy := *record
		return &copy, nil
	}
	return nil, nil
}

func (s *augmentGatewaySettingsStoreStub) Put(ctx context.Context, input AugmentGatewaySettingsWriteInput) (*AugmentGatewaySettingsVersion, error) {
	s.putInput = input
	if s.putErr != nil {
		return nil, s.putErr
	}
	if s.putResult != nil {
		copy := *s.putResult
		return &copy, nil
	}
	return &AugmentGatewaySettingsVersion{
		Namespace:    input.Namespace,
		SettingsJSON: append([]byte(nil), input.SettingsJSON...),
		Version:      input.ExpectedVersion + 1,
	}, nil
}

func (s *augmentGatewaySettingsStoreStub) Rollback(ctx context.Context, input AugmentGatewaySettingsRollbackInput) (*AugmentGatewaySettingsVersion, error) {
	s.rollbackInput = input
	if s.rollbackResult != nil {
		copy := *s.rollbackResult
		return &copy, nil
	}
	return &AugmentGatewaySettingsVersion{
		Namespace: input.Namespace,
		Version:   input.ExpectedVersion + 1,
	}, nil
}

type augmentGatewayGroupReaderStub struct {
	counts map[int64]augmentGatewayGroupCount
	groups []Group
}

type augmentGatewayGroupCount struct {
	total  int64
	active int64
}

func (s *augmentGatewayGroupReaderStub) GetAccountCount(ctx context.Context, groupID int64) (int64, int64, error) {
	if count, ok := s.counts[groupID]; ok {
		return count.total, count.active, nil
	}
	return 0, 0, nil
}

func (s *augmentGatewayGroupReaderStub) ListActive(ctx context.Context) ([]Group, error) {
	out := make([]Group, 0, len(s.groups))
	for _, group := range s.groups {
		if group.Status == StatusActive {
			out = append(out, group)
		}
	}
	return out, nil
}

func mustServiceJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return data
}
