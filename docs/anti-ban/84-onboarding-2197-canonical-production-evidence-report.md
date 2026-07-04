# Plan84 Onboarding 2.1.197 Canonical Production Evidence Report

Date: 2026-07-03 UTC

## Final Decision

**PARTIAL_PASS_ONBOARDING_CANONICAL_2197_FIXED_UPSTREAM_AUTH_BLOCKED_FOR_TARGET_ACCOUNTS**

The legacy onboarding/runtime-repair logic is now aligned with the production Claude Code **2.1.197** canonical tuple. The previous production healthcheck `500` / `claude_native_session_boundary_failed` blocker was fixed and deployed. However, the affected `0624-*` and `0610*` accounts still cannot be moved to warming because their official healthcheck requests now reach the production CC Gateway/sidecar path but receive safe-classified upstream **403/auth/forbidden** results. Per Plan84, these accounts were **not** directly SQL-promoted or forced into warming.

## Commits Used

### Sub2API

- `04c20ac57` — `fix: use 2197 canonical tuple for onboarding`
- `392e25331` — `fix: use 2197 canonical tuple for runtime repair`
- `df337e002` — `fix: allow safe formal pool 2197 session promotion`

### CC Gateway

- `b62a174` — `gateway: allow safe runtime canonical promotion`

## Code Changes

### Onboarding / runtime repair tuple

- New/default formal-pool onboarding and runtime repair now write/use canonical `cc_gateway_policy_version=2.1.197`.
- Persona defaults to `claude-code-2.1.197-macos-local` via canonical tuple helpers.
- Explicit fallback/rollback tuple behavior remains preserved for `2.1.185` and `2.1.179`.

### Healthcheck session boundary

- Added a narrow formal-pool session boundary migration allowance:
  - allowed source tuples: canonical `2.1.179` / `2.1.185` only;
  - allowed target tuple: canonical `2.1.197` only;
  - same account ref, credential ref, egress bucket, proxy identity, device, provider family/kind, AWS authority fields, billing/egress policy where applicable;
  - updates the safe session boundary ledger only after the above checks pass.
- Non-canonical tuple drift still fails closed with `claude_native_session_boundary_failed`.

## Tests Run

Sub2API focused tests:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/backend
/opt/homebrew/bin/go test ./internal/service -run 'FormalPool.*Onboarding|FormalPool.*Healthcheck|CCGateway.*Canonical|ObservedProfile|Canonical|Promotion|ControlPlane|Model|FormalPoolOperations.*RuntimeRegister' -count=1
/opt/homebrew/bin/go test ./internal/service -run 'FormalPool.*Onboarding|FormalPool.*Healthcheck|CCGateway.*Canonical|ObservedProfile|Canonical|Promotion|ControlPlane|Model' -count=1
/opt/homebrew/bin/go test ./internal/service -run 'ClaudeCodeSessionMapper|FormalPoolGatewayHealthcheck' -count=1
```

Result: all passed.

CC Gateway tests from earlier Plan84 fix:

```bash
npx tsx tests/proxy-sub2api.test.ts
npx tsx tests/config.test.ts
npx tsc --noEmit
```

Result: all passed.

## Production Deployment

Production server: `198.12.67.185`.

### Sub2API

Current production `18080` binary after Plan84 session-boundary fix:

```text
/opt/plan84/releases/20260703T154926Z-sub2api-df337e002/backend/sub2api-server-plan84-df337e002
```

Build hash:

```text
c4743b2b369ba2c5feaf6927f3cf29fa9f34bf53a84836b8318795c1d63ced26
```

Runtime health after deploy:

- `18080 /health`: `status_200`
- `18443 /health`: `status_403` (auth-protected CC Gateway health; expected bucket)
- `19484 /_health`: `status_200`

### CC Gateway / sidecar

- CC Gateway remains on `127.0.0.1:18443`, commit bucket corresponds to `b62a174` release.
- Go/uTLS sidecar remains on `127.0.0.1:19484` and health is `status_200`.
- Production sidecar path remains enabled.
- Mock response bridge remains disabled for production.

## Port / Service Safety

Final safe listener state:

- `18080`: new Sub2API Plan84 binary.
- `18443`: CC Gateway, loopback-only.
- `19484`: production Go/uTLS sidecar, loopback-only.
- `18081`: old docker-proxy rollback service still running, untouched.
- `3012`: untouched.
- `3017`: untouched.

No old Docker pair was deleted. No destructive DB/Redis operation was performed.

## Official Account Flow Results

All account operations used official admin endpoints:

- `runtime-register`
- `healthcheck`
- `start-warming` only if healthcheck passed

No direct SQL promotion was used. No raw account IDs, emails, tokens, prompts, bodies, responses, or secrets were written to this report.

### `0624-*` accounts

Targets found: `6`.

Outcome:

- runtime-register completed / tuple upgraded to `2.1.197`: `6`
- healthcheck reached CC Gateway evidence path: `6`
- session-boundary `500` after fix: `0`
- healthcheck passed: `0`
- warming started: `0`
- schedulable: `0`
- still quarantined after official healthcheck: `6`
- safe diagnostic class: upstream `403`, bucket `auth`, stable safe code `forbidden` (all 6)
- rate-limit metadata present on some accounts, but healthcheck blocker was still `403/auth/forbidden`, not a normal schedulable 429 wait state.

### `0610*` accounts

Targets found: `2`.

Outcome:

- runtime-register completed / tuple upgraded from `2.1.179` to `2.1.197`: `2`
- healthcheck reached CC Gateway evidence path: `2`
- session-boundary `500` after fix: `0`
- healthcheck passed: `0`
- warming started: `0`
- schedulable: `0`
- still quarantined after official healthcheck: `2`
- safe diagnostic class: upstream `403`, bucket `auth`, stable safe code `forbidden` (all 2)

### 10-day-login quarantine scope

A direct DB query was not performed because the running process environment did not expose a DB URL in a safe way. I used the official admin API to scan explicit `0610` and `0624` scopes. A broader “last login within 10 days” sweep remains not executed in this run because the available safe admin API scan did not expose a reliable last-login filter without pulling broad account data into scratch.

## Evidence Paths on Server

Safe evidence files were kept in scratch and were not committed:

```text
/tmp/plan84-onboarding-2197-20260703T125407Z/evidence/affected_0624_official_flow_after_session_promotion.safe
/tmp/plan84-onboarding-2197-20260703T125407Z/evidence/affected_0624_diagnostics_after_session_promotion.safe
/tmp/plan84-onboarding-2197-20260703T125407Z/evidence/admin_account_scan_0610_0624.safe
/tmp/plan84-onboarding-2197-20260703T125407Z/evidence/affected_0610_official_flow_after_session_promotion.safe
/tmp/plan84-onboarding-2197-20260703T125407Z/evidence/affected_0610_diagnostics_after_session_promotion.safe
/tmp/plan84-onboarding-2197-20260703T125407Z/evidence/final_prod_state_after_plan84.safe
```

Scratch cleanup status: `skipped_requires_user_approval`.

## Backups / Rollback

Prior Plan84 backups available:

- PostgreSQL backup: `/root/plan84-backups/20260703T142328Z/postgres-sub2api-plan84-runtime-repair.sql.gz`
  - size: `130733485`
  - sha256: `d499afa146b0bd0170514da85c6e7bea23dcf4c521d41df8061384c37116081c`
- Redis safe snapshot copy: `/root/plan84-backups/20260703T142328Z/redis-snapshot-plan84-runtime-repair.safe-copy`
  - size: `1309207`
  - sha256: `d7039842532bc3e71bb3edd383c8ed5e95baa7be3249f7beee4d7c375aa9db7b`

Rollback target remains available:

- previous Sub2API Plan84 binary: `/opt/plan84/releases/20260703T142627Z/sub2api/backend/sub2api-server-plan84-392e25331`
- old docker-proxy rollback service on `18081` remains untouched.

## Security / Leak Scan Summary

- No raw prompt/body/response was committed.
- No raw account IDs, emails, OAuth/ST token, admin JWT, DB URL, Redis URL, cookies, or proxy credentials were committed.
- Server scratch contains secret files and safe evidence only; scratch was not cleaned because deletion requires user approval.
- `.codegraph/` and tmp files were not deleted.

## CodeGraph / Index Note

The Sub2API worktree contains `.codegraph/`, but the local `codegraph` CLI was not available in PATH, so the requested incremental CodeGraph index update could not be run from this session. I did not delete or modify `.codegraph/`.

## Current Production State

- Latest Sub2API Plan84 binary deployed on `18080`.
- Latest CC Gateway Plan84-compatible runtime canonical promotion fix is deployed on `18443`.
- Production Go/uTLS sidecar remains enabled and healthy on `19484`.
- Mock bridge is not enabled for production.
- `18081`, `3012`, and `3017` were not touched.
- No destructive DB/Redis operations were run.

## Operator Conclusion

The old onboarding wizard mismatch with the 2.1.197 production canonical logic is fixed. New/repair runtime-register now canonicalizes to 2.1.197, and the legacy 2.1.179 session-boundary ledger no longer blocks safe canonical promotion.

The affected accounts are not schedulable because their official healthcheck is failing at upstream auth (`403/forbidden`), not because of Sub2API/CC Gateway tuple mismatch. They must be re-authenticated/replaced/repaired at the account credential/proxy/upstream-account layer before they can pass healthcheck and enter warming.

---

# 2026-07-04 Addendum: Account 87 Official Flow and Production Config Alignment

Date: 2026-07-04 UTC

## Addendum Final Decision

**PASS_ACCOUNT_87_RUNTIME_REGISTER_HEALTHCHECK_WARMING_SCHEDULABLE**

The newly imported account `id=87` was advanced only through the official production flow:

1. runtime-register
2. healthcheck
3. start-warming

No SQL hard-promotion to warming was used. The final safe DB state shows `onboarding_stage=warming`, `healthcheck_status=passed`, `cc_gateway_runtime_registered=true`, `schedulable=true`, and canonical tuple `2.1.197` with the 2.1.197 TLS profile ref.

## Root Cause Chain Confirmed

The earlier blockers were local production configuration/compatibility gaps, not Haiku 4.5 or a 1M-context healthcheck body:

1. CC Gateway initially rejected `x-sub2api-healthcheck-persona=claude-code-2.1.197-macos-local` as `unsupported_healthcheck_persona`.
2. Sub2API formal healthcheck initially used non-streaming shape; Plan76 production gateway requires streaming formal-pool `/v1/messages` shape.
3. Runtime registration initially did not propagate the canonical `egress_tls_profile_ref`, creating a strict TLS registration gap.
4. Production CC Gateway was restarted outside Docker `/app` without explicit persistent formal-pool session ledger env, causing `formal_pool_session_ledger_unavailable`.
5. Production CC Gateway had production upstream enabled in config but lacked the explicit process env `ALLOW_REAL_ANTHROPIC_PRODUCTION=1`, causing `real_anthropic_production_not_allowed`.
6. Production sidecar allowlists were still static Plan79 values and did not include account 87's runtime egress bucket / proxy identity refs, causing `egress_tls_sidecar_rejected`.
7. Production CC Gateway had no safe raw-capture summary dir, causing `raw_capture_missing` at Sub2API's healthcheck evidence gate.

After fixing the above, account 87 passed official healthcheck and start-warming.

## Code Commits Deployed for 2.1.197 Flow

### CC Gateway

- `d64c866` — `gateway: accept 2197 healthcheck persona`
- `599aaaf` — `gateway: propagate runtime tls profile refs`

### Sub2API

- `7ca178b71` — `fix: stream formal pool healthcheck`
- `55a1f09d5` — `fix: surface cc gateway healthcheck error code`
- `c4f11359d` — `fix: propagate canonical tls profile in runtime registration`

## Production Artifact State

Current release path:

```text
/opt/plan84/releases/20260704T073922Z-plan84-runtime-tls-ref/
```

Current production artifacts:

```text
Sub2API: /opt/plan84/releases/20260704T073922Z-plan84-runtime-tls-ref/backend/sub2api-server-c4f11359d6b5-embed-linux-amd64
CC Gateway: /opt/plan84/releases/20260704T073922Z-plan84-runtime-tls-ref/cc-gateway/dist/index.js
Sidecar: /opt/plan82/releases/20260703T081543Z/sidecar/egress-tls-sidecar
```

## Production Config Fixes Applied

Only production-owned processes were restarted/config-adjusted:

- CC Gateway `18443` restarted with:
  - `CC_GATEWAY_RUNTIME_MAPPING_FILE=<set>`
  - `CC_GATEWAY_FORMAL_POOL_SESSION_LEDGER_FILE=<set>`
  - `ALLOW_REAL_ANTHROPIC_PRODUCTION=<set>`
  - `CC_GATEWAY_RAW_CAPTURE_DIR=<set>`
  - `CC_GATEWAY_RAW_CAPTURE_LAYOUT=per-request`
- Go/uTLS sidecar `19484` restarted in production mode with expanded safe runtime allowlists:
  - `EGRESS_TLS_SIDECAR_DIAL_MODE=production`
  - no test dial override
  - allowed egress bucket count: `25`
  - allowed proxy ref count: `25`
  - account 87 bucket present in allowlist: `true`

No mock bridge was enabled. No loopback test dial override was enabled. No Node direct fallback was enabled.

## Service / Port Safety

Final safe listener and health state:

| Endpoint | State |
|---|---|
| `18080` | Sub2API healthy; public `/` returns `200 text/html`; `/health` returns `200 application/json` |
| `18443` | CC Gateway healthy on loopback |
| `19484` | Go/uTLS sidecar healthy on loopback |
| `18081` | old docker-proxy rollback service still running, untouched |
| `3012` | untouched |
| `3017` | untouched |

The recurring public `/` 404 is fixed by deploying the embedded frontend production binary.

## Account 87 Official Flow Result

Account: `id=87` (user-visible label omitted from evidence to avoid leaking raw account identity in repo).

### Runtime register

Result: `http_200`, safe diagnostics:

```text
cc_gateway_runtime_registered=true
runtime_evidence_complete=true
onboarding_stage=runtime_registered
schedulable=false
```

### Healthcheck

Result: `http_200`, safe diagnostics:

```text
healthcheck_status=passed
healthcheck_evidence_persisted=true
runtime_evidence_complete=true
cc_gateway_runtime_registered=true
onboarding_stage=healthcheck_passed
schedulable=false
```

### Start warming

Result: `http_200`, safe diagnostics:

```text
onboarding_stage=warming
healthcheck_status=passed
healthcheck_evidence_persisted=true
runtime_evidence_complete=true
cc_gateway_runtime_registered=true
schedulable=true
```

### Final safe DB bucket

```text
id=87
status=active
schedulable=true
onboarding_stage=warming
healthcheck_status=passed
cc_gateway_runtime_registered=true
healthcheck_last_status_code_bucket=status_2xx
healthcheck_last_raw_ref=hmac-sha256:<present>
cc_gateway_policy_version=2.1.197
cc_gateway_persona_profile=claude-code-2.1.197-macos-local
cc_gateway_egress_tls_profile_ref=tls-profile:claude-code-2.1.197-real-oracle-tcp-v1
formal_pool_last_failure_code=<empty>
formal_pool_last_failure_origin=<empty>
```

## Healthcheck Model / Context Finding

The formal-pool healthcheck remains the low-cost Haiku 4.5 path:

```text
model bucket: claude-haiku-4-5-20251001
max_tokens: 64
stream: true
1M context marker/header: not present
```

The failure was not caused by a 1M context request. It was caused by the local production config chain listed above.

## Security / Leak Scan

- No ST token, OAuth token, admin JWT, DB URL, Redis URL, proxy credential, account ID/email, raw prompt/body/response, raw TLS/pcap/HAR, or private key material was written to this report.
- Server safe evidence used redacted buckets only.
- Safe raw-capture dir stores safe summaries; full raw capture is not enabled.
- Server scratch and runtime env files were not deleted because deletion requires explicit user approval.
- `.codegraph/` and existing tmp files were not deleted.

## Current Operator Conclusion

For account `id=87`, the old onboarding/wizard compatibility path is now aligned with the production 2.1.197 canonical tuple and production sidecar path. The account is in warming and schedulable.

Deferred scope remains: broader `0610*`, `0624*`, and last-10-day isolated accounts should be re-run through the same official flow after ensuring their credentials/proxies are valid; do not SQL-force them into warming when healthcheck returns upstream auth or risk failures.
