# 59 - Claude Platform on AWS Formal-Pool Integration Plan

> **For agentic workers:** This is a design and execution plan for adding **Claude Platform on AWS** as a separate Anthropic-compatible upstream in Sub2API and CC Gateway. Implement task-by-task with TDD and review gates. Do not implement this by modifying the existing Bedrock card/path in-place.

**Goal:** Add a new **Claude Platform on AWS** account/card that can import multiple `anthropic_workspace_id` workspaces, bind each workspace to its own egress proxy, and route native Claude formal-pool traffic safely through Sub2API -> CC Gateway -> Claude Platform on AWS.

**Recommended baseline:** API-key authentication first, with two mutually exclusive Phase 1 auth profiles: `x_api_key` and `bearer_api_key`. `x_api_key` is the target implementation profile because current Anthropic Claude Platform on AWS docs say `apiKey` / `ANTHROPIC_AWS_API_KEY` resolves to `x-api-key`, but this is not a proven production fact for our target account. Checkpoint 0 must prove which exact profile works for the target workspace/endpoint/credential; production enables only the proven profile. If neither profile is proven, production remains fail-closed. SigV4 is a later gated phase because final SigV4 signing must happen after CC Gateway body/header rewrite and final verifier.

**Tech stack / working roots:**

- Sub2API plan/review worktree: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool` on branch `codex/claude-platform-aws-formal-pool`, created from the doc-58 merge candidate `codex/merge-formal-pool-58-to-main @ 3fc082748a3b`. Do not touch the dirty main checkout.
- CC Gateway target remains `/Users/muqihang/chelingxi_workspace/cc-gateway` at `443052a` / main `c37a234+` lineage; do not use `cc-gateway/.worktrees/claude-code-2173-main`.
- Verified doc-58 safety snapshots for this review are Sub2API `b6c999481` and CC Gateway `443052a`. Docs 56 and 58 remain mandatory safety boundaries for formal-pool native Claude traffic.
- Doc 59 implementation must treat doc 58 as an already-landed baseline and must first verify those fields/tests are present in the implementation branches. If the doc-58 trusted context/profile fields, persistent session ledger/fail-closed semantics, final verifier, `strip_attribution` default, or signed-CCH fail-closed gates are missing, Checkpoint 0 blocks implementation instead of recreating or bypassing them. Additive account/UI work may be prepared earlier only if feature-flagged and not enabled for formal-pool production.

## 0. 2026-06-26 alignment verdict

Status after re-review against the completed doc-58 baseline: `PASS_WITH_REQUIRED_EDITS` before this revision, `READY_FOR_IMPLEMENTATION_WITH_CP0_HARD_GATE` after the edits in this document. The original design direction remains correct: Claude Platform on AWS is neither Bedrock nor Vertex, one workspace is one schedulable formal-pool account identity, Phase 1 uses API-key mode, and CC Gateway remains the final safety boundary. Code implementation may start after document approval, but production enablement is blocked until CP0 proves the target workspace, endpoint, region, auth profile, and request shape.

Required clarifications now incorporated:

- Work roots now point to the new 59 worktree and the verified 58 snapshots instead of the older multiprovider worktree.
- Checkpoint 0 is now a hard baseline/protocol verification gate, not a vague "rebase on 58 later" note. It blocks production enablement until it proves the target endpoint `https://aws-external-anthropic.us-east-1.api.aws`, target workspace ID shape `wrkspc_...`, region/workspace consistency, auth scheme, and final `/v1/messages` host/path/query/header/body shape.
- Multi-workspace import now requires idempotent per-row semantics, explicit proxy binding, and safe duplicate handling without raw workspace IDs in logs/evidence.
- AWS Platform beta/header policy is provider-scoped: it may share the doc-58 request-shape/profile framework, but it must not reuse Vertex or Bedrock header policy blindly.
- Final verifier scope is explicit for endpoint host, region, path/query, auth scheme, workspace header source, internal-header stripping, billing/CCH, cache/request-shape profile, and no direct fallback.
- Production readiness remains blocked until CP0, implementation, targeted tests, local full-chain mock E2E, evidence updates, deployed equivalence, and tiny approved live smoke are green; this plan alone is not production evidence. Until then, AWS Platform formal-pool must stay feature-flagged, disabled, or fail-closed.

## 1. Confirmed external facts

Official AWS Claude Platform documentation establishes these facts:

- Claude Platform on AWS uses the Claude API surface and the Anthropic Messages API (`/v1/messages`) for inference. It differs from first-party Claude mainly by base URL, authentication method, AWS workspace scoping, and the required `anthropic-workspace-id` header. See [AWS Making requests](https://docs.aws.amazon.com/claude-platform/latest/userguide/making-requests.html). Phase 1 formal-pool routing is limited to `/v1/messages` unless a later request-shape profile explicitly allows additional endpoints such as count-tokens/files/batches/agents.
- User-provided target endpoint for this rollout is `https://aws-external-anthropic.us-east-1.api.aws`; therefore the initial configured `aws_region` must be `us-east-1`, and the workspace must be a workspace bound to `us-east-1`. The raw workspace ID is not recorded in this plan; only the `wrkspc_...` format and a safe workspace ref may appear in evidence. CP0 must fail closed if the workspace region cannot be proven to match `us-east-1`.
- The regional endpoint shape is `https://aws-external-anthropic.<region>.api.aws`. See [AWS Authentication](https://docs.aws.amazon.com/claude-platform/latest/userguide/authentication.html).
- Each data-plane request must include `anthropic-workspace-id`; SDKs can read it from `ANTHROPIC_AWS_WORKSPACE_ID`, and base Anthropic clients must pass the header explicitly. See [AWS Workspaces](https://docs.aws.amazon.com/claude-platform/latest/userguide/workspaces.html).
- Workspaces are region-scoped. The workspace ID belongs to the same ARN resource namespace used by IAM: `arn:aws:aws-external-anthropic:{region}:{account-id}:workspace/{workspace-id}`. See [AWS Workspaces](https://docs.aws.amazon.com/claude-platform/latest/userguide/workspaces.html).
- Claude Platform on AWS supports IAM SigV4 and API-key authentication. For SigV4, the service name is `aws-external-anthropic`, and the SigV4 region must match the endpoint region. See [AWS Making requests](https://docs.aws.amazon.com/claude-platform/latest/userguide/making-requests.html) and [AWS Authentication](https://docs.aws.amazon.com/claude-platform/latest/userguide/authentication.html).
- Current Anthropic Claude Platform on AWS documentation states the platform-specific SDK credential precedence as `apiKey` constructor argument -> `x-api-key` header and `ANTHROPIC_AWS_API_KEY` -> `x-api-key` header. The AWS User Guide also describes API-key authentication in terms of bearer-token authorization. To avoid an unsafe/wrong hard-code, Phase 1 treats both `x_api_key` and `bearer_api_key` as mutually exclusive gated profiles rather than already-proven production facts: Checkpoint 0 must record safe protocol evidence for the exact SDK/manual request path before production. If evidence for both profiles fails or is unavailable, production remains blocked. Silent fallback between the profiles is forbidden. See [Anthropic Claude Platform on AWS](https://platform.claude.com/docs/en/build-with-claude/claude-platform-on-aws).
- API keys for this service are not standard Claude Console keys and not Bedrock API keys. AWS docs state Claude Platform on AWS API keys are generated under AWS Console -> Claude Platform on AWS -> API keys, and Bedrock API keys do not work for this endpoint. See [AWS Authentication](https://docs.aws.amazon.com/claude-platform/latest/userguide/authentication.html). Short-term AWS-generated API keys/tokens are still treated as raw credentials: they may be stored only in sensitive credential storage, must receive their own `credential_ref`, must expire/refresh fail-closed, and must not appear in logs/evidence.

## 2. Current local code observations

Source inspection in the 59 worktree plus CC Gateway CodeGraph inspection confirms the screenshot is the existing **AWS Bedrock** path, not Claude Platform on AWS:

- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/backend/internal/domain/constants.go` defines `AccountTypeBedrock = "bedrock"` and `DefaultBedrockModelMapping`, which maps Anthropic model names to Bedrock model IDs.
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/backend/internal/service/bedrock_signer.go` signs requests with SigV4 service name `bedrock`, not `aws-external-anthropic`.
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/backend/internal/service/bedrock_request.go` prepares Bedrock-specific request bodies, including Bedrock `anthropic_version` conversion and unsupported-field stripping.
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool/frontend/src/components/account/CreateAccountModal.vue` currently has an Anthropic account card named `AWS Bedrock` with `SigV4 / API Key` modes. This card should remain Bedrock-only.
- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/proxy.ts` currently models formal-pool account context with provider/account/token/credential/egress/policy/session/profile fields but no `workspace_ref`, AWS endpoint kind, or auth scheme.
- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/rewriter.ts` currently preserves `authorization` for OAuth or `x-api-key` for API-key upstreams; it does not yet know provider-specific Claude Platform on AWS workspace injection or how to ensure the upstream API-key header is selected only from verified server-owned credentials.
- Current Sub2API account type validation and frontend typings are not generic enough for a new Anthropic type without explicit changes: `backend/internal/domain/constants.go`, `backend/internal/handler/admin/account_handler.go`, and `frontend/src/types/index.ts` need first-class `claude-platform-aws` support.
- Current Sub2API formal-pool predicates primarily target OAuth/setup-token accounts. `IsFormalPoolAccount`, `serviceFormalPoolAccount`, and `formalPoolRuntimeReplayEligible` must be extended via a dedicated formal-pool eligibility predicate rather than by treating the new type as ordinary `apikey` or Bedrock.
- Current Sub2API -> CC Gateway attestation serialization must be verified for canonical parity with CC Gateway's sorted JSON verifier before any new fields are added; otherwise the new context can be rejected even when semantically correct.
- Current CC Gateway runtime registration/config/persistence schema has no provider/workspace/upstream endpoint fields, and current upstream-safety allowlisting does not allow `aws-external-anthropic.<region>.api.aws`; both must be extended explicitly.

Conclusion: the current Bedrock card/path is not the same service and must not be overloaded.

## 3. User requirements restated

1. Add a new UI card: **Claude Platform on AWS**.
2. Do not break existing Anthropic OAuth, Setup Token, Claude Console API-key, or AWS Bedrock login/add-account flows.
3. Support multiple `anthropic_workspace_id` values.
4. Each workspace entry must have a selected egress proxy.
5. In formal-pool production, authority must remain server-owned per doc 56:
   - Sub2API strips/rejects user-forged authority headers.
   - Sub2API generates trusted context from server scheduler state only.
   - CC Gateway independently verifies account, credential, egress, persona/profile, session, metadata identity, billing/CCH, and final output.
6. Incorporate doc 58:
   - `observed_client_profile` is audit-only.
   - `trusted_egress_profile_ref` and profile fields are authoritative.
   - Default shared formal-pool remains `strip_attribution`.
   - `signed_cch` / sign-primary remain fail-closed unless oracle/profile gates explicitly pass.
7. Do not log or persist evidence containing API keys, tokens, cookies, Authorization, `x-api-key`, raw prompt/body/response/telemetry/CCH, raw account identity, proxy credentials, raw workspace IDs, or raw HMAC input/output. Raw workspace IDs may exist only in sensitive credential/runtime storage.

## 4. Core design decision

### 4.1 Create a separate account type

Add a new Anthropic-platform account type:

```text
platform = "anthropic"
type     = "claude-platform-aws"
```

User-facing label:

```text
Claude Platform on AWS
```

This type is distinct from:

- `oauth` / `setup-token`: first-party Claude Code OAuth-like accounts.
- `apikey`: first-party Claude Console API keys.
- `bedrock`: Amazon Bedrock Runtime.
- `service_account`: Vertex Anthropic.
- `upstream`: generic base-url passthrough.

Implementation isolation constraints:

- Do not add `claude-platform-aws` to `IsAnthropicOAuthOrSetupToken()` or any helper whose contract means first-party Claude Code OAuth/setup-token identity. AWS Platform is not OAuth/setup-token even when the outer platform is `anthropic`.
- Do not route `claude-platform-aws` through first-party `apikey`, Bedrock, Vertex `service_account`, or generic `upstream` builders. It must have provider-specific validation, scheduler eligibility, header policy, and direct-test builder.
- Do not let adding this type widen existing OAuth/Setup Token/API-key/Bedrock/Vertex/generic-upstream schedulability or login behavior. Tests must prove those flows remain unchanged.
- Formal-pool schedulability must use a dedicated eligibility predicate such as `IsFormalPoolEligibleAccount(account)` or a narrower `IsClaudePlatformAWSFormalPoolAccount(account)`. Eligibility requires account safe refs, credential binding, `workspace_ref`, `workspace_binding_hmac`, endpoint/region refs, proxy/egress binding, persona/profile refs, provider-scoped request-shape/cache/beta profile refs, and runtime registration readiness. Missing any required ref fails closed.

### 4.2 API-key MVP, SigV4 gated phase

Phase 1 implements Claude Platform on AWS API-key mode only, but the concrete wire auth scheme is profile-gated and mutually exclusive:

| Profile | Final auth header | Forbidden in same final request | Enablement rule |
|---|---|---|---|
| `x_api_key` | `x-api-key: <selected credential>` | `Authorization` | Enabled only if CP0 proves the exact target credential works with `x-api-key` for `https://aws-external-anthropic.us-east-1.api.aws/v1/messages`. |
| `bearer_api_key` | `Authorization: Bearer <selected credential>` | `x-api-key` | Enabled only if CP0 proves the exact target credential works with bearer-token auth for the same endpoint/workspace/request shape. |

Rules:

- `x_api_key` remains the Phase 1 target implementation profile because current Anthropic SDK documentation resolves API-key auth to `x-api-key`, but it is not a production fact until CP0 passes.
- `bearer_api_key` is not a fallback. It is a separate profile with separate CP0 evidence, tests, profile refs, final-verifier checks, and evidence status.
- Exactly one auth profile may be selected for a schedulable AWS Platform account/session. The selected `upstream_auth_scheme` is part of the sticky tuple and CC Gateway session ledger.
- If neither profile is proven, production enablement for AWS Platform formal-pool remains `BLOCKED_AUTH_PROFILE` / fail-closed. Silent fallback between profiles is forbidden.
- This API-key phase avoids adding a final SigV4 signer before the formal-pool rewrite/final-verifier path is fully covered.

Phase 2 adds SigV4 only after targeted tests prove signing happens after CC Gateway final body/header rewrite and uses service `aws-external-anthropic` with endpoint-region equality.

### 4.3 One workspace is one schedulable account identity

For correctness and sticky scheduling, treat each `anthropic_workspace_id` as a distinct Sub2API account row and a distinct CC Gateway account identity.

Multiple workspaces can be added in one UI operation, but the backend should create one account row per workspace:

```text
Claude Platform on AWS import batch
  row 1: workspace A + region + credential + proxy X -> account A
  row 2: workspace B + region + credential + proxy Y -> account B
  row 3: workspace C + region + credential + proxy Z -> account C
```

This keeps quota, concurrency, scheduler state, sticky session binding, egress proxy, healthcheck status, and evidence simple and auditable.

Implementation MUST introduce a formal-pool eligibility helper, for example:

```go
func IsClaudePlatformAWSFormalPoolAccount(account *Account) bool
func IsFormalPoolEligibleAccount(account *Account) bool
```

`IsFormalPoolEligibleAccount` should include existing OAuth/setup-token formal-pool accounts and `claude-platform-aws` accounts only when required safe refs, workspace ref, credential binding, proxy/egress binding, and profile fields are present. This avoids accidentally making every Anthropic API-key account formal-pool eligible.

### 4.4 Provider-scoped beta/header/request-shape policy

Claude Platform on AWS is an Anthropic-operated Claude API surface, but it is still a distinct provider path from first-party Anthropic, Vertex Anthropic, and Bedrock. PR #3375 / #3150 and issue #3358 identify a Vertex Anthropic `anthropic-beta` filtering problem; that is not automatically the same problem as AWS Platform. Do not copy the Vertex beta-token denylist into this plan as a Vertex fix, and do not assume all native Claude Code beta tokens are safe for AWS Platform without a provider profile.

Required policy shape:

- Reuse the doc-58 `request_shape_profile_ref` / `cache_parity_profile_ref` mechanism, but add a provider-scoped profile such as `claude_platform_aws_native_cli_2_1_179_strip_v1`.
- The profile owns the final `anthropic-beta` value, unknown beta-token disposition, tool-schema feature flags, `thinking`, `context_management`, `output_config`, diagnostics, cache-control placement, and any provider-specific fields such as future `inference_geo`.
- The Vertex issue tokens (`advisor-tool-2026-03-01`, `prompt-caching-scope-2026-01-05`, `redact-thinking-2026-02-12`, `thinking-token-count-2026-05-13`) must appear in AWS Platform tests as fixtures proving AWS policy is independent: either allowed by an explicit AWS profile, stripped/downscoped by that profile, or rejected fail-closed. They must not be forwarded merely because a Vertex workaround exists elsewhere.
- If current code shares a builder/header policy between Vertex, Bedrock, first-party Anthropic, and AWS Platform, Checkpoint 3/5 must split provider-specific policy before production enablement. AWS Platform implementation must not blindly forward Vertex/first-party/Bedrock beta tokens or header/body extensions.
- Client-supplied `anthropic-beta` is never authority. Sub2API records only safe observed names, and CC Gateway rewrites/verifies the final beta header from the trusted provider profile. Any beta/header/body extension not explicitly allowed by `claude_platform_aws_native_cli_2_1_179_strip_v1` must be stripped/downscoped or fail-closed.
- This section does not expand doc 59 into a Vertex fix; it prevents an AWS production risk if shared code would otherwise leak unsupported beta/header shape.

## 5. Data model

### 5.1 Credentials stored by Sub2API

For `type = "claude-platform-aws"`, store credentials with this shape:

```json
{
  "auth_mode": "apikey",
  "api_key": "<secret; never log>",
  "aws_region": "us-west-2",
  "anthropic_workspace_id": "<workspace id; redact in evidence>",
  "base_url": "https://aws-external-anthropic.us-west-2.api.aws"
}
```

Validation rules:

- `auth_mode` must be `apikey` in Phase 1.
- `api_key` must be non-empty and must never be returned in ordinary API responses except explicit admin backup/export paths that already expose credentials by design.
- `aws_region` must be a valid AWS region string and must equal the region embedded in `base_url`.
- `base_url` must be derived from `aws_region` in production. Do not allow arbitrary hosts in formal-pool production.
- `anthropic_workspace_id` must match the AWS documented tagged workspace form, e.g. `wrkspc_` followed by alphanumeric characters. Do not write real workspace IDs into logs or evidence.
- `proxy_id` is required for this account type.
- Batch imports must be idempotent by a safe server-derived key such as `(provider_kind, aws_region, workspace_ref, credential_ref)` or an explicit admin-provided external name plus workspace binding. Duplicate handling must not compare or log raw workspace IDs.
- If a future `inference_geo` override is supported, it must be a scheduler/profile-owned field and included in attested context/final verifier. Phase 1 should not accept client-supplied `inference_geo` as authority.

Redaction/storage rules:

- Add `anthropic_workspace_id` to backend and DTO credential redaction helpers, or store the raw workspace ID only in the same sensitive credential/runtime storage as `api_key` while exposing only `anthropic_aws_workspace_ref` in ordinary DTOs. Raw workspace ID must not appear in ordinary logs, evidence, scheduler artifacts, DTOs, or test artifacts.
- Admin backup/export paths that intentionally export raw credentials must be documented as sensitive exports and must not be used as evidence artifacts.
- Logs, ops errors, healthcheck results, and formal-pool evidence must show only `workspace_ref`, region, endpoint ref, booleans, and status codes.
- Sub2API scheduler snapshots, scheduler cache metadata, admin account DTOs, healthcheck DTOs, formal-pool diagnostics, ops-log payloads, risk events, replay fixtures, and test evidence must use explicit allowlists. They may include `workspace_ref`, `endpoint_ref`, `aws_region`, `upstream_auth_scheme`, profile refs, booleans, and status-code buckets; they must not include raw `anthropic_workspace_id`, raw API keys, `Authorization`, `x-api-key`, raw credential digests, raw HMAC input/output, proxy credentials, raw prompts/bodies/responses, or cookies.
- Any new field added to `Account.Extra`, scheduler metadata, or frontend types must be classified as safe-ref, sensitive, or internal-only before implementation. Unknown `claude-platform-aws` credential fields must default to redacted/omitted in ordinary API responses.

### 5.2 Safe refs stored in account extra

Generate safe refs from server-owned secrets, never from client-supplied headers:

```json
{
  "cc_gateway_account_ref": "account:<safe-ref>",
  "cc_gateway_credential_ref": "credential:<safe-ref>",
  "cc_gateway_credential_binding_hmac": "hmac-sha256:<safe-ref>",
  "cc_gateway_egress_bucket": "egress:<safe-ref>",
  "anthropic_aws_workspace_ref": "workspace:<safe-ref>",
  "anthropic_aws_endpoint_ref": "endpoint:<safe-ref>",
  "anthropic_aws_region": "us-west-2",
  "anthropic_aws_auth_scheme": "x_api_key",
  "anthropic_aws_request_shape_profile_ref": "request-shape:<safe-ref>"
}
```

Raw `anthropic_workspace_id`, raw API key, and raw HMAC inputs must remain outside logs/evidence. Safe refs are acceptable in evidence.

### 5.3 CC Gateway runtime/config identity

Extend CC Gateway account identity records with provider/upstream metadata:

```ts
type AccountIdentityConfig = {
  // existing fields
  device_id: string
  account_ref?: string
  credential_ref?: string
  credential_binding_hmac?: string
  token_type?: 'oauth' | 'apikey'
  persona_variant: string
  session_policy: 'preserve_downstream_session_id'
  policy_version: string

  // new fields for Claude Platform on AWS
  provider_kind?: 'anthropic_first_party' | 'claude_platform_aws'
  upstream_auth_scheme?: 'x_api_key' | 'bearer_api_key' | 'sigv4'
  aws_region?: string
  upstream_base_url?: string
  workspace_ref?: string
  workspace_binding_hmac?: string
  endpoint_ref?: string
  allowed_upstream_paths?: string[]
  beta_policy_ref?: string
  request_shape_profile_ref?: string
  cache_parity_profile_ref?: string
  // raw workspace id may exist only in sensitive runtime/config storage,
  // not in evidence/log output.
  anthropic_workspace_id?: string
}
```

For Phase 1, `upstream_auth_scheme` must be either `x_api_key` or `bearer_api_key`, never both. `x_api_key` is the target implementation profile for Claude Platform on AWS, not an unconditional production claim. CC Gateway receives the selected credential through the protected internal API-key path, verifies the credential binding, then emits exactly the header required by the proven profile. It must not forward client-supplied `Authorization` or `x-api-key`.

### 5.4 Workspace binding HMAC

Use one precise binding value throughout Sub2API, attestation, runtime registration, runtime persistence, and CC Gateway final verification:

```text
workspace_ref = hmac-sha256(ref_secret,
  "claude_platform_aws_workspace_ref_v1" || NUL || aws_region || NUL || raw_anthropic_workspace_id)

workspace_binding_hmac = hmac-sha256(binding_secret,
  "claude_platform_aws_workspace_binding_v1" || NUL ||
  provider_kind || NUL || account_ref || NUL || credential_ref || NUL ||
  workspace_ref || NUL || endpoint_ref || NUL || aws_region || NUL ||
  upstream_auth_scheme || NUL || egress_bucket || NUL || proxy_identity_ref)
```

Rules:

- `ref_secret` and `binding_secret` must be server/gateway formal-pool secrets unavailable to end users and distinct from ordinary client/API tokens. If Sub2API and CC Gateway both verify the binding, use a shared formal-pool binding secret or a key-derivation scheme explicitly configured on both sides.
- Raw workspace ID may exist only in sensitive credential/runtime storage. Raw workspace ID, raw HMAC input, and raw HMAC output must not appear in logs, errors, evidence, ordinary DTOs, scheduler artifacts, or test artifacts. Safe evidence may show only `workspace_ref`, `workspace_binding_hmac_present: true`, and boolean verification status.
- Runtime registration must verify `workspace_binding_hmac` against the selected account, credential, endpoint, region, auth scheme, egress bucket, and proxy identity before persisting the mapping.
- Formal-pool attestation must carry `workspace_ref` and `workspace_binding_hmac`; CC Gateway must verify they match the runtime mapping and selected account identity before trusting any AWS workspace field.
- The session ledger must bind `workspace_ref` and `workspace_binding_hmac`; the same formal-pool session cannot change either value.
- Final verifier must verify that the final `anthropic-workspace-id` header was injected from the sensitive runtime mapping whose raw workspace ID recomputes to `workspace_ref`. It must reject user-supplied or mismatched workspace headers.
- Reusing one raw API key across multiple workspaces is allowed only as multiple account identities: each workspace row still has a distinct `workspace_ref`, `workspace_binding_hmac`, account/session binding, and proxy/egress binding.

### 5.5 Sub2API sticky tuple persistence

Existing sticky `session -> account_id` behavior is not sufficient for Claude Platform on AWS formal-pool production. Sub2API must persist and verify the complete AWS authority tuple for each formal-pool session before producing trusted context:

```text
session_ref ->
  provider_kind,
  account_ref,
  credential_ref,
  workspace_ref,
  workspace_binding_hmac,
  endpoint_ref,
  aws_region,
  upstream_auth_scheme,
  egress_bucket,
  proxy_identity_ref,
  persona_profile,
  policy_version,
  trusted_egress_profile_ref,
  request_shape_profile_ref,
  cache_parity_profile_ref,
  beta_policy_ref,
  device_ref/session policy
```

Rules:

- A later request for the same canonical formal-pool session that changes any tuple field fails closed before CC Gateway forwarding. It must not silently reschedule to another AWS workspace, credential, endpoint, proxy, profile, or beta policy.
- Scheduler cache and replay metadata must include only safe refs for this tuple. Raw workspace IDs, raw credentials, raw HMAC inputs, and proxy credentials are forbidden in sticky/session artifacts.
- If sticky storage is unavailable or cannot atomically compare-and-set the tuple, formal-pool production admission for `claude-platform-aws` fails closed. Personal standalone first-party behavior must not be affected.

### 5.6 Non-downgrade security invariants from doc 58

Doc 59 is additive. It must not weaken the completed doc-58 formal-pool boundary:

- Sub2API can generate trusted AWS Platform formal-pool context only from server-side scheduler/account/runtime state. End-client input is observation only.
- Client-supplied account, credential, egress, persona, profile, session, billing/CCH, control-plane, auth, workspace, beta, runtime, policy, and internal context headers must be stripped, ignored, or rejected before trusted context creation.
- CC Gateway must independently verify account identity, credential binding, `workspace_ref` + `workspace_binding_hmac`, region/endpoint/path/query, egress bucket + proxy identity, persona/profile, session binding, `metadata.user_id`, billing/CCH strip/default policy, provider-scoped beta/request-shape/cache profile, and no-direct-fallback behavior before real AWS egress.
- `strip_attribution` remains the shared formal-pool default. `no_cch`, `signed_cch`, sign-primary, Bearer auth, and SigV4 are profile-gated and disabled unless their named proof gates pass.
- Provider final verifier must run after final URL/header/body construction. After it passes, no code may mutate auth, workspace, beta, body, path, query, billing/CCH, persona, profile, or other semantic authority fields. Only transport-only mechanics that cannot alter authority or request semantics are allowed.

## 6. Request flow

### 6.1 Admin import flow

```text
Admin UI
  -> selects "Claude Platform on AWS"
  -> enters one or more workspace rows
       workspace_id + region + api_key + proxy_id + optional name/concurrency/group
  -> Sub2API validates all rows
  -> Sub2API creates one account per row
  -> Sub2API generates safe refs and credential/workspace bindings
  -> Sub2API registers/updates CC Gateway runtime identity for each account
  -> Sub2API records per-row safe status and optional proxy healthcheck summary
```

Every row is independently schedulable only after region/workspace/credential/proxy binding and CC Gateway runtime registration succeed. Batch creation should be all-or-nothing if practical. If the existing repository layer makes full transactionality too invasive, the UI/API must return a per-row result and mark partial success explicitly; it must not silently drop failed workspace rows.

Concrete API choice for implementation:

- Add a dedicated admin batch route/service: `POST /admin/accounts/claude-platform-aws/batch`.
- Request shape: `idempotency_key`, optional batch name/group defaults, and `rows[]` where each row contains raw `workspace_id`, `aws_region`, API-key credential input or credential selector, `proxy_id`, optional display name suffix, optional concurrency/quota/group overrides, and optional operator notes. Raw row fields are accepted only on the admin write path and must be redacted before logs/errors/evidence.
- Semantics: validate all rows first; derive endpoint from region; derive safe refs; verify proxy existence; verify duplicate/idempotency state; then create account rows. If the repository supports a transaction, use all-or-nothing. If it cannot, the service must implement explicit per-row states (`validated`, `created`, `skipped_duplicate`, `failed_validation`, `failed_registration`) and must never silently drop a row.
- Idempotency/duplicates: `idempotency_key` plus safe server-derived row key `(provider_kind, aws_region, workspace_ref, credential_ref, proxy_identity_ref)` controls retry behavior. Duplicate responses may include existing safe account refs but never raw workspace IDs or raw credential material.
- Response fields must be safe: batch id/ref, row index, status, stable error code, region, `workspace_ref`, `credential_ref`, `proxy_identity_ref`, `account_ref`, redacted endpoint ref, and registration/health booleans. Do not include raw workspace IDs, API keys, proxy credentials, request/response bodies, or raw HMAC inputs.
- The frontend must call this dedicated route for multi-workspace import. Do not overload the existing `POST /admin/accounts` single-row API with an ambiguous multi-row payload, and do not implement the multi-workspace UX as untracked parallel single-row calls.

### 6.2 Formal-pool request flow

```text
User / unmodified Claude Code CLI
  -> Sub2API / Server API
     -> authenticate end user
     -> strip/reject user-forged authority headers
     -> scheduler selects one claude-platform-aws account row
     -> sticky session binds account + credential + workspace + proxy + profile tuple
     -> trusted context is signed from server state
  -> CC Gateway
     -> verifies attested context
     -> resolves account identity and workspace_ref
     -> verifies credential/account/workspace binding
     -> verifies egress allowlist and proxy_identity_ref
     -> applies doc 58 persona/profile/billing/CCH policy
     -> rewrites metadata.user_id/session/header/body
     -> injects exactly one server-owned anthropic-workspace-id header
     -> emits the selected API key using the verified AWS Claude Platform auth scheme
     -> final verifier before upstream
  -> https://aws-external-anthropic.<region>.api.aws/v1/messages
     through the selected proxy
```

If the existing formal-pool compatibility route uses an internal marker such as `/v1/messages?beta=true`, that marker is not upstream authority. CC Gateway must translate or verify the final provider path/query according to the AWS request-shape profile; Phase 1 final AWS egress should be `/v1/messages` with no unapproved query parameters unless targeted evidence proves the query is accepted and policy-owned.

### 6.3 Direct non-formal-pool path

If a non-formal-pool admin test route or account test route sends directly from Sub2API to AWS, it may inject `anthropic-workspace-id` directly from the selected server-owned account. That path is not a substitute for production formal-pool traffic. Production native CLI formal-pool traffic must go through CC Gateway when doc 56/58 safety is required.

The direct path still needs a dedicated dispatcher/builder for `type = "claude-platform-aws"`. It must not reuse the existing first-party Anthropic `apikey` passthrough blindly because that path does not validate workspace/region/base URL and may reject unknown account types in token selection. The builder must:

- derive `https://aws-external-anthropic.<region>.api.aws/v1/messages`;
- inject server-owned `anthropic-workspace-id`;
- use `x-api-key` for Phase 1;
- keep native Claude model IDs unchanged;
- avoid Bedrock model mapping/body conversion.

## 7. Header and authority rules

### 7.1 Inbound from end users

Sub2API must strip, ignore, or reject these end-user-supplied headers before any trusted context is built; none of them may become authority for AWS Platform formal-pool routing:

- `x-cc-*`
- `x-sub2api-*`
- `authorization`
- `x-api-key`
- `anthropic-workspace-id`
- `x-amz-*`
- persona/context/runtime/account/credential/egress/policy/session headers
- billing/CCH/control-plane/cache/profile/request-shape authority headers
- any future header that can influence account, credential, workspace, proxy, persona, policy, billing/CCH, auth scheme, route shape, or session identity

For native Claude Code users, this does not break the client. The user client is not the authority for the AWS workspace. Sub2API/CC Gateway re-inject the correct workspace header after scheduler selection.

### 7.2 Sub2API -> CC Gateway internal headers

Sub2API may send internal selected credential material only after scheduler selection and only through the existing protected CC Gateway path. CC Gateway must:

- treat raw credential headers as sensitive;
- verify them against the account's `credential_binding_hmac`;
- never log them;
- rewrite them into the correct upstream auth shape;
- strip all CC/Sub2API internal headers before AWS egress.

### 7.3 CC Gateway -> AWS upstream headers

For the Phase 1 `x_api_key` profile, after CP0/CP3 evidence enables that profile, final upstream headers must include exactly one API-key auth header:

```text
x-api-key: <selected AWS Claude Platform API key>
anthropic-workspace-id: <server-owned workspace id>
anthropic-version: 2023-06-01
content-type: application/json
```

For the Phase 1 `bearer_api_key` profile, after CP0/CP3 evidence enables that profile, final upstream headers must include exactly one bearer auth header:

```text
Authorization: Bearer <selected AWS Claude Platform API key or short-term token>
anthropic-workspace-id: <server-owned workspace id>
anthropic-version: 2023-06-01
content-type: application/json
```

The two profiles are mutually exclusive. `x_api_key` must not emit `Authorization`, `bearer_api_key` must not emit `x-api-key`, and neither profile may forward a client-supplied auth header. If neither profile is proven for the target credential, production remains fail-closed.

The final request must not include:

- `x-cc-*`
- `x-sub2api-*`
- user-supplied `anthropic-workspace-id`
- user-supplied `authorization` or `x-api-key`
- raw CCH/billing material when `strip_attribution` is selected
- client-supplied `anthropic-beta`; the final value must be provider-profile-owned
- internal compatibility query/header markers such as a downstream `?beta=true` unless the AWS profile explicitly allows them
- proxy credentials in headers or evidence

## 8. Formal-pool attestation extensions

Extend the doc 56/58 attested context with these authority fields:

```json
{
  "provider_kind": "claude_platform_aws",
  "upstream_auth_scheme": "x_api_key",
  "aws_region": "us-west-2",
  "upstream_endpoint_ref": "endpoint:<safe-ref>",
  "upstream_host": "aws-external-anthropic.us-west-2.api.aws",
  "allowed_upstream_path": "/v1/messages",
  "workspace_ref": "workspace:<safe-ref>",
  "workspace_binding_hmac": "hmac-sha256:<safe-ref>",

  "account_id": "account:<safe-ref>",
  "credential_ref": "credential:<safe-ref>",
  "egress_bucket": "egress:<safe-ref>",
  "proxy_identity_ref": "proxy:<safe-ref>",
  "policy_version": "...",
  "persona_profile": "...",
  "trusted_egress_profile_ref": "strip_attribution",
  "profile_policy_version": "...",
  "billing_shape_policy": "strip",
  "request_shape_profile_ref": "...",
  "cache_parity_profile_ref": "...",
  "beta_policy_ref": "...",
  "observed_client_profile": { "safe_summary_only": true },
  "session_id": "<canonical session ref>",
  "timestamp_ms": 0,
  "nonce": "<safe nonce>"
}
```

CC Gateway must bind `provider_kind`, `upstream_auth_scheme`, `aws_region`, `workspace_ref`, `workspace_binding_hmac`, `upstream_endpoint_ref`, final upstream host/path policy, and `beta_policy_ref` into its session authority ledger. A same formal-pool session must fail closed if any of these change, just like account, credential, egress, persona, policy, device, or billing profile changes.

Canonical serialization requirement:

- Before adding these fields, Sub2API and CC Gateway must agree on the exact canonical JSON used for HMAC/signature verification.
- Add shared fixture vectors for old and new contexts.
- Sub2API tests must prove the byte string signed for the context matches CC Gateway's sorted/canonical verifier, not merely Go's default `json.Marshal` map order.

## 9. CC Gateway requirements

For Claude Platform on AWS, CC Gateway must independently enforce:

1. `mode: sub2api` for shared formal-pool production.
2. Account identity exists for the selected `account_id`.
3. `provider_kind = claude_platform_aws` matches the selected account identity.
4. `credential_ref` and selected credential binding match the account identity.
5. `workspace_ref` and workspace binding match the account identity.
6. `aws_region` matches the configured endpoint host.
7. `upstream_base_url` is allowlisted as `https://aws-external-anthropic.<region>.api.aws`, and final host/region/path/query match the provider profile. Phase 1 allows `/v1/messages`; count-tokens/files/batches/agents/admin APIs remain blocked unless a later profile explicitly permits them.
8. `egress_bucket` allows the account and has a proxy identity; no proxy means fail closed for this type.
9. Persona/profile/billing fields from doc 58 match the attested context. `strip_attribution` remains default; `no_cch` and `signed_cch` remain fail-closed unless doc-58 oracle/profile gates explicitly pass for this provider profile.
10. `metadata.user_id` and `X-Claude-Code-Session-Id` are rewritten/verified from server-owned session/account/device refs.
11. `anthropic-workspace-id` is injected from CC Gateway trusted account identity or sensitive runtime mapping, not from user input.
12. Final verifier confirms no internal control headers, client auth/workspace headers, unapproved beta tokens, internal query markers, control-plane routes, or forbidden billing/CCH material remain.
13. Evidence and raw capture files use only safe summaries: header names, booleans, lengths, schema summaries, safe refs.
14. Runtime registration and runtime mapping persistence include provider/workspace/upstream fields in `RuntimeRegisterRequest`, `RuntimeMappingRecord`, conflict comparison, replay, and redacted diagnostics. Unknown extra provider/workspace fields must not be silently ignored.
15. Upstream safety allows `aws-external-anthropic.<region>.api.aws` only when `provider_kind = claude_platform_aws`, `aws_region` matches the endpoint region, and the formal-pool account identity matches the workspace/endpoint refs.
16. Final verifier explicitly checks `anthropic-workspace-id` presence/source, upstream endpoint host/region/path/query, selected auth scheme, provider-scoped beta policy, absence of internal control headers, and absence of user-supplied auth/workspace headers after rewrite.
17. Personal standalone first-party Anthropic OAuth/API-key behavior remains intact. Formal-pool production for `claude_platform_aws` must not run in standalone or direct-bypass mode.

Gateway ordering requirements:

- Upstream safety must be two-stage or reordered for AWS Platform. A generic/global `config.upstream.url` allowlist may only admit localhost/mock or preflight-safe targets before attestation. Real `aws-external-anthropic.<region>.api.aws` egress must not be allowed solely because the global URL is configured.
- The real AWS host/region/path/query allowlist must run after CC Gateway verifies the Sub2API attestation, account identity, credential binding, workspace binding HMAC, egress bucket, proxy identity, and provider profile.
- Provider final verification must run after provider-aware upstream URL resolution and after final auth/workspace/persona/beta/billing header rewrite. Its inputs must include the final `upstreamUrl`, final headers, final body, selected account identity, verified attestation, session ledger binding, and selected provider/request-shape profile.
- No mutation is allowed after provider final verification except transport-only mechanics that cannot alter authority or request semantics, such as connection pooling or recomputed `Content-Length`. Auth, workspace, beta, route/path/query, body, billing/CCH, persona, and internal-control headers must not change after verification.
- Runtime mapping persistence must be versioned or capability-gated. Old mappings without `provider_kind`, `workspace_ref`, `workspace_binding_hmac`, endpoint, auth-scheme, beta-policy, request-shape, and cache-profile fields may continue only as first-party/non-AWS identities. They must be blocked from AWS formal-pool production until re-registered with the new schema.

## 10. Sub2API requirements

Sub2API must implement:

1. New account type constant and validators for `claude-platform-aws`.
2. UI card and import form that does not alter existing OAuth/Setup Token/API-key/Bedrock branches.
3. Multiple workspace rows, each with required proxy assignment.
4. Server-side workspace/account/credential safe refs and HMAC bindings.
5. Scheduler selection that treats each workspace account as an independent formal-pool candidate.
6. Sticky session tuple extended with `workspace_ref`, `provider_kind`, `upstream_auth_scheme`, `aws_region`, and `endpoint_ref`.
7. CC Gateway runtime registration update for each account.
8. Account test path that uses safe mock or controlled upstream and never stores raw request/response bodies or secret headers.
9. Redaction for raw `anthropic_workspace_id` in logs, admin diagnostic responses, test artifacts, and evidence files.
10. The dedicated `POST /admin/accounts/claude-platform-aws/batch` import API with validate-all-first behavior, idempotency key, safe duplicate handling, and safe per-row response schema.
11. Canonical HMAC context serialization parity with CC Gateway, covered by shared fixtures.
12. A dedicated direct request builder/dispatcher for `claude-platform-aws` account tests and non-formal-pool diagnostics.
13. Provider-scoped beta/header/request-shape profile selection, with client-supplied beta/profile hints recorded only as safe observations.
14. Safe account healthcheck through the selected proxy using a mock or explicit user-approved controlled upstream; healthcheck artifacts must not contain raw request/response bodies or secret headers.
15. Feature flag or production admission gate so partially imported AWS Platform accounts cannot receive formal-pool traffic until CC Gateway registration, local mock E2E, and evidence gates pass.
16. Scheduler snapshot/cache/DTO allowlists and tests that prove raw `anthropic_workspace_id` and other sensitive fields are never exposed.
17. Complete AWS sticky tuple persistence and compare-and-fail-closed behavior; `session -> account_id` alone is not enough.

## 11. UI design

Add a new Anthropic account card next to the existing Anthropic cards:

```text
Claude Platform on AWS
API Key / SigV4 (SigV4 gated)
```

Phase 1 UI behavior:

- Shows API-key mode as enabled.
- Shows SigV4 as disabled or marked "later / gated" unless Phase 2 is implemented.
- Allows adding multiple workspace rows:
  - `Workspace ID`
  - `AWS Region`
  - `API Key` or shared API-key selector depending on final UX; even if one raw key is reused, each workspace row still gets a distinct account identity, workspace binding, credential ref, and proxy binding
  - `Proxy`
  - optional account name suffix / group / concurrency
- Requires proxy selection on every row.
- Derives and displays the base URL from region, e.g. `https://aws-external-anthropic.<region>.api.aws`.
- Does not expose a free-form production base URL.
- Does not change existing Claude Code OAuth/Setup Token controls or Bedrock region/auth controls.

## 12. Why this is not the current AWS Bedrock card

| Aspect | Existing AWS Bedrock | New Claude Platform on AWS |
|---|---|---|
| Endpoint | Bedrock Runtime endpoint | `aws-external-anthropic.<region>.api.aws` |
| API shape | Bedrock InvokeModel style and Bedrock body conversion | Anthropic-native `/v1/messages` |
| Model IDs | Bedrock model IDs / region prefixes | Native Claude model names |
| Auth | Bedrock SigV4 service `bedrock` or Bedrock API key | AWS Claude Platform API key (`x-api-key` Phase 1; Bearer only if separately proven) or SigV4 service `aws-external-anthropic` |
| Required workspace header | No | Yes, `anthropic-workspace-id` |
| Formal-pool identity | Account/proxy only today | Account + credential + workspace + endpoint + proxy |

Therefore, extending the Bedrock card would be misleading and risky.

## 13. Checkpoint implementation plan

### Checkpoint 0 - Hard gate: baseline, protocol, target shape, and branch hygiene

Implementation work may start after this document is approved, but production enablement is blocked until CP0 exits `PASS`. CP0 is the first hard gate for any real AWS Platform formal-pool traffic.

- Confirm this plan with the user before code changes.
- Verify work roots:
  - Sub2API: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-platform-aws-formal-pool`
  - CC Gateway: `/Users/muqihang/chelingxi_workspace/cc-gateway`
- Do not touch the dirty main checkout, 3012, or stale 2173 worktree.
- Verify the doc-58 baseline is present before coding:
  - Sub2API branch contains trusted server-side formal-pool context generation, forged authority-header stripping/rejection, sticky session tuple, `observed_client_profile` audit-only behavior, and signed-CCH fail-closed gates.
  - CC Gateway branch contains attestation verification, account/credential/egress/persona/session/profile final verifier, persistent/shared ledger or production fail-closed equivalent, control-plane isolation, `strip_attribution` default, and no direct fallback.
- Prove the target AWS Platform protocol shape with safe evidence only:
  - endpoint is exactly `https://aws-external-anthropic.us-east-1.api.aws`;
  - target workspace ID has shape `wrkspc_...`, but raw value is stored only in sensitive credential/runtime storage;
  - workspace region is proven to be `us-east-1`; mismatch or unknown region is `BLOCKED_REGION_WORKSPACE`;
  - final upstream request host is `aws-external-anthropic.us-east-1.api.aws`;
  - final path is `/v1/messages`;
  - final query is empty unless a later named AWS request-shape profile explicitly allows otherwise;
  - final headers include exactly one server-selected auth profile (`x_api_key` or `bearer_api_key`), exactly one server-owned `anthropic-workspace-id`, `anthropic-version`, and `content-type`;
  - final headers contain no user-supplied auth/workspace/beta/internal context headers;
  - final body shape is Anthropic Messages-compatible and recorded only as safe schema summary/top-level key summary, never raw prompt/body/response.
- Auth profile proof matrix:
  - test `x_api_key`: final request uses `x-api-key` and no `Authorization`;
  - test `bearer_api_key`: final request uses `Authorization: Bearer` and no `x-api-key`;
  - if one profile is proven, production may enable only that profile for the target account/session tuple;
  - if both profiles appear to work, the operator must explicitly choose one profile for the account/session tuple; runtime silent fallback remains forbidden;
  - if neither is proven, status is `BLOCKED_AUTH_PROFILE` and production remains fail-closed.
- CP0 evidence may contain only: doc link set/retrieval date, endpoint ref, `workspace_ref`, `region=us-east-1`, auth profile ref, request-shape profile ref, booleans, status-code bucket, and pass/block reason. It must not contain raw workspace ID, API key, Authorization, `x-api-key`, raw HMAC input/output, raw prompt/body/response, cookies, proxy credentials, or raw telemetry.

Exit gate: user approves scope, doc-58 substrate is present, endpoint/workspace/region/request shape are proven, exactly one auth profile is selected or production is `BLOCKED_AUTH_PROFILE`, and any missing 58 substrate is marked `BLOCKED_BASELINE` instead of being bypassed.

### Checkpoint 1 - Sub2API account model and validation

TDD first:

- Add backend tests proving:
  - `claude-platform-aws` account type accepts valid region/workspace/API-key/proxy.
  - Missing proxy fails.
  - Invalid workspace ID fails.
  - Base URL/region mismatch fails.
  - Multiple different workspace IDs create separate account identities.
  - Duplicate rows return deterministic safe duplicate/idempotency results without logging raw workspace IDs.
  - Same raw API key reused across two workspaces still creates distinct workspace/account/session bindings.
  - Existing OAuth/Setup Token/API-key/Bedrock creation still passes.
  - `claude-platform-aws` is formal-pool eligible only through the new eligibility predicate and only when safe refs/proxy/workspace binding exist.
  - Ordinary first-party Anthropic `apikey` accounts do not become formal-pool eligible by accident.
  - Logs/errors redact API key and raw workspace ID.
  - Scheduler cache metadata, account DTOs, healthcheck summaries, diagnostics, and evidence fixtures expose only safe workspace refs and never raw workspace IDs.

Implementation:

- Add `AccountTypeClaudePlatformAWS = "claude-platform-aws"`.
- Add credential validator and safe-ref builder.
- Add redaction helpers for raw workspace ID.
- Add the dedicated `POST /admin/accounts/claude-platform-aws/batch` route/service with validate-all-first behavior, idempotency key handling, duplicate detection, and safe per-row response schema.
- Add a production admission flag so newly imported rows remain non-formal-pool-schedulable until CC Gateway registration and health gates pass.

Exit gate: targeted Go tests pass and CodeGraph incrementally indexed if code changed.

Minimum CP0/CP1 test matrix that must be represented before production work proceeds:

- Auth profile tests prove `x_api_key` and `bearer_api_key` are mutually exclusive, no profile is enabled without CP0 evidence, and silent fallback is impossible.
- Region/workspace tests prove `workspace_ref` is bound to `us-east-1` and `https://aws-external-anthropic.us-east-1.api.aws`; region mismatch or unknown workspace region fails closed.
- Client spoofing tests prove forged `anthropic-workspace-id`, `x-api-key`, `Authorization`, `x-cc-*`, `x-sub2api-*`, account/credential/egress/persona/profile/session/billing/CCH/control-plane/internal context headers are ignored or rejected.
- CC Gateway final verifier tests prove final upstream request has only server-selected auth/workspace, host/path/query/profile match, and all internal headers are stripped.
- Session tuple tests prove the same formal-pool session cannot switch workspace, credential, egress, proxy, profile, beta policy, endpoint, auth scheme, or device.
- Redaction tests prove artifacts/logs/DTOs/scheduler cache/evidence contain no raw workspace ID, API key, Authorization, `x-api-key`, raw body, raw response, raw HMAC input/output, cookies, proxy credentials, or raw telemetry.
- No-bypass tests prove AWS Platform formal-pool production traffic cannot bypass CC Gateway and connect directly to the AWS upstream.

### Checkpoint 2 - UI card and multi-workspace import UX

TDD first:

- Add frontend tests proving:
  - The new card renders independently.
  - Bedrock card remains unchanged.
  - OAuth/Setup Token card remains unchanged.
  - Each workspace row requires proxy.
  - Multiple rows can be added and show per-row validation/status.
  - Payload never places raw workspace ID in `extra` safe-ref fields or safe evidence fields.

Implementation:

- Update `CreateAccountModal.vue` or split a focused `ClaudePlatformAWSFields.vue` component if the modal becomes too large.
- Add i18n strings in `frontend/src/i18n/locales/en.ts` and `frontend/src/i18n/locales/zh.ts`.
- Prefer a dedicated component if it prevents further growth of the already large modal.

Exit gate: targeted frontend tests pass and existing account modal tests do not regress.

### Checkpoint 3 - API-key upstream path and mock AWS tests

TDD first:

- Add mock upstream tests proving:
  - Request URL is `/v1/messages` on the derived AWS endpoint; internal compatibility markers such as `?beta=true` are not leaked unless explicitly profile-approved.
  - Model names remain Anthropic-native and are not Bedrock-mapped.
  - Server-owned `anthropic-workspace-id` is injected.
  - User-forged `anthropic-workspace-id` is stripped/rejected.
  - Auth is `x-api-key: <selected credential>` only when the gated `x_api_key` profile is enabled by CP0 evidence.
  - If the `x_api_key` protocol check is not green, the tests prove production remains blocked rather than silently switching auth schemes.
  - `Authorization: Bearer` is absent unless an explicit `bearer_api_key` compatibility profile is enabled by evidence.
  - Provider-scoped beta policy rewrites/filters/rejects fixture beta tokens deterministically and does not reuse Vertex/Bedrock policy by accident.
  - No raw API key, workspace ID, Authorization, prompt/body/response, or CCH appears in evidence.

Implementation:

- Add a Claude Platform on AWS request builder separate from Bedrock request builder.
- Add dispatch so `type = "claude-platform-aws"` reaches that builder and does not enter existing first-party Anthropic `apikey` passthrough, Vertex service-account, or Bedrock paths.
- Add provider-scoped header policy for workspace/auth/beta/path normalization.
- Keep Bedrock `ResolveBedrockModelID`, Bedrock signer, and Bedrock body conversion untouched.
- Ensure direct account test routes use safe captures only.

Exit gate: mock AWS targeted tests pass.

### Checkpoint 4 - Formal-pool Sub2API -> CC Gateway contract

TDD first:

- Add Sub2API tests proving trusted context includes safe AWS provider/workspace/endpoint fields and `workspace_binding_hmac` from scheduler state only.
- Add shared canonical JSON/HMAC fixture tests proving Sub2API serialized bytes match CC Gateway verifier bytes.
- Add forged-header tests for `anthropic-workspace-id`, `authorization`, `x-api-key`, `x-amz-*`, `anthropic-beta`, `x-cc-*`, and `x-sub2api-*`.
- Add sticky tuple persistence tests proving `session -> account_id` alone is insufficient and mismatches fail closed for workspace/account/credential/egress/policy/persona/device/profile/beta-policy/endpoint/auth-scheme/proxy changes.
- Add tests proving account healthcheck and scheduler eligibility fail closed until proxy binding, CC Gateway runtime registration, and provider profile fields are present.

Implementation:

- Extend `cc_gateway_adapter.go` formal-pool attestation fields.
- Extend scheduler/session mapper sticky tuple with provider/account/credential/workspace/workspace-binding/endpoint/region/auth/egress/proxy/persona/profile/beta-policy/device fields.
- Extend CC Gateway runtime registration payload with safe refs and sensitive workspace storage.
- Ensure server-side scheduler state, not inbound client headers, is the only source for AWS provider/workspace/endpoint/proxy/profile fields.

Exit gate: Sub2API targeted tests pass and no evidence leakage.

### Checkpoint 5 - CC Gateway verifier, ledger, and final upstream injection

TDD first:

- Add CC Gateway tests proving:
  - Missing workspace identity fails closed.
  - Workspace ref mismatch fails closed.
  - Region/base URL mismatch fails closed.
  - Host/path/query mismatch fails closed, including accidental leak of an internal `?beta=true` marker when the AWS profile allows only `/v1/messages`.
  - `aws-external-anthropic.<region>.api.aws` is allowed only for `provider_kind=claude_platform_aws`; other providers cannot use it.
  - Runtime registration persists provider/workspace/workspace-binding/upstream/auth/beta-policy fields and rejects conflicting replay.
  - Old runtime mappings without AWS provider/workspace/capability fields are blocked from AWS formal-pool production instead of defaulting open.
  - Same session cannot swap workspace, workspace binding HMAC, region, endpoint, auth scheme, beta policy, request-shape/cache profile, or device.
  - Egress allowlist must include the account and proxy identity.
  - Auth scheme `x_api_key` emits the internal selected credential as `x-api-key` only after binding verification and only when the gated profile is enabled; otherwise it fails closed and strips any user-supplied auth headers.
  - Provider-aware upstream safety runs after attestation/account/workspace binding verification for real AWS egress.
  - Provider final verifier runs after final URL resolution and final auth/workspace/header/body rewrite, receives final URL/headers/body/identity/attestation/session/profile inputs, and rejects any post-verifier authority mutation of auth/workspace/beta/body/path/query/billing/persona semantics.
  - Final upstream request has exactly one `anthropic-workspace-id`, exactly one verified API-key auth header for the enabled profile, no internal headers, no client-supplied beta/header authority, no forbidden billing/CCH markers, and safe evidence only.
  - Control-plane/admin/workspace/file/batch/agent routes cannot reach AWS upstream under the Phase 1 messages-only formal-pool profile.
  - Existing first-party Anthropic OAuth/API-key standalone and formal-pool tests still pass.

Implementation:

- Extend `AccountContext`, `AttestedFormalPoolContext`, `AccountIdentityConfig`, and `FormalPoolSessionAuthorityBinding`.
- Add provider-aware upstream URL resolution.
- Extend runtime registration/request/config schemas and redacted persistence for provider/workspace/workspace-binding/upstream/auth/beta-policy fields, with schema-version or capability gating for old mappings.
- Extend upstream-safety allowlist with a provider-aware, post-attestation region-bound AWS Claude Platform gate.
- Add provider-aware header rewrite for Claude Platform on AWS.
- Add final verifier checks for workspace/endpoint/path/query/auth scheme/beta policy/billing policy after final URL/header construction.
- Keep personal standalone behavior intact unless formal-pool/shared-account config is present.

Exit gate: CC Gateway targeted tests pass on `/Users/muqihang/chelingxi_workspace/cc-gateway` main-based checkout.

### Checkpoint 6 - Local full-chain E2E with safe mock upstream

TDD/E2E first:

- Run local full chain:

```text
native Claude-compatible request
  -> Sub2API local
  -> CC Gateway local
  -> mock aws-external-anthropic upstream
```

Assertions:

- Mock upstream receives `/v1/messages` on the derived AWS host/region; internal compatibility query markers do not leak unless profile-approved.
- Mock upstream receives server-owned workspace header; reports only `workspace_header_present: true` and safe `workspace_ref`, not the raw value.
- Mock upstream receives server-owned API-key auth; reports only `x_api_key_present: true` by default. If a later `bearer_api_key` compatibility profile is enabled, that separate test reports only `authorization_present: true`.
- Proxy selection is reflected by safe proxy identity ref.
- No internal headers, user-supplied auth/workspace/beta headers, or forbidden billing/CCH markers reach upstream under `strip_attribution`.
- Evidence contains only safe refs, booleans, lengths, status codes, and schema summaries.

Exit gate: localhost full-chain green before any 3017 or deployed consideration.

### Checkpoint 7 - Optional SigV4 phase

Do not start until API-key mode is green.

TDD first:

- Add SigV4 tests proving:
  - Service name is `aws-external-anthropic`.
  - Signing region equals endpoint region.
  - Signing happens after final body/header rewrite.
  - `anthropic-workspace-id` is included exactly as required by the canonical request.
  - Temporary session token handling is correct.
  - No signing canonical request, secret key, token, raw body, or raw workspace ID is logged.

Implementation:

- Add a CC Gateway SigV4 signer for final outbound AWS requests.
- Do not reuse the Bedrock signer, because it signs service `bedrock` and belongs to a different request path.

Exit gate: SigV4 mock/canonical tests pass; live canary still requires explicit user approval.

## 14. Production gates

Production enablement requires all of the following. The document phase is not production evidence, and any missing item means the feature remains feature-flagged, disabled, or fail-closed:

- CP0 hard gate `PASS`: endpoint/workspace/region/request-shape proof is present, and exactly one auth profile (`x_api_key` or `bearer_api_key`) is proven and selected; otherwise production is `BLOCKED_AUTH_PROFILE`, `BLOCKED_REGION_WORKSPACE`, or `BLOCKED_SHAPE`.
- Doc 58 safety substrate verified in the implementation branch, with targeted tests green for trusted profile fields, session ledger, final verifier, `strip_attribution`, and signed-CCH fail-closed behavior.
- Targeted unit tests green for Sub2API account validation, scheduler/cache/DTO redaction, dedicated batch import/idempotency/per-row semantics, direct AWS builder, trusted context, forged headers, complete sticky tuple persistence, provider-scoped beta policy, and no-bypass behavior.
- CC Gateway provider final verifier tests green for runtime schema/version gates, attestation, account/credential/workspace binding, workspace binding HMAC, region/endpoint/path/query, egress allowlist, post-attestation upstream safety, provider-aware rewrite, route/control-plane isolation, beta/cache/request-shape profile enforcement, billing/CCH strip/sign gates, session ledger, exactly-one-auth-header enforcement, and no post-verifier semantic mutation.
- Sub2API full-chain local mock E2E green for at least two AWS workspace accounts with distinct proxy refs, proving scheduler can select multiple AWS upstreams without mixing workspace/account/credential/egress/session.
- Safe artifact scan green across generated logs, DTO snapshots, scheduler cache evidence, test artifacts, local E2E reports, and documentation. Scan must reject raw workspace ID, API keys, Authorization, `x-api-key`, raw body, raw response, raw HMAC input/output, cookies, proxy credentials, and raw telemetry.
- CodeGraph incrementally indexed after code changes in each repo when a `.codegraph/` directory exists; if absent, record that indexing was not available for that worktree.
- 55 evidence report and final evidence map updated with safe evidence only.
- Deployed image/commit/config/profile equivalence proven with safe hashes and exact commit refs; no deployed/canary claim is valid without this.
- Tiny approved live smoke passes only after targeted tests, local full-chain E2E, safe artifact scan, and deployed equivalence are green. The live smoke requires explicit user approval and a tiny cost envelope.
- No live formal-pool traffic until localhost E2E, deployed equivalence, and approved live smoke pass.
- No 3017 rebuild/smoke until targeted tests and local full-chain E2E are green and the user explicitly asks for deployment/validation.
- No 3012 changes.
- No standalone formal-pool production. Personal standalone remains allowed.
- `signed_cch` / sign-primary remains fail-closed unless doc 58 oracle/profile gates pass.
- Bearer-token API-key compatibility and SigV4 remain disabled unless their named profiles have explicit proof.

## 15. Evidence status for this plan

| Item | Status | Evidence |
|---|---|---|
| Official endpoint/header semantics | PASS | AWS docs linked in section 1 |
| Current screenshot equals Bedrock path | PASS | `CreateAccountModal.vue` Bedrock card and Bedrock backend signer/request code |
| New card/type needed | PASS | Bedrock endpoint/auth/model/body semantics differ from Claude Platform on AWS |
| API-key MVP feasibility | PASS as design; BLOCKED_CP0 for production | AWS supports API-key auth against same regional endpoint, but `x_api_key` vs `bearer_api_key` must be proven by CP0 |
| Multiple workspace support | PASS as design | One workspace per account row, optional batch import |
| Per-workspace proxy | PASS as design | `proxy_id` required per created account row |
| Formal-pool safety integration | PASS as design | Extends doc 56/58 context, verifier, and ledger fields without weakening `strip_attribution`/fail-closed defaults |
| Doc 58 baseline alignment | PASS as reviewed | 59 now targets Sub2API 59 worktree from doc-58 merge candidate and CC Gateway `443052a`; CP0 blocks if substrate is missing |
| Provider-scoped beta/header policy | PASS as design | AWS Platform policy is separate from Vertex/Bedrock; fixture tokens from the Vertex issue must be tested without expanding 59 into a Vertex fix |
| Multiple AWS upstream production support | PASS as design | One workspace per account identity; local E2E must include at least two workspace/proxy refs before production |
| SigV4 implementation | BLOCKED_EXTERNAL_EVIDENCE until tests | Official docs confirm service/region; local final-signer implementation and tests not yet done |
| Current code feasibility review | PASS_WITH_REQUIRED_EDITS | Prior Raman review found required edits around account predicates, batch API, redaction, direct builder, canonical attestation parity, runtime schema, upstream safety, final verifier, and doc 58 sequencing; this revision incorporates those requirements |
| CP0 hard gate | BLOCKED_PENDING_EXECUTION | Must prove endpoint/workspace/region/request-shape and exactly one auth profile before production |
| Production readiness | BLOCKED_EXTERNAL_EVIDENCE | Requires CP0, code implementation, targeted unit tests, Sub2API full-chain local mock E2E, CC Gateway final verifier tests, safe artifact scan, deployed equivalence, tiny approved live smoke, and evidence updates |

## 16. CP1 broad-suite failure audit before CP2

Status as of 2026-06-27: CP0/CP1 targeted tests are green, but `cd backend && go test ./...` is not green. The failures below were investigated before CP2 and must not be reported as a green broad suite.

Audit method:

- Reproduced failing packages with targeted commands.
- Initialized CodeGraph in this worktree and confirmed it is locally ignored by `.git/info/exclude`.
- Compared suspect source/test blobs at `80fc0963f`, `fa50af8cfa26`, and current `HEAD` without switching worktrees.
- Checked `git diff --name-only 80fc0963f..fa50af8cfa26` and `fa50af8cfa26..HEAD` for each suspect package/file.
- Used `git log -S` to identify older candidate commits for historical failures.

Findings:

| Package / command | Failure | Relation to `fa50af8cfa26` | Status |
|---|---|---|---|
| `go test ./ent/schema -count=1` | Setup fails while downloading `golang.org/x/tools@v0.44.0` from `proxy.golang.org` with `i/o timeout`. | No code-path relation found; this is an external network/module-cache failure. | `BLOCKED_EXTERNAL_NETWORK` |
| `go test ./internal/handler/admin -count=1` | Build failures: `accountRefreshProxyRepoStub` lacks `CountExpired`; several `NewSettingHandler` calls pass 6 args instead of the current 7 including `*service.UserAttributeService`. | Suspect files are byte-identical at `80fc0963f`, `fa50af8cfa26`, and current `HEAD`. `CountExpired` traces to older proxy expiry work; `UserAttributeService` traces to older auth-identity/settings work. | `BLOCKED_HISTORICAL_TEST_DRIFT` |
| `go test ./internal/repository -run 'Test(SchedulerCacheMetadataSerializationPreservesFormalPoolSchedulableEvidence\|BuildSchedulerMetadataAccount_PreservesFormalPoolSchedulableEvidence)' -count=1` | Scheduler metadata tests fail because the slim cache allowlist/test fixture does not preserve all current formal-pool runtime evidence now required by `runtimeEvidenceComplete` / `healthcheckEvidenceComplete`. | Suspect files are byte-identical at `80fc0963f`, `fa50af8cfa26`, and current `HEAD`; stricter formal-pool evidence traces to older trusted gateway profile work, not AWS Platform 59. | `BLOCKED_HISTORICAL_FORMAL_POOL_TEST_DRIFT` |
| `go test ./internal/server/routes -run '^TestFormalPoolOperationsRoutes_PromoteProductionSuccessReturnsSafeAccount$' -count=1` | Route fixture expects promote-production success, but fixture lacks complete current runtime evidence and returns `PRODUCTION_EVIDENCE_INCOMPLETE`. | Suspect files are byte-identical at `80fc0963f`, `fa50af8cfa26`, and current `HEAD`; same historical formal-pool evidence-gate drift as above. | `BLOCKED_HISTORICAL_FORMAL_POOL_TEST_DRIFT` |
| `go test ./internal/handler -run '^TestGatewayHandlerBridgeLiveStoresSafeAuditSummaryOnContext$' -count=1 -v` | Expected `CacheMissTokens=13`, actual `0`. | Suspect handler/bridge files are byte-identical at `80fc0963f`, `fa50af8cfa26`, and current `HEAD`; failure traces to older Claude Code bridge/cache-audit work, not AWS Platform 59. | `BLOCKED_HISTORICAL_BRIDGE_TEST_DRIFT` |

CP2 entry decision:

- These broad-suite failures are not caused by `fa50af8cfa26` or later 59 commits.
- They remain recorded blockers for any future "broad suite green" or production-readiness claim.
- They do not change CP0 auth profile status: production remains `BLOCKED_AUTH_PROFILE` because no real target AWS Platform auth evidence has been supplied.
- CP2 may proceed only as frontend/import UX work on top of the already committed CP0/CP1 targeted green state; do not claim the broad Go suite is green until the historical/external failures above are fixed or the environment issue clears.

## 17. Non-goals

- Do not merge Claude Platform on AWS into the Bedrock card.
- Do not permit arbitrary custom base URLs for formal-pool production.
- Do not use user-supplied `anthropic-workspace-id` as authority.
- Do not change existing OAuth/Setup Token login behavior.
- Do not enable `signed_cch` or sign-primary as part of this AWS upstream work.
- Do not implement a Vertex beta-token patch in this plan; only add provider-scoped AWS safeguards if shared header code would otherwise create risk.
- Do not enable Bearer API-key compatibility or SigV4 in Phase 1 without explicit profile proof.
- Do not claim live production readiness from this document alone; this document is not production evidence.
- Do not implement production formal-pool routing unless Checkpoint 0 confirms the completed doc-58 safety substrate is present.
