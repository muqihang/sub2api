# 75 - Claude Code 2.1.185 Stable / 2.1.197 Sonnet 5 Canonical Promotion Proof Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` or `superpowers:subagent-driven-development` to implement this plan checkpoint-by-checkpoint. This is a production-safety promotion-proof plan, not production deployment approval and not live canary approval.

**Goal:** Promote the formal-pool server-selected canonical Claude Code upstream identity from `2.1.179` to the newest safe proven baseline, with `2.1.197` as the primary candidate for Sonnet 5 support, `2.1.185` as stable fallback, and `2.1.179` as rollback.

**Architecture:** Build a complete promotion proof before changing production canonical: verify npm/doc targets, statically analyze public Claude Code packages, dynamically capture loopback-only safe oracle summaries, update Sub2API and CC Gateway canonical profile registries only after proof gates pass, and prove mock E2E with Plan72 environment-residue defense plus Plan74 TLS sidecar equivalence preserved. User client versions remain observed/admission-only; upstream identity remains server-selected and account/session-bound.

**Tech Stack:** Sub2API Go service/tests, CC Gateway TypeScript proxy/config/tests, CC Gateway Go/uTLS sidecar collector tests, public npm package metadata/tarballs, Plan67/68 gap evidence, Plan70 TLS oracle, Plan72 env residue defense, Plan74 deployed local-only equivalence, safe JSON evidence, local mock upstream only.

## Current external version anchors

At plan-writing time on 2026-07-01 America/Los_Angeles:

- `npm view @anthropic-ai/claude-code dist-tags version time.modified --json` returned:
  - `stable=2.1.185`
  - `latest=2.1.197`
  - `next=2.1.197`
  - `version=2.1.197`
  - `time.modified=2026-06-30T17:55:42.305Z`
- Official Claude Code model configuration docs state: Sonnet 5 requires Claude Code `v2.1.197` or later.
- Official Claude Platform release notes state: Claude Sonnet 5 model id `claude-sonnet-5` is launched and supports 1M context, 128k max output, and the same set of tools/platform features as Sonnet 4.6 except Priority Tier.

Because npm tags and model docs may change, CP0 must re-check these anchors before any implementation.

## Input anchors

- Plan67 gap-audit plan: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/docs/anti-ban/67-claude-code-stable-latest-canonical-promotion-gap-audit-plan.md`.
- Plan68 gap evidence: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/docs/anti-ban/68-claude-code-stable-latest-canonical-promotion-gap-evidence-report.md`.
- Plan70 SNI-preserving Claude Code `2.1.179` TLS oracle: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/docs/anti-ban/70-sni-preserving-claude-code-tls-oracle-evidence-report.md`.
- Plan72 canonical local-environment residue defense report: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/docs/anti-ban/72-canonical-local-env-residue-defense-evidence-report.md`.
- Plan73 sidecar TLS engine remediation report: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/docs/anti-ban/73-cc-gateway-sidecar-tls-engine-remediation-report.md`.
- Plan74 deployed local-only equivalence report: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/docs/anti-ban/74-plan65-deployed-local-only-equivalence-evidence-report.md`.
- Current Sub2API Plan72 descendants: `ca127c63d8e8c774497e9ce9efe000781eb2ed90`, `3479b8cb3bb76780ae7963e7565e23e8ec1319be`, or descendants.
- Current CC Gateway Plan72 descendants: `6dceb0213f45c73737329ce47544b1e76dd3bcca`, `07f17f482cd133b2b936a89eee8ad2dd5f01fe62`, or descendants.
- Current CC Gateway Plan74 TLS descendant: `8b3b936f3433f3f2f5e9a3c66579e62db07ff622` or descendant.

## Promotion target policy

### Primary candidate

- `2.1.197` is the primary promotion candidate because it is current `latest/next` and is the minimum documented Claude Code version for Sonnet 5 support at plan-writing time.
- Desired primary final outcome: server-selected canonical upstream identity becomes `2.1.197` after all proof gates pass.

### Stable fallback candidate

- `2.1.185` is the stable fallback because it is current npm `stable`.
- If `2.1.197` has a blocker but `2.1.185` passes all non-Sonnet-5 gates, the executor may implement `2.1.185` canonical promotion only with final decision `PROMOTE_STABLE_2185_ONLY_SONNET5_BLOCKED`.
- If `2.1.185` is promoted, Sonnet 5 must remain disabled/blocked or routed to a fail-closed compatibility error until `2.1.197` passes.

### Rollback baseline

- `2.1.179` remains the rollback canonical until production live canary later proves the new canonical.
- Rollback must be one config/profile selection change plus service restart/redeploy in a later production plan; this plan must document the exact rollback knobs but must not deploy them.

### Observed admission floor

- Keep formal-pool observed version admission floor at `2.1.179` unless CP evidence proves a higher floor is required for safety.
- Inbound users with versions `>=2.1.179` remain admission-compatible if their shape is safe.
- Do not make upstream identity follow the user's observed version.

## Global constraints

- Do not touch, stop, restart, reconfigure, or bind over `3012`, `3017`, `18080`, or `18081`.
- Do not deploy or restart production services.
- Do not call real Anthropic, AWS, Vertex, Bedrock, OpenAI, DeepSeek, credentialed, paid, or non-local upstreams.
- Do not run a live canary in this plan.
- Do not use production account credentials, OAuth tokens, API keys, billing headers, session cookies, proxy credentials, or production account identifiers.
- Do not use client version, client family, client platform/OS/editor/terminal, client timezone, client base URL, client proxy, client domain/keyword residue, or user-supplied profile refs as authority for upstream identity.
- Do not assume `2.1.197` uses only the Plan72 date-marker residue channel; CP1/CP2 must scan for new local-environment residue markers and classify them safely before promotion.
- Do not enable `no_cch`, `signed_cch`, native CCH, strict native parity, or production sidecar egress unless exact proof gates explicitly cover them and the plan's final decision allows them. Default posture remains `strip_attribution` unless proven otherwise.
- Do not regress Plan72: system date marker, timezone, base URL, proxy, domain/keyword residue must remain server-canonicalized or fail-closed.
- Do not regress Plan74: real Go/uTLS sidecar must remain fail-closed and Node direct HTTPS fallback must remain `0` in local-only tests.
- Do not write raw request bodies, raw prompts, raw responses, raw decoded domain/keyword lists, raw ClientHello, raw TLS records, pcap, HAR, secrets, cookies, account UUID/email, workspace IDs, proxy credentials, private keys, certificates, mock CA material, or raw native binaries into repo/docs/evidence/logs/fixtures.
- Evidence may contain only safe summaries: version numbers, hashes, counts, enum buckets, booleans, redacted command results, path-only route buckets, synthetic fixture labels, and test status.
- Public npm tarballs may be stored under `/private/tmp/.../public-npm-cache` with sha256 provenance. Do not delete scratch, extracted packages, or native runtime copies without explicit user approval. Prefer creating isolated timestamped temp directories under `/private/tmp` and leaving them uncommitted; if cleanup is necessary for leak-risk remediation, stop and request approval first. Final report must record whether scratch cleanup was skipped, approved, or not needed.
- If any checkpoint cannot prove loopback-only behavior, stop with the appropriate blocked final decision.
- If any checkpoint discovers a new unsupported upstream behavior for `2.1.197`, do not silently fall back to partial promotion; choose an explicit blocked/fallback decision.

## Required final decision labels

The final report must choose exactly one:

- `PROMOTE_CANONICAL_2197_MOCK_E2E_READY`
- `PROMOTE_STABLE_2185_ONLY_SONNET5_BLOCKED`
- `COMPAT_ONLY_NO_PROMOTION`
- `BLOCKED_VERSION_ORACLE_GAP`
- `BLOCKED_TLS_ORACLE_GAP`
- `BLOCKED_CONTROL_PLANE_GAP`
- `BLOCKED_CCH_BILLING_GAP`
- `BLOCKED_ENV_RESIDUE_REGRESSION`
- `BLOCKED_FAMILY_ADMISSION_REGRESSION`
- `BLOCKED_LOCAL_ONLY_EGRESS_GUARD`

Allowed implementation outcomes by decision:

| Decision | May change code canonical defaults? | May enable Sonnet 5 in mock E2E? | May proceed to later production gate? |
|---|---:|---:|---:|
| `PROMOTE_CANONICAL_2197_MOCK_E2E_READY` | Yes, to `2.1.197` server-selected canonical | Yes | Yes, separate plan required |
| `PROMOTE_STABLE_2185_ONLY_SONNET5_BLOCKED` | Yes, to `2.1.185` only | No | Yes, separate plan required, Sonnet 5 blocked |
| `COMPAT_ONLY_NO_PROMOTION` | No | No | No, write gap plan |
| Any `BLOCKED_*` | No | No | No, remediate blocker first |

Decision precedence when multiple conditions apply:

1. If npm/version oracle is insufficient, choose `BLOCKED_VERSION_ORACLE_GAP`.
2. Else if loopback/local-only egress guard is insufficient, choose `BLOCKED_LOCAL_ONLY_EGRESS_GUARD`.
3. Else if TLS oracle or sidecar proof is insufficient, choose `BLOCKED_TLS_ORACLE_GAP`.
4. Else if Plan72 environment-residue defense regresses, choose `BLOCKED_ENV_RESIDUE_REGRESSION`.
5. Else if family admission is regressed or not closed, choose `BLOCKED_FAMILY_ADMISSION_REGRESSION`.
6. Else if control-plane/model/Sonnet-5 proof is insufficient, choose `BLOCKED_CONTROL_PLANE_GAP`.
7. Else if CCH/billing/attribution proof is insufficient, choose `BLOCKED_CCH_BILLING_GAP`.
8. Else if `2.1.197` passes all gates, choose `PROMOTE_CANONICAL_2197_MOCK_E2E_READY`.
9. Else if `2.1.197` is blocked only by Sonnet/model gate but `2.1.185` passes every non-Sonnet gate, choose `PROMOTE_STABLE_2185_ONLY_SONNET5_BLOCKED`.
10. Else, choose `COMPAT_ONLY_NO_PROMOTION` only when no promotion is allowed but safe compatibility work is complete and no higher-priority blocker applies.

## Canonical authority fields that must be updated together

If promotion is allowed, Sub2API and CC Gateway must update or add server-selected canonical refs as a single profile tuple. Partial promotion is forbidden.

Required tuple fields:

- `policy_version`
- `persona_profile`
- `request_shape_profile_ref`
- `cache_parity_profile_ref`
- `egress_tls_profile_ref`
- `env_residue_profile_ref`
- `locale_profile_ref`
- `base_url_residue_profile_ref`
- canonical upstream `user-agent`
- canonical upstream `anthropic-beta`
- canonical CCH/billing policy
- canonical tool/control-plane behavior
- canonical model mapping policy, including Sonnet 5 behavior
- canonical platform/runtime persona policy, including OS/arch/editor/terminal buckets if present in upstream shape
- canonical TLS expected safe summary if the new candidate's TLS differs from Plan70/73/74

The tuple must be HMAC-signed by Sub2API and verified by CC Gateway. The tuple must be included in session authority binding so one formal-pool session cannot drift between canonical versions.

## Required proof matrix

The executor must produce a matrix for `2.1.179`, `2.1.185`, and `2.1.197`. Historical `2.1.181`, `2.1.195`, and prior `2.1.196` may be included as comparators but cannot replace the required matrix.

| Axis | 2.1.179 rollback | 2.1.185 stable | 2.1.197 primary |
|---|---|---|---|
| npm tarball hash/provenance | Required | Required | Required |
| static package/runtime diff | Required | Required | Required |
| platform/native package scope | Required baseline | Required | Required |
| system/date/env residue behavior | Required baseline | Required | Required |
| new/unknown residue marker scan | Required baseline | Required | Required |
| `anthropic-beta` bucket | Required baseline | Required | Required |
| CCH/billing/attribution behavior | Required baseline | Required | Required |
| tools schema behavior | Required baseline | Required | Required |
| MCP/permission/editor/tool metadata buckets | Required baseline | Required | Required |
| `count_tokens` behavior | Required baseline | Required | Required |
| streaming/SSE behavior | Required baseline | Required | Required |
| error/retry/rate-limit behavior | Required baseline | Required | Required |
| control-plane/model list behavior | Required baseline | Required | Required |
| auth/account/remote-control endpoint buckets | Required baseline | Required | Required |
| Sonnet 5 behavior | Not expected | Expected blocked/absent unless proven | Required |
| TLS SNI safe summary | Required baseline from Plan70 | Required capture/compare | Required capture/compare |
| CC Gateway canonical rewrite/verifier | Required | Required | Required |
| Sub2API attestation refs | Required | Required | Required |
| mock E2E | Required regression | Required | Required |
| no Node direct HTTPS fallback | Required | Required | Required |
| leak scan | Required | Required | Required |

## File map

### Sub2API worktree

Root: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool`

Likely files to inspect/modify:

- Modify: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/backend/internal/service/cc_gateway_adapter.go`
  - Add canonical profile tuple selection for `2.1.185` and `2.1.197` candidates.
  - Keep observed user version separate from canonical authority version.
  - Bind canonical tuple into HMAC attestation.
- Modify/add tests under `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/backend/internal/service/`
  - Existing formal-pool, observed-profile, TLS-profile, env-residue tests should be extended.
  - Add focused tests such as `cc_gateway_canonical_promotion_contract_test.go` if clearer.
- Modify: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/backend/internal/service/testdata/cc_gateway_formal_pool_contract/vectors.json`
  - Add/update canonical tuple vectors for the selected promotion decision.
- Create report: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/docs/anti-ban/75-claude-code-2185-2197-canonical-promotion-evidence-report.md`.
- Optional create/update safe oracle tooling under `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/tools/` only if existing Plan67/71 tools cannot safely capture the required matrix.

### CC Gateway worktree

Root: `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5`

Likely files to inspect/modify:

- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/src/config.ts`
  - Add candidate canonical profile configuration and validation.
  - Add safe default for local tests only; production config must be explicit or documented.
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/src/proxy.ts`
  - Verify attested canonical tuple.
  - Rewrite upstream headers/body using server-selected candidate canonical, not observed client version.
  - Preserve Plan72 final verifier.
  - Preserve Plan74 sidecar-only egress path.
- Modify/add profile helpers if present:
  - `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/src/env-residue-profile.ts`
  - `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/src/egress-tls-profile.ts`
  - Any existing formal-pool profile registry files discovered during CP1.
- Add/modify tests:
  - `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/tests/formal-pool-canonical-promotion.test.ts`
  - `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/tests/formal-pool-env-residue.test.ts`
  - `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/tests/proxy-sub2api.test.ts`
  - `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/tests/egress-tls-profile.test.ts`
  - `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/tests/egress-tls-sidecar.test.ts`
  - `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/tests/config.test.ts`
  - `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/tests/session-and-beta-policy.test.ts` if beta/session binding behavior is affected.
- Sidecar files under `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/sidecar/egress-tls-sidecar/` may be modified only if candidate `2.1.185` or `2.1.197` TLS SNI oracle differs and exact safe-summary proof requires a new compiled-in reviewed profile. Do not store raw TLS templates in config/docs/evidence.

## Evidence root

Use a new evidence root:

`/private/tmp/plan75-claude-code-2185-2197-canonical-promotion-<timestamp>`

Required subdirectories:

- `safe/`: committed-safe summaries only.
- `public-npm-cache/`: optional public npm tarballs and sha256 provenance.
- `scratch/`: temporary extraction/runtime workspace. Leave scratch uncommitted and do not delete it without explicit user approval; if a leak scan requires cleanup, stop and request approval first.

Only `safe/` summaries may be referenced in the final report. Do not commit evidence root files.

## Checkpoint checklist

### CP0 - Anchor verification, latest-target lock, and safety gate

**Goal:** Confirm the current version/model targets and refuse to proceed if the promotion target moved materially.

- [ ] Verify Sub2API HEAD contains Plan72 commits `ca127c63d8e8c774497e9ce9efe000781eb2ed90` and `3479b8cb3bb76780ae7963e7565e23e8ec1319be` or descendants.
- [ ] Verify CC Gateway HEAD contains Plan72 commits `6dceb0213f45c73737329ce47544b1e76dd3bcca` and `07f17f482cd133b2b936a89eee8ad2dd5f01fe62` or descendants.
- [ ] Verify CC Gateway HEAD contains Plan74 commit `8b3b936f3433f3f2f5e9a3c66579e62db07ff622` or descendant.
- [ ] Run `npm view @anthropic-ai/claude-code dist-tags version time.modified --json` and record safe output.
- [ ] Query official Claude Code model configuration docs and official Claude Platform release notes, recording only safe facts about Sonnet 5 version requirements/model id/features.
- [ ] If `latest` is greater than `2.1.197`, lock the plan target to `2.1.197` unless the user explicitly approves retargeting. Record `latest_moved_target_locked=true`.
- [ ] If `stable` is not `2.1.185`, lock the plan fallback to `2.1.185` unless the user explicitly approves retargeting. Record `stable_moved_target_locked=true`.
- [ ] Create `$EVIDENCE_ROOT/safe/cp0-anchor-version-lock.json` with: npm dist-tags, target lock decision, official-doc safe facts, HEAD short hashes, forbidden ports untouched statement, and no-upstream statement.

Expected blocker decisions:

- If official docs cannot be reached but npm metadata is reachable, continue with `official_doc_status=unreachable`, but promotion is forbidden unless CP2 loopback dynamic oracle and CP4 control-plane/model/Sonnet-5 matrix independently prove the required model behavior for the selected candidate.
- If npm metadata cannot be reached, stop with `BLOCKED_VERSION_ORACLE_GAP`.

### CP1 - Static package provenance and diff audit

**Goal:** Understand candidate runtime changes before dynamic capture or code changes.

- [ ] Download public npm tarballs for exactly `@anthropic-ai/claude-code@2.1.179`, `@anthropic-ai/claude-code@2.1.185`, and `@anthropic-ai/claude-code@2.1.197` plus platform-native packages required for the local machine if needed.
- [ ] Record package name, version, tarball URL, integrity, sha256, file count, and package type in `$EVIDENCE_ROOT/safe/cp1-package-provenance.json`.
- [ ] Statically compare only safe summaries across versions:
  - CLI version/reporting strings.
  - User-agent construction behavior.
  - `anthropic-beta` token construction behavior.
  - CCH/billing/attribution string presence buckets.
  - Date-marker/env-residue code presence buckets.
  - Model list/control-plane string buckets, including `sonnet`, `claude-sonnet-5`, and any version-gating checks.
  - Tool schema/control-plane path buckets.
  - `count_tokens` path buckets.
  - Proxy/base-url env variable reference buckets.
- [ ] Do not copy raw extracted source code, raw decoded domain/keyword lists, native binary chunks, minified function bodies, or long string tables into evidence/report.
- [ ] Produce `$EVIDENCE_ROOT/safe/cp1-static-diff-summary.json` and a markdown-safe table for the final report.

Required static pass criteria:

- The executor can identify whether `2.1.197` introduces application-layer changes that affect upstream shape.
- The executor can identify whether `2.1.185` differs materially from `2.1.179` on beta/CCH/billing/tools/control-plane/env-residue/model behavior.
- If static analysis discovers raw credential handling, unknown exfiltration surfaces, or unclassifiable minified/native behavior affecting upstream shape, stop with `BLOCKED_VERSION_ORACLE_GAP` unless dynamic CPs can safely disambiguate without real upstream.

### CP1.5 - Platform, runtime, settings, and hidden-residue surface audit

**Goal:** Prevent a narrow macOS/local CLI capture from missing platform-, settings-, editor-, or new-residue-specific upstream shape changes.

- [ ] Identify every public Claude Code package involved for the local platform and any deployment-relevant platform package that can be inspected safely, such as darwin-arm64, linux-x64, linux-arm64, or other npm-distributed native/runtime packages. Record unavailable platform packages explicitly as `not_available` or `not_inspected_with_reason`.
- [ ] Define the server-selected canonical platform/runtime persona for promotion, for example a safe bucket such as `canonical_platform_persona=darwin_arm64_cli` or another evidence-backed bucket. Do not let inbound user OS/arch/editor/terminal choose this persona.
- [ ] Static-scan safe buckets for upstream-visible platform/runtime fields: OS, arch, terminal, shell, IDE/editor, extension host, MCP presence, permission mode, project/workspace metadata, and remote-control flags.
- [ ] Static-scan safe buckets for settings/config/env surfaces that may alter upstream shape, including model-selection envs, settings files, allowed tools, MCP servers, telemetry/debug flags, proxy envs, remote-control flags, and custom base URL variables. Do not record raw paths, raw config contents, or secrets.
- [ ] Static-scan for new residue/covert-marker shapes beyond Plan72's `Today<apostrophe>s date is <date>.` marker. Record only marker-shape buckets and hashes. If a new unhandled upstream-visible residue marker is found, promotion is forbidden until CP5-CP9 include canonicalization or fail-closed verification for it.
- [ ] Write `$EVIDENCE_ROOT/safe/cp1_5-platform-runtime-settings-residue-audit.json`.

Required pass criteria:

- The final report states exactly which platform/runtime persona the canonical profile represents.
- Any platform, editor, MCP, permission, settings, or remote-control field that can affect upstream shape is either canonicalized server-side, stripped, fail-closed, or proven absent.
- Missing platform coverage cannot be silently treated as proof; it must be recorded as a limitation and reflected in the final decision.

### CP2 - Loopback-only dynamic oracle harness for CLI candidates

**Goal:** Capture candidate request-shape safe summaries without real upstream and without credentials.

- [ ] Reuse or extend Plan67/71 harnesses to run Claude Code `2.1.179`, `2.1.185`, and `2.1.197` against a loopback mock endpoint only.
- [ ] Set dummy credentials only. Never use real OAuth/API keys.
- [ ] Use a same-scope egress guard that denies all non-loopback network access for the dynamic process. If guard cannot prove this, stop with `BLOCKED_LOCAL_ONLY_EGRESS_GUARD`.
- [ ] Capture safe summaries for representative request shapes:
  - basic messages request;
  - streaming request if Claude Code emits a distinct shape;
  - tool-use capable request, including tool metadata and permission/tool-choice buckets if emitted;
  - MCP-configured and MCP-absent buckets if safely simulatable with synthetic local MCP metadata only;
  - `count_tokens` request;
  - streaming/SSE request and non-streaming request;
  - model/control-plane request or model-list path if locally observable;
  - Sonnet 5 selection/model config path for `2.1.197` if locally observable;
  - unsupported/absent Sonnet 5 behavior for `2.1.185`;
  - env-residue date marker variants under neutral, Asia/Shanghai, Asia/Urumqi, official base URL, nonofficial synthetic base URL, synthetic domain bucket, and synthetic AI-keyword bucket;
  - settings/config variation buckets for synthetic safe model env, tool permission mode, debug/telemetry flags, and remote-control/base-url flags if locally observable;
  - error, retry, 401/403/429/5xx, and rate-limit response handling buckets from the loopback mock, because upstream-visible retries or fallback endpoints can change account risk.
- [ ] Capture only safe summaries: method, path bucket, header-name presence, header value hash/bucket for nonsecret known canonical strings, body structural keys, model id bucket, beta token set hash/bucket, cch/billing presence booleans, system marker bucket, new residue marker bucket, platform/runtime persona bucket, tool/MCP/permission buckets, streaming/SSE bucket, token/count shape bucket, error/retry bucket, and response handling bucket.
- [ ] Write `$EVIDENCE_ROOT/safe/cp2-cli-dynamic-oracle-summary.json`.

Required dynamic pass criteria:

- `2.1.197` Sonnet 5 path is observed or safely proven through static+control-plane evidence.
- `2.1.185` fallback behavior is observed for model/control-plane and absence/blocking of Sonnet 5 is explicit.
- No dynamic run attempts non-loopback network access.
- No real upstream request count is greater than `0`.

### CP3 - TLS SNI oracle capture and sidecar comparison for candidates

**Goal:** Determine whether candidate canonical promotion requires TLS profile changes beyond Plan70/73/74.

- [ ] Using the Plan69/70 SNI-preserving approach, capture safe TLS ClientHello summaries for Claude Code `2.1.185` and `2.1.197` with logical provider host `api.anthropic.com` and loopback collector/dial guard.
- [ ] Include `2.1.179` Plan70 summary as baseline in the comparison.
- [ ] Safe summary fields must include only: JA3 hash, JA4, cipher count, extension count, ALPN bucket, TLS version list, SNI bucket, GREASE boolean, and any safe extension-count/order hash already approved by prior plans.
- [ ] If candidate TLS summary matches Plan70/73/74, record `candidate_tls_profile_ref_can_reuse_2179=true` for that candidate.
- [ ] If candidate TLS summary differs, do not change runtime config to raw TLS parameters. Either:
  - implement a reviewed compiled-in sidecar candidate profile under source/tests only, then prove same-condition match; or
  - stop with `BLOCKED_TLS_ORACLE_GAP`.
- [ ] Run CC Gateway sidecar tests after any sidecar change.
- [ ] Write `$EVIDENCE_ROOT/safe/cp3-candidate-tls-oracle-comparison.json`.

Required TLS pass criteria for `2.1.197` promotion:

- Real sidecar same-condition local collector summary matches the selected `2.1.197` TLS oracle exactly on approved safe fields; or
- Candidate `2.1.197` TLS oracle matches existing Plan70/73/74 canonical and existing sidecar still matches.

### CP4 - Control-plane, tools, count_tokens, beta, CCH/billing gap closure

**Goal:** Close the Plan68 blockers before any canonical code change.

- [ ] Build a safe matrix comparing `2.1.179`, `2.1.185`, and `2.1.197` for:
  - `anthropic-beta` token set and ordering bucket;
  - `thinking-token-count` or equivalent app-layer token accounting bucket;
  - tool-use schema, tool metadata, permission mode, tool-choice, and MCP presence buckets;
  - `count_tokens` request/response shape buckets;
  - streaming/SSE request and response framing buckets;
  - error/retry/rate-limit behavior buckets;
  - model/control-plane path buckets;
  - auth/account/remote-control endpoint buckets using dummy credentials and loopback only;
  - Sonnet 5 model id/model alias behavior;
  - CCH/billing header/body/metadata presence;
  - attribution stripping requirements.
- [ ] Decide candidate policy:
  - `strip_attribution` remains required unless exact no-CCH/signed-CCH proof exists.
  - If `2.1.197` adds a beta/control-plane token required for Sonnet 5, CC Gateway canonical rewrite must add it server-side, not from observed client.
  - If `2.1.197` adds a billing/CCH field that cannot be safely stripped/proven, stop with `BLOCKED_CCH_BILLING_GAP`.
- [ ] Write `$EVIDENCE_ROOT/safe/cp4-control-plane-cch-billing-matrix.json`.

Required pass criteria:

- Every Plan68 blocker is either closed with evidence or carried into a blocked/fallback decision.
- No `>=2.1.179` observed client version can self-promote to new canonical, no-CCH, signed-CCH, or Sonnet 5 authority.

### CP4.5 - Family admission and observed-only policy closure

**Goal:** Close the Plan68 family-policy gap before any canonical promotion.

- [ ] Build a family admission matrix for `cli`, `desktop`, `vscode_extension`, and `unknown_future`.
- [ ] Record evidence status for each family bucket:
  - `cli`: dynamic loopback proof required for version candidates.
  - `desktop`: if GUI dynamic remains blocked, classify explicitly as `static_only_admitted_observed_only`, `blocked`, or `not_supported`; do not imply dynamic proof.
  - `vscode_extension`: if GUI/extension dynamic remains blocked, classify explicitly as `static_only_admitted_observed_only`, `blocked`, or `not_supported`; do not imply dynamic proof.
  - `unknown_future`: default fail-closed unless a server-side allow policy explicitly maps it to observed-only admission without authority.
- [ ] Confirm family bucket can only appear in `observed_client_profile` and cannot affect canonical version, model policy, beta token set, CCH/billing policy, TLS profile, env residue profile, locale profile, base-url residue profile, or upstream user-agent.
- [ ] Confirm a forged family field in headers/query/body/metadata/tool fields cannot enter the authority tuple.
- [ ] If Desktop/VS Code are admitted without dynamic proof, require final report language: `family_dynamic_incomplete_but_observed_only_admission_proven`; production/live canary must later choose whether to enable or restrict these buckets.
- [ ] If family admission policy cannot be proven safe, final decision must be `BLOCKED_FAMILY_ADMISSION_REGRESSION` or `COMPAT_ONLY_NO_PROMOTION`; promotion is forbidden.
- [ ] Write `$EVIDENCE_ROOT/safe/cp4-family-admission-matrix.json`.

Required pass criteria:

- CLI/Desktop/official VS Code are not wrongly blocked when policy says they are admitted observed-only.
- Unknown/future family cannot enter canonical path by version number or forged family hint alone.
- Family never changes server-selected canonical tuple.

### CP5 - Sub2API failing tests for selected canonical tuple

**Goal:** Prove Sub2API will sign only server-selected candidate canonical refs and will not let observed client data control promotion.

Write failing tests first. Required cases:

- [ ] With server candidate set to `2.1.197`, incoming observed `2.1.179`, `2.1.185`, `2.1.197`, and future `2.1.198` safe shapes all sign canonical `policy_version=2.1.197` and the selected `2.1.197` profile refs.
- [ ] With server candidate set to `2.1.185`, the same observed versions sign canonical `policy_version=2.1.185`; Sonnet 5 model requests fail closed or are marked unsupported according to CP4 policy.
- [ ] With rollback candidate set to `2.1.179`, canonical returns to `2.1.179` refs.
- [ ] User-forged profile refs in headers/query/body/metadata/tool fields cannot alter canonical tuple.
- [ ] User-forged version/family/platform/OS/editor/terminal/settings/env residue cannot alter canonical tuple.
- [ ] Observed profile still records safe `cli_version_bucket`, `client_family_bucket`, platform/runtime persona bucket, settings bucket, and env residue buckets for audit only.
- [ ] New/unknown residue marker-looking shapes from CP1.5 fail closed unless a canonicalizer/final verifier test explicitly covers them.
- [ ] Family matrix cases cover `cli`, `desktop`, `vscode_extension`, and `unknown_future`; known admitted families remain observed-only, and `unknown_future` fails closed unless a server-side allow policy explicitly admits it as observed-only.
- [ ] Contract vectors include selected candidate refs and reject mixed tuple fields.

Expected before implementation: FAIL on missing candidate tuple support or missing vector coverage.

### CP6 - Sub2API implementation for selected canonical tuple

**Goal:** Implement server-side canonical tuple selection and signing.

- [ ] Add candidate canonical constants/registry entries for `2.1.185` and `2.1.197` only after CP1-CP4 evidence exists.
- [ ] Add explicit server-side selection knob with safe default behavior for tests. Production default must be documented and not silently changed outside this plan's final decision.
- [ ] Ensure account/server config is the only source of canonical candidate selection.
- [ ] Add or update helper functions for `policy_version`, persona, request shape, cache parity, TLS, env residue, locale, base-url residue, beta, CCH/billing policy, and model policy refs.
- [ ] Bind the full selected tuple into HMAC context.
- [ ] Update contract vectors.
- [ ] Run targeted Sub2API tests and record output to `$EVIDENCE_ROOT/safe/cp6-sub2api-tests.txt`.

Required pass criteria:

- No test can make observed client version or user-supplied hints override canonical tuple.
- Mixed `2.1.179`/`2.1.185`/`2.1.197` tuple fields are rejected or impossible to sign.

### CP7 - CC Gateway failing tests for selected canonical tuple

**Goal:** Prove CC Gateway rejects missing/mixed/forged canonical promotion context and rewrites upstream shape to the selected candidate only.

Write failing tests first. Required cases:

- [ ] Missing candidate tuple fields fail closed.
- [ ] Unknown candidate version fails closed.
- [ ] Mixed tuple, e.g. `policy_version=2.1.197` with `egress_tls_profile_ref=2.1.179`, fails closed.
- [ ] Observed client `2.1.179` with server canonical `2.1.197` emits upstream `user-agent` and beta/model shape for `2.1.197`, not `2.1.179`.
- [ ] Observed client `2.1.197` with server canonical `2.1.185` emits upstream `2.1.185` shape or Sonnet 5 fail-closed policy, not `2.1.197`.
- [ ] Rollback canonical `2.1.179` tuple is explicitly accepted and rewrites upstream shape back to `2.1.179` while still preserving Plan72/Plan74 guards.
- [ ] Family matrix cases cover `cli`, `desktop`, `vscode_extension`, and `unknown_future`; family is observed-only and unknown/future cannot enter canonical path through forged hints.
- [ ] Platform/OS/editor/terminal/settings/MCP hints are observed-only buckets and cannot alter canonical tuple or upstream identity.
- [ ] New/unknown residue marker-looking shapes fail closed unless CP1.5/CP5 added an exact canonicalizer/final verifier.
- [ ] `anthropic-beta` output is exactly candidate canonical token set from CP4.
- [ ] body/header have no `x-anthropic-billing-header`, no raw `cch=`, and no client attribution unless CP4 exact proof authorizes otherwise.
- [ ] Plan72 canonical date marker rewrite still passes for candidate canonical.
- [ ] Session authority ledger rejects canonical tuple drift between requests.
- [ ] AWS scoped formal-pool context verifies the same tuple before egress.
- [ ] Node direct HTTPS fallback count remains `0` in tests.

Expected before implementation: FAIL on missing candidate tuple support or rewrite mismatch.

### CP8 - CC Gateway implementation for selected canonical tuple

**Goal:** Enforce candidate canonical tuple before sidecar/upstream egress.

- [ ] Add typed canonical candidate profile registry for `2.1.185` and/or `2.1.197` according to CP4.
- [ ] Extend attested formal-pool context parsing/validation for candidate tuple fields.
- [ ] Add `verifyCanonicalPromotionTuple(config, attested, account/session)` or equivalent small pure helper.
- [ ] Update upstream rewrite to use candidate canonical user-agent, beta, model policy, and attribution policy.
- [ ] Preserve Plan72 env residue canonicalizer/final verifier.
- [ ] Preserve Plan74 sidecar-only egress and fail-closed behavior.
- [ ] Bind selected candidate tuple into session authority equality.
- [ ] Run targeted CC Gateway tests and record output to `$EVIDENCE_ROOT/safe/cp8-cc-gateway-tests.txt`.

Required pass criteria:

- CC Gateway cannot be tricked into following observed client version.
- CC Gateway cannot send mixed canonical tuple upstream.
- CC Gateway cannot bypass sidecar to Node direct HTTPS.

### CP9 - Local mock E2E promotion proof

**Goal:** Prove the complete Sub2API -> CC Gateway -> real sidecar -> local collector/mock upstream chain for the primary, fallback, and rollback canonical tuples required by the final decision.

- [ ] Use independent local ports only, excluding `3012`, `3017`, `18080`, and `18081`.
- [ ] Use same-scope loopback-only egress guard.
- [ ] If the intended final decision is `PROMOTE_CANONICAL_2197_MOCK_E2E_READY`, run all three canonical tuple E2E sets:
  - primary `2.1.197` E2E, including Sonnet 5 behavior;
  - stable fallback `2.1.185` E2E, with Sonnet 5 fail-closed unless CP4 proves support;
  - rollback `2.1.179` E2E.
- [ ] If the intended final decision is `PROMOTE_STABLE_2185_ONLY_SONNET5_BLOCKED`, run:
  - stable fallback `2.1.185` E2E;
  - rollback `2.1.179` E2E;
  - a recorded `2.1.197` blocked reason from the highest-priority failed gate.
- [ ] For each E2E canonical tuple that is run, include:
  - observed inbound `2.1.179` with that canonical tuple;
  - observed inbound `2.1.185` with that canonical tuple;
  - observed inbound `2.1.197` with that canonical tuple;
  - env residue noncanonical system marker canonicalized;
  - any CP1.5 new residue marker either canonicalized or fail-closed;
  - synthetic nonofficial base-url/domain/keyword residue stripped or safe-bucketed;
  - synthetic platform/OS/editor/terminal/settings/MCP hints remain observed-only and do not alter canonical upstream identity;
  - Sonnet 5 request behavior according to that canonical tuple policy;
  - `count_tokens` route if supported;
  - tool-use capable request;
  - streaming route if applicable;
  - loopback mock error/retry/rate-limit paths prove no non-loopback fallback and no authority drift.
- [ ] Test canonical tuple switching `2.1.197 -> 2.1.185 -> 2.1.179`:
  - new sessions may use the newly selected tuple;
  - existing sessions with changed tuple must fail closed as session authority drift.
- [ ] Capture mock upstream safe summary:
  - canonical user-agent bucket;
  - beta token set hash/bucket;
  - model id bucket;
  - body structural shape hash/bucket;
  - platform/runtime persona bucket;
  - tool/MCP/permission bucket;
  - streaming/error/retry bucket;
  - no billing/CCH booleans unless exactly authorized;
  - canonical date marker bucket;
  - TLS safe summary match boolean;
  - Node direct HTTPS fallback count `0`;
  - real upstream request count `0`.
- [ ] Write `$EVIDENCE_ROOT/safe/cp9-local-mock-e2e-summary.json`.

Required pass criteria:

- The mock upstream sees one stable canonical identity per canonical tuple under test, independent of observed user version/family/env residue.
- Required primary/fallback/rollback E2E sets are present for the final decision label.
- TLS summary matches the oracle/profile for every canonical tuple under test.
- Session authority drift fails closed when canonical tuple changes inside an existing session.
- No real upstream access occurs.

### CP10 - Regression test suite and leak scan

**Goal:** Ensure promotion does not regress prior safety gates.

Run and record at minimum:

Sub2API:

```bash
go test ./internal/service -run 'CCGateway|FormalPool|ObservedProfile|TLSProfile|EnvResidue|LocalEnv|Canonical|Promotion|ControlPlane|CCH|Billing|Model' -count=1
```

CC Gateway:

```bash
npx tsx tests/formal-pool-canonical-promotion.test.ts
npx tsx tests/formal-pool-env-residue.test.ts
npx tsx tests/proxy-sub2api.test.ts
npx tsx tests/egress-tls-profile.test.ts
npx tsx tests/egress-tls-sidecar.test.ts
npx tsx tests/config.test.ts
npx tsc --noEmit
```

Sidecar, if modified:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/sidecar/egress-tls-sidecar
go test ./...
```

Leak scan requirements:

- Scan modified repo files, tests, reports, and `$EVIDENCE_ROOT/safe`.
- Block on raw secrets, raw prompts/bodies/responses, raw decoded domain/keyword list, raw TLS/pcap/HAR, cert/key material, account identifiers, proxy credentials, native binary copies in repo, or raw long minified source dumps.
- Write `$EVIDENCE_ROOT/safe/cp10-leak-scan-summary.json`.
- Record `scratch_cleanup_status` as one of `not_needed`, `skipped_requires_user_approval`, or `approved_by_user`, and do not perform cleanup unless approval is explicit.

Required pass criteria:

- All targeted tests pass.
- Leak scan blocking findings equal `0`.

### CP11 - Review and final report

**Goal:** Produce a defensible promotion decision without overclaiming production readiness.

- [ ] Write final report: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/docs/anti-ban/75-claude-code-2185-2197-canonical-promotion-evidence-report.md`.
- [ ] Include:
  - exact final decision label and the decision-precedence path used to choose it;
  - npm/doc target lock snapshot;
  - proof matrix for `2.1.179`, `2.1.185`, `2.1.197`;
  - static diff safe summary;
  - platform/runtime/settings/new-residue audit summary;
  - dynamic oracle safe summary;
  - TLS oracle/sidecar result;
  - control-plane/tools/MCP/permissions/count_tokens/streaming/error-retry/beta/CCH/billing matrix;
  - Sub2API and CC Gateway test results;
  - mock E2E result;
  - rollback knobs;
  - scratch cleanup status and whether user approval was requested/received;
  - non-goals: no production deployment, no live canary, no real upstream calls.
- [ ] Request exactly one high-spec review agent after CP9 or CP10, with focus on:
  - promotion proof completeness;
  - fail-open risks;
  - mixed tuple/session drift;
  - observed-client authority injection;
  - Plan72 env residue regressions;
  - Plan74 TLS/Node fallback regressions;
  - Sonnet 5/model control-plane gaps;
  - platform/runtime/settings/new-residue capture gaps;
  - leak/secret/raw evidence risk.
- [ ] Address required review edits.
- [ ] Run `git diff --check` in both worktrees.
- [ ] Commit Sub2API and CC Gateway changes separately if and only if final decision permits code changes or report-only evidence is complete.

## Reviewer prompt template

Use this prompt for the required final review agent:

```text
You are reviewing Plan75: Claude Code 2.1.185 stable / 2.1.197 Sonnet 5 canonical promotion proof.

Scope:
- Sub2API worktree: /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool
- CC Gateway worktree: /Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5
- Plan75 report: docs/anti-ban/75-claude-code-2185-2197-canonical-promotion-evidence-report.md

Review goals:
1. Decide whether the evidence is sufficient for the selected final decision label.
2. Check whether 2.1.197 canonical promotion is justified, or whether only 2.1.185 fallback / compat-only / blocked is justified.
3. Look for fail-open paths, mixed canonical tuple acceptance, session authority drift, observed-client authority injection, Sonnet 5/model control-plane gaps, CCH/billing gaps, Plan72 env residue regressions, Plan74 TLS/sidecar regressions, Node direct HTTPS fallback, and raw secret/evidence leaks.
4. Verify no production deployment, live canary, real upstream calls, or forbidden port touches occurred.
5. Verify leak scan and tests are adequate and that raw prompt/body/TLS/domain-list/native material is not committed.

Return exactly one verdict: PASS, PASS_WITH_REQUIRED_EDITS, or FAIL, with concrete required edits if not PASS.
```

## Final non-goals

Even if Plan75 ends with `PROMOTE_CANONICAL_2197_MOCK_E2E_READY`, it still does not approve production rollout. The next step must be a separate production gate/live canary plan that uses the promoted canonical profile, Plan72 residue defense, and Plan74 TLS sidecar path with explicit user approval.

## Self-review checklist

- Spec coverage: This plan covers primary promotion to `2.1.197`, stable fallback `2.1.185`, rollback `2.1.179`, static analysis, platform/runtime/settings/new-residue audit, dynamic capture, TLS oracle, control-plane/tools/MCP/permissions/count_tokens/streaming/error-retry, Sonnet 5, CCH/billing, env residue, family observed-only, Sub2API, CC Gateway, sidecar, mock E2E, leak scan, and review.
- Placeholder scan: No `TBD`, `TODO`, `implement later`, or unspecified final decision is intentionally left.
- Authority model: User observed version/family/env residue never controls canonical upstream identity.
- Safety model: No production ports, real upstreams, production credentials, raw secrets, raw prompts, raw domain lists, raw TLS, or live canary are allowed.
- Deployment model: Production gate remains separate even after mock E2E promotion proof.
