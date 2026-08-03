# Claude anti-ban local capture acceptance prep

- No real-account capture unless the user explicitly orders it.
- Compare the same local capture server across:
  1. real Claude Code CLI 2.1.145 baseline
  2. sub2api strict passthrough
  3. sub2api non-CC OAuth mimicry
  4. sub2api count_tokens strict / mimicry

## Verify strict passthrough
- body bytes identical
- UA / x-stainless / anthropic-beta / session header unchanged
- no `Accept-Encoding` forwarded
- no body-mutating retry after first 400
- existing billing block / `cch=` preserved verbatim

## Verify mimicry
- no billing attribution block
- `metadata.user_id` forced from account fingerprint
- `X-Claude-Code-Session-Id` equals metadata session_id
- messages beta exact:
  `claude-code-20250219,interleaved-thinking-2025-05-14,context-management-2025-06-27,prompt-caching-scope-2026-01-05,oauth-2025-04-20`
- count_tokens beta exact:
  `claude-code-20250219,interleaved-thinking-2025-05-14,context-management-2025-06-27,oauth-2025-04-20,token-counting-2024-11-01`
- no `Accept-Encoding`
- no `x-stainless-helper-method`

## Current code-level verification already covers
- strict messages/count_tokens body unchanged
- strict messages/count_tokens no `Accept-Encoding`
- strict messages/count_tokens no body-mutating retry
- mimicry messages/count_tokens metadata + session header overwrite
- mimicry messages/count_tokens exact beta sets
- mimicry messages no billing block
- UTF-16 fingerprint golden values
