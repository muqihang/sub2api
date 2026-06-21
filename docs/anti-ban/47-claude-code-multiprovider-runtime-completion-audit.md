# 47 Claude Code Multi-provider Runtime Completion Audit

Date: 2026-06-18
Branch: `codex/claude-code-multiprovider-runtime`
Worktree: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime`
Plan: `docs/anti-ban/47-zhumeng-agent-claude-code-multi-provider-runtime-patch-plan.md`

This audit records the current completion evidence for CP0-CP8. It is intentionally separate from the design plan so release review can check concrete files, tests, and remaining live-credential steps.

## Summary

Local implementation for CP0-CP8 is present in the dedicated worktree branch. Local verifier, fixture, loopback, bridge, guard, routing, transcript-boundary, UX, and strict-live assembly tests pass.

The only remaining evidence that cannot be produced without an operator-owned live Sub2API gateway and live scenario artifacts is the final CP8 external live matrix. The release path is Claude Code Runtime -> Sub2API Gateway (for example `http://127.0.0.1:3012`) -> provider routing; direct official provider credential collection is retained only as a lab/fallback collector, not as the primary product acceptance path. The verifier and Sub2API collector fail closed for mock-only loopback fixtures or forged evidence; a loopback Sub2API gateway origin such as `http://127.0.0.1:3012` is allowed only when it forwards to real upstream provider routing. The operator must run the commands in [External live matrix steps](#external-live-matrix-steps) against a real Sub2API gateway and the corresponding live scenario artifact directory.


## 2026-06-18 pre-L8 readiness update

Latest worktree commits before operator L8 live:

- `9cebbad6b feat: plan disabled claude code bridge placeholders` adds `tools/claude_code_runtime_canary_config.py` and `tools/tests/test_claude_code_runtime_canary_config.py`. The helper is dry-run only, emits disabled Claude Code bridge placeholder group plans for OpenAI, DeepSeek, AGNES, Anthropic-compatible, GLM, and Kimi, redacts target credentials/path/query, refuses `--apply`, does not bind upstream accounts, does not add native group 8 membership, and keeps `models_list_config.enabled=false`.
- `008b86d10 test: align guard integration with native managed auth` updates the local guard integration harness to the hardened native formal-pool boundary: native Claude formal-pool messages use the separate native managed access token, and missing native managed token fails closed before upstream.

Current local runtime configuration was checked with redacted field-presence output only:

- `gateway_base_url`: `http://127.0.0.1:3012`
- `server_base_url`: `http://127.0.0.1:3012`
- `claude_code_sub2api_api_key`: present, redacted
- `claude_code_native_access_token`: present, redacted
- `claude_code_native_managed_session_id`: present, redacted
- `claude_code_native_device_id`: present, redacted
- `claude_code_native_attestation_secret`: present, redacted
- `claude_code_route_hint_secret`: present, redacted

Runtime status before live: `zhumeng_agent claude-code status` reports Claude Code runtime `2.1.177`, `status=enabled`, and integrity `pass`. Existing Docker services were observed healthy without restarting `3012`: `3012` app healthy, `3017` canary app healthy, postgres/redis healthy.

Operator start command for L8 live from this worktree:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/tools/zhumeng-agent
uv run zhumeng-claude start
```

Optional project-specific start:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/tools/zhumeng-agent
uv run zhumeng-claude start --project-cwd /path/to/project
```

This start path uses the managed state above and points the Claude Code Runtime at the local Sub2API gateway (`3012`) through the loopback guard. It does not ask the operator to paste official Anthropic/OpenAI/DeepSeek provider keys directly into Claude Code Runtime.

Non-message startup smoke was run from this worktree with `uv run zhumeng-claude start -- --version`. It exited successfully with Claude Code `2.1.177`, `returncode=0`, `guard.attested=true`, `guard.route_hint_contract=true`, both `ANTHROPIC_BASE_URL` and `CLAUDE_CODE_API_BASE_URL` pointed at the ephemeral loopback guard, and `runtime.bridge_live_models=[]`. This smoke verifies the managed launcher/guard/base-url wiring without sending a live `/v1/messages` prompt to the formal pool.

## Checkpoint evidence

| CP | Evidence | Required tests / fixtures |
| --- | --- | --- |
| CP0 native guard red test and real launch path | `tools/zhumeng-agent/src/zhumeng_agent/adapters/claude_code/guard.py` passes `--native-attestation`; `launcher.py`, CLI, and desktop open path start the loopback guard and point `ANTHROPIC_BASE_URL` at it. | `tools/zhumeng-agent/tests/test_claude_code_guard.py::test_native_guard_forwards_attested_native_markers_without_prompt_leak`; `::test_native_guard_without_native_attestation_flag_fails_closed`; `tools/zhumeng-agent/tests/test_cli.py::test_claude_code_start_real_path_starts_loopback_guard`; `tools/zhumeng-agent/tests/test_claude_code_launcher.py::test_managed_launch_starts_native_guard_then_launches_claude_with_ready_base_url`; `tools/zhumeng-agent/tests/test_desktop_cli.py::test_desktop_open_claude_code_starts_managed_guard`. |
| CP1 managed runtime installer | Manifest, hash lock, active pointer, rollback metadata, no global overwrite, unknown-version fail closed, default `~/.claude` isolation. | `tools/zhumeng-agent/tests/test_claude_code_runtime_installer.py`. |
| CP2 mixed `/model` overlay proof-only | `model_overlay.py` builds proof-only mixed model list and route-hint stubs. Bridge entries are `live_enabled=False`, `formal_pool_eligible=False`, and not `claude_code_native`. | `tools/zhumeng-agent/tests/test_claude_code_model_overlay_cp2.py`, especially `test_cp2_static_patch_points_and_mixed_model_overlay_are_proof_only`, `test_cp2_route_hint_stub_for_bridge_fails_closed_before_cp4`, `test_cp2_print_smoke_uses_stubbed_runner_without_starting_live_process`. |
| CP3 subagent/workflow and transcript boundary contracts | Provider-local fast/background alias resolver, inherit-first subagent policy, `ReplaySafeAnthropicTranscript`, frozen safe summary/tool result envelopes, resume/continue/compact/checkpoint/history replay cleaning. | `tools/zhumeng-agent/tests/test_claude_code_model_overlay_cp3a.py`; `tools/zhumeng-agent/tests/test_claude_code_transcript_boundary_cp3b.py`. |
| CP4 routing trust contract | Signed route hint binds body/model, runtime/overlay/catalog hashes, session, nonce/timestamp; stale/replayed/mismatched/spoofed native fail closed; bridge routes do not receive native attestation. | `tools/zhumeng-agent/tests/test_claude_code_model_overlay_cp4_route_hint.py`; `tools/tests/test_cli_control_plane_guard.py` CP4 tests including route mismatch, spoofed native, unknown/stale/replayed nonce, and native-only attestation. |
| CP5 provider router and bridge skeleton | Guard/backend route split for `claude_code_native` vs `claude_code_bridge_*`; native formal-pool messages require the separate native managed access token; bridge stub emits Anthropic-compatible SSE/tool-use skeleton without upstream or native attestation; audit records route/catalog safely. | `tools/tests/test_cli_control_plane_guard.py::CliControlPlaneGuardTest::test_cp5_bridge_route_hint_returns_internal_skeleton_anthropic_sse_without_upstream_or_native_attestation`; `::test_cp5_bridge_skeleton_tool_use_sse_golden_and_safe_audit_without_upstream_or_native`; `tools/tests/test_cli_control_plane_guard_integration.py::CliControlPlaneGuardIntegrationTest::test_native_messages_fail_closed_without_native_managed_access_token`; backend bridge route tests. |
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


Additional pre-L8 checks run on 2026-06-18 after the final hardening commits:

```bash
python3 -m unittest tools.tests.test_cli_control_plane_guard_integration -v
# 30 tests OK

python3 -m unittest \
  tools.tests.test_claude_code_runtime_canary_config \
  tools.tests.test_cli_control_plane_policy \
  tools.tests.test_cli_control_plane_guard \
  tools.tests.test_cli_control_plane_network_safety \
  -v
# 74 tests OK

python3 tools/claude_code_runtime_canary_config.py --dry-run --target http://127.0.0.1:3017
# disabled bridge placeholders only; writes_enabled=false


cd tools/zhumeng-agent
uv run zhumeng-claude start -- --version
# Claude Code 2.1.177; guard attested=true; route_hint_contract=true; base URLs point to loopback guard; bridge_live_models=[]

cd backend
go test ./internal/service -run 'AnthropicAPIKeyPassthrough|CCGatewayControlPlane|CCGatewayAnthropicAPIKeyPassthrough|ClaudeCodeNativeAttestationAcceptsCountTokensBetaRoute' -count=1
go test ./internal/handler -run 'NativeCountTokensProbe|CountTokens|Gateway' -count=1
go test ./internal/server/routes -run 'ClaudeCodeNativeRouteMatrix|OpenAI' -count=1
go test ./internal/server/middleware -run 'APIKeyAuthAllowsExclusiveGroupWhenUserStillAllowed|ManagedDeviceOrAPIKeyAuth' -count=1
# all pass

git diff --check
# pass

codegraph sync /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime
# pass; .codegraph/ present
```


## 2026-06-21 CP26 L8 repair rollout update

Latest repair checkpoint after the L8 canary problem triage:

- Commit `36115a561` (`fix: harden claude code runtime effort ui and cache audit`) is applied on branch `codex/claude-code-multiprovider-runtime`.
- Managed Claude Code Runtime `2.1.177` was patched only inside the Zhumeng-managed runtime copy. Official/global `claude` remains untouched. Runtime status reports integrity `pass`; non-interactive `claude-code start -- --version` returns `2.1.177 (Claude Code)`.
- 3017 canary was hot-switched only on the `sub2api-canary-app-main-f3a9f235d-cc-runtime-current-3017` container. Port `3012` was not restarted or reconfigured. 3017 health currently returns `{"status":"ok"}`.
- The active 3017 backend binary is the CP26 build at `artifacts/bin/sub2api-cc-runtime-36115a561-20260621200915-linux-arm64`, mounted read-only at `/app/sub2api`.
- The CP26 3017 env readiness file is `artifacts/claude-code-runtime/3017-claude-code-runtime-cp26.env`. Secret-free readiness says `ready=true`; DeepSeek live models all prefer `anthropic_messages`, the OpenAI fallback gate is present and false, and the cache-audit HMAC key is present with id `claude-code-cache-audit-v1`.
- CP26 rollout evidence was written under `artifacts/claude-code-canary/cp26-rollout-20260621T202737Z/` with split preflight/UI/cache/live-matrix status files. The evidence intentionally records only health, hashes, route/cache policy enums, effort metadata, and readiness summaries; it does not contain raw prompts, raw bodies, raw responses, API keys, cookies, or Authorization values.

CP26 behavior now expected for the next L8 manual test:

- `/model` exact effort policy is enforced through the managed runtime patch plus launcher metadata: GPT bridge models expose `low/medium/high/xhigh` and not `max`; DeepSeek bridge models expose `high/max` and not `medium`; AGNES and Kimi expose no effort selector; GLM remains catalog-visible with `high/max` but is not part of the default L8 live provider scope. Backend bridge validation also rejects unsupported `output_config.effort` if a client bypasses UI.
- DeepSeek bridge remains Anthropic-compatible-first (`/v1/messages`). `chat/completions` remains an explicit fallback-only path and must not hijack Anthropic-compatible decisions. DeepSeek cache evidence is provider-truthful: `cache_control` is not treated as the cache mechanism; safe audit uses DeepSeek `prompt_cache_hit_tokens` / `prompt_cache_miss_tokens`, stable-prefix HMAC, token bucket, route/protocol/path enums, and no raw body.
- The production log line for safe cache truth is `gateway.claude_code_bridge_cache_audit`. A real DeepSeek request is still needed to observe provider cache hit/miss counters in live logs.

Current release statement after CP26: **local implementation complete for the repaired L8 scope, 3017 canary rolled out and ready for operator L8 live scenarios**. Do not claim `external_live_passed` until the external CP8 live matrix artifacts are collected and verified as described below.


## 2026-06-21 CP28 runtime-hash binding fix

Final review of CP26 found a blocking drift: the active patched managed Claude Code Runtime hash was `sha256:aa1e920563a2d32a6b96f7f2700a2c8f69d09bb4f2b1118974dd08a1484919b4`, but the first CP26 3017 env/catalog still advertised the pre-patch runtime hash. This would make route-hint/native attestation binding fail for real L8 traffic.

The fix added a readiness gate that compares the expected active runtime hash, `SUB2API_CLAUDE_CODE_NATIVE_RUNTIME_HASHES`, and the provider catalog top-level `runtime_hash`. A stale env now fails `--verify-env --runtime-hash ...` with `Claude Code runtime hash drift from active managed runtime`; a regenerated candidate env passes only when all runtime-hash fields match. The review follow-up made this binding strict: the native runtime hash allowlist must contain only the active managed runtime hash, and malformed supplied `--runtime-hash` values fail closed instead of being ignored.

3017 was hot-switched again with only the 3017 canary touched. The active env is now `artifacts/claude-code-runtime/3017-claude-code-runtime-cp28.env`, and secret-free readiness reports:

- `ready=true`
- `runtime_hash_binding.env_matches_catalog_runtime_hash=true`
- `runtime_hash_binding.env_matches_requested_runtime_hash=true`
- `runtime_hash_binding.env_native_runtime_hashes_exact=true`
- DeepSeek live selected protocol remains `anthropic_messages`
- DeepSeek OpenAI fallback gate remains present and false

CP28 evidence was written under `artifacts/claude-code-canary/cp28-hashfix-20260621T204535Z/`. Current release statement after CP28: **3017 canary hash binding repaired and ready for operator L8 live scenarios**. Still do not claim `external_live_passed` until the external CP8 live matrix artifacts are collected and verified.

## External live matrix steps

These steps require a real Sub2API gateway/session (for example the local gateway on `http://127.0.0.1:3012`) and real CP8 scenario artifacts. They must be run before claiming `external_live_passed` in production release notes. The Claude/GPT/DeepSeek provider keys remain inside Sub2API/gateway provider routing; the Claude Code Runtime path must not ask the operator to paste official OpenAI/DeepSeek/Anthropic keys directly.

1. Pick a fresh run id and evidence directory outside the source worktree:

```bash
export RUN_ID="cp8-live-$(date -u +%Y%m%dT%H%M%SZ)"
export EVIDENCE_ROOT="$HOME/zhumeng-claude-code-cp8-evidence/$RUN_ID"
mkdir -p "$EVIDENCE_ROOT"
```

2. Ensure the managed Claude Code Runtime is configured and the Sub2API gateway is reachable. The CLI will prefer values from managed setup state (`gateway_base_url`, `access_token`, server-provisioned `claude_code_native_attestation_secret`, server-provisioned `claude_code_route_hint_secret`, active runtime `runtime_hash`/`overlay_hash`, and the route catalog content hash derived from the active runtime route catalog). Use env/flags only to override or to run from a separate shell:

```bash
export SUB2API_CP8_LIVE_BASE_URL="http://127.0.0.1:3012"
export SUB2API_CP8_LIVE_GATEWAY_TOKEN="<Sub2API gateway/session token, not an official provider API key>"
export SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET="<server-provisioned managed setup secret>"
export SUB2API_CLAUDE_CODE_ROUTE_HINT_SECRET="<server-provisioned route hint secret>"
export ZHUMENG_CLAUDE_RUNTIME_HASH="sha256:<active managed runtime hash>"
export ZHUMENG_CLAUDE_OVERLAY_HASH="sha256:<active managed overlay hash>"
# Optional override only; by default the CLI derives the route catalog hash from the active runtime.
export ZHUMENG_CLAUDE_CATALOG_HASH="sha256:<active route/catalog hash>"
export ZHUMENG_CLAUDE_CATALOG_VERSION="<active route/catalog version>"
```

3. Collect Sub2API gateway-backed provider provenance. All providers enter through the Claude Code Runtime `/v1/messages` shape at the Sub2API gateway; GPT/OpenAI and DeepSeek are selected by signed bridge route hints and must not connect to the Claude formal-pool native path:

```bash
PYTHONPATH=.:tools/zhumeng-agent/src tools/zhumeng-agent/.venv/bin/python -m zhumeng_agent.cli \
  claude-code live-matrix \
  --collect-sub2api-provenance \
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

Success criteria: CLI output reports `release_gate=external_live_passed`. Any loopback/mock artifact, missing model binding, stale `run_id`, endpoint drift, route/client-type mismatch, artifact hash mismatch, direct official provider endpoint in Sub2API mode, or sensitive inline/raw evidence must fail closed.

### Lab/fallback direct provider collector

`claude-code live-matrix --collect-provider-provenance` still exists for isolated official-provider lab checks with `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, and `DEEPSEEK_API_KEY`, but it is not the primary 47 号逐梦版 acceptance path and should not be used for product CP8 sign-off. The product CP8 path above must prove Claude/GPT/DeepSeek live behavior through the Sub2API gateway. Future user-owned provider URLs/API keys belong behind the 逐梦 Agent/Sub2API ProviderRegistry or local/hybrid gateway mode, not as direct Claude Code Runtime egress.

## Known limitation before external release

Without the external live evidence above, the branch should be described as **local implementation complete and strict-live verifier ready**, not as **external provider live matrix passed**. This is a credential/artifact availability boundary, not a local code gap.
