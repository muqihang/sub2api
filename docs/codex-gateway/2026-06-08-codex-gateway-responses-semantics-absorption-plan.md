# Codex Gateway Responses Semantics Gap Audit and Regression Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Objectively improve Codex Gateway's GPT-like native behavior for DeepSeek, Claude, AGNES, and generic OpenAI-compatible providers by absorbing only proven protocol invariants and diagnostics, while explicitly preserving already-implemented local provider-native capabilities and avoiding duplicate/replacement implementations.

**Architecture:** Keep the existing provider-native Codex Gateway design as the source of truth: `/codex/v1/models` and `/codex/v1/responses` remain the only provider-facing surfaces. Most work below is gap audit, regression coverage, and small targeted completion of demonstrated gaps; do not replace stable local codecs/builders with generic bridges. Absorb protocol invariants, golden tests, request-shape stability, telemetry, and thin provider profiles; reject Desktop JSON-RPC control-plane implementation, gateway-managed session compaction, and provider-specific hacks that would weaken DeepSeek/Claude/AGNES native semantics.

**Tech Stack:** Go backend under `backend/internal/service` and `backend/internal/pkg/apicompat`, Codex Desktop model catalog JSON, OpenAI Responses-compatible SSE, DeepSeek OpenAI-compatible Chat Completions, Anthropic Messages, AGNES chat-compatible provider, capture/diagnostics infrastructure.

---

## 0. Scope and Objective Standard

The target is not to copy upstream wholesale. The target is a Codex Desktop experience that is as close as possible to native GPT behavior while preserving the stronger local provider-native work already implemented for:

- DeepSeek V4 Pro / Flash.
- Claude direct Anthropic-format models.
- AGNES independent provider.
- OpenAI Responses pass-through.
- `tool_search` / deferred tools / subagent exposure.
- Computer Use high-fidelity tool output handling.
- Capture, cache, and protocol diagnostics.

The absorption rule is:

1. **Absorb** protocol invariants that Codex CLI/Desktop demonstrably expect.
2. **Adapt cautiously** provider-specific strategies only inside the provider that needs them.
3. **Reject** control-plane APIs, lossy bridges, and session-management features that would make Gateway own semantics that belong to Codex Core/Desktop.
4. **Prefer local provider-native implementations** when they already preserve more Codex semantics than upstream generic bridges.

---

## 1. Evidence Sources

### 1.1 Upstream v0.1.135 / apicompat / OpenAI WS base

High-effort review was performed in:

`/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/upstream-0.1.135-sync`

Key evidence files:

- `backend/internal/pkg/apicompat/responses_stream_event_wire.go`
- `backend/internal/pkg/apicompat/responses_stream_event_wire_test.go`
- `backend/internal/pkg/apicompat/chatcompletions_responses_bridge.go`
- `backend/internal/pkg/apicompat/chatcompletions_responses_stream_lifecycle_test.go`
- `backend/internal/pkg/apicompat/chatcompletions_responses_request_invariants_test.go`
- `backend/internal/pkg/apicompat/responses_to_anthropic_request.go`
- `backend/internal/pkg/apicompat/responses_to_anthropic_tool_pairing_test.go`
- `backend/internal/service/openai_ws_forwarder.go`
- `backend/internal/service/openai_gateway_responses_chat_fallback.go`
- `backend/internal/service/openai_codex_transform.go`

### 1.2 Open-source Codex CLI

Temporary clone:

`/tmp/codex-cli-research-20260608-081411`

Commit:

`0526cb56ac3501a02968010d03873993c319e290`

Key evidence files:

- `codex-rs/codex-api/src/common.rs`
- `codex-rs/core/src/client.rs`
- `codex-rs/codex-api/src/sse/responses.rs`
- `codex-rs/protocol/src/models.rs`
- `codex-rs/protocol/src/openai_models.rs`
- `codex-rs/tools/src/responses_api.rs`
- `codex-rs/core/src/tools/handlers/tool_search_spec.rs`
- `codex-rs/core/src/mcp_tool_exposure.rs`
- `codex-rs/codex-api/src/endpoint/responses_websocket.rs`

### 1.3 Codex Desktop App.Server.v2 schema and bundle resources

Generated schema:

`/tmp/codex-appserver-schema-20260608-081418`

Generated with:

```bash
/Applications/Codex.app/Contents/Resources/codex app-server generate-json-schema --out /tmp/codex-appserver-schema-20260608-081418
```

Observed:

- Codex CLI: `codex-cli 0.133.0`
- Codex Desktop: `26.519.81530`, build `3178`
- 254 schema files, 215 under `v2/`

Key evidence files:

- `/tmp/codex-appserver-schema-20260608-081418/ClientRequest.json`
- `/tmp/codex-appserver-schema-20260608-081418/ServerRequest.json`
- `/tmp/codex-appserver-schema-20260608-081418/ServerNotification.json`
- `/tmp/codex-appserver-schema-20260608-081418/v2/ThreadStartParams.json`
- `/tmp/codex-appserver-schema-20260608-081418/v2/TurnStartParams.json`
- `/tmp/codex-appserver-schema-20260608-081418/v2/RawResponseItemCompletedNotification.json`
- `/tmp/codex-appserver-schema-20260608-081418/DynamicToolCallParams.json`
- `/tmp/codex-appserver-schema-20260608-081418/DynamicToolCallResponse.json`
- `/tmp/codex-appserver-schema-20260608-081418/v2/McpServerToolCallParams.json`
- `/tmp/codex-appserver-schema-20260608-081418/v2/McpServerToolCallResponse.json`

Computer Use bundle evidence:

- `/Applications/Codex.app/Contents/Resources/plugins/openai-bundled/plugins/computer-use/.codex-plugin/plugin.json`
- `/Applications/Codex.app/Contents/Resources/plugins/openai-bundled/plugins/computer-use/.mcp.json`
- `/Applications/Codex.app/Contents/Resources/plugins/openai-bundled/plugins/computer-use/skills/computer-use/SKILL.md`

### 1.4 DeepSeek official documentation

Official links used:

- [DeepSeek API first call](https://api-docs.deepseek.com/)
- [Chat Completion API](https://api-docs.deepseek.com/api/create-chat-completion/)
- [Thinking Mode](https://api-docs.deepseek.com/guides/thinking_mode)
- [Tool Calls](https://api-docs.deepseek.com/guides/tool_calls)
- [Context Caching / KV Cache](https://api-docs.deepseek.com/guides/kv_cache)
- [Rate Limit & Isolation](https://api-docs.deepseek.com/quick_start/rate_limit)
- [Models & Pricing](https://api-docs.deepseek.com/quick_start/pricing)
- [Anthropic API Compatibility](https://api-docs.deepseek.com/guides/anthropic_api)
- [Change Log](https://api-docs.deepseek.com/updates)

### 1.5 DeepSeek-Reasonix

Temporary clone:

`/tmp/DeepSeek-Reasonix-codex-research`

Commit:

`8b2028e82427dc4925bc61c793c639116a44e8ee`

Key evidence files:

- `internal/provider/schema_canonicalize.go`
- `internal/tool/tool.go`
- `internal/agent/cache_shape.go`
- `internal/provider/provider.go`
- `internal/provider/openai/openai.go`
- `internal/config/effort.go`
- `internal/config/config.go`
- `internal/plugin/cache.go`
- `internal/plugin/lazy.go`
- `internal/plugin/plugin.go`
- `internal/agent/session.go`
- `internal/agent/compact.go`

---

## 2. Decision Matrix

| Area | Decision | Why |
| --- | --- | --- |
| Responses SSE wire field presence | **Absorb** | Codex CLI strict parsing depends on zero `output_index` / `content_index` / `summary_index`, `content:[]`, `summary:[]`, and complete tool item fields. |
| Event lifecycle open/delta/done/terminal | **Absorb** | Codex treats stream close before `response.completed` as error; item deltas must follow item open. |
| Final tool item completeness | **Absorb** | Codex CLI currently ignores `response.function_call_arguments.delta` for ordinary function tools; complete `output_item.done.item.arguments` is the reliable tool-call source. |
| Structured tool outputs | **Absorb** | `function_call_output.output` can be string or content item array. Stringifying all output harms images/Computer Use/MCP results. |
| `tool_search` / deferred tools | **Absorb** | Codex CLI uses `tool_search` for large/deferred tool sets; App.Server.v2 schema exposes `tool_search_call` and `tool_search_output`. |
| Model catalog capability shape | **Absorb** | `ModelInfo` fields drive reasoning, search tools, modalities, truncation, parallel tool calls, and subagent/tool exposure. |
| Header / metadata preservation | **Adapt cautiously** | `x-request-id`, `openai-model`, `x-codex-turn-state`, `X-Models-Etag` improve UX/diagnostics; do not forge semantics we cannot support. |
| OpenAI WS v2 incremental | **Adapt only when enabled** | Codex CLI uses `previous_response_id` mainly for WS incremental. Do not implement half-WS or force HTTP semantics. |
| OpenAI OAuth `store=false` / encrypted reasoning | **Provider-specific only** | Valid for OpenAI native/OAuth, not for DeepSeek/Claude/AGNES. |
| Chat Completions fallback | **Fallback only** | Useful for providers without Responses API, but lossy for Codex-native tools. |
| Generic apicompat replacing local provider codecs | **Reject** | Local Codex Gateway supports more native semantics: custom/freeform/local shell/tool_search/provider state. |
| App.Server.v2 JSON-RPC implementation | **Reject** | It is Desktop/Core control plane, not provider API. |
| Gateway-managed session/compaction/summary | **Reject** | Session ownership belongs to Codex Core/Desktop or agent layer; Gateway rewriting would break audit/replay/tool pairing. |
| DeepSeek `prompt_cache_key` as KV cache control | **Reject** | Official DeepSeek docs do not support it. |
| Anthropic `cache_control` for DeepSeek cache | **Reject** | DeepSeek Anthropic compatibility docs mark it ignored. |
| Reasonix “never replay reasoning_content” | **Reject as blanket rule** | Conflicts with DeepSeek V4 thinking + tool-call official replay requirement. |
| Tool schema canonicalization / stable ordering | **Absorb** | Low risk if limited to key ordering and set-like arrays; improves prefix stability and fixture determinism. |
| Cache telemetry / prefix-shape diagnostics | **Absorb** | Necessary to distinguish true cache behavior from guesses. |
| Tool pairing sanitizer | **Adapt cautiously** | Good as send-time sanitized view plus diagnostics; silent long-term transcript repair is risky. |

---

## 3. Current Local Strengths to Preserve

The following local design is better suited than upstream generic bridges and must not be regressed.

### 3.1 Provider-native Codex Gateway routes

Existing routes remain the correct provider surface:

- `GET /codex/v1/models`
- `POST /codex/v1/responses`

Evidence:

- `backend/internal/server/routes/codex_gateway.go`
- `backend/internal/service/codex_gateway_service.go`
- `tools/zhumeng-agent/src/zhumeng_agent/proxy/upstream.py`

### 3.2 Provider-native state replay

Local state store binds replay to response/session/isolation/provider/upstream model:

- `backend/internal/service/codex_gateway_state_store.go`
- `backend/internal/service/codex_gateway_deepseek_request.go`
- `backend/internal/service/codex_gateway_anthropic_request.go`

This is more appropriate for DeepSeek/Claude/AGNES than importing OpenAI `previous_response_id` server-store semantics.

### 3.3 DeepSeek tool-call reasoning guard

Local state validation already guards DeepSeek tool loops:

`backend/internal/service/codex_gateway_state_store.go`

DeepSeek tool-call states require `ReasoningContentPresent` or `ReasoningContentSynthesized`. This aligns with the official DeepSeek V4 tool-call thinking replay requirement and must be preserved.

### 3.4 Tool search and native tool mapping

Local code already handles `tool_search_output`, custom/freeform tools, local shell outputs, and provider-specific mapping:

- `backend/internal/service/codex_gateway_deepseek_request.go`
- `backend/internal/service/codex_gateway_anthropic_request.go`
- `backend/internal/service/codex_gateway_tool_mapping.go`
- `backend/internal/service/codex_gateway_legacy_tool_normalization.go`

Upstream generic Chat/Anthropic bridges should provide tests/invariants, not replacement mapping logic.

### 3.5 Computer Use provider strategy injection

Local Computer Use instructions for DeepSeek/AGNES are already provider-specific:

- `codexGatewayDeepSeekComputerUseInstruction`
- `codexGatewayAgnesComputerUseInstruction`

Evidence:

`backend/internal/service/codex_gateway_deepseek_request.go`

This should be retained, then improved only with structured output preservation and semantic compression tests.

### 3.6 Model registry already contains Codex CLI-style fields

`backend/internal/service/codex_gateway_model_registry.go` already exports many `ModelInfo`-like fields:

- reasoning levels
- summaries
- verbosity
- apply patch / web search tool type
- truncation policy
- context windows
- parallel tool calls
- modalities
- supported experimental tools

This is a strong base; the task is gap audit, not rebuild.

### 3.7 Current implementation status and duplicate-work guard

The implementation plan must be read as a **gap audit and regression plan**, not a greenfield build plan. A second code-vs-plan review found these current-state categories:

| Area | Current status | Execution instruction |
| --- | --- | --- |
| DeepSeek `reasoning_content` tool-loop replay/state guard | Already implemented | Preserve and add regression tests only. Do not import Reasonix blanket stripping. |
| DeepSeek stable `user_id` | Already implemented | Preserve; audit format/source diagnostics. |
| DeepSeek streaming `stream_options.include_usage=true` | Already implemented | Preserve; add usage field tests only. |
| Official DeepSeek not receiving client `prompt_cache_key`; AGNES scoped key | Already implemented | Preserve provider split; add regression tests. |
| `tool_search` / deferred tools / subagent exposure | Already implemented | Treat as protected behavior; run live/regression validation. |
| Computer Use DeepSeek/AGNES prompt and semantic compression | Already implemented | Protect existing prompt/compression; only add versioned, fixture-proven changes. |
| Codex CLI-style model catalog fields | Mostly implemented | Gap audit missing/incorrect fields only. |
| Tool schema canonicalization/stable ordering | Partially implemented | Extend existing canonicalizer only; do not create parallel logic. |
| Prefix/cache diagnostics | Mostly implemented | Add aliases/specific missing fields only; do not rebuild capture. |
| Explicit provider profile struct | Not implemented | Allowed as thin abstraction with golden no-regression gates. |

**Execution rule:** for every phase, the worker must first verify whether the behavior already exists. If it exists, the task becomes "add/strengthen regression tests and document protection". Only a demonstrated gap may receive code changes.

---

## 4. Absorption Areas and Design Details

### 4.1 Responses SSE wire-shape compliance

**Problem:** Go `omitempty` and ad-hoc maps can accidentally omit zero-valued protocol fields that Codex CLI treats as meaningful.

**Absorb from upstream:** `backend/internal/pkg/apicompat/responses_stream_event_wire.go` explicit per-event serialization model.

**Local target:** `backend/internal/service/codex_gateway_responses_codec.go`.

**Required invariants:**

- `response.output_item.added` and `response.output_item.done` include `output_index`, including zero.
- `message` items always carry `content: []` when content is empty.
- `reasoning` items always carry `summary: []` when summary is empty.
- `function_call` items always carry `call_id`, `name`, and `arguments`; `arguments` may be empty string.
- `response.output_text.delta/done` always include `output_index` and `content_index`.
- `response.reasoning_summary_text.delta/done` always include `output_index` and `summary_index`.
- `response.function_call_arguments.done` always includes `arguments` even when empty.

**Non-goal:** Replace local response writer wholesale if provider-native event writing already works.

### 4.2 Event lifecycle and terminal discipline

**Absorb:** Chat->Responses lifecycle tests and terminal compatibility.

**Required invariants:**

- Always emit `response.created` before output events.
- Open item with `response.output_item.added` before text/reasoning/tool deltas.
- Close output with `response.output_item.done`.
- End stream with `response.completed`, `response.failed`, or `response.incomplete`.
- Treat unexpected upstream close as `response.failed` if no terminal was emitted.
- Accept terminal aliases such as `response.done` only internally; emit canonical `response.completed` to Codex unless pass-through requires otherwise.
- `response.completed.response.id` must be present and stable for trace/state.

**Why it matters:** Codex CLI reports an error when the stream closes before `response.completed`.

### 4.3 Tool call and tool output completeness

**Absorb:** Codex CLI finding that ordinary `response.function_call_arguments.delta` is not enough; final `output_item.done.item` is authoritative.

**Required invariants:**

- Function tool calls must be complete in `output_item.done.item.arguments`.
- Custom/freeform tool calls must stream and finalize through `custom_tool_call_input` plus complete done item.
- Tool output must keep `call_id` and exact output type.
- Do not string-flatten `function_call_output.output` if it is an array of content items.
- Cover `input_text`, `input_image`, and `encrypted_content` output forms.

### 4.4 Deferred tools and `tool_search`

**Absorb:** Codex CLI deferred tools and App.Server.v2 `tool_search_call` / `tool_search_output` model-visible items.

**Required invariants:**

- Preserve `tool_search_call` as a native Responses item, not generic function tool when returning to Codex.
- Replay `tool_search_output.tools` as deterministic compact JSON to chat-compatible providers.
- Use stable namespace/function names for deferred tools.
- Preserve `execution`, `status`, `call_id`, and tool family identity.
- Do not confuse Codex `tool_search` with hosted web search.

### 4.5 Model catalog capability gap audit

**Absorb:** Codex CLI `ModelInfo` shape.

**Gap-audit fields:**

- `supports_reasoning_summaries`
- `default_reasoning_summary`
- `supported_reasoning_levels`
- `support_verbosity`
- `default_verbosity`
- `apply_patch_tool_type`
- `web_search_tool_type`
- `truncation_policy`
- `supports_parallel_tool_calls`
- `supports_image_detail_original`
- `context_window`
- `auto_compact_token_limit`
- `experimental_supported_tools`
- `input_modalities`
- `supports_search_tool`
- `use_responses_lite`
- `tool_mode`
- `multi_agent_version`
- optional model ETag behavior

**Provider-specific desired results:**

- DeepSeek models expose deferred tools and reasoning levels accurately.
- Claude direct models expose Anthropic thinking/reasoning capabilities without misleading OpenAI encrypted reasoning semantics.
- AGNES exposes only capabilities that have passed protocol gates.
- OpenAI native pass-through can expose WS only when fully implemented and enabled.

### 4.6 Headers and metadata

**Adapt cautiously:**

- Generate or preserve `x-request-id`.
- Set `openai-model` to the effective upstream model when useful.
- Preserve `x-codex-turn-state` if upstream/native path returns it.
- Preserve `X-Models-Etag` for model catalog when implemented.
- Preserve safe `response.metadata` fields; do not drop verification/moderation metadata in OpenAI native pass-through.

**Do not:**

- Forge OpenAI verification metadata for non-OpenAI providers.
- Pretend a provider included reasoning if it did not.

### 4.7 OpenAI WS v2

**Decision:** Not a P0 unless Codex Gateway declares `supports_websockets` for a provider.

If implemented later, it must be complete enough to match Codex CLI:

- Upgrade path for `/responses`.
- `OpenAI-Beta: responses_websockets=2026-02-06` compatibility.
- Text frames with `{"type":"response.create", ...}`.
- Optional `generate:false` warmup.
- Optional `previous_response_id` incremental input.
- Same Responses event JSON frames on output.
- Error event classification and safe fallback only before client-visible output.

**Important:** Do not treat `previous_response_id` as a mandatory HTTP `/responses` field. Codex CLI's public HTTP `ResponsesApiRequest` does not include it; current CLI uses it for WS incremental semantics.

### 4.8 DeepSeek official semantics

**Absorb:**

- Current official models: `deepseek-v4-pro`, `deepseek-v4-flash`.
- Official OpenAI-compatible base URL is `https://api.deepseek.com`; beta strict tools use `/beta`.
- `thinking: {"type":"enabled"|"disabled"}`.
- `reasoning_effort: "high"|"max"`.
- Thinking mode ignores temperature/top_p/presence/frequency penalties.
- Tool calls are OpenAI function tools; max 128.
- Strict mode only on beta and only with official schema restrictions.
- Streaming should include `stream_options.include_usage=true` to obtain final usage chunk.
- Usage must capture `prompt_cache_hit_tokens`, `prompt_cache_miss_tokens`, and `completion_tokens_details.reasoning_tokens`.
- `user_id` is official for cache/privacy/scheduling isolation and must be stable, non-PII, `[a-zA-Z0-9-_]+`, max 512.

**Correct DeepSeek reasoning replay policy:**

- No-tool assistant reasoning does not need to be replayed upstream.
- Tool-call assistant reasoning must be replayed in subsequent requests, including later user turns, or DeepSeek V4 can return 400.
- If assistant tool-call content is empty, preserve empty `content` / empty `reasoning_content` fields when required by the provider message shape.

**Reject:**

- Treating `prompt_cache_key` as official DeepSeek cache control.
- Treating Anthropic `cache_control` as DeepSeek cache control.
- Advertising manual KV cache handles or guaranteed 99% cache hit.
- Blindly copying Reasonix's “never replay reasoning_content” rule.

### 4.9 DeepSeek cache optimization truth model

DeepSeek context caching is automatic and best-effort. Gateway can improve the **probability** of cache hits, not guarantee them.

**Cache-friendly actions Gateway can safely take:**

- Keep system/developer prefix stable.
- Keep tool schema order and JSON shape stable.
- Avoid dynamic timestamp/nonce/diagnostics in the prompt prefix.
- Append provider strategy instructions only when needed, and deduplicate them.
- Preserve stable `user_id` for official cache isolation.
- Avoid unnecessary prompt-cache-key injection to DeepSeek official API.
- Use request-shape hashes and hit/miss telemetry to diagnose real behavior.

**Cache-hostile actions to avoid:**

- Reordering tools every turn.
- Injecting changing diagnostics into system messages.
- Rewriting or summarizing old transcript inside Gateway.
- Changing Computer Use compression format without versioning.
- Blindly converting structured outputs to text.

### 4.10 Tool schema canonicalization and stable ordering

**Absorb from Reasonix:**

- Canonicalize JSON schema recursively.
- Sort object keys deterministically.
- Sort set-like arrays such as `required` and `dependentRequired`.
- Do not sort semantically ordered arrays such as `enum`, `oneOf`, `anyOf`.
- Sort tools by stable tool name after reversible namespace aliasing.

**Local target:** tool mapping/build stage, especially before DeepSeek/AGNES chat-compatible upstream body construction.

**Risk control:** feature flag or provider profile switch if any provider shows tool selection regressions.

### 4.11 Tool pairing sanitizer and diagnostics

**Adapt from Reasonix / upstream apicompat:**

- Detect orphan tool outputs.
- Detect assistant tool calls without matching tool results.
- Detect duplicate or empty tool call ids.
- For DeepSeek/OpenAI Chat upstream, generate a sanitized outbound view that satisfies Chat Completions ordering requirements.
- Prefer fail-close or diagnostics for ambiguous repair.
- If a repair is configured, add concise placeholder output and capture `tool_pairing_repaired=true` with reason.

**Do not:** silently mutate Gateway's stored canonical transcript as if the user/model actually produced the repaired item.

### 4.12 Prefix-shape and cache diagnostics

**Absorb from Reasonix:** prefix-shape hash diagnostics.

**Recommended capture fields:**

- `system_hash`
- `developer_hash`
- `tools_hash`
- `tool_count`
- `tool_schema_bytes`
- `messages_prefix_hash`
- `stable_prefix_bytes`
- `gateway_injection_version`
- `computer_use_compression_version`
- `request_allowlist_hash`
- `user_id_source`
- `prompt_cache_key_present` only as client/OpenAI metadata, not DeepSeek cache control
- DeepSeek official `prompt_cache_hit_tokens`
- DeepSeek official `prompt_cache_miss_tokens`
- OpenAI-compatible nested `prompt_tokens_details.cached_tokens` only in the generic/OpenAI usage normalizer, never as a DeepSeek official field
- `reasoning_tokens`
- `cache_change_hint=system|tools|messages|compression_version|provider|unknown`

**Privacy rule:** hash only, preferably HMAC with existing redaction infrastructure; never log raw prompt/tool output/images.

### 4.13 Computer Use fidelity

**Absorb:** Codex CLI structured tool output and App.Server.v2 distinction between model-visible output and Desktop/MCP control plane.

**Gateway responsibilities:**

- Preserve model-visible tool output shape.
- Preserve structured output arrays for image/text/encrypted content when provider supports them.
- For text-only providers, use deterministic semantic compression with explicit versioning.
- Keep `visible_text`, actionable `operable_lines`, key AX metadata, app/bundle id, and current error/truncation status.
- Do not attempt to start Computer Use MCP, read screenshots, parse AX tree, or call Desktop JSON-RPC.

**Provider strategy:**

- Continue DeepSeek/AGNES Computer Use prompt injection, but keep it stable and deduplicated.
- Claude direct should rely more on Anthropic-native reasoning/tool semantics and should not receive DeepSeek-specific wording.
- Add tests that compression preserves enough content for Electron/canvas apps without exploding token count.

---

## 5. Explicit Non-Goals

Do not implement or absorb:

- Codex Desktop App.Server.v2 JSON-RPC methods such as `thread/start`, `turn/start`, `plugin/list`, `mcpServer/tool/call`, `item/tool/call`, `fs/readFile`, `command/exec`, approvals, account login, marketplace, Windows sandbox, fuzzy file search.
- Gateway-managed append-only sessions, compaction, summarization, planner/executor split, or memory/skills lifecycle.
- OpenAI OAuth `store=false` as a global provider rule.
- OpenAI encrypted reasoning as a generic reasoning abstraction for Claude/DeepSeek.
- Chat Completions fallback as the primary path for Codex-native tools.
- Manual DeepSeek KV cache handles, DeepSeek `prompt_cache_key`, or DeepSeek cache guarantees.
- Anthropic `cache_control` as DeepSeek cache control.
- Reasonix “never replay reasoning_content” as a blanket rule.
- Half-implemented Responses WebSocket support.

## 5.1 Execution Prohibitions for Future Workers

These are hard constraints for implementation workers:

1. **Do not replace local Codex Gateway provider-native codecs with `backend/internal/pkg/apicompat/*` generic bridges.** Use apicompat only as invariant/test reference.
2. **Do not apply Reasonix's blanket "never replay `reasoning_content`" rule to DeepSeek.** Current DeepSeek tool-call reasoning replay is a protective feature required by V4 thinking + tools.
3. **Do not send client `prompt_cache_key` to official DeepSeek.** Only AGNES/OpenAI profiles may use prompt-cache-key semantics where already verified.
4. **Do not reimplement Codex `tool_search` as hosted web search or a generic function bridge.** Existing deferred-tool semantics are protected.
5. **Do not rewrite Computer Use prompt/compression format without versioning and regression fixtures.** Existing `visible_text`, `operable_lines`, settable input, app/bundle id, screenshot/truncation status, and errors must remain available.
6. **Do not let tool-pairing repair mutate Gateway's canonical transcript silently.** Repairs, if any, must be outbound sanitized views plus diagnostics.
7. **Do not make HTTP `/codex/v1/responses` depend on OpenAI server-store `previous_response_id` semantics.** Codex CLI uses that mainly for WS incremental.
8. **Do not use provider profiles as a reason for broad DeepSeek/Claude/AGNES request-builder refactors.** Profiles must be thin, opt-in, and snapshot-gated.

---

## 6. Gap Audit and Regression Plan

Before any phase changes code, inspect the current implementation and classify each item as already implemented, partially implemented, or missing. Do not duplicate or replace stable local provider-native paths.

### Phase 0: Baseline and fixture gap audit

**Goal:** Capture current behavior and identify fixture gaps before making changes. Existing fixtures under `backend/internal/service/testdata/codex_gateway/*` are the starting point, not something to recreate.

**Files:**

- Modify/create tests under `backend/internal/service/*codex_gateway*_test.go`.
- Modify/create fixtures under `backend/internal/service/testdata/codex_gateway/`.
- Reuse upstream tests from `backend/internal/pkg/apicompat/*` where appropriate.

- [ ] **Step 0.1: Record current focused test baseline**

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/upstream-0.1.135-sync/backend
go test ./internal/service ./internal/pkg/apicompat -run 'CodexGateway|Responses|DeepSeek|Anthropic|Agnes|ChatCompletions|ToolSearch|ComputerUse' -count=1 -timeout=300s
```

Expected: establish pass/fail baseline; do not hide pre-existing failures.

- [ ] **Step 0.2: Add protocol fixture matrix**

Audit existing fixtures first, then add only missing fixtures covering:

- simple message stream;
- function tool call stream;
- custom/freeform tool call stream;
- `tool_search_call` + `tool_search_output` replay;
- `function_call_output.output` string;
- `function_call_output.output` array with `input_text`;
- `function_call_output.output` array with `input_image`;
- DeepSeek reasoning + tool call replay;
- Computer Use visible text compression case;
- stream with terminal usage chunk.

Expected: fixtures are data-only and redacted.

### Phase 1: Responses wire-shape gap audit

**Goal:** Audit the existing local event writer against Codex CLI-compatible wire shape, then add small fixes only for demonstrated gaps. The writer already emits many zero-valued fields via maps; do not rewrite it wholesale.

**Files:**

- Modify: `backend/internal/service/codex_gateway_responses_codec.go`
- Test: `backend/internal/service/codex_gateway_responses_codec_test.go`
- Reference: `backend/internal/pkg/apicompat/responses_stream_event_wire.go`

- [ ] **Step 1.1: Write failing tests for zero-value field presence**

Test:

- `output_index:0` present;
- `content_index:0` present;
- `summary_index:0` present;
- empty `arguments` present on function call done;
- message item `content:[]` present;
- reasoning item `summary:[]` present.

Run:

```bash
go test ./internal/service -run 'TestCodexGateway.*Wire|Test.*ResponsesCodec' -count=1
```

Expected before fix: any missing field fails.

- [ ] **Step 1.2: Patch demonstrated wire-shape gaps only**

If tests show missing fields, add the smallest local normalization needed before writing maps. Do not introduce a parallel writer unless the existing writer cannot be safely patched. Must preserve existing provider-specific `RawFields` behavior.

- [ ] **Step 1.3: Verify no regression in existing apicompat tests**

Run:

```bash
go test ./internal/service ./internal/pkg/apicompat -run 'Responses|CodexGateway' -count=1
```

### Phase 2: Event lifecycle and terminal discipline

**Goal:** Audit existing stream lifecycle behavior and close only demonstrated gaps. DeepSeek and Anthropic already have lifecycle/terminal discipline; focus especially on OpenAI pass-through edge cases and provider-specific regressions.

**Files:**

- Modify: `backend/internal/service/codex_gateway_deepseek_stream.go`
- Modify: `backend/internal/service/codex_gateway_anthropic_stream.go`
- Modify: `backend/internal/service/codex_gateway_openai_responses_adapter.go`
- Test: matching stream tests.

- [ ] **Step 2.1: Add tests for upstream close without terminal**

Cases:

- DeepSeek upstream closes after text delta but before usage/finish.
- AGNES upstream closes mid-tool call.
- Claude direct stream errors after item open.

Expected: Gateway emits or returns `response.failed`; it does not look like a successful completed stream.

- [ ] **Step 2.2: Ensure final `response.completed.response.id` and usage are present**

Use terminal usage chunk when available; reconstruct minimal response from accumulated items when terminal response is empty.

- [ ] **Step 2.3: Add lifecycle golden checks**

Golden order:

`response.created` -> `response.output_item.added` -> deltas -> `response.output_item.done` -> `response.completed`.

### Phase 3: Structured tool output preservation gap audit

**Goal:** Preserve structured output where the provider path supports it, and protect existing high-fidelity text/Computer Use compression where the provider is text-only. Do not rewrite the existing compression pipeline.

**Files:**

- Modify: `backend/internal/service/codex_gateway_deepseek_request.go`
- Modify: `backend/internal/service/codex_gateway_anthropic_request.go`
- Modify: `backend/internal/service/codex_gateway_tool_mapping.go` if needed
- Test: `backend/internal/service/codex_gateway_*_test.go`

- [ ] **Step 3.1: Add tests for structured `function_call_output.output`**

Inputs:

```json
{"type":"function_call_output","call_id":"call_img","output":[{"type":"input_image","image_url":"data:image/png;base64,..."}]}
```

Expected:

- OpenAI native pass-through preserves array.
- Anthropic direct maps image output according to supported block semantics or explicit fallback.
- DeepSeek text-only path uses deterministic placeholder/metadata, not lossy random stringification.

- [ ] **Step 3.2: Patch provider-specific preservation/fallback gaps**

Use existing provider behavior first; only patch demonstrated gaps. A future thin provider profile may decide:

- native structured output supported;
- image supported;
- text-only semantic placeholder required.

- [ ] **Step 3.3: Verify Computer Use outputs remain semantically rich**

Use existing Computer Use size fixtures plus a new visible_text fixture.

### Phase 4: Deferred tool and `tool_search` regression validation

**Goal:** Treat existing `tool_search`, `tool_search_output`, deferred tools, and subagent exposure as protected behavior. Add regression/live validation only; do not reimplement or downgrade it.

**Files:**

- Modify: `backend/internal/service/codex_gateway_deepseek_request.go`
- Modify: `backend/internal/service/codex_gateway_anthropic_request.go`
- Modify: `backend/internal/service/codex_gateway_deepseek_stream.go`
- Modify: `backend/internal/service/codex_gateway_tool_mapping.go`
- Test: `backend/internal/service/codex_gateway_deepseek_adapter_test.go`, Anthropic tests.

- [ ] **Step 4.1: Add matrix tests**

Tool families:

- function tools;
- namespace tools;
- custom/freeform tools;
- local shell;
- Computer Use tools;
- `tool_search` deferred tools;
- subagent `multi_agent_v1.spawn_agent` discovered via `tool_search_output`.

Expected: each can be searched, called, and replayed without being degraded to unsupported generic calls.

- [ ] **Step 4.2: Verify `tool_search_output.tools` deterministic serialization**

Audit existing deterministic serialization. Add tests or minimal fixes only if names, namespace, descriptions, or parameters are not preserved.

- [ ] **Step 4.3: Verify model catalog exposes deferred tool support**

Ensure relevant model `experimental_supported_tools` / `supports_search_tool` / `multi_agent_version` fields are correct.

### Phase 5: Thin provider capability profile

**Goal:** Add an explicit profile abstraction only for confirmed capability facts, without changing existing DeepSeek/Claude/AGNES outbound behavior except for explicitly planned, snapshot-tested fields.

**Files:**

- Modify: `backend/internal/service/codex_gateway_provider_executor.go`
- Modify: `backend/internal/service/codex_gateway_model_registry.go`
- Modify: `backend/internal/service/codex_gateway_deepseek_request.go`
- Possibly create: `backend/internal/service/codex_gateway_provider_profile.go`
- Test: provider profile tests.

- [ ] **Step 5.1: Define profile struct**

Fields:

```go
type CodexGatewayProviderProfile struct {
    Provider string
    ReasoningProtocol string // openai_responses, deepseek, anthropic, agnes, none
    ThinkingParamPolicy string
    SupportedEfforts []string
    DefaultEffort string
    SupportsToolReasoningReplay bool
    SupportsStructuredToolOutput bool
    SupportsImageToolOutput bool
    CacheUsageShape string // deepseek_top_level, openai_nested, none
    SupportsOfficialUserID bool
    SupportsPromptCacheKey bool
    SupportsStrictTools bool
    SupportsResponsesWS bool
}
```

- [ ] **Step 5.2: Populate profiles**

Provider expectations:

- DeepSeek: `reasoning_protocol=deepseek`, efforts `high/max`, `SupportsToolReasoningReplay=true`, `SupportsOfficialUserID=true`, `SupportsPromptCacheKey=false`.
- Claude: `reasoning_protocol=anthropic`, signed thinking replay rules, no DeepSeek user_id field; Anthropic metadata where applicable.
- AGNES: separate profile; do not inherit DeepSeek assumptions unless confirmed.
- OpenAI native: `reasoning_protocol=openai_responses`, encrypted reasoning include, prompt_cache_key allowed.
- Generic OpenAI-compatible: configurable defaults.

- [ ] **Step 5.3: Wire profiles into request builders behind no-regression gates**

Do not perform a broad provider refactor. Enable profile wiring one provider at a time and only for fields already confirmed by tests. For each provider, capture outbound request golden snapshots before and after profile wiring; DeepSeek, Claude, and AGNES snapshots must remain identical except for the explicitly planned field being changed. Do not clean up or rewrite provider-native codecs opportunistically in this phase.

### Phase 6: DeepSeek official cache/reasoning regression audit

**Goal:** Preserve existing DeepSeek official-aligned behavior and close only confirmed gaps, especially reasoning-token usage propagation. Current code already has stable `user_id`, `stream_options.include_usage=true`, no official DeepSeek client `prompt_cache_key`, and tool-call reasoning replay guards.

**Files:**

- Modify: `backend/internal/service/codex_gateway_deepseek_request.go`
- Modify: `backend/internal/service/codex_gateway_deepseek_adapter.go`
- Modify: `backend/internal/service/codex_gateway_capture_state.go`
- Test: DeepSeek adapter tests.

- [ ] **Step 6.1: Preserve official DeepSeek no-`prompt_cache_key` behavior**

Audit existing behavior: official DeepSeek should not receive client `prompt_cache_key`; AGNES/OpenAI may use scoped prompt-cache-key semantics where already verified. Add a regression test if coverage is missing. Capture may still record client `prompt_cache_key_hash` as a client/session diagnostic, not as DeepSeek cache control.

- [ ] **Step 6.2: Verify stable official `user_id`**

Keep existing stable non-PII generation, validate format, and log `user_id_source` safely. Add tests only for missing format/source cases.

- [ ] **Step 6.3: Enforce DeepSeek reasoning replay policy**

Tests:

- no-tool assistant reasoning stripped or not replayed upstream;
- tool-call assistant reasoning replayed with tool_calls and preserved for later turns;
- missing reasoning in stored DeepSeek tool-call state fails closed or synthesizes only when explicitly safe.

- [ ] **Step 6.4: Verify `stream_options.include_usage=true`**

Existing DeepSeek streaming should already include it. Add or strengthen regression tests unless a known incompatible provider disables it by profile.

- [ ] **Step 6.5: Normalize usage**

Extract only DeepSeek official usage fields in this phase:

- `usage.prompt_cache_hit_tokens`
- `usage.prompt_cache_miss_tokens`
- `usage.completion_tokens_details.reasoning_tokens`

Do not treat `prompt_tokens_details.cached_tokens` as a DeepSeek official field. That nested field belongs in the generic/OpenAI-compatible usage normalizer and must be tested separately so DeepSeek capture never fabricates it. Propagate the official DeepSeek fields into provider usage extras and capture.

### Phase 7: Extend existing tool schema canonicalization and prefix stability

**Goal:** Extend existing tool canonicalization only where gaps are proven. Do not create a parallel canonicalizer or change default tool order unless an explicit cache-stability profile flag is enabled.

**Files:**

- Modify: `backend/internal/service/codex_gateway_tool_mapping.go` and existing canonicalization helpers
- Create: `backend/internal/service/codex_gateway_tool_schema_canonicalization.go` only if no existing helper can be safely extended
- Test: canonicalization tests.

- [ ] **Step 7.1: Extend existing safe schema canonicalization only for proven gaps**

Rules:

- sort object keys;
- sort `required` and `dependentRequired` arrays;
- do not sort `enum`, `oneOf`, `anyOf`, examples, or description text;
- empty schema can normalize to `{"type":"object"}` only where provider accepts it.

- [ ] **Step 7.2: Optionally sort tools after aliasing**

Default behavior must preserve existing tool order. Only when an explicit cache-stability profile flag is enabled, sort by stable upstream function name after aliasing. Preserve collision-safe alias map. Add a flag-off test proving the outbound body keeps the old order.

- [ ] **Step 7.3: Add cache-shape hash tests**

Same semantic schema with different input key order produces same hash and same outbound body.

### Phase 8: Tool pairing diagnostics/action-policy gap audit

**Goal:** Build on existing fail-close checks and apicompat repair tests. Add a service-level diagnostic/action policy only for outbound views; never silently rewrite the canonical transcript.

**Files:**

- Modify/create: `backend/internal/service/codex_gateway_tool_pairing.go` only if existing DeepSeek/Anthropic pairing helpers cannot host the diagnostic policy cleanly
- Modify: `backend/internal/service/codex_gateway_deepseek_request.go`
- Test: pairing tests.

- [ ] **Step 8.1: Audit/extend pairing analyzer**

Existing paths already detect several invalid states. Add only missing detection for:

- orphan output;
- dangling tool call;
- duplicate call id;
- empty call id;
- out-of-order outputs.

- [ ] **Step 8.2: Provider-specific action policy**

Policy options:

- `fail_close`
- `sanitize_view_only`
- `repair_placeholder`
- `drop_orphan`

Default:

- DeepSeek/OpenAI-compatible: sanitize outbound view with diagnostics for clear cases; fail close for ambiguous cases.
- Claude: keep Anthropic-specific pairing rules.
- OpenAI native Responses: prefer pass-through unless native API rejects.

- [ ] **Step 8.3: Capture diagnostics**

Record `tool_pairing_invalid`, `tool_pairing_action`, and reason without raw outputs.

### Phase 9: Prefix-shape diagnostics field gap audit

**Goal:** Extend existing capture diagnostics with missing aliases/fields. Do not rebuild capture or duplicate existing hashes.

**Files:**

- Modify: `backend/internal/service/codex_gateway_capture_state.go`
- Modify: `backend/internal/service/augment_gateway_openai_adapter.go` if shared diagnostics exist
- Test: capture redaction tests.

- [ ] **Step 9.1: Add prefix-shape hash computation**

Hash only:

- stable system/developer text;
- canonical tools JSON;
- stable prefix messages before latest user turn;
- compression/injection version strings.

Use HMAC/redaction helpers; never log raw text.

- [ ] **Step 9.2: Add miss attribution hints**

Compare previous same session/provider shape where available:

- system changed;
- tools changed;
- message prefix changed;
- compression version changed;
- provider/account changed;
- unknown.

- [ ] **Step 9.3: Report DeepSeek cache fields accurately**

Dashboard/capture must distinguish:

- DeepSeek official `prompt_cache_hit_tokens/miss_tokens`;
- OpenAI nested cached tokens;
- client `prompt_cache_key` as metadata only.

### Phase 10: Computer Use semantic compression regression protection

**Goal:** Protect the existing DeepSeek/AGNES Computer Use prompt and semantic compression behavior, then add fixtures for any demonstrated gaps. Any compression format change must be versioned and regression-tested.

**Files:**

- Modify: existing Computer Use compression helpers in Codex Gateway request path.
- Test: `backend/internal/service/testdata/codex_gateway_deepseek_native_parity/computer_use_output_sizes.json` plus new fixtures.

- [ ] **Step 10.1: Document current compression version/invariants and add missing version field only if absent**

Compression must retain:

- `visible_text` if present;
- active app/bundle id;
- screenshot/truncated status;
- actionable element ids and labels;
- settable text inputs;
- operable lower-screen lines;
- most recent error;
- enough content to answer without blind scroll loops.

- [ ] **Step 10.2: Add fixtures for Electron and canvas-like apps**

Cases:

- AX tree empty but visible_text present;
- screenshot metadata truncated;
- settable input available;
- stale element retry;
- text reply present in visible_text but not accessibility tree.

- [ ] **Step 10.3: Provider-specific strategy prompt stability**

Ensure instructions are stable, deduplicated, and injected only when Computer Use tools are present.

### Phase 11: OpenAI WS v2 readiness gate audit

**Goal:** Confirm Codex Gateway catalog/routes do not expose unsupported WS semantics. OpenAI Gateway already has WS v2 infrastructure; this plan does not require Codex Gateway to implement half-WS support.

**Files:**

- Modify: `backend/internal/service/openai_ws_protocol_resolver.go`
- Modify: `backend/internal/service/codex_gateway_protocol_gate.go`
- Test: WS gate tests.

- [ ] **Step 11.1: Audit catalog `supports_websockets` exposure**

If a provider lacks full WS v2 support, do not expose WS support.

- [ ] **Step 11.2: Add readiness tests**

Assert HTTP path does not require `previous_response_id`; WS path does if incremental input is used.

- [ ] **Step 11.3: Document future WS implementation checklist**

Use Codex CLI evidence list from this plan.

### Phase 12: Live smoke matrix and capture validation

**Goal:** Prove changes preserve 99%+ native feel without hidden regressions.

**Execution gates:** Live smoke tests require an actually running Codex Desktop/app-server wired to the current backend, available upstream credentials for the provider being tested, and capture enabled. A provider-specific smoke may be skipped only when that provider/key is unavailable; it must be marked skipped with reason, not passed. Mandatory post-change gates for any touched provider must pass before moving to the next checkpoint. Any failed mandatory smoke that shows protocol regression, missing terminal event, broken tool pairing, or cache/usage misreporting blocks the checkpoint.

**Smoke prompts:**

1. Basic repo understanding with file read/search.
2. Deferred tool search and subagent spawn model list.
3. Computer Use simple controllable app flow.
4. Computer Use Electron app visible_text flow.
5. Web Search bridge.
6. Image input / structured tool output.
7. Stop/Continue/Resume or WS/native continuation if enabled.
8. DeepSeek tool-call reasoning replay multi-turn.
9. DeepSeek cache prefix stability repeated prompt.
10. Claude Anthropic thinking/tool loop.
11. AGNES tool loop and interruption recovery.

**Validation:**

- Capture contains terminal events.
- Tool calls and outputs pair correctly.
- Deferred tools appear and are callable.
- No provider receives unsupported cache control fields.
- DeepSeek usage records hit/miss fields when upstream returns them.
- Computer Use outputs do not explode tokens unnecessarily and keep visible_text.
- No DeepSeek/Claude regression from AGNES-specific changes.

---

## 7. Quality Gates

Before merging implementation:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/upstream-0.1.135-sync/backend
go test ./internal/pkg/apicompat -count=1 -timeout=240s
go test ./internal/service -run 'CodexGateway|DeepSeek|Anthropic|Agnes|Responses|ToolSearch|ComputerUse|ModelRegistry|ProtocolGate|OpenAIWS' -count=1 -timeout=360s
go test ./internal/handler ./internal/server/routes -run 'CodexGateway|Gateway|Responses|Models' -count=1 -timeout=240s
```

If feasible after implementation:

```bash
go test ./... -count=1 -timeout=600s
```

Live gates:

- Codex Desktop model list shows intended DeepSeek/Claude/AGNES models.
- DeepSeek can call deferred subagents and Computer Use.
- Claude direct can use tools without DeepSeek-specific prompt pollution.
- AGNES failures do not affect DeepSeek/Claude profiles.
- DeepSeek cache diagnostics show correct official hit/miss fields or an explicit absence reason.

---

## 8. Risk Register

### R1: Over-absorbing generic bridges loses native tools

Mitigation: use upstream as invariant tests, not replacement code, for local provider-native mapping.

### R2: Misusing `previous_response_id`

Mitigation: document and test that HTTP Responses does not require it; only WS v2 uses it when enabled.

### R3: False cache claims

Mitigation: remove DeepSeek `prompt_cache_key` assumptions; report hit/miss only from official usage fields.

### R4: Reasoning replay conflict

Mitigation: provider profile distinguishes OpenAI encrypted reasoning, Anthropic signed thinking, and DeepSeek `reasoning_content` replay.

### R5: Tool schema canonicalization changes semantics

Mitigation: only sort object keys and set-like arrays; do not reorder semantic arrays.

### R6: Tool repair changes transcript meaning

Mitigation: default to diagnostics/fail-close for ambiguous cases; repair only outbound sanitized view with capture marks.

### R7: Computer Use compression loses actionable content

Mitigation: fixture tests must assert retention of visible_text, element ids, settable inputs, and error/truncation status.

### R8: App.Server.v2 temptation

Mitigation: keep Gateway boundary documented; app-server JSON-RPC remains Desktop/Core local control plane.

---

## 8.1 Phase Status Summary for Execution Agents

| Phase | Status | Worker posture |
| --- | --- | --- |
| 0 | Partially implemented | Baseline + missing fixtures only |
| 1 | Partially implemented | Wire gap tests + minimal fixes; no writer rewrite |
| 2 | Partially implemented | OpenAI pass-through terminal audit; protect DeepSeek/Anthropic |
| 3 | Partially implemented | Provider-specific structured output gap; no compression rewrite |
| 4 | Already implemented / protected | Regression/live validation only |
| 5 | New thin abstraction | Snapshot-gated, provider-by-provider |
| 6 | Mostly implemented | Usage/reasoning-token gap and regression tests |
| 7 | Partially implemented | Extend existing canonicalizer only |
| 8 | Partially implemented | Diagnostics/outbound view only |
| 9 | Mostly implemented | Add missing diagnostic fields/aliases only |
| 10 | Already implemented / protected | Regression fixtures only unless versioned change is proven |
| 11 | Mostly existing outside Codex Gateway | Gate audit only |
| 12 | Validation | Live smoke/capture matrix |

---

## 9. Recommended Checkpoints

### Checkpoint 1: Protocol invariants

Phases 0-3:

- wire shape;
- lifecycle;
- terminal discipline;
- structured tool outputs.

Commit after focused tests pass.

### Checkpoint 2: Deferred tools and provider profiles

Phases 4-6:

- tool_search/deferred tools;
- provider capability profile;
- DeepSeek official cache/reasoning corrections.

Commit after DeepSeek/Claude/AGNES focused tests pass.

### Checkpoint 3: Cache diagnostics and stability

Phases 7-9:

- schema canonicalization;
- pairing diagnostics;
- prefix-shape diagnostics.

Commit after capture/redaction tests pass.

### Checkpoint 4: Computer Use and live matrix

Phases 10-12:

- Computer Use semantic compression audit;
- WS readiness gate;
- live smoke/capture validation.

Commit after live smoke evidence is captured.

---

## 10. Bottom Line

The upstream v0.1.135 work and Codex CLI source provide valuable **protocol invariants** and **golden behaviors**, not a better wholesale architecture. The local Codex Gateway architecture remains the right foundation because it already preserves provider-native semantics for DeepSeek, Claude, AGNES, OpenAI Responses, deferred tools, and Computer Use.

The highest-value absorption path is:

1. strict Responses SSE wire shape;
2. lifecycle/terminal discipline;
3. structured tool output preservation;
4. deferred tool and model catalog parity;
5. provider profiles;
6. DeepSeek official `user_id`/usage/reasoning replay semantics;
7. schema canonicalization and prefix-shape diagnostics;
8. safe tool pairing diagnostics;
9. Computer Use semantic compression tests.

The plan deliberately rejects App.Server.v2 JSON-RPC implementation, Gateway-managed compaction/session ownership, DeepSeek `prompt_cache_key` myths, Anthropic `cache_control` myths, and Reasonix's blanket reasoning stripping rule.
