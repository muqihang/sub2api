# Claude Code Native Safety and Multiprovider Final Evidence Report

Date: 2026-06-25
Worktree: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime`
Plan: `docs/anti-ban/53-claude-code-native-safety-and-multiprovider-final-gap-remediation-plan.md`; formal-pool P0 overlay: `docs/anti-ban/56-cc-gateway-formal-pool-independent-safety-p0-remediation-plan.md`

## Overall status

`PASS_WITH_DEGRADED_SCOPE` for the local remediation implementation, with explicit external/canary blockers. The latest CP5 localhost-only full-chain E2E passes with `real_anthropic_upstream=false`; live 3017/L8 remains a separate gate.

The latest localhost-only full-chain E2E was rerun on 2026-06-25 after CP5 and passed end-to-end without real upstream traffic: synthetic/local Claude Code request -> local guard -> Sub2API harness -> CC Gateway harness -> localhost mock. This is safe evidence for the gateway-only/native safety contract, not evidence for live 3017 L8 or strict 2.1.177 native parity.

The implementation and reviews establish safe degraded behavior for gateway-only native CLI ingress, managed local takeover, bridge routing, control-plane fail-closed behavior, cache attribution, hot-switch replay safety, and the 2.1.177 evidence gate. This is not a claim of strict first-party Claude Code 2.1.177 native parity.

The following claims are safe now:

- Native Claude messages path has a strong final-output safety boundary.
- Gateway-only native CLI ingress does not depend on local Zhumeng takeover evidence as its only safety boundary; Server API/CC Gateway must own trusted account/egress/persona/session/control context.
- Bridge model hot-switches are guarded by replay-safety cleaning and safe audit diagnostics.
- DeepSeek/GPT cache attribution is safer and more truthful than before: cache counters are not invented, and zero/miss reason buckets are recorded where implemented.

The following claims are not safe yet:

- Upstream Anthropic sees every request as byte/shape-identical to a real Claude Code 2.1.177 client.
- 2.1.177 sign-primary is ready by default.
- WebSearch/WebFetch are fully first-party equivalent.
- L8 live cache hit rate reaches 95-99% across all bridge providers.


## Localhost full-chain preflight evidence (2026-06-24)

User reran the localhost-only full-chain controller from a normal Terminal after the explicit canary routing fix.

- Safe deliverable: `/private/tmp/full-chain-controller-20260624-012117-349394-50773/safe-deliverable/report.json` and `report.md`.
- Overall status: `PASS`; sensitive scan: `PASS`; `real_anthropic_upstream=false`.
- Scenario A: `PASS`, mock upstream request count `0`, cost envelope block `true`, Sub2API selected count `0`.
- Scenario B: `PASS`, Sub2API selected count `1`, mock upstream request count `1`, client observed `200`, CC Gateway mock request URL `/v1/messages?beta=true`.
- Scope: this validates the localhost-only gateway-only/native safety path and the explicit canary canary-only routing fix. It does not validate live 3017, deployed CC Gateway equivalence, 95-99% cache hit rate, WebSearch/WebFetch parity, or strict Claude Code 2.1.177 native parity.


## 2026-06-25 Formal-pool P0 Checkpoint 3 evidence refresh

Status for Checkpoint 3 (trusted context attestation, sticky ledger, credential binding): `PASS` for local targeted + localhost-only full-chain scope.

Claim scope: `Gateway-only native CLI / Server API + CC Gateway P0 safety boundary` remains `PASS_WITH_DEGRADED_SCOPE` overall because deployed CC Gateway image/commit equivalence, live 3017 rebuild/restart and smoke, strict Claude Code 2.1.177 parity, sign-primary readiness, WebSearch/WebFetch bridge, and 95-99% cache-hit evidence are still external/canary gates.

What is now locally proven for the Sub2API -> shared formal-pool -> CC Gateway -> Anthropic chain:

- Sub2API runtime registration is synchronized with the CC Gateway authority contract: registration JSON carries `token_type`; credential proof is sent only as the appropriate proof header and is not placed in the JSON body, logs, errors, or evidence.
- Sub2API onboarding, operations runtime-register, and startup replay paths derive registration proof only from server-side account credentials; missing selected access-token material leaves credential binding empty and therefore fails closed downstream.
- CC Gateway runtime registration verifies `token_type` plus selected credential proof against `credential_binding_hmac` at registration time; raw proof is not persisted.
- CC Gateway rejects old/persisted runtime mappings that lack `token_type`, rejects unsupported `gateway_generated` session policy until implemented, and rejects silent authority overwrite for existing `account_id` or `egress_bucket` mappings.
- CC Gateway uses one formal-pool production helper so `production_upstream_enabled=true` also requires persistent session authority ledger semantics/fail-closed behavior.
- Control-plane unsupported provider/token-type errors use stable fixed messages and do not reflect untrusted header values.
- Same formal-pool session authority remains sticky across account, credential, egress, policy, persona, and device; production ledger absence/corruption fails closed.
- 3012 and 3017 were not touched. CC Gateway work was done in `/Users/muqihang/chelingxi_workspace/cc-gateway` on `codex/formal-pool-p0-safety`; Sub2API work stayed in `.worktrees/claude-code-multiprovider-runtime`.

Latest targeted test evidence:

Sub2API:

- `go test ./internal/service -run 'FormalPool|CCGateway|ClaudeCodeSession|GatewayService_CCGateway|FormalPoolGatewayHealthcheck|OpenAICompatibleLive|DeepSeekOpenAICompatibleFallback' -count=1` -> `ok`.
- `go test ./internal/config -run CCGateway -count=1` -> `ok`.
- `go test ./.tmp-harness/cli-through-sub2api -count=1` -> `ok`.
- `python3 -m unittest tools.tests.test_cli_control_plane_full_chain_controller` -> `Ran 14 tests`, `OK`.

CC Gateway:

- `node --import tsx tests/security-boundary.test.ts` -> `4 passed, 0 failed`.
- `node --import tsx tests/config.test.ts` -> `23 passed, 0 failed`.
- `node --import tsx tests/preflight-safety.test.ts` -> `7 passed, 0 failed`.
- `node --import tsx tests/session-and-beta-policy.test.ts` -> `12 passed, 0 failed`.
- `node --import tsx tests/checkpoint3-remediation.test.ts` -> passed as part of the targeted chain.
- `node --import tsx tests/canary-cost-envelope.test.ts` -> passed as part of the targeted chain.
- `node --import tsx tests/proxy-sub2api.test.ts` -> `36 passed, 0 failed`.
- `npm run build` -> `tsc` passed.

Localhost-only full-chain E2E:

- Command: `tools/zhumeng-agent/.venv/bin/python tools/cli_control_plane_full_chain_controller.py --tmp-root /private/tmp`.
- Safe deliverable: `/private/tmp/full-chain-controller-20260625-033427-418394-73274/safe-deliverable/report.json` and `report.md`.
- Overall: `PASS`; sensitive scan: `PASS`; `real_anthropic_upstream=false`.
- Scenario A: `PASS`; mock request count `0`; cost-envelope block `true`; Sub2API selected count `0`; client observed `422`.
- Scenario B: `PASS`; mock request count `1`; Sub2API selected count `1`; client observed `200`; controller stop-requested events `1`.

Review / indexing / sensitive evidence:

- Goodall GPT-5.5 high read-only re-review verdict: `PASS`; all six old Aquinas findings closed.
- Review package: `/private/tmp/cp3-rereview-20260625-101304`.
- Safe-deliverable sensitive scan of full-chain report: `files_scanned=2`, `findings=0`.
- Safe-deliverable sensitive scan of review package: `files_scanned=2`, `findings=0`; custom token-shape scan also `PASS` after fixture redaction.
- Sub2API CodeGraph: `codegraph sync . && codegraph status .` -> up to date after CP4 changes.
- CC Gateway CodeGraph: `codegraph sync . && codegraph status .` -> up to date after CP4 changes.

Remaining external / degraded gates stay explicit:

| Gate | Status | Reason |
|---|---|---|
| Deployed CC Gateway image/commit equivalence | `BLOCKED_EXTERNAL_EVIDENCE` | Local branch tests passed, but deployed image/config equivalence has not been proven. |
| Live 3017 rebuild/restart and smoke | `BLOCKED_EXTERNAL_EVIDENCE` | 3017 was intentionally not touched in CP3. |
| Strict Claude Code 2.1.177 native parity | `PASS_WITH_DEGRADED_SCOPE` | Local gates prevent unsafe forwarding; strict parity still needs oracle/profile evidence. |
| 2.1.177 sign-primary readiness | `BLOCKED_EXTERNAL_EVIDENCE` | Must remain fail-closed until oracle/profile/CCH evidence is green. |
| WebSearch/WebFetch bridge | `PASS_WITH_DEGRADED_SCOPE` | Still explicit degraded/future bridge scope. |
| 95-99% cache hit rate | `BLOCKED_EXTERNAL_EVIDENCE` | Requires live stable-prefix evidence with safe counters/reason buckets. |


## 2026-06-25 Formal-pool P0 Checkpoint 4 evidence refresh

Status for Checkpoint 4 (internal control hardening, per-account device identity, 2.1.177 sign gate, and new failure-path redaction): `PASS` for local targeted test/build/review scope.

What is now locally proven for CP4:

- Task 6: CC Gateway runtime registration and protected internal persona/context/control headers require trusted internal control; Sub2API owns/overwrites trusted context rather than accepting end-client authority headers.
- Task 7: formal-pool runtime registration uses a server-generated per-account 64-hex `device_id`; missing or invalid device identity fails closed with safe diagnostic code `CC_GATEWAY_DEVICE_ID_REQUIRED`.
- Task 9: 2.1.177 sign-primary is default fail-closed unless explicit oracle/profile approval and safe proof ref are configured; the full proxy path fails before upstream when proof is absent.
- Task 9B: Sub2API and CC Gateway new formal-pool failure paths return or persist stable safe codes/messages only; raw control-plane response bodies/unsafe headers and replay inputs are not stored in evidence.
- Documentation: CC Gateway README and config example distinguish standalone global `identity.device_id` from formal-pool per-account runtime `device_id`, and document internal-control-only runtime registration.

Latest CP4 targeted test evidence:

Sub2API:

- `go test ./internal/service ./internal/handler ./internal/config -run 'TestGatewayService_CCGateway|TestCCGateway|TestFormalPool|Test.*Runtime.*Register|Test.*Internal.*Header|Test.*Redact|Test.*2177|Test.*FormalPool.*Redact|TestGatewayNative|TestFormalPoolHTTPCCGatewayRuntimeRegistrarFailureRedactsControlPlaneBody|TestFormalPoolRuntimeRegistrationReplayService_InvalidDeviceIDStoresOnlySafeFailureCode|TestCCGatewayControlPlaneFailurePathsRedactSensitiveMessages' -count=1` -> service/handler/config `ok`.
- `PYTHONDONTWRITEBYTECODE=1 tools/zhumeng-agent/.venv/bin/python -m pytest tools/tests/test_cli_control_plane_full_chain_controller.py tools/tests/test_cli_control_plane_guard.py -q -p no:cacheprovider` -> `95 passed, 17 subtests passed`.

CC Gateway:

- `npx tsx tests/proxy-sub2api.test.ts && npx tsx tests/checkpoint3-remediation.test.ts && npx tsx tests/session-and-beta-policy.test.ts && npx tsx tests/security-boundary.test.ts && npx tsx tests/config.test.ts && npm run build` -> targeted tests passed and `tsc` passed.

Review / indexing / sensitive evidence:

- Kant GPT-5.5 high read-only review verdict: `PASS`; no Critical or Important blockers.
- Gauss replacement review agent was closed after Kant returned to avoid duplicate review.
- CC Gateway CodeGraph: `codegraph sync .` synced 3 changed files; `codegraph status .` -> up to date.
- Sub2API CodeGraph: `codegraph sync .` synced 7 changed files; `codegraph status .` -> up to date.
- CP4 evidence text records only safe refs/status/codes and test counts; it does not include raw API keys, tokens, cookies, Authorization, prompts, bodies, responses, telemetry, CCH, account UUID/email, proxy credentials, or raw digest/HMAC inputs.

Remaining external / degraded gates stay explicit:

| Gate | Status | Reason |
|---|---|---|
| Deployed CC Gateway image/commit equivalence | `BLOCKED_EXTERNAL_EVIDENCE` | Local branch tests passed, but deployed image/config equivalence has not been proven. |
| Live 3017 rebuild/restart and smoke | `BLOCKED_EXTERNAL_EVIDENCE` | 3017 was intentionally not touched in CP4. |
| Strict Claude Code 2.1.177 native parity | `PASS_WITH_DEGRADED_SCOPE` | Local gates prevent unsafe forwarding; strict parity still needs oracle/profile evidence. |
| 2.1.177 sign-primary readiness | `BLOCKED_EXTERNAL_EVIDENCE` | Must remain fail-closed until oracle/profile/CCH evidence is green. |
| WebSearch/WebFetch bridge | `PASS_WITH_DEGRADED_SCOPE` | Still explicit degraded/future bridge scope. |
| 95-99% cache hit rate | `BLOCKED_EXTERNAL_EVIDENCE` | Requires live stable-prefix evidence with safe counters/reason buckets. |


## 2026-06-25 Formal-pool P0 Checkpoint 5 evidence refresh

Status for Checkpoint 5 (docs/capture reconciliation and final local verification): `PASS_WITH_DEGRADED_SCOPE` after high-spec read-only review. Local test/build/E2E evidence is PASS; deployed/live and strict-parity gates remain explicit blockers.

What CP5 adds:

- Task 8: CC Gateway now has `docs/formal-pool-sub2api-safety.md`, an operator-facing formal-pool safety boundary document for `mode: sub2api` production. It records required server-side context, CC Gateway final-output responsibilities, safe capture field families, safe operator evidence fields, and known degraded claims.
- Task 8: README Gateway Modes now links to the formal-pool safety boundary document.
- Task 10: Full local CC Gateway targeted verification passed under the existing test runner; build passed.
- Task 10: Localhost-only full-chain E2E passed with real Anthropic upstream disabled.

Latest CP5 targeted evidence:

CC Gateway docs TDD:

- `npx tsx tests/formal-pool-safety-doc.test.ts` RED: missing `docs/formal-pool-sub2api-safety.md` and README link.
- `npx tsx tests/formal-pool-safety-doc.test.ts` GREEN: `3 passed` after adding the doc and link.

CC Gateway verification:

- `npm test -- tests/config.test.ts tests/policy-cch.test.ts tests/persona-resolver.test.ts tests/persona-registry.test.ts tests/session-and-beta-policy.test.ts tests/checkpoint3-remediation.test.ts tests/proxy-sub2api.test.ts tests/security-boundary.test.ts tests/preflight-safety.test.ts` -> existing runner imported all 16 test files and reported `186 passed, 0 failed`.
- `npm run build` -> `tsc` passed.

Localhost-only full-chain E2E:

- Command: `tools/zhumeng-agent/.venv/bin/python tools/cli_control_plane_full_chain_controller.py --tmp-root /private/tmp` with Anthropic/proxy env vars unset, `TMPDIR=/private/tmp`, and `GOCACHE=/private/tmp/sub2api-go-cache`.
- Safe deliverable: `/private/tmp/full-chain-controller-20260625-062539-569056-98262/safe-deliverable/report.json` and `report.md`.
- Overall: `PASS`; sensitive scan: `PASS`; `real_anthropic_upstream=false`.
- Scenario A: `PASS`; mock request count `0`; cost-envelope block `true`; Sub2API selected count `0`; client observed `422`.
- Scenario B: `PASS`; mock request count `1`; Sub2API selected count `1`; client observed `200`; controller stop-requested events `1`.

- Anscombe GPT-5.5 high read-only review verdict: `PASS_WITH_DEGRADED_SCOPE`; Task 8/10 local evidence accepted and external/live blockers remain explicit.

- CodeGraph after CP5: CC Gateway `codegraph sync .` synced 1 changed file and status is up to date; Sub2API `codegraph sync .` synced 1 changed file and status is up to date.
Current safe final status map:

| Claim / gate | Status | Reason |
|---|---|---|
| Gateway-only native CLI / Server API + CC Gateway P0 safety boundary | `PASS_WITH_DEGRADED_SCOPE` | Local CC Gateway targeted tests/build and localhost-only full-chain E2E passed; deployed/live gates remain separate. |
| Deployed CC Gateway image/commit equivalence | `BLOCKED_EXTERNAL_EVIDENCE` | Local branch tests passed, but deployed image/config equivalence has not been proven. |
| Live 3017 rebuild/restart and smoke | `BLOCKED_EXTERNAL_EVIDENCE` | 3017 was intentionally not touched in CP5. |
| Strict Claude Code 2.1.177 native parity | `PASS_WITH_DEGRADED_SCOPE` | Local gates prevent unsafe forwarding; strict parity still needs oracle/profile evidence. |
| 2.1.177 sign-primary readiness | `BLOCKED_EXTERNAL_EVIDENCE` | Must remain fail-closed until oracle/profile/CCH evidence is green. |
| WebSearch/WebFetch bridge | `PASS_WITH_DEGRADED_SCOPE` | Still explicit degraded/future bridge scope. |
| 95-99% cache hit rate | `BLOCKED_EXTERNAL_EVIDENCE` | Requires live stable-prefix evidence with safe counters/reason buckets. |

Sensitive evidence policy: this CP5 evidence records only safe statuses, counts, file paths, stable codes, and summaries. It does not include raw API keys, tokens, cookies, Authorization, prompts, bodies, responses, telemetry, CCH, account UUID/email, proxy credentials, or raw digest/HMAC inputs.


## 2026-06-25 Formal-pool Native CLI 2.1.179 Production Adaptation CP5 evidence

Status for plan `docs/anti-ban/58-claude-formal-pool-native-cli-2179-production-adaptation-plan.md`: `PASS_WITH_DEGRADED_SCOPE` for localhost/mock and local test gates. Live canary / real formal-pool smoke remains blocked until deployed image/config/profile equivalence is proven and the user explicitly approves the smoke.

Scope kept for this CP5:

- Only native/unmodified Claude Code CLI -> Sub2API / Server API -> formal-pool scheduler -> CC Gateway -> Anthropic formal-pool safety.
- No Zhumeng-managed Claude Code runtime behavior, bridge multi-model routing, WebSearch/WebFetch bridge, 3012, or 3017 changes.
- Stable Claude Code `2.1.179` is the production target. `2.1.191` remains forward-compatibility evidence only and does not promote production behavior.
- Production default remains `strip_attribution`; `no_cch` and `signed_cch` require explicit `2.1.179` oracle/profile proof and approved egress profile.

Exact local refs and safe hashes:

Note: the Sub2API plan/evidence/oracle files under `docs/anti-ban/` are local evidence artifacts in this worktree and are git-ignored by the repository policy; their hashes are recorded here for audit.

| Item | Value |
|---|---|
| Sub2API worktree | `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime` |
| Sub2API branch / HEAD | `codex/claude-code-multiprovider-runtime` / `8225866264463e3e60e44ebc7517853c81ca1129` |
| Sub2API implementation diff hash, excluding this evidence report | `d4b4df2d9d4f5711517be7e858c685c083b24ffe51a79e1cd28c0043d28ccfa4` |
| Sub2API changed-file list hash | `ad0dbaeb586a178f2a942f5f8c740c33e10f2ac58676a8b00ddbd367280212db` |
| 2.1.179 safe oracle matrix hash | `0caba2c278cb40ceb6f25018ea5a66311b5b03bf539104b4b1d3c87de4f9c54f` |
| Plan 58 hash after redaction cleanup | `3a2cb0fe11a62aa02f3f975cd0c3a8e4d572f5f6427cff85df63bf8b7a5082d9` |
| CC Gateway worktree | `/Users/muqihang/chelingxi_workspace/cc-gateway` |
| CC Gateway branch / HEAD | `codex/formal-pool-p0-safety` / `c37a2347f425f8e4eec26d7fceb0995a08e0d9ea` |
| CC Gateway worktree diff hash | `1c480c8bc8b04eb51e01eb444cf26c488ca0e30abe682e7dc5295318c6ef7eed` |
| CC Gateway changed-file list hash | `f2cd6786cef25f49303f2a848b652649e7c027d4fbb1ea730ae05bbd87edc4a6` |
| CC Gateway formal-pool example config hash | `ea71fc9cde8e0502e9d360f741c0e2b9baddd6935d559df5007056d6dcbe86de` |
| CC Gateway formal-pool safety doc hash | `48da8ec3d55fdf94c10ce7a3121b6e669a4af9ac490ef86bd79944e32653720f` |
| CC Gateway CCH oracle doc hash | `d7d9bb56f51befdfc2e9b3ff8ba1025f4cca747ec31e375ae0adf8e33693b017` |

Latest 2.1.179 CP5 local test evidence:

Sub2API:

- `cd backend && go test ./internal/service -run 'TestCCGatewayFormalPoolAttestationMatchesSharedContractFixture|TestGatewayService_CCGatewayFormalPoolContextCarriesServerSelected2179ProfileRefs|TestGatewayService_CCGatewayFormalPoolUnknownObservedVersionDoesNotPromoteProfile|TestGatewayService_CCGatewayFormalPoolIgnoresBodyAuthorityHints|TestGatewayService_CCGatewayFormalPoolObservedClientProfileDropsUnknownBodyKeyNames|TestGatewayService_CCGatewayFormalPoolDefaultStripRemovesDownstreamBillingMaterial|TestCCGatewayAnthropicPolicyVersionTracksClaudeCode2179ProductionAnchor|TestCCGatewayPolicyVersionCompatibleAllowsOldCCHCompatibleVersionsOnly|TestGatewayService_CCGatewayAnthropicOAuthIgnoresForgedInternalHeaders|TestGatewayService_CCGatewayAnthropicOAuthBuildsAttestedFormalPoolContextFromServerState|TestFormalPoolRuntimeIdentityExtraDefaultsTo2179NativeDegradedPersona|TestFormalPoolGatewayHealthcheckRunnerSendsAttestedFormalPoolContext|TestFormalPoolGatewayHealthcheckSessionLedgerBindsHealthcheckPersona|TestFormalPoolGatewayHealthcheckRunnerUsesClaudeCodeLiteBodyWithoutOneMillionContext' -count=1` -> `ok`.
- `PYTHONDONTWRITEBYTECODE=1 tools/zhumeng-agent/.venv/bin/python -m pytest tools/tests/test_cli_control_plane_full_chain_controller.py -q -p no:cacheprovider` -> `23 passed`.
- `cd backend/.tmp-harness/cli-through-sub2api && go test . -count=1` -> `ok`.

CC Gateway:

- `npx tsx tests/formal-pool-safety-doc.test.ts` -> `3 passed`.
- `npx tsx tests/config.test.ts` -> `23 passed`.
- `npx tsx tests/session-and-beta-policy.test.ts` -> `14 passed`.
- `npx tsx tests/proxy-sub2api.test.ts` -> `186 passed`.
- `npx tsc --noEmit` -> passed.
- `npm test` -> `204 passed`.

Latest localhost-only full-chain E2E:

- Command: `env -u ANTHROPIC_API_KEY -u ANTHROPIC_AUTH_TOKEN -u ANTHROPIC_BASE_URL -u CLAUDE_CODE_OAUTH_TOKEN -u HTTP_PROXY -u HTTPS_PROXY -u ALL_PROXY -u NO_PROXY TMPDIR=/private/tmp GOCACHE=/private/tmp/sub2api-go-cache tools/zhumeng-agent/.venv/bin/python tools/cli_control_plane_full_chain_controller.py --tmp-root /private/tmp`.
- Safe deliverable: `/private/tmp/full-chain-controller-20260625-231251-795347-99006/safe-deliverable/report.json` and `report.md`.
- Overall: `PASS`; sensitive scan: `PASS`; `real_anthropic_upstream=false`.
- CP4 matrix: `8/8 PASS`, including default strip for CCH-present/no-CCH inbound, forged authority header handling, missing trusted context fail-closed, optional no-CCH harness proof-gate mechanics, optional signed-CCH requiring proof, and CC Gateway unavailable/no direct fallback. The no-CCH harness scenario is not production authorization; production `no_cch` still requires explicit 2.1.179 oracle/profile approval and an approved egress profile.

Sensitive/no-bypass evidence:

- Safe deliverable forbidden-shape scan: `PASS`.
- CC Gateway changed docs/config/tests forbidden-shape scan: `PASS`.
- Sub2API plan/evidence/oracle safe artifacts forbidden-shape scan: `PASS`.
- No direct Anthropic fallback remains locally proven by the full-chain `cc_gateway_unavailable_no_direct_fallback` scenario (`mock_request_count=0`, `real_anthropic_upstream=false`).
- The `missing_trusted_context_fail_closed` report now records `formal_pool_attested=false`, client fail-closed, and `mock_request_count=0`; this fixes the review-noted evidence precision issue where the harness configuration summary had previously been easy to misread as request attestation.

Production readiness gate status:

| Gate | Status | Reason |
|---|---|---|
| 2.1.179 oracle safe matrix | `PASS_WITH_DEGRADED_SCOPE` | Safe matrix exists and was hash-recorded; non-streaming upstream body parity remains degraded as recorded in CP1 evidence. |
| Sub2API trusted context producer | `PASS` | Targeted Go tests prove server-selected 2.1.179 profile refs, forged body/header authority ignored, safe observed profile, and sticky healthcheck tuple behavior. |
| CC Gateway final verifier | `PASS` | Targeted TypeScript tests and `npm test` prove attestation/profile/session/billing gates, 2.1.179 oracle proof requirement, unknown profile fail-closed, and strip default. |
| Localhost full-chain | `PASS` | Latest safe deliverable reports `PASS`, sensitive scan `PASS`, `real_anthropic_upstream=false`, and `missing_trusted_context_fail_closed.authority_boundary.formal_pool_attested=false`. |
| CodeGraph refresh | `PASS` | After review fixes and final evidence update, Sub2API and CC Gateway `codegraph sync . && codegraph status .` report indexes up to date. |
| Deployed CC Gateway image/commit/config/profile equivalence | `BLOCKED_EXTERNAL_EVIDENCE` | Local safe config/profile hashes are recorded; deployed image/config equivalence has not been proven. |
| Rollback safety | `PASS_LOCAL_POLICY` | Documented safe rollback is disable formal-pool egress or force `strip_attribution`; never direct Anthropic fallback or ungated sign-primary. |
| Live formal-pool smoke | `BLOCKED_USER_APPROVAL_AND_EXTERNAL_EVIDENCE` | Requires deployed equivalence proof and explicit user approval; 3017 was not touched. |


Review follow-up:

- GPT-5.5 xhigh CP5 review returned `PASS_WITH_NOTES`, with no Critical findings.
- Important note 1 was fixed: `missing_trusted_context_fail_closed` now reports request attestation as `false` while preserving fail-closed and zero-upstream evidence.
- Important note 2 was clarified in this evidence: the no-CCH full-chain scenario validates harness proof-gate mechanics only and is not production approval for `no_cch`.

Remaining degraded/blocking scopes:

- Live canary / real formal-pool smoke is not approved to start yet.
- Deployed CC Gateway image/commit/config/profile equivalence is not proven.
- 3017 was not rebuilt/restarted or smoke-tested; code/test references to 3017 outside this plan are not live rollout evidence.
- Strict native first-party parity/sign-primary and production `no_cch` for 2.1.179 remain optional/future and gated on explicit oracle/profile proof plus approved egress profile.
- WebSearch/WebFetch bridge, multi-provider hot-switching, DeepSeek/GPT/AGNES/Kimi/GLM, and managed Zhumeng Claude Code runtime are outside plan 58.

## 2026-06-27 Plan 59 Claude Platform on AWS evidence refresh

Status for plan `docs/anti-ban/59-claude-platform-on-aws-formal-pool-integration-plan.md`: `PASS_LOCAL_TARGETED_WITH_PRODUCTION_BLOCKERS` after CP7 review. Local/mock Sub2API and CC Gateway checkpoint evidence is green through the optional SigV4 phase, but production remains blocked by CP0 real auth-profile proof, deployed equivalence, and explicit live-smoke approval.

Tracked final evidence map for plan 59: `docs/anti-ban/59-claude-platform-on-aws-final-evidence-map.md`.

Safe CP7-reviewed implementation refs before this evidence-only update:

| Item | Value |
|---|---|
| Sub2API branch / CP7-reviewed implementation HEAD | `codex/claude-platform-aws-formal-pool` / `60416122c000` |
| CC Gateway CP branch / HEAD | `codex/claude-platform-aws-cp5` / `e6889daac6babde65e52716ffc5acdc8b5ad2314` |
| Endpoint ref / region | `endpoint_ref:aws-external-anthropic-us-east-1` / `us-east-1` |
| CP0 production auth profile | `BLOCKED_AUTH_PROFILE` |

Local safe evidence now recorded for plan 59:

- `claude-platform-aws` is a distinct account type and remains isolated from OAuth, Setup Token, first-party Claude Console API-key, Bedrock, Vertex service-account, and generic upstream paths.
- Sub2API local targeted tests cover safe workspace/credential/endpoint refs, per-workspace account identities, client-spoofing protection, redaction, no-bypass behavior, and the Sub2API -> CC Gateway formal-pool contract.
- CC Gateway local targeted/full tests cover account/credential/workspace binding, workspace binding HMAC, endpoint/region/path/query, egress/proxy, session ledger, provider-scoped beta/request-shape/cache profiles, final verifier ordering, and no post-verifier semantic mutation for the AWS provider path.
- CP6 local full-chain E2E proves local Sub2API -> local CC Gateway -> safe mock AWS upstream routing for two AWS workspace account identities with distinct proxy refs and no direct bypass.
- CP7 optional SigV4 mock/canonical tests pass behind an explicit gate and do not reuse the Bedrock signer. This is not live SigV4 evidence.

Remaining blockers stay explicit:

| Gate | Status | Reason |
|---|---|---|
| Real CP0 auth profile proof | `BLOCKED_AUTH_PROFILE` | No real target workspace/API-key evidence has selected exactly one of `x_api_key` or `bearer_api_key`. |
| Broad Sub2API Go suite | `BLOCKED_HISTORICAL_OR_EXTERNAL` | CP1 audit records historical/external failures unrelated to `fa50af8cfa26`/plan 59; broad green is not claimed. |
| Deployed 3017/CC Gateway equivalence | `BLOCKED_EXTERNAL_EVIDENCE` | No rebuild/restart/deploy/equivalence proof was performed. |
| Tiny live AWS smoke | `BLOCKED_USER_APPROVAL_AND_EXTERNAL_EVIDENCE` | Requires CP0 proof, deployed equivalence, explicit user approval, and tiny cost envelope. |

Sensitive evidence policy for this plan 59 refresh: records contain only commit refs, safe endpoint/profile/workspace refs, statuses, counts, file paths, and omission reasons. No raw workspace ID, API key, Authorization value, `x-api-key` value, raw prompt/body/response, canonical request/string-to-sign output, raw HMAC input/output, cookie, proxy credential, or raw telemetry is recorded.

## Sub2API / CC Gateway responsibility boundary

The intended and currently implemented boundary is:

1. Sub2API / Server API authenticates the end user and selects a server-owned formal-pool account from the shared account pool.
2. Sub2API strips or ignores end-client supplied gateway/account/egress/persona/control headers and regenerates trusted context from scheduler/account state.
3. Sub2API forwards only server-owned safe refs and routing metadata to CC Gateway, including account ref, credential type, egress bucket, policy version, route classification, and session binding.
4. CC Gateway does not act as an open client-selected scheduler. In `sub2api` mode it resolves the selected `x-cc-account-id` against `account_identities`, verifies egress bucket eligibility, rewrites/validates identity/session/persona/billing/CCH fields, and runs final-output verification before upstream egress.

So for a 10-user / 5-account formal pool, the expected mapping is: Sub2API schedules and sticks the 10 users onto the fixed 5 upstream accounts; CC Gateway validates and finalizes the selected account context rather than letting clients choose accounts.

## Workstream status

| Workstream | Status | Evidence | Remaining dependency |
|---|---|---|---|
| A0 Gateway-only native CLI / Server API safety | PASS_WITH_DEGRADED_SCOPE | CP38 complete; GPT-5.5 xhigh micro review PASS; backend boundary tests; forged internal headers ignored/stripped; no direct Anthropic fallback for formal pool; localhost-only full-chain controller PASS (`full-chain-controller-20260624-012117-349394-50773`) | Deployed CC Gateway equivalence and live formal-pool smoke remain external/canary gates |
| A CC Gateway baseline | PASS_WITH_DEGRADED_SCOPE | Read-only audit of `/Users/muqihang/chelingxi_workspace/cc-gateway` main `c37a234`; 2.1.175 persona/CCH materials present; stale 2173 not used as baseline | Confirm deployed runtime is main-equivalent c37a234+ or equivalent backports |
| B Account/session/egress/runtime mapping coherence | PASS_WITH_DEGRADED_SCOPE | CP39 complete; account/session/egress/provider-family sticky ledger tests; spoof protections | Strict native raw UUID/device strategy and durable distributed ledger proof remain degraded/external |
| C Native control-plane parity without raw leakage | PASS_WITH_DEGRADED_SCOPE | CP40 complete after rereview; control-plane policy/intent tests; count_tokens/event/MCP/settings/policy unknown routes stub/block/degrade | Full first-party control-plane parity not claimed; WebSearch/WebFetch bridge is future work |
| D Runtime capability parity | PASS_WITH_DEGRADED_SCOPE | CP41 complete; ToolSearch/FGTS/request-id presence-only audits; managed proof artifacts; effort patch tests | Real interactive `/model` proof and deferred-tool/live fixture evidence remain canary/manual |
| E Bridge tool/cache/hot-switch/subagent parity | PASS_WITH_DEGRADED_SCOPE | CP42 complete after required fixes; cache audit fields, hot-switch turn buckets, DeepSeek reason buckets, OpenAI/AGNES safe error audit | Live mixed-provider Agent resolver and high-hit-rate cache evidence remain L8/canary |
| F 2.1.177 persona/CCH/profile evidence | PASS_WITH_DEGRADED_SCOPE | CP43 complete; Nash GPT-5.5 xhigh second-final rereview PASS_WITH_NOTES; Python evidence gate 11 passed; Go policy-version guard clean | 2.1.177 strict native parity/sign-primary blocked until oracle/profile/compat proof |

## Section 10 execution gates

| Gate | Status | Evidence | Remaining dependency |
|---|---|---|---|
| 1 Gateway-only safety | PASS_WITH_DEGRADED_SCOPE | CP38; forged header strip/ignore; no-bypass tests; localhost-only full-chain controller PASS (`scenario_a=PASS`, `scenario_b=PASS`, sensitive scan PASS) | Deployed CC Gateway equivalence and live formal-pool smoke remain external/canary gates |
| 2 Baseline | PASS_WITH_DEGRADED_SCOPE | CC Gateway main `c37a234` read-only audit | Deployed CC Gateway baseline confirmation |
| 3 Minimal control-plane fail-closed | PASS_WITH_DEGRADED_SCOPE | CP40; root policy/intent subset passed; localhost full-chain observed control-plane stub/suppress counts with sensitive scan PASS | Full first-party control-plane parity still not claimed |
| 4 Formal-pool coherence | PASS_WITH_DEGRADED_SCOPE | CP39; sticky account/session/egress tests | Strict device/account UUID mimicry unresolved/degraded |
| 5 No-bypass/native safety | PASS_WITH_DEGRADED_SCOPE | CP40-42 replay/guard tests; Go non-socket targeted equivalents; localhost full-chain reached CC Gateway mock only and real Anthropic upstream was false | Live/deployed no-bypass evidence still required before formal-pool production smoke |
| 6 Runtime UX | PASS_WITH_DEGRADED_SCOPE | CP42 static/runtime patch tests for exact effort levels | Real interactive `/model` verification after 3017 rebuild |
| 7 Cache evidence | PASS_WITH_DEGRADED_SCOPE | CP42 diagnostics and reason buckets | Live 95-99% cache evidence for DeepSeek/GPT |
| 8 Tool capability | PASS_WITH_DEGRADED_SCOPE | CP41 ToolSearch/FGTS; CP42 subagent boundary tests | WebSearch/WebFetch bridge and live Agent resolver proof |
| 9 2.1.177 gate | PASS_WITH_DEGRADED_SCOPE | CP43 final rereview PASS_WITH_NOTES; unsafe 2.1.177 policy-version forwarding blocked | 2.1.177 oracle/profile before sign-primary |
| 10 L8 smoke | BLOCKED_EXTERNAL_EVIDENCE | Localhost-only full-chain preflight PASS after explicit canary routing fix; 3017 not rebuilt for latest changes | Requires 3017 rebuild/restart decision, health check, and user/live smoke |

## Section 12 L8 scenarios

| Scenario | Status | Evidence | Remaining dependency |
|---|---|---|---|
| 1 `/model` effort UX | PASS_WITH_DEGRADED_SCOPE | CP42 tests/static patch evidence | Real interactive process verification: DeepSeek High/Max only; GPT Low/Medium/High/XHigh only; no Ultra/Max mismatch |
| 2 Same-provider DeepSeek cache warmup | PASS_WITH_DEGRADED_SCOPE | CP42 DeepSeek cache audit diagnostics | Live stable-prefix warmup with non-zero cache read or zero reason buckets |
| 3 Hot-switch cache continuity | PASS_WITH_DEGRADED_SCOPE | CP42 hot-switch turn bucket tests | Live hot-switch cache evidence |
| 4 AGNES/GPT/DeepSeek tool loop | PASS_WITH_DEGRADED_SCOPE | CP42 safe bridge errors and existing bridge tests | Live AGNES/GPT/DeepSeek tool-loop smoke |
| 5 Mixed-provider subagents | BLOCKED_EXTERNAL_EVIDENCE | Static resolver/boundary evidence only | Managed runtime Agent resolver proof and live canary |
| 6 Claude native formal-pool safety | PASS_WITH_DEGRADED_SCOPE | Replay-safety and gateway boundary tests | Formal-pool live smoke only after deployed baseline/gates are confirmed |
| 7 WebSearch/WebFetch | PASS_WITH_DEGRADED_SCOPE | CP40/41 explicit degraded/auth-required posture | Dedicated local bridge patch not implemented |

## Files changed

Primary changed areas in this worktree:

- Backend gateway/Claude Code safety and bridge services under `backend/internal/service/` and `backend/internal/handler/`.
- Control-plane guard/policy/intent scripts and tests under `tools/`.
- Zhumeng Claude Code launcher/runtime/model overlay/evidence gate and tests under `tools/zhumeng-agent/`.
- New formal-pool coherence code/tests: `backend/internal/service/claude_code_formal_pool_coherence_cp39.go` and `backend/internal/service/claude_code_formal_pool_coherence_cp39_test.go`.
- Progress/review artifacts under `.superpowers/sdd/`.

CC Gateway CP4 code/docs changes were made only in `/Users/muqihang/chelingxi_workspace/cc-gateway`; the excluded 2173 worktree was not used.

## Tests and safe verification summary

Sub2API / zhumeng-agent:

- CP43 evidence gate: `11 passed`.
- CP43 Go policy-version guard: `ok`.
- Root pure control-plane policy/intent subset: `19 passed, 49 subtests passed`.
- Sub2API Python focused command as written: `133 passed, 8 failed`, where all 8 failures were local loopback bind `PermissionError` in the sandbox. Equivalent non-socket subset: `133 passed, 8 deselected`.
- Sub2API Go broad focused command as written hit first `httptest.NewServer` local bind `EPERM`; non-socket targeted equivalent passed.
- `git diff --check`: passed.
- `codegraph index`: refreshed, indexed 2,682 files.

Localhost-only full-chain controller verification:

- User-run normal Terminal command: `tools/zhumeng-agent/.venv/bin/python tools/cli_control_plane_full_chain_controller.py --tmp-root /private/tmp`.
- Safe deliverable: `/private/tmp/full-chain-controller-20260624-012117-349394-50773/safe-deliverable/report.json` and `report.md`.
- Overall: `PASS`; sensitive scan: `PASS`; real Anthropic upstream: `false`.
- Scenario A: `PASS`, mock upstream request count `0`, unsafe over-budget messages blocked before Sub2API/CC Gateway.
- Scenario B: `PASS`, Sub2API selected count `1`, mock upstream request count `1`, client observed `200`; CC Gateway final mock request was `POST /v1/messages?beta=true` through localhost proxy only.
- The run validates the localhost-only gateway-only/native safety path and the explicit canary routing fix; it does not validate live 3017, production CC Gateway deployment equivalence, or strict 2.1.177 native parity.

CC Gateway read-only verification (historical; superseded by CP5):

- Earlier sandbox limitations prevented full socket-bound `npm test -- ...` and `npm run build` from completing. CP5 superseded this: the full targeted CC Gateway command reported `186 passed, 0 failed`, and `npm run build` passed locally in `/Users/muqihang/chelingxi_workspace/cc-gateway`.

## Canary / deployment status

- 3012 was not touched.
- 3017 was not rebuilt/restarted during CP38-CP43 and formal-pool CP2-CP5 latest implementation.
- Therefore 3017 does not yet constitute evidence for the latest CP38-CP43 or formal-pool CP2-CP5 code.

Before L8 live smoke, rebuild/restart only 3017 after confirming the desired deployment path, then record safe health only:

```bash
curl -sS http://127.0.0.1:3017/health
```

## Remaining risks and exact dependencies

| Gap | Risk if ignored | Smallest dependency/action |
|---|---|---|
| Deployed CC Gateway baseline not proven main-equivalent | Formal-pool traffic could miss final-output or persona/CCH gates | Confirm deployed CC Gateway commit/config equals `c37a234`+ or equivalent backports; run socket-bound focused tests in an environment that permits local listeners |
| 2.1.177 oracle/profile absent | Strict native mimicry/sign-primary could diverge from real 2.1.177 | Capture safe localhost-only 2.1.177 request-shape oracle/profile and add compatibility proof before sign-primary |
| 3017 not rebuilt for CP38-CP43 / formal-pool CP2-CP5 latest code | User L8 would test stale canary | Rebuild/restart only 3017, then health check and L8 smoke |
| WebSearch/WebFetch degraded | Tool parity not complete | Dedicated bridge/shim patch or explicit product decision to keep degraded/auth-required |
| Cache 95-99% not proven live | Cost target not proven | Live stable-prefix L8 evidence with safe cache counters/reason buckets |
| Mixed-provider Agent resolver live proof missing | Subagent parity not complete | Managed runtime Agent resolver canary test with safe summaries only |

## Claim safety table

| Claim | Safe now? | Status |
|---|---:|---|
| Final-output safety boundary | Yes | Safe for native Claude messages path in safe/degraded scope |
| Gateway-only native CLI safety | Yes, degraded | Does not depend solely on local takeover; deployed CC Gateway proof still needed |
| Bridge hot-switch safety | Yes, degraded | Replay-safety and safe audit diagnostics implemented; live mixed-provider proof pending |
| Cache attribution | Yes, diagnostic | Counters are not invented; high hit-rate still external evidence |
| ToolSearch/MCP/WebSearch behavior | Partial | ToolSearch/FGTS guarded; WebSearch/WebFetch degraded/future bridge |
| 2.1.177 strict native mimicry | No | Blocked on oracle/profile/compat proof |
| Sign-primary readiness | No | Must remain disabled/fail-closed until 2.1.177 oracle/profile/CCH gates are green |

## Embedded evidence map

# 53 Plan Final Evidence Map (draft)

Status: SUPERSEDED_HISTORICAL_DRAFT. The authoritative CP5 evidence is recorded above and in `.superpowers/sdd/final-evidence-map.md`; this embedded section is retained only as historical context.

## Workstream status draft

| Area | Current status | Evidence | Remaining gate |
|---|---|---|---|
| Workstream A0 gateway-only native CLI/server API safety | PASS_WITH_DEGRADED_SCOPE | CP38 ledger/review; CP5 CC Gateway targeted tests/build and localhost-only full-chain E2E PASS | Deployed CC Gateway equivalence and live formal-pool smoke remain external/canary gates |
| Workstream A CC Gateway baseline | PASS_WITH_DEGRADED_SCOPE | Read-only CC Gateway main branch c37a234; docs/tests present for 2.1.175; stale 2173 not used as baseline | Deployed production/canary CC Gateway equivalence to c37a234+ remains external evidence |
| Workstream B account/session/egress/runtime mapping coherence | PASS_WITH_DEGRADED_SCOPE | CP39 ledger/review; Sub2API coherence tests | Per-account device/native UUID strict mimicry remains degraded unless CC Gateway/account storage proof is added |
| Workstream C native control-plane parity | PASS_WITH_DEGRADED_SCOPE | CP40 ledger/review; policy/guard tests | Full native control-plane parity not claimed; WebSearch/WebFetch local bridge remains separate future patch/degraded |
| Workstream D runtime capability parity | PASS_WITH_DEGRADED_SCOPE | CP41 and earlier effort patch tests; managed proof artifacts; ToolSearch healthcheck | Real interactive /model screenshot/probe and live Agent resolver proof are canary evidence gates |
| Workstream E bridge tool/cache/hot-switch/subagent parity | PASS_WITH_DEGRADED_SCOPE | CP42 rereview PASS_WITH_NOTES; focused cache/hot-switch/error tests | Live mixed-provider Agent and high hit-rate cache are canary/evidence gates, not strict pass |
| Workstream F 2.1.177 persona/CCH/profile evidence | PASS_WITH_DEGRADED_SCOPE | CP43 task report; cp43-second-final-rereview-verdict PASS_WITH_NOTES; Python evidence gate 11 passed; Go 2.1.177 policy-version guard clean; CC Gateway read-only audit found no 2.1.177 proof | 2.1.177 sign-primary/strict native parity remain blocked until oracle/profile/compat proof |

## Section 10 execution gate draft

| Gate | Current status | Evidence | Remaining gate |
|---|---|---|---|
| 1 Gateway-only safety | PASS_WITH_DEGRADED_SCOPE | CP38 | Deployed CC Gateway no-bypass evidence |
| 2 Baseline | PASS_WITH_DEGRADED_SCOPE | CC Gateway main c37a234 read-only | Runtime/deployed baseline confirmation |
| 3 Minimal control-plane fail-closed | PASS_WITH_DEGRADED_SCOPE | CP40 | Full parity intentionally not claimed |
| 4 Formal-pool coherence | PASS_WITH_DEGRADED_SCOPE | CP39 | Native UUID/device strategy strict mimicry unresolved |
| 5 No-bypass/native safety | PASS_WITH_DEGRADED_SCOPE | replay/guard tests from CP40-42; CP5 CC Gateway targeted command and localhost full-chain passed with `real_anthropic_upstream=false` | Live/deployed no-bypass evidence still pending |
| 6 Runtime UX | PASS_WITH_DEGRADED_SCOPE | CP42 static/runtime patch tests | Real interactive /model UI still canary/manual evidence |
| 7 Cache evidence | PASS_WITH_DEGRADED_SCOPE | CP42 cache audit diagnostics | 95-99% hit rate not proven; live evidence required |
| 8 Tool capability | PASS_WITH_DEGRADED_SCOPE | ToolSearch/FGTS evidence from CP41; subagent static tests | WebSearch/WebFetch bridge and live Agent resolver gate remain |
| 9 2.1.177 gate | PASS_WITH_DEGRADED_SCOPE | CP43 second-final rereview PASS_WITH_NOTES; evidence gate rejects unsafe sensitive/provenance inputs; 2.1.177 policy version is not forwarded as verified | Strict sign-primary/native parity blocked until 2.1.177 oracle/profile proof |
| 10 L8 smoke | BLOCKED_EXTERNAL_EVIDENCE | CP5 localhost-only full-chain E2E passed; 3017 not rebuilt/restarted for CP5 latest changes | Requires canary rebuild/restart decision and live/manual evidence |

## Section 12 L8 scenario draft

| Scenario | Current status | Evidence | Remaining gate |
|---|---|---|---|
| 1 /model effort UX | PASS_WITH_DEGRADED_SCOPE | CP42 tests/static patch evidence | Real interactive process verification |
| 2 Same-provider DeepSeek cache warmup | PASS_WITH_DEGRADED_SCOPE | CP42 diagnostics | Live warmup hit-rate evidence |
| 3 Hot-switch cache continuity | PASS_WITH_DEGRADED_SCOPE | CP42 hot-switch turn bucket tests | Live hot-switch evidence |
| 4 AGNES/GPT/DeepSeek tool loop | PASS_WITH_DEGRADED_SCOPE | CP42 error/audit + existing bridge tests | Live AGNES/GPT tool loop evidence |
| 5 Mixed-provider subagents | BLOCKED_EXTERNAL_EVIDENCE | Static resolver/boundary only | Managed runtime Agent resolver proof/canary |
| 6 Claude native formal-pool safety | PASS_WITH_DEGRADED_SCOPE | replay-safety tests and gateway boundary tests | Full formal-pool live smoke only after gates |
| 7 WebSearch/WebFetch | PASS_WITH_DEGRADED_SCOPE | CP40/41 degraded/auth-required treatment | Dedicated local bridge patch not implemented |

## Notes

- 3012 untouched.
- 3017 not restarted during CP38-CP43 and formal-pool CP2-CP5 implementation after latest changes.
- No strict 2.1.177 native mimicry or sign-primary readiness claim is safe at this draft stage.
- CP43 second-final xhigh review completed: `PASS_WITH_NOTES`; Workstream F is safe-degraded, not strict native parity.


## Verification run notes (2026-06-24)

Section 11 current local verification:

- Sub2API Python focused command as written: `133 passed, 8 failed` where all 8 failures are `PermissionError: [Errno 1] Operation not permitted` on local `127.0.0.1` test server bind in `tests/test_claude_code_launcher.py`.
- Sub2API Python non-socket equivalent subset excluding those 8 loopback-bind tests: `133 passed, 8 deselected`.
- Sub2API Go focused command as written: failed at first `httptest.NewServer` with loopback bind permission in `TestClaudeCodeBridgeAnthropicLivePostsRawBodyAndPassesThroughSSE`; recorded as sandbox-degraded.
- Sub2API Go non-socket targeted equivalent for cache/hot-switch/replay/policy-version: `ok github.com/Wei-Shaw/sub2api/internal/service`.
- Root control-plane command with system `python3`: unavailable because system Python lacks pytest.
- Root control-plane command with project venv: `49 passed, 59 subtests passed, 51 failed` where failures are local socket bind `PermissionError: [Errno 1] Operation not permitted` in guard tests; recorded as sandbox-degraded.
- Root pure policy/intent subset: `19 passed, 49 subtests passed`.
- CP43 evidence gate focused tests after second required-fixes round: `11 passed` and `11 passed` with `-p no:cacheprovider`.
- CP43 Go policy-version guard tests: `ok`.
- `git diff --check`: passed.
- `codegraph index`: refreshed, indexed 2,682 files after latest CP43 changes.

## Verification run notes (2026-06-24 CP43 post-review refresh)

- CP43 evidence gate focused test: `PYTHONDONTWRITEBYTECODE=1 .venv/bin/python -m pytest tests/test_claude_code_evidence_gate.py -q -p no:cacheprovider` -> `11 passed`.
- CP43 Go policy-version guard: `GOCACHE=/private/tmp/sub2api-go-cache go test ./internal/service -run 'TestGatewayService_CCGatewayAnthropicOAuth(ExplicitCanaryRejectsUnverified2177PolicyVersion|VerifiedLegacyDriftPolicyVersionPasses|VerifiedLegacyDriftWithoutTrustedContextFallsBackToAnchoredPolicyVersion)$|TestCCGatewayPolicyVersionCompatibleAllowsOldCCHCompatibleVersionsOnly' -count=1` -> `ok`.
- Root pure control-plane policy/intent subset: `tools/zhumeng-agent/.venv/bin/python -m pytest tools/tests/test_cli_control_plane_policy.py tools/tests/test_cli_control_plane_intent.py -q` -> `19 passed, 49 subtests passed`.
- `git diff --check` -> passed.
- `codegraph index` -> refreshed, indexed 2,682 files.

## Localhost full-chain verification notes (2026-06-24)

- Latest user-run full-chain controller: `/private/tmp/full-chain-controller-20260624-012117-349394-50773/safe-deliverable/report.json`.
- Overall status: `PASS`; sensitive scan: `PASS`; `real_anthropic_upstream=false`.
- Scenario A: `PASS`; mock request count `0`; cost envelope block `true`; Sub2API selected count `0`.
- Scenario B: `PASS`; mock request count `1`; Sub2API selected count `1`; client response status `200`; CC Gateway mock request URL `/v1/messages?beta=true`.
- Safe CC summary shows localhost proxy target only; no raw prompt/body/API key is documented here.
- This evidence closes the previous localhost full-chain rerun blocker caused by explicit canary canary-only broad-routing rejection. It does not close the live 3017 rebuild/L8/manual smoke blocker.

## CC Gateway verification notes (2026-06-24; superseded by CP5)

- Historical sandbox note only: an earlier environment could not run the full socket-bound Section 11 command or write `dist/*`. CP5 superseded this with the full targeted `npm test -- ...` command reporting `186 passed, 0 failed`, and `npm run build` passing locally in `/Users/muqihang/chelingxi_workspace/cc-gateway`.
