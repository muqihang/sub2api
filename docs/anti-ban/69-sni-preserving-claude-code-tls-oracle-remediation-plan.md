# 69 - SNI-preserving Claude Code TLS Oracle Remediation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` or `superpowers:executing-plans` to execute this plan checkpoint-by-checkpoint. This document is a plan only. Do not execute the dynamic oracle capture until CP0/CP1 have been implemented, CP2 hard egress guard passes in the same process scope, and a checkpoint reviewer approves proceeding.

**Goal:** Re-root the Claude Code TLS oracle under a logical `api.anthropic.com` SNI/Host scenario, then compare the real Go/uTLS sidecar under identical conditions so Plan 65 CP2 can be retried safely.

**Architecture:** Use Sub2API's existing safe oracle tooling as the evidence writer, add a local-only SNI-preserving capture harness, and keep CC Gateway's real sidecar as the comparison target. The preferred capture topology is Claude Code CLI -> loopback CONNECT proxy/collector -> in-memory ClientHello parser, with optional local TLS termination only for a safe Host-header bucket. The collector must never connect to real Anthropic and must never persist raw ClientHello, raw HTTP body, raw response, keys, certificates, or secrets.

**Tech Stack:** Sub2API Python oracle tools/tests, existing loopback-only egress guard helpers, real Claude Code CLI versions installed in temporary HOME/XDG/npm/cache sandboxes, CC Gateway Go/uTLS sidecar, local TCP/CONNECT collector, safe JSON evidence, markdown reports.

## Global Constraints

- Work only in Sub2API worktree: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool`.
- Work only in CC Gateway worktree: `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5`.
- Do not touch the Sub2API main worktree: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main`.
- Do not deploy, restart, stop, reconfigure, or bind over `3012`, `3017`, `18080`, or `18081`.
- Do not call real Anthropic, AWS, Vertex, Bedrock, OpenAI, DeepSeek, or other paid/credentialed upstreams.
- Do not modify production DB rows, account scheduling state, credentials, proxy config, or production runtime config.
- The canonical production app-layer profile remains Claude Code `2.1.179` with the existing strip-attribution posture. Captures of later versions are evidence only and cannot promote production behavior.
- Do not inherit real `ANTHROPIC_*`, AWS, Google, OpenAI, proxy, cookie, OAuth, refresh-token, or account environment variables. Dynamic CLI runs must use a strict env allowlist, temporary HOME, temporary XDG dirs, temporary Claude config/cache, and temporary npm cache.
- Use only dummy/local-only credentials. Do not print, log, or write real Authorization, x-api-key, cookie, workspace ID, account UUID/email, proxy credential, HMAC material, raw prompt, raw request body, raw response, raw telemetry, or raw CCH.
- The oracle target must be logical `api.anthropic.com`: TLS SNI must be `api.anthropic.com`, and the HTTP Host header must be `api.anthropic.com` or recorded as not safely observable. Do not substitute `tls.sub2api.org`, localhost, loopback IP, or no-SNI results for production SNI oracle evidence.
- Do not remove SNI to match doc-63. A no-SNI match is evidence about the old capture context, not production SNI equivalence.
- Do not write raw ClientHello, raw TLS records, pcap, raw cipher lists, raw extension lists, raw TLS templates, private keys, certificates, or mock CA material to docs, evidence, logs, fixtures, repo files, or formal scratch directories.
- If a local dummy certificate/key is required to observe the HTTP Host header, generate it only for the local collector process, keep it in memory when possible, otherwise place it in a `0700` private temporary directory outside the repo/evidence root, never log its path or content, delete it before checkpoint completion, and record only `mock_tls_trust_override=true/false` plus `dummy_cert_material_persisted=false` when true. If the runtime cannot satisfy this, mark `HOST_HEADER_BUCKET_NOT_OBSERVED` rather than weakening the secret/TLS-material policy.
- All dynamic captures using logical real provider hostnames must run under a same-scope deny-all-except-loopback egress guard. Required self-tests: loopback collector reachable, IPv4 external TCP blocked, IPv6 external TCP blocked, DNS/UDP external blocked, external proxy env rejected, real provider host direct connect blocked, and real provider host through non-loopback proxy blocked. If any self-test cannot be proven, mark `BLOCKED_DYNAMIC_EGRESS_GUARD` and do not run Claude Code or sidecar dynamic capture.
- Evidence may contain only safe summaries: JA3 hash, JA4 summary, TLS version list, ALPN list, cipher count, extension count, GREASE presence, `sni_present` boolean, `sni_host_bucket`, `host_header_bucket`, safe profile refs, safe bucket labels, timestamps, guard status, and commit/config identifiers.
- If root privileges, global hosts-file edits, `pfctl`, system DNS changes, global certificate installation, destructive git operations, or broad permission changes are required, stop and ask the user first.

---

## Background and Problem Statement

Plan 65 CP2 ended as `BLOCKED_TLS_ENGINE_MISMATCH`. The real Go/uTLS sidecar, when preserving Plan 65's logical provider SNI/Host boundary, produced this safe summary:

```text
sidecar with SNI: JA3 dc782a9d905fdcee1223a3d4e8108bc6, extension_count 13
```

The doc-63 Claude Code oracle used as the comparison baseline was:

```text
doc-63 oracle: JA3 e97f5146a7009cc2918b50e903b6ff8d, extension_count 12
```

The likely mismatch driver is the SNI extension: the sidecar's logical `api.anthropic.com` run includes SNI, while the old doc-63 oracle may have been captured against loopback/no-SNI, collector-host, or localhost/IP conditions. Therefore the old oracle must not be treated as a production SNI baseline until its context is proven. This plan re-captures the oracle under the production-relevant logical-host condition without making a real upstream connection.

## Recommended Capture Design

### Preferred topology: local CONNECT proxy / collector

```text
Claude Code CLI with dummy credentials
  -> HTTPS_PROXY=http://127.0.0.1:<collector_port>
  -> CONNECT api.anthropic.com:443
  -> collector returns 200 Connection Established
  -> Claude Code sends TLS ClientHello with SNI api.anthropic.com
  -> collector parses ClientHello in memory and writes safe summary only
  -> optional local TLS terminator reads only HTTP headers to bucket Host
```

Why this is preferred:

- It preserves the logical URL, SNI, and Host semantics of `https://api.anthropic.com/...`.
- It does not require global DNS, hosts-file, PF, or certificate changes.
- The collector can fail closed before any real upstream connection.
- The hard egress guard can prove that even a proxy bypass cannot reach external networks.

### Alternative designs and risk rating

| Design | Use | Risk / requirement |
| --- | --- | --- |
| Loopback CONNECT proxy | Preferred | Must prove Claude Code uses it and cannot bypass to external egress. |
| Sidecar-local dial override | Good for sidecar comparison, not enough for CLI unless CLI supports it | Must not be request-controlled or production-enabled. |
| Per-process DNS override | Only if available without global system mutation | Must keep SNI `api.anthropic.com` and prove no real provider connect. |
| hosts-file or PF redirect | Avoid by default | Requires root/global mutation; stop and ask user first. |
| `tls.sub2api.org`, localhost, loopback IP, no-SNI | Forbidden as production SNI oracle | May only explain old oracle context; cannot unlock production TLS equivalence. |

---

## Checkpoint 0 - Old doc-63 Oracle Context Review

**Goal:** Determine whether doc-63's old Claude Code oracle is valid for logical `api.anthropic.com` SNI/Host, or must be demoted to historical/non-production context.

**Files to read:**

- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/docs/anti-ban/65-formal-pool-tls-deployed-equivalence-and-real-sidecar-plan.md`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/docs/anti-ban/66-formal-pool-tls-deployed-equivalence-evidence-report.md`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/docs/anti-ban/63-claude-code-real-oracle-and-egress-tls-evidence-report.md`
- Plan 65 CP2 safe evidence file referenced by doc 66, if present, especially `$PLAN_65_EVIDENCE_ROOT/safe/cp2-sidecar-tls-summary.json`.
- Safe Phase A evidence under the original Phase A evidence root if still available.

**Steps:**

- [ ] Locate Plan 65 CP2 evidence anchors from doc 65/doc 66 and extract only safe fields: `BLOCKED_TLS_ENGINE_MISMATCH`, sidecar logical-SNI condition, sidecar JA3, sidecar extension count, comparison baseline, difference fields, and Plan 65 CP2 blocked reason.
- [ ] Write those safe source anchors into `$EVIDENCE_ROOT/safe/cp0-plan65-cp2-anchor.json`; if the referenced Plan 65 safe evidence file is unavailable, record `plan65_cp2_safe_evidence_file_available=false` and rely only on doc 66 safe report fields.
- [ ] Locate doc-63 oracle evidence references and extract only safe fields: JA3, JA4, extension count, ALPN, TLS versions, collector mode, target host bucket, SNI presence, and capture topology.
- [ ] Classify the old oracle context as exactly one of:
  - `OLD_ORACLE_LOGICAL_HOST_SNI_PROVEN`
  - `OLD_ORACLE_WAS_NO_SNI_OR_COLLECTOR_HOST`
  - `OLD_ORACLE_CONTEXT_INSUFFICIENT`
- [ ] If SNI presence or logical host cannot be proven from safe evidence, set `OLD_ORACLE_CONTEXT_INSUFFICIENT`.
- [ ] Write safe evidence to `$EVIDENCE_ROOT/safe/cp0-old-oracle-context.json`.
- [ ] Update the future 70 report draft with the classification.

**Required result:** The old doc-63 oracle must not remain the production SNI baseline unless `OLD_ORACLE_LOGICAL_HOST_SNI_PROVEN` is proven. `OLD_ORACLE_CONTEXT_INSUFFICIENT` is enough reason to re-root the oracle.

**Review gate:** Reviewer verifies the old oracle classification is evidence-based and does not infer SNI from a missing field.

---

## Checkpoint 1 - SNI-preserving Capture Harness Design and Tests

**Goal:** Implement a local capture harness that lets Claude Code logically connect to `api.anthropic.com` while the actual TCP connection terminates at loopback.

**Likely files:**

- Modify: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/tools/claude_code_real_oracle_loopback.py`
- Modify: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/tools/claude_code_tls_oracle.py`
- Modify: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/tools/tests/test_claude_code_real_oracle_loopback.py`
- Modify: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/tools/tests/test_claude_code_tls_oracle.py`

**Required harness behavior:**

- The CLI request URL remains logically `https://api.anthropic.com/v1/messages` or the equivalent Claude Code upstream path.
- The collector listens only on `127.0.0.1` or `::1`.
- The only allowed dynamic proxy env points to the loopback collector. Non-loopback proxy env is rejected.
- CONNECT authority must be exactly `api.anthropic.com:443`; any other host, port, scheme, absolute URL, path traversal, or proxy credential is rejected.
- ClientHello parsing happens in memory; only safe summary fields are written.
- Optional Host-header observation may use a local TLS terminator with temporary dummy trust. It must record only:
  - `host_header_bucket="anthropic_api"` when Host is exactly `api.anthropic.com` or `api.anthropic.com:443`.
  - `host_header_bucket="not_observed_tls_only"` if the TLS handshake is intentionally not completed.
  - `host_header_bucket="unexpected"` if a different Host is observed, which fails the checkpoint.
- The harness must not persist raw HTTP headers. It may count whether a Host header matched the expected bucket, then discard the raw header bytes.

**Tests to write first:**

- [ ] CONNECT `api.anthropic.com:443` produces a safe collector event with `logical_target_host_bucket="anthropic_api"`.
- [ ] CONNECT to localhost, loopback IP, `tls.sub2api.org`, or any non-443 port is rejected.
- [ ] Non-loopback proxy variables are rejected by the wrapper before launching Claude Code, covering `HTTP_PROXY`, `HTTPS_PROXY`, `http_proxy`, `https_proxy`, `ALL_PROXY`, `all_proxy`, `NO_PROXY`, `no_proxy`, npm proxy variables, and any provider endpoint override variable such as `ANTHROPIC_BASE_URL` or equivalent.
- [ ] Unexpected trust injection variables are rejected before launch, including `NODE_EXTRA_CA_CERTS`, `SSL_CERT_FILE`, `SSL_CERT_DIR`, `CURL_CA_BUNDLE`, `REQUESTS_CA_BUNDLE`, and npm CA/proxy config variables.
- [ ] ClientHello parser emits `sni_present=true` and `sni_host_bucket="anthropic_api"` when a test client uses SNI `api.anthropic.com`.
- [ ] ClientHello parser emits `sni_present=false` for no-SNI test input and classifies it as non-production oracle context.
- [ ] Evidence writer rejects raw ClientHello, pcap markers, private key markers, certificate PEM, raw request body, Authorization, x-api-key, cookie, workspace ID, and proxy credentials.
- [ ] Host-header bucket code records only the bucket and never writes raw headers.

**Expected commands:**

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest tools.tests.test_claude_code_real_oracle_loopback tools.tests.test_claude_code_tls_oracle -v
```

**Review gate:** Reviewer verifies the design preserves SNI/Host authority and cannot silently substitute localhost/no-SNI evidence.

---

## Checkpoint 2 - Hard Loopback-only Egress Gate

**Goal:** Prove the exact dynamic capture process scope cannot reach external networks before running real Claude Code.

**Required self-tests in the same scope as the future CLI process:**

- [ ] Loopback collector reachable.
- [ ] IPv4 external TCP blocked.
- [ ] IPv6 external TCP blocked.
- [ ] DNS/UDP external blocked.
- [ ] Direct `api.anthropic.com:443` connection blocked after the same-scope deny-all guard is installed; this self-test must attempt only a bare TCP connect with a short timeout and must not send TLS ClientHello, HTTP bytes, SNI, credentials, or application data.
- [ ] If any direct provider TCP connect unexpectedly succeeds, immediately close the socket without writing bytes, mark `provider_tcp_connect_unexpected_success=true`, set the checkpoint to `BLOCKED_DYNAMIC_EGRESS_GUARD`, and stop all dynamic capture.
- [ ] Non-loopback proxy env rejected before launch, covering uppercase/lowercase `HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY`, and `NO_PROXY`.
- [ ] npm proxy variables, provider base-url override variables, and unexpected trust variables such as `NODE_EXTRA_CA_CERTS`, `SSL_CERT_FILE`, `SSL_CERT_DIR`, `CURL_CA_BUNDLE`, and `REQUESTS_CA_BUNDLE` are rejected unless they point to the explicitly created local test trust material for the current process scope.
- [ ] Proxy-env-only external path blocked if the loopback collector is absent.
- [ ] Real provider host through non-loopback proxy blocked or wrapper-rejected.

**Evidence:**

- Write `$EVIDENCE_ROOT/safe/cp2-egress-guard.json` with booleans only.
- Do not record raw DNS answers, IP addresses beyond loopback classification, proxy credentials, or packet captures.

**Failure behavior:**

- If any required self-test is not proven, set final state `BLOCKED_DYNAMIC_EGRESS_GUARD` for this plan.
- Do not run Claude Code or sidecar dynamic capture under logical `api.anthropic.com` after a guard failure.

**Review gate:** Reviewer verifies the guard is same-scope, not a manual proof, and not merely an environment-variable proxy check.

---

## Checkpoint 3 - Claude Code 2.1.179 SNI-preserving Oracle Capture

**Goal:** Capture the production canonical Claude Code `2.1.179` TLS oracle with SNI and Host logically bound to `api.anthropic.com`, without contacting Anthropic.

**Preconditions:**

- CP0 classification completed.
- CP1 harness tests pass.
- CP2 hard loopback-only egress guard passes.

**Runtime requirements:**

- Use Claude Code `2.1.179` only for the canonical oracle.
- Use temporary HOME, XDG, Claude config/cache, and npm cache.
- Use strict env allowlist and dummy/local-only credentials.
- Keep the logical provider URL as `https://api.anthropic.com`.
- Use loopback CONNECT proxy/collector or a reviewer-approved equivalent that preserves SNI `api.anthropic.com`.
- Do not install a global root CA.

**Safe summary fields:**

```json
{
  "schema": "sni_preserving_claude_code_tls_oracle.v1",
  "source": "claude_code_cli_sni_preserving",
  "version": "2.1.179",
  "logical_target_host_bucket": "anthropic_api",
  "sni_present": true,
  "sni_host_bucket": "anthropic_api",
  "host_header_bucket": "anthropic_api | not_observed_tls_only",
  "ja3_hash": "<hash only>",
  "ja4": "<safe summary only>",
  "alpn_protocols": ["..."],
  "tls_versions": ["..."],
  "cipher_count": 0,
  "extension_count": 0,
  "grease_present": false,
  "mock_dial_override": true,
  "mock_tls_trust_override": "true|false",
  "dummy_cert_material_persisted": false,
  "raw_clienthello_omitted_reason": "raw_clienthello_forbidden"
}
```

**Host/trust evidence rule:**

- If `host_header_bucket` is observed by completing local TLS with dummy test trust, the evidence must set `mock_tls_trust_override=true` and `dummy_cert_material_persisted=false`.
- If Host cannot be observed without unsafe trust or persisted certificate/key material, write `host_header_bucket="not_observed_tls_only"`, set `mock_tls_trust_override=false`, and keep the SNI oracle valid only for TLS ClientHello comparison.
- `host_header_bucket="anthropic_api"` is invalid unless either no trust override was needed or the safe trust fields above are present.

**Evidence output:**

- `$EVIDENCE_ROOT/safe/cp3-claude-code-2179-sni-oracle.json`
- `$EVIDENCE_ROOT/safe/cp3-claude-code-2179-capture-matrix.json`

**Required classification:**

- `SNI_ORACLE_CAPTURED` if SNI is present, SNI bucket is `anthropic_api`, safe summary is complete, and no leakage occurs.
- `BLOCKED_DYNAMIC_EGRESS_GUARD` if CP2 guard is not proven.
- `BLOCKED_SNI_ORACLE_CAPTURE` if Claude Code cannot be made to preserve SNI without real upstream or unsafe trust changes.
- `HOST_HEADER_BUCKET_NOT_OBSERVED` may be used only when TLS was intentionally not terminated; it does not invalidate the SNI oracle but must be explicit.

**Review gate:** Reviewer verifies the captured oracle is production-relevant for SNI and not a loopback/no-SNI substitute.

---

## Checkpoint 4 - Optional Later-version Captures for 67 Promotion/Risk Context

**Goal:** Optionally capture SNI-preserving safe summaries for later Claude Code versions as risk context only.

**Allowed versions:**

- `2.1.185` if available.
- `2.1.196` if available.
- If those exact versions are unavailable, record `VERSION_NOT_AVAILABLE` instead of substituting unapproved versions.

**Constraints:**

- These captures do not alter canonical production `2.1.179`.
- These captures do not promote request-shape, CCH, billing, TLS, cache, or app-layer policy.
- They may support future 67-series promotion/risk analysis only.

**Evidence output:**

- `$EVIDENCE_ROOT/safe/cp4-later-version-sni-captures.json`

**Review gate:** Reviewer verifies optional captures are clearly non-authoritative for production promotion.

---

## Checkpoint 5 - Sidecar Same-condition Comparison

**Goal:** Compare CC Gateway's real Go/uTLS sidecar to the new Claude Code `2.1.179` SNI-preserving oracle under the same logical target conditions.

**Files likely involved:**

- Modify if needed: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/tools/claude_code_tls_oracle.py`
- Modify if needed: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/tools/tests/test_claude_code_tls_oracle.py`
- Read/use: `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/sidecar/egress-tls-sidecar/`

**Comparison conditions:**

- Sidecar `target_host=api.anthropic.com`.
- Sidecar SNI bucket `anthropic_api`.
- Sidecar HTTP Host bucket `anthropic_api` or same Host observation mode as CP3.
- Same safe summary fields as CP3.
- Same loopback-only guard requirement as CP2.
- Sidecar-local dial override may map logical host to loopback collector; it must not be request-controlled or production-enabled.

**Decision states:**

- `SIDECAR_MATCHES_SNI_ORACLE`: all compared safe fields match the new Claude Code `2.1.179` SNI-preserving oracle.
- `SIDECAR_STILL_MISMATCH`: any compared safe field differs.
- `BLOCKED_DYNAMIC_EGRESS_GUARD`: loopback-only guard failed or could not be proven.

**Supplemental old-oracle classification:**

- `OLD_ORACLE_WAS_NO_SNI_OR_COLLECTOR_HOST` may be recorded as a CP0/CP3 explanatory classification only. It is not a sidecar comparison pass state and cannot by itself unlock return-to-65.

**Evidence output:**

- `$EVIDENCE_ROOT/safe/cp5-sidecar-vs-sni-oracle-comparison.json`

**Review gate:** Reviewer verifies both sides used the same logical target host, SNI, Host-bucket policy, and safe summary comparison rules.

---

## Checkpoint 6 - Report, Decision, and Return-to-65 Gate

**Goal:** Publish the remediation evidence and decide whether Plan 65 CP2 may be retried with the new SNI-preserving oracle.

**Create report:**

- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/docs/anti-ban/70-sni-preserving-claude-code-tls-oracle-evidence-report.md`

**Report must include:**

- Plan 65 CP2 source anchor fields from doc 65/doc 66 and, if available, `$PLAN_65_EVIDENCE_ROOT/safe/cp2-sidecar-tls-summary.json`: `BLOCKED_TLS_ENGINE_MISMATCH`, sidecar logical-SNI condition, sidecar JA3/extension count, old comparison baseline, and blocked reason.
- CP0 old-oracle classification.
- CP1 capture harness summary.
- CP2 egress guard result.
- CP3 Claude Code `2.1.179` SNI-preserving oracle safe summary.
- CP4 optional later-version results or `VERSION_NOT_AVAILABLE`.
- CP5 sidecar comparison result.
- Artifact-class-aware leak scan result.
- Explicit statement that no live upstream was called and no production service was touched.
- Explicit statement that live canary remains forbidden without separate user approval.

**Final decision states:**

- `SNI_ORACLE_REBASE_READY_FOR_65_CP2_RETRY`: allowed only when CP2 guard passed, CP3 captured a valid SNI-preserving Claude Code `2.1.179` oracle, CP5 is exactly `SIDECAR_MATCHES_SNI_ORACLE`, the artifact-class-aware leak scan passed, and the report explicitly supersedes doc-63 only for production SNI decisions.
- `SIDECAR_MATCHES_SNI_ORACLE`: sidecar matches the new SNI-preserving oracle; this is a prerequisite for `SNI_ORACLE_REBASE_READY_FOR_65_CP2_RETRY`, but CP3/CP6/live canary still require their own gates.
- `SIDECAR_STILL_MISMATCH`: sidecar still does not match; do not proceed to deployed equivalence.
- `OLD_ORACLE_CONTEXT_INSUFFICIENT`: old oracle cannot be proven production SNI context; production parity remains unclaimed until CP3 succeeds.
- `BLOCKED_DYNAMIC_EGRESS_GUARD`: no dynamic capture may run.
- `BLOCKED_SNI_ORACLE_CAPTURE`: Claude Code SNI-preserving capture cannot be performed safely.

**Return-to-65 rule:**

- If CP2 guard passed, CP3 captured a valid SNI oracle, CP5 returns `SIDECAR_MATCHES_SNI_ORACLE`, and leak scan passed, reopen Plan 65 CP2 with the new SNI-preserving oracle as the comparison baseline and explicitly document that doc-63 old oracle was superseded for production SNI decisions.
- If CP5 returns `SIDECAR_STILL_MISMATCH`, keep Plan 65 blocked and write a new TLS engine remediation plan.
- Even after `SIDECAR_MATCHES_SNI_ORACLE`, do not run deployed local-only equivalence or live canary until the user separately approves the next execution phase.

**Review gate:** Reviewer verifies the report's final decision does not overclaim production readiness.

---

## Required Test Matrix for Execution

The future executor must run the targeted tests introduced or affected by this plan:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest tools.tests.test_claude_code_real_oracle_loopback tools.tests.test_claude_code_tls_oracle -v
```

If Sub2API service code is touched:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/backend
go test ./internal/service -run 'TLSProfile|CCGateway|FormalPool|Boundary|NoBypass|Spoof|ObservedProfile|Canonical|SessionTuple|ClaudePlatformAWS' -count=1
```

If CC Gateway sidecar code is touched:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/sidecar/egress-tls-sidecar
go test ./...
go build ./...
```

If CC Gateway Node sidecar invocation code is touched:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5
npx tsx tests/egress-tls-sidecar.test.ts
npx tsx tests/egress-tls-profile.test.ts
npx tsc --noEmit
```

## Evidence and Leak Scan Requirements

Before any final report is accepted, run an artifact-class-aware leak scan over:

- `$EVIDENCE_ROOT/safe`
- `docs/anti-ban/69-sni-preserving-claude-code-tls-oracle-remediation-plan.md`
- `docs/anti-ban/70-sni-preserving-claude-code-tls-oracle-evidence-report.md`
- Modified Sub2API oracle tools and tests
- Modified CC Gateway sidecar files and tests
- Local collector runtime logs

The scan must fail on real secrets, raw TLS artifacts, raw ClientHello, pcap markers, key/cert material, raw request/response/body content, raw proxy credentials, account identifiers, workspace IDs, and raw HMAC material. Policy prose may mention forbidden categories only as prohibitions.

## Self-check Before Executing This Plan

- The plan does not use no-SNI, localhost, loopback IP, or `tls.sub2api.org` as a production SNI oracle.
- The plan preserves logical `api.anthropic.com` SNI/Host while preventing real upstream egress.
- The plan treats doc-63 as historical until SNI context is proven.
- The plan requires same-scope hard egress guard before dynamic Claude Code capture.
- The plan records only safe summaries and explicit buckets.
- The plan cannot by itself enable production, deployed equivalence, or live canary.
- The plan has a clear return-to-65 CP2 gate and a clear stop condition if the sidecar still mismatches.
