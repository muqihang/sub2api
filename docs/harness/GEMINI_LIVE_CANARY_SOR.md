# Gemini Live Canary SOR

## Execution Status

- Status: completed
- Environment: local single-instance canary on `http://127.0.0.1:18080`
- Scope: one real Gemini Google OAuth account (`google_one` family), one Gemini group, one user API key, admin health/verify, public `/v1beta/models`, and one real `generateContent` request
- Residual gap: remote preprod / production host canary has not been executed yet

## Required Canary Evidence

- Canary account IDs:
  - `group_id=3` (`Gemini Canary Group`)
  - `account_id=3` (`Gemini OAuth Canary`)
  - `api_key_id=2` (`Gemini Canary Key`)
- Runtime contract evidence:
  - `/api/v1/admin/gemini/verify?account_id=3`
  - observed `runtime_contract.account_family=google_one`
  - observed `runtime_contract.upstream_family=code_assist`
  - observed `runtime_contract.requires_project_id=false`
- `project_id` evidence:
  - `project_id=empyrean-patrol-3qb1d`
  - `project_id_status=present`
- tier evidence:
  - `tier_id=google_one_free`
  - `tier_status=present`
  - this run used the explicit selected tier fallback path after Google Drive quota probing returned `403`; the persisted account remained healthy and `oauth_state=ok`
- token-cache evidence:
  - `token_cache_mode=encrypted`
  - `token_cache_state=ok`
  - `token_cache_reason=""`
- OAuth degraded evidence:
  - `oauth_state=ok`
  - `oauth_reason=""`
- session-topology evidence:
  - `session_store=redis`
  - `sticky_session_safety_required=true`
- smoke evidence:
  - `/v1beta/models` succeeded with user API key and returned real upstream model entries including:
    - `models/gemini-2.5-flash`
    - `models/gemini-2.5-pro`
    - `models/gemini-3.1-pro-preview`
  - one real generation path succeeded:
    - `POST /v1beta/models/gemini-2.5-flash:generateContent`
    - prompt: `Reply with exactly GEMINI_CANARY_OK and nothing else.`
    - exact response text: `GEMINI_CANARY_OK`
- redaction evidence:
  - `deploy/gemini-preflight.sh` completed successfully and would have failed on any leaked `access_token` / `refresh_token` / `api_key` / `service_account_json` / `private_key`
  - observed admin `health` / `verify` responses did not expose bearer secrets

## Health Snapshot

- `/api/v1/admin/gemini/health`
  - `gateway_status=healthy`
  - `oauth_status=healthy`
  - `gemini_accounts_total=1`
  - `accounts_by_family.google_one=1`
  - `policy.production_mode=true`
  - `policy.token_cache_mode=encrypted`
  - `policy.session_store=redis`
  - `policy.project_id_fallback_to_ai_studio=false`
  - `policy.unauthorized_client_retry_fallback=false`
  - `policy.google_one_default_tier_fallback=true`
  - `policy.google_one_default_tier_visible=true`
  - `policy.thought_signature_session_safety=true`
  - `warning_codes=[]`

## Verify Snapshot

- `/api/v1/admin/gemini/verify?account_id=3`
  - `account_name=Gemini OAuth Canary`
  - `runtime_contract.account_family=google_one`
  - `runtime_contract.upstream_family=code_assist`
  - `runtime_contract.supports_thought_signature=true`
  - `project_id_status=present`
  - `tier_status=present`
  - `token_cache_state=ok`
  - `oauth_state=ok`
  - `session_store=redis`
  - `sticky_session_safety_required=true`

## Stop Conditions

- `gateway_status=degraded` with unknown reason
- `invalid_runtime_contract`
- `project_id_status=required_missing`
- `token_cache_state=degraded`
- `oauth_state=degraded` with unknown reason
- any secret leakage in admin or public probe output

## Final Signoff Checklist

- health snapshot captured
- verify snapshot captured
- runtime contract matches expected account family
- required `project_id` present where needed
- tier state is expected
- token-cache state is production-safe
- session store / sticky-session safety state is acceptable
- smoke request succeeded
- redaction evidence captured

## Final Result

- Real Gemini OAuth live canary succeeded locally on May 10, 2026.
- Gemini v0.3 hardening objectives validated in the live path:
  - real Google OAuth exchange
  - real account persistence
  - production-safe Gemini policy surface
  - real public model listing
  - real content generation
- Remaining work is operational, not implementation:
  - execute the same canary on a remote preprod / production-like host
  - optionally add a second canary for `code_assist` family if that operator path will be used in production
