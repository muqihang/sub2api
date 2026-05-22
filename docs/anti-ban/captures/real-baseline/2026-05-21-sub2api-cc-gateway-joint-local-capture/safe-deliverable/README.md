# Joint local capture acceptance

- Executed at: `2026-05-21T23:02:28-07:00`
- Gateway mode: `sub2api`
- No real upstream: `true`
- No raw secrets in safe deliverable: `true`
- No native fallback: `true`
- Sub2API not final-mutating on CC Gateway routes: `true`
- CC Gateway final-output owner: `true`
- Negative cases fail closed: `true`

## oauth_native_messages_strip - PASS
- route: `/v1/messages?beta=true`
- decision: `forward_strip`
- selected account id hash: `sha256:c3ea99f86b2f8a74`
- egress bucket: `bucket-a`
- policy version: `2.1.146`
- response status: `200`
- request count: `1`
- no real upstream: `true`
- no native fallback: `true`
- sub2api->gateway route: `/v1/messages?beta=true`
- sub2api->gateway body sha256: `ae83f8d4dce205590e4684c8e486bfa822a8111e43a86f7f119587dc956aac16`
- sub2api->gateway billing/cch: `true/true`
- gateway->upstream route: `/v1/messages?beta=true`
- gateway->upstream body sha256: `ba1e2cb2bd08ad50abce17826c78025aa0808f952b7686502017c060b58d36b9`
- gateway->upstream billing/cch: `false/false`
- note: `sub2api->gateway body unchanged while gateway->upstream body stripped billing markers`
- note: `gateway final persona is canonical Claude Code 2.1.146`

## oauth_native_count_tokens_deferred - PASS
- route: `/v1/messages/count_tokens?beta=true`
- decision: `defer_block`
- selected account id hash: `sha256:c3ea99f86b2f8a74`
- egress bucket: `bucket-a`
- policy version: `2.1.146`
- response status: `403`
- control-plane: `cc_gateway_control_plane/count_tokens_deferred`
- request count: `1`
- no real upstream: `true`
- no native fallback: `true`
- sub2api->gateway route: `/v1/messages/count_tokens?beta=true`
- sub2api->gateway body sha256: `5be832de94a6089d6a5c13615778771ca19d69105170e843985ef18b0b9ef6fe`
- sub2api->gateway billing/cch: `false/false`
- note: `route is explicitly deferred in first wave; no upstream request observed`

## oauth_native_messages_sign_primary - PASS
- route: `/v1/messages?beta=true`
- decision: `forward_sign_primary`
- selected account id hash: `sha256:c3ea99f86b2f8a74`
- egress bucket: `bucket-a`
- policy version: `2.1.146`
- response status: `200`
- request count: `1`
- no real upstream: `true`
- no native fallback: `true`
- sub2api->gateway route: `/v1/messages?beta=true`
- sub2api->gateway body sha256: `4e1374b2d3b973e0030171e27a74a2b730855da91bf7cfa98b1a58b721125d01`
- sub2api->gateway billing/cch: `false/false`
- gateway->upstream route: `/v1/messages?beta=true`
- gateway->upstream body sha256: `0d6fd64bcd7686a7aa49c92ec5e2a9d5c6fbb0e48509dd78cf9e308cad2d2ff2`
- gateway->upstream billing/cch: `true/true`
- note: `sub2api->gateway body is pre-final with no billing/CCH material`
- note: `cc gateway generated billing block, cc_version suffix, CCH, canonical persona, and post-sign verifier passed before localhost upstream capture`

## apikey_native_messages_strip - PASS
- route: `/v1/messages?beta=true`
- decision: `forward_strip`
- selected account id hash: `sha256:43974ed74066b207`
- egress bucket: `bucket-a`
- policy version: `2.1.146`
- response status: `200`
- request count: `1`
- no real upstream: `true`
- no native fallback: `true`
- sub2api->gateway route: `/v1/messages?beta=true`
- sub2api->gateway body sha256: `ae83f8d4dce205590e4684c8e486bfa822a8111e43a86f7f119587dc956aac16`
- sub2api->gateway billing/cch: `true/true`
- gateway->upstream route: `/v1/messages?beta=true`
- gateway->upstream body sha256: `2abd863d0906a52657a53f9641e5ef03012cc0f4d73f2ee4aa92c9bc5b5e0693`
- gateway->upstream billing/cch: `false/false`
- note: `anthropic api-key passthrough is included for /v1/messages in first wave`
- note: `gateway strips billing markers before localhost upstream capture`

## apikey_native_count_tokens_deferred - PASS
- route: `/v1/messages/count_tokens?beta=true`
- decision: `defer_block`
- selected account id hash: `sha256:43974ed74066b207`
- egress bucket: `bucket-a`
- policy version: `2.1.146`
- response status: `403`
- control-plane: `cc_gateway_control_plane/count_tokens_deferred`
- request count: `1`
- no real upstream: `true`
- no native fallback: `true`
- sub2api->gateway route: `/v1/messages/count_tokens?beta=true`
- sub2api->gateway body sha256: `5be832de94a6089d6a5c13615778771ca19d69105170e843985ef18b0b9ef6fe`
- sub2api->gateway billing/cch: `false/false`
- note: `anthropic api-key count_tokens remains deferred; no native fallback observed`

## openai_chat_completions_to_anthropic - PASS
- route: `/v1/chat/completions -> /v1/messages?beta=true`
- decision: `forward_strip`
- selected account id hash: `sha256:c3ea99f86b2f8a74`
- egress bucket: `bucket-a`
- policy version: `2.1.146`
- response status: `200`
- request count: `1`
- no real upstream: `true`
- no native fallback: `true`
- sub2api->gateway route: `/v1/messages?beta=true`
- sub2api->gateway body sha256: `03b585411521c8ffab4b92f72457b76e032fa88aced87b984dbfffa35c20f836`
- sub2api->gateway billing/cch: `false/false`
- gateway->upstream route: `/v1/messages?beta=true`
- gateway->upstream body sha256: `afa8485f46f06a5b374ea4f1817439a6021d2fa4d0490e8d5d4b5e6fd339f09d`
- gateway->upstream billing/cch: `false/false`
- note: `Sub2API performs protocol conversion only; CC Gateway injects final metadata/session binding`

## openai_responses_to_anthropic - PASS
- route: `/v1/responses -> /v1/messages?beta=true`
- decision: `forward_strip`
- selected account id hash: `sha256:c3ea99f86b2f8a74`
- egress bucket: `bucket-a`
- policy version: `2.1.146`
- response status: `200`
- request count: `1`
- no real upstream: `true`
- no native fallback: `true`
- sub2api->gateway route: `/v1/messages?beta=true`
- sub2api->gateway body sha256: `03b585411521c8ffab4b92f72457b76e032fa88aced87b984dbfffa35c20f836`
- sub2api->gateway billing/cch: `false/false`
- gateway->upstream route: `/v1/messages?beta=true`
- gateway->upstream body sha256: `afa8485f46f06a5b374ea4f1817439a6021d2fa4d0490e8d5d4b5e6fd339f09d`
- gateway->upstream billing/cch: `false/false`
- note: `Sub2API responses conversion path leaves final metadata/session ownership to CC Gateway`

## event_logging_v2_suppressed_local - PASS
- route: `/api/event_logging/v2/batch`
- decision: `suppress_local`
- response status: `200`
- request count: `0`
- no real upstream: `true`
- no native fallback: `true`
- note: `legacy telemetry is suppressed before any CC Gateway routing`

## event_logging_legacy_suppressed_local - PASS
- route: `/api/event_logging/batch`
- decision: `suppress_local`
- response status: `200`
- request count: `0`
- no real upstream: `true`
- no native fallback: `true`
- note: `legacy telemetry is suppressed before any CC Gateway routing`

## unknown_event_endpoint_blocked - PASS
- route: `/api/event_logging/v3/batch`
- decision: `block`
- response status: `404`
- request count: `0`
- no real upstream: `true`
- no native fallback: `true`
- note: `unknown event route is blocked and never reaches CC Gateway`

## gateway_control_plane_invalid_token_401 - PASS
- route: `/v1/messages?beta=true`
- decision: `control_plane_401`
- selected account id hash: `sha256:c3ea99f86b2f8a74`
- egress bucket: `bucket-a`
- policy version: `2.1.146`
- response status: `401`
- control-plane: `control-plane/missing_gateway_token`
- request count: `0`
- no real upstream: `true`
- no native fallback: `true`
- note: `direct gateway control-plane probe; no upstream request observed`

## gateway_control_plane_missing_identity_403 - PASS
- route: `/v1/messages?beta=true`
- decision: `control_plane_403`
- selected account id hash: `sha256:83cf8b609de60036`
- egress bucket: `bucket-a`
- policy version: `2.1.146`
- response status: `403`
- control-plane: `control-plane/missing_account_identity`
- request count: `0`
- no real upstream: `true`
- no native fallback: `true`
- note: `direct gateway control-plane probe; no upstream request observed`

## gateway_control_plane_missing_egress_bucket_400 - PASS
- route: `/v1/messages?beta=true`
- decision: `control_plane_400`
- selected account id hash: `sha256:c3ea99f86b2f8a74`
- policy version: `2.1.146`
- response status: `400`
- control-plane: `control-plane/missing_egress_bucket`
- request count: `0`
- no real upstream: `true`
- no native fallback: `true`
- note: `direct gateway control-plane probe; no upstream request observed`

## gateway_unknown_endpoint_404 - PASS
- route: `/v1/unknown?beta=true`
- decision: `block_404`
- selected account id hash: `sha256:c3ea99f86b2f8a74`
- egress bucket: `bucket-a`
- policy version: `2.1.146`
- response status: `404`
- control-plane: `control-plane/unsupported_route`
- request count: `0`
- no real upstream: `true`
- no native fallback: `true`
- note: `direct gateway control-plane probe; no upstream request observed`

## gateway_strip_verifier_failure_400 - PASS
- route: `/v1/messages?beta=true`
- decision: `control_plane_400`
- selected account id hash: `sha256:c3ea99f86b2f8a74`
- egress bucket: `bucket-a`
- policy version: `2.1.146`
- response status: `400`
- control-plane: `control-plane/strip_verifier_failed`
- request count: `0`
- no real upstream: `true`
- no native fallback: `true`
- note: `direct gateway control-plane probe; no upstream request observed`

## gateway_signing_untrusted_cch_fail_closed_403 - PASS
- route: `/v1/messages?beta=true`
- decision: `control_plane_403`
- selected account id hash: `sha256:c3ea99f86b2f8a74`
- egress bucket: `bucket-a`
- policy version: `2.1.146`
- response status: `403`
- control-plane: `control-plane/signing_untrusted_billing_input`
- request count: `0`
- no real upstream: `true`
- no native fallback: `true`
- note: `direct gateway control-plane probe; no upstream request observed`

## gateway_billing_mode_disabled_403 - PASS
- route: `/v1/messages?beta=true`
- decision: `control_plane_403`
- selected account id hash: `sha256:c3ea99f86b2f8a74`
- egress bucket: `bucket-a`
- policy version: `2.1.146`
- response status: `403`
- control-plane: `control-plane/billing_cch_mode_disabled`
- request count: `0`
- no real upstream: `true`
- no native fallback: `true`
- note: `direct gateway control-plane probe; no upstream request observed`

## Redaction scan
- passed: `true`
