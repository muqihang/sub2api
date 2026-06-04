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
2. Confirm `/codex/v1/models` shows GPT entries and, once provider-group gates are satisfied, DeepSeek V4 Pro/Flash and Claude entries.
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
13. Use `gpt-5.4` as controller and dispatch DeepSeek and Claude subagents; verify the controller can summarize their results.

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
- `previous_response_id` on the DeepSeek HTTP path must still return a 400 unless the request is replayed through gateway-managed state.
- DeepSeek models should disappear from `/codex/v1/models` when their provider group is unset or unhealthy.
- Anthropic forced `tool_choice` should disable thinking only for that request.
- Anthropic-compatible upstream `520`, `522`, or `524` HTML errors should be returned as clean `upstream_timeout` errors, not raw HTML.
- Anthropic stream errors before visible output should be eligible for account failover; errors after visible output must not be transparently replayed.
