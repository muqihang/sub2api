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
2. Confirm `/codex/v1/models` shows GPT entries and, once provider-group gates are satisfied, DeepSeek V4 Pro/Flash.
3. Run a plain chat turn on `gpt-5.5`.
4. Switch to `deepseek-v4-pro` and verify a streamed text reply.
5. Run a shell tool turn.
6. Run a file edit / apply-patch style tool turn.
7. If MCP is configured, run one resource read and one tool call.
8. If Desktop exposes app tools, run one plugin/app tool.
9. If Computer Use / Chrome plugin tools are exposed locally, verify the tool list is forwarded and a simple tool call completes.

### Expected failure checks

- Generic or Augment-only API keys must be rejected on `/codex/v1/*`.
- `previous_response_id` on the DeepSeek HTTP path must still return a 400 unless the request is replayed through gateway-managed state.
- DeepSeek models should disappear from `/codex/v1/models` when their provider group is unset or unhealthy.
