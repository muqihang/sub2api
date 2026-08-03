# 77 - Claude Code 2.1.197 Server Deployed Mock Smoke Execution Checklist

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` or `superpowers:subagent-driven-development` to execute this checklist checkpoint-by-checkpoint. This plan is for server-side paired build plus deployed mock smoke only. It is not production deployment approval and not live canary approval.

**Goal:** Build and run a paired Sub2API + CC Gateway + Go/uTLS sidecar deployed mock smoke on server `66.163.122.103`, proving the Plan76 `2.1.197` canonical promotion path in a server environment with local mock upstream only.

**Architecture:** Treat the server as fresh for Plan77. Existing old Sub2API/CC Gateway server installs may be removed only after CP0 records a safe inventory and verifies they are not bound to forbidden ports; unknown files, scratch/evidence, and unrelated services remain out of scope. Build the exact Plan76 Sub2API and CC Gateway commits, run independent loopback-only canary processes on dedicated ports, route logical `api.anthropic.com` through the local Go/uTLS sidecar dial override into a local collector, execute smoke cases, stop only Plan77-owned processes, and write safe evidence only.

**Tech Stack:** Sub2API Go service, CC Gateway TypeScript/Node service, CC Gateway Go/uTLS egress sidecar, local loopback mock upstream collector, shell preflight/build scripts, safe JSON/Markdown evidence.

## Global Constraints

- Server: `66.163.122.103`, accessed as root by the executor using out-of-band credentials. Do not write credentials into repo, docs, logs, evidence, or scripts.
- Required Sub2API report commit: `ffcbecaa9 docs: add plan76 control-plane closure evidence report`.
- Required Sub2API implementation commit: `13f3fa08cfa58742b80c20d4534b49f82b621599`.
- Required CC Gateway commit: `fdf29bd062b378234ce3a4b9b17c9c8e6b2016d7`.
- Do not touch, stop, restart, reconfigure, delete, or bind over ports `3012`, `3017`, `18080`, or `18081`.
- Do not deploy production, switch production traffic, or run live canary.
- Do not access real Anthropic, AWS, Vertex, Bedrock, OpenAI, DeepSeek, paid, or credentialed upstreams.
- Do not use real OAuth/API keys/session cookies/account credentials/billing credentials/proxy credentials.
- Deletion authorization is limited to old server Sub2API/CC Gateway install files/directories that CP0 has positively identified as old, non-Plan77, non-evidence, unrelated to forbidden ports, and safe to remove for treating the server as fresh. Do not delete unknown files/directories, scratch evidence, unrelated services, or anything bound to `3012`, `3017`, `18080`, or `18081`.
- Do not run `git reset`, `git clean`, `git checkout --`, `git restore`, rebase, force push, `sudo`, `chmod -R`, or `chown -R`.
- Do not write raw prompt/body/response, secrets, cookies, account IDs, raw TLS/ClientHello, pcap, HAR, raw decoded domains, native dumps, cert/key material, or proxy credentials to repo/docs/evidence/logs.
- Evidence may contain only safe summaries: commit hashes, tags, PIDs for Plan77-owned processes, port buckets, route buckets, header key-set buckets, body schema hash/bucket, booleans, counts, stable error codes, safe TLS summary bucket, and redacted command statuses.
- If target server worktree is dirty or commit ancestry is unclear, stop and report instead of overwriting.
- If loopback-only egress guard cannot be proven, stop with `BLOCKED_LOCAL_ONLY_EGRESS_GUARD`.
- If any real upstream/non-loopback request succeeds, stop immediately and report blocker.
- Smoke completion must stop only Plan77-started processes and must not enter live canary.

---

## Fixed canary/mock port allocation

Do not use any of `3012`, `3017`, `18080`, or `18081`.

| Component | Port | Bind address | Notes |
|---|---:|---|---|
| Sub2API canary | `19082` | `127.0.0.1` | Sub2API mock-smoke instance only. |
| CC Gateway canary | `19083` | `127.0.0.1` | CC Gateway mock-smoke instance only. |
| Go/uTLS sidecar canary | `19084` | `127.0.0.1` | Real sidecar binary, local-only. |
| mock upstream collector | `19085` | `127.0.0.1` | Safe summary collector, no raw persistence. |
| guard probes | ephemeral | loopback only | No fixed port. |

If any chosen port is occupied during preflight, stop and report. Do not auto-select a replacement without an explicit plan update.

## Evidence locations

Server scratch root, created per run and not deleted without approval:

```text
/tmp/plan77-server-mock-smoke-<UTC_TIMESTAMP>/
```

Server safe evidence subdir:

```text
/tmp/plan77-server-mock-smoke-<UTC_TIMESTAMP>/safe/
```

Local final report path:

```text
/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/docs/anti-ban/77-claude-code-2197-server-deployed-mock-smoke-evidence-report.md
```

## CP0 - Preflight and rollback baseline

**Goal:** Prove the server state is understood before building or starting any canary process.

- [ ] Open an SSH session to `66.163.122.103` without writing credentials to any repo, script, log, or evidence file.
- [ ] Set a scratch root variable like:

```bash
export PLAN77_ROOT="/tmp/plan77-server-mock-smoke-$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$PLAN77_ROOT/safe"
```

- [ ] Record safe host facts to `$PLAN77_ROOT/safe/cp0-host.json`: UTC time, hostname, kernel, OS release bucket.
- [ ] Locate current Sub2API and CC Gateway repositories/install directories if present. Treat old installs as non-authoritative and eligible for removal only after the next inventory checks pass.
- [ ] Record current git branch/HEAD/dirty status for any discovered Sub2API and CC Gateway worktree to `$PLAN77_ROOT/safe/cp0-git-state.json`.
- [ ] Record old-install inventory to `$PLAN77_ROOT/safe/cp0-old-install-inventory.json`: path bucket, git HEAD if present, listener/port association, process association, and removal eligibility boolean. Do not record secrets or raw config.
- [ ] If an old install is positively identified as Sub2API/CC Gateway, not the target Plan77 source, not evidence/scratch, not bound to `3012`, `3017`, `18080`, or `18081`, and not required for rollback baseline, remove it or move it out of active paths according to the safest available server convention. Record only safe status labels in `$PLAN77_ROOT/safe/cp0-old-install-removal.json`.
- [ ] If any old install cannot be positively classified, leave it untouched and route Plan77 through independent scratch/build/run paths.
- [ ] Verify target ancestry:
  - Sub2API contains `ffcbecaa9` and `13f3fa08cfa58742b80c20d4534b49f82b621599`, or a clearly recorded descendant that contains both.
  - CC Gateway contains `fdf29bd062b378234ce3a4b9b17c9c8e6b2016d7`, or a clearly recorded descendant.
- [ ] If the target worktree is dirty, stop and report. Do not overwrite, reset, clean, restore, or checkout away changes.
- [ ] Record current listener snapshot to `$PLAN77_ROOT/safe/cp0-listeners-before.txt` using safe command output from `ss -ltnp` and `ss -lunp`.
- [ ] Record forbidden port baseline for `3012`, `3017`, `18080`, `18081` to `$PLAN77_ROOT/safe/cp0-forbidden-ports-before.json`.
- [ ] Record disk summary to `$PLAN77_ROOT/safe/cp0-disk.txt` with `df -h`.
- [ ] Record Docker/build cache summary if Docker exists to `$PLAN77_ROOT/safe/cp0-build-cache.txt`; otherwise record `docker_unavailable` and native toolchain versions.
- [ ] Record rollback production baseline to `$PLAN77_ROOT/safe/cp0-rollback-baseline.json`: existing listener states, relevant process buckets, image/tag buckets, and service status buckets only.
- [ ] Check ports `19082`, `19083`, `19084`, `19085` are free. If any is occupied, stop and report.

**Stop conditions:** dirty target worktree, missing commit ancestry, unclear server state after old-install inventory, attempted removal target cannot be positively classified, occupied canary port, or inability to record forbidden-port baseline.

## CP1 - Build paired artifacts

**Goal:** Build Sub2API, CC Gateway, and Go/uTLS sidecar from the required Plan76 commits or descendants without modifying production services.

- [ ] Create `$PLAN77_ROOT/build/` and `$PLAN77_ROOT/run/` directories.
- [ ] Record exact source paths and commits to `$PLAN77_ROOT/safe/cp1-source-lock.json`.
- [ ] Build Sub2API canary artifact or image from the verified Sub2API worktree.
  - Suggested tag/artifact bucket: `sub2api:plan77-ffcbecaa9-13f3fa08` or `sub2api-plan77-13f3fa08`.
  - Record command, exit code, artifact path/tag, and digest/hash bucket to `$PLAN77_ROOT/safe/cp1-sub2api-build.json`.
- [ ] Build CC Gateway canary artifact or image from the verified CC Gateway worktree.
  - Suggested tag/artifact bucket: `cc-gateway:plan77-fdf29bd` or `cc-gateway-plan77-fdf29bd`.
  - Record command, exit code, artifact path/tag, and digest/hash bucket to `$PLAN77_ROOT/safe/cp1-cc-gateway-build.json`.
- [ ] Build Go/uTLS sidecar from CC Gateway `sidecar/egress-tls-sidecar`.
  - Suggested tag/artifact bucket: `egress-tls-sidecar:plan77-fdf29bd` or `egress-tls-sidecar-plan77-fdf29bd`.
  - Record command, exit code, artifact path/tag, and digest/hash bucket to `$PLAN77_ROOT/safe/cp1-sidecar-build.json`.
- [ ] Do not install artifacts into production paths. Use `$PLAN77_ROOT/build/` or isolated image tags only.

**Stop conditions:** build failure, missing toolchain that cannot be safely installed without destructive operations, or build command attempts production deploy/restart.

## CP2 - Egress guard and mock upstream collector

**Goal:** Establish local-only mock upstream and prove the smoke environment cannot reach real upstreams.

- [ ] Start mock upstream collector on `127.0.0.1:19085` as a Plan77-owned process.
- [ ] Collector must persist only safe summaries:
  - request count;
  - method/route bucket;
  - header key-set bucket;
  - body structural schema hash/bucket;
  - model bucket;
  - CCH/billing/client-attribution booleans;
  - raw body persisted boolean, expected `false`.
- [ ] Write collector PID to `$PLAN77_ROOT/run/mock-collector.pid`.
- [ ] Establish egress guard around canary processes and smoke runner. Preferred implementation is a network namespace/firewall wrapper allowing loopback only. If unavailable, use app-level local-only config plus explicit guard probes and record the limitation.
- [ ] Clear inherited proxy/base URL/provider env variables for canary processes and smoke runner:
  - `HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY`, `NO_PROXY`, lowercase variants;
  - `ANTHROPIC_*`, `CLAUDE_*`, `AWS_*`, `GCP_*`, `GOOGLE_*`, `AZURE_*`, `OPENAI_*`, `DEEPSEEK_*`, except synthetic local variables explicitly needed by the smoke harness.
- [ ] Run guard probes and write `$PLAN77_ROOT/safe/cp2-egress-guard.json`:
  - DNS blocked;
  - IPv4 non-loopback blocked;
  - IPv6 non-loopback blocked;
  - UDP blocked;
  - inherited proxy env blocked;
  - provider direct TCP blocked;
  - real upstream request count `0`;
  - non-loopback attempt count `0`.

**Stop conditions:** guard cannot be proven, external network succeeds, collector would persist raw payloads, or real provider endpoint can be reached.

## CP3 - Start sidecar, CC Gateway, and Sub2API canary processes

**Goal:** Start only Plan77-owned loopback processes using the paired build artifacts.

- [ ] Start Go/uTLS sidecar on `127.0.0.1:19084`.
  - Logical host: `api.anthropic.com`.
  - Dial override: `api.anthropic.com:443 -> 127.0.0.1:19085`.
  - Allowed route: `/v1/messages`.
  - Allowed profile ref: Plan75/76 `2.1.197` TLS profile safe ref.
  - Expected summary bucket: Plan75/76 `2.1.197` TLS safe summary bucket.
  - Write PID to `$PLAN77_ROOT/run/sidecar.pid`.
- [ ] Start CC Gateway canary on `127.0.0.1:19083`.
  - Upstream path must use sidecar endpoint `127.0.0.1:19084` only.
  - Node direct HTTPS fallback must be disabled or fail-closed.
  - Formal-pool canonical tuple policy must be server-selected.
  - Write PID to `$PLAN77_ROOT/run/cc-gateway.pid`.
- [ ] Start Sub2API canary on `127.0.0.1:19082`.
  - CC Gateway endpoint must be `127.0.0.1:19083`.
  - Configure server-selected canonical candidates for `2.1.197`, fallback `2.1.185`, and rollback `2.1.179`.
  - Write PID to `$PLAN77_ROOT/run/sub2api.pid`.
- [ ] Record process summaries to `$PLAN77_ROOT/safe/cp3-processes.json`: process alive booleans, PID buckets, bind addresses, and ports.
- [ ] Re-check forbidden ports and write `$PLAN77_ROOT/safe/cp3-forbidden-ports-during.json`; compare to CP0 baseline.

**Stop conditions:** any canary binds non-loopback, any canary attempts forbidden port, any production process is stopped/restarted, or forbidden-port baseline changes unexpectedly.

## CP4 - Health/readiness smoke

**Goal:** Verify all canary components are reachable on loopback only.

- [ ] Call Sub2API health/readiness on `127.0.0.1:19082`; record status bucket.
- [ ] Call CC Gateway health/readiness on `127.0.0.1:19083`; record status bucket.
- [ ] Call sidecar health/readiness on `127.0.0.1:19084`; record status bucket.
- [ ] Call collector health/readiness on `127.0.0.1:19085`; record status bucket.
- [ ] Write `$PLAN77_ROOT/safe/cp4-health.json`.

**Stop conditions:** any health check fails or response contains raw sensitive material.

## CP5 - Canonical 2.1.197 primary smoke

**Goal:** Prove observed inbound versions do not select upstream identity; server tuple selects `2.1.197`.

- [ ] Send mock formal-pool request with observed inbound `2.1.179`; expect server-selected canonical `2.1.197` safe summary.
- [ ] Send mock formal-pool request with observed inbound `2.1.185`; expect same canonical `2.1.197` safe summary.
- [ ] Send mock formal-pool request with observed inbound `2.1.197`; expect same canonical `2.1.197` safe summary.
- [ ] Attempt user version/family/platform/profile spoof; expect no upstream authority change or fail-closed.
- [ ] Validate collector safe summaries:
  - user-agent bucket canonical `2.1.197`;
  - beta/model policy bucket canonical `2.1.197`;
  - observed client version not authoritative;
  - raw body persisted `false`.
- [ ] Write `$PLAN77_ROOT/safe/cp5-canonical-primary.json`.

**Stop conditions:** upstream summary follows user observed version, mixed tuple sent, raw body persisted, or non-loopback attempt occurs.

## CP6 - Sonnet 5, fallback, rollback, and tuple switching smoke

**Goal:** Prove `2.1.197` Sonnet 5 mock support and fail-closed fallback/rollback behavior.

- [ ] Under server-selected `2.1.197`, send Sonnet 5 mock path; expect PASS to local collector through sidecar.
- [ ] Under server-selected `2.1.185`, send Sonnet 5 mock path; expect fail-closed before upstream/sidecar with stable Plan76 error.
- [ ] Under server-selected `2.1.179`, send Sonnet 5 mock path; expect fail-closed before upstream/sidecar with stable Plan76 error.
- [ ] Under `2.1.185` fallback tuple, send non-Sonnet mock request; expect fallback tuple mock PASS.
- [ ] Under `2.1.179` rollback tuple, send non-Sonnet mock request; expect rollback tuple mock PASS.
- [ ] Tuple switching new session: `2.1.197 -> 2.1.185 -> 2.1.179`; expect new sessions can use each new tuple.
- [ ] Tuple switching old session drift: reuse old session with changed tuple; expect fail-closed.
- [ ] Write `$PLAN77_ROOT/safe/cp6-sonnet-fallback-rollback.json`.

**Stop conditions:** Sonnet 5 is allowed under `2.1.185`/`2.1.179`, old session drift is accepted, or local collector receives a request for a fail-closed case.

## CP7 - Plan76 control-plane fail-closed smoke

**Goal:** Prove unobserved high-risk shapes fail closed before upstream/sidecar.

- [ ] Send `count_tokens` request; expect `formal_pool_count_tokens_profile_unapproved` or equivalent stable Plan76 code, upstream count unchanged.
- [ ] Send MCP configured authority/body marker; expect `formal_pool_mcp_shape_unapproved` or equivalent stable Plan76 code, upstream count unchanged.
- [ ] Send non-streaming messages request; expect `formal_pool_non_streaming_profile_unapproved` or equivalent stable Plan76 code, upstream count unchanged.
- [ ] Send unapproved model/control-plane path; expect `formal_pool_control_plane_unapproved` or equivalent stable Plan76 code, upstream count unchanged.
- [ ] Write `$PLAN77_ROOT/safe/cp7-control-plane-fail-closed.json`.

**Stop conditions:** any fail-closed case reaches sidecar/collector, error code unstable/missing, or fallback to Node direct HTTPS occurs.

## CP8 - Plan72 env residue smoke

**Goal:** Prove environment residue cannot influence canonical upstream authority.

- [ ] Send request with Asia/Shanghai marker bucket; expect canonicalized or fail-closed, no upstream authority influence.
- [ ] Send request with Asia/Urumqi marker bucket; expect canonicalized or fail-closed, no upstream authority influence.
- [ ] Send request with non-official `ANTHROPIC_BASE_URL` residue bucket; expect stripped/fail-closed, no upstream authority influence.
- [ ] Send request with synthetic domain / AI keyword residue bucket; expect no upstream authority influence.
- [ ] Validate system marker final state is canonical or absent; no unsafe injection.
- [ ] Write `$PLAN77_ROOT/safe/cp8-env-residue.json`.

**Stop conditions:** residue changes sidecar logical host, upstream authority, proxy, tuple identity, or raw residue material is persisted.

## CP9 - TLS sidecar and CCH/billing/attribution smoke

**Goal:** Prove the real Go/uTLS sidecar is used and attribution remains stripped.

- [ ] Verify CC Gateway sends to Go/uTLS sidecar on `127.0.0.1:19084` and not directly to HTTPS/provider.
- [ ] Verify sidecar forwards only to local collector `127.0.0.1:19085` through the dial override.
- [ ] Verify `2.1.197` TLS profile is selected only by server-selected canonical tuple.
- [ ] Verify collector TLS safe summary bucket matches Plan75/76 `2.1.197` oracle bucket.
- [ ] Verify no `x-anthropic-billing-*` header bucket is present.
- [ ] Verify no raw/native CCH bucket is present.
- [ ] Verify no signed/no-CCH mode bucket is present.
- [ ] Verify no client attribution bucket is present in headers/body/metadata safe summary.
- [ ] Write `$PLAN77_ROOT/safe/cp9-tls-cch-billing.json`.

**Stop conditions:** Node direct HTTPS fallback count > 0, sidecar not used, profile follows client input, CCH/billing/client attribution appears, or real upstream count > 0.

## CP10 - Final safety counters, leak scan, shutdown, and report

**Goal:** Stop Plan77 processes, prove production untouched, and generate the local evidence report.

- [ ] Query collector/sidecar/gateway safe counters and write `$PLAN77_ROOT/safe/cp10-final-counters.json`:
  - real upstream request count `0`;
  - non-loopback attempt count `0`;
  - Node direct HTTPS fallback count `0`;
  - fail-closed upstream send count `0` for blocked cases.
- [ ] Re-check forbidden ports and write `$PLAN77_ROOT/safe/cp10-forbidden-ports-after.json`; compare with CP0 baseline.
- [ ] Stop only Plan77-owned PIDs from `$PLAN77_ROOT/run/*.pid`.
- [ ] Do not stop, restart, or reload any pre-existing service.
- [ ] Record post-shutdown process/port snapshot to `$PLAN77_ROOT/safe/cp10-post-shutdown.json`.
- [ ] Run leak scan over local report draft and safe evidence summaries. Record `$PLAN77_ROOT/safe/cp10-leak-scan.json` with blocking findings count.
- [ ] Write final local report:

```text
/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/docs/anti-ban/77-claude-code-2197-server-deployed-mock-smoke-evidence-report.md
```

- [ ] Report must include:
  - Sub2API / CC Gateway commits used;
  - build commands and artifacts/tags;
  - canary ports;
  - forbidden ports untouched proof;
  - egress guard proof;
  - smoke case status table;
  - TLS safe summary;
  - env residue validation;
  - control-plane fail-closed validation;
  - CCH/billing/attribution strip validation;
  - real upstream count `0`;
  - Node direct fallback `0`;
  - leak scan;
  - rollback baseline and after-state;
  - explicit no live canary, no production traffic switch, no real upstream.
- [ ] If report is accepted, commit only the report in Sub2API as a separate docs commit. If CC Gateway smoke scripts/tests were added, commit them separately in CC Gateway.

**Stop conditions:** leak scan blocking finding, forbidden-port after-state mismatch, unable to stop Plan77-owned processes, or final counters not zero.

## Final output to user

Return only a concise result summary after execution:

- Final smoke decision: PASS or exact blocker.
- Sub2API commit and report commit.
- CC Gateway commit and optional script commit.
- Canary ports used.
- Real upstream count.
- Node direct HTTPS fallback count.
- Non-loopback attempt count.
- Forbidden ports untouched status.
- Whether production/live canary remained untouched.
- Scratch cleanup status.

## Notes on old server installs

The server may contain old Sub2API and CC Gateway installs. This checklist treats the server as fresh for Plan77. The user has authorized removal of old Sub2API/CC Gateway server versions for this Plan77 run, but only after CP0 positively identifies them as old Sub2API/CC Gateway artifacts and verifies they are not bound to forbidden ports, not evidence/scratch, and not unrelated services. Unknown or ambiguous files/directories must remain untouched. Production/live canary traffic still must not be switched.
