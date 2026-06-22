package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
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

type codexGatewayProviderExecutorHTTPUpstreamStub struct {
	doFn func(req *http.Request, proxyURL string, accountID int64, concurrency int) (*http.Response, error)
}

func (s *codexGatewayProviderExecutorHTTPUpstreamStub) Do(req *http.Request, proxyURL string, accountID int64, concurrency int) (*http.Response, error) {
	if s != nil && s.doFn != nil {
		return s.doFn(req, proxyURL, accountID, concurrency)
	}
	return nil, errors.New("unexpected HTTP upstream call")
}

func (s *codexGatewayProviderExecutorHTTPUpstreamStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, concurrency)
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
	cfg.Gateway.Codex.ProviderGroups.Agnes = 404
	return &CodexGatewayProviderExecutor{cfg: cfg}
}

func TestCodexGatewayProviderExecutor_CompleteRoutesAgnesThroughAgnesAdapterAndProviderGroup(t *testing.T) {
	account := &Account{ID: 44, Platform: PlatformOpenAI, Type: AccountTypeUpstream, Status: StatusActive, Schedulable: true}
	executor := newCodexGatewayProviderExecutorForTest()
	executor.accountSelector = &codexGatewayProviderExecutorSelectorStub{
		selectFn: func(_ context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}) (*Account, error) {
			require.NotNil(t, groupID)
			require.Equal(t, int64(404), *groupID)
			require.Equal(t, "agnes_session:codex_gateway:agnes:agnes-2.0-flash:shared", sessionHash)
			require.Equal(t, "agnes-2.0-flash", requestedModel)
			require.Empty(t, excludedIDs)
			return account, nil
		},
	}
	executor.agnes = &codexGatewayProviderAdapterStub{
		completeFn: func(_ context.Context, selected *Account, req CodexGatewayProviderRequest) (CodexGatewayDeepSeekAdapterResult, error) {
			require.Equal(t, account.ID, selected.ID)
			require.Equal(t, "agnes", req.Model.Provider)
			return CodexGatewayDeepSeekAdapterResult{
				ServiceResponse: CodexGatewayServiceResponse{StatusCode: http.StatusOK, Headers: http.Header{"Content-Type": []string{"application/json"}}, Body: []byte(`{"id":"resp_agnes"}`)},
				ProviderResult:  CodexGatewayProviderResult{ResponseID: "resp_agnes", UpstreamModel: "agnes-2.0-flash"},
			}, nil
		},
	}

	resp, err := executor.Complete(context.Background(), CodexGatewayProviderRequest{
		Request:    CodexGatewayResponsesRequest{APIKey: validCodexGatewayAPIKeyForTest()},
		Model:      CodexGatewayModel{Slug: "agnes-2.0-flash", Provider: "agnes", UpstreamModel: "agnes-2.0-flash"},
		Parsed:     CodexGatewayResponsesCreateRequest{Model: "agnes-2.0-flash"},
		SessionKey: "agnes_session",
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestCodexGatewayAgnesProviderAdapter_CompleteOmitsThinkingAndPreservesImagesByModel(t *testing.T) {
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/chat/completions", r.URL.Path)
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		bodies = append(bodies, body)
		require.NotContains(t, body, "thinking")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-request-id", "req_agnes")
		_, _ = io.WriteString(w, `{"id":"chatcmpl_agnes","object":"chat.completion","model":"agnes-2.0-flash","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`)
	}))
	defer server.Close()

	adapter := &codexGatewayAgnesProviderAdapter{}
	account := &Account{
		ID:       44,
		Platform: PlatformOpenAI,
		Type:     AccountTypeUpstream,
		Credentials: map[string]any{
			"base_url": server.URL,
			"api_key":  "sk-agnes",
		},
	}

	_, err := adapter.Complete(context.Background(), account, CodexGatewayProviderRequest{
		Model: CodexGatewayModel{
			Slug:                        "agnes-2.0-flash",
			Provider:                    "agnes",
			UpstreamModel:               "agnes-2.0-flash",
			InputModalities:             []string{"text", "image"},
			SupportsImageDetailOriginal: true,
		},
		Parsed: CodexGatewayResponsesCreateRequest{
			Model:     "agnes-2.0-flash",
			Input:     json.RawMessage(`"describe the image"`),
			Reasoning: json.RawMessage(`{"effort":"xhigh"}`),
		},
	})
	require.NoError(t, err)
	require.Len(t, bodies, 1)
	require.Equal(t, "high", bodies[0]["reasoning_effort"])
	messages := bodies[0]["messages"].([]any)
	require.Equal(t, "describe the image", messages[0].(map[string]any)["content"])
}

func TestCodexGatewayAgnesProviderAdapter_CompleteRejectsImagesForTextOnlyModel(t *testing.T) {
	adapter := &codexGatewayAgnesProviderAdapter{}
	account := &Account{
		ID:       44,
		Platform: PlatformOpenAI,
		Type:     AccountTypeUpstream,
		Credentials: map[string]any{
			"base_url": "https://agnes.invalid",
			"api_key":  "sk-agnes",
		},
	}

	_, err := adapter.Complete(context.Background(), account, CodexGatewayProviderRequest{
		Model: CodexGatewayModel{Slug: "agnes-1.5-flash", Provider: "agnes", UpstreamModel: "agnes-1.5-flash", InputModalities: []string{"text"}},
		Parsed: CodexGatewayResponsesCreateRequest{
			Model: "agnes-1.5-flash",
			Input: json.RawMessage(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"describe"},{"type":"input_image","image_url":"data:image/png;base64,AAAA"}]}]`),
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not support image input")
}

func TestCodexGatewayAgnesProviderAdapter_CompleteRejectsEmptyBaseURL(t *testing.T) {
	adapter := &codexGatewayAgnesProviderAdapter{}
	account := &Account{
		ID:       44,
		Platform: PlatformOpenAI,
		Type:     AccountTypeUpstream,
		Credentials: map[string]any{
			"api_key": "sk-agnes",
		},
	}

	_, err := adapter.Complete(context.Background(), account, CodexGatewayProviderRequest{
		Model:  CodexGatewayModel{Slug: "agnes-2.0-flash", Provider: "agnes", UpstreamModel: "agnes-2.0-flash"},
		Parsed: CodexGatewayResponsesCreateRequest{Model: "agnes-2.0-flash", Input: json.RawMessage(`"hello"`)},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "AGNES provider account requires base_url")
	require.NotContains(t, err.Error(), "api.openai.com")
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

func TestCodexGatewayProviderExecutor_ExecuteOpenAIHostedWebSearch_FailsOverAcrossAccountsAndModels(t *testing.T) {
	executor := newCodexGatewayProviderExecutorForTest()
	flakyMini := &Account{
		ID:          123,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"base_url": "https://api.openai-mini.example",
			"api_key":  "sk-mini",
		},
	}
	stableFallback := &Account{
		ID:          124,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeUpstream,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"api_key":  "sk-real-upstream",
			"base_url": "https://api.fallback.example",
		},
	}

	selectionCalls := 0
	executor.accountSelector = &codexGatewayProviderExecutorSelectorStub{
		selectFn: func(_ context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}) (*Account, error) {
			require.NotNil(t, groupID)
			require.Equal(t, int64(101), *groupID)
			require.Equal(t, "user_session:codex_gateway:hosted_web_search", sessionHash)
			selectionCalls++
			switch selectionCalls {
			case 1:
				require.Empty(t, excludedIDs)
				require.Equal(t, codexGatewayHostedWebSearchOpenAIModel, requestedModel)
				return flakyMini, nil
			case 2:
				_, excluded := excludedIDs[flakyMini.ID]
				require.True(t, excluded)
				require.Equal(t, codexGatewayHostedWebSearchOpenAIModel, requestedModel)
				return stableFallback, nil
			default:
				t.Fatalf("unexpected selector call %d", selectionCalls)
				return nil, nil
			}
		},
	}
	executor.openaiGateway = &OpenAIGatewayService{
		cfg: &config.Config{
			Security: config.SecurityConfig{
				URLAllowlist: config.URLAllowlistConfig{
					Enabled:           false,
					AllowInsecureHTTP: true,
				},
			},
		},
		httpUpstream: &codexGatewayProviderExecutorHTTPUpstreamStub{
			doFn: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				switch req.Header.Get("Authorization") {
				case "Bearer sk-mini":
					require.Equal(t, "https://api.openai-mini.example/v1/responses", req.URL.String())
					return &http.Response{
						StatusCode: http.StatusTooManyRequests,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"rate limited"}}`)),
					}, nil
				case "Bearer sk-real-upstream":
					require.Equal(t, "https://api.fallback.example/v1/responses", req.URL.String())
				default:
					t.Fatalf("unexpected auth header %q", req.Header.Get("Authorization"))
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body: io.NopCloser(strings.NewReader(`{
						"id":"resp_hosted_search",
						"object":"response",
						"status":"completed",
						"output":[
							{
								"id":"msg_1",
								"type":"message",
								"role":"assistant",
								"content":[
									{"type":"output_text","text":"found result from hosted search"}
								]
							}
						]
					}`)),
				}, nil
			},
		},
	}

	out, err := executor.executeOpenAIHostedWebSearch(context.Background(), CodexGatewayProviderRequest{
		SessionKey: "user_session",
		Model: CodexGatewayModel{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		},
	}, "latest news")
	require.NoError(t, err)
	require.Equal(t, 2, selectionCalls)
	require.Contains(t, out, `"provider":"openai_responses"`)
	require.Contains(t, out, `"summary":"found result from hosted search"`)
}

func TestCodexGatewayProviderExecutor_ExecuteOpenAIHostedWebSearch_TriesNextSearchModelWhenCurrentModelHasNoAccounts(t *testing.T) {
	executor := newCodexGatewayProviderExecutorForTest()
	fallbackAccount := &Account{
		ID:          202,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeUpstream,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"base_url": "https://api.fallback.example",
			"api_key":  "sk-fallback",
		},
	}

	selectionCalls := 0
	executor.accountSelector = &codexGatewayProviderExecutorSelectorStub{
		selectFn: func(_ context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}) (*Account, error) {
			require.NotNil(t, groupID)
			require.Equal(t, int64(101), *groupID)
			require.Equal(t, "user_session:codex_gateway:hosted_web_search", sessionHash)
			selectionCalls++
			switch selectionCalls {
			case 1:
				require.Empty(t, excludedIDs)
				require.Equal(t, codexGatewayHostedWebSearchOpenAIModel, requestedModel)
				return nil, ErrNoAvailableAccounts
			case 2:
				require.Empty(t, excludedIDs)
				require.Equal(t, "gpt-5.4", requestedModel)
				return fallbackAccount, nil
			default:
				t.Fatalf("unexpected selector call %d", selectionCalls)
				return nil, nil
			}
		},
	}
	executor.openaiGateway = &OpenAIGatewayService{
		cfg: &config.Config{
			Security: config.SecurityConfig{
				URLAllowlist: config.URLAllowlistConfig{
					Enabled:           false,
					AllowInsecureHTTP: true,
				},
			},
		},
		httpUpstream: &codexGatewayProviderExecutorHTTPUpstreamStub{},
	}
	executor.openaiGateway.httpUpstream = &codexGatewayProviderExecutorHTTPUpstreamStub{
		doFn: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
			require.Equal(t, "https://api.fallback.example/v1/responses", req.URL.String())
			require.Equal(t, "Bearer sk-fallback", req.Header.Get("Authorization"))
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{
					"id":"resp_hosted_search",
					"object":"response",
					"status":"completed",
					"output":[
						{
							"id":"msg_1",
							"type":"message",
							"role":"assistant",
							"content":[
								{"type":"output_text","text":"fallback model search worked"}
							]
						}
					]
				}`)),
			}, nil
		},
	}

	out, err := executor.executeOpenAIHostedWebSearch(context.Background(), CodexGatewayProviderRequest{
		SessionKey: "user_session",
		Model: CodexGatewayModel{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		},
	}, "latest news")
	require.NoError(t, err)
	require.Contains(t, out, `"summary":"fallback model search worked"`)
	require.Equal(t, 2, selectionCalls)
}

func TestCodexGatewayProviderExecutor_OpenAIHostedWebSearchRuntimeGuardLocalBlockNotCapturedAsUpstream(t *testing.T) {
	baseDir := t.TempDir()
	capture := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                  true,
		BaseDir:                  baseDir,
		HashKeyFile:              filepath.Join(baseDir, ".key"),
		CaptureSuccessSampleRate: 1,
	})
	defer capture.Close()
	trace := capture.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{TraceID: "hosted_search_runtime_guard_local_block", Provider: "openai", Model: "gpt-5.4-mini"})
	require.NotNil(t, trace)

	oldPersona := &Account{
		ID:          5151,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-acc",
		},
		Extra: map[string]any{
			"openai_gateway_canonical_version":    "2.1.146",
			"openai_gateway_canonical_user_agent": "codex_cli_rs/2.1.146",
			"openai_responses_write_capable":      true,
		},
	}
	selectionCalls := 0
	executor := newCodexGatewayProviderExecutorForTest()
	executor.accountSelector = &codexGatewayProviderExecutorSelectorStub{
		selectFn: func(_ context.Context, _ *int64, _ string, requestedModel string, excludedIDs map[int64]struct{}) (*Account, error) {
			selectionCalls++
			if selectionCalls == 1 {
				require.Equal(t, "gpt-5.4-mini", requestedModel)
				require.Empty(t, excludedIDs)
				return oldPersona, nil
			}
			return nil, ErrNoAvailableAccounts
		},
	}
	executor.openaiGateway = &OpenAIGatewayService{
		cfg:          &config.Config{},
		httpUpstream: &codexGatewayProviderExecutorHTTPUpstreamStub{},
	}

	_, err := executor.executeOpenAIHostedWebSearch(context.Background(), CodexGatewayProviderRequest{
		SessionKey:   "user_session",
		CaptureTrace: trace,
		Model: CodexGatewayModel{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		},
	}, "latest news")

	require.Error(t, err)
	require.GreaterOrEqual(t, selectionCalls, 2)
	capture.FinishTrace(trace, CodexGatewayCaptureFinishSummary{Status: "blocked"})
	require.NoError(t, capture.Close())

	traceDir := filepath.Join(baseDir, time.Now().Format("2006-01-02"), "hosted_search_runtime_guard_local_block")
	for _, name := range []string{"upstream_request.shape.json", "upstream_requests.events.jsonl", "upstream_response.shape.json"} {
		_, statErr := os.Stat(filepath.Join(traceDir, name))
		require.True(t, os.IsNotExist(statErr), "%s must not be written for local runtime guard blocks", name)
	}
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
	agnesReq := base
	agnesReq.Model.Provider = "agnes"
	agnesReq.Model.Slug = "agnes-2.0-flash"
	agnesReq.Model.UpstreamModel = "agnes-2.0-flash"

	require.Equal(t, "session_hash:codex_gateway:deepseek:deepseek-v4-pro:isolation_hash", pro)
	require.Equal(t, "session_hash:codex_gateway:deepseek:deepseek-v4-flash:isolation_hash", flash)
	require.Equal(t, "session_hash:codex_gateway:deepseek:deepseek-v4-pro:other_isolation", otherIsolation)
	require.Equal(t, "session_hash:codex_gateway:deepseek:deepseek-v4-pro:isolation_hash", slugFallback)
	require.Equal(t, "session_hash:codex_gateway:agnes:agnes-2.0-flash:isolation_hash", codexGatewayProviderSelectionSessionKey(agnesReq))
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

func TestCodexGatewayProviderExecutor_AppliesAnthropicEffectiveModelBeforeProviderRuntime(t *testing.T) {
	account := &Account{ID: 10, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
	executor := newCodexGatewayProviderExecutorForTest()
	executor.stateSource = &codexGatewayRegistryStateSourceStub{
		state: &CodexGatewayRegistryState{
			ProviderGroups: map[CodexGatewayProvider]CodexGatewayProviderRuntime{
				CodexGatewayProviderAnthropic: {Provider: CodexGatewayProviderAnthropic, GroupID: 404, Healthy: true},
			},
		},
	}
	executor.accountSelector = &codexGatewayProviderExecutorSelectorStub{
		selectFn: func(_ context.Context, groupID *int64, _ string, requestedModel string, _ map[int64]struct{}) (*Account, error) {
			require.NotNil(t, groupID)
			require.Equal(t, int64(404), *groupID)
			require.Equal(t, "claude-opus-4-8-thinking", requestedModel)
			return account, nil
		},
	}
	executor.anthropic = &codexGatewayProviderAdapterStub{
		completeFn: func(_ context.Context, account *Account, req CodexGatewayProviderRequest) (CodexGatewayDeepSeekAdapterResult, error) {
			require.Equal(t, int64(10), account.ID)
			require.Equal(t, "claude-opus-4-8-thinking", req.Model.UpstreamModel)
			return CodexGatewayDeepSeekAdapterResult{
				ServiceResponse: CodexGatewayServiceResponse{StatusCode: http.StatusOK},
				ProviderResult:  CodexGatewayProviderResult{UpstreamRequestID: "req_claude", UpstreamModel: req.Model.UpstreamModel},
			}, nil
		},
	}
	executor.usageRecorder = &codexGatewayUsageRecorderStub{}

	resp, err := executor.Complete(context.Background(), CodexGatewayProviderRequest{
		Request: CodexGatewayResponsesRequest{APIKey: validCodexGatewayAPIKeyForTest()},
		Model: CodexGatewayModel{
			Slug:                  "claude-opus-4-8",
			Provider:              "anthropic",
			ProviderVariant:       "anthropic_direct",
			UpstreamModel:         "claude-opus-4-8",
			UpstreamBaseModel:     "claude-opus-4-8",
			UpstreamThinkingModel: "claude-opus-4-8-thinking",
		},
		Parsed:       CodexGatewayResponsesCreateRequest{Model: "claude-opus-4-8", Reasoning: json.RawMessage(`{"effort":"xhigh"}`)},
		SessionKey:   "sess_hash",
		IsolationKey: "iso_hash",
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestCodexGatewayProviderExecutor_SelectsAnthropicThinkingUpstreamForXHigh(t *testing.T) {
	account := &Account{ID: 10, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
	executor := newCodexGatewayProviderExecutorForTest()
	executor.accountSelector = &codexGatewayProviderExecutorSelectorStub{
		selectFn: func(_ context.Context, groupID *int64, _ string, requestedModel string, _ map[int64]struct{}) (*Account, error) {
			require.NotNil(t, groupID)
			require.Equal(t, int64(303), *groupID)
			require.Equal(t, "claude-opus-4-8-thinking", requestedModel)
			return account, nil
		},
	}
	executor.anthropic = &codexGatewayProviderAdapterStub{
		completeFn: func(_ context.Context, account *Account, req CodexGatewayProviderRequest) (CodexGatewayDeepSeekAdapterResult, error) {
			require.Equal(t, int64(10), account.ID)
			require.Equal(t, "claude-opus-4-8-thinking", req.Model.UpstreamModel)
			return CodexGatewayDeepSeekAdapterResult{
				ServiceResponse: CodexGatewayServiceResponse{StatusCode: http.StatusOK},
				ProviderResult:  CodexGatewayProviderResult{UpstreamRequestID: "req_claude", UpstreamModel: "claude-opus-4-8-thinking"},
			}, nil
		},
	}
	executor.usageRecorder = &codexGatewayUsageRecorderStub{}

	resp, err := executor.Complete(context.Background(), CodexGatewayProviderRequest{
		Request: CodexGatewayResponsesRequest{APIKey: validCodexGatewayAPIKeyForTest()},
		Model: CodexGatewayModel{
			Slug:                  "claude-opus-4-8",
			Provider:              "anthropic",
			ProviderVariant:       "anthropic_direct",
			UpstreamModel:         "claude-opus-4-8",
			UpstreamBaseModel:     "claude-opus-4-8",
			UpstreamThinkingModel: "claude-opus-4-8-thinking",
		},
		Parsed:       CodexGatewayResponsesCreateRequest{Model: "claude-opus-4-8", Reasoning: json.RawMessage(`{"effort":"xhigh"}`)},
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
