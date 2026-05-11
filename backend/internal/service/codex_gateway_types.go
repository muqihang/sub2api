package service

import (
	"encoding/json"
	"net/http"
)

type CodexGatewayModelsRequest struct {
	APIKey        *APIKey
	ClientVersion string
}

type CodexGatewayResponsesRequest struct {
	APIKey  *APIKey
	Headers http.Header
	Body    []byte
}

type CodexGatewayServiceResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

type CodexGatewayModel struct {
	Slug                       string   `json:"slug"`
	DisplayName                string   `json:"display_name"`
	Provider                   string   `json:"provider,omitempty"`
	UpstreamModel              string   `json:"upstream_model,omitempty"`
	Visibility                 string   `json:"visibility"`
	SupportedInAPI             bool     `json:"supported_in_api"`
	Priority                   int      `json:"priority,omitempty"`
	DefaultReasoningLevel      string   `json:"default_reasoning_level,omitempty"`
	SupportedReasoningLevels   []string `json:"supported_reasoning_levels,omitempty"`
	SupportVerbosity           bool     `json:"support_verbosity"`
	SupportsParallelToolCalls  bool     `json:"supports_parallel_tool_calls"`
	ContextWindow              int      `json:"context_window,omitempty"`
	AutoCompactTokenLimit      int      `json:"auto_compact_token_limit,omitempty"`
	MaxOutputTokens            int      `json:"max_output_tokens,omitempty"`
	InputModalities            []string `json:"input_modalities,omitempty"`
	SupportsImageDetailOriginal bool    `json:"supports_image_detail_original"`
	SupportsSearchTool         bool     `json:"supports_search_tool"`
	ExperimentalSupportedTools []string `json:"experimental_supported_tools,omitempty"`
	ShellType                  string   `json:"shell_type,omitempty"`
	WebSearchToolType          string   `json:"web_search_tool_type,omitempty"`
	ImageGenerationToolType    string   `json:"image_generation_tool_type,omitempty"`
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
	ID                string            `json:"id"`
	Object            string            `json:"object"`
	Model             string            `json:"model,omitempty"`
	Status            string            `json:"status,omitempty"`
	Output            []json.RawMessage `json:"output,omitempty"`
	Usage             json.RawMessage   `json:"usage,omitempty"`
	Error             *CodexGatewayResponseError `json:"error,omitempty"`
	IncompleteDetails json.RawMessage   `json:"incomplete_details,omitempty"`
	RawFields         map[string]json.RawMessage `json:"-"`
}

type CodexGatewayResponseError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	RawFields map[string]json.RawMessage `json:"-"`
}
