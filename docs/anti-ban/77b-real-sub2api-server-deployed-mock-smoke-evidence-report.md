# 77b - Real Sub2API Server Deployed Mock Smoke Addendum Evidence Report

**Final decision:** BLOCKED_REAL_SUB2API_CC_GATEWAY_SIDECAR_RESPONSE_SHAPE_GAP

**Scope:** Plan77b deployed mock smoke addendum only. This run attempted the requested real chain:

real Sub2API server canary -> real CC Gateway `dist/index.js` -> real Go/uTLS sidecar -> local mock collector.

This was not a production deployment, not a live canary, and did not access any real Anthropic/AWS/Vertex/Bedrock/OpenAI/DeepSeek or credentialed upstream. The smoke did **not** pass: the real Sub2API `/v1/messages` entrypoint could start and call the real CC Gateway, but the real Go/uTLS sidecar implementation returns a TLS verification JSON/bucket response rather than an Anthropic Messages-compatible mock response body. Sub2API therefore failed the positive `/v1/messages` path before a complete user-visible mock message response could be proven.

**Report generated:** 2026-07-02T08:34:51Z  
**Server scratch root:** `/tmp/plan77b-real-sub2api-server-smoke-20260702T062719Z`  
**Scratch cleanup status:** `skipped_requires_user_approval` (scratch retained; no cleanup/delete performed).

## Inputs and source lock

| Component | Required / input commit | Used commit / artifact | Evidence |
|---|---|---|---|
| Plan77 report | `3c8b5a1c2` | local Sub2API HEAD `3c8b5a1c2` before this report | present locally |
| Sub2API Plan76 implementation | `13f3fa08cfa58742b80c20d4534b49f82b621599` | server source lock used descendant `15e53f8b3` from Plan77 scratch reuse | descendant: true |
| CC Gateway | `d3f1920` descendant of `fdf29bd062b378234ce3a4b9b17c9c8e6b2016d7` | server source lock used `d3f1920` | descendant: true |
| Sub2API production server binary | built artifact from Plan77 scratch reuse | sha bucket `e4a441b38dd93354` | scratch/build only, not production path |
| CC Gateway `dist/index.js` | built artifact from Plan77 scratch reuse | sha bucket `2badf2adeff8d7bd` | real `dist/index.js` copied to Plan77b scratch |
| Go/uTLS sidecar | built artifact from Plan77 scratch reuse | sha bucket `e39fd967433f4918` | real sidecar binary |

No artifact was installed into a production path.

## CP0-CP6 status table

| CP | Status | Safe evidence |
|---|---|---|
| CP0 preflight | PASS | forbidden ports `3012/3017/18080/18081` not listening; Plan77b ports `19182/19183/19184/19185` not listening; production change planned false |
| CP1 build/prepare | PASS | reused Plan77 scratch-built real binaries in Plan77b scratch path; no production path modification |
| CP2 egress guard | PASS | Docker `--network none` same-scope guard; DNS/IPv4/IPv6 non-loopback/UDP/provider direct TCP blocked; proxy env cleared |
| CP3 start real chain | PASS_WITH_DIAGNOSTIC_FIXES | loopback-only real Sub2API, real CC Gateway, real sidecar, mock collector started inside isolated runner; scratch config fixes were needed for beta profile, model alias, and sidecar control token consistency |
| CP4 health/readiness | PASS | collector, sidecar, CC Gateway, Sub2API health checks passed during runner startup |
| CP5 minimal real-chain smoke | BLOCKED | positive `/v1/messages` path did not return a Messages-compatible mock response through real Sub2API; see blocker analysis |
| CP6 shutdown/report | PASS | Plan77b-owned runner exited; Plan77b/forbidden ports not listening after; leak scan safe evidence PASS |

## Canary/mock ports

| Component | Port | Bind scope |
|---|---:|---|
| Real Sub2API server canary | 19182 | loopback inside isolated `docker --network none` runner |
| Real CC Gateway canary | 19183 | loopback inside isolated `docker --network none` runner |
| Go/uTLS sidecar canary | 19184 | loopback inside isolated `docker --network none` runner |
| mock collector | 19185 | loopback inside isolated `docker --network none` runner |

The host did not bind/publish these ports. Post-run host status for `19182/19183/19184/19185` and forbidden ports was not listening.

## Forbidden ports before/after

| Port | Before | After |
|---:|---|---|
| 3012 | not listening | not listening |
| 3017 | not listening | not listening |
| 18080 | not listening | not listening |
| 18081 | not listening | not listening |
| 19182 | not listening | not listening |
| 19183 | not listening | not listening |
| 19184 | not listening | not listening |
| 19185 | not listening | not listening |

## Egress guard proof

| Guard item | Result |
|---|---|
| DNS | blocked |
| IPv4 non-loopback | blocked |
| IPv6 non-loopback | blocked |
| UDP | blocked |
| inherited proxy env | cleared |
| provider direct TCP | blocked |
| real upstream request count | 0 |
| non-loopback attempt count | 0 |
| scope | `docker_anchor_network_none_shared_netns` |

## Real-chain smoke case table

Latest fail-closed-first diagnostic summary:

| Case | Status | HTTP bucket | Stable error | Collector delta | Guard before upstream/sidecar |
|---|---|---|---|---:|---|
| `count_tokens_fail_closed` | PASS | 403 | `formal_pool_count_tokens_profile_unapproved` | 0 | true |
| `mcp_configured_fail_closed` | FAIL_MASKED | 503 | `api_error` | 0 | true |
| `non_streaming_fail_closed` | FAIL_MASKED | 503 | `api_error` | 0 | true |
| `model_control_plane_fail_closed` | FAIL_MASKED | 404 | `non_json` | 0 | true |
| `env_base_url_residue_fail_closed` | FAIL_MASKED | 503 | `api_error` | 0 | true |
| `canonical_inbound_2179_to_2197` | FAIL | 503 | `api_error` | 0 | true |
| `canonical_inbound_2185_to_2197` | FAIL | 503 | `api_error` | 0 | true |
| `canonical_inbound_2197_to_2197` | FAIL | 503 | `api_error` | 0 | true |
| `sonnet5_2197_pass` | FAIL | 503 | `api_error` | 0 | true |

Earlier positive-first diagnostic after scratch config corrections reached a more specific positive-path blocker:

| Case group | Observed blocker | Notes |
|---|---|---|
| canonical positive `/v1/messages` and Sonnet 5 positive | `upstream_error` at HTTP 502 | real Sub2API called real CC Gateway; CC Gateway sidecar branch returned a TLS verification response shape, not an Anthropic Messages mock response body |
| count_tokens | `formal_pool_count_tokens_profile_unapproved` | fail-closed before collector/upstream, PASS |
| later cases in same account/session | degraded/masked 503 | not used as proof of those policy closures |

## Blocker analysis

The Plan77b positive path requires a complete mock user-visible response through the real Sub2API server canary. The real sidecar in the used CC Gateway commit is a TLS ClientHello verifier: it authenticates control input, emits a uTLS ClientHello to the dial override target, verifies the safe TLS summary bucket, then returns a small verification JSON response with `x-cc-egress-tls-summary-bucket`. It does not proxy the Anthropic Messages HTTP request to the mock collector and does not return a Messages-compatible mock body.

That behavior was sufficient for Plan77's real-CC smoke because Plan77 tested CC Gateway + sidecar directly and treated the sidecar TLS bucket response as the mocked upstream response. It is not sufficient for Plan77b's stricter real Sub2API server entrypoint because Sub2API expects an Anthropic Messages-compatible response body from CC Gateway.

Therefore this addendum cannot responsibly claim PASS without either:

1. a production-code change, tested first, that makes the sidecar/CC Gateway local-only mock mode return a Messages-compatible mock response while preserving real uTLS sidecar proof; or
2. a different already-existing real Sub2API entrypoint that intentionally accepts the current sidecar verification response shape as a successful mock response.

No such already-existing entrypoint was proven in this run. I did not hard-promote or relabel the 502/503 results as success.

## Scratch diagnostic corrections applied

The following were scratch-runner configuration fixes only, not repository code changes:

| Fix | Why |
|---|---|
| `message_beta_profile` changed to `claude_code_2_1_197_sonnet5` | initial config used an obsolete/unknown 2.1.197 beta profile and triggered `persona_quarantine_unknown_beta` |
| positive model alias changed to `claude-sonnet-5` | initial dated alias was not in the 2.1.197 persona registry and triggered `persona_reject_untrusted_model` |
| sidecar `control_token` made consistent between CC config and sidecar env | initial mismatch triggered `egress_tls_sidecar_rejected` |
| fail-closed-first diagnostic attempted | avoided interpreting positive-path account degradation as policy evidence |

## TLS safe summary

| Item | Result |
|---|---|
| real Go/uTLS sidecar binary started | true |
| sidecar health during run | PASS |
| logical target host | `api.anthropic.com` |
| dial override target | loopback mock collector bucket `127.0.0.1:19185` |
| raw ClientHello persisted | false |
| final real-Sub2API positive TLS bucket through user-visible response | not proven due response-shape blocker |

The prior Plan77 report remains the latest PASS evidence for real CC Gateway + real sidecar TLS bucket, but Plan77b does not extend that to a full real Sub2API server positive response.

## Plan72 env residue validation

| Check | Result |
|---|---|
| non-official base URL residue into upstream authority | not observed; guarded before collector/upstream in diagnostic |
| residue changed logical host / tuple / proxy identity | not observed |
| full Asia/Shanghai and Asia/Urumqi real-server proof | not completed in Plan77b due positive-path blocker |

Plan76/Plan77 evidence still covers these in adapter/real-CC scopes, but Plan77b real Sub2API server scope remains blocked.

## Plan76 control-plane fail-closed validation

| Blocker path | Plan77b result |
|---|---|
| count_tokens | PASS: `formal_pool_count_tokens_profile_unapproved`, collector delta 0 |
| MCP configured shape | inconclusive in Plan77b final run; subsequent case masked by account degradation after fail-closed |
| non-streaming shape | inconclusive in Plan77b final run; subsequent case masked by account degradation after fail-closed |
| model/control-plane | inconclusive in Plan77b final run; 404/non-JSON path did not provide the expected stable code |
| Sonnet 5 2.1.197 positive | blocked by sidecar/response shape gap |

## CCH/billing/attribution validation

| Assertion | Result |
|---|---|
| `x-anthropic-billing-*` present | false |
| raw/native CCH present | false |
| signed/no-CCH mode present | false |
| client attribution present | false |

## Counters

| Counter | Value |
|---|---:|
| real upstream request count | 0 |
| Node direct HTTPS fallback count | 0 |
| non-loopback attempt count | 0 |
| collector HTTP request count | 0 |
| raw body persisted to report | 0 |

## Leak scan

Leak scan status: PASS. Blocking findings: 0. Scope was safe evidence only; raw scratch logs, raw request/response bodies, raw TLS/pcap/HAR, secrets, cookies, account ids, cert/key material, and native dumps were not committed.

## Rollback / production baseline

- Production/live canary planned: false.
- Production paths modified: false.
- No production service was stopped, restarted, rebound, or deployed.
- Old production/rollback state was not changed by Plan77b.
- Plan77b-owned processes were stopped by runner exit.

## Explicit non-actions

- No production deployment.
- No live canary.
- No production traffic switch.
- No real upstream access.
- No real OAuth/API key/session cookie/account/billing/proxy credentials used.
- Ports `3012`, `3017`, `18080`, `18081` were not touched, stopped, restarted, rebound, or used.
- No scratch/raw logs/native dumps/secrets were committed.

## Limitations and required next step before live canary

Plan77b did not meet the intended PASS criteria. Before live canary or production deployment, close `BLOCKED_REAL_SUB2API_CC_GATEWAY_SIDECAR_RESPONSE_SHAPE_GAP` with test-first implementation or a proven existing configuration path. The likely engineering closure is a local-only/mock-only CC Gateway/sidecar mode that preserves real uTLS ClientHello proof and also returns a safe Anthropic Messages-compatible mock response to Sub2API, with tests proving:

- no real upstream request,
- no Node direct HTTPS fallback,
- no non-loopback egress,
- sidecar selected the server-selected tuple TLS profile,
- Sub2API receives and returns a valid mock Messages response,
- fail-closed cases stop before sidecar/upstream send.
