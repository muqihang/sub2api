# CC Gateway Formal-Pool Independent Safety P0 Remediation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the `Sub2API / Server API -> formal-pool scheduler -> CC Gateway -> Anthropic` native Claude path independently safe even when the user runs an unmodified upstream Claude Code CLI and no local Zhumeng takeover evidence exists.

**Architecture:** Sub2API owns end-user auth, formal-pool scheduling, sticky server-side session/account selection, and trusted internal context generation. CC Gateway owns the final outbound Claude native shape for the selected upstream account: account identity lookup, credential/account binding, egress eligibility, persona/profile selection, session/body/header binding, billing/CCH strip or sign verification, control-plane separation, and final-output verification before any upstream egress. The local Zhumeng-managed Claude Code runtime remains an enhancement layer only; it must not be required for the native formal-pool safety boundary to hold.

**Tech Stack:** CC Gateway TypeScript on `/Users/muqihang/chelingxi_workspace/cc-gateway` main branch at or after `c37a234`; Sub2API Go/Python worktree `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime`; local safe capture/reference docs under `docs/anti-ban/`; Node 22+/tsx tests; Go/Python focused tests.

## Global Constraints

- Execute CC Gateway changes on `/Users/muqihang/chelingxi_workspace/cc-gateway` **main** or a new worktree/branch based directly on main `c37a234+`; do not use stale `cc-gateway/.worktrees/claude-code-2173-main`.
- Do not touch Sub2API main checkout at `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main`.
- Do not stop, restart, rebuild, or modify port/container `3012`.
- Do not rebuild/restart canary `3017` until local targeted tests and this CC Gateway P0 safety gate are green.
- Never print, persist, or commit API keys, tokens, cookies, Authorization headers, raw prompts, raw request bodies, raw responses, raw telemetry, raw CCH values, account emails/UUIDs, or proxy credentials.
- Use safe refs, HMACs, hashes, buckets, booleans, schema names, status codes, and redacted summaries only.
- Do not delete files/directories, run `git reset`, `git clean`, `git rebase`, `git checkout --`, `git restore`, force push, `sudo`, `chmod -R`, or `chown -R` without explicit user approval.
- Preserve ordinary standalone single-user CC Gateway behavior unless it conflicts with formal-pool safety. The P0 rule is: standalone may remain for personal/single-account mode, but formal-pool/shared-account production must fail closed unless `mode: sub2api` and required safety evidence are present.
- Treat `docs/anti-ban/cch-algorithm.md` as legacy/background only. Current 2.1.172+/2.1.175 CCH source of truth is CC Gateway `docs/cch-2175-recovery-method.md`, `docs/cch-oracle-regression.md`, and oracle-backed tests.
- Claude Code 2.1.177 strict native mimicry and sign-primary remain disabled/fail-closed until a 2.1.177 oracle/profile or explicit compatibility proof is green.

---

## 0. Audit Synthesis and Current Gap Statement

### 0.1 User objective restated

The required baseline is not merely “the Zhumeng-managed local Claude Code CLI is safe.” The required baseline is:

```text
User / unmodified native Claude Code CLI
  -> Sub2API / Server API
     -> formal-pool scheduler selects one of the server-owned Anthropic OAuth/API-key accounts
     -> trusted server-side context is generated
  -> CC Gateway
     -> final native Claude outbound shape is constructed and verified
  -> Anthropic upstream
```

This path must remain safe even without local guard evidence, local route-trust artifacts, local replay cleaning, or local managed-runtime capability artifacts.

### 0.2 Evidence already found

CC Gateway main `c37a234` already contains important `sub2api` shared-pool capabilities:

- `src/proxy.ts` parses selected account/provider/token/egress/policy context and calls final-output gates before upstream forwarding.
- `src/policy.ts` resolves account identity, egress bucket, persona decision, and `metadata.user_id` session binding.
- `src/rewriter.ts` rewrites the shared-pool header allowlist and strips client-controlled native/persona/control headers from upstream forwarding.
- `tests/checkpoint3-remediation.test.ts`, `tests/proxy-sub2api.test.ts`, `tests/preflight-safety.test.ts`, `tests/security-boundary.test.ts`, and `tests/policy-cch.test.ts` cover many final-output, preflight, CCH, and no-bypass invariants.

### 0.3 P0 gaps found by main controller and three review agents

The current state is **PARTIAL**, not complete:

1. **Strict egress/account eligibility is incomplete.** `resolveEgressBucket()` currently only checks `allowed_account_ids` when the list exists and is non-empty. Missing or empty allowlist can unintentionally allow any selected account.
2. **Session-to-account/egress stickiness is not independently enforced inside CC Gateway.** A request can present the same session with a different account or egress bucket, and CC Gateway currently relies on Sub2API to prevent that.
3. **Credential/account binding is not independently checked inside CC Gateway.** CC Gateway preserves the selected upstream `authorization` or `x-api-key` based on `x-cc-token-type`, but it does not verify a safe credential ref/hash is bound to the declared account identity.
4. **Internal control-plane trust is too broad.** `x-sub2api-context-1m`, healthcheck persona override, and runtime registration depend mainly on gateway-token/client name and local checks; formal-pool control must require a stronger internal attestation path so an end client cannot influence persona/profile/account registration if it obtains or can use a gateway token.
5. **Runtime registration uses global `identity.device_id` for all accounts.** This is safe-degraded for a single-account gateway, but not enough for multi-account formal-pool native identity claims.
6. **Formal-pool + standalone is not fail-closed by config.** README/config default to standalone. This may remain acceptable for personal mode, but if formal-pool/shared-pool maps or mode flags are present, startup must reject `standalone`.
7. **Documentation and deployment gates are not explicit enough.** README and config examples still read as original standalone OAuth proxy first; they do not clearly mark `sub2api` as the only formal-pool/shared-account production mode.
8. **2.1.177 profile/CCH strict parity remains external evidence.** No P0 patch may enable sign-primary or claim strict 2.1.177 native mimicry.

### 0.4 High-spec review required edits folded into this plan

A GPT-5.5 xhigh review returned `PASS_WITH_REQUIRED_EDITS`. The required edits are accepted as technically correct and are now part of this plan:

1. Ordinary `x-cc-*` scheduler context is not authority by itself. The entire formal-pool context must be internally attested, including route class, account, token type, credential ref, egress bucket, proxy identity ref, policy version, persona/profile request, canonical session id, timestamp, and nonce. Missing, expired, replayed, malformed, or mismatched attestation fails closed before body/header rewrite and before upstream egress.
2. Credential/account binding is mandatory for formal-pool accounts. `credential_ref` is not trusted unless covered by the scheduler context attestation, and the selected raw credential must match an account-owned keyed binding using transient constant-time verification without logging raw credentials or raw digests.
3. A pure in-memory session ledger is not sufficient for production claims. Production formal-pool mode requires persistent/shared replay before accepting traffic, or an explicit single-instance/sticky admission mode that fails closed when unavailable. Capacity exhaustion must fail closed, never evict silently.
4. Static formal-pool config validation must reject incomplete account/egress/credential/device maps at startup.
5. Claude Code 2.1.177 sign-primary must have an executable fail-closed gate, not only documentation.
6. Config examples must be split so default personal standalone config does not contain active formal-pool maps that would become invalid after validation.

### 0.5 Normative Sub2API <-> CC Gateway formal-pool contract

For formal-pool/shared-account traffic, Sub2API and CC Gateway MUST implement the same closed contract.

Sub2API producer requirements:

1. Sub2API scheduler is the only authority that may produce formal-pool context. End-user/native Claude Code clients MUST NOT be allowed to supply, override, or pass through authority-bearing `x-cc-*`, `x-sub2api-*`, persona override, context-1m, runtime registration, account, credential, egress, policy, or session binding headers.
2. Sub2API MUST select the formal-pool tuple from server-side scheduler state only:
   - `route_class`
   - `account_id`
   - `token_type`
   - `credential_ref`
   - selected raw credential source
   - `egress_bucket`
   - `proxy_identity_ref`
   - `policy_version`
   - `persona_profile`
   - canonical `session_id`
   - timestamp
   - nonce
3. Sub2API MUST generate a canonical attested context over that tuple using a secret unavailable to end users and independent from ordinary gateway/client tokens.
4. Sub2API MUST persist or otherwise enforce its sticky session/account/credential/egress/persona policy before sending traffic to CC Gateway.
5. The attested context MUST NOT contain raw credentials, raw account UUID/email, raw proxy credentials, raw prompts, raw bodies, raw CCH, or raw telemetry.

CC Gateway consumer requirements:

1. CC Gateway MUST verify the attested context before trusting any authority-bearing field, before body/header rewrite, and before upstream egress.
2. CC Gateway MUST compare request headers/body/session data to the attested context exactly. Any mismatch in `account_id`, `credential_ref`, selected raw credential binding, `egress_bucket`, `proxy_identity_ref`, `policy_version`, `persona_profile`, or `session_id` fails closed.
3. CC Gateway MUST strip all formal-pool context, attestation, internal-control, and scheduler headers before upstream forwarding.
4. CC Gateway MUST maintain a session ledger consistent with the Sub2API sticky tuple and fail closed on account/credential/egress/policy/persona/device/session changes.
5. Both repos MUST share safe fixture vectors for valid context, expired context, replayed nonce, and one-field mismatch cases.

---

## 1. Reference Materials to Read Before Implementing

- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/docs/anti-ban/53-claude-code-native-safety-and-multiprovider-final-gap-remediation-plan.md`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/docs/anti-ban/45-claude-code-custom-base-url-capability-delta.md`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/docs/anti-ban/46-zhumeng-agent-claude-code-native-baseline-memo.md`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/docs/anti-ban/47-claude-code-control-plane-classification-matrix.md`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/docs/anti-ban/51-formal-pool-claude-code-persona-safety-gap-analysis.md`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/docs/anti-ban/captures/real-baseline/2026-06-14-sub2api-cc-gateway-joint-local-capture/safe-deliverable/README.md`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/docs/anti-ban/captures/real-baseline/2026-06-20-sub2api-cc-gateway-joint-local-capture/safe-deliverable/README.md`
- `/Users/muqihang/chelingxi_workspace/cc-gateway/docs/cch-2175-recovery-method.md`
- `/Users/muqihang/chelingxi_workspace/cc-gateway/docs/cch-oracle-regression.md`
- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/proxy.ts`
- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/policy.ts`
- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/rewriter.ts`
- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/config.ts`
- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/persona-resolver.ts`
- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/persona-registry.ts`

---

## 2. File/Module Ownership Map

### CC Gateway target repo

`/Users/muqihang/chelingxi_workspace/cc-gateway` on main-based worktree/branch.

- Modify: `src/config.ts`
  - Formal-pool mode validation.
  - Shared-pool required config checks.
  - Optional helper for strict shared-pool mode detection.
- Modify: `src/policy.ts`
  - Egress allowlist fail-closed semantics.
  - Mandatory account identity fields for account/device/credential refs.
  - Pure helpers for safe identity/credential refs, credential binding verification, attested context validation, and session binding key creation.
- Modify: `src/proxy.ts`
  - Attested formal-pool scheduler context verification for all `x-cc-*` authority fields.
  - Persistent/shared session/account/credential/egress sticky ledger or fail-closed production admission.
  - Stronger internal attestation for runtime registration and trusted context/profile headers.
  - Mandatory credential/account binding verification before final-output verifier.
  - Per-account device_id runtime registration or explicit fail-closed rule.
- Modify: `src/rewriter.ts`
  - Preserve only the selected credential after credential/account binding passes; no additional allowlist broadening.
- Modify: `src/upstream-safety.ts` only if production/preflight mode needs a formal-pool mode check not possible in `config.ts`.
- Modify: `config.example.yaml`
  - Keep standalone documented for personal mode if desired.
  - Keep default example standalone/personal only, without active formal-pool maps.
  - Add a dedicated `config.sub2api.formal-pool.example.yaml` for `mode: sub2api` with strict account/egress/credential mapping.
- Modify: `README.md`
  - Add explicit “Formal-pool/Sub2API mode” safety section.
  - State standalone is forbidden as a shared formal-pool production boundary.
  - State scripts/quick setup are standalone/personal unless using the Sub2API integration path.
- Add or modify tests:
  - `tests/config.test.ts`
  - `tests/proxy-sub2api.test.ts`
  - `tests/checkpoint3-remediation.test.ts`
  - `tests/preflight-safety.test.ts`
  - `tests/security-boundary.test.ts`
  - New test file only if keeping existing files too large, e.g. `tests/formal-pool-boundary.test.ts`.

### Sub2API producer-side target files

Exact files must be confirmed with CodeGraph before implementation, but the task owns the modules that select formal-pool Anthropic accounts and build the CC Gateway upstream request, likely including:

- Modify: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend/internal/service/cc_gateway_adapter.go`
- Modify: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend/internal/service/gateway_service.go`
- Modify: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend/internal/service/claude_code_formal_pool_coherence_cp39.go`
- Modify/Add tests under `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend/internal/service/` for producer-side attested context, header stripping, sticky tuple, and redaction.
- Add shared safe fixture vectors under `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend/internal/service/testdata/cc_gateway_formal_pool_contract/` or equivalent.

### Sub2API current worktree docs

- Modify after CC Gateway plan/implementation status changes:
  - `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/docs/anti-ban/55-claude-code-native-safety-and-multiprovider-final-evidence-report.md`
  - `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/.superpowers/sdd/final-evidence-map.md`

---

## 3. Implementation Tasks

### Task 1: Establish Correct CC Gateway Baseline, Mode Gate, and Static Formal-Pool Config Validation

**Files:**
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/src/config.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/config.test.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/config.example.yaml`
- Create: `/Users/muqihang/chelingxi_workspace/cc-gateway/config.sub2api.formal-pool.example.yaml`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/README.md`

**Interfaces:**
- Produces: `hasFormalPoolConfig(config: Config): boolean`
- Produces: `validateFormalPoolMode(config: Config): void`
- Produces: `validateFormalPoolAccountIdentity(accountId: string, identity: AccountIdentityConfig): void`
- Produces: `validateFormalPoolEgressBucket(bucketId: string, bucket: EgressBucketConfig): void`
- Consumes: existing `loadConfig(configPath?: string): Config`

- [ ] **Step 1: Write failing config tests**

Add tests to `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/config.test.ts`:

```ts
test('standalone rejects formal-pool shared account maps', () => {
  const path = writeConfigYaml(configYaml(`
mode: standalone
shared_pool:
  max_body_bytes: 2097152
account_identities:
  account-a:
    device_id: ${'a'.repeat(64)}
    account_uuid_ref: opaque:account-ref:v1:acct-a
    persona_variant: claude-code-2.1.175-macos-local
    session_policy: preserve_downstream_session_id
    policy_version: 2.1.175
egress_buckets:
  bucket-a:
    enabled: true
    proxy_url: http://127.0.0.1:8080
    proxy_identity_ref: opaque:proxy-ref:v1:bucket-a
    allowed_account_ids: [account-a]
`))
  assert.throws(() => loadConfig(path), /formal-pool.*sub2api/i)
})

test('sub2api formal-pool requires account identities and egress buckets', () => {
  const path = writeConfigYaml(configYaml(`
mode: sub2api
auth:
  gateway_token: gateway-token
shared_pool:
  max_body_bytes: 2097152
`).replace(/oauth:\n  refresh_token: refresh-token\n/, ''))
  assert.throws(() => loadConfig(path), /account_identities.*egress_buckets/i)
})
```

- [ ] **Step 2: Run tests and verify failure**

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway
npm test -- tests/config.test.ts
```

Expected before implementation: the new tests fail because `loadConfig()` defaults/allows standalone formal-pool-like config and does not require sub2api maps.

- [ ] **Step 3: Implement config validation**

In `/Users/muqihang/chelingxi_workspace/cc-gateway/src/config.ts`, add pure helpers near `loadConfig()`:

```ts
function hasOwnKeys(value: unknown): boolean {
  return !!value && typeof value === 'object' && Object.keys(value as Record<string, unknown>).length > 0
}

export function hasFormalPoolConfig(config: Config): boolean {
  return hasOwnKeys((config as any).shared_pool)
    || hasOwnKeys((config as any).account_identities)
    || hasOwnKeys((config as any).egress_buckets)
}

export function validateFormalPoolMode(config: Config) {
  const formalPool = hasFormalPoolConfig(config)
  if (formalPool && config.mode !== 'sub2api') {
    throw new Error('config: formal-pool/shared-account configuration requires mode: sub2api; standalone is forbidden for formal-pool production')
  }
  if (config.mode === 'sub2api') {
    if (!hasOwnKeys((config as any).account_identities) || !hasOwnKeys((config as any).egress_buckets)) {
      throw new Error('config: sub2api formal-pool mode requires account_identities and egress_buckets')
    }
  }
}
```

Call `validateFormalPoolMode(config)` after `mode` and `providers/auth` defaults are applied and before returning.

Also validate every formal-pool map at startup. Reject if any of these are true:

- `account_identities` is missing or empty in `mode: sub2api`;
- `egress_buckets` is missing or empty in `mode: sub2api`;
- account `device_id` is not exactly 64 hex characters;
- account lacks a safe account ref and mandatory credential binding;
- account credential binding is a raw token, plain digest, UUID, email, proxy credential, or contains newlines;
- egress bucket lacks explicit safe `proxy_identity_ref`;
- egress bucket lacks explicit non-empty `allowed_account_ids`;
- any safe ref contains raw UUID/email/token/proxy material or newline characters.

Add negative tests for each validation family in `tests/config.test.ts`.

Formal-pool detection MUST reject standalone only when formal-pool/shared-account material is present, such as non-empty `account_identities`, non-empty `egress_buckets`, formal-pool credential bindings, formal-pool context attestation config, or shared-pool production account/egress settings. If any benign standalone-only `shared_pool` fields remain supported, they MUST NOT make personal standalone invalid. Add a regression test proving ordinary personal standalone still loads.

- [ ] **Step 4: Split config examples without breaking personal standalone docs**

In `/Users/muqihang/chelingxi_workspace/cc-gateway/config.example.yaml`, keep the default example personal/standalone only. It must not contain active `shared_pool`, `account_identities`, or `egress_buckets` maps that make the example invalid after formal-pool validation. Change the mode comments to clearly state:

````yaml
# Gateway mode:
# - standalone: personal/single-account OAuth proxy only. Do not use as a
#   shared formal-pool production boundary.
# - sub2api: required for Sub2API formal-pool/shared-account production.
#   Sub2API selects the account and CC Gateway performs final-output safety.
mode: standalone
````

Create `/Users/muqihang/chelingxi_workspace/cc-gateway/config.sub2api.formal-pool.example.yaml` with a redacted, safe, loadable `mode: sub2api` formal-pool example. It must include:

- `auth.gateway_token: change-me-gateway-token` and `auth.internal_control_token: change-me-independent-internal-control-token`;
- `shared_pool.billing_cch_mode: strip`;
- `shared_pool.upstream_mode: preflight`;
- one sample `account_identities` entry with 64-hex sample `device_id`, safe `account_uuid_ref`, safe `credential_ref`, and keyed `credential_binding_hmac` placeholder;
- one sample `egress_buckets` entry with explicit safe `proxy_identity_ref` and non-empty `allowed_account_ids`;
- comments saying placeholder values must be replaced by server-generated safe refs/HMACs and never raw account UUID/email/token/proxy credentials.

- [ ] **Step 5: Update README formal-pool mode section**

In `/Users/muqihang/chelingxi_workspace/cc-gateway/README.md`, under “Gateway Modes”, add:

````md
> **Formal-pool safety:** `standalone` is only for personal/single-account gateway use. It is forbidden as the shared Anthropic formal-pool production boundary. Formal-pool traffic must use `mode: sub2api`, where Sub2API authenticates the end user and selects the server-owned account while CC Gateway independently verifies account identity, egress, persona/session, billing/CCH, and final-output shape before upstream egress.
````

Also clarify that quick/admin setup scripts are standalone-personal unless the Sub2API integration path is explicitly configured.

- [ ] **Step 6: Run focused config test**

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway
npm test -- tests/config.test.ts
```

Expected: PASS. If the sandbox blocks `tsx` IPC/listen, run the narrow equivalent with `node --import tsx tests/run-all.ts tests/config.test.ts` or record exact `EPERM` and have the user run it in normal Terminal.

---

### Task 2: Make Egress Eligibility Fail Closed

**Files:**
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/src/policy.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/checkpoint3-remediation.test.ts` or `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/proxy-sub2api.test.ts`

**Interfaces:**
- Consumes: `resolveEgressBucket(config: Config, bucketId: string | undefined, accountId: string | undefined)`
- Produces behavior: missing or empty `allowed_account_ids` fails closed for `sub2api` formal-pool egress buckets.

- [ ] **Step 1: Write failing pure tests**

Add to `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/checkpoint3-remediation.test.ts` near existing egress tests:

```ts
test('egress bucket requires explicit non-empty account allowlist', () => {
  const config = sharedConfig()
  config.egress_buckets!['bucket-no-allowlist'] = {
    ...config.egress_buckets!['bucket-a'],
    allowed_account_ids: undefined,
  } as any
  config.egress_buckets!['bucket-empty-allowlist'] = {
    ...config.egress_buckets!['bucket-a'],
    allowed_account_ids: [],
  } as any

  assert.deepEqual(resolveEgressBucket(config, 'bucket-no-allowlist', 'account-a'), { error: 'missing_egress_account_allowlist' })
  assert.deepEqual(resolveEgressBucket(config, 'bucket-empty-allowlist', 'account-a'), { error: 'missing_egress_account_allowlist' })
  assert.deepEqual(resolveEgressBucket(config, 'bucket-a', 'account-denied'), { error: 'egress_bucket_account_denied' })
})
```

- [ ] **Step 2: Run the test and verify failure**

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway
npm test -- tests/checkpoint3-remediation.test.ts
```

Expected before implementation: the new missing/empty allowlist assertions fail because the current code only checks a non-empty list.

- [ ] **Step 3: Implement fail-closed allowlist**

In `/Users/muqihang/chelingxi_workspace/cc-gateway/src/policy.ts`, replace:

```ts
if (bucket.allowed_account_ids?.length && (!accountId || !bucket.allowed_account_ids.includes(accountId))) {
  return { error: 'egress_bucket_account_denied' }
}
```

with:

```ts
if (!Array.isArray(bucket.allowed_account_ids) || bucket.allowed_account_ids.length === 0) {
  return { error: 'missing_egress_account_allowlist' }
}
if (!accountId || !bucket.allowed_account_ids.includes(accountId)) {
  return { error: 'egress_bucket_account_denied' }
}
```

- [ ] **Step 4: Run focused test**

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway
npm test -- tests/checkpoint3-remediation.test.ts
```

Expected: PASS.

---

### Task 3A: Implement and Test Sub2API Producer-Side Formal-Pool Context

**Files:**
- Modify: Sub2API formal-pool scheduler / dispatch modules that select server-owned Anthropic accounts. Confirm exact symbols with CodeGraph before editing.
- Modify/Add: Sub2API tests for scheduler context generation, header stripping, sticky tuple, and redaction.
- Add: shared safe fixture vectors under `backend/internal/service/testdata/cc_gateway_formal_pool_contract/` or an equivalent test fixture path.

**Requirements:**

- [ ] Sub2API MUST discard or reject client-supplied authority-bearing `x-cc-*`, `x-sub2api-*`, persona override, context-1m, runtime registration, account, credential, egress, policy, and session binding headers.
- [ ] Sub2API scheduler MUST select `account_id`, `credential_ref`, selected raw credential source, `egress_bucket`, `proxy_identity_ref`, `policy_version`, `persona_profile`, and canonical `session_id` from trusted server-side state only.
- [ ] Sub2API MUST generate the formal-pool attested context after sticky account/session/egress selection and before calling CC Gateway.
- [ ] Sub2API MUST sign the canonical context with an internal secret unavailable to end users and distinct from ordinary gateway/client tokens.
- [ ] Sub2API MUST include safe tests proving:
  - valid scheduler context is accepted by a CC Gateway fixture verifier;
  - user-supplied authority headers are ignored/rejected;
  - same user/session keeps the same account/credential/egress/persona tuple according to sticky policy;
  - no raw credential, raw account UUID/email, proxy credential, raw prompt, raw body, raw CCH, or raw telemetry is logged or emitted in evidence.

### Task 3: Require Trusted Formal-Pool Scheduler Context Attestation

**Files:**
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/src/config.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/src/proxy.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/proxy-sub2api.test.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/security-boundary.test.ts`

**Interfaces:**
- Produces: `verifyFormalPoolContextAttestation(req, config, parsedContext, sessionId): { ok: true; context: AttestedFormalPoolContext } | { ok: false; status: number; code: string }`
- Consumes: all authority-bearing fields: route class, account id, token type, credential ref, egress bucket, proxy identity ref, policy version, persona/profile request, canonical session id, timestamp, nonce.

- [ ] **Step 1: Write failing tests for unattested `x-cc-*` context**

Add tests proving `mode: sub2api` rejects formal-pool messages when ordinary gateway auth is present but scheduler context attestation is absent or mismatched:

```ts
test('sub2api rejects unattested scheduler x-cc context before rewrite', async () => {
  const upstream = await startFakeUpstream()
  const proxy = await startFakeConnectProxy()
  const config = sub2apiConfig(upstream.url, proxy.url)
  ;(config.auth as any).internal_control_token = 'independent-internal-token'
  const gateway = startProxy(config)

  try {
    const response = await httpJson(serverUrl(gateway, '/v1/messages?beta=true'), {
      headers: {
        ...ccGatewayHeaders,
        'x-cc-gateway-token': 'gateway-token',
        'x-cc-provider': 'anthropic',
        'x-cc-account-id': 'account-1',
        'x-cc-egress-bucket': 'bucket-a',
        'x-cc-token-type': 'oauth',
        'x-cc-policy-version': '2.1.175',
        'x-cc-credential-ref': 'opaque:credential-ref:v1:cred-a',
        authorization: 'Bearer selected-token',
      },
      body: { metadata: {}, messages: [{ role: 'user', content: 'hello' }] },
    })
    assert.equal(response.status, 403)
    assert.equal(response.headers['x-cc-gateway-error-code'], 'missing_formal_pool_context_attestation')
    assert.equal(upstream.captured.length, 0)
  } finally {
    await close(gateway)
    await close(upstream.server)
    await close(proxy.server)
  }
})
```

Add a second test where attested `account_id` is `account-1` but header `x-cc-account-id` is `account-2`; expect `formal_pool_context_mismatch` and no upstream request.

Add negative tests for each attested-field mismatch independently:

- `account_id`
- `credential_ref`
- selected raw credential binding
- `egress_bucket`
- `proxy_identity_ref`
- `policy_version`
- `persona_profile`
- canonical `session_id`
- `route_class`
- timestamp expiry
- nonce replay

Each mismatch MUST fail closed before rewrite and before upstream egress, with no captured upstream request.

- [ ] **Step 2: Implement independent attestation config validation**

Add `auth.internal_control_token` or `shared_pool.context_attestation_secret_ref` support. Startup must reject formal-pool configs where the internal attestation secret is missing, low-entropy placeholder in production mode, equal to `auth.gateway_token`, or equal to any `auth.tokens[].token`. Do not log the secret.

- [ ] **Step 3: Implement attestation verification before account trust**

Implement HMAC verification over a canonical safe JSON object containing:

```json
{
  "method": "POST",
  "route_class": "messages",
  "path": "/v1/messages",
  "account_id": "account-1",
  "token_type": "oauth",
  "credential_ref": "opaque:credential-ref:v1:cred-a",
  "egress_bucket": "bucket-a",
  "proxy_identity_ref": "opaque:proxy-ref:v1:bucket-a",
  "policy_version": "2.1.175",
  "persona_profile": "claude-code-2.1.175-macos-local",
  "session_id": "canonical-uuid",
  "timestamp_ms": 0,
  "nonce": "safe-nonce"
}
```

The actual field values must come from Sub2API scheduler state. The signature header and nonce must be stripped before upstream. Reject expired timestamps and replayed nonces. If persistent replay storage is not available, production mode must fail closed; replay-in-memory is allowed only for local/preflight tests.

- [ ] **Step 4: Wire attestation before body/header rewrite**

Call attestation verification after request body/session id parsing but before `resolveAccountIdentity()` is treated as trusted for final output. Header values must match the attested context exactly. Missing/mismatch fails closed before rewrite and before upstream egress.

- [ ] **Step 5: Run focused tests**

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway
npm test -- tests/proxy-sub2api.test.ts tests/security-boundary.test.ts
```

Expected: PASS.

### Task 4: Add CC Gateway Persistent Session/Account/Credential/Egress Sticky Ledger

**Files:**
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/src/proxy.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/proxy-sub2api.test.ts` or create `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/formal-pool-boundary.test.ts`

**Interfaces:**
- Produces: `verifySessionBindingLedger(sessionId: string, context: AccountContext, identity: AccountIdentityRecord, egress: EgressBucketResolution, credentialRef: string): { ok: true } | { ok: false; code: string }`
- Consumes: existing `sessionId`, attested `accountContext`, `accountIdentity`, `egress`, and mandatory `credentialRef` in `handleRequest()`.
- Production requirement: ledger must be persistent or shared. In-memory ledger is allowed only for tests/local capture/preflight and must not be used for production claims. Capacity exhaustion must fail closed.

The ledger tuple MUST include `credentialRef` and `personaProfile`, and the verification call MUST pass the attested credential ref. The ledger key MUST be the canonical `session_id` from the verified formal-pool attested context, not a merely client-controlled header/body value. A mismatch in account, account ref, credential ref, token type, egress bucket, proxy identity ref, policy version, persona profile, or device id MUST fail closed before upstream egress.

- [ ] **Step 1: Write failing integration test for account swap**

Add an integration test with local fake upstream/proxy fixtures:

```ts
test('sub2api shared-pool fails closed when same session swaps account or egress', async () => {
  const upstream = await startFakeUpstream()
  const proxy = await startFakeConnectProxy()
  const config = sub2apiConfig(upstream.url, proxy.url)
  config.account_identities!['account-2'] = {
    ...config.account_identities!['account-1'],
    device_id: 'c'.repeat(64),
    account_uuid_ref: 'opaque:account-ref:v1:acct-2',
  }
  config.egress_buckets!['bucket-b'] = {
    ...config.egress_buckets!['bucket-a'],
    allowed_account_ids: ['account-2'],
  }
  const gateway = startProxy(config)
  const sessionId = '123e4567-e89b-42d3-a456-426614174999'

  try {
    const first = await httpJson(serverUrl(gateway, '/v1/messages?beta=true'), {
      headers: {
        ...ccGatewayHeaders,
        'x-cc-gateway-token': 'gateway-token',
        'x-cc-provider': 'anthropic',
        'x-cc-account-id': 'account-1',
        'x-cc-egress-bucket': 'bucket-a',
        'x-cc-token-type': 'oauth',
        'x-cc-policy-version': '2.1.175',
        'x-claude-code-session-id': sessionId,
        authorization: 'Bearer selected-token-a',
      },
      body: { metadata: { user_id: JSON.stringify({ session_id: sessionId }) }, messages: [{ role: 'user', content: 'hello' }] },
    })
    assert.equal(first.status, 200, first.body)

    const swapped = await httpJson(serverUrl(gateway, '/v1/messages?beta=true'), {
      headers: {
        ...ccGatewayHeaders,
        'x-cc-gateway-token': 'gateway-token',
        'x-cc-provider': 'anthropic',
        'x-cc-account-id': 'account-2',
        'x-cc-egress-bucket': 'bucket-b',
        'x-cc-token-type': 'oauth',
        'x-cc-policy-version': '2.1.175',
        'x-claude-code-session-id': sessionId,
        authorization: 'Bearer selected-token-b',
      },
      body: { metadata: { user_id: JSON.stringify({ session_id: sessionId }) }, messages: [{ role: 'user', content: 'swap' }] },
    })
    assert.equal(swapped.status, 409)
    assert.equal(swapped.headers['x-cc-gateway-error-kind'], 'control-plane')
    assert.equal(swapped.headers['x-cc-gateway-error-code'], 'session_account_egress_changed')
    assert.equal(upstream.captured.length, 1)
  } finally {
    await close(gateway)
    await close(upstream.server)
    await close(proxy.server)
  }
})
```

- [ ] **Step 2: Run and verify failure**

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway
npm test -- tests/proxy-sub2api.test.ts
```

Expected before implementation: second request incorrectly reaches upstream or returns a non-409 status.

- [ ] **Step 3: Implement in-memory sticky ledger**

In `/Users/muqihang/chelingxi_workspace/cc-gateway/src/proxy.ts`, define near module-level state:

```ts
type FormalPoolSessionBinding = {
  accountId: string
  accountRef: string
  egressBucket: string
  proxyIdentityRef: string
  tokenType: 'oauth' | 'apikey'
  policyVersion: string
  deviceId: string
}

const formalPoolSessionLedger = new Map<string, FormalPoolSessionBinding>()

function verifySessionBindingLedger(
  sessionId: string,
  context: AccountContext,
  identity: AccountIdentityRecord,
  egress: EgressBucketResolution,
  credentialRef: string,
): { ok: true } | { ok: false; code: string } {
  const next: FormalPoolSessionBinding = {
    accountId: context.accountId,
    accountRef: accountIdentityRef(identity),
    egressBucket: context.egressBucket,
    proxyIdentityRef: egress.proxyIdentityRef,
    tokenType: context.tokenType,
    credentialRef,
    policyVersion: context.policyVersion,
    deviceId: identity.device_id,
  }
  const prev = formalPoolSessionLedger.get(sessionId)
  if (!prev) {
    formalPoolSessionLedger.set(sessionId, next)
    return { ok: true }
  }
  if (
    prev.accountId !== next.accountId
    || prev.accountRef !== next.accountRef
    || prev.egressBucket !== next.egressBucket
    || prev.proxyIdentityRef !== next.proxyIdentityRef
    || prev.tokenType !== next.tokenType
    || prev.credentialRef !== next.credentialRef
    || prev.policyVersion !== next.policyVersion
    || prev.deviceId !== next.deviceId
  ) {
    return { ok: false, code: 'session_account_egress_changed' }
  }
  return { ok: true }
}
```

After `sessionId` is resolved and after `egress` is known, but before body/header rewrite and before upstream forwarding, call:

```ts
if (config.mode === 'sub2api' && accountContext && accountIdentity && egress && sessionId) {
  const binding = verifySessionBindingLedger(sessionId, accountContext, accountIdentity, egress)
  if (!binding.ok) {
    writeControlPlaneError(res, 409, binding.code, 'Formal-pool session attempted to change account, credential, policy, device, or egress binding')
    return
  }
}
```

- [ ] **Step 4: Add bounded cleanup if needed**

If reviewers object to unbounded memory growth, add a minimal cap:

```ts
const MAX_FORMAL_POOL_SESSION_LEDGER_ENTRIES = 10000

function rememberFormalPoolSession(sessionId: string, binding: FormalPoolSessionBinding): { ok: true } | { ok: false; code: string } {
  if (!formalPoolSessionLedger.has(sessionId) && formalPoolSessionLedger.size >= MAX_FORMAL_POOL_SESSION_LEDGER_ENTRIES) {
    return { ok: false, code: 'formal_pool_session_ledger_capacity_exceeded' }
  }
  formalPoolSessionLedger.set(sessionId, binding)
  return { ok: true }
}
```

Use it instead of direct `set()`.

- [ ] **Step 5: Run focused test**

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway
npm test -- tests/proxy-sub2api.test.ts
```

Expected: PASS.

---

### Task 5: Bind Selected Credential to Account Without Logging Secrets

**Files:**
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/src/config.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/src/policy.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/src/proxy.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/src/rewriter.ts` only if needed
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/proxy-sub2api.test.ts`

**Interfaces:**
- Add mandatory formal-pool account identity fields:
  - `credential_ref: string`
  - `credential_binding_hmac: string` or equivalent keyed binding
- Produces: `verifySelectedCredentialBinding(headers, tokenType, accountIdentity, attestedCredentialRef): { ok: true } | { ok: false; code: string }`.
- Produces behavior: every formal-pool account identity must declare credential binding; missing or mismatched binding fails closed before final-output verifier.

**Important:** `credential_ref` is not authority unless covered by the formal-pool context attestation. When computing a transient HMAC over the selected raw upstream credential, use constant-time comparison and never log, persist, capture, or print the raw credential or raw digest.

- [ ] **Step 1: Write failing credential/account mismatch test**

Add test:

```ts
test('sub2api selected credential ref must match selected account identity', async () => {
  const upstream = await startFakeUpstream()
  const proxy = await startFakeConnectProxy()
  const config = sub2apiConfig(upstream.url, proxy.url)
  config.account_identities!['account-1'] = {
    ...config.account_identities!['account-1'],
    credential_ref: 'opaque:credential-ref:v1:cred-a',
  } as any
  const gateway = startProxy(config)

  try {
    const response = await httpJson(serverUrl(gateway, '/v1/messages?beta=true'), {
      headers: {
        ...ccGatewayHeaders,
        'x-cc-gateway-token': 'gateway-token',
        'x-cc-provider': 'anthropic',
        'x-cc-account-id': 'account-1',
        'x-cc-egress-bucket': 'bucket-a',
        'x-cc-token-type': 'oauth',
        'x-cc-policy-version': '2.1.175',
        'x-cc-credential-ref': 'opaque:credential-ref:v1:cred-b',
        authorization: 'Bearer selected-token',
      },
      body: { messages: [{ role: 'user', content: 'hello' }] },
    })
    assert.equal(response.status, 403)
    assert.equal(response.headers['x-cc-gateway-error-code'], 'credential_account_mismatch')
    assert.equal(upstream.captured.length, 0)
  } finally {
    await close(gateway)
    await close(upstream.server)
    await close(proxy.server)
  }
})
```

Add a matching positive case with an attested `x-cc-credential-ref: opaque:credential-ref:v1:cred-a` and matching transient credential binding HMAC; expect `200`.

- [ ] **Step 2: Run and verify failure**

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway
npm test -- tests/proxy-sub2api.test.ts
```

Expected before implementation: mismatch is not rejected.

- [ ] **Step 3: Extend account identity types**

In `src/config.ts` `AccountIdentityConfig` and `src/policy.ts` `AccountIdentityRecord`, add:

```ts
credential_ref: string
credential_binding_hmac: string
```

Update `resolveAccountIdentity()` to reject unsafe refs:

```ts
const credentialRef = identity.credential_ref
if (!credentialRef || !isSafeIdentityRef(credentialRef)) return null
if (!identity.credential_binding_hmac || !isSafeIdentityRef(identity.credential_binding_hmac)) return null
```

Return the normalized `credential_ref` and `credential_binding_hmac`.

- [ ] **Step 4: Parse and verify credential ref**

In `src/proxy.ts`, add in `parseAccountContext()`:

```ts
const credentialRef = readHeader(req, 'x-cc-credential-ref')
```

Extend `AccountContext` with `credentialRef?: string`.

After `accountIdentity` resolution and before body rewrite:

```ts
const expectedCredentialRef = (accountIdentity as any).credential_ref
if (!expectedCredentialRef || !accountContext.credentialRef || accountContext.credentialRef !== expectedCredentialRef) {
  writeControlPlaneError(res, 403, 'credential_account_mismatch', 'Selected credential ref does not match the selected formal-pool account')
  return
}
const credentialBinding = verifySelectedCredentialBinding(req.headers, accountContext.tokenType, accountIdentity, accountContext.credentialRef)
if (!credentialBinding.ok) {
  writeControlPlaneError(res, 403, credentialBinding.code, 'Selected upstream credential does not match the selected formal-pool account')
  return
}
```

Also ensure `x-cc-credential-ref` is stripped before upstream by existing `stripGatewayControlHeaders` behavior.

- [ ] **Step 5: Run focused tests**

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway
npm test -- tests/proxy-sub2api.test.ts tests/checkpoint3-remediation.test.ts
```

Expected: PASS.

---

### Task 6: Harden Internal Control Headers and Runtime Registration

**Files:**
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/src/config.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/src/proxy.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/proxy-sub2api.test.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/session-and-beta-policy.test.ts`

**Interfaces:**
- Add config field under `auth` or `shared_pool`:
  - `internal_control_token?: string`
- Produces: `isTrustedInternalControl(req, config): boolean`
- Applies to:
  - `x-sub2api-context-1m`
  - `x-sub2api-healthcheck-persona`
  - `x-sub2api-persona-trusted`
  - `/_runtime/register-account`

Sub2API MUST NOT forward end-user supplied `x-sub2api-context-1m`, `x-sub2api-healthcheck-persona`, `x-sub2api-persona-trusted`, runtime registration, or internal-control headers. These may be generated only by trusted server-side/internal paths.

CC Gateway MUST require internal-control attestation plus an internal source boundary for those headers/endpoints. If the deployment cannot prove the request came from the internal path, CC Gateway MUST fail closed rather than treating the header as trusted.

- [ ] **Step 1: Write failing tests for spoofed internal headers**

Add tests:

```ts
test('sub2api ignores user-spoofed context-1m without internal control token', async () => {
  const upstream = await startFakeUpstream()
  const proxy = await startFakeConnectProxy()
  const config = sub2apiConfig(upstream.url, proxy.url)
  ;(config.auth as any).internal_control_token = 'internal-token'
  const gateway = startProxy(config)

  try {
    const response = await httpJson(serverUrl(gateway, '/v1/messages?beta=true'), {
      headers: {
        ...ccGatewayHeaders,
        'x-cc-gateway-token': 'gateway-token',
        'x-cc-provider': 'anthropic',
        'x-cc-account-id': 'account-1',
        'x-cc-egress-bucket': 'bucket-a',
        'x-cc-token-type': 'oauth',
        'x-cc-policy-version': '2.1.175',
        'x-sub2api-context-1m': 'true',
        authorization: 'Bearer selected-token',
      },
      body: { model: 'claude-sonnet-4-6', messages: [{ role: 'user', content: 'hello' }] },
    })
    assert.equal(response.status, 403)
    assert.equal(response.headers['x-cc-gateway-error-code'], 'missing_formal_pool_context_attestation')
  } finally {
    await close(gateway)
    await close(upstream.server)
    await close(proxy.server)
  }
})

test('runtime registration requires internal control token', async () => {
  const upstream = await startFakeUpstream()
  const proxy = await startFakeConnectProxy()
  const config = sub2apiConfig(upstream.url, proxy.url, { account_identities: {}, egress_buckets: {} })
  ;(config.auth as any).internal_control_token = 'internal-token'
  const gateway = startProxy(config)

  try {
    const response = await httpJson(serverUrl(gateway, '/_runtime/register-account'), {
      headers: { 'x-cc-gateway-token': 'gateway-token' },
      body: {
        account_id: 'runtime-account',
        account_ref: 'opaque:account-ref:v1:runtime-account',
        account_uuid_ref: 'opaque:account-ref:v1:runtime-account',
        egress_bucket: 'bucket-runtime',
        proxy_url: proxy.url,
        proxy_identity_ref: 'opaque:proxy-ref:v1:runtime-bucket',
        policy_version: '2.1.175',
      },
    })
    assert.equal(response.status, 403)
    assert.equal(response.headers['x-cc-gateway-error-code'], 'missing_internal_control_attestation')
  } finally {
    await close(gateway)
    await close(upstream.server)
    await close(proxy.server)
  }
})
```

- [ ] **Step 2: Implement internal control attestation**

In `src/config.ts`, add optional `internal_control_token?: string` to `auth`.

In `src/proxy.ts`, add:

```ts
const INTERNAL_CONTROL_HEADER = 'x-cc-internal-control-token'

function isTrustedInternalControl(req: IncomingMessage, config: Config): boolean {
  const expected = (config.auth as any).internal_control_token
  if (!expected) return false
  const actual = readHeader(req, INTERNAL_CONTROL_HEADER)
  return typeof actual === 'string' && actual === expected && isLocalRequest(req)
}
```

Change:

```ts
const trustedPersonaClient = isTrustedPersonaClient(req)
const requestedContext1M = readTrustedContext1MRequest(req, clientName)
const healthcheckPersonaProfile = readTrustedHealthcheckPersonaProfile(req, clientName)
```

to include `config` and internal attestation:

```ts
const trustedInternalControl = isTrustedInternalControl(req, config)
const trustedPersonaClient = isTrustedPersonaClient(req, trustedInternalControl)
const requestedContext1M = readTrustedContext1MRequest(req, clientName, trustedInternalControl)
const healthcheckPersonaProfile = readTrustedHealthcheckPersonaProfile(req, clientName, trustedInternalControl)
```

Require `trustedInternalControl` for runtime registration when `config.mode === 'sub2api'`:

```ts
if (target.pathname === RUNTIME_REGISTER_PATH) {
  if (config.mode === 'sub2api' && !isTrustedInternalControl(req, config)) {
    writeControlPlaneError(res, 403, 'missing_internal_control_attestation', 'Runtime account registration requires internal control attestation')
    return
  }
  await handleRuntimeRegister(req, res, config, method)
  return
}
```

- [ ] **Step 3: Update tests that legitimately register runtime accounts**

Where runtime registration tests should pass, include:

```ts
;(config.auth as any).internal_control_token = 'internal-token'
```

and request header:

```ts
'x-cc-internal-control-token': 'internal-token'
```

- [ ] **Step 4: Run focused tests**

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway
npm test -- tests/proxy-sub2api.test.ts tests/session-and-beta-policy.test.ts
```

Expected: PASS.

---

### Task 7: Account-Owned Device Identity for Runtime Registration

**Files:**
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/src/proxy.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/proxy-sub2api.test.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/config.example.yaml`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/README.md`

**Interfaces:**
- Runtime registration body gains required `device_id` for multi-account formal pool.
- Existing config-file `account_identities` already require `device_id`.

- [ ] **Step 1: Write failing runtime registration test**

Add test:

```ts
test('runtime registration requires account-owned device_id for formal-pool mapping', async () => {
  const upstream = await startFakeUpstream()
  const proxy = await startFakeConnectProxy()
  const config = sub2apiConfig(upstream.url, proxy.url, { account_identities: {}, egress_buckets: {} })
  ;(config.auth as any).internal_control_token = 'internal-token'
  const gateway = startProxy(config)

  try {
    const missing = await httpJson(serverUrl(gateway, '/_runtime/register-account'), {
      headers: { 'x-cc-gateway-token': 'gateway-token', 'x-cc-internal-control-token': 'internal-token' },
      body: {
        account_id: 'runtime-account',
        account_ref: 'opaque:account-ref:v1:runtime-account',
        account_uuid_ref: 'opaque:account-ref:v1:runtime-account',
        egress_bucket: 'bucket-runtime',
        proxy_url: proxy.url,
        proxy_identity_ref: 'opaque:proxy-ref:v1:runtime-bucket',
        policy_version: '2.1.175',
      },
    })
    assert.equal(missing.status, 400)
    assert.equal(missing.headers['x-cc-gateway-error-code'], 'missing_device_id')
  } finally {
    await close(gateway)
    await close(upstream.server)
    await close(proxy.server)
  }
})
```

- [ ] **Step 2: Implement device_id validation**

In runtime mapping type and normalization in `src/proxy.ts`, add:

```ts
device_id: string
```

Parse:

```ts
const deviceId = stringField(input.device_id)
if (!/^[a-f0-9]{64}$/i.test(deviceId)) {
  return { status: 400, code: 'missing_device_id', message: 'Runtime registration requires account-owned 64-hex device_id' }
}
```

In `applyRuntimeAccountMapping()`, replace:

```ts
device_id: String(config.identity.device_id || ''),
```

with:

```ts
device_id: mapping.device_id,
```

- [ ] **Step 3: Update positive runtime registration tests**

Add:

```ts
device_id: 'd'.repeat(64),
```

to runtime registration bodies and assert upstream `metadata.user_id.device_id` uses this per-account value where relevant.

- [ ] **Step 4: Update docs**

README/config example must say runtime registration requires a safe account-owned `device_id`; raw account UUID/email must not be logged.

- [ ] **Step 5: Run focused tests**

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway
npm test -- tests/proxy-sub2api.test.ts
```

Expected: PASS.

---

### Task 8: Reconcile with Captures and Control-Plane Materials

**Files:**
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/docs/formal-pool-sub2api-safety.md` or equivalent new doc
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/README.md`
- Modify: tests only if a missing control-plane path is discovered

**Interfaces:**
- Produces operator-facing evidence matrix for safe fields only.

- [ ] **Step 1: Create or update formal-pool safety doc**

Create `/Users/muqihang/chelingxi_workspace/cc-gateway/docs/formal-pool-sub2api-safety.md` with sections:

````md
# Formal-Pool Sub2API Safety Boundary

## Scope
This document covers `mode: sub2api` formal-pool/shared-account production. It does not make strict Claude Code 2.1.177 native-parity or sign-primary readiness claims.

## Required server-side context
- account ref
- credential type
- credential ref
- egress bucket
- persona/profile policy version
- route classification
- session binding
- control-plane disposition

## CC Gateway final-output responsibilities
- account identity lookup
- credential/account binding
- egress allowlist verification
- persona/profile header rewrite
- `metadata.user_id` rewrite and verifier
- `X-Claude-Code-Session-Id` body/header equality
- billing/CCH strip or sign verifier
- control-plane separation
- preflight/real-upstream gate

## Capture references
Use only safe summaries from Sub2API docs. Do not paste raw prompts, raw bodies, raw CCH, raw telemetry, account UUIDs/emails, tokens, or proxy credentials.

## Known degraded claims
- WebSearch/WebFetch bridge is not part of this P0.
- 2.1.177 strict native mimicry and sign-primary remain gated on oracle/profile evidence.
- Full first-party control-plane parity remains separate from safe stub/suppress/block behavior.
````

- [ ] **Step 2: Cross-check safe capture fields**

Read the safe capture docs listed in Section 1 and update the doc with a table of field families:

| Field family | Captured expectation | CC Gateway P0 behavior | Status |
|---|---|---|---|
| `metadata.user_id` | `device_id`, `account_uuid`, `session_id` | rewritten from selected account and session ledger | PASS/PARTIAL |
| session header | equals body `session_id` | final verifier checks equality | PASS |
| persona headers | UA/x-app/x-stainless/anthropic-beta | rewritten by resolver | PASS_WITH_PROFILE_GATE |
| billing/CCH | strip or sign | strip default, sign gated | PASS_WITH_DEGRADED_SCOPE |
| control-plane | separate from messages | suppress/defer/block | PASS_WITH_DEGRADED_SCOPE |
```

Do not copy raw request bodies.

- [ ] **Step 3: Update README with link**

Add a README pointer to the formal-pool doc from the Gateway Modes section.

---

### Task 9: Add Executable 2.1.177 Sign-Primary Fail-Closed Gate

**Files:**
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/src/persona-resolver.ts` or `/Users/muqihang/chelingxi_workspace/cc-gateway/src/policy.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/policy-cch.test.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/checkpoint3-remediation.test.ts`

**Interfaces:**
- Produces behavior: `billing_cch_mode: sign` with effective/policy version `2.1.177` fails closed unless explicit 2.1.177 oracle/profile proof is configured and green.

- [ ] **Step 1: Write failing tests**

Add tests that configure a formal-pool account with `policy_version: 2.1.177` and `shared_pool.billing_cch_mode: sign`. Expect CC Gateway to fail closed before upstream with a stable error such as `sign_primary_2177_oracle_missing`.

- [ ] **Step 2: Implement gate**

Add a small predicate such as `isSignPrimaryAllowedForVersion(version, config)` that returns false for `2.1.177` unless an explicit safe config flag and oracle/profile proof ref are present. The default must remain false. Do not infer 2.1.177 compatibility from 2.1.175 materials.

- [ ] **Step 3: Run tests**

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway
npm test -- tests/policy-cch.test.ts tests/checkpoint3-remediation.test.ts
```

Expected: PASS.

### Task 9B: Add Redaction Tests for New Formal-Pool Failure Paths

**Files:**
- Modify/Add CC Gateway redaction tests under `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/`.
- Modify/Add Sub2API redaction tests under `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend/internal/service/` or `tools/tests/` as applicable.

**Requirements:**

- [ ] Add tests for all new formal-pool failure paths. Error responses, logs, test artifacts, and evidence reports MUST NOT contain API keys, OAuth tokens, cookies, Authorization headers, raw prompts, raw request bodies, raw responses, raw telemetry, raw CCH, account UUIDs or emails, proxy credentials, or raw credential digest/HMAC inputs.
- [ ] Tests should assert only safe refs, buckets, booleans, status codes, schema names, and redacted summaries are emitted.

### Task 10: Verification and Evidence Refresh

**Files:**
- Modify: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/docs/anti-ban/55-claude-code-native-safety-and-multiprovider-final-evidence-report.md`
- Modify: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/.superpowers/sdd/final-evidence-map.md`

**Interfaces:**
- Produces updated status: CC Gateway P0 safety boundary PASS or exact blocker.

- [ ] **Step 1: Run CC Gateway targeted tests**

Run from normal Terminal if sandbox blocks local listen/tsx IPC:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway
npm test -- \
  tests/config.test.ts \
  tests/policy-cch.test.ts \
  tests/persona-resolver.test.ts \
  tests/persona-registry.test.ts \
  tests/session-and-beta-policy.test.ts \
  tests/checkpoint3-remediation.test.ts \
  tests/proxy-sub2api.test.ts \
  tests/security-boundary.test.ts \
  tests/preflight-safety.test.ts
```

Expected: PASS. If sandbox reports `listen EPERM`, record it and ask the user to run the exact command in normal Terminal.

- [ ] **Step 2: Run CC Gateway type/build check**

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway
npm run build
```

Expected: PASS. If current sandbox cannot write `dist`, run in a normal Terminal or a writable CC Gateway worktree.

- [ ] **Step 3: Rerun localhost-only full-chain E2E from Sub2API worktree**

Run in normal Terminal if sandbox blocks listeners:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime
unset ANTHROPIC_BASE_URL CLAUDE_CODE_API_BASE_URL ANTHROPIC_API_KEY HTTPS_PROXY HTTP_PROXY ALL_PROXY
export TMPDIR=/private/tmp
export GOCACHE=/private/tmp/sub2api-go-cache
tools/zhumeng-agent/.venv/bin/python tools/cli_control_plane_full_chain_controller.py --tmp-root /private/tmp
```

Expected safe deliverable:

- overall `PASS`
- scenario A `PASS`, mock upstream count `0`, cost envelope block `true`
- scenario B `PASS`, Sub2API selected count `1`, mock upstream count `1`, client `200`
- sensitive scan `PASS`
- `real_anthropic_upstream=false`

- [ ] **Step 4: Update evidence report honestly**

Update the evidence report status as follows:

- If CC Gateway P0 tests pass and localhost full-chain passes: `Gateway-only native CLI / Server API + CC Gateway P0 safety boundary: PASS_WITH_DEGRADED_SCOPE`.
- Keep these as external/canary blockers until separately proven:
  - deployed CC Gateway image/commit equivalence;
  - live 3017 rebuild;
  - 2.1.177 strict native parity;
  - sign-primary readiness;
  - WebSearch/WebFetch bridge;
  - 95-99% cache hit rate.

---

## 4. Execution Order and Checkpoints

### Checkpoint 1: Planning and review only

- This document created.
- GPT-5.5 xhigh review agent must review it before implementation.
- Do not edit CC Gateway production code until review is PASS or required edits are folded in.

### Checkpoint 2: CC Gateway P0 config and egress gates

- Complete Task 1 and Task 2.
- Run config/policy/checkpoint tests.
- Quality review before Task 3.

### Checkpoint 3: Trusted context attestation, sticky ledger, and credential binding

- Complete Task 3A, Task 3, Task 4, and Task 5.
- Run proxy-sub2api/checkpoint/security-boundary tests.
- Quality review before Task 6.

### Checkpoint 4: Internal control hardening, per-account device identity, and 2.1.177 sign gate

- Complete Task 6, Task 7, Task 9, and Task 9B.
- Run session-and-beta/proxy-sub2api/policy-cch tests.
- Quality review before docs/evidence.

### Checkpoint 5: Docs, capture reconciliation, and evidence refresh

- Complete Task 8 and Task 10.
- Run full targeted CC Gateway command and localhost full-chain E2E.
- Only after this checkpoint may the team consider rebuilding/restarting 3017 for live L8.

---

## 5. Non-Goals for This P0 Patch

- Do not implement WebSearch/WebFetch bridge in this patch.
- Do not claim full first-party Claude Code control-plane parity.
- Do not enable sign-primary by default.
- Do not claim strict Claude Code 2.1.177 native mimicry.
- Do not redesign Sub2API scheduler UI or formal-pool operator UX beyond required trusted context and evidence documentation.
- Do not change port/container `3012`.
- Do not deploy/restart `3017` until P0 local gates are green.

---

## 6. Success Criteria

The P0 objective is met only when all are true:

1. CC Gateway `sub2api` mode rejects formal-pool startup/configurations that lack required account/egress maps.
2. `standalone` cannot be accidentally used as formal-pool/shared-account production boundary.
3. Every egress bucket used by formal-pool requires an explicit non-empty account allowlist and explicit safe proxy identity ref.
4. Sub2API is the only formal-pool context producer; user-supplied authority headers are stripped/ignored/rejected before CC Gateway.
5. Every authority-bearing scheduler field is covered by internal formal-pool context attestation and mismatches fail closed.
6. Same session cannot silently switch account, credential, policy version, device, or egress bucket inside CC Gateway; production ledger is persistent/shared or admission fails closed.
7. Selected credential is bound to selected account by a safe attested ref plus keyed binding/HMAC and mismatch fails closed.
8. Runtime registration and persona/context override headers require internal control attestation, not merely ordinary client-controlled headers.
9. Runtime-registered accounts carry account-owned `device_id` or fail closed.
10. 2.1.177 sign-primary fails closed unless a 2.1.177 oracle/profile proof is explicitly green.
11. Final-output verifier still runs before upstream forwarding and strips or verifies billing/CCH according to mode.
12. Control-plane routes remain separated from messages signing and cannot forward raw telemetry/prompt/body/CCH.
13. Targeted CC Gateway tests and localhost-only full-chain E2E pass, or any remaining blocker is explicitly labeled with exact evidence and next action.
