# 66 - Formal-pool TLS Deployed Equivalence Evidence Report

Date: 2026-06-30T10:15:52.306579Z
Phase: Plan 65 Phase C local execution
Evidence root: `/private/tmp/formal-pool-tls-deployed-equivalence-20260630T063359Z`

## Executive decision

Final decision: `BLOCKED_TLS_ENGINE_MISMATCH`.

Production posture: `NO_PRODUCTION_CHANGE_PLUMBING_ONLY_FAIL_CLOSED`.

This is a fail-closed safety outcome. Phase C implemented and tested a real Go/uTLS ClientHello execution path, but it did not reach safe-summary equivalence with the doc-63 real Claude Code oracle while preserving Plan 65's logical provider SNI/Host authority boundary. Therefore this work does **not** enable production TLS safe-summary equivalence, byte-for-byte TLS parity, deployed local-only equivalence, rollout, or live canary.

## Authoritative inputs from Phase A/B

| Input | Status |
| --- | --- |
| Real Claude Code oracle | `REAL_ORACLE_COMPLETE` |
| TLS profile comparison | `TLS_PROFILE_MISMATCH_REQUIRES_IMPLEMENTATION` |
| Phase B sidecar/profile authority plumbing | `PLUMBING_ONLY_FAIL_CLOSED_READY` |

Phase C did not change the canonical app-layer production profile. Formal-pool request shape remains server-selected `2.1.179` with strip-attribution posture unless separately approved.

## Checkpoint status and review verdicts

| Checkpoint | Status | Review verdict | Notes |
| --- | --- | --- | --- |
| CP0 Baseline / evidence hygiene / CodeGraph | `PASS` | Franklin `PASS` | Safe evidence root created; Sub2API CodeGraph refreshed. |
| CP1 Sidecar protocol and fail-closed contract | `PASS` | Cicero `PASS after required edits` | Sidecar control metadata is safe-only; loopback endpoint only; target scheme HTTPS/443; no Node fallback. |
| CP2 Real Go/uTLS engine and local ClientHello proof | `BLOCKED_TLS_ENGINE_MISMATCH` | Tesla `PASS` accepting correctly blocked decision | Real uTLS path ran under loopback-only guard. Logical provider SNI creates a safe-summary mismatch vs doc-63 oracle. |
| CP3 CC Gateway -> real sidecar local mock E2E | `NOT_RUN_BLOCKED_BY_CP2` | Not requested after CP2 block | Plan 65 requires not proceeding to equivalence proof after CP2 mismatch. |
| CP4 Sub2API authority regression | `PASS_REGRESSION` | Bohr final review `PASS` | Targeted tests cover TLS/profile authority and forged hint stripping. |
| CP5 Evidence redaction / artifact scan | `PASS` | Bohr final review `PASS` | Artifact-class aware scan result is under safe evidence root. |
| CP6 Deployed local-only equivalence | `NOT_RUN_BLOCKED_BY_CP2` | Not requested after CP2 block | No deployed canary stack was launched. 3012/3017 were not touched. |
| CP7 Final report / production decision | `PASS_FINAL_REVIEW` | Bohr final review `PASS` | This report is the safe-only decision artifact. |

## CP2 TLS sidecar result

Expected doc-63 safe summary:

| Field | Value |
| --- | --- |
| `ja3_hash` | `e97f5146a7009cc2918b50e903b6ff8d` |
| `ja4` | `t13d0017h1_18560269b2cb_f2afa5bfee90` |
| `alpn_protocols` | `http/1.1` |
| `tls_versions` | `0x0304, 0x0303` |
| `cipher_count` | `17` |
| `extension_count` | `12` |
| `grease_present` | `false` |

Observed real sidecar safe summary with logical SNI/Host preserved:

| Field | Value |
| --- | --- |
| `ja3_hash` | `dc782a9d905fdcee1223a3d4e8108bc6` |
| `ja4` | `t13d0017h1_18560269b2cb_dd86c69b7cb0` |
| `alpn_protocols` | `http/1.1` |
| `tls_versions` | `0x0304, 0x0303` |
| `cipher_count` | `17` |
| `extension_count` | `13` |
| `grease_present` | `false` |
| `mock_dial_override` | `true` |
| `mock_tls_trust_override` | `false` |

Difference fields: `ja3_hash, ja4, extension_count`.

Blocked reason: Plan 65 requires logical provider target_host to drive SNI/Host. The actual uTLS ClientHello with logical SNI emits SNI extension and produces safe summary mismatch vs doc63 oracle, which was captured without SNI extension in the local non-decrypting oracle path.

Important distinction: a no-SNI variant can match the doc-63 summary, but Plan 65 requires the logical provider `target_host` to drive SNI and Host. Dropping SNI would weaken the authority boundary and was not used to claim equivalence.

## Local-only egress guard

CP2 same-scope guard result:

| Guard self-test | Result |
| --- | --- |
| loopback collector reachable | `true` |
| IPv4 external TCP blocked | `true` |
| IPv6 external TCP blocked | `true` |
| DNS/UDP external blocked | `true` |
| proxy-env-only rejected | `true` |
| real provider host connect blocked | `true` |

No real Anthropic/AWS/Vertex/Bedrock/OpenAI upstream was called.

## Code changes summary

Sub2API:

- `backend/internal/service/cc_gateway_adapter.go`: hardened TLS-profile hint detection to strip/ignore snake, kebab, colon, dot, and camelCase client authority hints before canonical observed-profile construction.
- `backend/internal/service/cc_gateway_tls_profile_contract_test.go`: regression coverage for header/query/body TLS profile hints, including camelCase variants, proving signed context remains server-selected.
- `tools/claude_code_tls_oracle.py`: added sidecar summary source and doc-63 comparison helper.
- `tools/tests/test_claude_code_tls_oracle.py`: tests for sidecar safe-summary match/mismatch classification and raw-material omission.

CC Gateway:

- `src/egress-sidecar-client.ts`: hardened sidecar config/control contract; loopback literal endpoints only; HTTPS/443 only; logical target host support; summary header strict parsing; unsafe config key rejection.
- `src/proxy.ts`: sidecar invocation uses the logical provider host with HTTPS/443 and final verifier path.
- `tests/egress-tls-sidecar.test.ts`: protocol/fail-closed negatives, summary header negatives, and mock E2E updates.
- `sidecar/egress-tls-sidecar/`: Go/uTLS sidecar module with control/profile/summary/tlsengine packages and tests.
- `config.sub2api.formal-pool.example.yaml`: disabled-by-default sidecar config and Phase C block note.
- `docs/formal-pool-sub2api-safety.md`: updated production posture to `BLOCKED_TLS_ENGINE_MISMATCH`.

## Test results recorded for Phase C

Final verification commands:

- `cd /Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5 && npx tsx tests/egress-tls-profile.test.ts && npx tsx tests/egress-tls-sidecar.test.ts && npx tsx tests/proxy-sub2api.test.ts && npx tsx tests/config.test.ts && npx tsc --noEmit && git diff --check` => PASS
- `cd /Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/sidecar/egress-tls-sidecar && go test ./... && go build ./...` => PASS
- `cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool && (cd backend && go test ./internal/service -run 'TLSProfile|CCGateway|FormalPool|Boundary|NoBypass|Spoof|ObservedProfile|Canonical|SessionTuple|ClaudePlatformAWS' -count=1 && go test ./internal/repository -count=1) && PYTHONDONTWRITEBYTECODE=1 python3 -m unittest tools.tests.test_claude_code_tls_oracle -v && git diff --check` => PASS

A new RED/GREEN regression was added after a late review found camelCase TLS profile hints could affect `unknown_top_level_body_key_count`. RED failed as expected; after normalizing compact key forms, the targeted test passed.

## Safety and leak posture

Safe evidence contains only summaries, profile refs, bucket labels, status codes, and test command results. It intentionally omits raw prompt, request body, response body, telemetry, CCH, Authorization, x-api-key, cookie, workspace ID, account UUID/email, proxy credential, HMAC input/output, raw ClientHello, pcap, private key, certificate, and raw TLS templates.

CP5 artifact scan: `PASS` at `/private/tmp/formal-pool-tls-deployed-equivalence-20260630T063359Z/safe/cp5-artifact-leak-scan.json`. Findings: `0`. Scanned file count: `30`.

## Production decision and rollback

Final state: `BLOCKED_TLS_ENGINE_MISMATCH`.

Production enablement decision: no production change. Do not enable formal-pool TLS sidecar safe-summary equivalence, parity, deployed equivalence, or live canary from this evidence.

Rollback / kill-switch posture remains:

- `shared_pool.egress_tls.enabled=false`
- `egress_tls_sidecar.enabled=false`
- force `strip_attribution` canonical profile
- disable formal-pool egress if final verifier, sidecar, profile, or evidence is uncertain
- never fall back from sidecar failure to Node direct HTTPS

## Next required work before any deployed/live step

A new approved oracle/profile plan must resolve the SNI-vs-oracle mismatch without weakening the Plan 65 authority boundary. Only after CP2 safe-summary equivalence passes may CP3 local mock E2E and CP6 deployed local-only equivalence be attempted. Any live canary still requires separate explicit approval.

## Final review

Bohr final review: `PASS`. Required edits from the prior review were closed before this final verdict.
