# Production upstream error incident - 2026-07-22

## Scope

- Production gateway: `www.5566676.xyz`
- Observation window: 2026-07-21 19:53 to 2026-07-22 19:53 (UTC+8)
- Sources: `ops_error_logs`, nested `upstream_errors`, channel-monitor history,
  gateway logs, and direct account connection tests
- Deployment status: no application hot deploy was performed

## Summary

The 24-hour upstream-error view contained 3,619 request-level records. Of these,
2,739 were recovered by failover and 880 were not recovered. Expanding each
record's nested retry/failover events showed that most failures were repeated
attempts against accounts which were still marked schedulable despite exhausted
balance, deleted groups, missing group permission, or persistent transport
timeouts.

The largest nested failure buckets were:

| Account | Failure | Attempts | Requests |
| --- | --- | ---: | ---: |
| 1 `浮生` | upstream precharge balance insufficient | 11,264 | 2,823 |
| 40 `浮生-kiro- Claude-0，2` | no permission for Kiro group | 1,040 | 261 |
| 38 `特价0.1 claude` | upstream API-key group deleted | 948 | 237 |
| 179 `小白kiro-0.12` | insufficient balance | 759 | 192 |
| 177 `小白-0.035` | insufficient balance | 575 | 575 |
| 214 `小白0.05kiro` | insufficient balance | 386 | 97 |
| 211 `帅气-0.08` | insufficient balance | 377 | 377 |
| 213 `帅哥kiro` | insufficient balance | 272 | 68 |
| 22 `反重力` | timeout/context cancellation | 187 | 187 |

The request-level status distribution was 3,311 HTTP 403, 229 HTTP 502, 54
HTTP 503, 15 HTTP 524, and a small remainder of 400/429/521/recovered-200
events. The dominant 502 source was account 22: 171 request-level failures in
the initial snapshot, all caused by the upstream request being canceled after
the monitor's response-header timeout.

## Channel monitor defect

The Anthropic monitor used `max_tokens=50` and extracted only
`content[0].text`. A production Haiku 4.5 response placed a `thinking` block at
index 0 and the answer in a `text` block at index 1, so the monitor recorded an
empty response and `challenge mismatch`. With `max_tokens=1024`, the same
request completed in 6.7 seconds and returned the expected answer.

The code fix now:

- uses an Anthropic-specific challenge output limit of 1,024 tokens;
- scans all Anthropic content blocks and aggregates `type=text` blocks;
- ignores leading thinking/tool blocks;
- includes a regression test for the observed production response shape.

## Production action

Direct tests confirmed the following accounts were still failing, so they were
made explicitly unschedulable:

| Account | Direct-test result |
| --- | --- |
| 1 `浮生` | 403, upstream precharge quota exhausted |
| 22 `反重力` | no response within 90 seconds |
| 38 `特价0.1 claude` | 403, API-key group deleted |
| 40 `浮生-kiro- Claude-0，2` | 403, no Kiro-group permission |
| 177 `小白-0.035` | 403, insufficient balance |
| 179 `小白kiro-0.12` | 403, insufficient balance |
| 211 `帅气-0.08` | 403, insufficient balance |
| 213 `帅哥kiro` | 403, insufficient balance |
| 214 `小白0.05kiro` | 403, insufficient balance |

Accounts 176 `禾` and 210 `禾2` passed current `gpt-5.6-luna` direct tests and
remained schedulable. Their older 503 events were upstream model-availability
failures for models such as GPT-5.2 and `codex-auto-review`, not current Luna
connectivity failures.

After the scheduler change, the OpenAI channel monitor returned operational in
1.3-1.9 seconds. Both Claude pools had no healthy accounts left and returned a
clear 503 in about 20 ms instead of timing out after 30-90 seconds.

## Recovery criteria

Do not re-enable an affected account solely because its status field was
cleared. Re-enable it only after a fresh direct test of the production model
succeeds. For Claude accounts, verify a non-streaming `/v1/messages` request in
addition to the UI's streaming account test. After at least one Claude account
passes, restore its schedulable flag and confirm the corresponding group API key
before re-enabling normal monitor expectations.
