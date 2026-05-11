package service

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	CodexGatewayProviderGroupOpenAINamespace   = "gateway.codex.provider_groups.openai"
	CodexGatewayProviderGroupDeepSeekNamespace = "gateway.codex.provider_groups.deepseek"
	CodexGatewayEnabledModelsNamespace         = "gateway.codex.enabled_models"
)

type CodexGatewayProvider string

const (
	CodexGatewayProviderOpenAI   CodexGatewayProvider = "openai"
	CodexGatewayProviderDeepSeek CodexGatewayProvider = "deepseek"
)

type CodexGatewaySmokeStatus string

const (
	CodexGatewaySmokeStatusPending CodexGatewaySmokeStatus = "pending"
	CodexGatewaySmokeStatusPassed  CodexGatewaySmokeStatus = "passed"
	CodexGatewaySmokeStatusFailed  CodexGatewaySmokeStatus = "failed"
)

var (
	ErrCodexGatewayProviderUnknown = infraerrors.BadRequest(
		"CODEX_GATEWAY_PROVIDER_UNKNOWN",
		"codex gateway provider is unknown",
	)
	ErrCodexGatewayModelUnknown = infraerrors.BadRequest(
		"CODEX_GATEWAY_MODEL_UNKNOWN",
		"codex gateway model is unknown",
	)
)

type CodexGatewayProviderRuntime struct {
	Provider  CodexGatewayProvider `json:"provider"`
	Namespace string               `json:"namespace,omitempty"`
	Version   int64                `json:"version,omitempty"`
	GroupID   int64                `json:"group_id"`
	Healthy   bool                 `json:"healthy"`
}

type CodexGatewayManagedModel struct {
	Model           CodexGatewayModel       `json:"model"`
	Namespace       string                  `json:"namespace,omitempty"`
	Version         int64                   `json:"version,omitempty"`
	Enabled         bool                    `json:"enabled"`
	Visible         bool                    `json:"visible"`
	SmokeStatus     CodexGatewaySmokeStatus `json:"smoke_status"`
	ProviderHealthy bool                    `json:"provider_healthy"`
	ProviderGroupID int64                   `json:"provider_group_id,omitempty"`
}

type CodexGatewayModelMutation struct {
	Enabled     bool                    `json:"enabled"`
	SmokeStatus CodexGatewaySmokeStatus `json:"smoke_status,omitempty"`
}

type CodexGatewaySmokeRequest struct {
	ModelID   string               `json:"model_id"`
	Provider  CodexGatewayProvider `json:"provider,omitempty"`
	RequestID string               `json:"request_id,omitempty"`
}

type CodexGatewaySmokeResult struct {
	Status    string               `json:"status"`
	ModelID   string               `json:"model_id,omitempty"`
	Provider  CodexGatewayProvider `json:"provider,omitempty"`
	RequestID string               `json:"request_id,omitempty"`
	Message   string               `json:"message,omitempty"`
	CreatedAt time.Time            `json:"created_at,omitempty"`
}

type CodexGatewayStateStoreSummary struct {
	EntryCount  int    `json:"entry_count"`
	TTLSeconds  int64  `json:"ttl_seconds"`
	MaxItems    int    `json:"max_items"`
	OldestEntry string `json:"oldest_entry,omitempty"`
	NewestEntry string `json:"newest_entry,omitempty"`
}

type CodexGatewayAdminSummary struct {
	ProviderGroups     []CodexGatewayProviderRuntime `json:"provider_groups"`
	Models             []CodexGatewayManagedModel    `json:"models"`
	StateStore         CodexGatewayStateStoreSummary `json:"state_store"`
	ProviderGroupCount int                           `json:"provider_group_count"`
	EnabledModelCount  int                           `json:"enabled_model_count"`
	VisibleModelCount  int                           `json:"visible_model_count"`
	StateEntryCount    int                           `json:"state_entry_count"`
}

type CodexGatewayAdminService struct {
	mu              sync.RWMutex
	fallback        config.GatewayCodexConfig
	stateStore      *CodexGatewayStateStore
	providerGroups  map[CodexGatewayProvider]CodexGatewayProviderRuntime
	models          map[string]CodexGatewayModelMutation
	versions        map[string]int64
	pricingChecker  CodexGatewayPricingReadyChecker
	protocolChecker CodexGatewayProtocolReadyChecker
}

func NewCodexGatewayAdminService(cfg config.GatewayCodexConfig, stateStore *CodexGatewayStateStore) *CodexGatewayAdminService {
	if len(cfg.EnabledModels) == 0 {
		cfg.EnabledModels = defaultCodexGatewayEnabledModelSlugs()
	}
	return &CodexGatewayAdminService{
		fallback:        cfg,
		stateStore:      stateStore,
		providerGroups:  buildFallbackCodexGatewayProviderGroups(cfg),
		models:          buildFallbackCodexGatewayModelMutations(cfg),
		versions:        make(map[string]int64),
		pricingChecker:  defaultCodexGatewayPricingReadyChecker,
		protocolChecker: defaultCodexGatewayProtocolReadyChecker,
	}
}

func (s *CodexGatewayAdminService) LoadCodexGatewayRegistryState(context.Context) (*CodexGatewayRegistryState, error) {
	if s == nil {
		return &CodexGatewayRegistryState{}, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	return &CodexGatewayRegistryState{
		ProviderGroups: cloneCodexGatewayProviderRuntimeMap(s.providerGroups),
		Models:         cloneCodexGatewayModelMutationMap(s.models),
	}, nil
}

func (s *CodexGatewayAdminService) GetSummary(ctx context.Context) (*CodexGatewayAdminSummary, error) {
	providerGroups, err := s.ListProviderGroups(ctx)
	if err != nil {
		return nil, err
	}
	models, err := s.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	stateStoreSummary, err := s.GetStateStoreSummary(ctx)
	if err != nil {
		return nil, err
	}

	summary := &CodexGatewayAdminSummary{
		ProviderGroups: providerGroups,
		Models:         models,
	}
	if stateStoreSummary != nil {
		summary.StateStore = *stateStoreSummary
		summary.StateEntryCount = stateStoreSummary.EntryCount
	}
	summary.ProviderGroupCount = len(providerGroups)
	for _, model := range models {
		if model.Enabled {
			summary.EnabledModelCount++
		}
		if model.Visible {
			summary.VisibleModelCount++
		}
	}
	return summary, nil
}

func (s *CodexGatewayAdminService) ListProviderGroups(context.Context) ([]CodexGatewayProviderRuntime, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows := []CodexGatewayProviderRuntime{
		s.providerGroups[CodexGatewayProviderOpenAI],
		s.providerGroups[CodexGatewayProviderDeepSeek],
	}
	return rows, nil
}

func (s *CodexGatewayAdminService) UpdateProviderGroup(_ context.Context, provider CodexGatewayProvider, groupID int64) (*CodexGatewayProviderRuntime, error) {
	if s == nil {
		return nil, nil
	}
	provider = normalizeCodexGatewayProvider(provider)
	namespace, ok := codexGatewayProviderNamespace(provider)
	if !ok {
		return nil, ErrCodexGatewayProviderUnknown
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	runtime := s.providerGroups[provider]
	runtime.Provider = provider
	runtime.Namespace = namespace
	runtime.GroupID = groupID
	runtime.Healthy = groupID > 0
	s.versions[namespace]++
	runtime.Version = s.versions[namespace]
	s.providerGroups[provider] = runtime

	return cloneCodexGatewayProviderRuntime(runtime), nil
}

func (s *CodexGatewayAdminService) ListModels(context.Context) ([]CodexGatewayManagedModel, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	knownModels := defaultCodexGatewayModels()
	rows := make([]CodexGatewayManagedModel, 0, len(knownModels))
	for _, model := range knownModels {
		mutation, ok := s.models[model.Slug]
		if !ok {
			mutation = CodexGatewayModelMutation{SmokeStatus: CodexGatewaySmokeStatusPending}
		}
		provider := normalizeCodexGatewayProvider(CodexGatewayProvider(model.Provider))
		providerRuntime := s.providerGroups[provider]
		effectiveModel := codexGatewayApplyVisibilityGates(model, mutation, providerRuntime, s.pricingChecker, s.protocolChecker)
		rows = append(rows, CodexGatewayManagedModel{
			Model:           effectiveModel,
			Namespace:       CodexGatewayEnabledModelsNamespace,
			Version:         s.versions[CodexGatewayEnabledModelsNamespace],
			Enabled:         mutation.Enabled,
			Visible:         codexGatewayModelVisible(effectiveModel),
			SmokeStatus:     normalizeCodexGatewaySmokeStatus(mutation.SmokeStatus),
			ProviderHealthy: providerRuntime.Healthy,
			ProviderGroupID: providerRuntime.GroupID,
		})
	}
	return rows, nil
}

func (s *CodexGatewayAdminService) UpdateModel(_ context.Context, modelID string, input CodexGatewayModelMutation) (*CodexGatewayManagedModel, error) {
	if s == nil {
		return nil, nil
	}
	model, ok := defaultCodexGatewayModelBySlug(modelID)
	if !ok {
		return nil, ErrCodexGatewayModelUnknown
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	current := s.models[model.Slug]
	current.Enabled = input.Enabled
	if strings.TrimSpace(string(input.SmokeStatus)) != "" {
		current.SmokeStatus = normalizeCodexGatewaySmokeStatus(input.SmokeStatus)
	} else if strings.TrimSpace(string(current.SmokeStatus)) == "" {
		current.SmokeStatus = CodexGatewaySmokeStatusPending
	}
	s.models[model.Slug] = current
	s.versions[CodexGatewayEnabledModelsNamespace]++

	providerRuntime := s.providerGroups[normalizeCodexGatewayProvider(CodexGatewayProvider(model.Provider))]
	effectiveModel := codexGatewayApplyVisibilityGates(model, current, providerRuntime, s.pricingChecker, s.protocolChecker)
	result := CodexGatewayManagedModel{
		Model:           effectiveModel,
		Namespace:       CodexGatewayEnabledModelsNamespace,
		Version:         s.versions[CodexGatewayEnabledModelsNamespace],
		Enabled:         current.Enabled,
		Visible:         codexGatewayModelVisible(effectiveModel),
		SmokeStatus:     current.SmokeStatus,
		ProviderHealthy: providerRuntime.Healthy,
		ProviderGroupID: providerRuntime.GroupID,
	}
	return &result, nil
}

func (s *CodexGatewayAdminService) Smoke(_ context.Context, input CodexGatewaySmokeRequest) (*CodexGatewaySmokeResult, error) {
	if s == nil {
		return nil, nil
	}
	modelID := strings.TrimSpace(input.ModelID)
	if modelID != "" {
		if _, ok := defaultCodexGatewayModelBySlug(modelID); !ok {
			return nil, ErrCodexGatewayModelUnknown
		}
	}
	result := &CodexGatewaySmokeResult{
		Status:    "accepted",
		ModelID:   modelID,
		Provider:  normalizeCodexGatewayProvider(input.Provider),
		RequestID: strings.TrimSpace(input.RequestID),
		Message:   "smoke execution is not wired in MVP",
		CreatedAt: time.Now().UTC(),
	}
	return result, nil
}

func (s *CodexGatewayAdminService) GetStateStoreSummary(context.Context) (*CodexGatewayStateStoreSummary, error) {
	if s == nil || s.stateStore == nil {
		return &CodexGatewayStateStoreSummary{}, nil
	}
	summary := s.stateStore.Summary()
	return &summary, nil
}

func (s *CodexGatewayStateStore) Summary() CodexGatewayStateStoreSummary {
	if s == nil {
		return CodexGatewayStateStoreSummary{}
	}
	now := s.nowTime()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)

	summary := CodexGatewayStateStoreSummary{
		EntryCount: len(s.entries),
		TTLSeconds: int64(s.ttl / time.Second),
		MaxItems:   s.max,
	}
	var oldestAt time.Time
	var newestAt time.Time
	for _, entry := range s.entries {
		if summary.OldestEntry == "" || entry.createdAt.Before(oldestAt) {
			summary.OldestEntry = entry.state.Key.ResponseID
			oldestAt = entry.createdAt
		}
		if summary.NewestEntry == "" || entry.createdAt.After(newestAt) {
			summary.NewestEntry = entry.state.Key.ResponseID
			newestAt = entry.createdAt
		}
	}
	return summary
}

func buildFallbackCodexGatewayProviderGroups(cfg config.GatewayCodexConfig) map[CodexGatewayProvider]CodexGatewayProviderRuntime {
	return map[CodexGatewayProvider]CodexGatewayProviderRuntime{
		CodexGatewayProviderOpenAI: {
			Provider:  CodexGatewayProviderOpenAI,
			Namespace: CodexGatewayProviderGroupOpenAINamespace,
			GroupID:   cfg.ProviderGroups.OpenAI,
			Healthy:   cfg.ProviderGroups.OpenAI > 0,
		},
		CodexGatewayProviderDeepSeek: {
			Provider:  CodexGatewayProviderDeepSeek,
			Namespace: CodexGatewayProviderGroupDeepSeekNamespace,
			GroupID:   cfg.ProviderGroups.DeepSeek,
			Healthy:   cfg.ProviderGroups.DeepSeek > 0,
		},
	}
}

func buildFallbackCodexGatewayModelMutations(cfg config.GatewayCodexConfig) map[string]CodexGatewayModelMutation {
	enabled := make(map[string]struct{}, len(cfg.EnabledModels))
	for _, slug := range cfg.EnabledModels {
		slug = strings.TrimSpace(slug)
		if slug == "" {
			continue
		}
		enabled[slug] = struct{}{}
	}
	rows := make(map[string]CodexGatewayModelMutation, len(defaultCodexGatewayModels()))
	for _, model := range defaultCodexGatewayModels() {
		_, isEnabled := enabled[model.Slug]
		status := CodexGatewaySmokeStatusPending
		if isEnabled {
			status = CodexGatewaySmokeStatusPassed
		}
		rows[model.Slug] = CodexGatewayModelMutation{
			Enabled:     isEnabled,
			SmokeStatus: status,
		}
	}
	return rows
}

func defaultCodexGatewayEnabledModelSlugs() []string {
	base := defaultCodexGatewayModels()
	rows := make([]string, 0, len(base))
	for _, model := range base {
		rows = append(rows, model.Slug)
	}
	return rows
}

func defaultCodexGatewayModelBySlug(slug string) (CodexGatewayModel, bool) {
	slug = strings.TrimSpace(slug)
	for _, model := range defaultCodexGatewayModels() {
		if model.Slug == slug {
			return model, true
		}
	}
	return CodexGatewayModel{}, false
}

func normalizeCodexGatewayProvider(provider CodexGatewayProvider) CodexGatewayProvider {
	switch strings.ToLower(strings.TrimSpace(string(provider))) {
	case string(CodexGatewayProviderOpenAI):
		return CodexGatewayProviderOpenAI
	case string(CodexGatewayProviderDeepSeek):
		return CodexGatewayProviderDeepSeek
	default:
		return CodexGatewayProvider(strings.ToLower(strings.TrimSpace(string(provider))))
	}
}

func codexGatewayProviderNamespace(provider CodexGatewayProvider) (string, bool) {
	switch normalizeCodexGatewayProvider(provider) {
	case CodexGatewayProviderOpenAI:
		return CodexGatewayProviderGroupOpenAINamespace, true
	case CodexGatewayProviderDeepSeek:
		return CodexGatewayProviderGroupDeepSeekNamespace, true
	default:
		return "", false
	}
}

func normalizeCodexGatewaySmokeStatus(status CodexGatewaySmokeStatus) CodexGatewaySmokeStatus {
	switch strings.ToLower(strings.TrimSpace(string(status))) {
	case string(CodexGatewaySmokeStatusPassed):
		return CodexGatewaySmokeStatusPassed
	case string(CodexGatewaySmokeStatusFailed):
		return CodexGatewaySmokeStatusFailed
	default:
		return CodexGatewaySmokeStatusPending
	}
}

func codexGatewayModelVisible(model CodexGatewayModel) bool {
	return model.SupportedInAPI && strings.TrimSpace(model.Visibility) == "visible"
}

func cloneCodexGatewayProviderRuntime(in CodexGatewayProviderRuntime) *CodexGatewayProviderRuntime {
	out := in
	return &out
}

func cloneCodexGatewayProviderRuntimeMap(in map[CodexGatewayProvider]CodexGatewayProviderRuntime) map[CodexGatewayProvider]CodexGatewayProviderRuntime {
	if len(in) == 0 {
		return map[CodexGatewayProvider]CodexGatewayProviderRuntime{}
	}
	out := make(map[CodexGatewayProvider]CodexGatewayProviderRuntime, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneCodexGatewayModelMutationMap(in map[string]CodexGatewayModelMutation) map[string]CodexGatewayModelMutation {
	if len(in) == 0 {
		return map[string]CodexGatewayModelMutation{}
	}
	out := make(map[string]CodexGatewayModelMutation, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
