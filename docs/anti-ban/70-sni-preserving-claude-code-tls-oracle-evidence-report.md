# 70 - SNI-preserving Claude Code TLS Oracle Evidence Report

## Final decision

- CP0 old oracle context: `OLD_ORACLE_CONTEXT_INSUFFICIENT`.
- CP2 hard egress guard: `PASS`.
- CP3 canonical SNI oracle: `SNI_ORACLE_CAPTURED` for Claude Code `2.1.179`.
- CP5 sidecar comparison: `SIDECAR_STILL_MISMATCH`.
- Return to Plan 65 CP2 retry: `false`.
- Final remediation decision: `SIDECAR_STILL_MISMATCH`.

This plan successfully re-rooted the Claude Code oracle under logical `api.anthropic.com` SNI, but the current CC Gateway Go/uTLS sidecar still does **not** match that new oracle. Do not proceed to deployed local-only equivalence or live canary from this evidence. A new TLS engine remediation plan is required before production TLS safe-summary equivalence can be claimed.

## Safety posture

- No production service was deployed, restarted, stopped, reconfigured, or bound over `3012`, `3017`, `18080`, or `18081`.
- No real Anthropic, AWS, Vertex, Bedrock, OpenAI, DeepSeek, or other paid/credentialed upstream was contacted.
- Dynamic captures used same-scope loopback-only guard plus local loopback CONNECT/collector or sidecar-local dial override.
- Host header was not decrypted or observed; evidence records `host_header_bucket=not_observed_tls_only`.
- No global root CA, user CA/profile, or global `NODE_TLS_REJECT_UNAUTHORIZED=0` was used.
- Evidence is safe-summary only: no raw ClientHello, pcap, raw TLS records, key/cert material, raw request/response/body, prompt, secrets, cookies, workspace IDs, account UUID/email, proxy credentials, or raw HMAC material.

## Evidence root

Safe evidence root:

```text
/private/tmp/sni-preserving-claude-tls-oracle-20260630T120513Z
```

## CP0 - Old doc-63 oracle context

Plan 65 CP2 source anchor:

- Status: `BLOCKED_TLS_ENGINE_MISMATCH`.
- Sidecar logical SNI condition: `True`.
- Old doc-63 baseline JA3: `e97f5146a7009cc2918b50e903b6ff8d`.
- Old doc-63 baseline extension count: `12`.
- Sidecar logical-SNI JA3: `dc782a9d905fdcee1223a3d4e8108bc6`.
- Sidecar logical-SNI extension count: `13`.
- Plan 65 blocked reason bucket: `Plan 65 requires logical provider target_host to drive SNI/Host. The actual uTLS ClientHello with logical SNI emits SNI extension and produces safe summary mismatch vs doc63 oracle, which was captured without SNI extension in the local non-decrypting oracle path.`.

Old oracle classification:

- Classification: `OLD_ORACLE_CONTEXT_INSUFFICIENT`.
- Decision: `do_not_use_old_oracle_as_production_sni_baseline`.
- Insufficient reason: `SNI presence, SNI host bucket, and logical api.anthropic.com target host bucket are not present in doc63 formal safe evidence`.

The old doc-63 oracle is therefore historical/non-production for SNI decisions unless separately proven otherwise. It is not used as the production SNI baseline in this report.

## CP1 - SNI-preserving capture harness

- Status: `PASS_AFTER_REQUIRED_EDITS`.
- Logical target host bucket: `anthropic_api`.
- CONNECT authority policy: `exact_api_anthropic_443_only`.
- Collector bind bucket: `loopback_only`.
- ClientHello parse mode: `in_memory_safe_summary_only`.
- Host header bucket policy: `anthropic_api_or_not_observed_tls_only`.
- Proxy URL policy: `loopback_http_no_userinfo_no_path_no_query_no_fragment`.
- Raw ClientHello persisted: `False`.
- Raw HTTP headers persisted: `False`.
- Global TLS trust modified: `False`.
- Targeted tests: `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest tools.tests.test_claude_code_real_oracle_loopback tools.tests.test_claude_code_tls_oracle -v` -> `PASS` (32 tests).

## CP2 - Hard loopback-only egress guard

CP2 guard status: `PASS`.

Required booleans:

| Field | Value |
| --- | --- |
| deny_all_except_loopback | `True` |
| loopback_collector_reachable | `True` |
| ipv4_external_tcp_blocked | `True` |
| ipv6_external_tcp_blocked | `True` |
| dns_udp_external_blocked | `True` |
| provider_direct_tcp_blocked | `True` |
| provider_tcp_connect_unexpected_success | `False` |
| proxy_env_only_rejected | `True` |
| non_loopback_proxy_env_rejected | `True` |
| proxy_env_only_external_path_blocked | `True` |
| real_provider_through_non_loopback_proxy_blocked | `True` |
| npm_proxy_endpoint_trust_env_rejected | `True` |
| real_cli_executed during guard | `False` |
| sidecar_executed during guard | `False` |

The direct `api.anthropic.com:443` self-test was a bare TCP connect probe only; no TLS ClientHello, SNI, HTTP bytes, credentials, or application data were sent.

## CP3 - Claude Code 2.1.179 SNI-preserving oracle

- Status: `SNI_ORACLE_CAPTURED`.
- Source: `claude_code_cli_sni_preserving`.
- Version: `2.1.179`.
- Logical target host bucket: `anthropic_api`.
- SNI present: `True`.
- SNI host bucket: `anthropic_api`.
- Host header bucket: `not_observed_tls_only`.
- JA3: `d871d02cecbde59abbf8f4806134addf`.
- JA4: `t13d0017h1_18560269b2cb_92d925a272a4`.
- ALPN: `http/1.1`.
- TLS versions: `0x0304, 0x0303`.
- Cipher count: `17`.
- Extension count: `14`.
- GREASE present: `False`.
- Mock dial override: `True`.
- Mock TLS trust override: `False`.
- Dummy cert material persisted: `False`.
- Raw ClientHello omission: `raw_clienthello_forbidden`.

This is the production-relevant SNI-preserving safe-summary oracle for the current canonical app-layer version `2.1.179`. It supersedes doc-63 only for production SNI decisions; it does not by itself approve production TLS parity.

## CP4 - Optional later-version captures

These captures are non-authoritative risk context only. They do not change canonical production `2.1.179` and make no production promotion claim.

| Version | Status | JA3 | Extension count | SNI bucket | Promotion claim |
| --- | --- | --- | --- | --- | --- |
| `2.1.185` | `SNI_ORACLE_CAPTURED` | `d871d02cecbde59abbf8f4806134addf` | `14` | `anthropic_api` | `False` |
| `2.1.196` | `SNI_ORACLE_CAPTURED` | `203503b7023848ab87b9836c336b8e81` | `10` | `anthropic_api` | `False` |

## CP5 - Sidecar same-condition comparison

- Status: `SIDECAR_STILL_MISMATCH`.
- Claim scope: `safe_summary_only`.
- Compared fields: `ja3_hash, ja4, alpn_protocols, tls_versions, cipher_count, extension_count, grease_present, sni_present, sni_host_bucket, logical_target_host_bucket, host_header_bucket`.
- Difference fields: `ja3_hash, ja4, extension_count`.
- Return to Plan 65 CP2 allowed: `False`.

Claude Code `2.1.179` SNI oracle:

- JA3: `d871d02cecbde59abbf8f4806134addf`.
- JA4: `t13d0017h1_18560269b2cb_92d925a272a4`.
- Extension count: `14`.
- SNI bucket: `anthropic_api`.

Current CC Gateway sidecar same-condition summary:

- JA3: `dc782a9d905fdcee1223a3d4e8108bc6`.
- JA4: `t13d0017h1_18560269b2cb_dd86c69b7cb0`.
- Extension count: `13`.
- SNI bucket: `anthropic_api`.

Both sides used logical `api.anthropic.com` SNI and `host_header_bucket=not_observed_tls_only`. The mismatch is therefore not explained away by the old no-SNI/collector-host concern. The current sidecar remains blocked for production safe-summary equivalence.

## Tests run

- Sub2API targeted: `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest tools.tests.test_claude_code_real_oracle_loopback tools.tests.test_claude_code_tls_oracle -v` -> PASS (39 tests final CP6 rerun).
- CC Gateway sidecar: `cd /Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/sidecar/egress-tls-sidecar && go test ./...` -> PASS.
- CP5 sidecar capture runner: local loopback current-worktree `go run` under `/private/tmp/cp5-sidecar-runner` -> PASS, safe summary only.

## Production / return-to-65 gate

Return-to-65 rule result: do **not** reopen Plan 65 CP2 as passing.

- CP2 guard passed.
- CP3 captured a valid SNI-preserving Claude Code `2.1.179` oracle.
- CP5 did **not** return `SIDECAR_MATCHES_SNI_ORACLE`; it returned `SIDECAR_STILL_MISMATCH`.
- Therefore `SNI_ORACLE_REBASE_READY_FOR_65_CP2_RETRY` is not reached.

Live canary remains forbidden without separate user approval. Deployed local-only equivalence should not run from this evidence because the sidecar TLS engine still mismatches the re-rooted oracle.

## Required next step

Write a new TLS engine remediation plan to adjust the reviewed Go/uTLS sidecar implementation so that, under logical `api.anthropic.com` SNI and the same Host-observation mode, its safe summary matches the CP3 `2.1.179` SNI-preserving oracle. Until then:

- no production TLS parity claim;
- no deployed equivalence;
- no live canary;
- no Node direct HTTPS fallback;
- formal-pool TLS path remains fail-closed/plumbing-only for production use.

## Leak scan

Leak scan result: `PASS` (0 findings). Recorded separately in:

```text
/private/tmp/sni-preserving-claude-tls-oracle-20260630T120513Z/safe/cp6-leak-scan-summary.json
```
