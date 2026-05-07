package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type AugmentGatewayProviderExecutor interface {
	Complete(ctx context.Context, req AugmentGatewayProviderRequest) (AugmentGatewayProviderResult, error)
	Stream(ctx context.Context, req AugmentGatewayProviderRequest, emit func(AugmentGatewayProviderChunk) error) error
}

type AugmentGatewayProviderExecutorImpl struct {
	cfg           *config.Config
	openaiGateway *OpenAIGatewayService
	anthropic     *GatewayService
	gemini        *GeminiMessagesCompatService
	turnStore     *AugmentGatewayReasoningTurnStore

	openAISelector    augmentGatewayAccountSelector
	deepSeekSelector  augmentGatewayAccountSelector
	anthropicSelector augmentGatewayAccountSelector
	geminiSelector    augmentGatewayAccountSelector

	openAIAdapter    augmentGatewayProviderAdapter
	deepSeekAdapter  augmentGatewayProviderAdapter
	anthropicAdapter augmentGatewayProviderAdapter
	geminiAdapter    augmentGatewayProviderAdapter
}

type AugmentGatewayProviderNotImplementedError struct {
	Provider  AugmentGatewayProvider
	Operation string
}

func (e *AugmentGatewayProviderNotImplementedError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("augment gateway %s forwarding for provider %q is not implemented", e.Operation, e.Provider)
}

func IsAugmentGatewayProviderNotImplemented(err error) (*AugmentGatewayProviderNotImplementedError, bool) {
	var typed *AugmentGatewayProviderNotImplementedError
	if errors.As(err, &typed) {
		return typed, true
	}
	return nil, false
}

type augmentGatewayAccountSelector interface {
	SelectAccountForModel(ctx context.Context, groupID *int64, sessionHash string, requestedModel string) (*Account, error)
}

type augmentGatewayProviderAdapter interface {
	Complete(ctx context.Context, req AugmentGatewayProviderRequest) (AugmentGatewayProviderResult, error)
	Stream(ctx context.Context, req AugmentGatewayProviderRequest, emit func(AugmentGatewayProviderChunk) error) error
}

func NewAugmentGatewayProviderExecutor(
	cfg *config.Config,
	openaiGateway *OpenAIGatewayService,
	anthropic *GatewayService,
	gemini *GeminiMessagesCompatService,
	turnStore *AugmentGatewayReasoningTurnStore,
) AugmentGatewayProviderExecutor {
	executor := &AugmentGatewayProviderExecutorImpl{
		cfg:           cfg,
		openaiGateway: openaiGateway,
		anthropic:     anthropic,
		gemini:        gemini,
		turnStore:     turnStore,
		openAIAdapter: &augmentGatewayOpenAIAdapter{
			gateway: openaiGateway,
		},
		deepSeekAdapter: &augmentGatewayOpenAIAdapter{
			provider: AugmentGatewayProviderDeepSeek,
			gateway:  openaiGateway,
		},
		anthropicAdapter: &augmentGatewayAnthropicAdapter{},
		geminiAdapter:    &augmentGatewayGeminiAdapter{},
	}
	executor.openAISelector = openaiGateway
	executor.deepSeekSelector = openaiGateway
	executor.anthropicSelector = anthropic
	executor.geminiSelector = gemini
	return executor
}

func (e *AugmentGatewayProviderExecutorImpl) Complete(ctx context.Context, req AugmentGatewayProviderRequest) (AugmentGatewayProviderResult, error) {
	prepared, adapter, err := e.prepareProviderRequest(ctx, req)
	if err != nil {
		return AugmentGatewayProviderResult{}, err
	}
	result, err := adapter.Complete(ctx, prepared)
	if err != nil {
		return AugmentGatewayProviderResult{}, err
	}
	return e.normalizeProviderResult(prepared, result), nil
}

func (e *AugmentGatewayProviderExecutorImpl) Stream(ctx context.Context, req AugmentGatewayProviderRequest, emit func(AugmentGatewayProviderChunk) error) error {
	if emit == nil {
		return fmt.Errorf("augment gateway provider stream requires emit callback")
	}
	prepared, adapter, err := e.prepareProviderRequest(ctx, req)
	if err != nil {
		return err
	}
	return adapter.Stream(ctx, prepared, func(chunk AugmentGatewayProviderChunk) error {
		return emit(e.normalizeProviderChunk(prepared, chunk))
	})
}

func (e *AugmentGatewayProviderExecutorImpl) prepareProviderRequest(ctx context.Context, req AugmentGatewayProviderRequest) (AugmentGatewayProviderRequest, augmentGatewayProviderAdapter, error) {
	req = normalizeAugmentGatewayProviderRequest(req)
	groupID, selector, adapter, err := e.providerDependencies(req)
	if err != nil {
		return AugmentGatewayProviderRequest{}, nil, err
	}
	if groupID == 0 {
		return AugmentGatewayProviderRequest{}, nil, &AugmentGatewayProviderUnavailableError{
			ModelID:  req.ModelID,
			Provider: req.Provider,
			Kind:     AugmentGatewayProviderUnavailableNoProviderGroup,
		}
	}
	if selector == nil {
		return AugmentGatewayProviderRequest{}, nil, fmt.Errorf("augment gateway provider %q has no account selector", req.Provider)
	}
	if adapter == nil {
		return AugmentGatewayProviderRequest{}, nil, fmt.Errorf("augment gateway provider %q has no adapter", req.Provider)
	}

	account, err := selector.SelectAccountForModel(ctx, &groupID, req.SessionHash, req.UpstreamModel)
	if err != nil {
		return AugmentGatewayProviderRequest{}, nil, err
	}
	req.Account = account
	req.ProviderGroupID = groupID

	if req.Provider == AugmentGatewayProviderDeepSeek {
		body, err := SanitizeAugmentGatewayDeepSeekChatCompletionsRequest(req.Model, req.RawBody)
		if err != nil {
			return AugmentGatewayProviderRequest{}, nil, err
		}
		req.RawBody = body
	}

	return req, adapter, nil
}

func (e *AugmentGatewayProviderExecutorImpl) providerDependencies(req AugmentGatewayProviderRequest) (int64, augmentGatewayAccountSelector, augmentGatewayProviderAdapter, error) {
	groups := config.GatewayAugmentProviderGroupsConfig{}
	if e != nil && e.cfg != nil {
		groups = e.cfg.Gateway.Augment.ProviderGroups
	}

	switch req.Provider {
	case AugmentGatewayProviderOpenAI:
		return groups.OpenAI, firstAugmentGatewaySelector(e.openAISelector, e.openaiGateway), e.openAIAdapter, nil
	case AugmentGatewayProviderDeepSeek:
		return groups.DeepSeek, firstAugmentGatewaySelector(e.deepSeekSelector, e.openaiGateway), e.deepSeekAdapter, nil
	case AugmentGatewayProviderAnthropic:
		return groups.Anthropic, firstAugmentGatewaySelector(e.anthropicSelector, e.anthropic), e.anthropicAdapter, nil
	case AugmentGatewayProviderGemini:
		return groups.Gemini, firstAugmentGatewaySelector(e.geminiSelector, e.gemini), e.geminiAdapter, nil
	default:
		return 0, nil, nil, fmt.Errorf("augment gateway provider %q is unsupported", req.Provider)
	}
}

func firstAugmentGatewaySelector(primary augmentGatewayAccountSelector, fallback augmentGatewayAccountSelector) augmentGatewayAccountSelector {
	if primary != nil {
		return primary
	}
	return fallback
}

func normalizeAugmentGatewayProviderRequest(req AugmentGatewayProviderRequest) AugmentGatewayProviderRequest {
	req.Endpoint = strings.TrimSpace(req.Endpoint)
	req.ConversationID = strings.TrimSpace(req.ConversationID)
	req.RequestID = strings.TrimSpace(req.RequestID)
	req.AssistantTurnID = strings.TrimSpace(req.AssistantTurnID)
	req.SessionHash = strings.TrimSpace(req.SessionHash)

	req.Model.ID = strings.TrimSpace(req.Model.ID)
	req.Model.UpstreamModel = strings.TrimSpace(req.Model.UpstreamModel)
	req.Model.ReasoningEffort = strings.TrimSpace(req.Model.ReasoningEffort)

	req.Provider = req.Model.Provider
	req.ModelID = req.Model.ID
	req.UpstreamModel = req.Model.UpstreamModel
	if req.UpstreamModel == "" {
		req.UpstreamModel = req.ModelID
		req.Model.UpstreamModel = req.UpstreamModel
	}

	if req.RawBody == nil {
		req.RawBody = augmentGatewayProviderRequestBodyFromParts(req)
	} else {
		req.RawBody = cloneAugmentGatewayRawMap(req.RawBody)
	}
	req.Metadata = cloneAugmentGatewayRawMap(req.Metadata)
	return req
}

func augmentGatewayProviderRequestBodyFromParts(req AugmentGatewayProviderRequest) map[string]any {
	body := map[string]any{}
	if req.UpstreamModel != "" {
		body["model"] = req.UpstreamModel
	}
	if len(req.Messages) > 0 {
		body["messages"] = augmentGatewayProviderAnySliceFromMaps(req.Messages)
	}
	if len(req.Tools) > 0 {
		body["tools"] = augmentGatewayProviderAnySliceFromMaps(req.Tools)
	}
	return body
}

func augmentGatewayProviderAnySliceFromMaps(in []map[string]any) []any {
	out := make([]any, 0, len(in))
	for _, item := range in {
		out = append(out, cloneAugmentGatewayRawMap(item))
	}
	return out
}

func cloneAugmentGatewayRawMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	raw, err := json.Marshal(in)
	if err != nil {
		out := make(map[string]any, len(in))
		for k, v := range in {
			out[k] = v
		}
		return out
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		out = make(map[string]any, len(in))
		for k, v := range in {
			out[k] = v
		}
	}
	return out
}

func (e *AugmentGatewayProviderExecutorImpl) normalizeProviderResult(req AugmentGatewayProviderRequest, result AugmentGatewayProviderResult) AugmentGatewayProviderResult {
	if result.Provider == "" {
		result.Provider = req.Provider
	}
	if result.ModelID == "" {
		result.ModelID = req.ModelID
	}
	if result.UpstreamModel == "" {
		result.UpstreamModel = req.UpstreamModel
	}
	if result.RequestID == "" {
		result.RequestID = req.RequestID
	}
	if result.Metadata == nil {
		result.Metadata = map[string]any{}
	}
	result.Metadata["augment_endpoint"] = req.Endpoint
	result.Metadata["augment_provider_group_id"] = req.ProviderGroupID
	return result
}

func (e *AugmentGatewayProviderExecutorImpl) normalizeProviderChunk(req AugmentGatewayProviderRequest, chunk AugmentGatewayProviderChunk) AugmentGatewayProviderChunk {
	if chunk.Provider == "" {
		chunk.Provider = req.Provider
	}
	if chunk.ModelID == "" {
		chunk.ModelID = req.ModelID
	}
	if chunk.UpstreamModel == "" {
		chunk.UpstreamModel = req.UpstreamModel
	}
	if chunk.RequestID == "" {
		chunk.RequestID = req.RequestID
	}
	if chunk.Metadata == nil {
		chunk.Metadata = map[string]any{}
	}
	chunk.Metadata["augment_endpoint"] = req.Endpoint
	chunk.Metadata["augment_provider_group_id"] = req.ProviderGroupID
	return chunk
}
