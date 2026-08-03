package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeChineseLLMThinking(t *testing.T) {
	tests := []struct {
		name          string
		model         string
		input         string
		wantApplied   bool
		wantTypeValue string
		wantUnchanged bool
	}{
		{
			name:          "minimax m3 enabled to adaptive",
			model:         "MiniMax-M3",
			input:         `{"model":"MiniMax-M3","thinking":{"type":"enabled","budget_tokens":8192},"messages":[]}`,
			wantApplied:   true,
			wantTypeValue: "adaptive",
		},
		{
			name:          "minimax m2.7 highspeed enabled to adaptive",
			model:         "minimax-m2.7-highspeed",
			input:         `{"model":"minimax-m2.7-highspeed","thinking":{"type":"enabled","budget_tokens":4096},"messages":[]}`,
			wantApplied:   true,
			wantTypeValue: "adaptive",
		},
		{
			name:          "minimax adaptive unchanged",
			model:         "minimax-m3",
			input:         `{"thinking":{"type":"adaptive","budget_tokens":8192},"messages":[]}`,
			wantApplied:   false,
			wantTypeValue: "adaptive",
			wantUnchanged: true,
		},
		{
			name:          "minimax disabled unchanged",
			model:         "minimax-m3",
			input:         `{"thinking":{"type":"disabled"},"messages":[]}`,
			wantApplied:   false,
			wantTypeValue: "disabled",
			wantUnchanged: true,
		},
		{
			name:          "kimi enabled unchanged",
			model:         "kimi-k2.6",
			input:         `{"thinking":{"type":"enabled","budget_tokens":8192},"messages":[]}`,
			wantApplied:   false,
			wantTypeValue: "enabled",
			wantUnchanged: true,
		},
		{
			name:          "glm enabled unchanged",
			model:         "glm-5.1",
			input:         `{"thinking":{"type":"enabled","budget_tokens":8192},"messages":[]}`,
			wantApplied:   false,
			wantTypeValue: "enabled",
			wantUnchanged: true,
		},
		{
			name:          "deepseek enabled unchanged",
			model:         "deepseek-v4-pro",
			input:         `{"thinking":{"type":"enabled","budget_tokens":8192},"messages":[]}`,
			wantApplied:   false,
			wantTypeValue: "enabled",
			wantUnchanged: true,
		},
		{
			name:          "claude enabled unchanged",
			model:         "claude-sonnet-4-5",
			input:         `{"thinking":{"type":"enabled","budget_tokens":8192},"messages":[]}`,
			wantApplied:   false,
			wantTypeValue: "enabled",
			wantUnchanged: true,
		},
		{
			name:          "invalid json fails open",
			model:         "minimax-m3",
			input:         `{"thinking":{"type":"enabled"`,
			wantApplied:   false,
			wantTypeValue: "",
			wantUnchanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, applied := NormalizeChineseLLMThinking([]byte(tt.input), tt.model)
			require.Equal(t, tt.wantApplied, applied)
			if tt.wantUnchanged {
				require.Equal(t, tt.input, string(out))
			}
			if tt.wantTypeValue != "" {
				require.Equal(t, tt.wantTypeValue, gjson.GetBytes(out, "thinking.type").String())
			}
		})
	}
}
