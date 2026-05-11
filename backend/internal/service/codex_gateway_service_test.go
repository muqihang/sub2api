package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type codexGatewayExecutorStub struct {
	completeFn func(ctx context.Context, req CodexGatewayProviderRequest) (*CodexGatewayServiceResponse, error)
	streamFn   func(ctx context.Context, req CodexGatewayProviderRequest) error
}

func (s *codexGatewayExecutorStub) Complete(ctx context.Context, req CodexGatewayProviderRequest) (*CodexGatewayServiceResponse, error) {
	return s.completeFn(ctx, req)
}

func (s *codexGatewayExecutorStub) Stream(ctx context.Context, req CodexGatewayProviderRequest) error {
	return s.streamFn(ctx, req)
}

func TestCodexGatewayService_ResponsesDispatchesSynchronousRequest(t *testing.T) {
	var captured CodexGatewayProviderRequest
	registry := NewDefaultCodexGatewayModelRegistry()
	executor := &codexGatewayExecutorStub{
		completeFn: func(_ context.Context, req CodexGatewayProviderRequest) (*CodexGatewayServiceResponse, error) {
			captured = req
			return &CodexGatewayServiceResponse{
				StatusCode: http.StatusCreated,
				Headers:    http.Header{"Content-Type": []string{"application/json"}},
				Body:       []byte(`{"id":"resp_123"}`),
			}, nil
		},
	}
	svc := NewCodexGatewayService(registry, executor)
	apiKey := validCodexGatewayAPIKeyForTest()
	body := []byte(`{"model":"gpt-5.5","prompt_cache_key":"pk_123","input":"hello"}`)
	headers := http.Header{"Session_ID": []string{"sess_1"}}

	resp, err := svc.Responses(context.Background(), CodexGatewayResponsesRequest{
		APIKey:  apiKey,
		Headers: headers,
		Body:    body,
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	require.Equal(t, "gpt-5.5", captured.Model.Slug)
	require.Equal(t, "openai", captured.Model.Provider)
	require.Equal(t, "gpt-5.5", captured.Parsed.Model)
	require.Equal(t, codexGatewaySessionKey(context.Background(), headers, body), captured.SessionKey)
	require.Equal(t, codexGatewayIsolationKey(context.Background(), apiKey), captured.IsolationKey)
}

func TestCodexGatewayService_ResponsesRejectsScopeMismatch(t *testing.T) {
	registry := NewDefaultCodexGatewayModelRegistry()
	svc := NewCodexGatewayService(registry, &codexGatewayExecutorStub{
		completeFn: func(_ context.Context, _ CodexGatewayProviderRequest) (*CodexGatewayServiceResponse, error) {
			t.Fatal("executor should not be called")
			return nil, nil
		},
	})
	apiKey := validCodexGatewayAPIKeyForTest()
	otherProduct := AugmentClientProductZhumeng
	apiKey.RestrictedClientProduct = &otherProduct

	resp, err := svc.Responses(context.Background(), CodexGatewayResponsesRequest{
		APIKey: apiKey,
		Body:   []byte(`{"model":"gpt-5.5"}`),
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	require.Contains(t, string(resp.Body), `"type":"authentication_error"`)
}

func TestCodexGatewayService_ResponsesDeepSeekPreviousResponseIDReturns400(t *testing.T) {
	registry := &CodexGatewayModelRegistry{
		models: []CodexGatewayModel{{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
			Visibility:    "visible",
			SupportedInAPI: true,
		}},
		index: map[string]CodexGatewayModel{
			"deepseek-v4-pro": {
				Slug:          "deepseek-v4-pro",
				Provider:      "deepseek",
				UpstreamModel: "deepseek-v4-pro",
				Visibility:    "visible",
				SupportedInAPI: true,
			},
		},
	}
	svc := NewCodexGatewayService(registry, &codexGatewayExecutorStub{
		completeFn: func(_ context.Context, _ CodexGatewayProviderRequest) (*CodexGatewayServiceResponse, error) {
			t.Fatal("executor should not be called")
			return nil, nil
		},
	})

	resp, err := svc.Responses(context.Background(), CodexGatewayResponsesRequest{
		APIKey: validCodexGatewayAPIKeyForTest(),
		Body:   []byte(`{"model":"deepseek-v4-pro","previous_response_id":"resp_prev"}`),
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Equal(t, "previous_response_id is not supported on the HTTP gateway path for DeepSeek models", gjson.GetBytes(resp.Body, "error.message").String())
}

func TestCodexGatewayService_ResponsesRejectsHiddenModel(t *testing.T) {
	registry := NewDefaultCodexGatewayModelRegistry()
	svc := NewCodexGatewayService(registry, &codexGatewayExecutorStub{
		completeFn: func(_ context.Context, _ CodexGatewayProviderRequest) (*CodexGatewayServiceResponse, error) {
			t.Fatal("executor should not be called")
			return nil, nil
		},
	})

	resp, err := svc.Responses(context.Background(), CodexGatewayResponsesRequest{
		APIKey: validCodexGatewayAPIKeyForTest(),
		Body:   []byte(`{"model":"deepseek-v4-pro"}`),
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Equal(t, "invalid_request", gjson.GetBytes(resp.Body, "error.code").String())
	require.Equal(t, `model "deepseek-v4-pro" is not supported`, gjson.GetBytes(resp.Body, "error.message").String())
}

func TestCodexGatewayService_ResponsesMapsProviderErrorToHTTP(t *testing.T) {
	registry := NewDefaultCodexGatewayModelRegistry()
	svc := NewCodexGatewayService(registry, &codexGatewayExecutorStub{
		completeFn: func(_ context.Context, _ CodexGatewayProviderRequest) (*CodexGatewayServiceResponse, error) {
			return nil, &CodexGatewayProviderUnavailableError{ModelID: "gpt-5.5", Provider: "openai", Kind: CodexGatewayProviderUnavailableNoAccounts}
		},
	})

	resp, err := svc.Responses(context.Background(), CodexGatewayResponsesRequest{
		APIKey: validCodexGatewayAPIKeyForTest(),
		Body:   []byte(`{"model":"gpt-5.5"}`),
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	require.Contains(t, string(resp.Body), `"code":"service_unavailable"`)
}

func TestCodexGatewayService_ResponsesStreamingWritesTerminalErrorEvent(t *testing.T) {
	registry := NewDefaultCodexGatewayModelRegistry()
	svc := NewCodexGatewayService(registry, &codexGatewayExecutorStub{
		streamFn: func(_ context.Context, req CodexGatewayProviderRequest) error {
			require.Equal(t, "gpt-5.5", req.Model.Slug)
			return errors.New("upstream disconnected")
		},
	})
	var out bytes.Buffer
	var statusCode int
	headers := http.Header{}

	resp, err := svc.Responses(context.Background(), CodexGatewayResponsesRequest{
		APIKey:         validCodexGatewayAPIKeyForTest(),
		Body:           []byte(`{"model":"gpt-5.5","stream":true}`),
		StreamWriter:   &out,
		ResponseHeader: headers,
		WriteStatus:    func(code int) { statusCode = code },
	})
	require.NoError(t, err)
	require.Nil(t, resp)
	require.Equal(t, http.StatusOK, statusCode)
	require.Equal(t, "text/event-stream", headers.Get("Content-Type"))
	require.Contains(t, out.String(), `"type":"response.failed"`)
	require.Contains(t, out.String(), `"message":"upstream disconnected"`)
}

func TestCodexGatewayService_ResponsesStreamingFailoverErrorKeepsSSEEnvelope(t *testing.T) {
	registry := NewDefaultCodexGatewayModelRegistry()
	svc := NewCodexGatewayService(registry, &codexGatewayExecutorStub{
		streamFn: func(_ context.Context, req CodexGatewayProviderRequest) error {
			if req.Request.ResponseHeader != nil {
				req.Request.ResponseHeader.Set("Content-Type", "application/json")
			}
			if req.Request.WriteStatus != nil {
				req.Request.WriteStatus(http.StatusTooManyRequests)
			}
			return &UpstreamFailoverError{StatusCode: http.StatusTooManyRequests}
		},
	})
	var out bytes.Buffer
	var statusCode int
	headers := http.Header{}

	resp, err := svc.Responses(context.Background(), CodexGatewayResponsesRequest{
		APIKey:         validCodexGatewayAPIKeyForTest(),
		Body:           []byte(`{"model":"gpt-5.5","stream":true}`),
		StreamWriter:   &out,
		ResponseHeader: headers,
		WriteStatus:    func(code int) { statusCode = code },
	})
	require.NoError(t, err)
	require.Nil(t, resp)
	require.Equal(t, http.StatusOK, statusCode)
	require.Equal(t, "text/event-stream", headers.Get("Content-Type"))
	require.Contains(t, out.String(), `"type":"response.failed"`)
}

func TestCodexGatewayService_ModelsReturnsVisibleCatalog(t *testing.T) {
	svc := NewCodexGatewayService(NewDefaultCodexGatewayModelRegistry(), &codexGatewayExecutorStub{
		completeFn: func(_ context.Context, _ CodexGatewayProviderRequest) (*CodexGatewayServiceResponse, error) {
			return nil, nil
		},
	})

	resp, err := svc.Models(context.Background(), CodexGatewayModelsRequest{
		APIKey: validCodexGatewayAPIKeyForTest(),
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var payload CodexGatewayModelsResponse
	require.NoError(t, json.Unmarshal(resp.Body, &payload))
	require.Equal(t, []string{"gpt-5.5", "gpt-5.4", "gpt-5.4-mini", "gpt-5.3-codex"}, codexGatewayModelSlugs(payload.Models))
}
