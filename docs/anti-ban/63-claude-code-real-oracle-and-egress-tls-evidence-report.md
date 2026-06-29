# 63 - Claude Code Real Oracle and Egress TLS Evidence Report

Date: 2026-06-29
Phase: Plan 62 Phase A only
Sub2API worktree: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool`
CC Gateway reference worktree: `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5`
Formal safe evidence root: `/private/tmp/claude-code-real-oracle-egress-tls-formal-safe-20260629T082744Z/safe`
Retained local analysis scratch root: `/private/tmp/claude-code-real-oracle-egress-tls-20260629T054028Z`

## Executive decisions

| Decision axis | Phase A decision |
| --- | --- |
| Real CLI application oracle | `REAL_ORACLE_COMPLETE` |
| TLS profile parity | `TLS_PROFILE_MISMATCH_REQUIRES_IMPLEMENTATION` |
| Production profile behavior | `NEW_PROFILE_PLAN_REQUIRED` |

Phase A did not deploy, restart, rebuild, or reconfigure production services. It did not touch 3012/3017 and did not call real Anthropic/AWS/Vertex/Bedrock/DeepSeek/OpenAI upstreams. Production egress behavior remains unchanged.

## CP0 - Baseline / evidence hygiene / CodeGraph

Status: `PASS`

Safe baseline evidence was written under the formal safe evidence root. The retained local analysis scratch root is not a commit artifact and is preserved only for possible later local analysis per user instruction. Plan 62 and prior baseline docs were read with safe read proofs only: file path, byte count, line count, and SHA-256. CodeGraph was used before code inspection in the Sub2API worktree.

## CP1 - Hard loopback-only egress gate

Status: `PASS`

Guard used: macOS process-level `sandbox-exec` loopback-only profile for child processes. This was selected to avoid global pf/root/Docker changes and to avoid interrupting 3012/3017.

Same-scope proof results:

| Required self-test | Result |
| --- | --- |
| Loopback collector reachable | `true` |
| IPv4 external TCP blocked | `true` |
| IPv6 external TCP blocked | `true` |
| DNS/UDP external blocked | `true` |
| Proxy-env-only rejected | `true` |
| Deny-all-except-loopback | `true` |

Manual proof alone was not used to unlock real CLI execution. CP1 review verdict: `PASS`.

## CP2 - Real CLI application oracle

Status: `PASS`
Decision: `REAL_ORACLE_COMPLETE`

The real Claude Code CLI was run only after CP1 passed and only inside the same `sandbox-exec` loopback-only guard. Runs used temporary HOME/XDG/Claude config/cache/npm cache directories, allowlisted environment variables, and dummy/local-only credentials. Raw prompts, request bodies, responses, telemetry, stdout, and stderr were not persisted.

Observed safe matrix:

| Version | Observed rows | Not observed rows | Key application-layer finding |
| --- | ---: | ---: | --- |
| `2.1.179` | 3 | 6 | `/v1/messages`; CCH marker present; beta token bucket `token_count:4`; prompt caching marker present |
| `2.1.181` | 3 | 6 | `/v1/messages`; CCH marker absent; beta token bucket `token_count:4`; prompt caching marker present |
| `2.1.195` | 3 | 6 | `/v1/messages`; CCH marker absent; beta token bucket `token_count:5`; prompt caching and thinking-token-count markers present |

Non-simple scenarios were explicitly marked `not_observed` when they were not emitted by the local-safe print-mode run. This is not a production parity claim.

Implication: `2.1.181` and `2.1.195` remain observed-only and must not self-promote to signed-CCH, no-CCH, strict native parity, context-1m, cache parity, or TLS egress authority.

CP2 leak scan: `PASS`, findings `0`.
CP2 review verdict: `PASS`.

## CP3 - TLS ClientHello oracle and profile comparison

Status: `PASS`
Decision: `TLS_PROFILE_MISMATCH_REQUIRES_IMPLEMENTATION`

Capture topology: local non-decrypting TCP/CONNECT ClientHello collection. No global root CA was installed. No raw ClientHello, pcap, key, or certificate material was written to evidence.

Safe TLS summary:

| Source | Version | JA3 hash | JA4 summary | ALPN | TLS versions | Cipher count | Extension count | GREASE |
| --- | --- | --- | --- | --- | --- | ---: | ---: | --- |
| Real Claude Code CLI | `2.1.179` | `e97f5146a7009cc2918b50e903b6ff8d` | `t13d0017h1_18560269b2cb_f2afa5bfee90` | `http/1.1` | `0x0304`, `0x0303` | 17 | 12 | false |
| Real Claude Code CLI | `2.1.181` | `e97f5146a7009cc2918b50e903b6ff8d` | `t13d0017h1_18560269b2cb_f2afa5bfee90` | `http/1.1` | `0x0304`, `0x0303` | 17 | 12 | false |
| Real Claude Code CLI | `2.1.195` | `e97f5146a7009cc2918b50e903b6ff8d` | `t13d0017h1_18560269b2cb_f2afa5bfee90` | `http/1.1` | `0x0304`, `0x0303` | 17 | 12 | false |
| Sub2API built-in uTLS | `sub2api-built-in-node24` | `59dbfbf96a3edf5986b1238c4cca12ae` | `t13d0017h1_18560269b2cb_ea7a6481fe87` | `http/1.1` | `0x0304`, `0x0303` | 17 | 13 | false |
| CC Gateway Node/agent | `node-agent-current` | `983846581fdb62fafdb21d2282592c57` | `t13d0052h2_7ca7f3b62f8e_c4f6fa30e2cf` | `h2`, `http/1.1` | `0x0304`, `0x0303` | 52 | 12 | false |

Comparison matrix:

| Pair | Status | Difference fields |
| --- | --- | --- |
| Real `2.1.179` vs Sub2API built-in uTLS | `DIFFERENT_UNEXPLAINED` | `ja3_hash`, `ja4`, `extension_count` |
| Real `2.1.181` vs real `2.1.179` | `MATCH` | none |
| Real `2.1.195` vs real `2.1.179` | `MATCH` | none |
| CC Gateway Node/agent vs real `2.1.179` | `DIFFERENT_UNEXPLAINED` | `ja3_hash`, `ja4`, `alpn_protocols`, `cipher_count` |

Implication: Phase A cannot claim production TLS parity. If Phase B proceeds toward production transport parity, it must implement a TLS-capable egress path/profile authority rather than relying on current CC Gateway Node/agent behavior or the current Sub2API built-in uTLS template as-is.

CP3 leak scan: `PASS`, findings `0`.
CP3 initial review found the retained analysis scratch root was not safe-only. Per user instruction, that scratch root is retained for local analysis only; formal evidence was re-rooted to a safe-only evidence root and future harness runs now separate scratch from formal evidence. Final review requires PASS on this distinction.

## CP4 - Phase A decision

Final Phase A decision:

- `REAL_ORACLE_COMPLETE`: real CLI application oracle ran safely under hard loopback-only egress.
- `TLS_PROFILE_MISMATCH_REQUIRES_IMPLEMENTATION`: real TLS evidence shows mismatch against current Sub2API built-in uTLS and CC Gateway Node/agent summaries.
- `NEW_PROFILE_PLAN_REQUIRED`: application-layer differences for `2.1.181`/`2.1.195` and TLS mismatches require a new evidence-backed profile/implementation plan before any production parity claim.

Production posture:

- No production TLS/profile behavior was changed in Phase A.
- Do not promote `2.1.181` or `2.1.195` beyond observed-only strip inputs from prior work.
- Do not claim CC Gateway production TLS parity.
- Phase B may proceed only as an implementation plan that is fail-closed until TLS profile authority and egress behavior are implemented and re-verified.

## Formal safe artifact index

- `cp0-baseline-summary.json`
- `egress-guard-summary.json`
- `cp1-tdd-summary.json`
- `cp1-leak-scan-summary.json`
- `application-oracle-summary.json`
- `cp2-application-decision-summary.json`
- `cp2-leak-scan-summary.json`
- `tls-oracle-summary.json`
- `tls-profile-comparison-summary.json`
- `cp3-tdd-summary.json`
- `cp3-leak-scan-summary.json`
- `cp4-final-leak-scan-summary.json`

No raw prompt, raw request body, raw response, raw telemetry, raw CCH, raw ClientHello, pcap, private key, certificate, Authorization value, x-api-key value, cookie value, workspace ID, account UUID/email, or proxy credential is intentionally stored in the formal safe artifacts or this report. The retained analysis scratch root is local-only and not submitted as evidence.
