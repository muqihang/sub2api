package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesRequiredCapability(t *testing.T) {
	tests := []struct {
		name string
		body string
		want service.OpenAIEndpointCapability
	}{
		{name: "hosted web search", body: `{"tools":[{"type":"web_search"}]}`, want: service.OpenAIEndpointCapabilityWebSearch},
		{name: "case insensitive", body: `{"tools":[{"type":"WEB_SEARCH"}]}`, want: service.OpenAIEndpointCapabilityWebSearch},
		{name: "custom tool", body: `{"tools":[{"type":"custom","name":"exec"}]}`, want: service.OpenAIEndpointCapabilityResponsesCustomTools},
		{name: "string custom tool", body: `{"tools":["exec"]}`, want: service.OpenAIEndpointCapabilityResponsesCustomTools},
		{name: "web search and custom tool", body: `{"tools":[{"type":"web_search"},{"type":"custom","name":"exec"}]}`, want: service.OpenAIEndpointCapabilityWebSearchCustomTools},
		{name: "function named web search is not hosted", body: `{"tools":[{"type":"function","name":"web_search"}]}`, want: service.OpenAIEndpointCapabilityChatCompletions},
		{name: "ordinary responses", body: `{"input":"hello"}`, want: service.OpenAIEndpointCapabilityChatCompletions},
		{name: "malformed tools", body: `{"tools":"web_search"}`, want: service.OpenAIEndpointCapabilityChatCompletions},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, openAIResponsesRequiredCapability([]byte(tc.body)))
		})
	}
}
