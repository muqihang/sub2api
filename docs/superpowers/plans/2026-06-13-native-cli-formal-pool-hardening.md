# Native CLI Formal Pool Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Harden the existing Sub2API + CC Gateway path for real/native Claude Code CLI requests so subscription formal-pool accounts see the most consistent verified-native outbound shape we currently have evidence for, without enabling non-CLI-to-native synthesis.

**Architecture:** Keep CC Gateway as the final outbound policy/signing/verifier authority. Sub2API owns ingress classification, native attestation, `claude_code_only`/fallback behavior, and safe audit propagation. This plan strengthens verified CLI traffic first: explicit profile tables, stronger spoofing tests, native attestation invariants, and localhost joint smoke.

**Tech Stack:** Go backend tests and services in Sub2API; TypeScript/Node tests and policy modules in CC Gateway; localhost/mock upstream only until explicit production canary approval.

---

## Scope boundaries

## Preflight and review amendments

Before any checkpoint starts:

- Run `git status --short` in both worktrees and record which dirty files belong to this task.
- Do not stage unrelated dirty files. Stage exact file paths only; avoid broad globs such as `backend/internal/service/*test.go`.
- This plan may be executed after earlier checkpoint work already exists. If a "write failing test" step already passes because a previous worker implemented it, record that fact and continue with the next missing assertion rather than reverting work.
- Build Sub2API with `go build -o /tmp/sub2api-server-check ./cmd/server` to avoid creating a local binary in the worktree.
- Confirm whether CC Gateway `dist/` is ignored before `npm run build`; if not, use `npx tsc --noEmit` and do not commit build output.

Native/compat boundary for this phase:

- `claude_code_native` means guard-signed native attestation only.
- Existing Claude Code UA/body compatibility may remain a Claude-Code-compatible signal for existing `claude_code_only` behavior, but it must not be logged or forwarded as `claude_code_native`.
- Compat/non-native + spoofed `User-Agent: claude-cli/...` + no guard attestation must never set `claude_code_native` audit headers.
- Guard-signed native attestation is the only strong native signal for future formal-pool hard-gates.
- Do not change existing `fallback_group_id` / `fallback_group_id_on_invalid_request` semantics unless a dedicated failing test proves a bug.

Trusted persona boundary:

- CC Gateway may trust `x-sub2api-persona-trusted` only from loopback/internal Sub2API traffic.
- External requests must not self-promote by setting that header.
- Sub2API may set that header only on controlled CC Gateway calls where the account/session/egress context has already been selected and audited.

Sensitive artifact policy:

- Sensitive scans must cover docs, fixtures, testdata, and acceptance artifacts in both repositories.
- In addition to tokens, scan for raw billing blocks (`x-anthropic-billing-header`), `metadata.user_id`, `device_id`, `account_uuid`, raw UUIDs, 64-hex values, `cch=`, proxy URLs, `Bearer`, `refresh_token`, `access_token`, and password-like fields.

### In scope

- Real/native Claude Code CLI request path.
- CC Gateway 2.1.175 profile/header/signing/verifier hardening.
- Sub2API native attestation and `claude_code_only` behavior preservation.
- Localhost/mock smoke proving no real Anthropic egress.
- Safe docs/fixtures only; no raw prompts/bodies/tokens/CCH values in committed artifacts.

### Out of scope

- Non-CLI requests being synthesized as native CLI.
- OpenAI-compatible protocol conversion into native Claude Code.
- Fake tool runners, fake telemetry, or fake control-plane uploads.
- Production deployment or real Anthropic canary.

## Evidence inputs

- `/tmp/synthesis-gap-planA/report.md`
- `/tmp/synthesis-gap-planA/tools-default.json`
- `/tmp/synthesis-gap-planA/tools-bare.json`
- `/tmp/synthesis-gap-planB/report.md`
- `/tmp/synthesis-gap-planB/production_audit.md`
- `/tmp/synthesis-gap-planB/extracted_recipe.md`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/docs/anti-ban/51-formal-pool-claude-code-persona-safety-gap-analysis.md`

## Files likely to change

Sub2API:

- Modify: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend/internal/pkg/claude/constants.go`
- Modify: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend/internal/handler/gateway_helper.go`
- Modify: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend/internal/handler/gateway_handler.go`
- Modify or create tests under: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend/internal/handler/*native*test.go`
- Modify or create tests under: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend/internal/service/*claude_code*test.go`
- Modify or create tests under: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend/internal/service/*cc_gateway*test.go`
- Optional safe docs: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/docs/anti-ban/52-native-cli-hardening-acceptance.md`

CC Gateway:

- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main/src/persona-registry.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main/src/persona-resolver.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main/src/policy.ts`
- Modify tests: `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main/tests/persona-registry.test.ts`
- Modify tests: `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main/tests/persona-resolver.test.ts`
- Modify tests: `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main/tests/checkpoint3-remediation.test.ts`
- Modify tests: `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main/tests/policy-cch.test.ts`

---

## Checkpoint 1: CC Gateway native persona profile exactness

### Task 1.1: Add explicit profile IDs for captured 2.1.175 variants

**Purpose:** Stop treating the current 2.1.175 subscription/1M profile as a universal profile. Make profile choice explicit and testable.

**Files:**

- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main/src/persona-registry.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main/tests/persona-registry.test.ts`

- [ ] **Step 1: Write failing registry tests**

Add tests asserting these profiles exist as distinct records:

- `claude_code_2_1_175_subscription_1m`
- `claude_code_2_1_175_api_key_non_1m`
- `claude_code_2_1_175_simple_bare`

Expected beta strings:

```text
subscription_1m:
claude-code-20250219,context-1m-2025-08-07,interleaved-thinking-2025-05-14,context-management-2025-06-27,prompt-caching-scope-2026-01-05,mid-conversation-system-2026-04-07,effort-2025-11-24

api_key_non_1m:
claude-code-20250219,interleaved-thinking-2025-05-14,context-management-2025-06-27,prompt-caching-scope-2026-01-05,mid-conversation-system-2026-04-07,effort-2025-11-24
```

For `simple_bare`, use the same beta string only if Plan A/B evidence confirms the same header for the selected simple/bare request class. If not confirmed, mark this profile as `capabilities.tools=false` or keep it absent and document the gap. Do **not** invent an unverified beta string.

Also model simple/bare as a low-tool profile, not just a boolean tools capability. At minimum assert `toolProfile.kind=low_tool`, `toolCount=3`, and tool names `Bash`, `Edit`, `Read`.

- [ ] **Step 2: Run failing test**

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main
npx tsx tests/persona-registry.test.ts
```

Expected: FAIL because the new profile IDs are missing.

- [ ] **Step 3: Implement profile entries**

In `src/persona-registry.ts`:

- add constants for the explicit beta strings;
- add profile entries with distinct aliases;
- keep `claude-code-2.1.175-macos-local` mapped to the current production subscription/1M profile unless an operator config selects otherwise;
- ensure `knownModels` includes `claude-opus-4-8` and `claude-fable-5`.

- [ ] **Step 4: Run registry tests**

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main
npx tsx tests/persona-registry.test.ts
```

Expected: PASS.

### Task 1.2: Make resolver fail closed for untrusted or mismatched 2.1.175 profile choices

**Files:**

- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main/src/persona-resolver.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main/tests/persona-resolver.test.ts`

- [ ] **Step 1: Write failing resolver tests**

Add tests for:

1. Untrusted client cannot self-promote to any `2.1.175` native profile.
2. Unknown profile string remains `quarantine_unknown_beta` unless candidate-beta allowlist/proof/budget conditions pass.
3. Account identity whose `persona_variant` is subscription/1M but config asks for API-key non-1M must produce an explicit audit tag or fail closed, depending on implementation choice.
4. `trustedClient=false` plus new model or new beta fails closed.
5. If no explicit `shared_pool.message_beta_profile` is configured, the per-account `identity.persona_variant` wins over the environment default profile; this prevents API-key/non-1M accounts from silently inheriting the subscription/1M profile.

- [ ] **Step 2: Run failing test**

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main
npx tsx tests/persona-resolver.test.ts
```

Expected: FAIL on missing explicit profile/status behavior.

- [ ] **Step 3: Implement minimal resolver changes**

Rules:

- Do not let `message_beta_profile` silently override an account identity into a different profile class unless `trustedClient=true` and the profile ID is known.
- Keep current production default stable.
- Add audit tags such as `profile:subscription_1m` or `profile:api_key_non_1m` if useful for safe logs.
- Do not accept future unknown `2.1.17x+` versions without explicit rollout gate.

- [ ] **Step 4: Run resolver tests**

Expected: PASS.

### Task 1.3: Verify final output enforces exact profile/header/session invariants

**Files:**

- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main/tests/checkpoint3-remediation.test.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main/src/policy.ts` only if tests expose a gap.

- [ ] **Step 1: Add tests**

Add/extend tests proving:

- sign-primary final output rejects beta mismatch;
- sign-primary final output rejects UA version mismatch;
- sign-primary final output rejects header/body session mismatch;
- sign-primary final output rejects untrusted downstream billing/CCH material;
- `X-Stainless-Package-Version` stays `0.94.0`.

- [ ] **Step 2: Run test**

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main
npx tsx tests/checkpoint3-remediation.test.ts
```

Expected: PASS after any minimal policy fixes.

### Checkpoint 1 validation

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main
npm run build
npx tsx tests/persona-registry.test.ts
npx tsx tests/persona-resolver.test.ts
npx tsx tests/policy-cch.test.ts
npx tsx tests/checkpoint3-remediation.test.ts
```

Expected: all PASS.

Commit in CC Gateway worktree:

```bash
git add src/persona-registry.ts src/persona-resolver.ts tests/persona-registry.test.ts tests/persona-resolver.test.ts
git commit -m "feat: harden native claude code persona profiles"
```

---

## Checkpoint 2: Sub2API native ingress and `claude_code_only` hardening

### Task 2.1: Preserve native attestation as the only strong native signal

**Files:**

- Modify: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend/internal/handler/gateway_helper.go`
- Modify tests: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend/internal/handler/gateway_native_attestation_test.go`
- Modify tests: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend/internal/service/claude_code_native_attestation_test.go`

- [ ] **Step 1: Write failing tests**

Add tests proving:

1. A request with only `User-Agent: claude-cli/...` but no valid native attestation does not get `NativeAttested=true`.
2. Spoofed `x-sub2api-client-type: claude_code_native` without a valid signature is rejected.
3. Compat/server-filled traffic is never labeled `claude_code_native`.
4. Native attestation requires route/method/body-bound signature and replay nonce freshness.

- [ ] **Step 2: Run tests**

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend
go test ./internal/handler ./internal/service -run 'NativeAttestation|ClaudeCodeClientContext|Spoof|Compat' -count=1 -timeout=180s
```

Expected: any current gap fails.

- [ ] **Step 3: Implement minimal fixes**

Rules:

- Keep `ClaudeCodeNativeAuditSummary.NativeAttested` as the strong signal.
- Keep legacy UA/body validator as a compatibility signal only; do not call it native attestation.
- Ensure forced compat non-native logic cannot be bypassed by spoofed headers.
- Do not break already valid guard-signed native requests.

- [ ] **Step 4: Re-run tests**

Expected: PASS.

### Task 2.2: Prove `claude_code_only` rejection/fallback is preserved

**Files:**

- Modify or create: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend/internal/handler/gateway_claude_code_only_test.go`
- Modify or create: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend/internal/service/gateway_channel_restriction_fallback_test.go`

- [ ] **Step 1: Write tests**

Cover matrix:

| Group config | Inbound request | Expected |
| --- | --- | --- |
| `claude_code_only=true`, no fallback | non-CLI | reject safe error |
| `claude_code_only=true`, `fallback_group_id` set | non-CLI | route fallback only if existing code supports it |
| `claude_code_only=true`, `fallback_group_id_on_invalid_request` set | invalid/spoofed native | fallback/reject exactly as existing semantics specify, never native synthesis |
| `claude_code_only=true` | valid native attested | allowed |
| `claude_code_only=false` | non-CLI Anthropic messages | existing compat behavior unchanged |
| `claude_code_only=true` | compat/non-native + spoofed official Claude Code UA + no guard attestation | existing compat/fallback behavior may remain, but audit must not mark native; future formal-native hard gate must reject/fallback |
| `claude_code_only=true` | real old Claude Code UA/body + no guard attestation | preserve current compatibility unless explicitly migrating; audit must distinguish from guard-signed native |

- [ ] **Step 2: Run tests**

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend
go test ./internal/handler ./internal/service -run 'ClaudeCodeOnly|Fallback|ChannelRestriction|InvalidRequest' -count=1 -timeout=240s
```

Expected: PASS after fixes.

### Task 2.3: Align Sub2API beta constants for verified native profile references

**Files:**

- Modify: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend/internal/pkg/claude/constants.go`
- Modify or create tests under: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend/internal/pkg/claude/*test.go`

- [ ] **Step 1: Write tests**

Add tests asserting:

- `BetaMidConversationSystem = "mid-conversation-system-2026-04-07"` exists.
- Verified 2.1.175 subscription/1M beta helper returns the same sequence as CC Gateway's subscription/1M profile.
- Existing legacy helper behavior remains unchanged unless deliberately migrated.

- [ ] **Step 2: Run tests**

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend
go test ./internal/pkg/claude -run 'Beta|ClaudeCodeMessages' -count=1
```

Expected: FAIL until constant/helper is added.

- [ ] **Step 3: Implement constants/helpers**

Add only constants/helpers needed by verified native profile references. Avoid changing outbound behavior unless tests require it.

Do not change `DefaultBetaHeader` or `APIKeyBetaHeader` in this task; migrating those defaults requires a separate compatibility rollout and regression test set.

- [ ] **Step 4: Re-run tests**

Expected: PASS.

### Checkpoint 2 validation

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend
go test ./internal/pkg/claude ./internal/service ./internal/handler ./internal/server/routes -run 'ClaudeCode|NativeAttestation|Compat|Fallback|Beta|Gateway' -count=1 -timeout=300s
go test ./internal/service -run '^TestJointLocalCaptureAcceptanceArtifact$' -count=1 -timeout=180s
```

Expected: all PASS.

Commit in Sub2API worktree:

```bash
git add backend/internal/pkg/claude/constants.go backend/internal/pkg/claude/*test.go backend/internal/handler/*test.go backend/internal/service/*test.go
git commit -m "test: harden native claude code ingress gates"
```

---

## Checkpoint 3: Native body-shape acceptance without non-CLI synthesis

### Task 3.1: Add safe structural shape fixtures for native CLI path

**Purpose:** Record what native 2.1.175 shape requires without committing raw request bodies or prompts.

**Files:**

- Create: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend/internal/service/testdata/claude_code_native_shape/README.md`
- Create: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend/internal/service/testdata/claude_code_native_shape/shape_summary_2175.json`
- Create or modify tests: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend/internal/service/claude_code_native_shape_test.go`

- [ ] **Step 1: Create safe fixture schema**

The JSON fixture may include only:

- field names;
- block type/order labels;
- tool counts and tool names;
- beta token names;
- booleans for presence of `thinking`, `context_management`, `output_config`;
- no raw prompt text, no raw body, no raw CCH, no token, no account identity.

- [ ] **Step 2: Write tests**

Tests should verify current native path preserves or recognizes these structural facts for attested/native requests, but must not inject native body shape into non-CLI compat requests.

- [ ] **Step 3: Run tests**

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend
go test ./internal/service -run 'NativeShape|ShapeSummary' -count=1
```

Expected: PASS.

### Task 3.2: Ensure Sub2API does not downgrade native CLI body fields before CC Gateway

**Files:**

- Modify tests under: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend/internal/service/`
- Modify code only if tests expose a mutation/downgrade.

- [ ] **Step 1: Write tests**

For native attested requests, prove Sub2API does not remove or overwrite:

- `thinking.type=adaptive`;
- `context_management`;
- `output_config.effort`;
- non-empty `tools` array;
- `system` block array except where CC Gateway signing is explicitly responsible.

- [ ] **Step 2: Run targeted tests**

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend
go test ./internal/service ./internal/handler -run 'NativeShape|NoDowngrade|Thinking|OutputConfig|Tools' -count=1 -timeout=240s
```

Expected: PASS after minimal fixes.

### Checkpoint 3 validation

Run both Sub2API and CC Gateway targeted suites:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend
go test ./internal/pkg/claude ./internal/service ./internal/handler ./internal/server/routes -run 'ClaudeCode|Native|Compat|Gateway|Beta|Shape' -count=1 -timeout=300s

cd /Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main
npm run build
npx tsx tests/persona-registry.test.ts
npx tsx tests/persona-resolver.test.ts
npx tsx tests/policy-cch.test.ts
npx tsx tests/checkpoint3-remediation.test.ts
```

Commit docs/fixtures/tests:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt
git add -f docs/anti-ban/51-formal-pool-claude-code-persona-safety-gap-analysis.md docs/superpowers/plans/2026-06-13-native-cli-formal-pool-hardening.md
git add <exact native-shape fixture/test files changed by this checkpoint>
git commit -m "docs: capture native claude code hardening plan"
```

---

## Checkpoint 4: Local joint smoke and acceptance memo

### Task 4.1: Run localhost/mock joint capture

**Files:**

- Existing joint harness under Sub2API tests.
- Optional acceptance memo: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/docs/anti-ban/52-native-cli-hardening-acceptance.md`

- [ ] **Step 1: Build both projects**

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main
npm run build

cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend
go build -o /tmp/sub2api-server-check ./cmd/server
```

Expected: both pass.

- [ ] **Step 2: Run joint localhost smoke**

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend
CC_GATEWAY_REPO_ROOT=/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main \
  go test ./internal/service -run '^TestJointLocalCaptureAcceptanceArtifact$' -count=1 -timeout=180s
```

Expected:

- no real Anthropic egress;
- CC Gateway final UA is 2.1.175 for selected native profile;
- verifier passes;
- no native fallback;
- no raw secrets in safe deliverable;
- native and compat modes are distinguishable in safe summary.

### Task 4.2: Write acceptance memo

**Files:**

- Create: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/docs/anti-ban/52-native-cli-hardening-acceptance.md`

- [ ] **Step 1: Record command results**

Include only:

- commit SHAs;
- commands run;
- pass/fail summary;
- safe artifact paths;
- no raw request bodies/prompts/tokens/CCH values.

- [ ] **Step 2: Sensitive scan**

Run the repository's existing safe-deliverable scanner if available. If no scanner exists, run at least:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt
rg -n 'sk-|Bearer |refresh_token|access_token|proxy|cch=[a-f0-9]{5}|BEGIN PRIVATE|password' docs/anti-ban/52-native-cli-hardening-acceptance.md docs/anti-ban/51-formal-pool-claude-code-persona-safety-gap-analysis.md || true
```

Review any hit manually; do not blindly ignore.

### Checkpoint 4 validation

Run final targeted suite:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main
npm run build
npm test

cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend
go build -o /tmp/sub2api-server-check ./cmd/server
go test ./internal/pkg/claude ./internal/service ./internal/handler ./internal/server/routes -count=1 -timeout=300s
```

Commit acceptance memo:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt
git add -f docs/anti-ban/52-native-cli-hardening-acceptance.md
git commit -m "docs: record native cli hardening acceptance"
```

---

## Final review gate

Before any deployment:

- Dispatch a read-only review agent over both repos' diffs.
- Verify no production account data, token, proxy credential, raw prompt/body, raw CCH, or raw signed body is committed.
- Verify `claude_code_only` behavior is unchanged or stricter.
- Verify non-CLI synthesis remains disabled.
- Verify rollback profile remains available.
