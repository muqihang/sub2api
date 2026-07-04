# Plan85: Plan76 Request-Shape Rejects Are Non-Quarantining + Account 87 Recovery Evidence

## Final decision

`PASS_REQUEST_SHAPE_FAIL_CLOSED_NON_QUARANTINE_AND_ACCOUNT87_WARMING`

Plan85 implemented the narrow Sub2API fix: Plan76 local request-shape/control-plane rejects remain request-level fail-closed, but they no longer permanently quarantine/deschedule the account unless hard account-risk signals are also present. Account `id=87` was recovered through the official admin flow and is now in warming and schedulable.

## Scope

- Sub2API production entry: `18080`
- CC Gateway production entry: `127.0.0.1:18443`
- Production Go/uTLS sidecar: `127.0.0.1:19484`
- Rollback/old service: `127.0.0.1:18081` untouched
- Forbidden ports `3012/3017` untouched
- No raw prompt/body/response, ST/OAuth token, proxy credential, DB/Redis URL/password, account id material beyond numeric `id=87`, or admin token was recorded.

## Root cause summary

A runtime Claude Code CLI request carrying an MCP configured marker triggered CC Gateway's local Plan76 gate and returned:

- `403 formal_pool_mcp_shape_unapproved`

This is not caused by observed Claude Code version `2.1.195`. The observed client version admission accepts versions `>= 2.1.179`; the server-selected canonical tuple remains `2.1.197`.

The local request itself must continue to fail closed. The Sub2API defect was that an unknown/local CC Gateway control-plane reject could be treated as an account-level risk, causing quarantine/descheduling. Plan85 classifies the following CC Gateway reject codes as request-level non-quarantining unless hard risk indicators are also present:

- `formal_pool_mcp_shape_unapproved`
- `formal_pool_non_streaming_profile_unapproved`
- `formal_pool_count_tokens_profile_unapproved`
- `formal_pool_model_version_unsupported`

Hard-risk signals such as missing identity, proxy mismatch, fallback, verifier failure, invalid auth, or risk markers still quarantine/fail closed as before.

## Code changes

Sub2API commit:

- `e713eef4d fix: keep plan76 request-shape rejects non-quarantining`

Changed files:

- `backend/internal/service/gateway_service.go`
- `backend/internal/service/gateway_cc_gateway_control_plane_test.go`

CC Gateway code was not changed by Plan85.

## Tests

Executed locally in Sub2API backend:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/backend
go test ./internal/service -run 'CCGatewayControlPlane|FormalPool.*Healthcheck|FormalPool.*Onboarding|Canonical|Promotion|ControlPlane|Model|CountTokens|MCP' -count=1
```

Result: PASS.

CC Gateway read-only verification confirmed the MCP gate is in `src/proxy.ts` and fires before upstream. Existing Plan76 MCP tests still pass. One unrelated future-version fixture test was observed to fail on an older `malformed_formal_pool_context_attestation` fixture and was not caused by Plan85.

## Production deployment

Production server: `198.12.67.185`

Deployed Sub2API binary:

- Release path bucket: `/opt/plan85/releases/20260704T093007Z-plan85-request-shape-no-quarantine/backend/`
- Binary: `sub2api-server-e713eef4-embed-linux-amd64`
- SHA256: `7f2770587943fcf1d027c6f0a189da7021f68a9610f10724c160b7aff23caf4b`

Runtime after deployment:

- `18080`: Plan85 Sub2API binary, health `200`, root UI `200`
- `18443`: CC Gateway running; unauthenticated health/root returns `401` authentication bucket
- `19484`: production sidecar health `200`
- `18081`: old rollback Docker proxy still running, untouched
- `3012/3017`: untouched

A restart hiccup caused by shell `env` expansion was recovered by launching with the saved process environment via `os.execve`. No rollback was required.

## Account 87 official recovery flow

Authorization for the admin calls was generated in memory on the production host from existing server configuration; no token/key/secret was printed or persisted.

Flow executed against `http://127.0.0.1:18080`:

1. `POST /api/v1/admin/accounts/87/formal-pool/runtime-register`
2. `POST /api/v1/admin/accounts/87/formal-pool/healthcheck`
3. `POST /api/v1/admin/accounts/87/formal-pool/start-warming`

Safe result table:

| Step | HTTP | Safe result |
| --- | ---: | --- |
| runtime-register | 200 | `onboarding_stage=runtime_registered`, `status=active`, `cc_gateway_seen=true`, `runtime_evidence_complete=true`, `schedulable=false` |
| healthcheck | 200 | `onboarding_stage=healthcheck_passed`, `healthcheck_status=passed`, `healthcheck_evidence_persisted=true`, `cc_gateway_seen=true`, `schedulable=false` |
| start-warming | 200 | `onboarding_stage=warming`, `healthcheck_status=passed`, `status=active`, `schedulable=true` |

Final account 87 safe state:

- `status=active`
- `onboarding_stage=warming`
- `healthcheck_status=passed`
- `schedulable=true`
- `cc_gateway_runtime_registered=true`
- `cc_gateway_policy_version=2.1.197`
- `cc_gateway_persona_profile=claude-code-2.1.197-macos-local`
- `cc_gateway_egress_tls_profile_ref=tls-profile:claude-code-2.1.197-real-oracle-tcp-v1`
- `quarantine_reason` cleared
- last health status bucket: `status_2xx`

## CC Gateway MCP shape findings

Read-only subagent review confirmed:

- `formal_pool_mcp_shape_unapproved` is emitted by `verifyPlan76FormalPoolBodyPolicy()` in CC Gateway `src/proxy.ts`.
- The gate triggers on either an observed profile MCP marker, top-level MCP keys, or recursive key/string MCP markers.
- A normal runtime `/v1/messages` MCP local reject happens before upstream and before persistent session-authority ledger writes, so CC Gateway itself does not persistently mutate account state for this request.
- The scanner is currently provenance-blind: ordinary text mentioning MCP can match the broad recursive marker. This is safe fail-closed but may be over-broad.

Recommended follow-up hardening, not required for Plan85:

- Add safe buckets such as `mcp_trigger_source_bucket`, `mcp_location_bucket`, and `mcp_config_surface_bucket` to distinguish actual MCP configuration from benign text without recording raw bodies.
- Add explicit tests for observed `2.1.195 + MCP marker` fail-closed and observed `2.1.195 + no MCP marker` canonical forward behavior.

## Safety / leak scan

- No raw prompt/body/response was written.
- No ST/OAuth token, proxy credential, admin token, DB URL/password, Redis URL/password, cookie, or account credential was written.
- Server scratch/env files remain outside the repo and were not committed.
- No destructive DB/Redis operations were run.
- No `git reset`, `git clean`, `git checkout --`, `git restore`, rebase, force push, `sudo`, `chmod -R`, or `chown -R` was run.

## Final production state

- `18080` is running the Plan85 Sub2API binary and serves health/root successfully.
- Production CC Gateway and Go/uTLS sidecar remain in the Plan82 sidecar-enabled production path.
- Node direct fallback remains not used by this recovery flow.
- Account `87` is restored through the official flow and is schedulable in warming.
