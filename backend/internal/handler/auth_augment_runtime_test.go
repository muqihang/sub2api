package handler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAugmentLegacyChatRoutesFirstBatchModelsThroughAugmentGateway(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		modelID  string
		provider service.AugmentGatewayProvider
	}{
		{modelID: "gpt-5.4", provider: service.AugmentGatewayProviderOpenAI},
		{modelID: "gpt-5.5", provider: service.AugmentGatewayProviderOpenAI},
		{modelID: "gpt-5.4-mini", provider: service.AugmentGatewayProviderOpenAI},
		{modelID: "deepseek-v4-pro", provider: service.AugmentGatewayProviderDeepSeek},
		{modelID: "deepseek-v4-flash", provider: service.AugmentGatewayProviderDeepSeek},
	} {
		t.Run(tc.modelID, func(t *testing.T) {
			t.Parallel()

			executor := &augmentGatewayRouteFakeExecutor{
				completeResult: service.AugmentGatewayProviderResult{
					Text: "gateway " + tc.modelID,
					Usage: service.AugmentGatewayProviderUsage{
						InputTokens:  3,
						OutputTokens: 4,
						TotalTokens:  7,
					},
				},
			}
			server, apiKey, loopbackCalls := newAugmentGatewayRuntimeTestServer(t, executor)
			defer server.Close()

			resp := postAugmentGatewayRuntimeJSON(t, server, apiKey, "/chat", `{
				"model":"`+tc.modelID+`",
				"message":"route through gateway",
				"conversation_id":"conv-route-`+tc.modelID+`"
			}`)
			defer resp.Body.Close()
			require.Equal(t, http.StatusOK, resp.StatusCode)

			body := decodeAugmentContractObjectFromReader(t, resp.Body)
			require.Equal(t, "gateway "+tc.modelID, body["text"])

			calls := executor.CompleteRequests()
			require.Len(t, calls, 1)
			require.Equal(t, tc.modelID, calls[0].Model.ID)
			require.Equal(t, tc.provider, calls[0].Model.Provider)
			require.Equal(t, tc.provider, calls[0].Provider)
			require.Equal(t, "/chat", calls[0].Endpoint)
			require.Equal(t, false, calls[0].RawBody["stream"])
			require.Equal(t, 0, *loopbackCalls, "Augment /chat must not call the local OpenAI loopback route")
		})
	}
}

func TestAugmentLegacyChatUnknownModelReturnsAugmentGatewayErrorWithoutFallback(t *testing.T) {
	t.Parallel()

	executor := &augmentGatewayRouteFakeExecutor{}
	server, apiKey, loopbackCalls := newAugmentGatewayRuntimeTestServer(t, executor)
	defer server.Close()

	resp := postAugmentGatewayRuntimeJSON(t, server, apiKey, "/chat", `{
		"model":"unknown-model",
		"message":"do not fallback"
	}`)
	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	body := readBody(t, resp.Body)
	require.Contains(t, body, "AUGMENT_GATEWAY_MODEL_UNAVAILABLE")
	require.Empty(t, executor.CompleteRequests())
	require.Equal(t, 0, *loopbackCalls)
}

func TestAugmentLegacyResolveRetrievalDoesNotLeakCheckpointMissWhenOfficialCodebaseToolOwnsRetrieval(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	user := &service.User{ID: 7, Email: "augment@example.com", Role: service.RoleAdmin, Status: service.StatusActive}
	apiKey := &service.APIKey{ID: 11, UserID: user.ID, Key: "sk-augment-runtime", Status: service.StatusActive, User: user}
	pluginService := service.NewAugmentPluginService(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		augmentPluginAuthStub{},
		augmentPluginUserStub{user: user},
		augmentPluginAPIKeyStub{apiKeyByValue: map[string]*service.APIKey{apiKey.Key: apiKey}},
		augmentPluginSubscriptionStub{},
		augmentPluginSettingStub{},
	)

	principal := &service.AugmentPluginPrincipal{APIKey: apiKey, User: user}
	namespace := buildAugmentLegacyNamespace(principal, "session-official-ce")
	pluginService.StoreLegacyBlobsForNamespace(namespace, []service.AugmentLegacyUploadedBlob{
		{BlobName: "blob-a", Path: "src/main.go", Content: "package main\nfunc main() {}\n"},
	})

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/chat-stream", nil)
	req.Header.Set("x-request-session-id", "session-official-ce")
	c.Request = req

	authHandler := &AuthHandler{augmentPluginService: pluginService}
	retrieval, unknown, checkpointNotFound := authHandler.augmentLegacyResolveRetrieval(c, principal, augmentLegacyChatRequest{
		Model:   "gpt-5.4",
		Message: "find gateway routing",
		Blobs: augmentLegacyCheckpointBlobsPayload{
			CheckpointID: "checkpoint-stale",
		},
		ToolDefinitions: []augmentLegacyToolDefinition{
			{Name: "codebase-retrieval", Description: "repo search", InputSchema: map[string]any{"type": "object"}},
		},
	})

	require.Equal(t, "", retrieval)
	require.Equal(t, []string{}, unknown)
	require.False(t, checkpointNotFound)
}

func TestAugmentLegacyChatRejectsGenericAPIKeyForAugmentRoutes(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	user := &service.User{
		ID:       3,
		Email:    "generic@example.com",
		Username: "generic",
		Role:     service.RoleAdmin,
		Status:   service.StatusActive,
	}
	group := service.Group{
		ID:                 301,
		Name:               "OpenAI",
		Platform:           service.PlatformOpenAI,
		Status:             service.StatusActive,
		Hydrated:           true,
		DefaultMappedModel: "gpt-5.4",
	}
	apiKey := &service.APIKey{
		ID:        3,
		UserID:    user.ID,
		Key:       "sk-generic-runtime",
		Name:      "generic-runtime",
		GroupID:   &group.ID,
		Group:     &group,
		Status:    service.StatusActive,
		CreatedAt: time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC),
		User:      user,
	}

	pluginService := service.NewAugmentPluginService(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		augmentPluginAuthStub{},
		augmentPluginUserStub{user: user},
		augmentPluginAPIKeyStub{
			apiKeyByValue: map[string]*service.APIKey{apiKey.Key: apiKey},
			keysByUser:    map[int64][]service.APIKey{user.ID: {*apiKey}},
			availableByUser: map[int64][]service.Group{
				user.ID: {group},
			},
		},
		augmentPluginSubscriptionStub{},
		augmentPluginSettingStub{},
	)

	authHandler := NewAuthHandler(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		nil, nil, nil, nil, nil, nil,
		pluginService,
	)

	router := gin.New()
	router.POST("/chat", authHandler.AugmentLegacyChat)

	req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(`{"model":"gpt-5.4","message":"deny generic key"}`))
	req.Header.Set("Authorization", "Bearer "+apiKey.Key)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "AUGMENT_SCOPED_API_KEY_REQUIRED")
}

func TestAugmentLegacyChatStreamRoutesThroughAugmentGatewayAndEmitsReasoningToolAndUsageNodes(t *testing.T) {
	t.Parallel()

	executor := &augmentGatewayRouteFakeExecutor{
		streamChunks: []service.AugmentGatewayProviderChunk{
			{ReasoningContentDelta: "thinking through route"},
			{ToolCallDelta: &service.AugmentGatewayToolCall{
				ID:   "call-stream-1",
				Type: "function",
				Function: service.AugmentGatewayToolCallFunction{
					Name:      "codebase-retrieval",
					Arguments: `{"query":"gateway"}`,
				},
			}},
			{Usage: service.AugmentGatewayProviderUsage{InputTokens: 11, OutputTokens: 13, TotalTokens: 24}},
			{Done: true, ProviderFinishReason: "tool_calls"},
		},
	}
	server, apiKey, loopbackCalls := newAugmentGatewayRuntimeTestServer(t, executor)
	defer server.Close()

	resp := postAugmentGatewayRuntimeJSON(t, server, apiKey, "/chat-stream", `{
		"model":"gpt-5.4",
		"message":"stream through gateway",
		"conversation_id":"conv-stream-route"
	}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	chunks := decodeAugmentContractNDJSON(t, resp.Body)
	require.NotEmpty(t, chunks)
	requireAugmentGatewayStreamHasNodeType(t, chunks, float64(augmentResponseNodeThinking))
	requireAugmentGatewayStreamHasNodeType(t, chunks, float64(augmentResponseNodeToolUse))
	requireAugmentGatewayStreamHasNodeType(t, chunks, float64(augmentResponseNodeTokenUsage))

	calls := executor.StreamRequests()
	require.Len(t, calls, 1)
	require.Equal(t, service.AugmentGatewayProviderOpenAI, calls[0].Provider)
	require.Equal(t, true, calls[0].RawBody["stream"])
	require.Equal(t, 0, *loopbackCalls, "Augment /chat-stream must not call the local OpenAI loopback route")
}

func TestAugmentLegacyChatStreamThroughGatewayBuffersIndexedToolCallArguments(t *testing.T) {
	t.Parallel()

	idx := 0
	executor := &augmentGatewayRouteFakeExecutor{
		streamChunks: []service.AugmentGatewayProviderChunk{
			{ToolCallDelta: &service.AugmentGatewayToolCall{
				Index: &idx,
				ID:    "call-read-1",
				Type:  "function",
				Function: service.AugmentGatewayToolCallFunction{
					Name:      "read-file",
					Arguments: `{"path":`,
				},
			}},
			{ToolCallDelta: &service.AugmentGatewayToolCall{
				Index: &idx,
				Function: service.AugmentGatewayToolCallFunction{
					Arguments: `"README.md"}`,
				},
			}},
			{Done: true, ProviderFinishReason: "tool_calls"},
		},
	}
	server, apiKey, _ := newAugmentGatewayRuntimeTestServer(t, executor)
	defer server.Close()

	resp := postAugmentGatewayRuntimeJSON(t, server, apiKey, "/chat-stream", `{
		"model":"gpt-5.4",
		"message":"read a file",
		"conversation_id":"conv-indexed-tool-call"
	}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	chunks := decodeAugmentContractNDJSON(t, resp.Body)
	var sawToolUse bool
	for _, chunk := range chunks {
		require.NotContains(t, chunk["text"], "incomplete tool arguments")
		for _, node := range augmentGatewayContractNodes(chunk) {
			if node["type"] != float64(augmentResponseNodeToolUse) {
				continue
			}
			toolUse, ok := node["tool_use"].(map[string]any)
			require.True(t, ok)
			require.Equal(t, "read-file", toolUse["tool_name"])
			require.Equal(t, `{"path":"README.md"}`, toolUse["input_json"])
			sawToolUse = true
		}
	}
	require.True(t, sawToolUse)
}

func TestAugmentLegacyChatStreamThroughGatewayCoalescesReasoningDeltas(t *testing.T) {
	t.Parallel()

	executor := &augmentGatewayRouteFakeExecutor{
		streamChunks: []service.AugmentGatewayProviderChunk{
			{ReasoningContentDelta: "The "},
			{ReasoningContentDelta: "user "},
			{ReasoningContentDelta: "asked."},
			{TextDelta: "answer"},
			{Done: true, ProviderFinishReason: "stop"},
		},
	}
	server, apiKey, _ := newAugmentGatewayRuntimeTestServer(t, executor)
	defer server.Close()

	resp := postAugmentGatewayRuntimeJSON(t, server, apiKey, "/chat-stream", `{
		"model":"deepseek-v4-pro",
		"message":"explain",
		"conversation_id":"conv-reasoning-coalesce"
	}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	chunks := decodeAugmentContractNDJSON(t, resp.Body)
	var thinkingNodes []map[string]any
	for _, chunk := range chunks {
		for _, node := range augmentGatewayContractNodes(chunk) {
			if node["type"] == float64(augmentResponseNodeThinking) {
				thinkingNodes = append(thinkingNodes, node)
			}
		}
	}
	require.Len(t, thinkingNodes, 1)
	thinking, ok := thinkingNodes[0]["thinking"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "The user asked.", thinking["summary"])
}

func TestAugmentGatewayDeepSeekToolCallReplayAcrossHTTPRequests(t *testing.T) {
	t.Parallel()

	executor := &augmentGatewayRouteFakeExecutor{
		completeFunc: func(req service.AugmentGatewayProviderRequest, callIndex int) (service.AugmentGatewayProviderResult, error) {
			if callIndex == 0 {
				return service.AugmentGatewayProviderResult{
					RequestID:               "provider-req-nonstream",
					UpstreamRequestID:       "upstream-nonstream",
					ReasoningContent:        "Need codebase retrieval before answering.",
					ReasoningContentPresent: true,
					ToolCalls:               []service.AugmentGatewayToolCall{augmentGatewayRouteCodebaseToolCall("call-nonstream")},
				}, nil
			}
			return service.AugmentGatewayProviderResult{Text: "done"}, nil
		},
	}
	server, apiKey, _ := newAugmentGatewayRuntimeTestServer(t, executor)
	defer server.Close()

	firstResp := postAugmentGatewayRuntimeJSON(t, server, apiKey, "/chat", `{
		"model":"deepseek-v4-pro",
		"message":"use retrieval",
		"conversation_id":"conv-deepseek-replay",
		"tool_definitions":[{"name":"codebase-retrieval","description":"repo search","input_schema":{"type":"object"}}]
	}`)
	defer firstResp.Body.Close()
	require.Equal(t, http.StatusOK, firstResp.StatusCode)

	secondResp := postAugmentGatewayRuntimeJSON(t, server, apiKey, "/chat", `{
		"model":"deepseek-v4-pro",
		"conversation_id":"conv-deepseek-replay",
		"requestNodes":[{
			"id":1,
			"type":1,
			"toolResultNode":{"toolUseId":"call-nonstream","content":"[CODEBASE_RETRIEVAL]\nREADME.md"}
		}],
		"tool_definitions":[{"name":"codebase-retrieval","description":"repo search","input_schema":{"type":"object"}}]
	}`)
	defer secondResp.Body.Close()
	require.Equal(t, http.StatusOK, secondResp.StatusCode)

	calls := executor.CompleteRequests()
	require.Len(t, calls, 2)
	requireAugmentGatewayDeepSeekReplayBody(t, calls[1].RawBody, "Need codebase retrieval before answering.")
	requireAugmentGatewayDeepSeekReplayToolArguments(t, calls[1].RawBody, "call-nonstream", `{"query":"gateway replay"}`)
}

func TestAugmentGatewayDeepSeekPairsMultipleToolResultsAcrossHTTPRequests(t *testing.T) {
	t.Parallel()

	executor := &augmentGatewayRouteFakeExecutor{
		completeFunc: func(req service.AugmentGatewayProviderRequest, callIndex int) (service.AugmentGatewayProviderResult, error) {
			if callIndex == 0 {
				return service.AugmentGatewayProviderResult{
					RequestID:               "provider-req-multi-tool",
					UpstreamRequestID:       "upstream-multi-tool",
					ReasoningContent:        "Need multiple retrievals before answering.",
					ReasoningContentPresent: true,
					ToolCalls: []service.AugmentGatewayToolCall{
						augmentGatewayRouteCodebaseToolCallWithArguments("call-first", `{"query":"first query"}`),
						augmentGatewayRouteCodebaseToolCallWithArguments("call-second", `{"query":"second query"}`),
					},
				}, nil
			}
			return service.AugmentGatewayProviderResult{Text: "done"}, nil
		},
	}
	server, apiKey, _ := newAugmentGatewayRuntimeTestServer(t, executor)
	defer server.Close()

	firstResp := postAugmentGatewayRuntimeJSON(t, server, apiKey, "/chat", `{
		"model":"deepseek-v4-pro",
		"message":"use retrieval twice",
		"conversation_id":"conv-deepseek-multi-tool-replay",
		"tool_definitions":[{"name":"codebase-retrieval","description":"repo search","input_schema":{"type":"object"}}]
	}`)
	defer firstResp.Body.Close()
	require.Equal(t, http.StatusOK, firstResp.StatusCode)

	secondResp := postAugmentGatewayRuntimeJSON(t, server, apiKey, "/chat", `{
		"model":"deepseek-v4-pro",
		"conversation_id":"conv-deepseek-multi-tool-replay",
		"requestNodes":[
			{
				"id":1,
				"type":1,
				"toolResultNode":{"toolUseId":"call-first","content":"[CODEBASE_RETRIEVAL]\nfirst result"}
			},
			{
				"id":2,
				"type":1,
				"toolResultNode":{"toolUseId":"call-second","content":"[CODEBASE_RETRIEVAL]\nsecond result"}
			}
		],
		"tool_definitions":[{"name":"codebase-retrieval","description":"repo search","input_schema":{"type":"object"}}]
	}`)
	defer secondResp.Body.Close()
	require.Equal(t, http.StatusOK, secondResp.StatusCode)

	calls := executor.CompleteRequests()
	require.Len(t, calls, 2)
	requireAugmentGatewayDeepSeekToolCallResultPairs(t, calls[1].RawBody, []string{"call-first", "call-second"})
	requireAugmentGatewayDeepSeekReplayToolArguments(t, calls[1].RawBody, "call-first", `{"query":"first query"}`)
	requireAugmentGatewayDeepSeekReplayToolArguments(t, calls[1].RawBody, "call-second", `{"query":"second query"}`)
}

func TestAugmentGatewayOpenAIPairsMultipleToolResultsAcrossHTTPRequests(t *testing.T) {
	t.Parallel()

	executor := &augmentGatewayRouteFakeExecutor{
		completeFunc: func(req service.AugmentGatewayProviderRequest, callIndex int) (service.AugmentGatewayProviderResult, error) {
			if callIndex == 0 {
				return service.AugmentGatewayProviderResult{
					RequestID:         "provider-req-openai-multi-tool",
					UpstreamRequestID: "upstream-openai-multi-tool",
					ToolCalls: []service.AugmentGatewayToolCall{
						augmentGatewayRouteCodebaseToolCallWithArguments("call-openai-first", `{"query":"first openai query"}`),
						augmentGatewayRouteCodebaseToolCallWithArguments("call-openai-second", `{"query":"second openai query"}`),
					},
				}, nil
			}
			return service.AugmentGatewayProviderResult{Text: "done"}, nil
		},
	}
	server, apiKey, _ := newAugmentGatewayRuntimeTestServer(t, executor)
	defer server.Close()

	firstResp := postAugmentGatewayRuntimeJSON(t, server, apiKey, "/chat", `{
		"model":"gpt-5.4",
		"message":"use retrieval twice",
		"conversation_id":"conv-openai-multi-tool-replay",
		"tool_definitions":[{"name":"codebase-retrieval","description":"repo search","input_schema":{"type":"object"}}]
	}`)
	defer firstResp.Body.Close()
	require.Equal(t, http.StatusOK, firstResp.StatusCode)

	secondResp := postAugmentGatewayRuntimeJSON(t, server, apiKey, "/chat", `{
		"model":"gpt-5.4",
		"conversation_id":"conv-openai-multi-tool-replay",
		"requestNodes":[
			{
				"id":1,
				"type":1,
				"toolResultNode":{"toolUseId":"call-openai-first","content":"[CODEBASE_RETRIEVAL]\nfirst result"}
			},
			{
				"id":2,
				"type":1,
				"toolResultNode":{"toolUseId":"call-openai-second","content":"[CODEBASE_RETRIEVAL]\nsecond result"}
			}
		],
		"tool_definitions":[{"name":"codebase-retrieval","description":"repo search","input_schema":{"type":"object"}}]
	}`)
	defer secondResp.Body.Close()
	require.Equal(t, http.StatusOK, secondResp.StatusCode)

	calls := executor.CompleteRequests()
	require.Len(t, calls, 2)
	requireAugmentGatewayDeepSeekToolCallResultPairs(t, calls[1].RawBody, []string{"call-openai-first", "call-openai-second"})
	requireAugmentGatewayDeepSeekReplayToolArguments(t, calls[1].RawBody, "call-openai-first", `{"query":"first openai query"}`)
	requireAugmentGatewayDeepSeekReplayToolArguments(t, calls[1].RawBody, "call-openai-second", `{"query":"second openai query"}`)
}

func TestAugmentGatewayDeepSeekToolCallReplayAcrossHTTPRequestsStreaming(t *testing.T) {
	t.Parallel()

	executor := &augmentGatewayRouteFakeExecutor{
		streamFunc: func(req service.AugmentGatewayProviderRequest, callIndex int, emit func(service.AugmentGatewayProviderChunk) error) error {
			if callIndex == 0 {
				if err := emit(service.AugmentGatewayProviderChunk{ReasoningContentDelta: ""}); err != nil {
					return err
				}
				if err := emit(service.AugmentGatewayProviderChunk{ReasoningContentDone: true}); err != nil {
					return err
				}
				if err := emit(service.AugmentGatewayProviderChunk{ToolCallDelta: &service.AugmentGatewayToolCall{
					ID:   "call-stream-empty-reasoning",
					Type: "function",
					Function: service.AugmentGatewayToolCallFunction{
						Name:      "codebase-retrieval",
						Arguments: `{"query":"stream replay"}`,
					},
				}}); err != nil {
					return err
				}
				return emit(service.AugmentGatewayProviderChunk{
					Done:                 true,
					ProviderFinishReason: "tool_calls",
					UpstreamRequestID:    "upstream-stream",
				})
			}
			return emit(service.AugmentGatewayProviderChunk{TextDelta: "done", Done: true, ProviderFinishReason: "stop"})
		},
	}
	server, apiKey, _ := newAugmentGatewayRuntimeTestServer(t, executor)
	defer server.Close()

	firstResp := postAugmentGatewayRuntimeJSON(t, server, apiKey, "/chat-stream", `{
		"model":"deepseek-v4-flash",
		"message":"use retrieval in stream",
		"conversation_id":"conv-deepseek-stream-replay",
		"tool_definitions":[{"name":"codebase-retrieval","description":"repo search","input_schema":{"type":"object"}}]
	}`)
	defer firstResp.Body.Close()
	require.Equal(t, http.StatusOK, firstResp.StatusCode)
	require.NotEmpty(t, decodeAugmentContractNDJSON(t, firstResp.Body))

	secondResp := postAugmentGatewayRuntimeJSON(t, server, apiKey, "/chat-stream", `{
		"model":"deepseek-v4-flash",
		"conversation_id":"conv-deepseek-stream-replay",
		"requestNodes":[{
			"id":1,
			"type":1,
			"toolResultNode":{"toolUseId":"call-stream-empty-reasoning","content":"[CODEBASE_RETRIEVAL]\nREADME.md"}
		}],
		"tool_definitions":[{"name":"codebase-retrieval","description":"repo search","input_schema":{"type":"object"}}]
	}`)
	defer secondResp.Body.Close()
	require.Equal(t, http.StatusOK, secondResp.StatusCode)
	require.NotEmpty(t, decodeAugmentContractNDJSON(t, secondResp.Body))

	calls := executor.StreamRequests()
	require.Len(t, calls, 2)
	requireAugmentGatewayDeepSeekReplayBody(t, calls[1].RawBody, "")
	requireAugmentGatewayDeepSeekReplayToolArguments(t, calls[1].RawBody, "call-stream-empty-reasoning", `{"query":"stream replay"}`)
}

func TestAugmentGatewayAddsCodebaseRetrievalQueryPolicy(t *testing.T) {
	t.Parallel()

	executor := &augmentGatewayRouteFakeExecutor{
		streamFunc: func(req service.AugmentGatewayProviderRequest, callIndex int, emit func(service.AugmentGatewayProviderChunk) error) error {
			return emit(service.AugmentGatewayProviderChunk{TextDelta: "ok", Done: true, ProviderFinishReason: "stop"})
		},
	}
	server, apiKey, _ := newAugmentGatewayRuntimeTestServer(t, executor)
	defer server.Close()

	resp := postAugmentGatewayRuntimeJSON(t, server, apiKey, "/chat-stream", `{
		"model":"deepseek-v4-pro",
		"message":"找 Augment Gateway /chat-stream 链路",
		"conversation_id":"conv-tool-policy",
		"tool_definitions":[{"name":"codebase-retrieval","description":"repo search","input_schema":{"type":"object"}}]
	}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NotEmpty(t, decodeAugmentContractNDJSON(t, resp.Body))

	calls := executor.StreamRequests()
	require.Len(t, calls, 1)
	messages, ok := calls[0].RawBody["messages"].([]any)
	require.True(t, ok)
	var policy string
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok || msg["role"] != "system" {
			continue
		}
		content, _ := msg["content"].(string)
		if strings.Contains(content, "codebase-retrieval") {
			policy = content
			break
		}
	}
	require.Contains(t, policy, "codebase-retrieval")
	require.Contains(t, policy, "完整")
	require.Contains(t, policy, "不要")
	require.Contains(t, policy, "短关键词")

	tools, ok := calls[0].RawBody["tools"].([]any)
	require.True(t, ok)
	require.Len(t, tools, 1)
	tool, ok := tools[0].(map[string]any)
	require.True(t, ok)
	function, ok := tool["function"].(map[string]any)
	require.True(t, ok)
	description, ok := function["description"].(string)
	require.True(t, ok)
	require.Contains(t, description, "repository-specific information_request")
	require.Contains(t, description, "routes, handlers")
}

func TestAugmentGatewayCacheableProvidersKeepVolatileContextOutOfFirstSystemMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	workspaceRoot := prepareAugmentContextBundleGitWorkspace(t)
	t.Setenv("AUGMENT_LEGACY_WORKSPACE_ROOT", workspaceRoot)

	for _, model := range []string{"gpt-5.4", "deepseek-v4-pro", "claude-sonnet-4-5"} {
		model := model
		t.Run(model, func(t *testing.T) {
			executor := &augmentGatewayRouteFakeExecutor{
				streamFunc: func(req service.AugmentGatewayProviderRequest, callIndex int, emit func(service.AugmentGatewayProviderChunk) error) error {
					return emit(service.AugmentGatewayProviderChunk{TextDelta: "ok", Done: true, ProviderFinishReason: "stop"})
				},
			}
			server, apiKey, _ := newAugmentGatewayRuntimeTestServerWithConfig(t, executor, config.GatewayAugmentConfig{
				Enabled:       true,
				EnabledModels: []string{"gpt-5.4", "deepseek-v4-pro", "claude-sonnet-4-5"},
				ProviderGroups: config.GatewayAugmentProviderGroupsConfig{
					OpenAI:    1001,
					DeepSeek:  1002,
					Anthropic: 1003,
				},
			})
			defer server.Close()

			resp := postAugmentGatewayRuntimeJSON(t, server, apiKey, "/chat-stream", `{
				"model":"`+model+`",
				"message":"找 Augment Gateway /chat-stream 链路",
				"conversation_id":"conv-volatile-context",
				"path":"backend/internal/handler/auth_augment_runtime.go",
				"lang":"go",
				"selected_text":"volatile selected text",
				"blobs":{"checkpoint_id":"ckpt-volatile","added_blobs":["blob-a","blob-b"],"deleted_blobs":[]},
				"chat_history":[{"request_message":"old question","response_text":"old answer"}],
				"tool_definitions":[{"name":"codebase-retrieval","description":"repo search","input_schema":{"type":"object"}}]
			}`)
			defer resp.Body.Close()
			require.Equal(t, http.StatusOK, resp.StatusCode)
			require.NotEmpty(t, decodeAugmentContractNDJSON(t, resp.Body))

			calls := executor.StreamRequests()
			require.Len(t, calls, 1)
			messages, ok := calls[0].RawBody["messages"].([]any)
			require.True(t, ok)
			require.NotEmpty(t, messages)
			first, ok := messages[0].(map[string]any)
			require.True(t, ok)
			require.Equal(t, "system", first["role"])
			content, _ := first["content"].(string)
			require.Contains(t, content, "codebase-retrieval")
			require.Contains(t, content, "Augment stable workspace context")
			require.Contains(t, content, "workspace_root: "+workspaceRoot)
			require.Contains(t, content, "branch: feature/context-bundle")
			require.NotContains(t, content, "conversation_id")
			require.NotContains(t, content, "chat_history_count")
			require.NotContains(t, content, "checkpoint_id")
			require.NotContains(t, content, "selected_text")
			require.NotContains(t, content, "old question")
			require.NotContains(t, content, "blob-a")
		})
	}
}

func TestAugmentLegacyRuntimeCompatibilityEndpoints(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	user := &service.User{
		ID:       2,
		Email:    "compat@example.com",
		Username: "compat",
		Role:     service.RoleAdmin,
		Status:   service.StatusActive,
	}
	apiKey := &service.APIKey{
		ID:        2,
		UserID:    user.ID,
		Key:       "sk-compat-runtime",
		Name:      "compat-runtime",
		Status:    service.StatusActive,
		CreatedAt: time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC),
		User:      user,
	}
	group := service.Group{
		ID:                 101,
		Name:               "OpenAI",
		Platform:           service.PlatformOpenAI,
		Status:             service.StatusActive,
		Hydrated:           true,
		DefaultMappedModel: "gpt-5.4",
	}
	markAugmentRuntimeAPIKey(apiKey, &group)

	pluginService := service.NewAugmentPluginService(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		augmentPluginAuthStub{},
		augmentPluginUserStub{user: user},
		augmentPluginAPIKeyStub{
			apiKeyByValue: map[string]*service.APIKey{apiKey.Key: apiKey},
			keysByUser:    map[int64][]service.APIKey{user.ID: {*apiKey}},
			availableByUser: map[int64][]service.Group{
				user.ID: {group},
			},
		},
		augmentPluginSubscriptionStub{},
		augmentPluginSettingStub{
			public: &service.PublicSettings{
				SiteName:   "逐梦站",
				APIBaseURL: "http://127.0.0.1:18081",
			},
		},
	)

	authHandler := NewAuthHandler(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		pluginService,
	)

	var capturedOpenAIBody string
	router := gin.New()
	router.POST("/batch-upload", authHandler.AugmentLegacyBatchUpload)
	router.POST("/find-missing", authHandler.AugmentLegacyFindMissing)
	router.POST("/chat", authHandler.AugmentLegacyChat)
	router.POST("/chat-stream", authHandler.AugmentLegacyChatStream)
	router.POST("/prompt-enhancer", authHandler.AugmentLegacyPromptEnhancer)
	router.POST("/instruction-stream", authHandler.AugmentLegacyInstructionStream)
	router.POST("/smart-paste-stream", authHandler.AugmentLegacySmartPasteStream)
	router.POST("/generate-commit-message-stream", authHandler.AugmentLegacyGenerateCommitMessageStream)
	router.POST("/next_edit_loc", authHandler.AugmentLegacyNextEditLocation)
	router.POST("/next-edit-stream", authHandler.AugmentLegacyNextEditStream)
	router.POST("/remote-agents/list", authHandler.AugmentLegacyListRemoteAgents)
	router.POST("/agents/codebase-retrieval", authHandler.AugmentLegacyCodebaseRetrieval)
	router.GET("/notifications/read", authHandler.AugmentLegacyNotificationsRead)
	router.POST("/notifications/mark-as-read", authHandler.AugmentLegacyNotificationsMarkRead)
	router.GET("/subscription-banner", authHandler.AugmentLegacySubscriptionBanner)
	router.POST("/subscription-banner", authHandler.AugmentLegacySubscriptionBanner)
	router.POST("/agents/list-remote-tools", authHandler.AugmentLegacyListRemoteTools)
	router.POST("/report-error", authHandler.AugmentLegacyJSONAck)
	router.POST("/client-metrics", authHandler.AugmentLegacyJSONAck)
	router.POST("/record-session-events", authHandler.AugmentLegacyJSONAck)
	router.POST("/openai/v1/chat/completions", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		capturedOpenAIBody = string(body)
		c.JSON(http.StatusOK, gin.H{
			"id":      "chatcmpl-compat",
			"object":  "chat.completion",
			"created": 1710000000,
			"model":   "gpt-5.4",
			"choices": []gin.H{
				{
					"index": 0,
					"message": gin.H{
						"role":    "assistant",
						"content": "hello from compat",
					},
					"finish_reason": "stop",
				},
			},
			"usage": gin.H{
				"prompt_tokens":     10,
				"completion_tokens": 5,
				"total_tokens":      15,
				"prompt_tokens_details": gin.H{
					"cached_tokens": 2,
				},
			},
		})
	})

	server := httptest.NewServer(router)
	defer server.Close()

	client := server.Client()
	postJSON := func(path string, body string) *http.Response {
		req, err := http.NewRequest(http.MethodPost, server.URL+path, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+apiKey.Key)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		require.NoError(t, err)
		return resp
	}
	get := func(path string) *http.Response {
		req, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+apiKey.Key)
		resp, err := client.Do(req)
		require.NoError(t, err)
		return resp
	}

	uploadResp := postJSON("/batch-upload", `{"blobs":[{"blob_name":"blob-a","path":"src/main.go","content":"package main\nfunc main(){}\n"}]}`)
	defer uploadResp.Body.Close()
	require.Equal(t, http.StatusOK, uploadResp.StatusCode)
	var uploadBody map[string][]string
	require.NoError(t, json.NewDecoder(uploadResp.Body).Decode(&uploadBody))
	require.Equal(t, []string{"blob-a"}, uploadBody["blob_names"])

	findResp := postJSON("/find-missing", `{"model":"gpt-5.4","mem_object_names":["blob-a","blob-missing"]}`)
	defer findResp.Body.Close()
	require.Equal(t, http.StatusOK, findResp.StatusCode)
	var findBody map[string][]string
	require.NoError(t, json.NewDecoder(findResp.Body).Decode(&findBody))
	require.Equal(t, []string{"blob-missing"}, findBody["unknown_memory_names"])
	require.Equal(t, []string{}, findBody["nonindexed_blob_names"])

	notifyResp := get("/notifications/read")
	defer notifyResp.Body.Close()
	require.Equal(t, http.StatusOK, notifyResp.StatusCode)
	var notifyBody map[string][]any
	require.NoError(t, json.NewDecoder(notifyResp.Body).Decode(&notifyBody))
	require.Equal(t, []any{}, notifyBody["notifications"])

	toolsResp := postJSON("/agents/list-remote-tools", `{"tool_id_list":{"tool_ids":["read-file"]}}`)
	defer toolsResp.Body.Close()
	require.Equal(t, http.StatusOK, toolsResp.StatusCode)
	var toolsBody map[string][]any
	require.NoError(t, json.NewDecoder(toolsResp.Body).Decode(&toolsBody))
	require.Equal(t, []any{}, toolsBody["tools"])

	remoteAgentsResp := postJSON("/remote-agents/list", `{}`)
	defer remoteAgentsResp.Body.Close()
	require.Equal(t, http.StatusOK, remoteAgentsResp.StatusCode)
	var remoteAgentsBody map[string]any
	require.NoError(t, json.NewDecoder(remoteAgentsResp.Body).Decode(&remoteAgentsBody))
	require.Equal(t, []any{}, remoteAgentsBody["remote_agents"])
	require.Equal(t, float64(0), remoteAgentsBody["max_remote_agents"])
	require.Equal(t, float64(0), remoteAgentsBody["max_active_remote_agents"])

	bannerResp := postJSON("/subscription-banner", `{}`)
	defer bannerResp.Body.Close()
	require.Equal(t, http.StatusOK, bannerResp.StatusCode)
	var bannerBody map[string]any
	require.NoError(t, json.NewDecoder(bannerResp.Body).Decode(&bannerBody))
	_, ok := bannerBody["banner"]
	require.True(t, ok)
	require.Nil(t, bannerBody["banner"])

	reportResp := postJSON("/report-error", `{"message":"boom"}`)
	defer reportResp.Body.Close()
	require.Equal(t, http.StatusOK, reportResp.StatusCode)
	require.JSONEq(t, `{}`, readBody(t, reportResp.Body))

	invalidReportReq, err := http.NewRequest(http.MethodPost, server.URL+"/report-error", strings.NewReader(`{"message":"bad-key"}`))
	require.NoError(t, err)
	invalidReportReq.Header.Set("Authorization", "Bearer sk-invalid-runtime")
	invalidReportReq.Header.Set("Content-Type", "application/json")
	invalidReportResp, err := client.Do(invalidReportReq)
	require.NoError(t, err)
	defer invalidReportResp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, invalidReportResp.StatusCode)

	retrievalResp := postJSON("/agents/codebase-retrieval", `{"information_request":"find main entry","blobs":{"checkpoint_id":"","added_blobs":["blob-a"],"deleted_blobs":[]},"dialog":[],"max_output_length":2000}`)
	defer retrievalResp.Body.Close()
	require.Equal(t, http.StatusOK, retrievalResp.StatusCode)
	var retrievalBody map[string]string
	require.NoError(t, json.NewDecoder(retrievalResp.Body).Decode(&retrievalBody))
	require.Contains(t, retrievalBody["formatted_retrieval"], "src/main.go")
	require.Contains(t, retrievalBody["formatted_retrieval"], "find main entry")

	chatResp := postJSON("/chat", `{"model":"gpt-5.4","message":"explain main","blobs":{"checkpoint_id":"","added_blobs":["blob-a"],"deleted_blobs":[]}}`)
	defer chatResp.Body.Close()
	require.Equal(t, http.StatusOK, chatResp.StatusCode)
	var chatBody map[string]any
	require.NoError(t, json.NewDecoder(chatResp.Body).Decode(&chatBody))
	require.Equal(t, "hello from compat", chatBody["text"])
	nodes, ok := chatBody["nodes"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, nodes)
	require.Contains(t, capturedOpenAIBody, "[CODEBASE_RETRIEVAL]")
	require.Contains(t, capturedOpenAIBody, "src/main.go")

	missingChatResp := postJSON("/chat", `{"model":"gpt-5.4","message":"explain missing blob","blobs":{"checkpoint_id":"","added_blobs":["blob-missing"],"deleted_blobs":[]}}`)
	defer missingChatResp.Body.Close()
	require.Equal(t, http.StatusOK, missingChatResp.StatusCode)
	var missingChatBody map[string]any
	require.NoError(t, json.NewDecoder(missingChatResp.Body).Decode(&missingChatBody))
	require.NotContains(t, capturedOpenAIBody, "unknown_blobs:")
	missingUnknown, ok := missingChatBody["unknown_blob_names"].([]any)
	require.True(t, ok)
	require.Equal(t, []any{"blob-missing"}, missingUnknown)

	staleCheckpointResp := postJSON("/chat", `{"model":"gpt-5.4","message":"stale checkpoint","blobs":{"checkpoint_id":"checkpoint-stale","added_blobs":[],"deleted_blobs":[]}}`)
	defer staleCheckpointResp.Body.Close()
	require.Equal(t, http.StatusOK, staleCheckpointResp.StatusCode)
	var staleCheckpointBody map[string]any
	require.NoError(t, json.NewDecoder(staleCheckpointResp.Body).Decode(&staleCheckpointBody))
	require.Equal(t, true, staleCheckpointBody["checkpoint_not_found"])

	streamResp := postJSON("/chat-stream", `{"model":"gpt-5.4","message":"stream main","blobs":{"checkpoint_id":"","added_blobs":["blob-a"],"deleted_blobs":[]}}`)
	defer streamResp.Body.Close()
	require.Equal(t, http.StatusOK, streamResp.StatusCode)
	streamLines := nonEmptyLines(t, streamResp.Body)
	require.Len(t, streamLines, 3)
	var firstChunk map[string]any
	require.NoError(t, json.Unmarshal([]byte(streamLines[0]), &firstChunk))
	require.Equal(t, "hello from compat", firstChunk["text"])
	var finalChunk map[string]any
	require.NoError(t, json.Unmarshal([]byte(streamLines[len(streamLines)-1]), &finalChunk))
	require.Equal(t, float64(1), finalChunk["stop_reason"])

	enhancerResp := postJSON("/prompt-enhancer", `{"model":"gpt-5.4","user_guidelines":"be concise","nodes":[{"id":1,"type":0,"text_node":{"content":"improve this prompt"}}]}`)
	defer enhancerResp.Body.Close()
	require.Equal(t, http.StatusOK, enhancerResp.StatusCode)
	enhancerLines := nonEmptyLines(t, enhancerResp.Body)
	require.Len(t, enhancerLines, 1)
	require.Contains(t, enhancerLines[0], `"text":"hello from compat"`)

	instructionResp := postJSON("/instruction-stream", `{"model":"gpt-5.4","instruction":"rewrite","selected_text":"fmt.Println(1)"}`)
	defer instructionResp.Body.Close()
	require.Equal(t, http.StatusOK, instructionResp.StatusCode)
	instructionLines := nonEmptyLines(t, instructionResp.Body)
	require.Len(t, instructionLines, 1)
	require.Contains(t, instructionLines[0], `"replacement_text":"hello from compat"`)

	nextLocResp := postJSON("/next_edit_loc", `{"instruction":"change file","path":"src/main.go","blobs":{"checkpoint_id":"","added_blobs":["blob-a"],"deleted_blobs":[]}}`)
	defer nextLocResp.Body.Close()
	require.Equal(t, http.StatusOK, nextLocResp.StatusCode)
	var nextLocBody map[string]any
	require.NoError(t, json.NewDecoder(nextLocResp.Body).Decode(&nextLocBody))
	require.Contains(t, nextLocBody, "candidate_locations")

	nextStreamResp := postJSON("/next-edit-stream", `{"model":"gpt-5.4","instruction":"change file","path":"src/main.go","blob_name":"blob-a","blobs":{"checkpoint_id":"","added_blobs":["blob-a"],"deleted_blobs":[]}}`)
	defer nextStreamResp.Body.Close()
	require.Equal(t, http.StatusOK, nextStreamResp.StatusCode)
	nextStreamLines := nonEmptyLines(t, nextStreamResp.Body)
	require.Len(t, nextStreamLines, 1)
	var nextStreamBody map[string]any
	require.NoError(t, json.Unmarshal([]byte(nextStreamLines[0]), &nextStreamBody))
	require.Contains(t, nextStreamBody, "next_edit")
}

func TestAugmentLegacySetLoopbackCodexHeaders(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/openai/v1/chat/completions", nil)
	augmentLegacySetLoopbackCodexHeaders(req)

	require.Equal(t, "codex_cli_rs/0.125.0", req.Header.Get("User-Agent"))
	require.Equal(t, "codex_cli_rs", req.Header.Get("originator"))
	require.Equal(t, "responses=experimental", req.Header.Get("OpenAI-Beta"))
}

func TestAugmentLegacyChatUsesNodeTextAndKeepsUserQuestionSeparateFromRetrieval(t *testing.T) {
	t.Parallel()

	server, apiKey, capturedBody := newAugmentLegacyRuntimeTestServer(t)
	defer server.Close()

	client := server.Client()
	reqBody := `{
		"model":"gpt-5.4",
		"message":"",
		"nodes":[{"id":1,"type":0,"text_node":{"content":"请基于当前仓库做一个简要概览"}}],
		"system_prompt":"你是仓库分析助手",
		"system_prompt_append":"必须基于代码回答",
		"system_prompt_replacements":[{"match":"旧规则","replacement":"新规则"}],
		"blobs":{"checkpoint_id":"","added_blobs":["blob-a"],"deleted_blobs":[]}
	}`
	httpReq, err := http.NewRequest(http.MethodPost, server.URL+"/chat", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var openaiBody map[string]any
	require.NoError(t, json.Unmarshal([]byte(*capturedBody), &openaiBody))
	messages, ok := openaiBody["messages"].([]any)
	require.True(t, ok)
	require.GreaterOrEqual(t, len(messages), 3)

	lastMessage, ok := messages[len(messages)-1].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "user", lastMessage["role"])
	require.Equal(t, "请基于当前仓库做一个简要概览", stringValueOrRawJSON(t, lastMessage["content"]))

	var systemTexts []string
	var retrievalTexts []string
	for _, entry := range messages {
		msg, ok := entry.(map[string]any)
		require.True(t, ok)
		role, _ := msg["role"].(string)
		content := stringValueOrRawJSON(t, msg["content"])
		switch role {
		case "system":
			systemTexts = append(systemTexts, content)
		case "assistant":
			retrievalTexts = append(retrievalTexts, content)
		}
	}
	require.NotEmpty(t, systemTexts)
	require.True(t, strings.Contains(strings.Join(systemTexts, "\n"), "你是仓库分析助手"))
	require.True(t, strings.Contains(strings.Join(systemTexts, "\n"), "必须基于代码回答"))
	require.True(t, strings.Contains(strings.Join(systemTexts, "\n"), "新规则"))
	require.True(t, strings.Contains(strings.Join(retrievalTexts, "\n"), "[CODEBASE_RETRIEVAL]"))
	require.True(t, strings.Contains(strings.Join(retrievalTexts, "\n"), "src/main.go"))
}

func TestAugmentLegacyResolveChatUserInputIgnoresBareContentNodes(t *testing.T) {
	t.Parallel()

	req := augmentLegacyChatRequest{
		Message: "请只回答这个最新问题",
		Nodes: []augmentLegacyChatNode{
			{
				ID:      1,
				Type:    ptrInt(augmentResponseNodeRawResponse),
				Content: "这是旧的 assistant 输出，不应该回灌成新的用户问题",
			},
		},
		RequestNodes: []augmentLegacyChatNode{
			{
				ID:   2,
				Type: ptrInt(augmentRequestNodeText),
				TextNode: &augmentLegacyChatTextNode{
					Content: "并且必须引用真实文件",
				},
			},
		},
	}

	resolved := augmentLegacyResolveChatUserInput(req)
	require.Equal(t, "请只回答这个最新问题\n\n并且必须引用真实文件", resolved.Text)
	require.Equal(t, "explicit_message_plus_request_text_nodes", resolved.Source)
}

func TestAugmentLegacyResolveChatUserInputSkipsNodeFallbackForToolFollowup(t *testing.T) {
	t.Parallel()

	req := augmentLegacyChatRequest{
		Nodes: []augmentLegacyChatNode{
			{
				ID:      1,
				Type:    ptrInt(augmentResponseNodeRawResponse),
				Content: "旧轮次的 assistant 输出，不应变成新的 user turn",
			},
		},
		RequestNodes: []augmentLegacyChatNode{
			{
				ID:   2,
				Type: ptrInt(augmentRequestNodeToolResult),
				ToolResultNode: &augmentLegacyToolResultNode{
					ToolUseID: "codebase-retrieval-1",
					Content:   "[CODEBASE_RETRIEVAL]\nREADME.md",
				},
			},
		},
	}

	resolved := augmentLegacyResolveChatUserInput(req)
	require.Empty(t, resolved.Text)
	require.Equal(t, "tool_result_followup", resolved.Source)
	require.True(t, resolved.HasToolResults)
}

func TestAugmentLegacyResolveChatUserInputPreservesExplicitRequestNodeTextForToolFollowup(t *testing.T) {
	t.Parallel()

	req := augmentLegacyChatRequest{
		Nodes: []augmentLegacyChatNode{
			{
				ID:      1,
				Type:    ptrInt(augmentResponseNodeRawResponse),
				Content: "旧轮次的 assistant 输出，不应变成新的 user turn",
			},
		},
		RequestNodes: []augmentLegacyChatNode{
			{
				ID:   2,
				Type: ptrInt(augmentRequestNodeToolResult),
				ToolResultNode: &augmentLegacyToolResultNode{
					ToolUseID: "codebase-retrieval-1",
					Content:   "[CODEBASE_RETRIEVAL]\nREADME.md",
				},
			},
			{
				ID:   3,
				Type: ptrInt(augmentRequestNodeText),
				TextNode: &augmentLegacyChatTextNode{
					Content: "请继续回答，不要重复问候",
				},
			},
		},
	}

	resolved := augmentLegacyResolveChatUserInput(req)
	require.Equal(t, "请继续回答，不要重复问候", resolved.Text)
	require.Equal(t, "request_text_nodes", resolved.Source)
	require.True(t, resolved.HasToolResults)
}

func TestAugmentLegacyPromptEnhancerUsesNodeText(t *testing.T) {
	t.Parallel()

	server, apiKey, capturedBody := newAugmentLegacyRuntimeTestServer(t)
	defer server.Close()

	client := server.Client()
	reqBody := `{
		"model":"gpt-5.4",
		"nodes":[{"id":1,"type":0,"text_node":{"content":"把这个仓库问题改写成更清晰的 Prompt"}}],
		"user_guidelines":"保留中文"
	}`
	httpReq, err := http.NewRequest(http.MethodPost, server.URL+"/prompt-enhancer", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var openaiBody map[string]any
	require.NoError(t, json.Unmarshal([]byte(*capturedBody), &openaiBody))
	messages, ok := openaiBody["messages"].([]any)
	require.True(t, ok)
	require.Len(t, messages, 1)
	msg, ok := messages[0].(map[string]any)
	require.True(t, ok)
	content := stringValueOrRawJSON(t, msg["content"])
	require.Contains(t, content, "把这个仓库问题改写成更清晰的 Prompt")
	require.Contains(t, content, "保留中文")
}

func TestAugmentLegacyOfficialRequiredRoutesFailClosedWithoutOfficialSession(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	user := &service.User{
		ID:       2,
		Email:    "compat@example.com",
		Username: "compat",
		Role:     service.RoleAdmin,
		Status:   service.StatusActive,
	}
	apiKey := &service.APIKey{
		ID:        2,
		UserID:    user.ID,
		Key:       "sk-compat-runtime",
		Name:      "compat-runtime",
		Status:    service.StatusActive,
		CreatedAt: time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC),
		User:      user,
	}
	group := service.Group{
		ID:                 101,
		Name:               "OpenAI",
		Platform:           service.PlatformOpenAI,
		Status:             service.StatusActive,
		Hydrated:           true,
		DefaultMappedModel: "gpt-5.4",
	}
	markAugmentRuntimeAPIKey(apiKey, &group)

	pluginService := service.NewAugmentPluginService(
		&config.Config{
			Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"},
			Gateway: config.GatewayConfig{
				Augment: config.GatewayAugmentConfig{
					Enabled: true,
				},
			},
		},
		augmentPluginAuthStub{},
		augmentPluginUserStub{user: user},
		augmentPluginAPIKeyStub{
			apiKeyByValue: map[string]*service.APIKey{apiKey.Key: apiKey},
			keysByUser:    map[int64][]service.APIKey{user.ID: {*apiKey}},
			availableByUser: map[int64][]service.Group{
				user.ID: {group},
			},
		},
		augmentPluginSubscriptionStub{},
		augmentPluginSettingStub{
			public: &service.PublicSettings{
				SiteName:   "逐梦站",
				APIBaseURL: "http://127.0.0.1:18081",
			},
		},
	)
	authHandler := NewAuthHandler(
		&config.Config{
			Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"},
			Gateway: config.GatewayConfig{
				Augment: config.GatewayAugmentConfig{
					Enabled: true,
				},
			},
		},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		pluginService,
	)

	router := gin.New()
	router.POST("/prompt-enhancer", authHandler.AugmentLegacyPromptEnhancer)
	router.POST("/agents/codebase-retrieval", authHandler.AugmentLegacyCodebaseRetrieval)

	tests := []struct {
		path string
		body string
	}{
		{
			path: "/prompt-enhancer",
			body: `{"model":"gpt-5.4","nodes":[{"id":1,"type":0,"text_node":{"content":"改写这个 Prompt"}}]}`,
		},
		{
			path: "/agents/codebase-retrieval",
			body: `{"information_request":"查找 chat-stream 链路","blobs":{"checkpoint_id":"","added_blobs":[],"deleted_blobs":[]}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer "+apiKey.Key)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusUnauthorized, rec.Code)
			require.Contains(t, rec.Body.String(), "AUGMENT_OFFICIAL_ROUTE_REQUIRED")
		})
	}
}

func TestAugmentLegacyOfficialRouteDecisionAllowsActivePlatformPoolSession(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	now := time.Now().UTC()
	poolService := service.NewAugmentOfficialPoolSessionService(
		&handlerOfficialPoolSessionStoreStub{
			adminViews: []service.AugmentOfficialPoolStoredAdminView{
				{
					ID:                   1,
					Source:               "official_quick_login",
					TenantOrigin:         "https://d12.api.augmentcode.com",
					Scopes:               []string{"email"},
					ExpiresAt:            handlerTimePtr(now.Add(time.Hour)),
					Status:               service.AugmentOfficialPoolSessionStatusActive,
					Fingerprint:          "fp-prefix-full",
					CreatedAt:            now.Add(-time.Hour),
					UpdatedAt:            now,
					LastSuccessAt:        handlerTimePtr(now.Add(-time.Minute)),
					HealthScore:          100,
					HasCredentialPayload: true,
				},
			},
		},
		newHandlerTestAugmentSessionVaultCipher(t),
		"bind-secret",
	)

	authHandler := NewAuthHandler(
		&config.Config{
			Gateway: config.GatewayConfig{
				Augment: config.GatewayAugmentConfig{Enabled: true},
			},
		},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		poolService,
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/prompt-enhancer", nil)

	resolved, err := poolService.ResolveRouteSession(context.Background(), service.AugmentOfficialPoolRouteSessionLookupInput{})
	require.NoError(t, err)
	require.NotNil(t, resolved)
	require.Equal(t, service.AugmentOfficialPoolSessionStatusActive, resolved.Status)

	decision := authHandler.augmentLegacyOfficialRouteDecision(c, &service.AugmentPluginPrincipal{
		User: &service.User{ID: 7, Status: service.StatusActive},
	}, "/prompt-enhancer")

	require.Equal(t, service.AugmentOfficialSessionStatusActive, decision.OfficialSessionStatus)
	require.Equal(t, service.AugmentOfficialRouteResultAllowed, decision.OfficialRouteResult)
	require.True(t, decision.AllowLocalHandler)
}

func TestAugmentLegacyPromptEnhancerCurrentShapeContract(t *testing.T) {
	t.Parallel()

	server, apiKey, recorder := newAugmentLegacyAuxiliaryContractTestServer(t)
	defer server.Close()

	reqBody := `{
		"nodes": [{
			"type": 0,
			"text": "make this request production-safe",
			"text_node": {"content": "make this request production-safe"}
		}],
		"chat_history": [],
		"workspace_file_chunks": [],
		"incorporated_external_sources": [],
		"conversation_id": "conv-contract-1",
		"model": "gpt-5.4"
	}`
	requireAugmentEndpointFixtureHasNoSecrets(t, reqBody)

	resp := postAugmentContractJSON(t, server, apiKey, "/prompt-enhancer", reqBody)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	chunks := decodeAugmentContractNDJSON(t, resp.Body)
	require.Len(t, chunks, 1)
	finalChunk := chunks[0]
	require.Equal(t, "contract upstream text", finalChunk["text"])
	require.Equal(t, false, finalChunk["checkpoint_not_found"])
	require.Equal(t, []any{}, finalChunk["unknown_blob_names"])
	require.Equal(t, []any{}, finalChunk["workspace_file_chunks"])
	require.Equal(t, []any{}, finalChunk["nodes"])

	_, hasIncorporatedExternalSources := finalChunk["incorporated_external_sources"]
	require.False(t, hasIncorporatedExternalSources, "current prompt-enhancer envelope does not emit incorporated_external_sources")
	requireAugmentContractNoStreamSequencing(t, finalChunk)

	calls := recorder.Calls()
	require.Len(t, calls, 1)
	require.Empty(t, calls[0].Cookie)
	require.Equal(t, "Bearer "+apiKey, calls[0].Authorization)
	openaiBody := decodeAugmentContractObject(t, calls[0].Body)
	require.Equal(t, "gpt-5.4", openaiBody["model"])
	require.Equal(t, false, openaiBody["stream"])
	messages, ok := openaiBody["messages"].([]any)
	require.True(t, ok)
	require.Len(t, messages, 1)
	message, ok := messages[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "user", message["role"])
	require.Contains(t, stringValueOrRawJSON(t, message["content"]), "make this request production-safe")
}

func TestAugmentLegacyOfficialRouteDecisionFallsBackToPoolWhenUserBoundSessionIsUnusable(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	now := time.Now().UTC()
	cipher := newHandlerTestAugmentSessionVaultCipher(t)
	officialService := service.NewAugmentOfficialSessionService(
		&handlerOfficialSessionStoreStub{
			publicView: &service.AugmentOfficialSessionStoredPublicView{
				UserID:       7,
				Source:       "official_quick_login",
				TenantOrigin: "https://expired.augment.local",
				Scopes:       []string{"augment:session"},
				ExpiresAt:    handlerTimePtr(now.Add(-time.Hour)),
				Status:       "active",
				Fingerprint:  "expiredfp1234567890",
				CreatedAt:    now.Add(-2 * time.Hour),
				UpdatedAt:    now.Add(-2 * time.Hour),
			},
		},
		cipher,
		"bind-secret",
	)
	poolService := service.NewAugmentOfficialPoolSessionService(
		&handlerOfficialPoolSessionStoreStub{
			credentialRow: &service.AugmentOfficialPoolStoredCredentialRow{
				ID:                         11,
				Source:                     "official_quick_login",
				TenantOrigin:               "https://pool.augment.local",
				Scopes:                     []string{"email"},
				Status:                     service.AugmentOfficialPoolSessionStatusActive,
				EncryptedCredentialPayload: mustEncryptHandlerOfficialPayload(t, cipher, map[string]string{"access_token": "pool-token"}),
				CredentialSchemaVersion:    1,
				KeyVersion:                 "local",
				Fingerprint:                "pool-fingerprint",
				HealthScore:                100,
			},
			adminViews: []service.AugmentOfficialPoolStoredAdminView{{
				ID:                   11,
				Source:               "official_quick_login",
				TenantOrigin:         "https://pool.augment.local",
				Scopes:               []string{"email"},
				ExpiresAt:            handlerTimePtr(now.Add(time.Hour)),
				Status:               service.AugmentOfficialPoolSessionStatusActive,
				Fingerprint:          "pool-fingerprint",
				CreatedAt:            now.Add(-time.Hour),
				UpdatedAt:            now,
				LastSuccessAt:        handlerTimePtr(now),
				HealthScore:          100,
				HasCredentialPayload: true,
			}},
		},
		cipher,
		"bind-secret",
	)

	authHandler := NewAuthHandler(
		&config.Config{Gateway: config.GatewayConfig{Augment: config.GatewayAugmentConfig{Enabled: true}}},
		nil, nil, nil, nil, nil, nil,
		officialService,
		poolService,
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/prompt-enhancer", nil)

	decision := authHandler.augmentLegacyOfficialRouteDecision(c, &service.AugmentPluginPrincipal{
		User: &service.User{ID: 7, Status: service.StatusActive},
	}, "/prompt-enhancer")

	require.Equal(t, service.AugmentOfficialSessionStatusActive, decision.OfficialSessionStatus)
	require.Equal(t, service.AugmentOfficialRouteResultAllowed, decision.OfficialRouteResult)
	require.True(t, decision.AllowLocalHandler)
}

func TestAugmentLegacyPromptEnhancerBareTextNodeCurrentShapeContract(t *testing.T) {
	t.Parallel()

	server, apiKey, recorder := newAugmentLegacyAuxiliaryContractTestServer(t)
	defer server.Close()

	reqBody := `{
		"nodes": [{"type": 0, "text": "make this request production-safe"}],
		"chat_history": [],
		"workspace_file_chunks": [],
		"incorporated_external_sources": [],
		"conversation_id": "conv-contract-1",
		"model": "gpt-5.4"
	}`
	requireAugmentEndpointFixtureHasNoSecrets(t, reqBody)

	resp := postAugmentContractJSON(t, server, apiKey, "/prompt-enhancer", reqBody)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.JSONEq(t, `{"code":"EMPTY_USER_INPUT","message":"no usable user input was found in the augment request"}`, readBody(t, resp.Body))
	require.Empty(t, recorder.Calls(), "bare node.text is not decoded by the current prompt-enhancer path")
}

func TestAugmentLegacyInstructionStreamCurrentShapeContract(t *testing.T) {
	t.Parallel()

	server, apiKey, recorder := newAugmentLegacyAuxiliaryContractTestServer(t)
	defer server.Close()

	reqBody := `{
		"model": "gpt-5.4",
		"instruction": "rewrite for nil safety",
		"selected_text": "fmt.Println(value)"
	}`
	requireAugmentEndpointFixtureHasNoSecrets(t, reqBody)

	resp := postAugmentContractJSON(t, server, apiKey, "/instruction-stream", reqBody)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	chunks := decodeAugmentContractNDJSON(t, resp.Body)
	require.Len(t, chunks, 1)
	chunk := chunks[0]
	require.Equal(t, "contract upstream text", chunk["text"])
	require.Equal(t, "contract upstream text", chunk["replacement_text"])
	require.Equal(t, "fmt.Println(value)", chunk["replacement_old_text"])
	require.Equal(t, float64(1), chunk["replacement_start_line"])
	require.Equal(t, float64(1), chunk["replacement_end_line"])
	require.Equal(t, []any{}, chunk["unknown_blob_names"])
	require.Equal(t, false, chunk["checkpoint_not_found"])
	requireAugmentContractNoStreamSequencing(t, chunk)

	calls := recorder.Calls()
	require.Len(t, calls, 1)
	openaiBody := decodeAugmentContractObject(t, calls[0].Body)
	require.Equal(t, "gpt-5.4", openaiBody["model"])
	require.Equal(t, false, openaiBody["stream"])
	require.Contains(t, calls[0].Body, "rewrite for nil safety")
	require.Contains(t, calls[0].Body, "fmt.Println(value)")
}

func TestAugmentLegacySmartPasteCurrentShapeContract(t *testing.T) {
	t.Parallel()

	server, apiKey, recorder := newAugmentLegacyAuxiliaryContractTestServer(t)
	defer server.Close()

	reqBody := `{
		"model": "gpt-5.4",
		"instruction": "adapt pasted code to this file",
		"selected_text": "oldCall()"
	}`
	requireAugmentEndpointFixtureHasNoSecrets(t, reqBody)

	resp := postAugmentContractJSON(t, server, apiKey, "/smart-paste-stream", reqBody)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	chunks := decodeAugmentContractNDJSON(t, resp.Body)
	require.Len(t, chunks, 1)
	chunk := chunks[0]
	require.Equal(t, "contract upstream text", chunk["text"])
	require.Equal(t, "contract upstream text", chunk["replacement_text"])
	require.Equal(t, "oldCall()", chunk["replacement_old_text"])
	require.Equal(t, []any{}, chunk["unknown_blob_names"])
	require.Equal(t, false, chunk["checkpoint_not_found"])
	requireAugmentContractNoStreamSequencing(t, chunk)

	calls := recorder.Calls()
	require.Len(t, calls, 1)
	require.Contains(t, calls[0].Body, "adapt pasted code to this file")
	require.Contains(t, calls[0].Body, "oldCall()")
}

func TestAugmentLegacyGenerateCommitCurrentShapeContract(t *testing.T) {
	t.Parallel()

	server, apiKey, recorder := newAugmentLegacyAuxiliaryContractTestServer(t)
	defer server.Close()

	reqBody := `{
		"changed_file_stats": {},
		"diff": "diff --git a/main.go b/main.go\n+fmt.Println(\"safe\")",
		"generatedCommitMessageSubrequest": {
			"relevant_commit_messages": ["fix runtime panic"],
			"example_commit_messages": ["test: lock contracts"]
		}
	}`
	requireAugmentEndpointFixtureHasNoSecrets(t, reqBody)

	resp := postAugmentContractJSON(t, server, apiKey, "/generate-commit-message-stream", reqBody)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	chunks := decodeAugmentContractNDJSON(t, resp.Body)
	require.Len(t, chunks, 1)
	chunk := chunks[0]
	require.Equal(t, "contract upstream text", chunk["text"])
	requireAugmentContractNoStreamSequencing(t, chunk)

	calls := recorder.Calls()
	require.Len(t, calls, 1)
	openaiBody := decodeAugmentContractObject(t, calls[0].Body)
	require.Equal(t, "gpt-5.4", openaiBody["model"])
	require.Contains(t, calls[0].Body, "fix runtime panic")
	require.Contains(t, calls[0].Body, "test: lock contracts")
}

func TestAugmentLegacyNextEditCurrentShapeContract(t *testing.T) {
	t.Parallel()

	server, apiKey, recorder := newAugmentLegacyAuxiliaryContractTestServer(t)
	defer server.Close()

	locationReqBody := `{
		"instruction": "change file",
		"path": "src/main.go",
		"blobs": {"checkpoint_id": "", "added_blobs": ["blob-a"], "deleted_blobs": []},
		"recent_changes": [],
		"diagnostics": [],
		"num_results": 1,
		"is_single_file": true
	}`
	requireAugmentEndpointFixtureHasNoSecrets(t, locationReqBody)

	locationResp := postAugmentContractJSON(t, server, apiKey, "/next_edit_loc", locationReqBody)
	defer locationResp.Body.Close()
	require.Equal(t, http.StatusOK, locationResp.StatusCode)

	locationBody := decodeAugmentContractObjectFromReader(t, locationResp.Body)
	require.Equal(t, []any{}, locationBody["unknown_blob_names"])
	require.Equal(t, false, locationBody["checkpoint_not_found"])
	require.Equal(t, []any{}, locationBody["critical_errors"])
	candidateLocations, ok := locationBody["candidate_locations"].([]any)
	require.True(t, ok)
	require.Len(t, candidateLocations, 1)
	candidate, ok := candidateLocations[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, float64(1), candidate["score"])
	require.Equal(t, "heuristic", candidate["debug_info"])
	item, ok := candidate["item"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "src/main.go", item["path"])
	require.Empty(t, recorder.Calls(), "next_edit_loc is satisfied by the current local handler without an upstream model call")

	streamReqBody := `{
		"model": "gpt-5.4",
		"instruction": "change file",
		"path": "src/main.go",
		"blob_name": "blob-a",
		"blobs": {"checkpoint_id": "", "added_blobs": ["blob-a"], "deleted_blobs": []},
		"recent_changes": [],
		"diagnostics": [],
		"client_created_at": "2026-05-06T00:00:00Z"
	}`
	requireAugmentEndpointFixtureHasNoSecrets(t, streamReqBody)

	streamResp := postAugmentContractJSON(t, server, apiKey, "/next-edit-stream", streamReqBody)
	defer streamResp.Body.Close()
	require.Equal(t, http.StatusOK, streamResp.StatusCode)

	streamChunks := decodeAugmentContractNDJSON(t, streamResp.Body)
	require.Len(t, streamChunks, 1)
	streamChunk := streamChunks[0]
	require.Equal(t, []any{}, streamChunk["unknown_blob_names"])
	require.Equal(t, false, streamChunk["checkpoint_not_found"])
	requireAugmentContractNoStreamSequencing(t, streamChunk)
	nextEdit, ok := streamChunk["next_edit"].(map[string]any)
	require.True(t, ok)
	require.NotEmpty(t, nextEdit["suggestion_id"])
	require.Equal(t, "src/main.go", nextEdit["path"])
	require.Equal(t, "blob-a", nextEdit["blob_name"])
	require.Equal(t, "package main\nfunc main(){}\n", nextEdit["existing_code"])
	require.Equal(t, "contract upstream text", nextEdit["suggested_code"])
	require.Equal(t, float64(1), nextEdit["editing_score"])
	require.Equal(t, float64(1), nextEdit["localization_score"])
	require.Equal(t, "compat generated suggestion", nextEdit["change_description"])

	calls := recorder.Calls()
	require.Len(t, calls, 1, "next-edit-stream currently uses the legacy simple-text loopback path, not a future AugmentGatewayRouter mock")
	require.Contains(t, calls[0].Body, "Generate the full suggested code")
	require.Contains(t, calls[0].Body, "package main")
}

func TestAugmentGatewayAuxiliaryEndpointsRouteThroughModelPool(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		path         string
		body         string
		wantModel    string
		wantResponse func(t *testing.T, chunk map[string]any)
		wantDeepSeek bool
	}{
		{
			name:      "prompt enhancer openai",
			path:      "/prompt-enhancer",
			wantModel: "gpt-5.5",
			body: `{
				"model":"gpt-5.5",
				"nodes":[{"id":1,"type":0,"text_node":{"content":"make this request production-safe"}}],
				"chat_history":[],
				"workspace_file_chunks":[],
				"incorporated_external_sources":[],
				"conversation_id":"conv-aux-1"
			}`,
			wantResponse: func(t *testing.T, chunk map[string]any) {
				t.Helper()
				require.Equal(t, "gateway-prompt enhancer openai", chunk["text"])
				require.Equal(t, []any{}, chunk["workspace_file_chunks"])
				require.Equal(t, []any{}, chunk["nodes"])
				require.NotContains(t, chunk, "incorporated_external_sources")
			},
		},
		{
			name:         "prompt enhancer deepseek",
			path:         "/prompt-enhancer",
			wantModel:    "deepseek-v4-pro",
			wantDeepSeek: true,
			body: `{
				"model":"deepseek-v4-pro",
				"nodes":[{"id":1,"type":0,"text_node":{"content":"make this request production-safe"}}],
				"chat_history":[],
				"workspace_file_chunks":[],
				"incorporated_external_sources":[],
				"conversation_id":"conv-aux-2"
			}`,
			wantResponse: func(t *testing.T, chunk map[string]any) {
				t.Helper()
				require.Equal(t, "gateway-prompt enhancer deepseek", chunk["text"])
				require.Equal(t, []any{}, chunk["workspace_file_chunks"])
				require.Equal(t, []any{}, chunk["nodes"])
				require.NotContains(t, chunk, "incorporated_external_sources")
			},
		},
		{
			name:      "instruction stream mini",
			path:      "/instruction-stream",
			wantModel: "gpt-5.4-mini",
			body: `{
				"model":"gpt-5.4-mini",
				"instruction":"rewrite for nil safety",
				"selected_text":"fmt.Println(value)"
			}`,
			wantResponse: func(t *testing.T, chunk map[string]any) {
				t.Helper()
				require.Equal(t, "gateway-instruction stream mini", chunk["text"])
				require.Equal(t, "gateway-instruction stream mini", chunk["replacement_text"])
				require.Equal(t, "fmt.Println(value)", chunk["replacement_old_text"])
				require.Equal(t, float64(1), chunk["replacement_start_line"])
				require.Equal(t, float64(1), chunk["replacement_end_line"])
				require.Equal(t, []any{}, chunk["unknown_blob_names"])
				require.Equal(t, false, chunk["checkpoint_not_found"])
			},
		},
		{
			name:      "smart paste mini",
			path:      "/smart-paste-stream",
			wantModel: "gpt-5.4-mini",
			body: `{
				"model":"gpt-5.4-mini",
				"instruction":"adapt pasted code to this file",
				"selected_text":"oldCall()"
			}`,
			wantResponse: func(t *testing.T, chunk map[string]any) {
				t.Helper()
				require.Equal(t, "gateway-smart paste mini", chunk["text"])
				require.Equal(t, "gateway-smart paste mini", chunk["replacement_text"])
				require.Equal(t, "oldCall()", chunk["replacement_old_text"])
				require.Equal(t, []any{}, chunk["unknown_blob_names"])
				require.Equal(t, false, chunk["checkpoint_not_found"])
			},
		},
		{
			name:      "generate commit default",
			path:      "/generate-commit-message-stream",
			wantModel: "gpt-5.4",
			body: `{
				"changed_file_stats": {},
				"diff": "diff --git a/main.go b/main.go\n+fmt.Println(\"safe\")",
				"generatedCommitMessageSubrequest": {
					"relevant_commit_messages": ["fix runtime panic"],
					"example_commit_messages": ["test: lock contracts"]
				}
			}`,
			wantResponse: func(t *testing.T, chunk map[string]any) {
				t.Helper()
				require.Equal(t, "gateway-generate commit default", chunk["text"])
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			executor := &augmentGatewayRouteFakeExecutor{
				completeResult: service.AugmentGatewayProviderResult{Text: "gateway-" + tc.name},
			}
			server, apiKey, loopbackCalls := newAugmentGatewayRuntimeTestServer(t, executor)
			defer server.Close()

			resp := postAugmentGatewayRuntimeJSON(t, server, apiKey, tc.path, tc.body)
			defer resp.Body.Close()
			require.Equal(t, http.StatusOK, resp.StatusCode)

			chunks := decodeAugmentContractNDJSON(t, resp.Body)
			require.Len(t, chunks, 1)
			tc.wantResponse(t, chunks[0])

			calls := executor.CompleteRequests()
			require.Len(t, calls, 1)
			require.Equal(t, tc.wantModel, calls[0].Model.ID)
			require.Equal(t, tc.wantModel, calls[0].RawBody["model"])
			require.Equal(t, false, calls[0].RawBody["stream"])
			if tc.wantDeepSeek {
				require.Equal(t, map[string]any{"type": "enabled"}, calls[0].RawBody["thinking"])
				require.Equal(t, "max", calls[0].RawBody["reasoning_effort"])
				require.NotContains(t, calls[0].RawBody, "tool_choice")
			}
			require.Equal(t, 0, *loopbackCalls)
		})
	}
}

func TestAugmentGatewayAuxiliaryUnknownModelReturnsUnavailable(t *testing.T) {
	t.Parallel()

	executor := &augmentGatewayRouteFakeExecutor{}
	server, apiKey, loopbackCalls := newAugmentGatewayRuntimeTestServer(t, executor)
	defer server.Close()

	resp := postAugmentGatewayRuntimeJSON(t, server, apiKey, "/prompt-enhancer", `{
		"model":"unknown-model",
		"nodes":[{"id":1,"type":0,"text_node":{"content":"make this request production-safe"}}],
		"chat_history":[],
		"workspace_file_chunks":[],
		"incorporated_external_sources":[],
		"conversation_id":"conv-aux-unknown"
	}`)
	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "application/x-ndjson")
	body := readBody(t, resp.Body)
	require.Contains(t, body, "AUGMENT_GATEWAY_MODEL_UNAVAILABLE")
	var chunk map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(body)), &chunk))
	require.Equal(t, "AUGMENT_GATEWAY_MODEL_UNAVAILABLE", chunk["code"])
	require.NotEmpty(t, chunk["text"])
	require.Empty(t, executor.CompleteRequests())
	require.Equal(t, 0, *loopbackCalls)
}

func TestAugmentGatewayClaudeAndGeminiEnabledRouteThroughProvider(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		modelID  string
		provider service.AugmentGatewayProvider
	}{
		{name: "claude", modelID: "claude-sonnet-4-5", provider: service.AugmentGatewayProviderAnthropic},
		{name: "gemini", modelID: "gemini-2.5-pro", provider: service.AugmentGatewayProviderGemini},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			executor := &augmentGatewayRouteFakeExecutor{
				completeResult: service.AugmentGatewayProviderResult{Text: "gateway-" + tc.name},
			}
			server, apiKey, loopbackCalls := newAugmentGatewayRuntimeTestServerWithConfig(t, executor, config.GatewayAugmentConfig{
				Enabled: true,
				EnabledModels: []string{
					"gpt-5.4",
					"gpt-5.5",
					"gpt-5.4-mini",
					"deepseek-v4-pro",
					"deepseek-v4-flash",
					"claude-sonnet-4-5",
					"gemini-2.5-pro",
				},
				ProviderGroups: config.GatewayAugmentProviderGroupsConfig{
					OpenAI:    1001,
					DeepSeek:  1002,
					Anthropic: 1003,
					Gemini:    1004,
				},
			})
			defer server.Close()

			resp := postAugmentGatewayRuntimeJSON(t, server, apiKey, "/chat", `{
				"model":"`+tc.modelID+`",
				"message":"route through gateway",
				"conversation_id":"conv-`+tc.name+`"
			}`)
			defer resp.Body.Close()
			require.Equal(t, http.StatusOK, resp.StatusCode)

			body := decodeAugmentContractObjectFromReader(t, resp.Body)
			require.Equal(t, "gateway-"+tc.name, body["text"])

			calls := executor.CompleteRequests()
			require.Len(t, calls, 1)
			require.Equal(t, tc.modelID, calls[0].Model.ID)
			require.Equal(t, tc.provider, calls[0].Provider)
			require.Equal(t, 0, *loopbackCalls)
		})
	}
}

func TestAugmentGatewayNextEditEndpointsBypassGateway(t *testing.T) {
	t.Parallel()

	executor := &augmentGatewayRouteFakeExecutor{}
	server, apiKey, loopbackCalls := newAugmentGatewayRuntimeTestServer(t, executor)
	defer server.Close()

	nextEditLocBody := `{
		"instruction":"change file",
		"path":"src/main.go",
		"blobs":{"checkpoint_id":"","added_blobs":["blob-a"],"deleted_blobs":[]},
		"recent_changes":[],
		"diagnostics":[],
		"num_results":1,
		"is_single_file":true
	}`
	resp := postAugmentGatewayRuntimeJSON(t, server, apiKey, "/next_edit_loc", nextEditLocBody)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Empty(t, executor.CompleteRequests())
	require.Empty(t, executor.StreamRequests())

	nextEditStreamBody := `{
		"model":"gpt-5.4",
		"instruction":"change file",
		"path":"src/main.go",
		"blob_name":"blob-a",
		"blobs":{"checkpoint_id":"","added_blobs":["blob-a"],"deleted_blobs":[]},
		"recent_changes":[],
		"diagnostics":[],
		"client_created_at":"2026-05-06T00:00:00Z"
	}`
	resp = postAugmentGatewayRuntimeJSON(t, server, apiKey, "/next-edit-stream", nextEditStreamBody)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	chunks := decodeAugmentContractNDJSON(t, resp.Body)
	require.Len(t, chunks, 1)
	nextEditChunk := chunks[0]
	nextEdit, ok := nextEditChunk["next_edit"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "local loopback text", nextEdit["suggested_code"])
	require.Equal(t, "src/main.go", nextEdit["path"])
	require.Equal(t, "blob-a", nextEdit["blob_name"])
	require.Equal(t, []any{}, nextEditChunk["unknown_blob_names"])
	require.Equal(t, false, nextEditChunk["checkpoint_not_found"])
	require.Empty(t, executor.CompleteRequests())
	require.Empty(t, executor.StreamRequests())
	require.Equal(t, 1, *loopbackCalls)
}

func TestAugmentLegacyFinalEnvelopeCaptureWritesPrePostSummaryWithoutRaw(t *testing.T) {
	server, apiKey, _ := newAugmentLegacyRuntimeTestServer(t)
	defer server.Close()

	captureDir := filepath.Join(t.TempDir(), "captures", "context-engine-envelope")
	t.Setenv("AUGMENT_CAPTURE_CONTEXT_ENGINE_ENVELOPE", "1")
	t.Setenv("AUGMENT_CAPTURE_CONTEXT_ENGINE_FINAL", "1")
	t.Setenv("AUGMENT_CAPTURE_CONTEXT_ENGINE_RAW", "")
	t.Setenv("AUGMENT_CAPTURE_CONTEXT_ENGINE_CASE_ID", "CE-A-P1B-TEST-PREPOST")
	t.Setenv("AUGMENT_CAPTURE_CONTEXT_ENGINE_DIR", captureDir)

	client := server.Client()
	reqBody := `{
		"model":"gpt-5.4",
		"message":"[CE-A-P1B-TEST-PREPOST] 请总结当前仓库结构",
		"blobs":{"checkpoint_id":"","added_blobs":["blob-a"],"deleted_blobs":[]}
	}`
	httpReq, err := http.NewRequest(http.MethodPost, server.URL+"/chat-stream", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NotEmpty(t, nonEmptyLines(t, resp.Body))

	rows := readAugmentCaptureSummaryRows(t, captureDir)
	require.Len(t, rows, 1)
	row := rows[0]
	require.Equal(t, "chat-stream", row["endpoint"])
	require.Equal(t, "local_gateway", row["route"])
	require.Equal(t, "final_envelope", row["reason"])
	require.Equal(t, "pre_post", row["final_capture_stage"])
	require.Equal(t, true, row["final_envelope_capture_enabled"])
	require.Equal(t, true, row["final_message_array_present"])
	require.Equal(t, false, row["has_tool_results"])
	require.Equal(t, "explicit_message", row["resolved_user_input_source"])
	require.Equal(t, false, row["final_contains_information_request"])
	require.Equal(t, float64(1), row["added_blobs_count"])
	require.Nil(t, row["raw_request_path"])
	require.Nil(t, row["raw_response_path"])
}

func TestAugmentLegacyCaptureCaseIDFromChatRequest(t *testing.T) {
	req := augmentLegacyChatRequest{
		Message: "[CE-A-Q1-PATH-001] 请检索 route 注册。",
	}

	require.Equal(t, "CE-A-Q1-PATH-001", augmentLegacyCaptureCaseIDFromChatRequest(req))
}

func TestAugmentLegacyFinalEnvelopeCapturePrefersIngressCaseIDOverFallbackEnv(t *testing.T) {
	server, apiKey, _ := newAugmentLegacyRuntimeTestServer(t)
	defer server.Close()

	captureDir := filepath.Join(t.TempDir(), "captures", "context-engine-envelope")
	t.Setenv("AUGMENT_CAPTURE_CONTEXT_ENGINE_ENVELOPE", "1")
	t.Setenv("AUGMENT_CAPTURE_CONTEXT_ENGINE_FINAL", "1")
	t.Setenv("AUGMENT_CAPTURE_CONTEXT_ENGINE_RAW", "1")
	t.Setenv("AUGMENT_CAPTURE_CONTEXT_ENGINE_CASE_ID", "CE-A-Q1-FALLBACK")
	t.Setenv("AUGMENT_CAPTURE_CONTEXT_ENGINE_DIR", captureDir)

	client := server.Client()
	reqBody := `{
		"model":"gpt-5.4",
		"message":"[CE-A-Q1-PATH-001] 请检索 route 注册。",
		"blobs":{"checkpoint_id":"","added_blobs":["blob-a"],"deleted_blobs":[]}
	}`
	httpReq, err := http.NewRequest(http.MethodPost, server.URL+"/chat-stream", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NotEmpty(t, nonEmptyLines(t, resp.Body))

	rows := readAugmentCaptureSummaryRows(t, captureDir)
	require.Len(t, rows, 1)
	row := rows[0]
	require.Equal(t, "CE-A-Q1-PATH-001", row["case_id"])
	rawRequestPath, ok := row["raw_request_path"].(string)
	require.True(t, ok)
	require.Contains(t, rawRequestPath, "CE-A-Q1-PATH-001")
	require.Nil(t, row["raw_response_path"])
}

func TestAugmentLegacyFinalEnvelopeCaptureKeepsToolResultOnlyChatStream(t *testing.T) {
	server, apiKey, _ := newAugmentLegacyRuntimeTestServer(t)
	defer server.Close()

	captureDir := filepath.Join(t.TempDir(), "captures", "context-engine-envelope")
	t.Setenv("AUGMENT_CAPTURE_CONTEXT_ENGINE_ENVELOPE", "1")
	t.Setenv("AUGMENT_CAPTURE_CONTEXT_ENGINE_FINAL", "1")
	t.Setenv("AUGMENT_CAPTURE_CONTEXT_ENGINE_RAW", "1")
	t.Setenv("AUGMENT_CAPTURE_CONTEXT_ENGINE_CASE_ID", "CE-A-P1B-TEST-TOOL")
	t.Setenv("AUGMENT_CAPTURE_CONTEXT_ENGINE_DIR", captureDir)

	client := server.Client()
	reqBody := `{
		"model":"gpt-5.4",
		"requestNodes":[{
			"id":1,
			"type":1,
			"toolResultNode":{
				"toolUseId":"codebase-retrieval-1",
				"content":"[CODEBASE_RETRIEVAL]\nbackend/internal/server/routes/gateway.go"
			}
		}],
		"blobs":{"checkpoint_id":"","added_blobs":[],"deleted_blobs":[]}
	}`
	httpReq, err := http.NewRequest(http.MethodPost, server.URL+"/chat-stream", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NotEmpty(t, nonEmptyLines(t, resp.Body))

	rows := readAugmentCaptureSummaryRows(t, captureDir)
	require.Len(t, rows, 1)
	row := rows[0]
	require.Equal(t, "tool_result_followup", row["resolved_user_input_source"])
	require.Equal(t, true, row["has_tool_results"])
	require.Equal(t, true, row["final_message_array_present"])
	require.Equal(t, float64(1), row["final_tool_result_count"])
	require.Equal(t, true, row["final_contains_codebase_retrieval_marker"])
	require.Contains(t, row["final_codebase_retrieval_marker_roles"], "tool")
}

func TestAugmentLegacyPromptEnhancerDoesNotWriteFinalEnvelopeRow(t *testing.T) {
	server, apiKey, _ := newAugmentLegacyRuntimeTestServer(t)
	defer server.Close()

	captureDir := filepath.Join(t.TempDir(), "captures", "context-engine-envelope")
	t.Setenv("AUGMENT_CAPTURE_CONTEXT_ENGINE_ENVELOPE", "1")
	t.Setenv("AUGMENT_CAPTURE_CONTEXT_ENGINE_FINAL", "1")
	t.Setenv("AUGMENT_CAPTURE_CONTEXT_ENGINE_RAW", "1")
	t.Setenv("AUGMENT_CAPTURE_CONTEXT_ENGINE_CASE_ID", "CE-A-P1B-TEST-PROMPT")
	t.Setenv("AUGMENT_CAPTURE_CONTEXT_ENGINE_DIR", captureDir)

	client := server.Client()
	reqBody := `{
		"model":"gpt-5.4",
		"nodes":[{"id":1,"type":0,"text_node":{"content":"把这个仓库问题改写成更清晰的 Prompt"}}],
		"user_guidelines":"保留中文"
	}`
	httpReq, err := http.NewRequest(http.MethodPost, server.URL+"/prompt-enhancer", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	_, err = os.Stat(filepath.Join(captureDir, "safe-summary.jsonl"))
	require.True(t, os.IsNotExist(err), "prompt-enhancer must not emit a final-envelope row")
}

func TestAugmentLegacyChatStreamAcceptsPromptAndCamelCaseRequestNodes(t *testing.T) {
	t.Parallel()

	server, apiKey, capturedBody := newAugmentLegacyRuntimeTestServer(t)
	defer server.Close()

	client := server.Client()
	reqBody := `{
		"model":"gpt-5.4",
		"prompt":"请总结当前仓库结构",
		"requestNodes":[{"id":1,"type":0,"textNode":{"content":"必须引用真实文件"}}],
		"userGuidelines":"中文回答",
		"workspaceGuidelines":"必须引用真实代码",
		"blobs":{"checkpoint_id":"","added_blobs":["blob-a"],"deleted_blobs":[]}
	}`
	httpReq, err := http.NewRequest(http.MethodPost, server.URL+"/chat-stream", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	lines := nonEmptyLines(t, resp.Body)
	require.NotEmpty(t, lines)

	var openaiBody map[string]any
	require.NoError(t, json.Unmarshal([]byte(*capturedBody), &openaiBody))
	messages, ok := openaiBody["messages"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, messages)

	var systemContents []string
	var userContents []string
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		require.True(t, ok)
		content := stringValueOrRawJSON(t, msg["content"])
		switch msg["role"] {
		case "system":
			systemContents = append(systemContents, content)
		case "user":
			userContents = append(userContents, content)
		}
	}

	require.NotEmpty(t, systemContents)
	require.NotEmpty(t, userContents)
	require.Contains(t, strings.Join(systemContents, "\n"), "中文回答")
	require.Contains(t, strings.Join(systemContents, "\n"), "必须引用真实代码")
	require.Contains(t, strings.Join(userContents, "\n"), "请总结当前仓库结构")
	require.Contains(t, strings.Join(userContents, "\n"), "必须引用真实文件")
}

func TestAugmentLegacyChatStreamFallsBackToCompatDefaultModelWhenModelMissing(t *testing.T) {
	t.Parallel()

	server, apiKey, capturedBody := newAugmentLegacyRuntimeTestServer(t)
	defer server.Close()

	client := server.Client()
	reqBody := `{
		"message":"请总结当前仓库结构",
		"requestNodes":[{"id":1,"type":0,"textNode":{"content":"必须引用真实文件"}}],
		"blobs":{"checkpoint_id":"","added_blobs":["blob-a"],"deleted_blobs":[]}
	}`
	httpReq, err := http.NewRequest(http.MethodPost, server.URL+"/chat-stream", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	lines := nonEmptyLines(t, resp.Body)
	require.NotEmpty(t, lines)

	var openaiBody map[string]any
	require.NoError(t, json.Unmarshal([]byte(*capturedBody), &openaiBody))
	require.Equal(t, "gpt-5.4", openaiBody["model"])
}

func TestAugmentLegacyChatStreamFallsBackToCompatDefaultModelWhenClaudeModelUnavailable(t *testing.T) {
	t.Parallel()

	server, apiKey, capturedBody := newAugmentLegacyRuntimeTestServer(t)
	defer server.Close()

	client := server.Client()
	reqBody := `{
		"model":"claude-sonnet-4-5",
		"message":"请总结当前仓库结构",
		"blobs":{"checkpoint_id":"","added_blobs":["blob-a"],"deleted_blobs":[]}
	}`
	httpReq, err := http.NewRequest(http.MethodPost, server.URL+"/chat-stream", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	lines := nonEmptyLines(t, resp.Body)
	require.NotEmpty(t, lines)

	var openaiBody map[string]any
	require.NoError(t, json.Unmarshal([]byte(*capturedBody), &openaiBody))
	require.Equal(t, "gpt-5.4", openaiBody["model"])
}

func TestAugmentLegacyChatStreamFallsBackToCompatDefaultModelWhenOpenAIGroupAdvertisesClaudeScope(t *testing.T) {
	t.Parallel()

	server, apiKey, capturedBody := newAugmentLegacyRuntimeTestServerWithGroups(t, []service.Group{
		{
			ID:                    101,
			Name:                  "openai-default",
			Platform:              service.PlatformOpenAI,
			Status:                service.StatusActive,
			Hydrated:              true,
			AllowMessagesDispatch: false,
			SupportedModelScopes:  []string{"claude", "gemini_text", "gemini_image"},
		},
		{
			ID:                   102,
			Name:                 "anthropic-default",
			Platform:             service.PlatformAnthropic,
			Status:               service.StatusActive,
			Hydrated:             true,
			SupportedModelScopes: []string{"claude", "gemini_text", "gemini_image"},
		},
	})
	defer server.Close()

	client := server.Client()
	reqBody := `{
		"model":"claude-sonnet-4-5",
		"message":"请总结当前仓库结构",
		"blobs":{"checkpoint_id":"","added_blobs":["blob-a"],"deleted_blobs":[]}
	}`
	httpReq, err := http.NewRequest(http.MethodPost, server.URL+"/chat-stream", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	lines := nonEmptyLines(t, resp.Body)
	require.NotEmpty(t, lines)

	var openaiBody map[string]any
	require.NoError(t, json.Unmarshal([]byte(*capturedBody), &openaiBody))
	require.Equal(t, "gpt-5.4", openaiBody["model"])
}

func TestAugmentLegacyChatStreamKeepsClaudeModelWhenClaudeScopeAvailable(t *testing.T) {
	t.Parallel()

	server, apiKey, capturedBody := newAugmentLegacyRuntimeTestServerWithGroups(t, []service.Group{
		{
			ID:                   201,
			Name:                 "Claude",
			Platform:             service.PlatformAntigravity,
			Status:               service.StatusActive,
			Hydrated:             true,
			SupportedModelScopes: []string{"claude"},
		},
	})
	defer server.Close()

	client := server.Client()
	reqBody := `{
		"model":"claude-sonnet-4-5",
		"message":"请总结当前仓库结构",
		"blobs":{"checkpoint_id":"","added_blobs":["blob-a"],"deleted_blobs":[]}
	}`
	httpReq, err := http.NewRequest(http.MethodPost, server.URL+"/chat-stream", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	lines := nonEmptyLines(t, resp.Body)
	require.NotEmpty(t, lines)

	var openaiBody map[string]any
	require.NoError(t, json.Unmarshal([]byte(*capturedBody), &openaiBody))
	require.Equal(t, "claude-sonnet-4-5", openaiBody["model"])
}

func TestAugmentLegacyChatStreamAcceptsToolFollowupHistoryShape(t *testing.T) {
	t.Parallel()

	server, apiKey, capturedBody := newAugmentLegacyRuntimeTestServer(t)
	defer server.Close()

	client := server.Client()
	reqBody := `{
		"model":"gpt-5.4",
		"message":"继续基于检索结果回答",
		"chat_history":[
			{
				"request_id":"prev-1",
				"request_message":null,
				"response_text":null,
				"requestNodes":[
					{
						"id":1,
						"type":1,
						"toolResultNode":{"toolUseId":"codebase-retrieval-1","content":"README.md\\nmain.go"}
					}
				],
				"responseNodes":[
					{
						"id":2,
						"type":5,
						"toolUse":{"toolUseId":"codebase-retrieval-1","toolName":"codebase-retrieval","inputJson":"{\"query\":\"repo layout\"}"}
					}
				]
			}
		],
		"requestNodes":[{"id":3,"type":0,"textNode":{"content":"请继续回答，不要重复问候"}}],
		"blobs":{"checkpoint_id":"","added_blobs":["blob-a"],"deleted_blobs":[]}
	}`
	httpReq, err := http.NewRequest(http.MethodPost, server.URL+"/chat-stream", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	lines := nonEmptyLines(t, resp.Body)
	require.NotEmpty(t, lines)

	var openaiBody map[string]any
	require.NoError(t, json.Unmarshal([]byte(*capturedBody), &openaiBody))
	messages, ok := openaiBody["messages"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, messages)
	require.Contains(t, *capturedBody, "继续基于检索结果回答")
	require.Contains(t, *capturedBody, "请继续回答，不要重复问候")
	var sawAssistantToolCall bool
	var sawToolResult bool
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		require.True(t, ok)
		role, _ := msg["role"].(string)
		if role == "assistant" {
			if toolCalls, ok := msg["tool_calls"].([]any); ok && len(toolCalls) > 0 {
				sawAssistantToolCall = true
			}
		}
		if role == "tool" && msg["tool_call_id"] == "codebase-retrieval-1" {
			sawToolResult = true
		}
	}
	require.True(t, sawAssistantToolCall)
	require.True(t, sawToolResult)
}

func TestAugmentLegacyBuildChatMessagesDropsOrphanToolCalls(t *testing.T) {
	t.Parallel()

	h := &AuthHandler{}
	req := augmentLegacyChatRequest{
		Message: "Please provide a clear and concise summary of our conversation so far.",
		ChatHistory: []augmentLegacyChatHistoryItem{
			{
				ResponseNodes: []augmentLegacyChatNode{
					{
						ID:   1,
						Type: ptrInt(augmentResponseNodeToolUse),
						ToolUse: &augmentLegacyToolUseNode{
							ToolUseID: "call-orphan-1",
							ToolName:  "codebase-retrieval",
							InputJSON: `{"query":"repo layout"}`,
						},
					},
				},
			},
		},
	}

	messages := h.augmentLegacyBuildChatMessages(req, "")
	for _, msg := range messages {
		require.Empty(t, msg.ToolCalls)
	}
	require.NotEmpty(t, messages)
	require.Equal(t, "user", messages[len(messages)-1].Role)
}

func TestAugmentLegacyBuildChatMessagesSynthesizesToolCallsForOrphanToolResults(t *testing.T) {
	t.Parallel()

	h := &AuthHandler{}
	req := augmentLegacyChatRequest{
		Message: "请用 3 点总结本会话到目前为止的关键结论。",
		ChatHistory: []augmentLegacyChatHistoryItem{
			{
				RequestNodes: []augmentLegacyChatNode{
					{
						ID:   1,
						Type: ptrInt(augmentRequestNodeToolResult),
						ToolResultNode: &augmentLegacyToolResultNode{
							ToolUseID: "call-result-only-1",
							Content:   "[CODEBASE_RETRIEVAL]\nbackend/internal/handler/auth_augment_runtime.go",
						},
					},
				},
			},
		},
	}

	messages := h.augmentLegacyBuildChatMessages(req, "")
	require.Len(t, messages, 3)
	require.Equal(t, "assistant", messages[0].Role)
	require.Len(t, messages[0].ToolCalls, 1)
	require.Equal(t, "call-result-only-1", messages[0].ToolCalls[0].ID)
	require.Equal(t, "codebase-retrieval", messages[0].ToolCalls[0].Function.Name)
	require.Equal(t, "tool", messages[1].Role)
	require.Equal(t, "call-result-only-1", messages[1].ToolCallID)
	require.Equal(t, "user", messages[2].Role)
}

func TestAugmentLegacyChatStreamAcceptsNullCheckpointID(t *testing.T) {
	t.Parallel()

	server, apiKey, capturedBody := newAugmentLegacyRuntimeTestServer(t)
	defer server.Close()

	client := server.Client()
	reqBody := `{
		"model":"gpt-5.4",
		"message":"请结合当前索引结果总结仓库",
		"blobs":{"checkpoint_id":null,"added_blobs":["blob-a"],"deleted_blobs":[]}
	}`
	httpReq, err := http.NewRequest(http.MethodPost, server.URL+"/chat-stream", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	lines := nonEmptyLines(t, resp.Body)
	require.NotEmpty(t, lines)
	require.Contains(t, *capturedBody, "请结合当前索引结果总结仓库")
}

func TestAugmentLegacyChatStreamAcceptsToolResultOnlyFollowup(t *testing.T) {
	t.Parallel()

	server, apiKey, capturedBody := newAugmentLegacyRuntimeTestServer(t)
	defer server.Close()

	client := server.Client()
	reqBody := `{
		"model":"gpt-5.4",
		"message":"",
		"requestNodes":[
			{
				"id":1,
				"type":1,
				"toolResultNode":{
					"toolUseId":"codebase-retrieval-1",
					"content":"[CODEBASE_RETRIEVAL]\\nREADME.md\\nmain.go\\nservice layer"
				}
			}
		],
		"blobs":{"checkpoint_id":"","added_blobs":["blob-a"],"deleted_blobs":[]}
	}`
	httpReq, err := http.NewRequest(http.MethodPost, server.URL+"/chat-stream", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	lines := nonEmptyLines(t, resp.Body)
	require.NotEmpty(t, lines)

	var openaiBody map[string]any
	require.NoError(t, json.Unmarshal([]byte(*capturedBody), &openaiBody))
	messages, ok := openaiBody["messages"].([]any)
	require.True(t, ok)
	require.Len(t, messages, 2)
	msg, ok := messages[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "assistant", msg["role"])
	toolCalls, ok := msg["tool_calls"].([]any)
	require.True(t, ok)
	require.Len(t, toolCalls, 1)
	msg, ok = messages[1].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "tool", msg["role"])
	require.Equal(t, "codebase-retrieval-1", msg["tool_call_id"])
	require.Contains(t, stringValueOrRawJSON(t, msg["content"]), "README.md")
	require.Contains(t, stringValueOrRawJSON(t, msg["content"]), "service layer")
}

func TestAugmentLegacyChatStreamToolFollowupPreservesSameTurnRequestTextWithoutReplayingBareContent(t *testing.T) {
	t.Parallel()

	server, apiKey, capturedBody := newAugmentLegacyRuntimeTestServer(t)
	defer server.Close()

	client := server.Client()
	reqBody := `{
		"model":"gpt-5.4",
		"message":"",
		"nodes":[
			{
				"id":9,
				"type":0,
				"content":"这是上一轮 assistant 的旧回答，不应被重新拼成新的用户请求"
			}
		],
		"requestNodes":[
			{
				"id":1,
				"type":1,
				"toolResultNode":{
					"toolUseId":"codebase-retrieval-1",
					"content":"[CODEBASE_RETRIEVAL]\\nREADME.md\\nmain.go"
				}
			},
			{
				"id":2,
				"type":0,
				"textNode":{"content":"请继续回答，不要重复问候"}
			}
		],
		"blobs":{"checkpoint_id":"","added_blobs":["blob-a"],"deleted_blobs":[]}
	}`
	httpReq, err := http.NewRequest(http.MethodPost, server.URL+"/chat-stream", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	lines := nonEmptyLines(t, resp.Body)
	require.NotEmpty(t, lines)

	var openaiBody map[string]any
	require.NoError(t, json.Unmarshal([]byte(*capturedBody), &openaiBody))
	messages, ok := openaiBody["messages"].([]any)
	require.True(t, ok)
	require.Len(t, messages, 3)

	msg, ok := messages[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "assistant", msg["role"])
	toolCalls, ok := msg["tool_calls"].([]any)
	require.True(t, ok)
	require.Len(t, toolCalls, 1)
	msg, ok = messages[1].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "tool", msg["role"])
	require.Equal(t, "codebase-retrieval-1", msg["tool_call_id"])
	userMsg, ok := messages[2].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "user", userMsg["role"])
	require.Contains(t, stringValueOrRawJSON(t, userMsg["content"]), "请继续回答，不要重复问候")
	require.NotContains(t, *capturedBody, "旧回答，不应被重新拼成新的用户请求")
}

func TestAugmentLegacyChatStreamIncludesHistoryRequestNodeText(t *testing.T) {
	t.Parallel()

	server, apiKey, capturedBody := newAugmentLegacyRuntimeTestServer(t)
	defer server.Close()

	client := server.Client()
	reqBody := `{
		"model":"gpt-5.4",
		"message":"我刚才给你的 CE-011R-E prompt 是什么？",
		"chat_history":[
			{
				"request_id":"ce-011r-e",
				"request_message":null,
				"response_text":"已完成。",
				"requestNodes":[
					{
						"id":1,
						"type":0,
						"textNode":{"content":"CE-011R-E：请基于 sub2api 当前仓库验证标题摘要和缓存命中情况。"}
					}
				]
			}
		],
		"blobs":{"checkpoint_id":"","added_blobs":[],"deleted_blobs":[]}
	}`
	httpReq, err := http.NewRequest(http.MethodPost, server.URL+"/chat-stream", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	lines := nonEmptyLines(t, resp.Body)
	require.NotEmpty(t, lines)
	require.Contains(t, *capturedBody, "CE-011R-E：请基于 sub2api 当前仓库验证标题摘要和缓存命中情况。")
	require.Contains(t, *capturedBody, "我刚才给你的 CE-011R-E prompt 是什么？")
}

func TestAugmentLegacyChatStreamAddsStablePromptCacheKeyFromConversation(t *testing.T) {
	t.Parallel()

	server, apiKey, capturedBody := newAugmentLegacyRuntimeTestServer(t)
	defer server.Close()

	client := server.Client()
	reqBody := `{
		"model":"gpt-5.4",
		"conversationId":"conv-ce-011r-e",
		"message":"继续验证缓存。",
		"blobs":{"checkpoint_id":"","added_blobs":[],"deleted_blobs":[]}
	}`
	httpReq, err := http.NewRequest(http.MethodPost, server.URL+"/chat-stream", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-request-session-id", "session-51ba670a")

	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	lines := nonEmptyLines(t, resp.Body)
	require.NotEmpty(t, lines)

	var openaiBody map[string]any
	require.NoError(t, json.Unmarshal([]byte(*capturedBody), &openaiBody))
	cacheKey, ok := openaiBody["prompt_cache_key"].(string)
	require.True(t, ok)
	require.NotEmpty(t, cacheKey)
	require.Contains(t, cacheKey, "augment_legacy_")
	require.NotContains(t, cacheKey, "conv-ce-011r-e")
	require.NotContains(t, cacheKey, "session-51ba670a")
}

func TestAugmentLegacyChatSkipsLocalRetrievalWhenDisableRetrievalIsTrue(t *testing.T) {
	t.Parallel()

	server, apiKey, capturedBody := newAugmentLegacyRuntimeTestServer(t)
	defer server.Close()

	client := server.Client()
	reqBody := `{
		"model":"gpt-5.4",
		"message":"explain main",
		"disable_retrieval":true,
		"blobs":{"checkpoint_id":"","added_blobs":["blob-a"],"deleted_blobs":[]}
	}`
	httpReq, err := http.NewRequest(http.MethodPost, server.URL+"/chat", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	require.NotContains(t, *capturedBody, "[CODEBASE_RETRIEVAL]")
	require.NotContains(t, *capturedBody, "src/main.go")
}

func TestAugmentLegacyChatSkipsLocalRetrievalWhenCodebaseToolDefinitionPresent(t *testing.T) {
	t.Parallel()

	server, apiKey, capturedBody := newAugmentLegacyRuntimeTestServer(t)
	defer server.Close()

	client := server.Client()
	reqBody := `{
		"model":"gpt-5.4",
		"message":"explain main",
		"tool_definitions":[
			{
				"name":"codebase-retrieval",
				"description":"repo search",
				"input_schema":{"type":"object"}
			}
		],
		"blobs":{"checkpoint_id":"","added_blobs":["blob-a"],"deleted_blobs":[]}
	}`
	httpReq, err := http.NewRequest(http.MethodPost, server.URL+"/chat", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	require.NotContains(t, *capturedBody, "[CODEBASE_RETRIEVAL]")
	require.NotContains(t, *capturedBody, "src/main.go")

	var openaiBody map[string]any
	require.NoError(t, json.Unmarshal([]byte(*capturedBody), &openaiBody))
	tools, ok := openaiBody["tools"].([]any)
	require.True(t, ok)
	require.Len(t, tools, 1)
	tool, ok := tools[0].(map[string]any)
	require.True(t, ok)
	function, ok := tool["function"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "codebase-retrieval", function["name"])
}

func TestAugmentLegacyChatRejectsWrappedPayload(t *testing.T) {
	t.Parallel()

	server, apiKey, _ := newAugmentLegacyRuntimeTestServer(t)
	defer server.Close()

	client := server.Client()
	reqBody := `{"encrypted_data":"deadbeef","iv":"wrapped","model":"gpt-5.4"}`
	httpReq, err := http.NewRequest(http.MethodPost, server.URL+"/chat-stream", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Contains(t, readBody(t, resp.Body), "UNSUPPORTED_WRAPPER_PAYLOAD")
}

func TestAugmentLegacyToolDefinitionsDropTasklistTools(t *testing.T) {
	t.Parallel()

	h := &AuthHandler{}
	defs := []augmentLegacyToolDefinition{
		{Name: "codebase-retrieval", Description: "repo search"},
		{Name: "view_tasklist", Description: "view tasks"},
		{Name: "add_tasks", Description: "add tasks"},
		{Name: "update_tasks", Description: "update tasks"},
		{Name: "reorganize_tasklist", Description: "reorg tasks"},
	}

	got := h.augmentLegacyToolDefinitionsToOpenAI(defs)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].Function)
	require.Equal(t, "codebase-retrieval", got[0].Function.Name)
}

func TestAugmentLegacyBuildChatObservability(t *testing.T) {
	toolResultType := augmentRequestNodeToolResult

	tests := []struct {
		name                  string
		req                   augmentLegacyChatRequest
		retrieval             string
		wantToolCount         int
		wantToolNames         []string
		wantToolResultFollow  bool
		wantRetrievalInjected bool
	}{
		{
			name: "tool result followup skips local retrieval injection",
			req: augmentLegacyChatRequest{
				ToolDefinitions: []augmentLegacyToolDefinition{
					{Name: " codebase-retrieval "},
					{Name: ""},
					{Name: "view_tasklist"},
				},
				RequestNodes: []augmentLegacyChatNode{
					{
						Type: &toolResultType,
						ToolResultNode: &augmentLegacyToolResultNode{
							ToolUseID: "call-1",
							Content:   "[CODEBASE_RETRIEVAL]\nREADME.md",
						},
					},
				},
			},
			wantToolCount:         3,
			wantToolNames:         []string{"codebase-retrieval", "view_tasklist"},
			wantToolResultFollow:  true,
			wantRetrievalInjected: false,
		},
		{
			name: "non-empty retrieval is reported when the request does not advertise codebase retrieval",
			req: augmentLegacyChatRequest{
				ToolDefinitions: []augmentLegacyToolDefinition{
					{Name: "view_tasklist"},
				},
			},
			retrieval:             "[CODEBASE_RETRIEVAL]\nsrc/main.go",
			wantToolCount:         1,
			wantToolNames:         []string{"view_tasklist"},
			wantToolResultFollow:  false,
			wantRetrievalInjected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obs := augmentLegacyBuildChatObservability(tt.req, tt.retrieval)
			require.Equal(t, tt.wantToolCount, obs.InboundToolDefinitionCount)
			require.Equal(t, tt.wantToolNames, obs.InboundToolDefinitionNames)
			require.Equal(t, tt.wantToolResultFollow, obs.HasToolResultFollowup)
			require.Equal(t, tt.wantRetrievalInjected, obs.LocalRetrievalInjected)
		})
	}
}

func TestAugmentLegacyChatForwardsInboundToolsAndSkipsLocalRetrievalWhenCodebaseRetrievalAdvertised(t *testing.T) {
	t.Parallel()

	server, apiKey, capturedBody := newAugmentLegacyRuntimeTestServer(t)
	defer server.Close()

	client := server.Client()
	reqBody := `{
		"model":"gpt-5.4",
		"message":"请总结当前仓库结构",
		"toolDefinitions":[
			{
				"name":"codebase-retrieval",
				"description":"repo search",
				"input_schema":{"type":"object","properties":{"query":{"type":"string"}}}
			},
			{
				"name":"view_tasklist",
				"description":"view tasks"
			}
		],
		"blobs":{"checkpoint_id":"","added_blobs":["blob-a"],"deleted_blobs":[]}
	}`
	httpReq, err := http.NewRequest(http.MethodPost, server.URL+"/chat", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var openaiBody map[string]any
	require.NoError(t, json.Unmarshal([]byte(*capturedBody), &openaiBody))
	require.Equal(t, "auto", openaiBody["tool_choice"])
	tools, ok := openaiBody["tools"].([]any)
	require.True(t, ok)
	require.Len(t, tools, 1)

	tool, ok := tools[0].(map[string]any)
	require.True(t, ok)
	functionBody, ok := tool["function"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "codebase-retrieval", functionBody["name"])

	messages, ok := openaiBody["messages"].([]any)
	require.True(t, ok)
	var assistantContents []string
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		require.True(t, ok)
		if msg["role"] == "assistant" {
			assistantContents = append(assistantContents, stringValueOrRawJSON(t, msg["content"]))
		}
	}
	require.NotContains(t, strings.Join(assistantContents, "\n"), "[CODEBASE_RETRIEVAL]")
	require.NotContains(t, strings.Join(assistantContents, "\n"), "src/main.go")
}

func TestAugmentLegacyChatStreamFlushesBeforeUpstreamCompletes(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	user := &service.User{
		ID:       2,
		Email:    "compat@example.com",
		Username: "compat",
		Role:     service.RoleAdmin,
		Status:   service.StatusActive,
	}
	apiKey := &service.APIKey{
		ID:        2,
		UserID:    user.ID,
		Key:       "sk-compat-runtime",
		Name:      "compat-runtime",
		Status:    service.StatusActive,
		CreatedAt: time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC),
		User:      user,
	}
	group := service.Group{
		ID:                 101,
		Name:               "OpenAI",
		Platform:           service.PlatformOpenAI,
		Status:             service.StatusActive,
		Hydrated:           true,
		DefaultMappedModel: "gpt-5.4",
	}
	markAugmentRuntimeAPIKey(apiKey, &group)

	pluginService := service.NewAugmentPluginService(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		augmentPluginAuthStub{},
		augmentPluginUserStub{user: user},
		augmentPluginAPIKeyStub{
			apiKeyByValue: map[string]*service.APIKey{apiKey.Key: apiKey},
			keysByUser:    map[int64][]service.APIKey{user.ID: {*apiKey}},
			availableByUser: map[int64][]service.Group{
				user.ID: {group},
			},
		},
		augmentPluginSubscriptionStub{},
		augmentPluginSettingStub{
			public: &service.PublicSettings{
				SiteName:   "逐梦站",
				APIBaseURL: "http://127.0.0.1:18081",
			},
		},
	)

	authHandler := NewAuthHandler(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		pluginService,
	)

	router := gin.New()
	router.POST("/chat-stream", authHandler.AugmentLegacyChatStream)
	router.POST("/openai/v1/chat/completions", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		c.Status(http.StatusOK)
		flusher, ok := c.Writer.(http.Flusher)
		require.True(t, ok)

		_, _ = c.Writer.Write([]byte("data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"created\":1710000000,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"你好\"},\"finish_reason\":null}]}\n\n"))
		flusher.Flush()
		time.Sleep(350 * time.Millisecond)
		_, _ = c.Writer.Write([]byte("data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"created\":1710000001,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"，世界\"},\"finish_reason\":null}]}\n\n"))
		flusher.Flush()
		time.Sleep(50 * time.Millisecond)
		_, _ = c.Writer.Write([]byte("data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"created\":1710000002,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = c.Writer.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	})

	server := httptest.NewServer(router)
	defer server.Close()

	client := server.Client()
	reqBody := `{"model":"gpt-5.4","message":"stream main","blobs":{"checkpoint_id":"","added_blobs":[],"deleted_blobs":[]}}`
	httpReq, err := http.NewRequest(http.MethodPost, server.URL+"/chat-stream", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey.Key)
	httpReq.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	reader := bufio.NewReader(resp.Body)
	firstLineCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				errCh <- err
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			firstLineCh <- line
			return
		}
	}()

	select {
	case line := <-firstLineCh:
		require.Less(t, time.Since(start), 250*time.Millisecond)
		require.Contains(t, line, "你好")
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(250 * time.Millisecond):
		t.Fatal("expected first streamed NDJSON chunk before upstream finished")
	}
}

func TestAugmentLegacyChatStreamReturnsHTTPErrorWhenLoopbackFailsBeforeStreaming(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	user := &service.User{
		ID:       2,
		Email:    "compat@example.com",
		Username: "compat",
		Role:     service.RoleAdmin,
		Status:   service.StatusActive,
	}
	apiKey := &service.APIKey{
		ID:        2,
		UserID:    user.ID,
		Key:       "sk-compat-runtime",
		Name:      "compat-runtime",
		Status:    service.StatusActive,
		CreatedAt: time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC),
		User:      user,
	}
	group := service.Group{
		ID:                 101,
		Name:               "OpenAI",
		Platform:           service.PlatformOpenAI,
		Status:             service.StatusActive,
		Hydrated:           true,
		DefaultMappedModel: "gpt-5.4",
	}
	markAugmentRuntimeAPIKey(apiKey, &group)

	pluginService := service.NewAugmentPluginService(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		augmentPluginAuthStub{},
		augmentPluginUserStub{user: user},
		augmentPluginAPIKeyStub{
			apiKeyByValue: map[string]*service.APIKey{apiKey.Key: apiKey},
			keysByUser:    map[int64][]service.APIKey{user.ID: {*apiKey}},
			availableByUser: map[int64][]service.Group{
				user.ID: {group},
			},
		},
		augmentPluginSubscriptionStub{},
		augmentPluginSettingStub{
			public: &service.PublicSettings{
				SiteName:   "逐梦站",
				APIBaseURL: "http://127.0.0.1:18081",
			},
		},
	)

	authHandler := NewAuthHandler(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		pluginService,
	)

	router := gin.New()
	router.POST("/chat-stream", authHandler.AugmentLegacyChatStream)
	router.POST("/openai/v1/chat/completions", func(c *gin.Context) {
		c.JSON(http.StatusBadGateway, gin.H{
			"error": gin.H{
				"message": "Upstream service temporarily unavailable",
				"type":    "upstream_error",
			},
		})
	})

	server := httptest.NewServer(router)
	defer server.Close()

	client := server.Client()
	reqBody := `{"model":"gpt-5.4","message":"stream main","blobs":{"checkpoint_id":"","added_blobs":[],"deleted_blobs":[]}}`
	httpReq, err := http.NewRequest(http.MethodPost, server.URL+"/chat-stream", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey.Key)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadGateway, resp.StatusCode)
	require.Contains(t, string(body), "upstream_error")
	require.NotContains(t, string(body), "loopback chat completion stream")
}

func TestAugmentLegacyChatStreamBuffersSplitToolCallArgumentsUntilValidJSON(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	user := &service.User{
		ID:       2,
		Email:    "compat@example.com",
		Username: "compat",
		Role:     service.RoleAdmin,
		Status:   service.StatusActive,
	}
	apiKey := &service.APIKey{
		ID:        2,
		UserID:    user.ID,
		Key:       "sk-compat-runtime",
		Name:      "compat-runtime",
		Status:    service.StatusActive,
		CreatedAt: time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC),
		User:      user,
	}
	group := service.Group{
		ID:                 101,
		Name:               "OpenAI",
		Platform:           service.PlatformOpenAI,
		Status:             service.StatusActive,
		Hydrated:           true,
		DefaultMappedModel: "gpt-5.4",
	}
	markAugmentRuntimeAPIKey(apiKey, &group)

	pluginService := service.NewAugmentPluginService(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		augmentPluginAuthStub{},
		augmentPluginUserStub{user: user},
		augmentPluginAPIKeyStub{
			apiKeyByValue: map[string]*service.APIKey{apiKey.Key: apiKey},
			keysByUser:    map[int64][]service.APIKey{user.ID: {*apiKey}},
			availableByUser: map[int64][]service.Group{
				user.ID: {group},
			},
		},
		augmentPluginSubscriptionStub{},
		augmentPluginSettingStub{
			public: &service.PublicSettings{
				SiteName:   "逐梦站",
				APIBaseURL: "http://127.0.0.1:18081",
			},
		},
	)

	authHandler := NewAuthHandler(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		pluginService,
	)

	router := gin.New()
	router.POST("/chat-stream", authHandler.AugmentLegacyChatStream)
	router.POST("/openai/v1/chat/completions", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		c.Status(http.StatusOK)
		flusher, ok := c.Writer.(http.Flusher)
		require.True(t, ok)

		_, _ = c.Writer.Write([]byte("data: {\"id\":\"chatcmpl-tool\",\"object\":\"chat.completion.chunk\",\"created\":1710000000,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_launch_1\",\"type\":\"function\",\"function\":{\"name\":\"launch-process\",\"arguments\":\"{\\\"command\\\":\"}}]},\"finish_reason\":null}]}\n\n"))
		flusher.Flush()
		_, _ = c.Writer.Write([]byte("data: {\"id\":\"chatcmpl-tool\",\"object\":\"chat.completion.chunk\",\"created\":1710000001,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"pwd\\\",\\\"wait_for_seconds\\\":1}\"}}]},\"finish_reason\":null}]}\n\n"))
		flusher.Flush()
		_, _ = c.Writer.Write([]byte("data: {\"id\":\"chatcmpl-tool\",\"object\":\"chat.completion.chunk\",\"created\":1710000002,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
		_, _ = c.Writer.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	})

	server := httptest.NewServer(router)
	defer server.Close()

	client := server.Client()
	reqBody := `{"model":"gpt-5.4","message":"请调用工具","blobs":{"checkpoint_id":"","added_blobs":[],"deleted_blobs":[]}}`
	httpReq, err := http.NewRequest(http.MethodPost, server.URL+"/chat-stream", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey.Key)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	lines := nonEmptyLines(t, resp.Body)
	require.NotEmpty(t, lines)

	var toolInputs []string
	for _, line := range lines {
		var chunk map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &chunk))
		nodes, ok := chunk["nodes"].([]any)
		if !ok {
			continue
		}
		for _, rawNode := range nodes {
			node, ok := rawNode.(map[string]any)
			require.True(t, ok)
			if node["type"] != float64(augmentResponseNodeToolUse) {
				continue
			}
			toolUse, ok := node["tool_use"].(map[string]any)
			require.True(t, ok)
			inputJSON, ok := toolUse["input_json"].(string)
			require.True(t, ok)
			toolInputs = append(toolInputs, inputJSON)
			require.True(t, json.Valid([]byte(inputJSON)), "tool input must be valid JSON before it is emitted: %q", inputJSON)
		}
	}

	require.Equal(t, []string{`{"command":"pwd","wait_for_seconds":1}`}, toolInputs)
}

func TestAugmentLegacyChatStreamInvalidFinalToolArgumentsDoNotLeaveToolUseRequired(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	user := &service.User{
		ID:       2,
		Email:    "compat@example.com",
		Username: "compat",
		Role:     service.RoleAdmin,
		Status:   service.StatusActive,
	}
	apiKey := &service.APIKey{
		ID:        2,
		UserID:    user.ID,
		Key:       "sk-compat-runtime",
		Name:      "compat-runtime",
		Status:    service.StatusActive,
		CreatedAt: time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC),
		User:      user,
	}
	group := service.Group{
		ID:                 101,
		Name:               "OpenAI",
		Platform:           service.PlatformOpenAI,
		Status:             service.StatusActive,
		Hydrated:           true,
		DefaultMappedModel: "gpt-5.4",
	}
	markAugmentRuntimeAPIKey(apiKey, &group)

	pluginService := service.NewAugmentPluginService(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		augmentPluginAuthStub{},
		augmentPluginUserStub{user: user},
		augmentPluginAPIKeyStub{
			apiKeyByValue: map[string]*service.APIKey{apiKey.Key: apiKey},
			keysByUser:    map[int64][]service.APIKey{user.ID: {*apiKey}},
			availableByUser: map[int64][]service.Group{
				user.ID: {group},
			},
		},
		augmentPluginSubscriptionStub{},
		augmentPluginSettingStub{
			public: &service.PublicSettings{
				SiteName:   "逐梦站",
				APIBaseURL: "http://127.0.0.1:18081",
			},
		},
	)

	authHandler := NewAuthHandler(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		pluginService,
	)

	router := gin.New()
	router.POST("/chat-stream", authHandler.AugmentLegacyChatStream)
	router.POST("/openai/v1/chat/completions", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		c.Status(http.StatusOK)
		flusher, ok := c.Writer.(http.Flusher)
		require.True(t, ok)

		_, _ = c.Writer.Write([]byte("data: {\"id\":\"chatcmpl-invalid-tool\",\"object\":\"chat.completion.chunk\",\"created\":1710000000,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_bad_1\",\"type\":\"function\",\"function\":{\"name\":\"read-file\",\"arguments\":\"{\\\"path\\\":\"}}]},\"finish_reason\":null}]}\n\n"))
		flusher.Flush()
		_, _ = c.Writer.Write([]byte("data: {\"id\":\"chatcmpl-invalid-tool\",\"object\":\"chat.completion.chunk\",\"created\":1710000001,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
		_, _ = c.Writer.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	})

	server := httptest.NewServer(router)
	defer server.Close()

	client := server.Client()
	reqBody := `{"model":"gpt-5.4","message":"请调用工具","blobs":{"checkpoint_id":"","added_blobs":[],"deleted_blobs":[]}}`
	httpReq, err := http.NewRequest(http.MethodPost, server.URL+"/chat-stream", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey.Key)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	lines := nonEmptyLines(t, resp.Body)
	require.NotEmpty(t, lines)

	var sawToolUse bool
	var sawErrorText bool
	var finalStopReason float64
	for _, line := range lines {
		var chunk map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &chunk))
		if text, _ := chunk["text"].(string); strings.Contains(text, "incomplete tool arguments") {
			sawErrorText = true
		}
		if nodes, ok := chunk["nodes"].([]any); ok {
			for _, rawNode := range nodes {
				node, ok := rawNode.(map[string]any)
				require.True(t, ok)
				if node["type"] == float64(augmentResponseNodeToolUse) {
					sawToolUse = true
				}
			}
		}
		if stopReason, ok := chunk["stop_reason"].(float64); ok {
			finalStopReason = stopReason
		}
	}

	require.False(t, sawToolUse)
	require.True(t, sawErrorText)
	require.Equal(t, float64(augmentStopReasonEndTurn), finalStopReason)
}

func TestAugmentLegacyChatStreamEmitsInterleavedToolCallsInIndexOrder(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	user := &service.User{
		ID:       2,
		Email:    "compat@example.com",
		Username: "compat",
		Role:     service.RoleAdmin,
		Status:   service.StatusActive,
	}
	apiKey := &service.APIKey{
		ID:        2,
		UserID:    user.ID,
		Key:       "sk-compat-runtime",
		Name:      "compat-runtime",
		Status:    service.StatusActive,
		CreatedAt: time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC),
		User:      user,
	}
	group := service.Group{
		ID:                 101,
		Name:               "OpenAI",
		Platform:           service.PlatformOpenAI,
		Status:             service.StatusActive,
		Hydrated:           true,
		DefaultMappedModel: "gpt-5.4",
	}
	markAugmentRuntimeAPIKey(apiKey, &group)

	pluginService := service.NewAugmentPluginService(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		augmentPluginAuthStub{},
		augmentPluginUserStub{user: user},
		augmentPluginAPIKeyStub{
			apiKeyByValue: map[string]*service.APIKey{apiKey.Key: apiKey},
			keysByUser:    map[int64][]service.APIKey{user.ID: {*apiKey}},
			availableByUser: map[int64][]service.Group{
				user.ID: {group},
			},
		},
		augmentPluginSubscriptionStub{},
		augmentPluginSettingStub{
			public: &service.PublicSettings{
				SiteName:   "逐梦站",
				APIBaseURL: "http://127.0.0.1:18081",
			},
		},
	)

	authHandler := NewAuthHandler(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		pluginService,
	)

	router := gin.New()
	router.POST("/chat-stream", authHandler.AugmentLegacyChatStream)
	router.POST("/openai/v1/chat/completions", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		c.Status(http.StatusOK)
		flusher, ok := c.Writer.(http.Flusher)
		require.True(t, ok)

		_, _ = c.Writer.Write([]byte("data: {\"id\":\"chatcmpl-multi-tool\",\"object\":\"chat.completion.chunk\",\"created\":1710000000,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_first\",\"type\":\"function\",\"function\":{\"name\":\"read-file\",\"arguments\":\"{\\\"path\\\":\"}},{\"index\":1,\"id\":\"call_second\",\"type\":\"function\",\"function\":{\"name\":\"read-file\",\"arguments\":\"{\\\"path\\\":\\\"README.md\\\"}\"}}]},\"finish_reason\":null}]}\n\n"))
		flusher.Flush()
		_, _ = c.Writer.Write([]byte("data: {\"id\":\"chatcmpl-multi-tool\",\"object\":\"chat.completion.chunk\",\"created\":1710000001,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"main.go\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"))
		_, _ = c.Writer.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	})

	server := httptest.NewServer(router)
	defer server.Close()

	client := server.Client()
	reqBody := `{"model":"gpt-5.4","message":"请调用两个工具","blobs":{"checkpoint_id":"","added_blobs":[],"deleted_blobs":[]}}`
	httpReq, err := http.NewRequest(http.MethodPost, server.URL+"/chat-stream", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey.Key)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	lines := nonEmptyLines(t, resp.Body)
	require.NotEmpty(t, lines)

	var toolCallIDs []string
	for _, line := range lines {
		var chunk map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &chunk))
		nodes, ok := chunk["nodes"].([]any)
		if !ok {
			continue
		}
		for _, rawNode := range nodes {
			node, ok := rawNode.(map[string]any)
			require.True(t, ok)
			if node["type"] != float64(augmentResponseNodeToolUse) {
				continue
			}
			toolUse, ok := node["tool_use"].(map[string]any)
			require.True(t, ok)
			callID, ok := toolUse["tool_use_id"].(string)
			require.True(t, ok)
			toolCallIDs = append(toolCallIDs, callID)
		}
	}

	require.Equal(t, []string{"call_first", "call_second"}, toolCallIDs)
}

func TestAugmentPluginServicePersistsLegacyCompatibilityState(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cfg := &config.Config{Pricing: config.PricingConfig{DataDir: tmpDir}}

	svc := service.NewAugmentPluginService(
		cfg,
		augmentPluginAuthStub{},
		augmentPluginUserStub{user: &service.User{ID: 1, Email: "persist@example.com", Role: service.RoleAdmin, Status: service.StatusActive}},
		augmentPluginAPIKeyStub{},
		augmentPluginSubscriptionStub{},
		augmentPluginSettingStub{public: &service.PublicSettings{SiteName: "逐梦站", APIBaseURL: "http://127.0.0.1:18081"}},
	)

	namespace := "workspace-session-1"
	stored := svc.StoreLegacyBlobsForNamespace(namespace, []service.AugmentLegacyUploadedBlob{
		{BlobName: "blob-a", Path: "src/main.go", Content: "package main\nfunc main(){}\n"},
	})
	require.Equal(t, []string{"blob-a"}, stored)

	checkpointID, err := svc.AdvanceLegacyCheckpointForNamespace(namespace, "", []string{"blob-a"}, nil)
	require.NoError(t, err)
	svc.SaveLegacyChatForNamespace(namespace, service.AugmentLegacySavedChat{
		ConversationID: "conv-1",
		Title:          "Repo Overview",
		Chat: []service.AugmentLegacySavedChatItem{
			{RequestMessage: "summarize", ResponseText: "done", RequestID: "req-1"},
		},
	})

	stateFile := filepath.Join(tmpDir, "augment-compat", "legacy-state.json")
	if _, err := os.Stat(stateFile); err != nil {
		t.Fatalf("expected persisted state file at %s: %v", stateFile, err)
	}

	svcReloaded := service.NewAugmentPluginService(
		cfg,
		augmentPluginAuthStub{},
		augmentPluginUserStub{user: &service.User{ID: 1, Email: "persist@example.com", Role: service.RoleAdmin, Status: service.StatusActive}},
		augmentPluginAPIKeyStub{},
		augmentPluginSubscriptionStub{},
		augmentPluginSettingStub{public: &service.PublicSettings{SiteName: "逐梦站", APIBaseURL: "http://127.0.0.1:18081"}},
	)

	resolved := svcReloaded.ResolveLegacyBlobsForNamespace(namespace, checkpointID, nil, nil)
	require.False(t, resolved.CheckpointNotFound)
	require.Len(t, resolved.Records, 1)
	require.Equal(t, "blob-a", resolved.Records[0].BlobName)
	require.Contains(t, svcReloaded.BuildLegacyFormattedRetrieval("repo summary", resolved, 2000), "src/main.go")
	require.Contains(t, svcReloaded.BuildLegacyFormattedRetrieval("repo summary", resolved, 2000), "repo summary")
}

func TestAugmentLegacyCompatNamespacesAreScopedByAuthenticatedUser(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	userOne := &service.User{
		ID:       11,
		Email:    "user-one@example.com",
		Username: "user-one",
		Role:     service.RoleAdmin,
		Status:   service.StatusActive,
	}
	userTwo := &service.User{
		ID:       22,
		Email:    "user-two@example.com",
		Username: "user-two",
		Role:     service.RoleAdmin,
		Status:   service.StatusActive,
	}
	apiKeyOne := &service.APIKey{
		ID:        101,
		UserID:    userOne.ID,
		Key:       "sk-compat-user-one",
		Name:      "compat-user-one",
		Status:    service.StatusActive,
		CreatedAt: time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC),
		User:      userOne,
	}
	apiKeyTwo := &service.APIKey{
		ID:        102,
		UserID:    userTwo.ID,
		Key:       "sk-compat-user-two",
		Name:      "compat-user-two",
		Status:    service.StatusActive,
		CreatedAt: time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC),
		User:      userTwo,
	}
	group := service.Group{
		ID:                 101,
		Name:               "OpenAI",
		Platform:           service.PlatformOpenAI,
		Status:             service.StatusActive,
		Hydrated:           true,
		DefaultMappedModel: "gpt-5.4",
	}
	markAugmentRuntimeAPIKey(apiKeyOne, &group)
	markAugmentRuntimeAPIKey(apiKeyTwo, &group)

	pluginService := service.NewAugmentPluginService(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		augmentPluginAuthStub{},
		augmentPluginUserStub{
			users: map[int64]*service.User{
				userOne.ID: userOne,
				userTwo.ID: userTwo,
			},
		},
		augmentPluginAPIKeyStub{
			apiKeyByValue: map[string]*service.APIKey{
				apiKeyOne.Key: apiKeyOne,
				apiKeyTwo.Key: apiKeyTwo,
			},
			keysByUser: map[int64][]service.APIKey{
				userOne.ID: {*apiKeyOne},
				userTwo.ID: {*apiKeyTwo},
			},
			availableByUser: map[int64][]service.Group{
				userOne.ID: {group},
				userTwo.ID: {group},
			},
		},
		augmentPluginSubscriptionStub{},
		augmentPluginSettingStub{
			public: &service.PublicSettings{
				SiteName:   "逐梦站",
				APIBaseURL: "http://127.0.0.1:18081",
			},
		},
	)

	authHandler := NewAuthHandler(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		pluginService,
	)

	router := gin.New()
	router.POST("/batch-upload", authHandler.AugmentLegacyBatchUpload)
	router.POST("/find-missing", authHandler.AugmentLegacyFindMissing)

	server := httptest.NewServer(router)
	defer server.Close()

	client := server.Client()
	postJSON := func(apiKey string, sessionID string, path string, body string) *http.Response {
		req, err := http.NewRequest(http.MethodPost, server.URL+path, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")
		if sessionID != "" {
			req.Header.Set("x-request-session-id", sessionID)
		}
		resp, err := client.Do(req)
		require.NoError(t, err)
		return resp
	}

	uploadWithoutSession := postJSON(apiKeyOne.Key, "", "/batch-upload", `{"blobs":[{"blob_name":"blob-user-one-empty","path":"src/one.go","content":"package one"}]}`)
	defer uploadWithoutSession.Body.Close()
	require.Equal(t, http.StatusOK, uploadWithoutSession.StatusCode)

	findWithoutSession := postJSON(apiKeyTwo.Key, "", "/find-missing", `{"model":"gpt-5.4","mem_object_names":["blob-user-one-empty"]}`)
	defer findWithoutSession.Body.Close()
	require.Equal(t, http.StatusOK, findWithoutSession.StatusCode)
	var missingWithoutSession map[string][]string
	require.NoError(t, json.NewDecoder(findWithoutSession.Body).Decode(&missingWithoutSession))
	require.Equal(t, []string{"blob-user-one-empty"}, missingWithoutSession["unknown_memory_names"])

	uploadSharedSession := postJSON(apiKeyOne.Key, "shared-session", "/batch-upload", `{"blobs":[{"blob_name":"blob-user-one-shared","path":"src/shared.go","content":"package shared"}]}`)
	defer uploadSharedSession.Body.Close()
	require.Equal(t, http.StatusOK, uploadSharedSession.StatusCode)

	findSharedSession := postJSON(apiKeyTwo.Key, "shared-session", "/find-missing", `{"model":"gpt-5.4","mem_object_names":["blob-user-one-shared"]}`)
	defer findSharedSession.Body.Close()
	require.Equal(t, http.StatusOK, findSharedSession.StatusCode)
	var missingSharedSession map[string][]string
	require.NoError(t, json.NewDecoder(findSharedSession.Body).Decode(&missingSharedSession))
	require.Equal(t, []string{"blob-user-one-shared"}, missingSharedSession["unknown_memory_names"])
}

func TestAugmentLegacyCheckpointBlobsKeepsAuthenticatedNamespaceAcrossCompatFlow(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	user := &service.User{
		ID:       33,
		Email:    "checkpoint@example.com",
		Username: "checkpoint-user",
		Role:     service.RoleAdmin,
		Status:   service.StatusActive,
	}
	apiKey := &service.APIKey{
		ID:        133,
		UserID:    user.ID,
		Key:       "sk-compat-checkpoint",
		Name:      "compat-checkpoint",
		Status:    service.StatusActive,
		CreatedAt: time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC),
		User:      user,
	}
	group := service.Group{
		ID:                 103,
		Name:               "OpenAI",
		Platform:           service.PlatformOpenAI,
		Status:             service.StatusActive,
		Hydrated:           true,
		DefaultMappedModel: "gpt-5.4",
	}
	markAugmentRuntimeAPIKey(apiKey, &group)

	pluginService := service.NewAugmentPluginService(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		augmentPluginAuthStub{},
		augmentPluginUserStub{user: user},
		augmentPluginAPIKeyStub{
			apiKeyByValue: map[string]*service.APIKey{apiKey.Key: apiKey},
			keysByUser:    map[int64][]service.APIKey{user.ID: {*apiKey}},
			availableByUser: map[int64][]service.Group{
				user.ID: {group},
			},
		},
		augmentPluginSubscriptionStub{},
		augmentPluginSettingStub{
			public: &service.PublicSettings{
				SiteName:   "逐梦站",
				APIBaseURL: "http://127.0.0.1:18081",
			},
		},
	)

	authHandler := NewAuthHandler(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		pluginService,
	)

	router := gin.New()
	router.POST("/batch-upload", authHandler.AugmentLegacyBatchUpload)
	router.POST("/checkpoint-blobs", authHandler.AugmentLegacyCheckpointBlobs)
	router.POST("/agents/codebase-retrieval", authHandler.AugmentLegacyCodebaseRetrieval)

	server := httptest.NewServer(router)
	defer server.Close()

	client := server.Client()
	postJSON := func(path string, body string) *http.Response {
		req, err := http.NewRequest(http.MethodPost, server.URL+path, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+apiKey.Key)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		require.NoError(t, err)
		return resp
	}

	uploadResp := postJSON("/batch-upload", `{"blobs":[{"blob_name":"blob-a","path":"src/main.go","content":"package main\nfunc main(){}\n"}]}`)
	defer uploadResp.Body.Close()
	require.Equal(t, http.StatusOK, uploadResp.StatusCode)

	checkpointResp := postJSON("/checkpoint-blobs", `{"blobs":{"checkpoint_id":"","added_blobs":["blob-a"],"deleted_blobs":[]}}`)
	defer checkpointResp.Body.Close()
	require.Equal(t, http.StatusOK, checkpointResp.StatusCode)

	var checkpointBody map[string]any
	require.NoError(t, json.NewDecoder(checkpointResp.Body).Decode(&checkpointBody))
	checkpointID, ok := checkpointBody["new_checkpoint_id"].(string)
	require.True(t, ok)
	require.NotEmpty(t, checkpointID)

	retrievalResp := postJSON("/agents/codebase-retrieval", `{"information_request":"find main entry","blobs":{"checkpoint_id":"`+checkpointID+`","added_blobs":[],"deleted_blobs":[]},"dialog":[],"max_output_length":2000}`)
	defer retrievalResp.Body.Close()
	require.Equal(t, http.StatusOK, retrievalResp.StatusCode)

	var retrievalBody map[string]string
	require.NoError(t, json.NewDecoder(retrievalResp.Body).Decode(&retrievalBody))
	require.Contains(t, retrievalBody["formatted_retrieval"], "src/main.go")
	require.Contains(t, retrievalBody["formatted_retrieval"], "find main entry")
}

func TestAugmentLegacyCompatNamespacesAreScopedPerAPIKeyPrincipal(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	user := &service.User{
		ID:       44,
		Email:    "same-user@example.com",
		Username: "same-user",
		Role:     service.RoleAdmin,
		Status:   service.StatusActive,
	}
	apiKeyOne := &service.APIKey{
		ID:        144,
		UserID:    user.ID,
		Key:       "sk-same-user-one",
		Name:      "same-user-one",
		Status:    service.StatusActive,
		CreatedAt: time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC),
		User:      user,
	}
	apiKeyTwo := &service.APIKey{
		ID:        145,
		UserID:    user.ID,
		Key:       "sk-same-user-two",
		Name:      "same-user-two",
		Status:    service.StatusActive,
		CreatedAt: time.Date(2026, 4, 22, 12, 5, 0, 0, time.UTC),
		User:      user,
	}
	group := service.Group{
		ID:                 104,
		Name:               "OpenAI",
		Platform:           service.PlatformOpenAI,
		Status:             service.StatusActive,
		Hydrated:           true,
		DefaultMappedModel: "gpt-5.4",
	}
	markAugmentRuntimeAPIKey(apiKeyOne, &group)
	markAugmentRuntimeAPIKey(apiKeyTwo, &group)

	pluginService := service.NewAugmentPluginService(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		augmentPluginAuthStub{},
		augmentPluginUserStub{user: user},
		augmentPluginAPIKeyStub{
			apiKeyByValue: map[string]*service.APIKey{
				apiKeyOne.Key: apiKeyOne,
				apiKeyTwo.Key: apiKeyTwo,
			},
			keysByUser: map[int64][]service.APIKey{
				user.ID: {*apiKeyOne, *apiKeyTwo},
			},
			availableByUser: map[int64][]service.Group{
				user.ID: {group},
			},
		},
		augmentPluginSubscriptionStub{},
		augmentPluginSettingStub{
			public: &service.PublicSettings{
				SiteName:   "逐梦站",
				APIBaseURL: "http://127.0.0.1:18081",
			},
		},
	)

	authHandler := NewAuthHandler(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		pluginService,
	)

	router := gin.New()
	router.POST("/batch-upload", authHandler.AugmentLegacyBatchUpload)
	router.POST("/find-missing", authHandler.AugmentLegacyFindMissing)

	server := httptest.NewServer(router)
	defer server.Close()

	client := server.Client()
	postJSON := func(apiKey string, sessionID string, path string, body string) *http.Response {
		req, err := http.NewRequest(http.MethodPost, server.URL+path, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-request-session-id", sessionID)
		resp, err := client.Do(req)
		require.NoError(t, err)
		return resp
	}

	uploadResp := postJSON(apiKeyOne.Key, "shared-session", "/batch-upload", `{"blobs":[{"blob_name":"blob-same-user","path":"src/same.go","content":"package same"}]}`)
	defer uploadResp.Body.Close()
	require.Equal(t, http.StatusOK, uploadResp.StatusCode)

	findResp := postJSON(apiKeyTwo.Key, "shared-session", "/find-missing", `{"model":"gpt-5.4","mem_object_names":["blob-same-user"]}`)
	defer findResp.Body.Close()
	require.Equal(t, http.StatusOK, findResp.StatusCode)

	var findBody map[string][]string
	require.NoError(t, json.NewDecoder(findResp.Body).Decode(&findBody))
	require.Equal(t, []string{"blob-same-user"}, findBody["unknown_memory_names"])
}

func newAugmentLegacyRuntimeTestServer(t *testing.T) (*httptest.Server, string, *string) {
	t.Helper()
	return newAugmentLegacyRuntimeTestServerWithGroups(t, []service.Group{
		{
			ID:                 101,
			Name:               "OpenAI",
			Platform:           service.PlatformOpenAI,
			Status:             service.StatusActive,
			Hydrated:           true,
			DefaultMappedModel: "gpt-5.4",
		},
	})
}

func markAugmentRuntimeAPIKey(apiKey *service.APIKey, group *service.Group) {
	if apiKey == nil || group == nil {
		return
	}
	group.AugmentGatewayEntitled = true
	product := service.AugmentClientProductZhumeng
	apiKey.RestrictedClientProduct = &product
	apiKey.GroupID = &group.ID
	apiKey.Group = group
}

func newAugmentLegacyRuntimeTestServerWithGroups(t *testing.T, groups []service.Group) (*httptest.Server, string, *string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	groups = append([]service.Group(nil), groups...)
	for idx := range groups {
		groups[idx].AugmentGatewayEntitled = true
	}

	user := &service.User{
		ID:       2,
		Email:    "compat@example.com",
		Username: "compat",
		Role:     service.RoleAdmin,
		Status:   service.StatusActive,
	}
	apiKey := &service.APIKey{
		ID:        2,
		UserID:    user.ID,
		Key:       "sk-compat-runtime",
		Name:      "compat-runtime",
		Status:    service.StatusActive,
		CreatedAt: time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC),
		User:      user,
	}
	restrictedClientProduct := service.AugmentClientProductZhumeng
	apiKey.RestrictedClientProduct = &restrictedClientProduct
	if len(groups) > 0 {
		currentGroup := groups[0]
		apiKey.GroupID = &currentGroup.ID
		apiKey.Group = &currentGroup
	}
	pluginService := service.NewAugmentPluginService(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		augmentPluginAuthStub{},
		augmentPluginUserStub{user: user},
		augmentPluginAPIKeyStub{
			apiKeyByValue: map[string]*service.APIKey{apiKey.Key: apiKey},
			keysByUser:    map[int64][]service.APIKey{user.ID: {*apiKey}},
			availableByUser: map[int64][]service.Group{
				user.ID: append([]service.Group(nil), groups...),
			},
		},
		augmentPluginSubscriptionStub{},
		augmentPluginSettingStub{
			public: &service.PublicSettings{
				SiteName:   "逐梦站",
				APIBaseURL: "http://127.0.0.1:18081",
			},
		},
	)
	pluginService.StoreLegacyBlobsForNamespace(buildAugmentLegacyNamespace(&service.AugmentPluginPrincipal{
		User:   user,
		APIKey: apiKey,
	}, ""), []service.AugmentLegacyUploadedBlob{
		{BlobName: "blob-a", Path: "src/main.go", Content: "package main\nfunc main(){}\n"},
	})

	authHandler := NewAuthHandler(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		pluginService,
	)

	capturedOpenAIBody := ""
	router := gin.New()
	router.POST("/chat", authHandler.AugmentLegacyChat)
	router.POST("/chat-stream", authHandler.AugmentLegacyChatStream)
	router.POST("/prompt-enhancer", authHandler.AugmentLegacyPromptEnhancer)
	router.POST("/openai/v1/chat/completions", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		capturedOpenAIBody = string(body)
		c.JSON(http.StatusOK, gin.H{
			"id":      "chatcmpl-compat",
			"object":  "chat.completion",
			"created": 1710000000,
			"model":   "gpt-5.4",
			"choices": []gin.H{
				{
					"index": 0,
					"message": gin.H{
						"role":    "assistant",
						"content": "hello from compat",
					},
					"finish_reason": "stop",
				},
			},
			"usage": gin.H{
				"prompt_tokens":     10,
				"completion_tokens": 5,
				"total_tokens":      15,
			},
		})
	})

	server := httptest.NewServer(router)
	return server, apiKey.Key, &capturedOpenAIBody
}

type augmentGatewayRouteFakeExecutor struct {
	mu sync.Mutex

	completeResult service.AugmentGatewayProviderResult
	completeFunc   func(req service.AugmentGatewayProviderRequest, callIndex int) (service.AugmentGatewayProviderResult, error)
	streamChunks   []service.AugmentGatewayProviderChunk
	streamFunc     func(req service.AugmentGatewayProviderRequest, callIndex int, emit func(service.AugmentGatewayProviderChunk) error) error

	completeRequests []service.AugmentGatewayProviderRequest
	streamRequests   []service.AugmentGatewayProviderRequest
}

func (e *augmentGatewayRouteFakeExecutor) Complete(ctx context.Context, req service.AugmentGatewayProviderRequest) (service.AugmentGatewayProviderResult, error) {
	captured := e.captureRequest(req, false)
	e.mu.Lock()
	callIndex := len(e.completeRequests)
	e.completeRequests = append(e.completeRequests, captured)
	fn := e.completeFunc
	result := e.completeResult
	e.mu.Unlock()
	if fn != nil {
		return fn(req, callIndex)
	}
	return result, nil
}

func (e *augmentGatewayRouteFakeExecutor) Stream(ctx context.Context, req service.AugmentGatewayProviderRequest, emit func(service.AugmentGatewayProviderChunk) error) error {
	captured := e.captureRequest(req, true)
	e.mu.Lock()
	callIndex := len(e.streamRequests)
	e.streamRequests = append(e.streamRequests, captured)
	fn := e.streamFunc
	chunks := append([]service.AugmentGatewayProviderChunk(nil), e.streamChunks...)
	e.mu.Unlock()
	if fn != nil {
		return fn(req, callIndex, emit)
	}
	for _, chunk := range chunks {
		if err := emit(chunk); err != nil {
			return err
		}
	}
	return nil
}

func (e *augmentGatewayRouteFakeExecutor) CompleteRequests() []service.AugmentGatewayProviderRequest {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]service.AugmentGatewayProviderRequest(nil), e.completeRequests...)
}

func (e *augmentGatewayRouteFakeExecutor) StreamRequests() []service.AugmentGatewayProviderRequest {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]service.AugmentGatewayProviderRequest(nil), e.streamRequests...)
}

func (e *augmentGatewayRouteFakeExecutor) captureRequest(req service.AugmentGatewayProviderRequest, stream bool) service.AugmentGatewayProviderRequest {
	req.Provider = req.Model.Provider
	req.ModelID = req.Model.ID
	req.UpstreamModel = req.Model.UpstreamModel
	if req.RawBody != nil {
		raw, err := json.Marshal(req.RawBody)
		if err == nil {
			var cloned map[string]any
			if json.Unmarshal(raw, &cloned) == nil {
				req.RawBody = cloned
			}
		}
	}
	if req.Provider == service.AugmentGatewayProviderDeepSeek {
		body, err := service.SanitizeAugmentGatewayDeepSeekChatCompletionsRequest(req.Model, req.RawBody)
		if err == nil {
			req.RawBody = body
		}
	}
	if req.RawBody != nil {
		req.RawBody["stream"] = stream
	}
	return req
}

func newAugmentGatewayRuntimeTestServer(t *testing.T, executor service.AugmentGatewayProviderExecutor) (*httptest.Server, string, *int) {
	t.Helper()
	return newAugmentGatewayRuntimeTestServerWithConfig(t, executor, config.GatewayAugmentConfig{
		Enabled:       true,
		EnabledModels: []string{"gpt-5.4", "gpt-5.5", "gpt-5.4-mini", "deepseek-v4-pro", "deepseek-v4-flash"},
		ProviderGroups: config.GatewayAugmentProviderGroupsConfig{
			OpenAI:   1001,
			DeepSeek: 1002,
		},
	})
}

func newAugmentGatewayRuntimeTestServerWithConfig(t *testing.T, executor service.AugmentGatewayProviderExecutor, gatewayCfg config.GatewayAugmentConfig) (*httptest.Server, string, *int) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	user := &service.User{
		ID:       2,
		Email:    "compat@example.com",
		Username: "compat",
		Role:     service.RoleAdmin,
		Status:   service.StatusActive,
	}
	apiKey := &service.APIKey{
		ID:        2,
		UserID:    user.ID,
		Key:       "sk-augment-gateway-runtime",
		Name:      "augment-gateway-runtime",
		Status:    service.StatusActive,
		CreatedAt: time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC),
		User:      user,
	}
	restrictedClientProduct := service.AugmentClientProductZhumeng
	apiKey.RestrictedClientProduct = &restrictedClientProduct
	group := service.Group{
		ID:                     101,
		Name:                   "OpenAI",
		Platform:               service.PlatformOpenAI,
		Status:                 service.StatusActive,
		Hydrated:               true,
		AugmentGatewayEntitled: true,
		DefaultMappedModel:     "gpt-5.4",
	}
	apiKey.GroupID = &group.ID
	apiKey.Group = &group

	pluginService := service.NewAugmentPluginService(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		augmentPluginAuthStub{},
		augmentPluginUserStub{user: user},
		augmentPluginAPIKeyStub{
			apiKeyByValue: map[string]*service.APIKey{apiKey.Key: apiKey},
			keysByUser:    map[int64][]service.APIKey{user.ID: {*apiKey}},
			availableByUser: map[int64][]service.Group{
				user.ID: {group},
			},
		},
		augmentPluginSubscriptionStub{},
		augmentPluginSettingStub{
			public: &service.PublicSettings{
				SiteName:   "逐梦站",
				APIBaseURL: "http://127.0.0.1:18081",
			},
		},
	)

	registry := service.NewAugmentGatewayModelRegistry(gatewayCfg)
	router := service.NewAugmentGatewayRouter(registry)
	turnStore := service.NewAugmentGatewayReasoningTurnStore()
	gatewayService := service.NewAugmentGatewayService(
		&config.Config{Gateway: config.GatewayConfig{Augment: gatewayCfg}},
		registry,
		router,
		executor,
		turnStore,
	)
	pluginService.StoreLegacyBlobsForNamespace(buildAugmentLegacyNamespace(&service.AugmentPluginPrincipal{
		User:   user,
		APIKey: apiKey,
	}, ""), []service.AugmentLegacyUploadedBlob{
		{BlobName: "blob-a", Path: "src/main.go", Content: "package main\nfunc main(){}\n"},
	})

	authHandler := NewAuthHandler(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		pluginService,
		gatewayService,
	)

	loopbackCalls := 0
	routerEngine := gin.New()
	routerEngine.POST("/chat", authHandler.AugmentLegacyChat)
	routerEngine.POST("/chat-stream", authHandler.AugmentLegacyChatStream)
	routerEngine.POST("/prompt-enhancer", authHandler.AugmentLegacyPromptEnhancer)
	routerEngine.POST("/instruction-stream", authHandler.AugmentLegacyInstructionStream)
	routerEngine.POST("/smart-paste-stream", authHandler.AugmentLegacySmartPasteStream)
	routerEngine.POST("/generate-commit-message-stream", authHandler.AugmentLegacyGenerateCommitMessageStream)
	routerEngine.POST("/next_edit_loc", authHandler.AugmentLegacyNextEditLocation)
	routerEngine.POST("/next-edit-stream", authHandler.AugmentLegacyNextEditStream)
	routerEngine.POST("/openai/v1/chat/completions", func(c *gin.Context) {
		loopbackCalls++
		c.JSON(http.StatusOK, gin.H{
			"id":      "chatcmpl-local-loopback",
			"object":  "chat.completion",
			"created": 1710000000,
			"model":   "gpt-5.4",
			"choices": []gin.H{
				{
					"index": 0,
					"message": gin.H{
						"role":    "assistant",
						"content": "local loopback text",
					},
					"finish_reason": "stop",
				},
			},
			"usage": gin.H{
				"prompt_tokens":     10,
				"completion_tokens": 5,
				"total_tokens":      15,
			},
		})
	})

	server := httptest.NewServer(routerEngine)
	return server, apiKey.Key, &loopbackCalls
}

func postAugmentGatewayRuntimeJSON(t *testing.T, server *httptest.Server, apiKey, path, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, server.URL+path, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := server.Client().Do(req)
	require.NoError(t, err)
	return resp
}

func requireAugmentGatewayStreamHasNodeType(t *testing.T, chunks []map[string]any, nodeType float64) {
	t.Helper()
	for _, chunk := range chunks {
		for _, node := range augmentGatewayContractNodes(chunk) {
			if node["type"] == nodeType {
				return
			}
		}
	}
	require.Failf(t, "missing node type", "node type %v not found in chunks %#v", nodeType, chunks)
}

func augmentGatewayContractNodes(chunk map[string]any) []map[string]any {
	nodes, _ := chunk["nodes"].([]any)
	out := make([]map[string]any, 0, len(nodes))
	for _, rawNode := range nodes {
		node, ok := rawNode.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, node)
	}
	return out
}

func requireAugmentGatewayDeepSeekReplayBody(t *testing.T, body map[string]any, wantReasoning string) {
	t.Helper()
	require.Equal(t, "max", body["reasoning_effort"])
	require.Equal(t, map[string]any{"type": "enabled"}, body["thinking"])
	require.NotContains(t, body, "tool_choice")
	require.NotEmpty(t, body["tools"])

	messages, ok := body["messages"].([]any)
	require.True(t, ok)
	var assistant map[string]any
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok || msg["role"] != "assistant" {
			continue
		}
		if toolCalls, ok := msg["tool_calls"].([]any); ok && len(toolCalls) > 0 {
			assistant = msg
			break
		}
	}
	require.NotNil(t, assistant, "expected assistant tool-call message")
	require.Contains(t, assistant, "content")
	require.Equal(t, "", assistant["content"])
	require.Contains(t, assistant, "reasoning_content")
	require.Equal(t, wantReasoning, assistant["reasoning_content"])
}

func requireAugmentGatewayDeepSeekReplayToolArguments(t *testing.T, body map[string]any, toolCallID, wantArguments string) {
	t.Helper()
	messages, ok := body["messages"].([]any)
	require.True(t, ok)
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok || msg["role"] != "assistant" {
			continue
		}
		toolCalls, _ := msg["tool_calls"].([]any)
		for _, rawToolCall := range toolCalls {
			toolCall, ok := rawToolCall.(map[string]any)
			if !ok || toolCall["id"] != toolCallID {
				continue
			}
			function, ok := toolCall["function"].(map[string]any)
			require.True(t, ok)
			require.JSONEq(t, wantArguments, fmt.Sprint(function["arguments"]))
			return
		}
	}
	require.Failf(t, "missing tool call", "tool call %s not found in %#v", toolCallID, body)
}

func requireAugmentGatewayDeepSeekToolCallResultPairs(t *testing.T, body map[string]any, toolCallIDs []string) {
	t.Helper()
	messages, ok := body["messages"].([]any)
	require.True(t, ok)
	cursor := 0
	for _, toolCallID := range toolCallIDs {
		assistantIndex := -1
		for idx := cursor; idx < len(messages); idx++ {
			msg, ok := messages[idx].(map[string]any)
			if !ok || msg["role"] != "assistant" {
				continue
			}
			if !augmentGatewayRawAssistantHasToolCallID(msg, toolCallID) {
				continue
			}
			assistantIndex = idx
			break
		}
		require.NotEqual(t, -1, assistantIndex, "assistant tool call %s not found in %#v", toolCallID, messages)
		require.Less(t, assistantIndex+1, len(messages), "tool result for %s must immediately follow assistant tool call", toolCallID)
		toolMessage, ok := messages[assistantIndex+1].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "tool", toolMessage["role"])
		require.Equal(t, toolCallID, toolMessage["tool_call_id"])
		cursor = assistantIndex + 2
	}
}

func augmentGatewayRawAssistantHasToolCallID(msg map[string]any, toolCallID string) bool {
	toolCalls, _ := msg["tool_calls"].([]any)
	for _, rawToolCall := range toolCalls {
		toolCall, ok := rawToolCall.(map[string]any)
		if ok && toolCall["id"] == toolCallID {
			return true
		}
	}
	return false
}

func augmentGatewayRouteCodebaseToolCall(id string) service.AugmentGatewayToolCall {
	return augmentGatewayRouteCodebaseToolCallWithArguments(id, `{"query":"gateway replay"}`)
}

func augmentGatewayRouteCodebaseToolCallWithArguments(id, arguments string) service.AugmentGatewayToolCall {
	return service.AugmentGatewayToolCall{
		ID:   id,
		Type: "function",
		Function: service.AugmentGatewayToolCallFunction{
			Name:      "codebase-retrieval",
			Arguments: arguments,
		},
	}
}

type augmentLegacyContractLoopbackCall struct {
	Body          string
	Authorization string
	Cookie        string
}

type augmentLegacyContractLoopbackRecorder struct {
	mu    sync.Mutex
	calls []augmentLegacyContractLoopbackCall
}

func (r *augmentLegacyContractLoopbackRecorder) Record(c *gin.Context, body string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, augmentLegacyContractLoopbackCall{
		Body:          body,
		Authorization: c.GetHeader("Authorization"),
		Cookie:        c.GetHeader("Cookie"),
	})
}

func (r *augmentLegacyContractLoopbackRecorder) Calls() []augmentLegacyContractLoopbackCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]augmentLegacyContractLoopbackCall(nil), r.calls...)
}

func newAugmentLegacyAuxiliaryContractTestServer(t *testing.T) (*httptest.Server, string, *augmentLegacyContractLoopbackRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	user := &service.User{
		ID:       2,
		Email:    "compat@example.com",
		Username: "compat",
		Role:     service.RoleAdmin,
		Status:   service.StatusActive,
	}
	apiKey := &service.APIKey{
		ID:        2,
		UserID:    user.ID,
		Key:       "sk-compat-runtime",
		Name:      "compat-runtime",
		Status:    service.StatusActive,
		CreatedAt: time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC),
		User:      user,
	}
	group := service.Group{
		ID:                 101,
		Name:               "OpenAI",
		Platform:           service.PlatformOpenAI,
		Status:             service.StatusActive,
		Hydrated:           true,
		DefaultMappedModel: "gpt-5.4",
	}
	markAugmentRuntimeAPIKey(apiKey, &group)

	pluginService := service.NewAugmentPluginService(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		augmentPluginAuthStub{},
		augmentPluginUserStub{user: user},
		augmentPluginAPIKeyStub{
			apiKeyByValue: map[string]*service.APIKey{apiKey.Key: apiKey},
			keysByUser:    map[int64][]service.APIKey{user.ID: {*apiKey}},
			availableByUser: map[int64][]service.Group{
				user.ID: {group},
			},
		},
		augmentPluginSubscriptionStub{},
		augmentPluginSettingStub{
			public: &service.PublicSettings{
				SiteName:   "逐梦站",
				APIBaseURL: "http://127.0.0.1:18081",
			},
		},
	)
	pluginService.StoreLegacyBlobsForNamespace(buildAugmentLegacyNamespace(&service.AugmentPluginPrincipal{
		User:   user,
		APIKey: apiKey,
	}, ""), []service.AugmentLegacyUploadedBlob{
		{BlobName: "blob-a", Path: "src/main.go", Content: "package main\nfunc main(){}\n"},
	})

	authHandler := NewAuthHandler(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		pluginService,
	)

	recorder := &augmentLegacyContractLoopbackRecorder{}
	router := gin.New()
	router.POST("/prompt-enhancer", authHandler.AugmentLegacyPromptEnhancer)
	router.POST("/instruction-stream", authHandler.AugmentLegacyInstructionStream)
	router.POST("/smart-paste-stream", authHandler.AugmentLegacySmartPasteStream)
	router.POST("/generate-commit-message-stream", authHandler.AugmentLegacyGenerateCommitMessageStream)
	router.POST("/next_edit_loc", authHandler.AugmentLegacyNextEditLocation)
	router.POST("/next-edit-stream", authHandler.AugmentLegacyNextEditStream)
	router.POST("/openai/v1/chat/completions", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		recorder.Record(c, string(body))
		c.JSON(http.StatusOK, gin.H{
			"id":      "chatcmpl-contract",
			"object":  "chat.completion",
			"created": 1710000000,
			"model":   "gpt-5.4",
			"choices": []gin.H{
				{
					"index": 0,
					"message": gin.H{
						"role":    "assistant",
						"content": "contract upstream text",
					},
					"finish_reason": "stop",
				},
			},
			"usage": gin.H{
				"prompt_tokens":     10,
				"completion_tokens": 4,
				"total_tokens":      14,
			},
		})
	})

	return httptest.NewServer(router), apiKey.Key, recorder
}

func postAugmentContractJSON(t *testing.T, server *httptest.Server, apiKey string, path string, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, server.URL+path, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := server.Client().Do(req)
	require.NoError(t, err)
	return resp
}

func requireAugmentEndpointFixtureHasNoSecrets(t *testing.T, fixture string) {
	t.Helper()
	lower := strings.ToLower(fixture)
	for _, forbidden := range []string{"authorization", "cookie", "refresh_token", "access_token", "session_object", "full_session"} {
		require.NotContains(t, lower, forbidden)
	}
}

func decodeAugmentContractObject(t *testing.T, body string) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &out))
	return out
}

func decodeAugmentContractObjectFromReader(t *testing.T, r io.Reader) map[string]any {
	t.Helper()
	return decodeAugmentContractObject(t, readBody(t, r))
}

func decodeAugmentContractNDJSON(t *testing.T, r io.Reader) []map[string]any {
	t.Helper()
	lines := nonEmptyLines(t, r)
	chunks := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		chunks = append(chunks, decodeAugmentContractObject(t, line))
	}
	return chunks
}

func requireAugmentContractNoStreamSequencing(t *testing.T, chunk map[string]any) {
	t.Helper()
	_, hasStreamID := chunk["stream_id"]
	require.False(t, hasStreamID, "current auxiliary Augment compatibility chunks do not emit stream_id")
	_, hasSeq := chunk["seq"]
	require.False(t, hasSeq, "current auxiliary Augment compatibility chunks do not emit seq")
}

func stringValueOrRawJSON(t *testing.T, value any) string {
	t.Helper()
	switch v := value.(type) {
	case string:
		return v
	default:
		b, err := json.Marshal(v)
		require.NoError(t, err)
		return string(b)
	}
}

func readBody(t *testing.T, r io.Reader) string {
	t.Helper()
	b, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(bytes.TrimSpace(b))
}

func nonEmptyLines(t *testing.T, r io.Reader) []string {
	t.Helper()
	body, err := io.ReadAll(r)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

func readAugmentCaptureSummaryRows(t *testing.T, captureDir string) []map[string]any {
	t.Helper()
	f, err := os.Open(filepath.Join(captureDir, "safe-summary.jsonl"))
	require.NoError(t, err)
	defer f.Close()

	rows := make([]map[string]any, 0)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var row map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &row))
		rows = append(rows, row)
	}
	require.NoError(t, scanner.Err())
	return rows
}

func readAugmentCaptureRawText(t *testing.T, captureDir string, relativePath string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(captureDir, filepath.FromSlash(relativePath)))
	require.NoError(t, err)
	return string(body)
}
