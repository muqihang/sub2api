package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func ptrStringForChineseThinkingTest(v string) *string { return &v }

func TestDefaultEffortForChineseThinkingEnabled(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  *string
	}{
		{name: "glm passback model", model: "glm-5.1", want: ptrStringForChineseThinkingTest("high")},
		{name: "kimi passback model", model: "kimi-k2.6", want: ptrStringForChineseThinkingTest("high")},
		{name: "moonshot passback model", model: "moonshot-v1-8k", want: ptrStringForChineseThinkingTest("high")},
		{name: "minimax passback model mixed case", model: "MiniMax-M3", want: ptrStringForChineseThinkingTest("high")},
		{name: "qwen thinking variant", model: "qwen3-235b-a22b-thinking-2507", want: ptrStringForChineseThinkingTest("high")},
		{name: "deepseek has native effort", model: "deepseek-v4-pro", want: nil},
		{name: "claude strict protocol", model: "claude-sonnet-4-5", want: nil},
		{name: "openai model", model: "gpt-5.6", want: nil},
		{name: "empty model", model: "", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DefaultEffortForChineseThinkingEnabled(tt.model)
			if tt.want == nil {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			require.Equal(t, *tt.want, *got)
		})
	}
}

func TestOpenAIBodyHasThinkingEnabled(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "enabled", body: `{"thinking":{"type":"enabled"}}`, want: true},
		{name: "adaptive", body: `{"thinking":{"type":"adaptive"}}`, want: true},
		{name: "uppercase enabled", body: `{"thinking":{"type":"ENABLED"}}`, want: true},
		{name: "disabled", body: `{"thinking":{"type":"disabled"}}`, want: false},
		{name: "missing type", body: `{"thinking":{"budget_tokens":8192}}`, want: false},
		{name: "missing thinking", body: `{"model":"glm-5.1"}`, want: false},
		{name: "invalid json fails closed", body: `{"thinking":{"type":"enabled"`, want: false},
		{name: "empty body", body: ``, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, OpenAIBodyHasThinkingEnabled([]byte(tt.body)))
		})
	}
}

func TestApplyChineseThinkingEffortFallback(t *testing.T) {
	existing := ptrStringForChineseThinkingTest("medium")
	tests := []struct {
		name     string
		effort   *string
		body     string
		model    string
		want     *string
		wantSame bool
	}{
		{
			name:     "explicit effort preserved",
			effort:   existing,
			body:     `{"thinking":{"type":"enabled"}}`,
			model:    "kimi-k2.6",
			wantSame: true,
		},
		{
			name:   "glm thinking enabled gets high",
			effort: nil,
			body:   `{"thinking":{"type":"enabled"}}`,
			model:  "glm-5.1",
			want:   ptrStringForChineseThinkingTest("high"),
		},
		{
			name:   "kimi adaptive gets high",
			effort: nil,
			body:   `{"thinking":{"type":"adaptive"}}`,
			model:  "kimi-k2.6",
			want:   ptrStringForChineseThinkingTest("high"),
		},
		{
			name:   "minimax enabled gets high",
			effort: nil,
			body:   `{"thinking":{"type":"enabled"}}`,
			model:  "minimax-m3",
			want:   ptrStringForChineseThinkingTest("high"),
		},
		{
			name:   "thinking disabled does not fallback",
			effort: nil,
			body:   `{"thinking":{"type":"disabled"}}`,
			model:  "glm-5.1",
			want:   nil,
		},
		{
			name:   "deepseek excluded",
			effort: nil,
			body:   `{"thinking":{"type":"enabled"}}`,
			model:  "deepseek-v4-pro",
			want:   nil,
		},
		{
			name:   "non passback model excluded",
			effort: nil,
			body:   `{"thinking":{"type":"enabled"}}`,
			model:  "gpt-5.6",
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ApplyChineseThinkingEffortFallback(tt.effort, []byte(tt.body), tt.model)
			if tt.wantSame {
				require.Same(t, tt.effort, got)
				return
			}
			if tt.want == nil {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			require.Equal(t, *tt.want, *got)
		})
	}
}
