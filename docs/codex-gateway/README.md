## Codex Gateway

`/codex/v1` is the dedicated Codex Responses gateway. It is separate from the existing `/v1/*` gateway surface and is intended for Codex Desktop/CLI custom provider wiring.

### Runtime requirements

- `gateway.codex.enabled: true`
- `gateway.codex.provider_groups.openai` must point at a healthy OpenAI-capable group for native Responses models
- `gateway.codex.provider_groups.deepseek` must point at a healthy DeepSeek-capable group before DeepSeek models become visible
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
    provider_groups:
      openai: 1001
      deepseek: 2002
```

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

`docs/codex-gateway/sub2api-codex-models.example.json` is a checked-in sample catalog representing a fully configured Codex gateway where the DeepSeek visibility gates are satisfied.
