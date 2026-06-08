# 27 - First-wave shared-pool messages-only design

> **Status:** Draft for review. This is **not** the full final signing-mode design.
> **Scope:** Define a first-wave shared-pool rollout for Anthropic `messages` traffic only, with `sign` as the target default/primary path and `strip` only as a manually approved controlled opt-out lane.
> **Non-goal:** This document does **not** authorize `count_tokens`, `event_logging` upstream forwarding, OpenAI-compatible converted Anthropic routes, Antigravity shared-pool traffic, automatic signing fallback, or endpoint-complete signing-mode rollout.
> **Source evidence:** `14-cc-gateway-shared-pool-compatibility-plan.md`, `25-claude-code-2146-reverse-coverage-and-signing-readiness-gates.md`, `26-signing-readiness-gap-closure-plan.md`, and the `2026-05-21` safe deliverables under `captures/real-baseline/`.

---

## 1. Purpose and strategic decision boundary

This document defines a **first-wave**, **messages-only** shared-pool design that can be evaluated before a later endpoint-complete signing-mode design exists.

The target first-wave operating model is **sign-primary**:

1. **Sign-primary lane**: target default and primary path for first-wave `/v1/messages`. It remains tiny and manually approved at canary start, but a successful canary should progress toward becoming the default first-wave lane.
2. **Strip-controlled lane**: a manually approved controlled opt-out path. It may be used only for baseline sanity, emergency diagnosis, emergency operator-approved fallback, cache optimization experiments, or another explicitly approved opt-out reason. It is not the long-term default and must not become the final resting point for first-wave rollout.
3. **Disabled lane**: fail-closed state for unsupported, blocked, unverified, or non-allowlisted routes/accounts.

A first-wave execution may start with a **strip-controlled baseline sanity** check to prove routing, redaction, route blocking, and CC Gateway final-output ownership. After that sanity check, the rollout must proceed to a **sign-primary canary** before it can be considered first-wave-ready. Stopping at strip-controlled would leave the strategic goal incomplete.

Both lanes are messages-only. Neither lane extends to `count_tokens`, event routes, OpenAI-compatible converted Anthropic routes, or Antigravity. There is no automatic fallback between lanes.

The first wave is intentionally narrow because current evidence supports:

1. native Anthropic `/v1/messages?beta=true` request shape;
2. Anthropic API-key passthrough `/v1/messages?beta=true` policy inclusion for messages-only traffic when configured;
3. CC Gateway as the sole final-output owner;
4. local CCH/`cc_version` fixture evidence sufficient to prepare a manually approved `/v1/messages` sign-primary canary;
5. fail-closed shared-pool routing with no real-upstream assumptions beyond the existing evidence and future manual canary approval.

CCH fixture validation is necessary for sign-primary, but it is not equivalent to whole-system safety.

---

## 2. Billing/CCH runtime mode semantics

First-wave shared-pool routing must use an explicit enum:

```text
billing_cch_mode = sign | strip | disabled
```

### 2.1 `sign`

`sign` is the **target default** and **primary** path for first-wave shared-pool `/v1/messages` traffic.

During initial rollout, `sign` is still manually approved, extremely small, account-allowlisted, route-allowlisted, and protected by feature flags and kill switches. After sign-primary canary success, it is the lane intended to become the default for eligible first-wave messages accounts.

`sign` is allowed only for native Anthropic `/v1/messages` final output. It is never allowed for `count_tokens`, event routes, OpenAI-compatible converted routes, Antigravity, or unknown routes.

### 2.2 `strip`

`strip` is a **manually approved controlled opt-out** lane. It is not the long-term default.

Allowed purposes:

1. baseline sanity before sign-primary canary;
2. emergency diagnostic investigation;
3. emergency operator-approved fallback for future requests only;
4. cache optimization experiments;
5. another explicitly approved opt-out reason with account-level and route-level allowlist.

`strip` must not be used as an automatic fallback after sign failure, must not expand silently to all accounts, and must always record policy version plus reason.

### 2.3 `disabled`

`disabled` is used for unsupported, blocked, unverified, or non-allowlisted routes/accounts. It must fail closed with the CC Gateway control-plane wire contract. It must not fall back to native direct upstream.

---

## 3. First-wave route scope

### 3.1 Included route family

Only this route family may enter first-wave shared-pool traffic:

1. **Native Anthropic messages**
   - inbound: `POST /v1/messages`
   - final upstream route: `POST /v1/messages?beta=true`
   - target primary lane: `billing_cch_mode=sign`
   - controlled opt-out lane: `billing_cch_mode=strip`

This includes:

1. OAuth/setup-token-backed `/v1/messages`;
2. Anthropic API-key passthrough `/v1/messages` if configured and explicitly eligible under the active policy.

Initial sign-primary canary should use a non-primary OAuth/setup-token account unless `28` explicitly approves an API-key passthrough sign-primary sub-cohort with separate gates.

### 3.2 Explicitly excluded route families

The following routes or flows are **not allowed** into first-wave shared-pool traffic:

1. `POST /v1/messages/count_tokens`
   - status: `blocked/deferred`
   - first-wave: excluded
   - reason: current evidence shows the path exists in the CLI/SDK internal layer, but there is no known official natural Claude Code `2.1.146` CLI trigger
   - reopening condition: only reopen after an official natural trigger condition is found and captured locally

2. Any `event_logging` route family
   - `POST /api/event_logging/batch`: `suppress locally`
   - `POST /api/event_logging/v2/batch`: `suppress locally`
   - unknown `/api/event_logging/*`: `block`
   - no first-wave upstream forwarding is allowed

3. OpenAI-compatible Anthropic conversion routes
   - `/v1/chat/completions` -> Anthropic
   - `/v1/responses` -> Anthropic
   - although local boundary work exists, these remain outside first-wave scope to keep rollout strictly messages-only

4. Antigravity shared-pool routes
   - provider remains disabled for first-wave shared-pool rollout

5. Any route outside native Anthropic `/v1/messages`
   - `billing_cch_mode=sign` must reject all non-messages routes
   - `billing_cch_mode=strip` must also reject all non-messages first-wave routes unless a later approved document expands scope

6. Any explicit flow that depends on unsupported or unstable lifecycle semantics
   - if a later CLI or environment change invalidates current `--resume` / `--session-id` assumptions, those flows must be re-gated before use

---

## 4. Route and lane matrix

| Route family | Sign-primary lane | Strip-controlled lane | First-wave policy |
|---|---:|---:|---|
| Native OAuth/setup-token `/v1/messages` | Target primary; allowed only after explicit sign canary approval | Allowed only for baseline sanity, diagnostic, emergency operator-approved fallback, cache optimization, or explicit opt-out | messages-only |
| Native API-key passthrough `/v1/messages` | Included if configured; disabled until explicit policy and canary approval | Included if configured; explicit controlled opt-out only | messages-only |
| Native `/v1/messages/count_tokens` | Excluded | Excluded | block/defer |
| API-key passthrough `/v1/messages/count_tokens` | Excluded | Excluded | block/defer |
| `/api/event_logging/batch` | Excluded | Excluded | suppress locally |
| `/api/event_logging/v2/batch` | Excluded | Excluded | suppress locally |
| unknown `/api/event_logging/*` | Excluded | Excluded | block |
| `/v1/chat/completions` -> Anthropic | Excluded | Excluded | do not canary |
| `/v1/responses` -> Anthropic | Excluded | Excluded | do not canary |
| Antigravity shared-pool | Excluded | Excluded | provider disabled |
| Unknown Anthropic route | Excluded | Excluded | fail closed / disabled |

---

## 5. Ownership boundary

### 5.1 Sub2API responsibilities

Sub2API remains the **scheduler/governance layer**. It owns:

1. upstream account selection;
2. shared-pool eligibility and account-level gates;
3. credential retrieval and routing token selection;
4. sticky/session governance where still needed at the scheduler layer;
5. quotas, cooldowns, budgets, circuit breaker, and audit decisions;
6. redacted logging and control-plane error classification;
7. route suppression/blocking before CC Gateway where policy requires it;
8. policy selection for `billing_cch_mode` using trusted server-side configuration only;
9. audit metadata for lane, policy version, route, selected-account hash, and reason.

For CC Gateway-selected first-wave routes, Sub2API must pass a **pre-final** body/request envelope. It must **not**:

1. sign CCH;
2. generate or normalize a billing attribution block;
3. compute or rewrite the `cc_version` suffix;
4. compact, canonicalize, or rewrite final Anthropic request bytes;
5. generate final `metadata.user_id` or final account/session identity fields;
6. synthesize the final Claude Code persona or endpoint-specific beta;
7. pass through user-supplied CCH, billing header, billing block, or final persona as shared-account identity;
8. use account proxy/TLS profile machinery to connect to CC Gateway;
9. fall back to direct native upstream if CC Gateway rejects the request.

### 5.2 CC Gateway responsibilities

CC Gateway is the **only final-output layer** for first-wave shared-pool routes. It owns:

1. final route allowlist decision for shared-pool Anthropic routes;
2. final mode enforcement for `billing_cch_mode=sign|strip|disabled`;
3. final account-level metadata/session identity normalization;
4. final canonical Claude Code `2.1.146` persona;
5. endpoint-specific `anthropic-beta`;
6. final header synthesis and stripping;
7. final body rewrite and compact JSON serialization;
8. billing block generation/normalization for sign-primary;
9. `cc_version` 3-hex suffix computation for sign-primary;
10. CCH placeholder/signing/post-sign verification for sign-primary;
11. strip verifier for the strip-controlled lane;
12. per-account identity and per-bucket egress isolation;
13. fail-closed control-plane error wire contract.

No other layer may mutate the final Anthropic request after CC Gateway final-output processing. Any post-sign mutation is forbidden.

---

## 6. Runtime decision tree

```text
if provider != anthropic:
  do not enter first wave

if route != native /v1/messages:
  block/defer/suppress according to route policy; do not sign

if shared-pool account gates fail:
  fail closed

if cc_gateway_enabled != true:
  fail closed for shared-pool route

if billing_cch_mode == disabled:
  fail closed for shared-pool route

if billing_cch_mode == sign:
  require sign-primary global flag, per-account allowlist, route allowlist, policy version, and manual approval id
  require CC Gateway final-output ownership
  CC Gateway owns final metadata/session identity, canonical persona, endpoint beta, billing block, cc_version suffix, compact JSON bytes, CCH signing, and post-sign verification
  fail closed on any missing precondition, verifier mismatch, or attempted post-sign mutation

if billing_cch_mode == strip:
  require strip-controlled flag, account allowlist, route allowlist, policy version, manual approval id, and explicit reason
  CC Gateway owns final strip-controlled output and verifies that no billing/CCH material remains
  fail closed if the request tries to use strip as automatic fallback from sign

otherwise:
  fail closed
```

### 6.1 No automatic fallback

Forbidden behaviors:

1. automatic `sign` -> `strip` fallback;
2. automatic `strip` -> `sign` fallback;
3. automatic CC Gateway -> native direct upstream fallback;
4. automatic account failover on control-plane rejection;
5. retrying a rejected sign request by stripping billing/CCH;
6. retrying a rejected strip request by signing it;
7. reuse of stale signed bytes;
8. user-supplied CCH/header/body passthrough as shared-account identity.

A manual rollback may change future account policy. An in-flight request whose mode fails verification must fail closed.

---

## 7. Sign-primary final-output pipeline

`billing_cch_mode=sign` is allowed only for `POST /v1/messages` and only inside CC Gateway final-output processing.

Input from Sub2API must be **pre-final**: it may carry normalized intent and scheduler context, but it must not be final persona, final account identity, final billing block, final compact JSON bytes, or user-supplied CCH material.

The sign-primary pipeline must perform, in order:

1. verify route is exactly native Anthropic `/v1/messages` final output;
2. verify account, policy version, egress bucket, sign-primary allowlist, and manual approval id;
3. drop or reject user-supplied CCH, billing header, billing block, and final persona fields;
4. rewrite account-level metadata/session identity inside CC Gateway;
5. synthesize the canonical Claude Code `2.1.146` headers/persona;
6. set endpoint-specific `anthropic-beta` and required Anthropic version headers;
7. generate or normalize the billing attribution block inside CC Gateway;
8. compute the `cc_version` 3-hex suffix using the validated Claude Code `2.1.146` formula:
   - `sha256("59cf53e54c78" + chars + cli_version)[:3]`
   - `chars = first_user_text positions [4,7,20] with "0" fallback`
9. serialize final compact JSON bytes with deterministic field handling required by the signer;
10. insert `cch=00000` placeholder into the final body bytes at the canonical billing location;
11. compute CCH as `xxh64(body_with_cch_00000, 0x4d659218e32a3268) & 0xFFFFF`;
12. encode CCH as lowercase zero-padded 5-hex;
13. replace the placeholder with the computed 5-hex CCH;
14. run post-sign verifier over the emitted final bytes;
15. forbid any post-sign body/header mutation;
16. fail closed before egress if any recomputation, route, header, persona, metadata/session, billing, serialization, signing, post-sign mutation, or redaction check fails.

The sign-primary lane must not log raw request bodies, raw prompts, raw CCH-bearing bodies, raw tokens, Authorization values, emails, or account UUIDs. Safe deliverables may contain only hashes, field names, booleans, and redacted summaries.

---

## 8. Strip-controlled lane restrictions

`billing_cch_mode=strip` is a controlled opt-out lane and must be explicitly justified.

Required restrictions:

1. no use as automatic fallback after sign verifier failure;
2. no silent expansion to all accounts;
3. account-level allowlist required;
4. route-level allowlist required;
5. manual approval id required;
6. policy version and reason required in audit logs;
7. CC Gateway remains the only final-output owner;
8. strip verifier must fail closed if billing/CCH material remains.

`strip` must fail closed if any final request still contains:

1. `x-anthropic-billing-header` header;
2. billing block text in body;
3. `cch=` marker anywhere in body;
4. unauthorized downstream identity fields after final rewrite.

Strip-controlled output is allowed only as a bounded lane for the purposes listed in section 2.2. It is not evidence that signing is safe, and it is not a substitute for the sign-primary canary.

---

## 9. Feature flags, allowlists, and kill switches

### 9.1 Sub2API gates

Required Sub2API gating for first wave:

- `gateway.cc_gateway.enabled=true`
- `gateway.cc_gateway.providers.anthropic=true`
- account `cc_gateway_enabled=true`
- account `cc_gateway_canary_only=false` for the selected canary cohort
- account `cc_gateway_policy_version` matches the active policy
- account route allowlist includes only native `/v1/messages` for first-wave shared-pool use
- account egress bucket exists and is enabled
- account policy selects one of `billing_cch_mode=sign|strip|disabled`
- sign-primary requires explicit per-account sign allowlist and manual approval reference
- strip-controlled requires explicit per-account strip allowlist, route allowlist, reason, and manual approval reference

### 9.2 CC Gateway gates

Required CC Gateway configuration for first wave:

- `mode=sub2api`
- `shared_pool.billing_cch_mode` supports `sign|strip|disabled`
- `shared_pool.sign_primary_enabled=false` by default until a manually approved canary window starts
- `shared_pool.sign_primary_kill_switch=true` must immediately stop sign-primary lane traffic when activated
- `shared_pool.strip_controlled_enabled=false` by default except approved baseline sanity, diagnostic, emergency, cache optimization, or opt-out windows
- `shared_pool.strip_controlled_kill_switch=true` must immediately stop strip-controlled lane traffic when activated
- Anthropic provider enabled
- strict shared-pool route/header allowlists active
- account identity records present for selected upstream accounts
- egress bucket mapping present and enabled
- post-sign verifier enabled before any `sign` lane egress
- strip verifier enabled before any `strip` lane egress

### 9.3 Explicit first-wave blocked gates

The following must remain explicit in rollout config and docs:

- `count_tokens`: blocked/deferred and first-wave excluded
- `event_logging`: suppress/block only; no upstream forward
- OpenAI-compatible Anthropic converted traffic: excluded from first wave
- Antigravity: disabled
- `disabled`: default for unsupported or unverified routes/accounts
- `sign`: target primary lane for eligible `/v1/messages`
- `strip`: controlled opt-out only, not primary and not automatic fallback

---

## 10. Header, body, and persona contract

### 10.1 Final persona contract

For first-wave shared-pool messages, CC Gateway owns the final Claude Code `2.1.146` persona contract:

- `User-Agent`
- `X-Stainless-*`
- `anthropic-version`
- endpoint-specific `anthropic-beta`
- `x-app`
- `Accept-Encoding`
- `X-Claude-Code-Session-Id`
- final `metadata.user_id` account/session binding
- final billing attribution block in sign-primary
- final compact JSON bytes in sign-primary

### 10.2 Session contract for first wave

Current evidence supports first-wave use of:

- default persistence;
- `-c/--continue`;
- explicit `--resume`;
- explicit `--session-id`.

Current Linux-local evidence also shows that `--output-format stream-json --verbose` does not change the request shape relative to JSON output in the tested messages path.

If future CLI versions alter that behavior, the affected route must be re-reviewed before rollout changes.

---

## 11. Control-plane and fail-closed contract

Any gateway-owned failure must fail closed with the CC Gateway control-plane contract.

Sub2API must not convert these into:

- Anthropic account ban/death signals;
- automatic account failover;
- native direct fallback;
- direct upstream bypass;
- automatic lane switch between `sign`, `strip`, and `disabled`.

Required control-plane characteristics:

- `X-CC-Gateway-Error-Kind: control-plane`;
- stable `X-CC-Gateway-Error-Code` values;
- redacted logging only;
- no raw credentials in logs or safe deliverables;
- no silent fallback to default bucket, direct egress, `strip`, or `sign`.

---

## 12. Egress and connection isolation

First-wave shared-pool routing requires:

1. per-account identity records;
2. explicit per-bucket egress mapping;
3. CC Gateway-owned connection/agent cache keys including provider + upstream account + egress bucket + proxy identity hash;
4. no use of Sub2API account proxy/TLS profile for CC Gateway-selected first-wave routes;
5. no process-global direct fallback.

Even if multiple accounts share a bucket, first-wave rollout must preserve account-level identity isolation.

---

## 13. Canary and rollout sequence

First-wave canary must not start unless all common gates are true:

1. P0-B refresh: PASS
2. P0-C metadata/session: PASS
3. P0-D Linux parity: PASS
4. P0-E event route-family policy: PASS
5. P0-F Sub2API boundary: PASS
6. P0-G CC Gateway final-output boundary: PASS
7. P0-H canonical persona lock: PASS
8. P0-I CCH/`cc_version` fixture evidence: PASS
9. P0-J API-key passthrough inclusion/defer policy: PASS
10. P0-K joint local capture: PASS
11. P0-A `count_tokens` remains explicitly blocked/deferred, first-wave excluded, and documented
12. no route outside `/v1/messages` is enabled

### 13.1 Required first-wave order

The first canary must include at least two steps:

1. **Strip-controlled baseline sanity**
   - verifies route selection, CC Gateway final-output ownership, blocked routes, redaction, control-plane errors, and fail-closed behavior;
   - uses explicit manual approval, account allowlist, route allowlist, policy version, and reason;
   - cannot be the final stopping point.

2. **Sign-primary canary**
   - exercises the target primary path with CC Gateway-owned billing/CCH signing;
   - starts only after baseline sanity and manual approval;
   - remains messages-only and extremely small.

### 13.2 Sign-primary canary shape

The sign-primary canary must remain extremely narrow:

- non-primary account;
- single account or very few explicitly allowlisted accounts;
- single egress bucket unless `28` explicitly approves more;
- very low request volume;
- one short messages request at a time at the beginning;
- messages-only;
- sign verifier enabled;
- post-sign mutation detector enabled;
- failure immediately sets the affected account/route to `disabled` or pauses the account/route;
- switch to strip-controlled only after separate manual approval.

### 13.3 Strip-controlled canary shape

The strip-controlled lane may be used only for:

- baseline sanity;
- emergency diagnostic investigation;
- emergency operator-approved fallback for future requests;
- cache optimization experiments;
- explicitly approved opt-out.

It requires account-level allowlist, route-level allowlist, manual approval id, policy version, and reason. It must not expand silently to all accounts.

---

## 14. Rollback and kill switches

Rollback must be explicit and auditable.

### 14.1 Sign-primary rollback

If sign-primary routing has any anomaly:

- activate `shared_pool.sign_primary_kill_switch` or disable `shared_pool.sign_primary_enabled`;
- remove the account from the sign allowlist;
- set the affected account/route to `billing_cch_mode=disabled`, or pause the account/route;
- preserve signed-request hashes and redacted verifier results for audit;
- do not replay the failed request in strip-controlled mode.

Switching future traffic from sign-primary to strip-controlled is allowed only after separate manual approval with account allowlist, route allowlist, policy version, and reason.

### 14.2 Strip-controlled rollback

If strip-controlled routing must stop:

- activate `shared_pool.strip_controlled_kill_switch` or disable `shared_pool.strip_controlled_enabled`;
- remove the account from the strip allowlist;
- set the affected account/route to `billing_cch_mode=disabled`, or pause the account/route;
- preserve redacted strip verifier results for audit.

### 14.3 What rollback must not do

Rollback must not silently:

- route `count_tokens` into shared-pool CC Gateway;
- route `event_logging` upstream;
- enable signing for another route/account;
- convert sign failure into strip-controlled traffic;
- fallback to unaudited native direct production traffic for the same shared-pool route;
- convert a control-plane error into account ban/death state.

### 14.4 Evidence-preserving rollback

Rollback must preserve:

- the blocked/deferred status of `count_tokens`;
- the suppress/block status of `event_logging`;
- the documented Linux parity basis;
- the audit trail that first-wave scope is messages-only;
- the exact lane (`sign`, `strip`, or `disabled`) and policy version for each canary attempt;
- the reason for any controlled opt-out to strip.

---

## 15. Verification matrix

| Case | Expected result |
|---|---|
| Native OAuth/setup-token messages, `billing_cch_mode=sign` | sign-primary success path: CC Gateway final-output pipeline signs and verifier passes |
| Native API-key passthrough messages, `billing_cch_mode=sign` | included only if configured and explicitly approved; otherwise disabled |
| Native OAuth/setup-token messages, `billing_cch_mode=strip` | allowed only with explicit strip-controlled approval, account allowlist, route allowlist, policy version, and reason |
| Native API-key passthrough messages, `billing_cch_mode=strip` | allowed only if configured and explicitly approved as strip-controlled |
| `billing_cch_mode=disabled` | fail closed for shared-pool route |
| Native `count_tokens` | blocked/deferred |
| API-key `count_tokens` | blocked/deferred |
| `/api/event_logging/batch` | suppress locally |
| `/api/event_logging/v2/batch` | suppress locally |
| unknown `/api/event_logging/*` | block |
| OpenAI chat/responses -> Anthropic | excluded from first wave |
| Antigravity | excluded from first wave |
| signing requested for non-`/v1/messages` route | fail closed |
| sign flag off / account not allowlisted / missing manual approval | fail closed |
| strip flag off / account not allowlisted / missing reason | fail closed |
| missing identity / bucket / policy mismatch | fail closed |
| sign verifier mismatch | fail closed |
| post-sign mutation detected | fail closed |
| sign failure then automatic strip retry | forbidden; fail closed |
| strip-controlled request without explicit approval | fail closed |
| user-supplied CCH/header/body identity | rejected or overwritten by CC Gateway; no passthrough as shared-account identity |
| strip verifier mismatch | fail closed |
| Linux persona fields | match Linux localhost evidence |
| `--resume` / `--session-id` messages path | covered by Linux localhost evidence |
| `stream-json` output mode | request shape matches JSON output in Linux localhost evidence |

---

## 16. What this document does not claim

This draft does **not** claim:

1. full endpoint-complete shared-pool signing readiness;
2. `count_tokens` readiness;
3. `event_logging` upstream readiness;
4. OpenAI-compatible Anthropic converted-route rollout readiness;
5. Antigravity shared-pool readiness;
6. automatic signing fallback safety;
7. broad multi-route production rollout;
8. real-upstream success for any route not already covered by evidence and manual approval.

This remains a messages-only first-wave design. Full endpoint-complete signing-mode still requires a future separate design and evidence set.

---

## 17. Next document boundary

This document is the constrained first-wave design only.

The next rollout document should be:

```text
docs/anti-ban/28-first-wave-shared-pool-rollout-plan.md
```

Document `28` must cover both first-wave lanes:

1. `sign-primary` lane rollout as the target primary/default path;
2. `strip-controlled` lane rollout only as baseline sanity, diagnostic, emergency operator-approved fallback, cache optimization, or explicit opt-out.

Document `28` must include separate entry criteria, traffic cohorts, observability, redaction checks, kill switches, rollback steps, and stop conditions for both lanes. It must not expand scope beyond messages-only unless a later approved design changes the route boundary.

## 20. 2026-05-22 OAuth scope gate clarification

Before any OAuth/setup-token account can enter first-wave native `/v1/messages`, Sub2API must verify the saved credential scope contains `user:inference`. This is a local pre-forward gate and must execute before CC Gateway selection/final-output forwarding or direct upstream forwarding. Missing, empty, non-string, or malformed scope fails closed with `inference_scope_missing`.

The ordinary OAuth web flow may successfully create an account while still returning only profile/API-key/file-upload scopes. Such accounts are not messages-capable and must remain blocked/quarantined. The current setup-token browser URL shape is not first-wave usable until separately repaired.

This clarification does not change the first-wave route scope: messages-only remains the only included route; count_tokens remains blocked/deferred; event_logging remains suppress/block; OpenAI-compatible Anthropic routes and Antigravity remain excluded. It also does not claim CCH real-upstream acceptance: the only real sign-primary attempt failed at OAuth scope authorization before CCH acceptance could be evaluated.
