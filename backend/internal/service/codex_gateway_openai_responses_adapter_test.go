package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type codexGatewayAdapterHTTPUpstreamStub struct {
	lastRequest *http.Request
	lastBody    []byte
	response    *http.Response
	err         error
}

func (s *codexGatewayAdapterHTTPUpstreamStub) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	s.lastRequest = req.Clone(req.Context())
	if req != nil && req.Body != nil {
		body, readErr := io.ReadAll(req.Body)
		if readErr != nil {
			return nil, readErr
		}
		s.lastBody = body
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.response, nil
}

func (s *codexGatewayAdapterHTTPUpstreamStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, accountConcurrency)
}

func newCodexGatewayNativeResponsesAdapterForTest(resp *http.Response, upstreamErr error) (*codexGatewayOpenAIResponsesAdapter, *codexGatewayAdapterHTTPUpstreamStub) {
	upstream := &codexGatewayAdapterHTTPUpstreamStub{response: resp, err: upstreamErr}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	gateway := &OpenAIGatewayService{
		cfg:          cfg,
		httpUpstream: upstream,
	}
	return &codexGatewayOpenAIResponsesAdapter{gateway: gateway}, upstream
}

func newCodexGatewayOpenAIAccountForTest(baseURL string) *Account {
	return &Account{
		ID:          101,
		Name:        "openai-apikey",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"api_key":  "sk-upstream",
			"base_url": baseURL,
		},
	}
}

func TestCodexGatewayOpenAIResponsesAdapter_CompletePreservesNativeRequestAndMapsUsage(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","prompt_cache_key":"pk_123","reasoning":{"effort":"high"},"text":{"verbosity":"high"},"tools":[{"type":"function","name":"weather"}],"tool_choice":{"type":"auto"},"parallel_tool_calls":true,"include":["usage"],"client_metadata":{"trace":"abc"},"input":"hello"}`)
	respBody := `{"id":"resp_123","object":"response","model":"gpt-5.5","status":"completed","usage":{"input_tokens":11,"output_tokens":7,"input_tokens_details":{"cached_tokens":3}}}`
	adapter, upstream := newCodexGatewayNativeResponsesAdapterForTest(&http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":         []string{"application/json"},
			"X-Request-Id":         []string{"req_789"},
			"X-Codex-Turn-State":   []string{"turn_state"},
			"Set-Cookie":           []string{"drop=true"},
			"X-Unrelated-Header":   []string{"blocked"},
			"X-Reasoning-Included": []string{"true"},
		},
		Body: io.NopCloser(strings.NewReader(respBody)),
	}, nil)
	captureBaseDir := t.TempDir()
	capture := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                  true,
		BaseDir:                  captureBaseDir,
		HashKeyFile:              filepath.Join(captureBaseDir, ".key"),
		CaptureSuccessSampleRate: 1,
	})
	defer capture.Close()
	trace := capture.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{TraceID: "openai_sync"})
	require.NotNil(t, trace)

	result, err := adapter.Complete(context.Background(), newCodexGatewayOpenAIAccountForTest("http://openai.local"), CodexGatewayProviderRequest{
		Request: CodexGatewayResponsesRequest{
			Headers: http.Header{
				"Content-Type":          []string{"application/json"},
				"OpenAI-Beta":           []string{"responses=v1"},
				"Session_ID":            []string{"sess_1"},
				"Conversation_ID":       []string{"conv_1"},
				"X-Codex-Turn-State":    []string{"turn_state"},
				"X-Codex-Turn-Metadata": []string{"turn_meta"},
				"X-Unsafe-Forwarded":    []string{"blocked"},
				"X-Not-On-Allowlist":    []string{"blocked"},
				"User-Agent":            []string{"codex-cli"},
				"Accept-Language":       []string{"en-US"},
			},
			Body: body,
		},
		Model:        CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
		CaptureTrace: trace,
	})
	require.NoError(t, err)
	capture.FinishTrace(trace, CodexGatewayCaptureFinishSummary{Status: "ok"})
	require.NoError(t, capture.Close())

	require.NotNil(t, upstream.lastRequest)
	require.Equal(t, "POST", upstream.lastRequest.Method)
	require.Equal(t, "http://openai.local/v1/responses", upstream.lastRequest.URL.String())
	require.Equal(t, string(body), string(upstream.lastBody))
	require.Equal(t, "Bearer sk-upstream", upstream.lastRequest.Header.Get("Authorization"))
	require.Equal(t, "application/json", upstream.lastRequest.Header.Get("Accept"))
	require.Equal(t, "responses=v1", upstream.lastRequest.Header.Get("OpenAI-Beta"))
	require.Equal(t, "sess_1", upstream.lastRequest.Header.Get("Session_ID"))
	require.Equal(t, "conv_1", upstream.lastRequest.Header.Get("Conversation_ID"))
	require.Equal(t, "turn_state", upstream.lastRequest.Header.Get("X-Codex-Turn-State"))
	require.Equal(t, "turn_meta", upstream.lastRequest.Header.Get("X-Codex-Turn-Metadata"))
	require.Len(t, upstream.lastRequest.Header.Values("Content-Type"), 1)
	require.Len(t, upstream.lastRequest.Header.Values("Accept"), 1)
	require.Empty(t, upstream.lastRequest.Header.Get("X-Unsafe-Forwarded"))
	require.Empty(t, upstream.lastRequest.Header.Get("X-Not-On-Allowlist"))

	require.Equal(t, http.StatusOK, result.ServiceResponse.StatusCode)
	require.Equal(t, "application/json", result.ServiceResponse.Headers.Get("Content-Type"))
	require.Equal(t, "req_789", result.ServiceResponse.Headers.Get("X-Request-Id"))
	require.Equal(t, "turn_state", result.ServiceResponse.Headers.Get("X-Codex-Turn-State"))
	require.Equal(t, "true", result.ServiceResponse.Headers.Get("X-Reasoning-Included"))
	require.Empty(t, result.ServiceResponse.Headers.Get("Set-Cookie"))
	require.Equal(t, "resp_123", result.ProviderResult.ResponseID)
	require.Equal(t, "req_789", result.ProviderResult.UpstreamRequestID)
	require.Equal(t, "gpt-5.5", result.ProviderResult.UpstreamModel)
	require.Equal(t, 11, result.ProviderResult.Usage.InputTokens)
	require.Equal(t, 7, result.ProviderResult.Usage.OutputTokens)
	require.Equal(t, 18, result.ProviderResult.Usage.TotalTokens)
	require.Equal(t, 3, result.ProviderResult.Usage.CacheReadInputTokens)
	upstreamResponseShape, err := os.ReadFile(filepath.Join(captureBaseDir, time.Now().Format("2006-01-02"), "openai_sync", "upstream_response.shape.json"))
	require.NoError(t, err)
	require.Contains(t, string(upstreamResponseShape), `"bytes": `+fmt.Sprint(len(respBody)))
	require.NotContains(t, string(upstreamResponseShape), "pk_123")
}

func TestCodexGatewayOpenAIResponsesAdapter_CompleteSupportsUpstreamAccountType(t *testing.T) {
	adapter, _ := newCodexGatewayNativeResponsesAdapterForTest(&http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"resp_upstream","object":"response","model":"gpt-5.5","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)),
	}, nil)
	account := &Account{
		ID:          303,
		Name:        "upstream-openai",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeUpstream,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"api_key":  "sk-upstream",
			"base_url": "http://openai.local",
		},
	}

	result, err := adapter.Complete(context.Background(), account, CodexGatewayProviderRequest{
		Request: CodexGatewayResponsesRequest{
			Body: []byte(`{"model":"gpt-5.5","stream":false}`),
		},
		Model: CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
	})
	require.NoError(t, err)
	require.Equal(t, "resp_upstream", result.ProviderResult.ResponseID)
}

func TestCodexGatewayOpenAIResponsesAdapter_PreservesStructuredFunctionOutputArrayBeforeUpstream(t *testing.T) {
	adapter, upstream := newCodexGatewayNativeResponsesAdapterForTest(&http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"resp_structured","object":"response","model":"gpt-5.5","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)),
	}, nil)

	body := []byte(`{
		"model":"gpt-5.5",
		"input":[
			{"type":"function_call_output","call_id":"call_img","output":[
				{"type":"input_text","text":"screenshot follows"},
				{"type":"input_image","image_url":"data:image/png;base64,QUJDRA==","detail":"high"}
			]}
		],
		"stream":false
	}`)

	_, err := adapter.Complete(context.Background(), newCodexGatewayOpenAIAccountForTest("http://openai.local"), CodexGatewayProviderRequest{
		Request: CodexGatewayResponsesRequest{Body: body},
		Model:   CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
	})
	require.NoError(t, err)
	require.Equal(t, "function_call_output", gjson.GetBytes(upstream.lastBody, "input.0.type").String())
	require.Equal(t, "input_text", gjson.GetBytes(upstream.lastBody, "input.0.output.0.type").String())
	require.Equal(t, "screenshot follows", gjson.GetBytes(upstream.lastBody, "input.0.output.0.text").String())
	require.Equal(t, "input_image", gjson.GetBytes(upstream.lastBody, "input.0.output.1.type").String())
	require.Equal(t, "data:image/png;base64,QUJDRA==", gjson.GetBytes(upstream.lastBody, "input.0.output.1.image_url").String())
	require.Equal(t, "high", gjson.GetBytes(upstream.lastBody, "input.0.output.1.detail").String())
}

func TestCodexGatewayOpenAIResponsesAdapter_StripsPlaintextReasoningHistoryBeforeUpstream(t *testing.T) {
	adapter, upstream := newCodexGatewayNativeResponsesAdapterForTest(&http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"resp_sanitized","object":"response","model":"gpt-5.4","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)),
	}, nil)

	body := []byte(`{
		"model":"gpt-5.4",
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"before"}]},
			{"type":"reasoning","summary":[],"content":[{"type":"reasoning_text","text":"provider private chain"}],"encrypted_content":null},
			{"type":"reasoning","summary":[],"content":null,"encrypted_content":"opaque-openai-reasoning"},
			{"type":"function_call","call_id":"call_1","name":"exec_command","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_1","output":"ok"}
		],
		"include":["reasoning.encrypted_content"],
		"stream":true
	}`)

	_, err := adapter.Stream(context.Background(), newCodexGatewayOpenAIAccountForTest("http://openai.local"), CodexGatewayProviderRequest{
		Request: CodexGatewayResponsesRequest{
			Body:         body,
			StreamWriter: &bytes.Buffer{},
		},
		Model: CodexGatewayModel{Slug: "gpt-5.4", Provider: "openai", UpstreamModel: "gpt-5.4"},
	})
	require.Error(t, err)
	require.NotNil(t, upstream.lastRequest)
	require.NotContains(t, string(upstream.lastBody), "provider private chain")
	require.Equal(t, int64(4), gjson.GetBytes(upstream.lastBody, "input.#").Int())
	require.Equal(t, "message", gjson.GetBytes(upstream.lastBody, "input.0.type").String())
	require.Equal(t, "reasoning", gjson.GetBytes(upstream.lastBody, "input.1.type").String())
	require.Equal(t, "opaque-openai-reasoning", gjson.GetBytes(upstream.lastBody, "input.1.encrypted_content").String())
	require.Equal(t, "function_call", gjson.GetBytes(upstream.lastBody, "input.2.type").String())
	require.Equal(t, "function_call_output", gjson.GetBytes(upstream.lastBody, "input.3.type").String())
}

func TestCodexGatewayOpenAIResponsesAdapter_CompleteFailoverAppliesRateLimitSideEffects(t *testing.T) {
	account := newCodexGatewayOpenAIAccountForTest("http://openai.local")
	repo := &openAIGatewayCoreRepoStub{
		openAIGatewayCoreAccountRepoStubBase: openAIGatewayCoreAccountRepoStubBase{
			accountsByID: map[int64]*Account{account.ID: account},
		},
	}
	upstream := &codexGatewayAdapterHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header: http.Header{
			"Content-Type":                          []string{"application/json"},
			"X-Codex-Primary-Used-Percent":          []string{"100"},
			"X-Codex-Primary-Reset-After-Seconds":   []string{"7200"},
			"X-Codex-Primary-Window-Minutes":        []string{"10080"},
			"X-Codex-Secondary-Used-Percent":        []string{"3"},
			"X-Codex-Secondary-Reset-After-Seconds": []string{"1800"},
			"X-Codex-Secondary-Window-Minutes":      []string{"300"},
		},
		Body: io.NopCloser(strings.NewReader(`{"error":{"message":"usage limit reached","type":"rate_limit_exceeded"}}`)),
	}}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	gateway := &OpenAIGatewayService{
		cfg:              cfg,
		httpUpstream:     upstream,
		accountRepo:      repo,
		rateLimitService: NewRateLimitService(repo, nil, cfg, nil, nil),
	}
	adapter := &codexGatewayOpenAIResponsesAdapter{gateway: gateway}

	_, err := adapter.Complete(context.Background(), account, CodexGatewayProviderRequest{
		Request: CodexGatewayResponsesRequest{
			Body: []byte(`{"model":"gpt-5.5","stream":false}`),
		},
		Model: CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
	})
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, 1, repo.updateExtraCalls)
	require.Equal(t, 100.0, repo.lastExtra["codex_7d_used_percent"])
	require.Equal(t, 3.0, repo.lastExtra["codex_5h_used_percent"])
}

func TestCodexGatewayOpenAIResponsesAdapter_StreamFailsOverBeforeClientOutput(t *testing.T) {
	streamBody := "" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"in_progress\"}}\n\n" +
		"data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"type\":\"server_error\",\"code\":\"server_error\",\"message\":\"temporary upstream issue\"}}}\n\n"
	adapter, _ := newCodexGatewayNativeResponsesAdapterForTest(&http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(streamBody)),
	}, nil)
	var out bytes.Buffer
	var statusCode int

	_, err := adapter.Stream(context.Background(), newCodexGatewayOpenAIAccountForTest("http://openai.local"), CodexGatewayProviderRequest{
		Request: CodexGatewayResponsesRequest{
			Body:           []byte(`{"model":"gpt-5.5","stream":true}`),
			StreamWriter:   &out,
			ResponseHeader: http.Header{},
			WriteStatus:    func(code int) { statusCode = code },
		},
		Model: CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
	})
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, 0, statusCode)
	require.Empty(t, out.String())
}

func TestCodexGatewayOpenAIResponsesAdapter_StreamRequestErrorFailsOverBeforeClientOutput(t *testing.T) {
	adapter, _ := newCodexGatewayNativeResponsesAdapterForTest(nil, fmt.Errorf(`Post "https://api.5566676.xyz/v1/responses": EOF`))
	var out bytes.Buffer

	_, err := adapter.Stream(context.Background(), newCodexGatewayOpenAIAccountForTest("https://api.5566676.xyz"), CodexGatewayProviderRequest{
		Request: CodexGatewayResponsesRequest{
			Body:         []byte(`{"model":"gpt-5.5","stream":true}`),
			StreamWriter: &out,
		},
		Model: CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
	})
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.Contains(t, string(failoverErr.ResponseBody), `"upstream_error"`)
	require.NotContains(t, string(failoverErr.ResponseBody), "api.5566676.xyz")
	require.Empty(t, out.String())
}

func TestCodexGatewayOpenAIResponsesAdapter_CompleteRequestErrorFailsOver(t *testing.T) {
	adapter, _ := newCodexGatewayNativeResponsesAdapterForTest(nil, fmt.Errorf(`Post "https://api.5566676.xyz/v1/responses": EOF`))

	_, err := adapter.Complete(context.Background(), newCodexGatewayOpenAIAccountForTest("https://api.5566676.xyz"), CodexGatewayProviderRequest{
		Request: CodexGatewayResponsesRequest{
			Body: []byte(`{"model":"gpt-5.5","stream":false}`),
		},
		Model: CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
	})
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.Contains(t, string(failoverErr.ResponseBody), `"upstream_error"`)
	require.NotContains(t, string(failoverErr.ResponseBody), "api.5566676.xyz")
}

func TestCodexGatewayOpenAIResponsesAdapter_StreamFailoverAppliesRateLimitSideEffects(t *testing.T) {
	account := newCodexGatewayOpenAIAccountForTest("http://openai.local")
	repo := &openAIGatewayCoreRepoStub{
		openAIGatewayCoreAccountRepoStubBase: openAIGatewayCoreAccountRepoStubBase{
			accountsByID: map[int64]*Account{account.ID: account},
		},
	}
	upstream := &codexGatewayAdapterHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header: http.Header{
			"Content-Type":                          []string{"application/json"},
			"X-Codex-Primary-Used-Percent":          []string{"100"},
			"X-Codex-Primary-Reset-After-Seconds":   []string{"604800"},
			"X-Codex-Primary-Window-Minutes":        []string{"10080"},
			"X-Codex-Secondary-Used-Percent":        []string{"100"},
			"X-Codex-Secondary-Reset-After-Seconds": []string{"18000"},
			"X-Codex-Secondary-Window-Minutes":      []string{"300"},
		},
		Body: io.NopCloser(strings.NewReader(`{"error":{"message":"usage limit reached","type":"rate_limit_exceeded"}}`)),
	}}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	gateway := &OpenAIGatewayService{
		cfg:              cfg,
		httpUpstream:     upstream,
		accountRepo:      repo,
		rateLimitService: NewRateLimitService(repo, nil, cfg, nil, nil),
	}
	adapter := &codexGatewayOpenAIResponsesAdapter{gateway: gateway}
	var out bytes.Buffer

	_, err := adapter.Stream(context.Background(), account, CodexGatewayProviderRequest{
		Request: CodexGatewayResponsesRequest{
			Body:         []byte(`{"model":"gpt-5.5","stream":true}`),
			StreamWriter: &out,
		},
		Model: CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
	})
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, 1, repo.updateExtraCalls)
	require.Equal(t, 100.0, repo.lastExtra["codex_7d_used_percent"])
	require.Equal(t, 100.0, repo.lastExtra["codex_5h_used_percent"])
	require.Empty(t, out.String())
}

func TestCodexGatewayOpenAIResponsesAdapter_StreamDoesNotFailoverAfterClientOutputStarts(t *testing.T) {
	streamBody := "" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"in_progress\"}}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"response_id\":\"resp_1\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\"hello\"}\n\n" +
		"data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"type\":\"server_error\",\"code\":\"server_error\",\"message\":\"temporary upstream issue\"}}}\n\n"
	adapter, _ := newCodexGatewayNativeResponsesAdapterForTest(&http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(streamBody)),
	}, nil)
	var out bytes.Buffer

	result, err := adapter.Stream(context.Background(), newCodexGatewayOpenAIAccountForTest("http://openai.local"), CodexGatewayProviderRequest{
		Request: CodexGatewayResponsesRequest{
			Body:         []byte(`{"model":"gpt-5.5","stream":true}`),
			StreamWriter: &out,
		},
		Model: CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
	})
	require.NoError(t, err)
	require.Empty(t, result.ResponseID)
	require.False(t, strings.Contains(out.String(), "[DONE]"))
	require.Contains(t, out.String(), `"type":"response.output_text.delta"`)
	require.Contains(t, out.String(), `"type":"response.failed"`)
}

func TestCodexGatewayOpenAIResponsesAdapter_StreamDrainsUpstreamAfterClientDisconnect(t *testing.T) {
	streamBody := "" +
		`data: {"type":"response.created","response":{"id":"resp_1","object":"response","status":"in_progress"}}` + "\n\n" +
		`data: {"type":"response.output_text.delta","response_id":"resp_1","item_id":"msg_1","output_index":0,"content_index":0,"delta":"hello"}` + "\n\n" +
		`data: {"type":"response.function_call_arguments.done","item_id":"call_1","output_index":1,"arguments":"{}"}` + "\n\n" +
		`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","model":"gpt-5.5","status":"completed","output":[],"usage":{"input_tokens":9,"output_tokens":3,"total_tokens":12}}}` + "\n\n"
	adapter, _ := newCodexGatewayNativeResponsesAdapterForTest(&http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(streamBody)),
	}, nil)
	writer := &codexGatewayOpenAIFailingStreamWriter{failAfterWrites: 1}

	result, err := adapter.Stream(context.Background(), newCodexGatewayOpenAIAccountForTest("http://openai.local"), CodexGatewayProviderRequest{
		Request: CodexGatewayResponsesRequest{
			Body:         []byte(`{"model":"gpt-5.5","stream":true}`),
			StreamWriter: writer,
		},
		Model: CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
	})
	require.NoError(t, err)
	require.Equal(t, "resp_1", result.ResponseID)
	require.Equal(t, 9, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)
	require.Equal(t, 2, writer.writeCalls, "first visible frame writes, second detects disconnect; later upstream events are drained without writing")
	require.Contains(t, writer.String(), `response.created`)
	require.NotContains(t, writer.String(), `response.output_text.delta`)
	require.NotContains(t, writer.String(), `response.completed`)
}

type codexGatewayOpenAIFailingStreamWriter struct {
	buf             bytes.Buffer
	writeCalls      int
	failAfterWrites int
}

func (w *codexGatewayOpenAIFailingStreamWriter) Write(p []byte) (int, error) {
	w.writeCalls++
	if w.failAfterWrites >= 0 && w.writeCalls > w.failAfterWrites {
		return 0, fmt.Errorf("client disconnected")
	}
	return w.buf.Write(p)
}

func (w *codexGatewayOpenAIFailingStreamWriter) String() string {
	return w.buf.String()
}

func TestCodexGatewayOpenAIResponsesAdapter_StreamEmitsFailureWhenTerminalMissingAfterClientOutputStarts(t *testing.T) {
	streamBody := "" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"in_progress\"}}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"response_id\":\"resp_1\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\"hello\"}\n\n"
	adapter, _ := newCodexGatewayNativeResponsesAdapterForTest(&http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(streamBody)),
	}, nil)
	var out bytes.Buffer

	result, err := adapter.Stream(context.Background(), newCodexGatewayOpenAIAccountForTest("http://openai.local"), CodexGatewayProviderRequest{
		Request: CodexGatewayResponsesRequest{
			Body:         []byte(`{"model":"gpt-5.5","stream":true}`),
			StreamWriter: &out,
		},
		Model: CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
	})
	require.NoError(t, err)
	require.Equal(t, "resp_1", result.ResponseID)
	require.Equal(t, "failed", result.Response.Status)
	require.Contains(t, out.String(), `"type":"response.output_text.delta"`)
	require.Contains(t, out.String(), "event: response.failed")
	require.Contains(t, out.String(), `"type":"response.failed"`)
	require.Contains(t, out.String(), `"sequence_number":0`)
	require.Contains(t, out.String(), `missing terminal event`)
	require.NotContains(t, out.String(), `[DONE]`)
}

func TestCodexGatewayOpenAIResponsesAdapter_StreamFlushesPreOutputTerminalFailure(t *testing.T) {
	streamBody := "" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"in_progress\"}}\n\n" +
		"data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"failed\",\"output\":[],\"error\":{\"type\":\"invalid_request_error\",\"code\":\"invalid_request\",\"message\":\"hard reject\"},\"usage\":{\"input_tokens\":3,\"output_tokens\":1,\"total_tokens\":4}}}\n\n"
	adapter, _ := newCodexGatewayNativeResponsesAdapterForTest(&http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(streamBody)),
	}, nil)
	var out bytes.Buffer

	result, err := adapter.Stream(context.Background(), newCodexGatewayOpenAIAccountForTest("http://openai.local"), CodexGatewayProviderRequest{
		Request: CodexGatewayResponsesRequest{
			Body:         []byte(`{"model":"gpt-5.5","stream":true}`),
			StreamWriter: &out,
		},
		Model: CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
	})
	require.NoError(t, err)
	require.Equal(t, "resp_1", result.ResponseID)
	require.Contains(t, out.String(), `"type":"response.failed"`)
	require.Contains(t, out.String(), `"message":"hard reject"`)
	require.Equal(t, 3, result.Usage.InputTokens)
	require.Equal(t, 1, result.Usage.OutputTokens)
	require.Equal(t, 4, result.Usage.TotalTokens)
}

func TestCodexGatewayOpenAIResponsesAdapter_CompleteAcceptsSSETerminalPayload(t *testing.T) {
	streamBody := "" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"in_progress\"}}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"model\":\"gpt-5.5\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":2,\"output_tokens\":1,\"total_tokens\":3}}}\n\n" +
		"data: [DONE]\n\n"
	adapter, _ := newCodexGatewayNativeResponsesAdapterForTest(&http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(streamBody)),
	}, nil)

	result, err := adapter.Complete(context.Background(), newCodexGatewayOpenAIAccountForTest("http://openai.local"), CodexGatewayProviderRequest{
		Request: CodexGatewayResponsesRequest{
			Body: []byte(`{"model":"gpt-5.5","stream":false}`),
		},
		Model: CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
	})
	require.NoError(t, err)
	require.Equal(t, "resp_1", result.ProviderResult.ResponseID)
	require.Equal(t, "completed", result.ProviderResult.Response.Status)
	require.Equal(t, "application/json; charset=utf-8", result.ServiceResponse.Headers.Get("Content-Type"))
	require.Equal(t, int64(0), gjson.GetBytes(result.ServiceResponse.Body, "output.#").Int())
}

func TestCodexGatewayOpenAIResponsesAdapter_CompleteReconstructsOutputFromSSE(t *testing.T) {
	streamBody := "" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"response_id\":\"resp_1\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\"hello\"}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"response_id\":\"resp_1\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\" world\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"model\":\"gpt-5.5\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":2,\"output_tokens\":2,\"total_tokens\":4}}}\n\n" +
		"data: [DONE]\n\n"
	adapter, _ := newCodexGatewayNativeResponsesAdapterForTest(&http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(streamBody)),
	}, nil)

	result, err := adapter.Complete(context.Background(), newCodexGatewayOpenAIAccountForTest("http://openai.local"), CodexGatewayProviderRequest{
		Request: CodexGatewayResponsesRequest{
			Body: []byte(`{"model":"gpt-5.5","stream":false}`),
		},
		Model: CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
	})
	require.NoError(t, err)
	require.Equal(t, "hello world", gjson.GetBytes(result.ServiceResponse.Body, "output.0.content.0.text").String())
}

func TestCodexGatewayOpenAIResponsesAdapter_CompleteAcceptsHeaderlessSSEOAuthPayload(t *testing.T) {
	streamBody := "" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"model\":\"gpt-5.5\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":2,\"output_tokens\":1,\"total_tokens\":3}}}\n\n" +
		"data: [DONE]\n\n"
	adapter, _ := newCodexGatewayNativeResponsesAdapterForTest(&http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(streamBody)),
	}, nil)
	account := newCodexGatewayOpenAIAccountForTest("http://openai.local")
	account.Type = AccountTypeOAuth
	account.Credentials = map[string]any{
		"access_token": "oauth-token",
		"base_url":     "http://openai.local",
	}

	result, err := adapter.Complete(context.Background(), account, CodexGatewayProviderRequest{
		Request: CodexGatewayResponsesRequest{
			Body: []byte(`{"model":"gpt-5.5","stream":false}`),
		},
		Model: CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
	})
	require.NoError(t, err)
	require.Equal(t, "resp_1", result.ProviderResult.ResponseID)
	require.Equal(t, "application/json; charset=utf-8", result.ServiceResponse.Headers.Get("Content-Type"))
}

func TestCodexGatewayOpenAIResponsesAdapter_CompleteDoesNotMisclassifyHeaderlessJSONAsSSE(t *testing.T) {
	body := `{"id":"resp_json","object":"response","model":"gpt-5.5","status":"completed","output":[{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"literal data: value and event: marker"}]}],"usage":{"input_tokens":2,"output_tokens":5,"total_tokens":7}}`
	adapter, _ := newCodexGatewayNativeResponsesAdapterForTest(&http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil)
	account := newCodexGatewayOpenAIAccountForTest("http://openai.local")
	account.Type = AccountTypeOAuth
	account.Credentials = map[string]any{
		"access_token": "oauth-token",
		"base_url":     "http://openai.local",
	}

	result, err := adapter.Complete(context.Background(), account, CodexGatewayProviderRequest{
		Request: CodexGatewayResponsesRequest{
			Body: []byte(`{"model":"gpt-5.5","stream":false}`),
		},
		Model: CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
	})
	require.NoError(t, err)
	require.Equal(t, "resp_json", result.ProviderResult.ResponseID)
	require.Equal(t, "literal data: value and event: marker", gjson.GetBytes(result.ServiceResponse.Body, "output.0.content.0.text").String())
}

func TestCodexGatewayOpenAIResponsesAdapter_CompleteAcceptsIncompleteSSETerminalPayload(t *testing.T) {
	streamBody := "" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_incomplete\",\"object\":\"response\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"data: {\"type\":\"response.incomplete\",\"response\":{\"id\":\"resp_incomplete\",\"object\":\"response\",\"model\":\"gpt-5.5\",\"status\":\"incomplete\",\"output\":[],\"incomplete_details\":{\"reason\":\"max_output_tokens\"},\"usage\":{\"input_tokens\":2,\"output_tokens\":1,\"total_tokens\":3}}}\n\n" +
		"data: [DONE]\n\n"
	adapter, _ := newCodexGatewayNativeResponsesAdapterForTest(&http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(streamBody)),
	}, nil)

	result, err := adapter.Complete(context.Background(), newCodexGatewayOpenAIAccountForTest("http://openai.local"), CodexGatewayProviderRequest{
		Request: CodexGatewayResponsesRequest{
			Body: []byte(`{"model":"gpt-5.5","stream":false}`),
		},
		Model: CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
	})
	require.NoError(t, err)
	require.Equal(t, "resp_incomplete", result.ProviderResult.ResponseID)
	require.Equal(t, "incomplete", result.ProviderResult.Response.Status)
	require.Equal(t, "max_output_tokens", gjson.GetBytes(result.ServiceResponse.Body, "incomplete_details.reason").String())
}

func TestCodexGatewayOpenAIResponsesAdapter_CompleteAcceptsCRLFMultilineSSE(t *testing.T) {
	streamBody := "" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_crlf\",\"object\":\"response\",\"status\":\"in_progress\",\"output\":[]}}\r\n\r\n" +
		"data: {\"type\":\"response.completed\",\"response\":\r\n" +
		"data: {\"id\":\"resp_crlf\",\"object\":\"response\",\"model\":\"gpt-5.5\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":2,\"output_tokens\":1,\"total_tokens\":3}}}\r\n\r\n" +
		"data: [DONE]\r\n\r\n"
	adapter, _ := newCodexGatewayNativeResponsesAdapterForTest(&http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(streamBody)),
	}, nil)

	result, err := adapter.Complete(context.Background(), newCodexGatewayOpenAIAccountForTest("http://openai.local"), CodexGatewayProviderRequest{
		Request: CodexGatewayResponsesRequest{
			Body: []byte(`{"model":"gpt-5.5","stream":false}`),
		},
		Model: CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
	})
	require.NoError(t, err)
	require.Equal(t, "resp_crlf", result.ProviderResult.ResponseID)
	require.Equal(t, "application/json; charset=utf-8", result.ServiceResponse.Headers.Get("Content-Type"))
}
