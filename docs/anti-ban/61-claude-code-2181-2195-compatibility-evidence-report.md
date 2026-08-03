# 61 - Claude Code 2.1.181 / 2.1.195 Compatibility Evidence Report

Status: `OBSERVED_ONLY_PROFILE_ADDED` for diagnostics/policy admission under strip; `NO_PRODUCTION_PROFILE_CHANGE` for production egress, CCH, no-CCH, strict native parity, and direct upstream fallback.

## Scope and safety constraints

- Evidence root: `/private/tmp/claude-code-compat-2181-2195-20260628T151931Z` (safe summaries only).
- No real Anthropic/AWS/Vertex/Bedrock upstream calls were made.
- Real Claude Code CLI dynamic oracle was not run because the hard egress gate could not prove deny-all-except-loopback.
- Raw prompts, raw request bodies, raw responses, raw telemetry, raw CCH, Authorization, x-api-key, API keys, cookies, raw workspace IDs, proxy credentials, account UUID/email, and raw HMAC material are non-evidence and not included here.
- Production policy remains pinned to Claude Code `2.1.179`; `2.1.181` and `2.1.195` are observed-only and cannot promote optional profiles.
- Rollback remains force `strip_attribution` or disable formal-pool egress; direct upstream fallback remains forbidden.

## Checkpoint verdicts

| Checkpoint | Status | Review verdict | Notes |
|---|---:|---|---|
| CP0 Baseline/evidence hygiene | PASS | GPT-5.5 xhigh PASS | Baseline metadata recorded; evidence root exists; default strip policy confirmed. |
| CP1 Package/static diff | PASS | GPT-5.5 xhigh PASS after leak-scan rerun | Main package and darwin-arm64 platform package acquired in tmp only; integrity OK; safe static diffs generated. |
| CP2 Localhost native oracle | BLOCKED_DYNAMIC_EGRESS_GUARD | GPT-5.5 xhigh PASS | Real CLI not executed; all real scenarios marked blocked by guard; mock/static/policy evidence continued. |
| CP3 CCH/no-CCH/strip regression | PASS with real future CCH blocked | GPT-5.5 xhigh PASS | 2.1.179 mock baseline verifier passed; future real CCH oracle blocked by egress guard; strip and gates verified by policy tests. |
| CP4 CC Gateway policy simulation | PASS | GPT-5.5 xhigh PASS | 2.1.181/2.1.195 observed profiles strip only; optional CCH profiles reject future versions; unapproved named future version fails closed. |
| CP5 Sub2API -> CC Gateway mock E2E | PASS | GPT-5.5 xhigh PASS | Mock chain passed observed 2.1.181/2.1.195 strip scenarios, forged authority, fail-closed, and no-direct-fallback scenarios. |
| CP6 Difference report/decision | PASS | Controller self-review + final leak scan PASS | This report contains safe summary only. |

## Version metadata

- npm stable at run time: `2.1.181`
- npm latest at run time: `2.1.195`
- npm next at run time: `2.1.195`
- Sub2API HEAD at baseline: `ff1881b0a1954ab74126e310a1beee056fa93fe6`
- CC Gateway HEAD at baseline: `e6889daac6babde65e52716ffc5acdc8b5ad2314`
- Managed runtime root check: `NO_PACKAGE_JSON_FOUND_UNDER_MAXDEPTH6`; version directory bucket `2.1.177`.

| Version | Publish time | Integrity | SRI OK | Signature | Provenance | Local tarball SHA256 |
|---|---|---|---:|---|---|---|
| 2.1.179 | 2026-06-16T17:51:27.805Z | present | True | signatures_present | provenance_absent | 41f0f4dfe1a10360eb43798e7decd0bdbe6b90a74a4d3017d23e7720fa9adaaa |
| 2.1.181 | 2026-06-17T18:28:59.962Z | present | True | signatures_present | provenance_absent | b4ffcded15984219efa0b6a0792030ba53be39b95092732dad45f24aaf03c54e |
| 2.1.195 | 2026-06-26T18:16:23.150Z | present | True | signatures_present | provenance_absent | a531d520e9ef0844c9883765aa7b4f83ea2f8fe914a7392accd4c249e1aec9e5 |

## Static package and bundle diff summary

Main npm package safe diff:

| Diff | File count | Added | Removed | Changed | Source maps | Known-string count diff |
|---|---|---:|---:|---:|---|---|
| main 2.1.179 -> 2.1.181 | 7 -> 7 | 0 | 0 | 2 | {'from': 0, 'to': 0} | {} |
| main 2.1.181 -> 2.1.195 | 7 -> 7 | 0 | 0 | 2 | {'from': 0, 'to': 0} | {} |

Darwin arm64 platform package safe diff (actual platform executable package):

| Diff | File count | Added | Removed | Changed | Source maps | Known-string count diff |
|---|---|---:|---:|---:|---|---|
| platform 2.1.179 -> 2.1.181 | 4 -> 4 | 0 | 0 | 2 | {'from': 0, 'to': 0} | {} |
| platform 2.1.181 -> 2.1.195 | 4 -> 4 | 0 | 0 | 2 | {'from': 0, 'to': 0} | {} |

Platform tarball integrity all OK: `True`. Platform source map counts: `{'2.1.179': 0, '2.1.181': 0, '2.1.195': 0}`.

Interpretation: static/package evidence showed metadata and declaration/executable package changes by SHA/size, but no safe manifest signal that would justify a production profile change. Known billing/control-plane/tool/cache/thinking markers did not show a count delta in the generated safe manifests.

## Dynamic oracle matrix summary

- Hard egress gate: `BLOCKED_DYNAMIC_EGRESS_GUARD`; guard type `not_available`; allowed bucket `loopback_only_not_proven`; real CLI executed `false`.
- Scenario count: `14`; all scenario status: `blocked_by_egress_guard`.
- 2.1.181 safe matrix statuses: `{'blocked_by_egress_guard': 14}`.
- 2.1.195 safe matrix statuses: `{'blocked_by_egress_guard': 14}`.

Because the hard dynamic egress gate could not prove loopback-only execution, real CLI capture is explicitly blocked and makes no request-shape claim beyond static/mock evidence.

## CCH / no-CCH / strip regression table

| Version | Oracle mode | CCH present | Verifier matched | Mismatch bucket | Billing shape | Production profile change |
|---|---|---|---|---|---|---|
| 2.1.179 | mock_self_test | True | True | none | cch_present | False |
| 2.1.181 | real_cli | None | None | blocked_by_egress_guard | unknown_blocked_by_egress_guard | False |
| 2.1.195 | real_cli | None | None | blocked_by_egress_guard | unknown_blocked_by_egress_guard | False |

Optional profiles remain exact-2.1.179 gated: `True`. Strip regression status: `verified_by_policy_tests`.

## Header, beta, request-shape, control-plane, and capability diff table

| Area | Evidence bucket | Decision |
|---|---|---|
| Control plane / request shape | Existing CC Gateway suites plus future-version focused test pass | New observed versions do not authorize profile refs; unknown body keys fail closed or are stripped/downscoped. |
| Telemetry / event logging | Static bucket counts unchanged in safe manifests; control-plane route tests pass | No production forwarding change. |
| MCP / settings / policy | Static bucket counts unchanged in safe manifests; control-plane route policy tests pass | No production forwarding change. |
| ToolSearch / WebFetch / WebSearch | Real dynamic observation blocked; static safe counts did not create a production signal | Observed-only/no claim; no profile promotion. |
| Prompt caching / cache-control | Real dynamic observation blocked; static safe counts did not create a production signal | Keep 2.1.179 cache parity profile; no promotion. |
| Context management / thinking / redact-thinking | Real dynamic observation blocked; static safe counts did not create a production signal | No production shape change. |
| Model / effort metadata | Real dynamic observation blocked; safe policy tests pass | No authority derived from client metadata. |
| Billing/CCH | CP3 table and policy tests pass; future real verifier blocked by guard | Strip remains default; signed/no-CCH remain exact 2.1.179 only. |

## CC Gateway policy simulation

- Observed versions: `['2.1.181', '2.1.195']`.
- Strip observed-only: `pass`.
- Optional signed CCH self-promotion: `blocked`.
- Optional no-CCH self-promotion: `blocked`.
- Unapproved named future strip profile: `fail_closed`; wildcard allowed: `False`.
- Direct fallback: `False`.
- Final CC Gateway rerun: `PASS` with `11` commands.

## Sub2API -> CC Gateway mock E2E summary

- Overall selected mock E2E status: `PASS`; sensitive scan: `PASS`; scenario count: `10`.
- `observed_2_1_181_strip_cch_present`: status `PASS`, mock requests `1`, real upstream `False`, upstream billing marker `False`, upstream CCH shape `False`, attested `True`, loopback-only `True`.
- `observed_2_1_195_strip_cch_present`: status `PASS`, mock requests `1`, real upstream `False`, upstream billing marker `False`, upstream CCH shape `False`, attested `True`, loopback-only `True`.
- `forged_authority_headers_ignored`: status `PASS`, mock requests `1`, real upstream `False`, upstream billing marker `False`, upstream CCH shape `False`, attested `True`, loopback-only `True`.
- `cc_gateway_unavailable_no_direct_fallback`: status `PASS`, mock requests `0`, real upstream `False`, upstream billing marker `False`, upstream CCH shape `False`, attested `False`, loopback-only `True`.
- `missing_trusted_context_fail_closed`: status `PASS`, mock requests `0`, real upstream `False`, upstream billing marker `False`, upstream CCH shape `False`, attested `False`, loopback-only `True`.
- Final Sub2API rerun: `PASS` with `4` commands.

## Verification commands

| Command | Status | Return code | Duration seconds |
|---|---:|---:|---:|
| `npx tsx tests/native-oracle-matrix.test.ts` | PASS | 0 | 2.36 |
| `npx tsx tests/cch-oracle-harness.test.ts` | PASS | 0 | 0.58 |
| `npx tsx tests/policy-cch.test.ts` | PASS | 0 | 0.4 |
| `npx tsx tests/security-boundary.test.ts` | PASS | 0 | 0.5 |
| `npx tsx tests/session-and-beta-policy.test.ts` | PASS | 0 | 0.47 |
| `npx tsx tests/config.test.ts` | PASS | 0 | 0.45 |
| `npx tsx tests/formal-pool-safety-doc.test.ts` | PASS | 0 | 0.39 |
| `npx tsx tests/proxy-sub2api.test.ts` | PASS | 0 | 1.68 |
| `npx tsx tests/preflight-safety.test.ts` | PASS | 0 | 0.48 |
| `npx tsx tests/claude-code-future-version-compat.test.ts` | PASS | 0 | 0.43 |
| `npx tsc --noEmit` | PASS | 0 | 1.04 |
| `python3 -m unittest tools.tests.test_cli_control_plane_full_chain_controller` | PASS | 0 | 0.16 |
| `python3 tools/cli_control_plane_full_chain_controller.py --tmp-root /private/tmp/claude-code-compat-2181-2195-20260628T151931Z/safe` | PASS | 0 | 83.77 |
| `go test ./internal/service -run CCGateway|FormalPool|Boundary|NoBypass|CCH|Attribution|ControlPlane|ClaudePlatformAWS|SessionTuple|Spoof|ObservedProfile -count=1` | PASS | 0 | 9.14 |
| `go test ./internal/repository -count=1` | PASS | 0 | 6.19 |

## Evidence leak scan gates

| Gate | Status | Finding count |
|---|---:|---:|
| CP0 | PASS | 0 |
| CP1 | PASS | 0 |
| CP2 | PASS | 0 |
| CP3 | PASS | 0 |
| CP4 | PASS | 0 |
| Final current artifacts | PASS | 0 |
| CP6 report | PASS | 0 |

## Risk assessment against doc-58/doc-59 boundaries

- No evidence justifies changing the doc-58/doc-59 production default from `strip_attribution`.
- `2.1.181` and `2.1.195` are admitted only as explicitly named observed-only strip inputs in CC Gateway policy simulation, not as production strict/native/CCH profiles.
- Optional `signed_cch` and `no_cch` still require exact `2.1.179` observed proof and oracle tuple; newer versions are blocked for those profiles.
- Unapproved named future versions fail closed, reducing the blind spot risk that an observed-only allowlist becomes a broad future-version bypass.
- The dynamic real CLI shape remains unknown for 2.1.181/2.1.195 because the hard egress guard was not proven. This is explicitly not a native parity claim.
- Direct upstream fallback remains forbidden; mock E2E verified CC Gateway unavailable returns no mock/upstream forwarding.

## 2.1.179 strategy blind spot assessment

One policy blind spot was identified and addressed in tests/policy simulation: explicitly named newer versions needed a narrow observed-only strip admission so diagnostics/mock E2E can pass without enabling optional profiles. A regression test now verifies an unapproved named future version still fails closed under strip, so the admission is not a wildcard. No blind spot was found that requires changing the production `2.1.179` target, signed-CCH/no-CCH gates, or direct fallback boundary.

## Decision

- Production profile decision: `NO_PRODUCTION_PROFILE_CHANGE`.
- Diagnostic/policy evidence decision: `OBSERVED_ONLY_PROFILE_ADDED`.
- `NEW_PROFILE_PLAN_REQUIRED` only if a future task wants strict native parity, `signed_cch`, or `no_cch` for `2.1.181` or `2.1.195`; this plan does not promote them.

## Explicit non-claims

- No raw body/prompt/response/CCH/telemetry evidence is included.
- No real upstream was called.
- No AWS live canary was run.
- No deployment/restart of 3017 or interaction with 3012 was performed.
- No automatic promotion of `2.1.181` or `2.1.195` to production native parity, `signed_cch`, or `no_cch` is claimed.

