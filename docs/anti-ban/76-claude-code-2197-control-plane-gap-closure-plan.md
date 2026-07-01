# 76 - Claude Code 2.1.197 Control-plane Gap Closure and Promotion Resume Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` or `superpowers:subagent-driven-development` to implement this plan checkpoint-by-checkpoint. This is a narrow blocker-remediation plan that may resume Plan75 CP5-CP9 only after the explicit control-plane gaps are closed. It is not production deployment approval and not live canary approval.

**Goal:** Close the Plan75 `BLOCKED_CONTROL_PLANE_GAP` blockers so the formal-pool canonical profile can safely promote to Claude Code `2.1.197` for Sonnet 5, while preserving `2.1.185` fallback evidence and `2.1.179` rollback.

**Architecture:** Reuse Plan75 evidence and avoid redoing solved surfaces. Focus on missing control-plane material: `count_tokens`, MCP configured shape, non-streaming request shape, model/control-plane paths, and `2.1.185` Sonnet 5 fail-closed behavior. Once and only once those gaps are closed or explicitly fail-closed by tested gateway policy, resume Plan75 CP5-CP9 canonical tuple implementation and three-version mock E2E.

**Tech Stack:** Sub2API Go service/tests, CC Gateway TypeScript proxy/config/tests, CC Gateway Go/uTLS sidecar already containing the `2.1.197` compiled TLS oracle profile from Plan75, public npm package metadata/tarballs, Plan75 safe evidence, local loopback harnesses only, safe JSON evidence, local mock upstream only.

## Current status and input anchors

Plan75 ended with final decision `BLOCKED_CONTROL_PLANE_GAP`.

Required input reports/commits:

- Plan75 plan: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/docs/anti-ban/75-claude-code-2185-2197-canonical-promotion-proof-plan.md`.
- Plan75 evidence report: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/docs/anti-ban/75-claude-code-2185-2197-canonical-promotion-evidence-report.md`.
- Plan75 Sub2API report commit: `2f5f5660f` or descendant.
- Plan75 CC Gateway TLS oracle commit: `dbc2649` or descendant.
- Plan72 env residue evidence report: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/docs/anti-ban/72-canonical-local-env-residue-defense-evidence-report.md`.
- Plan74 deployed local-only equivalence report: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/docs/anti-ban/74-plan65-deployed-local-only-equivalence-evidence-report.md`.

Current npm/doc anchors at plan-writing time on 2026-07-01 America/Los_Angeles:

- `stable=2.1.185`
- `latest=2.1.197`
- `next=2.1.197`
- `version=2.1.197`
- `time.modified=2026-06-30T17:55:42.305Z`
- Official docs safe fact from Plan75: Sonnet 5 requires Claude Code `2.1.197` or later.

Because npm/doc state can move, CP0 must re-check and lock targets before implementation.

## Exact Plan75 blockers to close

Plan76 must close these gaps or explicitly implement/prove fail-closed gateway policy for them:

1. `count_tokens_path_not_locally_observed` for `2.1.179`, `2.1.185`, and `2.1.197`.
2. `mcp_configured_upstream_body_marker_not_observed_synthetic_mcp_does_not_enter_request_body` for `2.1.179`, `2.1.185`, and `2.1.197`.
3. `non_streaming_request_shape_not_locally_observed_cli_emits_stream_true_for_print_scenarios` for `2.1.179`, `2.1.185`, and `2.1.197`.
4. `model/control-plane path not observed` for rollback/fallback/primary candidates.
5. `2.1.185:sonnet5_absent_or_blocked_not_proven_cli_can_emit_claude_sonnet_5_request_must_fail_closed_in_gateway_policy`.
6. `2.1.197` Sonnet 5 model/control-plane behavior must be observed or safely proven enough for canonical server-side policy.
7. CP1.5 new residue marker buckets from Plan75 must be either observed-only, canonicalized, stripped, or fail-closed by tests before promotion resumes.

## Promotion target policy

- Primary target remains `2.1.197` canonical promotion.
- `2.1.185` remains stable fallback evidence only unless `2.1.197` is blocked by a higher-priority gate and `2.1.185` passes every non-Sonnet gate.
- `2.1.179` remains current/rollback canonical until this plan reaches `PROMOTE_CANONICAL_2197_MOCK_E2E_READY` or a later production plan deploys a promoted profile.
- Observed admission floor remains `2.1.179`; inbound user version remains observed/admission-only and never selects upstream canonical identity.

## Global constraints

- Do not touch, stop, restart, reconfigure, or bind over `3012`, `3017`, `18080`, or `18081`.
- Do not deploy or restart production services.
- Do not run live canary.
- Do not call real Anthropic, AWS, Vertex, Bedrock, OpenAI, DeepSeek, credentialed, paid, or non-local upstreams.
- Do not use real OAuth/API keys, session cookies, account identifiers, billing credentials, proxy credentials, or production DB/account data.
- Do not use client version, client family, client platform/OS/editor/terminal, client timezone, client base URL, client proxy, client domain/keyword residue, settings, MCP config, or user-supplied refs as authority for upstream identity.
- Default attribution posture remains `strip_attribution`; do not enable `no_cch`, `signed_cch`, native CCH, or strict native parity in this plan.
- Preserve Plan72 env-residue canonicalization/fail-closed behavior.
- Preserve Plan74/75 sidecar-only TLS behavior. Node direct HTTPS fallback must remain `0`.
- Do not store raw request bodies, raw prompts, raw responses, raw decoded domain/keyword lists, raw ClientHello/TLS records, pcap, HAR, secrets, cookies, account/workspace IDs, proxy credentials, private keys/certs, mock CA material, native binary dumps, or long minified source dumps in repo/docs/evidence/logs/fixtures.
- Evidence may contain only safe summaries: hashes, counts, booleans, enum buckets, redacted command results, status labels, route buckets, and synthetic fixture labels.
- Do not delete scratch, extracted packages, runtime copies, or temp directories without explicit user approval. Prefer leaving timestamped `/private/tmp` scratch uncommitted. If cleanup is required for leak-risk remediation, stop and ask the user first.
- If any checkpoint cannot prove loopback-only behavior, stop with `BLOCKED_LOCAL_ONLY_EGRESS_GUARD`.
- If any blocker cannot be closed or fail-closed, do not proceed to promotion implementation.

## Required final decision labels

Choose exactly one:

- `PROMOTE_CANONICAL_2197_MOCK_E2E_READY`
- `PROMOTE_STABLE_2185_ONLY_SONNET5_BLOCKED`
- `CONTROL_PLANE_GAPS_CLOSED_READY_FOR_PLAN75_CP5_CP9`
- `COMPAT_ONLY_NO_PROMOTION`
- `BLOCKED_VERSION_ORACLE_GAP`
- `BLOCKED_LOCAL_ONLY_EGRESS_GUARD`
- `BLOCKED_COUNT_TOKENS_GAP`
- `BLOCKED_MCP_SHAPE_GAP`
- `BLOCKED_NON_STREAMING_SHAPE_GAP`
- `BLOCKED_MODEL_CONTROL_PLANE_GAP`
- `BLOCKED_SONNET5_POLICY_GAP`
- `BLOCKED_ENV_RESIDUE_REGRESSION`
- `BLOCKED_TLS_ORACLE_REGRESSION`
- `BLOCKED_CCH_BILLING_GAP`

Decision precedence:

1. If npm/version/doc oracle cannot be locked, choose `BLOCKED_VERSION_ORACLE_GAP`.
2. Else if loopback/local-only egress guard cannot be proven, choose `BLOCKED_LOCAL_ONLY_EGRESS_GUARD`.
3. Else if Plan75/sidecar TLS regresses, choose `BLOCKED_TLS_ORACLE_REGRESSION`.
4. Else if Plan72 env residue or CP1.5 new-residue policy regresses, choose `BLOCKED_ENV_RESIDUE_REGRESSION`.
5. Else if `count_tokens` cannot be observed or fail-closed by gateway policy, choose `BLOCKED_COUNT_TOKENS_GAP`.
6. Else if MCP configured shape cannot be observed or fail-closed by gateway policy, choose `BLOCKED_MCP_SHAPE_GAP`.
7. Else if non-streaming shape cannot be observed or fail-closed by gateway policy, choose `BLOCKED_NON_STREAMING_SHAPE_GAP`.
8. Else if model/control-plane paths cannot be observed or fail-closed by gateway policy, choose `BLOCKED_MODEL_CONTROL_PLANE_GAP`.
9. Else if Sonnet 5 behavior for `2.1.197` or fail-closed behavior for `2.1.185` cannot be proven, choose `BLOCKED_SONNET5_POLICY_GAP`.
10. Else if CCH/billing/attribution proof regresses, choose `BLOCKED_CCH_BILLING_GAP`.
11. Else if gaps are closed but promotion implementation was not resumed, choose `CONTROL_PLANE_GAPS_CLOSED_READY_FOR_PLAN75_CP5_CP9`.
12. Else if `2.1.197` full resumed CP5-CP9 proof passes, choose `PROMOTE_CANONICAL_2197_MOCK_E2E_READY`.
13. Else if only `2.1.185` non-Sonnet fallback passes and `2.1.197` remains blocked only by Sonnet/model gate, choose `PROMOTE_STABLE_2185_ONLY_SONNET5_BLOCKED`.
14. Else choose `COMPAT_ONLY_NO_PROMOTION` only when no promotion is allowed but safe compatibility/fail-closed work is complete and no higher-priority blocker applies.

## Key design principle for closing gaps

A gap may be closed in one of two ways:

1. **Observed-shape closure:** safely observe the real local loopback request/response shape for the candidate version and add it to the server-selected canonical profile; or
2. **Policy closure:** prove with tests that the gateway never forwards the unobserved shape for formal-pool accounts, returning a deterministic fail-closed error instead.

Do not infer unobserved upstream shape from static strings alone. Static evidence can guide tests, but promotion requires observed-shape closure or policy closure.

## File map

### Sub2API worktree

Root: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool`

Likely files:

- Modify: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/backend/internal/service/cc_gateway_adapter.go`
  - If gaps close and Plan75 CP5-CP6 resumes, add server-selected canonical tuple selection for `2.1.197` and rollback/fallback tuple vectors.
  - If policy closure is used, add observed-only/fail-closed signaling for unsupported formal-pool `count_tokens`, MCP configured shape, non-streaming, or model-control-plane routes if Sub2API owns admission.
- Modify/add tests under `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/backend/internal/service/`.
- Modify: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/backend/internal/service/testdata/cc_gateway_formal_pool_contract/vectors.json` if canonical tuple implementation resumes.
- Create report: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/docs/anti-ban/76-claude-code-2197-control-plane-gap-closure-evidence-report.md`.
- Optional safe tooling under `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/tools/` if Plan75 harnesses cannot trigger the missing local shapes.

### CC Gateway worktree

Root: `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5`

Likely files:

- Modify/add tests:
  - `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/tests/formal-pool-control-plane-gap-closure.test.ts`
  - `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/tests/formal-pool-canonical-promotion.test.ts`
  - `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/tests/proxy-sub2api.test.ts`
  - `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/tests/formal-pool-env-residue.test.ts`
  - `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/tests/egress-tls-profile.test.ts`
  - `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/tests/egress-tls-sidecar.test.ts`
  - `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/tests/config.test.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/src/config.ts`
  - If promotion resumes, add explicit canonical `2.1.197` profile config and validation.
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/src/proxy.ts`
  - Add fail-closed guards or canonical rewrite for `count_tokens`, MCP configured shape, non-streaming, and model/control-plane formal-pool routes.
  - Preserve Plan72 final verifier and Plan74/75 sidecar-only egress.
- Modify profile helper files if present, such as env residue or TLS profile helpers.
- Sidecar files should not require modification unless Plan75 `2.1.197` TLS profile regresses. Plan76 is not a TLS remediation plan.

## Evidence root

Use:

`/private/tmp/plan76-claude-code-2197-control-plane-gap-closure-<timestamp>`

Subdirectories:

- `safe/`: safe evidence summaries only.
- `scratch/`: local loopback harness scratch; leave uncommitted and do not delete without explicit user approval.
- `public-npm-cache/`: optional public package cache/provenance if reused Plan75 cache is insufficient.

## Checkpoints

### CP0 - Anchor verification and gap inventory lock

**Goal:** Ensure Plan76 starts from the exact Plan75 blocked state and current version targets.

- [ ] Verify Sub2API HEAD includes Plan75 report commit `2f5f5660f` or descendant.
- [ ] Verify CC Gateway HEAD includes Plan75 TLS oracle commit `dbc2649` or descendant.
- [ ] Re-run `npm view @anthropic-ai/claude-code dist-tags version time.modified --json`; record safe output.
- [ ] If `latest` moved beyond `2.1.197`, lock target to `2.1.197` unless user explicitly approves retargeting.
- [ ] If `stable` moved beyond `2.1.185`, lock fallback to `2.1.185` unless user explicitly approves retargeting.
- [ ] Read Plan75 report and record the seven exact blockers listed above into `$EVIDENCE_ROOT/safe/cp0-gap-inventory.json`.
- [ ] Record that TLS is not a current blocker because Plan75 CP3 passed, but CP7/CP10 must regression-test it.
- [ ] Record that family admission is not a current blocker but remains observed-only.

Blockers:

- If Plan75 report/commits are missing, stop with `BLOCKED_VERSION_ORACLE_GAP`.
- If npm metadata is unreachable, stop with `BLOCKED_VERSION_ORACLE_GAP`.

### CP1 - Harness strategy for missing shapes

**Goal:** Determine how to safely trigger or intentionally fail-close each missing shape without real upstream.

For each candidate version `2.1.179`, `2.1.185`, `2.1.197`, build a strategy matrix for:

- `count_tokens`:
  - Try documented/local CLI invocation, environment/config trigger, direct local API path fixture, or gateway policy closure.
  - If Claude Code CLI cannot emit count_tokens locally, define explicit formal-pool policy: reject or route count_tokens only after canonical shape proof.
- MCP configured shape:
  - Use synthetic local MCP server/config only if it does not expose secrets and the loopback guard proves no external network.
  - If MCP configured state does not alter upstream body, record observed absence; if it is unobservable, add gateway final-verifier fail-closed for unknown MCP authority fields.
- non-streaming shape:
  - Try CLI flags/env/config/API fixtures that set `stream=false` or non-streaming equivalent.
  - If CLI always emits `stream=true` for relevant scenarios, define canonical policy: formal-pool upstream messages must be streaming-only unless exact non-streaming proof exists.
- model/control-plane:
  - Trigger local model selection, model aliases, settings/env model overrides, and dummy control-plane endpoints under loopback.
  - If control-plane endpoint is not locally observable, define gateway policy: only allow preapproved canonical model ids/aliases and fail closed for unapproved control-plane paths.
- Sonnet 5 policy:
  - For `2.1.197`, observe or prove canonical model id/beta/control-plane behavior for `claude-sonnet-5`.
  - For `2.1.185`, prove Sonnet 5 fail-closed behavior in Sub2API/CC Gateway, regardless of whether CLI can emit the string.

Write `$EVIDENCE_ROOT/safe/cp1-harness-strategy-matrix.json`.

Required pass criteria:

- Every Plan75 blocker has a concrete observed-shape or policy-closure path before implementation begins.
- The strategy does not require real upstream credentials or non-loopback network.
- If any blocker has no safe strategy, stop with the corresponding `BLOCKED_*` label.

### CP2 - Failing tests for policy closure before deeper harness work

**Goal:** Ensure unobserved high-risk shapes cannot leak upstream while capture is incomplete.

Write failing tests first in CC Gateway and/or Sub2API covering:

- [ ] Formal-pool `count_tokens` request with no approved canonical count_tokens profile fails closed with stable error `formal_pool_count_tokens_profile_unapproved` or equivalent.
- [ ] Formal-pool MCP configured authority/body marker not in approved safe schema fails closed with stable error `formal_pool_mcp_shape_unapproved` or equivalent.
- [ ] Formal-pool non-streaming messages request fails closed unless an approved canonical non-streaming profile exists.
- [ ] Formal-pool model/control-plane path not in allowlisted path/model policy fails closed with stable error `formal_pool_control_plane_unapproved` or equivalent.
- [ ] `2.1.185` canonical/fallback candidate with Sonnet 5 request fails closed with stable error `formal_pool_model_version_unsupported` or equivalent.
- [ ] `2.1.197` candidate permits Sonnet 5 only when canonical model policy ref is server-selected and attested; user observed version/model hint cannot self-authorize.
- [ ] New residue marker buckets from Plan75 CP1.5 are either canonicalized by Plan72-compatible code or fail closed; user messages are still not scanned/modified.
- [ ] Errors are deterministic and do not fall back to Node direct HTTPS or alternate upstream paths.

Expected before implementation: FAIL on missing guards or missing stable errors.

### CP3 - Loopback dynamic capture retry for missing shapes

**Goal:** Retry dynamic oracle capture with targeted triggers and record safe results.

- [ ] Reuse Plan75 package cache or redownload public tarballs with provenance if needed.
- [ ] Run `2.1.179`, `2.1.185`, and `2.1.197` under same-scope loopback-only egress guard.
- [ ] For `count_tokens`, attempt every CP1 safe trigger. Record observed shape bucket or `not_emitted_after_safe_triggers`.
- [ ] For MCP, run synthetic local MCP configured and MCP absent cases. Record whether upstream body/header/path changes occur as safe buckets.
- [ ] For non-streaming, run safe `stream=false` or equivalent triggers if available. Record shape or `cli_always_stream_true_under_safe_triggers`.
- [ ] For model/control-plane, run safe model alias/config triggers and dummy loopback endpoints. Record path/model/header/body buckets.
- [ ] For Sonnet 5, record `2.1.197` `claude-sonnet-5` model bucket behavior and `2.1.185` behavior under the same synthetic request scenario.
- [ ] Record real upstream request count `0` and non-loopback attempt count `0`.
- [ ] Write `$EVIDENCE_ROOT/safe/cp3-targeted-dynamic-capture-summary.json`.

Required pass criteria:

- For each blocker, either safe observed shape is captured or CP2 policy closure remains the selected resolution.
- No evidence contains raw prompt/body/response.
- No dynamic process reaches non-loopback network.

### CP4 - Implement minimal policy closures and canonical model policy

**Goal:** Make unresolved shapes safe by default and enable only proven canonical `2.1.197` Sonnet 5 behavior.

- [ ] Implement fail-closed guards for unresolved count_tokens, MCP configured shape, non-streaming shape, and model/control-plane paths.
- [ ] If CP3 captured a safe shape, implement canonical rewrite/verifier for that shape instead of blocking it.
- [ ] Implement `2.1.197` canonical model policy allowing Sonnet 5 only under server-selected canonical tuple.
- [ ] Implement `2.1.185` Sonnet 5 fail-closed policy if fallback remains available.
- [ ] Ensure all guards run before any upstream/sidecar send.
- [ ] Ensure no policy reads observed client version/family/settings/MCP/user model hint as authority.
- [ ] Run CP2 tests until PASS.
- [ ] Write `$EVIDENCE_ROOT/safe/cp4-policy-closure-tests.txt`.

Required pass criteria:

- Every Plan75 blocker is closed by observed-shape canonical support or deterministic fail-closed policy.
- No fallback to Node direct HTTPS.
- Plan72 env residue and Plan75 TLS tests still pass.

### CP5 - Control-plane gap closure review gate

**Goal:** Obtain independent review before resuming promotion implementation.

Dispatch exactly one review agent with this scope:

```text
Review Plan76 CP0-CP4 evidence and code. Decide whether Plan75 BLOCKED_CONTROL_PLANE_GAP is closed enough to resume Plan75 CP5-CP9 canonical promotion implementation. Focus on count_tokens, MCP configured shape, non-streaming shape, model/control-plane, Sonnet 5 policy, fail-closed ordering before upstream send, observed-client authority injection, raw evidence leaks, loopback-only proof, Plan72 env residue regressions, and Node direct HTTPS fallback.
Return PASS, PASS_WITH_REQUIRED_EDITS, or FAIL.
```

- [ ] If review returns REQUIRED_EDITS, fix and rerun relevant tests.
- [ ] If review returns FAIL, choose the appropriate `BLOCKED_*` final decision.
- [ ] If review returns PASS, write `$EVIDENCE_ROOT/safe/cp5-review-verdict.json` and continue.

### CP6 - Resume Plan75 CP5/CP6: Sub2API canonical tuple tests and implementation

**Goal:** Implement server-selected canonical tuple support now that control-plane blockers are closed.

Write or enable failing tests first, then implement:

- [ ] With server candidate `2.1.197`, observed `2.1.179`, `2.1.185`, `2.1.197`, and safe future `2.1.198` all sign canonical `policy_version=2.1.197` and selected `2.1.197` refs.
- [ ] With fallback candidate `2.1.185`, Sonnet 5 requests fail closed according to CP4 policy.
- [ ] With rollback candidate `2.1.179`, canonical returns to rollback refs.
- [ ] User-forged version/family/platform/settings/MCP/model/env residue/profile refs cannot alter canonical tuple.
- [ ] Observed profile records only safe buckets.
- [ ] Update contract vectors for `2.1.197`, `2.1.185`, and `2.1.179` tuple cases.
- [ ] Run targeted Sub2API tests and record `$EVIDENCE_ROOT/safe/cp6-sub2api-canonical-tuple-tests.txt`.

Required pass criteria:

- Mixed tuple fields are rejected or impossible to sign.
- Account/server config is the only canonical tuple source.

### CP7 - Resume Plan75 CP7/CP8: CC Gateway canonical tuple tests and implementation

**Goal:** Enforce `2.1.197` canonical promotion tuple and preserve fallback/rollback.

Write or enable failing tests first, then implement:

- [ ] Missing/unknown/mixed canonical tuple fails closed.
- [ ] Observed client `2.1.179` with server canonical `2.1.197` emits upstream `2.1.197` canonical user-agent/beta/model policy, not observed version.
- [ ] Observed client `2.1.197` with server canonical `2.1.185` emits fallback shape or Sonnet 5 fail-closed policy.
- [ ] Rollback `2.1.179` tuple is accepted and rewrites upstream back to `2.1.179` shape.
- [ ] `2.1.197` TLS profile from Plan75 is selectable only by server-selected canonical tuple and matches sidecar safe oracle.
- [ ] Count_tokens/MCP/non-streaming/control-plane policies from CP4 remain enforced under promoted tuple.
- [ ] Plan72 env residue final verifier still runs.
- [ ] Session authority ledger rejects tuple drift.
- [ ] Node direct HTTPS fallback remains `0`.
- [ ] Run targeted CC Gateway tests and record `$EVIDENCE_ROOT/safe/cp7-cc-gateway-canonical-tuple-tests.txt`.

Required pass criteria:

- CC Gateway cannot follow observed client version.
- CC Gateway cannot send mixed tuple upstream.
- CC Gateway cannot bypass sidecar/fail-closed guards.

### CP8 - Three-version local mock E2E and tuple switching

**Goal:** Prove the complete promoted path locally before any production gate.

Use independent loopback ports only, excluding `3012`, `3017`, `18080`, `18081`.

Run E2E sets:

- [ ] Primary `2.1.197` canonical E2E with observed inbound `2.1.179`, `2.1.185`, and `2.1.197`.
- [ ] Stable fallback `2.1.185` canonical E2E with Sonnet 5 fail-closed.
- [ ] Rollback `2.1.179` canonical E2E.
- [ ] Tuple switching `2.1.197 -> 2.1.185 -> 2.1.179`: new sessions may use new tuple; existing session drift fails closed.
- [ ] Sonnet 5 request under `2.1.197` canonical passes mock policy; under `2.1.185`/`2.1.179` fails closed.
- [ ] Count_tokens/MCP/non-streaming/model-control-plane cases either pass with canonical observed shape or fail closed according to CP4 policy.
- [ ] Env residue noncanonical markers are canonicalized or fail closed according to Plan72/CP4 policy.
- [ ] TLS safe summary matches `2.1.197` oracle for promoted tuple and rollback/fallback profiles for those tuples.
- [ ] Node direct HTTPS fallback count `0`; real upstream request count `0`.
- [ ] Write `$EVIDENCE_ROOT/safe/cp8-three-version-mock-e2e-summary.json`.

Required pass criteria:

- One stable server-selected canonical identity per tuple.
- No observed user version/family/settings/MCP/env residue changes upstream identity.
- No real upstream access.

### CP9 - Regression tests and leak scan

Run and record at minimum:

Sub2API:

```bash
go test ./internal/service -run 'CCGateway|FormalPool|ObservedProfile|TLSProfile|EnvResidue|LocalEnv|Canonical|Promotion|ControlPlane|CCH|Billing|Model|CountTokens|MCP|Streaming' -count=1
```

CC Gateway:

```bash
npx tsx tests/formal-pool-control-plane-gap-closure.test.ts
npx tsx tests/formal-pool-canonical-promotion.test.ts
npx tsx tests/formal-pool-env-residue.test.ts
npx tsx tests/proxy-sub2api.test.ts
npx tsx tests/egress-tls-profile.test.ts
npx tsx tests/egress-tls-sidecar.test.ts
npx tsx tests/config.test.ts
npx tsc --noEmit
```

Sidecar:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/sidecar/egress-tls-sidecar
go test ./...
```

Leak scan:

- Scan modified files, report, safe evidence, tests, profile code.
- Block on raw prompts/bodies/responses, raw domain lists, raw TLS/pcap/HAR, secrets, cert/key material, native dumps, account identifiers, proxy credentials, and raw minified source dumps.
- Record `$EVIDENCE_ROOT/safe/cp9-leak-scan-summary.json`.
- Record scratch cleanup status as `not_needed`, `skipped_requires_user_approval`, or `approved_by_user`.

### CP10 - Final review and report

Dispatch exactly one final review agent:

```text
Review Plan76 final evidence and code. Decide whether the Plan75 control-plane blockers are closed and whether the final decision label is justified. Focus on count_tokens, MCP configured shape, non-streaming shape, model/control-plane, Sonnet 5 policy, fail-closed ordering, canonical tuple integrity, observed-client authority injection, Plan72 env residue, Plan75 TLS profile, Node direct fallback, three-version mock E2E, rollback/fallback, leak scan, and no production/upstream touch.
Return PASS, PASS_WITH_REQUIRED_EDITS, or FAIL.
```

- [ ] Address REQUIRED_EDITS if any.
- [ ] Write final report: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/docs/anti-ban/76-claude-code-2197-control-plane-gap-closure-evidence-report.md`.
- [ ] Include:
  - final decision label and decision precedence path;
  - CP0-CP10 status table;
  - exact Plan75 blockers and closure method for each;
  - dynamic capture retry safe summary;
  - policy closure test summary;
  - Sub2API canonical tuple result;
  - CC Gateway canonical tuple result;
  - three-version mock E2E result;
  - TLS regression result;
  - Plan72 env residue regression result;
  - test results;
  - leak scan;
  - review verdicts;
  - commits;
  - scratch cleanup status;
  - explicit non-goals: no production deployment, no live canary, no real upstream, no forbidden ports touched.
- [ ] Run `git diff --check` in both worktrees.
- [ ] Commit Sub2API and CC Gateway changes separately if final evidence is accepted.

## Required final output to user

Report:

- Final decision.
- Whether canonical promotion to `2.1.197` is mock-E2E-ready.
- If not ready, exact remaining blocker.
- Whether `2.1.185` fallback is mock-E2E-ready or blocked.
- Whether TLS remains closed.
- Whether Plan72 env residue remains closed.
- Whether production/live canary remains forbidden pending separate approval.
- Commit hashes.

## Self-review checklist

- This plan does not redo Plan75 solved surfaces except regression testing.
- This plan targets the exact CP2/CP4 gaps from Plan75.
- Every unobserved shape must be observed or fail-closed before promotion resumes.
- `2.1.197` promotion remains the primary objective.
- `2.1.185` fallback and `2.1.179` rollback are tested in mock E2E if promotion resumes.
- No real upstream, production deployment, forbidden ports, or production credentials are used.
- Scratch deletion requires explicit user approval.
