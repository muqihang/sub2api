package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeGLMOpenAIReasoningEffort(t *testing.T) {
	tests := []struct {
		name          string
		model         string
		input         string
		wantApplied   bool
		wantPath      string
		wantValue     string
		wantUnchanged bool
	}{
		{
			name:        "flat xhigh maps to max",
			model:       "glm-5.2",
			input:       `{"model":"glm-5.2","reasoning_effort":"xhigh","messages":[]}`,
			wantApplied: true,
			wantPath:    "reasoning_effort",
			wantValue:   "max",
		},
		{
			name:        "flat medium maps to high",
			model:       "glm-5.2",
			input:       `{"model":"glm-5.2","reasoning_effort":"medium","messages":[]}`,
			wantApplied: true,
			wantPath:    "reasoning_effort",
			wantValue:   "high",
		},
		{
			name:        "nested high case normalizes",
			model:       "GLM-5.2",
			input:       `{"model":"glm-5.2","reasoning":{"effort":"HIGH"},"messages":[]}`,
			wantApplied: true,
			wantPath:    "reasoning.effort",
			wantValue:   "high",
		},
		{
			name:          "native max unchanged",
			model:         "glm-5.2",
			input:         `{"model":"glm-5.2","reasoning_effort":"max","messages":[]}`,
			wantApplied:   false,
			wantUnchanged: true,
		},
		{
			name:          "non glm unchanged",
			model:         "deepseek-v4-pro",
			input:         `{"model":"deepseek-v4-pro","reasoning_effort":"xhigh","messages":[]}`,
			wantApplied:   false,
			wantUnchanged: true,
		},
		{
			name:          "unknown effort unchanged",
			model:         "glm-5.2",
			input:         `{"model":"glm-5.2","reasoning_effort":"banana","messages":[]}`,
			wantApplied:   false,
			wantUnchanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, applied := NormalizeGLMOpenAIReasoningEffort([]byte(tt.input), tt.model)
			require.Equal(t, tt.wantApplied, applied)
			if tt.wantUnchanged {
				require.Equal(t, tt.input, string(got))
				return
			}
			require.Equal(t, tt.wantValue, gjson.GetBytes(got, tt.wantPath).String())
		})
	}
}
