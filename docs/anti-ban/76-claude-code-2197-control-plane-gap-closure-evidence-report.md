# Plan76 Claude Code 2.1.197 Control-plane Gap Closure Evidence Report

## Final decision

`PROMOTE_CANONICAL_2197_MOCK_E2E_READY`

Canonical promotion to Claude Code `2.1.197` is **mock-E2E-ready**. This is not production deployment approval and not live canary approval.

## Decision precedence path

1. Version oracle locked: npm metadata target remained `latest=2.1.197`, `next=2.1.197`, `stable=2.1.185`, `version=2.1.197`, `time.modified=2026-06-30T17:55:42.305Z`.
2. Local-only guard passed: CP3/CP8 report real upstream count `0` and non-loopback attempt count `0`.
3. TLS did not regress: CP7/CP8/CP9 TLS profile and sidecar tests passed.
4. Plan72 env residue did not regress: env residue regression tests passed and noncanonical/new residue markers are observed-only/canonicalized/stripped/fail-closed.
5. count_tokens, MCP configured shape, non-streaming shape, model/control-plane, CCH/billing, and Sonnet 5 gates are closed by observed shape or deterministic fail-closed policy before upstream/sidecar send.
6. Plan75 CP5-CP9 resume completed with Sub2API, CC Gateway, three-version mock E2E, CP9 regression, leak scan, and CP10 final review PASS.

## CP0-CP10 status

| Checkpoint | Status | Safe summary |
|---|---|---|
| CP0 | PASS | Plan75 blocked state and npm/doc target lock recorded. |
| CP1 | PASS | Hermetic strategy and closure schema recorded. |
| CP2 | PASS | Fail-closed tests added before policy implementation. |
| CP3 | PASS | Hermetic loopback dynamic retry completed; missing shapes not emitted except safe observed Sonnet 5 messages path. |
| CP4 | PASS | Policy closures and canonical model policy implemented; closure matrix has 18 rows. |
| CP5 | PASS | Independent control-plane gap review PASS after required edits resolved. |
| CP6 | PASS | Sub2API canonical tuple implementation/tests passed. |
| CP7 | PASS | CC Gateway canonical tuple implementation/tests passed. |
| CP8 | PASS | Three-version local mock E2E and same-scope egress guard passed. |
| CP9 | PASS | Regression tests, sidecar tests, leak scan passed. |
| CP10 | PASS | Final review PASS; no required edits. |

## Exact Plan75 blockers and Plan76 closure methods

| Plan75 blocker | Plan76 closure bucket | Closure method | Stable code(s) |
|---|---|---|---|
| `count_tokens_path_not_locally_observed` | `count_tokens_path_not_locally_observed` | `policy_fail_closed` | `formal_pool_count_tokens_profile_unapproved` |
| `mcp_configured_upstream_body_marker_not_observed_synthetic_mcp_does_not_enter_request_body` | `mcp_configured_upstream_body_marker_not_observed` | `policy_fail_closed` | `formal_pool_mcp_shape_unapproved` |
| `non_streaming_request_shape_not_locally_observed_cli_emits_stream_true_for_print_scenarios` | `non_streaming_request_shape_not_locally_observed` | `policy_fail_closed` | `formal_pool_non_streaming_profile_unapproved` |
| `model_control_plane_path_not_observed` | `model_control_plane_path_not_observed` | `policy_fail_closed` | `formal_pool_control_plane_unapproved` |
| `2.1.185_sonnet5_absent_or_blocked_not_proven_cli_can_emit_claude_sonnet_5_request_must_fail_closed_in_gateway_policy` | `sonnet5_policy` | `observed_shape_plus_policy_fail_closed_for_unobserved_subpaths`, `policy_fail_closed` | `allow_server_selected_2_1_197_tuple_else_formal_pool_model_version_unsupported`, `formal_pool_model_version_unsupported` |
| `2.1.197_sonnet5_model_control_plane_behavior_requires_policy_closure_for_unobserved_subpaths` | `sonnet5_policy` | `observed_shape_plus_policy_fail_closed_for_unobserved_subpaths`, `policy_fail_closed` | `allow_server_selected_2_1_197_tuple_else_formal_pool_model_version_unsupported`, `formal_pool_model_version_unsupported` |
| `plan75_cp1_5_new_residue_marker_buckets_must_be_observed_only_canonicalized_stripped_or_fail_closed` | `new_residue_marker_policy` | `policy_fail_closed` | `formal_pool_env_residue_verifier_failed` |

## 2.1.179 / 2.1.185 / 2.1.197 proof matrix

| Version | Role | Proof result |
|---|---|---|
| 2.1.179 | rollback/current canonical | PASS: rollback tuple accepted; Sonnet 5 fail-closed; count_tokens/MCP/non-streaming/model-control-plane policy fail-closed. |
| 2.1.185 | stable fallback | PASS: fallback tuple accepted; Sonnet 5 fail-closed before upstream/sidecar; non-Sonnet gates pass. |
| 2.1.197 | primary promotion target | PASS: server-selected canonical tuple supports Sonnet 5 mock path; unobserved subpaths fail-closed; TLS profile selectable only by server tuple. |


## Full closure matrix

|version|blocker|closure_method|route_method_bucket|header_key_set_bucket|beta_token_set_hash_or_bucket|body_structural_schema_hash_or_bucket|model_id_alias_bucket|streaming_flag_bucket|mcp_configured_absent_diff_bucket|error_http_status_bucket|stable_error_code|guard_before_upstream_or_sidecar_send|real_upstream_count|non_loopback_attempt_count|
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
|2.1.179|count_tokens_path_not_locally_observed|policy_fail_closed|POST /v1/messages/count_tokens|canonical_formal_pool_attestation_plus_gateway_headers|policy_selected_canonical_or_absent_for_fail_closed|observed_safe_hash_for_messages_else_unobserved_shape_fail_closed|not_applicable_or_observed_only|stream_true_observed|observed_only_or_not_applicable|status_403|formal_pool_count_tokens_profile_unapproved|True|0|0|
|2.1.179|mcp_configured_upstream_body_marker_not_observed|policy_fail_closed|POST /v1/messages|canonical_formal_pool_attestation_plus_gateway_headers|policy_selected_canonical_or_absent_for_fail_closed|observed_safe_hash_for_messages_else_unobserved_shape_fail_closed|not_applicable_or_observed_only|stream_true_observed|configured_marker_fail_closed|status_403|formal_pool_mcp_shape_unapproved|True|0|0|
|2.1.179|non_streaming_request_shape_not_locally_observed|policy_fail_closed|POST /v1/messages|canonical_formal_pool_attestation_plus_gateway_headers|policy_selected_canonical_or_absent_for_fail_closed|observed_safe_hash_for_messages_else_unobserved_shape_fail_closed|not_applicable_or_observed_only|stream_false_fail_closed|observed_only_or_not_applicable|status_403|formal_pool_non_streaming_profile_unapproved|True|0|0|
|2.1.179|model_control_plane_path_not_observed|policy_fail_closed|POST /v1/models|canonical_formal_pool_attestation_plus_gateway_headers|policy_selected_canonical_or_absent_for_fail_closed|observed_safe_hash_for_messages_else_unobserved_shape_fail_closed|not_applicable_or_observed_only|stream_true_observed|observed_only_or_not_applicable|status_403|formal_pool_control_plane_unapproved|True|0|0|
|2.1.179|sonnet5_policy|policy_fail_closed|POST /v1/messages|canonical_formal_pool_attestation_plus_gateway_headers|policy_selected_canonical_or_absent_for_fail_closed|observed_safe_hash_for_messages_else_unobserved_shape_fail_closed|claude-sonnet-5|stream_true_observed|observed_only_or_not_applicable|status_403|formal_pool_model_version_unsupported|True|0|0|
|2.1.179|new_residue_marker_policy|policy_fail_closed|POST /v1/messages|canonical_formal_pool_attestation_plus_gateway_headers|policy_selected_canonical_or_absent_for_fail_closed|observed_safe_hash_for_messages_else_unobserved_shape_fail_closed|not_applicable_or_observed_only|stream_true_observed|observed_only_or_not_applicable|status_400|formal_pool_env_residue_verifier_failed|True|0|0|
|2.1.185|count_tokens_path_not_locally_observed|policy_fail_closed|POST /v1/messages/count_tokens|canonical_formal_pool_attestation_plus_gateway_headers|policy_selected_canonical_or_absent_for_fail_closed|observed_safe_hash_for_messages_else_unobserved_shape_fail_closed|not_applicable_or_observed_only|stream_true_observed|observed_only_or_not_applicable|status_403|formal_pool_count_tokens_profile_unapproved|True|0|0|
|2.1.185|mcp_configured_upstream_body_marker_not_observed|policy_fail_closed|POST /v1/messages|canonical_formal_pool_attestation_plus_gateway_headers|policy_selected_canonical_or_absent_for_fail_closed|observed_safe_hash_for_messages_else_unobserved_shape_fail_closed|not_applicable_or_observed_only|stream_true_observed|configured_marker_fail_closed|status_403|formal_pool_mcp_shape_unapproved|True|0|0|
|2.1.185|non_streaming_request_shape_not_locally_observed|policy_fail_closed|POST /v1/messages|canonical_formal_pool_attestation_plus_gateway_headers|policy_selected_canonical_or_absent_for_fail_closed|observed_safe_hash_for_messages_else_unobserved_shape_fail_closed|not_applicable_or_observed_only|stream_false_fail_closed|observed_only_or_not_applicable|status_403|formal_pool_non_streaming_profile_unapproved|True|0|0|
|2.1.185|model_control_plane_path_not_observed|policy_fail_closed|POST /v1/models|canonical_formal_pool_attestation_plus_gateway_headers|policy_selected_canonical_or_absent_for_fail_closed|observed_safe_hash_for_messages_else_unobserved_shape_fail_closed|not_applicable_or_observed_only|stream_true_observed|observed_only_or_not_applicable|status_403|formal_pool_control_plane_unapproved|True|0|0|
|2.1.185|sonnet5_policy|policy_fail_closed|POST /v1/messages|canonical_formal_pool_attestation_plus_gateway_headers|policy_selected_canonical_or_absent_for_fail_closed|observed_safe_hash_for_messages_else_unobserved_shape_fail_closed|claude-sonnet-5|stream_true_observed|observed_only_or_not_applicable|status_403|formal_pool_model_version_unsupported|True|0|0|
|2.1.185|new_residue_marker_policy|policy_fail_closed|POST /v1/messages|canonical_formal_pool_attestation_plus_gateway_headers|policy_selected_canonical_or_absent_for_fail_closed|observed_safe_hash_for_messages_else_unobserved_shape_fail_closed|not_applicable_or_observed_only|stream_true_observed|observed_only_or_not_applicable|status_400|formal_pool_env_residue_verifier_failed|True|0|0|
|2.1.197|count_tokens_path_not_locally_observed|policy_fail_closed|POST /v1/messages/count_tokens|canonical_formal_pool_attestation_plus_gateway_headers|policy_selected_canonical_or_absent_for_fail_closed|observed_safe_hash_for_messages_else_unobserved_shape_fail_closed|not_applicable_or_observed_only|stream_true_observed|observed_only_or_not_applicable|status_403|formal_pool_count_tokens_profile_unapproved|True|0|0|
|2.1.197|mcp_configured_upstream_body_marker_not_observed|policy_fail_closed|POST /v1/messages|canonical_formal_pool_attestation_plus_gateway_headers|policy_selected_canonical_or_absent_for_fail_closed|observed_safe_hash_for_messages_else_unobserved_shape_fail_closed|not_applicable_or_observed_only|stream_true_observed|configured_marker_fail_closed|status_403|formal_pool_mcp_shape_unapproved|True|0|0|
|2.1.197|non_streaming_request_shape_not_locally_observed|policy_fail_closed|POST /v1/messages|canonical_formal_pool_attestation_plus_gateway_headers|policy_selected_canonical_or_absent_for_fail_closed|observed_safe_hash_for_messages_else_unobserved_shape_fail_closed|not_applicable_or_observed_only|stream_false_fail_closed|observed_only_or_not_applicable|status_403|formal_pool_non_streaming_profile_unapproved|True|0|0|
|2.1.197|model_control_plane_path_not_observed|policy_fail_closed|POST /v1/models|canonical_formal_pool_attestation_plus_gateway_headers|policy_selected_canonical_or_absent_for_fail_closed|observed_safe_hash_for_messages_else_unobserved_shape_fail_closed|not_applicable_or_observed_only|stream_true_observed|observed_only_or_not_applicable|status_403|formal_pool_control_plane_unapproved|True|0|0|
|2.1.197|sonnet5_policy|observed_shape_plus_policy_fail_closed_for_unobserved_subpaths|POST /v1/messages|canonical_formal_pool_attestation_plus_gateway_headers|policy_selected_canonical_or_absent_for_fail_closed|observed_safe_hash_for_messages_else_unobserved_shape_fail_closed|claude-sonnet-5|stream_true_observed|observed_only_or_not_applicable|status_200_or_403_by_tuple|allow_server_selected_2_1_197_tuple_else_formal_pool_model_version_unsupported|True|0|0|
|2.1.197|new_residue_marker_policy|policy_fail_closed|POST /v1/messages|canonical_formal_pool_attestation_plus_gateway_headers|policy_selected_canonical_or_absent_for_fail_closed|observed_safe_hash_for_messages_else_unobserved_shape_fail_closed|not_applicable_or_observed_only|stream_true_observed|observed_only_or_not_applicable|status_400|formal_pool_env_residue_verifier_failed|True|0|0|

## Dynamic capture retry safe summary

- Hermetic wrapper: `explicit_env_only_subprocess` with temporary HOME/config/cache, synthetic MCP only, no real credentials/settings/MCP/user HOME reads.
- Egress guard: `deny_network_allow_loopback_tcp_only` via `sandbox-exec`.
- Raw prompt/body/response persisted: `False`.
- Real upstream request count total: `0`.
- Non-loopback attempt count total: `0`.
- CP3 result: count_tokens, MCP marker, non-streaming, and model/control-plane shapes were not safely observed for canonical support; they were closed by CP4 policy. Sonnet 5 message path was observed enough for `2.1.197` server-selected tuple, with unobserved subpaths fail-closed.

## Policy closure test summary

- CP4 closure matrix rows: `18`.
- Closure methods: `{'policy_fail_closed': 17, 'observed_shape_plus_policy_fail_closed_for_unobserved_subpaths': 1}`.
- CCH/billing/attribution assertions: `{'default_attribution_posture': 'strip_attribution', 'promotion_path_no_client_attribution': True, 'promotion_path_no_raw_native_cch': True, 'promotion_path_no_signed_or_no_cch_mode': True, 'promotion_path_no_x_anthropic_billing_headers': True}`.
- CP4 policy tests: `8 passed, 0 failed`.

## Sub2API canonical tuple result

- Commit: `13f3fa08cfa58742b80c20d4534b49f82b621599` on `codex/claude-platform-aws-formal-pool`.
- Modified implementation/tests cover server-selected `2.1.197`, fallback `2.1.185`, rollback `2.1.179`, forged tuple rejection, Sonnet 5 fallback fail-closed, safe observed profile buckets, and shared contract vectors.
- CP6 evidence: `ok  	github.com/Wei-Shaw/sub2api/internal/service	11.283s`.
- CP9 evidence: `ok  	github.com/Wei-Shaw/sub2api/internal/service	10.415s`.

## CC Gateway canonical tuple result

- Commit: `fdf29bd062b378234ce3a4b9b17c9c8e6b2016d7` on `codex/claude-platform-aws-cp5`.
- Implementation/tests cover canonical persona registry/resolver, formal-pool fail-closed control-plane gates, server-selected tuple authority, observed-client non-authority, session tuple drift fail-closed, `2.1.197 -> 2.1.185 -> 2.1.179` switching for new sessions, TLS sidecar-only authority, CCH/billing stripping, and Plan72 env residue regression.
- CP7 evidence: targeted suites passed (`formal-pool-control-plane`, `formal-pool-canonical-promotion`, `formal-pool-env-residue`, `egress-tls-profile`, `tsc --noEmit`).
- CP9 evidence: see test table below.
- CodeGraph: `/opt/homebrew/bin/codegraph sync` ran after the CC Gateway commit; status shows index up to date.

## Three-version mock E2E result

- Execution: `env_i_sandbox_exec_loopback_only_node_import_tsx`.
- Primary `2.1.197`: `PASS`.
- Fallback `2.1.185`: `PASS`.
- Rollback `2.1.179`: `PASS`.
- Tuple switching `2.1.197 -> 2.1.185 -> 2.1.179`: `PASS`.
- Old session drift fail-closed: `PASS`.
- New session tuple switch allowed: `PASS`.
- Sonnet 5 under `2.1.197`: `PASS`.
- Sonnet 5 fail-closed under `2.1.185`: `PASS`.
- Node direct HTTPS fallback count: `0`.

## Same-scope loopback-only egress guard summary

| Guard | Result |
|---|---|
| DNS blocked | True |
| IPv4 non-loopback blocked | True |
| IPv6 non-loopback blocked | True |
| UDP blocked | True |
| inherited proxy env blocked | True |
| provider direct TCP blocked | True |
| real upstream count | 0 |
| non-loopback attempt count | 0 |
| forbidden ports touched | [] |

## TLS regression result

- Plan75/sidecar TLS oracle path retained.
- `2.1.197` TLS profile selectable only by server-selected canonical tuple: `PASS`.
- CP9 `egress-tls-profile`: 5 passed, 0 failed.
- CP9 `egress-tls-sidecar`: 16 passed, 0 failed.
- Sidecar Go tests passed under `sidecar/egress-tls-sidecar`.

## Plan72 env residue regression result

- CP8 env residue structural leak canonicalized/fail-closed: `PASS`.
- CP9 `formal-pool-env-residue`: 13 passed, 0 failed.
- New CP1.5 residue marker buckets are observed-only/canonicalized/stripped/fail-closed and do not affect canonical tuple authority.

## CCH/billing/attribution strip assertions

- CP4 promotion path: no `x-anthropic-billing-*`, no raw/native CCH, no signed/no-CCH mode, no client attribution.
- CP8 CCH/billing/attribution stripped: `PASS`.
- CC Gateway tests assert stripping before upstream/sidecar and no client attribution in upstream headers/body/metadata.

## Test results

| Area | Command/result evidence |
|---|---|
| Sub2API CP9 | `go test ./internal/service -run 'CCGateway|FormalPool|ObservedProfile|TLSProfile|EnvResidue|LocalEnv|Canonical|Promotion|ControlPlane|CCH|Billing|Model|CountTokens|MCP|Streaming' -count=1` -> `ok  	github.com/Wei-Shaw/sub2api/internal/service	10.415s` |
| CC Gateway CP9 | Required tsx suites + `tsc --noEmit` -> `formal-pool-control-plane 8/0; canonical-promotion 9/0; env-residue 13/0; proxy-sub2api 43/0; egress-tls-profile 5/0; egress-tls-sidecar 16/0; config 24/0; tsc passed` |
| Sidecar CP9 | `go test ./...` -> `ok  	cc-gateway/egress-tls-sidecar/internal/tlsengine	3.359s` |
| git diff --check | PASS in both worktrees. |


## Leak scan

- Status: `PASS`.
- Blocking findings: `0`.
- Scanned scope: `modified_files, plan76_new_test, safe_evidence_files`.
- No complete provider key, private key/cert, credentialed proxy URL, raw prompt/body/response material, raw decoded domain list, pcap/HAR/raw TLS, native dump, raw minified source dump, account/workspace/proxy secret material was found in scanned scope.
- Synthetic fixture literals and policy strip/assertion literals were classified as non-blocking.

## Review verdicts

| Review gate | Verdict | Notes |
|---|---|---|
| CP5 | PASS | Initial `PASS_WITH_REQUIRED_EDITS`; required edits resolved: `streaming_fail_closed_requires_body_and_observed_profile_stream_true, mcp_marker_detection_uses_real_word_boundary_regex_and_nested_marker_tests`. |
| CP10 | PASS | Reviewer `019f1e26-45d8-7b82-bdc0-73ad60dbecb2`; required edits: none; final decision justified: `PROMOTE_CANONICAL_2197_MOCK_E2E_READY`. |

## Commits

| Worktree | Branch | Commit | Notes |
|---|---|---|---|
| Sub2API implementation | `codex/claude-platform-aws-formal-pool` | `13f3fa08cfa58742b80c20d4534b49f82b621599` | Plan76 canonical tuple and contract vectors. |
| CC Gateway implementation | `codex/claude-platform-aws-cp5` | `fdf29bd062b378234ce3a4b9b17c9c8e6b2016d7` | Plan76 control-plane policy closures and canonical promotion tests. |
| Sub2API report | `codex/claude-platform-aws-formal-pool` | to be committed after this report is staged | Final Plan76 evidence report. |

## Worktree / untracked status

- Sub2API current status after implementation commit: `clean before report file creation`.
- CC Gateway current status after implementation commit: `?? .codegraph/
?? tests/formal-pool-env-residue.test.ts.tmp`.
- CC Gateway `.codegraph/` is present locally and was sync-updated; it remains uncommitted. `tests/formal-pool-env-residue.test.ts.tmp` is an existing untracked tmp file and was not deleted.

## Explicit non-goals and safety assertions

- No production deployment.
- No live canary.
- No real Anthropic/AWS/Vertex/Bedrock/OpenAI/DeepSeek or other paid/credentialed upstream access.
- No real OAuth/API key/session cookie/account/billing credentials used.
- No forbidden ports touched, stopped, restarted, reconfigured, or bound: `3012`, `3017`, `18080`, `18081`.
- No file/directory deletion, scratch cleanup, `git reset`, `git clean`, `git checkout --`, `git restore`, rebase, force push, sudo, `chmod -R`, or `chown -R`.
- Scratch cleanup status: `skipped_requires_user_approval`.

## Residual requirements before production

This report proves mock-E2E readiness for canonical promotion to `2.1.197`. Production deployment, live canary, or use of real upstream/credentials remains forbidden and requires a separate approved plan.
