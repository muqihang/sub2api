package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type openAIImageHTTPUpstreamStub struct {
	resp           *http.Response
	err            error
	lastRequestURL string
	lastAuthHeader string
	lastBody       []byte
}

func (s *openAIImageHTTPUpstreamStub) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	if req != nil {
		s.lastRequestURL = req.URL.String()
		s.lastAuthHeader = req.Header.Get("authorization")
		if req.Body != nil {
			body, _ := io.ReadAll(req.Body)
			s.lastBody = body
			req.Body = io.NopCloser(bytes.NewReader(body))
		}
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.resp, nil
}

func (s *openAIImageHTTPUpstreamStub) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, "", 0, 0)
}

func ioNopCloserString(s string) io.ReadCloser {
	return io.NopCloser(bytes.NewBufferString(s))
}

func newChannelServiceNoPricing() *ChannelService {
	cs := &ChannelService{}
	cs.cache.Store(&channelCache{loadedAt: time.Now()})
	return cs
}

func TestOpenAIGatewayServiceForwardImageGeneration_APIKey(t *testing.T) {
	upstream := &openAIImageHTTPUpstreamStub{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
				"X-Request-Id": []string{"req_image_1"},
			},
			Body: ioNopCloserString(`{"created":1777027469,"data":[{"b64_json":"abc"}]}`),
		},
	}

	svc := NewOpenAIGatewayService(
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		&config.Config{},
		nil,
		nil,
		NewBillingService(&config.Config{}, nil),
		nil,
		nil,
		upstream,
		&DeferredService{},
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	account := &Account{
		ID:       85,
		Name:     "测试2-2",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "sk-upstream",
			"base_url": "https://main2.gptteam.space",
			"model_mapping": map[string]any{
				"gpt-image-2": "gpt-image-2",
			},
		},
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 10,
	}

	c, _ := gin.CreateTestContext(nil)
	body := []byte(`{"model":"gpt-image-2","prompt":"apple","size":"1024x1024"}`)

	result, responseBody, responseHeaders, err := svc.ForwardImageGeneration(context.Background(), c, account, body, "gpt-image-2", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "gpt-image-2", result.Model)
	require.Equal(t, "gpt-image-2", result.UpstreamModel)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, "1K", result.ImageSize)
	require.Contains(t, string(responseBody), `"b64_json":"abc"`)
	require.Equal(t, "application/json", responseHeaders.Get("Content-Type"))
	require.Equal(t, "req_image_1", result.RequestID)
	require.Equal(t, "https://main2.gptteam.space/v1/images/generations", upstream.lastRequestURL)
	require.Equal(t, "Bearer sk-upstream", upstream.lastAuthHeader)
}

func TestOpenAIGatewayServiceRecordUsage_ImageGenerationBillsByImageCount(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	subRepo := &openAIRecordUsageSubRepoStub{}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, subRepo, nil)

	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID:  "resp_image_1",
			Model:      "gpt-image-2",
			ImageCount: 1,
			ImageSize:  "1K",
		},
		APIKey: &APIKey{
			ID: 1001,
			Group: &Group{
				RateMultiplier: 1,
			},
		},
		User:    &User{ID: 2001},
		Account: &Account{ID: 3001},
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.Equal(t, "gpt-image-2", usageRepo.lastLog.Model)
	require.Equal(t, 1, usageRepo.lastLog.ImageCount)
	require.NotNil(t, usageRepo.lastLog.ImageSize)
	require.Equal(t, "1K", *usageRepo.lastLog.ImageSize)
	require.NotNil(t, usageRepo.lastLog.BillingMode)
	require.Equal(t, string(BillingModeImage), *usageRepo.lastLog.BillingMode)
	require.True(t, usageRepo.lastLog.ActualCost > 0)
	require.InDelta(t, usageRepo.lastLog.ActualCost, userRepo.lastAmount, 1e-12)
}

func TestOpenAIGatewayServiceRecordUsage_ImageGenerationBillsWithResolverFallback(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	subRepo := &openAIRecordUsageSubRepoStub{}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, subRepo, nil)
	svc.resolver = NewModelPricingResolver(newChannelServiceNoPricing(), svc.billingService)

	groupID := int64(2002)
	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID:  "resp_image_2",
			Model:      "gpt-image-2",
			ImageCount: 1,
			ImageSize:  "1K",
		},
		APIKey: &APIKey{
			ID:      1002,
			GroupID: &groupID,
			Group: &Group{
				ID:             groupID,
				RateMultiplier: 1,
			},
		},
		User:    &User{ID: 2002},
		Account: &Account{ID: 3002},
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.NotNil(t, usageRepo.lastLog.BillingMode)
	require.Equal(t, string(BillingModeImage), *usageRepo.lastLog.BillingMode)
	require.True(t, usageRepo.lastLog.ActualCost > 0)
	require.InDelta(t, usageRepo.lastLog.ActualCost, userRepo.lastAmount, 1e-12)
}

func TestOpenAIGatewayServiceForwardImageGeneration_Upstream400WritesOpenAIError(t *testing.T) {
	upstream := &openAIImageHTTPUpstreamStub{
		resp: &http.Response{
			StatusCode: http.StatusBadRequest,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
				"X-Request-Id": []string{"req_image_400"},
			},
			Body: ioNopCloserString(`{"error":{"message":"bad image prompt"}}`),
		},
	}

	svc := NewOpenAIGatewayService(
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		&config.Config{},
		nil,
		nil,
		NewBillingService(&config.Config{}, nil),
		nil,
		nil,
		upstream,
		&DeferredService{},
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	account := &Account{
		ID:       85,
		Name:     "测试2-2",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "sk-upstream",
		},
		Status:      StatusActive,
		Schedulable: true,
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewBufferString(`{"model":"gpt-image-2","prompt":"bad"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	result, responseBody, responseHeaders, err := svc.ForwardImageGeneration(context.Background(), c, account, []byte(`{"model":"gpt-image-2","prompt":"bad"}`), "gpt-image-2", "")
	require.Error(t, err)
	require.Nil(t, result)
	require.Nil(t, responseBody)
	require.NotNil(t, responseHeaders)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), `"type":"invalid_request_error"`)
	require.Contains(t, rec.Body.String(), `bad image prompt`)
}

func TestOpenAIGatewayServiceForwardImageGeneration_Upstream502ReturnsFailoverError(t *testing.T) {
	upstream := &openAIImageHTTPUpstreamStub{
		resp: &http.Response{
			StatusCode: http.StatusBadGateway,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
				"X-Request-Id": []string{"req_image_502"},
			},
			Body: ioNopCloserString(`{"error":{"message":"Upstream service temporarily unavailable"}}`),
		},
	}

	svc := NewOpenAIGatewayService(
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		&config.Config{},
		nil,
		nil,
		NewBillingService(&config.Config{}, nil),
		nil,
		nil,
		upstream,
		&DeferredService{},
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	account := &Account{
		ID:          85,
		Name:        "测试2-2",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-upstream"},
		Status:      StatusActive,
		Schedulable: true,
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewBufferString(`{"model":"gpt-image-2","prompt":"bad"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	result, responseBody, responseHeaders, err := svc.ForwardImageGeneration(context.Background(), c, account, []byte(`{"model":"gpt-image-2","prompt":"bad"}`), "gpt-image-2", "")
	require.Error(t, err)
	require.Nil(t, result)
	require.Nil(t, responseBody)
	require.NotNil(t, responseHeaders)
	require.False(t, rec.Result().StatusCode >= 400, "failover path should not write a direct client response")
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
}
