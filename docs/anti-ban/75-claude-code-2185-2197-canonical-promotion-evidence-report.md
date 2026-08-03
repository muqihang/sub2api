# Plan75 Claude Code 2.1.185 / 2.1.197 Canonical Promotion Evidence Report

- Generated UTC: 2026-07-01T10:14:42Z
- Evidence root: `/private/tmp/plan75-claude-code-2185-2197-canonical-promotion-20260701T082938Z/safe`
- Sub2API worktree: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool`
- CC Gateway worktree: `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5`
- Plan commit anchor: `b2962cda3 docs: expand claude code 2197 oracle coverage`

## Final decision

**Final decision: `BLOCKED_CONTROL_PLANE_GAP`**

Decision precedence path used: npm/version oracle passed; loopback/local-only guard passed; TLS oracle/sidecar proof passed; environment residue did not regress; family admission was reviewed as observed-only and not a blocker; CP4 control-plane/model/Sonnet/count_tokens/MCP/non-streaming proof remained insufficient, so precedence item 6 selects `BLOCKED_CONTROL_PLANE_GAP` before considering CCH/billing.

Promotion consequence: no canonical promotion to `2.1.197`; no stable fallback promotion to `2.1.185`; `2.1.179` remains rollback/current canonical; Sonnet 5 remains unpromoted/fail-closed for this plan.

## Safety statements

- No production deployment, service restart, production config change, or live canary was performed.
- No real Anthropic/AWS/Vertex/Bedrock/OpenAI/DeepSeek/credentialed/paid upstream was called; dynamic evidence records real upstream request count `0`.
- No real OAuth/API key/session cookie/account/billing credentials were used; test materials are synthetic fixtures only.
- Forbidden ports `3012`, `3017`, `18080`, and `18081` were not touched, stopped, restarted, reconfigured, or bound by Plan75. Tests used loopback ephemeral ports or non-forbidden fixture strings only.
- Evidence/report contain safe summaries only: hashes, counts, booleans, buckets, status labels, and redacted command results. Raw prompts, raw request/response bodies, raw decoded domain lists, raw ClientHello/pcap/HAR, secrets, cookies, account/workspace IDs, proxy credentials, cert/key material, and native dumps were not intentionally written.
- Scratch cleanup status: `skipped_requires_user_approval`; evidence/scratch under `/private/tmp/...` was left in place and not deleted.

## CP0-CP11 status table

| Checkpoint | Status | Evidence / notes |
|---|---|---|
| CP0 | PASS | npm/doc target lock and ancestor checks passed; safe file `cp0-anchor-version-lock.json`. |
| CP1 | PASS | public npm provenance/static diff safe summaries generated; reviewer-required provenance fields present. |
| CP1.5 | PASS_WITH_LIMITATIONS | platform/runtime/settings/new-residue audit done; canonical persona `darwin_arm64_cli`; downstream fail-closed/observed-only requirements recorded. |
| CP2 | PASS_WITH_EXPLICIT_GAPS | loopback-only CLI oracle, real upstream count 0; explicit gaps for count_tokens, MCP body marker, non-streaming, 2.1.185 Sonnet5 blocked behavior. |
| CP3 | PASS | 2.1.185 matches 2179 TLS profile; 2.1.197 differs but compiled sidecar profile matches safe oracle; sidecar tests pass. |
| CP4 | BLOCKED_CONTROL_PLANE_GAP | control-plane/count_tokens/MCP/non-streaming/model/Sonnet gaps remain; CCH remains strip_attribution. |
| CP4.5 | PASS_OBSERVED_ONLY_WITH_DYNAMIC_LIMITATION | cli dynamic proof; desktop/vscode static-only observed-only; unknown_future observed-only no authority; CP4/4.5 reviewer PASS. |
| CP5 | SKIPPED_DUE_CP4_BLOCKER | promotion tuple tests not written because Plan75 forbids promotion implementation after CP4 blocker. |
| CP6 | SKIPPED_DUE_CP4_BLOCKER | Sub2API canonical promotion implementation not performed; no canonical defaults changed. |
| CP7 | SKIPPED_DUE_CP4_BLOCKER | CC Gateway promotion tuple tests not written as promotion proof; a blocked-path CP10 guard test was added instead. |
| CP8 | SKIPPED_DUE_CP4_BLOCKER | CC Gateway canonical promotion implementation not performed; sidecar TLS profile addition only supports CP3 proof, not runtime promotion. |
| CP9 | NOT_EXECUTED_DUE_CP4_BLOCKER | three-version mock E2E/tuple switching not run because CP4 blocked promotion and CP5-CP8 were not authorized. |
| CP10 | PASS | required regression tests and sidecar tests passed; leak scan run after report draft. |
| CP11 | PASS | final report written; mandatory final reviewer verdict `PASS`. |

## npm/doc version lock

- npm dist-tags: stable `2.1.185`, latest `2.1.197`, next `2.1.197`, version `2.1.197`; modified `2026-06-30T17:55:42.305Z`.
- Target locks: primary `2.1.197`, fallback `2.1.185`; latest moved locked `False`, stable moved locked `False`.
- Official docs safe facts: Sonnet 5 model id `claude-sonnet-5`; Claude Code `2.1.197` or later required for Sonnet 5 access; 1M context / 128k output noted; token counting/default thinking behavior changes noted.
- Sub2API HEAD at CP0: `b2962cda3`; CC Gateway HEAD at CP0: `07f17f4`.

## Public package provenance safe summary

- Audited package count: `21`; required fields present: `{'file_count': True, 'integrity': True, 'sha256': True, 'tarball_url': True}`.
| Version | Package type | Package | sha256 prefix | tarball URL recorded | integrity recorded |
|---|---|---|---|---:|---:|
| 2.1.179 | darwin-arm64 | `@anthropic-ai/claude-code-darwin-arm64` | `c8e5fb712975c29f` | True | True |
| 2.1.179 | linux-x64 | `@anthropic-ai/claude-code-linux-x64` | `5da561d9c7abd1d8` | True | True |
| 2.1.179 | main | `@anthropic-ai/claude-code` | `41f0f4dfe1a10360` | True | True |
| 2.1.185 | darwin-arm64 | `@anthropic-ai/claude-code-darwin-arm64` | `16d8496d34eef29a` | True | True |
| 2.1.185 | linux-x64 | `@anthropic-ai/claude-code-linux-x64` | `b66ff90cb18524c3` | True | True |
| 2.1.185 | main | `@anthropic-ai/claude-code` | `760bfd4b79b04eef` | True | True |
| 2.1.197 | darwin-arm64 | `@anthropic-ai/claude-code-darwin-arm64` | `f5a7b05f69c3ab84` | True | True |
| 2.1.197 | linux-x64 | `@anthropic-ai/claude-code-linux-x64` | `42b12aa7a1d57d9f` | True | True |
| 2.1.197 | main | `@anthropic-ai/claude-code` | `0481de729ef296a6` | True | True |

## 2.1.179 / 2.1.185 / 2.1.197 proof matrix

| Axis | 2.1.179 rollback | 2.1.185 stable | 2.1.197 primary |
|---|---|---|---|
| npm tarball provenance | present | present | present |
| static package/runtime diff | safe bucket summary | safe bucket summary | safe bucket summary |
| platform/native scope | darwin/linux packages inspected | darwin/linux packages inspected | darwin/linux packages inspected |
| platform persona | darwin_arm64_cli rollback | darwin_arm64_cli candidate | darwin_arm64_cli candidate |
| env/new residue | Plan72 marker + new buckets audited | new bucket counts audited; not promoted | new bucket counts audited; not promoted |
| dynamic oracle | loopback observed with gaps | loopback observed with gaps | loopback observed with gaps |
| anthropic-beta | bucketed by hash/count | bucketed by hash/count | bucketed by hash/count |
| tools/MCP/permissions | tools observed; MCP configured marker gap | tools observed; MCP configured marker gap | tools observed; MCP configured marker gap |
| count_tokens | not locally observed | not locally observed | not locally observed |
| streaming/SSE | stream_true observed; non-streaming gap | stream_true observed; non-streaming gap | stream_true observed; non-streaming gap |
| error/retry/rate-limit | loopback retry/rate-limit scenario; no non-loopback fallback | same | same |
| model/control-plane | model path not observed; Sonnet5 request observed but not expected | Sonnet5 absent/blocked not proven | Sonnet5 request observed; control-plane path gap |
| CCH/billing | strip_attribution required | strip_attribution required | strip_attribution required |
| TLS SNI summary | matches Plan70/sidecar 2179 | matches/reuses 2179 | differs; compiled 2197 sidecar profile matches oracle |
| CC Gateway/Sub2API canonical rewrite | existing rollback guards pass | not implemented; not authorized | not implemented; not authorized |
| mock E2E | not rerun under promotion plan due CP4 blocker | not run | not run |
| no Node direct HTTPS fallback | covered by CP10 sidecar tests for existing path | promotion path not authorized | promotion path not authorized |
| leak scan | run on safe evidence/report/modified files | same | same |
| decision | retain rollback | not promotable | not promotable |

## Static diff safe summary

- Static scan used safe literal/count/hash buckets only; no raw extracted source, raw decoded domain list, native binary dump, or long minified function body was copied into report/evidence.
- Material bucket changes were present across `anthropic-beta`, CCH/billing/attribution, model/control-plane, tools/MCP/permission, count_tokens, streaming/SSE, settings/config, platform/runtime, date/env residue, proxy/base-url, remote-control, and editor/terminal marker categories.
- Static conclusion: 2.1.185 and 2.1.197 introduce upstream-shape-relevant bucket/count changes; static data alone is insufficient to promote either candidate.

## Platform/runtime/settings/new-residue audit

- Canonical platform/runtime persona: `darwin_arm64_cli`; inbound OS/arch/editor/terminal must not choose canonical persona.
- Local platform: `darwin_arm64`; deployment-relevant inspected packages: `darwin-arm64, darwin-x64, linux-x64, linux-arm64, linux-x64-musl, linux-arm64-musl`.
- Missing platform coverage: `none for main darwin/linux packages; win32 explicitly not inspected`.
- Covert/new-residue conclusion: editor_terminal, mcp_permission, remote_control, settings/config, proxy/base-url/timezone marker buckets are present in static scan and must be proven observed-only/fail-closed in CP5-CP9; no raw marker contents stored.
- New marker buckets recorded only as counts/hashes, including base URL, domain/keyword, editor/terminal, MCP/permission, proxy, remote-control, timezone/locale, and today-date marker buckets. Promotion remains forbidden unless these are canonicalized, observed-only, stripped, or fail-closed in later proof.

## Dynamic oracle safe summary

- CP2 status: `PASS_WITH_EXPLICIT_GAPS`; real upstream request count `0`; raw prompt/body/response written `False`.
- Loopback-only guard used macOS sandbox profile denying non-loopback network; added official-base, synthetic domain, synthetic AI-keyword, and Retry-After 429 probes after reviewer edits.
- Explicit CP2 gaps:
  - `2.1.179`: `non_streaming_request_shape_not_locally_observed_cli_emits_stream_true_for_print_scenarios`, `count_tokens_path_not_locally_observed`, `mcp_configured_upstream_body_marker_not_observed_synthetic_mcp_does_not_enter_request_body`
  - `2.1.185`: `non_streaming_request_shape_not_locally_observed_cli_emits_stream_true_for_print_scenarios`, `count_tokens_path_not_locally_observed`, `mcp_configured_upstream_body_marker_not_observed_synthetic_mcp_does_not_enter_request_body`, `sonnet5_absent_or_blocked_not_proven_cli_can_emit_claude_sonnet_5_request_must_fail_closed_in_gateway_policy`
  - `2.1.197`: `non_streaming_request_shape_not_locally_observed_cli_emits_stream_true_for_print_scenarios`, `count_tokens_path_not_locally_observed`, `mcp_configured_upstream_body_marker_not_observed_synthetic_mcp_does_not_enter_request_body`
- Gap policy: Do not claim count_tokens, MCP configured upstream body, non-streaming, or 2.1.185 Sonnet5 blocked behavior as proven by CP2; CP4/CP5-CP9 must fail-close or final decision must block.

## TLS oracle / sidecar result

- CP3 status: `PASS`.
- TLS profile decision: 2.1.179 and 2.1.185 reuse existing 2179 profile; 2.1.197 requires compiled profile tls-profile:claude-code-2.1.197-real-oracle-tcp-v1 and local sidecar test matches safe oracle fields.
| Version | Reuse 2179 | Sidecar/profile result | Difference vs 2179 |
|---|---:|---|---|
| 2.1.179 | True | existing 2179 profile match | none |
| 2.1.185 | True | existing 2179 profile match | none |
| 2.1.197 | False | compiled `tls-profile:claude-code-2.1.197-real-oracle-tcp-v1` matches oracle | ja3_hash, ja4, alpn_protocols, extension_count |

## Control-plane/tools/MCP/permissions/count_tokens/streaming/error-retry/beta/CCH/billing matrix

- CP4 status: `BLOCKED_CONTROL_PLANE_GAP`.
- Blocking gaps:
  - `2.1.179:non_streaming_request_shape_not_locally_observed_cli_emits_stream_true_for_print_scenarios`
  - `2.1.179:count_tokens_path_not_locally_observed`
  - `2.1.179:mcp_configured_upstream_body_marker_not_observed_synthetic_mcp_does_not_enter_request_body`
  - `2.1.185:non_streaming_request_shape_not_locally_observed_cli_emits_stream_true_for_print_scenarios`
  - `2.1.185:count_tokens_path_not_locally_observed`
  - `2.1.185:mcp_configured_upstream_body_marker_not_observed_synthetic_mcp_does_not_enter_request_body`
  - `2.1.185:sonnet5_absent_or_blocked_not_proven_cli_can_emit_claude_sonnet_5_request_must_fail_closed_in_gateway_policy`
  - `2.1.197:non_streaming_request_shape_not_locally_observed_cli_emits_stream_true_for_print_scenarios`
  - `2.1.197:count_tokens_path_not_locally_observed`
  - `2.1.197:mcp_configured_upstream_body_marker_not_observed_synthetic_mcp_does_not_enter_request_body`
- Candidate policy:
  - `2.1.179`: rollback canonical retained
  - `2.1.185`: not promotable as stable fallback because non-Sonnet gates still have count_tokens/MCP/non-streaming dynamic oracle gaps; Sonnet 5 must fail-closed if ever selected
  - `2.1.197`: not promotable until CP2/CP4 gaps are closed or explicit fail-closed implementation is proven for count_tokens, MCP-configured upstream shape, non-streaming shape, and model/control-plane paths
  - `attribution`: strip_attribution remains required; no no_cch/signed_cch promotion authorized
| Version | count_tokens | streaming | model/Sonnet policy | CCH/billing | TLS policy |
|---|---|---|---|---|---|
| 2.1.179 | not_locally_observed | stream_true=True; stream_false=False | not_expected_fail_closed | strip_attribution_required | reuse_2179 |
| 2.1.185 | not_locally_observed | stream_true=True; stream_false=False | fail_closed_required_not_proven_absent | strip_attribution_required | reuse_2179 |
| 2.1.197 | not_locally_observed | stream_true=True; stream_false=False | supported_candidate_required | strip_attribution_required | compiled_2197_match |

## Family admission matrix

- CP4.5 status: `PASS_OBSERVED_ONLY_WITH_DYNAMIC_LIMITATION`; required final language: `family_dynamic_incomplete_but_observed_only_admission_proven`.
- `family_dynamic_incomplete_but_observed_only_admission_proven`: Desktop and VS Code are not dynamically proven in CP2 and remain observed-only; production/live canary must later decide enable/restrict. Unknown future family has no canonical authority.
| Family | Evidence status | Admission policy | Canonical authority effect |
|---|---|---|---|
| cli | dynamic_loopback_proof_present_for_cli_candidates | admitted_observed_only | none |
| desktop | static_only_admitted_observed_only | admitted_observed_only_existing_sub2api_family_bucket_tests | none |
| unknown_future | server_side_safe_bucket_allows_unknown_as_observed_only_but_no_dynamic_family_proof | observed_only_no_authority; production should decide restrict_vs_admit later | none; cannot select version/model/beta/tls/env/cch |
| vscode_extension | static_only_admitted_observed_only | admitted_observed_only_existing_sub2api_family_bucket_tests | none |

## Sub2API / CC Gateway implementation and tests

- Sub2API: no canonical promotion defaults or tuple registry changes were made because CP4 blocked promotion. Existing rollback/profile tests passed.
- CC Gateway: no runtime canonical promotion defaults were changed. A compiled-in sidecar TLS profile for the 2.1.197 safe oracle was added under `sidecar/egress-tls-sidecar` to close CP3 TLS proof only; it is not in runtime config defaults and does not authorize promotion.
- Added `tests/formal-pool-canonical-promotion.test.ts` as a blocked-path CP10 guard proving CC Gateway formal-pool defaults remain rollback `2.1.179` and that the 2.1.197 TLS oracle exists only in sidecar source, not default runtime config.

### CP10 test results

| Test command bucket | Evidence file | Exit |
|---|---|---:|
| `ccgateway_canonical_promotion` | `cp10-ccgateway-formal-pool-canonical-promotion.rerun.txt` | 0 |
| `ccgateway_config` | `cp10-ccgateway-config.txt` | 0 |
| `ccgateway_egress_tls_profile` | `cp10-ccgateway-egress-tls-profile.txt` | 0 |
| `ccgateway_egress_tls_sidecar` | `cp10-ccgateway-egress-tls-sidecar.txt` | 0 |
| `ccgateway_env_residue` | `cp10-ccgateway-formal-pool-env-residue.txt` | 0 |
| `ccgateway_proxy_sub2api` | `cp10-ccgateway-proxy-sub2api.txt` | 0 |
| `ccgateway_tsc` | `cp10-ccgateway-tsc.txt` | 0 |
| `sidecar_go` | `cp10-sidecar-go-tests.txt` | 0 |
| `sub2api_targeted` | `cp10-sub2api-tests.txt` | 0 |

All required CP10 commands passed with exit code `0`, including Sub2API targeted `go test`, CC Gateway required `npx tsx` tests, `npx tsc --noEmit`, and sidecar `go test ./...`.

## CP9 / three-version mock E2E

- CP9 status: `NOT_EXECUTED_DUE_CP4_BLOCKED_CONTROL_PLANE_GAP`.
- Required 2.1.197 primary, 2.1.185 fallback, 2.1.179 rollback, and tuple switching `2.1.197 -> 2.1.185 -> 2.1.179` mock E2E sets are **not present**. This is intentional because CP4 blocked promotion before CP5-CP8 implementation; running promotion E2E would overclaim proof.
- Existing Plan74/CP10 sidecar tests still prove no Node direct HTTPS fallback for the current rollback path.

## Rollback knobs

- Rollback canonical remains `2.1.179`; no promotion occurred, so no rollback action is required in this plan.
- Future production rollback, if a later plan promotes, must be a single server-selected canonical tuple/config profile selection back to `2.1.179`, followed by a separately approved service restart/redeploy. This plan did not deploy or restart production.
- CC Gateway current formal-pool example retains `env.version=2.1.179`, `policy_version=2.1.179`, `tls-profile:claude-code-2.1.179-real-oracle-tcp-v1`, and `tls-bucket:claude-code-real-oracle-2179`.

## Leak scan

- Leak scan summary: `cp10-leak-scan-summary.json`; blocking findings `0`. Synthetic fixture labels/tokens in tests are not production secrets; sidecar compiled profile contains reviewed source-level uTLS profile code, not raw captured TLS records/pcap/HAR/evidence. First scan had self-referential false positives from prohibited-term labels; final scan excludes its own summary and stores counts/kinds only.

## Review verdicts

- CP1 provenance review: REQUIRED_EDITS resolved; tarball URL/integrity fields added; review passed per ledger.
- CP1.5 + CP2 review: REQUIRED_EDITS resolved with official-base/domain/AI-keyword/rate-limit probes and explicit CP2 gaps; review passed per ledger.
- CP4 + CP4.5 review: `PASS`. Reviewer agreed final path remains `BLOCKED_CONTROL_PLANE_GAP`; not TLS/CCH/family blocker; no raw evidence leak found.
- Final CP9/CP10/CP11 review: `PASS`. Final reviewer accepted the selected blocked decision and required no edits.

## Commits / working tree

- Sub2API current HEAD before final report commit: `b2962cda352a` (`b2962cda3` at CP0). Report file is newly generated and will be committed after final review if accepted.
- CC Gateway current HEAD before final sidecar/test commit: `07f17f482cd1`. Pending changes: sidecar 2.1.197 compiled TLS profile/test and blocked-path canonical promotion guard test. Pre-existing untracked tmp file `tests/formal-pool-env-residue.test.ts.tmp` was not deleted.

## Final conclusion

Plan75 did not produce sufficient proof for canonical promotion to Claude Code `2.1.197`, nor for stable fallback promotion to `2.1.185`. The correct responsible outcome is `BLOCKED_CONTROL_PLANE_GAP`. The TLS gap for `2.1.197` was closed locally with a compiled sidecar profile and tests, but CP2/CP4 control-plane gaps remain for count_tokens, MCP-configured shape, non-streaming shape, model/control-plane paths, and 2.1.185 Sonnet 5 blocked behavior. Promotion, production deployment, live canary, and real upstream access remain forbidden until a later plan closes those gaps with safe evidence.
