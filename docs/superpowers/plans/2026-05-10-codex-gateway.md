# Codex Gateway Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a separate Codex Gateway that lets Codex Desktop/CLI use native OpenAI Responses models and DeepSeek V4 Pro/Flash through a `/codex/v1` Responses-compatible provider.

**Architecture:** Build a new `codex_gateway` boundary parallel to `augment_gateway`: HTTP ingress and key scope checks in `handler/routes`, Codex model registry and state in `service`, provider-specific adapters for native Responses and DeepSeek Chat Completions, and shared usage/billing metadata through the existing usage log fields. Keep `/codex/v1/*` canonical for MVP; do not enable shared `/v1/*` aliases, WebSocket, or compact until separate contract tests exist.

**Tech Stack:** Go, Gin, Wire, Ent/PostgreSQL migrations, existing sub2api service/repository patterns, OpenAI Responses protocol, Codex model catalog JSON, DeepSeek OpenAI-compatible `/chat/completions`.

---

## Recalibration Notes

Current worktree:

- Path: `/Users/muqihang/chelingxi_workspace/sub2api/.worktrees/codex-gateway`
- Branch: `codex/codex-gateway`
- Base: `a9aa7853 test: align legacy usage log scan coverage`

Code facts from the current base:

- `GatewayConfig` already has `OpenAIWS`, `OpenAICore`, and `Augment`; add a new `Codex` field instead of extending Augment.
- `augment_gateway` exists under `backend/internal/service/augment_gateway_*`, `backend/internal/handler/admin/augment_gateway_handler.go`, and `backend/internal/server/routes/augment_gateway_admin.go`. Use it as a pattern, not as shared protocol types.
- `/v1/*` is already occupied in `backend/internal/server/routes/gateway.go`, including `/v1/responses`, `/responses`, `/backend-api/codex/responses`, and `/openai/v1/*`. The MVP must register only `/codex/v1/*`.
- `APIKey.RestrictedClientProduct` already exists and `APIKey.IsAugmentOnly()` is hard-coded to `zhumeng_augment`. Add generic helper methods and a Codex product constant instead of copying more `augmentOnly` branching.
- `groups.augment_gateway_entitled` and `api_keys.restricted_client_product` already exist from migration `140_augment_dedicated_group_isolation.sql`. Add only `groups.codex_gateway_entitled`.
- `usage_logs` already has scoped metadata from migration `137_usage_logs_augment_scope.sql` and token/cost columns for cache read. For Codex Gateway, use `client_product`, `request_scope`, `feature_scope`, `pricing_version`, `billable`, `cost_source`, `currency`, `upstream_attempt_id`, unit prices, `cache_read_tokens`, and normal `input_tokens`; do not add a new usage table.
- Augment provider groups use numeric group IDs through `GatewayAugmentProviderGroupsConfig`; Codex provider groups must use the same concrete `int64` style, not string names.
- `UsageBillingCommand` already supports `InputTokens`, `OutputTokens`, `CacheCreationTokens`, and `CacheReadTokens`. DeepSeek cache misses should remain regular `InputTokens`; cache hits go to `CacheReadTokens`.
- `backend/internal/pkg/httputil/body.go` already decodes `zstd`, `gzip`, and `deflate`, but it uses `io.LimitReader` without detecting overflow. Tighten this before Codex uses it for compressed Codex requests.
- `augment_gateway_deepseek.go` already captures practical DeepSeek pitfalls: force thinking, default effort, remove `tool_choice`, preserve empty `content`/`reasoning_content`, pair tool calls/results, and derive sanitized stable `user_id`. Port the behavior into Codex-specific code with stricter state validation.

Protocol facts rechecked before writing this plan:

- Official Codex config supports custom `model_providers.<id>` with `base_url`, `env_key`, `wire_api = "responses"`, `supports_websockets`, retry and stream timeout settings; `responses` is the only supported custom provider wire API.
- Official Codex `model_catalog_json` is a local path loaded at startup, optionally profile-scoped. It is not a provider endpoint.
- Codex app-server v2 methods such as `thread/start`, `turn/start`, `model/list`, `modelProvider/capabilities/read`, `plugin/list`, `app/list`, `mcpServer/tool/call`, `tool/requestUserInput`, and `fs/*` are the desktop/app control plane. They must not be implemented by sub2api's model provider.
- Current local Codex source at `/tmp/openai-codex` confirms model provider `/models`, Responses SSE parser, `response.completed` requirement, `ResponseItem` families, and app-server v2 method separation.
- DeepSeek official docs currently expose `deepseek-v4-flash` and `deepseek-v4-pro`, OpenAI format base URL `https://api.deepseek.com`, thinking default `enabled`, reasoning efforts `high`/`max`, compatibility mapping `low|medium -> high` and `xhigh -> max`, 1M context, 384K max output, cache hit/miss usage fields, and function-only upstream tools.
- DeepSeek thinking tool loops require preserving `reasoning_content` across subsequent requests; missing it causes 400. DeepSeek streaming with `stream_options.include_usage = true` can send a usage-only chunk with `choices: []`.

## File Map

Create these new service files:

- `backend/internal/service/codex_gateway_types.go`: provider/model/request/response/common error types.
- `backend/internal/service/codex_gateway_errors.go`: Responses-compatible error mapping.
- `backend/internal/service/codex_gateway_model_registry.go`: model definitions, `/models` envelope, catalog export.
- `backend/internal/service/codex_gateway_model_registry_test.go`: registry and catalog shape tests.
- `backend/internal/service/codex_key_scope_policy.go`: Codex product scope and entitlement policy.
- `backend/internal/service/codex_key_scope_policy_test.go`: scope policy tests.
- `backend/internal/service/codex_gateway_state_store.go`: in-memory TTL state store for DeepSeek replay.
- `backend/internal/service/codex_gateway_state_store_test.go`: TTL, mismatch, and reasoning replay tests.
- `backend/internal/service/codex_gateway_tool_mapping.go`: function/namespace/custom tool mapping and schema normalization.
- `backend/internal/service/codex_gateway_tool_mapping_test.go`: tool-name/schema tests.
- `backend/internal/service/codex_gateway_responses_codec.go`: minimal Responses request/items/events and JSON helpers.
- `backend/internal/service/codex_gateway_responses_codec_test.go`: request parsing and SSE event schema tests.
- `backend/internal/service/codex_gateway_deepseek_request.go`: Responses input/tools -> DeepSeek chat request.
- `backend/internal/service/codex_gateway_deepseek_request_test.go`: conversion golden tests.
- `backend/internal/service/codex_gateway_deepseek_stream.go`: DeepSeek chunks -> Responses SSE accumulator.
- `backend/internal/service/codex_gateway_deepseek_stream_test.go`: stream golden tests and usage-only chunk tests.
- `backend/internal/service/codex_gateway_deepseek_adapter.go`: DeepSeek upstream execution for sync/stream.
- `backend/internal/service/codex_gateway_openai_responses_adapter.go`: native Responses service-layer forwarder adapter.
- `backend/internal/service/codex_gateway_provider_executor.go`: account/provider dispatch, failover boundary, usage result.
- `backend/internal/service/codex_gateway_service.go`: top-level service used by handler.
- `backend/internal/service/codex_gateway_usage.go`: Codex scoped usage field helpers.

Create these new handler/route/admin files:

- `backend/internal/handler/codex_gateway_handler.go`: `/codex/v1/models`, `/codex/v1/responses`, 405/501 placeholders.
- `backend/internal/handler/codex_gateway_handler_test.go`: HTTP contract tests with fake service.
- `backend/internal/server/routes/codex_gateway.go`: route registration under `/codex/v1`.
- `backend/internal/server/routes/codex_gateway_test.go`: route registration, middleware, no `/v1` alias tests.
- `backend/internal/handler/admin/codex_gateway_handler.go`: summary/provider-groups/models/smoke/state summary admin API.
- `backend/internal/server/routes/codex_gateway_admin.go`: admin route registration.

Modify existing files:

- `backend/internal/config/config.go`: add `GatewayCodexConfig`, defaults, validation.
- `backend/internal/pkg/httputil/body.go`: detect decompressed overflow with typed errors.
- `backend/internal/pkg/httputil/body_test.go`: add overflow and unsupported-encoding tests.
- `backend/internal/service/api_key.go`: add generic `ClientProduct()`/`IsClientProduct()` and `IsCodexOnly()`.
- `backend/internal/service/api_key_service.go`: add `CodexOnly` or generic restricted product handling for create/update.
- `backend/internal/handler/api_key_handler.go`: accept and return Codex-only key fields.
- `backend/internal/handler/dto/types.go`: expose `codex_only` and optionally `restricted_client_product`.
- `backend/internal/handler/dto/mappers.go`: map Codex-only fields.
- `backend/internal/service/group.go`: add `CodexGatewayEntitled`.
- `backend/internal/handler/dto/types.go`: add `codex_gateway_entitled` to group DTOs.
- `backend/internal/handler/admin/group_handler.go`: create/update request fields and mapper logic.
- `backend/internal/repository/group_repo.go`: persist `codex_gateway_entitled`.
- `backend/ent/schema/group.go`: add Ent field.
- Generated Ent files after schema change: `backend/ent/group.go`, `backend/ent/group_create.go`, `backend/ent/group_update.go`, `backend/ent/group_query.go`, `backend/ent/group/where.go`, and any schema metadata files produced by `go generate ./ent`.
- `backend/internal/repository/migrations_schema_integration_test.go`: assert new migration column.
- `backend/internal/handler/handler.go`: add `CodexGateway` handler fields.
- `backend/internal/handler/wire.go`: add Codex Gateway top-level/admin handler providers to `ProvideHandlers`, `ProvideAdminHandlers`, and `ProviderSet`.
- `backend/internal/service/wire.go`: add providers for Codex services.
- `backend/cmd/server/wire_gen.go`: generated DI after `wire` or manual update consistent with repo workflow.
- `backend/internal/server/routes/admin.go`: call `registerCodexGatewayAdminRoutes`.
- `backend/internal/server/routes/gateway.go` or server route aggregator: call `RegisterCodexGatewayRoutes` from the central route setup, leaving existing `/v1` routes untouched.
- `backend/internal/server/middleware/api_key_auth.go` and `backend/internal/server/middleware/middleware.go`: add an API-key auth variant/error writer that returns OpenAI Responses-compatible auth errors for `/codex/v1/*`.
- `backend/internal/service/openai_gateway_service.go`: expose a service-layer native Responses forwarding interface if no suitable public method exists.
- `backend/internal/service/model_pricing_resolver.go` / pricing fixtures: add DeepSeek Codex pricing keys before making models visible.
- `backend/config.example.yaml` or equivalent sample config if present: document `gateway.codex`.

Create migration:

- `backend/migrations/141_codex_gateway_entitlement.sql`: add `groups.codex_gateway_entitled BOOLEAN NOT NULL DEFAULT FALSE`.

Docs/runtime artifacts:

- `docs/codex-gateway/README.md`: short operator notes, config sample, smoke commands.
- `docs/codex-gateway/sub2api-codex-models.example.json`: generated catalog example.

## Constants and Interfaces

Use these names unless implementation discovery shows a conflicting local convention:

```go
const (
	CodexUsageClientProduct       = "codex_gateway"
	CodexUsageRequestScopeGateway = "gateway"
	CodexUsagePricingVersionV1    = "codex_gateway_v1"
	CodexUsageCostSourceProvider  = "provider_usage"
)
```

Service boundary sketch:

```go
type CodexGatewayService struct {
	cfg       config.GatewayCodexConfig
	registry *CodexGatewayModelRegistry
	executor *CodexGatewayProviderExecutor
	state    CodexGatewayStateStore
}

type CodexGatewayProviderAdapter interface {
	Execute(ctx context.Context, req CodexGatewayProviderRequest) (*CodexGatewayProviderResult, error)
	Stream(ctx context.Context, req CodexGatewayProviderRequest, sink CodexGatewayEventSink) (*CodexGatewayProviderResult, error)
}

type NativeResponsesForwarder interface {
	ForwardCodexResponses(ctx context.Context, input NativeCodexResponsesForwardInput) (*CodexGatewayProviderResult, error)
	StreamCodexResponses(ctx context.Context, input NativeCodexResponsesForwardInput, sink CodexGatewayEventSink) (*CodexGatewayProviderResult, error)
}
```

The exact method names can change, but keep these boundaries:

- Handler parses HTTP and writes HTTP only.
- Service resolves model, scope, state, and provider dispatch.
- Adapters translate provider protocol only.
- Usage helpers produce existing `UsageLog`/billing metadata only.

## Task 1: Config, Entitlement, and API Key Scope

**Files:**
- Modify: `backend/internal/config/config.go`
- Modify: `backend/ent/schema/group.go`
- Create: `backend/migrations/141_codex_gateway_entitlement.sql`
- Modify: `backend/internal/service/group.go`
- Modify: `backend/internal/repository/group_repo.go`
- Modify: `backend/internal/handler/dto/types.go`
- Modify: `backend/internal/handler/dto/mappers.go`
- Modify: `backend/internal/handler/admin/group_handler.go`
- Modify: `backend/internal/service/api_key.go`
- Modify: `backend/internal/service/api_key_service.go`
- Modify: `backend/internal/handler/api_key_handler.go`
- Create: `backend/internal/service/codex_key_scope_policy.go`
- Create: `backend/internal/service/codex_key_scope_policy_test.go`
- Modify: `backend/internal/repository/migrations_schema_integration_test.go`
- Test: `backend/internal/service/api_key_service_augment_scope_test.go` or new `backend/internal/service/api_key_service_codex_scope_test.go`
- Test: `backend/internal/handler/admin/admin_basic_handlers_test.go`

- [ ] **Step 1: Write failing scope policy tests**

Add tests that prove:

- Codex-scoped key with `CodexGatewayEntitled` group can access `/codex/v1/models` and `/codex/v1/responses`.
- Generic key is rejected on `/codex/v1/*`.
- Augment-only key is rejected on `/codex/v1/*`.
- Codex key is rejected when group is missing or not Codex-entitled.
- Codex key is rejected on non-Codex paths.

Expected policy shape:

```go
func TestCodexScopedAPIKeyAccessAllowsCodexRoutes(t *testing.T) {
	groupID := int64(1)
	product := CodexUsageClientProduct
	apiKey := &APIKey{
		ID: 10, UserID: 20, Key: "sk-codex", Status: StatusActive,
		GroupID: &groupID, RestrictedClientProduct: &product,
		Group: &Group{ID: groupID, Status: StatusActive, Hydrated: true, Platform: PlatformOpenAI, CodexGatewayEntitled: true},
	}
	for _, path := range []string{"/codex/v1/models", "/codex/v1/responses"} {
		require.NoError(t, ValidateCodexScopedAPIKeyAccess(apiKey, path))
	}
}
```

- [ ] **Step 2: Run failing tests**

Run:

```bash
cd backend && go test ./internal/service -run 'TestCodexScopedAPIKey|TestAPIKeyService.*Codex' -count=1
```

Expected: FAIL because Codex scope code and fields do not exist.

- [ ] **Step 3: Add config struct and defaults**

Add `Codex GatewayCodexConfig` to `GatewayConfig`.

Minimum struct:

```go
type GatewayCodexConfig struct {
	Enabled              bool                             `mapstructure:"enabled"`
	ExposeV1Alias        bool                             `mapstructure:"expose_v1_alias"`
	ModelCatalogPath     string                           `mapstructure:"model_catalog_path"`
	SupportsWebSockets   bool                             `mapstructure:"supports_websockets"`
	StateStoreTTLSeconds int                              `mapstructure:"state_store_ttl_seconds"`
	MaxStateItems        int                              `mapstructure:"max_state_items"`
	StreamMaxLineSize    int64                            `mapstructure:"stream_max_line_size"`
	EnabledModels        []string                         `mapstructure:"enabled_models"`
	ProviderGroups       GatewayCodexProviderGroupsConfig `mapstructure:"provider_groups"`
}

type GatewayCodexProviderGroupsConfig struct {
	OpenAI   int64 `mapstructure:"openai"`
	DeepSeek int64 `mapstructure:"deepseek"`
}
```

Default `Enabled` can be false if the project prefers opt-in gateways; route registration must still be testable with config enabled. Default `ExposeV1Alias` and `SupportsWebSockets` must be false. Provider group IDs use the same numeric group ID convention as `GatewayAugmentProviderGroupsConfig`; no string group-name resolution is part of the MVP.

- [ ] **Step 4: Add migration and group persistence**

Migration:

```sql
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS codex_gateway_entitled BOOLEAN NOT NULL DEFAULT FALSE;
```

Add Ent field, service field, repository create/update/scan mapping, DTO mapping, and admin create/update request fields named `codex_gateway_entitled`.

- [ ] **Step 5: Regenerate Ent code**

Run the repo-standard Ent generation command from `backend`:

```bash
cd backend && go generate ./ent
```

Expected generated changes include group create/update/query fields and predicates for `codex_gateway_entitled`. If generation changes unrelated files, inspect them before staging and keep only generator-produced Ent changes required for the new field.

- [ ] **Step 6: Add generic key product helpers**

In `api_key.go`, keep `IsAugmentOnly()` but implement it through a generic helper:

```go
func (k *APIKey) ClientProduct() string {
	if k == nil || k.RestrictedClientProduct == nil {
		return ""
	}
	return strings.TrimSpace(*k.RestrictedClientProduct)
}

func (k *APIKey) IsClientProduct(product string) bool {
	return k.ClientProduct() == strings.TrimSpace(product)
}

func (k *APIKey) IsCodexOnly() bool {
	return k.IsClientProduct(CodexUsageClientProduct)
}
```

- [ ] **Step 7: Extend API key create/update DTOs**

Prefer adding `CodexOnly` as a parallel alias for UI simplicity while preserving `restricted_client_product` internally. Validate that `augment_only` and `codex_only` cannot both be true.

Create/update logic must:

- Require a group for Codex-only keys.
- Require `group.CodexGatewayEntitled`.
- Set `RestrictedClientProduct = "codex_gateway"` for Codex-only.
- Clear `RestrictedClientProduct` when both scoped flags are false.
- Preserve Augment behavior and existing tests.

- [ ] **Step 8: Implement `codex_key_scope_policy.go`**

Allowed paths for MVP:

```go
var codexScopedAPIKeyAllowedPaths = map[string]struct{}{
	"/codex/v1/models": {},
	"/codex/v1/responses": {},
}
```

Add support for `/codex/v1/responses/compact` only if the handler returns 501 and product scope should allow it; otherwise keep it denied until implemented. Do not include `/v1/*` aliases in MVP.

- [ ] **Step 9: Run focused tests**

Run:

```bash
cd backend && go test ./internal/service -run 'TestCodexScopedAPIKey|TestAPIKeyService.*Codex|TestAPIKeyService.*Augment' -count=1
cd backend && go test ./internal/repository -run TestMigrationsSchema -count=1
cd backend && go test ./internal/handler/admin -run TestAdminBasicHandlers -count=1
```

Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add backend/internal/config/config.go backend/ent backend/migrations/141_codex_gateway_entitlement.sql backend/internal/service backend/internal/repository backend/internal/handler
git commit -m "feat: add codex gateway scope configuration"
```

## Task 2: Compressed Body Reader Hardening

**Files:**
- Modify: `backend/internal/pkg/httputil/body.go`
- Modify: `backend/internal/pkg/httputil/body_test.go`

- [ ] **Step 1: Write failing overflow tests**

Add tests for:

- zstd/gzip/deflate compressed body that decompresses to exactly `maxDecompressedBodySize`: allowed.
- decompressed body of `maxDecompressedBodySize + 1`: returns a typed overflow error.
- unsupported `Content-Encoding`: typed unsupported error.
- malformed compressed body: typed malformed/decode error.
- successful decode deletes `Content-Encoding` and `Content-Length`.

- [ ] **Step 2: Run failing tests**

```bash
cd backend && go test ./internal/pkg/httputil -run TestReadRequestBodyWithPrealloc -count=1
```

Expected: FAIL on overflow detection.

- [ ] **Step 3: Implement typed errors and limit+1 read**

Use `io.LimitReader(reader, maxDecompressedBodySize+1)`, then return `ErrRequestBodyTooLarge` if the decoded length exceeds the configured limit. Keep callers compatible by wrapping errors with `errors.Is`.

Suggested error constants:

```go
var (
	ErrUnsupportedContentEncoding = errors.New("unsupported Content-Encoding")
	ErrRequestBodyTooLarge        = errors.New("decompressed request body too large")
)
```

- [ ] **Step 4: Run focused tests**

```bash
cd backend && go test ./internal/pkg/httputil -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/pkg/httputil/body.go backend/internal/pkg/httputil/body_test.go
git commit -m "fix: detect decompressed request body overflow"
```

## Task 3: Codex Gateway Route and Handler Skeleton

**Files:**
- Modify: `backend/internal/service/codex_gateway_types.go`
- Create: `backend/internal/handler/codex_gateway_handler.go`
- Create: `backend/internal/handler/codex_gateway_handler_test.go`
- Create: `backend/internal/server/routes/codex_gateway.go`
- Create: `backend/internal/server/routes/codex_gateway_test.go`
- Modify: `backend/internal/server/middleware/api_key_auth.go`
- Modify: `backend/internal/server/middleware/middleware.go`
- Modify: `backend/internal/server/routes/gateway.go` or central route setup file
- Modify: `backend/internal/handler/handler.go`

- [ ] **Step 1: Write route tests**

Tests must prove:

- `GET /codex/v1/models` is registered when `gateway.codex.enabled = true`.
- `POST /codex/v1/responses` is registered.
- `GET /codex/v1/responses` returns 405.
- `POST /codex/v1/responses/compact` returns 501.
- `/v1/models` and `/v1/responses` are not hijacked by Codex when `expose_v1_alias = false`.
- Missing, invalid, disabled, expired, and quota-exhausted keys on `/codex/v1/models` and `/codex/v1/responses` return Responses-compatible error envelopes with `{"error":{"type":...,"code":...,"message":...}}`, not the existing generic `{code,message}` shape.

- [ ] **Step 2: Run failing route tests**

```bash
cd backend && go test ./internal/server/routes -run TestCodexGatewayRoutes -count=1
```

Expected: FAIL because routes are missing.

- [ ] **Step 3: Add minimal compileable Codex service types**

Create `backend/internal/service/codex_gateway_types.go` in this task with only the types needed by the handler and route tests. Later tasks will extend the same file.

Minimum skeleton:

```go
type CodexGatewayModelsRequest struct {
	ClientVersion string
}

type CodexGatewayModelsResponse struct {
	Models []map[string]any `json:"models"`
}

type CodexGatewayHTTPRequest struct {
	APIKey          *APIKey
	RawBody         []byte
	Headers         http.Header
	InboundEndpoint string
}

type CodexGatewayHTTPResult struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
	Stream     bool
}
```

- [ ] **Step 4: Implement handler skeleton**

Handler constructor:

```go
type CodexGatewayServiceInterface interface {
	Models(ctx context.Context, req service.CodexGatewayModelsRequest) (*service.CodexGatewayModelsResponse, error)
	Responses(ctx context.Context, req service.CodexGatewayHTTPRequest) (*service.CodexGatewayHTTPResult, error)
}

type CodexGatewayHandler struct {
	svc CodexGatewayServiceInterface
}
```

`Models` should delegate to service and return JSON. `Responses` should read the already-authenticated API key from middleware context, call scope validation, decode body through `httputil.ReadRequestBodyWithPrealloc`, and delegate to service. Streaming support can initially write through a service-provided stream callback in later tasks; for skeleton tests, use fake service.

- [ ] **Step 5: Add Responses-compatible auth error writer**

The existing API-key middleware uses `AbortWithError` and can return a generic `{code,message}` shape before the Codex handler runs. Add either:

- a new API key auth constructor that accepts a protocol error writer, or
- a small Codex-specific auth middleware wrapper that preserves existing validation but writes OpenAI/Responses-shaped errors.

The writer must include both `error.type` and `error.code`. Example:

```json
{"error":{"type":"authentication_error","code":"invalid_api_key","message":"Invalid API key"}}
```

Map missing/invalid/disabled keys to 401, access denied to 403, expired/quota-exhausted according to existing auth semantics, but always with Responses-compatible body shape.

- [ ] **Step 6: Register `/codex/v1` independently**

Route group should use:

- request body limit
- client request ID
- ops error logger
- inbound endpoint middleware
- Codex API key auth with Responses-compatible error writer
- Codex scope middleware or handler-level validation

Do not add shared `/v1/*` alias in this task.

- [ ] **Step 7: Add handler fields without full Wire service integration**

Add `CodexGateway *CodexGatewayHandler` to `handler.Handlers`. For this skeleton task, route tests may build `handler.Handlers` manually with a fake Codex handler/service. Do not add real `wire.go` providers or edit `wire_gen.go` until Task 13, after `CodexGatewayService` exists.

- [ ] **Step 8: Run tests**

```bash
cd backend && go test ./internal/handler -run TestCodexGatewayHandler -count=1
cd backend && go test ./internal/server/routes -run TestCodexGatewayRoutes -count=1
cd backend && go test ./internal/server/routes -run 'TestGatewayRoutesOpenAI|TestGatewayRoutes' -count=1
```

Expected: PASS, existing OpenAI route tests unaffected.

- [ ] **Step 9: Commit**

```bash
git add backend/internal/service/codex_gateway_types.go backend/internal/handler backend/internal/server/routes backend/internal/server/middleware
git commit -m "feat: add codex gateway http surface"
```

## Task 4: Model Registry and Catalog Export

**Files:**
- Create: `backend/internal/service/codex_gateway_types.go`
- Create: `backend/internal/service/codex_gateway_model_registry.go`
- Create: `backend/internal/service/codex_gateway_model_registry_test.go`
- Create: `docs/codex-gateway/sub2api-codex-models.example.json`
- Modify: `backend/internal/handler/codex_gateway_handler.go`

- [ ] **Step 1: Write failing registry tests**

Tests must validate:

- Default visible models before billing/protocol gates: native OpenAI/Codex models only, such as `gpt-5.5`, `gpt-5.4`, `gpt-5.4-mini`, and `gpt-5.3-codex`.
- DeepSeek models `deepseek-v4-pro` and `deepseek-v4-flash` exist in the registry as internal/hidden entries until Task 12 pricing gates and Task 15 protocol fixtures pass.
- DeepSeek model metadata: 1M context, conservative `auto_compact_token_limit`, text-only modality, `supported_reasoning_levels: ["high","xhigh"]`, `supports_parallel_tool_calls = false`, no hosted web/image search.
- `/models` response envelope has `models` array, stable `slug`, `display_name`, `supported_in_api`, `visibility`, and Codex catalog fields.
- Catalog JSON generation is non-empty and matches `/models` entries.

- [ ] **Step 2: Run failing tests**

```bash
cd backend && go test ./internal/service -run TestCodexGatewayModelRegistry -count=1
```

Expected: FAIL because registry does not exist.

- [ ] **Step 3: Implement registry**

Define provider constants:

```go
type CodexGatewayProvider string
const (
	CodexGatewayProviderOpenAI   CodexGatewayProvider = "openai"
	CodexGatewayProviderDeepSeek CodexGatewayProvider = "deepseek"
)
```

DeepSeek model entries:

- Public IDs: `deepseek-v4-pro`, `deepseek-v4-flash`.
- Upstream IDs: same.
- Default visibility: `hidden` or `supported_in_api:false` until Task 12 and Task 15 explicitly flip the gate. They may be selectable internally in tests but must not be advertised as visible to Codex clients yet.
- `ContextWindow: 1000000`.
- `AutoCompactTokenLimit: 850000`.
- `MaxOutputTokens: 384000`, but enforce smaller gateway default if needed.
- `DefaultReasoningLevel: "xhigh"` for Codex agent workloads.
- `SupportsParallelToolCalls: false` until conformance tests pass.
- `ExperimentalSupportedTools: ["function","namespace","custom"]`.

Native entries:

- Public IDs from current Codex/OpenAI models.
- Provider `openai`.
- Preserve reasoning, verbosity, service tier, image/web/tool metadata according to registry config.

Visibility gate:

- Native OpenAI/Codex models can be visible after this task.
- DeepSeek entries stay hidden until cache-hit/cache-miss pricing tests pass in Task 12 and protocol golden fixtures pass in Task 15.

- [ ] **Step 4: Hook `/codex/v1/models` to registry**

`GET /codex/v1/models?client_version=...` should return the registry envelope. Include an ETag only if the registry can compute it from the same normalized catalog bytes; otherwise leave ETag absent and add a test that the handler does not emit a stale or hard-coded ETag.

- [ ] **Step 5: Generate example catalog**

Add a checked-in example JSON under docs by using the registry shape manually or via a small `go test` fixture. Do not require operators to use this static file; the service should be able to generate configured catalog output later.

- [ ] **Step 6: Run tests**

```bash
cd backend && go test ./internal/service -run TestCodexGatewayModelRegistry -count=1
cd backend && go test ./internal/handler -run TestCodexGatewayHandlerModels -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/service/codex_gateway_types.go backend/internal/service/codex_gateway_model_registry.go backend/internal/service/codex_gateway_model_registry_test.go backend/internal/handler/codex_gateway_handler.go docs/codex-gateway/sub2api-codex-models.example.json
git commit -m "feat: add codex gateway model registry"
```

## Task 5: Responses Codec and Event Writer

**Files:**
- Create: `backend/internal/service/codex_gateway_responses_codec.go`
- Create: `backend/internal/service/codex_gateway_responses_codec_test.go`
- Create: `backend/internal/service/codex_gateway_errors.go`

- [ ] **Step 1: Write failing codec tests**

Cover these fixtures:

- Minimal text request with `model`, `input`, `stream`.
- Request preserving unknown fields for native pass-through.
- `instructions`, `tools`, `tool_choice`, `parallel_tool_calls`, `reasoning`, `text`, `include`, `prompt_cache_key`, `client_metadata`, `max_output_tokens`.
- `previous_response_id` returns explicit invalid-request error on DeepSeek HTTP path.
- Error JSON shape:

```json
{"error":{"type":"invalid_request_error","code":"invalid_request","message":"..."}}
```

- SSE event JSON for `response.created`, `response.output_item.added`, `response.output_text.delta`, `response.function_call_arguments.delta`, `response.function_call_arguments.done`, `response.output_item.done`, `response.completed`, `response.failed`, and `response.incomplete`.

- [ ] **Step 2: Run failing tests**

```bash
cd backend && go test ./internal/service -run 'TestCodexGatewayResponsesCodec|TestCodexGatewayResponseEvents|TestCodexGatewayErrors' -count=1
```

Expected: FAIL.

- [ ] **Step 3: Implement minimal structs with raw preservation**

Use `json.RawMessage` for broad Responses item/tool bodies. Avoid over-modeling the whole OpenAI schema.

Required request struct fields:

```go
type CodexGatewayResponsesRequest struct {
	Model              string          `json:"model"`
	Instructions       json.RawMessage `json:"instructions,omitempty"`
	Input              json.RawMessage `json:"input"`
	Tools              json.RawMessage `json:"tools,omitempty"`
	ToolChoice         json.RawMessage `json:"tool_choice,omitempty"`
	ParallelToolCalls  *bool           `json:"parallel_tool_calls,omitempty"`
	Reasoning          json.RawMessage `json:"reasoning,omitempty"`
	Text               json.RawMessage `json:"text,omitempty"`
	Include            json.RawMessage `json:"include,omitempty"`
	PromptCacheKey     string          `json:"prompt_cache_key,omitempty"`
	MaxOutputTokens    *int            `json:"max_output_tokens,omitempty"`
	PreviousResponseID *string         `json:"previous_response_id,omitempty"`
	Stream             *bool           `json:"stream,omitempty"`
	ClientMetadata     json.RawMessage `json:"client_metadata,omitempty"`
	Raw                map[string]json.RawMessage `json:"-"`
}
```

- [ ] **Step 4: Implement event builder**

Terminal events must include a complete `response` object, not only an ID. Add tests that parse the emitted payload back into a generic map and assert required fields.

- [ ] **Step 5: Run tests**

```bash
cd backend && go test ./internal/service -run 'TestCodexGatewayResponsesCodec|TestCodexGatewayResponseEvents|TestCodexGatewayErrors' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/service/codex_gateway_responses_codec.go backend/internal/service/codex_gateway_responses_codec_test.go backend/internal/service/codex_gateway_errors.go
git commit -m "feat: add codex responses codec"
```

## Task 6: State Store for DeepSeek Tool Loops

**Files:**
- Create: `backend/internal/service/codex_gateway_state_store.go`
- Create: `backend/internal/service/codex_gateway_state_store_test.go`

- [ ] **Step 1: Write failing state tests**

Tests:

- Save and retrieve by response ID, session key, API key/user isolation, provider, and upstream model.
- Expired entries are not returned.
- Provider/model/API key/session mismatch returns explicit conflict/missing error.
- DeepSeek assistant with tool calls stores raw `reasoning_content`.
- Missing raw `reasoning_content` for a DeepSeek-generated thinking tool-loop assistant is invalid state.
- Gateway-synthesized compatibility messages may have `reasoning_content_synthesized = true`.
- Tool name map is retained for future tool outputs.

- [ ] **Step 2: Run failing tests**

```bash
cd backend && go test ./internal/service -run TestCodexGatewayStateStore -count=1
```

Expected: FAIL.

- [ ] **Step 3: Implement in-memory TTL store**

Use a mutex-protected map, bounded by `MaxStateItems`, with periodic opportunistic cleanup on set/get. Store only model-visible replay fragments and metadata; never store raw Authorization headers or complete request bodies.

Core record:

```go
type CodexGatewayStateRecord struct {
	ResponseID       string
	SessionKey       string
	APIKeyID         int64
	UserID           int64
	Provider         CodexGatewayProvider
	Model            string
	UpstreamModel    string
	AssistantItems   []CodexGatewayStoredAssistantItem
	ToolNameMap      map[string]CodexGatewayToolNameMapping
	Usage            CodexGatewayUsage
	UpstreamRequestID string
	CreatedAt        time.Time
	ExpiresAt        time.Time
}
```

- [ ] **Step 4: Add session key helper**

Hash seed order: `prompt_cache_key`, `x-codex-turn-state`, `session_id`, `thread_id`, stable `client_metadata`, then request body hash. Include API key/user isolation in the final state lookup key.

- [ ] **Step 5: Run tests**

```bash
cd backend && go test ./internal/service -run TestCodexGatewayStateStore -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/service/codex_gateway_state_store.go backend/internal/service/codex_gateway_state_store_test.go
git commit -m "feat: add codex gateway state store"
```

## Task 7: Tool Mapping and DeepSeek Schema Normalization

**Files:**
- Create: `backend/internal/service/codex_gateway_tool_mapping.go`
- Create: `backend/internal/service/codex_gateway_tool_mapping_test.go`

- [ ] **Step 1: Write failing tool mapping tests**

Cover:

- Direct function names remain unchanged when valid.
- Namespace tools flatten to names matching `[A-Za-z0-9_-]{1,64}`.
- Custom/freeform tools flatten to synthetic function tools.
- Long names are truncated with deterministic hash suffix/prefix.
- Collisions are detected deterministically.
- Reverse mapping restores original namespace/name/tool kind.
- `defer_loading` and `output_schema` are not sent to DeepSeek.
- Strict schemas are downgraded by default.
- Strict beta mode requires `additionalProperties:false` and all object properties in `required`.
- Unsupported strict constraints either stripped or rejected according to config.

- [ ] **Step 2: Run failing tests**

```bash
cd backend && go test ./internal/service -run TestCodexGatewayToolMapping -count=1
```

Expected: FAIL.

- [ ] **Step 3: Implement name encoder**

Suggested rule:

- Keep valid unique function names as-is.
- Namespace: `ns_<base32hash10>_<safe_suffix>`.
- Custom: `custom_<base32hash10>_<safe_suffix>`.
- Max final length: 64.
- If two different original tools map to same encoded name, return 400.

- [ ] **Step 4: Implement schema normalization**

Default MVP: set/keep `strict:false` for DeepSeek. Do not send OpenAI strict schemas to DeepSeek unless `gateway.codex.deepseek_strict_beta_enabled` or equivalent config is explicitly added and tested.

- [ ] **Step 5: Run tests**

```bash
cd backend && go test ./internal/service -run TestCodexGatewayToolMapping -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/service/codex_gateway_tool_mapping.go backend/internal/service/codex_gateway_tool_mapping_test.go
git commit -m "feat: add codex tool mapping for deepseek"
```

## Task 8: DeepSeek Request Conversion

**Files:**
- Create: `backend/internal/service/codex_gateway_deepseek_request.go`
- Create: `backend/internal/service/codex_gateway_deepseek_request_test.go`
- Modify: `backend/internal/service/codex_gateway_types.go`

- [ ] **Step 1: Write failing conversion tests**

Fixtures:

- `instructions` -> leading system message.
- User `message` with `input_text` -> role `user`.
- Assistant `message` with output text -> role `assistant`.
- `function_call` -> assistant `tool_calls` with `content:""`.
- `function_call_output` -> `role:"tool"` with string content and matching `tool_call_id`.
- Custom tool call/output -> reversible synthetic function mapping.
- Namespace/MCP tool call/output -> flattened function mapping.
- Structured tool output -> deterministic JSON string.
- Image content in DeepSeek text-only mode -> deterministic placeholder or configured reject.
- `max_output_tokens` -> `max_tokens`.
- `reasoning.effort` mapping: `low|medium|high -> high`, `xhigh -> max`, default `max`; `none|minimal` only disables thinking if model config explicitly allows.
- Thinking mode strips `temperature`, `top_p`, `presence_penalty`, `frequency_penalty`.
- Tools present: do not send `tool_choice` by default.
- No tools: delete `tool_choice`.
- Validate assistant tool call/result pairing.
- Missing raw `reasoning_content` in state for prior DeepSeek tool loop returns invalid state.
- Stable `user_id` contains only `[A-Za-z0-9_-]` and length <= 512.

- [ ] **Step 2: Run failing tests**

```bash
cd backend && go test ./internal/service -run TestCodexGatewayDeepSeekRequest -count=1
```

Expected: FAIL.

- [ ] **Step 3: Implement DeepSeek message structs without `omitempty` where empty fields matter**

Avoid omitting `content` on assistant tool-call messages. Avoid omitting `reasoning_content` when replay requires an empty synthesized string.

Use custom marshaling or map construction for protocol-significant empty strings:

```go
msg := map[string]any{
	"role": "assistant",
	"content": "",
	"reasoning_content": rawReasoningContent,
	"tool_calls": toolCalls,
}
```

- [ ] **Step 4: Implement request parameter policy**

DeepSeek body must include:

```json
{
  "model": "deepseek-v4-pro",
  "messages": [],
  "thinking": {"type":"enabled"},
  "reasoning_effort": "max",
  "stream": true,
  "stream_options": {"include_usage": true},
  "user_id": "codex_gateway_<hash>"
}
```

Send `tools` only when non-empty. Send no Responses-only fields upstream.

- [ ] **Step 5: Run tests**

```bash
cd backend && go test ./internal/service -run TestCodexGatewayDeepSeekRequest -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/service/codex_gateway_deepseek_request.go backend/internal/service/codex_gateway_deepseek_request_test.go backend/internal/service/codex_gateway_types.go
git commit -m "feat: translate codex responses requests to deepseek"
```

## Task 9: DeepSeek Non-Streaming Adapter

**Files:**
- Create: `backend/internal/service/codex_gateway_deepseek_adapter.go`
- Create: `backend/internal/service/codex_gateway_deepseek_adapter_test.go`
- Modify: `backend/internal/service/codex_gateway_state_store.go`

- [ ] **Step 1: Write failing non-stream tests**

Use `httptest.Server` fake DeepSeek `/chat/completions` and assert:

- Request body matches DeepSeek OpenAI-compatible schema.
- Normal text completion returns Responses object with `object:"response"`, `status:"completed"`, `output`, `usage`.
- Tool-call completion returns Responses `function_call` or `custom_tool_call` output items with IDs/call IDs.
- `finish_reason:"length"` returns `status:"incomplete"`.
- `finish_reason:"insufficient_system_resource"` maps to gateway incomplete/failed strategy.
- DeepSeek 400 maps to Responses error JSON without leaking raw Chat Completions chunks.
- Usage maps `prompt_cache_hit_tokens` to `input_tokens_details.cached_tokens` and internal cache read tokens.
- State store records raw `reasoning_content` for tool-call turns.

- [ ] **Step 2: Run failing tests**

```bash
cd backend && go test ./internal/service -run TestCodexGatewayDeepSeekAdapterNonStream -count=1
```

Expected: FAIL.

- [ ] **Step 3: Implement sync adapter**

Adapter responsibilities:

- Build request with Task 8 converter.
- Call upstream through the repo's standard HTTP upstream/client abstraction if available; otherwise add a narrow interface for tests.
- Parse DeepSeek response.
- Convert assistant message/tool calls into Responses output items.
- Normalize usage.
- Save state on successful or tool-call responses.
- Return `CodexGatewayProviderResult` for usage recording.

- [ ] **Step 4: Run tests**

```bash
cd backend && go test ./internal/service -run TestCodexGatewayDeepSeekAdapterNonStream -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/service/codex_gateway_deepseek_adapter.go backend/internal/service/codex_gateway_deepseek_adapter_test.go backend/internal/service/codex_gateway_state_store.go
git commit -m "feat: add deepseek codex non-stream adapter"
```

## Task 10: DeepSeek Streaming Adapter

**Files:**
- Create: `backend/internal/service/codex_gateway_deepseek_stream.go`
- Create: `backend/internal/service/codex_gateway_deepseek_stream_test.go`
- Modify: `backend/internal/service/codex_gateway_deepseek_adapter.go`

- [ ] **Step 1: Write failing stream golden tests**

Golden cases:

- Simple reasoning + text stream.
- Function tool call with argument deltas.
- Custom/freeform tool call with input deltas.
- Multiple tool calls if parser can accumulate them; keep registry flag false until real conformance passes.
- Usage-only chunk with `choices: []` before `[DONE]`.
- `[DONE]` is swallowed and not forwarded as a Responses event.
- `response.completed` emitted before downstream close.
- Terminal event contains complete `response`.
- Stream closes before completion -> `response.failed` or `response.incomplete`.
- `finish_reason:"insufficient_system_resource"` -> incomplete/failed, not completed.

- [ ] **Step 2: Run failing stream tests**

```bash
cd backend && go test ./internal/service -run TestCodexGatewayDeepSeekStream -count=1
```

Expected: FAIL.

- [ ] **Step 3: Implement chunk parser and accumulator**

Parser invariant:

```go
if chunk.Usage != nil && len(chunk.Choices) == 0 {
	acc.SetUsage(chunk.Usage)
	continue
}
if len(chunk.Choices) == 0 {
	continue
}
choice := chunk.Choices[0]
```

Event order:

1. `response.created`
2. `response.output_item.added` before deltas
3. text/reasoning/tool delta events
4. `response.function_call_arguments.done` as needed
5. `response.output_item.done`
6. `response.completed` or terminal failed/incomplete

- [ ] **Step 4: Implement stream failure boundary**

Before model-visible deltas, executor may fail over in a later task. After any delta/tool item is flushed, no account switch; emit terminal failure/incomplete and record partial usage if known.

- [ ] **Step 5: Run tests**

```bash
cd backend && go test ./internal/service -run TestCodexGatewayDeepSeekStream -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/service/codex_gateway_deepseek_stream.go backend/internal/service/codex_gateway_deepseek_stream_test.go backend/internal/service/codex_gateway_deepseek_adapter.go
git commit -m "feat: stream deepseek as codex responses events"
```

## Task 11: Native OpenAI Responses Adapter

**Files:**
- Create: `backend/internal/service/codex_gateway_openai_responses_adapter.go`
- Create: `backend/internal/service/codex_gateway_openai_responses_adapter_test.go`
- Modify: `backend/internal/service/openai_gateway_service.go`
- Modify: `backend/internal/service/openai_gateway_core.go` if needed

- [ ] **Step 1: Write failing native adapter tests**

Tests with fake forwarder:

- Preserves `prompt_cache_key`, `reasoning`, `text`, `tools`, `tool_choice`, `parallel_tool_calls`, `include`, `client_metadata`, and Codex headers.
- Does not run DeepSeek state-store transforms.
- Returns normalized `CodexGatewayProviderResult`.
- Streams native Responses SSE through without corrupting event order.
- Preserves safe headers: `x-request-id`, `x-codex-turn-state`, `X-Reasoning-Included`, `X-Models-Etag`, `OpenAI-Model`, `X-OpenAI-Model`.

- [ ] **Step 2: Run failing tests**

```bash
cd backend && go test ./internal/service -run TestCodexGatewayOpenAIResponsesAdapter -count=1
```

Expected: FAIL.

- [ ] **Step 3: Expose service-layer forwarder**

Do not call `OpenAIGatewayHandler.Responses`. Add a method or adapter around `OpenAIGatewayService` that accepts authenticated context, selected account/group, raw body, and headers.

The method must not assume the request came from `/openai/v1` or existing handler validation.

- [ ] **Step 4: Implement native adapter**

Keep it thin:

- Resolve upstream/native model from registry.
- Forward raw JSON with allowed Codex headers.
- Let existing OpenAI Gateway handle unsupported-field retry where already implemented.
- Normalize usage/log metadata into Codex result.

- [ ] **Step 5: Run tests**

```bash
cd backend && go test ./internal/service -run 'TestCodexGatewayOpenAIResponsesAdapter|TestOpenAIGatewayService.*Codex' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/service/codex_gateway_openai_responses_adapter.go backend/internal/service/codex_gateway_openai_responses_adapter_test.go backend/internal/service/openai_gateway_service.go backend/internal/service/openai_gateway_core.go
git commit -m "feat: add native responses adapter for codex gateway"
```

## Task 12: Provider Executor, Account Selection, and Usage/Billing

**Files:**
- Create: `backend/internal/service/codex_gateway_provider_executor.go`
- Create: `backend/internal/service/codex_gateway_provider_executor_test.go`
- Create: `backend/internal/service/codex_gateway_usage.go`
- Create: `backend/internal/service/codex_gateway_usage_test.go`
- Modify: `backend/internal/service/model_pricing_resolver.go`
- Modify: pricing fixtures/catalog files as needed
- Modify: `backend/internal/service/openai_gateway_service.go` only where usage recording needs a shared method

- [ ] **Step 1: Write failing executor and usage tests**

Tests:

- OpenAI model chooses configured OpenAI provider group.
- DeepSeek model chooses configured DeepSeek provider group.
- Missing provider group returns Responses-compatible account exhaustion/config error.
- Failover allowed before any downstream model-visible output.
- Failover forbidden after output flush.
- Usage fields include `client_product = "codex_gateway"`, `request_scope = "gateway"`, provider, requested/upstream model, stream/sync request type, reasoning effort, upstream request ID.
- DeepSeek cache hit tokens are billed/logged as `CacheReadTokens`.
- DeepSeek cache miss tokens remain `InputTokens`.
- Unit prices can represent separate cache hit/miss pricing before DeepSeek models are visible.
- Registry marks DeepSeek models pricing-ready but still hidden until Task 15 protocol fixture gate also passes.
- Native OpenAI path preserves existing usage mapping plus Codex product metadata.

- [ ] **Step 2: Run failing tests**

```bash
cd backend && go test ./internal/service -run 'TestCodexGatewayProviderExecutor|TestCodexGatewayUsage' -count=1
```

Expected: FAIL.

- [ ] **Step 3: Implement executor**

Use existing scheduler/account selection patterns from `augment_gateway_provider_executor.go` and OpenAI/Gateway services. Do not reuse `AugmentGatewayProviderRequest` or result types.

Executor input should include:

- API key/user/group.
- Requested Codex model entry.
- Raw request body hash.
- Stream flag.
- Session key.
- Inbound endpoint.
- Header subset.

- [ ] **Step 4: Implement usage helpers**

Use existing `AugmentUsageFields` only if it is generalized safely. Prefer introducing Codex constants and a helper returning the existing field struct or a renamed generalized type in a small refactor.

Do not add a new usage table. This task may set an internal `PricingReady`/equivalent gate for DeepSeek entries after cache-hit/cache-miss tests pass, but it must not make DeepSeek entries visible in `/codex/v1/models` yet. Visibility requires both this pricing gate and Task 15 protocol fixture gate.

- [ ] **Step 5: Run tests**

```bash
cd backend && go test ./internal/service -run 'TestCodexGatewayProviderExecutor|TestCodexGatewayUsage|TestModelPricingResolver' -count=1
cd backend && go test ./internal/repository -run 'TestUsageLog.*ClientProduct|TestUsageLog.*CacheRead' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/service/codex_gateway_provider_executor.go backend/internal/service/codex_gateway_provider_executor_test.go backend/internal/service/codex_gateway_usage.go backend/internal/service/codex_gateway_usage_test.go backend/internal/service/model_pricing_resolver.go backend/internal/repository
git commit -m "feat: integrate codex gateway provider execution and usage"
```

## Task 13: Top-Level Service and HTTP Responses Flow

**Files:**
- Create: `backend/internal/service/codex_gateway_service.go`
- Create: `backend/internal/service/codex_gateway_service_test.go`
- Modify: `backend/internal/handler/codex_gateway_handler.go`
- Modify: `backend/internal/handler/codex_gateway_handler_test.go`
- Modify: `backend/internal/handler/wire.go`
- Modify: `backend/internal/service/wire.go`
- Modify: `backend/cmd/server/wire_gen.go`

- [ ] **Step 1: Write failing service tests**

Tests:

- Unknown/disabled model -> 400 `model_not_found`.
- Valid Codex key but non-entitled group -> 403 `codex_gateway_not_entitled`.
- Previous response ID on DeepSeek HTTP -> explicit 400 until compatibility path is implemented.
- Stream request sets SSE headers and flushes terminal `response.completed`.
- Non-stream request returns complete Responses JSON.
- Unsupported content encoding -> 400 `unsupported_content_encoding`.
- Decompressed body too large -> 413 `request_body_too_large`.
- Native model dispatches to native adapter.
- DeepSeek model dispatches to DeepSeek adapter.

- [ ] **Step 2: Run failing tests**

```bash
cd backend && go test ./internal/service -run TestCodexGatewayService -count=1
cd backend && go test ./internal/handler -run TestCodexGatewayHandlerResponses -count=1
```

Expected: FAIL.

- [ ] **Step 3: Implement service orchestration**

Flow:

1. Parse request JSON with codec.
2. Resolve model from registry.
3. Validate product scope and group entitlement.
4. Build session key.
5. Dispatch through provider executor.
6. Return sync result or stream through event sink.
7. Map errors to Responses-compatible HTTP or SSE terminal events.

- [ ] **Step 4: Implement handler streaming**

For stream:

- Set `Content-Type: text/event-stream`.
- Set `Cache-Control: no-cache`.
- Use Gin writer flush.
- Ensure service is responsible for terminal event; handler should not silently close.

- [ ] **Step 5: Add real DI providers**

Add `ProvideCodexGateway...` constructors to `backend/internal/service/wire.go`, wire the real `CodexGatewayService` into `handler.NewCodexGatewayHandler` through `backend/internal/handler/wire.go`, and update `backend/cmd/server/wire_gen.go` using the repo-standard command:

```bash
cd backend && go generate ./cmd/server
```

If Wire is unavailable, make the minimal manual `wire_gen.go` update and record that in the task summary.

- [ ] **Step 6: Run tests**

```bash
cd backend && go test ./internal/service -run TestCodexGatewayService -count=1
cd backend && go test ./internal/handler -run TestCodexGatewayHandlerResponses -count=1
cd backend && go test ./cmd/server -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/service/codex_gateway_service.go backend/internal/service/codex_gateway_service_test.go backend/internal/handler/codex_gateway_handler.go backend/internal/handler/codex_gateway_handler_test.go backend/internal/handler/wire.go backend/internal/service/wire.go backend/cmd/server/wire_gen.go
git commit -m "feat: wire codex gateway responses flow"
```

## Task 14: Admin Surface and Smoke Visibility

**Files:**
- Create: `backend/internal/handler/admin/codex_gateway_handler.go`
- Create: `backend/internal/handler/admin/codex_gateway_handler_test.go`
- Create: `backend/internal/server/routes/codex_gateway_admin.go`
- Modify: `backend/internal/server/routes/admin.go`
- Modify: `backend/internal/handler/handler.go`
- Modify: `backend/internal/handler/wire.go`
- Modify: `backend/internal/service/wire.go`
- Modify: `backend/cmd/server/wire_gen.go`

- [ ] **Step 1: Write failing admin tests**

Admin routes:

- `GET /api/v1/admin/codex-gateway/summary`
- `GET /api/v1/admin/codex-gateway/provider-groups`
- `PUT /api/v1/admin/codex-gateway/provider-groups`
- `GET /api/v1/admin/codex-gateway/models`
- `PUT /api/v1/admin/codex-gateway/models/:id`
- `POST /api/v1/admin/codex-gateway/smoke`
- `GET /api/v1/admin/codex-gateway/state-store/summary`

Tests should verify route registration and basic JSON shapes. They do not need real upstream calls.

- [ ] **Step 2: Run failing tests**

```bash
cd backend && go test ./internal/handler/admin -run TestCodexGatewayAdminHandler -count=1
cd backend && go test ./internal/server/routes -run TestCodexGatewayAdminRoutes -count=1
```

Expected: FAIL.

- [ ] **Step 3: Implement minimal admin service/handler**

Mirror Augment admin operational shape but keep separate structs. It is acceptable for PUT/smoke to be basic in MVP if tests document behavior.

- [ ] **Step 4: Run tests**

```bash
cd backend && go test ./internal/handler/admin -run TestCodexGatewayAdminHandler -count=1
cd backend && go test ./internal/server/routes -run TestCodexGatewayAdminRoutes -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/handler/admin/codex_gateway_handler.go backend/internal/handler/admin/codex_gateway_handler_test.go backend/internal/server/routes/codex_gateway_admin.go backend/internal/server/routes/admin.go backend/internal/handler/handler.go backend/internal/handler/wire.go backend/internal/service/wire.go backend/cmd/server/wire_gen.go
git commit -m "feat: add codex gateway admin surface"
```

## Task 15: Integration Fixtures and Manual Codex Smoke Docs

**Files:**
- Create: `backend/internal/service/testdata/codex_gateway/*.json`
- Create: `docs/codex-gateway/README.md`
- Create: `docs/codex-gateway/smoke.md`
- Modify: relevant service tests to load golden fixtures

- [ ] **Step 1: Add golden fixture set**

Fixtures:

- simple text streaming turn
- function tool call
- namespace/MCP tool call
- custom/freeform apply-patch style tool call
- tool result follow-up
- DeepSeek thinking tool loop with raw `reasoning_content`
- final assistant in tool loop with no `tool_calls` but still requiring reasoning replay
- missing reasoning invalid-state path
- DeepSeek usage-only stream chunk
- DeepSeek context length, rate limit, and insufficient resource errors
- multimodal degradation placeholder

- [ ] **Step 2: Add fixture tests**

Every golden event stream must assert:

- no raw Chat Completions chunks leak
- each SSE `data` payload is valid JSON except no `[DONE]`
- event order is accepted by current Codex parser expectations
- terminal event is present
- usage shape is complete
- DeepSeek model entries remain hidden if any required fixture fails.

- [ ] **Step 3: Flip DeepSeek visibility only after gates pass**

After Task 12 pricing gates and this task's golden fixtures pass, add tests that `deepseek-v4-pro` and `deepseek-v4-flash` become visible in `/codex/v1/models` only when both gates are true:

- provider group configured and healthy
- model enabled
- cache-hit/cache-miss pricing gate true
- protocol fixture gate true
- admin model visibility not explicitly hidden

If any gate is false, registry must keep the DeepSeek model hidden or `supported_in_api:false`.

- [ ] **Step 4: Add operator docs**

Include a Codex config sample:

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

Manual smoke list:

- `GET /codex/v1/models`
- simple Codex CLI chat
- shell tool
- file edit/apply patch
- MCP resource/tool if configured
- plugin/app tool if available
- Computer Use/Chrome plugin if local Desktop exposes those tools

- [ ] **Step 5: Run integration-focused tests**

```bash
cd backend && go test ./internal/service -run 'TestCodexGateway.*Golden|TestCodexGateway.*Integration' -count=1
cd backend && go test ./internal/handler ./internal/server/routes -run TestCodexGateway -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/service/testdata/codex_gateway docs/codex-gateway
git commit -m "test: add codex gateway protocol fixtures"
```

## Task 16: Full Verification

**Files:**
- No new files unless fixes are required.

- [ ] **Step 1: Run focused package tests**

```bash
cd backend && go test ./internal/pkg/httputil -count=1
cd backend && go test ./internal/service -run 'TestCodexGateway|TestCodexScoped|TestAPIKeyService.*Codex|TestModelPricingResolver' -count=1
cd backend && go test ./internal/handler -run TestCodexGateway -count=1
cd backend && go test ./internal/handler/admin -run TestCodexGateway -count=1
cd backend && go test ./internal/server/routes -run TestCodexGateway -count=1
cd backend && go test ./internal/repository -run 'TestMigrationsSchema|TestUsageLog' -count=1
```

Expected: PASS.

- [ ] **Step 2: Run broader backend tests**

Use the repo's standard backend test command. If no single command exists, run:

```bash
cd backend && go test ./internal/...
```

Expected: PASS or known unrelated failures documented with exact failing tests.

- [ ] **Step 3: Run build or wire validation**

If Wire is available:

```bash
cd backend && wire ./cmd/server
```

Then:

```bash
cd backend && go test ./cmd/server -run Test -count=1
```

If Wire is not available, run:

```bash
cd backend && go test ./cmd/server -count=1
```

Expected: DI compiles.

- [ ] **Step 4: Manual local smoke**

Start server per repo convention. Then:

```bash
curl -sS -H "Authorization: Bearer $SUB2API_CODEX_API_KEY" http://127.0.0.1:3000/codex/v1/models | jq .
curl -sS -N -H "Authorization: Bearer $SUB2API_CODEX_API_KEY" -H "Content-Type: application/json" \
  -d '{"model":"deepseek-v4-flash","input":[{"role":"user","content":[{"type":"input_text","text":"Say ok"}]}],"stream":true}' \
  http://127.0.0.1:3000/codex/v1/responses
```

Expected: `/models` includes both native and DeepSeek models; stream ends with `response.completed`.

- [ ] **Step 5: Commit verification fixes only**

If verification requires changes:

```bash
git add <changed-files>
git commit -m "test: verify codex gateway integration"
```

## Explicit Deferrals

Do not implement these in this plan:

- Shared `/v1/*` alias dispatch. Add only after `/codex/v1/*` passes real Codex CLI/Desktop smoke and route precedence tests cover OpenAI, Codex, Augment, generic, invalid, and missing keys.
- Responses WebSocket. Keep `supports_websockets = false`, `GET /codex/v1/responses = 405`.
- `/codex/v1/responses/compact`. Return 501 until compact contract is separately designed.
- DeepSeek hosted web search or image generation. Codex-local/plugin/MCP tools can be represented through function/custom tools; hosted-only tools must be hidden, degraded, or rejected.
- DeepSeek strict beta mode by default. Add it later only behind config and schema conformance tests.
- Multi-instance durable state store. Keep an interface so Redis/database can replace in-memory TTL later.

## Review Checklist

Before implementation is considered ready:

- Codex app-server v2 methods are not exposed by sub2api.
- Provider surface is only `/codex/v1/models` and `/codex/v1/responses` for MVP.
- Every stream has a schema-valid terminal event.
- DeepSeek usage-only chunks with `choices: []` cannot panic the parser.
- DeepSeek thinking tool loops preserve raw `reasoning_content`; missing required reasoning fails closed.
- Tool mapping is reversible and collision-safe.
- DeepSeek function names always satisfy 64-char and character constraints.
- `tool_choice` is not sent to DeepSeek thinking mode by default.
- DeepSeek `user_id` is sanitized, non-reversible, and <= 512 chars.
- Usage logs can distinguish `client_product = "codex_gateway"` from Augment and generic OpenAI Gateway.
- Cache hits and misses are separately billable before DeepSeek models are visible.
- Existing Augment Gateway tests still pass.
- Existing `/v1/*`, `/responses`, `/backend-api/codex/responses`, and `/openai/v1/*` behavior is unchanged.
