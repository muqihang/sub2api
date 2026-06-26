# 58 - Claude Formal-Pool Native CLI 2.1.179 Production Adaptation Plan

> **For agentic workers:** This plan supersedes doc 57 for the immediate production-safety workstream. Scope is only native/unmodified Claude Code CLI using Claude models through Sub2API formal-pool and CC Gateway. Do not mix in Zhumeng-managed Claude Code runtime, bridge models, DeepSeek/GPT/AGNES/Kimi/GLM hot-switching, or WebSearch/WebFetch bridge work while executing this plan.

## Goal

Make the native Claude formal-pool path production-safe for users running unmodified Claude Code CLI, with stable Claude Code `2.1.179` as the primary compatibility target and latest `2.1.191` as forward-compatibility evidence.

Target chain:

```text
User / unmodified Claude Code CLI
  -> Sub2API / Server API
     -> authenticate end user
     -> select and stick one server-owned Anthropic OAuth/API-key account
     -> generate trusted server-side formal-pool context
  -> CC Gateway
     -> verify trusted context and account/egress/persona/session/billing policy
     -> rewrite/strip/final-verify the native Anthropic request shape
  -> Anthropic upstream
```

## Non-Goals For This Plan

- No Zhumeng-managed Claude Code runtime changes.
- No bridge/non-Claude model work.
- No mixed-model hot switching or subagent model routing work, except native Claude Code subagent request-shape capture when the unmodified CLI emits it.
- No live formal-pool Anthropic smoke until all localhost/mock and deployed-equivalence gates are green.
- No production `signed_cch` enablement unless explicit 2.1.179 oracle/profile gates pass.

## Current Evidence Summary

Safe evidence only; no raw prompt/body/response/CCH/account identity/proxy/credential material is stored here.

- Upstream Sub2API PR [Wei-Shaw/sub2api#3375](https://github.com/Wei-Shaw/sub2api/pull/3375) says newer Claude Code mimicry should not inject a zero-placeholder `cch` attribution field; `enable_cch_signing` became no-op. The PR also fixes Vertex Anthropic beta-token filtering. It clarifies that "cch canceled" means the `cch` field only; the billing block with `cc_version` and `cc_entrypoint` remains.
- Issue [Wei-Shaw/sub2api#3358](https://github.com/Wei-Shaw/sub2api/issues/3358) shows recent Claude Code clients emit beta tokens such as `advisor-tool-2026-03-01`, `prompt-caching-scope-2026-01-05`, `redact-thinking-2026-02-12`, and `thinking-token-count-2026-05-13`; non-Anthropic upstreams may reject them unless filtered.
- Local managed `2.1.177` custom-base capture still emitted a billing block with `cch=`.
- Isolated latest `2.1.191` custom-base localhost capture emitted a billing block without `cch=`.
- Isolated latest `2.1.191` first-party-assumed localhost capture emitted signed `cch=` and current CC Gateway verifier matched two minimal samples.
- Therefore CCH is profile/mode dependent. The production boundary must not infer upstream identity from the end user's observed client request shape.

## Core Architecture Decision

Separate audit from authority:

1. `observed_client_profile`: a safe, non-authoritative summary of inbound native CLI shape. Examples: version bucket, route, billing shape `absent | no_cch | cch_present`, beta-token set names, top-level key set, tool/schema feature flags. Used only for audit, diagnostics, and compatibility metrics.
2. `trusted_egress_profile_ref`: a server-selected authority value produced by Sub2API scheduler and verified by CC Gateway. It binds account, credential type/ref, egress bucket, proxy identity ref, persona/profile, policy version, route class, session binding, billing/CCH disposition, and beta/profile disposition.

End-user headers/body can never authorize account, egress, persona, billing/CCH mode, 1m context, or control-plane disposition.

### Mandatory Profile-Aware Authority Fields

The trusted Sub2API -> CC Gateway formal-pool attestation must include these profile fields. Missing or mismatched values fail closed before any upstream egress:

- `egress_profile_ref`: server-selected outbound Claude Code profile, e.g. `strip_attribution`, optional `claude_code_2_1_179_custom_base_no_cch`, or optional `claude_code_2_1_179_first_party_signed_cch`.
- `profile_policy_version`: safe policy/version ref for the selected profile matrix.
- `billing_shape_policy`: allowed final billing shape, e.g. `strip`, `no_cch`, or `signed_cch`.
- `request_shape_profile_ref`: selected request-shape/profile gate for beta tokens, diagnostics, tools, system/cache-control placement, `thinking`, `output_config`, and `context_management`.
- `cache_parity_profile_ref`: selected cache-stability profile for deterministic prompt-cache shape.
- `observed_client_profile`: audit-only safe summary. This field must never authorize or relax `egress_profile_ref`, billing mode, persona/profile, context-1m, or route disposition.

CC Gateway must bind all of the above into its final verifier and session ledger. A later request for the same canonical session that changes any of these fields fails closed.

## Production Default

For shared formal-pool production, default to `strip_attribution`:

- Strip downstream `x-anthropic-billing-header` and billing/CCH markers from headers and body.
- Reject if billing/CCH markers remain after rewrite.
- Do not generate `cch=` by default.
- Preserve cache stability by making the server-selected egress profile deterministic for a sticky session/account.
- Keep `signed_cch` disabled/fail-closed until 2.1.179 oracle/profile evidence proves it is necessary and safe for the selected account/profile.

Optional profiles may exist only after explicit oracle gates:

- `claude_code_2_1_179_custom_base_no_cch`: final billing block has `cc_version` + `cc_entrypoint`, no `cch=`. Optional, not default.
- `claude_code_2_1_179_first_party_signed_cch`: final billing block has signed `cch=`. Experimental/sign-primary only.
- `claude_code_latest_observed`: audit-only profile for 2.1.191+ drift; never authority by itself.

## Required 2.1.179 Oracle Capture Matrix

Because `2.1.179` is current stable, it is the primary production target. Capture with localhost/mock only, using an isolated CLI install. Keep raw captures outside the repo, e.g. `/private/tmp`, and commit only safe summaries.

Required profiles:

1. `2.1.179 custom-base`
2. `2.1.179 first-party-assumed` using the same safe localhost technique used for 2.1.191
3. OAuth-backed account shape when available, without storing raw account data
4. API-key-backed account shape when available, without storing raw account data

Required route/body variants:

- `/v1/messages` streaming and non-streaming
- `/v1/messages/count_tokens`
- at least one tool definition set
- at least one tool_use / tool_result turn
- subagent/Agent-related request shape if unmodified Claude Code emits it in native Claude-only mode
- retry/error-recovery request after a synthetic upstream failure
- MCP/settings/policy/event logging/control-plane routes
- WebSearch/WebFetch/ToolSearch capability-control routes if emitted by unmodified Claude Code, classified as control-plane/capability paths rather than messages-signing material

Safe summary fields per capture:

- CLI version bucket and invocation mode, not raw command line with secrets
- route/method and route class
- Anthropic beta token set names
- request-id/header family presence, no raw IDs
- top-level body key set
- `system` block count/type summary and cache-control placement summary
- `messages` role/content-block type summary
- tool count and schema feature flags such as `eager_input_streaming`, `defer_loading`, `tool_reference`
- `thinking`, `output_config`, `context_management`, `diagnostics` presence/shape
- billing block count and billing shape: `absent | no_cch | cch_present`
- `cc_entrypoint` bucket, e.g. `cli | sdk-cli | other | absent`
- CCH verifier result boolean only for CCH-present samples
- cache-control placement summary and response cache-counter availability summary

## CCH Source Of Truth And Gates

- `docs/anti-ban/cch-algorithm.md` is legacy/background only and must not be used as the 2.1.172+/2.1.179 algorithm source.
- Current CCH implementation authority is CC Gateway `docs/cch-2175-recovery-method.md`, `docs/cch-oracle-regression.md`, and oracle-backed tests.
- For `2.1.179`, run the verifier against fresh first-party-assumed oracle captures before any sign-primary consideration.
- If any 2.1.179 signed sample fails, keep `signed_cch` disabled/fail-closed and perform a new 2.1.179 recovery pass. Do not patch constants by guessing.
- Unknown future versions, unknown beta tokens, unknown body keys, or unknown billing shapes must strip/downscope or fail closed; they must never auto-enable `no_cch` or `signed_cch`.
- CC Gateway sign/no-CCH decisions must be profile-gated by `(egress_profile_ref, cli_version, provider/baseURL mode, oracle_profile_ref)` and never by version alone.
- Current CC Gateway behavior that only special-cases a single version, or lets non-special-cased versions sign by default, is not production-acceptable for formal-pool.

## Sub2API Required Adaptation

Sub2API owns end-user auth, formal-pool account scheduling, sticky session policy, and trusted context production.

Required behavior:

1. Strip/ignore/reject end-client supplied authority headers before scheduler context creation:
   - `x-cc-*`
   - `x-sub2api-*` authority/control headers
   - account/credential/egress/persona/profile/context-1m/runtime-registration headers
   - route-trust/control-plane disposition headers
   - billing/CCH authority headers that would affect CC Gateway policy
   - client-supplied beta/context-1m/profile hints when they would change trusted policy
2. Select the server-owned tuple from scheduler state only:
   - `route_class`
   - `account_ref`
   - `credential_type`
   - `credential_ref`
   - selected raw credential source, never logged
   - `egress_bucket`
   - `proxy_identity_ref`
   - `persona_profile`
   - `policy_version`
   - `trusted_egress_profile_ref`
   - `profile_policy_version`
   - `billing_shape_policy`
   - `request_shape_profile_ref`
   - `cache_parity_profile_ref`
   - canonical `session_binding`
   - timestamp and nonce
3. Generate a formal-pool context attestation using a secret unavailable to end users and distinct from ordinary gateway/client tokens.
4. Persist or enforce sticky binding of account/ref, credential type/ref, egress bucket, proxy identity, persona/profile, policy version, egress profile, route class, and session binding.
5. Emit only safe `observed_client_profile` summaries for audit; do not let this field relax downstream CC Gateway checks.
6. Do not log raw body, raw prompt, raw response, raw CCH, raw credential, raw account UUID/email, raw proxy material, Authorization, cookies, or API keys.

Required Sub2API tests:

- Forged inbound authority headers are stripped/ignored and cannot change selected account/egress/persona/profile/billing mode.
- Same user/session remains sticky to the same trusted tuple.
- Attempting to change account/credential/egress/profile/session on an existing sticky session fails closed.
- Missing scheduler tuple fields fail closed before CC Gateway forwarding.
- `observed_client_profile` is safe-summary only and non-authoritative.
- 2.1.179 custom-base and first-party-assumed safe-summary fixtures are accepted only as observations, not authority.
- Client-requested 1M/context-management/profile/beta changes are recorded as observed fields only unless scheduler policy independently selects and signs the corresponding trusted profile.

## CC Gateway Required Adaptation

CC Gateway owns final formal-pool safety verification before Anthropic upstream.

Required behavior:

1. Verify Sub2API formal-pool context attestation before trusting any authority-bearing field.
2. Fail closed on missing, expired, replayed, malformed, or mismatched attestation.
3. Look up `account_identities` by trusted account ref/id only.
4. Verify selected credential type/ref and transient credential binding against the selected account identity; never log raw credential or raw digest.
5. Verify egress bucket is enabled, has an explicit non-empty allowlist, and includes the selected account.
6. Verify proxy identity ref/bucket matches the trusted tuple.
7. Resolve persona/profile from trusted policy only; strip user-supplied persona headers.
8. Rewrite/verify `metadata.user_id` from selected account/device/session safe refs; downstream metadata identity is not authority.
9. Enforce persistent/shared session authority ledger or explicit single-instance sticky admission. Ledger mismatch across account, credential, egress, persona, device, policy, egress profile, or session fails closed.
10. Separate control-plane routes from messages signer/verifier paths. Control-plane must block/stub/suppress/defer safely and must not forward raw telemetry/prompt/body/CCH.
11. Apply billing/CCH disposition from trusted `trusted_egress_profile_ref` plus config, not from observed client version.
12. Apply request-shape/cache profile gates from trusted `request_shape_profile_ref` and `cache_parity_profile_ref`, not from observed client beta/body shape.
13. Strip all internal formal-pool context, attestation, scheduler, and control headers before upstream.
14. Run final-output verifier immediately before upstream egress.
15. No direct Anthropic bypass is allowed if CC Gateway preflight/final verifier fails or is unavailable.

Required CC Gateway tests:

- Gateway-only native CLI positive path: no local Zhumeng guard evidence, complete trusted server-side contract, final verifier passes, localhost mock receives one sanitized request.
- Forged end-client `x-cc-*`/persona/account/egress/control headers do not affect upstream.
- Missing account identity, credential ref/type, egress allowlist, proxy identity, persona/profile, policy version, session binding, route class, or egress profile fails closed.
- Replayed/expired/mismatched attestation fails closed.
- Existing session attempting account/credential/egress/persona/profile/device/policy/egress-profile change fails closed.
- `strip_attribution` removes inbound CCH/no-CCH billing blocks and fails if markers remain.
- `2.1.179 custom-base no-cch` optional profile rejects accidental/generated `cch=`.
- `2.1.179 first-party signed-cch` optional profile only passes with explicit oracle/profile proof.
- Unknown future version/profile does not auto-enable signed or no-CCH profile.
- Control-plane count_tokens/event/MCP/settings/policy routes cannot enter messages CCH signer.
- Cache/parity profile mismatch, unknown beta token, unknown diagnostics/body key, or unapproved tool-schema feature flag is either stripped/downscoped by policy or fails closed.

## Beta/Profile And Cache Stability Requirements

Prompt caching is affected by more than CCH. The selected egress profile must also own:

- Anthropic beta token set and filtering/downscope policy
- system block layout and cache-control placement
- tool schema feature flags
- diagnostics presence
- thinking/output_config/context_management disposition
- model/max_tokens normalization when signing is used
- account/session/persona consistency

This is not only a documentation matrix. CC Gateway final verifier must enforce the selected request-shape/cache profile at the same trust boundary where it enforces account, egress, persona, session, and billing/CCH. Profile enforcement may choose pass-through, strip/downscope, stub/defer, or fail-closed, but it cannot silently accept unknown production shapes.

For Anthropic upstream formal-pool, the selected native profile can pass through proven Anthropic-compatible beta tokens. For Vertex/Bedrock/non-Anthropic paths, profile-specific filtering must happen after policy evaluation and before final body/header send. The Vertex PR 3375 behavior is relevant if those accounts are in the pool, but it must not change Claude Anthropic formal-pool signer/verifier decisions.

Cache acceptance gates:

- localhost/mock summaries prove deterministic final request shape for repeated 2.1.179 native messages under the same sticky tuple.
- live canary may record only cache read/write token counters and safe profile IDs.
- No raw prompt/body/response is logged for cache debugging.

## Deployment And Production Gates

Do not send real shared formal-pool traffic until all are green:

1. 2.1.179 localhost oracle matrix safe summaries completed for custom-base and first-party-assumed.
2. Sub2API targeted tests green for header stripping, trusted context production, sticky session tuple, and safe observed-client audit.
3. CC Gateway targeted tests green for config validation, attestation, account/credential binding, egress allowlist, persona/session metadata rewrite, billing/CCH strip/sign gates, control-plane isolation, and final verifier.
4. Localhost full-chain E2E green:
   - native CCH-present inbound -> default `strip_attribution`
   - native no-CCH inbound -> default `strip_attribution`
   - optional no-CCH profile -> final verifier passes and no `cch=` appears
   - optional signed profile -> fails closed unless oracle proof enabled
5. Sensitive scan green across generated artifacts.
6. CodeGraph re-indexed for Sub2API and CC Gateway after code changes.
7. Deployed CC Gateway image/commit/config/profile equivalence proven against the tested exact commit and a safe config/profile hash.
8. No-bypass preflight proves CC Gateway unavailable/preflight failed => no direct Anthropic fallback.
9. Only then: explicit user-approved real-canary smoke with tiny cost envelope, safe audit fields only, no 3012 changes.

## Acceptance Criteria

- Formal-pool safety does not depend on local Zhumeng takeover.
- Sub2API is the only producer of trusted server-side formal-pool context.
- CC Gateway independently verifies account identity, credential binding, egress bucket, persona/profile, session binding, metadata identity, billing/CCH disposition, control-plane route class, and final-output safety.
- Stable Claude Code 2.1.179 is explicitly captured, summarized, and tested.
- Latest 2.1.191 drift is treated as forward-compat evidence only, not as automatic production authority.
- Production default remains `strip_attribution` until no-CCH or signed-CCH egress profiles are explicitly oracle-gated and approved.
- Unknown or future request shapes fail closed or downscope safely.
- No secrets or raw sensitive request/response material are persisted.
- CC Gateway docs, CCH oracle docs, and config examples explicitly describe 2.1.179 stable production policy and 2.1.191 latest as forward-compat evidence only.

## Execution Checkpoints

### CP1 - 2.1.179 Oracle Capture And Safe Profile Matrix

- Install/copy Claude Code `2.1.179` into an isolated temp/runtime path; do not modify the managed local `2.1.177` runtime.
- Capture localhost/mock custom-base and first-party-assumed variants.
- Do not assume `cc_entrypoint=cli` or `sdk-cli`; record the observed bucket and make profile behavior oracle-driven.
- Do not assume `cch=` is present or absent; record billing shape and verifier result if present.
- Produce a safe matrix artifact with only summaries listed above.
- Sensitive scan the artifact.

Exit gate: safe matrix exists for at least messages streaming/non-streaming plus one tool-shaped request; any missing variants are explicitly marked blocker or degraded scope.

### CP2 - Sub2API Producer Contract

- Add/verify trusted context construction after scheduler sticky selection.
- Add `trusted_egress_profile_ref` to the server-owned tuple.
- Add `profile_policy_version`, `billing_shape_policy`, `request_shape_profile_ref`, and `cache_parity_profile_ref` to the server-owned tuple.
- Add audit-only `observed_client_profile` safe summary.
- Strip/ignore forged client authority headers before scheduling and before forwarding.
- Bind sticky session to account, credential, egress, proxy identity, persona/profile, policy version, route class, egress profile, request-shape/cache profile, billing-shape policy, and session.

Exit gate: focused Go tests prove forged headers cannot steer the formal-pool account/egress/persona/billing/profile path.

### CP3 - CC Gateway Consumer And Final Verifier

- Verify formal-pool context attestation before trusting any authority-bearing field.
- Add `trusted_egress_profile_ref` to session authority ledger and mismatch checks.
- Add `profile_policy_version`, `billing_shape_policy`, `request_shape_profile_ref`, and `cache_parity_profile_ref` to session authority ledger and mismatch checks.
- Map profile refs to billing/CCH and beta/body dispositions.
- Keep `strip_attribution` as default.
- Gate `no_cch` and `signed_cch` profile use on explicit 2.1.179 oracle profile proof.
- Reject sign/no-cch auto-promotion for unknown versions or unknown profile refs.

Exit gate: TypeScript targeted tests prove positive gateway-only path plus all required fail-closed paths.

### CP4 - Localhost Full-Chain E2E

Run full chain with real upstream disabled:

```text
synthetic/unmodified-native-shaped Claude Code request
  -> Sub2API harness
  -> CC Gateway harness
  -> localhost mock Anthropic upstream
```

Required scenarios:

- valid trusted formal-pool context -> one sanitized mock request;
- forged authority headers -> ignored or fail-closed, no steering;
- missing trusted context -> fail-closed, zero upstream;
- CCH-present inbound -> default strip attribution -> no billing/CCH upstream;
- no-CCH inbound -> default strip attribution -> no billing/CCH upstream;
- optional no-CCH profile -> no `cch=` upstream only when proof enabled;
- optional signed-CCH profile -> fail-closed unless proof enabled;
- CC Gateway unavailable/preflight failed -> no direct Anthropic fallback.

Exit gate: local report `PASS`, sensitive scan `PASS`, `real_anthropic_upstream=false`.

### CP5 - Production Readiness Gate

Before live canary or production rollout:

- CodeGraph index refreshed for both repos.
- Sub2API and CC Gateway test commands green.
- CC Gateway deployed image commit equals tested commit.
- Runtime config/profile hash equals tested formal-pool profile config, secrets excluded from evidence.
- Rollback mode is safe: disable formal-pool egress or force `strip_attribution`; never fall back to direct Anthropic or ungated sign-primary.
- User explicitly approves any real formal-pool smoke.

Exit gate: production checklist signed off with exact commit/config refs and no raw sensitive material.

## Self-Review Notes

- This plan intentionally does not decide whether 2.1.179 should use no-CCH or signed-CCH upstream. That is an oracle result, not a design assumption.
- This plan intentionally treats 2.1.191 latest as forward-compat evidence only. Stable 2.1.179 drives production adaptation.
- This plan intentionally defaults shared formal-pool to strip attribution. Strict native mimicry/sign-primary is an optional future profile, not required for the baseline safety objective.
- If strict native first-party parity later becomes a product requirement, it needs its own plan after 2.1.179 oracle proof is green.
