package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGPT56CacheWriteIntake_NestedUsageTakesPrecedence(t *testing.T) {
	usage, ok := extractOpenAIUsageFromJSONBytes([]byte(`{
		"usage": {
			"input_tokens": 20,
			"output_tokens": 2,
			"cache_creation_input_tokens": 19,
			"input_tokens_details": {"cache_write_tokens": 7}
		}
	}`))
	require.True(t, ok)
	require.Equal(t, 7, usage.CacheCreationInputTokens)
}

func TestGPT56CacheWriteIntake_TypedResponsesUsageUsesCanonicalCacheWrite(t *testing.T) {
	usage := copyOpenAIUsageFromResponsesUsage(&apicompat.ResponsesUsage{
		InputTokens:              20,
		OutputTokens:             2,
		CacheCreationInputTokens: 7,
		InputTokensDetails: &apicompat.ResponsesInputTokensDetails{
			CachedTokens:        3,
			CacheCreationTokens: 11,
			CacheWriteTokens:    7,
		},
	})

	require.Equal(t, 3, usage.CacheReadInputTokens)
	require.Equal(t, 7, usage.CacheCreationInputTokens, "typed usage must preserve its canonical cache write value")
}

func TestGPT56CacheWriteIntake_BufferedChatCompletionsExtractsCacheWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"usage": {
				"prompt_tokens": 20,
				"completion_tokens": 2,
				"prompt_tokens_details": {
					"cached_tokens": 3,
					"cache_creation_tokens": 19,
					"cache_write_tokens": 7
				}
			}
		}`)),
	}
	service := &OpenAIGatewayService{cfg: &config.Config{}}

	result, err := service.bufferRawChatCompletions(
		ginContext,
		response,
		"gpt-5.6-sol",
		"gpt-5.6-sol",
		"gpt-5.6-sol",
		nil,
		nil,
		time.Now(),
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 20, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Equal(t, 3, result.Usage.CacheReadInputTokens)
	require.Equal(t, 7, result.Usage.CacheCreationInputTokens, "官方 cache_write_tokens 应优先于兼容 creation 字段")
}

func TestGPT56CacheWriteIntake_PerRequestTierIncludesCacheWrite(t *testing.T) {
	maxLowTier := 1000
	lowPrice := 0.01
	highPrice := 0.02
	service := NewBillingService(&config.Config{}, nil)
	resolver := NewModelPricingResolver(nil, service)
	resolved := &ResolvedPricing{
		Mode: BillingModePerRequest,
		RequestTiers: []PricingInterval{
			{MinTokens: 0, MaxTokens: &maxLowTier, PerRequestPrice: &lowPrice},
			{MinTokens: maxLowTier, PerRequestPrice: &highPrice},
		},
	}

	cost, err := service.CalculateCostUnified(CostInput{
		Model:          "gpt-5.6-sol",
		Tokens:         UsageTokens{InputTokens: 800, CacheCreationTokens: 300},
		RequestCount:   1,
		RateMultiplier: 1,
		Resolver:       resolver,
		Resolved:       resolved,
	})

	require.NoError(t, err)
	require.InDelta(t, highPrice, cost.TotalCost, 1e-12, "cache-write 应计入按次区间选择的 context")
}

func TestGPT56CacheWriteIntake_UsesOfficialStandardPricing(t *testing.T) {
	pricingService := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"gpt-5.6-sol": {
			InputCostPerToken:               5e-6,
			InputCostPerTokenPriority:       10e-6,
			OutputCostPerToken:              30e-6,
			OutputCostPerTokenPriority:      60e-6,
			CacheReadInputTokenCost:         0.5e-6,
			CacheReadInputTokenCostPriority: 1e-6,
		},
	}}
	billingService := NewBillingService(&config.Config{}, pricingService)

	pricing, err := billingService.GetModelPricing("gpt-5.6-sol")
	require.NoError(t, err)
	require.InDelta(t, 6.25e-6, pricing.CacheCreationPricePerToken, 1e-12)
	require.InDelta(t, 12.5e-6, pricing.CacheCreationPricePerTokenPriority, 1e-12)
	require.Equal(t, 272000, pricing.LongContextInputThreshold)
	require.InDelta(t, 2.0, pricing.LongContextInputMultiplier, 1e-12)
	require.InDelta(t, 1.5, pricing.LongContextOutputMultiplier, 1e-12)

	priorityCost, err := billingService.CalculateCostWithServiceTier(
		"gpt-5.6-sol",
		UsageTokens{CacheCreationTokens: 200},
		1,
		"priority",
	)
	require.NoError(t, err)
	require.InDelta(t, 200*12.5e-6, priorityCost.CacheCreationCost, 1e-12)
}

func TestGPT56CacheWriteIntake_RecordUsageUsesExclusiveTokenBuckets(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	service := newOpenAIRecordUsageServiceForTest(
		usageRepo,
		&openAIRecordUsageUserRepoStub{},
		&openAIRecordUsageSubRepoStub{},
		nil,
	)
	service.billingService = NewBillingService(service.cfg, &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"gpt-5.6-sol": {
			InputCostPerToken:       5e-6,
			OutputCostPerToken:      30e-6,
			CacheReadInputTokenCost: 0.5e-6,
		},
	}})

	err := service.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID: "resp_gpt56_cache_write_intake",
			Usage: OpenAIUsage{
				InputTokens:              1000,
				OutputTokens:             50,
				CacheCreationInputTokens: 200,
				CacheReadInputTokens:     100,
			},
			Model:    "gpt-5.6-sol",
			Duration: time.Second,
		},
		APIKey:  &APIKey{ID: 1056},
		User:    &User{ID: 2056},
		Account: &Account{ID: 3056},
		ChannelUsageFields: ChannelUsageFields{
			OriginalModel:      "gpt-5.6-sol",
			BillingModelSource: "requested",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.Equal(t, 700, usageRepo.lastLog.InputTokens)
	require.Equal(t, 200, usageRepo.lastLog.CacheCreationTokens)
	require.Equal(t, 100, usageRepo.lastLog.CacheReadTokens)
	require.Equal(t, 1050, usageRepo.lastLog.TotalTokens())
	require.InDelta(t, 700*5e-6, usageRepo.lastLog.InputCost, 1e-12)
	require.InDelta(t, 200*6.25e-6, usageRepo.lastLog.CacheCreationCost, 1e-12)
	require.InDelta(t, 100*0.5e-6, usageRepo.lastLog.CacheReadCost, 1e-12)
	require.InDelta(t, 50*30e-6, usageRepo.lastLog.OutputCost, 1e-12)
	require.InDelta(t, usageRepo.lastLog.TotalCost*1.1, usageRepo.lastLog.ActualCost, 1e-12)
}
