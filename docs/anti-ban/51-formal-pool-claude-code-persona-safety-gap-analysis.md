# Formal Pool Claude Code Persona Safety Gap Analysis

Date: 2026-06-13
Status: research synthesis, no code change, no production deployment
Worktree: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt`

## 0. Boundary and conclusion

This document consolidates the completed Plan A runtime capture and Plan B static extraction/audit. It is intentionally limited to safe operational design:

- It does **not** include raw request bodies, raw prompts, tokens, cookies, observed CCH values, account emails/UUIDs, proxy credentials, or raw telemetry.
- It does **not** recommend presenting arbitrary non-Claude-Code clients to Anthropic as native Claude Code clients.
- It focuses on protecting formal-pool subscription accounts by preserving verified Claude Code traffic, failing closed on unverified traffic, isolating lower-assurance compatibility traffic, and making account/session/egress behavior auditable.

High-level conclusion:

1. CCH correctness is necessary but not sufficient. Even with a valid CCH/signature path, a safe formal pool also depends on request shape, beta/profile selection, session/device identity, egress stability, concurrency/session budgets, control-plane behavior, and truthful capability handling.
2. The current Sub2API + CC Gateway stack already covers important pieces: canonical outbound headers in CC Gateway, CCH/signing/verifier gates, CC Gateway route policy for unsupported routes, and metadata/session equality in CC Gateway. Sub2API also has concurrency/RPM/session-limit/formal-pool-refresh/egress-related mechanisms, but each must be tested as a hard fail-closed gate before it is treated as a production hard gate.
3. The largest remaining risk is not one missing header. It is mixing two different traffic classes:
   - verified Claude Code CLI traffic; and
   - non-CLI / SDK / custom-client traffic that lacks native harness behavior.
4. The safest production design is therefore a dual-mode policy:
   - **CLI-only formal subscription pool**: only verified Claude Code CLI-shaped traffic may use the subscription formal pool; non-CLI requests are rejected or routed by existing fallback settings.
   - **Transparent compatibility pool**: non-CLI clients may be served only by a separately isolated pool/mode that does not claim native CLI attestation and has lower trust, stricter budgets, and explicit operator labeling.

## 1. Evidence sources

Plan A runtime capture:

- `/tmp/synthesis-gap-planA/report.md`
- `/tmp/synthesis-gap-planA/groundtruth.jsonl`
- `/tmp/synthesis-gap-planA/tools-default.json`
- `/tmp/synthesis-gap-planA/tools-bare.json`

Plan B static extraction and production audit:

- `/tmp/synthesis-gap-planB/extracted_recipe.md`
- `/tmp/synthesis-gap-planB/production_audit.md`
- `/tmp/synthesis-gap-planB/report.md`
- `/tmp/synthesis-gap-planB/request_largest.json`
- `/tmp/synthesis-gap-planB/headers_largest.json`
- `/tmp/synthesis-gap-planB/tools.json`
- `/tmp/synthesis-gap-planB/simple_tools.json`

Local preserved temporary references, if `/tmp` is cleared:

- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/tmp`

Code audited in this worktree:

- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend/internal/pkg/claude/constants.go`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend/internal/service/gateway_service.go`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend/internal/service/metadata_userid.go`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/synthesis-gap-hunt/backend/internal/service/claude_code_compat_shape.go`

CC Gateway code audited read-only:

- `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main/src/policy.ts`
- `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main/src/persona-registry.ts`

## 2. Verified 2.1.175 request-shape facts from Plan A/B

The following facts are evidence-backed and safe to record at the shape level.

### 2.1 Headers and beta profile

Runtime/static evidence for Claude Code 2.1.175 shows:

- `User-Agent` is `claude-cli/<version> (external, sdk-cli)`.
- `X-Stainless-Package-Version` is `0.94.0`.
- `x-app` is `cli`.
- `anthropic-version` is `2023-06-01`.
- The beta header is profile-dependent. First-turn 1M Opus captures include `context-1m`; continued/non-1M captures may omit it. Current captures include `mid-conversation-system-2026-04-07` and `effort-2025-11-24` in the relevant profile.

Implication: there is no single universal beta string. The implementation needs a version/model/profile table, not scattered constants.

### 2.2 System blocks and harness prompt

Plan A/B both show that native 2.1.175 requests are not just a user prompt plus a billing block. The body contains a structured system stack:

- a billing/CCH block owned by the client/signing path;
- an Agent SDK identity block;
- either the full normal harness prompt with environment/context sections, or a simple/bare environment block.

The simple/bare mode environment block is intentionally short. Normal mode is much richer and includes the CLI harness behavior text. These are not interchangeable.

Implication: body-level parity is profile-specific and can affect model behavior. It should not be blindly injected into arbitrary API clients.

### 2.3 Tools

Plan A and Plan B both captured tool arrays, but with one important difference:

- Plan A default capture: 25 tools.
- Plan B normal/static extraction: 26 tools, with `DesignSync` present.
- Both agree that simple/bare mode has 3 tools: `Bash`, `Edit`, `Read`.

Implication: tools are conditional/profile-dependent. A hardcoded universal tool list would be brittle. Also, exposing native CLI tool schemas to non-CLI clients can produce tool calls the downstream client cannot execute, causing broken UX and suspicious retries.

### 2.4 System reminder and content ordering

Plan A/B show that current-date context appears as a `system-reminder` text block before the real user text in the first user message content array. Later turns preserve history and apply cache-control behavior differently.

Implication: request shape includes ordering and cache behavior, not just field presence.

### 2.5 Thinking, context management, and effort

Plan A/B show:

- main Opus 4.8 path uses `thinking.type = adaptive`;
- `context_management` is paired with thinking in the captured shape;
- `output_config.effort` defaults to `high` for the captured Opus 4.8 path;
- effort and thinking are model/profile dependent.

Implication: if the implementation omits these fields, the outbound shape differs from current native CLI traffic. If it injects them blindly for clients that cannot handle the behavior, it may break downstream semantics.

### 2.6 Metadata, device ID, and session ID

Plan A/B show:

- `metadata.user_id` is a JSON string containing `device_id`, `account_uuid`, and `session_id`.
- `device_id` is a 64-character lowercase hex value persisted by the CLI config.
- `session_id` is a UUID generated at session start/reset and reused across the session.
- `X-Claude-Code-Session-Id` matches `metadata.user_id.session_id`.

Implication: for account safety, identity/session should be stable per account and conversation, but not derived from raw prompts in a way that creates deterministic cross-user fingerprints.

## 3. Current code coverage matrix

### 3.1 CC Gateway coverage

CC Gateway is relatively strong for outbound policy, with the important caveat that its current 2.1.175 profile is closest to a subscription/1M profile and must not be treated as a universal profile for API-key non-1M, simple, or bare requests:

- `src/policy.ts` builds canonical headers with allowlisted schema validation.
- `src/policy.ts` owns billing insertion, CCH computation, and pre-forward verification.
- `src/policy.ts` rejects duplicate/untrusted billing/CCH material.
- `src/policy.ts` normalizes `metadata.user_id` and ensures header/body session equality.
- `src/persona-registry.ts` already contains 2.1.175-style subscription/1M profiles with `mid-conversation-system-2026-04-07` and `effort-2025-11-24`.

Remaining CC Gateway cautions:

- It does not build body-level harness prompts, tools, reminders, thinking, or effort. That is currently outside `policy.ts`.
- Profile choice must remain explicit and fail-closed. Unknown future versions should not be silently accepted as native. The current registry does not fully model Plan B's captured API-key non-1M six-token profile or simple/bare profile, so account type, model, context-1m, and mode must be explicit inputs to profile selection.
- Sign-primary must remain protected by post-sign mutation checks.

### 3.2 Sub2API coverage

Sub2API has useful pieces but remains partial for 2.1.175 body-shape parity:

- `backend/internal/pkg/claude/constants.go` has many beta constants but currently lacks a Go constant for `mid-conversation-system-2026-04-07`.
- `DefaultBetaHeader`, `APIKeyBetaHeader`, and OAuth beta helpers do not cleanly model all captured 2.1.175 profiles.
- `normalizeClaudeOAuthRequestBody` ensures `tools` exists but does not synthesize native CLI tool schemas.
- `normalizeClaudeOAuthRequestBody` can inject `temperature` and `max_tokens` defaults that differ from the captured Opus 4.8 sample.
- It injects `context_management` only when thinking already exists; it does not default Opus 4.8 to adaptive thinking.
- `buildOAuthMetadataUserID` uses deterministic HMAC-derived session UUIDs based on account/client/first user text. Native CLI uses a random UUID per session and then reuses it.
- `NormalizeAnthropicCompatMessagesBody` correctly labels compat traffic as `claude_code_compat` / server-filled, but its generated reminder/env blocks are not the 2.1.175 native shapes.

This is acceptable for a transparent compat adapter, but not enough to claim full native Claude Code parity for non-CLI traffic.

## 4. Risks beyond CCH

CCH being correct only proves one integrity field matches the selected body. It does not prove the whole traffic pattern is safe. The remaining dimensions are:

### 4.1 Client attestation and spoofing risk

Inbound headers such as `User-Agent`, `x-app`, `x-stainless-*`, `anthropic-beta`, or even a client-provided billing block are not proof of native Claude Code. They are user-controlled HTTP fields.

Safe rule: native classification must be attestation/shape-first and fail-closed. Spoofed headers must not upgrade a request into the CLI-only pool.

### 4.2 Multi-user fanout through a small account pool

If 20 downstream users share 5 subscription accounts, the upstream-visible pattern can diverge from normal single-user CLI use even when each individual request shape looks valid:

- too many concurrent sessions per account;
- too many independent working directories/projects per account in a short window;
- inconsistent tool/harness behavior;
- session churn that does not match normal CLI usage;
- cross-user mixed histories on a sticky account;
- high error/retry loops after tool-call mismatches;
- egress changes for a fixed account.

Safe rule: account safety requires per-account budgets, sticky session mapping, fixed egress, and downstream fanout caps. Shape parity alone is not enough.

### 4.3 Tool capability truthfulness

Native Claude Code can execute tools because the CLI harness owns the local tool runner. A generic API client may not be able to execute the same tool calls. Injecting native tool schemas into a non-CLI request can cause:

- model emits tool calls the downstream cannot run;
- client retries or hangs;
- operator sees confusing errors;
- upstream sees a pattern of available tools without coherent tool-result follow-up.

Safe rule: only expose tool schemas backed by a real runner. For non-CLI clients, keep tools truthful or isolate them into a separate transparent compat mode.

### 4.4 Control-plane behavior

Native CLI has side routes and behavior outside the main `/v1/messages` request: count-token/classifier/title/control-plane/event-like paths vary by mode and feature. Current CC Gateway intentionally suppresses/blocks some control-plane routes.

Safe rule: do not fabricate control-plane traffic. Maintain route policy, suppress unsafe telemetry by default, and only enable canary/control-plane behavior through explicit, audited phases.

### 4.5 Identity and privacy

A formal pool account should have stable account identity and stable egress, but raw emails, raw UUIDs, raw tokens, and raw proxy credentials must not leak into logs or UI. At the same time, operator dashboards need readable labels.

Safe rule: UI may show operator-owned account display names/emails where authorized, but safe-deliverables/logs must use scoped refs. Runtime identity passed upstream must be generated by the account policy, not copied from arbitrary downstream clients.

## 5. Recommended production policy

### 5.1 Mode A: CLI-only formal subscription pool

This should be the default and highest-safety mode for Anthropic subscription accounts.

Ingress behavior:

- Target state: accept only requests that pass a verified native Claude Code policy. Current code must not treat UA, beta header, `x-app`, `metadata.user_id`, or CCH presence alone as sufficient attestation; Phase 1 must add tests and tighten the policy where needed.
- Reject/fallback non-CLI requests using existing group settings:
  - `claude_code_only` remains authoritative.
  - `fallback_group_id` remains available for ordinary fallback.
  - `fallback_group_id_on_invalid_request` remains orthogonal and must not become a synthesis gate.
- Do not let spoofed headers or body fields upgrade a request to native.

Outbound behavior:

- CC Gateway owns canonical headers, account identity rewrite, billing/CCH insertion, and verifier gates.
- Sign-primary must fail closed if the CCH/verifier/profile/session invariants do not hold.
- Existing account selection, egress, session-budget, RPM/concurrency, and formal-pool lifecycle mechanisms must remain active, and Phase 1/5 tests must prove which of them fail closed versus observe/degrade.

Operational behavior:

- Dashboard should report native vs rejected/fallback counts separately.
- Diagnostics should explain non-CLI rejection in Chinese and name the configured fallback behavior.

### 5.2 Mode B: transparent compatibility pool

This mode can support non-CLI clients, but it should be isolated from the highest-safety subscription formal pool unless explicitly approved.

Ingress behavior:

- Accept Anthropic `/v1/messages` shape only.
- Label traffic as compat/server-filled, not native.
- Reject OpenAI-shaped bodies from this mode unless a separate transparent protocol converter is explicitly enabled.

Outbound behavior:

- Do not claim native Claude Code attestation for arbitrary non-CLI traffic.
- Do not inject unsupported tools or fake local capabilities.
- Use stricter budgets and clearer operator labels.

Operational behavior:

- Separate pool/group from CLI-only production accounts.
- Separate metrics and alerting.
- No silent fallback into subscription accounts when compat shape is low-confidence.

### 5.3 Experimental synthesis mode

A future experimental mode should stay disabled by default. It should only be considered after:

- fixture parity tests exist;
- exact profile tables exist;
- tools are backed by a real runner;
- session/device identity is redesigned;
- explicit operator approval exists per group;
- legal/ToS/account-risk acceptance is documented.

Even then, it should not be described as native CLI traffic unless the inbound client is actually verified native CLI.

## 6. Implementation phases I recommend next

### Phase 0 — freeze evidence and add safe fixtures

Goal: make Plan A/B evidence reproducible without leaking raw sensitive material.

Work:

- Add a committed safe manifest summarizing Plan A/B artifact counts, structural summaries, and non-reversible artifact hashes where appropriate. Do not use unsalted hashes of raw prompts/bodies as durable identifiers.
- Add redacted fixture metadata for:
  - header profile table;
  - tool-name lists and counts;
  - simple/bare vs normal profile distinctions;
  - system block class/order, without raw prompt bodies.
- Do not commit raw `groundtruth.jsonl`, raw request bodies, raw prompts, raw tool arguments/results, raw CCH values, or raw signed bodies.

Acceptance:

- Sensitive scan passes.
- A reader can trace each design claim to a safe artifact path.

### Phase 1 — harden CLI-only gate without changing compat behavior

Goal: prove current `claude_code_only` behavior is preserved and strengthened.

Work:

- Add tests proving non-CLI requests are rejected or fallback-routed exactly as configured.
- Add tests proving spoofed Claude Code headers do not upgrade to native.
- Add tests proving `fallback_group_id_on_invalid_request` is not treated as permission to synthesize native persona.
- Ensure diagnostics/UI make the mode visible in Chinese.

Acceptance:

- Existing CLI traffic still works.
- Non-CLI traffic cannot enter CLI-only formal pool by spoofing headers/body.

### Phase 2 — profile table cleanup for verified CLI and CC Gateway

Goal: eliminate scattered beta/version constants and make 2.1.175 profile selection explicit.

Work:

- Add/align missing beta constant names such as `mid-conversation-system-2026-04-07` in Sub2API.
- Define explicit profile table entries for:
  - 2.1.175 subscription/1M;
  - 2.1.175 non-1M where supported;
  - simple/bare profile if it is used anywhere.
- Keep CC Gateway as source of truth for final outbound signing/profile enforcement.

Acceptance:

- Tests compare profile strings against Plan A/B safe facts.
- Unknown future persona versions fail closed until separately approved.

### Phase 3 — account identity/session redesign for formal pool

Goal: make multi-user sharing safer without copying downstream identities.

Work:

- Persist per-account generated device identity material in the formal-pool account record or CC Gateway identity config.
- Use a server-side session mapping/cache keyed by downstream conversation/session to produce stable UUIDs per conversation, rather than prompt-derived deterministic session IDs.
- Enforce max sessions/concurrency/RPM per account and report them in dashboard.
- Preserve account fixed-egress binding and make egress mismatch repair visible in diagnostics.

Acceptance:

- Same downstream conversation keeps one account/session identity.
- Different downstream conversations do not collide by identical first prompt.
- Raw email/UUID/token/proxy data do not leak into safe logs.

### Phase 4 — transparent compat pool only

Goal: serve non-CLI clients without risking the CLI-only formal pool.

Work:

- Add a group-level mode enum, for example:
  - `cli_only`
  - `transparent_compat`
- Keep existing booleans/fallback fields backward-compatible.
- In `transparent_compat`, keep server-filled audit labels and truthful tool behavior.
- Do not enable experimental native synthesis in this phase.

Acceptance:

- Operators can see which mode a group is using.
- Non-CLI clients in `cli_only` are rejected/fallback-routed.
- Non-CLI clients in `transparent_compat` are isolated and clearly labeled.

### Phase 5 — local joint smoke, then production canary

Goal: deploy only after localhost evidence matches the selected mode.

Work:

- Run Sub2API + CC Gateway localhost/mock smoke for both modes.
- Verify no real Anthropic egress during local smoke.
- Verify safe deliverables contain no raw sensitive fields.
- Only after explicit approval, run one low-cost production canary per mode/account class.

Acceptance:

- Local smoke passes.
- Production canary logs show correct mode, account ref/hash, egress ref, session ref, profile, and verifier status.
- Rollback path is documented.

## 7. Things I would not implement now

Do not implement these in the next phase:

- Do not route arbitrary non-CLI clients into the subscription formal pool by pretending they are native CLI.
- Do not inject the full native tool list unless a real compatible tool runner exists and the downstream protocol can handle tool calls/results.
- Do not add fake telemetry/control-plane uploads.
- Do not trust inbound `User-Agent`, `anthropic-beta`, `x-stainless-*`, `x-app`, or client-provided billing/CCH material.
- Do not silently accept future Claude Code versions without capture/verifier/profile review.
- Do not commit raw Plan A/B bodies or prompts.

## 8. Next concrete decision

Before implementation, choose the next work package:

1. **Recommended:** Phase 0 + Phase 1 only. This locks down safety and preserves existing production behavior.
2. Phase 0 + Phase 1 + Phase 2. This also cleans up verified 2.1.175 profile tables.
3. Defer all implementation and only keep this as an audit memo.

My recommendation is option 2: commit the safe evidence manifest, harden CLI-only behavior, and align profile tables for verified CLI traffic. Leave non-CLI transparent compatibility as a separately gated follow-up.
