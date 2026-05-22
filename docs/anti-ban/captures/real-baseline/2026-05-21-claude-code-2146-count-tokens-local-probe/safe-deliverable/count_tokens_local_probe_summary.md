# Claude Code 2.1.146 count_tokens local probe summary

- Route: `/v1/messages/count_tokens`
- Status: `DEFER`
- Route policy: `block`
- Excluded from first-wave canary: `true`
- Reason: minimal official CLI localhost attempts did not naturally emit `count_tokens`; no extra prompt/tool complexity was added.

## Attempts

### default_attribution_minimal_print_json
- attribution: `default`
- total localhost requests: `4`
- `/v1/messages` observed: `4`
- `/v1/messages/count_tokens` observed: `0`
- retry-like requests: `0`
- first request method/path: `POST /v1/messages?beta=true`
- first request header key order: `Accept, Authorization, Content-Type, User-Agent, X-Claude-Code-Session-Id, X-Stainless-Arch, X-Stainless-Lang, X-Stainless-OS, X-Stainless-Package-Version, X-Stainless-Retry-Count, X-Stainless-Runtime, X-Stainless-Runtime-Version, X-Stainless-Timeout, anthropic-beta, anthropic-dangerous-direct-browser-access, anthropic-version, x-app, Connection, Host, Accept-Encoding, Content-Length`
- first request anthropic-beta: `claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,context-management-2025-06-27,prompt-caching-scope-2026-01-05,advisor-tool-2026-03-01,effort-2025-11-24,extended-cache-ttl-2025-04-11`
- first request Accept-Encoding: `gzip, deflate, br, zstd`
- first request body keys: `context_management, max_tokens, messages, metadata, model, output_config, stream, system, thinking, tools`
- metadata.user_id fields: `account_uuid, device_id, session_id`
- metadata.user_id hash: `sha256:25117482ae0c71a4d12799fa304056c6dcd6121d2b14925e36f1c6d458df1f0c`
- billing block present: `True` / cch present: `True`

### attribution_off_minimal_print_json
- attribution: `CLAUDE_CODE_ATTRIBUTION_HEADER=0`
- total localhost requests: `4`
- `/v1/messages` observed: `4`
- `/v1/messages/count_tokens` observed: `0`
- retry-like requests: `0`
- first request method/path: `POST /v1/messages?beta=true`
- first request header key order: `Accept, Authorization, Content-Type, User-Agent, X-Claude-Code-Session-Id, X-Stainless-Arch, X-Stainless-Lang, X-Stainless-OS, X-Stainless-Package-Version, X-Stainless-Retry-Count, X-Stainless-Runtime, X-Stainless-Runtime-Version, X-Stainless-Timeout, anthropic-beta, anthropic-dangerous-direct-browser-access, anthropic-version, x-app, Connection, Host, Accept-Encoding, Content-Length`
- first request anthropic-beta: `claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,context-management-2025-06-27,prompt-caching-scope-2026-01-05,advisor-tool-2026-03-01,effort-2025-11-24,extended-cache-ttl-2025-04-11`
- first request Accept-Encoding: `gzip, deflate, br, zstd`
- first request body keys: `context_management, max_tokens, messages, metadata, model, output_config, stream, system, thinking, tools`
- metadata.user_id fields: `account_uuid, device_id, session_id`
- metadata.user_id hash: `sha256:066b47d5d57aa9ac816881c9ccc137f89b7e68f71bc0757831e8e5fd10350887`
- billing block present: `False` / cch present: `False`

> Safe deliverable stores only hashes and field/key summaries. No raw Authorization, raw prompt text, raw request bodies, raw CCH, or raw account UUIDs are included.