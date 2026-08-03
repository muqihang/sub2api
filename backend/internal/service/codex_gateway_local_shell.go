package service

import (
	"encoding/json"
	"strings"
)

func codexGatewayBuildLocalShellActionFromArguments(arguments string) CodexGatewayLocalShellAction {
	cmd := strings.TrimSpace(codexGatewayExtractShellExecCmd(arguments))
	if cmd == "" {
		return CodexGatewayLocalShellAction{
			Type:    "exec",
			Command: []string{},
		}
	}
	return CodexGatewayLocalShellAction{
		Type:    "exec",
		Command: []string{"zsh", "-lc", cmd},
	}
}

func codexGatewayExtractShellExecCmd(arguments string) string {
	raw := strings.TrimSpace(arguments)
	if raw == "" {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(firstCodexGatewayToolString(payload["cmd"], payload["command"]))
}

func codexGatewayExtractShellArgumentsFromItem(item map[string]any) string {
	if item == nil {
		return "{}"
	}
	if raw := strings.TrimSpace(normalizeCodexGatewayToolArguments(item["arguments"])); raw != "" && raw != "\"\"" && raw != "{}" {
		return raw
	}
	action, ok := item["action"].(map[string]any)
	if !ok {
		return "{}"
	}
	command, ok := action["command"].([]any)
	if !ok || len(command) == 0 {
		return "{}"
	}
	cmd := codexGatewayShellCommandArrayToString(command)
	if strings.TrimSpace(cmd) == "" {
		return "{}"
	}
	encoded, err := json.Marshal(map[string]string{"cmd": cmd})
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func codexGatewayShellCommandArrayToString(command []any) string {
	if len(command) == 0 {
		return ""
	}
	parts := make([]string, 0, len(command))
	for _, part := range command {
		s, ok := part.(string)
		if !ok {
			continue
		}
		parts = append(parts, s)
	}
	if len(parts) >= 3 && parts[0] == "zsh" && parts[1] == "-lc" {
		return parts[2]
	}
	return strings.Join(parts, " ")
}

func codexGatewayApplyLocalShellCallItemFields(item map[string]any, callID, status, arguments string) {
	if item == nil {
		return
	}
	item["type"] = CodexGatewayOutputItemTypeLocalShellCall
	item["call_id"] = callID
	item["status"] = status
	delete(item, "name")
	delete(item, "namespace")
	delete(item, "arguments")
	item["action"] = codexGatewayBuildLocalShellActionFromArguments(arguments)
}
