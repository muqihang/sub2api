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
- Do not implement Codex app-server v2 JSON-RPC in sub2api. Codex Gateway is a model provider, not a replacement for the Codex Desktop/App control plane.

## Protocol Boundary: App Server v2 vs Model Provider

Codex Desktop/App Server v2 is the local control-plane protocol between the desktop app and Codex core. Official app-server v2 methods include `thread/start`, `turn/start`, `thread/inject_items`, `model/list`, `modelProvider/capabilities/read`, `plugin/list`, `app/list`, `mcpServer/resource/read`, `mcpServer/tool/call`, `tool/requestUserInput`, and `fs/*`.

Those JSON-RPC-style app-server methods are not sent to custom model providers. Codex Gateway should not implement or proxy them.

The provider-facing contract remains:

- `GET /models?client_version=...` returning a Codex/OpenAI model catalog envelope.
- `POST /responses` with OpenAI Responses JSON and, for streaming, Responses SSE events.
- Optional Responses-over-WebSocket frames only when `supports_websockets = true`.

Plugins, apps, MCP servers, Computer Use, Chrome/browser integrations, built-in shell/edit tools, and dynamic client tools enter model context through Codex core. Core registers them as built-in tools, MCP namespace tools, dynamic tools, deferred tool-search entries, or local handlers. The model provider sees only the model-visible `tools` array and later `input` items containing tool results.

Therefore, the DeepSeek adapter only needs to translate model-visible Responses tools/items. It must not depend on app-server method names such as `item/tool/call`, `mcpServer/resource/read`, or `fs/readFile`.

## Evidence

Codex custom providers only support `wire_api = "responses"`. The Codex source rejects the old `chat` wire API, and the official config reference lists `responses` as the only supported value.

Codex sends Responses requests with fields such as `model`, `instructions`, `input`, `tools`, `tool_choice`, `parallel_tool_calls`, `reasoning`, `store`, `stream`, `include`, `service_tier`, `prompt_cache_key`, `max_output_tokens`, `text`, and `client_metadata`.

Codex consumes Responses stream events, especially:

- `response.created`
- `response.output_item.added`
- `response.output_text.delta`
- `response.function_call_arguments.delta`
- `response.function_call_arguments.done`
- `response.custom_tool_call_input.delta`
- `response.reasoning_summary_part.added`
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
- Context caching is enabled by default; usage exposes `prompt_cache_hit_tokens` and `prompt_cache_miss_tokens`.
- Streaming with `stream_options.include_usage = true` adds a usage-only chunk before `[DONE]` with `choices: []`.

Existing Augment Gateway DeepSeek code already handles several reusable pitfalls:

- Force `thinking: {"type":"enabled"}`.
- Default DeepSeek V4 reasoning effort to `max`.
- Preserve empty string fields such as `reasoning_content`.
- Add empty `content` on assistant tool-call messages.
- Add empty `reasoning_content` for synthesized compatibility messages; Codex Gateway should be stricter for real DeepSeek tool-call messages and fail closed if raw reasoning is missing.
- Pair contiguous assistant tool-call messages with their corresponding tool result messages.
- Inject a stable, sanitized `user_id` for DeepSeek cache/user isolation.

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

## Implementation Boundary

The implementation should add an explicit `codex_gateway` slice instead of branching inside the existing OpenAI or Augment handlers.

Expected new backend files:

- `backend/internal/handler/codex_gateway_handler.go`
- `backend/internal/server/routes/codex_gateway.go`
- `backend/internal/service/codex_gateway_types.go`
- `backend/internal/service/codex_gateway_service.go`
- `backend/internal/service/codex_gateway_model_registry.go`
- `backend/internal/service/codex_gateway_provider_executor.go`
- `backend/internal/service/codex_gateway_openai_responses_adapter.go`
- `backend/internal/service/codex_gateway_deepseek_adapter.go`
- `backend/internal/service/codex_gateway_state_store.go`
- `backend/internal/service/codex_key_scope_policy.go`

The Codex Gateway package may reuse small existing interfaces for account scheduling, API key lookup, usage recording, HTTP clients, body decoding, and OpenAI native Responses forwarding. It must not depend on these Augment-specific types or registries:

- `AugmentGatewayProviderRequest`
- `AugmentGatewayProviderResult`
- `AugmentGatewayProviderChunk`
- `AugmentGatewayModelRegistry`
- Augment admin route namespaces
- Augment auth/runtime conversation shapes

The native GPT/Codex adapter should call a service-layer forwarder, not the existing HTTP handler. The handler-level OpenAI Responses validation path already has assumptions that are not Codex-Gateway-specific, so the new Codex handler should own request parsing, model resolution, key scope checks, state-store interaction, and adapter dispatch.

Define a narrow `NativeResponsesForwarder` interface with input fields for authenticated API key/user, selected account, resolved Codex model entry, raw request body, preserved Codex headers, and adapter options. It should return a unified `CodexGatewayForwardResult` used by billing and logging for both native and DeepSeek paths.

## Public Surface

The initial external surface should be:

- `GET /codex/v1/models`
- `POST /codex/v1/responses`
- `GET /v1/models` only when `expose_v1_alias = true` and the key is scoped to `codex_gateway`
- `POST /v1/responses` only when `expose_v1_alias = true` and the key is scoped to `codex_gateway`

The MVP should make `/codex/v1/*` the canonical route and require a Codex-scoped key. `/v1/*` aliasing is optional and must be selected by key scope or explicit config, not by guessing from user agents.

Shared `/v1/*` dispatcher rule:

- Register `/codex/v1/*` independently from existing OpenAI Gateway routes.
- Keep `expose_v1_alias = false` for MVP unless a shared-path dispatcher is implemented.
- If `/v1/models` or `/v1/responses` aliasing is enabled, routing must run through a pre-auth dispatcher that first authenticates the key enough to read `restricted_client_product`.
- Keys with `restricted_client_product = "codex_gateway"` dispatch to Codex Gateway handlers.
- Keys without that restriction continue to the existing OpenAI Gateway handlers.
- Existing OpenAI `/v1/*` middleware must not reject Codex-scoped keys before the dispatcher can route them.
- Contract tests must cover route registration order for OpenAI-scoped, Codex-scoped, Augment-scoped, invalid, and missing keys.

Authorization and entitlement rules:

- Add `groups.codex_gateway_entitled BOOLEAN NOT NULL DEFAULT FALSE`.
- Extend the API key admin DTO to support either a generic `restricted_client_product` field or a dedicated `codex_only` alias that maps to `restricted_client_product = "codex_gateway"`.
- Add a Codex key scope policy that allows Codex-scoped keys only on `/codex/v1/models`, `/codex/v1/responses`, and enabled `/v1/*` aliases.
- Return 403 for valid keys whose group is not Codex-entitled.
- Return 403 for keys scoped to another client product.
- Do not share Augment-only path allowlists with Codex.

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
request_max_retries = 4
stream_max_retries = 5
stream_idle_timeout_ms = 300000
```

`model_catalog_json` is a local startup-only Codex config override, not a remote endpoint. The generated file must be non-empty and should be regenerated when gateway model visibility changes.

`supports_websockets` should remain false until the gateway implements and tests the Responses-over-WebSocket transport used by Codex.

## Model Registry

Add a new Codex model registry instead of reusing the Augment registry directly.

`GET /codex/v1/models` must accept an optional `client_version` query parameter and return a `ModelsResponse` envelope:

```json
{
  "models": [
    {
      "slug": "deepseek-v4-pro",
      "display_name": "DeepSeek V4 Pro",
      "supported_in_api": true,
      "priority": 50,
      "visibility": "visible",
      "default_reasoning_level": "xhigh",
      "supported_reasoning_levels": ["high", "xhigh"],
      "supports_reasoning_summaries": true,
      "default_reasoning_summary": "auto",
      "support_verbosity": false,
      "supports_parallel_tool_calls": false,
      "context_window": 1000000,
      "auto_compact_token_limit": 850000,
      "input_modalities": ["text"],
      "supports_image_detail_original": false,
      "supports_search_tool": false,
      "experimental_supported_tools": ["function", "namespace", "custom"],
      "shell_type": "local"
    }
  ]
}
```

The response may include an `ETag`; stream responses may surface `X-Models-Etag` if the upstream/native path provides it.

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
- Whether it is `supported_in_api`.
- Priority and visibility.
- `base_instructions`, if needed for Codex model behavior.
- Reasoning summary support and default summary policy.
- Default verbosity.
- Truncation/auto-compact policy.
- Service tier metadata.
- Availability/upgrade/model message metadata where Codex expects it.
- `supports_personality` behavior.

Recommended DeepSeek metadata:

- `context_window`: 1000000
- `auto_compact_token_limit`: conservative value below the real context, for example 850000 to leave space for tools/output
- `default_reasoning_level`: `xhigh` or `high`, mapped internally to DeepSeek `max`
- `supported_reasoning_levels`: `high`, `xhigh`; optionally accept `low`/`medium` from Codex but normalize to `high`
- `supports_parallel_tool_calls`: true only after conformance tests prove stable multi-call behavior
- `support_verbosity`: false
- `input_modalities`: `["text"]` unless image handling is explicitly validated
- `shell_type`: match the safest Codex local tool mode already used in model catalog defaults

DeepSeek legacy aliases:

- Expose only `deepseek-v4-pro` and `deepseek-v4-flash` by default.
- If compatibility aliases are explicitly enabled, map `deepseek-chat` to `deepseek-v4-flash` with thinking disabled and `deepseek-reasoner` to `deepseek-v4-flash` with thinking enabled.
- Do not map legacy aliases to `deepseek-v4-pro`.
- If aliases are disabled, return 400 with a migration message when a client requests an old alias.

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

Compression behavior:

- Supported encodings: identity, `zstd`, `gzip`, `deflate`.
- Unsupported `Content-Encoding` returns a Responses-compatible 400.
- Malformed compressed bodies return a Responses-compatible 400.
- Decompressed bodies over the configured limit return 413.
- The internal forwarded/native request should not retain the original `Content-Encoding` after decoding.

Codex-relevant headers should be read for state keys, telemetry, account stickiness, and native pass-through where policy allows:

- `session_id` and `session-id`
- `thread_id` and `thread-id`
- `x-client-request-id`
- `x-openai-subagent`
- `x-codex-beta-features`
- `x-codex-turn-state`
- `x-codex-turn-metadata`
- tracing and identity/attestation headers where existing gateway policy permits

HTTP `client_metadata` body fields should be preserved, but current Codex app-server turn metadata is primarily visible to providers through `x-codex-turn-metadata`. In the later WebSocket transport, that metadata may also appear inside `client_metadata`.

Current Codex HTTP Responses provider requests do not include `previous_response_id`. The DeepSeek HTTP adapter must derive replay context from the full `input` items plus gateway state such as call IDs, emitted response IDs, session/thread headers, and tool-name maps. `previous_response_id` is a WebSocket incremental optimization in current Codex source. If a third-party client sends `previous_response_id` to the Codex DeepSeek HTTP path, MVP behavior should be an explicit 400 unless a later compatibility flag implements and tests that state lookup.

For streaming requests, the handler must set:

- `Content-Type: text/event-stream`
- `Cache-Control: no-cache`
- streaming flush behavior matching existing OpenAI gateway conventions

For non-streaming requests, the handler should return a complete Responses JSON object.

Codex usually sends `stream: true`, but non-streaming behavior should exist for probes and compatibility tests.

Account/session stickiness:

- Prefer session hash seed order: `prompt_cache_key`, `x-codex-turn-state`, `session_id`, `thread_id`, stable `client_metadata`, then request body hash fallback.
- Include API key/user isolation in the hash seed.
- DeepSeek upstream `user_id` must satisfy DeepSeek's `[a-zA-Z0-9-_]` and max-512-character constraint. Use a sanitized, non-reversible value such as `codex_gateway_<base64url_sha256(api_key_id || user_id || session_hash)>`; do not send pipe delimiters or raw user identifiers.
- DeepSeek models use `gateway.codex.provider_groups.deepseek`; native GPT/Codex models use `gateway.codex.provider_groups.openai`.

## Config, Admin, and Migrations

Add `GatewayCodexConfig` under the existing gateway config tree:

```yaml
gateway:
  codex:
    enabled: true
    expose_v1_alias: false
    model_catalog_path: ""
    supports_websockets: false
    state_store_ttl_seconds: 86400
    max_state_items: 200
    stream_max_line_size: 1048576
    enabled_models:
      - gpt-5.5
      - gpt-5.4
      - gpt-5.4-mini
      - gpt-5.3-codex
      - deepseek-v4-pro
      - deepseek-v4-flash
    provider_groups:
      openai: codex-openai
      deepseek: codex-deepseek
```

Normalize/validate rules:

- `gateway.codex.enabled = false` disables all `/codex/v1/*` routes.
- `supports_websockets` must stay false unless the WebSocket milestone is complete.
- DeepSeek models require a configured DeepSeek provider group.
- Native GPT/Codex models require a configured OpenAI provider group.
- Visible models require explicit enabled state, correct pricing metadata, provider group health, and passing smoke tests unless an admin override marks them hidden-but-selectable.
- DeepSeek Codex models must remain hidden until first-class cache-hit/cache-miss pricing is configured and usage/billing tests pass.
- `model_catalog_path`, when set, should be regenerated from the same registry used by `/codex/v1/models`.

Admin API should mirror Augment's operational shape while staying separate:

- `GET /api/v1/admin/codex-gateway/summary`
- `GET/PUT /api/v1/admin/codex-gateway/provider-groups`
- `GET/PUT /api/v1/admin/codex-gateway/models/:id`
- `POST /api/v1/admin/codex-gateway/smoke`
- `GET /api/v1/admin/codex-gateway/state-store/summary`

Migrations:

- Add `groups.codex_gateway_entitled`.
- Extend API key create/update/read payloads for `restricted_client_product = "codex_gateway"` or `codex_only`.
- Add usage metadata support for Codex Gateway fields listed in the Usage section.
- If settings are versioned in SQL, add a `codex_gateway_settings_versions` table or a generic gateway settings version row; do not reuse Augment settings versioning.

## Native Responses Adapter

For GPT/Codex native Responses models:

- Forward the request through the existing OpenAI Gateway Responses path.
- Preserve Codex headers relevant to tracing/session behavior where existing gateway policy allows it.
- Preserve `prompt_cache_key`.
- Preserve `reasoning`, `text`, `tools`, `tool_choice`, `parallel_tool_calls`, `include`, and `client_metadata` unless an upstream account is known not to support a field.
- Keep existing unsupported-field retry/cache behavior from OpenAI Gateway where applicable.
- Normalize response usage into the same internal usage object used by Codex Gateway.

This adapter should be thin. Its main job is to place Codex Gateway routing, model registry, billing, and scope checks around existing OpenAI Responses forwarding.

Allowed native path behavior:

- Preserve existing unsupported-field retry/cache handling for OpenAI-compatible upstreams.
- Preserve prompt-cache/session headers that are safe to forward.
- Preserve image generation/web search/local tool specs only where the resolved upstream model and account support them.
- Preserve Codex stream response headers such as `x-request-id`, `x-codex-turn-state`, `X-Reasoning-Included`, `X-Models-Etag`, `OpenAI-Model`, and `X-OpenAI-Model` when upstream provides them and policy allows downstream exposure.

Behavior to avoid in the Codex native adapter:

- Do not route through the existing handler-level `/v1/responses` validation path.
- Do not let DeepSeek state-store logic alter native OpenAI Responses requests.
- Do not apply Augment-specific model mapping, runtime auth, or conversation transforms.
- Do not enable `/codex/v1/responses/compact` or WebSocket by reusing existing OpenAI routes unless the Codex Gateway contract tests explicitly cover them.

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
- `tool_search_call` and `tool_search_output` as Codex client-executed tool discovery/results, not as an upstream hosted search.
- `web_search_call` and `image_generation_call` as prior model-visible items only; DeepSeek should not be advertised as executing hosted web/image tools.
- `compaction` and `context_compaction` items as model-visible transcript markers only when Codex sends them.
- `message.phase`, preserving commentary/final-answer phase where Codex provides it.

Conversion rules:

- `instructions` becomes a leading `system` message.
- User `message` items become `role: "user"` messages.
- Assistant `message` items become `role: "assistant"` messages.
- Assistant `function_call` items become `role: "assistant"` messages with `tool_calls`.
- `function_call_output` becomes `role: "tool"` with `tool_call_id`.
- Custom/freeform tool calls are encoded as DeepSeek function tool calls with a reversible name mapping.
- Namespace tools are flattened to function names using a reversible, collision-safe encoding.
- Structured tool outputs are serialized as text or compact JSON according to the original output payload. `function_call_output.output` and `custom_tool_call_output.output` may be a string or an array of `input_text`/`input_image` content items; DeepSeek text-only mode must degrade images deterministically with a placeholder such as `[image omitted: <mime/type or detail>]`.
- Empty `content` must be present on assistant tool-call messages.
- Once a DeepSeek thinking-mode turn contains any tool call, raw `reasoning_content` must be saved and replayed byte-for-byte for every DeepSeek-generated assistant message in that tool loop, including later assistant messages that contain no `tool_calls`.
- If the state store cannot find raw `reasoning_content` for any DeepSeek-generated assistant message from a thinking tool loop, do not silently send `""`; return a gateway invalid-state error or use an explicit configured non-thinking recovery path.
- Empty `reasoning_content: ""` is allowed only for gateway-synthesized compatibility messages, and the state entry must record `reasoning_content_synthesized = true`.
- Before sending to DeepSeek, validate that every assistant `tool_calls` message is followed before the next user/assistant/system message by exactly one matching `role: "tool"` message for each `tool_call_id`. Repair only from state store; otherwise return gateway invalid state.
- Tool message `content` must be a string. JSON-stringify structured outputs deterministically.

The converter must avoid Go structs with `omitempty` for DeepSeek messages, because empty `content` and empty `reasoning_content` can be protocol-significant.

DeepSeek adapter support matrix:

| Responses family | DeepSeek behavior |
| --- | --- |
| `message` text | Supported as user/assistant chat messages. |
| `reasoning` | Stored and replayed as DeepSeek `reasoning_content`; may be emitted as reasoning text/summary events. |
| `function` tools and `function_call` | Supported through DeepSeek function tools. |
| `namespace` tools | Flatten to DeepSeek function names with reverse map. |
| `custom`/freeform tools | Encode as synthetic function tools with `{ "input": "..." }` schema. |
| `local_shell`/`apply_patch` | Supported only as Codex-local tool specs/results converted through function/custom forms. |
| `tool_search` | Preserve model-visible discovery/result items; do not call an upstream hosted search. |
| `web_search` and `image_generation` hosted tools | Native OpenAI path may pass through; DeepSeek path should hide, degrade, or reject unless explicitly mapped to local Codex tools. |
| multimodal input/output | DeepSeek path is text-only for MVP; images degrade deterministically or reject when required. |
| unknown item/tool types | Preserve in state if possible, but reject when model-visible semantics cannot be represented safely. |

### Tool Conversion

Responses tools should be converted to DeepSeek `tools` where possible:

- `type: "function"` maps directly to DeepSeek function tools.
- `type: "namespace"` maps each namespace function to a flattened function tool name.
- `strict` should default to false for DeepSeek unless using the DeepSeek beta endpoint is explicitly enabled and validated.
- `defer_loading` is a Codex/client hint and should not be sent upstream.
- `output_schema` is not a DeepSeek tool schema field and should not be sent upstream.
- DeepSeek supports only `function` tools upstream; all Codex tool kinds must be represented through function schemas or rejected/degraded.

DeepSeek strict mode requirements:

- Use `https://api.deepseek.com/beta`.
- All functions in `tools` that rely on strict mode must set `strict: true`.
- Every JSON Schema `object` must list all properties in `required`.
- Every JSON Schema `object` must set `additionalProperties: false`.
- Supported schema families include `object`, `string`, `number`, `integer`, `boolean`, `array`, `enum`, `anyOf`, and `$ref`/`$def`.
- Unsupported or risky constraints such as string `minLength`/`maxLength` and array `minItems`/`maxItems` must be stripped or cause a validation error according to the model config.
- The default MVP should strip/downgrade strict schemas rather than sending arbitrary Codex/OpenAI strict tool schemas to DeepSeek.

Name mapping requirements:

- Reversible.
- Stable across request and response conversion.
- Fits DeepSeek function name constraints: letters, digits, underscores, dashes, max length 64.
- Handles collisions deterministically.

Suggested strategy:

- Keep ordinary names as-is when valid and unique.
- Encode namespace tools as `ns_<base32hash10>_<safe_tool_name>`, trimming the safe suffix so the final name is at most 64 characters.
- Encode custom/freeform tools as `custom_<base32hash10>_<safe_tool_name>`.
- Maintain a per-request map from encoded name to original `{namespace, name, tool_kind}`.
- Include enough metadata in the Codex Gateway state store to decode future tool outputs.
- If a tool name cannot be represented without collision, return 400 instead of silently changing semantics.
- Add deterministic tests for truncation, hashing, collision handling, and reverse lookup across state-store continuation.

For custom/freeform tools:

- Treat them as function tools from DeepSeek's perspective.
- Use a synthetic JSON schema with a single string field such as `input` when the original tool is freeform.
- Convert returned DeepSeek function arguments back to `custom_tool_call.input`.
- Stream custom tool input via `response.custom_tool_call_input.delta` where feasible.

### DeepSeek Request Parameters

For DeepSeek V4 requests:

- Set `model` to the upstream DeepSeek model.
- Set `thinking: {"type":"enabled"}` by default for Codex agent workloads. This is a Codex Gateway policy; DeepSeek's regular API default effort is `high`, while the gateway may default agent turns to `max`.
- Set `reasoning_effort` from Codex effort:
  - `none` / `minimal`: consider `thinking: {"type":"disabled"}` only if the model entry allows non-thinking mode.
  - `low` / `medium` / `high`: `high`
  - `xhigh`: `max`
  - default: `max` for Codex agent workloads.
- Set `stream` based on the client request.
- If streaming, set `stream_options.include_usage = true`.
- Send `tools` only when non-empty.
- By default, do not send `tool_choice` to DeepSeek when tools are present; rely on DeepSeek's default `auto`.
- When no tools are present, delete `tool_choice`.
- Allow `required` or forced function `tool_choice` only behind a compatibility flag and conformance tests for the exact DeepSeek model/account/endpoint.
- Prefer `response_format: {"type":"json_object"}` plus gateway-side validation for structured output over forced tool choice.
- Do not send Responses-only fields like `store`, `include`, `previous_response_id`, `prompt_cache_key`, `client_metadata`, `service_tier`, or `text` to DeepSeek.
- Map Responses `max_output_tokens` to DeepSeek Chat Completions `max_tokens`. Validate positive integer values and clamp or reject values above the configured DeepSeek model max output according to model policy.
- Do not send thinking-incompatible sampling params in thinking mode.
- Inject stable `user_id` from Codex session/user/API key/account scope using only DeepSeek-allowed characters, for example `codex_gateway_<base64url_sha256(...)>`.
- Preserve DeepSeek request/response IDs in logs and usage metadata.

### DeepSeek Stream to Responses SSE

For each DeepSeek streamed Chat Completion:

1. Emit `response.created`.
2. When the first assistant output begins, emit `response.output_item.added`.
3. For text deltas, emit `response.output_text.delta`.
4. For reasoning deltas, emit `response.reasoning_text.delta` or `response.reasoning_summary_text.delta` depending on Codex model metadata and request `include`.
5. For each standard function tool call, emit a distinct `response.output_item.added` with a stable `item.id`, `call_id`, and `output_index` before any argument delta for that tool.
6. For standard function tool argument deltas, accumulate by tool index and emit `response.function_call_arguments.delta` followed by `response.function_call_arguments.done`.
7. For each custom/freeform tool call, emit a distinct `response.output_item.added` before any custom input delta.
8. For custom/freeform tool argument deltas, accumulate by tool index and emit `response.custom_tool_call_input.delta` where feasible.
9. On `finish_reason: "tool_calls"`, emit one `response.output_item.done` per function/custom tool call.
10. On natural completion, emit a final assistant `message` item via `response.output_item.done`.
11. Emit `response.completed` with normalized usage.

The final `response.completed` event is mandatory.

DeepSeek stream parsing invariants:

- With `stream_options.include_usage = true`, DeepSeek sends one usage-only chunk before `data: [DONE]`; that chunk has `usage != null` and `choices: []`.
- Other chunks may include `usage: null`.
- The parser must check `usage != null && len(choices) == 0` before reading `choices[0]`.
- `data: [DONE]` should not be forwarded to Codex as a Responses event.
- The gateway must emit `response.completed` before ending the downstream SSE stream.
- A DeepSeek chunk with `finish_reason: "insufficient_system_resource"` must become a fixed Responses incomplete/failed strategy, not a normal completion.

Required Responses stream grammar for gateway-generated events:

- `response.created` must contain a `response` object with `id`, `object: "response"`, `created_at`, `model`, `status`, `output`, and `usage` shape compatible with Codex's parser.
- `response.output_item.added` must contain `response_id`, `output_index`, and a parseable `item`.
- `response.function_call_arguments.delta` must contain `response_id`, `item_id`, `output_index`, and `delta`.
- `response.function_call_arguments.done` must contain `response_id`, `output_index`, and the completed function-call `item`.
- `response.output_item.done` must contain `response_id`, `output_index`, and a parseable completed `item`.
- `response.completed` must contain a complete schema-valid `response` object, not only `response.id`; include `id`, `object`, `created_at`, `status: "completed"`, `model`, `output`, `usage`, and `end_turn` when known.
- `response.failed` must contain a complete schema-valid failed `response` object with `status: "failed"` and an `error` object.
- `response.incomplete` must contain a complete schema-valid incomplete `response` object with `status: "incomplete"` and `incomplete_details`.
- `response.failed` and `response.incomplete` must not be raw Chat Completions error chunks.
- If the implementation chooses to emit text content-part lifecycle events such as `response.content_part.added`, `response.content_part.done`, or `response.output_text.done`, fixtures must validate them. If it omits them, fixtures must prove current Codex parsers accept the minimal event subset.

Additional Codex-consumed events/metadata to preserve or emit when available:

- `response.reasoning_summary_part.added`
- `response.metadata` with `openai_verification_recommendation`
- `OpenAI-Model` / `X-OpenAI-Model`
- `X-Reasoning-Included`
- `X-Models-Etag`
- `x-request-id`
- `x-codex-turn-state`

Usage mapping:

- DeepSeek `prompt_tokens` -> Responses `input_tokens`
- DeepSeek `completion_tokens` -> Responses `output_tokens`
- DeepSeek `total_tokens` -> Responses `total_tokens`
- DeepSeek `prompt_cache_hit_tokens` -> Responses `input_tokens_details.cached_tokens`
- DeepSeek `prompt_cache_hit_tokens` -> internal `cache_read_tokens`
- DeepSeek `prompt_cache_miss_tokens` -> internal `cache_miss_tokens`
- DeepSeek `completion_tokens_details.reasoning_tokens` -> Responses `output_tokens_details.reasoning_tokens`
- Assert `prompt_cache_hit_tokens + prompt_cache_miss_tokens == prompt_tokens` when both fields are present.
- Billing must price `cache_read_tokens` and `cache_miss_tokens` separately. A single blended input-token price is not acceptable for visible DeepSeek Codex models.

Failover and partial stream rules:

- Before any downstream model-visible output is written, the provider executor may fail over to another healthy account.
- After emitting `response.output_text.delta`, `response.function_call_arguments.delta`, `response.custom_tool_call_input.delta`, or a completed tool-call item, do not switch accounts. Emit `response.failed` or `response.incomplete`, record partial usage if known, and release the account slot.
- `response.created` may be buffered until the upstream first chunk if doing so enables safe pre-output failover, but once flushed the stream must be completed or failed with a valid Responses terminal event.

### Non-Streaming DeepSeek Responses

For non-streaming:

- Build a complete Responses JSON object.
- Populate `id`, `object: "response"`, `created_at`, `status`, `model`, `output`, `parallel_tool_calls`, `previous_response_id`, `reasoning`, `tool_choice`, `tools`, `usage`, and `metadata`.
- For current Codex HTTP, `previous_response_id` should normally be `null` unless a future compatibility path explicitly supports incoming HTTP continuation state.
- Use the same item conversion logic as the stream finalizer.

## Responses State Store

DeepSeek requires state that Chat Completions does not provide in the same shape as Responses. Add a Codex Gateway state store.

Required stored data:

- `response_id`
- optional `previous_response_id` only for future WebSocket or non-Codex compatibility paths
- public Codex model id
- upstream model id
- provider
- conversation/session key
- API key/user isolation key
- assistant output items
- tool call ids
- encoded tool name to original tool mapping
- assistant text content
- DeepSeek `reasoning_content`
- whether a DeepSeek turn involved tool calls
- whether reasoning content was present or synthesized
- normalized DeepSeek message replay fragments required for tool loops
- usage
- stream completion status
- upstream request id
- created timestamp and TTL

Lookup requirements:

- For current Codex HTTP, do not depend on incoming `previous_response_id`; reconstruct state from full `input` items and gateway call/tool maps.
- Resolve `previous_response_id` only for future WebSocket or explicitly enabled compatibility paths.
- Reconstruct prior assistant messages for DeepSeek thinking tool loops, including raw `reasoning_content` for every DeepSeek-generated assistant message in any turn that involved tools.
- Reattach raw `reasoning_content` for DeepSeek thinking tool loops; missing raw reasoning for any DeepSeek-generated assistant message in such a loop is invalid state.
- Map tool outputs back to the exact DeepSeek `tool_call_id`.
- Detect model/provider changes and avoid replaying incompatible state.
- Reject or fail closed on expired state, API key/session mismatch, provider mismatch, upstream model mismatch, and tool-name map mismatch.

Storage requirements:

- Start with in-memory TTL storage for MVP, matching Augment Gateway's current DeepSeek replay store approach.
- Store only the minimum model-visible transcript required for DeepSeek replay: assistant content, tool call ids, tool-visible tool outputs, encoded tool maps, raw `reasoning_content`, response ids, and usage metadata.
- Do not store Authorization headers, complete raw request bodies, non-model-visible secret payloads, or unredacted logs.
- Redact state-store contents in logs.
- Design the interface so it can later move to Redis/database storage if multi-instance deployment requires it.

## Usage and Billing

Add `CodexUsageFields` or generalize the existing scoped usage metadata so every Codex Gateway request records:

- `client_product = "codex_gateway"`
- `request_scope = "gateway"`
- `feature_scope`
- `provider`
- `requested_model`
- `upstream_model`
- `stream`
- `reasoning_effort`
- `service_tier`
- `state_store_hit`
- `upstream_attempt_id`
- `upstream_request_id`
- `cache_read_tokens`
- `cache_miss_tokens`
- `reasoning_tokens`

DeepSeek cost handling:

- `prompt_tokens` remains total input tokens.
- `prompt_cache_hit_tokens` and `prompt_cache_miss_tokens` must be stored separately because DeepSeek prices cache hits and misses differently.
- `completion_tokens` includes visible output plus reasoning tokens according to DeepSeek usage.
- `completion_tokens_details.reasoning_tokens` should be stored as metadata and exposed through Responses `output_tokens_details.reasoning_tokens`.
- Add first-class billing support for cache-read and cache-miss input token quantities/prices before exposing DeepSeek Codex models as visible.
- Reasoning tokens may remain metadata in MVP because DeepSeek completion pricing applies to total completion tokens, but the quantity must still be stored for reporting and debugging.

Native OpenAI/Codex cost handling:

- Preserve existing OpenAI usage mapping.
- Add the same Codex product/request metadata so reporting can separate Codex Gateway from OpenAI Gateway and Augment Gateway.
- Store upstream attempt IDs so failover accounting can be audited.

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

HTTP error mapping table:

| Case | HTTP status | `error.type` | `error.code` |
| --- | ---: | --- | --- |
| malformed JSON / malformed compression | 400 | `invalid_request_error` | `invalid_request` |
| unsupported content encoding | 400 | `invalid_request_error` | `unsupported_content_encoding` |
| decompressed body too large | 413 | `invalid_request_error` | `request_body_too_large` |
| model disabled / unknown model | 400 | `invalid_request_error` | `model_not_found` |
| scope denied | 403 | `permission_error` | `client_product_forbidden` |
| group not Codex-entitled | 403 | `permission_error` | `codex_gateway_not_entitled` |
| state missing or expired | 404 | `invalid_request_error` | `response_state_not_found` |
| state provider/model/session mismatch | 409 | `invalid_request_error` | `response_state_conflict` |
| DeepSeek 400 | 400 | `invalid_request_error` | upstream code or `deepseek_invalid_request` |
| DeepSeek 401/403 | 502 | `authentication_error` | `upstream_auth_failed` |
| DeepSeek 429 | 429 | `rate_limit_error` | `rate_limit_exceeded` |
| DeepSeek 5xx / insufficient resource | 502 or 503 | `server_error` | `server_is_overloaded` |
| account exhaustion | 503 | `server_error` | `provider_account_exhausted` |

Streaming error event mapping:

- After downstream streaming starts, emit a schema-valid `response.failed` or `response.incomplete`.
- Use Codex-recognized codes where possible: `context_length_exceeded`, `insufficient_quota`, `usage_not_included`, `cyber_policy`, `invalid_prompt`, `server_is_overloaded`, `slow_down`, and `rate_limit_exceeded`.
- Do not emit raw `{"type":"error"}` events for HTTP SSE unless a fixture proves the Codex parser accepts them for this path.

## WebSocket Phase

Do not enable `supports_websockets` initially.

MVP route behavior:

- `GET /codex/v1/responses` returns 405 until WebSocket support is implemented.
- `POST /codex/v1/responses/compact` returns 501 until compact support is implemented.
- The generated Codex provider config must keep `supports_websockets = false`.

A later WebSocket milestone should implement:

- WebSocket upgrade on `/codex/v1/responses` and enabled `/v1/responses` aliases.
- OpenAI beta header handling for `responses_websockets=2026-02-06`.
- Text-frame JSON only, with compatible compression behavior.
- `response.create` request frames.
- Wrapped error mapping.
- `response.processed` frames only where Codex feature-gated behavior expects them.
- `generate=false` prewarm.
- Per-turn `x-codex-turn-state`.
- Header replay and tracing metadata.
- Incremental reuse rules for `previous_response_id`.
- One in-flight response per connection.
- Close-before-completed errors.
- WebSocket-specific events such as `codex.rate_limits`.
- HTTP fallback behavior on unsupported/failed WS.

The existing `openai_ws_forwarder.go` has useful patterns, but Codex Gateway should not advertise WS until it has contract tests against Codex's websocket suite behavior.

`supports_websockets` is a provider transport capability. It does not change `wire_api`; Codex custom providers still use `wire_api = "responses"`.

## Logging and Operations

Codex Gateway logs should include:

- `component=handler.codex_gateway.responses`
- `inbound_endpoint`
- `provider`
- `requested_model`
- `upstream_model`
- `account_id`
- `provider_group_id`
- `stream`
- `state_store_hit`
- `previous_response_id_kind`
- `upstream_request_id`
- `failover_count`
- `first_token_ms`
- `request_payload_hash`
- `usage_hash`

Logs must not include:

- Authorization headers.
- Full raw request bodies.
- Tool output contents.
- Raw state-store transcript or raw `reasoning_content`.
- Non-model-visible secrets from tools/plugins/apps.

## Testing Strategy

Add tests before implementation code changes for each conversion boundary.

Unit tests:

- Responses request body decoding with `Content-Encoding: zstd`.
- Request body decoding with `gzip`, `deflate`, unsupported encoding, malformed compressed payload, decompressed-size overflow, and header cleanup.
- Codex model registry `/models` JSON shape.
- Generated `model_catalog_json` JSON shape and non-empty startup validation.
- Responses input item to DeepSeek messages.
- DeepSeek tools flattening and reverse mapping.
- DeepSeek strict schema stripping/validation.
- DeepSeek tool-choice deletion and compatibility-flag behavior.
- Responses `max_output_tokens` to DeepSeek `max_tokens` mapping and max-output validation.
- DeepSeek reasoning effort mapping.
- DeepSeek usage mapping, especially cache hit/miss tokens and reasoning tokens.
- DeepSeek billing split for cache-read and cache-miss input tokens.
- DeepSeek tool-call chunk accumulation.
- DeepSeek usage-only stream chunk with `choices: []`.
- Responses SSE event finalization with mandatory `response.completed`.
- Schema-valid terminal event bodies for `response.completed`, `response.failed`, and `response.incomplete`.
- Error response format.
- Codex key scope and group entitlement policy.
- Shared `/v1/*` dispatcher route precedence if aliases are enabled.
- State-store provider/model/API key/session mismatch.
- Tool name truncation/hash collision behavior.

Golden tests:

- Simple text turn.
- Function tool call turn.
- Multiple parallel tool calls.
- Namespace/MCP tool call.
- Custom/freeform `apply_patch`-style call.
- Tool result follow-up.
- Thinking mode tool loop with `reasoning_content` replay.
- Thinking mode tool loop where the final assistant has no `tool_calls` but must still replay `reasoning_content` on the next request.
- Missing DeepSeek `reasoning_content` invalid-state path for any DeepSeek-generated assistant message in a tool loop.
- DeepSeek stream with usage chunk.
- DeepSeek `finish_reason: "insufficient_system_resource"`.
- Context length error.
- Rate limit error with retry metadata.
- Stream disconnect before usage.
- State expiry.
- Provider/model mismatch.
- Multimodal tool output degradation.

Integration-style tests:

- Run a fake DeepSeek `/chat/completions` upstream and assert Codex Gateway emits valid Responses SSE.
- Run a fake native Responses upstream and assert pass-through preserves Codex fields.
- Use representative Codex source fixtures for `ResponseItem` variants and stream parser expectations.
- Validate every emitted SSE `data` payload with a Responses event schema or Codex parser fixture.
- Assert event order, `sequence_number` if emitted, `response.id`, `output_index`, item id, call id, tool call id, and terminal event.
- Assert no raw Chat Completions chunks leak downstream.

Manual validation:

- Configure Codex CLI with the generated provider config.
- Verify model list displays both GPT and DeepSeek models.
- Run simple chat.
- Run shell command tool.
- Run file edit/apply patch flow.
- Run MCP/resource listing flow.
- Run plugin/app tool flow where available.
- Run Computer Use/Chrome plugin flow if the local Codex Desktop environment exposes those tools.

Contract-test gate:

- Do not mark a DeepSeek model visible for Codex Gateway until simple text, normal function tool, namespace/MCP tool, custom/freeform tool, and tool-result continuation fixtures pass.
- Do not enable `supports_parallel_tool_calls` until multi-tool fixtures pass against real DeepSeek V4 Pro/Flash or a conformance stub modeled on official chunks.
- Do not enable `supports_websockets` until WebSocket frame tests and Codex client smoke tests pass.

## Implementation Milestones

### Milestone 1: Protocol Skeleton

- Add Codex Gateway config structs.
- Add Codex Gateway routes under `/codex/v1`.
- Add Codex Gateway key-scope and group entitlement policy.
- Add Codex Gateway model registry.
- Add `/models` response builder.
- Add `model_catalog_json` generator.
- Add handler skeleton for `/responses`.
- Add zstd/gzip/deflate-aware request body reading.
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
- Add settings/API-key/group migrations.
- Add generated model catalog export.

### Milestone 7: WebSocket Evaluation

- Implement only if HTTP compatibility is stable.
- Add WebSocket v2 frame tests before enabling `supports_websockets`.

## Open Questions

1. DeepSeek non-thinking mode: should `none`/`minimal` reasoning disable thinking, or should Codex Gateway force thinking for all DeepSeek agent turns?
2. State store: is in-memory TTL acceptable for the first deployment, or does this environment require multi-instance persistence immediately?
3. Strict mode: should DeepSeek strict tools be disabled until beta endpoint support is explicitly configured?
4. `/v1/*` aliasing: should it remain disabled until `/codex/v1/*` is proven with real Codex Desktop/CLI smoke tests?

## References

Official OpenAI/Codex references:

- [Codex App Server API overview](https://developers.openai.com/codex/app-server#api-overview)
- [Codex config reference](https://developers.openai.com/codex/config-reference#configtoml)
- [Streaming API responses](https://developers.openai.com/api/docs/guides/streaming-responses)
- [Streaming function calls](https://developers.openai.com/api/docs/guides/function-calling#streaming)

Codex source references checked locally:

- `/tmp/openai-codex/codex-rs/model-provider-info/src/lib.rs`
- `/tmp/openai-codex/codex-rs/codex-api/src/common.rs`
- `/tmp/openai-codex/codex-rs/codex-api/src/sse/responses.rs`
- `/tmp/openai-codex/codex-rs/codex-api/src/endpoint/models.rs`
- `/tmp/openai-codex/codex-rs/codex-api/src/endpoint/responses_websocket.rs`
- `/tmp/openai-codex/codex-rs/protocol/src/models.rs`
- `/tmp/openai-codex/codex-rs/protocol/src/openai_models.rs`
- `/tmp/openai-codex/codex-rs/app-server/tests/suite/v2`

Official DeepSeek references:

- [Create Chat Completion](https://api-docs.deepseek.com/api/create-chat-completion)
- [Thinking Mode](https://api-docs.deepseek.com/guides/thinking_mode)
- [Tool Calls](https://api-docs.deepseek.com/guides/tool_calls)
- [Context Caching](https://api-docs.deepseek.com/guides/kv_cache)
- [Models and Pricing](https://api-docs.deepseek.com/quick_start/pricing)

Community pitfall references used as non-authoritative evidence:

- DeepSeek/OpenClaw-style HTTP 400 reports where thinking tool loops drop `reasoning_content`.
- `pydantic-ai` and LangChain reports about `tool_choice` incompatibility on DeepSeek reasoning models.

## Recommendation

Use a new `codex_gateway` module with a provider registry and adapter interface parallel to `augment_gateway`.

Implement HTTP Responses first and keep `supports_websockets = false` until the Responses SSE and tool-loop contract is proven. Reuse Augment Gateway's DeepSeek sanitizer ideas, but build a Codex-specific Responses state store and tool conversion layer because Codex has a broader item/tool model than Augment.

The first implementation plan should cover Milestones 1 through 5. WebSocket should remain a separate follow-up plan after Codex CLI/Desktop HTTP validation passes.
