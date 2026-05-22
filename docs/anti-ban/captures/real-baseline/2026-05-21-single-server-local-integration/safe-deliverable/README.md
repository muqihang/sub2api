# Single-server local integration summary

- Executed at: `2026-05-21T23:01:18-07:00`
- Environment: Ubuntu 24.04.4 LTS on x86_64; server-local Node.js/npm/Go/Python available; CC Gateway built and tested; Sub2API joint local capture test passed on the server.
- Scope: localhost-only integration for first-wave messages-only sign-primary/strip-controlled prep; no real Anthropic upstream, no login, no MITM.
- No real upstream: `True`
- No raw secrets: `True`
- No native fallback: `True`
- Sub2API not final-mutating on CC Gateway routes: `True`
- CC Gateway final-output owner: `True`
- Negative cases fail closed: `True`

## Selected scenarios
### oauth_native_messages_strip
- route: `/v1/messages?beta=true`
- decision: `forward_strip`
- selected account id hash: `sha256:c3ea99f86b2f8a74`
- egress bucket: `bucket-a`
- policy version: `2.1.146`
- response status: `200`
- request count: `1`
- no real upstream: `True`
- no native fallback: `True`
- sub2api->gateway route: `/v1/messages?beta=true`
- sub2api->gateway header order: `Host, User-Agent, Content-Length, X-Claude-Code-Session-Id, anthropic-beta, anthropic-version, authorization, content-type, x-cc-account-id, x-cc-account-uuid, x-cc-egress-bucket, x-cc-gateway-token, x-cc-policy-version, x-cc-provider, x-cc-token-type`
- sub2api->gateway header summary: `{"Content-Length": "", "Host": "loopback", "User-Agent": "claude-cli/99.9.9 (external, sdk-cli)", "X-Claude-Code-Session-Id": "sha256:89d07480e9d09240", "anthropic-beta": "client-beta,oauth-2025-04-20", "anthropic-version": "2023-06-01", "authorization": "sha256:13f7f7e725757b6e", "content-type": "application/json", "x-cc-account-id": "sha256:c3ea99f86b2f8a74", "x-cc-account-uuid": "sha256:22e86c827a3582dd", "x-cc-egress-bucket": "bucket-a", "x-cc-gateway-token": "sha256:f15ae5b5899f8327", "x-cc-policy-version": "2.1.146", "x-cc-provider": "anthropic", "x-cc-token-type": "sha256:6e306c515177ca5a"}`
- sub2api->gateway body sha256: `ae83f8d4dce205590e4684c8e486bfa822a8111e43a86f7f119587dc956aac16`
- sub2api->gateway body keys: `messages, metadata, model, stream, system`
- sub2api->gateway billing/cch: `True/True`
- sub2api->gateway metadata fields: `account_uuid, device_id, session_id`
- sub2api->gateway metadata hashes: `{"account_uuid": "sha256:f82e9dfb3d284953", "device_id": "sha256:6cdb26c9367fbb72", "session_id": "sha256:89d07480e9d09240"}`
- sub2api->gateway session header hash: `sha256:89d07480e9d09240`
- gateway->upstream route: `/v1/messages?beta=true`
- gateway->upstream header order: `authorization, Accept, User-Agent, X-Stainless-Arch, X-Stainless-Lang, X-Stainless-OS, X-Stainless-Package-Version, X-Stainless-Retry-Count, X-Stainless-Runtime, X-Stainless-Runtime-Version, X-Stainless-Timeout, anthropic-dangerous-direct-browser-access, anthropic-version, x-app, Accept-Encoding, anthropic-beta, X-Claude-Code-Session-Id, content-type, host, content-length, Connection`
- gateway->upstream header summary: `{"Accept": "application/json", "Accept-Encoding": "gzip, deflate, br, zstd", "Connection": "close", "User-Agent": "claude-cli/2.1.146 (external, sdk-cli)", "X-Claude-Code-Session-Id": "sha256:89d07480e9d09240", "X-Stainless-Arch": "arm64", "X-Stainless-Lang": "js", "X-Stainless-OS": "MacOS", "X-Stainless-Package-Version": "0.94.0", "X-Stainless-Retry-Count": "0", "X-Stainless-Runtime": "node", "X-Stainless-Runtime-Version": "v24.3.0", "X-Stainless-Timeout": "600", "anthropic-beta": "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,context-management-2025-06-27,prompt-caching-scope-2026-01-05,advisor-tool-2026-03-01,effort-2025-11-24,extended-cache-ttl-2025-04-11", "anthropic-dangerous-direct-browser-access": "true", "anthropic-version": "2023-06-01", "authorization": "sha256:13f7f7e725757b6e", "content-length": "339", "content-type": "application/json", "host": "loopback", "x-app": "cli"}`
- gateway->upstream body sha256: `ba1e2cb2bd08ad50abce17826c78025aa0808f952b7686502017c060b58d36b9`
- gateway->upstream body keys: `messages, metadata, model, stream, system`
- gateway->upstream billing/cch: `False/False`
- gateway->upstream metadata fields: `account_uuid, device_id, session_id`
- gateway->upstream metadata hashes: `{"account_uuid": "sha256:e010d1d5d17dee3c", "device_id": "sha256:a0fab1377f49a759", "session_id": "sha256:89d07480e9d09240"}`
- gateway->upstream session header hash: `sha256:89d07480e9d09240`
- note: `sub2api->gateway body unchanged while gateway->upstream body stripped billing markers`
- note: `gateway final persona is canonical Claude Code 2.1.146`

### oauth_native_messages_sign_primary
- route: `/v1/messages?beta=true`
- decision: `forward_sign_primary`
- selected account id hash: `sha256:c3ea99f86b2f8a74`
- egress bucket: `bucket-a`
- policy version: `2.1.146`
- response status: `200`
- request count: `1`
- no real upstream: `True`
- no native fallback: `True`
- sub2api->gateway route: `/v1/messages?beta=true`
- sub2api->gateway header order: `Host, User-Agent, Content-Length, X-Claude-Code-Session-Id, anthropic-beta, anthropic-version, authorization, content-type, x-cc-account-id, x-cc-account-uuid, x-cc-egress-bucket, x-cc-gateway-token, x-cc-policy-version, x-cc-provider, x-cc-token-type`
- sub2api->gateway header summary: `{"Content-Length": "", "Host": "loopback", "User-Agent": "claude-cli/99.9.9 (external, sdk-cli)", "X-Claude-Code-Session-Id": "sha256:89d07480e9d09240", "anthropic-beta": "client-beta,oauth-2025-04-20", "anthropic-version": "2023-06-01", "authorization": "sha256:13f7f7e725757b6e", "content-type": "application/json", "x-cc-account-id": "sha256:c3ea99f86b2f8a74", "x-cc-account-uuid": "sha256:22e86c827a3582dd", "x-cc-egress-bucket": "bucket-a", "x-cc-gateway-token": "sha256:f15ae5b5899f8327", "x-cc-policy-version": "2.1.146", "x-cc-provider": "anthropic", "x-cc-token-type": "sha256:6e306c515177ca5a"}`
- sub2api->gateway body sha256: `4e1374b2d3b973e0030171e27a74a2b730855da91bf7cfa98b1a58b721125d01`
- sub2api->gateway body keys: `messages, metadata, model, stream`
- sub2api->gateway billing/cch: `False/False`
- sub2api->gateway metadata fields: `account_uuid, device_id, session_id`
- sub2api->gateway metadata hashes: `{"account_uuid": "sha256:f82e9dfb3d284953", "device_id": "sha256:6cdb26c9367fbb72", "session_id": "sha256:89d07480e9d09240"}`
- sub2api->gateway session header hash: `sha256:89d07480e9d09240`
- gateway->upstream route: `/v1/messages?beta=true`
- gateway->upstream header order: `authorization, Accept, User-Agent, X-Stainless-Arch, X-Stainless-Lang, X-Stainless-OS, X-Stainless-Package-Version, X-Stainless-Retry-Count, X-Stainless-Runtime, X-Stainless-Runtime-Version, X-Stainless-Timeout, anthropic-dangerous-direct-browser-access, anthropic-version, x-app, Accept-Encoding, anthropic-beta, X-Claude-Code-Session-Id, content-type, host, content-length, Connection`
- gateway->upstream header summary: `{"Accept": "application/json", "Accept-Encoding": "gzip, deflate, br, zstd", "Connection": "close", "User-Agent": "claude-cli/2.1.146 (external, sdk-cli)", "X-Claude-Code-Session-Id": "sha256:89d07480e9d09240", "X-Stainless-Arch": "arm64", "X-Stainless-Lang": "js", "X-Stainless-OS": "MacOS", "X-Stainless-Package-Version": "0.94.0", "X-Stainless-Retry-Count": "0", "X-Stainless-Runtime": "node", "X-Stainless-Runtime-Version": "v24.3.0", "X-Stainless-Timeout": "600", "anthropic-beta": "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,context-management-2025-06-27,prompt-caching-scope-2026-01-05,advisor-tool-2026-03-01,effort-2025-11-24,extended-cache-ttl-2025-04-11", "anthropic-dangerous-direct-browser-access": "true", "anthropic-version": "2023-06-01", "authorization": "sha256:13f7f7e725757b6e", "content-length": "455", "content-type": "application/json", "host": "loopback", "x-app": "cli"}`
- gateway->upstream body sha256: `0d6fd64bcd7686a7aa49c92ec5e2a9d5c6fbb0e48509dd78cf9e308cad2d2ff2`
- gateway->upstream body keys: `messages, metadata, model, stream, system`
- gateway->upstream billing/cch: `True/True`
- gateway->upstream metadata fields: `account_uuid, device_id, session_id`
- gateway->upstream metadata hashes: `{"account_uuid": "sha256:e010d1d5d17dee3c", "device_id": "sha256:add5b41fc5a881fc", "session_id": "sha256:89d07480e9d09240"}`
- gateway->upstream session header hash: `sha256:89d07480e9d09240`
- note: `sub2api->gateway body is pre-final with no billing/CCH material`
- note: `cc gateway generated billing block, cc_version suffix, CCH, canonical persona, and post-sign verifier passed before localhost upstream capture`

### oauth_native_count_tokens_deferred
- route: `/v1/messages/count_tokens?beta=true`
- decision: `defer_block`
- selected account id hash: `sha256:c3ea99f86b2f8a74`
- egress bucket: `bucket-a`
- policy version: `2.1.146`
- response status: `403`
- control-plane: `cc_gateway_control_plane/count_tokens_deferred`
- request count: `1`
- no real upstream: `True`
- no native fallback: `True`
- sub2api->gateway route: `/v1/messages/count_tokens?beta=true`
- sub2api->gateway header order: `Host, User-Agent, Content-Length, X-Claude-Code-Session-Id, anthropic-beta, anthropic-version, authorization, content-type, x-cc-account-id, x-cc-account-uuid, x-cc-egress-bucket, x-cc-gateway-token, x-cc-policy-version, x-cc-provider, x-cc-token-type`
- sub2api->gateway header summary: `{"Content-Length": "", "Host": "loopback", "User-Agent": "claude-cli/99.9.9 (external, sdk-cli)", "X-Claude-Code-Session-Id": "sha256:89d07480e9d09240", "anthropic-beta": "client-beta,oauth-2025-04-20,token-counting-2024-11-01", "anthropic-version": "2023-06-01", "authorization": "sha256:13f7f7e725757b6e", "content-type": "application/json", "x-cc-account-id": "sha256:c3ea99f86b2f8a74", "x-cc-account-uuid": "sha256:22e86c827a3582dd", "x-cc-egress-bucket": "bucket-a", "x-cc-gateway-token": "sha256:f15ae5b5899f8327", "x-cc-policy-version": "2.1.146", "x-cc-provider": "anthropic", "x-cc-token-type": "sha256:6e306c515177ca5a"}`
- sub2api->gateway body sha256: `5be832de94a6089d6a5c13615778771ca19d69105170e843985ef18b0b9ef6fe`
- sub2api->gateway body keys: `messages, model`
- sub2api->gateway billing/cch: `False/False`
- note: `route is explicitly deferred in first wave; no upstream request observed`

### apikey_native_messages_strip
- route: `/v1/messages?beta=true`
- decision: `forward_strip`
- selected account id hash: `sha256:43974ed74066b207`
- egress bucket: `bucket-a`
- policy version: `2.1.146`
- response status: `200`
- request count: `1`
- no real upstream: `True`
- no native fallback: `True`
- sub2api->gateway route: `/v1/messages?beta=true`
- sub2api->gateway header order: `Host, User-Agent, Content-Length, X-Claude-Code-Session-Id, anthropic-beta, anthropic-version, content-type, x-api-key, x-cc-account-id, x-cc-egress-bucket, x-cc-gateway-token, x-cc-policy-version, x-cc-provider, x-cc-token-type`
- sub2api->gateway header summary: `{"Content-Length": "", "Host": "loopback", "User-Agent": "claude-cli/99.9.9 (external, sdk-cli)", "X-Claude-Code-Session-Id": "sha256:89d07480e9d09240", "anthropic-beta": "client-beta", "anthropic-version": "2023-06-01", "content-type": "application/json", "x-api-key": "sha256:a7e3a1ddda2ce652", "x-cc-account-id": "sha256:43974ed74066b207", "x-cc-egress-bucket": "bucket-a", "x-cc-gateway-token": "sha256:f15ae5b5899f8327", "x-cc-policy-version": "2.1.146", "x-cc-provider": "anthropic", "x-cc-token-type": "sha256:6c793695171e793d"}`
- sub2api->gateway body sha256: `ae83f8d4dce205590e4684c8e486bfa822a8111e43a86f7f119587dc956aac16`
- sub2api->gateway body keys: `messages, metadata, model, stream, system`
- sub2api->gateway billing/cch: `True/True`
- sub2api->gateway metadata fields: `account_uuid, device_id, session_id`
- sub2api->gateway metadata hashes: `{"account_uuid": "sha256:f82e9dfb3d284953", "device_id": "sha256:6cdb26c9367fbb72", "session_id": "sha256:89d07480e9d09240"}`
- sub2api->gateway session header hash: `sha256:89d07480e9d09240`
- gateway->upstream route: `/v1/messages?beta=true`
- gateway->upstream header order: `x-api-key, Accept, User-Agent, X-Stainless-Arch, X-Stainless-Lang, X-Stainless-OS, X-Stainless-Package-Version, X-Stainless-Retry-Count, X-Stainless-Runtime, X-Stainless-Runtime-Version, X-Stainless-Timeout, anthropic-dangerous-direct-browser-access, anthropic-version, x-app, Accept-Encoding, anthropic-beta, X-Claude-Code-Session-Id, content-type, host, content-length, Connection`
- gateway->upstream header summary: `{"Accept": "application/json", "Accept-Encoding": "gzip, deflate, br, zstd", "Connection": "close", "User-Agent": "claude-cli/2.1.146 (external, sdk-cli)", "X-Claude-Code-Session-Id": "sha256:89d07480e9d09240", "X-Stainless-Arch": "arm64", "X-Stainless-Lang": "js", "X-Stainless-OS": "MacOS", "X-Stainless-Package-Version": "0.94.0", "X-Stainless-Retry-Count": "0", "X-Stainless-Runtime": "node", "X-Stainless-Runtime-Version": "v24.3.0", "X-Stainless-Timeout": "600", "anthropic-beta": "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,context-management-2025-06-27,prompt-caching-scope-2026-01-05,advisor-tool-2026-03-01,effort-2025-11-24,extended-cache-ttl-2025-04-11", "anthropic-dangerous-direct-browser-access": "true", "anthropic-version": "2023-06-01", "content-length": "339", "content-type": "application/json", "host": "loopback", "x-api-key": "sha256:a7e3a1ddda2ce652", "x-app": "cli"}`
- gateway->upstream body sha256: `2abd863d0906a52657a53f9641e5ef03012cc0f4d73f2ee4aa92c9bc5b5e0693`
- gateway->upstream body keys: `messages, metadata, model, stream, system`
- gateway->upstream billing/cch: `False/False`
- gateway->upstream metadata fields: `account_uuid, device_id, session_id`
- gateway->upstream metadata hashes: `{"account_uuid": "sha256:51e565071788eb67", "device_id": "sha256:52b6419d27bd7f54", "session_id": "sha256:89d07480e9d09240"}`
- gateway->upstream session header hash: `sha256:89d07480e9d09240`
- note: `anthropic api-key passthrough is included for /v1/messages in first wave`
- note: `gateway strips billing markers before localhost upstream capture`

### apikey_native_count_tokens_deferred
- route: `/v1/messages/count_tokens?beta=true`
- decision: `defer_block`
- selected account id hash: `sha256:43974ed74066b207`
- egress bucket: `bucket-a`
- policy version: `2.1.146`
- response status: `403`
- control-plane: `cc_gateway_control_plane/count_tokens_deferred`
- request count: `1`
- no real upstream: `True`
- no native fallback: `True`
- sub2api->gateway route: `/v1/messages/count_tokens?beta=true`
- sub2api->gateway header order: `Host, User-Agent, Content-Length, Anthropic-Version, Content-Type, X-Api-Key, X-Claude-Code-Session-Id, anthropic-beta, x-cc-account-id, x-cc-egress-bucket, x-cc-gateway-token, x-cc-policy-version, x-cc-provider, x-cc-token-type`
- sub2api->gateway header summary: `{"Anthropic-Version": "2023-06-01", "Content-Length": "", "Content-Type": "application/json", "Host": "loopback", "User-Agent": "claude-cli/99.9.9 (external, sdk-cli)", "X-Api-Key": "sha256:a7e3a1ddda2ce652", "X-Claude-Code-Session-Id": "sha256:89d07480e9d09240", "anthropic-beta": "client-beta", "x-cc-account-id": "sha256:43974ed74066b207", "x-cc-egress-bucket": "bucket-a", "x-cc-gateway-token": "sha256:f15ae5b5899f8327", "x-cc-policy-version": "2.1.146", "x-cc-provider": "anthropic", "x-cc-token-type": "sha256:6c793695171e793d"}`
- sub2api->gateway body sha256: `5be832de94a6089d6a5c13615778771ca19d69105170e843985ef18b0b9ef6fe`
- sub2api->gateway body keys: `messages, model`
- sub2api->gateway billing/cch: `False/False`
- note: `anthropic api-key count_tokens remains deferred; no native fallback observed`

### event_logging_v2_suppressed_local
- route: `/api/event_logging/v2/batch`
- decision: `suppress_local`
- response status: `200`
- request count: `0`
- no real upstream: `True`
- no native fallback: `True`
- note: `legacy telemetry is suppressed before any CC Gateway routing`

### event_logging_legacy_suppressed_local
- route: `/api/event_logging/batch`
- decision: `suppress_local`
- response status: `200`
- request count: `0`
- no real upstream: `True`
- no native fallback: `True`
- note: `legacy telemetry is suppressed before any CC Gateway routing`

### unknown_event_endpoint_blocked
- route: `/api/event_logging/v3/batch`
- decision: `block`
- response status: `404`
- request count: `0`
- no real upstream: `True`
- no native fallback: `True`
- note: `unknown event route is blocked and never reaches CC Gateway`

### gateway_control_plane_invalid_token_401
- route: `/v1/messages?beta=true`
- decision: `control_plane_401`
- selected account id hash: `sha256:c3ea99f86b2f8a74`
- egress bucket: `bucket-a`
- policy version: `2.1.146`
- response status: `401`
- control-plane: `control-plane/missing_gateway_token`
- request count: `0`
- no real upstream: `True`
- no native fallback: `True`
- note: `direct gateway control-plane probe; no upstream request observed`

### gateway_control_plane_missing_identity_403
- route: `/v1/messages?beta=true`
- decision: `control_plane_403`
- selected account id hash: `sha256:83cf8b609de60036`
- egress bucket: `bucket-a`
- policy version: `2.1.146`
- response status: `403`
- control-plane: `control-plane/missing_account_identity`
- request count: `0`
- no real upstream: `True`
- no native fallback: `True`
- note: `direct gateway control-plane probe; no upstream request observed`

### gateway_control_plane_missing_egress_bucket_400
- route: `/v1/messages?beta=true`
- decision: `control_plane_400`
- selected account id hash: `sha256:c3ea99f86b2f8a74`
- policy version: `2.1.146`
- response status: `400`
- control-plane: `control-plane/missing_egress_bucket`
- request count: `0`
- no real upstream: `True`
- no native fallback: `True`
- note: `direct gateway control-plane probe; no upstream request observed`

### gateway_unknown_endpoint_404
- route: `/v1/unknown?beta=true`
- decision: `block_404`
- selected account id hash: `sha256:c3ea99f86b2f8a74`
- egress bucket: `bucket-a`
- policy version: `2.1.146`
- response status: `404`
- control-plane: `control-plane/unsupported_route`
- request count: `0`
- no real upstream: `True`
- no native fallback: `True`
- note: `direct gateway control-plane probe; no upstream request observed`

### gateway_strip_verifier_failure_400
- route: `/v1/messages?beta=true`
- decision: `control_plane_400`
- selected account id hash: `sha256:c3ea99f86b2f8a74`
- egress bucket: `bucket-a`
- policy version: `2.1.146`
- response status: `400`
- control-plane: `control-plane/strip_verifier_failed`
- request count: `0`
- no real upstream: `True`
- no native fallback: `True`
- note: `direct gateway control-plane probe; no upstream request observed`

### gateway_signing_untrusted_cch_fail_closed_403
- route: `/v1/messages?beta=true`
- decision: `control_plane_403`
- selected account id hash: `sha256:c3ea99f86b2f8a74`
- egress bucket: `bucket-a`
- policy version: `2.1.146`
- response status: `403`
- control-plane: `control-plane/signing_untrusted_billing_input`
- request count: `0`
- no real upstream: `True`
- no native fallback: `True`
- note: `direct gateway control-plane probe; no upstream request observed`

### gateway_billing_mode_disabled_403
- route: `/v1/messages?beta=true`
- decision: `control_plane_403`
- selected account id hash: `sha256:c3ea99f86b2f8a74`
- egress bucket: `bucket-a`
- policy version: `2.1.146`
- response status: `403`
- control-plane: `control-plane/billing_cch_mode_disabled`
- request count: `0`
- no real upstream: `True`
- no native fallback: `True`
- note: `direct gateway control-plane probe; no upstream request observed`

## Omitted legacy coverage
- The underlying joint acceptance harness also exercised legacy OpenAI-compatible Anthropic conversion regression scenarios (`openai_chat_completions_to_anthropic`, `openai_responses_to_anthropic`).
- Those scenarios are excluded from this first-wave messages-only summary and are not counted as first-wave canary scope.

## Redaction scan
- passed: `True`