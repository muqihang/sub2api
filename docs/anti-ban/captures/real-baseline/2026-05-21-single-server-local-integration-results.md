# 2026-05-21 single-server local integration memo

## Summary
- Server: Ubuntu 24.04.4 LTS x86_64 at `107.215.140.93`.
- CC Gateway: built/tested on the server; local-only `sign-primary` pipeline now passes the server-local integration harness.
- Sub2API: backend joint local capture artifact test passed on the server and produced a redacted safe deliverable.
- No real Anthropic upstream, no login, no MITM, no raw secrets in safe deliverables.
- Server Node.js was upgraded to 22.22.2; the prior Node 18.19.1 engine warnings for `https-proxy-agent@9` / `agent-base@9` no longer appeared in the rerun build/test path.

## Node 22 rerun
- `npm run build` passed on the server under Node 22.22.2.
- `npm test -- --runInBand` passed on the server under Node 22.22.2.
- `go test ./internal/service -run TestJointLocalCaptureAcceptanceArtifact -count=1` passed with `CC_GATEWAY_REPO_ROOT=/root/shared-pool-local-integration-2026-05-21/cc-gateway`.
- `go test ./internal/service ./internal/server/routes -run 'CCGateway|ControlPlane|EventLogging|LocalCapture' -count=1` passed with the same `CC_GATEWAY_REPO_ROOT`.
- The rerun kept the same no-real-upstream / no-login / no-MITM boundary and did not introduce raw secrets into the safe deliverables.

## What passed
- `sign-primary` `/v1/messages` baseline: CC Gateway generated billing block, `cc_version`, CCH re-sign, and post-sign verification before localhost upstream capture.
- `strip-controlled` `/v1/messages` baseline: CC Gateway stripped billing/CCH material and preserved final-output ownership.
- `count_tokens`: blocked/deferred.
- `event_logging`: suppress/block only; no upstream forward.
- Unknown routes and control-plane failures: fail closed.
- Rollback to `billing_cch_mode=disabled`: fail closed with no native fallback.

## Safe deliverable
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/docs/anti-ban/captures/real-baseline/2026-05-21-single-server-local-integration/safe-deliverable/README.md`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/docs/anti-ban/captures/real-baseline/2026-05-21-single-server-local-integration/safe-deliverable/single_server_local_integration_summary.redacted.json`

## Notes
- The underlying acceptance harness also contains legacy OpenAI-compatible Anthropic conversion regression coverage, but that is out of first-wave messages-only scope and is excluded from the summary above.
- The server-side Sub2API joint capture run and CC Gateway tests both passed.
