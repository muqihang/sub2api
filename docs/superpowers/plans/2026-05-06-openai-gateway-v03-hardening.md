# OpenAI Gateway v0.3 Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the existing Sub2API OpenAI Gateway production-gated, fail-closed, auditable, and aligned with the v0.3 governance model before live GPT/OpenAI OAuth canary.

**Architecture:** Keep OpenAI Gateway inside `sub2api`; do not depend on `cc-gateway` and do not introduce `x-cc-*` semantics. Add hard policy contracts around egress resolution, route/client auth, canonical profile, secret storage, OAuth callback state, and operational redaction. Preserve local/test compatibility behind explicit flags, while production mode fails closed.

**Tech Stack:** Go, Gin, Ent, Viper config, Redis where already wired, PostgreSQL JSONB credentials, existing OpenAI Gateway services/tests, `go test`.

---

## Scope And Non-Goals

Implement in the isolated worktree created from `sub2api/main`. Do not edit the dirty root worktree.

In scope:

- Phase B: fail-closed egress, unified resolver, route-level gateway client-token parity, token/proxy redaction, route matrix tests.
- Phase C: `_verify` / admin hardening, bucket validation and concentration visibility, preflight update.
- Phase C2: production gate for OpenAI credential encryption and OAuth callback/session topology.
- Phase D: HTTP/WS parity for account runtime, egress, client token, canonical profile, and credential-type fallback.
- Phase E preparation: live canary SOR and runbook only. Actual live test waits for real GPT/OpenAI OAuth accounts.

Out of scope:

- No OpenAI Gateway rewrite.
- No frontend work.
- No live OpenAI OAuth test without a real account.
- No transport-fingerprint claim unless a later explicit task adds live JA3/JA4/ALPN evidence.

## Files To Create Or Modify

### Config And Deployment

- Modify: `backend/internal/config/config.go`
  - Add fail-closed/fallback config fields.
  - Add production gate fields for credential encryption and OAuth session mode.
  - Validate bucket names, duplicates, default bucket existence, disabled bucket use, proxy URL syntax, and production mode constraints.
- Modify: `deploy/config.example.yaml`
  - Document production-safe OpenAI Gateway profile separately from local defaults.
- Modify: `deploy/OPENAI_GATEWAY_PREFLIGHT.md`
  - Align expected `_verify` fields with redacted output.
- Modify: `deploy/openai-gateway-preflight.sh`
  - Fail if output contains raw upstream token/proxy URL patterns.
- Create: `docs/harness/OPENAI_GATEWAY_LIVE_CANARY_SOR.md`
  - Record blocked live OAuth items and acceptance evidence checklist.

### Egress And Runtime

- Create: `backend/internal/service/openai_gateway_egress.go`
  - Own the typed OpenAI egress resolver contract.
  - Own proxy URL redaction/hash helpers.
- Test: `backend/internal/service/openai_gateway_egress_test.go`
  - Unit tests for fail-closed, fallback, redaction, bucket validation helper behavior.
- Modify: `backend/internal/service/openai_gateway_core.go`
  - Replace raw `EgressProxyURL` in public snapshots.
  - Use typed resolver for runtime.
  - Mask admin bucket output.
- Modify: `backend/internal/service/openai_gateway_service.go`
  - Replace direct `account.Proxy.URL()` and string-only `resolveOpenAIProxyURL` usage in Responses/passthrough paths.
- Modify: `backend/internal/service/openai_gateway_chat_completions.go`
  - Use typed resolver for Chat Completions.
- Modify: `backend/internal/service/openai_gateway_chat_completions_raw.go`
  - Use typed resolver for raw Chat Completions path.
- Modify: `backend/internal/service/openai_gateway_messages.go`
  - Use typed resolver for Messages bridge.
- Modify: `backend/internal/service/openai_gateway_images.go`
  - Use typed resolver for OpenAI image path.
- Modify: `backend/internal/service/openai_images.go`
  - Remove direct account proxy fallback.
- Modify: `backend/internal/service/openai_images_responses.go`
  - Remove direct account proxy fallback.
- Modify: `backend/internal/service/openai_apikey_responses_probe.go`
  - Use typed resolver or explicitly mark API-key probe fallback policy.
- Modify: `backend/internal/service/openai_oauth_service.go`
  - Resolve egress for OAuth refresh/exchange/privacy flows under the same policy.
- Modify: `backend/internal/service/openai_privacy_service.go`
  - Ensure logs never include raw token/proxy values.
- Modify: `backend/internal/service/openai_ws_forwarder.go`
  - Use typed resolver for WS dial and reject policy errors.
- Modify: `backend/internal/service/openai_ws_v2_passthrough_adapter.go`
  - Use typed resolver for WS v2 passthrough.

### Handlers And Routes

- Modify: `backend/internal/handler/openai_gateway_handler.go`
  - Enforce gateway client token consistently for Responses and WS.
  - Harden `_health` / `_verify`.
- Modify: `backend/internal/handler/openai_images.go`
  - Enforce gateway client token consistently for Images.
- Verify/possibly modify: `backend/internal/handler/openai_chat_completions.go`
  - Already calls `enforceOptionalGatewayClientAuth`; add regression coverage.
- Modify: `backend/internal/server/routes/gateway.go`
  - Keep public aliases, but ensure route/platform contract is testable and fail-closed.
- Test: `backend/internal/server/routes/openai_gateway_route_matrix_test.go`
  - Matrix tests for canonical and compatibility aliases.

### Canonical Profile

- Create: `backend/internal/service/openai_gateway_profile.go`
  - Single source of truth for OpenAI-visible identity fields.
- Test: `backend/internal/service/openai_gateway_profile_test.go`
  - Verify `User-Agent`, `version`, `originator`, `OpenAI-Beta`, `X-Stainless-*`, WS headers, and route-specific variants.
- Modify: `backend/internal/service/openai_gateway_core.go`
  - Move profile struct/logic to the new artifact or delegate to it.
- Modify: `backend/internal/service/openai_gateway_service.go`
  - Replace scattered constants `codexCLIUserAgent` / `codexCLIVersion` with profile artifact references.
- Modify: `backend/internal/service/openai_ws_forwarder.go`
  - Apply the same profile artifact to WS headers.

### Secret Storage And OAuth Session

- Create: `backend/internal/service/openai_secret_protector.go`
  - Encrypt/decrypt selected OpenAI account credential fields.
- Test: `backend/internal/service/openai_secret_protector_test.go`
  - Round-trip, plaintext compatibility, missing-key production gate, no accidental token echo.
- Modify: `backend/internal/service/openai_oauth_service.go`
  - Build encrypted OpenAI credentials when the protector is configured.
- Modify: `backend/internal/service/openai_token_provider.go`
  - Decrypt credentials before token use, without exposing plaintext in logs/snapshots.
- Modify: `backend/internal/service/token_refresh_service.go`
  - Preserve encryption when refresh updates credentials.
- Modify: `backend/internal/service/openai_oauth_lifecycle.go`
  - Normalize imported encrypted/plaintext credentials safely.
- Create: `backend/internal/service/openai_gateway_credentials.go`
  - Central OpenAI credential accessor that decrypts OAuth and API-key credentials before use.
- Test: `backend/internal/service/openai_gateway_credentials_test.go`
  - Verify API-key and OAuth readers never return ciphertext to upstream code.
- Modify: `backend/internal/service/openai_gateway_chat_completions_raw.go`
  - Replace direct `account.GetOpenAIApiKey()` / `account.GetCredential("api_key")` reads with protected accessors.
- Modify: `backend/internal/service/openai_gateway_images.go`
  - Replace direct API-key reads with protected accessors.
- Modify: `backend/internal/service/openai_images.go`
  - Replace direct API-key reads with protected accessors.
- Modify: WS API-key paths in `backend/internal/service/openai_ws_forwarder.go` and `backend/internal/service/openai_ws_v2_passthrough_adapter.go`
  - Ensure encrypted API keys are decrypted once in memory before upstream use.
- Modify: `backend/internal/service/openai_token_provider.go`
  - Decide and implement encrypted Redis token-cache storage, production disabling, or explicit Redis-as-secret-store gate.
- Modify: `backend/internal/pkg/openai/oauth.go`
  - Define an `OAuthSessionStore` interface and keep memory implementation.
- Create: `backend/internal/service/openai_oauth_session_store.go`
  - Redis-backed OpenAI OAuth session store, or a strict deployment gate if Redis is unavailable.
- Test: `backend/internal/service/openai_oauth_session_store_test.go`
  - TTL, state/verifier retrieval, delete, malformed payload, and shared-store behavior.
- Modify: `backend/internal/service/wire.go`
  - Wire secret protector and OAuth session store into OpenAI OAuth service.
- Modify: `backend/cmd/server/wire_gen.go`
  - Regenerate checked-in Wire output after constructor/provider changes.
- Modify if present: `backend/cmd/server/wire_gen_test.go`
  - Keep generated wiring tests in sync.

## Implementation Rules

- Use TDD: write failing tests first, run them, then implement.
- Commit after each task.
- After every major task, dispatch a review agent before moving to the next theme.
- Do not run destructive git commands.
- Do not include raw tokens, refresh tokens, ID tokens, API keys, or full proxy URLs in tests/log fixtures.
- Prefer adding small helper files over expanding already large service files.

## Task 0: Baseline Build, Wiring, And Worktree Readiness

**Files:**

- Inspect: `backend/go.mod`
- Inspect: `backend/internal/service/wire.go`
- Inspect: `backend/cmd/server/wire_gen.go`
- Inspect if present: `backend/cmd/server/wire_gen_test.go`

- [ ] **Step 1: Confirm module root and baseline test command**

Run:

```bash
(cd backend && go test ./internal/config -run TestNonexistent -count=1)
```

Expected: PASS with no tests to run, proving commands must execute from `backend/`.

- [ ] **Step 2: Record Wire generation command**

Find the repo's existing Wire command from docs, Makefile, or developer scripts:

```bash
rg -n "wire|wire_gen" Makefile backend .github tools
```

Expected: identify the exact command, typically one of:

```bash
(cd backend && wire ./cmd/server)
```

or the project-specific wrapper if one exists.

- [ ] **Step 3: Establish generated-code rule**

Any task that changes constructors/providers for OpenAI OAuth service, secret protector, OAuth session store, Redis wiring, or config providers must:

- update `backend/internal/service/wire.go`;
- run the Wire generation command;
- stage `backend/cmd/server/wire_gen.go`;
- stage `backend/cmd/server/wire_gen_test.go` if generated or affected.

- [ ] **Step 4: Commit only if this task changes docs/scripts**

If no files changed, do not commit. If the executor records a local helper note, commit it with:

```bash
git add <changed-doc-or-script>
git commit -m "docs: record openai gateway implementation baseline"
```

## Task 1: Config Schema And Production Validation

**Files:**

- Modify: `backend/internal/config/config.go`
- Modify: `deploy/config.example.yaml`
- Test: add or extend `backend/internal/config/config_test.go`

- [ ] **Step 1: Add failing config tests**

Add tests for:

```go
func TestOpenAICoreConfigValidationRejectsDuplicateBuckets(t *testing.T) {}
func TestOpenAICoreConfigValidationRejectsMissingDefaultBucket(t *testing.T) {}
func TestOpenAICoreConfigValidationRejectsInvalidProxyURL(t *testing.T) {}
func TestOpenAICoreConfigValidationNormalizesSocks5Proxy(t *testing.T) {}
func TestOpenAICoreProductionRejectsDirectFallback(t *testing.T) {}
func TestOpenAICoreProductionRequiresCredentialGate(t *testing.T) {}
func TestOpenAICoreProductionRequiresOAuthSessionMode(t *testing.T) {}
```

Run:

```bash
(cd backend && go test ./internal/config -run 'TestOpenAICore' -count=1)
```

Expected: FAIL because the fields and validation do not exist yet.

- [ ] **Step 2: Add config fields**

In `GatewayOpenAICoreConfig`, add:

```go
EgressFailClosed          bool   `mapstructure:"egress_fail_closed"`
AllowAccountProxyFallback bool   `mapstructure:"allow_account_proxy_fallback"`
AllowDirectFallback       bool   `mapstructure:"allow_direct_fallback"`
ProductionMode            bool   `mapstructure:"production_mode"`
RequireEncryptedCredentials bool `mapstructure:"require_encrypted_credentials"`
CredentialEncryptionKey   string `mapstructure:"credential_encryption_key"`
OAuthSessionStore         string `mapstructure:"oauth_session_store"` // memory/redis
OAuthCallbackStickySingleInstance bool `mapstructure:"oauth_callback_sticky_single_instance"`
ExposeRawProxyInDebug     bool   `mapstructure:"expose_raw_proxy_in_debug"`
BucketWarnAccountThreshold int   `mapstructure:"bucket_warn_account_threshold"`
```

Use local-safe defaults:

```go
gateway.openai_core.egress_fail_closed = false
gateway.openai_core.allow_account_proxy_fallback = true
gateway.openai_core.allow_direct_fallback = true
gateway.openai_core.production_mode = false
gateway.openai_core.require_encrypted_credentials = false
gateway.openai_core.oauth_session_store = "memory"
gateway.openai_core.oauth_callback_sticky_single_instance = false
gateway.openai_core.expose_raw_proxy_in_debug = false
gateway.openai_core.bucket_warn_account_threshold = 2
```

Production docs must show the opposite posture:

```yaml
gateway:
  openai_core:
    production_mode: true
    egress_fail_closed: true
    allow_account_proxy_fallback: false
    allow_direct_fallback: false
    require_encrypted_credentials: true
    oauth_session_store: redis
```

- [ ] **Step 3: Implement validation**

Validation must reject:

- blank bucket names;
- duplicate bucket names;
- `default_egress_bucket` not present in `egress_buckets`;
- disabled `default_egress_bucket` while `egress_fail_closed=true`;
- non-empty `proxy_url` that fails the existing `internal/pkg/proxyurl.Parse` validator;
- direct `url.Parse` / `url.ParseRequestURI` proxy validation. The repo's proxy URL package is the required path because it normalizes `socks5` to `socks5h`, rejects missing hosts, supports the expected schemes, and avoids leaking credentials in parse errors;
- `production_mode=true` with `egress_fail_closed=false`;
- `production_mode=true` with either fallback flag enabled;
- `production_mode=true` with `require_encrypted_credentials=false`;
- `production_mode=true` with `oauth_session_store=memory` and `oauth_callback_sticky_single_instance=false`.

- [ ] **Step 4: Run config tests**

Run:

```bash
(cd backend && go test ./internal/config -run 'TestOpenAICore' -count=1)
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/config/config.go deploy/config.example.yaml backend/internal/config/config_test.go
git commit -m "feat: add openai gateway production config gates"
```

## Task 2: Typed Egress Resolver And Redaction Contract

**Files:**

- Create: `backend/internal/service/openai_gateway_egress.go`
- Test: `backend/internal/service/openai_gateway_egress_test.go`
- Modify: `backend/internal/service/openai_gateway_core.go`
- Update tests: `backend/internal/service/openai_gateway_core_test.go`

- [ ] **Step 1: Add failing resolver tests**

Cover:

- selected enabled bucket with proxy URL;
- selected enabled direct bucket allowed in local mode;
- missing bucket fails when `egress_fail_closed=true`;
- disabled bucket fails when `egress_fail_closed=true`;
- disabled bucket falls back only when fallback flag allows it;
- account proxy fallback disabled in fail-closed mode;
- direct fallback disabled in fail-closed mode;
- redacted proxy never exposes userinfo/password;
- stable `proxy_hash` changes when proxy endpoint changes.

Run:

```bash
(cd backend && go test ./internal/service -run 'TestOpenAIGatewayEgress|TestOpenAIGatewayCore' -count=1)
```

Expected: FAIL.

- [ ] **Step 2: Implement typed contract**

Add:

```go
type OpenAIEgressResolution struct {
    BucketName    string
    ProxyURL      string
    ProxySelected bool
    ProxyLabel    string
    ProxyHash     string
    Source        string // bucket/account_fallback/direct_fallback
}

type OpenAIEgressPolicyError struct {
    Code       string
    BucketName string
}
```

Provide:

```go
func (s *OpenAIGatewayCoreService) ResolveEgress(ctx context.Context, account *Account, fallbackProxyURL string) (*OpenAIEgressResolution, error)
func MaskOpenAIProxyURL(raw string) string
func HashOpenAIProxyURL(raw string) string
```

`OpenAIEgressPolicyError` must implement `Error()` and support `errors.As` so handlers and WS code can convert policy failures without string matching.

`HashOpenAIProxyURL` must not hash the full secret-bearing raw URL directly. Hash a canonical redacted endpoint identity, or use an HMAC with a deployment key if operators need a stable identity that includes sensitive endpoint distinctions.

Keep the old `ResolveEgressProxyURL` temporarily only as a deprecated wrapper for compatibility; all new call sites must use `ResolveEgress`.

- [ ] **Step 3: Replace public raw proxy fields**

In `OpenAIGatewayAccountRuntime` and `OpenAIGatewayVerifySnapshot`, replace:

```go
EgressProxyURL string `json:"egress_proxy_url,omitempty"`
```

with:

```go
ProxySelected bool   `json:"proxy_selected"`
ProxyLabel    string `json:"proxy_label,omitempty"`
ProxyHash     string `json:"proxy_hash,omitempty"`
```

For explicitly authorized debug mode, expose a separate `DebugProxyURL` only when `ExposeRawProxyInDebug=true` and the caller is already authenticated as a probe/operator.

- [ ] **Step 4: Mask admin bucket output**

Do not return raw `[]config.OpenAIGatewayEgressBucketConfig` from admin status. Introduce:

```go
type OpenAIGatewayAdminBucketSnapshot struct {
    Name          string `json:"name"`
    Enabled       bool   `json:"enabled"`
    ProxySelected bool   `json:"proxy_selected"`
    ProxyLabel    string `json:"proxy_label,omitempty"`
    ProxyHash     string `json:"proxy_hash,omitempty"`
    AccountCount  int64  `json:"account_count"`
    Warning       string `json:"warning,omitempty"`
}
```

- [ ] **Step 5: Run resolver/core tests**

Run:

```bash
(cd backend && go test ./internal/service -run 'TestOpenAIGatewayEgress|TestOpenAIGatewayCore' -count=1)
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/service/openai_gateway_egress.go backend/internal/service/openai_gateway_egress_test.go backend/internal/service/openai_gateway_core.go backend/internal/service/openai_gateway_core_test.go
git commit -m "feat: add fail-closed openai egress resolver"
```

## Task 3A: Wire Egress Resolver Across Responses And Messages HTTP Paths

**Files:**

- Modify: `backend/internal/service/openai_gateway_service.go`
- Modify: `backend/internal/service/openai_gateway_messages.go`
- Update tests: `backend/internal/service/openai_oauth_passthrough_test.go`, `backend/internal/service/openai_messages_dispatch_test.go`

- [ ] **Step 1: Add failing Responses/Messages egress tests**

Add tests that configure:

```go
cfg.Gateway.OpenAICore.EgressFailClosed = true
cfg.Gateway.OpenAICore.AllowAccountProxyFallback = false
cfg.Gateway.OpenAICore.AllowDirectFallback = false
```

Then verify these paths reject missing/disabled bucket before upstream dial:

- Responses OAuth passthrough in `openai_oauth_passthrough_test.go`;
- Messages bridge in `openai_messages_dispatch_test.go` or a focused new test;
- no test upstream receives a request when the resolver returns a policy error.

Run:

```bash
(cd backend && go test ./internal/service -run 'OpenAI.*(Responses|Passthrough|Messages).*Egress|OpenAI.*Proxy' -count=1)
```

Expected: FAIL where code still reads `account.Proxy.URL()` or ignores resolver errors.

- [ ] **Step 2: Replace direct proxy access in Responses/Messages**

Use this search as the implementation checklist:

```bash
rg -n 'account\\.Proxy\\.URL\\(\\)|resolveOpenAIProxyURL\\(|ResolveEgressProxyURL\\(' backend/internal/service/openai_gateway_service.go backend/internal/service/openai_gateway_messages.go
```

Every Responses/Messages upstream send path must call a typed helper that returns `(proxyURL string, err error)`. Policy errors must be returned to the handler as an upstream policy failure, not silently retried through direct egress.

- [ ] **Step 3: Preserve local compatibility only behind flags**

Fallback behavior is allowed only when the matching flag is true:

- bucket direct -> `AllowDirectFallback=true`;
- account proxy fallback -> `AllowAccountProxyFallback=true`;
- disabled/missing bucket fallback -> only in non-fail-closed migration mode.

- [ ] **Step 4: Convert policy errors to stable client errors**

For HTTP handlers, map egress policy failures to `503` with safe OpenAI-compatible error shape:

```json
{"error":{"type":"api_error","message":"OpenAI gateway egress policy rejected the request"}}
```

Do not include bucket internals, proxy URL, token value, or account proxy details in client responses.

For WS, close before upstream dial with an internal policy close reason that does not expose secrets.

- [ ] **Step 5: Run targeted service tests**

Run:

```bash
(cd backend && go test ./internal/service -run 'OpenAI.*(Responses|Passthrough|Messages).*Egress|OpenAI.*Proxy' -count=1)
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/service/openai_gateway_service.go backend/internal/service/openai_gateway_messages.go backend/internal/service/openai_oauth_passthrough_test.go backend/internal/service/openai_messages_dispatch_test.go
git commit -m "feat: enforce openai egress resolver on responses paths"
```

## Task 3B: Wire Egress Resolver Across Chat, Raw Chat, Images, And Probes

**Files:**

- Modify: `backend/internal/service/openai_gateway_chat_completions.go`
- Modify: `backend/internal/service/openai_gateway_chat_completions_raw.go`
- Modify: `backend/internal/service/openai_gateway_images.go`
- Modify: `backend/internal/service/openai_images.go`
- Modify: `backend/internal/service/openai_images_responses.go`
- Modify: `backend/internal/service/openai_apikey_responses_probe.go`
- Update tests near each path.

- [ ] **Step 1: Add failing Chat/Image/probe egress tests**

Cover fail-closed missing/disabled buckets for:

- Chat Completions;
- raw Chat Completions;
- image generation/edit;
- OpenAI API-key Responses probe.

Run:

```bash
(cd backend && go test ./internal/service -run 'OpenAI.*(Chat|Image|Probe).*Egress|OpenAI.*Proxy' -count=1)
```

Expected: FAIL until all paths use typed resolver errors.

- [ ] **Step 2: Replace direct proxy access in Chat/Image/probe files**

```bash
rg -n 'account\\.Proxy\\.URL\\(\\)|resolveOpenAIProxyURL\\(|ResolveEgressProxyURL\\(' backend/internal/service/openai_gateway_chat_completions.go backend/internal/service/openai_gateway_chat_completions_raw.go backend/internal/service/openai_gateway_images.go backend/internal/service/openai_images.go backend/internal/service/openai_images_responses.go backend/internal/service/openai_apikey_responses_probe.go
```

Expected after implementation: no direct `account.Proxy.URL()` in OpenAI upstream send paths.

- [ ] **Step 3: Run targeted tests**

```bash
(cd backend && go test ./internal/service -run 'OpenAI.*(Chat|Image|Probe).*Egress|OpenAI.*Proxy' -count=1)
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/service/openai_gateway_chat_completions.go backend/internal/service/openai_gateway_chat_completions_raw.go backend/internal/service/openai_gateway_images.go backend/internal/service/openai_images.go backend/internal/service/openai_images_responses.go backend/internal/service/openai_apikey_responses_probe.go backend/internal/service/*chat*test.go backend/internal/service/*image*test.go backend/internal/service/*probe*test.go
git commit -m "feat: enforce openai egress resolver on chat and image paths"
```

## Task 3C: Wire Egress Resolver Across OAuth Refresh And Privacy Calls

**Files:**

- Modify: `backend/internal/service/openai_oauth_service.go`
- Modify: `backend/internal/service/openai_privacy_service.go`
- Update tests: `backend/internal/service/openai_oauth_service_refresh_test.go`, `backend/internal/service/openai_privacy_retry_test.go`

- [ ] **Step 1: Add failing refresh/privacy egress tests**

Cover:

- OAuth refresh rejects fail-closed missing/disabled bucket;
- privacy fetch/disable calls use the same selected proxy as token refresh;
- privacy logs do not expose raw proxy or token values.

Run:

```bash
(cd backend && go test ./internal/service -run 'OpenAI.*(Refresh|Privacy).*Egress|Test.*Privacy' -count=1)
```

Expected: FAIL.

- [ ] **Step 2: Use typed resolver for account-backed refresh/privacy**

When an account exists, token refresh and privacy calls must resolve egress through `OpenAIGatewayCoreService.ResolveEgress`. Resolver policy errors must be returned as classified auth/runtime failures without exposing proxy URL.

- [ ] **Step 3: Run tests**

```bash
(cd backend && go test ./internal/service -run 'OpenAI.*(Refresh|Privacy).*Egress|Test.*Privacy' -count=1)
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/service/openai_oauth_service.go backend/internal/service/openai_privacy_service.go backend/internal/service/openai_oauth_service_refresh_test.go backend/internal/service/openai_privacy_retry_test.go
git commit -m "feat: enforce openai egress resolver on refresh and privacy"
```

## Task 3D: Define Pre-Account OAuth Egress Binding

**Files:**

- Modify: `backend/internal/pkg/openai/oauth.go`
- Modify: `backend/internal/service/openai_oauth_service.go`
- Modify: `backend/internal/handler/admin/openai_oauth_handler.go`
- Modify if needed: `backend/internal/service/openai_oauth_lifecycle.go`
- Test: `backend/internal/service/openai_oauth_service_auth_url_test.go`
- Test: `backend/internal/service/openai_oauth_service_state_test.go`
- Test: admin handler or lifecycle test covering `CreateAccountFromOAuth` account `Extra`

- [ ] **Step 1: Add failing pre-account OAuth egress tests**

Cover:

- `GenerateAuthURL` accepts or derives an egress bucket before account creation;
- OAuth session stores `egress_bucket`, `proxy_selected`, and masked proxy identity, not raw proxy URL in public output;
- `ExchangeCode` validates the stored bucket fail-closed before token exchange;
- `CreateAccountFromOAuth` or lifecycle import persists `openai_gateway_egress_bucket` from the OAuth session into account `Extra`;
- invalid/missing/disabled OAuth session bucket rejects before token exchange.

Run:

```bash
(cd backend && go test ./internal/service -run 'TestOpenAIOAuthService.*(AuthURL|State|Egress|Exchange)' -count=1)
```

Expected: FAIL because session currently stores only raw `ProxyURL`.

- [ ] **Step 2: Extend OAuth session egress fields**

Add to `openai.OAuthSession`:

```go
EgressBucket string `json:"egress_bucket,omitempty"`
ProxySelected bool `json:"proxy_selected,omitempty"`
ProxyLabel string `json:"proxy_label,omitempty"`
ProxyHash string `json:"proxy_hash,omitempty"`
```

Do not expose raw `ProxyURL` in user/admin output. If raw proxy is still needed internally for local compatibility, keep it internal to the session store and never return it from handlers.

If operators should choose a bucket during OAuth login, add `egress_bucket` to the admin OAuth request handling in `backend/internal/handler/admin/openai_oauth_handler.go` for both auth URL generation and exchange/create requests. If the intended behavior is default-bucket-only, state that explicitly in code/docs and test that default derivation is fail-closed in production.

- [ ] **Step 3: Add pre-account resolver path**

Add a resolver method for OAuth session creation, for example:

```go
ResolveOAuthSessionEgress(ctx context.Context, requestedBucket string, fallbackProxyURL string) (*OpenAIEgressResolution, error)
```

This must follow the same fail-closed/fallback flags as account-backed resolution.

- [ ] **Step 4: Persist imported account binding**

When OAuth token exchange succeeds and account credentials are built/imported, persist the selected `openai_gateway_egress_bucket` in account `extra`.

- [ ] **Step 5: Run tests**

```bash
(cd backend && go test ./internal/service -run 'TestOpenAIOAuthService.*(AuthURL|State|Egress|Exchange)' -count=1)
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/pkg/openai/oauth.go backend/internal/service/openai_oauth_service.go backend/internal/handler/admin/openai_oauth_handler.go backend/internal/service/openai_oauth_lifecycle.go backend/internal/service/openai_oauth_service_auth_url_test.go backend/internal/service/openai_oauth_service_state_test.go
git commit -m "feat: bind openai oauth sessions to egress buckets"
```

## Task 3E: Wire Egress Resolver Across WebSocket Paths

**Files:**

- Modify: `backend/internal/service/openai_ws_forwarder.go`
- Modify: `backend/internal/service/openai_ws_v2_passthrough_adapter.go`
- Update tests: `backend/internal/service/openai_ws_forwarder_success_test.go`, relevant WS v2 tests.

- [ ] **Step 1: Add failing WS egress tests**

Cover:

- WS rejects missing/disabled bucket before dial;
- WS v2 passthrough rejects resolver policy errors before dial;
- `ResolveAccountRuntime` errors are not ignored when building WS headers;
- WS close/error reason is safe and does not include bucket internals or proxy URL.

Run:

```bash
(cd backend && go test ./internal/service -run 'OpenAI.*WS.*Egress|TestOpenAIWS.*Proxy|TestOpenAIWS.*Runtime' -count=1)
```

Expected: FAIL until WS paths hard-fail resolver errors.

- [ ] **Step 2: Replace direct proxy access in WS paths**

```bash
rg -n 'account\\.Proxy\\.URL\\(\\)|resolveOpenAIProxyURL\\(|ResolveEgressProxyURL\\(' backend/internal/service/openai_ws_forwarder.go backend/internal/service/openai_ws_v2_passthrough_adapter.go
```

Expected after implementation: no silent fallback or ignored runtime error.

- [ ] **Step 3: Run tests**

```bash
(cd backend && go test ./internal/service -run 'OpenAI.*WS.*Egress|TestOpenAIWS.*Proxy|TestOpenAIWS.*Runtime' -count=1)
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/service/openai_ws_forwarder.go backend/internal/service/openai_ws_v2_passthrough_adapter.go backend/internal/service/*ws*test.go
git commit -m "feat: enforce openai egress resolver on websocket paths"
```

## Task 4: Gateway Client-Token And Route Matrix Parity

**Files:**

- Modify: `backend/internal/handler/openai_gateway_handler.go`
- Modify: `backend/internal/handler/openai_images.go`
- Verify/modify: `backend/internal/handler/openai_chat_completions.go`
- Modify if needed: `backend/internal/server/routes/gateway.go`
- Test: `backend/internal/server/routes/openai_gateway_route_matrix_test.go`
- Update: `backend/internal/handler/openai_gateway_core_handler_test.go`

- [ ] **Step 1: Add route matrix tests**

Create a table covering:

- `/openai/v1/responses`
- `/v1/responses`
- `/responses`
- `/backend-api/codex/responses`
- `/openai/v1/chat/completions`
- `/v1/chat/completions`
- `/chat/completions`
- `/openai/v1/images/generations`
- `/v1/images/generations`
- `/images/generations`
- `GET /openai/v1/responses` WS route
- `GET /v1/responses` WS route
- `GET /responses` WS route
- `GET /backend-api/codex/responses` WS route

For each alias, test:

- valid OpenAI group routes to OpenAI Gateway;
- non-OpenAI group fails closed or routes to the intended non-OpenAI handler, never to OpenAI Gateway;
- missing gateway client token is allowed only when no client token is configured;
- invalid `X-OpenAI-Gateway-Token` returns `401` before scheduling/upstream dial.
- `gateway.openai_core.enabled=false` fails closed on canonical OpenAI Gateway routes, including `/openai/v1/responses`, `/openai/v1/chat/completions`, `/openai/v1/images/generations`, and `GET /openai/v1/responses` WS.
- compatibility routes have explicit expected behavior when core is disabled and must not bypass into legacy direct OpenAI upstream silently.

Run:

```bash
(cd backend && go test ./internal/server/routes -run 'TestOpenAIGatewayRouteMatrix' -count=1)
```

Expected: FAIL until all entry points share client-token handling.

- [ ] **Step 2: Add a shared handler guard**

Ensure `Responses`, `Images`, `ChatCompletions`, and `ResponsesWebSocket` all call the same helper early:

```go
if !h.enforceOptionalGatewayClientAuth(c, reqLog) {
    return
}
```

Chat already has this guard; preserve it and add tests.

Also add a separate core-enabled guard. When `gateway.openai_core.enabled=false`, canonical OpenAI Gateway handlers must reject rather than silently continuing with partial legacy behavior.

- [ ] **Step 3: Make WS error fail hard**

In WS paths, do not ignore `ResolveAccountRuntime` or gateway client-token errors. A bad gateway token must reject before upstream dial.

- [ ] **Step 4: Run route and handler tests**

Run:

```bash
(cd backend && go test ./internal/handler ./internal/server/routes -run 'OpenAIGateway.*(ClientAuth|RouteMatrix|Health|Verify|Images|WS)' -count=1)
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/handler backend/internal/server/routes
git commit -m "feat: enforce openai gateway route auth parity"
```

## Task 5: Verify, Admin, Logs, And Preflight Redaction

**Files:**

- Modify: `backend/internal/service/openai_gateway_core.go`
- Modify: `backend/internal/handler/openai_gateway_handler.go`
- Modify: `backend/internal/repository/http_upstream.go`
- Modify: `backend/internal/service/gemini_messages_compat_service.go` only if shared sanitizer is reused there.
- Modify: `deploy/openai-gateway-preflight.sh`
- Modify: `deploy/OPENAI_GATEWAY_PREFLIGHT.md`
- Test: `backend/internal/handler/openai_gateway_core_handler_test.go`
- Test: `backend/internal/service/openai_gateway_core_test.go`

- [ ] **Step 1: Add failing redaction tests**

Verify:

- `_verify` response has no `egress_proxy_url` by default;
- `_verify` includes `proxy_selected`, `proxy_label` or `proxy_hash`;
- admin snapshot has no `proxy_url`;
- debug logs do not include raw proxy userinfo;
- preflight fails if response contains `access_token`, `refresh_token`, `id_token`, `sk-`, or `scheme://user:pass@host`.

Run:

```bash
(cd backend && go test ./internal/handler ./internal/service -run 'OpenAIGateway.*(Verify|Health|Admin|Redact|Proxy)' -count=1)
```

Expected: FAIL until public shapes are changed.

- [ ] **Step 2: Implement redacted response shapes**

Use the egress redaction helpers from Task 2. Do not return raw config structs in admin status.

- [ ] **Step 3: Harden log sanitizers**

Where proxy URL may appear in debug logs, sanitize:

- username;
- password;
- query params;
- full bearer token values;
- `access_token`, `refresh_token`, `id_token`, `api_key`.

- [ ] **Step 4: Update preflight script**

The script should fail with a clear message when unsafe output is detected:

```bash
jq '.. | objects | keys[]?' "$captured_json" | grep -E '^(access_token|refresh_token|id_token|api_key)$'
grep -E 'sk-[A-Za-z0-9]{12,}|://[^/@:]+:[^/@]+@' "$captured_json"
```

Use `jq` field checks first where available, and scope fallback grep to captured JSON responses rather than the whole script output. Field-name mentions such as a documented denylist are not failures by themselves; unsafe concrete values are failures.

- [ ] **Step 5: Run redaction tests**

Run:

```bash
(cd backend && go test ./internal/handler ./internal/service -run 'OpenAIGateway.*(Verify|Health|Admin|Redact|Proxy)' -count=1)
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/service/openai_gateway_core.go backend/internal/handler/openai_gateway_handler.go backend/internal/repository/http_upstream.go deploy/openai-gateway-preflight.sh deploy/OPENAI_GATEWAY_PREFLIGHT.md
git commit -m "feat: redact openai gateway proxy and token surfaces"
```

## Task 6: Canonical Profile Single Artifact

**Files:**

- Create: `backend/internal/service/openai_gateway_profile.go`
- Test: `backend/internal/service/openai_gateway_profile_test.go`
- Modify: `backend/internal/service/openai_gateway_core.go`
- Modify: `backend/internal/service/openai_gateway_service.go`
- Modify: `backend/internal/service/openai_ws_forwarder.go`
- Modify: `backend/internal/service/openai_ws_v2_passthrough_adapter.go`

- [ ] **Step 1: Add failing canonical profile tests**

Tests must prove there is one artifact for:

- `User-Agent`;
- `version`;
- `originator`;
- `OpenAI-Beta`;
- `X-Stainless-*`;
- WS session/beta headers that OpenAI sees;
- route-specific variant for compatibility Messages bridge where `OpenAI-Beta` / `originator` must be removed.

Run:

```bash
(cd backend && go test ./internal/service -run 'TestOpenAIGatewayCanonicalProfile' -count=1)
```

Expected: FAIL.

- [ ] **Step 2: Create profile artifact**

Add a struct like:

```go
type OpenAIGatewayProfileArtifact struct {
    ID string
    Mode string
    UserAgent string
    Version string
    Originator string
    OpenAIBeta string
    Stainless OpenAIStainlessProfile
    ApplyResponsesHeaders bool
    ApplyWSHeaders bool
}
```

Make `codex_cli_rs/0.104.0` vs `0.125.0` a deliberate config/profile value, not scattered constants. The implementation must not silently mix a `User-Agent` from one version with a `version` header from another.

- [ ] **Step 3: Replace scattered constants**

Search:

```bash
rg -n 'codex_cli_rs/|codexCLIVersion|OpenAI-Beta|originator|X-Stainless' backend/internal/service
```

Replace relevant OpenAI upstream identity writes with profile artifact methods:

```go
profile.ApplyHTTP(req.Header, routeKind)
profile.ApplyWS(headers, routeKind)
```

- [ ] **Step 4: Preserve observe/frozen behavior**

Existing observe/frozen account runtime behavior must still work, but it should populate the new artifact instead of only `UserAgent` / Stainless fields.

- [ ] **Step 5: Run profile tests**

Run:

```bash
(cd backend && go test ./internal/service -run 'TestOpenAIGatewayCanonicalProfile|TestOpenAIGatewayCore' -count=1)
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/service/openai_gateway_profile.go backend/internal/service/openai_gateway_profile_test.go backend/internal/service/openai_gateway_core.go backend/internal/service/openai_gateway_service.go backend/internal/service/openai_ws_forwarder.go backend/internal/service/openai_ws_v2_passthrough_adapter.go
git commit -m "feat: centralize openai gateway canonical profile"
```

## Task 7A: Secret Protector Primitive And Config Gate

**Files:**

- Create: `backend/internal/service/openai_secret_protector.go`
- Test: `backend/internal/service/openai_secret_protector_test.go`
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/service/wire.go`
- Modify generated: `backend/cmd/server/wire_gen.go`
- Modify if present/generated: `backend/cmd/server/wire_gen_test.go`

- [ ] **Step 1: Add failing secret-protector primitive tests**

Cover:

- encrypt/decrypt `access_token`, `refresh_token`, `id_token`, and upstream API-key values;
- keep non-secret profile/account fields plaintext;
- reject invalid key length;
- reject missing key when `production_mode=true` and `require_encrypted_credentials=true`;
- do not include plaintext or ciphertext values in error strings;
- `enc:v1:` prefix round-trips.

Run:

```bash
(cd backend && go test ./internal/service -run 'TestOpenAISecretProtector' -count=1)
```

Expected: FAIL.

- [ ] **Step 2: Implement protector primitive**

Use AES-256-GCM with a dedicated configured key. Store encrypted field values with a prefix:

```text
enc:v1:<base64 nonce+ciphertext>
```

Do not reuse the payment encryption package directly unless its key lifecycle and prefix semantics are acceptable for account credentials. If code is shared, extract a small neutral crypto helper first.

- [ ] **Step 3: Wire protector and regenerate Wire output**

Update `backend/internal/service/wire.go`, then run the Wire command found in Task 0. Stage `backend/cmd/server/wire_gen.go` and generated tests if changed.

- [ ] **Step 4: Run primitive tests**

```bash
(cd backend && go test ./internal/service -run 'TestOpenAISecretProtector' -count=1)
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/service/openai_secret_protector.go backend/internal/service/openai_secret_protector_test.go backend/internal/config/config.go backend/internal/service/wire.go backend/cmd/server/wire_gen.go backend/cmd/server/wire_gen_test.go
git commit -m "feat: add openai secret protector"
```

## Task 7B: Central OpenAI Credential Accessors

**Files:**

- Create: `backend/internal/service/openai_gateway_credentials.go`
- Test: `backend/internal/service/openai_gateway_credentials_test.go`
- Modify: `backend/internal/service/openai_gateway_chat_completions_raw.go`
- Modify: `backend/internal/service/openai_apikey_responses_probe.go`
- Modify: `backend/internal/service/openai_gateway_service.go`
- Modify: `backend/internal/service/openai_gateway_images.go`
- Modify: `backend/internal/service/openai_images.go`
- Modify: `backend/internal/service/openai_images_responses.go`
- Modify: `backend/internal/service/openai_oauth_service.go`
- Modify: `backend/internal/service/openai_ws_forwarder.go`
- Modify: `backend/internal/service/openai_ws_v2_passthrough_adapter.go`
- Modify: `backend/internal/service/openai_token_provider.go`
- Modify if constructors/providers change: `backend/internal/service/wire.go`
- Modify generated if Wire changes: `backend/cmd/server/wire_gen.go`
- Modify if present/generated: `backend/cmd/server/wire_gen_test.go`

- [ ] **Step 1: Add failing central accessor tests**

Cover:

- encrypted API-key credential returns plaintext only through the central accessor;
- encrypted OAuth refresh/access token returns plaintext only through the central accessor;
- direct ciphertext values are never passed to upstream authorization headers;
- legacy plaintext credentials remain readable in local/non-production mode;
- production mode rejects legacy plaintext credential use.

Run:

```bash
(cd backend && go test ./internal/service -run 'TestOpenAIGatewayCredential|TestOpenAI.*APIKey|TestOpenAITokenProvider' -count=1)
```

Expected: FAIL.

- [ ] **Step 2: Implement central accessor**

Add a single OpenAI credential accessor/decorator used by OAuth, API-key, token provider, refresh service, probes, Images, Chat, Responses, and WS. Do not decrypt ad hoc in each call site.

The accessor should expose narrowly typed methods:

```go
OpenAIAPIKey(account *Account) (string, error)
OpenAIRefreshToken(account *Account) (string, error)
OpenAIAccessToken(account *Account) (string, error)
OpenAIClientID(account *Account) (string, error)
```

- [ ] **Step 3: Replace direct API-key readers**

Search and replace OpenAI upstream credential paths:

```bash
rg -n 'GetOpenAIApiKey\\(|GetCredential\\(\"api_key\"\\)|GetCredential\\(\"access_token\"\\)|GetCredential\\(\"refresh_token\"\\)' backend/internal/service
```

Expected after implementation: direct readers remain only inside the central accessor, tests, or non-OpenAI code.

- [ ] **Step 4: Regenerate Wire output if injection changes constructors**

If the central accessor/protector is injected into `OpenAIGatewayService`, `OpenAITokenProvider`, `OpenAIOAuthService`, probes, or WS services through constructors/providers, update `backend/internal/service/wire.go` and run the Wire command recorded in Task 0. Stage generated files.

- [ ] **Step 5: Run accessor tests**

```bash
(cd backend && go test ./internal/service -run 'TestOpenAIGatewayCredential|TestOpenAI.*APIKey|TestOpenAITokenProvider' -count=1)
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/service/openai_gateway_credentials.go backend/internal/service/openai_gateway_credentials_test.go backend/internal/service/openai_gateway_chat_completions_raw.go backend/internal/service/openai_apikey_responses_probe.go backend/internal/service/openai_gateway_service.go backend/internal/service/openai_gateway_images.go backend/internal/service/openai_images.go backend/internal/service/openai_images_responses.go backend/internal/service/openai_oauth_service.go backend/internal/service/openai_ws_forwarder.go backend/internal/service/openai_ws_v2_passthrough_adapter.go backend/internal/service/openai_token_provider.go backend/internal/service/wire.go backend/cmd/server/wire_gen.go backend/cmd/server/wire_gen_test.go
git commit -m "feat: centralize protected openai credential access"
```

## Task 7C: OAuth Import And Refresh Write Encryption

**Files:**

- Modify: `backend/internal/service/openai_oauth_service.go`
- Modify: `backend/internal/service/openai_token_provider.go`
- Modify: `backend/internal/service/token_refresh_service.go`
- Modify: `backend/internal/service/openai_oauth_lifecycle.go`
- Modify tests: `backend/internal/service/openai_oauth_service_refresh_test.go`, `backend/internal/service/openai_token_provider_test.go`, `backend/internal/service/token_refresh_service_test.go`

- [ ] **Step 1: Add failing write-path encryption tests**

Cover:

- OAuth import writes encrypted `access_token`, `refresh_token`, and `id_token`;
- refresh updates preserve encrypted storage;
- no public snapshot contains encrypted ciphertext or plaintext token values.

Run:

```bash
(cd backend && go test ./internal/service -run 'TestOpenAIOAuth.*Credential|TestTokenRefresh.*OpenAI|TestOpenAITokenProvider' -count=1)
```

Expected: FAIL.

- [ ] **Step 2: Encrypt OAuth import and refresh writes**

Credential writers from OAuth import and refresh must write encrypted token fields when protector is configured.

- [ ] **Step 3: Preserve read compatibility**

Credential readers should decrypt `enc:v1:` values and return plaintext only in memory. Legacy plaintext remains readable when not in production gate mode.

- [ ] **Step 4: Run write-path tests**

```bash
(cd backend && go test ./internal/service -run 'TestOpenAIOAuth.*Credential|TestTokenRefresh.*OpenAI|TestOpenAITokenProvider' -count=1)
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/service/openai_oauth_service.go backend/internal/service/openai_token_provider.go backend/internal/service/token_refresh_service.go backend/internal/service/openai_oauth_lifecycle.go backend/internal/service/openai_oauth_service_refresh_test.go backend/internal/service/openai_token_provider_test.go backend/internal/service/token_refresh_service_test.go
git commit -m "feat: encrypt openai oauth credential writes"
```

## Task 7D: Redis Token Cache Secret Decision

**Files:**

- Modify: `backend/internal/service/openai_token_provider.go`
- Test: `backend/internal/service/openai_token_provider_test.go`
- Modify: `backend/internal/config/config.go`
- Modify: `deploy/config.example.yaml`

- [ ] **Step 1: Add failing Redis token cache tests**

Cover current plaintext cache risk:

- cached OpenAI access tokens are encrypted before Redis/cache write; or
- access-token Redis caching is disabled when `production_mode=true` and `require_encrypted_credentials=true`; or
- Redis is explicitly marked as an approved secret store and production validation verifies that decision.

Run:

```bash
(cd backend && go test ./internal/service -run 'TestOpenAITokenProvider.*Cache|TestOpenAI.*RedisSecret' -count=1)
```

Expected: FAIL because current `tokenCache` writes plaintext access tokens.

- [ ] **Step 2: Implement one explicit policy**

Choose exactly one production policy and document it in config:

- encrypt cached OpenAI access tokens with the secret protector before cache write; or
- disable Redis access-token caching for OpenAI in production secret mode; or
- require a configured `redis_token_cache_approved_secret_store=true` with operational documentation.

Do not leave Redis token cache as an implicit exception.

- [ ] **Step 3: Run cache tests**

```bash
(cd backend && go test ./internal/service -run 'TestOpenAITokenProvider.*Cache|TestOpenAI.*RedisSecret' -count=1)
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/service/openai_token_provider.go backend/internal/service/openai_token_provider_test.go backend/internal/config/config.go deploy/config.example.yaml
git commit -m "feat: gate openai redis token cache secrets"
```

## Task 7E: Production Credential Scan And Request Gate

**Files:**

- Modify: `backend/internal/service/openai_gateway_core.go`
- Modify: `backend/internal/service/openai_gateway_credentials.go`
- Test: `backend/internal/service/openai_gateway_core_test.go`
- Test: `backend/internal/service/openai_gateway_credentials_test.go`
- Modify: `deploy/OPENAI_GATEWAY_PREFLIGHT.md`

- [ ] **Step 1: Add failing production gate tests**

Cover:

- production mode detects plaintext OpenAI OAuth credentials;
- production mode detects plaintext OpenAI API-key credentials;
- unsafe credentials make `_health` degraded with a stable code;
- unsafe credentials also block scheduling/upstream credential access, not only health reporting.

Run:

```bash
(cd backend && go test ./internal/service -run 'TestOpenAIGateway.*Credential.*Gate|TestOpenAIGateway.*Health' -count=1)
```

Expected: FAIL.

- [ ] **Step 2: Add production validation**

On production startup, fail validation when:

- encryption is required but no valid key exists;
- existing OpenAI OAuth/API-key credentials contain plaintext secret fields.

If scanning all accounts at config load is not available, add a service startup check and surface it in `_health` as degraded until fixed. Request paths must also fail closed when the account selected for upstream use has unsafe plaintext credentials in production mode.

- [ ] **Step 3: Run production gate tests**

Run:

```bash
(cd backend && go test ./internal/service -run 'TestOpenAIGateway.*Credential.*Gate|TestOpenAIGateway.*Health' -count=1)
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/service/openai_gateway_core.go backend/internal/service/openai_gateway_credentials.go backend/internal/service/openai_gateway_core_test.go backend/internal/service/openai_gateway_credentials_test.go deploy/OPENAI_GATEWAY_PREFLIGHT.md
git commit -m "feat: fail closed on unsafe openai credentials"
```

## Task 8: OAuth Callback Session Deployment Gate

**Files:**

- Modify: `backend/internal/pkg/openai/oauth.go`
- Create: `backend/internal/service/openai_oauth_session_store.go`
- Test: `backend/internal/service/openai_oauth_session_store_test.go`
- Modify: `backend/internal/service/openai_oauth_service.go`
- Modify: `backend/internal/service/wire.go`
- Modify generated: `backend/cmd/server/wire_gen.go`
- Modify if present/generated: `backend/cmd/server/wire_gen_test.go`
- Modify config tests from Task 1 if needed.

- [ ] **Step 1: Add failing session-store tests**

Cover:

- current memory store still works for local mode;
- Redis/shared implementation stores and loads state/verifier/redirect/proxy with TTL;
- `ExchangeCode` deletes or invalidates session after use;
- malformed or expired session returns `OPENAI_OAUTH_SESSION_NOT_FOUND`;
- production mode rejects `memory` unless sticky single callback is explicitly enabled.

Run:

```bash
(cd backend && go test ./internal/pkg/openai ./internal/service -run 'Test.*OAuthSession' -count=1)
```

Expected: FAIL for shared store and production gate behavior.

- [ ] **Step 2: Introduce interface**

In `backend/internal/pkg/openai/oauth.go`:

```go
type OAuthSessionStore interface {
    Set(sessionID string, session *OAuthSession) error
    Get(sessionID string) (*OAuthSession, bool, error)
    Delete(sessionID string) error
    Stop() error
}
```

Adapt memory store to satisfy it while keeping old tests passing.

- [ ] **Step 3: Add Redis/shared store or strict gate**

If a Redis client is already available in `service/wire.go`, implement Redis-backed store with TTL. If not, implement the production gate first and document that production must run sticky single callback until Redis wiring lands.

- [ ] **Step 4: Update OpenAI OAuth service**

`OpenAIOAuthService` should receive the store via constructor. `GenerateAuthURL` must handle store errors. `ExchangeCode` must delete consumed sessions.

- [ ] **Step 5: Regenerate Wire output**

Run the Wire command recorded in Task 0 after constructor/provider changes. Stage generated files.

- [ ] **Step 6: Run session tests**

Run:

```bash
(cd backend && go test ./internal/pkg/openai ./internal/service -run 'Test.*OAuthSession|TestOpenAIOAuthService.*State' -count=1)
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/pkg/openai/oauth.go backend/internal/service/openai_oauth_session_store.go backend/internal/service/openai_oauth_service.go backend/internal/service/wire.go backend/cmd/server/wire_gen.go backend/cmd/server/wire_gen_test.go
git commit -m "feat: gate openai oauth callback session storage"
```

## Task 9: Bucket Concentration, Health, And Canary Stop Conditions

**Files:**

- Modify: `backend/internal/service/openai_gateway_core.go`
- Test: `backend/internal/service/openai_gateway_core_test.go`
- Modify: `deploy/OPENAI_GATEWAY_PREFLIGHT.md`
- Modify: `deploy/openai-gateway-preflight.sh`
- Create: `docs/harness/OPENAI_GATEWAY_LIVE_CANARY_SOR.md`

- [ ] **Step 1: Add failing health tests**

Cover:

- health reports per-bucket account count;
- health/admin warnings appear when account count exceeds `bucket_warn_account_threshold`;
- canary stop condition appears when an OAuth account resolves to direct egress while production mode is enabled.

Run:

```bash
(cd backend && go test ./internal/service -run 'TestOpenAIGateway.*(Health|Bucket|Canary)' -count=1)
```

Expected: FAIL until warning fields exist.

- [ ] **Step 2: Implement warnings**

Add stable warning codes:

- `bucket_concentration_high`
- `direct_egress_in_production`
- `missing_egress_bucket`
- `disabled_egress_bucket`
- `credential_storage_not_production_safe`
- `oauth_session_topology_not_production_safe`

- [ ] **Step 3: Create SOR**

`docs/harness/OPENAI_GATEWAY_LIVE_CANARY_SOR.md` must include:

- blocked item: real GPT/OpenAI OAuth account unavailable;
- required canary account IDs;
- bucket assignment evidence;
- HTTP Responses evidence;
- WS evidence;
- refresh evidence;
- explicit TLS/transport stance: "OpenAI Gateway makes no TLS/JA3/JA4/ALPN/HTTP2/WS transport camouflage claim in this phase" unless a later live evidence task is approved;
- log redaction evidence;
- rollback evidence;
- final signoff checklist.

- [ ] **Step 4: Run health/preflight tests**

Run:

```bash
(cd backend && go test ./internal/service -run 'TestOpenAIGateway.*(Health|Bucket|Canary)' -count=1)
bash -n deploy/openai-gateway-preflight.sh
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/service/openai_gateway_core.go backend/internal/service/openai_gateway_core_test.go deploy/OPENAI_GATEWAY_PREFLIGHT.md deploy/openai-gateway-preflight.sh docs/harness/OPENAI_GATEWAY_LIVE_CANARY_SOR.md
git commit -m "docs: add openai gateway canary gates"
```

## Task 10: Final Verification And Review Gate

**Files:**

- No planned source changes unless verification finds issues.

- [ ] **Step 1: Run targeted OpenAI Gateway tests**

```bash
(cd backend && go test ./internal/config ./internal/handler ./internal/server/routes ./internal/service -run 'OpenAI|OpenAIGateway|OAuthSession|SecretProtector' -count=1)
```

Expected: PASS.

- [ ] **Step 2: Run broader backend tests likely affected**

```bash
(cd backend && go test ./internal/service ./internal/handler ./internal/server/routes ./internal/pkg/openai ./internal/repository -count=1)
```

Expected: PASS.

- [ ] **Step 3: Run static checks available in repo**

Use the repo's existing commands. Start with:

```bash
(cd backend && go test ./... -count=1)
```

Expected: PASS, unless known unrelated failures are documented with exact package/test names.

- [ ] **Step 4: Search for remaining unsafe paths**

```bash
rg -n 'egress_proxy_url' backend/internal/service/openai_gateway_core.go backend/internal/handler deploy
rg -n 'account\\.Proxy\\.URL\\(\\)' backend/internal/service/openai*.go
rg -n 'ResolveEgressProxyURL\\(' backend/internal/service/openai*.go
rg -n 'slog\\.|zap\\.' backend/internal/service/openai*.go backend/internal/repository/http_upstream.go | rg 'proxy|token|authorization|access|refresh|id_token'
rg -n 'GetOpenAIApiKey\\(|GetCredential\\(\"api_key\"\\)|GetCredential\\(\"access_token\"\\)|GetCredential\\(\"refresh_token\"\\)' backend/internal/service/openai*.go
```

Expected:

- no OpenAI upstream path directly calls `account.Proxy.URL()`;
- no public response field named `egress_proxy_url`;
- `ResolveEgressProxyURL` remains only in deprecated compatibility wrappers or tests that explicitly verify migration behavior;
- direct OpenAI credential readers remain only inside `openai_gateway_credentials.go`, tests, or clearly non-upstream parsing code;
- logging sites that mention proxy/token-like data use sanitizer helpers before output.

- [ ] **Step 5: Dispatch implementation review**

Review context must include:

- spec: `OPENAI_GATEWAY_V03_ALIGNMENT_DESIGN.md`
- plan: `docs/superpowers/plans/2026-05-06-openai-gateway-v03-hardening.md`
- base SHA before implementation
- head SHA after implementation
- focus areas: egress fail-closed, raw proxy/token leakage, secret storage gate, OAuth session topology, canonical profile, route matrix, WS/HTTP parity.

- [ ] **Step 6: Fix review findings**

Fix Critical/Important findings before declaring ready. Re-run relevant tests and request review again if the changes are substantive.

- [ ] **Step 7: Final commit**

```bash
git status --short --branch --untracked-files=all
```

Expected: clean working tree on the implementation branch.

## Suggested Review Checkpoints During Execution

Checkpoint A after Tasks 1-2:

- Config production gates.
- Typed egress resolver.
- Fail-closed behavior and fallback flags.

Checkpoint B after Tasks 3A-5:

- All upstream paths use resolver.
- Route/client token parity.
- `_verify`, admin, logs, preflight redaction.

Checkpoint C after Tasks 6-8, including Tasks 7A-7E:

- Canonical profile source of truth.
- Secret storage gate.
- Central OpenAI credential accessors and Redis token-cache decision.
- OAuth session topology gate.

Checkpoint D after Tasks 9-10:

- Canary readiness.
- Full verification.
- Remaining production blockers.

## Acceptance Criteria

- `gateway.openai_core.production_mode=true` cannot start or report healthy when egress, credential storage, or OAuth session mode is unsafe.
- Every OpenAI upstream request path uses the same typed egress resolver.
- Missing/disabled/unknown buckets fail closed in production mode.
- `_verify`, admin status, logs, and preflight output do not expose raw proxy URLs or upstream secrets by default.
- Responses, Chat, Images, Messages bridge, WS, OAuth refresh/exchange, and privacy paths obey the same egress and token secrecy rules.
- Canonical profile has one artifact covering HTTP and WS visible identity fields.
- Route matrix tests cover canonical and compatibility aliases with OpenAI/non-OpenAI groups and valid/missing/bad gateway client token.
- Route matrix tests cover `gateway.openai_core.enabled=false` fail-closed behavior.
- OpenAI account secrets are encrypted at rest for production, or production mode is blocked.
- OpenAI API-key credentials and OAuth credentials are both read through central protected accessors.
- Redis token cache is encrypted, disabled in production secret mode, or explicitly approved as a secret store by config and runbook.
- OAuth callback/session behavior is shared-store safe, or production mode explicitly requires sticky single callback.
- Pre-account OAuth login/exchange is bound to an egress bucket or explicitly blocked by production policy.
- SOR/preflight state clearly that this phase makes no TLS/JA3/JA4/ALPN transport camouflage claim unless a later evidence task is approved.
- Live OAuth canary remains blocked in SOR until real GPT/OpenAI OAuth accounts are available.
