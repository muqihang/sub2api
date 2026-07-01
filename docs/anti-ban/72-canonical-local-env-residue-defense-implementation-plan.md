# 72 - Canonical Local Environment Residue Defense Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` or `superpowers:subagent-driven-development` to implement this plan checkpoint-by-checkpoint. This is a production-safety implementation plan, not live canary approval.

**Goal:** Prevent formal-pool shared accounts from inheriting end-user Claude Code local environment residue such as timezone, custom `ANTHROPIC_BASE_URL`, proxy/domain/keyword classification, or client-family variation, while allowing official Claude Code CLI/Desktop/VS Code clients to remain observed-only inputs.

**Architecture:** Sub2API records only safe observed client/family/residue buckets and signs server-selected canonical environment authority refs into the formal-pool context. CC Gateway verifies those refs, canonicalizes upstream-bound local-environment residue in message system content when present, and runs a final verifier before sidecar/upstream egress. TLS sidecar work from Plan73/74 remains unchanged and is treated as a separate transport authority.

**Tech Stack:** Sub2API Go service/tests, CC Gateway TypeScript config/proxy/tests, Plan71 safe oracle tools, existing formal-pool HMAC attestation, safe JSON evidence, local mock upstream only.

## Input anchors

- Plan71 evidence report: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/docs/anti-ban/71-claude-code-local-environment-attribution-oracle-evidence-report.md`.
- Plan71 evidence root: `/private/tmp/claude-code-local-env-attribution-oracle-20260630T142519Z`.
- Plan74 local-only equivalence report: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/docs/anti-ban/74-plan65-deployed-local-only-equivalence-evidence-report.md`.
- Current canonical production Claude Code app profile: `2.1.179`.
- Current observed admission floor: `2.1.179`.
- Current TLS state: `DEPLOYED_LOCAL_ONLY_EQUIVALENCE_READY`, not live canary approved.
- Current Sub2API HEAD after Plan71 tool commit: `fc6bdf1df` or descendant.
- Current CC Gateway HEAD after Plan74: `8b3b936f3433f3f2f5e9a3c66579e62db07ff622` or descendant.

## Global constraints

- Do not touch, stop, restart, reconfigure, or bind over `3012`, `3017`, `18080`, or `18081`.
- Do not deploy or restart production services.
- Do not call real Anthropic, AWS, Vertex, Bedrock, OpenAI, DeepSeek, credentialed, paid, or non-local upstreams.
- Do not change production canonical from `2.1.179`.
- Do not promote `2.1.185` or adapt/promote `2.1.196` in this plan.
- Do not enable `no_cch`, `signed_cch`, or strict native parity.
- Do not change Plan73/74 TLS sidecar behavior except to preserve existing tests.
- Do not use client timezone, client base URL, client proxy, client domain/keyword residue, or client family as authority for upstream identity.
- Do not write raw request bodies, raw prompts, raw responses, raw decoded domain/keyword lists, raw ClientHello, pcap, secrets, cookies, account UUID/email, workspace IDs, proxy credentials, private keys, certificates, or mock CA material to repo/docs/evidence/logs/fixtures.
- Evidence may contain only safe buckets, booleans, counts, hashes, path-only route buckets, status labels, and redacted command results.
- Live canary and production rollout require separate explicit approval after this plan.

## Required final decision labels

The final report must choose exactly one:

- `CANONICAL_ENV_RESIDUE_MOCK_E2E_READY`
- `BLOCKED_ENV_RESIDUE_PROFILE_DESIGN_GAP`
- `BLOCKED_ENV_RESIDUE_VERIFIER_GAP`
- `BLOCKED_FAMILY_ADMISSION_REGRESSION`

## Canonical profile model

### Server-selected authority refs

Add these HMAC-signed context fields for formal-pool requests:

- `env_residue_profile_ref`: canonical combined residue policy.
  - Default: `env-residue-profile:claude-code-2.1.179-us-pacific-official-anthropic-v1`.
- `locale_profile_ref`: canonical locale/date policy.
  - Default: `locale-profile:us-pacific-v1`.
- `base_url_residue_profile_ref`: canonical base-url/domain policy.
  - Default: `base-url-residue-profile:official-anthropic-v1`.

These refs are selected only from account/server config. They are never read from user headers, query parameters, request bodies, observed client profile, or client family.

Optional refs are allowed only if the executor proves they reduce ambiguity without changing scope:

- `date_marker_profile_ref`: optional auditability split for the date-marker policy; not required because `locale_profile_ref` plus `env_residue_profile_ref` already define the canonical date marker.
- `canonical_user_agent_profile_ref`: optional auditability split for future family expansion; not required because existing version/persona/profile refs and final verifier already lock upstream identity.
- `client_family_policy_ref`: not recommended as an authority ref. If added, it must remain admission/audit-only and must not affect upstream identity.

### Canonical upstream residue policy

For `env-residue-profile:claude-code-2.1.179-us-pacific-official-anthropic-v1`:

- Canonical timezone: `America/Los_Angeles`.
- Canonical date marker format when a Claude Code date marker is present: `Today's date is YYYY-MM-DD.` using ASCII apostrophe and hyphen separators.
- Date is computed server-side from the canonical timezone at request time.
- Do not inject a date marker when none exists. Absence is allowed for family compatibility until Desktop/VS Code dynamic oracle coverage exists.
- If a system field contains a recognized noncanonical Claude Code date marker, rewrite only that marker to the canonical marker.
- If a system field contains an unrecognized local-env marker shape, raw `ANTHROPIC_BASE_URL`, proxy env variable literal, or direct local proxy/base-url residue in system content, fail closed.
- User message content is not scanned or modified for these markers; only upstream-bound `system` content and upstream headers/query/allowed structural authority fields are in scope.

Recognized Claude Code date marker shapes for this plan are limited to exact `Today<apostrophe>s date is <date>.` sentences in `system` string or array text blocks, where:

- `<apostrophe>` is ASCII `'` or one of the Plan71 observed Unicode variants.
- `<date>` is ISO-like `YYYY-MM-DD` or slash `YYYY/MM/DD`.
- The sentence is a standalone date marker or a standalone text block; ordinary system instructions containing unrelated dates are not rewritten.
- Absence of the marker is allowed and must not cause injection.
- Any other local-env marker-looking shape in `system` is unrecognized and must fail closed.

### Safe bucket classification strategy

Plan72 must not copy the raw Plan71 decoded domain/keyword list into source code, fixtures, docs, or evidence. If bucket classification is needed in runtime code or tests, use one of these safe strategies only:

- high-level heuristic buckets such as TLD suffix, loopback, official host, or redacted synthetic fixture domains;
- hash/HMAC lookup over a private server-side list where the raw list is not committed;
- Plan71 safe evidence buckets as constants, never raw domain or keyword strings from the decoded list.

The implementation may classify `ai_lab_keyword` or `claude_proxy_resale_like` only from redacted synthetic fixtures or private hashed lookup. It must not persist the raw keyword/domain list.

### Observed-only client profile additions

Extend safe `observed_client_profile` only as audit/admission data. Suggested optional keys:

- `client_family_bucket`: `cli | desktop | vscode | unknown`.
- `local_env_residue_present`: boolean.
- `date_format_bucket`: `hyphen | slash | other | not_observed`.
- `apostrophe_bucket`: `ascii | unicode_variant_1 | unicode_variant_2 | unicode_variant_3 | other | not_observed`.
- `base_url_category_bucket`: `official_anthropic | neutral_gateway | china_tld | china_org_domain | china_cloud_domain | ai_lab_keyword | claude_proxy_resale_like | unknown | not_observed`.
- `proxy_env_bucket`: `no_proxy_env | loopback_proxy_only | non_loopback_proxy_rejected | no_proxy_bypass_guarded | unknown`.

These fields must not change `policy_version`, `persona_profile`, `request_shape_profile_ref`, `cache_parity_profile_ref`, `egress_tls_profile_ref`, `env_residue_profile_ref`, `locale_profile_ref`, or `base_url_residue_profile_ref`.

## File map

### Sub2API

- Modify: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/backend/internal/service/cc_gateway_adapter.go`
  - Add default canonical env residue refs.
  - Strip user-supplied env/profile hints from query/body/headers.
  - Add safe observed residue buckets to `observed_client_profile`.
  - Add server-selected canonical refs to formal-pool attestation context.
- Modify/add tests under `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/backend/internal/service/`.
  - Extend existing `cc_gateway_adapter_test.go` and `cc_gateway_tls_profile_contract_test.go`, or add `cc_gateway_env_residue_contract_test.go` if focused tests become clearer.
- Modify: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/backend/internal/service/testdata/cc_gateway_formal_pool_contract/vectors.json`
  - Add the new canonical refs to shared contract vectors.

### CC Gateway

- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/src/config.ts`
  - Add typed `shared_pool.env_residue` config, or equivalent explicit fields, for canonical refs and locale.
  - Validate refs are safe profile refs and canonical defaults in formal-pool mode.
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/src/proxy.ts`
  - Extend `AttestedFormalPoolContext` and parse/verify required canonical residue refs.
  - Extend safe observed profile keys and validation.
  - Add canonical system marker rewrite and final verifier for upstream-bound headers/body.
  - Bind the new refs into the session authority ledger equality check.
- Optional create: `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/src/env-residue-profile.ts`
  - Use this if the rewrite/verifier logic would make `src/proxy.ts` too large. It should export small pure functions:
    - `canonicalEnvResidueProfile()`
    - `canonicalizeSystemEnvResidue(body, now)`
    - `verifyCanonicalEnvResidue(headers, body, profile, now)`
    - `classifyObservedEnvResidue(body)`
- Add tests: `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/tests/formal-pool-env-residue.test.ts`.
- Modify existing tests as needed:
  - `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/tests/proxy-sub2api.test.ts`
  - `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/tests/egress-tls-profile.test.ts`
  - `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/tests/config.test.ts`
  - `/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/tests/session-and-beta-policy.test.ts` if beta/profile verifier ordering is affected.

### Evidence/report

- Create final report: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/docs/anti-ban/72-canonical-local-env-residue-defense-evidence-report.md`.
- Safe evidence root under `/private/tmp/plan72-canonical-local-env-residue-defense-<timestamp>/safe`.

## Checkpoint checklist

### CP0 - Anchor verification and gap acceptance

**Goal:** Confirm Plan72 starts from the intended evidence and accepts Plan71 limitations without overclaiming.

- [ ] Verify Sub2API HEAD includes the Plan71 tool commit and Plan74 report commit or descendants.
- [ ] Verify CC Gateway HEAD includes Plan74 commit `8b3b936f3433f3f2f5e9a3c66579e62db07ff622` or descendant.
- [ ] Read Plan71 and record only safe fields: `logic_confirmed`, `domain_list_confirmed`, `us_pacific_candidate`, `family_dynamic_blocked`, `ready_to_write_design`.
- [ ] Record that `2.1.179 official_anthropic` base-url row remains a dynamic blocker, but does not block a conservative server-selected canonical rewrite/verifier.
- [ ] Record that Desktop/VS Code dynamic remains blocked; Plan72 must allow family observed-only admission without relying on GUI dynamic oracle.
- [ ] Write `$EVIDENCE_ROOT/safe/cp0-anchor-verification.json`.

### CP1 - Sub2API failing tests for server authority

**Goal:** Prove Sub2API does not let client env residue become authority.

Write failing tests first covering:

- [ ] A request with `User-Agent` / observed version `2.1.196` and noncanonical system date marker records safe observed buckets but signs canonical refs:
  - `env_residue_profile_ref=env-residue-profile:claude-code-2.1.179-us-pacific-official-anthropic-v1`
  - `locale_profile_ref=locale-profile:us-pacific-v1`
  - `base_url_residue_profile_ref=base-url-residue-profile:official-anthropic-v1`
  - `policy_version=2.1.179`
- [ ] User-forged query/body/header keys such as `env_residue_profile_ref`, `locale_profile_ref`, `base_url_residue_profile_ref`, `ANTHROPIC_BASE_URL`, `HTTP_PROXY`, `HTTPS_PROXY`, `TZ`, and similar camelCase/kebab-case/snake_case variants cannot alter attested authority refs.
- [ ] User-forged nested authority keys in `metadata`, tool definitions, tool metadata, and tool fields are stripped or treated as observed-only safe buckets and cannot alter attested authority refs.
- [ ] CLI/Desktop/VS Code family hints become `client_family_bucket` only and do not affect canonical refs.
- [ ] Raw body/prompt/domain values are not written into test golden output; assertions use safe buckets only.

Expected result before implementation: FAIL on missing fields or missing stripping/classification.

### CP2 - Sub2API implementation

**Goal:** Implement the server-selected canonical refs and observed-only residue buckets.

- [ ] Add constants for the three canonical refs.
- [ ] Add helper `ccGatewayEnvResidueProfileRef(account)`, `ccGatewayLocaleProfileRef(account)`, and `ccGatewayBaseURLResidueProfileRef(account)` with safe account extra override only if explicitly configured server-side; default to canonical values.
- [ ] Extend client-hint stripping to remove env residue profile hints from query/body/header authority surfaces, including camelCase/kebab-case/snake_case aliases.
- [ ] Extend stripping/classification to allowed structural body fields only: top-level keys, `metadata`, tool definitions, tool metadata, and tool fields. Do not scan or modify `messages[*].content`.
- [ ] Extend observed profile snapshot with safe buckets from system content and family/user-agent classification.
- [ ] Add the three canonical refs to the HMAC-signed formal-pool attestation context.
- [ ] Update shared contract vectors.
- [ ] Run targeted Sub2API tests and record results.

### CP3 - CC Gateway failing tests for attestation/profile admission

**Goal:** Prove CC Gateway requires and binds the new canonical refs.

Write failing tests first covering:

- [ ] Missing `env_residue_profile_ref`, `locale_profile_ref`, or `base_url_residue_profile_ref` fails with `missing_env_residue_profile_ref` or `malformed_formal_pool_context_attestation`.
- [ ] Malformed attestation cases fail closed: bad base64, bad JSON, bad HMAC, duplicate/conflicting semantic fields, and expired timestamp.
- [ ] Unsafe refs fail closed: unknown profile ref, URL-like ref, credential-like ref, raw env-var-like ref, raw domain-list-like ref, newline-bearing ref.
- [ ] Mismatched refs fail with `formal_pool_context_mismatch` or `formal_pool_env_residue_profile_unapproved`.
- [ ] AWS scoped formal-pool context requires the three refs, signs them, validates them, and binds them with `provider_kind`, `upstream_host`, `allowed_upstream_path`, request shape, and cache refs.
- [ ] Session ledger rejects the same session if canonical residue refs change between requests.
- [ ] Observed client residue fields are accepted only from the safe key set and cannot override authority refs.

Expected result before implementation: FAIL.

### CP4 - CC Gateway attestation/profile implementation

**Goal:** Enforce canonical residue refs before any upstream rewrite/send.

- [ ] Extend `Config` with canonical env residue settings, using defaults if absent in non-production tests.
- [ ] Extend `AttestedFormalPoolContext` and `parseFormalPoolContext` required fields.
- [ ] Extend `isSafeObservedClientProfile` to allow the new safe observed-only keys with bounded enum values.
- [ ] Add `verifyFormalPoolEnvResidueProfiles(config, attested, accountIdentity)`.
- [ ] Add new refs to `FormalPoolSessionAuthorityBinding` and `sameFormalPoolSessionAuthority`.
- [ ] Ensure AWS scoped formal-pool verification applies the same env residue profile verification before AWS upstream egress.
- [ ] Update config examples and tests to include the canonical refs where formal-pool strict mode is enabled.

### CP5 - Canonical rewrite and final verifier

**Goal:** Ensure upstream-bound system/header residue is canonical or absent.

- [ ] Implement pure function `canonicalClaudeCodeDateMarker(now, timezone)` that returns ASCII apostrophe + hyphen date for `America/Los_Angeles`.
- [ ] Implement system-only rewrite for recognized Claude Code date markers in JSON `system` string or array text blocks.
- [ ] Test recognized marker variants: ASCII apostrophe + hyphen date, Unicode apostrophe + hyphen date, ASCII apostrophe + slash date, Unicode apostrophe + slash date, and wrong-date-but-recognized marker rewritten to canonical server date.
- [ ] Test unrecognized marker variants fail closed: malformed date, extra embedded classification text, env/base-url literal near the marker, non-standalone marker inside ordinary system instructions, and mixed multiple conflicting markers.
- [ ] Test marker absence is allowed and does not inject a new marker.
- [ ] Test normal system text containing ordinary dates but no Claude Code marker is not rewritten.
- [ ] Do not modify, scan for policy decisions, or rewrite user `messages[*].content`.
- [ ] Do not inject a marker when absent.
- [ ] Reject upstream-bound system content containing raw `ANTHROPIC_BASE_URL`, `HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY`, `NO_PROXY`, or `TZ=` literals.
- [ ] Reject unrecognized date marker variants after rewrite.
- [ ] Verify upstream headers and URL/query do not contain local env/proxy/base-url authority headers or forwarded local env residues.
- [ ] Verify allowed structural body authority fields do not contain env/profile/base-url/TZ/proxy hints: top-level keys, `metadata`, tool definitions, tool metadata, and tool fields. Exclude `messages[*].content` from this scan/rewrite.
- [ ] Run final verifier after billing/CCH strip/sign rewrite and before sidecar/upstream egress on every attempt.
- [ ] Return explicit fail-closed error code, recommended: `formal_pool_env_residue_verifier_failed`.

### CP6 - Family observed-only admission

**Goal:** Ensure Claude Code CLI/Desktop/VS Code can enter as observed-only families without changing upstream authority.

- [ ] Add/extend tests for user agents or safe family hints for:
  - Claude Code CLI
  - Claude Code Desktop
  - official Claude Code VS Code extension
- [ ] Each family should set `client_family_bucket` only.
- [ ] Unknown family can be observed as `unknown` if version/body shape is otherwise approved, but must not receive extra authority.
- [ ] No family may change canonical refs, user-agent rewrite, beta policy, CCH/billing policy, TLS profile, or locale/base-url residue profile.

### CP7 - Local mock E2E and regression

**Goal:** Prove the full local mock chain canonicalizes residue and preserves Plan74 transport gates.

Run a local mock E2E chain:

```text
client fixture with noncanonical local env residue
  -> Sub2API formal-pool attestation
  -> CC Gateway final verifier/rewrite
  -> mock upstream or Plan74 local-only sidecar path as applicable
```

Required assertions:

- [ ] Upstream captured body has canonical marker if a marker was present.
- [ ] Upstream captured body has no noncanonical apostrophe/date separator residue.
- [ ] Upstream captured headers and query have no local env/proxy/base-url residue.
- [ ] Upstream captured allowed structural body fields have no env/profile/base-url/TZ/proxy authority hints, including nested metadata and tool fields.
- [ ] `observed_client_profile` records safe residue buckets but authority refs stay canonical.
- [ ] AWS scoped formal-pool path includes the three refs in signed context, binds them in session authority, and runs the final verifier before AWS upstream egress.
- [ ] Retry/replay path: every upstream attempt reruns canonical rewrite + final verifier and cannot reuse an unverified body.
- [ ] Billing/CCH strip behavior remains unchanged.
- [ ] Node direct HTTPS fallback remains `0` in sidecar-enabled formal-pool tests.
- [ ] No real upstream requests occur.

Minimum commands:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/backend
go test ./internal/service -run 'CCGateway|FormalPool|ObservedProfile|TLSProfile|EnvResidue|LocalEnv' -count=1

cd /Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5
npx tsx tests/formal-pool-env-residue.test.ts
npx tsx tests/proxy-sub2api.test.ts
npx tsx tests/egress-tls-profile.test.ts
npx tsx tests/egress-tls-sidecar.test.ts
npx tsc --noEmit
```

If test names differ, record the actual targeted command and why it covers these gates.

### CP8 - Leak scan, report, and review

**Goal:** Produce safe evidence and decide readiness.

- [ ] Scan modified Sub2API files, modified CC Gateway files, tests, docs, and `$EVIDENCE_ROOT/safe`.
- [ ] Fail on raw body/prompt/response, raw decoded domain/keyword list, raw ClientHello/pcap, secrets, tokens, key/cert material, account UUID/email, workspace IDs, or proxy credentials.
- [ ] Scan generated evidence schema keys, error codes, and reason strings so they do not include raw env/domain/token fragments.
- [ ] Generate `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/docs/anti-ban/72-canonical-local-env-residue-defense-evidence-report.md`.
- [ ] Include CP0-CP8 statuses, safe evidence root, tests, code changes, final decision, and remaining blockers.
- [ ] Dispatch one review agent for final review. Required review focus:
  - client authority injection
  - fail-open verifier gaps
  - accidental family blocking
  - raw evidence leakage
  - real upstream/production touch risk
  - interaction with Plan73/74 TLS sidecar fail-closed behavior
- [ ] Commit CC Gateway code separately from Sub2API code/docs.

## Acceptance criteria

To return `CANONICAL_ENV_RESIDUE_MOCK_E2E_READY`, all must be true:

- Sub2API signs the three canonical residue authority refs from server/account state only.
- Client-supplied env/profile/base-url/TZ/proxy hints cannot alter authority refs.
- CC Gateway verifies the refs and binds them in session authority.
- Upstream-bound system residue is canonical or absent.
- Final verifier covers headers, URL/query, system content, and allowed structural body authority fields. It explicitly excludes user `messages[*].content` from scanning/rewrite.
- Noncanonical or raw env/base-url/proxy residue in system/header/query/allowed structural body fields fails closed.
- CLI/Desktop/VS Code family indicators are observed-only and do not change upstream authority.
- Existing canonical `2.1.179` version lock, strip billing/CCH policy, and Plan73/74 TLS sidecar behavior remain intact.
- All targeted tests pass.
- Leak scan reports zero blocking findings.
- No production service or real upstream is touched.

## Non-goals

- No production deployment.
- No live canary.
- No canonical promotion to `2.1.185` or `2.1.196`.
- No full ban/risk model claim.
- No IP/ASN/payment/account-age/concurrency risk scoring.
- No Desktop or VS Code dynamic oracle claim.
- No raw hardcoded domain list publication or persistence.
