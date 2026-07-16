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

The authenticated OpenAI gateway router registers `/alpha/search` and
`/v1/alpha/search` with the same API-key middleware chain used by `/responses`.
The handler requires an assigned OpenAI group and a valid authenticated user.
It applies the normal billing-eligibility check before scheduling an upstream
account.

The router also reserves `/alpha/*` and `/v1/alpha/*` as API namespaces. An
unknown path in either namespace returns a JSON 404 envelope. This prevents a
future Codex control-plane endpoint from silently receiving frontend HTML.

### Request validation

The handler reads the body through the existing bounded request reader. It
requires:

- a valid JSON object;
- a non-empty string `id`;
- a non-empty string `model`;
- a non-empty JSON object in `commands`.

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

The selected account acquires the same account concurrency lease used by OpenAI
HTTP requests. The lease is always released on success, error, cancellation,
and failover.

### Native upstream forwarding

A focused `ForwardNativeSearch` service method:

1. Requires an OpenAI OAuth account.
2. Gets a current token through `OpenAITokenProvider`, including managed refresh
   and refresh-failure state updates.
3. Posts the original body to
   `https://chatgpt.com/backend-api/codex/alpha/search`.
4. Applies the canonical Codex identity headers, ChatGPT account header,
   configured proxy, OpenAI HTTP profile, and TLS fingerprint policy used by
   existing Codex upstream calls.
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
- Upstream 401/403/429 and model errors use the existing OpenAI account state
  handlers.
- `INSUFFICIENT_BALANCE` never retries the same account.
- Upstream 429, 5xx, and transport failures may fail over to another eligible
  OAuth account within the existing switch limit.
- HTTP 502/503/504 remain request-local and do not globally poison account
  scheduling.
- A non-JSON 2xx upstream response is converted to a JSON 502 instead of being
  passed to Codex as a successful Search result.
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
- HTML or empty 2xx response becomes JSON 502.
- Balance 403 is terminal for the selected account and not same-account
  retryable.
- Temporary 502 is failover-eligible but does not create a global runtime block.

### Handler and router tests

- Both Search paths share the authenticated OpenAI route behavior.
- Missing API key, group, billing eligibility, malformed JSON, missing `id`,
  missing `model`, and empty commands fail before upstream.
- API-key upstream accounts are excluded by the Search capability.
- Account leases are released across success, error, cancellation, and failover.
- Unknown `/alpha/*` and `/v1/alpha/*` paths return JSON 404, not HTML 200.

### Production verification

The deployment candidate and public endpoint each receive a minimal native
Search request. The probe requires:

- HTTP 200;
- `Content-Type: application/json`;
- valid JSON;
- a non-empty string `output`;
- no HTML prefix;
- no token, query, or result content in deployment logs.

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
