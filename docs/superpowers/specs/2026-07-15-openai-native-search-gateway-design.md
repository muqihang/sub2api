# OpenAI Native Search Gateway Design

## Context

Codex CLI `0.144.2` implements its web tool by posting a typed request to
`alpha/search` relative to the configured model-provider `base_url`. The current
production provider points at Sub2API. Sub2API does not register that route, so
the frontend SPA fallback returns HTTP 200 with `text/html`. Codex then attempts
to decode the leading `<` as JSON and reports:

```text
failed to decode search response: expected value at line 1 column 1
```

Production evidence confirms that the preceding `/responses` call succeeds and
that an OpenAI OAuth account can call
`https://chatgpt.com/backend-api/codex/alpha/search` directly. The upstream
returns the native Codex search contract with `encrypted_output` and `output`.

## Goals

- Support OpenAI native Codex Search through Sub2API without third-party search
  emulation.
- Register both `POST /alpha/search` and `POST /v1/alpha/search` for provider
  base URLs with and without a `/v1` suffix.
- Reuse the existing API-key authentication, group assignment, billing
  eligibility, OpenAI account scheduler, account concurrency, OAuth refresh,
  proxy/TLS policy, failover, and runtime error classification.
- Restrict native Search scheduling to healthy OpenAI OAuth accounts.
- Preserve the request and successful upstream response contracts byte-for-byte
  except for hop-by-hop headers.
- Return JSON errors for every Search failure; never allow an `/alpha/*` API
  request to fall through to the SPA.
- Add local and production probes that validate the response is JSON and
  contains the native `output` field.

## Non-Goals

- Do not translate Search into Brave, Tavily, or a model-generated summary.
- Do not decrypt, interpret, cache, or persist `encrypted_output`.
- Do not log search queries, search results, full request bodies, tokens, or
  ChatGPT account identifiers.
- Do not add native Search to OpenAI API-key upstream accounts until an official
  upstream contract is independently verified for those accounts.
- Do not invent token usage or charge records because the native Search response
  does not expose Responses token usage.

## Architecture

### Routing and authentication

The embedded frontend bypass list includes the complete root `/alpha` namespace
(`/alpha`, `/alpha/`, and descendants) before any Search route is registered.
The `/v1/` namespace is already bypassed. A namespace guard runs before the
embedded frontend middleware and permits only `POST /alpha/search` and
`POST /v1/alpha/search`; every other method or path in `/alpha`, `/alpha/`,
`/alpha/*`, `/v1/alpha`, `/v1/alpha/`, or `/v1/alpha/*` is terminated there
with an OpenAI-compatible JSON 404. The guard avoids a Gin catch-all route, so
it cannot conflict with either static Search route.

The two static routes use explicit middleware chains because the root and
`/v1` gateway groups are not interchangeable:

- `/alpha/search` uses the root Responses body limit, client request ID, ops
  error logger, endpoint normalization, Augment-compatible bearer API-key
  authentication, OpenAI group assignment, and the OpenAI platform gate.
- `/v1/alpha/search` uses the `/v1` body limit, client request ID, ops error
  logger, endpoint normalization, `v1GatewayAuth`, OpenAI group assignment, and
  the same OpenAI platform gate. It is registered as an explicit root route,
  outside the Anthropic-default `/v1` route group.

The handler therefore always receives a valid authenticated user and an
assigned OpenAI group. Search never inherits an Anthropic-default group error
envelope and never reaches the SPA fallback.

### Request validation

The handler reads the body through the existing bounded request reader. It
requires:

- a valid JSON object;
- a non-empty string `id`;
- a non-empty string `model`.

When `commands` is present it must be a JSON object, but both an absent field
and an empty object are valid. Codex 0.144.2 serializes empty tool arguments as
`commands: {}`, and the upstream native request type makes the field optional.

The handler does not parse or reconstruct individual command payloads. After
the minimal envelope checks, it forwards the original bytes and leaves command
name validation to the native upstream. New native Search commands therefore
remain compatible without a gateway release.

### Account scheduling

`OpenAIEndpointCapabilitySearch` is an internal scheduler capability. It is
inherently supported only by OpenAI OAuth accounts and is not controlled by the
user-editable endpoint-capability list.

The Search handler uses `SelectAccountWithSchedulerForCapability` with:

- the authenticated API key's group;
- the request model;
- HTTP transport;
- `OpenAIEndpointCapabilitySearch`;
- the per-request excluded-account set.

Search does not use `previous_response_id`. A deterministic session hash is
derived from the request `id` so repeated operations can retain normal scheduler
locality without logging the identifier.

Search is a non-streaming request and follows the Responses admission sequence:

1. Acquire the authenticated user's concurrency slot.
2. Re-read the subscription and re-check billing eligibility after any wait.
3. Admit the OpenAI entity quota.
4. Select a Search-capable account.
5. Acquire that account's HTTP concurrency lease.
6. Forward one upstream attempt and release that account lease before retry or
   failover.

The user slot and entity quota lease span the complete request, including all
failover attempts, and are released on success, validation failure after
admission, billing failure, scheduler failure, upstream error, cancellation,
and panic. Each selected account has its own attempt-scoped lease and no lease
survives a switch to the next account.

### Native upstream forwarding

A focused `ForwardNativeSearch` service method:

1. Requires an OpenAI OAuth account.
2. Gets a current token through `OpenAITokenProvider`, including managed refresh
   and refresh-failure state updates.
3. Posts the original body to
   `https://chatgpt.com/backend-api/codex/alpha/search`.
4. Applies the canonical Codex identity headers and ChatGPT account header, then
   sends through `sendOpenAIHTTPRequest` so configured proxy resolution,
   `DoWithTLS`, and the OpenAI HTTP fingerprint policy remain centralized.
   Search has a dedicated gateway profile route kind and does not inherit the
   Responses-only `OpenAI-Beta: responses=experimental` header.
5. Copies only safe end-to-end response headers and returns the upstream body
   unchanged.

The upstream URL is a package variable in tests so a local HTTP server can
capture and verify requests. Production has no configurable arbitrary Search
URL.

### Errors and failover

- Malformed client input returns OpenAI-compatible JSON 400.
- Authentication, group, billing, and concurrency failures use existing gateway
  envelopes and status codes.
- Token refresh failures fail closed before an upstream request.
- Upstream HTTP failures call `handleOpenAIAccountUpstreamError` with endpoint
  scope `"search"`; Search learned state never falls back to the default
  `"responses"` scope.
- `INSUFFICIENT_BALANCE` immediately applies the existing balance-disable path,
  is never retryable on the same account, and may only continue by selecting a
  different eligible account.
- HTTP 502/503/504 remain request-local, may fail over within the existing
  switch limit, and never create a global runtime block. A Search 5xx therefore
  cannot make the same account unavailable to Responses.
- Transport failures use the centralized OpenAI transport classifier.
  `context.Canceled` and caller deadline cancellation terminate immediately and
  do not fail over; eligible transient transport errors may switch accounts.
- A 2xx response is successful only when it is valid JSON with a top-level
  object, a string `output`, and either an absent, null, or string
  `encrypted_output`. Empty bodies, HTML, arrays, a missing or non-string
  `output`, and an invalid `encrypted_output` type are converted to a JSON 502.
  Validation parses only the contract shape; valid bytes are relayed unchanged.
- Once a valid native Search response is written, no failover is attempted.

### Observability and privacy

Structured logs record the request ID, API key ID, group ID, selected account
ID, model, body-size bucket, upstream status, duration, and failover count.
They do not record commands, queries, results, tokens, ChatGPT account IDs, or
raw bodies.

Ops error records identify the inbound endpoint as `/alpha/search` and the
upstream endpoint as `/backend-api/codex/alpha/search`. Scheduler success and
failure metrics are reported through the existing OpenAI scheduler hooks.

## Tests

### Service tests

- OAuth Search request uses the native upstream URL and preserves the request
  bytes.
- Canonical identity and account headers are present; client authorization is
  never forwarded.
- Token refresh failure performs no upstream request.
- Valid native JSON is returned unchanged.
- Empty bodies, HTML, arrays, missing/null/non-string `output`, and non-string
  non-null `encrypted_output` in a 2xx response become JSON 502.
- Balance 403 is terminal for the selected account and not same-account
  retryable.
- Temporary 502 is failover-eligible but does not create a global runtime block.
- A Search 502 does not make the same account unavailable to Responses.
- Caller cancellation stops immediately without an account switch.

### Handler and router tests

- Both Search paths share the authenticated OpenAI route behavior.
- Missing API key, group, billing eligibility, malformed JSON, missing `id`,
  missing `model`, and non-object commands fail before upstream; absent commands
  and `commands: {}` are accepted.
- API-key upstream accounts are excluded by the Search capability.
- The user slot and entity quota lease are released across success, billing
  failure after a user wait, entity rejection, scheduler failure, cancellation,
  and failover; every attempt-scoped account lease is also released.
- `/alpha/search` and `/v1/alpha/search` bypass the SPA and reach the handler.
- Unknown methods and paths across `/alpha`, `/alpha/`, `/alpha/*`,
  `/v1/alpha`, `/v1/alpha/`, and `/v1/alpha/*` return JSON 404 with JSON
  Content-Type, never HTML or Gin's default plain-text body.

### Production verification

The deployment candidate and public endpoint each receive a minimal native
Search request through both `/alpha/search` and `/v1/alpha/search`. All four
probes require:

- HTTP 200;
- `Content-Type: application/json`;
- valid JSON;
- a non-empty string `output`;
- no HTML prefix;
- no token, query, or result content in deployment logs.

Search probes use a dedicated streaming validator instead of the existing
`run_probe` response artifact path. The fixed request is supplied without a
retained payload file; response bytes flow directly into a shape validator and
are never written to `STATE_DIR`. Headers are reduced to status and media type,
and only a sanitized pass/fail verdict is retained. Raw `output`,
`encrypted_output`, request JSON, authorization, and account headers are absent
from stdout, stderr, curl traces, deployment state, and failure artifacts.

`test-hot-deploy` covers HTML, an incorrect Content-Type, malformed JSON,
missing or invalid `output`, either Search path failing during candidate or
public verification, rollback-before-cutover behavior, and scans all retained
artifacts for probe secrets and sentinel Search content.

Normal Responses and streaming remote Compact probes remain mandatory. A Search
probe does not replace either existing gate.

## Rollout and rollback

The change is deployed only through `deploy/hot-deploy.sh`. The prior healthy
application container remains running as the immediate rollback target. A
candidate that fails Search, Responses, or Compact verification is stopped and
retained for evidence without changing the active Caddy upstream.

After cutover, verify the Caddy Admin API target, container health, PostgreSQL
and Redis continuity, both native Search paths, real `/responses` traffic, and
the absence of new Search decode errors. Rollback uses the deployment tool's
saved Caddy state; PostgreSQL and Redis are never restarted or modified.
