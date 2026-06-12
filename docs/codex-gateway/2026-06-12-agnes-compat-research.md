# AGNES Codex Gateway compatibility research - 2026-06-12

## Scope

This note records the AGNES-specific compatibility findings used for the 2026-06-12
Codex Gateway stream fix. The implementation is intentionally provider-scoped so
DeepSeek, Claude/Anthropic, and GPT routing semantics are not changed.

## Official protocol findings

Primary official references:

- https://agnes-ai.com/api/doc/overview?lang=en
- https://agnes-ai.com/api/doc/quick-start?lang=en
- https://agnes-ai.com/api/doc/agnes-20-flash?lang=en

Observed from the official docs:

1. AGNES exposes an OpenAI-compatible API surface.
   - Base URL: `https://apihub.agnes-ai.com/v1`
   - Chat endpoint: `/chat/completions`
   - Auth: `Authorization: Bearer <api key>`
2. `agnes-2.0-flash` is documented for agent workflows, tool calling, coding,
   reasoning, multi-turn conversations, streaming, and image understanding.
3. Message content may be plain text or an array containing `text` and
   `image_url` blocks.
4. Tool calling follows the OpenAI Chat Completions `tools` / `tool_choice`
   structure.
5. Thinking is exposed through extension fields:
   - OpenAI-compatible shape: `chat_template_kwargs.enable_thinking`
   - The docs also mention a `thinking` field for Anthropic-compatible callers,
     but our AGNES provider path is Chat Completions-compatible and therefore
     uses `chat_template_kwargs`.
6. The standard documented response shape includes `choices[].finish_reason`
   and `usage.prompt_tokens`, `usage.completion_tokens`, `usage.total_tokens`.

## Live behavior relevant to Codex Desktop

The failed Computer Use trace `trace_1781253269335405127` showed an AGNES stream
shape that is compatible with OpenAI-style streaming, but not identical to the
normal terminal shape expected by our adapter:

1. AGNES emitted normal text chunks.
2. AGNES then emitted a usage-only chunk with `choices: []` and `usage: {...}`.
3. AGNES then emitted `[DONE]`.
4. No chunk carried `choices[0].finish_reason`.

Before the fix, the gateway treated this as a missing terminal event and emitted
`response.incomplete` with `upstream_stream_closed`, even though the upstream had
actually finished a normal assistant text answer.

This is distinct from tool-call streams. Tool-call streams that include
`finish_reason: "tool_calls"` already complete correctly. Partial tool-call
streams without a terminal finish reason must still remain incomplete so Codex
Desktop does not execute ambiguous or truncated tool arguments.

## Compatibility decision

Add an AGNES-only terminal inference rule:

- If provider is `agnes`, and
- `[DONE]` was observed, and
- a usage-only trailer with `choices: []` was observed, and
- normal assistant text was emitted, and
- no tool call is pending/exposed, and
- no blocked hosted-tool condition exists,
- then infer `finish_reason = "stop"` and emit `response.completed`.

This is deliberately not applied to DeepSeek or other OpenAI-compatible
providers. DeepSeek streams should continue to require an explicit terminal
finish reason unless their provider-specific evidence proves otherwise.

## Current AGNES strategy retained

- Enable official AGNES thinking for ordinary non-tool text/vision tasks via
  `chat_template_kwargs.enable_thinking`.
- Disable official thinking when tools are present. This keeps Computer Use and
  other tool loops stable, because the live traces show AGNES can otherwise spend
  budget on reasoning while Codex Desktop needs prompt, well-formed tool-call
  turns.
- Preserve native `image_url` blocks only for AGNES models that declare image
  input support.
- Keep AGNES prompt-cache accounting marked as unsupported. AGNES currently
  returns token usage but does not expose provider cache-hit fields in the same
  way OpenAI/Anthropic/DeepSeek do.
