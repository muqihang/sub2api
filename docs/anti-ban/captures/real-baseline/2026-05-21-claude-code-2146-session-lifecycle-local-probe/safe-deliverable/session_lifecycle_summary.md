# Claude Code 2.1.146 session lifecycle local probe

- Status: `PARTIAL`
- Covered: `--no-session-persistence`, default persistence first turn, `-c/--continue`, local 500-once error path.
- Not covered: explicit `--resume` / `--session-id`.
- Attempted but not covered: `stream-json`.

## Scenario summaries

### no_session_json
- exit/timed_out: `0` / `False`
- request_count: `2`
- paths: `/v1/messages?beta=true, /v1/messages?beta=true`
digest_omitted_by_policy: true
digest_omitted_by_policy: true
- metadata_user_id_fields: `[('account_uuid', 'device_id', 'session_id')]`
- stream_values: `[True, True]`
- retry_counts: `['0', '0']`
- billing_block_present: `[True, True]` / cch_present: `[True, True]`

### default_session_json_first
- exit/timed_out: `0` / `False`
- request_count: `2`
- paths: `/v1/messages?beta=true, /v1/messages?beta=true`
digest_omitted_by_policy: true
digest_omitted_by_policy: true
- metadata_user_id_fields: `[('account_uuid', 'device_id', 'session_id')]`
- stream_values: `[True, True]`
- retry_counts: `['0', '0']`
- billing_block_present: `[True, True]` / cch_present: `[True, True]`

### default_session_continue_json
- exit/timed_out: `0` / `False`
- request_count: `1`
- paths: `/v1/messages?beta=true`
digest_omitted_by_policy: true
digest_omitted_by_policy: true
- metadata_user_id_fields: `[('account_uuid', 'device_id', 'session_id')]`
- stream_values: `[True]`
- retry_counts: `['0']`
- billing_block_present: `[True]` / cch_present: `[True]`

### no_session_stream_json
- exit/timed_out: `1` / `False`
- request_count: `0`
- paths: ``
digest_omitted_by_policy: true
digest_omitted_by_policy: true
- metadata_user_id_fields: `[]`
- stream_values: `[]`
- retry_counts: `[]`
- billing_block_present: `[]` / cch_present: `[]`

### error_once_default_json
- exit/timed_out: `0` / `False`
- request_count: `2`
- paths: `/v1/messages?beta=true, /v1/messages?beta=true`
digest_omitted_by_policy: true
digest_omitted_by_policy: true
- metadata_user_id_fields: `[('account_uuid', 'device_id', 'session_id')]`
- stream_values: `[True, True]`
- retry_counts: `['0', '0']`
- billing_block_present: `[True, True]` / cch_present: `[True, True]`

digest_omitted_by_policy: true
