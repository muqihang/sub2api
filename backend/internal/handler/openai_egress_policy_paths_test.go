package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type openAIEgressPolicyHandlerAccountRepo struct {
	service.AccountRepository
	accounts []service.Account
}

func (r *openAIEgressPolicyHandlerAccountRepo) GetByID(ctx context.Context, id int64) (*service.Account, error) {
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			return &r.accounts[i], nil
		}
	}
	return nil, service.ErrAccountNotFound
}

func (r *openAIEgressPolicyHandlerAccountRepo) ListSchedulableByPlatform(ctx context.Context, platform string) ([]service.Account, error) {
	out := make([]service.Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account.Platform == platform {
			out = append(out, account)
		}
	}
	return out, nil
}

func (r *openAIEgressPolicyHandlerAccountRepo) ListSchedulableByGroupIDAndPlatform(ctx context.Context, groupID int64, platform string) ([]service.Account, error) {
	return r.ListSchedulableByPlatform(ctx, platform)
}

func (r *openAIEgressPolicyHandlerAccountRepo) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]service.Account, error) {
	return r.ListSchedulableByPlatform(ctx, platform)
}

type openAIEgressPolicyHandlerUpstream struct {
	calls int
}

func (u *openAIEgressPolicyHandlerUpstream) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	u.calls++
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header)}, nil
}

func (u *openAIEgressPolicyHandlerUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	u.calls++
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header)}, nil
}

type openAIHandlerEntityRepoStub struct {
	service.EntityRegistryRepository

	resolved *service.ResolvedEntity
}

func (r *openAIHandlerEntityRepoStub) ResolveEntity(ctx context.Context, input service.EntityResolutionInput) (*service.ResolvedEntity, error) {
	return r.resolved, nil
}

type entityRateLimitPolicyRepoForHandlerTest struct {
	policy *service.EntityRateLimitPolicy
}

func (r *entityRateLimitPolicyRepoForHandlerTest) GetActiveByEntityID(ctx context.Context, entityID int64) (*service.EntityRateLimitPolicy, error) {
	return r.policy, nil
}

type entityRateLimitCacheForHandlerTest struct {
	rpmCalls int
	rpmCount int
}

func (c *entityRateLimitCacheForHandlerTest) AcquireEntitySlot(ctx context.Context, entityID int64, maxConcurrency int, requestID string) (bool, error) {
	return true, nil
}

func (c *entityRateLimitCacheForHandlerTest) ReleaseEntitySlot(ctx context.Context, entityID int64, requestID string) error {
	return nil
}

func (c *entityRateLimitCacheForHandlerTest) IncrementEntityRPM(ctx context.Context, entityID int64) (int, error) {
	c.rpmCalls++
	return c.rpmCount, nil
}

func (c *entityRateLimitCacheForHandlerTest) AddEntityTPM(ctx context.Context, entityID int64, tokens int) (int, error) {
	return 0, nil
}

func (c *entityRateLimitCacheForHandlerTest) AddEntityCost(ctx context.Context, entityID int64, amount float64) (float64, error) {
	return 0, nil
}

type openAIEgressPolicyHarnessConfigHook func(*config.Config)

func newOpenAIEgressPolicyHandlerHarness(t *testing.T, account service.Account, optionalDeps ...any) (*OpenAIGatewayHandler, *openAIEgressPolicyHandlerUpstream) {
	t.Helper()

	cfg := &config.Config{}
	cfg.RunMode = config.RunModeSimple
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.EgressFailClosed = true
	cfg.Gateway.OpenAICore.AllowAccountProxyFallback = false
	cfg.Gateway.OpenAICore.AllowDirectFallback = false
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{Name: "default", Enabled: true},
	}
	cfg.Default.RateMultiplier = 1
	serviceOptionalDeps := make([]any, 0, len(optionalDeps)+1)
	for _, dep := range optionalDeps {
		if hook, ok := dep.(openAIEgressPolicyHarnessConfigHook); ok {
			hook(cfg)
			continue
		}
		serviceOptionalDeps = append(serviceOptionalDeps, dep)
	}

	repo := &openAIEgressPolicyHandlerAccountRepo{accounts: []service.Account{account}}
	core := service.NewOpenAIGatewayCoreService(repo, cfg, nil)
	upstream := &openAIEgressPolicyHandlerUpstream{}
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg)
	t.Cleanup(billingCache.Stop)
	serviceOptionalDeps = append(serviceOptionalDeps, core)

	gatewaySvc := service.NewOpenAIGatewayService(
		repo,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		cfg,
		nil,
		nil,
		service.NewBillingService(cfg, nil),
		nil,
		billingCache,
		upstream,
		&service.DeferredService{},
		nil,
		serviceOptionalDeps...,
	)
	concurrencySvc := service.NewConcurrencyService(&concurrencyCacheMock{
		acquireUserSlotFn: func(ctx context.Context, userID int64, maxConcurrency int, requestID string) (bool, error) {
			return true, nil
		},
		acquireAccountSlotFn: func(ctx context.Context, accountID int64, maxConcurrency int, requestID string) (bool, error) {
			return true, nil
		},
	})
	return NewOpenAIGatewayHandler(
		gatewaySvc,
		core,
		concurrencySvc,
		billingCache,
		&service.APIKeyService{},
		nil,
		nil,
		cfg,
	), upstream
}

func newOpenAIEgressPolicyHandlerContext(method, target string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(method, target, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	groupID := int64(991)
	apiKey := &service.APIKey{
		ID:      992,
		GroupID: &groupID,
		Group:   &service.Group{ID: groupID, AllowImageGeneration: true},
		User:    &service.User{ID: 993, Status: service.StatusActive},
	}
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.User.ID, Concurrency: 1})
	return c, rec
}

func requireOpenAIEgressPolicyHandlerResponse(t *testing.T, rec *httptest.ResponseRecorder, upstream *openAIEgressPolicyHandlerUpstream) {
	t.Helper()
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Equal(t, "api_error", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
	require.Equal(t, service.OpenAIEgressPolicyClientMessage, gjson.GetBytes(rec.Body.Bytes(), "error.message").String())
	require.NotContains(t, rec.Body.String(), "missing")
	require.NotContains(t, rec.Body.String(), "missing_bucket")
	require.Zero(t, upstream.calls)
}

func TestOpenAIChatCompletionsHandlerMapsEgressPolicyError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	account := service.Account{
		ID:          19301,
		Name:        "openai-apikey-chat",
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeAPIKey,
		Status:      service.StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "https://api.openai.com",
		},
		Extra: map[string]any{
			"openai_gateway_egress_bucket":           "missing",
			openai_compat.ExtraKeyResponsesSupported: false,
		},
	}
	h, upstream := newOpenAIEgressPolicyHandlerHarness(t, account)
	body := []byte(`{"model":"gpt-5.3-codex","messages":[{"role":"user","content":"hello"}],"stream":false}`)
	c, rec := newOpenAIEgressPolicyHandlerContext(http.MethodPost, "/v1/chat/completions", body)

	h.ChatCompletions(c)

	requireOpenAIEgressPolicyHandlerResponse(t, rec, upstream)
}

func TestOpenAIChatCompletionsHandlerRejectsEntityQuotaBeforeUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	account := service.Account{
		ID:          19304,
		Name:        "openai-apikey-chat",
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeAPIKey,
		Status:      service.StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "https://api.openai.com",
		},
		Extra: map[string]any{
			openai_compat.ExtraKeyResponsesSupported: false,
		},
	}
	entityRepo := &openAIHandlerEntityRepoStub{resolved: &service.ResolvedEntity{
		Entity: service.Entity{
			ID:         123,
			EntityKey:  "workspace-alpha",
			EntityType: service.EntityTypeWorkspace,
			Status:     service.EntityStatusActive,
		},
		Source: service.EntityResolutionSourceClaimedBinding,
	}}
	quotaRepo := &entityRateLimitPolicyRepoForHandlerTest{policy: &service.EntityRateLimitPolicy{
		ID:       77,
		EntityID: 123,
		Status:   service.EntityRateLimitPolicyStatusActive,
		RPMLimit: 1,
	}}
	quotaCache := &entityRateLimitCacheForHandlerTest{rpmCount: 2}
	h, upstream := newOpenAIEgressPolicyHandlerHarness(
		t,
		account,
		openAIEgressPolicyHarnessConfigHook(func(cfg *config.Config) {
			cfg.Gateway.OpenAICore.EntityOrchestration.Enabled = true
		}),
		entityRepo,
		service.NewEntityRateLimitService(quotaRepo, quotaCache),
	)
	body := []byte(`{"model":"gpt-5.3-codex","messages":[{"role":"user","content":"hello"}],"stream":false}`)
	c, rec := newOpenAIEgressPolicyHandlerContext(http.MethodPost, "/v1/chat/completions", body)
	c.Request.Header.Set(service.EntityHeader, "workspace-alpha")

	h.ChatCompletions(c)

	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Equal(t, "rate_limit_error", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
	require.Contains(t, gjson.GetBytes(rec.Body.Bytes(), "error.message").String(), "requests-per-minute")
	require.Zero(t, upstream.calls)
	require.Equal(t, 1, quotaCache.rpmCalls)
}

func TestOpenAIImagesHandlerMapsEgressPolicyError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	account := service.Account{
		ID:          19302,
		Name:        "openai-apikey-images",
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeAPIKey,
		Status:      service.StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "https://api.openai.com",
		},
		Extra: map[string]any{
			"openai_gateway_egress_bucket": "missing",
		},
	}
	h, upstream := newOpenAIEgressPolicyHandlerHarness(t, account)
	body := []byte(`{"model":"gpt-image-2","prompt":"apple","size":"1024x1024"}`)
	c, rec := newOpenAIEgressPolicyHandlerContext(http.MethodPost, "/v1/images/generations", body)

	h.Images(c)

	requireOpenAIEgressPolicyHandlerResponse(t, rec, upstream)
}
