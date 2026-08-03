package service

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestComputeClaudeCodeFingerprint_NodeGoldens(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "ascii", input: "hello capture test message here", want: "7c9"},
		{name: "mixed emoji", input: "hello capture 世界 emoji 😀 cch test", want: "88a"},
		{name: "chinese", input: "你好世界这是中文测试消息用于验证UTF16索引", want: "1a6"},
		{name: "surrogate pair indexing", input: "ab😀cdefghijklmnopqrstuvwxyz", want: "7cb"},
		{name: "selected code units recombine into emoji", input: "abcd😀😀abcdefghijklmnop", want: "8cd"},
		{name: "empty string", input: "", want: "b73"},
		{name: "short string fallback", input: "abc", want: "b73"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"messages":[{"role":"user","content":[{"type":"text","text":%q}]}]}`, tt.input))
			require.Equal(t, tt.want, computeClaudeCodeFingerprint(body, "2.1.145"))
		})
	}
}
