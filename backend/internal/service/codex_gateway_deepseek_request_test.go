package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCodexGatewayDeepSeekRequest_BuildsMessagesToolsAndUserID(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model:        "deepseek-v4-pro",
		Instructions: json.RawMessage(`"Act as a precise coding assistant."`),
		Input: json.RawMessage(`[
			{
				"type":"message",
				"role":"user",
				"content":[
					{"type":"input_text","text":"inspect the repository"},
					{"type":"input_image","image_url":"data:image/png;base64,AAAA"}
				]
			}
		]`),
		Tools: json.RawMessage(`[
			{"type":"function","name":"shell","description":"run shell","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}
		]`),
		Reasoning:       json.RawMessage(`{"effort":"low"}`),
		ToolChoice:      json.RawMessage(`"auto"`),
		MaxOutputTokens: intPtr(512),
		RawFields: map[string]json.RawMessage{
			"temperature":       json.RawMessage(`0.7`),
			"top_p":             json.RawMessage(`0.9`),
			"presence_penalty":  json.RawMessage(`0.2`),
			"frequency_penalty": json.RawMessage(`0.1`),
		},
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{
		ImageInputMode: CodexGatewayDeepSeekImageInputModePlaceholder,
	})
	require.NoError(t, err)

	require.Equal(t, "deepseek-v4-pro", prepared.Body["model"])
	require.Equal(t, map[string]any{"type": "enabled"}, prepared.Body["thinking"])
	require.Equal(t, "high", prepared.Body["reasoning_effort"])
	require.Equal(t, float64(512), prepared.Body["max_tokens"])
	require.NotContains(t, prepared.Body, "tool_choice")
	require.NotContains(t, prepared.Body, "temperature")
	require.NotContains(t, prepared.Body, "top_p")

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 2)
	require.Equal(t, "system", messages[0].(map[string]any)["role"])
	require.Equal(t, "user", messages[1].(map[string]any)["role"])
	require.Contains(t, messages[1].(map[string]any)["content"].(string), "inspect the repository")
	require.Contains(t, messages[1].(map[string]any)["content"].(string), "input_image")

	tools := prepared.Body["tools"].([]any)
	require.Len(t, tools, 1)
	require.Equal(t, "shell", tools[0].(map[string]any)["function"].(map[string]any)["name"])

	userID := prepared.Body["user_id"].(string)
	require.True(t, regexp.MustCompile(`^[A-Za-z0-9_-]{1,512}$`).MatchString(userID))
}

func TestCodexGatewayDeepSeekRequest_FlattensDeepSchemasAndAddsDeepSeekToolHints(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"use tools carefully"}]}
		]`),
		Tools: json.RawMessage(`[
			{
				"type":"function",
				"name":"exec_command",
				"description":"run shell commands",
				"parameters":{
					"type":"object",
					"properties":{
						"cmd":{"type":"string"},
						"workspace":{
							"type":"object",
							"properties":{
								"root":{"type":"string"},
								"filters":{
									"type":"object",
									"properties":{
										"include":{"type":"string"},
										"exclude":{"type":"string"}
									},
									"required":["include"]
								}
							},
							"required":["root","filters"]
						}
					},
					"required":["cmd","workspace"]
				}
			},
			{
				"type":"function",
				"name":"python",
				"description":"run python",
				"parameters":{"type":"object","properties":{"code":{"type":"string"}}}
			},
			{
				"type":"custom",
				"name":"apply_patch",
				"description":"edit files",
				"format":{"type":"grammar","syntax":"lark","definition":"start: begin_patch hunk+ end_patch"}
			}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_flatten",
		IsolationKey: "iso_flatten",
	}, CodexGatewayDeepSeekRequestConfig{
		ToolMappingConfig: CodexGatewayToolMappingConfig{
			EnableDeepSeekSchemaFlattening: true,
			DeepSeekFlattenMinDepth:        3,
			DeepSeekFlattenMinLeaves:       4,
		},
	})
	require.NoError(t, err)

	tools := prepared.Body["tools"].([]any)
	require.Len(t, tools, 3)

	execTool := deepSeekRequestToolFunctionByName(t, tools, "exec_command")
	require.Contains(t, execTool["description"], "`cmd`")
	require.Contains(t, execTool["description"], "python3 - <<'PY'")
	execParams := execTool["parameters"].(map[string]any)
	execProperties := execParams["properties"].(map[string]any)
	require.Contains(t, execProperties, "workspace.root")
	require.Contains(t, execProperties, "workspace.filters.include")
	require.Contains(t, execProperties, "workspace.filters.exclude")
	require.NotContains(t, execProperties, "workspace")
	require.ElementsMatch(t, []any{"cmd", "workspace.root", "workspace.filters.include"}, execParams["required"].([]any))

	pythonTool := deepSeekRequestToolFunctionByName(t, tools, "python")
	require.Contains(t, pythonTool["description"], "python3 - <<'PY'")

	patchTool := deepSeekRequestToolFunctionByDescription(t, tools, "custom input field")
	require.Contains(t, patchTool["description"], "custom input field")
	require.Contains(t, patchTool["description"], "exact raw input")
}

func TestCodexGatewayDeepSeekRequest_AllowsDisablingSchemaFlatteningAndAvoidsFlatKeyCollisions(t *testing.T) {
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"use tool"}]}
		]`),
		Tools: json.RawMessage(`[
			{
				"type":"function",
				"name":"search_repo",
				"parameters":{
					"type":"object",
					"properties":{
						"query":{"type":"string"},
						"workspace":{
							"type":"object",
							"properties":{
								"root":{"type":"string"},
								"filters":{
									"type":"object",
									"properties":{"include":{"type":"string"}}
								}
							}
						}
					}
				}
			}
		]`),
	}

	disabled, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_no_flatten",
		IsolationKey: "iso_no_flatten",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	disabledProps := disabled.Body["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)["parameters"].(map[string]any)["properties"].(map[string]any)
	require.Contains(t, disabledProps, "workspace")
	require.NotContains(t, disabledProps, "workspace.root")

	collisionReq := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: req.Input,
		Tools: json.RawMessage(`[
			{
				"type":"function",
				"name":"collision_tool",
				"parameters":{
					"type":"object",
					"properties":{
						"a.b":{"type":"string"},
						"a":{"type":"object","properties":{"b":{"type":"string"}}, "required":["b"]},
						"workspace":{
							"type":"object",
							"properties":{
								"root":{"type":"string"},
								"filters":{
									"type":"object",
									"properties":{
										"include":{"type":"string"},
										"exclude":{"type":"string"}
									}
								}
							}
						}
					}
				}
			}
		]`),
	}
	collisionPrepared, err := BuildCodexGatewayDeepSeekRequest(model, collisionReq, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_collision",
		IsolationKey: "iso_collision",
	}, CodexGatewayDeepSeekRequestConfig{
		ToolMappingConfig: CodexGatewayToolMappingConfig{
			EnableDeepSeekSchemaFlattening: true,
			DeepSeekFlattenMinDepth:        3,
			DeepSeekFlattenMinLeaves:       4,
		},
	})
	require.NoError(t, err)
	collisionProps := collisionPrepared.Body["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)["parameters"].(map[string]any)["properties"].(map[string]any)
	require.Contains(t, collisionProps, "a")
	require.Contains(t, collisionProps, "a.b")
	require.NotContains(t, collisionProps, "workspace.root")
}

func TestCodexGatewayDeepSeekRequest_StrictBetaIsOptInAndOnlySimpleSchemasQualify(t *testing.T) {
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	simpleReq := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"use strict tool"}]}
		]`),
		Tools: json.RawMessage(`[
			{
				"type":"function",
				"name":"weather",
				"parameters":{
					"type":"object",
					"properties":{
						"city":{"type":"string"},
						"units":{"type":"string"}
					},
					"required":["city"]
				},
				"strict":true
			}
		]`),
	}

	defaultPrepared, err := BuildCodexGatewayDeepSeekRequest(model, simpleReq, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_strict_default",
		IsolationKey: "iso_strict_default",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	defaultFn := defaultPrepared.Body["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)
	require.NotContains(t, defaultFn, "strict")

	strictPrepared, err := BuildCodexGatewayDeepSeekRequest(model, simpleReq, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_strict_enabled",
		IsolationKey: "iso_strict_enabled",
	}, CodexGatewayDeepSeekRequestConfig{
		ToolMappingConfig: CodexGatewayToolMappingConfig{
			EnableStrictBeta: true,
		},
	})
	require.NoError(t, err)
	strictFn := strictPrepared.Body["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)
	require.Equal(t, true, strictFn["strict"])

	complexReq := CodexGatewayResponsesCreateRequest{
		Model: simpleReq.Model,
		Input: simpleReq.Input,
		Tools: json.RawMessage(`[
			{
				"type":"function",
				"name":"complex_search",
				"parameters":{
					"type":"object",
					"properties":{
						"workspace":{
							"type":"object",
							"properties":{
								"root":{"type":"string"},
								"filters":{
									"type":"object",
									"properties":{
										"include":{"type":"string"},
										"exclude":{"type":"string"}
									}
								}
							}
						}
					}
				},
				"strict":true
			},
			{
				"type":"custom",
				"name":"apply_patch",
				"strict":true,
				"format":{"type":"grammar","syntax":"lark","definition":"start: begin_patch hunk+ end_patch"}
			}
		]`),
	}
	complexPrepared, err := BuildCodexGatewayDeepSeekRequest(model, complexReq, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_strict_complex",
		IsolationKey: "iso_strict_complex",
	}, CodexGatewayDeepSeekRequestConfig{
		ToolMappingConfig: CodexGatewayToolMappingConfig{
			EnableStrictBeta: true,
		},
	})
	require.NoError(t, err)
	complexTools := complexPrepared.Body["tools"].([]any)
	require.NotContains(t, complexTools[0].(map[string]any)["function"].(map[string]any), "strict")
	require.NotContains(t, complexTools[1].(map[string]any)["function"].(map[string]any), "strict")
}

func TestCodexGatewayDeepSeekRequestWithVisionProxy_RewritesImageToHostedVisionText(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{
				"type":"message",
				"role":"user",
				"content":[
					{"type":"input_text","text":"请看这张图"},
					{"type":"input_image","image_url":"data:image/png;base64,AAAA"}
				]
			}
		]`),
	}
	cfg := CodexGatewayDeepSeekRequestConfig{
		HostedImageVision: func(ctx context.Context, imageURL string) (string, error) {
			require.Equal(t, "data:image/png;base64,AAAA", imageURL)
			return "这是一张终端截图，主要内容是目录树。", nil
		},
	}

	rewritten, err := codexGatewayDeepSeekRequestWithHostedVision(context.Background(), req, cfg)
	require.NoError(t, err)
	require.JSONEq(t, `[
		{
			"type":"message",
			"role":"user",
			"content":[
				{"type":"input_text","text":"请看这张图"},
				{"type":"input_text","text":"[hosted_image_vision]\n这是一张终端截图，主要内容是目录树。"}
			]
		}
	]`, string(rewritten.Input))
}

func TestCodexGatewayDeepSeekRequestWithVisionProxy_SkipsRequestsWithoutImages(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{
				"type":"message",
				"role":"user",
				"content":[
					{"type":"input_text","text":"只是一段文字"}
				]
			}
		]`),
	}
	called := false
	cfg := CodexGatewayDeepSeekRequestConfig{
		HostedImageVision: func(ctx context.Context, imageURL string) (string, error) {
			called = true
			return "unexpected", nil
		},
	}

	rewritten, err := codexGatewayDeepSeekRequestWithHostedVision(context.Background(), req, cfg)
	require.NoError(t, err)
	require.False(t, called)
	require.JSONEq(t, string(req.Input), string(rewritten.Input))
}

func TestCodexGatewayDeepSeekRequestWithVisionProxy_FallsBackToPlaceholderOnVisionError(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{
				"type":"message",
				"role":"user",
				"content":[
					{"type":"input_text","text":"请看这张图"},
					{"type":"input_image","image_url":"data:image/png;base64,AAAA"}
				]
			}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}
	cfg := CodexGatewayDeepSeekRequestConfig{
		ImageInputMode: CodexGatewayDeepSeekImageInputModePlaceholder,
		HostedImageVision: func(ctx context.Context, imageURL string) (string, error) {
			return "", errors.New("vision unavailable")
		},
	}

	rewritten, err := codexGatewayDeepSeekRequestWithHostedVision(context.Background(), req, cfg)
	require.NoError(t, err)
	prepared, err := BuildCodexGatewayDeepSeekRequest(model, rewritten, nil, CodexGatewayDeepSeekRequestContext{}, cfg)
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 1)
	require.Contains(t, messages[0].(map[string]any)["content"].(string), codexGatewayDeepSeekImagePlaceholder())
}

func TestCodexGatewayDeepSeekRequest_KeepsWebSearchBridgeAndFiltersUnsupportedHostedTools(t *testing.T) {
	parallel := true
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"reply ok"}]}
		]`),
		Tools: json.RawMessage(`[
			{"type":"web_search"},
			{"type":"tool_search","parameters":{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer"}},"required":["query"]}},
			{"type":"image_generation"},
			{"type":"computer_use_preview"},
			{"type":"file_search"},
			{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}
		]`),
		ToolChoice:        json.RawMessage(`"auto"`),
		ParallelToolCalls: &parallel,
	}
	model := CodexGatewayModel{
		Slug:                      "deepseek-v4-pro",
		Provider:                  "deepseek",
		UpstreamModel:             "deepseek-v4-pro",
		SupportsParallelToolCalls: false,
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	require.NotContains(t, prepared.Body, "parallel_tool_calls")
	require.NotContains(t, prepared.Body, "tool_choice")
	require.Contains(t, prepared.Body["messages"].([]any)[0].(map[string]any)["content"], "OpenAI hosted tools are not available")
	require.Contains(t, prepared.Body["messages"].([]any)[0].(map[string]any)["content"], "image_generation")
	require.Contains(t, prepared.Body["messages"].([]any)[0].(map[string]any)["content"], "computer_use_preview")
	require.Contains(t, prepared.Body["messages"].([]any)[0].(map[string]any)["content"], "file_search")
	require.NotContains(t, prepared.Body["messages"].([]any)[0].(map[string]any)["content"], "web_search")

	tools := prepared.Body["tools"].([]any)
	require.Len(t, tools, 3)
	require.NotNil(t, deepSeekRequestToolFunctionByName(t, tools, "web_search"))
	require.NotNil(t, deepSeekRequestToolFunctionByName(t, tools, "tool_search"))
	require.NotNil(t, deepSeekRequestToolFunctionByName(t, tools, "exec_command"))
}

func TestCodexGatewayDeepSeekRequest_StripsParallelToolCallsEvenWhenModelSupportsIt(t *testing.T) {
	parallel := true
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"reply ok"}]}
		]`),
		ParallelToolCalls: &parallel,
	}
	model := CodexGatewayModel{
		Slug:                      "deepseek-v4-pro",
		Provider:                  "deepseek",
		UpstreamModel:             "deepseek-v4-pro",
		SupportsParallelToolCalls: true,
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_parallel",
		IsolationKey: "user_parallel",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	require.NotContains(t, prepared.Body, "parallel_tool_calls")
}

func TestCodexGatewayDeepSeekRequest_DefaultUserIDIsStableAcrossSessionsWithinIsolation(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"reply ok"}]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	first, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_alpha",
		IsolationKey: "api_key_1",
		WorkspaceKey: "workspace_alpha",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	second, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_beta",
		IsolationKey: "api_key_1",
		WorkspaceKey: "workspace_alpha",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	third, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_alpha",
		IsolationKey: "api_key_2",
		WorkspaceKey: "workspace_alpha",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	require.Equal(t, first.Body["user_id"], second.Body["user_id"])
	require.NotEqual(t, first.Body["user_id"], third.Body["user_id"])
}

func TestCodexGatewayDeepSeekRequest_DefaultUserIDUsesWorkspaceScopeAndOptionalManagedSessionBucket(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"reply ok"}]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	baseA, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_alpha",
		IsolationKey: "api_key_1",
		WorkspaceKey: "workspace_alpha",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	baseB, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_beta",
		IsolationKey: "api_key_1",
		WorkspaceKey: "workspace_alpha",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	otherWorkspace, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_alpha",
		IsolationKey: "api_key_1",
		WorkspaceKey: "workspace_beta",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	bucketA, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:           "session_alpha",
		IsolationKey:         "api_key_1",
		WorkspaceKey:         "workspace_alpha",
		ManagedSessionBucket: "managed_bucket_a",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	bucketB, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:           "session_alpha",
		IsolationKey:         "api_key_1",
		WorkspaceKey:         "workspace_alpha",
		ManagedSessionBucket: "managed_bucket_b",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	require.Equal(t, baseA.Body["user_id"], baseB.Body["user_id"])
	require.NotEqual(t, baseA.Body["user_id"], otherWorkspace.Body["user_id"])
	require.NotEqual(t, baseA.Body["user_id"], bucketA.Body["user_id"])
	require.NotEqual(t, bucketA.Body["user_id"], bucketB.Body["user_id"])
}

func TestCodexGatewayWorkspaceKeyIsStableAcrossMetadataOrderAndPathFormatting(t *testing.T) {
	headersA := http.Header{}
	headersA.Set("X-Codex-Turn-Metadata", `{
		"workspaces": {
			"/Users/example/project": {},
			"/Users/example/other/": {}
		}
	}`)
	headersB := http.Header{}
	headersB.Set("X-Codex-Turn-Metadata", `{
		"workspaces": {
			"  /Users/example/other  ": {},
			" /Users/example/project/ ": {}
		}
	}`)

	require.Equal(t, codexGatewayWorkspaceKey(headersA), codexGatewayWorkspaceKey(headersB))
}

func TestCodexGatewayWorkspaceKeyDiffersForDifferentWorkspaces(t *testing.T) {
	headersA := http.Header{}
	headersA.Set("X-Codex-Turn-Metadata", `{"workspaces":{"/Users/example/project":{}}}`)
	headersB := http.Header{}
	headersB.Set("X-Codex-Turn-Metadata", `{"workspaces":{"/Users/example/other":{}}}`)

	require.NotEqual(t, codexGatewayWorkspaceKey(headersA), codexGatewayWorkspaceKey(headersB))
}

func TestCodexGatewayManagedSessionBucketRequiresManagedHeadersAndKeepsSubagentsIsolated(t *testing.T) {
	metadata := `{"session_id":"logical-session-1","workspaces":{"/Users/example/project":{}}}`
	withoutManagedHeaders := http.Header{}
	withoutManagedHeaders.Set("X-Codex-Turn-Metadata", metadata)
	withParentHeader := http.Header{}
	withParentHeader.Set("X-Codex-Turn-Metadata", metadata)
	withParentHeader.Set("X-Codex-Parent-Thread-Id", "parent-thread-1")
	withSubagentHeader := http.Header{}
	withSubagentHeader.Set("X-Codex-Turn-Metadata", metadata)
	withSubagentHeader.Set("X-OpenAI-Subagent", "true")

	require.Empty(t, codexGatewayManagedSessionBucket(withoutManagedHeaders))
	require.NotEmpty(t, codexGatewayManagedSessionBucket(withParentHeader))
	require.Equal(t, codexGatewayManagedSessionBucket(withParentHeader), codexGatewayManagedSessionBucket(withSubagentHeader))
}

func TestCodexGatewayDeepSeekRequest_ManagedSessionDiagnosticsExposeScopeAndHashes(t *testing.T) {
	baseDir := t.TempDir()
	keyPath := filepath.Join(baseDir, ".key")
	manager := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                true,
		BaseDir:                baseDir,
		HashKeyFile:            keyPath,
		CorrelationHashKeyFile: keyPath,
	})
	defer manager.Close()

	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"reply ok"}]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}
	workspaceKey := codexGatewayWorkspaceKey(http.Header{
		"X-Codex-Turn-Metadata": []string{`{"workspaces":{"/Users/example/project":{}}}`},
	})
	trace := manager.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{TraceID: "managed_scope_diag", Provider: "deepseek", Model: "deepseek-v4-pro"})
	require.NotNil(t, trace)

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:           "logical-session-1",
		IsolationKey:         "api-key-1",
		WorkspaceKey:         workspaceKey,
		ManagedSessionBucket: "managed-bucket-1",
		CaptureTrace:         trace,
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	require.NotEmpty(t, prepared.Body["user_id"])

	deepseekDiag, ok := trace.requestDiag["deepseek_cache"].(map[string]any)
	require.True(t, ok)
	stable, ok := deepseekDiag["stable_serialization"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "actor_workspace_session_bucket", stable["user_id_scope"])
	require.Equal(t, "derived_actor_workspace", stable["user_id_source"])
	require.Equal(t, "actor_workspace_session_bucket", deepseekDiag["user_id_scope"])
	require.Equal(t, "derived_actor_workspace", deepseekDiag["user_id_source"])
	require.NotEmpty(t, deepseekDiag["user_id_hash"])
	require.NotEmpty(t, deepseekDiag["workspace_scope_hash"])
	require.NotEmpty(t, deepseekDiag["managed_session_bucket_hash"])
	require.Equal(t, deepseekDiag["user_id_hash"], trace.cacheUsage["user_id_hash"])
	require.Equal(t, deepseekDiag["workspace_scope_hash"], trace.cacheUsage["workspace_scope_hash"])
	require.Equal(t, deepseekDiag["managed_session_bucket_hash"], trace.cacheUsage["managed_session_bucket_hash"])
	require.Equal(t, "actor_workspace_session_bucket", trace.cacheUsage["user_id_scope"])
	require.Equal(t, "derived_actor_workspace", trace.cacheUsage["user_id_source"])
}

func TestCodexGatewayDeepSeekRequest_ManagedSessionHeaderScopeIsDeterministicAndDiagnosed(t *testing.T) {
	baseDir := t.TempDir()
	keyPath := filepath.Join(baseDir, ".key")
	manager := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                true,
		BaseDir:                baseDir,
		HashKeyFile:            keyPath,
		CorrelationHashKeyFile: keyPath,
	})
	defer manager.Close()

	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"reply ok"}]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}
	metadata := `{"session_id":"logical-session-1","workspaces":{"/Users/example/project":{}}}`
	headersWithoutManagedScope := http.Header{}
	headersWithoutManagedScope.Set("X-Codex-Turn-Metadata", metadata)
	headersWithParentScope := http.Header{}
	headersWithParentScope.Set("X-Codex-Turn-Metadata", metadata)
	headersWithParentScope.Set("X-Codex-Parent-Thread-Id", "parent-thread-1")
	headersWithSubagentScope := http.Header{}
	headersWithSubagentScope.Set("X-Codex-Turn-Metadata", metadata)
	headersWithSubagentScope.Set("X-OpenAI-Subagent", "true")

	build := func(traceID string, headers http.Header) (CodexGatewayPreparedDeepSeekRequest, map[string]any) {
		trace := manager.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{TraceID: traceID, Provider: "deepseek", Model: "deepseek-v4-pro"})
		require.NotNil(t, trace)
		prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
			SessionKey:           "logical-session-1",
			IsolationKey:         "api-key-1",
			WorkspaceKey:         codexGatewayWorkspaceKey(headers),
			ManagedSessionBucket: codexGatewayManagedSessionBucket(headers),
			CaptureTrace:         trace,
		}, CodexGatewayDeepSeekRequestConfig{})
		require.NoError(t, err)
		deepseekDiag, ok := trace.requestDiag["deepseek_cache"].(map[string]any)
		require.True(t, ok)
		return prepared, deepseekDiag
	}

	basePrepared, baseDiag := build("managed_scope_without_headers", headersWithoutManagedScope)
	parentPrepared, parentDiag := build("managed_scope_parent", headersWithParentScope)
	subagentPrepared, subagentDiag := build("managed_scope_subagent", headersWithSubagentScope)

	require.NotEqual(t, basePrepared.Body["user_id"], parentPrepared.Body["user_id"])
	require.Equal(t, parentPrepared.Body["user_id"], subagentPrepared.Body["user_id"])
	require.Equal(t, "actor_workspace", baseDiag["user_id_scope"])
	require.Equal(t, "actor_workspace_session_bucket", parentDiag["user_id_scope"])
	require.Equal(t, "actor_workspace_session_bucket", subagentDiag["user_id_scope"])
	require.Equal(t, "derived_actor_workspace", baseDiag["user_id_source"])
	require.Equal(t, "derived_actor_workspace", parentDiag["user_id_source"])
	require.Equal(t, "derived_actor_workspace", subagentDiag["user_id_source"])
	require.NotEmpty(t, baseDiag["workspace_scope_hash"])
	require.NotContains(t, baseDiag, "managed_session_bucket_hash")
	require.NotEmpty(t, parentDiag["managed_session_bucket_hash"])
	require.Equal(t, parentDiag["managed_session_bucket_hash"], subagentDiag["managed_session_bucket_hash"])
}

func TestCodexGatewayDeepSeekRequest_StateMissDiagnosticsIncludeUserScope(t *testing.T) {
	baseDir := t.TempDir()
	keyPath := filepath.Join(baseDir, ".key")
	manager := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                true,
		BaseDir:                baseDir,
		HashKeyFile:            keyPath,
		CorrelationHashKeyFile: keyPath,
	})
	defer manager.Close()

	previousID := "resp_missing_scope_diag"
	trace := manager.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{TraceID: "state_miss_scope_diag", Provider: "deepseek", Model: "deepseek-v4-pro"})
	require.NotNil(t, trace)
	model := CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"}
	_, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:              "deepseek-v4-pro",
		PreviousResponseID: &previousID,
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}, NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{TTL: time.Minute, MaxItems: 4, Now: time.Now}), CodexGatewayDeepSeekRequestContext{
		SessionKey:           "session_miss_diag",
		IsolationKey:         "api-key-1",
		WorkspaceKey:         "workspace_miss_diag",
		ManagedSessionBucket: "bucket_miss_diag",
		CaptureTrace:         trace,
	}, CodexGatewayDeepSeekRequestConfig{})
	require.ErrorIs(t, err, ErrCodexGatewayStateNotFound)

	deepseekDiag, ok := trace.requestDiag["deepseek_cache"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, true, deepseekDiag["previous_response_id_present"])
	require.Equal(t, "miss", deepseekDiag["state_lookup_status"])
	require.Equal(t, "none", deepseekDiag["previous_response_replay_mode"])
	require.NotEmpty(t, deepseekDiag["user_id_hash"])
	require.NotEmpty(t, deepseekDiag["workspace_scope_hash"])
	require.NotEmpty(t, deepseekDiag["managed_session_bucket_hash"])
	require.Equal(t, "actor_workspace_session_bucket", deepseekDiag["user_id_scope"])
	require.Equal(t, "derived_actor_workspace", deepseekDiag["user_id_source"])
}

func TestCodexGatewayDeepSeekRequest_HostedToolMappingIsStableAcrossHostedToolOrder(t *testing.T) {
	reqA := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"reply ok"}]}
		]`),
		Tools: json.RawMessage(`[
			{"type":"web_search"},
			{"type":"image_generation"},
			{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}
		]`),
	}
	reqB := reqA
	reqB.Tools = json.RawMessage(`[
		{"type":"image_generation"},
		{"type":"web_search"},
		{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}
	]`)
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}
	ctx := CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "api_key_1",
	}

	first, err := BuildCodexGatewayDeepSeekRequest(model, reqA, nil, ctx, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	second, err := BuildCodexGatewayDeepSeekRequest(model, reqB, nil, ctx, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	firstTools := first.Body["tools"].([]any)
	secondTools := second.Body["tools"].([]any)
	require.NotNil(t, deepSeekRequestToolFunctionByName(t, firstTools, "web_search"))
	require.NotNil(t, deepSeekRequestToolFunctionByName(t, secondTools, "web_search"))
	require.NotNil(t, deepSeekRequestToolFunctionByName(t, firstTools, "exec_command"))
	require.NotNil(t, deepSeekRequestToolFunctionByName(t, secondTools, "exec_command"))
	require.Contains(t, first.Body["messages"].([]any)[0].(map[string]any)["content"], "image_generation")
	require.Contains(t, second.Body["messages"].([]any)[0].(map[string]any)["content"], "image_generation")
}

func TestCodexGatewayDeepSeekRequest_EquivalentToolOrderHasStableToolSchemaHash(t *testing.T) {
	baseDir := t.TempDir()
	keyPath := filepath.Join(baseDir, ".key")
	manager := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                true,
		BaseDir:                baseDir,
		HashKeyFile:            keyPath,
		CorrelationHashKeyFile: keyPath,
	})
	defer manager.Close()

	model := CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"}
	baseReq := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"reply ok"}]}
		]`),
	}
	reqA := baseReq
	reqA.Tools = json.RawMessage(`[
		{"type":"namespace","name":"browser","tools":[
			{"type":"function","name":"open","parameters":{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]}},
			{"type":"function","name":"click","parameters":{"type":"object","properties":{"y":{"type":"number"},"x":{"type":"number"}},"required":["y","x"]}}
		]},
		{"type":"custom","name":"apply_patch","format":{"type":"grammar"}},
		{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"},"cwd":{"type":"string"}},"required":["cwd","cmd"]}},
		{"type":"web_search"}
	]`)
	reqB := baseReq
	reqB.Tools = json.RawMessage(`[
		{"type":"web_search"},
		{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cwd":{"type":"string"},"cmd":{"type":"string"}},"required":["cmd","cwd"]}},
		{"type":"namespace","name":"browser","tools":[
			{"type":"function","name":"click","parameters":{"type":"object","properties":{"x":{"type":"number"},"y":{"type":"number"}},"required":["x","y"]}},
			{"type":"function","name":"open","parameters":{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]}}
		]},
		{"type":"custom","name":"apply_patch","format":{"type":"grammar"}}
	]`)

	build := func(traceID string, req CodexGatewayResponsesCreateRequest) (CodexGatewayPreparedDeepSeekRequest, map[string]any) {
		trace := manager.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{TraceID: traceID, Provider: "deepseek", Model: "deepseek-v4-pro"})
		require.NotNil(t, trace)
		prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
			SessionKey:   "stable_tool_session",
			IsolationKey: "stable_tool_actor",
			CaptureTrace: trace,
		}, CodexGatewayDeepSeekRequestConfig{})
		require.NoError(t, err)
		diag, ok := trace.requestDiag["deepseek_cache"].(map[string]any)
		require.True(t, ok)
		return prepared, diag
	}

	first, firstDiag := build("stable_tools_a", reqA)
	second, secondDiag := build("stable_tools_b", reqB)

	require.Equal(t, mustMarshalJSON(t, first.Body["tools"]), mustMarshalJSON(t, second.Body["tools"]))
	require.NotEmpty(t, firstDiag["tool_schema_hash"])
	require.Equal(t, firstDiag["tool_schema_hash"], secondDiag["tool_schema_hash"])
}

func TestCodexGatewayDeepSeekRequest_BodyIsDeterministicForCachePrefix(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model:        "deepseek-v4-pro",
		Instructions: json.RawMessage(`"Act as a coding agent."`),
		Input: json.RawMessage(`[
			{"type":"message","role":"developer","content":[{"type":"input_text","text":"Use local tools when useful."}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"inspect the project"}]}
		]`),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"computer-use","tools":[
				{"name":"click","parameters":{"type":"object","properties":{"x":{"type":"number"},"y":{"type":"number"}}}},
				{"name":"press_key","parameters":{"type":"object","properties":{"key":{"type":"string"}}}}
			]},
			{"type":"custom","name":"apply_patch","format":{"type":"grammar"}},
			{"type":"web_search"}
		]`),
		Reasoning: json.RawMessage(`{"effort":"xhigh"}`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}
	ctx := CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "api_key_1",
	}

	first, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, ctx, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	second, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, ctx, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	firstJSON, err := json.Marshal(first.Body)
	require.NoError(t, err)
	secondJSON, err := json.Marshal(second.Body)
	require.NoError(t, err)
	require.JSONEq(t, string(firstJSON), string(secondJSON))
	require.Equal(t, string(firstJSON), string(secondJSON))
}

func TestCodexGatewayDeepSeekRequest_CaptureDiagnosticsExcludeVolatileFields(t *testing.T) {
	baseDir := t.TempDir()
	keyPath := filepath.Join(baseDir, ".key")
	manager := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                true,
		BaseDir:                baseDir,
		HashKeyFile:            keyPath,
		CorrelationHashKeyFile: keyPath,
	})
	defer manager.Close()

	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}
	streamTrue := true
	reqA := CodexGatewayResponsesCreateRequest{
		Model:        "deepseek-v4-pro",
		Instructions: json.RawMessage(`"Keep answers short."`),
		Input: json.RawMessage(`[
			{"type":"message","role":"developer","content":[{"type":"input_text","text":"Use local tools when possible."}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"inspect the repository"}]}
		]`),
		Tools: json.RawMessage(`[
			{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}},
			{"type":"custom","name":"apply_patch","format":{"type":"grammar"}}
		]`),
		Stream: &streamTrue,
		RawFields: map[string]json.RawMessage{
			"stream_options": json.RawMessage(`{"include_usage":true}`),
		},
	}
	traceA := manager.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{TraceID: "diag_a", Provider: "deepseek", Model: "deepseek-v4-pro"})
	require.NotNil(t, traceA)
	_, err := BuildCodexGatewayDeepSeekRequest(model, reqA, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_a",
		IsolationKey: "isolation_a",
		CaptureTrace: traceA,
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	deepseekA, ok := traceA.requestDiag["deepseek_cache"].(map[string]any)
	require.True(t, ok)
	stableA, ok := deepseekA["stable_serialization"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "deepseek-cache-v1", stableA["version"])
	require.ElementsMatch(t, []string{"stream", "stream_options", "user_id"}, stableA["request_shape_excluded_fields"])
	require.Equal(t, 8, stableA["message_prefix_limit"])
	require.Equal(t, "actor_workspace", stableA["user_id_scope"])
	require.Equal(t, "derived_actor_workspace", stableA["user_id_source"])
	require.NotEmpty(t, deepseekA["request_prefix_hash"])
	require.NotEmpty(t, deepseekA["tool_schema_hash"])
	require.NotEmpty(t, deepseekA["message_prefix_hash"])
	require.NotEmpty(t, deepseekA["request_shape_hash"])
	require.NotEmpty(t, deepseekA["user_id_hash"])
	require.Equal(t, deepseekA["request_prefix_hash"], traceA.cacheUsage["request_prefix_hash"])
	require.Equal(t, deepseekA["tool_schema_hash"], traceA.cacheUsage["tool_schema_hash"])
	require.Equal(t, deepseekA["message_prefix_hash"], traceA.cacheUsage["message_prefix_hash"])
	require.Equal(t, deepseekA["request_shape_hash"], traceA.cacheUsage["request_shape_hash"])
	require.Equal(t, deepseekA["user_id_hash"], traceA.cacheUsage["user_id_hash"])
	require.Equal(t, "actor_workspace", traceA.cacheUsage["user_id_scope"])
	require.Equal(t, "derived_actor_workspace", traceA.cacheUsage["user_id_source"])

	streamFalse := false
	reqB := reqA
	reqB.Stream = &streamFalse
	reqB.RawFields = map[string]json.RawMessage{
		"stream_options": json.RawMessage(`{"include_usage":false,"reason":"client_toggle"}`),
	}
	traceB := manager.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{TraceID: "diag_b", Provider: "deepseek", Model: "deepseek-v4-pro"})
	require.NotNil(t, traceB)
	_, err = BuildCodexGatewayDeepSeekRequest(model, reqB, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_b",
		IsolationKey: "isolation_b",
		CaptureTrace: traceB,
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	deepseekB, ok := traceB.requestDiag["deepseek_cache"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, deepseekA["request_shape_hash"], deepseekB["request_shape_hash"])
	require.Equal(t, deepseekA["tool_schema_hash"], deepseekB["tool_schema_hash"])
	require.Equal(t, deepseekA["message_prefix_hash"], deepseekB["message_prefix_hash"])
	require.NotEqual(t, deepseekA["user_id_hash"], deepseekB["user_id_hash"])
}

func TestCodexGatewayDeepSeekRequest_NormalizesDeveloperRoleForChatCompletions(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"developer","content":[{"type":"input_text","text":"Always be concise."}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"Reply OK."}]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Equal(t, "system", messages[0].(map[string]any)["role"])
	require.Contains(t, messages[0].(map[string]any)["content"], "Always be concise.")
	require.Equal(t, "user", messages[1].(map[string]any)["role"])
}

func TestCodexGatewayDeepSeekRequest_NormalizesLatestReminderRoleForChatCompletions(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"latest_reminder","content":[{"type":"input_text","text":"Keep the most recent user instruction in force."}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"Reply OK."}]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Equal(t, "system", messages[0].(map[string]any)["role"])
	require.Contains(t, messages[0].(map[string]any)["content"], "Keep the most recent user instruction")
	require.Equal(t, "user", messages[1].(map[string]any)["role"])
}

func TestCodexGatewayDeepSeekPersistState_AllowsOrdinaryAssistantTurns(t *testing.T) {
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{TTL: time.Minute, MaxItems: 4, Now: time.Now})
	err := codexGatewayDeepSeekPersistState(
		store,
		"resp_ordinary_persist",
		"deepseek-v4-pro",
		CodexGatewayDeepSeekRequestContext{SessionKey: "session_ordinary_persist", IsolationKey: "iso_ordinary_persist"},
		"plain answer",
		true,
		"ordinary reasoning",
		true,
		false,
		nil,
		nil,
		nil,
	)
	require.NoError(t, err)

	state, err := store.Get(CodexGatewayStateLookupKey{
		ResponseID:    "resp_ordinary_persist",
		SessionKey:    "session_ordinary_persist",
		IsolationKey:  "iso_ordinary_persist",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	})
	require.NoError(t, err)
	require.Equal(t, "plain answer", state.AssistantContent)
	require.True(t, state.AssistantContentPresent)
	require.Equal(t, "ordinary reasoning", state.ReasoningContent)
	require.True(t, state.ReasoningContentPresent)
	require.Len(t, state.ReplayMessages, 1)
}

func TestCodexGatewayDeepSeekPersistState_SkipsEmptyAssistantTurns(t *testing.T) {
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{TTL: time.Minute, MaxItems: 4, Now: time.Now})
	err := codexGatewayDeepSeekPersistState(
		store,
		"resp_empty_persist",
		"deepseek-v4-pro",
		CodexGatewayDeepSeekRequestContext{SessionKey: "session_empty_persist", IsolationKey: "iso_empty_persist"},
		"",
		true,
		"",
		false,
		false,
		nil,
		nil,
		nil,
	)
	require.NoError(t, err)

	_, err = store.Get(CodexGatewayStateLookupKey{
		ResponseID:    "resp_empty_persist",
		SessionKey:    "session_empty_persist",
		IsolationKey:  "iso_empty_persist",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	})
	require.ErrorIs(t, err, ErrCodexGatewayStateNotFound)
}

func TestCodexGatewayDeepSeekRequest_ReplaysPreviousOrdinaryAssistantState(t *testing.T) {
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{TTL: time.Minute, MaxItems: 4, Now: time.Now})
	require.NoError(t, store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_ordinary_replay",
			SessionKey:    "session_ordinary_replay",
			IsolationKey:  "iso_ordinary_replay",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		},
		AssistantContent:        "plain answer",
		AssistantContentPresent: true,
		ReasoningContent:        "ordinary reasoning",
		ReasoningContentPresent: true,
	}))

	model := CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"}
	prepared, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:              "deepseek-v4-pro",
		PreviousResponseID: stringPtr("resp_ordinary_replay"),
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}, store, CodexGatewayDeepSeekRequestContext{SessionKey: "session_ordinary_replay", IsolationKey: "iso_ordinary_replay"}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 2)
	assistant := messages[0].(map[string]any)
	require.Equal(t, "assistant", assistant["role"])
	require.Equal(t, "plain answer", assistant["content"])
	require.Equal(t, "ordinary reasoning", assistant["reasoning_content"])
	require.Equal(t, "user", messages[1].(map[string]any)["role"])
}

func TestCodexGatewayDeepSeekRequest_ReplaysPreviousResponseFullMessagesPrefix(t *testing.T) {
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{
		TTL:      time.Minute,
		MaxItems: 4,
		Now:      time.Now,
	})
	replay := []json.RawMessage{
		json.RawMessage(`{"role":"system","content":"follow developer instructions"}`),
		json.RawMessage(`{"role":"user","content":"first prompt"}`),
		json.RawMessage(`{"role":"assistant","content":"first answer","reasoning_content":"first reasoning"}`),
		json.RawMessage(`{"role":"user","content":"second prompt"}`),
		json.RawMessage(`{"role":"assistant","content":"second answer","reasoning_content":"second reasoning"}`),
	}
	require.NoError(t, store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_full_replay",
			SessionKey:    "session_full_replay",
			IsolationKey:  "iso_full_replay",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		},
		AssistantContent:        "second answer",
		AssistantContentPresent: true,
		ReasoningContent:        "second reasoning",
		ReasoningContentPresent: true,
		ReplayMessages:          replay,
	}))

	model := CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"}
	prepared, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:              "deepseek-v4-pro",
		PreviousResponseID: stringPtr("resp_full_replay"),
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"third prompt"}]}
		]`),
	}, store, CodexGatewayDeepSeekRequestContext{SessionKey: "session_full_replay", IsolationKey: "iso_full_replay"}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 6)
	for i, expected := range replay {
		var want map[string]any
		require.NoError(t, json.Unmarshal(expected, &want))
		require.Equal(t, want, messages[i].(map[string]any))
	}
	require.Equal(t, "third prompt", messages[5].(map[string]any)["content"])
}

func TestCodexGatewayDeepSeekRequest_PreviousResponseDeltaMatchesFullReplayPrefix(t *testing.T) {
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{
		TTL:      time.Minute,
		MaxItems: 4,
		Now:      time.Now,
	})
	replay := []json.RawMessage{
		json.RawMessage(`{"role":"user","content":"ask one"}`),
		json.RawMessage(`{"role":"assistant","content":"answer one","reasoning_content":"reason one"}`),
	}
	require.NoError(t, store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_delta_replay",
			SessionKey:    "session_delta_replay",
			IsolationKey:  "iso_delta_replay",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		},
		AssistantContent:        "answer one",
		AssistantContentPresent: true,
		ReasoningContent:        "reason one",
		ReasoningContentPresent: true,
		ReplayMessages:          replay,
	}))

	model := CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"}
	fullReq := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"ask one"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer one"}],"reasoning_content":"reason one"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"ask two"}]}
		]`),
	}
	fullPrepared, err := BuildCodexGatewayDeepSeekRequest(model, fullReq, nil, CodexGatewayDeepSeekRequestContext{SessionKey: "session_delta_replay", IsolationKey: "iso_delta_replay"}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	deltaPrepared, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:              "deepseek-v4-pro",
		PreviousResponseID: stringPtr("resp_delta_replay"),
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"ask two"}]}
		]`),
	}, store, CodexGatewayDeepSeekRequestContext{SessionKey: "session_delta_replay", IsolationKey: "iso_delta_replay"}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	require.Equal(t, fullPrepared.Body["messages"], deltaPrepared.Body["messages"])
}

func TestCodexGatewayDeepSeekRequest_ReplaysPreviousToolLoopStateAndNormalizesOutputs(t *testing.T) {
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{
		TTL:      time.Minute,
		MaxItems: 4,
		Now:      time.Now,
	})
	require.NoError(t, store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_prev",
			SessionKey:    "session_1",
			IsolationKey:  "user_1",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		},
		AssistantContent:        "",
		AssistantContentPresent: true,
		ReasoningContent:        "need tool result",
		ReasoningContentPresent: true,
		ToolCalls: []CodexGatewayStoredToolCall{
			{ID: "call_1", Type: CodexGatewayToolKindNamespace, Name: "open-page", Arguments: `{"url":"https://example.com"}`},
		},
		ToolNameMap: map[string]CodexGatewayToolNameMapEntry{
			"browser__open-page": {Alias: "browser__open-page", Kind: CodexGatewayToolKindNamespace, Namespace: "browser", Name: "open-page"},
		},
	}))

	req := CodexGatewayResponsesCreateRequest{
		Model:              "deepseek-v4-pro",
		PreviousResponseID: stringPtr("resp_prev"),
		Input: json.RawMessage(`[
			{"type":"function_call_output","call_id":"call_1","output":{"ok":true,"url":"https://example.com"}},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"summarize the page"}]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, store, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
		UserID:       "stable_user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 3)

	assistant := messages[0].(map[string]any)
	require.Equal(t, "assistant", assistant["role"])
	require.Equal(t, "", assistant["content"])
	require.Equal(t, "need tool result", assistant["reasoning_content"])
	require.Equal(t, "browser__open-page", assistant["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"])

	toolMessage := messages[1].(map[string]any)
	require.Equal(t, "tool", toolMessage["role"])
	require.Equal(t, "call_1", toolMessage["tool_call_id"])
	require.Equal(t, `{"ok":true,"url":"https://example.com"}`, toolMessage["content"])
}

func TestCodexGatewayDeepSeekRequest_CoalescesConsecutiveFunctionCallsBeforeOutputs(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"function_call","call_id":"call_00","name":"exec_command","arguments":"{\"cmd\":\"pwd\"}"},
			{"type":"function_call","call_id":"call_01","name":"exec_command","arguments":"{\"cmd\":\"rg --files\"}"},
			{"type":"function_call","call_id":"call_02","name":"exec_command","arguments":"{\"cmd\":\"git status --short\"}"},
			{"type":"function_call_output","call_id":"call_00","output":"pwd output"},
			{"type":"function_call_output","call_id":"call_01","output":"rg output"},
			{"type":"function_call_output","call_id":"call_02","output":"git output"}
		]`),
		Tools: json.RawMessage(`[
			{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 4)

	assistant := messages[0].(map[string]any)
	require.Equal(t, "assistant", assistant["role"])
	require.Equal(t, "", assistant["content"])
	calls := assistant["tool_calls"].([]any)
	require.Len(t, calls, 3)
	require.Equal(t, "call_00", calls[0].(map[string]any)["id"])
	require.Equal(t, "call_01", calls[1].(map[string]any)["id"])
	require.Equal(t, "call_02", calls[2].(map[string]any)["id"])

	for i, expectedCallID := range []string{"call_00", "call_01", "call_02"} {
		toolMessage := messages[i+1].(map[string]any)
		require.Equal(t, "tool", toolMessage["role"])
		require.Equal(t, expectedCallID, toolMessage["tool_call_id"])
	}
}

func TestCodexGatewayDeepSeekRequest_CoalescesConsecutiveFunctionCallReasoning(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"function_call","call_id":"call_00","name":"exec_command","arguments":"{\"cmd\":\"pwd\"}","reasoning_content":"first tool reason"},
			{"type":"function_call","call_id":"call_01","name":"exec_command","arguments":"{\"cmd\":\"rg --files\"}","reasoning_content":"second tool reason"},
			{"type":"function_call_output","call_id":"call_00","output":"pwd output"},
			{"type":"function_call_output","call_id":"call_01","output":"rg output"}
		]`),
		Tools: json.RawMessage(`[
			{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_tool_reasoning",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 3)
	assistant := messages[0].(map[string]any)
	require.Equal(t, "first tool reason\nsecond tool reason", assistant["reasoning_content"])
}

func TestCodexGatewayDeepSeekRequest_PreservesResponsesReasoningItems(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"reasoning","summary_text":"first thought","content":[{"type":"summary_text","text":"second thought"},{"type":"text","text":"third thought"},{"text":"fourth thought"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_reasoning_item",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 2)
	assistant := messages[0].(map[string]any)
	require.Equal(t, "assistant", assistant["role"])
	require.Equal(t, "", assistant["content"])
	require.Equal(t, "first thought\nsecond thought\nthird thought\nfourth thought", assistant["reasoning_content"])
	require.NotContains(t, assistant["content"], "thought")
	require.Equal(t, "user", messages[1].(map[string]any)["role"])
}

func TestCodexGatewayDeepSeekRequest_IgnoresEmptyResponsesReasoningItems(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"reasoning","summary":[],"content":null},
			{"type":"reasoning","content":[{"type":"text","text":"   "}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_empty_reasoning_item",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 1)
	require.Equal(t, "user", messages[0].(map[string]any)["role"])
	require.NotContains(t, fmt.Sprint(messages), "reasoning_content")
}

func TestCodexGatewayDeepSeekRequest_PreservesReasoningBeforeFunctionCallOutput(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"reasoning","content":"need cwd before answering"},
			{"type":"function_call","call_id":"call_1","name":"exec_command","arguments":"{\"cmd\":\"pwd\"}"},
			{"type":"function_call_output","call_id":"call_1","output":"/tmp/project"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
		Tools: json.RawMessage(`[
			{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_reasoning_before_tool_output",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 3)
	assistant := messages[0].(map[string]any)
	require.Equal(t, "assistant", assistant["role"])
	require.Equal(t, "need cwd before answering", assistant["reasoning_content"])
	require.NotContains(t, assistant["content"], "need cwd")
	require.Len(t, assistant["tool_calls"].([]any), 1)
	require.Equal(t, "tool", messages[1].(map[string]any)["role"])
}

func TestCodexGatewayDeepSeekRequest_ReplaysPreviousCustomToolLoopState(t *testing.T) {
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{
		TTL:      time.Minute,
		MaxItems: 4,
		Now:      time.Now,
	})
	require.NoError(t, store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_custom",
			SessionKey:    "session_1",
			IsolationKey:  "user_1",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		},
		AssistantContent:        "",
		AssistantContentPresent: true,
		ReasoningContent:        "need to patch",
		ReasoningContentPresent: true,
		ToolCalls: []CodexGatewayStoredToolCall{
			{ID: "call_patch", Type: CodexGatewayToolKindCustom, Name: "apply_patch", Arguments: "*** Begin Patch\n*** End Patch\n"},
		},
		ToolNameMap: map[string]CodexGatewayToolNameMapEntry{
			"custom__apply_patch": {Alias: "custom__apply_patch", Kind: CodexGatewayToolKindCustom, Name: "apply_patch"},
		},
	}))

	req := CodexGatewayResponsesCreateRequest{
		Model:              "deepseek-v4-pro",
		PreviousResponseID: stringPtr("resp_custom"),
		Input: json.RawMessage(`[
			{"type":"custom_tool_call_output","call_id":"call_patch","name":"apply_patch","output":"patch applied"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, store, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 3)

	assistant := messages[0].(map[string]any)
	require.Equal(t, "assistant", assistant["role"])
	require.Equal(t, "custom__apply_patch", assistant["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"])

	toolMessage := messages[1].(map[string]any)
	require.Equal(t, "tool", toolMessage["role"])
	require.Equal(t, "call_patch", toolMessage["tool_call_id"])
	require.Equal(t, "patch applied", toolMessage["content"])
}

func TestCodexGatewayDeepSeekRequest_ConvertsInlineCustomToolCallsAndOutputs(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"custom_tool_call","call_id":"call_patch","name":"apply_patch","input":"*** Begin Patch\n*** End Patch\n"},
			{"type":"custom_tool_call_output","call_id":"call_patch","output":{"ok":true}}
		]`),
		Tools: json.RawMessage(`[
			{"type":"custom","name":"apply_patch","description":"edit files","format":{"type":"grammar"}}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 2)
	assistant := messages[0].(map[string]any)
	require.Equal(t, "assistant", assistant["role"])
	require.Equal(t, "custom__edit", assistant["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"])
	require.Equal(t, "*** Begin Patch\n*** End Patch\n", assistant["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["arguments"])
	require.Equal(t, `{"ok":true}`, messages[1].(map[string]any)["content"])
}

func TestCodexGatewayDeepSeekRequest_DropsStaleToolChoiceWhenReplayRequestHasNoTools(t *testing.T) {
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{
		TTL:      time.Minute,
		MaxItems: 4,
		Now:      time.Now,
	})
	require.NoError(t, store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_custom",
			SessionKey:    "session_1",
			IsolationKey:  "user_1",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		},
		AssistantContent:        "",
		AssistantContentPresent: true,
		ReasoningContent:        "need to patch",
		ReasoningContentPresent: true,
		ToolCalls: []CodexGatewayStoredToolCall{
			{ID: "call_patch", Type: CodexGatewayToolKindCustom, Alias: "custom__edit", Name: "edit", Arguments: "*** Begin Patch\n*** End Patch\n"},
		},
		ToolNameMap: map[string]CodexGatewayToolNameMapEntry{
			"custom__edit": {Alias: "custom__edit", Kind: CodexGatewayToolKindCustom, Name: "edit"},
		},
	}))

	req := CodexGatewayResponsesCreateRequest{
		Model:              "deepseek-v4-pro",
		PreviousResponseID: stringPtr("resp_custom"),
		ToolChoice:         json.RawMessage(`"edit"`),
		Input: json.RawMessage(`[
			{"type":"custom_tool_call_output","call_id":"call_patch","name":"edit","output":"patch applied"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, store, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(raw, "tool_choice").Exists())
	require.Equal(t, "custom__edit", gjson.GetBytes(raw, "messages.0.tool_calls.0.function.name").String())
}

func TestCodexGatewayDeepSeekRequest_ReplaysLegacyCustomToolCallWithoutCurrentTools(t *testing.T) {
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:      "deepseek-v4-pro",
		ToolChoice: json.RawMessage(`"edit"`),
		Input: json.RawMessage(`[
			{"type":"custom_tool_call","call_id":"call_legacy_custom","name":"edit","input":"*** Begin Patch\n*** End Patch\n"},
			{"type":"custom_tool_call_output","call_id":"call_legacy_custom","output":"patch applied"}
		]`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(raw, "tool_choice").Exists())
	require.Equal(t, "custom__edit", gjson.GetBytes(raw, "messages.0.tool_calls.0.function.name").String())
	require.Equal(t, "tool", gjson.GetBytes(raw, "messages.1.role").String())
	require.Equal(t, "call_legacy_custom", gjson.GetBytes(raw, "messages.1.tool_call_id").String())
}

func TestCodexGatewayDeepSeekRequest_NormalizesLegacyDottedNamespaceToolNames(t *testing.T) {
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	preparedStringChoice, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[{"type":"function_call","call_id":"call_ns","name":"shell.exec","arguments":"{\"cmd\":\"pwd\"}"}]`),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"shell","tools":[{"type":"function","name":"exec","parameters":{"type":"object"}}]}
		]`),
		ToolChoice: json.RawMessage(`"shell.exec"`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	rawStringChoice, err := json.Marshal(preparedStringChoice.Body)
	require.NoError(t, err)
	require.Equal(t, "shell__exec", gjson.GetBytes(rawStringChoice, "tool_choice.function.name").String())
	require.Equal(t, "shell__exec", gjson.GetBytes(rawStringChoice, "messages.0.tool_calls.0.function.name").String())

	preparedObjectChoice, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[{"type":"function_call","call_id":"call_ns_obj","name":"shell.exec","arguments":"{\"cmd\":\"pwd\"}"}]`),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"shell","tools":[{"type":"function","name":"exec","parameters":{"type":"object"}}]}
		]`),
		ToolChoice: json.RawMessage(`{"type":"function","name":"shell.exec"}`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	rawObjectChoice, err := json.Marshal(preparedObjectChoice.Body)
	require.NoError(t, err)
	require.Equal(t, "shell__exec", gjson.GetBytes(rawObjectChoice, "tool_choice.function.name").String())
	require.Equal(t, "shell__exec", gjson.GetBytes(rawObjectChoice, "messages.0.tool_calls.0.function.name").String())
}

func TestCodexGatewayDeepSeekRequest_AcceptsLocalShellCallInput(t *testing.T) {
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[{"type":"local_shell_call","call_id":"call_ns","name":"shell.exec","action":{"type":"exec","command":["zsh","-lc","pwd"]}}]`),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"shell","tools":[{"type":"function","name":"exec","parameters":{"type":"object"}}]}
		]`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	require.Equal(t, "shell__exec", gjson.GetBytes(raw, "messages.0.tool_calls.0.function.name").String())
	require.Equal(t, `{"cmd":"pwd"}`, gjson.GetBytes(raw, "messages.0.tool_calls.0.function.arguments").String())
}

func TestCodexGatewayDeepSeekRequest_MapsAssistantToolCallsAndBackfillsReasoningContent(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{
				"type":"message",
				"role":"assistant",
				"content":[{"type":"output_text","text":"let me open that"}],
				"tool_calls":[
					{
						"id":"call_1",
						"type":"function",
						"function":{
							"name":"open-page",
							"arguments":"{\"url\":\"https://example.com\"}"
						}
					}
				]
			},
			{
				"type":"message",
				"role":"assistant",
				"content":[{"type":"output_text","text":"waiting for tool"}]
			}
		]`),
		Tools: json.RawMessage(`[
			{
				"type":"namespace",
				"name":"browser",
				"tools":[
					{"name":"open-page","parameters":{"type":"object","properties":{"url":{"type":"string"}}}}
				]
			}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 2)

	first := messages[0].(map[string]any)
	require.Equal(t, "assistant", first["role"])
	require.Equal(t, "let me open that", first["content"])
	require.Equal(t, "", first["reasoning_content"])
	require.Equal(t, "browser__open-page", first["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"])

	second := messages[1].(map[string]any)
	require.Equal(t, "assistant", second["role"])
	require.Equal(t, "", second["reasoning_content"])
}

func TestCodexGatewayDeepSeekRequest_RejectsInvalidStateAndUnpairedToolOutputs(t *testing.T) {
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{
		TTL:      time.Minute,
		MaxItems: 4,
		Now:      time.Now,
	})
	invalidState := CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_invalid",
			SessionKey:    "session_1",
			IsolationKey:  "user_1",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		},
		AssistantContentPresent: true,
		ToolCalls:               []CodexGatewayStoredToolCall{{ID: "call_1", Name: "shell", Arguments: `{}`}},
	}
	store.entries[codexGatewayStateStorageKey(invalidState.Key)] = codexGatewayStateEntry{
		state:     invalidState,
		expiresAt: time.Now().Add(time.Minute),
	}

	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}
	_, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:              "deepseek-v4-pro",
		PreviousResponseID: stringPtr("resp_invalid"),
		Input:              json.RawMessage(`[{"type":"function_call_output","call_id":"call_1","output":"ok"}]`),
	}, store, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCodexGatewayStateInvalid))

	require.NoError(t, store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_parallel",
			SessionKey:    "session_1",
			IsolationKey:  "user_1",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		},
		AssistantContentPresent: true,
		ReasoningContent:        "need both tool results",
		ReasoningContentPresent: true,
		ToolCalls: []CodexGatewayStoredToolCall{
			{ID: "call_a", Name: "shell", Arguments: `{}`},
			{ID: "call_b", Name: "shell", Arguments: `{}`},
		},
		ToolNameMap: map[string]CodexGatewayToolNameMapEntry{
			"shell": {Alias: "shell", Kind: CodexGatewayToolKindFunction, Name: "shell"},
		},
	}))

	_, err = BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:              "deepseek-v4-pro",
		PreviousResponseID: stringPtr("resp_parallel"),
		Input:              json.RawMessage(`[{"type":"function_call_output","call_id":"call_a","output":"ok"}]`),
	}, store, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)

	_, err = BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:              "deepseek-v4-pro",
		PreviousResponseID: stringPtr("resp_parallel"),
		Input:              json.RawMessage(`[]`),
	}, store, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCodexGatewayStateInvalid))

	_, err = BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:              "deepseek-v4-pro",
		PreviousResponseID: stringPtr("resp_parallel"),
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"before tool output"}]},
			{"type":"function_call_output","call_id":"call_a","output":"ok"}
		]`),
	}, store, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCodexGatewayStateInvalid))

	_, err = BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:              "deepseek-v4-pro",
		PreviousResponseID: stringPtr("resp_parallel"),
		Input:              json.RawMessage(`[{"type":"function_call_output","call_id":"call_a","output":"ok"}]`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCodexGatewayStateInvalid))

	_, err = BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[{"type":"function_call_output","call_id":"call_missing","output":"ok"}]`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)

	_, err = BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"function_call","call_id":"call_dup","name":"shell","arguments":"{\"cmd\":\"pwd\"}"},
			{"type":"function_call","call_id":"call_dup","name":"shell","arguments":"{\"cmd\":\"ls\"}"},
			{"type":"function_call_output","call_id":"call_dup","output":"ok"}
		]`),
		Tools: json.RawMessage(`[{"type":"function","name":"shell","parameters":{"type":"object"}}]`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)

	_, err = BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"function_call","call_id":"call_reuse","name":"shell","arguments":"{\"cmd\":\"pwd\"}"},
			{"type":"function_call_output","call_id":"call_reuse","output":"ok"},
			{"type":"function_call","call_id":"call_reuse","name":"shell","arguments":"{\"cmd\":\"ls\"}"}
		]`),
		Tools: json.RawMessage(`[{"type":"function","name":"shell","parameters":{"type":"object"}}]`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)
}

func TestCodexGatewayDeepSeekRequest_StripsResponsesOnlyFieldsFromPreparedBody(t *testing.T) {
	store := true
	parallel := true
	req := CodexGatewayResponsesCreateRequest{
		Model:        "deepseek-v4-pro",
		Instructions: json.RawMessage(`"Keep answers short."`),
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"reply ok"}]}
		]`),
		Include:           json.RawMessage(`["reasoning.encrypted_content"]`),
		Store:             &store,
		ParallelToolCalls: &parallel,
		ClientMetadata:    json.RawMessage(`{"trace_id":"client-side-only"}`),
		PromptCacheKey:    "session-only-cache-key",
	}
	model := CodexGatewayModel{
		Slug:                      "deepseek-v4-pro",
		Provider:                  "deepseek",
		UpstreamModel:             "deepseek-v4-pro",
		SupportsParallelToolCalls: true,
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	require.NotContains(t, prepared.Body, "include")
	require.NotContains(t, prepared.Body, "store")
	require.NotContains(t, prepared.Body, "parallel_tool_calls")
	require.NotContains(t, prepared.Body, "client_metadata")
	require.NotContains(t, prepared.Body, "prompt_cache_key")
	require.Equal(t, "deepseek-v4-pro", prepared.Body["model"])
	require.Equal(t, "user", prepared.Body["messages"].([]any)[1].(map[string]any)["role"])
}

func TestCodexGatewayDeepSeekStreamRequest_StripsResponsesOnlyFieldsFromUpstreamBody(t *testing.T) {
	store := true
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}
		]`),
		Include: json.RawMessage(`["reasoning.encrypted_content"]`),
		Store:   &store,
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.False(t, gjson.GetBytes(body, "include").Exists())
		require.False(t, gjson.GetBytes(body, "store").Exists())
		require.True(t, gjson.GetBytes(body, "stream").Bool())
		require.True(t, gjson.GetBytes(body, "stream_options.include_usage").Bool())

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, strings.Join([]string{
			`data: {"id":"chatcmpl_stream_allowlist","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
			"",
			`data: {"id":"chatcmpl_stream_allowlist","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[],"usage":{"prompt_tokens":4,"completion_tokens":0,"total_tokens":4}}`,
			"",
			`data: [DONE]`,
			"",
		}, "\n"))
	}))
	defer server.Close()

	var buf bytes.Buffer
	_, err := ExecuteCodexGatewayDeepSeekStream(
		context.Background(),
		server.Client(),
		server.URL,
		"test-key",
		model,
		req,
		nil,
		CodexGatewayDeepSeekRequestContext{SessionKey: "session_stream", IsolationKey: "iso_stream"},
		CodexGatewayDeepSeekRequestConfig{},
		&buf,
	)
	require.NoError(t, err)
}

func TestCodexGatewayDeepSeekRequest_FunctionCallsToolChoiceAndReasoningDisablePolicy(t *testing.T) {
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-flash",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-flash",
	}
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-flash",
		Input: json.RawMessage(`[
			{"type":"function_call","call_id":"call_1","name":"shell","arguments":"{\"cmd\":\"pwd\"}"},
			{"type":"function_call_output","call_id":"call_1","output":"ok"}
		]`),
		Tools:      json.RawMessage(`[{"type":"function","name":"shell","parameters":{"type":"object"}}]`),
		ToolChoice: json.RawMessage(`{"type":"function","name":"shell"}`),
		Reasoning:  json.RawMessage(`{"effort":"minimal"}`),
		RawFields: map[string]json.RawMessage{
			"temperature": json.RawMessage(`0.4`),
		},
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{
		AllowReasoningDisable: true,
	})
	require.NoError(t, err)

	require.Equal(t, map[string]any{"type": "disabled"}, prepared.Body["thinking"])
	require.Equal(t, 0.4, prepared.Body["temperature"])

	toolChoice := prepared.Body["tool_choice"].(map[string]any)
	require.Equal(t, "function", toolChoice["type"])
	require.Equal(t, "shell", toolChoice["function"].(map[string]any)["name"])

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 2)
	require.Equal(t, "", messages[0].(map[string]any)["content"])
	require.Equal(t, "", messages[0].(map[string]any)["reasoning_content"])

	preparedWithSeed, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
		UserID:       "contains space",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	seededUserID, _ := preparedWithSeed.Body["user_id"].(string)
	require.NotEmpty(t, seededUserID)
	require.NotEqual(t, "contains space", seededUserID)
	require.True(t, regexp.MustCompile(`^[A-Za-z0-9_-]{1,512}$`).MatchString(seededUserID))

	_, err = BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:      "deepseek-v4-flash",
		Input:      json.RawMessage(`[{"type":"function_call","call_id":"call_2","name":"shell","arguments":"{\"cmd\":\"pwd\"}"}]`),
		Tools:      json.RawMessage(`[{"type":"function","name":"shell","parameters":{"type":"object"}}]`),
		ToolChoice: json.RawMessage(`{"type":"function","name":"missing_tool"}`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown tool")

	_, err = BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:      "deepseek-v4-flash",
		Input:      json.RawMessage(`[{"type":"function_call","call_id":"call_2b","name":"shell","arguments":"{\"cmd\":\"pwd\"}"}]`),
		Tools:      json.RawMessage(`[{"type":"function","name":"shell","parameters":{"type":"object"}}]`),
		ToolChoice: json.RawMessage(`"missing_tool"`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown tool")

	preparedLegacyPatch, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-flash",
		Input: json.RawMessage(`[{"type":"function_call","call_id":"call_legacy_patch","name":"apply_patch","arguments":"{\"input\":\"*** Begin Patch\\n*** End Patch\\n\"}"}]`),
		Tools: json.RawMessage(`[
			{"type":"custom","name":"edit","custom":{"input_schema":{"type":"object"}}}
		]`),
		ToolChoice: json.RawMessage(`"apply_patch"`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	toolChoiceLegacyPatch := preparedLegacyPatch.Body["tool_choice"].(map[string]any)
	require.Equal(t, "function", toolChoiceLegacyPatch["type"])
	require.Equal(t, "custom__edit", toolChoiceLegacyPatch["function"].(map[string]any)["name"])
	legacyMessages := preparedLegacyPatch.Body["messages"].([]any)
	require.Equal(t, "custom__edit", legacyMessages[0].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"])

	legacyCases := []struct {
		name       string
		toolChoice string
		tools      string
		wantAlias  string
	}{
		{
			name:       "update_plan",
			toolChoice: "update_plan",
			tools:      `[{"type":"function","name":"todowrite","parameters":{"type":"object"}}]`,
			wantAlias:  "todowrite",
		},
		{
			name:       "read_file",
			toolChoice: "read_file",
			tools:      `[{"type":"function","name":"read","parameters":{"type":"object"}}]`,
			wantAlias:  "read",
		},
		{
			name:       "write_file",
			toolChoice: "write_file",
			tools:      `[{"type":"function","name":"write","parameters":{"type":"object"}}]`,
			wantAlias:  "write",
		},
		{
			name:       "execute_bash",
			toolChoice: "execute_bash",
			tools:      `[{"type":"function","name":"bash","parameters":{"type":"object"}}]`,
			wantAlias:  "bash",
		},
	}
	for _, tc := range legacyCases {
		t.Run("legacy_"+tc.name, func(t *testing.T) {
			preparedLegacy, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
				Model:      "deepseek-v4-flash",
				Input:      json.RawMessage(`[{"type":"function_call","call_id":"call_legacy","name":"` + tc.toolChoice + `","arguments":"{}"}]`),
				Tools:      json.RawMessage(tc.tools),
				ToolChoice: json.RawMessage(`"` + tc.toolChoice + `"`),
			}, nil, CodexGatewayDeepSeekRequestContext{
				SessionKey:   "session_1",
				IsolationKey: "user_1",
			}, CodexGatewayDeepSeekRequestConfig{})
			require.NoError(t, err)
			toolChoiceLegacy := preparedLegacy.Body["tool_choice"].(map[string]any)
			require.Equal(t, tc.wantAlias, toolChoiceLegacy["function"].(map[string]any)["name"])
			legacyMessages := preparedLegacy.Body["messages"].([]any)
			require.Equal(t, tc.wantAlias, legacyMessages[0].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"])
		})
	}

	preparedLegacyObjectChoice, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-flash",
		Input: json.RawMessage(`[{"type":"custom_tool_call","call_id":"call_legacy_custom","name":"apply_patch","input":"*** Begin Patch\n*** End Patch\n"}]`),
		Tools: json.RawMessage(`[
			{"type":"custom","name":"edit","custom":{"input_schema":{"type":"object"}}}
		]`),
		ToolChoice: json.RawMessage(`{"type":"custom","function":{"name":"apply_patch"}}`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	toolChoiceLegacyObject := preparedLegacyObjectChoice.Body["tool_choice"].(map[string]any)
	require.Equal(t, "custom__edit", toolChoiceLegacyObject["function"].(map[string]any)["name"])
	legacyObjectMessages := preparedLegacyObjectChoice.Body["messages"].([]any)
	require.Equal(t, "custom__edit", legacyObjectMessages[0].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"])

	_, err = BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:      "deepseek-v4-flash",
		Input:      json.RawMessage(`[{"type":"function_call","call_id":"call_2c","name":"shell","arguments":"{\"cmd\":\"pwd\"}"}]`),
		Tools:      json.RawMessage(`[{"type":"function","name":"shell","parameters":{"type":"object"}}]`),
		ToolChoice: json.RawMessage(`{"type":"bogus"}`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported type")

	_, err = BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:      "deepseek-v4-flash",
		Input:      json.RawMessage(`[{"type":"function_call","call_id":"call_2d","name":"shell","arguments":"{\"cmd\":\"pwd\"}"}]`),
		Tools:      json.RawMessage(`[{"type":"function","name":"shell","parameters":{"type":"object"}}]`),
		ToolChoice: json.RawMessage(`{"type":"shell"}`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported type")

	_, err = BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:      "deepseek-v4-flash",
		Input:      json.RawMessage(`[{"type":"function_call","call_id":"call_2e","name":"shell","arguments":"{\"cmd\":\"pwd\"}"}]`),
		Tools:      json.RawMessage(`[{"type":"function","name":"shell","parameters":{"type":"object"}}]`),
		ToolChoice: json.RawMessage(`{"name":"shell"}`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "tool_choice.type is required")

	preparedCustomChoice, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:      "deepseek-v4-flash",
		Input:      json.RawMessage(`[{"type":"function_call","call_id":"call_3","name":"shell","arguments":"{\"cmd\":\"pwd\"}"}]`),
		Tools:      json.RawMessage(`[{"type":"custom","name":"scratch pad","custom":{"input_schema":{"type":"object"}}}]`),
		ToolChoice: json.RawMessage(`{"type":"custom"}`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	toolChoiceCustom := preparedCustomChoice.Body["tool_choice"].(map[string]any)
	require.Equal(t, "function", toolChoiceCustom["type"])
	require.Equal(t, "custom__scratch_pad", toolChoiceCustom["function"].(map[string]any)["name"])

	preparedCustomPathChoice, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:      "deepseek-v4-flash",
		Input:      json.RawMessage(`[{"type":"function_call","call_id":"call_3b","name":"shell","arguments":"{\"cmd\":\"pwd\"}"}]`),
		Tools:      json.RawMessage(`[{"type":"namespace","name":"browser","tools":[{"type":"custom","name":"scratch pad","custom":{"input_schema":{"type":"object"}}}]}]`),
		ToolChoice: json.RawMessage(`{"type":"custom","name":"browser__scratch pad"}`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	toolChoiceCustomPath := preparedCustomPathChoice.Body["tool_choice"].(map[string]any)
	require.Equal(t, "function", toolChoiceCustomPath["type"])
	require.Equal(t, "browser__custom__scratch_pad", toolChoiceCustomPath["function"].(map[string]any)["name"])

	_, err = BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:      "deepseek-v4-flash",
		Input:      json.RawMessage(`[{"type":"function_call","call_id":"call_5","name":"shell","arguments":"{\"cmd\":\"pwd\"}"}]`),
		Tools:      json.RawMessage(`[{"type":"custom","name":"scratch pad","custom":{"input_schema":{"type":"object"}}}]`),
		ToolChoice: json.RawMessage(`{"type":"custom","name":"missing_custom"}`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown tool")
}

func TestCodexGatewayDeepSeekRequest_RejectsAmbiguousLeafToolNames(t *testing.T) {
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"function_call","call_id":"call_1","name":"open","arguments":"{\"url\":\"https://example.com\"}"}
		]`),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"browser","tools":[{"name":"open","parameters":{"type":"object"}}]},
			{"type":"namespace","name":"tabs","tools":[{"name":"open","parameters":{"type":"object"}}]}
		]`),
	}

	_, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ambiguous tool name")
}

func TestCodexGatewayDeepSeekRequest_RejectsAmbiguousTopLevelAndNamespacedPath(t *testing.T) {
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"function_call","call_id":"call_1","name":"a__b","arguments":"{\"url\":\"https://example.com\"}"}
		]`),
		Tools: json.RawMessage(`[
			{"type":"function","name":"a__b","parameters":{"type":"object"}},
			{"type":"namespace","name":"a","tools":[{"name":"b","parameters":{"type":"object"}}]}
		]`),
	}

	_, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ambiguous tool name")
}

func deepSeekRequestToolFunctionByName(t *testing.T, tools []any, name string) map[string]any {
	t.Helper()
	for _, toolAny := range tools {
		tool, ok := toolAny.(map[string]any)
		if !ok {
			continue
		}
		function, ok := tool["function"].(map[string]any)
		if !ok {
			continue
		}
		if function["name"] == name {
			return function
		}
	}
	require.Failf(t, "deepseek request tool not found", "name=%s tools=%v", name, tools)
	return nil
}

func deepSeekRequestToolFunctionByDescription(t *testing.T, tools []any, needle string) map[string]any {
	t.Helper()
	for _, toolAny := range tools {
		tool, ok := toolAny.(map[string]any)
		if !ok {
			continue
		}
		function, ok := tool["function"].(map[string]any)
		if !ok {
			continue
		}
		if strings.Contains(fmt.Sprint(function["description"]), needle) {
			return function
		}
	}
	require.Failf(t, "deepseek request tool not found by description", "needle=%s tools=%v", needle, tools)
	return nil
}
