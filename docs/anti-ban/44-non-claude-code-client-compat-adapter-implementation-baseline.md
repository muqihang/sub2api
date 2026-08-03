# 44 Non-Claude-Code Compat Adapter Implementation Baseline

Date: 2026-06-06
Source of truth: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation`
CC Gateway source of truth: `/Users/muqihang/chelingxi_workspace/cc-gateway`

## Checkpoint 0 audit summary

This memo records the implementation boundary for doc 44 before code changes. It is intentionally safe: no raw request body, prompt, token, telemetry, CCH, account UUID, email, or proxy credential is captured here.

## Required semantics extracted from docs 44/45/41/40/39/36/38

- The first release is Anthropic-only: external clients may use `POST /v1/messages` with Anthropic Messages JSON only.
- `POST /v1/messages?beta=true` is also accepted as an internal/beta-shaped Messages route, but external beta/persona headers are untrusted.
- OpenAI-compatible `POST /v1/chat/completions` and `POST /v1/responses` are out of scope for this adapter and must fail closed for the doc-44 compat path.
- Inbound bare `/v1/messages` must normalize to CC Gateway outbound `/v1/messages?beta=true`.
- Compat traffic must be marked as `client_type=claude_code_compat`, `persona_source=server_selected`, and `server_filled_shape=true`; it must never be labeled `claude_code_native` without trusted guard/runtime attestation.
- Server-selected persona must rebuild `User-Agent`, `anthropic-beta`, `x-app`, `x-claude-code-*`, and `x-stainless-*`; client-supplied values are not authoritative.
- Sub2API must not generate CCH or billing material. CC Gateway policy/signing remains responsible for billing placeholder insertion, CCH computation, verifier gates, and post-sign mutation checks.
- The adapter must preserve valid Anthropic capabilities: `system`, `messages`, `tools`, `thinking`, `stream`, `max_tokens`, `context_management`, and `output_config`.
- The implementation must not reduce native Claude Code capability: 1m context, tools, thinking, streaming, Opus/Sonnet models, and `max_tokens=32000` remain allowed when the selected account/model/persona supports them.
- ToolSearch, `tool_reference`, `defer_loading`, and eager/fine-grained tool streaming are not native evidence when supplied by an external client. They may be enabled only when a real server/agent capability backs them; otherwise the adapter must strip or safe-error with audit.
- Synthetic telemetry upload is not part of this phase. Doc 38 remains shadow/safe-intent oriented, with real upload requiring a separate approval/canary phase.
- Formal pool hard gates and observe-only session budgets remain in force; compat must not bypass verifier/fallback/proxy/risk/sensitive-leak gates.

## Current Sub2API baseline

- `backend/internal/server/routes/gateway.go` registers `/v1/messages`, `/v1/messages/count_tokens`, `/v1/responses`, and `/v1/chat/completions` under `/v1`.
- `/v1/messages` and `/v1/messages?beta=true` currently hit the same Gin route; query normalization is mostly handled downstream.
- `GatewayHandler.Messages` in `backend/internal/handler/gateway_handler.go` parses Anthropic Messages bodies, sets Claude Code client context, applies version checks for detected Claude Code clients, and then schedules/forwards through `GatewayService.Forward`.
- `GatewayService.Forward` selects `ccGatewayRouteNativeMessages` and `buildUpstreamRequest` already sends CC Gateway traffic to `/v1/messages?beta=true` when CC Gateway is enabled.
- `backend/internal/service/cc_gateway_adapter.go` is the central CC Gateway route policy selector for `native_messages`, `native_count_tokens`, `chat_completions`, and `responses`.
- Existing Anthropic-group `/v1/chat/completions` and `/v1/responses` paths convert OpenAI-compatible protocols into Anthropic messages through `ForwardAsChatCompletions` and `ForwardAsResponses`; doc 44 requires keeping those out of the new Anthropic-only compat adapter and fail-closed for the compat route.

## Current CC Gateway baseline

- `src/policy.ts` `selectSharedPoolRoute` already forwards only `POST /v1/messages?beta=true` for messages and blocks plain `/v1/messages` at the CC Gateway boundary.
- `src/policy.ts` `canonicalPersonaHeaders` builds server-side Claude Code-shaped headers and validates allowlisted header schema.
- `src/rewriter.ts` rewrites `metadata.user_id`, persona/env text, and strips billing header blocks for messages; legacy raw telemetry rewriting remains explicitly not a production synthetic telemetry strategy.
- `src/proxy.ts` applies route policy, runtime account registration, upstream safety, safe capture summaries, and avoids raw-sensitive audit output by design.
- `src/upstream-safety.ts` blocks real Anthropic/Claude hosts in preflight unless explicit canary/production switches are set; this implementation must not trigger real Anthropic/Claude egress.

## Files likely to change

Sub2API:

- `backend/internal/service/claude_code_compat_adapter.go` and `*_test.go` for protocol/body/shape/audit normalization.
- `backend/internal/service/cc_gateway_adapter.go` and tests for Anthropic-only route eligibility and fail-closed OpenAI-compatible CC Gateway routes.
- `backend/internal/service/gateway_service.go` and `backend/internal/service/gateway_cc_gateway_boundary_test.go` for route normalization/audit propagation.
- `backend/internal/handler/gateway_handler.go`, `gateway_handler_chat_completions.go`, `gateway_handler_responses.go`, and route/handler tests for safe errors and non-entry into the compat signer path.
- `backend/internal/server/routes/gateway.go` and route matrix tests if endpoint-level fail-closed behavior must be enforced before handlers.
- `tools/safe_deliverable_sensitive_scan.py` only if scan coverage needs doc-44 fixture extensions.

CC Gateway:

- `src/policy.ts`, `src/rewriter.ts`, `src/proxy.ts`, `src/persona-registry.ts`, and `src/upstream-safety.ts` only if doc-44 tests expose gaps. Current route policy already requires `/v1/messages?beta=true`.
- Relevant tests under `tests/`, especially `proxy-sub2api.test.ts`, `rewriter.test.ts`, `persona-registry.test.ts`, and `persona-resolver.test.ts`.

## Explicit non-goals for this implementation round

- No OpenAI-compatible protocol conversion for doc-44 compat.
- No real Anthropic/Claude request, no real canary, and no production deployment.
- No synthetic telemetry real upload.
- No Zhumeng Agent native takeover, loopback guard implementation, or full native parity path.
- No forced ToolSearch/deferred-tool/native attestation spoofing.
- No account-pool expansion or new onboarding policy outside the doc-44 adapter surface.

## Initial checkpoint risks

- Existing Claude Code client detection has legacy UA/body-shape fast paths. Compat implementation must avoid treating spoofed external headers as native.
- Existing Anthropic-group OpenAI-compatible routes are useful legacy behavior but conflict with doc-44 scope if they are allowed into the new compat adapter.
- Server-filled Claude Code-like fields can look native unless audit fields are mandatory and tests assert them.
- ToolSearch/custom Base URL deltas can cause false parity claims; capability-backed audit and fixtures are required before enabling advanced tool modes.
- Any raw prompt/body/token/telemetry/CCH/account/proxy leakage in logs, fixtures, or summaries is a P0.
