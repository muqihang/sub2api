# 44 Compat Adapter Implementation Memo

Date: 2026-06-07
Status: implemented through Checkpoint 5; final full validation pending Checkpoint 7
Source of truth: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation`
CC Gateway source of truth: `/Users/muqihang/chelingxi_workspace/cc-gateway`

## Scope implemented

- Anthropic-only ingress for non-Claude-Code clients: `POST /v1/messages` and internal/beta-shaped `POST /v1/messages?beta=true`.
- OpenAI-compatible `/v1/chat/completions`, `/v1/responses`, and OpenAI-shaped bodies on `/v1/messages` fail closed with safe errors.
- Sub2API normalizes inbound `/v1/messages` to CC Gateway `/v1/messages?beta=true` and records `inbound_route` / `cc_gateway_route` audit fields.
- Compat requests are marked `client_type=claude_code_compat`; spoofed Claude Code UA/metadata cannot become native and any spoofed Claude Code version is cleared.
- Server-filled body shape is auditable with `server_filled_shape`, `server_filled_fields`, `persona_source=server_selected`, `compat_fidelity_level`, `tool_search_mode`, `tool_reference_present`, `defer_loading_present`, `eager_input_streaming_present`, and `capability_backed`.
- Valid Anthropic capabilities remain available: `system`, `messages`, ordinary `tools`, `thinking`, `stream`, `max_tokens`, `context_management`, and `output_config`.
- The adapter does not restrict native Claude Code 1m context, tools, thinking, stream, Opus/Sonnet families, or `max_tokens=32000`.
- Native-only ToolSearch/deferred markers from external clients are stripped with audit unless a future real runner makes them capability-backed. Ordinary Anthropic tool JSON Schema property names are preserved.
- Ops error logging and upstream error contexts store safe summaries only for compat requests; raw messages/system/metadata/tool schemas/content are not persisted.
- Shape healthcheck fixtures define denominator fields and L0/L1/L2/L3 classifier behavior.

## Explicitly not implemented in this phase

- No OpenAI-compatible protocol conversion.
- No synthetic telemetry real upload.
- No real Anthropic/Claude canary.
- No production deployment.
- No account-pool expansion.
- No Zhumeng Agent native takeover or loopback guard.
- No fake ToolSearch, fake deferred tools, fake MCP/subagent capability, fake CCH, or fake billing.

## Evidence files

Sub2API:

- `backend/internal/service/claude_code_compat_protocol.go`
- `backend/internal/service/claude_code_compat_shape.go`
- `backend/internal/service/claude_code_compat_shape_healthcheck.go`
- `backend/internal/handler/gateway_handler.go`
- `backend/internal/handler/gateway_helper.go`
- `backend/internal/handler/ops_error_logger.go`
- `backend/internal/service/ops_upstream_context.go`
- `backend/internal/service/gateway_service.go`
- `backend/internal/service/testdata/claude_code_compat/*.json`

CC Gateway:

- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/proxy.ts`
- `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/proxy-sub2api.test.ts`

## Review notes

Each completed checkpoint was reviewed by a read-only review agent and committed separately. Sensitive scan findings were zero after Checkpoints 0 through 5. Final Checkpoint 7 must rerun the complete command set before declaring the goal complete.
