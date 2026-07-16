# OpenAI Native Search Gateway Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Proxy Codex's native `alpha/search` contract through the authenticated OpenAI OAuth account pool and make native Search a mandatory, privacy-safe production deployment gate.

**Architecture:** Two explicit Search routes enter a dedicated non-streaming handler that reuses Responses admission, scheduling, concurrency, failover, and error classification. A focused service forwards opaque request bytes to ChatGPT's native Search endpoint with a Search-specific canonical identity profile, validates only the response shape, and relays valid bytes unchanged. A pre-frontend namespace guard prevents all `/alpha` API paths from falling into the SPA, while deployment probes validate both root and `/v1` paths without retaining Search payloads.

**Tech Stack:** Go 1.24, Gin, gjson, existing OpenAI scheduler/token/TLS services, Bash, curl, Python 3 probe validators.

## Global Constraints

- Native Search upstream is fixed to `https://chatgpt.com/backend-api/codex/alpha/search` in production.
- Only healthy OpenAI OAuth accounts may satisfy `OpenAIEndpointCapabilitySearch`; the capability is not user-editable.
- Preserve valid request and response bytes; never log commands, queries, results, raw bodies, tokens, or ChatGPT account IDs.
- Search creates no usage/billing record because the native response has no Responses token usage.
- `INSUFFICIENT_BALANCE` never retries the same account; HTTP 502/503/504 never create a global runtime block.
- Caller cancellation never fails over; transient upstream transport failures may fail over.
- Candidate and public verification must probe both `/alpha/search` and `/v1/alpha/search` without retaining raw Search request or response data.
- Production deployment uses only `deploy/hot-deploy.sh`, retains the prior app container, and never restarts or mutates PostgreSQL or Redis.

---

### Task 1: Search Capability And Canonical Identity

**Files:**
- Modify: `backend/internal/service/account.go`
- Modify: `backend/internal/service/openai_images_test.go`
- Modify: `backend/internal/service/openai_gateway_profile.go`
- Modify: `backend/internal/service/openai_gateway_profile_test.go`

**Interfaces:**
- Produces: `OpenAIEndpointCapabilitySearch OpenAIEndpointCapability = "search"`.
- Produces: `OpenAIGatewayProfileRouteNativeSearch OpenAIGatewayProfileRouteKind = "native_search"`.
- Produces: OAuth-only behavior from `(*Account).SupportsOpenAIEndpointCapability`.

- [ ] **Step 1: Write failing capability tests**

Add table cases proving Search is true for OpenAI OAuth even when `openai_capabilities` omits it, and false for OpenAI API-key, upstream, non-OpenAI, and nil accounts:

```go
func TestAccountSupportsOpenAIEndpointCapability_SearchIsInternalOAuthOnly(t *testing.T) {
    tests := []struct {
        name    string
        account *Account
        want    bool
    }{
        {"oauth", &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, true},
        {"oauth explicit list cannot disable search", &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"openai_capabilities": []any{"chat_completions"}}}, true},
        {"api key", &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, false},
        {"upstream", &Account{Platform: PlatformOpenAI, Type: AccountTypeUpstream}, false},
        {"other platform", &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth}, false},
        {"nil", nil, false},
    }
    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            require.Equal(t, tc.want, tc.account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilitySearch))
        })
    }
}
```

- [ ] **Step 2: Run capability tests and verify RED**

Run: `cd backend && go test ./internal/service -run 'TestAccountSupportsOpenAIEndpointCapability_SearchIsInternalOAuthOnly' -count=1`

Expected: compile failure because `OpenAIEndpointCapabilitySearch` does not exist.

- [ ] **Step 3: Implement the internal capability**

Add the constant and make Search an early inherent check before the editable capability list:

```go
const OpenAIEndpointCapabilitySearch OpenAIEndpointCapability = "search"

func (a *Account) SupportsOpenAIEndpointCapability(capability OpenAIEndpointCapability) bool {
    if a == nil || a.Platform != PlatformOpenAI {
        return false
    }
    if capability == OpenAIEndpointCapabilitySearch {
        return a.Type == AccountTypeOAuth
    }
    // existing editable chat/embeddings behavior remains unchanged
}
```

- [ ] **Step 4: Write failing Search profile test**

```go
func TestOpenAIGatewayCanonicalProfile_NativeSearchClearsResponsesHeaders(t *testing.T) {
    profile := &OpenAIGatewayCanonicalProfile{
        ProfileID: "search-profile", Mode: OpenAIGatewayProfileModeFixed,
        UserAgent: "codex_cli_rs/0.144.2", Version: "0.144.2",
        StainlessLang: "rust", StainlessPackageVersion: "0.144.2",
        StainlessOS: "macOS", StainlessArch: "arm64",
        StainlessRuntime: "native", StainlessRuntimeVersion: "0.144.2",
    }
    artifact := BuildOpenAIGatewayProfileArtifact(profile, OpenAIGatewayProfileRouteNativeSearch, OpenAIGatewayProfileArtifactOptions{RequestedOriginator: "codex_cli_rs", IsOfficialClient: true})
    headers := http.Header{"Openai-Beta": {"responses=experimental"}, "Version": {"0.144.2"}}
    artifact.ApplyHTTP(headers)
    require.Equal(t, "codex_cli_rs", headers.Get("originator"))
    require.Empty(t, headers.Get("OpenAI-Beta"))
    require.Empty(t, headers.Get("Version"))
}
```

- [ ] **Step 5: Run profile test and verify RED**

Run: `cd backend && go test ./internal/service -run 'TestOpenAIGatewayCanonicalProfile_NativeSearch' -count=1`

Expected: compile failure because the Search route kind does not exist.

- [ ] **Step 6: Implement Search profile behavior and verify GREEN**

Add the route kind and configure it with `ClearOpenAIBeta=true` and the normal official-client originator resolution. Run:

`cd backend && go test ./internal/service -run 'TestAccountSupportsOpenAIEndpointCapability|TestOpenAIGatewayCanonicalProfile' -count=1`

Expected: PASS.

### Task 2: Native Search Upstream Forwarding

**Files:**
- Create: `backend/internal/service/openai_native_search.go`
- Create: `backend/internal/service/openai_native_search_test.go`
- Modify: `backend/internal/service/openai_upstream_transport_error_handle_test.go`

**Interfaces:**
- Produces: `OpenAINativeSearchResponse{StatusCode int, Headers http.Header, Body []byte}`.
- Produces: `(*OpenAIGatewayService).ForwardNativeSearch(context.Context, *gin.Context, *Account, http.Header, []byte) (*OpenAINativeSearchResponse, error)`.
- Consumes: `GetAccessToken`, `ResolveAccountRuntime`, `BuildOpenAIGatewayProfileArtifact`, `resolveAndSetOpenAIChatGPTAccountHeaders`, `sendOpenAIHTTPRequest`, `handleOpenAIUpstreamTransportError`, and `handleOpenAIAccountUpstreamError`.

- [ ] **Step 1: Write failing contract and byte-preservation tests**

Use an `HTTPUpstream` spy and OAuth account fixture to assert the exact upstream path, original body bytes, refreshed bearer token, `chatgpt-account-id`, canonical `originator`/UA, absence of inbound authorization and Responses beta, and unchanged valid response bytes:

```go
func TestForwardNativeSearch_ForwardsOAuthNativeContractUnchanged(t *testing.T) {
    requestBody := []byte(`{"id":"search-session","model":"gpt-5.4","commands":{}}`)
    responseBody := []byte("{\n  \"encrypted_output\": \"opaque\", \"output\": \"result\"\n}")
    svc, upstream := newNativeSearchServiceFixture(t, http.StatusOK, responseBody)
    account := nativeSearchOAuthAccount()

    got, err := svc.ForwardNativeSearch(context.Background(), nil, account, http.Header{"Authorization": {"Bearer client-secret"}, "User-Agent": {"codex_cli_rs/0.144.2"}, "Originator": {"codex_cli_rs"}}, requestBody)

    require.NoError(t, err)
    require.Equal(t, requestBody, upstream.RequestBody())
    require.Equal(t, "/backend-api/codex/alpha/search", upstream.Request().URL.Path)
    require.Equal(t, "Bearer refreshed-token", upstream.Request().Header.Get("Authorization"))
    require.NotEqual(t, "Bearer client-secret", upstream.Request().Header.Get("Authorization"))
    require.Empty(t, upstream.Request().Header.Get("OpenAI-Beta"))
    require.Equal(t, responseBody, got.Body)
}
```

- [ ] **Step 2: Write failing response-shape table tests**

Table-test empty body, HTML, `[]`, `{}`, `{"output":null}`, numeric output, and invalid `encrypted_output`; each must return an `UpstreamFailoverError` with 502 and JSON error bytes. Also prove absent/null/string `encrypted_output` and empty/non-empty string `output` are structurally valid and preserve bytes.

- [ ] **Step 3: Write failing upstream error and cancellation tests**

Cover:

```go
func TestForwardNativeSearch_InsufficientBalanceDisablesWithoutSameAccountRetry(t *testing.T)
func TestForwardNativeSearch_Transient502DoesNotBlockResponsesScope(t *testing.T)
func TestForwardNativeSearch_ContextCanceledDoesNotFailOver(t *testing.T)
func TestForwardNativeSearch_CallerDeadlineDoesNotFailOver(t *testing.T)
func TestForwardNativeSearch_TransportTimeoutWithoutCallerDeadlineCanFailOver(t *testing.T)
func TestForwardNativeSearch_TokenFailureSendsNoRequest(t *testing.T)
```

The cancellation tests must cancel/expire the passed context so `ctx.Err()` is non-nil; an isolated `context.DeadlineExceeded` returned by the transport while the caller context remains active must still become a failover error.

- [ ] **Step 4: Run forwarding tests and verify RED**

Run: `cd backend && go test ./internal/service -run 'TestForwardNativeSearch' -count=1`

Expected: compile failure because `ForwardNativeSearch` and its response type do not exist.

- [ ] **Step 5: Implement minimal forwarding**

Implement a fixed, test-overridable endpoint and bounded response parsing:

```go
var chatGPTNativeSearchURL = "https://chatgpt.com/backend-api/codex/alpha/search"

type OpenAINativeSearchResponse struct {
    StatusCode int
    Headers    http.Header
    Body       []byte
}

func (s *OpenAIGatewayService) ForwardNativeSearch(ctx context.Context, c *gin.Context, account *Account, clientHeaders http.Header, body []byte) (*OpenAINativeSearchResponse, error) {
    if account == nil || !account.IsOpenAIOAuth() {
        return nil, errors.New("native Search requires an OpenAI OAuth account")
    }
    token, _, err := s.GetAccessToken(ctx, account)
    if err != nil { return nil, fmt.Errorf("get native Search access token: %w", err) }
    req, err := s.buildNativeSearchRequest(ctx, account, clientHeaders, body, token)
    if err != nil { return nil, err }
    resp, err := s.sendOpenAIHTTPRequest(ctx, c, req, account)
    if err != nil {
        if ctx != nil && ctx.Err() != nil { return nil, ctx.Err() }
        return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
    }
    defer resp.Body.Close()
    // read with configured bounded helper; classify non-2xx with endpoint "search";
    // validate only successful native shape; return original valid bytes.
}
```

Build headers from an explicit identity allowlist, resolve shadow credentials for ChatGPT account headers, apply the Search profile after account overrides, overwrite authorization last, and call `enforceCodexIdentityHeaders` last. Never append raw response bodies to Ops logs.

- [ ] **Step 6: Verify forwarding GREEN**

Run: `cd backend && go test ./internal/service -run 'TestForwardNativeSearch|TestAccountSupportsOpenAIEndpointCapability|TestOpenAIGatewayCanonicalProfile' -count=1`

Expected: PASS.

### Task 3: Search Handler Admission, Scheduling, And Failover

**Files:**
- Create: `backend/internal/handler/openai_native_search.go`
- Create: `backend/internal/handler/openai_native_search_test.go`

**Interfaces:**
- Produces: `(*OpenAIGatewayHandler).NativeSearch(*gin.Context)`.
- Produces: `parseOpenAINativeSearchEnvelope([]byte) (id string, model string, err error)`.
- Consumes: `OpenAIEndpointCapabilitySearch`, `ForwardNativeSearch`, Responses user/account slot helpers, entity quota admission, billing cache, scheduler hooks, and existing failover error writers.

- [ ] **Step 1: Write failing envelope tests**

Test valid missing commands, valid `{}`, arbitrary object commands, and rejection of empty body, array/null/scalar top level, empty/missing `id`, empty/missing `model`, and non-object commands. Assertions use the pure parser and never inspect/log command values.

- [ ] **Step 2: Run parser tests and verify RED**

Run: `cd backend && go test ./internal/handler -run 'TestParseOpenAINativeSearchEnvelope' -count=1`

Expected: compile failure because the parser does not exist.

- [ ] **Step 3: Implement the minimal parser**

Use `gjson.ValidBytes`, require a trimmed `{` prefix, extract only `id` and `model`, and if `commands` exists require `gjson.JSON` with a trimmed `{` raw prefix. Return stable validation errors without including raw values.

- [ ] **Step 4: Write failing handler lifecycle tests**

Build real `OpenAIGatewayService` fixtures with fake account repo, HTTP upstream, concurrency cache, billing cache, and entity quota service. Cover success plus:

```go
func TestNativeSearch_BillingRecheckedAfterUserWait(t *testing.T)
func TestNativeSearch_EntityQuotaRejectionSkipsScheduler(t *testing.T)
func TestNativeSearch_SchedulerFailureReleasesUserAndEntity(t *testing.T)
func TestNativeSearch_FailoverReleasesEachAccountLease(t *testing.T)
func TestNativeSearch_CancellationReleasesAllLeasesWithoutSwitch(t *testing.T)
func TestNativeSearch_UsesDeterministicSessionHashWithoutLoggingID(t *testing.T)
```

The failover case uses two OAuth accounts: the first returns 502, the second returns valid native JSON. Assert one user acquire/release, one entity acquire/release, and one account acquire/release per attempt.

- [ ] **Step 5: Run lifecycle tests and verify RED**

Run: `cd backend && go test ./internal/handler -run 'TestNativeSearch' -count=1`

Expected: compile failure because `NativeSearch` does not exist.

- [ ] **Step 6: Implement the non-streaming handler**

Follow this fixed order:

```go
body -> validate -> user slot -> subscription/billing recheck -> entity quota ->
session hash from SHA-256(id) -> scheduler(Search, HTTP) -> account slot ->
ForwardNativeSearch -> release attempt slot -> failover or write response
```

Use `reqStream=false`, `streamStarted=false`, a per-request excluded account map, existing `maxAccountSwitches`, schedule success/failure metrics, and safe end-to-end response headers. Do not call `RecordUsage` or persist Search response data.

- [ ] **Step 7: Verify handler GREEN**

Run: `cd backend && go test ./internal/handler -run 'TestParseOpenAINativeSearchEnvelope|TestNativeSearch' -count=1`

Expected: PASS.

### Task 4: Explicit Routes And SPA Namespace Isolation

**Files:**
- Create: `backend/internal/server/middleware/native_search_namespace.go`
- Create: `backend/internal/server/middleware/native_search_namespace_test.go`
- Modify: `backend/internal/server/router.go`
- Modify: `backend/internal/server/routes/gateway.go`
- Modify: `backend/internal/server/routes/gateway_test.go`
- Modify: `backend/internal/web/embed_on.go`
- Modify: `backend/internal/web/embed_test.go`

**Interfaces:**
- Produces: `middleware.NativeSearchNamespaceGuard() gin.HandlerFunc`.
- Consumes: `(*OpenAIGatewayHandler).NativeSearch`.

- [ ] **Step 1: Write failing namespace guard tests**

Assert only the two exact POST paths call the next handler; GET/PUT/DELETE on exact paths and every boundary path `/alpha`, `/alpha/`, `/alpha/x`, `/v1/alpha`, `/v1/alpha/`, `/v1/alpha/x` returns status 404, OpenAI JSON, and `application/json`. Assert `/alphabet` and `/v1/alphabet` are not captured.

- [ ] **Step 2: Write failing embedded frontend bypass test**

Add `/alpha`, `/alpha/`, and `/alpha/search` to `TestFrontendServer_Middleware/skips_api_routes`, and assert a registered handler runs rather than returning `text/html`.

- [ ] **Step 3: Write failing route registration/platform tests**

In `gateway_test.go`, assert POST on both Search paths reaches the OpenAI handler for an OpenAI group, neither path is 404, non-OpenAI groups receive the OpenAI platform 404, and missing group uses the OpenAI error envelope.

- [ ] **Step 4: Run routing tests and verify RED**

Run: `cd backend && go test ./internal/server/middleware ./internal/server/routes ./internal/web -run 'NativeSearch|alpha' -count=1`

Expected: failures because the guard, bypass, and routes are absent.

- [ ] **Step 5: Implement namespace guard before the frontend**

Register `r.Use(middleware2.NativeSearchNamespaceGuard())` before `FrontendServer.Middleware()`. Boundary detection must be exact `/alpha` or prefix `/alpha/`, and exact `/v1/alpha` or prefix `/v1/alpha/`; valid exact POST requests call `c.Next()`, all other namespace requests call the OpenAI error writer and abort.

- [ ] **Step 6: Register both explicit route chains**

In `RegisterGatewayRoutes`, outside the default `/v1` group, add:

```go
r.POST("/alpha/search", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm,
    apiKeyAuthWithAugmentBearer, requireGroupOpenAI,
    openAIGatewayHandler(h.OpenAIGateway.NativeSearch))
r.POST("/v1/alpha/search", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm,
    gin.HandlerFunc(v1GatewayAuth), requireGroupOpenAI,
    openAIGatewayHandler(h.OpenAIGateway.NativeSearch))
```

Update `shouldBypassEmbeddedFrontend` with exact `/alpha` and `/alpha/` prefix handling.

- [ ] **Step 7: Verify routing GREEN**

Run: `cd backend && go test ./internal/server/middleware ./internal/server/routes ./internal/web -count=1`

Expected: PASS.

### Task 5: Privacy-Safe Deployment Search Gates

**Files:**
- Modify: `deploy/lib/hot-deploy-common.sh`
- Modify: `deploy/tests/fixtures/fake-bin/curl`
- Modify: `deploy/tests/test-hot-deploy.sh`
- Modify: `deploy/HOT_DEPLOY.md`

**Interfaces:**
- Produces: `probe_native_search_path BASE_URL SCOPE PATH`.
- Produces: `probe_native_search_pair BASE_URL SCOPE`.
- Changes: `probe_api_pair` runs Responses, Compact, and both Search paths.

- [ ] **Step 1: Write failing probe validation tests**

Extend the fake curl with `native-search-root` and `native-search-v1` modes. Add cases for HTML, incorrect Content-Type, malformed JSON, missing/null/non-string output, invalid encrypted output, and root/v1 failures in direct and public scope. Assert each fails before `DEPLOYMENT SUCCEEDED` and preserves the previous Caddy target.

- [ ] **Step 2: Write failing artifact privacy tests**

Have the fake Search response contain `SEARCH_OUTPUT_SENTINEL` and `SEARCH_ENCRYPTED_SENTINEL`; after success and failure, recursively scan stdout, stderr, `STATE_DIR`, fake curl log, and Caddy artifacts. Assert neither sentinel, the fixed request body, nor `production-smoke-secret` appears.

- [ ] **Step 3: Run deploy tests and verify RED**

Run: `make -C deploy test-hot-deploy`

Expected: Search probe assertions fail because Search is not part of the deployment gate.

- [ ] **Step 4: Implement streaming Search probes**

Generate the fixed request on stdin, pipe the raw response directly into Python validation, and retain only status/media-type/verdict metadata. Do not use `${STATE_DIR}/${scope}-${kind}-response.json` for Search. The validator requires a 2xx status, JSON media type, top-level object, non-empty string `output`, and absent/null/string `encrypted_output`.

Call both paths for direct candidate and public base URLs after Responses and Compact. Ensure failure returns nonzero before cutover for direct probes and triggers the existing rollback transaction for public probes.

- [ ] **Step 5: Document and verify deploy gates**

Update `HOT_DEPLOY.md` to state that Search, Responses, and streaming Compact are mandatory and that Search bodies are never retained. Run:

`make -C deploy test-hot-deploy`

Expected: all tests pass and final output reports zero failures.

### Task 6: Full Verification, Review, And Production Rollout

**Files:**
- Verify: `backend/internal/service/account.go`
- Verify: `backend/internal/service/openai_gateway_profile.go`
- Verify: `backend/internal/service/openai_native_search.go`
- Verify: `backend/internal/handler/openai_native_search.go`
- Verify: `backend/internal/server/middleware/native_search_namespace.go`
- Verify: `backend/internal/server/router.go`
- Verify: `backend/internal/server/routes/gateway.go`
- Verify: `backend/internal/web/embed_on.go`
- Verify: `deploy/lib/hot-deploy-common.sh`
- Verify: `deploy/tests/test-hot-deploy.sh`
- Constraint: do not change PostgreSQL/Redis configuration or data.

**Interfaces:**
- Consumes: complete Search gateway and supported hot-deploy entry point.
- Produces: live native Search on both public paths with the prior app retained as rollback.

- [ ] **Step 1: Run focused race and regression suites**

Run:

```bash
cd backend
go test ./internal/service ./internal/handler ./internal/server/middleware ./internal/server/routes ./internal/web -count=1
go test -race ./internal/service ./internal/handler -run 'NativeSearch|OpenAIEndpointCapability|OpenAIGatewayCanonicalProfile' -count=1
```

Expected: PASS with no race reports.

- [ ] **Step 2: Run full project verification**

Run:

```bash
cd backend && go test ./... -count=1
cd backend && go build -tags embed ./cmd/server
make -C deploy test-hot-deploy
git diff --check
```

Expected: all commands exit 0.

- [ ] **Step 3: Perform independent code review**

Review the complete immutable diff for Critical/Important issues across auth, group isolation, scheduling, lease release, OAuth refresh, header privacy, cancellation, failover, namespace ordering, and deployment rollback. Do not deploy until the gate is `APPROVED: 0 Critical / 0 Important`.

- [ ] **Step 4: Build the Linux candidate artifact**

Build the same `linux/amd64` embedded server artifact used by production, create a new image tag and candidate container name, and record artifact/image hashes. Do not replace or stop the active/rollback containers.

- [ ] **Step 5: Execute the supported hot deploy**

Run `deploy/hot-deploy.sh` with the new image, candidate container, production smoke API key, public base URL, and required Search/Responses/Compact probes. Completion requires the exact line `DEPLOYMENT SUCCEEDED`.

- [ ] **Step 6: Verify live behavior and infrastructure continuity**

Verify:

```text
POST /alpha/search       -> 200 application/json, output string
POST /v1/alpha/search    -> 200 application/json, output string
unknown /alpha paths     -> JSON 404, never HTML
POST /responses          -> normal success
POST /responses compact  -> streaming response.completed with status completed
Caddy Admin API          -> new candidate upstream
PostgreSQL and Redis     -> original containers/volumes healthy and unchanged
prior app container      -> retained as immediate rollback target
```

Finally trigger Codex's web tool through the production URL and confirm the original session no longer reports `failed to decode search response`.
