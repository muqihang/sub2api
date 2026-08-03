# 64 - Formal-pool Observed Version Floor + Canonical Upstream Identity Lock

## Objective

Allow native Claude Code clients with observed safe versions `>= 2.1.179` to enter formal-pool strip-attribution admission, while keeping the final upstream Anthropic identity locked to the server-selected canonical `2.1.179` profile.

## Safety Rules

- Observed client version is admission/audit evidence only.
- Formal-pool Anthropic upstream identity must not follow each end user's local Claude Code version.
- `unknown`, `latest`, unparseable, and `< 2.1.179` observed versions fail closed for formal-pool messages admission.
- `no_cch`, `signed_cch`, and strict native parity remain exact-oracle gated; `>= 2.1.179` does not promote them.
- Server-selected authority fields remain canonical:
  - `policy_version = 2.1.179`
  - server persona/profile refs
  - request-shape/cache parity refs
  - billing policy `strip`
  - egress TLS profile ref as server authority/plumbing only
- Client-supplied `User-Agent`, beta, billing/CCH, TLS hints, and profile refs are never forwarded as authority.

## Implementation Notes

### CC Gateway

- Added `FORMAL_POOL_OBSERVED_MIN_CLI_VERSION = "2.1.179"`.
- `strip_attribution` now requires parseable semver `>= 2.1.179`.
- Future/newer parseable versions are admitted only as observed strip inputs.
- Final upstream checks assert canonical `claude-cli/2.1.179 ...`, stable beta set, and no billing/CCH residue.

### Sub2API

- Formal-pool policy header is locked to canonical `2.1.179` from server account state, not local trusted/canary client context.
- AWS formal-pool CC Gateway path now captures observed version from raw inbound headers before sanitizer strips authority headers.
- Sanitized headers are still used for forwarded/request authority; raw inbound headers are only used for observed audit/admission.

## Verification Summary

Sub2API:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/backend
go test ./internal/service -run 'TestGatewayService_CCGatewayFormalPool(LocksCanonicalPolicyWhenContextVersionIsDifferent|AdmitsNewerObservedVersionOnlyAsCanonicalStrip|ContextCarriesServerSelected2179ProfileRefs|IgnoresBodyAuthorityHints)|TestGatewayService_ClaudePlatformAWSFormalPoolObservedVersionUsesRawClientHeadersBeforeSanitize|TestClaudePlatformAWSLocalFullChainE2EUsesCCGatewayAndSafeMockUpstream|TestGatewayService_ForwardClaudePlatformAWSFormalPoolUsesCCGatewayWithoutAccountProxy' -count=1
go test ./internal/service -run 'CCGateway|FormalPool|Boundary|NoBypass|Spoof|ObservedProfile|TLSProfile' -count=1
go test ./internal/repository -count=1
```

CC Gateway:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5
npx tsx tests/claude-code-future-version-compat.test.ts
npx tsx tests/proxy-sub2api.test.ts
npx tsx tests/session-and-beta-policy.test.ts
npx tsx tests/canary-cost-envelope.test.ts
npx tsx tests/egress-tls-profile.test.ts
npx tsx tests/egress-tls-sidecar.test.ts
npx tsx tests/config.test.ts
npx tsx tests/preflight-safety.test.ts
npx tsx tests/security-boundary.test.ts
npx tsx tests/claude-platform-aws-cp5.test.ts
npx tsx tests/claude-platform-aws-cp7-sigv4.test.ts
npx tsx tests/checkpoint3-remediation.test.ts
npx tsc --noEmit
npm test
```

Result: PASS locally. Sub2API CodeGraph index refreshed and up to date. CC Gateway worktree has no `.codegraph/` directory, so no CodeGraph sync was performed there.

## Deployment Gate

This patch is ready for commit/build. Production rollout should still follow the existing paired deployment flow: build Sub2API + CC Gateway together, verify on 18081 with mock/canary, then replace 18080 only after paired equivalence is confirmed.
