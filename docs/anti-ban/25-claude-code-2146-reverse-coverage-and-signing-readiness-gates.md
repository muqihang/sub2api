# 25 - Claude Code 2.1.146 Reverse Coverage and Signing Readiness Gates

> **Status:** Checkpoint 5 snapshot updated on 2026-05-21. Final signing-mode design is not yet approved.
> **Source audits:** `audits/2026-05-21-shared-pool-signing-pre-design/A-claude-code-2146-reverse-eng-coverage-audit.md` and `audits/2026-05-21-shared-pool-signing-pre-design/B-sub2api-cc-gateway-signing-readiness-audit.md`.
> **Companion docs:** `14-cc-gateway-shared-pool-compatibility-plan.md`, `20-cch-cc-version-stability-regression.md`, `22-scheduler-state-model-and-distributed-consistency.md`, `24-disaster-recovery-and-policy-rollout.md`.
> **Decision:** Current evidence is enough for minimal messages-only no-CCH discussion and local/joint canary preparation. It is **not** enough to declare endpoint-complete, lifecycle-complete, Linux-deployable shared-pool signing readiness.
> **Fail-closed rule:** If any MUST gate below is open, do not run shared-pool signing canary. Continue with strip/no-CCH only where doc 16 evidence applies, or halt the affected route/cohort.

---

## 1. What the two audits add

### A audit: Claude Code 2.1.146 reverse coverage

Current 2.1.146 evidence is strongest for `/v1/messages?beta=true` under local capture and one minimal no-CCH real upstream acceptance. The audit adds that the strongest shared-pool signing design still lacks dynamic or version-current coverage for:

- `count_tokens`;
- event logging;
- OAuth token refresh;
- session lifecycle and rotation;
- retry/error behavior;
- SSE response/follow-up behavior;
- auxiliary endpoints;
- Linux-vs-Mac host parity;
- transport fingerprint rerun for 2.1.146.

### B audit: Sub2API + CC Gateway signing readiness

Current code is not signing-ready. The audit identifies three top blockers:

1. CC Gateway does not yet have a final-output signing pipeline: billing block emit/normalize, byte-stable final serialization, body CCH signer/verifier, post-sign immutability, fail-closed verification, and `strip|sign` mode switch.
2. CC Gateway does not yet have per-account identity and egress isolation: current identity is global config and outbound proxy is process-global.
3. Sub2API can still mutate body/persona/identity before CC Gateway on four route families: native messages, native count_tokens, OpenAI-compatible chat_completions, and OpenAI-compatible responses.

---

## 2. Reverse-coverage gates before shared-pool signing canary

### MUST close

| Gate | Required evidence | Why it blocks signing canary |
|---|---|---|
| G1 `count_tokens` 2.1.146 dynamic capture | Local capture for default attribution and `CLAUDE_CODE_ATTRIBUTION_HEADER=0`: path, header order, beta, body keys, metadata fields, billing/CCH booleans | Messages canary can pass while token-counting leaks endpoint-specific inconsistency |
| G2 OAuth token exchange / refresh shape | Static 2.1.146 extraction plus service-local mock refresh capture; no real `platform.claude.com` call without approval | Refresh can expose different headers, account binding, retry, or logging behavior |
| G3 `metadata.user_id` field preservation rules | Local matrix over `-p`, session options, resume/continue if available, tool-heavy prompts; field names and hashes only | Shared pool must not mix downstream user identity with upstream account identity |
| G4 `X-Claude-Code-Session-Id` rotation rules | Local session lifecycle matrix: separate invocations, multi-turn/stream-json, explicit session id, retry/fallback; hashes only | Session policy is central to shared-account behavior realism |
| G5 Mac-vs-Linux parity | Official 2.1.146 on Linux or deployment-like host to local capture: headers/body/env/TLS summaries | Production on Linux must not emit hybrid Mac-derived persona |
| G6a event_logging route policy | Explicit decision for every shared-pool canary: suppress locally, rewrite through CC Gateway, forward with allowlist, or block | Accidental event_logging passthrough can leak identity/env even if messages are correct |

Current Checkpoint 1 decision (local/static only, before any real canary):
- legacy `POST /api/event_logging/batch`: `suppress locally`
- `POST /api/event_logging/v2/batch`: `suppress locally`
- unknown `/api/event_logging/*`: `block`
- entire route family is excluded from first-wave canary until `2.1.146` schema capture exists

### SHOULD close before broad rollout, and preferably before signing canary if time allows

| Gate | Required evidence |
|---|---|
| G6b event_logging 2.1.146 schema | event names, field names/types, identity-field presence, redaction scan |
| G7 auxiliary endpoint census | path counts and header key lists for non-message endpoints |
| G8 beta/body variant matrix | flags, body key sets, beta token order, cache_control counts |
| G9 byte-exact beta ordering | raw header byte-string comparison in safe summaries |
| G10 retry/request-id behavior | fake 500/429/401/403/refusal responses; retry count, request id, session hash, body hash |
| G11 2.1.146 transport rerun | ClientHello/ALPN/JA3/JA4 summaries for Mac and ideally Linux |
| G12 SSE sequence behavior | controlled local SSE sequences and follow-up request observations |
| G13 refusal/billing/security error behavior | local Anthropic-shaped error fixtures; no raw body leakage |
| G15 attribution-off beyond messages | repeat count_tokens/event_logging/tool-heavy local probes with attribution off |

### NICE TO HAVE

| Gate | Required evidence |
|---|---|
| G14 anthropic-version pinning | include `anthropic-version` in endpoint/beta matrix |

---

## 3. Signing-readiness gates in code design

### 3.1 Sub2API gates

Before signing mode canary, Sub2API must have an explicit `CCGatewayAnthropic` boundary:

1. Native `/v1/messages` must not run final body/persona/billing/CCH generation when the selected account uses CC Gateway.
2. Native `/v1/messages/count_tokens` must not run final body/persona/billing/CCH generation when the selected account uses CC Gateway.
3. OpenAI-compatible `/v1/chat/completions` converted to Anthropic must not call Claude Code mimicry/body/persona helpers before CC Gateway.
4. OpenAI-compatible `/v1/responses` converted to Anthropic must not call Claude Code mimicry/body/persona helpers before CC Gateway.
5. OpenAI-compatible paths must not use the account proxy when the final target is CC Gateway; egress ownership belongs to CC Gateway.
6. CC Gateway control-plane failures must be classified separately from upstream Anthropic account failures so healthy accounts are not quarantined because of gateway config/auth/signing errors.
7. Sub2API must keep scheduler, sticky, quotas, cooldown, circuit breaker, audit, budget, and redaction controls; those are not final persona output and should not be removed.

### 3.2 CC Gateway gates

Before signing mode canary, CC Gateway must own the final output layer:

1. Per-account identity manager with stable storage keyed by selected upstream account.
2. Per-account/per-bucket egress proxy and connection-pool isolation, consuming `x-cc-egress-bucket` explicitly.
3. Strict provider/endpoint/header allowlist for Anthropic shared-pool routes.
4. Canonical Claude Code 2.1.146 persona output: UA, `x-stainless-*`, `x-app`, `anthropic-version`, endpoint-specific beta lists, and deterministic Accept-Encoding/transport policy.
5. Billing block emit/normalize step when `billing_cch_mode=sign`.
6. Validated `cc_version=2.1.146.<3hex>` suffix semantics.
7. Byte-stable final JSON serialization contract.
8. CCH signer as the last body mutation using verified xxHash64 seed/mask/output width.
9. Post-sign immutability guard and verifier.
10. Fail-closed mismatch path with redacted diagnostics.
11. Route decisions for messages, count_tokens, event logging, and OpenAI-compatible converted requests.
12. Memory/body-size limits for full-body buffering and signing.

---

## 4. Signing-mode decision tree

```text
if doc 14 P0 gates are not passed:
  do not start signing canary

if any MUST reverse-coverage gate G1-G5 is open:
  do not start signing canary

if event_logging route policy G6a is not explicit:
  do not start any real shared-pool canary

if Sub2API CCGatewayAnthropic boundary is not implemented for all four route families:
  do not start signing canary

if CC Gateway final-output signing pipeline is incomplete:
  stay in strip/no-CCH mode or halt route

if strip/no-CCH route is being used:
  only claim coverage for endpoints and scenarios actually validated by doc 16 / later captures

if signing route is enabled:
  CC Gateway must be the last body mutation layer
  verify after sign
  fail closed on mismatch
```

No permitted fallback:

- user-supplied CCH;
- user-supplied CCH/header passthrough;
- Sub2API native mimicry after CC Gateway route selection;
- silent native fallback;
- direct upstream bypass;
- direct Anthropic upstream without CC Gateway;
- unsigned billing block;
- stale cached signed body;
- auto egress rotation without canary;
- treating gateway control-plane errors as Anthropic account bans.

---

## 5. Minimal additional local work, ordered by ROI

All items are localhost-only and redaction-only unless separately approved.

1. 2.1.146 `count_tokens` local capture: default attribution and attribution-off.
2. 2.1.146 session lifecycle local matrix.
3. event_logging route policy decision, then 2.1.146 local schema capture.
4. 2.1.146 auxiliary endpoint/path census.
5. 2.1.146 retry/request-id/error local matrix.
6. 2.1.146 beta/body variant matrix.
7. 2.1.146 transport ClientHello/ALPN rerun.
8. Linux-host parity capture.
9. OAuth/token refresh static audit plus service-local mock refresh.
10. SSE response/fallback local sequence matrix.
11. Attribution-off delta beyond messages.

---

## 6. Verification matrix

| Case | Fixture / test hook | Expected |
|---|---|---|
| Signing canary requested while G1-G5 open | policy gate check | rejected before any real upstream traffic |
| Shared-pool canary requested without G6a event policy | policy gate check | rejected before any real upstream traffic |
| Native messages selected for CC Gateway | route fixture | no Sub2API final persona/body/billing/CCH mutation |
| Native count_tokens selected for CC Gateway | route fixture | no Sub2API final persona/body/billing/CCH mutation |
| OpenAI chat_completions selected for Anthropic CC Gateway | converted-route fixture | no Claude Code mimicry before CC Gateway; no account proxy to CC Gateway |
| OpenAI responses selected for Anthropic CC Gateway | converted-route fixture | no Claude Code mimicry before CC Gateway; no account proxy to CC Gateway |
| CC Gateway control-plane 401/403 | fake gateway response | Sub2API fails closed; account is not marked banned/dead |
| CC Gateway signing mismatch | post-sign mutation fixture | fail closed; no upstream request |
| Unknown endpoint on signing route | path fixture | rejected by endpoint allowlist |
| Large body exceeds signing buffer limit | body-size fixture | fail closed with redacted error |
| Event logging route not decided | event route fixture | suppressed/handled according to explicit policy; no accidental passthrough |

---

## 7. Current recommendation

Do not freeze a final signing-mode implementation design yet. First freeze a narrower implementation plan with two phases:

1. **Strip/no-CCH shared-pool route hardening:** Sub2API CCGatewayAnthropic boundary, CC Gateway per-account identity/egress/header allowlist, joint local capture, scheduler/audit/rollback gates.
2. **Manually approved opt-in signing mode:** only after G1-G5 reverse coverage and CC Gateway final-output pipeline pass local/joint verification. It is not an automatic fallback for strip/no-CCH failure.


## 8. Checkpoint 5 gate snapshot (2026-05-21)

| P0 gate | Maps to | Status | First-wave disposition | Evidence |
|---|---|---|---|---|
| P0-A count_tokens | G1 | DEFER | `/v1/messages/count_tokens` remains blocked/deferred and excluded from first-wave shared-pool canary until a real `2.1.146` dynamic fixture exists. | `captures/real-baseline/2026-05-21-claude-code-2146-count-tokens-local-probe/safe-deliverable/count_tokens_local_probe_summary.md`; `captures/real-baseline/2026-05-21-sub2api-cc-gateway-joint-local-capture/safe-deliverable/README.md` |
| P0-B refresh | G2 | PASS | Static CLI audit plus service-local mock covers the pre-design refresh boundary; no real `platform.claude.com` traffic was used. | `captures/real-baseline/2026-05-21-claude-code-2146-oauth-refresh-static-and-local-mock-audit/safe-deliverable/oauth_refresh_static_local_mock_summary.md` |
| P0-C metadata/session | G3 + G4 | DEFER | First-wave scope is limited to `--no-session-persistence`, default persistence first turn, `-c/--continue`, `stream-json`, and local retry/error paths; explicit `--resume` / `--session-id` stays excluded until additional local capture exists. | `captures/real-baseline/2026-05-21-claude-code-2146-session-lifecycle-local-probe/safe-deliverable/session_lifecycle_summary.md`; `captures/real-baseline/2026-05-21-sub2api-cc-gateway-joint-local-capture/safe-deliverable/README.md` |
| P0-D Linux parity | G5 | DEFER | Linux shared-pool deployment/persona parity claims remain blocked; no Linux/deployment-like host was available in this checkpoint. | `captures/real-baseline/2026-05-21-claude-code-2146-linux-parity-local-probe/safe-deliverable/linux_parity_summary.md` |
| P0-E event route-family policy | G6a | PASS | Legacy `/api/event_logging/batch` and `/api/event_logging/v2/batch` are suppressed locally; unknown `/api/event_logging/*` is blocked; the family stays excluded from first-wave canary until schema capture exists. | `14-cc-gateway-shared-pool-compatibility-plan.md`; `captures/real-baseline/2026-05-21-sub2api-cc-gateway-joint-local-capture/safe-deliverable/README.md` |
| P0-F Sub2API boundary | 3.1 | PASS | Native messages/count_tokens plus OpenAI-compatible chat/responses CC Gateway routes no longer do final Sub2API persona/body/billing/CCH mutation or account proxy/TLS ownership. | `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/backend/internal/service/gateway_cc_gateway_boundary_test.go`; `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/backend/internal/service/gateway_cc_gateway_control_plane_test.go` |
| P0-G CC Gateway final-output boundary | 3.2 items 1-12 | PASS | CC Gateway owns per-account identity/egress, strict header/route allowlists, strip verifier, retry re-entry contract, and control-plane fail-closed wiring. | `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/checkpoint3-remediation.test.ts`; `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/proxy-sub2api.test.ts` |
| P0-H canonical 2.1.146 persona lock | 3.2 item 4 | PASS | Canonical `2.1.146` UA / `x-stainless-*` / `anthropic-version` / `x-app` synthesis is validated in CC Gateway tests and observed in joint local capture. | `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/checkpoint3-remediation.test.ts`; `captures/real-baseline/2026-05-21-sub2api-cc-gateway-joint-local-capture/safe-deliverable/README.md` |
| P0-I CCH and cc_version fixture | 3.2 items 5-9 precursor evidence | PASS | Eight billing-attributed localhost fixtures matched the verified 5-hex CCH and 3-hex `cc_version` suffix formula. | `captures/real-baseline/2026-05-21-claude-code-2146-cch-cc-version-local-fixtures/safe-deliverable/cch_cc_version_fixture_summary.md` |
| P0-J API-key passthrough include/block/defer | 3.1 + 3.2 route matrix | PASS | Anthropic API-key passthrough `/v1/messages` is included through CC Gateway; API-key `/v1/messages/count_tokens` is explicitly deferred/blocked for first wave. | `captures/real-baseline/2026-05-21-sub2api-cc-gateway-joint-local-capture/safe-deliverable/README.md`; `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/proxy-sub2api.test.ts` |
| P0-K joint local capture | Section 6 verification matrix | PASS | Fifteen local topology scenarios passed with clean redaction scan, no real upstream, no native fallback, and negative cases fail-closed. | `captures/real-baseline/2026-05-21-sub2api-cc-gateway-joint-local-capture/safe-deliverable/README.md`; `captures/real-baseline/2026-05-21-sub2api-cc-gateway-joint-local-capture/safe-deliverable/joint_local_capture_summary.redacted.json` |

## 9. Decision after Checkpoint 5

Do **not** start `27-final-shared-pool-signing-mode-design.md` yet. Remaining open P0 `DEFER` items are `P0-A count_tokens`, `P0-C metadata/session`, and `P0-D Linux parity`.

Required blocker follow-up before final signing-mode design is recommended:

1. Close `P0-A` with a real `2.1.146` local `count_tokens` fixture, or keep the route blocked/deferred in the approved first-wave scope document.
2. Close `P0-C` with explicit `--resume` / `--session-id` lifecycle coverage, or keep those flows excluded from the approved first-wave scope.
3. Close `P0-D` with Linux/deployment-like localhost capture before any Linux shared-pool deployment or Linux persona claim.

