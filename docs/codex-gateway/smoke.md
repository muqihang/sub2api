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
