# 59 - Claude Platform on AWS Final Evidence Map

Date: 2026-06-28

Scope: plan 59 local/mock implementation evidence for `type = "claude-platform-aws"` as a third independent formal-pool account type. This map records only safe refs, commit IDs, test counts, status buckets, and non-claims. It is not live AWS evidence and does not authorize production traffic. CP9 adds server-side mock/simulated full-chain smoke evidence without touching 3012 or deploying/restarting 3017. CP10 records a production-like deployed mock full-chain smoke for the `839c73f` Sub2API fix and `e6889da` CC Gateway build, then leaves the tiny live AWS canary behind explicit user approval and CP0 evidence.

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
| Production auth profile | `BLOCKED_AUTH_PROFILE` until a real target workspace/API-key proof selects exactly one profile |
| Deployed/live status | `SERVER_MOCK_SMOKE_PASS_DEPLOYED_3017_BLOCKED`; no 3017 restart/deploy, no 3012 change, no live AWS/canary traffic |
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
| CP10 prodlike deployed mock full-chain smoke | `PASS_PRODLIKE_MOCK_FULL_CHAIN` | Deployed stack `/opt/claude-platform-aws-prodlike-63e178ef-e6889da` ran Sub2API `839c73f0d5b10ce873210f92bf679630e789615d` and CC Gateway `e6889daac6babde65e52716ffc5acdc8b5ad2314`; positive safe evidence reached mock AWS through `client -> Sub2API -> CC Gateway -> connect-proxy -> mock AWS`; negative no-bypass/direct-CCG/spoofing/leak/DTO scans passed. | Still mock-only: tiny live AWS canary and CP0 target profile proof remain pending explicit user approval and user-supplied real AWS Platform account material. |

## Production-readiness map

| Production gate | Status | Reason |
|---|---|---|
| CP0 auth profile selection | `PENDING_CP0`, production `BLOCKED_AUTH_PROFILE` | Mock evidence proves the deployed `x_api_key` request shape only. No real target AWS workspace/API-key proof has been run. `x_api_key` and `bearer_api_key` remain mutually exclusive; silent fallback is forbidden. |
| Workspace/region/endpoint proof | `PASS_LOCAL_SERVER_AND_PRODLIKE_MOCK_SHAPE`, live `BLOCKED_EXTERNAL_EVIDENCE` | Local, server mock, and prodlike mock evidence bind safe workspace refs to `us-east-1` and the AWS Platform endpoint shape; no raw workspace ID is recorded here. |
| Sub2API targeted tests | `PASS_LOCAL_TARGETED` | CP1-CP6 targeted commands in the plan record passed. |
| CC Gateway targeted/full tests | `PASS_LOCAL_TARGETED_AND_BROAD_GREEN` | CP7 record reports CP7 SigV4 `3 passed`, CP5 AWS `17 passed`, preflight `8 passed`, build passed, full CC Gateway suite `225 passed`. |
| Broad Sub2API Go suite | `BLOCKED_HISTORICAL_OR_EXTERNAL` | CP1 audit records historical test drift and one external network/module-cache blocker unrelated to `fa50af8cfa26`/59 work. Do not claim broad Sub2API green. |
| Safe artifact/leak scan | `PASS_LOCAL_SERVER_AND_PRODLIKE_PATTERN_SCAN` | Records, changed-file scans, server smoke report scan, host-side precise scan, prodlike reports/logs/session-ledger/DTO scan report no raw workspace ID, API key, Authorization value, raw prompt/body/response, raw HMAC input/output, cookie, proxy credential, or raw telemetry outside explicitly sensitive runtime/config storage. |
| CodeGraph refresh | `PASS_SUB2API`; `NOT_AVAILABLE_CCGATEWAY_CP_WORKTREE` | Sub2API `.codegraph/` has been incrementally synced after checkpoint records. CC Gateway CP worktree has no `.codegraph/`. |
| Server isolated source/archive equivalence | `PASS_SERVER_MOCK_ARCHIVE_EQUIVALENCE` | Server smoke used archived commits `2fdfb945268bcb8ab2f08c6288869afd035b9e16` and `e6889daac6babde65e52716ffc5acdc8b5ad2314` with safe archive SHA256s recorded in the CP9 plan checkpoint. |
| Prodlike deployed mock full-chain | `PASS` | Isolated prodlike stack with Sub2API `839c73f0d5b10ce873210f92bf679630e789615d` and CC Gateway `e6889daac6babde65e52716ffc5acdc8b5ad2314` passed positive/negative/spoofing/leak gates without touching 3012 or 3017. |
| Deployed 3017 image/config/profile equivalence | `BLOCKED_EXTERNAL_EVIDENCE` | CP10 did not rebuild/restart/deploy 3017 and did not inspect production secrets. A real deployed-service equivalence gate is still required before production traffic. |
| Tiny live AWS smoke | `PENDING_USER_APPROVAL_AND_CP0` | Requires a user-provided real AWS Platform upstream account/workspace/API key, explicit approval, and a tiny cost envelope. Not run. |
| Formal-pool production traffic | `BLOCKED` | No production traffic until CP0, tiny live canary, deployed equivalence, and final production enablement review are all green. |

## Non-claims and safety policy

- This map does not contain or authorize raw workspace IDs, API keys, Authorization/x-api-key values, raw prompt/body/response, raw HMAC input/output, canonical request/string-to-sign output, cookies, proxy credentials, or raw telemetry.
- Raw workspace IDs are allowed only in sensitive credential/runtime storage. Evidence may use only `workspace_ref`, endpoint/region/profile refs, booleans, status buckets, and safe counts.
- `signed_cch`, sign-primary, production `no_cch`, Bearer API-key compatibility, and SigV4 remain disabled/fail-closed unless their named proof gates pass.
- Plan 59 remains additive to plan 58. `strip_attribution` remains the shared formal-pool default and no direct fallback is allowed.


## CP9 server mock smoke evidence

- Host ref: `66.163.122.103`.
- Smoke root: `/opt/claude-platform-aws-smoke-2fdfb945-e6889da`.
- Passing report root: `/opt/claude-platform-aws-smoke-2fdfb945-e6889da/reports/aws-platform-smoke-20260628T013618Z-1`.
- Result: `PASS_SERVER_MOCK_SIMULATED_FULL_CHAIN` with `real_aws_upstream=false`, `no_live_aws_canary=true`, `no_3012_change=true`, and `no_3017_restart_or_deploy=true`.
- CP0 production auth profile remains `BLOCKED_AUTH_PROFILE`; mocked `x_api_key` evidence does not production-enable `x_api_key` and does not prove `bearer_api_key`.
- The first server run exposed a harness-only symlink/path issue before the Sub2API CP6 E2E; the passing rerun mounted CC Gateway directly at the fixed CP6 test path inside the isolated Docker container. No production code changed for that rerun.
- Sensitive scans passed. Evidence contains only safe refs, commit IDs, SHA256s, booleans/status buckets, test counts, and safe report paths.

## Tiny live AWS canary runbook - prepared, not executed

Status: `PENDING_USER_APPROVAL_AND_CP0`.

Doc basis checked on 2026-06-28:

- Claude Platform on AWS uses the Claude `/v1/messages` API surface with a regional base URL and required `anthropic-workspace-id` header.
- The target rollout continues to use `https://aws-external-anthropic.us-east-1.api.aws` and region `us-east-1`.
- Current Anthropic setup docs say the platform-specific client resolves `apiKey` and `ANTHROPIC_AWS_API_KEY` to the `x-api-key` header; AWS docs also describe API-key authorization with bearer-token wording. Therefore the production gate still treats `x_api_key` and `bearer_api_key` as separate, mutually exclusive CP0 profiles. The tiny canary below is for the selected `x_api_key` profile only and must not silently fall back.

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
5. If the live canary succeeds with `status_2xx`, update CP0 evidence to `x_api_key_proven=true` for this exact endpoint/region/workspace_ref/credential_ref/request shape, keep `bearer_api_key` unproven, and require a separate production enablement review.
6. If the live canary fails, record only the safe status bucket/error-code bucket and keep production `BLOCKED_AUTH_PROFILE`. Do not retry with `bearer_api_key`, do not enable SigV4, do not enable `no_cch` or `signed_cch`, and do not bypass CC Gateway.

Rollback / fail-closed plan:

- Disable or unschedule the `claude-platform-aws` formal-pool account.
- Keep CP0 auth profile status `BLOCKED_AUTH_PROFILE`.
- Keep direct production traffic blocked.
- Preserve only safe canary evidence and delete any local temporary raw request/response capture if a tool accidentally created one.
