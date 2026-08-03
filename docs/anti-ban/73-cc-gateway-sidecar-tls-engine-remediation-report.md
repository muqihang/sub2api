# 73 - CC Gateway Sidecar TLS Engine Remediation Report

## Final decision

- Final decision: `SIDECAR_MATCHES_SNI_ORACLE`.
- Return to Plan65 CP2/CP3 retry: `true`, for deployed local-only equivalence only.
- Live canary approval: `not_granted`.
- Production TLS parity claim: `not_claimed_by_this_report`.

This remediation updates the CC Gateway Go/uTLS sidecar so that, under logical `api.anthropic.com` SNI and local test-only loopback dial override, its safe ClientHello summary matches the Plan70 Claude Code `2.1.179` SNI-preserving oracle. This is a local safe-summary engine match, not a production deployment or live upstream approval.

## Scope and safety posture

- No production service was deployed, restarted, stopped, reconfigured, or bound over `3012`, `3017`, `18080`, or `18081`.
- No real Anthropic, AWS, Vertex, Bedrock, OpenAI, DeepSeek, credentialed, or paid upstream was contacted.
- SNI remained logical `api.anthropic.com`; the fix did not remove SNI and did not use the old doc-63 no-SNI oracle.
- No Node direct HTTPS fallback was added.
- No runtime/user/client-controlled TLS template was added.
- Raw ClientHello, raw TLS records, pcap, key/cert material, raw body, raw prompt, raw response, secrets, cookies, workspace IDs, account UUID/email, and proxy credentials were not written to docs/evidence/config/logs/fixtures.
- Because the system disk was full during tests, Go/Node build temp/cache paths were pointed to a temporary RAM disk at `/Volumes/plan73-go-build`; no user cache deletion was performed.

## Baseline from Plan70

Plan70 canonical SNI oracle:

| Field | Value |
| --- | --- |
| Claude Code version | `2.1.179` |
| logical target/SNI bucket | `anthropic_api` |
| JA3 | `d871d02cecbde59abbf8f4806134addf` |
| JA4 | `t13d0017h1_18560269b2cb_92d925a272a4` |
| cipher_count | `17` |
| extension_count | `14` |
| ALPN | `http/1.1` |
| TLS versions | `0x0304, 0x0303` |
| GREASE | `false` |

Plan70 sidecar before remediation:

| Field | Value |
| --- | --- |
| status | `SIDECAR_STILL_MISMATCH` |
| JA3 | `dc782a9d905fdcee1223a3d4e8108bc6` |
| JA4 | `t13d0017h1_18560269b2cb_dd86c69b7cb0` |
| extension_count | `13` |

Old doc-63 oracle remained demoted as `OLD_ORACLE_CONTEXT_INSUFFICIENT` and was not used as the SNI baseline.

## CP0-CP3 remediation summary

- CP0: Anchored target to Plan70 CP3 SNI oracle and current sidecar mismatch.
- CP1: Added/updated failing tests first:
  - profile expected summary must use Plan70 SNI oracle JA3/JA4/extension count;
  - sidecar ClientHello must match Plan70 SNI oracle;
  - sidecar summary must expose safe SNI bucket (`sni_present=true`, `sni_host_bucket=anthropic_api`).
- CP2: Diagnosed mismatch using safe hash/count comparison. Root cause was the missing tail TLS padding extension (type 21). Appending it at the end of the existing extension order produced the target JA3 and JA4 while keeping SNI.
- CP3: Minimal fix:
  - updated `ExpectedClaudeCode2179()` from the old doc-63 no-SNI baseline to the Plan70 SNI oracle;
  - added `UtlsPaddingExtension` at the end of `claudeCode2179LogicalSNISpec()`;
  - added safe SNI bucket fields to sidecar summaries and comparison.

No external runtime TLS profile template or client-controllable profile input was introduced.

## CP4 local same-condition collector verification

Safe evidence root:

```text
/private/tmp/plan73-sidecar-tls-remediation-20260630T162845Z
```

Safe evidence file:

```text
/private/tmp/plan73-sidecar-tls-remediation-20260630T162845Z/safe/cp4-sidecar-sni-oracle-comparison.json
```

Result:

| Field | Sidecar after remediation |
| --- | --- |
| status | `SIDECAR_MATCHES_SNI_ORACLE` |
| comparison_status | `MATCH` |
| difference_fields | `None` |
| JA3 | `d871d02cecbde59abbf8f4806134addf` |
| JA4 | `t13d0017h1_18560269b2cb_92d925a272a4` |
| cipher_count | `17` |
| extension_count | `14` |
| ALPN | `http/1.1` |
| TLS versions | `0x0304, 0x0303` |
| GREASE | `False` |
| SNI present | `True` |
| SNI bucket | `anthropic_api` |
| raw ClientHello persisted | `False` |
| pcap written | `False` |
| cert/key written | `False` |
| no real upstream | `True` |

## CP5 tests run

- `GOCACHE=/Volumes/plan73-go-build/gocache GOTMPDIR=/Volumes/plan73-go-build/gotmp TMPDIR=/Volumes/plan73-go-build/tmp go test ./internal/profile ./internal/summary ./internal/tlsengine` -> PASS.
- `GOCACHE=/Volumes/plan73-go-build/gocache GOTMPDIR=/Volumes/plan73-go-build/gotmp TMPDIR=/Volumes/plan73-go-build/tmp go test ./...` in `sidecar/egress-tls-sidecar` -> PASS.
- `TMPDIR=/Volumes/plan73-go-build/tmp npm_config_cache=/Volumes/plan73-go-build/npm-cache npx tsx tests/egress-tls-sidecar.test.ts` -> PASS (15 tests).
- `TMPDIR=/Volumes/plan73-go-build/tmp npm_config_cache=/Volumes/plan73-go-build/npm-cache npx tsx tests/egress-tls-profile.test.ts` -> PASS (5 tests).
- `TMPDIR=/Volumes/plan73-go-build/tmp npm_config_cache=/Volumes/plan73-go-build/npm-cache npx tsx tests/config.test.ts` -> PASS (23 tests).
- `TMPDIR=/Volumes/plan73-go-build/tmp npm_config_cache=/Volumes/plan73-go-build/npm-cache npx tsx tests/proxy-sub2api.test.ts` -> PASS (43 tests).
- `TMPDIR=/Volumes/plan73-go-build/tmp npm_config_cache=/Volumes/plan73-go-build/npm-cache npx tsc --noEmit` -> PASS.

## CP6 leak and safety scan

Leak scan file:

```text
/private/tmp/plan73-sidecar-tls-remediation-20260630T162845Z/safe/cp6-leak-scan-summary.json
```

Leak scan status: `PASS` with `0` findings.

## Version posture

- Production canonical remains Claude Code `2.1.179`.
- `2.1.185` matched the same Plan70 CP4 SNI safe summary and can remain auxiliary compatibility evidence only.
- `2.1.196` had a different Plan70 CP4 SNI safe summary and is not adapted or promoted by this remediation.

## Return-to-Plan65 guidance

Recommended next step: return to Plan65 CP2/CP3 for deployed local-only equivalence using the remediated sidecar, still under local-only/no-real-upstream constraints. Do not run live canary or claim deployed production TLS parity without separate approval and the Plan65 deployed equivalence gates.

## Commit anchors

- Sub2API HEAD before report commit: `ba9c44dfe7944cba3c143a01422384479381b73d`.
- CC Gateway HEAD before remediation commit: `bd4ad5c272d49ead77a7d80b9e0c51f2f0862a6c`.
