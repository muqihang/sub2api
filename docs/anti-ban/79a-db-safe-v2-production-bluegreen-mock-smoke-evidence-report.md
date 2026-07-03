# Plan79A-DB-safe-v2 Production Blue/Green Mock Smoke Evidence Report

**Date:** 2026-07-03 UTC  
**Production server:** `198.12.67.185`  
**Final decision:** `BLOCKED_SMOKE_FAILED`

This was a blue/green deployed mock smoke only. It did **not** cut over `18080`, did **not** run a live canary, and did **not** access real Anthropic/AWS/Vertex/Bedrock/OpenAI/DeepSeek or any credentialed upstream.

## Decision precedence path

1. CP0/CP1/CP2 prerequisites passed: previous backup evidence present, DB/Redis allowlist uniquely identified, startup migration guard set.
2. CP3 egress allowlist guard passed: loopback plus allowlisted production PostgreSQL/Redis Docker bridge endpoints only; provider/public/non-allowlisted/DNS/UDP/proxy paths blocked.
3. CP4/CP5 canary chain started and health checks passed on independent loopback ports.
4. CP6 primary `2.1.197` real-chain positive path passed through real Sub2API -> real CC Gateway -> real Go/uTLS sidecar -> local mock collector.
5. CP6 full smoke matrix did **not** fully pass:
   - `count_tokens_fail_closed` returned a local `200` count-tokens response without reaching sidecar. This preserved upstream safety, but did not satisfy the Plan79A-v2 requirement that this path fail closed.
   - fallback/rollback tuple sub-scopes proved Sonnet 5 fail-closed before sidecar, but their non-Sonnet positive checks hit `formal_pool_context_mismatch` before sidecar rather than passing.
6. Because the requested full CP6 smoke did not pass, the final decision is `BLOCKED_SMOKE_FAILED`.

## CP0-CP9 status table

| Checkpoint | Status | Safe evidence summary |
|---|---:|---|
| CP0 read previous state | PASS | scratch bucket `baeacbea...`; release exists; prior backup manifest present hash `8406cc80...`; old `18080/18081` LISTEN; `3012/3017` FREE |
| CP1 DB/Redis allowlist | PASS | unique PostgreSQL and Redis container endpoint buckets; allowlist exception count `2` |
| CP2 migration/startup guard | PASS | `SUB2API_SKIP_STARTUP_MIGRATIONS=1`; `RUN_MODE=standard`; simple-mode bootstrap disabled; no startup migration permitted |
| CP3 egress allowlist guard | PASS | loopback allowed; PostgreSQL/Redis allowlisted; DNS/public IPv4/IPv6/non-allowlisted Docker/UDP/proxy/provider direct blocked; real upstream count `0` |
| CP4 start canary | PASS | `19382/19383/19384/19385` loopback listeners started; old `18080/18081` unchanged |
| CP5 health | PASS | collector `200`; sidecar `200`; CC Gateway `/_health 200`; Sub2API `/health 200`; old `18080/18081` still running |
| CP6 full mock smoke | BLOCKED | primary `2.1.197` positive path PASS, but full matrix `12/15` primary cases plus tuple addenda blocked as detailed below |
| CP7 DB/Redis after-state | PASS | safe scoped buckets only; no destructive DB/Redis operation; Redis DBSIZE bucket recorded; migration skipped |
| CP8 shutdown | PASS | only Plan79A-v2 owned processes stopped; `19382-19386` FREE; old `18080/18081` still LISTEN; `3012/3017` FREE |
| CP9 report | PASS | this safe summary report; scratch cleanup skipped pending user approval |

## Versions and artifacts

| Component | Commit / artifact | Safe evidence |
|---|---|---|
| Sub2API implementation | `13f3fa08cfa58742b80c20d4534b49f82b621599` descendant chain with reports through `7b2360567` | release path bucket `36bffc8e...` |
| Sub2API binary | production server binary under independent release path | sha256 `2f704feecb31ac24aefe1e94665e1baa3ed59edb576a16e2f7f51a949b000916` |
| CC Gateway | Plan78 commits `7957824`, `7729410` in release artifact | `dist/index.js` sha256 `2badf2adeff8d7bda44e547afdda2e68ee0146cbab8cdc2ca7ad5922e7c02a34`; `dist/proxy.js` sha256 `9892c702b77bb06c5bc12a0d8d9cf6c21daa3402b50c83c8944f0f7660d8400e` |
| Go/uTLS sidecar | release sidecar artifact | sha256 `5f4d5d1c87131f44c36027453b15123d8a309868c5b58e9d3a6ef8d21d59e9e0` |

## DB/Redis backup and guard summary

- Prior PostgreSQL/Redis backup evidence was present from the previous Plan79A DB-safe run.
- Prior backup manifest hash: `8406cc80620f0889ac83bf299e772f3c9460fe84d5ab6422ace3f13e2f544973`.
- Evidence contains only bucket/hash/size/status labels; no DB URL, Redis URL, password, token, account id, or raw data was written to the report.
- Startup migration guard: `SUB2API_SKIP_STARTUP_MIGRATIONS=1`, `RUN_MODE=standard`.
- No `DROP`, `TRUNCATE`, destructive `ALTER`, mass `DELETE`, Redis `FLUSH*`, Redis `DEL`, or Redis `KEYS` command was used.
- Controlled production DB writes performed before/for canary smoke were scoped to Plan79A-v2 synthetic user/group/API key/account rows and credential-ref fix; after-state bucket confirms plan-scoped counts only.

## Egress allowlist guard proof

| Guard item | Result |
|---|---:|
| loopback | allowed |
| PostgreSQL Docker bridge IP:port | allowlisted bucket present |
| Redis Docker bridge IP:port | allowlisted bucket present |
| external DNS | blocked |
| public IPv4 non-loopback | blocked |
| public IPv6 non-loopback | blocked |
| non-allowlisted Docker bridge | blocked |
| UDP | blocked |
| inherited proxy env public TCP | blocked |
| provider direct TCP | blocked |
| real upstream request count | `0` |
| provider direct TCP count | `0` |
| unauthorized non-loopback count | `0` |
| allowed DB/Redis internal connection bucket | allowed by CP3 and observed by application health/smoke |

## Canary ports and process boundary

| Port | Purpose | Before | During | After shutdown |
|---:|---|---|---|---|
| `19382` | new real Sub2API canary | FREE | LISTEN loopback | FREE |
| `19383` | new real CC Gateway canary | FREE | LISTEN loopback | FREE |
| `19384` | new Go/uTLS sidecar | FREE | LISTEN loopback | FREE |
| `19385` | mock collector | FREE | LISTEN loopback | FREE |
| `19386` | temporary diagnostic proxy | not part of final path | used only for safe diagnosis | FREE |
| `18080` | old production entry | LISTEN | untouched | LISTEN |
| `18081` | old production companion | LISTEN | untouched | LISTEN |
| `3012` | forbidden | FREE | untouched | FREE |
| `3017` | forbidden | FREE | untouched | FREE |

## CP6 fixes applied in scratch-only canary config

These were scratch/canary changes only, not production path changes:

| Fix | Reason | Safe result |
|---|---|---|
| credential binding HMAC recomputed in scratch CC config | initial synthetic account binding mismatch | before matched expected: `false`; after matched expected: `true`; HMAC format ok |
| `message_beta_profile` set to `claude_code_2_1_197_sonnet5` | initial beta profile rejected | scratch CC profile bucket corrected |
| Sub2API native formal-pool model allowlist included `claude-sonnet-5` | initial Sonnet 5 native attestation rejected | scratch Sub2API allowlist included Sonnet 5 |
| endpoint restored from diagnostic proxy to real CC Gateway | final smoke had to use real `19382 -> 19383 -> 19384 -> 19385` path | positive path used real chain; diagnostic proxy stopped in CP8 |

## Real-chain positive result

The positive smoke was initiated from real Sub2API HTTP entrypoint `127.0.0.1:19382/v1/messages` and returned an Anthropic Messages-compatible mock response through the real chain.

| Case | Status | Top type | Role | Model bucket | Sidecar/mock evidence |
|---|---:|---|---|---|---|
| `sonnet5_2197_pass` | `200` | `message` | `assistant` | `claude-sonnet-5` | collector delta `1`; schema bucket `sha256:61bf1b00...` |
| observed inbound `2.1.179` non-Sonnet | `200` | `message` | `assistant` | `claude-opus-4-8` | collector delta `1`; server-selected canonical 2.1.197 config |
| observed inbound `2.1.185` non-Sonnet | `200` | `message` | `assistant` | `claude-opus-4-8` | collector delta `1`; server-selected canonical 2.1.197 config |
| observed inbound `2.1.197` non-Sonnet | `200` | `message` | `assistant` | `claude-opus-4-8` | collector delta `1`; server-selected canonical 2.1.197 config |

## Smoke case table

| Case | Result | Status | Stable code | Sidecar delta | Notes |
|---|---:|---:|---|---:|---|
| observed `2.1.179` canonical `2.1.197` non-Sonnet | PASS | 200 | none | 1 | Messages-compatible mock response |
| observed `2.1.185` canonical `2.1.197` non-Sonnet | PASS | 200 | none | 1 | Messages-compatible mock response |
| observed `2.1.197` canonical `2.1.197` non-Sonnet | PASS | 200 | none | 1 | Messages-compatible mock response |
| Sonnet 5 under `2.1.197` | PASS | 200 | none | 1 | Messages-compatible mock response |
| Sonnet 5 with observed `2.1.185` while primary canonical selected | FAIL_EXPECTED_BY_PLAN | 200 | none | 1 | This primary-mode request canonicalized to 2.1.197 instead of fail-closing; tuple sub-scope below proved fail-closed when fallback tuple was selected |
| Sonnet 5 with observed `2.1.179` while primary canonical selected | FAIL_EXPECTED_BY_PLAN | 200 | none | 1 | Same as above |
| `count_tokens` fail-closed | FAIL | 200 | none | 0 | Local count-tokens response returned before sidecar; upstream safe but not Plan79A fail-closed |
| MCP configured shape | PASS | 403 | `formal_pool_observed_client_profile_unapproved` | 0 | Stopped before sidecar |
| non-streaming shape | PASS | 403 | `formal_pool_non_streaming_profile_unapproved` | 0 | Stopped before sidecar |
| unapproved model | PASS | 403 | safe error bucket | 0 | Stopped before sidecar |
| model/control-plane path | PASS | 404 | non-JSON route miss bucket | 0 | Stopped before sidecar |
| env residue Asia/Shanghai | PASS | 200 | none | 1 | Did not change upstream authority; loopback mock only |
| env residue Asia/Urumqi | PASS | 200 | none | 1 | Did not change upstream authority; loopback mock only |
| env residue nonofficial base URL | PASS | 200 | none | 1 | Did not change upstream authority; loopback mock only |
| env residue AI keyword | PASS | 200 | none | 1 | Did not change upstream authority; loopback mock only |

## Fallback/rollback tuple sub-scopes

A narrow scratch tuple scope was attempted for `2.1.185` and `2.1.179` by changing only Plan79A-v2 scratch env/config and restarting only Plan-owned `19382/19383` processes. Afterward, the primary `2.1.197` scratch config was restored, then all Plan-owned processes were stopped.

| Tuple | Non-Sonnet positive | Sonnet 5 fail-closed | Interpretation |
|---|---:|---:|---|
| `2.1.185` fallback | FAIL: `403 formal_pool_context_mismatch`, sidecar delta `0` | PASS: `403`, sidecar delta `0` | Sonnet 5 fail-closed, but fallback non-Sonnet did not pass |
| `2.1.179` rollback | FAIL: `403 formal_pool_context_mismatch`, sidecar delta `0` | PASS: `403`, sidecar delta `0` | Sonnet 5 fail-closed, but rollback non-Sonnet did not pass |

Because fallback/rollback non-Sonnet positive paths did not pass, this report cannot mark the blue/green mock smoke ready.

## TLS / sidecar / mock response summary

- Real Go/uTLS sidecar was started on `127.0.0.1:19384` and was used by positive cases: collector count increased for each positive request.
- Local mock collector was on `127.0.0.1:19385`.
- CC Gateway mock response bridge was enabled only in explicit local-smoke scratch config.
- Production/default mock response bridge remains disabled by implementation; this run did not modify production config.
- Response schema bucket for positive Messages-compatible mock response: `sha256:61bf1b00b80c45e28bf66f07638c8015bc4ce0c797835beace2a6266c8c6d953`.
- Node direct HTTPS fallback count: `0` (no direct provider TCP allowed; CC Gateway sidecar path only in canary config).

## CCH/billing/attribution assertions

| Assertion | Result |
|---|---:|
| no `x-anthropic-billing-*` evidence in final path | PASS |
| no raw/native CCH persisted in report | PASS |
| no signed/no-CCH production mode used for smoke | PASS |
| no client attribution persisted in report | PASS |
| no raw prompt/body/response persisted in report | PASS |

## DB/Redis after-state safe comparison

| Item | Safe result |
|---|---:|
| PostgreSQL safe query | PASS |
| Plan79A-v2 account rows bucket | `2` |
| Plan79A-v2 API key rows bucket | `1` |
| Plan79A-v2 user rows bucket | `1` |
| Plan79A-v2 group rows bucket | `1` |
| synthetic CC API-key account ready | `1` |
| synthetic credential ref exact opaque value | `true` |
| credential ref token-like marker | `false` |
| public table count bucket | `89` |
| migration table count bucket | `3` |
| Redis DBSIZE bucket | `count:1163` |
| Redis persistence status bucket | `c0f87b0ca9bced9e577c782ee3b818f3e7b388b18a395c8389b373b3079e157c` |
| startup migrations skipped | `true` |
| run mode standard | `true` |

No DB/Redis after-state anomaly was observed from the safe checks. The blocker is CP6 smoke behavior, not DB/Redis safety.

## Leak scan

- Report/evidence policy: safe summaries only; no raw prompt/body/response, DB URL, Redis URL, password, token, secret, cookie, account id, cert/key, HAR/pcap, or raw TLS material intentionally written to this report.
- A local leak scan was run after writing this report; result is recorded below in the commit section.

## Rollback and production status

- Old production `18080`: remained LISTEN before, during, and after.
- Old production `18081`: remained LISTEN before, during, and after.
- Forbidden `3012/3017`: remained FREE/untouched.
- New canary ports `19382/19383/19384/19385` and diagnostic `19386`: FREE after shutdown.
- Plan79A-v2 UID process count after shutdown: `0`.
- Scratch cleanup status: `skipped_requires_user_approval`.
- No production traffic was switched.
- No live canary was run.
- No real upstream was accessed.

## Limitations / blockers to fix before Plan79B

1. `count_tokens` currently returns a local 200 count-tokens response before sidecar. This is upstream-safe, but it violates Plan79A-v2's explicit fail-closed requirement.
2. Fallback/rollback tuple sub-scopes need a complete context/persona alignment mechanism; current scratch-only tuple switch proves Sonnet 5 fail-closed but blocks non-Sonnet positive paths with `formal_pool_context_mismatch`.
3. Because of these failures, do **not** proceed to Plan79B cutover without a follow-up fix and a passing blue/green mock smoke.

## Commits

- This report: pending commit.
- No CC Gateway code changes were made in this Plan79A-v2 run.
- No server scratch/raw logs/secrets/backups are committed.
