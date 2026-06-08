# 28 - First-wave shared-pool messages-only rollout plan

> **Status:** Draft for review. Do not execute any real canary from this document without separate human approval.
> **Basis:** `27-first-wave-shared-pool-messages-only-design.md`.
> **Scope:** First-wave Anthropic shared-pool rollout for native `/v1/messages` only, using a short `strip-controlled` baseline sanity step followed by a `sign-primary` canary.
> **Non-goals:** This is not a full endpoint-complete signing-mode design, not a `count_tokens` rollout, not an `event_logging` upstream-forwarding rollout, not an OpenAI-compatible Anthropic conversion rollout, and not an Antigravity rollout.

---

## 1. Rollout objective

The rollout objective is to validate the revised first-wave target from doc `27`:

1. **`sign-primary` is the target default path** for eligible first-wave `/v1/messages` shared-pool traffic.
2. **`strip-controlled` is only a bounded baseline/diagnostic/opt-out lane**, not the long-term default and not a final resting point.
3. **`disabled` is the safe state** for unsupported routes, blocked routes, failed gates, failed verifiers, missing account identity, missing egress bucket, or any canary anomaly.

The first rollout sequence has two required lanes:

1. `strip-controlled baseline sanity`
   - validates deployment, route selection, blocked route policy, logs, metrics, redaction, control-plane errors, egress-bucket presence, and rollback controls;
   - does not validate CCH signing safety;
   - requires manual approval, account allowlist, route allowlist, policy version, and reason;
   - cannot become the long-term default.

2. `sign-primary canary`
   - validates the target `/v1/messages` signing path;
   - requires CC Gateway to own final-output synthesis, billing block, `cc_version`, compact JSON serialization, CCH re-signing, metadata/session normalization, canonical persona, endpoint-specific beta, post-sign verifier, and fail-closed behavior;
   - uses extremely small account and request volume;
   - fails to `disabled` or account/route pause by default;
   - forbids automatic `sign` -> `strip` fallback.

---

## 2. Hard boundaries

This rollout plan does not permit:

1. real canary execution without an explicit human confirmation gate;
2. traffic to any route except native Anthropic `/v1/messages`;
3. `/v1/messages/count_tokens` forwarding;
4. `/api/event_logging/*` upstream forwarding;
5. OpenAI-compatible Anthropic converted traffic;
6. Antigravity shared-pool traffic;
7. automatic fallback from `sign` to `strip` or direct upstream;
8. Sub2API generation of billing block, CCH, final persona, final metadata/session identity, or final body bytes;
9. user-supplied CCH/header/body passthrough as shared-account identity;
10. raw token, Authorization, email, account UUID, raw CCH-bearing body, raw body, or prompt text in safe deliverables.

Any violation must stop the rollout and set the affected account/route to `disabled` or paused.

---

## 3. Route block matrix

| Route / flow | First-wave status | Runtime action | Notes |
|---|---|---|---|
| OAuth/setup-token `POST /v1/messages` | Included | `sign-primary` target; `strip-controlled` baseline only | messages-only |
| Anthropic API-key passthrough `POST /v1/messages` | Included if configured | same lane rules as messages; may start disabled until explicitly configured | messages-only |
| OAuth/setup-token `POST /v1/messages/count_tokens` | Blocked/deferred | `disabled` / block | no official natural CLI trigger currently known |
| API-key passthrough `POST /v1/messages/count_tokens` | Blocked/deferred | `disabled` / block | no first-wave forwarding |
| `POST /api/event_logging/batch` | Excluded | suppress locally | no upstream forward |
| `POST /api/event_logging/v2/batch` | Excluded | suppress locally | no upstream forward |
| unknown `/api/event_logging/*` | Excluded | block | fail closed |
| `/v1/chat/completions` -> Anthropic | Excluded | block/defer outside first wave | no OpenAI-compatible Anthropic routes |
| `/v1/responses` -> Anthropic | Excluded | block/defer outside first wave | no OpenAI-compatible Anthropic routes |
| Antigravity shared-pool | Excluded | provider disabled | no first-wave canary |
| unknown Anthropic route | Excluded | `disabled` / fail closed | no fallback |

---

## 4. Feature flags and policy controls

### 4.1 Shared controls

Required controls before either lane can run:

- Sub2API `gateway.cc_gateway.enabled=true`
- Sub2API `gateway.cc_gateway.providers.anthropic=true`
- account `cc_gateway_enabled=true`
- account `cc_gateway_policy_version` equals the approved rollout policy version
- account route allowlist contains only native `/v1/messages`
- account egress bucket id is present and enabled
- CC Gateway `mode=sub2api`
- CC Gateway Anthropic provider enabled
- CC Gateway strict route/header allowlists enabled
- CC Gateway per-account identity record present
- CC Gateway egress bucket mapping present and enabled
- CC Gateway control-plane error contract enabled
- logging redaction enabled in both Sub2API and CC Gateway

### 4.2 `strip-controlled` controls

Required controls for baseline sanity:

- `billing_cch_mode=strip`
- `shared_pool.strip_controlled_enabled=true` only during the approved window
- `shared_pool.strip_controlled_kill_switch=false` only during the approved window
- per-account strip allowlist
- route allowlist restricted to native `/v1/messages`
- manual approval id
- explicit reason, for example `baseline_sanity`, `diagnostic`, `emergency_operator_fallback`, `cache_optimization`, or `approved_opt_out`
- policy version recorded in audit logs
- strip verifier enabled

`strip-controlled` must default to disabled outside approved windows.

### 4.3 `sign-primary` controls

Required controls for sign-primary canary:

- `billing_cch_mode=sign`
- `shared_pool.sign_primary_enabled=true` only during the approved window
- `shared_pool.sign_primary_kill_switch=false` only during the approved window
- per-account sign allowlist
- route allowlist restricted to native `/v1/messages`
- manual approval id
- policy version pinned to the reviewed sign-primary policy
- post-sign verifier enabled
- post-sign mutation detector enabled
- CCH signer uses `xxh64(body_with_cch_00000, 0x4d659218e32a3268) & 0xFFFFF`
- `cc_version` helper uses `sha256("59cf53e54c78" + chars + cli_version)[:3]`
- no automatic retry into `strip`, direct upstream, or another account

`sign-primary` is the target default lane after a successful canary, but it still starts behind manual approval, a tiny allowlist, a single account or very few explicitly allowlisted accounts, and very low request volume.

---

## 5. Account allowlist and egress bucket checks

Before any lane can run, the operator must confirm:

1. selected account is non-primary for initial canary;
2. selected account id appears only as a redacted hash in safe deliverables;
3. account has `cc_gateway_enabled=true`;
4. account has the approved `cc_gateway_policy_version`;
5. account route allowlist contains only `/v1/messages`;
6. account lane allowlist matches the lane:
   - strip baseline: strip allowlist only;
   - sign canary: sign allowlist only;
7. account egress bucket id is configured;
8. egress bucket is enabled;
9. CC Gateway connection/agent cache key includes provider + upstream account id + egress bucket + proxy identity hash;
10. no default egress bucket or direct egress fallback exists;
11. proxy URL and credentials are redacted in all logs;
12. account lifecycle status is eligible and not banned/dead/paused before canary.

If any check fails, set route/account to `disabled` or pause. Do not retry through native direct upstream.

---

## 6. Deployment checklist

### 6.1 Pre-deployment local verification

Run and record fresh local verification before a real canary request is approved:

1. Sub2API targeted tests covering CC Gateway route selection, control-plane errors, event logging policy, local capture, and no native fallback.
2. CC Gateway build and tests covering final-output pipeline, route allowlist, egress bucket isolation, strip verifier, sign verifier, post-sign mutation failure, control-plane errors, and redaction.
3. Joint local capture for messages-only strip and sign fixtures if available.
4. Sensitive-pattern scan for all safe deliverables and rollout notes.
5. Config diff review proving only the selected account/route/lane is enabled.

No test or local capture may call real Anthropic upstream.

### 6.2 Config deployment gate

Before enabling any lane, verify the deployed config contains:

- exact policy version;
- exact account allowlist;
- exact route allowlist;
- exact egress bucket id;
- selected lane (`strip` or `sign`);
- manual approval id;
- reason;
- kill switch values;
- log redaction settings;
- control-plane error code map;
- no broad wildcard route or account enablement.

### 6.3 Real canary manual confirmation gate

A real canary must not start until the operator explicitly confirms all of the following:

- route is native `/v1/messages` only;
- selected account(s) and egress bucket(s) are correct;
- lane is correct;
- manual approval id is recorded;
- stop conditions are understood;
- rollback command/config path is ready;
- no `count_tokens`, event logging upstream, OpenAI-compatible Anthropic route, or Antigravity traffic is enabled;
- no automatic fallback is enabled;
- safe-deliverable redaction rules are active.

This document is a plan only; it does not itself grant approval for a real canary.

---

## 7. Canary request shape

### 7.1 Common request constraints

Canary traffic must be:

- native `POST /v1/messages` only;
- one short request at a time initially;
- no raw prompt text in safe deliverables;
- no raw request body in safe deliverables;
- no downstream-supplied CCH or billing identity passthrough;
- no streaming expansion unless separately approved after non-streaming sanity;
- no tool-heavy, file-heavy, or large body requests in the initial canary.

Safe deliverable may record only:

- route;
- selected account id hash;
- egress bucket id;
- policy version;
- lane (`strip` or `sign`);
- manual approval id hash or redacted reference;
- header key order and value summary;
- body key summary;
- body hash;
- metadata/session field names and hashes;
- billing block presence boolean;
- CCH presence boolean;
- verifier result boolean;
- request count;
- retry count;
- negative-case fail-closed result;
- redaction scan result.

### 7.2 `strip-controlled` baseline sanity request

Use exactly the selected `/v1/messages` account/route allowlist and `billing_cch_mode=strip`.

Expected final-output properties:

- CC Gateway owns final headers/body;
- no billing header;
- no billing block;
- no `cch=` marker;
- canonical persona still owned by CC Gateway;
- route blocks and suppressions are active;
- negative cases fail closed;
- logs contain redacted lane/policy/account/bucket summaries.

This request proves deployment and policy sanity only. It does not complete the rollout.

### 7.3 `sign-primary` canary request

Use exactly the selected `/v1/messages` account/route allowlist and `billing_cch_mode=sign`.

Expected final-output properties:

- Sub2API input to CC Gateway is pre-final;
- CC Gateway rewrites account-level metadata/session identity;
- CC Gateway sets canonical Claude Code `2.1.146` persona;
- CC Gateway sets endpoint-specific beta;
- CC Gateway generates or normalizes the billing attribution block;
- CC Gateway computes `cc_version` 3-hex suffix using `sha256("59cf53e54c78" + chars + cli_version)[:3]`;
- CC Gateway serializes final compact JSON bytes;
- CC Gateway inserts `cch=00000` placeholder;
- CC Gateway computes CCH using `xxh64(body_with_cch_00000, 0x4d659218e32a3268) & 0xFFFFF`;
- CC Gateway emits lowercase zero-padded 5-hex CCH;
- post-sign verifier passes;
- no post-sign mutation occurs;
- failure fails closed before egress.

---

## 8. Success criteria

### 8.1 `strip-controlled` baseline success

Baseline sanity succeeds only if:

1. exactly one approved route family is active: native `/v1/messages`;
2. account and route allowlists match the approval;
3. egress bucket check passes;
4. CC Gateway is the only final-output layer;
5. strip verifier confirms no billing/CCH material remains;
6. `count_tokens` blocks/defer result is observed;
7. event logging routes are suppressed or blocked as configured;
8. OpenAI-compatible Anthropic and Antigravity routes remain disabled/excluded;
9. no native direct fallback occurs;
10. logs/metrics are redacted and include lane, policy version, account hash, bucket, and reason;
11. rollback can move the account/route to `disabled` or paused.

### 8.2 `sign-primary` canary success

Sign-primary canary succeeds only if:

1. all baseline success criteria remain true;
2. CC Gateway generates/normalizes billing block;
3. CC Gateway computes the `cc_version` suffix with the verified formula;
4. CC Gateway serializes final compact JSON bytes;
5. CC Gateway computes 5-hex CCH with seed `0x4d659218e32a3268`;
6. post-sign verifier passes;
7. post-sign mutation detector reports no mutation;
8. no automatic fallback to `strip`, direct upstream, or another account occurs;
9. request volume stays within approved extremely low limits;
10. no unexplained 400/401/403, security warning, abuse warning, or route spillover occurs;
11. safe deliverable contains only redacted summaries and hashes.

A successful sign-primary canary does not claim endpoint-complete signing readiness. It only supports the next controlled messages-only expansion decision.

---

## 9. Stop conditions

Stop the lane immediately and set the affected account/route to `disabled` or paused if any of the following occur:

1. any non-`/v1/messages` route reaches CC Gateway first-wave egress;
2. `count_tokens` is forwarded instead of blocked/deferred;
3. event logging is forwarded upstream instead of suppressed/blocked;
4. OpenAI-compatible Anthropic converted traffic enters first wave;
5. Antigravity traffic enters first wave;
6. missing or unexpected account identity;
7. missing or unexpected egress bucket;
8. policy version mismatch;
9. strip-controlled used without explicit approval, route allowlist, account allowlist, or reason;
10. sign-primary used without explicit approval, sign allowlist, or verifier;
11. sign verifier mismatch;
12. post-sign mutation detected;
13. raw secret, raw Authorization, raw email, raw account UUID, raw body, raw prompt, or raw CCH-bearing body appears in safe deliverables or logs;
14. CC Gateway emits control-plane error but Sub2API treats it as account ban/death, native fallback, or account failover;
15. unexpected 400/401/403 pattern, security warning, or abuse warning;
16. any operator cannot identify the active rollback path.

Do not continue canary traffic while diagnosing a stop condition.

---

## 10. Rollback plan

### 10.1 Default rollback target

Default rollback target for both lanes is:

- set account/route `billing_cch_mode=disabled`; or
- pause the account/route; or
- disable `gateway.cc_gateway.providers.anthropic` for the canary cohort only.

Rollback must not silently enable native direct upstream.

### 10.2 `sign-primary` rollback

On sign anomaly:

1. activate `shared_pool.sign_primary_kill_switch` or disable `shared_pool.sign_primary_enabled`;
2. remove the account from sign allowlist;
3. set account/route to `disabled` or paused;
4. preserve redacted verifier results, policy version, lane, account hash, and bucket id;
5. do not replay the failed request through `strip-controlled`;
6. switch future traffic to `strip-controlled` only after separate manual approval with reason.

### 10.3 `strip-controlled` rollback

On strip anomaly:

1. activate `shared_pool.strip_controlled_kill_switch` or disable `shared_pool.strip_controlled_enabled`;
2. remove the account from strip allowlist;
3. set account/route to `disabled` or paused;
4. preserve redacted strip verifier results, policy version, lane, account hash, bucket id, and reason.

### 10.4 Rollback verification

After rollback, verify:

- `/v1/messages` shared-pool route is disabled or paused for the affected account;
- `count_tokens` remains blocked/deferred;
- event logging remains suppress/block;
- OpenAI-compatible Anthropic routes remain excluded;
- Antigravity remains disabled;
- no request is sent through native direct fallback;
- logs are redacted;
- the safe deliverable records rollback time, reason, policy version, and lane.

---

## 11. Logs, metrics, and redaction

### 11.1 Required logs/metrics

Record redacted metrics for:

- lane: `strip-controlled`, `sign-primary`, or `disabled`;
- route;
- policy version;
- selected account id hash;
- egress bucket id;
- request count;
- retry count;
- verifier pass/fail;
- control-plane error kind/code;
- blocked route count;
- suppressed event route count;
- rollback/kill-switch activation;
- redaction scan result.

### 11.2 Prohibited log/safe-deliverable content

Never log or write to safe deliverables:

- raw token;
- raw Authorization;
- email address;
- account UUID;
- raw CCH-bearing body;
- raw body;
- raw prompt text;
- proxy credentials;
- OAuth refresh token;
- setup token.

### 11.3 Redaction scan

Before attaching rollout evidence to docs, run a sensitive-pattern scan for at least:

- Anthropic token patterns;
- Authorization header values;
- bearer tokens;
- emails;
- UUIDs;
- non-placeholder `cch=` values;
- raw prompt/body markers;
- proxy URLs with credentials.

Any hit stops evidence publication until the artifact is corrected or moved to a raw-only, non-committed location.

---

## 12. Human approval gates

### 12.1 Approval before strip-controlled baseline

Operator must approve:

- account id hash;
- route `/v1/messages`;
- egress bucket id;
- policy version;
- `billing_cch_mode=strip`;
- reason;
- request count limit;
- rollback command/config path.

### 12.2 Approval before sign-primary canary

Operator must approve:

- account id hash;
- route `/v1/messages`;
- egress bucket id;
- policy version;
- `billing_cch_mode=sign`;
- request count limit;
- signer/verifier config;
- stop conditions;
- rollback command/config path;
- confirmation that automatic `sign` -> `strip` fallback is disabled.

### 12.3 Approval before expansion

Expansion after the first sign-primary canary requires a new approval. Expansion may increase only one dimension at a time:

- account count;
- egress bucket count;
- request rate;
- API-key passthrough inclusion if not already included;
- request shape complexity.

No expansion may include `count_tokens`, event logging upstream forwarding, OpenAI-compatible Anthropic routes, or Antigravity without a separate future design.

---

## 13. Evidence deliverables

For each lane, create a safe deliverable containing:

- lane;
- route;
- account id hash;
- egress bucket id;
- policy version;
- manual approval id hash or redacted reference;
- request count and retry count;
- header key order/value summary;
- body key summary and body hash;
- metadata/session field names and hashes;
- billing block presence boolean;
- CCH presence boolean;
- verifier result;
- blocked/deferred `count_tokens` result;
- suppress/block `event_logging` result;
- OpenAI-compatible Anthropic route exclusion result;
- Antigravity exclusion result;
- control-plane negative cases;
- rollback test result;
- redaction scan result.

Safe deliverables must not include raw secrets, raw prompts, raw bodies, raw account identifiers, or raw CCH-bearing bytes.

---

## 14. Full signing-mode boundary

This rollout plan does not claim full endpoint-complete signing-mode readiness.

Remaining future work still needs separate design and evidence, including but not limited to:

- `count_tokens` official natural trigger and request-shape evidence;
- event logging upstream schema and acceptance, if ever allowed;
- OpenAI-compatible Anthropic conversion route signing/readiness;
- Antigravity route family readiness;
- larger multi-account and multi-egress expansion;
- full endpoint-complete signing-mode design.

Until those are separately approved, first-wave remains native `/v1/messages` only.
