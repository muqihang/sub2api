# DeepSeek Codex Native Parity Remediation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the remaining DeepSeek-via-Codex gaps that make the app feel non-native: deferred tool search/subagents, subagent model override freshness, skill routing parity, Computer Use visibility, abort/resume cache stability, and subagent session registration reliability.

**Architecture:** Keep DeepSeek on the OpenAI-compatible Chat Completions upstream and keep Codex App on Responses events. The gateway must translate DeepSeek's function-tool shape back into the exact Codex Responses item families that Codex core uses for native tool routing, while zhumeng-agent and capture tooling verify app-server model/plugin/runtime state without replacing app-server itself.

**Tech Stack:** Go backend Codex Gateway, Python zhumeng-agent, Codex Desktop capture hooks, JSONL session/capture fixtures, Go `testing` with `stretchr/testify`, Python `pytest`.

---

## Native Parity Standard

The target is not literal model-quality identity with GPT, because providers differ. The target is Codex-native behavior at the protocol and product layer:

- tool calls, deferred tools, Skills, Sub-Agent, Computer Use, streaming, replay, and resume must use the Codex-native item families and session semantics expected by Codex App;
- DeepSeek should not require user-visible workarounds that GPT models do not need for the same Codex workflow;
- stable-prefix abort/resume cache behavior should keep the already-implemented remediation target at 99%+ after warmup and push as close to 100% as the provider allows;
- any sub-99% cache run, `0 cached` turn after warmup, dropped tool, missing Skill load, missing Sub-Agent exposure, or lost Computer Use element must have capture-backed attribution instead of being accepted as normal.

## Context And Boundaries

Work in this worktree:

`/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/deepseek-codex-cache-remediation`

Read runtime evidence from the main checkout and Codex session files when needed, but write plan/code changes in the worktree.

Do not touch the unrelated dirty files currently present in the worktree:

- `backend/internal/service/codex_gateway_model_registry_test.go`
- `deploy/.env.p1_6`
- `deploy/docker-compose-p1_6.yml`
- `app-main-BnTSnuSB.js`
- `app-server-connection-state-DdI1cMjA.js`
- `app-server-manager-hooks-BUQb1vpx.js`
- `app-server-manager-signals-7MlBpIlX.js`

Do not duplicate the cache remediation work already present in:

`docs/superpowers/plans/2026-05-29-deepseek-codex-cache-remediation.md`

That branch already includes full replay messages, reasoning replay, stable `user_id` diagnostics, tool schema ordering, request allowlist, capture cache diagnostics, stream text deltas, and Computer Use summary tests. This plan only extends that foundation where the new parity evidence shows remaining gaps.

## Evidence Ledger

### Confirmed

- In the failed session `rollout-2026-06-03T03-42-38-019e8d13-ff25-7bd0-9cb7-95e67c398c4b.jsonl`, DeepSeek emitted seven ordinary `function_call` items named `tool_search` between `2026-06-03T11:32:45Z` and `2026-06-03T11:35:12Z`, with no `tool_search_output`.
- In the successful/native-shaped session `rollout-2026-06-03T04-42-56-019e8d4b-3217-74d1-afa8-c143c1ca8ba5.jsonl`, Codex recorded `tool_search_call` at `2026-06-03T11:43:33.493Z` with fields `type`, `call_id`, `status`, `execution`, and `arguments`, followed by `tool_search_output` at `2026-06-03T11:43:33.599Z`.
- The same successful `tool_search_output` exposed `multi_agent_v1.spawn_agent`, but its model override description listed only five Claude models and no DeepSeek models. The local `~/.codex/zhumeng-codex-models.json` contains `deepseek-v4-pro` and `deepseek-v4-flash`, so the stale list is not a backend catalog absence.
- The DeepSeek request path currently flattens `tool_search` into an ordinary function tool in `backend/internal/service/codex_gateway_tool_mapping.go`.
- `codexGatewayClientVisibleToolItemType` currently maps every non-custom tool to `function_call` in `backend/internal/service/codex_gateway_legacy_tool_normalization.go`, so a DeepSeek `tool_search` function call cannot become a native `tool_search_call`.
- `convertCodexGatewayInputItem` handles `function_call_output`, `local_shell_call_output`, and `custom_tool_call_output`, but not `tool_search_call` or `tool_search_output`, in `backend/internal/service/codex_gateway_deepseek_request.go`.
- The child Computer Use session `rollout-2026-06-03T04-43-59-019e8d4c-2a31-7a51-a622-d16f037b65d4.jsonl` has `get_app_state` raw outputs around 86k-108k chars in the last samples, with app-state text around 4.0k-5.0k chars, screenshot/base64 around 81k-102k chars, and closed `</app_state>` markers. The raw session evidence does not show a simple 6k-7k truncation of the stored tool output.
- The same child session reported `44302` input tokens and `0` cached input tokens at `2026-06-03T11:48:43.272Z`, after prior turns had high cached-token counts.
- Local gateway captures for `2026-06-03` end at `2026-06-03T08:27:07Z`. The `2026-06-03T11:48:43Z` cache miss has no matching local capture.
- DeepSeek tool-output normalization currently has `codexGatewayDeepSeekToolOutputMaxChars = 3500` and a `1200` char fallback preview. Existing remediation tests already add structured Computer Use summaries and a hosted vision proxy, but runtime proof still needs a DeepSeek-visible request fixture for the failing Computer Use shape.
- Developer messages in the failing sessions include skills instructions, and the model read `SKILL.md` via shell. Do not describe this as "DeepSeek has no Skills"; this is a routing/runtime parity issue, not a complete skill-injection absence.

### Still Unproven

- The exact DeepSeek upstream request body for the `2026-06-03T11:48:43Z` cache miss is not captured locally.
- The app-server source of the stale `spawn_agent` model override list is not yet isolated. The leading hypothesis is the `TurnContext.available_models` / model manager cache path, not the `/codex/v1/models` backend catalog.
- The subagent `unknown conversation` / `thread read empty` / later `maybe_resume_success` ordering needs a narrow trace. Broad grep is polluted by session/base64 content and must not be used as proof.

## Files

- Modify: `backend/internal/service/codex_gateway_types.go`
  - Add explicit item constants only if tests need them.
- Modify: `backend/internal/service/codex_gateway_tool_mapping.go`
  - Preserve DeepSeek upstream `tool_search` as a function tool, but tag/identify it for native Responses reconstruction.
- Modify: `backend/internal/service/codex_gateway_legacy_tool_normalization.go`
  - Return `tool_search_call` for the special `tool_search` entry.
- Modify: `backend/internal/service/codex_gateway_deepseek_request.go`
  - Convert `tool_search_call` and `tool_search_output` in replay/current input.
  - Preserve complete tool output content for DeepSeek without misclassifying it as a user message.
- Modify: `backend/internal/service/codex_gateway_deepseek_adapter.go`
  - Reconstruct non-stream DeepSeek `tool_search` calls as `tool_search_call`.
- Modify: `backend/internal/service/codex_gateway_deepseek_stream.go`
  - Reconstruct streamed DeepSeek `tool_search` calls as `tool_search_call` and avoid function-argument delta events for this item family.
- Modify: `backend/internal/service/codex_gateway_deepseek_request_test.go`
- Modify: `backend/internal/service/codex_gateway_deepseek_adapter_test.go`
- Modify: `backend/internal/service/codex_gateway_deepseek_stream_test.go`
- Modify: `backend/internal/service/codex_gateway_tool_mapping_test.go`
- Modify: `backend/internal/service/codex_gateway_capture_diagnostics.go`
  - Add request-visible tool summary diagnostics only if needed to prove Computer Use content survival.
- Modify: `backend/internal/service/codex_gateway_capture_test.go`
- Modify: `tools/zhumeng-agent/src/zhumeng_agent/cli.py`
  - Improve catalog sync diagnostics if app-server model freshness depends on restart or stale fallback behavior.
- Modify: `tools/zhumeng-agent/src/zhumeng_agent/doctor.py`
  - Report Codex catalog freshness, DeepSeek presence, and capture state.
- Modify: `tools/zhumeng-agent/src/zhumeng_agent/adapters/codex/capture_shape.py`
  - Extend desktop capture report shape for deferred tool and app-server session registration evidence.
- Modify: `tools/zhumeng-agent/src/zhumeng_agent/adapters/codex/capture_injector.py`
  - Add narrow, shape-only capture hooks for app-server event order only if existing hooks cannot report it.
- Modify: `tools/zhumeng-agent/tests/test_cli.py`
- Modify: `tools/zhumeng-agent/tests/test_desktop_diagnostics.py`
- Modify: `tools/zhumeng-agent/tests/test_codex_capture_shape.py`
- Modify: `docs/codex-gateway/smoke.md`
  - Add the new golden parity smoke scenarios.

## Task 1: Freeze Parity Fixtures Before Code Changes

**Files:**

- Create or modify: `backend/internal/service/testdata/codex_gateway_deepseek_native_parity/*.json`
- Modify: `backend/internal/service/codex_gateway_deepseek_request_test.go`
- Modify: `backend/internal/service/codex_gateway_deepseek_stream_test.go`
- Modify: `tools/zhumeng-agent/tests/test_codex_capture_shape.py`

- [ ] **Step 1: Add sanitized fixture metadata for failed `tool_search`**

Create a fixture that records only:

- session path hash or fixture label;
- timestamps;
- `function_call name=tool_search`;
- arguments JSON;
- absence of matching `tool_search_output`.

Do not include unrelated developer instructions, secrets, or large session payloads.

- [ ] **Step 2: Add sanitized fixture metadata for native `tool_search_call`**

Create a fixture with this exact observed shape:

```json
{
  "type": "tool_search_call",
  "call_id": "call_fixture",
  "status": "completed",
  "execution": "client",
  "arguments": {
    "query": "sub-agent dispatch multi-agent DeepSeek V4 Flash model tool",
    "limit": 10
  }
}
```

And a matching output shape:

```json
{
  "type": "tool_search_output",
  "call_id": "call_fixture",
  "status": "completed",
  "execution": "client",
  "tools": [
    {
      "type": "namespace",
      "name": "multi_agent_v1",
      "tools": [
        {
          "name": "spawn_agent",
          "description": "sanitized spawn-agent tool description",
          "input_schema": {
            "type": "object",
            "properties": {
              "task": {"type": "string"},
              "model": {"type": "string"}
            },
            "required": ["task"]
          }
        }
      ]
    }
  ]
}
```

The fixture must mirror the captured namespace shape as closely as practical and assert that `multi_agent_v1.spawn_agent` survives output replay/conversion. A generic non-empty `tools` placeholder is not enough because sub-agent discovery is the user-visible failure.

- [ ] **Step 3: Add sanitized GPT/native baseline labels**

For every parity fixture, record whether the target shape came from:

- GPT/OpenAI-native Codex App behavior;
- a successful Codex-native DeepSeek bridge session;
- an observed DeepSeek failure.

Do not use invented target schemas when a captured native GPT or native-shaped Codex fixture exists.

- [ ] **Step 4: Add sanitized fixture metadata for Computer Use output sizes**

Record only sizes and booleans:

- raw output chars;
- app-state chars;
- screenshot chars;
- whether `</app_state>` is present;
- whether DeepSeek-visible normalized output retained `computer_screenshot`, `operable_lines`, and lower-screen actionable lines.

- [ ] **Step 5: Run fixture tests**

Run:

```bash
cd backend
go test ./internal/service -run 'TestCodexGatewayDeepSeek.*NativeParityFixture|TestCodexGatewayDeepSeek.*ToolSearch' -count=1
```

Expected before implementation: tests that require `tool_search_call` reconstruction fail.

## Task 2: Bridge DeepSeek `tool_search` To Native Codex Deferred Tool Items

**Files:**

- Modify: `backend/internal/service/codex_gateway_tool_mapping.go`
- Modify: `backend/internal/service/codex_gateway_legacy_tool_normalization.go`
- Modify: `backend/internal/service/codex_gateway_deepseek_request.go`
- Modify: `backend/internal/service/codex_gateway_deepseek_adapter.go`
- Modify: `backend/internal/service/codex_gateway_deepseek_stream.go`
- Modify: `backend/internal/service/codex_gateway_tool_mapping_test.go`
- Modify: `backend/internal/service/codex_gateway_deepseek_request_test.go`
- Modify: `backend/internal/service/codex_gateway_deepseek_adapter_test.go`
- Modify: `backend/internal/service/codex_gateway_deepseek_stream_test.go`

- [ ] **Step 1: Write failing request conversion tests**

Add:

- `TestCodexGatewayDeepSeekRequest_ConvertsToolSearchCallToDeepSeekToolCall`
- `TestCodexGatewayDeepSeekRequest_ConvertsToolSearchOutputToToolMessage`

Expected:

- `tool_search_call` becomes an assistant Chat Completions `tool_calls` entry named `tool_search`.
- `tool_search_output` becomes a `role:"tool"` message with the same `tool_call_id`.
- `tool_search_output.tools` is serialized as compact deterministic JSON in `content`.
- serialized `content` preserves a sanitized `multi_agent_v1.spawn_agent` entry when present;
- The output is not converted to an empty user message.
- A native `tool_search_call` fixture without a `name` field still converts successfully.

Run:

```bash
cd backend
go test ./internal/service -run 'TestCodexGatewayDeepSeekRequest_ConvertsToolSearch' -count=1
```

Expected before implementation: FAIL.

- [ ] **Step 2: Write failing stream reconstruction test**

Add `TestCodexGatewayDeepSeekStream_ToolSearchFunctionCallEmitsToolSearchCall`.

Build a DeepSeek SSE fixture with one streamed function call:

```json
{
  "id": "chatcmpl_tool_search",
  "model": "deepseek-v4-pro",
  "choices": [
    {
      "delta": {
        "tool_calls": [
          {
            "index": 0,
            "id": "call_tool_search",
            "type": "function",
            "function": {
              "name": "tool_search",
              "arguments": "{\"query\":\"spawn_agent\",\"limit\":10}"
            }
          }
        ]
      }
    }
  ]
}
```

Expected Responses events:

- `response.output_item.added` item has `type:"tool_search_call"`;
- item has `call_id:"call_tool_search"`;
- item has `execution:"client"`;
- item has parsed `arguments`;
- `response.output_item.done` also carries `type:"tool_search_call"`;
- no `response.function_call_arguments.delta`;
- no `function_call` item for this call.

Run:

```bash
cd backend
go test ./internal/service -run 'TestCodexGatewayDeepSeekStream_ToolSearchFunctionCallEmitsToolSearchCall' -count=1
```

Expected before implementation: FAIL.

- [ ] **Step 3: Write failing non-stream reconstruction test**

Add `TestCodexGatewayDeepSeekAdapter_ToolSearchFunctionCallMapsToToolSearchCall`.

Expected output item:

```json
{
  "type": "tool_search_call",
  "call_id": "call_tool_search",
  "status": "completed",
  "execution": "client",
  "arguments": {"query": "spawn_agent", "limit": 10}
}
```

Run:

```bash
cd backend
go test ./internal/service -run 'TestCodexGatewayDeepSeekAdapter_ToolSearchFunctionCallMapsToToolSearchCall' -count=1
```

Expected before implementation: FAIL.

- [ ] **Step 4: Write non-regression tests for neighboring tool families**

Add tests that prove the exact `tool_search` special case does not change:

- ordinary function tools;
- custom tools;
- local shell tools;
- hosted web/search tools;
- Anthropic adapter/stream behavior unless that path already intentionally emits native `tool_search_call`.

Prefer keeping the native reconstruction DeepSeek-scoped where practical. If the shared helper is changed, prove every shared adapter path still emits its intended item family.

Run:

```bash
cd backend
go test ./internal/service -run 'ToolSearch|Anthropic.*Tool|LocalShell|CustomTool' -count=1
```

Expected before implementation: only the new exact-`tool_search` parity tests fail; existing non-`tool_search` behavior remains unchanged.

- [ ] **Step 5: Implement a single `tool_search` identity helper**

Add a helper such as:

```go
func codexGatewayIsToolSearchEntry(entry CodexGatewayToolNameMapEntry) bool
```

Return true only when the visible name or alias is exactly `tool_search`. Do not match hosted web search, browser search, or ordinary project search tools by substring.

- [ ] **Step 6: Preserve upstream function-tool shape**

Keep `flattenCodexGatewayToolSearchTool` output as a DeepSeek function tool. DeepSeek still needs a normal Chat Completions function schema.

Only change downstream Responses reconstruction and input replay behavior.

- [ ] **Step 7: Emit `tool_search_call` in stream path**

In `codex_gateway_deepseek_stream.go`:

- detect the `tool_search` entry before generic function item emission;
- parse buffered arguments into a JSON object when possible;
- emit the item with `type`, `call_id`, `status`, `execution`, and `arguments`;
- mark it completed in `writeDoneEvents`;
- skip function argument delta/done events for this item family.

- [ ] **Step 8: Emit `tool_search_call` in non-stream path**

In `codex_gateway_deepseek_adapter.go`:

- detect the `tool_search` entry in `codexGatewayDeepSeekToolCallOutputItem`;
- parse arguments;
- emit the native item shape;
- still persist the stored tool call so the following `tool_search_output` can replay.

- [ ] **Step 9: Accept `tool_search_call` and `tool_search_output` on request replay**

In `convertCodexGatewayInputItem`:

- route `tool_search_call` through a wrapper or dedicated converter that sets the canonical name to `tool_search` before reusing function-call conversion logic;
- route `tool_search_output` through a new converter that uses `tools` when `output` is absent;
- preserve `call_id`;
- serialize the `tools` list deterministically.

- [ ] **Step 10: Run targeted tests**

Run:

```bash
cd backend
go test ./internal/service -run 'ToolSearch|NeedsToolContinuation' -count=1
```

Expected: PASS.

## Task 3: Make Subagent Model Override Lists Fresh And Observable

**Files:**

- Modify: `tools/zhumeng-agent/src/zhumeng_agent/cli.py`
- Modify: `tools/zhumeng-agent/src/zhumeng_agent/doctor.py`
- Modify: `tools/zhumeng-agent/desktop/src/lib/modelCatalog.ts`
- Modify: `tools/zhumeng-agent/desktop/src/lib/modelCatalog.test.ts`
- Modify: `tools/zhumeng-agent/tests/test_cli.py`
- Modify: `tools/zhumeng-agent/tests/test_desktop_diagnostics.py`
- Modify: `docs/codex-gateway/smoke.md`

- [ ] **Step 1: Add a diagnostic test for catalog freshness**

Add a Python test that builds a Codex CLI catalog containing:

- `deepseek-v4-pro`;
- `deepseek-v4-flash`;
- at least one Claude model.

Expected doctor output:

- reports DeepSeek models present in `model_catalog_json`;
- reports catalog file mtime/hash;
- reports the active configured default model;
- reports whether Codex restart is required after catalog/config change.

Run:

```bash
cd tools/zhumeng-agent
pytest tests/test_cli.py tests/test_desktop_diagnostics.py -k 'model_catalog or codex_doctor' -q
```

Expected before implementation: FAIL for missing freshness fields.

- [ ] **Step 2: Add `spawn_agent` model-list mismatch reporting**

Extend capture report shape, not model routing, to flag:

- `spawn_agent` override list contains no DeepSeek models;
- local `model_catalog_json` contains DeepSeek models;
- capture timestamp and catalog mtime/hash.

This should produce a diagnostic like:

```json
{
  "spawn_agent_model_override_mismatch": true,
  "catalog_has_deepseek": true,
  "spawn_agent_has_deepseek": false
}
```

- [ ] **Step 3: Determine app-server refresh boundary**

Use shape-only capture and manual smoke to determine whether app-server reads the model catalog:

- on process start only;
- on `model/list`;
- through a cache with TTL;
- through `models_manager.list_models(OnlineIfUncached)` or equivalent.

Record the finding in `docs/codex-gateway/smoke.md`.

- [ ] **Step 4: Implement the least invasive refresh behavior**

If the app-server only reads the catalog at process start:

- zhumeng-agent should surface "restart Codex required" after model catalog changes;
- do not try to patch minified app-server code;
- keep the existing restart-required mechanism as the user-facing fix.

If app-server supports a refresh/list method:

- add a zhumeng-agent command or doctor hint to invoke it;
- verify the next `tool_search_output` `spawn_agent` description includes DeepSeek.

- [ ] **Step 5: Run model/catalog tests**

Run:

```bash
cd tools/zhumeng-agent
pytest tests/test_codex_config_manager.py tests/test_cli.py tests/test_desktop_diagnostics.py tests/test_codex_capture_shape.py -q
```

Expected: PASS.

## Task 4: Verify Skills Runtime Parity Without Overstating The Gap

**Files:**

- Modify: `docs/codex-gateway/smoke.md`
- Modify: `tools/zhumeng-agent/src/zhumeng_agent/doctor.py`
- Modify: `tools/zhumeng-agent/tests/test_desktop_diagnostics.py`

Do not modify `backend/internal/service/codex_gateway_model_registry_test.go` in this worktree unless the user first resolves or authorizes the unrelated dirty change. Skills parity diagnostics should be implemented in zhumeng-agent and docs unless fresh evidence proves the backend catalog instructions are the source.

- [ ] **Step 1: Add a skills evidence section to doctor output**

Report only facts that can be observed locally:

- configured marketplaces from `config.toml`;
- enabled plugins from `config.toml`;
- skills directories present under `$CODEX_HOME/skills`, `$CODEX_HOME/superpowers/skills`, and enabled plugin cache paths;
- whether current model catalog base instructions include Codex routing guidance for non-OpenAI providers.

Do not claim the model can or cannot use a skill based only on file presence.

- [ ] **Step 2: Add a smoke test for explicit DeepSeek skill-file use**

Add a manual smoke prompt to `docs/codex-gateway/smoke.md`:

```text
Using deepseek-v4-pro, read the superpowers:systematic-debugging SKILL.md and summarize only the four phase names.
Do not use tool_search.
```

Expected:

- model reads the local `SKILL.md` via shell;
- no `tool_search` is needed for ordinary skill-file use;
- response does not say skills are unavailable.

- [ ] **Step 3: Add a smoke test for implicit Skill trigger behavior**

Add a manual smoke prompt that triggers a skill by task shape instead of explicitly naming a file:

```text
Using deepseek-v4-pro, diagnose a reproducible failing test in this repository.
Follow the applicable local skill instructions before proposing a fix.
Do not implement code.
```

Expected:

- model identifies that `superpowers:systematic-debugging` applies from injected skill instructions;
- model opens the local `SKILL.md` before proposing diagnosis steps;
- smoke notes record the exact `SKILL.md` path opened so failures can distinguish routing/instruction issues from ordinary file lookup issues;
- model follows the skill's evidence-first phases;
- no claim is made that Skills are unavailable;
- no `tool_search` is needed for ordinary file-backed Skills.

- [ ] **Step 4: Add a smoke test for deferred tool use**

Add a separate prompt:

```text
Using deepseek-v4-pro, search for the deferred subagent tool and spawn one no-op explorer.
```

Expected:

- `tool_search_call`;
- `tool_search_output`;
- `multi_agent_v1.spawn_agent`;
- no ordinary `function_call name=tool_search` visible in the session.

- [ ] **Step 5: Run relevant tests**

Run:

```bash
cd tools/zhumeng-agent
pytest tests/test_desktop_diagnostics.py -q
```

Expected: PASS.

## Task 5: Harden Computer Use DeepSeek-Visible Output

**Files:**

- Modify: `backend/internal/service/codex_gateway_deepseek_request.go`
- Modify: `backend/internal/service/codex_gateway_deepseek_vision_proxy.go`
- Modify: `backend/internal/service/codex_gateway_deepseek_request_test.go`
- Modify: `backend/internal/service/codex_gateway_capture_diagnostics.go`
- Modify: `backend/internal/service/codex_gateway_capture_test.go`
- Modify: `docs/codex-gateway/smoke.md`

- [ ] **Step 1: Add a failing regression for second-pass summary loss**

Build a `function_call_output` with:

- `screenshot` or `image_base64` larger than 80k chars;
- `app_state` or `accessibility_tree` around 5k-8k chars;
- lower-screen operable lines near the end;
- total summarized JSON above `3500` chars before final fallback.

Expected DeepSeek-visible content:

- no raw base64;
- semantic normalized content is preserved instead of raw screenshots/base64;
- includes `computer_screenshot` when hosted vision succeeds;
- includes `operable_lines`;
- includes at least one lower-screen input/reply/action line;
- includes `sha256` and original length diagnostics;
- does not collapse the whole object into only a `preview` field.

Run:

```bash
cd backend
go test ./internal/service -run 'TestCodexGatewayDeepSeekRequest_.*ComputerUse.*SecondPass|TestCodexGatewayDeepSeekRequestWithVisionProxy' -count=1
```

Expected before implementation: FAIL if current fallback erases required Computer Use fields.

- [ ] **Step 2: Split Computer Use summary budget by class**

Adjust normalization so Computer Use summaries have stable fields:

- `computer_screenshot` or `binary_or_image` summary;
- `accessibility_tree` / `visual_tree` summary;
- `operable_lines`;
- optional `preview`;
- hashes and original lengths.

The final fallback must be class-aware. It may trim individual previews, but it must not drop `operable_lines` or the screenshot vision summary.

- [ ] **Step 3: Add request diagnostics for tool-output summary**

Add redacted diagnostics under capture, for example:

```json
{
  "deepseek_tool_output_summary": {
    "tool_name": "get_app_state",
    "raw_chars": 108674,
    "normalized_chars": 3472,
    "classes": ["computer_screenshot", "accessibility_tree"],
    "operable_line_count": 8,
    "fallback_preview_only": false
  }
}
```

Do not record raw app text or raw screenshots unless raw payload capture is explicitly unlocked.

- [ ] **Step 4: Run Computer Use tests**

Run:

```bash
cd backend
go test ./internal/service -run 'ComputerUse|ToolOutput|VisionProxy|CaptureDiagnostics' -count=1
```

Expected: PASS.

## Task 6: Close Abort/Resume Cache Evidence Gaps Without Replanning Existing Cache Work

**Files:**

- Modify: `backend/internal/service/codex_gateway_capture_diagnostics.go`
- Modify: `backend/internal/service/codex_gateway_capture_test.go`
- Modify: `backend/internal/service/codex_gateway_deepseek_request_test.go`
- Modify: `docs/codex-gateway/smoke.md`

- [ ] **Step 1: Audit existing `previous_response_id` replay diagnostics**

The remediation branch already contains full replay messages, reasoning replay, tool-schema stability, stream text delta, and cache diagnostics tests. First audit the existing tests before adding new ones:

- `TestCodexGatewayDeepSeekRequest_ReplaysPreviousResponseFullMessagesPrefix`;
- `TestCodexGatewayDeepSeekRequest_PreviousResponseDeltaMatchesFullReplayPrefix`;
- `TestCodexGatewayDeepSeekRequest_PreservesResponsesReasoningItems`;
- `TestCodexGatewayDeepSeekRequest_EquivalentToolOrderHasStableToolSchemaHash`;
- `TestCodexGatewayCaptureV2DeepSeekCacheMissAttribution`;
- `TestCodexGatewayCaptureV2DeepSeekRecordsFullPrefixDiagnostics`.

Only extend tests if one of the required fields below is missing for the new `2026-06-03T11:48:43Z` evidence gap.

- [ ] **Step 2: Extend diagnostics only for missing fields**

Use an in-memory state store with `ReplayMessages`.

Expected capture fields:

- `previous_response_id_present:true`;
- `previous_response_replay_mode:"full_replay_messages"`;
- `state_lookup_status:"hit"`;
- `messages_full_hash`;
- `message_prefix_hash`;
- `message_suffix_hash`;
- `tool_schema_hash`;
- `request_shape_hash`;
- cache miss attribution can distinguish request-not-warmed from prefix-shape changes.

Run:

```bash
cd backend
go test ./internal/service -run 'TestCodexGateway.*PreviousResponse.*Diagnostics|TestCodexGateway.*Capture.*DeepSeekCache' -count=1
```

Expected: PASS after diagnostics are complete.

- [ ] **Step 3: Add a manual repro that forces a capture for abort/resume**

Add to `docs/codex-gateway/smoke.md`:

1. Enable gateway capture with shape-only mode.
2. Start a DeepSeek thread.
3. Execute one tool turn.
4. Interrupt or abort.
5. Resume the same thread.
6. Record token usage and gateway trace id.

Expected:

- gateway capture exists for the resumed DeepSeek request;
- session token usage and gateway `prompt_cache_*` fields can be correlated by trace/session hashes;
- any `0 cached` turn has a cache attribution reason or is classified as `upstream_best_effort_or_unknown`.

- [ ] **Step 4: Do not expand replay scope unless diagnostics prove a prefix gap**

The existing remediation branch already stores `ReplayMessages` and reasoning. Only change replay behavior if a fresh capture shows one of:

- missing stored replay state;
- replay mode fallback when full replay should exist;
- request shape changed;
- message prefix hash changed unexpectedly;
- tool schema hash changed unexpectedly.

- [ ] **Step 5: Run cache-focused tests**

Run:

```bash
cd backend
go test ./internal/service -run 'DeepSeek.*Replay|DeepSeek.*Cache|CaptureDiagnostics|StateStore' -count=1
```

Expected: PASS.

## Task 7: Trace Subagent Session Registration Races

**Files:**

- Modify: `tools/zhumeng-agent/src/zhumeng_agent/adapters/codex/capture_shape.py`
- Modify: `tools/zhumeng-agent/src/zhumeng_agent/adapters/codex/capture_injector.py`
- Modify: `tools/zhumeng-agent/tests/test_codex_capture_shape.py`
- Modify: `tools/zhumeng-agent/tests/test_cli.py`
- Modify: `docs/codex-gateway/smoke.md`

- [ ] **Step 1: Add shape-only event-order capture**

Capture only event names, timestamps, hashed conversation/thread ids, and safe status fields for:

- `thread/start`;
- `thread/resume`;
- `thread/read`;
- `item/started`;
- `item/completed`;
- app-server console messages containing `unknown conversation`;
- `thread read empty`;
- `maybe_resume_success`.

Do not capture user prompt text, tool output bodies, screenshots, or raw session content.

- [ ] **Step 2: Add report detection**

The capture report should flag:

```json
{
  "subagent_registration_race_suspected": true,
  "first_item_before_conversation_registered": true,
  "unknown_conversation_count": 3,
  "maybe_resume_success_after_unknown_conversation": true
}
```

- [ ] **Step 3: Add a manual subagent smoke**

Add to `docs/codex-gateway/smoke.md`:

```text
Using deepseek-v4-pro as controller, spawn one DeepSeek subagent that only says "ready" and then wait for it.
```

Expected:

- no `unknown conversation` before registration;
- if an `unknown conversation` appears, it is followed by deterministic resume recovery and no lost tool/result events;
- report includes ordered evidence.

- [ ] **Step 4: Add a conditional remediation path if the race is confirmed**

If the shape-only trace confirms a race in code we control:

- fix ordering so conversation/session registration is durable before first item/result delivery;
- or add a bounded retry/rehydration handoff around the failing read/resume boundary;
- add a regression test that replays the captured event order and proves no tool/result event is lost.

If the failing boundary is inside closed-source or minified Codex app-server code:

- do not patch minified bundles as the primary fix;
- document the boundary in `docs/codex-gateway/smoke.md`;
- make the capture report/doctor surface the exact condition and accepted restart/retry fallback.

- [ ] **Step 5: Run capture tests**

Run:

```bash
cd tools/zhumeng-agent
pytest tests/test_codex_capture_shape.py tests/test_cli.py -k 'capture' -q
```

Expected: PASS.

## Task 8: End-To-End Regression Matrix

**Files:**

- Modify: `docs/codex-gateway/smoke.md`

- [ ] **Step 1: Gateway unit tests**

Run:

```bash
cd backend
go test ./internal/service -run 'DeepSeek|CodexGatewayToolMapping|CodexGatewayModelRegistry|CodexGatewayService|CaptureDiagnostics|NeedsToolContinuation' -count=1
```

Expected: PASS.

- [ ] **Step 2: zhumeng-agent tests**

Run:

```bash
cd tools/zhumeng-agent
pytest tests/test_codex_config_manager.py tests/test_cli.py tests/test_desktop_diagnostics.py tests/test_codex_capture_shape.py tests/test_codex_model_picker.py -q
```

Expected: PASS.

- [ ] **Step 3: Manual native parity smoke**

Run these in Codex Desktop against DeepSeek:

- `tool_search` discovers `multi_agent_v1.spawn_agent`;
- `spawn_agent` model override description includes DeepSeek or doctor reports the exact refresh boundary;
- ordinary skill file read works without `tool_search`;
- an implicit Skill-trigger prompt causes DeepSeek to load and follow the relevant local `SKILL.md` without being handed the file path;
- Computer Use `get_app_state` gives the model lower-screen actionable elements or a vision summary plus operable lines;
- abort/resume preserves the already-implemented GPT-like warm-cache target on stable-prefix turns, with 99%+ prompt cache hit rate after warmup as the floor and as close to 100% as the provider allows; any lower or `0 cached` turn must have capture attribution;
- one DeepSeek subagent starts with the requested DeepSeek model, completes, and reports without registration loss.

- [ ] **Step 4: Capture report check**

Generate a report that links desktop capture and gateway capture:

```bash
zhumeng-agent codex capture report --trace-dir <desktop-trace-dir> --gateway-trace-dir <gateway-capture-dir>
```

Expected report includes:

- `tool_search_call` followed by `tool_search_output`;
- spawn-agent model override freshness status;
- Computer Use normalized-output class summary;
- cache replay diagnostics for resumed DeepSeek requests;
- subagent registration ordering summary.

## Review Loop

Review this plan before implementation in two or three passes:

1. Root-cause closure: every fix must map to a confirmed evidence item or an explicitly marked unproven hypothesis.
2. Native parity: every DeepSeek bridge must target a captured Codex-native Responses item family, not an invented substitute.
3. Cache protection: no task may reorder stable prefixes, change tool schema ordering, or discard replay reasoning without a failing test.
4. Observability: every runtime-only hypothesis must gain a shape-only trace or doctor check before deeper code changes.

Stop and ask for direction if review finds that the stale subagent model list can only be fixed inside closed-source/minified Codex app-server code. The acceptable fallback is a precise doctor warning plus restart/refresh workflow, not a fragile bundle patch.

## Inline Author Review Completed

Completed on 2026-06-03 before implementation:

1. Mechanical pass: verified referenced backend, zhumeng-agent, desktop model catalog, and smoke-doc files exist in the remediation worktree.
2. Scope pass: removed `backend/internal/service/codex_gateway_model_registry_test.go` from optional write scope because it is already dirty and explicitly protected in this worktree.
3. Protocol pass: corrected `tool_search_call` replay guidance so native items without a `name` field use a wrapper or dedicated converter that injects canonical `tool_search`.
4. Overlap pass: confirmed the Computer Use and cache sections extend the existing remediation branch instead of replanning full replay messages, reasoning replay, stable tool schema ordering, or the existing vision proxy from scratch.
5. Subagent reviewer pass: dispatched reviewer `Darwin` to check native-parity coverage against Computer Use, Skills, Sub-Agent, protocol, cache/resume, and plan quality goals. Merged its blocking feedback by adding sanitized `spawn_agent` tools fixture assertions, implicit Skill trigger smoke, `tool_search_call` done-event assertions, subagent race remediation/fallback path, and shared-helper regression checks.
6. Subagent reviewer re-review: `Darwin` approved the updated plan with no blocking issues. Merged its advisory recommendations by using a nested `multi_agent_v1` namespace fixture and requiring implicit Skill smoke notes to record the exact `SKILL.md` path opened.
