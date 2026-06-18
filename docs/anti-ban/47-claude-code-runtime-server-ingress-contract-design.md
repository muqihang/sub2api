# Claude Code Runtime Server Ingress Contract Design Gate

> Status: Task 3B design gate. Do not use this document as deployment approval.
>
> Local scope: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime`.
> Remote endpoint `http://198.12.67.185:18080` is production-side Sub2API main + CC Gateway main with real Claude formal-pool accounts.

## Decision

The local Claude Code Runtime cannot safely enter the production Claude native formal-pool yet. It must stop at the Task 3B design gate until a reviewed local-to-server ingress contract is available and provisioned.

The current local code has a CC Gateway adapter that can emit the right `x-cc-*` boundary headers, but the running canary `3017` does not have app-level `gateway.cc_gateway` configuration, and the existing local account `132` is only an ordinary Anthropic API-key passthrough record. Using account `132` or ordinary passthrough to `18080` would not be a safe native formal-pool boundary.

## Evidence Summary

### Local 3017 configuration shape

Redacted container/env inspection shows Claude Code native attestation and route hint material exist, but no app-level `gateway.cc_gateway` configuration is present:

- no `gateway.cc_gateway.enabled` equivalent;
- no `gateway.cc_gateway.base_url` equivalent;
- no `gateway.cc_gateway.token` equivalent;
- no `gateway.cc_gateway.providers.anthropic` equivalent.

Therefore local Sub2API cannot currently select the explicit CC Gateway Anthropic route in the running canary.

### Local account 132

Redacted DB topology shows account `132` has:

- platform `anthropic`;
- type `apikey`;
- group `8` (`zhumeng-claude-code-native`);
- status `error`;
- schedulable `false`;
- extra key only `anthropic_passthrough`;
- credential keys `api_key,base_url`.

It lacks the required CC Gateway and formal-pool fields:

- `cc_gateway_enabled`;
- `cc_gateway_canary_only`;
- `cc_gateway_policy_version`;
- `cc_gateway_routes`;
- `cc_gateway_egress_bucket_enabled`;
- `cc_gateway_egress_bucket`;
- `cc_gateway_account_ref`;
- formal-pool onboarding/runtime evidence.

It must not be edited in place while 3012 and 3017 share a DB.

### Existing local code boundary

Local Sub2API can build CC Gateway requests only when both app config and account metadata are present:

- app config: `gateway.cc_gateway.enabled=true`, `base_url`, `token`, `providers.anthropic=true`;
- account extra: `cc_gateway_enabled=true`, compatible `cc_gateway_policy_version`, route allowlist, and egress bucket;
- intended production identity metadata: a safe `cc_gateway_account_ref` mapped by the remote CC Gateway;
- outbound headers: `x-cc-gateway-token`, `x-cc-account-id`, `x-cc-provider`, `x-cc-token-type`, `x-cc-policy-version`, `x-cc-egress-bucket`.

Current local adapter code can fall back from missing `cc_gateway_account_ref` to a numeric account ID. That fallback is not sufficient for the approved production formal-pool ingress contract. Before live, formal-pool ingress must either explicitly require a safe account ref locally or prove that the remote CC Gateway fails closed on missing identity mapping.

Local `x-sub2api-*` native attestation and route markers are local trust material. They must not be ordinary passthrough material to a remote Sub2API or Anthropic-compatible endpoint.

### CC Gateway main policy

The current server-side CC Gateway shared-pool policy supports only a narrow set of paths:

| Route | Action |
| --- | --- |
| `POST /v1/messages?beta=true` | forward |
| `POST /v1/messages/count_tokens?beta=true` | block `403 count_tokens_deferred` |
| `POST /api/event_logging/batch` | suppress locally |
| `POST /api/event_logging/v2/batch` | suppress locally |
| other `/api/event_logging/*` | block |
| unknown method/path | block |

This means local count-token startup probes must remain local stub/deferred for launch unless a separate server-side auxiliary route is designed and approved.

## Required Contract

The correct production contract is a two-stage boundary:

```text
Claude Code CLI
  -> local loopback guard
  -> local Sub2API verifies local native/bridge route contract
  -> remote ingress with gateway-to-gateway proof or direct CC Gateway sub2api token
  -> remote Sub2API + CC Gateway selects pool account/persona/egress bucket
  -> Anthropic formal-pool account sees only canonical Claude Code native traffic
```

### Local guard to local Sub2API

- Use local `x-sub2api-*` native attestation only inside the local trust boundary.
- Use managed-device access token for Claude native formal-pool traffic.
- Use dedicated Sub2API key for bridge traffic.
- Never send bridge requests with native attestation.
- Control-plane requests are classified before any upload.

### Local Sub2API to remote ingress

One of these must be explicitly provisioned before live:

1. **Direct CC Gateway sub2api contract**
   - Local `gateway.cc_gateway.base_url` points to a CC Gateway sub2api ingress.
   - Local has a dedicated `x-cc-gateway-token` value.
   - Remote CC Gateway has account identity and egress bucket mappings for the selected account refs.
   - Local sends selected upstream credential plus `x-cc-*` headers; local `x-sub2api-*` is stripped.

2. **Server-side Sub2API gateway-to-gateway contract**
   - Remote Sub2API exposes a reviewed internal endpoint for local runtime ingress.
   - Local authenticates with a gateway-to-gateway proof, not raw user API key passthrough.
   - Remote Sub2API rebuilds selected formal-pool account, persona, and egress bucket server-side before invoking CC Gateway.
   - Local `x-sub2api-*` markers are not trusted by the remote as ordinary native attestation.

Using ordinary Anthropic-compatible API-key passthrough to `18080` is not an acceptable native formal-pool contract.

## Control-Plane Policy

Launch policy remains conservative:

- `/v1/messages?beta=true`: may forward only through the approved formal-pool ingress contract.
- `/v1/messages/count_tokens`: local startup probe stub or CC Gateway deferred response; no raw prompt-like body live upload unless separately approved.
- telemetry/event logging/eval: local safe intent/suppress only; never raw upload.
- bootstrap/feature/MCP/settings/org paths: local stub/cache/block according to the classification matrix.
- unknown drift: block/quarantine.

## Remote Worktree / Deploy / Rollback Plan

If server-side code changes are needed, do not edit deployed main directly.

1. Create a CC Gateway worktree if CC Gateway changes are required:
   - repo: `/Users/muqihang/chelingxi_workspace/cc-gateway`
   - worktree: `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-runtime-formal-pool-bridge-repair`
   - branch: `codex/claude-code-runtime-formal-pool-bridge-repair`
   - initialize/sync CodeGraph in that worktree.

2. Create a separate Sub2API server-side worktree/branch if remote Sub2API ingress code is required. Do not deploy from the local Claude Code Runtime worktree by accident.

3. Secret provisioning:
   - provision `x-cc-gateway-token` or gateway-to-gateway token out-of-band;
   - never commit or print the value;
   - verify only presence/length and hash-free safe metadata.

4. Tests before live:
   - localhost/mock CC Gateway sub2api messages forward;
   - count_tokens deferred/stub behavior;
   - event logging suppress and unknown route block;
   - local `x-sub2api-*` not forwarded to remote;
   - bridge provider cannot enter formal-pool;
   - account identity/egress bucket missing fails closed;
   - wrong/replayed/stale route hint fails closed.

5. Deployment approval:
   - provide exact branch/commit list;
   - provide config keys to set, redacted;
   - provide rollback command/previous image or commit;
   - get explicit user approval before changing production `18080` behavior.

6. Rollback:
   - disable the local canary feature flag first;
   - remove/disable new gateway-to-gateway ingress route;
   - revert to local stub-only control-plane behavior;
   - do not fall back to ordinary passthrough for native formal-pool.

## Current Stop Criteria

Stop before L8 live formal-pool messages unless all are true:

- 3017 has explicit app-level gateway config or a reviewed remote ingress contract;
- a canary-only account/group exists or an approved remote account mapping is provisioned;
- account 132 remains untouched in place while DB is shared;
- local messages route does not use ordinary passthrough to `18080`;
- count_tokens behavior is local stub/deferred and tested;
- bridge live remains disabled or isolated from formal-pool;
- all targeted local tests pass;
- any real-pool live canary has explicit user approval.
