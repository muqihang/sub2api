package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAugmentGatewayProviderExecutor_OpenAISelectsConfiguredGroup(t *testing.T) {
	selector := &augmentGatewayProviderExecutorFakeSelector{
		account: &Account{ID: 101, Platform: PlatformOpenAI},
	}
	adapter := &augmentGatewayProviderExecutorFakeAdapter{
		result: AugmentGatewayProviderResult{Text: "ok"},
	}
	executor := newAugmentGatewayProviderExecutorTestSubject(selector, nil, nil, adapter, nil, nil)

	result, err := executor.Complete(context.Background(), AugmentGatewayProviderRequest{
		Endpoint:       "/chat",
		ConversationID: "conv-openai",
		RequestID:      "req-openai",
		SessionHash:    "session-openai",
		Model: AugmentGatewayModel{
			ID:            "gpt-5.4",
			Provider:      AugmentGatewayProviderOpenAI,
			UpstreamModel: "gpt-5.4",
		},
		RawBody: map[string]any{"messages": []any{}},
	})

	require.NoError(t, err)
	require.Equal(t, int64(1001), selector.calls[0].groupID)
	require.Equal(t, "session-openai", selector.calls[0].sessionHash)
	require.Equal(t, "gpt-5.4", selector.calls[0].modelID)
	require.Same(t, selector.account, adapter.completeRequests[0].Account)
	require.Equal(t, AugmentGatewayProviderOpenAI, result.Provider)
	require.Equal(t, "gpt-5.4", result.ModelID)
}

func TestAugmentGatewayProviderExecutor_DeepSeekSelectsConfiguredGroupAndSanitizes(t *testing.T) {
	selector := &augmentGatewayProviderExecutorFakeSelector{
		account: &Account{ID: 202, Platform: PlatformOpenAI},
	}
	adapter := &augmentGatewayProviderExecutorFakeAdapter{
		result: AugmentGatewayProviderResult{Text: "ok"},
	}
	executor := newAugmentGatewayProviderExecutorTestSubject(nil, selector, nil, nil, adapter, nil)

	_, err := executor.Complete(context.Background(), AugmentGatewayProviderRequest{
		Endpoint:       "/chat",
		ConversationID: "conv-deepseek",
		RequestID:      "req-deepseek",
		SessionHash:    "session-deepseek",
		Model: AugmentGatewayModel{
			ID:              "deepseek-v4-pro",
			Provider:        AugmentGatewayProviderDeepSeek,
			UpstreamModel:   "deepseek-v4-pro",
			ReasoningEffort: "max",
		},
		RawBody: map[string]any{
			"model":       "wrong-model",
			"tool_choice": "auto",
			"tools": []any{
				map[string]any{"type": "function"},
			},
			"messages": []any{
				map[string]any{
					"role": "assistant",
					"tool_calls": []any{
						map[string]any{
							"id":   "tool-1",
							"type": "function",
							"function": map[string]any{
								"name":      "read_file",
								"arguments": "{}",
							},
						},
					},
				},
				map[string]any{
					"role":              "assistant",
					"content":           nil,
					"reasoning_content": nil,
					"tool_calls": []any{
						map[string]any{
							"id":   "tool-2",
							"type": "function",
						},
					},
				},
			},
		},
	})

	require.NoError(t, err)
	require.Equal(t, int64(1002), selector.calls[0].groupID)
	require.Equal(t, "deepseek-v4-pro", selector.calls[0].modelID)

	body := adapter.completeRequests[0].RawBody
	require.Equal(t, "deepseek-v4-pro", body["model"])
	require.Equal(t, map[string]any{"type": "enabled"}, body["thinking"])
	require.Equal(t, "max", body["reasoning_effort"])
	require.NotContains(t, body, "tool_choice")

	messages := body["messages"].([]any)
	firstAssistant := messages[0].(map[string]any)
	require.Equal(t, "", firstAssistant["content"])
	require.Equal(t, "", firstAssistant["reasoning_content"])

	secondAssistant := messages[1].(map[string]any)
	require.Equal(t, "", secondAssistant["content"])
	require.Equal(t, "", secondAssistant["reasoning_content"])
}

func TestAugmentGatewayProviderExecutor_ClaudeSelectsConfiguredAnthropicGroup(t *testing.T) {
	selector := &augmentGatewayProviderExecutorFakeSelector{
		account: &Account{ID: 303, Platform: PlatformAnthropic},
	}
	adapter := &augmentGatewayProviderExecutorFakeAdapter{
		result: AugmentGatewayProviderResult{Text: "ok"},
	}
	executor := newAugmentGatewayProviderExecutorTestSubject(nil, nil, selector, nil, nil, adapter)

	_, err := executor.Complete(context.Background(), AugmentGatewayProviderRequest{
		Endpoint:    "/chat",
		RequestID:   "req-claude",
		SessionHash: "session-claude",
		Model: AugmentGatewayModel{
			ID:            "claude-sonnet-4-5",
			Provider:      AugmentGatewayProviderAnthropic,
			UpstreamModel: "claude-sonnet-4-5",
		},
		RawBody: map[string]any{},
	})

	require.NoError(t, err)
	require.Equal(t, int64(1003), selector.calls[0].groupID)
	require.Equal(t, "claude-sonnet-4-5", selector.calls[0].modelID)
	require.Same(t, selector.account, adapter.completeRequests[0].Account)
}

func TestAugmentGatewayProviderExecutor_GeminiSelectsConfiguredGeminiGroup(t *testing.T) {
	selector := &augmentGatewayProviderExecutorFakeSelector{
		account: &Account{ID: 404, Platform: PlatformGemini},
	}
	adapter := &augmentGatewayProviderExecutorFakeAdapter{
		result: AugmentGatewayProviderResult{Text: "ok"},
	}
	executor := newAugmentGatewayProviderExecutorTestSubject(nil, nil, nil, nil, nil, adapter)
	executor.geminiSelector = selector

	_, err := executor.Complete(context.Background(), AugmentGatewayProviderRequest{
		Endpoint:    "/chat",
		RequestID:   "req-gemini",
		SessionHash: "session-gemini",
		Model: AugmentGatewayModel{
			ID:            "gemini-2.5-pro",
			Provider:      AugmentGatewayProviderGemini,
			UpstreamModel: "gemini-2.5-pro",
		},
		RawBody: map[string]any{},
	})

	require.NoError(t, err)
	require.Equal(t, int64(1004), selector.calls[0].groupID)
	require.Equal(t, "gemini-2.5-pro", selector.calls[0].modelID)
	require.Same(t, selector.account, adapter.completeRequests[0].Account)
}

func TestAugmentGatewayProviderExecutor_DoesNotUseRouteMiddleware(t *testing.T) {
	selector := &augmentGatewayProviderExecutorFakeSelector{
		account: &Account{ID: 505, Platform: PlatformOpenAI},
	}
	adapter := &augmentGatewayProviderExecutorFakeAdapter{
		result: AugmentGatewayProviderResult{Text: "ok"},
	}
	executor := newAugmentGatewayProviderExecutorTestSubject(selector, nil, nil, adapter, nil, nil)

	_, err := executor.Complete(context.Background(), AugmentGatewayProviderRequest{
		Endpoint: "/chat",
		Model: AugmentGatewayModel{
			ID:            "gpt-5.4",
			Provider:      AugmentGatewayProviderOpenAI,
			UpstreamModel: "gpt-5.4",
		},
		RawBody: map[string]any{},
	})

	require.NoError(t, err)
	require.Len(t, selector.calls, 1)
	require.Len(t, adapter.completeRequests, 1)
	require.False(t, adapter.sawGinContext)
}

func TestAugmentGatewayProviderExecutor_NormalizesProviderResults(t *testing.T) {
	selector := &augmentGatewayProviderExecutorFakeSelector{
		account: &Account{ID: 606, Platform: PlatformOpenAI},
	}
	adapter := &augmentGatewayProviderExecutorFakeAdapter{
		result: AugmentGatewayProviderResult{
			Text:                    "normalized text",
			ReasoningContent:        "",
			ReasoningContentPresent: true,
			Usage: AugmentGatewayProviderUsage{
				InputTokens:  7,
				OutputTokens: 11,
				TotalTokens:  18,
			},
			Raw: map[string]any{"id": "upstream-raw"},
		},
		streamChunks: []AugmentGatewayProviderChunk{
			{TextDelta: "hel"},
			{TextDelta: "lo", Done: true},
		},
	}
	executor := newAugmentGatewayProviderExecutorTestSubject(selector, nil, nil, adapter, nil, nil)
	req := AugmentGatewayProviderRequest{
		Endpoint:    "/chat",
		RequestID:   "req-normalize",
		SessionHash: "session-normalize",
		Model: AugmentGatewayModel{
			ID:            "gpt-5.4",
			Provider:      AugmentGatewayProviderOpenAI,
			UpstreamModel: "gpt-5.4-upstream",
		},
		RawBody: map[string]any{},
	}

	result, err := executor.Complete(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, AugmentGatewayProviderOpenAI, result.Provider)
	require.Equal(t, "gpt-5.4", result.ModelID)
	require.Equal(t, "gpt-5.4-upstream", result.UpstreamModel)
	require.Equal(t, "req-normalize", result.RequestID)
	require.Equal(t, "normalized text", result.Text)
	require.True(t, result.ReasoningContentPresent)
	require.Equal(t, 18, result.Usage.TotalTokens)

	var chunks []AugmentGatewayProviderChunk
	err = executor.Stream(context.Background(), req, func(chunk AugmentGatewayProviderChunk) error {
		chunks = append(chunks, chunk)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"hel", "lo"}, []string{chunks[0].TextDelta, chunks[1].TextDelta})
	require.Equal(t, AugmentGatewayProviderOpenAI, chunks[0].Provider)
	require.Equal(t, "gpt-5.4", chunks[0].ModelID)
	require.Equal(t, "gpt-5.4-upstream", chunks[0].UpstreamModel)
	require.Equal(t, "req-normalize", chunks[0].RequestID)
	require.True(t, chunks[1].Done)
}

func newAugmentGatewayProviderExecutorTestSubject(
	openAISelector augmentGatewayAccountSelector,
	deepSeekSelector augmentGatewayAccountSelector,
	anthropicSelector augmentGatewayAccountSelector,
	openAIAdapter augmentGatewayProviderAdapter,
	deepSeekAdapter augmentGatewayProviderAdapter,
	anthropicOrGeminiAdapter augmentGatewayProviderAdapter,
) *AugmentGatewayProviderExecutorImpl {
	return &AugmentGatewayProviderExecutorImpl{
		cfg: &config.Config{
			Gateway: config.GatewayConfig{
				Augment: config.GatewayAugmentConfig{
					Enabled: true,
					ProviderGroups: config.GatewayAugmentProviderGroupsConfig{
						OpenAI:    1001,
						DeepSeek:  1002,
						Anthropic: 1003,
						Gemini:    1004,
					},
				},
			},
		},
		openAISelector:    openAISelector,
		deepSeekSelector:  deepSeekSelector,
		anthropicSelector: anthropicSelector,
		openAIAdapter:     openAIAdapter,
		deepSeekAdapter:   deepSeekAdapter,
		anthropicAdapter:  anthropicOrGeminiAdapter,
		geminiAdapter:     anthropicOrGeminiAdapter,
		turnStore:         NewAugmentGatewayReasoningTurnStore(),
	}
}

type augmentGatewayProviderExecutorFakeSelector struct {
	account *Account
	err     error
	calls   []augmentGatewayProviderExecutorFakeSelectorCall
}

type augmentGatewayProviderExecutorFakeSelectorCall struct {
	groupID     int64
	sessionHash string
	modelID     string
}

func (s *augmentGatewayProviderExecutorFakeSelector) SelectAccountForModel(ctx context.Context, groupID *int64, sessionHash string, requestedModel string) (*Account, error) {
	if groupID == nil {
		s.calls = append(s.calls, augmentGatewayProviderExecutorFakeSelectorCall{
			sessionHash: sessionHash,
			modelID:     requestedModel,
		})
	} else {
		s.calls = append(s.calls, augmentGatewayProviderExecutorFakeSelectorCall{
			groupID:     *groupID,
			sessionHash: sessionHash,
			modelID:     requestedModel,
		})
	}
	if s.err != nil {
		return nil, s.err
	}
	if s.account == nil {
		return nil, errors.New("missing fake account")
	}
	return s.account, nil
}

type augmentGatewayProviderExecutorFakeAdapter struct {
	result           AugmentGatewayProviderResult
	err              error
	streamChunks     []AugmentGatewayProviderChunk
	completeRequests []AugmentGatewayProviderRequest
	streamRequests   []AugmentGatewayProviderRequest
	sawGinContext    bool
}

func (a *augmentGatewayProviderExecutorFakeAdapter) Complete(ctx context.Context, req AugmentGatewayProviderRequest) (AugmentGatewayProviderResult, error) {
	a.completeRequests = append(a.completeRequests, req)
	return a.result, a.err
}

func (a *augmentGatewayProviderExecutorFakeAdapter) Stream(ctx context.Context, req AugmentGatewayProviderRequest, emit func(AugmentGatewayProviderChunk) error) error {
	a.streamRequests = append(a.streamRequests, req)
	if a.err != nil {
		return a.err
	}
	for _, chunk := range a.streamChunks {
		if err := emit(chunk); err != nil {
			return err
		}
	}
	return nil
}
