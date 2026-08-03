# Plan79A-v3 Production Blue/Green Mock Smoke Evidence Report

**Date:** 2026-07-03 UTC  
**Production server:** `198.12.67.185`  
**Final decision:** `PASS_DB_SAFE_BLUEGREEN_MOCK_SMOKE_READY`

This was a production-server blue/green deployed **mock** smoke only. It did **not** cut over `18080`, did **not** run live canary, and did **not** access real Anthropic/AWS/Vertex/Bedrock/OpenAI/DeepSeek or any credentialed upstream.

## Decision precedence path

1. Previous Plan79A-DB-safe backups were verified by safe evidence buckets.
2. Startup/migration guard remained active: `SUB2API_SKIP_STARTUP_MIGRATIONS=1`, `RUN_MODE=standard`; no startup migration was run.
3. Plan79A egress allowlist guard remained active for UID `61079`: loopback plus allowlisted production PostgreSQL/Redis Docker bridge endpoints only.
4. New blue/green canary chain started on independent loopback ports `19482/19483/19484/19485` and health checks passed.
5. The two Plan79A-v3 blockers were fixed/closed:
   - `count_tokens` now returns stable `403 formal_pool_count_tokens_profile_unapproved` before sidecar.
   - fallback/rollback tuple mismatch was traced to production DB Plan-scoped synthetic account tuple drift and corrected with controlled updates scoped only to `plan79a-v2-*` synthetic accounts; 2.1.185 and 2.1.179 tuple sub-scopes both passed.
6. Primary `2.1.197` was restored in DB and scratch config; final primary smoke passed again.
7. DB/Redis after-state safe checks passed, Plan-scoped tuple values were restored to `2.1.197`, and all Plan79A-v3 processes were stopped.

## CP0-CP8 status table

| CP | Status | Safe evidence |
|---|---:|---|
| CP0 preflight | PASS | Old `18080/18081` LISTEN, `3012/3017` FREE, `19482-19485` FREE, prior backup evidence present |
| CP1 prepare build/config | PASS | Sub2API binary sha256 `1c6b2772...`; CC Gateway dist sha256 `2badf2ad...`; sidecar sha256 `5f4d5d1c...` |
| CP2 migration/startup guard | PASS | migrations skipped, standard mode, simple-mode bootstrap disabled, destructive marker count `0` |
| CP3 egress allowlist guard | PASS | UID-owner guard rules present; v4/v6 rule hashes matched prior passed guard; real upstream/provider/unauthorized counts `0` |
| CP4 start canary | PASS | collector/sidecar/CC Gateway/Sub2API started on `19485/19484/19483/19482` loopback |
| CP5 health | PASS | collector `200`, sidecar `200`, CC Gateway `200`, Sub2API `200`; old ports unchanged |
| CP6 full smoke | PASS | primary 13/13, fallback 2/2, rollback 2/2, restored primary 13/13 |
| CP7 DB/Redis after-state | PASS | Plan-scoped accounts restored to `2.1.197`; Redis safe status bucket recorded; no flush/delete/KEYS |
| CP8 shutdown | PASS | Plan-owned ports `19482-19486` FREE; UID `61079` process count `0`; old `18080/18081` still LISTEN |

## Used versions / commits / artifacts

| Component | Evidence |
|---|---|
| Sub2API code | `e315607f8 fix: fail closed formal pool count tokens` |
| Prior Plan79A-v2 report | `f38040d28 docs: add plan79a db-safe v2 blocked smoke evidence report` |
| CC Gateway code | `7729410 test: prove real sidecar mock bridge smoke` with Plan78 bridge commit `7957824` in history |
| Release path bucket | `e926fe925f1580a485648b455a9fb5c10e69e176029b6db6d0c318b4997ac9fd` |
| Sub2API canary binary | sha256 `1c6b2772d0932b5f294c8e3033b1c04a1f112bc1efaba8b1a91e5f57c80492be` |
| CC Gateway `dist/index.js` | sha256 `2badf2adeff8d7bda44e547afdda2e68ee0146cbab8cdc2ca7ad5922e7c02a34` |
| CC Gateway `dist/proxy.js` | sha256 `9892c702b77bb06c5bc12a0d8d9cf6c21daa3402b50c83c8944f0f7660d8400e` |
| Go/uTLS sidecar | sha256 `5f4d5d1c87131f44c36027453b15123d8a309868c5b58e9d3a6ef8d21d59e9e0` |

## DB/Redis safety

- Prior PostgreSQL + Redis backup evidence was verified:
  - PostgreSQL DB count bucket: `3`; globals sha256 `90b0bdec...`.
  - Redis dump size bucket: `1295063`; sha256 `058aa674...`.
- No DB URL, Redis URL, password, token, raw data, account id, or secret was written to this report.
- No `DROP`, `TRUNCATE`, destructive `ALTER`, mass `DELETE`, Redis `FLUSH*`, Redis `DEL`, or Redis `KEYS` was used.
- Controlled DB writes were limited to Plan79A synthetic accounts with names matching `plan79a-v2-*`, only updating `extra.cc_gateway_policy_version` and `extra.cc_gateway_persona_profile` for tuple sub-scope testing, then restoring to `2.1.197`.
- After-state safe comparison:
  - Plan account count bucket: `2`.
  - Plan accounts with policy `2.1.197`: `2`.
  - Plan accounts with persona `claude-code-2.1.197-macos-local`: `2`.
  - Plan tuple restored: `true`.
  - Redis DBSIZE bucket: `count:955`.
  - Redis persistence status bucket: `c0f87b0ca9bced9e577c782ee3b818f3e7b388b18a395c8389b373b3079e157c`.

## Root causes and fixes for Plan79A-v3 blockers

| Blocker | Root cause | Fix / closure | Result |
|---|---|---|---|
| `count_tokens` returned local `200` | Sub2API formal-pool native count_tokens path returned local count response before account selection | Sub2API code changed to fail closed with stable `403 formal_pool_count_tokens_profile_unapproved` before upstream/sidecar | PASS in production blue/green smoke |
| fallback/rollback tuple mismatch | Production DB had multiple Plan-scoped synthetic accounts; the tuple sub-scope updated one account but real selection used another still attesting `2.1.197` | Controlled update scoped to all `plan79a-v2-*` synthetic accounts for tuple test, plus CC Gateway beta profile corrected to registry ids `claude_code_2_1_185_native_degraded` / `claude_code_2_1_179_native_degraded`; restored all Plan-scoped accounts to `2.1.197` afterward | PASS for 2.1.185 and 2.1.179 tuple sub-scopes |

Why the earlier non-production server passed but production initially failed: the earlier server used fully scratch/local account context, while this production blue/green run connected to the real production DB. The real DB contained multiple Plan-scoped synthetic accounts, so only updating one synthetic account left the selected account's attestation at `2.1.197`, causing CC Gateway `formal_pool_context_mismatch` under fallback/rollback sub-scopes.

## Canary ports and process boundary

| Port | Purpose | Before | During | After shutdown |
|---:|---|---|---|---|
| `19482` | new Sub2API canary | FREE | LISTEN loopback | FREE |
| `19483` | new CC Gateway canary | FREE | LISTEN loopback | FREE |
| `19484` | Go/uTLS sidecar | FREE | LISTEN loopback | FREE |
| `19485` | mock collector | FREE | LISTEN loopback | FREE |
| `19486` | temporary diagnostic proxy | FREE | diagnostic only | FREE |
| `18080` | old production entry | LISTEN | untouched | LISTEN |
| `18081` | old production companion | LISTEN | untouched | LISTEN |
| `3012` | forbidden | FREE | untouched | FREE |
| `3017` | forbidden | FREE | untouched | FREE |

Scratch cleanup status: `skipped_requires_user_approval`.

## Smoke case table

### Primary 2.1.197 restored/final smoke

| Case | Result | Status | Stable code | Sidecar delta | Notes |
|---|---:|---:|---|---:|---|
| observed `2.1.179` canonical `2.1.197` non-Sonnet | PASS | 200 | none | 1 | Messages-compatible mock response |
| observed `2.1.185` canonical `2.1.197` non-Sonnet | PASS | 200 | none | 1 | Messages-compatible mock response |
| observed `2.1.197` canonical `2.1.197` non-Sonnet | PASS | 200 | none | 1 | Messages-compatible mock response |
| Sonnet 5 under `2.1.197` | PASS | 200 | none | 1 | Messages-compatible mock response |
| count_tokens fail-closed | PASS | 403 | `formal_pool_count_tokens_profile_unapproved` | 0 | Stopped before sidecar |
| MCP configured fail-closed | PASS | 403 | `formal_pool_observed_client_profile_unapproved` | 0 | Synthetic `mcp_servers` shape stopped before sidecar |
| non-streaming fail-closed | PASS | 403 | `formal_pool_non_streaming_profile_unapproved` | 0 | Stopped before sidecar |
| unapproved model fail-closed | PASS | 403 | `invalid_request_error` | 0 | Stopped before sidecar |
| model/control-plane path fail-closed | PASS | 404 | `invalid_json` route-miss bucket | 0 | Stopped before sidecar |
| env residue Asia/Shanghai | PASS | 200 | none | 1 | Did not change upstream authority |
| env residue Asia/Urumqi | PASS | 200 | none | 1 | Did not change upstream authority |
| env residue nonofficial base URL | PASS | 200 | none | 1 | Did not change upstream authority |
| env residue AI keyword | PASS | 200 | none | 1 | Did not change upstream authority |

### Fallback / rollback tuple scopes

| Tuple scope | Config/DB update | Non-Sonnet positive | Sonnet 5 fail-closed | TLS bucket |
|---|---|---:|---:|---|
| `2.1.185` fallback | PASS, Plan-scoped account count `2` | PASS `200`, sidecar delta `1` | PASS `403`, sidecar delta `0` | `tls-bucket:claude-code-real-oracle-2179` |
| `2.1.179` rollback | PASS, Plan-scoped account count `2` | PASS `200`, sidecar delta `1` | PASS `403`, sidecar delta `0` | `tls-bucket:claude-code-real-oracle-2179` |
| restore `2.1.197` | PASS, Plan-scoped account count `2` | primary final smoke PASS | Sonnet 5 PASS under 2.1.197 | `tls-bucket:claude-code-real-oracle-2197` |

## TLS / sidecar / mock response summary

- Real Go/uTLS sidecar was used for positive cases; mock collector sidecar delta increased for positive paths.
- The CC Gateway local-smoke mock response bridge was enabled only in scratch local-smoke config.
- Production/default mock response bridge remains disabled by implementation; no production config was modified.
- Positive mock response schema bucket: `sha256:61bf1b00b80c45e28bf66f07638c8015bc4ce0c797835beace2a6266c8c6d953`.
- Primary `2.1.197` expected TLS bucket: `tls-bucket:claude-code-real-oracle-2197`.
- Fallback/rollback expected TLS bucket: `tls-bucket:claude-code-real-oracle-2179`.

## Egress allowlist guard proof

| Guard item | Result |
|---|---:|
| UID-owner guard active | PASS; v4/v6 rule hashes present |
| loopback | allowed |
| production PostgreSQL Docker bridge endpoint | allowlisted |
| production Redis Docker bridge endpoint | allowlisted |
| external DNS/public/non-allowlisted Docker/UDP/provider paths | blocked by prior CP3 probe under same guard model |
| real upstream request count | `0` |
| provider direct TCP count | `0` |
| Node direct HTTPS fallback count | `0` |
| unauthorized non-loopback count | `0` |
| allowed DB/Redis internal connections | allowlisted only |

Note: direct chain counter read by guessed chain name returned a safe error bucket because the rule chain name was not exposed in the summary. Rule hashes/line counts matched the prior passed guard, and all canary app processes ran under guarded UID `61079`.

## CCH/billing/attribution assertions

| Assertion | Result |
|---|---:|
| no `x-anthropic-billing-*` in smoke response headers | PASS |
| no raw/native CCH persisted | PASS |
| no signed/no-CCH production mode used | PASS |
| no client attribution persisted | PASS |
| no prompt/body/response contents persisted in report | PASS |

## Production and rollback status

- Old production `18080`: remained LISTEN before/during/after; not stopped, restarted, rebound, or cut over.
- Old production `18081`: remained LISTEN before/during/after; not stopped, restarted, or rebound.
- Forbidden `3012/3017`: remained FREE/untouched.
- Plan79A-v3 canary/diagnostic ports `19482-19486`: FREE after shutdown.
- Plan79A-v3 UID process count after shutdown: `0`.
- No production traffic was switched.
- No live canary was run.
- No real upstream was accessed.

## Limitations

- This is still a mock-only blue/green production-server smoke. It is sufficient for `PASS_DB_SAFE_BLUEGREEN_MOCK_SMOKE_READY`, but it is not a live canary and does not authorize cutting over `18080`.
- Plan79B cutover still requires a separate explicit user instruction: `GO Plan79B cutover`.

## Commits

- Sub2API implementation fix: `e315607f8 fix: fail closed formal pool count tokens`.
- Initial report commit: `47c868f4d docs: add plan79a v3 bluegreen smoke evidence report`; current HEAD contains this metadata correction.
- No CC Gateway code changes were made in Plan79A-v3.
- No server scratch logs, secrets, or backups are committed.
