## Codex Gateway Smoke

### Basic HTTP checks

```bash
curl -sS http://127.0.0.1:3000/codex/v1/models \
  -H "Authorization: Bearer $SUB2API_CODEX_API_KEY" | jq
```

```bash
curl -sS http://127.0.0.1:3000/codex/v1/responses \
  -H "Authorization: Bearer $SUB2API_CODEX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-5.5","input":"reply with ok","stream":false}' | jq
```

```bash
curl -N http://127.0.0.1:3000/codex/v1/responses \
  -H "Authorization: Bearer $SUB2API_CODEX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"deepseek-v4-pro","input":"explain the current directory","stream":true}'
```

```bash
curl -N http://127.0.0.1:3000/codex/v1/responses \
  -H "Authorization: Bearer $SUB2API_CODEX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"agnes-2.0-flash","input":[{"role":"user","content":[{"type":"input_text","text":"describe this tiny image in one sentence"},{"type":"input_image","image_url":"data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII="}]}],"reasoning":{"effort":"high"},"stream":true}'
```

```bash
curl -N http://127.0.0.1:3000/codex/v1/responses \
  -H "Authorization: Bearer $SUB2API_CODEX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-sonnet-4-6","input":"reply with ok","stream":true}'
```

```bash
curl -N http://127.0.0.1:3000/codex/v1/responses \
  -H "Authorization: Bearer $SUB2API_CODEX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-opus-4-6-thinking","input":"reply with ok after brief reasoning","reasoning":{"effort":"high"},"stream":true}'
```

### Admin checks

```bash
curl -sS http://127.0.0.1:3000/api/v1/admin/codex-gateway/summary \
  -H "Authorization: Bearer $SUB2API_ADMIN_KEY" | jq
```

```bash
curl -sS http://127.0.0.1:3000/api/v1/admin/codex-gateway/state-store/summary \
  -H "Authorization: Bearer $SUB2API_ADMIN_KEY" | jq
```

### Codex client smoke

1. Start Codex Desktop/CLI with `wire_api = "responses"` and `base_url = ".../codex/v1"`.
2. Confirm `/codex/v1/models` shows GPT entries and, once provider-group gates are satisfied, DeepSeek V4 Pro/Flash, Claude, and AGNES entries.
3. Run a plain chat turn on `gpt-5.5`.
4. Switch to `deepseek-v4-pro` and verify a streamed text reply.
5. Run a shell tool turn.
6. Run a file edit / apply-patch style tool turn.
7. If MCP is configured, run one resource read and one tool call.
8. If Desktop exposes app tools, run one plugin/app tool.
9. If Computer Use / Chrome plugin tools are exposed locally, verify the tool list is forwarded and a simple tool call completes.
10. Switch to `claude-sonnet-4-6` and verify a streamed text reply.
11. Switch to `claude-opus-4-6-thinking`, select a non-none reasoning effort, and run a small tool-read turn.
12. Continue the same Claude Thinking conversation with a follow-up tool-result turn and verify the stream completes.
13. Switch to `agnes-2.0-flash`, select high reasoning, and verify a streamed text reply plus one image-input turn.
14. Use `gpt-5.4` as controller and dispatch DeepSeek, Claude, and AGNES subagents; verify the controller can summarize their results.

### Checkpoint 4 WS gate and live capture matrix readiness

This section is a readiness checklist, not a claimed live pass. Mark a row as
`skipped` with an explicit reason when Codex Desktop/app-server is not running,
the current backend is not wired into Desktop, capture is disabled, or the
provider key/account is unavailable. Do not mark such rows as passed.

Preconditions:

1. Codex Desktop or Codex app-server is running against this backend.
2. `/codex/v1/models` and `/codex/v1/responses` use the current worktree build.
3. Gateway capture is enabled in summary/shape-only mode.
4. Provider credentials exist for every provider being tested.
5. `zhumeng-agent codex capture report` can read both Desktop and Gateway traces.

#### WS readiness gate

Codex Gateway is HTTP Responses only until full Responses WebSocket v2 support is
implemented and explicitly enabled. OpenAI server-store continuation semantics
must not leak into `/codex/v1/responses`.

```bash
curl -sS 'http://127.0.0.1:3000/codex/v1/models?catalog_format=codex_cli' \
  -H "Authorization: Bearer $SUB2API_CODEX_API_KEY" \
  | jq '[.. | objects | select(has("supports_websockets"))]'
```

Expected: an empty list, or only explicit `supports_websockets:false` entries
after a future fully gated WS implementation.

```bash
curl -i --http1.1 http://127.0.0.1:3000/codex/v1/responses \
  -H 'Connection: Upgrade' \
  -H 'Upgrade: websocket' \
  -H 'Sec-WebSocket-Version: 13' \
  -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ=='
```

Expected: a normal HTTP error envelope such as `405 method_not_allowed`, never
`101 Switching Protocols`.

```bash
curl -sS http://127.0.0.1:3000/codex/v1/responses \
  -H "Authorization: Bearer $SUB2API_CODEX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-5.5","input":"reply ok","stream":false}' | jq
```

Expected: HTTP works without `previous_response_id`.

```bash
curl -sS -i http://127.0.0.1:3000/codex/v1/responses \
  -H "Authorization: Bearer $SUB2API_CODEX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-5.5","previous_response_id":"resp_openai_prev","input":"continue"}'
```

Expected: OpenAI HTTP path rejects `previous_response_id` with a WS v2 message.
DeepSeek/Claude/AGNES may accept `previous_response_id` only as
Gateway-managed replay against local state; they must not depend on OpenAI
server-store semantics.

#### Live prompt matrix

Run only safe prompts for available providers:

| Row | Provider/model | Prompt focus | Pass evidence |
| --- | --- | --- | --- |
| C4-1 | any visible GPT/OpenAI model | Basic repo understanding with file read/search. | Terminal events and final `response.completed`. |
| C4-2 | `deepseek-v4-pro` | Deferred tool search and subagent spawn model list. | Native `tool_search_call` plus matching `tool_search_output`; no hosted web-search rewrite. |
| C4-3 | `deepseek-v4-pro` | Computer Use simple controllable app flow. | Visible text/operable lines retained; no raw screenshot/base64 in model text. |
| C4-4 | `deepseek-v4-pro` | Computer Use Electron/canvas visible_text flow. | `visible_text`, app/bundle id, latest error/truncation status, and `computer_use_compression_version`. |
| C4-5 | OpenAI/GPT or configured bridge | Web Search bridge. | Hosted search fields only on providers that support them; local/deferred tools otherwise. |
| C4-6 | image-capable provider | Image input / structured tool output. | Structured content preserved where supported; text-only providers summarize safely. |
| C4-7 | OpenAI WS-enabled environment only | Stop/Continue/Resume or WS/native continuation. | If WS unavailable, skip; if enabled, terminal events and WS v2 continuation diagnostics. |
| C4-8 | `deepseek-v4-pro` | DeepSeek tool-call reasoning replay multi-turn. | Tool-call reasoning replay succeeds; no Reasonix blanket stripping. |
| C4-9 | `deepseek-v4-pro` | DeepSeek cache prefix stability repeated prompt. | Stable prefix hashes and provider hit/miss usage when upstream returns it. |
| C4-10 | Claude direct | Anthropic thinking/tool loop. | Thinking/tool replay and terminal events complete. |
| C4-11 | `agnes-2.0-flash` | AGNES tool loop and interruption recovery. | AGNES-specific cache/key semantics, no DeepSeek official cache-control leak. |

Validation checklist for every non-skipped row:

- capture contains terminal events and a clear terminal status;
- tool calls and tool outputs pair with no missing/orphan/duplicate results;
- deferred tools are discovered and callable through native `tool_search` shape;
- unsupported cache controls are absent from provider requests;
- DeepSeek usage records `prompt_cache_hit_tokens` /
  `prompt_cache_miss_tokens` when upstream returns them;
- Computer Use output remains token-bounded and keeps `visible_text`,
  actionable `operable_lines`, app/bundle id, latest error/truncation status,
  and `computer_use_compression_version`;
- no DeepSeek, Claude, or AGNES regression is introduced by provider-specific
  prompt or compression changes.

### AGNES cache diagnostics smoke

AGNES cache evidence must be reported as provider-unsupported unless the
upstream starts returning explicit cache-hit fields. Do not treat `0` cache
tokens as a confirmed cold miss for AGNES.

1. Send two same-shape AGNES requests in the same Codex thread.
2. Verify the upstream request shape includes a scoped `prompt_cache_key`.
3. Verify the admin usage row shows AGNES as cache unsupported instead of only a
   silent zero cache token count.
4. Verify gateway capture marks the trace with
   `provider_prompt_cache_status:"unsupported"` and the diagnostic
   `provider_prompt_cache_unsupported`.

Expected:

- billing keeps `cache_read_tokens=0` unless the AGNES upstream returns a real
  provider cache metric;
- capture/reporting explains the unsupported metric, so 0 cache tokens are not
  misdiagnosed as a gateway cache-regression;
- repeated same-shape requests should preserve deterministic request shape,
  tool schema order, session key, and scoped `prompt_cache_key`.

### Protocol capture smoke

Enable capture in local config:

```yaml
gateway:
  codex:
    capture:
      enabled: true
      level: summary
      raw_payloads: false
      base_dir: data/codex-gateway-captures
      capture_success_sample_rate: 1.0
      capture_errors_always: true
```

After one GPT, one DeepSeek, and one Claude request, verify:

```bash
find data/codex-gateway-captures -maxdepth 3 -type f | sort
```

Expected capture files include:

- `summary.json`
- `client_request.shape.json`
- `client_request.headers.json`
- `upstream_request.shape.json`
- `upstream_response.shape.json`
- `client_stream.events.jsonl` for stream requests
- `tool_closure.json` when a turn emits or returns tool calls/results
- `cache_usage.json`
- `errors.jsonl` for failed requests

Content checks:

```bash
rg -n "private prompt|sk-|Bearer " data/codex-gateway-captures || true
rg -n "hmac-sha256|cache_read_input_tokens|response.output" data/codex-gateway-captures
```

The first command should not find raw user prompt text or credentials in summary-mode captures. The second command should show hashed content metadata, cache usage fields, and stream event names.

### DeepSeek native parity capture smoke

Run these with capture enabled in `summary` mode and `raw_payloads: false`.

#### Deferred tool search and subagent discovery

1. Select `deepseek-v4-pro` in Codex Desktop.
2. Ask the model to dispatch a subagent or discover the subagent tool.
3. Inspect the session JSONL and gateway stream capture.

Expected:

- the Codex session records `tool_search_call` followed by `tool_search_output`;
- `tool_search_output.tools` contains `multi_agent_v1.spawn_agent`;
- there is no user-visible ordinary `function_call` named `tool_search` for the deferred tool search;
- gateway request replay accepts the later `tool_search_output` as a `role:"tool"` Chat Completions message.

Use this explicit prompt when checking the deferred tool path:

```text
Using deepseek-v4-pro, search for the deferred subagent tool and spawn one no-op explorer.
```

Expected capture evidence:

- `tool_search_call`;
- `tool_search_output`;
- `multi_agent_v1.spawn_agent`;
- no ordinary `function_call name=tool_search` visible in the session.

Live smoke evidence from 2026-06-05:

- parent session:
  `/Users/muqihang/.codex/sessions/2026/06/05/rollout-2026-06-05T05-12-40-019e97b3-24bf-7f93-b3fc-9247bbf66daf.jsonl`;
- child session:
  `/Users/muqihang/.codex/sessions/2026/06/05/rollout-2026-06-05T05-12-53-019e97b3-565e-7ea2-accc-f828ed0e2c70.jsonl`;
- parent `turn_context.model` was `deepseek-v4-pro`;
- parent emitted `tool_search_call` for `spawn subagent multi agent`;
- `tool_search_output.tools` contained namespace `multi_agent_v1` and `spawn_agent`;
- parent emitted native namespace calls `multi_agent_v1.spawn_agent`, `multi_agent_v1.wait_agent`,
  and `multi_agent_v1.close_agent`;
- `spawn_agent` arguments omitted `model`, so the child inherited the parent model;
- child `turn_context.model` was `deepseek-v4-pro`;
- `wait_agent` returned `{"completed":"Explorer ready"}` and `close_agent` returned the same previous
  completed status.

Known app-server boundary from the same run: the deferred `spawn_agent` description still listed only
Claude model overrides and did not list DeepSeek overrides, but actual inherited-model spawn validation
accepted DeepSeek and the child ran on `deepseek-v4-pro`. Treat the missing override-list entry as a
picker/description freshness issue, not a native spawn-routing blocker. The exact app-server refresh
boundary for that override description remains unproven as of this live run; do not claim it is fixed by
catalog writes alone. A future follow-up must prove whether the boundary is a full Desktop restart, an
app-server process refresh, a model-list cache refresh, or another native Codex path by capturing the same
`spawn_agent` description before and after the refresh action.

If `tool_search_output.tools` does not list DeepSeek-capable `spawn_agent` model overrides while the local
`model_catalog_json` contains DeepSeek models, run:

```bash
zhumeng-agent codex capture report --trace-dir <desktop-trace-dir>
```

Expected diagnostic:

- `spawn_agent_model_override.spawn_agent_model_override_mismatch:true`;
- `spawn_agent_model_override.catalog_has_deepseek:true`;
- `spawn_agent_model_override.spawn_agent_has_deepseek:false`;
- `catalog_hash`, `catalog_mtime`, and capture timestamp are present.

#### Model catalog refresh boundary

Current boundary assumption for DeepSeek/Claude catalog changes: Codex app-server may keep model catalog
state in-process or behind an app-server cache. Treat catalog/config writes as requiring a Codex restart
unless a same-session `model/list` capture proves the refreshed catalog and `spawn_agent` description are
visible.

After changing `model_catalog_json` or `config.toml`:

1. Run `zhumeng-agent desktop diagnose --redacted --json`.
2. Inspect `doctor.model_catalog_freshness`.
3. Restart Codex Desktop when `restart_required:true` or `restart_required_reasons` is non-empty.
4. Re-run the deferred tool prompt above and confirm the next `tool_search_output.tools` includes DeepSeek,
   or record the exact `spawn_agent_model_override` mismatch from the capture report.

Expected doctor fields:

- `model_catalog_json`;
- `catalog_hash`;
- `catalog_mtime`;
- `catalog_has_deepseek`;
- `deepseek_models_present`;
- `active_default_model`;
- `restart_required`;
- `restart_required_reasons`;
- `app_server_refresh_boundary`.

#### Skills runtime parity

Doctor evidence should remain factual only. It may report configured marketplaces, enabled plugins, skills
directories, plugin cache skill paths, and whether DeepSeek catalog base instructions contain local routing
guidance. It must not claim a model can or cannot use a Skill from file presence alone.

Explicit skill-file prompt:

```text
Using deepseek-v4-pro, read the superpowers:systematic-debugging SKILL.md and summarize only the four phase names.
Do not use tool_search.
```

Expected:

- the model reads the local `SKILL.md` via shell/file access;
- no `tool_search` is needed for ordinary file-backed Skills;
- the response does not say Skills are unavailable.

Implicit Skill trigger prompt:

```text
Using deepseek-v4-pro, diagnose a reproducible failing test in this repository.
Follow the applicable local skill instructions before proposing a fix.
Do not implement code.
```

Expected:

- the model identifies that `superpowers:systematic-debugging` applies from injected skill instructions;
- the model opens the local `SKILL.md` before proposing diagnosis steps;
- smoke notes record the exact `SKILL.md` path opened;
- the model follows the skill's evidence-first phases;
- no claim is made that Skills are unavailable;
- no `tool_search` is needed for ordinary file-backed Skills.

Live smoke evidence from 2026-06-05:

- session:
  `/Users/muqihang/.codex/sessions/2026/06/05/rollout-2026-06-05T18-56-41-019e9aa5-8db3-79c3-b35b-d54b21e97481.jsonl`;
- `turn_context.model` was `deepseek-v4-flash`;
- the prompt asked DeepSeek to diagnose a reproducible test failure and follow the applicable local Skill
  before proposing a fix;
- DeepSeek opened `/Users/muqihang/.codex/superpowers/skills/systematic-debugging/SKILL.md`;
- it identified `superpowers:systematic-debugging` and summarized the four evidence-first phases;
- no `tool_search` was used for the ordinary file-backed Skill load.

#### Computer Use visibility

1. Select `deepseek-v4-pro`.
2. Run a Computer Use `get_app_state` turn against a local app window that includes visible lower-screen controls or a reply/input area.
3. If hosted vision is configured, ensure the tool output includes a large screenshot or `image_base64`.
4. Inspect `client_request.diagnostics.json` and the upstream request shape.

Expected:

- DeepSeek-visible tool content does not contain raw screenshot/base64;
- the normalized tool content retains `computer_screenshot` when hosted vision succeeds;
- the normalized tool content retains `accessibility_tree` or `visual_tree`;
- `operable_lines` includes at least one lower-screen input/reply/action line;
- summaries include `sha256` and `original_chars`;
- `deepseek_tool_output_summary.fallback_preview_only` is `false`;
- `deepseek_tool_output_summary.classes` includes `computer_screenshot` and/or `accessibility_tree`;
- `deepseek_tool_output_summary.operable_line_count` is non-zero.

Live smoke evidence from 2026-06-05:

- session:
  `/Users/muqihang/.codex/sessions/2026/06/05/rollout-2026-06-05T18-56-41-019e9aa5-8db3-79c3-b35b-d54b21e97481.jsonl`;
- `list_apps` and `get_app_state` completed against local macOS apps, including `com.apple.freeform`;
- the Computer Use MCP server was not in the previous 120s-timeout failure mode;
- gateway capture summary showed `deepseek_tool_output_summary.fallback_preview_only:false`;
- normalized output classes included `accessibility_tree` and binary/image metadata, with non-zero
  `operable_line_count`;
- Freeform canvas manipulation still required more model-side strategy than GPT-like use, but the gateway
  and tool-output preservation path did not drop the app-state content.

#### Abort/resume cache evidence

1. Keep capture in shape-only summary mode.
2. Start a DeepSeek thread and execute one tool turn.
3. Interrupt or abort the next turn.
4. Resume the same thread.
5. Record the Codex session id, gateway trace id, token usage, and `prompt_cache_*` fields from the session/capture.

Expected:

- a gateway capture exists for the resumed DeepSeek request;
- `client_request.diagnostics.json` contains `deepseek_cache.previous_response_id_present:true`;
- `deepseek_cache.previous_response_replay_mode` is `full_replay_messages` when gateway state is available;
- `deepseek_cache.state_lookup_status` identifies `hit`, `miss`, or the exact invalid-state reason;
- `messages_full_hash`, `message_prefix_hash`, `message_suffix_hash`, `tool_schema_hash`, and `request_shape_hash` are present;
- `cache_usage.json` can be correlated to session token usage through hashed trace/session fields;
- any post-warmup `0 cached` turn has a cache attribution reason such as `request_not_warmed`, `message_prefix_changed`, `tool_schema_changed`, `request_shape_changed`, or `upstream_best_effort_or_unknown`.

Live interruption/continue evidence from 2026-06-05:

- session:
  `/Users/muqihang/.codex/sessions/2026/06/05/rollout-2026-06-05T19-30-00-019e9ac4-0e3d-7bc3-9044-863990c2a2cf.jsonl`;
- `turn_context.model` was `deepseek-v4-flash`;
- a long read-only tool task started at line 59 and emitted tool calls at lines 65, 69, and 70;
- Codex recorded `turn_aborted` at line 75 with `reason:"interrupted"` and `duration_ms:12122`;
- the same thread resumed from the user's `继续` message at line 79, read additional package files, and
  completed at line 119 with a final architecture summary;
- before the interrupt, session token counts were:
  - line 67: `input_tokens:20375`, `cached_input_tokens:19200`, cache ratio about 94.23%;
  - line 73: `input_tokens:20664`, `cached_input_tokens:20352`, cache ratio about 98.49%;
- after the user-driven continue, session token counts were:
  - line 90: `input_tokens:23578`, `cached_input_tokens:19328`, cache ratio about 81.97%;
  - line 106: `input_tokens:25945`, `cached_input_tokens:23424`, cache ratio about 90.28%;
  - line 110: `input_tokens:29095`, `cached_input_tokens:25856`, cache ratio about 88.87%;
  - line 114: `input_tokens:29295`, `cached_input_tokens:29056`, cache ratio about 99.18%;
  - line 118: `input_tokens:30009`, `cached_input_tokens:29184`, cache ratio about 97.25%;
- matching gateway traces included:
  - `trace_1780713241539984345` and `trace_1780713246557384458` before the interrupt;
  - `trace_1780713258913134964`, `trace_1780713263374228841`,
    `trace_1780713266791420676`, `trace_1780713269448016594`, and
    `trace_1780713273212555930` after the user-driven continue;
- all checked post-continue `tool_closure.json` files had empty `missing_results`, `orphan_results`, and
  `duplicate_results`;
- every checked post-continue cache miss below 99% had capture attribution including
  `context_compaction_changed_prefix`, `request_not_warmed`, and `request_shape_changed`.

Boundary note: this live run proves an interrupted DeepSeek turn followed by a user `继续` continuation did
not lose tool results and recovered high cache hit rates with capture-backed attribution. It does not prove
gateway-managed state replay through a native `previous_response_id`: the checked DeepSeek traces recorded
`previous_response_id_present:false`, `previous_response_replay_mode:"none"`, and
`state_lookup_status:"not_requested"`. If a future Codex Desktop "native Continue/Resume" control emits
`previous_response_id`, run the stricter repro above and require `previous_response_id_present:true` plus
`full_replay_messages`.

Native Stop/Continue control boundary from 2026-06-05:

- session:
  `/Users/muqihang/.codex/sessions/2026/06/05/rollout-2026-06-05T20-00-34-019e9ae0-0ad8-7e52-b28f-be83b74ecb7c.jsonl`;
- the warmup turn completed on `deepseek-v4-flash`;
- the long read-only task emitted tool calls and tool results through line 49, then the user clicked the
  Codex Desktop Stop control;
- Codex recorded `turn_aborted` at line 52 with `reason:"interrupted"`;
- the UI did not show a native Continue/Resume button, and the user did not type `继续`, so no native
  resumed request was produced;
- matching DeepSeek gateway traces all recorded `previous_response_id_present:false`,
  `previous_response_replay_mode:"none"`, and `state_lookup_status:"not_requested"`;
- the final stopped trace `trace_1780714863392059971` ended as `response.incomplete`, had no visible output,
  had zero provider usage because the upstream stream did not provide final usage, and had empty
  `missing_results`, `orphan_results`, and `duplicate_results`.

Treat this as a Desktop UI boundary, not a DeepSeek gateway replay failure: this run did not provide a
native `previous_response_id` input for the gateway to replay. A stricter native-resume proof remains
conditional on finding a Codex Desktop path that actually emits `previous_response_id`.

#### Subagent registration ordering

Manual prompt:

```text
Using deepseek-v4-pro as controller, spawn one DeepSeek subagent that only says "ready" and then wait for it.
```

Expected:

- no `unknown conversation` before registration;
- if `unknown conversation` appears, it is followed by deterministic resume recovery and no lost tool/result events;
- `subagent_registration.jsonl` contains only event names, timestamps, hashed conversation/thread ids, and safe status classes;
- capture report includes ordered evidence:
  - `subagent_registration_events`;
  - `subagent_registration_race_suspected`;
  - `first_item_before_conversation_registered`;
  - `unknown_conversation_count`;
  - `thread_read_empty_count`;
  - `maybe_resume_success_after_unknown_conversation`.

If the report confirms a race outside zhumeng-agent controlled code, do not patch minified Codex bundles as
the primary fix. Record the app-server boundary and use the exact report condition to decide restart/retry
fallback. If a race is confirmed in zhumeng-agent controlled code, fix ordering or add bounded
retry/rehydration around the failing read/resume boundary and add a regression test from the captured event
order.

#### Capture report matrix

Link desktop and gateway captures after a full DeepSeek run:

```bash
zhumeng-agent codex capture report --trace-dir <desktop-trace-dir> --gateway-trace-dir <gateway-capture-dir>
```

Expected report includes:

- `tool_search_call` followed by `tool_search_output` in session/capture evidence;
- spawn-agent model override freshness status;
- Computer Use normalized-output class summary;
- cache replay diagnostics for resumed DeepSeek requests;
- subagent registration ordering summary.

### Regression prompts

Use these prompts when validating Codex Desktop end-to-end.

#### Claude Thinking tool replay

```text
Please inspect the Codex Gateway Anthropic adapter without editing files.

1. Read codex_gateway_anthropic_request.go and codex_gateway_anthropic_stream.go.
2. Identify where thinking is preserved, where forced tool choice disables thinking, and where upstream HTML errors are sanitized.
3. Return no more than five bullet points.
```

Then continue in the same thread:

```text
Continue from the files you already inspected.

1. Read codex_gateway_anthropic_stream_test.go only.
2. List the tests that protect thinking signature replay and Cloudflare 524 handling.
3. Explain what each test protects in one sentence.
```

#### Mixed controller and subagents

```text
Dispatch two background subagents without editing files:

1. Use deepseek-v4-pro to inspect the project structure and summarize the Codex Gateway modules.
2. Use claude-sonnet-4-6 to inspect the Anthropic adapter tests and summarize the risk controls.

As controller, return only whether either subagent found a blocking issue and three observations I should check in the UI.
```

### Expected failure checks

- Generic or Augment-only API keys must be rejected on `/codex/v1/*`.
- OpenAI HTTP `/codex/v1/responses` with `previous_response_id` must return a 400 WS v2 error.
- DeepSeek/Claude/AGNES HTTP `previous_response_id` is allowed only as Gateway-managed replay through local state; missing or invalid local state must fail closed with replay diagnostics, not fall back to OpenAI server-store semantics.
- DeepSeek models should disappear from `/codex/v1/models` when their provider group is unset or unhealthy.
- Anthropic forced `tool_choice` should disable thinking only for that request.
- Anthropic-compatible upstream `520`, `522`, or `524` HTML errors should be returned as clean `upstream_timeout` errors, not raw HTML.
- Anthropic stream errors before visible output should be eligible for account failover; errors after visible output must not be transparently replayed.

#### Deferred tools family matrix smoke

Purpose: prove DeepSeek handles Codex deferred tools as a generic `tool_search_call` / `tool_search_output` protocol family, not as a one-off `multi_agent_v1.spawn_agent` special case. This smoke must classify each tool family as one of:

- `deferred`: discovered through `tool_search_output.tools` and callable on the next turn;
- `direct`: already exposed as a normal tool, so `tool_search` is not required;
- `skill_only`: loaded from local `SKILL.md` or injected skill routing instructions, not via `tool_search`;
- `unavailable`: not exposed in this Desktop/plugin environment.

Do not treat `direct`, `skill_only`, or `unavailable` as failures unless Codex returns the tool in `tool_search_output.tools` and DeepSeek cannot call it on the next turn.

##### Prompt A: discover-only matrix

Run with `deepseek-v4-pro` selected:

```text
请做一次 deferred tools matrix 发现测试。
请分别搜索这些关键词：spawn_agent、computer use、browser、chrome、document、spreadsheet、presentation。
只报告 tool_search 返回的 namespace 和 tool name；不要执行会修改文件、打开外部网站、操作真实 App、发送消息或派遣子代理的动作。
如果某一类工具不是通过 tool_search 暴露，而是直接可见或 Skill-only，请明确分类为 direct 或 skill_only。
```

Expected session/capture evidence:

- one or more native `tool_search_call` items;
- matching `tool_search_output` items after the calls;
- no visible ordinary `function_call` item named `tool_search`;
- `tool_search_output.tools` includes any deferred namespaces actually available in this Desktop environment;
- the final answer classifies each requested family without inventing unsupported call names.

After the run, generate a capture report:

```bash
zhumeng-agent codex capture report --trace-dir <desktop-trace-dir> --gateway-trace-dir <gateway-capture-dir>
```

Expected `deferred_tool_search` fields when deferred tools are present:

```json
{
  "tool_search_call_count": 1,
  "tool_search_output_count": 1,
  "tool_search_call_followed_by_output": true,
  "discovered_namespaces": ["..."],
  "discovered_tools": ["namespace.tool"],
  "tool_family_matrix": {
    "namespace": {"tool_count": 1, "tools": ["tool"]}
  }
}
```

The exact namespaces depend on enabled Codex plugins. The important invariant is that safe namespace/tool names are preserved in the shape-only report while descriptions, prompts, call IDs, and sensitive values are not serialized.

##### Prompt B: deferred SubAgent execution

Run with `deepseek-v4-pro` selected:

```text
请搜索 deferred subagent 工具，然后派遣一个 no-op 子代理。
子代理只需要回复一句：DeepSeek deferred subagent matrix smoke passed。
优先选择 DeepSeek 模型；如果 spawn_agent 的模型 override 列表没有 DeepSeek，请不要失败，请报告模型列表，并在没有显式 model 参数时继承当前 DeepSeek 模型。
```

Expected:

- `tool_search_call` discovers `multi_agent_v1.spawn_agent`;
- parent emits native namespace calls such as `multi_agent_v1.spawn_agent`, `multi_agent_v1.wait_agent`, and `multi_agent_v1.close_agent`;
- child session uses DeepSeek if inherited or explicitly selected;
- if model overrides omit DeepSeek, `spawn_agent_model_override` report records the freshness mismatch instead of masking it.

##### Prompt C: Computer Use classification

Run with `deepseek-v4-pro` selected:

```text
请判断 Computer Use 工具在当前 Codex Desktop 里是 direct 还是 deferred。
如果它是 direct，请只调用 list_apps；如果必须通过 tool_search，请先搜索再只调用 list_apps。
不要打开、点击或操作任何 App。最后报告你实际使用的是 direct、deferred，还是 unavailable。
```

Expected:

- if direct: a normal `list_apps` tool call succeeds or times out with a factual tool availability error;
- if deferred: `tool_search_output.tools` lists the Computer Use namespace/tool and the next turn calls it through the mapped alias;
- no blind clicking, scrolling, text input, or app mutation occurs.

##### Prompt D: Browser/Chrome classification

Run with `deepseek-v4-pro` selected:

```text
请判断 Browser 和 Chrome 工具在当前 Codex Desktop 里是 direct、deferred、还是 unavailable。
不要打开外部网站；如果需要做最小验证，只能使用 about:blank 或 localhost。
最后报告每类工具的来源和是否真实调用。
```

Expected:

- direct/deferred/unavailable classification is explicit;
- no remote URL or authenticated page is opened;
- if deferred, the report shows Browser/Chrome namespace/tool entries in `tool_family_matrix`.

##### Prompt E: Skill negative control

Run with `deepseek-v4-pro` selected:

```text
请加载 Computer Use Skill 的说明，摘要说明其中关于 Electron/画布类 App 的操作策略。
不要使用 tool_search 搜索 Skill，不要操作任何 App。
```

Expected:

- the model reads or follows local Skill instructions;
- no `tool_search` is needed for ordinary file-backed Skills;
- the model does not claim Skills are unavailable just because they are not in `tool_search_output.tools`.

##### Cache interpretation for the matrix

For any family classified as `deferred`, run a two-turn warmup:

1. discover the tool with `tool_search`;
2. repeat a same-shape safe action using the discovered tool.

Expected cache behavior:

- the discovery turn may have lower cache because `tool_search_output.tools` changes the next-turn prompt shape;
- after warmup, repeated same-shape turns should keep stable tool schema ordering and should not produce unexplained `0 cached` runs;
- if `0 cached` appears, the capture report must show whether the request had `previous_response_id_present`, stable replay diagnostics, and cache attribution fields.

## Codex Desktop Full-Capture Coverage Smoke

These smoke entries are keyed by matrix row id from `docs/codex-gateway/codex-desktop-full-capture-v3.md`. Do not mark a row `shipped` just because a file exists. A shipped row needs: action-specific dynamic evidence, static denominator comparison where applicable, redaction-negative checks, and high-confidence trace joins where the row crosses Desktop and Gateway.

Pre-launch:

```bash
# install renderer addBinding bridge
zhumeng-agent codex capture install --app /Applications/Codex.app
# launch Desktop with CDP open
open -a /Applications/Codex.app --args --remote-debugging-port=9222
# attach the bridge in the background
zhumeng-agent codex capture attach --cdp-port 9222 --trace-dir /tmp/codex-desktop-capture-smoke
```

### Required exercises

One full text turn is not enough. Run only exercises that are safe for the local account and environment. Record skipped exercises as `gap` with a reason.

1. Fresh login / refresh / logout where safe (C3, C18, C21, C23).
2. New thread, resume existing thread, cancel/abort turn, permission denied path, malformed/error path if available (C1, C14, C17).
3. Menu/context-menu/worktree command/open frontmost window/browser sidebar path (C10, C24).
4. MCP OAuth or MCP startup, plugin marketplace/cache, skill trigger, subagent spawn/wait/close (C20).
5. Computer Use, browser-use, node_repl each in a tiny safe workflow (C7-C9).
6. External-agent detect/import in a disposable fixture if available (C22).
7. Realtime session with mic permission accepted and denied paths if available (C16).
8. Binary/static baseline pass against the current app (C6, C11, C12, C19).

### Per-row evidence checks

- C1 / C14 / C17: `app_server_v2.jsonl` must include method/notification shape, request ids where present, error schema, cancel/abort, reconnect or explicit not-observed marker, and event ordering. Required events are action-specific; `model/rerouted` is optional unless the smoke action forces reroute.
- C2 / C23: `network.events.jsonl` must include path-level shape entries for `https://chatgpt.com/backend-api`, Codex Desktop auth/profile/MFA paths under `api.openai.com` / `auth.openai.com`, connector/entitlement/model/device/feature endpoints when exercised, and telemetry/updater hosts when exercised. Body shape/hash must exist in shape-only mode; raw bodies require `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_NETWORK`.
- C3 / C21: `oauth.state.jsonl` must include redacted OAuth/PKCE/MFA or explicit skipped marker; JWT bodies show claim key sets only. Remote-control pairing/revoke must be captured when exercised.
- C4 / C5 / C25: `telemetry.shape.jsonl` must record DSN/endpoint, transport mode, event/span/log/crash metadata shape. Raw messages and crash dumps must be absent unless `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_TELEMETRY` is set.
- C6 / C11 / C12 / C19: baseline output must validate schema, hashes, version pin, codesign parse, entitlements parse, asar listing completeness, static denominator extraction, updater appcast metadata, and signature validation status. Raw deobfuscated source must not be committed.
- C7 / C13: `pipe.cua.jsonl` must include process spawn argv/cwd/env shape, binary hash, frame methods, trust decisions, stderr/stdout class, crash/restart or explicit not-observed marker, and permission denial when exercised. Raw screenshot bytes must be absent unless `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_CUA` is set.
- C8: `pipe.browser.jsonl` must include peer auth, CDP relay, navigation/download lifecycle, crash/restart or explicit not-observed marker. Raw page bodies require `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_BROWSER`.
- C9: `pipe.node_repl.jsonl` must include invocation lifecycle, allowlist evaluation, request meta shape, stdout/stderr class, crash/restart or explicit not-observed marker. Raw code must be absent unless `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_NODE_REPL` is set.
- C10 / C24: `ipc.events.jsonl` must include static denominator comparison, runtime samples for `codex_desktop:*` channels exercised by the UI, explicit method ids exercised by the smoke, and UI-to-app-server chain links. Preload-patch-only evidence can make the row `partial`, not `shipped`.
- C15: dev URL evidence only passes when the smoke explicitly launches a dev build or feature override. Otherwise mark C15 skipped/gap; do not pass on “no dev surface hit”.
- C16: `realtime.events.jsonl` must record PeerConnection lifecycle, SDP, transcript ordering, codec/chunk class, mic permission outcome, interrupt/barge-in/VAD where exercised. Raw audio requires `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_REALTIME`.
- C18: `local_state.schema.json` must include filesystem schema and credential item names only. Secret values, token values, local absolute paths, repo URLs, branch names, and raw session content must be absent.
- C20: plugin/MCP/skills/subagent evidence must include plugin manifest/cache/marketplace shape, MCP transport/OAuth/startup shape, skill trigger/loader/cache, `multi_agent_v1.*` discovery/spawn/wait/close lifecycle, and deferred tool output families when exercised.
- C22: external-agent import evidence must include detect/import schema, source discovery, completion notification, generated-file staging, failure/retry or explicit not-observed marker.

### Redaction-negative checks

Run these after the report is generated. They must return no matches unless the exact row raw unlock is set and the session TTL/retention policy explicitly permits it.

```bash
TRACE=/tmp/codex-desktop-capture-smoke
rg -n --hidden --no-ignore -S 'Authorization:|Bearer |refresh_token|access_token|device_token|Cookie:|Set-Cookie:' "$TRACE"
rg -n --hidden --no-ignore -S '/Users/|git@github.com|https://github.com/.+/.+|branch:|commit [0-9a-f]{7,40}' "$TRACE"
rg -n --hidden --no-ignore -S 'data:image/|iVBORw0KGgo|RIFF|WEBM|BEGIN PRIVATE KEY|BEGIN OPENSSH PRIVATE KEY' "$TRACE"
```

Expected: no output. If output appears, keep the affected matrix row at `gap` and fix redaction before proceeding.

### Report and join checks

```bash
zhumeng-agent codex capture report \
  --trace-dir /tmp/codex-desktop-capture-smoke \
  --gateway-trace-dir data/codex-gateway-captures/$(date +%Y-%m-%d)
# planned subcommand; implement before relying on it for shipped status
zhumeng-agent codex capture matrix --trace-dir /tmp/codex-desktop-capture-smoke
```

Expected:

- every shipped row appears in the report with `coverage_denominator_count`, `seen_count`, `unseen_required[]`, and `sampled_optional[]`;
- every gap row appears in the doctor and matrix output with a documented next action;
- rows that cross Desktop and Gateway have at least one high-confidence shared-HMAC join in `trace_link.jsonl`; timestamp-only `low_confidence` links do not satisfy shipped status;
- `unmatched_expected` rows are emitted for expected gateway peers that were not found;
- replay/golden fixtures exist for rows where replay is meaningful, or an explicit not-replayable rationale exists.
