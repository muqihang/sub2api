package handler

import (
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const (
	augmentContextBundleMaxChatTurns        = 6
	augmentContextBundleMaxTurnRunes        = 600
	augmentContextBundleMaxBlobReferences   = 8
	augmentContextBundleMaxBlobSnippetRunes = 240
)

type augmentContextBundleChatTurn struct {
	Request  string
	Response string
}

type augmentContextBundleBlobReference struct {
	BlobName string
	Path     string
	Snippet  string
}

type augmentContextBundle struct {
	ConversationID      string
	ChatHistoryCount    int
	ChatHistory         []augmentContextBundleChatTurn
	CheckpointID        string
	AddedBlobCount      int
	DeletedBlobCount    int
	AddedBlobNames      []string
	DeletedBlobNames    []string
	ActiveBlobCount     int
	ActiveBlobRefs      []augmentContextBundleBlobReference
	UnknownBlobCount    int
	UserGuidedBlobs     []string
	ExternalSourceIDs   []string
	UserGuidelines      string
	WorkspaceGuidelines string
	RulesText           string
	Path                string
	Lang                string
	SelectedText        string
	SelectedTextPresent bool
	DialogCount         int
	Workspace           service.AugmentGatewayWorkspaceMetadata
}

func augmentContextBundleFromChatRequest(req augmentLegacyChatRequest) augmentContextBundle {
	addedBlobNames := dedupeAugmentContextBundleStrings(req.Blobs.AddedBlobs)
	deletedBlobNames := dedupeAugmentContextBundleStrings(req.Blobs.DeletedBlobs)
	selectedText := augmentContextBundleCompactText(augmentLegacyFirstNonBlank(req.SelectedCode, req.SelectedText), augmentContextBundleMaxTurnRunes)
	return augmentContextBundle{
		ConversationID:      strings.TrimSpace(req.ConversationID),
		ChatHistoryCount:    len(req.ChatHistory),
		ChatHistory:         augmentContextBundleChatTurns(req.ChatHistory),
		CheckpointID:        strings.TrimSpace(req.Blobs.CheckpointID),
		AddedBlobCount:      len(addedBlobNames),
		DeletedBlobCount:    len(deletedBlobNames),
		AddedBlobNames:      addedBlobNames,
		DeletedBlobNames:    deletedBlobNames,
		UserGuidedBlobs:     dedupeAugmentContextBundleStrings(req.UserGuidedBlobs),
		ExternalSourceIDs:   dedupeAugmentContextBundleStrings(req.ExternalSourceIDs),
		UserGuidelines:      strings.TrimSpace(req.UserGuidelines),
		WorkspaceGuidelines: strings.TrimSpace(req.WorkspaceGuidelines),
		RulesText:           strings.TrimSpace(augmentLegacyJoinRuleTexts(req.Rules)),
		Path:                strings.TrimSpace(req.Path),
		Lang:                strings.TrimSpace(req.Lang),
		SelectedText:        selectedText,
		SelectedTextPresent: strings.TrimSpace(selectedText) != "",
		Workspace:           service.ResolveAugmentGatewayWorkspaceMetadata(),
	}
}

func augmentContextBundleFromPromptEnhancerRequest(req augmentLegacyPromptEnhancerRequest) augmentContextBundle {
	addedBlobNames := dedupeAugmentContextBundleStrings(req.Blobs.AddedBlobs)
	deletedBlobNames := dedupeAugmentContextBundleStrings(req.Blobs.DeletedBlobs)
	selectedText := augmentContextBundleCompactText(req.SelectedText, augmentContextBundleMaxTurnRunes)
	return augmentContextBundle{
		ConversationID:      strings.TrimSpace(req.ConversationID),
		ChatHistoryCount:    len(req.ChatHistory),
		ChatHistory:         augmentContextBundleChatTurns(req.ChatHistory),
		CheckpointID:        strings.TrimSpace(req.Blobs.CheckpointID),
		AddedBlobCount:      len(addedBlobNames),
		DeletedBlobCount:    len(deletedBlobNames),
		AddedBlobNames:      addedBlobNames,
		DeletedBlobNames:    deletedBlobNames,
		UserGuidedBlobs:     dedupeAugmentContextBundleStrings(req.UserGuidedBlobs),
		ExternalSourceIDs:   dedupeAugmentContextBundleStrings(req.ExternalSourceIDs),
		UserGuidelines:      strings.TrimSpace(req.UserGuidelines),
		WorkspaceGuidelines: strings.TrimSpace(req.WorkspaceGuidelines),
		RulesText:           strings.TrimSpace(augmentLegacyJoinRuleTexts(req.Rules)),
		Path:                strings.TrimSpace(req.Path),
		Lang:                strings.TrimSpace(req.Lang),
		SelectedText:        selectedText,
		SelectedTextPresent: strings.TrimSpace(selectedText) != "",
		Workspace:           service.ResolveAugmentGatewayWorkspaceMetadata(),
	}
}

func augmentContextBundleFromCodebaseRetrievalRequest(req augmentLegacyCodebaseRetrievalRequest) augmentContextBundle {
	addedBlobNames := dedupeAugmentContextBundleStrings(req.Blobs.AddedBlobs)
	deletedBlobNames := dedupeAugmentContextBundleStrings(req.Blobs.DeletedBlobs)
	return augmentContextBundle{
		CheckpointID:     strings.TrimSpace(req.Blobs.CheckpointID),
		AddedBlobCount:   len(addedBlobNames),
		DeletedBlobCount: len(deletedBlobNames),
		AddedBlobNames:   addedBlobNames,
		DeletedBlobNames: deletedBlobNames,
		DialogCount:      len(req.Dialog),
		Workspace:        service.ResolveAugmentGatewayWorkspaceMetadata(),
	}
}

func (b augmentContextBundle) withResolvedBlobs(resolved service.AugmentLegacyResolvedBlobs) augmentContextBundle {
	b.ActiveBlobCount = len(resolved.Records)
	b.ActiveBlobRefs = augmentContextBundleBlobReferences(resolved)
	b.UnknownBlobCount = len(resolved.Unknown)
	return b
}

func (b augmentContextBundle) HasContent() bool {
	return strings.TrimSpace(b.ConversationID) != "" ||
		b.ChatHistoryCount > 0 ||
		strings.TrimSpace(b.CheckpointID) != "" ||
		b.AddedBlobCount > 0 ||
		b.DeletedBlobCount > 0 ||
		b.ActiveBlobCount > 0 ||
		b.UnknownBlobCount > 0 ||
		len(b.UserGuidedBlobs) > 0 ||
		len(b.ExternalSourceIDs) > 0 ||
		strings.TrimSpace(b.UserGuidelines) != "" ||
		strings.TrimSpace(b.WorkspaceGuidelines) != "" ||
		strings.TrimSpace(b.RulesText) != "" ||
		strings.TrimSpace(b.Path) != "" ||
		strings.TrimSpace(b.Lang) != "" ||
		strings.TrimSpace(b.SelectedText) != "" ||
		b.DialogCount > 0 ||
		strings.TrimSpace(b.Workspace.WorkspaceRoot) != "" ||
		strings.TrimSpace(b.Workspace.Branch) != "" ||
		strings.TrimSpace(b.Workspace.Worktree) != ""
}

func (b augmentContextBundle) Format() string {
	lines := []string{"Augment context bundle:"}
	appendLine := func(key, value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			lines = append(lines, fmt.Sprintf("%s: %s", key, value))
		}
	}
	appendCount := func(key string, value int) {
		if value > 0 {
			lines = append(lines, fmt.Sprintf("%s: %d", key, value))
		}
	}

	appendLine("conversation_id", b.ConversationID)
	appendLine("workspace_root", b.Workspace.WorkspaceRoot)
	appendLine("branch", b.Workspace.Branch)
	appendLine("worktree", b.Workspace.Worktree)
	appendCount("chat_history_count", b.ChatHistoryCount)
	appendCount("dialog_count", b.DialogCount)
	appendLine("checkpoint_id", b.CheckpointID)
	appendCount("added_blobs_count", b.AddedBlobCount)
	appendCount("deleted_blobs_count", b.DeletedBlobCount)
	appendCount("active_blob_count", b.ActiveBlobCount)
	appendCount("unknown_blob_count", b.UnknownBlobCount)
	appendLine("added_blobs", strings.Join(b.AddedBlobNames, ", "))
	appendLine("deleted_blobs", strings.Join(b.DeletedBlobNames, ", "))
	appendLine("user_guided_blobs", strings.Join(b.UserGuidedBlobs, ", "))
	appendLine("external_source_ids", strings.Join(b.ExternalSourceIDs, ", "))
	appendLine("path", b.Path)
	appendLine("lang", b.Lang)
	appendLine("selected_text", b.SelectedText)
	if len(b.ChatHistory) > 0 {
		lines = append(lines, "chat_history:")
		for idx, turn := range b.ChatHistory {
			prefix := fmt.Sprintf("- turn %d", idx+1)
			if strings.TrimSpace(turn.Request) != "" {
				lines = append(lines, fmt.Sprintf("%s request: %s", prefix, turn.Request))
			}
			if strings.TrimSpace(turn.Response) != "" {
				lines = append(lines, fmt.Sprintf("%s response: %s", prefix, turn.Response))
			}
		}
	}
	if len(b.ActiveBlobRefs) > 0 {
		lines = append(lines, "active_blob_references:")
		for _, ref := range b.ActiveBlobRefs {
			label := strings.TrimSpace(ref.Path)
			if label == "" {
				label = strings.TrimSpace(ref.BlobName)
			}
			if strings.TrimSpace(ref.BlobName) != "" && strings.TrimSpace(ref.Path) != "" {
				label = fmt.Sprintf("%s (%s)", ref.Path, ref.BlobName)
			}
			if label != "" {
				lines = append(lines, "- "+label)
			}
			if strings.TrimSpace(ref.Snippet) != "" {
				lines = append(lines, "  snippet: "+ref.Snippet)
			}
		}
	}
	appendLine("user_guidelines", b.UserGuidelines)
	appendLine("workspace_guidelines", b.WorkspaceGuidelines)
	appendLine("rules", b.RulesText)

	if len(lines) == 1 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func (b augmentContextBundle) DeepSeekCacheStableFormat() string {
	lines := []string{"Augment stable workspace context:"}
	appendLine := func(key, value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			lines = append(lines, fmt.Sprintf("%s: %s", key, value))
		}
	}

	appendLine("workspace_root", b.Workspace.WorkspaceRoot)
	appendLine("branch", b.Workspace.Branch)
	appendLine("worktree", b.Workspace.Worktree)

	if len(lines) == 1 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func (b augmentContextBundle) RetrievalMetadata() string {
	lines := []string{"Augment context metadata:"}
	appendLine := func(key, value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			lines = append(lines, fmt.Sprintf("%s: %s", key, value))
		}
	}
	appendCount := func(key string, value int) {
		if value > 0 {
			lines = append(lines, fmt.Sprintf("%s: %d", key, value))
		}
	}

	appendLine("conversation_id", b.ConversationID)
	appendLine("workspace_root", b.Workspace.WorkspaceRoot)
	appendLine("branch", b.Workspace.Branch)
	appendLine("worktree", b.Workspace.Worktree)
	appendLine("checkpoint_id", b.CheckpointID)
	appendCount("dialog_count", b.DialogCount)
	appendCount("active_blob_count", b.ActiveBlobCount)
	appendCount("unknown_blob_count", b.UnknownBlobCount)
	appendCount("added_blobs_count", b.AddedBlobCount)
	appendCount("deleted_blobs_count", b.DeletedBlobCount)
	appendLine("added_blobs", strings.Join(b.AddedBlobNames, ", "))
	appendLine("deleted_blobs", strings.Join(b.DeletedBlobNames, ", "))
	if len(lines) == 1 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func (b augmentContextBundle) TraceFields() []any {
	return []any{
		"context_bundle_present", b.HasContent(),
		"context_conversation_id_present", strings.TrimSpace(b.ConversationID) != "",
		"context_chat_history_count", b.ChatHistoryCount,
		"context_chat_history_turns_included", len(b.ChatHistory),
		"context_dialog_count", b.DialogCount,
		"context_checkpoint_id_present", strings.TrimSpace(b.CheckpointID) != "",
		"context_added_blob_count", b.AddedBlobCount,
		"context_deleted_blob_count", b.DeletedBlobCount,
		"context_active_blob_count", b.ActiveBlobCount,
		"context_blob_reference_count", len(b.ActiveBlobRefs),
		"context_unknown_blob_count", b.UnknownBlobCount,
		"context_user_guided_blob_count", len(b.UserGuidedBlobs),
		"context_external_source_count", len(b.ExternalSourceIDs),
		"context_rules_present", strings.TrimSpace(b.RulesText) != "",
		"context_user_guidelines_present", strings.TrimSpace(b.UserGuidelines) != "",
		"context_workspace_guidelines_present", strings.TrimSpace(b.WorkspaceGuidelines) != "",
		"context_path_present", strings.TrimSpace(b.Path) != "",
		"context_lang_present", strings.TrimSpace(b.Lang) != "",
		"context_selected_text_present", b.SelectedTextPresent,
		"context_workspace_root_present", strings.TrimSpace(b.Workspace.WorkspaceRoot) != "",
		"context_workspace_root", strings.TrimSpace(b.Workspace.WorkspaceRoot),
		"context_git_branch", strings.TrimSpace(b.Workspace.Branch),
		"context_git_worktree", strings.TrimSpace(b.Workspace.Worktree),
	}
}

func augmentLegacyTextWithContextBundle(text string, bundle augmentContextBundle) string {
	return augmentLegacyCompactTextParts(text, bundle.Format())
}

func augmentLegacyTextWithDeepSeekCacheStableContextBundle(text string, bundle augmentContextBundle) string {
	return augmentLegacyCompactTextParts(text, bundle.DeepSeekCacheStableFormat())
}

func augmentLegacyAppendContextBundleRetrievalMetadata(text string, bundle augmentContextBundle, maxOutputLength int) string {
	metadata := strings.TrimSpace(bundle.RetrievalMetadata())
	if metadata == "" {
		return text
	}
	section := "\n\n" + metadata
	limit := maxOutputLength
	if limit <= 0 {
		limit = 20000
	}
	if limit > 20000 {
		limit = 20000
	}
	if len(text)+len(section) > limit {
		return text
	}
	return strings.TrimSpace(text + section)
}

func augmentLegacyContextBundleMetadata(bundle augmentContextBundle) map[string]any {
	metadata := map[string]any{
		"context_bundle_present":               bundle.HasContent(),
		"context_conversation_id_present":      strings.TrimSpace(bundle.ConversationID) != "",
		"context_chat_history_count":           bundle.ChatHistoryCount,
		"context_chat_history_turns_included":  len(bundle.ChatHistory),
		"context_dialog_count":                 bundle.DialogCount,
		"context_checkpoint_id_present":        strings.TrimSpace(bundle.CheckpointID) != "",
		"context_added_blob_count":             bundle.AddedBlobCount,
		"context_deleted_blob_count":           bundle.DeletedBlobCount,
		"context_active_blob_count":            bundle.ActiveBlobCount,
		"context_blob_reference_count":         len(bundle.ActiveBlobRefs),
		"context_unknown_blob_count":           bundle.UnknownBlobCount,
		"context_user_guided_blob_count":       len(bundle.UserGuidedBlobs),
		"context_external_source_count":        len(bundle.ExternalSourceIDs),
		"context_rules_present":                strings.TrimSpace(bundle.RulesText) != "",
		"context_user_guidelines_present":      strings.TrimSpace(bundle.UserGuidelines) != "",
		"context_workspace_guidelines_present": strings.TrimSpace(bundle.WorkspaceGuidelines) != "",
		"context_path_present":                 strings.TrimSpace(bundle.Path) != "",
		"context_lang_present":                 strings.TrimSpace(bundle.Lang) != "",
		"context_selected_text_present":        bundle.SelectedTextPresent,
		"context_workspace_root_present":       strings.TrimSpace(bundle.Workspace.WorkspaceRoot) != "",
		"context_workspace_root":               strings.TrimSpace(bundle.Workspace.WorkspaceRoot),
		"context_git_branch":                   strings.TrimSpace(bundle.Workspace.Branch),
		"context_git_worktree":                 strings.TrimSpace(bundle.Workspace.Worktree),
	}
	return metadata
}

func augmentContextBundleChatTurns(items []augmentLegacyChatHistoryItem) []augmentContextBundleChatTurn {
	out := make([]augmentContextBundleChatTurn, 0, min(len(items), augmentContextBundleMaxChatTurns))
	for _, item := range items {
		request := augmentContextBundleCompactText(augmentLegacyHistoryRequestText(item), augmentContextBundleMaxTurnRunes)
		response := augmentContextBundleCompactText(item.ResponseText, augmentContextBundleMaxTurnRunes)
		if request == "" && response == "" {
			continue
		}
		out = append(out, augmentContextBundleChatTurn{
			Request:  request,
			Response: response,
		})
		if len(out) >= augmentContextBundleMaxChatTurns {
			break
		}
	}
	return out
}

func augmentContextBundleBlobReferences(resolved service.AugmentLegacyResolvedBlobs) []augmentContextBundleBlobReference {
	out := make([]augmentContextBundleBlobReference, 0, min(len(resolved.Records), augmentContextBundleMaxBlobReferences))
	for _, record := range resolved.Records {
		blobName := strings.TrimSpace(record.BlobName)
		path := strings.TrimSpace(record.Path)
		if blobName == "" && path == "" {
			continue
		}
		out = append(out, augmentContextBundleBlobReference{
			BlobName: blobName,
			Path:     path,
			Snippet:  augmentContextBundleCompactText(record.Content, augmentContextBundleMaxBlobSnippetRunes),
		})
		if len(out) >= augmentContextBundleMaxBlobReferences {
			break
		}
	}
	return out
}

func augmentContextBundleCompactText(value string, maxRunes int) string {
	value = strings.Join(strings.Fields(value), " ")
	if value == "" || maxRunes <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "..."
}

func dedupeAugmentContextBundleStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
