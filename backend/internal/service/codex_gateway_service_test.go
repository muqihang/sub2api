package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

func TestCodexGatewayInstructionsContainIgnoresWhitespaceDifferences(t *testing.T) {
	equivalent := strings.Replace(
		codexGatewayDefaultBaseInstructions,
		"(If the `rg` command is not found, then use alternatives.)\n- Act as an agent:",
		"(If the `rg` command is not found, then use alternatives.)\n\n- Act as an agent:",
		1,
	)

	require.True(t, codexGatewayInstructionsContain(equivalent, codexGatewayDefaultBaseInstructions))
	require.False(t, codexGatewayInstructionsContain("You are Codex.", codexGatewayDefaultBaseInstructions))
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
	traceDirs := codexGatewayCaptureTraceDirsForTest(t, dateDir)
	require.Len(t, traceDirs, 1)
	traceDir := filepath.Join(dateDir, traceDirs[0])
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
	traceDirs := codexGatewayCaptureTraceDirsForTest(t, dateDir)
	require.Len(t, traceDirs, 1)
	modelShape, err := os.ReadFile(filepath.Join(dateDir, traceDirs[0], "model_catalog.shape.json"))
	require.NoError(t, err)
	require.Contains(t, string(modelShape), "models")
	require.Contains(t, string(modelShape), "supported_in_api")
}

func codexGatewayCaptureTraceDirsForTest(t *testing.T, dateDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dateDir)
	require.NoError(t, err)
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			out = append(out, entry.Name())
		}
	}
	return out
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

func TestCodexGatewayService_ResponsesInjectsBaseInstructionsForDeepSeek(t *testing.T) {
	var captured CodexGatewayProviderRequest
	registry := NewCodexGatewayModelRegistry(
		config.GatewayCodexConfig{
			EnabledModels: []string{"deepseek-v4-pro"},
		},
		WithCodexGatewayRegistryStateSource(&codexGatewayRegistryStateSourceStub{
			state: &CodexGatewayRegistryState{
				ProviderGroups: map[CodexGatewayProvider]CodexGatewayProviderRuntime{
					CodexGatewayProviderDeepSeek: {Provider: CodexGatewayProviderDeepSeek, GroupID: 2002, Healthy: true},
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
		completeFn: func(_ context.Context, req CodexGatewayProviderRequest) (*CodexGatewayServiceResponse, error) {
			captured = req
			return &CodexGatewayServiceResponse{
				StatusCode: http.StatusOK,
				Headers:    http.Header{"Content-Type": []string{"application/json"}},
				Body:       []byte(`{"id":"resp_deepseek"}`),
			}, nil
		},
	})

	resp, err := svc.Responses(context.Background(), CodexGatewayResponsesRequest{
		APIKey: validCodexGatewayAPIKeyForTest(),
		Body:   []byte(`{"model":"deepseek-v4-pro","input":"hello"}`),
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	instructions, ok := parseCodexGatewayJSONString(captured.Parsed.Instructions)
	require.True(t, ok)
	require.Contains(t, instructions, "You are Codex, based on GPT-5.")
	require.Contains(t, instructions, "Try to use `edit`")
	require.Contains(t, instructions, "skills, plugins, MCP servers, or tool routing guidance")
	require.Contains(t, instructions, "clearly matches")
	require.Contains(t, instructions, "Do not load unrelated skills")
}

func TestCodexGatewayService_ResponsesAddsRoutingBridgeWithoutDuplicatingExistingBaseInstructions(t *testing.T) {
	var captured CodexGatewayProviderRequest
	registry := NewCodexGatewayModelRegistry(
		config.GatewayCodexConfig{
			EnabledModels: []string{"deepseek-v4-pro"},
		},
		WithCodexGatewayRegistryStateSource(&codexGatewayRegistryStateSourceStub{
			state: &CodexGatewayRegistryState{
				ProviderGroups: map[CodexGatewayProvider]CodexGatewayProviderRuntime{
					CodexGatewayProviderDeepSeek: {Provider: CodexGatewayProviderDeepSeek, GroupID: 2002, Healthy: true},
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
		completeFn: func(_ context.Context, req CodexGatewayProviderRequest) (*CodexGatewayServiceResponse, error) {
			captured = req
			return &CodexGatewayServiceResponse{
				StatusCode: http.StatusOK,
				Headers:    http.Header{"Content-Type": []string{"application/json"}},
				Body:       []byte(`{"id":"resp_deepseek"}`),
			}, nil
		},
	})

	rawInstructions, err := json.Marshal(codexGatewayDefaultBaseInstructions + "\n\n<skills_instructions>Available skills...</skills_instructions>")
	require.NoError(t, err)
	body := []byte(fmt.Sprintf(`{"model":"deepseek-v4-pro","instructions":%s,"input":"hello"}`, rawInstructions))
	resp, err := svc.Responses(context.Background(), CodexGatewayResponsesRequest{
		APIKey: validCodexGatewayAPIKeyForTest(),
		Body:   body,
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	instructions, ok := parseCodexGatewayJSONString(captured.Parsed.Instructions)
	require.True(t, ok)
	require.Equal(t, 1, strings.Count(instructions, "You are Codex, based on GPT-5."))
	require.Contains(t, instructions, "skills, plugins, MCP servers, or tool routing guidance")
	require.Contains(t, instructions, "<skills_instructions>Available skills...</skills_instructions>")
}

func TestCodexGatewayService_ResponsesAddsRoutingBridgeWithoutDuplicatingEquivalentBaseInstructions(t *testing.T) {
	var captured CodexGatewayProviderRequest
	registry := NewCodexGatewayModelRegistry(
		config.GatewayCodexConfig{
			EnabledModels: []string{"deepseek-v4-pro"},
		},
		WithCodexGatewayRegistryStateSource(&codexGatewayRegistryStateSourceStub{
			state: &CodexGatewayRegistryState{
				ProviderGroups: map[CodexGatewayProvider]CodexGatewayProviderRuntime{
					CodexGatewayProviderDeepSeek: {Provider: CodexGatewayProviderDeepSeek, GroupID: 2002, Healthy: true},
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
		completeFn: func(_ context.Context, req CodexGatewayProviderRequest) (*CodexGatewayServiceResponse, error) {
			captured = req
			return &CodexGatewayServiceResponse{
				StatusCode: http.StatusOK,
				Headers:    http.Header{"Content-Type": []string{"application/json"}},
				Body:       []byte(`{"id":"resp_deepseek"}`),
			}, nil
		},
	})

	equivalentBase := strings.Replace(
		codexGatewayDefaultBaseInstructions,
		"(If the `rg` command is not found, then use alternatives.)\n- Act as an agent:",
		"(If the `rg` command is not found, then use alternatives.)\n\n- Act as an agent:",
		1,
	)
	rawInstructions, err := json.Marshal(equivalentBase + "\n<skills_instructions>Available skills...</skills_instructions>")
	require.NoError(t, err)
	body := []byte(fmt.Sprintf(`{"model":"deepseek-v4-pro","instructions":%s,"input":"hello"}`, rawInstructions))
	resp, err := svc.Responses(context.Background(), CodexGatewayResponsesRequest{
		APIKey: validCodexGatewayAPIKeyForTest(),
		Body:   body,
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	instructions, ok := parseCodexGatewayJSONString(captured.Parsed.Instructions)
	require.True(t, ok)
	require.Equal(t, 1, strings.Count(instructions, "You are Codex, based on GPT-5."))
	require.Equal(t, 1, strings.Count(instructions, "skills, plugins, MCP servers, or tool routing guidance"))
	require.Contains(t, instructions, "<skills_instructions>Available skills...</skills_instructions>")
}

func TestCodexGatewayService_ResponsesInjectsRoutingBridgeForAnthropic(t *testing.T) {
	var captured CodexGatewayProviderRequest
	registry := NewCodexGatewayModelRegistry(
		config.GatewayCodexConfig{
			EnabledModels: []string{"claude-opus-4-7"},
		},
		WithCodexGatewayRegistryStateSource(&codexGatewayRegistryStateSourceStub{
			state: &CodexGatewayRegistryState{
				ProviderGroups: map[CodexGatewayProvider]CodexGatewayProviderRuntime{
					CodexGatewayProviderAnthropic: {Provider: CodexGatewayProviderAnthropic, GroupID: 3003, Healthy: true},
				},
				Models: map[string]CodexGatewayModelMutation{
					"claude-opus-4-7": {Enabled: true},
				},
			},
		}),
		WithCodexGatewayPricingReadyChecker(codexGatewayPricingReadyCheckerStub{ready: map[string]bool{"claude-opus-4-7": true}}),
		WithCodexGatewayProtocolReadyChecker(codexGatewayProtocolReadyCheckerStub{ready: map[string]bool{"claude-opus-4-7": true}}),
	)
	svc := NewCodexGatewayService(registry, &codexGatewayExecutorStub{
		completeFn: func(_ context.Context, req CodexGatewayProviderRequest) (*CodexGatewayServiceResponse, error) {
			captured = req
			return &CodexGatewayServiceResponse{
				StatusCode: http.StatusOK,
				Headers:    http.Header{"Content-Type": []string{"application/json"}},
				Body:       []byte(`{"id":"resp_claude"}`),
			}, nil
		},
	})

	resp, err := svc.Responses(context.Background(), CodexGatewayResponsesRequest{
		APIKey: validCodexGatewayAPIKeyForTest(),
		Body:   []byte(`{"model":"claude-opus-4-7","input":"hello"}`),
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	instructions, ok := parseCodexGatewayJSONString(captured.Parsed.Instructions)
	require.True(t, ok)
	require.Contains(t, instructions, "You are Codex, based on GPT-5.")
	require.Contains(t, instructions, "skills, plugins, MCP servers, or tool routing guidance")
	require.Contains(t, instructions, "Do not load unrelated skills")
}

func TestCodexGatewayService_ResponsesDoesNotInjectBaseInstructionsForOpenAI(t *testing.T) {
	var captured CodexGatewayProviderRequest
	registry := NewDefaultCodexGatewayModelRegistry()
	svc := NewCodexGatewayService(registry, &codexGatewayExecutorStub{
		completeFn: func(_ context.Context, req CodexGatewayProviderRequest) (*CodexGatewayServiceResponse, error) {
			captured = req
			return &CodexGatewayServiceResponse{
				StatusCode: http.StatusOK,
				Headers:    http.Header{"Content-Type": []string{"application/json"}},
				Body:       []byte(`{"id":"resp_openai"}`),
			}, nil
		},
	})

	resp, err := svc.Responses(context.Background(), CodexGatewayResponsesRequest{
		APIKey: validCodexGatewayAPIKeyForTest(),
		Body:   []byte(`{"model":"gpt-5.5","instructions":"existing app instructions","input":"hello"}`),
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	instructions, ok := parseCodexGatewayJSONString(captured.Parsed.Instructions)
	require.True(t, ok)
	require.Equal(t, "existing app instructions", instructions)
	require.NotContains(t, instructions, "skills, plugins, MCP servers, or tool routing guidance")
	require.NotContains(t, instructions, "Try to use `edit`")
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

func TestCodexGatewayService_ManagedDeviceAllowsGenericEntitledKey(t *testing.T) {
	registry := NewDefaultCodexGatewayModelRegistry()
	executorCalled := false
	svc := NewCodexGatewayService(registry, &codexGatewayExecutorStub{
		completeFn: func(_ context.Context, _ CodexGatewayProviderRequest) (*CodexGatewayServiceResponse, error) {
			executorCalled = true
			return &CodexGatewayServiceResponse{
				StatusCode: http.StatusOK,
				Headers:    http.Header{"Content-Type": []string{"application/json"}},
				Body:       []byte(`{"id":"resp_managed","model":"gpt-5.5","output":[]}`),
			}, nil
		},
	})
	groupID := int64(44)
	apiKey := &APIKey{
		ID:      7,
		UserID:  88,
		Key:     "sk-generic",
		Status:  StatusActive,
		GroupID: &groupID,
		Group: &Group{
			ID:                   groupID,
			Platform:             PlatformOpenAI,
			Status:               StatusActive,
			Hydrated:             true,
			CodexGatewayEntitled: true,
		},
	}

	modelsResp, err := svc.Models(context.Background(), CodexGatewayModelsRequest{
		APIKey:        apiKey,
		ManagedDevice: true,
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, modelsResp.StatusCode)

	resp, err := svc.Responses(context.Background(), CodexGatewayResponsesRequest{
		APIKey:        apiKey,
		Body:          []byte(`{"model":"gpt-5.5"}`),
		ManagedDevice: true,
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.True(t, executorCalled)
}

func TestCodexGatewayService_ResponsesDeepSeekPreviousResponseIDDispatchesToExecutor(t *testing.T) {
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
	var captured CodexGatewayProviderRequest
	svc := NewCodexGatewayService(registry, &codexGatewayExecutorStub{
		completeFn: func(_ context.Context, req CodexGatewayProviderRequest) (*CodexGatewayServiceResponse, error) {
			captured = req
			return &CodexGatewayServiceResponse{
				StatusCode: http.StatusOK,
				Headers:    http.Header{"Content-Type": []string{"application/json"}},
				Body:       []byte(`{"id":"resp_next","output":[]}`),
			}, nil
		},
	})

	resp, err := svc.Responses(context.Background(), CodexGatewayResponsesRequest{
		APIKey: validCodexGatewayAPIKeyForTest(),
		Body:   []byte(`{"model":"deepseek-v4-pro","previous_response_id":"resp_prev"}`),
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NotNil(t, captured.Parsed.PreviousResponseID)
	require.Equal(t, "resp_prev", *captured.Parsed.PreviousResponseID)
	require.Equal(t, "deepseek", captured.Model.Provider)
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

func TestCodexGatewayService_ResponsesStreamingDelaysSSEHeadersUntilFirstWrite(t *testing.T) {
	registry := NewDefaultCodexGatewayModelRegistry()
	var statusCode int
	headers := http.Header{}
	svc := NewCodexGatewayService(registry, &codexGatewayExecutorStub{
		streamFn: func(_ context.Context, req CodexGatewayProviderRequest) error {
			require.Equal(t, 0, statusCode)
			require.Empty(t, headers.Get("Content-Type"))
			_, err := req.Request.StreamWriter.Write([]byte("event: ping\ndata: {}\n\n"))
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, statusCode)
			require.Equal(t, "text/event-stream", headers.Get("Content-Type"))
			return nil
		},
	})
	var out bytes.Buffer

	resp, err := svc.Responses(context.Background(), CodexGatewayResponsesRequest{
		APIKey:         validCodexGatewayAPIKeyForTest(),
		Body:           []byte(`{"model":"gpt-5.5","stream":true}`),
		StreamWriter:   &out,
		ResponseHeader: headers,
		WriteStatus:    func(code int) { statusCode = code },
	})
	require.NoError(t, err)
	require.Nil(t, resp)
	require.Equal(t, "event: ping\ndata: {}\n\n", out.String())
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

func TestCodexGatewayService_ModelsReturnsCapabilitiesAndPricing(t *testing.T) {
	groupID := int64(1001)
	registry := NewCodexGatewayModelRegistry(
		config.GatewayCodexConfig{EnabledModels: []string{"gpt-5.5"}},
		WithCodexGatewayRegistryStateSource(&codexGatewayRegistryStateSourceStub{
			state: &CodexGatewayRegistryState{
				ProviderGroups: map[CodexGatewayProvider]CodexGatewayProviderRuntime{
					CodexGatewayProviderOpenAI: {Provider: CodexGatewayProviderOpenAI, GroupID: groupID, Healthy: true},
				},
			},
		}),
		WithCodexGatewayModelPricingResolver(codexGatewayModelPricingResolverStub{pricing: map[string]*CodexGatewayModelPricing{
			"gpt-5.5": {InputPrice: stringPtr("2.50"), OutputPrice: stringPtr("15.00"), Currency: "USD", Unit: "per_1m_tokens", Source: "database_model_pricing"},
		}}),
	)
	svc := NewCodexGatewayService(registry, &codexGatewayExecutorStub{})
	apiKey := validCodexGatewayAPIKeyForTest()
	apiKey.GroupID = &groupID

	resp, err := svc.Models(context.Background(), CodexGatewayModelsRequest{APIKey: apiKey})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var payload CodexGatewayModelsResponse
	require.NoError(t, json.Unmarshal(resp.Body, &payload))
	require.Len(t, payload.Models, 1)
	require.True(t, payload.Models[0].Capabilities.ToolCalls)
	require.NotNil(t, payload.Models[0].Pricing)
	require.Equal(t, "database_model_pricing", payload.Models[0].Pricing.Source)
}
