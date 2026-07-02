# 78 - Local Mock Response Bridge and Real Sub2API Smoke Evidence Report

**Final decision:** PASS_REAL_SUB2API_CC_GATEWAY_SIDECAR_MOCK_RESPONSE_READY

**Scope:** Plan78 implementation addendum and deployed mock smoke only. The change was intentionally narrow and implemented in CC Gateway only: in explicit local-smoke mode, after the real Go/uTLS sidecar returns a verified TLS summary bucket, CC Gateway can synthesize an Anthropic Messages-compatible mock response body for real Sub2API server smoke. This was not a production deployment, not a live canary, and did not access any real Anthropic/AWS/Vertex/Bedrock/OpenAI/DeepSeek or credentialed upstream.

**Report generated:** 2026-07-02T11:40:36Z  
**Server scratch roots:** primary `/root/plan78-smoke-20260702T100859Z`; fallback `/root/plan78-smoke-tuple-2185-20260702T121232Z`; rollback `/root/plan78-smoke-tuple-2179-20260702T121309Z`  
**Scratch cleanup status:** `skipped_requires_user_approval` (scratch retained; no cleanup/delete performed).

## Inputs and source lock

| Component | Required / input commit | Used commit / artifact | Evidence |
|---|---|---|---|
| Plan77b report | `c3c7ec125` | Sub2API HEAD before this report: `c3c7ec125e974228dea77a408f94f3425209d9b6` | present locally |
| Plan77 report | `3c8b5a1c2` | ancestor of Sub2API HEAD | present locally |
| Sub2API Plan76 implementation | `13f3fa08cfa58742b80c20d4534b49f82b621599` | server source tar bucket `sub2api-c3c7ec125.tar.gz` | descendant includes implementation/report commits |
| CC Gateway Plan78 implementation | base `d3f1920`, required descendant of `fdf29bd` | `7957824ed2f91cf1da874f6910a6ba08c01678b0` | committed: `gateway: add local smoke mock response bridge` |
| CC Gateway Plan78 review fix | review-required real-sidecar proof fixes | `7729410` | committed: `test: prove real sidecar mock bridge smoke` |
| Sub2API server artifact | built in scratch | sha256 `e4a441b38dd933542b5fb0cbd57f1412567883271af53b44ddd3bc1f557c55dd` | scratch/build only |
| CC Gateway `dist/index.js` | built in scratch from `7957824` | sha256 `2badf2adeff8d7bda44e547afdda2e68ee0146cbab8cdc2ca7ad5922e7c02a34` | real `dist/index.js` |
| CC Gateway `dist/proxy.js` | built in scratch from `7957824` | sha256 `9892c702b77bb06c5bc12a0d8d9cf6c21daa3402b50c83c8944f0f7660d8400e` | includes bridge implementation |
| CC Gateway sidecar client dist | built in scratch from `7957824` | sha256 `d42dd84f166023c1ddaf7775516b0c96f1f93455466957b4aa827231193cbc30` | includes config/runtime guards |
| Go/uTLS sidecar | unchanged real sidecar binary | sha256 `e39fd967433f49189079acc37ea25d4fc45c9dd0eea5600e6cc78ae4d5206c13` | real sidecar used |

No artifact was installed into a production path.

## Code changes

CC Gateway commit: `7957824ed2f91cf1da874f6910a6ba08c01678b0` (`gateway: add local smoke mock response bridge`).

Files changed:

| File | Summary |
|---|---|
| `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/src/egress-sidecar-client.ts` | Added `egress_tls_sidecar.mock_messages_response` validation and stable local-only rejection rules. |
| `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/src/proxy.ts` | Added runtime safety gate; in the existing TLS sidecar branch, calls the sidecar first, requires the TLS summary bucket, then optionally replaces verification JSON with synthetic Messages-compatible body only in local-smoke mode. |
| `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/tests/config.test.ts` | Added config gating tests for production rejection and explicit local-smoke enablement. |
| `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/tests/egress-tls-sidecar.test.ts` | Added sidecar-first bridge, default-disabled, unsafe/provider-direct, and no Node-direct fallback tests. |
| `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/tests/formal-pool-real-chain-mock-response.test.ts` | Added real-chain mock response tests for Messages-compatible response, fail-closed-before-sidecar paths, and CCH/billing strip assertions. |
| `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/tests/egress-tls-sidecar-real.test.ts` | Strengthened real Go/uTLS sidecar E2E to prove TLS bucket before synthetic Messages response in explicit local-smoke mode. |
| `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/sidecar/egress-tls-sidecar/internal/server/server_test.go` | Fixed a stale test helper call; no sidecar production code changed. |

Go sidecar production source changed: false. One Go sidecar test helper call was corrected so the unchanged sidecar package test suite compiles and passes.

## Config gating / production default disabled proof

| Gate | Result |
|---|---|
| Default config with sidecar enabled but mock bridge absent | returns existing sidecar verification response; no synthetic Messages body |
| `egress_tls_sidecar.mock_messages_response.enabled=true` without `mode: local_smoke` | rejected at config load |
| production / real-canary / provider-direct upstream modes | rejected with stable local-only config error |
| `mode != sub2api` | rejected |
| sidecar disabled | rejected |
| non-loopback upstream URL | rejected |
| non-loopback sidecar endpoint | rejected at runtime safety gate |
| logical target not `api.anthropic.com` | rejected |
| extra control/override keys under mock bridge config | rejected |
| explicit local-smoke config with `shared_pool.upstream_mode` `local-capture` or `preflight` and loopback-only endpoints | accepted |

The production/default state is disabled; the bridge is reachable only with explicit local-smoke configuration.

## CP0-CP6 Plan78 status table

| CP | Status | Safe evidence |
|---|---|---|
| CP0 preflight | PASS | Sub2API and CC Gateway commits recorded; forbidden ports `3012/3017/18080/18081` and Plan78 ports `19282/19283/19284/19285` not listening before run |
| CP1 test-first implementation | PASS | CC Gateway tests added for config, sidecar bridge, fail-closed, and CCH/billing strip; implementation committed separately |
| CP2 build/prepare | PASS | Sub2API server, CC Gateway dist, node runtime deps, and real sidecar built/copied to scratch artifacts only |
| CP3 same-scope egress guard | PASS | Docker `--network none` scope; DNS/IPv4/IPv6 non-loopback/UDP/provider direct TCP blocked; proxy env cleared |
| CP4 start/health real chain | PASS | real Sub2API server, real CC Gateway `dist/index.js`, real Go/uTLS sidecar, and local collector health buckets all `status_200` |
| CP5 real-chain smoke | PASS | real Sub2API `/v1/messages` positive path returned Messages-compatible mock body via real CC + real sidecar; fail-closed cases stopped before sidecar/upstream |
| CP6 shutdown/report | PASS | Plan78-owned processes stopped; canary and forbidden ports not listening after; report uses safe summaries only |

## Canary/mock ports

| Component | Port | Bind scope |
|---|---:|---|
| Real Sub2API server canary | 19282 | loopback inside isolated `docker --network none` runner |
| Real CC Gateway canary | 19283 | loopback inside isolated `docker --network none` runner |
| Real Go/uTLS sidecar canary | 19284 | loopback inside isolated `docker --network none` runner |
| mock collector | 19285 | loopback inside isolated `docker --network none` runner |

Host ports were not published. Post-run host status for all four Plan78 ports was not listening.

## Forbidden ports before/after

| Port | Before | After |
|---:|---|---|
| 3012 | not listening | not listening |
| 3017 | not listening | not listening |
| 18080 | not listening | not listening |
| 18081 | not listening | not listening |
| 19282 | not listening | not listening |
| 19283 | not listening | not listening |
| 19284 | not listening | not listening |
| 19285 | not listening | not listening |

## Egress guard proof

| Guard item | Result |
|---|---|
| Scope | `docker_anchor_network_none_shared_netns` |
| DNS | blocked |
| IPv4 non-loopback | blocked |
| IPv6 non-loopback | blocked |
| UDP | blocked |
| inherited proxy env | cleared |
| provider direct TCP | blocked |
| guard pass | true |
| real upstream request count | 0 |
| non-loopback attempt count | 0 |

No real Anthropic/AWS/Vertex/Bedrock/OpenAI/DeepSeek or credentialed upstream request was made.

## Health/readiness

| Component | Result |
|---|---|
| mock collector | `status_200` |
| real Go/uTLS sidecar | `status_200` |
| real CC Gateway | `status_200` |
| real Sub2API server | `status_200` |

## Real Sub2API entrypoint proof

The Plan78 smoke was initiated from the real Sub2API HTTP entrypoint, not the Plan77 adapter. The path under test was:

`real Sub2API server canary -> real CC Gateway dist/index.js -> real Go/uTLS sidecar -> local mock collector`

Safe proof buckets:

- Sub2API positive cases returned HTTP `200` with body key bucket `content,id,model,role,stop_reason,stop_sequence,type,usage`.
- Response schema bucket: `anthropic_messages_compatible`.
- CC Gateway safe capture contained `01_final_upstream_request.json`, `02_upstream_response.json`, and `03_final_output.json` safe summaries.
- CC safe capture response header names included `x-cc-egress-tls-summary-bucket`, `x-cc-egress-tls-profile-status`, and `x-cc-mock-response-schema-bucket`.
- Mock response schema bucket: `anthropic-messages:synthetic-local-smoke-v1`.
- No raw prompt/body/response was committed to this report or repository.

## Smoke case table

### Primary 2.1.197 server-selected scope

| Case | Status | HTTP bucket | Response schema | Stable error | Send state | Collector HTTP delta |
|---|---|---|---|---|---|---:|
| `canonical_inbound_2179_to_2197` | PASS | `status_200` | `anthropic_messages_compatible` | `message` | sidecar_used_no_real_upstream | 0 |
| `canonical_inbound_2185_to_2197` | PASS | `status_200` | `anthropic_messages_compatible` | `message` | sidecar_used_no_real_upstream | 0 |
| `canonical_inbound_2197_to_2197` | PASS | `status_200` | `anthropic_messages_compatible` | `message` | sidecar_used_no_real_upstream | 0 |
| `sonnet5_2197_pass` | PASS | `status_200` | `anthropic_messages_compatible` | `message` | sidecar_used_no_real_upstream | 0 |
| `count_tokens_fail_closed` | PASS | `status_403` | `not_messages_compatible` | `formal_pool_count_tokens_profile_unapproved` | stopped_before_sidecar_upstream | 0 |
| `mcp_configured_fail_closed` | PASS | `status_403` | `not_messages_compatible` | `formal_pool_observed_client_profile_unapproved` | stopped_before_sidecar_upstream | 0 |
| `non_streaming_fail_closed` | PASS | `status_403` | `not_messages_compatible` | `formal_pool_non_streaming_profile_unapproved` | stopped_before_sidecar_upstream | 0 |
| `model_control_plane_fail_closed` | PASS | `status_404` | `not_messages_compatible` | `non_json` | stopped_before_sidecar_upstream | 0 |
| `env_base_url_residue_fail_closed` | PASS | `status_400` | `not_messages_compatible` | `formal_pool_env_residue_verifier_failed` | stopped_before_sidecar_upstream | 0 |
| `env_asia_shanghai_fail_closed` | PASS | `status_400` | `not_messages_compatible` | `formal_pool_env_residue_verifier_failed` | stopped_before_sidecar_upstream | 0 |
| `env_asia_urumqi_fail_closed` | PASS | `status_400` | `not_messages_compatible` | `formal_pool_env_residue_verifier_failed` | stopped_before_sidecar_upstream | 0 |
| `env_synthetic_domain_ai_residue_fail_closed` | PASS | `status_400` | `not_messages_compatible` | `formal_pool_env_residue_verifier_failed` | stopped_before_sidecar_upstream | 0 |

### Fallback/rollback addendum scopes

| Case | Status | HTTP bucket | Response schema | Stable error | Send state | Collector HTTP delta |
|---|---|---|---|---|---|---:|
| `fallback_2185_non_sonnet_pass` | PASS | `status_200` | `anthropic_messages_compatible` | `message` | sidecar_used_no_real_upstream | 0 |
| `sonnet5_2185_fail_closed` | PASS | `status_403` | `not_messages_compatible` | `formal_pool_model_version_unsupported` | stopped_before_sidecar_upstream | 0 |
| `rollback_2179_non_sonnet_pass` | PASS | `status_200` | `anthropic_messages_compatible` | `message` | sidecar_used_no_real_upstream | 0 |
| `sonnet5_2179_fail_closed` | PASS | `status_403` | `not_messages_compatible` | `formal_pool_model_version_unsupported` | stopped_before_sidecar_upstream | 0 |

Notes:

- For positive cases, Sub2API did not expose CC proof headers to the client; CC Gateway safe capture supplied the TLS/mock bridge proof.
- `stopped_before_sidecar_upstream` for fail-closed cases means the local collector count did not increase and real upstream/non-loopback counters stayed zero.
- `model_control_plane_fail_closed` reached Sub2API route-level `404/non_json` in this real-server scope; no sidecar/upstream/collector send occurred and the case is counted as local fail-closed for Plan78 smoke.
- Fallback/rollback addendum scopes reused the same fixed ports and loopback-only guard after confirming ports were free; each scope stopped all Plan78-owned processes and left forbidden/canary ports not listening.

## TLS safe summary

| Item | Result |
|---|---|
| real Go/uTLS sidecar binary used | true |
| sidecar health | PASS |
| sidecar endpoint bucket | `127.0.0.1:19284` |
| logical target host | `api.anthropic.com` |
| dial override target | loopback mock collector bucket `127.0.0.1:19285` |
| raw ClientHello persisted | false |
| CC safe capture TLS bucket present | true |
| 2.1.197 primary TLS bucket | matches Plan75/76 `claude-code-real-oracle-2197` safe bucket |
| fallback/rollback TLS bucket | real server addendum scopes used the Plan75/76 `claude-code-real-oracle-2179` safe bucket; CC safe capture TLS bucket present in both addendum scopes |

The bridge did not bypass the sidecar: CC Gateway required the sidecar TLS summary bucket before returning the synthetic Messages-compatible mock body.

## Mock response schema bucket

| Field | Bucket |
|---|---|
| body top-level keys | `content,id,model,role,stop_reason,stop_sequence,type,usage` |
| `type` | message-compatible |
| `role` | assistant-compatible |
| `content` | array of synthetic text block(s) |
| `usage` | synthetic token count object with `input_tokens,output_tokens` keys |
| raw user prompt echoed | false |
| raw body persisted | false |
| schema bucket | `anthropic-messages:synthetic-local-smoke-v1` |

## Plan76 / Plan77b blocker closure matrix

| Blocker/path | Closure method | Result | Stable bucket | Sidecar/upstream before send? | Real upstream | Non-loopback | Node direct HTTPS |
|---|---|---|---|---|---:|---:|---:|
| Plan77b response shape gap | implementation + observed real-chain smoke | closed | `anthropic_messages_compatible` | sidecar first for positive path | 0 | 0 | 0 |
| count_tokens | policy_fail_closed | PASS | `formal_pool_count_tokens_profile_unapproved` | stopped before sidecar/upstream | 0 | 0 | 0 |
| MCP configured shape | policy_fail_closed | PASS | `formal_pool_observed_client_profile_unapproved` | stopped before sidecar/upstream | 0 | 0 | 0 |
| non-streaming shape | policy_fail_closed | PASS | `formal_pool_non_streaming_profile_unapproved` | stopped before sidecar/upstream | 0 | 0 | 0 |
| model/control-plane | policy_fail_closed | PASS | `non_json` route-local fail-closed bucket | stopped before sidecar/upstream | 0 | 0 | 0 |
| Sonnet 5 under 2.1.197 | observed_shape + mock response bridge | PASS | `anthropic_messages_compatible` | sidecar first for positive path | 0 | 0 | 0 |
| Sonnet 5 under 2.1.185 / 2.1.179 | policy_fail_closed observed in Plan78 fallback/rollback addendum scopes | PASS | `formal_pool_model_version_unsupported` | stopped before sidecar/upstream | 0 | 0 | 0 |
| env residue variants | policy_fail_closed/canonicalized | PASS | `formal_pool_env_residue_verifier_failed` | stopped before sidecar/upstream | 0 | 0 | 0 |

## Plan72 env residue validation

| Check | Result |
|---|---|
| non-official base URL marker | fail-closed before sidecar/upstream with `formal_pool_env_residue_verifier_failed` |
| `TZ=Asia/Shanghai` marker | fail-closed before sidecar/upstream with `formal_pool_env_residue_verifier_failed` |
| `TZ=Asia/Urumqi` marker | fail-closed before sidecar/upstream with `formal_pool_env_residue_verifier_failed` |
| synthetic domain / AI keyword residue | fail-closed before sidecar/upstream with `formal_pool_env_residue_verifier_failed` |
| residue changed logical host / tuple / proxy identity | false |
| upstream authority changed to non-official host | false |

## CCH/billing/attribution assertions

| Assertion | Result |
|---|---|
| `x-anthropic-billing-*` present | false |
| raw/native CCH present | false |
| signed/no-CCH mode present | false |
| client attribution present | false |
| final output verifier safe capture present | true |
| raw body persisted | false |

## Final counters

| Counter | Value |
|---|---:|
| primary server smoke runner exit code | 0 |
| fallback 2.1.185 addendum runner exit code | 0 |
| rollback 2.1.179 addendum runner exit code | 0 |
| failed smoke cases | 0 |
| collector request count | 0 |
| real upstream request count | 0 |
| Node direct HTTPS fallback count | 0 |
| non-loopback attempt count | 0 |
| CCH/billing present | 0 |
| raw/native CCH present | 0 |
| signed/no-CCH present | 0 |
| client attribution present | 0 |
| Plan78-owned process leftovers | 0 |

## Test results

### CC Gateway

Commands run from `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5` with bundled Node on `PATH`:

| Command | Result |
|---|---|
| `npx tsx tests/egress-tls-sidecar.test.ts` | PASS: `21 passed, 0 failed` |
| `npx tsx tests/egress-tls-sidecar-real.test.ts` | PASS: `1 passed, 0 failed` |
| `npx tsx tests/formal-pool-real-chain-mock-response.test.ts` | PASS: `2 passed, 0 failed` |
| `npx tsx tests/proxy-sub2api.test.ts` | PASS: `43 passed, 0 failed` |
| `npx tsx tests/config.test.ts` | PASS: `27 passed, 0 failed` |
| `npx tsc --noEmit` | PASS |

### Sub2API

Command run from `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/backend`:

```text
go test ./internal/service -run 'CCGateway|FormalPool|ObservedProfile|TLSProfile|EnvResidue|LocalEnv|Canonical|Promotion|ControlPlane|CCH|Billing|Model|CountTokens|MCP|Streaming' -count=1
```

Result: PASS, `ok github.com/Wei-Shaw/sub2api/internal/service 10.183s`.

### Sidecar

Go sidecar production source was not changed by Plan78. The stale `safePolicy` helper call in `internal/server/server_test.go` was corrected, and the sidecar package suite now passes:

```text
cd /Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/sidecar/egress-tls-sidecar
go test ./...
```

Result: PASS across `cmd/egress-tls-sidecar`, `internal/control`, `internal/profile`, `internal/server`, `internal/summary`, and `internal/tlsengine`.

## Review verdicts

| Review | Verdict | Notes |
|---|---|---|
| Initial Plan78 implementation review | REQUIRED_EDITS | Required stronger runtime safety around production-like bypass and Node-direct fallback; fixed before commit. |
| Review after first report draft | REQUIRED_EDITS | Required sidecar Go test fix, real Go/uTLS sidecar + mock bridge proof, and fallback/rollback server-smoke evidence; fixed with CC Gateway commit `7729410` plus server addendum scopes. |
| Final Plan78 implementation review | PASS-ready after fixes | Production default disabled, explicit local-smoke gating, real sidecar TLS proof preserved, no Node direct fallback, no real upstream, fail-closed cases before sidecar, Messages-compatible Sub2API response proven, no raw evidence/secret leakage. |

## Leak scan

Local report leak scan status: PASS after review. Blocking findings: 0.

Server safe-evidence leak scan status: reviewed. One safe false-positive bucket was the word `oauth` inside a synthetic fix-summary filename; no raw credential, cookie, account id, workspace id, raw prompt/body/response, cert/key, native dump, HAR/pcap, or provider secret was written to this report or committed evidence.

## Rollback / production baseline

- Production/live canary planned: false.
- Production paths modified: false.
- No production service was stopped, restarted, rebound, or deployed.
- Existing production/rollback state was not changed by Plan78.
- Plan78-owned processes were stopped by runner exit.
- Ports `3012`, `3017`, `18080`, and `18081` were untouched before and after.

## Explicit non-actions

- No production deployment.
- No live canary.
- No production traffic switch.
- No real upstream access.
- No real OAuth/API key/session cookie/account/billing/proxy credentials used.
- No Node direct HTTPS fallback.
- No non-loopback egress.
- No scratch/raw logs/native dumps/secrets committed.
- No raw prompt/body/response, raw TLS/pcap/HAR, cert/key, cookie, account id, workspace id, or secret material was written into this report.

## Limitations

- This is still a deployed mock smoke, not a production deploy or live canary.
- The mock response bridge is intentionally local-smoke only and must remain disabled for production/provider-direct modes.
- The mock collector is used only as loopback TLS proof target; user-visible mock content is synthetic and generated by CC Gateway after sidecar proof.
- Even with PASS, live canary and production deployment still require separate explicit approval and runbook execution.

## Commits

| Repository | Commit | Purpose |
|---|---|---|
| CC Gateway | `7957824ed2f91cf1da874f6910a6ba08c01678b0` | Plan78 local-smoke mock response bridge code/tests |
| CC Gateway | `7729410` | Review-fix real sidecar mock bridge E2E and sidecar test compile fix |
| Sub2API | pending report commit | This Plan78 evidence report |
