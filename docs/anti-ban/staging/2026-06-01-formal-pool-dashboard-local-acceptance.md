# Formal Pool dashboard and hard-gate local acceptance report

Date: 2026-06-01

Worktree:

```text
/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation
```

Branch:

```text
codex/claude-antiban-implementation
```

Current HEAD at acceptance time:

```text
c1fc9d55d fix(formal-pool): allow operational dashboard labels
```

## 1. Scope

This report records the local, non-production acceptance state for the Formal Pool hard-gate, diagnostics, and real-time status dashboard work.

It covers:

- new-account hard-gate and recovery visibility;
- Formal Pool diagnostics and operator guidance;
- Formal Pool real-time dashboard;
- local staging/mock artifact generation;
- local test and sensitive-scan evidence.

It does not approve production deployment, production data changes, server restarts, or real Anthropic directed healthchecks.

## 2. Safety boundaries observed

During this acceptance pass:

- no production server was changed;
- no service was restarted;
- no production database, Redis, raw capture, or ledger data was modified;
- no real Anthropic request was sent;
- no real directed healthcheck was run;
- no production account state was changed.

## 3. Relevant committed changes

Recent commits included in the current HEAD:

```text
c1fc9d55d fix(formal-pool): allow operational dashboard labels
f281df764 fix(formal-pool): fail closed on missing dashboard runtime counters
cb20fe868 docs(formal-pool): document realtime status dashboard
e29003034 feat(staging): emit mock smoke checklist
609dcd9ab fix(formal-pool): tighten dashboard and diagnostic signals
773fce24b fix(formal-pool): guide rate limit recovery diagnostics
1005bdf72 docs(formal-pool): clarify dashboard state priority
04a1ba3d8 fix(formal-pool): tighten dashboard risk classification
```

Key properties now covered by code and tests:

- backend dashboard endpoint is admin-only and returns all Formal Pool accounts independent of account-list pagination;
- dashboard state classification is backend-owned;
- state priority is: inactive, manual risk, rate-limited, quarantined, error, not schedulable, evidence missing, data missing, warming, production, normal;
- 401/403/hold/KYC/unusual/risk signals display as manual intervention;
- 429/rate-limit signals display as cooldown/waiting recovery, not as generic account failure;
- missing runtime counters fail closed to data-missing when a limit is enabled;
- missing runtime or healthcheck evidence fails closed to evidence-missing;
- ordinary operational account labels, including email-style names, may be displayed;
- token-like labels, raw request markers, UUID-like identifiers, and proxy credentials are redacted or replaced with `账号 #<id>`;
- the dashboard is read-only and does not run healthchecks or mutate accounts.

## 4. Local staging/mock artifact evidence

Generated local artifacts:

```text
/tmp/sub2api-formal-pool-localhost-preflight-20260601083513
```

Files generated:

```text
runtime-manifest.json
cc-gateway.yaml
start-runtime.sh
server-staging-mock-smoke.md
server-staging-mock-smoke.checklist.json
```

Verified artifact gates:

| Gate | Result |
| --- | --- |
| runtime mode is `localhost-preflight` | PASS |
| CC Gateway upstream is `http://127.0.0.1:19082` | PASS |
| real Anthropic canary switch is false | PASS |
| real Anthropic production switch is false | PASS |
| manifest `requires_real_anthropic` is false | PASS |
| checklist forbids real Anthropic request | PASS |
| artifact sensitive scan | PASS, findings=0 |

This artifact set is suitable for a server-side localhost/mock smoke only. It is not approval for a real canary or production switch.

## 5. Verification commands and results

### Sub2API targeted Go verification

```bash
cd backend
go test ./internal/service ./internal/handler ./internal/server/routes -run 'FormalPool|StatusDashboard|Account|RateLimit|DTO|CCGateway|ControlPlane' -count=1 -timeout=240s
```

Result:

```text
ok github.com/Wei-Shaw/sub2api/internal/service
ok github.com/Wei-Shaw/sub2api/internal/handler
ok github.com/Wei-Shaw/sub2api/internal/server/routes
```

### Frontend verification

```bash
cd frontend
npm run test:run -- FormalPoolStatusDashboardModal FormalPoolDiagnosticsModal formalPoolStatusDashboard AccountsView.bulkEdit
npm run typecheck
```

Result:

```text
4 test files passed
47 tests passed
vue-tsc --noEmit passed
```

### Python tools verification

```bash
PYTHONPATH=. python3 -m unittest discover -s tools/tests -v
```

Result:

```text
Ran 139 tests
OK
```

### Sensitive scan

```bash
python3 tools/safe_deliverable_sensitive_scan.py --max-findings 100
python3 tools/safe_deliverable_sensitive_scan.py --root docs/anti-ban/43-formal-pool-status-dashboard.md --max-findings 100
python3 tools/safe_deliverable_sensitive_scan.py --root /tmp/sub2api-formal-pool-localhost-preflight-20260601083513 --max-findings 100
git diff --check
```

Result:

```text
findings=0
findings=0
findings=0
git diff --check passed
```

### CC Gateway verification

Directory:

```text
/Users/muqihang/chelingxi_workspace/cc-gateway
```

Commands:

```bash
npm run build
npm test -- --runInBand
```

Result:

```text
build passed
104 passed, 0 failed
```

Note: the CC Gateway directory had existing untracked local folders `.claude/` and `.worktrees/`; no CC Gateway source changes were made during this acceptance pass.

## 6. Acceptance checklist

| Requirement | Evidence | Status |
| --- | --- | --- |
| Account list layout remains separate from dashboard | `AccountsView.vue` uses compact `DataTable` and opens independent dashboard modal | PASS |
| Dashboard is independent full-screen read-only UI | `FormalPoolStatusDashboardModal.vue` and tests | PASS |
| Dashboard auto-refreshes and aborts on close | frontend modal tests | PASS |
| Backend returns all Formal Pool accounts, not current page only | route/service tests | PASS |
| 401/403/hold/KYC/risk are manual-intervention states | backend classification tests and docs | PASS |
| 429/rate-limit is cooldown/waiting recovery, not generic quarantine | backend/frontend tests and docs | PASS |
| missing identity/egress evidence is explained as evidence-chain issue, not account ban | docs and dashboard classification text | PASS |
| enabled runtime counters missing fail closed to data-missing | backend tests | PASS |
| missing runtime/healthcheck evidence fails closed to evidence-missing | backend tests | PASS |
| ordinary operational labels and email-style names can display | backend and route tests | PASS |
| tokens/raw/proxy/UUID-like data are not displayed | backend, route, frontend tests and scans | PASS |
| localhost-preflight artifact forbids real Anthropic request | generated checklist and assertions | PASS |
| no production deployment or real healthcheck occurred | operator boundary of this pass | PASS |

## 7. Remaining steps before production changes

The next stage requires explicit operator approval because it touches server runtime state.

Recommended order:

1. server staging build from current HEAD;
2. preserve production data, Redis, raw/capture/log directories;
3. start localhost mock upstream only;
4. start CC Gateway with localhost-preflight config;
5. run server localhost/mock smoke;
6. verify raw capture safe summary and session budget JSONL;
7. run server-side sensitive scan on generated artifacts and safe summaries;
8. only after server mock passes, separately request approval for any real directed healthcheck;
9. only after directed healthcheck passes, separately request approval for warming or production scheduling changes.

Do not combine server mock smoke, real directed healthcheck, and production account state changes into one implicit step.
