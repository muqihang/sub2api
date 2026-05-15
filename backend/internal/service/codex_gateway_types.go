package service

import (
	"encoding/json"
	"io"
	"net/http"
	"time"
)

type CodexGatewayModelsRequest struct {
	APIKey        *APIKey
	ClientVersion string
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
}

type CodexGatewayServiceResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

type CodexGatewayModel struct {
	Slug                        string   `json:"slug"`
	DisplayName                 string   `json:"display_name"`
	Provider                    string   `json:"provider,omitempty"`
	UpstreamModel               string   `json:"upstream_model,omitempty"`
	Visibility                  string   `json:"visibility"`
	SupportedInAPI              bool     `json:"supported_in_api"`
	Priority                    int      `json:"priority,omitempty"`
	DefaultReasoningLevel       string   `json:"default_reasoning_level,omitempty"`
	SupportedReasoningLevels    []string `json:"supported_reasoning_levels,omitempty"`
	SupportVerbosity            bool     `json:"support_verbosity"`
	SupportsParallelToolCalls   bool     `json:"supports_parallel_tool_calls"`
	ContextWindow               int      `json:"context_window,omitempty"`
	AutoCompactTokenLimit       int      `json:"auto_compact_token_limit,omitempty"`
	MaxOutputTokens             int      `json:"max_output_tokens,omitempty"`
	InputModalities             []string `json:"input_modalities,omitempty"`
	SupportsImageDetailOriginal bool     `json:"supports_image_detail_original"`
	SupportsSearchTool          bool     `json:"supports_search_tool"`
	ExperimentalSupportedTools  []string `json:"experimental_supported_tools,omitempty"`
	ShellType                   string   `json:"shell_type,omitempty"`
	WebSearchToolType           string   `json:"web_search_tool_type,omitempty"`
	ImageGenerationToolType     string   `json:"image_generation_tool_type,omitempty"`
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

const (
	CodexGatewayToolKindFunction  = "function"
	CodexGatewayToolKindNamespace = "namespace"
	CodexGatewayToolKindCustom    = "custom"
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
}

type CodexGatewayToolMappingResult struct {
	Tools                  []map[string]any
	NameMap                map[string]CodexGatewayToolNameMapEntry
	IgnoredHostedToolTypes []string
	originalToAlias        map[string]string
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
	ReplayMessages              []json.RawMessage                       `json:"replay_messages,omitempty"`
}

type CodexGatewayToolMappingConfig struct {
	EnableStrictBeta               bool
	RejectUnsupportedStrictSchemas bool
}

const (
	CodexGatewayDeepSeekImageInputModePlaceholder = "placeholder"
	CodexGatewayDeepSeekImageInputModeReject      = "reject"
)

type CodexGatewayDeepSeekRequestContext struct {
	SessionKey   string
	IsolationKey string
	UserID       string
	CaptureTrace *CodexGatewayTrace
}

type CodexGatewayDeepSeekRequestConfig struct {
	ToolMappingConfig     CodexGatewayToolMappingConfig
	ImageInputMode        string
	AllowReasoningDisable bool
}

type CodexGatewayPreparedDeepSeekRequest struct {
	Body        map[string]any
	ToolNameMap map[string]CodexGatewayToolNameMapEntry
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
