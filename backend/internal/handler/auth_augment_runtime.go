package handler

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const (
	augmentResponseNodeRawResponse  = 0
	augmentResponseNodeToolUse      = 5
	augmentResponseNodeToolUseStart = 7
	augmentResponseNodeThinking     = 8
	augmentResponseNodeTokenUsage   = 10

	augmentRequestNodeText       = 0
	augmentRequestNodeToolResult = 1
	augmentToolResultContentText = 1

	augmentStopReasonUnspecified     = 0
	augmentStopReasonEndTurn         = 1
	augmentStopReasonMaxTokens       = 2
	augmentStopReasonToolUseRequired = 3
	augmentStopReasonSafety          = 4

	augmentLegacyUnsupportedWrapperPayloadCode = "UNSUPPORTED_WRAPPER_PAYLOAD"
	augmentLegacyEmptyUserInputCode            = "EMPTY_USER_INPUT"
)

const augmentLegacyGatewayCodebaseRetrievalPolicy = `Augment Gateway tool policy for codebase-retrieval:
- 调用 codebase-retrieval 时，用用户语言写完整、具体、可检索的问题；必须包含用户关心的仓库、模块、端点、路由、controller/service/middleware/adapter、可能文件路径和符号名。
- 不要把同一个检索意图拆成多个短关键词查询，例如只写 "DeepSeek tool replay" 或 "reasoning handling"；优先发起一个上下文充分的完整查询。
- 如果第一次检索证据不足，最多再发起一次更聚焦的补充查询，然后基于已有证据回答并明确缺口。`

type augmentLegacyBatchUploadBlob struct {
	BlobName string `json:"blob_name"`
	Path     string `json:"path"`
	Content  string `json:"content"`
}

type augmentLegacyBatchUploadRequest struct {
	Blobs []augmentLegacyBatchUploadBlob `json:"blobs"`
}

type augmentLegacyFindMissingRequest struct {
	Model          string   `json:"model"`
	MemObjectNames []string `json:"mem_object_names"`
}

type augmentLegacyChatHistoryItem struct {
	RequestMessage      string                  `json:"request_message"`
	RequestMessageCamel string                  `json:"requestMessage"`
	Message             string                  `json:"message"`
	ResponseText        string                  `json:"response_text"`
	ResponseTextCamel   string                  `json:"responseText"`
	Response            string                  `json:"response"`
	Text                string                  `json:"text"`
	RequestID           string                  `json:"request_id"`
	RequestIDCamel      string                  `json:"requestId"`
	RequestNodes        []augmentLegacyChatNode `json:"request_nodes"`
	RequestNodesCamel   []augmentLegacyChatNode `json:"requestNodes"`
	ResponseNodes       []augmentLegacyChatNode `json:"response_nodes"`
	ResponseNodesCamel  []augmentLegacyChatNode `json:"responseNodes"`
}

type augmentLegacyToolDefinition struct {
	Name            string         `json:"name"`
	Description     string         `json:"description"`
	InputSchema     map[string]any `json:"input_schema"`
	InputSchemaJSON string         `json:"input_schema_json"`
}

type augmentLegacyChatTextNode struct {
	Content string `json:"content"`
}

type augmentLegacyChatNode struct {
	ID                  int                          `json:"id"`
	Type                *int                         `json:"type"`
	NodeType            *int                         `json:"node_type"`
	NodeTypeCamel       *int                         `json:"nodeType"`
	Content             string                       `json:"content"`
	TextNode            *augmentLegacyChatTextNode   `json:"text_node"`
	TextNodeCamel       *augmentLegacyChatTextNode   `json:"textNode"`
	ToolUse             *augmentLegacyToolUseNode    `json:"tool_use"`
	ToolUseCamel        *augmentLegacyToolUseNode    `json:"toolUse"`
	ToolResultNode      *augmentLegacyToolResultNode `json:"tool_result_node"`
	ToolResultNodeCamel *augmentLegacyToolResultNode `json:"toolResultNode"`
}

type augmentLegacyToolUseNode struct {
	ToolUseID      string `json:"tool_use_id"`
	ToolUseIDCamel string `json:"toolUseId"`
	ToolName       string `json:"tool_name"`
	ToolNameCamel  string `json:"toolName"`
	InputJSON      string `json:"input_json"`
	InputJSONCamel string `json:"inputJson"`
}

type augmentLegacyToolResultNode struct {
	ToolUseID         string                               `json:"tool_use_id"`
	ToolUseIDCamel    string                               `json:"toolUseId"`
	Content           string                               `json:"content"`
	ContentNodes      []augmentLegacyToolResultContentNode `json:"content_nodes"`
	ContentNodesCamel []augmentLegacyToolResultContentNode `json:"contentNodes"`
}

type augmentLegacyToolResultContentNode struct {
	Type             *int   `json:"type"`
	NodeType         *int   `json:"node_type"`
	NodeTypeCamel    *int   `json:"nodeType"`
	TextContent      string `json:"text_content"`
	TextContentCamel string `json:"textContent"`
}

type augmentLegacyChatRequest struct {
	Model                           string                              `json:"model"`
	Message                         string                              `json:"message"`
	Prompt                          string                              `json:"prompt"`
	Instruction                     string                              `json:"instruction"`
	ChatHistory                     []augmentLegacyChatHistoryItem      `json:"chat_history"`
	ChatHistoryCamel                []augmentLegacyChatHistoryItem      `json:"chatHistory"`
	Blobs                           augmentLegacyCheckpointBlobsPayload `json:"blobs"`
	UserGuidedBlobs                 []string                            `json:"user_guided_blobs"`
	UserGuidedBlobsCamel            []string                            `json:"userGuidedBlobs"`
	ExternalSourceIDs               []string                            `json:"external_source_ids"`
	ExternalSourceIDsCamel          []string                            `json:"externalSourceIds"`
	DisableAutoExternalSources      bool                                `json:"disable_auto_external_sources"`
	DisableAutoExternalSourcesCamel *bool                               `json:"disableAutoExternalSources"`
	DisableRetrieval                bool                                `json:"disable_retrieval"`
	DisableRetrievalCamel           *bool                               `json:"disableRetrieval"`
	UserGuidelines                  string                              `json:"user_guidelines"`
	UserGuidelinesCamel             string                              `json:"userGuidelines"`
	WorkspaceGuidelines             string                              `json:"workspace_guidelines"`
	WorkspaceGuidelinesCamel        string                              `json:"workspaceGuidelines"`
	ToolDefinitions                 []augmentLegacyToolDefinition       `json:"tool_definitions"`
	ToolDefinitionsCamel            []augmentLegacyToolDefinition       `json:"toolDefinitions"`
	Rules                           []any                               `json:"rules"`
	ConversationID                  string                              `json:"conversation_id"`
	ConversationIDCamel             string                              `json:"conversationId"`
	Path                            string                              `json:"path"`
	Lang                            string                              `json:"lang"`
	Language                        string                              `json:"language"`
	Prefix                          string                              `json:"prefix"`
	SelectedCode                    string                              `json:"selected_code"`
	SelectedCodeCamel               string                              `json:"selectedCode"`
	SelectedText                    string                              `json:"selected_text"`
	SelectedTextCamel               string                              `json:"selectedText"`
	Suffix                          string                              `json:"suffix"`
	Nodes                           []augmentLegacyChatNode             `json:"nodes"`
	RequestNodes                    []augmentLegacyChatNode             `json:"request_nodes"`
	RequestNodesCamel               []augmentLegacyChatNode             `json:"requestNodes"`
	StructuredRequestNodes          []augmentLegacyChatNode             `json:"structured_request_nodes"`
	StructuredRequestNodesCamel     []augmentLegacyChatNode             `json:"structuredRequestNodes"`
	SystemPrompt                    string                              `json:"system_prompt"`
	SystemPromptCamel               string                              `json:"systemPrompt"`
	SystemPromptAppend              string                              `json:"system_prompt_append"`
	SystemPromptAppendCamel         string                              `json:"systemPromptAppend"`
	SystemPromptReplacements        []map[string]any                    `json:"system_prompt_replacements"`
	SystemPromptReplacementsCamel   []map[string]any                    `json:"systemPromptReplacements"`
}

type augmentLegacyResolvedChatUserInput struct {
	Text           string
	Source         string
	HasToolResults bool
}

type augmentLegacyInstructionRequest struct {
	Model                       string                              `json:"model"`
	Instruction                 string                              `json:"instruction"`
	Prompt                      string                              `json:"prompt"`
	Prefix                      string                              `json:"prefix"`
	SelectedText                string                              `json:"selected_text"`
	SelectedTextCamel           string                              `json:"selectedText"`
	Suffix                      string                              `json:"suffix"`
	Path                        string                              `json:"path"`
	Lang                        string                              `json:"lang"`
	Language                    string                              `json:"language"`
	BlobName                    string                              `json:"blob_name"`
	Blobs                       augmentLegacyCheckpointBlobsPayload `json:"blobs"`
	ChatHistory                 []augmentLegacyChatHistoryItem      `json:"chat_history"`
	ChatHistoryCamel            []augmentLegacyChatHistoryItem      `json:"chatHistory"`
	UserGuidelines              string                              `json:"user_guidelines"`
	UserGuidelinesCamel         string                              `json:"userGuidelines"`
	WorkspaceGuidelines         string                              `json:"workspace_guidelines"`
	WorkspaceGuidelinesCamel    string                              `json:"workspaceGuidelines"`
	CodeBlock                   string                              `json:"code_block"`
	TargetFilePath              string                              `json:"target_file_path"`
	TargetFileContent           string                              `json:"target_file_content"`
	ContextCodeExchangeID       string                              `json:"context_code_exchange_request_id"`
	Nodes                       []augmentLegacyChatNode             `json:"nodes"`
	RequestNodesCamel           []augmentLegacyChatNode             `json:"requestNodes"`
	StructuredRequestNodesCamel []augmentLegacyChatNode             `json:"structuredRequestNodes"`
}

type augmentLegacyPromptEnhancerRequest struct {
	Model                       string                              `json:"model"`
	Message                     string                              `json:"message"`
	Prompt                      string                              `json:"prompt"`
	Nodes                       []augmentLegacyChatNode             `json:"nodes"`
	RequestNodesCamel           []augmentLegacyChatNode             `json:"requestNodes"`
	StructuredRequestNodesCamel []augmentLegacyChatNode             `json:"structuredRequestNodes"`
	ChatHistory                 []augmentLegacyChatHistoryItem      `json:"chat_history"`
	ChatHistoryCamel            []augmentLegacyChatHistoryItem      `json:"chatHistory"`
	Blobs                       augmentLegacyCheckpointBlobsPayload `json:"blobs"`
	ConversationID              string                              `json:"conversation_id"`
	ConversationIDCamel         string                              `json:"conversationId"`
	UserGuidedBlobs             []string                            `json:"user_guided_blobs"`
	UserGuidedBlobsCamel        []string                            `json:"userGuidedBlobs"`
	ExternalSourceIDs           []string                            `json:"external_source_ids"`
	ExternalSourceIDsCamel      []string                            `json:"externalSourceIds"`
	UserGuidelines              string                              `json:"user_guidelines"`
	UserGuidelinesCamel         string                              `json:"userGuidelines"`
	WorkspaceGuidelines         string                              `json:"workspace_guidelines"`
	WorkspaceGuidelinesCamel    string                              `json:"workspaceGuidelines"`
	Rules                       []any                               `json:"rules"`
	Path                        string                              `json:"path"`
	Lang                        string                              `json:"lang"`
	Language                    string                              `json:"language"`
	SelectedText                string                              `json:"selected_text"`
	SelectedTextCamel           string                              `json:"selectedText"`
	SelectedCode                string                              `json:"selected_code"`
	SelectedCodeCamel           string                              `json:"selectedCode"`
}

type augmentLegacyCommitMessageRequest struct {
	ChangedFileStats struct {
	} `json:"changed_file_stats"`
	Diff                             string `json:"diff"`
	GeneratedCommitMessageSubrequest struct {
		RelevantCommitMessages []string `json:"relevant_commit_messages"`
		ExampleCommitMessages  []string `json:"example_commit_messages"`
	} `json:"generatedCommitMessageSubrequest"`
}

type augmentLegacyNextEditLocationRequest struct {
	Instruction   string                              `json:"instruction"`
	Path          string                              `json:"path"`
	Blobs         augmentLegacyCheckpointBlobsPayload `json:"blobs"`
	RecentChanges []map[string]any                    `json:"recent_changes"`
	Diagnostics   []map[string]any                    `json:"diagnostics"`
	NumResults    int                                 `json:"num_results"`
	IsSingleFile  bool                                `json:"is_single_file"`
	Nodes         []augmentLegacyChatNode             `json:"nodes"`
}

type augmentLegacyNextEditStreamRequest struct {
	Model           string                              `json:"model"`
	Instruction     string                              `json:"instruction"`
	Prefix          string                              `json:"prefix"`
	SelectedText    string                              `json:"selected_text"`
	Suffix          string                              `json:"suffix"`
	BlobName        string                              `json:"blob_name"`
	Lang            string                              `json:"lang"`
	Path            string                              `json:"path"`
	Blobs           augmentLegacyCheckpointBlobsPayload `json:"blobs"`
	RecentChanges   []map[string]any                    `json:"recent_changes"`
	Diagnostics     []map[string]any                    `json:"diagnostics"`
	ClientCreatedAt string                              `json:"client_created_at"`
	Nodes           []augmentLegacyChatNode             `json:"nodes"`
}

type augmentLegacySaveChatItem struct {
	RequestMessage string `json:"request_message"`
	ResponseText   string `json:"response_text"`
	RequestID      string `json:"request_id"`
}

type augmentLegacySaveChatRequest struct {
	ConversationID string                      `json:"conversation_id"`
	Title          string                      `json:"title"`
	Chat           []augmentLegacySaveChatItem `json:"chat"`
}

type augmentLegacyCodebaseRetrievalRequest struct {
	InformationRequest          string                              `json:"information_request"`
	Blobs                       augmentLegacyCheckpointBlobsPayload `json:"blobs"`
	Dialog                      []map[string]any                    `json:"dialog"`
	MaxOutputLength             int                                 `json:"max_output_length"`
	DisableCodebaseRetrieval    bool                                `json:"disable_codebase_retrieval"`
	EnableCommitRetrieval       bool                                `json:"enable_commit_retrieval"`
	EnableConversationRetrieval bool                                `json:"enable_conversation_retrieval"`
}

type augmentLegacyNotification struct {
	NotificationID string `json:"notification_id,omitempty"`
	Level          int    `json:"level,omitempty"`
	Message        string `json:"message,omitempty"`
	DisplayType    int    `json:"display_type,omitempty"`
}

type augmentLegacyToolSummary struct {
	ToolDefinition     map[string]any `json:"tool_definition,omitempty"`
	RemoteToolID       string         `json:"remote_tool_id,omitempty"`
	AvailabilityStatus string         `json:"availability_status,omitempty"`
	ToolSafety         int            `json:"tool_safety,omitempty"`
	OAuthURL           string         `json:"oauth_url,omitempty"`
}

type augmentLegacyPendingToolCall struct {
	ID        string
	Name      string
	Arguments string
	Emitted   bool
}

type augmentLegacyStreamToolCallBuffer struct {
	order              []int
	byIndex            map[int]*augmentLegacyPendingToolCall
	indexByID          map[string]int
	nextSyntheticIndex int
}

func newAugmentLegacyStreamToolCallBuffer() *augmentLegacyStreamToolCallBuffer {
	return &augmentLegacyStreamToolCallBuffer{
		byIndex:   make(map[int]*augmentLegacyPendingToolCall),
		indexByID: make(map[string]int),
	}
}

func (b *augmentLegacyStreamToolCallBuffer) ensureIndex(toolCall apicompat.ChatToolCall) int {
	if toolCall.Index != nil {
		return *toolCall.Index
	}
	if id := strings.TrimSpace(toolCall.ID); id != "" {
		if idx, ok := b.indexByID[id]; ok {
			return idx
		}
	}
	idx := b.nextSyntheticIndex
	b.nextSyntheticIndex++
	return idx
}

func (b *augmentLegacyStreamToolCallBuffer) absorb(toolCalls []apicompat.ChatToolCall) {
	for _, toolCall := range toolCalls {
		idx := b.ensureIndex(toolCall)
		state, ok := b.byIndex[idx]
		if !ok {
			state = &augmentLegacyPendingToolCall{}
			b.byIndex[idx] = state
			b.order = append(b.order, idx)
		}
		if id := strings.TrimSpace(toolCall.ID); id != "" {
			state.ID = id
			b.indexByID[id] = idx
		}
		if name := strings.TrimSpace(toolCall.Function.Name); name != "" {
			state.Name = name
		}
		if args := toolCall.Function.Arguments; args != "" {
			state.Arguments += args
		}
	}
}

func (b *augmentLegacyStreamToolCallBuffer) flushReady(nextID *int, force bool) ([]gin.H, int) {
	nodes := make([]gin.H, 0, len(b.order))
	invalidCount := 0
	for _, idx := range b.order {
		state := b.byIndex[idx]
		if state == nil || state.Emitted {
			continue
		}
		name := strings.TrimSpace(state.Name)
		if name == "" && !force {
			break
		}
		if name == "" {
			name = "unknown_tool"
		}
		inputJSON := strings.TrimSpace(state.Arguments)
		if inputJSON == "" && !force {
			break
		}
		if inputJSON == "" {
			inputJSON = "{}"
		}
		if !json.Valid([]byte(inputJSON)) {
			if force {
				invalidCount++
			}
			break
		}
		toolUseID := strings.TrimSpace(state.ID)
		if toolUseID == "" {
			toolUseID = fmt.Sprintf("toolcall_%d", idx)
		}
		*nextID++
		nodes = append(nodes, augmentLegacyToolUseResponseNode(*nextID, toolUseID, name, inputJSON))
		state.Emitted = true
	}
	return nodes, invalidCount
}

func (b *augmentLegacyStreamToolCallBuffer) materialize() []service.AugmentGatewayToolCall {
	if b == nil {
		return nil
	}
	calls := make([]service.AugmentGatewayToolCall, 0, len(b.order))
	for _, idx := range b.order {
		state := b.byIndex[idx]
		if state == nil {
			continue
		}
		name := strings.TrimSpace(state.Name)
		if name == "" {
			continue
		}
		arguments := strings.TrimSpace(state.Arguments)
		if arguments == "" {
			arguments = "{}"
		}
		calls = append(calls, service.AugmentGatewayToolCall{
			ID:   strings.TrimSpace(state.ID),
			Type: "function",
			Function: service.AugmentGatewayToolCallFunction{
				Name:      name,
				Arguments: arguments,
			},
		})
	}
	return calls
}

type augmentLegacyLoopbackChatResult struct {
	Response *apicompat.ChatCompletionsResponse
	Text     string
}

type augmentLegacyChatObservability struct {
	InboundToolDefinitionCount int
	InboundToolDefinitionNames []string
	HasToolResultFollowup      bool
	LocalRetrievalInjected     bool
	ResolvedUserInputSource    string
	ResolvedUserInputBytes     int
}

func augmentLegacyRawResponseNode(id int, content string) gin.H {
	return gin.H{"id": id, "type": augmentResponseNodeRawResponse, "content": content}
}

func augmentLegacyThinkingNode(id int, summary string) gin.H {
	return gin.H{"id": id, "type": augmentResponseNodeThinking, "content": "", "thinking": gin.H{"summary": summary}}
}

func augmentLegacyToolUseResponseNode(id int, toolUseID, toolName, inputJSON string) gin.H {
	return gin.H{
		"id":      id,
		"type":    augmentResponseNodeToolUse,
		"content": "",
		"tool_use": gin.H{
			"tool_use_id": toolUseID,
			"tool_name":   toolName,
			"input_json":  inputJSON,
		},
	}
}

func augmentLegacyTokenUsageNode(id int, usage *apicompat.ChatUsage) gin.H {
	node := gin.H{
		"id":      id,
		"type":    augmentResponseNodeTokenUsage,
		"content": "",
		"token_usage": gin.H{
			"input_tokens":  usage.PromptTokens,
			"output_tokens": usage.CompletionTokens,
		},
	}
	if usage != nil && usage.PromptTokensDetails != nil && usage.PromptTokensDetails.CachedTokens > 0 {
		node["token_usage"].(gin.H)["cache_read_input_tokens"] = usage.PromptTokensDetails.CachedTokens
	}
	return node
}

func augmentLegacyChatChunk(text string, nodes []gin.H, stopReason *int, unknownBlobNames []string, checkpointNotFound bool) gin.H {
	chunk := gin.H{
		"text":                  text,
		"unknown_blob_names":    unknownBlobNames,
		"checkpoint_not_found":  checkpointNotFound,
		"workspace_file_chunks": []any{},
	}
	if len(nodes) > 0 {
		chunk["nodes"] = nodes
	}
	if stopReason != nil {
		chunk["stop_reason"] = *stopReason
	}
	return chunk
}

func augmentLegacyMapOpenAIFinishReason(reason string) int {
	switch strings.TrimSpace(strings.ToLower(reason)) {
	case "length":
		return augmentStopReasonMaxTokens
	case "tool_calls", "function_call":
		return augmentStopReasonToolUseRequired
	case "content_filter":
		return augmentStopReasonSafety
	case "stop":
		fallthrough
	default:
		return augmentStopReasonEndTurn
	}
}

func (h *AuthHandler) AugmentLegacyJSONAck(c *gin.Context) {
	if _, ok := h.augmentPrincipalFromBearer(c); !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{})
}

func (h *AuthHandler) AugmentLegacyNotificationsRead(c *gin.Context) {
	if _, ok := h.augmentPrincipalFromBearer(c); !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"notifications": []augmentLegacyNotification{}})
}

func (h *AuthHandler) AugmentLegacyNotificationsMarkRead(c *gin.Context) {
	if _, ok := h.augmentPrincipalFromBearer(c); !ok {
		return
	}
	h.AugmentLegacyJSONAck(c)
}

func (h *AuthHandler) AugmentLegacySubscriptionBanner(c *gin.Context) {
	if _, ok := h.augmentPrincipalFromBearer(c); !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"banner": nil})
}

func (h *AuthHandler) AugmentLegacyListRemoteTools(c *gin.Context) {
	if _, ok := h.augmentPrincipalFromBearer(c); !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"tools": []augmentLegacyToolSummary{}})
}

func (h *AuthHandler) AugmentLegacyListRemoteAgents(c *gin.Context) {
	if _, ok := h.augmentPrincipalFromBearer(c); !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"remote_agents":            []any{},
		"max_remote_agents":        0,
		"max_active_remote_agents": 0,
	})
}

func (h *AuthHandler) AugmentLegacyBatchUpload(c *gin.Context) {
	if h.augmentPluginService == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "augment plugin service is unavailable"})
		return
	}
	principal, ok := h.augmentPrincipalFromBearer(c)
	if !ok {
		return
	}

	var req augmentLegacyBatchUploadRequest
	if !augmentLegacyDecodeRequest(c, &req) {
		return
	}

	namespace := h.augmentLegacyNamespace(c, principal)
	uploaded := make([]service.AugmentLegacyUploadedBlob, 0, len(req.Blobs))
	for _, blob := range req.Blobs {
		uploaded = append(uploaded, service.AugmentLegacyUploadedBlob{
			BlobName: blob.BlobName,
			Path:     blob.Path,
			Content:  blob.Content,
		})
	}
	blobNames := h.augmentPluginService.StoreLegacyBlobsForNamespace(namespace, uploaded)
	augmentLegacyTrace(c, "batch_upload", "namespace", namespace, "blob_count", len(req.Blobs))
	c.JSON(http.StatusOK, gin.H{"blob_names": blobNames})
}

func (h *AuthHandler) AugmentLegacyFindMissing(c *gin.Context) {
	if h.augmentPluginService == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "augment plugin service is unavailable"})
		return
	}
	principal, ok := h.augmentPrincipalFromBearer(c)
	if !ok {
		return
	}

	var req augmentLegacyFindMissingRequest
	if !augmentLegacyDecodeRequest(c, &req) {
		return
	}

	namespace := h.augmentLegacyNamespace(c, principal)
	unknown, nonindexed := h.augmentPluginService.FindLegacyMissingForNamespace(namespace, req.MemObjectNames)
	augmentLegacyTrace(c, "find_missing", "namespace", namespace, "requested_blobs", len(req.MemObjectNames), "unknown_blobs", len(unknown))
	c.JSON(http.StatusOK, gin.H{
		"unknown_memory_names":  unknown,
		"nonindexed_blob_names": nonindexed,
	})
}

func (h *AuthHandler) AugmentLegacySaveChat(c *gin.Context) {
	if h.augmentPluginService == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "augment plugin service is unavailable"})
		return
	}
	principal, ok := h.augmentPrincipalFromBearer(c)
	if !ok {
		return
	}

	var req augmentLegacySaveChatRequest
	if !augmentLegacyDecodeRequest(c, &req) {
		return
	}

	namespace := h.augmentLegacyNamespace(c, principal)
	items := make([]service.AugmentLegacySavedChatItem, 0, len(req.Chat))
	for _, item := range req.Chat {
		items = append(items, service.AugmentLegacySavedChatItem{
			RequestMessage: item.RequestMessage,
			ResponseText:   item.ResponseText,
			RequestID:      item.RequestID,
		})
	}
	h.augmentPluginService.SaveLegacyChatForNamespace(namespace, service.AugmentLegacySavedChat{
		ConversationID: req.ConversationID,
		Title:          req.Title,
		Chat:           items,
	})
	augmentLegacyTrace(c, "save_chat", "namespace", namespace, "conversation_id", strings.TrimSpace(req.ConversationID), "message_count", len(req.Chat))
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *AuthHandler) AugmentLegacyGetImplicitExternalSources(c *gin.Context) {
	if _, ok := h.augmentPrincipalFromBearer(c); !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"external_source_ids": []string{}})
}

func (h *AuthHandler) AugmentLegacySearchExternalSources(c *gin.Context) {
	if _, ok := h.augmentPrincipalFromBearer(c); !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"results": []any{}})
}

func (h *AuthHandler) AugmentLegacyContextCanvasList(c *gin.Context) {
	if _, ok := h.augmentPrincipalFromBearer(c); !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"canvases": []any{}, "next_page_token": ""})
}

func (h *AuthHandler) AugmentLegacyCodebaseRetrieval(c *gin.Context) {
	if h.augmentPluginService == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "augment plugin service is unavailable"})
		return
	}
	principal, ok := h.augmentPrincipalFromBearer(c)
	if !ok {
		return
	}

	var req augmentLegacyCodebaseRetrievalRequest
	if !augmentLegacyDecodeRequest(c, &req) {
		return
	}

	namespace := h.augmentLegacyNamespace(c, principal)
	records := h.augmentPluginService.ResolveLegacyBlobsForNamespace(namespace, req.Blobs.CheckpointID, req.Blobs.AddedBlobs, req.Blobs.DeletedBlobs)
	contextBundle := augmentContextBundleFromCodebaseRetrievalRequest(req).withResolvedBlobs(records)
	traceFields := []any{"namespace", namespace, "checkpoint_id", strings.TrimSpace(req.Blobs.CheckpointID), "record_count", len(records.Records), "unknown_count", len(records.Unknown), "active_blob_count", len(records.Records) + len(records.Unknown), "added_blob_count", len(req.Blobs.AddedBlobs), "deleted_blob_count", len(req.Blobs.DeletedBlobs), "checkpoint_not_found", records.CheckpointNotFound, "resolution_reason", records.ResolutionReason}
	traceFields = append(traceFields, contextBundle.TraceFields()...)
	augmentLegacyTrace(c, "codebase_retrieval", traceFields...)
	text := h.augmentPluginService.BuildLegacyFormattedRetrieval(req.InformationRequest, records, req.MaxOutputLength)
	text = augmentLegacyAppendContextBundleRetrievalMetadata(text, contextBundle, req.MaxOutputLength)
	c.JSON(http.StatusOK, gin.H{"formatted_retrieval": text})
}

func (h *AuthHandler) augmentLegacyLoopbackBaseURL(c *gin.Context) string {
	scheme := "http"
	if c.Request != nil {
		if c.Request.TLS != nil {
			scheme = "https"
		}
		if forwarded := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); forwarded != "" {
			scheme = forwarded
		}
	}
	host := ""
	if c.Request != nil {
		host = strings.TrimSpace(c.Request.Host)
	}
	if host == "" && h.cfg != nil && strings.TrimSpace(h.cfg.Server.FrontendURL) != "" {
		return strings.TrimSpace(h.cfg.Server.FrontendURL)
	}
	if host == "" {
		host = "127.0.0.1:18081"
	}
	return fmt.Sprintf("%s://%s", scheme, host)
}

func (h *AuthHandler) augmentLegacyGatewayBearer(ctx context.Context, principal *service.AugmentPluginPrincipal) (string, error) {
	if principal == nil {
		return "", service.ErrInvalidToken
	}
	if principal.Kind == "api_key" && principal.APIKey != nil && strings.TrimSpace(principal.APIKey.Key) != "" {
		return strings.TrimSpace(principal.APIKey.Key), nil
	}
	summary, err := h.augmentPluginService.BuildSummary(ctx, *principal)
	if err != nil {
		return "", err
	}
	if summary == nil || strings.TrimSpace(summary.GatewayAPIKey) == "" {
		return "", service.ErrAPIKeyNotFound
	}
	return strings.TrimSpace(summary.GatewayAPIKey), nil
}

func (h *AuthHandler) augmentLegacyToolDefinitionsToOpenAI(defs []augmentLegacyToolDefinition) []apicompat.ChatTool {
	out := make([]apicompat.ChatTool, 0, len(defs))
	for _, def := range defs {
		name := strings.TrimSpace(def.Name)
		if name == "" {
			continue
		}
		switch name {
		case "view_tasklist", "add_tasks", "update_tasks", "reorganize_tasklist":
			// These tools are conversation-planning helpers. In the local compat path
			// they can induce unproductive loops in repo-analysis chats, especially
			// when no real task list exists yet.
			continue
		}
		var params json.RawMessage
		switch {
		case len(def.InputSchema) > 0:
			if b, err := json.Marshal(def.InputSchema); err == nil {
				params = b
			}
		case strings.TrimSpace(def.InputSchemaJSON) != "":
			params = json.RawMessage(strings.TrimSpace(def.InputSchemaJSON))
		default:
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, apicompat.ChatTool{
			Type: "function",
			Function: &apicompat.ChatFunction{
				Name:        name,
				Description: augmentLegacyGatewayToolDescription(def),
				Parameters:  params,
			},
		})
	}
	return out
}

func augmentLegacyGatewayToolDescription(def augmentLegacyToolDefinition) string {
	description := strings.TrimSpace(def.Description)
	if strings.TrimSpace(def.Name) != "codebase-retrieval" {
		return description
	}
	policy := "When using codebase-retrieval, write one complete, repository-specific information_request rather than short keywords. Preserve exact file paths, symbols, endpoint paths, JSON fields, and user constraints. Expand the request into concrete evidence targets such as routes, handlers, controllers, services, middleware, adapters, request/response schemas, tests, and likely file paths. Only issue a second retrieval when the first result leaves a clearly missing evidence layer."
	if description == "" {
		return policy
	}
	if strings.Contains(description, "repository-specific information_request") {
		return description
	}
	return description + "\n\nAugment Gateway guidance: " + policy
}

func augmentLegacyJSONString(v string) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func augmentLegacyMakeMessage(role, text string) apicompat.ChatMessage {
	return apicompat.ChatMessage{
		Role:    role,
		Content: augmentLegacyJSONString(text),
	}
}

func augmentLegacyJoinRuleTexts(rules []any) string {
	if len(rules) == 0 {
		return ""
	}
	parts := make([]string, 0, len(rules))
	for _, rule := range rules {
		switch x := rule.(type) {
		case string:
			if strings.TrimSpace(x) != "" {
				parts = append(parts, strings.TrimSpace(x))
			}
		case map[string]any:
			if msg, ok := x["text"].(string); ok && strings.TrimSpace(msg) != "" {
				parts = append(parts, strings.TrimSpace(msg))
			}
		}
	}
	return strings.Join(parts, "\n")
}

func augmentLegacyCompactTextParts(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return strings.Join(out, "\n\n")
}

func augmentLegacyFirstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func augmentLegacyMergeNodes(primary []augmentLegacyChatNode, extra ...[]augmentLegacyChatNode) []augmentLegacyChatNode {
	if len(extra) == 0 {
		return primary
	}
	total := len(primary)
	for _, part := range extra {
		total += len(part)
	}
	if total == 0 {
		return nil
	}
	out := make([]augmentLegacyChatNode, 0, total)
	out = append(out, primary...)
	for _, part := range extra {
		out = append(out, part...)
	}
	return out
}

func augmentLegacyNormalizeHistoryItem(item *augmentLegacyChatHistoryItem) {
	if item == nil {
		return
	}
	item.RequestMessage = augmentLegacyFirstNonBlank(item.RequestMessage, item.RequestMessageCamel, item.Message)
	item.ResponseText = augmentLegacyFirstNonBlank(item.ResponseText, item.ResponseTextCamel, item.Response, item.Text)
	item.RequestID = augmentLegacyFirstNonBlank(item.RequestID, item.RequestIDCamel)
	item.RequestNodes = augmentLegacyMergeNodes(item.RequestNodes, item.RequestNodesCamel)
	item.ResponseNodes = augmentLegacyMergeNodes(item.ResponseNodes, item.ResponseNodesCamel)
}

func augmentLegacyNormalizeHistoryItems(items []augmentLegacyChatHistoryItem) []augmentLegacyChatHistoryItem {
	for i := range items {
		augmentLegacyNormalizeHistoryItem(&items[i])
	}
	return items
}

func augmentLegacyHistoryRequestText(item augmentLegacyChatHistoryItem) string {
	if text := strings.TrimSpace(item.RequestMessage); text != "" {
		return text
	}
	return strings.TrimSpace(augmentLegacyExtractRequestNodeText(item.RequestNodes))
}

func augmentLegacyNormalizeNodeType(node augmentLegacyChatNode) int {
	if node.Type != nil {
		return *node.Type
	}
	if node.NodeType != nil {
		return *node.NodeType
	}
	if node.NodeTypeCamel != nil {
		return *node.NodeTypeCamel
	}
	return -1
}

func augmentLegacyNormalizeNodeText(node augmentLegacyChatNode) string {
	if node.TextNode != nil && strings.TrimSpace(node.TextNode.Content) != "" {
		return node.TextNode.Content
	}
	if node.TextNodeCamel != nil && strings.TrimSpace(node.TextNodeCamel.Content) != "" {
		return node.TextNodeCamel.Content
	}
	if strings.TrimSpace(node.Content) != "" {
		return node.Content
	}
	return ""
}

func augmentLegacyNormalizeRequestNodeText(node augmentLegacyChatNode) string {
	if node.TextNode != nil && strings.TrimSpace(node.TextNode.Content) != "" {
		return node.TextNode.Content
	}
	if node.TextNodeCamel != nil && strings.TrimSpace(node.TextNodeCamel.Content) != "" {
		return node.TextNodeCamel.Content
	}
	return ""
}

func augmentLegacyNormalizeToolUseNode(node augmentLegacyChatNode) *augmentLegacyToolUseNode {
	toolUse := node.ToolUse
	if toolUse == nil {
		toolUse = node.ToolUseCamel
	}
	return toolUse
}

func augmentLegacyNormalizeToolResultNode(node augmentLegacyChatNode) *augmentLegacyToolResultNode {
	toolResult := node.ToolResultNode
	if toolResult == nil {
		toolResult = node.ToolResultNodeCamel
	}
	return toolResult
}

func augmentLegacyNormalizeToolResultContentType(node augmentLegacyToolResultContentNode) int {
	if node.Type != nil {
		return *node.Type
	}
	if node.NodeType != nil {
		return *node.NodeType
	}
	if node.NodeTypeCamel != nil {
		return *node.NodeTypeCamel
	}
	return -1
}

func augmentLegacyToolResultContent(tr *augmentLegacyToolResultNode) string {
	if tr == nil {
		return ""
	}
	content := strings.TrimSpace(tr.Content)
	if content != "" {
		return content
	}
	contentNodes := tr.ContentNodes
	if len(contentNodes) == 0 {
		contentNodes = tr.ContentNodesCamel
	}
	textParts := make([]string, 0, len(contentNodes))
	for _, item := range contentNodes {
		if augmentLegacyNormalizeToolResultContentType(item) != augmentToolResultContentText {
			continue
		}
		text := strings.TrimSpace(augmentLegacyFirstNonBlank(item.TextContent, item.TextContentCamel))
		if text == "" {
			continue
		}
		textParts = append(textParts, text)
	}
	return strings.TrimSpace(strings.Join(textParts, "\n\n"))
}

func augmentLegacyExtractToolResultText(nodes []augmentLegacyChatNode) string {
	parts := make([]string, 0, len(nodes))
	seen := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		if augmentLegacyNormalizeNodeType(node) != augmentRequestNodeToolResult {
			continue
		}
		content := augmentLegacyToolResultContent(augmentLegacyNormalizeToolResultNode(node))
		if content == "" {
			continue
		}
		if _, ok := seen[content]; ok {
			continue
		}
		seen[content] = struct{}{}
		parts = append(parts, content)
	}
	return strings.Join(parts, "\n\n")
}

func augmentLegacyNormalizeChatRequest(req *augmentLegacyChatRequest) {
	if req == nil {
		return
	}
	req.Message = augmentLegacyFirstNonBlank(req.Message, req.Prompt, req.Instruction)
	req.ChatHistory = augmentLegacyNormalizeHistoryItems(append(req.ChatHistory, req.ChatHistoryCamel...))
	req.UserGuidedBlobs = append(req.UserGuidedBlobs, req.UserGuidedBlobsCamel...)
	req.ExternalSourceIDs = append(req.ExternalSourceIDs, req.ExternalSourceIDsCamel...)
	req.UserGuidelines = augmentLegacyFirstNonBlank(req.UserGuidelines, req.UserGuidelinesCamel)
	req.WorkspaceGuidelines = augmentLegacyFirstNonBlank(req.WorkspaceGuidelines, req.WorkspaceGuidelinesCamel)
	req.ToolDefinitions = append(req.ToolDefinitions, req.ToolDefinitionsCamel...)
	req.ConversationID = augmentLegacyFirstNonBlank(req.ConversationID, req.ConversationIDCamel)
	req.Lang = augmentLegacyFirstNonBlank(req.Lang, req.Language)
	req.SelectedCode = augmentLegacyFirstNonBlank(req.SelectedCode, req.SelectedCodeCamel, req.SelectedText, req.SelectedTextCamel)
	req.RequestNodes = augmentLegacyMergeNodes(req.RequestNodes, req.RequestNodesCamel)
	req.StructuredRequestNodes = augmentLegacyMergeNodes(req.StructuredRequestNodes, req.StructuredRequestNodesCamel)
	req.SystemPrompt = augmentLegacyFirstNonBlank(req.SystemPrompt, req.SystemPromptCamel)
	req.SystemPromptAppend = augmentLegacyFirstNonBlank(req.SystemPromptAppend, req.SystemPromptAppendCamel)
	req.SystemPromptReplacements = append(req.SystemPromptReplacements, req.SystemPromptReplacementsCamel...)
	if req.DisableAutoExternalSourcesCamel != nil {
		req.DisableAutoExternalSources = *req.DisableAutoExternalSourcesCamel
	}
	if req.DisableRetrievalCamel != nil {
		req.DisableRetrieval = *req.DisableRetrievalCamel
	}
}

func augmentLegacyNormalizeInstructionRequest(req *augmentLegacyInstructionRequest) {
	if req == nil {
		return
	}
	req.Instruction = augmentLegacyFirstNonBlank(req.Instruction, req.Prompt)
	req.SelectedText = augmentLegacyFirstNonBlank(req.SelectedText, req.SelectedTextCamel)
	req.Lang = augmentLegacyFirstNonBlank(req.Lang, req.Language)
	req.ChatHistory = augmentLegacyNormalizeHistoryItems(append(req.ChatHistory, req.ChatHistoryCamel...))
	req.UserGuidelines = augmentLegacyFirstNonBlank(req.UserGuidelines, req.UserGuidelinesCamel)
	req.WorkspaceGuidelines = augmentLegacyFirstNonBlank(req.WorkspaceGuidelines, req.WorkspaceGuidelinesCamel)
	req.Nodes = augmentLegacyMergeNodes(req.Nodes, req.RequestNodesCamel, req.StructuredRequestNodesCamel)
}

func augmentLegacyNormalizePromptEnhancerRequest(req *augmentLegacyPromptEnhancerRequest) {
	if req == nil {
		return
	}
	req.Message = augmentLegacyFirstNonBlank(req.Message, req.Prompt)
	req.Nodes = augmentLegacyMergeNodes(req.Nodes, req.RequestNodesCamel, req.StructuredRequestNodesCamel)
	req.ChatHistory = augmentLegacyNormalizeHistoryItems(append(req.ChatHistory, req.ChatHistoryCamel...))
	req.ConversationID = augmentLegacyFirstNonBlank(req.ConversationID, req.ConversationIDCamel)
	req.UserGuidedBlobs = append(req.UserGuidedBlobs, req.UserGuidedBlobsCamel...)
	req.ExternalSourceIDs = append(req.ExternalSourceIDs, req.ExternalSourceIDsCamel...)
	req.UserGuidelines = augmentLegacyFirstNonBlank(req.UserGuidelines, req.UserGuidelinesCamel)
	req.WorkspaceGuidelines = augmentLegacyFirstNonBlank(req.WorkspaceGuidelines, req.WorkspaceGuidelinesCamel)
	req.Lang = augmentLegacyFirstNonBlank(req.Lang, req.Language)
	req.SelectedText = augmentLegacyFirstNonBlank(req.SelectedText, req.SelectedTextCamel, req.SelectedCode, req.SelectedCodeCamel)
}

func augmentLegacySystemPromptReplacementText(replacements []map[string]any) string {
	if len(replacements) == 0 {
		return ""
	}
	parts := make([]string, 0, len(replacements))
	for _, replacement := range replacements {
		if replacement == nil {
			continue
		}
		b, err := json.Marshal(replacement)
		if err != nil {
			continue
		}
		parts = append(parts, string(b))
	}
	if len(parts) == 0 {
		return ""
	}
	return "system_prompt_replacements:\n" + strings.Join(parts, "\n")
}

func augmentLegacyExtractNodeText(nodes []augmentLegacyChatNode) string {
	return augmentLegacyExtractNodeTextWithOptions(nodes, true)
}

func augmentLegacyExtractRequestNodeText(nodes []augmentLegacyChatNode) string {
	return augmentLegacyExtractNodeTextWithOptions(nodes, false)
}

func augmentLegacyExtractNodeTextWithOptions(nodes []augmentLegacyChatNode, allowBareContent bool) string {
	parts := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if augmentLegacyNormalizeNodeType(node) != 0 {
			continue
		}
		var text string
		if allowBareContent {
			text = strings.TrimSpace(augmentLegacyNormalizeNodeText(node))
		} else {
			text = strings.TrimSpace(augmentLegacyNormalizeRequestNodeText(node))
		}
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n\n")
}

func augmentLegacyHasToolResultNodes(nodeSets ...[]augmentLegacyChatNode) bool {
	for _, nodes := range nodeSets {
		for _, node := range nodes {
			if augmentLegacyNormalizeNodeType(node) == augmentRequestNodeToolResult && augmentLegacyNormalizeToolResultNode(node) != nil {
				return true
			}
		}
	}
	return false
}

func augmentLegacyExtractToolResultMessages(nodes []augmentLegacyChatNode) []apicompat.ChatMessage {
	out := make([]apicompat.ChatMessage, 0, len(nodes))
	seen := make(map[string]struct{}, len(nodes))
	for idx, node := range nodes {
		if augmentLegacyNormalizeNodeType(node) != augmentRequestNodeToolResult {
			continue
		}
		toolResult := augmentLegacyNormalizeToolResultNode(node)
		if toolResult == nil {
			continue
		}
		toolCallID := strings.TrimSpace(augmentLegacyFirstNonBlank(toolResult.ToolUseID, toolResult.ToolUseIDCamel))
		if toolCallID == "" {
			toolCallID = fmt.Sprintf("tool_result_%d", idx)
		}
		content := augmentLegacyToolResultContent(toolResult)
		if content == "" {
			continue
		}
		key := toolCallID + "\x00" + content
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, apicompat.ChatMessage{
			Role:       "tool",
			ToolCallID: toolCallID,
			Content:    augmentLegacyJSONString(content),
		})
	}
	return out
}

func augmentLegacyExtractToolCallMessages(nodes []augmentLegacyChatNode) []apicompat.ChatMessage {
	out := make([]apicompat.ChatMessage, 0, len(nodes))
	seen := make(map[string]struct{}, len(nodes))
	for idx, node := range nodes {
		toolUse := augmentLegacyNormalizeToolUseNode(node)
		if toolUse == nil {
			continue
		}
		toolName := strings.TrimSpace(augmentLegacyFirstNonBlank(toolUse.ToolName, toolUse.ToolNameCamel))
		if toolName == "" {
			continue
		}
		toolCallID := strings.TrimSpace(augmentLegacyFirstNonBlank(toolUse.ToolUseID, toolUse.ToolUseIDCamel))
		if toolCallID == "" {
			toolCallID = fmt.Sprintf("tool_call_%d", idx)
		}
		inputJSON := strings.TrimSpace(augmentLegacyFirstNonBlank(toolUse.InputJSON, toolUse.InputJSONCamel))
		if inputJSON == "" {
			inputJSON = "{}"
		}
		if !json.Valid([]byte(inputJSON)) {
			continue
		}
		key := toolCallID + "\x00" + toolName + "\x00" + inputJSON
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, apicompat.ChatMessage{
			Role: "assistant",
			ToolCalls: []apicompat.ChatToolCall{{
				ID:   toolCallID,
				Type: "function",
				Function: apicompat.ChatFunctionCall{
					Name:      toolName,
					Arguments: inputJSON,
				},
			}},
		})
	}
	return out
}

func augmentLegacyFilterToolCallMessagesWithResults(toolCalls, toolResults []apicompat.ChatMessage) []apicompat.ChatMessage {
	if len(toolCalls) == 0 || len(toolResults) == 0 {
		return nil
	}
	resultIDs := make(map[string]struct{}, len(toolResults))
	for _, msg := range toolResults {
		if id := strings.TrimSpace(msg.ToolCallID); id != "" {
			resultIDs[id] = struct{}{}
		}
	}
	if len(resultIDs) == 0 {
		return nil
	}

	out := make([]apicompat.ChatMessage, 0, len(toolCalls))
	for _, msg := range toolCalls {
		if len(msg.ToolCalls) == 0 {
			continue
		}
		filtered := make([]apicompat.ChatToolCall, 0, len(msg.ToolCalls))
		for _, toolCall := range msg.ToolCalls {
			if _, ok := resultIDs[strings.TrimSpace(toolCall.ID)]; ok {
				filtered = append(filtered, toolCall)
			}
		}
		if len(filtered) == 0 {
			continue
		}
		msg.ToolCalls = filtered
		out = append(out, msg)
	}
	return out
}

func augmentLegacyToolCallIDsFromMessages(toolCalls []apicompat.ChatMessage) map[string]struct{} {
	ids := make(map[string]struct{})
	for _, msg := range toolCalls {
		for _, toolCall := range msg.ToolCalls {
			if id := strings.TrimSpace(toolCall.ID); id != "" {
				ids[id] = struct{}{}
			}
		}
	}
	return ids
}

func augmentLegacySyntheticToolCallMessagesForResults(toolResults []apicompat.ChatMessage, existingIDs map[string]struct{}) []apicompat.ChatMessage {
	if len(toolResults) == 0 {
		return nil
	}
	out := make([]apicompat.ChatMessage, 0, len(toolResults))
	for _, result := range toolResults {
		toolCallID := strings.TrimSpace(result.ToolCallID)
		if toolCallID == "" {
			continue
		}
		if _, ok := existingIDs[toolCallID]; ok {
			continue
		}
		existingIDs[toolCallID] = struct{}{}
		out = append(out, apicompat.ChatMessage{
			Role: "assistant",
			ToolCalls: []apicompat.ChatToolCall{{
				ID:   toolCallID,
				Type: "function",
				Function: apicompat.ChatFunctionCall{
					Name:      augmentLegacyInferToolNameForToolResult(result),
					Arguments: "{}",
				},
			}},
		})
	}
	return out
}

func augmentLegacyAppendToolExchangeMessages(messages []apicompat.ChatMessage, toolCalls, toolResults []apicompat.ChatMessage) []apicompat.ChatMessage {
	if len(toolResults) == 0 {
		return messages
	}

	type indexedToolResult struct {
		index  int
		result apicompat.ChatMessage
	}

	resultsByID := make(map[string][]indexedToolResult, len(toolResults))
	for idx, result := range toolResults {
		toolCallID := strings.TrimSpace(result.ToolCallID)
		if toolCallID == "" {
			continue
		}
		resultsByID[toolCallID] = append(resultsByID[toolCallID], indexedToolResult{index: idx, result: result})
	}

	consumedResults := make(map[int]struct{}, len(toolResults))
	for _, toolCallMessage := range toolCalls {
		if len(toolCallMessage.ToolCalls) == 0 {
			continue
		}
		filteredCalls := make([]apicompat.ChatToolCall, 0, len(toolCallMessage.ToolCalls))
		for _, toolCall := range toolCallMessage.ToolCalls {
			toolCallID := strings.TrimSpace(toolCall.ID)
			if toolCallID == "" || len(resultsByID[toolCallID]) == 0 {
				continue
			}
			filteredCalls = append(filteredCalls, toolCall)
		}
		if len(filteredCalls) == 0 {
			continue
		}
		toolCallMessage.ToolCalls = filteredCalls
		messages = append(messages, toolCallMessage)
		for _, toolCall := range filteredCalls {
			for _, item := range resultsByID[strings.TrimSpace(toolCall.ID)] {
				if _, ok := consumedResults[item.index]; ok {
					continue
				}
				consumedResults[item.index] = struct{}{}
				messages = append(messages, item.result)
			}
		}
	}

	for idx, result := range toolResults {
		if _, ok := consumedResults[idx]; ok {
			continue
		}
		toolCallID := strings.TrimSpace(result.ToolCallID)
		if toolCallID == "" {
			continue
		}
		messages = append(messages, augmentLegacySyntheticToolCallMessageForResult(result))
		messages = append(messages, result)
	}

	return messages
}

func augmentLegacySyntheticToolCallMessageForResult(result apicompat.ChatMessage) apicompat.ChatMessage {
	toolCallID := strings.TrimSpace(result.ToolCallID)
	return apicompat.ChatMessage{
		Role: "assistant",
		ToolCalls: []apicompat.ChatToolCall{{
			ID:   toolCallID,
			Type: "function",
			Function: apicompat.ChatFunctionCall{
				Name:      augmentLegacyInferToolNameForToolResult(result),
				Arguments: "{}",
			},
		}},
	}
}

func augmentLegacyInferToolNameForToolResult(result apicompat.ChatMessage) string {
	content, err := decodeOpenAIMessageContent(result.Content)
	if err == nil && strings.Contains(strings.ToUpper(content), "[CODEBASE_RETRIEVAL]") {
		return "codebase-retrieval"
	}
	return "codebase-retrieval"
}

func augmentLegacyResolveChatUserInput(req augmentLegacyChatRequest) augmentLegacyResolvedChatUserInput {
	parts := make([]string, 0, 4)
	seen := make(map[string]struct{}, 4)
	explicitCandidates := []string{
		req.Message,
		req.Prompt,
		req.Instruction,
	}
	for _, candidate := range explicitCandidates {
		text := strings.TrimSpace(candidate)
		if text == "" {
			continue
		}
		if _, ok := seen[text]; ok {
			continue
		}
		seen[text] = struct{}{}
		parts = append(parts, candidate)
	}
	explicitText := augmentLegacyCompactTextParts(parts...)
	hasToolResults := augmentLegacyHasToolResultNodes(req.Nodes, req.StructuredRequestNodes, req.RequestNodes)
	nodeTextCandidates := []string{
		augmentLegacyExtractRequestNodeText(req.StructuredRequestNodes),
		augmentLegacyExtractRequestNodeText(req.RequestNodes),
	}
	if !hasToolResults {
		nodeTextCandidates = append(nodeTextCandidates, augmentLegacyExtractRequestNodeText(req.Nodes))
	}
	for _, candidate := range nodeTextCandidates {
		text := strings.TrimSpace(candidate)
		if text == "" {
			continue
		}
		if _, ok := seen[text]; ok {
			continue
		}
		seen[text] = struct{}{}
		parts = append(parts, candidate)
	}

	resolvedText := augmentLegacyCompactTextParts(parts...)
	source := "empty"
	hasExplicitText := strings.TrimSpace(explicitText) != ""
	hasNodeText := strings.TrimSpace(resolvedText) != "" && strings.TrimSpace(resolvedText) != strings.TrimSpace(explicitText)
	switch {
	case hasExplicitText && hasNodeText:
		source = "explicit_message_plus_request_text_nodes"
	case hasExplicitText:
		source = "explicit_message"
	case strings.TrimSpace(resolvedText) != "":
		source = "request_text_nodes"
	case hasToolResults:
		source = "tool_result_followup"
	}

	return augmentLegacyResolvedChatUserInput{
		Text:           resolvedText,
		Source:         source,
		HasToolResults: hasToolResults,
	}
}

func augmentLegacyResolveChatUserMessage(req augmentLegacyChatRequest) string {
	return augmentLegacyResolveChatUserInput(req).Text
}

func augmentLegacyCaptureCaseIDFromChatRequest(req augmentLegacyChatRequest) string {
	candidates := []string{
		augmentLegacyResolveChatUserInput(req).Text,
		req.Message,
		req.Prompt,
		req.Instruction,
		req.ConversationID,
		req.ConversationIDCamel,
		req.Path,
		req.SelectedCode,
		req.SelectedCodeCamel,
		req.SelectedText,
		req.SelectedTextCamel,
	}
	for _, item := range req.ChatHistory {
		candidates = append(candidates,
			item.RequestMessage,
			item.RequestMessageCamel,
			item.Message,
			item.ResponseText,
			item.ResponseTextCamel,
			item.Response,
			item.Text,
		)
	}
	for _, item := range req.ChatHistoryCamel {
		candidates = append(candidates,
			item.RequestMessage,
			item.RequestMessageCamel,
			item.Message,
			item.ResponseText,
			item.ResponseTextCamel,
			item.Response,
			item.Text,
		)
	}
	for _, candidate := range candidates {
		if match := augmentLegacyCaseMarkerPattern.FindStringSubmatch(candidate); len(match) == 2 {
			return sanitizeCaptureComponent(match[1])
		}
	}
	return ""
}

func augmentLegacyResolveInstructionText(instruction string, nodes []augmentLegacyChatNode) string {
	if text := strings.TrimSpace(instruction); text != "" {
		return text
	}
	return strings.TrimSpace(augmentLegacyExtractNodeText(nodes))
}

func augmentLegacyDecodeRequest(c *gin.Context, out any) bool {
	raw, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request: " + err.Error()})
		return false
	}
	if len(raw) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request: empty body"})
		return false
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(raw))

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request: " + err.Error()})
		return false
	}
	if _, hasEncrypted := payload["encrypted_data"]; hasEncrypted {
		augmentLegacyTrace(c, "wrapper_rejected", "code", augmentLegacyUnsupportedWrapperPayloadCode)
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    augmentLegacyUnsupportedWrapperPayloadCode,
			"message": "wrapped protocol payload is not supported by the local augment compatibility layer",
		})
		return false
	}
	if _, hasIV := payload["iv"]; hasIV {
		augmentLegacyTrace(c, "wrapper_rejected", "code", augmentLegacyUnsupportedWrapperPayloadCode)
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    augmentLegacyUnsupportedWrapperPayloadCode,
			"message": "wrapped protocol payload is not supported by the local augment compatibility layer",
		})
		return false
	}
	if err := json.Unmarshal(raw, out); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request: " + err.Error()})
		return false
	}
	return true
}

func (h *AuthHandler) augmentLegacyNamespace(c *gin.Context, principals ...*service.AugmentPluginPrincipal) string {
	var principal *service.AugmentPluginPrincipal
	if len(principals) > 0 {
		principal = principals[0]
	}
	return buildAugmentLegacyNamespaceWithFallback(
		principal,
		strings.TrimSpace(c.GetHeader("x-request-session-id")),
		extractBearerToken(c.GetHeader("Authorization")),
	)
}

func buildAugmentLegacyNamespace(principal *service.AugmentPluginPrincipal, sessionID string) string {
	return buildAugmentLegacyNamespaceWithFallback(principal, sessionID, "")
}

func buildAugmentLegacyNamespaceWithFallback(principal *service.AugmentPluginPrincipal, sessionID, bearerToken string) string {
	principalNamespace := "principal:authenticated"
	switch {
	case principal != nil && principal.APIKey != nil && principal.APIKey.ID > 0:
		principalNamespace = fmt.Sprintf("api_key:%d", principal.APIKey.ID)
	case principal != nil && principal.User != nil && principal.User.ID > 0:
		principalNamespace = fmt.Sprintf("user:%d", principal.User.ID)
	case principal != nil && principal.APIKey != nil && principal.APIKey.UserID > 0:
		principalNamespace = fmt.Sprintf("user:%d", principal.APIKey.UserID)
	case strings.TrimSpace(bearerToken) != "":
		sum := sha256.Sum256([]byte(strings.TrimSpace(bearerToken)))
		principalNamespace = "bearer:" + hex.EncodeToString(sum[:8])
	}

	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return principalNamespace + ":default"
	}
	return principalNamespace + ":session:" + sessionID
}

func augmentLegacyTrace(c *gin.Context, event string, attrs ...any) {
	fields := []any{
		"event", event,
		"request_id", strings.TrimSpace(c.GetHeader("x-request-id")),
		"session_id", strings.TrimSpace(c.GetHeader("x-request-session-id")),
	}
	fields = append(fields, attrs...)
	slog.Info("augment_compat_trace", fields...)
}

func augmentLegacyToolDefinitionNames(defs []augmentLegacyToolDefinition) []string {
	names := make([]string, 0, len(defs))
	seen := make(map[string]struct{}, len(defs))
	for _, def := range defs {
		name := strings.TrimSpace(def.Name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		names = append(names, name)
	}
	return names
}

func augmentLegacyHasNamedToolDefinition(defs []augmentLegacyToolDefinition, toolName string) bool {
	toolName = strings.TrimSpace(strings.ToLower(toolName))
	if toolName == "" {
		return false
	}
	for _, def := range defs {
		if strings.TrimSpace(strings.ToLower(def.Name)) == toolName {
			return true
		}
	}
	return false
}

func augmentLegacyHasToolUseNodes(toolName string, nodeSets ...[]augmentLegacyChatNode) bool {
	toolName = strings.TrimSpace(strings.ToLower(toolName))
	if toolName == "" {
		return false
	}
	for _, nodes := range nodeSets {
		for _, node := range nodes {
			toolUse := augmentLegacyNormalizeToolUseNode(node)
			if toolUse == nil {
				continue
			}
			name := strings.TrimSpace(strings.ToLower(augmentLegacyFirstNonBlank(toolUse.ToolName, toolUse.ToolNameCamel)))
			if name == toolName {
				return true
			}
		}
	}
	return false
}

func augmentLegacyBuildChatObservability(req augmentLegacyChatRequest, retrieval string) augmentLegacyChatObservability {
	resolvedUserInput := augmentLegacyResolveChatUserInput(req)
	return augmentLegacyChatObservability{
		InboundToolDefinitionCount: len(req.ToolDefinitions),
		InboundToolDefinitionNames: augmentLegacyToolDefinitionNames(req.ToolDefinitions),
		HasToolResultFollowup:      resolvedUserInput.HasToolResults,
		LocalRetrievalInjected:     strings.TrimSpace(retrieval) != "",
		ResolvedUserInputSource:    resolvedUserInput.Source,
		ResolvedUserInputBytes:     len(resolvedUserInput.Text),
	}
}

func augmentLegacyTraceChatRequest(c *gin.Context, event string, req augmentLegacyChatRequest, retrieval string) {
	obs := augmentLegacyBuildChatObservability(req, retrieval)
	contextBundle := augmentContextBundleFromChatRequest(req)
	fields := []any{
		"requested_model", strings.TrimSpace(req.Model),
		"message_present", strings.TrimSpace(req.Message) != "",
		"nodes", len(req.Nodes),
		"request_nodes", len(req.RequestNodes),
		"structured_request_nodes", len(req.StructuredRequestNodes),
		"tool_definitions", len(req.ToolDefinitions),
		"inbound_tool_definition_count", obs.InboundToolDefinitionCount,
		"inbound_tool_definition_names", obs.InboundToolDefinitionNames,
		"has_tool_result_followup", obs.HasToolResultFollowup,
		"local_retrieval_injected", obs.LocalRetrievalInjected,
		"resolved_user_input_source", obs.ResolvedUserInputSource,
		"resolved_user_input_bytes", obs.ResolvedUserInputBytes,
	}
	fields = append(fields, contextBundle.TraceFields()...)
	augmentLegacyTrace(c, event, fields...)
}

func augmentLegacyEnsureNonEmptyInput(c *gin.Context, input string) bool {
	if strings.TrimSpace(input) != "" {
		return true
	}
	c.JSON(http.StatusBadRequest, gin.H{
		"code":    augmentLegacyEmptyUserInputCode,
		"message": "no usable user input was found in the augment request",
	})
	return false
}

func augmentLegacyEnsureNonEmptyChatInput(c *gin.Context, input string, hasToolResults bool) bool {
	if strings.TrimSpace(input) != "" || hasToolResults {
		return true
	}
	return augmentLegacyEnsureNonEmptyInput(c, input)
}

func (h *AuthHandler) augmentLegacyBuildChatMessages(req augmentLegacyChatRequest, retrieval string) []apicompat.ChatMessage {
	messages := make([]apicompat.ChatMessage, 0, len(req.ChatHistory)*4+6)

	system := augmentLegacyCompactTextParts(
		req.UserGuidelines,
		req.WorkspaceGuidelines,
		augmentLegacyJoinRuleTexts(req.Rules),
		req.SystemPrompt,
		req.SystemPromptAppend,
		augmentLegacySystemPromptReplacementText(req.SystemPromptReplacements),
	)
	if system != "" {
		messages = append(messages, augmentLegacyMakeMessage("system", system))
	}

	for _, item := range req.ChatHistory {
		if text := augmentLegacyHistoryRequestText(item); text != "" {
			messages = append(messages, augmentLegacyMakeMessage("user", text))
		}
		toolResults := augmentLegacyExtractToolResultMessages(item.RequestNodes)
		messages = augmentLegacyAppendToolExchangeMessages(messages, augmentLegacyExtractToolCallMessages(item.ResponseNodes), toolResults)
		if text := strings.TrimSpace(item.ResponseText); text != "" {
			messages = append(messages, augmentLegacyMakeMessage("assistant", text))
		}
	}

	if text := strings.TrimSpace(retrieval); text != "" {
		messages = append(messages, augmentLegacyMakeMessage("assistant", text))
	}

	currentToolResults := augmentLegacyExtractToolResultMessages(augmentLegacyMergeNodes(req.Nodes, req.StructuredRequestNodes, req.RequestNodes))
	if len(currentToolResults) > 0 {
		messages = augmentLegacyAppendToolExchangeMessages(messages, nil, currentToolResults)
	}

	userText := augmentLegacyCompactTextParts(
		augmentLegacyResolveChatUserMessage(req),
		func() string {
			if strings.TrimSpace(req.Path) == "" && strings.TrimSpace(req.Lang) == "" {
				return ""
			}
			return fmt.Sprintf("path=%s\nlang=%s", strings.TrimSpace(req.Path), strings.TrimSpace(req.Lang))
		}(),
		func() string {
			if strings.TrimSpace(req.SelectedCode) == "" {
				return ""
			}
			return "selected_code:\n" + req.SelectedCode
		}(),
	)
	if strings.TrimSpace(userText) != "" {
		messages = append(messages, augmentLegacyMakeMessage("user", userText))
	}
	return messages
}

func augmentLegacyLoopbackPromptCacheKey(ginContext *gin.Context, req augmentLegacyChatRequest) string {
	sessionID := ""
	if ginContext != nil {
		sessionID = strings.TrimSpace(ginContext.GetHeader("x-request-session-id"))
	}
	conversationID := strings.TrimSpace(req.ConversationID)
	if sessionID == "" && conversationID == "" {
		return ""
	}

	parts := []string{"augment_legacy"}
	if sessionID != "" {
		parts = append(parts, "session="+sessionID)
	}
	if conversationID != "" {
		parts = append(parts, "conversation="+conversationID)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return "augment_legacy_" + hex.EncodeToString(sum[:8])
}

func augmentLegacyAttachLoopbackPromptCacheKey(ginContext *gin.Context, req augmentLegacyChatRequest, body map[string]any) {
	if body == nil {
		return
	}
	if existing, ok := body["prompt_cache_key"].(string); ok && strings.TrimSpace(existing) != "" {
		return
	}
	if cacheKey := augmentLegacyLoopbackPromptCacheKey(ginContext, req); cacheKey != "" {
		body["prompt_cache_key"] = cacheKey
	}
}

func (h *AuthHandler) augmentLegacyResolveRetrieval(c *gin.Context, principal *service.AugmentPluginPrincipal, req augmentLegacyChatRequest) (string, []string, bool) {
	namespace := h.augmentLegacyNamespace(c, principal)
	resolved := h.augmentPluginService.ResolveLegacyBlobsForNamespace(namespace, req.Blobs.CheckpointID, req.Blobs.AddedBlobs, req.Blobs.DeletedBlobs)
	resolvedUserInput := augmentLegacyResolveChatUserInput(req)
	question := resolvedUserInput.Text
	toolDefinitionNames := augmentLegacyToolDefinitionNames(req.ToolDefinitions)
	hasCodebaseToolDefinition := augmentLegacyHasNamedToolDefinition(req.ToolDefinitions, "codebase-retrieval")
	hasCodebaseToolFollowup := augmentLegacyHasToolUseNodes("codebase-retrieval", req.Nodes, req.StructuredRequestNodes, req.RequestNodes)
	skipReason := ""
	switch {
	case req.DisableRetrieval:
		skipReason = "disable_retrieval"
	case hasCodebaseToolDefinition:
		skipReason = "official_codebase_retrieval_tool_definition"
	case hasCodebaseToolFollowup:
		skipReason = "codebase_retrieval_followup_node"
	case resolvedUserInput.HasToolResults:
		skipReason = "tool_result_node_present"
	case strings.TrimSpace(question) == "":
		skipReason = "empty_question"
	}
	contextBundle := augmentContextBundleFromChatRequest(req)
	traceFields := []any{
		"namespace", namespace,
		"checkpoint_id", strings.TrimSpace(req.Blobs.CheckpointID),
		"record_count", len(resolved.Records),
		"active_blob_count", len(resolved.Records) + len(resolved.Unknown),
		"added_blobs", len(req.Blobs.AddedBlobs),
		"deleted_blobs", len(req.Blobs.DeletedBlobs),
		"unknown_blobs", len(resolved.Unknown),
		"checkpoint_not_found", resolved.CheckpointNotFound,
		"resolution_reason", resolved.ResolutionReason,
		"tool_definitions_count", len(toolDefinitionNames),
		"tool_definition_names", strings.Join(toolDefinitionNames, ","),
		"resolved_user_input_source", resolvedUserInput.Source,
		"resolved_user_input_bytes", len(question),
		"has_codebase_retrieval_tool_definition", hasCodebaseToolDefinition,
		"has_codebase_retrieval_followup_node", hasCodebaseToolFollowup,
		"local_retrieval_injected", skipReason == "",
		"local_retrieval_skipped_reason", skipReason,
	}
	traceFields = append(traceFields, contextBundle.TraceFields()...)
	augmentLegacyTrace(c, "resolve_retrieval", traceFields...)
	if skipReason != "" {
		return "", resolved.Unknown, resolved.CheckpointNotFound
	}
	text := h.augmentPluginService.BuildLegacyFormattedRetrieval(question, resolved, 4000)
	return text, resolved.Unknown, resolved.CheckpointNotFound
}

func augmentLegacyRequestedModelScope(model string) string {
	normalized := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.HasPrefix(normalized, "claude"):
		return "claude"
	case strings.HasPrefix(normalized, "gemini"):
		return "gemini_text"
	default:
		return ""
	}
}

func augmentLegacyCompatSupportsRequestedModel(model string, compat *service.AugmentPluginCompatMetadata) bool {
	scope := augmentLegacyRequestedModelScope(model)
	if scope == "" || compat == nil {
		return true
	}

	for _, supported := range compat.ModelRegistry.SupportedModelScopes {
		if strings.EqualFold(strings.TrimSpace(supported), scope) {
			return true
		}
	}

	for _, group := range compat.ModelRegistry.Groups {
		switch scope {
		case "claude":
			if strings.EqualFold(group.Platform, service.PlatformAnthropic) || strings.EqualFold(group.Platform, service.PlatformAntigravity) {
				return true
			}
		case "gemini_text":
			if strings.EqualFold(group.Platform, service.PlatformGemini) {
				return true
			}
		}
		for _, supported := range group.SupportedModelScopes {
			if strings.EqualFold(strings.TrimSpace(supported), scope) {
				return true
			}
		}
	}
	return false
}

func augmentLegacyGroupHasScope(group *service.Group, scope string) bool {
	if group == nil || strings.TrimSpace(scope) == "" {
		return false
	}
	for _, supported := range group.SupportedModelScopes {
		if strings.EqualFold(strings.TrimSpace(supported), scope) {
			return true
		}
	}
	return false
}

func augmentLegacyFallbackModelForGroup(group *service.Group) string {
	if group == nil {
		return "gpt-5.4"
	}
	if fallback := strings.TrimSpace(group.DefaultMappedModel); fallback != "" {
		return fallback
	}
	switch group.Platform {
	case service.PlatformOpenAI:
		return "gpt-5.4"
	case service.PlatformAnthropic, service.PlatformAntigravity:
		if augmentLegacyGroupHasScope(group, "gemini_text") && !augmentLegacyGroupHasScope(group, "claude") {
			return "gemini-2.5-pro"
		}
		return "claude-sonnet-4-5"
	case service.PlatformGemini:
		return "gemini-2.5-pro"
	default:
		if augmentLegacyGroupHasScope(group, "claude") {
			return "claude-sonnet-4-5"
		}
		if augmentLegacyGroupHasScope(group, "gemini_text") {
			return "gemini-2.5-pro"
		}
		return "gpt-5.4"
	}
}

func augmentLegacyCurrentAPIKeyGroup(principal *service.AugmentPluginPrincipal) *service.Group {
	if principal == nil || principal.APIKey == nil {
		return nil
	}
	if principal.APIKey.Group != nil && principal.APIKey.Group.ID > 0 {
		return principal.APIKey.Group
	}
	return nil
}

func (h *AuthHandler) augmentLegacyResolveModel(c *gin.Context, principal *service.AugmentPluginPrincipal, raw string) string {
	model := strings.TrimSpace(raw)
	fallbackModel := "gpt-5.4"
	var compat *service.AugmentPluginCompatMetadata
	currentGroup := augmentLegacyCurrentAPIKeyGroup(principal)
	if principal != nil && principal.APIKey != nil && currentGroup != nil && strings.EqualFold(currentGroup.Platform, service.PlatformOpenAI) {
		fallbackModel = resolveOpenAIForwardDefaultMappedModel(principal.APIKey, fallbackModel)
	} else if currentGroup != nil {
		if fallback := augmentLegacyFallbackModelForGroup(currentGroup); fallback != "" {
			fallbackModel = fallback
		}
	}
	if h.augmentPluginService != nil && principal != nil {
		if builtCompat, err := h.augmentPluginService.BuildCompatMetadata(c.Request.Context(), *principal, h.augmentGatewayBaseURL(c)); err == nil {
			compat = builtCompat
			if currentGroup == nil {
				if fallback := strings.TrimSpace(builtCompat.DefaultModel); fallback != "" {
					fallbackModel = fallback
				}
			}
		}
	}
	if model == "" {
		return fallbackModel
	}
	if principal != nil && principal.APIKey != nil && currentGroup != nil && strings.EqualFold(currentGroup.Platform, service.PlatformOpenAI) {
		if augmentLegacyRequestedModelScope(model) != "" {
			if currentGroup.AllowMessagesDispatch {
				if mapped := resolveOpenAIMessagesDispatchMappedModel(principal.APIKey, model); mapped != "" {
					augmentLegacyTrace(
						c,
						"resolve_model_fallback",
						"requested_model", model,
						"resolved_model", mapped,
						"reason", "openai_messages_dispatch",
					)
					return mapped
				}
			}
			augmentLegacyTrace(
				c,
				"resolve_model_fallback",
				"requested_model", model,
				"resolved_model", fallbackModel,
				"reason", "openai_group_non_openai_model_fallback",
			)
			return fallbackModel
		}
	}
	if !augmentLegacyCompatSupportsRequestedModel(model, compat) {
		augmentLegacyTrace(
			c,
			"resolve_model_fallback",
			"requested_model", model,
			"resolved_model", fallbackModel,
			"reason", "requested_model_scope_unavailable",
		)
		return fallbackModel
	}
	return model
}

func (h *AuthHandler) augmentLegacyLoopbackChatCompletion(
	ctx context.Context,
	c *gin.Context,
	bearer string,
	endpoint string,
	reqBody []byte,
	captureMeta *augmentLegacyFinalEnvelopeCaptureMeta,
) (*augmentLegacyLoopbackChatResult, error) {
	baseURL := h.augmentLegacyLoopbackBaseURL(c)
	startedAt := time.Now()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/openai/v1/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+bearer)
	augmentLegacySetLoopbackCodexHeaders(httpReq)
	h.augmentLegacyCaptureFinalEnvelope(c, endpoint, reqBody, nil, 0, nil, startedAt, augmentLegacyCaptureMetaOrZero(captureMeta))

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("loopback chat completion %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	var out apicompat.ChatCompletionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}

	text := ""
	if len(out.Choices) > 0 {
		if content, err := decodeOpenAIMessageContent(out.Choices[0].Message.Content); err == nil {
			text = content
		}
	}
	return &augmentLegacyLoopbackChatResult{Response: &out, Text: text}, nil
}

func (h *AuthHandler) augmentLegacyLoopbackChatCompletionStream(
	ctx context.Context,
	c *gin.Context,
	bearer string,
	endpoint string,
	reqBody []byte,
	captureMeta *augmentLegacyFinalEnvelopeCaptureMeta,
	onChunk func(apicompat.ChatCompletionsChunk) error,
) error {
	baseURL := h.augmentLegacyLoopbackBaseURL(c)
	startedAt := time.Now()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/openai/v1/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+bearer)
	augmentLegacySetLoopbackCodexHeaders(httpReq)
	h.augmentLegacyCaptureFinalEnvelope(c, endpoint, reqBody, nil, 0, nil, startedAt, augmentLegacyCaptureMetaOrZero(captureMeta))

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("loopback chat completion stream %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	contentType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if !strings.Contains(contentType, "text/event-stream") {
		var out apicompat.ChatCompletionsResponse
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return err
		}
		if len(out.Choices) > 0 {
			msg := out.Choices[0].Message
			if strings.TrimSpace(msg.ReasoningContent) != "" {
				reasoning := msg.ReasoningContent
				if err := onChunk(apicompat.ChatCompletionsChunk{
					ID:      out.ID,
					Object:  "chat.completion.chunk",
					Created: out.Created,
					Model:   out.Model,
					Choices: []apicompat.ChatChunkChoice{{Index: 0, Delta: apicompat.ChatDelta{ReasoningContent: &reasoning}}},
				}); err != nil {
					return err
				}
			}
			if text, err := decodeOpenAIMessageContent(msg.Content); err == nil && strings.TrimSpace(text) != "" {
				content := text
				if err := onChunk(apicompat.ChatCompletionsChunk{
					ID:      out.ID,
					Object:  "chat.completion.chunk",
					Created: out.Created,
					Model:   out.Model,
					Choices: []apicompat.ChatChunkChoice{{Index: 0, Delta: apicompat.ChatDelta{Content: &content}}},
				}); err != nil {
					return err
				}
			}
			for _, toolCall := range msg.ToolCalls {
				if err := onChunk(apicompat.ChatCompletionsChunk{
					ID:      out.ID,
					Object:  "chat.completion.chunk",
					Created: out.Created,
					Model:   out.Model,
					Choices: []apicompat.ChatChunkChoice{{Index: 0, Delta: apicompat.ChatDelta{ToolCalls: []apicompat.ChatToolCall{toolCall}}}},
				}); err != nil {
					return err
				}
			}
			finishReason := out.Choices[0].FinishReason
			if err := onChunk(apicompat.ChatCompletionsChunk{
				ID:      out.ID,
				Object:  "chat.completion.chunk",
				Created: out.Created,
				Model:   out.Model,
				Choices: []apicompat.ChatChunkChoice{{Index: 0, Delta: apicompat.ChatDelta{}, FinishReason: &finishReason}},
				Usage:   out.Usage,
			}); err != nil {
				return err
			}
			return nil
		}
		if out.Usage != nil {
			return onChunk(apicompat.ChatCompletionsChunk{
				ID:      out.ID,
				Object:  "chat.completion.chunk",
				Created: out.Created,
				Model:   out.Model,
				Choices: []apicompat.ChatChunkChoice{},
				Usage:   out.Usage,
			})
		}
		return nil
	}

	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 2*1024*1024)

	dataLines := make([]string, 0, 4)
	flushEvent := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		data := strings.TrimSpace(strings.Join(dataLines, "\n"))
		dataLines = dataLines[:0]
		if data == "" || data == "[DONE]" {
			return nil
		}
		var chunk apicompat.ChatCompletionsChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return err
		}
		return onChunk(chunk)
	}

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if err := flushEvent(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(trimmed, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flushEvent()
}

func (h *AuthHandler) augmentLegacyChatStreamThroughGateway(
	c *gin.Context,
	principal *service.AugmentPluginPrincipal,
	req augmentLegacyChatRequest,
	retrieval string,
	unknown []string,
	checkpointNotFound bool,
) error {
	gatewayReq, _, err := h.augmentLegacyBuildGatewayRequest(c, principal, req, retrieval, true)
	if err != nil {
		if augmentLegacyGatewayUnavailable(err) {
			augmentLegacyTraceGatewayUnavailable(c, "chat_stream_gateway_unavailable", req, service.AugmentGatewayRoutedModel{}, err)
			c.JSON(http.StatusBadRequest, gin.H{"code": augmentLegacyGatewayUnavailableCode(err), "message": err.Error()})
			return nil
		}
		return err
	}

	nextID := 0
	firstChunk := true
	streamStarted := false
	finalStopReason := augmentStopReasonEndTurn
	toolCallBuffer := newAugmentLegacyStreamToolCallBuffer()
	var reasoningBuffer strings.Builder
	aggregate := service.AugmentGatewayProviderResult{
		Provider:      gatewayReq.Model.Provider,
		ModelID:       gatewayReq.Model.ID,
		UpstreamModel: gatewayReq.Model.UpstreamModel,
	}

	emitChunk := func(chunk gin.H) error {
		if !streamStarted {
			c.Header("Content-Type", "application/x-ndjson")
			c.Status(http.StatusOK)
			streamStarted = true
		}
		b, err := json.Marshal(chunk)
		if err != nil {
			return err
		}
		_, err = c.Writer.Write(append(b, '\n'))
		if err != nil {
			return err
		}
		c.Writer.Flush()
		return nil
	}
	emitWithMeta := func(text string, nodes []gin.H, stopReason *int) error {
		chunk := augmentLegacyChatChunk(text, nodes, stopReason, unknown, checkpointNotFound)
		if firstChunk {
			firstChunk = false
		} else {
			chunk["unknown_blob_names"] = []string{}
			chunk["checkpoint_not_found"] = false
		}
		return emitChunk(chunk)
	}

	err = h.augmentGatewayService.Executor().Stream(c.Request.Context(), gatewayReq, func(chunk service.AugmentGatewayProviderChunk) error {
		pendingChunks := make([]gin.H, 0, 4)
		flushReasoning := func() {
			summary := reasoningBuffer.String()
			if strings.TrimSpace(summary) == "" {
				reasoningBuffer.Reset()
				return
			}
			nextID++
			pendingChunks = append(pendingChunks, augmentLegacyChatChunk("", []gin.H{augmentLegacyThinkingNode(nextID, summary)}, nil, unknown, checkpointNotFound))
			reasoningBuffer.Reset()
		}

		if chunk.ProviderFinishReason != "" {
			finalStopReason = augmentLegacyMapOpenAIFinishReason(chunk.ProviderFinishReason)
		}
		if chunk.RequestID != "" {
			aggregate.RequestID = chunk.RequestID
		}
		if chunk.UpstreamRequestID != "" {
			aggregate.UpstreamRequestID = chunk.UpstreamRequestID
		}
		if chunk.TextDelta != "" {
			flushReasoning()
			aggregate.Text += chunk.TextDelta
			nextID++
			pendingChunks = append(pendingChunks, augmentLegacyChatChunk(chunk.TextDelta, []gin.H{augmentLegacyRawResponseNode(nextID, chunk.TextDelta)}, nil, unknown, checkpointNotFound))
		}
		if chunk.ReasoningContentDelta != "" || chunk.ReasoningContentDone {
			if chunk.ReasoningContentDelta != "" {
				aggregate.ReasoningContent += chunk.ReasoningContentDelta
				reasoningBuffer.WriteString(chunk.ReasoningContentDelta)
			}
			aggregate.ReasoningContentPresent = true
			if chunk.ReasoningContentDone && chunk.ReasoningContentDelta == "" {
				flushReasoning()
			}
		}
		if chunk.ToolCallDelta != nil {
			flushReasoning()
			toolCallBuffer.absorb([]apicompat.ChatToolCall{{
				Index: chunk.ToolCallDelta.Index,
				ID:    chunk.ToolCallDelta.ID,
				Type:  chunk.ToolCallDelta.Type,
				Function: apicompat.ChatFunctionCall{
					Name:      chunk.ToolCallDelta.Function.Name,
					Arguments: chunk.ToolCallDelta.Function.Arguments,
				},
			}})
		}
		if nodes, invalidCount := toolCallBuffer.flushReady(&nextID, chunk.Done || chunk.ProviderFinishReason != ""); len(nodes) > 0 {
			pendingChunks = append(pendingChunks, augmentLegacyChatChunk("", nodes, nil, unknown, checkpointNotFound))
			aggregate.ToolCalls = toolCallBuffer.materialize()
			if invalidCount > 0 {
				finalStopReason = augmentStopReasonEndTurn
				pendingChunks = append(pendingChunks, augmentLegacyChatChunk("Tool call failed: upstream returned incomplete tool arguments.", nil, nil, unknown, checkpointNotFound))
			}
		} else if invalidCount > 0 {
			finalStopReason = augmentStopReasonEndTurn
			pendingChunks = append(pendingChunks, augmentLegacyChatChunk("Tool call failed: upstream returned incomplete tool arguments.", nil, nil, unknown, checkpointNotFound))
		}
		if chunk.Usage.TotalTokens > 0 || chunk.Usage.InputTokens > 0 || chunk.Usage.OutputTokens > 0 {
			flushReasoning()
			aggregate.Usage = chunk.Usage
			nextID++
			usage := &apicompat.ChatUsage{
				PromptTokens:     chunk.Usage.InputTokens,
				CompletionTokens: chunk.Usage.OutputTokens,
				TotalTokens:      chunk.Usage.TotalTokens,
			}
			if chunk.Usage.CachedInputTokens > 0 {
				usage.PromptTokensDetails = &apicompat.ChatTokenDetails{CachedTokens: chunk.Usage.CachedInputTokens}
			}
			pendingChunks = append(pendingChunks, augmentLegacyChatChunk("", []gin.H{augmentLegacyTokenUsageNode(nextID, usage)}, nil, unknown, checkpointNotFound))
		}
		if chunk.Done || chunk.ProviderFinishReason != "" {
			flushReasoning()
		}
		for _, pending := range pendingChunks {
			text, _ := pending["text"].(string)
			nodes, _ := pending["nodes"].([]gin.H)
			var stopReason *int
			if rawStopReason, ok := pending["stop_reason"].(int); ok {
				stopReason = &rawStopReason
			}
			if err := emitWithMeta(text, nodes, stopReason); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		augmentLegacyTraceGatewayError(c, "chat_stream_gateway_error", gatewayReq, err)
		if !streamStarted {
			return err
		}
		_ = emitWithMeta(err.Error(), nil, ptrInt(augmentStopReasonEndTurn))
		return nil
	}

	aggregate.ToolCalls = toolCallBuffer.materialize()
	h.augmentLegacyStoreGatewayReasoningTurn(gatewayReq.ConversationID, gatewayReq.Model, aggregate)
	if !streamStarted {
		c.Header("Content-Type", "application/x-ndjson")
		c.Status(http.StatusOK)
	}
	return emitWithMeta("", nil, &finalStopReason)
}

func decodeOpenAIMessageContent(raw json.RawMessage) (string, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return "", nil
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString, nil
	}
	var parts []map[string]any
	if err := json.Unmarshal(raw, &parts); err == nil {
		var b strings.Builder
		for _, part := range parts {
			if text, ok := part["text"].(string); ok {
				if b.Len() > 0 {
					b.WriteString("\n")
				}
				b.WriteString(text)
			}
		}
		return b.String(), nil
	}
	return "", fmt.Errorf("unsupported content shape")
}

func augmentLegacyWriteNDJSON(c *gin.Context, chunks ...gin.H) {
	augmentLegacyWriteNDJSONStatus(c, http.StatusOK, chunks...)
}

func augmentLegacyWriteNDJSONStatus(c *gin.Context, status int, chunks ...gin.H) {
	c.Header("Content-Type", "application/x-ndjson")
	c.Status(status)
	for _, chunk := range chunks {
		if chunk == nil {
			continue
		}
		b, err := json.Marshal(chunk)
		if err != nil {
			continue
		}
		_, _ = c.Writer.Write(append(b, '\n'))
		c.Writer.Flush()
	}
}

func augmentLegacySetLoopbackCodexHeaders(req *http.Request) {
	if req == nil {
		return
	}
	if strings.TrimSpace(req.Header.Get("User-Agent")) == "" {
		req.Header.Set("User-Agent", "codex_cli_rs/0.125.0")
	}
	if strings.TrimSpace(req.Header.Get("originator")) == "" {
		req.Header.Set("originator", "codex_cli_rs")
	}
	if strings.TrimSpace(req.Header.Get("OpenAI-Beta")) == "" {
		req.Header.Set("OpenAI-Beta", "responses=experimental")
	}
}

func (h *AuthHandler) augmentLegacyGatewayReady() bool {
	return h != nil && h.augmentGatewayService != nil && h.augmentGatewayService.Executor() != nil && h.augmentGatewayService.Router() != nil
}

func (h *AuthHandler) augmentLegacyGatewayConversationID(c *gin.Context, req augmentLegacyChatRequest) string {
	return strings.TrimSpace(augmentLegacyFirstNonBlank(
		req.ConversationID,
		req.ConversationIDCamel,
		h.augmentLegacyNamespace(c),
	))
}

func (h *AuthHandler) augmentLegacyGatewayResolveModel(req augmentLegacyChatRequest) (service.AugmentGatewayRoutedModel, error) {
	if h == nil || h.augmentGatewayService == nil || h.augmentGatewayService.Router() == nil {
		return service.AugmentGatewayRoutedModel{}, fmt.Errorf("augment gateway router is unavailable")
	}
	return h.augmentGatewayService.Router().Resolve(req.Model)
}

func (h *AuthHandler) augmentLegacyGatewayMessages(conversationID string, req augmentLegacyChatRequest, retrieval string, routed service.AugmentGatewayRoutedModel) []apicompat.ChatMessage {
	messages := h.augmentLegacyBuildChatMessages(req, retrieval)
	store := h.augmentGatewayService.TurnStore()
	if store == nil {
		return messages
	}
	if conversationID == "" {
		return messages
	}
	out := make([]apicompat.ChatMessage, 0, len(messages))
	for _, msg := range messages {
		if len(msg.ToolCalls) > 0 {
			for _, toolCall := range msg.ToolCalls {
				if turn, ok := store.LookupLatestForConversationToolCall(conversationID, routed.Model.ID, toolCall.ID); ok {
					msg.ReasoningContent = turn.ReasoningContent
					msg.ToolCalls = augmentLegacyReplayStoredToolCalls(msg.ToolCalls, turn.ToolCalls)
					break
				}
			}
		}
		out = append(out, msg)
	}
	return out
}

func augmentLegacyReplayStoredToolCalls(current []apicompat.ChatToolCall, stored []service.AugmentGatewayToolCall) []apicompat.ChatToolCall {
	if len(current) == 0 || len(stored) == 0 {
		return current
	}
	byID := make(map[string]service.AugmentGatewayToolCall, len(stored))
	for _, toolCall := range stored {
		if id := strings.TrimSpace(toolCall.ID); id != "" {
			byID[id] = toolCall
		}
	}
	out := make([]apicompat.ChatToolCall, len(current))
	copy(out, current)
	for i, toolCall := range out {
		storedToolCall, ok := byID[strings.TrimSpace(toolCall.ID)]
		if !ok {
			continue
		}
		out[i] = apicompat.ChatToolCall{
			Index: storedToolCall.Index,
			ID:    storedToolCall.ID,
			Type:  augmentLegacyFirstNonBlank(storedToolCall.Type, toolCall.Type, "function"),
			Function: apicompat.ChatFunctionCall{
				Name:      augmentLegacyFirstNonBlank(storedToolCall.Function.Name, toolCall.Function.Name),
				Arguments: augmentLegacyFirstNonBlank(storedToolCall.Function.Arguments, toolCall.Function.Arguments),
			},
		}
	}
	return out
}

func augmentLegacyChatMessagesToRawMaps(messages []apicompat.ChatMessage) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		raw, err := json.Marshal(msg)
		if err != nil {
			continue
		}
		var cloned map[string]any
		if err := json.Unmarshal(raw, &cloned); err != nil {
			continue
		}
		out = append(out, cloned)
	}
	return out
}

func augmentLegacyToolDefinitionsToRawMaps(defs []apicompat.ChatTool) []map[string]any {
	out := make([]map[string]any, 0, len(defs))
	for _, def := range defs {
		raw, err := json.Marshal(def)
		if err != nil {
			continue
		}
		var cloned map[string]any
		if err := json.Unmarshal(raw, &cloned); err != nil {
			continue
		}
		out = append(out, cloned)
	}
	return out
}

func (h *AuthHandler) augmentLegacyBuildGatewayRequest(c *gin.Context, principal *service.AugmentPluginPrincipal, req augmentLegacyChatRequest, retrieval string, stream bool) (service.AugmentGatewayProviderRequest, service.AugmentGatewayRoutedModel, error) {
	routed, err := h.augmentLegacyGatewayResolveModel(req)
	if err != nil {
		return service.AugmentGatewayProviderRequest{}, service.AugmentGatewayRoutedModel{}, err
	}

	conversationID := h.augmentLegacyGatewayConversationID(c, req)
	messages := h.augmentLegacyGatewayMessages(conversationID, req, retrieval, routed)
	contextBundle := augmentContextBundleFromChatRequest(req)
	if augmentLegacyHasToolDefinition(req.ToolDefinitions, "codebase-retrieval") {
		policy := augmentLegacyTextWithContextBundle(augmentLegacyGatewayCodebaseRetrievalPolicy, contextBundle)
		switch routed.Model.Provider {
		case service.AugmentGatewayProviderOpenAI, service.AugmentGatewayProviderDeepSeek, service.AugmentGatewayProviderAnthropic:
			policy = augmentLegacyTextWithDeepSeekCacheStableContextBundle(augmentLegacyGatewayCodebaseRetrievalPolicy, contextBundle)
		}
		messages = append([]apicompat.ChatMessage{augmentLegacyMakeMessage("system", policy)}, messages...)
	}
	rawMessages := augmentLegacyChatMessagesToRawMaps(messages)
	body := map[string]any{
		"model":    routed.UpstreamModel,
		"messages": rawMessages,
		"stream":   stream,
	}
	if tools := h.augmentLegacyToolDefinitionsToOpenAI(req.ToolDefinitions); len(tools) > 0 {
		body["tools"] = tools
		body["tool_choice"] = "auto"
	}

	sessionHash := h.augmentLegacyNamespace(c, principal)

	return service.AugmentGatewayProviderRequest{
		Endpoint:       strings.TrimSpace(c.FullPath()),
		ConversationID: conversationID,
		RequestID:      strings.TrimSpace(c.GetHeader("X-Request-ID")),
		SessionHash:    sessionHash,
		Model:          routed.Model,
		APIKey:         principal.APIKey,
		User:           principal.User,
		UserAgent:      c.GetHeader("User-Agent"),
		IPAddress:      c.ClientIP(),
		Messages:       rawMessages,
		RawBody:        body,
		Metadata:       augmentLegacyContextBundleMetadata(contextBundle),
	}, routed, nil
}

func augmentLegacyHasToolDefinition(defs []augmentLegacyToolDefinition, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, def := range defs {
		if strings.TrimSpace(def.Name) == name {
			return true
		}
	}
	return false
}

func (h *AuthHandler) augmentLegacyStoreGatewayReasoningTurn(conversationID string, model service.AugmentGatewayModel, result service.AugmentGatewayProviderResult) {
	if h == nil || h.augmentGatewayService == nil || h.augmentGatewayService.TurnStore() == nil {
		return
	}
	if len(result.ToolCalls) == 0 {
		return
	}
	records, _, _, err := service.BuildAugmentGatewayReasoningTurnRecords(service.AugmentGatewayReasoningTurnWriteInput{
		ConversationID:          conversationID,
		RequestID:               result.RequestID,
		ModelID:                 strings.TrimSpace(augmentLegacyFirstNonBlank(result.ModelID, model.ID)),
		AssistantContent:        result.Text,
		ReasoningContent:        result.ReasoningContent,
		ReasoningContentPresent: result.ReasoningContentPresent,
		ToolCalls:               result.ToolCalls,
		StreamComplete:          true,
		UpstreamRequestID:       result.UpstreamRequestID,
	})
	if err != nil {
		return
	}
	store := h.augmentGatewayService.TurnStore()
	for _, record := range records {
		store.Store(record)
	}
}

func augmentLegacyGatewayUnavailableCode(err error) string {
	if _, ok := service.IsAugmentGatewayProviderUnavailable(err); ok {
		return "AUGMENT_GATEWAY_PROVIDER_UNAVAILABLE"
	}
	return "AUGMENT_GATEWAY_MODEL_UNAVAILABLE"
}

func augmentLegacyGatewayUnavailable(err error) bool {
	if _, ok := service.IsAugmentGatewayProviderUnavailable(err); ok {
		return true
	}
	if _, ok := service.IsAugmentGatewayModelUnavailable(err); ok {
		return true
	}
	return false
}

func augmentLegacyTraceGatewayUnavailable(c *gin.Context, event string, req augmentLegacyChatRequest, routed service.AugmentGatewayRoutedModel, err error) {
	if c == nil || err == nil {
		return
	}
	fields := []any{
		"requested_model", strings.TrimSpace(req.Model),
		"error_code", augmentLegacyGatewayUnavailableCode(err),
		"error", err.Error(),
	}
	if modelErr, ok := service.IsAugmentGatewayModelUnavailable(err); ok && modelErr != nil {
		fields = append(fields,
			"unavailable_kind", string(modelErr.Kind),
			"unavailable_model", modelErr.ModelID,
		)
	}
	if providerErr, ok := service.IsAugmentGatewayProviderUnavailable(err); ok && providerErr != nil {
		fields = append(fields,
			"unavailable_kind", string(providerErr.Kind),
			"unavailable_model", providerErr.ModelID,
			"provider", string(providerErr.Provider),
		)
	}
	if routed.Model.ID != "" || routed.UpstreamModel != "" || routed.Provider != "" {
		fields = append(fields,
			"routed_model", routed.Model.ID,
			"routed_provider", string(routed.Provider),
			"upstream_model", routed.UpstreamModel,
		)
	}
	augmentLegacyTrace(c, event, fields...)
}

func augmentLegacyTraceGatewayError(c *gin.Context, event string, req service.AugmentGatewayProviderRequest, err error) {
	if c == nil || err == nil {
		return
	}
	augmentLegacyTrace(c, event,
		"model", req.ModelID,
		"provider", string(req.Provider),
		"provider_group_id", req.ProviderGroupID,
		"upstream_model", req.UpstreamModel,
		"endpoint", req.Endpoint,
		"error", err.Error(),
	)
}

func (h *AuthHandler) AugmentLegacyChat(c *gin.Context) {
	if h.augmentPluginService == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "augment plugin service is unavailable"})
		return
	}
	principal, ok := h.augmentPrincipalFromBearer(c)
	if !ok {
		return
	}

	var req augmentLegacyChatRequest
	if !augmentLegacyDecodeRequest(c, &req) {
		return
	}
	augmentLegacyNormalizeChatRequest(&req)

	resolvedUserInput := augmentLegacyResolveChatUserInput(req)
	if !augmentLegacyEnsureNonEmptyChatInput(c, resolvedUserInput.Text, resolvedUserInput.HasToolResults) {
		return
	}

	bearer, err := h.augmentLegacyGatewayBearer(c.Request.Context(), principal)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": err.Error()})
		return
	}

	retrieval, unknown, checkpointNotFound := h.augmentLegacyResolveRetrieval(c, principal, req)
	augmentLegacyTraceChatRequest(c, "chat_request", req, retrieval)
	if h.augmentLegacyGatewayReady() {
		gatewayReq, _, err := h.augmentLegacyBuildGatewayRequest(c, principal, req, retrieval, false)
		if err != nil {
			if augmentLegacyGatewayUnavailable(err) {
				augmentLegacyTraceGatewayUnavailable(c, "chat_gateway_unavailable", req, service.AugmentGatewayRoutedModel{}, err)
				c.JSON(http.StatusBadRequest, gin.H{"code": augmentLegacyGatewayUnavailableCode(err), "message": err.Error()})
			} else {
				c.JSON(http.StatusBadGateway, augmentLegacyChatChunk(err.Error(), nil, ptrInt(augmentStopReasonEndTurn), unknown, checkpointNotFound))
			}
			return
		}

		result, err := h.augmentGatewayService.Executor().Complete(c.Request.Context(), gatewayReq)
		if err != nil {
			augmentLegacyTraceGatewayError(c, "chat_gateway_error", gatewayReq, err)
			c.JSON(http.StatusBadGateway, augmentLegacyChatChunk(err.Error(), nil, ptrInt(augmentStopReasonEndTurn), unknown, checkpointNotFound))
			return
		}
		h.augmentLegacyStoreGatewayReasoningTurn(gatewayReq.ConversationID, gatewayReq.Model, result)

		nodes := make([]gin.H, 0, 4)
		nextID := 0
		if strings.TrimSpace(result.ReasoningContent) != "" || result.ReasoningContentPresent {
			nextID++
			nodes = append(nodes, augmentLegacyThinkingNode(nextID, result.ReasoningContent))
		}
		if strings.TrimSpace(result.Text) != "" {
			nextID++
			nodes = append(nodes, augmentLegacyRawResponseNode(nextID, result.Text))
		}
		for _, toolCall := range result.ToolCalls {
			nextID++
			nodes = append(nodes, augmentLegacyToolUseResponseNode(nextID, toolCall.ID, toolCall.Function.Name, toolCall.Function.Arguments))
		}
		if result.Usage.TotalTokens > 0 || result.Usage.InputTokens > 0 || result.Usage.OutputTokens > 0 {
			nextID++
			usage := &apicompat.ChatUsage{
				PromptTokens:     result.Usage.InputTokens,
				CompletionTokens: result.Usage.OutputTokens,
				TotalTokens:      result.Usage.TotalTokens,
			}
			if result.Usage.CachedInputTokens > 0 {
				usage.PromptTokensDetails = &apicompat.ChatTokenDetails{CachedTokens: result.Usage.CachedInputTokens}
			}
			nodes = append(nodes, augmentLegacyTokenUsageNode(nextID, usage))
		}
		c.JSON(http.StatusOK, augmentLegacyChatChunk(result.Text, nodes, ptrInt(augmentStopReasonEndTurn), unknown, checkpointNotFound))
		return
	}
	model := h.augmentLegacyResolveModel(c, principal, req.Model)
	messages := h.augmentLegacyBuildChatMessages(req, retrieval)
	body := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   false,
	}
	augmentLegacyAttachLoopbackPromptCacheKey(c, req, body)
	if tools := h.augmentLegacyToolDefinitionsToOpenAI(req.ToolDefinitions); len(tools) > 0 {
		body["tools"] = tools
		body["tool_choice"] = "auto"
	}
	reqBody, err := json.Marshal(body)
	if err != nil {
		c.JSON(http.StatusBadGateway, augmentLegacyChatChunk(err.Error(), nil, ptrInt(augmentStopReasonEndTurn), unknown, checkpointNotFound))
		return
	}
	captureMeta := augmentLegacyBuildFinalEnvelopeCaptureMeta(req, resolvedUserInput)

	res, err := h.augmentLegacyLoopbackChatCompletion(c.Request.Context(), c, bearer, "chat", reqBody, &captureMeta)
	if err != nil {
		c.JSON(http.StatusBadGateway, augmentLegacyChatChunk(err.Error(), nil, ptrInt(augmentStopReasonEndTurn), unknown, checkpointNotFound))
		return
	}

	nodes := make([]gin.H, 0, 4)
	nextID := 0
	if len(res.Response.Choices) > 0 {
		msg := res.Response.Choices[0].Message
		if strings.TrimSpace(msg.ReasoningContent) != "" {
			nextID++
			nodes = append(nodes, augmentLegacyThinkingNode(nextID, msg.ReasoningContent))
		}
		if strings.TrimSpace(res.Text) != "" {
			nextID++
			nodes = append(nodes, augmentLegacyRawResponseNode(nextID, res.Text))
		}
		for _, toolCall := range msg.ToolCalls {
			nextID++
			nodes = append(nodes, augmentLegacyToolUseResponseNode(nextID, toolCall.ID, toolCall.Function.Name, toolCall.Function.Arguments))
		}
	}
	if res.Response.Usage != nil {
		nextID++
		nodes = append(nodes, augmentLegacyTokenUsageNode(nextID, res.Response.Usage))
	}

	stopReason := augmentStopReasonEndTurn
	if len(res.Response.Choices) > 0 {
		stopReason = augmentLegacyMapOpenAIFinishReason(res.Response.Choices[0].FinishReason)
	}
	c.JSON(http.StatusOK, augmentLegacyChatChunk(res.Text, nodes, &stopReason, unknown, checkpointNotFound))
}

func (h *AuthHandler) AugmentLegacyChatStream(c *gin.Context) {
	if h.augmentPluginService == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "augment plugin service is unavailable"})
		return
	}
	principal, ok := h.augmentPrincipalFromBearer(c)
	if !ok {
		return
	}

	var req augmentLegacyChatRequest
	if !augmentLegacyDecodeRequest(c, &req) {
		return
	}
	augmentLegacyNormalizeChatRequest(&req)

	resolvedUserInput := augmentLegacyResolveChatUserInput(req)
	if !augmentLegacyEnsureNonEmptyChatInput(c, resolvedUserInput.Text, resolvedUserInput.HasToolResults) {
		return
	}

	bearer, err := h.augmentLegacyGatewayBearer(c.Request.Context(), principal)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": err.Error()})
		return
	}

	retrieval, unknown, checkpointNotFound := h.augmentLegacyResolveRetrieval(c, principal, req)
	augmentLegacyTraceChatRequest(c, "chat_stream_request", req, retrieval)
	if h.augmentLegacyGatewayReady() {
		if err := h.augmentLegacyChatStreamThroughGateway(c, principal, req, retrieval, unknown, checkpointNotFound); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{
				"message": "Upstream service temporarily unavailable, please retry.",
				"type":    "upstream_error",
			}})
		}
		return
	}
	model := h.augmentLegacyResolveModel(c, principal, req.Model)
	messages := h.augmentLegacyBuildChatMessages(req, retrieval)
	body := map[string]any{
		"model":    model,
		"messages": messages,
	}
	augmentLegacyAttachLoopbackPromptCacheKey(c, req, body)
	if tools := h.augmentLegacyToolDefinitionsToOpenAI(req.ToolDefinitions); len(tools) > 0 {
		body["tools"] = tools
		body["tool_choice"] = "auto"
	}
	body["stream"] = true
	reqBody, err := json.Marshal(body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{
			"message": "Upstream service temporarily unavailable, please retry.",
			"type":    "upstream_error",
		}})
		return
	}
	captureMeta := augmentLegacyBuildFinalEnvelopeCaptureMeta(req, resolvedUserInput)
	nextID := 0
	firstChunk := true
	streamStarted := false
	finalStopReason := augmentStopReasonEndTurn
	toolCallBuffer := newAugmentLegacyStreamToolCallBuffer()
	emitChunk := func(chunk gin.H) error {
		if !streamStarted {
			c.Header("Content-Type", "application/x-ndjson")
			c.Status(http.StatusOK)
			streamStarted = true
		}
		b, err := json.Marshal(chunk)
		if err != nil {
			return err
		}
		_, err = c.Writer.Write(append(b, '\n'))
		if err != nil {
			return err
		}
		c.Writer.Flush()
		return nil
	}
	emitWithMeta := func(text string, nodes []gin.H, stopReason *int) error {
		chunk := augmentLegacyChatChunk(text, nodes, stopReason, unknown, checkpointNotFound)
		if firstChunk {
			firstChunk = false
		} else {
			chunk["unknown_blob_names"] = []string{}
			chunk["checkpoint_not_found"] = false
		}
		return emitChunk(chunk)
	}

	err = h.augmentLegacyLoopbackChatCompletionStream(c.Request.Context(), c, bearer, "chat-stream", reqBody, &captureMeta, func(streamChunk apicompat.ChatCompletionsChunk) error {
		pendingChunks := make([]gin.H, 0, 4)
		if len(streamChunk.Choices) > 0 {
			choice := streamChunk.Choices[0]
			if choice.FinishReason != nil {
				finalStopReason = augmentLegacyMapOpenAIFinishReason(*choice.FinishReason)
			}
			if content := choice.Delta.Content; content != nil && strings.TrimSpace(*content) != "" {
				nextID++
				pendingChunks = append(pendingChunks, augmentLegacyChatChunk(*content, []gin.H{augmentLegacyRawResponseNode(nextID, *content)}, nil, unknown, checkpointNotFound))
			}
			if thinking := choice.Delta.ReasoningContent; thinking != nil && strings.TrimSpace(*thinking) != "" {
				nextID++
				pendingChunks = append(pendingChunks, augmentLegacyChatChunk("", []gin.H{augmentLegacyThinkingNode(nextID, *thinking)}, nil, unknown, checkpointNotFound))
			}
			if len(choice.Delta.ToolCalls) > 0 {
				toolCallBuffer.absorb(choice.Delta.ToolCalls)
			}
			if nodes, invalidCount := toolCallBuffer.flushReady(&nextID, choice.FinishReason != nil); len(nodes) > 0 {
				pendingChunks = append(pendingChunks, augmentLegacyChatChunk("", nodes, nil, unknown, checkpointNotFound))
				if invalidCount > 0 {
					finalStopReason = augmentStopReasonEndTurn
					pendingChunks = append(pendingChunks, augmentLegacyChatChunk("Tool call failed: upstream returned incomplete tool arguments.", nil, nil, unknown, checkpointNotFound))
				}
			} else if invalidCount > 0 {
				finalStopReason = augmentStopReasonEndTurn
				pendingChunks = append(pendingChunks, augmentLegacyChatChunk("Tool call failed: upstream returned incomplete tool arguments.", nil, nil, unknown, checkpointNotFound))
			}
		}
		if streamChunk.Usage != nil {
			nextID++
			pendingChunks = append(pendingChunks, augmentLegacyChatChunk("", []gin.H{augmentLegacyTokenUsageNode(nextID, streamChunk.Usage)}, nil, unknown, checkpointNotFound))
		}
		for _, chunk := range pendingChunks {
			text, _ := chunk["text"].(string)
			nodes, _ := chunk["nodes"].([]gin.H)
			if err := emitWithMeta(text, nodes, nil); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if !streamStarted {
			c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{
				"message": "Upstream service temporarily unavailable, please retry.",
				"type":    "upstream_error",
			}})
			return
		}
		_ = emitWithMeta(err.Error(), nil, ptrInt(augmentStopReasonEndTurn))
		return
	}
	_ = emitWithMeta("", nil, &finalStopReason)
}

func (h *AuthHandler) augmentLegacySimpleTextPromptCompletion(
	ctx context.Context,
	c *gin.Context,
	principal *service.AugmentPluginPrincipal,
	model string,
	system string,
	user string,
) (*augmentLegacyLoopbackChatResult, error) {
	return h.augmentLegacySimpleTextPromptCompletionWithMode(ctx, c, principal, model, system, user, true)
}

func (h *AuthHandler) augmentLegacySimpleTextPromptCompletionWithMode(
	ctx context.Context,
	c *gin.Context,
	principal *service.AugmentPluginPrincipal,
	model string,
	system string,
	user string,
	allowGateway bool,
) (*augmentLegacyLoopbackChatResult, error) {
	if allowGateway && h.augmentLegacyGatewayReady() {
		return h.augmentLegacySimpleTextPromptCompletionThroughGateway(ctx, c, principal, model, system, user)
	}
	bearer, err := h.augmentLegacyGatewayBearer(ctx, principal)
	if err != nil {
		return nil, err
	}
	messages := make([]apicompat.ChatMessage, 0, 2)
	if strings.TrimSpace(system) != "" {
		messages = append(messages, augmentLegacyMakeMessage("system", system))
	}
	messages = append(messages, augmentLegacyMakeMessage("user", user))
	resolvedModel := h.augmentLegacyResolveModel(c, principal, model)
	reqBody, err := json.Marshal(map[string]any{
		"model":    resolvedModel,
		"messages": messages,
		"stream":   false,
	})
	if err != nil {
		return nil, err
	}
	return h.augmentLegacyLoopbackChatCompletion(ctx, c, bearer, "simple-text-prompt", reqBody, nil)
}

func (h *AuthHandler) augmentLegacySimpleTextPromptCompletionThroughGateway(
	ctx context.Context,
	c *gin.Context,
	principal *service.AugmentPluginPrincipal,
	model string,
	system string,
	user string,
) (*augmentLegacyLoopbackChatResult, error) {
	if h == nil || h.augmentGatewayService == nil || h.augmentGatewayService.Executor() == nil {
		return nil, fmt.Errorf("augment gateway executor is unavailable")
	}
	routed, err := h.augmentLegacyGatewayResolveModel(augmentLegacyChatRequest{Model: model})
	if err != nil {
		return nil, err
	}

	messages := make([]apicompat.ChatMessage, 0, 2)
	if strings.TrimSpace(system) != "" {
		messages = append(messages, augmentLegacyMakeMessage("system", system))
	}
	messages = append(messages, augmentLegacyMakeMessage("user", user))
	rawMessages := augmentLegacyChatMessagesToRawMaps(messages)
	req := service.AugmentGatewayProviderRequest{
		Endpoint:    strings.TrimSpace(c.FullPath()),
		RequestID:   strings.TrimSpace(c.GetHeader("X-Request-ID")),
		SessionHash: h.augmentLegacyNamespace(c, principal),
		Model:       routed.Model,
		APIKey:      principal.APIKey,
		User:        principal.User,
		UserAgent:   c.GetHeader("User-Agent"),
		IPAddress:   c.ClientIP(),
		Messages:    rawMessages,
		RawBody: map[string]any{
			"model":    routed.UpstreamModel,
			"messages": rawMessages,
			"stream":   false,
		},
	}
	result, err := h.augmentGatewayService.Executor().Complete(ctx, req)
	if err != nil {
		return nil, err
	}
	return &augmentLegacyLoopbackChatResult{Text: result.Text}, nil
}

func (h *AuthHandler) AugmentLegacyPromptEnhancer(c *gin.Context) {
	principal, ok := h.augmentPrincipalFromBearer(c)
	if !ok {
		return
	}
	var req augmentLegacyPromptEnhancerRequest
	if !augmentLegacyDecodeRequest(c, &req) {
		return
	}
	augmentLegacyNormalizePromptEnhancerRequest(&req)
	promptText := strings.TrimSpace(augmentLegacyExtractNodeText(req.Nodes))
	if promptText == "" {
		promptText = strings.TrimSpace(req.Message)
	}
	if !augmentLegacyEnsureNonEmptyInput(c, promptText) {
		return
	}
	contextBundle := augmentContextBundleFromPromptEnhancerRequest(req)
	if h.augmentPluginService != nil {
		resolved := h.augmentPluginService.ResolveLegacyBlobsForNamespace(h.augmentLegacyNamespace(c, principal), req.Blobs.CheckpointID, req.Blobs.AddedBlobs, req.Blobs.DeletedBlobs)
		contextBundle = contextBundle.withResolvedBlobs(resolved)
	}
	augmentLegacyTrace(c, "prompt_enhancer_request", contextBundle.TraceFields()...)
	user := augmentLegacyCompactTextParts(
		"Enhance the following prompt for use in an IDE assistant. Return only the improved prompt text.",
		promptText,
		contextBundle.Format(),
	)
	res, err := h.augmentLegacySimpleTextPromptCompletion(c.Request.Context(), c, principal, req.Model, "", user)
	if err != nil {
		if augmentLegacyGatewayUnavailable(err) {
			augmentLegacyWriteNDJSONStatus(c, http.StatusBadRequest, gin.H{
				"code":    augmentLegacyGatewayUnavailableCode(err),
				"message": err.Error(),
				"text":    err.Error(),
			})
			return
		}
		augmentLegacyWriteNDJSON(c, gin.H{"text": err.Error()})
		return
	}
	augmentLegacyWriteNDJSON(c, gin.H{"text": res.Text, "unknown_blob_names": []string{}, "checkpoint_not_found": false, "workspace_file_chunks": []any{}, "nodes": []any{}})
}

func (h *AuthHandler) AugmentLegacyInstructionStream(c *gin.Context) {
	principal, ok := h.augmentPrincipalFromBearer(c)
	if !ok {
		return
	}
	var req augmentLegacyInstructionRequest
	if !augmentLegacyDecodeRequest(c, &req) {
		return
	}
	augmentLegacyNormalizeInstructionRequest(&req)
	instructionText := augmentLegacyResolveInstructionText(req.Instruction, req.Nodes)
	if !augmentLegacyEnsureNonEmptyInput(c, instructionText) {
		return
	}
	user := augmentLegacyCompactTextParts(
		"Rewrite the selected code according to the instruction. Return only the replacement text.",
		"instruction:\n"+instructionText,
		func() string {
			if strings.TrimSpace(req.SelectedText) == "" {
				return ""
			}
			return "selected_text:\n" + req.SelectedText
		}(),
	)
	res, err := h.augmentLegacySimpleTextPromptCompletion(c.Request.Context(), c, principal, req.Model, "", user)
	if err != nil {
		if augmentLegacyGatewayUnavailable(err) {
			c.JSON(http.StatusBadRequest, gin.H{"code": augmentLegacyGatewayUnavailableCode(err), "message": err.Error()})
			return
		}
		augmentLegacyWriteNDJSON(c, gin.H{})
		return
	}
	augmentLegacyWriteNDJSON(c, gin.H{
		"text":                   res.Text,
		"replacement_text":       res.Text,
		"replacement_old_text":   req.SelectedText,
		"replacement_start_line": 1,
		"replacement_end_line":   1,
		"unknown_blob_names":     []string{},
		"checkpoint_not_found":   false,
	})
}

func (h *AuthHandler) AugmentLegacySmartPasteStream(c *gin.Context) {
	h.AugmentLegacyInstructionStream(c)
}

func (h *AuthHandler) AugmentLegacyGenerateCommitMessageStream(c *gin.Context) {
	principal, ok := h.augmentPrincipalFromBearer(c)
	if !ok {
		return
	}
	var req augmentLegacyCommitMessageRequest
	if !augmentLegacyDecodeRequest(c, &req) {
		return
	}
	user := augmentLegacyCompactTextParts(
		"Generate a concise git commit message for the following diff. Return only the commit message.",
		req.Diff,
		strings.Join(req.GeneratedCommitMessageSubrequest.RelevantCommitMessages, "\n"),
		strings.Join(req.GeneratedCommitMessageSubrequest.ExampleCommitMessages, "\n"),
	)
	res, err := h.augmentLegacySimpleTextPromptCompletionWithMode(c.Request.Context(), c, principal, "gpt-5.4", "", user, true)
	if err != nil {
		if augmentLegacyGatewayUnavailable(err) {
			c.JSON(http.StatusBadRequest, gin.H{"code": augmentLegacyGatewayUnavailableCode(err), "message": err.Error()})
			return
		}
		augmentLegacyWriteNDJSON(c, gin.H{"text": ""})
		return
	}
	augmentLegacyWriteNDJSON(c, gin.H{"text": res.Text})
}

func (h *AuthHandler) AugmentLegacyNextEditLocation(c *gin.Context) {
	if h.augmentPluginService == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "augment plugin service is unavailable"})
		return
	}
	if _, ok := h.augmentPrincipalFromBearer(c); !ok {
		return
	}
	var req augmentLegacyNextEditLocationRequest
	if !augmentLegacyDecodeRequest(c, &req) {
		return
	}
	if !augmentLegacyEnsureNonEmptyInput(c, augmentLegacyResolveInstructionText(req.Instruction, req.Nodes)) {
		return
	}

	candidateLocations := make([]gin.H, 0, 1)
	path := strings.TrimSpace(req.Path)
	if path != "" {
		candidateLocations = append(candidateLocations, gin.H{
			"item": gin.H{
				"path": path,
				"range": gin.H{
					"start": 1,
					"stop":  1,
				},
			},
			"score":      1,
			"debug_info": "heuristic",
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"candidate_locations":  candidateLocations,
		"unknown_blob_names":   []string{},
		"checkpoint_not_found": false,
		"critical_errors":      []string{},
	})
}

func (h *AuthHandler) AugmentLegacyNextEditStream(c *gin.Context) {
	if h.augmentPluginService == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "augment plugin service is unavailable"})
		return
	}
	principal, ok := h.augmentPrincipalFromBearer(c)
	if !ok {
		return
	}
	var req augmentLegacyNextEditStreamRequest
	if !augmentLegacyDecodeRequest(c, &req) {
		return
	}
	instructionText := augmentLegacyResolveInstructionText(req.Instruction, req.Nodes)
	if !augmentLegacyEnsureNonEmptyInput(c, instructionText) {
		return
	}

	resolved := h.augmentPluginService.ResolveLegacyBlobsForNamespace(h.augmentLegacyNamespace(c, principal), req.Blobs.CheckpointID, req.Blobs.AddedBlobs, req.Blobs.DeletedBlobs)
	existingCode := req.Prefix + req.SelectedText + req.Suffix
	if existingCode == "" && len(resolved.Records) > 0 {
		existingCode = resolved.Records[0].Content
	}

	user := augmentLegacyCompactTextParts(
		"Generate the full suggested code for the edit request. Return only the suggested replacement code.",
		"instruction:\n"+instructionText,
		func() string {
			if existingCode == "" {
				return ""
			}
			return "existing_code:\n" + existingCode
		}(),
	)
	res, err := h.augmentLegacySimpleTextPromptCompletionWithMode(c.Request.Context(), c, principal, req.Model, "", user, false)
	if err != nil {
		augmentLegacyWriteNDJSON(c, gin.H{
			"next_edit": gin.H{
				"suggestion_id":      "failed",
				"path":               strings.TrimSpace(req.Path),
				"blob_name":          strings.TrimSpace(req.Blobs.CheckpointID),
				"char_start":         0,
				"char_end":           0,
				"existing_code":      existingCode,
				"suggested_code":     existingCode,
				"editing_score":      0,
				"localization_score": 0,
			},
			"unknown_blob_names":   resolved.Unknown,
			"checkpoint_not_found": resolved.CheckpointNotFound,
		})
		return
	}

	blobName := strings.TrimSpace(req.BlobName)
	if blobName == "" && len(resolved.Records) > 0 {
		blobName = resolved.Records[0].BlobName
	}
	path := strings.TrimSpace(req.Path)
	if path == "" && len(resolved.Records) > 0 {
		path = resolved.Records[0].Path
	}

	augmentLegacyWriteNDJSON(c, gin.H{
		"next_edit": gin.H{
			"suggestion_id":           fmt.Sprintf("suggestion-%d", time.Now().UnixNano()),
			"path":                    path,
			"blob_name":               blobName,
			"char_start":              0,
			"char_end":                len(existingCode),
			"existing_code":           existingCode,
			"suggested_code":          res.Text,
			"editing_score":           1,
			"localization_score":      1,
			"editing_score_threshold": 1,
			"change_description":      "compat generated suggestion",
		},
		"unknown_blob_names":   resolved.Unknown,
		"checkpoint_not_found": resolved.CheckpointNotFound,
	})
}

func ptrInt(v int) *int {
	return &v
}
