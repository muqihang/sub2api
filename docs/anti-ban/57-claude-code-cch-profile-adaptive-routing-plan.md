# 57 - Claude Code Conditional CCH Profile-Adaptive Routing Plan

> **For agentic workers:** Execute this only after doc 56 local safety gates remain green. Use test-first changes. Do not enable real Anthropic formal-pool traffic, sign-primary, or live 3017 canary until the profile matrix and localhost full-chain gates in this document are green.

## Goal

Adapt Sub2API + CC Gateway formal-pool handling to current Claude Code reality: CCH is no longer a global invariant. Newer Claude Code versions can emit no-CCH billing blocks on custom base URLs while still emitting signed CCH on first-party/selected provider modes. The formal-pool boundary must select a safe server-owned upstream egress profile instead of trusting or blindly copying each end user's client version/shape.

## Current Evidence

All evidence below is safe summary only. No raw prompt, raw body, raw response, token, account UUID/email, proxy credential, or raw CCH value is committed here.

1. Upstream Sub2API `v0.1.138` / PR `Wei-Shaw/sub2api#3375` changed Claude Code mimicry to stop injecting `cch=<redacted-5hex>;`; `enable_cch_signing` is now documented no-op. The validator still accepts billing blocks without `cch=` when Claude Code UA/entrypoint evidence is present.
2. Local managed runtime `2.1.177` capture to localhost mock still emitted one billing block with `cch=` on `/v1/messages`.
3. Isolated latest `@anthropic-ai/claude-code@2.1.191` custom-base localhost capture emitted one billing block with `cc_version` and `cc_entrypoint=sdk-cli`, but no `cch=`.
4. The same isolated `2.1.191` capture with `_CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL=1` emitted signed `cch=` again.
5. For two `2.1.191` first-party-mode localhost captures, current CC Gateway `verifySignedCCH` returned `ok:true`, and `computeCCVersionSuffix` matched the observed `cc_version` suffix. This is preliminary proof that the 2.1.172+/2.1.175 CCH algorithm still applies to 2.1.191 first-party-mode samples, not proof that CCH should be used by default.

## Core Decision

Do **not** normalize every request to "latest Claude Code" and do **not** let end-user client versions choose formal-pool upstream identity.

Instead, separate two concepts:

1. **Observed inbound client profile**: safe audit/compat evidence derived from the end user's request, such as `cli_version_bucket`, `entrypoint`, and billing shape `absent | no_cch | cch_present`. This is not authority.
2. **Trusted egress profile**: server-selected per formal-pool account/route by Sub2API scheduler policy and verified by CC Gateway before upstream. This is authority.

## Formal-Pool Egress Profiles

### `strip_attribution` - production default

- Strip all downstream `x-anthropic-billing-header` blocks and HTTP billing/CCH material.
- Reject if any billing/CCH marker remains after rewrite.
- Maps to the official `CLAUDE_CODE_ATTRIBUTION_HEADER=false` style and maximizes shared prompt-cache stability.
- This remains the safest default for shared formal-pool accounts.

### `claude_code_custom_base_no_cch` - optional, oracle-gated

- Final body has a Claude Code billing block with `cc_version` + `cc_entrypoint`, but no `cch=`.
- Matches isolated `2.1.191` custom-base behavior and upstream Sub2API `v0.1.138` mimicry direction.
- Must be enabled only after localhost oracle proves exact block placement, entrypoint, suffix, beta/body shape, and cache effect for the selected CLI/profile.
- Must not accept client-supplied billing block as-is; CC Gateway must own final insertion or verification.

### `claude_code_first_party_signed_cch` - experimental/sign-primary only

- Final body has a signed `cch=` billing block.
- Allowed only when the selected profile/version/mode has oracle proof that real Claude Code emits CCH for that mode.
- `2.1.177` and `2.1.191 first-party-mode` have preliminary localhost evidence, but production sign-primary remains disabled until the full oracle matrix and live preflight gates are green.
- Any missing proof, unknown version, post-sign mutation, verifier mismatch, or retry mode downgrade must fail closed.


## 2.1.191 Safe Schema Delta From Localhost Capture

The following deltas were observed with identical localhost mock target and `claude-sonnet-4-6`; they are safe schema summaries only.

| Profile | CCH | Distinct request-shape differences vs 2.1.177 managed/custom capture |
| --- | --- | --- |
| `2.1.177 managed/current runtime` | present | Beta set contained `claude-code-20250219`, `context-management-2025-06-27`, `effort-2025-11-24`, `interleaved-thinking-2025-05-14`, `prompt-caching-scope-2026-01-05`; body had `context_management`, `output_config.effort`, `thinking`, 26 tools, 3 system blocks, 2 system cache-control blocks; no `diagnostics` top-level object in this minimal run. |
| `2.1.191 latest custom-base` | absent | Beta set added `advisor-tool-2026-03-01` and `thinking-token-count-2026-05-13`; body added/kept `output_config.effort`; still had 26 tools, 3 system blocks, 2 system cache-control blocks; no `diagnostics` top-level object in this minimal custom-base run. |
| `2.1.191 latest first-party-assumed` | present | Beta set additionally included `advanced-tool-use-2025-11-20` and `cache-diagnosis-2026-04-07`; headers included an extra request-id family; body included top-level `diagnostics.previous_message_id`; tool schemas had `eager_input_streaming`; tool count was lower in the minimal capture; system block count increased to 4. |

Implication: CCH is only one part of the version/mode drift. Beta tokens, diagnostics, tool-schema flags, system block layout, request-id header families, and output config also need profile-aware handling. For this formal-pool safety line, CC Gateway final verification checks the selected Anthropic formal-pool egress profile, not only the billing/CCH marker. Vertex/Bedrock/non-Anthropic beta filtering remains an out-of-scope follow-up patch.

## 2.1.191 CCH Algorithm Status

For the local `2.1.191 latest first-party-assumed` samples that contain CCH:

- current CC Gateway `verifySignedCCH` returned `ok:true` for two captured `/v1/messages` request bodies;
- current CC Gateway `computeCCVersionSuffix` matched the observed `cc_version` suffix for those same samples;
- therefore the current 2.1.172+/2.1.175 verifier appears compatible with these minimal 2.1.191 first-party-mode samples.

This is not enough to enable production sign-primary. Before enabling signed-CCH egress for any formal-pool account, rerun the oracle matrix across multiple prompts, streaming/non-streaming, with and without tools/subagents, and after body sanitization. If any sample fails, freeze signed-CCH egress and run a new recovery pass using the 2.1.191 raw-oracle method; do not patch by guessing constants.

### Required 2.1.191 CCH Regression Gate

The 2.1.172+/2.1.175 CCH recovery material is still the current reference until disproven, but it must be treated as an oracle-backed implementation detail, not an assumption. The implementation must add a regression gate that verifies the current CC Gateway CCH verifier against fresh `2.1.191` first-party-mode oracle captures before any `signed_cch` egress profile can be enabled.

Minimum variants for this gate:

- messages streaming and non-streaming;
- with and without tool definitions;
- with at least one tool-use/tool-result turn;
- with subagent/Agent-related metadata if Claude Code emits it;
- with context-management and prompt-cache beta tokens present;
- after Sub2API and CC Gateway sanitization/rewrite, not only against pristine raw localhost captures.

If any `2.1.191` signed-CCH sample fails verification, the correct response is to keep `signed_cch` disabled/fail-closed and run a new recovery pass from fresh raw oracle captures. Do not cargo-cult the old `docs/anti-ban/cch-algorithm.md`; for 2.1.172+ the valid references remain CC Gateway `docs/cch-2175-recovery-method.md` plus oracle tests, extended with new 2.1.191 evidence.

## Request-Shape Parity Diff Requirements

The localhost capture comparison must become a field-level parity matrix, not a one-line "has CCH" check. For every supported Claude Code profile/version, record and test only safe summaries of:

- HTTP route and method;
- user-agent family and Claude Code version bucket;
- Anthropic beta token set;
- request-id/header family presence, without raw IDs;
- top-level body key set;
- `messages` role/content-block type summary;
- `system` block count, type summary, and cache-control placement summary;
- tool count and tool schema feature flags such as `eager_input_streaming`;
- `thinking`, `output_config`, `context_management`, and `diagnostics` presence/shape;
- billing block count and billing shape: `absent | no_cch | cch_present`;
- cache-control presence summary for system/tools/history/top-level;
- response usage cache counter availability summary where the mock or live-safe upstream exposes it.

The adapter must not blindly forward all `2.1.191` deltas upstream. Each delta needs a disposition:

- **pass through** when the selected egress profile proves native compatibility;
- **strip/downscope** when the upstream profile does not support it or when it destabilizes cache/safety;
- **stub/defer** for control-plane-only capability requests;
- **fail closed** when the route/body/header shape is unknown and could affect formal-pool safety.

## Additional Gap Checklist

- Capture `2.1.179 stable` custom-base and first-party-assumed profiles; stable may differ from latest.
- Capture subagent/multiagent requests because billing can add subagent markers and tool lists differ.
- Capture count_tokens/control-plane requests separately; they must not reuse messages CCH signing.
- Capture WebSearch/WebFetch/ToolSearch capability paths; missing authorization may require bridge/stub, but must not leak into formal-pool native messages.
- Verify prompt-cache behavior per profile: billing block presence, beta set, diagnostics, tools, and effort changes can all break cache, not only CCH.
- Add safe audit fields to live canary before real upstream: selected egress profile, observed client profile, billing shape, beta profile, verifier result, cache read/write token counters, and fail-closed reason enums only.
- Confirm deployed CC Gateway commit/config equivalence after the 56/57 changes before any real formal-pool smoke.
- Capture OAuth-account and API-key-account variants if Claude Code emits different billing/session/persona/account-identity material for them; never store raw account IDs or secrets.
- Capture retry/error-recovery traffic because post-error retries can reorder or downscope fields and can break CCH/cache if not profile-aware.
- Capture MCP/settings/policy/control-plane traffic and verify it is classified before any messages signer/verifier path.
- Verify hot model switching and mixed subagent flows do not let DeepSeek/GPT/AGNES/Kimi/GLM reasoning, provider-private fields, tool internals, or cache annotations enter Claude formal-pool native egress.
- Verify DeepSeek/GPT Anthropic-compatible cache behavior separately from Claude formal-pool CCH; non-Claude cache fixes must not change Claude billing/CCH/profile verifier behavior.
- Verify sticky-session authority survives profile selection: account ref, credential type, egress bucket, persona/profile, policy version, session binding, route classification, control-plane disposition, and `egress_profile_ref` must be bound together.
- Verify unknown future Claude Code versions, unknown beta tokens, and unknown body keys cannot auto-enable `no_cch` or `signed_cch`; they must strip/downscope or fail closed.

## Required Implementation Delta

### Task A: Profile matrix and oracle artifacts

- Add a safe oracle matrix document/artifact for:
  - `2.1.177 managed/current runtime`
  - `2.1.179 stable custom-base`
  - `2.1.179 stable first-party-assumed`
  - `2.1.191 latest custom-base`
  - `2.1.191 latest first-party-assumed`
- Each row records only safe fields: version, entrypoint, path template, top-level key set, billing block count, billing has cc_version, billing has cch boolean, verifier ok boolean, suffix match boolean, beta token set names, cache-control presence summary.
- Do not store raw bodies in the repo. Raw localhost captures may stay in `/private/tmp` only while verifying.

### Task B: Sub2API trusted context extension

- Sub2API should strip/ignore end-client supplied authority headers as already required by doc 56.
- Add server-selected `egress_profile_ref` or equivalent to the trusted formal-pool context.
- Add non-authoritative `observed_client_profile` safe summary only for audit/debug; CC Gateway must not use this to relax safety.
- Ensure sticky session ledger binds `egress_profile_ref` together with account, credential, egress bucket, persona/profile, policy version, and session binding.

### Task C: CC Gateway profile-aware final verifier

- Replace any version-only sign-primary decision with `(egress_profile_ref, cli_version, provider/baseURL mode)` profile decision.
- Keep `strip_attribution` as default for `mode: sub2api` formal-pool.
- Add tests that unknown/newer versions fall back to `strip_attribution` or fail closed; they must never auto-enable signed CCH.
- Add tests that `2.1.191 custom-base no-cch` rejects accidental generated `cch=`.
- Add tests that `2.1.191 first-party signed-cch` can pass verifier only when explicit oracle/profile proof is configured.

### Task D: Backward compatibility

- Keep current `billing_cch_mode: strip | sign | disabled` behavior stable for existing config, but document it as a low-level mode.
- Add a higher-level formal-pool profile knob or mapping so operators do not have to infer correct behavior from CCH alone.
- `sign` remains forbidden by default for production formal-pool unless the selected profile proof is green.

### Task E: Cache and safety canary gates

- Before live 3017 or real formal-pool smoke, rerun localhost full-chain E2E with at least:
  - native old/current CCH-present inbound -> `strip_attribution` egress
  - latest no-CCH inbound -> `strip_attribution` egress
  - optional no-CCH profile egress -> final verifier passes and no CCH appears
  - optional signed-CCH profile egress -> verifier passes only with proof flag
- DeepSeek/GPT/multi-provider traffic must not enter Claude formal-pool billing/CCH signer paths.
- Control-plane routes must remain separate and must reject billing/CCH markers.

## Non-Goals

- Do not update the managed local runtime from `2.1.177` as part of this plan.
- Do not claim strict native parity for all Claude Code versions.
- Do not send real Anthropic upstream traffic to validate CCH until localhost oracle and deployed CC Gateway equivalence gates are green.
- Do not copy raw capture bodies, raw CCH values, credentials, prompts, responses, telemetry, account UUID/email, or proxy material into docs/tests.

## Acceptance Criteria

1. CC Gateway formal-pool safety remains independent of local Zhumeng takeover.
2. End-user client version/shape is observed but never trusted as authority.
3. Default formal-pool production path is `strip_attribution` and remains green.
4. `no_cch` and `signed_cch` are explicit oracle-gated egress profiles, not automatic fallbacks.
5. `2.1.191 custom-base` no-CCH and `2.1.191 first-party` CCH-present behavior are both represented in tests/docs.
6. Unknown future Claude Code versions fail closed or degrade to strip; they never auto-enable sign-primary.
