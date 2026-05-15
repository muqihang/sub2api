## Codex Gateway

`/codex/v1` is the dedicated Codex Responses gateway. It is separate from the existing `/v1/*` gateway surface and is intended for Codex Desktop/CLI custom provider wiring.

### Runtime requirements

- `gateway.codex.enabled: true`
- `gateway.codex.provider_groups.openai` must point at a healthy OpenAI-capable group for native Responses models
- `gateway.codex.provider_groups.deepseek` must point at a healthy DeepSeek-capable group before DeepSeek models become visible
- `gateway.codex.provider_groups.anthropic` must point at a healthy Anthropic Messages-compatible group before Claude or other Anthropic-compatible models become visible
- DeepSeek visibility is gated by:
  - model enabled
  - provider group configured and healthy
  - explicit pricing present in the embedded pricing catalog
  - protocol fixture gate enabled in the registry
  - admin model state not explicitly hidden

### Config sample

```yaml
gateway:
  codex:
    enabled: true
    expose_v1_alias: false
    supports_websockets: false
    model_catalog_path: "/absolute/path/to/sub2api-codex-models.json"
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
      - claude-opus-4-6
      - claude-opus-4-6-thinking
      - claude-sonnet-4-6
    provider_groups:
      openai: 1001
      deepseek: 2002
      anthropic: 3003
    capture:
      enabled: true
      level: summary
      raw_payloads: false
      base_dir: data/codex-gateway-captures
      retention_days: 7
      capture_success_sample_rate: 1.0
      capture_errors_always: true
      include_response_header: true
```

### Protocol capture

Codex Gateway includes a local protocol capture system for debugging provider compatibility without defaulting to user-content capture.

Default `summary` capture records:

- request method/path, model, provider, selected upstream model, trace id, timing, and sanitized status
- top-level Responses request field shape
- tool names, tool schema fields, required parameters, and protocol item types
- prompt/tool-output lengths and keyed hashes, not raw prompt or output text
- upstream request/response shape for OpenAI Responses, DeepSeek Chat Completions, and Anthropic Messages conversions
- client SSE event names, ordering, payload shape, size, timing, and terminal signals
- `tool_closure.json`, `cache_usage.json`, and `errors.jsonl`

Trace layout:

```text
data/codex-gateway-captures/YYYY-MM-DD/<trace_id>/
  summary.json
  client_request.shape.json
  client_request.headers.json
  upstream_request.shape.json
  upstream_request.headers.json
  upstream_response.headers.json
  upstream_response.shape.json
  client_stream.events.jsonl
  upstream_stream.events.jsonl
  tool_closure.json
  cache_usage.json
  errors.jsonl
```

`raw_payloads` is a local debugging escape hatch. It is rejected in `server.mode=production` and requires both config and the exact unlock environment variable:

```bash
export SUB2API_CODEX_CAPTURE_RAW_UNLOCK=I_UNDERSTAND_THIS_WRITES_LOCAL_RAW_PROTOCOL_PAYLOADS
```

Even in raw mode, known credential fields and bearer tokens are redacted. Raw mode is not intended for shared deployments.

### Codex provider sample

```toml
model_provider = "sub2api-codex"
model = "gpt-5.5"
model_catalog_json = "/absolute/path/to/sub2api-codex-models.json"

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

### Admin surface

- `GET /api/v1/admin/codex-gateway/summary`
- `GET/PUT /api/v1/admin/codex-gateway/provider-groups`
- `GET /api/v1/admin/codex-gateway/models`
- `PUT /api/v1/admin/codex-gateway/models/:id`
- `POST /api/v1/admin/codex-gateway/smoke`
- `GET /api/v1/admin/codex-gateway/state-store/summary`

The current admin implementation is intentionally MVP-scoped:

- provider-group and model mutations update the in-process Codex admin state
- the live model registry reads that admin state through the registry state source
- smoke execution is an accepted stub; it records intent but does not call upstreams yet

### Catalog example

`docs/codex-gateway/sub2api-codex-models.example.json` is a checked-in `model_catalog_json` sample for the Codex CLI/Desktop local model catalog loader. This is intentionally different from the remote `/codex/v1/models` provider response:

- local `model_catalog_json` uses Codex CLI-native values such as `visibility: "list"` and object-shaped `supported_reasoning_levels`
- remote `/codex/v1/models` keeps the gateway provider envelope used by sub2api admin/runtime code
- GPT entries keep hosted OpenAI Responses capabilities such as `web_search_tool_type`
- DeepSeek entries declare only capabilities that can be executed through Codex client-side tools or MCP/function/custom/namespace calls; OpenAI hosted tools such as `web_search` and `image_generation` are not advertised for DeepSeek because the DeepSeek Chat Completions upstream cannot execute them server-side
- Anthropic entries use the same Codex client-side tool ecosystem as DeepSeek. The gateway converts Responses messages, function/custom/namespace tools, tool results, image blocks, prompt-cache markers, and tool-loop state into Anthropic Messages-compatible requests.

Codex Desktop app-server v2 remains Codex's local control plane for `thread/*`, `turn/*`, `plugin/*`, `app/list`, `mcpServer/*`, and `fs/*` methods. The gateway is only the custom Responses model provider behind `model_providers.<id>.base_url`; it should not implement app-server v2 itself.

### Integration closure notes

The integration target is considered ready for merge preparation after focused Codex Gateway regression, local Desktop testing, and upstream error hardening.

Protocol observability documents:

- `docs/codex-gateway/protocol-capture-design.md` defines the safe protocol-capture architecture for improving Codex Gateway without defaulting to user-content capture.
- `docs/codex-gateway/protocol-capture-implementation-plan.md` breaks that design into implementation checkpoints.

Verified provider groups:

- OpenAI Responses-compatible GPT models
- DeepSeek V4 Pro and Flash through Chat Completions-compatible conversion
- Anthropic Messages-compatible Claude models, including ordinary and thinking variants

Verified Codex Desktop scenarios:

- model picker visibility for GPT, DeepSeek, and Claude entries
- GPT controller turns and subagent dispatch
- DeepSeek V4 Pro and Flash tool calls
- Computer Use and browser plugin tool forwarding through Codex client-side tools
- context compaction after gateway-backed turns
- Claude ordinary and thinking model turns
- Anthropic thinking signature preservation for tool-result replay
- Cloudflare HTML upstream errors mapped to clean `upstream_timeout` errors

Anthropic robustness rules:

- Thinking is preserved by default, including large tool-result replay requests.
- Forced Anthropic `tool_choice` disables thinking only for that request because Anthropic extended thinking is incompatible with forced tool choice.
- Streaming upstream errors before any client-visible output may trigger account failover.
- Streaming errors after visible output are not transparently replayed, preserving Responses event ordering.
- Cloudflare `520`, `522`, and `524` upstream errors are sanitized and never forwarded as raw HTML.

Known upstream limitation:

- Cloudflare-fronted Anthropic-compatible relays can still return `524` when their origin server cannot produce timely streaming output for heavy Claude Thinking requests. This is an upstream capacity or relay behavior issue, not a gateway protocol failure. The gateway isolates it through sanitized errors and account failover before visible output.

Merge preparation checklist:

- Exclude local Codex Desktop patch artifacts unless they are intentionally part of the merge.
- Keep local `~/.codex` sessions and credentials out of the repository.
- Run the focused verification command from `smoke.md`.
- Run a wider suite only after unrelated existing Gemini service test failures are resolved.
