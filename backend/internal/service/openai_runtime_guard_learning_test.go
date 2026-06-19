package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestClassifyOpenAIRuntimeGuardUpstreamErrorBuckets(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		message   string
		bucket    string
		category  string
		retryable bool
		terminal  bool
	}{
		{
			name:     "illegal reasoning effort max",
			status:   http.StatusBadRequest,
			body:     `{"error":{"message":"Unsupported value: 'max' for reasoning_effort","type":"invalid_request_error","param":"reasoning_effort","code":"invalid_value"}}`,
			bucket:   "illegal_reasoning_effort",
			category: "reasoning.illegal_effort",
		},
		{
			name:     "unsupported oauth model channel",
			status:   http.StatusForbidden,
			message:  "ChatGPT OAuth account does not support model gpt-5.4 on this unsupported model/profile/channel",
			bucket:   "unsupported_oauth_model_channel",
			category: "capability.unsupported_oauth_model_profile_channel",
		},
		{
			name:     "image generation disabled",
			status:   http.StatusForbidden,
			message:  "Image generation is not enabled for this group",
			bucket:   "image_generation_disabled",
			category: "capability.image_generation_disabled",
		},
		{
			name:     "context overflow",
			status:   http.StatusBadRequest,
			message:  "This model's context length was exceeded: context window exceeded and max tokens/context too large",
			bucket:   "context_overflow",
			category: "context.upstream_overflow",
		},
		{
			name:     "assistant input_text invalid value",
			status:   http.StatusBadRequest,
			body:     `{"error":{"message":"Invalid value: 'input_text'. Supported values are: 'output_text' and 'refusal'.","type":"invalid_request_error","param":"input.0.content.0.type","code":"invalid_value"}}`,
			bucket:   "shape_transcript_error",
			category: "shape.transcript_error",
		},
		{
			name:     "token invalidated revoked 401",
			status:   http.StatusUnauthorized,
			message:  "401 token invalidated or revoked; needs relogin",
			bucket:   "token_invalidated",
			category: "auth.token_invalidated",
			terminal: true,
		},
		{
			name:     "invalid encrypted content",
			status:   http.StatusBadRequest,
			body:     `{"error":{"message":"The encrypted content could not be verified.","type":"invalid_request_error","param":null,"code":"invalid_encrypted_content"}}`,
			bucket:   "invalid_encrypted_content",
			category: "content.invalid_encrypted_content",
			terminal: true,
		},
		{
			name:      "502 html cloudflare transient",
			status:    http.StatusBadGateway,
			body:      `<html><title>502 Bad Gateway</title><body>cloudflare temporary network error</body></html>`,
			bucket:    "temporary_network",
			category:  "temporary.network",
			retryable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyOpenAIRuntimeGuardUpstreamError(tt.status, http.Header{}, []byte(tt.body), tt.message)
			require.Equal(t, tt.bucket, got.Bucket)
			require.Equal(t, tt.category, got.Category)
			require.NotEmpty(t, got.Metric)
			require.Equal(t, tt.retryable, got.Retryable)
			require.Equal(t, tt.terminal, got.Terminal)
			require.Equal(t, tt.status, got.Status)
			require.NotContains(t, strings.ToLower(got.Message), "sk-proj-secret")
		})
	}
}

func TestOpenAIRuntimeGuardLearnedBlockScopeAndTTL(t *testing.T) {
	svc := &OpenAIGatewayService{}
	classification := OpenAIRuntimeGuardUpstreamErrorClassification{
		Bucket:   "unsupported_oauth_model_channel",
		Category: "capability.unsupported_oauth_model_profile_channel",
		Metric:   "openai_runtime_guard.upstream.unsupported_oauth_model_channel",
		Action:   "learn_block",
		TTL:      40 * time.Millisecond,
	}
	scope := OpenAIRuntimeGuardLearnedBlockScope{
		AccountType: AccountTypeOAuth,
		Model:       "gpt-5.4",
		Endpoint:    "responses",
		Profile:     "profile-a",
	}

	require.True(t, svc.RecordOpenAIRuntimeGuardLearnedBlock(scope, classification))
	got, ok := svc.IsOpenAIRuntimeGuardLearnedBlocked(scope)
	require.True(t, ok)
	require.Equal(t, classification.Bucket, got.Bucket)

	for _, other := range []OpenAIRuntimeGuardLearnedBlockScope{
		{AccountType: AccountTypeAPIKey, Model: "gpt-5.4", Endpoint: "responses", Profile: "profile-a"},
		{AccountType: AccountTypeOAuth, Model: "gpt-5.3-codex", Endpoint: "responses", Profile: "profile-a"},
		{AccountType: AccountTypeOAuth, Model: "gpt-5.4", Endpoint: "chat_completions", Profile: "profile-a"},
		{AccountType: AccountTypeOAuth, Model: "gpt-5.4", Endpoint: "responses", Profile: "profile-b"},
	} {
		_, blocked := svc.IsOpenAIRuntimeGuardLearnedBlocked(other)
		require.False(t, blocked, "%#v must not share learned block", other)
	}

	time.Sleep(60 * time.Millisecond)
	_, ok = svc.IsOpenAIRuntimeGuardLearnedBlocked(scope)
	require.False(t, ok, "expired learned block must be evicted")
}

func TestOpsUpstreamErrorRuntimeGuardMetadataIsStructuredAndSanitized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform:             PlatformOpenAI,
		UpstreamStatusCode:   http.StatusBadRequest,
		Kind:                 "http_error",
		Message:              "invalid_encrypted_content with access_token=secret raw prompt marker",
		Detail:               `{"error":{"code":"invalid_encrypted_content","message":"bad"},"encrypted_content":"gAAAA-secret-payload","prompt":"prompt-secret"}`,
		UpstreamResponseBody: `{"encrypted_content":"gAAAA-secret-payload","access_token":"secret-token"}`,
	})

	rawEvents, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events := rawEvents.([]*OpsUpstreamErrorEvent)
	require.Len(t, events, 1)
	require.Equal(t, "invalid_encrypted_content", events[0].RuntimeGuardBucket)
	require.Equal(t, "content.invalid_encrypted_content", events[0].RuntimeGuardCategory)
	require.NotEmpty(t, events[0].RuntimeGuardMetric)
	require.NotEmpty(t, events[0].RuntimeGuardAction)

	raw, err := json.Marshal(events)
	require.NoError(t, err)
	serialized := string(raw)
	require.NotContains(t, serialized, "gAAAA-secret-payload")
	require.NotContains(t, serialized, "prompt-secret")
	require.NotContains(t, serialized, "secret-token")
	require.NotContains(t, strings.ToLower(serialized), "raw prompt marker")
}

func TestOpenAIGatewayService_Forward_RuntimeGuardLearnedBlockStopsRepeatedUpstreamCall(t *testing.T) {
	upstream, rec, c, svc, account := newOpenAIRuntimeGuardForwardHarness(t)
	account.Extra["openai_gateway_profile_id"] = "profile-a"
	classification := ClassifyOpenAIRuntimeGuardUpstreamError(
		http.StatusForbidden,
		http.Header{},
		[]byte(`{"error":{"message":"unsupported model/profile/channel for ChatGPT OAuth account","type":"invalid_request_error"}}`),
		"",
	)
	recorded := svc.RecordOpenAIRuntimeGuardLearnedBlock(openAIRuntimeGuardLearnedBlockScopeForAccount(account, "gpt-5.4", "responses"), classification)
	require.True(t, recorded)

	result, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"gpt-5.4","stream":false,"input":"hi"}`))

	require.Error(t, err)
	require.Nil(t, result)
	require.Len(t, upstream.bodies, 0)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.JSONEq(t, `{"error":{"type":"invalid_request_error","code":"local_policy_block","category":"capability.local_policy_block","message":"OpenAI OAuth request is temporarily blocked by runtime guard learning","param":"model"}}`, rec.Body.String())
}

func TestHandleOpenAIAccountUpstreamErrorRecordsCapabilityLearnedBlockScopedToModelEndpointProfile(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 7001, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"openai_gateway_profile_id": "profile-a"}}
	body := []byte(`{"error":{"message":"unsupported model/profile/channel for ChatGPT OAuth account","type":"invalid_request_error"}}`)

	shouldDisable := svc.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusForbidden, http.Header{}, body, "gpt-5.4")

	require.False(t, shouldDisable)
	_, blocked := svc.IsOpenAIRuntimeGuardLearnedBlocked(openAIRuntimeGuardLearnedBlockScopeForAccount(account, "gpt-5.4", "responses"))
	require.True(t, blocked)
	_, otherModelBlocked := svc.IsOpenAIRuntimeGuardLearnedBlocked(openAIRuntimeGuardLearnedBlockScopeForAccount(account, "gpt-5.3-codex", "responses"))
	require.False(t, otherModelBlocked)
}

func TestHandleOpenAIAccountUpstreamErrorDoesNotLearnBlockRequestShapeSpecificErrors(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 7004, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"openai_gateway_profile_id": "profile-a"}}
	body := []byte(`{"error":{"message":"Invalid value: 'input_text'. Supported values are: 'output_text' and 'refusal'.","type":"invalid_request_error","param":"input.0.content.0.type","code":"invalid_value"}}`)

	shouldDisable := svc.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusBadRequest, http.Header{}, body, "gpt-5.4", "responses")

	require.False(t, shouldDisable)
	_, blocked := svc.IsOpenAIRuntimeGuardLearnedBlocked(openAIRuntimeGuardLearnedBlockScopeForAccount(account, "gpt-5.4", "responses"))
	require.False(t, blocked, "request-shape errors must not poison the whole model+endpoint with a learned block")
}

func TestHandleOpenAIAccountUpstreamErrorLearnedBlockHonorsEndpointHint(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 7002, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"openai_gateway_profile_id": "profile-a"}}
	body := []byte(`{"error":{"message":"unsupported model/profile/channel for ChatGPT OAuth account","type":"invalid_request_error"}}`)

	_ = svc.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusForbidden, http.Header{}, body, "gpt-5.4", "chat_completions")

	_, chatBlocked := svc.IsOpenAIRuntimeGuardLearnedBlocked(openAIRuntimeGuardLearnedBlockScopeForAccount(account, "gpt-5.4", "chat_completions"))
	require.True(t, chatBlocked)
	_, responsesBlocked := svc.IsOpenAIRuntimeGuardLearnedBlocked(openAIRuntimeGuardLearnedBlockScopeForAccount(account, "gpt-5.4", "responses"))
	require.False(t, responsesBlocked)
}

func TestOpenAIRuntimeGuardLearnedBlockHelperHonorsEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 7003, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"openai_gateway_profile_id": "profile-a"}}
	classification := OpenAIRuntimeGuardUpstreamErrorClassification{Bucket: "unsupported_oauth_model_channel", Category: "capability.unsupported_oauth_model_profile_channel", Metric: "openai_runtime_guard.upstream.unsupported_oauth_model_channel", Action: "learn_block", TTL: time.Minute}
	require.True(t, svc.RecordOpenAIRuntimeGuardLearnedBlock(openAIRuntimeGuardLearnedBlockScopeForAccount(account, "gpt-5.4", "chat_completions"), classification))

	responsesRecorder := httptest.NewRecorder()
	responsesCtx, _ := gin.CreateTestContext(responsesRecorder)
	require.Nil(t, svc.blockOpenAIRuntimeGuardLearnedRequest(responsesCtx, account, "gpt-5.4", "responses"))
	require.Equal(t, http.StatusOK, responsesRecorder.Code)

	chatRecorder := httptest.NewRecorder()
	chatCtx, _ := gin.CreateTestContext(chatRecorder)
	blocked := svc.blockOpenAIRuntimeGuardLearnedRequest(chatCtx, account, "gpt-5.4", "chat_completions")
	require.NotNil(t, blocked)
	require.Equal(t, http.StatusBadRequest, chatRecorder.Code)
}
