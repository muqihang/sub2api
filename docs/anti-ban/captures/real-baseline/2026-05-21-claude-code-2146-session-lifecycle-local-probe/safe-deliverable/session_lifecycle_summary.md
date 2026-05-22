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
- session_header_hashes: `sha256:826432dfca19be4dd0169df1d925ae8d5303f9e23e0df45d2b9bcf2659391374`
- metadata_user_id_hashes: `sha256:93ab922d450eec08a506ee7851ad2673b837c872360163fa9b20ec4de3dff8b4`
- metadata_user_id_fields: `[('account_uuid', 'device_id', 'session_id')]`
- stream_values: `[True, True]`
- retry_counts: `['0', '0']`
- billing_block_present: `[True, True]` / cch_present: `[True, True]`

### default_session_json_first
- exit/timed_out: `0` / `False`
- request_count: `2`
- paths: `/v1/messages?beta=true, /v1/messages?beta=true`
- session_header_hashes: `sha256:bf0dd2ee86562564764e270ef6ce7218c4528649d121ad01a77bf26bf7a81bbc`
- metadata_user_id_hashes: `sha256:4b8737a7c07bea89cd8bbea897d6320003b1738b2f894adbd5e82a858277b7fc`
- metadata_user_id_fields: `[('account_uuid', 'device_id', 'session_id')]`
- stream_values: `[True, True]`
- retry_counts: `['0', '0']`
- billing_block_present: `[True, True]` / cch_present: `[True, True]`

### default_session_continue_json
- exit/timed_out: `0` / `False`
- request_count: `1`
- paths: `/v1/messages?beta=true`
- session_header_hashes: `sha256:bf0dd2ee86562564764e270ef6ce7218c4528649d121ad01a77bf26bf7a81bbc`
- metadata_user_id_hashes: `sha256:4b8737a7c07bea89cd8bbea897d6320003b1738b2f894adbd5e82a858277b7fc`
- metadata_user_id_fields: `[('account_uuid', 'device_id', 'session_id')]`
- stream_values: `[True]`
- retry_counts: `['0']`
- billing_block_present: `[True]` / cch_present: `[True]`

### no_session_stream_json
- exit/timed_out: `1` / `False`
- request_count: `0`
- paths: ``
- session_header_hashes: ``
- metadata_user_id_hashes: ``
- metadata_user_id_fields: `[]`
- stream_values: `[]`
- retry_counts: `[]`
- billing_block_present: `[]` / cch_present: `[]`

### error_once_default_json
- exit/timed_out: `0` / `False`
- request_count: `2`
- paths: `/v1/messages?beta=true, /v1/messages?beta=true`
- session_header_hashes: `sha256:4742a1da9e622cf0df3a9382e3f43099cc6c3517224ae0935d9dfd6e875fafc9`
- metadata_user_id_hashes: `sha256:497dc940274ac798b2cfa5c0d10a7b4255f6a0ef58896a2cc55c5abe26396c26`
- metadata_user_id_fields: `[('account_uuid', 'device_id', 'session_id')]`
- stream_values: `[True, True]`
- retry_counts: `['0', '0']`
- billing_block_present: `[True, True]` / cch_present: `[True, True]`

> Safe deliverable stores only hashes, booleans, field names, and key-order summaries. No raw prompt text, raw bodies, raw Authorization, raw UUIDs, or raw CCH are included.
