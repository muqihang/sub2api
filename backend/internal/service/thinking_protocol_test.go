package service

import "testing"

func TestResolveThinkingProtocol(t *testing.T) {
	tests := []struct {
		name    string
		modelID string
		want    ThinkingProtocol
	}{
		{"claude official", "claude-sonnet-4-5", ThinkingProtocolAnthropicStrict},
		{"opus shorthand", "opus-4-5", ThinkingProtocolAnthropicStrict},
		{"haiku shorthand", "haiku-4-5", ThinkingProtocolAnthropicStrict},
		{"deepseek anthropic compat", "deepseek-v4-pro", ThinkingProtocolPassbackRequired},
		{"kimi coding", "kimi-coding-v2", ThinkingProtocolPassbackRequired},
		{"moonshot", "moonshot-v1-32k", ThinkingProtocolPassbackRequired},
		{"glm", "glm-5.1", ThinkingProtocolPassbackRequired},
		{"minimax m series", "MiniMax-M2.7-highspeed", ThinkingProtocolPassbackRequired},
		{"qwen thinking", "qwen3-235b-a22b-thinking-2507", ThinkingProtocolPassbackRequired},
		{"qwen non thinking", "qwen3-32b", ThinkingProtocolUnknown},
		{"gpt unknown", "gpt-5.1", ThinkingProtocolUnknown},
		{"empty", "", ThinkingProtocolUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveThinkingProtocol(tt.modelID); got != tt.want {
				t.Fatalf("ResolveThinkingProtocol(%q) = %v, want %v", tt.modelID, got, tt.want)
			}
		})
	}
}

func TestThinkingProtocolFilterDecisions(t *testing.T) {
	if !ShouldPreFilterThinkingBlocks("claude-sonnet-4-5") {
		t.Fatal("Anthropic strict models should pre-filter invalid thinking blocks")
	}
	if ShouldPreFilterThinkingBlocks("deepseek-v4-pro") {
		t.Fatal("passback-required models must not pre-filter thinking blocks")
	}
	if ShouldRectifyThinkingSignatureError("deepseek-v4-pro") {
		t.Fatal("passback-required models must not trigger signature rectifier retry")
	}
	if ShouldApplyRetryFilters("gpt-5.1") {
		t.Fatal("unknown models should conservatively skip retry filters")
	}
}
