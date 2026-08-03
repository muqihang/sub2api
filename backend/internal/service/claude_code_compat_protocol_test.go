package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnthropicOnlyCompatProtocolAcceptsMessagesRoutes(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-6","max_tokens":32000,"stream":true,"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}],"thinking":{"type":"enabled","budget_tokens":1024},"tools":[{"name":"lookup","description":"lookup","input_schema":{"type":"object"}}]}`)

	for _, route := range []string{"/v1/messages", "/v1/messages?beta=true"} {
		t.Run(route, func(t *testing.T) {
			decision, err := ValidateAnthropicOnlyCompatIngress(http.MethodPost, route, body)
			require.NoError(t, err)
			require.Equal(t, "/v1/messages", decision.InboundRoute)
			require.Equal(t, "/v1/messages?beta=true", decision.CCGatewayRoute)
		})
	}
}

func TestAnthropicOnlyCompatProtocolAcceptsSystemRoleMessagesForClaudeModels(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-6","max_tokens":32000,"messages":[{"role":"system","content":"private system"},{"role":"user","content":"hello"}]}`)

	decision, err := ValidateAnthropicOnlyCompatIngress(http.MethodPost, "/v1/messages", body)
	require.NoError(t, err)
	require.Equal(t, "/v1/messages", decision.InboundRoute)
	require.Equal(t, "/v1/messages?beta=true", decision.CCGatewayRoute)
}

func TestAnthropicOnlyCompatProtocolAllowsBridgeRuntimeNonClaudeModelOnlyWithExplicitOption(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-flash","max_tokens":1,"messages":[{"role":"user","content":"hello"}]}`)

	_, err := ValidateAnthropicOnlyCompatIngress(http.MethodPost, "/v1/messages", body)
	require.Error(t, err)
	var protocolErr *AnthropicCompatProtocolError
	require.ErrorAs(t, err, &protocolErr)
	require.Equal(t, "unsupported_body_shape", protocolErr.Code)

	decision, err := ValidateAnthropicOnlyCompatIngressWithOptions(http.MethodPost, "/v1/messages", body, AnthropicCompatIngressOptions{AllowBridgeRuntimeModels: true})
	require.NoError(t, err)
	require.Equal(t, AnthropicCompatClientType, decision.ClientType)
	require.Equal(t, AnthropicCompatInboundMessages, decision.InboundRoute)
}

func TestAnthropicOnlyCompatProtocolRejectsClaudeCodeBridgeDisplayIDsEvenWithRuntimeOption(t *testing.T) {
	body := []byte(`{"model":"claude-code-bridge-deepseek-v4-pro","max_tokens":1,"messages":[{"role":"user","content":"bridge-display-must-not-reach-formal-pool"}]}`)

	for _, opts := range []AnthropicCompatIngressOptions{
		{},
		{AllowBridgeRuntimeModels: true},
	} {
		_, err := ValidateAnthropicOnlyCompatIngressWithOptions(http.MethodPost, "/v1/messages", body, opts)
		require.Error(t, err)
		var protocolErr *AnthropicCompatProtocolError
		require.ErrorAs(t, err, &protocolErr)
		require.Equal(t, "unsupported_body_shape", protocolErr.Code)
		require.NotContains(t, protocolErr.Error(), "bridge-display-must-not-reach-formal-pool")
		require.NotContains(t, protocolErr.Error(), "claude-code-bridge-deepseek-v4-pro")
	}
}

func TestAnthropicOnlyCompatProtocolRejectsOpenAIProtocolRoutes(t *testing.T) {
	body := []byte(`{"model":"gpt-5","messages":[{"role":"user","content":"hello RAW_TOKEN_SENTINEL PROXY_CREDENTIAL_SENTINEL ACCOUNT_REF_SENTINEL"}]}`)

	for _, route := range []string{"/v1/chat/completions", "/v1/responses"} {
		t.Run(route, func(t *testing.T) {
			_, err := ValidateAnthropicOnlyCompatIngress(http.MethodPost, route, body)
			require.Error(t, err)
			var protocolErr *AnthropicCompatProtocolError
			require.ErrorAs(t, err, &protocolErr)
			require.Equal(t, http.StatusBadRequest, protocolErr.Status)
			require.Equal(t, "unsupported_protocol", protocolErr.Code)
			require.NotContains(t, protocolErr.Error(), "hello")
			require.NotContains(t, protocolErr.Error(), "gpt-5")
			require.NotContains(t, protocolErr.Error(), "RAW_TOKEN_SENTINEL")
			require.NotContains(t, protocolErr.Error(), "PROXY_CREDENTIAL_SENTINEL")
			require.NotContains(t, protocolErr.Error(), "ACCOUNT_REF_SENTINEL")
		})
	}
}

func TestAnthropicOnlyCompatProtocolRejectsOpenAIShapedBodyOnMessages(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "responses input",
			body: `{"model":"claude-sonnet-4-6","input":"private prompt","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`,
		},
		{
			name: "chat functions",
			body: `{"model":"claude-sonnet-4-6","functions":[{"name":"leak"}],"messages":[{"role":"user","content":"hello"}]}`,
		},
		{
			name: "openai function tool shape",
			body: `{"model":"claude-sonnet-4-6","tools":[{"type":"function","function":{"name":"leak","parameters":{"type":"object"}}}],"messages":[{"role":"user","content":"hello"}]}`,
		},
		{
			name: "openai function tool type without function",
			body: `{"model":"claude-sonnet-4-6","tools":[{"type":"function","name":"leak","parameters":{"type":"object"}}],"messages":[{"role":"user","content":"hello"}]}`,
		},
		{
			name: "openai function tool choice",
			body: `{"model":"claude-sonnet-4-6","tools":[{"name":"lookup","input_schema":{"type":"object"}}],"tool_choice":{"type":"function","function":{"name":"leak"}},"messages":[{"role":"user","content":"hello"}]}`,
		},
		{
			name: "invalid anthropic tool missing name",
			body: `{"model":"claude-sonnet-4-6","tools":[{"input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"hello"}]}`,
		},
		{
			name: "invalid anthropic tool missing input schema",
			body: `{"model":"claude-sonnet-4-6","tools":[{"name":"leak"}],"messages":[{"role":"user","content":"hello"}]}`,
		},
		{
			name: "invalid anthropic tool name",
			body: `{"model":"claude-sonnet-4-6","tools":[{"name":"unsafe.tool","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"hello"}]}`,
		},
		{
			name: "invalid anthropic tool choice missing name",
			body: `{"model":"claude-sonnet-4-6","tools":[{"name":"lookup","input_schema":{"type":"object"}}],"tool_choice":{"type":"tool"},"messages":[{"role":"user","content":"hello"}]}`,
		},
		{
			name: "invalid anthropic tool choice unknown tool",
			body: `{"model":"claude-sonnet-4-6","tools":[{"name":"lookup","input_schema":{"type":"object"}}],"tool_choice":{"type":"tool","name":"unknown_tool"},"messages":[{"role":"user","content":"hello"}]}`,
		},

		{
			name: "developer role in messages",
			body: `{"model":"claude-sonnet-4-6","messages":[{"role":"developer","content":"private developer"},{"role":"user","content":"hello"}]}`,
		},
		{
			name: "tool role in messages",
			body: `{"model":"claude-sonnet-4-6","messages":[{"role":"tool","content":"private tool"},{"role":"user","content":"hello"}]}`,
		},
		{
			name: "openai model",
			body: `{"model":"gpt-5","messages":[{"role":"user","content":"private prompt"}]}`,
		},
		{
			name: "openai max completion tokens",
			body: `{"model":"claude-sonnet-4-6","max_completion_tokens":100,"messages":[{"role":"user","content":"private prompt"}]}`,
		},
		{
			name: "openai chat completion options",
			body: `{"model":"claude-sonnet-4-6","n":2,"stop":["private stop"],"stream_options":{"include_usage":true},"user":"private-user","messages":[{"role":"user","content":"private prompt"}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidateAnthropicOnlyCompatIngress(http.MethodPost, "/v1/messages", []byte(tt.body))
			require.Error(t, err)
			var protocolErr *AnthropicCompatProtocolError
			require.ErrorAs(t, err, &protocolErr)
			require.Equal(t, "unsupported_body_shape", protocolErr.Code)
			require.NotContains(t, protocolErr.Error(), "private")
			require.NotContains(t, protocolErr.Error(), "leak")
			require.NotContains(t, protocolErr.Error(), "hello")
		})
	}
}

func TestAnthropicCompatProtocolAcceptsClaudeCodeParallelToolName(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-6","max_tokens":64,"messages":[{"role":"user","content":"launch agents"}],"tools":[{"name":"multi_tool_use.parallel","description":"parallel tools","input_schema":{"type":"object"}},{"name":"Agent","description":"subagent","input_schema":{"type":"object"}}],"tool_choice":{"type":"tool","name":"multi_tool_use.parallel"}}`)

	decision, err := ValidateAnthropicOnlyCompatIngress(http.MethodPost, "/v1/messages", body)

	require.NoError(t, err)
	require.Equal(t, AnthropicCompatClientType, decision.ClientType)
}
