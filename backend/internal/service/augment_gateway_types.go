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
