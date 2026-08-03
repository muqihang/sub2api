# 77 - Claude Code 2.1.197 Server Deployed Mock Smoke Evidence Report

**Final smoke decision:** PASS

**Scope:** server deployed mock smoke only. No production deploy, no live canary, and no credentialed/paid upstream access. The smoke used a loopback-only mock collector plus the real Go/uTLS sidecar. A Plan77 Sub2API canary adapter was used for server-selected tuple and policy-shape generation, and a second smoke executed the real built CC Gateway `dist/index.js` against the real built sidecar. This report does **not** claim production Sub2API deployment or production traffic readiness.

**Server scratch root:** `/tmp/plan77-server-mock-smoke-20260701T154439Z`  
**Safe evidence archive:** `/tmp/plan77-safe-evidence.tgz` on local operator host, sha256 `715ef1aebdadb02b856dde459b093673ef75a9ac56e0e3621a646841b820ad4c`.  
**Report generated:** 2026-07-02T06:06:47Z

## Inputs and source lock

| Component | Required commit | Used commit | Proof |
|---|---|---|---|
| Sub2API Plan76 report | `ffcbecaa9` | `15e53f8b3` | descendant contains required report: true |
| Sub2API implementation | `13f3fa08cfa58742b80c20d4534b49f82b621599` | `15e53f8b3` | descendant contains required implementation: true |
| CC Gateway | `fdf29bd062b378234ce3a4b9b17c9c8e6b2016d7` | `d3f1920` | descendant contains required commit: true |

Source transport: `local_git_archive_tar_gz`. Runner image: `formal-pool-strict-runner:b6c999481-443052a`. Production paths modified during build: `False`.

## CP0-CP10 status table

| CP | Status | Evidence |
|---|---|---|
| CP0 preflight / rollback baseline | PASS | forbidden ports initially not listening; rollback baseline recorded; old cpaws image cleanup authorized and completed |
| CP1 paired builds | PASS | Sub2API, CC Gateway, sidecar artifacts built in scratch path |
| CP2 same-scope egress guard | PASS | Docker `--network none` guard proof for adapter smoke and real-CC smoke |
| CP3 start canary/mock processes | PASS | loopback ports used inside isolated container; host ports not published |
| CP4 health/readiness | PASS | Sub2API adapter, CC Gateway adapter/real CC, sidecar, collector health buckets 200 |
| CP5 canonical 2.1.197 primary | PASS | inbound 2.1.179/2.1.185/2.1.197 selected 2.1.197 in real CC smoke |
| CP6 Sonnet/fallback/rollback/switching | PASS | 2.1.197 Sonnet 5 pass; 2.1.185/2.1.179 Sonnet 5 fail-closed; fallback/rollback non-Sonnet pass |
| CP7 Plan76 fail-closed | PASS | count_tokens, MCP, non-streaming, model/control-plane fail before sidecar/upstream |
| CP8 Plan72 env residue | PASS | residue fail-closed/canonical authority unaffected |
| CP9 TLS/CCH/billing | PASS | real Go/uTLS sidecar used; no CCH/billing/client attribution buckets |
| CP10 counters/leak/shutdown | PASS | final counters zero; no canary/forbidden ports listening; leak scan PASS |

## Preflight and rollback baseline

- Forbidden ports before: `18080=false, 18081=false, 3012=false, 3017=false`.
- Forbidden ports after: `3012=false, 3017=false, 18080=false, 18081=false`.
- Rollback baseline recorded with `production_change_planned=false`, docker available `True`, repo candidate count `2`.
- Old cpaws images: before `4`, after `0`, unrelated images touched `False`.
- Host canary ports after shutdown: `19082=false, 19083=false, 19084=false, 19085=false`.

## Build artifacts

| Component | Command bucket | Exit | Artifact bucket(s) |
|---|---|---:|---|
| Sub2API | `docker_run_go_build_sub2api_server_and_harness` | 0 | `sub2api-server` sha `e4a441b38dd93354`, `sub2api-cli-through-harness` sha `bec5610dcf264830` |
| CC Gateway | `docker_run_npm_ci_tsc_cc_gateway` | 0 | `dist/index.js` sha `2badf2adeff8d7bd` |
| Go/uTLS sidecar | `docker_run_go_build_egress_tls_sidecar` | 0 | `egress-tls-sidecar` sha `e39fd967433f4918` |

No artifact was installed into a production path.

## Canary/mock ports and execution scope

| Component | Port | Bind scope |
|---|---:|---|
| Sub2API canary adapter | 19082 | loopback inside isolated `docker --network none` runner |
| CC Gateway canary / real CC Gateway | 19083 | loopback inside isolated `docker --network none` runner |
| Go/uTLS sidecar | 19084 | loopback inside isolated `docker --network none` runner |
| mock collector | 19085 | loopback inside isolated `docker --network none` runner |

Host did not bind or publish these ports during the final run.

## Egress guard proof

Both smoke scopes ran with the guard probes and canary processes in the same `docker --network none` execution scope.

| Scope | DNS | IPv4 non-loopback | IPv6 non-loopback | UDP | proxy env | provider direct TCP | real upstream | non-loopback count |
|---|---|---|---|---|---|---|---:|---:|
| adapter smoke | blocked | blocked | blocked | blocked | cleared | blocked | 0 | 0 |
| real CC smoke | blocked | blocked | blocked | blocked | cleared | blocked | 0 | 0 |

No real Anthropic/AWS/Vertex/Bedrock/OpenAI/DeepSeek or credentialed upstream request was made.

## Real CC Gateway + real sidecar smoke cases

| Case | Status | Selected | Inbound | HTTP bucket | Stable error | TLS bucket | Guard before upstream/sidecar |
|---|---|---|---|---|---|---|---|
| `canonical_inbound_2179_to_2197` | PASS | `2.1.197` | `2.1.179` | `status_200` | `none` | `tls-bucket:claude-code-real-oracle-2197` | false |
| `canonical_inbound_2185_to_2197` | PASS | `2.1.197` | `2.1.185` | `status_200` | `none` | `tls-bucket:claude-code-real-oracle-2197` | false |
| `canonical_inbound_2197_to_2197` | PASS | `2.1.197` | `2.1.197` | `status_200` | `none` | `tls-bucket:claude-code-real-oracle-2197` | false |
| `sonnet5_2197_pass` | PASS | `2.1.197` | `2.1.197` | `status_200` | `none` | `tls-bucket:claude-code-real-oracle-2197` | false |
| `spoof_tuple_fail_closed` | PASS | `2.1.197` | `2.1.197` | `status_403` | `formal_pool_model_version_unsupported` | `absent` | true |
| `count_tokens_fail_closed` | PASS | `2.1.197` | `2.1.197` | `status_403` | `formal_pool_count_tokens_profile_unapproved` | `absent` | true |
| `mcp_configured_fail_closed` | PASS | `2.1.197` | `2.1.197` | `status_403` | `formal_pool_mcp_shape_unapproved` | `absent` | true |
| `non_streaming_fail_closed` | PASS | `2.1.197` | `2.1.197` | `status_403` | `formal_pool_non_streaming_profile_unapproved` | `absent` | true |
| `model_control_plane_fail_closed` | PASS | `2.1.197` | `2.1.197` | `status_403` | `formal_pool_control_plane_unapproved` | `absent` | true |
| `env_base_url_residue_fail_closed` | PASS | `2.1.197` | `2.1.197` | `status_400` | `formal_pool_env_residue_verifier_failed` | `absent` | true |
| `old_session_drift_fail_closed` | PASS | `2.1.185` | `2.1.197` | `status_403` | `formal_pool_model_version_unsupported` | `absent` | true |
| `fallback_2185_non_sonnet_pass` | PASS | `2.1.185` | `2.1.185` | `status_200` | `none` | `tls-bucket:claude-code-real-oracle-2179` | false |
| `sonnet5_2185_fail_closed` | PASS | `2.1.185` | `2.1.185` | `status_403` | `formal_pool_model_version_unsupported` | `absent` | true |
| `rollback_2179_non_sonnet_pass` | PASS | `2.1.179` | `2.1.179` | `status_200` | `none` | `tls-bucket:claude-code-real-oracle-2179` | false |
| `sonnet5_2179_fail_closed` | PASS | `2.1.179` | `2.1.179` | `status_403` | `formal_pool_model_version_unsupported` | `absent` | true |

## Sub2API Plan77 canary adapter smoke cases

This layer validates server-selected tuple switching, fail-closed policy, and sidecar flow using a scratch Plan77 adapter. It is included as Sub2API policy-shape evidence, not as production Sub2API server deployment proof.

| Case | Status | Selected | Inbound | Result | Stable error | TLS bucket |
|---|---|---|---|---|---|---|
| `canonical_primary_inbound_2.1.179` | PASS | `2.1.197` | `2.1.179` | `forwarded_via_sidecar` | `none` | `tls-bucket:claude-code-real-oracle-2197` |
| `canonical_primary_inbound_2.1.185` | PASS | `2.1.197` | `2.1.185` | `forwarded_via_sidecar` | `none` | `tls-bucket:claude-code-real-oracle-2197` |
| `canonical_primary_inbound_2.1.197` | PASS | `2.1.197` | `2.1.197` | `forwarded_via_sidecar` | `none` | `tls-bucket:claude-code-real-oracle-2197` |
| `version_spoof_fail_closed` | PASS | `2.1.197` | `2.1.179` | `fail_closed` | `formal_pool_model_version_unsupported` | `absent` |
| `sonnet5_2197_pass` | PASS | `2.1.197` | `2.1.197` | `forwarded_via_sidecar` | `none` | `tls-bucket:claude-code-real-oracle-2197` |
| `sonnet5_2185_fail_closed` | PASS | `2.1.185` | `2.1.197` | `fail_closed` | `formal_pool_model_version_unsupported` | `absent` |
| `sonnet5_2179_fail_closed` | PASS | `2.1.179` | `2.1.197` | `fail_closed` | `formal_pool_model_version_unsupported` | `absent` |
| `fallback_2185_non_sonnet_pass` | PASS | `2.1.185` | `2.1.197` | `forwarded_via_sidecar` | `none` | `tls-bucket:claude-code-real-oracle-2179` |
| `rollback_2179_non_sonnet_pass` | PASS | `2.1.179` | `2.1.197` | `forwarded_via_sidecar` | `none` | `tls-bucket:claude-code-real-oracle-2179` |
| `tuple_switch_new_2197` | PASS | `2.1.197` | `2.1.197` | `forwarded_via_sidecar` | `none` | `tls-bucket:claude-code-real-oracle-2197` |
| `tuple_switch_new_2185` | PASS | `2.1.185` | `2.1.197` | `forwarded_via_sidecar` | `none` | `tls-bucket:claude-code-real-oracle-2179` |
| `tuple_switch_new_2179` | PASS | `2.1.179` | `2.1.197` | `forwarded_via_sidecar` | `none` | `tls-bucket:claude-code-real-oracle-2179` |
| `old_session_drift_fail_closed` | PASS | `2.1.185` | `2.1.197` | `fail_closed` | `formal_pool_session_authority_drift` | `absent` |
| `count_tokens_fail_closed` | PASS | `2.1.197` | `2.1.197` | `fail_closed` | `formal_pool_count_tokens_profile_unapproved` | `absent` |
| `mcp_configured_fail_closed` | PASS | `2.1.197` | `2.1.197` | `fail_closed` | `formal_pool_mcp_shape_unapproved` | `absent` |
| `non_streaming_fail_closed` | PASS | `2.1.197` | `2.1.197` | `fail_closed` | `formal_pool_non_streaming_profile_unapproved` | `absent` |
| `model_control_plane_fail_closed` | PASS | `2.1.197` | `2.1.197` | `fail_closed` | `formal_pool_control_plane_unapproved` | `absent` |
| `env_asia_shanghai_fail_closed` | PASS | `2.1.197` | `2.1.197` | `fail_closed` | `formal_pool_env_residue_verifier_failed` | `absent` |
| `env_asia_urumqi_fail_closed` | PASS | `2.1.197` | `2.1.197` | `fail_closed` | `formal_pool_env_residue_verifier_failed` | `absent` |
| `env_base_url_residue_fail_closed` | PASS | `2.1.197` | `2.1.197` | `fail_closed` | `formal_pool_env_residue_verifier_failed` | `absent` |
| `env_synthetic_domain_ai_residue_fail_closed` | PASS | `2.1.197` | `2.1.197` | `fail_closed` | `formal_pool_env_residue_verifier_failed` | `absent` |

## Plan76 blocker / closure matrix

| Blocker/case | Closure method | HTTP bucket | Stable error | Guard before send | Real upstream | Non-loopback | Node direct HTTPS |
|---|---|---|---|---|---:|---:|---:|
| `count_tokens_fail_closed` | policy_fail_closed | `status_403` | `formal_pool_count_tokens_profile_unapproved` | true | 0 | 0 | 0 |
| `mcp_configured_fail_closed` | policy_fail_closed | `status_403` | `formal_pool_mcp_shape_unapproved` | true | 0 | 0 | 0 |
| `non_streaming_fail_closed` | policy_fail_closed | `status_403` | `formal_pool_non_streaming_profile_unapproved` | true | 0 | 0 | 0 |
| `model_control_plane_fail_closed` | policy_fail_closed | `status_403` | `formal_pool_control_plane_unapproved` | true | 0 | 0 | 0 |
| `sonnet5_2185_fail_closed` | policy_fail_closed | `status_403` | `formal_pool_model_version_unsupported` | true | 0 | 0 | 0 |
| `sonnet5_2179_fail_closed` | policy_fail_closed | `status_403` | `formal_pool_model_version_unsupported` | true | 0 | 0 | 0 |
| `env_base_url_residue_fail_closed` | policy_fail_closed | `status_400` | `formal_pool_env_residue_verifier_failed` | true | 0 | 0 | 0 |

Positive paths:

- `2.1.197` Sonnet 5: PASS through real CC Gateway and real Go/uTLS sidecar; TLS bucket `tls-bucket:claude-code-real-oracle-2197`.
- `2.1.185` fallback non-Sonnet: PASS through real CC Gateway and real Go/uTLS sidecar; TLS bucket `tls-bucket:claude-code-real-oracle-2179`.
- `2.1.179` rollback non-Sonnet: PASS through real CC Gateway and real Go/uTLS sidecar; TLS bucket `tls-bucket:claude-code-real-oracle-2179`.

## TLS safe summary

- Real Go/uTLS sidecar binary used: true.
- Sidecar endpoint bucket: `127.0.0.1:19084`.
- Logical target `api.anthropic.com` was dial-overridden to local collector bucket `127.0.0.1:19085`.
- 2.1.197 bucket observed: `tls-bucket:claude-code-real-oracle-2197`.
- 2.1.179/2.1.185 fallback bucket observed: `tls-bucket:claude-code-real-oracle-2179`.
- Raw ClientHello persisted: false.

## Env residue validation

- `ANTHROPIC_BASE_URL` residue bucket failed closed with `formal_pool_env_residue_verifier_failed` before sidecar/upstream.
- Adapter smoke also covered Asia/Shanghai, Asia/Urumqi, and synthetic-domain/AI-keyword residue buckets with authority unaffected and fail-closed/canonical final state.
- No residue changed sidecar logical host, upstream authority, proxy, or tuple identity.

## CCH/billing/attribution assertions

- `x-anthropic-billing-*` present: false.
- raw/native CCH present: false.
- signed/no-CCH mode present: false.
- client attribution present: false.
- raw body persisted: false.

## Final counters

| Counter | Value |
|---|---:|
| Adapter smoke status | PASS |
| Adapter case count | 21 |
| Real CC smoke status | PASS |
| Real CC case count | 15 |
| Failed count | 0 |
| Real upstream request count | 0 |
| Non-loopback attempt count | 0 |
| Node direct HTTPS fallback count | 0 |
| Fail-closed upstream send count | 0 |
| Collector raw body persisted | false |

## Leak scan

Leak scan status: `PASS`. Blocking findings: `0`. One false-positive bucket was reviewed as synthetic fixture/source-summary field naming, not secret material.

## Explicit non-actions

- No production deployment.
- No live canary.
- No production traffic switch.
- No real upstream request.
- No real account credential, OAuth session, API key, cookie, billing credential, or proxy credential used.
- Ports `3012`, `3017`, `18080`, `18081` were not touched, stopped, restarted, rebound, or used.
- Server scratch/raw diagnostics were not committed.

## Rollback / cleanup status

- Existing production/rollback state was recorded and not changed.
- Plan77-owned processes were stopped by container exit; post-shutdown canary ports were not listening.
- Scratch cleanup status: `skipped_requires_user_approval` (server scratch retained; no files/directories deleted beyond user-authorized old cpaws container/image cleanup scope).

## Evidence limitations

- The Sub2API production server binary was built but not deployed. The Sub2API runtime portion of this smoke used a Plan77 canary adapter to generate server-selected tuple/attestation requests. Therefore this is a server deployed **mock smoke** proof, not production deployment proof.
- The CC Gateway portion was strengthened by running the actual built `dist/index.js` with the actual built Go/uTLS sidecar in the same loopback-only scope.
- A first real-CC diagnostic run exposed a fixture-model mismatch for fallback; root cause was an unknown non-Sonnet model fixture. The rerun used a registry-known non-Sonnet model bucket and passed. The failed diagnostic scratch file remains on server scratch but is not part of committed evidence.
