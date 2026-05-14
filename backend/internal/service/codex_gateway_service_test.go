package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type codexGatewayExecutorStub struct {
	completeFn func(ctx context.Context, req CodexGatewayProviderRequest) (*CodexGatewayServiceResponse, error)
	streamFn   func(ctx context.Context, req CodexGatewayProviderRequest) error
}

func TestCodexGatewayService_ResponsesRecordsCaptureTrace(t *testing.T) {
	baseDir := t.TempDir()
	capture := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                  true,
		BaseDir:                  baseDir,
		HashKeyFile:              filepath.Join(baseDir, ".key"),
		CaptureSuccessSampleRate: 1,
		IncludeResponseHeader:    true,
	})
	defer capture.Close()
	registry := NewDefaultCodexGatewayModelRegistry()
	executor := &codexGatewayExecutorStub{
		completeFn: func(_ context.Context, req CodexGatewayProviderRequest) (*CodexGatewayServiceResponse, error) {
			require.NotNil(t, req.CaptureTrace)
			return &CodexGatewayServiceResponse{
				StatusCode: http.StatusOK,
				Headers:    http.Header{"Content-Type": []string{"application/json"}},
				Body:       []byte(`{"id":"resp_capture","model":"gpt-5.5","output":[]}`),
			}, nil
		},
	}
	svc := NewCodexGatewayService(registry, executor, capture)
	resp, err := svc.Responses(context.Background(), CodexGatewayResponsesRequest{
		APIKey:  validCodexGatewayAPIKeyForTest(),
		Headers: http.Header{"Authorization": []string{"Bearer sk-secret"}},
		Body:    []byte(`{"model":"gpt-5.5","input":"private prompt"}`),
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, capture.Close())

	dateDir := filepath.Join(baseDir, time.Now().Format("2006-01-02"))
	entries, err := os.ReadDir(dateDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	traceDir := filepath.Join(dateDir, entries[0].Name())
	summary, err := os.ReadFile(filepath.Join(traceDir, "summary.json"))
	require.NoError(t, err)
	require.Contains(t, string(summary), `"status": "ok"`)
	clientShape, err := os.ReadFile(filepath.Join(traceDir, "client_request.shape.json"))
	require.NoError(t, err)
	require.NotContains(t, string(clientShape), "private prompt")
	headers, err := os.ReadFile(filepath.Join(traceDir, "client_request.headers.json"))
	require.NoError(t, err)
	require.Contains(t, string(headers), "[REDACTED]")
}

func TestCodexGatewayService_ModelsRecordsCaptureTrace(t *testing.T) {
	baseDir := t.TempDir()
	capture := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                  true,
		BaseDir:                  baseDir,
		HashKeyFile:              filepath.Join(baseDir, ".key"),
		CaptureSuccessSampleRate: 1,
	})
	defer capture.Close()
	svc := NewCodexGatewayService(NewDefaultCodexGatewayModelRegistry(), &codexGatewayExecutorStub{}, capture)

	resp, err := svc.Models(context.Background(), CodexGatewayModelsRequest{APIKey: validCodexGatewayAPIKeyForTest()})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, capture.Close())

	dateDir := filepath.Join(baseDir, time.Now().Format("2006-01-02"))
	entries, err := os.ReadDir(dateDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	modelShape, err := os.ReadFile(filepath.Join(dateDir, entries[0].Name(), "model_catalog.shape.json"))
	require.NoError(t, err)
	require.Contains(t, string(modelShape), "gpt-5.5")
	require.Contains(t, string(modelShape), "supported_in_api")
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

func TestCodexGatewayService_ResponsesFailoverErrorUsesMappedBodyMessage(t *testing.T) {
	registry := NewDefaultCodexGatewayModelRegistry()
	mappedBody, err := MarshalCodexGatewayErrorJSON(CodexGatewayErrorTypeAPI, "upstream_timeout", "Anthropic upstream returned Cloudflare 524 timeout.")
	require.NoError(t, err)
	svc := NewCodexGatewayService(registry, &codexGatewayExecutorStub{
		completeFn: func(_ context.Context, _ CodexGatewayProviderRequest) (*CodexGatewayServiceResponse, error) {
			return nil, &UpstreamFailoverError{StatusCode: 524, ResponseBody: mappedBody}
		},
	})

	resp, err := svc.Responses(context.Background(), CodexGatewayResponsesRequest{
		APIKey: validCodexGatewayAPIKeyForTest(),
		Body:   []byte(`{"model":"gpt-5.5","input":"hello"}`),
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusBadGateway, resp.StatusCode)
	require.Equal(t, "upstream_timeout", gjson.GetBytes(resp.Body, "error.code").String())
	require.Equal(t, "Anthropic upstream returned Cloudflare 524 timeout.", gjson.GetBytes(resp.Body, "error.message").String())
}

func TestCodexGatewayService_StreamFailoverErrorUsesMappedBodyMessage(t *testing.T) {
	registry := NewDefaultCodexGatewayModelRegistry()
	mappedBody, err := MarshalCodexGatewayErrorJSON(CodexGatewayErrorTypeAPI, "upstream_timeout", "Anthropic upstream returned Cloudflare 524 timeout.")
	require.NoError(t, err)
	svc := NewCodexGatewayService(registry, &codexGatewayExecutorStub{
		streamFn: func(_ context.Context, _ CodexGatewayProviderRequest) error {
			return &UpstreamFailoverError{StatusCode: 524, ResponseBody: mappedBody}
		},
	})

	var out bytes.Buffer
	resp, err := svc.Responses(context.Background(), CodexGatewayResponsesRequest{
		APIKey:         validCodexGatewayAPIKeyForTest(),
		Body:           []byte(`{"model":"gpt-5.5","input":"hello","stream":true}`),
		StreamWriter:   &out,
		ResponseHeader: http.Header{},
		WriteStatus:    func(int) {},
	})
	require.NoError(t, err)
	require.Nil(t, resp)
	require.Contains(t, out.String(), `"code":"upstream_timeout"`)
	require.Contains(t, out.String(), `"message":"Anthropic upstream returned Cloudflare 524 timeout."`)
	require.NotContains(t, out.String(), "<!DOCTYPE html>")
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
	registry := NewCodexGatewayModelRegistry(
		config.GatewayCodexConfig{
			EnabledModels: []string{"deepseek-v4-pro"},
		},
		WithCodexGatewayRegistryStateSource(&codexGatewayRegistryStateSourceStub{
			state: &CodexGatewayRegistryState{
				ProviderGroups: map[CodexGatewayProvider]CodexGatewayProviderRuntime{
					CodexGatewayProviderDeepSeek: {
						Provider: CodexGatewayProviderDeepSeek,
						GroupID:  2002,
						Healthy:  true,
					},
				},
				Models: map[string]CodexGatewayModelMutation{
					"deepseek-v4-pro": {Enabled: true},
				},
			},
		}),
		WithCodexGatewayPricingReadyChecker(codexGatewayPricingReadyCheckerStub{ready: map[string]bool{"deepseek-v4-pro": true}}),
		WithCodexGatewayProtocolReadyChecker(codexGatewayProtocolReadyCheckerStub{ready: map[string]bool{"deepseek-v4-pro": true}}),
	)
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

func TestCodexGatewayService_ResponsesStreamingFailoverErrorClearsStaleUpstreamHeaders(t *testing.T) {
	registry := NewDefaultCodexGatewayModelRegistry()
	svc := NewCodexGatewayService(registry, &codexGatewayExecutorStub{
		streamFn: func(_ context.Context, req CodexGatewayProviderRequest) error {
			if req.Request.ResponseHeader != nil {
				req.Request.ResponseHeader.Set("X-Request-Id", "stale-upstream")
				req.Request.ResponseHeader.Set("X-Codex-Turn-State", "stale-turn")
				req.Request.ResponseHeader.Set("Content-Type", "application/json")
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
	require.Empty(t, headers.Get("X-Request-Id"))
	require.Empty(t, headers.Get("X-Codex-Turn-State"))
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
