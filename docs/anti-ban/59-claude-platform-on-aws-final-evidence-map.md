# 59 - Claude Platform on AWS Final Evidence Map

Date: 2026-06-28

Scope: plan 59 local/mock implementation evidence for `type = "claude-platform-aws"` as a third independent formal-pool account type. This map records only safe refs, commit IDs, test counts, status buckets, and non-claims. It is not live AWS evidence and does not authorize production traffic. CP9 adds server-side mock/simulated full-chain smoke evidence without touching 3012 or deploying/restarting 3017.

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

## Production-readiness map

| Production gate | Status | Reason |
|---|---|---|
| CP0 auth profile selection | `BLOCKED_AUTH_PROFILE` | No real target AWS workspace/API-key evidence has been supplied. `x_api_key` and `bearer_api_key` remain mutually exclusive; silent fallback is forbidden. |
| Workspace/region/endpoint proof | `PASS_LOCAL_AND_SERVER_MOCK_SHAPE`, live `BLOCKED_EXTERNAL_EVIDENCE` | Local and server mock evidence bind safe workspace refs to `us-east-1` and `endpoint_ref:aws-external-anthropic-us-east-1`; no raw workspace ID is recorded here. |
| Sub2API targeted tests | `PASS_LOCAL_TARGETED` | CP1-CP6 targeted commands in the plan record passed. |
| CC Gateway targeted/full tests | `PASS_LOCAL_TARGETED_AND_BROAD_GREEN` | CP7 record reports CP7 SigV4 `3 passed`, CP5 AWS `17 passed`, preflight `8 passed`, build passed, full CC Gateway suite `225 passed`. |
| Broad Sub2API Go suite | `BLOCKED_HISTORICAL_OR_EXTERNAL` | CP1 audit records historical test drift and one external network/module-cache blocker unrelated to `fa50af8cfa26`/59 work. Do not claim broad Sub2API green. |
| Safe artifact/leak scan | `PASS_LOCAL_AND_SERVER_PATTERN_SCAN` | Records, changed-file scans, server smoke report scan, and host-side precise scan report no raw workspace ID, API key, Authorization value, raw prompt/body/response, raw HMAC input/output, cookie, proxy credential, or raw telemetry. |
| CodeGraph refresh | `PASS_SUB2API`; `NOT_AVAILABLE_CCGATEWAY_CP_WORKTREE` | Sub2API `.codegraph/` has been incrementally synced after checkpoint records. CC Gateway CP worktree has no `.codegraph/`. |
| Server isolated source/archive equivalence | `PASS_SERVER_MOCK_ARCHIVE_EQUIVALENCE` | Server smoke used archived commits `2fdfb945268bcb8ab2f08c6288869afd035b9e16` and `e6889daac6babde65e52716ffc5acdc8b5ad2314` with safe archive SHA256s recorded in the CP9 plan checkpoint. |
| Deployed 3017 image/config/profile equivalence | `BLOCKED_EXTERNAL_EVIDENCE` | CP9 did not rebuild/restart/deploy 3017 and did not inspect production secrets. A real deployed-service equivalence gate is still required before any live smoke. |
| Tiny live AWS smoke | `BLOCKED_USER_APPROVAL_AND_EXTERNAL_EVIDENCE` | Requires CP0 proof, deployed equivalence, explicit approval, and a tiny cost envelope. Not run. |
| Formal-pool production traffic | `BLOCKED` | No live formal-pool traffic until local gates, deployed equivalence, approved live smoke, and CP0 profile proof are all green. |

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
