package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
)

func codexGatewayWorkspaceKey(headers http.Header) string {
	if workspaceKey := codexGatewayWorkspaceKeyFromTurnMetadata(firstHeaderValue(headers, "X-Codex-Turn-Metadata")); workspaceKey != "" {
		return workspaceKey
	}
	if threadID := firstCaptureNonEmpty(firstHeaderValue(headers, "Thread_id"), firstHeaderValue(headers, "Conversation_id")); threadID != "" {
		return codexGatewayScopeDigest("thread", threadID)
	}
	return ""
}

func codexGatewayManagedSessionBucket(headers http.Header) string {
	if firstHeaderValue(headers, "X-Codex-Parent-Thread-Id") == "" && firstHeaderValue(headers, "X-OpenAI-Subagent") == "" {
		return ""
	}
	if sessionBucket := codexGatewayManagedSessionBucketFromTurnMetadata(firstHeaderValue(headers, "X-Codex-Turn-Metadata")); sessionBucket != "" {
		return sessionBucket
	}
	if sessionID := firstHeaderValue(headers, "Session_id"); sessionID != "" {
		return codexGatewayScopeDigest("session", sessionID)
	}
	return ""
}

func codexGatewayWorkspaceKeyFromTurnMetadata(raw string) string {
	metadata := codexGatewayTurnMetadata(raw)
	if len(metadata) == 0 {
		return ""
	}
	workspaces, _ := metadata["workspaces"].(map[string]any)
	if len(workspaces) == 0 {
		return ""
	}
	paths := make([]string, 0, len(workspaces))
	for path := range workspaces {
		path = normalizeCodexGatewayWorkspaceScopePath(path)
		if path != "" {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return codexGatewayScopeDigest("workspaces", paths...)
}

func codexGatewayManagedSessionBucketFromTurnMetadata(raw string) string {
	metadata := codexGatewayTurnMetadata(raw)
	if len(metadata) == 0 {
		return ""
	}
	if value, _ := metadata["session_id"].(string); strings.TrimSpace(value) != "" {
		return codexGatewayScopeDigest("session", value)
	}
	return ""
}

func codexGatewayTurnMetadata(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return nil
	}
	return metadata
}

func normalizeCodexGatewayWorkspaceScopePath(path string) string {
	path = strings.TrimSpace(path)
	for len(path) > 1 && strings.HasSuffix(path, "/") {
		path = strings.TrimSuffix(path, "/")
	}
	return path
}

func codexGatewayScopeDigest(prefix string, values ...string) string {
	parts := make([]string, 0, len(values)+1)
	parts = append(parts, strings.TrimSpace(prefix))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			parts = append(parts, value)
		}
	}
	if len(parts) == 1 {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:16])
}
