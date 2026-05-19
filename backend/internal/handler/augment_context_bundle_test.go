package handler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAugmentContextBundleFromChatRequestAssemblesStructuredSources(t *testing.T) {
	workspaceRoot := prepareAugmentContextBundleGitWorkspace(t)
	t.Setenv("AUGMENT_LEGACY_WORKSPACE_ROOT", workspaceRoot)

	req := augmentLegacyChatRequest{
		ConversationID: "conv-chat-context",
		ChatHistory: []augmentLegacyChatHistoryItem{{
			RequestMessage: "Where is the gateway route?",
			ResponseText:   "It is registered in gateway.go.",
		}},
		Blobs: augmentLegacyCheckpointBlobsPayload{
			CheckpointID: "checkpoint-chat",
			AddedBlobs:   []string{"blob-a", "blob-a"},
			DeletedBlobs: []string{"blob-old"},
		},
		UserGuidedBlobs:     []string{"blob-a"},
		ExternalSourceIDs:   []string{"ticket-456"},
		UserGuidelines:      "answer in Chinese",
		WorkspaceGuidelines: "cite paths",
		Rules:               []any{map[string]any{"text": "do not invent files"}},
		Path:                "backend/internal/handler/auth_augment_runtime.go",
		Lang:                "go",
		SelectedCode:        "func AugmentLegacyChat() {}",
	}

	bundle := augmentContextBundleFromChatRequest(req)
	formatted := bundle.Format()
	metadata := augmentLegacyContextBundleMetadata(bundle)

	require.Contains(t, formatted, "conversation_id: conv-chat-context")
	require.Contains(t, formatted, "workspace_root: "+workspaceRoot)
	require.Contains(t, formatted, "branch: feature/context-bundle")
	require.Contains(t, formatted, "worktree: "+workspaceRoot)
	require.Contains(t, formatted, "chat_history_count: 1")
	require.Contains(t, formatted, "turn 1 request: Where is the gateway route?")
	require.Contains(t, formatted, "turn 1 response: It is registered in gateway.go.")
	require.Contains(t, formatted, "checkpoint_id: checkpoint-chat")
	require.Contains(t, formatted, "added_blobs: blob-a")
	require.Contains(t, formatted, "deleted_blobs: blob-old")
	require.Contains(t, formatted, "user_guided_blobs: blob-a")
	require.Contains(t, formatted, "external_source_ids: ticket-456")
	require.Contains(t, formatted, "path: backend/internal/handler/auth_augment_runtime.go")
	require.Contains(t, formatted, "lang: go")
	require.Contains(t, formatted, "selected_text: func AugmentLegacyChat() {}")
	require.Contains(t, formatted, "rules: do not invent files")
	require.Equal(t, true, metadata["context_bundle_present"])
	require.Equal(t, 1, metadata["context_chat_history_turns_included"])
	require.Equal(t, 1, metadata["context_added_blob_count"])
	require.Equal(t, true, metadata["context_selected_text_present"])
}

func TestAugmentGatewayPromptEnhancerIncludesContextBundle(t *testing.T) {
	workspaceRoot := prepareAugmentContextBundleGitWorkspace(t)
	t.Setenv("AUGMENT_LEGACY_WORKSPACE_ROOT", workspaceRoot)

	executor := &augmentGatewayRouteFakeExecutor{
		completeResult: service.AugmentGatewayProviderResult{Text: "enhanced"},
	}
	server, apiKey, _ := newAugmentGatewayRuntimeTestServer(t, executor)
	defer server.Close()

	resp := postAugmentGatewayRuntimeJSON(t, server, apiKey, "/prompt-enhancer", `{
		"model":"gpt-5.4",
		"conversation_id":"conv-enhancer-context",
		"nodes":[{"id":1,"type":0,"text_node":{"content":"make retrieval prompt clearer"}}],
		"chat_history":[{"request_message":"Find Augment Gateway entrypoints","response_text":"Looked at auth_augment_runtime.go"}],
		"blobs":{"checkpoint_id":"checkpoint-context","added_blobs":["blob-a"],"deleted_blobs":["blob-old"]},
		"user_guided_blobs":["blob-a"],
			"external_source_ids":["ticket-123"],
			"user_guidelines":"answer in Chinese",
			"workspace_guidelines":"cite file paths",
			"selected_text":"func selectedContext() {}",
			"path":"backend/internal/handler/auth_augment_runtime.go",
			"lang":"go",
			"rules":[{"text":"do not invent evidence"}]
		}`)
	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)

	chunks := decodeAugmentContractNDJSON(t, resp.Body)
	require.Len(t, chunks, 1)
	require.Equal(t, "enhanced", chunks[0]["text"])
	require.Equal(t, []any{}, chunks[0]["workspace_file_chunks"])
	require.Equal(t, []any{}, chunks[0]["nodes"])
	require.NotContains(t, chunks[0], "incorporated_external_sources")

	calls := executor.CompleteRequests()
	require.Len(t, calls, 1)
	bodyText := augmentContextBundleProviderBodyText(t, calls[0].RawBody)
	require.Contains(t, bodyText, "Augment context bundle")
	require.Contains(t, bodyText, "conversation_id: conv-enhancer-context")
	require.Contains(t, bodyText, "workspace_root: "+workspaceRoot)
	require.Contains(t, bodyText, "branch: feature/context-bundle")
	require.Contains(t, bodyText, "worktree: "+workspaceRoot)
	require.Contains(t, bodyText, "chat_history_count: 1")
	require.Contains(t, bodyText, "chat_history:")
	require.Contains(t, bodyText, "turn 1 request: Find Augment Gateway entrypoints")
	require.Contains(t, bodyText, "turn 1 response: Looked at auth_augment_runtime.go")
	require.Contains(t, bodyText, "checkpoint_id: checkpoint-context")
	require.Contains(t, bodyText, "added_blobs: blob-a")
	require.Contains(t, bodyText, "active_blob_count: 1")
	require.Contains(t, bodyText, "active_blob_references:")
	require.Contains(t, bodyText, "src/main.go (blob-a)")
	require.Contains(t, bodyText, "snippet: package main func main(){}")
	require.Contains(t, bodyText, "user_guided_blobs: blob-a")
	require.Contains(t, bodyText, "external_source_ids: ticket-123")
	require.Contains(t, bodyText, "path: backend/internal/handler/auth_augment_runtime.go")
	require.Contains(t, bodyText, "lang: go")
	require.Contains(t, bodyText, "selected_text: func selectedContext() {}")
	require.Contains(t, bodyText, "rules: do not invent evidence")
	require.Contains(t, bodyText, "answer in Chinese")
	require.Contains(t, bodyText, "cite file paths")
}

func TestAugmentGatewayCodebaseRetrievalGuidanceIncludesWorkspaceContext(t *testing.T) {
	workspaceRoot := prepareAugmentContextBundleGitWorkspace(t)
	t.Setenv("AUGMENT_LEGACY_WORKSPACE_ROOT", workspaceRoot)

	executor := &augmentGatewayRouteFakeExecutor{
		streamChunks: []service.AugmentGatewayProviderChunk{{TextDelta: "ok", Done: true, ProviderFinishReason: "stop"}},
	}
	server, apiKey, _ := newAugmentGatewayRuntimeTestServer(t, executor)
	defer server.Close()

	resp := postAugmentGatewayRuntimeJSON(t, server, apiKey, "/chat-stream", `{
		"model":"gpt-5.4",
		"message":"find the Augment Gateway /chat-stream path",
		"conversation_id":"conv-codebase-context",
		"chat_history":[{"request_message":"Earlier retrieval task","response_text":"Earlier answer"}],
		"blobs":{"checkpoint_id":"checkpoint-codebase","added_blobs":["blob-a"],"deleted_blobs":[]},
		"tool_definitions":[{"name":"codebase-retrieval","description":"repo search","input_schema":{"type":"object"}}]
	}`)
	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)
	require.NotEmpty(t, decodeAugmentContractNDJSON(t, resp.Body))

	calls := executor.StreamRequests()
	require.Len(t, calls, 1)
	bodyText := augmentContextBundleProviderBodyText(t, calls[0].RawBody)
	require.Contains(t, bodyText, "Augment stable workspace context")
	require.Contains(t, bodyText, "workspace_root: "+workspaceRoot)
	require.Contains(t, bodyText, "branch: feature/context-bundle")
	require.Contains(t, bodyText, "worktree: "+workspaceRoot)
	require.Contains(t, bodyText, "Earlier retrieval task")
	require.Contains(t, bodyText, "Earlier answer")
	require.Contains(t, bodyText, "codebase-retrieval")
	require.NotContains(t, bodyText, "conversation_id: conv-codebase-context")
	require.NotContains(t, bodyText, "checkpoint_id: checkpoint-codebase")
	require.NotContains(t, bodyText, "added_blobs: blob-a")
	require.NotContains(t, bodyText, "chat_history_count: 1")
}

func TestAugmentContextBundleFromChatRequestPrefersIDEStateWorkspaceMetadata(t *testing.T) {
	fallbackWorkspace := t.TempDir()
	terminalWorkspace := prepareAugmentContextBundleGitWorkspace(t)
	t.Setenv("AUGMENT_LEGACY_WORKSPACE_ROOT", fallbackWorkspace)

	req := augmentLegacyChatRequest{
		RequestNodes: []augmentLegacyChatNode{{
			ID:   1,
			Type: intPtr(4),
			IDEStateNode: &augmentLegacyIDEStateNode{
				WorkspaceFolders: []augmentLegacyIDEWorkspaceFolder{{
					FolderRoot:     fallbackWorkspace,
					RepositoryRoot: fallbackWorkspace,
				}},
				CurrentTerminal: &augmentLegacyIDECurrentTerminal{
					CurrentWorkingDirectory: terminalWorkspace,
				},
			},
		}},
	}

	bundle := augmentContextBundleFromChatRequest(req)
	formatted := bundle.Format()

	require.Equal(t, terminalWorkspace, bundle.Workspace.WorkspaceRoot)
	require.Equal(t, "feature/context-bundle", bundle.Workspace.Branch)
	require.Equal(t, terminalWorkspace, bundle.Workspace.Worktree)
	require.Equal(t, []string{fallbackWorkspace}, bundle.WorkspaceFolders)
	require.Equal(t, terminalWorkspace, bundle.CurrentTerminalCWD)
	require.True(t, bundle.FileToolWorkspaceMismatch)
	require.Contains(t, formatted, "workspace_root: "+terminalWorkspace)
	require.Contains(t, formatted, "workspace_folders: "+fallbackWorkspace)
	require.Contains(t, formatted, "current_terminal_cwd: "+terminalWorkspace)
	require.Contains(t, formatted, "file_tool_workspace_mismatch: true")
}

func TestAugmentGatewayAddsWorkspaceFileToolPolicyForWorktreeMismatch(t *testing.T) {
	fallbackWorkspace := t.TempDir()
	terminalWorkspace := prepareAugmentContextBundleGitWorkspace(t)
	t.Setenv("AUGMENT_LEGACY_WORKSPACE_ROOT", fallbackWorkspace)

	executor := &augmentGatewayRouteFakeExecutor{
		streamChunks: []service.AugmentGatewayProviderChunk{{TextDelta: "ok", Done: true, ProviderFinishReason: "stop"}},
	}
	server, apiKey, _ := newAugmentGatewayRuntimeTestServer(t, executor)
	defer server.Close()

	resp := postAugmentGatewayRuntimeJSON(t, server, apiKey, "/chat-stream", `{
		"model":"deepseek-v4-pro",
		"message":"把方案写到目标 worktree 里",
		"tool_definitions":[
			{"name":"read-file","description":"read file","input_schema":{"type":"object"}},
			{"name":"save-file","description":"save file","input_schema":{"type":"object"}},
			{"name":"edit","description":"edit file","input_schema":{"type":"object"}}
		],
		"request_nodes":[
			{
				"id":1,
				"type":4,
				"ide_state_node":{
					"workspace_folders":[{"folder_root":"`+fallbackWorkspace+`","repository_root":"`+fallbackWorkspace+`"}],
					"current_terminal":{"terminal_id":0,"current_working_directory":"`+terminalWorkspace+`"}
				}
			}
		]
	}`)
	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)
	require.NotEmpty(t, decodeAugmentContractNDJSON(t, resp.Body))

	calls := executor.StreamRequests()
	require.Len(t, calls, 1)
	bodyText := augmentContextBundleProviderBodyText(t, calls[0].RawBody)
	require.Contains(t, bodyText, "workspace file access")
	require.Contains(t, bodyText, "workspace_folders: "+fallbackWorkspace)
	require.Contains(t, bodyText, "current_terminal_cwd: "+terminalWorkspace)
	require.Contains(t, bodyText, "outside the open workspace folders")
	require.Equal(t, terminalWorkspace, calls[0].Metadata["context_workspace_root"])
}

func prepareAugmentContextBundleGitWorkspace(t *testing.T) string {
	t.Helper()
	workspaceRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceRoot, ".git", "refs", "heads", "feature"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, ".git", "HEAD"), []byte("ref: refs/heads/feature/context-bundle\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, ".git", "refs", "heads", "feature", "context-bundle"), []byte("0123456789012345678901234567890123456789\n"), 0o644))
	return workspaceRoot
}

func augmentContextBundleProviderBodyText(t *testing.T, body map[string]any) string {
	t.Helper()
	messages, ok := body["messages"].([]any)
	require.True(t, ok)
	var parts []string
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		parts = append(parts, stringValueOrRawJSON(t, msg["content"]))
	}
	return strings.Join(parts, "\n\n")
}

func intPtr(v int) *int {
	return &v
}
