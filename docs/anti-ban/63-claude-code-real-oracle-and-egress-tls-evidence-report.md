# 63 - Claude Code Real Oracle and Egress TLS Evidence Report

Date: 2026-06-29
Phase: Plan 62 Phase A + Phase B local/mock plumbing
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
| Phase B production readiness | `PLUMBING_ONLY_FAIL_CLOSED_READY` |

Phase A and Phase B did not deploy, restart, rebuild, or reconfigure production services. They did not touch 3012/3017 and did not call real Anthropic/AWS/Vertex/Bedrock/DeepSeek/OpenAI upstreams. Production egress behavior remains unchanged; Phase B is local/mock plumbing and fail-closed policy work only.

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
CP3 initial review found the retained analysis scratch root was not safe-only. Per user instruction, that scratch root is retained for local analysis only; formal evidence was re-rooted to a safe-only evidence root and future harness runs now separate scratch from formal evidence. CP3 final review verdict: PASS after re-rooting formal evidence.

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

---

# Phase B - TLS-capable egress/profile authority

## CP5 - TLS Profile Authority Model and Contract Tests

Status: `PASS`
Review verdict: `PASS` (Curie; follow-up focused addendum also `PASS` after map-key raw TLS material rejection fix)

Implemented safe authority model:

- Sub2API emits `egress_tls_profile_ref` from server-side account/egress configuration and includes it in the canonical HMAC-signed formal-pool context.
- Client TLS profile hints in header/query/body are stripped or ignored before Sub2API canonicalization. Nested TLS hint material is excluded from observed billing/body summaries.
- CC Gateway validates the TLS profile ref as part of one coherent server-selected tuple: account id, credential type/ref, egress bucket, proxy identity, route/provider, policy version, and `egress_tls_profile_ref`.
- CC Gateway strict mode fails closed for missing, unknown, unsafe, or mismatched TLS refs.
- Degraded/plumbing-only behavior marks `tls_profile_unverified`; it does not emit a verified parity claim.
- TLS profile config accepts safe refs only and rejects raw TLS template material, raw ClientHello terms, cipher/extension material, keys/certs/pcaps, token-like values, and unsafe map keys.

CP5 targeted tests:

```text
Sub2API: go test ./internal/service -run 'TLSProfile|CCGateway|FormalPool|Boundary|NoBypass|Spoof|ObservedProfile' -count=1 => PASS
CC Gateway: npx tsx tests/egress-tls-profile.test.ts => PASS
CC Gateway: npx tsx tests/proxy-sub2api.test.ts => PASS
CC Gateway: npx tsc --noEmit => PASS
```

## CP6 - Egress TLS Execution Path Design Spike

Status: `PASS`
Review verdict: `PASS` (Curie)

Implemented local/mock sidecar client plumbing:

- `egress_tls_sidecar` is disabled by default and accepts only loopback HTTP(S) endpoints in the current implementation. Plan 62 permits loopback or Unix socket; Unix socket support remains future work.
- Sidecar control channel requires a configured control token and allowlists target host, route/path, and profile ref.
- CC Gateway invokes sidecar only after formal-pool final verifier and TLS profile tuple checks pass.
- When sidecar is enabled, sidecar unavailable, unauthenticated, rejected, route/target/profile mismatch, or TLS summary mismatch fails closed.
- There is no Node direct HTTPS/proxy fallback after sidecar failure.
- Sidecar control metadata is safe-only: profile ref, egress bucket, proxy identity ref, target host/scheme/port/path, route, method, and expected TLS summary bucket. It does not include Authorization, x-api-key, cookies, raw prompt, raw body, raw response, raw CCH, raw ClientHello, pcap, key, cert, account UUID/email, or proxy credentials.

CP6 targeted tests:

```text
CC Gateway: npx tsx tests/egress-tls-sidecar.test.ts => PASS
CC Gateway: npx tsx tests/egress-tls-profile.test.ts => PASS
CC Gateway: npx tsx tests/proxy-sub2api.test.ts => PASS
CC Gateway: npx tsc --noEmit => PASS
```

## CP7 - Mock End-to-End TLS Parity Gate

Status: `PASS`
Initial review verdict: `FAIL`
Final review verdict: `PASS` after proving the sidecar-to-local-upstream hop.

Initial CP7 review found that the positive mock E2E only proved CC Gateway called the sidecar and that the sidecar self-reported a TLS bucket; it did not prove sidecar -> local upstream/collector forwarding. The fix added safe target scheme/port control metadata and updated the mock sidecar to forward streamed body bytes to a local loopback upstream only for the E2E test. The test now proves exactly one local upstream request through the sidecar and no Node direct/proxy fallback.

Mock E2E chain proven locally:

```text
client/mock native CLI request
  -> Sub2API shared formal-pool contract fixture
  -> CC Gateway final verifier
  -> TLS sidecar mock with authenticated safe control metadata
  -> local loopback upstream/collector
```

CP7 positive proof:

- account, credential type/ref, egress bucket, proxy identity, policy version, route, and `egress_tls_profile_ref` remain coherent.
- Sidecar control `profile_ref` equals the server-selected fixture profile ref.
- Sidecar control `egress_bucket` and `proxy_identity_ref` equal the fixture values.
- Sidecar response carries expected safe TLS summary bucket `tls-bucket:claude-code-real-oracle-2179`.
- Local upstream captured exactly one `/v1/messages` request from the sidecar path.
- Response is marked `x-cc-egress-tls-profile-status: tls_profile_unverified`.

CP7 negative proof:

- Forged client TLS profile headers do not alter sidecar profile authority.
- Forged body TLS hints fail closed before sidecar/upstream egress.
- Forged query TLS hints fail closed before sidecar/upstream egress.
- Account/bucket/profile mismatch fails closed before sidecar.
- Sidecar unavailable, unauthenticated, allowlist mismatch, profile mismatch, and TLS summary mismatch fail closed with no direct upstream fallback.

CP7 targeted tests:

```text
CC Gateway: npx tsx tests/egress-tls-profile.test.ts => 5 passed
CC Gateway: npx tsx tests/egress-tls-sidecar.test.ts => 10 passed
CC Gateway: npx tsx tests/proxy-sub2api.test.ts => 42 passed
CC Gateway: npx tsc --noEmit => PASS
Sub2API: go test ./internal/service -run 'TLSProfile|CCGateway|FormalPool|Boundary|NoBypass|Spoof|ObservedProfile' -count=1 => PASS
```

## CP8 - Final production decision and rollback

Final Phase B decision: `PLUMBING_ONLY_FAIL_CLOSED_READY`

Not claimed:

- `PRODUCTION_TLS_PARITY_READY_FOR_DEPLOYED_EQUIVALENCE` is **not** claimed.
- Production TLS parity is **not** claimed.
- Node-only TLS parity is **not** claimed.
- Live/deployed sidecar equivalence is **not** claimed.

Reasoning:

- CP1/CP2/CP3 completed in Phase A, and CP3 proved current Sub2API built-in uTLS and current CC Gateway Node/agent do not match the real Claude Code TLS summary.
- CP5/CP6/CP7 implemented and proved server-selected TLS profile authority and a local/mock fail-closed sidecar path.
- The sidecar execution path is still a local mock, not a deployed uTLS sidecar/connect-proxy equivalence proof.
- Therefore Phase B is ready as fail-closed plumbing for the next deployed equivalence design/canary preparation step, but not for production TLS parity enablement.

Required rollback posture:

- Set `shared_pool.egress_tls.enabled=false` to disable TLS profile strict authority mode.
- Set `egress_tls_sidecar.enabled=false` to disable sidecar execution.
- Force `strip_attribution` for attribution/CCH behavior.
- Disable formal-pool egress for affected accounts/profiles when final verifier or sidecar checks fail.
- Never fall back from sidecar failure to Node direct HTTPS or an unverified proxy path.

Production posture after Phase B:

- No production enablement was performed.
- No service was deployed, restarted, rebuilt, or reconfigured.
- 3012/3017 were not touched.
- No real paid/credentialed upstream was called.
- Deployed image/commit/config/profile equivalence and explicit user approval are still required before any live canary.

## Phase B safe artifact/code index

Sub2API:

- `backend/internal/service/cc_gateway_adapter.go`
- `backend/internal/service/admin_service.go`
- `backend/internal/service/cc_gateway_adapter_test.go`
- `backend/internal/service/cc_gateway_tls_profile_contract_test.go`
- `backend/internal/service/testdata/cc_gateway_formal_pool_contract/vectors.json`

CC Gateway:

- `src/egress-tls-profile.ts`
- `src/egress-sidecar-client.ts`
- `src/config.ts`
- `src/policy.ts`
- `src/proxy.ts`
- `tests/egress-tls-profile.test.ts`
- `tests/egress-tls-sidecar.test.ts`
- `tests/proxy-sub2api.test.ts`
- `tests/config.test.ts`
- `config.sub2api.formal-pool.example.yaml`
- `docs/formal-pool-sub2api-safety.md`

No raw prompt, raw request body, raw response, raw telemetry, raw CCH, raw ClientHello, pcap, private key, certificate, Authorization value, x-api-key value, cookie value, workspace ID, account UUID/email, or proxy credential is intentionally stored in Phase B code, test fixtures, config examples, or this report. Tests use dummy/local-only credentials and safe opaque/HMAC refs.
