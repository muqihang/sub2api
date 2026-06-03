package service

import (
	"context"
	"net/http"
	"net/http/httptest"
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
			name: "claude-code ua with json metadata",
			ua:   "claude-code/2.1.161 (darwin; arm64)",
			body: validClaudeCodeValidatorBody(validClaudeCodeJSONMetadataUserID(), ""),
		},
		{
			name: "Claude Code ua with json metadata and system prompt",
			ua:   "Claude Code/2.1.161",
			body: validClaudeCodeValidatorBody(validClaudeCodeJSONMetadataUserID(), "You are Claude Code, Anthropic's official CLI for Claude."),
		},
		{
			name: "ClaudeCode ua with json metadata",
			ua:   "ClaudeCode/2.1.161",
			body: validClaudeCodeValidatorBody(validClaudeCodeJSONMetadataUserID(), ""),
		},
		{
			name: "claude-cli without version with json metadata",
			ua:   "claude-cli",
			body: validClaudeCodeValidatorBody(validClaudeCodeJSONMetadataUserID(), ""),
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

func TestClaudeCodeValidator_MessagesAcceptsValidMetadataWithNonClaudeCodePrompt(t *testing.T) {
	validator := NewClaudeCodeValidator()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
	req.Header.Set("User-Agent", "Claude Code/2.1.161")

	ok := validator.Validate(req, validClaudeCodeValidatorBody(
		validClaudeCodeJSONMetadataUserID(),
		"Generate JSON data for testing database migrations.",
	))
	require.True(t, ok)
}

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
