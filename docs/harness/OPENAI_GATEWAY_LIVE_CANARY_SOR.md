# OpenAI Gateway Live Canary SOR

## Blocked Item

- Real GPT/OpenAI OAuth account is still unavailable in the current workspace, so live canary execution remains blocked.

## Required Canary Evidence

- Canary account IDs:
  Record the exact OpenAI OAuth account IDs selected for canary.
- Bucket assignment evidence:
  For each canary account, capture `/_verify` output proving the expected `egress_bucket`, `proxy_selected`, `proxy_label`, and `proxy_hash`.
- HTTP Responses evidence:
  Capture at least one successful `/v1/responses` request per selected bucket.
- WS evidence:
  Capture at least one successful WS/Codex request path and confirm the expected bucket/runtime/profile behavior.
- Refresh evidence:
  Capture a successful OpenAI OAuth refresh path and verify the account remains schedulable afterward.
- Log redaction evidence:
  Confirm logs, admin snapshots, usage records, and preflight output do not expose raw upstream tokens or proxy credentials.
- Rollback evidence:
  Record the exact rollback step for each canary account or bucket assignment change.

## Explicit Transport Stance

- OpenAI Gateway makes no TLS/JA3/JA4/ALPN/HTTP2/WS transport camouflage claim in this phase.
- Any future transport-level claim requires separate live evidence and explicit approval.

## Canary Stop Conditions

- `direct_egress_in_production`
- `missing_egress_bucket`
- `disabled_egress_bucket`
- `credential_storage_not_production_safe`
- `oauth_session_topology_not_production_safe`
- Any canary account becomes terminal, cooling unexpectedly, or loses expected `responses_write` capability.

## Final Signoff Checklist

- `/_health` shows no blocking degraded reason for the canary set.
- `warning_codes` do not contain any stop condition.
- Each selected account resolves to the intended bucket.
- HTTP and WS evidence has been captured.
- Refresh evidence has been captured.
- Redaction evidence has been captured.
- Rollback path has been tested or explicitly documented.
