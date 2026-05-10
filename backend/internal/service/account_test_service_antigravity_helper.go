package service

import "strings"

func resolveAntigravityTestModel(modelID string) string {
	if strings.TrimSpace(modelID) != "" {
		return modelID
	}
	return "claude-sonnet-4-6"
}
