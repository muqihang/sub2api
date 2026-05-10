//go:build unit

package service

import "testing"

func TestResolveAntigravityTestModel(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
	}{
		{
			name:  "empty model uses current supported claude default",
			input: "",
			want:  "claude-sonnet-4-6",
		},
		{
			name:  "explicit model is preserved",
			input: "claude-sonnet-4-5",
			want:  "claude-sonnet-4-5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveAntigravityTestModel(tt.input)
			if got != tt.want {
				t.Fatalf("resolveAntigravityTestModel(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
