package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func validClaudeCodeValidatorBody(metadataUserID string, prompt string) map[string]any {
	body := map[string]any{
		"model": "claude-sonnet-4-20250514",
	}
	if prompt != "" {
		body["system"] = []any{map[string]any{"type": "text", "text": prompt}}
	}
	if metadataUserID != "" {
		body["metadata"] = map[string]any{"user_id": metadataUserID}
	}
	return body
}

func validClaudeCodeJSONMetadataUserID() string {
	return `{"device_id":"a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2","account_uuid":"550e8400-e29b-41d4-a716-446655440000","session_id":"123e4567-e89b-12d3-a456-426614174000"}`
}

func TestClaudeCodeValidator_MessagesAcceptsCurrentCLIJSONMetadataAndPrompt(t *testing.T) {
	validator := NewClaudeCodeValidator()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
	req.Header.Set("User-Agent", "claude-cli/2.1.161 (external, sdk-cli)")

	ok := validator.Validate(req, validClaudeCodeValidatorBody(
		validClaudeCodeJSONMetadataUserID(),
		"You are Claude Code, Anthropic's official CLI for Claude.",
	))
	require.True(t, ok)
}

func TestClaudeCodeValidator_MessagesAcceptsOfficialClaudeCodeUAFamily(t *testing.T) {
	validator := NewClaudeCodeValidator()

	tests := []struct {
		name string
		ua   string
		body map[string]any
	}{
		{
			name: "claude-code ua with json metadata and system prompt",
			ua:   "claude-code/2.1.161 (darwin; arm64)",
			body: validClaudeCodeValidatorBody(validClaudeCodeJSONMetadataUserID(), "You are Claude Code, Anthropic's official CLI for Claude."),
		},
		{
			name: "Claude Code ua with json metadata and system prompt",
			ua:   "Claude Code/2.1.161",
			body: validClaudeCodeValidatorBody(validClaudeCodeJSONMetadataUserID(), "You are Claude Code, Anthropic's official CLI for Claude."),
		},
		{
			name: "ClaudeCode ua with json metadata and system prompt",
			ua:   "ClaudeCode/2.1.161",
			body: validClaudeCodeValidatorBody(validClaudeCodeJSONMetadataUserID(), "You are Claude Code, Anthropic's official CLI for Claude."),
		},
		{
			name: "claude-cli without version with json metadata and system prompt",
			ua:   "claude-cli",
			body: validClaudeCodeValidatorBody(validClaudeCodeJSONMetadataUserID(), "You are Claude Code, Anthropic's official CLI for Claude."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
			req.Header.Set("User-Agent", tt.ua)
			require.True(t, validator.Validate(req, tt.body), "UA: %q", tt.ua)
		})
	}
}

func TestClaudeCodeValidator_MessagesAcceptsClaudeVSCodeObservedShape(t *testing.T) {
	validator := NewClaudeCodeValidator()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
	req.Header.Set("User-Agent", "claude-vscode/2.1.196.b90")

	ok := validator.Validate(req, map[string]any{
		"model": "claude-opus-4-8",
		"system": []any{
			map[string]any{"type": "text", "text": "x-anthropic-billing-header: cc_version=2.1.196.b90; cc_entrypoint=claude-vscode;"},
			map[string]any{"type": "text", "text": "You are a Claude agent, built on Anthropic's Claude Agent SDK."},
		},
		"metadata":      map[string]any{"user_id": validClaudeCodeJSONMetadataUserID()},
		"output_config": map[string]any{"effort": "high"},
	})
	require.True(t, ok)
}

func TestClaudeCodeValidator_MessagesToleratesStrippedHeadersWithStrongEvidence(t *testing.T) {
	validator := NewClaudeCodeValidator()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
	req.Header.Set("User-Agent", "Claude Code/2.1.161")
	// Simulate downstream/proxy layers stripping X-App, anthropic-beta, and anthropic-version.

	ok := validator.Validate(req, validClaudeCodeValidatorBody(
		validClaudeCodeJSONMetadataUserID(),
		"You are Claude Code, Anthropic's official CLI for Claude.",
	))
	require.True(t, ok)
}

func TestClaudeCodeValidator_MessagesRejectsThirdPartyUAEvenWithStrongBodyEvidence(t *testing.T) {
	validator := NewClaudeCodeValidator()
	body := validClaudeCodeValidatorBody(
		validClaudeCodeJSONMetadataUserID(),
		"You are Claude Code, Anthropic's official CLI for Claude.",
	)

	for _, ua := range []string{"curl/8.0.0", "new-api/1.0.0", "openai/5.0.0"} {
		t.Run(ua, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
			req.Header.Set("User-Agent", ua)
			require.False(t, validator.Validate(req, body), "UA: %q", ua)
		})
	}
}

func TestClaudeCodeValidator_MessagesRejectsInvalidMetadataWithoutClaudeCodePrompt(t *testing.T) {
	validator := NewClaudeCodeValidator()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
	req.Header.Set("User-Agent", "claude-cli/2.1.161")

	ok := validator.Validate(req, validClaudeCodeValidatorBody("not-json-or-legacy", "Generate JSON data for testing database migrations."))
	require.False(t, ok)
}

func TestClaudeCodeValidator_MessagesRejectsClaudeCodePromptWithoutValidMetadata(t *testing.T) {
	validator := NewClaudeCodeValidator()

	tests := []struct {
		name           string
		metadataUserID string
	}{
		{name: "missing metadata"},
		{name: "invalid metadata", metadataUserID: "not-json-or-legacy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
			req.Header.Set("User-Agent", "Claude Code/2.1.161")

			ok := validator.Validate(req, validClaudeCodeValidatorBody(
				tt.metadataUserID,
				"You are Claude Code, Anthropic's official CLI for Claude.",
			))
			require.False(t, ok)
		})
	}
}

func TestClaudeCodeValidator_MessagesRejectsValidMetadataWithNonClaudeCodePrompt(t *testing.T) {
	validator := NewClaudeCodeValidator()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
	req.Header.Set("User-Agent", "Claude Code/2.1.161")

	ok := validator.Validate(req, validClaudeCodeValidatorBody(
		validClaudeCodeJSONMetadataUserID(),
		"Generate JSON data for testing database migrations.",
	))
	require.False(t, ok)
}

const claudeCodeMetadataUserIDJSON = `{"device_id":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","account_uuid":"","session_id":"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"}`

func TestClaudeCodeValidator_ProbeBypass(t *testing.T) {
	validator := NewClaudeCodeValidator()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
	req.Header.Set("User-Agent", "claude-cli/1.2.3 (darwin; arm64)")
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.IsMaxTokensOneHaikuRequest, true))

	ok := validator.Validate(req, map[string]any{
		"model":      "claude-haiku-4-5",
		"max_tokens": 1,
	})
	require.True(t, ok)
}

func TestClaudeCodeValidator_ProbeBypassRequiresUA(t *testing.T) {
	validator := NewClaudeCodeValidator()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
	req.Header.Set("User-Agent", "curl/8.0.0")
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.IsMaxTokensOneHaikuRequest, true))

	ok := validator.Validate(req, map[string]any{
		"model":      "claude-haiku-4-5",
		"max_tokens": 1,
	})
	require.False(t, ok)
}

func TestClaudeCodeValidator_MessagesWithoutProbeStillNeedStrictValidation(t *testing.T) {
	validator := NewClaudeCodeValidator()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
	req.Header.Set("User-Agent", "claude-cli/1.2.3 (darwin; arm64)")

	ok := validator.Validate(req, map[string]any{
		"model":      "claude-haiku-4-5",
		"max_tokens": 1,
	})
	require.False(t, ok)
}

func TestClaudeCodeValidator_CountTokensPathUAOnly(t *testing.T) {
	validator := NewClaudeCodeValidator()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages/count_tokens", nil)
	req.Header.Set("User-Agent", "claude-cli/2.1.156 (Claude Code)")

	ok := validator.Validate(req, map[string]any{
		"model": "claude-opus-4-8",
	})
	require.True(t, ok)
}

func TestClaudeCodeValidator_CountTokensPathRequiresUA(t *testing.T) {
	validator := NewClaudeCodeValidator()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages/count_tokens", nil)
	req.Header.Set("User-Agent", "curl/8.0.0")

	ok := validator.Validate(req, map[string]any{
		"model": "claude-opus-4-8",
	})
	require.False(t, ok)
}

func TestClaudeCodeValidator_MessagesPathFullValid(t *testing.T) {
	validator := NewClaudeCodeValidator()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
	req.Header.Set("User-Agent", "claude-cli/2.1.156 (Claude Code)")
	req.Header.Set("X-App", "claude-code")
	req.Header.Set("anthropic-beta", "claude-code-20250219")
	req.Header.Set("anthropic-version", "2023-06-01")

	ok := validator.Validate(req, map[string]any{
		"model":  "claude-opus-4-8",
		"stream": true,
		"system": []any{
			map[string]any{
				"type": "text",
				"text": "You are Claude Code, Anthropic's official CLI for Claude.",
			},
		},
		"metadata": map[string]any{
			"user_id": "user_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa_account__session_aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		},
	})
	require.True(t, ok)
}

func TestClaudeCodeValidator_BillingBlockRecognizedWithoutIdentityPrompt(t *testing.T) {
	// 真实抓取的完整安全监视器 system prompt（不含身份 prose）。
	monitorPrompt, err := os.ReadFile("testdata/security_monitor_system_prompt.txt")
	require.NoError(t, err)

	validator := NewClaudeCodeValidator()

	// 前提：完整监视器正文经 Dice 相似度远低于阈值，无法被身份 prose 机制识别——
	// 故下面 Validate 的放行只可能来自计费归因块识别。
	require.Less(t, validator.bestSimilarityScore(string(monitorPrompt)), systemPromptThreshold)

	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
	req.Header.Set("User-Agent", "claude-cli/2.1.162 (external, cli)")
	req.Header.Set("X-App", "cli")
	req.Header.Set("anthropic-beta", "claude-code-20250219")
	req.Header.Set("anthropic-version", "2023-06-01")

	// Claude Code 安全监视器子请求：不携带身份 prose，但 system 数组携带计费归因块
	// cc_entrypoint=cli，应据此识别为 Claude Code 客户端。
	ok := validator.Validate(req, map[string]any{
		"model": "claude-3-5-haiku-20241022",
		"system": []any{
			map[string]any{
				"type": "text",
				"text": "x-anthropic-billing-header: cc_version=2.1.162.884; cc_entrypoint=cli; cch=d8726;",
			},
			map[string]any{
				"type": "text",
				"text": string(monitorPrompt),
			},
		},
		"metadata": map[string]any{
			"user_id": claudeCodeMetadataUserIDJSON,
		},
	})
	require.True(t, ok)
}

func TestClaudeCodeValidator_BillingBlockVSCodeEntrypointRecognized(t *testing.T) {
	monitorPrompt, err := os.ReadFile("testdata/security_monitor_system_prompt.txt")
	require.NoError(t, err)

	validator := NewClaudeCodeValidator()
	require.Less(t, validator.bestSimilarityScore(string(monitorPrompt)), systemPromptThreshold)

	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
	req.Header.Set("User-Agent", "claude-cli/2.1.181 (external, claude-vscode, agent-sdk/0.3.181)")
	req.Header.Set("X-App", "cli")
	req.Header.Set("anthropic-beta", "claude-code-20250219")
	req.Header.Set("anthropic-version", "2023-06-01")

	ok := validator.Validate(req, map[string]any{
		"model": "claude-opus-4-8",
		"system": []any{
			map[string]any{
				"type": "text",
				"text": "x-anthropic-billing-header: cc_version=2.1.181.f17; cc_entrypoint=claude-vscode;",
			},
			map[string]any{
				"type": "text",
				"text": string(monitorPrompt),
			},
		},
		"metadata": map[string]any{
			"user_id": claudeCodeMetadataUserIDJSON,
		},
	})
	require.True(t, ok)
}

func TestClaudeCodeValidator_BillingBlockWithoutEntrypointFallsThrough(t *testing.T) {
	validator := NewClaudeCodeValidator()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
	req.Header.Set("User-Agent", "claude-cli/2.1.162 (external, cli)")
	req.Header.Set("X-App", "cli")
	req.Header.Set("anthropic-beta", "claude-code-20250219")
	req.Header.Set("anthropic-version", "2023-06-01")

	// 计费块前缀命中但完全没有 cc_entrypoint= 字段，且无身份 prose：
	// 不应凭前缀放行，应落回 Dice 检查并失败。验证 cc_entrypoint= 字段存在仍是必要条件。
	ok := validator.Validate(req, map[string]any{
		"model": "claude-3-5-haiku-20241022",
		"system": []any{
			map[string]any{
				"type": "text",
				"text": "x-anthropic-billing-header: cc_version=2.1.162.884; cch=d8726;",
			},
			map[string]any{
				"type": "text",
				"text": "Some unrelated system prompt that does not resemble Claude Code.",
			},
		},
		"metadata": map[string]any{
			"user_id": claudeCodeMetadataUserIDJSON,
		},
	})
	require.False(t, ok)
}

func TestClaudeCodeValidator_BillingBlockStillRequiresClaudeCodeUA(t *testing.T) {
	validator := NewClaudeCodeValidator()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
	req.Header.Set("User-Agent", "curl/8.0.0")
	req.Header.Set("X-App", "cli")
	req.Header.Set("anthropic-beta", "claude-code-20250219")
	req.Header.Set("anthropic-version", "2023-06-01")

	// 计费块无法绕过 UA 校验：非 claude-cli 客户端在 Step 1 即被拒。
	ok := validator.Validate(req, map[string]any{
		"model": "claude-3-5-haiku-20241022",
		"system": []any{
			map[string]any{
				"type": "text",
				"text": "x-anthropic-billing-header: cc_version=2.1.162.884; cc_entrypoint=cli; cch=d8726;",
			},
		},
	})
	require.False(t, ok)
}

func TestClaudeCodeValidator_MessagesPathRejectsNonClaudeCodeUA(t *testing.T) {
	validator := NewClaudeCodeValidator()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
	req.Header.Set("User-Agent", "curl/8.0.0")
	req.Header.Set("X-App", "claude-code")
	req.Header.Set("anthropic-beta", "claude-code-20250219")
	req.Header.Set("anthropic-version", "2023-06-01")

	ok := validator.Validate(req, map[string]any{
		"model":  "claude-opus-4-8",
		"stream": true,
		"system": []any{
			map[string]any{
				"type": "text",
				"text": "You are Claude Code, Anthropic's official CLI for Claude.",
			},
		},
		"metadata": map[string]any{
			"user_id": "user_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa_account__session_aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		},
	})
	require.False(t, ok)
}

func TestClaudeCodeValidator_MessagesPathWithoutSystemPromptStillRejected(t *testing.T) {
	validator := NewClaudeCodeValidator()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
	req.Header.Set("User-Agent", "claude-cli/2.1.156 (Claude Code)")
	req.Header.Set("X-App", "claude-code")
	req.Header.Set("anthropic-beta", "claude-code-20250219")
	req.Header.Set("anthropic-version", "2023-06-01")

	ok := validator.Validate(req, map[string]any{
		"model":  "claude-opus-4-8",
		"stream": true,
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		"metadata": map[string]any{
			"user_id": "user_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa_account__session_aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		},
	})
	require.False(t, ok)
}

func TestClaudeCodeValidator_NonMessagesPathUAOnly(t *testing.T) {
	validator := NewClaudeCodeValidator()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/models", nil)
	req.Header.Set("User-Agent", "claude-cli/1.2.3 (darwin; arm64)")

	ok := validator.Validate(req, nil)
	require.True(t, ok)
}

func TestExtractVersion(t *testing.T) {
	v := NewClaudeCodeValidator()
	tests := []struct {
		ua   string
		want string
	}{
		{"claude-cli/2.1.22 (darwin; arm64)", "2.1.22"},
		{"claude-cli/2.1.161 (external, sdk-cli)", "2.1.161"},
		{"Claude Code/2.1.161", "2.1.161"},
		{"claude-code/2.1.161", "2.1.161"},
		{"ClaudeCode/2.1.161", "2.1.161"},
		{"claude-cli/1.0.0", "1.0.0"},
		{"Claude-CLI/3.10.5 (linux; x86_64)", "3.10.5"}, // 大小写不敏感
		{"curl/8.0.0", ""},                              // 非 Claude CLI
		{"", ""},                                        // 空字符串
		{"claude-cli/", ""},                             // 无版本号
		{"claude-cli/2.1.22-beta", "2.1.22"},            // 带后缀仍提取主版本号
	}
	for _, tt := range tests {
		got := v.ExtractVersion(tt.ua)
		require.Equal(t, tt.want, got, "ExtractVersion(%q)", tt.ua)
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"2.1.0", "2.1.0", 0},   // 相等
		{"2.1.1", "2.1.0", 1},   // patch 更大
		{"2.0.0", "2.1.0", -1},  // minor 更小
		{"3.0.0", "2.99.99", 1}, // major 更大
		{"1.0.0", "2.0.0", -1},  // major 更小
		{"0.0.1", "0.0.0", 1},   // patch 差异
		{"", "1.0.0", -1},       // 空字符串 vs 正常版本
		{"v2.1.0", "2.1.0", 0},  // v 前缀处理
	}
	for _, tt := range tests {
		got := CompareVersions(tt.a, tt.b)
		require.Equal(t, tt.want, got, "CompareVersions(%q, %q)", tt.a, tt.b)
	}
}

func TestSetGetClaudeCodeVersion(t *testing.T) {
	ctx := context.Background()
	require.Equal(t, "", GetClaudeCodeVersion(ctx), "empty context should return empty string")

	ctx = SetClaudeCodeVersion(ctx, "2.1.63")
	require.Equal(t, "2.1.63", GetClaudeCodeVersion(ctx))
}

func TestParseMetadataUserIDRejectsAllZeroJSONPlaceholder(t *testing.T) {
	placeholder := `{"device_id":"0000000000000000000000000000000000000000000000000000000000000000","account_uuid":"","session_id":"00000000-0000-4000-8000-000000000000"}`

	require.Nil(t, ParseMetadataUserID(placeholder))
}

func TestParseMetadataUserIDRejectsAllZeroLegacyPlaceholder(t *testing.T) {
	placeholder := "user_0000000000000000000000000000000000000000000000000000000000000000_account__session_00000000-0000-4000-8000-000000000000"

	require.Nil(t, ParseMetadataUserID(placeholder))
}

func TestDetectSuspiciousClaudeCodeProbeBlocksZeroMetadataTestBillingHeader(t *testing.T) {
	body := []byte(`{
		"model":"claude-opus-4-8",
		"system":[
			{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.126.test; cc_entrypoint=sdk-cli; cch=00000;"},
			{"type":"text","text":"You are a Claude agent, built on Anthropic's Claude Agent SDK."}
		],
		"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],
		"max_tokens":1,
		"stream":true,
		"metadata":{"user_id":"{\"device_id\":\"0000000000000000000000000000000000000000000000000000000000000000\",\"account_uuid\":\"\",\"session_id\":\"00000000-0000-4000-8000-000000000000\"}"}
	}`)

	result := DetectSuspiciousClaudeCodeProbe(body)

	require.True(t, result.Block)
	require.Contains(t, result.Reasons, "zero_metadata_user_id")
	require.Contains(t, result.Reasons, "test_cc_version")
	require.Contains(t, result.Reasons, "old_cc_version")
	require.Contains(t, result.Reasons, "placeholder_cch")
}

func TestDetectSuspiciousClaudeCodeProbeDoesNotBlockRealMetadataWithPlaceholderCCH(t *testing.T) {
	body := []byte(`{
		"model":"claude-opus-4-8",
		"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.161; cc_entrypoint=sdk-cli; cch=00000;"}],
		"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],
		"max_tokens":1,
		"stream":true,
		"metadata":{"user_id":"{\"device_id\":\"a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2\",\"account_uuid\":\"\",\"session_id\":\"123e4567-e89b-42d3-a456-426614174000\"}"}
	}`)

	result := DetectSuspiciousClaudeCodeProbe(body)

	require.False(t, result.Block)
	require.Contains(t, result.Reasons, "placeholder_cch")
	require.NotContains(t, result.Reasons, "zero_metadata_user_id")
}

func TestDetectSuspiciousClaudeCodeProbeRequiresStreamingOneTokenProbeShape(t *testing.T) {
	body := []byte(`{
		"model":"claude-opus-4-8",
		"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.126.test; cc_entrypoint=sdk-cli; cch=00000;"}],
		"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],
		"max_tokens":128,
		"stream":true,
		"metadata":{"user_id":"{\"device_id\":\"0000000000000000000000000000000000000000000000000000000000000000\",\"account_uuid\":\"\",\"session_id\":\"00000000-0000-4000-8000-000000000000\"}"}
	}`)

	result := DetectSuspiciousClaudeCodeProbe(body)

	require.False(t, result.Block)
	require.NotContains(t, result.Reasons, "streaming_one_token_probe")
}

func TestDetectSuspiciousClaudeCodeProbeBlocksLegacyZeroMetadataPlaceholder(t *testing.T) {
	body := []byte(`{
		"model":"claude-opus-4-8",
		"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.126.test; cc_entrypoint=sdk-cli; cch=00000;"}],
		"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],
		"max_tokens":1,
		"stream":true,
		"metadata":{"user_id":"user_0000000000000000000000000000000000000000000000000000000000000000_account__session_00000000-0000-4000-8000-000000000000"}
	}`)

	result := DetectSuspiciousClaudeCodeProbe(body)

	require.True(t, result.Block)
	require.Contains(t, result.Reasons, "zero_metadata_user_id")
}
