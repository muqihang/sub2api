package service

type AugmentGatewayProvider string

const (
	AugmentGatewayProviderOpenAI    AugmentGatewayProvider = "openai"
	AugmentGatewayProviderDeepSeek  AugmentGatewayProvider = "deepseek"
	AugmentGatewayProviderAnthropic AugmentGatewayProvider = "anthropic"
	AugmentGatewayProviderGemini    AugmentGatewayProvider = "gemini"
)

type AugmentGatewayModel struct {
	ID              string
	Provider        AugmentGatewayProvider
	UpstreamModel   string
	ReasoningEffort string
	ProviderGroupID int64
}

type AugmentGatewayToolCall struct {
	ID       string                         `json:"id,omitempty"`
	Type     string                         `json:"type,omitempty"`
	Function AugmentGatewayToolCallFunction `json:"function"`
}

type AugmentGatewayToolCallFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}
