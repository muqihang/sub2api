# 29 - Single-server deployment and canary prep

> **Status:** Preparation handbook only. Do not deploy, log in, or send real Anthropic traffic from this document without separate user approval.
> **Basis:** `27-first-wave-shared-pool-messages-only-design.md`, `28-first-wave-shared-pool-messages-only-rollout-plan.md`, `14-cc-gateway-shared-pool-compatibility-plan.md`, `25-claude-code-2146-reverse-coverage-and-signing-readiness-gates.md`, and `captures/real-baseline/2026-05-21-signing-readiness-gate-status-memo.md`.
> **Scope:** One Ubuntu server running Sub2API + CC Gateway together, with local integration first and later preparation for a single-account, extremely small, sign-primary, messages-only canary.

---

## 1. Purpose and scope

This document prepares a **single-server** deployment model for the first-wave Anthropic shared-pool path:

- one Ubuntu server;
- Sub2API and CC Gateway deployed on the same server;
- account configuration on the same server;
- one server egress IP;
- local logs/monitoring;
- explicit rollback configuration;
- local integration before any real canary.

This is **not** a full final signing-mode design and does not authorize a real canary. It is a single-server deployment and local-integration preparation handbook for the already narrowed first-wave messages-only scope. The only prepared route is native `/v1/messages` only.

First-wave policy remains:

- native `/v1/messages` only;
- `sign-primary` is the target default path;
- `strip-controlled` is only a manually approved exception for baseline sanity, diagnosis, emergency operator-approved fallback, cache optimization, or explicit opt-out;
- `disabled` is the safe state for unsupported routes, failed gates, or anomalies;
- no automatic fallback between lanes;
- no real Anthropic request until the user gives a separate explicit approval after local integration evidence is reviewed.

Current account reality:

- there is only one real Claude account available;
- therefore this plan can only prepare an **extremely small single-account canary**;
- it must not do real multi-user round-robin testing;
- it must not do high frequency;
- it must not do concurrency;
- it must not use complex prompts;
- it must not use `count_tokens`;
- it must not forward `event_logging` upstream;
- it must not use OpenAI-compatible Anthropic routes;
- it must validate only the messages-only sign-primary chain;
- any anomaly stops the canary immediately.

---

## 2. Server environment requirements

### 2.1 Operating system and tools

Target server:

- Ubuntu LTS, preferably Ubuntu 24.04 x86_64 or newer compatible LTS;
- non-shared host or VM with stable network identity;
- no unrelated production services sharing the same exposed management ports.

Required tools:

- Node.js and npm for CC Gateway;
- Go for Sub2API backend build/test;
- Python 3 for local capture and utility scripts;
- `jq` for config and JSON inspection;
- `curl` for local health checks only before real-canary approval;
- `openssl` for local TLS/cert inspection if local capture uses HTTPS;
- `git` for code checkout/update if deployment uses git;
- a process supervisor chosen by the operator, for example systemd or a minimal process manager.

Do not install long-running background services beyond what is required for Sub2API, CC Gateway, logging, and monitoring.

### 2.2 Time, network, and egress

Requirements:

- time synchronization enabled and verified, for example via `timedatectl`;
- one known fixed server egress IP for CC Gateway -> Anthropic during future canary;
- no rotating egress, NAT pool ambiguity, or untracked proxy changes;
- DNS behavior documented;
- outbound access reviewed before real canary;
- local integration mode must route CC Gateway to localhost capture, not real Anthropic.

The fixed egress IP becomes part of the account's egress bucket definition. If the egress IP changes, sign-primary canary must pause until the account/bucket mapping is re-reviewed.

### 2.3 Port planning

Recommended local-only port model:

| Component | Bind address | Example port | Exposure policy |
|---|---:|---:|---|
| Sub2API public/API listener | operator-selected public or private address | `3000` or existing service port | expose only if required by downstream clients |
| Sub2API admin/control-plane | `127.0.0.1` or private admin network | operator-selected | never public internet |
| CC Gateway listener for Sub2API | `127.0.0.1` or private server address | `8787` | prefer localhost on single server |
| Local capture upstream | `127.0.0.1` only | ephemeral, e.g. `9443` | local integration only |
| Metrics/log viewer | `127.0.0.1` or private admin network | operator-selected | never public internet |

Management ports must not be exposed publicly. If remote access is required, use SSH tunneling or a private management network; do not publish admin dashboards or control-plane ports on `0.0.0.0`.

### 2.4 Log directories

Suggested directories:

```text
/var/log/sub2api/
/var/log/cc-gateway/
/var/log/shared-pool-canary/
/opt/sub2api/
/opt/cc-gateway/
/opt/shared-pool-capture/
```

Safe deliverables should be copied back to the repository under:

```text
docs/anti-ban/captures/real-baseline/<DATE>-single-server-local-integration/safe-deliverable/
docs/anti-ban/captures/real-baseline/<DATE>-single-account-sign-primary-canary-prep/safe-deliverable/
```

Raw captures, if unavoidable during local-only tests, must remain in a clearly marked raw directory on the server and must not be committed.

---

## 3. Deployment topology

Single-server topology:

```text
downstream test client
  -> Sub2API on the same Ubuntu server
  -> CC Gateway on the same Ubuntu server, preferably 127.0.0.1:<cc_gateway_port>
  -> local capture upstream during integration
  -> Anthropic only after separate real-canary approval
```

Production-like future canary topology on the same server:

```text
downstream single test request
  -> Sub2API
  -> CC Gateway
  -> server fixed egress IP
  -> Anthropic /v1/messages only
```

The server owns one complete system:

- Sub2API;
- CC Gateway;
- account config;
- egress bucket config;
- logs/metrics;
- rollback configuration.

If a second server is introduced later, it is not a split Sub2API/CC-Gateway pair. It is a second complete system:

```text
server 1: Sub2API + CC Gateway + account pool A + egress bucket A
server 2: Sub2API + CC Gateway + account pool B + egress bucket B
```

The current one-account situation must not be treated as multi-account or multi-user coverage.

### 3.1 Account and egress binding

Each canary account must bind to:

- one upstream account identity record;
- one egress bucket id;
- one fixed server egress IP;
- one policy version;
- one route allowlist containing only `/v1/messages`;
- one lane policy, initially `disabled`, later manually approved `sign` for sign-primary canary.

There must be no default egress bucket fallback and no direct egress fallback.

---

## 4. Sub2API configuration checklist

Sub2API configuration must be prepared so that Sub2API remains the scheduler/governance layer and never becomes the final-output signer.

Required settings or policy state:

- CC Gateway integration enabled: `cc_gateway.enabled=true` or equivalent active config;
- Anthropic provider enabled for CC Gateway routing only after local integration passes;
- selected account `cc_gateway_enabled=true` only during approved local/canary windows;
- selected account `cc_gateway_policy_version=<approved_version>`;
- selected account route allowlist contains only native `/v1/messages`;
- selected account egress bucket id is present;
- `billing_cch_mode=sign` for sign-primary canary preparation;
- sign-primary account allowlist contains only the approved account hash/id;
- `strip-controlled` disabled by default;
- `strip-controlled` usable only with manual approval, route allowlist, account allowlist, policy version, and reason;
- `count_tokens` disabled/block/defer for both OAuth/setup-token and API-key passthrough paths;
- `event_logging` v2 and legacy suppress/block, never upstream forward;
- unknown `/api/event_logging/*` block;
- OpenAI-compatible Anthropic routes excluded from first wave;
- Antigravity excluded/disabled;
- no native direct fallback if CC Gateway returns control-plane error;
- no account failover on CC Gateway control-plane error;
- no account ban/death marking on CC Gateway control-plane error;
- no Sub2API billing block generation;
- no Sub2API CCH signing;
- no Sub2API final Claude Code persona synthesis;
- no user-supplied CCH/header/body passthrough as shared-account identity.

Control-plane errors from CC Gateway must be consumed as stable control-plane failures, not as Anthropic upstream failures.

---

## 5. CC Gateway configuration checklist

CC Gateway configuration must make CC Gateway the only final-output layer.

Required settings or policy state:

- `mode=sub2api`;
- gateway token configured for Sub2API -> CC Gateway authentication;
- gateway token redacted from logs;
- canonical Claude Code `2.1.146` persona source configured;
- canonical `User-Agent` configured;
- canonical `X-Stainless-*` headers configured;
- canonical `x-app`, `anthropic-version`, endpoint-specific `anthropic-beta`, and `Accept-Encoding` configured;
- per-account identity record present;
- per-account metadata/session normalization enabled;
- egress bucket present and enabled;
- connection/agent cache key includes provider + upstream account id + egress bucket + proxy identity hash;
- strict route allowlist limited to `/v1/messages` for first wave;
- strict header allowlist/normalizer enabled;
- sign-primary disabled by default until manual approval;
- sign-primary enable flag and kill switch present;
- strip-controlled disabled by default;
- strip-controlled enable flag and kill switch present;
- billing block generate/normalize implemented in CC Gateway;
- `cc_version` helper uses the verified formula:
  - `sha256("59cf53e54c78" + chars + cli_version)[:3]`
  - `chars = first_user_text positions [4,7,20] with "0" fallback`
- CCH signer inserts `cch=00000` placeholder before signing;
- CCH signer uses `xxh64(body_with_cch_00000, 0x4d659218e32a3268) & 0xFFFFF`;
- CCH emitted as lowercase zero-padded 5-hex;
- final compact JSON serialization occurs before CCH signing;
- post-sign verifier enabled;
- post-sign mutation detector enabled;
- verifier failure fails closed before egress;
- fixed control-plane error wire contract emitted:
  - `X-CC-Gateway-Error-Kind: control-plane`
  - `X-CC-Gateway-Error-Code: <stable_code>`
- log redaction enabled for tokens, Authorization, emails, account UUIDs, proxy credentials, raw body, raw prompt, raw CCH-bearing body, and setup/OAuth material.

Any missing identity, missing egress bucket, unknown route, disabled lane, proxy failure, signer failure, verifier failure, or post-sign mutation must fail closed. It must not direct-fallback.

---

## 6. Local integration steps, no real Anthropic

Local integration must happen before any real canary. It must route to localhost capture and must not touch real Anthropic.

### 6.1 Local integration topology

```text
local downstream test client
  -> Sub2API on server
  -> CC Gateway on same server
  -> localhost capture upstream, e.g. https://127.0.0.1:<capture_port>
```

Local capture rules:

- CC Gateway upstream base URL points to localhost capture;
- `api.anthropic.com` is not used;
- no real OAuth login;
- no new account login;
- no MITM;
- no raw secrets in safe deliverables;
- optional local TLS override is allowed only for localhost capture and must be documented.

### 6.2 Step A - health and config dry run

Verify locally:

- Sub2API process starts;
- CC Gateway process starts;
- Sub2API can authenticate to CC Gateway using gateway token;
- CC Gateway can reach localhost capture;
- policy version is visible in redacted logs;
- selected account hash and egress bucket id are visible in redacted logs;
- no management port is publicly exposed.

### 6.3 Step B - sign-primary final-output local capture

Run a local-only `/v1/messages` request through Sub2API -> CC Gateway -> localhost capture with `billing_cch_mode=sign`.

Verify safe output:

- route is `/v1/messages?beta=true` or the approved final messages path;
- Sub2API input to CC Gateway is pre-final;
- CC Gateway owns final header synthesis;
- CC Gateway owns final body rewrite;
- CC Gateway normalizes metadata/session account identity;
- canonical Claude Code `2.1.146` persona appears in final request summary;
- endpoint-specific beta appears in final request summary;
- billing block presence boolean is true;
- `cc_version` suffix verifier passes;
- final compact JSON serialization verifier passes;
- `cch=00000` placeholder was used before signing, recorded only as boolean/process summary;
- 5-hex CCH verifier passes, with raw CCH not written to safe deliverable;
- post-sign verifier passes;
- no post-sign mutation occurs;
- no real upstream attempt occurs.

### 6.4 Step C - route blocking and suppression

Verify local negative cases:

- `/v1/messages/count_tokens` is blocked/deferred;
- API-key passthrough `/v1/messages/count_tokens` is blocked/deferred if that path is enabled in local test harness;
- `/api/event_logging/v2/batch` is suppressed or blocked as configured;
- legacy `/api/event_logging/batch` is suppressed or blocked as configured;
- unknown `/api/event_logging/*` is blocked;
- `/v1/chat/completions` -> Anthropic conversion is excluded from first wave;
- `/v1/responses` -> Anthropic conversion is excluded from first wave;
- Antigravity provider is disabled/excluded;
- unknown route fails closed.

### 6.5 Step D - control-plane and rollback local tests

Verify local failure behavior:

- missing account identity fails closed;
- missing egress bucket fails closed;
- disabled `billing_cch_mode` fails closed;
- sign allowlist missing fails closed;
- signer/verifier failure fails closed;
- post-sign mutation detector failure fails closed;
- proxy/egress bucket failure fails closed;
- CC Gateway control-plane error is not treated as account ban/death;
- Sub2API does not fail over to another account;
- Sub2API does not direct-fallback to native upstream;
- rollback sets `billing_cch_mode=disabled` or pauses the account/route;
- rollback does not auto-switch to strip-controlled.

### 6.6 Step E - log redaction verification

Scan Sub2API, CC Gateway, local capture, and safe deliverable outputs for:

- raw token;
- raw Authorization;
- email address;
- account UUID;
- raw body;
- raw prompt;
- raw CCH-bearing body;
- non-placeholder raw CCH value;
- proxy URL credentials;
- OAuth refresh token;
- setup token.

Any hit blocks deployment and must be corrected before real-canary approval is requested.

---

## 7. Single real account pre-check

Before connecting the one real Claude account to this server, confirm:

- using a non-primary account is preferable; if only one account exists, the canary must be extremely small;
- no multi-user round-robin test is allowed;
- no concurrent traffic is allowed;
- no high-frequency test is allowed;
- no complex prompt is allowed;
- no long prompt is allowed;
- no tool-heavy/file-heavy request is allowed;
- only native `/v1/messages` sign-primary is allowed;
- count_tokens remains blocked/deferred;
- event_logging remains suppress/block and no upstream forward;
- OpenAI-compatible Anthropic routes remain excluded;
- Antigravity remains excluded;
- server fixed egress IP is known and stable;
- account identity record is configured in CC Gateway;
- account egress bucket maps to the fixed server egress;
- Sub2API account policy version matches CC Gateway policy version;
- sign-primary allowlist contains only this account/route;
- strip-controlled is disabled unless separately approved;
- rollback to `disabled` or pause is ready before the first request.

If any item is uncertain, do not connect the account to real upstream canary.

---

## 8. Real canary human confirmation gate

A real canary requires a new explicit user confirmation after local integration evidence and this checklist are reviewed.

The first real canary may include only:

- one account;
- one fixed egress IP;
- one native `POST /v1/messages` request;
- `billing_cch_mode=sign`;
- one simple prompt/request shape;
- no concurrency;
- no high frequency;
- no `count_tokens`;
- no event logging upstream;
- no OpenAI-compatible Anthropic route;
- no Antigravity;
- no automatic sign -> strip fallback;
- no native direct fallback.

If the first request succeeds and all success criteria are met, the operator may ask the user for another approval for at most 1-2 additional simple `/v1/messages` requests. Success does not authorize user/route/account expansion.

This document does not grant canary approval. It only defines what must be true before asking for approval.

---

## 9. Success criteria

A single-account sign-primary canary is considered successful only if all of the following are true:

- normal response from the single `/v1/messages` request;
- no 401;
- no 403;
- no KYC warning;
- no unusual-activity warning;
- no third-party/shared/abuse/security warning;
- CC Gateway sign verifier passes;
- CC Gateway post-sign mutation detector passes;
- final-output route is `/v1/messages` only;
- no native fallback occurs;
- no account failover occurs;
- no raw secret appears in logs;
- no raw body or raw prompt appears in safe deliverables;
- no raw account id/email/UUID appears in safe deliverables;
- `count_tokens` remains blocked/deferred;
- event logging remains suppress/block and is not forwarded upstream;
- OpenAI-compatible Anthropic routes remain excluded;
- Antigravity remains excluded;
- rollback remains immediately available.

A successful single request does not prove multi-account safety, multi-user round-robin safety, count_tokens readiness, event_logging readiness, or endpoint-complete signing-mode readiness.

---

## 10. Stop conditions

Stop immediately and set the account/route to `disabled` or paused if any of the following happen:

- any 400/401/403 that could indicate policy, auth, or safety risk;
- KYC warning;
- unusual-activity warning;
- third-party/shared/abuse/security warning;
- sign verifier failure;
- `cc_version` verifier failure;
- post-sign mutation detected;
- missing identity;
- missing or unexpected egress bucket;
- route spillover to `count_tokens`;
- event logging route spillover;
- OpenAI-compatible Anthropic route spillover;
- Antigravity route spillover;
- raw token, Authorization, email, UUID, raw CCH, raw body, or prompt appears in logs or safe deliverable;
- proxy or fixed-egress mismatch;
- CC Gateway control-plane error is interpreted as account ban/death;
- Sub2API attempts failover or native direct fallback;
- rollback path cannot be identified or executed.

Do not diagnose by sending more real upstream requests. Diagnose only with local capture or static evidence until the user approves another real canary.

---

## 11. Rollback steps

Rollback target is fail-closed, not strip fallback.

Required rollback sequence:

1. set selected account/route `billing_cch_mode=disabled`;
2. remove the account from sign-primary allowlist;
3. pause the selected account/route in Sub2API;
4. disable account `cc_gateway_enabled` if route-level disable is not sufficient;
5. disable CC Gateway sign-primary flag or activate sign-primary kill switch;
6. keep strip-controlled disabled unless the user separately approves a controlled diagnostic;
7. verify Sub2API does not direct-fallback to native upstream;
8. verify Sub2API does not fail over to another account;
9. preserve redacted evidence: policy version, lane, account hash, egress bucket id, route, verifier status, stop reason, rollback time;
10. run sensitive scan before moving any evidence into safe deliverable.

Forbidden rollback behaviors:

- automatic sign -> strip fallback;
- automatic direct native upstream fallback;
- automatic account failover;
- marking the account banned/dead because of a CC Gateway control-plane error;
- sending additional real canary requests while rollback is incomplete.

---

## 12. Artifacts and redaction rules

### 12.1 Safe deliverable paths

Use date-specific directories, for example:

```text
docs/anti-ban/captures/real-baseline/<DATE>-single-server-local-integration/safe-deliverable/
docs/anti-ban/captures/real-baseline/<DATE>-single-account-sign-primary-canary-prep/safe-deliverable/
```

Do not hard-code a past date in future evidence directories. Use the actual execution date.

### 12.2 Safe deliverable content

Allowed safe content:

- route;
- lane;
- policy version;
- selected account id hash;
- egress bucket id;
- server OS summary;
- fixed egress IP hash or redacted summary;
- request count;
- retry count;
- header key order and value summary;
- body key summary;
- body hash;
- metadata/session field names and hashes;
- billing block presence boolean;
- CCH presence boolean;
- `cc_version` verifier boolean;
- CCH verifier boolean;
- post-sign verifier boolean;
- rollback result;
- redaction scan result.

Forbidden safe content:

- raw token;
- raw Authorization;
- email address;
- account UUID;
- raw body;
- raw prompt;
- raw CCH value;
- raw CCH-bearing body;
- setup token;
- OAuth refresh token;
- proxy credential;
- full request/response body.

Raw material, if temporarily needed for local-only debugging, must stay in a clearly marked raw directory, must not be committed, and must not be copied into docs/evidence.

### 12.3 Sensitive scan

Before any artifact is proposed for commit, scan for at least:

- Anthropic token patterns;
- Authorization header values;
- bearer tokens;
- emails;
- UUIDs;
- non-placeholder `cch=` values;
- raw prompt/body phrases;
- proxy URLs with credentials;
- OAuth/setup-token strings.

Any hit stops the commit/evidence flow until corrected.

---

## 13. Future expansion boundary

Future scale-up is out of scope for this document.

Boundary rules:

- a second server is a second complete system, not a split deployment:
  - server 1: Sub2API + CC Gateway + account pool A + egress bucket A;
  - server 2: Sub2API + CC Gateway + account pool B + egress bucket B;
- a second account must first go through its own single-account sign-primary canary;
- multi-account scheduling waits until multiple single-account sign-primary canaries are stable;
- multi-user round-robin waits until single-account and multi-account sign-primary evidence is stable;
- high frequency waits until low-frequency evidence is stable;
- concurrency waits until non-concurrent evidence is stable;
- complex prompts wait until simple messages-only evidence is stable;
- `count_tokens` requires a separate future evidence and rollout plan;
- event logging upstream forwarding requires a separate future evidence and rollout plan if ever allowed;
- OpenAI-compatible Anthropic routes require a separate future evidence and rollout plan;
- Antigravity requires a separate future evidence and rollout plan;
- full endpoint-complete signing-mode still requires a separate future design.

Until those future gates exist, the only prepared real canary shape is one server, one account, one fixed egress, native `/v1/messages`, sign-primary, extremely low volume, with immediate rollback.

---

## 14. Server-local integration result note

Server-local integration was executed on the Ubuntu host using the single-server topology and localhost capture only.

Result summary:

- CC Gateway build/test passed on the server.
- Sub2API joint local capture test passed on the server.
- `sign-primary` `/v1/messages` passed with CC Gateway final-output ownership.
- `strip-controlled` `/v1/messages` passed.
- `count_tokens` remained blocked/deferred.
- `event_logging` remained suppress/block only.
- `billing_cch_mode=disabled` rollback failed closed without native fallback.
- No real upstream, no MITM, no login, and no raw secrets in safe deliverables.

### 14.1 2026-05-22 server readiness check

Read-only environment probe on `107.215.140.93`:

- `hostname`: `xeelee`
- `hostname -I`: `107.215.140.93 172.17.0.1 172.18.0.1`
- `curl -s ifconfig.me`: `107.215.140.93`
- `curl -s ipinfo.io/ip`: `107.215.140.93`
- `uname -a`: Ubuntu 24.04.4 LTS x86_64 kernel `6.8.0-101-generic`
- `node -v`: `v22.22.2`
- `npm -v`: `10.9.7`

Outcome:

- login IP and observed egress IP matched at `107.215.140.93`;
- Node/npm remained on the expected Node 22 line after upgrade;
- this check did not contact `api.anthropic.com`, `platform.claude.com`, or any real Claude account.

See:

- `docs/anti-ban/captures/real-baseline/2026-05-21-single-server-local-integration-results.md`
- `docs/anti-ban/captures/real-baseline/2026-05-21-single-server-local-integration/safe-deliverable/`
