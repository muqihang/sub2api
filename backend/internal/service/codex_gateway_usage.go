package service

import (
	"context"
	"strings"
	"time"
)

const (
	codexGatewayUsageRequestScope    = "gateway"
	codexGatewayUsagePricingVersion  = "codex_gateway_v1"
	codexGatewayUsageCostSource      = "provider_usage"
	codexGatewayUsageCurrency        = "USD"
	codexGatewayUsageSettlementState = "settled"
)

func codexGatewayProviderUsageToOpenAIUsage(usage CodexGatewayProviderUsage) OpenAIUsage {
	return OpenAIUsage{
		InputTokens:              usage.InputTokens,
		OutputTokens:             usage.OutputTokens,
		CacheCreationInputTokens: 0,
		CacheReadInputTokens:     usage.CacheReadInputTokens,
	}
}

func codexGatewayForwardResult(
	model CodexGatewayModel,
	parsed CodexGatewayResponsesCreateRequest,
	providerResult CodexGatewayProviderResult,
	stream bool,
	duration time.Duration,
) *OpenAIForwardResult {
	requestedModel := strings.TrimSpace(parsed.Model)
	if requestedModel == "" {
		requestedModel = strings.TrimSpace(model.Slug)
	}
	upstreamModel := strings.TrimSpace(providerResult.UpstreamModel)
	if upstreamModel == "" {
		upstreamModel = strings.TrimSpace(model.UpstreamModel)
	}

	return &OpenAIForwardResult{
		RequestID:       strings.TrimSpace(providerResult.UpstreamRequestID),
		ResponseID:      strings.TrimSpace(providerResult.ResponseID),
		Usage:           codexGatewayProviderUsageToOpenAIUsage(providerResult.Usage),
		Model:           requestedModel,
		UpstreamModel:   upstreamModel,
		ReasoningEffort: codexGatewayReasoningEffort(parsed),
		Stream:          stream,
		OpenAIWSMode:    false,
		Duration:        duration,
	}
}

func codexGatewayReasoningEffort(req CodexGatewayResponsesCreateRequest) *string {
	raw := strings.TrimSpace(gjsonStringBytes(req.Reasoning, "effort"))
	if raw == "" {
		return nil
	}
	normalized := normalizeOpenAIReasoningEffort(raw)
	if normalized == "" {
		return nil
	}
	return &normalized
}

func codexGatewayUsageFields(provider string, upstreamAttemptID string) AugmentUsageFields {
	clientProduct := CodexUsageClientProduct
	requestScope := codexGatewayUsageRequestScope
	featureScope := strings.TrimSpace(provider)
	pricingVersion := codexGatewayUsagePricingVersion
	billable := true
	costSource := codexGatewayUsageCostSource
	currency := codexGatewayUsageCurrency
	settlementStatus := codexGatewayUsageSettlementState

	fields := AugmentUsageFields{
		ClientProduct:      &clientProduct,
		RequestScope:       &requestScope,
		FeatureScope:       &featureScope,
		PricingVersion:     &pricingVersion,
		Billable:           &billable,
		CostSource:         &costSource,
		Currency:           &currency,
		SettlementStatus:   &settlementStatus,
		UpstreamAttemptID:  optionalTrimmedStringPtr(upstreamAttemptID),
		ReasoningUnitPrice: float64Ptr(0),
	}
	return fields
}

func codexGatewayRecordUsageBestEffort(ctx context.Context, recorder codexGatewayUsageRecorder, req CodexGatewayProviderRequest, account *Account, providerResult CodexGatewayProviderResult, stream bool, startedAt time.Time) {
	if recorder == nil || req.Request.APIKey == nil || account == nil {
		return
	}

	apiKey := req.Request.APIKey
	user := apiKey.User
	if user == nil {
		user = &User{ID: apiKey.UserID}
	}

	result := codexGatewayForwardResult(req.Model, req.Parsed, providerResult, stream, time.Since(startedAt))
	fields := codexGatewayUsageFields(req.Model.Provider, providerResult.UpstreamRequestID)
	if err := recorder.RecordUsage(ctx, &OpenAIRecordUsageInput{
		Result:             result,
		APIKey:             apiKey,
		User:               user,
		Account:            account,
		InboundEndpoint:    "/codex/v1/responses",
		UpstreamEndpoint:   codexGatewayUsageUpstreamEndpoint(req.Model.Provider),
		RequestPayloadHash: HashUsageRequestPayload(req.Request.Body),
		ChannelUsageFields: ChannelUsageFields{
			OriginalModel:      strings.TrimSpace(req.Parsed.Model),
			ChannelMappedModel: strings.TrimSpace(providerResult.UpstreamModel),
			BillingModelSource: BillingModelSourceUpstream,
		},
		AugmentUsageFields: fields,
	}); err != nil {
		// Best effort only.
	}
}

func codexGatewayUsageUpstreamEndpoint(provider string) string {
	switch strings.TrimSpace(provider) {
	case "deepseek":
		return "/v1/chat/completions"
	default:
		return "/v1/responses"
	}
}
