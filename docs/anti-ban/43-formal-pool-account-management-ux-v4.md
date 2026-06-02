# Formal Pool Account Management UX V4 Sync Memo

> Scope: Task 10 documentation sync and static draft safety review for Dashboard V2, Diagnostics V2, and Onboarding V2. This memo describes implemented/expected behavior without real credentials, real network addresses, real account identifiers, real organization identifiers, real email addresses, or proxy secrets.

## 1. V4 three-panel overview

V4 keeps the legacy account-management experience available and adds three runtime-flagged V2 surfaces for formal-pool operators:

1. **Dashboard V2**: a real-time pool overview that compresses many fine-grained statuses into four operator buckets and pins accounts that need action.
2. **Diagnostics V2**: a guided repair panel with root-cause hero messaging, allowed-action matrices, grouped evidence, search, and explicit forbidden-action boundaries.
3. **Onboarding V2**: a vertical stepper for adding Claude formal-pool accounts, with proxy validation before browser egress verification, explicit healthcheck consent, and safe recovery paths.

The three panels share the same safety posture: show buckets, labels, and scrubbed references only; never surface credentials, full login material, proxy secrets, direct browser egress values, or raw upstream evidence payloads.

## 2. Runtime feature flag and rollout control

- Runtime setting key: `use_new_account_management_ux`.
- Default: `false`.
- Production enablement: an admin turns the flag on from backend Settings after deployment.
- Rollback: turn the same setting off; the UI returns to legacy components without rebuilding the frontend.
- Local development override: `VITE_NEW_ACCOUNT_UX` is only a dev override for local testing. It is not the production control plane.
- Legacy components remain mounted behind the negative branch of the runtime flag, so a flag-off rollback keeps existing operator flows available.

## 3. Backend hardening summary

### 3.1 Public browser egress route redaction

The public browser verification route must return only constant safe responses and sanitized status. Access logging for that route must redact the client address and nonce path segment, using a route template or bucketed reference instead of request-specific values.

### 3.2 Side-effect-free proxy egress probe

Browser verification uses a dedicated proxy egress probe that is side-effect-free for this flow. It must not call the normal admin proxy-test path, must not update proxy latency records, and must not persist discovered direct egress values. Probe results live only in bounded backend memory/cache with configured TTLs and timeout handling.

### 3.3 Risk writer and orphan events

Risk events are written through the formal-pool risk writer using sanitized ledger entries. Public-route rate-limit events can be orphan events without account/session context. They must contain only safe event kind, reason bucket, nonce bucket, and address bucket; they must not contain credentials, account identifiers, session identifiers, proxy URLs, or direct egress values.

### 3.4 Rate limiter

The public browser route is protected by per-nonce, per-address-bucket, and total-per-nonce limits. Redis keys use HMAC/bucketed forms rather than request-specific nonce strings. Redis-unavailable fallback is fail-safe and records a sanitized rate-limit event.

### 3.5 Nonce lifecycle

- `StartSession` creates an idle onboarding session and returns no browser-check URL.
- `TestProxy` validates the selected proxy, captures a safe proxy bucket, then mints the browser-check nonce and unlocks the check URL.
- Existing-session nonce regeneration is not part of V4. If the nonce expires, operators start a new onboarding session.
- Empty nonce must produce no public URL.
- If nonce expiry races with verification, the result is nonce-expired, not success or mismatch based on stale state.

## 4. Dashboard V2 requirements

Dashboard V2 maps detailed backend states into four operator buckets:

| Bucket | Meaning | Operator behavior |
| --- | --- | --- |
| `active` | Production-ready or warming accounts | Normal observation; warming remains low weight |
| `paused` | Temporarily unschedulable, commonly quota/rate-window related | Wait, inspect, or refresh diagnostics |
| `needs_intervention` | Requires human action, missing evidence, risk, credential expiry, or quarantine | Pinned above normal rows with primary repair CTA |
| `inactive` | Disabled or intentionally out of service | Hidden unless selected or filtered |

Specific Dashboard V2 sync points:

- Warming accounts are still counted as usable but must be visually distinct and copy must include `low weight`.
- `needs_intervention` rows are pinned/highlighted and carry direct diagnostic CTAs.
- Sensitive values are scrubbed before rendering. The dashboard may show safe status buckets, last-safe error buckets, coarse timestamps, and human-readable recommendations only.
- Detailed runtime metrics stay in drill-down panels, not the main table.

## 5. Diagnostics V2 requirements

### 5.1 Hero action matrix

Diagnostics V2 starts with a root-cause hero and an action matrix. Each scenario declares a primary safe action, secondary manual actions, and forbidden actions.

| Scenario | Primary safe action | Secondary/manual action | Forbidden action |
| --- | --- | --- | --- |
| Credential refresh failure | Open manual reauthorization guidance / new onboarding navigation | Inspect evidence and proxy grouping | Fake OAuth reauth button, automatic credential rotation, hidden backend repair API |
| 5h rate-window exhaustion | Wait or refresh diagnostics | Manual healthcheck only when operator understands quota impact | Promote production or bypass scheduler guard |
| Proxy mismatch / fallback | Swap proxy or open onboarding guidance | Runtime register after proxy repair, then healthcheck | Direct healthcheck before proxy repair |
| Evidence missing | Runtime register or healthcheck | Inspect grouped evidence | Promote production without evidence |
| Risk/hold/KYC | Manual review and quarantine guidance | Keep account isolated | Auto-repair, auto-promote, or credential reuse |

### 5.2 Forbidden actions

Diagnostics V2 must not present:

- a fake OAuth reauth API button;
- any button implying automatic credential regeneration;
- a one-click account rebirth flow;
- direct backend `replace account` API behavior from the diagnostics panel;
- unsafe production promotion while evidence is incomplete;
- hidden bypasses for quarantine, rate-window, or proxy-mismatch states.

### 5.3 Quarantine normalize

Quarantine/risk states normalize into a safe bucket and a human-readable reason. UI copy should say that the account is isolated or requires manual review, while raw upstream payloads, full account identifiers, and direct provider messages remain scrubbed.

### 5.4 Evidence grouping and search

Evidence is grouped by lifecycle area: authorization, proxy/browser egress, runtime registration, healthcheck, warming, rate-window, and quarantine/risk. Search filters only scrubbed labels and buckets. Evidence panels may display safe status-code buckets, coarse time buckets, and sanitized event references.

### 5.5 Sensitive scrub

Diagnostics V2 must scrub credentials, browser-session material, direct egress values, provider payloads, proxy URLs, account identifiers, organization identifiers, email addresses, and token-shaped strings. The static draft uses frontend navigation text for account replacement guidance: operators are sent to a new onboarding flow with prefilled safe context; it is not a backend replacement API.

## 6. Onboarding V2 requirements

### 6.1 Vertical stepper

Onboarding V2 is a vertical stepper with explicit stage boundaries:

1. Proxy and account policy setup.
2. Browser egress verification.
3. Authorization or Setup Token exchange.
4. Healthcheck and warming handoff.

### 6.2 StartSession idle, TestProxy unlock

- `StartSession` returns an idle session without a browser-check URL.
- `TestProxy` must succeed before the browser-check URL is shown.
- After `TestProxy`, the operator opens the check URL in the same browser/proxy context used for Claude login.

### 6.3 Expired CTA

When the browser-check nonce expires, the CTA must say `重新开一个上号会话`. It must not use wording that implies regenerating the check link inside the same session.

### 6.4 Mismatch bucket-only display

Browser/proxy mismatch messages use bucket labels only, for example `browser bucket != proxy bucket`. The UI must not display direct browser egress values, direct proxy egress values, or proxy credentials.

### 6.5 Poller abort rules

The egress-check poller stops when:

- the session reaches a terminal success state;
- the nonce expires;
- the user leaves the stepper or closes the panel;
- a new session replaces the current one;
- the component unmounts;
- repeated safe error buckets indicate no further progress without operator action.

Abort must cancel the in-flight polling request and avoid updating stale sessions.

### 6.6 Healthcheck real-request prompt

Before healthcheck, the UI must clearly state that a very small real `messages` request will be sent through the selected proxy and CC Gateway, may consume a tiny amount of quota, and will be captured for evidence. Operators must explicitly start this step; it is not automatic after token exchange.

## 7. Operations and rollback

- Default deployment keeps V4 off.
- To enable: deploy backend/frontend, confirm Settings exposes `use_new_account_management_ux`, then turn it on in admin Settings.
- To rollback UI: turn the setting off. Legacy Dashboard, Diagnostics, and Onboarding routes/components become active again without frontend rebuild.
- To rollback local testing: unset `VITE_NEW_ACCOUNT_UX` or set it according to local dev needs; do not treat it as production rollout state.
- Do not deploy with real canary traffic as part of this documentation task.

## 8. Do-not-output sensitive-field rules

Never output or persist in V2 UI, drafts, docs, tests, snapshots, logs, or risk events:

- full browser egress values or proxy egress values;
- proxy URLs, proxy credentials, proxy usernames, or proxy passwords;
- OAuth refresh/access tokens, Setup Token login material, authorization codes, or token-shaped placeholders;
- real account identifiers, organization identifiers, session identifiers, or email addresses;
- raw provider payloads, raw request/response bodies, or unsanitized headers;
- nonce values, except as bucketed/HMAC references where needed for rate limiting or audit correlation.

Allowed output is limited to safe status buckets, safe reason buckets, route templates, coarse time buckets, redacted references, and operator guidance.

## 9. Acceptance and test command summary

Static/document checks:

```bash
# Run the Task 10 forbidden-string scan against the three drafts and this memo.
# Keep the literal pattern in the terminal command, not in checked-in docs, so the memo itself stays clean.
PYTHONPATH=. python3 tools/safe_deliverable_sensitive_scan.py --max-findings 100
```

Backend regression groups used by V4 hardening:

```bash
go test ./backend/internal/config ./backend/internal/handler ./backend/internal/server ./backend/internal/service
```

Focused backend examples:

```bash
go test ./backend/internal/service -run 'FormalPool|ProxyEgress|RiskEvent|RateLimiter|Healthcheck|Setting'
go test ./backend/internal/handler -run 'FormalPool|Setting'
go test ./backend/internal/server -run 'FormalPool|PublicSettings|AccessLog|Contract'
```

Frontend regression groups used by V2 surfaces:

```bash
cd frontend && npm test -- --run src/utils/__tests__/featureFlags.spec.ts src/stores/__tests__/app.spec.ts src/views/admin/__tests__/AccountsView.dashboardFlag.spec.ts src/views/admin/__tests__/ClaudeOnboardingWizardView.flag.spec.ts src/views/admin/__tests__/SettingsView.spec.ts
cd frontend && npm test -- --run src/components/account/__tests__/FormalPoolStatusDashboardModalV2.spec.ts src/components/account/__tests__/FormalPoolDiagnosticsModalV2.spec.ts src/components/account/__tests__/ClaudeFormalPoolOnboardingWizardV2.spec.ts src/composables/__tests__/useEgressCheckPolling.spec.ts
```

Manual UX review:

- Open the three static drafts and confirm no fake links, fake credential repair buttons, or sensitive placeholders are present.
- Confirm Dashboard V2 shows four buckets and warming `low weight` copy.
- Confirm Diagnostics V2 uses guidance/navigation for reauthorization/replacement, not hidden backend repair APIs.
- Confirm Onboarding V2 shows no browser-check URL before proxy validation and uses `重新开一个上号会话` for expiry.
