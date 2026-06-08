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
digest_omitted_by_policy: true
- egress bucket: `bucket-a`
- policy version: `2.1.146`
- response status: `200`
- request count: `1`
- no real upstream: `True`
- no native fallback: `True`
- sub2api->gateway route: `/v1/messages?beta=true`
- sub2api->gateway header order: `Host, User-Agent, Content-Length, X-Claude-Code-Session-Id, anthropic-beta, anthropic-version, authorization, content-type, x-cc-account-id, x-cc-account-uuid, x-cc-egress-bucket, x-cc-gateway-token, x-cc-policy-version, x-cc-provider, x-cc-token-type`
digest_omitted_by_policy: true
digest_omitted_by_policy: true
- sub2api->gateway body keys: `messages, metadata, model, stream, system`
- sub2api->gateway billing/cch: `True/True`
- sub2api->gateway metadata fields: `account_uuid, device_id, session_id`
digest_omitted_by_policy: true
digest_omitted_by_policy: true
- gateway->upstream route: `/v1/messages?beta=true`
- gateway->upstream header order: `authorization, Accept, User-Agent, X-Stainless-Arch, X-Stainless-Lang, X-Stainless-OS, X-Stainless-Package-Version, X-Stainless-Retry-Count, X-Stainless-Runtime, X-Stainless-Runtime-Version, X-Stainless-Timeout, anthropic-dangerous-direct-browser-access, anthropic-version, x-app, Accept-Encoding, anthropic-beta, X-Claude-Code-Session-Id, content-type, host, content-length, Connection`
digest_omitted_by_policy: true
digest_omitted_by_policy: true
- gateway->upstream body keys: `messages, metadata, model, stream, system`
- gateway->upstream billing/cch: `False/False`
- gateway->upstream metadata fields: `account_uuid, device_id, session_id`
digest_omitted_by_policy: true
digest_omitted_by_policy: true
- note: `sub2api->gateway body unchanged while gateway->upstream body stripped billing markers`
- note: `gateway final persona is canonical Claude Code 2.1.146`

### oauth_native_messages_sign_primary
- route: `/v1/messages?beta=true`
- decision: `forward_sign_primary`
digest_omitted_by_policy: true
- egress bucket: `bucket-a`
- policy version: `2.1.146`
- response status: `200`
- request count: `1`
- no real upstream: `True`
- no native fallback: `True`
- sub2api->gateway route: `/v1/messages?beta=true`
- sub2api->gateway header order: `Host, User-Agent, Content-Length, X-Claude-Code-Session-Id, anthropic-beta, anthropic-version, authorization, content-type, x-cc-account-id, x-cc-account-uuid, x-cc-egress-bucket, x-cc-gateway-token, x-cc-policy-version, x-cc-provider, x-cc-token-type`
digest_omitted_by_policy: true
digest_omitted_by_policy: true
- sub2api->gateway body keys: `messages, metadata, model, stream`
- sub2api->gateway billing/cch: `False/False`
- sub2api->gateway metadata fields: `account_uuid, device_id, session_id`
digest_omitted_by_policy: true
digest_omitted_by_policy: true
- gateway->upstream route: `/v1/messages?beta=true`
- gateway->upstream header order: `authorization, Accept, User-Agent, X-Stainless-Arch, X-Stainless-Lang, X-Stainless-OS, X-Stainless-Package-Version, X-Stainless-Retry-Count, X-Stainless-Runtime, X-Stainless-Runtime-Version, X-Stainless-Timeout, anthropic-dangerous-direct-browser-access, anthropic-version, x-app, Accept-Encoding, anthropic-beta, X-Claude-Code-Session-Id, content-type, host, content-length, Connection`
digest_omitted_by_policy: true
digest_omitted_by_policy: true
- gateway->upstream body keys: `messages, metadata, model, stream, system`
- gateway->upstream billing/cch: `True/True`
- gateway->upstream metadata fields: `account_uuid, device_id, session_id`
digest_omitted_by_policy: true
digest_omitted_by_policy: true
- note: `sub2api->gateway body is pre-final with no billing/CCH material`
- note: `cc gateway generated billing block, cc_version suffix, CCH, canonical persona, and post-sign verifier passed before localhost upstream capture`

### oauth_native_count_tokens_deferred
- route: `/v1/messages/count_tokens?beta=true`
- decision: `defer_block`
digest_omitted_by_policy: true
- egress bucket: `bucket-a`
- policy version: `2.1.146`
- response status: `403`
- control-plane: `cc_gateway_control_plane/count_tokens_deferred`
- request count: `1`
- no real upstream: `True`
- no native fallback: `True`
- sub2api->gateway route: `/v1/messages/count_tokens?beta=true`
- sub2api->gateway header order: `Host, User-Agent, Content-Length, X-Claude-Code-Session-Id, anthropic-beta, anthropic-version, authorization, content-type, x-cc-account-id, x-cc-account-uuid, x-cc-egress-bucket, x-cc-gateway-token, x-cc-policy-version, x-cc-provider, x-cc-token-type`
digest_omitted_by_policy: true
digest_omitted_by_policy: true
- sub2api->gateway body keys: `messages, model`
- sub2api->gateway billing/cch: `False/False`
- note: `route is explicitly deferred in first wave; no upstream request observed`

### apikey_native_messages_strip
- route: `/v1/messages?beta=true`
- decision: `forward_strip`
digest_omitted_by_policy: true
- egress bucket: `bucket-a`
- policy version: `2.1.146`
- response status: `200`
- request count: `1`
- no real upstream: `True`
- no native fallback: `True`
- sub2api->gateway route: `/v1/messages?beta=true`
- sub2api->gateway header order: `Host, User-Agent, Content-Length, X-Claude-Code-Session-Id, anthropic-beta, anthropic-version, content-type, x-api-key, x-cc-account-id, x-cc-egress-bucket, x-cc-gateway-token, x-cc-policy-version, x-cc-provider, x-cc-token-type`
digest_omitted_by_policy: true
digest_omitted_by_policy: true
- sub2api->gateway body keys: `messages, metadata, model, stream, system`
- sub2api->gateway billing/cch: `True/True`
- sub2api->gateway metadata fields: `account_uuid, device_id, session_id`
digest_omitted_by_policy: true
digest_omitted_by_policy: true
- gateway->upstream route: `/v1/messages?beta=true`
- gateway->upstream header order: `x-api-key, Accept, User-Agent, X-Stainless-Arch, X-Stainless-Lang, X-Stainless-OS, X-Stainless-Package-Version, X-Stainless-Retry-Count, X-Stainless-Runtime, X-Stainless-Runtime-Version, X-Stainless-Timeout, anthropic-dangerous-direct-browser-access, anthropic-version, x-app, Accept-Encoding, anthropic-beta, X-Claude-Code-Session-Id, content-type, host, content-length, Connection`
digest_omitted_by_policy: true
digest_omitted_by_policy: true
- gateway->upstream body keys: `messages, metadata, model, stream, system`
- gateway->upstream billing/cch: `False/False`
- gateway->upstream metadata fields: `account_uuid, device_id, session_id`
digest_omitted_by_policy: true
digest_omitted_by_policy: true
- note: `anthropic api-key passthrough is included for /v1/messages in first wave`
- note: `gateway strips billing markers before localhost upstream capture`

### apikey_native_count_tokens_deferred
- route: `/v1/messages/count_tokens?beta=true`
- decision: `defer_block`
digest_omitted_by_policy: true
- egress bucket: `bucket-a`
- policy version: `2.1.146`
- response status: `403`
- control-plane: `cc_gateway_control_plane/count_tokens_deferred`
- request count: `1`
- no real upstream: `True`
- no native fallback: `True`
- sub2api->gateway route: `/v1/messages/count_tokens?beta=true`
- sub2api->gateway header order: `Host, User-Agent, Content-Length, Anthropic-Version, Content-Type, X-Api-Key, X-Claude-Code-Session-Id, anthropic-beta, x-cc-account-id, x-cc-egress-bucket, x-cc-gateway-token, x-cc-policy-version, x-cc-provider, x-cc-token-type`
digest_omitted_by_policy: true
digest_omitted_by_policy: true
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
digest_omitted_by_policy: true
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
digest_omitted_by_policy: true
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
digest_omitted_by_policy: true
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
digest_omitted_by_policy: true
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
digest_omitted_by_policy: true
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
digest_omitted_by_policy: true
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
digest_omitted_by_policy: true
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
