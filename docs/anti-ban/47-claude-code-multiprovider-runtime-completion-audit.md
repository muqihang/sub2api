# 47 Claude Code Multi-provider Runtime Completion Audit

Date: 2026-06-17
Branch: `codex/claude-code-multiprovider-runtime`
Worktree: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime`
Plan: `docs/anti-ban/47-zhumeng-agent-claude-code-multi-provider-runtime-patch-plan.md`

This audit records the current completion evidence for CP0-CP8. It is intentionally separate from the design plan so release review can check concrete files, tests, and remaining live-credential steps.

## Summary

Local implementation for CP0-CP8 is present in the dedicated worktree branch. Local verifier, fixture, loopback, bridge, guard, routing, transcript-boundary, UX, and strict-live assembly tests pass.

The only remaining evidence that cannot be produced without operator-owned external credentials and live scenario artifacts is the final CP8 external live matrix. The verifier and collector are implemented and fail closed for mock/loopback/forged evidence; the operator must run the commands in [External live matrix steps](#external-live-matrix-steps) with real Claude/OpenAI/DeepSeek credentials and the corresponding live scenario artifact directory.

## Checkpoint evidence

| CP | Evidence | Required tests / fixtures |
| --- | --- | --- |
| CP0 native guard red test and real launch path | `tools/zhumeng-agent/src/zhumeng_agent/adapters/claude_code/guard.py` passes `--native-attestation`; `launcher.py`, CLI, and desktop open path start the loopback guard and point `ANTHROPIC_BASE_URL` at it. | `tools/zhumeng-agent/tests/test_claude_code_guard.py::test_native_guard_forwards_attested_native_markers_without_prompt_leak`; `::test_native_guard_without_native_attestation_flag_fails_closed`; `tools/zhumeng-agent/tests/test_cli.py::test_claude_code_start_real_path_starts_loopback_guard`; `tools/zhumeng-agent/tests/test_claude_code_launcher.py::test_managed_launch_starts_native_guard_then_launches_claude_with_ready_base_url`; `tools/zhumeng-agent/tests/test_desktop_cli.py::test_desktop_open_claude_code_starts_managed_guard`. |
| CP1 managed runtime installer | Manifest, hash lock, active pointer, rollback metadata, no global overwrite, unknown-version fail closed, default `~/.claude` isolation. | `tools/zhumeng-agent/tests/test_claude_code_runtime_installer.py`. |
| CP2 mixed `/model` overlay proof-only | `model_overlay.py` builds proof-only mixed model list and route-hint stubs. Bridge entries are `live_enabled=False`, `formal_pool_eligible=False`, and not `claude_code_native`. | `tools/zhumeng-agent/tests/test_claude_code_model_overlay_cp2.py`, especially `test_cp2_static_patch_points_and_mixed_model_overlay_are_proof_only`, `test_cp2_route_hint_stub_for_bridge_fails_closed_before_cp4`, `test_cp2_print_smoke_uses_stubbed_runner_without_starting_live_process`. |
| CP3 subagent/workflow and transcript boundary contracts | Provider-local fast/background alias resolver, inherit-first subagent policy, `ReplaySafeAnthropicTranscript`, frozen safe summary/tool result envelopes, resume/continue/compact/checkpoint/history replay cleaning. | `tools/zhumeng-agent/tests/test_claude_code_model_overlay_cp3a.py`; `tools/zhumeng-agent/tests/test_claude_code_transcript_boundary_cp3b.py`. |
| CP4 routing trust contract | Signed route hint binds body/model, runtime/overlay/catalog hashes, session, nonce/timestamp; stale/replayed/mismatched/spoofed native fail closed; bridge routes do not receive native attestation. | `tools/zhumeng-agent/tests/test_claude_code_model_overlay_cp4_route_hint.py`; `tools/tests/test_cli_control_plane_guard.py` CP4 tests including route mismatch, spoofed native, unknown/stale/replayed nonce, and native-only attestation. |
| CP5 provider router and bridge skeleton | Guard/backend route split for `claude_code_native` vs `claude_code_bridge_*`; bridge stub emits Anthropic-compatible SSE/tool-use skeleton without upstream or native attestation; audit records route/catalog safely. | `tools/tests/test_cli_control_plane_guard.py::CliControlPlaneGuardTest::test_cp5_bridge_route_hint_returns_internal_skeleton_anthropic_sse_without_upstream_or_native_attestation`; `::test_cp5_bridge_skeleton_tool_use_sse_golden_and_safe_audit_without_upstream_or_native`; backend bridge route tests. |
| CP6 DeepSeek/GPT bridge parity and replay safety | DeepSeek defaults to Anthropic-compatible `/v1/messages` when probe fixtures pass; OpenAI-compatible/Responses fallback seam exists; GPT/OpenAI Responses bridge maps tool streaming/cache usage; foreign thinking/signature cleaning; dynamic background native egress remains 0; Codex Gateway no-regression fixture is hash-bound. | `tools/zhumeng-agent/tests/test_claude_code_provider_probe_cp6.py`; `tools/zhumeng-agent/tests/test_claude_code_transcript_boundary_cp6.py`; `backend/internal/service/claude_code_bridge_live_test.go`; `backend/internal/service/claude_code_bridge_stream_test.go`; `backend/internal/service/testdata/claude_code_bridge/cp6_tool_use_sse_golden.sse`; `tools/zhumeng-agent/tests/fixtures/claude_code_cp6/codex_gateway_no_regression_cp6.json`. |
| CP7 UX / shell integration | `zhumeng-claude` entry point maps to `claude-code start`; status/install/rollback/alias commands avoid global overwrite and destructive delete; desktop open path starts managed guard. | `tools/zhumeng-agent/tests/test_cli.py::test_zhumeng_claude_entrypoint_maps_to_claude_code_start`; `::test_claude_code_runtime_install_status_rollback_and_alias_commands`; `tools/zhumeng-agent/tests/test_claude_code_runtime_installer.py::test_shell_alias_enable_disable_never_aliases_official_claude`; desktop open tests. |
| CP8 live matrix verifier and final review | Local matrix fixture covers Claude native, GPT bridge, DeepSeek bridge, subagent, Claude->DeepSeek->Claude, manual switch, ToolSearch/MCP, Workflow, long context, interruption, cache/account audit, and netwatch bypass. Strict-live verifier rejects loopback/mock/forged/minimal/mismatched/stale/sensitive artifacts and requires provider/model/endpoint/run_id/artifact binding. | `tools/zhumeng-agent/tests/test_claude_code_live_matrix_cp8.py`; fixture `tools/zhumeng-agent/tests/fixtures/claude_code_cp8/live_matrix_pass.json`; workflow artifact `tools/zhumeng-agent/tests/fixtures/claude_code_cp8/artifacts/workflow_background.json`. |

## Local verification already run

All commands below were run serially in the dedicated worktree.

```bash
PYTHONPATH=.:tools/zhumeng-agent/src tools/zhumeng-agent/.venv/bin/python -m pytest \
  tools/zhumeng-agent/tests/test_claude_code_runtime_installer.py \
  tools/zhumeng-agent/tests/test_claude_code_guard.py \
  tools/zhumeng-agent/tests/test_claude_code_launcher.py \
  tools/zhumeng-agent/tests/test_claude_code_model_overlay_cp2.py \
  tools/zhumeng-agent/tests/test_claude_code_model_overlay_cp3a.py \
  tools/zhumeng-agent/tests/test_claude_code_transcript_boundary_cp3b.py \
  tools/zhumeng-agent/tests/test_claude_code_model_overlay_cp4_route_hint.py \
  tools/zhumeng-agent/tests/test_claude_code_provider_probe_cp6.py \
  tools/zhumeng-agent/tests/test_claude_code_transcript_boundary_cp6.py \
  tools/zhumeng-agent/tests/test_claude_code_live_matrix_cp8.py \
  tools/zhumeng-agent/tests/test_cli.py \
  tools/tests/test_cli_control_plane_guard.py \
  -q
# 349 passed, 7 subtests passed

cd backend && go test -p 1 ./internal/pkg/apicompat ./internal/service ./internal/server/routes ./internal/handler
# pass

cd frontend && pnpm vitest run \
  src/api/__tests__/zhumengAgent.spec.ts \
  src/stores/__tests__/codexEntry.spec.ts \
  src/views/plugin/zhumeng-codex/__tests__/CodexConsole.spec.ts
# 26 passed

PYTHONPATH=.:tools/zhumeng-agent/src tools/zhumeng-agent/.venv/bin/python -m pytest \
  tools/zhumeng-agent/tests/test_claude_code_toolsearch_profile.py \
  tools/zhumeng-agent/tests/test_claude_code_shape_check.py \
  tools/zhumeng-agent/tests/test_claude_code_netwatch.py \
  tools/zhumeng-agent/tests/test_desktop_cli.py::test_desktop_open_claude_code_starts_managed_guard \
  tools/zhumeng-agent/tests/test_desktop_cli.py::test_desktop_open_zhumeng_claude_alias_starts_nonblocking_managed_runtime \
  tools/zhumeng-agent/tests/test_claude_code_transcript_boundary_cp3b.py \
  tools/zhumeng-agent/tests/test_claude_code_transcript_boundary_cp6.py \
  -q
# 63 passed

PYTHONPATH=.:tools/zhumeng-agent/src tools/zhumeng-agent/.venv/bin/python -m pytest \
  tools/tests/test_cli_control_plane_guard.py::CliControlPlaneGuardTest::test_cp4_messages_route_decision_requires_route_hint_catalog_even_for_claude_native \
  tools/tests/test_cli_control_plane_guard.py::CliControlPlaneGuardTest::test_cp4_signed_route_hint_binds_model_route_hashes_session_and_nonce \
  tools/tests/test_cli_control_plane_guard.py::CliControlPlaneGuardTest::test_cp4_route_hint_fails_closed_for_model_mismatch_stale_and_replay \
  tools/tests/test_cli_control_plane_guard.py::CliControlPlaneGuardTest::test_cp4_route_hint_fails_closed_when_body_claude_claims_bridge_route \
  tools/tests/test_cli_control_plane_guard.py::CliControlPlaneGuardTest::test_cp4_route_hint_missing_or_spoofed_native_blocks_before_upstream \
  tools/tests/test_cli_control_plane_guard.py::CliControlPlaneGuardTest::test_cp4_native_route_hint_adds_native_attestation_only_for_native \
  tools/tests/test_cli_control_plane_guard.py::CliControlPlaneGuardTest::test_cp5_bridge_route_hint_returns_internal_skeleton_anthropic_sse_without_upstream_or_native_attestation \
  tools/tests/test_cli_control_plane_guard.py::CliControlPlaneGuardTest::test_cp5_bridge_skeleton_tool_use_sse_golden_and_safe_audit_without_upstream_or_native \
  tools/tests/test_cli_control_plane_guard.py::CliControlPlaneGuardTest::test_cp6_deepseek_background_live_bridge_forward_has_zero_native_egress \
  tools/tests/test_cli_control_plane_guard.py::CliControlPlaneGuardTest::test_cp6_deepseek_background_tasks_have_zero_native_egress \
  tools/tests/test_cli_control_plane_guard.py::CliControlPlaneGuardTest::test_cp6_claude_profile_then_deepseek_background_switch_has_zero_native_egress \
  -q
# 11 passed

PYTHONPATH=.:tools/zhumeng-agent/src tools/zhumeng-agent/.venv/bin/python -m pytest \
  tools/zhumeng-agent/tests/test_cli.py::test_claude_code_live_matrix_module_entrypoint_executes_main_for_provider_provenance \
  -q
# 1 passed

cd backend && go test -p 1 ./internal/service -run 'TestCP6DeepSeekAnthropicLivePreservesToolUseInputFieldsNamedThinking|TestCP6DeepSeekAnthropicLiveStripsForeignThinkingAndSignatureSSE|TestCP6BridgeToolUseSSEMatchesGoldenFixture|TestCP6OpenAIBridgeResponsesStreamMapsToolCallUsageCacheAndCleansReasoning' -count=1
# pass

git diff --check
# pass

codegraph index
# pass, `.codegraph/` present
```

## External live matrix steps

These steps require real credentials and real CP8 scenario artifacts. They must be run before claiming `external_live_passed` in production release notes.

1. Pick a fresh run id and evidence directory outside the source worktree:

```bash
export RUN_ID="cp8-live-$(date -u +%Y%m%dT%H%M%SZ)"
export EVIDENCE_ROOT="$HOME/zhumeng-claude-code-cp8-evidence/$RUN_ID"
mkdir -p "$EVIDENCE_ROOT"
```

2. Provide real credentials only in the current shell/session:

```bash
export ANTHROPIC_API_KEY='<real Anthropic/Claude credential for the CP8 Claude native provider probe>'
export OPENAI_API_KEY='<real OpenAI bridge credential>'
export DEEPSEEK_API_KEY='<real DeepSeek bridge credential>'
# Equivalent fallback env names supported by the collector:
#   SUB2API_CLAUDE_CODE_LIVE_ANTHROPIC_API_KEY
#   SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_API_KEY
#   SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY
```

3. Collect provider provenance. This command is covered by the module-entrypoint regression test and must fail closed with a JSON error if any required credential is missing:

```bash
PYTHONPATH=.:tools/zhumeng-agent/src tools/zhumeng-agent/.venv/bin/python -m zhumeng_agent.cli \
  claude-code live-matrix \
  --collect-provider-provenance \
  --run-id "$RUN_ID" \
  --output-root "$EVIDENCE_ROOT"
```

4. Run the live scenarios for the same `RUN_ID` and write only safe scenario artifact JSON into `$EVIDENCE_ROOT/artifacts`. Required scenarios are:

- `claude_native`
- `gpt_bridge`
- `deepseek_bridge`
- `subagent`
- `claude_deepseek_subagent_claude`
- `manual_provider_switch`
- `toolsearch_mcp`
- `workflow`
- `long_context`
- `interruption`
- `cache_account_audit`
- `netwatch_bypass`

The scenario artifacts must use schema `cp8-live-scenario-evidence-v1`, must reference the same `run_id`, must contain provider/model/endpoint/upstream request id bindings, must point to provider provenance artifact refs, and must not contain raw prompt/body/header/token/payload/secret material.

5. Assemble and verify strict-live evidence:

```bash
PYTHONPATH=.:tools/zhumeng-agent/src tools/zhumeng-agent/.venv/bin/python -m zhumeng_agent.cli \
  claude-code live-matrix \
  --assemble-external \
  --evidence "$EVIDENCE_ROOT/matrix.json" \
  --provenance "$EVIDENCE_ROOT/live_provenance.json" \
  --out "$EVIDENCE_ROOT/external-matrix.json"

PYTHONPATH=.:tools/zhumeng-agent/src tools/zhumeng-agent/.venv/bin/python -m zhumeng_agent.cli \
  claude-code live-matrix \
  --evidence "$EVIDENCE_ROOT/external-matrix.json" \
  --strict-live
```

Success criteria: CLI output reports `release_gate=external_live_passed`. Any loopback/mock artifact, missing model binding, stale `run_id`, endpoint drift, artifact hash mismatch, or sensitive inline/raw evidence must fail closed.

## Known limitation before external release

Without the external live evidence above, the branch should be described as **local implementation complete and strict-live verifier ready**, not as **external provider live matrix passed**. This is a credential/artifact availability boundary, not a local code gap.
