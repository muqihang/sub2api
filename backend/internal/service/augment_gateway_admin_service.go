package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	augmentGatewaySettingsNamespacePrefix               = "gateway.augment."
	AugmentGatewayProviderGroupOpenAINamespace         = "gateway.augment.provider_groups.openai"
	AugmentGatewayProviderGroupDeepSeekNamespace       = "gateway.augment.provider_groups.deepseek"
	AugmentGatewayProviderGroupAnthropicNamespace      = "gateway.augment.provider_groups.anthropic"
	AugmentGatewayProviderGroupGeminiNamespace         = "gateway.augment.provider_groups.gemini"
	AugmentGatewayEnabledModelsNamespace               = "gateway.augment.enabled_models"
	AugmentGatewaySourcePolicyWukongEnabledNamespace   = "gateway.augment.source_policy.wukong_enabled"
	AugmentGatewaySourcePriorityNamespace              = "gateway.augment.source_policy.priority"
	AugmentGatewayRoutePolicyVersionNamespace          = "gateway.augment.route_policy_version"
	AugmentGatewayDefaultRoutePolicyVersion            = "v1"
	AugmentGatewaySettingsActionUpdate                 = "update"
	AugmentGatewaySettingsActionRollback               = "rollback"
	AugmentGatewaySettingsResultSuccess                = "success"
)

type AugmentGatewaySmokeStatus string

const (
	AugmentGatewaySmokeStatusPending AugmentGatewaySmokeStatus = "pending"
	AugmentGatewaySmokeStatusPassed  AugmentGatewaySmokeStatus = "passed"
	AugmentGatewaySmokeStatusFailed  AugmentGatewaySmokeStatus = "failed"
)

var (
	ErrAugmentGatewaySettingsVersionConflict = infraerrors.Conflict(
		"AUGMENT_GATEWAY_SETTINGS_VERSION_CONFLICT",
		"augment gateway settings version conflict",
	)
	ErrAugmentGatewayProviderGroupWithoutAccounts = infraerrors.BadRequest(
		"AUGMENT_GATEWAY_PROVIDER_GROUP_WITHOUT_ACCOUNTS",
		"augment gateway provider group has no accounts",
	)
	ErrAugmentGatewayModelSmokeRequired = infraerrors.BadRequest(
		"AUGMENT_GATEWAY_MODEL_SMOKE_REQUIRED",
		"augment gateway model requires passed smoke status before visible",
	)
	ErrAugmentGatewayModelUnknown = infraerrors.BadRequest(
		"AUGMENT_GATEWAY_MODEL_UNKNOWN",
		"augment gateway model is unknown",
	)
	ErrAugmentGatewayProviderUnknown = infraerrors.BadRequest(
		"AUGMENT_GATEWAY_PROVIDER_UNKNOWN",
		"augment gateway provider is unknown",
	)
)

type AugmentGatewaySettingsStore interface {
	ListLatest(ctx context.Context, namespacePrefix string) ([]AugmentGatewaySettingsVersion, error)
	GetLatest(ctx context.Context, namespace string) (*AugmentGatewaySettingsVersion, error)
	Put(ctx context.Context, input AugmentGatewaySettingsWriteInput) (*AugmentGatewaySettingsVersion, error)
	Rollback(ctx context.Context, input AugmentGatewaySettingsRollbackInput) (*AugmentGatewaySettingsVersion, error)
}

type AugmentGatewayGroupReader interface {
	GetAccountCount(ctx context.Context, groupID int64) (total int64, active int64, err error)
}

type AugmentGatewaySettingsVersion struct {
	Namespace            string          `json:"namespace"`
	SettingsJSON         json.RawMessage `json:"settings_json"`
	Version              int64           `json:"version"`
	PreviousVersion      *int64          `json:"previous_version,omitempty"`
	RollbackSnapshotJSON json.RawMessage `json:"rollback_snapshot_json,omitempty"`
	ActorAdminID         *int64          `json:"actor_admin_id,omitempty"`
	RequestID            string          `json:"request_id,omitempty"`
	BeforeJSON           json.RawMessage `json:"before_json,omitempty"`
	AfterJSON            json.RawMessage `json:"after_json,omitempty"`
	Action               string          `json:"action"`
	Result               string          `json:"result"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
}

type AugmentGatewaySettingsWriteInput struct {
	Namespace       string
	SettingsJSON    json.RawMessage
	ExpectedVersion int64
	ActorAdminID    int64
	RequestID       string
	Action          string
}

type AugmentGatewaySettingsRollbackInput struct {
	Namespace       string
	TargetVersion   int64
	ExpectedVersion int64
	ActorAdminID    int64
	RequestID       string
}

type AugmentGatewaySettingsMutationMeta struct {
	ExpectedVersion int64
	ActorAdminID    int64
	RequestID       string
}

type AugmentGatewayProviderGroupSetting struct {
	GroupID int64 `json:"group_id"`
}

type AugmentGatewaySourcePolicySetting struct {
	WukongEnabled bool `json:"wukong_enabled"`
}

type AugmentGatewaySourcePrioritySetting struct {
	Sources []string `json:"sources"`
}

type AugmentGatewayRoutePolicySetting struct {
	Version string `json:"version"`
}

type AugmentGatewayModelSetting struct {
	Enabled     bool                      `json:"enabled"`
	SmokeStatus AugmentGatewaySmokeStatus `json:"smoke_status,omitempty"`
}

type AugmentGatewayProviderRuntime struct {
	Provider       AugmentGatewayProvider `json:"provider"`
	Namespace      string                 `json:"namespace,omitempty"`
	Version        int64                  `json:"version,omitempty"`
	GroupID        int64                  `json:"group_id"`
	Healthy        bool                   `json:"healthy"`
	TotalAccounts  int64                  `json:"total_accounts"`
	ActiveAccounts int64                  `json:"active_accounts"`
}

type AugmentGatewayRegistryState struct {
	GatewayEnabled     bool                                              `json:"gateway_enabled"`
	ProviderGroups     map[AugmentGatewayProvider]AugmentGatewayProviderRuntime `json:"provider_groups"`
	Models             map[string]AugmentGatewayModelSetting             `json:"models"`
	SourcePolicy       AugmentGatewaySourcePolicySetting                 `json:"source_policy"`
	SourcePriority     []string                                          `json:"source_priority"`
	RoutePolicyVersion string                                            `json:"route_policy_version"`
}

type AugmentGatewayAdminService struct {
	store      AugmentGatewaySettingsStore
	groupReader AugmentGatewayGroupReader
	fallback   config.GatewayAugmentConfig
}

func NewAugmentGatewayAdminService(
	store AugmentGatewaySettingsStore,
	groupReader AugmentGatewayGroupReader,
	fallback config.GatewayAugmentConfig,
) *AugmentGatewayAdminService {
	if len(fallback.EnabledModels) == 0 {
		fallback.EnabledModels = defaultAugmentGatewayEnabledModelIDs()
	}
	return &AugmentGatewayAdminService{
		store:       store,
		groupReader: groupReader,
		fallback:    fallback,
	}
}

func (s *AugmentGatewayAdminService) LoadAugmentGatewayRegistryState(ctx context.Context) (*AugmentGatewayRegistryState, error) {
	state := &AugmentGatewayRegistryState{
		GatewayEnabled:     s.fallback.Enabled,
		ProviderGroups:     buildFallbackAugmentGatewayProviderGroups(s.fallback),
		Models:             buildFallbackAugmentGatewayModelSettings(s.fallback),
		SourcePolicy:       AugmentGatewaySourcePolicySetting{WukongEnabled: true},
		SourcePriority:     normalizePoolSourcePriority(nil),
		RoutePolicyVersion: AugmentGatewayDefaultRoutePolicyVersion,
	}
	if s == nil || s.store == nil {
		return state, nil
	}

	records, err := s.store.ListLatest(ctx, augmentGatewaySettingsNamespacePrefix)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		switch record.Namespace {
		case AugmentGatewayProviderGroupOpenAINamespace, AugmentGatewayProviderGroupDeepSeekNamespace, AugmentGatewayProviderGroupAnthropicNamespace, AugmentGatewayProviderGroupGeminiNamespace:
			provider, ok := augmentGatewayProviderFromNamespace(record.Namespace)
			if !ok {
				continue
			}
			var setting AugmentGatewayProviderGroupSetting
			if err := json.Unmarshal(record.SettingsJSON, &setting); err != nil {
				return nil, err
			}
			runtime := state.ProviderGroups[provider]
			runtime.Provider = provider
			runtime.Namespace = record.Namespace
			runtime.Version = record.Version
			runtime.GroupID = setting.GroupID
			state.ProviderGroups[provider] = runtime
		case AugmentGatewayEnabledModelsNamespace:
			var settings map[string]AugmentGatewayModelSetting
			if err := json.Unmarshal(record.SettingsJSON, &settings); err != nil {
				return nil, err
			}
			for modelID, setting := range settings {
				if _, ok := defaultAugmentGatewayModelByID(modelID); !ok {
					continue
				}
				state.Models[modelID] = normalizeAugmentGatewayModelSetting(setting)
			}
		case AugmentGatewaySourcePolicyWukongEnabledNamespace:
			var setting AugmentGatewaySourcePolicySetting
			if err := json.Unmarshal(record.SettingsJSON, &setting); err != nil {
				return nil, err
			}
			state.SourcePolicy = setting
		case AugmentGatewaySourcePriorityNamespace:
			var setting AugmentGatewaySourcePrioritySetting
			if err := json.Unmarshal(record.SettingsJSON, &setting); err != nil {
				return nil, err
			}
			state.SourcePriority = normalizePoolSourcePriority(setting.Sources)
		case AugmentGatewayRoutePolicyVersionNamespace:
			var setting AugmentGatewayRoutePolicySetting
			if err := json.Unmarshal(record.SettingsJSON, &setting); err != nil {
				return nil, err
			}
			if strings.TrimSpace(setting.Version) != "" {
				state.RoutePolicyVersion = strings.TrimSpace(setting.Version)
			}
		}
	}

	for provider, runtime := range state.ProviderGroups {
		if runtime.GroupID <= 0 {
			runtime.Healthy = false
			state.ProviderGroups[provider] = runtime
			continue
		}
		if s.groupReader == nil {
			runtime.Healthy = true
			state.ProviderGroups[provider] = runtime
			continue
		}
		total, active, err := s.groupReader.GetAccountCount(ctx, runtime.GroupID)
		if err != nil {
			return nil, err
		}
		runtime.TotalAccounts = total
		runtime.ActiveAccounts = active
		runtime.Healthy = active > 0
		state.ProviderGroups[provider] = runtime
	}

	return state, nil
}

func (s *AugmentGatewayAdminService) GetSourcePriority(ctx context.Context) ([]string, error) {
	state, err := s.LoadAugmentGatewayRegistryState(ctx)
	if err != nil {
		return nil, err
	}
	return append([]string(nil), state.SourcePriority...), nil
}

func (s *AugmentGatewayAdminService) UpdateSourcePriority(ctx context.Context, sources []string, meta AugmentGatewaySettingsMutationMeta) (*AugmentGatewaySettingsVersion, error) {
	payload, err := json.Marshal(AugmentGatewaySourcePrioritySetting{
		Sources: normalizePoolSourcePriority(sources),
	})
	if err != nil {
		return nil, err
	}
	return s.store.Put(ctx, AugmentGatewaySettingsWriteInput{
		Namespace:       AugmentGatewaySourcePriorityNamespace,
		SettingsJSON:    payload,
		ExpectedVersion: meta.ExpectedVersion,
		ActorAdminID:    meta.ActorAdminID,
		RequestID:       strings.TrimSpace(meta.RequestID),
		Action:          AugmentGatewaySettingsActionUpdate,
	})
}

func (s *AugmentGatewayAdminService) UpdateProviderGroup(
	ctx context.Context,
	provider AugmentGatewayProvider,
	setting AugmentGatewayProviderGroupSetting,
	meta AugmentGatewaySettingsMutationMeta,
) (*AugmentGatewaySettingsVersion, error) {
	namespace := augmentGatewayNamespaceForProvider(provider)
	if namespace == "" {
		return nil, ErrAugmentGatewayProviderUnknown
	}
	if setting.GroupID > 0 && s.groupReader != nil {
		total, _, err := s.groupReader.GetAccountCount(ctx, setting.GroupID)
		if err != nil {
			return nil, err
		}
		if total <= 0 {
			return nil, ErrAugmentGatewayProviderGroupWithoutAccounts
		}
	}

	payload, err := json.Marshal(setting)
	if err != nil {
		return nil, err
	}
	return s.store.Put(ctx, AugmentGatewaySettingsWriteInput{
		Namespace:       namespace,
		SettingsJSON:    payload,
		ExpectedVersion: meta.ExpectedVersion,
		ActorAdminID:    meta.ActorAdminID,
		RequestID:       strings.TrimSpace(meta.RequestID),
		Action:          AugmentGatewaySettingsActionUpdate,
	})
}

func (s *AugmentGatewayAdminService) UpdateModel(
	ctx context.Context,
	modelID string,
	setting AugmentGatewayModelSetting,
	meta AugmentGatewaySettingsMutationMeta,
) (*AugmentGatewaySettingsVersion, error) {
	modelID = strings.TrimSpace(modelID)
	if _, ok := defaultAugmentGatewayModelByID(modelID); !ok {
		return nil, ErrAugmentGatewayModelUnknown
	}
	setting = normalizeAugmentGatewayModelSetting(setting)
	if setting.Enabled && setting.SmokeStatus != AugmentGatewaySmokeStatusPassed {
		return nil, ErrAugmentGatewayModelSmokeRequired
	}

	models := buildFallbackAugmentGatewayModelSettings(s.fallback)
	if s != nil && s.store != nil {
		record, err := s.store.GetLatest(ctx, AugmentGatewayEnabledModelsNamespace)
		if err != nil {
			return nil, err
		}
		if record != nil && len(record.SettingsJSON) > 0 {
			if err := json.Unmarshal(record.SettingsJSON, &models); err != nil {
				return nil, err
			}
		}
	}
	models[modelID] = setting
	payload, err := json.Marshal(models)
	if err != nil {
		return nil, err
	}
	return s.store.Put(ctx, AugmentGatewaySettingsWriteInput{
		Namespace:       AugmentGatewayEnabledModelsNamespace,
		SettingsJSON:    payload,
		ExpectedVersion: meta.ExpectedVersion,
		ActorAdminID:    meta.ActorAdminID,
		RequestID:       strings.TrimSpace(meta.RequestID),
		Action:          AugmentGatewaySettingsActionUpdate,
	})
}

func (s *AugmentGatewayAdminService) RollbackNamespace(
	ctx context.Context,
	namespace string,
	targetVersion int64,
	meta AugmentGatewaySettingsMutationMeta,
) (*AugmentGatewaySettingsVersion, error) {
	return s.store.Rollback(ctx, AugmentGatewaySettingsRollbackInput{
		Namespace:       strings.TrimSpace(namespace),
		TargetVersion:   targetVersion,
		ExpectedVersion: meta.ExpectedVersion,
		ActorAdminID:    meta.ActorAdminID,
		RequestID:       strings.TrimSpace(meta.RequestID),
	})
}

func (s *AugmentGatewayAdminService) ListProviderGroups(ctx context.Context) ([]AugmentGatewayProviderRuntime, error) {
	state, err := s.LoadAugmentGatewayRegistryState(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AugmentGatewayProviderRuntime, 0, len(state.ProviderGroups))
	for _, provider := range augmentGatewayProviderOrder() {
		runtime := state.ProviderGroups[provider]
		runtime.Provider = provider
		out = append(out, runtime)
	}
	return out, nil
}

func (s *AugmentGatewayAdminService) ListModels(ctx context.Context) ([]AugmentGatewayManagedModel, error) {
	state, err := s.LoadAugmentGatewayRegistryState(ctx)
	if err != nil {
		return nil, err
	}
	registry := NewAugmentGatewayModelRegistry(s.fallback, WithAugmentGatewayRegistryStateSource(s))
	version := int64(0)
	if s != nil && s.store != nil {
		record, err := s.store.GetLatest(ctx, AugmentGatewayEnabledModelsNamespace)
		if err != nil {
			return nil, err
		}
		if record != nil {
			version = record.Version
		}
	}
	out := make([]AugmentGatewayManagedModel, 0, len(defaultAugmentGatewayModels))
	for _, model := range defaultAugmentGatewayModels {
		setting := state.Models[model.ID]
		providerState := state.ProviderGroups[model.Provider]
		model.ProviderGroupID = providerState.GroupID
		out = append(out, AugmentGatewayManagedModel{
			Model:           model,
			Enabled:         setting.Enabled,
			Visible:         registry.IsVisible(model.ID),
			SmokeStatus:     setting.SmokeStatus,
			ProviderHealthy: providerState.Healthy,
			SettingsNamespace: AugmentGatewayEnabledModelsNamespace,
			SettingsVersion:   version,
		})
	}
	return out, nil
}

type AugmentGatewayManagedModel struct {
	Model             AugmentGatewayModel       `json:"model"`
	Enabled           bool                      `json:"enabled"`
	Visible           bool                      `json:"visible"`
	SmokeStatus       AugmentGatewaySmokeStatus `json:"smoke_status"`
	ProviderHealthy   bool                      `json:"provider_healthy"`
	SettingsNamespace string                    `json:"settings_namespace"`
	SettingsVersion   int64                     `json:"settings_version"`
}

func augmentGatewayNamespaceForProvider(provider AugmentGatewayProvider) string {
	switch provider {
	case AugmentGatewayProviderOpenAI:
		return AugmentGatewayProviderGroupOpenAINamespace
	case AugmentGatewayProviderDeepSeek:
		return AugmentGatewayProviderGroupDeepSeekNamespace
	case AugmentGatewayProviderAnthropic:
		return AugmentGatewayProviderGroupAnthropicNamespace
	case AugmentGatewayProviderGemini:
		return AugmentGatewayProviderGroupGeminiNamespace
	default:
		return ""
	}
}

func augmentGatewayProviderFromNamespace(namespace string) (AugmentGatewayProvider, bool) {
	switch strings.TrimSpace(namespace) {
	case AugmentGatewayProviderGroupOpenAINamespace:
		return AugmentGatewayProviderOpenAI, true
	case AugmentGatewayProviderGroupDeepSeekNamespace:
		return AugmentGatewayProviderDeepSeek, true
	case AugmentGatewayProviderGroupAnthropicNamespace:
		return AugmentGatewayProviderAnthropic, true
	case AugmentGatewayProviderGroupGeminiNamespace:
		return AugmentGatewayProviderGemini, true
	default:
		return "", false
	}
}

func buildFallbackAugmentGatewayProviderGroups(fallback config.GatewayAugmentConfig) map[AugmentGatewayProvider]AugmentGatewayProviderRuntime {
	return map[AugmentGatewayProvider]AugmentGatewayProviderRuntime{
		AugmentGatewayProviderOpenAI: {
			Provider: AugmentGatewayProviderOpenAI,
			GroupID:  fallback.ProviderGroups.OpenAI,
			Healthy:  fallback.ProviderGroups.OpenAI > 0,
		},
		AugmentGatewayProviderDeepSeek: {
			Provider: AugmentGatewayProviderDeepSeek,
			GroupID:  fallback.ProviderGroups.DeepSeek,
			Healthy:  fallback.ProviderGroups.DeepSeek > 0,
		},
		AugmentGatewayProviderAnthropic: {
			Provider: AugmentGatewayProviderAnthropic,
			GroupID:  fallback.ProviderGroups.Anthropic,
			Healthy:  fallback.ProviderGroups.Anthropic > 0,
		},
		AugmentGatewayProviderGemini: {
			Provider: AugmentGatewayProviderGemini,
			GroupID:  fallback.ProviderGroups.Gemini,
			Healthy:  fallback.ProviderGroups.Gemini > 0,
		},
	}
}

func buildFallbackAugmentGatewayModelSettings(fallback config.GatewayAugmentConfig) map[string]AugmentGatewayModelSetting {
	settings := make(map[string]AugmentGatewayModelSetting, len(defaultAugmentGatewayModels))
	for _, model := range defaultAugmentGatewayModels {
		settings[model.ID] = AugmentGatewayModelSetting{
			Enabled:     false,
			SmokeStatus: AugmentGatewaySmokeStatusPending,
		}
	}
	enabledIDs := fallback.EnabledModels
	if len(enabledIDs) == 0 {
		enabledIDs = defaultAugmentGatewayEnabledModelIDs()
	}
	for _, modelID := range enabledIDs {
		if _, ok := settings[modelID]; !ok {
			continue
		}
		settings[modelID] = AugmentGatewayModelSetting{
			Enabled:     true,
			SmokeStatus: AugmentGatewaySmokeStatusPassed,
		}
	}
	return settings
}

func normalizeAugmentGatewayModelSetting(setting AugmentGatewayModelSetting) AugmentGatewayModelSetting {
	switch setting.SmokeStatus {
	case AugmentGatewaySmokeStatusPassed, AugmentGatewaySmokeStatusFailed:
		return setting
	case AugmentGatewaySmokeStatusPending:
		return setting
	default:
		setting.SmokeStatus = AugmentGatewaySmokeStatusPending
		return setting
	}
}

func defaultAugmentGatewayModelByID(modelID string) (AugmentGatewayModel, bool) {
	for _, model := range defaultAugmentGatewayModels {
		if model.ID == modelID {
			return model, true
		}
	}
	return AugmentGatewayModel{}, false
}

func augmentGatewayProviderOrder() []AugmentGatewayProvider {
	return []AugmentGatewayProvider{
		AugmentGatewayProviderOpenAI,
		AugmentGatewayProviderDeepSeek,
		AugmentGatewayProviderAnthropic,
		AugmentGatewayProviderGemini,
	}
}
