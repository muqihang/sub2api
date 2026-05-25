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
