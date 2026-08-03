# Claude Code 2.1.175 final persona local smoke

Date: 2026-06-13
Scope: Phase B Checkpoint 4, localhost/mock only. No production deployment and no real Anthropic upstream request were performed.

## Inputs

- Sub2API worktree: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-2173-main`
- Sub2API commit under test: `4aa656c3477f305415dd0bb48872fac6d2b06b86`
- CC Gateway worktree: `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main`
- CC Gateway commit under test: `9df1890acde33590ce41b887f8542c5e485aeda4`
- Target persona: Claude Code `2.1.175`

## Commands run

Sub2API:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-2173-main/backend
go build ./cmd/server
CC_GATEWAY_REPO_ROOT=/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main \
  go test ./internal/service -run '^TestJointLocalCaptureAcceptanceArtifact$' -count=1 -timeout=120s
```

Result: PASS.

CC Gateway:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main
npm run build
npx tsx tests/checkpoint3-remediation.test.ts
npx tsx tests/policy-cch.test.ts
npx tsx tests/persona-resolver.test.ts
```

Result: PASS (`checkpoint3-remediation`: 31 passed, `policy-cch`: 11 passed, `persona-resolver`: 12 passed, TypeScript build passed).

## Local smoke assertions

The Sub2API joint harness started local CC Gateway processes from the 2.1.175 worktree and a localhost upstream capture server. The generated safe deliverable is committed under:

- `docs/anti-ban/captures/real-baseline/2026-06-13-sub2api-cc-gateway-joint-local-capture/safe-deliverable/README.md`
- `docs/anti-ban/captures/real-baseline/2026-06-13-sub2api-cc-gateway-joint-local-capture/safe-deliverable/joint_local_capture_summary.redacted.json`

Safe summary results:

- Scenario count: 19 / 19 PASS.
- No real upstream: true.
- No native fallback: true.
- No raw secrets in safe deliverable: true.
- Negative control-plane cases fail closed: true.
- Sub2API-to-CC-Gateway policy version: `2.1.175`.
- Final CC Gateway User-Agent shape: `claude-cli/2.1.175 (external, sdk-cli)`.
- Sign-primary path generated a billing block with `cc_version=2.1.175.<3hex>` and CCH present, and the CC Gateway post-sign verifier passed before forwarding to localhost upstream.
- Strip path removed downstream billing/CCH before localhost upstream.
- The joint harness includes dedicated sign-primary localhost shape scenarios for both `claude-opus-4-8` and `claude-fable-5`; both reached localhost upstream with CC Gateway-owned billing/CCH and the expected 2.1.175 persona. The CC Gateway signer/verifier tests also verify normalized 2.1.175 CCH for these model shapes.

## Safety notes

- This smoke used localhost/mock upstream only; it does not prove real upstream entitlement for `claude-opus-4-8` or any other model.
- Production canary still requires a separately authorized, low-token real upstream request.
- The safe deliverable may include HTTP header key names such as `authorization`, but secret values are redacted scoped refs. It does not include raw token values, raw prompts, raw request bodies, observed raw CCH values, cookies, account email/UUID, or API keys.
