# Plan79B Production Cutover Evidence Report

**Date:** 2026-07-03 UTC  
**Production server:** `198.12.67.185`  
**Scope:** Plan79B cutover after explicit user approval `GO Plan79B cutover`  
**Final decision:** `PASS_PLAN79B_CUTOVER_LOCAL_HEALTH_READY`

Plan79B cutover replaced the old `18080` Sub2API + CC Gateway pair with the Plan79B new paired release. The old `18080` Docker pair was stopped, not deleted. `18081` was not stopped/restarted/rebound. `3012/3017` remained free/untouched. No agent-initiated live canary and no agent-initiated real upstream probe were run.

## Decision path

1. Revalidated Plan79B-PreCutover readiness and final DB/Redis backups.
2. Revalidated old production baseline: `18080/18081` health `200`, `3012/3017` free.
3. Prepared Plan79B production config from the Plan79A-v3 accepted release/config lineage:
   - Sub2API release binary `e315607f8`.
   - CC Gateway release with Plan78 mock bridge code, production mock bridge disabled.
   - `2.1.197` formal-pool canonical message beta profile.
4. Started Plan79B-owned internal CC Gateway and local egress proxy without binding `18080`.
5. Stopped only the old `18080` Docker pair: `sub2api` and `cc-gateway` from `/opt/chelingxi-staging/docker-compose.yml`.
6. Started new Sub2API on `0.0.0.0:18080`.
7. Verified `18080` local health `200`, `18081` local health `200`, forbidden ports unchanged, DB/Redis health OK.
8. Short safe observation window passed with all Plan79B-owned processes alive.

## CP0-CP5 status table

| CP | Status | Evidence |
|---|---:|---|
| CP0 baseline | PASS | old `18080/18081` health `200`; `3012/3017` free; backup hashes verified |
| CP1 production config prepare | PASS | config validated with CC Gateway `loadConfig`; mock bridge disabled; production upstream gate enabled |
| CP2 internal services | PASS | Plan79B CC Gateway `127.0.0.1:18443` and egress proxy `127.0.0.1:18445` started before releasing `18080` |
| CP3 cutover | PASS | old Docker `sub2api/cc-gateway` stopped; new Sub2API bound `18080`; rollback not triggered |
| CP4 local verification | PASS | new `18080` health `200`; old `18081` health `200`; `3012/3017` free; DB/Redis ready |
| CP5 observe/report | PASS | processes alive after observation; safe report written; no raw logs/secrets committed |

## Versions / artifacts

| Component | Evidence |
|---|---|
| Release path bucket | `e926fe925f1580a485648b455a9fb5c1` |
| Sub2API implementation | `e315607f8 fix: fail closed formal pool count tokens` |
| Plan79A-v3 evidence | `47c868f4d`, `64cffc60c` |
| Plan79B PreCutover report | `4f14e3057 docs: add plan79b precutover readiness report` |
| CC Gateway Plan78 bridge lineage | `7957824`, `7729410` |
| Sub2API binary sha256 | `1c6b2772d0932b5f294c8e3033b1c04a1f112bc1efaba8b1a91e5f57c80492be` |
| CC Gateway `dist/index.js` sha256 | `2badf2adeff8d7bda44e547afdda2e68ee0146cbab8cdc2ca7ad5922e7c02a34` |
| CC Gateway `dist/proxy.js` sha256 | `9892c702b77bb06c5bc12a0d8d9cf6c21daa3402b50c83c8944f0f7660d8400e` |

## Final backup status

Plan79B reused the final PreCutover backups and reverified their hashes before cutover.

| Component | Method | Size | SHA256 |
|---|---|---:|---|
| PostgreSQL | `pg_dumpall` gzip | `127163687` | `d3bcca1dae9a681cc25298be6bed7f67d523f4bceef35c88c58a91335c739c9d` |
| Redis | authenticated `BGSAVE` RDB snapshot | `1285246` | `ba2072abd60898d8fbe7dd8811c03fad00b1493b1407d3ce7d80efef31b70f54` |

No DB contents, Redis keys, DB URL, Redis URL, password, token, account id, or secret were written into this report. No `DROP`, `TRUNCATE`, destructive `ALTER`, mass `DELETE`, Redis `KEYS`, Redis `DEL`, or Redis `FLUSH*` was used.

## Production config summary

| Item | Result |
|---|---:|
| Sub2API bind | `0.0.0.0:18080` |
| CC Gateway bind | `127.0.0.1:18443` |
| Plan79B local egress proxy bind | `127.0.0.1:18445` |
| `RUN_MODE` | `standard` |
| `SERVER_MODE` | `release` |
| `SUB2API_SKIP_STARTUP_MIGRATIONS` | `1` |
| CC Gateway mode | `sub2api` |
| CC Gateway upstream mode | `production` |
| `production_upstream_enabled` | `true` |
| `real_canary_user_approved` | `false` |
| message beta profile | `claude_code_2_1_197_sonnet5` |
| local-smoke mock response bridge | `disabled` |
| production sidecar | `disabled` |

Important production note: the current Go/uTLS sidecar binary is still loopback-test/mock-smoke oriented because it requires an explicit loopback test dial override. Therefore Plan79B production config disables the local-smoke mock bridge and disables the sidecar for real production traffic rather than routing production users to a mock collector. This cutover deploys the new paired Sub2API + CC Gateway code and 2.1.197 canonical policy, but does not claim a real-provider sidecar live canary.

## Port / process transition

| Port | Before | During | After |
|---:|---|---|---|
| `18080` | old Docker pair LISTEN | briefly FREE after old stop | new Plan79B Sub2API LISTEN, health `200` |
| `18081` | old companion LISTEN | untouched | LISTEN, health `200` |
| `3012` | FREE | untouched | FREE |
| `3017` | FREE | untouched | FREE |
| `18443` | FREE | new CC Gateway LISTEN | LISTEN |
| `18445` | FREE | new local egress proxy LISTEN | LISTEN |

Plan79B-owned process safe status after observation:

| Process | Status |
|---|---:|
| `plan79b-sub2api` | alive |
| `plan79b-cc-gateway` | alive |
| `plan79b-egress-proxy` | alive |

Old Docker pair state after cutover:

| Old container | State |
|---|---:|
| old `chelingxi-cc-gateway` | exited |
| old `chelingxi-sub2api` | exited |
| `chelingxi-postgres` | ready |
| `chelingxi-redis` | ready |

## Verification results

| Check | Result |
|---|---:|
| new local `18080 /health` | `200` |
| old companion `18081 /health` | `200` |
| PostgreSQL health | OK |
| Redis health | OK |
| `3012/3017` | FREE |
| local-smoke mock bridge enabled | `false` |
| agent-initiated live canary | `false` |
| agent-initiated real upstream probe | `false` |
| rollback triggered | `false` |

A short observation window completed with `18080` and `18081` healthy and Plan79B-owned processes alive. After cutover, production traffic may reach `18080`; one existing production route emitted an upstream 4xx safe error bucket in Sub2API logs during observation. The raw upstream body/request was not persisted in this report.

## Rollback status

Rollback remains available because old Docker containers and compose/config files were not deleted.

Rollback command, prepared but not executed because cutover health passed:

```bash
SCR=$(cat /tmp/plan79b-cutover-current)
for n in plan79b-sub2api plan79b-cc-gateway plan79b-egress-proxy; do
  pid=$(cat "$SCR/$n.pid" 2>/dev/null || true)
  [ -n "$pid" ] && kill "$pid" || true
done
cd /opt/chelingxi-staging
docker compose up -d cc-gateway sub2api
curl -fsS http://127.0.0.1:18080/health >/dev/null
```

DB/Redis restore is not part of automatic rollback and still requires separate explicit approval.

## Limitations / follow-up

- Plan79B services are currently Plan79B-owned host processes with pid files under the server scratch. They are not yet converted into persistent systemd units or Docker restart-managed containers.
- No agent-initiated live canary was performed. If desired, a tiny real live canary needs separate explicit approval and a new guarded runbook.
- The production sidecar is disabled because the current sidecar artifact is loopback-test/mock-smoke constrained. A future production uTLS sidecar would require a separate implementation/proof.
- Scratch cleanup status: `skipped_requires_user_approval`.
