package service

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
)

func (m *CodexGatewayCaptureManager) summarizeHeaders(headers http.Header) map[string]any {
	out := map[string]any{
		"headers": m.redact.RedactHeaders(codexGatewayCaptureNonCodexHeaders(headers)),
	}
	context := m.codexHeaderContext(headers)
	if len(context) > 0 {
		out["codex_context"] = context
	}
	return out
}

func codexGatewayCaptureNonCodexHeaders(headers http.Header) http.Header {
	out := http.Header{}
	for key, values := range headers {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "session_id", "thread_id", "conversation_id", "x-client-request-id", "x-codex-window-id", "x-codex-parent-thread-id", "x-codex-turn-metadata":
			continue
		default:
			out[key] = values
		}
	}
	return out
}

func (m *CodexGatewayCaptureManager) codexHeaderContext(headers http.Header) map[string]any {
	if headers == nil || m == nil || m.redact == nil {
		return nil
	}
	out := map[string]any{}
	addHash := func(name, kind, value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			out[name] = m.redact.CorrelationHash(kind, value)
		}
	}
	addHash("session_id_hash", "session_id", firstHeaderValue(headers, "Session_id"))
	addHash("thread_id_hash", "thread_id", firstCaptureNonEmpty(firstHeaderValue(headers, "Thread_id"), firstHeaderValue(headers, "Conversation_id")))
	addHash("x_client_request_id_hash", "x_client_request_id", firstHeaderValue(headers, "X-Client-Request-Id"))
	addHash("window_id_hash", "window_id", firstHeaderValue(headers, "X-Codex-Window-Id"))
	addHash("parent_thread_id_hash", "parent_thread_id", firstHeaderValue(headers, "X-Codex-Parent-Thread-Id"))
	if subagent := strings.TrimSpace(firstHeaderValue(headers, "X-OpenAI-Subagent")); subagent != "" {
		out["subagent_kind"] = subagent
	}
	if metadata := strings.TrimSpace(firstHeaderValue(headers, "X-Codex-Turn-Metadata")); metadata != "" {
		m.mergeCodexTurnMetadata(out, metadata)
	}
	return out
}

func (m *CodexGatewayCaptureManager) mergeCodexTurnMetadata(out map[string]any, raw string) {
	var metadata map[string]any
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		out["turn_metadata_hash"] = m.redact.HashText(raw)
		return
	}
	if value, _ := metadata["session_id"].(string); strings.TrimSpace(value) != "" {
		out["session_id_hash"] = m.redact.CorrelationHash("session_id", value)
	}
	if value, _ := metadata["thread_id"].(string); strings.TrimSpace(value) != "" {
		out["thread_id_hash"] = m.redact.CorrelationHash("thread_id", value)
	}
	if value, _ := metadata["turn_id"].(string); strings.TrimSpace(value) != "" {
		out["turn_id_hash"] = m.redact.CorrelationHash("turn_id", value)
	}
	if value, _ := metadata["thread_source"].(string); strings.TrimSpace(value) != "" {
		out["thread_source"] = strings.TrimSpace(value)
	}
	if value, _ := metadata["sandbox"].(string); strings.TrimSpace(value) != "" {
		out["sandbox"] = strings.TrimSpace(value)
	}
	if workspaces, ok := metadata["workspaces"].(map[string]any); ok {
		out["workspaces"] = m.summarizeWorkspaces(workspaces)
	}
}

func (m *CodexGatewayCaptureManager) summarizeWorkspaces(workspaces map[string]any) []map[string]any {
	paths := make([]string, 0, len(workspaces))
	for path := range workspaces {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	out := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		item := map[string]any{
			"path_hash":     m.redact.CorrelationHash("workspace_path", path),
			"path_depth":    codexGatewayCapturePathDepth(path),
			"basename_hash": m.redact.CorrelationHash("workspace_basename", filepath.Base(path)),
		}
		if obj, ok := workspaces[path].(map[string]any); ok {
			if hasChanges, ok := obj["has_changes"].(bool); ok {
				item["has_changes"] = hasChanges
			}
			if commit, _ := obj["latest_git_commit_hash"].(string); strings.TrimSpace(commit) != "" {
				item["latest_git_commit_hash_hmac"] = m.redact.CorrelationHash("git_commit", commit)
			}
			if branch, _ := obj["branch"].(string); strings.TrimSpace(branch) != "" {
				item["branch_name_hash"] = m.redact.CorrelationHash("git_branch", branch)
			}
			if remotes, ok := obj["associated_remote_urls"].(map[string]any); ok {
				item["remote_url_hashes"] = m.remoteURLHashes(remotes)
			}
		}
		out = append(out, item)
	}
	return out
}

func (m *CodexGatewayCaptureManager) remoteURLHashes(remotes map[string]any) []string {
	keys := make([]string, 0, len(remotes))
	for key := range remotes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	hashes := make([]string, 0, len(keys))
	for _, key := range keys {
		if value, ok := remotes[key].(string); ok && strings.TrimSpace(value) != "" {
			hashes = append(hashes, m.redact.CorrelationHash("remote_url", value))
		}
	}
	return hashes
}

func firstHeaderValue(headers http.Header, key string) string {
	if headers == nil {
		return ""
	}
	return strings.TrimSpace(headers.Get(key))
}

func firstCaptureNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func codexGatewayCapturePathDepth(path string) int {
	path = strings.Trim(path, "/")
	if path == "" {
		return 0
	}
	return len(strings.Split(path, "/"))
}
