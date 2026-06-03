# Staging deployment readiness runbook

Status: required before small-flow production operation.

This runbook defines the staging gate for the Claude Code formal pool. Staging is a pre-production runtime verification stage. It may use the same deploy shape as production, but it is not full rollout. If any real user traffic is allowed during staging, it must be tiny, reversible, and continuously observed.

## 1. Mode boundaries

- `localhost-preflight`: localhost upstream only; never reaches real Anthropic or Claude domains.
- `real-canary`: one-off approved canary only; not a production mode.
- `production-session`: production-capable runtime; requires explicit production switch and must not inherit canary cost envelope or low capability caps.

Production-session must keep Claude Code capability intact: 1m-capable profiles, Sonnet/Opus model families, tools, thinking, context management, streaming, and `max_tokens=32000` are not reduced by Session Budget.

## 2. Required switches

Before staging with any real traffic:

- Confirm Sub2API build path is the worktree source of truth.
- Confirm CC Gateway config comes from generated production-session artifacts, not hand-edited canary config.
- Set `SUB2API_SESSION_BUDGET_EXPORT_PATH` to a restricted JSONL path for redacted ledger export.
- Confirm `ALLOW_REAL_ANTHROPIC_PRODUCTION=1` is only present for production-session validation and never for localhost-preflight.
- Confirm `ALLOW_REAL_ANTHROPIC_CANARY` is absent or `0` outside explicit canary.
- Confirm upstream host is either localhost for preflight or the approved Anthropic host for production-session; nonlocal third-party upstreams are forbidden.


## 2.5 Localhost/mock smoke checklist for Account Management V2

This checklist is the only allowed preflight path before any real canary or production-session work. It is safe to run only when every item below is true.

### 2.5.1 Preconditions

- Worktree source of truth is the reviewed V4 branch and the expected commit hash is recorded in the smoke notes.
- `use_new_account_management_ux` remains `false` before the first smoke request, so legacy rollback is the initial state.
- `ALLOW_REAL_ANTHROPIC_PRODUCTION` is unset or `0`.
- `ALLOW_REAL_ANTHROPIC_CANARY` is unset or `0`.
- Any upstream or proxy target used by the smoke environment is loopback-only: `localhost`, `127.0.0.1`, or `::1`.
- No operator imports real Setup Tokens, OAuth codes, refresh tokens, account cookies, or proxy credentials for this smoke.
- Session-budget export, if enabled, writes only to a local restricted path and is scanned before sharing.

### 2.5.2 Build and launch boundaries

- Build the backend and frontend from the reviewed worktree only.
- Launch the server in a localhost-only/mock configuration. Do not reuse a production-session config, production upstream URL, or real proxy pool.
- Before opening the UI, inspect the runtime environment and config for forbidden values:
  - real Anthropic or Claude hostnames;
  - non-loopback upstream URLs;
  - `ALLOW_REAL_ANTHROPIC_PRODUCTION=1`;
  - `ALLOW_REAL_ANTHROPIC_CANARY=1`;
  - real token-shaped material or proxy credentials.
- If any forbidden value is present, stop before the first request and return to config review.

### 2.5.3 Smoke sequence

1. Start with the runtime flag off and confirm legacy Dashboard, Diagnostics, and Onboarding remain reachable.
2. Enable `use_new_account_management_ux` only in the localhost/mock environment.
3. Open Dashboard V2 and verify:
   - four lanes are visible: active, paused, needs-intervention, inactive;
   - warming rows show `low weight` copy;
   - row DOM references and fallback labels do not include raw account identifiers.
4. Open Diagnostics V2 and verify:
   - no fake one-click OAuth reauthorization or fake replace-account API appears;
   - account replacement and reauthorization are guidance/navigation flows;
   - 5h rate-limit scenarios do not offer a default healthcheck action;
   - evidence text is scrubbed and bucketed.
5. Open Onboarding V2 and verify:
   - StartSession creates an idle session without rendering a browser-egress URL;
   - proxy-test mock can produce a browser-egress check state without rendering a raw nonce;
   - copy-link behavior does not write raw nonce material into the DOM;
   - expiry uses `重新开一个上号会话`;
   - healthcheck copy warns that a real directed request would be sent, but the smoke must not click a real healthcheck path.
6. Turn the runtime flag off and confirm the legacy UI returns without rebuilding the frontend.

### 2.5.4 Evidence and exit rules

- Save only screenshots or notes that contain scrubbed labels, buckets, and route templates.
- Run the safe-deliverable sensitive scan before sharing any smoke artifact.
- Stop immediately if a network log, access log, ledger export, screenshot, or DOM snapshot contains raw nonce, token-shaped material, proxy credentials, raw egress values, real account identifiers, or a non-loopback upstream hostname.
- Passing localhost/mock smoke does not approve real canary, directed healthcheck, start warming, or production-session rollout. Each requires a separate explicit approval.

## 3. Observe-only budget checks

Session Budget Phase 1 is observe-only except explicit P0 safety failures. It must record redacted ledger evidence for:

- 2xx success;
- 400 validation failure;
- 401 and 403 auth/risk classes;
- 429 cooldown;
- CC Gateway control-plane rejection;
- verifier/fallback/proxy mismatch and risk text quarantine.

The ledger export must contain only scoped references, buckets, status classes, decision actions, and safe reason codes. It must not contain local credentials, prompts, request payload bytes, response payload bytes, account identifiers, or proxy secrets.

## 4. Normal and aggressive pools

- `normal`: target smooth 7-day 90%-100% weekly utilization.
- `aggressive`: target 3-day 95%-100% weekly utilization.

In staging these profiles are scheduling signals only: account weight, queue priority, catch-up, slow-down, and cooldown recommendations. They must not rewrite requests, reduce model capability, or bypass P0 safety.

## 5. Kill switches and rollback

Before any real traffic, confirm operators can:

- set production upstream switch off;
- pause the queue for one account, one pool, or all pools;
- quarantine one account;
- disable aggressive scheduling;
- revert to localhost-preflight;
- stop the local guard and CC Gateway processes;
- preserve only safe summaries for reporting.

Stop immediately if any of these occurs: verifier failure, fallback, proxy mismatch, 401/403 risk class, 429 storm, unusual-activity or KYC-style text, control-plane unknown path, sensitive scan finding, or third-party upstream configuration.

## 6. Telemetry posture

Telemetry and eval payloads are not uploaded in staging. They remain suppressed or converted to safe intent summaries until synthetic telemetry shadow-only is implemented and separately approved. Shadow-only means producing the safe event we would upload, storing only a safe summary, and comparing it against expected shape without sending it upstream.

## 7. Staging exit criteria

Staging can move to small-flow formal operation only when:

- localhost-preflight passes;
- production-session config validation passes;
- Session Budget export is present and scan-clean;
- 2xx, 400, 401/403, 429, and CC Gateway control-plane paths all produce redacted ledger evidence;
- CC Gateway build and tests pass;
- Go targeted tests pass;
- safe deliverable scan finds zero findings;
- rollback and queue pause are documented and tested by operator dry run.

Small-flow formal operation starts with one or two accounts, normal pool first, aggressive pool observed before ramp. Full rollout and telemetry upload require separate approval.
