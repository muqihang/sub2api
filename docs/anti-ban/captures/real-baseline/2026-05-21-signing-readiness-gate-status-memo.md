# 2026-05-21 signing readiness gate status memo

## Recommendation

Do **not** start `docs/anti-ban/27-final-shared-pool-signing-mode-design.md` yet.

Checkpoint 5 closes the local code-boundary and joint-capture work, and one P0 gate remains in `DEFER`:

- `P0-A count_tokens`: Linux localhost attempts still did not naturally emit `/v1/messages/count_tokens`, so the route remains blocked/deferred and excluded from first-wave shared-pool canary.
- `P0-C metadata/session`: PASS via Linux localhost capture for default persistence, `-c/--continue`, explicit `--resume`, and explicit `--session-id`; `stream-json` is treated as output-side here because the Linux localhost request shape matched JSON output.
- `P0-D Linux parity`: PASS via Linux localhost capture on Ubuntu 24.04.4 x86_64.

## Gates that are now supported by evidence

- `P0-B refresh`: PASS via static audit + service-local mock only.
- `P0-C metadata/session`: PASS via Linux localhost capture plus static CLI triage.
- `P0-D Linux parity`: PASS via Linux localhost capture on Ubuntu 24.04.4 x86_64.
- `P0-E event route-family policy`: PASS with local suppress/block policy.
- `P0-F Sub2API boundary`: PASS.
- `P0-G CC Gateway final-output boundary`: PASS.
- `P0-H canonical 2.1.146 persona lock`: PASS.
- `P0-I CCH and cc_version fixtures`: PASS.
- `P0-J API-key passthrough include/block/defer`: PASS (`/v1/messages` included; `/v1/messages/count_tokens` deferred).
- `P0-K joint local capture`: PASS.

## Evidence index

- `captures/real-baseline/2026-05-21-p0-c-session-static-triage/safe-deliverable/README.md`
- `captures/real-baseline/2026-05-21-p0-a-count-tokens-static-triage/safe-deliverable/README.md`
- `captures/real-baseline/2026-05-21-p0-d-linux-parity-local-probe/safe-deliverable/linux_parity_local_probe_summary.md`
- `captures/real-baseline/2026-05-21-p0-c-session-lifecycle-linux-local-probe/safe-deliverable/session_lifecycle_linux_local_probe_summary.md`
- `captures/real-baseline/2026-05-21-p0-a-count-tokens-linux-local-probe/safe-deliverable/count_tokens_linux_local_probe_summary.md`
- `captures/real-baseline/2026-05-21-p0-a-count-tokens-local-defer/safe-deliverable/README.md`
- `captures/real-baseline/2026-05-21-p0-c-session-lifecycle-local-defer/safe-deliverable/README.md`
- `captures/real-baseline/2026-05-21-p0-d-linux-parity-runbook/safe-deliverable/README.md`
- `docs/anti-ban/captures/real-baseline/2026-05-21-claude-code-2146-count-tokens-local-probe/safe-deliverable/count_tokens_local_probe_summary.md`
- `docs/anti-ban/captures/real-baseline/2026-05-21-claude-code-2146-oauth-refresh-static-and-local-mock-audit/safe-deliverable/oauth_refresh_static_local_mock_summary.md`
- `docs/anti-ban/captures/real-baseline/2026-05-21-claude-code-2146-session-lifecycle-local-probe/safe-deliverable/session_lifecycle_summary.md`
- `docs/anti-ban/captures/real-baseline/2026-05-21-claude-code-2146-linux-parity-local-probe/safe-deliverable/linux_parity_summary.md`
- `docs/anti-ban/captures/real-baseline/2026-05-21-claude-code-2146-cch-cc-version-local-fixtures/safe-deliverable/cch_cc_version_fixture_summary.md`
- `docs/anti-ban/captures/real-baseline/2026-05-21-sub2api-cc-gateway-joint-local-capture/safe-deliverable/README.md`
- `docs/anti-ban/captures/real-baseline/2026-05-21-sub2api-cc-gateway-joint-local-capture/safe-deliverable/joint_local_capture_summary.redacted.json`

## Server rerun note

- On `2026-05-21`, the shared-pool server at `107.215.140.93` was upgraded from Node.js 18.19.1 to 22.22.2.
- `npm run build` and `npm test -- --runInBand` on CC Gateway passed after the upgrade, and the earlier Node 18 engine warnings for `https-proxy-agent@9` / `agent-base@9` no longer appeared in the rerun.
- The server-local Sub2API harness was rerun with `CC_GATEWAY_REPO_ROOT=/root/shared-pool-local-integration-2026-05-21/cc-gateway`; both the joint local capture artifact test and the route/control-plane regression set passed again.

## 2026-05-22 canary-prep environment check

- Hostname: `xeelee`
- `hostname -I`: `107.215.140.93 172.17.0.1 172.18.0.1`
- External IP observed from the host: `107.215.140.93`
- `node -v`: `v22.22.2`
- `npm -v`: `10.9.7`
- This was a read-only check only; it did not touch Anthropic upstream or any real Claude account.

## Required next actions

1. Produce a real `2.1.146` localhost `count_tokens` fixture or formally keep the route blocked/deferred in first-wave scope.
2. Keep the Linux P0-A note and static triage in the audit trail so first-wave count_tokens exclusion stays explicit.
3. Keep Linux P0-C session evidence current if CLI option semantics change; current evidence covers explicit `--resume` / `--session-id` and shows `stream-json` is output-side in this path.
4. Re-run Linux parity if the deployment host, runtime, or canonical persona source changes.

## Next-step recommendation

Do **not** start the full final signing-mode design yet. However, the evidence now supports drafting a constrained `27-first-wave-shared-pool-messages-only-design.md`, because the remaining open gate (`P0-A count_tokens`) is explicitly blocked/deferred outside first-wave scope.
