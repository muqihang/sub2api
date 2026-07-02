# 79A-DB-safe - Production Blue/Green Mock Smoke Addendum Evidence Report

**Final decision:** BLOCKED_LOCAL_ONLY_EGRESS_GUARD

**Scope:** Plan79A-DB-safe addendum only. This run re-opened the prior Plan79A blocker with controlled production PostgreSQL/Redis access allowed, but it did **not** switch `18080`, did **not** run live canary, and did **not** access real Anthropic/AWS/Vertex/Bedrock/OpenAI/DeepSeek or any credentialed upstream.

**Production server:** `198.12.67.185` via SSH port `57275`  
**Server scratch:** `/tmp/plan79a-db-safe-20260702T135133Z/`  
**Existing new release path:** `/opt/plan79/releases/20260702T125510Z/`  
**Scratch cleanup status:** `skipped_requires_user_approval`  
**Report generated:** 2026-07-02T17:59:00Z

## Decision precedence path

The addendum passed backup and migration/startup-write audit, but stopped before starting the new canary chain because the required same-scope loopback-only egress guard cannot be proven together with production PostgreSQL/Redis access on this host:

1. Production PostgreSQL/Redis are running in Docker and are not published on host-loopback ports `5432` / `6379`.
2. The production Sub2API container uses container-name/Docker-network DB/Redis targets rather than loopback targets.
3. A `docker --network none`/loopback-only canary scope can prove upstream/provider egress isolation, but it cannot also connect to production DB/Redis.
4. Starting a real Sub2API canary with production DB/Redis would require Docker bridge/DNS or another non-loopback path, making `non_loopback_attempt_count = 0` unprovable for the same execution scope.

Because Plan79A-DB-safe requires DNS/non-loopback/UDP/provider-direct blocking and `non_loopback_attempt_count = 0` in the same scope, the run stopped with `BLOCKED_LOCAL_ONLY_EGRESS_GUARD` instead of weakening the guard or claiming unproven evidence.

## CP0-CP9 status table

| CP | Status | Safe evidence / result |
|---|---|---|
| CP0 read previous state and reverify baseline | PASS | release exists; `18080`/`18081` still old docker-proxy listeners; `3012`/`3017` free; `19382`-`19385` free |
| CP1 PostgreSQL/Redis backup | PASS | full PostgreSQL dumps and Redis RDB copy completed with size/hash evidence |
| CP2 migration/startup write audit | PASS_WITH_GUARDS | migrations contain destructive tokens, so startup migrations must be skipped; `RUN_MODE=standard`; bootstrap secret create-if-missing/no-overwrite policy verified |
| CP3 prepare/start isolated canary | BLOCKED_NOT_STARTED | prestart feasibility showed production DB/Redis are Docker-bridge/non-loopback and not host-loopback published |
| CP4 egress guard | BLOCKED_LOCAL_ONLY_EGRESS_GUARD | same-scope loopback-only guard incompatible with production DB/Redis connectivity on current server topology |
| CP5 health | NOT_RUN | stopped before starting Plan79A-owned canary processes |
| CP6 full mock smoke | NOT_RUN | stopped before canary start; no Sub2API/CC/sidecar/collector chain ran |
| CP7 DB/Redis after-state validation | PASS_NO_WRITE | safe after-state read-only buckets collected; no addendum canary writes expected or performed |
| CP8 shutdown | PASS | no Plan79A addendum-owned PIDs existed; canary ports remained free; old services still running |
| CP9 report | PASS | this safe-summary report written locally |

## Source and commit lock

| Component | Required | Used / status |
|---|---|---|
| Sub2API Plan76 implementation | `13f3fa08cfa58742b80c20d4534b49f82b621599` | included in accepted chain |
| Sub2API Plan77 report | `3c8b5a1c2` | ancestor of current report chain |
| Sub2API Plan78 report | `c448fdbbb` | ancestor of current report chain |
| Prior Plan79A blocked report | `966f9bd8e78d` | current local base before this report |
| CC Gateway mock bridge | `7957824` | release artifact descendant |
| CC Gateway real sidecar proof | `7729410` | release artifact descendant |

Tracked-source release artifacts were already built under `/opt/plan79/releases/20260702T125510Z/`. No production service path was overwritten.

## Existing release artifacts

| Artifact | Safe path bucket / hash |
|---|---|
| release path bucket | `36bffc8e34cdb6e9210e9b8f6b99f0eaa6973ca96b59f8462df58be790441fed` |
| Sub2API server binary | sha256 `2f704feecb31ac24aefe1e94665e1baa3ed59edb576a16e2f7f51a949b000916` |
| CC Gateway `dist/index.js` | sha256 `2badf2adeff8d7bda44e547afdda2e68ee0146cbab8cdc2ca7ad5922e7c02a34` |
| CC Gateway `dist/proxy.js` | sha256 `9892c702b77bb06c5bc12a0d8d9cf6c21daa3402b50c83c8944f0f7660d8400e` |
| Go/uTLS sidecar | sha256 `5f4d5d1c87131f44c36027453b15123d8a309868c5b58e9d3a6ef8d21d59e9e0` |

## CP0 production baseline and forbidden ports

Latest prestart reverify:

| Port | Baseline status | After status |
|---:|---|---|
| 18080 | LISTEN by old docker-proxy, listener bucket `5d94f5d55489560fa6b5145f080228db1762fa8c06c1412b907c6c30b3a5801d` | same listener bucket |
| 18081 | LISTEN by old docker-proxy, listener bucket `c2a4b726f231bf2998fffa5ecc5e53f334d2196b24818090fa6aacab3405e023` | same listener bucket |
| 3012 | FREE | FREE |
| 3017 | FREE | FREE |
| 19382 | FREE | FREE |
| 19383 | FREE | FREE |
| 19384 | FREE | FREE |
| 19385 | FREE | FREE |

Old production containers still running after stop point: `chelingxi-sub2api`, `chelingxi-cc-gateway`, `chelingxi-postgres`, and `chelingxi-redis`.

## PostgreSQL backup evidence

Backup root path bucket: `7d0ac13f6f5b4b5110733e4d82d47a9b4399dba7c23ef0584abe8108030c6bd6`.

| Backup item | Size bytes | sha256 |
|---|---:|---|
| globals | 455 | `90b0bdec74f1f430942f7eb7e6129743c227e8bd68f04096753d014624701d53` |
| database dump 1 | 1053 | `0ad20019d400608f49a8f43325eecfffb32758d0ef307a266744e3b5c9271213` |
| database dump 2 | 68929110 | `56fe22cabad9f17f855d9ffd8e4c668cabb32c3a5e635c31426987069b9fe92d` |
| database dump 3 | 60733304 | `f583e9fe8bd281c4d59a6276b962f6de368a3286b21e4cf322bbbfbd5ff73d3b` |

PostgreSQL DB count bucket evidence: DB count `3`; DB name-set hash `00738869943cc3641e8beabc82e224b3e9066517cab5dd6d1fc35feeba3cac47`.

## Redis backup evidence

| Backup item | Size bytes | sha256 |
|---|---:|---|
| Redis RDB snapshot copy | 1295063 | `058aa674f4fd2be04336b9cb1793a87b7b90e7bcf7060caa900114072d8b069e` |

Redis backup path bucket: `8d7492ea86f69bbfb06b5f1275a828d4f40e3f15174824b1222bd2465cfe8fb1`. Redis `BGSAVE`/snapshot copy completed; no `KEYS`, `FLUSH*`, or `DEL` was used.

## Migration/startup audit summary

| Audit item | Result |
|---|---|
| migration file count | 196 |
| `DROP` token hits | 80 |
| `TRUNCATE` token hits | 0 |
| `DELETE` token hits | 88 |
| `ALTER` token hits | 246 |
| `UPDATE` token hits | 70 |
| migration decision | do not run migrations |
| required startup knob | `SUB2API_SKIP_STARTUP_MIGRATIONS=1` |
| required run mode | `RUN_MODE=standard` |
| simple-mode bootstrap | disabled by `RUN_MODE=standard` |
| Redis init | client init only; no static flush/delete path observed |

## Bootstrap overwrite risk result

| Item | Safe result |
|---|---|
| `security_secrets` table exists before | true |
| bootstrap secret existed before | true |
| bootstrap secret meta hash before | `f181b61f767a0026af740b38c5c1075e9979c4f64c1ac23f52c91e47d6800567` |
| bootstrap policy | create-if-missing / no overwrite, with after-state monitoring |
| canary started | false |
| bootstrap write expected | false |
| bootstrap overwrite risk | not observed; no canary startup occurred |

## DB/Redis after-state safe comparison

No canary chain was started, no migrations were run, and no application DB/Redis writes were expected from this addendum run.

| After-state item | Safe bucket / status |
|---|---|
| app DB name hash | `7e00c4e93784ee94268cd479b51ca0633d3fd2311d165b17b962a8d04806ab88` |
| schema migrations row-count bucket | `417742aec2ed2c52ee528213a2a58511e6bbb1c9b81acc2a72d4a8031daf1dec` |
| bootstrap secret existed after | true |
| bootstrap secret meta hash after | `f181b61f767a0026af740b38c5c1075e9979c4f64c1ac23f52c91e47d6800567` |
| Plan79 named accounts count bucket | `9a271f2a916b0b6ee6cecb2426f0b3206ef074578be55d9bc94f6f3fe3ab86aa` |
| Plan79 API keys count bucket | `9a271f2a916b0b6ee6cecb2426f0b3206ef074578be55d9bc94f6f3fe3ab86aa` |
| Redis `DBSIZE` bucket after | `8c9a013ab70c0434313e3e881c310b9ff24aff1075255ceede3f2c239c231623` |
| Redis persistence bucket after | `41a13c7fc2d666818a4d3da5a1461b1599470bfa936572c498951b0daca2a889` |
| Redis `FLUSH*` / `DEL` / `KEYS` | not executed |

## Canary ports and process status

| Component | Intended port | Status |
|---|---:|---|
| New real Sub2API canary | 19382 | not started; port remained free |
| New CC Gateway canary | 19383 | not started; port remained free |
| New Go/uTLS sidecar | 19384 | not started; port remained free |
| Mock collector | 19385 | not started; port remained free |

Plan79A addendum-owned pid files found: `0`. Shutdown action: `not_needed`.

## Egress guard proof / blocker

| Guard assertion | Result |
|---|---|
| DNS blocked | not safely provable with production DB/Redis connectivity requirement |
| IPv4 non-loopback blocked | not safely provable with production DB/Redis connectivity requirement |
| IPv6 non-loopback blocked | not safely provable with production DB/Redis connectivity requirement |
| UDP blocked | not safely provable with production DB/Redis connectivity requirement |
| inherited proxy env blocked | not reached; no canary scope started |
| provider direct TCP blocked | not reached; no canary scope started |
| real upstream request count | 0 |
| Node direct HTTPS fallback count | 0 |
| non-loopback attempt count | 0 by not starting; not a successful guard proof |

Safe network feasibility evidence:

- Host `5432` listener count: `0`.
- Host `6379` listener count: `0`.
- Production PostgreSQL/Redis containers are running on Docker bridge, not host-loopback published.
- Production Sub2API DB/Redis config source uses container-name/Docker-network targets, not loopback.

Conclusion: with current topology, starting the real Sub2API canary against production DB/Redis would require non-loopback/Docker-network connectivity. That would invalidate the required same-scope local-only egress guard. The guard was not waived.

## Health and smoke case table

Because CP4 blocked before process start, CP5 health and CP6 full mock smoke were not run.

| Case | Status | Reason |
|---|---|---|
| New Sub2API health | NOT_RUN | no canary process started |
| New CC Gateway health | NOT_RUN | no canary process started |
| Go/uTLS sidecar health | NOT_RUN | no canary process started |
| Mock collector health | NOT_RUN | no canary process started |
| observed inbound `2.1.179` canonicalizes to `2.1.197` | NOT_RUN | blocked by local-only guard |
| observed inbound `2.1.185` canonicalizes to `2.1.197` | NOT_RUN | blocked by local-only guard |
| observed inbound `2.1.197` canonicalizes to `2.1.197` | NOT_RUN | blocked by local-only guard |
| Sonnet 5 under `2.1.197` | NOT_RUN | blocked by local-only guard |
| Sonnet 5 under `2.1.185` / `2.1.179` fail-closed | NOT_RUN | blocked by local-only guard |
| count_tokens fail-closed before sidecar | NOT_RUN | blocked by local-only guard |
| MCP configured fail-closed before sidecar | NOT_RUN | blocked by local-only guard |
| non-streaming fail-closed before sidecar | NOT_RUN | blocked by local-only guard |
| unapproved model/control-plane fail-closed before sidecar | NOT_RUN | blocked by local-only guard |
| Plan72 env residue cases | NOT_RUN | blocked by local-only guard |
| CCH/billing/attribution strip | NOT_RUN | blocked by local-only guard |
| fallback/rollback TLS buckets | NOT_RUN | blocked by local-only guard |

## TLS safe summary

| Item | Result |
|---|---|
| real Go/uTLS sidecar binary available in release | true |
| sidecar artifact hash | `5f4d5d1c87131f44c36027453b15123d8a309868c5b58e9d3a6ef8d21d59e9e0` |
| sidecar process started | false |
| `2.1.197` TLS bucket observed in this addendum | NOT_RUN |
| fallback/rollback TLS bucket observed in this addendum | NOT_RUN |

No TLS regression claim is made by this addendum because the real chain was not started.

## Env residue validation

| Case | Result |
|---|---|
| `Asia/Shanghai` marker | NOT_RUN |
| `Asia/Urumqi` marker | NOT_RUN |
| nonofficial base URL residue | NOT_RUN |
| synthetic domain / AI keyword residue | NOT_RUN |
| residue changing logical host/tuple/proxy/upstream identity | not observed; no canary chain started |

## Control-plane fail-closed validation

| Path | Result |
|---|---|
| count_tokens | NOT_RUN |
| MCP configured shape | NOT_RUN |
| non-streaming shape | NOT_RUN |
| unapproved model/control-plane | NOT_RUN |
| Sonnet 5 under fallback/rollback | NOT_RUN |

## CCH/billing/attribution strip assertions

| Assertion | Result |
|---|---|
| no `x-anthropic-billing-*` | not emitted by any addendum canary process; no canary started |
| no raw/native CCH | not emitted by any addendum canary process; no canary started |
| no signed/no-CCH mode | not emitted by any addendum canary process; no canary started |
| no client attribution | not emitted by any addendum canary process; no canary started |

## Safety counters

| Counter / action | Value |
|---|---:|
| real upstream request count | 0 |
| Node direct HTTPS fallback count | 0 |
| non-loopback attempt count | 0 by no-start; guard remains blocked |
| production traffic cutover | 0 |
| live canary requests | 0 |
| migration command executed | 0 |
| destructive SQL executed | 0 |
| Redis `FLUSH*` / `DEL` / `KEYS` | 0 |
| Plan79A addendum-owned running PIDs after run | 0 |

## Leak scan

Evidence text files were scanned for DB/Redis URL forms, authorization headers, cookies, private keys, provider-key patterns, and raw evidence markers. No DB URL, Redis URL, cookie, authorization header, private key, or provider key hits were found. Raw-marker matches were false-positive schema/status/default words in safe schema evidence, not raw prompt/body/response, pcap, HAR, or TLS dumps.

No server scratch, raw logs, native dumps, backups, credentials, DB URLs, Redis URLs, cookies, account IDs, workspace IDs, proxy credentials, cert/key material, raw prompts, raw request bodies, raw responses, raw ClientHello, pcap, or HAR were committed.

## Rollback / old production state

- Old `18080` production entry remained listening on the old docker-proxy listener bucket.
- Old `18081` remained listening on the old docker-proxy listener bucket.
- `3012` and `3017` remained free.
- Existing production containers stayed running.
- No production service was stopped, restarted, rebound, or reconfigured.
- Backups are available under the server backup root path bucket recorded above.

## Limitations and required next decision

This addendum did not produce a blue/green mock-smoke PASS. The precise blocker is not migration safety or backup safety; it is the egress-guard topology conflict:

- If the next attempt must connect to production DB/Redis and still require global same-scope `non_loopback_attempt_count = 0`, the server needs a loopback-only way to reach production DB/Redis that does not touch current production listeners and does not weaken the upstream egress guard.
- Alternatively, the plan must be explicitly revised to count production DB/Redis Docker-network traffic as an allowed internal exception while still proving provider/upstream egress remains loopback-only and zero-real-upstream.

Until a revised guard model is explicitly approved and proven, **Plan79B cutover must not proceed**.

## Explicit non-actions

- Did not switch, stop, restart, rebind, or replace `18080`.
- Did not stop, restart, rebind, or replace `18081`.
- Did not touch `3012` or `3017`.
- Did not run live canary.
- Did not access real Anthropic/AWS/Vertex/Bedrock/OpenAI/DeepSeek or credentialed upstream.
- Did not use real OAuth/API key/session cookie/account/billing credentials for tests.
- Did not run migrations.
- Did not clear, truncate, drop, or overwrite PostgreSQL/Redis data.
- Did not execute Redis `FLUSH*`, `DEL`, or `KEYS`.
- Did not delete files/directories.
- Did not run git reset/clean/checkout/restore/rebase or force push.
