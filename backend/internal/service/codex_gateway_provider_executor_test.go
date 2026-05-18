package service

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type codexGatewayProviderExecutorSelectorStub struct {
	selectFn func(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}) (*Account, error)
}

func (s *codexGatewayProviderExecutorSelectorStub) SelectAccountForModelWithExclusions(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}) (*Account, error) {
	return s.selectFn(ctx, groupID, sessionHash, requestedModel, excludedIDs)
}

type codexGatewayProviderAdapterStub struct {
	completeFn func(ctx context.Context, account *Account, req CodexGatewayProviderRequest) (CodexGatewayDeepSeekAdapterResult, error)
	streamFn   func(ctx context.Context, account *Account, req CodexGatewayProviderRequest) (CodexGatewayProviderResult, error)
}

func (s *codexGatewayProviderAdapterStub) Complete(ctx context.Context, account *Account, req CodexGatewayProviderRequest) (CodexGatewayDeepSeekAdapterResult, error) {
	return s.completeFn(ctx, account, req)
}

func (s *codexGatewayProviderAdapterStub) Stream(ctx context.Context, account *Account, req CodexGatewayProviderRequest) (CodexGatewayProviderResult, error) {
	return s.streamFn(ctx, account, req)
}

type codexGatewayUsageRecorderStub struct {
	inputs  []*OpenAIRecordUsageInput
	ctxErrs []error
	err     error
}

func (s *codexGatewayUsageRecorderStub) RecordUsage(ctx context.Context, input *OpenAIRecordUsageInput) error {
	if ctx != nil {
		s.ctxErrs = append(s.ctxErrs, ctx.Err())
	} else {
		s.ctxErrs = append(s.ctxErrs, nil)
	}
	s.inputs = append(s.inputs, input)
	return s.err
}

func newCodexGatewayProviderExecutorForTest() *CodexGatewayProviderExecutor {
	cfg := &config.Config{}
	cfg.Gateway.Codex.ProviderGroups.OpenAI = 101
	cfg.Gateway.Codex.ProviderGroups.DeepSeek = 202
	cfg.Gateway.Codex.ProviderGroups.Anthropic = 303
	return &CodexGatewayProviderExecutor{cfg: cfg}
}

func TestCodexGatewayProviderExecutor_CompleteFailsOverBeforeVisibleOutput(t *testing.T) {
	account1 := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
	account2 := &Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
	recorder := &codexGatewayUsageRecorderStub{}
	captureBaseDir := t.TempDir()
	capture := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                  true,
		BaseDir:                  captureBaseDir,
		HashKeyFile:              filepath.Join(captureBaseDir, ".key"),
		CaptureSuccessSampleRate: 1,
	})
	defer capture.Close()
	trace := capture.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{TraceID: "failover"})
	require.NotNil(t, trace)
	executor := newCodexGatewayProviderExecutorForTest()
	executor.accountSelector = &codexGatewayProviderExecutorSelectorStub{
		selectFn: func(_ context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}) (*Account, error) {
			require.NotNil(t, groupID)
			require.Equal(t, int64(101), *groupID)
			require.Equal(t, "sess_hash", sessionHash)
			require.Equal(t, "gpt-5.5", requestedModel)
			if len(excludedIDs) == 0 {
				return account1, nil
			}
			_, firstExcluded := excludedIDs[account1.ID]
			require.True(t, firstExcluded)
			return account2, nil
		},
	}
	executor.usageRecorder = recorder
	executor.openaiAdapter = &codexGatewayProviderAdapterStub{
		completeFn: func(_ context.Context, account *Account, req CodexGatewayProviderRequest) (CodexGatewayDeepSeekAdapterResult, error) {
			if account.ID == account1.ID {
				return CodexGatewayDeepSeekAdapterResult{}, &UpstreamFailoverError{StatusCode: http.StatusTooManyRequests}
			}
			require.Equal(t, "gpt-5.5", req.Model.UpstreamModel)
			return CodexGatewayDeepSeekAdapterResult{
				ServiceResponse: CodexGatewayServiceResponse{
					StatusCode: http.StatusOK,
					Headers:    http.Header{"Content-Type": []string{"application/json"}},
					Body:       []byte(`{"id":"resp_2"}`),
				},
				ProviderResult: CodexGatewayProviderResult{
					ResponseID:        "resp_2",
					UpstreamRequestID: "req_2",
					UpstreamModel:     "gpt-5.5",
					Usage: CodexGatewayProviderUsage{
						InputTokens:  12,
						OutputTokens: 5,
						TotalTokens:  17,
					},
				},
			}, nil
		},
	}

	resp, err := executor.Complete(context.Background(), CodexGatewayProviderRequest{
		Request:      CodexGatewayResponsesRequest{APIKey: validCodexGatewayAPIKeyForTest()},
		Model:        CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
		Parsed:       CodexGatewayResponsesCreateRequest{Model: "gpt-5.5"},
		SessionKey:   "sess_hash",
		IsolationKey: "iso_hash",
		CaptureTrace: trace,
	})
	require.NoError(t, err)
	capture.FinishTrace(trace, CodexGatewayCaptureFinishSummary{Status: "ok"})
	require.NoError(t, capture.Close())
	require.NotNil(t, resp)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Len(t, recorder.inputs, 1)
	require.Equal(t, account2.ID, recorder.inputs[0].Account.ID)
	require.Equal(t, "req_2", recorder.inputs[0].Result.RequestID)
	traceDir := filepath.Join(captureBaseDir, time.Now().Format("2006-01-02"), "failover")
	attempts, err := os.ReadFile(filepath.Join(traceDir, "provider_attempts.jsonl"))
	require.NoError(t, err)
	require.Contains(t, string(attempts), `"attempt":1`)
	require.Contains(t, string(attempts), `"attempt":2`)
	errorsBytes, err := os.ReadFile(filepath.Join(traceDir, "errors.jsonl"))
	require.NoError(t, err)
	require.Contains(t, string(errorsBytes), "upstream_failover")
}

func TestCodexGatewayProviderExecutor_StreamDoesNotFailoverAfterVisibleOutput(t *testing.T) {
	account1 := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
	account2 := &Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
	executor := newCodexGatewayProviderExecutorForTest()
	selectionCalls := 0
	executor.accountSelector = &codexGatewayProviderExecutorSelectorStub{
		selectFn: func(_ context.Context, _ *int64, _ string, _ string, excludedIDs map[int64]struct{}) (*Account, error) {
			selectionCalls++
			if selectionCalls == 1 {
				require.Empty(t, excludedIDs)
				return account1, nil
			}
			return account2, nil
		},
	}
	executor.openaiAdapter = &codexGatewayProviderAdapterStub{
		streamFn: func(_ context.Context, account *Account, req CodexGatewayProviderRequest) (CodexGatewayProviderResult, error) {
			require.Equal(t, account1.ID, account.ID)
			_, writeErr := req.Request.StreamWriter.Write([]byte("data: visible\n\n"))
			require.NoError(t, writeErr)
			return CodexGatewayProviderResult{}, errors.New("stream closed after output flush")
		},
	}
	var out bytes.Buffer

	err := executor.Stream(context.Background(), CodexGatewayProviderRequest{
		Request: CodexGatewayResponsesRequest{
			APIKey:       validCodexGatewayAPIKeyForTest(),
			StreamWriter: &out,
		},
		Model:      CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
		Parsed:     CodexGatewayResponsesCreateRequest{Model: "gpt-5.5", Stream: testBoolPtr(true)},
		SessionKey: "sess_hash",
	})
	require.EqualError(t, err, "stream closed after output flush")
	require.Equal(t, 1, selectionCalls)
	require.Equal(t, "data: visible\n\n", out.String())
}

func TestCodexGatewayOpenAIHostedWebSearchOutput_ReconstructsTextFromSSE(t *testing.T) {
	body := []byte("" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"response_id\":\"resp_1\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\"hello\"}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"response_id\":\"resp_1\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\" world\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"model\":\"gpt-5.5\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":2,\"output_tokens\":2,\"total_tokens\":4}}}\n\n" +
		"data: [DONE]\n\n")

	got := codexGatewayOpenAIHostedWebSearchOutput("latest news", body)
	require.Contains(t, got, `"provider":"openai_responses"`)
	require.Contains(t, got, `"summary":"hello world"`)
	require.NotContains(t, got, "completed the web search, but no output text was returned")
}

func testBoolPtr(v bool) *bool {
	return &v
}

func TestCodexGatewayProviderExecutor_UsesDeepSeekProviderGroup(t *testing.T) {
	account := &Account{ID: 9, Platform: PlatformOpenAI, Type: AccountTypeUpstream, Status: StatusActive, Schedulable: true}
	executor := newCodexGatewayProviderExecutorForTest()
	executor.accountSelector = &codexGatewayProviderExecutorSelectorStub{
		selectFn: func(_ context.Context, groupID *int64, _ string, requestedModel string, _ map[int64]struct{}) (*Account, error) {
			require.NotNil(t, groupID)
			require.Equal(t, int64(202), *groupID)
			require.Equal(t, "deepseek-v4-pro", requestedModel)
			return account, nil
		},
	}
	executor.deepseek = &codexGatewayProviderAdapterStub{
		completeFn: func(_ context.Context, account *Account, _ CodexGatewayProviderRequest) (CodexGatewayDeepSeekAdapterResult, error) {
			require.Equal(t, int64(9), account.ID)
			return CodexGatewayDeepSeekAdapterResult{
				ServiceResponse: CodexGatewayServiceResponse{StatusCode: http.StatusOK},
				ProviderResult:  CodexGatewayProviderResult{UpstreamRequestID: "req_ds", UpstreamModel: "deepseek-v4-pro"},
			}, nil
		},
	}
	executor.usageRecorder = &codexGatewayUsageRecorderStub{}

	resp, err := executor.Complete(context.Background(), CodexGatewayProviderRequest{
		Request:      CodexGatewayResponsesRequest{APIKey: validCodexGatewayAPIKeyForTest()},
		Model:        CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"},
		Parsed:       CodexGatewayResponsesCreateRequest{Model: "deepseek-v4-pro"},
		SessionKey:   "sess_hash",
		IsolationKey: "iso_hash",
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestCodexGatewayProviderExecutor_DeepSeekSelectionUsesModelScopedStickySession(t *testing.T) {
	account := &Account{ID: 9, Platform: PlatformOpenAI, Type: AccountTypeUpstream, Status: StatusActive, Schedulable: true}
	executor := newCodexGatewayProviderExecutorForTest()
	var selectedSessionHash string
	executor.accountSelector = &codexGatewayProviderExecutorSelectorStub{
		selectFn: func(_ context.Context, groupID *int64, sessionHash string, requestedModel string, _ map[int64]struct{}) (*Account, error) {
			require.NotNil(t, groupID)
			require.Equal(t, int64(202), *groupID)
			require.Equal(t, "deepseek-v4-pro", requestedModel)
			selectedSessionHash = sessionHash
			return account, nil
		},
	}
	executor.deepseek = &codexGatewayProviderAdapterStub{
		completeFn: func(_ context.Context, account *Account, _ CodexGatewayProviderRequest) (CodexGatewayDeepSeekAdapterResult, error) {
			require.Equal(t, int64(9), account.ID)
			return CodexGatewayDeepSeekAdapterResult{
				ServiceResponse: CodexGatewayServiceResponse{StatusCode: http.StatusOK},
				ProviderResult:  CodexGatewayProviderResult{UpstreamRequestID: "req_ds", UpstreamModel: "deepseek-v4-pro"},
			}, nil
		},
	}
	executor.usageRecorder = &codexGatewayUsageRecorderStub{}

	resp, err := executor.Complete(context.Background(), CodexGatewayProviderRequest{
		Request:      CodexGatewayResponsesRequest{APIKey: validCodexGatewayAPIKeyForTest()},
		Model:        CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"},
		Parsed:       CodexGatewayResponsesCreateRequest{Model: "deepseek-v4-pro"},
		SessionKey:   "session_hash",
		IsolationKey: "isolation_hash",
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "session_hash:codex_gateway:deepseek:deepseek-v4-pro:isolation_hash", selectedSessionHash)
}

func TestCodexGatewayProviderSelectionSessionKeyScopesOnlyDeepSeek(t *testing.T) {
	base := CodexGatewayProviderRequest{
		Model:        CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"},
		SessionKey:   "session_hash",
		IsolationKey: "isolation_hash",
	}

	pro := codexGatewayProviderSelectionSessionKey(base)
	flashReq := base
	flashReq.Model.UpstreamModel = "deepseek-v4-flash"
	flashReq.Model.Slug = "deepseek-v4-flash"
	flash := codexGatewayProviderSelectionSessionKey(flashReq)
	otherIsolationReq := base
	otherIsolationReq.IsolationKey = "other_isolation"
	otherIsolation := codexGatewayProviderSelectionSessionKey(otherIsolationReq)
	slugFallbackReq := base
	slugFallbackReq.Model.UpstreamModel = ""
	slugFallback := codexGatewayProviderSelectionSessionKey(slugFallbackReq)
	openAIReq := base
	openAIReq.Model.Provider = "openai"
	openAIReq.Model.UpstreamModel = "gpt-5.4"
	anthropicReq := base
	anthropicReq.Model.Provider = "anthropic"
	anthropicReq.Model.UpstreamModel = "claude-sonnet-4-6"

	require.Equal(t, "session_hash:codex_gateway:deepseek:deepseek-v4-pro:isolation_hash", pro)
	require.Equal(t, "session_hash:codex_gateway:deepseek:deepseek-v4-flash:isolation_hash", flash)
	require.Equal(t, "session_hash:codex_gateway:deepseek:deepseek-v4-pro:other_isolation", otherIsolation)
	require.Equal(t, "session_hash:codex_gateway:deepseek:deepseek-v4-pro:isolation_hash", slugFallback)
	require.NotEqual(t, pro, flash)
	require.NotEqual(t, pro, otherIsolation)
	require.Equal(t, "session_hash", codexGatewayProviderSelectionSessionKey(openAIReq))
	require.Equal(t, "session_hash", codexGatewayProviderSelectionSessionKey(anthropicReq))
}

func TestCodexGatewayProviderExecutor_UsesAnthropicProviderGroup(t *testing.T) {
	account := &Account{ID: 10, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
	executor := newCodexGatewayProviderExecutorForTest()
	executor.accountSelector = &codexGatewayProviderExecutorSelectorStub{
		selectFn: func(_ context.Context, groupID *int64, _ string, requestedModel string, _ map[int64]struct{}) (*Account, error) {
			require.NotNil(t, groupID)
			require.Equal(t, int64(303), *groupID)
			require.Equal(t, "claude-sonnet-4-6", requestedModel)
			return account, nil
		},
	}
	executor.anthropic = &codexGatewayProviderAdapterStub{
		completeFn: func(_ context.Context, account *Account, _ CodexGatewayProviderRequest) (CodexGatewayDeepSeekAdapterResult, error) {
			require.Equal(t, int64(10), account.ID)
			return CodexGatewayDeepSeekAdapterResult{
				ServiceResponse: CodexGatewayServiceResponse{StatusCode: http.StatusOK},
				ProviderResult:  CodexGatewayProviderResult{UpstreamRequestID: "req_claude", UpstreamModel: "claude-sonnet-4-6"},
			}, nil
		},
	}
	executor.usageRecorder = &codexGatewayUsageRecorderStub{}

	resp, err := executor.Complete(context.Background(), CodexGatewayProviderRequest{
		Request:      CodexGatewayResponsesRequest{APIKey: validCodexGatewayAPIKeyForTest()},
		Model:        CodexGatewayModel{Slug: "claude-sonnet-4-6", Provider: "anthropic", UpstreamModel: "claude-sonnet-4-6"},
		Parsed:       CodexGatewayResponsesCreateRequest{Model: "claude-sonnet-4-6"},
		SessionKey:   "sess_hash",
		IsolationKey: "iso_hash",
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestCodexGatewayAnthropicAccountBaseURLFallsBackForLegacyAPIKeyType(t *testing.T) {
	account := &Account{
		ID:       10,
		Platform: PlatformAnthropic,
		Type:     "api_key",
		Credentials: map[string]any{
			"base_url": "https://anthropic-compatible.example/v1",
		},
	}

	require.Equal(t, "https://anthropic-compatible.example/v1", codexGatewayAnthropicAccountBaseURL(account))
}

func TestCodexGatewayProviderExecutor_UsesAdminProviderGroupState(t *testing.T) {
	account := &Account{ID: 9, Platform: PlatformOpenAI, Type: AccountTypeUpstream, Status: StatusActive, Schedulable: true}
	executor := newCodexGatewayProviderExecutorForTest()
	executor.cfg.Gateway.Codex.ProviderGroups.DeepSeek = 0
	executor.stateSource = &codexGatewayRegistryStateSourceStub{
		state: &CodexGatewayRegistryState{
			ProviderGroups: map[CodexGatewayProvider]CodexGatewayProviderRuntime{
				CodexGatewayProviderDeepSeek: {
					Provider: CodexGatewayProviderDeepSeek,
					GroupID:  303,
					Healthy:  true,
				},
			},
		},
	}
	executor.accountSelector = &codexGatewayProviderExecutorSelectorStub{
		selectFn: func(_ context.Context, groupID *int64, _ string, requestedModel string, _ map[int64]struct{}) (*Account, error) {
			require.NotNil(t, groupID)
			require.Equal(t, int64(303), *groupID)
			require.Equal(t, "deepseek-v4-pro", requestedModel)
			return account, nil
		},
	}
	executor.deepseek = &codexGatewayProviderAdapterStub{
		completeFn: func(_ context.Context, account *Account, _ CodexGatewayProviderRequest) (CodexGatewayDeepSeekAdapterResult, error) {
			require.Equal(t, int64(9), account.ID)
			return CodexGatewayDeepSeekAdapterResult{
				ServiceResponse: CodexGatewayServiceResponse{StatusCode: http.StatusOK},
				ProviderResult:  CodexGatewayProviderResult{UpstreamRequestID: "req_ds_admin", UpstreamModel: "deepseek-v4-pro"},
			}, nil
		},
	}
	executor.usageRecorder = &codexGatewayUsageRecorderStub{}

	resp, err := executor.Complete(context.Background(), CodexGatewayProviderRequest{
		Request:      CodexGatewayResponsesRequest{APIKey: validCodexGatewayAPIKeyForTest()},
		Model:        CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"},
		Parsed:       CodexGatewayResponsesCreateRequest{Model: "deepseek-v4-pro"},
		SessionKey:   "sess_hash",
		IsolationKey: "iso_hash",
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestCodexGatewayProviderExecutor_CompleteReturnsUnavailableWhenNoAccountCanBeSelected(t *testing.T) {
	executor := newCodexGatewayProviderExecutorForTest()
	executor.accountSelector = &codexGatewayProviderExecutorSelectorStub{
		selectFn: func(_ context.Context, _ *int64, _ string, _ string, _ map[int64]struct{}) (*Account, error) {
			return nil, ErrNoAvailableAccounts
		},
	}
	executor.openaiAdapter = &codexGatewayProviderAdapterStub{
		completeFn: func(_ context.Context, _ *Account, _ CodexGatewayProviderRequest) (CodexGatewayDeepSeekAdapterResult, error) {
			return CodexGatewayDeepSeekAdapterResult{}, nil
		},
	}

	_, err := executor.Complete(context.Background(), CodexGatewayProviderRequest{
		Request:    CodexGatewayResponsesRequest{APIKey: validCodexGatewayAPIKeyForTest()},
		Model:      CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
		Parsed:     CodexGatewayResponsesCreateRequest{Model: "gpt-5.5"},
		SessionKey: "sess_hash",
	})
	require.Error(t, err)
	var unavailable *CodexGatewayProviderUnavailableError
	require.ErrorAs(t, err, &unavailable)
	require.Equal(t, CodexGatewayProviderUnavailableNoAccounts, unavailable.Kind)
}

func TestCodexGatewayProviderExecutor_CompleteReturnsLastFailoverWhenAccountsExhausted(t *testing.T) {
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
	executor := newCodexGatewayProviderExecutorForTest()
	selectionCalls := 0
	executor.accountSelector = &codexGatewayProviderExecutorSelectorStub{
		selectFn: func(_ context.Context, _ *int64, _ string, _ string, excludedIDs map[int64]struct{}) (*Account, error) {
			selectionCalls++
			if selectionCalls == 1 {
				require.Empty(t, excludedIDs)
				return account, nil
			}
			_, excluded := excludedIDs[account.ID]
			require.True(t, excluded)
			return nil, ErrNoAvailableAccounts
		},
	}
	executor.openaiAdapter = &codexGatewayProviderAdapterStub{
		completeFn: func(_ context.Context, _ *Account, _ CodexGatewayProviderRequest) (CodexGatewayDeepSeekAdapterResult, error) {
			return CodexGatewayDeepSeekAdapterResult{}, &UpstreamFailoverError{StatusCode: http.StatusTooManyRequests}
		},
	}

	_, err := executor.Complete(context.Background(), CodexGatewayProviderRequest{
		Request:    CodexGatewayResponsesRequest{APIKey: validCodexGatewayAPIKeyForTest()},
		Model:      CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
		Parsed:     CodexGatewayResponsesCreateRequest{Model: "gpt-5.5"},
		SessionKey: "sess_hash",
	})
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusTooManyRequests, failoverErr.StatusCode)
	require.Equal(t, 2, selectionCalls)
}

func TestCodexGatewayProviderExecutor_CompletePropagatesSelectorErrors(t *testing.T) {
	executor := newCodexGatewayProviderExecutorForTest()
	executor.accountSelector = &codexGatewayProviderExecutorSelectorStub{
		selectFn: func(_ context.Context, _ *int64, _ string, _ string, _ map[int64]struct{}) (*Account, error) {
			return nil, errors.New("db unavailable")
		},
	}
	executor.openaiAdapter = &codexGatewayProviderAdapterStub{
		completeFn: func(_ context.Context, _ *Account, _ CodexGatewayProviderRequest) (CodexGatewayDeepSeekAdapterResult, error) {
			return CodexGatewayDeepSeekAdapterResult{}, nil
		},
	}

	_, err := executor.Complete(context.Background(), CodexGatewayProviderRequest{
		Request:    CodexGatewayResponsesRequest{APIKey: validCodexGatewayAPIKeyForTest()},
		Model:      CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
		Parsed:     CodexGatewayResponsesCreateRequest{Model: "gpt-5.5"},
		SessionKey: "sess_hash",
	})
	require.EqualError(t, err, "db unavailable")
}

func TestCodexGatewayProviderExecutor_CompleteDoesNotMaskSelectorErrorsAfterFailover(t *testing.T) {
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
	executor := newCodexGatewayProviderExecutorForTest()
	selectionCalls := 0
	executor.accountSelector = &codexGatewayProviderExecutorSelectorStub{
		selectFn: func(_ context.Context, _ *int64, _ string, _ string, excludedIDs map[int64]struct{}) (*Account, error) {
			selectionCalls++
			if selectionCalls == 1 {
				require.Empty(t, excludedIDs)
				return account, nil
			}
			_, excluded := excludedIDs[account.ID]
			require.True(t, excluded)
			return nil, errors.New("db unavailable")
		},
	}
	executor.openaiAdapter = &codexGatewayProviderAdapterStub{
		completeFn: func(_ context.Context, _ *Account, _ CodexGatewayProviderRequest) (CodexGatewayDeepSeekAdapterResult, error) {
			return CodexGatewayDeepSeekAdapterResult{}, &UpstreamFailoverError{StatusCode: http.StatusTooManyRequests}
		},
	}

	_, err := executor.Complete(context.Background(), CodexGatewayProviderRequest{
		Request:    CodexGatewayResponsesRequest{APIKey: validCodexGatewayAPIKeyForTest()},
		Model:      CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
		Parsed:     CodexGatewayResponsesCreateRequest{Model: "gpt-5.5"},
		SessionKey: "sess_hash",
	})
	require.EqualError(t, err, "db unavailable")
	require.Equal(t, 2, selectionCalls)
}

func TestCodexGatewayProviderExecutor_StreamReturnsLastFailoverWhenAccountsExhausted(t *testing.T) {
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
	executor := newCodexGatewayProviderExecutorForTest()
	selectionCalls := 0
	executor.accountSelector = &codexGatewayProviderExecutorSelectorStub{
		selectFn: func(_ context.Context, _ *int64, _ string, _ string, excludedIDs map[int64]struct{}) (*Account, error) {
			selectionCalls++
			if selectionCalls == 1 {
				require.Empty(t, excludedIDs)
				return account, nil
			}
			_, excluded := excludedIDs[account.ID]
			require.True(t, excluded)
			return nil, ErrNoAvailableAccounts
		},
	}
	executor.openaiAdapter = &codexGatewayProviderAdapterStub{
		streamFn: func(_ context.Context, _ *Account, _ CodexGatewayProviderRequest) (CodexGatewayProviderResult, error) {
			return CodexGatewayProviderResult{}, &UpstreamFailoverError{StatusCode: http.StatusTooManyRequests}
		},
	}
	var out bytes.Buffer

	err := executor.Stream(context.Background(), CodexGatewayProviderRequest{
		Request: CodexGatewayResponsesRequest{
			APIKey:       validCodexGatewayAPIKeyForTest(),
			StreamWriter: &out,
		},
		Model:      CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
		Parsed:     CodexGatewayResponsesCreateRequest{Model: "gpt-5.5", Stream: testBoolPtr(true)},
		SessionKey: "sess_hash",
	})
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusTooManyRequests, failoverErr.StatusCode)
	require.Equal(t, 2, selectionCalls)
	require.Empty(t, out.String())
}

func TestCodexGatewayProviderExecutor_StreamDoesNotMaskSelectorErrorsAfterFailover(t *testing.T) {
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
	executor := newCodexGatewayProviderExecutorForTest()
	selectionCalls := 0
	executor.accountSelector = &codexGatewayProviderExecutorSelectorStub{
		selectFn: func(_ context.Context, _ *int64, _ string, _ string, excludedIDs map[int64]struct{}) (*Account, error) {
			selectionCalls++
			if selectionCalls == 1 {
				require.Empty(t, excludedIDs)
				return account, nil
			}
			_, excluded := excludedIDs[account.ID]
			require.True(t, excluded)
			return nil, errors.New("db unavailable")
		},
	}
	executor.openaiAdapter = &codexGatewayProviderAdapterStub{
		streamFn: func(_ context.Context, _ *Account, _ CodexGatewayProviderRequest) (CodexGatewayProviderResult, error) {
			return CodexGatewayProviderResult{}, &UpstreamFailoverError{StatusCode: http.StatusTooManyRequests}
		},
	}
	var out bytes.Buffer

	err := executor.Stream(context.Background(), CodexGatewayProviderRequest{
		Request: CodexGatewayResponsesRequest{
			APIKey:       validCodexGatewayAPIKeyForTest(),
			StreamWriter: &out,
		},
		Model:      CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
		Parsed:     CodexGatewayResponsesCreateRequest{Model: "gpt-5.5", Stream: testBoolPtr(true)},
		SessionKey: "sess_hash",
	})
	require.EqualError(t, err, "db unavailable")
	require.Equal(t, 2, selectionCalls)
	require.Empty(t, out.String())
}

func TestCodexGatewayProviderExecutor_RecordUsageDurationUsesCallStart(t *testing.T) {
	executor := newCodexGatewayProviderExecutorForTest()
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
	recorder := &codexGatewayUsageRecorderStub{}
	executor.accountSelector = &codexGatewayProviderExecutorSelectorStub{
		selectFn: func(_ context.Context, _ *int64, _ string, _ string, _ map[int64]struct{}) (*Account, error) {
			return account, nil
		},
	}
	executor.usageRecorder = recorder
	executor.openaiAdapter = &codexGatewayProviderAdapterStub{
		completeFn: func(_ context.Context, _ *Account, _ CodexGatewayProviderRequest) (CodexGatewayDeepSeekAdapterResult, error) {
			time.Sleep(5 * time.Millisecond)
			return CodexGatewayDeepSeekAdapterResult{
				ServiceResponse: CodexGatewayServiceResponse{StatusCode: http.StatusOK},
				ProviderResult:  CodexGatewayProviderResult{UpstreamRequestID: "req_1", UpstreamModel: "gpt-5.5"},
			}, nil
		},
	}
	_, err := executor.Complete(context.Background(), CodexGatewayProviderRequest{
		Request: CodexGatewayResponsesRequest{APIKey: validCodexGatewayAPIKeyForTest()},
		Model:   CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
		Parsed:  CodexGatewayResponsesCreateRequest{Model: "gpt-5.5"},
	})
	require.NoError(t, err)
	require.Len(t, recorder.inputs, 1)
	require.GreaterOrEqual(t, recorder.inputs[0].Result.Duration, 5*time.Millisecond)
}

func TestCopyCodexGatewayHTTPHeaders_RemovesStaleAllowlistedHeaders(t *testing.T) {
	dst := http.Header{
		"X-Request-Id":       []string{"stale"},
		"X-Codex-Turn-State": []string{"old"},
		"Content-Type":       []string{"application/json"},
	}
	src := http.Header{
		"Content-Type": []string{"text/event-stream"},
	}

	copyCodexGatewayHTTPHeaders(dst, src)

	require.Equal(t, "text/event-stream", dst.Get("Content-Type"))
	require.Empty(t, dst.Get("X-Request-Id"))
	require.Empty(t, dst.Get("X-Codex-Turn-State"))
}
