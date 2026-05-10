# Codex Gateway Design

Date: 2026-05-10
Status: Draft for user review
Target branch: `codex/augment-gateway-multi-model`

## Goal

Add a new `codex_gateway` module to sub2api. It exposes an OpenAI Responses-compatible provider for Codex Desktop and Codex CLI, while routing each requested model to the correct upstream provider.

The first supported model group should include:

- Native Responses models, such as GPT/Codex models, forwarded through the existing OpenAI gateway path with minimal normalization.
- DeepSeek V4 models, such as `deepseek-v4-pro` and `deepseek-v4-flash`, adapted from Codex Responses requests to DeepSeek OpenAI Chat Completions requests and then back to Responses output/events.

The gateway must be designed for Codex's full agent surface, including built-in shell/edit tools, MCP tools, app/plugin tools, Computer Use, Chrome/browser plugins, and future namespace/freeform tools. It should not be a text-only compatibility shim.

## Non-Goals

- Do not merge this behavior into `augment_gateway`; Codex compatibility has a different protocol contract and state model.
- Do not make DeepSeek appear as a native Responses implementation internally. DeepSeek V4 remains a Chat Completions upstream with a translation layer.
- Do not enable Responses WebSocket advertising in the first implementation milestone unless the HTTP Responses contract is already passing.
- Do not claim full hosted OpenAI tool support for DeepSeek where DeepSeek cannot execute hosted tools. For DeepSeek, the target is Codex-local tool orchestration through function/namespace/custom-tool adaptation.

## Evidence

Codex custom providers only support `wire_api = "responses"`. The Codex source rejects the old `chat` wire API, and the official config reference lists `responses` as the only supported value.

Codex sends Responses requests with fields such as `model`, `instructions`, `input`, `tools`, `tool_choice`, `parallel_tool_calls`, `reasoning`, `store`, `stream`, `include`, `service_tier`, `prompt_cache_key`, `text`, and `client_metadata`.

Codex consumes Responses stream events, especially:

- `response.created`
- `response.output_item.added`
- `response.output_text.delta`
- `response.custom_tool_call_input.delta`
- `response.reasoning_summary_text.delta`
- `response.reasoning_text.delta`
- `response.output_item.done`
- `response.completed`
- `response.failed`
- `response.incomplete`

Codex treats a stream that closes before `response.completed` as an error.

Codex model metadata comes from `/models` and/or `model_catalog_json`. Important fields include `slug`, `display_name`, `default_reasoning_level`, `supported_reasoning_levels`, `shell_type`, `visibility`, `support_verbosity`, `apply_patch_tool_type`, `web_search_tool_type`, `supports_parallel_tool_calls`, `context_window`, `auto_compact_token_limit`, `input_modalities`, `supports_image_detail_original`, `supports_search_tool`, and `experimental_supported_tools`.

DeepSeek V4 official docs confirm:

- Models are `deepseek-v4-pro` and `deepseek-v4-flash`.
- OpenAI format base URL remains `https://api.deepseek.com`.
- The OpenAI-compatible endpoint is `/chat/completions`.
- Thinking mode defaults to enabled.
- OpenAI-format thinking controls are `thinking: {"type":"enabled"}` plus `reasoning_effort: "high" | "max"`.
- `low` and `medium` map to `high`; `xhigh` maps to `max`.
- Thinking mode ignores `temperature`, `top_p`, `presence_penalty`, and `frequency_penalty`.
- In thinking mode with tool calls, prior assistant `reasoning_content` must be passed back in subsequent requests, otherwise DeepSeek can return 400.
- Tool calls support function tools; strict mode is beta and has JSON Schema limitations.
- Context caching is enabled by default; cache hit usage is exposed as `prompt_cache_hit_tokens`.

Existing Augment Gateway DeepSeek code already handles several reusable pitfalls:

- Force `thinking: {"type":"enabled"}`.
- Default DeepSeek V4 reasoning effort to `max`.
- Preserve empty string fields such as `reasoning_content`.
- Add empty `content` on assistant tool-call messages.
- Add empty `reasoning_content` during tool loops when missing.
- Pair contiguous assistant tool-call messages with their corresponding tool result messages.
- Inject a stable `user_id` for DeepSeek cache/user isolation.

## Architecture

Add a new service boundary parallel to Augment Gateway:

```text
Codex Desktop / Codex CLI
        |
        | OpenAI-compatible provider config
        | wire_api = "responses"
        v
codex_gateway HTTP ingress
        |
        +-- model catalog / model registry
        +-- request normalization
        +-- Responses state store
        +-- provider executor
              |
              +-- GPT/Codex native Responses adapter
              |     -> existing OpenAI Gateway Responses forwarding
              |
              +-- DeepSeek V4 adapter
                    -> Responses to Chat Completions request
                    -> DeepSeek /chat/completions
                    -> Chat Completion chunks to Responses SSE events
```

The module should share account selection, group configuration, usage recording, request-size handling, zstd/gzip/deflate body decoding, logging, and error passthrough patterns with existing gateway services.

The module should not share Augment-specific request shapes, Augment auth runtime endpoints, or Augment conversation node semantics.

## Public Surface

The initial external surface should be:

- `GET /codex/v1/models`
- `GET /v1/models` when routed through a Codex Gateway-specific mount or key scope
- `POST /codex/v1/responses`
- `POST /v1/responses` when routed through a Codex Gateway-specific mount or key scope

The exact route prefix can follow existing OpenAI gateway route conventions, but the Codex-specific behavior should be selectable by client product/scope or provider group, not by guessing from arbitrary user agents.

Future optional surface:

- `POST /v1/responses/compact`
- Responses WebSocket on `/v1/responses`

## Codex Configuration Contract

The gateway should produce a ready-to-use Codex config snippet:

```toml
model_provider = "sub2api-codex"
model = "gpt-5.5"
model_catalog_json = "/path/to/sub2api-codex-models.json"

[model_providers.sub2api-codex]
name = "Sub2API Codex Gateway"
base_url = "http://127.0.0.1:3000/codex/v1"
env_key = "SUB2API_CODEX_API_KEY"
wire_api = "responses"
supports_websockets = false
```

`supports_websockets` should remain false until the gateway implements and tests the Codex Responses WebSocket v2 protocol.

## Model Registry

Add a new Codex model registry instead of reusing the Augment registry directly.

Suggested default visible models:

- `gpt-5.5`
- `gpt-5.4`
- `gpt-5.4-mini`
- `gpt-5.3-codex`
- `deepseek-v4-pro`
- `deepseek-v4-flash`

Each model entry should include:

- Public Codex model id.
- Provider: `openai` or `deepseek`.
- Upstream model id.
- Provider group id.
- Visibility/enabled state.
- Context window and auto-compact threshold.
- Reasoning support and default effort.
- Tool capability flags.
- Whether to allow images.
- Whether to allow verbosity.
- Whether to allow service tiers.

Recommended DeepSeek metadata:

- `context_window`: 1000000
- `auto_compact_token_limit`: conservative value below the real context, for example 850000 to leave space for tools/output
- `default_reasoning_level`: `xhigh` or `high`, mapped internally to DeepSeek `max`
- `supported_reasoning_levels`: `high`, `xhigh`; optionally accept `low`/`medium` from Codex but normalize to `high`
- `supports_parallel_tool_calls`: true only after conformance tests prove stable multi-call behavior
- `support_verbosity`: false
- `input_modalities`: `["text"]` unless image handling is explicitly validated
- `shell_type`: match the safest Codex local tool mode already used in model catalog defaults

## Request Handling

The HTTP handler should:

1. Authenticate the API key and enforce Codex Gateway scope.
2. Read and decode the body using the existing request-body utility that supports `Content-Encoding: zstd`, `gzip`, and `deflate`.
3. Parse the body as a generic Responses request first, preserving unknown fields for pass-through adapters.
4. Resolve the requested model through the Codex model registry.
5. Select a provider account through the existing group/account scheduler.
6. Dispatch to the selected provider adapter.
7. Normalize usage and errors into Responses format.
8. Record usage with provider/model metadata.

For streaming requests, the handler must set:

- `Content-Type: text/event-stream`
- `Cache-Control: no-cache`
- streaming flush behavior matching existing OpenAI gateway conventions

For non-streaming requests, the handler should return a complete Responses JSON object.

Codex usually sends `stream: true`, but non-streaming behavior should exist for probes and compatibility tests.

## Native Responses Adapter

For GPT/Codex native Responses models:

- Forward the request through the existing OpenAI Gateway Responses path.
- Preserve Codex headers relevant to tracing/session behavior where existing gateway policy allows it.
- Preserve `prompt_cache_key`.
- Preserve `reasoning`, `text`, `tools`, `tool_choice`, `parallel_tool_calls`, `include`, and `client_metadata` unless an upstream account is known not to support a field.
- Keep existing unsupported-field retry/cache behavior from OpenAI Gateway where applicable.
- Normalize response usage into the same internal usage object used by Codex Gateway.

This adapter should be thin. Its main job is to place Codex Gateway routing, model registry, billing, and scope checks around existing OpenAI Responses forwarding.

## DeepSeek Adapter

The DeepSeek adapter is the hard part. It must translate between two different state machines:

- Codex Responses conversation items and stream events.
- DeepSeek Chat Completions messages and chunks.

### Responses Input to DeepSeek Messages

Input conversion should support:

- `message` input items with `input_text` and `output_text` content.
- `function_call` output items from prior assistant turns.
- `function_call_output` input items from Codex tool results.
- `custom_tool_call` and `custom_tool_call_output`.
- `local_shell_call` and related tool outputs where they are model-visible.
- `reasoning` items where DeepSeek needs `reasoning_content` replay.
- Unknown or hosted-only Responses items with conservative degradation.

Conversion rules:

- `instructions` becomes a leading `system` message.
- User `message` items become `role: "user"` messages.
- Assistant `message` items become `role: "assistant"` messages.
- Assistant `function_call` items become `role: "assistant"` messages with `tool_calls`.
- `function_call_output` becomes `role: "tool"` with `tool_call_id`.
- Custom/freeform tool calls are encoded as DeepSeek function tool calls with a reversible name mapping.
- Namespace tools are flattened to function names using a reversible, collision-safe encoding.
- Structured tool outputs are serialized as text or compact JSON according to the original output payload.
- Empty `content` must be present on assistant tool-call messages.
- During any DeepSeek tool loop, assistant messages that participated in a tool call must carry `reasoning_content`; if none is known, send an empty string rather than omitting the field.

The converter must avoid Go structs with `omitempty` for DeepSeek messages, because empty `content` and empty `reasoning_content` can be protocol-significant.

### Tool Conversion

Responses tools should be converted to DeepSeek `tools` where possible:

- `type: "function"` maps directly to DeepSeek function tools.
- `type: "namespace"` maps each namespace function to a flattened function tool name.
- `strict` should default to false for DeepSeek unless using beta endpoint is explicitly enabled.
- `defer_loading` is a Codex/client hint and should not be sent upstream.
- `output_schema` is not a DeepSeek tool schema field and should not be sent upstream.

Name mapping requirements:

- Reversible.
- Stable across request and response conversion.
- Fits DeepSeek function name constraints: letters, digits, underscores, dashes, max length 64.
- Handles collisions deterministically.

Suggested strategy:

- Keep ordinary names as-is when valid and unique.
- Encode namespace tools as `ns__<short_hash>__<safe_tool_name>`.
- Maintain a per-request map from encoded name to original `{namespace, name, tool_kind}`.
- Include enough metadata in the Codex Gateway state store to decode future tool outputs.

For custom/freeform tools:

- Treat them as function tools from DeepSeek's perspective.
- Use a synthetic JSON schema with a single string field such as `input` when the original tool is freeform.
- Convert returned DeepSeek function arguments back to `custom_tool_call.input`.
- Stream custom tool input via `response.custom_tool_call_input.delta` where feasible.

### DeepSeek Request Parameters

For DeepSeek V4 requests:

- Set `model` to the upstream DeepSeek model.
- Set `thinking: {"type":"enabled"}` by default.
- Set `reasoning_effort` from Codex effort:
  - `none` / `minimal`: consider `thinking: {"type":"disabled"}` only if the model entry allows non-thinking mode.
  - `low` / `medium` / `high`: `high`
  - `xhigh`: `max`
  - default: `max` for Codex agent workloads.
- Set `stream` based on the client request.
- If streaming, set `stream_options.include_usage = true`.
- Send `tools` only when non-empty.
- Remove `tool_choice` when DeepSeek compatibility requires it; otherwise map `auto`, `none`, `required`, and forced function choices carefully.
- Do not send Responses-only fields like `store`, `include`, `previous_response_id`, `prompt_cache_key`, `client_metadata`, `service_tier`, or `text` to DeepSeek.
- Do not send thinking-incompatible sampling params in thinking mode.
- Inject stable `user_id` from Codex session/user/API key/account scope.

### DeepSeek Stream to Responses SSE

For each DeepSeek streamed Chat Completion:

1. Emit `response.created`.
2. When the first assistant output begins, emit `response.output_item.added`.
3. For text deltas, emit `response.output_text.delta`.
4. For reasoning deltas, emit `response.reasoning_text.delta` or `response.reasoning_summary_text.delta` depending on Codex model metadata and request `include`.
5. For tool-call argument deltas, accumulate by tool index and emit suitable tool input deltas for custom tools where applicable.
6. On `finish_reason: "tool_calls"`, emit one `response.output_item.done` per function/custom tool call.
7. On natural completion, emit a final assistant `message` item via `response.output_item.done`.
8. Emit `response.completed` with normalized usage.

The final `response.completed` event is mandatory.

Usage mapping:

- DeepSeek `prompt_tokens` -> Responses `input_tokens`
- DeepSeek `completion_tokens` -> Responses `output_tokens`
- DeepSeek `total_tokens` -> Responses `total_tokens`
- DeepSeek `prompt_cache_hit_tokens` -> Responses `input_tokens_details.cached_tokens`
- DeepSeek `completion_tokens_details.reasoning_tokens` -> Responses `output_tokens_details.reasoning_tokens`

### Non-Streaming DeepSeek Responses

For non-streaming:

- Build a complete Responses JSON object.
- Populate `id`, `object: "response"`, `created_at`, `status`, `model`, `output`, `parallel_tool_calls`, `previous_response_id`, `reasoning`, `tool_choice`, `tools`, `usage`, and `metadata`.
- Use the same item conversion logic as the stream finalizer.

## Responses State Store

DeepSeek requires state that Chat Completions does not provide in the same shape as Responses. Add a Codex Gateway state store.

Required stored data:

- `response_id`
- `previous_response_id`
- public Codex model id
- upstream model id
- provider
- conversation/session key
- assistant output items
- tool call ids
- encoded tool name to original tool mapping
- assistant text content
- DeepSeek `reasoning_content`
- whether reasoning content was present or synthesized
- usage
- stream completion status
- upstream request id
- created timestamp and TTL

Lookup requirements:

- Resolve `previous_response_id` when Codex sends a compact/incremental continuation.
- Reconstruct prior assistant tool-call messages for DeepSeek.
- Reattach `reasoning_content` for DeepSeek tool loops.
- Map tool outputs back to the exact DeepSeek `tool_call_id`.
- Detect model/provider changes and avoid replaying incompatible state.

Storage requirements:

- Start with in-memory TTL storage for MVP, matching Augment Gateway's current DeepSeek replay store approach.
- Keep no tokens, Authorization headers, full request bodies, or secret tool outputs beyond what is needed for model-visible replay.
- Design the interface so it can later move to Redis/database storage if multi-instance deployment requires it.

## Errors

Responses-compatible errors should be returned for HTTP failures:

```json
{
  "error": {
    "type": "invalid_request_error",
    "code": "invalid_request",
    "message": "..."
  }
}
```

For stream failures after streaming has started, emit `response.failed` when possible. If upstream disconnects without completion, close with a clear gateway error event rather than silently ending the stream.

Error mappings:

- DeepSeek context length -> `context_length_exceeded`
- DeepSeek invalid request -> `invalid_request_error`
- DeepSeek rate limit -> `rate_limit_error`
- DeepSeek insufficient resource -> retryable/server overloaded style error
- Provider/account exhaustion -> existing gateway failover error style, wrapped in Responses format

## WebSocket Phase

Do not enable `supports_websockets` initially.

A later WebSocket milestone should implement:

- WebSocket upgrade on `/v1/responses`.
- OpenAI beta header handling for `responses_websockets=2026-02-06`.
- `response.create` request frames.
- `response.processed` frames.
- `generate=false` prewarm.
- Per-turn `x-codex-turn-state`.
- Reuse rules for `previous_response_id`.
- HTTP fallback behavior on unsupported/failed WS.

The existing `openai_ws_forwarder.go` has useful patterns, but Codex Gateway should not advertise WS until it has contract tests against Codex's websocket suite behavior.

## Testing Strategy

Add tests before implementation code changes for each conversion boundary.

Unit tests:

- Responses request body decoding with `Content-Encoding: zstd`.
- Codex model registry `/models` JSON shape.
- Responses input item to DeepSeek messages.
- DeepSeek tools flattening and reverse mapping.
- DeepSeek reasoning effort mapping.
- DeepSeek usage mapping, especially cache hit tokens.
- DeepSeek tool-call chunk accumulation.
- Responses SSE event finalization with mandatory `response.completed`.
- Error response format.

Golden tests:

- Simple text turn.
- Function tool call turn.
- Multiple parallel tool calls.
- Namespace/MCP tool call.
- Custom/freeform `apply_patch`-style call.
- Tool result follow-up.
- Thinking mode tool loop with `reasoning_content` replay.
- DeepSeek stream with usage chunk.

Integration-style tests:

- Run a fake DeepSeek `/chat/completions` upstream and assert Codex Gateway emits valid Responses SSE.
- Run a fake native Responses upstream and assert pass-through preserves Codex fields.
- Use representative Codex source fixtures for `ResponseItem` variants and stream parser expectations.

Manual validation:

- Configure Codex CLI with the generated provider config.
- Verify model list displays both GPT and DeepSeek models.
- Run simple chat.
- Run shell command tool.
- Run file edit/apply patch flow.
- Run MCP/resource listing flow.
- Run plugin/app tool flow where available.
- Run Computer Use/Chrome plugin flow if the local Codex Desktop environment exposes those tools.

## Implementation Milestones

### Milestone 1: Protocol Skeleton

- Add Codex Gateway config structs.
- Add Codex Gateway model registry.
- Add `/models` response builder.
- Add handler skeleton for `/responses`.
- Add zstd-aware request body reading.
- Add model/provider dispatch.
- Add tests for registry and request parsing.

### Milestone 2: Native Responses Adapter

- Route GPT/Codex models through existing OpenAI Responses forwarding.
- Preserve Codex-specific request fields.
- Normalize usage and errors.
- Add pass-through tests.

### Milestone 3: DeepSeek Non-Streaming Adapter

- Convert Responses input/tools to DeepSeek Chat Completions.
- Convert DeepSeek non-streaming output to Responses JSON.
- Add state store write/read for `response_id` and tool calls.
- Add reasoning replay tests.

### Milestone 4: DeepSeek Streaming Adapter

- Convert DeepSeek SSE chunks to Responses SSE events.
- Accumulate text, reasoning, and tool-call deltas.
- Guarantee `response.completed`.
- Add usage mapping.
- Add stream golden tests.

### Milestone 5: Codex Tool Compatibility

- Add namespace tool flattening.
- Add custom/freeform tool conversion.
- Add local shell/apply patch compatibility tests.
- Add MCP/plugin tool compatibility tests.

### Milestone 6: Operational Integration

- Add admin settings for Codex Gateway provider groups and model visibility.
- Add smoke test endpoints or reuse existing gateway health style.
- Add billing/usage model mapping.
- Add logging and metrics.

### Milestone 7: WebSocket Evaluation

- Implement only if HTTP compatibility is stable.
- Add WebSocket v2 frame tests before enabling `supports_websockets`.

## Open Questions

1. Route prefix: should Codex Gateway be exposed under `/codex/v1/*`, or should `/v1/*` behavior be selected by API key/client product?
2. DeepSeek non-thinking mode: should `none`/`minimal` reasoning disable thinking, or should Codex Gateway force thinking for all DeepSeek agent turns?
3. Model visibility: should DeepSeek models be visible by default once configured, or hidden until smoke tests pass like Augment Gateway?
4. State store: is in-memory TTL acceptable for the first deployment, or does this environment require multi-instance persistence immediately?
5. Strict mode: should DeepSeek strict tools be disabled until beta endpoint support is explicitly configured?

## Recommendation

Use a new `codex_gateway` module with a provider registry and adapter interface parallel to `augment_gateway`.

Implement HTTP Responses first and keep `supports_websockets = false` until the Responses SSE and tool-loop contract is proven. Reuse Augment Gateway's DeepSeek sanitizer ideas, but build a Codex-specific Responses state store and tool conversion layer because Codex has a broader item/tool model than Augment.

The first implementation plan should cover Milestones 1 through 5. WebSocket should remain a separate follow-up plan after Codex CLI/Desktop HTTP validation passes.

