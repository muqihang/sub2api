# Harness Engineering SOR: Claude Code formal pool staging to small-flow operation

Date: 2026-05-25
Status: staging runtime deployed; small-flow production mode enabled on 2026-05-26; operator-watched only
Source of truth: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation`
CC Gateway source: `/Users/muqihang/chelingxi_workspace/cc-gateway`

This document is the handoff entry point for any future agent. If conversation context is lost, start here before making changes, deploying, or running real traffic.

## 1. Current decision

We are ready to deploy a staging runtime and then run very small-flow formal operation if staging passes.

Staging means pre-production verification. It may use production-capable configuration, but it is not broad production rollout. If user traffic is allowed during staging, it must be tiny, reversible, and continuously observed.

Do not treat staging as full traffic launch. Small-flow formal operation begins only after the staging checks in this document pass.

## 2. What has been proven

Local verification has passed for the current worktree and CC Gateway state:

- Python tools suite: 125 tests OK.
- Go targeted OAuth/repository tests: PASS.
- Go targeted service/handler/routes tests: PASS.
- CC Gateway build: PASS.
- CC Gateway test suite: 97 tests PASS.
- Safe deliverable sensitive scan: 139 files scanned, 0 findings.
- CC Gateway required docs/config scan: 0 findings.
- Localhost-only full-chain controller: PASS.
  - Scenario A: unsafe/high envelope blocked before localhost mock, mock count 0.
  - Scenario B: safe messages path reached localhost mock, mock count 1.
  - real upstream flag: false.
  - sensitive scan: PASS.

No real request was sent during this hardening pass.

## 3. Deployment artifact policy

### Sub2API

Use the root multi-stage Dockerfile:

`/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/Dockerfile`

This builds frontend assets, compiles the Go backend into a release binary, and copies only the runtime binary/resources into the final image.

Do not use:

`/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/backend/Dockerfile`

That backend Dockerfile is a simple build image and is not the preferred staging/production artifact because it is less controlled for deployment packaging.

### CC Gateway

Use:

`/Users/muqihang/chelingxi_workspace/cc-gateway/Dockerfile`

This builds TypeScript and runs compiled JavaScript. It does not ship TypeScript source, but the runtime JavaScript remains visible inside the image. If stronger code hiding is needed, create a later packaging task; do not block today's staging on that unless the operator explicitly requires it.

## 4. Runtime modes and switches

- `localhost-preflight`: localhost upstream only; no real upstream.
- `real-canary`: one-off approved canary only; not production.
- `production-session`: production-capable formal-pool mode.

For production-session staging:

- `ALLOW_REAL_ANTHROPIC_PRODUCTION=1` is required only when intentionally validating real production upstream behavior.
- `ALLOW_REAL_ANTHROPIC_CANARY` must be absent or disabled outside explicit canary.
- Production-session must not inherit canary cost envelope, low body caps, or one-message canary hard gate.
- Session Budget remains observe-only except explicit P0 safety boundary failures.
- Nonlocal third-party upstreams are forbidden in real modes.

## 5. Capability policy

Do not reduce Claude Code capability for production-session:

- Keep 1m-capable profile support.
- Do not block Sonnet/Opus Claude Code model families by default.
- Do not remove tools, tool loops, thinking, context management, streaming, or max output capability from normal requests.
- Do not rewrite user request body for budget reasons.
- Do not rewrite response body for budget reasons.

The budget system is for observation, scheduling, cooldown, quarantine, and safety. It is not a capability downgrade layer.

## 6. Monitoring and data collection currently available

Available now:

- Sub2API health check.
- CC Gateway health check.
- Redacted Session Budget JSONL export via `SUB2API_SESSION_BUDGET_EXPORT_PATH`.
- Budget observation for success, validation errors, auth/risk classes, rate-limit classes, and CC Gateway control-plane rejections.
- Normal/aggressive pool recommendation fields: account weight, queue priority, catch-up, slow-down, cooldown.
- Sensitive scan for safe deliverables and staging docs/artifacts.
- CC Gateway safety gates for upstream mode, persona, signing, fallback, proxy bucket, and route policy.

Not yet complete:

- Full graphical operations dashboard.
- Automated alerting pipeline.
- Fully automated normal/aggressive account scheduler consumption.
- Synthetic telemetry shadow-only implementation.
- Real telemetry upload. This remains disabled until separately approved.

Therefore the first live phase must be very small-flow and operator-watched.

Data retention for this live phase is governed by:

- `docs/anti-ban/40-real-operation-data-retention-strategy.md`

Short version: raw evidence stays only on the restricted server runtime path; the worktree only receives scan-clean safe deliverables and aggregated summaries. Do not move raw request bodies, prompts, tokens, CCH, account UUIDs, email, or proxy credentials into this repository.

## 7. Staging deployment checklist

Before starting containers:

1. Confirm both repositories are at the intended worktree state.
2. Confirm no main-root drift is used as source of truth.
3. Build Sub2API using the root multi-stage Dockerfile.
4. Build CC Gateway using the CC Gateway Dockerfile.
5. Use generated production-session CC Gateway config, not a hand-edited canary config.
6. Set `SUB2API_SESSION_BUDGET_EXPORT_PATH` to a restricted JSONL path inside a mounted runtime directory.
7. Confirm ledger directory permission policy: operator-readable only; no public web exposure.
8. Confirm real-mode upstream target is approved and not a third-party host.
9. Confirm queue pause, account quarantine, aggressive-disable, and full rollback actions are known.

Staging smoke sequence:

1. Start localhost-preflight runtime and verify no real upstream.
2. Start production-session runtime without user traffic.
3. Send a controlled smoke request through the intended public entry path.
4. Confirm request succeeds or fails safely.
5. Confirm Session Budget JSONL line is produced.
6. Run sensitive scan on the exported safe artifacts/log summary path.
7. Check CC Gateway logs for no raw token, raw body, raw prompt, raw CCH, raw account id, or proxy credential.
8. Confirm no fallback, no sign-to-strip downgrade, no proxy mismatch, and no unexpected control-plane route.

Small-flow entry requires all smoke checks to pass.

## 8. Small-flow operation policy

Start with one or two accounts only.

Recommended order:

1. Normal pool first.
2. Aggressive pool observe-only first.
3. Increase aggressive scheduling only after normal pool is stable.
4. Do not enable telemetry upload.
5. Keep synthetic telemetry work as a separate next phase.

Stop or pause immediately on:

- verifier failure;
- fallback or sign-to-strip downgrade;
- proxy/account mismatch;
- repeated rate-limit class responses;
- auth/risk class responses;
- unusual-activity or KYC-style response text;
- unknown control-plane route;
- sensitive scan finding;
- nonlocal third-party upstream configuration;
- missing ledger export during real traffic.

## 9. Normal and aggressive pool strategy status

Implemented status: observe-only and scheduling recommendation layer.

- Normal: target smooth 7-day 90%-100% utilization.
- Aggressive: target 3-day 95%-100% utilization.

Current behavior:

- They influence recommendations and future scheduling fields.
- They do not hard-block normal Claude Code capability.
- They do not mutate request or response payloads.
- They do not bypass safety quarantine or cooldown.

Do not claim full automated pool optimization until scheduler consumption, dashboard, and alerting are completed.

## 10. Telemetry status

Current staging posture:

- Raw telemetry/eval payloads are not uploaded.
- Telemetry/eval remains suppress or safe-intent only.
- Synthetic telemetry shadow-only is designed but not yet implemented.

Meaning of shadow-only:

- Generate the safe event we would later upload.
- Store only safe summaries.
- Compare shape, timing, and safety.
- Do not send it upstream.

Real telemetry upload requires a later separate approval.

## 11. Commit and release discipline

There are many modified and untracked files in both the Sub2API worktree and CC Gateway repository. This is expected after the B1/B2/session-budget/runtime hardening work, but it is not safe to leave the deploy state uncommitted for long.

Recommendation before server deployment:

1. Do not commit blindly.
2. Review diff by domain.
3. Run the verification set again.
4. Commit in grouped changes so rollback is understandable.

Suggested commit groups:

1. Sub2API OAuth/proxy/fail-closed and Claude Code adapter gates.
2. Sub2API control-plane guard/intent/attestation/cache/quarantine.
3. Sub2API Session Budget observe-only ledger and staging export.
4. Sub2API docs and safe deliverable cleanup.
5. CC Gateway persona/resolver/upstream-safety/canary-cost gates.
6. CC Gateway tests and docs.

Do not run `git add` or `git commit` until the operator explicitly approves committing. When committing, avoid raw artifacts and inspect untracked directories before staging.

## 12. Handoff map

Primary docs:

- Staging runbook: `docs/anti-ban/staging/staging-deployment-readiness-runbook.md`
- This SOR: `docs/anti-ban/staging/2026-05-25-harness-engineering-sor.md`
- Real operation data retention: `docs/anti-ban/40-real-operation-data-retention-strategy.md`
- Session Budget design: `docs/anti-ban/39-formal-pool-session-budget-strategy.md`
- Session Budget implementation memo: `docs/anti-ban/session-budget-phase1-implementation-memo.md`
- Control-plane upload strategy: `docs/anti-ban/35-formal-pool-control-plane-upload-strategy.md`
- Synthetic telemetry design: `docs/anti-ban/38-formal-pool-synthetic-telemetry-strategy.md`
- Runtime artifacts: `docs/anti-ban/runtime-productization/2026-05-24-cli-through/`

Primary verification commands:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation
PYTHONPATH=. python3 -m unittest discover -s tools/tests -v
python3 tools/safe_deliverable_sensitive_scan.py --max-findings 100
```

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/backend
go test ./internal/pkg/oauth ./internal/repository -run 'BuildAuthorizationURL|OAuth|Proxy|Refresh' -count=1 -timeout=120s
go test ./internal/service ./internal/handler ./internal/server/routes -run 'InferenceScope|CCGateway|ControlPlane|EventLogging|LocalCapture|OAuth|GatewayForward|ExplicitCanary|Adapter|AnthropicAPIKey|StrictPassthrough|JointLocalCaptureAcceptanceArtifact|Persona|Session|Budget|Utilization|Risk' -count=1 -timeout=180s
```

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway
npm run build
npm test -- --runInBand
```

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation
python3 tools/cli_control_plane_full_chain_controller.py
```

## 13. Next operator action

If this document is current and verification remains green, the next action is:

1. Decide whether to commit before deployment.
2. Build Docker images from the documented source roots.
3. Deploy staging runtime.
4. Run staging smoke.
5. Confirm ledger export and scan-clean artifacts.
6. Only then allow tiny small-flow user traffic.


## Claude formal pool onboarding wizard

Status: implemented for localhost/mock readiness. The wizard uses backend-only OAuth exchange/create, registers the CC Gateway runtime account identity and egress bucket mapping, creates accounts unschedulable, writes formal-pool CC Gateway defaults, runs acceptance without real Anthropic/Claude messages, and requires manual activation. Real OAuth login/smoke remains blocked pending separate approval.
