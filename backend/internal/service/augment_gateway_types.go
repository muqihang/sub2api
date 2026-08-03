package service

type AugmentGatewayEndpoint string

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
	Index    *int                           `json:"index,omitempty"`
	ID       string                         `json:"id,omitempty"`
	Type     string                         `json:"type,omitempty"`
	Function AugmentGatewayToolCallFunction `json:"function"`
}

type AugmentGatewayToolCallFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type AugmentGatewayProviderRequest struct {
	Endpoint        string
	ConversationID  string
	RequestID       string
	AssistantTurnID string
	SessionHash     string

	Model AugmentGatewayModel

	APIKey       *APIKey
	User         *User
	Subscription *UserSubscription
	UserAgent    string
	IPAddress    string

	Account         *Account
	ProviderGroupID int64
	Provider        AugmentGatewayProvider
	ModelID         string
	UpstreamModel   string

	Messages []map[string]any
	Tools    []map[string]any
	RawBody  map[string]any
	Metadata map[string]any
}

type AugmentGatewayProviderUsage struct {
	InputTokens        int
	OutputTokens       int
	TotalTokens        int
	CachedInputTokens  int
	ReasoningTokens    int
	ProviderUsageExtra map[string]any
}

type AugmentGatewayProviderResult struct {
	Provider          AugmentGatewayProvider
	ModelID           string
	UpstreamModel     string
	RequestID         string
	UpstreamRequestID string

	Text                    string
	ReasoningContent        string
	ReasoningContentPresent bool
	ToolCalls               []AugmentGatewayToolCall
	Usage                   AugmentGatewayProviderUsage

	Raw      map[string]any
	Metadata map[string]any
}

type AugmentGatewayProviderChunk struct {
	Provider          AugmentGatewayProvider
	ModelID           string
	UpstreamModel     string
	RequestID         string
	UpstreamRequestID string

	TextDelta             string
	ReasoningContentDelta string
	ReasoningContentDone  bool
	ToolCallDelta         *AugmentGatewayToolCall
	Usage                 AugmentGatewayProviderUsage
	Done                  bool
	ProviderFinishReason  string
	NormalizedStopReason  string
	Raw                   map[string]any
	Metadata              map[string]any
}
