package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func validCodexGatewayAPIKeyForTest() *APIKey {
	groupID := int64(44)
	product := CodexUsageClientProduct
	return &APIKey{
		ID:                      7,
		UserID:                  88,
		Key:                     "sk-codex",
		Status:                  StatusActive,
		GroupID:                 &groupID,
		RestrictedClientProduct: &product,
		Group: &Group{
			ID:                   groupID,
			Platform:             PlatformOpenAI,
			Status:               StatusActive,
			Hydrated:             true,
			CodexGatewayEntitled: true,
		},
	}
}

func TestCodexGatewayUsage_ProviderUsagePreservesCacheReadTokens(t *testing.T) {
	usage := codexGatewayProviderUsageToOpenAIUsage(CodexGatewayProviderUsage{
		InputTokens:          18,
		OutputTokens:         6,
		CacheReadInputTokens: 5,
	})

	require.Equal(t, 18, usage.InputTokens)
	require.Equal(t, 6, usage.OutputTokens)
	require.Equal(t, 5, usage.CacheReadInputTokens)
	require.Equal(t, 0, usage.CacheCreationInputTokens)
}

func TestCodexGatewayUsage_RecordUsageBestEffortPopulatesGatewayMetadata(t *testing.T) {
	recorder := &codexGatewayUsageRecorderStub{}
	apiKey := validCodexGatewayAPIKeyForTest()

	codexGatewayRecordUsageBestEffort(context.Background(), recorder, CodexGatewayProviderRequest{
		Request: CodexGatewayResponsesRequest{
			APIKey: apiKey,
			Body:   []byte(`{"model":"gpt-5.5","input":"hello"}`),
		},
		Model: CodexGatewayModel{
			Slug:          "gpt-5.5",
			Provider:      "openai",
			UpstreamModel: "gpt-5.5",
		},
		Parsed: CodexGatewayResponsesCreateRequest{
			Model:     "gpt-5.5",
			Reasoning: []byte(`{"effort":"high"}`),
		},
	}, &Account{ID: 12, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, CodexGatewayProviderResult{
		ResponseID:        "resp_1",
		UpstreamRequestID: "req_1",
		UpstreamModel:     "gpt-5.5-2026-01-01",
		Usage: CodexGatewayProviderUsage{
			InputTokens:          20,
			OutputTokens:         6,
			CacheReadInputTokens: 4,
		},
	}, false, time.Now().Add(-3*time.Second))

	require.Len(t, recorder.inputs, 1)
	input := recorder.inputs[0]
	require.Equal(t, "/codex/v1/responses", input.InboundEndpoint)
	require.Equal(t, "/v1/responses", input.UpstreamEndpoint)
	require.Equal(t, "gpt-5.5", input.OriginalModel)
	require.Equal(t, "gpt-5.5-2026-01-01", input.ChannelMappedModel)
	require.Equal(t, BillingModelSourceUpstream, input.BillingModelSource)
	require.NotNil(t, input.Result)
	require.Equal(t, "req_1", input.Result.RequestID)
	require.Equal(t, "resp_1", input.Result.ResponseID)
	require.Equal(t, "gpt-5.5", input.Result.Model)
	require.Equal(t, "gpt-5.5-2026-01-01", input.Result.UpstreamModel)
	require.Equal(t, 20, input.Result.Usage.InputTokens)
	require.Equal(t, 6, input.Result.Usage.OutputTokens)
	require.Equal(t, 4, input.Result.Usage.CacheReadInputTokens)
	require.NotNil(t, input.Result.ReasoningEffort)
	require.Equal(t, "high", *input.Result.ReasoningEffort)
	require.NotNil(t, input.ClientProduct)
	require.Equal(t, CodexUsageClientProduct, *input.ClientProduct)
	require.NotNil(t, input.RequestScope)
	require.Equal(t, "gateway", *input.RequestScope)
	require.NotNil(t, input.FeatureScope)
	require.Equal(t, "openai", *input.FeatureScope)
	require.NotNil(t, input.PricingVersion)
	require.Equal(t, "codex_gateway_v1", *input.PricingVersion)
	require.NotNil(t, input.Billable)
	require.True(t, *input.Billable)
	require.NotNil(t, input.Currency)
	require.Equal(t, "USD", *input.Currency)
	require.NotEmpty(t, input.RequestPayloadHash)
	require.NotNil(t, input.User)
	require.Equal(t, apiKey.UserID, input.User.ID)
}

func TestCodexGatewayUsage_DeepSeekUsesChatCompletionsBillingEndpoint(t *testing.T) {
	require.Equal(t, "/v1/chat/completions", codexGatewayUsageUpstreamEndpoint("deepseek"))
	require.Equal(t, "/v1/responses", codexGatewayUsageUpstreamEndpoint("openai"))
}
