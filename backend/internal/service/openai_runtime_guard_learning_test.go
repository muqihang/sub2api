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
			terminal: true,
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
			terminal: true,
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

func TestAppendOpsUpstreamErrorRuntimeGuardOverwritesRawSingleDetail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	rawDetail := `{"error":{"code":"invalid_encrypted_content","message":"bad"},"prompt":"PROMPT_SECRET","input":"INPUT_SECRET","messages":[{"content":"MESSAGE_SECRET"}],"encrypted_content":"ENCRYPTED_SECRET","access_token":"ACCESS_SECRET"}`
	setOpsUpstreamError(c, http.StatusBadRequest, "invalid_encrypted_content", rawDetail)

	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{Platform: PlatformOpenAI, UpstreamStatusCode: http.StatusBadRequest, Kind: "http_error", Message: "invalid_encrypted_content", Detail: rawDetail})

	singleDetailRaw, ok := c.Get(OpsUpstreamErrorDetailKey)
	require.True(t, ok)
	singleDetail, _ := singleDetailRaw.(string)
	rawEvents, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	serializedEvents, err := json.Marshal(rawEvents)
	require.NoError(t, err)
	combined := singleDetail + "\n" + string(serializedEvents)
	for _, secret := range []string{"PROMPT_SECRET", "INPUT_SECRET", "MESSAGE_SECRET", "ENCRYPTED_SECRET", "ACCESS_SECRET"} {
		require.NotContains(t, combined, secret)
	}
}

func TestOpsServiceRecordErrorBatch_RuntimeGuardSingleFieldRedactsOpaquePayload(t *testing.T) {
	var captured []*OpsInsertErrorLogInput
	repo := &opsRepoMock{
		InsertErrorLogFn: func(ctx context.Context, input *OpsInsertErrorLogInput) (int64, error) {
			captured = append(captured, input)
			return int64(len(captured)), nil
		},
	}
	svc := NewOpsService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	rawDetail := `{"error":{"code":"invalid_encrypted_content","message":"The encrypted content could not be verified."},"prompt":"PROMPT_SECRET","input":"INPUT_SECRET","messages":[{"content":"MESSAGE_SECRET"}],"encrypted_content":"ENCRYPTED_SECRET","access_token":"ACCESS_SECRET","refresh_token":"REFRESH_SECRET","api_key":"sk-proj-secretsecretsecret"}`
	entry := &OpsInsertErrorLogInput{
		Platform:             PlatformOpenAI,
		UpstreamStatusCode:   intPtr(http.StatusBadRequest),
		UpstreamErrorMessage: strPtr("invalid_encrypted_content"),
		UpstreamErrorDetail:  strPtr(rawDetail),
		UpstreamErrors: []*OpsUpstreamErrorEvent{{
			Platform:           PlatformOpenAI,
			UpstreamStatusCode: http.StatusBadRequest,
			Kind:               "http_error",
			Message:            "invalid_encrypted_content",
			Detail:             rawDetail,
		}},
	}

	require.NoError(t, svc.RecordErrorBatch(context.Background(), []*OpsInsertErrorLogInput{entry}))
	require.Len(t, captured, 1)
	require.NotNil(t, captured[0].UpstreamErrorDetail)
	require.NotNil(t, captured[0].UpstreamErrorsJSON)
	combined := *captured[0].UpstreamErrorDetail + "\n" + *captured[0].UpstreamErrorsJSON
	for _, secret := range []string{"PROMPT_SECRET", "INPUT_SECRET", "MESSAGE_SECRET", "ENCRYPTED_SECRET", "ACCESS_SECRET", "REFRESH_SECRET", "sk-proj-secretsecretsecret"} {
		require.NotContains(t, combined, secret)
	}
	require.Contains(t, *captured[0].UpstreamErrorsJSON, `"runtime_guard_bucket":"invalid_encrypted_content"`)
}

func TestOpenAIRuntimeGuardLearnedBlockScopeIncludesCapabilityVersion(t *testing.T) {
	svc := &OpenAIGatewayService{}
	classification := OpenAIRuntimeGuardUpstreamErrorClassification{Bucket: OpenAIRuntimeGuardBucketUnsupportedOAuthModelChannel, Category: "capability.unsupported_oauth_model_profile_channel", Metric: "openai_runtime_guard.upstream.unsupported_oauth_model_channel", Action: "learn_block", TTL: time.Minute}
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"openai_gateway_profile_id": "profile-a", "openai_runtime_guard_capability_version": "cap-v1"}}

	recorded := svc.RecordOpenAIRuntimeGuardLearnedBlock(openAIRuntimeGuardLearnedBlockScopeForAccount(account, "gpt-5.4", "responses"), classification)
	require.True(t, recorded)
	_, blocked := svc.IsOpenAIRuntimeGuardLearnedBlocked(openAIRuntimeGuardLearnedBlockScopeForAccount(account, "gpt-5.4", "responses"))
	require.True(t, blocked)

	account.Extra["openai_runtime_guard_capability_version"] = "cap-v2"
	_, blocked = svc.IsOpenAIRuntimeGuardLearnedBlocked(openAIRuntimeGuardLearnedBlockScopeForAccount(account, "gpt-5.4", "responses"))
	require.False(t, blocked, "capability/credential version changes must not reuse stale learned blocks")
}

func TestOpenAIGatewayService_Forward_RuntimeGuardLearnedBlockChecksMappedResponsesModel(t *testing.T) {
	upstream, rec, c, svc, account := newOpenAIRuntimeGuardForwardHarness(t)
	account.Credentials["model_mapping"] = map[string]any{"alias-gpt": "gpt-5.4"}
	classification := OpenAIRuntimeGuardUpstreamErrorClassification{Bucket: OpenAIRuntimeGuardBucketUnsupportedOAuthModelChannel, Category: "capability.unsupported_oauth_model_profile_channel", Metric: "openai_runtime_guard.upstream.unsupported_oauth_model_channel", Action: "learn_block", TTL: time.Minute}
	recorded := svc.RecordOpenAIRuntimeGuardLearnedBlock(openAIRuntimeGuardLearnedBlockScopeForAccount(account, "gpt-5.4", "responses"), classification)
	require.True(t, recorded)

	result, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"alias-gpt","stream":false,"input":"hi"}`))

	require.Error(t, err)
	require.Nil(t, result)
	require.Len(t, upstream.bodies, 0, "mapped alias learned block must not call upstream")
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestClassifyOpenAIRuntimeGuardUpstreamErrorActionMatchesLearnBlockEligibility(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantBucket string
	}{
		{"illegal reasoning", `{"error":{"message":"Unsupported value: 'max' for reasoning_effort","param":"reasoning_effort","type":"invalid_request_error"}}`, OpenAIRuntimeGuardBucketIllegalReasoningEffort},
		{"context overflow", `{"error":{"message":"context length exceeded max tokens","type":"invalid_request_error"}}`, OpenAIRuntimeGuardBucketContextOverflow},
		{"shape transcript", `{"error":{"message":"Invalid value: 'input_text'. Supported values are: 'output_text' and 'refusal'.","type":"invalid_request_error","param":"input.0.content.0.type","code":"invalid_value"}}`, OpenAIRuntimeGuardBucketShapeTranscriptError},
		{"invalid encrypted", `{"error":{"message":"The encrypted content could not be verified.","type":"invalid_request_error","code":"invalid_encrypted_content"}}`, OpenAIRuntimeGuardBucketInvalidEncryptedContent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyOpenAIRuntimeGuardUpstreamError(http.StatusBadRequest, nil, []byte(tc.body), "")
			require.Equal(t, tc.wantBucket, got.Bucket)
			require.False(t, openAIRuntimeGuardClassificationCanLearnBlock(got))
			require.NotEqual(t, "learn_block", got.Action)
		})
	}

	learnable := ClassifyOpenAIRuntimeGuardUpstreamError(http.StatusForbidden, nil, nil, "ChatGPT OAuth account does not support model on this unsupported model/profile/channel")
	require.True(t, openAIRuntimeGuardClassificationCanLearnBlock(learnable))
	require.Equal(t, "learn_block", learnable.Action)
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

func TestOpenAIWSRuntimeGuardOpsMetadataStructuredAndSanitized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	account := &Account{ID: 9901, Name: "openai-ws-runtime-guard", Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	rawEvent := []byte(`{"type":"error","error":{"code":"invalid_encrypted_content","message":"The encrypted content could not be verified.","encrypted_content":"ENCRYPTED_SECRET","input":"INPUT_SECRET","messages":[{"content":"MESSAGE_SECRET"}],"access_token":"ACCESS_SECRET"}}`)

	appendOpenAIWSRuntimeGuardOpsError(c, account, http.StatusBadRequest, rawEvent, "The encrypted content could not be verified", nil)

	rawEvents, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events := rawEvents.([]*OpsUpstreamErrorEvent)
	require.Len(t, events, 1)
	require.Equal(t, OpenAIRuntimeGuardBucketInvalidEncryptedContent, events[0].RuntimeGuardBucket)
	require.Equal(t, "terminal", events[0].RuntimeGuardAction)

	singleDetailRaw, ok := c.Get(OpsUpstreamErrorDetailKey)
	require.True(t, ok)
	singleDetail, _ := singleDetailRaw.(string)
	serializedBytes, err := json.Marshal(events)
	require.NoError(t, err)
	combined := singleDetail + "\n" + string(serializedBytes)
	for _, secret := range []string{"ENCRYPTED_SECRET", "INPUT_SECRET", "MESSAGE_SECRET", "ACCESS_SECRET"} {
		require.NotContains(t, combined, secret)
	}
}
