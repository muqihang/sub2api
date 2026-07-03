# Plan79B PreCutover Readiness and Rollback Report

**Date:** 2026-07-03 UTC  
**Production server:** `198.12.67.185`  
**Scope:** Plan79B-PreCutover only  
**Final decision:** `READY_FOR_CUTOVER_APPROVAL`

This report prepares the production cutover runbook and rollback path, but the cutover was **not** executed. Current production entry `18080` was not stopped, restarted, rebound, or switched. `18081` was not stopped, restarted, or rebound. `3012/3017` were not touched. No live canary and no real credentialed upstream request were run.

## Decision path

1. Plan79A-v3 was already accepted with final decision `PASS_DB_SAFE_BLUEGREEN_MOCK_SMOKE_READY`.
2. Current production baseline was rechecked: old `18080/18081` were still listening and healthy; `3012/3017` were free.
3. Final PostgreSQL and Redis backups were created and hashed.
4. Latest release artifacts and Plan79A-v3 safe evidence were verified by sha256.
5. A short PreCutover dry-start used independent loopback ports `19582-19585`, did not bind `18080`, and ran the proven primary mock smoke with `13/13` cases passing.
6. The temporary PreCutover processes were stopped and `19582-19585` were free afterward.
7. Rollback and cutover runbooks were prepared below, but no cutover command was executed.

## CP0-CP5 status table

| CP | Status | Safe evidence |
|---|---:|---|
| CP0 production baseline | PASS | `18080/18081` LISTEN and health `200`; `3012/3017` FREE; old service/image/config buckets recorded |
| CP1 final backups | PASS | PostgreSQL `pg_dumpall` backup and Redis `BGSAVE` RDB snapshot created with size/hash buckets |
| CP2 new release readiness | PASS | Release artifacts hashed; short loopback dry-start on `19582-19585`; primary mock smoke `13/13` PASS; ports freed afterward |
| CP3 rollback runbook | PASS | Old target and rollback steps prepared; DB restore explicitly requires separate human approval |
| CP4 cutover runbook | PASS | Commands/steps prepared but not executed; live canary/real upstream remains separately gated |
| CP5 report/commit | PASS | This report contains only safe summaries; no secrets/raw DB/Redis contents/raw responses |

## Old production baseline

| Item | Evidence |
|---|---|
| Server time bucket | `2026-07-03T06:13:39Z` final after-check |
| `18080` | LISTEN before and after; health `200`; listener bucket `b8a8ecf4256bced70ca18405ca1fd7f77b8bf6cde2756bbea5699aac3f1d9b17` |
| `18081` | LISTEN before and after; health `200`; listener bucket `69efffb6ed7284c14023a0c5c21c88ef053b95182fc18df6c3fa96f592929cab` |
| `3012` | FREE before and after |
| `3017` | FREE before and after |
| Old public-entry topology | `18080` is exposed by the old `chelingxi-cc-gateway` Docker network namespace; old `chelingxi-sub2api` shares that namespace and serves the Sub2API HTTP entrypoint |
| Old CC Gateway image | `chelingxi-cc-gateway-staging:0d65c80-vscode-observed-20260630T094106Z` |
| Old Sub2API image | `chelingxi-sub2api-staging:2ef34631-vscode-safe-20260630T094106Z` |
| Old CC Gateway config/mount bucket | env bucket `142b47ceb122d0729f6b909be25003bb1291e7faa8e6f3de2ff24e7f2a97718a`; net/ports bucket `3f08ad86e5b13a79591d049286fc90b6dc9a3bf874db5a808f9dd6f537795241` |
| Old Sub2API config/mount bucket | env bucket `cc6bdfe0c9fcd02c1427bce521064e501fd99d9e268770e1533a43438cd5019f`; net/ports bucket `80ee9055828d496f918b526b82feabc9b2d1834d296cd9cfe836405cf3043ce1` |
| DB/Redis config source | Docker containers `chelingxi-postgres` and `chelingxi-redis`; env/mount counts recorded only as redacted buckets |

## Final backup evidence

Backup directory bucket: `197700dfb2d64fdd7c7c95e87a238323`.

| Component | Method | Path bucket | Size | SHA256 |
|---|---|---|---:|---|
| PostgreSQL | `pg_dumpall` compressed with gzip | `a47bdf78098932f0c0e6d8ddf12152b9` | `127163687` | `d3bcca1dae9a681cc25298be6bed7f67d523f4bceef35c88c58a91335c739c9d` |
| Redis | authenticated `BGSAVE` RDB snapshot copied from container | `001739f814d16659a14d3e7462247c62` | `1285246` | `ba2072abd60898d8fbe7dd8811c03fad00b1493b1407d3ce7d80efef31b70f54` |

Redis uses AOF (`aof_enabled=1`) and RDB persistence. A first live-volume tar attempt encountered a normal live AOF race (`file changed as we read it`) and is **not** used as authoritative backup evidence. It was not deleted; cleanup remains `skipped_requires_user_approval`. The authoritative Redis backup for this report is the successful `BGSAVE` RDB snapshot above.

No database contents, Redis keys, DB URL, Redis URL, password, token, account id, or secret were printed or written into this report. No `DROP`, `TRUNCATE`, destructive `ALTER`, mass `DELETE`, Redis `FLUSH*`, Redis `DEL`, or Redis `KEYS` was used.

## New release readiness

| Item | Evidence |
|---|---|
| Release path bucket | `e926fe925f1580a485648b455a9fb5c1` |
| Sub2API implementation commit | `e315607f8 fix: fail closed formal pool count tokens` |
| Plan79A-v3 report commits | `47c868f4d`, `64cffc60c` |
| CC Gateway commits | `7957824 gateway: add local smoke mock response bridge`; `7729410 test: prove real sidecar mock bridge smoke` |
| Sub2API binary sha256 | `1c6b2772d0932b5f294c8e3033b1c04a1f112bc1efaba8b1a91e5f57c80492be` |
| CC Gateway `dist/index.js` sha256 | `2badf2adeff8d7bda44e547afdda2e68ee0146cbab8cdc2ca7ad5922e7c02a34` |
| CC Gateway `dist/proxy.js` sha256 | `9892c702b77bb06c5bc12a0d8d9cf6c21daa3402b50c83c8944f0f7660d8400e` |
| Go/uTLS sidecar sha256 | `5f4d5d1c87131f44c36027453b15123d8a309868c5b58e9d3a6ef8d21d59e9e0` |
| `sub2api.env` safe config hash | `b4e9520189321f10cbeae6fb6fe36c7d3035f5f0d452f9d5f8fdb1fa8286c63d` |
| `cc-gateway.yaml` safe config hash | `35c36e28e7c6444c66947c480846ed347f922489ed089d811b5a19acf8ec988a` |
| `sidecar.env` safe config hash | `8e70a4866dc31e457a5779dd9b11553d15e788a48783736e06ba6b40a1f5cf9c` |

Readiness knobs verified in safe summaries:

- `SUB2API_SKIP_STARTUP_MIGRATIONS=1`.
- `RUN_MODE=standard`.
- `SERVER_HOST=127.0.0.1` for PreCutover dry-start.
- Plan79A-v3 destructive marker count: `0` for Sub2API env and CC Gateway config.
- Plan78/Plan79 local-smoke mock bridge remains explicit local-smoke config only; production/default behavior remains disabled by implementation and tests.
- CC Gateway sidecar config is ready with loopback sidecar in mock-smoke scope and Go/uTLS sidecar artifact present.

## PreCutover dry-start result

A new PreCutover-owned scratch was started on independent ports `19582/19583/19584/19585` and stopped afterward. It did **not** bind `18080` and did **not** switch traffic.

| Check | Result |
|---|---:|
| Scratch bucket | `b89ad4ed2e724454202d227f43fdc592` |
| `19582` Sub2API dry-start | LISTEN during run; FREE after shutdown |
| `19583` CC Gateway dry-start | LISTEN during run; FREE after shutdown |
| `19584` sidecar dry-start | LISTEN during run; FREE after shutdown |
| `19585` mock collector dry-start | LISTEN during run; FREE after shutdown |
| Sub2API `/health` | `200` |
| Primary mock smoke | PASS `13/13` |
| observed `2.1.197` canonical non-Sonnet | PASS `200`, sidecar delta `1` |
| Sonnet 5 under `2.1.197` | PASS `200`, sidecar delta `1` |
| `count_tokens` | PASS `403 formal_pool_count_tokens_profile_unapproved`, sidecar delta `0` |
| old `18080` after dry-start | still LISTEN |
| old `18081` after dry-start | still LISTEN |

Some copied helper services do not expose unauthenticated `GET /health` semantics on every component (`CC Gateway` returned auth-gated `401`; sidecar returned method-gated `405`; collector GET health was not authoritative in this copied harness), but the full proven primary mock smoke passed through Sub2API -> CC Gateway -> Go/uTLS sidecar -> mock collector with safe counters. Plan79A-v3 also retained the earlier component health evidence: collector `200`, sidecar `200`, CC Gateway `200`, Sub2API `200` in its native harness.

## Rollback target

Rollback target is the current old production deployment as observed before PreCutover:

- Old CC Gateway container: `chelingxi-cc-gateway`, image `chelingxi-cc-gateway-staging:0d65c80-vscode-observed-20260630T094106Z`, restart policy `unless-stopped`, public binding `0.0.0.0:18080->8080/tcp`.
- Old Sub2API container: `chelingxi-sub2api`, image `chelingxi-sub2api-staging:2ef34631-vscode-safe-20260630T094106Z`, network mode sharing old CC Gateway namespace.
- Old `18081` companion service remains untouched and is not part of the default rollback unless a later approved cutover explicitly touches it.
- PostgreSQL/Redis restore is **not** part of automatic rollback. DB restore requires separate human approval and a confirmed data-anomaly diagnosis.

## Rollback runbook prepared, not executed

If a later approved cutover fails after binding the new version to `18080`, rollback should be performed in this order:

1. Stop only the new Plan79B-owned `18080` process/container/compose project.
2. Start or re-enable the old `chelingxi-cc-gateway` and `chelingxi-sub2api` containers using the existing `/opt/chelingxi-staging/docker-compose.yml` and old config.
3. Verify old `18080` is listening and `http://127.0.0.1:18080/health` returns `200`.
4. Verify `18081` remains in its prior state.
5. Verify `3012/3017` remain untouched.
6. Do **not** restore PostgreSQL or Redis unless a data anomaly is found and the user explicitly approves DB/Redis restore.
7. If DB/Redis restore is approved separately, use the final backup buckets in this report and perform a separate restore runbook with destructive-restore confirmation.

Rollback command template, prepared but **not executed**:

```bash
# TEMPLATE ONLY - DO NOT RUN WITHOUT EXPLICIT ROLLBACK APPROVAL
set -euo pipefail
cd /opt/chelingxi-staging
# Stop only new Plan79B-owned services first; exact names depend on the approved cutover implementation.
# docker stop <new-plan79b-sub2api> <new-plan79b-cc-gateway> <new-plan79b-sidecar>
# docker rm <new-plan79b-sub2api> <new-plan79b-cc-gateway> <new-plan79b-sidecar>

docker compose up -d cc-gateway sub2api
curl -fsS http://127.0.0.1:18080/health >/dev/null
ss -ltnp 'sport = :18080'
```

## Plan79B cutover runbook prepared, not executed

The following is the prepared cutover flow for a later explicit `GO Plan79B cutover`. It is intentionally not executed in PreCutover.

1. Reconfirm current baseline immediately before cutover:
   - `18080` old service listening and health `200`.
   - `18081` unchanged.
   - `3012/3017` untouched.
   - New release artifact hashes match this report.
2. Create a cutover scratch directory under `/tmp/plan79b-cutover-<timestamp>/` and copy only the verified release config templates/secrets from the already-proven server scratch; do not write secrets into logs.
3. Prepare the new Plan79B-owned process/container names and ports:
   - New Sub2API binds the production entry only after the old `18080` binding is intentionally released.
   - New CC Gateway uses the real Go/uTLS sidecar and production-safe config.
   - Mock response bridge remains production-default disabled unless an explicit local-smoke config is used for mock-only tests.
4. Stop or move the old `18080` binding only after the above checks pass. Do not touch `18081` unless separately approved.
5. Start the new version on `18080`.
6. Verify only local health first:
   - `curl -fsS http://127.0.0.1:18080/health`.
   - process/container is the new release bucket.
   - old `18081` still healthy.
7. Do not run tiny live canary or real upstream request unless the user separately approves live canary / real upstream.
8. Observe error rate/log safe buckets for the approved observation window.
9. Roll back immediately if health fails, process exits, `18081` changes unexpectedly, forbidden ports change, DB/Redis anomaly is detected, or egress/upstream policy differs from the approved mode.

Cutover command skeleton, prepared but **not executed**:

```bash
# TEMPLATE ONLY - DO NOT RUN UNTIL USER SAYS: GO Plan79B cutover
set -euo pipefail
TS=$(date -u +%Y%m%dT%H%M%SZ)
REL=/opt/plan79/releases/20260703T042643Z-v3
SCR=/tmp/plan79b-cutover-$TS
mkdir -p "$SCR/run" "$SCR/evidence"

# 1. Reconfirm baseline and artifact hashes.
ss -ltnp 'sport = :18080'
ss -ltnp 'sport = :18081'
ss -ltnp 'sport = :3012' || true
ss -ltnp 'sport = :3017' || true
sha256sum "$REL/sub2api/backend/sub2api-server" \
          "$REL/cc-gateway/dist/index.js" \
          "$REL/cc-gateway/dist/proxy.js" \
          "$REL/sidecar/egress-tls-sidecar/egress-tls-sidecar"

# 2. Back up current compose/config metadata again to a root-only cutover audit directory.
# cp -a /opt/chelingxi-staging/docker-compose.yml "$SCR/evidence/docker-compose.before"
# cp -a /opt/chelingxi-staging/config/cc-gateway.yaml "$SCR/evidence/cc-gateway.before"

# 3. Stop only old 18080 binding; do not touch 18081/3012/3017.
# docker compose -f /opt/chelingxi-staging/docker-compose.yml stop sub2api cc-gateway

# 4. Start new Plan79B-owned services binding 18080.
# Exact implementation should use the verified release artifacts and production-safe config.
# docker compose -f "$SCR/run/docker-compose.plan79b.yml" up -d plan79b-cc-gateway plan79b-sub2api plan79b-sidecar

# 5. Verify local health only.
# curl -fsS http://127.0.0.1:18080/health >/dev/null
# ss -ltnp 'sport = :18080'
```

## Safety confirmations

| Confirmation | Result |
|---|---:|
| `18080` switched in PreCutover | `false` |
| `18080` stopped/restarted/rebound in PreCutover | `false` |
| `18081` stopped/restarted/rebound in PreCutover | `false` |
| `3012/3017` touched | `false` |
| Live canary run | `false` |
| Real upstream request run | `false` |
| PostgreSQL destructive operation | `false` |
| Redis `KEYS`/`DEL`/`FLUSH*` | `false` |
| DB/Redis backup complete | `true` |
| Temporary PreCutover ports freed | `true` |
| Scratch cleanup | `skipped_requires_user_approval` |

## Limitations and required next approval

- This is only PreCutover readiness. It authorizes waiting for cutover approval; it does not itself authorize switching `18080`.
- A later production cutover still requires the explicit user instruction: `GO Plan79B cutover`.
- A tiny live canary or any real upstream request remains separately gated by explicit approval after cutover health is verified.
- DB/Redis restore is not automatic and requires separate explicit approval.
