# Plan72 - Canonical Local Environment Residue Defense Evidence Report

## Decision

`CANONICAL_ENV_RESIDUE_MOCK_E2E_READY`

This is local/mock readiness only. It is not production deployment approval and not live canary approval.

## Scope and safety constraints

- No production deploy/restart/reconfiguration was performed.
- No live canary was performed.
- No real Anthropic/AWS/Vertex/Bedrock/OpenAI/DeepSeek/credentialed/paid upstream call was performed.
- Ports `3012`, `3017`, `18080`, and `18081` were not touched, rebound, restarted, or reconfigured.
- Canonical production profile remains `2.1.179`; no promotion to `2.1.185` or `2.1.196`.
- `no_cch`, `signed_cch`, and strict native parity were not enabled.
- Plan73/74 TLS sidecar behavior was preserved by regression tests.
- Evidence uses safe buckets, counts, labels, command results, and paths only.

Safe evidence root:

`/private/tmp/plan72-canonical-local-env-residue-defense-20260701T045216Z/safe`

## Checkpoint status

| Checkpoint | Status | Evidence |
|---|---:|---|
| CP0 Anchor verification | PASS | `cp0-anchor-verification.json` |
| CP1 Sub2API failing tests | PASS | Tests added before implementation for canonical refs, forged hints, nested structural fields, and family observed-only admission. Historical red evidence retained in safe evidence. |
| CP2 Sub2API implementation | PASS | `cp2-sub2api-unit-contract-tests-rerun.txt`, `cp7-sub2api-targeted-tests-final.txt` |
| CP3 CC Gateway failing tests | PASS | `tests/formal-pool-env-residue.test.ts` covers missing/malformed/unsafe/mismatched refs, HMAC, duplicate semantic fields, AWS scoped path, session binding, and observed profile safe keys. |
| CP4 CC Gateway implementation | PASS | `cp4-cc-gateway-config-test-final.txt`, `cp7-cc-gateway-required-tests-final.txt` |
| CP5 Canonical rewrite + final verifier | PASS | `tests/formal-pool-env-residue.test.ts`, `cp7-cc-gateway-required-tests-final.txt` |
| CP6 Family observed-only admission | PASS | Sub2API family tests cover CLI, Desktop, VS Code, and unknown as `client_family_bucket` only. |
| CP7 Local mock E2E + regression | PASS | `cp7-sub2api-targeted-tests-final.txt`, `cp7-cc-gateway-required-tests-final.txt` |
| CP8 Leak scan / report / review | PASS | `cp8-leak-scan-summary.json` (`plan72_leak_scan.v6`), this report, final read-only review |

## CP0 anchor summary

Verified safe anchor labels:

- Plan71: `logic_confirmed`, `domain_list_confirmed`, `us_pacific_candidate`, `family_dynamic_blocked`, `ready_to_write_design`.
- Plan74: `DEPLOYED_LOCAL_ONLY_EQUIVALENCE_READY`.
- Sub2API HEAD contains required Plan72 anchor commit lineage.
- CC Gateway HEAD contains required Plan74 anchor commit lineage.

Accepted Plan71 limitations:

- The `2.1.179 official_anthropic` dynamic row was not observed.
- Desktop and VS Code dynamic oracle coverage remains blocked.
- These limitations do not block conservative server-selected canonical rewrite and verifier behavior for Plan72 local/mock readiness.

## Implemented authority refs

Sub2API now server-selects and HMAC-signs these authority refs into formal-pool context:

- `env_residue_profile_ref`: `env-residue-profile:claude-code-2.1.179-us-pacific-official-anthropic-v1`
- `locale_profile_ref`: `locale-profile:us-pacific-v1`
- `base_url_residue_profile_ref`: `base-url-residue-profile:official-anthropic-v1`

CC Gateway now requires, validates, verifies against config/defaults, and binds these refs in formal-pool session authority.

## Sub2API result summary

Sub2API changes:

- Added canonical env residue refs and safe server/account override validation.
- Added client env residue hint stripping for headers, query, top-level body fields, metadata, tool definitions, tool metadata, and tool fields.
- Kept `messages[*].content` out of stripping/scanning.
- Added observed-only safe buckets for client family and local environment residue.
- Added canonical refs to formal-pool HMAC attestation and shared contract vectors.
- Synced mutated request body back to the upstream wire body.
- Added regression coverage for alias forms including snake_case, camelCase, kebab-case, timezone, base-url, and proxy URL hints.

Targeted gate:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/backend
go test ./internal/service -run 'CCGateway|FormalPool|ObservedProfile|TLSProfile|EnvResidue|LocalEnv' -count=1
```

Result: PASS (`cp7-sub2api-targeted-tests-final-rerun.txt`; earlier pass retained in `cp7-sub2api-targeted-tests-final.txt`).

## CC Gateway result summary

CC Gateway changes:

- Added `shared_pool.env_residue` config defaults and canonical validation.
- Extended formal-pool attestation parsing to require env residue refs.
- Added safe observed profile keys and enum validation.
- Added env residue profile verification and session authority binding.
- Added AWS scoped formal-pool env residue verification and binding.
- Added canonical Pacific date marker rewrite for recognized system markers.
- Added fail-closed final verifier before sidecar/upstream egress and on retry/replay attempts.
- Final verifier covers headers, query, system text, system text block extra fields, and allowed structural body fields.
- Final verifier skips only `messages[*].content`, not sibling structural fields under `messages[*]`.
- Added fail-closed coverage for forged env/base-url/proxy/timezone aliases and unsafe refs.
- Preserved billing/CCH strip behavior and TLS sidecar regression behavior.

Required gate:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5
npx tsx tests/formal-pool-env-residue.test.ts
npx tsx tests/proxy-sub2api.test.ts
npx tsx tests/egress-tls-profile.test.ts
npx tsx tests/egress-tls-sidecar.test.ts
npx tsc --noEmit
```

Result: PASS (`cp7-cc-gateway-required-tests-final-rerun2.txt`; earlier pass retained in `cp7-cc-gateway-required-tests-final.txt`).

Additional config gate:

```bash
npx tsx tests/config.test.ts
```

Result: PASS (`cp4-cc-gateway-config-test-final-rerun.txt`; earlier pass retained in `cp4-cc-gateway-config-test-final.txt`).

## Local mock E2E assertions

Local/mock tests demonstrated:

- Upstream-bound system marker is canonical when present or absent when not present.
- Noncanonical apostrophe/date separator residue is rewritten or rejected.
- Headers and query reject env/base-url/proxy/timezone/profile residue aliases.
- Allowed structural body fields reject env/profile/base-url/timezone/proxy hints.
- `messages[*].content` is not scanned or rewritten.
- Sibling structural fields under `messages[*]` are verified and rejected if they carry residue.
- `observed_client_profile` carries only safe buckets.
- Authority refs remain canonical and are session-bound.
- AWS scoped path includes and verifies the three refs.
- Retry/replay path reruns rewrite/verifier.
- Billing/CCH strip behavior remains unchanged.
- Node direct HTTPS fallback remains zero in sidecar tests.
- Real upstream request count remains zero; only local mock upstreams were used.

## Leak scan

Leak scan summary file:

`/private/tmp/plan72-canonical-local-env-residue-defense-20260701T045216Z/safe/cp8-leak-scan-summary.json`

Summary:

- Scanned modified Sub2API files, modified CC Gateway files, tests, docs/report, and safe evidence.
- Schema: `plan72_leak_scan.v6`.
- Blocking findings: `0`.
- Decision: `PASS`.
- Synthetic fixture tokens/hosts/profile refs, TLS guard literal mentions, and workspace fixture refs were counted only; matched snippets were not stored.

## Review status

Required reviews performed:

- CP0-CP2 review: completed; findings addressed.
- CP3-CP5 review: completed; findings addressed, including query aliases, structural body aliases, system block extra fields, `messages[*]` structural sibling verification, proxy URL aliases, mixed date separators, and escaped duplicate semantic keys.
- CP8 final review: completed. Initial review found only an evidence mismatch from a stale failed leak-scan artifact; the leak scan was rerun as `plan72_leak_scan.v6` with `PASS` and `0` blocking findings. The untracked empty CC Gateway temp file was not committed and was left untouched because deletion requires explicit approval.

## Remaining conditions before any production or live canary work

- This report does not authorize production deployment.
- This report does not authorize live canary.
- Deployed/live canary preparation requires a separate explicit approval and a separate plan/gate.
