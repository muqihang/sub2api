# 59 - Claude Platform on AWS Final Evidence Map

Date: 2026-06-28

Scope: plan 59 local/mock/prodlike/live-canary evidence for `type = "claude-platform-aws"` as a third independent formal-pool account type. This map records only safe refs, commit IDs, test counts, status buckets, booleans, and non-claims. It does not authorize production traffic. CP9 adds server-side mock/simulated full-chain smoke evidence without touching 3012 or deploying/restarting 3017. CP10 records a production-like deployed mock full-chain smoke for the `839c73f` Sub2API fix and `e6889da` CC Gateway build. CP11 records a user-approved tiny live AWS canary that reached AWS and was blocked by upstream account/organization state, so production remains blocked.

## CP7-reviewed implementation refs before this evidence-only update

| Item | Value |
|---|---|
| Sub2API worktree | `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool` |
| Sub2API branch / CP7-reviewed implementation HEAD | `codex/claude-platform-aws-formal-pool` / `60416122c000` |
| CC Gateway CP worktree | `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5` |
| CC Gateway branch / HEAD | `codex/claude-platform-aws-cp5` / `e6889daac6babde65e52716ffc5acdc8b5ad2314` |
| Target endpoint ref | `endpoint_ref:aws-external-anthropic-us-east-1` |
| Target region | `us-east-1` |
| Phase 1 request shape | `/v1/messages`, empty query |
| Production auth profile | `BLOCKED_UPSTREAM_ACCOUNT_STATE`; `x_api_key` reached the AWS account-state layer but no `status_2xx` proof exists; `bearer_api_key` remains unproven |
| Deployed/live status | `PRODLIKE_MOCK_PASS_LIVE_CANARY_REACHED_AWS_BLOCKED_UPSTREAM_ACCOUNT_STATE`; no 3017 restart/deploy, no 3012 change, no production traffic |
| Tracking note | This file was force-added in CP8 because of the broad `docs/anti-ban/*` ignore rule and is now tracked. |

## CP10 prodlike deployed mock full-chain smoke evidence

Status: `PASS_PRODLIKE_MOCK_FULL_CHAIN`.

This section records the latest deployed production-like mock smoke only. It is still not live AWS evidence and does not production-enable AWS Platform formal-pool traffic.

| Item | Safe value |
|---|---|
| Server stack | `/opt/claude-platform-aws-prodlike-63e178ef-e6889da` |
| Sub2API deployed HEAD | `839c73f0d5b10ce873210f92bf679630e789615d` |
| CC Gateway deployed HEAD | `e6889daac6babde65e52716ffc5acdc8b5ad2314` |
| Deployment source marker | `specified-worktrees-git-archive` |
| Link path | `client -> Sub2API -> CC Gateway -> connect-proxy -> mock AWS` |
| Public prodlike entry | `127.0.0.1:19080` on the isolated stack only |
| CC Gateway admin/gateway port | `127.0.0.1:19081` on the isolated stack only |
| Mock AWS port | `127.0.0.1:19082` on the isolated stack only |
| Report files | `/opt/claude-platform-aws-prodlike-63e178ef-e6889da/reports/deployed-source.txt`, `/opt/claude-platform-aws-prodlike-63e178ef-e6889da/reports/mock-aws-requests.ndjson`, `/opt/claude-platform-aws-prodlike-63e178ef-e6889da/reports/connect-proxy.ndjson` |
| 3012 / 3017 | `no_3012_change=true`; `no_3017_restart_or_deploy=true` |

Positive smoke safe upstream evidence:

| Field | Safe observed value |
|---|---|
| HTTP status bucket | `status_2xx` |
| Final path | `/v1/messages` |
| Final host header | `aws-external-anthropic.us-east-1.api.aws` |
| `workspace_header_present` | `true` |
| `x_api_key_present` | `true` |
| `authorization_present` | `false` |
| `internal_header_present` | `false` |
| `anthropic_beta_present` | `false` |
| `billing_header_present` | `false` |
| `raw_query_empty` | `true` |
| `body_shape_has_messages` | `true` |
| `safe_evidence_only` | `true` |

Negative and spoofing smoke evidence:

| Scenario | Safe outcome |
|---|---|
| Wrong client API key | `401`, no mock AWS hit, no connect-proxy hit |
| Query variant | `404`, no mock AWS hit, no connect-proxy hit |
| Direct CC Gateway without gateway token | `401 missing_gateway_token`, no mock AWS hit, no connect-proxy hit |
| Direct CC Gateway with gateway token but missing context/signature | `403 missing_formal_pool_context_attestation`, no mock AWS hit, no connect-proxy hit |
| Direct CC Gateway with gateway token and forged bad attestation | `403 malformed_formal_pool_context_attestation`, no mock AWS hit, no connect-proxy hit |
| Forged client authority headers | Final mock upstream still used server-selected workspace/auth only; forged `anthropic-workspace-id`, `x-api-key`, `x-cc-*`, `x-sub2api-*`, billing/CCH, and beta headers were stripped or ignored |

Leak and DTO scan summary:

| Surface | Safe result |
|---|---|
| Ordinary reports/evidence | `PASS`, no raw workspace ID, raw API key, Authorization/x-api-key value, raw prompt/body/response, raw HMAC input/output, cookie, or proxy credential detected |
| Recent service logs | `PASS`, no raw workspace ID, raw API key, Authorization/x-api-key value, raw prompt/body/response, raw HMAC input/output, cookie, or proxy credential detected |
| CC Gateway session ledger | `PASS`, no raw workspace ID or auth header pattern detected |
| Sub2API admin account DTO | `PASS`, no raw workspace ID/API key/auth secret detected |
| Sensitive runtime/config storage exception | `.env`, `config/cc-gateway.yaml`, and `runtime/cc-gateway/runtime-mapping.json` may contain raw runtime credential/workspace material by design; these are not ordinary evidence/DTO/log artifacts and must not be copied into evidence or commits |

CP10 root-cause closure:

- A prior prodlike smoke exposed an actual Sub2API no-bypass regression: `claude-platform-aws` formal-pool traffic built a CC Gateway URL but still carried the account proxy around CC Gateway.
- Fix commit `839c73f0d5b10ce873210f92bf679630e789615d` sets the CPAWS formal-pool path to `useCCGateway=true` and selects proxy/TLS fingerprinting from `!useCCGateway`.
- The post-fix prodlike smoke shows Sub2API forwarding to CC Gateway with empty account proxy and CC Gateway emitting the final AWS Platform-shaped request.


## CP11 tiny live AWS canary evidence

Status: `REACHED_AWS_BLOCKED_UPSTREAM_ACCOUNT_STATE`.

This section records the user-approved live canary only as safe evidence. It does not production-enable AWS Platform formal-pool traffic because the canary did not return `status_2xx`.

| Item | Safe value |
|---|---|
| Server stack | `/opt/claude-platform-aws-prodlike-63e178ef-e6889da` |
| Sub2API deployed HEAD | `839c73f0d5b10ce873210f92bf679630e789615d` |
| Local evidence HEAD before CP11 doc update | `c0887104958c0bc2b65464774d96f904f679aecd` |
| CC Gateway deployed HEAD | `e6889daac6babde65e52716ffc5acdc8b5ad2314` |
| Link path | `client -> Sub2API -> CC Gateway -> SOCKS5 egress -> AWS Claude Platform endpoint` |
| Target endpoint ref | `endpoint_ref:aws-external-anthropic-us-east-1` |
| Region | `us-east-1` |
| Request path | `/v1/messages` |
| Model used | `claude-sonnet-4-6` |
| Auth profile attempted | `x_api_key` only |
| Bearer fallback | `not_attempted` |
| SigV4 fallback | `not_attempted` |
| CCH / attribution policy | `strip_attribution=true`; `billing_shape_policy=strip`; `no_cch=false`; `signed_cch=false` |
| Safe result file | `/opt/claude-platform-aws-prodlike-63e178ef-e6889da/reports/live-canary-result-safe.json` |
| Safe refs file | `/opt/claude-platform-aws-prodlike-63e178ef-e6889da/reports/live-canary-safe-refs.json` |

Live canary safe outcome:

| Field | Safe observed value |
|---|---|
| Full chain reached AWS | `true` |
| Upstream status bucket | `status_4xx` |
| Upstream message bucket | `organization_disabled` |
| Sub2API surfaced status bucket | `status_5xx_gateway_mapping` |
| AWS account/auth layer reached by `x_api_key` | `true` |
| `x_api_key` production profile proof | `not_2xx_proven` |
| `bearer_api_key` production profile proof | `unproven` |
| Direct production | `BLOCKED_UPSTREAM_ACCOUNT_STATE` |

Header/model policy confirmed during CP11:

- Official Claude Platform on AWS docs checked on 2026-06-28 list `claude-sonnet-4-6` and describe Claude Platform on AWS requests as the Claude API surface with a regional base URL and a required `anthropic-workspace-id` header.
- The live canary request used `claude-sonnet-4-6`, not `claude-sonnet-4-5`.
- The `anthropic-workspace-id` header is required by AWS but must be injected by CC Gateway from sensitive runtime config after Sub2API trusted-context selection. Client-supplied workspace headers remain spoof/audit input and must be stripped or ignored.

Leak and temporary-artifact summary:

| Surface | Safe result |
|---|---|
| Ordinary reports/evidence | `PASS`, safe refs/status buckets/booleans only |
| Recent service logs | `PASS_PATTERN_SCAN`, no raw workspace ID/API key/Auth/raw prompt/body/response/HMAC/proxy credential detected outside sensitive runtime/config storage |
| Temporary raw canary files | `TRUNCATED_OR_REMOVED` |
| Sensitive runtime/config storage exception | Stack `.env`, `config/cc-gateway.yaml`, and runtime backup/config files may contain raw workspace/API key/proxy material by design and must not be copied into evidence or commits |
| Mitigated incident | One temporary seed-output file briefly contained sensitive marker material; it was not printed to chat and was truncated to zero bytes. |

CP11 root-cause conclusion:

- The live failure is not evidence of a Sub2API/CC Gateway integration or 58/59 boundary failure: the request traversed `client -> Sub2API -> CC Gateway -> egress -> AWS`, AWS was reached, and the safe error bucket points to upstream account/organization state.
- No silent fallback was attempted. `bearer_api_key`, SigV4 production, `no_cch`, and `signed_cch` remain disabled/fail-closed.
- Production remains blocked until a valid AWS account/workspace returns a `status_2xx` canary for exactly one auth profile, followed by deployed-equivalence and production enablement review.

## Checkpoint evidence status

| Gate | Status | Safe evidence | Remaining dependency |
|---|---|---|---|
| CP0 baseline/formal-pool substrate | `PASS_LOCAL_BASELINE`, production `BLOCKED_AUTH_PROFILE` | Doc 58 trusted context, strip-attribution default, no direct fallback, session ledger, and final-verifier fields were verified in targeted tests and records. | Real CP0 target endpoint/workspace/API-key proof must prove exactly one of `x_api_key` or `bearer_api_key`. |
| CP1 account model/validation | `PASS_LOCAL_TARGETED` | `claude-platform-aws` is isolated from OAuth/setup-token/API-key/Bedrock/Vertex/generic upstream paths; safe refs and per-workspace account identity are enforced. | Broad Go suite remains blocked by historical/external failures recorded in the CP1 audit. |
| CP2 UI/import UX | `PASS_LOCAL_TARGETED` | Dedicated Claude Platform on AWS card and multi-workspace batch payload tests passed; Bedrock/OAuth/setup-token cards remain independent. | None for local UI scope. |
| CP3 Sub2API direct/mock builder | `PASS_LOCAL_TARGETED` | Dedicated builder derives AWS endpoint/path, preserves native model names, strips client workspace/auth/beta authority, and fails closed without CP0 evidence. | Direct path is not formal-pool production evidence. |
| CP4 Sub2API -> CC Gateway contract | `PASS_LOCAL_TARGETED_AND_REVIEWED` | Attested context contains provider/workspace/endpoint/auth/profile tuple fields from server scheduler/account state only; sticky tuple mismatch fails closed. | Production remains blocked by CP0/deployed/live gates. |
| CP5 CC Gateway verifier/final injection | `PASS_LOCAL_TARGETED_AND_BROAD_GREEN_REVIEWED` | CC Gateway final verifier, ledger, provider-aware rewrite, upstream-safety, runtime schema/replay, and no-bypass tests passed in the CP worktree. | CC Gateway CP worktree has no CodeGraph index; deployed image/config equivalence is not proven. |
| CP6 local full-chain E2E | `PASS_LOCAL_TARGETED_REVIEW_FIXED` | Sub2API -> local CC Gateway -> safe mock AWS upstream proved two AWS workspace accounts, distinct proxy refs, server-owned workspace/auth, empty query, no internal header leak, and no direct bypass. | Local mock only; no 3017/live AWS/canary traffic. |
| CP7 optional SigV4 | `PASS_MOCK_CANONICAL_REVIEWED`, production disabled unless explicitly gated | CC Gateway SigV4 signer uses service `aws-external-anthropic`, endpoint region, server-owned workspace, final body hash, and optional session token after final rewrite and before final verifier. | Mock/canonical only; no live SigV4 proof. API-key Phase 1 remains separate and CP0-blocked for production. |
| CP9 server-side simulated full-chain smoke | `PASS_SERVER_MOCK_SIMULATED_FULL_CHAIN` | Isolated server runner archived Sub2API `2fdfb945268bcb8ab2f08c6288869afd035b9e16` and CC Gateway `e6889daac6babde65e52716ffc5acdc8b5ad2314`; CP7 SigV4 `3 passed`, CP5 AWS `17 passed`, preflight `8 passed`, build passed, focused CP6 E2E passed, AWS regression slice passed, config authority slice passed, and report scans passed. Report root: `/opt/claude-platform-aws-smoke-2fdfb945-e6889da/reports/aws-platform-smoke-20260628T013618Z-1`. | Server mock only: `real_aws_upstream=false`, CP0 production auth profile still `BLOCKED_AUTH_PROFILE`, no 3017 deploy/restart, no 3012 change, no live AWS/canary traffic. |
| CP10 prodlike deployed mock full-chain smoke | `PASS_PRODLIKE_MOCK_FULL_CHAIN` | Deployed stack `/opt/claude-platform-aws-prodlike-63e178ef-e6889da` ran Sub2API `839c73f0d5b10ce873210f92bf679630e789615d` and CC Gateway `e6889daac6babde65e52716ffc5acdc8b5ad2314`; positive safe evidence reached mock AWS through `client -> Sub2API -> CC Gateway -> connect-proxy -> mock AWS`; negative no-bypass/direct-CCG/spoofing/leak/DTO scans passed. | Live canary still required before production. |
| CP11 tiny live AWS canary | `REACHED_AWS_BLOCKED_UPSTREAM_ACCOUNT_STATE` | User-approved one-request live canary used `claude-sonnet-4-6` and traversed `client -> Sub2API -> CC Gateway -> SOCKS5 egress -> AWS`; AWS returned `status_4xx` with safe message bucket `organization_disabled`; no bearer/SigV4/CCH fallback was attempted. | Valid AWS account/workspace must return `status_2xx` before CP0 can mark one auth profile production-proven. |

## Production-readiness map

| Production gate | Status | Reason |
|---|---|---|
| CP0 auth profile selection | `BLOCKED_UPSTREAM_ACCOUNT_STATE`, production blocked | Live canary reached AWS with `x_api_key` but received `status_4xx` / `organization_disabled`, so `x_api_key` is not `status_2xx` production-proven. `bearer_api_key` remains unproven and was not attempted; silent fallback is forbidden. |
| Workspace/region/endpoint proof | `REACHED_AWS_WITH_SAFE_REFS`, production blocked | Local, server mock, prodlike mock, and live canary evidence bind safe workspace refs to `us-east-1` and the AWS Platform endpoint shape; no raw workspace ID is recorded here. |
| Sub2API targeted tests | `PASS_LOCAL_TARGETED` | CP1-CP6 targeted commands in the plan record passed. |
| CC Gateway targeted/full tests | `PASS_LOCAL_TARGETED_AND_BROAD_GREEN` | CP7 record reports CP7 SigV4 `3 passed`, CP5 AWS `17 passed`, preflight `8 passed`, build passed, full CC Gateway suite `225 passed`. |
| Broad Sub2API Go suite | `BLOCKED_HISTORICAL_OR_EXTERNAL` | CP1 audit records historical test drift and one external network/module-cache blocker unrelated to `fa50af8cfa26`/59 work. Do not claim broad Sub2API green. |
| Safe artifact/leak scan | `PASS_LOCAL_SERVER_PRODLIKE_AND_LIVE_PATTERN_SCAN` | Records, changed-file scans, server smoke report scan, host-side precise scan, prodlike/live reports/logs/session-ledger/DTO scan report no raw workspace ID, API key, Authorization value, raw prompt/body/response, raw HMAC input/output, cookie, proxy credential, or raw telemetry outside explicitly sensitive runtime/config storage; temporary raw canary artifacts were truncated or removed. |
| CodeGraph refresh | `PASS_SUB2API`; `NOT_AVAILABLE_CCGATEWAY_CP_WORKTREE` | Sub2API `.codegraph/` has been incrementally synced after checkpoint records. CC Gateway CP worktree has no `.codegraph/`. |
| Server isolated source/archive equivalence | `PASS_SERVER_MOCK_ARCHIVE_EQUIVALENCE` | Server smoke used archived commits `2fdfb945268bcb8ab2f08c6288869afd035b9e16` and `e6889daac6babde65e52716ffc5acdc8b5ad2314` with safe archive SHA256s recorded in the CP9 plan checkpoint. |
| Prodlike deployed mock full-chain | `PASS` | Isolated prodlike stack with Sub2API `839c73f0d5b10ce873210f92bf679630e789615d` and CC Gateway `e6889daac6babde65e52716ffc5acdc8b5ad2314` passed positive/negative/spoofing/leak gates without touching 3012 or 3017. |
| Deployed 3017 image/config/profile equivalence | `BLOCKED_EXTERNAL_EVIDENCE` | CP10 did not rebuild/restart/deploy 3017 and did not inspect production secrets. A real deployed-service equivalence gate is still required before production traffic. |
| Tiny live AWS smoke | `EXECUTED_REACHED_AWS_BLOCKED_UPSTREAM_ACCOUNT_STATE` | User-approved canary reached AWS with `x_api_key` and `claude-sonnet-4-6`, but AWS returned `status_4xx` / `organization_disabled`. No production profile is marked green. |
| Formal-pool production traffic | `BLOCKED_UPSTREAM_ACCOUNT_STATE` | No production traffic until a valid AWS account/workspace returns `status_2xx` for exactly one CP0 auth profile, deployed equivalence is green, and final production enablement review passes. |

## Non-claims and safety policy

- This map does not contain or authorize raw workspace IDs, API keys, Authorization/x-api-key values, raw prompt/body/response, raw HMAC input/output, canonical request/string-to-sign output, cookies, proxy credentials, or raw telemetry.
- Raw workspace IDs are allowed only in sensitive credential/runtime storage. Evidence may use only `workspace_ref`, endpoint/region/profile refs, booleans, status buckets, and safe counts.
- `signed_cch`, sign-primary, production `no_cch`, Bearer API-key compatibility, and SigV4 remain disabled/fail-closed unless their named proof gates pass. CP11 did not enable any of these fallbacks.
- Plan 59 remains additive to plan 58. `strip_attribution` remains the shared formal-pool default and no direct fallback is allowed.


## CP9 server mock smoke evidence

- Host ref: `66.163.122.103`.
- Smoke root: `/opt/claude-platform-aws-smoke-2fdfb945-e6889da`.
- Passing report root: `/opt/claude-platform-aws-smoke-2fdfb945-e6889da/reports/aws-platform-smoke-20260628T013618Z-1`.
- Result: `PASS_SERVER_MOCK_SIMULATED_FULL_CHAIN` with `real_aws_upstream=false`, `no_live_aws_canary=true`, `no_3012_change=true`, and `no_3017_restart_or_deploy=true`.
- CP0 production auth profile remains `BLOCKED_AUTH_PROFILE`; mocked `x_api_key` evidence does not production-enable `x_api_key` and does not prove `bearer_api_key`.
- The first server run exposed a harness-only symlink/path issue before the Sub2API CP6 E2E; the passing rerun mounted CC Gateway directly at the fixed CP6 test path inside the isolated Docker container. No production code changed for that rerun.
- Sensitive scans passed. Evidence contains only safe refs, commit IDs, SHA256s, booleans/status buckets, test counts, and safe report paths.

## Tiny live AWS canary runbook - executed CP11, retained as procedure

Status: `EXECUTED_REACHED_AWS_BLOCKED_UPSTREAM_ACCOUNT_STATE`.

Doc basis checked on 2026-06-28:

- Claude Platform on AWS uses the Claude `/v1/messages` API surface with a regional base URL and required `anthropic-workspace-id` header.
- The target rollout continues to use `https://aws-external-anthropic.us-east-1.api.aws` and region `us-east-1`.
- Current Anthropic setup docs say the platform-specific client resolves `apiKey` and `ANTHROPIC_AWS_API_KEY` to the `x-api-key` header and the SDK handles `anthropic-workspace-id`. Therefore the production gate still treats `x_api_key` and `bearer_api_key` as separate, mutually exclusive CP0 profiles. CP11 attempted only the selected `x_api_key` profile and did not silently fall back.

Prerequisites before running:

1. User supplies a real Claude Platform on AWS account/workspace/API key through an approved sensitive channel or local sensitive config, not through commit/log/evidence.
2. The real workspace belongs to `us-east-1` and has a safe `workspace_ref`, `endpoint_ref`, `credential_ref`, `credential_binding_hmac`, and `workspace_binding_hmac` derived by server code.
3. The prodlike stack source remains equivalent to Sub2API `839c73f0d5b10ce873210f92bf679630e789615d` and CC Gateway `e6889daac6babde65e52716ffc5acdc8b5ad2314`, or the evidence map is updated with the newer exact commits before the canary.
4. Billing/CCH policy remains `strip`; `strip_attribution` remains default; `signed_cch`, `no_cch`, `bearer_api_key`, SigV4 production, and direct upstream fallback remain disabled.
5. The run uses exactly one minimal `/v1/messages` request. The request body is constructed in memory and is not printed, persisted, committed, or copied into evidence.

Execution plan:

1. Add or activate exactly one `type = "claude-platform-aws"` formal-pool account using the user-provided real workspace/API key and selected egress. Stop here for user-provided material if no real account is present.
2. Mark CP0 preconditions as `PENDING` until the canary response is observed; do not mark `x_api_key` proven before the live request.
3. Send one tiny request through the same formal-pool path: `client -> Sub2API -> CC Gateway -> selected egress -> https://aws-external-anthropic.us-east-1.api.aws/v1/messages`.
4. Record only safe fields:
   - Sub2API commit and CC Gateway commit.
   - region `us-east-1`.
   - `workspace_ref`, `endpoint_ref`, `credential_ref`, profile refs, and booleans.
   - status bucket such as `status_2xx`, `status_4xx`, or `status_5xx`.
   - provider request shape booleans: path allowed, query empty, workspace header present from server, selected auth profile present, forbidden auth header absent, internal headers absent, beta/billing/CCH stripped.
   - token counts only if already returned as aggregate numeric fields and not accompanied by raw body/response content.
5. If a future live canary succeeds with `status_2xx`, update CP0 evidence to `x_api_key_proven=true` for that exact endpoint/region/workspace_ref/credential_ref/request shape, keep `bearer_api_key` unproven, and require a separate production enablement review.
6. CP11 failed with safe bucket `organization_disabled`; record only safe status/error buckets and keep production blocked. Do not retry with `bearer_api_key`, do not enable SigV4, do not enable `no_cch` or `signed_cch`, and do not bypass CC Gateway.

Rollback / fail-closed plan:

- Disable or unschedule the `claude-platform-aws` formal-pool account.
- Keep CP0 auth profile status blocked until a valid AWS account/workspace returns `status_2xx`.
- Keep direct production traffic blocked.
- Preserve only safe canary evidence and delete any local temporary raw request/response capture if a tool accidentally created one.
