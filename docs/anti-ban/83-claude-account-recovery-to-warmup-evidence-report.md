# Plan83 Claude Account Recovery to Warmup Evidence Report

Date: 2026-07-03 UTC

## Final decision

`BLOCKED_UPSTREAM_AUTH_RATE_LIMIT_MANUAL_REVIEW_NO_WARMING`

No account was moved to warming, because no candidate reached the required formal-pool `healthcheck_passed` stage. The run followed the supported production workflow and did not bypass the formal-pool evidence gate.

## Scope

Requested account set:

- Claude/Anthropic formal-pool accounts in quarantined repair state with last use/login activity bucket `lt_10d`.
- Claude/Anthropic accounts with name prefix bucket `0610*`.

Safe candidate summary:

| Bucket | Count |
| --- | ---: |
| Candidate union | 42 |
| Recent quarantined bucket | 42 |
| `0610*` bucket | 2 |
| Candidate type `setup-token` with proxy | 32 |
| Candidate type `oauth` with proxy | 10 |
| Candidate last-used bucket `lt_10d` | 42 |

No raw account IDs, credentials, account names, tokens, prompts, request bodies, response bodies, DB URLs, or Redis URLs are recorded in this report.

## Production baseline and backup

| Check | Result |
| --- | --- |
| `18080` health before | PASS |
| CC Gateway `18443` health before | PASS |
| Sidecar `19484` health before | PASS |
| PostgreSQL backup | PASS |
| Redis backup | PASS |

Backup safe summary:

| Backup | Size | SHA256 |
| --- | ---: | --- |
| PostgreSQL dump gzip | 69,872,108 bytes | `a15773ff4a6882f22822b94bd9529d21554e2f135b7901f4e40188d446d52ab7` |
| Redis RDB copy | 1,293,780 bytes | `7e0a49d7645c9a884fa0faacf24ec4cbc89248845874884dad97b51f0e721ce2` |

Backup path is on the production server under the Plan83 backup bucket; the full path is intentionally not expanded with secrets or data contents. No DB/Redis destructive operation was executed.

## Workflow executed

Supported formal-pool workflow confirmed from code:

1. `runtime-register`
2. `healthcheck`
3. `start-warming`

`start-warming` requires persisted runtime evidence plus healthcheck evidence and `onboarding_stage=healthcheck_passed`. The run did not directly update SQL to set accounts to warming.

A short-lived admin JWT was generated in server scratch and used only for loopback admin API calls. It was redacted after use. Candidate raw ID scratch was also redacted after use.

## Round 1: runtime register + healthcheck + start-warming gate

| Metric | Count |
| --- | ---: |
| Candidates | 42 |
| Runtime-register success | 41 |
| Runtime-register failure | 1 |
| Healthcheck success HTTP bucket | 41 |
| Accounts reaching `healthcheck_passed` | 0 |
| Start-warming attempts | 0 |
| Start-warming success | 0 |
| Auth failures calling admin API | 0 |
| Transport errors | 0 |

Stage buckets after round 1:

| Bucket | Count |
| --- | ---: |
| `runtime-register:runtime_registered` | 41 |
| `runtime-register:quarantined` | 1 |
| `healthcheck:quarantined` | 41 |

## Diagnostics after round 1

Diagnostics remained quarantined and recommended remediation rather than warming:

| Recommendation bucket | Count |
| --- | ---: |
| `manual_review` | 32 |
| `repair_token` | 31 |
| `healthcheck` | 31 |
| `wait_rate_limit` | 10 |
| `runtime_register` | 1 |

Evidence buckets:

| Evidence bucket | Count |
| --- | ---: |
| `runtime=True | health=None | raw=None | cc=True` | 41 |
| `runtime=False | health=None | raw=True | cc=True` | 1 |

The system did not recommend `start_warming` for any candidate.

## Round 2: official refresh-login-state path for `repair_token` recommendations

The UI-backed `refreshLoginState` flow maps to `POST /api/v1/admin/accounts/:id/refresh` followed by diagnostics. It was attempted only for accounts with the `repair_token` recommendation bucket.

| Metric | Count |
| --- | ---: |
| Repair-token candidates attempted | 31 |
| Refresh success | 4 |
| Refresh fail | 27 |
| Runtime-register success after refresh | 4 |
| Healthcheck success HTTP bucket after refresh | 4 |
| Accounts reaching `healthcheck_passed` after refresh | 0 |
| Start-warming attempts | 0 |
| Start-warming success | 0 |
| Skipped because no `repair_token` recommendation | 11 |
| Transport errors | 0 |

Refresh failures are recorded only as safe error buckets; raw upstream responses were not stored.

## Final after-state

| Check | Result |
| --- | --- |
| `18080` health after | PASS |
| CC Gateway `18443` health after | PASS |
| Sidecar `19484` health after | PASS |
| PostgreSQL health after | PASS |
| Redis health after | PASS |

Candidate after-state buckets:

| Bucket | Count |
| --- | ---: |
| `stage=quarantined | status=error | sched=false | health=quarantined | runtime=true` | 41 |
| `stage=quarantined | status=error | sched=false | health=pending | runtime=false` | 1 |
| `0610* stage=quarantined | status=error | sched=false | health=quarantined` | 2 |

Safe failure buckets:

| Safe failure bucket | Count |
| --- | ---: |
| `origin=upstream | last=formal_pool_healthcheck_failed | health_code=forbidden | health_bucket=auth | status_bucket=status_403` | 19 |
| `origin=upstream | last=formal_pool_healthcheck_failed | health_code=forbidden | health_bucket=auth | status_bucket=status_403 | rate_class=long_context_usage_credits | rate_action=pass_through` | 11 |
| `origin=upstream | last=formal_pool_healthcheck_failed | health_code=forbidden | health_bucket=auth | status_bucket=status_403 | rate_class=rate_limited | rate_action=rate_limited` | 10 |
| `origin=cc_gateway_control_plane | last=runtime_registration_failed | health_code=auth | health_bucket=auth | status_bucket=status_401` | 1 |
| `origin=upstream | last=formal_pool_healthcheck_failed | health_code=forbidden | health_bucket=auth | status_bucket=status_403 | rate_class=unknown | rate_action=pass_through` | 1 |

## Interpretation

The production infrastructure path is healthy, but the candidate accounts did not pass the formal-pool healthcheck gate. The dominant blockers are upstream auth/403 and rate-limit/long-context windows, plus one runtime-registration/auth bucket. Because the system still classifies the accounts as quarantined and recommends manual review / token repair / wait-rate-limit, pushing them to warming would bypass the formal evidence gate and was not performed.

## Safety confirmations

- No direct SQL state promotion to warming.
- No raw account IDs persisted in repo/docs/evidence.
- No raw credentials, JWT, secrets, DB URL, Redis URL, prompt/body/response, or upstream body recorded.
- PostgreSQL and Redis backups completed before the operation.
- No DB/Redis destructive operation executed.
- Production `18080` remained healthy.
- CC Gateway and production TLS sidecar remained healthy.
- Short-lived admin JWT and raw candidate ID scratch files were redacted after use.
- Scratch cleanup status: `skipped_requires_user_approval`; sensitive scratch files redacted in place.

## Required follow-up

To move these accounts to warming, the next safe actions are account-specific and cannot be automated from the current evidence:

1. For `repair_token` failures: replace/re-authorize the upstream login state or setup token, then rerun runtime-register and healthcheck.
2. For `wait_rate_limit` buckets: wait for the cooldown/window to recover, then rerun healthcheck.
3. For `manual_review` buckets: manually verify upstream hold/KYC/risk state before any further automated healthchecks.
4. For the single runtime-registration/auth bucket: inspect CC Gateway/account runtime diagnostics without exposing raw credentials, then rerun runtime-register.
