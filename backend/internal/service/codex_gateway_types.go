package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

type CodexGatewayModelsRequest struct {
	APIKey        *APIKey
	ClientVersion string
	CatalogFormat string
	ManagedDevice bool
}

type CodexGatewayResponsesRequest struct {
	APIKey         *APIKey
	Headers        http.Header
	Body           []byte
	StreamWriter   io.Writer
	ResponseHeader http.Header
	WriteStatus    func(int)
	Flush          func()
	CaptureTrace   *CodexGatewayTrace
	ManagedDevice  bool
}

type CodexGatewayServiceResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

type CodexGatewayModelCapabilities struct {
	Responses           bool `json:"responses"`
	Streaming           bool `json:"streaming"`
	ToolCalls           bool `json:"tool_calls"`
	ImageInput          bool `json:"image_input"`
	CachePricing        bool `json:"cache_pricing"`
	ContextContinuation bool `json:"context_continuation"`
}

type CodexGatewayModelPricing struct {
	InputPrice       *string `json:"input_price"`
	OutputPrice      *string `json:"output_price"`
	CachedInputPrice *string `json:"cached_input_price"`
	CacheWritePrice  *string `json:"cache_write_price"`
	Currency         string  `json:"currency"`
	Unit             string  `json:"unit"`
	UpdatedAt        *string `json:"updated_at"`
	Source           string  `json:"source"`
}

type CodexGatewayModel struct {
	Slug                          string                        `json:"slug"`
	DisplayName                   string                        `json:"display_name"`
	Origin                        string                        `json:"origin"`
	ProviderID                    string                        `json:"provider_id"`
	Capabilities                  CodexGatewayModelCapabilities `json:"capabilities"`
	Pricing                       *CodexGatewayModelPricing     `json:"pricing"`
	Provider                      string                        `json:"provider,omitempty"`
	ProviderVariant               string                        `json:"provider_variant,omitempty"`
	UpstreamModel                 string                        `json:"upstream_model,omitempty"`
	UpstreamBaseModel             string                        `json:"upstream_base_model,omitempty"`
	UpstreamThinkingModel         string                        `json:"upstream_thinking_model,omitempty"`
	Visibility                    string                        `json:"visibility"`
	SupportedInAPI                bool                          `json:"supported_in_api"`
	Priority                      int                           `json:"priority,omitempty"`
	DefaultReasoningLevel         string                        `json:"default_reasoning_level,omitempty"`
	SupportedReasoningLevels      []string                      `json:"supported_reasoning_levels,omitempty"`
	SupportVerbosity              bool                          `json:"support_verbosity"`
	SupportsParallelToolCalls     bool                          `json:"supports_parallel_tool_calls"`
	ContextWindow                 int                           `json:"context_window,omitempty"`
	AutoCompactTokenLimit         int                           `json:"auto_compact_token_limit,omitempty"`
	EffectiveContextWindowPercent int                           `json:"effective_context_window_percent,omitempty"`
	MaxOutputTokens               int                           `json:"max_output_tokens,omitempty"`
	InputModalities               []string                      `json:"input_modalities,omitempty"`
	SupportsImageDetailOriginal   bool                          `json:"supports_image_detail_original"`
	SupportsSearchTool            bool                          `json:"supports_search_tool"`
	ExperimentalSupportedTools    []string                      `json:"experimental_supported_tools,omitempty"`
	ShellType                     string                        `json:"shell_type,omitempty"`
	WebSearchToolType             string                        `json:"web_search_tool_type,omitempty"`
	ImageGenerationToolType       string                        `json:"image_generation_tool_type,omitempty"`
}

type CodexGatewayModelsResponse struct {
	Models []CodexGatewayModel `json:"models"`
}

type CodexGatewayResponsesCreateRequest struct {
	Model              string                     `json:"model"`
	Instructions       json.RawMessage            `json:"instructions,omitempty"`
	Input              json.RawMessage            `json:"input,omitempty"`
	Tools              json.RawMessage            `json:"tools,omitempty"`
	ToolChoice         json.RawMessage            `json:"tool_choice,omitempty"`
	Reasoning          json.RawMessage            `json:"reasoning,omitempty"`
	Text               json.RawMessage            `json:"text,omitempty"`
	Include            json.RawMessage            `json:"include,omitempty"`
	ClientMetadata     json.RawMessage            `json:"client_metadata,omitempty"`
	PromptCacheKey     string                     `json:"prompt_cache_key,omitempty"`
	ParallelToolCalls  *bool                      `json:"parallel_tool_calls,omitempty"`
	Store              *bool                      `json:"store,omitempty"`
	Stream             *bool                      `json:"stream,omitempty"`
	MaxOutputTokens    *int                       `json:"max_output_tokens,omitempty"`
	PreviousResponseID *string                    `json:"previous_response_id,omitempty"`
	RawFields          map[string]json.RawMessage `json:"-"`
}

type CodexGatewayResponse struct {
	ID                string                     `json:"id"`
	Object            string                     `json:"object"`
	Model             string                     `json:"model,omitempty"`
	Status            string                     `json:"status,omitempty"`
	Output            []json.RawMessage          `json:"output,omitempty"`
	Usage             json.RawMessage            `json:"usage,omitempty"`
	Error             *CodexGatewayResponseError `json:"error,omitempty"`
	IncompleteDetails json.RawMessage            `json:"incomplete_details,omitempty"`
	RawFields         map[string]json.RawMessage `json:"-"`
}

type CodexGatewayResponseError struct {
	Code      string                     `json:"code,omitempty"`
	Message   string                     `json:"message,omitempty"`
	RawFields map[string]json.RawMessage `json:"-"`
}

type CodexGatewayLocalShellAction struct {
	Type             string            `json:"type"`
	Command          []string          `json:"command"`
	TimeoutMS        *int64            `json:"timeout_ms"`
	WorkingDirectory *string           `json:"working_directory"`
	Env              map[string]string `json:"env"`
	User             *string           `json:"user"`
}

const (
	CodexGatewayToolKindFunction  = "function"
	CodexGatewayToolKindNamespace = "namespace"
	CodexGatewayToolKindCustom    = "custom"
	CodexGatewayToolKindHosted    = "hosted"
)

const (
	CodexGatewayOutputItemTypeFunctionCall   = "function_call"
	CodexGatewayOutputItemTypeCustomToolCall = "custom_tool_call"
	CodexGatewayOutputItemTypeLocalShellCall = "local_shell_call"
)

type CodexGatewayStateStoreConfig struct {
	TTL      time.Duration
	MaxItems int
	Now      func() time.Time
}

type CodexGatewayStateLookupKey struct {
	ResponseID    string `json:"response_id"`
	SessionKey    string `json:"session_key"`
	IsolationKey  string `json:"isolation_key"`
	Provider      string `json:"provider"`
	UpstreamModel string `json:"upstream_model"`
}

type CodexGatewayStoredToolCall struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Alias     string `json:"alias,omitempty"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type CodexGatewayToolNameMapEntry struct {
	Alias         string
	Kind          string
	Namespace     string
	NamespacePath string `json:"-"`
	Name          string
	FlattenedArgs []CodexGatewayToolArgumentPath `json:"-"`
}

type CodexGatewayToolMappingResult struct {
	Tools                  []map[string]any
	NameMap                map[string]CodexGatewayToolNameMapEntry
	IgnoredHostedToolTypes []string
	originalToAlias        map[string]string
}

type CodexGatewayStoredToolContext struct {
	ToolNameMap map[string]CodexGatewayToolNameMapEntry
	ToolSchemas []json.RawMessage
}

type CodexGatewayResponseState struct {
	Key                         CodexGatewayStateLookupKey              `json:"key"`
	AssistantContent            string                                  `json:"assistant_content,omitempty"`
	AssistantContentPresent     bool                                    `json:"assistant_content_present,omitempty"`
	ReasoningContent            string                                  `json:"reasoning_content,omitempty"`
	ReasoningContentPresent     bool                                    `json:"reasoning_content_present,omitempty"`
	ReasoningContentSynthesized bool                                    `json:"reasoning_content_synthesized,omitempty"`
	AnthropicThinkingBlocks     []json.RawMessage                       `json:"anthropic_thinking_blocks,omitempty"`
	ToolCalls                   []CodexGatewayStoredToolCall            `json:"tool_calls,omitempty"`
	ToolNameMap                 map[string]CodexGatewayToolNameMapEntry `json:"tool_name_map,omitempty"`
	ToolSchemas                 []json.RawMessage                       `json:"tool_schemas,omitempty"`
	ReplayMessages              []json.RawMessage                       `json:"replay_messages,omitempty"`
}

type CodexGatewayToolMappingConfig struct {
	EnableStrictBeta                bool
	RejectUnsupportedStrictSchemas  bool
	EnableDeepSeekSchemaFlattening  bool
	DisableDeepSeekSchemaFlattening bool
	DeepSeekFlattenMinDepth         int
	DeepSeekFlattenMinLeaves        int
}

type CodexGatewayToolArgumentPath struct {
	FlatKey string
	Path    []string
}

const (
	CodexGatewayDeepSeekImageInputModePlaceholder = "placeholder"
	CodexGatewayDeepSeekImageInputModeReject      = "reject"
)

type CodexGatewayDeepSeekRequestContext struct {
	SessionKey           string
	IsolationKey         string
	WorkspaceKey         string
	ManagedSessionBucket string
	UserID               string
	CaptureTrace         *CodexGatewayTrace
}

type CodexGatewayDeepSeekRequestConfig struct {
	ToolMappingConfig     CodexGatewayToolMappingConfig
	ImageInputMode        string
	AllowReasoningDisable bool
	HostedWebSearch       func(ctx context.Context, query string) (string, error)
	HostedImageVision     func(ctx context.Context, imageURL string) (string, error)
	HostedToolContext     *codexGatewayHostedToolContext
	StreamSequenceNumber  int
}

type CodexGatewayPreparedDeepSeekRequest struct {
	Body           map[string]any
	ToolNameMap    map[string]CodexGatewayToolNameMapEntry
	ToolSchemas    []json.RawMessage
	ReplayMessages []json.RawMessage
}

type CodexGatewayAnthropicRequestContext struct {
	SessionKey   string
	IsolationKey string
	CaptureTrace *CodexGatewayTrace
}

type CodexGatewayAnthropicRequestConfig struct {
	ToolMappingConfig    CodexGatewayToolMappingConfig
	ImageInputMode       string
	CacheTTL             string
	ForceDisableThinking bool
	HostedWebSearch      func(ctx context.Context, query string) (string, error)
	HostedToolContext    *codexGatewayHostedToolContext
	StreamSequenceNumber int
}

type CodexGatewayPreparedAnthropicRequest struct {
	Body           map[string]any
	ToolNameMap    map[string]CodexGatewayToolNameMapEntry
	ReplayMessages []json.RawMessage
}

type CodexGatewayProviderUsage struct {
	InputTokens              int
	OutputTokens             int
	TotalTokens              int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
	CacheCreation5mTokens    int
	CacheCreation1hTokens    int
	ProviderUsageExtra       map[string]any
}

type CodexGatewayProviderResult struct {
	ResponseID              string
	UpstreamRequestID       string
	UpstreamModel           string
	Response                CodexGatewayResponse
	Usage                   CodexGatewayProviderUsage
	ReasoningContent        string
	ReasoningContentPresent bool
	ToolCalls               []CodexGatewayStoredToolCall
}

type CodexGatewayDeepSeekAdapterResult struct {
	ServiceResponse CodexGatewayServiceResponse
	ProviderResult  CodexGatewayProviderResult
}
