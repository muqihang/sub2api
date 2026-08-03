package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
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
	usageRecorder augmentGatewayUsageRecorder

	openAISelector    augmentGatewayAccountSelector
	deepSeekSelector  augmentGatewayAccountSelector
	anthropicSelector augmentGatewayAccountSelector
	geminiSelector    augmentGatewayAccountSelector

	openAIAdapter    augmentGatewayProviderAdapter
	deepSeekAdapter  augmentGatewayProviderAdapter
	anthropicAdapter augmentGatewayProviderAdapter
	geminiAdapter    augmentGatewayProviderAdapter
}

type augmentGatewayUsageRecorder interface {
	RecordUsage(ctx context.Context, input *OpenAIRecordUsageInput) error
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
		usageRecorder: openaiGateway,
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
	startedAt := time.Now()
	result, err := adapter.Complete(ctx, prepared)
	if err != nil {
		return AugmentGatewayProviderResult{}, err
	}
	result = e.normalizeProviderResult(prepared, result)
	e.recordUsageBestEffort(ctx, prepared, result.Usage, result.RequestID, result.UpstreamRequestID, time.Since(startedAt), false)
	return result, nil
}

func (e *AugmentGatewayProviderExecutorImpl) Stream(ctx context.Context, req AugmentGatewayProviderRequest, emit func(AugmentGatewayProviderChunk) error) error {
	if emit == nil {
		return fmt.Errorf("augment gateway provider stream requires emit callback")
	}
	prepared, adapter, err := e.prepareProviderRequest(ctx, req)
	if err != nil {
		return err
	}
	startedAt := time.Now()
	var streamUsage AugmentGatewayProviderUsage
	var requestID string
	var upstreamRequestID string
	err = adapter.Stream(ctx, prepared, func(chunk AugmentGatewayProviderChunk) error {
		chunk = e.normalizeProviderChunk(prepared, chunk)
		if augmentGatewayProviderUsagePresent(chunk.Usage) {
			streamUsage = chunk.Usage
		}
		if strings.TrimSpace(chunk.RequestID) != "" {
			requestID = strings.TrimSpace(chunk.RequestID)
		}
		if strings.TrimSpace(chunk.UpstreamRequestID) != "" {
			upstreamRequestID = strings.TrimSpace(chunk.UpstreamRequestID)
		}
		return emit(chunk)
	})
	if err != nil {
		return err
	}
	e.recordUsageBestEffort(ctx, prepared, streamUsage, requestID, upstreamRequestID, time.Since(startedAt), true)
	return nil
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
		ApplyAugmentGatewayDeepSeekStableUserID(&req)
	}
	if req.Provider == AugmentGatewayProviderOpenAI {
		ApplyAugmentGatewayOpenAICacheHints(&req)
	}
	if req.Provider == AugmentGatewayProviderAnthropic {
		ApplyAugmentGatewayAnthropicCacheControl(&req)
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
		return augmentGatewayResolvedProviderGroupID(groups.OpenAI, req.Model.ProviderGroupID), firstAugmentGatewaySelector(e.openAISelector, e.openaiGateway), e.openAIAdapter, nil
	case AugmentGatewayProviderDeepSeek:
		return augmentGatewayResolvedProviderGroupID(groups.DeepSeek, req.Model.ProviderGroupID), firstAugmentGatewaySelector(e.deepSeekSelector, e.openaiGateway), e.deepSeekAdapter, nil
	case AugmentGatewayProviderAnthropic:
		return augmentGatewayResolvedProviderGroupID(groups.Anthropic, req.Model.ProviderGroupID), firstAugmentGatewaySelector(e.anthropicSelector, e.anthropic), e.anthropicAdapter, nil
	case AugmentGatewayProviderGemini:
		return augmentGatewayResolvedProviderGroupID(groups.Gemini, req.Model.ProviderGroupID), firstAugmentGatewaySelector(e.geminiSelector, e.gemini), e.geminiAdapter, nil
	default:
		return 0, nil, nil, fmt.Errorf("augment gateway provider %q is unsupported", req.Provider)
	}
}

func augmentGatewayResolvedProviderGroupID(configuredGroupID int64, routedGroupID int64) int64 {
	if configuredGroupID > 0 {
		return configuredGroupID
	}
	return routedGroupID
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
	req.UserAgent = strings.TrimSpace(req.UserAgent)
	req.IPAddress = strings.TrimSpace(req.IPAddress)

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

func (e *AugmentGatewayProviderExecutorImpl) recordUsageBestEffort(
	ctx context.Context,
	req AugmentGatewayProviderRequest,
	usage AugmentGatewayProviderUsage,
	requestID string,
	upstreamRequestID string,
	duration time.Duration,
	stream bool,
) {
	if e == nil || e.usageRecorder == nil {
		return
	}
	if !augmentGatewayProviderUsagePresent(usage) {
		return
	}
	apiKey := req.APIKey
	if apiKey == nil {
		return
	}
	user := req.User
	if user == nil {
		user = apiKey.User
	}
	if user == nil {
		return
	}
	account := req.Account
	if account == nil {
		return
	}

	rawBody, _ := json.Marshal(req.RawBody)
	modelID := firstNonBlankAugmentGatewayString(req.ModelID, req.Model.ID)
	upstreamModel := firstNonBlankAugmentGatewayString(req.UpstreamModel, req.Model.UpstreamModel, modelID)
	resultRequestID := firstNonBlankAugmentGatewayString(upstreamRequestID, requestID, req.RequestID)
	var reasoningEffort *string
	if effort := strings.TrimSpace(req.Model.ReasoningEffort); effort != "" {
		reasoningEffort = &effort
	}

	if err := e.usageRecorder.RecordUsage(ctx, &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID: resultRequestID,
			Usage: OpenAIUsage{
				InputTokens:          usage.InputTokens,
				OutputTokens:         usage.OutputTokens,
				CacheReadInputTokens: usage.CachedInputTokens,
			},
			Model:           modelID,
			UpstreamModel:   upstreamModel,
			ReasoningEffort: reasoningEffort,
			Stream:          stream,
			Duration:        duration,
		},
		APIKey:             apiKey,
		User:               user,
		Account:            account,
		Subscription:       req.Subscription,
		InboundEndpoint:    req.Endpoint,
		UpstreamEndpoint:   augmentGatewayUsageUpstreamEndpoint(req.Provider),
		UserAgent:          req.UserAgent,
		IPAddress:          req.IPAddress,
		RequestPayloadHash: HashUsageRequestPayload(rawBody),
		AugmentUsageFields: augmentGatewayUsageFields(req, resultRequestID),
	}); err != nil {
		logger.LegacyPrintf(
			"service.augment_gateway",
			"usage_record_failed provider=%s model=%s upstream_model=%s endpoint=%s stream=%t input_tokens=%d output_tokens=%d cache_read_tokens=%d err=%v",
			req.Provider,
			modelID,
			upstreamModel,
			req.Endpoint,
			stream,
			usage.InputTokens,
			usage.OutputTokens,
			usage.CachedInputTokens,
			err,
		)
	}
}

func augmentGatewayUsageFields(req AugmentGatewayProviderRequest, upstreamAttemptID string) AugmentUsageFields {
	clientProduct := AugmentUsageClientProduct
	requestScope := AugmentUsageRequestScopeGateway
	featureScope := augmentGatewayFeatureScope(req.Endpoint)
	routePolicyVersion := AugmentOfficialRoutePolicyVersion
	billable := true
	if ClassifyAugmentOfficialRoute(req.Endpoint) == AugmentOfficialRouteOwnerOfficialCloud {
		requestScope = AugmentUsageRequestScopeOfficial
		billable = false
	}

	fields := AugmentUsageFields{
		ClientProduct:      &clientProduct,
		RequestScope:       &requestScope,
		FeatureScope:       &featureScope,
		RoutePolicyVersion: &routePolicyVersion,
		Billable:           &billable,
		CostSource:         optionalTrimmedStringPtr(AugmentUsageCostSourceProviderUsage),
		Currency:           optionalTrimmedStringPtr(AugmentUsageCurrencyUSD),
		UpstreamAttemptID:  optionalTrimmedStringPtr(strings.TrimSpace(upstreamAttemptID)),
	}

	if sessionID := firstNonBlankAugmentGatewayString(req.ConversationID, req.SessionHash); sessionID != "" {
		fields.AugmentSessionID = &sessionID
	}
	return fields
}

func augmentGatewayFeatureScope(endpoint string) string {
	switch normalizeAugmentOfficialRoutePath(endpoint) {
	case "/chat", "/chat-stream":
		return AugmentUsageFeatureScopeChat
	case "/agents/codebase-retrieval":
		return AugmentUsageFeatureScopeContextEngine
	case "/prompt-enhancer":
		return AugmentUsageFeatureScopePromptEnhancer
	case "/instruction-stream":
		return AugmentUsageFeatureScopeInstruction
	case "/smart-paste-stream":
		return AugmentUsageFeatureScopeSmartPaste
	case "/generate-commit-message-stream":
		return AugmentUsageFeatureScopeCommitMessage
	case "/next_edit_loc", "/next-edit-stream":
		return AugmentUsageFeatureScopeNextEdit
	default:
		return "unknown"
	}
}

func augmentGatewayProviderUsagePresent(usage AugmentGatewayProviderUsage) bool {
	return usage.InputTokens > 0 ||
		usage.OutputTokens > 0 ||
		usage.TotalTokens > 0 ||
		usage.CachedInputTokens > 0 ||
		usage.ReasoningTokens > 0
}

func augmentGatewayUsageUpstreamEndpoint(provider AugmentGatewayProvider) string {
	switch provider {
	case AugmentGatewayProviderAnthropic:
		return "/v1/messages"
	case AugmentGatewayProviderGemini:
		return "/v1beta/models:generateContent"
	default:
		return "/v1/chat/completions"
	}
}

func firstNonBlankAugmentGatewayString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
