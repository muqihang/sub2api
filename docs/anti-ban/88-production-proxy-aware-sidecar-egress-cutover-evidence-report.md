# Plan88 Production Proxy-Aware Sidecar Egress Cutover Evidence Report

## Final decision

`PASS_PRODUCTION_PROXY_AWARE_SIDECAR_CUTOVER`

Plan88 completed the narrow production fix for the 429 root cause: production Go/uTLS sidecar egress now requires the server-selected account proxy instead of direct-dialing `api.anthropic.com:443` from the server IP.

No extra Claude account canary request was sent by this plan. The user may now retest through Claude Code CLI.

## Commits and artifacts

- Sub2API production entrypoint remains current Plan86/87-era production binary:
  - commit bucket: `ed73ffbb2`
  - process path bucket: `/opt/plan86/releases/20260704T135938Z-mcp-global/.../sub2api-server-...`
- CC Gateway commits deployed:
  - `da48a4b` `sidecar: route production TLS egress through account proxy`
  - `6b56ed9` `sidecar: bind production egress to account proxy`
- Production release path:
  - `/opt/plan88/releases/20260705T034351Z-proxy-aware-sidecar-6b56ed9/`
- Artifact hashes:
  - `cc-gateway/dist/index.js`: `2badf2adeff8d7bda44e547afdda2e68ee0146cbab8cdc2ca7ad5922e7c02a34`
  - `cc-gateway/dist/egress-sidecar-client.js`: `bb5d73238b9172453a9a97cabd1ed9088f55abf5911f97412232c15900f4962f`
  - `sidecar/egress-tls-sidecar`: `56440ce260863ed9a3450680cdf5769d0d28a20ed842ce501ce1a30c00a68311`

## Code changes

CC Gateway / sidecar only:

- Added production `proxy_binding_secret` config gate.
- CC Gateway now sends `x-cc-egress-proxy-url` plus an HMAC `x-cc-egress-proxy-binding`.
- The proxy binding HMAC covers:
  - egress bucket
  - proxy identity ref
  - proxy URL
  - target host
  - target port
- Go sidecar production mode now requires:
  - account proxy URL
  - valid proxy binding HMAC
  - production dial mode
  - no test dial override
- Production rejects weak, placeholder, or reused proxy binding material.
- Direct provider host as proxy remains rejected.
- Node direct HTTPS fallback remains disabled/fail-closed.

## Tests run locally

All passed:

- `npx tsx tests/egress-tls-sidecar.test.ts`
- `npx tsx tests/config.test.ts`
- `npx tsx tests/proxy-sub2api.test.ts`
- `npx tsx tests/formal-pool-real-chain-mock-response.test.ts`
- `npx tsx tests/formal-pool-mcp-connector-compat.test.ts`
- `npx tsx tests/egress-tls-sidecar-real.test.ts`
- `npx tsc --noEmit`
- `cd sidecar/egress-tls-sidecar && go test ./...`

Review agent REQUIRED_EDITS fixed:

- HMAC now binds `egress_bucket`.
- Production proxy binding secret strength/reuse validation added.

## Production baseline before cutover

Production server: `198.12.67.185`

Before Plan88 cutover:

| Port | Binding | Process bucket | Status |
|---|---:|---|---|
| `18080` | `*` | Plan86 Sub2API PID bucket `2649442` | running |
| `18443` | `127.0.0.1` | Plan87 CC Gateway PID bucket `2683878` | running |
| `19484` | `127.0.0.1` | Plan87 sidecar PID bucket `2683870` | running |
| `18081` | `127.0.0.1` | old docker-proxy PID bucket `2823001` | running, untouched |
| `3012` | n/a | not listening | untouched |
| `3017` | n/a | not listening | untouched |

## Dry validation

Static production config validation PASS:

- `mode=sub2api`
- production upstream enabled
- `egress_tls_sidecar.enabled=true`
- `mock_messages_response.enabled=false`
- `dial_override` absent
- logical target host: `api.anthropic.com`
- sidecar endpoint loopback-only
- production sidecar env: `EGRESS_TLS_SIDECAR_DIAL_MODE=production`
- no test dial override env
- `proxy_binding_secret` present in CC Gateway config and sidecar env
- config load PASS with Plan88 dist

Dry-start validation PASS on temporary loopback ports:

- dry CC Gateway: `127.0.0.1:19543`
- dry sidecar: `127.0.0.1:19584`
- dry sidecar health: PASS
- dry gateway health: PASS via `/_health`
- dry processes stopped after validation
- existing `18080/18081/18443/19484` baseline remained unchanged during dry run

## Cutover result

Cutover action:

- Stopped only Plan87-owned CC Gateway and sidecar PIDs.
- Started Plan88 sidecar on `127.0.0.1:19484`.
- Started Plan88 CC Gateway on `127.0.0.1:18443`.
- Did not restart or rebind `18080` Sub2API.
- Did not touch `18081`, `3012`, or `3017`.

After cutover:

| Port | Binding | Process bucket | Status |
|---|---:|---|---|
| `18080` | `*` | Plan86 Sub2API PID bucket `2649442` | running, unchanged |
| `18443` | `127.0.0.1` | Plan88 CC Gateway PID bucket `3071240` | running |
| `19484` | `127.0.0.1` | Plan88 sidecar PID bucket `3071227` | running |
| `18081` | `127.0.0.1` | old docker-proxy PID bucket `2823001` | running, unchanged |
| `3012` | n/a | not listening | untouched |
| `3017` | n/a | not listening | untouched |

Health after cutover:

- `18080`: `200`
- `18443 /_health`: `200`
- `19484 /_health`: `200`

## Post-cutover observation

Observed safe buckets only:

- 18080 health remained `200`.
- CC Gateway health remained `200`.
- Sidecar health remained `200`.
- No unexpected 5xx spike observed in safe log tail.
- No sidecar rejection spike observed in safe log tail.
- `node_direct_fallback_count=0`.
- `provider_direct_bypass_count=0`.
- `sidecar_direct_dial_count=0`.

No raw prompts, bodies, responses, account IDs, DB URLs, Redis URLs, proxy credentials, cookies, tokens, or secrets were recorded.

## Rollback availability

Rollback remains available:

1. Stop Plan88 CC Gateway PID bucket `3071240`.
2. Stop Plan88 sidecar PID bucket `3071227`.
3. Restart preserved Plan87 scripts under:
   - `/opt/plan87/releases/20260704T151700Z-sidecar-stream-475626d/`
4. Verify:
   - `127.0.0.1:18443 /_health`
   - `127.0.0.1:19484 /_health`
   - `18080` health

Old docker pair was not deleted.

## Explicit safety confirmations

- Production traffic is not routed to mock collector.
- `mock_messages_response` remains disabled for production.
- test dial override is disabled/absent.
- sidecar target remains server-selected `api.anthropic.com:443`.
- request-controlled target host/SNI/Host/dial override remains forbidden.
- no Node direct HTTPS fallback was enabled.
- no DB/Redis destructive operation was performed.
- no extra Claude account canary request was sent.
- old docker-proxy on `18081` remains running and untouched.

## Final production state

Current production path:

```text
Public 18080 Sub2API
-> local CC Gateway 127.0.0.1:18443 (Plan88, proxy-aware sidecar client)
-> local Go/uTLS sidecar 127.0.0.1:19484 (Plan88, production dial mode)
-> server-selected account proxy
-> api.anthropic.com:443
```

This is ready for user retest from Claude Code CLI.
