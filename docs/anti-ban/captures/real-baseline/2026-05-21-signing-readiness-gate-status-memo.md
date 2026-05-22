# 2026-05-21 signing readiness gate status memo

## Recommendation

Do **not** start `docs/anti-ban/27-final-shared-pool-signing-mode-design.md` yet.

Checkpoint 5 closes the local code-boundary and joint-capture work, but three P0 gates remain in `DEFER`:

- `P0-A count_tokens`: `count_tokens` did not naturally emit in the official `2.1.146` localhost probe, so `/v1/messages/count_tokens` remains blocked/deferred and excluded from first-wave shared-pool canary.
- `P0-C metadata/session`: no-session/default/continue/stream-json/error-once paths are covered, but explicit `--resume` / `--session-id` lifecycle behavior is still excluded from first-wave scope.
- `P0-D Linux parity`: no Linux/deployment-like host was available, so Linux shared-pool deployment/persona parity claims remain blocked.

## Gates that are now supported by evidence

- `P0-B refresh`: PASS via static audit + service-local mock only.
- `P0-E event route-family policy`: PASS with local suppress/block policy.
- `P0-F Sub2API boundary`: PASS.
- `P0-G CC Gateway final-output boundary`: PASS.
- `P0-H canonical 2.1.146 persona lock`: PASS.
- `P0-I CCH and cc_version fixtures`: PASS.
- `P0-J API-key passthrough include/block/defer`: PASS (`/v1/messages` included; `/v1/messages/count_tokens` deferred).
- `P0-K joint local capture`: PASS.

## Evidence index

- `docs/anti-ban/captures/real-baseline/2026-05-21-claude-code-2146-count-tokens-local-probe/safe-deliverable/count_tokens_local_probe_summary.md`
- `docs/anti-ban/captures/real-baseline/2026-05-21-claude-code-2146-oauth-refresh-static-and-local-mock-audit/safe-deliverable/oauth_refresh_static_local_mock_summary.md`
- `docs/anti-ban/captures/real-baseline/2026-05-21-claude-code-2146-session-lifecycle-local-probe/safe-deliverable/session_lifecycle_summary.md`
- `docs/anti-ban/captures/real-baseline/2026-05-21-claude-code-2146-linux-parity-local-probe/safe-deliverable/linux_parity_summary.md`
- `docs/anti-ban/captures/real-baseline/2026-05-21-claude-code-2146-cch-cc-version-local-fixtures/safe-deliverable/cch_cc_version_fixture_summary.md`
- `docs/anti-ban/captures/real-baseline/2026-05-21-sub2api-cc-gateway-joint-local-capture/safe-deliverable/README.md`
- `docs/anti-ban/captures/real-baseline/2026-05-21-sub2api-cc-gateway-joint-local-capture/safe-deliverable/joint_local_capture_summary.redacted.json`

## Required next actions

1. Produce a real `2.1.146` localhost `count_tokens` fixture or formally keep the route blocked/deferred in first-wave scope.
2. Add explicit `--resume` / `--session-id` lifecycle capture, or formally exclude those flows from first-wave scope.
3. Run Linux/deployment-like localhost parity capture before any Linux shared-pool deployment claim.
