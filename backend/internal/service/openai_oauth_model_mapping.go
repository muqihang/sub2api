package service

import "strings"

const OpenAIOAuthDefaultTestModel = "gpt-5.5"

func ensureOpenAIOAuthModelMappingIncludesDefault(credentials map[string]any) map[string]any {
	if len(credentials) == 0 {
		return credentials
	}
	rawMapping, ok := credentials["model_mapping"].(map[string]any)
	if !ok || len(rawMapping) == 0 {
		return credentials
	}
	if _, exists := rawMapping[OpenAIOAuthDefaultTestModel]; exists {
		return credentials
	}

	updated := cloneJSONMap(credentials)
	updatedMapping := cloneJSONMap(rawMapping)
	updatedMapping[OpenAIOAuthDefaultTestModel] = strings.TrimSpace(OpenAIOAuthDefaultTestModel)
	updated["model_mapping"] = updatedMapping
	return updated
}
