package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
)

type betaPolicySettingRepoStub struct {
	values map[string]string
}

func newBedrockCCCompatServiceForTest(groupID int64) *GatewayService {
	channel := &Channel{
		ID:     1,
		Status: StatusActive,
		FeaturesConfig: map[string]any{
			featureKeyBedrockCCCompat: true,
		},
	}
	channelService := &ChannelService{}
	channelService.cache.Store(&channelCache{
		channelByGroupID: map[int64]*Channel{groupID: channel},
		byID:             map[int64]*Channel{channel.ID: channel},
		groupPlatform:    map[int64]string{groupID: PlatformAnthropic},
		loadedAt:         time.Now(),
	})
	return &GatewayService{channelService: channelService}
}

func TestApplyBedrockCCCompatFiltersBetaHeaderForPassthroughPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(42)
	service := newBedrockCCCompatServiceForTest(groupID)
	body := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)

	for _, testCase := range []struct {
		name    string
		account *Account
	}{
		{
			name:    "API key passthrough",
			account: &Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey},
		},
		{
			name:    "OAuth passthrough",
			account: &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth},
		},
		{
			name: "formal pool CC Gateway passthrough",
			account: &Account{
				Platform: PlatformAnthropic,
				Type:     AccountTypeOAuth,
				Extra:    map[string]any{"cc_gateway_enabled": "true"},
			},
		},
		{
			name: "CCH passthrough",
			account: &Account{
				Platform: PlatformAnthropic,
				Type:     AccountTypeOAuth,
				Extra:    map[string]any{"enable_cch_signing": true},
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(response)
			context.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			context.Request.Header.Set("anthropic-beta", "context-1m-2025-08-07,files-api-2025-04-14")

			service.ApplyBedrockCCCompat(context, body, "us.anthropic.claude-opus-4-6-v1", testCase.account, &groupID)

			if got := getHeaderRaw(context.Request.Header, "anthropic-beta"); got != "context-1m-2025-08-07" {
				t.Fatalf("anthropic-beta = %q, want only Bedrock-supported token", got)
			}
		})
	}
}

func TestApplyBedrockCCCompatPreservesTrueBedrockHeaderForBetaPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(42)
	service := newBedrockCCCompatServiceForTest(groupID)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	context.Request.Header.Set("anthropic-beta", "advanced-tool-use-2025-11-20")

	service.ApplyBedrockCCCompat(
		context,
		[]byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		"us.anthropic.claude-opus-4-6-v1",
		&Account{Platform: PlatformAnthropic, Type: AccountTypeBedrock},
		&groupID,
	)

	if got := getHeaderRaw(context.Request.Header, "anthropic-beta"); got != "advanced-tool-use-2025-11-20" {
		t.Fatalf("anthropic-beta = %q, want unmodified header for true Bedrock policy evaluation", got)
	}
}

func TestApplyBedrockCCCompatRestoresCanonicalHeaderAfterCCGatewayAttempt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(42)
	svc := newBedrockCCCompatServiceForTest(groupID)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	context.Request.Header.Set("anthropic-beta", "context-1m-2025-08-07,files-api-2025-04-14")
	body := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)

	// The CC Gateway OAuth attempt fails before a true Bedrock account is selected.
	svc.ApplyBedrockCCCompat(context, body, "us.anthropic.claude-opus-4-6-v1", &Account{
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{"cc_gateway_enabled": "true"},
	}, &groupID)
	if got := getHeaderRaw(context.Request.Header, "anthropic-beta"); got != "context-1m-2025-08-07" {
		t.Fatalf("CC Gateway anthropic-beta = %q, want filtered value", got)
	}

	// A later Bedrock attempt must see the original inbound header for policy evaluation.
	svc.ApplyBedrockCCCompat(context, body, "us.anthropic.claude-opus-4-6-v1", &Account{
		Platform: PlatformAnthropic,
		Type:     AccountTypeBedrock,
	}, &groupID)
	if got := getHeaderRaw(context.Request.Header, "anthropic-beta"); got != "context-1m-2025-08-07,files-api-2025-04-14" {
		t.Fatalf("Bedrock anthropic-beta = %q, want canonical inbound header", got)
	}
}

func (s *betaPolicySettingRepoStub) Get(ctx context.Context, key string) (*Setting, error) {
	panic("unexpected Get call")
}

func (s *betaPolicySettingRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	if v, ok := s.values[key]; ok {
		return v, nil
	}
	return "", ErrSettingNotFound
}

func (s *betaPolicySettingRepoStub) Set(ctx context.Context, key, value string) error {
	panic("unexpected Set call")
}

func (s *betaPolicySettingRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	panic("unexpected GetMultiple call")
}

func (s *betaPolicySettingRepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	panic("unexpected SetMultiple call")
}

func (s *betaPolicySettingRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}

func (s *betaPolicySettingRepoStub) Delete(ctx context.Context, key string) error {
	panic("unexpected Delete call")
}

func TestResolveBedrockBetaTokensForRequest_BlocksOnOriginalAnthropicToken(t *testing.T) {
	settings := &BetaPolicySettings{
		Rules: []BetaPolicyRule{
			{
				BetaToken:    "advanced-tool-use-2025-11-20",
				Action:       BetaPolicyActionBlock,
				Scope:        BetaPolicyScopeAll,
				ErrorMessage: "advanced tool use is blocked",
			},
		},
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}

	svc := &GatewayService{
		settingService: NewSettingService(
			&betaPolicySettingRepoStub{values: map[string]string{
				SettingKeyBetaPolicySettings: string(raw),
			}},
			&config.Config{},
		),
	}
	account := &Account{Platform: PlatformAnthropic, Type: AccountTypeBedrock}

	_, err = svc.resolveBedrockBetaTokensForRequest(
		context.Background(),
		account,
		"advanced-tool-use-2025-11-20",
		[]byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		"us.anthropic.claude-opus-4-6-v1",
	)
	if err == nil {
		t.Fatal("expected raw advanced-tool-use token to be blocked before Bedrock transform")
	}
	if err.Error() != "advanced tool use is blocked" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveBedrockBetaTokensForRequest_FiltersAfterBedrockTransform(t *testing.T) {
	settings := &BetaPolicySettings{
		Rules: []BetaPolicyRule{
			{
				BetaToken: "tool-search-tool-2025-10-19",
				Action:    BetaPolicyActionFilter,
				Scope:     BetaPolicyScopeAll,
			},
		},
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}

	svc := &GatewayService{
		settingService: NewSettingService(
			&betaPolicySettingRepoStub{values: map[string]string{
				SettingKeyBetaPolicySettings: string(raw),
			}},
			&config.Config{},
		),
	}
	account := &Account{Platform: PlatformAnthropic, Type: AccountTypeBedrock}

	betaTokens, err := svc.resolveBedrockBetaTokensForRequest(
		context.Background(),
		account,
		"advanced-tool-use-2025-11-20",
		[]byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		"us.anthropic.claude-opus-4-6-v1",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, token := range betaTokens {
		if token == "tool-search-tool-2025-10-19" {
			t.Fatalf("expected transformed Bedrock token to be filtered")
		}
	}
}

// TestResolveBedrockBetaTokensForRequest_BlocksBodyAutoInjectedComputerUse 验证：
// 管理员 block 了 computer-use，客户端不在 header 中带该 token，
// 但请求体包含 computer_use 工具 → 自动注入后应被 block。
func TestResolveBedrockBetaTokensForRequest_BlocksBodyAutoInjectedComputerUse(t *testing.T) {
	settings := &BetaPolicySettings{
		Rules: []BetaPolicyRule{
			{
				BetaToken:    "computer-use-2025-11-24",
				Action:       BetaPolicyActionBlock,
				Scope:        BetaPolicyScopeAll,
				ErrorMessage: "computer use is blocked",
			},
		},
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}

	svc := &GatewayService{
		settingService: NewSettingService(
			&betaPolicySettingRepoStub{values: map[string]string{
				SettingKeyBetaPolicySettings: string(raw),
			}},
			&config.Config{},
		),
	}
	account := &Account{Platform: PlatformAnthropic, Type: AccountTypeBedrock}

	// header 中不带 beta token，但 body 中有 computer_use 工具
	_, err = svc.resolveBedrockBetaTokensForRequest(
		context.Background(),
		account,
		"", // 空 header
		[]byte(`{"tools":[{"type":"computer_20250124","name":"computer"}],"messages":[{"role":"user","content":"hi"}]}`),
		"us.anthropic.claude-opus-4-6-v1",
	)
	if err == nil {
		t.Fatal("expected body-injected computer-use to be blocked")
	}
	if err.Error() != "computer use is blocked" {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestResolveBedrockBetaTokensForRequest_BlocksBodyAutoInjectedToolSearch 验证：
// 管理员 block 了 tool-search-tool，客户端不在 header 中带 beta token，
// 但请求体包含 tool search 工具 → 自动注入后应被 block。
func TestResolveBedrockBetaTokensForRequest_BlocksBodyAutoInjectedToolSearch(t *testing.T) {
	settings := &BetaPolicySettings{
		Rules: []BetaPolicyRule{
			{
				BetaToken:    "tool-search-tool-2025-10-19",
				Action:       BetaPolicyActionBlock,
				Scope:        BetaPolicyScopeAll,
				ErrorMessage: "tool search is blocked",
			},
		},
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}

	svc := &GatewayService{
		settingService: NewSettingService(
			&betaPolicySettingRepoStub{values: map[string]string{
				SettingKeyBetaPolicySettings: string(raw),
			}},
			&config.Config{},
		),
	}
	account := &Account{Platform: PlatformAnthropic, Type: AccountTypeBedrock}

	// header 中不带 beta token，但 body 中有 tool_search_tool 工具
	_, err = svc.resolveBedrockBetaTokensForRequest(
		context.Background(),
		account,
		"",
		[]byte(`{"tools":[{"type":"tool_search_tool_regex_20251119","name":"search"}],"messages":[{"role":"user","content":"hi"}]}`),
		"us.anthropic.claude-sonnet-4-6",
	)
	if err == nil {
		t.Fatal("expected body-injected tool-search-tool to be blocked")
	}
	if err.Error() != "tool search is blocked" {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestResolveBedrockBetaTokensForRequest_PassesWhenNoBlockRuleMatches 验证：
// body 自动注入的 token 如果没有对应的 block 规则，应正常通过。
func TestResolveBedrockBetaTokensForRequest_PassesWhenNoBlockRuleMatches(t *testing.T) {
	settings := &BetaPolicySettings{
		Rules: []BetaPolicyRule{
			{
				BetaToken:    "context-1m-2025-08-07",
				Action:       BetaPolicyActionBlock,
				Scope:        BetaPolicyScopeAll,
				ErrorMessage: "context is blocked",
			},
		},
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}

	svc := &GatewayService{
		settingService: NewSettingService(
			&betaPolicySettingRepoStub{values: map[string]string{
				SettingKeyBetaPolicySettings: string(raw),
			}},
			&config.Config{},
		),
	}
	account := &Account{Platform: PlatformAnthropic, Type: AccountTypeBedrock}

	// body 中有 computer_use 工具（会注入 computer-use token），但 block 规则只针对 context-1m
	tokens, err := svc.resolveBedrockBetaTokensForRequest(
		context.Background(),
		account,
		"",
		[]byte(`{"tools":[{"type":"computer_20250124","name":"computer"}],"messages":[{"role":"user","content":"hi"}]}`),
		"us.anthropic.claude-opus-4-6-v1",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, token := range tokens {
		if token == "computer-use-2025-11-24" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected computer-use token to be present")
	}
}
