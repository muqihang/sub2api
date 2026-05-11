package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

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
			"Content-Type":        []string{"application/json"},
			"X-Request-Id":        []string{"req_789"},
			"X-Codex-Turn-State":  []string{"turn_state"},
			"Set-Cookie":          []string{"drop=true"},
			"X-Unrelated-Header":  []string{"blocked"},
			"X-Reasoning-Included": []string{"true"},
		},
		Body: io.NopCloser(strings.NewReader(respBody)),
	}, nil)

	result, err := adapter.Complete(context.Background(), newCodexGatewayOpenAIAccountForTest("http://openai.local"), CodexGatewayProviderRequest{
		Request: CodexGatewayResponsesRequest{
			Headers: http.Header{
				"Content-Type":           []string{"application/json"},
				"OpenAI-Beta":            []string{"responses=v1"},
				"Session_ID":             []string{"sess_1"},
				"Conversation_ID":        []string{"conv_1"},
				"X-Codex-Turn-State":     []string{"turn_state"},
				"X-Codex-Turn-Metadata":  []string{"turn_meta"},
				"X-Unsafe-Forwarded":     []string{"blocked"},
				"X-Not-On-Allowlist":     []string{"blocked"},
				"User-Agent":             []string{"codex-cli"},
				"Accept-Language":        []string{"en-US"},
			},
			Body: body,
		},
		Model: CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai", UpstreamModel: "gpt-5.5"},
	})
	require.NoError(t, err)

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
	require.Equal(t, http.StatusOK, statusCode)
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

func TestCodexGatewayOpenAIResponsesAdapter_StreamFlushesPreOutputTerminalFailure(t *testing.T) {
	streamBody := "" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"in_progress\"}}\n\n" +
		"data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"failed\",\"output\":[],\"error\":{\"type\":\"invalid_request_error\",\"code\":\"invalid_request\",\"message\":\"hard reject\"}}}\n\n"
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
