package handler

import (
	"bufio"
	"bytes"
	"encoding/json"
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

func newAugmentLegacyRuntimeTestServerWithGroups(t *testing.T, groups []service.Group) (*httptest.Server, string, *string) {
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
