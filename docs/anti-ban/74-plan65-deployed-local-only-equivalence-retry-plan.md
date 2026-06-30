# 74 - Plan65 Deployed Local-only Equivalence Retry Addendum

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to run this checklist checkpoint-by-checkpoint. This is a lightweight Plan65 retry addendum, not a new broad TLS design plan.

**Goal:** Retry only the Plan65 CP2/CP3 local/deployed-local-only equivalence gates now that Plan73 remediated the CC Gateway Go/uTLS sidecar TLS engine.

**Architecture:** Keep Plan65 authority boundaries unchanged: Sub2API supplies server-selected formal-pool context, CC Gateway performs final verification, and the real Go/uTLS sidecar owns the TLS ClientHello. The retry must use independent local canary ports/processes, a loopback collector/mock upstream, and sidecar-local test dial override only.

**Tech Stack:** Sub2API formal-pool worktree, CC Gateway TypeScript tests/config, CC Gateway Go/uTLS sidecar, local loopback collector/mock upstream, safe JSON evidence, markdown report.

## Input anchors

- Plan70 oracle report: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/docs/anti-ban/70-sni-preserving-claude-code-tls-oracle-evidence-report.md`.
- Plan73 remediation report: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/docs/anti-ban/73-cc-gateway-sidecar-tls-engine-remediation-report.md`.
- CC Gateway remediation commit: `b76c5a70631e7dc4d5f326562737b72f7208107a`.
- Sub2API Plan73 report commit: `d06fb896281de070bf8ac6f9ce1ff6d78794a6a6`.
- Canonical TLS oracle: Claude Code `2.1.179`, logical `api.anthropic.com` SNI, JA3 `d871d02cecbde59abbf8f4806134addf`, JA4 `t13d0017h1_18560269b2cb_92d925a272a4`, `cipher_count=17`, `extension_count=14`, ALPN `http/1.1`, TLS versions `0x0304,0x0303`, GREASE `false`.
- Current sidecar status from Plan73: `SIDECAR_MATCHES_SNI_ORACLE` under local loopback same-condition collector.

## Global constraints

- Do not touch, stop, restart, reconfigure, or bind over `3012`, `3017`, `18080`, or `18081`.
- Do not deploy, restart, or reconfigure production services.
- Do not call real Anthropic, AWS, Vertex, Bedrock, OpenAI, DeepSeek, credentialed, paid, or non-local upstreams.
- Use only independent local canary ports/processes chosen dynamically outside the forbidden port set.
- Dynamic logical-provider-host tests must run under a same-scope deny-all-except-loopback egress guard. If the guard cannot prove loopback-only behavior, stop with `BLOCKED_LOCAL_ONLY_EGRESS_GUARD`.
- Only sidecar-local test dial override may map logical `api.anthropic.com:443` to a loopback collector/mock upstream. The override must not be request-controlled and must never be production-enabled.
- Do not write raw ClientHello, raw TLS records, pcap, raw request body, raw response, raw prompt, secrets, cookies, workspace/account identifiers, proxy credentials, private keys, certificates, or mock CA material to repo/docs/evidence/logs/fixtures.
- Evidence may contain safe summaries only: hashes, counts, safe bucket labels, booleans, commit/config hashes, route/status buckets, and test command results.
- Do not modify or stage 71号 files: `tools/claude_code_local_env_attribution_oracle.py` and `tools/tests/test_claude_code_local_env_attribution_oracle.py`.
- This addendum does not approve live canary, does not claim real production TLS parity, and does not approve production rollout. Live canary requires separate explicit approval.

## Allowed final decisions

The final report must choose exactly one:

- `DEPLOYED_LOCAL_ONLY_EQUIVALENCE_READY`
- `BLOCKED_LOCAL_ONLY_EGRESS_GUARD`
- `BLOCKED_DEPLOYED_EQUIVALENCE`

## Checkpoint checklist

### CP0 - Anchor verification

**Goal:** Confirm the retry starts from the intended commits and safe oracle/remediation anchors.

- [ ] Verify Sub2API HEAD includes commit `d06fb896281de070bf8ac6f9ce1ff6d78794a6a6` or is a descendant.
- [ ] Verify CC Gateway HEAD includes commit `b76c5a70631e7dc4d5f326562737b72f7208107a` or is a descendant.
- [ ] Read Plan70 and Plan73 reports and extract only safe fields listed in Input anchors.
- [ ] Confirm old doc-63 no-SNI oracle is not used as the retry baseline.
- [ ] Confirm `2.1.185` and `2.1.196` are not promoted by this retry.
- [ ] Write `$EVIDENCE_ROOT/safe/cp0-anchor-verification.json`.

### CP1 - Local-only config overlay + hash

**Goal:** Build a test-only overlay for independent canary processes without modifying production config.

- [ ] Create a private scratch overlay under `$EVIDENCE_ROOT/scratch/` or `/private/tmp`, not in repo.
- [ ] Include only safe refs and local-only settings: `egress_tls_sidecar.enabled=true`, `strict=true`, expected profile ref, expected summary bucket, loopback sidecar endpoint, and sidecar-local test dial override to loopback collector/mock.
- [ ] Keep logical target host as `api.anthropic.com`; only the sidecar dial target may point to loopback.
- [ ] Compute redacted overlay hash and redacted production-intended config hash if a production-intended config file is available; otherwise record `production_intended_config_available=false`.
- [ ] Write `$EVIDENCE_ROOT/safe/cp1-config-overlay-hash.json`.

### CP2 - Same-scope egress guard

**Goal:** Prove the exact local canary/sidecar/mock scope cannot egress except loopback before running E2E.

- [ ] Use a deny-all-except-loopback guard, preferably `sandbox-exec` on macOS, for the dynamic probe scope.
- [ ] Prove loopback collector/mock reachable.
- [ ] Prove IPv4 external TCP blocked.
- [ ] Prove IPv6 external TCP blocked.
- [ ] Prove DNS/UDP external blocked.
- [ ] Prove direct `api.anthropic.com:443` TCP connect blocked using a bare TCP connect probe only; send no TLS ClientHello, SNI, HTTP bytes, credentials, or application data.
- [ ] Prove proxy-env-only/non-loopback-proxy path rejected or blocked.
- [ ] If any required proof fails, stop with `BLOCKED_LOCAL_ONLY_EGRESS_GUARD` and do not run CP3.
- [ ] Write `$EVIDENCE_ROOT/safe/cp2-egress-guard.json`.

### CP3 - Deployed local-only E2E

**Goal:** Prove the local canary chain uses Sub2API formal-pool context, CC Gateway final verifier, the real Go/uTLS sidecar, and a loopback collector/mock upstream with no Node direct fallback.

Required chain:

```text
client/mock native CLI
  -> Sub2API formal-pool scheduler context/fixture
  -> CC Gateway final verifier
  -> real Go/uTLS sidecar
  -> loopback ClientHello collector/mock upstream
```

- [ ] Use independent dynamic local ports/processes only; never use `3012`, `3017`, `18080`, or `18081`.
- [ ] Run the E2E under the same local-only guard proven in CP2, or record an equivalent same-scope guard proof for every launched canary process.
- [ ] Confirm CC Gateway invokes the real sidecar only after final verifier passes.
- [ ] Confirm sidecar safe summary matches the Claude Code `2.1.179` SNI oracle from Plan70/Plan73.
- [ ] Confirm `node_direct_https_fallback_count=0` and `real_upstream_request_count=0`.
- [ ] Confirm end-client TLS/profile hints do not alter the server-selected profile authority.
- [ ] Write `$EVIDENCE_ROOT/safe/cp3-deployed-local-only-e2e.json`.

### CP4 - Fail-closed / rollback drill

**Goal:** Prove disabling or breaking the sidecar path does not fall back to Node direct HTTPS and does not report verified equivalence.

- [ ] Sidecar unavailable fails closed.
- [ ] TLS summary mismatch fails closed.
- [ ] Missing/malformed/duplicate/conflicting sidecar summary header fails closed.
- [ ] `egress_tls_sidecar.enabled=false` or equivalent kill switch fails closed or enters explicitly documented non-sidecar safe mode; it must not report verified/equivalent TLS status.
- [ ] Formal-pool egress disabled or `shared_pool.egress_tls.enabled=false` equivalent does not produce Node direct HTTPS fallback.
- [ ] Write `$EVIDENCE_ROOT/safe/cp4-fail-closed-rollback.json`.

### CP5 - Leak scan

**Goal:** Verify code, docs, safe evidence, test logs, and overlay artifacts contain no raw TLS or secret material.

- [ ] Scan modified CC Gateway files.
- [ ] Scan new Plan74 docs/report.
- [ ] Scan `$EVIDENCE_ROOT/safe/`.
- [ ] Scan any local canary logs retained for the checkpoint; prefer not retaining logs at all.
- [ ] The scanner may allow policy text mentions and safe booleans such as `raw_clienthello_persisted=false`, but must fail on actual secrets/raw TLS/key/cert/pcap/body/prompt material.
- [ ] Write `$EVIDENCE_ROOT/safe/cp5-leak-scan-summary.json`.

### CP6 - Evidence report + decision

**Goal:** Produce the final Plan74 evidence report and decide whether Plan65 deployed local-only equivalence can proceed to a separately approved live canary request.

- [ ] Create `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/docs/anti-ban/74-plan65-deployed-local-only-equivalence-evidence-report.md`.
- [ ] Include CP0-CP5 status, evidence root, safe summaries, test commands/results, config overlay hashes, rollback result, and review verdicts if any.
- [ ] Choose exactly one allowed final decision.
- [ ] State explicitly: this is not live canary approval, not deployed production TLS parity, and live canary still requires separate explicit approval.
- [ ] Commit any CC Gateway changes separately from Sub2API docs. If only Sub2API docs/report changes, commit only the Sub2API worktree docs with `git add -f` for `docs/anti-ban/*`.

## Minimum verification commands

Run as applicable:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/sidecar/egress-tls-sidecar
go test ./...

cd /Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5
npx tsx tests/egress-tls-sidecar.test.ts
npx tsx tests/egress-tls-profile.test.ts
npx tsx tests/proxy-sub2api.test.ts
npx tsc --noEmit
```

If a new focused E2E test is added for this retry, run it and list it in the evidence report.
