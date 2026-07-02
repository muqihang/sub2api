# 79A - Production Blue/Green Deploy + Real Server Mock Smoke Evidence Report

**Final decision:** BLOCKED_DB_REDIS_STARTUP_WRITE_GUARD

**Scope:** Plan79A only. This was a production-server blue/green preparation and deployed mock-smoke preflight. It did **not** cut `18080`, did **not** run live canary, and did **not** access real Anthropic/AWS/Vertex/Bedrock/OpenAI/DeepSeek or credentialed upstream.

**Production server:** `198.12.67.185` via SSH port `57275`
**Server scratch:** `/tmp/plan79-production-bluegreen-20260702T125510Z/`
**New release path:** `/opt/plan79/releases/20260702T125510Z/`
**Scratch cleanup status:** `skipped_requires_user_approval`
**Report generated:** 2026-07-02T13:40:00Z

## Decision precedence path

Plan79A required stopping if the new environment would write PostgreSQL/Redis or run migrations at startup. CP1 code audit/build evidence showed the real Sub2API server startup path initializes Ent/PostgreSQL and can write at startup:

- `InitEnt` applies startup migrations unless `SUB2API_SKIP_STARTUP_MIGRATIONS` is set.
- `ensureBootstrapSecrets` persists a configured JWT secret if absent, or generates and persists one if unset.
- `RunModeSimple` can seed default groups/admin concurrency.

Because Plan79A explicitly says: if application startup will write DB/Redis, stop and report, CP2-CP5 were not executed. No mock canary chain was started.

## CP0-CP7 status table

| CP | Status | Safe evidence |
|---|---|---|
| CP0 read-only production inventory | PASS | scratch created; host/time/OS/disk/listener/service manager/config-source buckets recorded; DB/Redis config source recorded as key-set hashes only |
| CP1 deploy/build second isolated release artifacts | PASS | sources unpacked to `/opt/plan79/releases/20260702T125510Z`; CC Gateway/Sub2API/sidecar artifacts built in new release only |
| CP2 start new mock-only canary | BLOCKED_NOT_RUN | stopped before process start due startup DB/migration write guard |
| CP3 egress guard | NOT_RUN | no canary process scope started |
| CP4 health checks | NOT_RUN | no canary process scope started |
| CP5 full mock smoke | NOT_RUN | no canary process scope started |
| CP6 stop/verify unchanged | PASS | no Plan79A-owned PIDs; no Plan79A containers; canary ports free; forbidden ports unchanged |
| CP7 report | PASS | this report written with safe summaries only |

## Source and commit lock

| Component | Required | Used |
|---|---|---|
| Sub2API Plan76 implementation | `13f3fa08cfa58742b80c20d4534b49f82b621599` | ancestor of `c448fdbbbe92` |
| Sub2API Plan77 report | `3c8b5a1c2` | ancestor of `c448fdbbbe92` |
| Sub2API Plan78 report | `c448fdbbb` | `c448fdbbbe92` tarball |
| CC Gateway mock bridge | `7957824` | ancestor of `7729410512bf` |
| CC Gateway real sidecar proof | `7729410` | `7729410512bf` tarball |

Tracked-source tarballs only were transferred; untracked `.codegraph/`, tmp files, local `.env`, scratch, and secrets were not included.

## CP0 production baseline

| Item | Safe result |
|---|---|
| OS bucket | `ubuntu-24.04` |
| disk root | `size=144G, used=46G, avail=92G, pct=34%` |
| TCP listener count | `10` |
| UDP listener count | `3` |
| service manager | systemd present; docker present; docker compose present; pm2 present with 0 processes |
| old production candidate | docker containers included `chelingxi-sub2api` and `chelingxi-cc-gateway` |
| DB/Redis config source | docker env key-set hashes and config path buckets recorded; no values printed |
| direct DB/Redis connection | false |
| production DB/Redis write | false |
| migration executed | false |

## Forbidden and canary ports

| Port | Before | After |
|---:|---|---|
| 18080 | LISTEN by `docker-proxy`, pid bucket `74b827...f7d0` | LISTEN by `docker-proxy`, same pid bucket |
| 3012 | FREE | FREE |
| 3017 | FREE | FREE |
| 18081 | LISTEN by `docker-proxy`, pid bucket `ca0098...9361` | LISTEN by `docker-proxy`, same pid bucket |
| 19382 | FREE | FREE |
| 19383 | FREE | FREE |
| 19384 | FREE | FREE |
| 19385 | FREE | FREE |

No Plan79A process bound any port.

## New release artifacts

| Artifact | Hash |
|---|---|
| Sub2API source tarball | `94b4d07a85ff6fc826e2597e933714d3cae8f5e0409b2295a0d81b2147bb6358` |
| CC Gateway source tarball | `488c5ea4c991485ed365b81511168e26a4604c150d4f93eef0ed3e1f3abc31c1` |
| CC Gateway `dist/index.js` | `2badf2adeff8d7bda44e547afdda2e68ee0146cbab8cdc2ca7ad5922e7c02a34` |
| CC Gateway `dist/proxy.js` | `9892c702b77bb06c5bc12a0d8d9cf6c21daa3402b50c83c8944f0f7660d8400e` |
| Go/uTLS sidecar binary | `5f4d5d1c87131f44c36027453b15123d8a309868c5b58e9d3a6ef8d21d59e9e0` |
| Sub2API server binary | `2f704feecb31ac24aefe1e94665e1baa3ed59edb576a16e2f7f51a949b000916` |

Build happened only in `/opt/plan79/releases/20260702T125510Z` and scratch. No production path or service config was modified.

## DB/Redis untouched and no-migration proof

| Assertion | Result |
|---|---|
| production PostgreSQL direct connection | false |
| production Redis direct connection | false |
| production DB/Redis write | false |
| migration command run | false |
| Sub2API process started | false |
| startup migration/write risk identified before CP2 | true |
| stopped before canary process start | true |

The blocker is conservative: Plan79A forbids starting if startup would write DB/Redis. The real Sub2API server startup path could write DB, so the smoke did not proceed.

## Egress/upstream/canary results

| Counter / action | Value |
|---|---:|
| real upstream request count | 0 |
| Node direct HTTPS fallback count | 0 |
| non-loopback attempt count | 0 |
| live canary requests | 0 |
| production traffic cutover | 0 |
| Plan79A-owned running containers | 0 |
| Plan79A-owned pid files | 0 |

CP3 egress guard was not executed because the canary chain was not started.

## Smoke case table

| Case group | Status | Reason |
|---|---|---|
| health/readiness | NOT_RUN | blocked before canary start |
| canonical 2.1.197 primary | NOT_RUN | blocked before canary start |
| Sonnet 5 policy | NOT_RUN | blocked before canary start |
| Plan76 fail-closed paths | NOT_RUN | blocked before canary start |
| Plan72 env residue | NOT_RUN | blocked before canary start |
| TLS sidecar bucket | NOT_RUN | blocked before canary start |
| CCH/billing/attribution strip | NOT_RUN | blocked before canary start |
| fallback/rollback | NOT_RUN | blocked before canary start |

## Leak scan

Safe evidence/log scan was run on generated server scratch text/log/json files. A scanner script initially embedded a server-password literal in its own grep pattern; it was immediately redacted in-place without deleting files. Final non-script evidence/log scan showed zero DB URL, Redis URL, cookie, authorization, provider key, or raw secret-like hits.

No raw prompts, request bodies, responses, DB URLs, Redis URLs, cookies, account IDs, workspace IDs, proxy credentials, cert/key material, pcap/HAR, or native dumps were committed.

## Rollback/production state

- Old `18080` production entry remained listening on the same docker-proxy pid bucket before and after.
- Old `18081` remained listening on the same docker-proxy pid bucket before and after.
- `3012` and `3017` remained free.
- No production container/service was stopped, restarted, or rebound.
- No production DB/Redis action was performed.

## Limitations / blocker

Plan79A did not reach deployed mock smoke because the real Sub2API server startup path is not proven read-only with respect to DB. To proceed safely, a Plan79A addendum needs one of:

1. a verified read-only/mock-only Sub2API server startup mode that does not run migrations or persist bootstrap/default data; or
2. explicit user approval to use isolated ephemeral DB/Redis inside the same local-only mock scope, with clear confirmation that the Plan79A DB/Redis write prohibition applies only to production DB/Redis.

Until then, Plan79B cutover must not proceed.

## Explicit non-actions

- Did not switch or rebind `18080`.
- Did not run live canary.
- Did not access real Anthropic/AWS/Vertex/Bedrock/OpenAI/DeepSeek or any credentialed upstream.
- Did not use real OAuth/API key/session cookie/account/billing credentials for tests.
- Did not run migration.
- Did not write PostgreSQL/Redis data.
- Did not delete files/directories.
- Did not run git reset/clean/checkout/restore/rebase or force push.
