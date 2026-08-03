# Claude Code 2.1.179 Native Oracle CP1 Safe Evidence

Date: 2026-06-25

Scope: CP1 for plan 58. Localhost-only oracle capture for unmodified Claude Code CLI 2.1.179; no Anthropic upstream, no formal-pool live traffic, no 3012/3017 interaction.

## Artifact safety

- Raw captures stayed outside the repo under `/private/tmp/cc-native-oracle-2179-20260625T162816Z-valid/`.
- Committed artifact: `native-oracle-matrix-2.1.179.safe.json`.
- The committed artifact contains safe summaries only: route class, beta token names, body key sets, block counts/types, billing shape, cc_entrypoint bucket, and CCH verifier booleans.
- It does not contain raw prompts, raw request bodies, raw responses, raw CCH values, Authorization, cookies, API keys, account UUID/email, proxy credentials, raw digest, or HMAC preimage material.

## Runtime provenance

- Primary user-provided runtime path was unusable on this host, so a fresh npm tarball was downloaded into `/private/tmp/cc-native-oracle-range-2179-20260625T161520Z-23799/`.
- Verified binary: `/private/tmp/cc-native-oracle-range-2179-20260625T161520Z-23799/package/claude`.
- `claude --version` output bucket: `2.1.179 (Claude Code)`.

## Matrix result

- Schema: `claude_native_oracle_matrix.v1`
- Target CLI version: `2.1.179`
- Mode: `real-cli`
- Upstream: `127.0.0.1-stub-only`
- real_anthropic_upstream: `false`
- raw persisted flags: body=`false`, prompt=`false`, response=`false`, cch=`false`

| profile_ref | invocation_mode | observed_version | variants | billing_shape | cc_entrypoint_bucket | CCH verifier | degraded_scope |
|---|---|---|---|---|---|---|---|
| `claude_code_2_1_179_custom_base` | `custom-base` | `2.1.179 (Claude Code)` | `messages_non_streaming, messages_streaming, messages_with_tools` | `cch_present` | `sdk-cli` | `true` | `non_streaming_messages_not_observed` |
| `claude_code_2_1_179_first_party_assumed` | `first-party-assumed` | `2.1.179 (Claude Code)` | `messages_non_streaming, messages_streaming, messages_with_tools` | `cch_present` | `sdk-cli` | `true` | `non_streaming_messages_not_observed` |

Observed details, safe-only:

### claude_code_2_1_179_custom_base

- `messages_non_streaming`: route_class=`messages`, method=`POST`, observed_request_stream=`true`, tool_count=`3`, billing_shape=`cch_present`, cc_entrypoint_bucket=`sdk-cli`, cch_verifier_ok=`true`, top_level_body_keys=`context_management, max_tokens, messages, metadata, model, output_config, stream, system, thinking, tools`, beta_tokens=`claude-code-20250219, context-management-2025-06-27, effort-2025-11-24, interleaved-thinking-2025-05-14, prompt-caching-scope-2026-01-05`.
- `messages_streaming`: route_class=`messages`, method=`POST`, observed_request_stream=`true`, tool_count=`3`, billing_shape=`cch_present`, cc_entrypoint_bucket=`sdk-cli`, cch_verifier_ok=`true`, top_level_body_keys=`context_management, max_tokens, messages, metadata, model, output_config, stream, system, thinking, tools`, beta_tokens=`claude-code-20250219, context-management-2025-06-27, effort-2025-11-24, interleaved-thinking-2025-05-14, prompt-caching-scope-2026-01-05`.
- `messages_with_tools`: route_class=`messages`, method=`POST`, observed_request_stream=`true`, tool_count=`3`, billing_shape=`cch_present`, cc_entrypoint_bucket=`sdk-cli`, cch_verifier_ok=`true`, top_level_body_keys=`context_management, max_tokens, messages, metadata, model, output_config, stream, system, thinking, tools`, beta_tokens=`claude-code-20250219, context-management-2025-06-27, effort-2025-11-24, interleaved-thinking-2025-05-14, prompt-caching-scope-2026-01-05`.

### claude_code_2_1_179_first_party_assumed

- `messages_non_streaming`: route_class=`messages`, method=`POST`, observed_request_stream=`true`, tool_count=`3`, billing_shape=`cch_present`, cc_entrypoint_bucket=`sdk-cli`, cch_verifier_ok=`true`, top_level_body_keys=`context_management, max_tokens, messages, metadata, model, output_config, stream, system, thinking, tools`, beta_tokens=`claude-code-20250219, context-management-2025-06-27, effort-2025-11-24, interleaved-thinking-2025-05-14, prompt-caching-scope-2026-01-05`.
- `messages_streaming`: route_class=`messages`, method=`POST`, observed_request_stream=`true`, tool_count=`3`, billing_shape=`cch_present`, cc_entrypoint_bucket=`sdk-cli`, cch_verifier_ok=`true`, top_level_body_keys=`context_management, max_tokens, messages, metadata, model, output_config, stream, system, thinking, tools`, beta_tokens=`claude-code-20250219, context-management-2025-06-27, effort-2025-11-24, interleaved-thinking-2025-05-14, prompt-caching-scope-2026-01-05`.
- `messages_with_tools`: route_class=`messages`, method=`POST`, observed_request_stream=`true`, tool_count=`3`, billing_shape=`cch_present`, cc_entrypoint_bucket=`sdk-cli`, cch_verifier_ok=`true`, top_level_body_keys=`context_management, max_tokens, messages, metadata, model, output_config, stream, system, thinking, tools`, beta_tokens=`claude-code-20250219, context-management-2025-06-27, effort-2025-11-24, interleaved-thinking-2025-05-14, prompt-caching-scope-2026-01-05`.

## CP1 coverage status

| Requirement | Status | Safe observation / disposition |
|---|---|---|
| 2.1.179 custom-base profile | captured | Safe profile `claude_code_2_1_179_custom_base` exists. |
| 2.1.179 first-party-assumed profile | captured | Safe profile `claude_code_2_1_179_first_party_assumed` exists. |
| `/v1/messages` streaming request shape | captured | All samples emitted `/v1/messages` with `stream=true`. |
| `/v1/messages` non-streaming body shape | degraded | `--output-format json` still emitted an upstream `stream=true` request in 2.1.179; the matrix records `non_streaming_messages_not_observed`. Production must not assume a proven non-streaming 2.1.179 body shape from this CP1 run. |
| Tool definition set | captured | Safe summary observed `tool_count=3` in each messages sample. |
| Tool use / tool result turn | not_observed/degraded | Minimal localhost oracle did not force a tool-use turn. Do not use this CP1 as tool-turn parity proof. |
| `/v1/messages/count_tokens` | not_observed/degraded | Not emitted by the minimal native CLI invocations captured here. Control-plane isolation must remain fail-closed until separately proven. |
| Retry / synthetic upstream failure | not_observed/degraded | Not exercised in this CP1 run. |
| MCP/settings/policy/event/control-plane routes | not_observed/degraded | Not emitted by the minimal native CLI invocations captured here. They remain control-plane/capability paths, not messages-signing authority. |
| OAuth/API-key account shape | unavailable/degraded | No real account identity was used or stored. Credential shape must remain server-owned and attested by Sub2API/CC Gateway, not inferred from this oracle. |

## CCH decision

- Both captured 2.1.179 profiles observed `billing_shape=cch_present`, `cc_entrypoint_bucket=sdk-cli`, and `cch_verifier_ok=true` for the captured messages samples.
- This is evidence that the current CC Gateway verifier matches the captured 2.1.179 samples; it is not production authorization by itself.
- Production default remains `strip_attribution`. `signed_cch` or `no_cch` must still require explicit `egress_profile_ref` plus oracle/profile proof and CC Gateway final verifier approval.
- Unknown future versions, unknown betas, unknown body keys, and unknown billing shapes must strip/downscope or fail closed; never auto-promote to signed/no-CCH.

## Commands and results

```text
node --import tsx tests/native-oracle-matrix.test.ts
=> 4 passed, 0 failed

node --import tsx tests/cch-oracle-harness.test.ts
=> 2 passed, 0 failed

CC_GATEWAY_NATIVE_ORACLE_REAL_CLI=1 CC_GATEWAY_NATIVE_ORACLE_VERSION=2.1.179 CC_GATEWAY_NATIVE_ORACLE_RUNTIME_ROOT=<verified-2.1.179-binary> CC_GATEWAY_NATIVE_ORACLE_OUTPUT=<private-tmp-safe-json> node --import tsx tools/claude-native-oracle-matrix.ts
=> exited 0; safe matrix written

safe sensitive scan over native-oracle-matrix-2.1.179.json
=> PASS
```

