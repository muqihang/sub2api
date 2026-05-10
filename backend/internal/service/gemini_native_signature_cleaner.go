package service

import (
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
)

type GeminiNativeThoughtSignatureCleanResult struct {
	Body          []byte
	ReplacedCount int
}

// CleanGeminiNativeThoughtSignatures 从 Gemini 原生 API 请求中替换 thoughtSignature 字段为 dummy 签名，
// 以避免跨账号签名验证错误。
//
// 当粘性会话切换账号时（例如原账号异常、不可调度等），旧账号返回的 thoughtSignature
// 会导致新账号的签名验证失败。通过替换为 dummy 签名，跳过签名验证。
//
// CleanGeminiNativeThoughtSignatures replaces thoughtSignature fields with dummy signature
// in Gemini native API requests to avoid cross-account signature validation errors.
//
// When sticky session switches accounts (e.g., original account becomes unavailable),
// thoughtSignatures from the old account will cause validation failures on the new account.
// By replacing with dummy signature, we skip signature validation.
func CleanGeminiNativeThoughtSignatures(body []byte) []byte {
	return CleanGeminiNativeThoughtSignaturesDetailed(body).Body
}

func CleanGeminiNativeThoughtSignaturesDetailed(body []byte) GeminiNativeThoughtSignatureCleanResult {
	if len(body) == 0 {
		return GeminiNativeThoughtSignatureCleanResult{Body: body}
	}

	// 解析 JSON
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		// 如果解析失败，返回原始 body（可能不是 JSON 或格式不正确）
		return GeminiNativeThoughtSignatureCleanResult{Body: body}
	}

	// 仅替换 Gemini contents[*].parts[*].thoughtSignature，避免误伤任意同名业务字段。
	replaced, replacedCount := replaceThoughtSignaturesInGeminiContents(data)

	// 重新序列化
	result, err := json.Marshal(replaced)
	if err != nil {
		// 如果序列化失败，返回原始 body
		return GeminiNativeThoughtSignatureCleanResult{Body: body}
	}

	return GeminiNativeThoughtSignatureCleanResult{
		Body:          result,
		ReplacedCount: replacedCount,
	}
}

func replaceThoughtSignaturesInGeminiContents(root map[string]any) (map[string]any, int) {
	if root == nil {
		return nil, 0
	}
	contents, ok := root["contents"].([]any)
	if !ok {
		return root, 0
	}
	replacedCount := 0
	for i, content := range contents {
		contentMap, ok := content.(map[string]any)
		if !ok {
			continue
		}
		parts, ok := contentMap["parts"].([]any)
		if !ok {
			continue
		}
		for j, part := range parts {
			partMap, ok := part.(map[string]any)
			if !ok {
				continue
			}
			if _, exists := partMap["thoughtSignature"]; exists {
				partMap["thoughtSignature"] = antigravity.DummyThoughtSignature
				replacedCount++
			}
			parts[j] = partMap
		}
		contentMap["parts"] = parts
		contents[i] = contentMap
	}
	root["contents"] = contents
	return root, replacedCount
}
