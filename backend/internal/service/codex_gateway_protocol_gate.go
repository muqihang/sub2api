package service

import "strings"

type CodexGatewayProtocolReadyChecker interface {
	IsReady(modelID string) bool
}

type codexGatewayProtocolReadyAllowList struct {
	ready map[string]struct{}
}

var defaultCodexGatewayProtocolReadyChecker CodexGatewayProtocolReadyChecker = codexGatewayProtocolReadyAllowList{
	ready: map[string]struct{}{
		"deepseek-v4-pro":   {},
		"deepseek-v4-flash": {},
	},
}

func (c codexGatewayProtocolReadyAllowList) IsReady(modelID string) bool {
	_, ok := c.ready[strings.ToLower(strings.TrimSpace(modelID))]
	return ok
}
