# 26 - Signing Readiness Gap Closure Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` or `superpowers:executing-plans` to implement this plan checkpoint-by-checkpoint. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **Status:** Gap-closure execution plan. Not final signing-mode design. Not implemented.
> **Scope:** Close the blocking gaps identified in docs 14, 15, 20, 25 and the 2026-05-21 A/B audits before writing any final shared-pool signing-mode design.
> **Authoritative repos:**
> - Sub2API: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation`
> - CC Gateway: `/Users/muqihang/chelingxi_workspace/cc-gateway`
> - Archived path `/Users/muqihang/chelingxi_workspace/sub2api` is not authoritative.
> **Safety boundary:** No real upstream calls, no MITM, no OAuth login, no production traffic, and no deletion/cleanup unless explicitly approved. Localhost capture and static analysis only unless a later instruction explicitly authorizes real canary.
> **Decision:** Do not write/freeze `final signing-mode design` until this plan's P0 checkpoints pass and the evidence is reviewed.

**Goal:** Turn the open signing-readiness blockers into evidence-backed, testable checkpoints so a later agent can safely write the final CC Gateway signing-mode design.

**Architecture:** Sub2API remains the scheduler/governance/control-plane owner. CC Gateway must become the only final upstream output layer for shared-pool Anthropic traffic. Until proven otherwise, first-wave production remains `strip/no-CCH` for only validated routes; `sign` is a manually approved opt-in mode, not an automatic fallback.

**Tech Stack:** Go backend in Sub2API, TypeScript/Node in CC Gateway, localhost HTTPS capture fixtures, redacted markdown/JSON safe deliverables, Go/Node unit and integration tests.

---

## 1. Why this document exists

Docs 14-25 are now broad enough to cover both request-level and operations-level shared-pool risk:

- request identity, persona, egress, CCH, beta, body, and header gates;
- account lifecycle, session affinity, behavioral shaping, canary, decoy, soft signals;
- cross-account correlation, distributed scheduler state, audit/budget retention, disaster recovery;
- Claude Code 2.1.146 reverse-coverage gates and signing-readiness gates.

However, the latest GPT-5.5 xhigh review correctly concluded that these documents are still **pre-design / gate / gap-plan material**, not a final signing-mode design. The remaining blockers are not wording issues; they are missing evidence and missing code boundaries.

This document is the bridge:

```text
14-25 docs + A/B audits
  -> 26 gap closure plan
  -> local/static evidence + boundary implementation + joint capture
  -> reviewed final signing-mode design
  -> executable implementation plan
```

---

## 2. Non-negotiable invariants

1. **No overclaiming:** The target is risk reduction and consistency, not zero risk or guaranteed avoidance of controls.
2. **No extrapolation:** Doc 16 no-CCH PASS applies only to the minimal `/v1/messages` scenario already tested.
3. **No user CCH passthrough for shared pool:** Do not use downstream user-supplied billing/CCH/session/header as the shared account's final identity.
4. **One final output layer:** On shared-pool CC Gateway paths, CC Gateway must be the last body/header/persona/billing/CCH mutation layer.
5. **No double rewrite:** Sub2API must not generate final Claude Code mimicry/body/persona/billing/CCH on CC Gateway-selected paths.
6. **No silent native fallback:** If CC Gateway is unavailable or rejects a control-plane request, fail closed or pause the route/cohort.
7. **No accidental endpoint passthrough:** Unknown Anthropic endpoints must be rejected or explicitly deferred.
8. **No automatic manual opt-in signing mode:** `sign` mode can exist only as a manually approved opt-in mode after evidence gates pass.
9. **Fail closed on uncertainty:** Missing scheduler state, account identity, egress bucket, route policy, fixture evidence, or signing verifier result blocks the request.
10. **Redaction first:** Safe deliverables must not include raw Authorization, raw tokens, raw emails, raw account UUIDs, raw prompt text, raw request bodies, or raw CCH unless explicitly approved for a local-only raw directory.
11. **First-wave provider scope:** This plan applies only to the Claude / Anthropic shared pool. Antigravity is explicitly out of scope for every P0 gate, every task, and every joint capture in this document. Antigravity must not half-open through CC Gateway during this plan; if Antigravity routing must remain on, it stays on its existing native path and is not used to satisfy any P0 evidence here.
12. **Bounded body buffering and retry re-signing:** Any final-output pipeline must enforce an explicit max body size and reject larger requests; unbounded full-body buffering is not allowed. Any retry that changes body bytes must re-enter the final-output pipeline (normalize, re-serialize, re-strip or re-sign, verify); no reuse of previously signed bytes after a body mutation, and no unsigned/strip-uncertain fallback on retry.

---

## 3. P0 exit criteria for writing final signing-mode design

Final signing-mode design may be written only after all P0 items below have pass/fail evidence.

| ID | Gate | Maps to doc 25 gate(s) | Pass criteria | Evidence location |
|---|---|---|---|---|
| P0-A | 2.1.146 `count_tokens` local capture | G1 | Default attribution and attribution-off captures summarize path, header order, beta, body keys, metadata fields, billing/CCH booleans | `docs/anti-ban/captures/real-baseline/<DATE>-claude-code-2146-count-tokens-local-probe/` |
| P0-B | OAuth refresh/static + service-local mock | G2 | Static 2.1.146 extraction plus Sub2API/CC Gateway mock refresh behavior; no real `platform.claude.com` call | `docs/anti-ban/captures/real-baseline/<DATE>-claude-code-2146-oauth-refresh-static-and-local-mock-audit/` |
| P0-C | metadata/session lifecycle matrix | G3, G4 | Field names/hashes for `metadata.user_id` and `X-Claude-Code-Session-Id` under `-p`, no-session, session/resume if available, retry/error | `docs/anti-ban/captures/real-baseline/<DATE>-claude-code-2146-session-lifecycle-local-probe/` |
| P0-D | Linux parity | G5 | Deployment-like Linux/local capture compares OS/Arch/runtime/header/body/TLS summaries with intended server persona | `docs/anti-ban/captures/real-baseline/<DATE>-claude-code-2146-linux-parity-local-probe/` |
| P0-E | event_logging route-family policy | G6a | Explicit decision for `/api/event_logging/v2/batch`, legacy `/api/event_logging/batch`, and unknown event endpoints: `suppress`, `rewrite_via_cc_gateway`, `forward_allowlisted`, or `block`; no accidental passthrough | Updated doc 14/25 and route tests |
| P0-F | Sub2API CC Gateway boundary | doc 14 P0-4, doc 25 §3.1 | Four route families do not run final body/persona/billing/CCH mimicry before CC Gateway; OpenAI-compatible routes do not use account proxy/TLS profile to reach CC Gateway | Go tests in Sub2API |
| P0-G | CC Gateway final-output boundary | doc 14 P0-1/P0-2/P0-3, doc 25 §3.2 | per-account identity, egress bucket, route/header allowlist, control-plane error wire contract, strip verifier baseline, bounded body size, retry re-signing contract | Node tests in CC Gateway |
| P0-H | CC Gateway canonical Claude Code 2.1.146 persona lock | doc 14 P0-5, doc 25 §3.2 | final output locks and tests UA, `x-stainless-*`, `x-app`, `anthropic-version`, endpoint-specific `anthropic-beta`, `Accept-Encoding`, session header/body binding, config fixtures, and version/build-time source | Node tests and local capture summaries |
| P0-I | CCH and `cc_version` 2.1.146 fixture validation | docs 15/20, doc 25 | 2.1.146 raw local fixtures verify 5-hex CCH seed/placeholder flow and 3-hex `cc_version` suffix formula; mismatches block final signing design | `docs/anti-ban/captures/real-baseline/<DATE>-claude-code-2146-cch-cc-version-local-fixtures/` |
| P0-J | Anthropic API-key passthrough route decision | doc 14 P0-6 | messages/count_tokens API-key passthrough is either covered in joint capture or explicitly blocked/deferred for first wave | Updated doc 14/25 and route tests |
| P0-K | Joint local capture | doc 14 P0-6 | Sub2API -> CC Gateway -> local capture proves final bytes/headers are owned by CC Gateway and route policy is enforced for OAuth plus any included API-key passthrough routes | `docs/anti-ban/captures/real-baseline/<DATE>-sub2api-cc-gateway-joint-local-capture/` |

If any P0 gate fails, do not proceed to final signing-mode design. Write a blocker report and update doc 25.

---

## 4. Checkpoint 0 - Baseline hygiene and document correction

**Purpose:** Ensure future agents start from the right files and do not repeat known formula/path mistakes.

**Files:**
- Modify: `docs/anti-ban/15-cch-algorithm-validation-and-usage-plan.md`
- Modify: `docs/anti-ban/20-cch-cc-version-stability-regression.md`
- Modify: `docs/anti-ban/README.md`

- [ ] **Step 0.1: Verify authoritative paths**

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation
pwd
git status --short
cd /Users/muqihang/chelingxi_workspace/cc-gateway
pwd
git status --short
```

Expected:
- Sub2API path is the worktree above.
- CC Gateway path is `/Users/muqihang/chelingxi_workspace/cc-gateway`.
- Do not use archived `/Users/muqihang/chelingxi_workspace/sub2api` as evidence.

- [ ] **Step 0.2: Confirm `cc_version` formula text**

Check docs 15/20 state:

```text
suffix = sha256("59cf53e54c78" + chars + cli_version)[:3]
chars = char_at(first_user_text, 4) + char_at(first_user_text, 7) + char_at(first_user_text, 20)
missing positions use "0"
```

Expected:
- No remaining wording says `sha256(first_user_message + version)` as the actual formula.
- Docs still say 2.1.146 raw fixtures must verify the formula.

- [ ] **Step 0.3: Update index**

Add this document to `docs/anti-ban/README.md` as the bridge between docs 14-25 and final signing-mode design.

- [ ] **Step 0.4: Prepare document-only commit only after user approval**

Do not commit automatically. If the user explicitly approves committing these ignored docs, then run:

```bash
git add docs/anti-ban/15-cch-algorithm-validation-and-usage-plan.md \
        docs/anti-ban/20-cch-cc-version-stability-regression.md \
        docs/anti-ban/26-signing-readiness-gap-closure-plan.md \
        docs/anti-ban/README.md
git commit -m "docs(anti-ban): add signing readiness gap closure plan"
```

If `docs/anti-ban` is ignored, do not force-add without user approval; record that the document exists as an ignored deliverable.

---

## 5. Checkpoint 1 - Close reverse-coverage P0 gates locally

**Purpose:** Collect missing 2.1.146 evidence without MITM or real upstream traffic.

**Files / artifacts:**
- Create safe deliverables under `docs/anti-ban/captures/real-baseline/...`
- Reuse or extend existing local capture scripts under `docs/anti-ban/captures/real-baseline/2026-05-20-pre-capture-sop/`
- Do not store raw secrets in safe deliverables.

### Task 1.1: `count_tokens` local capture

- [ ] Trigger Claude Code 2.1.146 against localhost HTTPS capture with `ANTHROPIC_BASE_URL=https://localhost:<port>`.
- [ ] Run both default attribution and `CLAUDE_CODE_ATTRIBUTION_HEADER=0`.
- [ ] Capture only localhost requests; block non-localhost proxy/env routes.
- [ ] Safe summary must include:
  - method/path/query;
  - header key order and important values redacted;
  - `anthropic-beta` exact string;
  - body top-level keys;
  - metadata field names and hashes;
  - billing block present/absent;
  - `cch=` present/absent;
  - `Accept-Encoding` present/value;
  - request count and retry count.
- [ ] If official CLI does not produce `count_tokens`, record the trigger attempt and mark route as **deferred/block** until fixture exists.

Acceptance:
- `P0-A` is PASS or explicit FAIL/DEFER with route policy.

### Task 1.2: CCH and `cc_version` 2.1.146 local fixture validation

- [ ] Capture at least 8 Claude Code 2.1.146 localhost raw `/v1/messages?beta=true` requests across at least two prompt/body variants.
- [ ] For each request, verify 5-hex body CCH by restoring `cch=00000;` and computing `xxh64(final_body_bytes, 0x4d659218e32a3268) & 0xFFFFF`.
- [ ] For each request with billing attribution, verify the 3-hex `cc_version` suffix using `sha256("59cf53e54c78" + chars + cli_version)[:3]`, where `chars` comes from first-user-text positions `[4, 7, 20]` with `0` fallback.
- [ ] Include fixtures for `<system-reminder>` / first text block selection edge cases if available.
- [ ] Safe deliverable must include only match booleans, hashes, body-shape summaries, and formula version; raw bodies stay local-only and are not committed.

Acceptance:
- `P0-I` is PASS before final signing-mode design can be written. If CCH passes but `cc_version` fails, signing design remains blocked.

### Task 1.3: OAuth refresh static + service-local mock

- [ ] Static-audit installed Claude Code 2.1.146 package for token exchange/refresh endpoint, method, headers, body keys, retry behavior.
- [ ] Do not call real `platform.claude.com` or `api.anthropic.com`.
- [ ] Build a service-local mock scenario for Sub2API/CC Gateway refresh handling.
- [ ] Safe summary must include endpoint names, field names, redaction checklist, retry/lock observations.

Acceptance:
- `P0-B` is PASS or real-refresh-only gap is explicitly deferred with production route blocked until approved.

### Task 1.4: metadata/session lifecycle matrix

- [ ] Local capture matrix for:
  - `claude -p ... --no-session-persistence`;
  - default session persistence if safe locally;
  - explicit session id/resume/continue options if available;
  - stream/non-stream request modes where possible;
  - retry/error fake upstream.
- [ ] Record only hashes/field names for `device_id`, `account_uuid`, `session_id`, and `X-Claude-Code-Session-Id`.
- [ ] Derive rules for account-level stable fields vs per-session/lease fields.

Acceptance:
- `P0-C` and `P0-G` session-model inputs are sufficient for design.

### Task 1.5: Linux parity capture

- [ ] Run official Claude Code 2.1.146 or the deployment-intended equivalent on Linux/deployment-like host against localhost capture.
- [ ] Compare Mac vs Linux summaries for:
  - `User-Agent`;
  - `X-Stainless-OS`, `X-Stainless-Arch`, runtime/version;
  - `Accept-Encoding`;
  - beta order;
  - body keys;
  - TLS/ALPN summary if available.
- [ ] Decide whether server persona is Linux-native, account-frozen, or variant-pool controlled.

Acceptance:
- `P0-D` is PASS or production deployment on Linux remains blocked.

### Task 1.6: event_logging route-family policy and schema

- [ ] Decide first-wave policy before implementation for the full event route family:
  - `/api/event_logging/v2/batch`;
  - legacy `/api/event_logging/batch`;
  - any future `/api/event_logging/*` route;
  - unknown event-like endpoints.
- [ ] Policy must be exactly one of:
  - `suppress`: Sub2API ACKs locally and nothing reaches CC Gateway/upstream;
  - `rewrite_via_cc_gateway`: CC Gateway rewrites per-account event identity and forwards;
  - `forward_allowlisted`: only strict allowlisted event fields forward;
  - `block`: shared-pool route rejects event logging.
- [ ] Prefer `suppress` or `block` for first-wave unless 2.1.146 schema is verified.
- [ ] If schema capture is attempted, use local event endpoint only.
- [ ] Add route tests proving no accidental passthrough for v2, legacy, and unknown event endpoints.

Acceptance:
- `P0-E` is explicit in docs and tests; no event route family member can pass through by accident.

---

## 6. Checkpoint 2 - Sub2API shared-pool boundary hardening

**Purpose:** Make Sub2API a scheduler/governance layer on CC Gateway routes, not a competing final persona layer.

**Primary files:**
- `backend/internal/service/cc_gateway_adapter.go`
- `backend/internal/service/gateway_service.go`
- `backend/internal/service/gateway_forward_as_chat_completions.go`
- `backend/internal/service/gateway_forward_as_responses.go`
- `backend/internal/service/gateway_billing_header.go`
- `backend/internal/handler/gateway_handler.go`
- `backend/internal/handler/failover_loop.go`

### Task 2.1: Introduce explicit CCGatewayAnthropic boundary helper

- [ ] Write tests that fail today for each route family:
  - native messages selected for CC Gateway;
  - native count_tokens selected for CC Gateway;
  - OpenAI chat_completions converted to Anthropic selected for CC Gateway;
  - OpenAI responses converted to Anthropic selected for CC Gateway.
- [ ] Expected behavior:
  - no Sub2API final Claude mimicry body rewrite;
  - no Sub2API metadata.user_id final generation;
  - no Sub2API billing block generation;
  - no Sub2API CCH signing;
  - no account proxy used to reach CC Gateway.

### Task 2.2: Gate existing mimicry/body mutation blocks

- [ ] Add a clear branch around `shouldUseCCGatewayAnthropic(account)` before body/persona mutation blocks.
- [ ] Preserve Sub2API governance features:
  - account selection;
  - sticky/lease/scheduler hooks;
  - quota/budget;
  - circuit breaker;
  - redacted audit;
  - request validation that is not final persona output.

### Task 2.3: Per-account allow/deny/canary gates

- [ ] Add or verify account-level gates for shared-pool CC Gateway routing:
  - `cc_gateway_enabled`;
  - `cc_gateway_canary_only`;
  - route allow/deny list;
  - account lifecycle eligibility;
  - egress bucket enabled;
  - policy version match.
- [ ] A disabled or canary-only account must not accidentally receive broad production shared-pool traffic.
- [ ] Unknown policy version or missing gate state fails closed before CC Gateway.

### Task 2.4: Control-plane error classification and wire contract consumption

- [ ] Consume a fixed CC Gateway control-plane error wire contract, not ad-hoc status/body text:
  - response header: `X-CC-Gateway-Error-Kind: control-plane`;
  - response header: `X-CC-Gateway-Error-Code: <stable_code>`;
  - JSON body: `{ "error": { "type": "cc_gateway_control_plane", "code": "<stable_code>" } }` or equivalent stable schema.
- [ ] Required stable codes include:
  - invalid gateway token;
  - unsupported provider;
  - unsupported token type;
  - signing verifier failure;
  - strip verifier failure;
  - missing per-account identity;
  - missing egress bucket;
  - disabled egress bucket;
  - proxy/agent creation failure;
  - route/header allowlist reject;
  - body too large.
- [ ] Ensure these errors:
  - fail closed to caller;
  - do not mark Anthropic account banned/dead;
  - do not trigger failover to another account;
  - do not trigger fallback to native Sub2API mimicry or direct upstream;
  - are logged redacted as gateway/control-plane events.
- [ ] Add paired tests: CC Gateway emits the contract; Sub2API consumes it and suppresses account-health side effects.

### Task 2.5: Test command

Run from Sub2API backend:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/backend
go test ./internal/service ./internal/handler ./internal/server/routes ./internal/config -run 'CCGateway|Mimicry|CountTokens|ChatCompletions|Responses|FailClosed|ControlPlane|Canary|EventLogging' -count=1
```

Expected:
- New route-boundary tests pass.
- Existing anti-ban strict/mimicry tests still pass where applicable.

---

## 7. Checkpoint 3 - CC Gateway final-output boundary hardening

**Purpose:** Prepare CC Gateway to be the sole final upstream output owner, first for strip/no-CCH and later for opt-in sign.

**Primary files:**
- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/config.ts`
- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/proxy.ts`
- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/rewriter.ts`
- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/proxy-agent.ts`
- New candidate modules:
  - `src/account-identity.ts`
  - `src/egress.ts`
  - `src/header-policy.ts`
  - `src/route-policy.ts`
  - `src/billing-attribution.ts`
  - `src/cch.ts`

### Task 3.1: Per-account identity manager

- [ ] Design identity records keyed by selected upstream account id, not downstream user id.
- [ ] Fields must include at minimum:
  - stable device identity;
  - account/email hashes for logging;
  - selected persona variant;
  - session policy state if owned by CC Gateway;
  - policy version.
- [ ] Missing identity fails closed.

### Task 3.2: Per-account egress bucket and connection-pool isolation

- [ ] Parse and consume `x-cc-egress-bucket`.
- [ ] Map bucket to proxy/agent configuration without using a single process-global proxy for all accounts.
- [ ] Agent cache key must include at minimum provider + upstream account id + egress bucket + proxy identity hash.
- [ ] Ensure connection reuse is bucket/account scoped.
- [ ] Missing, unknown, disabled, or proxy-failing bucket fails closed; never direct-connect as fallback.
- [ ] Proxy URLs and credentials must be redacted in logs, health output, and safe deliverables.
- [ ] Explicitly retire or gate any `openai_gateway_egress_bucket` legacy fallback so it cannot silently override the shared-pool egress decision.

### Task 3.3: Strict route and header allowlists

- [ ] Replace blacklist/pass-through semantics on shared-pool Anthropic routes with allowlist output.
- [ ] Explicitly decide per route:
  - OAuth/setup-token `/v1/messages?beta=true`;
  - OAuth/setup-token `/v1/messages/count_tokens?beta=true`;
  - Anthropic API-key passthrough `/v1/messages?beta=true` if included, otherwise block/defer;
  - Anthropic API-key passthrough `/v1/messages/count_tokens?beta=true` if included, otherwise block/defer;
  - `/api/event_logging/v2/batch`;
  - legacy `/api/event_logging/batch`;
  - unknown `/api/event_logging/*`;
  - `policy_limits` / `settings` / auxiliary endpoints;
  - unsupported endpoints.
- [ ] Header policy must define exact ownership for:
  - Authorization / x-api-key;
  - User-Agent;
  - `X-Stainless-*`;
  - `X-Claude-Code-Session-Id`;
  - `anthropic-beta`;
  - `anthropic-version`;
  - `Accept-Encoding`;
  - `x-app`;
  - `x-cc-*` stripping.
- [ ] Define an explicit **max request body size** for shared-pool Anthropic routes; requests exceeding it fail closed with a redacted control-plane error.
- [ ] Forbid unbounded full-body buffering for routes that may sign or strip-verify; the pipeline must decide before reading whether the body fits the configured cap.

### Task 3.4: Emit fixed control-plane error wire contract

- [ ] CC Gateway must mark gateway-owned failures with a stable wire contract:
  - `X-CC-Gateway-Error-Kind: control-plane`;
  - `X-CC-Gateway-Error-Code: <stable_code>`;
  - redacted JSON body with stable `type` and `code` fields.
- [ ] These failures must not be indistinguishable from Anthropic upstream 401/403/429/5xx responses.
- [ ] Tests must cover invalid token, unsupported provider, unsupported route, missing identity, missing/disabled egress bucket, strip verifier failure, signing verifier failure, and body-too-large.

### Task 3.5: Rename misleading helper

- [ ] Rename or clearly isolate CC Gateway `computeCCH()` because it currently computes the 3-hex `cc_version` suffix, not 5-hex body CCH.
- [ ] Add tests for `cc_version` suffix once fixtures exist.

### Task 3.6: Canonical Claude Code 2.1.146 persona lock

- [ ] Define fixtures/config for the exact final persona CC Gateway emits for shared-pool Anthropic routes:
  - `User-Agent`;
  - all required `X-Stainless-*` keys and values;
  - `x-app`;
  - `anthropic-version`;
  - endpoint-specific `anthropic-beta` for messages and count_tokens;
  - `Accept-Encoding` policy;
  - `X-Claude-Code-Session-Id` and its binding to body session fields;
  - source of `2.1.146` version/build-time values;
  - `config.example.yaml` defaults and comments.
- [ ] Tests must prove CC Gateway synthesizes or overwrites the final persona and does not pass through contradictory downstream/Sub2API values.
- [ ] Header casing/order requirements should be captured if the runtime allows deterministic output; if not, document the residual transport/header-order risk.

### Task 3.7: Strip verifier baseline

- [ ] For first-wave strip mode, verify final body/header contains no:
  - `x-anthropic-billing-header`;
  - `cch=`;
  - `x-anthropic-billing-header` HTTP header;
  - downstream user identity fields outside approved account-level transformations.
- [ ] Fail closed on verification failure.

### Task 3.8: Manual opt-in signing pipeline skeleton behind disabled flag

Do not enable in production yet. The skeleton should make the future final design concrete:

```text
if billing_cch_mode == "sign":
  require all signing evidence gates passed
  normalize/generate billing block
  compute cc_version suffix
  final compact JSON serialize
  set cch=00000 placeholder
  compute 5-hex CCH from final bytes
  replace placeholder
  verify by restoring placeholder and recomputing
  forbid post-sign body mutation
else if billing_cch_mode == "strip":
  strip and verify no billing/CCH remains
else:
  fail closed or disabled route
```

Retry contract on top of the skeleton:

```text
if a retry mutates request body bytes:
  re-enter the final-output pipeline (normalize -> serialize -> strip or sign -> verify)
  do not reuse previously signed body bytes
  do not downgrade sign -> strip silently (and never sign -> unsigned)
  if pipeline cannot re-execute safely: fail closed

if a retry does not mutate body:
  reuse final bytes only if header policy and signing gates have not changed
  otherwise fail closed
```

Acceptance:
- `sign` path can be tested locally but remains off by default.
- `strip` verifier is active for shared-pool routes.
- Body-size cap and retry re-signing rules have unit tests.

### Task 3.9: Redaction and logging tests

- [ ] Add CC Gateway tests for `logger.ts`, `proxy-agent.ts`, and proxy error paths proving logs do not expose raw Authorization, upstream tokens, raw account UUID/email, raw CCH, raw request body, or proxy credentials.
- [ ] Add Sub2API tests or fixtures covering debug/full-body/ops artifacts on CC Gateway paths; generated account identity and control-plane errors must be redacted.
- [ ] Safe deliverables must include a sensitive-pattern scan result.

### Task 3.10: Test command

Run from CC Gateway:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway
npm run build
npm test -- --runInBand
```

If the project uses Node's built-in test runner or a different package script, run the existing build/test scripts from `package.json` and record exact output.

Expected:
- Per-account identity tests pass.
- Egress bucket tests pass.
- Header/route allowlist tests pass.
- Strip verifier tests pass.
- Signing skeleton tests pass with mode disabled by default.

---

## 8. Checkpoint 4 - Joint local capture acceptance

**Purpose:** Prove the two projects cooperate correctly before any real canary.

**Topology:**

```text
downstream test client
  -> Sub2API selected Anthropic account
  -> CC Gateway sub2api mode
  -> localhost capture upstream
```

**Scenarios:**

- [ ] OAuth/setup-token native `/v1/messages`, shared-pool strip;
- [ ] OAuth/setup-token native `/v1/messages/count_tokens`, if P0-A fixture exists; otherwise route blocked/deferred;
- [ ] Anthropic API-key passthrough `/v1/messages`, if included; otherwise prove blocked/deferred;
- [ ] Anthropic API-key passthrough `/v1/messages/count_tokens`, if included; otherwise prove blocked/deferred;
- [ ] OpenAI `/v1/chat/completions` converted to Anthropic;
- [ ] OpenAI `/v1/responses` converted to Anthropic;
- [ ] `/api/event_logging/v2/batch`, legacy `/api/event_logging/batch`, and unknown event endpoint according to P0-E policy;
- [ ] CC Gateway control-plane 401/403/400 with stable wire contract;
- [ ] unknown endpoint;
- [ ] missing identity;
- [ ] missing egress bucket;
- [ ] strip verifier failure fixture;
- [ ] optional local-only signing verifier fixture, disabled for normal route.

**Safe deliverable must include:**

- route;
- selected account id hash;
- egress bucket id;
- policy version;
- header key order/values summary;
- body hash and body key summary;
- billing/CCH presence booleans;
- metadata/session field names and hashes;
- request count;
- fail-closed result for negative cases;
- redaction scan result.

**Artifact path:**

```text
docs/anti-ban/captures/real-baseline/<DATE>-sub2api-cc-gateway-joint-local-capture/
```

Use the actual execution date in `YYYY-MM-DD` form. Do not hard-code `2026-05-21` if the work runs on a later date.

Acceptance:
- No raw secrets in safe deliverable.
- No direct Anthropic upstream traffic.
- No native fallback.
- No Sub2API final persona/body mutation on CC Gateway-selected paths.
- CC Gateway owns final headers/body.
- Negative cases fail closed.

---

## 9. Checkpoint 5 - Update gates and decide whether final signing design may start

**Files:**
- Modify: `docs/anti-ban/14-cc-gateway-shared-pool-compatibility-plan.md`
- Modify: `docs/anti-ban/25-claude-code-2146-reverse-coverage-and-signing-readiness-gates.md`
- Create or update: local capture summary docs under `captures/real-baseline/...`

- [ ] Mark each P0 gate PASS / FAIL / DEFER with evidence path.
- [ ] If a route is DEFER, document whether it is blocked, suppressed, or excluded from first-wave canary.
- [ ] If all P0 gates pass, write a short readiness memo recommending whether to write final signing-mode design.
- [ ] If any P0 gate fails, do not write final design; write blocker remediation tasks.

Readiness to write final design requires:

```text
P0-A count_tokens evidence/pass or route blocked
P0-B refresh evidence/pass or refresh path safely isolated
P0-C metadata/session rules defined
P0-D Linux parity resolved
P0-E event route-family policy explicit
P0-F Sub2API boundary tests pass
P0-G CC Gateway final-output boundary tests pass
P0-H canonical 2.1.146 persona lock tests pass
P0-I CCH and cc_version 2.1.146 fixture validation pass
P0-J API-key passthrough included or blocked/deferred explicitly
P0-K joint local capture pass
```

---

## 10. What the later final signing-mode design must contain

Only after Checkpoint 5 passes, create a separate document, likely:

```text
docs/anti-ban/27-final-shared-pool-signing-mode-design.md
```

Required sections:

1. Scope / non-goals / first-wave route matrix.
2. Sub2API vs CC Gateway ownership boundary.
3. `strip | sign | disabled` runtime decision tree.
4. CC Gateway final-output signing pipeline.
5. CCH 5-hex algorithm and verifier.
6. `cc_version` 3-hex suffix algorithm and fixtures.
7. Endpoint policy for messages / count_tokens / event_logging / chat / responses.
8. Per-account identity/session model.
9. Per-account egress and connection-pool isolation.
10. Strict route/header allowlist.
11. Fail-closed and no native fallback contract.
12. Control-plane error wire contract.
13. Audit/budget/redaction requirements.
14. Linux parity and transport residual-risk gate.
15. Canary / rollback / disaster runbook.
16. Verification matrix and fixture acceptance criteria.

Do not call that document final unless it explicitly references the PASS evidence from this plan.

---

## 11. Review procedure

After this plan or any checkpoint is completed:

1. Run a self-check against docs 14, 15, 20, 25.
2. Have a separate review agent review only the produced documents/artifacts, not chat history.
3. Fix P0/P1 feedback.
4. Repeat review once.
5. Only then ask the user whether to proceed to the next checkpoint.

Review focus:

- target consistency with multi-account shared pool;
- no overclaiming;
- no accidental real-upstream/MITM requirement;
- Sub2API/CC Gateway boundary clarity;
- evidence sufficiency;
- testability and rollback.

---

## 12. Current recommendation

Proceed in this order:

1. Finish Checkpoint 0 document hygiene.
2. Execute Checkpoint 1 localhost/static reverse-coverage probes.
3. Implement Checkpoint 2 and Checkpoint 3 code boundaries in isolated worktrees or carefully reviewed branches.
4. Run Checkpoint 4 joint local capture.
5. Update Checkpoint 5 gate status.
6. Only then write the final signing-mode design.

Do not skip directly from CCH algorithm validation to runtime signing. Correct CCH is necessary for sign mode, but it is not sufficient for shared-pool safety.
